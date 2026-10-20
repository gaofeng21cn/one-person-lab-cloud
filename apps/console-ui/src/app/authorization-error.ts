// A Console read that the server refused for lack of a live session (401) or a
// missing privilege (403) is an authorization decision, not a dead source. The
// controllers keep that distinction so a non-operator sees "forbidden" instead
// of the synthesized `unavailable` envelope with a backend reason code.
export function isAuthorizationFailure(error: unknown): boolean {
  const status = error && typeof error === "object" && "status" in error
    ? (error as { status?: unknown }).status
    : undefined;
  return status === 401 || status === 403;
}

export function authorizationFailureMessage(error: unknown): string {
  const status = error && typeof error === "object" && "status" in error
    ? (error as { status?: unknown }).status
    : undefined;
  return status === 401 ? "登录已失效，请重新登录" : "没有权限访问运营数据";
}

// Controllers commit this state instead of a synthesized unavailable envelope:
// no source value, no backend reason code, and the server's authorization
// decision as the visible error.
export function authorizationFailureState(error: unknown): {
  readonly value: null;
  readonly loading: false;
  readonly error: string;
} | null {
  if (!isAuthorizationFailure(error)) return null;
  return { value: null, loading: false, error: authorizationFailureMessage(error) };
}
