# Install OPL Cloud

Download a complete portable **Release** or **Candidate** bundle from
[GitHub Releases](https://github.com/gaofeng21cn/one-person-lab-cloud/releases)
matching your target commit/tag and install from one bundle rather than mixing
assets across versions.

## Installation Preconditions

Before pulling images or starting containers, `install.sh` checks two host
requirements fail-closed:

1. **Docker Compose v2 (`docker compose`)** capable of parsing the shipped
   `compose.cloud.yaml` (standalone legacy `docker-compose` is unsupported).
2. **Reachable External Sub2API Authority**: `OPL_SUB2API_BASE_URL` must resolve
   and accept a TCP connection (skipped only under `--dry-run`). Sub2API
   remains external to `compose.cloud.yaml`.

## Standard One-Command Install & Update

```bash
# Download an admitted Release/Candidate bundle (example: current public Release v0.1.8 or newer)
gh release download <tag> --repo gaofeng21cn/one-person-lab-cloud --dir opl-cloud-bundle
cd opl-cloud-bundle

# Dry-run validation (verifies bundle SHA256SUMS, state & compose syntax)
bash install.sh --dry-run

# First run generates opl-cloud/installation.json and prompts for operator fields
# Fill Workspace domain, OPL_SUB2API_BASE_URL, provider profile & credentials, then re-run:
bash install.sh --install-dir /srv/opl-cloud
```

Re-running `bash install.sh` against an existing directory upgrades the stack
to the bundle's pinned `OPL_CLOUD_IMAGE` digest and `OPL_RELEASE_SHA` while
preserving PostgreSQL volumes and existing secrets.

## Shipped Runtime Topology (`compose.cloud.yaml`)

- **3-Container Portable Host Stack**:
  1. `postgres`: PostgreSQL 16 (isolated database and role pair per Cloud owner).
  2. `cloud-backend`: single-PID composed container running `/usr/local/bin/opl-cloud-composition` (`opl-cloud`) against the image-baked `/app/contracts/opl-cloud-owner-topology.json`, running all 11 Cloud domain modules (`gateway-integration`, `capability`, `build`, `runtime-control`, `resource-catalog`, `workspace`, `serve`, `control-plane`, `fabric`, `ledger`, `console-bff`) in-process over `owneridentity.InProcTransport` while exposing per-owner `OPL_GRPC_CERT_DIR` mTLS and HTTP listeners and keeping per-owner PostgreSQL roles (`opl_control_plane`, etc.).
  3. `cloud-local-v2`: same-origin TLS reverse proxy routing `/api/v2/` and Console SPA (`/`) to `console-bff`, `/objects/` to `capability`, `/w/` to `serve`, and `/api/` to `control-plane`.
- Local Docker is the single-host installation provider (Linux host with ext4/XFS
  project quotas required for full Workspace local execution). Production Tencent/TKE
  deployments are owned by `opl-instance-medopl`.

## Legacy Manual Compose Install (Deprecated Compatibility)

```bash
sha256sum --check --strict SHA256SUMS   # or: shasum -a 256 -c SHA256SUMS
cp opl-cloud.env.example .env           # fill installation-owned values
docker compose --env-file .env -f compose.yaml \
  -f compose.deployment-platform-owned.yaml \
  -f compose.fabric-local-docker.yaml -f compose.local-workspace.yaml pull
docker compose --env-file .env -f compose.yaml \
  -f compose.deployment-platform-owned.yaml \
  -f compose.fabric-local-docker.yaml -f compose.local-workspace.yaml up -d
```

## Kubernetes Admission & Rollback

- Kubernetes installations enforce cluster endpoint proofs, admission CLI checks
  and explicit Secret bindings owned by the target Instance repository.
- Rollback re-runs deployment with the prior verified Release bundle manifest and
  immutable image digest; PostgreSQL schema migrations are forward-compatible per
  release policy.
