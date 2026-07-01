# OPL Fabric

OPL Fabric is the resource and connector substrate for OPL App, OPL Workspace,
and OPL Cloud-managed execution.

It keeps compute, storage, software environments, connectors, and execution
adapters under one platform layer. The same Fabric flow can serve local OPL App,
cloud OPL Workspace, user-provided SSH or HPC resources, and OPL Cloud-hosted
resources.

```text
OPL Fabric
├─ OPL Connect        databases, literature sources, tools, APIs, internal systems
├─ OPL Compute        Docker, VM, GPU, SSH, HPC, managed workers
├─ OPL Environments   software stacks, package locks, container images, runtime manifests
├─ OPL Agent Registry approved agent packages, versions, requirements
└─ Storage Vault      workspace volumes, private buckets, institutional storage refs
```

## OPL Connect

OPL Connect is the connector registry for data, literature, tools, APIs, and
internal systems.

Examples:

- Literature sources: PubMed, arXiv, Semantic Scholar, Crossref, Zotero.
- Research databases: institutional databases, PostgreSQL, SQLite, DuckDB.
- Tools and APIs: GitHub, Jupyter, Python, R, Office, PDF, MinerU, chart tools.
- Internal systems: ELN, shared drives, private APIs, hospital or lab systems.

Each connector should have a lifecycle:

```text
proposed -> configured -> approved -> available -> suspended -> retired
```

Connector records should clarify owner, credential boundary, data scope,
available actions, approval policy, usage signal, and Ledger receipt behavior.

## OPL Compute

OPL Compute is the controlled compute layer.

Initial paths can stay small:

- Docker or VM for the first managed Workspace jobs.
- GPU workers for model, image, or deep-learning tasks.
- SSH or HPC adapters for lab and institutional infrastructure.
- Managed workers for later self-hosted Cloud capacity.

The execution flow should remain standard across OPL App and OPL Workspace:

```text
plan → approve → execute → monitor → collect → receipt
```

Console management starts when compute is OPL Cloud-hosted or
organization-managed. User-provided local, SSH, or HPC compute can use the same
flow without becoming Console-billed compute by default.

## Adapter Order

Fabric should add adapters in dependency order:

1. Docker or VM plus one storage path.
2. SSH/HPC with user-provided credentials and explicit approval.
3. OPL Cloud-hosted GPU or managed worker capacity.
4. Literature, database, and institutional storage connectors.

Each adapter should define the same fields: configuration, approval, execution,
status, output collection, cost or usage signal when managed, and Ledger receipt
refs.

## OPL Environments

OPL Environments is the versioned software environment catalog under Compute and
Fabric.

It should cover software stacks such as Python, R, Quarto, LaTeX, CUDA,
bioinformatics toolchains, Office/PDF tooling, OPL App runtime payloads, and
domain-agent packages.

In the first version, it stays inside Fabric. Console configures environment
policy. OPL App and OPL Workspace select or inherit approved environment
templates.

Environment records should clarify:

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

## Storage Vault

Storage Vault covers workspace volumes, private buckets, institutional storage
refs, database refs, retention policy, and data access boundaries.

It should be modeled separately from compute because data access, retention,
permission, and sensitivity have a different lifecycle from job execution.

## Resource Catalog

Fabric capabilities should be surfaced as a resource catalog for App,
Workspace, and Console:

| Resource type | User-facing examples | Platform owner |
| --- | --- | --- |
| Compute | Standard compute, GPU compute, SSH/HPC, managed worker | OPL Compute |
| Storage | Workspace volume, private bucket, organization storage ref | Storage Vault |
| Connector | Literature source, database, internal API, tool integration | OPL Connect |
| Environment | Python/R, Quarto/LaTeX, CUDA, document tooling, agent runtime | OPL Environments |
| Agent package | Approved package, version, requirements, review gates | OPL Agent Registry |

The catalog lets downstream products choose resources without exposing raw
infrastructure details to ordinary users.
