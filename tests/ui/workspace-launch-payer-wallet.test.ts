import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import test from "node:test";

import { walletReadRoute } from "../../apps/console-ui/src/api/console-read-api.ts";
import { getLaunchPayerWallet } from "../../apps/console-ui/src/api/workspaces-api.ts";

const repositoryRoot = fileURLToPath(new URL("../..", import.meta.url)).replace(/\/$/, "");

function sourceText(relativePath: string): string {
  return readFileSync(join(repositoryRoot, relativePath), "utf8");
}

const payerReadback = {
  source: "gateway",
  status: "available",
  currency: "USD",
  balanceUSDMicros: "12500000",
  fetchedAt: "2026-11-22T00:00:00Z"
};

// Records the route the reader actually requests instead of pinning the
// expression that decides it.
async function recordRequestedRoutes<T>(run: () => Promise<T>): Promise<{ requested: string[]; result: T }> {
  const originalFetch = globalThis.fetch;
  const requested: string[] = [];
  globalThis.fetch = (async (input: string | URL | Request) => {
    requested.push(String(input));
    return new Response(JSON.stringify(payerReadback), {
      status: 200,
      headers: { "content-type": "application/json" }
    });
  }) as typeof globalThis.fetch;
  try {
    return { requested, result: await run() };
  } finally {
    globalThis.fetch = originalFetch;
  }
}

// The purchase gate decides on the balance the order is actually charged to.
// The Tenant's paying account and the caller's personal account are two
// different facts, and the launch page used to compare the price against the
// personal one, so an order the Tenant could pay for was refused as
// "insufficient balance".
test("the launch gate reads the Tenant's paying balance", async () => {
  const { requested, result } = await recordRequestedRoutes(() => getLaunchPayerWallet());

  assert.deepEqual(requested, ["/api/v2/tenant/wallet"]);
  assert.equal(result.balanceUSDMicros, "12500000");
  assert.equal(result.source, "gateway");
  assert.equal(result.status, "available");
});

// The personal wallet read belongs to the caller and stays that way: the frozen
// interaction spec keeps the default balance and token usage on the logged-in
// user. Only the purchase gate moves to the Tenant's payer.
test("the personal wallet read still names the caller's own account", () => {
  assert.equal(walletReadRoute(true), "/api/v2/wallet");
  assert.equal(walletReadRoute(false), "/api/gateway/wallet");

  const launchApi = sourceText("apps/console-ui/src/api/workspaces-api.ts");
  assert.doesNotMatch(launchApi, /"\/api\/v2\/wallet"/, "the launch module must not read the caller's personal wallet");
});

// A React hook cannot run in this Node lane, so the gate itself is pinned at
// the seam: the quote total is compared against the payer wallet, and no
// personal balance enters that decision.
test("the launch gate compares the quote against the payer wallet", () => {
  const controller = sourceText("apps/console-ui/src/app/use-workspace-launch-controller.ts");

  assert.match(controller, /walletSufficient:\s*Boolean\(agentPayerWallet\s*&&\s*agentQuote\s*&&\s*BigInt\(agentPayerWallet\.balanceUSDMicros\)\s*>=\s*BigInt\(agentQuote\.totalUSDMicros\)\)/);
  assert.match(controller, /walletReadbackReady:\s*Boolean\(agentPayerWallet\)/);
  assert.doesNotMatch(controller, /walletSufficient:[^\n]*personal/i, "the purchase gate must not read a personal balance");
});

// The page shows which balance the decision uses, and there is no self-service
// payment: the Tenant is recharged by an administrator, so the insufficient
// balance alert points at the administrator instead of a payment button.
test("the launch page states the available balance and points at the administrator", () => {
  const page = sourceText("apps/console-ui/src/components/workspaces/WorkspaceLaunchPage.tsx");

  assert.match(page, /<dt>可用余额<\/dt>/);
  assert.match(page, /<dt>当前可用余额<\/dt>/);
  assert.match(page, /当前可用余额不足以支付本次费用，请联系管理员充值后重新获取报价。/);
  assert.doesNotMatch(page, /钱包余额不足，请充值后重新报价。/, "the old personal self-service wording must be gone");
});
