# Workspace Launch Recovery Reference

This reference owns current Launch binding and recovery behavior.
[The reconciler](../../services/control-plane/internal/server/workspace_launch_reconciler.go)
and [its focused tests](../../services/control-plane/internal/server/workspace_launch_reconciler_test.go)
are the current source; production execution remains Instance-owned.

## Stage Binding

Fabric exposes `/fabric/workspace-launches/preflight`, the persisted binding
readback `/fabric/workspace-launches/preflight/read`,
`/fabric/workspace-launches/stages/read`, and
`/fabric/workspace-launches/stages/ensure`. The stage DTO contains only the
provider-neutral binding and resource refs used by the real Control Plane caller.
Fabric persists the parent binding and a deterministic child record before each
actual provider write, then reads both by exact operation identity; typed
readback never scans operation listings or reconstructs Launch ownership from
suffixes or provider tags. Both owners consume the same focused golden vectors,
and the normal Launch/Resume caller is integrated. Full product qualification is tracked in [the roadmap](../roadmap.md).

The preflight readback accepts only one opaque binding reference, reuses the
strict persisted-preflight decoder, and returns the verified launch identity
and `specDigest` without the canonical provider plan. It is service-authenticated
but has no mutation capability and does not call a provider. Control Plane uses
it only for the narrow operator preview that repairs a schema-3 operation whose
sole missing canonical fact is `specDigest`.

## Authorization And Owner Readback

The current recovery row keeps `Max=1` and does not reset `Attempted`. An
operator may CAS-persist one exact-idempotency replay budget plus a finite typed
continuation-read budget; the server binds the starting readback count. Fabric
reports only `ready/none`, `pending/provider_provisioning`, compute-only
`pending/ownership_pending`, the three explicit absent reasons, or the two
explicit unknown reasons. Adapters perform owner read, child replay CAS, owner
read again, then reuse the exact original key only for an admitted absence or
compute ownership continuation. Budget exhaustion records
`unknown/manual_review`. Schema-v3 rows missing the new fields decode with zero
authorization and cannot read or mutate until explicitly reviewed.

## Storage Recovery

A resource-billed Storage attempt already parked as `unknown/manual_review`
continues through the same Launch Reconciler. For the first replay, the worker
may create the deterministic system authorization after a fresh Tencent/TKE
`absent` read; the existing operator Resume route retains the same bounded
capability. The authorization is bound to the original `Max=1` attempt,
idempotency key, and Fabric operation with zero mutation budget, one replay
budget, and a finite typed read budget. Fabric reads before any Ensure call:
`ready` advances read-only, `pending` consumes only bounded reads, and `absent`
permits one same-key replay. Unknown, conflict, read failure, or identity drift
does not persist the authorization or issue another CBS mutation.

After that same-key replay has a durable terminal `failed` claim and its
administrator authorization is consumed, the existing Resume route accepts
only a new zero-mutation, one-replay, three-read authorization bound to the
same Storage attempt and identity. The Reconciler first performs one typed
Fabric read: exact `ready` confirms the original attempt without Ensure;
authoritative `absent` replaces only the failed claim and permits one Ensure
with the original idempotency key. `pending`, `unknown`, read failure, or
identity drift returns conflict without persistence or Ensure. This is a
continuation of the original Launch, not another workflow or operation. Durable
authorization history remains audit evidence rather than a terminal retry cap.
After another inconclusive replay, any continuation still requires a new
single-use administrator authorization bound to the immediately preceding
consumed authorization and its exact durable failed claim.

## Runtime Recovery

A resource-billed Runtime already parked as `unknown/manual_review` has one
narrow operator recovery path in the same Reconciler. It accepts only the
original `Max=1` attempt and exact idempotency identity. A zero-mutation,
zero-replay authorization advances only on exact `ready` facts. For an initial
Runtime apply that left no provider resource and consumed no continuation or
replay, a zero-mutation, one-replay, three-read authorization first requires
authoritative `absent`, resets only that attempt to `reserved`, and calls the
normal Reconciler with the original key. Every other observation or read error
returns a conflict without persisting authorization or calling Fabric ensure.
If an exhausted Runtime read budget was owned by a failed fresh typed-pending
continuation, the READY transition also marks that continuation consumed so its
persisted state remains coherent.

## Worker Recovery

The Workspace Launch worker also presents `manual_review` rows to a distinct
Reconciler auto-recovery entry point. For `providerProfileRef=tencent-tke`, a
fresh `ready` read on the exact original `unknown`, `Max=1` Compute, Storage,
Attachment, Secret, or Runtime attempt generates a deterministic
`control-plane-system` `0/0/3` authorization that atomically confirms the
stage. Compute additionally accepts a fresh `ownership_pending` read and
generates a distinct deterministic zero-mutation, one-replay authorization.
Storage additionally accepts a fresh authoritative `absent` read, when no
earlier replay or active authorization exists, and generates its distinct
deterministic zero-mutation, one-replay authorization.
That continuation preserves the original Fabric operation and idempotency key,
so Tencent discovers the Ready Machine before claiming CVM/Node ownership and
cannot scale the NodePool again. A failed typed Compute continuation is
replaceable only when it has no replay claim and no earlier replay
authorization; its terminal read claims are removed before the new
authorization is persisted. Provider provisioning, unknown, conflict, read
failure, active Resume, an ineligible absence, an earlier stage replay, Runtime
repair, or an active fresh continuation leaves the row unchanged. The ready
path does not inherit replay or image revision authority, and neither replay
path can authorize another business attempt.

The capability-protected, read-only stage observation returns schema v3 with
the same auto-recovery eligibility decision and one safe block-reason
enum. It performs no persistence or provider/Kubernetes mutation and exposes no
operation, provider, or customer identity.

## Runtime Image Revision

For the Tencent/TKE adapter, the same operator Resume request may also carry a
replacement Workspace image digest when the exact original Runtime exists and
its only drift is the old admitted image. Control Plane accepts the field only
for `manual_review/runtime`, `providerProfileRef=tencent-tke`, the exact Launch
version, `0/1/3` mutation/replay/read budgets, and a digest equal to the
active Workspace image release. Before classification it fixes the
authorization's starting read count, then projects a schema-1 proof containing
the authorization digest, original Launch/Workspace/Runtime identities, old
image, and replacement image. The original `workspaceImageDigest`, Fabric
binding, request hash, and `${operationId}:runtime` key remain unchanged.

Fabric validates that proof only on the Runtime stage and only for an opted-in
Tencent adapter. Exact old-image drift becomes
`pending/runtime_image_revision_required`; any other image or identity drift is
rejected. Ensure claims one replay epoch on the already-succeeded Tencent
Runtime child mutation, records the authorization and image digests there,
applies the replacement manifest once, and treats an unready replacement as
read-only provider convergence. Every later read validates the same child proof
before readiness. READY updates the original Runtime stage record and returns
the same Launch to Activation; it does not create a repair operation or another
Launch path. The persisted decoder accepts the production-shaped failed fresh
continuation while the Runtime read ceiling expands from three to six.

If an image-revision authorization is consumed with a durable failed claim, the
same Resume route may continue only from the exact completed readback window.
An undispatched revision retains the same replacement digest. When Tencent
authoritative readback instead proves that the prior replacement is now the
exact retained image but the Runtime is still unready, a later administrator
authorization may supersede it with the currently deployed qualified digest.
Control Plane derives the new proof's previous image from the immediately
preceding consumed authorization; Fabric atomically advances the existing
Runtime child journal from that exact retained image before applying the new
manifest. Any different Runtime identity, image chain, authorization lineage,
partial readback window, or active lease remains fail-closed. The Launch,
Runtime operation, stage request hash, and original idempotency key do not
change.

## Local-Docker Fulfillment Repair

When an authoritative read proves a local-Docker Runtime is genuinely unready,
the current local-Docker implementation exposes a distinct operator
Fulfillment Repair command. Control Plane admits only the exact paid Launch
whose Key, Debit, Compute, Storage, Attachment, and Secret are confirmed and
whose Activation and Receipt have not started. The operator provides only the
new immutable image digest, reason, Launch version, and idempotency identity;
Control Plane binds the authenticated operator user and server timestamp into
the persisted repair authorization before mutation, and exact replay preserves
those facts. All resource identities come from the persisted Launch. Fabric checks the
persisted original Runtime evidence before mutation, serializes repair per
Workspace, retains the Secret and all non-Runtime resources, replaces the
container through the local-Docker adapter, and persists the replacement as the
canonical Runtime readback. Unsupported adapters fail closed. READY then
continues the existing Activation and Purchase Receipt stages.

## Post-Mutation Continuation

Fresh post-mutation typed `pending` uses a distinct system continuation record,
not the operator Resume record. The mutation's mandatory owner read persists
`PendingReadbacks=1`, a zero-mutation authorization, and an exact
account/Launch/Workspace/stage/idempotency/attempt/version binding in one CAS.
Before each GET, Control Plane claims and increments the exact ordinal by CAS; a
loser stops before GET, and a crashed claim is never refunded or reissued.
Non-compute stages keep zero replay and at most two subsequent reads.

`ensure_compute_allocation` instead persists a ten-minute deadline, at most
sixty subsequent worker reads, and one same-key replay budget. Fabric maps a
Tencent provisioning response to `pending/provider_provisioning`; those reads
cannot mutate. When the existing Machine is Ready but exact ownership is
recoverable, Fabric returns `pending/ownership_pending`. Control Plane durably
claims the replay, reads again, and calls the same Fabric Ensure with the
original key. Tencent Ensure discovers the persisted Machine before CVM/Node
claim and therefore does not call `ScaleNodePool` again. Ready consumes the
authorization and advances; unknown/conflict/error or exact budget/deadline
exhaustion records `unknown/manual_review`.

For an already parked historical compute operation, the worker-owned path
accepts `ready` read-only or `ownership_pending` through the same-key
continuation and persists its deterministic authorization first. A failed
fresh continuation with no replay evidence can be replaced by that worker
path; the existing operator Resume route remains available for
`provider_provisioning` and one terminal failed replay replacement. Both paths
preserve `Attempted=1`, `Max=1`, the original binding, and the original
idempotency key. Absent, unknown, conflict, and read failure do not change the
operation. This does not add a generic compute-unknown mutation route. A
schema-v3 row without the required authorization and claim maps remains
explicitly zero-budget.

Fabric's child transport claim is a local replay epoch, not Control Plane
operator authorization and not a second business attempt budget. It binds the
parent operation, exact child identity, original idempotency key, and lease
generation only to serialize dispatch and crash recovery inside Fabric.

## Capability Boundary

Control Plane uses its own Fabric transport identity for these mutations and
signs a short-lived capability binding account, Workspace, resource kind/id,
action, operation identity, expiry, and request-body digest. Fabric derives the
expected scope from the typed request and rejects missing or mismatched
capabilities before operation-store or provider mutation. Runner transport
identity remains limited to job lease routes.

Ordinary Fabric Runtime status is a non-secret read and always redacts the
provider password. Credential reveal is a separate POST issued only after the
Control Plane verifies the Workspace owner; Fabric requires the same short-lived
request-bound capability and independently matches account and Workspace to the
persisted Runtime operation before returning the password. The former compute,
volume, and snapshot sync HTTP routes had no product caller and are absent;
Fabric's internal reconciliation methods remain owned by the service and are not
transport-token-only public writes.

The targeted compute-pool-head terminalization route is the only current
operator exception. Its protected Instance workflow must sign `caller=operator`
for the exact request body, while Fabric independently derives account,
Workspace, node-pool, approval, and replay scope from its persisted candidate or
exact terminal evidence before accepting the capability. The product source and
tests define this protocol; Instance credential wiring, deployment, and runtime
readback remain owned by `opl-instance-medopl` and are not implied by source
absorption.


## Strict Decode And Bounded Canonical-Fact Repair

`contracts.Stage`, `LaunchStatus` and `StageState` type the business cursor,
operation status and current observation. The single persisted ingress and
egress retain allowlisted values and cross-field validation. Succeeded cursor
and status must agree; ownership-pending is Compute-only and image-revision
pending is Runtime-only. Non-ready observations carry no canonical facts.
Named string types do not validate untrusted serialized bytes by themselves.

The operator canonical-fact repair is paid by a specific persisted-row defect:
schema 3, `debit/manual_review`, no forbidden legacy field, and exactly one
missing canonical fact, `specDigest`. Fabric reads the persisted preflight
binding rather than resolving the current plan or invoking a provider. Control
Plane matches Launch/account/Workspace/package/size/image/request/provider
identities, adds only `specDigest` and the next version, and requires strict
decode plus an exact two-field semantic diff.

The preview digest binds prior bytes, version, Fabric evidence and proposed
change. Apply regenerates the preview, requires the exact version/digest,
reason and idempotency key, then commits row CAS and deterministic operator
audit atomically. Drift, identity mismatch or audit conflict has no partial
write; exact replay returns the same repaired result and audit. This operation
does not clear manual review, resume Launch, change billing, create resources or
relax the decoder. The current route and implementation are in
[`workspace_launch_canonical_fact_repair.go`](../../services/control-plane/internal/server/workspace_launch_canonical_fact_repair.go)
and [`routes_admin.go`](../../services/control-plane/internal/server/routes_admin.go).

## Redacted Diagnostics

Operator health publishes `workspaceLaunchOperationDiagnostics` separately from
`controlledBasicPilot`. It classifies errors from the real strict decoder,
retains the admission failure, and performs no mutation. Summaries are bounded
and contain identity digests, valid timestamps, recognized scalar metadata,
counts/enums and missing/forbidden key names. They do not expose raw identities,
canonical values, credentials, provider payloads or arbitrary error strings.
Diagnostics do not authorize migration or reset. The current decoder and
[`workspace_launch_admission.go`](../../services/control-plane/internal/server/workspace_launch_admission.go)
own the exact categories and projection.
