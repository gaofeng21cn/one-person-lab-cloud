# OPL Console Experience Guide

Owner: `OPL Console`

Purpose: `evolvable_product_experience_guidance`

State: `current_truth`

This guide defines the user outcomes expected from the generic OPL Cloud
Console. It is not a visual freeze or a machine contract. Current routes,
components, layout and styling are owned by `apps/console-ui`; product facts
and API authority remain with their domain owners.

## Product Outcome

The Console should make a signed-in user understand, without architecture
jargon:

1. that this is OPL Cloud;
2. which Workspace, OPL Gateway, balance and billing tasks are currently available;
3. whether each displayed fact is available, empty or unavailable;
4. what action can be taken next and what the result was.

Public entry and login surfaces introduce the generic OPL Cloud product. They
must not hard-code medopl or another instance identity; instance branding and
deployment facts belong to the instance owner.

The desktop customer destinations are `概览`, `工作空间`, `OPL Gateway`, and
`费用`, in that order; mobile uses the shorter `Gateway` label for the same
destination. Messages stay in the top bar and account facts in one account
menu. Gateway has local Service Information, Usage and API Key destinations;
Billing separates subscription terms from transaction history. The platform
brand remains `OPL Cloud` with its canonical `public/opl-app-icon.png` asset.

The account menu presents email, customer identity, account status and safe
logout. Internal account, user and Session IDs are not customer tasks. Account
and mobile navigation overlays are mutually exclusive and preserve focus.

## Experience Principles

- Prefer user tasks and outcomes over internal service, contract or authority
  vocabulary.
- Keep operational surfaces professional, coherent and efficient for repeated
  work. Visual quality is evaluated in the context of the current product and
  viewport, not by conformance to a historical palette or screenshot.
- Use a clear responsive hierarchy. All supported actions and critical facts
  must remain reachable on desktop and mobile.
- Make loading, empty, unavailable, partial-failure and permission states
  explicit. Never present an unavailable fact as zero, empty or healthy.
- Keep independent sources independently usable. A failed balance read must not
  hide a valid Workspace list, for example.
- Use accessible semantics, keyboard interaction, focus management, readable
  contrast and non-color status cues.

## Customer Task Hierarchy

| Surface | Required customer outcome and information priority |
| --- | --- |
| Overview | Read existing balance, monthly actual API cost/request count and Workspace total, then the currently loaded Workspace with its available next action; recent fees and messages are secondary |
| Workspace list and launch | Find a Workspace or configure, review the actual amount due, explicitly confirm and submit once; distinguish preparing, manual review, failed, refunded and unconfirmed results without inventing refund or completion |
| Workspace detail | Availability and Open first, then credentials, plan/renewal/storage and advanced budget controls; maintenance resets remain distinct and Delete has its own confirmed high-risk group |
| API Keys | Current Key identity, status, limits, expiry and usage first; create, reveal/copy and usage instructions stay reachable; edit, enable/disable, the two distinct usage resets and Delete retain their permissions and confirmations |
| Gateway Usage | Current Key and period contextualize requests, total Tokens and actual cost; records show time, model, input/output Tokens and cost; searchable paginated selection does not become another Key-management surface |
| Subscriptions | Desktop and mobile expose Workspace identity, plan, owner monthly amount, the complete billing period, renewal and auto-renew state before entering detail; missing period endpoints or unknown flags stay explicitly unavailable |
| Transactions and Receipt | Read-only history and detail expose business type, status, date, amount, billing period and Workspace name/number; failure to resolve a Workspace name must not hide a valid Receipt |

Mobile results and primary actions precede filters and low-frequency controls.
Long names, identifiers, dates and complete amounts remain readable without
horizontal page overflow; fixed navigation cannot cover actions. Controls keep
visible focus, meaningful accessible names and usable touch targets. The
current visual baseline uses restrained neutral surfaces, blue for primary
actions/focus, semantic status colors and the existing system fonts/tokens.
Normal text maintains accessible contrast; style values remain owned by CSS.

Technical disclosure needs a current diagnostic purpose. Workspace and Usage
can expose their relevant original evidence on demand; Usage records retain
API path, cache Tokens, latency and copyable request ID. A metering record does
not prove request success. The ordinary Receipt UI contains no Ledger technical
disclosure: internal Receipt ID, price version, debit references, provider
components and fulfillment facts stay in the owning service/DTO. Their presence
in an API is not a reason to add customer UI. Support tickets are retired and
must not appear as a next action for failed customer tasks.

## Truth And Safety

- The browser calls only Control Plane product APIs. It does not call Fabric,
  Ledger, Sub2API or provider APIs directly.
- Display service-provided money, time, status and price snapshots without
  inventing business truth in the browser.
- Do not synthesize trends, capacity, health, ETA or completion facts when the
  owning API does not provide them.
- Passwords and keys are hidden by default. Reveal requires an explicit owner
  action, uses a private/no-store response and must not persist in browser
  storage, logs, receipts or ordinary page state beyond the active sensitive
  interaction.
- Do not expose internal provider cost, credentials, raw downstream admin DTOs
  or management endpoints.

The canonical field, source and permission details live in the public API
schemas and eligible contracts under `packages/contracts`; this guide does not
copy their field lists.

## Design Freedom

Visual design and implementation choices belong to `apps/console-ui` and evolve
with current content, brand, accessibility, devices, and user workflows. A
product-level update is needed only when the user outcome, authority boundary,
or durable experience principle changes.

## Capability And Currentness

Functional module documents describe intended user capabilities. Source and
API schemas describe current implementation. `docs/status.md` reports current
evidence, while `docs/roadmap.md` owns missing capabilities and acceptance
outcomes. A visual treatment must not imply that a roadmap target is already
available.

## Verification

For a meaningful Console change, verify the affected user path at representative
desktop and mobile viewports, plus focused behavior, accessibility and source-
truth tests. Screenshots are review evidence, not immutable product contracts.
Do not add exact CSS-value or image-hash assertions unless a separate legal,
brand-integrity or safety requirement specifically justifies them.
