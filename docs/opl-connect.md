# OPL Connect

OPL Connect is the connector capability inside OPL Fabric.

It gives OPL App, OPL Workspace, OPL Console, and domain agents one stable way
to use literature sources, databases, tools, APIs, and institutional systems.
Console governs organization-managed connector access, but Connect itself is a
platform capability that can be used from local App and cloud Workspace.

## First Stable Path: Literature Connectors

PubMed should be the first stable literature connector because MAS already
needs reliable literature access.

The first version should stay read-only:

- search literature;
- fetch article metadata;
- normalize source refs such as PMID, DOI, title, journal, authors, date,
  abstract, and subject terms when available;
- return enough provenance for MAS and Ledger receipts;
- expose clear rate-limit, credential, and source-boundary behavior.

## Skill To Platform Path

Skills remain the fastest way to prototype a capability. OPL Connect becomes
the home for capabilities that are used often, need stable behavior, and are
shared across agents or workbench surfaces.

```text
skill prototype
-> reusable connector behavior
-> OPL Connect adapter
-> App / Workspace capability
-> optional Console governance
-> Ledger receipt refs
```

| Stage | Owner | Purpose |
| --- | --- | --- |
| Skill prototype | ScholarSkills, MAS, or another domain skill | Explore search strategy, prompt shape, result ranking, and domain workflow |
| Connector behavior | OPL Connect | Stabilize API access, normalization, error semantics, source refs, and rate-limit behavior |
| Workbench capability | OPL App and OPL Workspace | Let users and agents call the connector through the same capability path |
| Governance | OPL Console | Approve organization-managed connectors, credentials, quotas, and audit policy |
| Evidence | OPL Ledger | Record connector refs, input query, selected sources, outputs, and continuation refs |

## MAS Literature Flow

MAS should call literature sources through a capability request instead of
depending on a private skill-only integration.

```text
MAS agent
-> literature capability request
-> OPL App or OPL Workspace capability profile
-> OPL Connect / PubMed connector
-> normalized literature refs
-> MAS evidence and writing workflow
-> Ledger receipt refs when the task is recorded
```

This keeps the chain smooth without making Fabric own medical or academic
judgment. OPL Connect owns reliable access and normalized refs. MAS owns query
intent, inclusion judgment, citation use, synthesis, and reviewer behavior.

## Maintenance Boundary

| Concern | Owner |
| --- | --- |
| API access, request shape, retries, rate limits, source refs | OPL Connect |
| Search strategy, citation judgment, evidence synthesis | MAS or the calling domain agent |
| Local user configuration and capability display | OPL App |
| Cloud workbench configuration and status display | OPL Workspace |
| Organization credentials, quota, approval, audit policy | OPL Console |
| Receipt refs and continuation entry | OPL Ledger |

## First Version

The first version should avoid building a full literature platform.

It should provide one stable PubMed connector, one normalized result shape, one
workbench capability entry, and one receipt reference shape. Reference manager
features, central indexing, vector search, paid-content access, and multi-source
ranking can wait until real usage shows they are needed.
