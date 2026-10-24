import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { once } from "node:events";
import { mkdtemp, rm, stat } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";

import type { WorkspaceLaunchRequest, WorkspaceLaunchResponse } from "../../apps/console-ui/src/api/dtos.ts";
import { apiFixture, createConsoleFixtureState } from "../../tools/console-browser-qa.ts";

async function runConsoleBrowserQa(options) {
  const harness = await import("../../tools/console-browser-qa.ts");
  return harness.runConsoleBrowserQa(options);
}

test("Console browser covers customer and operator journeys at desktop and mobile", { timeout: 120_000 }, async (t) => {
  const configuredScreenshotDir = process.env.OPL_CONSOLE_QA_SCREENSHOT_DIR;
  const screenshotDir = configuredScreenshotDir || await mkdtemp(join(tmpdir(), "opl-console-fixture-"));
  if (!configuredScreenshotDir) t.after(() => rm(screenshotDir, { recursive: true, force: true }));
  const result = await runConsoleBrowserQa({ network: "fake-only", screenshotDir });

  assert.equal(result.ok, true);
  assert.equal(result.network, "fake-only");
  assert.deepEqual(result.viewports, ["desktop", "mobile"]);
  assert.deepEqual(result.roles, ["customer", "operator"]);
  assert.deepEqual(result.sourceStates, ["available", "empty", "unavailable", "error"]);
  assert.deepEqual(result.repeatedWrites, { gatewayKey: 1, walletAdjustment: 1 });
  assert.deepEqual(result.highRiskWrites, {
    operatorProvision: 0,
    announcementCreate: 1,
    announcementPublish: 1,
    announcementWithdraw: 1
  });
  assert.equal(result.historicalLaunchReadback, true);
  assert.equal(result.announcementLifecycle, true);
  assert.equal(result.workspaceNavigation, true);
  assert.equal(result.workspacePagination, true);
  assert.equal(result.directDetailRefresh, true);
  assert.equal(result.billingViews, true);
  assert.equal(result.secretCleanup, true);
  assert.equal(result.externalRequests, 0);
  assert.deepEqual(result.consoleErrors, []);

  const screens = [
    "console-overview",
    "workspace-list",
    "api-overview",
    "api-usage",
    "billing",
    "announcements",
    "admin-overview",
    "admin-accounts",
    "admin-balance-operation",
    "admin-reconciliation",
    "admin-resources",
    "admin-system"
  ];
  await Promise.all(screens.flatMap((screen) => ["desktop", "mobile"].map((viewport) => (
    stat(join(screenshotDir, `fixture-${screen}-${viewport}.png`))
  ))));
});

test("Console fake refuses new Launch intent without writes and retains original-owner historical read, replay and resume", async () => {
  const state = createConsoleFixtureState();
  const session = { authenticated: true, role: "customer", accountId: "acct-1", consoleUserId: "user-customer" };
  const input: WorkspaceLaunchRequest = { name: "Historical Workspace", packageId: "basic", autoRenew: false, provisioningMode: "full" };
  const historical: WorkspaceLaunchResponse & WorkspaceLaunchRequest & { idempotencyKey: string; ownerUserId: string } = {
    ...input, idempotencyKey: "original-key", ownerUserId: "user-customer",
    operationId: "launch-historical", accountId: "acct-1", workspaceId: "ws-1", status: "pending", phase: "runtime",
    sizeGb: 10, priceVersion: "pilot-usd-2026-07-v1", currency: "USD", totalChargeUsdMicros: 52_580_000
  };
  async function request(path: string, method = "GET", key = "", body: Partial<WorkspaceLaunchRequest> = input) {
    let result: { status: number; body: unknown } | undefined;
    await apiFixture({
      request: () => ({ url: () => `http://127.0.0.1${path}`, method: () => method, headers: () => ({ "idempotency-key": key }), postDataJSON: () => body }),
      fulfill: async (response) => { result = { status: response.status, body: JSON.parse(response.body) }; },
      abort: async () => { assert.fail("Launch admission must not inject a lost new-purchase response"); }
    }, state, session);
    assert.ok(result);
    return result;
  }
  const fence = {
    status: 409,
    body: { error: "workspace_launch_new_purchase_retired", owner: "workspace", next: ["POST /api/v2/quotes", "POST /api/v2/workspaces"] }
  };
  const beforeNewIntent = structuredClone(state);
  assert.deepEqual(await request("/api/workspace-launches", "POST", "fresh-key"), fence);
  assert.deepEqual(state, beforeNewIntent, "refusal must not allocate a Workspace, Key, operation or any write state");

  state.launches = [historical];
  const beforeHistory = structuredClone(state);
  assert.deepEqual(await request("/api/workspace-launches"), { status: 200, body: [historical] });
  assert.deepEqual(await request("/api/workspace-launches/launch-historical"), { status: 200, body: historical });
  assert.deepEqual(await request("/api/workspace-launches", "POST", "original-key"), { status: 202, body: historical });
  assert.deepEqual(await request("/api/workspace-launches/launch-historical/resume", "POST", "resume-key"), { status: 202, body: historical });
  assert.deepEqual(await request("/api/workspace-launches", "POST", "original-key", { ...input, name: "Different Workspace" }), { status: 409, body: { error: "idempotency_conflict" } });
  assert.deepEqual(await request("/api/workspace-launches", "POST", "another-key"), fence);
  session.accountId = "acct-2";
  session.consoleUserId = "user-other";
  assert.deepEqual(await request("/api/workspace-launches"), { status: 200, body: [] });
  assert.deepEqual(await request("/api/workspace-launches/launch-historical"), { status: 404, body: { error: "workspace_launch_not_found" } });
  assert.deepEqual(await request("/api/workspace-launches/launch-historical/resume", "POST", "resume-key"), { status: 404, body: { error: "workspace_launch_not_found" } });
  assert.deepEqual(await request("/api/workspace-launches", "POST", "original-key"), fence);
  assert.deepEqual(state, beforeHistory, "historical fixture requests must retain the pre-seeded identities and resources");
});

test("Console browser refuses non-fixture network access before startup", async () => {
  let started = 0;
  await assert.rejects(() => runConsoleBrowserQa({
    network: "production",
    serverFactory: async () => { started += 1; },
    browserFactory: async () => { started += 1; }
  }), /console_browser_fake_only_required/);
  assert.equal(started, 0);
});

test("Console browser CLI refuses non-fixture network access", async () => {
  const child = spawn(process.execPath, ["tools/console-browser-qa.ts", "--network=production"], {
    cwd: process.cwd(),
    stdio: ["ignore", "pipe", "pipe"]
  });
  let stdout = "";
  let stderr = "";
  child.stdout.setEncoding("utf8");
  child.stderr.setEncoding("utf8");
  child.stdout.on("data", (chunk) => { stdout += chunk; });
  child.stderr.on("data", (chunk) => { stderr += chunk; });
  const [exitCode] = await once(child, "close");

  assert.equal(exitCode, 1);
  assert.equal(stdout, "");
  assert.match(stderr, /console_browser_fake_only_required/);
});
