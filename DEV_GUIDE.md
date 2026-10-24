# OPL Cloud Developer Guide

## Product Boundary

OPL Cloud develops and releases one portable product: Console, Control Plane,
Fabric, Ledger, and Workspace delivery. It publishes reusable source,
multi-architecture images, Compose assets, and GitHub Releases; it does not
deploy a concrete customer instance.

`opl-instance-medopl` is the separate owner for the medopl instance profile. It
explicitly binds the `.com` domains, Tencent/TKE profile, immutable Workspace
image, production configuration and Secrets, deployment, installation health,
deployment rollback and receipts while consuming an immutable OPL Cloud product
SHA and image digest. Cloud source must not provide those instance defaults.
Customer operations and business qualification follow the existing product
owners; [the roadmap](docs/roadmap.md#workspace-key-and-customer-read-boundaries)
tracks the protected entry and release-evidence migration. Current publication
checks remain enforced until their producers and consumers migrate together.

Current implementation facts belong to
[implementation-architecture.md](docs/implementation-architecture.md) and
[status.md](docs/status.md). Current P0 gaps and their acceptance outcomes belong
only to [roadmap.md](docs/roadmap.md).

## MVP Development Path

The MVP has one vertical path:

```text
thin Console
  -> Control Plane Workspace orchestration
  -> local-docker Workspace provider
  -> OPL App/WebUI Workspace
  -> Sub2API-authoritative balance, usage, and debit
```

This path describes the retained MVP flow, not the complete current service
inventory or Console routing. The current owners, extracted services, retained
callers and request paths belong to
[implementation-architecture.md](docs/implementation-architecture.md). Closing
the product Core path still requires the same-revision Console-to-Workspace
acceptance evidence described in the roadmap. Workspace Delete performs no
wallet or refund mutation. Ledger records the required receipts and
reconciliation evidence; it never owns spendable balance.

## Local Console Preview

```bash
npm ci
npm run demo
```

The demo binds to `127.0.0.1`, uses in-memory fixtures, and makes no external
requests. It proves only the interaction preview.

## Portable Control Services

Use the release-owned Compose file and environment template to validate the
portable control stack. The current deployment-unit inventory belongs to
[implementation-architecture.md](docs/implementation-architecture.md):

```bash
docker compose --env-file deploy/portable/opl-cloud.env.example config --quiet
```

For an actual installation, use only the assets from one GitHub Release and
replace the template values as described in
[installation.md](docs/installation.md). The only public Release, `v0.1.7`, has
five assets. The current source Candidate format has ten and must not be mixed
with it. A healthy Compose stack proves only that the Cloud control services
start; it does not by itself prove Workspace create, readback, access, and
delete through the Local-Docker provider.

## Provider Adapters

Fabric owns provider-neutral resource operations and adapter boundaries. The
current source still wires the Tencent provider, which is an implementation
fact used by the medopl instance, not an OPL Cloud MVP prerequisite. Do not add
Tencent credentials, production domains, deployment dispatch, or instance
receipts to this repository.

## Ownership Rules

- Current Console/BFF routing, extracted owners and retained Control Plane
  responsibilities follow
  [implementation-architecture.md](docs/implementation-architecture.md), not a
  Console-only-to-Control-Plane assumption.
- Domain authority remains in the canonical architecture owners; extraction
  moves real callers and retires the old write path instead of adding a second
  writer.
- Fabric owns provider resources and provider adapters.
- Sub2API owns identity credentials, spendable balance, API Keys, routing, and
  request usage.
- Ledger owns append-only receipts and reconciliation evidence.

## Ordinary Pull Request Flow

The [2026-10-13 decision](docs/decisions.md#2026-10-13-ordinary-source-development-uses-git-scope-and-current-ci)
sets the default for ordinary source maintenance:

1. Establish the verified Git base and actual change scope, then identify the
   canonical module or document owner and real callers. Disjoint write sets may
   share a workspace; isolate or coordinate overlapping changes as needed.
2. Run the relevant local behavior checks described in
   [Pre-Commit Checks](#pre-commit-checks). Record actual commands, results and
   unverified boundaries; typecheck alone does not prove behavior.
3. Open a PR that describes the observed problem, resulting behavior, verification
   and risks using the repository template. Ordinary PRs need no phase-plan
   registration, fixed owner/write-set form or independent source-check JSON.
4. Assess the exact source head and base during technical review. An independent
   agent may perform ordinary PR review. Production, money and resource changes
   retain owner review and explicit authorization.
5. Obtain actual successful CI for the current revision and satisfy the
   repository's existing review, branch protection and merge authorization
   requirements. A revision change needs CI for the new revision and renewed
   verification or review wherever the changed inputs affect earlier evidence.

[Pull Request CI](.github/workflows/pull-request-ci.yml) runs dependency review
alongside the `source`, `go` and two `browser` shards on independent runners.
They select groups from the same exhaustive `npm run verify:local` step list;
the ungrouped local command still executes every step. Browser shards partition
the existing package-script file inventory through Node's test runner; they keep
at most three concurrent files per runner and preserve each test's isolated
fixtures.
`source` also checks the changed development plan from the explicit PR base/head
SHAs and checks committed whitespace. The existing `validate` check always
aggregates every required job and fails on failure, cancellation or skipping.
`dependency-review` retains its name and severity threshold. Remote branch
protection must still be read back independently; preserving check names does
not prove it is configured.

On pushes to `main`, [Warm CI Caches](.github/workflows/warm-ci-caches.yml)
prepares npm/pip and pinned generator caches, verifies generated contracts and
runs the Go group to populate its
module/build cache. Producer and PR consumers share the exact action inputs and
cache keys. PR merge-ref caches cannot be restored by other PRs, so the trusted
main run supplies the default-branch cache new PRs can read. This adds main-run
compute and cache transfer/storage costs; it does not cache passing test results.
An active main cache producer is allowed to finish during subsequent merges.
Cache preparation is a separate workflow and does not report a PR source-gate
result. Pull Request CI retains its PR-only trigger and existing cancellation
policy. First-run cache reuse and speedups require actual GitHub readback after adoption,
including a new PR's first run, a subsequent run and a dependency-change run.

There is no PR-body governance job or `check:pr-body` invocation. Local results
and review cannot replace current-revision CI or authorize a merge, publication
or Instance operation. Candidate, Instance, money and resource evidence retains
its existing owners and requirements.

## Scoped Host And Worker Entry

Use this entry only for tasks that explicitly select controlled delegation, or
when changing its host entry/runner. The following admission, worker, runner and
receipt requirements govern admitted runs, not ordinary source changes or PRs.
This section owns the Cloud host contract and its commands; `AGENTS.md` supplies
the conditional reading entrypoint.

### Admission And Worker Operations

The existing phase plan, including work packages in
`docs/spec/target/checks/development_plan.json`, supplies task inputs. Stable
source ownership remains in the canonical documents mapped by
[docs/README.md](docs/README.md); a plan or execution record cannot redefine an
owner or create a permanent business-owner registry, task schema or global state.

A trusted host admits each run with an authorization record outside the repository
or in ignored runtime storage protected from worker writes. Admission references
an existing plan record and canonical owner, narrows the read/write scope and
declares exact stage inputs and a host-controlled runner with no arbitrary shell
interface. Scope expansion requires new host admission, not a worker edit to the
plan or authorization. Read permission and acceptance targets are not write grants.

The worker interface is limited to `dev_context`, `dev_read`, `dev_search`,
`dev_write`, `dev_status` and `dev_verify`. The worker must receive admitted
context before file operations; reads, searches and writes stay inside that scope.
The host refreshes context before every model turn. Every operation rechecks the
baseline and workspace change scope. A failed refresh or admission check revokes
the old admission; work cannot resume from stale context and requires a successful
`dev_context`. Workers cannot alter approval, runner policy, run state or receipts.
`dev_status` is readback, and `dev_verify` requests execution by the trusted runner;
there is no worker `complete`, `publish` or arbitrary-command operation. Run state
advances only from actual execution results; failed or blocked results stay so.

Every admission reads `AGENTS.md`, this guide, `docs/status.md` and
`docs/roadmap.md`, verifies that the approved base SHA exists and remains an
ancestor of the checkout, checks Git changes against the host-admitted write
scopes, and returns all ready gates. Git metadata establishes the change set;
source and receipts are read only through admitted paths and declared dependencies.
This is a baseline readback, not a repository-wide source scan.

Concurrent changes in one checkout require explicitly named, host-admitted runs
on the same base SHA and plan with current phase scopes. `coauthorRuns` names exact
independent run IDs; `requires` names prerequisite IDs and their transitive chain.
The host validates exact write scopes, never grants every directory in a plan,
and never discovers unrelated runs by scanning the host store. Coexistence allows
authorized changes to remain; it does not satisfy prerequisite receipts or bypass
checks on writes and acceptance. Stale downstream evidence neither erases its
previous edits nor prevents an upstream owner repairing its own stage. Disjoint
checkouts need no coauthor references until integration on an admitted baseline.

### Isolated Inputs And Execution Evidence

The host materializes each stage's declared inputs into an isolated snapshot.
The runner cannot read undeclared repository dependencies; declare them before
admission instead of granting a repository mount or shell escape. Receipt reuse
requires unchanged exact stage inputs and bound upstream evidence. Downstream
receipts bind actual stage input hashes and upstream receipt hashes; an input or
bound evidence change invalidates only the affected stage and its dependents.
Status is computed from those inputs and hashes, not an agent-editable completion
field. A new session/model can continue a valid run without restarting its stages.

The host appends per-run execution receipts to host-owned storage protected from
workers; it never overwrites attempts or automatically edits `docs/status.md`,
`docs/roadmap.md` or a global state file. Canonical summaries are explicit owner
changes. Passing behavior requires actual executed tests, zero exit code and zero
failures, skips or TODOs; a passing label alone is not evidence.

Receipt lookup reads only the named development gate's attempt sequence and
verifies identity, input hashes and bound dependency hashes. It does not enumerate
business receipt bodies. Source, business qualification and Instance receipts
retain their existing owners and formats; none proves completion of another layer.
Fixtures and source checks do not establish runtime, Candidate qualification,
Instance deployment or production readiness. Preserve accepted owner work and
production steps; adopting this entry does not justify discarding or rerunning them.

### Host Commands And Adapters

Use `npm run dev:tools` and `npm run dev:run` as host integration entrypoints.
The current command interface is:

```bash
npm run dev:approve -- /absolute/host-approval.json /absolute/host-store
npm run dev:context -- /absolute/host-store run-id
npm run dev:tools -- /absolute/host-store run-id
npm run dev:run -- /absolute/host-store run-id https://model.example/v1/chat/completions model-id
npm run dev:verify -- /absolute/host-store run-id gate-id
npm run verify:dev-scope -- /absolute/host-store run-id
```

Enforcement belongs at the trusted host entry, not a model loop.
`runRestrictedWorker` is one replaceable adapter; `dev:run` launches a separate
worker with only the six admitted tools. Existing clients may use the host-owned
`dev:context`/`dev:tools` entry without that adapter. A chat retaining unrestricted
shell/filesystem access is not controlled merely because these tools are attached.
These commands and references prove neither universal restricted adoption nor
remote branch protection; both require actual host/runner or remote evidence.

The host's `dev:verify` executes an admitted stage and appends its runtime receipt.
It does not automatically create a repository source-check record. When a run
needs that optional reference, use [Host Source-Check Reference](#host-source-check-reference);
complete stage-input/dependency-hash binding remains in the host receipt. The
source runner is not an Instance executor. Keep live business-chain work in its
own authorized session and existing protected workflows/receipt validators.

Changes to host tools require their relevant behavior tests and development-tools
typecheck. Ordinary CI retains the exhaustive source gate; development-plan
freshness is selected separately for affected target-spec paths using explicit
PR base/head SHAs, as described in
[Development Plan Verification](#development-plan-verification).

### Framework Reference

The generic owner-slice process (S0-S7, implementation versus recovery/evidence
refresh RED/GREEN applicability, receipt reuse and parallel/serial criteria) is
owned by `RenDeHuang/opl-development-framework` at commit
`d995df3aaf6473a2b7714fc868327919bac4bbd4`:

- Skill: `skills/owner-slice-development/SKILL.md`
- Checklist: `skills/owner-slice-development/references/slice-checklist.md`
- Design: `docs/design/owner-slice-framework-v1.md`
- Wiring acceptance: `docs/design/wiring-acceptance.md`

This guide and the existing host entry retain Cloud wiring for explicitly
controlled runs. The framework's migration-task process is not an ordinary PR
prerequisite. Citing it or attaching tools restricts nothing by itself; restricted
adoption requires actual admitted-run evidence under its wiring-acceptance
criteria. The optional host export can assess that wiring and does not establish
that all Codex sessions are restricted.

### Host Source-Check Reference

An admitted host run retains its authoritative execution receipts in runtime
storage. When that run needs a repository evidence reference, the host may
export its current passed stage:

```bash
node tools/dev-session.ts source-check /absolute/host-store <run-id> <gate-id>
```

Export is optional for the host branch and is not an ordinary PR prerequisite.
The exporter writes
`docs/evidence/source-checks/<run-id>-<gate-id>-<attempt>.json` only from a current
passed stage and its recorded command. It refuses stale or absent execution
evidence and appends a new record without rewriting a previous attempt. This
host operation does not grant workers access to approval, run state or receipts.

The host exporter binds the actual base and changed content to its recorded
execution. Complete declared-input and upstream receipt-hash binding remains in
host stage receipts. Historical receipts stay unchanged and cannot serve as
current PASS evidence. The former PR-body checker is retired; host evidence never
substitutes for actual current-revision CI, review, merge authorization,
business-chain E2E or Instance acceptance.

### Scoped Acceptance Inputs

Restricted workers cannot write `AGENTS.md`, `DEV_GUIDE.md`, `.github/**`,
`tools/**`, `tests/tools/**`, `package.json` or the development plan; the
development-governance work is a host-owned reference lane
(`W27.development-governance`), not a worker grant. A bounded Console
presentation fix uses `W16.console-ui-fixes`; its phase record includes the
shared Console stylesheet and explicitly grants the listed existing console
test files through `testWritePaths` (exact files under
`tests/`, never a directory or a business owner) so the implementation and its
test stay in one owner scope.

A Go acceptance gate that can only be executed against a real PostgreSQL server
declares the host-owned fixture with `"database": "isolated-owner-postgres"` on
that gate. The host provisions one ephemeral container from the pinned compose
image with trust authentication, a loopback-only published port and, on Linux, a
host-created Unix-socket directory, and removes the container after the stage.
The sealed runner receives only that endpoint as
`OPL_OWNER_MIGRATION_TEST_ADMIN_DSN` with `OPL_POSTGRES_TESTS=1`; a Linux stage
runs in a private network namespace that cannot reach the host's published
port, so it connects through the bound socket directory. The runner validates
the endpoint shape and the socket-directory pairing before any execution;
provenance comes from the host entry being the only provisioner, which never
reads a DSN or socket directory from the worker or the invoking environment.
The declaration is part of the approved record, so the stage receipt's approval
hash binds which fixture the evidence came from.

A browser acceptance gate (`"kind": "browser"`) names its exact test targets
from the approved `test:browser:suite` inventory; a target outside that
inventory is refused before any browser is resolved. The host resolves one
fixed installed browser (macOS: `/Applications/Google Chrome.app`, Linux:
`/opt/google/chrome`), launches it inside the OS sandbox before any repository
test executes, and refuses absent installations instead of falling back to a
downloaded bundle: `PLAYWRIGHT_BROWSERS_PATH` is redirected to an absent
scratch path and browser downloads stay disabled. When a stage declares the
locked `package.json`/`package-lock.json`, the host materializes the approved
`node_modules` as a private writable scratch copy, so Vite's configuration and
dependency temporary writes stay in admitted scratch while the shared
installation and the source checkout are never granted. Zero registered tests,
failures, skips and TODOs all refuse PASS.

W20 owner source closeout uses `W20.workspace-deletion-source` for the existing
Workspace deletion/recovery files and `W20.deletion-grant-source` for Gateway's
accepted-deletion authorization files. These exact-file slices consume typed
source inputs and isolated owner tests, not completion receipts for the whole
purchase, upgrade and renewal program. Their receipts prove only source checks;
the full W20 dependencies and deletion/refund business acceptance remain intact.
The Workspace source run requires the Gateway source run's actual receipt;
the downstream serialized check consumes both on the same exact baseline.
Legacy cleanup still uses its original owner and the existing Cloud
chain PASS prerequisite; source checks never authorize production effects.

## Development Plan Verification

Ordinary exhaustive source checks do not compare the current migration task
book with generated projections. The PR `source` job uses its explicit
base and head SHAs to select the dedicated development-plan freshness check for
affected target-spec paths. Focused local selection uses the same scope classifier.
Plan verification tests exercise isolated fresh and stale projections; they do
not turn unrelated task-book edits into ordinary source-test failures.

The old PR-body checker and its command are retired. Host admission and execution
receipts remain available for explicitly controlled tasks. Adoption and remaining
qualification evidence are maintained in the canonical
[development governance outcome](docs/roadmap.md#development-governance-migration-2026-10-13).

## Pre-Commit Checks

```bash
npm run verify:local:focused -- --base origin/main
```

Focused verification is the normal local path. It requires an explicit base
ref, inspects committed, staged, unstaged, and untracked paths, and selects only
the affected checks. It does not claim that unrelated full-suite evidence ran.
Development/tool changes select development typecheck; generated-contract inputs
and outputs reuse the existing freshness inventory; plan changes select plan
freshness; `apps/` and `packages/` TypeScript changes select typecheck. Current
changed test files run directly. A source-only change must declare the existing
owner behavior targets from its acceptance scope, for example:

```bash
npm run verify:local:focused -- --base origin/main --test tests/tools/dev-session.test.ts
```

Repeated `--test` selects multiple targets; deleted tests are not executed, and
missing or nonexistent targets fail before running checks. This is an explicit
seam selection, not a guessed source-to-test dependency graph. The existing
`test:browser:suite` inventory identifies real browser targets, including those
without a browser filename suffix. Go source changes run the affected packages;
module metadata or non-Go assets run that affected module's suite. Node and Go
behavior evidence rejects zero executed tests, failures, skips and TODOs.
If an owner suite needs PostgreSQL or a provider, supply that real boundary's
validation environment; compilation or skipping is not a substitute. Combining
`--focused` with `--with-postgres` is rejected: the latter remains the explicit
exhaustive lane, not an implicit way to start unrelated integration suites. Unrelated
browser suites, builds and full repository tests are not selected automatically.
Git scope uses the merge base plus local index/worktree changes, not unrelated
upstream changes. Whitespace checks cover committed, staged and unstaged diffs.
The focused command outputs actual execution results. Record them in the ordinary
PR; no independent source-check JSON is required. Explicitly admitted host runs
retain their separate runtime receipts and may use
[Host Source-Check Reference](#host-source-check-reference) when needed. Focused
success proves only the selected checks, not downstream completeness,
business-chain E2E, Instance acceptance or production readiness.
The exhaustive source gate remains available for CI or explicit local rehearsal:

```bash
npm run verify:local
```

The same step inventory can be executed by group for CI or targeted rehearsal:

```bash
npm run verify:local -- --group source
npm run verify:local -- --group go
npm run verify:local -- --group browser --shard 1/2
npm run verify:local -- --group browser --shard 2/2
```

All groups are required for exhaustive evidence, and every browser shard must
succeed. A single group or shard is not the full gate. Group selection cannot be
combined with `--focused` or `--with-postgres`; `--shard` is valid only for the
browser group. The full command and focused change-scope selection remain
available independently.

The exhaustive gate needs no database. It validates the product boundary, Node
tests, Console typecheck/lint/build, the current Go module set and Git
whitespace. The module inventory is owned by
[implementation-architecture.md](docs/implementation-architecture.md), not a
fixed count in this guide. Go coverage means all-module compilation plus the
explicitly database-free package tests. Changes to persistence, capacity behavior, local
Docker, or a cross-service path require the relevant focused owner checks and,
when the database/provider boundary is affected, the explicit complete local gate:

```bash
npm run verify:local:full
```

The complete gate uses Docker to start an ephemeral PostgreSQL 16 container,
runs the PostgreSQL, capacity, and local-Docker integration tests with zero
skips, and removes the temporary container on exit. Neither gate accesses a
production network or dispatches an instance deployment.

The manually dispatched [Qualification workflow](.github/workflows/qualification.yml)
also runs the real Linux quota and first Local application path in its existing
`fabric` job. Each run owns a 12 GiB sparse ext4 image under the runner temporary
directory, explicitly enables project quotas, and checks available disk space
before mounting it. The existing hard-limit test must observe `EDQUOT` before
the first application test runs.

Fabric, Serve and both test executables are compiled by the ordinary runner.
Only quota-dependent execution uses `sudo`, with a cleared environment and
explicit isolated PostgreSQL, storage-root and precompiled-binary inputs. The
first application test uses real owner processes and Docker resources named and
cleaned up by that test. The job unmounts the filesystem and removes its temporary
image and executables on completion or failure. This is Linux source
qualification; it does not deploy an Instance or establish installed product
readiness. A Docker Desktop daemon does not qualify a macOS Fabric process's
filesystem, and fixture quota backends do not prove kernel enforcement.

The manual [Clean Host Qualification workflow](.github/workflows/clean-host-qualification.yml)
requires the intended exact Product commit and immutable Cloud Candidate image.
It accepts an explicit immutable Workspace image or builds only the existing
qualification Workspace fixture into a run-owned loopback registry. Cloud is
always pulled by the supplied digest. The current dispatch workflow and
[release procedure](docs/runtime/release.md) own that selection; a fixture result
does not establish production application or live Sub2API acceptance. Local
source-image construction remains a separate CLI option and cannot qualify an
existing published Candidate's bytes.

For generated-field changes, run `npm run verify:generated-contracts` to
regenerate the covered outputs from their declared source inputs and compare
exact bytes. For migration-plan inputs or projections, run
`npm run verify:development-plan` to regenerate the plan task
book into an isolated tree, compare exact task-book bytes and canonicalized plan
JSON with the checked-in projection and validate its coverage read-only; a stale or hand-edited plan projection is refused
instead of being treated as current state. CI runs this check separately for affected target-spec paths; see
[Development Plan Verification](#development-plan-verification).
Freshness is separate from production/spec reconciliation, W01
migration and real consumer verification; it does not freeze handwritten
business fields. See [contract ownership](packages/contracts/README.md).
Fixture and source passes are evidence only for the exercised layer, not
installed product runtime or Instance acceptance.

Whitepaper source or Profile changes additionally run `npm run build:whitepaper`;
rendering is separate from the ordinary source gate and from publication.
