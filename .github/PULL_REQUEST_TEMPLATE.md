<!--
One governance record per pull request. Every section below is required
exactly once, and CI refuses a body with a missing, duplicated or empty
section. `npm run check:pr-body -- --body-file <path>` runs the same check
locally. A pull-request body declares claims; it is never a receipt.
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
One line per exact repository-relative path this PR may change; a trailing `/` declares that directory. Every path the PR actually changes must be declared here, and the diff is machine-compared against this list.
-->

- `<repository-relative path>`

## Receipt Pipeline

<!--
One line per receipt. Do not paste receipt contents and do not use this body as evidence: `source-check` points at an in-repo `docs/evidence/source-checks` receipt; `development-stage` names the host-store receipt identity (run/gate/attempt and its SHA-256); `business` and `instance` name their existing owner references.
-->

- receipt: source-check; result: `<passed|failed|pending>`; path: `docs/evidence/source-checks/<receipt-name>.json`
- receipt: development-stage; result: `<passed|failed|pending>`; path: `runs/<run-id>/receipts/<gate-id>-<attempt>.json`; run: `<run-id>`; gate: `<gate-id>`; attempt: `<attempt>`; sha256: `<64-hex receipt digest>`
- receipt: business; result: `<passed|failed|pending>`; authority: `<business owner>`; ref: `<existing owner receipt reference>`
- receipt: instance; result: `<passed|failed|pending>`; ref: `<existing instance receipt reference>`

## Acceptance Criteria

<!-- One checkbox per acceptance item. `merge-ready` and `source-complete` require every box checked. -->

- [ ] `<observable acceptance item>`

## Verification

<!-- The exact commands and executed results (test counts, exit codes), including failures. Worker claims and planned commands are not results. -->

- `<command>`: `<actual executed result>`

## Limitations

<!-- What this evidence does not prove (runtime, Instance, provider, unrun suites), and what remains open. -->

- `<limitation>`

## Terminal State

- Terminal state: `<merge-ready|source-complete|blocked>`

<!--
`merge-ready` means every declared receipt passed and a passed host `development-stage` acceptance receipt exists. `source-complete` means the source layer is complete with source-layer receipts and no host acceptance receipt is claimed. `blocked` carries open obligations.
-->

## Merge Danger

<!--
State what merging can break: contract or migration order, coordinated deployment, rollback, or none. Also confirm no secret, customer data or raw provider response is included, and that the branch is current with main with review conversations resolved.
-->

- Merge danger: `<none|what merging can break and the required follow-up>`
