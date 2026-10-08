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
existing plan's owner write declarations, and returns the complete set of ready
gates. This is a baseline readback, not a repository-wide source scan: Git
metadata establishes the change set, while source and receipts are read only
through the admitted paths and declared dependencies.

Receipt lookup reads only the named development gate's attempt sequence and
verifies its signature and identity. It does not enumerate and parse business
receipt bodies. Development receipts prove source checks; business and Instance
receipts stay in their existing owner formats and qualification paths. Neither
layer is accepted as evidence of completion of the other.

## Pre-Commit Checks

```bash
npm run verify:local
```

The default gate needs no database. It validates the product boundary, Node
tests, Console typecheck/lint/build, the current Go module set and Git
whitespace. The module inventory is owned by
[implementation-architecture.md](docs/implementation-architecture.md), not a
fixed count in this guide. Go coverage means all-module compilation plus the
explicitly database-free package tests. Changes to persistence, capacity behavior, local
Docker, or a cross-service path also run the complete local gate:

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
book into an isolated tree, byte-compare the checked-in projection and validate
its coverage read-only; a stale or hand-edited plan projection is refused
instead of being treated as current state. Freshness is separate from production/spec reconciliation, W01
migration and real consumer verification; it does not freeze handwritten
business fields. See [contract ownership](packages/contracts/README.md).
Fixture and source passes are evidence only for the exercised layer, not
installed product runtime or Instance acceptance.

Whitepaper source or Profile changes additionally run `npm run build:whitepaper`;
rendering is separate from the ordinary source gate and from publication.
