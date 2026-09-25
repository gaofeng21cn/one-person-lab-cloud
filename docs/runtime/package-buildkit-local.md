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

## Capability data plane

The Capability process now requires these explicit settings in addition to its
existing owner database, gRPC peers and approved Package schema configuration:

- `OPL_CAPABILITY_OBJECT_LISTEN_ADDR`: the HTTP bind address for its data plane.
- `OPL_CAPABILITY_OBJECT_URL`: the externally reachable URL used for signed
  upload-part URLs and Build object reads. HTTP is admitted only for loopback;
  an installed instance supplies its TLS endpoint.
- `OPL_CAPABILITY_OBJECT_TOKEN`: a dedicated service-only read token of at least
  32 bytes, supplied to both Capability and Build through the existing approved
  secret store. It is never returned in upload permits or customer responses.

`PUT /parts` still requires its signed permit and exact checksum header.
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
- `OPL_PLATFORM_ADMIN_SUBJECTS`: comma-separated, explicitly admitted Gateway
  subject IDs. Tenant ownership never implies this platform role.
- `OPL_BUILD_ADDR`, `OPL_BUILD_TOKEN`: typed owner-commit readback for grant
  issuance and resource-limited continuation. The Build peer allowlist includes
  Tenant. The shared `OPL_CLOUD_IDENTITY_URL`/token points to this authority for
  the other owners and shared Tenant operation readback.
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

CloudIdentity stores only cookie/CSRF hashes and non-bearer references. Delegated
Gateway access credentials live in its bounded-lifetime process memory, as in
the retained credential-vault model: an identity-process restart requires browser
reauthentication. Accepted grants survive in PostgreSQL and original obligations
remain available after logout/restart. Every interactive call checks the live
Gateway identity and current Tenant membership/permission epoch; every accepted
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
