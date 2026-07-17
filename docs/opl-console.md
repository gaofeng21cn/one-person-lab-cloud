# OPL Console

Owner: `one-person-lab-cloud`
Purpose: `console_target_reference`
State: `active_target_reference`
Machine boundary: Human-readable target product definition. It does not prove
a Console implementation, account state, policy decision, metering, billing,
release, or production readiness.

OPL Console is the planned account and managed-resource governance surface for
OPL Cloud. It manages user accounts, quotas, budgets, approvals, the account's
single Workspace lifecycle, and policy for Cloud-hosted or explicitly managed
resources and Agent Services.

Console is not the package manager, Serve service-state owner, resource
executor, connector runtime, Ledger truth, or domain authority.

## Governance Objects

| Object | Purpose |
| --- | --- |
| Account | Identity, billing, policy, and resource boundary |
| Collaboration policy | Optional sharing of refs, artifacts, and approved resources without sharing Workspace ownership |
| Role | Permission bundle for collaboration, approval, administration, or audit |
| Workspace policy | How the account's single Workspace is provisioned, accessed, suspended, deleted, and retained |
| Resource policy | Which compute, storage, connector, and environment refs are permitted |
| Package availability policy | Which exact OPL Package refs may be used by the account Workspace |
| Service policy | Which exact revisions may be published and under which consumer, data, quota, and retention policy |
| Service plan | How managed usage is attributed and billed; not an OPL Package |
| Approval policy | Which actions need approval and who can approve them |
| Audit policy | Which actions require receipts and retention |

## Approval Targets

Console may approve:

- initial Workspace provisioning and lifecycle actions;
- connector credentials and explicitly managed access;
- environment, compute, and storage use;
- availability of an exact OPL Package manifest/lock ref;
- Agent Instance creation under account policy;
- creation, deployment, traffic promotion, pause, rollback, custom domain, and
  retention actions for an exact Agent Revision through Serve;
- budget, quota, retention, and reviewer-gate policy.

Package approval is a policy decision only. Install, version resolution, digest
verification, lock, update, rollback, and repair remain OPL Packages actions.

## Service Plan Model

Billing plans are deliberately not called packages:

| Area | User-facing plan | Metered breakdown |
| --- | --- | --- |
| Gateway | AI usage plan | provider, model, tokens, requests |
| Workspace | Workspace service plan | instance, uptime, storage allocation |
| Serve | Agent Service plan | service, revision, deployment, endpoint, invocation/session |
| Compute | Standard or accelerated compute plan | adapter, duration, GPU flag |
| Storage | Workspace or private storage plan | allocation, retention, transfer signal |
| Connectors | Managed connector access | actions and policy events |
| Agents | Agent-run usage | exact package/revision ref, run, resource, and reviewer gate |

The first Serve commercial boundary bills the publisher account. Marketplace,
merchant-of-record, tax, refund, KYC, revenue sharing, and end-customer
subscription behavior are separate later product decisions.

## Metering And Billing Boundary

Console can meter Gateway provider usage, the managed Workspace plan, Serve
endpoint and invocation/session usage, Cloud-hosted compute and storage, and
explicitly managed connector usage. User-provided local, SSH, or HPC resources
can still produce Fabric and Ledger refs without becoming Cloud-billed by
default.

Attaching an exact package or revision ref to usage is attribution only. Console
cannot change its manifest, digest, lock, installed state, service runtime
state, or domain readiness.

## Product Boundary

Ordinary users work in App or Workspace. Administrators use Console to decide
who may use or publish which managed capability and under what budget or policy.
Serve performs Agent Service lifecycle actions, Runway owns Invocation/Session
execution, Fabric executes approved resource bindings, OPL Packages performs
package mutations, and Ledger records refs. Domain owners retain professional
quality and delivery authority.
