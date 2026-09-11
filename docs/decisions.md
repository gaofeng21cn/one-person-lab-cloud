# Decisions

This file records durable product and architecture choices. Current
implementation evidence belongs in [status.md](./status.md); unfinished outcomes
belong in [roadmap.md](./roadmap.md).

## 2026-09-11: Control Plane Coordinates Applications And Keeps One Process

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
