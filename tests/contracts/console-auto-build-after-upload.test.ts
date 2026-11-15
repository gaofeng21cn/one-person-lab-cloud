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

// The automatic build has to be generic. It starts for any package version an
// upload publishes and takes its runtime and WebUI versions from the
// installation's effective build policy, so the code that starts it may not
// carry one installation's package, repository, image, host or digest: a
// literal like that would make the trigger work for that installation only.
const triggerFiles = [
  "apps/console-bff/internal/httpapi/auto_build.go",
  "apps/console-bff/internal/httpapi/publisher.go",
];

const instanceFacts: { pattern: RegExp; what: string }[] = [
  { pattern: /[a-z0-9][a-z0-9.-]*\.[a-z]{2,}(?::\d+)?\/[a-z0-9._/-]+/i, what: "a repository or image reference" },
  { pattern: /(?:\d{1,3}\.){3}\d{1,3}/, what: "an IP address" },
  { pattern: /[a-z0-9.-]+:\d{2,5}\/[a-z0-9._/-]+/i, what: "a host:port repository reference" },
  { pattern: /["'][^"']*[0-9a-f]{16,}[^"']*["']/, what: "a pinned digest or identifier" },
];

// literals returns the string literals of a Go file without the import paths:
// a module path is not an instance fact.
function literals(contents: string): string[] {
  const withoutImports = contents.replace(/import\s*\([^)]*\)/gs, "").replace(/import\s+"[^"]*"/g, "");
  return [
    ...[...withoutImports.matchAll(/"(?:[^"\\]|\\.)*"/g)].map((match) => match[0]),
    ...[...withoutImports.matchAll(/`(?:[^`\\]|\\.)*`/g)].map((match) => match[0]),
  ];
}

test("the automatic build carries no instance fact of its own", async () => {
  for (const path of triggerFiles) {
    const contents = await readFile(path, "utf8");
    for (const literal of literals(contents)) {
      for (const { pattern, what } of instanceFacts) {
        assert.doesNotMatch(literal, pattern, `${path} must not carry ${what}: ${literal}`);
      }
    }
  }
});
