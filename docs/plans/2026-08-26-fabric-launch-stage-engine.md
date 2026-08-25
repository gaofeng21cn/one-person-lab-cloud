# Fabric Launch Stage Engine Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Make one narrowly constructed Fabric Launch Stage Engine the only owner of Stage operation transitions while preserving every current caller, provider behavior, and persisted fact.

**Architecture:** Keep Workspace Launch admission on `Service`, split its minimal persistence port from Stage mutation persistence, and move the shared Ensure/Read state machine to an internal `launchStageEngine`. `Service` remains the public application facade. Storage is the first focused end-to-end acceptance slice, while all five Stages continue to use the same single state machine.

**Tech Stack:** Go 1.24, existing Fabric operation stores, typed HTTP contracts, Local-Docker and Tencent/TKE adapters, Node verification runner.

---

### Task 1: Separate admission persistence from Stage mutation persistence

**Files:**
- Modify: `services/fabric/internal/fabric/operation_capability_ports.go`
- Modify: `services/fabric/internal/fabric/service.go`
- Modify: `services/fabric/internal/fabric/workspace_launch_stage.go`

**Step 1: Record the existing characterization baseline**

Run:

```bash
cd services/fabric
go test ./internal/fabric -run 'WorkspaceLaunchPreflight|WorkspaceLaunchStageReadback|WorkspaceLaunchStageRejectsEveryPreflightIdentityDrift' -count=1
```

Expected: PASS.

**Step 2: Add the narrow preflight store contract**

Introduce one shared point reader and two capability ports:

```go
type workspaceLaunchOperationReader interface {
    Get(context.Context, string) (FabricOperation, error)
}

type WorkspaceLaunchPreflightStore interface {
    workspaceLaunchOperationReader
    ClaimStageOperation(context.Context, FabricOperation) (FabricOperation, bool, error)
}

type WorkspaceLaunchStageStore interface {
    workspaceLaunchOperationReader
    OperationByActionIdempotency(context.Context, string, string) (FabricOperation, bool, error)
    ClaimStageOperation(context.Context, FabricOperation) (FabricOperation, bool, error)
    SaveStageOutcome(context.Context, FabricOperation) error
    ConvergeStageReadback(context.Context, FabricOperation, FabricOperation) error
    ConvergeStageDiagnostic(context.Context, FabricOperation, FabricOperation) error
}
```

Keep `operationStoreCapabilityPorts` as the adapter for both contracts.

**Step 3: Restrict Service to the preflight capability**

Replace the current `workspaceLaunchStages` field with
`workspaceLaunchPreflights WorkspaceLaunchPreflightStore`. Update only
Preflight persistence and readback callers in this task.

The Stage behavior will temporarily receive its Store through the Engine added
in Task 3; do not add a second Service Stage field.

**Step 4: Run focused compile and characterization tests**

Run:

```bash
cd services/fabric
go test ./internal/fabric -run 'WorkspaceLaunchPreflight|WorkspaceLaunchStageReadback|WorkspaceLaunchStageRejectsEveryPreflightIdentityDrift' -count=1
```

Expected: PASS after the Engine wiring in Task 3; before that, `go test -run '^$' ./internal/fabric` may be used to expose remaining compile references.

### Task 2: Make Provider operation scope constructible without Service

**Files:**
- Modify: `services/fabric/internal/fabric/provider_mutation.go`

**Step 1: Extract one dependency-explicit context helper**

Create a helper whose arguments are the actual journal dependencies:

```go
func withProviderOperationJournal(
    ctx context.Context,
    operation FabricOperation,
    readOnly bool,
    operations ProviderMutationStore,
    machineOwnership MachineOwnershipStore,
    provider string,
    now func() time.Time,
) context.Context
```

It must preserve the existing launch/direct-storage binding selection and return
the original context when neither binding is valid.

**Step 2: Keep Service wrappers behavior-identical**

`providerMutationContext`, `providerReadContext`, and
`providerOperationContext` delegate to the helper with existing Service fields.
No provider mutation behavior changes.

**Step 3: Verify mutation and read-only behavior**

Run:

```bash
cd services/fabric
go test ./internal/fabric -run 'LaunchStageBinding|ProviderMutation|WorkspaceLaunchStageReadContextRejectsProviderMutation' -count=1
```

Expected: PASS.

### Task 3: Extract the shared Launch Stage state machine

**Files:**
- Create: `services/fabric/internal/fabric/workspace_launch_stage_engine.go`
- Modify: `services/fabric/internal/fabric/workspace_launch_stage.go`
- Modify: `services/fabric/internal/fabric/service.go`

**Step 1: Define the minimal Engine**

Create:

```go
type launchStageEngine struct {
    stages               WorkspaceLaunchStageStore
    provider             workspaceLaunchProvider
    providerDescriptor   providerDescriptorReader
    imagePolicy          workspaceImagePolicy
    runtimeImageRevision workspaceLaunchRuntimeImageRevisionProvider
    providerMutations    ProviderMutationStore
    machineOwnership     MachineOwnershipStore
    now                  func() time.Time
}
```

The constructor accepts those fields directly. It does not accept `Service`,
`Provider`, or `optionalProviderPorts`.

**Step 2: Move stateful Stage methods to Engine receivers**

Move these responsibilities from `Service` to `launchStageEngine`:

- persisted Preflight admission lookup used by Stage execution;
- Stage input and expected-binding validation;
- frozen Provider request construction and prior-stage validation;
- diagnostic persistence;
- success/failure persistence;
- Ensure claim/replay/mutation flow;
- Read and authoritative readback classification.

Keep pure DTO, hashing, encoding, digest, and result-classification functions as
package functions. Do not change JSON tags, payload keys, status strings, reason
strings, or error identities.

**Step 3: Use dependency-explicit Provider contexts**

Engine mutation and read paths call `withProviderOperationJournal` directly with
their own dependencies. Read must continue to reject provider mutation.

**Step 4: Make Service a thin facade**

Construct one Engine in `NewServiceWithOperationStore`. The following Service
methods contain only delegation:

```go
func (s *Service) EnsureWorkspaceLaunchStage(ctx context.Context, input WorkspaceLaunchStageInput) (WorkspaceLaunchStageResult, error)
func (s *Service) ReadWorkspaceLaunchStage(ctx context.Context, input WorkspaceLaunchStageInput) (WorkspaceLaunchStageResult, error)
func (s *Service) WorkspaceLaunchProviderRequest(ctx context.Context, input WorkspaceLaunchStageInput, current workspaceLaunchStageRecord) (WorkspaceLaunchProviderRequest, error)
```

No Service field may expose `WorkspaceLaunchStageStore` after construction.

**Step 5: Verify the generic state machine**

Run:

```bash
cd services/fabric
go test ./internal/fabric -run 'WorkspaceLaunchStage|LaunchStageBinding|WorkspaceLaunchPreflight' -count=1
```

Expected: PASS with existing tests unchanged.

### Task 4: Prove Storage as the first complete acceptance slice

**Files:**
- No production file should be added solely for this task.
- Existing tests remain the authority unless an observed uncovered regression requires a typed contract test.

**Step 1: Run Tencent Storage recovery coverage**

Run:

```bash
cd services/fabric
go test ./internal/fabric -run 'TencentWorkspaceLaunchStorage|WorkspaceLaunchCompletesTypedFiveStageChain' -count=1
```

Expected: PASS, including exact CBS/static binding readback, response-loss
recovery, store/provider reopen, corrupt child state rejection, and replay.

**Step 2: Run provider-neutral and HTTP boundaries**

Run:

```bash
cd services/fabric
go test ./internal/fabric ./internal/http -run 'WorkspaceLaunch|LaunchStage' -count=1
```

Expected: PASS.

**Step 3: Run the complete Fabric package**

Run:

```bash
cd services/fabric
go test ./... -count=1
```

Expected: PASS.

### Task 5: Reconcile evidence and run full verification

**Files:**
- Modify: `docs/status.md`
- Modify: `docs/roadmap.md`
- Move after completion: `docs/plans/2026-08-26-fabric-launch-stage-engine-design.md` to `docs/history/2026-08-26-fabric-launch-stage-engine-design.md`
- Move after completion: `docs/plans/2026-08-26-fabric-launch-stage-engine.md` to `docs/history/2026-08-26-fabric-launch-stage-engine.md`

**Step 1: Update current evidence only**

Record that Fabric Stage operations have an independent Engine and that Service
retains only the Preflight admission capability. Keep
`PB-F-TYPED-OPERATIONS-01` active because Runtime reads, typed Control Plane
Reconciler state, and Console Controller work remain open.

**Step 2: Archive the completed planning records**

Move both plan files to `docs/history/**` so active documentation does not retain
a completed execution checklist.

**Step 3: Run formatting and focused verification**

Run:

```bash
gofmt -w services/fabric/internal/fabric/*.go
git diff --check
cd services/fabric && go test ./... -count=1
```

Expected: PASS.

**Step 4: Run repository gates**

Run:

```bash
npm run verify:local
npm run verify:local:full
```

Expected: PASS, including temporary PostgreSQL 16 and Local-Docker integration.

**Step 5: Structural acceptance searches**

Run:

```bash
rg -n 'workspaceLaunchStages' services/fabric/internal/fabric --glob '*.go'
rg -n 'func \(s \*Service\) (validateWorkspaceLaunchStageInput|validateWorkspaceLaunchExpectedBinding|persistWorkspaceLaunchStageDiagnostic|persistWorkspaceLaunchStageResult|failWorkspaceLaunchStage|readWorkspaceLaunchStage)' services/fabric/internal/fabric --glob '*.go'
```

Expected: no results. Public Service facade methods remain.
