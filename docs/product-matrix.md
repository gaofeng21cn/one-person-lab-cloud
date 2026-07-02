# OPL Cloud Product Matrix

OPL Cloud is the umbrella brand for One Person Lab cloud infrastructure.
Its user-visible Cloud products are OPL Gateway, OPL Workspace, and
OPL Console. OPL App is the local workbench that can consume the same Cloud
platform capabilities without making Console the only entry point.

| Layer | Brand | Primary user value | Surface |
| --- | --- | --- | --- |
| AI access | OPL Gateway | One managed entry point for frontier AI access, model routing, keys, provider policy, and usage metering | Product |
| User workbench | OPL Workspace / OPL App integration | Cloud OPL App and local OPL App share project work, task sessions, artifact preview, and result delivery | Product / local entry |
| Management | OPL Console | One cloud console for accounts, organizations, billing, permissions, workspaces, connectors, and resource policy | Product |
| Resource substrate | OPL Fabric | One resource layer for Connect, Compute, Storage, Environments, Gateway/App/Workspace adapters, agent packages, and execution adapters | Platform |
| Evidence record | OPL Ledger | One evidence layer for job receipts, artifact provenance, reviewer gates, audit records, and continuation refs | Platform |

## Product Surfaces

The Cloud product-facing surfaces are:

- OPL Gateway: the AI capability and usage entry point.
- OPL Workspace: the cloud OPL App surface.
- OPL Console: the management surface.

OPL App remains the local workbench surface and a first-class consumer of Cloud
platform capabilities.

The next two surfaces are platform capabilities:

- OPL Fabric: resource and connector substrate.
- OPL Ledger: evidence and receipt record.

Fabric and Ledger can be used by more than one product surface. OPL Console
governs hosted or organization-managed usage, while OPL App, OPL Workspace, MAS,
and other domain systems can call reusable platform capabilities directly when
their capability profile allows it.

## Gateway And Fabric

Gateway is technically a resource access capability, but it remains top-level
because users can directly configure it, use it, meter it, and pay for it.
Fabric keeps the resource substrate behind OPL App, OPL Workspace, and Console:
OPL Connect, OPL Compute, Storage, OPL Environments, Gateway/App/Workspace
adapters, agent registry, and execution adapters.

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

OPL App is the local workbench surface. OPL Workspace is the cloud OPL App
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

The same pattern applies to connectors. PubMed access, databases, tool
libraries, resource catalogs, or large skill packs can start as a MAS,
ScholarSkills, or other domain skill prototype, then move into OPL Connect when
they become stable shared capabilities. Console manages them only when the
connector is organization-approved, quota-controlled, audited, or billed.
