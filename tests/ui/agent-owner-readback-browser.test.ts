import assert from "node:assert/strict";
import test from "node:test";
import { launchBrowser } from "../../tools/launch-browser.ts";

import { CONSOLE_DEMO_CREDENTIALS, startConsoleDemoServer } from "../../tools/start-console-demo.ts";

test("Cloud 我的智能体 reads Package, versions, Capability and Build owners", { timeout: 30_000 }, async () => {
  const previousIdentity = process.env.VITE_CONSOLE_IDENTITY;
  process.env.VITE_CONSOLE_IDENTITY = "cloud";
  const server = await startConsoleDemoServer({ port: 0, log: false });
  const browser = await launchBrowser({ headless: true });
  try {
    const page = await browser.newPage();
    await page.route("**/api/v2/**", async (route) => {
      const url = new URL(route.request().url());
      const json = (body: unknown, status = 200) => route.fulfill({ status, contentType: "application/json", body: JSON.stringify(body) });
      if (url.pathname === "/api/v2/auth/session") return json({ actorId: "user-customer", tenantId: "tenant-fixture", displayName: "Customer", permissions: [], csrfToken: "csrf-fixture", expiresAt: "2099-01-01T00:00:00Z" });
      if (url.pathname === "/api/v2/packages") return json({ items: [{ id: "pkg-1", name: "Research Agent", description: "OMA generated research assistant", status: "active", latestReadyVersionId: "pv-1" }] });
      if (url.pathname === "/api/v2/packages/pkg-1") return json({ id: "pkg-1", name: "Research Agent", description: "OMA generated research assistant", status: "active", latestReadyVersionId: "pv-1" });
      if (url.pathname === "/api/v2/packages/pkg-1/versions") return json({ items: [{ id: "pv-1", packageId: "pkg-1", versionLabel: "1.2.0", status: "uploaded", createdAt: "2026-09-27T00:00:00Z" }] });
      if (url.pathname === "/api/v2/capability-versions") return json({ items: [
        { id: "capv-other", packageId: "pkg-other", packageVersionId: "pv-other", versionLabel: "0.9.0", status: "ready", modelRequirements: [] },
        { id: "capv-1", packageId: "pkg-1", packageVersionId: "pv-1", buildJobId: "build-1", versionLabel: "1.2.0", status: "ready", provenance: "oma", referenceCount: 0, createdAt: "2026-09-27T00:01:00Z", artifactDigest: "sha256:" + "b".repeat(64), modelRequirements: [] }
      ] });
      if (url.pathname === "/api/v2/builds/build-1") return json({ id: "build-1", packageVersionId: "pv-1", status: "succeeded", stage: "registering", resultCapabilityVersionId: "capv-1" });
      if (url.pathname === "/api/v2/catalog/runtime-versions") return json({ items: [{ id: "rt-1", name: "OPL App", versionLabel: "1.0.0", status: "approved" }] });
      if (url.pathname === "/api/v2/catalog/compute-plans") return json({ items: [{ id: "compute-1", name: "Basic", vcpus: 2, memoryMiB: 4096, availability: "available", billingMode: "prepaid_monthly", workspaceOffers: [{ storagePlanId: "storage-1", storageCapacityGiB: 50, monthlyUSDMicros: "20000000", pricePolicyVersionId: "pp-1" }] }] });
      if (url.pathname === "/api/v2/models") return json({ items: [] });
      if (url.pathname === "/api/v2/wallet/payer") return json({ balanceUSDMicros: "100000000", currency: "USD" });
      return json({ error: "unexpected_agent_readback_request", path: url.pathname }, 500);
    });
    const session = await page.request.post(`${server.origin}/api/auth/login`, { data: CONSOLE_DEMO_CREDENTIALS.customer });
    assert.equal(session.ok(), true);
    await page.goto(`${server.origin}/console/agents`, { waitUntil: "networkidle" });
    await page.locator(".agents-page > .panel-title").getByRole("heading", { name: "我的智能体", exact: true }).waitFor();
    await page.getByRole("button", { name: "查看详情", exact: true }).click();
    await page.getByRole("heading", { name: "Research Agent", exact: true }).waitFor();
    await page.getByText("版本已可部署", { exact: true }).waitFor();
    await page.getByText("oma · 构建 build-1", { exact: true }).waitFor();
    assert.equal(await page.getByRole("button", { name: "上传新版本", exact: true }).isVisible(), true);
    assert.equal(await page.getByRole("button", { name: "创建工作空间", exact: true }).isVisible(), true);
    assert.equal(await page.getByText("sha256:", { exact: false }).count(), 0);
    for (const jargon of ["Capability owner", "owner readback", "OCI 字节销毁", "Build build-1"]) {
      assert.equal(await page.getByText(jargon, { exact: false }).count(), 0, `unexpected jargon: ${jargon}`);
    }
    assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth));
    await page.getByRole("button", { name: "创建工作空间", exact: true }).click();
    await page.waitForURL(/\/console\/workspaces\/new\?capabilityVersionId=capv-1$/);
    await page.getByLabel("智能体版本", { exact: true }).waitFor({ state: "visible" });
    assert.equal(await page.getByLabel("智能体版本", { exact: true }).inputValue(), "capv-1");
  } finally {
    await browser.close();
    await server.close();
    if (previousIdentity === undefined) delete process.env.VITE_CONSOLE_IDENTITY;
    else process.env.VITE_CONSOLE_IDENTITY = previousIdentity;
  }
});
