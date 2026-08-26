# Console Customer Announcement Read Boundary

## Decision

Extract the customer announcement read lifecycle from the broad
`useConsoleController` into one narrow `useCustomerAnnouncementController`.
This is an internal Console ownership change. It preserves the current Control
Plane routes, DTOs, customer-visible content, Overview limit of 3, announcement
list limit of 20, mark-read command, navigation, and rendered behavior.

## Owner

Control Plane remains the domain-fact owner for active published announcements
and each user's durable read receipt. The Console capability owns only the
browser lifecycle:

- the active customer announcement projection;
- the Overview/list query scope and request freshness;
- one mark-read intent and idempotency key per announcement;
- the in-flight claim and busy projection;
- command-response validation, current-scope refresh, and reset.

The composition root retains Session, Router, route orchestration, and toast
injection. `CustomerPages` remains a presenter.

## Why This Slice

The current root combines two query shapes and a user mutation. A mark-read
started on Overview can complete after navigation and start a 3-item read using
the new global request generation, overwriting the 20-item list projection.
The root also ignores the typed command response and generates a new mutation
key for every retry.

Operator publication is not reused: it owns global draft/schedule/publish/
withdraw transitions, while customer read state is scoped by user identity,
uses different routes and authorization, and has a different failure model.
A generic route-source controller would hide rather than remove these owner
differences.

## Contract And Flow

`CustomerAnnouncementController` exposes one typed remote projection,
`busyAnnouncementId`, `refresh`, and `markRead`. Composition-only `load` and
`reset` remain private to the root integration.

The route scope is `overview`, `list`, or inactive. `load` binds the expected
page size to its query generation and rejects mismatched pagination. A late
Overview request cannot commit into the list scope and vice versa.

`markRead` reuses the same idempotency key for the same unresolved announcement,
validates that the Control Plane receipt names that announcement and contains a
valid RFC3339 `readAt`, and refreshes only the currently active customer scope.
If the refreshed projection still contains the target, it must report
`read=true`. The command receipt remains the authoritative read fact when the
target has concurrently left the active or bounded projection.

Route changes invalidate query completions. Session replacement and reset
invalidate both query and mutation completions and clear intents. An in-flight
claim is released only by the request that owns it, so a stale completion
cannot release a newer claim or commit state.

## Invariants

- Overview reads page 1 with page size 3; the list reads page 1 with page size
  20.
- A query result commits only to the exact current route scope and Session.
- A mark-read receipt must match the requested announcement and a valid time.
- An unresolved same-announcement retry reuses its original idempotency key.
- A visible target after refresh must be `read=true` before the intent clears.
- Route, Session, reset, and unmount boundaries cannot restore stale state.
- Customer and Operator announcement state, intents, claims, and reset remain
  independent.

## Migration

1. Add typed pure-model tests for intent reuse and receipt/readback matching.
2. Add browser tests for 3/20 scope isolation, response loss, route changes,
   Session reset, and response identity mismatch.
3. Add the typed controller contract and owning hook.
4. Compose it from the root and switch both customer pages.
5. Delete `sources.announcements`, `announcementBusy`, `loadAnnouncements`,
   `markRead`, and their root reset path.
6. Reconcile current status and roadmap package F.

## Non-goals

- No backend, HTTP, DTO JSON, persistence, or schema change.
- No merge with Operator Announcement Lifecycle.
- No pagination UI, polling, notification center, or edit feature.
- No generic route-source framework or compatibility fields.
- No deployment, PostgreSQL, Tencent TKE, or Instance receipt.

