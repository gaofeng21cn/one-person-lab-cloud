# OPL Ledger

Owner: `one-person-lab-cloud`
Purpose: `ledger_target_reference`
State: `active_target_reference`
Machine boundary: Human-readable target evidence reference; runtime and
production evidence come from Ledger source, tests, status, and owner readback.

OPL Ledger is the target evidence-record capability for OPL Cloud work.

It records what happened, which inputs and environments were used, which outputs
were produced, what checks ran, and how the work can be reviewed or continued
later.

Ledger records receipts and provenance while MAS, MAG, RCA, BookForge, OPL App,
and other domain owners retain source truth, quality judgment, and delivery
authority.

## Receipt Shape

Every meaningful App action, Workspace action, Serve deployment or invocation,
or Cloud-managed job should be able to leave a receipt:

```text
plan → approval → command/code → environment → input refs → output refs → reviewer result → owner → continuation ref
```

## What Ledger Owns

- Append-only job and Workspace receipts.
- Reconciliation evidence and idempotency for Ledger writes.
- Receipt retention and privacy lifecycle operations.
- Opaque provenance fields supplied by the calling owner, including artifact,
  review, output, reviewer-check, and continuation refs.

For skill-first flows, Ledger should record which main skill, enhancement pack,
connector, input refs, selected sources, outputs, and continuation entry were
used as opaque provenance. This gives MAS, Workspace, App, and other callers a
shared evidence trail without moving domain truth or continuation authority into
Ledger.

For Serve flows, a receipt should connect exact package digest, service,
revision, deployment, consumer-policy, provider-session, resource, model-usage,
input, output, artifact, review, cost and continuation refs where applicable.
Ledger persists these as caller-owned opaque provenance; provider secrets and
event/session state remain with their owners.

Gateway remains the spendable-balance owner; Control Plane owns account-total
billing and settlement policy, and Console presents it. Ledger records immutable evidence about
money movements and resource charges.

## Local No-Charge Receipts

Workspace may append a `LOCAL_NO_CHARGE` receipt only for a Catalog-accepted
quote whose total and every line amount are zero and whose frozen resource plan
selects the `local-docker` provider and `LOCAL_NO_CHARGE` billing mode. Ledger reads
the accepted quote back from Catalog and the original owner commit back from
Workspace before persisting the evidence. Both typed readbacks must match the
submitted evidence exactly. Catalog authorizes its own read from the session or
accepted operation grant; Ledger does not forward its audience-bound
authorization context.

The authenticated Workspace peer supplies the original order reference,
workspace, tenant, actor and owner commitment. Live CloudIdentity authorization
binds append and read actions to that workspace. Ledger assigns the receipt ID
and creation time and stores one immutable record in its existing evidence
store. Replaying the same evidence under the original order returns the same
receipt, including after restart or when the request key changes. Changing the
evidence for that order, or reusing a caller key for a different order, fails
with an idempotency conflict. A new insertion rechecks live authorization after
acquiring both database locks, so a revoked request cannot write after waiting
behind another append.

Workspace and Fabric read this evidence through `LedgerCoordination` using the
original Workspace reference. `ReadReceiptByReference` returns the public
receipt; the internal `ReadLocalNoChargeReceipt` additionally returns the typed
accepted quote and owner commitment needed for Fabric validation. The legacy
HTTP receipt writer, reader, listing and retention/privacy mutations cannot
access this new coordination receipt type. Existing legacy receipt types keep
their existing behavior.

The typed listener remains opt-in through `OPL_LEDGER_ADDR`. Its owner readbacks
use the existing CloudIdentity configuration plus `OPL_RESOURCE_CATALOG_ADDR`
and `OPL_WORKSPACE_ADDR` with their corresponding owner tokens and TLS policy.
Missing owner dependencies refuse the new coordination calls. A no-charge
receipt records an accepted zero-price obligation; it does not create a wallet
transaction, prove that resources exist, or authorize a provider action.

## MVP Boundary

Core Ledger is limited to receipts, reconciliation evidence, idempotency, and
receipt lifecycle operations required by the local Workspace plus Gateway
accounting path. Ledger does not own structured domain Artifact, Review,
ReviewPolicy or Continuation services. Persisted provenance and historical data
custody are documented in [implementation architecture](implementation-architecture.md#persistence);
they do not create a domain API or Workspace authorization. Current capability belongs
to [status](status.md), while any later owner decision belongs only to the
[roadmap](roadmap.md).

## Evidence Record View

Receipts should be useful to people, not only machines. A human-readable record
should answer:

- what was requested;
- who approved it;
- what ran;
- which inputs and environments were used;
- which artifact refs were supplied;
- which review-check refs were supplied;
- what the result was;
- who owns follow-up;
- where the work can continue.

## Retention And Continuation

Ledger keeps enough receipt data to support audit, handoff, and later owner-side
review. Retention policy belongs to the receipt lifecycle owner, while source
data and artifact storage remain with the owning storage or domain system.
`continuationId` and `continuation` are caller-supplied opaque provenance:
Ledger does not generate an identity, resolve a continuation, hide it on reads,
or authorize a Workspace operation.

## Review And Domain Ownership

Each domain owner defines and evaluates its review policies and turns review
outcomes into domain actions. Ledger records only opaque `reviewId`,
`reviewerChecks`, and related refs.

Examples of domain-owned review semantics:

- MAS: citation, statistics, figure-code, and manuscript consistency.
- MAG: funder fit, eligibility, compliance, and budget fields.
- RCA: chart data source, transformation, and narrative consistency.
- BookForge: chapter continuity, citation coverage, style consistency, and
  export readiness.


## Target owner boundary under product review

Ledger remains append-only evidence. It accepts opaque references from
Capability, Build, Runtime Control, Workspace, Serve, Fabric, Gateway, and
domain owners, and records receipts where the contract requires them. It does
not own Package bytes, Runtime Release catalog rows, OCI deployment state,
Workspace entitlement, Fabric resources, Agent readiness, routing, or business
Saga decisions.
