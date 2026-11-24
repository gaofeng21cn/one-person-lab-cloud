# Workspace Image Lifecycle

This reference owns the remaining Control Plane image-policy read, historical
replacement decoding and Fabric image-replacement/node-cache behavior. See
[status](../status.md#current-capability-baseline), [runtime access](workspace-runtime-access.md)
and [launch recovery](workspace-launch-recovery.md).

## Retained Control Plane Reads & Retired Paths

- **Retired**: Control Plane runtime image-replacement creation, preview, dispatch,
  retry worker (`OPL_WORKSPACE_RUNTIME_IMAGE_REPLACEMENT_WORKER_ENABLED`), admin
  policy activations (`POST /api/operator/workspace-image-release-activations`),
  replacement-history GETs and their Console/BFF UI controls are removed.
- **Active Operator Read**: `GET /api/operator/workspace-runtime-image-policy`
  serves the schema-1 catalog (`OPL_WORKSPACE_IMAGE_RELEASES_JSON`) and retained
  active policy for Instance registry-retention and node-cache readers.
- **Preserved History Decoders**: `workspace_runtime_image_history.go` and
  `workspace_image_release_policy.go` retain strict decoding of historical
  `started` image operations so resource reconciliation and original Launch
  recovery continue to respect frozen replacement digests without admitting new
  CP image changes.

## Fabric Replacement Capability

Fabric retains its typed replacement API, operation CAS (`resourceVersion` on TKE
`workspace` Deployment containers), provider-mutation journal, locks and owner
readback (`ObservedGeneration` + Ready Pods matching the target digest). Image
replacement reuses existing Compute/CBS/Attachment/Secret/URL identities and reads
back uncertain RPC responses from the original journal before any new action.

## Node Cache Retirement (`opl-node-image-retire`)

The portable Cloud image includes `opl-node-image-retire` for bounded containerd
CRI digest cleanup run by Instance short-lived maintenance Pods (mounting only the
containerd socket): it inspects all active/exited containers, protects pinned
and Instance-protected aliases, executes single-digest `RemoveImage` (never global
prune) and verifies image absence via readback before recording receipts.
