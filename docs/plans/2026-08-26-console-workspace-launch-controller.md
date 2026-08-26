# Console Workspace Launch Controller Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Extract the complete Workspace Launch browser lifecycle from the broad Console controller into one narrow, tested capability owner without changing current API, UX, request, polling, or navigation behavior.

**Architecture:** Add a small pure Launch model for intent and recovery decisions, then add `useWorkspaceLaunchController` as the owner of Launch form, catalog, pricing preview, operation, polling, readback, and busy state. Keep Session, Router, wallet loading, Workspace loading, toast rendering, and route orchestration in `useConsoleController`; compose them through narrow functions and switch only the Launch views to `WorkspaceLaunchController`.

**Tech Stack:** React 18 hooks, TypeScript, Node test runner, existing typed Console DTOs and HTTP adapters, existing fake-only Playwright browser QA.

---

### Task 1: Add The Pure Workspace Launch Lifecycle Model

**Files:**
- Create: `apps/console-ui/src/app/workspace-launch-controller-model.ts`
- Create: `tests/ui/workspace-launch-controller-model.test.ts`
- Modify: `package.json`

**Step 1: Write the failing typed lifecycle tests**

Create typed `WorkspaceLaunchRequest` and `WorkspaceLaunchResponse` fixtures and cover one invariant per test:

```typescript
import assert from "node:assert/strict";
import test from "node:test";

import type { WorkspaceLaunchRequest, WorkspaceLaunchResponse } from "../../apps/console-ui/src/api/dtos.ts";
import {
  classifyWorkspaceLaunchRecovery,
  resolveWorkspaceLaunchIntent,
  shouldPollWorkspaceLaunch,
  shouldRetainWorkspaceLaunchIntent,
  workspaceLaunchSubmission
} from "../../apps/console-ui/src/app/workspace-launch-controller-model.ts";

const basicRequest: WorkspaceLaunchRequest = {
  name: "Research",
  packageId: "basic",
  autoRenew: true
};

function launchOperation(operationId: string, status: string): WorkspaceLaunchResponse {
  return {
    operationId,
    status,
    phase: "preflight",
    accountId: "account-alpha",
    name: "Research",
    packageId: "basic",
    sizeGb: 20,
    autoRenew: true,
    priceVersion: "2026-08",
    currency: "USD",
    totalChargeUsdMicros: 100
  };
}
```

Assertions must prove:

- an exact retry reuses the existing idempotency key;
- a conflicting input returns `conflict` and creates no key;
- an unknown response retains the intent and a known error releases it;
- recovery classifies zero, one, and multiple non-terminal operations as `none`, `resume`, and `conflict`;
- `manual_review` and terminal statuses do not poll, while an active status does;
- customer-owned submission forces `autoRenew: false`.

Add the new test file to the explicit `test:source` command in `package.json`.

**Step 2: Run the focused test and verify RED**

Run:

```bash
node --test tests/ui/workspace-launch-controller-model.test.ts
```

Expected: FAIL because `workspace-launch-controller-model.ts` does not exist.

**Step 3: Implement the minimal pure model**

Implement only typed Launch decisions:

```typescript
import type { WorkspaceLaunchRequest, WorkspaceLaunchResponse } from "../api/dtos.ts";
import { isTerminalWorkspaceLaunch } from "../api/workspaces-api.ts";

export interface WorkspaceLaunchIntent {
  input: WorkspaceLaunchRequest;
  idempotencyKey: string;
}

export type WorkspaceLaunchIntentResolution =
  | { kind: "ready"; intent: WorkspaceLaunchIntent }
  | { kind: "conflict" };

export type WorkspaceLaunchRecovery =
  | { kind: "none" }
  | { kind: "resume"; operation: WorkspaceLaunchResponse }
  | { kind: "conflict" };
```

The functions must remain pure. They may compare typed Launch inputs, accept an injected key factory, filter with the existing `isTerminalWorkspaceLaunch`, and inspect the current typed API error payload. They must not duplicate Control Plane Stage transitions or infer a server phase.

**Step 4: Run the focused test and source suite and verify GREEN**

Run:

```bash
node --test tests/ui/workspace-launch-controller-model.test.ts
npm run test:source
```

Expected: all tests PASS.

**Step 5: Commit**

```bash
git add package.json apps/console-ui/src/app/workspace-launch-controller-model.ts tests/ui/workspace-launch-controller-model.test.ts
git commit -m "test(console): define workspace launch lifecycle decisions"
```

### Task 2: Cut The Hook Owner, Composition Root, And Real Consumers Together

This is one commit because the Hook, its composition, and its real consumer cut
form one compile-time boundary. Do not commit an unused Hook, flat compatibility
fields, or an intermediate tree that fails typecheck.

**Files:**
- Create: `apps/console-ui/src/app/use-workspace-launch-controller.ts`
- Modify: `apps/console-ui/src/app/console-controller-types.ts`
- Modify: `apps/console-ui/src/app/use-console-controller.ts`
- Modify: `apps/console-ui/src/pages/CustomerPages.tsx`
- Test: `tests/ui/workspace-launch-controller-model.test.ts`

**Step 1: Extend the focused model test before the structural cut**

Add focused cases for the exact decisions needed by the Hook:

- the recovery result returns the single non-terminal operation unchanged;
- terminal operations are ignored during recovery;
- `succeeded` and `refunded` remain terminal decisions and are never pollable;
- changing only `autoRenew`, `packageId`, or submission name conflicts with a retained different request.

If Task 1 already proves one of these cases, do not duplicate it. Add only a
missing assertion, run it against the current model, and observe the expected
failure before implementing the missing decision.

**Step 2: Run the focused test and verify RED**

Run:

```bash
node --test tests/ui/workspace-launch-controller-model.test.ts
```

Expected: the new missing-decision assertion FAILS for the intended reason.

**Step 3: Define the narrow public type**

In `console-controller-types.ts`, add `WorkspaceLaunchController` with only the fields consumed by Launch views:

```typescript
export interface WorkspaceLaunchController {
  catalog: RemoteState<PricingCatalogResponse>;
  previews: Partial<Record<PlanId, WorkspacePricePreview>>;
  launchName: string;
  setLaunchName: (value: string) => void;
  launchPlan: PlanId;
  setLaunchPlan: (value: PlanId) => void;
  launchAutoRenew: boolean;
  setLaunchAutoRenew: (value: boolean) => void;
  launchStep: WorkspaceLaunchStep;
  setLaunchStep: (value: WorkspaceLaunchStep) => void;
  launchConfirmed: boolean;
  setLaunchConfirmed: (value: boolean) => void;
  selectedPlan: PricingPlan | null;
  selectedPrice: number | null;
  walletUsdMicros: string | null;
  balanceSufficient: boolean | null;
  customerOwned: boolean;
  launchOperation: WorkspaceLaunchResponse | null;
  launchPollIssue: "" | "error" | "timeout" | "readback";
  busy: boolean;
  reviewWorkspaceLaunch: () => void;
  submitWorkspaceLaunch: () => Promise<void>;
  openLaunchedWorkspace: () => Promise<void>;
}
```

Move `catalog` out of `ConsoleSources` and remove the unused duplicate
`ConsoleTransientState.launchOperation` projection in the same integrated cut.
Keep `WorkspaceLaunchStep` with the public Launch type definitions.

**Step 4: Implement `useWorkspaceLaunchController`**

The Hook accepts only:

```typescript
interface WorkspaceLaunchDependencies {
  session: AuthSession | null;
  wallet: RemoteState<SourceEnvelope<GatewayWallet>>;
  isRequestCurrent: (generation: number, userId?: string) => boolean;
  currentMutationRequest: () => () => boolean;
  currentRequestGeneration: () => number;
  navigate: (path: string) => void;
  flash: (text: string, tone?: "good" | "danger") => void;
  friendlyError: (error: unknown) => string;
}
```

It owns:

- catalog `RemoteState`, previews, form, confirmation, operation, poll issue, busy, and intent ref;
- `loadCatalog(generation, activeSession)`;
- `recover(generation, activeSession)`;
- `reset()`;
- review, submit, bounded poll, authoritative readback, and open commands.

It directly reuses the existing typed APIs. Keep `workspaceLaunchPollAttempts = 30`, `workspaceLaunchPollIntervalMs = 10_000`, the same flash text, and the same call order. Successful completion must still call `findWorkspaceInPages` before navigation. `reset`, `loadCatalog`, and `recover` are composition commands returned in addition to the public `WorkspaceLaunchController` fields; they are not added to the view type.

**Step 5: Compose the capability in the root**

Instantiate `useWorkspaceLaunchController` only after `flash`,
`isRequestCurrent`, and `currentMutationRequest` exist. Expose the current
request generation through a function, not a mutable ref. Return the capability
as `workspaceLaunch` with no flat compatibility aliases.

Replace route orchestration calls while preserving the existing concurrent
request order:

```typescript
if (routePath === "/console/workspaces") {
  await Promise.all([
    loadWorkspaces(generation, activeSession, workspacePageNumber, 10),
    workspaceLaunch.recover(generation, activeSession)
  ]);
  return;
}
if (routePath === "/console/workspaces/new") {
  await Promise.all([
    loadWallet(generation, activeSession),
    workspaceLaunch.loadCatalog(generation, activeSession),
    workspaceLaunch.recover(generation, activeSession)
  ]);
  return;
}
```

Keep `loadRoute` in the Console composition root.

Replace reset aggregation with `workspaceLaunch.reset()`. Do not reset the
Launch form on ordinary route changes. Request generation changes continue to
suppress stale polling and readback writes.

Remove only the Launch-owned imports, constants, helper, state, intent ref,
catalog source, load/recover/poll/readback/review/submit/open functions, derived
fields, and flat return fields identified in the design audit. Keep:

- Session and request generation refs/guards;
- Router and toast owners;
- wallet and Workspace loaders;
- `commandBusy` for unrelated mutations;
- Workspace Delete, Renewal, Secret, Operator, Billing, Announcement, and Support behavior.

**Step 6: Switch the real Workspace Launch views**

Change these components to accept `WorkspaceLaunchController` instead of `ConsoleController`:

- `PlanOption`;
- `WorkspaceOrderSummary`;
- `WorkspaceLaunchPage`;
- `WorkspaceLaunchConfirm`;
- `LaunchOperation`.

Pass route-owned commands separately:

```typescript
interface WorkspaceLaunchPageProps {
  controller: WorkspaceLaunchController;
  onBack: () => void;
  onRefresh: () => Promise<void>;
}
```

`WorkspaceListPage` remains a list consumer of `ConsoleController`, but passes `controller.workspaceLaunch` to `LaunchOperation`. `CustomerPages` passes `controller.navigate` and `controller.refreshCurrentPage` as commands. Use `controller.catalog`, `controller.walletUsdMicros`, and `controller.busy`; do not reintroduce `sources.catalog`, `sources.wallet`, `commandBusy`, `navigate`, or `refreshCurrentPage` into the narrow Launch type.

**Step 7: Run focused tests, typecheck, and verify the consumer cut**

Run:

```bash
node --test tests/ui/workspace-launch-controller-model.test.ts
npm run typecheck
rg -n "controller\.(sources\.catalog|previews|launchName|launchPlan|launchAutoRenew|launchStep|launchConfirmed|selectedPlan|selectedPrice|balanceSufficient|customerOwned|launchOperation|launchPollIssue|openLaunchedWorkspace|reviewWorkspaceLaunch|submitWorkspaceLaunch)" apps/console-ui/src/pages/CustomerPages.tsx
```

Expected: tests and typecheck PASS. Every Launch field match is inside a
component typed as `WorkspaceLaunchController`; the broad controller appears
only at route/list composition points.

**Step 8: Run source and browser acceptance**

Run:

```bash
npm run test:source
node --test tests/ui/console-browser-acceptance.test.ts
```

Expected: all tests PASS, including response-loss idempotency, one high-risk Workspace Launch write, desktop/mobile flows, and authoritative Workspace readback.

**Step 9: Commit the complete green boundary**

```bash
git add apps/console-ui/src/app/console-controller-types.ts \
  apps/console-ui/src/app/use-workspace-launch-controller.ts \
  apps/console-ui/src/app/use-console-controller.ts \
  apps/console-ui/src/pages/CustomerPages.tsx \
  tests/ui/workspace-launch-controller-model.test.ts
git commit -m "refactor(console): extract workspace launch controller"
```

### Task 3: Verify The Boundary And Persist Current Evidence

**Files:**
- Modify: `docs/status.md`
- Modify: `docs/roadmap.md`
- Modify: `docs/plans/2026-08-26-console-workspace-launch-controller.md`

**Step 1: Run structural and focused checks**

Run:

```bash
rg -n "launch(Name|Plan|AutoRenew|Step|Confirmed|Operation|PollIssue)|workspaceLaunchIntent|loadCatalog|recoverWorkspaceLaunch|pollWorkspaceLaunch|submitWorkspaceLaunch|openLaunchedWorkspace|workspaceLaunchPoll" apps/console-ui/src/app/use-console-controller.ts
rg -n "catalog" apps/console-ui/src/app/console-controller-types.ts apps/console-ui/src/app/use-console-controller.ts
git diff --check origin/main...HEAD
```

Expected: the broad controller contains only `workspaceLaunch` composition calls, no retired Launch state/intent/workflow, and `ConsoleSources` has no catalog owner.

**Step 2: Run the repository gate**

Run:

```bash
npm run verify:local
```

Expected: PASS. `verify:local:full` is not required because this change does not alter persistence, schema, cross-service contracts, PostgreSQL, capacity, or Local-Docker behavior.

**Step 3: Update canonical evidence**

Update `docs/status.md` with current source/test truth:

- `useWorkspaceLaunchController` owns the complete browser Launch lifecycle;
- the broad root owns Session/Router/toast and composes narrow guards;
- real Launch views consume `WorkspaceLaunchController`;
- API/DTO/request/polling/navigation behavior is unchanged.

Update roadmap package F without closing it:

- record the completed Console Workspace Launch slice and its focused evidence;
- retain `active` because other broad Console capabilities and retired Acceptance B surfaces remain open;
- name the next justified typed capability or Acceptance B readback gate, not a global rewrite.

No Instance receipt is created because this is an internal Console structural refactor with no deployment, qualification, or changed product behavior.

**Step 4: Mark plan execution evidence**

Append a short execution result to this plan naming the focused tests and `npm run verify:local` result. Do not turn the plan into a command transcript.

**Step 5: Commit**

```bash
git add docs/status.md docs/roadmap.md docs/plans/2026-08-26-console-workspace-launch-controller.md
git commit -m "docs: record console launch controller boundary"
```

### Task 4: Review And Deliver

**Step 1: Run specification and code-quality review**

Run a specification review first, then a code-quality review over `origin/main...HEAD`. Resolve every finding and rerun its affected focused test before push. Finally push `codex/console-workspace-launch-controller` and open a PR that references roadmap package `PB-F-TYPED-OPERATIONS-01` and explains why the package remains `active`.

**Step 2: Re-run the final gates after review fixes**

```bash
git diff --check origin/main...HEAD
npm run verify:local
```

Expected: PASS with a clean worktree.
