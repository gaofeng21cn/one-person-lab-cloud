# OPL Cloud Roadmap And Current Gaps

Owner: `opl-cloud`
Purpose: `open_outcomes_and_acceptance`
State: `active`

This page tracks only open gaps and acceptance outcomes. Current evidence belongs
to [status.md](status.md); durable decisions belong to [decisions.md](decisions.md);
target ownership belongs to [architecture.md](architecture.md). Completed work is
removed here and preserved in Git history.

## Priority Outcomes

### Protected Same-Candidate Lifecycle & Paid Customer Acceptance

Close end-to-end customer lifecycle verification for the latest Cloud Candidate on
`opl-instance-medopl` Tencent/TKE:

- Default OPL App launch (`create -> quote -> debit -> Fabric provision -> Serve delivery -> readiness -> access -> renewal -> deletion -> refund/reconcile -> Ledger receipt`), plus custom Agent Package build-to-serve E2E on TKE.
- The renewal step of that chain is implemented end-to-end across Resource
  Catalog, Fabric, CloudIdentity, Workspace, Console BFF, and Console UI for
  active pre-expiry paid Workspaces, and still requires live Tencent/TKE
  acceptance verification. A Workspace whose paid term already ended is refused
  before any charge, because the provider extends the prepaid period from its
  own deadline and cannot cover a month that starts now.
- Verify that one customer deletion reaches confirmed CBS final release, compute
  absence, and settlement through the installed Fabric path without an operator
  releasing the recycled disk.
- Invoke protected J0 READY producer against the same Candidate digest and capture Instance-side profile-digest-bound installation summary receipts.
- Retire the remaining 11 zero-replica split Deployments on Instance only after fresh readback confirms zero active traffic.

### PlanChange v2 (D17 Resource Plan Resize & Downgrade Execution)

Implement the [D17 settlement and lifecycle specification](invariants.md#plan-change-and-settlement-d17):

- Resource Catalog typed `PlanChangePolicy` & exact micro-USD ceil/floor proration (`unusedSupplement` / `upgradeCharge`).
- Workspace `PlanChange` state machine (`upgrade_immediate` and next-period `downgrade_next_period` scheduled at `paidThrough`), cancellation CAS and billing calendar boundary enforcement.
- Fabric compute/CBS provider adaptation (`ResetInstancesType` / pool replacement + `ResizeDisk` without shrinking prepaid disks).
- Console quote preview, active-plan display and explicit error/pending surfaces.

### Build Three-Input W01 End-to-End Parity

- Ensure `packageVersionId` + `runtimeVersionId` + independent approved `webuiVersionId` claims, `build.artifact_confirmed.v1` Outbox/Inbox handoff, and BFF `ownerOperation`/receipt projections are fully exercised by production builds and protected TKE delivery without synthetic fallbacks.

### Agent Read Carrier Adoption

- Roll out Cloud's read-only MCP endpoint (`/api/v2/mcp`), same-origin quote review and 1-hour parent-session-bound helper credentials (`runtime/cloud-connect.md`) to an installed OPL App / Studio Host under the same CloudIdentity account.

### Retained v0.1.8 Deployment Worker Upgrade Qualification

Qualify an installed upgrade on the Instance-owned managed TKE or Local
Workspace overlay. For one retained Workspace, read back an accepted pending
or running `workspace.application.deploy` child that completes under its
original operation, resource and digest identities, with activation and receipt
recovered. An unresolved rotation parent still fences Workspace deletion.
Cross-installation transfer requires separate evidence if used.

### Manual Workspace Renewal

A paid Workspace that reaches the end of its period still has no renewal. The
Resource Catalog prices one renewal offer, the Fabric owner extends the
provider's prepaid month for the same compute and storage the Workspace already
owns, the identity owner grants the renewal obligation, and the Workspace owner
runs the renewal as one accepted obligation: charged once, extended by the
provider, and committed as a new subscription period that continues the
confirmed paid-through. What is still open:

- A Workspace that stopped at expiry does not come back. A renewal of an
  expired Workspace is refused before any charge — the provider extends the
  prepaid period from its own deadline, so a period that already ended cannot
  produce a renewed period the provider also covers — and a stopped Workspace
  therefore has no renewal path back to its paid state. Reviving it needs a
  separate owner-authorized path, not a renewal.
- A provider that refuses the extension after the charge leaves the renewal
  pending with the money already taken. No refund path exists for a renewal the
  provider would not deliver.

### Generic Agent WebUI Console And Model Links

The generic Agent WebUI guides customers when model access is missing or a
session expires, and renders console/model links only if the Runtime explicitly
publishes an absolute `consoleUrl` (plus `workspaceId` for the direct model
configuration path). Because the Console and Workspace entry run on separate
hosts by installation contract and the Runtime does not yet publish those
fields, closing direct cross-host navigation requires one named upstream fact
rather than a guessed relative URL.

### Private Software Package Entitlements & Controlled Delivery

Deliver the Cloud-governed restricted Software Center package authorization flow
defined in [software-package-entitlements.md](software-package-entitlements.md)
after Framework v2 Wave 1 single-writer source storage lands:

- `CloudIdentity` (`opl_tenant`): add `tenant.package_entitlements` and
  `tenant.link_relay_entitlements` (both fail-closed allowlists with atomic
  operator grant/revoke commands, audit/idempotency records, and an initial
  active-Gateway-user allowlist seed step for OPL Link Relay), plus planned
  DB-backed `tenant.platform_admins` delegation preserving
  `OPL_PLATFORM_ADMIN_SUBJECTS` as immutable bootstrap/break-glass roots.
- Console BFF & Operator UI: expose operator package, Link Relay, and
  delegated-admin governance alongside Workspace purchase eligibility, and add
  `GET /api/v2/software/entitlements` plus
  `POST /api/v2/software/packages/{packageId}/download` (Stage 1 server-only
  GHCR fetch with PVC `sha256` blob cache; Stage 2 private COS Presigned URLs).
- Cross-repo alignment: wire OPL Link control-plane admission against
  `CloudIdentity` (`403` -> `companion_access_denied` without touching the WS
  data plane), wire `@opl/pack` Gateway-session source consumption in
  `one-person-lab`, and simplify `opl-studio` Software Center to use the active
  OPL Gateway account by default while keeping public packages zero-login.

### Unified Service Entitlements & Endpoint Discovery

Implement Cloud-governed companion admission and zero-URL client bootstrap as
specified in [service-entitlements-and-discovery.md](service-entitlements-and-discovery.md):

- `CloudIdentity` (`opl_tenant`): add `tenant.service_entitlements` migration,
  admission evaluator (`allowlist_only` and
  `active_gateway_default_with_overrides`), Console `/operator` grant/revoke and
  override governance, and Ed25519 `grant_token` signing with JWKS publication
  (`GET /.well-known/opl-grant-jwks.json`).
- Instance Service Catalog & Console BFF: bind instance-owned `service_id ->
  { default_origin, audience, admission_mode }` configuration and expose
  non-browser `POST /api/v2/client/service-grant` (supporting direct Gateway
  login with optional 2FA challenge or existing Gateway Bearer session).
- Consumer Cutover: adopt `service-grant` verification and `actor_id` isolation
  across `opl-netfleet-cloud` / `site-sync` and unify `opl-link` admission on the
  same entitlement boundary.

## Workspace Key And Customer Read Boundaries

Complete migration of remaining transitional customer reads off legacy Control
Plane / delegated process-local custody onto canonical domain owners:

1. **Personal Gateway Key & Balance Reads**: Move key CRUD, usage/balance
   history and Gateway endpoint reads out of transitional delegated custody onto
   a verified first-class Gateway Integration boundary, retiring legacy `/api/*`
   Gateway panels once live callers migrate.
   - The key CRUD half has moved for the published Console: the Gateway owner
     owns the personal Key create/update/reveal/revoke capability and the Console
     reaches it through the Console BFF. The legacy `/api/*` write panels are
     retired in the follow-up slice, together with the retained Control Plane
     Console build's call to them; the Control Plane's read and reveal routes
     stay, because a non-browser consumer still reads them.
   - The balance history and endpoint half has moved for the published Console:
     the Gateway owner publishes the caller's own balance history and its public
     endpoint, and the BFF reaches both over the owner's typed client. Control
     Plane's `GET /internal/cloud/gateway/balance-history` and
     `GET /internal/cloud/gateway/endpoint` bridges are retired with it; the
     retained `/api/gateway/*` read routes stay until the retained Control Plane
     Console build is retired.
   - The customer's account status is bidirectional at the Tenant owner:
     suspending a Tenant suspends that customer because a personal customer
     owns exactly one Tenant and the owner's own fences refuse a suspended
     Tenant, and the platform administrator re-enables a suspended account
     through `POST /api/v2/admin/tenants/{tenantId}/reenable`, restoring the
     Tenant, its permission version and its durable Operation in one
     transaction. The Workspace resume continuation the re-enable contract
     describes is still open, and it is open for a concrete reason: the
     Workspace owner suspends a Workspace only for a paid term that expired or a
     resource that went absent, and it recovers those on its own sweep when the
     paid term is valid again and the resources are present. The Cloud's tenant
     suspension pauses no Workspace, so "the Workspaces that the tenant
     suspension stopped" is an empty set today, and the tenant-to-workspace
     coordination rpc (`TenantWorkspaceCoordination`) plus the
     `getTenantLifecycleOperation` readback that would carry the sub-operations
     are published but implemented nowhere.
2. **Billing & Announcements Extraction**: Retire retained Control Plane
   bridges (`/internal/cloud/legacy-billing/*`, `/internal/cloud/announcements`)
   once Cloud Ledger and Announcement owners serve authenticated CloudIdentity
   reads directly without losing historical user acknowledgements or receipts.
3. **Retained Control Plane Cleanup**: Progressively retire remaining legacy
   Launch/Policy/Admin surfaces once open persisted Workspaces converge and all
   non-browser Instance callers confirm zero dependency.
   - The Cloud purchase-eligibility policy writer and Workspace reader have
     migrated to `CloudIdentity` (`opl_tenant`). The legacy Control Plane
     endpoint `POST /api/operator/accounts/{accountId}/workspace-purchase-eligibility`
     stays only until the retained legacy Console build is retired.
   - The legacy Control Plane endpoint `POST /api/operator/accounts/{accountId}/disable`
     writes `control_plane_users.status` in the retiring service. The Cloud
     build suspends the customer through the Tenant owner instead; the legacy
     endpoint stays only until the retained legacy Console build is retired.
   - Preserve the retained deletion, refund, and renewal executors until their
     live clients have migrated and a current owner census can distinguish
     non-terminal operations from completed settlement. Include accepted
     refunds that still need their first execution and renewals whose expiry
     or Ledger settlement is incomplete.
4. **Module & DB-Role Coverage**: `tests/contracts/module-physical-boundaries.test.ts`
   now enforces package and SDK isolation across all 11 Go services under `services/`
   and `apps/console-bff`; extend PostgreSQL role-isolation checks to cover every
   extracted service and Console BFF.

## Backend Single-Binary Convergence (P1)

- Evolve `opl-cloud-composition` (currently 1 container supervising 11 separate Go
  processes over loopback gRPC/mTLS) into a single in-process modular Go binary
  (`opl-cloud`) preserving `services/<domain>/` package boundaries, per-owner
  PostgreSQL schema/role isolation, and external BuildKit/Workspace trust boundaries,
  while eliminating intra-container process orchestration, redundant Go runtimes,
  and intra-pod mTLS overhead alongside retiring `services/control-plane` and
  legacy root `compose.yaml`.
