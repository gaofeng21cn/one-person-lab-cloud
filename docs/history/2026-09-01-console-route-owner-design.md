# Console Route Owner Design

Date: 2026-09-01

State: approved design record

## Objective And Ownership

UX-01 makes the current Console route the single typed fact that determines
surface, title, data-loading plan, rendered page, sensitivity, and navigation
state. It also repairs the known `/admin/announcements` route so that it renders
the existing announcement-management capability instead of falling through to
the operator overview.

- Primary module owner: `apps/console-ui`.
- Product guidance owner: `docs/product/console-experience-guide.md`.
- Current implementation owner: `apps/console-ui/src/app/console-router.ts` and
  its focused tests.
- Domain contract impact: none. The Console continues to call only current
  Control Plane product APIs and does not change any DTO or service authority.
- Real consumers: the root Console controller, `App`, `ConsoleShell`,
  `CustomerPages`, and `AdminPages`.

## Problem

The same route is currently interpreted independently in multiple places:

- `console-router.ts` decides whether a path is known or sensitive;
- `use-console-controller.ts` derives feature scopes, data loading, titles, and
  customer/operator surface from path strings;
- `ConsoleShell.tsx` separately derives navigation selection;
- `CustomerPages.tsx` and `AdminPages.tsx` separately choose the rendered page;
- `console-model.ts` contains another set of API and Workspace route helpers.

These copies already disagree. `/admin/announcements` is accepted, titled, and
loaded, but `AdminPages` has no matching page and therefore renders the operator
overview, whose required overview source was not loaded. This is an experience
failure caused by split ownership, not a visual styling problem.

## Decision Logic

The product-design sequence is:

```text
business outcome -> user task -> route state -> required authority reads
-> rendered interaction -> visual treatment -> verification
```

Route ownership is the first implementation package because reliable UI/UX
requires the interface to show the correct task and authoritative state before
language, density, or visual polish can be evaluated. Styling a route that can
load the wrong page would preserve the underlying failure.

Three approaches were considered:

1. **Typed route owner (selected).** One discriminated union and parser provide
   the route kind and metadata consumed throughout the Console. This directly
   removes the observed disagreement without adding a framework.
2. **Generated router or third-party routing framework.** This could provide a
   larger routing abstraction, but the current application has a small static
   route set and no observed need for nested loaders or a new dependency.
3. **Patch `/admin/announcements` only.** This repairs the visible symptom but
   leaves the independent route writers in place and cannot prevent the next
   title/loader/page mismatch.

## Design

### Typed Route Fact

`console-router.ts` will own a `ConsoleRoute` discriminated union. Every parsed
route contains:

- a stable `kind` used for exhaustive branching;
- its normalized `path`;
- its `surface` (`public`, `customer`, or `admin`);
- its user-visible `title`;
- whether it requires a Session;
- whether leaving it must clear sensitive state;
- its navigation identity, when the route belongs to product navigation;
- the decoded Workspace ID only for a Workspace-detail route.

`parseConsoleRoute(path)` returns one of these route values or `null`. It owns
trailing-slash normalization, the legacy `/console/gateway` alias, static-route
matching, exact one-segment Workspace detail matching, and safe URL decoding.
Unknown or malformed paths remain unknown; they are not coerced into a nearby
page.

Compatibility predicates such as `isKnownConsoleRoute` and
`isSensitiveConsoleRoute` may remain as thin consumers of the parser while
current callers are migrated. They must not contain their own route lists.

### Controller And Loading

The root controller receives both normalized `path` and parsed `route` from the
router. Feature scopes, the active Workspace ID, authorization, page title,
sensitivity, and `loadRoute` are derived from `route.kind`. `loadRoute` uses an
exhaustive switch, so adding a route without an explicit loading decision fails
type checking.

The root remains the Composition Root. This change does not extract another
feature controller, move data authority into the browser, or alter any existing
freshness and readback lifecycle.

### Page And Navigation Dispatch

`App` selects the public/customer/admin surface from the typed route.
`CustomerPages` and `AdminPages` switch exhaustively on the route kind allowed
for their surface. A shared `assertNever`-style check prevents a new route from
silently falling through to another page.

Navigation items carry a stable navigation identity. Desktop and mobile active
state compare that identity with `route.navigationId`; path-prefix regular
expressions no longer form a second routing system. Workspace detail remains
active under the Workspace item, and all API subroutes remain active under the
API item. Announcement management is added to operator navigation as a
lower-frequency desktop item; it is intentionally omitted from the fixed mobile
primary bar while remaining reachable from the operator overview.

### Announcement Management

The existing `AnnouncementList`, `AnnouncementDraftModal`, and
`OperatorAnnouncementController` remain the single announcement capability.
An `AnnouncementsPage` composes those same parts for
`/admin/announcements`. The overview may continue to show its current compact
management section, but no state, API call, or mutation logic is copied.

## Failure Handling

- Unknown and malformed paths render the existing not-found page.
- A customer Session cannot access an admin route.
- A known route with an unavailable owner read renders its existing explicit
  unavailable state.
- `/admin/announcements` loads only its announcement projection and does not
  wait for the operator-overview projection.
- Existing secret-clearing and request-generation behavior is preserved.

## Scope

Included:

- route parsing and metadata ownership;
- controller, page, and navigation consumption;
- the broken operator announcement route;
- focused route-matrix and browser acceptance evidence.

Excluded:

- terminology and content redesign across the full Console;
- customer/admin information-architecture restructuring;
- `KeysPanel` controller extraction;
- CSS decomposition or broad visual restyling;
- new APIs, DTOs, state frameworks, services, or persistence.

Those changes follow only after the current route-to-task chain is trustworthy.

## Completion Evidence

The package is complete when:

1. a focused matrix covers every static route, Workspace detail, aliases,
   trailing slashes, sensitivity, titles, surfaces, and unknown paths;
2. every route kind has an explicit loader and explicit page dispatch;
3. `/admin/announcements` renders announcement management at desktop and mobile
   viewports without requesting or waiting for operator overview;
4. focused tests, TypeScript checks, production build, and
   `npm run verify:local` pass;
5. the verification result is recorded under `docs/history/**` and the local
   branch contains only the UX-01 write set.
