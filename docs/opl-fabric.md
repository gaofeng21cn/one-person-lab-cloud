# OPL Fabric

OPL Fabric is the resource and connector substrate for OPL App, OPL Workspace,
and OPL Cloud-managed execution.

It keeps Connect, Compute, Storage, Environments, Gateway/App/Workspace
adapters, agent packages, and execution adapters under one platform layer. The
same Fabric flow can serve local OPL App, cloud OPL Workspace, MAS and other
domain agents, user-provided SSH or HPC resources, and OPL Cloud-hosted
resources.

Fabric capabilities are reusable platform capabilities. OPL Console can govern
organization-managed Fabric resources, but Fabric adapters do not require
Console as their only entry point. Local OPL App, cloud OPL Workspace, MAS, and
other domain agents can use approved Fabric capabilities directly through their
capability profiles.

Fabric is the general resource substrate. OPL Connect is the connection
capability inside Fabric, not a separate product and not a private Console
backend. Console governs the subset of Fabric resources that are hosted by OPL
Cloud or managed by an organization. Ledger records receipts and provenance for
Fabric work without taking over resource ownership or domain judgment.

```text
OPL Fabric
├─ OPL Connect        databases, literature sources, tools, APIs, resources, skill packs
├─ OPL Compute        Docker, VM, GPU, SSH, HPC, managed workers
├─ OPL Environments   software stacks, package locks, container images, runtime manifests
├─ Gateway adapters   AI access profiles, usage signals, provider policy links
├─ OPL Agent Registry approved agent packages, versions, requirements
└─ Workspace Storage      workspace volumes, private buckets, institutional storage refs
```

## OPL Connect

OPL Connect is the connector registry for data, literature, tools, APIs,
resources, large skill packs, and internal systems.

Examples:

- Literature sources: PubMed, arXiv, Semantic Scholar, Crossref, Zotero.
- Research databases: institutional databases, PostgreSQL, SQLite, DuckDB.
- Tools and APIs: GitHub, Jupyter, Python, R, Office, PDF, MinerU, chart tools.
- Skill and resource packs: MAS Scholar Skills references, shared prompt packs,
  quality-floor packs, data dictionaries, and approved tool bundles.
- Internal systems: ELN, shared drives, private APIs, hospital or lab systems.

Each connector uses a lifecycle:

```text
proposed -> configured -> approved -> available -> suspended -> retired
```

Connector records clarify owner, credential boundary, data scope,
available actions, approval policy, usage signal, and Ledger receipt behavior.

Literature access is the first stable Connect path. PubMed read-only access
moves from high-frequency MAS, ScholarSkills, or other domain skill prototypes
into OPL Connect when the behavior is stable enough to share across App,
Workspace, MAS, and agents. The domain skill continues to own strategy,
evidence judgment, quality floors, writing, and review behavior. Connect owns
discovery, sync, install, API access, normalization, source refs, connector
errors, retries, rate limits, and connector receipt refs.

The PubMed path is:

```text
MAS skill
-> App or Workspace capability profile
-> OPL Connect PubMed read-only connector
-> normalized source refs
-> MAS evidence workflow
-> optional Ledger receipt refs
```

## OPL Compute

OPL Compute is the controlled compute layer.

Initial paths can stay small:

- Docker or VM for the first managed Workspace jobs.
- GPU workers for model, image, or deep-learning tasks.
- SSH or HPC adapters for lab and institutional infrastructure.
- Managed workers for later self-hosted Cloud capacity.

The execution flow remains standard across OPL App and OPL Workspace:

```text
plan → approve → execute → monitor → collect → receipt
```

Console management starts when compute is OPL Cloud-hosted or
organization-managed. User-provided local, SSH, or HPC compute can use the same
flow without becoming Console-billed compute by default.

## Adapter Order

Fabric adds adapters in dependency order:

1. PubMed read-only connector as the first OPL Connect baseline.
2. Docker or VM plus one storage path.
3. SSH/HPC with user-provided credentials and explicit approval.
4. OPL Cloud-hosted GPU or managed worker capacity.
5. Database, institutional storage, and additional literature connectors.

Each adapter defines the same fields: configuration, approval, execution,
status, output collection, cost or usage signal when managed, and Ledger receipt
refs.

## OPL Environments

OPL Environments is the versioned software environment catalog used by Fabric
compute paths.

It covers software stacks such as Python, R, Quarto, LaTeX, CUDA,
bioinformatics toolchains, Office/PDF tooling, OPL App runtime payloads, and
domain-agent packages.

Console configures environment policy for managed organizations. OPL App and
OPL Workspace select or inherit approved environment templates through their
capability profiles.

Environment records clarify:

- display name and purpose;
- container image or runtime reference;
- package lock or manifest reference;
- compatible compute profiles;
- compatible agent packages;
- owner and review status;
- update and retirement policy.

## OPL Agent Registry

OPL Agent Registry is the approved package index for OPL-compatible agents.

It records which Agent Packages can be used, which versions are approved, what
resources they need, which connectors they require, and which teams or
workspaces may instantiate them.

OPL Meta Agent can produce the blueprint and package candidate. Console approves
the package for use. Fabric uses the registry record to prepare compute,
storage, environments, and connectors for each App or Workspace Agent Instance.

## Workspace Storage

Workspace Storage covers workspace volumes, private buckets, institutional storage
refs, database refs, retention policy, and data access boundaries.

It is modeled separately from compute because data access, retention,
permission, and sensitivity have a different lifecycle from job execution.

## Resource Catalog

Fabric capabilities are surfaced as a resource catalog for App, Workspace,
Console, MAS, and approved domain agents:

| Resource type | User-facing examples | Platform owner |
| --- | --- | --- |
| Compute | Standard compute, GPU compute, SSH/HPC, managed worker | OPL Compute |
| Storage | Workspace volume, private bucket, organization storage ref | Workspace Storage |
| Connector | Literature source, database, internal API, tool integration | OPL Connect |
| Environment | Python/R, Quarto/LaTeX, CUDA, document tooling, agent runtime | OPL Environments |
| Agent package | Approved package, version, requirements, review gates | OPL Agent Registry |

The catalog lets downstream products choose resources without exposing raw
infrastructure details to ordinary users.
