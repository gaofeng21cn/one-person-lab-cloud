# OPL Cloud Documentation

Owner: `one-person-lab-cloud`
Purpose: `docs_index`
State: `active_index`
Machine boundary: Human-readable navigation and ownership map. This index does
not prove Cloud implementation, runtime state, billing, release, security,
owner acceptance, or production readiness.

This repository is a documentation and product-architecture aggregation
surface. It defines the OPL Cloud target product split and routes implementation
questions to their owners; it does not host a Cloud runtime or duplicate
Framework, App, service, package, or domain truth.

## Current Owners

| Theme | Single Source of Truth | Role |
| --- | --- | --- |
| Public product entry | [README](../README.md) and [中文入口](../README.zh-CN.md) | User problem, value, journey, and repository boundary |
| Current status and open gaps | [Roadmap](./roadmap.md) | Single Active Truth plan, current summary, open gaps, and next Agent prompt |
| Technical product split | [Architecture](./architecture.md) | Cloud, Framework, App, and domain authority boundaries |
| Workspace identity | [Workspace identity and external SaaS boundary](./workspace-identity-and-external-saas-boundary.md) | One-account/one-primary-Workspace decision and excluded external owners |
| Public vision | [OPL Cloud whitepaper](./whitepapers/opl-cloud-whitepaper.md) | Product rationale and target experience |
| Publication evidence boundary | [Whitepaper delivery](./delivery/whitepapers/README.md) and [generated-site boundary](./site/README.md) | Build, publication, and exact-byte readback separation |

Canonical filenames are mapped rather than duplicated: the root README carries
the `project` role, `roadmap.md` carries `status` and the Active Truth role,
`AGENTS.md` plus `architecture.md` carry repository invariants, and durable
product decisions live in the architecture or a focused decision owner such as
the Workspace identity document. This repository does not need separate
`project.md`, `status.md`, `invariants.md`, or `decisions.md` copies.

## Product References

| Surface | Target reference |
| --- | --- |
| AI access | [OPL Gateway](./opl-gateway.md) |
| Online workbench | [OPL Workspace](./opl-workspace.md) |
| Agent publication | [OPL Serve](./opl-serve.md) |
| Account governance | [OPL Console](./opl-console.md) |
| Resource substrate | [OPL Fabric](./opl-fabric.md) |
| Connector capability | [OPL Connect](./opl-connect.md) |
| Evidence refs | [OPL Ledger](./opl-ledger.md) |
| Cross-surface Agent objects | [Agent lifecycle](./agent-lifecycle.md) |

These documents define target product responsibilities. They do not establish
that a service, endpoint, resource, account, receipt store, or billing path is
implemented or available.

## Planning Contracts

The files below are human-readable target contracts, not executable schemas:

- [Shared execution contract](./contracts/shared-execution-contract.md)
- [Agent service publication contract](./contracts/agent-service-publication-contract.md)
- [Fabric adapter contract](./contracts/fabric-adapter-contract.md)
- [Workspace lifecycle](./contracts/workspace-lifecycle.md)
- [Ledger receipt schema](./contracts/ledger-receipt-schema.md)
- [Resource ownership and billing](./contracts/resource-ownership-and-billing.md)

Implementation must introduce an owning machine contract and runtime readback in
the proper implementation surface. Prose here cannot become that authority by
itself.

## History And Provenance

[History](./history/README.md) contains superseded research translations and
retired model tombstones. History explains why a boundary changed but cannot
be used as a current product, contract, runtime, or readiness source.

## Current Portfolio Coverage

Every tracked `README*` and `docs/**/*.md` file is assigned below; there is no
unclassified active document.

| Lifecycle | Covered files |
| --- | --- |
| Public entries | `README.md`, `README.zh-CN.md` |
| Asset inventory | `assets/README.md` |
| Navigation and Active Truth | `docs/README.md`, `docs/roadmap.md` |
| Canonical architecture and decisions | `docs/architecture.md`, `docs/workspace-identity-and-external-saas-boundary.md` |
| Product target references | `docs/opl-gateway.md`, `docs/opl-workspace.md`, `docs/opl-serve.md`, `docs/opl-console.md`, `docs/opl-fabric.md`, `docs/opl-connect.md`, `docs/opl-ledger.md`, `docs/agent-lifecycle.md` |
| Human-readable planning contracts | `docs/contracts/shared-execution-contract.md`, `docs/contracts/agent-service-publication-contract.md`, `docs/contracts/fabric-adapter-contract.md`, `docs/contracts/workspace-lifecycle.md`, `docs/contracts/ledger-receipt-schema.md`, `docs/contracts/resource-ownership-and-billing.md` |
| Whitepaper source and evidence boundary | `docs/whitepapers/README.md`, `docs/whitepapers/opl-cloud-whitepaper.md`, `docs/delivery/whitepapers/README.md`, `docs/site/README.md` |
| History and tombstones | `docs/history/README.md`, `docs/history/research-provenance.md`, `docs/history/retired-agent-registry.md` |
| Ignored generated output | `docs/site/latest/**` is derived from the whitepaper source/profile and covered by `docs/site/README.md`; it has no independent truth or lifecycle owner |

## Growth Rule

Add a document only when it has a durable owner, one purpose, one lifecycle
state, and a machine boundary that cannot be expressed by an existing owner.
New current gaps go only to `roadmap.md`; implementation or Live Evidence stays
with the owning repository or service.
