import assert from "node:assert/strict";
import test from "node:test";

import type {
  AnnouncementDTO,
  AnnouncementDraftRequest,
  AnnouncementScheduleRequest
} from "../../apps/console-ui/src/api/dtos.ts";
import {
  announcementProjectionMatches,
  createAnnouncementCommandMatches,
  publishAnnouncementCommandMatches,
  resolveAnnouncementCreateIntent,
  resolveAnnouncementPublishIntent,
  sameAnnouncementDraft,
  withdrawAnnouncementCommandMatches,
  type AnnouncementCreateIntent,
  type AnnouncementPublishIntent
} from "../../apps/console-ui/src/app/operator-announcement-controller-model.ts";

const draftInput: AnnouncementDraftRequest = {
  title: "Planned maintenance",
  body: "Workspace access remains available.",
  startsAt: "2026-08-28T01:00:00Z",
  endsAt: "2026-08-28T02:00:00Z"
};

const draft: AnnouncementDTO = {
  id: "announcement-alpha",
  title: draftInput.title,
  body: draftInput.body,
  status: "draft",
  startsAt: draftInput.startsAt,
  endsAt: draftInput.endsAt,
  createdAt: "2026-08-27T00:00:00Z",
  updatedAt: "2026-08-27T00:00:00Z",
  read: false
};

test("create intent normalizes one semantic draft and reuses its original key", () => {
  const input: AnnouncementDraftRequest = {
    title: "  Planned maintenance  ",
    body: "  Workspace access remains available.  ",
    startsAt: "  2026-08-28T01:00:00Z ",
    endsAt: "  2026-08-28T02:00:00Z "
  };
  let keysCreated = 0;
  const first = resolveAnnouncementCreateIntent(null, input, () => {
    keysCreated += 1;
    return "announcement-create:first";
  });
  const replay = resolveAnnouncementCreateIntent(first, { ...draftInput }, () => {
    keysCreated += 1;
    return "announcement-create:second";
  });

  assert.deepEqual(first.input, draftInput);
  assert.equal(replay, first);
  assert.equal(sameAnnouncementDraft(first.input, input), true);
  assert.equal(keysCreated, 1);
});

test("changed draft semantics receive a new key", () => {
  const current: AnnouncementCreateIntent = {
    input: draftInput,
    idempotencyKey: "announcement-create:existing"
  };
  const changes: AnnouncementDraftRequest[] = [
    { ...draftInput, title: "Different title" },
    { ...draftInput, body: "Different body" },
    { ...draftInput, startsAt: "2026-08-29T01:00:00Z" },
    { ...draftInput, endsAt: undefined }
  ];

  for (const [index, input] of changes.entries()) {
    const key = `announcement-create:new-${index}`;
    const next = resolveAnnouncementCreateIntent(current, input, () => key);
    assert.notEqual(next, current);
    assert.equal(next.idempotencyKey, key);
    assert.equal(sameAnnouncementDraft(current.input, input), false);
  }
});

test("publish intent freezes one schedule for response-loss retry", () => {
  const schedule: AnnouncementScheduleRequest = {
    startsAt: draft.startsAt || "",
    endsAt: draft.endsAt
  };
  const current: AnnouncementPublishIntent = {
    announcementId: draft.id,
    input: schedule,
    idempotencyKey: "announcement-publish:existing"
  };
  let keysCreated = 0;

  const replay = resolveAnnouncementPublishIntent(current, draft, () => "2026-08-27T03:00:00Z", () => {
    keysCreated += 1;
    return "announcement-publish:new";
  });
  const immediate = resolveAnnouncementPublishIntent(null, { ...draft, startsAt: undefined, endsAt: undefined }, () => "2026-08-27T03:00:00Z", () => {
    keysCreated += 1;
    return "announcement-publish:immediate";
  });

  assert.equal(replay, current);
  assert.deepEqual(immediate, {
    announcementId: draft.id,
    input: { startsAt: "2026-08-27T03:00:00Z" },
    idempotencyKey: "announcement-publish:immediate"
  });
  assert.equal(keysCreated, 1);
});

test("commands fail closed on announcement identity, state, or schedule mismatch", () => {
  const scheduled: AnnouncementDTO = { ...draft, status: "scheduled" };
  const published: AnnouncementDTO = {
    ...draft,
    status: "published",
    publishedAt: "2026-08-27T00:01:00Z"
  };
  const schedule: AnnouncementScheduleRequest = {
    startsAt: draft.startsAt || "",
    endsAt: draft.endsAt
  };

  assert.equal(createAnnouncementCommandMatches(draft, draftInput), true);
  assert.equal(createAnnouncementCommandMatches({ ...draft, id: "" }, draftInput), false);
  assert.equal(createAnnouncementCommandMatches({ ...draft, status: "published" }, draftInput), false);
  assert.equal(publishAnnouncementCommandMatches(scheduled, draft.id, schedule), true);
  assert.equal(publishAnnouncementCommandMatches(published, draft.id, schedule), true);
  assert.equal(publishAnnouncementCommandMatches({ ...scheduled, id: "announcement-beta" }, draft.id, schedule), false);
  assert.equal(publishAnnouncementCommandMatches({ ...scheduled, startsAt: "2026-08-29T01:00:00Z" }, draft.id, schedule), false);
  assert.equal(withdrawAnnouncementCommandMatches({ ...published, status: "withdrawn" }, draft.id), true);
  assert.equal(withdrawAnnouncementCommandMatches(published, draft.id), false);
});

test("command and readback matching use RFC3339 instant semantics", () => {
  const offsetInput: AnnouncementDraftRequest = {
    ...draftInput,
    startsAt: "2026-08-28T09:00:00+08:00",
    endsAt: "2026-08-28T10:00:00.000+08:00"
  };
  const canonicalDraft: AnnouncementDTO = {
    ...draft,
    startsAt: "2026-08-28T01:00:00Z",
    endsAt: "2026-08-28T02:00:00Z"
  };
  const offsetSchedule: AnnouncementScheduleRequest = {
    startsAt: offsetInput.startsAt || "",
    endsAt: offsetInput.endsAt
  };

  assert.equal(sameAnnouncementDraft(offsetInput, draftInput), true);
  assert.equal(createAnnouncementCommandMatches(canonicalDraft, offsetInput), true);
  assert.equal(publishAnnouncementCommandMatches({ ...canonicalDraft, status: "scheduled" }, draft.id, offsetSchedule), true);
  assert.equal(announcementProjectionMatches(
    { ...canonicalDraft, status: "published", publishedAt: "2026-08-28T01:00:00.0000000009Z" },
    { ...canonicalDraft, status: "published", publishedAt: "2026-08-28T09:00:00+08:00" }
  ), true);

  const invalidTime = "2026-02-30T01:00:00Z";
  assert.equal(createAnnouncementCommandMatches(
    { ...canonicalDraft, startsAt: invalidTime },
    { ...draftInput, startsAt: invalidTime }
  ), false);
  assert.equal(publishAnnouncementCommandMatches(
    { ...canonicalDraft, status: "scheduled", startsAt: invalidTime },
    draft.id,
    { ...offsetSchedule, startsAt: invalidTime }
  ), false);
});

test("readback must match the complete command projection", () => {
  const command: AnnouncementDTO = {
    ...draft,
    status: "published",
    publishedAt: "2026-08-28T01:00:00Z",
    updatedAt: "2026-08-28T01:00:00Z"
  };

  assert.equal(announcementProjectionMatches(command, { ...command, read: true }), true);
  for (const mismatch of [
    { ...command, id: "announcement-beta" },
    { ...command, title: "Different title" },
    { ...command, body: "Different body" },
    { ...command, status: "scheduled" as const },
    { ...command, startsAt: "2026-08-29T01:00:00Z" },
    { ...command, endsAt: undefined },
    { ...command, publishedAt: undefined },
    { ...command, createdAt: "2026-08-26T00:00:00Z" },
    { ...command, updatedAt: "2026-08-28T01:00:01Z" }
  ]) {
    assert.equal(announcementProjectionMatches(command, mismatch), false);
  }
});
