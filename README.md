<p align="center">
  <a href="./README.md"><strong>English</strong></a> | <a href="./README.zh-CN.md">中文</a>
</p>

<h1 align="center">OPL Cloud</h1>

<p align="center"><strong>Frontier AI infrastructure for One Person Lab</strong></p>
<p align="center">AI gateway · online workspace · cloud console · resource fabric · evidence ledger</p>

<!--
Owner: `one-person-lab-cloud`
Purpose: `public_cloud_entry`
State: `active_public_entry`
Machine boundary: Human-readable product and architecture entry. Machine truth for App, Framework, Gateway services, Workspace runtime, billing, jobs, receipts, and release status remains with the owning repositories, services, contracts, runtime outputs, and owner receipts.
-->

<p align="center">
  <img src="assets/branding/opl-cloud-overview.png" alt="OPL Cloud overview" width="100%" />
</p>

## Why OPL Cloud

One Person Lab is built for complex knowledge work: research, grants,
presentations, books, agents, and other projects that need progress, review,
revision, files, and evidence over many rounds.

OPL Cloud extends that work from a local workbench into online and team
workflows:

- Users access frontier AI capability through one stable OPL entry point.
- Users open managed online OPL App workspaces with isolated access and
  resource packages.
- Teams manage accounts, billing, permissions, workspaces, connectors, and
  resource policies from one console.
- Remote jobs run on approved compute, storage, software environments, and
  external systems.
- Important results keep receipts, provenance, reviewer checks, and
  continuation entries.

**OPL Cloud is the cloud infrastructure layer for those workflows.**

It keeps OPL Workspace as the user workbench, OPL Console as the management
surface, OPL Gateway as the AI capability entry point, OPL Fabric as the
resource substrate, and OPL Ledger as the evidence record.

## Product Map

| Layer | Brand | Role | Surface |
| --- | --- | --- | --- |
| AI access | **OPL Gateway** | Models, keys, routing, provider policy, and usage metering | Product |
| User workbench | **OPL Workspace** | Online OPL App, project workspace, task sessions, artifact preview, result delivery | Product |
| Management | **OPL Console** | Organization, users, billing, quotas, workspace lifecycle, resource policy | Product |
| Resource substrate | **OPL Fabric** | Compute, storage, software environments, connectors, and execution adapters | Platform |
| Evidence record | **OPL Ledger** | Job receipts, artifact provenance, reviewer gates, audit records, continuation refs | Platform |

## Core Highlights

**OPL Gateway stays top-level**<br/>
Gateway is the directly visible AI access, metering, and billing surface. It is
the first available capability foundation for OPL Cloud.

**OPL Workspace is the user-facing workbench**<br/>
Workspace is where users open projects, start tasks, inspect job status, preview
artifacts, receive reviewer feedback, and collect deliverables.

**OPL Console is the management plane**<br/>
Console manages who can use Cloud, what they can access, how much they can
spend, which workspaces can run, which connectors are approved, and which
environments are available.

**OPL Fabric does the resource work**<br/>
Fabric contains the compute pool, storage vault, environment catalog, connector
registry, and execution adapters. Ordinary users should see productized choices
such as standard compute, GPU acceleration, private data bucket, or
institutional HPC.

**OPL Ledger makes results accountable**<br/>
Ledger records the plan, approval, commands or code, environment, input refs,
output refs, reviewer results, owner, and continuation entry for meaningful
Cloud work.

## OPL Fabric

OPL Fabric is the resource and connector substrate behind OPL Cloud.

```text
OPL Fabric
├─ OPL Connect        databases, literature sources, tools, APIs, internal systems
├─ OPL Compute        Docker, VM, GPU, SSH, HPC, managed workers
├─ OPL Environments   reproducible software environments and runtime templates
└─ Storage Vault      workspace volumes, private buckets, institutional storage refs
```

Together, these capabilities let workspaces connect materials, use tools, obtain
compute resources, and run tasks in the right software environment.

## OPL Ledger

OPL Ledger is the evidence layer for remote work and result delivery.

Every meaningful Workspace action or Cloud job should be able to leave a
receipt:

```text
plan → approval → run → artifacts → reviewer checks → receipt → continuation
```

Ledger records what happened, which inputs and environments were used, which
outputs were produced, what checks ran, and how the work can be resumed or
reviewed later.

## Minimum Viable Cloud

The first practical version should prove one complete path:

1. Gateway: AI access, key management, routing, usage, and billing data.
2. Workspace: one online OPL App instance with URL, account, storage directory,
   and base package.
3. Console: user, package, Gateway usage, and Workspace lifecycle management.
4. Fabric: one Docker or VM compute path plus one volume or bucket storage path.
5. Ledger: inspectable receipts for Workspace actions and jobs.

HPC, GPU workers, literature databases, institutional storage, and software
environment catalogs can enter as Fabric adapters after the first real
Workspace job path works end to end.

## Documentation

- [Product Matrix](docs/product-matrix.md)
- [Architecture](docs/architecture.md)
- [OPL Gateway](docs/opl-gateway.md)
- [OPL Workspace](docs/opl-workspace.md)
- [OPL Console](docs/opl-console.md)
- [OPL Fabric](docs/opl-fabric.md)
- [OPL Ledger](docs/opl-ledger.md)
- [Research Provenance](docs/research-provenance.md)
- [Roadmap](docs/roadmap.md)

## Technical Entry

<details>
  <summary><strong>Developer and operator notes</strong></summary>

This repository currently contains product and architecture documentation.
Gateway services, Console implementation, Workspace runtime, Fabric adapters,
Ledger storage, and billing systems live in their owning implementation
surfaces.

Readiness, release, billing, runtime, security, and reproducibility claims
should come from the owning service, repository, contract, runtime readback, or
owner receipt.

### Repository Layout

```text
one-person-lab-cloud/
  assets/              README and product visual assets
  docs/                Cloud product, architecture, and roadmap notes
  README.md            English public entry
  README.zh-CN.md      Chinese public entry
```

### Minimum Checks

```bash
python3 - <<'PY'
from pathlib import Path
import re
missing = []
for f in Path('.').rglob('*.md'):
    text = f.read_text(encoding='utf-8')
    for m in re.finditer(r'\[[^\]]+\]\(([^)#][^)]+)\)', text):
        target = m.group(1)
        if '://' in target or target.startswith('mailto:'):
            continue
        if not (f.parent / target).resolve().exists():
            missing.append((str(f), target))
if missing:
    raise SystemExit(missing)
print('local_links_ok')
PY
```

</details>
