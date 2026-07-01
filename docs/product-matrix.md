# OPL Cloud Product Matrix

OPL Cloud is the umbrella brand for One Person Lab cloud infrastructure.

| Layer | Brand | Primary user value | Surface |
| --- | --- | --- | --- |
| AI access | OPL Gateway | One managed entry point for frontier AI access, model routing, keys, provider policy, and usage metering | Product |
| User workbench | OPL Workspace | One managed online OPL App environment for project work, task sessions, artifact preview, and result delivery | Product |
| Management | OPL Console | One cloud console for accounts, organizations, billing, permissions, workspaces, connectors, and resource policy | Product |
| Resource substrate | OPL Fabric | One resource layer for compute, storage, software environments, connectors, and execution adapters | Platform |
| Evidence record | OPL Ledger | One evidence layer for job receipts, artifact provenance, reviewer gates, audit records, and continuation refs | Platform |

## Product Surfaces

The first three surfaces are product-facing:

- OPL Gateway: the AI capability and usage entry point.
- OPL Workspace: the user workbench.
- OPL Console: the management surface.

The next two surfaces are platform capabilities:

- OPL Fabric: resource and connector substrate.
- OPL Ledger: evidence and receipt record.

## Gateway And Fabric

Gateway is technically a resource access capability, but it remains top-level
because users can directly configure it, use it, meter it, and pay for it.
Fabric keeps the resource substrate behind Workspace and Console: compute,
storage, environments, connectors, and execution adapters.

## Relationship To OPL App

OPL App remains the local-first user workbench. OPL Workspace hosts the online
OPL App experience. OPL Cloud should extend App workflows with Gateway, Fabric,
and Ledger capabilities while keeping source-of-truth claims with the owning
services, repositories, contracts, runtime outputs, and receipts.

