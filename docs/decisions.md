# Durable Decisions

This document synthesizes lasting architectural and product decisions. It does
not maintain chronological changelogs; completed implementation history belongs in
Git log.

## Repository And Authority Separation

- **Single Cloud repo vs external owners**: `one-person-lab-cloud` (`opl-cloud`)
  owns Cloud product architecture, Console UI/BFF, domain services, contracts and
  portable releases. `opl-instance-medopl` owns domains, Tencent/TKE profile,
  Secrets, deployment, rollback and installation receipts. External Sub2API owns
  user credentials, spendable wallet balance, API Keys, routing and usage.
  OPL Framework (`one-person-lab`), App (`one-person-lab-app`) and Studio
  (`opl-studio`) retain Host/desktop/runtime authorities.
- **Medical Agent Platform reuse**: Medical domains reuse OPL Cloud Workspace,
  Gateway, Fabric, Serve and Ledger rather than building a parallel platform
  stack.
- **Backend unit composition**: Domain write isolation does not require one
  container or Kubernetes Deployment per domain. Services may share a single
  composed deployment unit while keeping separate database roles, single-writer
  boundaries, idempotency fences and privileged BuildKit / running Workspace
  execution outside the ordinary backend trust boundary.

## Workspace Application Model

### Default OPL App And Optional Agent Delivery

- New Workspace delivery supports exactly two product combinations:
  1. **Default OPL App** (`opl_app(runtimeVersionId)`): uses an approved
     Runtime Release and its built-in UI straight through Serve without synthetic
     Package, BuildJob or CapabilityVersion rows.
  2. **Custom Built Agent** (`agent(capabilityVersionId)`): requires explicit
     exact `packageVersionId`, `runtimeVersionId` and independent approved
     `webuiVersionId` fixed by Build into one immutable OCI digest.
- Partial/mixed combinations (Package without approved WebUI, WebUI without
  Package, or attaching an independent WebUI to default App) are rejected
  explicitly. A provisioned Workspace may also hold zero applications.
- **Separate Workspace Identity**: Account may own 0..N Workspaces (`workspace_id`
  keyed). Data bindings attach to stable application identity rather than tags or
  attempts, and `host/namespace/repository@digest` is the sole executable image
  reference format.

### Application Origin & Tenant Container Repositories

- Each Workspace application binding receives a deterministic, non-overlapping
  browser origin derived from binding identity (reversible DNS encoding under
  the tenant root). Application origins never carry platform routes or cookies.
- Publisher custom OCI images push only to CloudIdentity-verified user/tenant-scoped
  destinations (`Gateway.ReadUserExternalIdentity` / `PUBLISHER_NAMESPACE_MISMATCH`
  enforcement), preventing cross-tenant image overwrite.

## Identity, Gateway & Workspace Key Boundary

- **Cloud Login Boundary**: Cloud login delegates identity verification to the
  existing Gateway/Sub2API credential boundary and binds the live CloudIdentity
  session; external User-to-Sub2API and User-to-OCI-Destination mappings stay in
  `gateway.identity_mappings` (keyed by verified external subject, never email).
- **Explicit Gateway & Secret Ownership**: `Gateway Integration` is the sole
  writer of `gateway.key_bindings` for Workspace-managed Gateway Keys and model
  scopes; `Fabric` holds explicit initial `BindSecret` and predecessor-checked
  `RebindSecret` authority (`one active binding per runtime purpose`). Routine
  Workspace model changes adjust approved scope on the existing Key rather than
  rotating keys; automated Workspace Key rotation is retired.
- **Delegated User Read Custody**: Personal Gateway Key/balance-history views and
  transitional Control Plane announcements/billing reads require live
  CloudIdentity proof and reciprocal subject bindings; CloudIdentity User
  delegated credentials never bypass Tenant/Role fences.
- **Account Purchase-Eligibility Policy Ownership**: The retained Control Plane
  operator endpoint
  `POST /api/operator/accounts/{accountId}/workspace-purchase-eligibility`
  (`services/control-plane/internal/server/routes_admin.go:126`) stays the only
  path that can change `Account.WorkspacePurchaseEnabled` after an Account
  exists; it commits the column and its audit event in one transaction through
  `ApplyWorkspacePurchaseEligibility`
  (`services/control-plane/internal/server/ent_state_store_identity.go:197`). The
  native Workspace owner gates every new purchase on the value it produces
  (`services/workspace/internal/launch/service.go:229` ->
  `services/workspace/internal/launch/purchase_eligibility.go:87`) by reading the
  Control Plane's `GET /internal/cloud/workspace-purchase-eligibility` reader
  (`services/control-plane/internal/server/cloud_workspace_purchase_eligibility.go:15`);
  no other owner has a writer for this fact. Removing the writer therefore
  requires a two-sided owner/caller migration — an Account-policy writer in the
  target topology plus the native Workspace consumer's read path — not a
  writer-only deletion, and the shared read contract
  `contracts.WorkspacePurchaseEligibility`
  (`packages/contracts/go/workspace_provisioning.go:39`) stays.
- **Personal Gateway Key Write Ownership**: `Gateway Integration` owns the native
  personal Gateway Key create/update/reveal/revoke capability over the delegated
  Sub2API authority, projecting the same `PersonalGatewayKey` fact the personal
  reads use. The Tenant-managed `GatewayKey` contract is a different contract and
  is not reused for personal writes — it cannot express a group, an IP list, a
  quota or a rate limit. The public surface is the Console BFF, which carries the
  customer's whole instruction to that owner and projects its answer; the Control
  Plane's own `/api/gateway/keys` write routes are not yet migrated onto it.

## Non-Spending Publication & Incremental Retirement

- **Non-Spending Release Admission**: Formal Cloud Candidate/Release
  qualification proves clean-host installation, `LOCAL_NO_CHARGE` lifecycle,
  and exact-source CI / business-entry readiness (`paidBusinessExecuted: false`)
  without executing live cost-incurring Tencent purchases; paid Tencent/TKE
  acceptance belongs to explicitly authorized protected Instance qualification.
- **Consumer-First Path Retirement**: Legacy Control Plane writers, routes and
  tests are retired only after live callers move and persisted customer/financial
  obligations retain explicit dispositions.
- **Retired Execution Keeps Its Money Obligations Open**: Withdrawing a Control
  Plane execution engine does not withdraw the money obligations it owned. Every
  persisted refund or wallet obligation keeps an explicit disposition — settle,
  transfer, or keep the executor — and source withdrawal is not installed
  adoption.
- **Renewal Engine Withdrawal**: The retained Control Plane monthly
  renewal/expiry execution engine and its worker are withdrawn. The withdrawal
  removes the executor of a `workspace.renewal` operation and of a carried-forward
  `autoRenew` authorization — not their purchase, debit, refund and
  retained-history evidence, and not the readers that still report them. A
  renewal operation an earlier release left non-terminal therefore stays a
  visible obligation instead of being silently completed or dropped.
- **Application Runtime Write Path Withdrawal**: The Control Plane application
  runtime convergence writer
  (`services/control-plane/internal/server/workspace_application_lifecycle.go`)
  is withdrawn with the delete and renewal executors that were its only
  callers. Its readers stay: the application runtime inventory the renewal
  recovery reader projects, the historical delete and renewal operation
  decoders and validators that read a persisted record, and the shared
  application deployment worker that still writes a runtime lifecycle. An
  application suspension or recovery that was accepted for a persisted record
  therefore stays a visible obligation instead of being silently completed or
  dropped.
- **Retained Launch Executor**: The original Launch worker stays as the executor of
  a non-terminal `workspace.launch.v2` operation. A fresh create is refused at the
  route fence and new purchases belong to the Workspace owner route, so the retained
  executor only continues an operation that was already accepted; an in-place upgrade
  from public v0.1.8 can carry such an operation forward in any non-terminal phase and
  no other retained command can advance it. Its customer-owned Key stage still
  requires the customer's own Gateway credential. Removing it would strand a
  carried-forward accepted operation with no executor — the same exposure the shared
  application deployment worker is retained for.
- **Retained Deployment Worker Until Upgrade Completion**: The shared application
  deployment Worker, its driver, the deployment activation store and the
  portable/local `OPL_WORKSPACE_APPLICATION_DEPLOYMENT_WORKER_ENABLED`
  declarations stay until a supported upgrade completes or transfers a retained
  installation's accepted pending or running deployment operations with their
  original operation and resource identities.

## Ordinary Source Development And CI

Ordinary source and documentation changes follow standard Git hygiene: narrow
branch off verified `origin/main`, focused behavioral tests, PR template summary,
and required PR CI checks. No external task form, phase-plan registration or
standalone `source-check` JSON ledger is required. Production mutations,
financial paths and Instance configuration retain explicit owner authorization.
