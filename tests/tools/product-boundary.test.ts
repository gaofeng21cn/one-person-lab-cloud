import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { cp, mkdir, mkdtemp, readFile, rm, symlink, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

const repository = fileURLToPath(new URL("../../", import.meta.url));
const workflows = [
  "build-cloud-agent-webui.yml",
  "build-opl-cloud-candidate.yml",
  "clean-host-qualification.yml",
  "cloud-business-readback.yml",
  "codeql.yml",
  "pull-request-ci.yml",
  "qualification.yml",
  "release-opl-cloud-image.yml",
  "release-opl-cloud-public-readback.yml",
  "whitepaper.yml",
  "warm-ci-caches.yml",
  "workspace-compute-readback-handoff.yml"
];
const inputs = [
  "compose.yaml",
  "Dockerfile",
  "tools/validate-product-boundary.mjs",
  "packages/contracts/opl-cloud-distribution-contract.json",
  "packages/contracts/opl-cloud-candidate-receipt-contract.json",
  "deploy/portable/installer-manifest.mjs",
  "deploy/portable/compose.cloud.yaml",
  "deploy/portable/opl-cloud-owner-topology.json",
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
  await mkdir(join(directory, "deploy/portable"), { recursive: true });
  for (const input of inputs) await cp(join(repository, input), join(directory, input));
  await symlink(join(repository, "node_modules"), join(directory, "node_modules"), "dir");
  return directory;
}

function validate(directory: string) {
  return spawnSync(process.execPath, ["tools/validate-product-boundary.mjs"], { cwd: directory, encoding: "utf8" });
}

// The owner-topology declaration lives in the distribution contract, so the
// mutations below rewrite that contract inside the scratch tree and let the
// real validator refuse it.
async function mutateContract(directory: string, mutate: (contract: any) => void) {
  const path = join(directory, "packages/contracts/opl-cloud-distribution-contract.json");
  const contract = JSON.parse(await readFile(path, "utf8"));
  mutate(contract);
  await writeFile(path, `${JSON.stringify(contract, null, 2)}\n`);
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

test("WebUI input publication cannot widen into Product source or protected Instance execution", async () => {
  const changes = [
    ["git archive \"$PRODUCT_SHA\" webui/cloud-agent", "git archive \"$PRODUCT_SHA\""],
    ["runs-on: ubuntu-latest", "runs-on: self-hosted"],
    ["    timeout-minutes: 15", "    environment: production\n    timeout-minutes: 15"],
    ["github.sha == inputs.product_sha", "github.sha != inputs.product_sha"]
  ];
  for (const [before, after] of changes) {
    const directory = await boundaryFixture();
    try {
      const path = join(directory, ".github/workflows/build-cloud-agent-webui.yml");
      const source = await readFile(path, "utf8");
      assert.ok(source.includes(before));
      await writeFile(path, source.replace(before, after));
      const result = validate(directory);
      assert.equal(result.status, 1, result.stdout);
      assert.match(result.stderr, /Cloud Agent WebUI workflow authority boundary is invalid/);
    } finally {
      await rm(directory, { recursive: true, force: true });
    }
  }
});


test("an unregistered workflow is refused even when copied from the admitted read-only receiver", async () => {
  const directory = await boundaryFixture();
  try {
    await cp(
      join(directory, ".github/workflows/workspace-compute-readback-handoff.yml"),
      join(directory, ".github/workflows/unregistered-readback.yml")
    );
    const result = validate(directory);
    assert.equal(result.status, 1, result.stdout);
    assert.match(result.stderr, /unexpected Cloud workflow: unregistered-readback\.yml/);
  } finally {
    await rm(directory, { recursive: true, force: true });
  }
});

test("the admitted receiver still requires the exact existing Release secret environment and owner gate", async () => {
  for (const [before, after] of [
    ["environment: cloud-release", "environment: production"],
    ["github.ref == 'refs/heads/main'", "github.ref_type == 'branch'"],
    ["github.actor == github.triggering_actor", "true"],
    ["github.actor == 'RenDeHuang'", "github.actor == 'unapproved'"],
  ]) {
    const directory = await boundaryFixture();
    try {
      const path = join(directory, ".github/workflows/workspace-compute-readback-handoff.yml");
      const source = await readFile(path, "utf8");
      assert.ok(source.includes(before));
      await writeFile(path, source.replace(before, after));
      const result = validate(directory);
      assert.equal(result.status, 1, result.stdout);
      assert.match(result.stderr, /Cloud publisher credential requires its protected environment and owner gate: workspace-compute-readback-handoff\.yml:admission/);
    } finally {
      await rm(directory, { recursive: true, force: true });
    }
  }
});

test("the published entrypoint cannot drift back to a Compose file that is not the owner topology", async () => {
  const directory = await boundaryFixture();
  try {
    await mutateContract(directory, (contract) => { contract.portableInstallation.entrypoint = "compose.yaml"; });
    const result = validate(directory);
    assert.equal(result.status, 1, result.stdout);
    assert.match(result.stderr, /entrypoint must be the owner topology Compose file: compose\.yaml != compose\.cloud\.yaml/);
  } finally {
    await rm(directory, { recursive: true, force: true });
  }
});

test("a dropped owner process is refused even when the contract still names an entrypoint", async () => {
  const directory = await boundaryFixture();
  try {
    await mutateContract(directory, (contract) => {
      contract.portableInstallation.processes = contract.portableInstallation.processes.filter((name: string) => name !== "ledger");
    });
    const result = validate(directory);
    assert.equal(result.status, 1, result.stdout);
    assert.match(result.stderr, /processes must equal the owner topology processes in order/);
  } finally {
    await rm(directory, { recursive: true, force: true });
  }
});

test("the candidate bundle must carry the entrypoint and every file it references", async () => {
  const mutations: Array<[string, (contract: any) => void, RegExp]> = [
    ["compose.cloud.yaml",
      (contract) => {
        const format = contract.bundleFormats.formats[contract.bundleFormats.current];
        format.candidateAssets = format.candidateAssets.filter((name: string) => name !== "compose.cloud.yaml");
        format.releaseAssets = format.releaseAssets.filter((name: string) => name !== "compose.cloud.yaml");
      },
      /candidate bundle must carry the declared entrypoint: compose\.cloud\.yaml/],
    ["compose.local-v2.yaml",
      (contract) => {
        const format = contract.bundleFormats.formats[contract.bundleFormats.current];
        format.candidateAssets = format.candidateAssets.filter((name: string) => name !== "compose.local-v2.yaml");
        format.releaseAssets = format.releaseAssets.filter((name: string) => name !== "compose.local-v2.yaml");
      },
      /candidate bundle must carry every file the entrypoint references: compose\.local-v2\.yaml/]
  ];
  for (const [, mutate, expected] of mutations) {
    const directory = await boundaryFixture();
    try {
      await mutateContract(directory, mutate);
      const result = validate(directory);
      assert.equal(result.status, 1, result.stdout);
      assert.match(result.stderr, expected);
    } finally {
      await rm(directory, { recursive: true, force: true });
    }
  }
});

test("the owner topology contract must not quietly enter the public Release bundle", async () => {
  const mutations: Array<[(contract: any) => void, RegExp]> = [
    [(contract) => {
      const format = contract.bundleFormats.formats[contract.bundleFormats.current];
      format.releaseAssets = [...format.releaseAssets, "opl-cloud-owner-topology.json"];
    }, /must not be published in the Release bundle: opl-cloud-owner-topology\.json/],
    [(contract) => {
      const format = contract.bundleFormats.formats[contract.bundleFormats.current];
      format.releaseAssets = "candidateAssets";
    }, /must declare its release assets explicitly instead of aliasing the candidate assets: portable-v1/]
  ];
  for (const [mutate, expected] of mutations) {
    const directory = await boundaryFixture();
    try {
      await mutateContract(directory, mutate);
      const result = validate(directory);
      assert.equal(result.status, 1, result.stdout);
      assert.match(result.stderr, expected);
    } finally {
      await rm(directory, { recursive: true, force: true });
    }
  }
});

test("the candidate-only declaration of the owner topology contract cannot be dropped", async () => {
  const directory = await boundaryFixture();
  try {
    await mutateContract(directory, (contract) => { delete contract.bundleFormats.candidateOnlyAssets; });
    const result = validate(directory);
    assert.equal(result.status, 1, result.stdout);
    assert.match(result.stderr, /must declare the owner topology contract Candidate-only: opl-cloud-owner-topology\.json/);
  } finally {
    await rm(directory, { recursive: true, force: true });
  }
});
