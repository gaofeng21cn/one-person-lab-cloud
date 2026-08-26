import type {
  AnnouncementDTO,
  AnnouncementDraftRequest,
  AnnouncementScheduleRequest
} from "../api/dtos.ts";

export interface AnnouncementCreateIntent {
  input: AnnouncementDraftRequest;
  idempotencyKey: string;
}

export interface AnnouncementPublishIntent {
  announcementId: string;
  input: AnnouncementScheduleRequest;
  idempotencyKey: string;
}

function optionalTime(value: string | undefined) {
  return value?.trim() || undefined;
}

const rfc3339Pattern = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(?:\.(\d+))?(Z|([+-])(\d{2}):(\d{2}))$/;

function rfc3339Instant(value: string | undefined) {
  const normalized = optionalTime(value);
  if (!normalized) return null;
  const match = rfc3339Pattern.exec(normalized);
  if (!match) return undefined;

  const [, yearText, monthText, dayText, hourText, minuteText, secondText, fractionText = "", zone, offsetSign, offsetHourText, offsetMinuteText] = match;
  const year = Number(yearText);
  const month = Number(monthText);
  const day = Number(dayText);
  const hour = Number(hourText);
  const minute = Number(minuteText);
  const second = Number(secondText);
  const leapYear = year % 4 === 0 && (year % 100 !== 0 || year % 400 === 0);
  const daysInMonth = [31, leapYear ? 29 : 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31];
  if (month < 1 || month > 12 || day < 1 || day > daysInMonth[month - 1]
    || hour > 23 || minute > 59 || second > 59) return undefined;

  let offsetMinutes = 0;
  if (zone !== "Z") {
    const offsetHours = Number(offsetHourText);
    const offsetMinute = Number(offsetMinuteText);
    if (offsetHours > 23 || offsetMinute > 59) return undefined;
    offsetMinutes = (offsetHours * 60 + offsetMinute) * (offsetSign === "+" ? 1 : -1);
  }

  const local = new Date(0);
  local.setUTCFullYear(year, month - 1, day);
  local.setUTCHours(hour, minute, second, 0);
  const utcSeconds = local.getTime() / 1000 - offsetMinutes * 60;
  return `${utcSeconds}:${fractionText.slice(0, 9).padEnd(9, "0")}`;
}

function timeMatches(left: string | undefined, right: string | undefined) {
  const leftInstant = rfc3339Instant(left);
  const rightInstant = rfc3339Instant(right);
  return leftInstant !== undefined && rightInstant !== undefined && leftInstant === rightInstant;
}

function normalizeAnnouncementDraft(input: AnnouncementDraftRequest): AnnouncementDraftRequest {
  return {
    title: input.title.trim(),
    body: input.body.trim(),
    ...(optionalTime(input.startsAt) ? { startsAt: optionalTime(input.startsAt) } : {}),
    ...(optionalTime(input.endsAt) ? { endsAt: optionalTime(input.endsAt) } : {})
  };
}

export function sameAnnouncementDraft(left: AnnouncementDraftRequest, right: AnnouncementDraftRequest) {
  return left.title.trim() === right.title.trim()
    && left.body.trim() === right.body.trim()
    && timeMatches(left.startsAt, right.startsAt)
    && timeMatches(left.endsAt, right.endsAt);
}

export function resolveAnnouncementCreateIntent(
  current: AnnouncementCreateIntent | null,
  input: AnnouncementDraftRequest,
  createKey: () => string
): AnnouncementCreateIntent {
  const normalized = normalizeAnnouncementDraft(input);
  if (current && sameAnnouncementDraft(current.input, normalized)) return current;
  return { input: normalized, idempotencyKey: createKey() };
}

export function resolveAnnouncementPublishIntent(
  current: AnnouncementPublishIntent | null,
  announcement: AnnouncementDTO,
  now: () => string,
  createKey: () => string
): AnnouncementPublishIntent {
  if (current?.announcementId === announcement.id) return current;
  const startsAt = optionalTime(announcement.startsAt) || now();
  const endsAt = optionalTime(announcement.endsAt);
  return {
    announcementId: announcement.id,
    input: { startsAt, ...(endsAt ? { endsAt } : {}) },
    idempotencyKey: createKey()
  };
}

function scheduleMatches(announcement: AnnouncementDTO, schedule: AnnouncementScheduleRequest) {
  return timeMatches(announcement.startsAt, schedule.startsAt)
    && timeMatches(announcement.endsAt, schedule.endsAt);
}

export function createAnnouncementCommandMatches(
  announcement: AnnouncementDTO,
  input: AnnouncementDraftRequest
) {
  return Boolean(announcement.id.trim())
    && announcement.status === "draft"
    && sameAnnouncementDraft(announcement, input);
}

export function publishAnnouncementCommandMatches(
  announcement: AnnouncementDTO,
  announcementId: string,
  schedule: AnnouncementScheduleRequest
) {
  return announcement.id === announcementId
    && (announcement.status === "scheduled" || announcement.status === "published")
    && scheduleMatches(announcement, schedule);
}

export function withdrawAnnouncementCommandMatches(announcement: AnnouncementDTO, announcementId: string) {
  return announcement.id === announcementId && announcement.status === "withdrawn";
}

export function announcementProjectionMatches(expected: AnnouncementDTO, actual: AnnouncementDTO) {
  return actual.id === expected.id
    && actual.title === expected.title
    && actual.body === expected.body
    && actual.status === expected.status
    && timeMatches(actual.startsAt, expected.startsAt)
    && timeMatches(actual.endsAt, expected.endsAt)
    && timeMatches(actual.publishedAt, expected.publishedAt)
    && timeMatches(actual.createdAt, expected.createdAt)
    && timeMatches(actual.updatedAt, expected.updatedAt);
}
