# Console Customer Information Architecture And Support Retirement Verification

## Objective

UX-02B aligns the customer Console with four recurring tasks and retires the
unused customer Support ticket capability without changing Workspace, API Key,
usage, billing, announcement, account, Session, or deployment contracts:

```text
概览 -> 工作空间 / API -> 费用
消息从顶栏进入，账号信息从账号菜单进入
```

The Support decision is capability retirement, not historical data deletion.
Legacy Support migrations, tables, rows, and generic audit evidence remain
preserved and are not exposed through a live Support API.

## Source Identity And Commits

- Branch: `codex/workspace-task-experience`
- Base and merge base: `f024fd34ab9944093672a07ee91e44ce5a8d62b8`
  (`origin/main` at verification time)
- Verified implementation HEAD: `6e38d409`
- UX-02B design and implementation plan: `fced28e5`, `b3521e06`
- Customer vocabulary, navigation, language, and technical disclosure:
  `86e58a2f`, `d4597259`, `508feafd`, `2b3f804e`, `48f49f29`, `8f1643bd`,
  `cadf673d`, `044baadf`
- Support application-path retirement and historical-data preservation:
  `822c8745`, `2ee87d62`
- Customer browser acceptance and stale-response evidence:
  `0ea65897`, `6d9841c5`, `4bc1dfe6`, `6e38d409`

## Verification

All commands below ran from the isolated worktree at the verified HEAD and
completed with exit code 0 unless noted.

| Command | Result |
| --- | --- |
| `node --test tests/ui/console-browser-acceptance.test.ts tests/ui/logout-safety-browser.test.ts tests/ui/customer-console-task-experience-browser.test.ts` | 8/8 passed: desktop/mobile customer IA, terminology boundary, account technical disclosure, Support absence, logout/login/reveal stale-response safety, and fake-only network refusal. |
| `npm run test:browser:workspace-lifecycle` | 18/18 passed, preserving UX-02A Workspace launch, readback, renewal, delete, budget, and Secret lifecycle behavior. |
| `npm run typecheck` | Passed. |
| `npm run lint` | Passed with no unused symbols. |
| `git diff --check` | Passed. |
| `go test ./internal/server` | Not run to completion because the local environment lacks the repository-required PostgreSQL test configuration; this is an environment prerequisite, not a source assertion failure. |

The focused browser evidence passed at `1280x900` desktop and `390x844`
mobile widths. The customer navigation is exactly `概览 / 工作空间 / API / 费用`
at both widths, `消息` remains reachable from the top bar, and `账号信息`
remains reachable from the account menu. No horizontal overflow was observed.

The stale-response tests use the live page's controller projection rather than
a newly mounted page: a late Workspace response released during unconfirmed
logout is absent from the existing controller's Workspace list projection, and
a late password reveal invalidated by a competing reveal remains masked in the
same detail component. A late login response is released only after logout and
cannot restore an account or cancel the public state.

Support retirement evidence covers all live layers: customer UI affordances,
Console controller/client/DTO paths, Control Plane HTTP/application/Store/state
paths, Support Ent schema/generated access, and Support retention deletion are
absent. The retired `/api/support/tickets` GET, POST, and PATCH paths return
`404`. Fresh schema initialization does not create
`control_plane_support_ticket_mappings`; a legacy table and marker row survive
schema initialization and retention. Historical migration files and audit
records were not deleted, rewritten, exported, anonymized, or dropped.

## Changed-File Boundary

The UX-02B stack changes only the Console presentation and acceptance surface,
the retired Support access path and its owner-generated access, focused tests,
the fake-only browser fixture, and current documentation. No Workspace, API Key,
usage, billing, announcement, account, Session, provider, deployment, release,
or production business contract was changed. The verification record and history
index are documentation-only additions after implementation.

## Limitations And Exclusions

This is local source and fake-only browser evidence. It does not claim a
Candidate, publication, deployment, Instance qualification, production
availability, physical Support data migration, or production data deletion.
The full `npm run verify:local:full` gate remains the final repository checkpoint
for this retained persistence boundary; it is intentionally run after this
record is added.
