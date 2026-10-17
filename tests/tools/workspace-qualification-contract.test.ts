import assert from "node:assert/strict";
import test from "node:test";

import {
  LEGACY_QUALIFICATION_J0_READY_KINDS,
  QUALIFICATION_J0_READY_KIND,
  createJ0ReadyReceiptValidator,
  qualificationProviderProfile,
} from "../../tools/workspace-qualification-contract.ts";
import {
  LOCAL_DOCKER_QUALIFICATION_PROFILE,
  validateJ0ReadyReceipt,
} from "../../tools/local-workspace-qualification.ts";

const SOURCE_SHA = "a".repeat(40);
const SOURCE_TREE = "b".repeat(40);

function j0ReadyReceipt(overrides = {}) {
  return {
    schemaVersion: 1,
    kind: QUALIFICATION_J0_READY_KIND,
    status: "READY",
    source: { sha: SOURCE_SHA, tree: SOURCE_TREE, clean: true },
    authority: { mode: "live", class: "sandbox", dedicated: true, confirmed: true },
    provider: "local-docker",
    failed: 0,
    skipped: 0,
    ...overrides,
  };
}

test("the J0-ready kind is named for the qualification, not for a provider", () => {
  assert.equal(QUALIFICATION_J0_READY_KIND, "opl.workspace-qualification.j0-ready.v2");
  assert.doesNotMatch(QUALIFICATION_J0_READY_KIND, /local|docker|tke|tencent/);
});

test("a provider profile declares its own identity rather than being asserted by the validator", () => {
  const profile = qualificationProviderProfile("tencent-tke");
  assert.equal(profile.provider, "tencent-tke");
  assert.deepEqual([...profile.receiptKinds], [QUALIFICATION_J0_READY_KIND]);
});

test("a declared provider profile validates its own receipt", () => {
  const tencent = qualificationProviderProfile("tencent-tke");
  const validate = createJ0ReadyReceiptValidator(tencent);
  const receipt = j0ReadyReceipt({ provider: "tencent-tke" });
  assert.equal(validate(receipt, SOURCE_SHA, SOURCE_TREE), receipt);
});

test("a provider profile rejects another provider's receipt", () => {
  const tencent = qualificationProviderProfile("tencent-tke");
  const validate = createJ0ReadyReceiptValidator(tencent);
  assert.throws(() => validate(j0ReadyReceipt({ provider: "local-docker" }), SOURCE_SHA, SOURCE_TREE), {
    code: "j0_ready_receipt_invalid",
  });
});

test("the local validator rejects a Tencent-TKE receipt", () => {
  assert.throws(() => validateJ0ReadyReceipt(j0ReadyReceipt({ provider: "tencent-tke" }), SOURCE_SHA, SOURCE_TREE), {
    code: "j0_ready_receipt_invalid",
  });
});

test("the legacy provider-named kind still validates for the Local-Docker profile", () => {
  // The external J0 producer lives outside this repository and still emits
  // opl.local-workspace.j0-ready.v1. Rejecting it would break the live path
  // without moving anything forward.
  const receipt = j0ReadyReceipt({ kind: LEGACY_QUALIFICATION_J0_READY_KINDS[0] });
  assert.equal(validateJ0ReadyReceipt(receipt, SOURCE_SHA, SOURCE_TREE), receipt);
});

test("the legacy kind is not accepted for a profile that did not declare it", () => {
  const tencent = qualificationProviderProfile("tencent-tke");
  const validate = createJ0ReadyReceiptValidator(tencent);
  const receipt = j0ReadyReceipt({ kind: LEGACY_QUALIFICATION_J0_READY_KINDS[0], provider: "tencent-tke" });
  assert.throws(() => validate(receipt, SOURCE_SHA, SOURCE_TREE), { code: "j0_ready_receipt_invalid" });
});

test("the Local-Docker profile declares the provider instead of hard-coding it", () => {
  assert.equal(LOCAL_DOCKER_QUALIFICATION_PROFILE.provider, "local-docker");
  assert.ok(LOCAL_DOCKER_QUALIFICATION_PROFILE.receiptKinds.includes(LEGACY_QUALIFICATION_J0_READY_KINDS[0]));
});