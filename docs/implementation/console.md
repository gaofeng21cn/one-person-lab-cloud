# Console Browser Lifecycle

This reference owns current browser composition and state semantics. Product
outcomes belong to [the experience guide](../product/console-experience-guide.md);
durable facts remain in Control Plane, Fabric, Ledger and Sub2API.

## Composition

`console-router.ts` owns the typed `ConsoleRoute`: route kind, normalized path,
surface, title, Session requirement, sensitivity and navigation identity. The
root loader and page dispatch switch exhaustively on that fact; navigation
consumes its identity. Unknown paths and malformed Workspace IDs remain
unknown. Route aliases resolve through the same parser rather than maintaining
another route list. `/admin/announcements` composes the existing announcement
controller and loads that projection independently of operator Overview.

[`use-console-controller.ts`](../../apps/console-ui/src/app/use-console-controller.ts)
composes Session/Auth, Router, global toast and shell state, cross-owner route
loading, reset and dependency ordering. Operator Overview, Reconciliation and
Health are single-request projections. A capability hook receives narrow typed
ports rather than the complete root controller, root setters or sibling state.
The current exports in that directory own the capability inventory. Session was
not extracted first because its reset boundary spans every capability; moving it
without explicit child reset contracts would merely relocate the broad coupling.

Each extracted capability owns its intent, busy claims, request freshness,
command validation, readback and reset. A new extraction requires an independent
selection, pagination, timer, stale-write or failure lifecycle; file size alone
is not a reason. Session changes invalidate every protected capability before
new data can commit.

## Query Isolation

A response commits only for its current Session, route, request generation and
selected identity. Pagination parameters and source identity must match.
Independent sources settle independently: Runtime failure cannot replace
Workspace detail; Usage failure cannot hide a valid Summary; operator detail
and replacement-preview errors cannot overwrite each other.

| Query owner | Scope and completion |
| --- | --- |
| Customer Workspace Read | Overview, list, detail and Billing terms; owns the monotonic detail projection lease and committed page |
| Fabric Runtime Read | Active customer Workspace; requires Fabric source and exact Workspace identity; starts independently of detail |
| Gateway Account Read | Wallet, monthly account usage, endpoint and 20-item balance-history pages; a failed page never advances the committed page |
| Gateway Usage | Searchable/paginated Key query, independently selected Key, period/committed Usage page and separate Usage/Summary generations; only authoritative single-Key absence clears an established selection |
| Billing / Receipt | Ledger list/detail, selected Receipt and opaque cursor stack; Overview requests 3 without adopting Billing cursor state, Billing requests 20 |
| Operator Resource Read | Resource page, selected Workspace detail, image policy and replacement preview; each projection has independent identity/freshness and failure settlement |

The root owns the independent Workspace Budget lease and starts budget lookup
only after accepting Workspace detail and its Key identity. Renewal readback
may commit only through the current detail lease; Budget may commit only through
its budget lease. `findWorkspaceInPages` remains a thin API adapter used by each
owning command, not another Workspace state owner.

Gateway Usage Key search and pagination only choose candidates; they cannot
change the active Usage range. Explicit Key selection or period changes hide
the old range and load Summary and Usage separately. Page refresh confirms the
selected Key through single-Key readback, then loads Summary and Usage page 1.
An authoritative `404` clears the selection; transient failure retains its
identity but hides stale results. Each source retries independently, and Usage
pagination commits its new page only after an identity-matching success,
without reloading Summary. Search explicitly submits, starts at page 1 and
retains its query across pagination; late dialog responses cannot change the
committed range. An initial authoritative empty collection leaves no selection
and does not request Usage.

`customer-experience-model.ts` and `workspace-experience-model.ts` own pure,
exact presentation mappings. Unknown values remain unconfirmed and unavailable
values remain unavailable. Presenters do not decode APIs, calculate billing,
poll, retain Secrets or own command state. Workspace and Gateway Usage page
components compose these projections; `CustomerPages` remains their route
composition layer.

## Commands And Authoritative Completion

Unresolved same-input mutations retain their idempotency identity across response
loss. Typed response identity and the capability's authoritative completion must
both match before clearing an intent. This browser rule does not imply that every
backend command implements server-side replay.

| Command owner | Required completion |
| --- | --- |
| Workspace Launch | Stable input/key, zero or one recoverable operation, bounded polling, then authoritative paged Workspace confirmation before navigation; `manual_review`, terminal and refunded results stop polling |
| Workspace Delete | Paged Control Plane Workspace read proves final absence; no refund or wallet conclusion is inferred |
| Workspace Renewal | Returned setting and authoritative Workspace projection match `autoRenew`; command scheduling and lifecycle projection remain separate |
| Workspace Budget | Exact Workspace/Key and requested stable policy fields match the Gateway owner readback |
| Operator Account | Provision/disable/purchase-eligibility response and authoritative paged Account projection match target identity and fields |
| Wallet Adjustment / Recovery | Typed wallet operation reaches terminal or manual review and its operation/account readback is refreshed |
| Operator Announcement | Create/publish/withdraw response and operator collection agree on identity, content, schedule and target state |

Launch forces `autoRenew=false` for `resourceBillingMode=none`. Its hook and
focused model own polling limits; the browser never reconstructs the server
stage machine. Delete, Renewal and Budget keep independent intents, busy and
freshness state. The detail view cross-disables conflicting Delete/Renewal
commands. Route changes invalidate pending completions without discarding
unresolved per-Workspace intents; Session replacement clears them.

Launch confirmation takes the amount due from `selectedPrice`, not the catalog
component preview. Confirmed entitlement mode requires zero due; billed mode
requires a positive due amount. Missing or contradictory input cannot submit.
Unknown results, ambiguous recovery operations or missing success identity
retain the original intent and offer status recheck, not another purchase.
Opening a Workspace requires its current owner Runtime projection to be
running, ready and have a URL. Budget forms convert customer USD decimals to
the existing exact micro-dollar request boundary and reject invalid input.

Operator announcement claims survive route exit until their request settles;
route and Session generations reject stale completion. Views retain dialog and
draft fields only. Wallet Adjustment receives the narrow Account refresh port
and does not acquire Account lifecycle state.

## Customer Announcement Receipts

Customer announcements have a separate owner from operator content mutation.
Overview reads page 1 with 3 items; the list reads page 1 with 20. A receipt must
name the requested announcement and carry a valid RFC3339 `readAt`. Once accepted,
it completes the intent and projects the announcement as read for that Session.
A later GET failure preserves that fact. Absence from a bounded active collection
is valid, but a visible `read=false` conflict cannot commit.

Per-announcement unresolved intents, one active claim and scope generations are
owned by `useCustomerAnnouncementController`. Session reset clears them and
replaces the claim namespace, so an old `finally` cannot release a new claim.

## Ephemeral Workspace Secrets

`useWorkspaceSecretController` owns one visible Runtime credential or Workspace
Gateway Key, mutual exclusion, busy state, the 60-second lifetime and request
generation. Route, Session, selected Workspace, reset and unmount clear the
Secret and invalidate pending completions. Only an identity-matching owner
response can reveal it; late completion cannot restore cleared data.

Runtime rotation retains its idempotency key after an unknown result. A valid
response displays the returned credential and refreshes authoritative Workspace
detail. Clipboard is a transient interaction; raw values never enter browser
storage, logs, receipts or ordinary persisted state. Gateway Key CRUD remains
with its own panel because its pagination and mutation lifecycle differ.

## Verification Sources

Pure models and `tests/ui/*controller-model.test.ts` cover identity, intent and
freshness decisions. The corresponding browser tests verify navigation,
response loss, independent failures, Secret cleanup and real rendered consumers.
Use [the developer guide](../../DEV_GUIDE.md) for the current local commands.
