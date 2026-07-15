# Platform Capability Gaps

OPL Cloud is the target infrastructure family for OPL products and domain
Agents. This document separates functional/structural gaps from later live
evidence; it does not claim implementation readiness.

## Functional And Structural Gaps

| Capability | Target boundary | Gap to close |
| --- | --- | --- |
| Workspace workbench | Continuous App/Workspace project, task, artifact and review model | Close one explicit shared workbench contract |
| Console governance | Account policy, roles, approval, quota, billing and managed lifecycle | Keep policy distinct from package, service and resource mutation |
| Fabric resources | Compute, storage, connector and environment availability/binding | Close one shared plan/execute/collect envelope |
| Package projection | OPL Packages is sole manifest/digest/install/lock/update/rollback/repair owner | Make every Cloud surface consume exact refs without a registry copy |
| Service publication | Serve owns Agent Service, immutable Revision, Deployment, endpoint and traffic state | Add a dedicated service control plane above exact package refs |
| External Agent Edge | Public auth, consumer identity, validation, rate limits, quota, SSE and signed Webhooks | Do not expose Workspace, sandbox or provider endpoints directly |
| Execution providers | Runway owns Invocation/Session lifecycle across native and external adapters | Close one provider-neutral lifecycle and failure contract |
| Hosted UI | Task, Report, Workflow and Chat clients consume the same public Serve API | Keep templates out of runtime truth and avoid a generic site builder |
| Connector access | Connect owns shared access; domain adapters own domain semantics | Remove false shared-connector/currentness claims |
| Evidence | Ledger records receipt/provenance/review/continuation refs | Keep refs connected to owner sources without copying truth |
| Usage and quota | Console groups managed usage by account/workspace/service/revision/invocation/resource | Distinguish commercial service plans from OPL Packages |

## Evidence Gaps

- real App-to-Workspace continuation;
- real Cloud-hosted and institution-owned resource paths;
- sensitive-data egress policy and owner acceptance;
- provider and connector runtime evidence;
- public service authentication, isolation, rate-limit and abuse-control evidence;
- exact revision deployment, health, traffic promotion and rollback proof;
- Invocation/Session streaming, cancellation, deletion and retention proof;
- API, Embed and Hosted UI equivalence against the same public contract;
- managed billing and quota readback;
- exact user-visible receipts and continuation;
- production soak and release evidence.

Docs, contracts, generated projections and focused tests can close structural
gaps but cannot substitute for these evidence lanes.

## Package No-Second-Truth Rule

Cloud does not create an Agent Registry. OPL Packages provides the validated
manifest, digest, dependency closure, installed state, lock and lifecycle
receipt. Console projects account availability, Fabric projects resource
binding, Workspace projects user state/actions, Serve projects immutable service
revisions and Ledger stores refs. Every package projection must route mutation
back to OPL Packages.

## Suggested Planning Order

1. Shared App/Workspace workbench contract.
2. Package projection and owner routing.
3. Account and approval model.
4. Resource plan/execution contract.
5. Portable Service Entrypoint Contract.
6. Serve Service/Revision/Deployment control-plane contract.
7. Agent Edge and Runway provider lifecycle.
8. API-only private-beta path with exact receipts and rollback.
9. Hosted UI and Embed projections through the same API.
10. Service usage, quota and billing model.
11. Live provider, resource, security and user-path evidence.
