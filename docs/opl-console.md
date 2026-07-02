# OPL Console

OPL Console is the planned management console for OPL Cloud.

It will manage accounts, organizations, billing, usage, permissions,
workspaces, and operational status for OPL Cloud-hosted or
organization-managed resources.

Console is a governance surface, not the only entry point for Fabric or Ledger.
Local OPL App and cloud OPL Workspace can use reusable Fabric and Ledger
capabilities directly. Console becomes active when a resource, connector,
workspace, receipt policy, quota, or credential is hosted by OPL Cloud or
managed by an organization.

## Initial Scope

- Account and organization management.
- Gateway usage and billing visibility.
- Workspace creation, configuration, suspension, and deletion.
- Resource package selection for Gateway, Fabric, and Workspace usage.
- Agent Package approval, version policy, and workspace access policy.
- Connector approval through OPL Fabric.
- Environment policy through OPL Fabric.
- Ledger policy for receipts, review gates, audit, and retention.
- Operational receipts for user-visible actions.

## Governance Model

Console should make these general governance objects explicit:

| Object | Purpose |
| --- | --- |
| Organization | Top-level account, billing, policy, and resource boundary |
| Team | Group of users sharing workspaces, packages, quotas, and approvals |
| User | Person or service identity using OPL Cloud |
| Role | Permission bundle for using, approving, administering, or auditing |
| Workspace policy | Who can create, access, suspend, delete, and retain workspaces |
| Resource policy | Which compute, storage, connectors, environments, and agents are available |
| Approval policy | Which actions need approval and who can approve them |
| Audit policy | Which user-visible actions require receipts and retention |

## Approval Targets

Console should support approval for:

- Workspace creation and package changes.
- Connector enablement.
- Environment template use.
- Managed compute or storage use.
- Agent package versions and Agent Instance creation.
- Budget or quota changes.
- Receipt retention and reviewer-gate policy.

## Metering And Billing Boundary

Console should meter and bill managed Cloud usage first:

- Gateway provider usage.
- Workspace package usage.
- OPL Cloud-hosted compute and storage.
- Organization-managed connectors, environments, and quotas.

User-provided local, SSH, or HPC resources can still produce Fabric and Ledger
receipts. They enter Console billing only when the organization chooses to
manage them through OPL Cloud policy.

## Product Boundary

OPL Console is the management surface. It should expose product choices and
policy controls, not raw infrastructure internals by default.

Users should see Workspace and task status in OPL Workspace. Administrators
should use Console for organization, billing, resource, connector, environment,
and policy management.

Console does not need to manage every Fabric resource. Local resources and
user-provided SSH or HPC resources can use the standard plan, approve, execute,
collect, and receipt flow from OPL App or OPL Workspace. Console becomes the
management and billing surface when those resources are hosted by OPL Cloud,
approved by an organization, or governed by team policy.

The same rule applies to connectors. A local App can use a user-configured
PubMed connector through OPL Connect without becoming Console-managed usage. A
team-approved literature source, institution credential, paid database, or
audited connector policy enters Console governance.

For agents, Console approves which Agent Packages are available to a team, which
versions can be instantiated, and which quotas, connectors, environments, and
workspaces they may use. It does not replace OPL Meta Agent as the agent-building
surface or Workspace as the user workbench.
