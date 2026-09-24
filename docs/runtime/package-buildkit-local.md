# Isolated Package / BuildKit verification

Run `npm run verify:package-to-oci` from a clean Cloud checkout with Go, Docker,
Buildx and a running local Docker engine. The command builds only test fixtures
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
The publisher identity decision is a test fixture. After digest-checked upload,
the production Build runner consumes those exact ZIP bytes and a fixed recipe.
A filesystem export verifies the actual Package and WebUI content. Registry
read failures and a reopened Build database then exercise the real worker's
unknown/recovery transitions while the builder is stopped.

This command does **not** qualify Runtime/WebUI catalog admission, all reference
claims, a live worker killed during push, complete Build/Capability Inbox/Outbox
acknowledgements, Ledger, Console, a protected Instance or release readiness.
It must not close #625 by itself. Those gaps are owned by [the roadmap](../roadmap.md).

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
