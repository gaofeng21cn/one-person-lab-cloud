import assert from "node:assert/strict";
import test from "node:test";
import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { createServer } from "vite";
import react from "@vitejs/plugin-react";
import type { Browser, Page } from "playwright";

import { launchBrowser } from "../../tools/launch-browser.ts";

function subscription(overrides: Record<string, unknown> = {}) {
  return {
    id: "sub-workspace-one", workspaceId: "workspace-one",
    currentPeriodStart: "2026-11-01T00:00:00Z", currentPeriodEnd: "2026-12-01T00:00:00Z",
    periodMonths: 1, status: "active", createdAt: "2026-11-01T00:00:00Z", provenance: "quoted",
    acceptedQuoteId: "quote-original", refundPolicyVersionId: "refund-original",
    retentionPolicyVersionId: "retention-original", refundTerms: "原退款条款", retentionTerms: "原保留条款",
    renewalMode: "manual", renewalSettingsVersion: "1", version: "1",
    currentPricePolicyVersionId: "accepted-policy-original", currentMonthlyUSDMicros: "52580001",
    ...overrides
  };
}

function renewalQuote() {
  return {
    id: "quote-renew-1", purpose: "renew", workspaceId: "workspace-one",
    computePlanId: "compute-original", storagePlanId: "storage-original", modelSelections: [], periodMonths: 1,
    periodStart: "2026-11-26T00:00:00Z", periodEnd: "2026-12-26T00:00:00Z",
    pricePolicyVersionId: "policy-today", refundPolicyVersionId: "refund-today", retentionPolicyVersionId: "retention-today",
    refundTerms: "退款条款", retentionTerms: "保留条款", expectedInterruption: "none_before_period_update",
    lineItems: [{ kind: "product", description: "月度套餐", quantity: 1, amountUSDMicros: "52580001" }],
    totalUSDMicros: "52580001", status: "offered", expiresAt: "2099-01-01T00:00:00Z",
    createdAt: "2026-11-26T00:00:00Z", runtimeReadbackRequirement: "required"
  };
}

function acceptedOperation() {
  return {
    operationId: "op-renew-1", owner: "workspace", kind: "renew_workspace", resourceId: "workspace-one",
    status: "accepted", stage: "quote_binding", requestId: "req-renew-1",
    createdAt: "2026-11-26T00:00:00Z", updatedAt: "2026-11-26T00:00:00Z", pollAfterSeconds: 1
  };
}

async function startFixture(t: { after: (fn: () => Promise<unknown>) => void }) {
  const cacheDir = await mkdtemp(join(tmpdir(), "opl-renewal-vite-"));
  const server = await createServer({
    configFile: false,
    cacheDir,
    define: { "import.meta.env.VITE_CONSOLE_IDENTITY": JSON.stringify("cloud") },
    plugins: [react(), {
      name: "native-renewal-consumer",
      configureServer(vite) {
        vite.middlewares.use(async (request, response, next) => {
          if (request.url !== "/__renewal") return next();
          response.setHeader("Content-Type", "text/html");
          response.end(await vite.transformIndexHtml(request.url, `<!doctype html><html lang="zh"><body><div id="root"></div><script type="module">
            import "/apps/console-ui/src/components/ui/apps-sdk.css";
            import "/apps/console-ui/src/components/ui/tokens.css";
            import "/apps/console-ui/src/components/ui/components.css";
            import "/apps/console-ui/src/styles.css";
            import React from "react";
            import { createRoot } from "react-dom/client";
            import { flushSync } from "react-dom";
            import { WorkspaceSubscriptionPanel } from "/apps/console-ui/src/components/workspaces/WorkspaceSubscriptionPanel.tsx";
            import { ConfirmDialogHost } from "/apps/console-ui/src/components/ui/ConfirmDialog.tsx";
            const root = createRoot(document.getElementById("root"));
            window.mountRenewal = () => {
              const session = { user: { id: "user-1", accountId: "acct-1", role: "owner", email: "owner@example.test", status: "active" }, isOperator: false, csrfToken: "csrf-1" };
              const workspace = { id: "workspace-one", computePlanId: "compute-original", storagePlanId: "storage-original" };
              flushSync(() => root.render(React.createElement(React.Fragment, null,
                React.createElement(WorkspaceSubscriptionPanel, { session, workspace }),
                React.createElement(ConfirmDialogHost))));
            };
            window.mountRenewal();
          </script></body></html>`));
        });
      }
    }], server: { host: "127.0.0.1", port: 0, hmr: false, watch: null }
  });
  await server.listen();
  let browser: Browser | undefined;
  t.after(async () => { await browser?.close(); await server.close(); await rm(cacheDir, { recursive: true, force: true }); });
  browser = await launchBrowser({ headless: true });
  const page = await browser.newPage();
  page.setDefaultTimeout(10_000);
  const errors: string[] = [];
  page.on("pageerror", (error) => errors.push(error.message));
  return { page, origin: server.resolvedUrls!.local[0], errors };
}

function panel(page: Page) { return page.getByRole("region", { name: "工作空间订阅", exact: true }); }

test("a manual renewal prices the owner's own offer, charges it once and reports the owner's own outcome", { timeout: 60_000 }, async (t) => {
  const { page, origin, errors } = await startFixture(t);
  let periodEnd = "2026-12-01T00:00:00Z";
  let reads = 0;
  const quoteBodies: unknown[] = [];
  const renewBodies: unknown[] = [];
  let renewalOutcome = { status: "succeeded", errorCode: undefined as string | undefined };
  await page.route("**/api/v2/**", async (route) => {
    const request = route.request();
    const path = new URL(request.url()).pathname;
    if (path === "/api/v2/catalog/compute-plans" || path === "/api/v2/catalog/storage-plans") {
      return route.fulfill({ json: { items: [] } });
    }
    if (path === "/api/v2/workspaces/workspace-one/subscription") {
      reads += 1;
      return route.fulfill({ json: subscription({ currentPeriodEnd: periodEnd }) });
    }
    if (path === "/api/v2/quotes") {
      quoteBodies.push(request.postDataJSON());
      return route.fulfill({ json: renewalQuote() });
    }
    if (path === "/api/v2/workspaces/workspace-one/renew") {
      renewBodies.push(request.postDataJSON());
      return route.fulfill({ json: acceptedOperation() });
    }
    if (path === "/api/v2/operations/workspace/op-renew-1") {
      return route.fulfill({ json: {
        ...acceptedOperation(),
        status: renewalOutcome.status,
        stage: renewalOutcome.status === "succeeded" ? "succeeded" : "failure_fence",
        ...(renewalOutcome.errorCode ? { errorCode: renewalOutcome.errorCode } : {})
      } });
    }
    return route.fulfill({ status: 503, json: { error: "owner_unavailable" } });
  });
  await page.goto(`${origin}__renewal`);

  await panel(page).getByRole("button", { name: "立即续费", exact: true }).waitFor();
  assert.equal(await panel(page).getByText("手动续费", { exact: true }).count(), 1);

  await panel(page).getByRole("button", { name: "立即续费", exact: true }).click();
  await page.getByRole("dialog").waitFor();
  assert.deepEqual(quoteBodies, [{
    purpose: "renew", workspaceId: "workspace-one", computePlanId: "compute-original",
    storagePlanId: "storage-original", modelSelections: [], periodMonths: 1
  }], "the renewal quote names the renewal variant and keeps the Workspace's own plan pair");
  assert.equal(await page.getByText("$52.580001", { exact: true }).count() > 0, true, "the customer's own price is shown before anything is charged");

  await page.getByRole("button", { name: "取消", exact: true }).click();
  assert.deepEqual(renewBodies, [], "a cancelled confirmation never charges the customer");
  assert.equal(await page.getByText("续费成功，订阅已更新为新的付费期。", { exact: true }).count(), 0);

  await panel(page).getByRole("button", { name: "立即续费", exact: true }).click();
  await page.getByRole("dialog").waitFor();
  await page.getByRole("button", { name: "确认续费", exact: true }).click();
  await panel(page).getByText("续费成功，订阅已更新为新的付费期。", { exact: true }).waitFor();
  assert.deepEqual(renewBodies, [{ quoteId: "quote-renew-1" }], "the owner receives the confirmed quote identifier");
  assert.equal(reads >= 2, true, "the owner's own subscription readback is read again after the renewal");

  renewalOutcome = { status: "failed", errorCode: "RENEWAL_PERIOD_ELAPSED" };
  periodEnd = "2027-01-01T00:00:00Z";
  await panel(page).getByRole("button", { name: "立即续费", exact: true }).click();
  await page.getByRole("dialog").waitFor();
  await page.getByRole("button", { name: "确认续费", exact: true }).click();
  await panel(page).getByText("当前付费期已结束，续费已被拒绝，未扣款。", { exact: true }).waitFor();
  assert.equal(await panel(page).getByText("续费成功，订阅已更新为新的付费期。", { exact: true }).count(), 0, "a refused renewal is never reported as a renewal");
  assert.deepEqual(errors, []);
});

test("an automatic or ended subscription never offers the manual renewal entry", { timeout: 60_000 }, async (t) => {
  const { page, origin, errors } = await startFixture(t);
  let current = subscription();
  await page.route("**/api/v2/**", async (route) => {
    const path = new URL(route.request().url()).pathname;
    if (path === "/api/v2/catalog/compute-plans" || path === "/api/v2/catalog/storage-plans") {
      return route.fulfill({ json: { items: [] } });
    }
    if (path === "/api/v2/workspaces/workspace-one/subscription") return route.fulfill({ json: current });
    return route.fulfill({ status: 503, json: { error: "owner_unavailable" } });
  });
  await page.goto(`${origin}__renewal`);

  await panel(page).getByRole("button", { name: "立即续费", exact: true }).waitFor();
  current = subscription({ renewalMode: "automatic", renewalConsentId: "consent-original" });
  await panel(page).getByRole("button", { name: "刷新订阅", exact: true }).click();
  await panel(page).getByText("自动续费", { exact: true }).waitFor();
  assert.equal(await panel(page).getByRole("button", { name: "立即续费", exact: true }).count(), 0, "an automatic subscription is renewed by its own cycle");

  current = subscription({ status: "expired" });
  await panel(page).getByRole("button", { name: "刷新订阅", exact: true }).click();
  await panel(page).getByText("已到期", { exact: true }).waitFor();
  assert.equal(await panel(page).getByRole("button", { name: "立即续费", exact: true }).count(), 0, "an ended paid period has no renewal entry");
  assert.deepEqual(errors, []);
});
