import { isImmutableDigest, resolveEffectiveBuildDefaults, type EffectiveBuildDefaults, type EffectiveRuntimeRow } from "../app/cloud-webui-model.ts";
import { getJson } from "./console-api.ts";

// Only Capability's bounded signed permit authorizes this data-plane PUT.
// Browser cookies, Cloud sessions and service credentials are never forwarded.
export interface UploadPermit {
  method: string;
  url: string;
  contentType: string;
  requiredChecksumHeaderName: string;
  requiredChecksumHeaderValue: string;
}

export async function uploadPackagePart(permit: UploadPermit, bytes: ArrayBuffer, checksum: string, signal: AbortSignal): Promise<string> {
  const target = new URL(permit.url, window.location.origin);
  if (permit.method !== "PUT" || target.username || target.password || target.hash ||
    (target.protocol !== "https:" && !(target.protocol === "http:" && ["localhost", "127.0.0.1", "[::1]"].includes(target.hostname))) ||
    permit.requiredChecksumHeaderName.toLowerCase() !== "x-opl-sha256" || permit.requiredChecksumHeaderValue !== checksum) {
    throw new Error("上传服务返回了无效的分片许可。");
  }
  const response = await fetch(target.href, { method: "PUT", credentials: "omit", redirect: "error", headers: { "Content-Type": permit.contentType, [permit.requiredChecksumHeaderName]: checksum }, body: bytes, signal });
  const etag = response.headers.get("ETag");
  if (!response.ok || !etag) throw new Error("分片上传未确认；可用同一文件继续上传。");
  return etag;
}

interface OwnerPage<T> { items: T[]; nextCursor?: string }

async function readCatalog(path: string, signal: AbortSignal): Promise<EffectiveRuntimeRow[]> {
  const rows: EffectiveRuntimeRow[] = [];
  let cursor = "";
  for (;;) {
    const page = await getJson<OwnerPage<EffectiveRuntimeRow>>(`${path}?cursor=${encodeURIComponent(cursor)}`, { signal });
    if (!Array.isArray(page.items)) throw new Error("上传目录返回了无效的 Runtime 版本页。");
    rows.push(...page.items);
    cursor = page.nextCursor || "";
    if (!cursor) return rows;
  }
}

/**
 * Read the effective default inputs of a new Agent Build for the signed-in
 * session. The Runtime half is the Runtime Control owner's own projection of its
 * effective policy onto the member-authorized catalog. Both frozen inputs must
 * match that policy and an approved immutable catalog row; unreadable, withdrawn
 * or inconsistent inputs keep the Build command unavailable.
 */
export async function readEffectiveBuildDefaults(signal: AbortSignal): Promise<EffectiveBuildDefaults> {
  const [runtimes, webuis, policy] = await Promise.all([
    readCatalog("/api/v2/catalog/runtime-versions", signal),
    readCatalog("/api/v2/catalog/webui-versions", signal),
    getJson<{ runtimeVersionId?: string; defaultWebuiVersionId?: string }>("/api/v2/catalog/build-policy", { signal })
  ]);
  const matches = webuis.filter(row => row.id === policy.defaultWebuiVersionId);
  const row = matches.length === 1 ? matches[0] : undefined;
  const webui = row?.status === "approved" && isImmutableDigest(row.artifactDigest)
    ? { id: row.id, name: row.name, versionLabel: row.versionLabel, artifactDigest: row.artifactDigest! }
    : null;
  const result = resolveEffectiveBuildDefaults(runtimes, webui);
  if (result.runtime && result.runtime.id !== policy.runtimeVersionId) {
    result.runtime = undefined;
    result.reasons.push("effective_default_runtime_policy_mismatch");
  }
  return result;
}
