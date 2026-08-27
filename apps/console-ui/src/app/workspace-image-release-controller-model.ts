export interface WorkspaceImageReleaseActivationIntent {
  readonly releaseVersion: string;
  readonly expectedRevision: number;
  readonly reason: string;
  readonly idempotencyKey: string;
}

export function workspaceImageReleaseActivationIdempotencyKey(createNonce: () => string): string {
  return `wira-${createNonce()}`;
}

export function resolveWorkspaceImageReleaseActivationIntent(
  current: WorkspaceImageReleaseActivationIntent | null,
  releaseVersion: string,
  expectedRevision: number,
  reason: string,
  createIdempotencyKey: () => string
): WorkspaceImageReleaseActivationIntent {
  if (current?.releaseVersion === releaseVersion
    && current.expectedRevision === expectedRevision
    && current.reason === reason) return current;
  return { releaseVersion, expectedRevision, reason, idempotencyKey: createIdempotencyKey() };
}

export function workspaceImageReleaseActivationMatches(
  policy: { revision: number; active: { version: string } },
  intent: WorkspaceImageReleaseActivationIntent
): boolean {
  return policy.active.version === intent.releaseVersion
    && (policy.revision === intent.expectedRevision || policy.revision === intent.expectedRevision + 1);
}
