export interface WorkspaceRuntimeImageReplacementIntent {
  readonly workspaceId: string;
  readonly replacementImageDigest: string;
  readonly reason: string;
  readonly idempotencyKey: string;
}

/** Keep the browser-generated mutation key within Control Plane's opaque-ID contract. */
export function workspaceRuntimeImageReplacementIdempotencyKey(createNonce: () => string): string {
  return `wri-${createNonce()}`;
}

export function resolveWorkspaceRuntimeImageReplacementIntent(
  current: WorkspaceRuntimeImageReplacementIntent | null,
  workspaceId: string,
  replacementImageDigest: string,
  reason: string,
  createIdempotencyKey: () => string
): WorkspaceRuntimeImageReplacementIntent {
  if (current?.workspaceId === workspaceId
    && current.replacementImageDigest === replacementImageDigest
    && current.reason === reason) return current;
  return { workspaceId, replacementImageDigest, reason, idempotencyKey: createIdempotencyKey() };
}

export function isTerminalWorkspaceRuntimeImageReplacement(status: string): boolean {
  return status === "succeeded" || status === "failed";
}

export function workspaceRuntimeImageReplacementReadbackMatches(
  readback: { workspaceId: string; runtimeId: string; currentImageDigest: string; targetImageDigest: string; canReplace: boolean },
  workspaceId: string,
  runtimeId: string,
  replacementImageDigest: string
): boolean {
  return readback.workspaceId === workspaceId
    && readback.runtimeId === runtimeId
    && readback.currentImageDigest === replacementImageDigest
    && readback.targetImageDigest === replacementImageDigest
    && readback.canReplace === false;
}
