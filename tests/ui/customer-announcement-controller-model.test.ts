import assert from "node:assert/strict";
import test from "node:test";

import type {
  AnnouncementDTO,
  AnnouncementPageDTO,
  AnnouncementReadDTO
} from "../../apps/console-ui/src/api/dtos.ts";
import {
  announcementReadReceiptMatches,
  announcementReadbackMatches,
  announcementReadbackPreservesReceipts,
  resolveAnnouncementReadIntent,
  type AnnouncementReadIntent
} from "../../apps/console-ui/src/app/customer-announcement-controller-model.ts";

const unread: AnnouncementDTO = {
  id: "announcement-alpha",
  title: "Planned maintenance",
  body: "Workspace access remains available.",
  status: "published",
  publishedAt: "2026-08-27T00:00:00Z",
  createdAt: "2026-08-27T00:00:00Z",
  updatedAt: "2026-08-27T00:00:00Z",
  read: false
};

function page(items: AnnouncementDTO[]): AnnouncementPageDTO {
  return { items, total: items.length, page: 1, pageSize: 20 };
}

test("same unresolved announcement reuses its original idempotency key", () => {
  let keysCreated = 0;
  const first = resolveAnnouncementReadIntent(null, unread.id, () => {
    keysCreated += 1;
    return "announcement-read:first";
  });
  const replay = resolveAnnouncementReadIntent(first, unread.id, () => {
    keysCreated += 1;
    return "announcement-read:replay";
  });

  assert.equal(replay, first);
  assert.equal(keysCreated, 1);
});

test("a different announcement receives a different intent and key", () => {
  const current: AnnouncementReadIntent = {
    announcementId: unread.id,
    idempotencyKey: "announcement-read:existing"
  };
  const next = resolveAnnouncementReadIntent(current, "announcement-beta", () => "announcement-read:next");

  assert.notEqual(next, current);
  assert.deepEqual(next, {
    announcementId: "announcement-beta",
    idempotencyKey: "announcement-read:next"
  });
});

test("read receipt requires the exact announcement and a valid RFC3339 instant", () => {
  const receipt: AnnouncementReadDTO = {
    announcementId: unread.id,
    readAt: "2026-08-27T08:00:00.123456789+08:00"
  };

  assert.equal(announcementReadReceiptMatches(receipt, unread.id), true);
  assert.equal(announcementReadReceiptMatches({ ...receipt, announcementId: "announcement-beta" }, unread.id), false);
  for (const readAt of [
    "",
    "2026-08-27",
    "2026-02-30T00:00:00Z",
    "2026-08-27T24:00:00Z",
    "2026-08-27T00:00:00+24:00"
  ]) {
    assert.equal(announcementReadReceiptMatches({ ...receipt, readAt }, unread.id), false);
  }
});

test("readback accepts an absent target but rejects a visible unread target", () => {
  assert.equal(announcementReadbackMatches(page([{ ...unread, read: true }]), unread.id), true);
  assert.equal(announcementReadbackMatches(page([unread]), unread.id), false);
  assert.equal(announcementReadbackMatches(page([{ ...unread, id: "announcement-beta" }]), unread.id), true);
});

test("readback preserves every receipt confirmed in the current Session", () => {
  const alpha = { ...unread, read: true };
  const beta = { ...unread, id: "announcement-beta", read: true };

  assert.equal(announcementReadbackPreservesReceipts(page([alpha, beta]), [alpha.id, beta.id]), true);
  assert.equal(announcementReadbackPreservesReceipts(page([alpha]), [alpha.id, beta.id]), true);
  assert.equal(announcementReadbackPreservesReceipts(page([alpha, { ...beta, read: false }]), [alpha.id, beta.id]), false);
});
