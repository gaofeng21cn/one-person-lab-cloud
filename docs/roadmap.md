# OPL Cloud Roadmap And Current Gaps

Owner: `one-person-lab-cloud`
Purpose: `single_active_truth_plan`
State: `active_planning`
Machine boundary: This document owns only the current human-readable Cloud
planning summary, open product/structure gaps, evidence boundary, and next Agent
prompt. It does not prove service implementation, runtime health, billing,
security, release, owner acceptance, or production readiness.

## Target State

OPL Cloud extends OPL work from a local App into one primary online Workspace
per user, approved AI/resource use, optional exact Agent Service publication,
and inspectable evidence continuity. Cloud products consume Framework package
and execution refs and domain-owner judgments without creating competing
truth.

This repository remains the target product-architecture and documentation
owner. Gateway, Workspace, Serve, Console, Fabric, Ledger, Runway, package, and
domain implementation truth belongs to the corresponding implementation,
machine contract, runtime readback, and owner receipt.

## Current Status Summary

| Theme | Current state | Boundary |
| --- | --- | --- |
| Repository role | `docs_and_product_architecture_only` | No Cloud service runtime or billing system is implemented here |
| Product split | `documented` | Gateway, Workspace, Serve, Console, Fabric, and Ledger have one target responsibility owner each |
| Framework boundary | `documented` | OPL Packages and OPL Runway remain Framework-owned; Cloud consumes refs |
| Domain boundary | `documented` | Domain Agents retain professional truth, quality, artifact, and delivery authority |
| Workspace identity | `decided` | One user account owns one primary Workspace; Services are separate deployment resources |
| Active documentation | `consolidated` | This file owns current gaps/next prompt; public product map stays in the root README and the technical split stays in architecture |
| Whitepaper | `source_and_build_profile_present` | Source/build evidence does not prove publication or Cloud service readiness |
| Service delivery | `not_established_by_this_repo` | Availability requires fresh evidence from each owning implementation |

## Functional And Structural Gaps

| Gap | Target outcome | Owner route |
| --- | --- | --- |
| Workspace continuity | One project/task/artifact model works locally and online | App + Workspace implementation owners |
| Managed-resource policy | Account approval and quotas stay distinct from package, service, and resource mutation | Console owner |
| Resource binding | Compute, storage, environments, and connectors share one plan/approve/execute/collect contract | Fabric owner |
| Package projection | Every Cloud surface consumes exact OPL Package refs without a registry copy or Cloud writer | Framework OPL Packages + each consumer |
| Service publication | An exact package digest becomes a stable Service, immutable Revision, and controlled Deployment | OPL Serve + package/domain contract owners |
| Public service edge | External consumers receive authenticated, rate-limited API, event, and Webhook surfaces | OPL Serve Agent Edge + Console policy |
| Provider lifecycle | Native and approved external providers share one Invocation/Session contract | OPL Runway + Fabric/provider adapters |
| Hosted clients | API, Embed, and Hosted UI consume one Serve contract | OPL Serve |
| Connector boundary | Shared access stays generic while domain adapters keep domain semantics | OPL Connect + domain owner |
| Evidence continuation | Runs and service calls return owner refs, review status, and continuation without centralizing source data | OPL Ledger + workbench/Serve owners |

These are target-family gaps, not tasks to implement inside this docs-only
repository. A gap closes here only after its owner surface exists and the target
architecture can reference fresh machine or implementation evidence without
copying that truth.

## Evidence Gaps

Real App-to-Workspace continuation, managed and institution-owned resource
paths, data-egress acceptance, connector/provider runtime evidence, public-edge
security and isolation, deployment health and rollback, Invocation/Session
streaming and cancellation, billing/quota readback, user-visible receipts,
production soak, release evidence, and owner acceptance remain later evidence
lanes.

Docs, planning contracts, generated projections, tests, or a rendered
whitepaper can close only their own layers. They cannot substitute for these
runtime, release, security, billing, domain, or owner-evidence lanes.

## Target Delivery Order

1. Close the App/Workspace contract and one managed Workspace path.
2. Close one resource execution path with explicit plan, approval, collection,
   and receipt.
3. Project exact OPL Package status/actions into Cloud surfaces without a
   registry copy.
4. Add Console account policy for exact package and resource refs.
5. Add one stable connector or compute adapter through the shared contract.
6. Add exact Ledger/public readback for user-visible actions and artifacts.
7. Close a portable Service Entrypoint Contract and immutable Agent Revision.
8. Add a dedicated Agent Edge and API-only private-beta path.
9. Add the provider-neutral Runway port and one OPL-native provider adapter.
10. Add Hosted UI and Embed clients only through the same public API.
11. Add broader service plans only after live security, isolation, billing, and
    soak evidence exists.

## Explicit Non-Goals

- Do not rebuild package discovery, installation, lock, update, rollback, or
  repair in Cloud.
- Do not create a multi-tenant SaaS workbench or multiple primary Workspaces per
  user.
- Do not expose a Workspace URL, sandbox, or provider session as a public Agent
  endpoint.
- Do not create separate execution behavior for API, Hosted UI, and Embed.
- Do not use external collaboration repositories as OPL Cloud implementation
  owners.
- Do not treat policy approval, resource binding, receipt presence, document
  completeness, or artifact rendering as Agent, service, or production
  readiness.

## Next-Round Agent Prompt

### Goal

Close one owner-approved OPL Cloud functional or structural gap and fold the
result back into this single Active Truth without adding runtime claims to the
docs-only repository.

### Write Scope

- `docs/roadmap.md`
- the one existing product or planning-contract owner directly affected by the
  selected gap
- `docs/README.md` and root `README*` only if public navigation or product
  language materially changes
- an owning implementation repository only when its owner and write set are
  explicitly in scope

### Non-goals And Forbidden Scope

- no Cloud service/runtime implementation inside `one-person-lab-cloud`;
- no second package registry, lock, Invocation/Session store, domain verdict,
  or owner receipt;
- no adoption of external SaaS repositories as an implementation owner;
- no availability, release, billing, security, or production claim without
  exact owner evidence.

### Live Truth Inputs

- fresh `main`, worktree, dirty, ahead/behind, remote, and owner/write-set gate
  for every affected repository;
- current App and Framework contracts/read models for package, state/action,
  Workspace, and Runway boundaries;
- the selected service owner's machine contract, source/tests, runtime readback,
  blockers, and owner receipt;
- this index, architecture, focused product owner, and current roadmap.

### Required Actions

1. Select exactly one open row from `Functional And Structural Gaps` and name
   its owner surface.
2. Verify the current implementation and contract state from fresh owner
   evidence; do not infer it from Cloud prose.
3. Implement or update only the authorized owner surface and its focused tests.
4. Rewrite the focused Cloud target reference only where the owner boundary or
   public target changed.
5. Remove or rewrite the closed gap in this file; keep evidence-only tails in
   `Evidence Gaps`.
6. Update the docs index only when a current owner or entry changes.

### Verification Commands

- run the affected implementation repository's native focused/full validation;
- run the OPL Doc doctor against each changed repository as a risk map;
- scan all tracked Markdown relative links;
- run `git diff --check`;
- if whitepaper source/profile changes, run
  `node --experimental-strip-types scripts/build-opl-cloud-whitepaper.ts`.

### Completion Gate

- one structural gap is closed in its owning surface and no competing truth is
  introduced;
- current docs describe only fresh facts and remaining gaps;
- tests/docs evidence is not promoted to runtime, release, owner acceptance, or
  production readiness;
- changed repository `main` branches contain the final bytes and their task
  worktrees/branches are cleaned after absorption.

### Foldback Target

- current status, remaining gaps, evidence tails, and the next prompt return to
  `docs/roadmap.md`;
- stable product responsibility returns to `docs/architecture.md` or the one
  focused product/contract owner;
- navigation changes return to `docs/README.md` and public entry language only
  when needed.
