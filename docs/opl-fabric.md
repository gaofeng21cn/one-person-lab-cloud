# OPL Fabric

OPL Fabric is the resource and connector substrate used by OPL App, OPL
Workspace, Cloud-managed jobs and approved domain-agent actions. It connects
work to compute, storage, software environments and external systems through a
shared plan, approval, execution, collection and receipt pattern.

Fabric is not a package registry or package lifecycle owner.

```text
OPL Fabric
├─ OPL Connect        connector access and normalized source refs
├─ OPL Compute        Docker, VM, GPU, SSH, HPC and managed workers
├─ OPL Environments   software stacks and runtime environment refs
├─ Workspace Storage  volumes, private buckets and institutional storage refs
├─ Surface adapters   Gateway/App/Workspace integration
└─ Execution adapters resource binding, status and output collection
```

## Reusable Capability Boundary

OPL Console governs Cloud-hosted or organization-managed use, but is not the
only Fabric caller. App, Workspace and approved domain agents can call Fabric
when their capability and policy refs allow it. Ledger records receipt refs.
The resource owner remains authority for the underlying resource state.

## OPL Connect

Connect owns stable connector access, normalized source refs, credentials,
error semantics, retries and rate limits. It does not own OPL Package discovery,
installation, version resolution or lock. A connector implementation may be
distributed in an OPL Package, but that package is installed and managed by
OPL Packages before Fabric binds it to a run.

Domain-specific retrieval strategy, evidence judgment, synthesis and quality
remain with the domain adapter and domain Agent.

## OPL Compute

Compute adapters cover local or managed Docker/VM, GPU workers, SSH/HPC and
later managed workers. Every path follows:

```text
plan -> approve -> execute -> monitor -> collect -> receipt
```

Console policy becomes relevant for Cloud-hosted or organization-managed
resources. User-provided resources can use the same flow without default Cloud
billing.

## OPL Environments

Environments describe software stacks, container/runtime refs, hardware needs
and compatibility constraints. They are resource inputs, not package locks.
When an environment requires an OPL Package, Fabric consumes its exact
manifest/lock ref from OPL Packages and records the binding.

## Package Resource Binding

Fabric may project package requirements for resource planning:

- exact package manifest and lock refs;
- compute and hardware requirements;
- storage and data-boundary requirements;
- environment and connector requirements;
- review gates and runtime policy refs.

Fabric can report `binding_available`, `binding_blocked` or equivalent resource
status. It cannot install, update, roll back or repair package bytes, choose a
new version, or write the package lock. A missing package routes to OPL
Packages; a missing resource routes to Fabric or Console policy as applicable.

## Resource Catalog

| Resource type | User-facing examples | Owner |
| --- | --- | --- |
| Compute | Standard compute, GPU, SSH/HPC, managed worker | OPL Compute / resource provider |
| Storage | Workspace volume, private bucket, institutional storage ref | Storage owner + Fabric binding |
| Connector | Literature source, database, internal API, tool integration | OPL Connect + source owner |
| Environment | Python/R, CUDA, document tooling, runtime profile | OPL Environments |
| Package requirement | Exact package ref and resource requirements | OPL Packages truth; Fabric projection only |

This catalog lets products select resources without exposing infrastructure
internals or creating a second package registry.
