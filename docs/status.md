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
  - The native Workspace owner accepts new purchases. The Control Plane
    [Launch executor](implementation/workspace-launch-recovery.md) continues
    accepted operations carried forward from before the purchase cutover.
  - A current-owner API observation established that retained lifecycle
    operations can be enumerated. Its coverage was partial, and operation
    identities and terminal phases still need a complete readback for the
    deletion, refund, and renewal retirement evidence tracked in the roadmap.
- **Retired Application Deployment Worker**: The shared Control Plane application
  deployment Worker (write driver, operation-progress CAS store, worker
  registration, portable/local `OPL_WORKSPACE_APPLICATION_DEPLOYMENT_WORKER_ENABLED`)
  is retired at the source layer: it had no production creator, so it could only
  ever advance an already accepted historical intent. Its persisted intent, its
  version and binding decoders, the historical read routes
  (`GET /api/operator/application-deployments/{operationId}`), the Workspace
  columns and the historical rows stay. The source-layer upgrade qualification
  (`services/control-plane/internal/server/workspace_application_upgrade_qualification_test.go`)
  covers retained v0.1.8 fixtures, response loss and restart against the same
  database, and is a source result, not installed adoption; the retirement is
  merged only after the installed qualification receipt proves the
  carried-forward operation can finish under its original operation, resource
  and digest identities.
- **What Remains Unproven Here**: Same-Candidate paid customer purchase
  execution on Tencent/TKE, J0 READY qualification bound to the latest
  Candidate, end-to-end custom Agent Package build-to-serve production delivery,
  PlanChange v2 runtime resize implementation, and installed qualification of
  the retained application deployment worker (tracked in [roadmap.md](roadmap.md)).
