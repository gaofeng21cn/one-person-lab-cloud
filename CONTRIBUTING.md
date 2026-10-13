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
2. Choose one bounded objective and identify its canonical owner and Git change
   scope. Reference a roadmap gap when applicable; ordinary maintenance does not
   need a registered gap or phase record. Multiple rows may proceed concurrently;
   priority ranks urgency and benefit,
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

## Ordinary Source Development

The [2026-10-13 decision](docs/decisions.md#2026-10-13-ordinary-source-development-uses-git-scope-and-current-ci)
sets the default: explicit Git scope, relevant local behavior tests, a PR and CI
for the current revision. Describe the problem, resulting behavior, actual
verification and risks in the PR. No independent source-check JSON, fixed
owner/phase/write-set form or phase-plan registration is required. An independent
agent may review the exact source head and base. Production, money and resource
changes retain owner review and explicit authorization; existing branch
protection and merge authorization still apply.

## Scoped Development Runs

Select this entry only for tasks that explicitly require controlled delegation.
It is not a prerequisite for ordinary source changes.

Existing phase-plan records are task inputs, not a permanent registry of owners
or permissions. Stable source ownership remains with the canonical documents.
For a scoped worker, the trusted host admits a run outside the repository or in
ignored runtime storage, referencing that plan record and owner with a narrower
scope, explicit stage inputs and a runner that exposes no arbitrary shell.
Follow [Scoped Development Contract](AGENTS.md#scoped-development-contract) and
[the developer entry](DEV_GUIDE.md#scoped-host-and-worker-entry); use
`npm run dev:tools` and `npm run dev:run`, not a worker publication command.

Acceptance comes from actual runner results and append-only host-owned per-run
receipts. It does not create a task schema, global current-state writer, or
automatic status/roadmap projection. Product evidence summaries are separate,
explicit changes by their canonical owner. A chat with unrestricted shell or
filesystem access is not a restricted worker; tooling documentation does not
establish enforcement across existing Codex sessions.

## Branch And Pull Request Flow

1. Use a short-lived branch based on a verified `origin/main` for one objective.
   Use `codex/<objective>` for Codex-authored branches. Disjoint changes may share
   the current workspace; a new worktree or host admission is not required.
2. Keep the write set narrow. Separate unrelated UI, contract, billing, auth,
   runtime, infrastructure, and documentation work.
3. Open a pull request to `main` describing the problem, behavior, actual local
   verification and risks with the repository template. Add existing owner
   evidence references for high-consequence changes when applicable.
4. Update the branch to current `main` and resolve every review conversation.
   An independent agent may perform ordinary technical review against the exact
   head and base. Production, money and resource changes require their owner
   review and explicit authorization.
5. Merge only after CI succeeds for the current revision, including `validate`,
   and actual branch protection, applicable review and merge authorization
   requirements are satisfied. Delete the branch after merge.

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
`npm run verify:local`; it has no PR-body governance job and does not invoke
`check:pr-body`. The manually dispatched Qualification workflow uses a
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

Run the relevant local checks for the explicit Git scope before pushing:

```bash
npm run verify:local:focused -- --base <verified-base-ref> --test <existing-owner-behavior-test>
```

Use repeated `--test` for existing behavior targets when a source change has no
changed test. For changed test files or affected Go packages, focused verification
selects those targets directly. Record actual execution and failures; do not use
typecheck or a planned command as passing behavior evidence. See
[Pre-Commit Checks](DEV_GUIDE.md#pre-commit-checks) for selection and boundary rules.
Documentation-only changes use relevant static checks locally and still receive
current-revision PR CI.

The exhaustive source gate runs in CI and is available for explicit local
rehearsal; it is not a routine local prerequisite:

```bash
npm run verify:local
```

The Workspace lifecycle browser regressions in the baseline gate drive the
Google Chrome already installed on the machine (`channel: "chrome"`, resolved
through `tools/launch-browser.ts`). No Playwright browser download is required.

For persistence, capacity, local-Docker, or cross-service behavior changes whose
database/provider boundary is affected, run `npm run verify:local:full` alongside
the relevant owner checks. It starts an ephemeral PostgreSQL 16 container and
requires the PostgreSQL, capacity, and local-Docker integration tests to finish
without skips before removing the container. Both local gates remain source
verification only; they do not access production or replace the required hosted
`validate` check.

The exhaustive source gate and migration-plan freshness have distinct scope.
CI selects the dedicated plan check for affected target-spec changes using its
explicit PR base and head SHAs; focused local checks reuse that classifier.
The former PR-body checker is retired. See
[Development Plan Verification](DEV_GUIDE.md#development-plan-verification).

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
