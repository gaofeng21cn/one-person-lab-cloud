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

| Priority | Outcome | Cloud owner | Current state | Completion evidence |
| --- | --- | --- | --- | --- |
| P0 | Move live model-configuration callers to Workspace/Gateway/Fabric/Serve | Workspace, Console BFF/UI, Control Plane migration | Owner-separated source path is implemented; retained callers still use Control Plane | BFF/UI uses the Workspace owner for read/write, old writer has no configured caller, owner and browser tests pass |
| P0 | Complete live managed-key issuer and delegation | Gateway Integration plus Sub2API | Cloud coordination and stub tests pass; approved service issuer/delegation is unresolved | Isolated real Sub2API issuer, caller identity and key/usage readback; no raw key in Cloud persistence or receipts |
| P0 | Finish Serve typed application execution and route activation | Serve; Instance owns ingress/DNS/TLS | Source route owner and model apply/readback exist; retained Fabric adapter and route performer remain | Typed execution contract, conditional route CAS, real TKE Service/readiness/access request and lost-response recovery |
| P0 | Repair the Resource Catalog refund policy list encoding defect | Resource Catalog | `scanRefundPolicy` normalized neither separator when it mapped the stored hyphenated algorithm, so the required `algorithm` property silently became the unspecified member and the public encoder refused the administrator list page once a row existed; the source is repaired and the focused list-read test over a stored hyphenated algorithm passes, while the deployed instance still returns 502 until a Candidate built from the repaired source is deployed | [defect source check](evidence/source-checks/2026-10-08-refund-policy-list-enum-encoding-defect.json); [repair source check](evidence/source-checks/2026-10-09-refund-policy-algorithm-enum-encoding.json); the administrator refund policy list reads back at 200 on a Candidate built from the repaired source |
| P0 | Complete runtime SecretInput delivery | Serve, Fabric and contract owners | Gateway-managed input is covered; other declared runtime inputs are refused | Approved opaque binding/version delivery through RuntimeDeployCommand, application authorization and readback tests |
| P0 | Build one exact Candidate and install the complete owner topology | Cloud release owner; Instance deployment owner | Topology and Candidate preconditions are declared; installed Instance still has the legacy three-service set | Exact SHA/tree/index/digest manifest, complete owner deployment, protected health/readiness and route readback |
| P0 | Qualify Default App and Custom Agent on the same Candidate | Serve, Workspace, Instance | Local owner journeys exist; no protected TKE user acceptance | One confirmation reaches a ready application in each mode, restart and response-loss recovery, exact image and route evidence |
| P1 | Retire retained Control Plane application route and duplicate DTOs | Control Plane migration owner and Serve | Retained route/proxy remains for un-migrated callers | Caller inventory is empty or explicitly historical, old route writer and current-selection projection are removed |
| P1 | Public registration, administrator top-up and controlled purchase | Control Plane, CloudIdentity, Console | Existing operator/admin path exists; public self-service chain is incomplete | Concurrent registration, zero initial balance, one audited top-up, quote/purchase idempotency and protected browser readback |
| P1 | Backup, restore, alert and data-exit contracts | Cloud owners; Instance operates them | Owner boundaries are described; production schedules and receivers are external | Isolated restore identity/readback, stable active/recovered signals, stop-purchase boundary and export/delete receipts |
| P1 | MAP domain contracts and evaluation gate | MAP medical domain owner/Instance | Cloud logical Connect boundary is documented; no clinical domain or qualified MAP adapter/backend combination is claimed | MAP task/adapter contract, selected OPL Connect backend, approved data classes, source/evidence model, human review states, evaluation set, release gate and institution acceptance; no medical-specific Cloud adapter is required |
| P2 | Customer-operated payment, multi-user Workspace, HA/GPU and broader provider profiles | Product and Instance owners | Deliberately outside the current beta cut | Separate product decision, owner and qualification evidence before admission |

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
