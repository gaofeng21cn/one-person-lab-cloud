# OPL Console

Owner: `one-person-lab-cloud`
Purpose: `console_target_reference`
State: `active_target_reference`
Machine boundary: Human-readable target product reference; implementation and
readiness come from Console source, tests, status, and owner readback.

OPL Console is the user and administrator management surface for OPL Cloud. It
manages account onboarding, the account's Workspace collection, plans, balance
and charge projections, quotas, budgets, approvals, and policy for Cloud-hosted
or explicitly managed resources and Agent Services.

The Framework `Console` contribution projects runtime/read-model facts inside
the single Framework Cordis Host and may provide typed client contributions for
an App Shell. OPL Cloud Console presents Cloud accounts, policy, quotas,
Workspace lifecycle and billing through owner-backed product APIs. Console BFF
aggregates the extracted owners; Control Plane retains unmigrated APIs and
obligations. The Cloud repository owns product release. App selects `opl-studio`
as its active GUI carrier; `opl-aion-shell` is a retired migration baseline.

## MVP Boundary

Core Console is deliberately thin: it exposes Workspace, balance, usage and
owner-backed delivery facts. The current identity-specific browser paths are
owned by [implementation architecture](implementation-architecture.md#console-source-truth).
Sub2API owns balance and usage, and Ledger supplies receipt projections.

The accepted public-beta target adds zero-balance registration, administrator
top-up and controlled purchase. Customer-operated payment/top-up, broader
managed-resource policy and complete public serving remain later outcomes.
Current capability is owned by [status](status.md); gap and priority
are owned by the [roadmap](roadmap.md).

## Governance Objects

| Object | Purpose |
| --- | --- |
| Account | Identity, billing, policy, and resource boundary |
| Collaboration policy | Optional sharing of refs, artifacts, and approved resources without sharing Workspace ownership |
| Role | Permission bundle for collaboration, approval, administration, or audit |
| Workspace policy | How each account Workspace is created, accessed, renewed, suspended, deleted, and retained |
| Resource policy | Which compute, storage, connector, and environment refs are permitted |
| Package availability policy | Which exact OPL Package refs may be used by the account Workspace |
| Service policy | Which exact revisions may be published and under which consumer, data, quota, and retention policy |
| Service plan | How managed usage is attributed and billed; not an OPL Package |
| Approval policy | Which actions need approval and who can approve them |
| Audit policy | Which actions require receipts and retention |

## Approval Targets

Console may approve:

- Workspace creation, renewal, recovery, suspension, and deletion;
- connector credentials and explicitly managed access;
- environment, compute, and storage use;
- availability of an exact Package-owner publication ref with fresh carrier
  state;
- Agent Instance creation under account policy;
- creation, deployment, traffic promotion, pause, rollback, custom domain, and
  retention actions for an exact Agent Revision through Serve;
- budget, quota, retention, and reviewer-gate policy.

Package approval is a policy decision only. The Package owner controls identity
and publication bytes; the configured carrier controls install/update/remove
and native readback. Framework only delegates those actions and aggregates
their state.

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

Console can present owner-backed metering projections for Gateway provider
usage, the managed Workspace plan, Serve endpoint and invocation/session usage,
Cloud-hosted compute and storage, and explicitly managed connector usage.
User-provided local, SSH, or HPC resources can still produce Fabric and Ledger
refs without becoming Cloud-billed by default.

Sub2API is the only spendable-balance owner. Resource Catalog owns extracted
plans and pricing policies; Gateway Integration coordinates the original
accepted wallet operation. Control Plane retains the legacy catalog, billing
projection and settlement paths for existing callers. Console presents the
corresponding owner DTOs. Fabric returns resource and provider facts, while
Ledger records append-only charge, refund, resource, and reconciliation
receipts.

An exact package or revision ref attached to usage is attribution metadata.
Package owners, carriers, Serve, and domain owners supply the descriptor,
installed state, service state, and readiness facts that Console presents.

## Product Boundary

Ordinary users ultimately use Console for account onboarding, balance and usage, Workspace
creation and lifecycle; they perform professional work in App or
Workspace. Administrators use Console to decide who may use or publish which
managed capability and under what budget or policy.
Serve performs Agent Service lifecycle actions, Runway owns Invocation/Session
execution, Fabric executes approved resource bindings, the configured carrier
performs Package mutations through Framework delegation, and Ledger records
refs. Domain owners retain professional quality and delivery authority.


## Domain Owner Boundary

The [adopted architecture](architecture.md#repository-and-instance-topology)
separates the browser surface from domain authority:

- Migrated cloud Console surfaces call the BFF for authentication and typed owner
  readbacks; unmigrated callers retain Control Plane APIs.
- Workspace owns identity, membership, entitlement, resource plans, quote/purchase obligations, and target authorization.
- Capability, Build, and Runtime Control own Package facts, immutable OCI build facts, and approved Runtime Release facts respectively.
- Serve owns the Workspace Agent delivery, current deployment, readiness, and API/Embed/Hosted UI access.
- Fabric returns only infrastructure resource provisioning, binding, and readback facts; it does not answer Agent readiness.
- Ledger records append-only evidence and opaque provenance; it does not become a lifecycle or Saga writer.

Control Plane remains the migration source until each caller and write path is
transferred. The owner map does not claim that all handlers are installed or
qualified in an Instance.

The portable Console build defaults to `VITE_CONSOLE_IDENTITY=legacy`, retaining
the Control Plane login and Workspace APIs. Only a build explicitly selecting
`cloud` exposes the Publisher route, its Workspace-list entry, and the Agent
delivery readback panel, together with the CloudIdentity session API. Shipping
the owner executables in the image does not enable these BFF surfaces.
