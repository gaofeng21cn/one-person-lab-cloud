# OPL Ledger

OPL Ledger is the evidence record for OPL Cloud work.

It records what happened, which inputs and environments were used, which outputs
were produced, what checks ran, and how the work can be reviewed or continued
later.

## Receipt Shape

Every meaningful App action, Workspace action, or Cloud-managed job should be
able to leave a receipt:

```text
plan → approval → command/code → environment → input refs → output refs → reviewer result → owner → continuation entry
```

## What Ledger Owns

- Job receipts.
- Artifact provenance.
- Reviewer checks.
- Policy decisions.
- Audit records.
- Continuation refs.

## Evidence Record View

Receipts should be useful to people, not only machines. A human-readable record
should answer:

- what was requested;
- who approved it;
- what ran;
- which inputs and environments were used;
- which artifacts were produced;
- which review checks ran;
- what the result was;
- who owns follow-up;
- where the work can continue.

## Retention And Continuation

Ledger should keep enough information to support audit, review, handoff, and
later continuation. Retention policy belongs to Console, while source data and
artifact storage remain with the owning storage or domain system.

## What Ledger Does Not Own

Ledger is not the file store, database, model provider, runtime scheduler, or
domain-quality authority. It records references and receipts from the owning
systems.

## Reviewer Gate

Reviewer gates should stay domain-aware:

- MAS: citation, statistics, figure-code, and manuscript consistency.
- MAG: funder fit, eligibility, compliance, and budget fields.
- RCA: chart data source, transformation, and narrative consistency.
- BookForge: chapter continuity, citation coverage, style consistency, and
  export readiness.
