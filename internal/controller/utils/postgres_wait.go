package utils

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	olsv1alpha1 "github.com/openshift/lightspeed-operator/api/v1alpha1"
	"github.com/openshift/lightspeed-operator/internal/controller/reconciler"
)

// PostgresWaitSAVolumeName is the volume name for the service account token used by the Postgres wait init container.
// Mount this volume in the init container at PostgresWaitSAMountPath. The cluster CA for TLS is at the default
// service account path (DefaultServiceAccountPath) when the pod has ServiceAccountName set.
const PostgresWaitSAVolumeName = "postgres-wait-sa-token"
const PostgresWaitSAMountPath = "/var/run/postgres-wait-sa"
// DefaultServiceAccountPath is the standard path for the default SA volume (token, ca.crt, namespace).
const DefaultServiceAccountPath = "/var/run/secrets/kubernetes.io/serviceaccount"
// PostgresWaitMaxSeconds is the maximum time the wait init container will run before exiting with an error.
const PostgresWaitMaxSeconds = 300

// GeneratePostgresWaitInitContainer returns an init container that waits until the Postgres
// deployment is in a safe state (at most one ready instance) before the main container starts.
// This is backend-specific logic: ensure no more than one Postgres instance when connecting.
//
// Safe states:
//   - readyReplicas == 0 (Postgres disabled/deleted) → OK, exit 0
//   - readyReplicas == 1 && replicas == 1 → OK (stable single instance), exit 0
//
// Wait states (retry with backoff):
//   - readyReplicas == 0 && replicas == 1 → starting up
//   - readyReplicas >= 2 → scaling/restarting, unsafe
//
// The container uses the pod's service account to read the Postgres deployment via the Kubernetes API.
// Mount the volume from GeneratePostgresWaitSAVolume() at PostgresWaitSAMountPath in this container.
// image: if empty, PostgresWaitInitContainerImageDefault is used.
func GeneratePostgresWaitInitContainer(image string) corev1.Container {
	if image == "" {
		image = PostgresWaitInitContainerImageDefault
	}
	// Script: call Kube API with TLS (cluster CA), parse JSON with POSIX shell tools, apply safe-state rules; exponential backoff; max wait time.
	script := `
set -e
NAMESPACE=$(cat ` + PostgresWaitSAMountPath + `/namespace)
TOKEN=$(cat ` + PostgresWaitSAMountPath + `/token)
CA_CERT="` + DefaultServiceAccountPath + `/ca.crt"
API="https://kubernetes.default.svc/apis/apps/v1/namespaces/$NAMESPACE/deployments/` + PostgresDeploymentName + `"
sleep_sec=1
max_sleep=30
start=$(date +%s)
max_elapsed=` + fmt.Sprintf("%d", PostgresWaitMaxSeconds) + `
while true; do
  now=$(date +%s)
  elapsed=$((now - start))
  if [ $elapsed -ge $max_elapsed ]; then
    echo "wait-for-postgres: timed out after ${max_elapsed}s" >&2
    exit 1
  fi
  if ! json=$(wget -qO- --header="Authorization: Bearer $TOKEN" --ca-certificate="$CA_CERT" "$API" 2>/dev/null); then
    echo "wait-for-postgres: API call failed, retrying" >&2
    sleep $sleep_sec
    sleep_sec=$((sleep_sec * 2))
    if [ $sleep_sec -gt $max_sleep ]; then sleep_sec=$max_sleep; fi
    continue
  fi
  if [ -z "$json" ]; then
    echo "wait-for-postgres: empty API response, retrying" >&2
    sleep $sleep_sec
    sleep_sec=$((sleep_sec * 2))
    if [ $sleep_sec -gt $max_sleep ]; then sleep_sec=$max_sleep; fi
    continue
  fi
  if ! one_line=$(printf '%s' "$json" | tr -d '\n'); then
    echo "wait-for-postgres: failed to parse deployment JSON, retrying" >&2
    sleep $sleep_sec
    sleep_sec=$((sleep_sec * 2))
    if [ $sleep_sec -gt $max_sleep ]; then sleep_sec=$max_sleep; fi
    continue
  fi
  if ! ready=$(printf '%s' "$one_line" | sed -n 's/.*"readyReplicas"[[:space:]]*:[[:space:]]*\([0-9][0-9]*\).*/\1/p'); then
    echo "wait-for-postgres: failed to parse deployment JSON, retrying" >&2
    sleep $sleep_sec
    sleep_sec=$((sleep_sec * 2))
    if [ $sleep_sec -gt $max_sleep ]; then sleep_sec=$max_sleep; fi
    continue
  fi
  if ! replicas=$(printf '%s' "$one_line" | sed -n 's/.*"replicas"[[:space:]]*:[[:space:]]*\([0-9][0-9]*\).*/\1/p'); then
    echo "wait-for-postgres: failed to parse deployment JSON, retrying" >&2
    sleep $sleep_sec
    sleep_sec=$((sleep_sec * 2))
    if [ $sleep_sec -gt $max_sleep ]; then sleep_sec=$max_sleep; fi
    continue
  fi
  # Treat missing fields as startup defaults used by prior logic.
  if [ -z "$ready" ]; then ready=0; fi
  if [ -z "$replicas" ]; then replicas=1; fi
  if [ "$ready" = "0" ]; then
    exit 0
  fi
  if [ "$ready" = "1" ] && [ "$replicas" = "1" ]; then
    exit 0
  fi
  sleep $sleep_sec
  sleep_sec=$((sleep_sec * 2))
  if [ $sleep_sec -gt $max_sleep ]; then sleep_sec=$max_sleep; fi
done
`
	return corev1.Container{
		Name:            PostgresWaitInitContainerName,
		Image:           image,
		ImagePullPolicy: corev1.PullIfNotPresent,
		Command:         []string{"/bin/sh", "-c", script},
		SecurityContext: &corev1.SecurityContext{
			AllowPrivilegeEscalation: &[]bool{false}[0],
			ReadOnlyRootFilesystem:   &[]bool{true}[0],
		},
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("10m"),
				corev1.ResourceMemory: resource.MustParse("32Mi"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("100m"),
				corev1.ResourceMemory: resource.MustParse("64Mi"),
			},
		},
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      PostgresWaitSAVolumeName,
				MountPath: PostgresWaitSAMountPath,
				ReadOnly:  true,
			},
		},
	}
}

// GeneratePostgresWaitSAVolume returns a projected volume containing the service account token
// and namespace for the Postgres wait init container. Add this volume to the pod and mount it
// in the init container at PostgresWaitSAMountPath.
func GeneratePostgresWaitSAVolume() corev1.Volume {
	expirationSeconds := int64(3600)
	return corev1.Volume{
		Name: PostgresWaitSAVolumeName,
		VolumeSource: corev1.VolumeSource{
			Projected: &corev1.ProjectedVolumeSource{
				Sources: []corev1.VolumeProjection{
					{
						ServiceAccountToken: &corev1.ServiceAccountTokenProjection{
							Path:              "token",
							ExpirationSeconds: &expirationSeconds,
						},
					},
					{
						DownwardAPI: &corev1.DownwardAPIProjection{
							Items: []corev1.DownwardAPIVolumeFile{
								{
									Path: "namespace",
									FieldRef: &corev1.ObjectFieldSelector{
										FieldPath: "metadata.namespace",
									},
								},
							},
						},
					},
				},
			},
		},
	}
}

// GeneratePostgresWaitRole returns a Role that allows the backend service account to get the
// Postgres deployment (used by the wait init container). Create this in the same namespace as the backend.
func GeneratePostgresWaitRole(r reconciler.Reconciler, cr *olsv1alpha1.OLSConfig) (*rbacv1.Role, error) {
	role := &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Name:      PostgresWaitRoleName,
			Namespace: r.GetNamespace(),
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups:     []string{"apps"},
				Resources:     []string{"deployments"},
				ResourceNames: []string{PostgresDeploymentName},
				Verbs:         []string{"get"},
			},
		},
	}
	if err := controllerutil.SetControllerReference(cr, role, r.GetScheme()); err != nil {
		return nil, err
	}
	return role, nil
}

// GeneratePostgresWaitRoleBinding returns a RoleBinding that binds the backend service account
// to the Postgres wait Role.
func GeneratePostgresWaitRoleBinding(r reconciler.Reconciler, cr *olsv1alpha1.OLSConfig) (*rbacv1.RoleBinding, error) {
	rb := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      PostgresWaitRoleBindingName,
			Namespace: r.GetNamespace(),
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      OLSAppServerServiceAccountName,
				Namespace: r.GetNamespace(),
			},
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "Role",
			Name:     PostgresWaitRoleName,
		},
	}
	if err := controllerutil.SetControllerReference(cr, rb, r.GetScheme()); err != nil {
		return nil, err
	}
	return rb, nil
}
