# OPL Cloud Implementation Architecture

This repository implements the OPL Cloud product layer. Browser Console calls
Cloud Control Plane APIs; App Shells consume Cloud-facing projections through
App/Framework contracts. Cloud service and database authority stays in the
owning Cloud service.

## Request Path

```text
Browser Console
  -> Control Plane product API
       -> Sub2API management API: live balance, account Key/usage, idempotent debit/refund
       -> Fabric API: typed compute, storage, attachment, Secret, and runtime stages
       -> Ledger API: receipts and reconciliation evidence
```

Sub2API is external and remains the only spendable-balance, API-key, routing,
and request-usage owner. The repository reads those records on demand and does
not mirror them. Its code, image, database, configuration, and deployment remain
outside this repository's mutation boundary.

## Family Domains And Host/Client Integration

The physical Cloud owners are Console UI, Control Plane, Fabric, and Ledger. A
family capability may span those modules and the Framework/App repositories;
the integration boundary remains the versioned Cloud image and typed service
contracts.

```text
Cloud service/image + typed API contracts
  <- Framework Host Cordis Cloud adapter contribution
       <- Host-selected product profile
            -> allowlisted Client Cordis graph
                 -> App renderer/package carrier
```

Framework Host projects the client graph used by App renderers and reaches Cloud
through a Framework-owned typed adapter. Cloud API, persistence, provider,
wallet, Workspace, and Ledger ownership remain with their modules. Package and
service versioning follow each owner's release cadence.

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

packages/contracts -> CI and service-boundary verification only
```

| Module | Physical boundary | Owns | Allowed dependencies | Forbidden coupling |
| --- | --- | --- | --- | --- |
| Console UI | `apps/console-ui`, TypeScript build | presentation and customer interaction | Control Plane product APIs under `/api/*` | direct Fabric, Ledger, Sub2API, Tencent, Kubernetes, persistence, or server implementation imports |
| Control Plane | independent `go.mod`, binary, Deployment and schema | session/account mapping, Workspace entitlement, Launch cursor/attempt/lease/CAS, account/settlement coordination and customer DTOs | typed HTTP clients for Fabric, Ledger and Sub2API; narrow PostgreSQL migration helper | resource-stage reducers, Fabric operation derivation, Fabric/Ledger implementation imports, provider fields/SDKs/Kubernetes, provider mutations, or downstream table writes |
| Fabric | independent `go.mod`, binary, Deployment and schema | compute, storage, attachment, Secret binding, Runtime, provider-neutral operation bindings/store, provider mutations and authoritative readback | provider adapters, cloud SDKs and narrow PostgreSQL migration helper | wallet, customer billing policy, Console session state, or Ledger table writes |
| Ledger | independent `go.mod`, binary, Deployment and schema | receipts, reconciliation, idempotency, retention, and caller-owned opaque provenance refs | narrow PostgreSQL migration helper | review-policy or review-gate semantics, Launch continuation authority, spendable balance mutation, provider SDKs, Fabric execution, or Control Plane table writes |
| PostgreSQL migration helper | independent narrow Go module under `services/internal/postgresmigrate` | advisory lock, migration journal and TLS validation mechanics | PostgreSQL driver only | any Console, Control Plane, Fabric or Ledger domain type |
| Machine contracts | JSON under `packages/contracts` | executable ownership and protocol boundaries | tests, build and validation | runtime state, service implementation or a second status owner |

`tests/contracts/module-physical-boundaries.test.ts` enforces the source-level edges:
no Go service may import another service implementation, only Fabric may import
Tencent or Kubernetes SDKs, and Console network calls must remain inside its API
adapter and resolve to `/api/*`. This gate runs through the existing `npm test`
lane; it complements behavior and contract tests rather than replacing them.

The portable Compose source now gives each service its own PostgreSQL
role/database and inbound service token; Control Plane receives separate Fabric
and Ledger outbound tokens. A source-built portable Compose isolation run starts
the services with those identities, rejects cross-owner database access and
caller-token impersonation, and rotates each token without restarting PostgreSQL
or unrelated services. This proves the reusable source configuration, not a
clean-host release installation or concrete medopl adoption. The common image
still makes the services one intentional product release unit rather than
independent service releases.

Deployment isolation is an independent implementation lane, not a predecessor
to Console, Control Plane, Fabric, or Ledger development. Portable distribution
assets and provider adapter code stay in this repository, while concrete
manifests, values and secret references stay in `opl-instance-medopl`. The two owners
join only when qualifying an exact deployment, rollback, and authoritative
readback. The common release image may remain shared unless measured release
blast radius creates a separate requirement.

Internal cohesion has improved but remains uneven. Control Plane Launch now uses
one focused, provider-neutral Reconciler with separate account, Fabric,
activation, service, and persistence files. Fabric moved local-Docker and Tencent
Workspace Launch behavior behind adapter files and reduced
`internal/fabric/service.go`; Tencent compute-allocation identity validation now
belongs to the Tencent adapter and is reused by the targeted operator pool-head
path. The remaining facade and provider/operator extensions still mix several
capabilities. These are change-collision and review risks inside the correct owner
modules, not a reason to introduce cross-service packages.

Cohesion work is also lane-scoped rather than a repository-wide freeze. Splits
inside different owning modules may proceed concurrently. Work that touches the
same large file or changes one public contract uses a single short-lived owner
until that shared change reaches fresh-main CI; other module lanes continue.

## Current Simplification Pressure

Current implementation facts and retained evidence live in [status](status.md);
open simplification outcomes live in the [roadmap](roadmap.md). Removing a
persisted or provider-backed path requires fresh caller, data, and external
resource evidence.

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

Control Plane currently projects `https://gflabtoken.cn/v1` as the public model
endpoint. Console may present that public endpoint according to the current UX.
It never exposes or directly calls Sub2API management APIs;
`OPL_SUB2API_BASE_URL` remains server-only, and Cloud does not inject a second
Runtime Gateway base URL.

### Console Controller Owner Map

The browser application currently composes the remaining capabilities through
`apps/console-ui/src/app/use-console-controller.ts`. This is an implementation
inventory, not a promise to create one hook per row. A capability is extracted
only when its browser lifecycle, invariant, failure model, and real consumers
form one complete slice.

| Capability | Current root facts | Completion proof | Real consumers | Domain authority | Extraction decision |
| --- | --- | --- | --- | --- | --- |
| Workspace Delete | `useWorkspaceDeleteController` owns delete intent, busy, issue, local freshness, and paged absence readback | authoritative Workspace list confirms `absent`; no refund or wallet mutation is inferred by the browser | Customer Workspace detail | Control Plane Workspace lifecycle | extracted; resource absence remains separate from renewal |
| Workspace Renewal | `useWorkspaceRenewalController` owns per-Workspace intent, busy/issue, response validation, local freshness, and list/detail readback | returned renewal fields and authoritative Workspace projection match the requested setting | Customer Workspace detail | Control Plane Workspace lifecycle | extracted; account period and eligibility semantics do not share Delete state |
| Workspace Budget | `useWorkspaceBudgetController` owns per-Workspace/key intent, request signature, busy claim, local freshness, and typed owner readback | returned Workspace/key identity and stable requested policy fields match the authoritative result | Customer Workspace budget panel | Sub2API/Gateway budget authority | extracted; configuration conflict is not a Workspace lifecycle transition |
| Support Mapping | `useSupportController` owns ticket projection, loading/error, create intent, busy, and post-write reload | mapping write is followed by an authoritative ticket read | Console support slide | Control Plane support mapping | extracted independent slice |
| Billing / Receipt | `useBillingController` owns billing view, Receipt list/detail remote state, selected Receipt ID, opaque cursor stack, independent list/detail freshness, and route/session/reset | only the current Session, route, request generation, and selected Receipt may commit; overview requests 3 Receipts and Billing requests 20 | Customer billing page and overview receipt links | Ledger | extracted query slice; Workspace terms remain in the Workspace projection |
| Gateway Usage | `useGatewayUsageController` owns Key collection, selected Key, period/page, independent usage/summary state, and Key/usage generations | only the current Session, route, Key, period and request generation may commit; an empty authoritative Key collection clears both projections | Customer usage page | Sub2API/Gateway | extracted; query freshness and partial failure remain separate from Billing/Receipt |
| Operator Account | `useOperatorAccountController` owns account projection/pagination, provision intent/operation, per-account disable and purchase-eligibility intents, busy claims, local freshness, readback, and reset | typed command identity and target fields must match; provision, disable, and eligibility complete only after the authoritative paged Account projection matches | Admin accounts page; Wallet Adjustment depends only on its narrow `refresh` port | Control Plane Account/Access; Sub2API remains Gateway identity/wallet authority | extracted; same-semantic response-loss retries resend the original key, without claiming server-side replay semantics for provision or disable |
| Wallet Adjustment / Recovery | `useWalletAdjustmentController` owns wallet intent, operation projection, recovery intent, busy, and operation/account readback | one wallet operation reaches a typed terminal or manual-review state and is read back | Admin wallet panel | Sub2API wallet with Control Plane audit coordination | extracted; money and recovery remain separate from Account lifecycle |
| Operator Announcement Lifecycle | `useOperatorAnnouncementController` owns the operator projection, normalized create intent, per-announcement publish/withdraw intents, busy claims, local freshness, authoritative readback, and reset | typed command identity, target status and schedule must match; `draft/scheduled -> scheduled|published -> withdrawn` completes only after the operator projection matches | Admin overview and announcement routes | Control Plane operator/content | extracted state-machine slice; customer published-list/read receipts remain separate |
| Customer Announcement Read | `useCustomerAnnouncementController` owns the active published projection, Overview/list query scope, per-announcement unresolved read intents, one in-flight claim, Session-confirmed receipt IDs, local freshness, receipt projection, and reset | page 1 and page size must match the active `overview:3` or `list:20` scope; the receipt must name the requested announcement and contain a valid RFC3339 `readAt`; no current or later GET may commit a visible `read=false` for a Session-confirmed receipt | Customer overview and announcements pages | Control Plane announcement content and per-user read-receipt authority | extracted; a valid receipt completes the write while GET only synchronizes the current bounded projection |
| Operator Resource Read | `useOperatorResourceReadController` owns the `/admin/resources` list, Workspace detail, Runtime image policy, and replacement preview projections, fixed page size, selected Workspace identity, local route/Session/request freshness, independent failure settlement, and reset | list page/pageSize, detail/preview Workspace identity, route/Session/request scope, and source identity must match before commit; detail and preview failures never replace their sibling projection | Admin resources page and Runtime Image Replacement refresh ports | Control Plane, Fabric, Ledger, and Sub2API remain authorities for their projected facts; Console owns only the browser read projection | extracted; no new backend authority or mutation path |
| Gateway Account Read | `useGatewayAccountReadController` owns Wallet, monthly account Usage, fixed 20-item Balance History, endpoint, committed history page, and per-projection Session/route/request freshness | each response must match its Sub2API source, current route plan, Session, request generation, and requested page; failures settle independently and cannot advance the committed page | Customer overview, Workspace Launch, API overview, and Keys endpoint projection | Sub2API/Gateway | extracted; Gateway Usage, Key mutation, and Workspace Budget remain separate capabilities |
| Customer Workspace Read | `useCustomerWorkspaceReadController` owns Overview/list/detail/Billing-terms Workspace projections, committed page, active Workspace identity, local freshness, and the detail projection lease | list page/pageSize or detail Workspace identity, Control Plane source, Session, route, and request generation must match; Renewal readback may commit only through the current lease | Customer overview, Workspace list/detail, Billing terms, Renewal readback, Secret access, Delete, and Budget coordination | Control Plane Workspace projection and lifecycle | extracted query slice; Launch/Delete/Renewal commands retain their operation-specific readback invariants |
| Fabric Runtime Read | `useFabricRuntimeReadController` owns the current customer Workspace Runtime projection and its Session/route/request freshness | an available response must originate from Fabric and name the active Workspace; Runtime failure or stale completion cannot replace Workspace detail or Budget | Customer Workspace detail and Secret composite refresh | Fabric Runtime | extracted; Runtime mutation and Operator Runtime Image Replacement remain separate capabilities |

The root remains the Composition Root: Session and Auth, Router, global toast
and shell state, cross-owner route loading, aggregate reset orchestration,
Workspace Gateway Budget dependency ordering, and the single-request Operator
Overview, Reconciliation, and Health projections. A child controller receives
narrow typed dependencies and owns its own command intent, busy semantics,
local freshness, authoritative readback, and reset. It must not receive the
complete root controller, root setters, or another capability's internal state.

Customer Workspace Read owns the monotonic `workspaceDetail` projection lease;
the Composition Root retains only the independent `workspaceBudget` lease and
the ordering that waits for an accepted Workspace detail before reading the
Workspace Key budget. Renewal and Budget may commit their typed owner readback
only while their corresponding lease is current. Fabric Runtime Read starts in
parallel with Workspace detail and owns a separate freshness scope, so neither
projection can block, cancel, or overwrite the other.

This map makes the current coupling explicit. `commandBusy` now remains only
with operator Account provisioning; Workspace Delete and Renewal no longer
share it. `findWorkspaceInPages` is a thin API adapter used independently by
the owning Workspace controllers, while cross-owner route loading and
`resetConsoleState` remain Composition Root responsibilities. The Console
stop-audit confirms every remaining root field and loader is composition, a
narrow owner port, or a single-request projection without an independent
lifecycle, so this Console decomposition round is closed. Another slice is
justified only by an observed selection, pagination, timer, stale-write, or
independent failure model; Session is not extracted until every child reset
contract is explicit.

Operator Announcement Lifecycle is owned by
`useOperatorAnnouncementController`. Create, publish, and withdraw retain their
same-semantic idempotency key until both the typed command response and the
authoritative operator list agree on announcement identity, content, schedule,
and target status. Per-announcement claims survive route exit until the pending
request settles, while local scope and list generations reject old route or
Session completions. `AdminPages` keeps only dialog visibility and draft form
fields. Customer announcement loading and read receipts are owned by the
independent `useCustomerAnnouncementController` because their published-only
projection, fixed list limits, user identity, and failure model are different
from operator content mutation. Overview and list reads bind page 1 to page
sizes 3 and 20, and only the current Session, route scope, and query generation
may commit. A valid typed receipt immediately projects the target as read;
subsequent GET failure preserves that durable fact, absence is valid for a
bounded active projection, and a visible `read=false` conflict is not committed.
Session reset clears intents, the active claim, and busy projection while claim
tokens keep stale `finally` blocks from releasing a new Session's claim.

Support Mapping is the first post-inventory slice. Its
`useSupportController` owns ticket loading/error state, the mapping intent and
idempotency key, the command busy state, local read freshness, POST response
identity validation, and the GET list readback. `ConsoleShell` consumes only
the typed `SupportController`; the root supplies Session, mutation freshness,
toast/error rendering, and reset composition. No support state or command
remains in the broad root.

Workspace lifecycle browser mutations now have three separate owners.
`useWorkspaceDeleteController` alone retains the stable delete intent and
accepts success only after the Control Plane list confirms final absence.
`useWorkspaceRenewalController` validates the typed renewal command response
and then requires the authoritative Workspace projection to match the requested
`autoRenew` value. Command scheduling status and lifecycle projection status
remain separate facts.
`useWorkspaceBudgetController` remains outside the resource lifecycle and
scopes its intent and readback to the exact Workspace Gateway Key. The three
controllers have independent busy, freshness, failure, and reset state; the
Workspace detail page only cross-disables Delete and Renewal while either
conflicting command is active. Navigation invalidates active requests and busy
claims without deleting unresolved per-Workspace intents; Session replacement
is the boundary that clears all three controllers.

Gateway Usage now has one `useGatewayUsageController` query owner for the Key
collection, selected Key, period/page, independent Usage and Summary remote
state, and both freshness generations. The Customer usage page consumes the
typed controller, while the root only composes route loading, Session lookup,
error presentation, and aggregate reset. Key, period, route, Session, and reset
changes invalidate older completions; an authoritative empty Key collection
clears the selected Key and both projections. Usage and Summary may fail
independently without hiding a successful sibling result. Billing/Receipt
remains separate because its cursor/detail identity and Ledger failure model do
not share these invariants.

Billing/Receipt now has one typed `useBillingController` query owner for the
billing view, Receipt list/detail remote state, selected Receipt ID, opaque
cursor stack, independent list/detail freshness, and route/session/reset. The
Overview route requests the latest 3 Receipts without adopting Billing cursor
state, while the Billing route requests 20 and owns cursor navigation and
Receipt detail selection. Ledger remains the Receipt authority. Workspace terms
continue to come from the Workspace projection and do not enter the Billing
controller. The root only composes the typed controller with Session, Router,
error presentation, and aggregate route/reset orchestration.

`code-complete` means the local contracts, code, PostgreSQL, browser, and
structure gates pass on one revision. `pilot-ready` additionally requires
approved real service/resource evidence. `production-proven` requires the same
immutable revision deployed and authoritatively read back in production.

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
unknown and is reconciled by read only. A retry is never inferred from elapsed
time: after an owner-authoritative read proves that both package NodePools still
have the exact legacy taint, an independently confirmed recovery action may
claim one fixed recovery attempt independently bound to the exact original
attempt digest and a fresh full NodePool/Node digest. That recovery attempt is
also one-way and cannot be replayed after an unknown result. This is source
capability in PR #502; no Instance receipt currently proves that the production
NodePools were changed.

The read-only `POST /fabric/provider-facts/batch` boundary delegates resource
interpretation to the selected adapter. Control Plane Provider Acceptance uses
that same provider-neutral facts shape for compute, storage, attachment, and
Runtime readiness. The Cloud Provider Acceptance CLI and production live-QA
require canonical compute/storage provider IDs; compatibility node-pool and
persistent-volume fields are optional response-only projections and do not
participate in readiness or continuity comparison. The Local Docker adapter also
validates an immutable Workspace image against its trusted repository or exact
release-manifest source before Docker access or Fabric operation persistence.
Its running container ID, service identity, and provider binding are immutable
Runtime identity facts; Docker-assigned HostPort and URL are live routing facts
that authoritative Runtime readback refreshes after a restart.
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

## Launch Boundary Integration

Fabric exposes `/fabric/workspace-launches/preflight`, the persisted binding
readback `/fabric/workspace-launches/preflight/read`,
`/fabric/workspace-launches/stages/read`, and
`/fabric/workspace-launches/stages/ensure`. The stage DTO contains only the
provider-neutral binding and resource refs used by the real Control Plane caller.
Fabric persists the parent binding and a deterministic child record before each
actual provider write, then reads both by exact operation identity; typed
readback never scans operation listings or reconstructs Launch ownership from
suffixes or provider tags. Both owners consume the same focused golden vectors,
and the normal Launch/Resume caller is integrated. This closes the typed boundary
slice but not the full Console-to-local-Workspace P0 vertical.

The preflight readback accepts only one opaque binding reference, reuses the
strict persisted-preflight decoder, and returns the verified launch identity
and `specDigest` without the canonical provider plan. It is service-authenticated
but has no mutation capability and does not call a provider. Control Plane uses
it only for the narrow operator preview that repairs a schema-3 operation whose
sole missing canonical fact is `specDigest`.

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

## Persistence

Control Plane, Fabric, and Ledger each own their PostgreSQL schema and table
namespaces. Cross-service writes go through typed HTTP clients; no service writes
another service's tables. Sub2API data remains in Sub2API. The portable Compose
configuration and its source-built acceptance prove separate roles/databases and
cross-owner denial. Legacy production credentials have not been replaced and
read back through the Instance owner, so production adoption remains unproven.

Ledger verifies capability signature, caller, resource, action, operation,
expiry, and body digest before any owner lookup, then compares the claims with
the persisted account and Workspace. Only Receipt identity is used for the
capability owner lookup. Artifact and review identifiers remain provenance
columns for historical compatibility, while `review_policies` remains a
historical table with no current writer, API, or migration/delete operation.

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

The deployed Sub2API has no generic hold/capture API. The launch path validates
the account and quote, runs read-only provider preflight, confirms balance, and
debits the exact monthly amount before Fabric mutates provider resources. It then
claims every PREPAID CVM/CBS fact and activates the Workspace only after
readback. A confirmed zero-resource result permits one idempotent refund;
partial or unknown provider results enter manual review without refund or
repurchase. Ledger receipt failure retries only the receipt. Source and focused
tests implement this behavior; live Sub2API and Tencent evidence remains pending,
and the repository remains `code-complete=false`.

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
attachment absence -> storage absence -> compute absence -> Sub2API Key absence
-> Control Plane Workspace absence -> Ledger workspace.deleted.v1 Receipt ->
complete`. The operation binds the same account, Workspace, Launch Receipt,
Runtime, current Key, and provider-neutral resources throughout. The
`workspace_absent` transaction deletes the exact matching Control Plane
compute, storage, attachment, and Workspace projections with its cursor. Fabric owns resource
mutation and authoritative absence, including Tencent Machine/CVM/CBS readback;
Sub2API owns exact Key deletion and performs zero Delete wallet mutations.
Ledger Receipt failure retries only the deletion Receipt. Non-terminal legacy
v1 Delete and concurrent Renewal fail closed before a v2 mutation. Delete and
Key Rotation use the same durable Workspace claim order and block each other
before Fabric or Sub2API mutation.

Each Workspace operation owns renewal intent and one combined monthly debit.
Compute and storage rows are provider/compatibility facts, not independent
customer renewal controls. At unpaid expiry, access is denied and auto-renew is
disabled, but Control Plane performs no Fabric/Tencent stop, renew, destroy, or
delete mutation; Tencent expiry policy owns eventual provider reclamation.

### Local-Docker Host Capacity Admission

The local-docker Fabric adapter owns host-capacity admission. Control Plane
continues to call the existing staged Workspace APIs and Console has no direct
Docker or quota surface. The Runtime stage maps the admitted package to Docker
CPU, memory, and memory-swap cgroup limits, then reads the exact HostConfig
values back. Storage maps admitted SizeGB to a Linux project-quota hard limit on
the host-owned Workspace root.

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

## Current Medopl Workspace Access Path

The current medopl Tencent/TKE extension data path is:

```text
Browser
  -> configured Instance Workspace domain (currently workspace.medopl.com)
  -> shared CLB / TKE Ingress
  -> Control Plane reverse proxy
  -> Fabric-created per-Workspace ClusterIP Service :3000
  -> Workspace runtime
```

The current Workspace Runtime compatibility boundary fixes the internal WebUI
port at `3000`. `opl-cloud-workspace-runtime-abi-contract.json` is its versioned
cross-module owner; Control Plane proxy routing and both Fabric adapters project
the fixed value through named constants. The ABI is not an environment override,
an Instance-selectable option, an installation default, or a separate feature
lane.

Cloud requires the Instance to supply `OPL_WORKSPACE_DOMAIN`; Control Plane and
Tencent/TKE startup fail closed when it is absent. There is no access-domain
fallback in current source. The `.cn` Kubernetes label and annotation keys are
persisted metadata identifiers rather than access domains; they require owner
inventory and a bounded metadata namespace migration instead of a mechanical
`.com` replacement.

`/w/<workspaceId>/` selects a Workspace from the URL. Root `/api/`, `/ws`, and
other Workspace-host requests select it from the `opl_ws_active` cookie or a
Workspace referrer. The proxy writes `opl_ws_active` as routing context when a
clean Workspace URL is opened; the cookie is not an authentication credential.
It forwards traffic only after Fabric reports the Runtime ready and the
persisted Workspace state becomes `running`.

Fabric runs the Workspace image in `cloud` deployment mode with `password`
authentication. Fabric derives the runtime password and session secret from a
stable per-Workspace credential seed. Tencent/TKE stores them in a Kubernetes
Secret; `local-docker` stores immutable versions under a protected host-owned
root and mounts only the selected version read-only into the Runtime.
Control Plane resolves the target Workspace's persisted `workspaceApiKeyId` and
hands the Key transiently to Fabric. Fabric writes or rotates a deterministic
Workspace-scoped secret bound to account, Workspace, Key ID, and fingerprint,
and records only its ref, version, and fingerprint. Existing
account-scoped Secrets remain read-compatible until that Workspace's first Key
rotation; ordinary reads never infer scope from Workspace count or Key name.
Ordinary runtime status is non-secret. Dedicated owner-only POST commands reveal
or rotate the password transiently; Control Plane never persists it, and Console
retains it only in Workspace detail component memory. A Workspace image candidate
combines exact `one-person-lab-app`, active-shell, and Framework revisions. The
Workspace owner publishes that image independently; an instance pins its
immutable `repository@sha256` alongside the OPL Cloud product release. The Cloud
product release does not build, publish, or promote an instance Workspace image.
The immutable Workspace image is pinned for deployment, but a customer
Workspace Ready-Pod `imageID` readback remains pending. No configured digest,
placeholder, or local timestamp substitutes for that Pod evidence.

Control Plane carries Workspace HTML, API, and WebSocket traffic, so its
availability is coupled to every Workspace connection. It selects the Runtime
Service; the Runtime owns password validation, its authenticated session, and
WebSocket access. A 2xx/non-empty-page check proves routing only; acceptance
requires an authenticated Workspace session and the exact Ready-Pod `imageID`
readback described above.

The operator-provisioned Pilot retains this single shared entry. Revisit a
dedicated Workspace Router when measured connection load or CLB rule limits
require a separate scaling owner.

## Product Release And Instance Qualification

During the current pre-1.0 phase, Cloud must produce a replaceable candidate
from one exact canonical product SHA before formal publication. The supported
Local-Docker path and `opl-instance-medopl` must qualify the same Cloud image
identity with their own Workspace image and Provider Profile receipts. The
Instance owns Tencent/TKE deployment and qualification through its protected
environment. Only after both paths succeed may an allowlisted Cloud Release
publisher manually publish the same SHA and Cloud image bytes as a formal
Release. Cloud does not dispatch or operate the Instance, and failed
development or deployment attempts do not create a formal version.

The owner-only `build-opl-cloud-candidate.yml` workflow verifies one exact
canonical Product SHA and tree, builds and pushes one run-scoped
`linux/amd64` + `linux/arm64` OCI index, and reads back the index digest, both
child manifest digests, platforms, and OCI revision. Its portable Candidate
bundle contains the installation assets, canonical
`opl-cloud-candidate.json`, and `SHA256SUMS`. The manifest binds the Cloud
source/image identity and each installation asset digest; `SHA256SUMS` covers
the installation assets and manifest. The bundle validator recomputes every
digest. Installation owners bind Workspace image, Provider Profile, domain, and
deployment evidence in their qualification receipts. The bundle carries the
generic `opl-cloud.env.example` from the same Product SHA.

The formal Release workflow has two recoverable stages. `admission` downloads
one exact Candidate plus Local qualification, Instance qualification decision,
and `workspace_verified` artifacts. It executes the Cloud and Instance-native
validators from their exact source commits, binds the Product SHA/tree, index
and platform digests, and seals a checksum-bound publication checkpoint. Its
private Instance reader uses `OPL_INSTANCE_EVIDENCE_TOKEN`, scoped only to
`Actions: read` and `Contents: read` on `opl-instance-medopl`.

`publish` is the only job under the protected `cloud-release` Environment and
the only job holding the global publication lock or write permissions. It
promotes the Candidate digest with `imagetools create`, attests the sealed
assets, and reconciles the GitHub Release as one complete same-tag cohort; a
publication-tool failure is retried on that tag and does not create a product
version. The separate
`.github/workflows/release-opl-cloud-public-readback.yml` workflow is a
read-only follower triggered after a successful publication run. It downloads
that run's sealed checkpoint, then anonymously reads back the GHCR digest and
Release bytes, verifies checksums, and verifies the OIDC attestations. A failed
follower never changes the publication result and can be retried with the
publication run ID; the publication manual dispatch still requires matching
original/current actors and either the repository owner or `RenDeHuang`.

This source path no longer rebuilds qualified bytes. A successor to `v0.1.7`
still requires a real hosted run whose four admission evidence sources bind one
Candidate and whose public readback succeeds. Product capability changes choose
new version tags; publication recovery does not. Mutable `latest` and `stable`
tags remain forbidden.

Current public Release, repository security, and Instance evidence is recorded
in [status](status.md). Open Candidate qualification and distribution work is
recorded in the [roadmap](roadmap.md).

Control Plane remains one Pod. Existing load evidence covers request concurrency
and replay, but its historical per-resource renewal scan is not proof of the
current Workspace renewal saga. The current gates must run against an isolated
PostgreSQL database. Additional replicas remain out of scope unless
production measurements justify the ownership and locking changes.

Infrastructure alarms remain in Tencent Cloud Monitor. Business alarms are a
projection of Workspace renewal operations plus compute/storage compatibility
facts; there is no alert table. Stable, redacted transition codes drive CLS
alerting.
