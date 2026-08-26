# Console Workspace Secret Controller Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task.

**Goal:** Give the Workspace detail Secret workflow one browser-side owner and remove its retired broad `useConsoleController` path without changing behavior.

**Architecture:** A new `useWorkspaceSecretController` owns ephemeral Workspace password/Key state, request invalidation, the 60-second timer, rotation intent, and commands. The root Console controller retains Session, Router, Workspace sources, mutation-current guards, detail refresh, toast, and composition; the Workspace detail view consumes a narrow `WorkspaceSecretController` contract.

**Tech Stack:** React hooks, TypeScript typed contracts, Node test runner, Playwright browser acceptance, Vite.

---

### Task 1: Lock The Lifecycle Decisions

**Files:**
- Create: `apps/console-ui/src/app/workspace-secret-controller-model.ts`
- Create: `tests/ui/workspace-secret-controller-model.test.ts`
- Modify: `package.json`

**Steps:**

1. Add failing typed tests for one-visible-secret projection, invalidation,
   60-second expiry decisions, and same-Workspace rotation-intent reuse.
2. Run `node --test tests/ui/workspace-secret-controller-model.test.ts` and
   confirm it fails because the model is absent.
3. Implement only the pure decisions required by those tests. Reuse
   `RuntimeCredentialResponse`, `WorkspaceCredentialAccess`, and
   `GatewayKeySecretDTO`; do not construct or assert `map[string]any`.
4. Add the focused test to `test:source` and rerun it until green.
5. Run `npm run typecheck`.

### Task 2: Add The Capability Owner

**Files:**
- Create: `apps/console-ui/src/app/use-workspace-secret-controller.ts`
- Modify: `apps/console-ui/src/app/console-controller-types.ts`
- Modify: `apps/console-ui/src/app/use-console-controller.ts`

**Steps:**

1. Define the narrow `WorkspaceSecretController` view contract.
2. Implement the hook with private state, timer, request generation, and
   rotation intent. Clear and invalidate on Session, route, Workspace scope,
   explicit clear, and unmount.
3. Preserve the existing `revealWorkspaceCredentials`, `revealGatewayKey`, and
   `rotateWorkspaceCredentials` request order and the 60-second timeout.
4. Compose the hook from the root using the current Session, selected Workspace,
   mutation-current guard, detail-refresh callback, and `flash`.
5. Keep reset/recovery commands private to composition; do not expose root refs,
   source setters, or full Console state.
6. Run the focused model test and `npm run typecheck`.

### Task 3: Switch The Real Consumer And Delete The Broad Path

**Files:**
- Modify: `apps/console-ui/src/pages/CustomerPages.tsx`
- Modify: `apps/console-ui/src/app/use-console-controller.ts`
- Modify: `apps/console-ui/src/app/console-controller-types.ts`

**Steps:**

1. Change the Workspace detail access section to consume
   `WorkspaceSecretController` rather than flattened root fields.
2. Retain the existing generic clipboard command only as a UI side effect; do
   not persist copied values.
3. Remove root Secret state, timer, request generation, rotation intent,
   lifecycle helpers, command implementations, and retired imports.
4. Confirm no compatibility fields remain with focused `rg` and CodeGraph
   caller exploration.
5. Run focused tests, typecheck, lint, and build.

### Task 4: Preserve Browser Safety

**Files:**
- Modify only if a current gap is proven: `tests/ui/console-browser-acceptance.test.ts`
- Modify only if a current gap is proven: `tests/ui/logout-safety-browser.test.ts`

**Steps:**

1. Run current browser acceptance before adding coverage.
2. Add only the missing typed/browser assertion needed to prove timeout,
   navigation/logout cleanup, or late-response rejection.
3. Run the focused browser tests and confirm behavior is unchanged.

### Task 5: Reconcile Canonical Evidence

**Files:**
- Modify: `docs/status.md`
- Modify: `docs/roadmap.md`
- Move: `docs/plans/2026-08-26-console-workspace-secret-controller-design.md` to `docs/history/`
- Move: `docs/plans/2026-08-26-console-workspace-secret-controller.md` to `docs/history/`
- Modify: `docs/history/README.md`

**Steps:**

1. Record the implemented owner and preserved behavior in `docs/status.md`.
2. Keep roadmap package F `active`, record this completed slice, and name the
   next selection criterion rather than pre-authorizing another refactor.
3. Archive the completed design and plan and add one history index entry.

### Task 6: Verify And Deliver

**Steps:**

1. Run focused model and browser tests.
2. Run `npm run test:source`, `npm run typecheck`, `npm run lint`, and
   `npm run build`.
3. Run `npm run verify:local`; do not run `verify:local:full` because this slice
   does not change persistence, schema, PostgreSQL, cross-service contracts, or
   Local-Docker behavior.
4. Run a specification review, then a code-quality review over
   `origin/main...HEAD`; resolve every finding and rerun affected checks.
5. Commit, push `codex/console-workspace-secret-controller`, and open a PR that
   references `PB-F-TYPED-OPERATIONS-01` and explains why F remains `active`.
