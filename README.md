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

OPL Cloud extends that work from a local workbench into online, team, and
remote-resource workflows:

- Users access frontier AI capability through one stable OPL entry point.
- Users can work from OPL App locally or from OPL Workspace online with the
  same workbench model.
- Teams manage accounts, billing, permissions, workspaces, connectors, and
  resource policies from one console.
- Remote jobs run on approved compute, storage, software environments, and
  external systems.
- Important results keep receipts, provenance, reviewer checks, and
  continuation entries.

**OPL Cloud is the cloud infrastructure layer for those workflows.**

It keeps OPL App and OPL Workspace as equivalent user workbench surfaces:
OPL App is the local surface, and OPL Workspace is the cloud Docker/WebUI
surface. Both can use OPL Gateway, OPL Fabric, and OPL Ledger capabilities.
OPL Console manages the resources, permissions, lifecycle, and billing that are
hosted or organization-managed through OPL Cloud.

## Product Map

| Layer | Brand | Role | Surface |
| --- | --- | --- | --- |
| AI access | **OPL Gateway** | Models, keys, routing, provider policy, and usage metering | Product |
| User workbench | **OPL App / OPL Workspace** | Local OPL App and cloud Docker/WebUI OPL App surfaces for project work, task sessions, artifact preview, and result delivery | Product |
| Management | **OPL Console** | Organization, users, billing, quotas, workspace lifecycle, resource policy | Product |
| Resource substrate | **OPL Fabric** | Compute, storage, software environments, connectors, and execution adapters | Platform |
| Evidence record | **OPL Ledger** | Job receipts, artifact provenance, reviewer gates, audit records, continuation refs | Platform |

## Core Highlights

**OPL Gateway stays top-level**<br/>
Gateway is the directly visible AI access, metering, and billing surface. It is
the first available capability foundation for OPL Cloud.

**OPL App and OPL Workspace are equivalent workbench surfaces**<br/>
OPL App is the local workbench. OPL Workspace is the cloud Docker/WebUI
workbench. Users open projects, start tasks, inspect job status, preview
artifacts, receive reviewer feedback, and collect deliverables from either
surface.

**OPL Console is the management plane for managed resources**<br/>
Console manages OPL Cloud-hosted or organization-managed accounts, billing,
permissions, workspace lifecycle, connector approvals, environment policy, and
resource quotas. User-provided local, SSH, or HPC resources can also use the
standard Fabric flow; when an organization brings those resources under shared
policy, Console becomes the management surface.

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
├─ OPL Agent Registry approved agent packages, versions, requirements
└─ Storage Vault      workspace volumes, private buckets, institutional storage refs
```

Together, these capabilities let OPL App and OPL Workspace connect materials,
use tools, obtain compute resources, and run tasks in the right software
environment.

## OPL Ledger

OPL Ledger is the evidence layer for remote work and result delivery.

Every meaningful App, Workspace, or Cloud-managed job should be able to leave a
receipt:

```text
plan → approval → run → artifacts → reviewer checks → receipt → continuation
```

Ledger records what happened, which inputs and environments were used, which
outputs were produced, what checks ran, and how the work can be resumed or
reviewed later.

## Core Delivery Path

OPL Cloud is organized around one complete delivery path:

1. Gateway: AI access, key management, routing, usage, and billing data.
2. Workspace: online OPL App instances with URL, account, storage directory, and
   base package.
3. Console: user, package, Gateway usage, and Workspace lifecycle management.
4. Fabric: compute paths, storage paths, software environments, and connector
   capabilities.
5. Ledger: inspectable receipts for App actions, Workspace actions, and managed
   jobs.

HPC, GPU workers, literature databases, institutional storage, and software
environment catalogs can enter OPL Cloud through the same Fabric flow.

## Documentation

- [OPL Cloud Whitepaper Markdown](docs/public/whitepaper/opl-cloud-whitepaper.md)
- [OPL Cloud Whitepaper PDF](docs/public/whitepaper/opl-cloud-whitepaper.pdf)
- [Product Matrix](docs/product-matrix.md)
- [Architecture](docs/architecture.md)
- [Platform Capability Gaps](docs/platform-capability-gaps.md)
- [OPL Gateway](docs/opl-gateway.md)
- [OPL Workspace](docs/opl-workspace.md)
- [OPL Console](docs/opl-console.md)
- [OPL Fabric](docs/opl-fabric.md)
- [OPL Ledger](docs/opl-ledger.md)
- [OPL Agent Lifecycle](docs/agent-lifecycle.md)
- [Research Provenance](docs/research-provenance.md)
- [Shared Execution Contract](docs/contracts/shared-execution-contract.md)
- [Ledger Receipt Schema](docs/contracts/ledger-receipt-schema.md)
- [Agent Registry Entry](docs/contracts/agent-registry-entry.md)
- [Resource Ownership and Billing](docs/contracts/resource-ownership-and-billing.md)
- [Workspace Lifecycle](docs/workspace-lifecycle.md)
- [Console Governance and Billing](docs/console-governance-and-billing.md)
- [Fabric Adapter Contract](docs/fabric-adapter-contract.md)
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
