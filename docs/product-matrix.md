# OPL Cloud Product Matrix

OPL Cloud is the umbrella brand for One Person Lab cloud infrastructure.

| Layer | Brand | Primary user value | Surface |
| --- | --- | --- | --- |
| AI access | OPL Gateway | One managed entry point for frontier AI access, model routing, keys, provider policy, and usage metering | Product |
| User workbench | OPL App / OPL Workspace | Local and cloud Docker/WebUI OPL App surfaces for project work, task sessions, artifact preview, and result delivery | Product |
| Management | OPL Console | One cloud console for accounts, organizations, billing, permissions, workspaces, connectors, and resource policy | Product |
| Resource substrate | OPL Fabric | One resource layer for compute, storage, software environments, connectors, agent packages, and execution adapters | Platform |
| Evidence record | OPL Ledger | One evidence layer for job receipts, artifact provenance, reviewer gates, audit records, and continuation refs | Platform |

## Product Surfaces

The first three surfaces are product-facing:

- OPL Gateway: the AI capability and usage entry point.
- OPL App / OPL Workspace: equivalent user workbench surfaces.
- OPL Console: the management surface.

The next two surfaces are platform capabilities:

- OPL Fabric: resource and connector substrate.
- OPL Ledger: evidence and receipt record.

## Gateway And Fabric

Gateway is technically a resource access capability, but it remains top-level
because users can directly configure it, use it, meter it, and pay for it.
Fabric keeps the resource substrate behind OPL App, OPL Workspace, and Console:
compute, storage, environments, connectors, agent registry, and execution
adapters.

## Relationship To OPL Meta Agent

OPL Meta Agent can create an Agent Blueprint and Agent Package candidate. OPL
Cloud carries the package through approval, registry, instantiation, execution,
and receipt:

```text
Agent Blueprint → Agent Package → OPL Agent Registry → Agent Instance → Agent Run
```

Console approves package versions and access policy. Fabric prepares resources.
OPL App or OPL Workspace is where users run the Agent Instance. Ledger records
the Agent Run.

## Relationship To OPL App

OPL App is the local workbench surface. OPL Workspace is the cloud Docker/WebUI
surface for the same OPL App experience. They should share the same task,
artifact, resource-plan, approval, execution, collection, and receipt model.

OPL Cloud should extend both surfaces with Gateway, Fabric, and Ledger
capabilities while keeping source-of-truth claims with the owning services,
repositories, contracts, runtime outputs, and receipts.

Console management starts when a resource is OPL Cloud-hosted or
organization-managed: billing, quotas, permissions, workspace lifecycle,
connector approvals, environment policy, and audit policy belong there.
User-provided local, SSH, or HPC resources can still use the standard Fabric
flow without becoming Console-billed resources by default.
