import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { resolve } from "node:path";
import test from "node:test";

import type { GatewayAccountUsageSummaryDTO, GatewayWallet, SourceEnvelope } from "../../apps/console-ui/src/api/dtos.ts";
import {
  decodeGatewayUsageSummaryReadback,
  decodeGatewayWalletReadback,
  gatewayUsageSummaryRoute,
  getGatewayWallet
} from "../../apps/console-ui/src/api/console-read-api.ts";
import { cloudIdentity } from "../../apps/console-ui/src/app/console-identity.ts";
import {
  accountUsageSourceTag,
  applyGatewayAccountReadCompletion,
  beginGatewayAccountReadProjection,
  createGatewayAccountReadRequestScope,
  createGatewayAccountReadState,
  gatewayAccountReadSourceMatchesScope,
  walletSourceTag,
  type GatewayAccountReadCompletion,
  type GatewayAccountReadState
} from "../../apps/console-ui/src/app/gateway-account-read-controller-model.ts";

const fetchedAt = "2026-10-14T00:00:00Z";

const retainedWallet: GatewayWallet = { userId: "41", currency: "USD", usdMicros: "25000000", status: "active" };

function retainedWalletEnvelope(): SourceEnvelope<GatewayWallet> {
  return { source: walletSourceTag(false), status: "available", available: true, fetchedAt, data: retainedWallet };
}

// What a cloud build gets back from the BFF, shaped by the reader's cloud branch.
function cloudWalletEnvelope(): SourceEnvelope<GatewayWallet> {
  return { source: walletSourceTag(true), status: "available", available: true, fetchedAt, data: retainedWallet };
}

function walletScope() {
  return createGatewayAccountReadRequestScope("wallet", {
    sessionGeneration: 3,
    routeGeneration: 5,
    routeScope: "workspace_launch",
    requestGeneration: 1
  });
}

function loadingWallet(): GatewayAccountReadState {
  return beginGatewayAccountReadProjection(createGatewayAccountReadState(), "wallet");
}

function walletCompletion(source: SourceEnvelope<GatewayWallet>): GatewayAccountReadCompletion {
  const scope = walletScope();
  return { kind: "wallet", activeScope: scope, responseScope: scope, source };
}

// Records the routes the reader actually requests instead of pinning the
// expression that decides them. The Node lane has no baked cloud identity, so it
// proves the retained branch; the cloud branch is the same call with the other
// identity and is covered by the guard tests below.
async function recordRequestedRoutes<T>(run: () => Promise<T>): Promise<{ requested: string[]; result: T }> {
  const originalFetch = globalThis.fetch;
  const requested: string[] = [];
  globalThis.fetch = (async (input: string | URL | Request) => {
    requested.push(String(input));
    return new Response(JSON.stringify(retainedWalletEnvelope()), {
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

// The reader is the caller behind the identity decision. A cloud build that
// requests the BFF route from a retained session gets a 401 (#840); a retained
// build that requests the BFF route has no Gateway session at all.
test("the retained wallet read requests the Control Plane route and answers with the retained owner", async () => {
  assert.equal(cloudIdentity, false, "the Node lane runs without a baked cloud identity");

  const { requested, result } = await recordRequestedRoutes(() => getGatewayWallet());

  assert.deepEqual(requested, ["/api/gateway/wallet"]);
  assert.equal(result.source, walletSourceTag(cloudIdentity));
  assert.equal(result.available, true);
  assert.equal(result.data?.usdMicros, "25000000");
  // The settle layer accepts the reader's own answer under the same identity.
  assert.equal(gatewayAccountReadSourceMatchesScope(walletScope(), result, cloudIdentity), true);
});

// The wallet's owner follows the identity, so a cloud read names the Gateway
// owner while a retained read keeps naming Sub2API.
test("the account usage route and source tag follow the baked identity", () => {
  assert.equal(gatewayUsageSummaryRoute(true), "/api/v2/usage/summary");
  assert.equal(gatewayUsageSummaryRoute(false), "/api/gateway/usage-summary");
  assert.equal(accountUsageSourceTag(true), "gateway");
  assert.equal(accountUsageSourceTag(false), "sub2api");
});

// The apply guard is the only write path into the projection state. It has to be
// evaluated with the same identity the settle layer accepted the read with:
// dropping it made a cloud completion return null, and commit()'s `?? current`
// left the projection loading forever with no error and no message.
test("the apply guard settles a cloud wallet completion under the cloud identity", () => {
  const settled = applyGatewayAccountReadCompletion(loadingWallet(), walletCompletion(cloudWalletEnvelope()), true);

  assert.ok(settled, "a cloud wallet completion must not be dropped by the apply guard");
  assert.equal(settled.wallet.loading, false);
  assert.equal(settled.wallet.error, "");
  assert.equal(settled.wallet.value?.source, "gateway");
  assert.equal(settled.wallet.value?.data?.usdMicros, "25000000");
});

test("the apply guard still refuses the other identity's wallet owner", () => {
  assert.equal(
    applyGatewayAccountReadCompletion(loadingWallet(), walletCompletion(cloudWalletEnvelope()), false),
    null,
    "a retained build must not commit a Gateway-source wallet completion"
  );
  assert.equal(
    applyGatewayAccountReadCompletion(loadingWallet(), walletCompletion(retainedWalletEnvelope()), true),
    null,
    "a cloud build must not commit a Sub2API-source wallet completion"
  );

  const retained = applyGatewayAccountReadCompletion(loadingWallet(), walletCompletion(retainedWalletEnvelope()), false);
  assert.ok(retained, "a retained build must still commit its own wallet completion");
  assert.equal(retained.wallet.loading, false);
  assert.equal(retained.wallet.value?.source, "sub2api");
});

// The behaviour above proves the guard, but not that the controller hands it the
// baked identity: a React hook cannot run in this Node lane, and the demo
// browser server only drives the retained login path. This pins the seam itself,
// so dropping the third argument fails here instead of at runtime on a cloud build.
test("the controller commits with the same baked identity the settle layer validates with", async () => {
  const controller = await readFile(
    resolve(import.meta.dirname, "../../apps/console-ui/src/app/use-gateway-account-read-controller.ts"),
    "utf8"
  );

  assert.match(controller, /import \{ cloudIdentity \} from "\.\/console-identity\.ts";/);
  const applyCalls = [...controller.matchAll(/applyGatewayAccountReadCompletion\(([^()]*)\)/g)]
    .map((match) => match[1].trim());
  assert.deepEqual(applyCalls, ["current, completion, cloudIdentity"]);
  assert.match(controller, /gatewayAccountReadSourceMatchesScope\(requestScope, result, cloudIdentity\)/);
  assert.match(controller, /walletSourceTag\(cloudIdentity\)/);
});

// The decoder previously used /^[1-9][0-9]*$/, which rejected the zero a charge
// leaves behind even though the contract's USDMicros pattern permits it.
test("the usage decoder accepts canonical decimal int64 strings and rejects unsafe or malformed values", () => {
  const readback = (overrides: Partial<Record<string, unknown>> = {}) => ({
    totalRequests: "17",
    totalInputTokens: "170",
    totalOutputTokens: "34",
    totalTokens: "204",
    totalActualCostUSDMicros: "125001",
    ...overrides
  });
  const decoded: GatewayAccountUsageSummaryDTO = decodeGatewayUsageSummaryReadback(readback());
  assert.deepEqual(decoded, {
    totalRequests: 17,
    totalInputTokens: 170,
    totalOutputTokens: 34,
    totalTokens: 204,
    totalActualCostUsdMicros: "125001"
  });
  for (const invalid of [
    { totalRequests: "-1" },
    { totalInputTokens: "01" },
    { totalOutputTokens: "1.5" },
    { totalTokens: 9223372036854775807 },
    { totalActualCostUSDMicros: "" }
  ]) {
    assert.throws(() => decodeGatewayUsageSummaryReadback(readback(invalid)), /invalid_gateway_usage_summary|invalid_personal_gateway_money/);
  }
});

test("the wallet decoder accepts a nonnegative balance including zero", () => {
  const readback = (micros: string) => ({ source: "gateway", status: "available", currency: "USD", balanceUSDMicros: micros, fetchedAt: "2026-10-14T00:00:00Z" });
  assert.equal(decodeGatewayWalletReadback(readback("0")).usdMicros, "0");
  assert.equal(decodeGatewayWalletReadback(readback("25000000")).usdMicros, "25000000");
  assert.equal(decodeGatewayWalletReadback(readback("9223372036854775807")).usdMicros, "9223372036854775807");
  for (const invalid of ["-1", "01", "1.5", "", " 1"]) {
    assert.throws(() => decodeGatewayWalletReadback(readback(invalid)), /invalid_gateway_wallet_readback/, `accepted ${invalid}`);
  }
});

test("Cloud usage summary retains exact legal int64 money while refusing unsafe numeric counts", () => {
  const readback = { totalRequests: "7", totalInputTokens: "70", totalOutputTokens: "14", totalTokens: "84", totalActualCostUSDMicros: "9223372036854775807" };
  assert.equal(decodeGatewayUsageSummaryReadback(readback).totalActualCostUsdMicros, "9223372036854775807");
  assert.equal(decodeGatewayUsageSummaryReadback({ ...readback, totalActualCostUSDMicros: "9007199254740993" }).totalActualCostUsdMicros, "9007199254740993");
  assert.throws(() => decodeGatewayUsageSummaryReadback({ ...readback, totalRequests: "9007199254740993" }), /invalid_gateway_usage_summary_requests/);
});
