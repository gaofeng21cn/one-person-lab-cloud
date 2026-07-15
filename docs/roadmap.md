# OPL Cloud Roadmap

This roadmap describes the target delivery order for the Cloud implementation
family. It does not freeze current availability. Fresh service, contract,
runtime and owner evidence is required for any delivered or ready claim.

## Stable Design Decisions

- OPL App and OPL Workspace are continuous local/cloud workbenches.
- One user account owns one primary OPL Workspace; projects and collaboration
  do not create additional Workspace products.
- One account may publish multiple Agent Services; they are deployment resources,
  not additional Workspaces.
- OPL Serve owns Agent Service, immutable Revision, Deployment, endpoint and
  traffic state. API, Embed and Hosted UI use the same public contract.
- Console owns account policy, approval, quota, billing and the single managed
  Workspace lifecycle; it projects Serve policy without owning service runtime.
- Fabric owns resource discovery/binding and execution adapters.
- Ledger owns receipt and provenance refs, not source or domain truth.
- OPL Packages is the only Agent/capability package lifecycle, manifest, digest
  and lock authority. Cloud does not create an Agent Registry.
- Domain Agents own professional strategy, quality verdict and delivery.
- OPL Runway owns Invocation and Session execution lifecycle across approved
  native and external provider adapters.

## Functional And Structural Gaps

| Gap | Target outcome | Owner route |
| --- | --- | --- |
| Workspace continuity | The same project/task/artifact model works locally and online | App + Workspace implementation owners |
| Managed-resource policy | Account approval and quotas are clear without routing every action through Console | Console |
| Resource binding | Compute, storage, environments and connectors use one plan/execute/collect contract | Fabric |
| Package projection | Console, Fabric and Workspace consume one OPL Package manifest/lock ref without copying truth | Framework OPL Packages + each consumer |
| Service publication | An exact package digest becomes a stable Service, immutable Revision and controlled Deployment | OPL Serve + OPL Packages refs |
| Public service edge | External consumers receive authenticated, rate-limited API, event and Webhook surfaces | OPL Serve Agent Edge + Console policy |
| Provider lifecycle | Native Runway/Fabric and approved external providers share one Invocation/Session contract | OPL Runway + Fabric/provider adapters |
| Hosted UI | Task, Report, Workflow and Chat templates use the same API as publisher Apps | OPL Serve |
| Connector boundary | Shared access stays generic; domain-specific adapters keep domain semantics | Connect + domain owner |
| Evidence continuation | Runs and external invocations return refs, review status and continuation without centralizing source data | Ledger + workbench/Serve |

## Suggested Delivery Order

1. Close the App/Workspace contract and one managed Workspace path.
2. Close one resource execution path with explicit plan, approval, collection
   and receipt.
3. Project OPL Package status/actions into Cloud surfaces without a registry
   copy or Cloud writer.
4. Add Console account policy for exact package and resource refs.
5. Add connector, SSH/HPC, GPU and storage adapters only after the common
   resource contract is stable.
6. Add exact Ledger/public readback for user-visible actions and artifacts.
7. Close a portable Service Entrypoint Contract and immutable Agent Revision.
8. Add a dedicated Agent Edge and an API-only private-beta path with exact
   authentication, quota, cancellation, receipt and rollback behavior.
9. Add the Runway execution-provider port with one OPL-native adapter and only
   then an approved external managed-Agent adapter.
10. Add Task and Report Hosted UI templates as ordinary public API clients.
11. Add stateful Sessions, Chat, Embed, custom domains and broader service plans
    only after the API path has live security, isolation, billing and soak proof.

## Evidence Lane

Functional/structural completion and live evidence are separate. Contracts,
docs, generated surfaces and tests can close structural gaps. Provider soak,
real institutional data boundaries, external-consumer isolation, endpoint
security, traffic rollback, managed billing, real user paths and owner acceptance
remain later evidence lanes and cannot be inferred from the structural work.

## Explicit Non-Goals

- Do not rebuild package discovery, installation, lock or repair in Cloud.
- Do not create a multi-tenant SaaS workbench or multiple OPL Workspaces per user.
- Do not reuse a Workspace URL or expose a sandbox/provider session as a public
  Agent Service endpoint.
- Do not create separate execution behavior for Hosted UI and Embed clients.
- Do not expand the first Serve release into a generic website builder,
  marketplace, merchant-of-record or end-customer subscription platform.
- Do not treat external `RenDeHuang/OPL-Webui` or `RenDeHuang/MedOPL` as an OPL Cloud implementation or maintenance target.
- Do not use Console policy approval as package-byte or domain-quality approval.
- Do not use Fabric binding success as Agent readiness.
- Do not use Ledger receipt presence as source truth or professional verdict.
- Do not keep target connector examples as false current availability claims.
