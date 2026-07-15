# Workspace Identity And External SaaS Boundary

This document records the current OPL Cloud product decision. It is a planning
boundary, not a delivery or runtime-readiness claim.

## Workspace Identity

The canonical Cloud identity rule is:

```text
1 user account -> 1 primary OPL Workspace
```

- OPL App is the user's local workbench and OPL Workspace is the cloud form of
  the same workbench model.
- Projects, tasks, files, artifacts and continuation entries live inside that
  Workspace. They do not create additional Workspace products.
- Collaboration may share refs, artifacts, approved resources and policy, but
  it does not turn one Workspace into a shared multi-tenant SaaS account.
- Console may manage account billing, quotas, approvals and managed resources.
  It does not let one user accumulate multiple independent OPL Workspace
  instances or create a second workbench truth.
- OPL Serve may let the account publish multiple Agent Services. A Service is an
  externally callable deployment resource, not another Workspace, browser
  workbench, project container or collaboration account.

The browser carrier for this path is the OPL App WebUI implementation provided
through the active App shell. It consumes App, Framework and domain-owner
projections. A browser renderer or transport may not own a second task,
package, artifact or Workspace state model.

## External Collaboration Repositories

The following repositories are external collaboration history, not canonical
OPL Cloud implementation repositories:

| Repository | External role | OPL maintenance status |
| --- | --- | --- |
| `RenDeHuang/OPL-Webui` | Multi-user browser SaaS and Web control-plane experiment | Not owned, not an OPL Cloud carrier, excluded from the maintained repo set |
| `RenDeHuang/MedOPL` | Companion commercial resource control plane for that SaaS experiment | Not owned, not an OPL Cloud owner surface, excluded from the maintained repo set |

These repositories must not be used as OPL App WebUI truth, OPL Workspace
identity truth, Cloud architecture authority, default audit scope or an OPL
family maintenance target. Useful implementation lessons may be reintroduced
only through an explicit, owner-reviewed intake into a current OPL-owned
surface.

Similarly named third-party or collaborator repositories are not adopted by
name. The canonical planning owner remains
`gaofeng21cn/one-person-lab-cloud`; App WebUI behavior remains owned by
`gaofeng21cn/one-person-lab-app` and its active shell carrier.

## Reading Older Team Language

Older Cloud documents may describe organizations, teams or shared resources.
Those terms only describe optional policy, approval and collaboration around
separately owned user workspaces. They do not override the one-user,
one-primary-Workspace identity rule and do not authorize a multi-tenant Web
product or multiple Workspaces per user.

This non-goal does not prohibit OPL Serve. Serve exposes an exact Agent Revision
through a dedicated Agent Edge and optional API client templates. It does not
turn the user's primary Workspace into a multi-tenant SaaS workbench or reuse a
Workspace URL as an external service endpoint.
