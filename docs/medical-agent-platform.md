# Medical Agent Platform

Owner: medical domain owner and institution Instance
Platform dependency: OPL Cloud
Purpose: target boundary and acceptance contract
State: target reference; current implementation is recorded in [status.md](status.md)

This document defines how a medical meta-agent platform uses OPL Cloud. It
does not claim that the clinical domain, a qualified MAP adapter or a
production medical environment already exists.

## Platform Promise

The platform should let an institution or an authorized clinician run a
versioned medical agent against approved clinical sources, receive
evidence-linked work, review the result, and retain an auditable decision trail.

OPL Cloud supplies the reusable platform path:

    identity and Workspace
      -> Package, Build and approved Runtime
      -> Gateway model access
      -> Fabric resources, Secret bindings and execution policy
      -> Serve execution and access
      -> Ledger evidence and reconciliation

The medical domain supplies the meaning of the work:

    patient/encounter scope
      -> source documents and observations
      -> extracted clinical facts
      -> agent reasoning and cited evidence
      -> clinician review or rejection
      -> institution-authorized release or action

The two paths must meet through typed, opaque references and explicit review
states. A Cloud deployment or model response is never a clinical conclusion by
itself.

## Three Ownership Layers

| Layer | Owns | Must not own |
| --- | --- | --- |
| Cloud platform | Tenant, Workspace, package/runtime delivery, model access, Secret bindings, provider resources, operations, receipts and access | Patient identity, clinical meaning, hospital-system adapters, diagnosis, prescription, physician sign-off |
| Medical domain | Patient/encounter/document identity, clinical facts, evidence selection, agent skills, output quality, review/signature and clinical policy | Wallet, OCI registry, provider mutation, generic deployment route or Cloud-wide receipt writer |
| Institution/Instance | MAP deployment profile, site-specific system access, data residency, institution roles, protected deployment, retention, export/delete and clinical-use acceptance | A second Cloud Workspace lifecycle, unmanaged model key, hidden clinical side effect |

The medical domain can be implemented as Packages and Agent services delivered
through Serve, with MAP-owned task state, medical components and site adapters
behind the approved boundaries. It does not require a new Cloud process unless
a current caller, separate data owner and bounded migration justify one.

## Medical Agent Lifecycle

1. **Admit**: an institution authorizes a tenant, Workspace, role and data
   class. The medical owner creates opaque patient, encounter and document
   references; Cloud receives only the references needed for scope and policy.
2. **Ingest**: the MAP adapter reads an approved source through the
   institution's authorized system path. The immutable source object, source
   version, extraction method and source timestamp are retained by the medical
   owner or its approved private store.
3. **Extract**: OCR, text, table, image or waveform extraction creates
   versioned observations. Each observation points to an exact source span or
   frame and keeps extraction uncertainty.
4. **Reason**: the Agent Package and approved Runtime perform the task inside
   the authorized Workspace. Prompts, intermediate reasoning and model
   telemetry follow the institution's data-classification and retention policy;
   PHI must not enter ordinary Cloud logs or public receipts.
5. **Cite**: each material claim links to the source observation or an explicit
   missing-evidence reason. A model token, confidence score or generated
   sentence is not a source.
6. **Review**: the medical owner assigns a state such as needs_review,
   accepted, rejected, unsafe or superseded. A clinician or authorized reviewer
   records what was checked and may override the output.
7. **Release**: only an institution-authorized workflow may publish a report,
   message or downstream update. A review result is separate from a clinical
   order or treatment action.
8. **Retain or delete**: the institution applies its retention, legal hold,
   export and deletion policy and returns an owner receipt. Cloud retains only
   the platform evidence permitted by that policy.

Every step needs an idempotent operation identity and an unknown-result
recovery path. A request acknowledgement, a saved draft or a model response
does not prove that the medical step completed.

## Canonical Medical Objects

The medical owner should define these objects and their relationships. Cloud
uses opaque identifiers and scope checks only:

| Object | Minimum contract |
| --- | --- |
| Institution | tenant, legal owner, data-residency and policy reference |
| Patient | institution-scoped opaque id; no raw identifier in Cloud logs |
| Encounter | patient-scoped visit or episode identity and time range |
| Source document | immutable content/version, media type, source system and provenance |
| Observation | source span/frame, extractor version, uncertainty and reviewer state |
| Clinical fact | normalized fact, assertion status, temporal scope and evidence refs |
| Agent task | goal, allowed data class, Agent/Runtime version and reviewer policy |
| Agent result | claims, citations, missing evidence, safety flags and review state |
| Review | reviewer role, decision, reason, timestamp and superseded result |
| Institution action | explicit downstream target, authorization, idempotency and receipt |

Patient, encounter, document and result IDs must never be inferred from email,
file name, model output or an unscoped provider resource. Cross-owner references
are opaque and tenant-scoped.

## Evidence And Data Boundary

The medical source remains the medical owner's authoritative record. The
platform must preserve:

- source bytes or a content-addressed private reference;
- exact extraction and normalization versions;
- source location for every material claim;
- model, Package, Runtime and prompt-policy versions;
- reviewer identity, role, decision and override reason;
- deletion, export and legal-hold outcomes.

A Cloud Ledger receipt may bind hashes, operation identities, owner references,
timestamps and retention classifications. It must not contain raw patient data,
raw model keys, unrestricted prompt traces, or a second clinical record.

Before a MAP adapter or model call, the owner must resolve:

- data class and purpose;
- tenant, institution and encounter scope;
- least-privilege role and Secret reference;
- residency and egress rule;
- retention and deletion rule;
- whether a human review is mandatory.

A failure to resolve any item keeps the operation pending or rejected; it does
not fall back to a broader credential or a less restricted model.

## Human Review And Safety

The platform must distinguish these states:

| State | Meaning | Allowed next step |
| --- | --- | --- |
| draft | work is being assembled | continue domain processing |
| needs_review | a human decision is required | assign and record review |
| accepted | an authorized reviewer accepted the stated scope | publish the approved artifact |
| rejected | the result is not approved | retain reason or supersede |
| unsafe | a safety policy blocked use | remediate or close with explicit disposition |
| unknown | completion or side effect cannot be proven | read back the original operation |
| superseded | a newer result replaced this one | retain lineage; do not act on it |

High-risk output must remain a draft or needs_review until the medical owner
proves an authorized human decision. Cloud must not expose a generic "success"
state that hides these domain states.

The medical owner defines clinical contraindications, out-of-scope questions,
required citations, escalation rules and when the agent must refuse. The
institution defines who may review, sign, send, order or modify a record.
These rules belong beside the clinical domain, while Cloud provides the
operation, identity and receipt primitives.

## MAP Adapter And Execution Model

Medical system integration follows the existing MAP design; the clinical
semantics are not a medical-specific Cloud platform capability. MAP is a
callable clinical Agent for Codex or another approved host, with no required
standalone business UI. Its `TaskEngine` owns task state and recovery, the
medicine component produces versioned proposals, and the MAP adapter exposes
semantic operations such as `read_context`, `apply`, `read_back` and
`find_submission` through the logical OPL Connect contract.

The hospital system remains authoritative for records, orders and execution
state. `glkvm-native` is one OPL Connect backend for device/session control
when the deployment uses KVM; API, CLI, MCP or another qualified backend may
be selected instead. MAP owns the hospital-system semantics and clinical
workflow. A deployment profile outside the product repository supplies the
actual product, version, page paths, backend selection and qualification
evidence. MAP's capability directory, not backend installation alone, decides
whether a clinical capability is callable.

MAP adapters must preserve the following boundary:

- reads return patient/encounter identity, time range, source references and
  observation status rather than unscoped OCR or screen coordinates;
- writes bind the exact patient, encounter, target object, facts revision and
  approved proposal, then verify the authoritative object after the action;
- a lost response is reconciled against the original target before any retry;
- read and write authority are separate even when an institution technically
  exposes them through one account or Secret.

Cloud supplies the Workspace, runtime, Secret, resource, egress, operation and
receipt primitives that MAP may use. OPL Connect supplies the logical access
contract; its backend is selected and qualified per deployment. Cloud does not
maintain a hospital-system inventory, page model, site playbook or clinical
adapter lifecycle for MAP. OPL Connect does not replace MAP adapters: it is the
transport and recovery boundary beneath them.

## Agent Package, Evaluation And Release

A medical Agent Package is a versioned domain artifact consumed by Cloud's
Capability/Build chain. The package should declare:

- intended clinical task and population;
- accepted input data classes and required sources;
- evidence and citation behavior;
- refusal, escalation and human-review policy;
- required MAP adapter capabilities and write boundaries;
- model/runtime compatibility;
- evaluation dataset identity and expected failure classes.

Before release, the medical owner must run an evaluation set that covers normal,
missing, conflicting, ambiguous, out-of-distribution and unsafe cases. The
release gate should retain result distributions, critical failures, reviewer
adjudication, model/runtime/package identities and the reason for promotion or
rejection. A passing benchmark alone does not authorize clinical use.

Serve may deploy a qualified medical Package through the same immutable OCI and
runtime path as any other Agent. Runtime, model and MAP adapter changes require
a new version or an explicit replacement operation; changing a prompt or Secret
silently must not alter a confirmed medical result.

## Cloud Boundary And MAP Contracts Needed Before Clinical Use

These platform gaps must be closed or explicitly supplied by the medical owner
and Instance before a clinical deployment:

| Gap | Contract owner | Acceptance |
| --- | --- | --- |
| Data classification and least privilege | Medical domain plus Instance | Each task declares class, purpose, scope, residency, egress and retention; unauthorized combinations fail closed |
| Opaque patient/encounter/document scope | Medical domain | Every read and result is tenant/institution scoped; cross-scope references are rejected |
| Immutable source and provenance | Medical domain/MAP adapter owner | Source hash, version, span/frame and extractor identity resolve after restart |
| Fact versus inference separation | Medical domain | Clinical facts retain assertion/time/evidence; model inferences cannot overwrite facts |
| Citation and missing-evidence rule | Medical domain | Every material claim has a source citation or explicit unresolved reason |
| Human review and signing | Medical domain/Instance | High-risk results cannot publish or write downstream records without authorized review |
| Evaluation and release gate | Medical domain | Regression set, adjudication and release decision bind exact Package/Runtime/model inputs |
| PHI/log/receipt boundary | Cloud plus Instance | No raw PHI, keys or unrestricted prompts in ordinary Cloud logs or public receipts |
| MAP adapter qualification | MAP and Instance | The site-specific adapter is qualified for the declared system/version, read/write scope, Secret owner, rotation and downstream preconditions |
| Retention/export/delete | Institution/Instance | Policy execution returns owner receipts and preserves required legal holds |
| Unknown and recovery states | Cloud plus medical domain | Original operation can be read back; unknown is never rendered as accepted |
| Institution acceptance | Instance owner | Protected deployment, network, backups, alerts and clinical browser/API checks pass |

The medical owner may implement these contracts in a domain service or Package
while preserving Cloud's ownership boundaries. New Cloud schemas or services
need a current cross-owner caller and a bounded migration.

## Acceptance Definition

A medical platform release is accepted only when all of the following are
proven together:

1. an institution-scoped Workspace and role admit only the declared data class;
2. a real source document is retained with exact provenance and can be read
   back after restart;
3. a versioned Agent produces evidence-linked output with explicit uncertainty;
4. unsafe, missing-evidence and unknown cases remain visible;
5. an authorized reviewer can accept, reject, supersede or sign the result;
6. any downstream MAP adapter write is a separate authorized operation with
   precondition and readback;
7. the exact Cloud Candidate, medical Package, Runtime and model policy are
   bound to the result;
8. the Instance returns protected deployment, audit, retention, export/delete,
   backup/restore and rollback evidence.

Cloud source tests, a successful model request, a green browser suite or a
Package build cannot satisfy the institution acceptance rows by themselves.

## Explicit Non-Claims

This document does not claim:

- a patient database or PHI store exists in Cloud;
- a qualified MAP adapter or hospital-system integration is implemented;
- a medical diagnosis, prescription or clinical order is automated;
- a clinician has reviewed any current model output;
- a medical Agent is safe, effective or approved for patient care;
- an Instance has completed medical deployment or institutional acceptance.

The next medical work should start with MAP domain contracts and a bounded
read-only adapter evidence task, then add review and downstream writes only
after the source, policy and acceptance evidence is complete.
