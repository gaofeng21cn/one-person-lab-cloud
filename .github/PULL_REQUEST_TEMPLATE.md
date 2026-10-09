<!--
One governance record per pull request. Every section below is required
exactly once, and CI refuses a body with a missing, duplicated or empty
section. `npm run check:pr-body -- --body-file <path> --base <full-base-sha>`
checks the record locally. CI executes the checker from the PR base commit in
record mode, treating head as data; a missing base checker fails closed.
A pull-request body declares claims; it is never execution evidence or merge permission.
-->

## Decision Conclusion

<!-- The single decision this change implements at its owning layer. Keep unrelated decisions out. -->

## Current Problem

<!-- The observed current behavior and why it is wrong. Name the reproduced path, not a speculative one. -->

## Business SSOT

<!--
Either state `No business SSOT change:` and name the owners that stay authoritative, or name the canonical business owner this PR actually updates, for example `docs/architecture.md`, `docs/roadmap.md` or a contracts file.
-->

## Development SSOT

<!--
Either state `No development SSOT change:` and name the owners that stay authoritative, or name the development owner this PR updates, for example `AGENTS.md`, `DEV_GUIDE.md` or `docs/spec/target/checks/development_plan.json`.
-->

## Baseline

- Base SHA: `<40-hex base commit>` (required; must equal the pull request base commit)

## Ownership

- DDD owner: `<development-plan source owner, for example console, workspace, cloud or ledger>`
- Phase: `<work package id, execution slice id or preparation window from development_plan.json>`

<!-- The declared owner must own the declared phase record in the current development plan. -->

## Write Set

<!--
One line per exact repository-relative path this PR may change; a trailing `/` declares that directory. Every path the PR actually changes must be declared here, and the diff is machine-compared against this list. Source evidence separately binds the actual write set and corresponding changed-content hashes, not just this allowed scope.
-->

- `<repository-relative path>`

## Receipt Pipeline

<!--
One line per applicable receipt; omit non-applicable business/Instance entries.
Use a source-check the host generated and validated from actual execution results,
not a hand-written passing label. Focused outputs actual results, not an automatic
PR receipt. The host's dev:verify runs the admitted stage and appends a runtime
receipt to worker-protected host storage; the PR source-check export interface is
not yet confirmed. Source-check validation covers the complete base SHA, actual
write set, every changed path's content hash and execution summary with command,
exit code, test counts and captured-output SHA-256. Owner/phase are constrained by
PR context and host approval, not source-check receipt fields; complete input and
dependency-hash binding requires a host stage receipt. Only the
current receipt's own path is exempt from self-hashing; `deleted` must match an
actual Git deletion. Passing behavior evidence requires a zero exit code, a positive
count of registered tests actually executed and zero failures, skips or TODOs.
Keep historical receipts unchanged; they cannot serve as current PASS evidence.
The current checker owns validation of the receipt format. Business/Instance receipts remain
in their existing owner formats and never substitute for source evidence or CI.
-->

- receipt: source-check; result: `<passed|failed|pending>`; path: `docs/evidence/source-checks/<receipt-name>.json`
- receipt: business; result: `<passed|failed|pending>`; authority: `<business owner>`; ref: `<existing owner receipt reference>`
- receipt: instance; result: `<passed|failed|pending>`; ref: `<existing instance receipt reference>`

## Acceptance Criteria

<!-- One checkbox per observable acceptance item. `merge-ready` and `source-complete` require every box checked against actual evidence; unchecked criteria remain blocked, not passed. -->

- [ ] `<observable acceptance item>`

## Verification

<!-- Exact commands and executed results: registered/executed test counts, failures, skips, TODOs and exit codes, including failed attempts. Reference the fresh host-generated PR source-check. Focused output and host runtime receipts are distinct from that PR record. Record actual CI results for the current revision separately; local receipts, worker claims and planned commands do not prove CI ran. -->

- `<command>`: `<actual executed result>`

## Limitations

<!-- What this evidence does not prove (runtime, Instance, provider, unrun suites), and what remains open. -->

- `<limitation>`

## Terminal State

- Terminal state: `<merge-ready|source-complete|blocked>`

<!--
`merge-ready` records closed acceptance criteria and passed declared receipts,
without an additional development-stage gate. `source-complete` proves only the
executed source layer, not business-chain E2E, Instance or production readiness.
Neither label grants merge permission: actual successful current-revision CI,
gaofeng21cn review and existing user merge authorization decide eligibility.
`blocked` carries open obligations. If the base checker still requires retired
acceptance for a merge-ready label, use source-complete with genuine source evidence;
do not fabricate acceptance or execute the head checker to bypass the base check.
-->

## Merge Danger

<!--
State what merging can break: contract or migration order, coordinated deployment, rollback, or none. Also confirm no secret, customer data or raw provider response is included, and that the branch is current with main with review conversations resolved. Before merge, obtain gaofeng21cn review and actual successful current-revision CI under the existing merge rules; do not assume remote branch protection is configured.
-->

- Merge danger: `<none|what merging can break and the required follow-up>`
