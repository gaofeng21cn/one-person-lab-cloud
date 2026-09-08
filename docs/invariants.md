# OPL Cloud Durable Invariants

This document contains only facts that must survive implementation changes.
Exact routes, DTO fields, stage names, retry counts, schemas, compatibility
decoders, and workflow steps belong to source, focused contracts, tests, or
current implementation documentation.

## Authority

- Control Plane owns customer sessions, account policy, Workspace business
  operations, settlement coordination, and customer-facing DTOs.
- Fabric owns provider-neutral compute, storage, attachment, Secret binding,
  Runtime facts, provider operations, and provider-authoritative readback.
- Ledger owns append-only receipts, evidence, retention, reconciliation, and
  caller-supplied opaque provenance.
- Sub2API owns customer identity credentials, API Keys, Usage, and the spendable
  wallet balance.
- An instance repository owns its domains, provider profile, production
  environment, Secrets, deployment, rollback, acceptance, and receipts.
- Each authoritative fact has one writer. Other modules consume it through a
  typed public contract or an owner readback.

## Physical Ownership

- Control Plane, Fabric, and Ledger remain separate processes, Go modules, and
  PostgreSQL schema owners.
- Cross-service integration uses typed HTTP APIs. A service reads and writes
  only its own tables.
- Console calls Control Plane product APIs. Provider, wallet, and Ledger access
  stays behind the owning server boundary.
- Shared infrastructure remains policy-free and serves at least two current
  owners.

## Identity And Secrets

- Customer access is authorized from the current session, Account, User, and
  downstream identity mapping. Operator visibility does not grant customer
  ownership.
- Protected reads and mutations require an unambiguous owner match from the
  authoritative source.
- Passwords, raw Keys, tokens, provider credentials, approval payloads, and raw
  downstream responses stay out of URLs, logs, audit payloads, Ledger, browser
  storage, and non-secret artifacts.
- Secret reveal is an explicit owner-authorized, private, no-store interaction.
  Ordinary status projections remain redacted.
- A Workspace Gateway Key is persisted only by the selected Fabric secret
  owner. Control Plane and Ledger retain references, not the raw Key.

## Money

- Customer prices and wallet mutations use integer USD micros. Provider costs
  are reconciliation evidence, not a customer pricing formula.
- A Workspace purchase or renewal confirms at most one customer debit for the
  accepted period price. Compute and storage are fulfillment of that purchase.
- Sub2API must atomically reject an insufficient debit without consuming its
  transaction identity. A used transaction must represent the full requested
  amount; clamping a debit to the available balance violates settlement.
- Confirmation binds the original transaction code, account, USD amount and
  authoritative used status and applied amount committed with the wallet write.
  Historical transactions without that fact remain unverified; a current
  balance or a version cutoff cannot certify their original applied amount.
  Current wallet snapshots are presentation or
  historical evidence, not proof of one transaction amid other consumption.
- Business refunds belong to the original confirmed charge and its account.
  Persist the refund reservation atomically with the operation; completed and
  unresolved refunds together cannot exceed that charge. Only a confirmed
  pre-dispatch rejection releases its reservation. Dispatch and recovery compare
  persisted operation state so a stale process cannot repeat a monetary write.
- Operations that can change money use stable idempotency identities and retain
  enough evidence to distinguish confirmed, absent, and unknown outcomes.
- A confirmed absence of all billable fulfillment after a debit may authorize
  one idempotent refund. Partial, conflicting, or unknown evidence requires
  review before another monetary mutation.
- Receipt failure retries the receipt only; it does not repeat an already
  confirmed debit, refund, provider operation, activation, or renewal.
- Financial reconciliation starts from retained original purchase, renewal and
  refund operations. Workspace deletion, provider absence or a newer period
  cannot remove historical settlements from the audit set. Each transaction
  and receipt binds the original account, operation, amount and period.
- Normal unfinished settlement is reported as pending, never matched. Missing
  or conflicting evidence for a completed or manually reviewed settlement is
  an exception. Reconciliation observes money and fulfillment; it never repeats
  a payment, refund or provider mutation to manufacture a match.

## Workspace Lifecycle

- Ending an unfulfilled Launch preserves its original authorization and order.
  Freeze and normal dispatch share owner serialization. Key revocation and exact
  resource absence precede refund; previous and closing refunds share the same
  original-charge reservation. A ready-before-freeze Workspace is never closed
  as failed. Unknown outcomes cannot become absence or payment confirmation.
- Launch, renewal, Key rotation, Runtime repair, and deletion are durable
  Control Plane operations over one Workspace identity.
- Recovery continues the original operation and original resource identities.
  It cannot create a second purchase or silently replace an already confirmed
  resource.
- An authorized image revision for an existing unready Runtime preserves the
  Launch, Runtime operation, service identity, stage request hash, idempotency
  key, and all non-Runtime resources; only owner-authoritative READY readback
  may advance the original Launch.
- Before an external write, the owning operation persists its identity and
  idempotency binding. An uncertain result converges through owner-authoritative
  readback before another write is considered.
- Workspace deletion removes owned Runtime, Secret, attachment, storage,
  compute, and Key state through their respective owners before removing the
  Workspace projection. Delete is independent from refund and performs no
  automatic wallet mutation.
- Operations that can mutate the same Workspace-owned resource serialize on the
  owning durable state.
- Historical rows needed for current reads or migration validation remain
  readable until their real consumers and data obligations are retired.

## Provider Resources

- Provider selection and concrete resource policy come from the active instance
  profile. Provider-specific behavior stays inside the Fabric adapter.
- Customer and verification compute/storage procurement uses the approved
  prepaid monthly policy. Capacity and price checks are read-only.
- Provider mutations bind the authorized caller, exact target, operation,
  allowed action, request integrity, and expiry.
- Provider absence, ownership, and completion come from the provider authority.
  Ambiguous or conflicting facts require review before another mutation.
- Instance-designated system resources remain outside customer allocation and
  cleanup.

## Console

- Console presents current owner-backed account, wallet, Workspace, Usage,
  receipt, and failure facts.
- Independent sources load independently. A failed source does not erase valid
  facts from another source.
- `empty` represents a successful authoritative read with no rows;
  `unavailable` represents a failed authoritative read and carries no invented
  zero value.
- Sensitive values are masked by default and revealed only through the owning
  command.
- Accessibility and clear responsive interaction are product requirements;
  visual details remain implementation choices.

## Release And Production

- Cloud publishes portable product artifacts. The instance owner deploys and
  qualifies an immutable Cloud candidate in its own protected environment.
- Publication and deployment bind exact source commits, image digests, caller
  authority, and owner readback. A build or test result proves only its own
  layer.
- Production credentials are available only inside the smallest authorized
  publish or instance-mutation boundary.
- Local development does not connect directly to production-private services.
- Production runtime artifacts contain no project test programs, fixtures,
  test datasets or test-only routes and startup hooks. Tests run outside the
  deployable artifact; a disabled test switch does not establish isolation.
- Test accounts, wallet entries, orders, Workspaces, Keys and receipts stay in
  independently configured non-production stores and resources. Deployment
  never imports test database snapshots, seeds, volumes or verification state.
- Qualification reports stay in engineering evidence storage, not the
  production business Ledger. Production readback observes actual deployed
  state and natural business; verification must not create synthetic business
  records to manufacture evidence.
- Ordinary verification is read-only with respect to customer billing and
  provider resources. Real mutations require separate explicit authorization.

## Evidence Levels

- `code-complete` means the exact source revision passed its required local
  source, build, persistence, and integration gates with no required skips.
- `pilot-ready` additionally requires approved real Gateway, Runtime, provider,
  billing, and browser evidence for that revision.
- `production-proven` additionally requires the same immutable revision to be
  deployed and read back by the production owner.
- Evidence is reported at the layer actually observed; lower-layer evidence
  does not imply a higher layer.
