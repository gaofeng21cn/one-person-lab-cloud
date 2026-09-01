# Console Workspace Task Experience Implementation Plan

> **For the implementing agent:** execute task by task with tests first. Preserve
> all existing controller, source, idempotency, readback, and secret-lifetime
> boundaries.

**Goal:** Make the current plan-to-Workspace-entry flow understandable and
efficient without exposing implementation vocabulary as primary customer
content.

**Architecture:** Add one pure typed Workspace experience model between current
DTOs and `CustomerPages.tsx`. Keep the existing feature controllers and Control
Plane APIs unchanged, then restructure only the Launch and Workspace-detail
presentation around the customer's next decision.

**Tech stack:** TypeScript, React, Node test runner, Playwright, Vite, existing
Console UI components and CSS tokens.

---

### Task 1: Establish The Typed Workspace Presentation Model

**Files:**

- Create: `apps/console-ui/src/app/workspace-experience-model.ts`
- Create: `tests/ui/workspace-experience-model.test.ts`
- Modify: `package.json`

**Step 1: Write the failing pure tests**

Use typed DTO fixtures and table-driven assertions for:

- Launch statuses `pending`, `manual_review`, `succeeded`, `failed`, and
  `refunded`;
- an unknown Launch status that produces an explicit unconfirmed state;
- exact current stages `key`, `debit`, `ensure_compute_allocation`, `storage`,
  `attachment`, `secret`, `runtime`, `activation`, `receipt`, and `succeeded`;
- an unknown stage that is labeled as awaiting an owner update without being
  classified as a known stage;
- Runtime `running/ready`, `unready`, `not_found`, `destroyed`, and a ready
  Runtime without a URL;
- current renewal and budget statuses plus explicit unknown values.

Add the new pure test to `test:source` so the ordinary repository gate owns it.

**Step 2: Run the focused test and confirm red**

```bash
node --test tests/ui/workspace-experience-model.test.ts
```

Expected: failure because the presentation model does not exist.

**Step 3: Implement the pure model**

Implement discriminated view types and exact `switch` mappings. The unknown
branches must be explicit, must not infer progress or success, and may expose a
raw value only for the technical-detail consumer.

Do not change DTOs, controller state, API decoding, or money write units.

**Step 4: Run focused model checks**

```bash
node --test tests/ui/workspace-experience-model.test.ts
npm run typecheck
```

Expected: both commands pass.

### Task 2: Rebuild The Launch Result Hierarchy

**Files:**

- Modify: `apps/console-ui/src/pages/CustomerPages.tsx`
- Modify: `apps/console-ui/src/styles.css`
- Modify: `tools/console-browser-qa.ts`
- Modify: `tests/ui/workspace-task-experience-browser.test.ts` if created by
  Task 4 while test-first work is split

**Step 1: Add failing customer-language assertions**

Assert that the default operation view shows the Presenter title, customer
stage, and safe next action. Assert that `operation ID`, raw status/phase,
`errorCode`, block reason, and check names are not visible until `技术详情` is
opened. Assert that success uses `查看工作空间`.

Align the fake-only pending operation with the current canonical `pending`
status and exact `runtime` stage rather than teaching the Presenter a stale
fixture vocabulary.

**Step 2: Render the Presenter output**

Replace the local substring phase lookup and generic status dictionary in
`LaunchOperation`. Render a clear state summary, stage, and actions. Move all
raw operation evidence and manual-review checks into a native closed
`技术详情` disclosure.

Keep polling, recovery, refresh, failed/refunded return behavior, and
authoritative success readback unchanged.

**Step 3: Add only scoped layout styles**

Style the state summary, disclosure, and actions with existing tokens. Verify
stable mobile wrapping and do not refactor unrelated repeated CSS selectors in
this package.

**Step 4: Run focused checks**

```bash
node --test tests/ui/workspace-experience-model.test.ts
npm run typecheck
npm run build
```

Expected: all pass.

### Task 3: Reorder Workspace Detail Around Availability And Entry

**Files:**

- Modify: `apps/console-ui/src/pages/CustomerPages.tsx`
- Modify: `apps/console-ui/src/styles.css`
- Modify: `tests/ui/customer-workspace-read-navigation-browser.test.ts`
- Modify: `tests/ui/fabric-runtime-read-controller-browser.test.ts`
- Modify: `tests/ui/logout-safety-browser.test.ts`
- Modify: `tests/ui/workspace-budget-navigation-browser.test.ts`
- Modify: `tests/ui/workspace-lifecycle-freshness-browser.test.ts`
- Modify: `tests/ui/workspace-renewal-navigation-browser.test.ts`
- Modify: `tests/ui/workspace-mvp-flow.test.ts`

**Step 1: Strengthen the existing failing browser assertions**

Assert that the Workspace header shows availability, plan, monthly price,
entitlement end date, and the primary `打开工作空间` command. Assert that the
default view no longer visibly contains `Runtime ready`, `Workspace URL`,
`Workspace Key`, `Secret`, raw renewal status, `micros`, or the permanent
`CPU / 内存规格` row.

Update budget, renewal, delete, and sensitive-value tests to explicitly open
the relevant disclosure before interacting. Keep their original request-count,
idempotency-key, stale-completion, and cleanup assertions intact.

**Step 2: Implement the new semantic order**

- Put availability and open action in the identity panel.
- Rename credentials to `登录账号`, `登录密码`, and `API 密钥`.
- Keep reveal/copy/rotate behavior and the 60-second lifetime notice.
- Keep plan, price, and entitlement facts visible; map renewal status through
  the Presenter and remove the permanent empty specification row.
- Put budget and delete under closed `高级设置`.
- Put Runtime URL/checks and raw support facts under closed `技术详情`.
- Replace customer-visible delete reason codes with a task-oriented message.

Do not move or copy controller state into page-local workflow state.

**Step 3: Add responsive styles**

Use explicit grid constraints and mobile stacking for the status summary,
primary actions, credentials, and disclosures. Retain at least 44px mobile
touch targets and prevent long IDs or URLs from widening the viewport.

**Step 4: Run the focused Workspace regressions**

```bash
node --test \
  tests/ui/customer-workspace-read-navigation-browser.test.ts \
  tests/ui/fabric-runtime-read-controller-browser.test.ts \
  tests/ui/logout-safety-browser.test.ts \
  tests/ui/workspace-budget-navigation-browser.test.ts \
  tests/ui/workspace-lifecycle-freshness-browser.test.ts \
  tests/ui/workspace-renewal-navigation-browser.test.ts \
  tests/ui/workspace-mvp-flow.test.ts
```

Expected: all existing owner, freshness, command, and secret assertions pass
through the new hierarchy.

### Task 4: Prove The Complete Customer Task At Desktop And Mobile

**Files:**

- Create: `tests/ui/workspace-task-experience-browser.test.ts`
- Modify: `package.json`

**Step 1: Write the failing task acceptance**

Against the fake-only demo server, run the same journey at `1280x900` and
`390x844`:

1. sign in as a customer;
2. open Workspace list and start a new Workspace;
3. select a current plan and confirm the quoted price;
4. submit exactly one Launch command;
5. use `查看工作空间` after authoritative readback;
6. verify availability and the primary open command;
7. reveal and copy the login password and API key;
8. open advanced and technical details and verify that budget and diagnostic
   facts remain reachable.

Assert no external network request, no browser console error, no viewport
overflow, no duplicate launch write, and no secret remaining visible after
leaving the route.

**Step 2: Run the test and confirm red**

```bash
node --test tests/ui/workspace-task-experience-browser.test.ts
```

Expected: failure against the old labels and hierarchy.

**Step 3: Complete only the missing presentation behavior**

Fix the scoped page/model/style behavior required by the task. Do not change
the demo API semantics or controller rules to make the acceptance pass.

**Step 4: Run focused and broad Console checks**

```bash
node --test tests/ui/workspace-task-experience-browser.test.ts
npm run test:browser:workspace-lifecycle
npm test
npm run typecheck
npm run lint
npm run build
```

Expected: all pass.

### Task 5: Verify, Record, And Commit Locally

**Files:**

- Create: `docs/history/2026-09-01-console-workspace-task-experience-verification.md`
- Modify: `docs/history/README.md`

**Step 1: Run the repository gate**

```bash
npm run verify:local
```

Expected: all default source, browser, typecheck, lint, build, Go, and boundary
checks pass.

**Step 2: Inspect the exact stacked write set**

```bash
git status --short
git diff --check
git diff --stat codex/console-route-owner...HEAD
```

Confirm that `.codegraph/`, `node_modules/`, `dist/`, and the user's unrelated
main-worktree `package-lock.json` change are absent.

**Step 3: Record persistent local evidence**

Record the base and final SHA, focused commands, desktop/mobile task result,
secret-cleanup evidence, and broad gate result. Do not include credentials,
private addresses, or generated screenshots.

Index the design, implementation plan, and verification record in
`docs/history/README.md` as historical evidence only.

**Step 4: Perform two-stage and final review**

Review spec compliance first, then code quality, then the complete stacked diff.
Resolve every finding before completion.

**Step 5: Create the final local commit**

Stage only the UX-02A write set and commit locally. Do not push, create a pull
request, publish, deploy, or modify Instance state.
