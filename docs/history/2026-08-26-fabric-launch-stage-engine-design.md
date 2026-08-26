# Fabric Launch Stage Engine Design

## Objective

Give Fabric's provider-neutral Launch Stage lifecycle one internal owner without
changing the public HTTP contract, persisted operation schema, provider adapter
behavior, or Control Plane Workspace Launch authority.

The change extracts the shared Stage operation state machine from `Service` into
one narrowly constructed `launchStageEngine`. Storage is the first complete
acceptance slice because it exercises prior-stage authority, provider mutation,
authoritative readback, response-loss recovery, restart recovery, and CAS without
the special Compute ownership, Secret credential, or Runtime revision rules.

## Owner Boundary

Control Plane continues to own:

- the durable Workspace Launch business cursor;
- account policy, Key and debit coordination;
- attempt, lease, read budget, Resume authorization, and `manual_review`;
- activation, Receipt creation, and customer projection.

Fabric Launch Stage Engine owns:

- the immutable provider-neutral Stage binding;
- the parent Fabric Stage operation and its CAS transitions;
- prior-stage authority validation and monotonic resource identity;
- provider mutation scope, authoritative readback, and Stage observation;
- persisted provider state, diagnostic, and Runtime image revision proof.

Provider adapters continue to own Local-Docker or Tencent/TKE resource writes
and readback. The Engine treats provider state as opaque bytes.

## State Model

The Engine keeps two state layers separate.

| Layer | States |
| --- | --- |
| Persisted Fabric operation | absent, `started`, `failed`, `succeeded` |
| Owner observation returned to Control Plane | `absent`, `pending`, `unknown`, `ready` |

Allowed persisted transitions:

- absent to `started`: claim and persist the immutable binding before mutation;
- `started` to `started`: typed pending or diagnostic-only convergence;
- `started` to `failed`: non-pending provider error or invalid provider result;
- `started` or `failed` to `succeeded`: exact provider result or authoritative readback;
- `failed` to a new `started` claim: only after readback proves same-key continuation is allowed;
- `succeeded` to `succeeded`: exact resource readback, diagnostic convergence, or the existing Compute ownership correction.

Control Plane business states are not part of this state machine.

## Invariants

- Stage/action is one of Compute, Storage, Attachment, Secret, or Runtime.
- Launch, Account, Workspace, Provider, preflight binding, spec digest, operation
  ID, idempotency key, and request hash match the persisted authority exactly.
- Provider mutation cannot start before the parent Stage binding is durable.
- Prior Stages are `succeeded`, belong to the same Launch identity, and preserve
  all already established resource identities.
- Provider results preserve input resources and bind the current Stage resource
  to the current Fabric operation ID.
- Provider readback runs in a read-only mutation scope.
- Raw Gateway credentials are never persisted.
- Runtime image revision is restricted to the original Runtime operation and
  its exact authorization proof.
- A succeeded Stage cannot silently accept resource identity drift.

## Construction

`launchStageEngine` receives only:

```go
WorkspaceLaunchStageStore
workspaceLaunchProvider
providerDescriptorReader
workspaceImagePolicy
workspaceLaunchRuntimeImageRevisionProvider
ProviderMutationStore
MachineOwnershipStore
func() time.Time
```

`Service` retains a separate `WorkspaceLaunchPreflightStore` for admission
creation and readback. It no longer holds the Stage operation write store.

The Engine does not receive the complete `Service`, complete `Provider`, or
`optionalProviderPorts`. Plan resolution, readiness, monthly preflight, caches,
Jobs, Runtime stores, HTTP authorization, billing, Ledger, and Control Plane
Reconciler state remain outside the Engine.

## Migration

The five Stages share one parent operation lifecycle, so the shared Ensure/Read
state machine moves once. `Service.EnsureWorkspaceLaunchStage`,
`Service.ReadWorkspaceLaunchStage`, and `Service.WorkspaceLaunchProviderRequest`
remain as thin compatibility-free facades over the Engine.

Routing only Storage to a new implementation while retaining a second Service
state machine is rejected because it would create two writers for the same
invariants. Per-Stage engines are also rejected because they would duplicate
Claim, CAS, binding, and readback policy.

Storage is the first focused end-to-end verification slice. Provider adapter
switches are not redesigned in this change.

## Minimal Scope

This change does not add a service, process, workflow framework, event bus,
database table, schema migration, public DTO, provider registry, Stage plugin
system, or Control Plane change. It does not rewrite provider adapters or alter
any persisted payload.

## Acceptance

- `Service` cannot write a Workspace Launch Stage operation directly.
- all Stage Claim, Save, CAS, readback classification, and failure persistence
  live on `launchStageEngine`;
- the existing Fabric HTTP contract and Control Plane callers are unchanged;
- focused generic Stage tests pass unchanged;
- Tencent Storage response-loss, restart, corrupt-child-state, and replay tests
  pass unchanged;
- Local-Docker's complete Workspace path passes;
- `npm run verify:local` and `npm run verify:local:full` pass;
- `docs/status.md` records the verified boundary while `docs/roadmap.md` keeps
  `PB-F-TYPED-OPERATIONS-01` active.
