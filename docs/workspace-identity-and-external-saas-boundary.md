# Workspace Identity And External SaaS Boundary

Owner: `one-person-lab-cloud`
Purpose: `workspace_identity_decision`
State: `active_decision`
Machine boundary: Human-readable product decision; implementation and runtime
state come from the owning service and instance readback.

This document records the current OPL Cloud product decision. It is a planning
boundary, not a delivery or runtime-readiness claim.

## Workspace Identity

The canonical Cloud identity rule is:

```text
1 user account -> 0..N independent OPL Workspaces
```

- OPL App is the user's workbench and the default Workspace application.
  Workspace identity is independent of the selected OCI application; the
  [application boundary](architecture.md#workspace-application-boundary) owns
  deployment and data separation.
- Each Workspace has a stable `workspace_id`, URL, runtime, storage, resource
  binding, credentials, billing period, lifecycle, and receipt chain.
- OPL Cloud sets no fixed product-level Workspace count limit. Balance,
  provider capacity, quota, and account policy can still admit or reject each
  creation independently.
- Projects, tasks, files, artifacts and continuation entries live inside their
  selected Workspace. They do not become Workspace identity.
- Collaboration may share refs, artifacts, approved resources and policy, but
  it does not merge independently owned Workspaces into a shared SaaS account.
- Console may manage account billing, quotas, approvals and managed resources.
  It lists and governs every Workspace owned by the account without becoming
  the runtime or provider-state owner.
- OPL Serve may let the account publish multiple Agent Services. A Service is an
  externally callable deployment resource, not another Workspace, browser
  workbench, project container or collaboration account.

For the OPL App application, its WebUI carrier is provided through the active
App shell and consumes App, Framework and domain-owner projections. Other
applications retain their own browser or worker interfaces. A browser renderer
or transport does not become a second owner of Workspace identity or its
resource and entitlement facts.

## Collaboration And Serve

Organizations and teams govern policy, approval, and collaboration around
independent Workspaces. OPL Serve publishes Agent Revisions as separate Agent
Services through its Agent Edge.


## Proposed Workspace Agent relationship under product review

The proposed target changes only the Agent relationship, not Workspace
cardinality: one account may still own many independent Workspaces. An Agent
is delivered into a selected Workspace, and that Workspace has at most one
current Agent at a time; replacement preserves deployment history but never
creates a second current selection. Serve owns the delivery/current state.
Workspace owns identity, membership, entitlement, resource plan, and target
authorization, and stores no deployment pointer or readiness copy. API, Embed,
and Hosted UI all route to the same Serve-owned current Agent.

The historical separate Agent Service wording is retained in the baseline
commit for review; this section is the proposed target and requires explicit
product-owner approval.
