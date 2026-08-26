# Console Customer Announcement Read Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task.

**Goal:** Give customer announcement reads one complete browser lifecycle owner without changing current API or visible functionality.

**Architecture:** A new `useCustomerAnnouncementController` owns the customer projection, 3/20 route query scopes, mark-read intent/claim, freshness, receipt validation, projection refresh, and reset. The root retains Session, Router, route orchestration, and toast composition; customer pages consume a narrow typed controller.

**Tech Stack:** React hooks, TypeScript typed DTOs, Node test runner, Playwright, Vite.

---

### Task 1: Lock Pure Decisions

**Files:**
- Create: `apps/console-ui/src/app/customer-announcement-controller-model.ts`
- Create: `tests/ui/customer-announcement-controller-model.test.ts`
- Modify: `package.json`

1. Add failing typed tests for same-announcement intent reuse, different-target
   keys, exact receipt identity/RFC3339 time, and visible readback matching.
2. Run the focused test and confirm the model is absent.
3. Implement only the pure decisions required by the tests.
4. Add the test to `test:source` and rerun it.

### Task 2: Add The Lifecycle Owner

**Files:**
- Create: `apps/console-ui/src/app/use-customer-announcement-controller.ts`
- Modify: `apps/console-ui/src/app/console-controller-types.ts`
- Modify: `apps/console-ui/src/app/use-console-controller.ts`

1. Define the narrow `CustomerAnnouncementController` contract.
2. Implement scope-specific load freshness for Overview 3 and list 20.
3. Implement mark-read intent reuse, claim ownership, response validation, and
   current-scope projection refresh.
4. Compose the capability using only current Session, current route scope,
   toast/error functions, and the existing typed API calls.

### Task 3: Switch Consumers And Remove The Broad Path

**Files:**
- Modify: `apps/console-ui/src/pages/CustomerPages.tsx`
- Modify: `apps/console-ui/src/app/use-console-controller.ts`
- Modify: `apps/console-ui/src/app/console-controller-types.ts`

1. Switch Overview and Announcements pages to the narrow controller.
2. Preserve all current labels, buttons, page limits, routes, and retry actions.
3. Remove root announcement source, busy state, functions, imports, and reset.
4. Use focused searches to prove no compatibility fields remain.

### Task 4: Prove Browser Lifecycles

**Files:**
- Create: `tests/ui/customer-announcement-controller-browser.test.ts`
- Modify: `package.json`
- Modify: `tools/verify-local.ts`
- Modify: `tests/tools/verify-local.test.ts`

1. Prove Overview/list late responses cannot overwrite each other.
2. Prove response loss retains the key and a matching retry converges.
3. Prove response identity/readback mismatch fails closed.
4. Prove route exit/re-entry and Session replacement reject stale completion
   while claims release only after settlement.
5. Add the focused browser suite to the ordinary local gate.

### Task 5: Reconcile Evidence And Verify

**Files:**
- Modify: `docs/status.md`
- Modify: `docs/roadmap.md`
- Move the completed design and plan to `docs/history/`
- Modify: `docs/history/README.md`

1. Record the implemented owner while keeping package F `active`.
2. Run focused model/browser tests, typecheck, lint, build, and
   `npm run verify:local`.
3. Run specification and code-quality reviews; resolve all findings.
4. Commit and prepare a PR. Do not run the PostgreSQL/full gate because no
   persistence or cross-service contract changes.

