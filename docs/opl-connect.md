# OPL Connect

OPL Connect is the connector capability inside OPL Fabric.

It gives OPL App, OPL Workspace, OPL Console, MAS, and other domain agents one
stable way to use literature sources, databases, tool libraries, resources,
large skill packs, APIs, and institutional systems. Console governs
organization-managed connector access, but Connect itself is a platform
capability that can be used from local App, cloud Workspace, MAS, and other
approved callers.

Connect owns the platform connector contract: stable access, API behavior,
normalization, source refs, credential boundaries, error semantics, retry
behavior, and rate-limit semantics. MAS, ScholarSkills, and other domain owners
own the domain strategy, quality judgment, evidence synthesis, writing, and
review behavior. OPL Ledger records receipts and provenance refs only.

## First Stable Path: PubMed Read-only Connector

PubMed is the first stable literature connector path because MAS already needs
reliable literature access and PubMed supports a clear read-only boundary.

The first connector stays read-only:

- search literature;
- fetch article metadata;
- normalize source refs such as PMID, DOI, title, journal, authors, date,
  abstract, and subject terms when available;
- return enough provenance for MAS and Ledger receipts;
- expose clear rate-limit, credential, and source-boundary behavior.

The human-readable call chain is:

```text
MAS skill
-> OPL Connect PubMed read-only connector
-> normalized refs
-> MAS evidence workflow
-> optional Ledger receipt refs
```

When the call starts from OPL App or OPL Workspace, the workbench capability
profile supplies the caller policy for the same Connect path.

## PubMed Shape

This section describes the first human-readable connector shape. It is a
documentation boundary, not a release-readiness claim.

### Request

A PubMed request tells Connect what to retrieve and how the caller will use the
result:

| Field | Human meaning |
| --- | --- |
| `request_id` | Stable request reference supplied by the caller or workbench |
| `caller_profile` | App, Workspace, MAS, or domain-agent capability profile allowed to call the connector |
| `connector_id` | PubMed read-only connector identity |
| `query` | Search terms, MeSH terms, PMID list, DOI list, or citation seed |
| `filters` | Date range, article type, language, humans/animals, publication status, or other PubMed-supported limits |
| `max_results` | Result cap for the retrieval step |
| `evidence_intent` | Why MAS needs the sources, such as screening, citation support, background, or reviewer response |
| `provenance_policy` | Whether to return raw source links, normalized refs, and receipt refs |

### Result

A PubMed result returns normalized source refs plus source-specific metadata:

| Field | Human meaning |
| --- | --- |
| `result_id` | Result reference for this connector response |
| `source` | PubMed / NCBI source name and connector version reference |
| `query_echo` | The query and filters Connect actually submitted |
| `normalized_refs` | PMID, DOI, title, journal, authors, date, abstract, MeSH or subject terms when available, and source URL |
| `retrieval_status` | Completed, partial, empty, or failed with connector-owned error semantics |
| `rate_limit` | Observed or declared rate-limit state when available |
| `source_boundary` | Read-only source note, credential boundary, and any returned source limitation |

### Receipt refs

Ledger receipt refs can record what the connector returned without taking over
domain judgment:

| Field | Human meaning |
| --- | --- |
| `receipt_ref` | Ledger reference for a recorded task or connector step |
| `request_ref` | The request that produced the connector call |
| `connector_ref` | PubMed connector identity and configuration reference |
| `input_ref` | Query, filters, and caller profile reference |
| `source_refs` | Normalized PMID/DOI/source URLs selected or returned |
| `output_ref` | MAS evidence artifact, citation set, manuscript section, or review packet that used the refs |
| `continuation_ref` | Where App, Workspace, MAS, or a reviewer can continue the evidence workflow |

## Skill To Platform Path

Skills remain the fastest way to prototype a capability. OPL Connect becomes
the home for capabilities that are used often, need stable behavior, and are
shared across agents or workbench surfaces.

```text
skill prototype
-> reusable connector behavior
-> OPL Connect adapter
-> App / Workspace / MAS capability profile call
-> normalized refs into domain workflow
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
academic judgment. MAS Scholar Skills and similar enhancement packs supply
references, reusable packs, examples, and quality floors. OPL Connect handles
the platform work: discovery, sync, install, credentials, rate limits, source
refs, normalized results, connector errors, and stable connector behavior.

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

MAS calls literature sources through a capability request instead of depending
on a private skill-only integration.

```text
MAS skill
-> literature capability request
-> OPL Connect PubMed read-only connector
-> normalized literature refs
-> MAS evidence and writing workflow
-> optional Ledger receipt refs
```

OPL App and OPL Workspace can issue the same request through their capability
profiles for user-facing workbench flows.

This keeps the chain smooth without making Fabric own medical or academic
judgment. OPL Connect owns reliable access, discovery, installation,
normalized refs, and receipt-ready connector refs. MAS owns query intent,
inclusion judgment, citation use, synthesis, reviewer behavior, and final
domain truth.

## Maintenance Boundary

| Concern | Owner |
| --- | --- |
| Discovery, sync, install, API access, request/result shape, errors, retries, rate limits, source refs | OPL Connect |
| Main skill, search strategy, citation judgment, evidence synthesis, quality floor, writing, review | MAS or the calling domain agent |
| References, packs, examples, quality floors | MAS Scholar Skills or another enhancement pack |
| Local user configuration and capability display | OPL App |
| Cloud workbench configuration and status display | OPL Workspace |
| Organization credentials, quota, approval, audit policy | OPL Console |
| Receipt refs, provenance, and continuation entry | OPL Ledger |

## First Version

The first version's target shape is intentionally small: one stable PubMed
connector, one normalized result shape, one workbench capability entry, and one
receipt reference shape. Reference manager features, central indexing, vector
search, paid-content access, and multi-source ranking remain outside the first
path until real usage proves they are needed.

The first path does not claim runtime or release readiness. It defines the
platform boundary that implementation, contracts, and runtime evidence can use.
