# Decisions

## Review package: historical definition and proposed Cloud boundary

This section is the reviewable product and architecture proposal after the September 21, 2026 source baseline. It deliberately separates the historical definition from the proposed target. It is not implementation or release evidence.

| Area | Historical definition at the baseline | Proposed definition for approval | Why the change is necessary | Affected modules, fields, and interactions |
| --- | --- | --- | --- | --- |
| Product identity | Workspace and externally served Agent Service are separate product objects. | Workspace is the delivery target and has at most one current Agent; API, Embed, and Hosted UI reach that same current Agent. | Removes competing lifecycles and makes replacement, access, and deletion unambiguous. | `workspace` keeps identity, membership, entitlement, and resource plan; `serve.agent_deployments` is the sole deployment/current-state writer; BFF/UI read owner projections. |
| Package ownership | Publication is described at product level without one Cloud Package writer. | Capability owns uploaded Package bytes, identity, metadata, and immutable versions. | Prevents Serve, Workspace, or Build from becoming a second Package authority. | Capability package/version/upload APIs; Build consumes opaque version IDs and digest claims; Serve UI starts the flow but does not persist Package truth. |
| Runtime release | Runtime and serving responsibilities are not separated in the product contract. | Runtime Control owns only the approved Runtime Release catalog; OPL App/Framework owns Runtime implementation. | Separates version admission from instance deployment and execution. | `runtime_release_id`, digest, ABI/compatibility claims; Build fixes the release in its input snapshot; Serve never writes the catalog. |
| OCI build | Image/runtime selection can be read as part of Control Plane/Fabric application delivery. | Build fixes Package + WebUI + Runtime Release and emits one immutable OCI digest. | Makes the executable reproducible and prevents mutable tag drift. | `build_jobs`, input snapshot, artifact digest; Build reads Capability/Runtime Control and hands the digest to Serve. |
| Workspace authority | Workspace orchestration may carry application/deployment selection. | Workspace owns identity, member authorization, entitlement, resource plan, quote/purchase obligations, and target authorization; it stores no current-Agent deployment copy. | Keeps business entitlement separate from delivery truth. | Workspace fields remain business-only; Workspace→Serve passes opaque workspace/tenant/grant/resource references; duplicate current-deployment fields are retired. |
| Fabric authority | Fabric is a broad runtime/resource substrate and may be mistaken for application readiness authority. | Fabric only provisions, binds, and reads back compute/storage/network/Secret resource facts. | Resource readiness is not Agent readiness; each fact needs one writer. | Fabric resource-set/action/readback fields; Serve consumes resource refs but owns deployment/readiness/access. |
| Serve authority | Serve is an external Agent Service publication surface without one Workspace deployment owner. | Serve is the sole Agent delivery/deployment/readiness/access owner for a Workspace, retaining history and one current selection. | Gives the delivery chain one real result and removes duplicate deployment writers. | `agent_deployments`, `agent_runtime_instances`, `access_bindings`; delivery/readiness/access APIs; all access modes share one route. |
| Evidence and presentation | Console/Control Plane projections can be mistaken for business truth. | Ledger remains append-only evidence; BFF/UI aggregate owner readbacks and persist no business copy. | Preserves DDD ownership and traceability. | Typed RPC/events use opaque IDs; BFF DTOs name reporting owners; Ledger receives receipt references only. |

This proposal requires review of the public product promise and the canonical engineering contracts together. It does not authorize a merge, deployment, publication, or end-to-end implementation claim.


This file records durable product and architecture choices. Current
implementation evidence belongs in [status.md](./status.md); unfinished outcomes
belong in [roadmap.md](./roadmap.md).

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
