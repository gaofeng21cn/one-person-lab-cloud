# Console Governance And Billing

OPL Console is the management surface for OPL Cloud-hosted and explicitly
managed account usage.

## Governance Scope

Console manages user accounts, the account's single Workspace lifecycle,
Gateway usage policy, Serve publication and consumer policy,
compute/storage/connector/environment policy, quotas, budgets, approvals,
optional collaboration policy, audit policy and Ledger retention policy.

For Agents and capability packages, Console stores only account availability and
service policy referencing exact OPL Package and Agent Revision refs. It does not
install, update, roll back, repair or write package state, and it does not own
Serve Deployment or Runway Invocation/Session truth.

## Service Plan Model

Billing plans are intentionally not called packages:

| Area | User-facing plan | Metered breakdown |
| --- | --- | --- |
| Gateway | AI usage plan | provider, model, tokens, requests |
| Workspace | Workspace service plan | instance, uptime, storage allocation |
| Serve | Agent Service plan | service, revision, deployment, endpoint, invocation/session |
| Compute | Standard or accelerated compute plan | adapter, duration, GPU flag |
| Storage | Workspace or private storage plan | allocation, retention, transfer signal |
| Connectors | Managed connector access | actions and policy events |
| Agents | Agent-run usage | OPL Package ref, Agent Instance or Service Revision, run, reviewer gate |

## Billing Boundary

Billing applies first to Gateway usage, the Cloud-hosted Workspace service plan,
Serve endpoint and invocation/session usage, managed compute/storage and
explicitly managed connectors. User-provided local, SSH or HPC resources can
produce usage and receipt refs without becoming Cloud-billed by default.

The first Serve commercial boundary bills the publisher account. Marketplace,
merchant-of-record, tax, refund, KYC, revenue-sharing and end-customer
subscription behavior are separate later decisions.

Attaching an OPL Package ref to usage is attribution only; it grants no package
lifecycle authority and no domain readiness claim.
