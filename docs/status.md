# OPL Cloud Current Status

Owner: `opl-cloud`
Purpose: `replaceable_current_evidence_snapshot`
State: `current_snapshot`

This page records the current verified implementation boundary without copying
commit logs or acting as a release guarantee. Target ownership belongs to
[architecture.md](architecture.md), durable rules to [invariants.md](invariants.md),
and unclosed outcomes to [roadmap.md](roadmap.md). Git history preserves earlier
chronological snapshots.

## Current Capability Baseline

- **Single-Module Go Codebase & Persistence**: All Go services and Console BFF
  live under the single root Go module `go.mod` (`module opl-cloud`), backed by
  typed `database/sql` PostgreSQL stores and `services/internal/postgresmigrate`
  (`ent` ORM has been retired).
- **Composed Backend & Clean Host**: Portable topology `deploy/portable/compose.cloud.yaml`
  runs the single PostgreSQL instance, the 11 Cloud owner processes and the
  same-origin proxy; clean-host `local-v2` verification passes `LOCAL_NO_CHARGE`
  lifecycle qualification (`p0Ready: false`). Composed backend TKE deployment has
  been verified for the installed Candidate, while formal public Release remains
  at `v0.1.8` (GitHub Releases).
- **Console / BFF / Control Plane Boundary**:
  - CloudIdentity routes (`/api/v2/*`) serve Workspace, Quote, Launch, Capability
    upload/build, Serve delivery, Gateway wallet/usage summary, Agent read-only
    MCP (`/api/v2/mcp`), quote review and Gateway session-backed customer
    reads (announcements and legacy personal billing through authenticated
    Control Plane bridges).
  - Control Plane image-replacement creation/preview/worker commands and admin
    activation endpoints are retired; only the operator image-policy GET
    (`GET /api/operator/workspace-runtime-image-policy`), Fabric image
    replacement and `opl-node-image-retire` remain active.
  - Workspace Gateway Key rotation (`workspace.gateway_key.rotate`) creation,
    routes and child deployment constructor are retired; only the read-only
    `NewKeyID -> OldKeyID` resolution decoder and fences remain in
    `services/control-plane/internal/server/workspace_delete.go` and
    `workspace_store.go` so historical rotated Workspaces can still be deleted
    safely.
  - The Control Plane purchase-eligibility policy writer
    (`POST /api/operator/accounts/{accountId}/workspace-purchase-eligibility`)
    is retained: it is the only path that grants or revokes
    `Account.WorkspacePurchaseEnabled` for an existing Account, and the native
    Workspace owner gates every new purchase on the value it produces through
    the Control Plane's `GET /internal/cloud/workspace-purchase-eligibility`
    reader.
- **Retained Control Plane Operation Census**: One read-only Instance
  `workspace-state-observation` run (`45320968122`, Candidate
  `8baf91f1e5a64d1ec46a041f904fd7bc5dcb51c2`) recorded `businessMutations: 0`,
  `providerMutations: 0` and `kubernetesMutations: 0`, and projected 44 runtime
  operations from the current owner API result:

  | `action` | status | count |
  | --- | --- | --- |
  | `workspace.launch.v2` | `succeeded` | 7 |
  | `workspace.delete.v2` | `succeeded` | 5 |
  | `workspace.renewal` | `expired_unpaid` | 2 |
  | `workspace.auto_renew` | `succeeded` | 1 |
  | `workspace.runtime_image_replacement` | `succeeded` / `failed` | 5 / 2 |
  | unnamed (`action: null`, identity digest only) | `succeeded` 17, `blocked` 2, `active` 1, `manual_review` 1, `failed` 1 | 22 |

  No operation is in `pending` or `running`, so the shared application-deployment
  worker's own scan condition (`workspace.application.deploy` with statuses
  `pending`/`running`) matches no live record in this observation. Every
  `workspace.launch.v2` and every `workspace.delete.v2` row is `succeeded`. The
  two `workspace.renewal` rows are `expired_unpaid`, which is terminal only when
  the operation's own recorded expiry phase is `complete`; the receipt projects
  the status but not that phase, so their terminality is not established.
- **What Remains Unproven Here**: Same-Candidate paid customer purchase
  execution on Tencent/TKE, J0 READY qualification bound to the latest
  Candidate, end-to-end custom Agent Package build-to-serve production delivery,
  and PlanChange v2 runtime resize implementation (tracked in
  [roadmap.md](roadmap.md)). The operation census above is `partial /
  owner_readback_partial` and covers the current owner API result rather than an
  audit archive, so it proves no terminality for historical operations. 22 of
  its 44 rows carry no action name at all and resolve to five
  `actionIdentityDigest` values that nothing in the receipt names. The Control
  Plane operation actions the retirement records obligations under
  (`workspace.delete.refund.v1`, `gateway.wallet_adjustment.v1`,
  `workspace.gateway_key.rotate`, `workspace.application.deploy`,
  `workspace.resource_reconcile`) are outside that tool's known-action
  projection, so a refund row, a wallet reservation, a rotation parent and a
  deployment child cannot be told apart from an unrelated action in this
  receipt. The one `active`, two `blocked` and one `manual_review` unnamed
  rows therefore stay unresolved (tracked in [roadmap.md](roadmap.md)).
