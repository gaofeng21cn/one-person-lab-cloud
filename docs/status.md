# OPL Cloud Current Status

Owner: `one-person-lab-cloud`
Purpose: `replaceable_current_evidence_snapshot`
State: `current_snapshot`

This file reports current implementation and the latest retained evidence. It
is not a chronological work log. Target architecture lives in
[architecture.md](./architecture.md); open outcomes live in
[roadmap.md](./roadmap.md).

## Product Cut

The current Pilot is an administrator-provisioned account product. One Console
User maps to one Account and one Sub2API identity/wallet. An Account may own
multiple independent Workspaces. Public registration, customer payment/top-up,
shared multi-user Workspaces, HA, and GPU are not current customer capabilities.

Basic and Pro are visible Workspace packages with integer USD-micros pricing.
Control Plane owns quotes and purchase eligibility; Sub2API owns spendable
balance, Keys, routing, and usage. The current offer set is Basic and Pro, and
Console and server admission both accept an available balance exactly equal to
the authoritative quote.

Console calls Control Plane product APIs. Control Plane, Fabric, and Ledger are
separate processes and PostgreSQL schema owners. Fabric provides Local-Docker
and Tencent/TKE adapters behind the same provider-neutral boundary. Tencent/TKE
is selected and configured by the medopl instance, not by the portable product.

## Implemented Paths

- Workspace Launch persists one Control Plane operation and coordinates Key,
  debit, Fabric resource stages, activation, and one purchase Receipt. Unknown
  external results enter manual review and resume through the same operation.
  An unknown Storage attempt is classified from its exact Fabric binding;
  ready advances read-only, pending remains bounded and read-only, and only
  authoritative absence permits one replay with the original idempotency key.
  If that replay itself ends failed, the same Resume route may classify the
  exact binding again: ready finishes read-only, while authoritative absence
  replaces only the failed claim and permits one new authorization to replay
  the original key. No new Launch, purchase, stage operation, or volume key is
  created, and durable authorization history rejects every later replay grant.
- Tencent/TKE Runtime manifests encode Launch operation identities into valid
  Kubernetes label values while retaining the exact Runtime operation ID in
  annotations. If the original Runtime apply was rejected before creating any
  resource, the existing Resume route may require authoritative absence and
  replay that same Runtime operation key once; it does not create a new Launch
  or alternate recovery path. Static Tencent CBS PVs use the CBS CSI driver's
  topology zone key for both manifests and strict readback. An identity-valid
  Runtime that exists but is not Ready remains typed provider-pending so the
  original Launch can converge by readback instead of being marked failed.
  When that exact Runtime is blocked only by its old immutable image, the same
  Resume route can bind an administrator-approved replacement to the currently
  deployed Workspace image and replay the original Tencent Runtime child once.
  The Launch, attempt, Fabric operation, Runtime ID/service, request hash,
  idempotency key, debit, CBS, attachment, Secret, and downstream Receipt path
  remain unchanged. Focused route and Tencent vertical tests cover the
  production-shaped `3 -> 6` readback window, typed proof journal, zero-write
  rejection matrices, delayed READY convergence, and no second apply.
- An administrator-only Runtime image replacement API now upgrades an existing
  succeeded Workspace without relaunching it. Control Plane persists an
  idempotent operation after resolving the successful Launch and live Runtime,
  binds the requested digest to the protected immutable `OPL_WORKSPACE_IMAGE`,
  and dispatches a typed Fabric capability. Fabric and the Tencent adapter
  recheck the complete owner chain, journal/CAS the provider mutation, patch
  only the existing `workspace` container image, and read the Runtime back.
  The Launch, URL, Compute, CBS, Attachment, Secret, billing Receipt, and
  Runtime identity remain unchanged. Focused Control Plane, Fabric, HTTP, and
  Tencent tests pass; no production Instance deployment or acceptance receipt
  has been executed for this capability.
- Tencent prepaid preflight requires Candidate-bound schema-v3 evidence that
  the live Fabric identity has the active system policy
  `QcloudCVMFinanceAccess`. Both compute and storage checks re-read the current
  STS identity and CAM attachment and fail before Debit when that proof is
  absent; a successful price inquiry is not payment proof.
- Source now keeps Tencent compute provisioning typed and pending for a
  persisted ten-minute window. A Ready Machine with recoverable ownership
  becomes compute-only `ownership_pending`; one durable same-key continuation
  discovers that Machine and claims CVM/Node ownership without a second
  NodePool scale. The existing administrator Resume route can return only the
  historical matching compute-unknown operation to this same Reconciler after
  authoritative `provider_provisioning` or `ownership_pending` readback. One
  terminal failed compatibility replay can be replaced exactly once by a new
  single-use administrator authorization without changing the original stage,
  attempt, Fabric operation, resource binding, or idempotency key. Local Fabric,
  Control Plane, route, and PostgreSQL tests cover the original attempt, delayed
  readiness, fail-closed conflicts, downstream stages, and one Receipt.
- Candidate A implements a narrow audited repair for the historical schema-3
  Launch defect whose only missing canonical fact is `specDigest`. Fabric
  strictly reads the persisted preflight binding; Control Plane previews the
  exact two-field semantic change and atomically writes only the repaired
  operation result plus one audit event under CAS and idempotency protection.
  Local focused and full PostgreSQL gates pass. No production repair has been
  executed, and the repair intentionally preserves `stage=debit` and
  `status=manual_review`; business-state convergence remains separate work.
- Workspace Delete is permanent under the current contract. It removes the
  owned runtime/resource path, performs no refund or wallet mutation, and writes
  `workspace.deleted.v1` after authoritative cleanup.
- Fabric now exposes typed Local-Docker and Tencent/TKE Runtime delete
  observations, and Control Plane keeps Delete fail closed until the typed
  Runtime, Secret, and resource facts confirm absence. Asymmetric PV/PVC
  residue and the complete Tencent Gateway Secret deletion path remain open
  outcomes rather than proven cleanup.
- Fabric persists `compute_pool_key`, FIFO pool-head ownership, lease owner, and
  lease expiry for compute allocation. Memory and PostgreSQL-focused source
  tests cover same-pool serialization, cross-pool parallelism, database-clock
  fencing, and crash recovery. Current Control Plane admission remains one
  in-flight Launch, so a higher-concurrency Tencent receipt is trigger-gated.
- A successful Launch Receipt is now projected to the Workspace inside the
  owning PostgreSQL CAS transaction. Exact replay preserves the same Receipt;
  a different Receipt or mismatched Account/Workspace identity fails closed.
- Control Plane exposes a protected, read-only disposable Launch reset preview.
  It composes redacted Control Plane, Fabric/provider, Kubernetes, Sub2API, and
  Ledger observations into a deterministic plan digest. It performs no reset,
  refund, Key deletion, provider deletion, Ledger append, or terminalization.
- Workspace renewal authorization is persisted and exposed through Control
  Plane and Console. Expired-Workspace reactivation and live renewal evidence
  are not implemented end to end.
- Fabric persists provider-neutral Launch stage bindings and request hashes
  before provider writes. Local-Docker and Tencent/TKE project the fixed
  Workspace WebUI `http/3000` compatibility fact.
- Fabric operation reads use bounded lookup and cursor pagination, and repeated
  job heartbeats reuse one mutable operation identity. A fresh sealed external
  scan has not yet closed the earlier resource-history finding.
- Fabric application callers no longer retain the broad `OperationStore`.
  Machine Ownership, Compute Pool admission/leases, Jobs, Runtime operations,
  resource locks, Compute claim recovery, Workspace Launch stages, provider
  mutation journaling, operation history, and resource operation outcomes use
  capability-specific ports. One in-memory or PostgreSQL backend still owns the
  same tables and is adapted into those ports at service composition; no schema,
  persistence authority, or public behavior changed.
- Fabric application callers also no longer retain the broad `Provider`.
  Provider Profile, Compute, Storage Volume, Attachment, Gateway Secret,
  Workspace Runtime, image policy, Launch plan, monthly preflight, and readiness
  use capability-specific ports. Optional Launch, repair, destroy-readback,
  provider-facts, and diagnostic capabilities are resolved once at service
  composition. Local-Docker and Tencent/TKE remain complete adapters behind one
  composition contract; no adapter object, public contract, or provider behavior
  changed.
- Fabric Workspace Launch Stage operation transitions now have one internal
  `launchStageEngine` owner. The Engine alone receives the Stage mutation Store,
  Launch Provider, runtime-image revision capability, provider-mutation journal,
  Machine Ownership port, and clock. `Service` retains Preflight admission
  persistence and thin Ensure/Read/request facades; Stage claim, replay,
  Provider mutation/readback, diagnostic persistence, failure, and CAS
  convergence no longer execute on the broad Service receiver. Existing
  Local-Docker and Tencent/TKE behavior, HTTP contracts, payloads, and database
  schema are unchanged.
- Fabric non-secret Workspace Runtime status reads now have one internal
  `workspaceRuntimeReadEngine` owner. It receives only the provider Runtime
  status reader and the persisted Runtime identity-candidate reader. Public
  status and observation classify provider facts only after exact persisted
  identity matching and redact passwords; Runtime create/repair response-loss
  recovery reuses the Engine's read-only Provider port without changing its
  operation CAS. Runtime mutations, Gateway Secret reads, and delete-residue
  observation remain separate capabilities with their existing owners.
- Control Plane `WorkspaceLaunchReconciler` now types its three owned state
  axes with shared contracts: the business cursor uses `contracts.Stage`, the
  Launch operation status uses `contracts.LaunchStatus`, and the current Stage
  observation uses `contracts.StageState`. The strict schema-v3 decoder remains
  the single ingress from generic persistence rows and the row encoder remains
  the single egress; unknown values and invalid Stage/status/observation
  combinations fail closed. Generic persistence rows, heterogeneous canonical
  Stage facts, and child authorization/claim statuses remain explicit
  serialization boundaries. Existing schema-v3 JSON, HTTP payloads, database
  schema, request hashes, idempotency keys, CAS/Workspace projection, and
  external side-effect order are unchanged.
- Console Workspace Launch now has one `useWorkspaceLaunchController` owner for
  browser form and confirmation state, pricing catalog and previews,
  idempotency intent, interrupted-operation recovery, bounded polling,
  authoritative Workspace readback, and Launch-specific busy state. The broad
  Console controller retains Session, Router, toast, wallet/Workspace reads,
  and route orchestration, and composes the Launch owner through narrow request
  guards. Launch views consume `WorkspaceLaunchController` instead of the broad
  controller; existing Control Plane APIs and DTOs, `30 x 10s` polling,
  customer-visible behavior, navigation, and request order are unchanged.
- Console Workspace access Secrets now have one
  `useWorkspaceSecretController` owner for the mutually exclusive Runtime
  credential and Workspace Gateway Key projections, the `60,000ms` timer,
  request invalidation, independent busy facts, and Runtime rotation intent.
  Session, Router, Workspace sources, authoritative detail refresh, and toast
  remain in the root and enter through narrow dependencies; the access view
  consumes `WorkspaceSecretController`. Route, Session, Workspace, logout,
  reset, and unmount changes invalidate pending completions and clear the
  projection and busy state. Mismatched credential/Key identities fail closed,
  while valid API payloads, request order, Secret lifetime, copy behavior, and
  customer-visible text remain unchanged.
- Console operator Wallet Adjustment now has one
  `useWalletAdjustmentController` owner for the durable adjustment operation,
  response-loss idempotency intent, manual-review recovery, operation readback,
  account projection refresh, and Wallet-specific busy/reset state. `AdminPages`
  consumes that typed capability; it requests account refresh through the
  Operator Account capability's narrow port. Wallet balance, Receipt,
  audit, and Sub2API facts remain owned by their services, and the exact
  operation/recovery API identities and customer-visible behavior are unchanged.
  Focused model tests cover intent reuse, input/account conflict, and
  recovery-key derivation.
- Console Operator Account now has one `useOperatorAccountController` owner for
  the account projection and pagination, normalized provision intent and
  operation, per-account disable and purchase-eligibility intents, busy claims,
  route/Session/request freshness, authoritative paged readback, and reset.
  `AdminPages` consumes the typed capability and keeps only dialog/form state;
  Wallet Adjustment receives only its `refresh` port. Command identity or
  target mismatches and stale projections fail closed, while a same-semantic
  response-loss retry reuses the original client key. This does not claim that
  Control Plane provision or disable already implements strict server-side
  idempotent replay. Focused model and browser tests cover normalization,
  identity/readback invariants, late list results, route exit, response loss,
  stale readback, and Session reset.
- Console Operator Announcement Lifecycle now has one
  `useOperatorAnnouncementController` owner for the operator projection,
  normalized create intent, per-announcement publish/withdraw intents, busy
  claims, route/Session/list freshness, authoritative readback, and reset.
  Admin overview and announcement routes consume the typed capability and keep
  only dialog/form state. Command identity, target status, schedule, and final
  projection mismatches fail closed; response-loss retries retain the original
  key until readback succeeds. Focused model and browser tests cover stale list
  completion, route exit/re-entry claims, response loss, readback mismatch, and
  Session reset. Customer announcement reads remain an independent lifecycle.
- Console Customer Announcement Read now has one
  `useCustomerAnnouncementController` owner for the active published
  projection, Overview/list query scopes, per-announcement unresolved read
  intents, the in-flight claim and busy projection, Session-confirmed receipt
  IDs, route/Session/query freshness, typed receipt validation and projection,
  current-scope refresh, and reset. Customer overview and announcements pages
  consume the typed capability. Overview remains limited to 3 items and the
  list to 20. A valid
  Control Plane receipt completes the durable read fact; refresh failure cannot
  downgrade it, target absence is valid in a bounded active projection, and a
  visible `read=false` conflict cannot commit. Focused model and browser tests
  cover cross-scope late GET rejection, current-scope mutation refresh,
  response-loss key reuse across announcements, receipt/readback conflicts,
  route exit/re-entry, and Session reset with a stale completion. Control Plane
  remains the owner of published content and durable per-user read receipts.
- Console Support Mapping now has one `useSupportController` owner for ticket
  loading/error state, mapping intent and idempotency, busy state, local stale
  response invalidation, typed POST identity validation, and authoritative GET
  list readback. `ConsoleShell` consumes the narrow `SupportController`; root
  reset explicitly invalidates the capability. Existing `/api/support/tickets`
  payloads, response-loss retry behavior, and customer-visible text are
  unchanged. Focused model tests, source tests, browser acceptance, and logout
  safety evidence pass.
- Console Gateway Usage now has one `useGatewayUsageController` owner for the
  Key collection, selected Key, period/page, independent Usage and Summary
  remote state, and query freshness. The Customer usage page consumes the
  typed capability; the broad root retains only route, Session, error, and
  reset composition. Key/period changes discard late responses, an
  authoritative empty Key collection clears both projections, and Usage and
  Summary failures remain independent. Focused browser regressions are part of
  the ordinary local/PR gate.
- Console Billing/Receipt now has one typed `useBillingController` query owner
  for the billing view, Receipt list/detail remote state, selected Receipt ID,
  opaque cursor stack, independent list/detail freshness, and route/session/
  reset. Overview requests the latest 3 Receipts, while Billing requests 20 and
  owns cursor navigation and detail selection. Ledger remains the Receipt
  authority; Workspace terms continue to use the Workspace projection. The
  broad root retains only Session, Router, error, and aggregate route/reset
  composition for this capability.
- Console Workspace Delete now has one `useWorkspaceDeleteController` owner for
  the stable delete intent, Delete-specific busy and issue state, Session and
  Workspace freshness, and final paged absence readback. A command response,
  `workspace_not_found`, or local navigation never proves success by itself;
  only an available Control Plane projection with no matching Workspace clears
  the intent and navigates away. No refund or Wallet fact is inferred.
- Console Workspace Renewal now has one `useWorkspaceRenewalController` owner
  for the per-Workspace intent, busy/issue state, response validation, and
  authoritative list/detail readback. The command scheduling response is
  validated independently; readback success requires the authoritative
  Workspace projection to match the requested `autoRenew` value. Unknown
  results preserve the original idempotency intent across Workspace navigation.
  Delete and Renewal no longer share root mutation state and are cross-disabled
  only at their real page consumer.
- Console Workspace Gateway Budget now has one `useWorkspaceBudgetController`
  owner for the Workspace/Key-scoped intent, stable request signature,
  independent busy claim, mutation response validation, and Sub2API owner
  readback. It preserves unresolved intent across Workspace navigation and
  validates stable policy fields without treating live usage counters as
  resource-lifecycle state. The broad root retains only source composition and
  route loading. Typed monotonic leases serialize Renewal and Budget owner
  readbacks against their corresponding route source reads in both request
  orderings without coupling the two projections together or invalidating the
  independent Runtime projection. The ordinary local/PR gate runs the focused
  Delete, Renewal, Budget navigation and stale-order browser regressions.
- Portable Compose separates Ledger, Fabric, and Control Plane credentials,
  databases, and service tokens. The Local-Docker override grants Docker Engine
  access to Fabric only and requires an immutable Workspace image.
- Candidate tooling builds one `linux/amd64` + `linux/arm64` Cloud index and a
  checksum-bound installation bundle. Candidate manifests bind source, image,
  child manifests, assets, and workflow provenance without selecting an
  installation domain, Provider Profile, or Workspace image.
- Ledger owns the append-only Cloud Evidence Index for operation identity,
  Candidate identity, receipt identity, status, and redacted export links.
  Control Plane billing reconciliation uses one transaction and the same
  purchase-admission lock as Workspace Launch; a committed mismatch blocks new
  purchases without changing Ledger's evidence ownership.
- The active machine contracts are Candidate identity, Distribution identity,
  Control Plane/Fabric Launch request hashing, and the Workspace Runtime ABI.
  Other current behavior is owned by source, public APIs, schemas, and focused
  tests.

## Local Runtime Evidence

Separate Local-Docker runs on 2026-08-19 exercised both ownership modes on
`linux/arm64`:

- `customer_owned + local-docker` created two independent running Workspaces,
  preserved them across control-service restart, and completed a real model
  request whose Sub2API usage increased by exactly one request.
- `platform_owned + local-docker` created one prepaid Workspace, one Workspace
  Key, one `52,580,000` USD-micros debit, and one linked purchase Receipt. A
  failed first Runtime was repaired by replacing only that Runtime while
  retaining the confirmed Key, debit, compute, storage, attachment, and Secret.
  Exact replay did not duplicate those resources or the Receipt. The repaired
  Workspace completed a real model request and survived Control Plane restart.

The retained evidence is outside Git under
`/Users/huangrende/Desktop/opl-cloud/evidence/2026-08-18-v2` and
`/Users/huangrende/Desktop/opl-cloud/evidence/2026-08-19-platform-owned-repair`.
Tokens and login cookies are not retained.

These runs do not form one exact-current clean-host create/delete journey. They
do not prove final resource and Key absence, zero Delete wallet mutation, and
the deletion Receipt for the same Candidate.

## Distribution Evidence

`v0.1.7` is the only retained public Product Release. It was published from
product SHA `a59bde68397528186a5220f73195fa1f3eda311b` as multi-architecture
index `sha256:e64504731f8b61c0864cf59faa647a1150e8a2a5eada34b26faf3a5487d28e8f`.
Its five installation assets match their API digests and `SHA256SUMS`.

That proves the public bytes of `v0.1.7`; it does not prove current `main`, a
clean installation, or medopl production readiness. Current source separates
Candidate construction, qualification admission, public mutation, and public
readback, and promotes the admitted digest without rebuilding it. No hosted run
has yet exercised that path for one current qualified Candidate, so this remains
source capability rather than evidence of a successor Product Release.

## Instance Evidence

`opl-instance-medopl` owns the medopl profile, production workflow, Secrets,
deployment, verification, rollback, and receipts. Retained evidence shows an old
Candidate reached a successful deployment mutation and later standalone
deployment verification. The latest recorded generic admission attempt was
blocked at customer login with HTTP `503`.

There is no receipt set for one current portable Candidate that proves generic
admission, a normal purchase, post-activation Runtime/provider/billing readback,
an executed and verified rollback, and `workspace_verified`. Cloud therefore
does not claim current Instance qualification or production readiness.

## Repository Security Evidence

The latest retained GitHub readback reports private vulnerability reporting,
Dependabot alerts and updates, secret scanning and push protection, full-SHA
Actions pinning, strict required `validate` and `dependency-review` checks, and
force-push/deletion protection on `main`. Secret validity checks were still
reported disabled after an attempted settings change.

Current source includes request-size limits, separate Control Plane and runner
transport identities, scoped Fabric capabilities, immutable Local-Docker image
admission, same-origin browser login admission, and bounded Fabric operation
history. These are source controls; their external alert and scan state remains
separate.

## Readiness Summary

Source and local tests cover the current service boundaries, money and
idempotency rules, persistence paths, provider adapters, portable assets, and
Local-Docker lifecycle. The accepted public-beta target additionally requires
public registration, lifecycle recovery, operator/data/alert closure, and the
same-byte Candidate/Release path. Their current evidence and open outcomes are
projected through the A-N packages in [roadmap.md](./roadmap.md); none is
upgraded by an older Candidate or Instance receipt.

The evidence meanings are defined in [invariants.md](./invariants.md). A test,
document, Candidate, Release, or Instance receipt proves only the layer and exact
identity it names.
