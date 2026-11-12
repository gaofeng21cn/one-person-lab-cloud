import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";
import {
  admitAdminRuntime, confirmRuntimeAdmission, listAdminRuntimeVersions, parseRuntimeAdmission,
  type AdminRuntimeVersion, type RuntimeAdmissionDraft
} from "../../apps/console-ui/src/api/admin-runtime-api.ts";

const schema = JSON.parse(await readFile(new URL("../../docs/spec/target/contracts/publisher-contract.schema.json", import.meta.url), "utf8"));
const contract = schema.examples.find((entry: { kind: string }) => entry.kind === "runtime");
const draft: RuntimeAdmissionDraft = {
  name: "opl-app", versionLabel: "2.0.0", publisherNamespaceId: contract.publisherNamespaceId,
  admissionReceiptId: "receipt-new", publisherContractJson: JSON.stringify(contract)
};
const input = parseRuntimeAdmission(draft);
const row: AdminRuntimeVersion = {
  ...input, id: "runtime-new", artifactDigest: contract.image.digest, status: "approved", runtimeAbiVersion: contract.runtimeAbiVersion,
  publisherContractDigest: "sha256:" + "c".repeat(64), publisherContractObjectRef: "runtime-contract:runtime-new", createdAt: "2026-11-12T00:00:00Z"
};
const response = (body: unknown, status = 200) => new Response(JSON.stringify(body), { status, headers: { "content-type": "application/json" } });

test("Runtime admission preserves the canonical publisher contract and rejects invalid identity/digest JSON", () => {
  assert.deepEqual(input, { name: draft.name, versionLabel: draft.versionLabel, publisherNamespaceId: draft.publisherNamespaceId, admissionReceiptId: draft.admissionReceiptId, publisherContract: contract });
  for (const invalid of [
    { name: "bad name" }, { versionLabel: "bad/tag" }, { publisherNamespaceId: "another-publisher" },
    { admissionReceiptId: " " }, { publisherContractJson: "{" }, { publisherContractJson: "[]" },
    { publisherContractJson: JSON.stringify({ ...contract, image: { ...contract.image, digest: "latest" } }) }
  ]) assert.throws(() => parseRuntimeAdmission({ ...draft, ...invalid }));
});

test("Runtime API uses existing typed paths and sends only admission fields with CSRF, stable key and abort signal", async t => {
  const calls: { path: string; options?: RequestInit }[] = [];
  t.mock.method(globalThis, "fetch", async (path: string, options?: RequestInit) => {
    calls.push({ path, options });
    return response(row, 201);
  });
  const abort = new AbortController();
  assert.deepEqual(await admitAdminRuntime(input, "csrf-admin", "stable-admission-key", abort.signal), row);
  assert.equal(calls[0].path, "/api/v2/admin/catalog/runtime-versions");
  assert.equal(calls[0].options?.method, "POST");
  assert.deepEqual(JSON.parse(calls[0].options?.body as string), input);
  assert.equal((calls[0].options?.headers as Record<string, string>)["X-CSRF-Token"], "csrf-admin");
  assert.equal((calls[0].options?.headers as Record<string, string>)["Idempotency-Key"], "stable-admission-key");
  abort.abort();
  assert.equal(calls[0].options?.signal?.aborted, true);
});

test("Runtime catalog follows all owner cursors and confirms the returned immutable version", async t => {
  const calls: string[] = [];
  t.mock.method(globalThis, "fetch", async (path: string) => {
    calls.push(path);
    return response(calls.length === 1 ? { items: [], nextCursor: "owner/cursor+1" } : { items: [row] });
  });
  const rows = await listAdminRuntimeVersions(new AbortController().signal);
  assert.deepEqual(calls, ["/api/v2/catalog/runtime-versions?cursor=", "/api/v2/catalog/runtime-versions?cursor=owner%2Fcursor%2B1"]);
  assert.deepEqual(confirmRuntimeAdmission(rows, row), row);
  assert.throws(() => confirmRuntimeAdmission([], row));
  assert.throws(() => confirmRuntimeAdmission([{ ...row, publisherContractDigest: "sha256:" + "d".repeat(64) }], row));
  assert.throws(() => confirmRuntimeAdmission([{ ...row, admissionReceiptId: "another-receipt" }], row));
});

test("Runtime owner errors, malformed responses, duplicate identities and cursor cycles cannot become success", async t => {
  await t.test("owner rejection", async t => {
    t.mock.method(globalThis, "fetch", async () => response({ message: "publisher contract fails approved schema" }, 400));
    await assert.rejects(admitAdminRuntime(input, "csrf", "key", new AbortController().signal), { status: 400 });
  });
  await t.test("wrong admission identity", async t => {
    t.mock.method(globalThis, "fetch", async () => response({ ...row, versionLabel: "unexpected" }, 201));
    await assert.rejects(admitAdminRuntime(input, "csrf", "key", new AbortController().signal), /不一致/);
  });
  for (const [index, body] of [null, {}, { items: null }, { items: [{ ...row, artifactDigest: "latest" }] }, { items: [{ ...row, publisherContract: null }] }, { items: [row, row] }, { items: [], nextCursor: 1 }].entries()) {
    await t.test(`invalid page ${index + 1}`, async t => {
      t.mock.method(globalThis, "fetch", async () => response(body));
      await assert.rejects(listAdminRuntimeVersions(new AbortController().signal));
    });
  }
  await t.test("cycle", async t => {
    t.mock.method(globalThis, "fetch", async () => response({ items: [], nextCursor: "same" }));
    await assert.rejects(listAdminRuntimeVersions(new AbortController().signal), /游标重复/);
  });
});
