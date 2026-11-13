# Workspace Image Lifecycle

This reference owns the retained Control Plane image-policy and replacement
history reads, their safety consumers and Fabric's image capability. The source
withdrawal is based on `173d1afbcbd3dc120f0839707c9e69eb2ba2787b`;
[status](../status.md#retained-runtime-image-command-withdrawal-2026-11-04)
separates that source change from verification and installed adoption.
[Runtime access](workspace-runtime-access.md) owns routing and credentials;
[Launch recovery](workspace-launch-recovery.md) owns image revision while an
original purchase is still non-terminal.

## Retained CP reads and withdrawn commands

The source change removes CP Runtime image replacement creation, preview,
dispatch and retry/worker execution, and their Console controllers, API/DTOs
and BFF/internal Cloud adapters. Replacement creation/preview routes are removed. Worker
startup, `OPL_WORKSPACE_RUNTIME_IMAGE_REPLACEMENT_WORKER_ENABLED`, the exclusive
CP Fabric client/service wrapper leave together.
Console no longer reads image policy, preview or replacement results through
either its legacy API adapter or the BFF.

The operator-authenticated policy activation POST and its atomic revision/audit
store remain with the original Launch recovery consumer. A persisted active policy
can differ from a newer installed default: changing installation configuration alone
does not advance it. The existing activation command selects an approved catalog
release with expected-revision and idempotency checks before the original Launch
can authorize that image. This preserves the current owner and authorization rule;
it does not restore existing-Workspace replacement or a Console controller. Retire
this writer only after the original recovery selection authority migrates.

One direct, operator-authenticated CP read remains:

- `GET /api/operator/workspace-runtime-image-policy` serves the validated catalog
  and retained active policy. The Instance runtime-image client consumes it, and
  original Launch image-revision recovery still consumes the policy/catalog
  validation. The schema-1 `OPL_WORKSPACE_IMAGE_RELEASES_JSON` catalog and
  immutable image validation stay.

One operator-authenticated CP read is retired:

- `GET /api/operator/workspaces/{workspaceId}/runtime-image-replacements/{operationId}`
  read an existing persisted operation under its Workspace identity. It neither
  accepted nor dispatched a replacement, and no production client called it, so it
  is retired and no longer registered.

`workspace_runtime_image_history.go` retains the operation decoder and response
projection; invalid history decoding still fails the decoder. Pending-transition
detection recognizes valid `started` image operations, and resource reconciliation
still blocks on image-action rows marked `started`. Policy decoding remains in
`workspace_image_release_policy.go`. Original operations, policy records,
audits and schema remain unchanged. Failed historical records do not acquire a
new CP retry path. Original Launch revision recovery remains a separate live
caller and is not retired by removing existing-Workspace replacement.

Instance #675 has merged its rollout/rollback and policy-mutation caller
withdrawal, with 2/2 CI checks SUCCESS; [status](../status.md#retained-runtime-image-command-withdrawal-2026-11-04)
records its exact merge/head/run evidence. This removes configured workflow/tool callers at source; it is not a deployment
of the Cloud version that removes these entrypoints. Check in-flight legacy
executions before that Cloud adoption through the existing Instance process. Source verification and installed adoption are separate evidence layers.
The shared Instance image client and its retention/node-cache readers remain.
Source cleanup does not prove production adoption, zero traffic, archive absence
or the disposition of later operations or other installations. Open acceptance
belongs to [roadmap](../roadmap.md#retained-code-removal-review-2026-11-03).

## Fabric capability remains

Fabric retains its own replacement API, provider adapter, Runtime operation CAS,
provider-mutation journal, lock and authoritative readback. Its catalog admission
and account/Workspace/Compute/CBS/Attachment/Runtime ownership checks remain.
Tencent/TKE replaces only the existing `workspace` container image on the existing
Deployment with a `resourceVersion` compare-and-swap; completion requires current
observed generation and Ready Pods reporting the target digest. A stale Ready Pod
cannot establish completion. Local-Docker retains its existing Launch and power
capabilities. Removing the CP caller does not retire Fabric provider execution.

Fabric resolves an uncertain mutation response through the original journal and
owner readback. It does not recreate Compute, CBS, Attachment, Secret, Launch,
billing Receipt, Runtime service identity or Workspace URL for an image-only
replacement. Instance continues to own protected catalog values, deployment
adoption, rollback authorization and receipts.

## Node cache retirement

The portable Cloud image includes `opl-node-image-retire`, a bounded CRI client
used by Instance maintenance Pods. It removes one exact repository digest from
containerd through `RemoveImage`; it does not prune, stop containers, remove
snapshots, delete host directories or mutate volumes. The command reads all
containers, including exited ones, protects pinned images and aliases of all
Instance-supplied protected references, rechecks before mutation, and confirms
image absence afterwards. An RPC response loss is followed by readback, never
a second removal in the same execution. Invalid or unavailable inventory cannot
prove absence. CRI filesystem usage is recorded before and after; shared layers
and asynchronous content GC mean image absence does not prove bytes reclaimed.

Instance owns current reference collection, exact Workspace-node selection,
the short-lived Pod, authorization, cleanup and redacted per-node receipts.
The Pod mounts only the containerd socket, has no service-account token or
customer-data mounts, and uses the installed immutable Cloud image. Catalog and
retained rollback references must be retired before their cache is eligible.
TCR deletion is independent and is not required for node cache retirement.
The runtime can pull a retained Registry image again if a future authorized
operation references it; a retirement receipt records observed cache absence,
not a permanent ban on that digest.
