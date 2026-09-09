# Workspace Image Lifecycle

This reference owns the source-level image catalog, activation and existing
Workspace replacement behavior. [Runtime access](workspace-runtime-access.md)
owns routing and credentials; [Launch recovery](workspace-launch-recovery.md)
owns image revision while a purchase is still non-terminal.

Instance injects a schema-1 `OPL_WORKSPACE_IMAGE_RELEASES_JSON` catalog whose
unique entries are immutable `repository@sha256` references and which must
contain the installation's `OPL_WORKSPACE_IMAGE`. Control Plane exposes that
catalog plus one persisted active release. An administrator changes the active
release through `POST /api/operator/workspace-image-release-activations` with
an expected revision, idempotency key, reason, and audit identity. A new Launch
copies the active image into its immutable launch descriptor; changing the
policy does not rewrite existing Launches or Runtimes.

For an already succeeded and running Workspace whose Runtime still exists,
including an unready Runtime that needs image recovery, Control Plane exposes a
separate administrator-only Runtime image replacement operation:
`POST /api/operator/workspaces/{workspaceId}/runtime-image-replacements` creates
an asynchronous operation and the matching `GET` route returns its persisted
status. The request fixes an immutable target from the approved catalog, independently
of subsequent active-release changes. Preview accepts the optional
`replacementImageDigest` query; omitting it selects the active release for the
existing Console caller. Tags and images outside the catalog are rejected. Control Plane resolves the successful Launch and live
Runtime using a Workspace-scoped successful-Launch query and persists the request
before dispatching a typed Fabric capability. Current paid entitlement, including
confirmed renewal and the current Key, is checked again before dispatch. Expired,
suspended and deleting Workspaces cannot be restarted by an image update.
Fabric independently requires the target to remain in its injected release
catalog, rechecks the full account/Workspace/Compute/CBS/Attachment/Runtime
owner chain, shares the persisted Runtime lock with power and deletion, uses its Runtime
operation CAS and provider-mutation journal, and
performs a provider-specific image-only mutation. Tencent/TKE patches only the
existing `workspace` container image on the existing Deployment using a
`resourceVersion` compare-and-swap. Ready readback requires the current observed
generation and Ready Pods reporting the target digest. A stale ready Pod cannot
complete an update. The currently supported replacement adapter is Tencent/TKE;
Local-Docker retains its existing Launch and power capabilities. No Compute, CBS, Attachment,
Secret, Launch, billing Receipt, Runtime service identity, or Workspace URL is
recreated or rewritten. Console presents activation and existing-Workspace
replacement as two separate commands. The Cloud source and portable image own
these APIs and capabilities; `opl-instance-medopl` still owns the protected
catalog values, production deployment authorization, TKE readback, rollback,
and receipts.

A failed pre-mutation read can resume the original operation. A lost mutation
response is resolved through owner readback and the same journal, without a
blind second write. A superseded operation cannot reapply its old target after
a later rollback. Started and terminal audit evidence use distinct identities.

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
