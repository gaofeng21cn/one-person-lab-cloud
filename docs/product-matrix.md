# OPL Cloud Product Matrix

OPL Cloud is the umbrella brand for One Person Lab cloud infrastructure.

| Layer | Product | Primary user value |
| --- | --- | --- |
| AI capability | OPL Gateway | One managed entry point for frontier AI access, tokens, providers, and usage metering |
| Management | OPL Console | One cloud console for account, organization, workspace, billing, permission, and operations management |
| Workspace | OPL Workspace | One managed online OPL App environment per user or task, with isolated access and resource configuration |
| Evidence | Provenance and reviewer gate | Reproducible artifact lineage, reviewer receipts, and policy checks for research and agent workflows |

## Relationship To OPL App

OPL App remains the user-facing local-first workspace. It can consume Cloud
capabilities, but it should not become a cloud dashboard.

OPL Cloud owns remote execution, billing, tenant policy, gateway access, job
coordination, and evidence services. OPL App should display Cloud-backed refs
and receipts without becoming their source of truth.

## Relationship To OPL Gateway

OPL Gateway is the first available component in the Cloud matrix. It should be
presented as the AI capability foundation of OPL Cloud, even if its public
integration scripts currently live outside a dedicated public implementation
repository.

