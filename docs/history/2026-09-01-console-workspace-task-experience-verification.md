# Console Workspace Task Experience Verification

## Objective

UX-02A optimizes one current customer outcome without changing backend,
contract, or deployment ownership:

```text
选择套餐 -> 确认价格 -> 开通工作空间 -> 权威回读
-> 获取凭据 -> 打开工作空间
```

The Console remains a presentation layer over typed Control Plane and Fabric
facts. This package changes customer-facing hierarchy and terminology while
preserving the existing Launch, Runtime, renewal, budget, delete, and Secret
controller boundaries.

## Source Identity And Commits

- Branch: `codex/workspace-task-experience`
- Base and merge base: `7867c2d0c0100478b658547b98e8b9dd2063ab16`
  (`codex/console-route-owner`)
- Verified implementation HEAD: `17013caeb18b539b72532c4cd34b18decf1440bb`
- `97071515617e8db961874cd39fcbcc5ee09a04f1` -
  `docs(console): define workspace task experience plan`
- `e1ed0a4dcef89c6dbb7b8efc8dace0221291c5f8` -
  `feat(console): add workspace experience presenter`
- `59c507a013bbaded38b598792a297bd8cc3d6e09` -
  `fix(console): reject inconsistent workspace quotes`
- `99c352712a308469b4009e993790df45c87db43d` -
  `feat(console): clarify workspace launch experience`
- `afa77c4c9849f31a6672e9a68b9d633bf7469692` -
  `fix(console): retain unconfirmed workspace launch`
- `f18cda1858e8fe9f11be62715adb38fc9f35824f` -
  `feat(console): prioritize workspace availability and entry`
- `fd8505fa931557a25eb10e0ecb856a91804fa13d` -
  `fix(console): complete workspace detail semantics`
- `0edf51fe90dbe020fb6222ea695268b61512f8f5` -
  `test(console): prove workspace runtime entry authority`
- `35f0278116bbf58910fdb1691de197768560f2ea` -
  `test(console): verify workspace customer journey`
- `4b9d9fe00e5c942cb18e6d358bbd49c5df694e4c` -
  `test(console): strengthen workspace journey evidence`
- `f9550f25e4e805c8d3b4fde9884e1d62136f17b6` -
  `test(console): verify workspace result action`
- `c28a92d2e543891d32f4a625bcfa4498dfd59c56` -
  `fix(console): reject workspace success without identity`
- `b1eb5fbf11ed5e98d38501fed7fd96851f362ae1` -
  `test(console): verify malformed launch status evidence`
- `17013caeb18b539b72532c4cd34b18decf1440bb` -
  `fix(console): block ambiguous workspace launch recovery`

## Verification

All commands below ran from the isolated worktree at the verified
implementation HEAD. The final standard gate reran on 2026-09-02 and completed
with exit code 0.

| Command | Result |
| --- | --- |
| `node --test tests/ui/workspace-experience-model.test.ts` | 9/9 passed, covering every current Launch status and stage, explicit unknown handling, Runtime availability outcomes, renewal labels, budget labels, and strict quote presentation. |
| `node --test tests/ui/workspace-launch-controller-model.test.ts` | 7/7 passed, including independent review and submit denial for every non-`clear` recovery state. |
| `node --test tests/ui/workspace-task-experience-browser.test.ts` | 10/10 passed. The complete journey ran at desktop and mobile widths; focused failure, zero-price, unavailable-price, pending-operation, ambiguous-recovery, readback-retry, and succeeded-without-Workspace-identity cases also passed. |
| `npm run test:browser:workspace-lifecycle` | 18/18 passed, including existing freshness, idempotency, renewal, delete, budget, readback, and Secret lifecycle coverage. |
| `npm run verify:local` | Passed the product-distribution boundary; 186/186 Node source tests; 77/77 browser tests; TypeScript typecheck and unused-symbol lint; Vite production build with 2,559 modules transformed; Contracts, Control Plane, Fabric, Ledger, and shared PostgreSQL migration module compile and database-free tests; Git whitespace gate. |

## Customer Journey Evidence

The same fake-only customer journey passed at `1280x900` and `390x844`:

1. The customer selected Basic, saw the authoritative prepaid total, reviewed
   the order, and explicitly confirmed the price.
2. The Console sent exactly one Workspace Launch POST with one non-empty
   idempotency key.
3. The succeeded result remained on the Launch page while its first
   authoritative Workspace readback was blocked. The customer-facing success
   state and `查看工作空间` action were visible and no detail projection appeared.
4. Clicking `查看工作空间` initiated a second exact
   `GET /api/workspaces?page=1&pageSize=50` read. The route still did not enter
   detail before the authoritative responses were released; it entered the
   canonical Workspace detail only after readback succeeded. The Launch POST
   count remained one after the click and through the end of the journey.
5. The primary open action used the independently consumed Fabric Runtime URL,
   not the conflicting URL carried by the Workspace DTO, and opened with
   `_blank` plus `noopener,noreferrer`.
6. Password and API-key copy actions were each proved equal to the currently
   revealed value using boolean assertions that do not include Secret material
   in failure output. Reveals were mutually exclusive.
7. The default customer surface contained no visible Runtime term or raw
   operation, stage, error, check, reason-code, Secret, or micros value.
   Runtime and Launch diagnostics remained reachable only after explicitly
   opening technical details.
8. Budget and delete controls remained reachable only after opening advanced
   settings. Leaving the detail route removed revealed credentials from the
   DOM; returning to the same Workspace showed both credentials masked again.
9. A malformed `succeeded` result without a non-blank Workspace identity was
   presented as `结果待确认`, exposed no open action, initiated no authoritative
   Workspace-detail read, and kept the raw status behind technical details.
10. A deferred recovery read kept configuration, review, and submit entry
    points absent while the result was unknown. Two active Launch operations
    produced an explicit duplicate-purchase block; an unreadable recovery
    response remained fail-closed; only an authoritative empty result restored
    the configuration form. No Launch POST occurred across those transitions.

The browser harness rejected external requests, captured no unexpected Console
error, and verified no horizontal page overflow at either viewport. Its
`finally` cleanup releases pending readback gates and closes the browser without
recording credentials, private addresses, or screenshot content.

## Review And Write-Set Hygiene

- Task-level specification and code-quality reviews completed with no remaining
  Critical, Important, or Minor findings.
- The final review specifically proved the Launch result action, both gated
  authoritative reads, Runtime URL ownership, exact clipboard equality, Secret
  cleanup, recovery checking/conflict/unavailable gates, controller-level
  review and submit denial, and the absence of duplicate Launch writes.
- The stacked implementation write set is limited to seven Console source files,
  nine focused test files, one browser-support tool, `package.json`, and the
  UX-02A design and implementation-plan records. This verification record and
  the history index are the only final documentation additions.
- `.codegraph/`, `node_modules/`, generated `dist/`, and `package-lock.json` are
  absent from the stacked change set.
- The main worktree's pre-existing `package-lock.json` modification remains
  outside this isolated branch. Its diff SHA-256 remained
  `91ce520d57b1f68ed1a0ee8e4f4dcf853b58c84e83d7e39c2406c90cc1f0ae1a`;
  it was neither changed nor committed here.

## Limitations

UX-02A covers only the Workspace purchase-to-entry customer task. It does not
perform a Console-wide terminology migration, redesign customer navigation,
API keys, Billing, Support, or Admin, decompose the global CSS surface, or
change any backend API, DTO, schema, or contract. Tests and builds prove only
the local source and build layers. No Candidate was created, no publication,
deployment, Instance qualification, or production readback was performed, and
this record makes no availability or production claim.
