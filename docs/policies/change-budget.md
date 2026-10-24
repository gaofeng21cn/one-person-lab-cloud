# Change Budget Policy

Temporary brake on repository growth. It exists to stop adding layers while the
current migration finishes, not to change product scope.

**Time box:** 2026-10-24 to 2026-12-05. It ends automatically; it is not renewed
without a new owner decision.

## Why

Observed in the 30 days before this policy (`bbd76f7d..c623e554`):

| Measure | Change |
|---|---:|
| handwritten code | +59,820 lines (63,394 added, 3,574 deleted) |
| tests | +71,608 lines |
| docs | +64,171 lines |
| files | 835 new, 7 deleted |
| binaries | 11 → 13 |
| internal RPCs | 171 → 192 |
| new freeze-list items | 108 |

Two open defects have the same cause: #1041 (a retained route still accepts new
purchase obligations) and #1018 (a retained order cannot be closed). Each fix so
far added a guard or a stage instead of retiring the path it guards. The budget
stops that loop long enough for the v2 chain to be proven and the retained path
to be deleted.

This policy adds one report and one check inside the existing pull request CI. It
adds no workflow, runtime code, contract, service or owner.

## Freeze list

Until the time box ends, a change must not add:

1. a file whose basename matches `reconcile|readback|receipt|fence|admission|settlement`,
   or a new directory matching `coordination|reconcile|readback|receipt|fence|admission|settlement`;
2. an `rpc` in `packages/contracts/proto/internal.proto`;
3. a schema under `services/*/ent/schema/`;
4. a workflow under `.github/workflows/`;
5. a file under `docs/spec/target/`;
6. a binary under `services/*/cmd/*/main.go` or `apps/*/cmd/*/main.go`.

## Exception

A freeze-list item may land when either condition holds:

- the same pull request deletes an equivalent item (one RPC for one RPC, one
  schema for one schema, one workflow for one workflow); or
- the pull request carries the `brake-exception` label, applied by the owner,
  with the reason in the pull request body.

The label is an owner decision, not an agent decision.

## Weekly budget

- **From week 2 (2026-11-02):** net handwritten code must be ≤ 0 per week.
- **New files:** ≤ 20 per week, measured on `main` every Monday.
- **Tests:** may grow only where a behavior test replaces an implementation test.

When a week exceeds the budget, the following week is delete-only: only
net-negative pull requests merge, except for a production defect named in an
issue.

## Deletion day

Friday. No new feature work. The day is spent merging net-negative pull requests,
removing retired paths, and deleting tests that only pin internal state.

## Definition of done

A pull request is not reviewable without a **Deletion** section stating what it
removed, with `git diff --shortstat` and the named file, route, RPC, schema or
test. "Nothing" is an answer only for a defect fix that also states why no path
could be retired.

## Measurement

`node tools/check-change-budget.ts --base origin/main` reports additions,
deletions, net lines by class and every freeze-list violation. Pull request CI runs it
in the existing `source` job, report-only, and does not fail a build during the
first week.

To enforce later, run it with `--enforce` and pass `--exception` when the label
is present.

## Exit criteria

The brake ends when all hold for three consecutive weeks:

- net handwritten code ≤ 0;
- binaries ≤ 11;
- internal RPCs ≤ 175;
- the v2 chain passes end to end and the retained write path is deleted (#1052).

Then re-baseline this policy or delete it with the freeze list it protects.
