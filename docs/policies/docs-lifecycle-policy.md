# Documentation Lifecycle Policy

This repository applies the OPL Doc method through the hierarchy in
`docs/README.md`. The policy governs semantic ownership; it does not prescribe
one file layout for every topic.

## One Topic, One Current Owner

Classify each changed section as `current_truth`, `active_gap`,
`support_detail`, `history_or_provenance`, or `stale_or_conflicting`.

- Keep one current owner for each semantic topic.
- Reduce other active documents to a pointer plus unique support detail.
- Put all open work in `docs/roadmap.md`; put current evidence in
  `docs/status.md`.
- Move dated plans, design freezes, screenshots, execution logs, raw
  verification output and completed ledgers to `docs/history/**` or rely on Git
  history when no non-resurrection record is needed.
- Delete stale or conflicting text after its successor and callers are proven.

## Creation And Retirement

Before adding a document, identify its reader, one question, current owner and
the existing document it replaces or complements. Update an existing owner when
the question is already covered. Keep target, implementation reference,
operating procedure, current evidence and open plan in separate documents;
summaries link to the owner rather than copying its rules or field inventory.

Update the owning text in place when behavior changes. Replace incremental
completion lists with a cohesive current explanation. Status snapshots name an
observation date and exact evidence identity; a commit described inside a file
is a checked baseline, never a self-updating claim about `main`.

At completion, transfer durable rationale and unique safety or data obligations
to their current owner, then remove task prompts, command transcripts, migration
checklists and obsolete test inventories. Git preserves routine execution
history. Keep a historical record only for useful decision provenance, legal
custody or a plausible reintroduction risk; mark it historical and link its
successor. Unimplemented accepted decisions remain explicit roadmap outcomes,
not falsely completed history.

Retire stale module/API/test prose together with obsolete references, navigation
and unused documentation assets. Do not leave aliases or compatibility pages.
Actual live callers, published artifacts and persisted financial/resource
obligations must be identified before retiring their implementation: a prose
cleanup cannot establish that an external consumer or stored resource is absent.

Review these boundaries whenever the owning source, public contract, release
format or product decision changes. No separate recurring audit ledger, keyword
gate or prescribed document count is needed.

## Downward Reconciliation

An upper-level change must identify affected lower projections. Reconcile them
in the same change when possible. If implementation cannot yet follow a target
decision, preserve the target, report the current implementation honestly and
record one roadmap gap. Never weaken the target to match accidental current
code, and never claim the target is implemented from prose alone.

## Machine Contract Admission

Machine-readable contracts live in `packages/contracts/**`. A fact belongs
there only when all of the following are true:

1. It has one named authority owner.
2. A current runtime, cross-module caller, public interface, security rule,
   data-integrity rule or irreversible side effect depends on it.
3. Deterministic validation is more valuable than leaving the decision to the
   owning implementation.
4. The contract does not duplicate a schema, source constant, workflow or
   another contract that already owns the fact.

Colors, spacing, layout dimensions, page or slide counts, component libraries,
model choices, query strategies, batch sizes, concurrency tuning, worker
intervals, file paths, command sequences, current progress and pending evidence
do not qualify merely because tests can assert them. Use an evolvable guide,
source, API schema, performance test, workflow, `docs/status.md` or
`docs/roadmap.md` instead.

## Tests

Long-term tests should validate public behavior, authority, accessibility,
security, integrity and side-effect bounds. Source or workflow shape belongs in a
test only when that shape is itself a consumed contract.

Migration and cleanup tests are temporary. Move active callers and persisted
state to the current surface, then retire the old wrapper, route, fixture and
test together.
