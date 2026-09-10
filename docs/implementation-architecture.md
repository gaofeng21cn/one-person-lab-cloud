# OPL Cloud Implementation Architecture

This repository implements the OPL Cloud product layer. Browser Console calls
Cloud Control Plane APIs; App Shells consume Cloud-facing projections through
App/Framework contracts. Cloud service and database authority stays in the
owning Cloud service.

## Request Path

```text
Browser Console
  -> Control Plane product API
       -> Sub2API management API: live balance, account Key/usage, once-dispatched debit/refund
       -> Fabric API: typed compute, storage, attachment, Secret, and runtime stages
       -> Ledger API: receipts and reconciliation evidence
```

Sub2API is external and remains the only spendable-balance, API-key, routing,
and request-usage owner. The repository reads those records on demand and does
not mirror them. Its code, image, database, configuration, and deployment remain
outside this repository's mutation boundary.

## Family Integration

Cloud has separate Console, Control Plane, Fabric and Ledger owners. Framework
and App integrations consume public contracts; their scoped Host composition is
defined by [the architecture boundary](architecture.md#host-client-and-cloud-authority-boundary).
Cloud API, persistence, provider, wallet, Workspace and Ledger authority never
moves into a desktop renderer or application Host.

## Core Path

The Core path is `Console -> Control Plane -> Workspace launcher/provider ->
local Docker`. Fabric owns resource mutation and readback, Sub2API owns balance,
Keys and usage, and Ledger owns receipts plus the append-only Evidence Index.
Installers select `OPL_FABRIC_PROVIDER` and immutable Workspace images
explicitly; missing provider or image inputs fail closed. Current capability is
recorded in [status](status.md); remaining outcomes are in the
[roadmap](roadmap.md).

## Physical Module And Dependency Map

The current repository is a modular product repository with three Go service
modules and one browser application. Repository co-location and one release image
do not authorize implementation imports between service owners.

```text
apps/console-ui
  -> same-origin /api/*
services/control-plane
  -> typed HTTP client -> services/fabric
  -> typed HTTP client -> services/ledger
  -> typed HTTP client -> external Sub2API

services/control-plane ─┐
services/fabric        ├─> services/internal/postgresmigrate
services/ledger       ─┘

packages/contracts/go -> shared runtime types consumed by Control Plane and Fabric
packages/contracts/*.json -> consumed artifact and cross-owner byte contracts
```

| Module | Physical boundary | Owns | Allowed dependencies | Forbidden coupling |
| --- | --- | --- | --- | --- |
| Console UI | `apps/console-ui`, TypeScript build | presentation and customer interaction | Control Plane product APIs under `/api/*` | direct Fabric, Ledger, Sub2API, Tencent, Kubernetes, persistence, or server implementation imports |
| Control Plane | independent `go.mod`, binary, Deployment and schema | session/account mapping, Workspace entitlement, Launch cursor/attempt/lease/CAS, account/settlement coordination and customer DTOs | typed HTTP clients for Fabric, Ledger and Sub2API; narrow PostgreSQL migration helper | resource-stage reducers, Fabric operation derivation, Fabric/Ledger implementation imports, provider fields/SDKs/Kubernetes, provider mutations, or downstream table writes |
| Fabric | independent `go.mod`, binary, Deployment and schema | compute, storage, attachment, Secret binding, Runtime, provider-neutral operation bindings/store, provider mutations and authoritative readback | provider adapters, cloud SDKs and narrow PostgreSQL migration helper | wallet, customer billing policy, Console session state, or Ledger table writes |
| Ledger | independent `go.mod`, binary, Deployment and schema | receipts, reconciliation, idempotency, retention, and caller-owned opaque provenance refs | narrow PostgreSQL migration helper | review-policy or review-gate semantics, Launch continuation authority, spendable balance mutation, provider SDKs, Fabric execution, or Control Plane table writes |
| PostgreSQL migration helper | independent narrow Go module under `services/internal/postgresmigrate` | advisory lock, migration journal and TLS validation mechanics | PostgreSQL driver only | any Console, Control Plane, Fabric or Ledger domain type |
| Shared contracts | JSON and the Go module under `packages/contracts` | artifact identities, cross-owner byte boundaries and shared runtime types | current runtime consumers, build and validation | service implementation, runtime state or a second status owner |

`tests/contracts/module-physical-boundaries.test.ts` enforces the source-level edges:
no Go service may import another service implementation, only Fabric may import
Tencent or Kubernetes SDKs, and Console network calls must remain inside its API
adapter and resolve to `/api/*`. This gate runs through the existing `npm test`
lane; it complements behavior and contract tests rather than replacing them.

Portable Compose gives each service its own PostgreSQL role/database and
inbound service token; Control Plane receives separate Fabric and Ledger
outbound tokens. Token rotation does not require restarting PostgreSQL or
unrelated services. The common image makes the services one product release
unit. Configuration and isolation tests do not prove concrete Instance adoption.

Deployment isolation is an independent implementation lane, not a predecessor
to Console, Control Plane, Fabric, or Ledger development. Portable distribution
assets and provider adapter code stay in this repository, while concrete
manifests, values and secret references stay in `opl-instance-medopl`. The two owners
join only when qualifying an exact deployment, rollback, and authoritative
readback. The common release image may remain shared unless measured release
blast radius creates a separate requirement.

## Repository And Instance Boundary

`one-person-lab-cloud` owns both product architecture and this reusable Console,
Control Plane, Fabric, and Ledger implementation. These are logical service
boundaries inside one repository, not authorization for separate current
implementation repos. `opl-cloud` is retained only as the short package, image,
binary, service, namespace, environment-variable and runner identifier.

`opl-instance-medopl` owns one concrete installation: domain names, provider
profile, region and resource ids, the enabled subset of Cloud-defined plans,
image pins, secret references, promotion policy, and deployment receipts. The
current fixed customer prices and `priceVersion` are implemented by the Cloud
Control Plane catalog; an Instance does not override them. Instance repositories
consume exact `one-person-lab-cloud` candidates for pre-publication qualification
and digest-addressed Releases after publication. Their internal artifacts may use the
`opl-cloud` identifier, but they never copy runtime code, product contracts, or
spendable-balance state.

The Instance boundary also owns medopl-specific production, acceptance,
recovery, canary, rollback, and approval/evidence tooling. Those sources and
focused tests are now canonical in `opl-instance-medopl` `main`; Cloud retains
product runtime code, provider-neutral contracts, reusable adapters, and
portable candidate/release assets. Instance workflows still checkout an exact
Cloud `product_sha`, but they execute instance tools from the run-scoped
Instance checkout. Cloud no longer provides an instance-specific production
command or an accepted caller for these paths.

## Console Source Truth

| Console area | Authority | Control Plane projection |
| --- | --- | --- |
| Signed-in identity | Sub2API identity plus local Session mapping | `/api/auth/me` |
| Public model endpoint | configured Sub2API origin projected as `/v1` | `/api/gateway/endpoint` |
| Wallet, owned Keys, per-Key Usage, account aggregate, balance history | live Sub2API JSON APIs | granular `/api/gateway/*` source DTOs |
| Workspace and renewal state | Control Plane Workspace row | `/api/workspaces` and launch/renewal DTOs |
| Runtime readiness | live Fabric/Kubernetes readback | `/api/workspaces/{workspaceId}/runtime-status` |
| `/data` and `/projects` release persistence | direct Runtime Pod SHA256 markers | rollout/rollback validation only; metadata/statfs product APIs are paused |
| Billing receipts | live Ledger readback | `/api/billing/receipts` |

Each source returns `source`, `status`, `available`, and `fetchedAt`. A successful
zero-row read is `empty`; dependency failure is `unavailable` and carries no
invented zero, empty collection, success state, or stale data. `sourceUpdatedAt`
is omitted unless the authority supplies it. Browser identity parameters never
override the current Session mapping, and raw downstream DTOs never cross the
Control Plane boundary.

Control Plane projects the configured public model endpoint. Console may
present that endpoint according to the current UX.
It never exposes or directly calls Sub2API management APIs;
`OPL_SUB2API_BASE_URL` remains server-only, and Cloud does not inject a second
Runtime Gateway base URL.

### Console Browser Lifecycle

[Console implementation](implementation/console.md) owns composition, query
freshness, command identity, Secret lifetime and capability-specific browser
completion. The current controller exports and API adapter are discoverable in
`apps/console-ui/src/app`; they are not maintained as a second migration list.

## Service Ownership

`apps/console-ui` owns presentation only. It has no persistence and never calls
Fabric, Ledger, Tencent, Kubernetes, or Sub2API directly.

`services/control-plane` owns local sessions, one-to-one Account-to-Sub2API
mappings, Account/User owner authorization, N Workspace entitlements per
Account, Workspace-level monthly operations, the Launch business cursor,
attempts/leases/CAS, settlement coordination, selected provider-profile refs,
and strict customer DTOs. It does not own a Fabric operation store,
resource-stage reducer, live Compute, Storage, Attachment, Secret, or Runtime
status, or provider mutation. Sub2API authenticates customer credentials.
Organization and Membership application/Ent models, runtime store APIs, and
provisioning writes are retired; their raw PostgreSQL tables remain only to
preserve historical rows and IDs for migration validation. They are not shared-
account or customer-authorization surfaces.

The login route admits JSON before credential processing. If a browser supplies
an `Origin` or `Referer`, Control Plane compares its scheme, host, and effective
port with `OPL_PUBLIC_URL` or the request web origin and rejects a mismatch or an
invalid configured origin before login or session material is written. The
same-origin Console JSON path remains valid, as do non-browser JSON callers that
supply neither header. This is a narrow pre-session request boundary, not a
general claim that every remaining browser or login hardening finding is closed.

`services/fabric` owns compute, storage, attachments, Secret binding, Workspace
runtimes, provider-neutral stage-operation bindings, its operation store,
provider mutations, and provider readback. The local Docker and Tencent/TKE
adapters each own their concrete writes and authoritative readback; Tencent
TKE/CVM/CBS and Kubernetes names do not enter the typed launch contract.
Provider callbacks may update resource facts but cannot overwrite Control Plane
entitlement state.

`services/ledger` owns receipts, reconciliation, idempotency, retention, and
caller-owned opaque provenance fields. Artifact, Review, ReviewPolicy, ReviewGate,
and Continuation APIs and their stores are retired. Historical `review_policies`
rows and Receipt provenance columns remain for data integrity; Ledger neither
interprets these refs nor generates continuation identities, hides them on reads,
or authorizes or advances a Workspace Launch. Control Plane's typed continuation
authorization is a separate owner-owned path.

`packages/contracts` contains narrow machine-enforced cross-module, interface,
security, integrity, permission, and irreversible-side-effect boundaries; it is
not a runtime service or a complete current-implementation specification.
Speculative route and object entries remain outside the active contracts.

## Fabric Internal Engines

`launchStageEngine` owns the immutable binding, prior-stage identity checks,
parent Claim/CAS, provider scope, readback and persisted stage outcome. The
service keeps preflight admission separate. Provider adapters own concrete
writes and opaque provider state. One shared stage engine avoids separate
Storage/Compute state machines duplicating journal, CAS and identity policy.
It receives narrow stores/provider capabilities, never the complete Service.

A stage persists its claim before mutation. Exact readback may confirm a
started or failed stage; continuation may replace a failed claim only under
its owner-approved same-key policy. A succeeded stage cannot silently change
resource identity. Readback uses a read-only provider scope; raw Gateway
credentials never enter the journal.

`workspaceRuntimeReadEngine` separately owns non-secret Runtime reads, persisted
identity-candidate matching, redaction and observation classification. A running
or unready Runtime requires exactly one valid persisted identity candidate.
Missing/ambiguous identities, provider errors and conflicts cannot become ready.
Its ports cannot claim or mutate operations. Runtime mutation, credential reveal,
Gateway Secret and deletion-residue observations retain their separate owners.

## Provider Port

Fabric exposes one Go `Provider` port paid by both `local-docker` and
`tencent-tke`. Process startup requires an explicit `OPL_FABRIC_PROVIDER`;
`local-docker` and `tencent-tke` are the only accepted current values.
The Fabric CI job enables the real local Docker integration test, which verifies
the provider writes and owner-authoritative readback rather than treating an
interface or control-service health check as portability evidence.

The Core port exposes provider-neutral compute, storage, attachment, runtime,
preflight, readback, renewal, and recovery facts. The selected instance profile
chooses an adapter. Provider-specific identities, diagnostics, retry rules, and
mutation sequences remain inside that adapter. Generic `kubernetes` follows only
when the common contract is proven by real paths. Control Plane keeps the one
Launch business Reconciler and selected provider-profile ref; Fabric persists
each stage-operation binding and the provider resource mapping.

Both adapters resolve package identity from their deployment-owned Provider
Profile. Tencent/TKE reads CVM instance type, TKE NodePool, zone, CBS disk and
billing facts from that profile; Local-Docker reads CPU, memory, storage and
quota policy from its own profile. A missing or invalid profile produces an
empty catalog and a fail-closed launch error. Fabric never falls back to
`basic`/`pro` resource literals. An admitted launch carries the canonical
Provider plan and `specDigest`; replay, destroy and readback use that immutable
binding or persisted resource facts, so later profile rotation cannot silently
change an existing Workspace.

The Tencent provisioner also owns Workspace NodePool image-garbage-collection
settings. New native NodePools receive explicit high/low kubelet thresholds.
For existing package NodePools, the read path inventories and validates the
exact protected pool set, preserves unrelated kubelet arguments, and reports
whether reconciliation is required. The mutation path requires its dedicated
manual confirmation and live-mutation flags, updates existing nodes, and reads
the NodePool configuration back. An unknown mutation or readback result remains
unknown and is reconciled by read only. Its failure projection retains only the
fixed `modify_node_pool` or `node_pool_readback` stage and a bounded Tencent SDK
error code when one exists; SDK messages, request IDs, and original NodePool or
Node values are excluded.

A retry is never inferred from elapsed time. After an owner-authoritative read
proves that both package NodePools still have the exact legacy taint, the first
independently confirmed recovery action may claim one fixed attempt bound to
the exact original migration digest and a fresh full NodePool/Node digest. If
that attempt also remains `started` with no provider request recorded and a new
read again proves both exact legacy taints, recovery v2 requires a distinct
action, confirmation, operation ID, record ID, and idempotency key. It validates
both immutable prior attempts and all three binding digests before it may claim
one fresh attempt. Every attempt is one-way: an unknown result permanently
allows GET-only reconciliation for that identity and never authorizes replay.
These are source capabilities; only Instance receipts and owner readback prove
their production execution and effect, as reported in [status](status.md).

The read-only `POST /fabric/provider-facts/batch` boundary delegates resource
interpretation to the selected adapter. Control Plane Provider Acceptance uses
that same provider-neutral facts shape for compute, storage, attachment, and
Runtime readiness. Provider Acceptance tooling and protected Instance qualification
require canonical compute/storage provider IDs; compatibility node-pool and
persistent-volume fields are optional response-only projections and do not
participate in readiness or continuity comparison.
For Tencent Runtime facts, `computeRuntimeBinding` reports `matched` or
`mismatched` only after the persisted Compute, active Machine ownership, and
provider Pod/Node binding can be verified together. Missing ownership or a
failed binding read leaves this observation absent; it does not imply a match.
Control Plane projects only the bounded provider error code prefix; provider
messages and response bodies are excluded from the operator fact projection.

Operator resource reads consume the typed `ResourceObservation` attached by
Fabric to the same provider read. Its current state, observation time and
bounded reason are separate from retained `ProviderFact.Available/Facts`
validation used by financial and lifecycle operations. An observed stopped or
absent resource does not change those operations' validation rules. Missing
observation evidence is unavailable, never inferred from a historical status.

`GET /fabric/runtime-observations` inventories physical Runtime controllers
before matching persisted Fabric ownership. Binding claims remain visible for
unregistered or conflicting objects. Control Plane reconciles this inventory
against its complete current Workspace set and lifecycle intent through
`GET /api/operator/runtime-observations`; the operator health card uses the same
query. Running, normal suspension, pending transitions, missing and unmatched
objects remain distinct. Incomplete discovery cannot prove absence. The older
physical summary retains `ready + unready = total`; a suspended controller is
physically unready without necessarily being a business fault.

Fabric readiness separates `serviceReady` from strict `ready` and retains all
image qualification fields. Operator service health uses the former and
exposes the latter as `releaseReady`; Launch preflight keeps strict admission.
Neither field proves Tencent balance or authorizes procurement.

Gateway operator totals use one complete Control Plane Account collection,
validate each current Sub2API identity, and read native balance, Key count and
batched usage through the existing bounded client concurrency. Disabled mapped
Accounts are included; Gateway-only users are outside this scope. Duplicate
identity mappings or failed identity reads invalidate all three totals.
An independently unavailable balance, Key count or usage invalidates only that
metric after identity validation. No partial sum, wallet replica or raw Key is
returned to the Console.

The Local Docker adapter validates an immutable Workspace image against its
trusted repository or exact release-manifest source before Docker access or
Fabric operation persistence. Its running container ID, service identity, and
provider binding are immutable Runtime identity facts; Docker-assigned HostPort
and URL are live routing facts that authoritative Runtime readback refreshes
after a restart.
Its Runtime stage maps the admitted package to Docker cgroup CPU and memory
limits and requires exact `HostConfig` readback. Its storage stage assigns a
stable Linux project ID to the Workspace host-directory tree, applies the
requested `SizeGB` as the project hard block limit, and requires kernel quota
readback before returning `ready`. The backend requires Linux 5.14+ for
`quotactl_fd` and a dedicated ext4/XFS filesystem with project quota enabled.
Readiness verifies that the configured root is the filesystem root of one unique
visible mount, rejects bind-mounted subdirectories and foreign root inventory,
and checks kernel quota readback for every retained Workspace. Quota application
uses fd-relative, no-symlink, no-cross-mount traversal. Storage deletion writes a
durable Fabric-owned tombstone before removing data, then clears and reads back
the project quota before removing the tombstone, so retry and process restart do
not leak a quota record or reuse its project ID.
Legacy schema-1 directories make the provider fail readiness before Launch and
must be deleted/recreated with the preceding release; they are not silently
adopted without a known `SizeGB`. Non-Linux or non-project-quota storage roots
also fail the existing preflight, with no unenforced-directory fallback. These
provider-specific mechanics do not add CPU, memory, or disk HTTP routes.

## Launch Integration

[Workspace Launch recovery](implementation/workspace-launch-recovery.md) owns
current stage routes, exact binding, authorization, continuation, replay and
Runtime image-repair semantics. Control Plane's
`internal/server/workspace_launch_reconciler.go` owns the business reducer;
Fabric's `internal/fabric/workspace_launch_stage_engine.go` owns the resource
stage journal and provider readback. Their narrow shared Go types and hash
contracts live under `packages/contracts`.

The read-only disposable-reset preview remains incomplete and does not authorize
cleanup. Canonical-fact repair changes only the proven missing `specDigest` and
version through an audited CAS; it does not resume the purchase. The supporting
implementations are `workspace_launch_disposable_reset.go`,
`workspace_launch_canonical_fact_repair.go` and
`workspace_launch_stage_diagnostic.go` in Control Plane's server package.

## Persistence

Control Plane, Fabric, and Ledger each own their PostgreSQL schema and table
namespaces. Cross-service writes go through typed HTTP clients; no service writes
another service's tables. Sub2API data remains in Sub2API. Portable Compose
defines separate roles/databases and
cross-owner credentials. Production adoption requires Instance readback;
retained evidence is reported only in [status](status.md).

Ledger verifies capability signature, caller, resource, action, operation,
expiry, and body digest before any owner lookup, then compares the claims with
the persisted account and Workspace. Only Receipt identity is used for the
capability owner lookup. Artifact and review identifiers remain provenance
columns for historical compatibility, while `review_policies` remains a
historical table with no current writer, API, or migration/delete operation.

Support has no current controller, route, HTTP client, application service,
Store method, Ent schema or retention writer. The exact retired
`/api/support/tickets` path returns `404` for every method instead of the SPA.
Existing migrations, legacy tables/rows and generic audit evidence remain data
custody only: fresh schema initialization omits the Support table, and startup
or retention does not delete retained rows. This does not provide a compatible
ticket API or justify restoring the customer affordance.

All three services serialize startup migrations with one database-wide PostgreSQL
advisory lock. A migration is journaled in `opl_schema_migrations` by service and
version only after it succeeds. Completed hard cuts, backfills, Ent schema changes,
and embedded SQL are skipped on every later start; a failed migration has no success
record and is retried on the next start.

Production upgrades run the journaled migrations against the existing database.
Legacy identity collisions fail closed; migrations never merge or delete those
records automatically. The identity cutover requires the same migrations to pass
against an isolated PostgreSQL copy before production deployment.

## Resource And Billing State

Financial confirmation uses the original code, user, amount and used status.
Launch and Renewal do not wait for a post-debit wallet snapshot; wallet
adjustments may report a separately observed snapshot without making it a
settlement condition. Receipts and reconciliation do not invent an unobserved
zero balance or compare observations from different times.

Control Plane financial reconciliation enumerates retained Launch, Renewal and
business-refund operations. Each purchase, renewal debit and refund is counted
independently, including multiple periods on one Workspace and orders whose
Workspace has been deleted. An automatically reversed Renewal has two financial
legs bound by its single refund receipt. Retained original order facts and
immutable receipts supply the historical resource and period; current provider existence
is checked by fulfillment and recovery owners, not used to rewrite old accounts.
Retained schema-2 single-debit purchases and their automatic refunds have a
read-only financial decoder; it does not reactivate the retired Launch executor.
Older split-resource Launch rows without a retained single-debit record remain
explicit review exceptions rather than being certified from current resources.
Normal in-progress settlements contribute to `counts.pending`, not `matched` or
the stop-purchase guard. Missing, conflicting, duplicated or over-refunded
completed settlements retain account/operation/kind/Workspace exception bindings.

Ledger applies `accountId` and exact `requestId` before receipt pagination, with
an expression index over retained receipt JSON. The returned typed lookup scope
confirms that the owner applied the filters; an older endpoint ignoring the new
filter cannot prove absence. Launch readback queries the current and explicitly
retained historical request identities separately. Disposable inspection scopes
its existing request/execution matching to the Workspace, preserving historical
execution-only evidence without scanning unrelated account history.

The existing customer fees page uses one Ledger cursor over billing receipts
and business-refund wallet-adjustment receipts. Refunds resolve their original
order through Control Plane's retained typed operation, even after Workspace
deletion. The page identifies the original order and period, distinguishes
charges, refunds and expiry without a charge, and explains that monthly fees and
API use share the account balance. Ledger's query remains policy-free; Console
receives a safe customer projection without upstream transaction codes.

Wallet adjustment persistence owns refund admission and progress. Initial
creation locks the account and original order in a PostgreSQL transaction,
validates the original charge/account, and counts all associated completed and
unresolved refunds before inserting the operation. The first dispatch rechecks
the original order and reserved amount. Progress compares the exact stored
result and status. This reuses RuntimeOperation rows without a second wallet,
reservation table or workflow engine; legacy operation JSON remains readable.
Successful Renewal refund sources use their owning `active/complete` state.
Renewal recovery rechecks the original debit before further fulfillment, and a
new automatic refund requires that debit's authoritative readback. Recovery
never repeats an attempted wallet adjustment. It reads the original evidence
and leaves missing or conflicting evidence pending/manual review, including
when an operator requests recovery after a process restart.

The native Sub2API 0.2.4 wallet integration uses
`POST /api/v1/admin/users/{userId}/balance` with positive decimal `balance`,
`operation: subtract` for debits or `add` for credits, and the original Cloud
code as `Idempotency-Key`. Before any request, the adapter rejects amounts that
cannot preserve exact USD micros through the native float64/decimal conversion
or fit its `numeric(20,8)` storage. The exact `notes` value is
`OPL Cloud balance adjustment: <code>`; descriptive text does not change this
binding. Cloud persists dispatch before sending and does not replay a monetary
POST after a lost response, authentication error, process crash, or missing
history. The upstream idempotency cache is not a durable financial journal.

Readback uses `GET /api/v1/admin/users/{userId}/balance-history` with
`type=admin_balance`, `page`, and `page_size`. The Gateway `code` identifies its
random audit record; exact `notes` binds the Cloud operation. Cloud confirms
one matching record with the original user, successful status, exact signed
amount and timestamps. Duplicate adjustments conflict; missing history does
not prove a debit or refund absent. Historical `balance` redeem records remain
readable only with their original verified `balance_applied_value`; no current
balance can establish a historical applied amount.

Before a credit, a read of `/api/v1/admin/settings` must establish both typed
`affiliate_enabled` and `affiliate_admin_recharge_enabled` flags; at least one
must be false so a refund cannot award recharge commission. Cloud does not
change Gateway settings. Instance qualification reads the same native history
and exact notes binding and records only the relevant redacted capability facts.
The Gateway source, image and database remain unmodified.

The deployed Sub2API has no generic hold/capture API. The launch path validates
the account and quote, runs read-only provider preflight, confirms balance, and
debits the exact monthly amount before Fabric mutates provider resources. It then
claims every PREPAID CVM/CBS fact and activates the Workspace only after
readback. A confirmed zero-resource result permits one idempotent refund;
partial or unknown provider results enter manual review without refund or
repurchase. Ledger receipt failure retries only the receipt. Current source and
tests implement this behavior. The exact runtime evidence
and remaining acceptance outcomes belong to [status](status.md) and
[roadmap](roadmap.md).

Tencent compute and storage monthly preflight each call the zero-mutation IAM
gate before their provider checks. The gate accepts only schema-v3,
Candidate-bound deployment evidence for the live STS identity, the exact Tag
actions, and `QcloudCVMFinanceAccess`; each call also reads the current STS
identity and attached CAM system policies. The Fabric boundary revalidates all
safe facts. `InquiryPriceCreateDisks` remains a price check and is never treated
as evidence that `CreateDisks` may perform prepaid settlement.

Activation readback is the Control Plane `GetWorkspace(workspaceId)` point-read
projection matched to the original launch and Fabric bindings. The terminal
purchase receipt uses `RequestID=launchOperationId` and
`Idempotency-Key=<launchOperationId>:purchase-receipt`, with exact Workspace,
debit code, user, total, component, and downstream resource identities.

Workspace DELETE is a separate durable `workspace.delete.v2` Control Plane
owner operation. Before cleanup, it reads the immutable succeeded Launch and
matches its exact charged or zero-cost Ledger Launch Receipt; it does not read
Debit history or invoke a wallet mutation. Runtime, compute, storage, and
attachment remain bound to the Launch, while the current Workspace Key and
Gateway Secret are recovered from the current Workspace projection and the
strict completed Key Rotation lineage from the Launch Key. It then consumes Fabric's typed
Runtime and Gateway Secret observations (`ready/absent/pending/conflict/error`)
and advances only through the same-operation chain `runtime + Secret absence ->
attachment absence -> storage absence -> compute absence
-> Control Plane Workspace absence -> Ledger workspace.deleted.v1 Receipt ->
complete`. The operation binds the same account, Workspace, Launch Receipt,
Runtime, current Key, and provider-neutral resources throughout. The
`workspace_absent` transaction deletes the exact matching Control Plane
compute, storage, attachment, and Workspace projections with its cursor. Fabric owns resource
mutation and authoritative absence, including Tencent Machine/CVM/CBS readback;
Gateway Keys remain in Sub2API. Delete performs no Gateway or wallet mutation.
Ledger Receipt failure retries only the deletion Receipt. Non-terminal legacy
v1 Delete and concurrent Renewal fail closed before a v2 mutation. Delete and
Key Rotation use the same durable Workspace claim order and block each other
before Fabric or Sub2API mutation.

The enabled Workspace Launch worker starts the retained v2 Delete worker; the
portable Local Workspace overlay enables both it and the monthly worker. The
Delete worker continues retained v2 operations using service
authorization and the original Launch/Key identity; it needs no customer password
or session credential. `GET /api/workspaces/{id}/deletion` returns owner-bound
pending/manual-review/deleted progress, or `null` before a Delete is authorized.
It remains readable after the Workspace projection is removed so a pending
Ledger Receipt is not displayed as completion. Compute absence reads remain
scheduled until the owner confirms absence, without repeating destroy.

Each Workspace operation owns renewal intent and one combined monthly debit.
Compute and storage rows are provider facts, not independent customer renewal
controls. The existing monthly worker wakes at the next known renewal/expiry
boundary, with a one-minute default discovery interval. Unpaid expiry disables
entitlement and auto-renew, terminates existing proxied streams, and persists a
`runtime_suspend` step. The typed Fabric Runtime power contract binds the
original account, Workspace, Runtime operation and paid-through period. Fabric
serializes power with destroy and rejects stale periods or deleted Runtimes;
Tencent scales the exact Deployment to zero and waits for Pod absence, while
Local-Docker stops the original container. Storage is not deleted by expiry.

The renewal read reports recovery eligibility from original resource, Runtime,
period and wallet readback. A customer's explicit `autoRenew=true` authorization
after expiry both authorizes original-period recovery and enables subsequent
auto-renewal. Every new debit attempt rechecks original resources. Confirmed
renewal waits for original Runtime running before restoring entitlement. Access then validates the current
period against that committed renewal and exact debit, retaining the immutable
initial Launch. Existing proxy connections follow the confirmed renewed period. A
reclaimed resource or elapsed next anchored period is unavailable for recovery;
no replacement purchase or multi-period catch-up is implied. Adding balance
alone cannot recover a Workspace. Non-billing customer-owned resources keep
their existing non-billing access semantics. Provider reclamation remains the
provider's lifecycle, and no post-expiry data retention is promised.

### Local-Docker Host Capacity Admission

The local-docker Fabric adapter owns host-capacity admission. Control Plane
continues to call the existing staged Workspace APIs and Console has no direct
Docker or quota surface. The Runtime stage maps the admitted package to Docker
CPU, memory, and memory-swap cgroup limits, then reads HostConfig back. CPU and
memory must match exactly. Memory-swap must equal the admitted memory limit
unless `OPL_FABRIC_LOCAL_DOCKER_ALLOW_UNBOUNDED_SWAP=1` explicitly permits
Docker's `MemorySwap=-1`; this does not relax CPU, memory or storage admission.
Storage maps admitted SizeGB to a Linux project-quota hard limit on the
host-owned Workspace root.

Before a Runtime is dispatched, Fabric holds the storage-root flock, reads the
Docker daemon NCPU and MemTotal facts, validates every OPL Runtime label and
cgroup readback, and sums durable Runtime reservations. The reservation is
persisted before Docker run. It remains charged after restart or an uncertain
Docker response and is released only after container absence is read back.
An existing reservation retains its admitted CPU and memory facts independently
of later Provider Profile changes and must match the live labels and cgroup
limits exactly. A missing reservation is recovered only from complete positive
live cgroup limits, one canonical Runtime name, and deterministic identity; an
unbounded legacy Runtime is not inferred from the current profile. Public
Local-Docker Runtime status uses the same locked reservation reconciliation
before its storage, Secret, network, and health readback. Lock acquisition
honors the caller context so status and mutation deadlines remain bounded while
another Local-Docker operation owns the storage-root lock.
Malformed capacity evidence, unknown OPL Runtime, drift, inventory errors, and
arithmetic overflow reject admission. This is an OPL-managed reservation
boundary, so a shared Docker daemon must reserve capacity for non-OPL workloads
or be dedicated to Fabric.

Storage uses the same flock to recover journals and sum unique StorageID
reservations across active roots, staging roots, and deletion tombstones. New
SizeGB is admitted only when that sum plus the new project-quota limit fits the
effective filesystem capacity, calculated from Blocks, Bfree, Bavail, and Bsize
with overflow checks; the immediate writable-block check must also succeed. A
tombstone stays charged until quota clear and its zero-limit readback complete.

## Workspace Runtime Access

[Workspace Runtime access](implementation/workspace-runtime-access.md) owns the
current proxy routing, Runtime ABI, credential and immutable-image integration.
Instance domains and deployment evidence remain in the Instance repository.

The separate [image lifecycle](implementation/workspace-images.md) reference
owns activation and image-only replacement of an existing Workspace.

## Product Distribution

[Product release operations](runtime/release.md) owns Candidate qualification,
publication admission, same-digest promotion, recovery and public readback.
[Installation](installation.md) owns the downloadable installation procedure.
[Status](status.md) owns retained release and Instance evidence, while
[roadmap](roadmap.md) owns unresolved qualification outcomes.
