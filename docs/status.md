# OPL Cloud Current Status

Owner: `one-person-lab-cloud`
Purpose: `replaceable_current_evidence_snapshot`
State: `current_snapshot`

This page reports current implementation and the latest retained evidence. It
is not a work log. Target architecture lives in
[architecture.md](./architecture.md); open outcomes live in
[roadmap.md](./roadmap.md).

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

## Application Hosting Boundary

The baseline below combines canonical main
`a0430b099cc787a8931e54f0bda4a1424f376eb1` with the default installation,
replacement and lifecycle implementation at `46f1eb3938c602dbcee9825eb3c4686425fface0`,
verified locally on 2026-09-14 (Asia/Shanghai). It is not live resource readback,
Candidate qualification, or an Instance deployment. Target boundaries belong
to [architecture](architecture.md#workspace-application-boundary); sequencing
and deliverables belong to [roadmap](roadmap.md#implementation-sequence).

### Current Capability Baseline

| Capability | Current source behavior | Remaining gap and owning source |
| --- | --- | --- |
| Resource purchase and fulfillment | New default purchases commit a resource-only Launch and an independent default installation request. Charges advance in the resource worker; application failure does not rewrite purchase success. Explicit resource-only and retained full Launch contracts remain distinct. | Actual Instance adoption is unqualified. CP owns Launch and Workspace orchestration; Fabric owns resources. |
| Application admission and deployment | Immutable revisions, actual configuration and Secret bindings feed a reserved deployment generation. Preflight precedes predecessor suspension; activation switches one selection atomically, then retires the predecessor and records evidence. Failed commands resume by original identity. | Registry repository/tag discovery is absent. Full IBD dependency configuration remains open. |
| Runtime execution | Local-Docker and Tencent execute declared components and live health/entry readback. OPL App uses an explicit profile and Secret-file ABI; lifecycle targets exact generations and fences late creation after deletion. | Tencent supports at most one native readiness probe. Local Docker fixtures do not qualify the actual upstream OPL App or IBD image. |
| Persistent storage | Stable application data bindings survive compatible updates and reinstallations; different applications have separate namespaces. Historical layouts require the original runtime identity and readback. | General data import/restore and CBS/TKE qualification remain open. |
| Application entry | Current selection and live Fabric readiness determine the entry. Applications retain their own origin/root and declared port; an application without a web entry has no Open action. `cloud_private` publishes no external entry. | Application Cookie/API/SSE behavior, DNS/TLS and Instance origin qualification remain open. No platform-authenticated private access is claimed. |
| Existing lifecycle consumers | Access and credential capabilities use the selected profile. Suspension/deletion inventory current and incomplete generations; resume starts only the selection. Delete confirms Runtime and owned Secret absence before resources, retaining external source Secrets and Sub2API Keys. | Instance verification and Tencent node-image collection remain external obligations. |
| Administrator UI | Structured registration, real configuration, selection progress, operator retry and owner default-installation resume are implemented. Navigation rejects delayed responses from a different Workspace; application changes clear revealed passwords. | Namespace → repository → tag/version selection remains open. |

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
`/Users/huangrende/Documents/ChatGPT/one-person-lab-cloud-application-delivery/output/workspace-default-install-replacement-20260913/`.
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

## Evidence Matrix

| Layer | Current evidence | What it does not prove |
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
| Public Product Release | `v0.1.7`, product SHA `a59bde68397528186a5220f73195fa1f3eda311b`, GHCR digest `sha256:e64504731f8b61c0864cf59faa647a1150e8a2a5eada34b26faf3a5487d28e8f`, five public assets | Current `main`, the current ten-asset Candidate format, or current Instance qualification |
| Medopl Instance | The 2026-08-30 `workspace-private-state-repair` receipt passed for two existing Workspaces and recorded zero-mutation post-repair readback | A fresh Workspace purchase, full lifecycle, rollback, or qualification of current `main` |
| D5 local image lifecycle | Catalog-fixed replacement, renewed entitlement, persisted recovery, current-generation Pod digest readback and exact CRI cache retirement pass local source/full PostgreSQL/Docker regression. An isolated real containerd verifies preview, protected alias, removal and replay; Instance source passes 345 tests and 18 workflow checks | Production rollout, actual CVM cache/space reclamation, TCR deletion or deployed maintenance permissions |
| NodePool maintenance | Source implements guarded image-GC configuration, taint recovery and bounded redacted readback | Production mutation; the retained evidence here contains no matching Instance execution receipt |
| Operator observations | On 2026-09-10, `verify:local:full` passed with PostgreSQL and Docker integration and zero required PostgreSQL skips. Complete mapped-account Gateway totals, typed resource observations, physical Runtime inventory/business reconciliation, separate service/image readiness, and desktop/mobile operator browser chains are covered | A deployed Candidate, repaired historical resources, available Tencent balance, or permission to delete unmatched objects |
| External resource reconciliation | On 2026-09-11, focused Workspace-loss, PostgreSQL CAS/audit/race, Fabric power/readback and retained-operation ownership tests pass. `verify:local:full` passed its source/browser/Docker and other Go modules but hit two Control Plane transaction-start deadlines; both unchanged tests passed three isolated repetitions, then all 2,511 Control Plane tests/subtests passed against a fresh local PostgreSQL with zero test skips. Final operator browser checks pass 9/9. Successful Workspace bindings are scanned even without old child projections; confirmed loss closes access/auto-renew and suspends the original Runtime, while unknown facts preserve entitlement and verified CVM observations survive missing TKE associations | Exact-Candidate Instance adoption, actual cloud refund/destruction history, repaired historical Runtime controllers or customer-money qualification |
| Observation state semantics | On 2026-09-11, `verify:local:full` passes source, browser, build, all Go modules, PostgreSQL and Docker business checks with zero required PostgreSQL skips. Focused tests prove stopped/pending-deletion/deleting CVMs retain their independent TKE association failures, per-Workspace immutable targets and controller ownership distinguish version differences from unverified images, and resource refresh issues no business writes. Delete navigation assertions wait for the list owner read to commit; the complete lifecycle browser group passes 23/23 | A deployed Candidate, confirmed CVM refund/destruction cause, a configured Instance scan interval, or new-purchase qualification |

Evidence applies only to the exact identity and layer named in its row. An older
Candidate or Instance receipt cannot upgrade current source to release or
production-ready status.

Confirmed `data_deleted` is rendered as a known data-loss state in customer and
operator Workspace lists. The lifecycle model and browser resource-refresh chain
verify that the synchronized state replaces the prior running label.
Renewal readback reports lost storage as reclaimed even before the original paid
period expires; the customer recovery chain keeps opening and renewal unavailable.

The public Release list was read again through the GitHub API on 2026-09-07 and
still contained only `v0.1.7`. Endpoint, Local runtime and Instance rows retain
their original observation dates; this documentation audit did not requalify
those environments.

## Current Product Cut

The current product is administrator-provisioned. One Console user maps to one
Account and one Sub2API identity/wallet; an Account may own multiple independent
Workspaces. Basic and Pro are the visible Workspace packages. Control Plane owns
quotes and purchase eligibility, while Sub2API remains the sole owner of
spendable balance, Keys, routing, and usage.

Console calls Control Plane product APIs. Control Plane, Fabric, and Ledger are
separate processes and PostgreSQL schema owners. Fabric provides Local-Docker
and Tencent/TKE adapters through one provider-neutral boundary. The medopl
Instance selects and configures Tencent/TKE; Cloud does not carry its production
domain, Secrets, or provider profile as defaults.

Public registration, customer-operated payment or top-up, shared multi-user
Workspaces, high availability, and GPU are not current customer capabilities.

The Console customer experience and Support retirement are merged in PR #530.
Current interaction rules belong to the product experience guide, and browser
state boundaries belong to the Console implementation reference. Retained
Support data is historical custody, not an available ticket capability.

## Implemented Capability

- Workspace Launch is a durable Control Plane operation that coordinates the
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
  one checksum-bound ten-asset installation bundle from an exact Cloud SHA.

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

## Distribution Boundary

The five `v0.1.7` assets and their public API digests match the Release manifest
and `SHA256SUMS`. The Release contains the base Compose file and its historical
Local-Docker overlay, but not the current three deployment overlays, two Fabric
overlays, or current complete Workspace installation contract. See
[installation.md](./installation.md) for the executable boundary.

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
