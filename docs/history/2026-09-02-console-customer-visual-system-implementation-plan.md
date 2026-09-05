# Console Customer Visual System UX-03C Implementation Plan

> **For the implementing agent:** execute this plan task by task with tests
> first. Preserve the existing Control Plane API, controller, source,
> idempotency, readback, Session, Secret, billing, Workspace, Gateway, and
> logout boundaries.

**Goal:** Implement the approved UX-03B customer Console reference slice for
shared Shell, Overview, account surface, Gateway identity, and Workspace
confirmation Checkbox without changing the already-running business flow.

**Architecture:** Keep the work inside the existing presentation owners:
`ConsoleShell`, customer `CustomerPages`, shared `Checkbox`, customer
presentation/route metadata, and affected CSS selectors. Control Plane remains
the only product API called by Console; Sub2API remains the internal wallet,
Key, Gateway, and usage authority. Change labels, hierarchy, affordances, and
responsive layout only—not DTOs, requests, state ownership, persistence, or
business decisions.

**Tech Stack:** TypeScript, React, existing Console UI components, existing CSS
tokens, Node test runner, Playwright, Vite, `npm run verify:local`.

---

## 1. Scope Gate

Inputs already accepted:

- UX-03A audit: `docs/history/2026-09-02-console-customer-ui-audit.md`;
- UX-03B visual direction: `docs/history/2026-09-02-console-customer-visual-system-design.md`;
- canonical platform asset: `public/opl-app-icon.png`;
- fixed business path: sign in → Overview → Workspace or OPL Gateway → fees,
  messages, and account management.

This phase must not change a Control Plane route/API/DTO/controller, database,
billing calculation, Workspace launch command, Gateway Key command, Session
command, Secret lifetime, logout safety rule, or authority boundary. Do not add
Support, a second wallet/Gateway/route/i18n/design framework, a global CSS
rewrite, Admin redesign, Gateway Key page redesign, release, deployment, PR,
merge, or Instance/production claim.

Reference viewports: desktop `1280x900`; mobile `390x844`.

## 2. Acceptance Contract

| Surface | Required result |
| --- | --- |
| Platform brand | Existing `OPL Cloud` name and `public/opl-app-icon.png`; brand still navigates to Overview |
| Desktop customer navigation | `概览` → `工作空间` → `OPL Gateway` → `费用` |
| Mobile customer navigation | `概览` → `工作空间` → `Gateway` → `费用` |
| Gateway page | Page title `OPL Gateway`; local destinations `服务信息` → `用量` → `API 密钥` |
| Top actions | Messages, route-owned refresh, one account trigger; no separate top-bar logout |
| Account surface | Email, customer identity, account status, safe `退出登录`; no account IDs/Sub2API fields |
| Workspace confirmation | Visible unchecked border, checked state, keyboard focus, semantic state, mobile-operable target |
| Overview order | Summary facts → primary Workspace region/action → recent fees/messages |
| Failure wording | Retry/return/unavailable actions; no retired Support wording or request |

## 3. Task 1 — Freeze Acceptance In Typed And Browser Tests

**Files:**

- Modify `tests/ui/console-model.test.ts`.
- Modify `tests/ui/customer-console-task-experience-browser.test.ts`.
- Modify `tests/ui/workspace-experience-model.test.ts` only for the retired
  Support expectation.
- Modify `tests/ui/workspace-task-experience-browser.test.ts` only where an
  existing customer-facing Support selector/text is asserted.

### Step 1: Update typed route/menu assertions

Keep all existing navigation IDs and paths. Assert desktop menu labels exactly
`概览`, `工作空间`, `OPL Gateway`, `费用`; assert mobile presentation labels
exactly `概览`, `工作空间`, `Gateway`, `费用`; assert all three API routes
retain `customer.api` ownership and title `OPL Gateway`; assert API tabs are
exactly `服务信息`, `用量`, `API 密钥`.

### Step 2: Add Shell assertions at both viewports

Assert canonical icon load and brand href; exactly four visible task links in
approved order; top bar has messages, refresh, and one account trigger but no
top-bar logout; opening navigation then account leaves no visible drawer/scrim
and account surface together; account shows email/`客户`/`正常`/safe logout but
not `Account ID`, `Console User ID`, `Sub2API User ID`, `Session ID`, or expiry;
no `/api/support/tickets` request occurs.

### Step 3: Add Checkbox and Overview assertions

Assert one summary region, one dynamic primary Workspace action, recent fees,
and messages without new requests or derived metrics. Preserve loading, retry,
empty, unavailable, view, and create action states. Assert the confirmation
Checkbox is visible unchecked, semantic checked, focus-visible, and keyboard
operable at both viewports. Assert customer failure copy has no `联系支持`,
`Support`, or retired route.

### Step 4: Run red tests

```bash
node --test tests/ui/console-model.test.ts tests/ui/workspace-experience-model.test.ts
node --test tests/ui/customer-console-task-experience-browser.test.ts
```

Expected: failures identify current API labels/title, top-bar logout, account
internals, overlay/Checkbox gaps, and/or retired Support copy. Do not weaken
these assertions to preserve the old UI.

## 4. Task 2 — Implement Route/Menu Presentation Identity

**Files:** `apps/console-ui/src/console-model.ts`,
`apps/console-ui/src/app/console-router.ts`, and the Task 1 model test.

Keep IDs, route paths, aliases, and route sensitivity unchanged. Add only the
presentation metadata needed for desktop `OPL Gateway` and mobile `Gateway`;
do not create a second route/owner. Set all customer API route titles to
`OPL Gateway` and the API tab to `API 密钥`.

Run:

```bash
node --test tests/ui/console-model.test.ts
npm run typecheck
```

## 5. Task 3 — Repair Shared Shell And Account Surface

**Files:** `apps/console-ui/src/layout/ConsoleShell.tsx`, affected existing
Shell/account/mobile/focus selectors in `apps/console-ui/src/styles.css`,
`tests/ui/customer-console-task-experience-browser.test.ts`, and only the
removed top-bar logout assertion in `tests/ui/logout-safety-browser.test.ts`.

### Step 1: Make overlays mutually exclusive

Add one small local Shell action that closes the mobile drawer before opening
the existing account surface. Closing account must not navigate or mutate
business state. Do not add a global overlay manager or move state into pages.

### Step 2: Keep one account surface and move logout into it

Remove the separate top-bar logout command. Keep existing `signOut()` behind the
single account surface. Preserve both sidebar and top-bar account entry points
as triggers for that one surface. Remove account/Console user/Sub2API user/
Session/expiry rows from the customer surface, without removing those fields
from DTOs/controller state used by other owners.

### Step 3: Preserve accessibility

Retain existing overlay owner/tokens, visible focus, close behavior, and
semantic labels. Ensure email does not break when width permits. Use existing
`Button` and focus styles.

Run:

```bash
node --test tests/ui/customer-console-task-experience-browser.test.ts
node --test tests/ui/logout-safety-browser.test.ts
```

## 6. Task 4 — Repair Checkbox And Workspace Failure Copy

**Files:** `apps/console-ui/src/components/ui/Checkbox.tsx` only if the current
primitive cannot be styled, `apps/console-ui/src/components/ui/components.css`,
affected Checkbox/confirmation selectors in `apps/console-ui/src/styles.css`,
`apps/console-ui/src/app/workspace-experience-model.ts`, and corresponding
model/browser tests.

Use the current Apps SDK Checkbox and wrapper. Make unchecked border visible on
the neutral surface, checked state use approved action blue, focus-visible
obvious, and label/touch target operable at 390px. Preserve `checked`,
`onChange`, `disabled`, `invalid`, `name`, and `value`; do not replace the
primitive or introduce another form-state owner.

At the presentation owner, replace failed Workspace copy that says `联系支持`
with an existing customer-safe retry/return/technical-detail action. Keep raw
error facts only in the existing technical disclosure; add no Support route or
request.

Run:

```bash
node --test tests/ui/workspace-experience-model.test.ts
node --test tests/ui/workspace-task-experience-browser.test.ts
npm run typecheck
```

All launch, readback, idempotency, retry, sensitive-value, and disclosure
assertions must remain valid.

## 7. Task 5 — Apply Overview Hierarchy Without Changing Reads

**Files:** `apps/console-ui/src/pages/CustomerPages.tsx`, only existing customer
Overview/panel/table/metric/responsive selectors in `apps/console-ui/src/styles.css`,
customer browser tests, and focused read-scope tests if needed.

Do not change `useConsoleController`, workspace-read, Gateway-read, billing, or
announcement controllers. Overview continues using the existing independent
owner reads and limits. Express this scan order without inventing data:

1. available Gateway balance, current-month API cost, current-month requests,
   Workspace count;
2. currently loaded Workspace region and existing route action;
3. recent fees and messages and their existing route actions.

Preserve loading/empty/unavailable distinctions and dynamic primary action.
Do not add trends, health, ETA, forecasts, unread counts, browser aggregates,
or a new authority. Apply existing 4px/8px spacing, neutral surfaces, action
blue, minimum text sizes, contrast, and no-overflow rules. At 1280px the primary
Workspace command must be in the first scan path without covering title/actions;
at 390px regions stack and every action remains reachable.

Run:

```bash
node --test tests/ui/customer-console-task-experience-browser.test.ts
npm run typecheck
npm run build
```

## 8. Task 6 — Dual-Viewport Browser Review

**Files:** customer browser test only for semantic selectors; generated PNGs stay
outside product source (use the existing local design evidence directory).

Run:

```bash
npm run test:browser:customer-experience
```

Capture and inspect desktop Overview/account, mobile Overview/drawer/account,
and desktop/mobile Workspace confirmation in unchecked and checked states.
Review task completion, readability, reachability, hierarchy, brand correctness,
no console errors, and no horizontal overflow. Mockup pixel equality is not a
new contract.

## 9. Task 7 — Verification Record And Local Commit

**Files:** create
`docs/history/2026-09-02-console-customer-visual-system-verification.md`;
modify `docs/history/README.md`.

Run focused checks and the repository gate:

```bash
node --test tests/ui/console-model.test.ts tests/ui/workspace-experience-model.test.ts
npm run test:browser:customer-experience
npm run test:browser:workspace-lifecycle
npm run typecheck
npm run lint
npm run build
npm run verify:local
```

Use `verify:local:full` only if the implementation unexpectedly touches
persistence, schema, retained service behavior, or Local-Docker boundaries.
The verification record must state implementation SHA, exact tests and viewport
dimensions, screenshot paths/hashes if retained, unchanged business/API/
authority boundaries, and unresolved issues. It must say local verified and
must not claim deployment, Instance, production, or business redesign.

Inspect and commit only the exact implementation write set:

```bash
git diff --check
git status --short
git diff --stat
git add apps/console-ui/src/app/console-router.ts \
  apps/console-ui/src/app/workspace-experience-model.ts \
  apps/console-ui/src/components/ui/Checkbox.tsx \
  apps/console-ui/src/components/ui/components.css \
  apps/console-ui/src/console-model.ts \
  apps/console-ui/src/layout/ConsoleShell.tsx \
  apps/console-ui/src/pages/CustomerPages.tsx \
  apps/console-ui/src/styles.css \
  tests/ui/console-model.test.ts \
  tests/ui/customer-console-task-experience-browser.test.ts \
  tests/ui/logout-safety-browser.test.ts \
  tests/ui/workspace-experience-model.test.ts \
  tests/ui/workspace-task-experience-browser.test.ts \
  docs/history/2026-09-02-console-customer-visual-system-verification.md \
  docs/history/README.md
git diff --cached --check
git commit -m "feat(console): implement customer visual reference slice"
```

Do not push, create a PR, merge to `main`, deploy, or produce an Instance
receipt in this phase.

## UX-03C Completion Definition

UX-03C is complete only when local evidence proves:

1. OPL Cloud asset/platform identity and current routes/actions are intact;
2. desktop/mobile task navigation and OPL Gateway identity are deterministic;
3. account surface has no internal identifiers and cannot overlap mobile drawer;
4. refresh, messages, account access, and safe logout invoke existing owners;
5. Checkbox is visible, focusable, semantic, and touch-operable;
6. Overview follows scan order while preserving independent reads and all source
   states/action outcomes;
7. no retired Support wording/request remains in customer path;
8. browser checks, typecheck, lint, build, and `verify:local` pass;
9. verification record makes no deployment/Instance/production claim.

## Deferred Next Slice

Only after UX-03C passes both viewports should the next explicitly named slice
(UX-03D) begin the page-level Gateway Key work: mobile filter priority and
operation grouping while preserving every current Key action, reveal lifecycle,
and Workspace Key restrictions. Workspace list/detail, Gateway service/usage,
fees, messages, login, and public entry remain separate slices.
