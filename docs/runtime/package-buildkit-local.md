# Isolated Package / BuildKit Verification

Run `npm run verify:package-to-oci` from a clean Cloud checkout with Go, Docker,
Buildx, Node dependencies, Playwright Chromium and a local Docker daemon. It
uses disposable loopback `registry:2`, `postgres:16` and `moby/buildkit:buildx-stable-1`
containers (`OPL_BUILD_TEST_FRONTEND` overrides the pinned Dockerfile frontend).
No production secrets or public registry pushes are touched.

## What The Suite Exercises

1. **Real Owner Stores & gRPC/BFF Guards**: Live Capability gRPC/HTTP data-plane
   upload, CloudIdentity session/CSRF/idempotency/grant forwarding through
   Console BFF, and Playwright-driven ZIP upload -> Build -> ready version
   readback -> reload recovery.
2. **Post-Commit Ack-Loss Recovery**: Build and Capability simulate lost export/ack
   callbacks and worker crashes while Ledger consumes original events once; replays
   recover the exact OCI digest without re-exporting.
3. **Generated Shared JSON Codec**: Validates `packages/contracts/proto/generate_public_json_shape.py`
   codec consistency alongside strict schema validation.

## Runnable IBD Package -> OCI -> Workspace Handoff

Use `OPL_BUILD_TEST_HANDOFF_ONLY=1` with `OPL_BUILD_TEST_RUNTIME_CONTRACT`,
`OPL_BUILD_TEST_PACKAGE`, `OPL_CAPABILITY_PACKAGE_SCHEMA_PATH` and
`OPL_CAPABILITY_PACKAGE_SCHEMA_DIGEST` (fixture contract at
`services/build/internal/build/testdata/ibd-runtime-publisher-contract.json`,
password/session files via `OPL_WEBUI_PASSWORD_FILE` / `OPL_WEBUI_SESSION_SECRET_FILE`)
to prove exact Build input pinning through Local-Docker `Serve`/`Fabric` deployment.
Passing local BuildKit checks does not certify production Registry policies,
TLS/Instance configuration or TKE runtime qualification.
