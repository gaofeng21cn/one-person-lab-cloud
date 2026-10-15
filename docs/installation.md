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
Only Control Plane is published to the host. `/api/healthz` is a Control Plane
liveness check; it does not prove Fabric provider readiness, Workspace creation,
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

Current source constructs a Candidate with nine portable assets plus
`opl-cloud-candidate.json` and `SHA256SUMS`: one base Compose file, three
deployment overlays, two Fabric overlays, one Local-Workspace overlay, an
environment template and `opl-cloud-owner-topology.json`. The Candidate receipt
contract and builder own this exact set. The formal Product Release contract
still lists its separate ten-file promotion format with
`opl-cloud-release.json`; neither set describes the five public `v0.1.7` assets.
Deployment mode and Fabric provider are independent selections. Pair the base
`compose.yaml` with the selected deployment overlay and exactly one Fabric
overlay: `compose.fabric-local-docker.yaml` or `compose.fabric-tencent-tke.yaml`.
The current `compose.local-workspace.yaml` enables Workspace workers and supplies
local PostgreSQL storage; it does not select a Fabric provider or replace its
overlay.

Create `.env` from the same Candidate's bundled `opl-cloud.env.example`. For
source development, its source is `deploy/portable/opl-cloud.env.example`; the
root `.env.example` is not a replacement. Compose uses `.env` for interpolation;
a value there is not automatically passed into a service container. The current
base file does not forward `OPL_FABRIC_PROVIDER`; the selected Fabric overlay
sets it and supplies the provider configuration and mounts. Current Fabric
rejects an absent provider with `OPL_FABRIC_PROVIDER is required`. On a clean
Compose startup, this prevents Control Plane from starting because it depends
on Fabric being healthy. This current-source requirement does not apply to the
historical `v0.1.7` base file above.

Fabric's `/healthz` is a liveness check, while `/fabric/readiness` calls the
provider's readiness checks. The Local-Docker provider validates its Secret
root, storage root and provider profile, and readiness reports those validation
errors. It also requires Workspace storage on a dedicated ext4/XFS mount with
project quota enabled and rejects unsupported layouts before Launch mutation.
These configuration and health checks do not establish Candidate qualification;
see [Candidate Qualification](runtime/release.md#candidate-qualification).

The retained Compose services are Control Plane, Fabric and Ledger. The product
image also carries Console BFF and the extracted
domain-owner executables, but the base profile does not install them. A conforming
owner-backed installation materializes the addresses, databases, peer identities,
required variables and browser HTTP surfaces declared by
`opl-cloud-owner-topology.json`. The Instance owner supplies its concrete
manifest, Secrets and certificates.

For an owner-backed installation, Gateway Integration requires both its Tenant
and Gateway databases. Set `OPL_TENANT_DATABASE_URL` for CloudIdentity and
`OPL_GATEWAY_DATABASE_URL` for Gateway. These must identify distinct databases
with their own writer roles and connection pools; changing only DSN formatting
does not provide isolation. Login and live-session validation consume the
Gateway store for the User↔Sub2API User and User↔OCI Destination mappings, so
Gateway DB is required even when settlement and Key operations are not selected
for development. Supply these values through the installation's protected
configuration. This requirement does not change the historical v0.1.7 procedure
above or establish that an Instance has adopted the source change.

Fabric keeps its HTTP provider bridge and owner RPC boundary in the same process.
The optional RPC surface uses `OPL_FABRIC_ADDR` (default `:8187`),
`OPL_FABRIC_DATABASE_URL` and `OPL_FABRIC_PEER_TOKENS`. Its template admits the
actual Workspace, Serve and Tenant transport principals; method-level guards
still decide which of those peers may perform each operation. Installation must
configure the RPC peer tokens separately from the HTTP service token.

Console identity is an image-level fact: `VITE_CONSOLE_IDENTITY` is a build
argument with default `legacy`, recorded by the image label
`opl.cloud.console-identity`. `legacy` uses the retained Control Plane APIs;
`cloud` uses Console BFF for authentication, Workspace, Publisher and delivery,
while shared Gateway/Billing and other unmigrated panels retain Control Plane
APIs. A runtime environment variable cannot change that baked bundle. Owner
binaries alone do not prove that the selected APIs are routed or ready. See the
[current request map](implementation-architecture.md#request-path).

The installation model has three separate axes: `OPL_DEPLOYMENT_MODE` is the
deployment-owner value (`platform_owned`, `managed_tke`, or `customer_owned`),
`OPL_FABRIC_PROVIDER` is the execution provider (`local-docker` or
`tencent-tke`), and the `admin`/`customer` surface is derived from login and
Console routing. Customer Console does not select a provider. The current
`managed_tke` path is paired with `tencent-tke`; `managed_tke` with
`local-docker` is rejected, while `platform_owned` with `local-docker` remains
qualification-only.

`compose.fabric-tencent-tke.yaml` forwards Fabric every installation fact its
Tencent provider validates or reports: `OPL_WORKSPACE_DOMAIN`,
`OPL_CLOUD_IMAGE`, `OPL_WORKSPACE_IMAGE`, `OPL_K8S_NAMESPACE`,
`OPL_IMAGE_PULL_SECRET_NAME`, `OPL_WORKSPACE_STORAGE_CLASS`,
`OPL_TENCENT_PROVISIONER_BIN`, `TENCENT_DEPLOY_KUBECONFIG_REF` and
`RUN_TENCENT_CREATE_RELEASE_EXECUTION`. It also mounts the TKE deploy
kubeconfig read-only from `TENCENT_DEPLOY_KUBECONFIG_HOST_PATH` to
`TENCENT_DEPLOY_KUBECONFIG_REF`, because Fabric is the only process that holds
the cluster credentials. `RUN_TENCENT_CREATE_RELEASE_EXECUTION=1` is the
provisioner's explicit live-mutation opt-in; `0` keeps an installation
read-only.

Five further facts protect the installation's own system node:
`OPL_SYSTEM_COMPUTE_NODE_POOL_ID`, `OPL_SYSTEM_COMPUTE_MACHINE_ID`,
`OPL_SYSTEM_COMPUTE_NODE_NAME`, `OPL_SYSTEM_COMPUTE_MACHINE_TYPE` and
`OPL_SYSTEM_COMPUTE_CVM_ID`. Fabric's protected-resource guard refuses every
mutating `kubectl` action unless they identify that node, so the overlay
requires them and the environment template declares them. The same names are
validated by `services/control-plane/ops/production-manifest.ts`.

`OPL_WORKSPACE_APPLICATION_DOMAIN` is the installer's own per-binding
application-origin domain; the base Compose forwards it to the Control Plane,
and leaving it empty publishes no application origins rather than an address
that cannot resolve.

A Candidate is not a Product Release. Its files are admitted and qualified as
one checksum-bound set from one exact Cloud SHA and image digest. Candidate
construction admits the workflow's allowed branch refs; formal publication
requires the exact source to be merged into canonical `main`. The
Instance owner supplies the domain, provider profile, immutable Workspace image
catalog, production Secrets, deployment, rollback, and receipts. Only after the
same Candidate bytes pass the required Local and Instance qualification may an
authorized publisher promote those bytes without rebuilding them.
The [October 15 ownership decision](decisions.md#2026-10-15-workspace-key-lifecycle-and-cloud-customer-reads)
separates Instance installation evidence from Cloud-owned product qualification.
The current receipt producers and release validators have not yet migrated;
[release operations](runtime/release.md) describes their enforced mechanism.
Installing Cloud does not authorize customer business operations.

No public successor currently contains this format. For current source
development and qualification, use the repository-owned tooling and
[developer guide](../DEV_GUIDE.md); do not present a locally generated Candidate
as a public Release.

## Kubernetes Installation Admission CLI

From the exact Product checkout matching the installation, validate the full
Kubernetes `v1/List`, including Deployments, Services, NetworkPolicies and referenced
ConfigMaps:

```bash
node /path/to/exact-product/tools/validate-production-manifest.ts \
  --installation-manifest installation-bound.json > installation-report.json
```

The Instance renderer must bind the approved namespace before this call. This
entry performs local manifest analysis; it neither resolves live cluster DNS nor
reads Secrets or connects to private endpoints. The existing `--manifest` mode
retains its production-environment validation behavior.

`deploy/portable/opl-cloud-owner-topology.json` owns the explicit owner RPC edges,
transport principals, activation requirements and token minimum length. Gateway's
transport principal is `tenant`; BFF is `console_bff`. The declarations cover the
BFF owner clients, shared CloudIdentity authorizer, owner coordination/readback
clients, Build's additional `OPL_TENANT_TOKEN` call at the CloudIdentity address,
and Serve's distinct Fabric coordination address/token. Configured optional edges
are checked without making unrelated provider/profile variables mandatory.
Retained HTTP transports, including variables not consumed by this checkout's
BFF, are outside this owner-RPC contract.

The report has `ok`, fixed-code `errors` and `communication`. Successful
`communication.edges` contain `caller`, `target`, `addressEnv`, `tokenEnv`,
`callerDeployment`, `targetDeployment`, `port`, `tokenRef: {name,key}`,
`peerRef: {name,key}`, `ingressPolicies` and `egressPolicies`. `port` is the
resolved target container TCP port, after Service `port`/`targetPort` mapping.
`peerRef` comes from the actual target container's declared peer-token environment.
Policy arrays identify selecting policies; they do not assert policy ownership.
`communication.security.tokenMinimumLength` is mandatory (currently 32), so a
protected executor can validate Secret contents without inventing Product policy.
The executor must reject missing/invalid security requirements, short tokens,
missing caller principals and token mismatches; the CLI itself checks references
and never reads Secret contents.

The CLI proves Service selection, the target listener, non-optional Secret
references, target ingress admission and caller egress admission when egress is
isolated. It evaluates additive policies, selector conjunction, namespace facts,
named ports and ranges. An absent/empty listener uses the Product-declared
`defaultAddress` or `defaultRPCAddress`; an explicit invalid value is refused.
Same ports in different Pods are valid. Same-Deployment Service calls still need
policy proof because a Service can route to another replica. ConfigMap `envFrom`
and relevant `configMapKeyRef` values are resolved from the List in Kubernetes
precedence order; explicit env overrides imported ConfigMap data. Unrelated
profile placeholders are not rendered or printed.

This report format supports one Product namespace and one provable Deployment
per transport principal. Ambiguous Service backends, opaque Secret `envFrom`,
host networking, arbitrary IP targets and policies whose required namespace/IP
facts cannot be proved are refused. A non-isolated ingress is refused rather than
reported as protected closure. This is manifest-level communication admission,
not live CNI enforcement, certificate validity, application health or business
qualification evidence.

Successful admission exits 0. Failure exits 1 with no executable edges; stdout
remains JSON and stderr contains `installation_manifest_invalid`. Values of
addresses, tokens and profiles are omitted. Input errors still include the Product
security requirement; an unreadable/invalid Product security contract instead
returns `security: null`, an error and no edges, which must never be executed.

## Local Qualification Fixture

`deploy/portable/compose.local-qualification.yaml` runs the repository-owned
Sub2API authority fixture, so a Local installation can be qualified without an
external wallet backend. The fixture serves one qualification user and one
optional qualification admin, including the delegated identity read that
`customer_owned` requires. Both deployment modes can therefore be qualified
against it:

- `platform_owned` and `managed_tke` authenticate with
  `OPL_SUB2API_ADMIN_EMAIL` / `OPL_SUB2API_ADMIN_PASSWORD`.
- `customer_owned` additionally requires `OPL_SUB2API_USER_EMAIL` /
  `OPL_SUB2API_USER_PASSWORD` and signs in as that configured user.

The fixture is qualification-only. It carries no real balance authority, holds
no production credential, and keeps its state in the
`opl-qualification-sub2api` volume.

## Local Qualification Fixture Balance Envelope

`deploy/portable/compose.local-qualification.yaml` runs the repository-owned
Sub2API authority fixture so a Local installation can be qualified without an
external wallet backend. The fixture models one qualification run rather than a
general wallet: its state file holds **one debit and one refund**, each refused
by name once used (`debit_identity_conflict` / `refund_identity_conflict`).

The envelope is deliberate. It is what makes a second charge under a different
code impossible to mistake for a missing one, which is the property a
qualification run must be able to prove. A repeat of the *same* code replays
idempotently instead of charging again.

Consequences an operator must plan for:

- A qualification run consumes the envelope. `OPL_QUALIFICATION_STATE_PATH` is
  the reset boundary: start a fresh run from a fresh state file rather than
  retrying the dispatch that was refused.
- Scenarios that need a second debit against the same state file — a second
  Workspace purchase, or a monthly renewal — are outside the fixture's envelope.
  Qualify those against a live authority instead.
- `customer_owned` installations do not debit resource charges at all, so the
  envelope does not constrain them.

## Upgrade and Rollback

For an admitted Product Release, set `OPL_CLOUD_IMAGE` to the immutable digest
in that Release manifest, then run `docker compose pull` and
`docker compose up -d` with exactly that Release's files. Rollback uses the same
procedure with another available admitted Release. Because only `v0.1.7` is
currently public, there is no earlier public rollback target. Mutable `latest`
and `stable` tags are not publication or rollback evidence.

Serve application access requires the Workspace peer read: set
`OPL_WORKSPACE_ADDR` and `OPL_WORKSPACE_TOKEN`, and include `serve` in
`OPL_WORKSPACE_PEER_TOKENS`. An absent/unreadable verdict never permits a route.
