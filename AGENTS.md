# OPL Cloud Development Rules

`one-person-lab-cloud` (`gaofeng21cn/one-person-lab-cloud`) is the single Cloud
product repository. `opl-cloud` is the package, image, service and runner name.
Instance, Sub2API and Framework remain separate authorities.

## Scope And Completion

- Before editing, identify the outcome, canonical owner, callers, verified Git
  base, write set and completion evidence; no separate governance form is needed.
- Complete authorized inspection, implementation and verification autonomously.
  Ask only when an unresolved decision changes scope, authority or irreversible
  effects; skills must not add duplicate approvals or mandatory checkpoints.
- Keep one bounded objective. Include adjacent fixes only when they are needed
  for that objective's acceptance. Record unrelated findings without starting
  another Issue, PR review or cleanup task unless the user extends the scope.
- Finish when the requested outcome and necessary checks are satisfied. Report
  actual changes, evidence and remaining limitations; a plan or source pass is
  not completion of a requested runtime outcome.
- Parallelize independent owner/write sets; serialize overlapping files, shared
  contracts, canonical `main` and production changes. Bound delegated tasks and
  close agents after integration. Coordinate existing workspace changes; a new
  worktree is not an ordinary prerequisite.
- Follow [the PR flow](DEV_GUIDE.md#ordinary-pull-request-flow) for PR delivery.
  Keep commit, push, PR, merge, publish and deployment actions within the user's
  authorization; passing checks or technical review grants no further authority.
- New GitHub-visible text is English: commits, PRs, Issues, comments and Release
  notes. Replies to another person's text use that person's language; existing
  historical text and original quotations retain their language.

## Read For The Current Task

Use [docs/README.md](docs/README.md#entrypoints-and-consumers) to locate the owner;
read the relevant sections rather than loading every linked document.

| Task | Required reference |
| --- | --- |
| Change a module or its callers | [Target topology](docs/architecture.md#repository-and-instance-topology), [owner matrix](docs/spec/target/01_domain_ownership_matrix.md#2-领域分工与内部lane), and [current source/request map](docs/implementation-architecture.md) |
| Change billing, Workspace, Fabric, Ledger, Gateway, deployment or E2E | Owning sections in [architecture](docs/architecture.md), [invariants](docs/invariants.md), and the current [machine contracts](packages/contracts/README.md) |
| Resolve conflicting intent or evidence | [Decisions](docs/decisions.md), the owning source/contracts, relevant Git history and authoritative readback |
| Change documentation | [Documentation hierarchy](docs/README.md) and [lifecycle policy](docs/policies/docs-lifecycle-policy.md) |
| Build a Candidate or handle Release evidence | [Release operations](docs/runtime/release.md) and the current distribution contract |
| Explicitly controlled delegation or host-tool changes | [Scoped Development Contract](#scoped-development-contract) |

The latest user decision sets intent. Distinguish target, implementation,
runtime/production evidence, history and unknowns; reconcile conflicts in their
canonical owner and consumers. Issues, PRs and session claims are inputs.
Update [status](docs/status.md) and [roadmap](docs/roadmap.md) when evidence or
acceptance changes; preserve open external obligations. Completed plans and
transcripts belong in Git history or `docs/history/`.

## Ownership And Implementation

- Trace the real input-to-owner path before fixing a bug. Repair the deepest
  failing owner with the smallest complete change and verify the real consumer.
- Keep one writer per fact. Move a live capability and its callers together;
  retire the old write path while preserving persisted data and historical
  obligations. Control Plane is a migration source, not a permanent second writer.
- Keep DTOs, state machines and retry policy with their owner; consumers use thin
  typed clients. Provider behavior stays behind Fabric adapters. Cross-owner
  changes update the owning contract, affected consumers and boundary tests.
- Keep the single shared Go module at `packages/contracts/go/go.mod`, proto in
  `packages/contracts/proto/` and generated Go under that module. Consumers use
  the same Cloud commit, schema hashes and locked tools; update required
  consumer `go.mod`/`go.sum` with the contract.
- Shared infrastructure needs two current callers and stays policy-free. New
  abstractions, dependencies, services or gates need a demonstrated capability
  gap or reachable failure, a caller and a bounded owner. Follow adopted typed
  coordination; add no second wallet, Gateway, registry, lock, business authority
  or orchestration framework.
- Fix the cause within the current owner. Preserve unknown or failed readback;
  do not add fallbacks, compatibility branches or weaker assertions to hide it.

<!-- CODEGRAPH_START -->
## CodeGraph And Context Use

- Use the existing `.codegraph/` index for code definitions, callers, impact and
  covering tests before changing shared symbols; use `rg` for literal text.
  Refresh the existing index after updating source. Keep it untracked.
- Read relevant symbols/ranges and reuse them until inputs change. Batch
  independent reads; retain decisive errors/results rather than full files,
  repeated diffs or entire logs. Keep diagnostic output in temporary/ignored storage.
- Use existing wait/watch commands for long checks; avoid model turns merely
  polling unchanged status. Return compact results with failures visible.
- Compaction/handoff retains the objective, exact base/head, changed paths,
  verified results, unresolved failures and next action; continue the task.
<!-- CODEGRAPH_END -->

<!-- TESTING_RULES_START -->
## Verification

Start with the verified base for this change:

```bash
npm run verify:local:focused -- --base <verified-base-ref>
```

- For source behavior changes without changed tests, declare existing owner
  targets with repeated `--test <file>`. Missing targets are refused. Use current
  typed DTOs/structs; update affected tests with production type changes.
- At generic persistence or untrusted JSON boundaries, test the real decoder and
  rejection behavior. Test owner behavior and real consumers, not duplicate
  contract JSON or assertions that retired files remain absent.
- `npm run verify:local` is the exhaustive CI gate. Run it locally only when the
  affected boundary or requested verification requires it. Use
  `npm run verify:local:full` for affected persistence, schema, capacity,
  Local-Docker or retained/cross-service behavior as described in
  [Pre-Commit Checks](DEV_GUIDE.md#pre-commit-checks).
- Behavior evidence requires executed tests, zero failures, skips and TODOs;
  compilation alone is not behavior evidence. Record failed or unavailable
  checks and their reasons. Re-run affected checks after relevant changes;
  broaden testing only for new failures or unresolved risks.
- PR delivery requires actual CI for its current revision. Source checks,
  fixture/SQL tests, runtime qualification and Instance deployment prove distinct
  layers; report each only at the layer actually verified.
<!-- TESTING_RULES_END -->

## High-Consequence Boundaries

- Production-private access stays in `opl-instance-medopl` protected workflows
  and authorized runners. Never connect the local machine directly to production
  private networks or dispatch Instance deployment from this repository.
  A proposed Cloud-owned protected entry grants no access before its actual
  authorized implementation and evidence consumers exist.
- Sub2API/Gateway owns spendable balance, effective Keys, Groups, routing and
  usage. Cloud owns approved mappings and coordination, not a second authority.
- Ordinary CI, release and E2E are read-only for real money and provider
  resources. Charges, purchases, renewals and deletion need separate explicit
  authorization and a bounded owner workflow; verification code must not clean
  customer resources or add a public test-billing mode.
- Customer and verification CVM/CBS procurement uses approved prepaid monthly
  policy. Never use `POSTPAID_BY_HOUR`. Secrets stay in approved secret stores
  and authorized runtime boundaries.

## Release And Instance Boundary

Cloud publishes a portable product; Instance owns installation configuration,
Secrets, deployment, rollback and deployment receipts. Before a Candidate or
Release action, read [release operations](docs/runtime/release.md).

- A write-access collaborator may build a replaceable Candidate from the exact
  head of its pushed, workflow-admitted branch, self-attributed. The build guard
  has no actor allowlist; publication has separate authority.
- Formal Release requires the qualified exact source on canonical `main` and
  promotes the same Candidate digest without rebuilding. Only the repository
  owner and `RenDeHuang` may dispatch it; the original publisher stays the actor.
  Local development, Candidate construction and retries authorize neither
  formal publication nor Instance deployment.

<!-- RECEIPT_RULES_START -->
## Receipt And Evidence Persistence

Record ordinary local verification in the PR; no separate source-check JSON is
required. Host runs retain host receipts. Preserve qualification/release schemas
and checks until producers and consumers migrate together. Bind source SHA,
image digest, timestamp, workflow provenance and actual owner readback. Append
receipts; never overwrite attempts or include credentials, raw Keys, customer
data or private addresses. Cloud owns source/business evidence, Instance owns
installation/deployment evidence; Cloud-only success leaves Instance obligations open.
<!-- RECEIPT_RULES_END -->

## Scoped Development Contract

Only when a task explicitly selects controlled delegation, or changes its host
entry/runner, read and follow the full contract in
[Scoped Host And Worker Entry](DEV_GUIDE.md#scoped-host-and-worker-entry).
Ordinary source development does not require host admission, phase-plan
registration, restricted workers or host receipts. Attaching tools or citing the
framework alone does not restrict a shell-enabled chat.
