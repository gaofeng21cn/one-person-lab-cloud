# Platform Capability Gaps

OPL Cloud is the general infrastructure substrate for OPL products and
industry-specific platforms.

For downstream products to build on it safely, OPL Cloud needs to make several
general capabilities clearer. These are platform capabilities, not
industry-specific workflows.

## Capability Gap Map

| Capability | Current state | Needs to be clearer | Suggested next planning artifact |
| --- | --- | --- | --- |
| Workspace product flow | Basic role and lifecycle are defined | What a Workspace contains, how users enter it, how projects/tasks/artifacts/receipts appear | Workspace product flow |
| Console governance | Scope and billing boundary are defined | Organization model, roles, approval targets, quota policy, audit policy | Organization and approval model |
| Resource catalog | Fabric components are named | How compute, storage, connectors, environments, and agent registry are presented as product choices | Resource catalog model |
| Literature connector baseline | PubMed is named as the first stable OPL Connect path | How MAS, App, Workspace, Connect, Console, and Ledger share one literature access flow | [OPL Connect](opl-connect.md) |
| Connector governance | Connect examples exist | Connector lifecycle, approval, ownership, credentials, scope, audit | Connector lifecycle model |
| Environment catalog | Environment role is named | Environment templates, image/package/version ownership, compatibility with agents and jobs | Environment catalog model |
| Evidence records | Ledger shape is sketched | Human-readable receipt views, retention, review status, continuation, artifact refs | Evidence record model |
| Agent deployment | Agent lifecycle is sketched | Package intake, approval, resource binding, instance creation, run records | Agent deployment model |
| Usage and quota | Gateway/Console boundaries are named | Usage grouping across org, workspace, resource, connector, agent, and task | Usage and quota model |

## Why These Gaps Matter

Industry products can supply their own domain knowledge, rules, tools, review
criteria, and deployment model. OPL Cloud should supply the reusable platform
primitives:

- workspaces;
- organizations and approvals;
- AI access and usage;
- compute, storage, connectors, and environments;
- agent package deployment;
- receipts, provenance, and continuation records.

When these primitives are clear, a downstream product can describe how it uses
them without changing Cloud's core model.

## Suggested Planning Order

1. Workspace product flow.
2. Organization and approval model.
3. Resource catalog model.
4. Literature connector baseline.
5. Evidence record model.
6. Agent deployment model.
7. Usage and quota model.
8. Connector lifecycle model.
9. Environment catalog model.

This order starts from what users and administrators need to understand, then
moves into platform internals.
