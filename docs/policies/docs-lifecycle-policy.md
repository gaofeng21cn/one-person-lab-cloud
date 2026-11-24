# Documentation Lifecycle Policy

[`one-person-lab`](https://github.com/gaofeng21cn/one-person-lab/blob/main/docs/policies/docs-lifecycle-policy.md)
owns the OPL family documentation lifecycle rules. This file applies them to
`one-person-lab-cloud`, whose hierarchy is rooted at [docs/README.md](../README.md).

## Core Principles

1. **Single Source of Truth**: Keep one canonical active owner per semantic
   topic. Index, summary and cross-surface files link to the owner rather than
   copying rules, module tables or field lists.
2. **Content-Level Maintenance**: When source, contracts or live product behavior
   change, update the owning paragraphs in place. Remove stale module, script,
   route, ORM or compatibility claims directly instead of appending "update"
   addenda.
3. **Target-State Synthesis (No Incremental Stacks)**: Never accumulate dated
   decision logs, version-by-version changelogs, PR-number narratives or long
   issue-scoped inventories inside active docs. Synthesize durable principles
   thematically; `docs/status.md` is a single current snapshot and `docs/roadmap.md`
   holds open outcomes only.
4. **Zero Compatibility Surface & Git as Archive**: Retire obsolete documents,
   historical migration plans, dated `docs/history/**` tombstones and static
   spec copies (`docs/spec/target/*.md`) completely once their durable rules live
   in canonical owners. Git history preserves commit/diff provenance; keep only
   active machine-consumed schemas (`03_api_contract_complete.yaml`,
   `publisher-contract.schema.json`, `packages/contracts/**`) under machine roots.

## Separation Of Layers

- **Product & Authority Target**: `docs/architecture.md`, `docs/opl-*.md`,
  `docs/decisions.md`
- **Durable Safety & Money Rules**: `docs/invariants.md`, `SECURITY.md`
- **Current Physical Code & Routing**: `docs/implementation-architecture.md`,
  `docs/implementation/*.md`
- **Evidence Snapshot vs Open Gaps**: `docs/status.md` (observed evidence only) vs
  `docs/roadmap.md` (unclosed outcomes only)
- **Operations & Install**: `docs/installation.md`, `docs/runtime/*.md`

Lower layers implement and report upper-level decisions without redefining them.
If code does not yet match a target rule, keep the rule intact and log the exact
remaining gap in `docs/roadmap.md`.

## Machine Contract & Test Hygiene

Machine-readable contracts belong in `packages/contracts/**` (or retained active
schemas under `docs/spec/target/`) only when cross-owner bytes or irreversible
side-effect validation require deterministic enforcement. Long-term tests verify
live behavior, identity fences, money/idempotency integrity and schema codecs,
not retired wrapper files or deleted prose layouts.
