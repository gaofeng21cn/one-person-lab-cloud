# OPL Cloud Durable Invariants

This document contains durable integrity, authority, security and settlement rules
that must survive refactors. Exact HTTP/gRPC fields, DTO definitions and workflow
code belong to source, `packages/contracts/**` and [spec/README.md](spec/README.md).

## Authority

- **Single Writer per Fact**:
  - `CloudIdentity` owns sessions, Tenants, membership and accepted-operation grants.
  - `Workspace` owns Workspace business identity, entitlements, plans, Launch/Delete/PlanChange Sagas and target authorization.
  - `Resource Catalog` owns compute/storage/product plans and pricing/plan-change policies.
  - `Capability` owns uploaded Package bytes, Package/WebUI catalog metadata and immutable reference claims.
  - `Build` owns three-input BuildJobs, input snapshots and immutable Agent OCI outputs.
  - `Runtime Control` owns approved Runtime Releases; `Serve` owns per-Workspace application delivery, current deployment selection, readiness evidence and API/Embed/Hosted UI access bindings.
  - `Fabric` owns provider-neutral compute, storage, attachment, Secret bindings, provider execution and authoritative resource readback.
  - `Gateway Integration` owns Cloud↔Sub2API and Cloud↔OCI-Destination mappings plus `gateway.key_bindings` scope coordination; external `Sub2API` owns effective balance, API Keys, routing and Token usage.
  - `Ledger` owns append-only financial/deletion receipts, reconciliation evidence and caller-owned opaque provenance.
  - `Console BFF` composes owner readbacks without business persistence; `Control Plane` retains only unmigrated legacy surfaces/history until consumers finish moving.
  - `opl-instance-medopl` owns domains, provider profile, production Secrets, deployment, rollback and installation receipts; Cloud keeps customer-business evidence custody and receipt validators.

## Physical Ownership

- Services under the single root `go.mod` (`module opl-cloud`) maintain strict runtime, store and PostgreSQL database-role isolation per owner schema (CloudIdentity and Gateway Integration share one deployment unit while retaining separate Tenant and Gateway databases).
- Cross-owner communication uses typed gRPC/protobuf or authenticated owner HTTP (`Console` → same-origin BFF `/api/v2/*` or retained `/api/*`); an unavailable owner fails explicitly (`unavailable`) and never falls back to another writer or guessed zero values.
- Shared infrastructure in `services/internal/` remains policy-free and serves at least two real consumers.

## Identity And Secrets

- Protected reads and mutations verify the live authenticated session, subject mapping and tenant/role authorization; operator visibility never grants customer data ownership.
- Raw passwords, API Keys, tokens, provider Secrets and private downstream bodies never enter URLs, logs, browser storage, OCI images or Ledger payloads. Secret reveal uses a transient, non-stored, owner-authorized path.
- `gateway.key_bindings` stores only opaque binding IDs referenced by Workspace model configs; ordinary model updates reconcile approved model scopes on the existing key binding through `Serve` and `Fabric.RebindSecret` (at most one active Secret binding per runtime purpose).
- Privacy redaction on a receipt may hide fields from a reader but never bypasses or widens the underlying record's authorization fence.

## Money

- Customer prices, wallet debits and refunds use exact `int64` / bigint USD micros serialized as decimal strings at JSON boundaries; floating-point arithmetic is forbidden. Provider cost is reconciliation evidence, not a customer price formula.
- Sub2API rejects insufficient balance atomically (no debit clamping). Every financial write binds a stable `Idempotency-Key` and canonical request digest; unknown outcomes never trigger automatic duplicate debits or speculative refunds without prior owner readback.
- Business refunds belong to confirmed original charges under atomic reservation guards so cumulative completed plus pending refunds never exceed the original charge. Workspace deletion and customer refund are separate steps: automatic refund requires verified resource absence plus Ledger deletion receipt.
- Receipt persistence failure retries only the receipt, never repeating a confirmed debit, refund or provider mutation.
- A refund reservation freezes its money facts (canonical redeem code, amount, original charge, adjustment-attempted, and the refund's own amount, used hours, refund hours and order identity) when it is created. A later executor dispatches against those recorded facts and never recomputes them from a later clock, so an adopted or retried reservation cannot double-refund.

## Plan Change And Settlement (D17)

- **Upgrade (`upgrade_immediate`)**: Takes effect immediately upon confirmed resource/runtime execution and health readback while **preserving `currentPeriodStart` and `paidThrough`** (`E`). Billing does not reset a new month.
  - Using accepted prior monthly total `P_old`, target monthly total `P_new` (`USDMicros`), period timestamps `S < E`, and quote time `T` (`S <= T < E`), where `D = E - S > 0`, `R = E - T`:
    `upgradeCharge = ceil(max(P_new - P_old, 0) * R / D)`
  - Round up to 1 micro-USD **once** at the final step using arbitrary-precision integer math (no per-component ceil summing, no fixed 720-hour shortcut). If `P_new <= P_old`, charge is `0`.
  - Supplemental charges carry exact coverage `quote_at_ms..period_end_ms`; if an upgraded Workspace is later deleted before `E`, refundable remaining supplement rounds **down**:
    `unusedSupplement = floor(confirmedSupplement * max(E - deleteConfirmedAt, 0) / (E - quoteT))`
- **Downgrade (`downgrade_next_period`)**: Preserves current plan, paid fee and running resources until `E`; queues one `scheduled` change effective at `E` billed at the accepted target plan price. Prepaid storage shrinking is forbidden where the provider (`tencent-tke` CBS) does not support disk shrink, and mixed up/down transitions must be split into explicit directional requests.
- **Mutual Exclusion & Failure**: At most one active/scheduled PlanChange exists per Workspace. Payment or resource `unknown` freezes state until owner readback; unrecoverable partial infrastructure changes (e.g., expanded disk) preserve actual Fabric state and fail closed without charging unfulfilled target plans.

## Error & Recovery Handling

- Synchronous and asynchronous errors return structured machine codes (`Validation`, `Unauthenticated`, `Forbidden`, `VersionConflict`, `IdempotencyConflict`, `ExternalOutcomeUnknown`, etc.); clients branch on canonical error codes rather than parsing upstream strings.
- `awaiting_confirmation` / `unknown` means unverified outcome, never `failed` or resource absence. Response-lost retries reuse the exact `Idempotency-Key` and payload; polling honors `Retry-After` / `pollAfterSeconds` and stops on terminal states.

## Workspace Lifecycle & Application Delivery

- **Two Approved Combinations**: New deliveries select either **Default OPL App** (`opl_app(runtimeVersionId)`) or **Custom Built Agent** (`agent(capabilityVersionId)` requiring `packageVersionId` + `runtimeVersionId` + independent approved `webuiVersionId`). Half-selected Agent combinations are rejected. Legacy `resource_only` orders retain only existing obligations.
- **Immutable Artifacts & Bindings**: Executable images use exact `host/namespace/repository@digest`; tags (`latest`) are refused at deployment. Data bindings and deterministic isolated browser origins key on stable application/binding identity (never on mutable tags or individual attempts), and tenant data lives on explicit persistent storage rather than ephemeral layers.
- **Provider & Prepaid Destroy Fences**: Every external mutation persists an atomic epoch / one-send fence before dispatch. TKE Machine detachment uses retain mode, and prepaid CVM/CBS final release requires fresh absence plus explicit identity/ownership verification. Unknown RPC outcomes require owner readback (`destroy-status` / journal / audit) before any retry or reconciliation.
- **Expiry Enforcement**: Serving data planes enforce owner entitlement verdicts fail-closed; a running/ready container never overrides an expired or suspended Workspace verdict.

## Release & Environment Isolation

- Release qualification separates layers cleanly: `code-complete` (source/tests/gates pass) -> `pilot-ready` (approved Gateway/Runtime/provider/browser proof) -> `production-proven` (immutable revision deployed & read back on Instance).
- Production artifacts exclude test fixtures, seeds and mock hooks; production credentials stay inside bounded Instance/publish workflows and never enter Console, Build workers or ordinary logs.
