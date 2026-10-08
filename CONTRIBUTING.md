# Contributing To OPL Cloud

OPL Cloud is a shared product and implementation repository. Keep changes small,
owner-aligned, and reviewable; do not create another source of current product,
implementation, instance, billing, or deployment truth.

## Source Of Truth

| Topic | Current owner |
| --- | --- |
| Product target and authority boundaries | `docs/architecture.md` |
| Current implementation boundary | `docs/implementation-architecture.md`, `docs/invariants.md`, machine contracts, source, tests, and runtime readback |
| Open outcomes and priority | `docs/roadmap.md` |
| Durable decisions | `docs/decisions.md` |
| Medopl instance profile, production deployment and receipts | `opl-instance-medopl` |

Issues, pull requests, discussions, and project boards are proposals or work
surfaces. They do not replace these owners.

When these surfaces disagree, first trace the conflict through Git history,
the canonical topic owner, real callers, and runtime evidence. Classify each
claim as target, current implementation, runtime/production evidence,
historical, stale, derived, or unknown; then update the canonical owner and
remove the duplicate current writer in the same pull request.

## Choosing Work

1. Read the ranked outcomes in `docs/roadmap.md`, then check open pull requests
   for an existing owner. The roadmap owns intent, priority and acceptance; the
   pull request owns the live execution attempt.
2. Select one gap ID and keep its owner/write set explicit in the pull request.
   Multiple rows may proceed concurrently; priority ranks urgency and benefit,
   not a global dependency queue. Do not combine Console, provider, billing,
   deployment and cleanup changes unless one acceptance path requires them.
3. Before deleting a public route, schema, stored model, or compatibility path,
   prove that current callers, persisted data, and external consumers no longer
   depend on it. Record any still-open removal outcome in the roadmap.
4. Read the current module inventory, Console/BFF routing, protocol boundaries
   and physical dependency map in `docs/implementation-architecture.md`.
   Extracted owners and retained callers do not share one universal HTTP path.
   Runtime imports of sibling service implementations are forbidden; shared
   infrastructure stays policy-free and `packages/contracts/go` remains the
   single shared contracts module for actual cross-owner consumers.
5. Rebase or update from fresh `main` before review. If another pull request has
   entered the same write set, coordinate ownership or split at a contract/API
   boundary before continuing.

Use `parallel_work_serialized_integration`: develop independent module lanes in
parallel and serialize only an overlapping write set, one shared contract
revision, canonical `main`, or a real production mutation. A production or
instance readback is a qualification gate for that exact release, not a reason
to block unrelated development, CI, or preview work. Reusable deployment code
and instance-specific application may progress independently and converge at
deployment qualification.

## Scoped Development Runs

Existing phase-plan records are task inputs, not a permanent registry of owners
or permissions. Stable source ownership remains with the canonical documents.
For a scoped worker, the trusted host admits a run outside the repository or in
ignored runtime storage, referencing that plan record and owner with a narrower
scope, explicit stage inputs and a runner that exposes no arbitrary shell.
Follow [Scoped Development Contract](AGENTS.md#scoped-development-contract) and
[the developer entry](DEV_GUIDE.md#scoped-host-and-worker-entry); use
`npm run dev:tools` and `npm run dev:run`, not a worker publication command.

Acceptance comes from actual runner results and append-only signed per-run
receipts. It does not create a task schema, global current-state writer, or
automatic status/roadmap projection. Product evidence summaries are separate,
explicit changes by their canonical owner. A chat with unrestricted shell or
filesystem access is not a restricted worker; tooling documentation does not
establish enforcement across existing Codex sessions.

## Branch And Pull Request Flow

1. Start one short-lived branch or worktree from fresh `origin/main` for one
   objective. Use `codex/<objective>` for Codex-authored branches.
2. Keep the write set narrow. Separate unrelated UI, contract, billing, auth,
   runtime, infrastructure, and documentation work.
3. Open a pull request to `main` and complete the repository template.
4. Update the branch to current `main` and resolve every review conversation.
   Human review is risk-based and may be requested, but is not a universal merge
   gate for either active developer.
5. Merge only after the required `validate` check succeeds. Delete the branch
   after merge.

Direct pushes and force pushes are not the normal path. An administrator may
bypass the PR path only for a time-critical repository or production recovery, and
must leave the reason and final readback in a pull request or incident record.

Before editing, name the primary module and canonical owner using the current
inventory in `docs/implementation-architecture.md`. Cross-service behavior uses
the owning typed public contract for that path. Do not import sibling service
source, access sibling tables, deep-import service code from Console UI, copy
state machines/DTOs, or
create a shared package for one caller. If a change truly crosses modules, name
the owning contract and update both sides and their focused tests together.

## Validation

Pull Request CI runs dependency review and a `validate` job that executes
`npm run verify:local`. The manually dispatched Qualification workflow uses a
separate `validate` aggregate for five parallel jobs:

- `node-console`
- `go-contracts`
- `postgres-ledger`
- `control-plane`
- `fabric`

Keep `validate` as the single intended branch-protection context. This rule and
a callable local gate do not prove remote branch protection is configured;
confirm the repository's actual protection settings for enforcement claims.
Do not add path-based skip logic, a merge queue, or a second aggregate check
without measured queue or runtime evidence that the current gate is a real
blocker.

Run the checks affected by your change before pushing. The repository-wide
baseline is:

```bash
npm run verify:local
```

The Workspace lifecycle browser regressions in the baseline gate drive the
Google Chrome already installed on the machine (`channel: "chrome"`, resolved
through `tools/launch-browser.ts`). No Playwright browser download is required.

For persistence, capacity, local-Docker, or cross-service behavior changes, run
`npm run verify:local:full`. It starts an ephemeral PostgreSQL 16 container and
requires the PostgreSQL, capacity, and local-Docker integration tests to finish
without skips before removing the container. Both local gates remain source
verification only; they do not access production or replace the required hosted
`validate` check.

Documentation-only changes still run the shared PR gate. This keeps one stable
required context and avoids a second change-classification authority.

For generated-field changes, `npm run verify:generated-contracts` verifies exact
regeneration of covered outputs from their declared source inputs. W01 separately
owns production/spec reconciliation and real consumer migration; freshness does
not compare every schema, establish field ownership, or complete W01. Preserve
the canonical proto/events/schema owners instead of inventing another catalog.

## Evidence And Production

- A fixture, source check, local pass or green PR proves only its exercised
  layer and checked revision, not product runtime or Instance acceptance.
- Do not report `pilot-ready` or `production-proven` without the matching
  immutable deployment and owner-authoritative readback.
- Production deployment and private-network verification run only through the
  approved GitHub Actions workflows and `production` environment.
- Never add secrets, customer data, raw provider responses, or mutable local
  configuration to a pull request.
- Treat Dependabot pull requests as code changes. Major Action upgrades that
  touch production workflows require deliberate contract-test updates and
  explicit production-risk review; they are not auto-merged.
