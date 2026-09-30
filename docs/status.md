# OPL Cloud Current Status

Owner: `opl-cloud`
Purpose: `replaceable_current_evidence_snapshot`
State: `current_snapshot`

This page reports current implementation and the latest retained evidence. It
is not a work log. Target architecture lives in
[architecture.md](./architecture.md); open outcomes live in
[roadmap.md](./roadmap.md).

## Current Source And Evidence Scope

The documentation snapshot reviewed on September 30 uses canonical Cloud
source `26bf195e`. Current module paths, API routing and retained callers are
owned by [implementation architecture](implementation-architecture.md), not by
a dated migration-start inventory.

Console BFF and the Capability, Build, Runtime Control, Workspace, Resource
Catalog, Serve and Gateway Integration processes exist alongside the retained
Control Plane, Fabric and Ledger paths. The product Dockerfile includes all owner
executables and Build's buildx plugin. Tenant and Gateway have separate database
writers inside one Gateway Integration process; the Gateway surface is registered
only when its own database is configured. This is a source and packaging fact,
not proof that a concrete installation runs every process.

The cloud-identity Console creates and reads Workspaces through the BFF and the
Workspace owner. Its default-App selection uses an approved Runtime Release,
without a synthetic Package, BuildJob or CapabilityVersion; the alternative
Agent selection uses an accepted immutable Build output. Serve owns the current
application delivery, readiness and access fact. Shared Gateway/Billing panels
and the legacy Workspace path retain Control Plane APIs. The bundle defaults to
`legacy`, and its identity is baked into the image and recorded by its label; deployment
configuration cannot switch it. The [current Console read evidence](#cloud-identity-customer-workspace-read)
supersedes the earlier missing owner-read observation.

The [September 29 application decision](decisions.md#2026-09-29-default-opl-app-and-optional-agent-on-one-tencenttke-delivery-path)
owns the default-App/optional-Agent product target. Earlier resource-only orders
and three-service migration snapshots retain their original obligations and
provenance; they do not define the current new-customer flow or module inventory.
Earlier source handoff and serial-stage narratives remain in Git history. The
immutable source-check receipts retain their exact identities and evidence layers.

## Source Qualification

Retained verification includes a full local source gate with temporary
PostgreSQL and zero required database-test skips
([receipt](evidence/source-checks/2026-09-30-tke-serial-full-verification-green.json)),
the complete Console browser suite
([receipt](evidence/source-checks/2026-09-30-tke-serial-browser-suite-green.json)),
and canonical-main Cloud Qualification
([run 36649409916](https://github.com/gaofeng21cn/one-person-lab-cloud/actions/runs/36649409916),
[receipt](evidence/source-checks/2026-09-30-tke-serial-qualification-main-green.json)).
The Linux qualification exercises real Local application deployment, process
failure and recovery. These records prove the exact sources and environments
they bind, not an untested later Candidate or Instance.

Serve's default-App reserve/deploy/access tests use the real owner store
([reservation](evidence/source-checks/2026-09-30-tke-serial-serve-default-app-reservation.json),
[deployment](evidence/source-checks/2026-09-30-tke-serial-serve-default-app-deploy.json)).
The observed applied model version remains 0 until the executing runtime reports
a version; requesting a version is not evidence it loaded
([receipt](evidence/source-checks/2026-09-30-tke-serial-serve-model-applied.json)).
Built-Agent selection and durable-command recovery regressions have focused
[selection](evidence/source-checks/2026-09-30-tke-serial-agent-selection-mutual-exclusion-fix.json)
and [recovery](evidence/source-checks/2026-09-30-tke-serial-source-record-data-compatibility.json)
receipts and a subsequent green qualification run. The default-App Workspace
source resolves the approved Runtime Release rather than a Build artifact
([receipt](evidence/source-checks/2026-09-30-tke-serial-workspace-default-app-source.json)).
Product-image buildx and distinct cloud Console builds have focused
[image](evidence/source-checks/2026-09-30-tke-serial-product-image-buildx.json)
and [Console](evidence/source-checks/2026-09-30-tke-serial-console-cloud-mode-build.json)
evidence; they do not qualify a complete Candidate image or an Instance.

### Retained Qualification Limits

Gateway coordination's real PostgreSQL/gRPC tests use a stub Sub2API; they prove
owner intent/replay handling, not live wallet or managed-key acceptance
([receipt](evidence/source-checks/2026-09-30-tke-serial-gateway-coordination.json)).
Managed-key issuance remains blocked: inspected Sub2API has no approved
service-authorized issuer, the delegated user's authority remains in Control
Plane, and Serve's current caller identity differs from the Workspace peer
admitted by Gateway. The approved issuer/delegation and caller decision must be
resolved before the Gateway-owned Secret path can succeed
([boundary](evidence/source-checks/2026-09-30-tke-serial-managed-key-serving-boundary.json)).

Serve still executes through the old Fabric application HTTP adapter. I06 needs
the typed application-execution boundary; I07 needs the installation route object
and its conditional-revision CAS performer. The production RouteProvider and
Deploy-to-Fence/Activate orchestration are not implemented. The publisher's
`opl-model-config/v1` apply/readback wire is not carried by
`WorkspaceApplicationRuntimeInput`, so observed applied version 0 does not
establish a usable model configuration
([boundary](evidence/source-checks/2026-09-30-tke-serial-serve-exec-route-boundary.json),
[confirmation](evidence/source-checks/2026-09-30-tke-serial-i06-i07-blocker-confirmation.json)).
These limits, complete runtime model apply/readback and both product
combinations' Instance acceptance remain open in [roadmap](roadmap.md).

## Instance Installation And Publication Evidence

The latest retained September 30 Instance inspection found only the legacy
Control Plane, Fabric and Ledger installation, without the full owner set
([receipt](evidence/source-checks/2026-09-30-tke-serial-instance-install-gap.json)).
Cloud now supplies the [owner topology declaration](#cloud-owner-topology-installation-contract)
and the [HTTP/Console identity declaration](#browser-facing-http-surface-and-console-identity);
the Instance owner must materialize and qualify them. This document edit performs
no deployment, production access, provider mutation, customer migration or
financial action.

An exact Candidate still needs the repository-owner dispatch and protected
Instance qualification
([preconditions](evidence/source-checks/2026-09-30-tke-serial-candidate-instance-preconditions.json)).
The only public Product Release is `v0.1.7`; its five assets predate the current
Candidate and owner topology. [Installation](installation.md) owns the exact
downloadable versus source-Candidate boundary. Passing source, browser or
qualification checks does not create a successor Release.

## Agent Delivery Chain Owner-Process Baseline

Capability, Build, Runtime Control, Workspace, Resource Catalog, and Serve each
have an owner process, migration set, readiness surface, and owner-local
Operation readback.
The Console BFF carries the authenticated `CallContext` and `console_bff` mTLS
identity to owner boundaries, where tenant and actor authorization is checked
again. Capability Package upload/reference claims, Runtime Release catalog
validation, Build input admission, BuildKit/OCI digest and descriptor readback,
and Build-to-Capability artifact registration are implemented with focused
tests. Workspace accepted-order recovery, Fabric resource acceptance/readback and
Serve first-delivery reservation/execution are implemented. Complete application
deployment on a qualified provider host and Instance qualification remain open.

The implementation incorporates the accepted SSOT correction: Serve owns
current Agent deployment selection and route/readiness/access state; Workspace
carries authorization and business intent without a deployment pointer or
cross-database selection transaction. Build persistence includes the exact
`call_context` and `descriptor_bytes` fields used by the worker and migration.

### Cloud Console Agent directory and detail readback

The September 28 source repair implements the previously missing Capability
version list, preserves status/cursor/limit filters through the BFF, and derives
Package's latest ready version from Capability rows. Isolated PostgreSQL tests
exercise tenant/private visibility, official packages, status, pagination and
active references. Native OMA upload validation now checks the upstream
candidate identity and rejects duplicate index paths; the canonical digest
check is compared with Framework's actual serializer. See the
[source repair receipt](./evidence/source-checks/2026-09-28-runtime-capability-readback-repair.json).
These checks establish source acceptance without claiming production adoption.

The Cloud Console now exposes `/console/agents` and
`/console/agents/:packageId`. The directory reads Capability-owned Package DTOs;
the detail page reads the Package, PackageVersion page, CapabilityVersion page,
and related Build owner status through the same-origin BFF. A version is shown as
"可部署" only when the Capability owner returns `ready`. The page does not show
artifact digests or internal receipts by default and does not offer archive or
removal actions without the corresponding owner write/reference-check contract.

The source receipt
[2026-09-27 Agent owner readback](./evidence/source-checks/2026-09-27-agent-owner-readback-console.json)
binds the original implementation commit. The current PR #654 port is bound
by [the PR port receipt](./evidence/source-checks/2026-09-27-agent-owner-readback-pr-654.json),
which records commit `48b66fe7` and the rerun focused browser/BFF checks. The
prior TCR package/runtime/WebUI receipt remains explicitly unverified for this
checkout; its artifact and smoke claims are not promoted into Console or
deployment evidence.

### Serve delivery read surface

The Serve process exposes `ListDeployments`, `GetDeployment` and
`GetWorkspaceAccess` through the production BFF mux and the live CloudIdentity
boundary. Deployment history is ordered by `(created_at DESC, id DESC)`;
Serve enforces the persisted Workspace tenant before returning owner-local facts.
Every composed BFF read carries the decision for its own owner/action/resource.
The public API uses the canonical JSON field and enum vocabulary, the common
session/CSRF/idempotency guard, typed owner errors and `Cache-Control: no-store`.

Access succeeds only for an active, ready deployment with a confirmed HTTP(S)
entry and a supported exposure mode. Pending, absent, invalid-entry and
unconfirmed Cloud-private access return `APP_ACCESS_UNAVAILABLE` rather than an
invalid successful access object. Application login remains application-owned;
no arbitrary expiry or second application session is created. The optional
`expiresAt` is omitted when the entry provider supplies no expiry.

Runtime observations are persisted in Serve's own database. Seeded deployment
rows in the read tests prove authorization and projection, not a real deployment.
The real-identity tests use production CloudIdentity, gRPC, isolated PostgreSQL
and the BFF; only the external Gateway identity response is simulated.

Serve also implements the first-delivery write path: it allocates delivery
identities, acquires and binds the exact Capability claim, requires confirmed
Fabric execution resources, and calls the retained Fabric application adapter
with a dedicated Serve identity. Runtime observation and active selection are
serialized across service instances in one Workspace-scoped transaction.
ReadRuntime re-observes the original command after response loss. The
[Serve source receipt](./evidence/source-checks/2026-09-26-serve-write.json)
records real PostgreSQL owner checks and the remaining fixture boundaries.
These checks do not establish a deployment on a qualified Local provider host.

### Resource Catalog approved plans and policy versions

The Resource Catalog owner is its own Go module, process, migration set and
database role, and serves `ResourceCatalogProductService` behind the shared owner
identity boundary. An administrator can create approved compute and storage plans
inside the instance-declared provider/region/billing profile, create price policy
versions that bind an exact approved plan pair to one-month amounts, and create
the frozen refund and retention policy versions. The customer-facing availability
projection, the D17 upgrade/supplement arithmetic and the
`catalog.policy_changed.v1` Outbox event all live in this owner.

The catalog now has a real caller and a real authorization boundary. The Console
BFF carries the catalog REST surface on its authenticated boundary (browser
session, CSRF, same-origin, idempotency key and a CloudIdentity decision), the
generated policy carries the 14 `resource_catalog` actions with the contract's
audiences and roles, and the response status is compiled from the same contract,
so a platform administrator is admitted for the administrator actions, a member
for the customer reads, a tenant administrator is refused the administrator
actions, and an availability update answers with the declared `200`. A focused
live check drives the real CloudIdentity process, the real BFF route functions and
the real owner over one isolated PostgreSQL server, and a money-bearing price
policy version round-trips the contract's `...USDMicros` decimal-string spelling
in both directions; only the external Sub2API Gateway is a fixture. The
[source-check
receipt](./evidence/source-checks/2026-09-25-resource-catalog-owner-caller-local.json)
binds the source and dependency digests, commands, exit codes and logs.

The deploy quote is priced. A member obtains a quote for an approved plan pair
through the running BFF process and reads it back: the owner resolves the single
effective price, refund and retention version, builds the offer lines under the
contract money rule (total is the sum of the charge lines minus the credits, with
no second multiplication by quantity) and stores the immutable offer with its
expiry. `AcceptQuote` binds that offer to exactly one Workspace obligation,
returns the same acceptance on replay, refuses a second obligation, refuses an
expired offer and reports an unknown offer as absent. `resize` and `renew` are
refused with the gap named rather than answered with a deploy-shaped offer,
because they depend on a Workspace subscription, a paid period and an accepted
plan change that do not exist yet. The [source-check
receipt](./evidence/source-checks/2026-09-25-resource-catalog-quote-local.json)
binds the revision, commands, exit codes and logs.

The quote also freezes the complete approved provider plan. Workspace accepts it
through its authenticated BFF create route, commits the original order and
resumes the same Catalog acceptance and Fabric resource intent after response
loss or service restart. Fabric stores the original resource-set, resource IDs
and operation without inventing provider references. The CloudIdentity
continuation grant is bound to that exact Workspace and Capability version;
queued creates revalidate authority after their command lock.

The Ledger `DomainInbox` still accepts only tenant-scoped build and capability
producers, so the platform-scoped catalog policy event remains pending. Local
zero-charge delivery uses a separate typed receipt operation over the existing
append-only Ledger store; it reads both original owners and cannot be created,
read or mutated through the legacy generic HTTP receipt paths. A no-charge
receipt is not a customer payment or subscription period.

### Workspace original-order and Local delivery boundary

The [Workspace source receipt](./evidence/source-checks/2026-09-26-workspace-resource-original-order.json)
separates the real accepted-quote/order/resource-reference check from provider
qualification. The acceptance harness uses production CloudIdentity, Catalog,
Workspace, Fabric and Ledger implementations over real gRPC and independent
PostgreSQL databases, with the BFF HTTP routes as caller. Only the external
Sub2API identity response is a fixture. It checks stable acceptance/resource
identities, receipt replay after a lost reply, session-expiry recovery, queued
authority revocation and cross-tenant refusals. No paid subscription, provider
reference or customer charge is fabricated.

The Local source path validates the exact approved profile/SKU/specification and
tenant-to-account binding, obtains the typed zero-charge receipt and performs
storage quota preflight before any compute allocation. On confirmed resource
readback, Workspace resumes Capability and Serve with durable descriptor,
reservation and runtime command evidence. Unknown results remain pending;
Workspace activation, paid periods and access are not inferred from resource
acceptance. Actual Local deployment remains unqualified: the inspected Fabric
host is macOS, whose project-quota guard refuses dispatch, and no existing
qualified Linux entry was established. This environment boundary does not
invalidate the accepted-order/resource-reference result.

### Canonical-main local verification and receipt

The canonical-main baseline at `4e78b6fb96d0f4474a9b6ba885ef6f7c879f05fe`
(before the subsequent PR #628 CSS-only correction) was first exercised and
exposed a Fabric PostgreSQL fixture mismatch:
production delete path uses `read_storage_for_delete`, while the fixture only
accepted `sync_storage_volume`. A one-line test-fixture correction was applied
on candidate source `de6eeb046300656b7879783ce60c901656a2687d`; production code
and the no-redispatch assertion were unchanged. The exact-head full local gate
then passed with 228 source tests, 114 browser tests, all required PostgreSQL
packages with zero skips, and Local-Docker integration. The append-only
[source-check receipt](./evidence/source-checks/2026-09-24-owner-process-full-local-verification.json)
records the failed attempts, final pass, log digest, exact source SHA/tree, and
scope limits.

This receipt proves the stated historical Cloud source/local checks. It does not
prove the complete authenticated Package-to-CapabilityVersion journey.

### Isolated Package upload and BuildKit execution

`npm run verify:package-to-oci` now exercises Capability's gRPC Package/upload
handlers and signed HTTP multipart data plane against its own PostgreSQL schema,
then sends the confirmed ZIP bytes through the production Build runner to a real
isolated BuildKit and Registry. Build consumes ZIP packages; only the approved
recipe remains a tar artifact. Capability's idempotency lock uses an unambiguous
PostgreSQL text key, and completed uploads reject conflicting part identities.
The Capability process starts the same restricted data handler used by the test.

The full local gate also exposed a retained Fabric adapter defect: repository
list filtering omitted digest-only Docker images and could report premature
retirement. Fabric now inspects the exact immutable image reference, verifies
its identity and treats unavailable or malformed readback as unknown. The real
Docker application replacement/retirement scenario and focused failure cases
cover that correction; it does not authorize any Instance resource cleanup.

The live check verifies actual Package and WebUI files in the exported image,
manifest/config digest readback, and Build's persisted recovery from a pre-ack
`building` state with the builder stopped. A registry outage retains
`needs_attention`; recovery creates one artifact and one Outbox event with
matching descriptor bytes. Multipart retries, completion retries, conflicting
completion, corrupt bytes, and cross-tenant reads are also exercised.

The follow-up owner-chain check now uses actual Runtime registration and policy
selection, CreateBuild/ResolveBuildInput, all three claims bound against Build
commit readback, and Build/Capability registration Inbox/Outbox delivery. Both
consumer acknowledgements are deliberately lost after commit. Retrying converges
to one ready CapabilityVersion, one succeeded Build and zero pending deliveries
between those two owners. The check exposed and repaired reversed consumer/event
arguments that previously left acknowledged deliveries pending indefinitely.

Publisher JSON now uses its public schema vocabulary rather than protobuf enum
names, including required false/zero/empty values and access-union objects. The
shared codec is used by Runtime admission, Capability reads and Build descriptor
creation. The schema compiler supports the canonical ECMA-262 lookahead patterns;
Package source roots are safe relative paths as the existing contract requires.

The [owner registration receipt](./evidence/source-checks/2026-09-25-package-owner-registration-local.json)
binds the tested source and successful full-local gate (228 source tests, 114
browser tests, all required PostgreSQL modules with zero skips, Docker integration)
plus final focused rechecks. The earlier
[local execution receipt](./evidence/source-checks/2026-09-25-package-buildkit-local.json)
remains historical evidence for its own source.

The latest [publisher/Ledger/process-interruption receipt](./evidence/source-checks/2026-09-25-publisher-ledger-worker-local.json)
adds the actual Console publishing journey through authenticated BFF handlers,
Capability upload, Build admission and real BuildKit/Registry execution. The
CloudIdentity Console build exposes the publisher entry without changing the four
primary customer navigation tasks. The browser verifies the same ready version after
reload; CSRF, forged identity and cross-tenant rejection are covered. Public
HTTP DTOs use the shared canonical API/publisher JSON codec.

The default legacy Console now keeps the Publisher route, Workspace-list entry,
and Agent delivery panel disabled through the same `VITE_CONSOLE_IDENTITY`
selection as authentication. The [dual-mode browser evidence](./evidence/source-checks/2026-09-26-legacy-console-gate.json)
checks rendered navigation, direct URLs and requests: legacy makes no `/api/v2`
calls, while explicit `cloud` builds retain publisher catalog and delivery reads.
This is source validation; it does not establish a deployed Instance change.

Ledger now consumes upload, Build confirmation/failure and version-registration
events through its authenticated gRPC Inbox and existing receipt store. Each
consumer retries independently. Lost acknowledgements, duplicate delivery,
producer mismatch and conflicting event identity are verified with real
PostgreSQL. Generic receipt HTTP writes cannot forge these domain event types.

A separate OS worker is also killed after the real exporter commits to Registry
but before the worker receives exporter completion. A new worker and database
connection recover the original digest and register exactly one artifact/event/
version without a second export. This replaces the former process-interruption
coverage gap; it does not claim a mid-layer network-partition test.

The full-local gate passed: 228 source tests, 114 browser-suite tests,
all required PostgreSQL modules with zero skips, and Docker integration. The
additional real browser/owner-chain test passed with inspected desktop/mobile
screenshots. A final [HTTP contract-alignment receipt](./evidence/source-checks/2026-09-25-publisher-public-api-alignment.json) records focused rechecks for the canonical session path, CSRF header, response codes and Error shape. Earlier receipts remain bound to their historical source.

The [September 28 exact Runtime build](./evidence/source-checks/2026-09-28-exact-runtime-local-build.json)
uses the approved upstream GHCR Runtime digest, the real Cloud WebUI source and
an explicitly synthetic Package. Isolated BuildKit composition, immutable
Registry readback, real browser publisher flow, file-content checks and worker
restart/lost-ack recovery all pass. Earlier GHCR/network failures therefore do
not describe the current local build path. The local source acceptance is
separate from the original OMA candidate run: its archive is unavailable on this
machine and no authorized TCR publication/readback occurred. Those historical
unknown facts are retained in the append-only receipts, without a source-merge
or recurring monitoring obligation. No Agent loading, real model call or
production deployment is claimed.

### Publisher identity and admission

`services/gateway-integration` now owns real Cloud session issuance, live
Tenant/member authorization, context introspection and accepted-Build grants.
The BFF forwards non-bearer session references; raw browser credentials no longer
enter persisted Build call contexts. OwnerCommitReadback binds the original
context, actor, scope, action/resource and immutable continuation inputs before a
grant can be issued. Grants are idempotent and do not disappear on logout.

Capability now admits publisher namespaces and complete immutable WebUI contracts
through platform-authorized APIs, rejects overlapping/foreign repository scopes,
and enforces revocation on new Builds. The live browser logs in through the real
issuer and these catalog records are created through BFF/owner commands. The
external Sub2API authentication server and known test Tenant membership remain
explicit isolation prerequisites; Cloud session/grant/catalog decisions are no
longer fixtures. The retained Console selects the successor identity path only
with an explicit build configuration; no production migration is implied.

The [identity/admission source receipt](./evidence/source-checks/2026-09-25-publisher-identity-admission-local.json)
records the actual verification and limitations. The preceding receipts are
historical, exact-source evidence. This implements the #625 publisher identity
and admission prerequisites; the Tenant member and invitation slice is recorded
separately below, and Tenant lifecycle plus Gateway wallet migration remain
separate outcomes. No production, Instance or real Sub2API-account qualification
is claimed.
[Reproduction and configuration](./runtime/package-buildkit-local.md) gives the
local command and process settings, including restart reauthentication.

### Tenant member and invitation governance

### Tenant-scoped application OCI destination (Cloud source, local)

CloudIdentity `CreateTenant` admits a Cloud Tenant, writes its owner membership
and reserves one stable `tenant_id -> repository` binding in a single command
transaction. The initial repository name derives from the verified owner email
local-part; a collision receives a deterministic suffix, so two Tenants never
alias one repository. The installation supplies `OPL_WORKSPACE_REGISTRY_HOST` /
`OPL_WORKSPACE_REGISTRY_NAMESPACE` together as non-secret facts, and
`GetTenantRepositoryBinding` exposes the binding to Build and the Console BFF
only over the authenticated owner boundary.

At Tenant-owner startup, `BackfillTenantRepositoryBindings` reserves a binding
for every Tenant admitted before this table existed. It derives a stable
`tenant-<sha256-prefix>` repository name from the durable Tenant ID and the same
installation facts; it neither calls Gateway nor replaces an existing binding.

Build resolves its output destination from that binding through
`Runner.DestinationRepository` and stores `<repository>:<job-id>` as the
immutable `executor_ref`, replacing the previous global-prefix + hashed
Tenant/Package path. The append-only
[binding receipts](./evidence/source-checks/2026-09-29-tenant-repository-binding-local.json)
and [upgrade backfill receipt](./evidence/source-checks/2026-09-29-tenant-repository-binding-backfill-local.json)
record the exact commands and the remaining boundary.

Verified locally: `TestRepositorySlugCandidate`; the real `CreateTenant` /
`GetTenantRepositoryBinding` path over isolated PostgreSQL (platform-admin gate,
first Tenant reserves `huangrende`, a second same-local-part Tenant gets
`huangrende-1`, stable re-read, upgrade backfill and restart idempotency for
pre-binding Tenants, `NotFound` for an unknown Tenant, Build/BFF read
allowed and Ledger read refused); and the full live owner chain
`TestLivePackageBuildAndRestartReadback`, which pushed to
`127.0.0.1:<port>/result/tenant-<sha256-prefix>` — the Tenant-resolved
`host/namespace/repository`, not the hashed path. `npm run verify:local` passes.
Still open: no Tencent/TKE-hosted Cloud Build has pushed a Tenant-resolved
destination, and the existing `oplcloud/huangrende` digest is not bound to a
recorded Build job.

`services/gateway-integration` now also serves CloudIdentity's Tenant member
surface: `getTenant`, `listMembers`, `listInvitations`, `inviteMember`,
`acceptInvitation`, `revokeInvitation`, `updateMemberRole` and `removeMember`,
reached through the Console BFF. The role decision comes from the canonical API
permissions, now generated for the tenant owner instead of only Capability and
Build. `acceptInvitation` is bound to the invitee's own live session and the
recorded invitation subject, because an invitee is not yet a member of the
inviting Tenant.

Membership changes lock the Tenant row, refuse to demote or remove the last
owner, advance `permission_version`, and revoke a removed member's sessions, so
losing access takes effect on the next request rather than at session expiry.
Invitations keep only a token hash, and each governance decision writes an
immutable audit row; a refusal commits that evidence and then reports the
canonical `ErrorCodeEnum`, including `LAST_OWNER` and `INVITATION_INVALID`.
Concurrent owner removal was proved to serialize to exactly one success.

The generated permission table is the single authorization policy for the
capability, build, tenant, runtime control, resource catalog, serve and workspace
audiences, each generated wholesale per owner from the canonical contract.

`getWorkspaceAccess` was the one contradiction: the REST contract named the
`workspace` owner while the wire RPC sits on `ServeProductService`,
`01_domain_ownership_matrix.md` gives Serve the access-binding facts, and
`decisions.md` records Serve as the sole delivery/deployment/readiness/access
owner with Workspace keeping target authorization only. The projection had lagged
the adopted decision, so the contract now names `serve` and the operation is
authorized on the Serve audience for the fact it reports. The composed BFF
delivery view authorizes each owner fact against the owner that reports it: the
Workspace identity read against Workspace, the access read against Serve.

A CloudIdentity decision now reaches every owner boundary as itself.
`ownerservice.Authorizer` used to collapse every failure from the authorization
call into `Unavailable`, so a policy denial, a revoked session and a failed
precondition were indistinguishable from an authority outage at the Serve,
Capability, Build, Runtime Control and BFF boundaries. Decision codes are now
preserved, only a non-decision failure is reported as unavailable, and the
boundary still fails closed.

The BFF's HTTP success status is now compiled from the same contract into
`apps/console-bff/internal/httpapi/status_generated.go`, so a routed operation
answers with the status the contract declares. The previous hand-written switch
had already drifted: `setComputePlanAvailability` and `setStoragePlanAvailability`
are declared 200 but would have answered the write default 201. A route whose
action carries no declared status is refused rather than defaulted.

The invitation validity window is not fixed by the canonical contract, so the
deployment now supplies it explicitly through `OPL_INVITATION_TTL`; this owner
refuses to start without a positive value rather than baking a product decision
into code. Deployments that enable invitations must set it.

The [member governance source receipt](./evidence/source-checks/2026-09-25-cloudidentity-member-governance-local.json)
records the exact source files, the governed cases and the limitations.
`Member.displayName` now resolves through CloudIdentity's own authorized,
read-only Gateway directory identity (`OPL_GATEWAY_DIRECTORY_EMAIL` /
`OPL_GATEWAY_DIRECTORY_PASSWORD`), which also validates that an invited subject
exists before any row is written. That identity is the service's own
administrative credential for a directory read; it never presents a member token
and never writes in the Gateway authority, so it is not impersonation and no copy
of the directory is kept. A deployment without it leaves the name empty and skips
the subject check rather than fabricating either fact.

The member REST surface (BFF) is implemented; the Console member page is not, and
the two are tracked separately. That page belongs to W13 Console/BFF basic
integration, not to the W14 agent/upload/build front end. Tenant onboarding,
suspend/reenable, delete/restore and Gateway wallet binding stay separate
W03/W21 outcomes. No production, Instance or real-account qualification is
claimed.

## Conclusion

OPL Cloud has reached a basically usable administrator-operated stage: the
public Console and health endpoint respond, current source implements the
account, Workspace, provider, billing, and evidence paths, and a protected
medopl run has verified two existing Workspaces through restart, storage,
private-state, and Package readback.

This does not mean Public Beta or a current Product Release is ready. Public
registration, deployed Renewal/Delete qualification, alert and restore qualification,
one exact-current Local plus Tencent/TKE Candidate cohort, and same-byte public
promotion remain open. The only public Product Release is the older `v0.1.7`.

## Workspace Deletion and Hourly Refund

The 2026-09-26 [tag hydration source check](./evidence/source-checks/2026-09-26-compute-tag-hydration-identity.json)
isolates why the deployed ownership-tag repair still left five retained computes
without tags: the child create and successful Launch have different provider
read request IDs. Instance observation `36193121456` confirms one original active
ownership per allocation, matching stable identities, and only three additional
Describe request ID differences. Empty-tag hydration now excludes those three
read metadata keys from its local comparison copies. All other identity checks,
the original deletion guard, current allocation facts and stored Launch records
remain unchanged. Focused race/PostgreSQL checks and both Linux builds pass;
the repaired source still requires Instance adoption and owner readback.

The 2026-09-26 [focused source check](./evidence/source-checks/2026-09-26-compute-launch-ownership-tags.json)
preserves the original active MachineOwnership tags in successful Tencent Launch
compute records. Retained empty tag projections recover only from one exact
successful Launch stage and its persisted ownership, including after PostgreSQL
reopen or when a child record already populated memory. Partial tags, conflicting
stages and changed resource identities remain rejected by the existing destroy
guard. Already externally deleted allocations enter readback-only convergence,
without a new cloud delete dispatch. Source tests and Linux builds pass; adoption
and the affected production deletion still require Instance readback.

The 2026-09-26 [focused source check](./evidence/source-checks/2026-09-26-pv-storage-class-delete-binding.json)
fixes a false `launch_stage_binding_conflict` when Kubernetes omits an empty PV
`storageClassName`. Only that PV omission is accepted; PVC omissions, null and
nonempty classes, and other identity differences still fail. Eight focused Go
tests pass, including nine class cases and existing owner/binding rejection and
readback checks. This is source evidence; the affected production deletion has
not been rerun or confirmed by this change.

The Control Plane persists each deletion stage's exact resource identity, result,
observation time and readback reference with its progress. Fabric confirms
storage absence through a fresh read after destruction and accepts multiple
owned Pods and ReplicaSets from normal rollouts while rejecting identity
conflicts. Console customer and operator views read the same persisted stages.

Automatic platform refund is a separate operation admitted only after all
required absence evidence and the exact `workspace.deleted.v1` Ledger receipt.
It uses the original charge and account, the 720-hour policy, prior refund
reservations and the existing wallet replay mechanism. Unknown money outcomes
remain pending; deletion completion does not imply refund completion.

Focused regressions exercise missing, stale, future and conflicting storage
readbacks, multiple owned rollout children, unsafe refund rejection, and
idempotent recovery. Source tests (226), typecheck, lint, build and required
PostgreSQL checks pass with no required database skips. Browser checks pass
113 of 114 cases; the overview layout assertion also fails on pristine base
`36b8647885cc87c937290c01e2daf8d6bfa140dc` with the same browser. The real Local
Docker gate reaches successful application login, replacement and isolation,
then fails an unchanged exact-image retirement assertion; the same test fails
on that pristine base. These two existing failures remain separate Console
layout and Fabric image-retirement followups; full local verification is not
reported as all green. Production Tencent/TKE deletion and actual wallet
results remain Instance-owned qualification obligations for the adopted
Candidate.

## Launch Resource Readback

Fabric resource reads now recover missing in-memory projections from the existing
validated Launch operation records. Previously a successful Launch in the same
process could leave compute queries reporting absence and provider-fact queries
reporting `provider_fact_identity_mismatch` for compute, storage and attachment.
Both failures were reproduced through the public service methods before the fix.
The reads reuse the existing resource projection and retain account, Workspace,
stage-sequence and ambiguity checks; they do not rewrite operation records or
invoke provider mutations. Focused regression checks cover all three resource
types, compute lookup, foreign-account refusal and unchanged operation history.
Invalid and conflicting stage records are still refused. The focused race checks
pass, as do 215 source tests, 106 browser tests, typecheck, lint, build and all
database-free verification steps. The Fabric suite passes 2,103 tests/subtests;
70 database/capacity/Docker cases are skipped. The full local gate was attempted,
but the local Docker engine returned HTTP 500 before its temporary PostgreSQL
container could start, so that integration result is unavailable.

This is source acceptance. Deployment and fresh resource observation belong to
the Instance owner; no production identity repair or runtime recovery is claimed.

## Application Hosting Boundary

The baseline below combines canonical main
`a0430b099cc787a8931e54f0bda4a1424f376eb1` with the default installation,
replacement and lifecycle implementation at `46f1eb3938c602dbcee9825eb3c4686425fface0`,
verified locally on 2026-09-14 (Asia/Shanghai). It is not live resource readback,
Candidate qualification, or an Instance deployment. Target boundaries belong
to [architecture](architecture.md#workspace-application-boundary); sequencing
and deliverables belong to [roadmap](roadmap.md#implementation-sequence).

### Current Capability Baseline

This table records the retained Control Plane application path and its tested
layers. The current cloud-identity entry and default-App/Agent selection are
described [above](#current-source-and-evidence-scope); the retained path does not
become a fallback or a second writer for an owner Workspace.

| Capability | Current source behavior | Remaining gap and owning source |
| --- | --- | --- |
| Resource purchase and fulfillment | Retained Control Plane purchases commit a resource-only Launch and an independent default installation request. Charges advance in the resource worker; application failure does not rewrite purchase success. Explicit resource-only and retained full Launch contracts remain distinct. | Actual Instance adoption is unqualified. CP owns this retained Launch and Workspace orchestration; Fabric owns resources. |
| Application admission and deployment | Immutable revisions, actual configuration and Secret bindings feed a reserved deployment generation. Each component may declare its CPU and memory request and limit, which the provider applies to that component alone. Preflight precedes predecessor suspension; activation switches one selection atomically, then retires the predecessor and records evidence. Failed commands resume by original identity. The operator registry catalog browses the approved namespace's repositories and tags and resolves one tag to its digest-pinned reference before admission. | Registry multi-platform manifest readback (per-platform digest listing) remains open. Full IBD dependency configuration remains open. Target-node feasibility for a declared envelope is not yet read back. |
| Runtime execution | Local-Docker and Tencent execute declared components and live health/entry readback. OPL App uses an explicit profile and Secret-file ABI; lifecycle targets exact generations and fences late creation after deletion. | Tencent supports at most one native readiness probe. Local Docker fixtures do not qualify the actual upstream OPL App or IBD image. |
| Persistent storage | Stable application data bindings survive compatible updates and reinstallations; different applications have separate namespaces. Historical layouts require the original runtime identity and readback. | General data import/restore and CBS/TKE qualification remain open. |
| Application entry | One gateway serves the OPL App and every admitted application under the installation's existing workspace route. The executing provider reports the resolved destination or URL, the Control Plane owns the route, and an application without a published entry has no Open action. `cloud_private` publishes no external entry. | Target-installation end-to-end routing, application assets under the route's path prefix, Cookie/API/SSE behavior and Instance qualification remain open. No platform-authenticated private access is claimed. |
| Existing lifecycle consumers | Access and credential capabilities use the selected profile. Suspension/deletion inventory current and incomplete generations; resume starts only the selection. Delete confirms Runtime and owned Secret absence before resources, retaining external source Secrets and Sub2API Keys. | Instance verification and Tencent node-image collection remain external obligations. |
| Administrator UI | Structured registration, real configuration, selection progress, operator retry and owner default-installation resume are implemented. The registration form browses `repository` and tag lists from the catalog namespace and fills the digest-pinned reference after server-side resolution. Navigation rejects delayed responses from a different Workspace; application changes clear revealed passwords. | Registry browsing shows one flat repository list; multi-platform digest selection remains open. |

### Default Application Replacement Verification

The local change combines the resource purchase and default application as
independent durable operations. It preserves exact resource/financial identity
through replacement; current and reserved application IDs are persisted on the
Workspace. Legacy full Launch and generic runtime records remain readable under
their original contracts. New retirement evidence does not rewrite purchase
receipts or include CP recovery cursors, credentials or financial mutations.

Focused checks cover default installation, compatible update, switching to an
unrelated application, reusing original data on reinstall, preflight failure
without stopping the predecessor, entitlement fencing and immutable purchase
readback. Ledger boundary tests accept explicit resource-only evidence and
reject incomplete cleanup or runtime fields inserted into that resource contract.
Retained full Launch recovery and monthly preflight checks continue to pass.
The final implementation passes `verify:local:full`: 311 source/browser
tests, TypeScript typecheck and lint, Console build, Go compilation and
non-database checks, plus all 18 required PostgreSQL/Docker test packages
with zero skips. The full run includes the corrected PostgreSQL baseline restore,
ready-recovery concurrency barrier, initial pending/ready readback and
queue-clock fixture assertions;
production recovery, health and cleanup acceptance remains enforced.

The real Docker fixture verifies OPL-profile login/session/Gateway Secret-file
ABI, same-application data/password/session retention, unrelated-application data
and Secret isolation, exact predecessor Runtime/image absence, and stop/resume/
delete with current-entry HTTP readback. The complete focused Docker run also
passed on `9c42f228`; the later source delta only repairs queue test setup to
use authoritative admission timestamps with a skewed proposed queue timestamp.

| Source-check evidence | Exact value |
| --- | --- |
| Implementation SHA | `46f1eb3938c602dbcee9825eb3c4686425fface0` |
| Implementation tree | `02210787b660ed6479ffc05fdf12e9fbd19c876a` |
| Command | `GOMAXPROCS=2 GOFLAGS=-p=1 npm run verify:local:full` |
| Completed at (UTC) | `2026-09-13T17:05:27.619110+00:00` |
| Full log SHA-256 | `6a5e2a7b97544f95d483a2958b996a89917257896220c75bf77c25f0b0e29491` |
| Source consistency | Before/after HEAD, tree and clean tracked worktree match. Subsequent closeout changes only update documentation evidence. |
| Focused Docker source | `9c42f228334a031da4bc3c10ac31f36960850853` |
| Focused Docker log SHA-256 | `28f3a358edc8676a840688ef46188bc8805d223fa99b19a63b2fc9a31eb96632` |

The retained artifacts are `verify-local-full-sixth.log`,
`verify-local-full-sixth-result.json`, `docker-application-ninth.log` and its
result JSON. Earlier failed attempts and local environment recovery records
remain separate and do not qualify the final source.

The owning checks are the CP `workspace_default_application_test.go`,
`workspace_application_recovery_test.go`,
`workspace_application_operation_store_test.go` and
`workspace_application_lifecycle_test.go`; the Fabric application lifecycle and
real Docker integration tests; and Ledger `workspace_application_receipt_test.go`.
Local logs are retained under
`output/workspace-default-install-replacement-20260913/` in the canonical
checkout.
These local checks do not establish a Candidate, Product Release, Tencent
Instance deployment, real OPL App business session, or IBD business availability.

### Independent Application Deployment Verification

The application deployment repair follows the existing owner path. CP sends
the account and original successful Launch's
`attachmentBindingRef` through the typed Fabric client. Both application
create/readback routes use the existing scoped capability check. Invalid input
is rejected as a client error; pending create and readback return structured
HTTP 202 observations. A failed or foreign Runtime cannot activate a Workspace.
Workspace binding and receipt-phase intent commit in one PostgreSQL transaction;
a failed phase write rolls both back, and a lost commit response resumes receipt
recording without another runtime mutation.
Compose now forwards `OPL_WORKSPACE_APPLICATION_DEPLOYMENT_WORKER_ENABLED`;
the local Workspace overlay enables the worker. Other installations must
explicitly enable it to execute admitted deployment intents.

The shared revision's `entryPort` names an explicitly declared TCP port.
Port declaration or health-check configuration alone does not publish a web
entry. Component names must be unique, with `main` reserved for the primary
application. The providers consume primary entrypoint/mount/probe declarations
only for that component. The portable Local-Docker overlay uses the same pinned
`OPL_CLOUD_IMAGE` as its temporary HTTP probe image; native Fabric deployments
must explicitly configure `OPL_FABRIC_LOCAL_DOCKER_PROBE_IMAGE` for HTTP probes.
Tencent uses installation-level `OPL_IMAGE_PULL_SECRET_NAME` and optional
`OPL_INGRESS_CLASS`; when omitted, Kubernetes admission/controller selection
owns the class. This change does not implement personal Registry connections.

Focused contract, typed HTTP, worker recovery, Console/portable and real
PostgreSQL activation rollback/reconnect checks pass. Tencent readback tests
cover stale generation, missing or foreign Pod, wrong digest, missing probe,
and pending/mismatched public entry. The real Docker visit-counter test proves
HTTP 503 keeps the original operation pending, HTTP 200 converges without
recreating the main container, the published entry serves traffic, and data
survives subsequent container reconstruction with no probe containers left.

| Evidence surface | Owning checks |
| --- | --- |
| CP intent, worker and transaction recovery | [Deployment tests](../services/control-plane/internal/server/workspace_application_deployment_test.go) and [typed HTTP client](../services/control-plane/internal/clients/fabric_application_runtime_test.go) |
| Fabric HTTP and operation convergence | [HTTP boundary](../services/fabric/internal/http/workspace_application_runtime_test.go) and [runtime engine](../services/fabric/internal/fabric/workspace_application_runtime_test.go) |
| Provider execution and observation | [Real Docker](../services/fabric/internal/fabric/local_docker_application_runtime_integration_test.go), [Local probe/entry checks](../services/fabric/internal/fabric/local_docker_application_runtime_test.go), and [Tencent manifests/readback](../services/fabric/internal/fabric/tencent_provider_application_runtime_test.go) |
| Console and installation configuration | [Form/progress model](../tests/ui/workspace-application-deployment-controller-model.test.ts) and [resolved portable Compose](../tests/contracts/portable-local-docker-assets.test.ts) |

The first full local attempts exposed a browser timeout and shared PostgreSQL/
Docker contention: five retained checks and the new application test timed out
during store setup, migration or container removal. The browser check passed
an isolated retry; all six backend checks passed when run serially. The local
verification runner now serializes modules sharing those resources, retaining
the tests' own concurrency assertions, deadlines and zero-skip requirement.
`npm run verify:local:full` passed on 2026-09-13, including source and browser
checks, typecheck/lint, builds, and all 18 PostgreSQL/Docker test packages with
zero required skips. A subsequent provider-only account-readback review
reproduced an identity gap: observed account labels were not compared with the
authorized request account. Both providers now reject that drift. The final
application engine/provider/HTTP suite was rerun after this correction with
real Docker enabled: 68 tests/subtests passed, zero failed and zero skipped.

Local source and verification artifacts are retained at
`/Users/huangrende/Documents/ChatGPT/one-person-lab-cloud/output/stage1-deployment-chain-20260913/`.
`source-manifest.json` binds the final 29 source/test/config files to the base
commit and `source.patch` SHA-256
`0781c8ce4239cc97947f363f784809974693fa5dda5983722ad8d7c7110e01b3`.
The preceding full-check snapshot and log are retained separately from the
final provider amendment and its `provider-final.jsonl`; the manifest records
which source each check proves. These are local verification artifacts. Cloud
source integration is tracked by [PR #551](https://github.com/gaofeng21cn/one-person-lab-cloud/pull/551);
Instance qualification requires its own deployment and runtime receipts.

These checks prove their respective source and local-runtime layers. The
non-OPL Docker fixture is not OPL App or IBD business qualification. Application
replacement and retirement verification for the successor change are reported
above. Real Tencent networking/TLS and the selected Workspace's IBD launch
remain open. No new Candidate, Product Release
or Instance receipt is claimed.

### Customer Launch Provisioning Shape (Local Development)

A customer Launch now states its provisioning shape explicitly. The Console
launch controller composes `provisioningMode: "resource_only"` in the single
place it builds the wire request, so ordinary purchase opens compute, storage,
attachment and Workspace entitlement and does not create a default OPL App
installation operation. The administrator never picks a provisioning shape, and
changing it is a conflict against an in-flight intent rather than a silent reuse
of the previous idempotency key.

This does not change the retained contract. A request that omits
`provisioningMode` still takes the existing path, which is what the operator
qualification flows under `tools/` use; historical Launches keep their recorded
completion obligations and replay through their own stored mode.

Evidence: `node --test tests/ui/workspace-launch-controller-model.test.ts`
passes 9 cases, including the new resources-only composition and the
provisioning-mode conflict case.

### Per-Binding Application Origin (Local Development)

A Workspace's binding to an application is now published at its own browser
origin instead of the shared `workspaceDomain/w/<workspaceId>/` path. The origin
is derived, not allocated, and sits directly under its own installation-stated
domain:

```text
<workspaceId>-<12 hex of stableID("workspace-application-origin", workspaceId, applicationId)>.<OPL_WORKSPACE_APPLICATION_DOMAIN>
```

`OPL_WORKSPACE_APPLICATION_DOMAIN` is separate from `OPL_WORKSPACE_DOMAIN` on
purpose: the origin layout decides which DNS record and which certificate the
installation must hold. Publishing origins one label under the zone keeps them
inside the public DNS proxy's free edge certificate, which covers a root domain
and its first-level subdomains; one label deeper would need a paid edge
certificate plus a purchased load-balancer certificate. An installation that
states no such domain publishes no origins and every binding keeps the retained
`/w/` entry.

`services/control-plane/internal/server/workspace_application_origin.go` owns
that shape. Detail and the compatibility split are in
[Workspace Runtime Access](./implementation/workspace-runtime-access.md#selected-application-access).

- Routing reads the Workspace identity out of the name, so no distribution table
  and no second writer exist. The application component is confirmed against the
  Workspace's current binding; a superseded origin answers `410`
  `workspace_application_origin_retired`.
- A binding origin is dispatched before the management route table, so this
  server's own routes never answer on an application host.
- Only platform credentials are removed from the forwarded request
  (`opl_session`, `opl_ws_active`, `opl_ws_session_*`, `X-OPL-CSRF*`). The
  application keeps its own `Authorization` header and cookies, replacing the
  previous behaviour that also dropped every application cookie and every
  `Set-Cookie` except `aionui-session`.
- Response cookies keep their values but lose any `Domain` attribute, so no
  application can widen a cookie onto a sibling binding or onto the Console.
- `X-Forwarded-Host` and `X-Forwarded-Proto` are now stated by the proxy.
  Previously neither was sent, so an application building absolute URLs from its
  request saw the in-cluster service name. A caller-supplied value is
  overwritten. Both derive the scheme from one installation fact,
  `workspaceExternalScheme`, which reads the installation's declared
  `OPL_PUBLIC_URL` rather than a literal: the in-cluster hop is plain HTTP, so
  the request cannot report the external scheme, and stating it once means the
  published entry URLs and the forwarded header cannot disagree. `OPL_PUBLIC_URL`
  joins `OPL_WORKSPACE_DOMAIN` as a startup requirement, so an installation that
  cannot state its external origin fails before serving anything instead of
  publishing an address that cannot resolve.
- The retained path-based entry keeps its previous behaviour exactly, including
  the per-Workspace rename of the OPL runtime's fixed session cookie, because
  several Workspaces still answer on that one hostname.
- The customer entry URL projected for a current application is the binding
  origin. A binding with no derivable origin — an installation that states no
  application-origin domain, or a Workspace identity that cannot form a DNS
  label — keeps the path-based entry rather than an address that cannot resolve.

Focused evidence: 8 new cases in `workspace_application_origin_test.go` cover
derivation and reverse parsing, foreign-host rejection, the requirement for a
resolvable identity, platform-credential-only stripping, cookie confinement,
host ownership, and superseded-origin refusal.
`TestApplicationAccessUsesSelectedRuntimeAndSuppressesLegacyCredentials` was
updated to the current contract, and it now also asserts the retained path-based
route is no longer published as the customer entry.

| Source-check evidence | Exact value |
| --- | --- |
| Command | `go test ./... -count=1` under `services/control-plane` |
| Result | all packages pass; only the PostgreSQL-gated cases are skipped for a missing database |

#### Instance obligation

The instance must provide a proxied wildcard DNS record for
`*.<OPL_WORKSPACE_APPLICATION_DOMAIN>`. No origin-side certificate is required:
the public DNS proxy terminates TLS for those names and its edge certificate
already covers the zone's first-level subdomains. Cloud has no default domain and
creates no DNS records or certificates. The domain, its wildcard record and the
real browser qualification remain instance-owned, and no production state was
read or changed here.

The instance acceptance tool `tools/workspace-application-delivery.mjs` now
probes the origin space itself (`app-preflight-<run>.<application domain>`) and
requires the Ingress to declare the origin route, replacing an earlier
requirement that the Ingress declare wildcard TLS. The retained error code string
stays so historical receipts still validate.

Public verification of the infrastructure layer, without deploying: three
derived origin addresses resolved, completed a TLS handshake against the edge
certificate (`medopl.com`, `*.medopl.com`) and answered over HTTPS.

The origin dispatch itself is now deployed and read back. Candidate `dc34b4de`
deployed through the instance workflow with no rollback, and the instance
receipt is `receipts/2026-09-18-application-origin-deploy-35253804870.json`.
Public readback after the rollout shows the dispatch is live:

| Request | Before | After |
| --- | --- | --- |
| `<workspace>-<12hex>.medopl.com/` | Console static fallback, `200` | `404`, no static fallback |
| `gateway.medopl.com/` (not an origin shape) | `200` | `200` |
| `workspace.medopl.com/w/<unknown>/` | `404` | `404` |
| `cloud.medopl.com/api/healthz` | `200` | `200` |

An origin-shaped host is now resolved and, when no such Workspace exists,
refused rather than answered by the Console. Routing an origin to a *deployed*
application still needs a Workspace that has an application deployed, so a real
browser acceptance of application assets, APIs, cookies and streaming remains an
instance obligation.

### Unified Deployment Command With Inline Revision (Local Development)

An operator can describe the image and its run requirements in the deployment
command itself, so deploying an image no longer requires a separate prior
"register an application version" business step. `POST
/api/operator/application-deployments` accepts an optional `revision` object
alongside the existing deployment inputs.

The revision snapshot keeps its single owner. The command writes the same
admitted-revision row the registration route writes; it does not copy the
revision into the deployment intent and does not introduce a second table.
Deployment still resolves the admitted payload and its digest, not the request
body, so the existing admission/deployment consistency checks stay in force. An
admitted identity with different content remains a conflict and never rewrites
the stored revision, and a request whose envelope identity disagrees with the
inline revision is rejected.

The Console deployment action sends that inline revision, so one operator
action deploys an image that was never pre-registered. The separate "register an
application version" action remains for a publisher who wants to pre-stage a
revision; it is no longer a prerequisite.

Evidence: `go test ./internal/server/ -run
'TestApplicationDeploymentAcceptsInlineRevision|TestApplicationDeploymentIntentHTTP'`
passes, including a new case that asserts the revision reaches the single
revision owner, that conflicting content is refused without rewriting the stored
payload, and that a mismatched envelope identity is a bad request. The browser
suite `npm run test:browser:operator-resource-read` passes 13 cases, and its
deployment assertion now requires the command to carry the resolved image
description.

### Image Reference Identity (Local Development)

An executable image has one identity: `host/namespace/repository@digest`. The
registry resolve route returns the catalog `host` alongside the resolution, and
the Console deployment controller confirms the returned reference against
`host/namespace/repository@digest`.

The previous check compared the returned reference against a locally assembled
`repository@digest`. Because Control Plane resolves the full host-qualified
reference, that comparison rejected a correct server response and the Console
reported `workspace_registry_resolution_unconfirmed` after a successful
resolution. The controller now verifies the identity the owner reported instead
of deriving a second format, and the browser test fixture uses the same
host-qualified shape.

### Registry Endpoint, Credential And Approved Set (Local Development)

The registry endpoint is an installation fact, not a product default. Cloud has
no built-in registry host: `OPL_WORKSPACE_REGISTRY_HOST` comes from the
instance's deployment values, and its shape is validated before use.

Which repositories are approved for deployment is also an installation fact, so
it is declared in `OPL_WORKSPACE_REGISTRY_REPOSITORIES` as
`<namespace>/<repository>` entries. The catalog reports that declared set and
never enumerates the registry.

The enumeration was removed because it cannot answer the question. Measured on
the installation's own credential:

```
GET /v2/oplcloud/chaokang_agent_ibd/tags/list  -> 200, real tags
GET /v2/_catalog?n=100                         -> 200, {"repositories":[]}
```

The same credential that reads a repository's tags returns an empty catalog, so
an empty result is indistinguishable from an installation that approved nothing.
The declaration is also the narrower statement: it names what is approved rather
than everything that exists. Every declared entry passes the same namespace
boundary and repository validation as a lookup, so a declaration cannot widen
what may be browsed; a malformed or foreign-namespace entry fails startup, and a
configured registry with no approved set fails startup rather than reporting an
empty catalog.

An installation that configures no host has no image-selection capability rather
than an anonymous one. All three registry routes answer `503`
`workspace_registry_unconfigured`, and the catalog is never constructed. A host
that is present but invalid, or a credential pair with only one half set, fails
startup, because that is a misconfigured capability rather than an absent one.
The cataloged-namespace boundary stays server-side, so configuring an endpoint
never widens which namespaces may be browsed.

This replaces the previous fallback to a hardcoded host default, and it removes
the earlier assumption that a credentialed registry always answers an anonymous
catalog request with 401. Reading an unauthenticated `_catalog` from
`uswccr.ccs.tencentyun.com` returns `200` with an empty repository list, so an
empty catalog cannot be distinguished from a missing credential by response
shape. An anonymous account list is therefore no longer a possible reading of
the image-selection surface.

Focused evidence: `go test ./internal/server/ -run TestRegistry` passes 9 cases,
including the configuration matrix (absent host, invalid host, invalid or
foreign-namespace declaration, half-configured credential pair, complete
configuration) and a case proving the catalog reports the declared set for both
an unscoped and a namespace-scoped request. The client contract suite covers the
removed enumeration's absence indirectly: the client exposes only tag listing
and digest resolution, and its error mapping is exercised through the tags
route.

| Source-check evidence | Exact value |
| --- | --- |
| Command | `npm run verify:local` |
| Result | exit 0; source/browser checks pass, all service packages compile, database-free tests pass, git whitespace clean |
| Focused browser run | `npm run test:browser:operator-resource-read` — 13 passed |
| Local limitation | No PostgreSQL instance was available, so `TestPostgres*` cases were not exercised in this run |

#### Instance obligation

The instance must supply `OPL_WORKSPACE_REGISTRY_HOST`,
`OPL_WORKSPACE_REGISTRY_REPOSITORIES` and the browse credential, injected to
Control Plane from an approved Secret store. The node pull credential stays in
the runtime environment under its own binding. Cloud does not require both to be
the same identity; it requires each consumer to hold only its own permission.

The installation's pull credential was measured to read a repository's tags but
not to enumerate the catalog. The declared set is what makes selection work
under that credential; it does not widen the credential's permissions.

### Registry Catalog Selection Verification

The registry catalog follows the same owner path as the admission route it
feeds. `packages/contracts/go` owns the namespace boundary: only the cataloged
`oplcloud` namespace browses, repository names validate against the same
component pattern family as applications, and the assembled reference must
satisfy the existing digest-pinned workspace image contract. The Control Plane
client speaks the OCI Distribution API v2 (`_catalog`, `tags/list`, manifest
HEAD-style resolution through `Docker-Content-Digest`) with bearer-token
negotiation from the `WWW-Authenticate` challenge; credentials come from
`OPL_WORKSPACE_REGISTRY_USERNAME`/`OPL_WORKSPACE_REGISTRY_PASSWORD` and a
half-configured pair fails startup.

The route exposes three administrator reads under the existing scoped session
protection: `GET /api/operator/registry/repositories`,
`GET /api/operator/registry/tags/{namespace}/{repository}`, and
`POST /api/operator/registry/resolve`. Error mapping is explicit: the
registry's 401/403 is 401 access denied, `NAME_UNKNOWN` is 404 target unknown,
transport and timeout failures are 502, and nothing degrades into invented
catalog rows. The Console registration form gains a
namespace → 列出仓库 → 列出 tag → 解析 digest flow; a successful resolution
writes the digest-pinned reference into the draft image field and the form's
existing validation then gates admission unchanged. Tag references never
validate as images.

Focused evidence: contracts 6 cases, clients 9 cases (namespace filtering,
tag ordering, digest pinning, tag rejection, credential negotiation, invalid
hosts), server routes 8 cases (admin session requirement, namespace
boundaries, resolve identity checks, full error mapping) and the Console form
model 2 new cases — all passing. Before this run the whole Control Plane
server package also passed against an isolated real PostgreSQL with zero
failures.

| Source-check evidence | Exact value |
| --- | --- |
| Implementation base | `2670621d44a19aa6713576103bed211aa7fed29d` (canonical main after PR #552) |
| Command | `GOMAXPROCS=2 GOFLAGS=-p=1 npm run verify:local:full` |
| Result | exit 0; 313 browser/source checks pass; 4 PostgreSQL owners pass with zero skips (`postgresmigrate` 1, ledger 3, control-plane 8, fabric 6 packages) |
| Full log SHA-256 | `40c45fcb6783593b7ae68bd53040c0149a673bd71e01b0451d1f7f80174ac429` |
| Completed at | 2026-09-14 (Asia/Shanghai) |

### Dependency Component Specification Verification

The dependency contract upgrades from a bare name/image pair to a full
per-component specification, driven by the real IBD deployment shape (one
main service plus six knowledge services). A dependency now declares its own
ports (declared inside the application network, never published), TCP or HTTP
health checks, persistent and scratch mounts under the application's data
namespace, its own entrypoint/args/environment, and its own Secret inputs in
the revision-wide SecretBindings namespace. The main component's mounts,
entrypoint and probes never leak into a dependency — the audit finding that
Local applied main's mount/entrypoint to every dependency is closed by
construction: dependency containers run only what their own spec declares.

Fabric Local-Docker runs dependencies exactly from their spec (`--expose`
for declared TCP ports, per-dependency env/entrypoint/args, per-dependency
mounts and tmpfs) and readback gates dependency readiness on the first
declared TCP/HTTP check against the container's real network address.
Fabric Tencent renders per-dependency Deployments with their own command,
args, env, tcpSocket/httpGet readiness probes, workspace-data subPath mounts
and memory-backed scratch emptyDirs; dependency ports appear as container
ports without publication. CP admission validates the full dependency spec
through the existing revision contract, so hand-registered and
registry-selected revisions obey the same bounds.

Focused evidence: contracts dependency-spec tests (full spec accepted, 13
rejection cases, revision-level propagation), Local
`TestLocalDockerApplicationRuntimeDependencyRunsOwnSpec` (spec-driven args,
no main inheritance), Tencent
`TestTencentApplicationManifestDependencyOwnSpec` (command/args/env/probe/
ports/mount per spec). The whole CP server package passed against isolated
real PostgreSQL; the whole Fabric package passed with its PostgreSQL URL.

| Source-check evidence | Exact value |
| --- | --- |
| Implementation base | `929ba15a` (canonical main after PR #553) |
| Command | `GOMAXPROCS=2 GOFLAGS=-p=1 npm run verify:local:full` |
| Result | exit 0; 313 browser/source checks pass; 4 PostgreSQL owners pass with zero skips |
| Full log SHA-256 | `295c023157ee96b7e6d21ffdd39b0a4491126f601e48d8f8b9a5d16ecea682dd` |
| Completed at | 2026-09-14 (Asia/Shanghai) |
| Source consistency | Clean tracked worktree at the recorded HEAD; docs-only closeout followed. Two earlier full runs failed on `node:22-bookworm-slim` digest pulls being reset by the network; the same digest pre-pulled by exact reference, and the passing run consumed it without a fetch. Those failed attempts are retained separately and do not qualify this result. |

IBD image locks are maintained in
[ibd-image-lock.md](./delivery/ibd-image-lock.md): all seven TCR repositories
(`chaokang_agent_ibd`, `ibd-ragflow`, `ibd-tei`, `ibd-es01`, `ibd-mysql`,
`ibd-minio-silo`, `ibd-valkey`, tag `20260909-verified`) now carry server
digests identical to the bundle manifest's frozen `image_id` values, pushed
blob-by-blob from the verified delivery archives without rebuilding. The
`oplcloud` namespace is private; browsing uses
`OPL_WORKSPACE_REGISTRY_USERNAME/PASSWORD` and node pulls use the
installation-level pull secret.

### Original IBD Runtime Alignment (Local Development)

An isolated development worktree based on `ff957a65` fixes the original IBD
bundle as reference. The bundle's own verify command passes all 74 file checks
and eight image archive checks without mutation. New generic source implements
component-scoped non-secret files, versioned Secret file/env consumption,
dependency readiness ordering and original-command continuation, authenticated
shell/exec probes, explicit execution requirements and scratch semantics. No IBD
service names, repository list, restore algorithm or model credentials are added
to the product runtime. `GOMAXPROCS=2 GOFLAGS=-p=1 npm run verify:local:full`
passes 313 source/browser tests, compile/typecheck/lint/build, and all 18
PostgreSQL/Docker packages with zero skips. The final run completed at
`2026-09-15T06:29:05.131356+00:00` and retained the same source-file fingerprint before
and after execution: `6e71ca89557248e8a6f762c86513bb743044cb3ded9516ec6240d21346bdd32a`. Its log SHA-256 is
`1d8349541bacf1b7c134a6bddd523aad55d01ff2981a6c858094c8386dc62d9e` (`verify-local-full-closed.log` and result JSON below).
That fingerprint qualifies the recorded run, not subsequent Console/CP changes
or the opt-in business-verifier hook. An earlier diagnostic run
exposed expired hard-coded test periods; test fixtures now use a current valid
monthly period with the existing month algorithm. Production entitlement checks
are unchanged, and boundary tests still reject access at paid-through expiry.

The opt-in `application_reference` test tag compiles separately from ordinary
verification. Its input validation tests pass; trying the prepared incomplete
reference stops at the missing model Secret before any resource write. A small
real-Docker check also verifies approved seccomp JSON readback; a separate
isolated original Valkey image verifies the credential-file transport and
unchanged memory policy without mounting the original knowledge volumes.

On 2026-09-15, the user selected `glm-5.3-flash` with provider credentials
from `/Users/huangrende/.codex/config.toml`. The original publisher helper
produced an isolated model-only Secret; the source file was not modified.
The exact-reference runtime test passed startup, suspend, resume, and deletion
in 568.94 seconds. The first run exposed MySQL data-dictionary incompatibility
on case-insensitive host storage; rerunning on an isolated case-sensitive APFS
test disk passed without changing database settings or original data.
Evidence: `runtime-current-model-case-sensitive.log` in the directory below.
The subsequent original-image business attempt with `glm-5.3-flash` passed:
one submitted question, eight retrieved chunks, four cited evidence cards,
226 SSE snapshots including 220 partial-answer updates. The final SSE result
equals the HTTP answer, all numbered citations resolve to quoted evidence,
and the HTTP receipt equals its container artifact. Run identity:
`345f21db-b394-43ca-abd0-7768020c2000`. Evidence is under
`business-verification/attempt-20260915T085400Z/` in the directory below.
The external publisher verifier copy adds only the missing allowlist entry for
`opl-response-receipt.json`, already requested by its own validator; original
bundle bytes and validation assertions remain unchanged. This is Fabric/runtime
business evidence, not Console-to-Control-Plane replacement or clinical quality
qualification. Original cold data
restoration remains application-owned; pre-seeded qualification data is not a
completed Control Plane restore feature. Tencent rejects the reference's
currently unsupported init/custom-seccomp/exact-tmpfs requirements. There is no
new Candidate or Instance deployment claim. Development evidence lives at
`output/ibd-baseline-20260915/` in the canonical checkout. Transient local test
inputs under that directory's `private/` tree and its case-sensitive scratch
disk image are not retained.

### Application Deployment Input Delivery (Local Development)

The Console now renders the existing registry repository/tag lists as selectable
options and admits complete publisher revision JSON without reducing dependency
or execution fields to the simple form. Deployment forwards non-secret
`environment/files` and exact `name/secretRef/version/key` bindings through the
existing CP endpoint. Switching Workspace/session clears scoped configuration;
changing registry selection clears the old resolved image. No IBD-specific
runtime, Secret-value upload API, or new persistence schema was added.

CP rejects unknown revision/configuration/binding properties, binds Secret
references to the accepted command digest, and retains its OPL credential
ownership. Focused HTTP tests cover default OPL installation followed by generic
replacement, exact Fabric inputs, unchanged purchase/resources, isolated data
bindings, ordered predecessor retirement, command replay conflicts, and
preflight failure without predecessor interruption. These tests use a Fabric
fixture; the Console Playwright tests use mocked API responses. They do not
establish an uninterrupted Console→real CP→real Fabric→IBD qualification.

`GOMAXPROCS=2 GOFLAGS=-p=1 npm run verify:local:full` passed on
2026-09-15: 316 source/browser tests and 18 PostgreSQL/Docker packages, zero
skips. The source fingerprint was unchanged throughout verification:
`fc396bed20180288df754879ec44f96db6a1c4c03f4f833488a3a4590751dfc9`.
The full log SHA-256 is
`a52e1317738a22394a035ca8c67c9d4eab72beb4aa4493dec9aaf5995784e4d6`.
Evidence for the current full regression is recorded under
`output/product-replacement-20260915/` in the canonical checkout.
The real integrated replacement and Tencent execution requirements remain open.
The portable Local provider requires Linux 5.14+ ext4/XFS project quota; this
macOS/Docker Desktop host is not a qualifying storage host. Installation can use
the publisher's frozen knowledge restoration materials; a new generic product
Restore platform is not a prerequisite to this replacement acceptance. Pre-seeded
data tests still do not qualify any product Restore API. This host limitation does not require purchasing new resources; actual cloud
qualification uses an existing Workspace through its Instance owner.

### Component Compute Envelope (Local Development)

Two gaps that only surfaced when a real hosted application was attempted are
closed at their owners.

- **Component compute envelope.** `WorkspaceApplicationRevision.Compute` and
  `WorkspaceApplicationDependency.Compute` declare one component's CPU and
  memory request and limit. Admission rejects a negative value, a request above
  its own limit, and a value beyond the per-component ceiling. The Tencent
  provider translates the envelope into the container's `resources`; the Local
  provider translates it into `--cpus`, `--memory`, `--memory-swap` and
  `--memory-reservation`, so a local run cannot hide an overrun in swap that the
  hosted provider reports as an eviction. An undeclared component keeps the
  previous unbounded shape and is never given invented bounds.
`GOMAXPROCS=2 GOFLAGS=-p=1 npm run verify:local:full` passed on 2026-09-16:
316 source/browser tests and 18 PostgreSQL/Docker packages, zero skips. Source
fingerprint `081f4acbebcf14590fb3fd430f36c833e4ca6569d169095a1e85a667d6d2463a`; log SHA-256 `82ee04b2e9e2ba90c36f6eb93828a6027f4851af0a063536eb4fb6b4ae1c8b37`; evidence in
`output/application-entry-and-resources-20260916/`.

This does not prove that a target node has enough allocatable memory for a
declared envelope, or that any real application fits the basic package. Those
need the protected Instance readback, and no production state was changed.

### Unified Application Entry (Local Development)

One gateway serves both the OPL App and every admitted application, so adding an
application adds no DNS record, certificate, or load balancer.

- The executing provider reports one resolved entry: `WorkspaceApplicationEntry`.
  An installation gateway publishes `ServiceName` and `Port` for the destination
  it proxies to; a provider that binds an endpoint itself publishes `URL`.
  Exactly one shape is admitted, and an entry exists only once it is ready.
- The Tencent adapter creates no entry object of its own: the Ingress and the
  public-entry NetworkPolicy are gone, because the installation's gateway is the
  publisher and the application NetworkPolicy already admits it.
- The Control Plane owns the route. `workspaceEntryUpstream` resolves the single
  upstream — the current application's reported destination when it publishes an
  entry, otherwise the launched workspace runtime — and `workspaceGatewayEntryURL`
  is the one place the customer-facing route is composed.
- The historical refusal to proxy a Workspace that had deployed an application
  (`workspace_application_entry_required`) is removed, together with the
  provider-prefix and fixed-port assumptions in `workspaceServiceTarget`.

`GOMAXPROCS=2 GOFLAGS=-p=1 npm run verify:local:full` passed on 2026-09-16:
316 source/browser tests and 18 PostgreSQL/Docker packages, zero skips. Source
fingerprint `63f74d04bd1dabbde240824452f521540dc85696fca9e291a0d4337cd9a11e90`;
log SHA-256
`0468d448af32f0fb7ce075b01300ab10b6fdf4d777731bd527fde7cee8706675`; evidence in
`output/entry-unification-20260916/`.

This does not prove that a target installation serves the route end to end: the
application must answer on its declared entry port and work under the workspace
route's path prefix. That needs the protected Instance readback, and no
production state was changed.

### Retained Runtime Entry Projection Repair

The retained full-Launch status and Runtime repair consumers now accept the
installation-gateway observation shape introduced by `995ea522`: Fabric reports
the observed Service without a customer URL. Control Plane composes that route
through `workspaceGatewayEntryURL`; provider-published endpoints are preserved.
Previously the remaining nonempty-URL guards rejected otherwise valid Runtime
readback, making Console entry and credential metadata unavailable. No provider,
Secret, entitlement, persistence schema, or public DTO shape was changed.

The status HTTP regression first reproduced HTTP 502 with the valid URL-empty
observation, then passed after the fix. Coverage includes ready/unready state,
provider URLs, absent/destroyed Runtime, identity/check rejection, credential
redaction and read-only projection. Repair tests cover both endpoint shapes,
persisted entry, replay without repeated activation/receipt, and rejection of
unconfirmed health or identity. The focused owner/access/repair suite passes.
The full local command ran on 2026-09-17: 319 source/browser tests and all
Control Plane, Ledger and migration PostgreSQL packages passed with zero skips.
It failed in the unchanged Fabric
`TestLocalDockerApplicationRuntimeEndToEndNonOPLApplication`: its Gateway-only
credential declaration still leads to a bind mount for an ungenerated
`opl_webui_password` file. Both that fixture and its Fabric implementation match
base `22b17336`; this failure is not repaired or counted as a pass here. Full log
SHA-256: `1293c2ba871c6a6da5e5bed49f15a5ac92b0b38143c093dc3d7ee9b35a96479a`.
Evidence is retained under `output/retained-runtime-entry-fix-20260917/`.
After `npm ci` restored the locked dependencies and the matching Playwright
Chromium was installed, `GOMAXPROCS=2 GOFLAGS=-p=1 npm run verify:local` passed:
319 source/browser tests, typecheck, lint, Console build, Go compilation and
required database-free checks. The four changed source/test files were unchanged
throughout both runs. Locked-dependency log SHA-256:
`6dd0c44f56144fdc15195a1c88b96073e05f526610f8e914449407a5f18d61aa`.
[PR #559](https://github.com/gaofeng21cn/one-person-lab-cloud/pull/559) CI run
`35182897650` independently passed `dependency-review` and `validate` for source
commit `33baf54f`; this evidence update changes documentation only. The separate
Fabric failure above remains an open full-qualification gap.

This is source-check evidence only. No Candidate was built, Product Release
published, Instance deployment performed, or customer Runtime repaired. The
Instance must separately deploy the qualified fix and verify status, entry and
owner-only credential access before claiming the reported outage restored.

### Pre-PR Replacement Safety Review

The pre-PR review reproduced a component configuration file colliding with a
data mount while both admission and Local preflight accepted it. Unified
component target validation now rejects duplicate mount destinations and file
ancestors of other mounts/files before predecessor suspension. Directory mounts
containing file inputs remain allowed. Main and dependency regression cases
cover these boundaries.

Tencent execution rejection remains intentional and tested before mutation.
The original reference seccomp source contains Docker/Moby conditional profile
fields; it cannot simply be declared an already-installed Kubernetes profile.
The Init and exact scratch requirements also need provider/installation
implementation and target-node evidence. No target Workspace was replaced by
this source delivery. The final pre-PR full regression passed 316 source/browser tests and
18 PostgreSQL/Docker packages with zero skips at `2026-09-15T11:21:55.859964+00:00`.
Its source fingerprint remained `e32b8912b59e31df031354e288ec4f53884d1c19a684854560b1b7e48fe0122e` throughout the run;
log SHA-256: `f90314be28f02e9d0315a4b8dbf8c6ecc7772bd0cebe14a898bd51c28db5b820`.
Evidence is retained in `output/pr-delivery-20260915/` of the canonical checkout.

## Evidence Matrix

| Layer | Retained evidence | What it does not prove |
| --- | --- | --- |
| Audited source baseline | Documentation reconciliation inspected remote `main` at `efb5f07b` on 2026-09-07, including merged Console UX-03 and Support retirement | Deployment, public Release, production acceptance, or a permanently current `main` identity |
| Local Console simplification | On 2026-09-09, the local diff over `b1d811ea` removed unused presentation helpers, route sensitivity metadata and duplicate selected-Key ID state; 39 focused route, API lifecycle, Gateway Usage and logout/Secret tests plus `npm run verify:local` passed | Canonical merge, Candidate qualification or deployment |
| D1 local finance development | The 2026-09-08 worktree on `b1d811eab2a2fcff5d77b4cf9685e801d7681360` passes `verify:local:full` with zero PostgreSQL test skips, 6 HTTP business chains plus 11 reservation/CAS cases under race, and focused Renewal/Wallet recovery race tests | Canonical merge, actual Gateway adoption, a released Candidate, provider capacity, or production financial qualification |
| D2 local reconciliation and customer billing | The retained 2026-09-08 cumulative worktree passes `verify:local:full` with zero PostgreSQL skips; original purchase/renewal/refund HTTP chains, pending and recovery, exact receipt lookup beyond 10k history, historical schema-2 financial readback, race checks and desktop/mobile billing tests pass | Canonical merge, actual Gateway adoption, a deployed Candidate or Tencent/TKE production qualification |
| D3 local Launch recovery and capacity | Provider-neutral original-result recovery, bounded scheduling, and failed-Launch closure are implemented. Focused business, HTTP, restart and race tests plus `verify:local:full` pass; exact Key identity, five resource absence facts, original-account refund budget, and Ledger closeout receipt are covered | Actual Tencent cloud capacity, Instance adoption or deployment |
| D4 local lifecycle | Expiry stops original Runtime use; explicit recovery preserves original period, transaction and resources; service-authorized Delete continues after restart and confirms residual absence plus Receipt. Focused PostgreSQL, race, HTTP and desktop/mobile business tests plus `verify:local:full` pass with zero required PostgreSQL skips | Actual Tencent/TKE lifecycle, production Gateway adoption or deployment |
| Native Sub2API 0.2.4 integration | The unmodified official image passes isolated debit/refund/history, lost-refund-response recovery, historical unverified-debit rejection and concurrent full-amount debit checks. Focused PostgreSQL business and race checks cover Key retention and once-only monetary dispatch; the exact source snapshot and full-check results are retained below | Production adoption, real customer-money or provider qualification; older patched-Gateway evidence is historical |
| Public endpoint | On 2026-08-31, `https://cloud.medopl.com/` and `/api/healthz` both returned HTTP `200` | Login, purchase, Workspace lifecycle, provider health, or billing correctness |
| Local runtime | Retained 2026-08-19 Linux/arm64 runs exercised customer-owned and platform-owned Local-Docker Workspace paths, including real model use and restart | One exact-current clean-host create/read/use/delete journey |
| Public Product Release | `v0.1.7`, product SHA `a59bde68397528186a5220f73195fa1f3eda311b`, GHCR digest `sha256:e64504731f8b61c0864cf59faa647a1150e8a2a5eada34b26faf3a5487d28e8f`, five public assets | Current `main`, the current Candidate/owner-topology format, or current Instance qualification |
| Medopl Instance | The 2026-08-30 `workspace-private-state-repair` receipt passed for two existing Workspaces and recorded zero-mutation post-repair readback | A fresh Workspace purchase, full lifecycle, rollback, or qualification of current `main` |
| D5 local image lifecycle | Catalog-fixed replacement, renewed entitlement, persisted recovery, current-generation Pod digest readback and exact CRI cache retirement pass local source/full PostgreSQL/Docker regression. An isolated real containerd verifies preview, protected alias, removal and replay; Instance source passes 345 tests and 18 workflow checks | Production rollout, actual CVM cache/space reclamation, TCR deletion or deployed maintenance permissions |
| NodePool maintenance | Source implements guarded image-GC configuration, taint recovery and bounded redacted readback | Production mutation; the retained evidence here contains no matching Instance execution receipt |
| Operator observations | On 2026-09-10, `verify:local:full` passed with PostgreSQL and Docker integration and zero required PostgreSQL skips. Complete mapped-account Gateway totals, typed resource observations, physical Runtime inventory/business reconciliation, separate service/image readiness, and desktop/mobile operator browser chains are covered | A deployed Candidate, repaired historical resources, available Tencent balance, or permission to delete unmatched objects |
| External resource reconciliation | On 2026-09-11, focused Workspace-loss, PostgreSQL CAS/audit/race, Fabric power/readback and retained-operation ownership tests pass. `verify:local:full` passed its source/browser/Docker and other Go modules but hit two Control Plane transaction-start deadlines; both unchanged tests passed three isolated repetitions, then all 2,511 Control Plane tests/subtests passed against a fresh local PostgreSQL with zero test skips. Final operator browser checks pass 9/9. Successful Workspace bindings are scanned even without old child projections; confirmed loss closes access/auto-renew and suspends the original Runtime, while unknown facts preserve entitlement and verified CVM observations survive missing TKE associations | Exact-Candidate Instance adoption, actual cloud refund/destruction history, repaired historical Runtime controllers or customer-money qualification |
| Observation state semantics | On 2026-09-11, `verify:local:full` passes source, browser, build, all Go modules, PostgreSQL and Docker business checks with zero required PostgreSQL skips. Focused tests prove stopped/pending-deletion/deleting CVMs retain their independent TKE association failures, per-Workspace immutable targets and controller ownership distinguish version differences from unverified images, and resource refresh issues no business writes. Delete navigation assertions wait for the list owner read to commit; the complete lifecycle browser group passes 23/23 | A deployed Candidate, confirmed CVM refund/destruction cause, a configured Instance scan interval, or new-purchase qualification |
| Local Console overview trend | On 2026-09-18, the local diff over `25b26ee6` aggregates the customer overview trend from typed receipt micros instead of the presented amount text, pages Ledger receipts by cursor until the 14-day window is covered, and renders unavailable, empty and unconfirmed amounts separately from a confirmed zero. Four new source tests and four new browser tests fail on `25b26ee6` and pass on the fix; the full source (217), browser (110), typecheck, lint, build and `npm run verify:local` checks pass | Canonical merge, a deployed Candidate, Instance adoption or production qualification |
| Local Console public-asset delivery | On 2026-09-20, `https://cloud.medopl.com/` served the home page but `/opl-cloud-overview.jpg` answered `200 text/html` with the SPA `index.html`, so the home `<figure>` rendered nothing; `consoleStatic` only special-cased `/assets/*` and `/opl-app-icon.png` and let every other path fall through to the SPA fallback. Root-level requests with a file extension are now resolved to that file in the Console dist (local path check rejects traversal), so the overview image is served as `image/jpeg` and unknown assets return `404` instead of HTML. `TestConsoleStaticDelivery` covers the JPEG content type/cache and the no-fallback `404`; the focused Control Plane server suite passes apart from the pre-existing PostgreSQL tests that require `CONTROL_PLANE_TEST_DATABASE_URL` | Canonical merge, a deployed Candidate, Instance adoption or production qualification |
| Local Console public entry copy | On 2026-09-20, the public home and login surfaces no longer render the 「当前为 Pilot，账户由管理员开通…」 notice; the `access-pilot` and `login-footnote` elements, their CSS, and the fixture assertion in `tools/console-browser-qa.ts` are removed together. Login keeps the account-provisioned hint inside the login panel identity row. `npm run typecheck`/`lint`, `npm run build` (no Pilot string remains in the bundle), `tests/ui/console-production-dist.test.ts` and `tests/ui/console-browser-acceptance.test.ts` (3/3) pass | Canonical merge, a deployed Candidate or Instance adoption |
| Local Console layout alignment | On 2026-09-18, the local diff over `822a010b` audited all 15 Console routes at 1440/1280/1024/390 plus an 18-width sweep (390–1600) for content leaving its box. Fixed: panel table containers overhanging the panel by 16–18px on 8 pages; the Workspace resource table collapsing its action column below the 「查看资源」button and its 「Key 累计费用」header below its text; `.data-list` value columns squeezed to 0 by the fixed 260px label column; a mobile resource-detail digest producing 138px of page-level horizontal scroll; the workspace plan option exceeding its column by 92px; a 4px identity-panel title offset; truncated account status badges and identity emails; customer Workspace rows overflowing the panel at 821–1095px; the squeezed API usage action column and keys table/filter rows. `tests/ui/console-layout-alignment-browser.test.ts` (new, registered in `test:browser:suite`) reproduces these findings on the pre-fix sources and passes after the fix; `npm run test:source` 217/217, `npm run test:browser:suite` 112/112, `tests/ui/console-browser-acceptance.test.ts` 3/3, `npm run typecheck`/`lint`/`build` and `npm run verify:local` pass | Canonical merge, a deployed Candidate, Instance adoption or production qualification |
| Local Workspace settlement trend | On 2026-09-18, the customer overview trend was rebuilt over the local diff at `d1c446e5` as a read-only Control Plane projection of confirmed Workspace wallet movements. The initial UI amount-parsing and three-receipt-window defects were corrected before `d1c446e5`; the remaining defect at that revision was receipt-based financial interpretation, which omitted a reversed Renewal's original debit because no success receipt existed. `projectWorkspaceSettlementTrend` reuses `projectBillingSettlements`, confirms each purchase, renewal and business-refund movement against the Sub2API balance record (`used_at` effective time, wallet user, signed amount, stored code), attributes refunds through the recorded original order, and returns a fixed 14-day Asia/Shanghai window with pre-bucketed days. The unknown-result rule was then repaired: a Renewal worker persists `ChargeAttempted=true`, dispatches the debit and only afterwards learns the result is unknown, so the row stays in `debit_pending` while the wallet may already have moved and its balance record is not readable yet. The trend previously called that in flight and returned `chargedUsdMicros=0`, `inFlightCount=1`, `complete=true`, which the page rendered as "尚未产生资金变动". It now reports in flight only when the owner's dispatch facts prove no fund request was sent, and treats any dispatched movement without a readable wallet record as `unconfirmed` with `complete=false`; Console labels that window as its confirmed part and no longer claims no charge. `TestWorkspaceSettlementTrendKeepsAnUnknownRenewalDebitUnconfirmed`, `...KeepsAUnknownDispatchedRefundUnconfirmed`, `...KeepsAUnknownDispatchedAdjustmentUnconfirmed` and `...RecoversAfterTheFundEvidenceArrives` fail on `9de1878e` with `inFlightCount=1, unconfirmedCount=0` and pass after the fix, alongside `...CountsNothingForAnUndispatchedRenewalOrder` and `...KeepsAnUndispatchedOrderInFlight` for genuinely undispatched processing. Independent review then added the retired Workspace-delete refund (retained on its own operation row rather than a business-refund adjustment) and the owner refund limit, so an over-refunded order stays `unconfirmed` instead of inflating the total. New `TestWorkspaceSettlementTrend*` owner tests cover purchase/renewal charges, the charged-then-fully-refunded Renewal, the retired delete refund in both confirmed and unconfirmed states, refund overflow, Shanghai-day bucketing, cross-window refunds, partial and multiple linked refunds, duplicate codes, repeat reads, deleted Workspaces, ambiguous or missing fund evidence, real zero versus empty, account isolation and no Ledger/Fabric/fund mutation; `npm run verify:local` passes, and from a non-symlinked checkout `verify:local:full`'s Control Plane PostgreSQL and migrations lanes pass including all eleven Sub2API authority chains; its remaining `services/fabric` Local-Docker failure (`bind source path does not exist: /var/folders/...`) reproduces unchanged on `25b26ee6` and `9de1878e` and was initially attributed to macOS. A later real-path reproduction identified undeclared, uncreated application credential bind sources instead; the Local-Docker credential declaration row below records that correction. Running the Go tests from a symlinked `/tmp` worktree instead fails those chains with an empty `Sub2API authority exited before READY`, because the fixture compares `process.argv[1]` with `import.meta.url` and macOS resolves `/tmp` to `/private/tmp`; that is an environment artifact, not a test result. `npm run verify:local:full` passes the source, browser, build, Control Plane PostgreSQL and migrations lanes and fails only `services/fabric` Local-Docker integration (`docker_run_failed ... bind source path does not exist: /var/folders/...`), which reproduces unchanged on `25b26ee6` and is a macOS Docker bind-mount limitation, not this change | Canonical merge, a deployed Candidate, Instance adoption or production qualification |
| Partial Workspace settlement evidence | On 2026-09-21, a real Sub2API HTTP fixture connected to the customer settlement route reproduces HTTP 502 on `50520e27` when one confirmed transaction is mixed with a legacy record lacking its applied amount. The read-only observation now retains the confirmed part and reports the legacy code as unconfirmed with `complete=false`, using one history scan. The same fixture still fails the strict financial lookup, conflicting rows cannot be restored by later valid-looking rows, and transport or malformed pagination remains unavailable. Focused client and settlement-owner tests pass, and the complete Control Plane suite passes with isolated PostgreSQL: 2,816 tests/subtests, no failed or skipped tests, including the actual Sub2API authority fixtures. The unrelated browser portion of `verify:local` could not start because this host lacks its pinned Playwright Chromium binary; no Console code changed. | Instance adoption and proof that the observed production 502 has this exact cause; no financial or provider mutation is authorized by these source tests |
| Local-Docker application credential declarations | On 2026-09-18, source `ad8d23d3` passed `npm run verify:local:full`: 216 Node source tests, 113 browser-suite tests, builds, database-free Go checks and all four PostgreSQL module suites, with zero required test skips. Separate Console acceptance passes 3/3. The real Docker application lifecycle passes login, session/data retention, replacement and deletion. The old missing-bind-source failure reproduced in a real shared directory: the adapter mounted uncreated WebUI credential files for a Gateway-only declaration, and the fixture omitted credentials it actually consumed. Mounts now follow explicit credential targets; four credential combinations fail before the fix and pass after it. An earlier whole-host capacity collision was isolated by serializing shared Docker verification; no assertions, capacity checks or test selection were weakened. Full-gate log SHA-256: `8ac1b8848dba724a91b3cc0aa9d5e2c77237a65f8dd946c540df4ed1877e8ffd` | Production adoption, a deployed Candidate or Instance qualification |

Evidence applies only to the exact identity and layer named in its row. An older
Candidate or Instance receipt cannot upgrade current source to release or
production-ready status.

Confirmed `data_deleted` is rendered as a known data-loss state in customer and
operator Workspace lists. The lifecycle model and browser resource-refresh chain
verify that the synchronized state replaces the prior running label.
Renewal readback reports lost storage as reclaimed even before the original paid
period expires; the customer recovery chain keeps opening and renewal unavailable.

The public Release list was read again through the GitHub API on 2026-09-30 and
still contained only `v0.1.7`. Endpoint, Local runtime and Instance rows retain
their original observation dates; this documentation audit did not requalify
those environments.

## Current Product Cut

The retained administrator-provisioned surface maps one Console user to one
Account and Sub2API identity/wallet. Its Account may own multiple Workspaces and
its legacy catalog exposes Basic and Pro. The extracted cloud-identity surface
uses CloudIdentity/Tenant, Resource Catalog quotes and Workspace-owner readback,
with default OPL App or an optional built Agent. Sub2API remains the sole owner
of spendable balance, Keys, routing and usage.

The [implementation map](implementation-architecture.md#console-source-truth)
owns the two Console API paths and service boundaries. Fabric provides
Local-Docker and Tencent/TKE adapters; the medopl Instance selects/configures
Tencent/TKE and owns its installed topology. Cloud carries no production domain,
Secrets or provider profile as defaults. The new source surface is not evidence
that the current public Release or installed Instance exposes it.

Public registration, customer-operated payment or top-up, shared multi-user
Workspaces, high availability, and GPU are not current customer capabilities.

The Console customer experience and Support retirement are merged in PR #530.
Current interaction rules belong to the product experience guide, and browser
state boundaries belong to the Console implementation reference. Retained
Support data is historical custody, not an available ticket capability.

## Implemented Capability

- Console BFF and the extracted domain-owner modules implement the bounded
  source capabilities described above. The cloud-identity Workspace list/detail
  reads only its Workspace owner; owner-backed maintenance remains an open gap.
- Retained Workspace Launch is a durable Control Plane operation that coordinates the
  Sub2API Key and debit, Fabric stages, Workspace activation, and one Ledger
  purchase Receipt. Exact replay and bounded recovery preserve the original
  identities and fail closed on unproven provider results.
- Workspace Delete is permanent and performs no refund or wallet mutation.
  Its background worker preserves the original owner intent without a customer
  credential, polls delayed compute absence, and waits for the deletion Receipt.
  Fabric converges exact standalone Gateway Secret and asymmetric PV/PVC residue.
- Renewal authorization and recovery eligibility are exposed through Control
  Plane and Console. Unpaid expiry closes access and stops the original Runtime.
  Explicit recovery confirms the original charge, provider renewal and Runtime
  readiness before entitlement; balance changes alone never restart service.
  Customer-facing purchase/details explain pre-expiry backup responsibility.
- Fabric owns compute, storage, attachment, Secret, Runtime, provider mutation,
  and authoritative readback. Local-Docker enforces immutable Workspace images,
  cgroup limits, and project-quota storage on a supported Linux host.
- Tencent/TKE supports protected prepaid provisioning, typed delayed-readiness
  recovery, immutable Workspace image catalogs, active-release selection, and
  image-only replacement of an existing Workspace Runtime.
- New and existing Tencent Workspace NodePools have a source-owned, explicit
  image-GC threshold path. Existing pools are changed only through a separately
  confirmed mutation with owner inventory and post-mutation readback.
- Console uses capability-specific controllers for Workspace Launch, access
  Secrets, Delete, Renewal, Gateway budget and usage, customer and operator
  reads, billing/Receipts, Wallet adjustment, and announcements. Customer
  navigation and presentation are task-oriented; the retired Support client and
  its live requests are absent.
- Ledger owns append-only receipts, reconciliation evidence, and the Cloud
  Evidence Index. It does not own spendable balance or provider mutation.
- Candidate tooling builds one `linux/amd64` plus `linux/arm64` image index and
  a checksum-bound asset set including the owner topology from an exact Cloud
  SHA. [Installation](installation.md) owns the exact Candidate/Release formats.

The detailed dependency and operation boundaries remain in
[implementation-architecture.md](./implementation-architecture.md).

## Retained Runtime Evidence

Native Sub2API 0.2.4 integration evidence is retained at
`/Users/huangrende/Documents/ChatGPT/native-sub2api-024-20260910/VALIDATION.md`,
with `cloud.patch`, `source-manifest.json`, exact official image identity and
verification logs. The source baseline is
`8c52625d98fffc06e944efccb539e923ba9f2001`. Four tests against the unmodified
official image cover native debit/refund/history, lost refund response with
read-only recovery, unverified historical debit rejection and concurrent
insufficient-funds full-debit rejection. Focused HTTP/PostgreSQL and race tests
cover retained dispatch reservations, crash/restart, positive audit recovery,
unknown-money non-replay, original-account refund limits, retained Keys and
current Rotation-bound Fabric cleanup. Native amount checks reproduce the
official float64/lib-pq/numeric conversion and reject precision loss before
HTTP. Full source/browser/PostgreSQL/Docker results are recorded against that
source snapshot; failed attempts remain separate from passing logs.

All tests run in isolated local stores and containers. The Candidate archive
and runtime image contain no test account, database, credential or runtime
volume. Instance PR #263 supplies the native read-only capability and audit
checks; production settings, deployment, technical health and any paid
Workspace qualification require their own protected Instance receipts.
Historical patched-Gateway and Key-revocation tests below do not require
modifying the current Gateway or cleaning its Keys.

The D1 local finance evidence is retained outside Git at
`/Users/huangrende/Documents/ChatGPT/d1-delivery-20260908/VALIDATION.md` and its
`logs/` and `sub2api/` artifacts. The business chains use real Control Plane,
HTTP clients, PostgreSQL and Ledger, with explicit Sub2API/Fabric fixtures.
They exercise concurrent partial refunds, account/amount conflicts, response
loss, restart and receipt-only recovery. Isolated real Sub2API evidence is a
historical patched-Gateway layer, superseded by the native 0.2.4 integration decision. Historical
transactions without a verified applied amount remain unverified. The actual
production Gateway image identity and adoption are still an Instance obligation.

D2 local source evidence is retained separately at
`/Users/huangrende/Documents/ChatGPT/d2-delivery-20260908/VALIDATION.md`, with
`cloud.patch`, `source-manifest.json` and `logs/`. Its snapshot includes the
retained D1 changes; it does not overwrite the D1 snapshot. Tests prove two
purchases plus partial refunds, all paid renewal periods after Workspace
removal, missing/conflicting/duplicate receipts, normal receipt progress without
blocking new buyers, manual-review refund recovery, automatic refund accounting,
and schema-2 historical readback. Ledger migration and exact lookup pass with
10,005 retained receipts. The final full local run passes PostgreSQL and Docker
integration with no required skips. Tests use separate local stores and fixtures;
engineering evidence is not written into the production business Ledger.

D3 evidence is retained separately at
`/Users/huangrende/Documents/ChatGPT/d3-delivery-20260908/VALIDATION.md` and its
source manifest, patch and logs. The source baseline is local commit
`0a1a78d6aa2caf4898c1e35c08c30c2a906f371b`, which preserves the verified D1/D2
work before D3. Tests include fifty concurrent PostgreSQL admissions, the 51st
rejection, 10,001 unrelated retained operations, keyset ties, one slow account
while 49 others advance, first-dispatch CAS competition, and late exact debit
confirmation after restart on both provider profiles. Real typed HTTP tests
cover long queue wait, original dispatch, provisioning, ownership and stage
advance. Actual Tencent adapter tests use isolated provider fixtures and
PostgreSQL, not live Tencent resources. The full local gate passes with zero required PostgreSQL skips. After the final
Control Plane recovery edits, its complete suite passes again (2,380 test events
plus the explicitly rerun opt-in 1,000-user data-scale case); focused recovery
and concurrency tests pass under race. Browser tests cover desktop/mobile
original-order return and ordinary administrator recovery without technical
budgets. Test stores and evidence remain outside production state.

D3 local failed-Launch closure is complete. The original operation is frozen by
CAS; the historical implementation revoked exact Key identity. The current
closeout retains Gateway Keys while all
five owner resources must read back absent, the original-account refund shares
the existing reservation budget, and Ledger records the append-only closeout
receipt. Unknown money, Key, or provider ownership remains pending. The
existing Tencent Instance explicitly enables the Launch worker and still
overrides admission to one; adopting this source does not silently change that
setting. Tencent capacity and Instance adoption remain external obligations.

The final D3 closeout evidence is retained separately at
`/Users/huangrende/Documents/ChatGPT/d3-closeout-delivery-20260909/VALIDATION.md`
with its logs, replay-verified patch and source manifest, based on local commit
`72e52e7dfeee86cd7ac37128ad14968e1f9de742`. Real Control Plane HTTP,
PostgreSQL, financial HTTP clients and Ledger exercise response loss, restart,
partial/manual refunds, historical paid orders, ready-before-freeze protection
and new purchase after closure. Resource and Key physical owners in those
orchestration tests are explicit local fixtures. Actual Tencent adapter tests
separately cover delayed Machine ownership, partial CBS binding, independent
Gateway Secret cleanup and queued-order cancellation without releasing an
unknown head. The real isolated patched Sub2API proves exact revocation,
disabled-Key identity lookup, cache-failure recovery, service restart and
fifty-user creation/revocation races. That patched-Gateway evidence is historical;
the current adoption target keeps Gateway unmodified and retains Keys. These tests do not certify production Gateway multi-node
cache convergence or actual Tencent resource capacity.

D4 local lifecycle evidence is retained at
`/Users/huangrende/Documents/ChatGPT/d4-delivery-20260909/VALIDATION.md`, based on
Cloud `4aeb239146165b8612bfcf0300dfc0629992fafd` and Instance
`a30d9ebeb775b6d566758a8909960838be6f7791`. Original-period renewal/recovery uses
real Control Plane HTTP and PostgreSQL with explicit local wallet/provider
fixtures. Focused tests cover response loss, competing processes, repaired
Runtime identity, historical expiry, precise worker wakeup, and existing
WebSocket termination. Delete tests cover no-session background continuation,
late compute absence beyond the old read limit, the historical disabled-Key
revocation path, partial Runtime objects, owner isolation and receipt-only
recovery. The native 0.2.4 decision supersedes that Key cleanup requirement.
Independent HTTP tests validated the Fabric power capability and historical
Sub2API deletion read. Tencent adapter tests use isolated Kubernetes/Tencent IO and persisted
owner journals; they do not access the real provider.

The final `verify:local:full` passes all source/browser/build checks and all four
PostgreSQL owners, including capacity and real Local-Docker integration, with
zero required skips. Fabric's independently retained final whole-module run
contains 1,842 passing test events and no skips. The real Docker path preserves
the original container ID and data in both mounted directories across stop/start.
The final renewal race suite passes 275 cases, including new and existing
connections after a real confirmed renewal. Initial failed verification logs
are retained separately and are not counted as passing evidence.

The Console browser evidence includes expiry/recovery, top-up without automatic
recovery, unknown-response reentry, Runtime readiness, and one-submit deletion
on desktop and mobile. The local Workspace overlay enables the existing monthly
worker; Instance source defaults its interval to 60 seconds while retaining
Bootstrap's disabled workers. Protected environment overrides and actual
adoption require Instance readback. Test accounts, charges, resources and
receipts remain isolated; none is deployment input.

D5 local image lifecycle evidence is retained at
`/Users/huangrende/Documents/ChatGPT/d5-delivery-20260909/VALIDATION.md`, with
source manifests and patches based on Cloud `30ccd1a31e9bc0f9308782798fc6c51ca856ad79`
and Instance `c11001a3c9ab312930fb42919be77548049a0b68`. The final
`verify:local:full` passes all four PostgreSQL owners with zero required skips;
Instance passes 345 tests and 18 workflow validations. Focused business tests
cover fixed catalog targets, current paid renewal, stopped/deleting exclusion,
response-loss recovery, Runtime locking, exact Pod digest and rollback without
changing the global default. Tencent/Kubernetes mutation boundaries use isolated
fixtures; Local-Docker image replacement was not added.

The real isolated containerd run uses the compiled CRI maintenance command and
proves preview without removal, protected alias retention, exact old-image
removal, absence readback and idempotent replay. Its reported image filesystem
usage remains unchanged immediately after removal, so this evidence proves
cache-reference absence, not reclaimed bytes. All test resources are disposable
and outside production. The delivery manifest binds the exact source, executable
and logs. Instance must adopt that Cloud image, configure its read-only Cloud
qualification-evidence credential, qualify the same linux/amd64 Workspace
manifest and execute protected rollout/node retirement before claiming actual
CVM results. TCR deletion is optional and unchanged.

The 2026-08-19 Local-Docker runs covered two ownership modes:

- `customer_owned` created two independent Workspaces, retained them across
  control-service restart, and completed a real model request with one matching
  Sub2API usage increment;
- `platform_owned` created one prepaid Workspace, one Workspace Key, one
  `52,580,000` USD-micros debit, and one linked purchase Receipt. Runtime repair
  retained the confirmed Key, debit, compute, storage, attachment, and Secret,
  and exact replay did not duplicate resources or the Receipt.

The retained evidence is outside Git under
`/Users/huangrende/Desktop/opl-cloud/evidence/2026-08-18-v2` and
`/Users/huangrende/Desktop/opl-cloud/evidence/2026-08-19-platform-owned-repair`.
These runs did not prove final Delete absence and its Receipt on one
exact-current clean host.

The Instance receipt
`opl-instance-medopl/receipts/2026-08-30-workspace-private-state-repair.json`
binds product SHA `eaa1a95bbdc587b5cd49d38fbc0395013821331a`, Cloud image digest
`sha256:625c47b32def08ba3519a5ecb7dddb475a42bd8f082ad691b3c0d45d0f6b784b`,
and its Workspace image digest. It verifies two retained Workspace storage
bindings, private Framework state modes, Workspace image binding, a second
restart, all seven official Packages installed, five professional Packages
launchable, and five shortcuts visible. Its later readback made no Control
Plane, Kubernetes, Package, or Workspace-restart mutation. It is strong
evidence for those existing
Workspaces, not for a new purchase or current-source qualification.

### Portable Managed-TKE Configuration Completeness

The portable Candidate assets now declare the facts their own owners require.
Fabric's `callKubectl` runs `protectedresource.FromEnv().Check` for every
mutating verb, and `Config.Validate` requires `OPL_SYSTEM_COMPUTE_NODE_POOL_ID`,
`OPL_SYSTEM_COMPUTE_MACHINE_ID`, `OPL_SYSTEM_COMPUTE_NODE_NAME`,
`OPL_SYSTEM_COMPUTE_MACHINE_TYPE` and `OPL_FABRIC_TENCENT_TKE_PROVIDER_PROFILE_JSON`,
so `deploy/portable/compose.fabric-tencent-tke.yaml` now forwards them and
`deploy/portable/opl-cloud.env.example` declares them. The same overlay also
forwards every fact the provider's own constructor and readiness check require
(`OPL_WORKSPACE_DOMAIN`, `OPL_CLOUD_IMAGE`, `OPL_WORKSPACE_IMAGE`,
`OPL_K8S_NAMESPACE`, `OPL_IMAGE_PULL_SECRET_NAME`,
`OPL_WORKSPACE_STORAGE_CLASS`, `OPL_TENCENT_PROVISIONER_BIN`,
`TENCENT_DEPLOY_KUBECONFIG_REF`, `RUN_TENCENT_CREATE_RELEASE_EXECUTION`) and
mounts the TKE deploy kubeconfig read-only, because a rendered overlay had
previously delivered none of them to the Fabric container. A rendered overlay
now delivers the whole set with `OPL_FABRIC_PROVIDER=tencent-tke`.
`compose.yaml` now forwards `OPL_WORKSPACE_APPLICATION_DOMAIN` to the Control
Plane, the same name `parseWorkspaceApplicationOriginHost` and Serve's
`ApplicationEntryURL` read, and the template declares it. The focused contract
test `tests/contracts/portable-tke-configuration.test.ts` reads the required
names from the owning Go source instead of restating them. Evidence:
[portable TKE configuration](./evidence/source-checks/2026-09-30-tke-serial-portable-tke-configuration.json).
This is a Cloud source and asset fact; no Candidate image, installation,
provider action or Instance receipt exists yet.

### Default App Console Delivery View

The Console delivery view over a Workspace supports both delivery combinations.
`apps/console-bff/internal/httpapi/delivery.go` previously failed the whole view
with `serve: deployment <id> names no capability version` whenever neither the
current deployment nor the Workspace named a capability version, which is
exactly the default-App deployment; the Workspace page then showed
`delivery_unavailable`. It now reports the Capability and Build layers as
`not_applicable` (`default_app_runtime_release`) while still composing the real
Workspace and Serve facts, and the Agent combination and fail-closed behavior
are unchanged. Evidence:
[default App delivery view](./evidence/source-checks/2026-09-30-default-app-delivery-view.json).

### Candidate Source Ref

A Candidate is a replaceable input, not a formal Release. The Candidate workflow
(`.github/workflows/build-opl-cloud-candidate.yml`) now admits any branch ref
whose head commit is the requested `product_sha`, instead of pinning the
dispatched ref to `refs/heads/main`, so the serial integration branch can build
the Candidate before its work lands on canonical main. The publication boundary
is unchanged: only the repository owner may dispatch (`github.actor` and
`github.triggering_actor`), a non-branch ref is refused, and the Release
workflow still runs from `main`. `tools/validate-product-boundary.mjs` now gates
that guard shape. Evidence:
[candidate branch source](./evidence/source-checks/2026-09-30-candidate-branch-source.json).

### Default App Launch Confirm Step

The Console launch page renders both application combinations from one catalog.
`apps/console-ui/src/app/use-workspace-launch-controller.ts` only failed the
catalog when the approved Runtime Release list *and* the ready CapabilityVersion
list were both empty, which is what a default-App-only tenant presents, but its
state init then dereferenced `availableVersions[0].id` unconditionally. On an
empty ready list that threw during render and the route unmounted to an empty
body, so `/console/workspaces/new` showed nothing at all. The CapabilityVersion
init now falls back to `""` like the sibling Runtime Release init. The confirm
step in `WorkspaceLaunchPage.tsx` had also hardcoded the `Agent 版本` row from
the CapabilityVersion, so a default-App quote rendered an empty version row; it
now labels and reads the `Runtime Release` row for the default App and keeps the
`Agent 版本` row for a built Agent. The browser test
`default App confirm step shows its Runtime Release instead of an empty
CapabilityVersion` drives the real Console through the real BFF with an approved
Runtime Release and an empty ready-CapabilityVersion catalog; it passes with the
fix and fails without it. Evidence:
[default App confirm step](./evidence/source-checks/2026-09-30-default-app-confirm-step-release.json).
This is a Cloud source fact; no deployed Console, Candidate or Instance receipt
exists yet.

### Cloud Owner Topology Installation Contract

Milestone M1 starts with the instance installing the complete Cloud owner
topology, and the product image already carries an executable for every owner.
What was missing was Cloud's own statement of that topology: the portable
environment template named only the legacy three services' variables, so an
instance extending its manifest had to invent the owner set, addresses,
databases and peer identities. `deploy/portable/opl-cloud-owner-topology.json`
now declares each process (owner identity, binary, `OPL_<OWNER>_ADDR`,
`OPL_<OWNER>_PEER_TOKENS`, `DATABASE_URL`, and every owner-specific variable),
`deploy/portable/opl-cloud.env.example` names each variable in a dedicated
section, and `tests/contracts/owner-topology.test.ts` derives the owner set,
identities, listen addresses, binaries and environment names from the owning Go
source, the Dockerfile and the candidate asset contract, failing on any
undeclared or stale entry. The Candidate bundle now declares, copies, checksums
and validates the new asset. The declaration also records that Ledger and
Resource Catalog share the `:8186` default listen address, so an installation
must set both explicitly. Evidence:
[owner topology installation contract](./evidence/source-checks/2026-09-30-owner-topology-installation-contract.json).
This is a Cloud source and packaging fact; no Candidate image, installation,
TKE readback or route activation exists yet.

### Browser-Facing HTTP Surface And Console Identity

The installed product serves the Console from one origin while the Console calls
two processes' APIs, and the Console bundle's mode is baked at image build, so no
deployment can change it. Neither fact was declared.
`deploy/portable/opl-cloud-owner-topology.json` now names every HTTP surface the
Cloud processes register (`/api/` from control-plane, `/api/v2/` from the Console
BFF, `/fabric/`, `/ledger/`, `/objects/`, `/healthz/`, `/readyz/`), states which
of them a conforming installation must route the browser to, and adds
`consoleBundle`: the `VITE_CONSOLE_IDENTITY` argument, its values, its default,
its image label and the API surface each identity requires. The Dockerfile's
runtime stage now declares the same argument and emits
`LABEL opl.cloud.console-identity`, so the identity travels with the digest.
`tests/contracts/owner-topology.test.ts` derives the surfaces from the processes'
own route registrations and the Console's own API path literals and asserts both
directions, and three mutation probes (mis-attributing `/api/v2/`, registering a
new source surface, drifting the runtime-stage default) each fail the suite.
Evidence:
[browser surface and Console identity](./evidence/source-checks/2026-09-30-http-surface-and-console-identity-declaration.json).
The customer Workspace owner-read gap recorded in that receipt is superseded by
the [cloud-identity read evidence](#cloud-identity-customer-workspace-read) below.
Owner-backed maintenance actions remain open.
This is a Cloud source and packaging fact; no Candidate image, installation or
TKE readback exists yet.

### Cloud-Identity Customer Workspace Read

The Console has two identities and each one now reads the customer Workspace
from exactly one owner. The legacy identity keeps the retained Control Plane
projection. The cloud identity reads the Workspace owner, which is the only
owner that can answer for a Workspace the default-App or Agent launch created
through `POST /api/v2/workspaces`. `workspaces-api.ts` projects the owner's
complete cursor list into the Console page model and reads the owner detail,
carrying only the facts the owner returns; the projection validator in
`customer-workspace-read-controller-model.ts` accepts the owner projection only
for the identity that selected that owner and refuses the other owner's
projection in both directions. The list, detail and overview pages render the
owner's delivery model, lifecycle state, resource readiness, application
availability, period end and access URL under the cloud identity, and
`use-console-controller.ts` supplies no Workspace to the Control Plane
credential, deletion, renewal, application-installation and budget controllers
under that identity, so those routes are never addressed for an owner
Workspace. A browser test drives the real cloud-identity Console and records
that it reads `GET /api/v2/workspaces` and `GET /api/v2/workspaces/{id}`, opens
the owner's access URL, and issues zero `/api/workspaces` requests. Evidence:
[cloud-identity customer Workspace read](./evidence/source-checks/2026-09-30-cloud-console-workspace-owner-read.json).
Known gap recorded there: the Console BFF exposes no Workspace-owner renewal,
deletion, credential or application-installation route, so the cloud detail page
offers those controls only once such owner surfaces exist. `npm run
verify:local:full` also passed on this revision: every PostgreSQL-gated owner
suite ran with zero skips against a temporary PostgreSQL 16 container, and the
browser suite, the product-boundary validator, typecheck, lint and the Go module
tests were green.
This is a Cloud source fact; no Candidate image, installation or TKE readback
exists yet.

## Distribution Boundary

The five `v0.1.7` assets and their public API digests match the Release manifest
and `SHA256SUMS`. The Release contains the base Compose file and its historical
Local-Docker overlay, but not the current three deployment overlays, two Fabric
overlays, or current complete Workspace installation contract. See
[installation.md](./installation.md) for the executable boundary.

The runnable product image builds one executable per owner the deployment can
select. `tests/contracts/product-image-owners.test.ts` derives the expected owner
set from `services/*/cmd/server` and asserts the `publisher-build` lane builds
every one of them plus the Console BFF, and that the runtime stage copies them;
the three legacy control services keep their own asserted stages. It previously
verified only `opl-control-plane`, `opl-fabric` and `opl-ledger`, so a silently
dropped new owner would have shipped an image that cannot start that capability.

Current source separates Candidate construction, Local qualification, Instance
qualification, publication, and public readback, and is designed to promote the
qualified image digest without rebuilding it. No hosted cohort has completed
that path for one current Candidate, so a successor Product Release is not yet
proven.

## Readiness

"Basically usable" describes the presently demonstrated administrator-operated
surface. Public Beta requires the Cloud and Instance evidence named by the A-N
work packages in [roadmap.md](./roadmap.md). In particular, current evidence
does not yet close public registration, production lifecycle qualification, data
restore and alert operations, clean exact-Candidate qualification, rollback,
or same-byte publication.
