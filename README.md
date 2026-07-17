<p align="center">
  <img src="assets/branding/opl-cloud-logo.png" alt="OPL Cloud logo" width="132" />
</p>

<p align="center">
  <a href="./README.md"><strong>English</strong></a> | <a href="./README.zh-CN.md">中文</a>
</p>

<h1 align="center">OPL Cloud</h1>

<p align="center"><strong>Cloud infrastructure for continuous One Person Lab work</strong></p>
<p align="center">AI access · online workbench · Agent serving · governed resources · evidence continuity</p>

<!--
Owner: `one-person-lab-cloud`
Purpose: `public_cloud_entry`
State: `active_public_entry`
Machine boundary: Human-readable product and architecture entry. This repository does not own a Cloud runtime, deployed service state, billing truth, release state, domain verdict, or owner acceptance.
-->

<p align="center">
  <img src="assets/branding/opl-cloud-overview-v2.png" alt="OPL Cloud journey from local work to online continuation, private data, remote compute, shared review, and optional publication" width="100%" />
</p>

## Why OPL Cloud

Research, grants, presentations, books, and Agent development rarely finish in
one session or on one machine. Work may start locally, need private data or
remote compute, pass through human review, and later become a service that
others can call. Splitting that journey across unrelated tools makes project
state, permissions, costs, and evidence drift apart.

OPL Cloud defines how those needs can remain part of one OPL work chain:

- continue a local OPL App project in an online OPL Workspace;
- use approved models, data sources, software environments, storage, and
  compute without moving authority away from their owners;
- publish an exact validated Agent revision through a stable API, Embed, or
  Hosted UI;
- keep approval, usage, provenance, review, and continuation refs connected to
  the work;
- leave professional judgment with the responsible domain Agent and human
  owner.

## Product Model

| User need | Target surface | Responsibility boundary |
| --- | --- | --- |
| AI access and usage | **OPL Gateway** | Model access, routing, provider policy, and usage signals |
| Online project work | **OPL Workspace** | One primary cloud workbench per user account |
| External Agent use | **OPL Serve** | Exact Service, immutable Revision, Deployment, API, Embed, and Hosted UI |
| Account governance | **OPL Console** | Account policy, approval, quota, billing, and managed-resource policy |
| Data, tools, and compute | **OPL Fabric** | Connect, Compute, Storage, Environments, and execution adapters |
| Evidence continuity | **OPL Ledger** | Receipt, provenance, review, and continuation refs |

OPL Framework remains the owner of package lifecycle and generic execution
semantics. OPL Packages owns manifest, digest, install, lock, update, rollback,
and repair. OPL Runway owns Invocation and Session execution lifecycle. Domain
Agents own professional strategy, quality verdicts, artifacts, and delivery
authority. Cloud surfaces consume those refs; they do not create competing
truth.

## One Continuous Journey

```text
local OPL App project
-> optional online OPL Workspace
-> approved Gateway / Fabric capabilities
-> results and review return to the workbench
-> Ledger refs preserve how to inspect and continue
-> optional exact Agent Revision publication through OPL Serve
```

Each user account has one primary OPL Workspace. Projects and collaboration live
inside or around that Workspace; they do not create multiple workbench truths.
One account may publish multiple Agent Services because Services are deployment
resources, not additional Workspaces.

## Current Repository Boundary

This repository owns the public OPL Cloud vision, target product architecture,
planning contracts, and whitepaper source. It is a documentation and
product-architecture aggregation repository, not an implementation repository.
The presence of a design, contract, generated artifact, or passing document
check does not mean that a Gateway, Workspace, Serve, Console, Fabric, or Ledger
service is deployed or ready.

For current availability, runtime health, security, billing, release, or owner
acceptance, read the owning implementation, machine contract, runtime output,
and owner receipt. The [roadmap](docs/roadmap.md) is the single current planning
owner for open Cloud gaps; it is not a readiness dashboard.

## Start Here

- [OPL Cloud whitepaper](https://gaofeng21cn.github.io/one-person-lab-cloud/latest/whitepapers/opl-cloud-whitepaper.html)
- [Documentation index and ownership map](docs/README.md)
- [Architecture and authority boundaries](docs/architecture.md)
- [Current roadmap, gaps, and next Agent prompt](docs/roadmap.md)
- [Workspace identity and external SaaS boundary](docs/workspace-identity-and-external-saas-boundary.md)

<details>
  <summary><strong>Developer and operator details</strong></summary>

### Repository Layout

```text
one-person-lab-cloud/
  assets/              public brand and journey assets
  contracts/           whitepaper artifact profile
  docs/                product, architecture, planning, and provenance docs
  scripts/             whitepaper build and publication request wrappers
```

The canonical technical navigation is [docs/README.md](docs/README.md). Avoid
adding service code, runtime state, package state, or copied owner truth to this
repository.

### Minimum Checks

```bash
node --experimental-strip-types scripts/build-opl-cloud-whitepaper.ts
git diff --check
```

The whitepaper build proves artifact rendering only. Publication requires the
approved workflow and exact-byte public readback described in
[docs/delivery/whitepapers/README.md](docs/delivery/whitepapers/README.md).

</details>
