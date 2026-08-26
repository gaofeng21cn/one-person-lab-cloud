# Control Plane Reconciler Typed Boundary Implementation Plan

**Goal:** Type the Workspace Launch Reconciler's Stage, Launch status, and
observation state while preserving schema-v3 bytes and behavior.

**Architecture:** Reuse the shared Go contract enums, keep the existing strict
codec as the persistence anti-corruption layer, and migrate one complete
Reconcile flow without changing external contracts or child state machines.

**Tech Stack:** Go 1.22 contract module, Control Plane Go module, PostgreSQL
CAS integration tests, repository local verification.

## Task 1: Add the missing shared Stage state

Create `packages/contracts/go/stage_state.go` with the six states already
consumed by the Fabric/Control Plane Launch path. Run `go test ./...` in the
contract module and commit the isolated contract change.

## Task 2: Wire the Control Plane to the contract module

Update `services/control-plane/go.mod` with the repository-local
`opl-cloud/packages/contracts/go` dependency. Compile the Control Plane before
changing behavior to prove module wiring.

## Task 3: Type the Reconciler core

In `services/control-plane/internal/server/workspace_launch_reconciler.go`:

- replace the local Stage order with `contracts.AllLaunchStages()`;
- type `workspaceLaunchReconcileOperation.Stage` and `.Status`;
- type `workspaceLaunchStageObservation.State`;
- keep strict decode allow-lists and schema-v3 cross-field validation;
- use typed constants in `Reconcile`, `advance`, and the ready reducer;
- convert explicitly at map-key, log, persistence-row, and HTTP boundaries.

Compile after each axis so Stage, status, and observation failures stay local.

## Task 4: Switch direct consumers

Make only required conversions in the Stage adapters, worker/service facade,
admission/repair paths, diagnostics, and routes. Do not type unrelated child
claim statuses or canonical fact maps.

## Task 5: Add focused typed-boundary evidence

Use `packages/contracts/go` structs and enum values in new tests. Cover strict
enum decode, schema-v3 round trip, cross-field rejection, and one complete typed
normal flow. Reuse the existing recovery, replay, CAS, and PostgreSQL tests
rather than duplicating them.

Run:

```text
cd packages/contracts/go && go test ./...
cd services/control-plane && go test ./internal/server -run 'TestWorkspaceLaunch(Typed|Decoder|RecoveryAtEveryStage|CreateAndResume|FreshTypedPending|FabricReadyWithoutRequiredFacts)' -count=1
cd services/control-plane && go test ./... -count=1
npm run verify:local
npm run verify:local:full
```

## Task 6: Reconcile evidence and deliver

Update `docs/status.md` and `docs/roadmap.md` only after verification. Keep
Roadmap F active because Console Controller separation remains open. Archive
these completed plans under `docs/history/**`, run `git diff --check`, request
independent spec and quality review, then commit, push, and open the fifth-step
pull request.
