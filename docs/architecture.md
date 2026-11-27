# OPL Cloud Architecture

Owner: `opl-cloud`
Purpose: `architecture_boundary`
State: `active_target_reference`

This document defines static responsibility boundaries, repository/instance
topology and domain ownership for OPL Cloud. Current implementation details
belong to [implementation-architecture.md](implementation-architecture.md);
durable safety and settlement formulas belong to [invariants.md](invariants.md);
evidence and open gaps belong to [status.md](status.md) and [roadmap.md](roadmap.md).

## OPL Family Position

The external product model is `OPL Base + OPL App + OPL Packages + OPL Cloud`:

| Layer / Repo | Canonical Responsibility |
| --- | --- |
| `one-person-lab` (OPL Framework) | Generic runtime, Package projection/discovery, Cordis Base/Host graph, Runway execution |
| `one-person-lab-app` | Desktop App product, client profiles, GUI/MCP ABI and app releases |
| `opl-studio` | Active DSH/Cordis desktop, WebUI and Docker Application Host |
| `opl-link` | Remote companion mobile client and broker/pairing transport |
| `one-person-lab-cloud` (`opl-cloud`) | Online Console/BFF, Cloud domain services, contracts and portable Cloud releases |
| `opl-instance-medopl` | Concrete production instance: domains, Tencent/TKE profile, Secrets, deploy/rollback and install receipts |
| External `Sub2API` | Spendable balance, API Keys, model routing and Token usage authority |

Framework's in-process `Console` contribution and Cloud's account/governance `OPL Console` remain distinct authorities. Switching the App shell or local host never turns Cloud services into desktop plugins.

## Family Capability Domains And Cloud Surfaces

| Domain | Cloud Surface & Owner | Outside Cloud |
| --- | --- | --- |
| `Console` | Console UI/BFF (`/api/v2/*`); Control Plane retains unmigrated panels | App local UI; Framework operator projections |
| `Workspace` | Workspace service (identity, membership, plans, Launch/Delete/PlanChange Sagas) | Package bytes (`Capability`), Agent deployment (`Serve`) |
| `Capability` / `Build` / `Runtime Control` | Package & WebUI catalog, three-input OCI BuildKit jobs, approved Runtime catalog | Upstream OPL App/Framework runtime implementation |
| `Serve` | Unique per-Workspace current-application selection, deployment, readiness & access routing (`API / Embed / Hosted UI`) | Provider VM/disk CRUD (`Fabric`) |
| `Fabric` | Provider-neutral compute, storage, attachment, Secret binding & adapters (`tencent-tke`, `local-docker`) + logical `Connect` | Application logic, spendable wallet |
| `Gateway / Wallet` | `Gateway Integration` (`tenant` + `gateway` mappings, `gateway.key_bindings`, read proxies) | `Sub2API` ledger & Key authority |
| `Ledger` | Append-only financial/deletion receipts, reconciliation & opaque provenance | Domain workflow orchestration |

## Repository And Instance Topology

```text
one-person-lab-cloud (module opl-cloud)
  -> portable Candidate / Release bundle + compose.cloud.yaml + owner containers
        | immutable product SHA + image digest + schema hashes
        v
opl-instance-medopl (Instance profile + protected workflows)
  -> resolves production Secrets, TKE profile, deploy/rollback & install receipts
```

### Target Directory And Service Map

| Path | Boundary | Owns |
| --- | --- | --- |
| `apps/console-ui/` | React / TypeScript SPA | Presentation & same-origin BFF/CP requests |
| `apps/console-bff/` | Go BFF in `opl-cloud` | Session security, CSRF, MCP `/api/v2/mcp`, owner DTO aggregation & Console SPA static serving |
| `services/gateway-integration/` | Go service (`tenant` + `gateway` DBs) | CloudIdentity sessions/Tenants, external identity mappings, `key_bindings` |
| `services/capability/` | Go service (`capability` DB) | Publisher namespaces, Agent Package uploads/versions, approved WebUI catalog |
| `services/build/` | Go service (`build` DB) | Immutable BuildJobs & OCI artifacts (no Cloud/Sub2API DB access) |
| `services/runtime-control/` | Go service (`runtime_control` DB) | Approved Runtime Release catalog |
| `services/resource-catalog/` | Go service (`resource_catalog` DB) | Compute/storage/product offers & PlanChange policies |
| `services/workspace/` | Go service (`workspace` DB) | Workspace orders, entitlements, Sagas & target authorization |
| `services/serve/` | Go service (`serve` DB) | Per-Workspace deployment lifecycle, readiness & access bindings |
| `services/fabric/` | Go service (`fabric` DB) | Compute/storage/network/Secret operations & provider adapters |
| `services/ledger/` | Go service (`ledger` DB) | Financial/deletion receipts & reconciliation evidence |
| `services/control-plane/` | Go service (`control_plane` DB) | Unmigrated legacy Launch/account/announcement/billing compatibility |
| `services/internal/` | Shared policy-free internal Go libs | Narrow cross-owner utilities (two-caller minimum, e.g. `postgresmigrate`) |
| `packages/contracts/` | Single repo root `go.mod` + proto + JSON schemas | Cross-owner gRPC/protobuf (`proto/`, `go/`) and immutable byte contracts |

### Deployment Model & Unit Composition

- `OPL_DEPLOYMENT_MODE` (`platform_owned`, `managed_tke`, `customer_owned`) and `OPL_FABRIC_PROVIDER` (`local-docker`, `tencent-tke`) vary independently; `opl-instance-medopl` binds `managed_tke` + `tencent-tke`. macOS does not support `local-docker` project quotas for production Workspace verification.
- Domain boundaries do not force separate deployment containers: Cloud ships a unified 3-container portable topology (`compose.cloud.yaml`: PostgreSQL + one composed `cloud-backend` container running all 11 owner processes via `opl-cloud-composition` against the image-baked `/app/contracts/opl-cloud-owner-topology.json` + same-origin proxy `cloud-local-v2`) while keeping BuildKit workers and running customer Workspace containers outside backend privileges. The runtime image (`debian:bookworm-slim`) contains no Node.js runtime or `node_modules`.

## Candidate And Installation-Data Boundary

- Cloud produces an immutable **Candidate** manifest binding Git SHA, image digests, installation scripts and `SHA256SUMS`, plus diagnostic validators and business evidence custody.
- Instance supplies one canonical non-secret **Instance profile** for both install and upgrade; protected Instance workflows inject Secrets/kubeconfig at execution time and emit redacted installation receipts bound to Candidate identity (and cross-stage profile digests once adopted on Instance). Cloud never reads Instance Secrets, DSNs or raw cluster state.

## Workspace Application Boundary

### Provisioning And Application Combinations

Customer purchases decouple resource fulfillment from application delivery:

1. **Default OPL App (`opl_app(runtimeVersionId)`)**: Workspace provisions compute/storage (`Fabric`) and deploys the approved Runtime Release and built-in UI straight through `Serve`, generating no synthetic Package, BuildJob or CapabilityVersion.
2. **Custom Built Agent (`agent(capabilityVersionId)`)**: User selects uploaded `packageVersionId` + approved `runtimeVersionId` + independent approved `webuiVersionId`. **Build** freezes all three inputs (rejecting partial selections or raw Dockerfiles/arbitrary URLs) into one immutable OCI digest and `DeploymentDescriptor` before `Capability` registers `CapabilityVersion` and `Serve` deploys it.

### DDD Consistency & Ownership Rules

- **One Current Application**: A Workspace has 0 or 1 active application selection owned solely by `Serve` (`serve.agent_runtime_instances`). Replacing an Agent or Runtime updates the active Serve slot without repurchasing compute/storage unless resource expansion requires a `PlanChange`.
- **Isolated Origins & Persistence**: Each Workspace-application binding derives its own deterministic HTTPS origin (`*.<root>`) isolated from Cloud platform cookies/routes. Persistent application data attaches to Workspace CBS/volumes keyed by stable application identity rather than tags or attempts.
- **Model Config & Secret Lifecycle**: Public model updates reconcile allowed models through `Gateway Integration` (never rotating keys automatically), bind or replace runtime Secrets via `Fabric.BindSecret`/`RebindSecret` (at most one active binding per purpose), and advance Workspace revision only after `Serve` readback succeeds.

## Host, Client, And Cloud Authority Boundary

`one-person-lab-cloud` does not import Framework/App Hosts (`@deepseek-ai/cordis` / desktop GUI shells), and App/Studio never embeds Cloud database roles, Sub2API credentials, provider SDKs or Cloud billing/Saga writes. Cross-surface integration occurs only through published Cloud APIs/BFF `/api/v2/*`, `/api/v2/mcp` and `runtime/cloud-connect.md`.

## Capability Design Review

Before introducing a new service, contract or cross-owner integration, verify:
1. Exactly one canonical writer owns each stateful table/fact;
2. Cross-owner data flows use typed public protobuf/gRPC or authenticated HTTP readbacks (no cross-DB joins or sibling source imports);
3. Provider-specific SDKs (`tencentcloud-sdk-go`, `github.com/moby/*`) remain isolated inside `services/fabric/`; Kubernetes SSA client access stays isolated behind `services/internal/tkeapply` (`services/fabric/` and `services/serve/`), and BuildKit CLI invocation stays isolated inside `services/build/`;
4. Monetary, Secret, and irreversible deletion paths enforce idempotency keys, CAS revisions and readback-before-retry fences.
