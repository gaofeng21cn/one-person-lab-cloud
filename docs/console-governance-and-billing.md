# Console Governance And Billing

OPL Console is the management surface for OPL Cloud-hosted and
organization-managed usage.

## Governance Scope

Console manages accounts, teams, roles, managed Workspace lifecycle, Gateway
usage policy, compute/storage/connector/environment policy, quotas, budgets,
approvals, audit policy and Ledger retention policy.

For Agents and capability packages, Console stores only an organization
availability policy referencing an exact OPL Package manifest/lock ref. It does
not install, update, roll back, repair or write package state.

## Service Plan Model

Billing plans are intentionally not called packages:

| Area | User-facing plan | Metered breakdown |
| --- | --- | --- |
| Gateway | AI usage plan | provider, model, tokens, requests |
| Workspace | Workspace service plan | instance, uptime, storage allocation |
| Compute | Standard or accelerated compute plan | adapter, duration, GPU flag |
| Storage | Workspace or private storage plan | allocation, retention, transfer signal |
| Connectors | Managed connector access | actions and policy events |
| Agents | Agent-run usage | OPL Package ref, instance, run, reviewer gate |

## Billing Boundary

Billing applies first to Gateway usage, Cloud-hosted Workspace service plans,
managed compute/storage and organization-managed connectors. User-provided
local, SSH or HPC resources can produce usage and receipt refs without becoming
Cloud-billed by default.

Attaching an OPL Package ref to usage is attribution only; it grants no package
lifecycle authority and no domain readiness claim.
