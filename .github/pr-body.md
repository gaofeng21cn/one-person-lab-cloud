## Summary

- Refresh the Console presentation, responsive layouts, tables and operator views; compile the Apps SDK theme through Tailwind's Vite integration.
- Present a Workspace net-charge trend backed by a read-only Control Plane query, not by parsing receipt labels or summing receipt rows in the browser.
- Correct the Local-Docker credential declaration/mount mismatch uncovered by the complete integration gate.

## Financial projection and ownership

Console owns the customer-facing experience. Control Plane exposes the account-scoped query over retained Workspace order identities and Sub2API-confirmed money movements; Sub2API remains the wallet and usage authority, while Ledger remains the receipt/evidence owner.

The trend includes Workspace purchase/renewal debits and their related refunds, excluding API usage, top-ups, gifts, unrelated adjustments and provider costs. Movements use the confirmed wallet effective time and a fixed fourteen-day Asia/Shanghai window. A reversed renewal contributes its original debit and refund independently even when only one refund receipt exists.

The projection distinguishes undispatched work, dispatched-but-unconfirmed money and confirmed movements. Missing audit history never proves that no money moved. Incomplete totals are explicitly labelled as the confirmed portion, including accessible chart text. The actual debit/refund execution and idempotency rules are unchanged.

## Local-Docker root-cause correction

The former `bind source path does not exist` failure was not established as a generic macOS sharing limitation: ordinary bind probes passed, and the real failure reproduced with an explicit shared temporary directory. A Gateway-only declaration implicitly mounted WebUI password/session files that were never created. The adapter now mounts only declared credential consumers at their declared targets, including the immutable Gateway key. The integration fixture declares the three credentials its application actually reads.

No path-prefix heuristic, skipped test, weakened assertion, new service or second wallet was introduced.

## Verification

Full behavioral verification was run on `ad8d23d30cdd01a0df5c998b1a7f6e466245f5ef` (tree `90b5a20db9092533d058ecd1bc0061408dfe7a3e`), including the current `main` base. The subsequent closeout change updates only this description and `docs/status.md`.

- `npm run verify:local:full`: **PASS**, including PostgreSQL and real Docker integration.
- Node source tests: **216/216**; Console browser suite: **113/113**.
- Separate Console browser acceptance: **3/3**.
- PostgreSQL module packages: migration helper **1**, Ledger **3**, Control Plane **8**, Fabric **6**; **zero required skips**.
- Credential regression tests fail before the fix and pass after it; real Docker verifies credential/session behavior, data retention, application isolation, replacement, stop/resume/delete and image retirement.
- Full-gate log SHA-256: `8ac1b8848dba724a91b3cc0aa9d5e2c77237a65f8dd946c540df4ed1877e8ffd`.

Earlier failed attempts are retained in local verification evidence. A whole-host capacity collision was resolved by serializing tests that share the Docker daemon; a later transient browser startup failure was diagnosed and the unmodified full gate was rerun successfully.

`docs/status.md` records owner evidence. This is source/local-integration verification only, not a Product Release, Instance deployment, production acceptance, cloud-resource mutation or real-money operation.
