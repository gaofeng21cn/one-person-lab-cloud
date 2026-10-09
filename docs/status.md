# OPL Cloud Current Status

Owner: `opl-cloud`
Purpose: `replaceable_current_evidence_snapshot`
State: `current_snapshot`
Snapshot source: `9db27a91bc236cdf294253a352a3e1682fd35235` on `main`
Snapshot date: 2026-10-01

This page answers one question: what is proven in the Cloud repository at the
current source revision? It does not describe the target architecture, an
Instance installation, or a Product Release. The target boundary is in
[architecture.md](architecture.md), durable decisions are in
[decisions.md](decisions.md), and open work is in
[roadmap.md](roadmap.md).

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

## Console Product Decision Alignment (2026-10-09)

The [October 9 decision](decisions.md#2026-10-09-task-oriented-console-and-customer-agent-version-lifecycle)
aligns the adopted task/permission/default-selection/cleanup/50-artifact behavior
with its existing product and architecture owners. The source audit was against
`b957655950f3dc53961fc6f589d4e18cb9b8f238`; source files, generated contracts,
Candidate bytes and Instance state were not changed. The
[documentation receipt](evidence/source-checks/2026-10-09-console-business-ssot-alignment.json)
records document consistency/focused-gate evidence only, not implemented behavior
or production readiness. Pending implementation and next-consumer acceptance are
in the [Console outcome](roadmap.md#task-oriented-console-and-agent-version-lifecycle).
Existing TKE launch and Legacy refund lane receipts remain separate evidence.

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
| Resource Catalog and Workspace order | Approved plan, quote, acceptance, Workspace order and owner-local recovery are implemented | [Workspace original-order](evidence/source-checks/2026-09-26-workspace-resource-original-order.json), [refund list encoding defect](evidence/source-checks/2026-10-08-refund-policy-list-enum-encoding-defect.json) | Paid production purchase and provider acceptance are Instance obligations; the administrator refund policy list cannot encode a stored hyphenated algorithm, so its readback returns 502 until the reader normalizes the separator |
| Gateway managed key | Gateway Integration owns issuance, allowlist, Secret delivery reference and revocation; Workspace consumes only opaque binding identity | [model update owner](evidence/source-checks/2026-10-01-workspace-model-configuration-update-owner.json) | The approved live Sub2API issuer/delegation and caller identity are unresolved |
| Fabric Secret binding | Initial bind/replay and explicit predecessor-checked `RebindSecret` are implemented; one active binding is preserved | [Secret rebind](evidence/source-checks/2026-10-01-fabric-secret-rebind-second-configuration.json) | Declared non-Gateway runtime Secret inputs still need an end-to-end delivery contract |
| Serve model apply/readback | Serve resolves the frozen publisher contract, applies through the declared interface and records only the application's readback | [Serve model readback](evidence/source-checks/2026-09-30-serve-model-configuration-apply-readback.json) | Console/Control Plane callers have not moved; live TKE execution and route acceptance are open |
| Serve delivery and access | Deployment/readiness/access ownership and the stable Instance ingress boundary are implemented in source | [Serve access data plane](evidence/source-checks/2026-10-01-serve-access-data-plane-rebase-on-merged-ssot.json) | Typed application execution, route activation, DNS/TLS and a real Instance request remain unqualified |
| Owner topology and browser identity | Portable owner topology, HTTP surface and baked Console identity are declared and contract-tested | [topology](evidence/source-checks/2026-09-30-owner-topology-installation-contract.json), [HTTP/identity](evidence/source-checks/2026-09-30-http-surface-and-console-identity-declaration.json) | The Instance manifest still has to install and route the complete set |
| Ledger | Append-only receipts, evidence and reconciliation boundaries remain in the retained Ledger owner | [local receipts](evidence/source-checks/2026-09-26-ledger-local-receipts.json) | Current Candidate and Instance evidence must be bound to the same bytes |

A source capability is usable for local development only to the extent its
owner tests and caller readback prove it. The table does not claim that a
portable image, Candidate, Instance or public endpoint has adopted every row.

## Source Qualification

The current source gate that is safe to claim from this checkout is:

- `npm run verify:local` passes the repository contract validators, source
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

1. Gateway coordination tests use a Sub2API stub. They prove owner
   authorization, idempotency and replay semantics, not live wallet or managed
   key acceptance. The unresolved boundary is recorded in
   [managed-key serving](evidence/source-checks/2026-09-30-tke-serial-managed-key-serving-boundary.json).
2. Serve's application execution still has an old Fabric adapter path in the
   retained caller set. The typed application-execution boundary and route
   activation performer must be completed before the access owner can be
   qualified on TKE. See [execution and route boundary](evidence/source-checks/2026-09-30-tke-serial-serve-exec-route-boundary.json).
3. A Runtime that declares an authorization Secret input other than the
   Gateway-managed input is refused until the runtime Secret delivery channel
   carries the declared binding, version and value through the approved owner
   boundary.
4. The Console BFF/UI model-configuration callers still use the retained
   Control Plane path. Source ownership is settled; caller retirement is not.
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
