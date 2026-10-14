import assert from "node:assert/strict";
import test from "node:test";

import { decodeGatewayWalletReadback, walletReadRoute } from "../../apps/console-ui/src/api/console-read-api.ts";
import { walletSourceTag } from "../../apps/console-ui/src/app/gateway-account-read-controller-model.ts";

// The wallet read follows the baked console identity. A retained build still
// authenticates through /api/auth/login and must keep reading the Control Plane
// answer it has always had; requesting the BFF route there yields 401.
test("the wallet read route follows the baked console identity", () => {
  assert.equal(walletReadRoute(true), "/api/v2/wallet");
  assert.equal(walletReadRoute(false), "/api/gateway/wallet");
});

test("the wallet source tag follows the same identity as the route", () => {
  assert.equal(walletSourceTag(true), "gateway");
  assert.equal(walletSourceTag(false), "sub2api");
  assert.equal(
    (walletReadRoute(true) === "/api/v2/wallet") === (walletSourceTag(true) === "gateway"),
    true
  );
  assert.equal(
    (walletReadRoute(false) === "/api/gateway/wallet") === (walletSourceTag(false) === "sub2api"),
    true
  );
});

// The decoder previously used /^[1-9][0-9]*$/, which rejected the zero a charge
// leaves behind even though the contract's USDMicros pattern permits it.
test("the wallet decoder accepts a nonnegative balance including zero", () => {
  const readback = (micros: string) => ({ source: "gateway", status: "available", currency: "USD", balanceUSDMicros: micros, fetchedAt: "2026-10-14T00:00:00Z" });
  assert.equal(decodeGatewayWalletReadback(readback("0")).usdMicros, "0");
  assert.equal(decodeGatewayWalletReadback(readback("25000000")).usdMicros, "25000000");
  assert.equal(decodeGatewayWalletReadback(readback("9223372036854775807")).usdMicros, "9223372036854775807");
  for (const invalid of ["-1", "01", "1.5", "", " 1"]) {
    assert.throws(() => decodeGatewayWalletReadback(readback(invalid)), /invalid_gateway_wallet_readback/, `accepted ${invalid}`);
  }
});
