# Roadmap

## Available

- OPL Gateway brand and public integration surface.
- OPL Cloud product and architecture entry point.

## First Cloud Slice

The first Cloud slice proves the App/Workspace equivalence and one managed
resource path:

- OPL Workspace: create one cloud Docker/WebUI OPL App surface with isolated
  URL, account, storage directory, and base package.
- OPL App cloud connection: let local OPL App use the same Gateway, Fabric, and
  Ledger contracts as Workspace.
- OPL Console: manage users, packages, Gateway usage, Workspace lifecycle, and
  managed-resource policy.
- OPL Fabric v0: support one shared App/Workspace resource flow with Docker or
  VM compute plus one volume or bucket storage path.
- OPL Ledger v0: generate receipt JSON for App actions, Workspace actions, and
  managed jobs.
- Agent lifecycle docs: define Agent Blueprint, Agent Package, OPL Agent
  Registry, Agent Instance, Agent Run, and Ledger receipt boundaries.

## In Development

- Workspace package and billing model.
- Console management surface.
- Gateway usage and package visibility.

## Planning Gaps To Close

| Gap | Decision | Planning artifact |
| --- | --- | --- |
| Platform capability clarity | Cloud must clarify reusable platform primitives before downstream products specialize them | [Platform Capability Gaps](platform-capability-gaps.md) |
| Shared App/Workspace execution contract | One plan, approve, execute, monitor, collect, receipt contract works from local App and cloud Workspace | [Shared Execution Contract](contracts/shared-execution-contract.md) |
| Managed-resource boundary | Console manages OPL Cloud-hosted or organization-managed resources; user-provided local, SSH, and HPC resources use Fabric without default Console billing | [Resource Ownership And Billing](contracts/resource-ownership-and-billing.md) |
| Workspace surface | Workspace is the cloud Docker/WebUI surface for OPL App with its own lifecycle | [Workspace Lifecycle](workspace-lifecycle.md) |
| Console metering and billing | Billing starts from Gateway usage, Workspace package, managed compute, managed storage, and managed connectors | [Console Governance And Billing](console-governance-and-billing.md) |
| Fabric adapter order | Start with Docker/VM and storage; add SSH/HPC after the shared contract works; add GPU and data connectors after job receipts stabilize | [Fabric Adapter Contract](fabric-adapter-contract.md) |
| Ledger receipt schema | Receipts bind plan, approval, command/code, environment, input refs, output refs, review result, owner, cost, and continuation | [Ledger Receipt Schema](contracts/ledger-receipt-schema.md) |
| Environment catalog | Environments stay inside Fabric while App and Workspace select approved templates | Environment manifest shape for runtime image, packages, locks, hardware needs, and owner |
| Agent lifecycle | OPL Meta Agent builds packages; Console approves; Fabric binds resources; App or Workspace exposes Agent Instances; Ledger records Agent Runs | [OPL Agent Lifecycle](agent-lifecycle.md) and [Agent Registry Entry](contracts/agent-registry-entry.md) |

## Milestones

### M0: Planning Baseline

- Align product docs on App/Workspace equivalence.
- Clarify Workspace product flow.
- Clarify Console organization and approval model.
- Clarify Fabric resource catalog.
- Clarify Ledger evidence record view.
- Define resource ownership and billing matrix.
- Define the shared execution contract.
- Define minimal Ledger receipt schema.
- Define Workspace lifecycle and Console scope.

### M1: Gateway + Workspace Baseline

- Use OPL Gateway as the first metered Cloud capability.
- Create one Workspace instance as cloud Docker/WebUI OPL App.
- Expose the same Gateway configuration path to local OPL App.
- Record basic usage events for Gateway and Workspace packages.

### M2: Fabric v0

- Run one managed Docker or VM job from App and Workspace through the shared
  plan, approve, execute, monitor, collect, receipt flow.
- Attach one storage path and collect output refs.
- Record a Ledger receipt for each managed job.

### M3: Console v0

- Manage users, packages, Workspace lifecycle, Gateway usage, and managed
  resource quotas.
- Show which resources are OPL Cloud-hosted, organization-managed, or
  user-provided.
- Keep user-provided SSH/HPC resources outside default Console billing while
  still supporting Fabric receipts.

### M4: Fabric Adapters

- Add SSH/HPC adapters after the shared execution contract is stable.
- Add GPU workers when job scheduling, environment selection, and cost capture
  are covered by receipts.
- Add literature, database, and institutional storage connectors after access
  policy and source refs are stable.

### M5: Agent Lifecycle

- Register approved OPL-compatible Agent Packages.
- Instantiate agents from OPL App or OPL Workspace.
- Bind each Agent Instance to approved resources, connectors, environments, and
  quotas.
- Record Agent Run receipts in OPL Ledger.

## Planned Fabric Adapters

- GPU workers.
- SSH and HPC adapters.
- Literature databases.
- Research databases.
- Institutional storage refs.
- Team-approved connector registry.
- Versioned software environment catalog.
- OPL Agent Registry for approved OPL-compatible Agent Packages.

## Planned Ledger Capabilities

- Artifact provenance.
- Reviewer gates for MAS, MAG, RCA, and BookForge.
- Policy decisions and audit records.
- Continuation refs for resumed work.
- Agent Run receipts for App and Workspace Agent Instances.

## First-Version Focus

- Keep OPL Workspace as the cloud Docker/WebUI surface for OPL App.
- Keep OPL Console focused on OPL Cloud-hosted or organization-managed
  resources, permissions, lifecycle, policy, and billing.
- Keep OPL Fabric as the shared resource layer for local App, cloud Workspace,
  user-provided SSH/HPC, and OPL Cloud-hosted resources.
- Keep OPL Environments inside Fabric until environment management becomes a
  user-visible team workflow.
