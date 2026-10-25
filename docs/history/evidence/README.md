# Archived Evidence Receipts

These receipts moved out of the active `docs/evidence/source-checks/` tree under
the evidence-archival rule in
[the documentation lifecycle policy](../../policies/docs-lifecycle-policy.md#evidence-archival).
They are historical provenance, not current evidence. Their bytes, hashes,
original claims and verification timestamps are unchanged, and no path recorded
inside a receipt or release manifest was rewritten.

Archived batches:

| Batch | Baseline | Archived | Kept in `docs/evidence/source-checks/` |
| --- | --- | --- | --- |
| 1 | `f736a597` | 80 | 179 |

## Batch 1 — 2026-10-25

The directory held 260 entries at the baseline: 259 top-level receipts and logs
plus the `fixtures/` directory. Every top-level entry was checked against every
tracked file for its full path, its repository-relative path and its bare
filename; each file was also checked by SHA-256 and for release-manifest,
receipt-identity or directory-enumeration binding.

| Check | Result |
| --- | --- |
| Full path referenced by a tracked file | 115 kept |
| Repository-relative path referenced by a tracked file | 19 kept |
| Bare filename referenced by an active receipt in the same directory | 1 kept |
| Original path recorded through brace shorthand in an active receipt | 2 kept |
| Bare filename referenced only by another receipt that moves in this batch | 2 archived with their referrer |
| No external path or name binding | 120: 78 archived; 42 kept because the receipt records its own active path |
| Archived after all checks | **80** |

Archived: 69 receipts from 2026-09 and 11 from 2026-10.

The two bare-filename references that remain resolvable after the move are
`2026-09-25-publisher-ledger-worker-local.json`, named by
`2026-09-25-publisher-public-api-alignment.json`, and
`2026-09-26-workspace-final-local.json`, named by
`2026-09-26-workspace-main-absorption.json`. Each pair moves together into the
same archive directory, so the unchanged bare filename still resolves there.

### Kept because a reader binds the original path

Five files with readers bound to their original paths remain in the active tree
with identical bytes:

| File | Binding reader and reason |
| --- | --- |
| `2026-09-27-cloud-package-runtime-webui-oci-recheck.log` | `2026-09-27-cloud-package-runtime-webui-oci-recheck.json` records this active path and its SHA-256 `21fc04965d7d1c0074f72eb23b02e3d033c140efb0871bb73a7bb5aca93dd549` |
| `2026-09-27-cloud-package-runtime-webui-oci-recheck-r2.log` | `2026-09-27-cloud-package-runtime-webui-oci-recheck-r2.json` records this active path and its SHA-256 `0842b139db701b57c231b42d6d818f9b8ccd83d0dc31ca805d9ec388de5272b2` |
| `2026-09-30-tke-serial-serve-default-app-reservation.json` | `2026-09-30-tke-serial-i06-i07-blocker-confirmation.json` names it by bare filename from the same directory as supporting first-delivery evidence |
| `2026-09-30-serve-owns-tke-application-execution.json` | `2026-09-30-serve-model-configuration-apply-readback.json` records its original path through `serve-{owns-tke-application-execution,route-revision-identity,access-data-plane}.json` |
| `2026-09-30-serve-route-revision-identity.json` | The same immutable Serve model readback receipt records its original path through that brace shorthand |

The two OCI recheck receipts themselves remain archived. Their recorded log paths
are unchanged and still resolve in the active tree. A receipt that stays in the
active tree because a document or another receipt names it remains current
evidence only for that reader; archival never turns an old pass into current
evidence.

A receipt that records its own `docs/evidence/source-checks/` path stays in the
active tree: moving it would leave that immutable self-reference pointing at a
path that no longer exists. Archival cannot rewrite the recorded path.

`docs/evidence/local-qualification/` and `docs/evidence/source-checks/fixtures/`
were outside this batch.
