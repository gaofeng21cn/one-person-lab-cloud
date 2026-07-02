# OPL Workspace

OPL Workspace is the cloud OPL App workbench.

It should feel equivalent to OPL App in product role: a user workbench for
projects, tasks, progress, artifacts, review, and delivery. The difference is
deployment form. Workspace runs the OPL App experience online and can provide an
isolated access URL, account, password, storage directory, and optional resource
package.

## User Model

The product should feel closer to OPL App online than to a container hosting
panel. Users choose the workspace they need; the underlying Docker runtime,
compute node, and storage location remain implementation details.

Use three terms consistently:

| Term | Meaning |
| --- | --- |
| OPL Workspace | The user-visible cloud workbench product. |
| Workspace Instance | One provisioned online OPL App instance with URL, account, runtime, and lifecycle. |
| Workspace Storage | The persistent project files, volumes, buckets, output locations, and delivery space attached to an instance. |

## Workspace Product Flow

The general Workspace flow should be:

```text
request workspace
-> select package and resource profile
-> provision URL, account, storage, and base runtime
-> open project workbench
-> run tasks or agent instances
-> inspect artifacts, status, reviews, and receipts
-> suspend, resume, or delete according to policy
```

## Workspace Contents

A Workspace should be able to show:

- projects and task sessions;
- files and workspace storage;
- approved agent instances;
- job status and resource use;
- artifacts and delivery outputs;
- review status and Ledger receipts;
- continuation entries for resumed work.

## Initial Capability

- Create a managed OPL App workspace.
- Select compute and storage configuration.
- View package-based billing with compute, storage, and AI usage breakdown.
- Open the workspace through an isolated URL.
- Reset or rotate workspace credentials.
- Start approved jobs through OPL Fabric.
- Create approved Agent Instances from OPL Agent Registry packages.
- Display OPL Ledger receipts and reviewer results.

## Boundary

OPL Workspace runs OPL App online. It should reuse the same task, artifact,
resource-plan, approval, execution, collection, and receipt model as local OPL
App.

OPL Console manages Workspace lifecycle when the workspace is OPL Cloud-hosted
or organization-managed. OPL Gateway provides AI capability access. OPL Fabric
runs approved resources. OPL Ledger records receipts and provenance. These
boundaries should stay visible in product docs and implementation contracts.

For agents, Workspace is the user surface for an Agent Instance. It should show
what the agent can do, what resources it will use, what is running, what was
produced, and what needs review. Package approval, resource policy, and
organization permissions remain in Console and Fabric.
