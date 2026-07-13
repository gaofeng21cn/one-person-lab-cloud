# Platform Capability Gaps

OPL Cloud is the target infrastructure family for OPL products and domain
Agents. This document separates functional/structural gaps from later live
evidence; it does not claim implementation readiness.

## Functional And Structural Gaps

| Capability | Target boundary | Gap to close |
| --- | --- | --- |
| Workspace workbench | Continuous App/Workspace project, task, artifact and review model | Close one explicit shared workbench contract |
| Console governance | Organization policy, roles, approval, quota, billing and managed lifecycle | Keep policy distinct from package/resource mutation |
| Fabric resources | Compute, storage, connector and environment availability/binding | Close one shared plan/execute/collect envelope |
| Package projection | OPL Packages is sole manifest/digest/install/lock/update/rollback/repair owner | Make every Cloud surface consume exact refs without a registry copy |
| Connector access | Connect owns shared access; domain adapters own domain semantics | Remove false shared-connector/currentness claims |
| Evidence | Ledger records receipt/provenance/review/continuation refs | Keep refs connected to owner sources without copying truth |
| Usage and quota | Console groups managed usage by organization/workspace/resource/task | Distinguish commercial service plans from OPL Packages |

## Evidence Gaps

- real App-to-Workspace continuation;
- real Cloud-hosted and institution-owned resource paths;
- sensitive-data egress policy and owner acceptance;
- provider and connector runtime evidence;
- managed billing and quota readback;
- exact user-visible receipts and continuation;
- production soak and release evidence.

Docs, contracts, generated projections and focused tests can close structural
gaps but cannot substitute for these evidence lanes.

## Package No-Second-Truth Rule

Cloud does not create an Agent Registry. OPL Packages provides the validated
manifest, digest, dependency closure, installed state, lock and lifecycle
receipt. Console projects organization availability, Fabric projects resource
binding, Workspace projects user state/actions and Ledger stores refs. Every
projection must route mutation back to OPL Packages.

## Suggested Planning Order

1. Shared App/Workspace workbench contract.
2. Package projection and owner routing.
3. Organization and approval model.
4. Resource plan/execution contract.
5. Connector and domain-adapter boundary.
6. Evidence/continuation model.
7. Usage and quota model.
8. Live provider, resource and user-path evidence.
