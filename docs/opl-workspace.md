# OPL Workspace

OPL Workspace is the cloud OPL App workbench. It should feel like the same
project, task, artifact, review and delivery experience in an online deployment,
not like a container hosting panel.

## User Model

| Term | Meaning |
| --- | --- |
| OPL Workspace | The user-visible cloud workbench product |
| Workspace Instance | One provisioned workbench runtime with access and lifecycle |
| Workspace Storage | Project files, volumes, buckets, outputs and delivery space attached to an instance |

## Workspace Product Flow

```text
request workspace
-> select resource profile and permitted OPL Package refs
-> provision access, storage and runtime
-> open project workbench
-> run tasks or Agent Instances
-> inspect artifacts, reviews and receipts
-> suspend, resume or delete according to policy
```

## Workspace Contents

A Workspace can show projects, task sessions, files, storage, Agent Instances,
job status, resource use, artifacts, review status, Ledger refs and
continuation entries. Package state and actions are Framework projections from
OPL Packages; Workspace does not infer or store a second installed-version truth.

## Responsibility Boundary

- OPL Packages owns install, update, rollback, repair, manifest, digest and lock.
- Console owns organization availability, quota and managed Workspace policy.
- Fabric binds and runs compute, storage, environment and connector resources.
- Gateway supplies AI access.
- Ledger records receipt and provenance refs.
- Workspace presents the user experience and dispatches owner actions.

Package availability, resource availability and domain readiness are different
states. Workspace must display their owner and next action rather than collapse
them into a single ready flag.
