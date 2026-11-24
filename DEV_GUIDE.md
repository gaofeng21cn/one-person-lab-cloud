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

For an actual installation, use only the assets from one GitHub Release (`v0.1.8`
for the current public release, or a current Candidate bundle) and replace template values as described in
[installation.md](docs/installation.md). Do not mix assets across releases. A healthy Compose stack proves only that the Cloud control services
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

The [ordinary development policy](docs/decisions.md#ordinary-source-development-and-ci)
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
   `main` branch protection prevents direct pushes rather than owner self-merges:
   when the owner has authorized landing changes to `main`, verify live branch
   protection and PR mergeability, wait for all required checks to succeed
   (`mergeStateStatus: CLEAN`), and when `required_approving_review_count` is
   `0`, complete the merge with normal `gh pr merge --delete-branch` (without
   bypassing protection) and fast-forward local `main` to `origin/main`.

[Pull Request CI](.github/workflows/pull-request-ci.yml) runs dependency review
alongside the `source`, `go` and two `browser` shards on independent runners.
They select groups from the same exhaustive `npm run verify:local` step list;
the ungrouped local command still executes every step. Browser shards partition
the existing package-script file inventory through Node's test runner; they keep
at most three concurrent files per runner and preserve each test's isolated
fixtures.
`source` also checks committed whitespace. The existing `validate` check always
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

## Pre-Commit Checks

```bash
npm run verify:local:focused -- --base origin/main
```

Focused verification is the normal local path. It requires an explicit base
ref, inspects committed, staged, unstaged, and untracked paths, and selects only
the affected checks. It does not claim that unrelated full-suite evidence ran.
Development/tool changes select development typecheck; generated-contract inputs
and outputs reuse the existing freshness inventory; `apps/` and `packages/`
TypeScript changes select typecheck. Current changed test files run directly.
A source-only change must declare the existing owner behavior targets from its
acceptance scope, for example:

```bash
npm run verify:local:focused -- --base origin/main --test tests/tools/verify-local.test.ts
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
PR; no separate source-check JSON is required. Focused success proves only the
selected checks, not downstream completeness, business-chain E2E, Instance
acceptance or production readiness.

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

The [Composed Backend Runtime Gate](.github/workflows/composed-backend-runtime-gate.yml)
runs the composed backend's runtime suite on the pull requests and `main` pushes
whose changes can break that claim: the product image, the published
installation, the owner topology contract, the suite itself and the gate. The
suite builds the product image and starts several containers, so it runs in its
own workflow and leaves Pull Request CI at its present speed. Each run owns a
12 GiB sparse ext4 image under the runner temporary directory, mounts it with
project quota the way Clean Host Qualification does, and unmounts and removes it
on completion or failure. The gate uses no secret, reaches no cluster and no
customer resource, and its failing suite fails the build. The gate proves the
runtime layer of the composed unit; it does not prove production-cluster
behaviour, downstream adoption, the customer lifecycle, or any savings or
high-availability claim.

For generated-field changes, run `npm run verify:generated-contracts` to
regenerate the covered outputs from their declared source inputs and compare
exact bytes. Freshness is separate from production/spec reconciliation and real
consumer verification; it does not freeze handwritten business fields. See
[contract ownership](packages/contracts/README.md).
Fixture and source passes are evidence only for the exercised layer, not
installed product runtime or Instance acceptance.

Whitepaper source or Profile changes additionally run `npm run build:whitepaper`;
rendering is separate from the ordinary source gate and from publication.
