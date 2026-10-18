import assert from "node:assert/strict";
import { chmod, mkdir, mkdtemp, readFile, rm, stat, symlink, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import test from "node:test";

import { candidateReceiptDigest } from "../../tools/cloud-candidate-receipt.ts";
import { loadProtectedJ0ReadyReceipt } from "../../tools/protected-business-qualification.ts";
import {
  FABRIC_J0_SANDBOX_DECLARATION_KIND,
  fabricSandboxOperationKey,
  main,
  parseFabricJ0ReadyArgs,
  readFabricCandidate,
  readFabricSandboxDeclaration,
  runFabricJ0Ready,
  writeFabricJ0ReadyReceipt,
} from "../../tools/workspace-qualification-j0-ready.ts";
import {
  QUALIFICATION_J0_READY_KIND,
  QUALIFICATION_J0_READY_MAX_BYTES,
} from "../../tools/workspace-qualification-contract.ts";
import {
  CLOUD_IMAGE_DIGEST,
  CLUSTER_ID,
  NAMESPACE,
  OWNER_ORIGIN,
  REGION,
  candidateReceipt,
} from "../helpers/protected-qualification-fixture.ts";

const REPOSITORY_ROOT = resolve(".");

function otherCandidateReceipt() {
  const sha = "c".repeat(40);
  const tree = "d".repeat(40);
  const indexDigest = "sha256:" + "5".repeat(64);
  return candidateReceipt({
    product: { repository: "gaofeng21cn/one-person-lab-cloud", sha, tree },
    cloudImage: {
      repository: "ghcr.io/gaofeng21cn/one-person-lab-cloud",
      indexRef: "ghcr.io/gaofeng21cn/one-person-lab-cloud@" + indexDigest,
      indexDigest,
      revision: sha,
      platforms: [
        { platform: "linux/amd64", digest: "sha256:" + "6".repeat(64) },
        { platform: "linux/arm64", digest: "sha256:" + "7".repeat(64) },
      ],
    },
  });
}

function sandboxDeclaration(candidate: Record<string, any>, overrides = {}) {
  return {
    schemaVersion: 1,
    authorityClass: "sandbox",
    dedicated: true,
    deployment: {
      deploymentMode: "managed_tke",
      fabricProvider: "tencent-tke",
      candidateReceiptDigest: candidateReceiptDigest(candidate),
      clusterId: CLUSTER_ID,
      region: REGION,
      namespace: NAMESPACE,
      ownerOrigin: OWNER_ORIGIN,
    },
    ...overrides,
  };
}

/*
 * The provider boundary module the producer loads. It is the injected transport
 * and nothing else: the qualification flow, the receipt derivation and the file
 * write are the production code under test. `mode` selects what the sandbox
 * provider answers.
 */
function boundaryModuleSource({
  open = "open",
  health = "ready",
  release = "released",
}: { open?: string; health?: string; release?: string } = {}) {
  return `const http = {
  json: async () => ({ status: 404, payload: {} }),
  request: async () => ({ response: { status: 404 }, payload: {} }),
};
export function createTencentTkeProviderBoundary({ deployment }) {
  const identity = { provider: "tencent-tke", clusterId: deployment.clusterId, namespace: deployment.namespace };
  return {
    ...identity,
    async establishReachability() {
      const state = ${JSON.stringify(open)};
      if (state === "throw") throw new Error("sandbox provider unavailable");
      if (state !== "open") return { ...identity, state, reason: "sandbox-host-missing" };
      return { ...identity, state: "open", transport: { origin: ${JSON.stringify(OWNER_ORIGIN)}, http, auth: { cookie: "fixture-session" } } };
    },
    async readProviderHealth() {
      const state = ${JSON.stringify(health)};
      return { ...identity, state, ready: state === "ready", serviceReady: state === "ready",
        failedChecks: state === "ready" ? [] : ["node-not-ready"] };
    },
    async releaseReachability() {
      return { ...identity, state: ${JSON.stringify(release)} };
    },
  };
}
`;
}

async function writeInputs(
  root: string,
  { candidate = candidateReceipt(), mode = {}, declaration = sandboxDeclaration(candidate) }: any = {},
) {
  const digest = candidateReceiptDigest(candidate);
  const paths = {
    candidatePath: join(root, "candidate.json"),
    candidateReceiptDigest: digest,
    sandboxPath: join(root, "sandbox.json"),
    boundaryModulePath: join(root, "provider-boundary.mjs"),
    outputPath: join(root, "j0-ready", "receipt.json"),
  };
  await mkdir(root, { recursive: true });
  await writeFile(paths.candidatePath, JSON.stringify(candidate), { mode: 0o600 });
  await writeFile(paths.sandboxPath, JSON.stringify(declaration), { mode: 0o600 });
  await writeFile(paths.boundaryModulePath, boundaryModuleSource(mode), { mode: 0o600 });
  return { candidate, digest, paths };
}

function recordingBoundary({
  open = "open",
  health = "ready",
  release = "released",
}: { open?: string; health?: string; release?: string } = {}) {
  const calls: string[] = [];
  const identity = { provider: "tencent-tke", clusterId: CLUSTER_ID, namespace: NAMESPACE };
  const boundary = {
    ...identity,
    async establishReachability({ operationKey }: any) {
      calls.push("establish");
      if (open !== "open") return { ...identity, state: open, reason: "sandbox-host-missing" };
      return {
        ...identity,
        state: "open",
        transport: {
          origin: OWNER_ORIGIN,
          http: { json: async () => ({ status: 404, payload: {} }), request: async () => ({ status: 404, payload: {} }) },
          auth: { cookie: "fixture-session" },
        },
        operationKey,
      };
    },
    async readProviderHealth() {
      calls.push("health");
      return { ...identity, state: health, ready: health === "ready", serviceReady: health === "ready",
        failedChecks: health === "ready" ? [] : ["node-not-ready"] };
    },
    async releaseReachability() {
      calls.push("release");
      return { ...identity, state: release };
    },
  };
  return { boundary, calls };
}

test("fabric derives a J0 READY receipt from a sandbox run and the protected loader admits it", async (t) => {
  const root = await mkdtemp(join(tmpdir(), "opl-fabric-j0-"));
  try {
    const { candidate, paths } = await writeInputs(root);
    const result = await runFabricJ0Ready(paths);
    assert.equal(result.status, "READY");
    assert.deepEqual(result.receipt, {
      schemaVersion: 1,
      kind: QUALIFICATION_J0_READY_KIND,
      status: "READY",
      source: {
        sha: candidate.product.sha,
        tree: candidate.product.tree,
        cloudImageDigest: CLOUD_IMAGE_DIGEST,
        clean: true,
      },
      authority: { mode: "live", class: "sandbox", dedicated: true, confirmed: true },
      provider: "tencent-tke",
      failed: 0,
      skipped: 0,
    });
    // The producer wrote it with the consumer's own file standard: 0600, created
    // (not overwritten) through one descriptor.
    assert.equal((await stat(paths.outputPath)).mode & 0o777, 0o600);
    assert.equal((await readFile(paths.outputPath, "utf8")), result.receiptText);
    // The real consumer loader admits exactly the bytes the producer wrote.
    const loaded = await loadProtectedJ0ReadyReceipt(paths.outputPath, {
      sourceSha: candidate.product.sha,
      sourceTree: candidate.product.tree,
      cloudImageDigest: CLOUD_IMAGE_DIGEST,
      repositoryRoot: REPOSITORY_ROOT,
    });
    assert.deepEqual(loaded, result.receipt);
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

test("the producer CLI runs the real qualification path through the boundary module", async (t) => {
  const root = await mkdtemp(join(tmpdir(), "opl-fabric-j0-cli-"));
  try {
    const { paths } = await writeInputs(root);
    const summary = await main([
      "run",
      "--candidate", paths.candidatePath,
      "--candidate-digest", paths.candidateReceiptDigest,
      "--sandbox", paths.sandboxPath,
      "--boundary", paths.boundaryModulePath,
      "--output", paths.outputPath,
    ], {});
    assert.equal(summary.status, "READY");
    assert.equal(summary.provider, "tencent-tke");
    assert.equal(summary.authorityClass, "sandbox");
    assert.equal(summary.failed, 0);
    assert.equal(summary.skipped, 0);
    assert.equal(summary.candidateReceiptDigest, paths.candidateReceiptDigest);
    assert.match(summary.receiptDigest, /^sha256:[0-9a-f]{64}$/);
    assert.equal(summary.artifact, "receipt.json");
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

test("a sandbox run that cannot open or health-check the scope is never emitted as READY", async (t) => {
  const root = await mkdtemp(join(tmpdir(), "opl-fabric-j0-blocked-"));
  try {
    const { paths } = await writeInputs(root);
    const absent = recordingBoundary({ open: "absent" });
    const blocked = await runFabricJ0Ready(paths, { boundary: absent.boundary as any });
    assert.equal(blocked.status, "BLOCKED");
    assert.equal(blocked.receipt.status, "BLOCKED");
    assert.equal(blocked.receipt.failed, 1, "the open is a failed check");
    assert.equal(blocked.receipt.skipped, 1, "there is no transport to inspect, so that check was not evaluated");
    assert.equal(blocked.receipt.authority.confirmed, false);
    assert.deepEqual(absent.calls, ["establish", "health", "release"], "the scope is always released after an attempt");
    await assert.rejects(
      loadProtectedJ0ReadyReceipt(paths.outputPath, {
        sourceSha: blocked.receipt.source.sha,
        sourceTree: blocked.receipt.source.tree,
        cloudImageDigest: CLOUD_IMAGE_DIGEST,
        repositoryRoot: REPOSITORY_ROOT,
      }),
      (error: any) => error.code === "j0_ready_receipt_invalid",
    );
    const degraded = recordingBoundary({ health: "degraded" });
    const unhealthy = await runFabricJ0Ready({ ...paths, outputPath: join(root, "j0-ready", "degraded.json") },
      { boundary: degraded.boundary as any });
    assert.equal(unhealthy.status, "BLOCKED");
    assert.equal(unhealthy.receipt.failed, 1);
    assert.equal(unhealthy.receipt.skipped, 0);
    assert.equal(unhealthy.receipt.authority.confirmed, true, "the provider did confirm the scope open and release");
    assert.deepEqual(degraded.calls, ["establish", "health", "release"]);
    await assert.rejects(
      loadProtectedJ0ReadyReceipt(join(root, "j0-ready", "degraded.json"), {
        sourceSha: unhealthy.receipt.source.sha,
        sourceTree: unhealthy.receipt.source.tree,
        cloudImageDigest: CLOUD_IMAGE_DIGEST,
        repositoryRoot: REPOSITORY_ROOT,
      }),
      (error: any) => error.code === "j0_ready_receipt_invalid",
    );
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

test("a receipt produced for one Candidate is refused when another Candidate asks for it", async (t) => {
  const root = await mkdtemp(join(tmpdir(), "opl-fabric-j0-candidate-"));
  try {
    const { paths } = await writeInputs(root);
    const result = await runFabricJ0Ready(paths);
    assert.equal(result.status, "READY");
    const other = otherCandidateReceipt();
    await assert.rejects(
      loadProtectedJ0ReadyReceipt(paths.outputPath, {
        sourceSha: other.product.sha,
        sourceTree: other.product.tree,
        cloudImageDigest: other.cloudImage.indexDigest,
        repositoryRoot: REPOSITORY_ROOT,
      }),
      (error: any) => error.code === "external_prerequisite_absent" && /for this Candidate/.test(error.message),
    );
    // The producer also refuses to run a sandbox declaration that names another
    // Candidate, so it cannot emit a receipt for the wrong bundle in the first
    // place.
    const wrongDeclaration = sandboxDeclaration(other);
    const wrongRoot = join(root, "wrong");
    await writeInputs(wrongRoot, { declaration: wrongDeclaration });
    await assert.rejects(
      runFabricJ0Ready({
        candidatePath: join(wrongRoot, "candidate.json"),
        candidateReceiptDigest: paths.candidateReceiptDigest,
        sandboxPath: join(wrongRoot, "sandbox.json"),
        boundaryModulePath: join(wrongRoot, "provider-boundary.mjs"),
        outputPath: join(wrongRoot, "j0-ready", "receipt.json"),
      }),
      (error: any) => error.code === "fabric_j0_ready_invalid" && /another Candidate/.test(error.message),
    );
    await assert.rejects(stat(join(wrongRoot, "j0-ready", "receipt.json")), /ENOENT/);
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

test("the receipt writer refuses to overwrite, follow a symlink, leave the repository boundary or exceed the byte cap", async (t) => {
  const root = await mkdtemp(join(tmpdir(), "opl-fabric-j0-writer-"));
  try {
    const { paths } = await writeInputs(root);
    const result = await runFabricJ0Ready(paths);
    const receipt = result.receipt;
    await assert.rejects(
      writeFabricJ0ReadyReceipt(paths.outputPath, receipt, { repositoryRoot: REPOSITORY_ROOT }),
      (error: any) => error.code === "fabric_j0_ready_invalid" && /already exists/.test(error.message),
    );
    const link = join(root, "linked.json");
    const target = join(root, "target.json");
    await writeFile(target, "{}\n", { mode: 0o600 });
    await symlink(target, link);
    await assert.rejects(
      writeFabricJ0ReadyReceipt(link, receipt, { repositoryRoot: REPOSITORY_ROOT }),
      (error: any) => error.code === "fabric_j0_ready_invalid",
    );
    await assert.rejects(
      writeFabricJ0ReadyReceipt(join(root, "inside.json"), receipt, { repositoryRoot: root }),
      (error: any) => error.code === "fabric_j0_ready_invalid" && /outside the repository/.test(error.message),
    );
    await assert.rejects(
      writeFabricJ0ReadyReceipt(join(root, "oversize.json"), { ...receipt, padding: "x".repeat(QUALIFICATION_J0_READY_MAX_BYTES) },
        { repositoryRoot: REPOSITORY_ROOT }),
      (error: any) => error.code === "fabric_j0_ready_invalid" && /byte cap/.test(error.message),
    );
    await assert.rejects(stat(join(root, "oversize.json")), /ENOENT/);
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

test("an absent Candidate, sandbox declaration or boundary module is an external prerequisite", async (t) => {
  const root = await mkdtemp(join(tmpdir(), "opl-fabric-j0-prerequisite-"));
  try {
    const { paths } = await writeInputs(root);
    for (const [name, patch] of [
      ["Candidate receipt", { candidatePath: join(root, "absent-candidate.json") }],
      ["fabric sandbox declaration", { sandboxPath: join(root, "absent-sandbox.json") }],
      ["protected provider boundary module", { boundaryModulePath: join(root, "absent-boundary.mjs") }],
    ] as const) {
      const outputPath = join(root, "j0-ready", `${name.replace(/\s+/gu, "-")}.json`);
      await assert.rejects(
        runFabricJ0Ready({ ...paths, ...patch, outputPath }),
        (error: any) => error.code === "external_prerequisite_absent" && error.message.includes(name),
        name,
      );
      await assert.rejects(stat(outputPath), /ENOENT/, `${name} wrote no receipt`);
    }
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

test("the run arguments and the derived operation identity are exact", async (t) => {
  const base = [
    "run",
    "--candidate", "/candidate.json",
    "--candidate-digest", "sha256:" + "a".repeat(64),
    "--sandbox", "/sandbox.json",
    "--boundary", "/boundary.mjs",
    "--output", "/receipt.json",
  ];
  assert.equal(parseFabricJ0ReadyArgs(base).boundaryModulePath, "/boundary.mjs");
  for (const broken of [
    ["run", ...base.slice(1, -1)],
    ["run", ...base, "--unknown", "x"],
    ["run", ...base, "--output", "/another.json"],
    ["run", ...base.slice(0, -1), ""],
    base.slice(1),
  ] as string[][]) {
    assert.throws(
      () => parseFabricJ0ReadyArgs(broken),
      (error: any) => error.code === "fabric_j0_ready_invalid",
    );
  }
  const key = fabricSandboxOperationKey("sha256:" + "a".repeat(64), { clusterId: CLUSTER_ID, namespace: NAMESPACE });
  assert.match(key, /^opl-j0-sandbox:[0-9a-f]{64}$/);
  assert.equal(key, fabricSandboxOperationKey("sha256:" + "a".repeat(64), { clusterId: CLUSTER_ID, namespace: NAMESPACE }));
  assert.notEqual(key, fabricSandboxOperationKey("sha256:" + "b".repeat(64), { clusterId: CLUSTER_ID, namespace: NAMESPACE }));
  const root = await mkdtemp(join(tmpdir(), "opl-fabric-j0-readers-"));
  try {
    const { candidate, paths } = await writeInputs(root);
    const read = await readFabricCandidate(paths.candidatePath, paths.candidateReceiptDigest);
    assert.equal(read.digest, paths.candidateReceiptDigest);
    const sandbox = await readFabricSandboxDeclaration(paths.sandboxPath, paths.candidateReceiptDigest);
    assert.equal(sandbox.deployment.clusterId, CLUSTER_ID);
    assert.equal(sandbox.candidateReceiptDigest, paths.candidateReceiptDigest);
    assert.equal(FABRIC_J0_SANDBOX_DECLARATION_KIND, "opl.workspace-qualification.j0-sandbox.v1");
    await assert.rejects(
      readFabricCandidate(paths.candidatePath, "sha256:" + "0".repeat(64)),
      (error: any) => error.code === "fabric_j0_ready_invalid" && /does not match the declared digest/.test(error.message),
    );
    for (const declaration of [
      { ...sandboxDeclaration(candidate), authorityClass: "preproduction" },
      { ...sandboxDeclaration(candidate), dedicated: false },
      { ...sandboxDeclaration(candidate), deployment: { ...sandboxDeclaration(candidate).deployment, deploymentMode: "customer_owned" } },
    ]) {
      await writeFile(paths.sandboxPath, JSON.stringify(declaration), { mode: 0o600 });
      await chmod(paths.sandboxPath, 0o600);
      await assert.rejects(
        readFabricSandboxDeclaration(paths.sandboxPath, paths.candidateReceiptDigest),
        (error: any) => error.code === "fabric_j0_ready_invalid",
      );
    }
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});
