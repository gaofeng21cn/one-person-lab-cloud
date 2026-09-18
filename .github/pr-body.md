## Summary

- Gate the platform refund behind one fresh, identity-bound Fabric readback of the same Delete operation, and dispatch it only when every resource of that Workspace is confirmed gone and the deletion receipt is recorded.
- Persist each deletion stage's own observation — result, resource identity, observation time, readback reference and attempts — in the same owner write that advances the phase, and carry that evidence into the final deletion receipt.
- Deploy an application in one command: select a Registry repository/tag, resolve the immutable digest, describe only what the image actually needs, deploy. No separate version-registration step remains.
- Fix the Runtime delete wait that recorded a provider observation it had never read.
- Integrate current `main` (#566) without dropping its Console layout, financial projection, billing trend or credential-binding work.

## Platform refund precondition

`packages/contracts/go/workspace_delete.go` owns the deletion stages, the typed readback facts, the 720-hour versioned refund policy and the single `PlatformRefundDispatchAllowed` gate. Control Plane evaluates that gate over a fresh, read-only, identity-bound Fabric readback of the same original Delete operation: Runtime and Gateway Secret absent, no residual Deployment/ReplicaSet/Pod/Service/NetworkPolicy/environment Secret, the TKE mount binding released, PVC/PV absent, CBS `NOT_FOUND`, Machine absent and CVM `NOT_FOUND`. Only when all of them hold, and `workspace.deleted.v1` is recorded, does it create and dispatch the platform refund.

Refusal is the default. CVM `SHUTDOWN`, a `suspended` Workspace, `autoRenew=false`, `data_deleted`, a single absent resource, a Runtime that is gone while CBS/CVM are unconfirmed, an unavailable or stale or partial readback, an identity mismatch, an unfinished delete operation and a missing deletion receipt all leave the refund blocked instead of dispatching one. The readback must report the exact CBS, machine and CVM identity this operation destroyed; a readback that omits an identity is refused, and an expected identity is never substituted for an observed one. A refusal never rewrites the deletion result.

The refund is settled from the confirmed charge that paid for the period in use — an in-force renewal over the original purchase — with the period start taken from that order, bounded by what that order charged minus what it already refunded, in USD micros under the versioned policy and a deterministic idempotency key. An unresolved refund keeps the money facts it was created with and queries the original refund operation rather than dispatching again. Fabric performs the provider mutation and the authoritative readback, Control Plane decides and computes, Sub2API executes the wallet movement, and Ledger records the deletion and the refund receipt. Control Plane never calls a Tencent Cloud API, merchant refunds are excluded from customer refund decisions, and no operation centre, second wallet or global workflow was added.

## Deletion evidence

Every stage records the observation it made in the owner write that advances the phase: the stage, the result, the resource identity, the time the observing owner observed it, the readback record, and separate read and mutation attempts. A waiting or failed stage is recorded too, so a stalled deletion is explainable from state. A recorded confirmation is append-only; a local state transition is never presented as a provider readback, and a query never stands in for a destroy. The deletion receipt carries the persisted evidence summary, is written only from complete confirmations, and gives each entry an opaque reference derived from the resource identity, the readback and the time. Polling adds no Ledger receipt, and the refund reuses the existing `business_refund` receipt.

The Runtime wait previously recorded an observation it had not read yet: `markStageWaiting` filled the missing observation time from the local clock and left the readback reference empty, so the persisted entry claimed a provider readback while naming none and failed its own validator. The wait now records the readback the owning `ObserveWorkspaceDeleteRuntimeResiduals` call actually returned, and an attempt that returned no usable readback is recorded with an explicit `unavailable` kind — no invented provider time, never a confirmation, never part of a receipt.

## Single-command deployment

The operator flow is select Workspace → select Registry repository/tag → resolve digest → fill the necessary configuration → deploy. The deployment command admits the revision inside that same command, and the Console registration card and its admission call are gone. The application identity is an internal deployment fact: the platform derives a stable identity for the Workspace's application slot and derives the version from the description's content, so the same image always resolves to the same identity, a different legal run description is a new version instead of a conflict, and an image update keeps the data namespace and published entry because a different description cannot overwrite the version.

The platform declares only facts the image actually has. No port, health check, persistent mount, dependency, Secret reference or derived credential is defaulted on the image's behalf; a publishing exposure policy that omits its entry port is refused rather than guessed; and an image that needs none of them receives none. Each declared credential is created, mounted and exposed at the target that credential declares, and deriving a credential no longer requires the Gateway credential to be declared as well. That declaration/mount mismatch, not a Docker directory-sharing limitation, is what previously failed the Local-Docker integration case, and one earlier attribution in `docs/status.md` was corrected accordingly.

The internal revision and deployment-snapshot model is deliberately kept: the deployment command and the default application policy still write through the revision owner, so only the Console dead code was removed.

## Main integration

Current `origin/main` (#566) was integrated, not re-derived. `AdminPages.tsx` keeps main's whole-file layout refactor together with this branch's single-command deployment panel, now presented through main's disclosure pattern. `local_docker_application_configuration.go` keeps both declared-credential fixes, and the secret and runtime integration tests keep both owners' cases. `styles.css` keeps main's file plus this branch's narrow-screen deletion rule. Files changed only on main were taken verbatim. No file was resolved by whole-file ours/theirs, and no current change was dropped.

## Verification

Full local verification was run on the integrated tree, not on either side alone.

- `npm run verify:local:full`: **PASS**, exit `0`, including PostgreSQL and real Docker integration, on the integrated tree whose base is `7661b3a9`.
- Node source tests: **227/227**; Console browser suite: **114/114**.
- PostgreSQL module packages: migration helper **1**, Ledger **3**, Control Plane **8**, Fabric **6**; **zero required skips**.
- The Runtime wait regression fails before the fix and passes after it: a wait records exactly the observation time and readback reference its own read returned, a failed owner read and a readback naming no reference both record `unavailable` with no provider time, and an unavailable entry never satisfies the receipt.
- `git diff --check` is clean and `package-lock.json` matches `main`.

`docs/status.md` records the owner evidence. This is source and local-integration verification only: no Product Release, Instance deployment, production acceptance, cloud-resource mutation or real-money operation is included, and Instance qualification of these bytes remains an external obligation.
