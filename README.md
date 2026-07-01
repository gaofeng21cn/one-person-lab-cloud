<p align="center">
  <img src="assets/branding/opl-cloud-overview.png" alt="OPL Cloud overview" width="100%" />
</p>

<p align="center">
  <a href="./README.md"><strong>English</strong></a> | <a href="./README.zh-CN.md">中文</a>
</p>

<h1 align="center">OPL Cloud</h1>

<p align="center"><strong>Frontier AI infrastructure for One Person Lab</strong></p>
<p align="center">AI access · online OPL workspaces · controlled compute · usage metering · reproducible evidence chains</p>

<!--
Owner: `one-person-lab-cloud`
Purpose: `public_cloud_entry`
State: `active_public_entry`
Machine boundary: Human-readable product and architecture entry. Machine truth for App, Framework, Gateway services, Workspace runtime, billing, jobs, receipts, and release status remains with the owning repositories, services, contracts, runtime outputs, and owner receipts.
-->

## Why OPL Cloud

One Person Lab is built for complex knowledge work: research, grants,
presentations, books, agents, and other projects that need progress, review,
revision, files, and evidence over many rounds.

Local-first work remains essential. The harder cloud problem is different:

- Can users access frontier AI capability through one stable OPL entry point?
- Can a user start an online OPL App workspace without thinking about Docker
  hosts, compute nodes, or storage placement?
- Can long-running compute produce receipts instead of disappearing into logs?
- Can teams manage usage, billing, permissions, and workspace lifecycle from
  one console?
- Can research artifacts keep enough provenance for review, audit, and later
  continuation?

**OPL Cloud is the cloud infrastructure layer for those questions.**

It does not replace One Person Lab App. The App remains the local-first user
workspace. OPL Cloud provides the remote control plane, AI gateway, managed
workspace layer, controlled compute path, and evidence services that extend OPL
from one machine to online and team workflows.

## Core Highlights

**OPL Gateway as the AI capability foundation**<br/>
Gateway is the first available Cloud component. It provides a unified access
point for frontier AI APIs, token management, provider configuration, usage
visibility, and downstream OPL workflows.

**Managed online OPL workspaces**<br/>
OPL Workspace is the planned cloud workspace product: each workspace should
have an isolated URL, account, password, compute configuration, storage
configuration, and package-based billing.

**A console for users, not infrastructure operators**<br/>
OPL Console should manage accounts, organizations, billing, permissions,
Gateway usage, and Workspace lifecycle. Docker hosts, compute nodes, and storage
placement should stay behind the product boundary unless diagnostics need them.

**Controlled compute with receipts**<br/>
Remote jobs should follow a simple pattern: plan, approve, run, collect
artifacts, and leave a receipt. Local Docker, remote VM, GPU worker, SSH, and
HPC-like execution can share one job contract over time.

**Evidence chains for research and agent workflows**<br/>
Cloud-side provenance should preserve refs to inputs, code, commands,
environment, owner, reviewer checks, and continuation entry points without
turning Cloud into the owner of sensitive source data.

**Curated capability packs, not a plugin marketplace**<br/>
OPL Cloud should support team-approved capability packs for MAS, MAG, RCA,
BookForge, and future Foundry Agents. Ordinary users should see professional
entry points, not a raw catalog of skills, connectors, and runtimes.

## Product Matrix

| Product | Role | Status |
| --- | --- | --- |
| **OPL Gateway** | Frontier AI capability gateway, API access, token management, provider routing, and usage metering | Available |
| **OPL Console** | Cloud management console for accounts, organizations, billing, permissions, workspaces, and operations | In development |
| **OPL Workspace** | Managed online OPL App workspace with isolated URL, credentials, compute, storage, and billing package | In development |
| **Evidence Services** | Provenance store, reviewer gate, artifact receipts, and policy checks for research and agent workflows | Planned |

## Product Boundary

OPL Cloud is a product umbrella and architecture entry point. It should not
become a second source of truth for OPL App, OPL Framework, domain agents, or
Gateway service internals.

| Repository or product | Owns |
| --- | --- |
| [`one-person-lab`](https://github.com/gaofeng21cn/one-person-lab) | OPL Framework, runtime contracts, CLI, stage execution, progress and evidence interfaces |
| [`one-person-lab-app`](https://github.com/gaofeng21cn/one-person-lab-app) | Desktop App, Docker/WebUI user experience, packaging, release assets, GUI contracts |
| **OPL Gateway** | AI capability access, token management, provider routing, usage metering, and Gateway integration assets |
| **OPL Console** | Cloud account, organization, billing, workspace, permission, and operations management |
| **OPL Workspace** | Managed online OPL App runtime instances and workspace lifecycle |

Public Gateway integration assets may live outside this repository until a
dedicated OPL Gateway implementation repository exists. This repository keeps
the public OPL Cloud product narrative, architecture, and roadmap together.

## Current Position

- OPL Gateway is available and should be presented as the AI capability
  foundation of OPL Cloud, not as a generic token platform.
- OPL Console and OPL Workspace are under development.
- Research provenance, reviewer gates, job adapters, and team capability packs
  are planned Cloud capabilities.
- Sensitive data should remain in user workspaces, institutional storage, or
  private buckets by default; Cloud should store refs, metadata, lineage,
  receipts, usage, and policy records unless configured otherwise.

## Documentation

- [Product Matrix](docs/product-matrix.md)
- [Architecture](docs/architecture.md)
- [OPL Gateway](docs/opl-gateway.md)
- [OPL Console](docs/opl-console.md)
- [OPL Workspace](docs/opl-workspace.md)
- [Research Provenance](docs/research-provenance.md)
- [Roadmap](docs/roadmap.md)

## Technical Entry

<details>
  <summary><strong>Developer and operator notes</strong></summary>

This repository currently contains product and architecture documentation. It
does not ship the Gateway service, Console implementation, Workspace runtime, or
billing system.

Before making readiness, release, billing, runtime, or security claims, use the
owning service, repository, contract, runtime readback, or owner receipt. Docs
in this repository explain the public product boundary; they do not prove live
availability.

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

