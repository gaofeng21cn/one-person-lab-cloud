# Control Plane Reconciler Typed Boundary

## Decision

The fifth bounded-context cut types the three state axes owned by the Control
Plane Workspace Launch Reconciler:

```text
Launch Stage cursor
  + Launch operation status
  + current Stage observation state
  -> strict schema-v3 decode
  -> one Reconciler reducer
  -> CAS persistence and optional Workspace Receipt projection
```

This is an internal type-safety change. It preserves the existing schema-v3
JSON fields, HTTP payloads, database rows, request hashes, idempotency keys, and
external side effects.

## Owner

`WorkspaceLaunchReconciler` owns:

- the fixed business cursor from Key through Receipt and Succeeded;
- the Launch operation status;
- Stage observation reduction;
- attempt reservation, continuation reads, replay leases, and CAS;
- Resume and system continuation authorization consumption;
- final Workspace Receipt projection.

The persistence Store owns atomic row writes, not state-machine decisions.
Fabric, Ledger, and Sub2API remain physical fact or mutation owners behind
typed clients. Console remains a Control Plane API consumer.

## Typed Cut

The Reconciler uses the current shared types from `packages/contracts/go`:

- `contracts.Stage` for `operation.Stage` and the cursor order;
- `contracts.LaunchStatus` for `operation.Status`;
- `contracts.StageState` for `observation.State`.

`decodeWorkspaceLaunchReconcileOperation` remains the single ingress from the
generic persistence row. `workspaceLaunchReconcileOperationRow` remains the
single egress. Both retain runtime allow-list and cross-field validation;
assigning a named string type does not replace validation of untrusted bytes.

## Invariants

- The cursor advances only through `contracts.AllLaunchStages()`.
- `StageSucceeded` requires `StatusSucceeded` and no other stage may carry that
  terminal status.
- The existing failed disposable-reset state remains the only admitted failed
  schema-v3 combination.
- `StageStateOwnershipPending` is valid only for Compute.
- `StageStateRuntimeImageRevisionPending` is valid only for Runtime.
- A non-ready observation carries no canonical facts.
- A ready observation is reduced only through the current stage's fact schema.
- Mutation still requires a persisted reservation, and progress still requires
  owner-authoritative readback followed by CAS.
- Pending/replay budgets and `unknown/manual_review` behavior do not change.

## Preserved String Boundaries

The following stay strings because they are serialization or separate state
machines rather than the three Reconciler axes:

- PostgreSQL/Ent generic row fields and operation queries;
- schema-v3 raw JSON and heterogeneous canonical Stage facts;
- HTTP DTO fields, provider reason/error codes, logs, hashes, and keys;
- attempt, replay claim, continuation authorization, and read-claim statuses.

Conversions occur only where these boundaries enter or leave the typed
Reconciler. This cut does not create a speculative union of all Stage facts.

## Acceptance

- Strict decode rejects unknown Stage, Launch status, or observation state.
- Schema-v3 encode/decode and public JSON behavior remain compatible.
- The normal Key-to-Receipt path ends at typed Succeeded/Succeeded.
- Existing pending, unknown, recovery, replay, CAS, and restart matrices pass.
- No Fabric, Ledger, database schema, or Console behavior changes.

## Non-goals

- No new service, workflow engine, event bus, schema version, or endpoint.
- No broad typing of every Control Plane table or every canonical fact.
- No redesign of Acceptance B, repair, disposable reset, or child claim state.
- No Console Controller work in this pull request.
