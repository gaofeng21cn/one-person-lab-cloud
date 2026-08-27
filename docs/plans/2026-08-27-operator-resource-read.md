# Operator Resource Read Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Extract the `/admin/resources` browser read lifecycle into one narrow Operator Resource Read capability while preserving the current list, selection, detail, image policy, image preview, and Runtime Image Replacement refresh behavior.

**Architecture:** The Console capability owns only browser projections, selection scope, read freshness, and reset. Control Plane, Fabric, Ledger, and Sub2API remain the authorities for their facts. List, detail, policy, and preview use independent typed remote states and generations; Runtime Image Replacement receives only a typed refresh port. Shared root composition and page-consumer changes are integrated by one writer after the isolated capability is tested.

**Tech Stack:** React + TypeScript, existing typed API DTOs and `SourceEnvelope`, Node test runner, Playwright/browser fixtures, `npm run verify:local`.

---

### Task 1: Freeze the capability boundary

**Files:**
- Read: `apps/console-ui/src/app/use-console-controller.ts`
- Read: `apps/console-ui/src/pages/AdminPages.tsx`
- Read: `apps/console-ui/src/app/use-workspace-runtime-image-replacement-controller.ts`
- Read: `apps/console-ui/src/api/console-read-api.ts`
- Read: `apps/console-ui/src/api/dtos.ts`

**Step 1: Record the exact current consumers and API calls**

Confirm that `/admin/resources` consumes `getOperatorWorkspaces`, `getOperatorWorkspace`, `getOperatorWorkspaceRuntimeImagePolicy`, and `getOperatorWorkspaceRuntimeImageReplacementPreview`, while Runtime Image Replacement remains a separate mutation capability.

**Step 2: Record the non-goals**

Do not change backend routes, PostgreSQL schemas, Fabric provider behavior, Ledger behavior, Sub2API behavior, or the public DTO shape. Do not extract Session/Auth/Router in this slice.

**Step 3: Verify the worktree**

Run: `git status --short --branch`

Expected: `main` is aligned with `origin/main` and there are no unrelated changes.

### Task 2: Add pure resource-read model rules

**Files:**
- Create: `apps/console-ui/src/app/operator-resource-read-controller-model.ts`
- Test: `tests/ui/operator-resource-read-controller-model.test.ts`

**Step 1: Write failing model tests**

Cover typed rules for:

- fixed operator page size `20`;
- list page/pageSize identity validation;
- selected Workspace identity validation for detail and preview;
- independent list/detail/policy/preview scope keys;
- late completion rejection after route or Session generation changes;
- reset invalidation;
- `empty` as a valid authoritative result and `unavailable` as a failed read;
- partial failure does not replace a successful sibling projection.

Construct fixtures with typed DTOs and `SourceEnvelope`; do not construct or assert `map[string]any`.

**Step 2: Run the focused test to verify it fails**

Run: `node --test tests/ui/operator-resource-read-controller-model.test.ts`

Expected: FAIL because the model module and rules do not yet exist.

**Step 3: Implement the smallest pure functions**

Keep the model free of React, root setters, persistence, provider calls, and generic read-hook abstractions. Functions should validate request scope and return an explicit accept/reject result for each projection.

**Step 4: Run the focused test**

Run: `node --test tests/ui/operator-resource-read-controller-model.test.ts`

Expected: all model tests pass.

### Task 3: Implement the isolated Operator Resource Read controller

**Files:**
- Create: `apps/console-ui/src/app/use-operator-resource-read-controller.ts`
- Read: `apps/console-ui/src/api/console-read-api.ts`
- Read: `apps/console-ui/src/app/operator-resource-read-controller-model.ts`

**Step 1: Define the local capability type**

Expose typed read state for `workspaces`, `detail`, `imagePolicy`, and `imagePreview`, plus `page`, derived `pages`, `selectedWorkspaceId`, `load`, `changePage`, `selectWorkspace`, `refresh`, and `reset`. Keep the local type available for the later serialized shared-contract edit; do not modify `console-controller-types.ts` in this task.

**Step 2: Add independent scope and Session guards**

Accept `active: boolean` and `currentSession: () => AuthSession | null`. Maintain local scope/list/detail/policy/preview generations. A result may commit only when its route scope, Session identity/CSRF, requested page or selected Workspace ID, and local generation still match.

**Step 3: Implement independent reads**

- `load` reads the current page and global image policy with independent settlement.
- `changePage` validates `page >= 1` and the returned page/pageSize.
- `selectWorkspace` updates the selection, invalidates old detail/preview projections, and reads detail and preview independently.
- `refresh` reloads only the active resource-read scope.
- `reset` clears projections, selection, page, intents, and local generations.

Do not use `Promise.all` to turn detail and preview into one failure result.

**Step 4: Run type and focused model checks**

Run: `npm run typecheck`

Expected: PASS. Run the model test again and expect PASS.

### Task 4: Add the narrow Runtime Replacement refresh port

**Files:**
- Modify later, serialized: `apps/console-ui/src/app/use-workspace-runtime-image-replacement-controller.ts`
- Test later with integration: `tests/ui/operator-resource-read-controller-browser.test.ts`

**Step 1: Specify the port**

Use only typed operations equivalent to:

```text
refreshWorkspace(workspaceId)
refreshPreview(workspaceId)
```

The mutation controller must not receive `ConsoleController`, root setters, or root loader functions.

**Step 2: Preserve mutation ownership**

Keep idempotency, operation polling, terminal status handling, and authoritative replacement readback in `useWorkspaceRuntimeImageReplacementController`.

### Task 5: Serialize root and AdminPages integration

**Files:**
- Modify: `apps/console-ui/src/app/console-controller-types.ts`
- Modify: `apps/console-ui/src/app/use-console-controller.ts`
- Modify: `apps/console-ui/src/pages/AdminPages.tsx`
- Modify: `apps/console-ui/src/app/use-workspace-runtime-image-replacement-controller.ts`

**Step 1: Add the shared typed interface**

Copy the already-tested local capability shape into `console-controller-types.ts`. This is a shared contract and has one writer.

**Step 2: Compose the capability from the root**

Instantiate it only for `/admin/resources`. Remove the resource list/detail/policy/preview state, page state, selected Workspace ref, resource loaders, and resource reset/generation from the root. Keep Session/Auth/Router and other capability resets in the root.

**Step 3: Switch every real consumer**

Update `ResourcesPage`, `ResourceDetail`, `WorkspaceRuntimeImageUpgrade`, and mobile resource cards to consume the narrow controller. Update Runtime Image Replacement to use only the refresh port.

**Step 4: Delete the retired path**

Use `rg` to prove no resource page consumer calls the retired root loaders or reads the retired root fields.

### Task 6: Add browser lifecycle acceptance

**Files:**
- Create: `tests/ui/operator-resource-read-controller-browser.test.ts`
- Modify: `package.json`
- Modify: `tools/verify-local.ts`
- Modify: `tests/tools/verify-local.test.ts`

**Step 1: Add focused browser cases**

Cover:

- `/admin/resources` list and image policy load;
- page 1 late response rejected after page 2 completes;
- Workspace A late detail/preview rejected after selecting Workspace B;
- detail failure leaves successful preview visible;
- preview failure leaves successful detail visible;
- route exit and Session reset reject late list/detail/preview responses;
- successful Runtime Image Replacement refreshes only the current Workspace detail/preview.

Use typed fixture DTOs and existing browser interception helpers.

**Step 2: Run the focused browser test**

Run: `npm run test:browser:operator-resource-read`

Expected: all cases pass.

**Step 3: Run ordinary local verification**

Run: `npm run verify:local`

Expected: typecheck, lint, build, source tests, focused browser tests, and whitespace checks pass.

### Task 7: Update evidence and close the slice

**Files:**
- Modify: `docs/status.md`
- Modify: `docs/roadmap.md`
- Optional history: `docs/history/**` for completed design/plan provenance

**Step 1: Record current evidence**

Document the controller owner, real consumers, independent failure model, focused tests, and the fact that no PostgreSQL/TKE validation was required.

**Step 2: Verify structural removal**

Run focused `rg` checks against `use-console-controller.ts` and `AdminPages.tsx`, then run `npm run verify:local`.

**Step 3: Commit the integrated slice**

Use a focused commit message such as:

```bash
git add apps/console-ui/src/app apps/console-ui/src/pages/AdminPages.tsx tests/ui package.json tools/verify-local.ts tests/tools/verify-local.test.ts docs/status.md docs/roadmap.md
git commit -m "refactor(console): extract operator resource read lifecycle"
```

Expected: one integrated commit with no unrelated files and a clean worktree.

