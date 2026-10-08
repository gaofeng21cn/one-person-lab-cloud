# OPL Cloud Implementation Packages

This directory contains shared package boundaries only. Runtime ownership now lives under `services/*`; browser ownership lives under `apps/console-ui`.

## Packages

| Package | Current role | Runtime owner |
| --- | --- | --- |
| `contracts` | Artifact and byte-level JSON contracts plus shared Go runtime types; [contract ownership](contracts/README.md) | Actual contract consumers; current service paths are owned by [implementation architecture](../docs/implementation-architecture.md) |

## Current Boundary

The current service/module inventory, Console identity routing, extracted
owners, retained Control Plane callers and protocol paths are owned by
[implementation architecture](../docs/implementation-architecture.md). This
package entry does not freeze a service count or assume all Console surfaces
call only Control Plane. Stable domain authority remains in the canonical
architecture documents, not in an implementation inventory or phase plan.
Runtime services remain under `services/*`; `packages/contracts` contains the
shared machine contracts.

The Console experience principles live in
`docs/product/console-experience-guide.md`; current routes, components and
presentation live in `apps/console-ui`. Visual and navigation choices are not
machine contracts. Downstream authority never moves into the browser.

## Ownership Rule

- Console consumes the owning browser API's typed customer projections, not
  service implementation imports or downstream persistence models; current
  BFF and retained Control Plane routes follow implementation architecture.
- Resource catalog policy and Fabric provider/execution facts retain their
  distinct canonical owners; shared packages do not merge their authority.
- Ledger owns receipts, reconciliation, retention, and opaque evidence under `services/ledger`.
- Retained Control Plane and extracted Workspace responsibilities follow the
  current implementation map, with one writer for each migrated capability.
  Compute, storage and attachment rows remain Workspace details and Fabric
  provider facts, not standalone customer purchase surfaces.
- The default Workspace runtime template remains `one-person-lab-app`; template behavior belongs to that app contract, not to Console billing or resource ownership.

## Development And Evidence

Package work follows [Scoped Development Contract](../AGENTS.md#scoped-development-contract)
and the existing [developer entry](../DEV_GUIDE.md#scoped-host-and-worker-entry).
An existing phase-plan record supplies the task; canonical source ownership is
not moved into a new registry. Use `npm run dev:tools` and `npm run dev:run` as
host integration entries. Only the trusted host/dedicated restricted worker can
enforce admission; attaching tools to a shell-enabled chat is not confinement.

[Generated-field freshness](contracts/README.md#generated-field-freshness)
checks covered outputs, not W01 migration or all business fields. Fixtures and
source evidence do not establish installed product runtime or Instance readiness.
