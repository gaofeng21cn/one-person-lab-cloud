import assert from "node:assert/strict";
import test from "node:test";

import type {
  CreateSupportTicketMappingRequest,
  SupportTicketMappingDTO,
  SupportTicketPageDTO
} from "../../apps/console-ui/src/api/dtos.ts";
import {
  resolveSupportMappingIntent,
  supportMappingReadbackMatches,
  supportMappingResponseMatches
} from "../../apps/console-ui/src/app/support-controller-model.ts";

const input: CreateSupportTicketMappingRequest = {
  accountId: "acct-alpha",
  externalTicketId: "SUP-2026-001",
  title: "Workspace cannot start",
  externalSystem: "support",
  resourceIds: ["runtime-alpha"]
};

const ticket: SupportTicketMappingDTO = {
  id: "support-1",
  externalSystem: "support",
  externalTicketId: "SUP-2026-001",
  externalUrl: "",
  accountId: "acct-alpha",
  resourceIds: ["runtime-alpha"],
  title: "Workspace cannot start",
  category: "support",
  priority: "normal",
  status: "open",
  createdAt: "2026-08-26T00:00:00Z",
  updatedAt: "2026-08-26T00:00:00Z",
  messages: []
};

test("Support mapping intent reuses the idempotency key for the same input", () => {
  const current = { input, idempotencyKey: "support-map:existing" };
  let created = 0;

  const result = resolveSupportMappingIntent(current, { ...input, resourceIds: ["runtime-alpha"] }, () => {
    created += 1;
    return "support-map:new";
  });

  assert.equal(result, current);
  assert.equal(created, 0);
});

test("Support mapping intent changes only when the input changes", () => {
  const current = { input, idempotencyKey: "support-map:existing" };
  const result = resolveSupportMappingIntent(current, { ...input, title: "Different title" }, () => "support-map:new");

  assert.deepEqual(result, {
    input: { ...input, title: "Different title" },
    idempotencyKey: "support-map:new"
  });
});

test("Support mapping accepts an identity-matched response and list readback", () => {
  const page: SupportTicketPageDTO = { tickets: [ticket] };

  assert.equal(supportMappingResponseMatches(ticket, input), true);
  assert.equal(supportMappingReadbackMatches(page, input), true);
});

test("Support mapping rejects a different account or external ticket", () => {
  assert.equal(supportMappingResponseMatches({ ...ticket, accountId: "acct-other" }, input), false);
  assert.equal(supportMappingReadbackMatches({ tickets: [{ ...ticket, externalTicketId: "SUP-OTHER" }] }, input), false);
});
