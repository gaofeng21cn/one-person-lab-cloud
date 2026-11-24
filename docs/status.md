# OPL Cloud Current Status

Owner: `opl-cloud`
Purpose: `replaceable_current_evidence_snapshot`
State: `current_snapshot`
Reviewed source baseline: `3773966a31e2a5d6630038decfa0a812c1488830` on `main`
Snapshot reconciliation date: 2026-11-10
Scoped Workspace deletion update: 2026-11-12 (installed product source `8db78b34c7be640948f349436af7cd4fcadd584d`)
Scoped lifecycle ownership review: 2026-11-12 (source inspection, not a new runtime qualification)
Scoped Workspace deletion recovery source repair: 2026-11-16 (source behavior over isolated PostgreSQL, not a new runtime qualification)
Scoped retained image-policy reconciliation: 2026-11-16 (retired routes and preserved PostgreSQL records, not installed adoption)
Scoped composed backend runtime gate: 2026-11-18 (automated runtime gate for the composed unit, not a production qualification)
Scoped composed backend production switch: 2026-11-21 (Instance-owned deployment evidence for the composed unit, not a Product Release)
Scoped customer-package delivery round: 2026-11-22 (Linux rehearsal, artifact identity and local no-charge deletion)
Scoped customer purchase acceptance: 2026-11-22 (installation and qualification prerequisites)

Each observation below retains its original source, run and environment; the
reviewed source baseline does not requalify those receipts. Source tests,
Candidate construction, Instance installation and customer acceptance prove
separate layers; none alone establishes a formal Product Release. Target
ownership is in [architecture.md](architecture.md), durable decisions in
[decisions.md](decisions.md), remaining work in [roadmap.md](roadmap.md), and
prior historical observation logs are preserved in Git history.

## Workspace-Key rotation retirement closeout, R3 (2026-11-24)

R3 is closed at the source layer. Rotation creation, rotation recovery and the
rotation's child deployment constructor are already retired: the creation
command left in `880f9939c`, the route registration in `2555840ac`, and the
child deployment constructor in `2b56ab847`. No production code creates or
advances a `workspace.gateway_key.rotate` operation.

What remains under that name is history, and it is retained because it is a
deletion precondition rather than a capability. The rotation decoder and its
evidence validator are consumed by the deletion identity read
(`workspace_delete.go:472-518`, which walks the `NewKeyID → OldKeyID` chain to
resolve a rotated Workspace's current Gateway Key, Secret reference and
fingerprint), by the delete fence (`workspace_delete.go:832`), by the
provider-reconcile suspend guard (`workspace_resource_reconcile.go:56`), by the
delete CAS guard (`ent_state_store_workspace.go:930`) and by the
budget-mutation guard (`workspace_gateway_budget.go:326-332`). Removing them
would make a rotated Workspace's deletion refuse with
`workspace_delete_unconfirmed` and would drop the in-flight rotation fence.
Ledger validates the historical `workspace.gateway_key_rotated.v1` receipt
independently of this decoder.

This records the disposition of the remaining R3 surfaces in the owning SSOT.
It changes no runtime behavior, adds no capability and removes no code. It
does not establish an installed observation, a Candidate build, an Instance
deployment or a production mutation.

## Customer purchase acceptance (2026-11-22)

The latest verified installation uses Cloud `30ac77e3`, before the Gateway
managed-Key repair and the Console Tenant payer balance repair. The recorded
Workspace state found no reusable paid Workspace for the per-version dialogue
check. The customer purchase workflow had no execution receipt. An earlier
application acceptance exercised login and the first reply on its own revision.
The current deployment still needs application preflight and independently
verified customer purchase evidence.

The protected purchase workflow requires Deployment Verification and a J0 READY
receipt for the same Candidate, plus the Instance owner's typed authorization.
An Instance workflow still needs to invoke the existing J0 producer with an
approved sandbox bound to that Candidate. The exact evidence and authorization
remain with the Instance owner. The remaining work is tracked in
[customer lifecycle convergence](roadmap.md#customer-lifecycle-owner-convergence).

## Customer package delivery round on the Linux lane (2026-11-22)

The November 22 rehearsal used the Foundry/OMA export
`ibd-evidence-assistant-v2-0.2.0-upload-package.zip` (SHA-256
`1befa906cdfb4f343beb0f615df36588a03bda53abefdfd3a90f7a171bba2b19`). Both the
November 5 Publisher test and the November 22 round ran a locally built image
from rehearsal source snapshot `74195984d9e51a706c0855aaf0c519c89f6342ae` (tree
`e951614ee155dad390310e23b3b553c32b043f5a`).

- **r13, archive.** The caller's bytes were uploaded unchanged. Build, Workspace
  and Serve deployment readbacks correlate to CapabilityVersion
  `capv_e729f9e26df091416c6a11902868bde7`. The lane archive confirms that the
  running Serve image matches the Build artifact digest
  `sha256:fdaaf352391073035b49d59c07eb1e5a94df90e5d3ebb6177340eb78536474e4`.
  The `LOCAL_NO_CHARGE` quote and order produced a Workspace whose published
  entry returned HTTP 302 then 200, confirming entry reachability. Customer
  deletion succeeded with `workspaceAbsent: true`. The round recorded
  `artifact-identity` and ended `NOT_READY` (`local_v2_customer_evidence_degraded`)
  because the entry did not return this run's probe.
- **r14, directory.** The directory upload included a run-minted probe under
  `carrier/`. The tested Runtime uses a platform entrypoint outside that package
  source root, so the layout check returned `NOT_READY`
  (`local_v2_customer_runtime_outside_source_root`) before Build.

The lane archive also records a separate authenticated interaction probe that
reached `codex_app_server`, returning `401 Unauthorized` without an authorized
selected provider and model configured on the lane. Same-Candidate
qualification, installed Tencent/TKE customer acceptance and paid lifecycle
evidence remain open.
