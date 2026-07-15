# OPL Console

OPL Console is the planned account and managed-resource governance surface for
OPL Cloud. It manages user accounts, quotas, budgets, approvals, the account's
single Workspace lifecycle, and policy for Cloud-hosted or explicitly managed
resources and Agent Services.

Console is not the package manager, Serve service-state owner, resource executor,
connector runtime, Ledger truth or domain authority.

## Governance Objects

| Object | Purpose |
| --- | --- |
| Account | Identity, billing, policy and resource boundary |
| Collaboration policy | Optional sharing of refs, artifacts and approved resources without sharing Workspace ownership |
| Role | Permission bundle for collaboration, approval, administration or audit |
| Workspace policy | How the account's single Workspace is provisioned, accessed, suspended, deleted and retained |
| Resource policy | Which compute, storage, connector and environment refs are permitted |
| Package availability policy | Which exact OPL Package refs may be used by the account Workspace |
| Service policy | Which exact package revisions may be published and under which consumer, data, quota and retention policy |
| Service plan | How Serve endpoint, invocation/session, provider, compute, storage and managed connector usage are attributed |
| Approval policy | Which actions need approval and who can approve them |
| Audit policy | Which actions require receipts and retention |

## Approval Targets

Console may approve:

- initial Workspace provisioning and lifecycle actions;
- connector credentials and explicitly managed access;
- environment, compute and storage use;
- availability of an exact OPL Package manifest/lock ref;
- Agent Instance creation under account policy;
- creation, deployment, traffic promotion, pause, rollback, custom domain and
  retention actions for an exact Agent Revision through Serve;
- budget, quota, retention and reviewer-gate policy.

Package approval is a policy decision only. Install, version resolution, digest
verification, lock, update, rollback and repair remain OPL Packages actions.

## Metering And Billing Boundary

Console can meter Gateway provider usage, the managed Workspace plan,
Serve endpoint and invocation/session usage, Cloud-hosted compute and storage,
and explicitly managed connector usage. User-provided local, SSH or HPC
resources can still produce Fabric and Ledger receipts without becoming
Cloud-billed resources by default.

Service plans and billing tiers must not be called package lifecycle records.
An OPL Package ref may be attached to usage for attribution, but Console cannot
change its manifest, digest or installed state.

## Product Boundary

Ordinary users work in App or Workspace. Administrators use Console to decide
who may use or publish which managed capability and under what budget or policy.
Serve performs Agent Service lifecycle actions, Runway owns Invocation/Session
execution, and Fabric executes approved resource bindings. OPL Packages performs
package mutations. Ledger records refs. Domain owners retain professional
quality and delivery authority.
