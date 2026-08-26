import type {
  CreateSupportTicketMappingRequest,
  SupportTicketMappingDTO,
  SupportTicketPageDTO
} from "../api/dtos.ts";

export interface SupportMappingIntent {
  readonly input: CreateSupportTicketMappingRequest;
  readonly idempotencyKey: string;
}

function optionalFieldMatches(expected: string | undefined, actual: string | undefined): boolean {
  return expected === undefined || expected === actual;
}

function resourceIdsMatch(expected: string[] | undefined, actual: string[]): boolean {
  return expected === undefined || expected.length === actual.length
    && expected.every((resourceId, index) => resourceId === actual[index]);
}

export function supportMappingInputEqual(
  left: CreateSupportTicketMappingRequest,
  right: CreateSupportTicketMappingRequest
): boolean {
  return left.accountId === right.accountId
    && left.externalTicketId === right.externalTicketId
    && left.title === right.title
    && optionalFieldMatches(left.externalSystem, right.externalSystem)
    && optionalFieldMatches(right.externalSystem, left.externalSystem)
    && optionalFieldMatches(left.externalUrl, right.externalUrl)
    && optionalFieldMatches(right.externalUrl, left.externalUrl)
    && optionalFieldMatches(left.description, right.description)
    && optionalFieldMatches(right.description, left.description)
    && optionalFieldMatches(left.workspaceId, right.workspaceId)
    && optionalFieldMatches(right.workspaceId, left.workspaceId)
    && optionalFieldMatches(left.operationId, right.operationId)
    && optionalFieldMatches(right.operationId, left.operationId)
    && resourceIdsMatch(left.resourceIds, right.resourceIds)
    && resourceIdsMatch(right.resourceIds, left.resourceIds);
}

export function resolveSupportMappingIntent(
  current: SupportMappingIntent | null,
  input: CreateSupportTicketMappingRequest,
  createIdempotencyKey: () => string
): SupportMappingIntent {
  if (current && supportMappingInputEqual(current.input, input)) return current;
  return { input, idempotencyKey: createIdempotencyKey() };
}

export function supportMappingResponseMatches(
  response: SupportTicketMappingDTO,
  input: CreateSupportTicketMappingRequest
): boolean {
  return response.id.trim() !== ""
    && response.accountId === input.accountId
    && response.externalTicketId === input.externalTicketId
    && response.title === input.title
    && optionalFieldMatches(input.externalSystem, response.externalSystem)
    && optionalFieldMatches(input.externalUrl, response.externalUrl)
    && optionalFieldMatches(input.workspaceId, response.workspaceId)
    && optionalFieldMatches(input.operationId, response.operationId)
    && resourceIdsMatch(input.resourceIds, response.resourceIds);
}

export function supportMappingReadbackMatches(
  page: SupportTicketPageDTO,
  input: CreateSupportTicketMappingRequest
): boolean {
  return page.tickets.some((ticket) => supportMappingResponseMatches(ticket, input));
}
