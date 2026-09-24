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
CloudIdentity and peer identity decisions are test fixtures. The test exercises
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

The shell login/CloudIdentity decisions, publisher namespace and approved WebUI
are explicit fixtures. `services/gateway-integration` remains the canonical
planned CloudIdentity owner: its absence is a Cloud implementation gap, not an
external or Instance blocker. This command does not qualify real identity/grant
issuance, publisher/WebUI admission, production credentials/TLS/registry policy,
a protected Instance or release readiness.

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
