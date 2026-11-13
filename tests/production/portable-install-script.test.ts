import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { createHash } from "node:crypto";
import { chmod, mkdir, mkdtemp, readFile, stat, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";
import { canonicalJson, PORTABLE_ASSETS } from "../../tools/cloud-candidate-receipt.ts";

const IMAGE_DIGEST = `sha256:${"a".repeat(64)}`;
const IMAGE_REFERENCE = `ghcr.io/gaofeng21cn/one-person-lab-cloud@${IMAGE_DIGEST}`;
const SOURCE_SHA = "1".repeat(40);
const SOURCE_TREE = "2".repeat(40);
const AMD64_DIGEST = `sha256:${"b".repeat(64)}`;
const ARM64_DIGEST = `sha256:${"c".repeat(64)}`;

const DEPLOYMENT_OVERLAYS = [
  "compose.deployment-platform-owned.yaml",
  "compose.deployment-managed-tke.yaml",
  "compose.deployment-customer-owned.yaml"
];
const FABRIC_OVERLAYS = [
  "compose.fabric-local-docker.yaml",
  "compose.fabric-tencent-tke.yaml"
];

// Every asset of the bundle, and where the repository keeps it.
function sourceOf(name: string) {
  if (name === "compose.yaml") return "compose.yaml";
  return `deploy/portable/${name}`;
}

function sha256Of(bytes: Buffer) {
  return createHash("sha256").update(bytes).digest("hex");
}

// The manifest of one Candidate. The installer reads the image reference and
// the product revision from it, so the shape here is the shape the tool
// refuses to guess at.
function candidateReceipt(assets: { name: string; sha256: string }[]) {
  return {
    assets,
    cloudImage: {
      indexDigest: IMAGE_DIGEST,
      indexRef: IMAGE_REFERENCE,
      platforms: [
        { digest: AMD64_DIGEST, platform: "linux/amd64" },
        { digest: ARM64_DIGEST, platform: "linux/arm64" }
      ],
      repository: "ghcr.io/gaofeng21cn/one-person-lab-cloud",
      revision: SOURCE_SHA
    },
    kind: "opl_cloud_candidate",
    product: {
      repository: "gaofeng21cn/one-person-lab-cloud",
      sha: SOURCE_SHA,
      tree: SOURCE_TREE
    },
    provenance: {
      workflowRepository: "gaofeng21cn/one-person-lab-cloud",
      workflowRunAttempt: "1",
      workflowRunId: "1",
      workflowSha: SOURCE_SHA
    },
    schemaVersion: 2
  };
}

// One bundle on disk, exactly as a Candidate download holds it.
async function writeBundle(root: string) {
  const bundle = join(root, "bundle");
  await mkdir(bundle, { recursive: true });
  const assets: { name: string; sha256: string }[] = [];
  for (const name of PORTABLE_ASSETS) {
    const bytes = await readFile(sourceOf(name));
    await writeFile(join(bundle, name), bytes);
    if (name === "install.sh") await chmod(join(bundle, name), 0o755);
    assets.push({ name, sha256: sha256Of(bytes) });
  }
  const receipt = `${canonicalJson(candidateReceipt(assets))}\n`;
  await writeFile(join(bundle, "opl-cloud-candidate.json"), receipt);
  const lines = [];
  for (const name of [...PORTABLE_ASSETS, "opl-cloud-candidate.json"]) {
    lines.push(`${sha256Of(await readFile(join(bundle, name)))}  ${name}`);
  }
  await writeFile(join(bundle, "SHA256SUMS"), `${lines.join("\n")}\n`);
  return bundle;
}

// A Docker that records what it was asked to do and lets the real Compose
// parse the environment the installer derived.
async function writeDockerStub(root: string) {
  const bin = join(root, "bin");
  await mkdir(bin, { recursive: true });
  const real = spawnSync("command", ["-v", "docker"], { encoding: "utf8" });
  const docker = real.status === 0 ? real.stdout.trim() : "";
  const stub = `#!/bin/sh
echo "docker $*" >> "$DOCKER_LOG"
case "$1 $2" in
  "compose version") echo "Docker Compose version v2.29.0"; exit 0 ;;
  "compose config") exec ${docker} "$@" ;;
esac
exit 0
`;
  await writeFile(join(bin, "docker"), stub, { mode: 0o755 });
  return bin;
}

async function runInstaller(bundle: string, bin: string, arguments_: string[] = [], cwd = bundle) {
  const log = join(bundle, "..", "docker.log");
  const result = spawnSync("bash", [join(bundle, "install.sh"), ...arguments_], {
    cwd,
    encoding: "utf8",
    env: { ...process.env, PATH: `${bin}:${process.env.PATH}`, DOCKER_LOG: log }
  });
  const calls = await readFile(log, "utf8").catch(() => "");
  return { ...result, calls };
}

async function withBundle(body: (bundle: string, bin: string) => Promise<void>) {
  const root = await mkdtemp(join(tmpdir(), "opl-cloud-install-"));
  try {
    const bundle = await writeBundle(root);
    const bin = await writeDockerStub(root);
    await body(bundle, bin);
  } finally {
    await spawnSync("rm", ["-rf", root]);
  }
}

test("the portable bundle ships the installer as one of its assets", async () => {
  assert.ok(PORTABLE_ASSETS.includes("install.sh"), "the candidate receipt must own the installer");
  const mode = (await stat("deploy/portable/install.sh")).mode;
  assert.ok(mode & 0o111, "the installer must be executable");

  // The Candidate bundle and the Release bundle both copy the installer, and
  // both name it in the file set they verify. An asset that is copied but not
  // listed is an asset nothing checks.
  const candidate = await readFile(".github/workflows/build-opl-cloud-candidate.yml", "utf8");
  const release = await readFile(".github/workflows/release-opl-cloud-image.yml", "utf8");
  for (const [name, workflow] of [["candidate", candidate], ["release", release]]) {
    assert.ok(workflow.includes("install.sh"), `the ${name} workflow must carry the installer`);
  }
  assert.match(candidate, /cp "\$BUILD_CONTEXT\/deploy\/portable\/install\.sh" "\$bundle\/install\.sh"/);
  assert.match(candidate, /\{name:"install\.sh",sha256:\$installer\}/);
  assert.match(release, /release\/install\.sh/);
});

test("the candidate bundle accepts the installer as one of its assets", async () => {
  await withBundle(async (bundle) => {
    const validated = spawnSync(process.execPath, [
      "tools/cloud-candidate-receipt.ts",
      "validate-bundle",
      "--bundle",
      bundle
    ], { encoding: "utf8" });
    assert.equal(validated.status, 0, `${validated.stdout}${validated.stderr}`);

    // A bundle that does not carry the installer is not this bundle: the file
    // set is the contract, and the contract changed with the installer.
    await writeFile(join(bundle, "SHA256SUMS"), (await readFile(join(bundle, "SHA256SUMS"), "utf8"))
      .split("\n").filter((line) => !line.endsWith("  install.sh")).join("\n"));
    const refused = spawnSync(process.execPath, [
      "tools/cloud-candidate-receipt.ts",
      "validate-bundle",
      "--bundle",
      bundle
    ], { encoding: "utf8" });
    assert.notEqual(refused.status, 0);
    assert.match(`${refused.stdout}${refused.stderr}`, /cloud_candidate_bundle_invalid/);
  });
});

test("the installer refuses a bundle whose file does not match its checksum", async () => {
  await withBundle(async (bundle, bin) => {
    await writeFile(join(bundle, "compose.yaml"), `${await readFile(join(bundle, "compose.yaml"), "utf8")}\n# tampered\n`);
    const result = await runInstaller(bundle, bin);
    assert.notEqual(result.status, 0);
    assert.match(result.stderr, /does not match its checksum/);
  });
});

test("the installer refuses a file the checksum manifest does not name", async () => {
  await withBundle(async (bundle, bin) => {
    await writeFile(join(bundle, "compose.override.yaml"), "services: {}\n");
    const result = await runInstaller(bundle, bin);
    assert.notEqual(result.status, 0);
    assert.match(result.stderr, /unverified file/);
  });
});

test("the installer takes the image and the revision from the bundle's own manifest", async () => {
  await withBundle(async (bundle, bin) => {
    const result = await runInstaller(bundle, bin);
    assert.equal(result.status, 2, `${result.stdout}${result.stderr}`);

    const environment = await readFile(join(bundle, ".env"), "utf8");
    assert.match(environment, new RegExp(`^OPL_CLOUD_IMAGE=${IMAGE_REFERENCE}$`, "m"));
    // The product revision is not a variable this profile's stack reads, so
    // the installer reports it instead of writing a fact nobody reads.
    assert.match(result.stdout, new RegExp(SOURCE_SHA));
    assert.doesNotMatch(environment, /^OPL_RELEASE_SHA=/m);
  });
});

test("the environment file holds the facts this installation has, not every fact the template knows", async () => {
  await withBundle(async (bundle, bin) => {
    const result = await runInstaller(bundle, bin);
    assert.equal(result.status, 2, `${result.stdout}${result.stderr}`);

    const environment = await readFile(join(bundle, ".env"), "utf8");
    const lines = environment.split("\n").filter(Boolean);
    // The template declares 211 names for four deployment profiles. The
    // selected profile reads 39 of them, and the file the operator edits
    // holds those and nothing else.
    assert.ok(lines.length <= 50, `the environment file must hold this installation's facts only, got ${lines.length} lines`);

    // A fact only another profile reads is not in this installation's file.
    assert.doesNotMatch(environment, /^OPL_TENCENT_REGION=/m);
    assert.doesNotMatch(environment, /^OPL_LOCAL_V2_/m);
  });
});

test("the installer reports the facts only the operator can supply and writes the environment file", async () => {
  await withBundle(async (bundle, bin) => {
    const result = await runInstaller(bundle, bin);
    assert.equal(result.status, 2, `${result.stdout}${result.stderr}`);
    assert.match(result.stderr, /OPL_WORKSPACE_DOMAIN/);
    assert.match(result.stderr, /OPL_SUB2API_BASE_URL/);
    assert.match(result.stderr, /OPL_WORKSPACE_IMAGE/);
    assert.match(result.stderr, /OPL_FABRIC_LOCAL_DOCKER_PROVIDER_PROFILE_JSON/);

    const environment = await readFile(join(bundle, ".env"), "utf8");
    assert.match(environment, /^OPL_WORKSPACE_DOMAIN=unset-opl_workspace_domain$/m);
    assert.match(environment, /^OPL_PUBLIC_URL=http:\/\/localhost:8787$/m);
  });
});

test("the installer fills every secret the stack owns with a distinct random value", async () => {
  await withBundle(async (bundle, bin) => {
    const result = await runInstaller(bundle, bin);
    assert.equal(result.status, 2, `${result.stdout}${result.stderr}`);

    const environment = await readFile(join(bundle, ".env"), "utf8");
    const values = new Map(
      environment.split("\n").filter(Boolean).map((line) => [line.slice(0, line.indexOf("=")), line.slice(line.indexOf("=") + 1)])
    );
    const passwords = [...values.entries()]
      .filter(([name]) => name.endsWith("_DATABASE_PASSWORD") || name === "OPL_POSTGRES_ADMIN_PASSWORD")
      .map(([, value]) => value);
    assert.ok(passwords.length >= 4);
    for (const password of passwords) {
      assert.match(password, /^[0-9a-f]{64}$/);
    }
    assert.equal(new Set(passwords).size, passwords.length, "two database roles must not share a password");
    assert.match(values.get("OPL_CONTROL_PLANE_SERVICE_TOKEN") ?? "", /^[0-9a-f]{64}$/);
  });
});

test("the installer starts the stack with the profile's compose files", async () => {
  await withBundle(async (bundle, bin) => {
    // The operator fills the facts the installer reported, and runs it again.
    const first = await runInstaller(bundle, bin);
    assert.equal(first.status, 2, `${first.stdout}${first.stderr}`);

    const environment = await readFile(join(bundle, ".env"), "utf8");
    const filled = environment
      .replace(/^OPL_WORKSPACE_DOMAIN=.*$/m, "OPL_WORKSPACE_DOMAIN=ws.localhost")
      .replace(/^OPL_SUB2API_BASE_URL=.*$/m, "OPL_SUB2API_BASE_URL=https://sub2api.example.org")
      .replace(/^OPL_SUB2API_ADMIN_EMAIL=.*$/m, "OPL_SUB2API_ADMIN_EMAIL=operator@example.org")
      .replace(/^OPL_SUB2API_ADMIN_PASSWORD=.*$/m, `OPL_SUB2API_ADMIN_PASSWORD=${"c".repeat(64)}`)
      .replace(/^OPL_WORKSPACE_IMAGE=.*$/m, `OPL_WORKSPACE_IMAGE=ghcr.io/gaofeng21cn/opl-cloud-workspace@sha256:${"d".repeat(64)}`)
      .replace(/^OPL_FABRIC_LOCAL_DOCKER_PROVIDER_PROFILE_JSON=.*$/m, "OPL_FABRIC_LOCAL_DOCKER_PROVIDER_PROFILE_JSON={\"schemaVersion\":1}");
    await writeFile(join(bundle, ".env"), filled);

    const second = await runInstaller(bundle, bin);
    assert.equal(second.status, 0, `${second.stdout}${second.stderr}`);
    assert.match(second.calls, /compose --env-file .*\.env -f compose\.yaml -f compose\.deployment-platform-owned\.yaml -f compose\.fabric-local-docker\.yaml -f compose\.local-workspace\.yaml pull/);
    assert.match(second.calls, /compose --env-file .*\.env -f compose\.yaml -f compose\.deployment-platform-owned\.yaml -f compose\.fabric-local-docker\.yaml -f compose\.local-workspace\.yaml up -d/);

    // The environment the installer derived is one Compose accepts.
    const configuration = spawnSync("docker", [
      "compose",
      "--env-file", join(bundle, ".env"),
      "-f", join(bundle, "compose.yaml"),
      "-f", join(bundle, "compose.deployment-platform-owned.yaml"),
      "-f", join(bundle, "compose.fabric-local-docker.yaml"),
      "-f", join(bundle, "compose.local-workspace.yaml"),
      "config", "--format", "json"
    ], { encoding: "utf8" });
    assert.equal(configuration.status, 0, configuration.stderr);
    const services = JSON.parse(configuration.stdout).services;
    assert.equal(services["control-plane"].image, IMAGE_REFERENCE);
    assert.equal(services.fabric.environment.OPL_FABRIC_LOCAL_DOCKER_GATEWAY_CONTAINER, "opl-cloud-control-plane");
  });
});

test("the installer keeps the value the operator already decided", async () => {
  await withBundle(async (bundle, bin) => {
    await writeFile(join(bundle, ".env"), "OPL_WORKSPACE_DOMAIN=ws.decided.example\n");
    const result = await runInstaller(bundle, bin);
    assert.equal(result.status, 2, `${result.stdout}${result.stderr}`);
    const environment = await readFile(join(bundle, ".env"), "utf8");
    assert.match(environment, /^OPL_WORKSPACE_DOMAIN=ws\.decided\.example$/m);
  });
});

test("the installer keeps a decided fact another profile reads", async () => {
  await withBundle(async (bundle, bin) => {
    // A fact this profile does not read stays out of the file, unless the
    // operator decided it. Their value is theirs, and the file is theirs.
    await writeFile(join(bundle, ".env"), "OPL_TENCENT_REGION=ap-guangzhou\n");
    const result = await runInstaller(bundle, bin);
    assert.equal(result.status, 2, `${result.stdout}${result.stderr}`);
    const environment = await readFile(join(bundle, ".env"), "utf8");
    assert.match(environment, /^OPL_TENCENT_REGION=ap-guangzhou$/m);
  });
});

test("the installer runs the stack from the bundle whatever directory it was invoked from", async () => {
  await withBundle(async (bundle, bin) => {
    const first = await runInstaller(bundle, bin, [], tmpdir());
    assert.equal(first.status, 2, `${first.stdout}${first.stderr}`);
    const environment = await readFile(join(bundle, ".env"), "utf8");
    await writeFile(join(bundle, ".env"), environment
      .replace(/^OPL_WORKSPACE_DOMAIN=.*$/m, "OPL_WORKSPACE_DOMAIN=ws.localhost")
      .replace(/^OPL_SUB2API_BASE_URL=.*$/m, "OPL_SUB2API_BASE_URL=https://sub2api.example.org")
      .replace(/^OPL_SUB2API_ADMIN_EMAIL=.*$/m, "OPL_SUB2API_ADMIN_EMAIL=operator@example.org")
      .replace(/^OPL_SUB2API_ADMIN_PASSWORD=.*$/m, `OPL_SUB2API_ADMIN_PASSWORD=${"c".repeat(64)}`)
      .replace(/^OPL_WORKSPACE_IMAGE=.*$/m, `OPL_WORKSPACE_IMAGE=ghcr.io/gaofeng21cn/opl-cloud-workspace@sha256:${"d".repeat(64)}`)
      .replace(/^OPL_FABRIC_LOCAL_DOCKER_PROVIDER_PROFILE_JSON=.*$/m, "OPL_FABRIC_LOCAL_DOCKER_PROVIDER_PROFILE_JSON={\"schemaVersion\":1}"));

    const second = await runInstaller(bundle, bin, [], tmpdir());
    assert.equal(second.status, 0, `${second.stdout}${second.stderr}`);
    assert.match(second.calls, /-f compose\.yaml -f compose\.deployment-platform-owned\.yaml/);
  });
});
