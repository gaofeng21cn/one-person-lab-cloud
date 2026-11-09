import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { createHash } from "node:crypto";
import { constants } from "node:fs";
import { appendFile, copyFile, lstat, mkdir, mkdtemp, readFile, readdir, realpath, rm, symlink, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import test, { type TestContext } from "node:test";
import { parse as parseYaml } from "yaml";
import { candidateInstallationIdentity, decodeCandidateReceipt } from "../../tools/cloud-candidate-receipt.ts";
import { COMPUTE_READBACK_ERROR as ERROR, validateWorkspaceComputeLiveReadbackReceipt, type WorkspaceComputeLiveReadbackReceipt } from "../../tools/workspace-compute-live-readback.ts";
import { main, validateWorkspaceComputeReadbackHandoff } from "../../tools/workspace-compute-readback-handoff.ts";

const INVALID = "workspace_compute_readback_handoff_invalid";
const secret = "private-token-never-publish";
const sha = "a".repeat(40), instanceSha = "f".repeat(40);
const digest = (seed: string) => `sha256:${seed.repeat(64)}`;
const hash = (text: string | Buffer) => `sha256:${createHash("sha256").update(text).digest("hex")}`;
const workspaceId = "workspace_12345678", operationId = "operation_delete_1";
const instanceRepository = "gaofeng21cn/opl-instance-medopl";
const workflowPath = ".github/workflows/cloud-workspace-compute-readback.yml";
const workflowFile = resolve(".github/workflows/workspace-compute-readback-handoff.yml");
const toolFile = resolve("tools/workspace-compute-readback-handoff.ts");
const contract = JSON.parse(await readFile("packages/contracts/opl-cloud-candidate-receipt-contract.json", "utf8"));
const candidate = {
  schemaVersion: 2, kind: "opl_cloud_candidate",
  product: { repository: "gaofeng21cn/one-person-lab-cloud", sha, tree: "b".repeat(40) },
  cloudImage: {
    repository: "ghcr.io/gaofeng21cn/one-person-lab-cloud",
    indexRef: `ghcr.io/gaofeng21cn/one-person-lab-cloud@${digest("c")}`, indexDigest: digest("c"), revision: sha,
    platforms: [{ platform: "linux/amd64", digest: digest("d") }, { platform: "linux/arm64", digest: digest("e") }],
  },
  assets: contract.candidateReceiptV2.portableAssets.map((name: string) => ({ name, sha256: "1".repeat(64) })),
  provenance: { workflowRepository: "gaofeng21cn/one-person-lab-cloud", workflowSha: sha, workflowRunId: "11", workflowRunAttempt: "1" },
};
function receipt(observed = false): WorkspaceComputeLiveReadbackReceipt {
  const value: WorkspaceComputeLiveReadbackReceipt = {
    schemaVersion: 1, kind: "opl_cloud_workspace_compute_live_readback", scope: "compute_destroy_status",
    status: "blocked", errorCode: "compute_live_access_unavailable", candidate: candidateInstallationIdentity(candidate),
    provenance: { diagnosticSourceSha: sha, instanceRepository, instanceSha, runId: "22", runAttempt: "2", workflowPath },
    workspaceIdDigest: hash(`console-customer-workspace-readback:workspace:v1\0${workspaceId}`),
    workspaceOperationIdDigest: hash(`cloud-launch-readback:operation:v1\0${operationId}`),
    fabricOperationIdDigest: null, computeIdDigest: null,
    startedAt: "2026-11-09T00:00:00.000Z", completedAt: "2026-11-09T00:00:01.000Z",
    installation: null, httpStatus: null, failure: null, observation: null,
  };
  if (observed) Object.assign(value, {
    status: "observed", errorCode: "none", fabricOperationIdDigest: digest("1"), computeIdDigest: digest("2"),
    httpStatus: 200,
    installation: {
      podUidDigest: digest("3"), deploymentUidDigest: digest("4"), replicaSetUidDigest: digest("5"),
      imageIndexDigest: digest("c"), platformDigest: digest("d"), runtimeDigest: digest("d"),
    },
    observation: {
      machineType: "NativeCVM", machinePresent: true,
      allocationStatus: { value: null, digest: hash("deletion_pending") },
      tkeStatus: { value: null, digest: hash("Deleting") },
      cvmStatus: { value: "SHUTDOWN", digest: hash("SHUTDOWN") },
      isolatedSource: { value: "MANMADE", digest: hash("MANMADE") },
      observedAt: "2026-11-09T00:00:00.500Z", readbackIdDigest: digest("6"),
      providerRequestIdDigest: digest("7"), machineRequestIdDigest: digest("8"), cvmRequestIdDigest: digest("9"),
    },
  });
  return validateWorkspaceComputeLiveReadbackReceipt(value);
}
function run() {
  return {
    id: 22, run_attempt: 2, repository: { full_name: instanceRepository }, path: workflowPath,
    event: "workflow_dispatch", head_branch: "main", head_sha: instanceSha, status: "completed", conclusion: "success",
    actor: { login: "gaofeng21cn" }, triggering_actor: { login: "gaofeng21cn" },
  };
}
function input(observed = false) {
  return {
    candidate: structuredClone(candidate), receiptText: `${JSON.stringify(receipt(observed), null, 2)}\n`, run: run(),
    expectedWorkspaceId: workspaceId, expectedWorkspaceOperationId: operationId, expectedDiagnosticSourceSha: sha,
  };
}
function change<T>(source: T, path: string, value: unknown, remove = false): T {
  const result = structuredClone(source), keys = path.split(".");
  let parent = result as Record<string, unknown>;
  for (const key of keys.slice(0, -1)) parent = parent[key] as Record<string, unknown>;
  if (remove) delete parent[keys.at(-1)!]; else parent[keys.at(-1)!] = value;
  return result;
}
function invalid(fn: () => unknown) {
  assert.throws(fn, (error: unknown) => {
    assert.ok(error instanceof Error);
    assert.equal(error.message, INVALID);
    assert.equal(error.cause, undefined);
    return true;
  });
}
async function temporary(t: TestContext) {
  const directory = await mkdtemp(join(await realpath(tmpdir()), "compute-handoff-"));
  t.after(() => rm(directory, { recursive: true, force: true }));
  return directory;
}

test("binds the original bytes through the live decoder and preserves blocked and observed diagnostics", () => {
  for (const observed of [false, true]) {
    const source = input(observed), result = validateWorkspaceComputeReadbackHandoff(source);
    assert.equal(result.receiptDigest, hash(source.receiptText));
    assert.equal(result.receiptStatus, observed ? "observed" : "blocked");
    assert.equal(result.errorCode, observed ? "none" : "compute_live_access_unavailable");
    assert.deepEqual(result.identity, candidateInstallationIdentity(candidate));
    assert.deepEqual(result.provenance, receipt().provenance);
    assert.equal(result.evidenceRepository, "gaofeng21cn/one-person-lab-cloud");
    assert.equal(result.scope, "compute_destroy_status");
    const compact = JSON.stringify(JSON.parse(source.receiptText));
    assert.notEqual(result.receiptDigest, validateWorkspaceComputeReadbackHandoff({ ...source, receiptText: compact }).receiptDigest);
    assert.doesNotMatch(JSON.stringify(result), /delete(?:d|_complete)|refund|private-token|workspace_12345678|operation_delete_1/u);
  }
});

const bindingFailureCodes = [
  ERROR.WORKSPACE_ACCESS, ERROR.WORKSPACE_BINDING,
  ERROR.FABRIC_ACCESS, ERROR.FABRIC_BINDING, ERROR.BINDING,
] as const;

test("CLI roundtrips all blocked binding phases and historical binding receipts", async t => {
  for (const errorCode of bindingFailureCodes) {
    await t.test(errorCode, async t => {
      const files = await cliFiles(await temporary(t));
      const persisted = change(receipt(), "errorCode", errorCode);
      const original = `${JSON.stringify(persisted, null, 2)}\n`;
      await writeFile(files.receiptPath, original);
      const imported = await main(cliArgs(files));
      const retained = JSON.parse(await readFile(files.outputPath, "utf8"));
      assert.deepEqual(retained, imported);
      assert.equal(retained.receiptStatus, "blocked");
      assert.equal(retained.errorCode, errorCode);
      assert.equal(retained.receiptDigest, hash(original));
      const retainedReceipt = await readFile(files.receiptPath, "utf8");
      assert.equal(retainedReceipt, original);
      assert.deepEqual(validateWorkspaceComputeLiveReadbackReceipt(JSON.parse(retainedReceipt)), persisted);
      assert.deepEqual(validateWorkspaceComputeReadbackHandoff({ ...input(), receiptText: retainedReceipt }), retained);
    });
  }
});

test("binding phase and historical failures reject forged bound identities and observed state through handoff", async t => {
  for (const errorCode of bindingFailureCodes) {
    const blocked = change(receipt(), "errorCode", errorCode);
    const forgeries = [
      ["Fabric identity", change(blocked, "fabricOperationIdDigest", digest("1"))],
      ["compute identity", change(blocked, "computeIdDigest", digest("2"))],
      ["both bound identities", { ...blocked, fabricOperationIdDigest: digest("1"), computeIdDigest: digest("2") }],
      ["observed status", change(blocked, "status", "observed")],
      ["complete observed evidence", change(receipt(true), "errorCode", errorCode)],
    ] as const;
    for (const [name, persisted] of forgeries) {
      await t.test(`${errorCode}: ${name}`, () => {
        invalid(() => validateWorkspaceComputeReadbackHandoff({ ...input(), receiptText: JSON.stringify(persisted) }));
      });
    }
  }
});

test("persisted blocked and observed receipts reject unknown phase codes through handoff", () => {
  for (const observed of [false, true]) {
    const persisted = change(receipt(observed), "errorCode", "compute_live_unknown_phase");
    invalid(() => validateWorkspaceComputeReadbackHandoff({ ...input(observed), receiptText: JSON.stringify(persisted) }));
  }
});

test("persisted JSON receipt rejects non-string machineType through handoff", async t => {
  for (const machineType of [
    ["NativeCVM"], ["Native"], ["CXM"], [["NativeCVM"]], [], ["NativeCVM", "Native"],
    null, true, false, 0, 1, {}, { machineType: "NativeCVM" },
  ]) {
    await t.test(`machineType=${JSON.stringify(machineType)}`, () => {
      const source = input(true);
      const persisted = change(receipt(true), "observation.machineType", machineType);
      invalid(() => validateWorkspaceComputeReadbackHandoff({ ...source, receiptText: JSON.stringify(persisted) }));
    });
  }
});

test("persisted JSON receipt rejects contradictory NativeCVM absence evidence through handoff", async t => {
  const absent = receipt(true);
  Object.assign(absent.observation!, {
    machinePresent: false,
    allocationStatus: { value: "external_deleted", digest: hash("external_deleted") },
    tkeStatus: { value: "NOT_FOUND", digest: hash("NOT_FOUND") },
  });
  for (const machineType of ["NativeCVM", ["NativeCVM"]]) {
    for (const [name, cvmStatus, cvmRequestIdDigest] of [
      ["SHUTDOWN with CVM request", { value: "SHUTDOWN", digest: hash("SHUTDOWN") }, digest("9")],
      ["unknown without CVM request", { value: null, digest: hash("unknown") }, null],
      ["NOT_FOUND without CVM request", { value: "NOT_FOUND", digest: hash("NOT_FOUND") }, null],
    ] as const) {
      await t.test(`machineType=${JSON.stringify(machineType)}: ${name}`, () => {
        let persisted = change(absent, "observation.machineType", machineType);
        persisted = change(persisted, "observation.cvmStatus", cvmStatus);
        persisted = change(persisted, "observation.cvmRequestIdDigest", cvmRequestIdDigest);
        invalid(() => validateWorkspaceComputeReadbackHandoff({ ...input(true), receiptText: JSON.stringify(persisted) }));
      });
    }
  }
});

test("persisted JSON receipt preserves legal machineType strings and still-present diagnostics through handoff", async t => {
  for (const machineType of ["NativeCVM", "Native", "CXM"]) {
    const states = machineType === "NativeCVM"
      ? ["machine-present", "CVM-SHUTDOWN", "absent"]
      : ["machine-present", "absent"];
    for (const state of states) {
      await t.test(`${machineType}: ${state}`, () => {
        const persisted = change(receipt(true), "observation.machineType", machineType);
        Object.assign(persisted.observation!, {
          cvmStatus: { value: null, digest: null }, cvmRequestIdDigest: null,
          isolatedSource: { value: null, digest: null },
        });
        if (state !== "machine-present") {
          Object.assign(persisted.observation!, {
            machinePresent: false,
            allocationStatus: state === "absent"
              ? { value: "external_deleted", digest: hash("external_deleted") }
              : { value: null, digest: hash("active") },
            tkeStatus: { value: "NOT_FOUND", digest: hash("NOT_FOUND") },
          });
          if (machineType === "NativeCVM") {
            const cvmStatus = state === "absent" ? "NOT_FOUND" : "SHUTDOWN";
            persisted.observation!.cvmStatus = { value: cvmStatus, digest: hash(cvmStatus) };
            persisted.observation!.cvmRequestIdDigest = digest("9");
            if (state === "CVM-SHUTDOWN") persisted.observation!.isolatedSource = { value: "MANMADE", digest: hash("MANMADE") };
          }
        }
        const receiptText = JSON.stringify(persisted);
        const result = validateWorkspaceComputeReadbackHandoff({ ...input(true), receiptText });
        assert.equal(result.receiptStatus, "observed");
        assert.equal(result.errorCode, "none");
        assert.equal(result.receiptDigest, hash(receiptText));
      });
    }
  }
});

test("TKE-only receipt types reject CVM evidence through handoff", async t => {
  for (const machineType of ["Native", "CXM"]) {
    for (const field of ["cvmStatus", "cvmRequestIdDigest", "isolatedSource"] as const) {
      await t.test(`${machineType}: ${field}`, () => {
        const persisted = change(receipt(true), "observation.machineType", machineType);
        Object.assign(persisted.observation!, {
          cvmStatus: { value: null, digest: null }, cvmRequestIdDigest: null,
          isolatedSource: { value: null, digest: null },
        });
        const invalidReceipt = change(persisted, `observation.${field}`, receipt(true).observation![field]);
        invalid(() => validateWorkspaceComputeReadbackHandoff({ ...input(true), receiptText: JSON.stringify(invalidReceipt) }));
      });
    }
  }
});

test("receipt self-reported provenance cannot replace independently fetched run, attempt or actor authority", () => {
  const patches: [string, unknown][] = [
    ["id", 23], ["id", "22"], ["id", Number.MAX_SAFE_INTEGER + 1], ["run_attempt", 1], ["run_attempt", "2"],
    ["run_attempt", 0], ["run_attempt", 2.5], ["repository.full_name", "attacker/opl-instance-medopl"],
    ["path", ".github/workflows/deployment-verify.yml"], ["event", "push"], ["head_branch", "feature"],
    ["status", "in_progress"], ["conclusion", "failure"], ["head_sha", "e".repeat(40)],
    ["actor.login", secret], ["triggering_actor.login", secret], ["actor", null], ["triggering_actor", {}],
    ["triggering_actor.login", "RenDeHuang"],
  ];
  for (const [path, value] of patches) invalid(() => validateWorkspaceComputeReadbackHandoff({ ...input(), run: change(run(), path, value) }));
  for (const key of ["id", "run_attempt", "repository", "path", "event", "head_branch", "status", "conclusion", "head_sha"]) {
    invalid(() => validateWorkspaceComputeReadbackHandoff({ ...input(), run: change(run(), key, null, true) }));
  }
  for (const key of ["actor", "triggering_actor"]) {
    const value = change(run(), key, null, true);
    assert.equal(validateWorkspaceComputeReadbackHandoff({ ...input(), run: value }).receiptStatus, "blocked");
  }
  const admitted = { ...run(), actor: { login: "RenDeHuang" }, triggering_actor: { login: "RenDeHuang" } };
  assert.equal(validateWorkspaceComputeReadbackHandoff({ ...input(), run: admitted }).provenance.runAttempt, "2");
});

test("every Candidate installation identity and full Candidate manifest remain bound", () => {
  const patches: [string, unknown][] = [
    ["candidateReceiptDigest", digest("0")], ["productSha", "e".repeat(40)], ["productTree", "e".repeat(40)],
    ["cloudImageRef", secret], ["cloudImageDigest", digest("0")], ["cloudImageRevision", "e".repeat(40)],
    ["cloudRuntimeChildDigest", digest("e")],
  ];
  for (const [path, value] of patches) {
    const wrong = change(receipt(), `candidate.${path}`, value);
    invalid(() => validateWorkspaceComputeReadbackHandoff({ ...input(), receiptText: JSON.stringify(wrong) }));
  }
  // These remain valid Candidates but have a different full manifest digest.
  for (const path of ["assets.0.sha256", "cloudImage.platforms.1.digest", "provenance.workflowRunAttempt"]) {
    const value = path.endsWith("sha256") ? "2".repeat(64) : path.endsWith("digest") ? digest("0") : "3";
    invalid(() => validateWorkspaceComputeReadbackHandoff({ ...input(), candidate: change(candidate, path, value) }));
  }
  invalid(() => validateWorkspaceComputeReadbackHandoff({ ...input(), candidate: { ...candidate, unknown: secret } }));
});

test("workspace and operation domain separators, exact source and unknown receipt keys are enforced", () => {
  for (const [key, value] of [
    ["expectedWorkspaceId", "workspace_87654321"], ["expectedWorkspaceOperationId", "operation_other"],
    ["expectedDiagnosticSourceSha", "e".repeat(40)], ["expectedDiagnosticSourceSha", "a".repeat(39)],
    ["expectedDiagnosticSourceSha", "A".repeat(40)], ["expectedWorkspaceId", secret],
  ]) invalid(() => validateWorkspaceComputeReadbackHandoff({ ...input(), [key]: value }));
  for (const path of ["unknown", "candidate.unknown", "provenance.unknown", "observation.unknown", "installation.unknown", "observation.allocationStatus.unknown"]) {
    invalid(() => validateWorkspaceComputeReadbackHandoff({ ...input(true), receiptText: JSON.stringify(change(receipt(true), path, secret)) }));
  }
  for (const [path, value] of [
    ["workspaceIdDigest", hash(workspaceId)], ["workspaceOperationIdDigest", hash(operationId)],
    ["workspaceIdDigest", hash(`cloud-launch-readback:operation:v1\0${workspaceId}`)],
    ["provenance.runAttempt", "1"], ["provenance.runId", "23"], ["provenance.instanceSha", "e".repeat(40)],
    ["provenance.diagnosticSourceSha", "e".repeat(40)], ["status", "deleted"], ["errorCode", secret],
  ]) invalid(() => validateWorkspaceComputeReadbackHandoff({ ...input(), receiptText: JSON.stringify(change(receipt(), path, value)) }));
  invalid(() => validateWorkspaceComputeReadbackHandoff({ ...input(), unknown: secret } as ReturnType<typeof input>));
});

test("invalid original JSON, encoding and unknown API data never escape the safe boundary", () => {
  for (const receiptText of ["", secret, `{\"secret\":\"${secret}\",}`, "null", "[]", "{}", "x".repeat(1048577), "\ud800"]) {
    invalid(() => validateWorkspaceComputeReadbackHandoff({ ...input(), receiptText }));
  }
  const result = validateWorkspaceComputeReadbackHandoff({ ...input(), run: {
    ...run(), unknown: secret, repository: { ...run().repository, privateValue: secret },
    actor: { ...run().actor, token: secret },
  } });
  assert.ok(!JSON.stringify(result).includes(secret));
  for (const value of [null, [], {}, { ...receipt(), observation: {} }]) {
    invalid(() => validateWorkspaceComputeReadbackHandoff({ ...input(), receiptText: JSON.stringify(value) }));
  }
});

async function cliFiles(directory: string) {
  const source = input(), { receiptText, ...metadata } = source;
  const inputPath = join(directory, "metadata.json"), receiptPath = join(directory, "receipt.json"), outputPath = join(directory, "binding.json");
  await writeFile(inputPath, JSON.stringify(metadata));
  await writeFile(receiptPath, receiptText);
  return { inputPath, receiptPath, outputPath, receiptText, metadata };
}
const cliArgs = (f: Awaited<ReturnType<typeof cliFiles>>) => ["import", "--input", f.inputPath, "--receipt", f.receiptPath, "--output", f.outputPath];

test("CLI imports exact bytes into an exclusive mode-0600 binding without changing inputs", async t => {
  const directory = await temporary(t), files = await cliFiles(directory);
  await main(cliArgs(files));
  assert.equal((await lstat(files.outputPath)).mode & 0o777, 0o600);
  const binding = JSON.parse(await readFile(files.outputPath, "utf8"));
  assert.equal(binding.receiptDigest, hash(await readFile(files.receiptPath)));
  assert.equal(await readFile(files.receiptPath, "utf8"), files.receiptText);
  await assert.rejects(main(cliArgs(files)), { message: INVALID });
  assert.deepEqual(JSON.parse(await readFile(files.outputPath, "utf8")), binding);
  for (const outputPath of [files.inputPath, files.receiptPath]) await assert.rejects(main(cliArgs({ ...files, outputPath })), { message: INVALID });
  assert.deepEqual(JSON.parse(await readFile(files.inputPath, "utf8")), files.metadata);
});

test("CLI refuses symlinks including ancestor paths, oversized data, invalid UTF-8 and unsafe options", async t => {
  const directory = await temporary(t), files = await cliFiles(directory);
  for (const key of ["inputPath", "receiptPath", "outputPath"] as const) {
    const path = join(directory, `link-${key}`);
    await symlink(files[key === "outputPath" ? "inputPath" : key], path);
    await assert.rejects(main(cliArgs({ ...files, [key]: path })), { message: INVALID });
  }
  await mkdir(join(directory, "real"));
  await symlink(join(directory, "real"), join(directory, "linked"));
  await assert.rejects(main(cliArgs({ ...files, outputPath: join(directory, "linked", "binding.json") })), { message: INVALID });
  for (const key of ["inputPath", "receiptPath"] as const) {
    const path = join(directory, `oversized-${key}`);
    await writeFile(path, " ".repeat(1048577));
    await assert.rejects(main(cliArgs({ ...files, [key]: path })), { message: INVALID });
  }
  const bad = join(directory, "bad-utf8.json");
  await writeFile(bad, Buffer.from([0xff, 0xfe]));
  await assert.rejects(main(cliArgs({ ...files, receiptPath: bad })), { message: INVALID });
  await writeFile(bad, JSON.stringify({ ...files.metadata, unknown: secret }));
  await assert.rejects(main(cliArgs({ ...files, inputPath: bad })), { message: INVALID });
  for (const args of [[], ["validate"], [...cliArgs(files), "--other", secret], ["import", "--input", files.inputPath, "--input", files.inputPath, "--output", files.outputPath]]) {
    await assert.rejects(main(args), { message: INVALID });
  }
  const child = spawnSync(process.execPath, [toolFile, ...cliArgs({ ...files, inputPath: bad })], { encoding: "utf8" });
  assert.equal(child.status, 1);
  assert.equal(child.stdout, "");
  assert.equal(child.stderr.trim(), INVALID);
});

const workflow = parseYaml(await readFile(workflowFile, "utf8"));
const steps = workflow.jobs.admission.steps;
const apiStep = steps.find((step: { id?: string }) => step.id === "evidence");
const importStep = steps.find((step: { name: string }) => step.name === "Validate original receipt bytes and retain Cloud binding");
function script(step: { run: string }) {
  const source = step.run.split("<<'NODE'\n")[1].split("\nNODE")[0];
  return source.split("\n").filter(line => !line.startsWith("import ")).join("\n");
}
// Execute the actual workflow consumer with narrow injected Git/API ports. The
// decoder, CLI and filesystem remain real, and no network or dispatch occurs.
async function workflowAdmission(directory: string, responses: unknown[]) {
  const calls: { url: string; options: { method: string; headers: Record<string, string> } }[] = [];
  const errors: string[] = [], gitCalls: string[][] = [];
  const processMock = { env: {
    INSTANCE_RUN_ID: "22", WORKSPACE_ID: workspaceId, WORKSPACE_OPERATION_ID: operationId,
    CANDIDATE_RECEIPT_B64: Buffer.from(JSON.stringify(candidate)).toString("base64"), DIAGNOSTIC_SOURCE_SHA: sha,
    CLOUD_CONSUMER_SHA: sha, INSTANCE_TOKEN: secret, RUNNER_TEMP: directory, GITHUB_OUTPUT: join(directory, "outputs"),
  }, exitCode: 0 };
  const ports = {
    process: processMock, console: { error: (value: string) => errors.push(value) },
    execFileSync: (command: string, args: string[]) => {
      assert.equal(command, "git"); gitCalls.push(args);
      return args[0] === "rev-parse" ? sha : "";
    },
    mkdir, writeFile, appendFile, join, decodeCandidateReceipt,
    fetch: async (url: string, options: typeof calls[number]["options"]) => {
      calls.push({ url, options });
      assert.equal(options.method, "GET");
      assert.equal(options.headers.Authorization, `Bearer ${secret}`);
      if (!responses.length) throw new Error(secret);
      const value = responses.shift();
      return { ok: true, json: async () => value };
    },
  };
  const AsyncFunction = Object.getPrototypeOf(async function () {}).constructor;
  await new AsyncFunction(...Object.keys(ports), script(apiStep))(...Object.values(ports));
  return { calls, errors, gitCalls, processMock };
}
function artifact(id = 101) {
  return { id, name: "cloud-workspace-compute-readback-handoff-22-2", expired: false,
    workflow_run: { id: 22, head_sha: instanceSha, head_branch: "main" } };
}

test("workflow uses only admitted main code, read-only API/artifact ports and final Cloud evidence custody", async t => {
  assert.deepEqual(Object.keys(workflow.on), ["workflow_dispatch"]);
  assert.deepEqual(Object.keys(workflow.on.workflow_dispatch.inputs).sort(), ["candidate_receipt_b64", "diagnostic_source_sha", "instance_run_id", "workspace_id", "workspace_operation_id"].sort());
  assert.deepEqual(Object.keys(workflow.jobs), ["admission"]);
  const job = workflow.jobs.admission;
  assert.equal(job.environment, "cloud-release");
  assert.match(job.if, /github.ref == 'refs\/heads\/main'/u);
  assert.match(job.if, /github.actor == github.triggering_actor/u);
  assert.match(job.if, /github.actor == github.repository_owner/u);
  assert.match(job.if, /github.actor == 'RenDeHuang'/u);
  assert.deepEqual(job.permissions, { contents: "read", actions: "read" });
  for (const step of steps) {
    if (step.uses) assert.match(step.uses, /@[a-f0-9]{40}$/u);
    if (step.run) {
      assert.doesNotMatch(step.run, /\$\{\{|dispatches|workflow run|kubectl|ssh |POST|DELETE|production/iu);
    }
  }
  const checkout = steps.find((step: { uses?: string }) => step.uses?.startsWith("actions/checkout@"));
  assert.equal(checkout.with.ref, "${{ github.sha }}");
  assert.equal(checkout.with["fetch-depth"], 0);
  assert.equal(checkout.with["persist-credentials"], false);
  const download = steps.find((step: { uses?: string }) => step.uses?.startsWith("actions/download-artifact@"));
  assert.equal(download.with["artifact-ids"], "${{ steps.evidence.outputs.artifact_id }}");
  assert.equal(download.with.repository, instanceRepository);
  assert.equal(download.with["run-id"], "${{ env.INSTANCE_RUN_ID }}");
  assert.equal(download.with["github-token"], "${{ secrets.OPL_GITHUB_RELEASE_ADMIN_TOKEN }}");
  const upload = steps.find((step: { uses?: string }) => step.uses?.startsWith("actions/upload-artifact@"));
  assert.equal(upload.with["retention-days"], 90);
  assert.equal(upload.with.name, "workspace-compute-readback-${{ env.INSTANCE_RUN_ID }}-${{ github.run_id }}-${{ github.run_attempt }}");
  assert.equal(upload.with.path.trim(), "${{ runner.temp }}/compute-readback-evidence/receipt.json\n${{ runner.temp }}/compute-readback-evidence/binding.json");
  assert.equal(upload.if, undefined); // Invalid imports never reach publication.
  const directory = await temporary(t);
  const result = await workflowAdmission(directory, [run(), { total_count: 1, artifacts: [artifact()] }]);
  assert.deepEqual(result.errors, []);
  assert.equal(result.processMock.exitCode, 0);
  assert.equal(result.calls.length, 2);
  assert.equal(result.calls[0].url, `https://api.github.com/repos/${instanceRepository}/actions/runs/22`);
  assert.deepEqual(result.gitCalls, [["rev-parse", "HEAD"], ["merge-base", "--is-ancestor", sha, "refs/remotes/origin/main"], ["merge-base", "--is-ancestor", sha, "refs/remotes/origin/main"]]);
  assert.equal(await readFile(join(directory, "outputs"), "utf8"), "artifact_id=101\n");
  await mkdir(join(directory, "compute-readback-download"));
  const original = input().receiptText;
  await writeFile(join(directory, "compute-readback-download", "receipt.json"), original);
  const errors: string[] = [], processMock = { env: { RUNNER_TEMP: directory }, exitCode: 0 };
  const ports = { process: processMock, console: { error: (value: string) => errors.push(value) }, constants, copyFile, mkdir, readdir, join, main };
  const AsyncFunction = Object.getPrototypeOf(async function () {}).constructor;
  await new AsyncFunction(...Object.keys(ports), script(importStep))(...Object.values(ports));
  assert.deepEqual(errors, []);
  assert.equal(processMock.exitCode, 0);
  const evidence = join(directory, "compute-readback-evidence");
  assert.deepEqual((await readdir(evidence)).sort(), ["binding.json", "receipt.json"]);
  assert.equal(await readFile(join(evidence, "receipt.json"), "utf8"), original);
  const binding = JSON.parse(await readFile(join(evidence, "binding.json"), "utf8"));
  assert.equal(binding.receiptDigest, hash(original));
  assert.equal(binding.receiptStatus, "blocked");
});

test("workflow rejects duplicate, expired and wrong-run artifacts across all API pages with fixed errors", async t => {
  for (const artifacts of [
    [artifact(), artifact(102)], [{ ...artifact(), expired: true }],
    [change(artifact(), "workflow_run.id", 23)], [change(artifact(), "workflow_run.head_sha", "e".repeat(40))],
    [change(artifact(), "workflow_run.head_branch", "feature")], [{ ...artifact(), name: secret }],
  ]) {
    const result = await workflowAdmission(await temporary(t), [run(), { total_count: artifacts.length, artifacts }]);
    assert.deepEqual(result.errors, [INVALID]);
    assert.equal(result.processMock.exitCode, 1);
  }
  const firstPage = Array.from({ length: 100 }, (_, i) => ({ ...artifact(i + 1), name: `other-${i}` }));
  firstPage[0] = artifact(200);
  const duplicate = await workflowAdmission(await temporary(t), [run(), { total_count: 101, artifacts: firstPage }, { total_count: 101, artifacts: [artifact(201)] }]);
  assert.equal(duplicate.calls.length, 3);
  assert.deepEqual(duplicate.errors, [INVALID]);
  const valid = await workflowAdmission(await temporary(t), [run(), { total_count: 101, artifacts: firstPage }, { total_count: 101, artifacts: [{ ...artifact(201), name: "other-final" }] }]);
  assert.deepEqual(valid.errors, []);
  const wrongRun = await workflowAdmission(await temporary(t), [{ ...run(), id: 23 }]);
  assert.deepEqual(wrongRun.errors, [INVALID]);
});

test("workflow refuses extra files and symlinks instead of accepting arbitrary downloaded contents", async t => {
  for (const kind of ["extra", "directory", "symlink"]) {
    const directory = await temporary(t), source = join(directory, "compute-readback-download");
    await mkdir(source);
    if (kind === "directory") await mkdir(join(source, "receipt.json"));
    else if (kind === "symlink") {
      await writeFile(join(directory, "elsewhere.json"), input().receiptText);
      await symlink(join(directory, "elsewhere.json"), join(source, "receipt.json"));
    } else {
      await writeFile(join(source, "receipt.json"), input().receiptText);
      await writeFile(join(source, "extra.json"), secret);
    }
    const processMock = { env: { RUNNER_TEMP: directory }, exitCode: 0 }, errors: string[] = [];
    const ports = { process: processMock, console: { error: (value: string) => errors.push(value) }, constants, copyFile, mkdir, readdir, join, main };
    const AsyncFunction = Object.getPrototypeOf(async function () {}).constructor;
    await new AsyncFunction(...Object.keys(ports), script(importStep))(...Object.values(ports));
    assert.equal(processMock.exitCode, 1);
    assert.deepEqual(errors, [INVALID]);
    await assert.rejects(lstat(join(directory, "compute-readback-evidence")), { code: "ENOENT" });
  }
});
