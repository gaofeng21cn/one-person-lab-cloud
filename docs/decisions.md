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
- **Slim Go-only runtime image & single-binary packaging**: `Dockerfile`
  compiles **4** stamped Go binaries in a single shared `go-build` stage
  (`/usr/local/bin/opl-cloud` from `cmd/opl-cloud`, plus `opl-tencent-provisioner`,
  `opl-node-image-retire` and `opl-owner-probe`) and **12** BusyBox-style
  symlinks (`opl-cloud-composition` and every `opl-<service>` -> `opl-cloud`),
  packaging `debian:bookworm-slim` without Node.js or `node_modules` while baking
  `/app/dist` and `/app/contracts/opl-cloud-owner-topology.json` and retaining
  `CMD ["/usr/local/bin/opl-control-plane"]` for existing deployment profiles.
- **Single-binary in-process backend convergence (P1)**: `opl-cloud-composition`
  runs all 11 declared domain modules (`services/*/module`, `apps/console-bff/module`)
  in-process as concurrent goroutines inside **1 OS PID / 1 Go runtime** via
  `composition.InProcStarter` with strict contract-allowlisted
  `owneridentity.ModuleEnv`. Internal owner-to-owner gRPC traffic uses zero-copy
  `owneridentity.InProcTransport` (`InProcAuthInfo` unforgeable caller identity +
  bearer token + `CloudIdentity` authorization) instead of intra-pod loopback
  TLS handshakes, while retaining external TCP+mTLS/HTTP listeners for health
  probes, reverse proxy and Kubernetes Services, preserving separate per-owner
  PostgreSQL databases/roles, and keeping `services/control-plane` in-process as
  module 11 until v0.1.8 retirement obligations close.
- **Unified 3-container portable Compose**: `deploy/portable/compose.cloud.yaml`
  standardizes single-host portable installations on 3 containers (`postgres`,
  single-PID `cloud-backend` running `opl-cloud-composition`, and `cloud-local-v2`
  proxy) instead of 11 separate backend service containers.
- **Console SPA ownership**: `apps/console-bff` serves the compiled Console SPA
  static bundle (`/app/dist`) on `/` for the `cloud` Console identity so
  Console static delivery no longer depends on the retained `services/control-plane`.

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
- **Account Purchase-Eligibility Policy Ownership**: `CloudIdentity`
  (`services/gateway-integration`, `tenant.workspace_purchase_eligibility`) is
  the canonical owner of new-Workspace purchase eligibility per Gateway
  subject (`actor_id`). Platform administrators grant or revoke eligibility
  through the Console BFF route
  `POST /api/v2/operator/accounts/{actorId}/workspace-purchase-eligibility`
  backed by `TenantProductService.SetWorkspacePurchaseEligibility`, which
  commits the policy row, audit event and idempotency record in one
  `opl_tenant` transaction; customer activation persists the initial eligibility
  there as well. The native Workspace owner gates every new purchase directly
  through `CloudIdentityAuthorization.ReadWorkspacePurchaseEligibility`
  (`services/workspace/internal/launch/purchase_eligibility.go`) over its
  existing gRPC channel, retiring the transitional Control Plane HTTP reader
  `GET /internal/cloud/workspace-purchase-eligibility` and
  `OPL_CONTROL_PLANE_WORKSPACE_TOKEN`. Missing rows for non-admin subjects fail
  closed (`false`). The legacy Control Plane route
  `POST /api/operator/accounts/{accountId}/workspace-purchase-eligibility`
  remains only for the retained legacy Console build until Control Plane
  retirement.
- **Private Software Package, Link Relay & Admin Delegation Ownership**:
  All Cloud-governed Gateway-user permissions follow one uniform fail-closed
  allowlist model inside `CloudIdentity` (`opl_tenant`): restricted Software
  Center packages (`tenant.package_entitlements`, keeping public packages
  anonymous on GitHub with zero client-side GHCR secrets and two-stage
  Cloud/COS delivery), OPL Link Relay forwarding
  (`tenant.link_relay_entitlements` checked only at control-plane registration
  and host enrollment routes, with existing active Gateway users explicitly
  seeded into the allowlist during initial rollout for zero-config UX), and
  planned runtime operator delegation (`tenant.platform_admins` backed by
  Console `/operator` with audit + idempotency + last-admin lockout protection,
  keeping `OPL_PLATFORM_ADMIN_SUBJECTS` as immutable bootstrap/break-glass root
  admins) ([software-package-entitlements.md](software-package-entitlements.md)).
- **Unified Companion Service Entitlements & Endpoint Discovery Ownership**:
  `CloudIdentity` (`opl_tenant`, `tenant.service_entitlements`) is the single
  authority for companion-service admission (`allowlist_only` or
  `active_gateway_default_with_overrides`), per-instance service-origin
  dispatch, and short-lived Ed25519 `service-grant` token issuance over
  `POST /api/v2/client/service-grant` and `/.well-known/opl-grant-jwks.json`,
  allowing companion services to verify grants offline and isolate tenants by
  immutable numeric `actor_id` without static email allowlists or client-side
  endpoint configuration ([service-entitlements-and-discovery.md](service-entitlements-and-discovery.md)).
- **Personal Gateway Key Write Ownership**: `Gateway Integration` owns the native
  personal Gateway Key create/update/reveal/revoke capability over the delegated
  Sub2API authority, projecting the same `PersonalGatewayKey` fact the personal
  reads use. The Tenant-managed `GatewayKey` contract is a different contract and
  is not reused for personal writes — it cannot express a group, an IP list, a
  quota or a rate limit. The public surface is the Console BFF, which carries the
  customer's whole instruction to that owner and projects its answer; the Control
  Plane's own `/api/gateway/keys` write routes are not yet migrated onto it.
- **Customer Gateway Balance History And Endpoint Ownership**: `Gateway
  Integration` owns the customer's own balance history and the public Gateway
  endpoint, because the Gateway keeps the spendable balance and the endpoint is a
  fact of the owner's own configuration. The owner resolves the subject from the
  caller's own verified identity binding (`gateway.identity_mappings`) and never
  from a request field, so a read can only return the caller's own money; the
  Gateway's two monetary record streams (`balance` and `admin_balance`) are read
  and merged by the owner, because an unfiltered upstream read would mix
  subscriptions and concurrency into the customer's USD. The public surface is
  the Console BFF, which projects the owner's answer. The Control Plane's
  `GET /internal/cloud/gateway/balance-history` and
  `GET /internal/cloud/gateway/endpoint` bridges are retired; its retained
  `/api/gateway/*` read routes stay only for the legacy Console build until
  Control Plane retirement.

- **Tenant Lifecycle Ownership**: `CloudIdentity` (`services/gateway-integration`,
  `tenant.tenants`) owns the Tenant lifecycle, because the owner's own fences
  already read that status: a session, an authorization and a Tenant read all
  require an active Tenant. Suspending a Tenant therefore suspends the
  customer, and the owner writes the status, the durable Operation and the
  audit evidence in one transaction before revoking the Tenant's live
  sessions. A personal customer owns exactly one Tenant, so this is the
  customer's account status rather than a second copy of it. The public surface
  is the Console BFF route `POST /api/v2/admin/tenants/{tenantId}/suspend`,
  which carries the platform administrator's command to the owner. The
  re-enable half is not claimed here: the published contract restores the
  Tenant first and then has the Workspace owner resume the Workspaces that
  suspension stopped, and that continuation is not implemented anywhere yet.
  The Control Plane's `POST /api/operator/accounts/{accountId}/disable` stays
  only for the retained legacy Console build until Control Plane retirement.

## Non-Spending Publication & Incremental Retirement

- **Non-Spending Release Admission**: Formal Cloud Candidate/Release
  qualification proves clean-host installation, `LOCAL_NO_CHARGE` lifecycle,
  and exact-source CI / business-entry readiness (`paidBusinessExecuted: false`)
  without executing live cost-incurring Tencent purchases; paid Tencent/TKE
  acceptance belongs to explicitly authorized protected Instance qualification.
- **Consumer-First Path Retirement**: Legacy Control Plane writers, routes and
  tests are retired only after live callers move and persisted customer/financial
  obligations retain explicit dispositions.
- **Lifecycle Execution Retirement Evidence**. Before removing the retained
  deletion, refund, or renewal executors, migrate their live callers and use
  an owner-scoped operation census to settle or transfer the obligations they
  can still advance. The census must identify the relevant operation types and
  the stage needed to determine terminality. A partial observation of the
  current owner API leaves the disposition of unobserved obligations unknown.
- **Retained Launch Executor**: The Control Plane continues accepted
  `workspace.launch.v2` operations carried forward from before the purchase
  cutover. Keep that execution path until those obligations complete or migrate.
  New purchases enter through the Workspace owner. A retained customer Key
  continuation uses the operation owner's Gateway credential.
- **Retained Application Deployment Executor**: Keep the shared Control Plane
  deployment worker, activation store and deployment wiring until a supported
  upgrade completes or transfers accepted pending and running deployment
  operations under their original operation and resource identities.

## Workspace Subscription Renewal

- **Renewal Is A Manual Continuation, Not A New Purchase**: A renewal continues
  the subscription a Workspace already owns. The Resource Catalog prices one
  month of that Workspace's own plan pair and binds the offer to it; the Fabric
  owner extends the prepaid period of the compute and storage the Workspace
  already runs, in place; the CloudIdentity owner grants a renewal obligation
  whose surface is separate from the launch and the deletion surface; and the
  Workspace owner accepts the offer, charges it once through the single Gateway
  wallet, and commits the renewed paid period. The provider's prepaid month and
  the Cloud's paid period are one fact seen from two sides, so both move
  together in one obligation.
- **The Renewed Period Is Anchored On The Paid-Through**: The renewed period
  continues the confirmed paid-through — the later of the paid-through and the
  moment of the renewal — so the customer keeps the days it already paid for
  and receives a full month on top of them. The billing anchor day does not
  drift, and the Catalog places no period itself: where the renewed period
  lands belongs to the owner of the subscription.
- **An Expired Workspace Is Not Renewable, And Is Never Charged**: A renewal
  whose paid term has already ended is refused before any charge. The provider
  extends its prepaid period from its own expiry date, so a month that starts
  after the term ended is a month the provider does not cover; charging for it
  would take the customer's money for compute nobody delivers. A Workspace that
  stopped at expiry therefore has no renewal path back to its paid state, and
  reviving it requires a separate owner-authorized path rather than a renewal.
  A renewal that is refused for any other reason — resources the provider has
  lost, a Workspace that is not active, a paid month the subscription already
  confirmed — is refused on the same boundary, before the charge.
- **The Charge Precedes The Provider, And The Provider Precedes The Period**:
  The customer is charged once, the provider is extended only after the Ledger
  confirms the charge of that exact renewal obligation, and the renewed paid
  period is written only after the provider confirmed the extension. A pending
  provider leaves the renewal pending rather than recording a period nobody
  paid for.

## Ordinary Source Development And CI

Ordinary source and documentation changes follow standard Git hygiene: narrow
branch off verified `origin/main`, focused behavioral tests, PR template summary,
and required PR CI checks. No external task form, phase-plan registration or
standalone `source-check` JSON ledger is required. Production mutations,
financial paths and Instance configuration retain explicit owner authorization.
