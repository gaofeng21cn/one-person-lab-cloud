# Console Route Owner Verification

## Objective

UX-01 makes one typed Console route fact own surface, title, loading plan, page
dispatch, sensitivity, and navigation identity. It also repairs
`/admin/announcements` so that the route renders announcement management rather
than falling through to the operator overview. Existing feature controllers and
Control Plane API ownership remain unchanged.

## Source Identity And Commits

- Branch: `codex/console-route-owner`
- Base and merge base: `57d230ddf14daa3350b87a47144c6d1a1dd73f87`
  (`origin/main` at verification time)
- Verified implementation HEAD: `c75e35e3c8c196cb93ddc233d5529922ec3e30b3`
- `99af879d7d8d4dcb105b59b529260c50817d372b` -
  `docs(console): define typed route owner plan`
- `ac6ad6ea0ca2355df51f5f33db579f00cc56aeeb` -
  `refactor(console): establish typed route owner`
- `cc98e438e3a9e75405a69bfb2d0d875be196ec88` -
  `refactor(console): bind route aliases to canonical metadata`
- `c75e35e3c8c196cb93ddc233d5529922ec3e30b3` -
  `refactor(console): route pages through typed owner`

## Verification

All commands below ran from the isolated worktree at the verified HEAD and
completed with exit code 0.

| Command | Result |
| --- | --- |
| `npm run verify:local` | Passed the product-distribution boundary; 176/176 Node source tests; 67/67 browser tests across billing, Gateway usage, Console owner reads, customer announcements, operator accounts, operator announcements, operator resources, and Workspace lifecycle; TypeScript typecheck and unused-symbol lint; Vite production build; compile and database-free tests for Contracts, Control Plane, Fabric, Ledger, and shared PostgreSQL migration modules; Git whitespace gate. |
| `node --test tests/ui/console-model.test.ts tests/ui/gateway-request-lifecycle.test.ts` | 21/21 passed, including the static route matrix, Workspace detail parsing, alias normalization, unknown-path rejection, navigation identities, and sensitive-route classification. |
| `npm run test:browser:operator-announcement` | 6/6 passed, including the decisive dedicated-route acceptance and announcement request freshness/intent lifecycle cases. |
| `npm run typecheck` | Passed `tsc --noEmit`. |
| `npm run lint` | Passed `tsc --noEmit --noUnusedLocals --noUnusedParameters`. |
| `npm run build` | Passed Vite production build; 2,558 modules transformed. |

The decisive browser acceptance ran at `1280x900` and `390x844`. At both
viewports `/admin/announcements` rendered its own `公告管理` page, the
announcement list, and `新建草稿`; the operator-overview heading was absent and
the intercepted operator-overview endpoint received zero requests. Desktop
navigation marked `公告管理` with `aria-current="page"`. The mobile layout kept
the fixed five-item primary navigation unchanged and satisfied
`document.body.scrollWidth <= document.body.clientWidth`, proving no horizontal
page overflow at `390x844`.

## Review And Write-Set Hygiene

- Task 1 typed-route-matrix work passed both specification and code-quality
  review with no blocking findings.
- Task 2 route-consumer and page-dispatch work passed both specification and
  code-quality review with no blocking findings.
- The implementation write set is limited to seven Console source files,
  three focused UI tests, and the design and implementation-plan records:
  `apps/console-ui/src/App.tsx`,
  `apps/console-ui/src/app/console-router.ts`,
  `apps/console-ui/src/app/use-console-controller.ts`,
  `apps/console-ui/src/console-model.ts`,
  `apps/console-ui/src/layout/ConsoleShell.tsx`,
  `apps/console-ui/src/pages/AdminPages.tsx`,
  `apps/console-ui/src/pages/CustomerPages.tsx`,
  `tests/ui/console-model.test.ts`,
  `tests/ui/gateway-request-lifecycle.test.ts`,
  `tests/ui/operator-announcement-controller-browser.test.ts`,
  `docs/history/2026-09-01-console-route-owner-design.md`, and
  `docs/history/2026-09-01-console-route-owner-implementation-plan.md`.
- This verification record is the only additional file created during final
  verification. `.codegraph/`, `node_modules/`, and generated `dist/` output are
  not in the change set.
- The main worktree's pre-existing `package-lock.json` modification remains
  present and outside this isolated branch. Its diff SHA-256 remained
  `91ce520d57b1f68ed1a0ee8e4f4dcf853b58c84e83d7e39c2406c90cc1f0ae1a`;
  it was neither changed nor committed here.

## Limitations

This package establishes route truth and repairs the announcement route. It
does not yet redesign Console information architecture, normalize mixed Chinese
and English terminology, remove low-value content, or refactor the large CSS
surface; those are later UX packages. Tests and builds prove only the local
source and build layers. No Candidate was created, no deployment or production
readback was performed, and this record makes no Instance or production claim.
