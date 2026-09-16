# Agentic v2 Bundle Automation

This document defines the checked-in automation contract for the OCP 5.0+
agentic bundle. The implementation is delivered after the v1/v2 bundle split
lands.

## Independent bundle line

The agentic bundle is an independent Konflux application and component:

```text
Application: ols-bundle-v2
Component:   ols-bundle-v2
CI image:    quay.io/redhat-user-workloads/crt-nshift-lightspeed-tenant/ols-bundle-v2
Stable image: registry.redhat.io/openshift-lightspeed/lightspeed-operator-bundle-v2
```

It is distinct from the v1 `ols-bundle` application. Shared component images
are consumed by digest; they are not rebuilt by the v2 bundle application.

## Bundle update inputs

A v2 update refreshes the digest-pinned entries in `related_images.json` for:

- classic/shared: `lightspeed-operator`, `lightspeed-service-api`,
  `lightspeed-console-plugin`, `lightspeed-postgresql`,
  `lightspeed-to-dataverse-exporter`, `openshift-mcp-server`, and `rhokp`;
- agentic-only: `lightspeed-agentic-operator`,
  `lightspeed-agentic-console-plugin`, `lightspeed-agentic-alerts-adapter`, and
  `lightspeed-agentic-sandbox`.

The update pipeline must preserve the v1 bundle record and update only the v2
`lightspeed-operator-bundle-v2` record.

## Required generation sequence

The release automation must execute this sequence in a clean checkout:

```bash
make sync-agentic-crds
make bundle BUNDLE_VARIANT=v2 BUNDLE_TAG=2.0.0
```

The first command synchronizes the versioned agentic CRD/RBAC contract. The
second command generates the v2 CSV with classic and agentic controller
content. The generated bundle must then pass `operator-sdk bundle validate`.

## Required checked-in automation

The implementation adds dedicated v2 PipelineRun and release-pipeline files:

```text
.tekton/ols-bundle-v2-pull-request.yaml
.tekton/ols-bundle-v2-push.yaml
.tekton/release/bundle-update-v2/pipeline.yaml
.tekton/release/bundle-update-v2/pipelinerun.yaml
```

The push and pull-request PipelineRuns use the `ols-bundle-v2` application and
component labels and publish only the v2 bundle image. The release pipeline
creates a bundle-update pull request after refreshing all input digests and
regenerating the v2 bundle.

## Catalog handoff

The v2 bundle Snapshot is the only bundle input to `ols-fbc-v5-0`. The OCP 5.0
catalog update runs `hack/bundle_to_catalog.sh -t v2` and keeps
`olm.skipRange: ">=1.0.0 <2.0.0"` on the v2 channel head. OCP 4.x catalogs
continue to consume only the v1 bundle.

## External Konflux configuration

Konflux must provide `ols-bundle-v2` and `ols-fbc-v5-0` Applications and their
Snapshots. The corresponding bundle and FBC ReleasePlans are managed in
`konflux-release-data`.
