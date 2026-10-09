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
signed receipts. The trusted host creates the approval JSON and signing store
outside the repository; the worker cannot edit either. Current commands are:

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
complete or publish a run by assertion. A shell-enabled chat remains
unrestricted. Run receipts do not automatically write product status or roadmap.

The host refreshes admitted context before every model turn. Status is computed
from the current declared inputs and signed predecessor evidence, not stored as
an agent-editable completion field. A new session or model can continue the same
run without restarting valid stages; an actual input or bound evidence change
invalidates only the affected stage and its dependents. A refresh failure stops
the worker rather than continuing with an old admission.

Every context admission also reads `AGENTS.md`, this guide, `docs/status.md` and
`docs/roadmap.md`, verifies that the approved base SHA is available and remains
an ancestor of the checkout, checks the current Git change scope against the
host-signed write scopes, and returns the complete set of ready gates. This is a
baseline readback, not a repository-wide source scan: Git
metadata establishes the change set, while source and receipts are read only
through the admitted paths and declared dependencies.

Every file operation rechecks that scope. Failure revokes admission until a
successful `dev_context`; a read-only path or an acceptance target never grants
write authority. For a shared checkout, the host lists independent collaborators
in the optional `coauthorRuns` array of exact run IDs; prerequisite run IDs come
from `requires`, including its named transitive chain. Each contributor must use
the same base SHA and plan and retain a valid signature and current phase scope.
An unreferenced run is not discovered by scanning the host store. These references
permit existing authorized changes to coexist, not task completion or a bypass
of prerequisite receipt checks on writes and acceptance. Disjoint checkouts need
no coauthor references until their changes are integrated on an admitted baseline.

Receipt lookup reads only the named development gate's attempt sequence and
verifies its signature and identity. It does not enumerate and parse business
receipt bodies. Development receipts prove source checks; business and Instance
receipts stay in their existing owner formats and qualification paths. Neither
layer is accepted as evidence of completion of the other.

## Pull Request Governance Record

Every pull request except an automated dependency bump carries a governance
record in its description. The required sections, their exact structure and the
terminal states are machine-enforced by `npm run check:pr-body` and by the
`governance` job in
[pull request CI](.github/workflows/pull-request-ci.yml). A missing, duplicated
or empty section, an incomplete base SHA, an owner that does not own the
declared phase record, a changed path outside the declared write set, an untyped
or inconsistent receipt entry, or a terminal state without the required
evidence is refused; a changed path without a declared phase record fails the
same way instead of being grandfathered in.

```bash
npm run check:pr-body -- --body-file /absolute/pr-body.md --base origin/main
```

The record separates the evidence layers instead of merging them:

| Receipt entry | Names | Layer it proves |
| --- | --- | --- |
| `source-check` | an in-repo `docs/evidence/source-checks/*.json` receipt with `evidenceLayer: source`, `result: pass`, its `sourceBaseSha` and its write set | Executed source checks for the declared base and write set |
| `development-stage` | the host store identity `runs/<run>/receipts/<gate>-<attempt>.json` plus `run`, `gate`, `attempt` and the signed receipt digest | Host acceptance: the signed payload binds approval, phase, declared inputs, dependencies, runner and output digest |
| `business` / `instance` | the existing owner receipt reference | The business or Instance layer in its own format; never a substitute for source evidence |

`Terminal state: merge-ready` requires every declared receipt to have passed and
a verified host `development-stage` receipt; a pull request body, an agent
claim, a business receipt or a self-signed file never provides that.
`source-complete` claims the source layer only and is not a merge
authorization, and `blocked` carries open obligations.

Record compliance and merge authorization are separate machine checks. The
`governance` CI job runs the checker in `record` mode; the existing `validate`
job runs the same checker in `merge` mode as a step, so an unverified
merge-ready claim fails that job instead of appearing as a skipped `needs`
dependency. Both steps run the checker from the pull request's **base**
revision and inspect the head revision only as reviewed data, so a pull request
cannot weaken the check that judges it by editing the checker.

The first landing of this mechanism is bootstrap, and the condition is purely
physical: the pull request whose base revision lacks
`tools/check-pr-governance.ts`. There, `validate` runs the head copy with
`--mode merge --bootstrap-base <base worktree>` and the checker itself
downgrades that one run to `record` enforcement, reporting
`effectiveMode: "record"` and `bootstrap: true`, with an explicit `::warning`
that merge authorization is not enforced remotely for this first landing and
**is** enforced from the next pull request. A base revision that carries the
checker always keeps full merge enforcement; no actor, user or label may
downgrade it. The workflow uses `pull_request`, never `pull_request_target`,
and reads no secrets.

The merge step trusts one host authentication root, supplied by the repository
variable `OPL_DEVELOPMENT_AUTHORITY_PUBLIC_KEY`: either the SPKI PEM public key
or its base64 text (for example `base64 -i <host-store>/authority.pub`). The
value is never taken from the pull request, a pull-request-provided key or a
repository file. A `development-stage` entry may name a trusted local store
with `--host-store <absolute path>` or an in-repo public proof with
`proof: docs/evidence/development-stage/<name>.json`, whose content is
`schemaVersion: 1`, `kind: opl.development.stage.proof.v1`, and the signed
`approval` and `receipt` envelopes (`payload` + `signature`) produced by
`approveRun` and a passed `verifyGate`. The checker validates both signatures,
the approval/receipt identity, the approved phase and write scope, the declared
owner, the bound base SHA, the phase and input fingerprints, the runner
identity, the receipt digest and every dependency receipt hash. The receipt's
`sourceSha` may precede the reviewed head only by proof-only commits; any other
post-receipt change is refused. `merge-ready` without a configured trust root
or a verifyable proof is refused, and no actor receives a blanket exemption.

The development host (a shell-enabled session or CI runner holding the trusted
store) produces that proof after the candidate commit: approve and verify the
run at the candidate, export the two signed envelopes into the proof file,
commit the proof-only successor, then rewrite the body to `merge-ready` with the
`development-stage` entry. The receipt binds the candidate `sourceSha`; the
proof file is the only successor change allowed. The host-source-check receipt
for this governance change records the local source runs and does not
substitute for that host acceptance.

Restricted workers cannot write `AGENTS.md`, `DEV_GUIDE.md`, `.github/**`,
`tools/**`, `tests/tools/**`, `package.json` or the development plan; the
development-governance work is a host-owned reference lane
(`W27.development-governance`), not a worker grant. A bounded Console
presentation fix uses `W16.console-ui-fixes`; its phase record grants the two
existing console test files through `testWritePaths` (exact files under
`tests/`, never a directory or a business owner) so the implementation and its
test stay in one owner scope.

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
Focused success proves only the selected checks, not downstream completeness,
host receipt acceptance, business-chain E2E or production readiness.
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
