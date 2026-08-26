import assert from "node:assert/strict";
import test from "node:test";

import type { WalletAdjustmentRequest } from "../../apps/console-ui/src/api/dtos.ts";
import {
  resolveWalletAdjustmentIntent,
  sameWalletAdjustmentInput,
  walletRecoveryIdempotencyKey,
  type WalletAdjustmentIntent
} from "../../apps/console-ui/src/app/wallet-adjustment-controller-model.ts";

const recharge: WalletAdjustmentRequest = {
  kind: "recharge",
  amountUsd: "10.000000",
  reason: "support correction",
  confirmationAccountId: "account-alpha"
};

test("same wallet adjustment input reuses the original idempotency intent", () => {
  const current: WalletAdjustmentIntent = {
    accountId: "account-alpha",
    input: recharge,
    idempotencyKey: "wallet-adjustment:existing"
  };
  let keysCreated = 0;

  const result = resolveWalletAdjustmentIntent(current, "account-alpha", { ...recharge }, () => {
    keysCreated += 1;
    return "wallet-adjustment:new";
  });

  assert.equal(result, current);
  assert.equal(keysCreated, 0);
});

test("wallet input comparison is independent of optional field insertion order", () => {
  const reordered: WalletAdjustmentRequest = {
    confirmationAccountId: "account-alpha",
    reason: "support correction",
    amountUsd: "10.000000",
    kind: "recharge"
  };

  assert.equal(sameWalletAdjustmentInput(recharge, reordered), true);
});

test("account or input changes create a new wallet adjustment intent", () => {
  const current: WalletAdjustmentIntent = {
    accountId: "account-alpha",
    input: recharge,
    idempotencyKey: "wallet-adjustment:existing"
  };
  const changed: WalletAdjustmentRequest = { ...recharge, amountUsd: "11.000000" };
  let keysCreated = 0;

  const result = resolveWalletAdjustmentIntent(current, "account-beta", changed, () => {
    keysCreated += 1;
    return "wallet-adjustment:new";
  });

  assert.deepEqual(result, {
    accountId: "account-beta",
    input: changed,
    idempotencyKey: "wallet-adjustment:new"
  });
  assert.equal(keysCreated, 1);
  assert.equal(sameWalletAdjustmentInput(recharge, changed), false);
});

test("wallet recovery keeps the original operation identity", () => {
  assert.equal(walletRecoveryIdempotencyKey("wallet-adjustment-0123456789abcdef12"), "wallet-recovery-0123456789abcdef");
  assert.equal(walletRecoveryIdempotencyKey("wallet-adjustment-invalid"), "");
});
