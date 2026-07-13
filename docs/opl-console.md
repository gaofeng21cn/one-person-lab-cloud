# OPL Console

OPL Console is the planned organization governance surface for OPL Cloud. It
manages accounts, organizations, roles, quotas, budgets, approvals, managed
Workspace lifecycle and policy for Cloud-hosted or organization-managed
resources.

Console is not the package manager, resource executor, connector runtime,
Ledger truth or domain authority.

## Governance Objects

| Object | Purpose |
| --- | --- |
| Organization | Account, billing, policy and resource boundary |
| Team | Users sharing workspaces, quotas and approvals |
| Role | Permission bundle for use, approval, administration or audit |
| Workspace policy | Who can create, access, suspend, delete and retain workspaces |
| Resource policy | Which compute, storage, connector and environment refs are permitted |
| Package availability policy | Which exact OPL Package refs may be used by a team or workspace |
| Approval policy | Which actions need approval and who can approve them |
| Audit policy | Which actions require receipts and retention |

## Approval Targets

Console may approve:

- Workspace creation and lifecycle actions;
- connector credentials and organization-managed access;
- environment, compute and storage use;
- availability of an exact OPL Package manifest/lock ref;
- Agent Instance creation under organization policy;
- budget, quota, retention and reviewer-gate policy.

Package approval is a policy decision only. Install, version resolution, digest
verification, lock, update, rollback and repair remain OPL Packages actions.

## Metering And Billing Boundary

Console can meter Gateway provider usage, managed Workspace plans, Cloud-hosted
compute and storage, and organization-managed connector usage. User-provided
local, SSH or HPC resources can still produce Fabric and Ledger receipts without
becoming Cloud-billed resources by default.

Service plans and billing tiers must not be called package lifecycle records.
An OPL Package ref may be attached to usage for attribution, but Console cannot
change its manifest, digest or installed state.

## Product Boundary

Ordinary users work in App or Workspace. Administrators use Console to decide
who may use which managed capability and under what budget or policy. Fabric
executes approved resource bindings. OPL Packages performs package mutations.
Ledger records refs. Domain owners retain professional quality and delivery
authority.
