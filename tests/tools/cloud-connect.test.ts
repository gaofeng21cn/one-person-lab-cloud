import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { mkdtemp, readFile, readdir, rm, stat, writeFile } from "node:fs/promises";
import { createServer, type IncomingMessage, type ServerResponse } from "node:http";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test, { type TestContext } from "node:test";
import { fileURLToPath } from "node:url";

const helper = fileURLToPath(new URL("../../scripts/cloud-connect.py", import.meta.url));
const secret = "opl-cloud-local-test-credential-never-display-0123456789";
const capabilities = [
  "cloud_list_compute_plans", "cloud_list_storage_plans", "cloud_list_workspaces",
  "cloud_get_quote", "cloud_get_workspace", "cloud_get_workspace_operation",
];
const schemaVersion = "opl-cloud-connect.v1";

type Envelope = {
  ok: boolean;
  disposition: string;
  result?: any;
  error?: { code: string };
};
type Invocation = { code: number | null; stdout: string; stderr: string; envelope: Envelope };
type Fixture = Awaited<ReturnType<typeof fixture>>;

async function run(store: string, args: string[], input?: unknown): Promise<Invocation> {
  const child = spawn("python3", [helper, ...args], {
    env: { ...process.env, OPL_CLOUD_CONNECT_HOME: store },
    stdio: ["pipe", "pipe", "pipe"],
  });
  let stdout = "", stderr = "";
  child.stdout.setEncoding("utf8").on("data", (part) => { stdout += part; });
  child.stderr.setEncoding("utf8").on("data", (part) => { stderr += part; });
  const timer = setTimeout(() => child.kill("SIGKILL"), 20_000);
  const code = await new Promise<number | null>((resolve, reject) => {
    child.once("error", reject);
    child.once("close", resolve);
    child.stdin.end(input === undefined ? "" : JSON.stringify(input));
  }).finally(() => clearTimeout(timer));
  assert.equal(stderr, "", "helper errors must stay in the sanitized JSON envelope");
  assert.ok(stdout.trim(), "helper must emit an envelope");
  assert.equal(stdout.trim().split("\n").length, 1);
  assert.ok(!stdout.includes(secret), "the credential must never appear in stdout");
  return { code, stdout, stderr, envelope: JSON.parse(stdout) };
}

async function fixture(t: TestContext) {
  const directory = await mkdtemp(join(tmpdir(), "opl-cloud-connect-test-"));
  const store = join(directory, "store");
  const credentialFile = join(directory, "download.json");
  t.after(() => rm(directory, { recursive: true, force: true }));
  const credential = {
    id: "agent-credential-1", label: "Local Agent", tenantId: "tenant-1",
    audience: "opl-cloud-mcp", issuedAt: new Date().toISOString(), expiresAt: new Date(Date.now() + 3_600_000).toISOString(),
    credential: secret,
  };
  const download = async (patch: Record<string, unknown> = {}) => {
    await writeFile(credentialFile, JSON.stringify({ ...credential, ...patch }), { mode: 0o600 });
  };
  await download();
  const connect = (request: Record<string, unknown>) => run(store, ["connect"], {
    schema_version: schemaVersion, ...request,
  });
  const importCredential = (endpoint = "https://cloud.example.test") => run(store, [
    "import", "--credential-file", credentialFile, "--endpoint", endpoint,
  ]);
  return { directory, store, credentialFile, credential, download, connect, importCredential };
}

function success(invocation: Invocation) {
  assert.equal(invocation.code, 0, invocation.stdout);
  assert.equal(invocation.envelope.ok, true);
  assert.equal(invocation.envelope.disposition, "completed");
  return invocation.envelope.result;
}

function failure(invocation: Invocation, disposition: string, code?: string) {
  assert.notEqual(invocation.code, 0);
  assert.equal(invocation.envelope.ok, false);
  assert.equal(invocation.envelope.disposition, disposition);
  assert.deepEqual(Object.keys(invocation.envelope.error ?? {}), ["code"]);
  assert.match(invocation.envelope.error!.code, /^[A-Z0-9_]+$/);
  if (code) assert.equal(invocation.envelope.error!.code, code);
}

function binding(metadata: any) {
  return { opaque: metadata.cloud_ref, revision: metadata.revision };
}

async function importReady(f: Fixture, endpoint?: string) {
  return success(await f.importCredential(endpoint));
}

type RequestRecord = { method: string | undefined; path: string | undefined; headers: IncomingMessage["headers"]; body: any };
async function cloud(t: TestContext, alter?: (request: RequestRecord, response: ServerResponse) => boolean) {
  const calls: RequestRecord[] = [];
  const structuredContent = {
    source: "/api/v2/quotes/quote-1", revision: "sha256:owner-revision",
    observedAt: "2026-11-09T00:00:00Z", data: { id: "quote-1", amount: "19000000" },
    reviewPath: "/api/v2/agent/quotes/quote-1/review?revision=sha256%3Aowner-revision",
  };
  const server = createServer(async (request, response) => {
    let body = "";
    for await (const chunk of request) body += chunk;
    const record = { method: request.method, path: request.url, headers: request.headers, body: JSON.parse(body) };
    calls.push(record);
    if (alter?.(record, response)) return;
    if (record.body.method === "notifications/initialized") {
      response.writeHead(202).end();
      return;
    }
    const result = record.body.method === "initialize" ? {
      protocolVersion: "2025-03-26", capabilities: { tools: {} },
      serverInfo: { name: "opl-cloud-test", version: "1.0.0" },
    } : { content: [{ type: "text", text: JSON.stringify(structuredContent) }], structuredContent };
    response.writeHead(200, { "content-type": "application/json" });
    response.end(JSON.stringify({ jsonrpc: "2.0", id: record.body.id, result }));
  });
  await new Promise<void>((resolve, reject) => {
    server.once("error", reject);
    server.listen(0, "127.0.0.1", resolve);
  });
  t.after(() => new Promise<void>((resolve, reject) => {
    server.closeAllConnections();
    server.close((error) => error ? reject(error) : resolve());
  }));
  const address = server.address();
  assert.ok(address && typeof address !== "string");
  return { endpoint: `http://127.0.0.1:${address.port}`, calls, structuredContent };
}

test("Cloud credential import stores private owner bytes and only exposes opaque metadata", async (t) => {
  const f = await fixture(t);
  const imported = await f.importCredential();
  const metadata = success(imported);
  assert.match(metadata.cloud_ref, /^cloud:[0-9a-f-]{36}$/);
  assert.match(metadata.revision, /^rev:[0-9a-f-]{36}$/);
  assert.equal(metadata.credential_id, f.credential.id);
  assert.equal(metadata.label, f.credential.label);
  assert.equal(metadata.tenant_id, f.credential.tenantId);
  assert.equal(metadata.audience, "opl-cloud-mcp");
  assert.equal(metadata.status, "ready");
  assert.equal(metadata.status_code, "READY");
  assert.deepEqual(new Set(metadata.capabilities), new Set(capabilities));
  assert.ok(!imported.stdout.includes(f.credentialFile));
  assert.ok(!imported.stdout.includes(f.store));
  assert.equal((await stat(f.store)).mode & 0o777, 0o700);
  const files = (await readdir(f.store)).filter((name) => name.endsWith(".json"));
  assert.equal(files.length, 1);
  assert.equal((await stat(join(f.store, files[0]))).mode & 0o777, 0o600);
  assert.ok((await readFile(join(f.store, files[0]), "utf8")).includes(secret));
  assert.deepEqual(success(await f.connect({ operation: "list" })), { connections: [metadata] });
  assert.deepEqual(success(await f.connect({ operation: "inspect", credential_handle: binding(metadata) })), { connection: metadata });
});

test("reimport keeps one Cloud binding but makes its previous revision unusable", async (t) => {
  const f = await fixture(t);
  const first = await importReady(f);
  await f.download({ label: "Renewed local Agent" });
  const replacement = await importReady(f);
  assert.equal(replacement.cloud_ref, first.cloud_ref);
  assert.notEqual(replacement.revision, first.revision);
  assert.equal(replacement.label, "Renewed local Agent");
  failure(await f.connect({ operation: "inspect", credential_handle: binding(first) }), "not_dispatched", "STALE_CREDENTIAL_HANDLE");
  failure(await f.connect({ operation: "invoke", credential_handle: binding(first), capability: "cloud_get_quote", input: { id: "quote-1" } }), "not_dispatched", "STALE_CREDENTIAL_HANDLE");
  assert.deepEqual(success(await f.connect({ operation: "list" })).connections, [replacement]);
});

test("import rejects malformed credentials and non-origin or insecure endpoints without secret output", async (t) => {
  const f = await fixture(t);
  for (const patch of [
    { audience: "other-service" }, { credential: "" }, { expiresAt: "not-a-date" },
    { id: "" }, { credential: "secret\r\nInjected: yes" },
  ]) {
    await f.download(patch);
    failure(await f.importCredential(), "not_dispatched");
  }
  await f.download();
  for (const endpoint of [
    "http://cloud.example.test", "https://user:password@cloud.example.test",
    "https://cloud.example.test/api/v2/mcp", "https://cloud.example.test?tenant=1",
    "https://cloud.example.test#fragment", "file:///tmp/cloud.json",
  ]) failure(await f.importCredential(endpoint), "not_dispatched");
});

test("Cloud invocation uses the fixed authenticated MCP sequence and returns original owner content", async (t) => {
  const f = await fixture(t), remote = await cloud(t);
  const metadata = await importReady(f, remote.endpoint);
  const input = { id: "quote-1", expectedRevision: "sha256:owner-revision" };
  const invocation = await f.connect({ operation: "invoke", credential_handle: binding(metadata), capability: "cloud_get_quote", input });
  assert.deepEqual(success(invocation), remote.structuredContent);
  assert.deepEqual(remote.calls.map((call) => call.body.method), ["initialize", "notifications/initialized", "tools/call"]);
  for (const call of remote.calls) {
    assert.equal(call.method, "POST");
    assert.equal(call.path, "/api/v2/mcp");
    assert.equal(call.headers.authorization, `Bearer ${secret}`);
    assert.equal(call.headers.origin, undefined);
    assert.equal(call.headers.cookie, undefined);
  }
  assert.equal(remote.calls[0].body.params.protocolVersion, "2025-03-26");
  assert.deepEqual(remote.calls[2].body.params, { name: "cloud_get_quote", arguments: input });
});

test("untrusted connection and tool inputs cannot select a URL, actor, path, header or extra capability", async (t) => {
  const f = await fixture(t), remote = await cloud(t);
  const metadata = await importReady(f, remote.endpoint);
  const request = { operation: "invoke", credential_handle: binding(metadata), capability: "cloud_get_quote", input: { id: "quote-1" } };
  for (const patch of [
    { url: "https://other.example.test" }, { actor: "admin" }, { path: "/api/v2/purchase" },
    { headers: { authorization: "Bearer foreign" } }, { capability: "cloud_purchase" },
    { credential_handle: { ...binding(metadata), endpoint: "https://other.example.test" } },
    { input: { id: "quote-1", url: remote.endpoint } },
    { input: { id: "quote-1", actor: "admin" } },
    { input: { id: "quote-1", headers: {} } },
    { input: { id: "quote-1", credential: secret } },
    { input: { id: "" } }, { input: { id: "x".repeat(257) } },
    { input: { id: "quote-1", expectedRevision: "x".repeat(257) } },
    { capability: "cloud_list_workspaces", input: { limit: true } },
    { capability: "cloud_list_workspaces", input: { limit: 101 } },
    { capability: "cloud_list_workspaces", input: { cursor: "x".repeat(4097) } },
  ]) failure(await f.connect({ ...request, ...patch }), "not_dispatched");
  assert.equal(remote.calls.length, 0);
});

test("expired local credential is visible as unavailable and never dispatched", async (t) => {
  const f = await fixture(t), remote = await cloud(t);
  await f.download({ expiresAt: new Date(Date.now() + 1_500).toISOString() });
  const metadata = await importReady(f, remote.endpoint);
  await new Promise((resolve) => setTimeout(resolve, 1_600));
  const readback = success(await f.connect({ operation: "inspect", credential_handle: binding(metadata) })).connection;
  assert.equal(readback.status, "attention_needed");
  assert.equal(readback.status_code, "CREDENTIAL_EXPIRED");
  assert.deepEqual(readback.capabilities, []);
  failure(await f.connect({ operation: "invoke", credential_handle: binding(metadata), capability: "cloud_get_quote", input: { id: "quote-1" } }), "not_dispatched", "CREDENTIAL_EXPIRED");
  assert.equal(remote.calls.length, 0);
});

test("Cloud HTTP authorization and redirect errors expose only safe codes and are never retried", async (t) => {
  for (const status of [401, 302]) {
    const f = await fixture(t);
    const remote = await cloud(t, (_request, response) => {
      response.writeHead(status, { "content-type": "text/plain", location: "http://127.0.0.1:1/stolen" });
      response.end(`${secret} private-remote-error /sensitive/server/path`);
      return true;
    });
    const metadata = await importReady(f, remote.endpoint);
    const invocation = await f.connect({ operation: "invoke", credential_handle: binding(metadata), capability: "cloud_get_quote", input: { id: "quote-1" } });
    failure(invocation, "provider_reported", status === 401 ? "REMOTE_UNAUTHORIZED" : "REMOTE_REDIRECT_REJECTED");
    assert.doesNotMatch(invocation.stdout, /private-remote-error|sensitive|stolen/);
    assert.equal(remote.calls.length, 1);
  }
});

test("a lost tools/call response stays unknown and is not replayed", async (t) => {
  const f = await fixture(t);
  const remote = await cloud(t, (request, response) => {
    if (request.body.method !== "tools/call") return false;
    response.socket!.destroy();
    return true;
  });
  const metadata = await importReady(f, remote.endpoint);
  const invocation = await f.connect({ operation: "invoke", credential_handle: binding(metadata), capability: "cloud_get_workspace_operation", input: { id: "operation-1" } });
  failure(invocation, "unknown_after_dispatch", "REMOTE_RESPONSE_UNKNOWN");
  assert.deepEqual(remote.calls.map((call) => call.body.method), ["initialize", "notifications/initialized", "tools/call"]);
});

test("MCP tool failures and malformed response bodies cannot leak raw provider text", async (t) => {
  for (const malformed of [false, true]) {
    const f = await fixture(t);
    const remote = await cloud(t, (request, response) => {
      if (request.body.method !== "tools/call") return false;
      response.writeHead(200, { "content-type": "application/json" });
      response.end(malformed ? `{broken ${secret}` : JSON.stringify({
        jsonrpc: "2.0", id: request.body.id,
        result: { isError: true, content: [{ type: "text", text: "private-owner-error /sensitive/backend/path" }] },
      }));
      return true;
    });
    const metadata = await importReady(f, remote.endpoint);
    const invocation = await f.connect({ operation: "invoke", credential_handle: binding(metadata), capability: "cloud_get_quote", input: { id: "quote-1" } });
    failure(invocation, malformed ? "unknown_after_dispatch" : "provider_reported");
    assert.doesNotMatch(invocation.stdout, /broken|private-owner-error/);
    assert.equal(remote.calls.length, 3);
  }
});

test("oversized MCP response is bounded and not accepted or retried", async (t) => {
  const f = await fixture(t);
  const remote = await cloud(t, (request, response) => {
    if (request.body.method !== "tools/call") return false;
    response.writeHead(200, { "content-type": "application/json" });
    response.end(JSON.stringify({ jsonrpc: "2.0", id: request.body.id, result: {
      structuredContent: { data: "x".repeat(2 * 1024 * 1024), credential: secret },
    } }));
    return true;
  });
  const metadata = await importReady(f, remote.endpoint);
  const invocation = await f.connect({ operation: "invoke", credential_handle: binding(metadata), capability: "cloud_get_quote", input: { id: "quote-1" } });
  failure(invocation, "unknown_after_dispatch");
  assert.equal(remote.calls.length, 3);
  assert.ok(invocation.stdout.length < 1_000);
});

test("JSON escaping cannot reflect the local credential into structured owner output", async (t) => {
  const f = await fixture(t);
  const remote = await cloud(t, (request, response) => {
    if (request.body.method !== "tools/call") return false;
    response.writeHead(200, { "content-type": "application/json" });
    const body = JSON.stringify({ jsonrpc: "2.0", id: request.body.id, result: {
      structuredContent: { source: "/api/v2/quotes/quote-1", data: { reflected: secret } },
    } });
    response.end(body.replace(secret, "\\u006f" + secret.slice(1)));
    return true;
  });
  const metadata = await importReady(f, remote.endpoint);
  const invocation = await f.connect({ operation: "invoke", credential_handle: binding(metadata), capability: "cloud_get_quote", input: { id: "quote-1" } });
  failure(invocation, "unknown_after_dispatch", "REMOTE_RESPONSE_INVALID");
  assert.equal(remote.calls.length, 3);
});

test("a continuously active slow response cannot extend the MCP request deadline", async (t) => {
  const f = await fixture(t);
  let chunks = 0;
  const remote = await cloud(t, (request, response) => {
    if (request.body.method !== "tools/call") return false;
    response.writeHead(200, { "content-type": "application/json" });
    response.write('{"jsonrpc":"2.0","result":{"structuredContent":{"data":"');
    const drip = setInterval(() => {
      response.write("x");
      chunks++;
    }, 100);
    response.once("close", () => clearInterval(drip));
    t.after(() => clearInterval(drip));
    return true;
  });
  const metadata = await importReady(f, remote.endpoint);
  const startedAt = performance.now();
  const invocation = await f.connect({ operation: "invoke", credential_handle: binding(metadata), capability: "cloud_get_quote", input: { id: "quote-1" } });
  const elapsedMs = performance.now() - startedAt;
  failure(invocation, "unknown_after_dispatch", "REMOTE_RESPONSE_UNKNOWN");
  assert.ok(chunks >= 10, "the socket remained active before the total deadline");
  assert.ok(elapsedMs < 6_500, `active response exceeded the total deadline: ${elapsedMs} ms`);
  assert.deepEqual(remote.calls.map((call) => call.body.method), ["initialize", "notifications/initialized", "tools/call"]);
});
