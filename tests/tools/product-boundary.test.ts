import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { cp, mkdir, mkdtemp, rm, symlink, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

const repository = fileURLToPath(new URL("../../", import.meta.url));
const workflows = [
  "build-opl-cloud-candidate.yml",
  "clean-host-qualification.yml",
  "cloud-business-readback.yml",
  "codeql.yml",
  "pull-request-ci.yml",
  "qualification.yml",
  "release-opl-cloud-image.yml",
  "release-opl-cloud-public-readback.yml",
  "whitepaper.yml",
  "warm-ci-caches.yml"
];
const inputs = [
  "compose.yaml",
  "Dockerfile",
  "tools/validate-product-boundary.mjs",
  "packages/contracts/opl-cloud-distribution-contract.json",
  "packages/contracts/opl-cloud-candidate-receipt-contract.json",
  ...workflows.map((name) => `.github/workflows/${name}`)
];

// The boundary check resolves its repository root from the script location, so
// copy the real inputs into a scratch tree and run the real script there. Only
// node_modules is linked, because the script imports YAML from the repository.
async function boundaryFixture(): Promise<string> {
  const directory = await mkdtemp(join(tmpdir(), "opl-product-boundary-"));
  await mkdir(join(directory, ".github/workflows"), { recursive: true });
  await mkdir(join(directory, "tools"), { recursive: true });
  await mkdir(join(directory, "packages/contracts"), { recursive: true });
  for (const input of inputs) await cp(join(repository, input), join(directory, input));
  await symlink(join(repository, "node_modules"), join(directory, "node_modules"), "dir");
  return directory;
}

function validate(directory: string) {
  return spawnSync(process.execPath, ["tools/validate-product-boundary.mjs"], { cwd: directory, encoding: "utf8" });
}

test("the real boundary check accepts the single Candidate workflow", async () => {
  const directory = await boundaryFixture();
  try {
    const result = validate(directory);
    assert.equal(result.status, 0, result.stderr);
    assert.match(result.stdout, /distribution boundary is valid/);
  } finally {
    await rm(directory, { recursive: true, force: true });
  }
});

test("a second build workflow is refused even when it looks like the Candidate", async () => {
  const directory = await boundaryFixture();
  try {
    await cp(
      join(directory, ".github/workflows/build-opl-cloud-candidate.yml"),
      join(directory, ".github/workflows/build-opl-cloud-candidate-bridge.yml")
    );
    const result = validate(directory);
    assert.equal(result.status, 1, result.stdout);
    assert.match(result.stderr, /exactly one Cloud build workflow is allowed/);
    assert.match(result.stderr, /build-opl-cloud-candidate-bridge\.yml/);
  } finally {
    await rm(directory, { recursive: true, force: true });
  }
});
