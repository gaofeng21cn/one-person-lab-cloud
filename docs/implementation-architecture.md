# OPL Cloud Implementation Architecture

This reference owns Cloud's current source topology, request routing, module
ownership and database persistence layout. Product targets belong to
[architecture.md](architecture.md); durable safety and settlement rules belong to
[invariants.md](invariants.md); observed evidence belongs to [status.md](status.md).

## Request Path

```text
Browser Console (apps/console-ui)
  -> Console SPA static assets (/):
       Served by Console BFF (apps/console-bff) from OPL_CONSOLE_DIST_DIR (/app/dist)
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

The personal Gateway Key management actions are the customer's in every baked
identity: the Gateway owner decides whether a Key may be managed (the Workspace's
own managed Key is neither manageable nor revocable), and the Console offers
create, edit, revoke and reveal against the owner — the cloud identity through
the Console BFF, the retained Control Plane identity through the Control Plane's
own routes while those routes still exist.

### Console Browser Lifecycle

See [Console Browser Lifecycle](implementation/console.md) for route/hook
composition (`use-console-controller.ts`), identity-tagged query isolation,
transitional customer read fences and ephemeral Secret handling.

## Physical Module And Dependency Map

`one-person-lab-cloud` builds and versions all Go services under **one root Go
module** (`go.mod`, `module opl-cloud`) and compiles all 11 domain modules
(`services/<domain>/module`, `apps/console-bff/module`) into one unified
multi-call binary (`/usr/local/bin/opl-cloud` built from `cmd/opl-cloud/`, with
BusyBox-style symlinks for `opl-cloud-composition` and every `opl-<service>`).
Domain isolation is enforced by package boundaries, independent PostgreSQL
databases/roles and boundary tests
(`tests/contracts/module-physical-boundaries.test.ts`), not separate `go.mod`
files or per-service repositories.

| Owner / Module | Path | Persistence / Store | Transport & Allowed Dependencies |
| --- | --- | --- | --- |
| Console UI | `apps/console-ui/` | None (browser SPA) | Same-origin BFF `/api/v2/*` & legacy `/api/*` adapters only |
| Console BFF | `apps/console-bff/` | None (stateless HTTP/MCP proxy + Console SPA static host) | CloudIdentity + generated gRPC owner clients + authenticated CP HTTP bridges |
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

- **Single-Stage Build & Slim Runtime Image**: `Dockerfile` compiles **4** stamped Go binaries in a single shared `go-build` stage (`golang:1.27-bookworm`) — the unified multi-call `/usr/local/bin/opl-cloud` (`cmd/opl-cloud`) plus 3 standalone CLI helpers (`opl-tencent-provisioner`, `opl-node-image-retire`, `opl-owner-probe`) — alongside **12** BusyBox-style symlinks (`opl-cloud-composition` and all 11 `opl-<service>` names -> `opl-cloud`), running on `debian:bookworm-slim` without Node.js or `node_modules` and baking `/app/dist` and `/app/contracts/opl-cloud-owner-topology.json`.
- **Single-Binary In-Process Composed Backend**: `deploy/portable/compose.cloud.yaml` packages a 3-container host unit (`postgres`, single `cloud-backend` running `/usr/local/bin/opl-cloud-composition` -> `opl-cloud`, and same-origin `cloud-local-v2` reverse proxy). `composition.InProcStarter` launches all 11 domain modules in-process as goroutines inside **1 OS PID / 1 Go runtime** with contract-allowlisted `owneridentity.ModuleEnv`, routing intra-unit owner RPCs over zero-copy `owneridentity.InProcTransport` (preserving caller identity verification, bearer tokens, and `CloudIdentity` authorization) while keeping external TCP+mTLS/HTTP listeners for `opl-owner-probe`, reverse proxy, and Kubernetes readiness probes. Clean-host verification runs via `npm run verify:local-v2`.
- **Module Boundary Enforcement**: `tests/contracts/module-physical-boundaries.test.ts` enforces package and SDK isolation across all 11 Go services under `services/`, `apps/console-bff/`, and `services/internal/*`, restricts `cmd/opl-cloud` imports strictly to `packages/contracts/go`, `services/cloud-composition/composition`, and thin `<unit>/module` packages, and forbids direct `os.Getenv`/`os.LookupEnv` in domain `module/` runtime packages.
- **Instance Separation**: `opl-instance-medopl` owns TKE manifests, Secrets,
  domains and install/rollback receipts; protected Cloud diagnostic scripts live
  in `tools/` (`workspace-fabric-dispatch-error-readback.ts`,
  `workspace-compute-live-readback.ts`, `workspace-compute-destroy-journal.ts`,
  `workspace-compute-detach-reconciliation.ts`, `cloud-business-readback.ts`).

## Owner Transition

- **Retired Paths**: Control Plane package pricing catalog/preview, image-replacement
  creation/preview/worker/activation routes, and automated `workspace.gateway_key.rotate`
  creation/child deployments are retired. The legacy Launch descriptor,
  request-hash constructor and preflight confirmation are test fixtures for
  historical operations.
- **Retained Launch Executor**: The Control Plane worker continues accepted
  `workspace.launch.v2` operations using their persisted identities. See
  [Launch recovery](implementation/workspace-launch-recovery.md).
- **Preserved Historical Decoders**: `services/control-plane/internal/server/workspace_delete.go`
  and `workspace_store.go` keep read-only historical key-rotation decoding (`NewKeyID -> OldKeyID`)
  and `workspace_runtime_image_history.go` keeps historical image operation decoding
  strictly as deletion/reconciliation preconditions for pre-existing Workspaces.
- **Personal Gateway Key Writes**: `services/gateway-integration` declares and
  implements the native personal Key create/update/reveal/revoke RPCs on
  `GatewayProductService`, over the same delegated Sub2API authority and the same
  `PersonalGatewayKey` projection the personal reads already use. The Tenant-managed
  `GatewayKey` contract is separate and unchanged. `apps/console-bff` exposes the four
  personal-Key write routes over those RPCs, and the Console's cloud-identity path
  calls them instead of refusing the capability. The Control Plane's four
  `/api/gateway/keys` write routes are **not** migrated onto these RPCs and still
  call Sub2API directly; that migration, and only then the retirement of those
  routes, is the follow-up slice.
- **Open Extractions**: Tracked in [roadmap.md](roadmap.md#workspace-key-and-customer-read-boundaries).
