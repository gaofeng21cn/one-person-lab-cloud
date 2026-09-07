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
status. Its target is always the active release; the request cannot supply an
arbitrary image or tag. Control Plane resolves the successful Launch and live
Runtime and persists the request before dispatching a typed Fabric capability.
Fabric independently requires the target to remain in its injected release
catalog, rechecks the full account/Workspace/Compute/CBS/Attachment/Runtime
owner chain, uses its Runtime operation CAS and provider-mutation journal, and
performs a provider-specific image-only mutation. Tencent/TKE patches only the
existing `workspace` container image on the existing Deployment, then reads the
Runtime back through the normal status path. No Compute, CBS, Attachment,
Secret, Launch, billing Receipt, Runtime service identity, or Workspace URL is
recreated or rewritten. Console presents activation and existing-Workspace
replacement as two separate commands. The Cloud source and portable image own
these APIs and capabilities; `opl-instance-medopl` still owns the protected
catalog values, production deployment authorization, TKE readback, rollback,
and receipts.
