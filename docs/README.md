# OPL Cloud Documentation

This index owns the documentation hierarchy. It follows OPL Doc's semantic
governance model: one current owner per topic, current truth separated from
active gaps, support detail and Git history.

## Hierarchy

| Level | Question | Canonical owner | Change rate |
| --- | --- | --- | --- |
| 1. Product scope | What does this repository own? | [project.md](./project.md) | Long term |
| Public vision | Why would a user adopt Cloud? | [whitepaper](./whitepapers/opl-cloud-whitepaper.md); root READMEs are entry summaries | Long term |
| 2. Target architecture | What should the product become and who owns each authority? | [architecture.md](./architecture.md) and durable [decisions.md](./decisions.md) | Long term |
| 3. Durable invariants | Which safety, integrity and ownership facts must survive refactors? | [invariants.md](./invariants.md) and eligible machine contracts | Infrequent |
| Security policy | Which boundaries are supported and how are vulnerabilities reported? | [`SECURITY.md`](../SECURITY.md) | Infrequent |
| 4. Current implementation | What paths, schemas and module boundaries exist now? | [implementation-architecture.md](./implementation-architecture.md), source, schemas and focused tests | Frequent |
| 5. Functional modules | What user capability does each product surface provide? | `docs/opl-*.md`, `docs/product/**` and [machine API schemas](./spec/README.md) | Feature paced |
| 6. Status and plan | What is proven now, what is missing and what comes next? | [status.md](./status.md) for evidence; [roadmap.md](./roadmap.md) for gaps, priority and acceptance | Continuous |
| 7. Operations | How is the current release operated? | `docs/runtime/**`, [installation.md](./installation.md), deployment manifests and workflows | Release paced |

The hierarchy is directional. A lower level may implement, explain or report
an upper-level decision; it cannot redefine it. When a product or architecture
owner changes, the same change must reconcile affected invariants, current
implementation docs, module docs, status and roadmap. A lower projection that
cannot yet follow is an explicit roadmap gap, not a competing SSOT.

## Entrypoints And Consumers

- Root READMEs introduce the product and route readers here; this index maps
  questions to existing owners and does not repeat their business rules.
- [`AGENTS.md`](../AGENTS.md) owns agent execution constraints and verification
  rules; [`DEV_GUIDE.md`](../DEV_GUIDE.md) owns developer commands;
  [`CONTRIBUTING.md`](../CONTRIBUTING.md) owns PR workflow. None is a second
  product specification.
- Product intent belongs in `architecture.md` and `decisions.md`; durable rules
  belong in `invariants.md`; current source behavior belongs in
  `implementation-architecture.md`; live evidence belongs in `status.md`; open
  outcomes belong in `roadmap.md`. Historical docs and completed logs live in Git
  history, not `docs/`.
- [Contract inputs and generated consumers](../packages/contracts/README.md),
  [machine API/publisher contracts](./spec/README.md) and
  [release operations](./runtime/release.md) retain their own owners.
- Use the [lifecycle policy](./policies/docs-lifecycle-policy.md) before adding
  or restructuring a document.

## Authority Rules

- `one-person-lab` owns generic runtime/Package host contracts; `one-person-lab-app`
  owns desktop product and release truth; `opl-studio` owns the desktop/Docker
  application host; `opl-instance-medopl` owns the medopl Instance profile,
  domains, Secrets, deployment, rollback and installation receipts; Sub2API
  remains the external spendable-wallet, Key, Group, routing and usage owner.
- Framework `Console` is an in-process read-model projection. OPL Console here
  is Cloud's account, policy and Workspace governance UI.
- Cloud owns portable product builds (Candidates and Releases), diagnostic
  validators and business evidence custody without reading Instance Secrets,
  kubeconfig or cluster state.
- Every authoritative fact has one writer; no doc summarizes or freezes another
  owner's live status or dynamic API counts.

## Active Navigation

- [Current capability baseline](./status.md#current-capability-baseline)
- [Open outcomes and acceptance](./roadmap.md#priority-outcomes)
- [Target architecture and boundaries](./architecture.md#repository-and-instance-topology)
- [Workspace application architecture](./architecture.md#workspace-application-boundary)
- [Current source and module map](./implementation-architecture.md#physical-module-and-dependency-map)
- [Durable invariants and D17 settlement](./invariants.md)
- [Machine API & Publisher contracts](./spec/README.md)
- [Medical Agent Platform boundary](./medical-agent-platform.md)
- [Install OPL Cloud](./installation.md)
- [Product release operations](./runtime/release.md)
- [Isolated Package / BuildKit verification](./runtime/package-buildkit-local.md)
- [Cloud connection helper](./runtime/cloud-connect.md)
- [Console browser lifecycle](./implementation/console.md)
- [Launch recovery reference](./implementation/workspace-launch-recovery.md)
- [Workspace Runtime access](./implementation/workspace-runtime-access.md)
- [Workspace image lifecycle](./implementation/workspace-images.md)
- [Agent Package + Runtime + WebUI chain](./implementation/agent-package-runtime-webui-chain.md)
- [Console Workspace interaction](./product/workspace-experience.md)
- [Console experience guide](./product/console-experience-guide.md)
- [Workspace identity and external SaaS boundary](./workspace-identity-and-external-saas-boundary.md)
- [Documentation lifecycle policy](./policies/docs-lifecycle-policy.md)
- [Development worktree policy](./policies/development-worktree-policy.md)

## Specialized Owners

- [`agent-lifecycle.md`](./agent-lifecycle.md): cross-surface object and owner model for exposing standard OPL Agents in App and Workspace.
- [`opl-workspace.md`](./opl-workspace.md): Cloud application environment and resource plan-change rules.
- [`opl-console.md`](./opl-console.md): account, approval, policy, billing and Agent-assisted read/review boundary.
- [`opl-gateway.md`](./opl-gateway.md): Frontier-AI Gateway boundary above Sub2API.
- [`opl-serve.md`](./opl-serve.md): delivering and exposing an approved OPL App or built Agent per Workspace.
- [`opl-fabric.md`](./opl-fabric.md): provider-neutral resource substrate (compute, storage, attachment, Secret binding, Connect).
- [`opl-connect.md`](./opl-connect.md): logical external-access capability inside Fabric.
- [`opl-ledger.md`](./opl-ledger.md): append-only financial/deletion receipts, reconciliation and opaque provenance.
- [`software-package-entitlements.md`](./software-package-entitlements.md): per-subject Cloud entitlement governance and controlled delivery for restricted Software Center packages.
- [`service-entitlements-and-discovery.md`](./service-entitlements-and-discovery.md): unified per-subject companion-service entitlements (`tenant.service_entitlements`), instance service catalog dispatch, and short-lived Ed25519 `service-grant` discovery protocol.
