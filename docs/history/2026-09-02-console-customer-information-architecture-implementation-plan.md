# Console Customer Information Architecture And Support Retirement Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Make the customer Console use one Chinese, task-oriented information
architecture and retire every live Support Mapping access path without deleting
historical data.

**Architecture:** Keep the current typed route owner, feature controllers, and
business APIs. Add one pure customer presentation model, simplify only customer
navigation and content hierarchy, then retire Support vertically from Console
through Control Plane Store/Ent/retention while preserving legacy tables,
rows, migrations, and audit evidence.

**Tech Stack:** TypeScript, React, Node test runner, Playwright, Vite, Go,
`net/http`, Ent, SQLite/PostgreSQL schema verification, existing Console UI
components and CSS tokens.

---

### Task 1: Lock The Customer Vocabulary And Navigation Contract

**Files:**

- Create: `apps/console-ui/src/app/customer-experience-model.ts`
- Create: `tests/ui/customer-experience-model.test.ts`
- Modify: `apps/console-ui/src/app/workspace-experience-model.ts`
- Modify: `tests/ui/workspace-experience-model.test.ts`
- Modify: `apps/console-ui/src/app/console-router.ts`
- Modify: `apps/console-ui/src/console-model.ts`
- Modify: `tests/ui/console-model.test.ts`
- Modify: `package.json`

**Step 1: Write failing pure-model and route-owner tests**

Add table-driven assertions for:

- the four customer menu entries `概览 / 工作空间 / API / 费用` in exact
  order;
- API page entries `服务信息 / 用量 / 密钥`;
- customer route titles `工作空间`, `API`, `费用`, and `消息` while keeping
  route kinds, paths, navigation IDs, sensitivity, and aliases unchanged;
- exact current receipt types and current customer-visible status values;
- unknown or missing receipt/status values producing `待确认` or `暂不可用`
  instead of echoing the raw value;
- exact Workspace lifecycle labels in the existing UX-02A Workspace Presenter
  without a substring or regular-expression branch.

The customer model owns only account, source, API-key, balance-history, and
billing presentation. It must not duplicate Workspace launch, Runtime, renewal,
or budget mappings from `workspace-experience-model.ts`. Model results should
carry a customer label and an explicit
`known | unknown | unavailable` kind where the caller needs to distinguish
states. Raw values remain available only for a technical-details consumer.

**Step 2: Run the focused tests and confirm red**

```bash
node --test tests/ui/customer-experience-model.test.ts \
  tests/ui/workspace-experience-model.test.ts \
  tests/ui/console-model.test.ts
```

Expected: the new model import and four-item navigation assertions fail.

**Step 3: Implement only exact mappings and labels**

Use explicit `switch` branches in `customer-experience-model.ts`. Do not add a
translation framework, dictionary lookup fallback, substring match, or regex
post-processing. Keep money/date/count helpers in their current owner. Add only
the missing customer lifecycle mapping to the UX-02A Workspace model and reuse
its existing renewal mapping from billing views.

Remove `customer.announcements` from `customerMenu`, not from the typed route
registry. Preserve `/console/announcements` so the top bar can still navigate
to messages.

Register the new pure test in `test:source`.

**Step 4: Run focused checks**

```bash
node --test tests/ui/customer-experience-model.test.ts \
  tests/ui/workspace-experience-model.test.ts \
  tests/ui/console-model.test.ts
npm run typecheck
```

Expected: both commands pass.

**Step 5: Commit**

```bash
git add apps/console-ui/src/app/customer-experience-model.ts \
  apps/console-ui/src/app/workspace-experience-model.ts \
  apps/console-ui/src/app/console-router.ts apps/console-ui/src/console-model.ts \
  tests/ui/customer-experience-model.test.ts \
  tests/ui/workspace-experience-model.test.ts tests/ui/console-model.test.ts package.json
git commit -m "feat(console): define customer experience vocabulary"
```

### Task 2: Simplify The Customer Shell And Remove The Support Client

**Files:**

- Modify: `apps/console-ui/src/layout/ConsoleShell.tsx`
- Modify: `apps/console-ui/src/app/console-controller-types.ts`
- Modify: `apps/console-ui/src/app/use-console-controller.ts`
- Modify: `apps/console-ui/src/api/console-read-api.ts`
- Modify: `apps/console-ui/src/api/dtos.ts`
- Modify: `apps/console-ui/src/pages/AdminPages.tsx`
- Modify: `apps/console-ui/src/styles.css`
- Delete: `apps/console-ui/src/app/use-support-controller.ts`
- Delete: `apps/console-ui/src/app/support-controller-model.ts`
- Delete: `tests/ui/support-controller-model.test.ts`
- Modify: `tests/ui/console-browser-acceptance.test.ts`
- Create: `tests/ui/customer-console-task-experience-browser.test.ts`
- Modify: `tools/verify-local.ts`

**Step 1: Write the failing shell acceptance**

At desktop and `390x844`, assert that a signed-in customer sees exactly four
top-level navigation entries in the approved order. Assert that:

- `消息` navigates to `/console/announcements` from the top bar;
- `账号信息` opens from the account command;
- both desktop and mobile active destinations carry `aria-current="page"`;
- no Support button, slide, controller request, or `/api/support/tickets`
  network request exists;
- the account default view shows customer identity and status, while account,
  Console-user, linked-service, and Session identifiers appear only after
  opening `技术详情`.

**Step 2: Run the shell test and confirm red**

```bash
node --test tests/ui/customer-console-task-experience-browser.test.ts
```

Expected: five-item navigation and Support assertions fail.

**Step 3: Remove the Support client vertically inside Console**

- Delete Support DTOs and GET/POST API functions.
- Delete the Support controller interface, hook, model, composition-root
  construction/reset/load/return path, and model test.
- Narrow `GlobalSlide` to `"account" | ""`.
- Delete `SupportSlide`, its imports, action, markup, and Support-only CSS.
- Remove the stale `Support 工单映射` row from the Admin page; do not redesign
  any other Admin content.
- Remove the Support fixture expectation from browser acceptance only after the
  fake server is retired in Task 6.

**Step 4: Implement the approved shell hierarchy**

- Render `customerMenu` as the only customer desktop/mobile top-level list.
- Remove the shell-level API sub-navigation; the page-level control remains.
- Add an icon command with tooltip and accessible name `消息` that navigates to
  the existing announcement route.
- Rename the account command and slide to `账号信息`.
- Put internal account and login-session facts in a closed native
  `技术详情`; keep email, role/customer identity, account status, and logout as
  the default task layer.
- Edit only selectors required by the four-item navigation, account details,
  and deleted Support UI.
- Register `test:browser:customer-experience` in `package.json` and add it to
  `tools/verify-local.ts`; creating an unowned browser test is not completion.

**Step 5: Run focused checks**

```bash
node --test tests/ui/console-model.test.ts \
  tests/ui/customer-console-task-experience-browser.test.ts
npm run typecheck
npm run build
```

Expected: the shell, route, type, and build checks pass without Support client
symbols.

**Step 6: Commit**

```bash
git add -A apps/console-ui/src tests/ui package.json
git commit -m "feat(console): simplify customer navigation"
```

### Task 3: Apply The Customer Information Hierarchy To API And Fees

**Files:**

- Modify: `apps/console-ui/src/app/customer-experience-model.ts`
- Modify: `apps/console-ui/src/pages/CustomerPages.tsx`
- Modify: `apps/console-ui/src/components/keys/KeysPanel.tsx`
- Modify: `apps/console-ui/src/components/source/SourceState.tsx`
- Modify: `apps/console-ui/src/styles.css`
- Modify: `tests/ui/customer-experience-model.test.ts`
- Modify: `tests/ui/workspace-experience-model.test.ts`
- Modify: `tests/ui/customer-console-task-experience-browser.test.ts`
- Modify: `tests/ui/billing-controller-browser.test.ts`
- Modify: `tests/ui/gateway-usage-controller-browser.test.ts`
- Modify: current Workspace browser tests whose visible labels change

**Step 1: Add failing customer-surface assertions**

For overview, Workspace list/launch/detail, API service information, usage,
keys, fees, messages, and account views, assert that the default visible layer
does not contain:

```text
Control Plane, Fabric, Ledger, Sub2API, API Key, API Endpoint,
Account Settings, Receipt ID, raw reasonCode, or customer-visible micros
```

Assert the approved Chinese terms instead. Open technical details separately
and prove that currently useful raw IDs, source reasons, and diagnostic values
remain reachable.

Strengthen fee tests so receipt type/status and renewal status use exact
Presenter results. An unknown value must display `待确认` and must not be echoed
as a customer label.

**Step 2: Run affected tests and confirm red**

```bash
node --test \
  tests/ui/customer-experience-model.test.ts \
  tests/ui/workspace-experience-model.test.ts \
  tests/ui/customer-console-task-experience-browser.test.ts \
  tests/ui/billing-controller-browser.test.ts \
  tests/ui/gateway-usage-controller-browser.test.ts
```

Expected: old terms, inline reason codes, and heuristic receipt labels fail.

**Step 3: Replace local heuristics with the Presenter**

Remove `CustomerPages.tsx` local status/receipt/lifecycle guessers. Use the
exact customer model for current values and explicit unknown states. Convert
customer copy from Workspace/API Key/API Endpoint/announcement/receipt wording
to the approved vocabulary; preserve `API` and `Token`.

Do not change DTO decoding, controller state, request order, pagination,
billing amounts, key payloads, Workspace lifecycle commands, or route paths.

**Step 4: Move implementation evidence out of the default layer**

- `SourceState` must accept an explicit customer-safe `unavailableDescription`
  or equivalent prop. Customer callers use it rather than exposing
  `reasonCode` inline; existing Admin callers keep their current semantics.
- API keys default to customer task facts and commands. Keep internal key ID,
  group/platform, quota implementation values, and raw source reasons in a
  closed technical disclosure where currently required.
- Receipt details default to type, status, date, amount, period, and customer
  Workspace reference. Put receipt ID, price version, charge reference, and
  component evidence in technical details.
- Rename the fee segmented control to `订阅与续费 / 账单记录` and remove the
  `Control Plane 当前商业条款` subtitle.
- Preserve Workspace technical and advanced disclosures introduced by UX-02A;
  change only their customer-visible labels.

**Step 5: Run focused customer regressions**

```bash
node --test \
  tests/ui/customer-experience-model.test.ts \
  tests/ui/workspace-experience-model.test.ts \
  tests/ui/customer-console-task-experience-browser.test.ts \
  tests/ui/billing-controller-browser.test.ts \
  tests/ui/gateway-usage-controller-browser.test.ts \
  tests/ui/workspace-task-experience-browser.test.ts
npm run test:browser:workspace-lifecycle
npm run typecheck
npm run lint
npm run build
```

Expected: customer hierarchy passes and all key, billing, usage, and UX-02A
behavior remains unchanged.

**Step 6: Commit**

```bash
git add apps/console-ui/src tests/ui package.json
git commit -m "feat(console): align customer task language"
```

### Task 4: Retire The Support HTTP And Application Paths

**Files:**

- Modify: `services/control-plane/internal/server/server.go`
- Modify: `services/control-plane/internal/server/admin_ops.go`
- Modify: `services/control-plane/internal/server/app_state.go`
- Modify: `services/control-plane/internal/server/shared_store.go`
- Modify: `services/control-plane/internal/server/ent_state_store.go`
- Modify: `services/control-plane/internal/server/memory_table_store_test.go`
- Modify: `services/control-plane/internal/server/server_test.go`
- Modify: `services/control-plane/internal/server/console_tenant_isolation_test.go`
- Modify: `services/control-plane/internal/server/security_bounds_test.go`
- Delete: `services/control-plane/internal/server/routes_support.go`
- Delete: `services/control-plane/internal/server/support_mapping.go`

**Step 1: Turn Support into a retired-route test**

Add GET `/api/support/tickets?scope=all`, POST `/api/support/tickets`, and PATCH
`/api/support/tickets` cases to
`TestRetiredConsoleRoutesRemainConsoleScoped`. They must return `404` before the
next handler is called. Remove the endpoint from the current-active route list.

Delete tests that prove the retired validation, creation, persistence,
server-owned Support fields, tenant-scoped Support reads, or Support overflow.
Do not replace retired behavior with compatibility assertions.

**Step 2: Run the route test and confirm red**

```bash
cd services/control-plane
go test ./internal/server -run 'Test(RetiredConsoleRoutesRemainConsoleScoped|ActiveConsoleAPIRoutesReachControlPlane)$' -count=1
```

Expected: Support still dispatches to its live route.

**Step 3: Remove every application access path**

- Stop registering Support routes and add the exact retired path.
- Delete the route and application implementation files.
- Remove Support from management-state and internal app-state projections.
- Remove Support methods from shared Store interfaces and both Store
  implementations.
- Remove the Support test-memory table and methods.

Keep generic audit storage and historical `support.map_external_ticket` audit
records unchanged. Do not add a compatibility route or data export.

**Step 4: Run the Control Plane package tests**

```bash
cd services/control-plane
go test ./internal/server
```

Expected: the package compiles with no Support application symbol and retired
route tests pass.

**Step 5: Commit**

```bash
git add -A services/control-plane/internal/server
git commit -m "refactor(control-plane): retire support application paths"
```

### Task 5: Retire Support Ent Access Without Dropping Historical Data

**Files:**

- Modify: `services/control-plane/internal/server/ent_state_store_test.go`
- Modify: `services/control-plane/internal/server/archive_worker_test.go`
- Modify: `services/control-plane/internal/server/ent_state_store.go`
- Modify: `services/control-plane/internal/server/retention_policy.go`
- Modify: `services/control-plane/ent/schema/shared.go`
- Delete: `services/control-plane/ent/schema/support_ticket_mapping.go`
- Delete: `services/control-plane/ent/supportticketmapping.go`
- Delete: `services/control-plane/ent/supportticketmapping/supportticketmapping.go`
- Delete: `services/control-plane/ent/supportticketmapping/where.go`
- Delete: `services/control-plane/ent/supportticketmapping_create.go`
- Delete: `services/control-plane/ent/supportticketmapping_delete.go`
- Delete: `services/control-plane/ent/supportticketmapping_query.go`
- Delete: `services/control-plane/ent/supportticketmapping_update.go`
- Regenerate: `services/control-plane/ent/client.go`
- Regenerate: `services/control-plane/ent/ent.go`
- Regenerate: `services/control-plane/ent/hook/hook.go`
- Regenerate: `services/control-plane/ent/migrate/schema.go`
- Regenerate: `services/control-plane/ent/mutation.go`
- Regenerate: `services/control-plane/ent/predicate/predicate.go`
- Regenerate: `services/control-plane/ent/runtime.go`
- Regenerate: `services/control-plane/ent/tx.go`

**Step 1: Write the legacy-table preservation test**

Following
`TestEntStateStoreSchemaRetiresWorkspaceBackupWithoutDroppingLegacyTable`, add
a typed/scalar SQL test with two cases:

1. a fresh SQLite database does not contain
   `control_plane_support_ticket_mappings` after Ent schema creation;
2. a database pre-seeded with that legacy table and a marker row still
   contains the same old marker after Ent schema creation and one retention
   run.

Use `database/sql` scalar values for the legacy assertion. Do not construct or
assert `map[string]any` in the new test. Do not enable `DropTable`.

Remove only the retired Support seed and deletion assertion from the existing
retention worker test; preserve its terminal-resource, audit archive, and
Production E2E assertions.

**Step 2: Run the preservation test and confirm red**

```bash
cd services/control-plane
go test ./internal/server -run 'Test(EntStateStoreSchemaRetiresSupportTicketMappingWithoutDroppingLegacyTable|RetentionWorkerKeepsTerminalResourcesAndAppliesOwnedRetention)$' -count=1
```

Expected: the fresh schema still creates the Support table or the generated
client still references it.

**Step 3: Remove Support schema and retention ownership**

- Delete the Support Ent schema and dedicated generated files.
- Delete `supportTicketMappingFields` and
  `SupportTicketMapping.Annotations()`.
- Delete Support Ent imports, field mappings, CRUD methods, and retention
  deletion logic from the Store.
- Delete `SupportDays`, `OPL_RETENTION_SUPPORT_DAYS`, the retention DTO field,
  and `supportDeleted` output.

Preserve both historical hard-cut migration SQL files and all existing audit
schemas. Do not add a `DROP TABLE` migration.

**Step 4: Regenerate Ent deterministically**

```bash
cd services/control-plane
go run entgo.io/ent/cmd/ent@v0.13.1 generate ./ent/schema
gofmt -w internal/server/ent_state_store.go \
  internal/server/ent_state_store_test.go \
  internal/server/archive_worker_test.go \
  internal/server/retention_policy.go
```

Inspect the generated diff. It must contain only removal of
`SupportTicketMapping` generated access and the corresponding schema table
descriptor.

**Step 5: Prove application absence and data preservation**

```bash
cd services/control-plane
go test ./internal/server -run 'Test(EntStateStoreSchemaRetiresSupportTicketMappingWithoutDroppingLegacyTable|RetentionWorkerKeepsTerminalResourcesAndAppliesOwnedRetention)$' -count=1
go test ./internal/server
cd ../..
rg -n 'SupportTicketMapping|supportTicketMapping|supportticketmapping|SupportDays|OPL_RETENTION_SUPPORT_DAYS|supportDeleted' \
  services/control-plane --glob '!migrations/**' --glob '!internal/server/ent_migrations/**'
```

Expected: tests pass and the final search has no live application or generated
schema matches. Matches in the two historical migration SQL files remain and
must not be edited.

**Step 6: Commit**

```bash
git add -A services/control-plane/ent services/control-plane/internal/server
git commit -m "refactor(control-plane): preserve retired support data"
```

### Task 6: Remove Support From Browser Fixtures And Prove The Full Journey

**Files:**

- Modify: `tools/console-browser-qa.ts`
- Modify: `tests/ui/console-browser-acceptance.test.ts`
- Modify: `tests/ui/logout-safety-browser.test.ts`
- Modify: `tests/ui/customer-console-task-experience-browser.test.ts`

**Step 1: Remove the fake Support product**

Delete Support fixture state, GET/POST handlers, write attempts, readbacks,
fault injection, interaction steps, and acceptance result fields. The fake
server must not preserve a route the product has retired.

Change the logout late-response safety test to hold a still-live customer
Workspace refresh GET. Preserve the original assertion: a response that
completes after logout must not commit into the cleared Session generation.

**Step 2: Extend the customer acceptance**

At `1280x900` and `390x844`, traverse all four top-level destinations, the
message route, and account information. Assert:

- exact navigation order and active semantics;
- page-level API navigation is present once;
- no horizontal page overflow;
- no Support request or external network request;
- prohibited internal vocabulary is not visible before a technical disclosure
  is opened;
- API Key commands and the UX-02A Workspace journey still complete through
  their current owner APIs.

Use `startConsoleDemoServer({ port: 0, log: false })` or a test-owned listener.
Do not rely on the shared QA harness's default Vite port and do not terminate an
unrelated local server.

For the changed API Key surface, add the missing lifetime boundary acceptance:
create, reveal, copy, remain visible at 59,999 ms, hide at 60,000 ms, reveal
again, disable, enable, and delete. Use a fresh Playwright context and clock,
and preserve request-count and idempotency-key assertions.

**Step 3: Run browser acceptance**

```bash
node --test \
  tests/ui/console-browser-acceptance.test.ts \
  tests/ui/logout-safety-browser.test.ts \
  tests/ui/customer-console-task-experience-browser.test.ts
npm run test:browser:workspace-lifecycle
```

Expected: all browser checks pass with no Support fixture or call.

**Step 4: Commit**

```bash
git add tools/console-browser-qa.ts tests/ui package.json
git commit -m "test(console): verify customer information architecture"
```

### Task 7: Reconcile Current Truth And Record Verification

**Files:**

- Modify: `docs/project.md`
- Modify: `docs/status.md`
- Modify: `docs/product/console-workspace-v1.md`
- Modify: `docs/implementation-architecture.md`
- Create: `docs/history/2026-09-02-console-customer-information-architecture-verification.md`
- Modify: `docs/history/README.md`

**Step 1: Update canonical current owners**

- Describe the four-item customer task hierarchy in the product owner.
- Remove Support Mapping from current capability and controller-owner claims.
- Record in implementation architecture that HTTP/application/Store/Ent/
  retention ownership is retired while historical tables, rows, migrations,
  and audit evidence are retained without an application access path.
- Update status only with locally proven source/test evidence. Do not claim
  deployment, Instance adoption, production state, or physical data migration.
- Inspect `docs/roadmap.md`; change it only if an existing package explicitly
  claims Support or UX-02B scope. Do not create a roadmap package for completed
  work.

**Step 2: Run focused static reconciliation checks**

```bash
rg -n 'Support Mapping|support/tickets|useSupportController|SupportTicketMapping|OPL_RETENTION_SUPPORT_DAYS' \
  apps services/control-plane docs \
  --glob '!docs/history/**' \
  --glob '!services/control-plane/migrations/**' \
  --glob '!services/control-plane/internal/server/ent_migrations/**'
git diff --check
```

Expected: no current product/application claim remains; only deliberately
preserved audit action strings are acceptable and must be listed in the
verification record.

**Step 3: Run the structural repository gate**

```bash
npm run verify:local:full
```

Expected: source, browser, typecheck, lint, build, Go, boundary, PostgreSQL, and
Local-Docker checks all pass.

**Step 4: Record the persistent local receipt**

The verification record must include:

- exact base, HEAD, and commit list;
- focused TypeScript/browser/Go results and full-gate counts;
- desktop/mobile acceptance dimensions;
- route-retirement and legacy-table preservation evidence;
- exact changed-file set;
- confirmation that historical migrations/tables/rows/audits were not
  deleted;
- explicit exclusions for deployment, Instance qualification, publication,
  and production claims.

Add the implementation plan and verification record to the history index.

**Step 5: Commit documentation and verification locally**

```bash
git add docs
git commit -m "docs(console): record customer experience verification"
git status --short --branch
```

Expected: the worktree is clean and the branch remains local unless the user
separately authorizes push or publication.
