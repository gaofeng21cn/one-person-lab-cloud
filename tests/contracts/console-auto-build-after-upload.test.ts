import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

// The upload publishes a package version, and that package version is built
// with the installation's effective build policy. The Console owns the upload
// entry, and it already holds the Capability, Runtime Control and Build
// clients, so the automatic build is wired where the upload completes.
//
// The Console's Go tests are compiled but not executed by the CI `go` group,
// so this contract keeps the wiring visible to the CI `source` group.

const publisherPath = "apps/console-bff/internal/httpapi/publisher.go";
const autoBuildPath = "apps/console-bff/internal/httpapi/auto_build.go";

test("the upload completion starts the automatic build of the package version it published", async () => {
  const publisher = await readFile(publisherPath, "utf8");

  assert.match(publisher, /POST \/api\/v2\/uploads\/\{uploadId\}\/complete/,
    "the upload completion route must stay the entry that publishes a package version");
  assert.match(publisher, /s\.startAutomaticBuild\(r\.Context\(\), c, operation\.GetResourceId\(\)\)/,
    "the upload completion must start the automatic build of the package version the operation names");
});

test("the automatic build reads the effective build policy and hands its pair to the Build owner", async () => {
  const autoBuild = await readFile(autoBuildPath, "utf8");

  assert.match(autoBuild, /s\.runtimeControl\.GetBuildRuntimePolicy\(/,
    "the automatic build must read the installation's effective build policy");
  assert.match(autoBuild, /RuntimeVersionId:\s+policy\.GetRuntimeVersionId\(\)/,
    "the build must use the runtime version the policy names");
  assert.match(autoBuild, /WebuiVersionId:\s+policy\.GetDefaultWebuiVersionId\(\)/,
    "the build must use the default WebUI version the policy names");
  assert.match(autoBuild, /s\.build\.CreateBuild\(/,
    "the automatic build must ask the Build owner to build");
});

test("the automatic build carries its own authorization and its own idempotency key", async () => {
  const autoBuild = await readFile(autoBuildPath, "utf8");

  assert.match(autoBuild, /RequireAuthorizedAction\([\s\S]{0,240}?owneridentity\.Build/,
    "the Build owner authorizes the actor itself, so the automatic build needs a Build owner decision");
  assert.match(autoBuild, /buildCall\.IdempotencyKey = autoBuildKey\(packageVersionID\)/,
    "the automatic build must key itself by the package version, not by the caller's command key");
  assert.match(autoBuild, /func autoBuildKey\(packageVersionID string\) string \{\s*return "auto-build:" \+ packageVersionID/,
    "the automatic build key must be derived from the package version");
});

test("a missing or unusable build policy fails the upload instead of skipping the build", async () => {
  const autoBuild = await readFile(autoBuildPath, "utf8");

  assert.match(autoBuild, /code: "BUILD_POLICY_MISSING"/,
    "a missing build policy must be named to the caller");
  assert.match(autoBuild, /code: "BUILD_POLICY_INCOMPLETE"/,
    "a build policy that names no runtime or WebUI version must be named to the caller");
  assert.match(autoBuild, /code: "BUILD_NOT_STARTED"/,
    "a build the owner refuses must be named to the caller");
});
