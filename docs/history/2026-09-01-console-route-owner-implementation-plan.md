# Console Route Owner Implementation Plan

> **For the implementing agent:** execute task by task with tests first. Use the
> repository's typed boundaries and run focused checks before broad checks.

**Goal:** Make one typed route fact own Console surface, title, loading, page
dispatch, sensitivity, and navigation while repairing `/admin/announcements`.

**Architecture:** `apps/console-ui/src/app/console-router.ts` owns a pure typed
parser plus the existing browser navigation hook. The Composition Root consumes
the parsed route and passes it to exhaustive page and shell dispatch; existing
feature controllers and Control Plane APIs remain unchanged.

**Tech stack:** TypeScript, React, Node test runner, Playwright, Vite.

---

### Task 1: Establish The Typed Route Matrix

**Files:**

- Modify: `apps/console-ui/src/app/console-router.ts`
- Modify: `tests/ui/console-model.test.ts`
- Modify: `tests/ui/gateway-request-lifecycle.test.ts`

**Step 1: Write the failing matrix test**

Add table-driven assertions for all current static paths and one dynamic
Workspace path. Assert the exact `kind`, normalized `path`, `surface`, `title`,
Session requirement, sensitivity, navigation identity, and decoded Workspace
ID where applicable. Add explicit invalid-path and malformed-encoding cases.

**Step 2: Run the focused test and confirm red**

Run:

```bash
node --test tests/ui/console-model.test.ts tests/ui/gateway-request-lifecycle.test.ts
```

Expected: failure because `parseConsoleRoute` and route metadata do not exist.

**Step 3: Implement the route owner**

Add the `ConsoleRoute`, route-kind, surface, and navigation-id types; immutable
static route definitions; exact Workspace-detail parsing; and thin known-route
and sensitive-route predicates. Make `useConsoleRouter` return the parsed route
alongside the normalized path.

**Step 4: Run focused tests and typecheck**

```bash
node --test tests/ui/console-model.test.ts tests/ui/gateway-request-lifecycle.test.ts
npm run typecheck
```

Expected: both commands pass.

### Task 2: Move All Route Consumers To The Typed Fact

**Files:**

- Modify: `apps/console-ui/src/app/use-console-controller.ts`
- Modify: `apps/console-ui/src/console-model.ts`
- Modify: `apps/console-ui/src/App.tsx`
- Modify: `apps/console-ui/src/layout/ConsoleShell.tsx`
- Modify: `apps/console-ui/src/pages/CustomerPages.tsx`
- Modify: `apps/console-ui/src/pages/AdminPages.tsx`

**Step 1: Strengthen the failing route assertions**

Add navigation assertions that Workspace detail selects Workspace, each API
subroute selects API, and operator announcements select announcement
management. The test should fail against path-specific navigation logic.

**Step 2: Replace string interpretation with route-kind consumption**

- derive initial and current auth state from `route.requiresSession`;
- derive controller title, surface, sensitivity, active Workspace, feature
  scopes, and loader selection from `route.kind`;
- make loader dispatch exhaustive without changing existing read ordering;
- make `App`, customer pages, and admin pages dispatch by surface/kind;
- make shell active state compare route navigation identity;
- remove route helpers from `console-model.ts` after all real callers move;
- add operator announcement management to desktop navigation without expanding
  the fixed mobile primary bar.

Do not alter DTOs, owner APIs, controller freshness, command behavior, or
business state mapping.

**Step 3: Run focused source checks**

```bash
node --test tests/ui/console-model.test.ts tests/ui/gateway-request-lifecycle.test.ts
npm run typecheck
npm run lint
```

Expected: all pass with no non-exhaustive route branch.

### Task 3: Repair And Prove Operator Announcement Routing

**Files:**

- Modify: `apps/console-ui/src/pages/AdminPages.tsx`
- Modify: `tests/ui/operator-announcement-controller-browser.test.ts`

**Step 1: Write the failing browser acceptance**

Open `/admin/announcements` as an operator at `1280x900` and `390x844`.
Intercept the announcement projection, make any operator-overview request fail
the test, and assert:

- the page title and `公告管理` heading are visible;
- the announcement list and `新建草稿` command are reachable;
- operator announcement navigation is current;
- the page settles without the overview projection.

Run:

```bash
npm run test:browser:operator-announcement
```

Expected before implementation: failure because the route falls through to the
overview page.

**Step 2: Compose the dedicated page from existing capability parts**

Extract only the shared announcement-management presentation needed by the
overview and dedicated page. Reuse `OperatorAnnouncementController`; do not
duplicate query or mutation state.

**Step 3: Run focused behavior and build checks**

```bash
npm run test:browser:operator-announcement
npm test
npm run typecheck
npm run build
```

Expected: all pass.

### Task 4: Verify And Record The Result

**Files:**

- Create: `docs/history/2026-09-01-console-route-owner-verification.md`

**Step 1: Run the repository gate**

```bash
npm run verify:local
```

Expected: the default Node, build, boundary, and Go-module checks pass.

**Step 2: Inspect the exact write set**

```bash
git status --short
git diff --check
git diff --stat origin/main...HEAD
```

Confirm that `.codegraph/`, `node_modules/`, generated `dist/`, and the user's
main-worktree `package-lock.json` change are absent.

**Step 3: Record persistent verification evidence**

Write the exact branch/base SHA, focused commands, broad gate result, and the
decisive browser assertion to the verification record. Do not include raw
credentials, private addresses, or generated screenshots.

**Step 4: Create the local implementation commit**

Stage only the documented UX-01 write set and commit locally. Do not push,
create a pull request, deploy, or publish.
