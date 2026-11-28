# OPL Cloud Current Status

Owner: `opl-cloud`
Purpose: `replaceable_current_evidence_snapshot`
State: `current_snapshot`

This page records the current verified implementation boundary without copying
commit logs or acting as a release guarantee. Target ownership belongs to
[architecture.md](architecture.md), durable rules to [invariants.md](invariants.md),
and unclosed outcomes to [roadmap.md](roadmap.md). Git history preserves earlier
chronological snapshots.

## Current Capability Baseline

- **Single-Module Go Codebase & Persistence**: All Go services and Console BFF
  live under the single root Go module `go.mod` (`module opl-cloud`), backed by
  typed `database/sql` PostgreSQL stores and `services/internal/postgresmigrate`
  (`ent` ORM has been retired).
- **Dead-Code Boundary Over The Owner Packages**: The repository's dead-code
  boundary is the tool `tools/go/deadcode`, not a service artifact. It runs
  `golang.org/x/tools/cmd/deadcode` over the whole module from the repository
  root, keeps only production declarations in the packages
  `tools/go/deadcode/targets.txt` declares, and compares the unreachable set
  against `tools/go/deadcode/allowlist.txt`. The declared list covers the 54
  production packages under `services/` (including the 11 in-process
  `*/module` entry packages and exported `composition`) and `apps/console-bff`
  — every owner package, not one service — and the tool refuses to run when that
  list no longer equals the packages the tree contains, so a package cannot
  leave the boundary unnoticed. Allowlist keys are package-qualified
  (`<import path>.<Symbol>` for a package-level symbol,
  `<import path>.<ReceiverType>.<name>` for a method) and every entry carries
  the reason its declaration cannot simply be deleted. The HTTP surface scanners
  the owner-topology contract tests read live in `tools/go/http-surface-scan` and
  `tools/go/route-scan`, so retiring the retained Control Plane directory
  removes no source check.
- **Control Plane Retirement Residue Gate**: The retirement of
  `services/control-plane` is proved by one instrument,
  `tools/verify-no-control-plane.ts`, which runs in the `source` group of
  `npm run verify:local`. It declares thirteen assertions, one per retirement
  slice, and runs the assertions whose slice has arrived; the assertions of the
  slices that have not arrived stay pending and are reported as pending rather
  than as a pass. The declared stage table is checked against the assertions'
  `fromSlice` markers, so a check cannot be postponed by editing one copy of
  the plan. The gate scans the bridge implementation surface, the Local-Docker
  gateway label, the published portable deployment surface, the product image
  pipeline, the retired service directory, the local verification targets, the
  compose files, the retired Control Plane environment variables, the retired
  internal bridge prefix and every repository path that still names the retired
  source directory; it does not scan the documentation layer.
- **Single-Binary In-Process Backend & 3-Container Portable Topology (P1)**:
  `Dockerfile` compiles **4** stamped Go binaries (`cmd/opl-cloud` ->
  `/usr/local/bin/opl-cloud` plus CLI helpers `opl-tencent-provisioner`,
  `opl-node-image-retire`, `opl-owner-probe`) and **12** BusyBox-style symlinks
  (`opl-cloud-composition` and all 11 `opl-<service>` -> `opl-cloud`) in one
  shared `go-build` stage onto a
  Node-free `debian:bookworm-slim` runtime carrying `/app/dist` and baked
  `/app/contracts/opl-cloud-owner-topology.json`. Portable topology
  `deploy/portable/compose.cloud.yaml` runs a 3-container stack (`postgres` +
  single-PID `cloud-backend` running all 11 domain modules in-process via
  `opl-cloud-composition` over `owneridentity.InProcTransport` with retained
  external TCP+mTLS/HTTP readiness listeners + `cloud-local-v2` proxy);
  `apps/console-bff` serves Console SPA static assets on `/`, `opl-owner-probe`
  supports `OPL_GRPC_CERT_DIR`, and
  `tests/contracts/module-physical-boundaries.test.ts` enforces package/SDK
  isolation across all 11 Go services, `console-bff`, `cmd/opl-cloud`, and
  `*/module` `ModuleEnv` boundaries. Clean-host `local-v2` qualification and
  `opl-instance-medopl` TKE rollout of the converged P1 single-binary Candidate
  are verified / tracked separately, while formal public Release remains at
  `v0.1.8` (GitHub Releases).
- **Fabric Provider Account Resolution**: Fabric resolves the ordering Tenant of
  an accepted plan to the provider account that runs its package. A binding in
  the deployment-owned provider profile names a Tenant that runs in a particular
  account; a Tenant that no binding names runs in the profile's
  `defaultAccountId`, so onboarding a customer needs no profile edit and no
  restart. A profile that declares neither refuses that Tenant. The profile
  itself stays an `opl-instance-medopl` deployment fact.
- **Prepaid Provider Renewal At The Fabric Owner**: The Fabric coordination owner
  extends the prepaid provider period of one accepted resource set. It renews the
  compute and storage the Workspace already owns, in place, with their original
  provider identities and their original durable mutation claim, so a lost
  response can never buy a second resource. The provider is mutated only after
  the Ledger confirms the wallet charge recorded for that exact renewal
  obligation, exactly like the purchase path; an unconfirmed provider stays
  unknown and is retried against the same resources and keys, never reported as
  absent. A provider deadline that does not move forward, or that covers less
  than the prepaid months the Workspace bought, is refused instead of stored,
  and a zero-charge resource set — which owns no provider period — is refused.
  The charge, the renewed Cloud paid period, the authorization group of the
  renewal obligation and the recovery of a stopped Workspace belong to the
  Workspace and identity owners and are not implemented here.
- **Generic Agent WebUI Product Surface**: The runtime WebUI (`webui/cloud-agent`)
  presents a Simplified Chinese customer interface aligned with OPL Cloud visual
  tokens, with friendly expired-session and missing-model guidance, DOM-only
  safe Markdown answers with code-block and reply copy, failed-turn retry, new
  chat thread reset, and Markdown conversation export. Console and model-config
  links render only when the Runtime explicitly publishes an absolute
  `consoleUrl` (never guessed relative `/console/...` paths across hosts); that
  upstream publication remains an open item in [roadmap.md](roadmap.md).
- **Console / BFF / Control Plane Boundary**:
  - CloudIdentity routes (`/api/v2/*`) serve Workspace, Quote, Launch, Capability
    upload/build, Serve delivery, Gateway wallet/usage summary, Agent read-only
    MCP (`/api/v2/mcp`), quote review and Gateway session-backed customer
    reads (announcements and legacy personal billing through authenticated
    Control Plane bridges).
  - The publisher can read the structured build log: the contract's
    `GET /api/v2/builds/{buildId}/logs` route is served by the BFF and forwards
    to the Build owner, and the upload page shows the owner's log lines with
    their stage and severity. The log answers why a build stopped; the build
    status alone never carried that reason.
  - The Cloud personal Gateway Key surface is writable: the Gateway owner
    reports a personal Key as manageable and revocable (the Workspace's own
    managed Key is neither), and the Console offers create, edit, revoke and
    reveal against the BFF. The Control Plane's own `/api/gateway/keys` write
    routes are still live and still write Sub2API directly; they are retired in
    the follow-up slice, once the retained Control Plane Console build has
    stopped calling them.
  - The customer's Gateway balance history and public endpoint are read from the
    Gateway owner. The BFF calls `ListPersonalGatewayBalanceHistory` and
    `GetPersonalGatewayEndpoint` on `GatewayProductService`; the owner resolves
    the caller's own Gateway subject from its own verified identity binding
    before it reads the Gateway's two monetary record streams, so no request
    names a subject and no other service proxies the read. Control Plane's
    `GET /internal/cloud/gateway/balance-history` and
    `GET /internal/cloud/gateway/endpoint` bridges are retired. The retained
    `GET /api/gateway/balance-history` and `GET /api/gateway/endpoint` routes
    stay for the legacy Console build and are retired with the retained
    Control Plane.
  - A platform administrator can suspend a customer account through the Tenant
    owner: `POST /api/v2/admin/tenants/{tenantId}/suspend` is served by the BFF
    and forwards to `TenantProductService.SuspendTenant`. The owner writes the
    Tenant's status, its durable Operation and its audit evidence in one
    transaction, revokes the Tenant's live sessions and advances its permission
    version, so the owner's own fences refuse that customer afterwards. A
    personal customer owns exactly one Tenant, so suspending that Tenant is
    what disables the account.
  - A platform administrator can re-enable a customer account through the Tenant
    owner: `POST /api/v2/admin/tenants/{tenantId}/reenable` is served by the BFF
    and forwards to `TenantProductService.ReenableTenant`. The owner restores
    the Tenant's status, clears its suspension and advances its permission
    version in one transaction together with the durable Operation and the
    audit evidence, so the owner's own fences serve that customer again. A
    personal customer owns exactly one Tenant, so re-enabling that Tenant is
    what brings the account back. The revoked sessions stay revoked and the
    customer signs in again. The Workspace resume continuation the re-enable
    contract describes stays open, because the Cloud's tenant suspension pauses
    no Workspace; that is recorded in [roadmap.md](roadmap.md).
  - The historical OCI destinations are handed to the Gateway owner. At owner
    startup, `Gateway Integration` (`services/gateway-integration`) reads the
    frozen `tenant.tenant_repository_bindings` table and moves each destination
    into the single live mapping `gateway.identity_mappings`. A destination is
    attributed only to a Tenant that is `active` and has exactly one registered
    owner (`tenant.tenant_members` with `role='owner'` and no revocation); that
    owner's `actor_id` is the Gateway/Sub2API subject id, so the mapping's
    `actor_id` and `sub2api_user_id` are the same value. A `retired` reservation
    is reported as released, an inactive Tenant or any other owner count is
    reported as unattributable, and a live mapping that holds a different
    destination, is revoked, or lacks its destination verification is reported
    as a conflict. The pass is read-only on the frozen table, is idempotent, and
    never overwrites a live destination. It inherits the recorded reservation
    fact and does not re-verify the destination against the OCI registry.
  - The runtime fallback read of the frozen `tenant.tenant_repository_bindings`
    table is retired. The Gateway owner holds the only live destination mapping
    (`gateway.identity_mappings`), and its unique index
    `identity_mappings_destination` decides which actor may hold a destination:
    a second actor for a destination that is already held is refused with
    `AlreadyExists` on the login path and on the platform-admin command path.
    The runtime resolution path reads the owner alone and no longer consults the
    historical table, and the second reader that guarded a historical
    reservation is gone with it. The owner startup gate now holds one uniform
    rule: every historical row that records a destination the owner does not
    hold is a residual, whether the row was unattributable, conflicting, failed
    to move, or released without a holder. A released row still records a
    destination — no code in this repository ever wrote the `retired` status — so
    it is resolved against the owner's live mapping and refused to serve when
    nobody holds it. The gate names how many rows need an operator and shows the
    first ones. The frozen table stays readable for provenance and is never
    deleted.
  - Control Plane image-replacement creation/preview/worker commands and admin
    activation endpoints are retired; only the operator image-policy GET
    (`GET /api/operator/workspace-runtime-image-policy`), Fabric image
    replacement and `opl-node-image-retire` remain active.
  - Workspace Gateway Key rotation (`workspace.gateway_key.rotate`) creation,
    routes and child deployment constructor are retired; only the read-only
    `NewKeyID -> OldKeyID` resolution decoder and fences remain in
    `services/control-plane/internal/server/workspace_delete.go` and
    `workspace_store.go` so historical rotated Workspaces can still be deleted
    safely.
  - Workspace new-purchase eligibility has completed its two-sided migration to
    `CloudIdentity` (`opl_tenant`, `tenant.workspace_purchase_eligibility`).
    The Console BFF publishes
    `POST /api/v2/operator/accounts/{actorId}/workspace-purchase-eligibility`,
    persists initial eligibility during customer activation, and projects
    authoritative eligibility onto `GET /api/v2/operator/accounts`; the native
    Workspace owner reads eligibility directly via
    `CloudIdentityAuthorization.ReadWorkspacePurchaseEligibility`, retiring
    Control Plane's `GET /internal/cloud/workspace-purchase-eligibility` and
    `OPL_CONTROL_PLANE_WORKSPACE_TOKEN`. The retained Control Plane endpoint
    `POST /api/operator/accounts/{accountId}/workspace-purchase-eligibility`
    remains only for the legacy Console build.
  - The native Workspace owner accepts new purchases. The Control Plane
    [Launch executor](implementation/workspace-launch-recovery.md) continues
    accepted operations carried forward from before the purchase cutover.
  - A current-owner API observation established that retained lifecycle
    operations can be enumerated. Its coverage was partial, and operation
    identities and terminal phases still need a complete readback for the
    deletion, refund, and renewal retirement evidence tracked in the roadmap.
- **Renewal Pricing At The Catalog Owner**: The Resource Catalog prices one
  manual renewal offer: it binds the Workspace the renewal continues, keeps that
  Workspace's application and models untouched, prices the effective price
  version of the Workspace's own plan pair, and refuses an offer whose plan was
  revoked. A deprecated plan stays renewable so a customer who already paid for
  it is not stranded. The renewal itself — the charge, the renewed paid period,
  the provider's prepaid month and the recovery of a stopped Workspace — belongs
  to the Workspace owner and is not implemented here.
- **Retained Application Deployment**: The shared Control Plane worker advances
  accepted `workspace.application.deploy` children through predecessor
  suspension, activation and receipt. The source qualification covers retained
  v0.1.8 fixtures, response loss and restart against the same database.
- **Renewal Obligation Authorization At The Identity Owner**: CloudIdentity issues
  and honors the bounded grant of an accepted renewal obligation. A Workspace
  owner that accepted a `renewWorkspace` obligation carries exactly the renewal
  surface: the provider extension that extends the prepaid deadline of the
  compute and storage the Workspace already owns, the renewal offer it reads and
  accepts, the charge that funds it, and the receipt that records it. The renewal
  surface is separate from the launch and the deletion surface, so a renewal can
  neither provision new resources nor release the ones it renews, and a launch
  grant cannot reach the provider extension. The Workspace owner still has to
  request that grant and run the renewal itself.
- **Paid TKE Deletion Recovery**: The November 25, 2026 production readback
  confirmed resource and data deletion, Key closeout, the Ledger deletion
  receipt, and the original-charge refund on the same deletion operation after
  an operator released its recycled CBS. See Instance readback run `45639384732`.
- **Manual Workspace Renewal At The Workspace Owner**: The Workspace owner accepts
  one manual renewal of a paid Workspace and carries it as its own accepted
  obligation: it asks the identity owner for the renewal grant, accepts the
  renewal offer, observes the resources the renewal would extend, charges the
  customer exactly once, asks the Fabric owner to extend the prepaid deadline of
  those resources, and records the renewed paid period. The renewed period
  continues the confirmed paid-through, so the customer keeps the days it
  already paid for and receives a full month on top of them. A renewal whose
  paid term has ended, whose Workspace is not active, whose provider resources
  are gone, or whose paid month this subscription already confirmed is refused
  before any charge, so a customer is never charged for a renewal that cannot be
  delivered. The provider is extended only after the Ledger confirms the charge
  of that exact renewal; a pending provider keeps the renewal pending instead of
  recording a period nobody paid for. The Console entry that reaches this
  renewal belongs to the BFF and Console and is not implemented here.
- **What Remains Unproven Here**: Same-Candidate paid customer purchase
  execution on Tencent/TKE, J0 READY qualification bound to the latest
  Candidate, end-to-end custom Agent Package build-to-serve production delivery,
  PlanChange v2 runtime resize implementation, and installed qualification of
  the retained application deployment worker (tracked in [roadmap.md](roadmap.md)).
