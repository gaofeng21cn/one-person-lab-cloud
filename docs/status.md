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
- **Fabric Provider Account Resolution**: Fabric resolves the ordering Tenant of
  an accepted plan to the provider account that runs its package. A binding in
  the deployment-owned provider profile names a Tenant that runs in a particular
  account; a Tenant that no binding names runs in the profile's
  `defaultAccountId`, so onboarding a customer needs no profile edit and no
  restart. A profile that declares neither refuses that Tenant. The profile
  itself stays an `opl-instance-medopl` deployment fact.
- **Console / BFF / Control Plane Boundary**:
  - CloudIdentity routes (`/api/v2/*`) serve Workspace, Quote, Launch, Capability
    upload/build, Serve delivery, Gateway wallet/usage summary, Agent read-only
    MCP (`/api/v2/mcp`), quote review and Gateway session-backed customer
    reads (announcements and legacy personal billing through authenticated
    Control Plane bridges).
  - The publisher can read the structured build log: the contract's
    `GET /api/v2/builds/{buildId}/logs` route is served by the BFF and forwards
    to the Build owner, and the upload page shows the owner's log lines with
    their stage and severity. The log answers why a build stopped; the build
    status alone never carried that reason.
  - The Cloud personal Gateway Key surface is writable: the Gateway owner
    reports a personal Key as manageable and revocable (the Workspace's own
    managed Key is neither), and the Console offers create, edit, revoke and
    reveal against the BFF. The Control Plane's own `/api/gateway/keys` write
    routes are still live and still write Sub2API directly; they are retired in
    the follow-up slice, once the retained Control Plane Console build has
    stopped calling them.
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
  - The native Workspace owner accepts new purchases. The Control Plane
    [Launch executor](implementation/workspace-launch-recovery.md) continues
    accepted operations carried forward from before the purchase cutover.
  - A current-owner API observation established that retained lifecycle
    operations can be enumerated. Its coverage was partial, and operation
    identities and terminal phases still need a complete readback for the
    deletion, refund, and renewal retirement evidence tracked in the roadmap.
- **Retained Application Deployment**: The shared Control Plane worker advances
  accepted `workspace.application.deploy` children through predecessor
  suspension, activation and receipt. The source qualification covers retained
  v0.1.8 fixtures, response loss and restart against the same database.
- **Renewal Obligation Authorization At The Identity Owner**: CloudIdentity issues
  and honors the bounded grant of an accepted renewal obligation. A Workspace
  owner that accepted a `renewWorkspace` obligation carries exactly the renewal
  surface: the provider extension that extends the prepaid deadline of the
  compute and storage the Workspace already owns, the renewal offer it reads and
  accepts, the charge that funds it, and the receipt that records it. The renewal
  surface is separate from the launch and the deletion surface, so a renewal can
  neither provision new resources nor release the ones it renews, and a launch
  grant cannot reach the provider extension. The Workspace owner still has to
  request that grant and run the renewal itself.
- **Paid TKE Deletion Recovery**: The November 25, 2026 production readback
  confirmed resource and data deletion, Key closeout, the Ledger deletion
  receipt, and the original-charge refund on the same deletion operation after
  an operator released its recycled CBS. See Instance readback run `45639384732`.
- **What Remains Unproven Here**: Same-Candidate paid customer purchase
  execution on Tencent/TKE, J0 READY qualification bound to the latest
  Candidate, end-to-end custom Agent Package build-to-serve production delivery,
  PlanChange v2 runtime resize implementation, and installed qualification of
  the retained application deployment worker (tracked in [roadmap.md](roadmap.md)).
