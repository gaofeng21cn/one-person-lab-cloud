# Console Governance And Billing

OPL Console is the management surface for OPL Cloud-hosted and
organization-managed usage.

## Governance Scope

Console manages:

- accounts, teams, and organizations;
- Workspace lifecycle and packages;
- Gateway usage and provider policy;
- managed compute and storage packages;
- connector approvals;
- environment approvals;
- agent package approvals;
- quotas and budget controls;
- Ledger receipt retention and reviewer-gate policy.

## Package Model

The first package model can stay simple:

| Package area | User-facing package | Metered breakdown |
| --- | --- | --- |
| Gateway | AI usage package | provider, model, tokens, requests |
| Workspace | Workspace package | instance, uptime, storage allocation |
| Compute | Standard or accelerated compute | adapter, duration, GPU flag |
| Storage | Workspace or private storage | allocation, retention, transfer signal |
| Connectors | Approved connector access | connector actions and policy events |
| Agents | Approved agent runs | package, instance, run, reviewer gate |

## Policy Objects

- `workspace_policy`
- `gateway_policy`
- `compute_policy`
- `storage_policy`
- `connector_policy`
- `environment_policy`
- `agent_policy`
- `ledger_policy`
- `budget_policy`

## Billing Boundary

Console can report all activity that flows through OPL Cloud contracts. Billing
applies first to Gateway usage, OPL Cloud-hosted Workspace packages, managed
compute, managed storage, and organization-managed connector packages.

User-provided local, SSH, or HPC resources can still produce receipts and usage
signals without becoming Cloud-billed resources by default.
