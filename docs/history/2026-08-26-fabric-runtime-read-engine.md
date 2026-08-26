# Runtime Resource Read Engine Implementation Plan

## Task 1: Split Runtime read capabilities

Add `runtimeResourceReader` and `WorkspaceRuntimeReadStore` ports. Adapt the
existing provider and operation-store composition without changing the public
`Provider` or persistence schema.

## Task 2: Extract the read engine

Create `workspace_runtime_read_engine.go` and move the provider status,
identity-candidate validation, password redaction, and observation classification
to Engine methods. Construct one Engine in `NewServiceWithOperationStore`.

## Task 3: Switch all Service Runtime status reads

Keep `WorkspaceRuntimeStatus` and `ObserveWorkspaceRuntime` as thin facades.
Route Runtime create response-loss, Runtime repair recovery, credentials owner
lookup, and Provider Facts Runtime reads through the Engine. No Service method
may call `runtimeProvider.WorkspaceRuntimeStatus` directly.

## Task 4: Verify the complete read slice

Run the focused Fabric Runtime identity/readback tests, the Fabric package, and
the repository local gates. Confirm no direct Service Runtime status provider
calls remain and that the third-step Stage Engine remains unchanged.

## Task 5: Reconcile evidence

Update `docs/status.md` and `docs/roadmap.md` to record the Runtime status read
Owner while keeping `PB-F-TYPED-OPERATIONS-01` active. Move the completed plan
files to `docs/history/**` only after verification.
