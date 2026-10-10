# OPL Cloud Developer Guide

## Product Boundary

OPL Cloud develops and releases one portable product: Console, Control Plane,
Fabric, Ledger, and Workspace delivery. It publishes reusable source,
multi-architecture images, Compose assets, and GitHub Releases; it does not
deploy a concrete customer instance.

`opl-instance-medopl` is the separate owner for the medopl instance profile. It
explicitly binds the `.com` domains, Tencent/TKE profile, immutable Workspace
image, production configuration and Secrets, deployment, verification,
rollback, and receipts while consuming an immutable OPL Cloud product SHA and
image digest. Cloud source must not provide those instance defaults.

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

## Scoped Host And Worker Entry

Follow [Scoped Development Contract](AGENTS.md#scoped-development-contract) for
per-run admission, restricted worker capabilities, isolated stage inputs and
append-only execution receipts. The trusted host stores approval and run receipts
outside the repository or in ignored runtime storage protected from worker
writes; only the host creates them from admitted scope and actual execution.
Current commands are:

```bash
npm run dev:approve -- /absolute/host-approval.json /absolute/host-store
npm run dev:context -- /absolute/host-store run-id
npm run dev:tools -- /absolute/host-store run-id
npm run dev:run -- /absolute/host-store run-id https://model.example/v1/chat/completions model-id
npm run dev:verify -- /absolute/host-store run-id gate-id
npm run verify:dev-scope -- /absolute/host-store run-id
```

`dev:run` is an optional reference model adapter that launches a separate worker
with only the six admitted tools. The hard entry contract is the host-owned
`dev:context`/`dev:tools` path: an existing development client may use that path
without adopting this adapter. Attaching `dev:tools` to a shell-enabled chat
does not restrict that chat. The source runner is not an Instance executor and
does not consume an Instance receipt as source completion. Keep live
business-chain work in its current owner session; its protected workflows and
existing receipt validators remain authoritative.
Multiple checkouts are allowed: bind evidence to exact source and input hashes,
not a directory name or a thread's completion claim. Do not discard ongoing
owner work or rerun accepted production steps merely to adopt this entry.

The host references an existing phase-plan record and canonical owner, narrows
scope and declares stage inputs and the runner. A worker requests acceptance
with `dev_verify` and observes actual results with `dev_status`; it cannot
complete or publish a run by assertion. The host's `dev:verify` executes the
admitted stage and appends a runtime receipt to the host store; it does not
automatically create a PR source-check record. A shell-enabled chat remains
unrestricted. Run receipts do not automatically write product status or roadmap.

The host refreshes admitted context before every model turn. Status is computed
from the current declared input hashes and bound predecessor receipt hashes,
not stored as an agent-editable completion field. A new session or model can
continue the same run without restarting valid stages; an actual input or bound evidence change
invalidates only the affected stage and its dependents. A refresh failure stops
the worker rather than continuing with an old admission.

Every context admission also reads `AGENTS.md`, this guide, `docs/status.md` and
`docs/roadmap.md`, verifies that the approved base SHA is available and remains
an ancestor of the checkout, checks the current Git change scope against the
host-admitted write scopes, and returns the complete set of ready gates. This is a
baseline readback, not a repository-wide source scan: Git
metadata establishes the change set, while source and receipts are read only
through the admitted paths and declared dependencies.

Every file operation rechecks that scope. Failure revokes admission until a
successful `dev_context`; a read-only path or an acceptance target never grants
write authority. For a shared checkout, the host lists independent collaborators
in the optional `coauthorRuns` array of exact run IDs; prerequisite run IDs come
from `requires`, including its named transitive chain. Each contributor must use
the same base SHA and plan and retain current host admission and phase scope.
An unreferenced run is not discovered by scanning the host store. These references
permit existing authorized changes to coexist, not task completion or a bypass
of prerequisite receipt checks on writes and acceptance. Disjoint checkouts need
no coauthor references until their changes are integrated on an admitted baseline.

Receipt lookup reads only the named development gate's attempt sequence and
verifies its identity, input hashes and bound dependency hashes. It does not
enumerate and parse business receipt bodies. Development receipts prove source
checks; business and Instance receipts stay in their existing owner formats and
qualification paths. Neither
layer is accepted as evidence of completion of the other.

## Pull Request Governance Record

Every pull request carries a governance record in its description. The required
sections, their exact structure and the terminal states are machine-enforced by
`npm run check:pr-body` and by the
`governance` job in
[pull request CI](.github/workflows/pull-request-ci.yml). A missing, duplicated
or empty section, an incomplete base SHA, an owner that does not own the
declared phase record, a changed path outside the declared write set, an untyped
or inconsistent receipt entry, stale changed-content hashes, or a
completion claim without executed evidence and closed acceptance criteria is
refused; a changed path without a declared phase record fails the
same way instead of being grandfathered in.

```bash
npm run check:pr-body -- --body-file /absolute/pr-body.md --base origin/main
```

The workflow is real local focused execution results, host-generated PR
source-check evidence, PR record and freshness checks, actual CI `validate`
execution for the current revision, a recorded technical review, then a
separately authorized merge. The review is user-controlled: a user-authorized
reviewer examines and records it against the exact source head and base,
covering SSOT/owner/write-set compliance, the real focused behavior evidence
and the fresh host source-check; when the contributor account authored the pull
request the review is recorded as a COMMENT review rather than a
self-approval, and no specific GitHub username is required. Merge still
requires actual successful current-revision CI, the repository's actual
protection requirements and merge authorization by a separately authorized
actor. Updating the revision requires current CI results and fresh evidence for
changed inputs; editing the PR body reruns record validation.

The host owns approval and authoritative run receipts in runtime storage. After
`dev:verify` records a passed stage in the approved run, the host exports exactly
that executed result as the PR source-check receipt:

```bash
node tools/dev-session.ts source-check /absolute/host-store <run-id> <gate-id>
```

The exporter refuses a run, gate or gate state without a recorded passed stage
receipt, writes `docs/evidence/source-checks/<run-id>-<gate-id>-<attempt>.json` from the
current change scope and stage verification fields, and never accepts a
hand-written `result: pass`. Neither the focused command nor `dev:verify`
creates the PR record by itself; only this host exit does, and it is a host
operation outside worker scope. Its repository reference does not grant workers
permission to write host approval, run state or receipts. The current checker
owns validation of its format, not a separately hand-maintained schema in this
guide. Source-check validation covers the complete
base SHA, actual write set, corresponding changed-content hashes and execution
summary. Owner and phase are constrained by PR context and host approval, not
validated as source-check receipt fields. Complete declared-input and dependency
receipt-hash binding belongs to host stage receipts; a PR source-check alone does
not provide it. Every actual changed path is bound to its content hash; the
current receipt's own path is the sole self-hash exclusion. A deleted path uses
the explicit `deleted` marker, checked against the actual Git deletion.
Execution evidence records the command actually run, its exit code, actual test
counts and the SHA-256 digest of captured output. Passing behavior evidence
requires a zero exit code, a positive count of registered tests actually run and
zero failures, skips or TODOs; a `result: pass` label, a planned command or
unchecked acceptance criteria cannot establish completion. Each attempt appends
new evidence rather than rewriting an earlier result. Historical receipts are
neither migrated nor overwritten and cannot serve as current PASS evidence.

The record separates the evidence layers instead of merging them. Declare only
applicable receipts, and retain failed or unavailable execution as such:

| Receipt entry | Names | Layer it proves |
| --- | --- | --- |
| `source-check` | a host-generated source-check reference in the format accepted by the current checker | The recorded source execution for the bound base, actual write set and changed contents; no complete stage-input or dependency binding |
| `business` / `instance` | the existing owner receipt reference | The business or Instance layer in its own format; never a substitute for source evidence |

`Terminal state: merge-ready` records closed acceptance criteria and passed
declared receipts; it requires no additional development-stage acceptance.
`source-complete` records only the exercised source layer, not business-chain
E2E, Instance qualification or production readiness. Neither label grants merge
permission. `blocked` carries open obligations and preserves failed or pending
evidence instead of claiming success.

Record validity and merge eligibility are separate. The `governance` CI job
executes `tools/check-pr-governance.ts` from the PR **base commit** in `record`
mode, with head treated only as inspected data. If the base has no checker, the
job fails closed; it never executes a PR head checker or grants an actor
exception. The existing `validate` job independently runs its full source
commands against the checked-out current revision, including the changed
implementation and its tests. A local receipt cannot substitute for that actual
CI execution. The record checker establishes record validity and freshness,
not the provenance of a claimed execution or permission to merge.

When the base checker still requires the retired acceptance gate for a
`merge-ready` label, use `source-complete` with genuine executed source evidence
for this governance transition. Keep the base checker in record mode; do not
manufacture a retired acceptance receipt or switch to the head checker to get a
passing record. Actual CI and the existing review/merge rules still decide
whether that source revision can merge.

Review and merge authorization remain user-controlled: a user-authorized
reviewer records the technical review against the exact source head and base
(a COMMENT review rather than self-approval when the contributor account
authored the pull request), and a separately authorized actor performs the
merge under the repository's actual protection requirements; no specific
GitHub username is required. Merge requires actual successful CI results for
the current revision and the applicable review rules; a valid record alone is
insufficient. These workflow checks do not establish that remote branch
protection is configured. The workflow uses `pull_request`, never
`pull_request_target`, and reads no secrets.

Restricted workers cannot write `AGENTS.md`, `DEV_GUIDE.md`, `.github/**`,
`tools/**`, `tests/tools/**`, `package.json` or the development plan; the
development-governance work is a host-owned reference lane
(`W27.development-governance`), not a worker grant. A bounded Console
presentation fix uses `W16.console-ui-fixes`; its phase record grants the two
existing console test files through `testWritePaths` (exact files under
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
The focused command outputs actual execution results; it does not automatically
write a source-check receipt. The host's `dev:verify` separately runs an admitted
stage and appends a runtime receipt to the host store; that receipt records
the command line that actually executed and the executed cwd, and the exporter
publishes the recorded command while refusing a historical receipt that never
captured one. For a PR, the host generates the source-check from that executed
record with
`node tools/dev-session.ts source-check <host-store> <run-id> <gate-id>`. Do not
hand-write a passing result or treat a runtime receipt as that PR record. Focused success proves only the selected checks, not
downstream
completeness, business-chain E2E, Instance acceptance or production readiness.
The exhaustive source gate remains available for CI or explicit local rehearsal:

```bash
npm run verify:local
```

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

For generated-field changes, run `npm run verify:generated-contracts` to
regenerate the covered outputs from their declared source inputs and compare
exact bytes. Run `npm run verify:development-plan` to regenerate the plan task
book into an isolated tree, compare exact task-book bytes and canonicalized plan
JSON with the checked-in projection and validate its coverage read-only; a stale or hand-edited plan projection is refused
instead of being treated as current state. Freshness is separate from production/spec reconciliation, W01
migration and real consumer verification; it does not freeze handwritten
business fields. See [contract ownership](packages/contracts/README.md).
Fixture and source passes are evidence only for the exercised layer, not
installed product runtime or Instance acceptance.

Whitepaper source or Profile changes additionally run `npm run build:whitepaper`;
rendering is separate from the ordinary source gate and from publication.
