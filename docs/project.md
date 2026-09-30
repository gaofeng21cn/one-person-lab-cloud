# Project Scope

This is the single `one-person-lab-cloud` GitHub product and implementation
repository for OPL Cloud. It follows the development framework from
`one-person-lab`; `opl-cloud` also remains the package and runtime identifier.
All target Cloud domain services stay in this repository with independent
module, process and data-owner boundaries. The adopted target and detailed
placement are owned by [architecture.md](./architecture.md) and the
[target architecture owner mapping](./spec/target/01_domain_ownership_matrix.md); external
Instance, Sub2API and Framework authorities are not consolidated here.

## Owned Here

The current module and caller map lives in
[implementation architecture](implementation-architecture.md). This scope covers
the extracted owners and retained migration paths without implying completed
Instance qualification.

- Console UI, its runtime route registry and the browser-facing Console BFF.
- CloudIdentity/Tenant and native Gateway integration; Capability Package/catalog
  facts; immutable Build jobs/artifacts; approved Runtime Release references.
- Workspace business operations, Resource Catalog plans/pricing policies and
  Serve application delivery/readiness/access.
- Retained Control Plane Sessions, account mapping, permissions, named product DTOs,
  Workspace state machines, purchase recovery, and product projections. Historical
  Support mapping custody remains here; the customer Support capability is retired.
- Fabric resource catalog, provider-neutral resource operations, attachments,
  runtime operations, provider evidence, and provider adapters, including the
  explicitly selected Local-Docker and Tencent/TKE paths. Historical migrations
  and data preserve ContentTransfer and Snapshot/Restore custody.
- Ledger receipts, reconciliation evidence, idempotency, retention, and
  caller-owned opaque provenance required by Core, including custody of
  historical rows and receipt provenance columns.
- Portable image, Compose installation assets, product release, readiness, and
  reusable provider-verification mechanisms.

## Instance Boundary

`opl-instance-medopl` owns the concrete medopl installation: domains, provider
profile, region and resource ids, the enabled subset of Cloud-defined plans,
image pins, secret references, promotion policy, and deployment receipts. Cloud
Resource Catalog owns the extracted plans and price policies; Control Plane
retains its legacy catalog for existing callers. The Instance does not
copy this repository's runtime code or product contracts.

Medopl-specific manifests, production workflows, Secrets, runbooks, rollback,
canaries, and receipts belong only in that instance repository. This repository
retains provider adapter source and portable product-release mechanisms, but no
automatic instance deployment writer.

## External

- Sub2API, reached only through the server-only configured management origin:
  spendable balance, API keys, models, routing, and request usage.
- Application publishers: OCI images, application behavior, configuration
  formats and data restore algorithms. `one-person-lab-app` supplies the default
  Workspace workbench application.
- `one-person-lab`: framework and CLI behavior.
- Tencent Cloud: current medopl provider resources and internal cost.
