# OPL Cloud Documentation

This index owns the documentation hierarchy. It follows OPL Doc's semantic
governance model: one current owner per topic, current truth separated from
active gaps, support detail and history.

## Hierarchy

| Level | Question | Canonical owner | Change rate |
| --- | --- | --- | --- |
| 1. Product scope | What does this repository own? | [project.md](./project.md) | Long term |
| Public vision | Why would a user adopt Cloud? | [whitepaper](./whitepapers/opl-cloud-whitepaper.md); root READMEs are entry summaries | Long term |
| 2. Target architecture | What should the product become and who owns each authority? | [architecture.md](./architecture.md) and durable [decisions.md](./decisions.md) | Long term |
| 3. Durable invariants | Which safety, integrity and ownership facts must survive refactors? | [invariants.md](./invariants.md) and eligible machine contracts | Infrequent |
| Security policy | Which boundaries are supported and how are vulnerabilities reported? | [`SECURITY.md`](../SECURITY.md) | Infrequent |
| 4. Current implementation | What paths, schemas and module boundaries exist now? | [implementation-architecture.md](./implementation-architecture.md), source, schemas and focused tests | Frequent |
| 5. Functional modules | What user capability does each product surface provide? | `docs/opl-*.md`, `docs/product/**` and public API schemas | Feature paced |
| 6. Status and plan | What is proven now, what is missing and what comes next? | [status.md](./status.md) for evidence; [roadmap.md](./roadmap.md) for gaps, priority and acceptance | Continuous |
| 7. Operations | How is the current release operated? | `docs/runtime/**`, deployment manifests and workflows | Release paced |
| 8. History | Why did a retired decision or shape exist? | [history](./history/README.md) | Append or retire |

The hierarchy is directional. A lower level may implement, explain or report
an upper-level decision; it cannot redefine it. When a product or architecture
owner changes, the same change must reconcile affected invariants, current
implementation docs, module docs, status and roadmap. A lower projection that
cannot yet follow is an explicit roadmap gap, not a competing SSOT.

## Authority Rules

- OPL family topology is owned outside this repository. Framework and Studio
  have separate, explicitly scoped Hosts; App owns product/GUI/release truth
  and the active Shell choice. The [architecture boundary](./architecture.md#host-client-and-cloud-authority-boundary)
  links the current Framework contract. Cloud owns online services and does not
  redefine another repository's runtime or Package authority.
- Framework `Console` means an in-process read-model contribution. OPL Cloud
  Console means the account/governance product owned here. Lower-level docs must
  not merge these meanings.

- Target intent comes from the latest user decision and its canonical product
  or architecture owner. Current code does not silently redefine the target.
- Current implementation claims require source, schema, tests or runtime
  evidence. A target document or contract field is not delivery evidence.
- `docs/status.md` is a replaceable current snapshot, not a chronological
  ledger. `docs/roadmap.md` owns open gaps and acceptance outcomes, not agent
  prompts, shell commands or branch write sets.
- Machine contracts exist only when a cross-module, public-interface, security,
  data-integrity or irreversible-side-effect fact needs deterministic
  enforcement. Visual preference and ordinary implementation shape stay out.
- `SECURITY.md` owns disclosure scope and reporting policy. Scanner artifacts,
  GitHub alerts, Issues, pull requests, and agent sessions are evidence or work
  surfaces; they do not become a second security, product, or status owner.
- `one-person-lab` owns the reusable development method. Instance identity,
  provider profile and deployment receipts belong to the instance repository;
  `opl-cloud` remains an internal artifact identifier.

## Active Navigation

- [Workspace application delivery plan](./roadmap.md#workspace-application-decoupling)
- [Final deliverables and owner handoff](./roadmap.md#required-deliverables)
- [Workspace current capability baseline](./status.md#current-capability-baseline)
- [Console Workspace interaction](./product/workspace-experience.md)
- [Console experience guide](./product/console-experience-guide.md)
- [Workspace identity and external SaaS boundary](./workspace-identity-and-external-saas-boundary.md)
- [Product release operations](./runtime/release.md)
- [Console browser lifecycle](./implementation/console.md)
- [Launch recovery reference](./implementation/workspace-launch-recovery.md)
- [Workspace Runtime access](./implementation/workspace-runtime-access.md)
- [Workspace image lifecycle](./implementation/workspace-images.md)
- [Documentation lifecycle policy](./policies/docs-lifecycle-policy.md)
- [Development worktree policy](./policies/development-worktree-policy.md)

## Specialized Owners

`opl-workspace.md` defines the Cloud application environment; the
[Workspace application architecture](architecture.md#workspace-application-boundary)
owns its DDD model and context boundaries. The Workspace identity decision owns
account cardinality, and `product/workspace-experience.md` owns its
Console interaction. `opl-console.md` defines the wider Console target while
the experience guide owns presentation principles. The other `opl-*.md` files
are target capability references, not implementation inventories.

`DEV_GUIDE.md` owns local setup and commands; `CONTRIBUTING.md` owns contribution
and review; `AGENTS.md` owns agent execution rules. Installation, release
operations and Instance operations have distinct owners. Whitepaper source,
generated-output handling and publication evidence boundaries stay in their
respective directory READMEs.

The [lifecycle policy](./policies/docs-lifecycle-policy.md) owns creation,
replacement, archival and retirement rules. This index only routes readers.
