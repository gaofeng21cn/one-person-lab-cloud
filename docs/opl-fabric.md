# OPL Fabric

OPL Fabric is the resource and connector substrate behind OPL Cloud.

It keeps compute, storage, software environments, connectors, and execution
adapters under one platform layer instead of turning every resource family into
a frontstage product.

```text
OPL Fabric
├─ OPL Connect        databases, literature sources, tools, APIs, internal systems
├─ OPL Compute        Docker, VM, GPU, SSH, HPC, managed workers
├─ OPL Environments   software stacks, package locks, container images, runtime manifests
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

## OPL Compute

OPL Compute is the controlled compute layer.

Initial paths can stay small:

- Docker or VM for the first managed Workspace jobs.
- GPU workers for model, image, or deep-learning tasks.
- SSH or HPC adapters for lab and institutional infrastructure.
- Managed workers for later self-hosted Cloud capacity.

The execution flow should remain:

```text
plan → approve → submit → monitor → collect → receipt
```

## OPL Environments

OPL Environments is the versioned software environment catalog under Compute and
Fabric.

It should cover software stacks such as Python, R, Quarto, LaTeX, CUDA,
bioinformatics toolchains, Office/PDF tooling, OPL App runtime payloads, and
domain-agent packages.

It should not become a separate frontstage product in the first version.
Console configures environment policy. Workspace selects or inherits approved
environment templates.

## Storage Vault

Storage Vault covers workspace volumes, private buckets, institutional storage
refs, database refs, retention policy, and data access boundaries.

It should be modeled separately from compute because data access, retention,
permission, and sensitivity have a different lifecycle from job execution.

