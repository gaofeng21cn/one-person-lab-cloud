# OPL Cloud Current Status

Owner: `opl-cloud`
Purpose: `replaceable_current_evidence_snapshot`
State: `current_snapshot`
Baseline evidence source: `9db27a91bc236cdf294253a352a3e1682fd35235` on `main`
Baseline evidence date: 2026-10-01

This page keeps the dated baseline evidence above and explicitly dated source
updates below. The baseline SHA does not describe the later changes or qualify
them: each receipt retains its original source, run and environment. The latest
source review is 2026-10-14 at
[`9c6d8fe882ace96f2b94d1a4d75afabaef38dca1`](https://github.com/gaofeng21cn/one-person-lab-cloud/tree/9c6d8fe882ace96f2b94d1a4d75afabaef38dca1).
It answers what the cited evidence proves within its declared scope. It does not describe the target architecture, an
Instance installation, or a Product Release. The target boundary is in
[architecture.md](architecture.md), durable decisions are in
[decisions.md](decisions.md), and open work is in
[roadmap.md](roadmap.md).

## Local Qualification Repairs (2026-10-13)

PostgreSQL scratch-root recovery and optional Workspace-fixture construction
were merged through Cloud PR #801 at
`c8b33a251a3c4a595e995293c9984b055c88e225`. It clears only the exact
run-owned bind root after confirmed Compose shutdown using the pinned PostgreSQL
image, then returns the empty `0700` directory to the actual host owner. Recovery
authority and earlier qualification failures remain preserved. This change does
not introduce a competing cleanup implementation.

Independent local Linux/Docker ownership experiments at runner UIDs 10001 and
12037 reproduced `EACCES` and verified ownership transfer without widening mode
or following external symlinks. Those experiments exercised a retained alternate
implementation; they do not validate PR #801 or complete Candidate qualification.
PR #801's owning evidence remains in that PR. Candidate construction run
`38401075308` succeeded for its exact source; qualification remains separate.

Verification of this change uses base
`c8b33a251a3c4a595e995293c9984b055c88e225`. Ordinary source tests run without a
current task-book freshness dependency; plan changes retain their separate
freshness check. Existing Clean Host selects an explicit immutable Workspace
image or constructs only its qualification fixture while retaining the exact
supplied Cloud image. This change does not replace that merged workflow.

The application-entry check retains HTTP success plus the qualification fixture
marker as its acceptance criteria. It now reports `workspace_open_http_failed`
with only the HTTP status, or `workspace_open_fixture_marker_missing`; both
paths retain `NOT_READY` and `workspace_open`, with no response body, private
headers or entry URL persisted. Real HTTP lifecycle tests exercise both failures
and the unchanged success path. These local tests do not diagnose the earlier
patrol's launch 502 or establish new hosted READY evidence.

Clean Host READY evidence is now available below. The governance source change passed its current-head PR CI; this does not
explain the patrol's separate historical launch 502.

### Exact Candidate Follow-Up

Clean Host run `38408791877` on 2026-10-13 used PR #801's exact Candidate
source `a894a7478739d1b99a9e5b0d38dcae05b8220dab`, Cloud index digest
`sha256:fb60b2917c19400fb940062bb913bd04ad4af3d7f63ea07cce3b91b6ae9119c5`
and the canonical `ghcr.io/gaofeng21cn/one-person-lab-webui` Workspace digest
`sha256:f6ce30fe1b0cdf8e1c6722a4c96d893ff8286e6af718940dab5a1d778ba70b71`.
Anonymous manifest readback matched, and the hosted runner pulled that Workspace
image. The Candidate bundle's existing validator accepted its asset checksums
and neutral receipt.

The uploaded [unchanged failure receipt](evidence/local-qualification/2026-10-13-clean-host-38408791877-not-ready.json)
is `NOT_READY / terminal_readback`, with `Workspace Runtime open failed` and
residual containers/volumes/networks `1 / 0 / 1`. Source flow reaches this check
after launch success and application-installation readiness. Its error combines
an unsuccessful entry response and a missing qualification-fixture marker;
the receipt does not distinguish those predicates. The workflow storage cleanup
succeeded, but resource absence and full owner deletion are unproved. No new
PostgreSQL `EACCES` was observed; this earlier failure does not qualify PR #801's
successful scratch cleanup. The patrol's original SHA and HTTP 502 still need
separate attribution. No real Sub2API, Instance or production acceptance follows.

The later owner-dispatched run `38415379681` used the merged workflow,
retained that same Cloud Candidate and built only a run-owned Workspace fixture.
Its [unchanged receipt](evidence/local-qualification/2026-10-13-clean-host-38415379681-not-ready.json)
reports `NOT_READY / owner_delete`, `owner Workspace DELETE continuation failed`,
residuals `0 / 0 / 1`, and the last pending readback `storage_absent / compute`,
ordinal 1 of 8. This flow passed application open and restart but did not prove
completed owner deletion or resource absence. Its receipt carries no specific
failed DELETE response status or upstream reason; do not infer a provider cause
or successful scratch cleanup. The built fixture digest is bound to this run,
not a portable Workspace distribution reference.

### Verified Clean Host Qualification

[Clean Host run 38420381927](https://github.com/gaofeng21cn/one-person-lab-cloud/actions/runs/38420381927)
qualified Product `78dbe9b474e6b614ca0def565fd2ab60cdb37e4e` and exact Cloud
Candidate digest `sha256:05992806bf27789adfc0956c2df5bdf10d8e127c3b0641430cf38028e8f6e987` with the existing Workspace fixture. The unchanged
[validated READY receipt](evidence/local-qualification/2026-10-13-clean-host-qualification-38420381927.json)
binds startup, application access, restart continuity, owner DELETE and its
Ledger receipt, and zero container/volume/network residuals after cleanup.
This fixture run does not qualify the canonical OPL App Workspace image,
live Sub2API, TKE, or the original paid-order obligations.
The earlier owner-delete failure in run 38415379681 was not reproduced in this
run; the new bounded failure callback improves diagnosis but does not establish
the runtime root cause of that earlier failure.

The original artifact bytes match the preserved receipt and the current producing
validator accepts it. Run `38421676224` independently returned a valid READY
receipt for the same Cloud Candidate, with zero resource residuals. This closes
#793's clean-host PostgreSQL harness acceptance. The implemented qualification
path retains the supplied immutable Cloud Candidate and may build the declared
run-owned Workspace fixture; this is a valid local harness path. #787 remains
open for owner disposition of the patrol's separate original launch HTTP 502;
the exact old-input replay stopped before login or launch at the known cleanup
failure, so it remains a historical, non-reproducible event. Canonical
App, live-authority and Instance business acceptance remain separate product
outcomes, not additional prerequisites for closing that local qualification
issue. The READY evidence does not qualify the older `d5903d7a` recovery
Candidate or establish its current Instance state.

The newer Candidate construction `38428936740` produced source
`d430825c08c73c294d44ce886228ecfd66c72704` / Cloud digest
`sha256:5fb282fa50cae0c06d0e0bbeb02473f9a1c65807e898edf38d6de0740a505d0b`.
Its complete neutral bundle validates. [Clean Host run 38430143919](https://github.com/gaofeng21cn/one-person-lab-cloud/actions/runs/38430143919)
returned [unchanged, validator-accepted READY evidence](evidence/local-qualification/2026-10-13-clean-host-qualification-38430143919.json)
with fixture authority and zero container/volume/network residuals. This newer
build has local harness evidence; these artifacts do not establish its Instance
adoption.

The Instance owner separately deployed source `78dbe9b4` / digest `05992806…`
in Deploy `38425411874`. [Fresh protected verification 38428162906](https://github.com/gaofeng21cn/opl-instance-medopl/actions/runs/38428162906)
binds those exact bytes and reports all twelve checks true with zero
Kubernetes/provider/business mutations. The [Instance receipt owner](https://github.com/gaofeng21cn/opl-instance-medopl/tree/main/receipts)
preserves its deployment and verification evidence. This proves the inspected
ten-owner topology and health boundary, not the newer build's adoption, an
administrator refund-policy page, or original paid-order application acceptance.

## First Tencent/TKE Launch Scope Alignment (2026-10-10)

The [October 10 decision](decisions.md#2026-10-10-first-tencenttke-launch-without-cloud-model-configuration)
defers Cloud Console model management from the first default App launch. The
publisher schema/API and generated public JSON vocabulary describe absent optional
capabilities without claiming them. This is a contract/source-check layer only:
consumer adoption, Instance schema-digest adoption, exact Candidate deployment,
real application use and charge/Ledger reconciliation still need their owner
receipts. Existing production evidence is not upgraded by this decision.
The [scope/contract receipt](evidence/source-checks/2026-10-10-first-tke-default-app-without-cloud-model-config.json)
records the public JSON red/green check and the remaining owner acceptance.

## Current Source Baseline

The current source contains the target owner modules and the retained
migration source in one repository:

- Console UI/BFF provides the browser surface and typed owner aggregation.
- CloudIdentity/Tenant and Gateway Integration are separate data owners in one
  deployment unit.
- Capability, Build, Runtime Control, Workspace, Resource Catalog and Serve
  have their own owner processes and databases.
- Fabric owns provider resources and Secret bindings; Ledger owns receipts and
  reconciliation.
- Control Plane remains a migration source for callers that have not moved. It
  is not a second target writer.

The current product selection is a strict union:

- **Default OPL App**: an approved Runtime Release and its declared built-in UI.
  It does not invent a Package, BuildJob or CapabilityVersion.
- **Custom Agent**: an admitted Package, an approved Runtime Release and a
  compatible independent WebUI, fixed into one immutable Build output.

Workspace owns entitlement, model-configuration intent and target
authorization. Serve owns the current deployment, readiness, applied runtime
configuration and access route. Fabric never deploys the Agent OCI, Gateway
never owns Agent lifecycle, and Ledger never authorizes continuation.

## Current Capability Baseline

| Capability | Current Cloud source | Evidence layer | Boundary still open |
| --- | --- | --- | --- |
| CloudIdentity and tenant scope | Owner process, typed authorization and cloud-identity Console Workspace read are implemented | [cloud-identity read](evidence/source-checks/2026-09-30-cloud-console-workspace-owner-read.json) | Owner-backed renewal, deletion, credential and application-installation routes are not yet exposed through the cloud identity |
| Package, WebUI and Capability catalog | Package/version/reference-claim reads and admission are implemented | [runtime capability readback](evidence/source-checks/2026-09-28-runtime-capability-readback-repair.json) | Original external OMA candidate and hosted publication remain unverified |
| Build and immutable OCI evidence | Build admission, BuildKit/OCI readback and artifact registration are implemented | [package-to-registry readback](evidence/source-checks/2026-09-29-build-package-to-real-registry-readback.json) | A hosted Candidate must prove the real tenant destination and exact publication path |
| Runtime Control | Approved Runtime Release catalog and compatibility facts are implemented | [exact Runtime local build](evidence/source-checks/2026-09-28-exact-runtime-local-build.json) | Approved production Runtime/native-UI inputs and Instance readback remain open |
| Resource Catalog and Workspace order | Approved plan, quote, acceptance, Workspace order and owner-local recovery are implemented | [Workspace original-order](evidence/source-checks/2026-09-26-workspace-resource-original-order.json), [refund list encoding defect](evidence/source-checks/2026-10-08-refund-policy-list-enum-encoding-defect.json) | Paid production purchase and provider acceptance are Instance obligations; the administrator refund policy list reader now normalizes the stored hyphenated algorithm onto its wire member ([repair source check](evidence/source-checks/2026-10-09-refund-policy-algorithm-enum-encoding.json)), and the repaired source is included in the verified `78dbe9b4` Instance Candidate; current administrator-page HTTP acceptance still requires its own readback |
| Gateway managed key | Gateway Integration owns issuance, allowlist, Secret delivery reference and revocation; Workspace consumes only opaque binding identity. A production issuer now calls the Sub2API owner's service issuance route (`POST /api/v1/service/workspace-keys`) with the deployment's own service token, and an empty declared selection is resolved by that authority into a concrete, bounded model scope that is recorded with the binding and the original command; the raw key is written only through Fabric's approved Secret store (`FabricCoordination.PutManagedSecret`) | [model update owner](evidence/source-checks/2026-10-01-workspace-model-configuration-update-owner.json) | The live Sub2API service route, its Instance secret wiring and a service revocation path remain to be qualified; the owner tests use the HTTP contract fixture, not the live authority. Production revocation remains refused; the refusal is now recorded as a `RevokeManagedKey` attempt with outcome and cause code, without marking the key revoked. Because the owner confirms the reload before it attempts that revocation, a refused revocation leaves a model configuration whose applied version already equals its own; the owner's status derivation reads the operation lifecycle before the configuration and requires a succeeded operation for `applied`, and the Console readback derives application evidence from the owner's applied version alone, so the same readback answers `needs_attention` while the Console reports the configuration as applied with an unsettled closeout instead of a failed configuration ([PR 842](https://github.com/gaofeng21cn/one-person-lab-cloud/pull/842)) |
| Fabric Secret binding | Serve performs the initial launch handover; authenticated Workspace model updates may bind/rebind through Fabric as well. Both guards retain predecessor and opaque binding/version checks. The current caller fix is [PR 837](https://github.com/gaofeng21cn/one-person-lab-cloud/pull/837) | [Secret rebind](evidence/source-checks/2026-10-01-fabric-secret-rebind-second-configuration.json), [launch handover](evidence/source-checks/workspace-handover-contracts2-20261010-source-contracts-2.json) | Workspace presents a Fabric-confirmed binding to Serve; that source route is admitted. Production mTLS/Secret handover and declared non-Gateway runtime inputs remain separately unqualified |
| Serve model apply/readback | Serve resolves the frozen publisher contract, applies through the declared interface and records only the application's readback | [Serve model readback](evidence/source-checks/2026-09-30-serve-model-configuration-apply-readback.json) | Console BFF/UI model routes use the Workspace owner; the retained Control Plane routes have separate callers; live TKE execution and route acceptance are open |
| Serve delivery and access | Deployment/readiness/access ownership and the stable Instance ingress boundary are implemented in source; initial dependency progression is bounded by existing deployment, runtime and lifecycle facts | [Serve access data plane](evidence/source-checks/2026-10-01-serve-access-data-plane-rebase-on-merged-ssot.json); [PostgreSQL progression boundaries](../services/serve/internal/delivery/dependency_progression_postgres_test.go) and [executor dependency/read boundaries](../services/serve/internal/tkeapply/executor_test.go) | Typed application execution, route activation, DNS/TLS and a real Instance request remain unqualified |
| Owner topology and browser identity | Portable owner topology, HTTP surface and baked Console identity are declared and contract-tested | [topology](evidence/source-checks/2026-09-30-owner-topology-installation-contract.json), [HTTP/identity](evidence/source-checks/2026-09-30-http-surface-and-console-identity-declaration.json) | The Instance manifest still has to install and route the complete set |
| Ledger | Append-only receipts, evidence and reconciliation boundaries remain in the retained Ledger owner | [local receipts](evidence/source-checks/2026-09-26-ledger-local-receipts.json) | Current Candidate and Instance evidence must be bound to the same bytes |

A source capability is usable for local development only to the extent its
owner tests and caller readback prove it. The table does not claim that a
portable image, Candidate, Instance or public endpoint has adopted every row.

## Source Qualification

The 2026-10-14 Serve repair passed
`npm run verify:local:focused -- --base ea07b16405a30b6b69208361a9b1c933e916495d`:
110 delivery test/subtest results and 14 executor tests passed with no failures
or skips. The delivery tests use an isolated PostgreSQL database and typed runtime
fixtures; the executor tests use the real executor with the Kubernetes fixture.
They cover dependency progression, repeated reads after failure, lifecycle intent,
retired replacement isolation and an explicit Deploy retry. Workspace retirement
uses the existing `terminate` action rather than a value rejected by the owner
schema. Its tests cover actual PostgreSQL persistence, provider failure and retry,
confirmed replay, retained attachment identity and no restart on repeated polls.
These results do not
qualify a real TKE application, Candidate or Instance.

The retained source-check receipts report the following checks for their own
revisions. This documentation change does not rerun those suites or rebind a
historical result to the latest source review:

- `npm run verify:local` passed the repository contract validators, source
  tests, Console browser suite, typecheck, lint, build, Go compilation and
  database-free owner tests.
- Focused PostgreSQL owner tests for Workspace, Serve, Gateway Integration and
  Fabric pass for the model-configuration and Secret-binding changes.
- The generated bindings are synchronized with
  `packages/contracts/proto/internal.proto`.
- The Fabric Local-Docker integration test remains dependent on a working Docker
  daemon. A failed or unavailable Docker check is an environment boundary, not
  evidence that a provider deployment succeeded.
- No command in this documentation change built a Candidate, dispatched an
  Instance workflow, bought or deleted a provider resource, changed a real
  wallet, or accessed production-private endpoints.

The retained receipts bind narrower claims to their own source and environment:

- [full local source receipt](evidence/source-checks/2026-09-30-tke-serial-full-verification-green.json)
- [browser suite receipt](evidence/source-checks/2026-09-30-tke-serial-browser-suite-green.json)
- [canonical-main qualification receipt](evidence/source-checks/2026-09-30-tke-serial-qualification-main-green.json)
- [merged model-configuration owner receipt](evidence/source-checks/2026-10-01-workspace-model-configuration-update-owner.json)

A test, build or receipt proves only its own evidence layer. It does not
promote source into a Candidate, a Candidate into an Instance, or an Instance
into production.

### Retained Qualification Limits

1. Gateway coordination and managed-key tests use a Sub2API stub or the fixed
   HTTP contract fixture. They prove owner authorization, idempotency, replay and
   scope-resolution semantics, not live wallet or managed key acceptance. The
   unresolved boundary is recorded in
   [managed-key serving](evidence/source-checks/2026-09-30-tke-serial-managed-key-serving-boundary.json);
   the approved service issuance route itself is owned and implemented by the
   Sub2API owner and still requires live qualification.
2. Serve's application execution still reaches Fabric over the retained signed
   HTTP adapter, and the frozen contract still declares no typed
   application-execution RPC in `FabricCoordination`. That half of the boundary
   is unchanged. The Serve-owned route activation performer is no longer
   missing: Serve fences and activates the Workspace's current route inside the
   same transaction that records the readiness. What the receipt was still
   waiting on, and what the Cloud source still does not produce, is the
   installation Ingress/route-object conditional switch, so the access owner is
   still unqualified on TKE. See [execution and route boundary](evidence/source-checks/2026-09-30-tke-serial-serve-exec-route-boundary.json).
3. A Runtime that declares an authorization Secret input other than the
   Gateway-managed input is refused until the runtime Secret delivery channel
   carries the declared binding, version and value through the approved owner
   boundary.
4. The Console BFF/UI model-configuration callers no longer use the retained
   Control Plane path: the UI calls the BFF model routes and both BFF model
   routes target the Workspace owner for read/write. Source ownership is
   settled and those callers have moved; this page makes no claim about the
   remaining unmigrated Control Plane callers.
5. The installed Instance inspected on September 30 still contained only the
   legacy Control Plane, Fabric and Ledger set. The Cloud topology declaration
   does not install those owners by itself; see
   [Instance install gap](evidence/source-checks/2026-09-30-tke-serial-instance-install-gap.json).

## Candidate, Instance And Release Boundary

The current source is not a Candidate or a Product Release.

- Candidate construction requires an owner-dispatched workflow against an exact
  Cloud SHA and a complete portable asset/image manifest. The current preconditions
  are recorded in [candidate/Instance preconditions](evidence/source-checks/2026-09-30-tke-serial-candidate-instance-preconditions.json).
- The Instance owner must install the declared owner topology, configure
  protected secrets and provider facts, deploy the exact Candidate, and return
  runtime, route and billing readback.
- The public Product Release remains `v0.1.7`; it predates the current owner
  topology and model-configuration path. A successor Release needs the same
  qualified bytes promoted without a rebuild.
- Cloud does not dispatch Instance deployment, mutate Tencent resources, or
  certify a production medical environment.

[installation.md](installation.md) owns the downloadable Release versus source
Candidate distinction. [opl-instance-medopl](../../opl-instance-medopl) owns
deployment, rollback, provider qualification and production receipts.

## Cloud Owner Topology Installation Contract

[The portable topology declaration](../deploy/portable/opl-cloud-owner-topology.json)
is the Cloud-owned input for an Instance manifest. It names owner identities,
executables, addresses, database roles, peer identities, HTTP surfaces and the
baked Console identity. It is a source and packaging contract; it is not proof
that an installation runs the complete set.

## Browser-Facing HTTP Surface And Console Identity

The product image serves one Console origin while the Console calls the
surfaces declared by the topology contract. The Console identity is baked into
the image and cannot be changed by deployment configuration. The source
contract-test evidence is
[here](evidence/source-checks/2026-09-30-http-surface-and-console-identity-declaration.json).
A real Instance must route the surfaces required by its baked identity and
return owner-authoritative readback.

## Cloud-Identity Customer Workspace Read

The cloud identity reads customer Workspaces from the Workspace owner through
the BFF and does not call the retained Control Plane Workspace read path. The
legacy identity keeps its retained Control Plane projection. The source browser
evidence is [the owner read receipt](evidence/source-checks/2026-09-30-cloud-console-workspace-owner-read.json).
The missing owner-backed maintenance routes remain an open roadmap item.

## Current Implementation Anchors

The following headings are retained because implementation and history pages
link to them. They are concise pointers to the owning current source or evidence;
they are not a second status ledger.

### Workspace Deletion And Hourly Refund

The retained Control Plane deletion and separate refund operations remain
implemented owner paths. Their exact ordering, identity gates and unknown-result
behavior are owned by
[workspace-launch-recovery](implementation/workspace-launch-recovery.md) and
the relevant source tests. Instance adoption and same-byte qualification remain
open.

### Launch Resource Readback

Launch resource and provider readback remain Fabric-owned facts coordinated by
Control Plane for the retained path. The source/Instance boundary is described
in [implementation architecture](implementation-architecture.md#launch-integration).

### Application Hosting Boundary

Serve owns the target application delivery boundary. The retained Control Plane
proxy is compatibility behavior and does not become a second target route owner.

### Default Application Replacement Verification

Default-App replacement and recovery are covered by focused source checks. The
same Candidate, Instance route and real browser acceptance remain open.

### Independent Application Deployment Verification

The generic Agent deployment path has owner-local source coverage. A provider
host, real route and external consumer have not yet been qualified from one
current Candidate.

### Customer Launch Provisioning Shape (Local Development)

The retained resource-only launch shape remains a compatibility contract. The
current new-customer shape is explicit Default App or Custom Agent selection as
defined in [decisions.md](decisions.md#2026-09-29-default-opl-app-and-optional-agent-on-one-tencenttke-delivery-path).

### Per-Binding Application Origin (Local Development)

Serve's application origin is derived from the Workspace/application identity;
stable DNS/TLS and real browser access remain Instance-owned. The implementation
details are in [workspace runtime access](implementation/workspace-runtime-access.md).

### Unified Deployment Command With Inline Revision (Local Development)

The retained deployment command may carry an inline revision snapshot. Its
revision identity remains owned by the application/deployment owner and is not
a second catalog writer.

### Image Reference Identity (Local Development)

OCI identity is compared as the complete host/namespace/repository/digest
reference. Registry endpoints, credentials and approved repository sets are
installation inputs, not Cloud user choices.

### Registry Endpoint, Credential And Approved Set (Local Development)

An Instance supplies the endpoint and protected credentials; Cloud consumes the
declared approved repository set. Empty or out-of-scope declarations fail
closed.

### Unified Application Entry (Local Development)

The current target entry is the Serve-owned binding behind stable Instance
Ingress. The retained path remains available only while its real callers have
not moved.

### Component Compute Envelope (Local Development)

Provider adapters consume the declared component CPU/memory envelope. Provider
capacity and billing acceptance still require Instance evidence.

### Retained Runtime Entry Projection Repair

A provider URL or Service name is an observation used by the owning route
projection. It is not a second route authority and cannot by itself mark a
Runtime ready.

### Retained Runtime Evidence

Historical local and provider-fixture evidence remains provenance for the
retained Control Plane/Fabric lifecycle. It is not current Candidate,
production, wallet or medical acceptance evidence.

## Readiness

OPL Cloud currently has a coherent source architecture and a broad local owner
implementation. The user-visible capability is still an administrator-operated
source/candidate preparation surface. The next terminal evidence is an exact
Candidate plus protected Instance installation and real route/runtime readback.

The medical platform described in
[medical-agent-platform.md](medical-agent-platform.md) is a target boundary and
gap analysis. No clinical workflow, patient-data production use or medical
quality claim is made by this Cloud source snapshot.
