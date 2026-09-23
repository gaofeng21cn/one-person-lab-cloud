# Development Specifications

This directory holds retained development specifications that the product's
canonical owners adopt by reference.

## v2.26 Target Development Specification

`v2.26/` is the accepted target and planning specification for the
domain-separated Agent SaaS architecture. The durable decision that adopts it is
[2026-09-22: Adopt The Domain-Separated Agent SaaS Target Architecture](../decisions.md).

- Entry point: [00_master_index.md](./v2.26/00_master_index.md)
- Product main description: [12_product_spec.md](./v2.26/12_product_spec.md)
- Implementation work packages: [14_implementation_work_packages.md](./v2.26/14_implementation_work_packages.md)
- Machine contracts: [`v2.26/contracts/`](./v2.26/contracts)
- Verification receipts: [`v2.26/checks/`](./v2.26/checks)

### Evidence Layer

This specification is a target and planning owner. It is **not** implementation
or production evidence. Its own self-check reports
`status: ready_for_implementation`, which means the contract set is internally
consistent and reviewable; it does not mean any service is implemented.

The specification's verification scope is documentation contracts, an
interactive prototype, and isolated PostgreSQL contract tests using named
fixtures. Provider router and Storage are explicit fixtures, not implemented
services.

### Source Freeze

This copy was frozen when the target architecture was adopted:

- Adopting repository: `RenDeHuang/opl-cloud`
- Cloud source SHA the specification read as its migration start point:
  `50520e27a6b9a630eefdc2df7da3e5ec498d28a0`
- Upstream provenance: `gaofeng21cn/one-person-lab-cloud` at that SHA
- Specification self-check counts: 17 features, 89 typed flow steps, 108 REST
  operations, 96 tables, 172 internal RPCs, 32 work packages

The specification ships its own `checks/validate_*.py` scripts and recorded
receipts under `v2.26/checks/runs/`. Re-run them with the versions pinned in
`v2.26/checks/requirements.txt`.
