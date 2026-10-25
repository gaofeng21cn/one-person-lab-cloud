# Archived Evidence Receipts

These receipts moved out of the active `docs/evidence/source-checks/` tree under
the evidence-archival rule in
[the documentation lifecycle policy](../../policies/docs-lifecycle-policy.md#evidence-archival).
They are historical provenance, not current evidence. Their bytes, hashes,
original claims and verification timestamps are unchanged, and no path recorded
inside a receipt or release manifest was rewritten.

Archived batches:

| Batch | Baseline | Archived | Kept in `docs/evidence/source-checks/` |
| --- | --- | --- | --- |
| 1 | `f736a597` | 85 | 175 |

## Batch 1 — 2026-10-25

The directory held 260 entries at the baseline. Each entry was checked against
every tracked file for its full path and its bare filename, then for a release
manifest or receipt hash binding and for directory enumeration by a tool or test.

| Check | Result |
| --- | --- |
| Full path referenced by a tracked file | 158 kept |
| Bare filename referenced by a tracked document | 17 of the remaining 102 kept |
| No path or name reference, no release or hash binding, no enumeration | **85 archived** |

Archived: 74 receipts from 2026-09 and 11 from 2026-10. `docs/evidence/local-qualification/`
and `docs/evidence/source-checks/fixtures/` were outside this batch. A receipt
kept in the active tree because a document names it remains current evidence for
that document; archival never turns an old pass into current evidence.
