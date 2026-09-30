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
- OPL Serve owns one current application delivery per Workspace. API, Embed and
  Hosted UI address that Workspace's current Agent when an Agent is selected;
  they do not create another Workspace or a parallel Agent Service collection.

For the OPL App application, its WebUI carrier is provided through the active
App shell and consumes App, Framework and domain-owner projections. Other
applications retain their own browser or worker interfaces. A browser renderer
or transport does not become a second owner of Workspace identity or its
resource and entitlement facts.

## Collaboration And Serve

Organizations and teams govern policy, approval, and collaboration around
independent Workspaces. OPL Serve publishes exact Agent Revisions into those
Workspaces and routes external access through its Agent Edge.


## Workspace Application Selection

The adopted [application decision](decisions.md#2026-09-29-default-opl-app-and-optional-agent-on-one-tencenttke-delivery-path)
preserves Workspace cardinality: one account may own many independent
Workspaces. New delivery selects default OPL App/native UI or an optional built
Agent. A Workspace has at most one current application selection; replacement
preserves deployment history but never creates a second current selection.
Serve owns the delivery/current state.
Workspace owns identity, membership, entitlement, resource plan, and target
authorization, and stores no deployment pointer or readiness copy. API, Embed,
and Hosted UI all route to the same Serve-owned current Agent.

Default App comes from an approved Runtime Release and has no synthetic Package,
BuildJob or CapabilityVersion. The current implementation and retained migration
paths are owned by [implementation architecture](implementation-architecture.md);
this product decision does not claim complete Instance qualification.
