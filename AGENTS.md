# OPL Cloud Development Rules

`one-person-lab-cloud` is the single GitHub product repository for OPL Cloud architecture,
Console/BFF, domain services, contracts, portable distribution, and reusable
release mechanisms. A domain is not a separate GitHub repository.

## Canonical Owners

- `docs/README.md` maps documentation topics to their canonical owners.
- `docs/architecture.md` and `docs/decisions.md` own target architecture and
  durable decisions.
- `docs/implementation-architecture.md`, source, schemas, and focused tests own
  current implementation facts.
- `docs/status.md` owns current evidence. `docs/roadmap.md` owns open gaps,
  priority, and acceptance outcomes.
- `packages/contracts` owns only machine-readable facts that have a current
  cross-module, public-interface, security, integrity, or irreversible-side-
  effect consumer.
- `opl-instance-medopl` owns the medopl domains, provider profile, production
  environment and Secrets, deployment, rollback, acceptance, and receipts.

`opl-cloud` is the product's package, image, service, namespace, and runner name.
The current repository identity is `gaofeng21cn/one-person-lab-cloud`, as declared
by the distribution contract. Archived repositories and Git history are
provenance, not separate current product writers. Instance,
Sub2API, and Framework remain separate authorities outside this consolidation.

## Reconcile Before Editing

Before implementation, name the objective, primary module owner, canonical
contract or document owner, real callers, exact write set, and completion
evidence.

The latest direct user decision sets product intent. Source, tests, documents,
Candidates, Releases, and runtime readback prove different layers; none of them
silently upgrades a lower-layer result into a production claim.

When current sources disagree:

1. Trace the relevant Git history, callers, contracts, and runtime readback.
2. Classify each statement as target, current implementation, runtime evidence,
   production evidence, history, derived, stale, or unknown.
3. Reconcile the decision in its canonical owner and remove the duplicate
   current writer in the same change.
4. Stop only when unresolved authority would change an irreversible production
   action or the requested terminal outcome.

Issues, PRs, comments, agent sessions, generated docs, and old plans are inputs.
They do not override the current owner.

## Ordinary Source Development

Follow the [2026-10-13 decision](docs/decisions.md#2026-10-13-ordinary-source-development-uses-git-scope-and-current-ci):
use an explicit Git change scope, relevant local behavior checks, a pull request
and actual CI results for its current revision. Canonical module and document
owners define responsibility. Ordinary changes do not require host admission,
phase-plan registration, a fixed governance record or a separate source-check
JSON. Record the problem, resulting behavior, actual verification and risks in
the PR using [the developer flow](DEV_GUIDE.md#ordinary-pull-request-flow).

An independent agent may perform technical review against the exact source head
and base. Production, money and resource changes retain owner review and explicit
authorization. Merge remains subject to the repository's actual branch protection
and existing merge authorization; review or a local pass grants no permission to
merge, publish or deploy. These instructions do not prove remote protection is
configured.

Independent write sets may share the current workspace; coordinate overlapping
changes. A new worktree or registered host run is not a routine prerequisite.

## Scoped Development Contract

Apply this contract only when a task explicitly selects controlled delegation.
The following admission, worker, runner and receipt requirements govern that
admitted host run, not ordinary source development.

The existing phase plan, including the work-package records in
`docs/spec/target/checks/development_plan.json`, supplies development tasks; it
is not a permanent owner or task registry. Stable source ownership remains in
the canonical documents mapped by `docs/README.md`. A plan or execution record
cannot redefine that ownership or create another business-field authority.

A trusted host admits each run using an authorization record stored outside the
repository or in ignored runtime storage, protected from worker writes. Admission
references an existing plan record and its canonical owner, narrows the read/write
scope, and declares exact stage inputs and a host-controlled runner with no
arbitrary shell interface. It does not introduce a new business-owner registry,
task schema, or repository-global current state. Scope expansion requires a new
host admission, not a worker edit to its plan or authorization. Read permissions
and acceptance targets are not write permissions. Concurrent workspace changes
require an explicitly named, host-admitted run on the same baseline and plan:
`coauthorRuns` names independent collaborators; `requires` names prerequisites
and their transitive dependency chain. The host verifies their exact write scopes
and current phase ownership, never authorizes every directory in a plan, and
never scans unrelated run or business-receipt bodies. Coexistence permission is
not completion evidence: prerequisites and acceptance still require actual
verified receipts; stale downstream evidence does not erase its previous edits
or prevent an upstream owner from repairing its own stage.

Use `npm run dev:tools` and `npm run dev:run` as the host integration entry points;
CLI arguments are finalized with the tool implementation. The worker interface
is limited to `dev_context`, `dev_read`, `dev_search`, `dev_write`, `dev_status`
and `dev_verify`. Status is a readback; verification is an acceptance request to
the trusted runner, not permission to execute an arbitrary command. There is no
worker `complete` or `publish` operation. The worker must receive admitted
context before file operations; reads, searches and writes stay inside that
scope. Every operation rechecks the current baseline and workspace change scope.
A failed context refresh or admission check revokes the previous admission; the
worker cannot continue using context from before the failure. Worker writes
cannot alter admission, runner policy, run state or receipts.
Run state advances only from actual execution results, never from a worker's
completion claim; a failed or blocked result remains failed or blocked.

The host materializes each stage's declared inputs into an isolated snapshot.
The runner cannot read undeclared repository code dependencies; declare those
inputs before admission rather than granting a repository mount or shell escape.
Recovery may reuse a stage only when its exact inputs and bound upstream evidence
are unchanged. Downstream evidence binds the upstream receipt hashes and actual
stage input hashes; a change invalidates only the affected stage and its
dependents. The host appends per-run receipts to host-owned runtime storage,
protected from worker writes; it never overwrites receipts or
automatically updates `docs/status.md`, `docs/roadmap.md`, or a global state file.
Canonical evidence summaries remain explicit owner-maintained changes.

The host's `dev:verify` runs an admitted stage and appends its runtime receipt to
host-owned storage. If this run needs a repository source-check reference, the
host may export actual results with
`node tools/dev-session.ts source-check <host-store> <run-id> <gate-id>` as described in
[Host Source-Check Reference](DEV_GUIDE.md#host-source-check-reference).
Export and record validation are optional for this branch of development; they
are not ordinary PR requirements and never replace current-revision CI.
Complete stage-input and dependency-hash binding remains in the host receipt.
Passing behavior evidence requires actual executed tests, a zero exit code and
zero failures, skips or TODOs; a passing label is not execution evidence.

Enforcement belongs at the trusted host entry, not in a particular model loop.
The host must require current context before file operations, validate the
baseline and change scope, and expose only the admitted operations to the
client. `runRestrictedWorker` is one replaceable reference adapter; an existing
chat may use the same host-owned entry without adopting that adapter. A chat
that retains unrestricted shell or filesystem access is not controlled merely
because these tools are attached. These rules and entry points do not establish
that all Codex sessions are restricted, that the rewritten interface is enabled,
or that remote branch protection is configured; those claims require actual host
and runner evidence.

For changes to these host tools, run their relevant behavior tests and
development-tools typecheck. Ordinary CI retains the exhaustive source gate;
plan freshness is selected separately for affected target-spec paths using the
PR's explicit base and head SHAs. See
[Development Plan Verification](DEV_GUIDE.md#development-plan-verification).
Fixture and source-check evidence do not establish product runtime, Candidate
qualification, Instance deployment or production readiness.

**Process-layer reference.** The generic owner-slice process — the S0–S7 step
shape, the implementation-vs-recovery/evidence-refresh applicability of RED
and GREEN, the receipt-reuse rules and the parallel/serial criterion — is
owned by the process repository `RenDeHuang/opl-development-framework` at
commit `d995df3aaf6473a2b7714fc868327919bac4bbd4` (skill
`skills/owner-slice-development/SKILL.md`, checklist
`skills/owner-slice-development/references/slice-checklist.md`, design
`docs/design/owner-slice-framework-v1.md`, wiring acceptance
`docs/design/wiring-acceptance.md`). This document, `DEV_GUIDE.md` and the
existing host entry retain the Cloud wiring for explicitly controlled delegation.
The framework's migration-task process is not an ordinary PR prerequisite.
Citing the framework or attaching its tools restricts nothing by itself;
restricted adoption needs real admitted-run execution evidence under its
wiring-acceptance criteria. Use the optional host export when assessing that
wiring. This does not establish that all Codex sessions are restricted.

## Documentation

Follow the hierarchy in `docs/README.md`. Lower layers may implement or report
an upper-layer decision; they do not redefine it.

Keep active documentation limited to current truth, open gaps, and reusable
operations. Completed plans, freezes, task checklists, shell transcripts, and
closeout notes belong in Git history or `docs/history/**` when provenance is
still useful.

Machine contracts contain stable inter-owner facts, not implementation layout,
query strategy, worker tuning, presentation details, current progress, or a
catalog of retired alternatives. Tests exercise the owning behavior or current
consumer instead of restating contract JSON or proving retired files remain
absent.

## Module Ownership

The target architecture is the domain-separated Agent SaaS architecture adopted
in [docs/decisions.md](docs/decisions.md) on 2026-09-22 and specified by the
target architecture development specification under
[docs/spec/target](docs/spec/target/00_master_index.md). The target domains are
`tenant` (CloudIdentity), `capability`, `build`, `workspace`,
`runtime_control`, `resource_catalog`, `gateway` (Gateway Integration),
`fabric`, and `ledger`, surfaced through `apps/console-ui` and a Console BFF.
Each domain owns its own data and writes; cross-owner references use opaque
identifiers only. The canonical in-repository directory and deployment-unit map
is [Repository And Instance Topology](docs/architecture.md#repository-and-instance-topology).

The current implementation has both extracted owner processes and retained
Control Plane callers. [Implementation architecture](docs/implementation-architecture.md)
owns their source paths and request map; the target work packages own remaining
migration and qualification outcomes.

| Module | Owns |
| --- | --- |
| `apps/console-ui` | Presentation; migrated cloud surfaces call Console BFF, while retained callers use Control Plane APIs |
| `apps/console-bff` | Same-origin browser REST, session security and typed owner aggregation; no business persistence |
| `services/gateway-integration` | CloudIdentity/Tenant and Gateway integration, with distinct data owners in one deployment unit |
| `services/capability`, `services/build`, `services/runtime-control` | Package/catalog facts, immutable OCI build facts, approved Runtime Release references respectively |
| `services/workspace`, `services/resource-catalog`, `services/serve` | Workspace business operations, approved plans/pricing policies, application delivery/readiness/access respectively |
| `services/control-plane` | Retained sessions, account policy, Workspace lifecycle, settlement coordination and customer DTOs until their callers migrate |
| `services/fabric` | Provider-neutral compute, storage, attachment, Secret binding, Runtime facts, provider adapters |
| `services/ledger` | Receipts, evidence, retention, reconciliation, opaque provenance |
| `services/internal` | Policy-free infrastructure shared by at least two current services |

- Under the target architecture, each business service has an independent Go
  module and process inside this repository. Each data owner has its own
  PostgreSQL database and roles. CloudIdentity (`tenant`) and Gateway Integration
  (`gateway`) are distinct data owners in the same Gateway Integration module
  and deployment unit; neither each domain name nor each proto service creates
  another process. Extracted owners use typed gRPC/protobuf; migrated cloud
  Console surfaces use the BFF REST API. Retained Control Plane, Fabric, and Ledger
  callers keep their current module, process, and typed HTTP boundaries.
- Control Plane is a migration source, not a permanent second writer alongside
  the extracted services. Move a capability and its real callers together, then
  retire its old write path while preserving historical obligations.
- Console's `cloud` identity uses `apps/console-bff` for authentication,
  Workspace, Publisher, delivery and wallet/model-catalog reads; other
  unmigrated Gateway/Billing panels still use Control Plane APIs. `legacy` retains its current
  Control Plane authentication and Workspace APIs. The identity is baked into
  the image and declared by its label and installation topology, not changed at
  runtime.
- Keep one shared contracts Go module at `packages/contracts/go/go.mod`. W01
  places production proto sources in `packages/contracts/proto/` and generated
  Go bindings under the existing contracts module, not another module. Internal
  consumers build from the same Cloud commit, record the schema hash, and lock
  generator versions. Necessary consumer `go.mod`/`go.sum` updates belong to
  that contract change; do not add a module solely to avoid those updates.
- Provider-specific behavior stays behind the owning Fabric adapter.
- Console does not own persistence, provider calls, billing decisions, or
  Fabric/Ledger/Sub2API state. Control Plane does not own the wallet, provider
  resources, or another service's tables. Fabric does not own customer balance
  or Ledger evidence. Ledger does not own provider mutation or Workspace
  orchestration.
- DTOs, reducers, state machines, retry policy, and authority facts have one
  owner. Other modules use a thin typed client or adapter.
- A shared module requires at least two current callers and remains policy-free.
- Cross-module changes update the owning contract, both consumers, and focused
  boundary tests together.

Development with distinct owners and write sets may proceed in parallel. Changes
to the same file, one shared contract revision, canonical `main`, or production
state are serialized.

Cross-service coordination is typed gRPC/protobuf plus owner readback under the
target architecture, with per-domain PostgreSQL Outbox delivery where a
reliable event is required; the un-migrated services still coordinate over typed
HTTP. Do not add a framework, service, shared policy layer, durable workflow
engine, or global event bus beyond the domains and mechanisms the adopted target
defines. A new service still needs a current caller, an observed missing
capability, a bounded migration, and an owner. The target architecture does not
adopt Spring Modulith, Dapr, Temporal, or a second Cordis runtime.

## Implementation

- Reproduce or trace the real path before fixing a bug. Once the deepest
  breakpoint and owner are known, repair that owner before expanding tests or
  process.
- Prefer the direct path inside the current owner. A new file, abstraction,
  dependency, state, fallback, compatibility path, workflow, or gate needs a
  current caller, contract, observed failure, or concrete reachable risk.
- Improve cohesion inside the existing module before adding a framework or
  service. Adopt a new runtime or architectural dependency only when a measured
  missing capability and a focused replacement path justify it.
- Refactor one live capability at a time, preserve its public behavior and
  persisted data obligations, switch real callers, then remove the retired path.
- Keep source, tests, `docs/status.md`, and `docs/roadmap.md` aligned at their
  respective evidence layers.

## Release And Instance Boundary

Cloud publishes portable GHCR images, GitHub Releases, installation assets, and
reusable provider adapters. It does not own an instance deployment workflow or
require a production environment to build the product.

During pre-1.0, build a replaceable candidate from an exact Cloud SHA and image
digest on a branch admitted by the Candidate workflow. Formal publication still
requires that exact source to be merged into canonical `main`. The instance owner
deploys and qualifies that candidate in its protected environment. A formal Product Release promotes the same qualified
bytes without a rebuild.

Candidate construction and local development do not authorize formal
publication or Instance deployment. The Release workflow admits qualification
evidence and promotes the existing Candidate digest without rebuilding it.
Required Local and Instance receipts must bind that exact Candidate; see
`docs/runtime/release.md` for publication and recovery procedures.

Only the repository owner and `RenDeHuang` may dispatch the manual Cloud Release
workflow from `main`, and the original publisher must remain the triggering
actor. Development, CI, qualification retries, and instance-only work do not
authorize publication.

## Verification

- Run focused checks first with `npm run verify:local:focused -- --base <verified-base-ref>`. The command selects checks from the explicit Git change scope; it never guesses the base. Declare existing owner behavior targets with repeated `--test <file>` when no current test changed; missing targets are refused rather than falling back to full tests or treating typecheck as behavior evidence. Generated checks use the existing generator input/output inventory, E2E classification uses the existing browser suite inventory, and Go changes run tests for the affected packages. Focused test evidence requires actual executed tests with zero failures, skips or TODOs; it is not host acceptance or proof of complete downstream coverage.
- `npm run verify:local` is the exhaustive source/CI gate, not a required local precondition for every change. CI keeps this exhaustive gate; local work should not repeat it unless the boundary requires it or the developer explicitly asks.
- Use `npm run verify:local:full` only for persistence, schema, retained service
  behavior, cross-module contracts, or structural changes involving PostgreSQL,
  capacity, or Local-Docker behavior, and only when that boundary is affected.
- Tests and builds prove their own layer. Instance adoption and production state
  require owner-authoritative deployment and runtime readback.
- Before changing billing, Workspace, Fabric, Ledger, Gateway, deployment, or
  E2E, read the owning architecture/invariant sections and current machine
  contract, then update the source owner and its evidence projection together.

## High-Consequence Boundaries

- Production-private endpoints, clusters, databases, and services are accessed
  only through `opl-instance-medopl` protected workflows and authorized runners.
- The local development machine must never access the production private
  network directly; this repository must not dispatch Instance deployment.
- Sub2API remains the spendable-wallet and Gateway backend authority.
- Do not create a second wallet, Gateway, package registry, lock, or domain
  authority in Cloud.
- Customer and verification compute/storage procurement uses the approved
  prepaid monthly policy.
- Never use `POSTPAID_BY_HOUR` for customer or verification CVM/CBS resources.
- Ordinary CI, release, and E2E are read-only for real customer money and
  provider resources. Real charges, purchases, renewals, or deletion require a
  separate explicit authorization and bounded owner workflow.
- Ordinary CI, release, or E2E must not buy or delete Tencent CVM/CBS resources,
  charge a real monthly product fee, add a public test-billing mode, or clean up
  customer resources from verification code.
- Secrets remain in their approved secret stores and authorized runtime
  boundaries.

<!-- CODEGRAPH_START -->
## CodeGraph

- This repository uses a local `.codegraph/` index; do not commit it.
- Use CodeGraph for definitions, callers, impact, and paths; use `rg` for literal
  text.
- Run `codegraph init .` or `codegraph sync .` when the index is missing or stale.
<!-- CODEGRAPH_END -->

<!-- TESTING_RULES_START -->
## Testing Rules

- Use existing typed owner DTOs and structs for test input and assertions.
- Shared types belong in `packages/contracts/go/` only when current cross-owner consumers require them; internal test cases do not justify a new shared contract.
- At a genuinely generic persistence or untrusted JSON boundary, test the real decoder and its rejection behavior without inventing a parallel domain schema.
- When production types change, update affected tests to the current typed contract.
- Do NOT add compatibility branches, extra fields, or defensive checks in production code just to make old tests pass.
- If a type definition changes, update all consumers (including tests) in the same commit.

## Before Writing Code

- Use `codegraph explore "<symbol>"` to understand blast radius and covering tests before modifying any shared type or function.
- Run `codegraph index` after pulling latest main to keep the index current.
<!-- TESTING_RULES_END -->

<!-- RECEIPT_RULES_START -->
## Receipt And Evidence Persistence

For ordinary source changes, persist actual local verification in the PR and
retain CI results for the current revision; no separate source-check JSON is
required. A commit alone does not prove verification. Admitted host runs retain
host-owned execution receipts. Deployment and qualification retain their
required owner receipts. Failed or unavailable verification records the attempted
action and reason rather than implying pass.

Cloud owns source evidence and roadmap state. Instance owns immutable
Candidate, launch/use, deployment, rollback and provider qualification receipts
under its `receipts/` surface; use its current validators and schemas instead of
copying an Instance receipt format into this file. Bind applicable source SHA,
image digests, timestamps, workflow provenance and actual focused readback.
Append a new receipt for a new action and never overwrite a prior receipt.
Receipts contain no credentials, raw Keys, customer data or private addresses.

When a roadmap outcome completes on the Cloud side, update its state and
reference the owner evidence in the same change. Required Instance evidence
remains an explicit external obligation; Cloud-only success cannot mark the
combined outcome complete.
<!-- RECEIPT_RULES_END -->

- GitHub 上自己新建的对外文本用英文书写：commit subject/body、PR 标题与正文、Issue、comment、Release 正文与 Release Notes。产品名、代码标识、路径、命令与原始引用除外。他人写的 Issue、PR 或 comment，无论对方用什么语言，回复沿用对方的语言；历史中已有的非英文 commit 保持原样。
