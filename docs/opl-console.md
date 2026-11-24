# OPL Console

Owner: `one-person-lab-cloud`
Purpose: `console_target_reference`
State: `active_target_reference`

OPL Console is the user and administrator browser surface for OPL Cloud:
account onboarding, Workspace lifecycle, plans/quotes, billing projections,
quotas, approvals and policy. It stays separate from Framework's in-process
`Console` read-model projection; desktop App delivery uses `opl-studio`.

## Agent-Assisted Read And Review

- **Read-Only MCP Endpoint (`/api/v2/mcp`)**: Provides `cloud_list_compute_plans`,
  `cloud_list_storage_plans`, `cloud_list_workspaces`, `cloud_get_quote`,
  `cloud_get_workspace` and `cloud_get_workspace_operation` with `cursor`/`limit`
  (1..100) and optional `expectedRevision`. Tools preserve owner JSON and
  observation times (including `unknown`) and never create/retry purchases or
  reveal credentials.
- **Exact Quote Review**: Quote reads return a same-origin `reviewPath` displaying
  exact quote terms; changed quotes reject with `stale_revision`.
- **Short-Lived Connections**: `/api/v2/agent/connections` issues 1-hour
  parent-session-bound credentials for [Cloud Connect](runtime/cloud-connect.md);
  Framework holds only opaque refs and revocation/logout takes effect on the next
  owner call. Implementation details live in
  [implementation architecture](implementation-architecture.md#agent-reads-and-quote-review).

## MVP Boundary

Core Console exposes owner-backed Workspace, quote, billing and Service reads;
identity-specific routing lives in [implementation architecture](implementation-architecture.md#console-source-truth).
Sub2API owns spendable balance/usage and Ledger owns receipts. Current verified
state belongs to [status](status.md); open work belongs to [roadmap](roadmap.md).

## Governance Objects & Approval Targets

- **Objects**: `Account`, `Collaboration policy`, `Role`, `Workspace policy`,
  `Resource policy`, `Package availability policy`, `Service policy`,
  `Service plan` (billing plan, distinct from OPL Package), `Approval policy`,
  `Audit policy`.
- **Approvals**: Workspace create/renew/recover/suspend/delete, connector access,
  compute/storage entitlement, approved Package refs (owner retains bytes/carrier),
  Agent Instances, Serve deployment/promotion/rollback/custom domains and
  quotas/reviewer gates.

## Service Plan, Metering & Ownership Boundary

Billing plans (`Gateway`, `Workspace`, `Serve`, `Compute`, `Storage`, `Connectors`,
`Agents`) map metered usage from Gateway, Workspace, Serve and Fabric through
Ledger receipts. Console composes owner projections without inheriting Fabric,
Sub2API, Ledger or domain agent authority; commercial marketplace/tax/refund/end-customer
billing extensions remain outside current MVP scope.
