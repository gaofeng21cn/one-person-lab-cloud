# OPL Workspace

OPL Workspace is the cloud OPL App workbench. It should feel like the same
project, task, artifact, review and delivery experience in an online deployment,
not like a container hosting panel.

## User Model

| Term | Meaning |
| --- | --- |
| OPL Workspace | The user-visible cloud workbench product |
| Workspace Instance | The one provisioned workbench runtime owned by one user account |
| Workspace Storage | Project files, volumes, buckets, outputs and delivery space attached to an instance |

One user account owns exactly one primary OPL Workspace. Projects and
collaboration remain inside or around that Workspace and do not create
additional Workspace instances.

## Workspace Product Flow

```text
open account workspace
-> select resource profile and permitted OPL Package refs
-> provision access, storage and runtime
-> open project workbench
-> run tasks or Agent Instances
-> optionally publish an exact Agent Package revision to OPL Serve
-> inspect artifacts, reviews and receipts
-> suspend, resume or delete according to policy
```

## Workspace Contents

A Workspace can show projects, task sessions, files, storage, Agent Instances,
job status, resource use, artifacts, review status, Ledger refs and
continuation entries. Package state and actions are Framework projections from
OPL Packages; Workspace does not infer or store a second installed-version truth.

Workspace may expose a **Publish to OPL Serve** action after the package,
entrypoint, policy and owner gates are satisfied. The action calls Serve owner
surfaces. Workspace does not create canonical Service, Revision, Deployment,
endpoint or traffic state.

## Responsibility Boundary

- OPL Packages owns install, update, rollback, repair, manifest, digest and lock.
- Console owns account availability, quota and the single managed Workspace policy.
- Serve owns Agent Service publication, immutable revisions and external endpoints.
- Fabric binds and runs compute, storage, environment and connector resources.
- Gateway supplies AI access.
- Ledger records receipt and provenance refs.
- Workspace presents the user experience and dispatches owner actions.

A Workspace URL is a workbench access surface and cannot be reused as an Agent
Service endpoint. Multiple Agent Services published from one account remain
deployment resources and do not create additional Workspace instances.

Package availability, resource availability and domain readiness are different
states. Workspace must display their owner and next action rather than collapse
them into a single ready flag.
