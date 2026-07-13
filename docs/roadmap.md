# OPL Cloud Roadmap

This roadmap describes the target delivery order for the Cloud implementation
family. It does not freeze current availability. Fresh service, contract,
runtime and owner evidence is required for any delivered or ready claim.

## Stable Design Decisions

- OPL App and OPL Workspace are continuous local/cloud workbenches.
- Console owns organization policy, approval, quota, billing and managed
  Workspace lifecycle.
- Fabric owns resource discovery/binding and execution adapters.
- Ledger owns receipt and provenance refs, not source or domain truth.
- OPL Packages is the only Agent/capability package lifecycle, manifest, digest
  and lock authority. Cloud does not create an Agent Registry.
- Domain Agents own professional strategy, quality verdict and delivery.

## Functional And Structural Gaps

| Gap | Target outcome | Owner route |
| --- | --- | --- |
| Workspace continuity | The same project/task/artifact model works locally and online | App + Workspace implementation owners |
| Managed-resource policy | Organization approval and quotas are clear without routing every action through Console | Console |
| Resource binding | Compute, storage, environments and connectors use one plan/execute/collect contract | Fabric |
| Package projection | Console, Fabric and Workspace consume one OPL Package manifest/lock ref without copying truth | Framework OPL Packages + each consumer |
| Connector boundary | Shared access stays generic; domain-specific adapters keep domain semantics | Connect + domain owner |
| Evidence continuation | Runs return refs, review status and continuation without centralizing source data | Ledger + workbench |

## Suggested Delivery Order

1. Close the App/Workspace contract and one managed Workspace path.
2. Close one resource execution path with explicit plan, approval, collection
   and receipt.
3. Project OPL Package status/actions into Cloud surfaces without a registry
   copy or Cloud writer.
4. Add Console organization policy for exact package and resource refs.
5. Add connector, SSH/HPC, GPU and storage adapters only after the common
   resource contract is stable.
6. Add exact Ledger/public readback for user-visible actions and artifacts.

## Evidence Lane

Functional/structural completion and live evidence are separate. Contracts,
docs, generated surfaces and tests can close structural gaps. Provider soak,
real institutional data boundaries, managed-resource billing, real user paths
and owner acceptance remain later evidence lanes and cannot be inferred from
the structural work.

## Explicit Non-Goals

- Do not rebuild package discovery, installation, lock or repair in Cloud.
- Do not use Console policy approval as package-byte or domain-quality approval.
- Do not use Fabric binding success as Agent readiness.
- Do not use Ledger receipt presence as source truth or professional verdict.
- Do not keep target connector examples as false current availability claims.
