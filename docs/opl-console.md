# OPL Console

OPL Console is the planned management console for OPL Cloud.

It will manage accounts, organizations, billing, usage, permissions,
workspaces, and operational status across OPL Cloud products.

## Initial Scope

- Account and organization management.
- Gateway usage and billing visibility.
- Workspace creation, configuration, suspension, and deletion.
- Resource package selection for Gateway, Fabric, and Workspace usage.
- Connector approval through OPL Fabric.
- Environment policy through OPL Fabric.
- Ledger policy for receipts, review gates, audit, and retention.
- Operational receipts for user-visible actions.

## Product Boundary

OPL Console is the management surface. It should expose product choices and
policy controls, not raw infrastructure internals by default.

Users should see Workspace and task status in OPL Workspace. Administrators
should use Console for organization, billing, resource, connector, environment,
and policy management.
