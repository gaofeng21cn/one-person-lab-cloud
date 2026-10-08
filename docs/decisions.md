# Decisions

This file records durable product and architecture choices. Current
implementation evidence belongs in [status.md](./status.md); unfinished outcomes
belong in [roadmap.md](./roadmap.md).

## 2026-10-08: Separate Legacy Workspace Retention From The Cloud Tenant Entry

A Gateway-authenticated User and a Cloud Tenant are distinct concepts. A User
such as `testcloud4@medopl.com` may be admitted as the `owner` of a Tenant,
but the User identity is not itself the Tenant. The Tenant is the authorization
boundary for its members, repository binding, Packages, Builds and Cloud
Workspaces.

For the agreed first Cloud chain, the designated User is admitted as the owner
of one Tenant whose installation-owned repository binding is
`oplcloud/testcloud4`. Tenant admission and Cloud Workspace creation are
separate operations: the protected administrator path admits the Tenant, then
the customer creates the Cloud Workspace through the Cloud product path. The
first Cloud Workspace uses the approved OPL App Runtime and the approved Basic
plan (`tencent-tke`, `na-siliconvalley`, 2c4g, 10 GB, prepaid monthly).

The existing paid Control Plane Workspace remains a Legacy Workspace during
this transition. It is not copied, silently adopted, or read through a fallback
writer. After the new Cloud chain has a complete owner-authoritative PASS
receipt, the Legacy Workspace may be deleted and refunded through its existing
owner workflows. Deletion and refund remain separate readback obligations; the
original order, charge and receipt evidence are preserved.

The delivery is split into two lanes:

1. The first Cloud lane proves Tenant admission, Cloud Workspace creation,
   default OPL App delivery, one bounded real application interaction, and
   Ledger readback.
2. The second lane proves custom Package upload, Build, immutable OCI evidence
   and Serve delivery.

Independent owner work may proceed in parallel. Tenant admission, the Cloud
Workspace production mutation, the Legacy deletion/refund mutation, shared
contracts and final acceptance are serialized. A lane receipt must identify
its exact input and output artifact, owner, verification, and the next
consumer action.

This decision does not add a Control Plane fallback to the Cloud Console,
create a second writer, or require custom Package upload to block the first
Cloud lane.

## 2026-10-01: Reuse OPL Cloud For The Medical Meta-Agent Platform

The medical meta-agent platform is a domain product built on OPL Cloud. It
reuses CloudIdentity, Workspace, Capability, Build, Runtime Control, Gateway,
Fabric, Serve, Console and Ledger rather than creating a parallel platform
stack.

The medical domain owner owns clinical goals, medical data semantics, source
documents, evidence selection, clinical facts, model inferences, quality
verdicts, doctor review and institution acceptance. Cloud owns the platform
contracts that make those operations isolated, recoverable and auditable:
tenant/workspace scope, package and runtime delivery, model access, Secret
bindings, durable operations, receipts and provider-neutral infrastructure
facts.

The medical platform has three explicit layers:

1. **Cloud platform layer**: identity, Workspace, Package/Build/Runtime,
   Gateway, Fabric, Serve and Ledger.
2. **Medical domain layer**: patient and encounter identities, source
   documents, extraction, clinical facts, evidence chains, agent skills,
   review/signing and medical quality policy.
3. **Institution/Instance layer**: MAP deployment profiles, site-specific
   system access, data residency, protected deployment, institution roles,
   retention, export/delete and clinical-use acceptance.

Medical records, identifiers and clinical conclusions remain opaque domain
objects to Cloud. Cloud receipts may retain owner references and hashes needed
for provenance, but must not become a second medical record, prompt trace or
patient identity authority. Patient-facing, clinician-facing and
institution-facing operations must carry explicit data classification,
authorization, retention and review state.

No medical model output may silently become an order, diagnosis, prescription,
or other clinical action. A medical agent release requires a domain-owned
evaluation set, safety and quality gates, evidence-linked output, human review
policy and institution acceptance. Unknown, needs-review, unsafe and rejected
states remain visible until the medical owner resolves them.

Medical system integration follows the Med Auto Practice (MAP) design. OPL
Connect is the logical access contract; MAP owns its TaskEngine, medical
proposal components, semantic site adapters and task-level evidence.
`glkvm-native` is one replaceable OPL Connect backend for device/session
control when KVM is required. API, CLI, MCP and other qualified backends may
implement the same contract. The hospital system remains authoritative for
records and orders. Site-specific products, page models, playbooks and
qualification evidence stay in the MAP deployment profile and are not promoted
into a Cloud-owned institution-system catalog. Cloud provides the Workspace,
runtime, Secret, resource, egress, operation and receipt primitives; it does not
choose one backend as the medical implementation.

This decision does not create a medical-specific Cloud service, global workflow
engine, second wallet, second Package registry or alternative deployment
authority. The target boundary, missing contracts and acceptance criteria are
maintained in [medical-agent-platform.md](medical-agent-platform.md); current
implementation and external qualification remain separate status layers.

## 2026-09-29: Default OPL App And Optional Agent On One Tencent/TKE Delivery Path

The primary delivery target is `tencent-tke`. A customer confirms the application
selection and compute/storage plan once; Cloud completes the order, resource
provisioning, application deployment and usable access asynchronously. Resource
readiness alone is not the successful outcome.

An Agent Package and a separately selected WebUI are not prerequisites for a
new Workspace. With neither selected, Cloud deploys the exact approved OPL App
Runtime Release with its publisher-declared built-in WebUI. This is a running
application, not `resource_only`, a legacy import, or a failure fallback. Do not
invent a Package, BuildJob or CapabilityVersion to fit the earlier mandatory-
Agent schema. A default App does not need a redundant Cloud Build or a copy in
the Tenant output repository merely to become deployable.

For a custom Agent, Build requires all three exact inputs: Package, approved
Runtime and compatible approved independent WebUI. These and default App are
the only two product combinations. A Package without independent WebUI, or an
independent WebUI without Package, is rejected rather than filled or ignored. The complete selection, publisher
contracts, platform, recipe and exact digests are frozen before execution. Runtime Control owns release
admission/default policy and immutable release readback; it never owns running
instances. Capability remains the Package/WebUI/build-result catalog owner.
Serve owns the deployment and access of both default App and custom Agent, with
at most one current application per Workspace. Workspace owns the accepted
order and resource intent, not a duplicate current-deployment pointer.

This refines the mandatory-Agent wording of the September 22 decision without
changing D17, the wallet authority, domain topology or legacy obligations.
Product combinations are detailed in `docs/spec/target/12_product_spec.md`;
the pending wire migration is recorded by `03_api_contract_complete.yaml` and
`02_database_schema_complete.md`, and W01 must update production contracts,
owner migrations, callers and decoder tests together. Prose approval is not
proof that the existing mandatory CapabilityVersion/WebUI fields support it.

### Workspace Model Configuration And Gateway Key Binding Owner

Workspace owns the accepted model-configuration intent, its monotonically
versioned history, and the `gateway_key_binding_id` recorded for each accepted
configuration. Gateway Integration is the sole owner of Gateway-managed key
issuance, model allowlists, secret-delivery references and key revocation;
Workspace never fabricates or edits those facts. Fabric owns the runtime Secret
binding and its provider readback. Serve owns runtime application of the frozen
configuration, publisher apply/readback, readiness and the applied-version
observation; Serve is not a Gateway key issuer.

The update path is one owner-separated operation:

1. Workspace authorizes the update and validates the new selections against the
   admitted Workspace/application.
2. Workspace calls `GatewayCoordination.CreateManagedKey` with the exact model
   set and target runtime. Gateway returns an opaque `ManagedKeyBinding`; the
   raw key is never returned to Workspace, persisted in Workspace, or placed in
   a receipt.
3. Workspace records the returned binding identity with the new configuration
   version. The `gateway_key_binding_id` column therefore remains `NOT NULL`;
   a configuration without a confirmed Gateway binding is not accepted.
4. The approved Secret reference is bound through `FabricCoordination.BindSecret`
   and read back. Fabric owns only the Secret binding fact, not application
   readiness or routing.
5. Workspace calls `ServeAgentCoordination.ReloadModels` with the opaque
   `RuntimeManagedKeyBinding` and the expected applied version. Serve applies
   the publisher contract and records only the version the application reads
   back.
6. Workspace advances its applied model-configuration version only after the
   confirmed Serve readback, using the same operation/CAS and receipt chain.

The public `updateWorkspaceModels` request contains selections and an expected
version only; clients never choose a Gateway key binding. The binding is an
internal owner-to-owner fact created by Gateway for that exact configuration.
Default `opl-app` and Agent plus independent WebUI use this same path whenever
the runtime declares model configuration; a runtime that declares no such
publisher interface has no fabricated model-configuration capability.

This decision resolves the previous contract gap without making Serve a second
Gateway owner, weakening the non-null schema invariant, or introducing a global
workflow/event-bus authority.

### 2026-10-01: Fabric Owns Explicit Secret Binding Replacement

`FabricCoordination.BindSecret` remains the initial-bind and same-request replay
operation. It must not silently replace a different Secret: a retry with the same
runtime, purpose and Secret replays the confirmed binding, while a different
Secret is a conflict. This preserves the invariant that one runtime purpose has
one active Fabric binding and makes lost responses safe to recover.

A model-configuration update that changes the Gateway key therefore uses a
separate owner operation, `FabricCoordination.RebindSecret`. The replacement
command carries the expected current Fabric binding identity, the new opaque
Gateway binding and approved Secret delivery reference, the target purpose and
an idempotency key in `CallContext`. Fabric verifies the predecessor, asks the
provider to confirm the exact new Secret, and returns the new binding's version,
fingerprint, identity and receipt. It never receives or persists the raw key.

The replacement sequence is:

1. Workspace authorizes and validates the new model selection and creates a new
   exact Gateway managed-key binding.
2. Fabric performs `RebindSecret` against the expected current binding. The
   active-index invariant remains: Fabric atomically records the new confirmed
   binding as current and retains the predecessor as historical/recoverable;
   it does not allow two active bindings for one runtime purpose.
3. Serve receives the new opaque `RuntimeManagedKeyBinding`, applies the new
   publisher configuration and reads back the target applied version.
4. Workspace advances its applied model-configuration version only after the
   Serve readback and owner-local CAS confirm the new version.
5. Only after that confirmation does Workspace request Gateway to revoke the
   predecessor key. Fabric then records the predecessor binding as retired;
   Gateway key revocation and Fabric binding retirement remain separate owner
   facts.

If Fabric or Serve rejects, the predecessor Gateway key is not revoked. If a
replacement or compensation response is unknown, the same operation identity is
read back before any new side effect. A failed Serve apply may compensate by
re-binding the predecessor Secret through Fabric; a compensation result that is
unknown blocks later model changes until the owner readback resolves it.

This is a bounded replacement saga, not a global workflow engine and not a
second database writer. The public `updateWorkspaceModels` request remains
`expectedVersion + selections`; `RebindSecret` is an internal owner-to-owner
contract. The same semantics apply to default `opl-app` and Agent plus
independent WebUI whenever their Runtime declares model configuration.

The change is necessary because the existing active-binding uniqueness rule is
correct for idempotency but cannot represent a second legitimate configuration
without either fabricating success, dropping the uniqueness invariant, or
revoking the old Gateway key before the new runtime is confirmed. None of those
is acceptable under the Workspace, Gateway, Fabric and Serve ownership rules.

### Workspace Access Routing Owner

Serve is the sole business and persistence owner of each Workspace's current
application route. Its `serve.access_bindings` row is the authoritative route
selection; route generation, execution epoch, switch identity, and confirmed
target/readback are committed by Serve. The Serve access data plane resolves an
incoming Workspace application origin against that confirmed binding and
forwards only to the exact admitted, ready TKE Service target. It consumes
Serve-owned state and does not create a second route writer.

The installation's Ingress, DNS, and TLS are stable Instance configuration:
they deliver matching Workspace application-origin traffic to the Serve access
entry and are not edited for per-Workspace deployment switches. Fabric owns
infrastructure resources and their provider facts, not application routing.
Control Plane's existing Workspace reverse proxy and current-route resolution
are migration-source behavior only. Move its live access callers to the Serve
entry and retire the old route/proxy writer in the same bounded migration; it
must not remain a permanent fallback or a parallel reader of current selection.

Route activation is a Serve-local conditional state transition, not a CAS on a
second Kubernetes route object. Serve persists the switch before execution,
checks the expected generation/epoch and predecessor, commits the new binding
only after the target is ready, and returns an owner readback/receipt. A lost
acknowledgement is resolved by reading the original switch identity; unknown
state blocks later switches. External request verification through the stable
Instance Ingress proves that the access data plane serves the confirmed target.

`opl-instance-medopl` deploys Cloud and supplies installation configuration,
protected Secret references, certificates and installation receipts. Cloud owns
the customer Build/Workspace/Serve/Fabric workflows and reusable acceptance
harness. An authorized Instance runner may execute that harness against the
installed Cloud; it must not implement a second per-customer application
provisioning/deployment workflow. Installing the full necessary service set for
an isolated business chain is not itself a double writer; command ownership and
migration fences, not the count of running processes, establish the boundary.

Local-Docker remains a supported regression/qualification surface. Its current
formal-release receipt requirements remain intact, but completing all Local
features, legacy migration or later lifecycle features is not a prerequisite to
implementing and testing a bounded Tencent/TKE first-use slice. First-use proof
must not be relabelled complete F01–F17 or formal release qualification.

## 2026-09-29: Tenant-Scoped Application OCI Repositories On Personal TCR

The customer TCR account is Tencent TCR personal edition, so application output
uses one installation-owned namespace, `oplcloud`, and one private repository
per admitted Cloud Tenant:

```text
Tenant A  -> uswccr.ccs.tencentyun.com/oplcloud/alice
Tenant B  -> uswccr.ccs.tencentyun.com/oplcloud/huangrende
```

This is a repository per Tenant, not a TCR namespace per Tenant. The Tenant id
stays the authorization identity; the verified email local-part is only the
initial repository-name candidate, and a collision receives a deterministic
suffix, so two Tenants never alias one repository and a rename never moves
existing artifact history. `tenant_id -> repository` is reserved once and
persisted by the Tenant owner.

- `tenant`/CloudIdentity owns Tenant admission and the stable repository
  reservation.
- The Instance owner deploys the Cloud installation and supplies protected
  references for the personal TCR account, the `oplcloud` namespace and scoped
  registry credentials; it does not create each Tenant repository, does not run a
  per-application workflow, and does not deploy Tenant applications.
- `capability` owns Package namespaces, Package versions and uploaded bytes.
- `build` consumes the Tenant-owned destination through an authenticated typed
  owner read and owns the OCI push, immutable output identity, digest readback
  and artifact evidence.
- `serve`/Fabric own delivery of a selected OCI to a Workspace/TKE runtime;
  `ledger` owns append-only evidence.

Cloud Capability namespace, TCR namespace and TCR repository are three distinct
identities. The physical personal-TCR repository is materialized on the first
authorized Build push; users never submit a TCR destination or credential in a
Build request, and retries preserve the original repository and Build identity.

This is the generic Agent distribution path:

```text
Agent Package + approved Runtime + selected WebUI -> one immutable OCI
```

IBD/OMA is one reference Agent Package used to exercise the generic path; it does
not define a separate build owner, registry policy, OCI format, or required
multi-service deployment path.

Local-Docker and Tencent/TKE implement the same resolver and ownership rules and
may use different registry endpoints and credentials. The local source and live
chain are implemented and verified; a Tencent/TKE-hosted Cloud Build pushing a
Tenant-resolved destination to the real personal-TCR repository remains the
outstanding hosted proof and is not claimed here.

## 2026-09-22: Adopt The Domain-Separated Agent SaaS Target Architecture

This repository's target product is Agent SaaS: a customer selects an Agent
version and a compute/storage plan, pays, and receives a running online agent it
can use, update, and renew. The earlier path in which a customer bought bare
resources and an administrator separately deployed an application is replaced,
with an explicit migration for existing Workspaces, purchases, Keys, and
receipts.

The target is domain-separated services rather than one Control Plane process.
Each domain owns its data, its API, and its writes. The target domains are
`tenant` (CloudIdentity), `capability`, `build`, `workspace`,
`runtime_control`, `resource_catalog`, `gateway` (Gateway Integration),
`fabric`, and `ledger`; `console`/BFF is the browser aggregation surface.
All Cloud product code lives in the single GitHub repository `opl-cloud`.
`Capability`, `Build`, `Workspace`, `Runtime Control`, `Resource Catalog`, and
`Gateway Integration` are service modules inside that repository, not new
GitHub repositories. `Fabric` and `Ledger` keep their execution and evidence
authority. The physical target map is owned by
[Repository And Instance Topology](architecture.md#repository-and-instance-topology).

The product has one Agent delivery chain, not parallel Workspace and Agent Service lifecycles. A Workspace may have zero or one current Agent; replacing it creates a new deployment attempt and preserves history, but never exposes two current Agents. OPL Serve owns the Agent delivery/deployment lifecycle and its sole current-deployment fact for each Workspace. Workspace owns the Workspace identity, membership, entitlement, resource plan and target authorization, not an Agent deployment pointer or status copy. The Console/BFF reads the two owners and composes a product view without becoming a writer.

The owner flow is: the user may start upload in the Serve experience, while Capability owns upload sessions, Package metadata/versions and immutable Package bytes/references; Build fixes exact Package, WebUI and Runtime-release inputs and owns the build job and OCI evidence; Runtime Control owns the approved Runtime release catalog and immutable Runtime references consumed by Build, not deployed Agent instances; Workspace owns the target Workspace and resource entitlement; Fabric provisions/binds compute, storage and network resources and owns those resource facts; Serve deploys the built OCI to the authorized Workspace, owns deployment/readiness/routing state and exposes API, Embed and Hosted UI access to that same Agent. The Runtime implementation is supplied by the OPL App/Framework owner and is packaged into the OCI. Ledger records required evidence without becoming a lifecycle writer.

For avoidance of doubt, this is the new-customer entry: Console/Serve starts
the native OMA Package flow, the approved OPL App Runtime and selected WebUI are
fixed into a Build-owned immutable TCR OCI, and the customer selects that Agent
version together with the Workspace plan. A successful new-customer outcome
requires Serve readback of the delivered/current Agent; a resource-only
readback is intermediate evidence, not completion. The earlier
`resource_only` order remains valid only as a retained historical/legacy
contract and is not silently converted into a new Agent order.

Serve is a Cloud service/data Owner because it owns a durable per-Workspace Agent delivery lifecycle. This adds one service module and one data Owner to the target topology; Runtime Control is an existing target module with a narrowed, accurately documented version-catalog responsibility, not a new service. Product entry screens may live in the Console UI/Serve experience, but UI placement never transfers Package or deployment write authority.

The remaining target decisions are:

- Each business service keeps its own Go module, process, and service boundary.
  CloudIdentity (`tenant`) remains inside the Gateway Integration module and
  deployment unit, with a separate database/writer boundary from `gateway`.
  Proto service groups are API groups, not extra processes. The old Control
  Plane is a bounded migration source, not a permanent parallel writer.
- Shared wire contracts use the existing `packages/contracts/go/go.mod` module:
  proto source belongs in `packages/contracts/proto/`, and generated target architecture Go
  bindings belong in `packages/contracts/go/v226/`. No independent target architecture Go
  module or contracts GitHub repository is introduced. Cloud consumers and
  contracts are revised atomically at one source commit, with schema hashes
  and locked generation tools. Necessary consumer dependency-file changes are
  part of W01; unchanged dependency files are not an architectural boundary.
- Consolidation covers Cloud product code only. Instance deployment, Sub2API
  wallet/Gateway authority, and Framework remain outside this repository;
  their exact artifact references and authority boundaries remain intact.
- Each data owner has its own PostgreSQL database and owner/writer roles. Cross-
  owner references use opaque identifiers only; there are no cross-domain
  foreign keys, joins, or transactions.
- Browser traffic reaches the product only through the Console BFF over REST.
  Internal calls use typed gRPC/protobuf. Reliable events use a per-domain
  PostgreSQL Outbox delivered to a consumer Inbox; no additional message
  runtime is introduced for the current chains.
- The BFF performs authentication, authorization context, and product DTO
  aggregation only. The Workspace service owns the business Saga.
- New-customer Workspace admission is Agent-version-plus-plan, not a bare
  `resource_only` purchase followed by administrator installation. Workspace
  owns entitlement and target authorization; Serve owns the Agent deployment,
  readiness, access and current-selection facts. The old Control Plane/Fabric
  application writer is migration source only and must be retired when its
  callers move.
- Serve's `serve.access_bindings` is also the sole route-selection authority.
  Serve's access data plane reads the committed binding; Instance Ingress/DNS/TLS
  stays stable and forwards to that entry. Neither Control Plane nor Fabric owns
  or writes the current application route, and no second Kubernetes route CAS
  object is introduced.
- `Capability` owns Package, version, and catalog metadata while object bytes
  live in the storage provider. `Build` reads immutable references and writes
  its own jobs and artifact evidence. A ready Capability version is created only
  after a successful build; there is no pending version without a digest.
- Package, OCI references, and Build history are not deleted with a Workspace.
  Package archival does not cascade to history, and there is no automatic
  90-day purge.
- New identifiers are opaque strings. Existing identifiers, original charge
  keys, original receipts, and timestamps are preserved across migration.
- Money is `USDMicros` on the wire and `bigint` in PostgreSQL; times are UTC
  RFC3339. Authorized product roles are `owner`, `admin`, `member`, and
  `platform_admin`. Login identity is separate from the Tenant billing subject.
- The Console stays React/TypeScript. Public marketplace, self-service
  third-party WebUI publishing, cross-Tenant private OCI sharing, arbitrary
  customer-selected Runtime images, and public registration are out of this
  delivery unless separately decided.

The authoritative product, field, API, frontend, migration, and acceptance
specification is the target architecture development specification retained under
[`docs/spec/target`](./spec/target/00_master_index.md). That specification is a
target and planning owner, not implementation evidence.

### Superseded Decisions

This decision replaces two earlier statements, which are retained here only as
history:

- **2026-08-15: Keep The Current Service Architecture Until A Real Gap Pays For
  Change.** Its requirement that Cloud remain exactly Control Plane, Fabric,
  and Ledger is superseded for the target. Its general rule still holds: a new
  service still needs a current caller, an observed missing capability, a
  bounded migration, and an owner. The target architecture work packages supply that
  justification for the target domains.
- **2026-09-11: Control Plane Coordinates Applications And Keeps One Process.**
  Its single-process requirement is superseded. Its application-authority
  boundary remains: the owning service admits immutable revisions and drives
  lifecycle, while application behavior and data formats stay with the
  publisher, execution and readback stay with Fabric, wallet authority stays
  with Sub2API, and evidence stays with Ledger.

## 2026-09-17: A Customer Launch Delivers Resources, Not An Application

> **Superseded as the new-customer default entry** by
> [2026-09-22: Adopt The Domain-Separated Agent SaaS Target Architecture](#2026-09-22-adopt-the-domain-separated-agent-saas-target-architecture).
> The new customer entry is Agent version plus plan. This decision remains the
> historical interpretation of an existing resource-only Launch, and the
> retained Launch obligations below still hold.

A Workspace purchase ends at resource fulfillment. The customer Console path
opens compute, storage, attachment and the Workspace's entitlement, and it does
not install a default application: no installation operation, application
Gateway Key or application login is part of a customer Launch. Deploying an
application afterwards is a separate authorized administrator operation on the
resources that Launch already delivered. A customer Launch therefore always
declares its provisioning shape explicitly instead of inheriting a default.

This does not retire the retained Launch contract. Historical Launches keep the
completion obligations recorded in their own operation, and the operator
qualification flows that still install and verify the Workspace runtime continue
to use the retained provisioning mode until their own migration lands.

## 2026-09-17: One Origin Per Workspace-Application Binding, Derived Not Allocated

Each Workspace's binding to an application is published at its own browser
origin, and the origin is a pure function of the binding identity. No module
allocates it, no table records it, and no module can disagree about it: the same
pair always composes the same name.

The name carries the Workspace identity so routing resolves a request without a
lookup. The application component is in the name because an origin has to change
when the Workspace's application changes: a compatible update keeps the same
application identity and therefore the same origin, so a visitor's session
survives, while an unrelated application gets a different origin, so the
previous application's service worker or browser storage cannot act on its
replacement. A name whose application component no longer matches the current
binding belonged to a superseded application and is refused rather than served.

The Serve access read returns only a confirmed ready entry. A missing, not-ready,
or unprotected Cloud-private entry returns `APP_ACCESS_UNAVAILABLE`. Static
application origins do not expire through a second Cloud session authority;
`expiresAt` is optional and is reported only when the entry provider supplies an
actual expiry. This preserves the application's own authentication and does not
mint an arbitrary access lifetime.

A binding origin belongs entirely to its application. Requests are dispatched to
the binding before the management route table, so this server's own routes never
answer on an application host. Only platform credentials are removed from the
forwarded request; the application keeps its own authorization and cookies, and
a response cookie cannot widen onto a sibling binding or onto the Console. The
proxy states the external host and scheme itself instead of forwarding a
caller-supplied claim.

The domain origins are published under is its own installation value, separate
from the Workspace host. The layout is a deliberate choice with a cost: origins
sit one label under the zone so the public DNS proxy's free edge certificate,
which covers a root domain and its first-level subdomains, keeps them in scope.
One label deeper would require both a paid edge certificate and a purchased
certificate on the load balancer. Because the proxy terminates TLS for these
names, the origins declare no Ingress TLS entry.

The instance still owns the domain, its wildcard DNS record and its certificate.
An installation that states no application-origin domain, or a binding whose
Workspace identity cannot compose into a hostname, has no origin and keeps the
retained path-based entry rather than an address that cannot resolve.

## 2026-09-17: One Image Reference Format, Owned By The Contract

An executable image is identified as `host/namespace/repository@digest`
everywhere: in the registry catalog response, in the resolved reference an
administrator selects, in admission, and in what Fabric executes. A plain
`repository@digest` is not an image reference and must never be produced by one
module and re-derived by another. The registry host is returned with the
resolution so a consumer confirms the exact identity the owner produced rather
than assembling a second format.

Image selection is discovery, not publication. Choosing a repository and tag
resolves the exact digest and platform for one deployment; it is not a global
application publication or marketplace step. A tag is discovery input only, and
an accepted operation keeps the digest it resolved rather than following a tag
that moved.

The registry endpoint and its credentials are installation facts. Cloud has no
default registry host, and an installation that configures none has no image
selection capability rather than an anonymous one; its routes answer an explicit
unconfigured result.

The repositories an installation approves for deployment are also an
installation fact, and they are declared rather than discovered. A registry's
catalog endpoint cannot answer the question: the same credential that reads a
repository's tags can return an empty catalog, and an empty catalog is
indistinguishable from an installation that approved nothing. Declaring the set
is also the narrower statement, and it keeps the approval decision with the
installation instead of with whatever the registry happens to list. A configured
registry that declares no approved repository fails startup rather than
reporting an empty catalog. The instance owns the endpoint and the Secret material; it
injects browse credentials to Control Plane and keeps the node pull credential
in the runtime environment. The cataloged namespace boundary stays a server-side
admission rule, so configuring a host never widens which namespaces may be
browsed. Which identity holds which permission is an installation choice; Cloud
does not require Control Plane and the runtime nodes to share one credential.

## 2026-09-11: Control Plane Coordinates Applications And Keeps One Process

> **Superseded for the target** by [2026-09-22: Adopt The Domain-Separated
> Agent SaaS Target Architecture](#2026-09-22-adopt-the-domain-separated-agent-saas-target-architecture).
> The single-process requirement no longer holds; the application-authority
> boundary described below still does.

Control Plane's application authority is admission and deployment coordination,
not application business. It admits immutable revisions, selects each
Workspace's current deployment binding and drives lifecycle operations.
Application behavior and data formats stay with the publisher; execution and
authoritative readback stay with Fabric; wallet and gateway-key authority stay
with Sub2API; evidence stays with Ledger.

Control Plane remains a single process; separation happens through package
boundaries, typed contracts and owner-local persistence. A process split waits
for measured scale or isolation need. Its Fabric, Ledger and Sub2API service
tokens stay process-scoped and are never forwarded to applications, browser
sessions or application traffic; an application receives only the secrets its
own deployment declares, and administrator credentials do not reach the
application runtime.

The boundary details live in
[Control Plane authority and credential boundary](architecture.md#control-plane-authority-and-credential-boundary).

## 2026-09-10: Separate Workspace Identity From Its Application

A Workspace is an account-owned isolated application environment with stable
identity, access, resource entitlement and data bindings. OPL App is its default
application, not the definition of the environment. Other OCI container images
may be deployed when their declared requirements match the selected provider's
capabilities and the owner's policy. An application may comprise several
services; uploading its main image alone does not supply dependencies or data.

The IBD delivery is the concrete second application motivating this decision:
its existing image uses different ports, application sessions, configuration,
and external knowledge services. Requiring each application to imitate OPL App
would preserve the coupling. The target instead admits an immutable application
description and separates resource provisioning, application deployment and
data restoration into independent commands. A provisioned Workspace may have
no installed application. Cloud management requires role-specific authorization;
application visitors may be anonymous, use application-owned login, or use an
optional Cloud-private entry according to the selected exposure policy.
Application business logic, packaging, formats and restore algorithms remain
with the application owner; several services may run on one CVM or be packaged
in one image.

The 2026-09-11 refinement makes initial application distribution an administrator
operation against explicitly selected Workspaces. Each Workspace pins its own
revision and data bindings; publishing another image or changing a default does
not update existing installations. Customers use the selected application and
retain their separately authorized Workspace lifecycle actions. Existing admin
resource views are extended for deployment, rather than introducing a customer
application marketplace. Tencent application data uses Workspace-owned CBS via
explicit mounts; ordinary image updates reuse those bindings and never restore,
overwrite or delete retained data. This preserves the existing paid retention
and deletion boundaries and does not promise automatic backups.

The design and ownership are defined in
[Workspace application boundary](architecture.md#workspace-application-boundary).
The current fixed Runtime ABI remains an implementation fact until a coordinated
migration; the open outcomes are in
[the roadmap](roadmap.md#workspace-application-decoupling).

## 2026-09-10: Integrate The Unmodified Gateway Through Its Native APIs

Cloud integrates official Sub2API 0.2.4 without modifying Gateway source, images,
database schema, or settings. Workspace deletion and failed-Launch closeout
retain Gateway Keys; they remove Cloud/Fabric resources and injected Secrets.
A retained Key remains managed and metered by the Gateway account owner.
Unpaid expiry still closes Cloud access and stops the original Runtime.

Cloud reserves each wallet dispatch on its existing durable operation and uses
the native atomic admin balance adjustment. Recovery reads the original audit
record; a missing record after reservation remains pending or manual review and
never authorizes another debit or refund. Operator recovery also remains read-only.
Refunds preserve the original account and reserved refund limit and require
readback that native admin adjustments cannot award an affiliate rebate.

The previous patched-Gateway approach is historical test evidence, not an
Instance adoption requirement. The native API wire binding belongs to
[implementation architecture](./implementation-architecture.md).

## 2026-08-20: Cloud Owns The Product; Instances Own Installations

> The repository name below is historical. The 2026-09-22 decision uses
> `opl-cloud` for the single Cloud GitHub repository and keeps this
> product/Instance authority boundary unchanged.

`one-person-lab-cloud` is the single product and implementation repository for
Console, Control Plane, Fabric, Ledger, reusable provider adapters, portable
installation assets, Candidate images, and formal Releases. `opl-cloud` remains
the internal package, image, service, namespace, and runner identifier.

An installation is an explicit instance, not a fork of the product. An instance
selects domains, Provider Profile, enabled plans, immutable Workspace image,
Secrets, deployment policy, and rollback procedure while consuming an immutable
Cloud artifact. `opl-instance-medopl` owns those facts for medopl. Product source
and customer pricing stay in Cloud; production state and receipts stay in the
instance owner.

## 2026-08-20: Cloud Prices The Product; Sub2API Owns Spendable Balance

Control Plane owns the versioned customer price catalog, integer USD-micros
quotes, purchase eligibility, accepted price snapshots, and settlement
coordination. Console presents Control Plane DTOs. Fabric reports provider
availability, capacity, and cost evidence. Ledger records accepted price and
receipt evidence.

Sub2API remains the only spendable-wallet, API Key, model-routing, and usage
authority. Its management origin and credentials stay server-side. Basic and Pro
are prepaid monthly Workspace packages; provider compute and storage details do
not become separate customer charges.

An Instance may enable only the plans its Provider Profile can fulfill, but it
does not redefine Cloud customer prices.

Ledger's Evidence Index is an append-only cross-surface lookup projection, not a
second authority. It stores operation, Candidate, receipt, status, and redacted
link facts with idempotent writes; source state, wallet state, provider state,
and Instance state remain owned by their existing services.

## 2026-08-17: Control Plane Owns Workspace Purchase Eligibility

External identity, spendable balance, a Cloud Account, and permission to buy a
Workspace are separate facts. `workspacePurchaseEnabled` on the Control Plane
Account is the purchase-eligibility authority. Provisioning chooses the account
scope explicitly, and grant or revoke is an audited operator command.

Revocation blocks future purchases. It does not delete or alter existing
Workspaces. Historical accounts remain disabled until an authorized migration
and readback changes them.

## 2026-08-11: One Launch Coordinates Separate Physical Owners

A Workspace Launch has one durable Control Plane operation. Create and
operator-authorized Resume enter the same Reconciler. The operation advances
through admission, Key and debit coordination, Fabric resource stages,
activation, and Receipt creation.

Control Plane owns the business cursor, account policy, settlement coordination,
and customer projection. Fabric owns compute, storage, attachment, Secret
binding, Runtime, provider mutation, and authoritative resource readback. Ledger
owns append-only receipts, reconciliation, idempotency, and opaque provenance.
Sub2API owns identity, wallet, Keys, and usage.

Control Plane and Fabric bind every write to an explicit provider-neutral stage
request identity. Unknown external results remain recoverable from persisted
identity and owner readback; recovery continues the original operation and does
not create a second state machine or resource writer.

Legacy Launch migration is implemented only for a proven persisted consumer. It
must preserve identity, money, resource, idempotency, billing-period, and attempt
facts with exact-row compare-and-swap. Otherwise the row remains manual review.

## 2026-08-15: Keep The Current Service Architecture Until A Real Gap Pays For Change

> **Superseded for the target** by [2026-09-22: Adopt The Domain-Separated
> Agent SaaS Target Architecture](#2026-09-22-adopt-the-domain-separated-agent-saas-target-architecture).
> Retained as history; the general "new service needs a current caller" rule
> still applies.

Console remains a TypeScript browser application. Control Plane, Fabric, and
Ledger remain separate Go modules, processes, and PostgreSQL schema owners.
Cross-service integration uses typed public HTTP contracts and owner readback.

Architecture work improves cohesion inside the existing owner first. A new
framework, runtime, service, shared infrastructure layer, or durable workflow
engine requires a current caller, an observed missing capability, a bounded
migration and rollback path, and measurable benefit over changing the owner
directly.

Framework-side composition integrates through a Framework-owned typed Cloud
client. It does not move Cloud service, database, provider, billing, release, or
deployment authority into the Framework process.

## 2026-08-15: Qualify A Candidate Before Formal Publication

During pre-1.0, Cloud first builds a replaceable Candidate from one exact
canonical source SHA. The Candidate is one multi-architecture OCI identity plus
a checksum-bound portable installation bundle. Local-Docker and the instance
owner qualify those same Cloud image bytes with their own Workspace image,
Provider Profile, domain, and runtime receipts.

A formal Product Release promotes the already qualified Cloud digest without a
rebuild. Cloud publication does not dispatch or operate an instance. Only the
repository owner and `RenDeHuang` may manually publish from `main`, with the
original publisher still acting as the triggering actor.

## 2026-07-19: Retire A Path After Its Real Consumers Move

Before deleting a route, field, state path, or persisted model, trace current
callers, non-terminal data, external consumers, and cleanup ownership. Move the
real consumers, read back the successor, then remove the old application path.

Executed migrations, billing history, Receipts, and externally owned resources
remain under their established custody. A compatibility path is retained only
when a current consumer or unreconstructable state still needs it.

## 2026-07-19: Evidence Is Reported At Its Own Layer

Source and tests can prove implementation behavior. A local runtime can prove
that exact local configuration. A Candidate receipt can prove artifact identity.
An instance receipt can prove deployment and runtime state for that candidate.
Formal publication can prove a public Release. No lower layer implies a higher
one.
