import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { chmodSync, mkdtempSync, mkdirSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";
import YAML from "yaml";

const workflow = YAML.parse(readFileSync(new URL("../../.github/workflows/build-cloud-agent-webui.yml", import.meta.url), "utf8"));
const readback = workflow.jobs.artifact.steps.find((step: { id?: string }) => step.id === "readback").run;
const digest = (character: string) => `sha256:${character.repeat(64)}`;
const sha = "a".repeat(40);

function fixture() {
  const image = (architecture: string) => ({ os: "linux", architecture, config: { Labels: {
    "org.opencontainers.image.revision": sha,
    "org.opencontainers.image.source": "https://github.com/gaofeng21cn/one-person-lab-cloud"
  } } });
  return {
    index: { digest: digest("c"), mediaType: "application/vnd.oci.image.index.v1+json", manifests: [
      { digest: digest("d"), platform: { os: "linux", architecture: "amd64" } },
      { digest: digest("e"), platform: { os: "linux", architecture: "arm64", variant: "v8" } }
    ] },
    amd64: image("amd64"), arm64: image("arm64")
  };
}

// Only the registry transport is replaced. Execute the workflow's actual Bash,
// jq JSON decoding, identity checks and checksums against captured buildx shapes.
function runReadback(data: ReturnType<typeof fixture>) {
  const root = mkdtempSync(join(tmpdir(), "opl-webui-readback-"));
  const artifacts = join(root, "artifact files");
  const bin = join(root, "bin");
  mkdirSync(artifacts); mkdirSync(bin);
  for (const [name, value] of Object.entries(data)) writeFileSync(join(root, `${name}.json`), JSON.stringify(value));
  writeFileSync(join(artifacts, "build-metadata.json"), JSON.stringify({ "containerimage.digest": digest("c") }));
  writeFileSync(join(artifacts, "source.txt"), `${sha}\n${"b".repeat(40)}\n`);
  writeFileSync(join(bin, "docker"), `#!/usr/bin/env node
const fs = require("node:fs");
const ref = process.argv[process.argv.indexOf("inspect") + 1];
const images = { "${digest("c")}": "index", "${digest("d")}": "amd64", "${digest("e")}": "arm64" };
const file = images[ref.split("@")[1]];
if (!file) process.exit(1);
process.stdout.write(fs.readFileSync(process.env.FIXTURE_ROOT + "/" + file + ".json"));
`);
  chmodSync(join(bin, "docker"), 0o755);
  const result = spawnSync("bash", ["--noprofile", "--norc", "-c", readback], {
    cwd: root, encoding: "utf8", timeout: 10000,
    env: { ...process.env, PATH: `${bin}:${process.env.PATH}`, FIXTURE_ROOT: root,
      ARTIFACT_DIR: artifacts, IMAGE_REPOSITORY: "ghcr.io/gaofeng21cn/opl-cloud-agent-webui",
      INDEX_DIGEST: digest("c"), PRODUCT_SHA: sha, PRODUCT_REPOSITORY: "gaofeng21cn/one-person-lab-cloud",
      GITHUB_STEP_SUMMARY: join(root, "summary.md") }
  });
  return { root, artifacts, result };
}

test("actual publication step retains matching registry identities, labels and verifiable checksums", () => {
  const data = fixture();
  const { root, artifacts, result } = runReadback(data);
  try {
    assert.equal(result.status, 0, result.stderr);
    assert.deepEqual(JSON.parse(readFileSync(join(artifacts, "index.json"), "utf8")), data.index);
    for (const architecture of ["amd64", "arm64"]) {
      const image = JSON.parse(readFileSync(join(artifacts, `${architecture}.image.json`), "utf8"));
      assert.equal(image.architecture, architecture);
      assert.equal(image.config.Labels["org.opencontainers.image.revision"], sha);
    }
    const checksums = spawnSync("sha256sum", ["-c", "SHA256SUMS"], { cwd: artifacts, encoding: "utf8" });
    assert.equal(checksums.status, 0, checksums.stderr);
    assert.equal(checksums.stdout.split("OK").length - 1, 5);
  } finally { rmSync(root, { recursive: true, force: true }); }
});

test("actual publication step refuses mismatched or ambiguous registry results", async t => {
  const cases: [string, (data: ReturnType<typeof fixture>) => void][] = [
    ["push/readback digest mismatch", data => { data.index.digest = digest("f"); }],
    ["unexpected index type", data => { data.index.mediaType = "application/vnd.docker.distribution.manifest.list.v2+json"; }],
    ["missing platform", data => { data.index.manifests.pop(); }],
    ["duplicate platform", data => { data.index.manifests[1].platform.architecture = "amd64"; }],
    ["shared child digest", data => { data.index.manifests[1].digest = data.index.manifests[0].digest; }],
    ["unexpected platform", data => { data.index.manifests[1].platform.os = "windows"; }],
    ["invalid child digest", data => { data.index.manifests[1].digest = "mutable-tag"; }],
    ["wrong image architecture", data => { data.arm64.architecture = "amd64"; }],
    ["wrong source revision", data => { data.amd64.config.Labels["org.opencontainers.image.revision"] = "b".repeat(40); }],
    ["wrong source repository", data => { data.arm64.config.Labels["org.opencontainers.image.source"] = "https://github.com/other/product"; }]
  ];
  for (const [name, mutate] of cases) await t.test(name, () => {
    const data = fixture(); mutate(data);
    const { root, result } = runReadback(data);
    try { assert.notEqual(result.status, 0, result.stdout); }
    finally { rmSync(root, { recursive: true, force: true }); }
  });
});
