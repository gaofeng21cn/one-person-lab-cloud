# OPL Cloud Architecture

OPL Cloud is the target product architecture and implementation-family
navigation surface for extending OPL work from a local App into online
workspaces, organization-managed resources and remote execution. This document
defines responsibility boundaries; it does not claim that every service is
currently deployed.

```text
OPL Cloud
├─ OPL Gateway       user-visible AI access, routing and usage
├─ OPL Workspace     user-visible cloud workbench
├─ OPL Console       organization policy, approval, quota and billing
├─ OPL Fabric        Connect, Compute, Storage, Environments and adapters
└─ OPL Ledger        receipt and provenance refs

OPL Framework
└─ OPL Packages      manifest, digest, install, lock, update, rollback and repair

Domain agents        domain strategy, quality verdict and delivery authority
```

```mermaid
flowchart TB
  User[User / Team] --> App[OPL App]
  User --> Workspace[OPL Workspace]
  Admin[Admin / Operator] --> Console[OPL Console]
  Domain[Domain Agent] --> App
  Domain --> Workspace

  App --> Gateway[OPL Gateway]
  Workspace --> Gateway
  App --> Packages[OPL Packages]
  Workspace --> Packages
  Console -. organization availability policy .-> Packages

  App --> Fabric[OPL Fabric]
  Workspace --> Fabric
  Console -. resource policy and approval .-> Fabric
  Packages -. package refs and requirements .-> Fabric

  Fabric --> Connect[OPL Connect]
  Fabric --> Compute[OPL Compute]
  Fabric --> Environments[OPL Environments]
  Fabric --> Storage[Workspace Storage]
  Fabric --> Ledger[OPL Ledger]
  Domain --> Ledger
```

## Surface Roles

| Surface | Owner responsibility | Explicit non-owner boundary |
| --- | --- | --- |
| OPL Gateway | AI access, routing, provider policy and usage signals | Package state and domain quality |
| OPL Workspace | Cloud workbench, project state, artifacts and user-visible status | Package lifecycle and resource truth |
| OPL Console | Organization, role, quota, approval, billing and managed-resource policy | Package install/update/repair and job execution |
| OPL Fabric | Connector, compute, storage and environment availability; resource binding and execution adapters | Package registry/lock and domain verdicts |
| OPL Ledger | Receipt, provenance, review and continuation refs | Source data, package truth and domain verdicts |
| OPL Packages | Discovery, validated manifest, digest, dependency closure, install, lock, update, rollback, repair and lifecycle receipt | Organization policy and domain truth |
| Domain agent | Domain strategy, evidence judgment, quality verdict and delivery authority | Cloud infrastructure truth |

## Execution Boundary

OPL App and OPL Workspace use the same resource execution pattern:

```text
plan -> approve -> execute -> monitor -> collect -> receipt
```

Console applies organization policy when a workspace, connector or resource is
Cloud-hosted or organization-managed. Fabric performs the approved resource
binding and execution. User-provided local, SSH or HPC resources can use the
same pattern without becoming Console-billed resources by default.

## Package Lifecycle Boundary

There is no Cloud-owned Agent Registry. `opl packages` is the only package
lifecycle and lock authority. Its validated manifest and lifecycle receipt are
the source for package identity, version, digest, dependencies and current
installation state.

Cloud surfaces consume those refs without redefining them:

- Console projects whether an organization permits a package ref and which
  roles, quotas or workspaces may use it.
- Fabric reads package requirements and binds compute, storage, environments
  and connectors for a run.
- App and Workspace display current package state and actions from Framework.
- Ledger may record package lock and lifecycle receipt refs for later review.

None of these projections can install, update, roll back, repair or create a
second package registry truth.

## Connector And Domain Boundary

OPL Connect owns stable connector access, normalized source refs, credential
boundaries, errors, retries and rate limits. Domain-specific adapters and
domain agents own retrieval strategy, evidence selection, synthesis and quality
judgment. Ledger records refs only.

The current OPL connector surface and any domain-specific adapter must be read
from fresh Framework/domain contracts and runtime readback. A target connector
described in Cloud docs is not a readiness claim.

## Data Boundary

Cloud stores refs, metadata, lineage, receipts, usage and policy records.
Sensitive source data remains in user workspaces, institutional storage or
private buckets by default. A Cloud receipt points back to the owning source; it
does not become a second source of truth.

## Currentness Boundary

This repository explains the target product split. Service availability comes
from the corresponding implementation repo, API contract, runtime health and
owner receipt. Framework package state comes from `opl packages` readback.
Contract presence, documentation, a successful build or an empty queue does not
prove Cloud, package, domain or production readiness.
