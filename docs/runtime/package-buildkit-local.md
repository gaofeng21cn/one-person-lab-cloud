# Isolated Package / BuildKit verification

Run `npm run verify:package-to-oci` from a clean Cloud checkout with Go, Docker,
Buildx, the repository Node dependencies, Playwright Chromium and a running local
Docker engine. The command builds only test fixtures
and writes only disposable loopback Registry, BuildKit and PostgreSQL containers.
It uses no Instance environment, customer provider credentials or public push.

The test uses `registry:2`, `postgres:16`, `moby/buildkit:buildx-stable-1` and a
pinned Dockerfile frontend. Set `OPL_BUILD_TEST_FRONTEND` to another explicit
`repository@sha256:...` only when intentionally qualifying that frontend.
The local engine may download these tools when absent. BuildKit's temporary
container is privileged for its snapshotter, shares only the temporary Registry's
network namespace, and has no host Docker socket or Cloud database credentials.
Containers and their anonymous volumes are removed at test exit.

The test uses real Capability gRPC handlers, its restricted runtime database
role and the same HTTP data handler wired by `services/capability/cmd/server`.
CloudIdentity runs its real session, permission and accepted-operation-grant
implementation against its own PostgreSQL database. Its gRPC boundary verifies
the configured peer tokens. Domain transport uses explicit local test peers. The test exercises
actual same-origin BFF guards (session, CSRF, action/resource authorization,
idempotency and authorization-context forwarding), then real Capability and
Runtime APIs and separate owner stores. A production Console build is driven by
Playwright from the Workspace page's publisher link to ZIP upload, Build creation,
ready-version readback, desktop/mobile rendering and reload recovery.

Build and Capability each lose post-commit acknowledgements; Ledger consumes the
original upload/build/registration events through authenticated peer tokens and
its existing PostgreSQL receipt store. Replays must leave one result and no
pending deliveries. Forged producers and changed event bytes are rejected.

The actual worker runs in a child OS process. A test-only command wrapper runs
real Docker/BuildKit export, then withholds exporter exit acknowledgement. After
the Registry manifest is read back while the Job is still building, the test
kills the worker process group. A fresh worker with an unusable builder must
recover the original digest and register one version without exporting again.
This proves the post-commit acknowledgement-loss boundary, not a mid-layer
network partition. A separate persisted-state fixture still tests registry-read
outage and recovery with BuildKit stopped.

The browser now logs in through BFF and the real CloudIdentity issuer. Publisher
namespace and WebUI approval use their real authenticated BFF/Capability commands;
no publisher, WebUI, session, authorization-context or grant row is seeded. Only
the external Sub2API HTTP authentication boundary and existing test Tenant/member
records are controlled fixtures. No external wallet/account is registered.
The check does not qualify Instance credentials, TLS/registry policy, existing
identity-data migration or production deployment.

The shared public JSON codec is generated from the canonical schema vocabulary
with `python3 packages/contracts/proto/generate_public_json_shape.py` (also called
by the existing protobuf generation entrypoint). It serializes owner-created
public descriptors; it does not claim to reproduce an external publisher's
original JSON bytes or replace schema validation.

## Runnable IBD Build and Workspace handoff

The IBD Local-Docker scope is uploaded Package → real BuildKit OCI digest →
CapabilityVersion → Workspace/Serve/Fabric Local-Docker → ready route, password
login and a real Agent response through the authenticated native HTTP API. IBD
is the feasibility exemplar; this procedure adds no provider or generic WebUI
chat feature. The [historical Linux evidence](../status.md#linux-custom-agent-owner-qualification-2026-10-21)
records the bound source inputs, readbacks and qualification limits.

The existing live test accepts a runnable Runtime contract through
`OPL_BUILD_TEST_RUNTIME_CONTRACT`. The non-secret input used by the successful
Linux IBD case is retained at
`services/build/internal/build/testdata/ibd-runtime-publisher-contract.json`.
It binds the exact linux/amd64 Runtime, Agent Plugins schema, `carrier` source,
`/opt/ibd-carrier` target and native manifest installer. Its password/session
files are consumed through `OPL_WEBUI_PASSWORD_FILE` and
`OPL_WEBUI_SESSION_SECRET_FILE`. The fixture's frontend/recipe references are
replaced by the test's pinned frontend and disposable registry recipe; they are
not a production catalog admission. Runtime publication remains its owner's job.

Run from the repository root with explicit local inputs:

```sh
OPL_RUNTIME_IMAGE=uswccr.ccs.tencentyun.com/oplcloud/one-person-lab-app@sha256:8efab2844b52c66bcaa3da05637abc3cbdb0c3b29364718839b121284711de92 \
OPL_BUILD_TEST_PLATFORM=linux/amd64 \
OPL_BUILD_TEST_RUNTIME_CONTRACT="$PWD/services/build/internal/build/testdata/ibd-runtime-publisher-contract.json" \
OPL_BUILD_TEST_PACKAGE="$IBD_PACKAGE_ZIP" \
OPL_CAPABILITY_PACKAGE_SCHEMA_PATH="$AGENT_PLUGINS_SCHEMA_PATH" \
OPL_CAPABILITY_PACKAGE_SCHEMA_DIGEST=sha256:0a4aad95ce337878ad38802ebf0daa3fde76abe3f65400c86bcbb1ec0b3ab883 \
OPL_BUILD_TEST_HANDOFF="$BUILD_HANDOFF_PATH" \
OPL_BUILD_TEST_HANDOFF_ONLY=1 npm run verify:package-to-oci
```

The successful input ZIP was 219963 bytes, SHA-256
`1befa906cdfb4f343beb0f615df36588a03bda53abefdfd3a90f7a171bba2b19`;
it is an external Agent Package, not Cloud-owned source. It contains the carrier
under `carrier/` and its Agent Plugins manifest at `manifest.json`. Set the four
path variables above to existing isolated files; the schema bytes must match the
declared digest. `OPL_BUILD_TEST_FRONTEND` may select another explicit pinned
frontend; the test otherwise uses its checked-in default. The optional
`OPL_BUILD_TEST_RUNTIME_LOCAL=1` mirrors a pre-pulled Runtime only if its exact
manifest digest is preserved. A new action can produce a new OCI; read its actual
Build handoff rather than copying the historical successful digest.

The schema-2 handoff retains public CapabilityVersion JSON and internal protobuf
BuildArtifact JSON, including `confirmed` outcome, without registration grant or
browser session. A successful export deliberately retains the named Registry;
the caller owns its later removal and volume cleanup. A failed test removes its
own Registry. Handoff-only mode omits the publisher browser and later revoked-input
probe; the default test still executes them. The Linux consumer imports the
exact Build/Capability readbacks into its isolated Capability fixture. External
login and the approved Runtime/WebUI catalog also remain local fixtures; this
handoff does not establish installed catalog admission or live identity acceptance.

On an isolated Linux amd64 root host with Docker and an empty ext4 project-quota
mount, the existing Workspace test consumes that file using
`OPL_LOCAL_FIRST_BUILD_HANDOFF`. Supply `OPL_LOCAL_FIRST_DEPLOY_QUALIFICATION=1`,
`OPL_POSTGRES_TESTS=1`, `OPL_OWNER_MIGRATION_TEST_ADMIN_DSN` for disposable databases,
`OPL_TEST_PROJECT_QUOTA_ROOT`, and the existing
`OPL_QUALIFICATION_SERVE_BINARY` / `OPL_QUALIFICATION_FABRIC_BINARY` overrides for
executables built from the same source. The handoff Registry and exact image must
be accessible on that host; moving the harness across hosts requires preserving
the original manifest, config and layer digests without rebuilding.

Supply only the authorized selected provider/model TOML through
`OPL_QUALIFICATION_CODEX_CONFIG`. The test writes it into the validated ephemeral
`/data/codex/config.toml`, then uses the existing stop/start recovery to load it.
It never mounts the user's home or writes that configuration into the OCI or
evidence. Run `go -C services/workspace test -tags=livebuild ./internal/launch
-run '^TestLocalFirstApplicationDeploymentQualification$' -count=1 -timeout=15m -v`
with those variables. The Runtime probe assumes the existing OPL native Headless
HTTP protocol and exactly one writable `/data` mount; it is not a generic Runtime
adapter. macOS Docker Desktop cannot replace the required Linux quota host.

The probe logs in through `/api/auth/login`, reads the declared Agent through
`/api/opl/state?profile=fast`, then sends through native `/api/send-message` using
the application session and CSRF token. It requires `codex_app_server`, matching
thread/turn identities, canonical `completed` status and a nonempty reply. This
is authenticated HTTP API evidence; browser-driven sending is outside this
closure. Same-Candidate and installed Tencent/TKE acceptance remain separate.

This qualification ends after the positive delivery, login, response and recovery
checks. Its no-charge order has no paid period. Business deletion, resource
absence, a Ledger deletion receipt and original-charge refund require separate
owner execution. Preserve the sanitized Agent IDs/response hash and owner
readbacks before removing only the caller-owned test Registry/volumes, model
data and quota mount; harness cleanup is not business deletion evidence.

## Capability data plane

The Capability process now requires these explicit settings in addition to its
existing owner database, gRPC peers and approved Package schema configuration:

- `OPL_CAPABILITY_OBJECT_LISTEN_ADDR`: the HTTP bind address for its data plane.
- `OPL_CAPABILITY_STORAGE_KIND`: `local` (default) or `cos`. `local` keeps
  immutable bytes on a mounted volume; `cos` stores them in the instance Tencent
  COS bucket.
- `OPL_CAPABILITY_OBJECT_URL`: for `local`, the externally reachable URL used for
  signed upload-part URLs and Build object reads. HTTP is admitted only for
  loopback; an installed instance supplies its TLS endpoint. For `cos`, it is
  only the Build object-read endpoint because upload parts go straight to COS.
- `OPL_CAPABILITY_OBJECT_TOKEN`: a dedicated service-only read token of at least
  32 bytes, supplied to both Capability and Build through the existing approved
  secret store. It is never returned in upload permits or customer responses.

`OPL_CAPABILITY_STORAGE_KIND=cos` instead requires `OPL_CAPABILITY_COS_BUCKET`,
`OPL_CAPABILITY_COS_REGION`, `OPL_CAPABILITY_COS_SECRET_ID` and
`OPL_CAPABILITY_COS_SECRET_KEY` (optional `OPL_CAPABILITY_COS_ENDPOINT` for a
custom domain). The secret pair is a bucket-scoped sub-account credential held
only in the instance Secret store; it is never sent to a browser. Browser uploads
receive short-lived presigned URLs scoped to one upload part, so the bucket needs
CORS permitting the Console origin with `PUT`/`GET`/`HEAD`, exposing `ETag`, and
allowing the `X-OPL-SHA256` request header. With `cos`, Capability does not
register `PUT /parts` at all, and immutable objects stay content-addressed by
SHA-256 (`objects/sha256/<hex>`) so Build reads them by digest alone. A completed
assembly verifies the provider object's exact size and SHA-256 before it becomes
immutable; the customer-declared checksum is never trusted on its own.

With `local`, `PUT /parts` still requires its signed permit and exact checksum header.
`GET /objects/<sha256-hex>` requires the Build bearer token and an uploaded
PackageVersion with that immutable object identity. It exposes no object list,
write or deletion API. Build recomputes the object digest and length before
extracting ZIP entries. Production ingress and Secret distribution remain
Instance responsibilities; changing this source does not deploy that listener.

## Publisher and Ledger process wiring

The Console publisher entry is available from Workspaces at `/console/publisher`.
Its same-origin `/api/v2` requests must reach Console BFF. The existing BFF
CloudIdentity and Capability/Build peer configuration supplies authority; browser
actor/tenant headers and body fields are not accepted as identity. Upload parts
use only the owner-issued signed PUT permit, omit browser credentials, reject
redirects and verify the returned checksum identity. Only `/parts` permits
credentialless CORS and exposes its ETag; service-only object reads gain no CORS.

Ledger retains its HTTP `LEDGER_ADDR` listener. Set `OPL_LEDGER_ADDR` to enable
the typed domain Inbox on a separate listener, with `DATABASE_URL`, existing
`OPL_GRPC_*` certificate settings and `OPL_LEDGER_PEER_TOKENS` entries for Build
and Capability. Those producers use the matching Ledger address and their
approved `OPL_LEDGER_TOKEN`. Capability now requires this Ledger peer just as
Build already did. All configuration comes from the existing approved stores;
no Secret or deployment is created by this source change.

## CloudIdentity and catalog admission

`services/gateway-integration/cmd/server` is the real Tenant/CloudIdentity entry.
It consumes the Tenant database and migration roles using the same owner process
bootstrap as Capability and Build. The portable Dockerfile now includes this
binary and the publisher owner/BFF binaries; it does not deploy them.

In addition to the existing owner database and `OPL_GRPC_*` transport settings,
configure these through approved instance configuration/Secret stores:

- `OPL_TENANT_ADDR`, `OPL_TENANT_PEER_TOKENS`: Tenant listener and the explicit
  Console BFF/Capability/Build/Runtime Control inbound peer credentials.
- `OPL_SUB2API_URL`: existing Sub2API endpoint. HTTPS is required outside loopback.
  The adapter calls only `/api/v1/auth/login` and `/api/v1/auth/me`; it performs no
  registration, wallet, Key, charge or provider operation.
- `OPL_SESSION_SIGNING_KEY`: at least 32 secret bytes. Login challenges and CSRF
  values are signed by CloudIdentity; cookies are Secure, HttpOnly and SameSite.
- `OPL_GATEWAY_DIRECTORY_EMAIL`, `OPL_GATEWAY_DIRECTORY_PASSWORD`: required on
  every replica, and required together. CloudIdentity's own read-only
  administrative identity in the Gateway authority, used to resolve
  `Member.displayName`, to confirm an invited Gateway subject exists, and to
  confirm on each session read that the actor is still active. It is not a member
  credential and never acts as a member. Startup refuses a missing or partial
  directory configuration, so an instance cannot start with an identity that
  would reject every session read. These values must resolve to the approved
  read-only directory identity; startup validation does not prove its live
  credentials or the instance rollout.
- `OPL_INVITATION_TTL`: how long a pending Tenant invitation may be accepted, as a
  Go duration such as `168h`. The canonical contract fixes `Invitation.expiresAt`
  but names no window, so the deployment supplies it and CloudIdentity **refuses to
  start** without a positive value rather than baking a product decision into code.
  A deployment that does not yet issue invitations still must set it, because the
  process validates it during startup; this is an intentional fail-fast, not a
  default. It carries no secret and is safe to keep in ordinary instance
  configuration.
- `OPL_PLATFORM_ADMIN_SUBJECTS`: comma-separated, explicitly admitted Gateway
  subject IDs. Tenant ownership never implies this platform role.
- `OPL_BUILD_ADDR`, `OPL_BUILD_TOKEN`: typed owner-commit readback for grant
  issuance and resource-limited continuation. The Build peer allowlist includes
  Tenant. The shared `OPL_CLOUD_IDENTITY_URL`/token points to this authority for
  the other owners and shared Tenant operation readback. An address that is not
  configured leaves that owner's grant path refusing rather than dialing an empty
  target, because this authority is deployed before Build in the unit.
- `OPL_PUBLIC_URL`: BFF's configured public origin behind a TLS reverse proxy.
- `OPL_PUBLISHER_SCHEMA_PATH` and `OPL_PUBLISHER_SCHEMA_DIGEST`: Capability's exact
  canonical publisher schema; the image includes it at
  `/app/contracts/publisher-contract.schema.json`.

Build the successor Console with `VITE_CONSOLE_IDENTITY=cloud npm run build`
(or the Docker build argument of the same name). This selects v2 login/session/
logout explicitly; there is no automatic fallback to the retained Control Plane
issuer. The default retained-runtime build stays on its existing identity path
until the instance adopts the successor endpoints and data. Route all `/api/v2`
requests to BFF on the same public origin.

CloudIdentity stores cookie/CSRF hashes, non-bearer references and the login
display name in PostgreSQL. Session readback uses that durable row on every
replica and after a process restart, and does not require the member login token
to remain in process memory. Each read still resolves the active Gateway subject
through the read-only directory identity and checks the current Tenant membership
and permission epoch; an inactive actor or revoked membership is refused.
Migration `0003` adds nullable `display_name`, backfills existing rows from
`actor_id`, and readers use `COALESCE(display_name, actor_id)` so old-image writers
and image rollback remain compatible. A future required-column migration is not
part of this source and must wait until old writers and the rollback window are
retired. Accepted grants and original obligations remain durable after
logout/restart; every accepted
Build grant is bound to a separately read owner commit and its exact inputs.
Revoked/downgraded access permits bounded readback/cleanup, not new claims.

Publisher admission uses the existing `/api/v2/admin/catalog/publisher-namespaces`
and `/api/v2/admin/catalog/webui-versions` contracts. A platform administrator
supplies the declared admission receipt, exact namespace/repository scope and
complete immutable WebUI PublisherContract. Capability rejects overlapping
publisher prefixes, foreign image repositories, invalid contracts and revoked
inputs; changing a status does not replace any existing Workspace. These APIs
record catalog admission, not a fabricated external production qualification.

The publisher slice assumes existing, authorized Tenant membership. Public
Tenant provisioning, invitation management, retained-account migration and the
Gateway wallet facade remain their separate work packages; this check does not
claim those APIs are implemented.
