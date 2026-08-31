# Install OPL Cloud

OPL Cloud publishes portable product artifacts; a concrete Instance owns its
deployment, domain, Secrets, provider profile, rollback, and acceptance. This
page distinguishes the currently downloadable Product Release from the newer
Candidate format in source. Do not combine files from those two formats.

## Public Product Release: v0.1.7

`v0.1.7` is the only public Product Release. It binds:

- product SHA `a59bde68397528186a5220f73195fa1f3eda311b`;
- multi-architecture GHCR digest
  `sha256:e64504731f8b61c0864cf59faa647a1150e8a2a5eada34b26faf3a5487d28e8f`;
- exactly five downloadable assets:
  `compose.yaml`, `compose.local-workspace.yaml`,
  `opl-cloud.env.example`, `opl-cloud-release.json`, and `SHA256SUMS`.

Earlier public tags and artifacts are no longer available and are not rollback
targets. `v0.1.7` predates the current deployment/Fabric overlay split; files
such as `compose.deployment-*.yaml` and `compose.fabric-*.yaml` do not belong to
this Release.

### Requirements

- Docker Engine and Docker Compose v2 on `linux/amd64` or `linux/arm64`;
- a reachable Sub2API installation and an active administrator identity;
- a TLS reverse proxy when exposing the Console beyond localhost;
- for the optional Local-Docker Workspace path, an immutable Workspace image
  and a supported Linux Docker host. This older Release does not contain the
  current project-quota storage contract or current provider-profile split.

### Verify the assets

Download all five assets from the same `v0.1.7` GitHub Release, then verify the
checksums before reading values from the manifest:

```bash
if command -v sha256sum >/dev/null; then
  sha256sum --check --strict SHA256SUMS
else
  shasum -a 256 -c SHA256SUMS
fi
```

The four files named in `SHA256SUMS` must match. Also verify that
`opl-cloud-release.json` names `v0.1.7`, the product SHA above, and the same
immutable GHCR digest. The GitHub API asset digests are independent public
readback for the downloaded bytes.

### Start the control services

Create `.env` from `opl-cloud.env.example`, replace every placeholder, and keep
all database passwords, service tokens, capability keys, and password seeds
independent. Then start only the base file:

```bash
docker compose --env-file .env -f compose.yaml pull
docker compose --env-file .env -f compose.yaml up -d
docker compose --env-file .env -f compose.yaml ps
curl --fail http://127.0.0.1:8787/api/healthz
```

This starts PostgreSQL, Ledger, Fabric, and Control Plane as separate processes.
Only Control Plane is published to the host. A successful health check proves
that these control services started; it does not prove Workspace creation,
provider delivery, billing correctness, or Instance readiness.

The services require verified PostgreSQL TLS or, when TLS is explicitly
disabled, an RFC1918 address. If the default Docker subnet conflicts with
another network, change `OPL_DOCKER_SUBNET` and `OPL_POSTGRES_HOST` together.

### Optional v0.1.7 Local-Docker path

The fifth asset, `compose.local-workspace.yaml`, grants Docker access only to
Fabric, selects `local-docker`, and enables the Launch worker. Set
`OPL_WORKSPACE_IMAGE` to an immutable `repository@sha256:...` reference, then
start both files:

```bash
docker compose --env-file .env \
  -f compose.yaml \
  -f compose.local-workspace.yaml pull
docker compose --env-file .env \
  -f compose.yaml \
  -f compose.local-workspace.yaml up -d
```

This is the historical `v0.1.7` Local-Docker path. It is not the current source
Candidate path and must not be supplemented with newer overlays copied from
`main`.

## Current Candidate Format

Current source constructs a ten-asset Candidate bundle: one base Compose file,
three deployment overlays, two Fabric overlays, one Local-Workspace overlay,
an environment template, a manifest, and checksums. Deployment mode and Fabric
provider are independent selections. The current Local-Docker provider also
requires Workspace storage on a dedicated ext4/XFS mount with project quota
enabled and rejects unsupported layouts before Launch mutation.

A Candidate is not a Product Release. Its files are admitted and qualified as
one checksum-bound set from one canonical Cloud SHA and image digest. The
Instance owner supplies the domain, provider profile, immutable Workspace image
catalog, production Secrets, deployment, rollback, and receipts. Only after the
same Candidate bytes pass the required Local and Instance qualification may an
authorized publisher promote those bytes without rebuilding them.

No public successor currently contains this format. For current source
development and qualification, use the repository-owned tooling and
[developer guide](../DEV_GUIDE.md); do not present a locally generated Candidate
as a public Release.

## Upgrade and Rollback

For an admitted Product Release, set `OPL_CLOUD_IMAGE` to the immutable digest
in that Release manifest, then run `docker compose pull` and
`docker compose up -d` with exactly that Release's files. Rollback uses the same
procedure with another available admitted Release. Because only `v0.1.7` is
currently public, there is no earlier public rollback target. Mutable `latest`
and `stable` tags are not publication or rollback evidence.
