# OPL Cloud Implementation Architecture

This reference owns Cloud's current source topology, request routing, module
ownership and database persistence layout. Product targets belong to
[architecture.md](architecture.md); durable safety and settlement rules belong to
[invariants.md](invariants.md); observed evidence belongs to [status.md](status.md).

## Request Path

```text
Browser Console (apps/console-ui)
  -> CloudIdentity paths (/api/v2/*):
       Console BFF (apps/console-bff)
       -> CloudIdentity session & audience authorization (services/gateway-integration)
       -> typed gRPC owners:
            Capability / Build / Runtime Control / Resource Catalog / Workspace / Serve / Gateway Integration
       -> Fabric (provider resources) & Ledger (receipts/provenance)
  -> Transitional BFF -> Control Plane bridges (/api/v2/announcements, /api/v2/legacy-billing/*):
       authenticated HTTP (/internal/cloud/*) + Gateway live subject proof -> original stores
  -> Remaining unmigrated legacy panels & original Launch recovery (/api/*):
       Control Plane (services/control-plane) -> Sub2API + Fabric/Ledger HTTP
```

Idempotency keys and operation IDs are opaque caller tokens validated for
format; session/tenant authorization, CSRF/origin validation, owner subject
matching and redacted projections enforce security independently.

### Agent Reads And Quote Review

`apps/console-bff` exposes read-only MCP tools at `/api/v2/mcp` (`cloud_list_compute_plans`,
`cloud_list_storage_plans`, `cloud_list_workspaces`, `cloud_get_quote`,
`cloud_get_workspace`, `cloud_get_workspace_operation`). Exact quote reads return
a same-origin `reviewPath`; stale revisions fail with `stale_revision`. External
Agents obtain short-lived (max 1 hour), session-bound connection credentials via
`/api/v2/agent/connections` and [Cloud Connect](runtime/cloud-connect.md) without
translating cookies or minting renewable PATs.

## Console Source Truth

`apps/console-ui` compiles a single build-time `VITE_CONSOLE_IDENTITY` (`cloud` or
`legacy`) checked by response `source` tags (`gateway`, `sub2api`, `control_plane`,
`ledger`, `workspace`). There is no cross-identity silent fallback.

### Console Browser Lifecycle

See [Console Browser Lifecycle](implementation/console.md) for route/hook
composition (`use-console-controller.ts`), identity-tagged query isolation,
transitional customer read fences and ephemeral Secret handling.

## Physical Module And Dependency Map

`one-person-lab-cloud` builds and versions all Go services under **one root Go
module** (`go.mod`, `module opl-cloud`). Domain isolation is enforced by package
boundaries, independent PostgreSQL databases/roles and boundary tests
(`tests/contracts/module-physical-boundaries.test.ts`), not separate `go.mod`
files or per-service repositories.

| Owner / Module | Path | Persistence / Store | Transport & Allowed Dependencies |
| --- | --- | --- | --- |
| Console UI | `apps/console-ui/` | None (browser SPA) | Same-origin BFF `/api/v2/*` & legacy `/api/*` adapters only |
| Console BFF | `apps/console-bff/` | None (stateless HTTP/MCP proxy) | CloudIdentity + generated gRPC owner clients + authenticated CP HTTP bridges |
| CloudIdentity & Gateway Integration | `services/gateway-integration/` | `opl_tenant` & `opl_gateway` (two distinct DBs) | gRPC/protobuf, external Sub2API HTTP APIs, OCI registry identity |
| Capability | `services/capability/` | `opl_capability` | gRPC/protobuf + storage provider (`filesystem` / presigned COS) |
| Build | `services/build/` | `opl_build` | gRPC/protobuf + BuildKit worker + OCI Registry exporter |
| Runtime Control | `services/runtime-control/` | `opl_runtime_control` | gRPC/protobuf + publisher contract admission |
| Resource Catalog | `services/resource-catalog/` | `opl_resource_catalog` | gRPC/protobuf + plan/offer/pricing catalog |
| Workspace | `services/workspace/` | `opl_workspace` | gRPC/protobuf + Fabric/Ledger/Gateway/Serve coordination |
| Serve | `services/serve/` | `opl_serve` | gRPC/protobuf + Fabric/Gateway/first-delivery Runtime adapters |
| Fabric | `services/fabric/` | `opl_fabric` | gRPC + HTTP (`destroy-status` / `reconcile-prepaid-detach`), provider SDKs (`tencentcloud-sdk-go`, Docker) |
| Ledger | `services/ledger/` | `opl_ledger` | gRPC event listener (`ownerservice`) + HTTP receipt APIs |
| Control Plane (retained) | `services/control-plane/` | `opl_control_plane` | Retained `/api/*`, `/internal/cloud/*`, `GET /api/operator/workspace-runtime-image-policy`; no new Workspace orders or Serve current-agent writes |
| Shared Internal Infra | `services/internal/` | None | Policy-free shared runtime libs (`postgresmigrate`, `ownerservice`, etc.) with >=2 callers |
| Shared Contracts | `packages/contracts/` | None | `proto/`, generated Go under `packages/contracts/go/` (same root `go.mod`), immutable JSON schemas |

### Persistence

All Go services use **`database/sql`** with explicit typed SQL stores and
advisory-locked migrations via `services/internal/postgresmigrate` (`ent` ORM is
retired). Each owner connects with its own PostgreSQL schema/writer credentials
defined in `deploy/portable/compose.cloud.yaml` and
`deploy/portable/opl-cloud-owner-topology.json`; cross-owner SQL joins or table
reads are forbidden.

## Deployment & Diagnostic Units

- **Composed Backend**: `deploy/portable/compose.cloud.yaml` packages PostgreSQL,
  all 11 owner processes and the same-origin reverse proxy as one portable host
  unit. Clean-host verification runs via `npm run verify:local-v2`.
- **Instance Separation**: `opl-instance-medopl` owns TKE manifests, Secrets,
  domains and install/rollback receipts; protected Cloud diagnostic scripts live
  in `tools/` (`workspace-fabric-dispatch-error-readback.ts`,
  `workspace-compute-live-readback.ts`, `workspace-compute-destroy-journal.ts`,
  `workspace-compute-detach-reconciliation.ts`, `cloud-business-readback.ts`).

## Owner Transition

- **Retired Paths**: Control Plane package pricing catalog/preview, image-replacement
  creation/preview/worker/activation routes, the monthly renewal/expiry execution
  engine with its worker and the operator billing-review resolve route, and
  automated `workspace.gateway_key.rotate` creation/child deployments are retired.
  The application runtime convergence writer
  (`services/control-plane/internal/server/workspace_application_lifecycle.go`)
  is retired with the delete and renewal executors that were its only callers;
  its inventory reader, its historical operation decoders and validators, and
  the shared application deployment worker that still writes a runtime lifecycle
  stay. The fresh-create side of the original Launch is
  retired too: `workspaceLaunchDescriptor`, its constructors, the request-hash calculator
  and the preflight confirmation have no production caller and now exist only in the
  retained test support that builds a historical Launch record. The `requestHash` a
  persisted row carries is still decoded and still bounds the Fabric stage request hash,
  so the identity of an accepted purchase is unchanged.
- **Retained Launch Executor**: The Launch worker is the executor of a non-terminal
  `workspace.launch.v2` operation. An in-place upgrade from public v0.1.8 can carry an
  operation accepted before the create cutover forward in any non-terminal phase, the
  scheduler cursor is process-local state that starts empty on every boot, and no other
  retained command can advance it — the history routes only read and the operator
  recovery route only advances a `manual_review` operation. The customer-owned Key stage
  is the one state no credential-free executor can advance, so the customer route keeps
  its single continuation branch. See
  [Launch recovery reference](implementation/workspace-launch-recovery.md).
- **Preserved Historical Decoders**: `services/control-plane/internal/server/workspace_delete.go`
  and `workspace_store.go` keep read-only historical key-rotation decoding (`NewKeyID -> OldKeyID`),
  `workspace_runtime_image_history.go` keeps historical image operation decoding
  strictly as deletion/reconciliation preconditions for pre-existing Workspaces,
  and `workspace_renewal_history.go` keeps the `workspace.renewal` operation
  decoder, receipt shapes, terminal-state classifier, recovery-state reader and
  recovery-eligibility reader for renewal records an earlier release wrote.
- **Open Extractions**: Tracked in [roadmap.md](roadmap.md#workspace-key-and-customer-read-boundaries).
