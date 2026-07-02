# OPL Connect

OPL Connect is the connector capability inside OPL Fabric.

It gives OPL App, OPL Workspace, OPL Console, MAS, and other domain agents one
stable way to use literature sources, databases, tool libraries, resources,
large skill packs, APIs, and institutional systems. Console governs
organization-managed connector access, but Connect itself is a platform
capability that can be used from local App, cloud Workspace, MAS, and other
approved callers.

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
-> MAS / domain agent usage
-> optional Console governance
-> Ledger receipt refs
```

| Stage | Owner | Purpose |
| --- | --- | --- |
| Skill prototype | MAS, ScholarSkills, or another domain skill | Explore search strategy, prompt shape, result ranking, quality floor, and domain workflow |
| Enhancement pack | MAS Scholar Skills or another enhancement pack | Provide references, packs, quality floors, examples, and reusable domain material |
| Connector behavior | OPL Connect | Stabilize discovery, sync, install, API access, normalization, error semantics, source refs, and rate-limit behavior |
| Workbench capability | OPL App and OPL Workspace | Let users and agents call the connector through the same capability path |
| Domain use | MAS or another domain owner | Keep query intent, inclusion judgment, synthesis, writing behavior, and delivery authority |
| Governance | OPL Console | Approve organization-managed connectors, credentials, quotas, and audit policy |
| Evidence | OPL Ledger | Record connector refs, input query, selected sources, outputs, provenance, and continuation refs |

## Skill-first Collaboration

Domain owners keep the primary skill. MAS keeps the MAS skill and medical or
academic judgment. MAS Scholar Skills and similar enhancement packs can supply
references, reusable packs, examples, and quality floors. OPL Connect handles
the platform work: discovery, sync, install, credentials, rate limits, source
refs, normalized results, and stable connector behavior.

This makes the path positive and incremental:

```text
domain skill
-> enhancement pack
-> high-frequency connector behavior
-> OPL Connect / OPL Fabric capability
-> MAS / Workspace / App usage
-> Ledger receipt and provenance
```

OPL Ledger records what was used and where it came from. It does not replace
the domain source of truth or the domain owner's quality judgment.

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
judgment. OPL Connect owns reliable access, discovery, installation, normalized
refs, and connector receipts. MAS owns query intent, inclusion judgment,
citation use, synthesis, reviewer behavior, and final domain truth.

## Maintenance Boundary

| Concern | Owner |
| --- | --- |
| Discovery, sync, install, API access, request shape, retries, rate limits, source refs | OPL Connect |
| Main skill, search strategy, citation judgment, evidence synthesis, quality floor | MAS or the calling domain agent |
| References, packs, examples, quality floors | MAS Scholar Skills or another enhancement pack |
| Local user configuration and capability display | OPL App |
| Cloud workbench configuration and status display | OPL Workspace |
| Organization credentials, quota, approval, audit policy | OPL Console |
| Receipt refs, provenance, and continuation entry | OPL Ledger |

## First Version

The first version should avoid building a full literature platform.

It should provide one stable PubMed connector, one normalized result shape, one
workbench capability entry, and one receipt reference shape. Reference manager
features, central indexing, vector search, paid-content access, and multi-source
ranking can wait until real usage shows they are needed.
