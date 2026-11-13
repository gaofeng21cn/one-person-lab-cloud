import assert from "node:assert/strict";
import { execFile } from "node:child_process";
import { mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { resolve } from "node:path";
import test from "node:test";
import { promisify } from "node:util";
import { localV2AnonymousDockerOrigin } from "../../tools/local-workspace-qualification.ts";

const execute = promisify(execFile);
const root = resolve(import.meta.dirname, "../..");
const sourceSha = "a".repeat(40);
const cloudImage = `ghcr.io/example/cloud@sha256:${"b".repeat(64)}`;
const workspaceImage = `127.0.0.1:5000/qualification@sha256:${"c".repeat(64)}`;

test("an anonymous application entry binds the real Docker image, owner and published port", () => {
  const labels = { "opl.fabric.kind": "application_runtime", "opl.component.role": "main",
    "opl.fabric.provider": "local-docker", "opl.workspace.id": "workspace_test", "opl.account.id": "local-host",
    "opl.image.ref": workspaceImage, "opl.runtime.id": "runtime_test" };
  const container = JSON.parse(JSON.stringify({ Image: `sha256:${"f".repeat(64)}`, State: { Running: true },
    Config: { Labels: labels }, NetworkSettings: { Ports: { "3000/tcp": [{ HostIp: "127.0.0.1", HostPort: "32769" }] } } }));
  const input = { entryUrl: "http://127.0.0.1:32769/", workspaceId: "workspace_test", accountId: "local-host",
    imageRef: workspaceImage, imageId: container.Image,
    revision: { exposurePolicy: "anonymous", entryPort: "http", ports: [{ name: "http", port: 3000 }] }, containers: [container] };
  assert.equal(localV2AnonymousDockerOrigin(input), "http://127.0.0.1:32769");
  for (const invalid of [
    { ...input, workspaceId: "workspace_other" },
    { ...input, imageId: `sha256:${"e".repeat(64)}` },
    { ...input, entryUrl: "http://127.0.0.1:32770/" },
    { ...input, entryUrl: "http://credential@127.0.0.1:32769/" },
    { ...input, entryUrl: "http://example.test:32769/" },
    { ...input, revision: { ...input.revision, exposurePolicy: "application" } },
  ]) assert.throws(() => localV2AnonymousDockerOrigin(invalid), { code: "local_v2_application_runtime_binding_invalid" });
});

test("the CLI binds an owner-returned Tenant without rotating existing installation credentials", async () => {
  const directory = await mkdtemp(resolve(tmpdir(), "opl-native-clean-host-test-"));
  try {
    const args = [resolve(root, "tools/local-v2-clean-host.ts"), "--product-root", root,
      "--directory", directory, "--source-sha", sourceSha, "--cloud-image", cloudImage,
      "--workspace-image", workspaceImage, "--storage-root", resolve(directory, "storage"), "--project", "qualification-test"];
    await execute(process.execPath, args);
    const envFile = resolve(directory, "installation.env");
    const decode = (bytes) => Object.fromEntries(bytes.trim().split("\n").map((line) => {
      const offset = line.indexOf("=");
      return [line.slice(0, offset), line.slice(offset + 1)];
    }));
    const before = decode(await readFile(envFile, "utf8"));
    assert.equal(before.OPL_RELEASE_SHA, sourceSha, "the CLI writes the exact product revision");
    assert.equal(Object.hasOwn(before, "OPL_QUALIFICATION_SOURCE_SHA"), false);
    assert.equal(Object.hasOwn(before, "OPL_LOCAL_V2_RELEASE_SHA"), false);
    const receipt = {
      schemaVersion: 1, status: "NOT_READY", mode: "local-v2", errorCode: "local_v2_account_binding_required",
      source: { sha: sourceSha }, evidence: { readbacks: {
        installation: { cloudImage, workspaceImage }, tenant: { id: "tenant-owner-confirmed" },
      } },
    };
    const receiptFile = resolve(directory, "bootstrap.json");
    await writeFile(receiptFile, JSON.stringify(receipt));
    await execute(process.execPath, [...args, "--bind-from", receiptFile]);
    const after = decode(await readFile(envFile, "utf8"));
    assert.deepEqual(JSON.parse(after.OPL_FABRIC_LOCAL_DOCKER_PROVIDER_PROFILE_JSON).accountBindings,
      [{ tenantId: "tenant-owner-confirmed", accountId: "local-host" }]);
    delete before.OPL_FABRIC_LOCAL_DOCKER_PROVIDER_PROFILE_JSON;
    delete after.OPL_FABRIC_LOCAL_DOCKER_PROVIDER_PROFILE_JSON;
    assert.deepEqual(after, before);

    const bound = await readFile(envFile, "utf8");
    for (const invalid of [
      { ...receipt, source: { sha: "d".repeat(40) } },
      { ...receipt, errorCode: "local_v2_compose_start_failed" },
      { ...receipt, evidence: { readbacks: { installation: { cloudImage, workspaceImage: `other@sha256:${"e".repeat(64)}` }, tenant: { id: "tenant-other" } } } },
    ]) {
      await writeFile(receiptFile, JSON.stringify(invalid));
      await assert.rejects(execute(process.execPath, [...args, "--bind-from", receiptFile]));
      assert.equal(await readFile(envFile, "utf8"), bound);
    }
    await writeFile(receiptFile, "{invalid json");
    await assert.rejects(execute(process.execPath, [...args, "--bind-from", receiptFile]));
    assert.equal(await readFile(envFile, "utf8"), bound);
  } finally {
    await rm(directory, { recursive: true, force: true });
  }
});
