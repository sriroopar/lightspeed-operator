package utils

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/client-go/kubernetes/scheme"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	olsv1alpha1 "github.com/openshift/lightspeed-operator/api/v1alpha1"
)

var _ = Describe("Postgres wait", func() {
	var r *TestReconciler
	var cr *olsv1alpha1.OLSConfig

	BeforeEach(func() {
		r = NewTestReconciler(k8sClient, logf.Log, scheme.Scheme, OLSNamespaceDefault)
		cr = GetDefaultOLSConfigCR()
	})

	Describe("GeneratePostgresWaitInitContainer", func() {
		It("uses default image when image is empty", func() {
			c := GeneratePostgresWaitInitContainer("")
			Expect(c.Name).To(Equal(PostgresWaitInitContainerName))
			Expect(c.Image).To(Equal(PostgresWaitInitContainerImageDefault))
			Expect(c.SecurityContext).NotTo(BeNil())
			Expect(*c.SecurityContext.AllowPrivilegeEscalation).To(BeFalse())
			Expect(*c.SecurityContext.ReadOnlyRootFilesystem).To(BeTrue())
			Expect(c.Resources.Requests).To(HaveKey(corev1.ResourceCPU))
			Expect(c.Resources.Requests).To(HaveKey(corev1.ResourceMemory))
			Expect(c.Resources.Requests[corev1.ResourceCPU]).To(Equal(resource.MustParse("10m")))
			Expect(c.Resources.Requests[corev1.ResourceMemory]).To(Equal(resource.MustParse("32Mi")))
			Expect(c.Resources.Limits[corev1.ResourceCPU]).To(Equal(resource.MustParse("100m")))
			Expect(c.Resources.Limits[corev1.ResourceMemory]).To(Equal(resource.MustParse("64Mi")))
			Expect(c.VolumeMounts).To(HaveLen(1))
			Expect(c.VolumeMounts[0].Name).To(Equal(PostgresWaitSAVolumeName))
			Expect(c.VolumeMounts[0].MountPath).To(Equal(PostgresWaitSAMountPath))
			Expect(c.Command).To(HaveLen(3))
			Expect(c.Command[2]).To(ContainSubstring("max_elapsed=" + fmt.Sprintf("%d", PostgresWaitMaxSeconds)))
			Expect(c.Command[2]).To(ContainSubstring("--ca-certificate"))
			Expect(c.Command[2]).To(ContainSubstring("ca.crt"))
			Expect(c.Command[2]).NotTo(ContainSubstring("--no-check-certificate"))
			Expect(c.Command[2]).To(ContainSubstring("API call failed, retrying"))
			Expect(c.Command[2]).To(ContainSubstring("empty API response, retrying"))
			Expect(c.Command[2]).To(ContainSubstring("failed to parse deployment JSON, retrying"))
			Expect(c.Command[2]).NotTo(ContainSubstring("|| ready=0"))
			Expect(c.Command[2]).NotTo(ContainSubstring("|| replicas=1"))
			Expect(c.Command[2]).NotTo(ContainSubstring("python3"))
			Expect(c.Command[2]).To(ContainSubstring("sed -n"))
		})

		It("uses custom image when provided", func() {
			c := GeneratePostgresWaitInitContainer("myimage:tag")
			Expect(c.Image).To(Equal("myimage:tag"))
		})
	})

	Describe("GeneratePostgresWaitSAVolume", func() {
		It("returns projected volume with token and namespace", func() {
			v := GeneratePostgresWaitSAVolume()
			Expect(v.Name).To(Equal(PostgresWaitSAVolumeName))
			Expect(v.Projected).NotTo(BeNil())
			Expect(v.Projected.Sources).To(HaveLen(2))
			Expect(v.Projected.Sources[0].ServiceAccountToken).NotTo(BeNil())
			Expect(v.Projected.Sources[0].ServiceAccountToken.Path).To(Equal("token"))
			Expect(v.Projected.Sources[1].DownwardAPI).NotTo(BeNil())
			Expect(v.Projected.Sources[1].DownwardAPI.Items).To(HaveLen(1))
			Expect(v.Projected.Sources[1].DownwardAPI.Items[0].Path).To(Equal("namespace"))
		})
	})

	Describe("GeneratePostgresWaitRole", func() {
		It("returns role with get deployments permission", func() {
			role, err := GeneratePostgresWaitRole(r, cr)
			Expect(err).NotTo(HaveOccurred())
			Expect(role.Name).To(Equal(PostgresWaitRoleName))
			Expect(role.Namespace).To(Equal(OLSNamespaceDefault))
			Expect(role.Rules).To(HaveLen(1))
			Expect(role.Rules[0].APIGroups).To(Equal([]string{"apps"}))
			Expect(role.Rules[0].Resources).To(Equal([]string{"deployments"}))
			Expect(role.Rules[0].ResourceNames).To(Equal([]string{PostgresDeploymentName}))
			Expect(role.Rules[0].Verbs).To(Equal([]string{"get"}))
			Expect(role.OwnerReferences).To(HaveLen(1))
			Expect(role.OwnerReferences[0].Kind).To(Equal("OLSConfig"))
			Expect(role.OwnerReferences[0].Name).To(Equal(cr.Name))
		})
	})

	Describe("GeneratePostgresWaitRoleBinding", func() {
		It("returns rolebinding binding SA to role", func() {
			rb, err := GeneratePostgresWaitRoleBinding(r, cr)
			Expect(err).NotTo(HaveOccurred())
			Expect(rb.Name).To(Equal(PostgresWaitRoleBindingName))
			Expect(rb.Namespace).To(Equal(OLSNamespaceDefault))
			Expect(rb.Subjects).To(HaveLen(1))
			Expect(rb.Subjects[0].Kind).To(Equal("ServiceAccount"))
			Expect(rb.Subjects[0].Name).To(Equal(OLSAppServerServiceAccountName))
			Expect(rb.Subjects[0].Namespace).To(Equal(OLSNamespaceDefault))
			Expect(rb.RoleRef.APIGroup).To(Equal("rbac.authorization.k8s.io"))
			Expect(rb.RoleRef.Kind).To(Equal("Role"))
			Expect(rb.RoleRef.Name).To(Equal(PostgresWaitRoleName))
			Expect(rb.OwnerReferences).To(HaveLen(1))
			Expect(rb.OwnerReferences[0].Kind).To(Equal("OLSConfig"))
			Expect(rb.OwnerReferences[0].Name).To(Equal(cr.Name))
		})
	})
})
