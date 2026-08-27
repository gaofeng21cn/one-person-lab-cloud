import type {
  AnnouncementPageDTO,
  AnnouncementReadDTO
} from "../api/dtos.ts";

export interface AnnouncementReadIntent {
  announcementId: string;
  idempotencyKey: string;
}

const rfc3339Pattern = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(?:\.\d+)?(?:Z|([+-])(\d{2}):(\d{2}))$/;

function validRfc3339(value: string) {
  const match = rfc3339Pattern.exec(value);
  if (!match) return false;

  const [, yearText, monthText, dayText, hourText, minuteText, secondText, , offsetHourText, offsetMinuteText] = match;
  const year = Number(yearText);
  const month = Number(monthText);
  const day = Number(dayText);
  const leapYear = year % 4 === 0 && (year % 100 !== 0 || year % 400 === 0);
  const daysInMonth = [31, leapYear ? 29 : 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31];

  return month >= 1
    && month <= 12
    && day >= 1
    && day <= daysInMonth[month - 1]
    && Number(hourText) <= 23
    && Number(minuteText) <= 59
    && Number(secondText) <= 59
    && (!offsetHourText || Number(offsetHourText) <= 23)
    && (!offsetMinuteText || Number(offsetMinuteText) <= 59);
}

export function resolveAnnouncementReadIntent(
  current: AnnouncementReadIntent | null,
  announcementId: string,
  createKey: () => string
): AnnouncementReadIntent {
  if (current?.announcementId === announcementId) return current;
  return { announcementId, idempotencyKey: createKey() };
}

export function announcementReadReceiptMatches(receipt: AnnouncementReadDTO, announcementId: string) {
  return receipt.announcementId === announcementId && validRfc3339(receipt.readAt);
}

export function announcementReadbackMatches(page: AnnouncementPageDTO, announcementId: string) {
  const announcement = page.items.find((candidate) => candidate.id === announcementId);
  return !announcement || announcement.read;
}

export function announcementReadbackPreservesReceipts(
  page: AnnouncementPageDTO,
  confirmedAnnouncementIds: Iterable<string>
) {
  for (const announcementId of confirmedAnnouncementIds) {
    if (!announcementReadbackMatches(page, announcementId)) return false;
  }
  return true;
}
