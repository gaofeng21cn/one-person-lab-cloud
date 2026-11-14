import assert from "node:assert/strict";
import { createServer } from "node:http";
import test from "node:test";
import { cloudIdentityLogin, createHTTP } from "../../tools/local-workspace-qualification.ts";

async function withIdentity(handler, work) {
  const server = createServer(handler);
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  const origin = `http://127.0.0.1:${server.address().port}`;
  try { await work(createHTTP(origin, { mode: "local-v2" }), origin); }
  finally { server.closeIdleConnections(); await new Promise<void>((resolve) => server.close(() => resolve())); }
}

test("identity startup retries only context GETs before a single login command", async () => {
  let reads = 0, writes = 0;
  const waits = [];
  await withIdentity((request, response) => {
    if (request.method === "GET") {
      reads += 1;
      if (reads <= 2) return response.writeHead(reads === 1 ? 502 : 503).end("starting");
      return response.writeHead(200, { "content-type": "application/json", "set-cookie": ["opl_login_context=challenge; Path=/", "opl_login_csrf=csrf; Path=/"] }).end(JSON.stringify({ csrfToken: "csrf" }));
    }
    writes += 1;
    response.writeHead(200, { "content-type": "application/json", "set-cookie": "opl_console_session=session; Path=/" }).end(JSON.stringify({ actorId: "41", csrfToken: "owner" }));
  }, async (http, origin) => {
    const result = await cloudIdentityLogin(http, origin, "fixture@example.test", "synthetic-password", "fixture-login", { wait: async (ms) => { waits.push(ms); } });
    assert.equal(result.session.actorId, "41");
  });
  assert.deepEqual([reads, writes, waits.length], [3, 1, 2]);
});

test("an owner rejection stops immediately and startup failure stays bounded", async () => {
  for (const [status, expectedReads] of [[401, 1], [502, 30]]) {
    let reads = 0, writes = 0;
    await withIdentity((request, response) => {
      if (request.method === "GET") reads += 1; else writes += 1;
      response.writeHead(status).end("unavailable");
    }, async (http, origin) => {
      await assert.rejects(cloudIdentityLogin(http, origin, "fixture@example.test", "synthetic-password", "fixture-login", { wait: async () => {} }), { status });
    });
    assert.deepEqual([reads, writes], [expectedReads, 0]);
  }
});

test("a failed login command is returned without another send", async () => {
  let reads = 0, writes = 0, waits = 0;
  await withIdentity((request, response) => {
    if (request.method === "GET") {
      reads += 1;
      return response.writeHead(200, { "content-type": "application/json", "set-cookie": ["opl_login_context=challenge; Path=/", "opl_login_csrf=csrf; Path=/"] }).end(JSON.stringify({ csrfToken: "csrf" }));
    }
    writes += 1;
    response.writeHead(503).end("command response unavailable");
  }, async (http, origin) => {
    await assert.rejects(cloudIdentityLogin(http, origin, "fixture@example.test", "synthetic-password", "fixture-login", { wait: async () => { waits += 1; } }), { status: 503 });
  });
  assert.deepEqual([reads, writes, waits], [1, 1, 0]);
});
