# OPL Cloud Roadmap And Current Gaps

Owner: OPL Cloud
Purpose: open outcomes and acceptance
State: active

This page contains open outcomes and their completion evidence. It does not
repeat historical execution logs or act as a second status ledger. The current
source snapshot is [status.md](status.md); target ownership is
[architecture.md](architecture.md); durable choices are [decisions.md](decisions.md).

## SSOT Reading Order

Use the following order when a source, document and receipt disagree:

1. The latest durable decision and its canonical owner define the target.
2. Source, contracts and focused tests define Cloud implementation facts.
3. [status.md](status.md) records the current evidence boundary.
4. This page records the remaining gap, owner and acceptance evidence.
5. Candidate, Instance and production claims require the external owner
   readback named by the row.

A passing test, generated contract, merged PR or queued workflow proves only its
own layer. It does not close an Instance or production outcome.

## Current Target

The reusable Cloud product has two application combinations:

- **Default OPL App**: an approved Runtime Release plus its declared built-in UI.
- **Custom Agent**: an admitted Package, an approved Runtime Release and a
  compatible independent WebUI, fixed into one immutable OCI.

Workspace owns identity, entitlement, resource intent and model-configuration
intent. Gateway Integration owns managed-key issuance and revocation. Fabric
owns provider resources and Secret bindings. Serve owns application deployment,
readiness, route and runtime apply/readback. Ledger records evidence. Control
Plane remains only where a real caller has not yet moved.

The current product target is an exact Cloud Candidate deployed and qualified
through the managed TKE Instance path, with Local-Docker remaining a supported
source qualification profile. The target is not complete until a user can open
the confirmed application route and the exact bytes, owner facts and receipts
can be read back.

## Priority Outcomes

### First Tencent/TKE Default App Launch (2026-10-10)

The [October 10 decision](decisions.md#2026-10-10-first-tencenttke-launch-without-cloud-model-configuration)
narrows the first launch: Cloud Console model selection/update is not a release
gate. Reuse the existing published default App OCI at its verified digest; retain
authentication, tenant isolation, Gateway credentials, data safety, actual message
execution, money reconciliation and exact deployment evidence. Runtime publication
may omit undeclared model/Build capabilities; source consumers and Instance admission
must adopt the approved schema without inventing defaults. Custom Agent Build still
requires its original recipe/format contracts and has separate acceptance.

Consume the existing Ingress repair, Candidate and binding receipts at their actual
evidence layers. First implement only the affected admission/launch/UI seams, then
integrate an exact branch SHA and build a Candidate if Cloud bytes changed. The
Instance owner adopts the exact schema digest and qualifies those bytes. Finally
resume the original operation and verify ready URL, App login, one real answer,
original order/charge and Gateway/Ledger readback under the existing authorization
limits. Unchanged checks are reused; changed boundaries receive focused tests. Do
not start a new App release for the deferred model-management interface or repeat
a Local full run by default. The broader table below is not the first-launch gate.

The affected Cloud owners are implemented on `codex/first-tke-default-app-closure`
(commit `0aa598c7`, consumer receipt
`docs/evidence/source-checks/2026-10-10-standalone-default-app-consumer-adoption.json`):
Runtime Control admits and stores the standalone declaration with its migration
upgrade path verified against a real 0001-only database, Workspace refuses a model
update for a release that declares no interface before any owner effect, and the
custom Agent Build refusals for absent recipe/Package-format facts stay pinned. This
prose/schema change is not
a deployed Candidate or final business PASS.

| Priority | Outcome | Cloud owner | Current state | Completion evidence |
| --- | --- | --- | --- | --- |
| P0 | Move live model-configuration callers to Workspace/Gateway/Fabric/Serve | Workspace, Console BFF/UI, Control Plane migration | Owner-separated source path is implemented; retained callers still use Control Plane | BFF/UI uses the Workspace owner for read/write, old writer has no configured caller, owner and browser tests pass |
| P0 | Complete live managed-key issuer and delegation | Gateway Integration plus Sub2API | Cloud coordination and stub tests pass; approved service issuer/delegation is unresolved | Isolated real Sub2API issuer, caller identity and key/usage readback; no raw key in Cloud persistence or receipts |
| P0 | Finish Serve typed application execution and route activation | Serve; Instance owns ingress/DNS/TLS | Source route owner and model apply/readback exist; retained Fabric adapter and route performer remain | Typed execution contract, conditional route CAS, real TKE Service/readiness/access request and lost-response recovery |
| P0 | Repair the Resource Catalog refund policy list encoding defect | Resource Catalog | The administrator refund policy list answers 502 once a row exists: `scanRefundPolicy` maps the stored hyphenated algorithm without normalizing the separators, so the required `algorithm` property silently becomes the unspecified member and the public encoder refuses the page. The five admissions themselves succeed and no customer-facing route serializes that version, so the reach is the administrator list route | [defect source check](evidence/source-checks/2026-10-08-refund-policy-list-enum-encoding-defect.json); a focused list-read test over a stored hyphenated algorithm passes and a Candidate built from the repaired source is read back |
| P0 | Complete runtime SecretInput delivery | Serve, Fabric and contract owners | Gateway-managed input is covered; other declared runtime inputs are refused | Approved opaque binding/version delivery through RuntimeDeployCommand, application authorization and readback tests |
| P0 | Build one exact Candidate and install the complete owner topology | Cloud release owner; Instance deployment owner | Topology and Candidate preconditions are declared; installed Instance still has the legacy three-service set | Exact SHA/tree/index/digest manifest, complete owner deployment, protected health/readiness and route readback |
| P0 | Qualify Default App and Custom Agent on the same Candidate | Serve, Workspace, Instance | Local owner journeys exist; no protected TKE user acceptance | One confirmation reaches a ready application in each mode, restart and response-loss recovery, exact image and route evidence |
| P1 | Retire retained Control Plane application route and duplicate DTOs | Control Plane migration owner and Serve | Retained route/proxy remains for un-migrated callers | Caller inventory is empty or explicitly historical, old route writer and current-selection projection are removed |
| P1 | Public registration, administrator top-up and controlled purchase | Control Plane, CloudIdentity, Console | Existing operator/admin path exists; public self-service chain is incomplete | Concurrent registration, zero initial balance, one audited top-up, quote/purchase idempotency and protected browser readback |
| P1 | Backup, restore, alert and data-exit contracts | Cloud owners; Instance operates them | Owner boundaries are described; production schedules and receivers are external | Isolated restore identity/readback, stable active/recovered signals, stop-purchase boundary and export/delete receipts |
| P1 | MAP domain contracts and evaluation gate | MAP medical domain owner/Instance | Cloud logical Connect boundary is documented; no clinical domain or qualified MAP adapter/backend combination is claimed | MAP task/adapter contract, selected OPL Connect backend, approved data classes, source/evidence model, human review states, evaluation set, release gate and institution acceptance; no medical-specific Cloud adapter is required |
| P2 | Customer-operated payment, multi-user Workspace, HA/GPU and broader provider profiles | Product and Instance owners | Deliberately outside the current beta cut | Separate product decision, owner and qualification evidence before admission |

## Task-Oriented Console And Agent Version Lifecycle

The [October 9 decision](decisions.md#2026-10-09-task-oriented-console-and-customer-agent-version-lifecycle)
and [Workspace experience](product/workspace-experience.md) are adopted intent,
not completed code, Candidate or production claims. The source at `b9576559`
still exposes Runtime/WebUI pickers in Publisher, a largely read-only Agent
catalog, retained administrator maintenance, and no complete Tenant 50-artifact
publication/deletion journey. Existing upload recovery, owner default-policy
methods, quote/launch, finance, logout and node cleanup are reuse inputs.

| Open vertical outcome | Owning behavior / real consumer | Acceptance and next consumer |
| --- | --- | --- |
| Active defaults and simple upload/build | Runtime Control policy, Capability Package/WebUI, Build admission; Console BFF/UI | Import is not activation; exact approved defaults are frozen; customer supplies only Package/name/version; actual Build produces a ready digest; receipt feeds deployment |
| Complete Agent version-to-Workspace handoff | Capability/Build outputs, Workspace authorization/quote, Serve deployment; Console BFF/UI | Several artifacts per upload remain visible; an exact ready version reaches an entitled empty Workspace or the existing explicit new quote; URL/application readback feeds switching |
| Customer same-Agent upgrade/return | Tenant authorization, Workspace entitlement, Serve current deployment/data compatibility, Fabric resource facts | Same stable Package identity, capacity/data/downtime preview, negative authorization tests, one durable operation, real target readiness/access and unchanged data/other Workspaces; receipt feeds cleanup |
| Local cleanup and cloud artifact lifecycle | Capability availability/quota/reference admission, Build registry execution, Serve consumers, Fabric/Instance node cleanup | Per-Tenant 50 root digests including in-flight reservations; no auto-eviction; current/in-flight artifacts protected; cloud deletion confirmed before quota release; local absence does not claim reclaimed bytes; metadata/data preserved and retained digest can be re-pulled |
| First-cut customer disable and coherent Admin surfaces | Tenant new-command authorization and Console aggregation; existing finance/app owners | View/admit/enable/disable; no implicit app/renewal/Key/resource/refund changes; import/active/single-Workspace actions are visibly separate; existing authorized operations recover |

Deliver each changed seam with focused executed behavior evidence before its
consumer; bind exact source inputs, outputs, upstream receipts and next action.
Source/browser receipts and desktop/mobile screenshots do not close Instance
acceptance. Integrate owner branches before one serial user journey on exact
Candidate bytes. Independent development can run in parallel; shared contracts,
files, production changes, wallets and destructive acceptance are serialized.
These outcomes do not restart Local full qualification or redefine the separate
TKE launch/refund lanes; unchanged evidence is consumed directly and only affected
inputs/downstream behavior are reverified. Machine contracts and lower technical
projections remain pending where they still expose older picker, authorization,
disable or artifact semantics; no changed wire behavior is claimed by this prose.

## Cloud-Identity Customer Workspace Read (2026-09-30)

The cloud Console identity reads Workspaces from the Workspace owner through the
BFF. The remaining gap is the owner-backed maintenance surface: renewal,
deletion, credential rotation and application-installation actions must be
implemented by their owning service and then projected by the BFF/UI. The
legacy identity remains on the retained Control Plane path until its callers
move.

Evidence: [cloud Console owner read](evidence/source-checks/2026-09-30-cloud-console-workspace-owner-read.json).

## Workspace Application Decoupling

The decoupling outcome is complete only when the target owner chain carries
real callers:

    Workspace entitlement and model intent
      -> Gateway managed key
      -> Fabric Secret binding/rebinding
      -> Serve execution, readiness and route
      -> Console BFF/UI readback

The current source already has the owner-separated model update, predecessor
checked Secret replacement, Serve publisher apply/readback and stable access
owner. The remaining work is caller migration, live Sub2API authority, typed
execution/route activation and protected Instance qualification. The retained
Control Plane proxy remains a migration source until those callers have moved.

The medical platform does not require a medical-specific Cloud adapter service.
Its institution-system integration follows MAP: a callable Agent owns the
TaskEngine, semantic adapters, site profile and clinical workflow, while the
logical OPL Connect contract selects a qualified backend such as
`glkvm-native`. Cloud supplies the runtime and recovery primitives that MAP may
use. The adapter/backend combination is an Instance/MAP qualification outcome,
not a new Cloud service or P0 platform dependency.

### Required Deliverables

A completion package must contain:

- the exact Cloud source SHA, tree digest and Candidate image index/digests;
- the owner topology and baked Console identity consumed by the Instance;
- focused Cloud source and browser receipts for both product combinations;
- protected Instance deployment, health, route, image and rollback readback;
- Gateway/Sub2API issuer, usage and revocation evidence without raw secrets;
- runtime model apply/readback and unknown-result recovery evidence;
- the medical domain package/evaluation/clinical-review evidence when the
  medical platform is included.

A source receipt or a Candidate manifest without the corresponding Instance
readback leaves the outcome open.

### Implementation Sequence

1. Finish the typed contracts and generated consumers for Serve execution,
   route activation and runtime SecretInput delivery.
2. Move Console BFF/UI model and Workspace maintenance callers to their owner
   services; remove the old writer only after readback proves no live caller.
3. Resolve the live Gateway/Sub2API issuer and usage boundary in the approved
   owner workflow.
4. Build the exact Candidate and install the complete Cloud owner topology in
   the protected Instance.
5. Qualify Default App and Custom Agent with the same Candidate, including
   restart, response-loss, route and model readback.
6. Promote only those qualified bytes to a Product Release; keep Instance
   deployment and rollback receipts with the Instance owner.
7. Add the MAP domain contracts, adapter qualification, evaluation and
   institution acceptance as a domain layer on top of the qualified platform.

This sequence is a dependency order for the open outcomes. It does not require
unrelated legacy lifecycle work to block the typed owner migration.

### Handoff Inputs And Parallel Work

Cloud can prepare contracts, owner tests, topology assets, Candidate manifests
and local qualification in parallel when they have disjoint write sets.
Integration of shared contracts, generated bindings, main, Candidate
construction and Instance state is serialized.

The Instance handoff must receive the exact Cloud SHA, image/index digests,
topology declaration, required non-secret configuration names, protected secret
references, provider profile and acceptance commands. The Instance returns
deployment and runtime receipts; it does not redefine Cloud ownership or add a
per-customer provisioning writer.

## Qualification Boundaries

### Local Development

Local development proves source behavior, owner boundaries and disposable
fixtures. The macOS Docker daemon limitation recorded in [status](status.md)
does not authorize changing provider guards or calling a real provider.

### Candidate

A Candidate is a replaceable input built from an exact Cloud SHA. It is not a
Product Release and may be constructed only through the repository owner’s
Candidate workflow.

### Instance

The Instance owner supplies protected deployment configuration, installs the
complete owner topology, performs provider actions and returns runtime,
billing, route, rollback and medical-environment receipts. Cloud source tests
cannot certify that state.

### Production

Formal publication requires the exact qualified Candidate bytes promoted without
a rebuild. Real customer money, provider resources, private networks and
clinical data remain outside ordinary Cloud CI and E2E.

## Deferred Product Scope

The following are intentionally deferred until a separate owner and acceptance
boundary exist: customer-operated payment, shared multi-user Workspaces, HA,
GPU scheduling, institution-wide multi-region operation, arbitrary customer
providers, autonomous clinical action and automatic conversion of model output
into medical orders.

## Completion Evidence

An outcome is complete only when its row has:

- a canonical owner and current caller;
- source/contract evidence at the Cloud layer;
- the exact Candidate identity where packaging is involved;
- owner-authoritative Instance readback where deployment/provider state is
  involved; and
- no unresolved unknown result that could change the next external side effect.

The current unresolved rows and their receipts are maintained in this page and
[status.md](status.md). Historical plans and completed execution transcripts
belong in Git history or [docs/history](history/README.md).
