# Research Provenance

Claude Science is a useful reference because it frames scientific AI as a
workbench: domain skills, data connections, compute resources, software
environments, reproducible artifacts, and reviewer agents work together around
research outputs.

OPL Cloud should learn the platform pattern, not the life-sciences packaging.

## OPL Translation

| Claude Science pattern | OPL Cloud surface |
| --- | --- |
| Scientific workbench | OPL App / OPL Workspace |
| Team and admin enablement | OPL Console |
| Claude/provider access | OPL Gateway |
| HPC, SSH, local, and on-demand compute | OPL Fabric / OPL Compute |
| Scientific databases and connectors | OPL Fabric / OPL Connect |
| Domain skills and reference packs | Domain owner skills + OPL Connect for sync/install |
| Exact code, environment, and history | OPL Fabric / OPL Environments + OPL Ledger |
| Reviewer agent | OPL Ledger / Reviewer Gate |
| Figures and manuscripts | App / Workspace artifact delivery surface |

## Product Principles

1. Cloud is a workbench infrastructure layer, not just a connector catalog.
2. Every meaningful remote run should produce a receipt.
3. Artifacts should preserve refs to inputs, code, commands, environment,
   owner, reviewer checks, and continuation entry points.
4. Reviewer checks should be domain-aware and produce explicit receipts.
5. Sensitive data should remain with the user or institution by default.
6. Domain skills keep domain truth; Connect stabilizes shared access and
   Ledger records provenance.

## Domain Reviewer Paths

- MAS reviewer: citation, statistics, figure-code, and manuscript consistency.
- MAG reviewer: funder fit, eligibility, compliance, and budget fields.
- RCA reviewer: chart data source, transformation, and narrative consistency.
- BookForge reviewer: chapter continuity, citation coverage, style consistency,
  and export readiness.

## Minimum Provenance Path

The first version can start with a simple Ledger receipt for each App action,
Workspace action, or managed job:

```text
plan → approval → command/code → environment → input refs → output refs → reviewer result → continuation entry
```

This is enough to establish the OPL Cloud evidence model before building a
larger audit system.
