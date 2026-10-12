import { resolveEffectiveBuildDefaults, type EffectiveBuildDefaults, type EffectiveInput, type EffectiveRuntimeRow } from "../app/cloud-webui-model.ts";
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

async function readRuntimeCatalog(signal: AbortSignal): Promise<EffectiveRuntimeRow[]> {
  const rows: EffectiveRuntimeRow[] = [];
  let cursor = "";
  for (;;) {
    const page = await getJson<OwnerPage<EffectiveRuntimeRow>>(`/api/v2/catalog/runtime-versions?cursor=${encodeURIComponent(cursor)}`, { signal });
    if (!Array.isArray(page.items)) throw new Error("上传目录返回了无效的 Runtime 版本页。");
    rows.push(...page.items);
    cursor = page.nextCursor || "";
    if (!cursor) return rows;
  }
}

/**
 * Read the effective default inputs of a new Agent Build for the signed-in
 * session. The Runtime half is the Runtime Control owner's own projection of its
 * effective policy onto the member-authorized catalog. The effective default WebUI
 * is the same policy's other half: the member-authorized effective-policy read
 * names it, and this reader then looks that exact row up in the member-authorized
 * WebUI catalog to freeze an immutable digest beside it. Nothing here chooses a
 * default — a policy the Console cannot read back leaves the WebUI half unresolved,
 * and the caller must present that missing fact and keep the Build command
 * unavailable rather than substituting some other approved catalog row.
 */
export async function readEffectiveBuildDefaults(signal: AbortSignal): Promise<EffectiveBuildDefaults> {
  const runtimeRows = await readRuntimeCatalog(signal);
  return resolveEffectiveBuildDefaults(runtimeRows, await readEffectiveDefaultWebui(signal));
}

/**
 * Resolve the owner-named default WebUI, or null when the platform has published
 * no policy, names none, or names one this session cannot read back. A lookup miss
 * is the same missing fact as an absent policy: neither may be filled in from the
 * catalog, because only the owner's policy decides the default.
 */
async function readEffectiveDefaultWebui(signal: AbortSignal): Promise<EffectiveInput | null> {
  const policy = await getJson<{ defaultWebuiVersionId?: string }>(`/api/v2/catalog/build-policy`, { signal });
  const id = typeof policy.defaultWebuiVersionId === "string" ? policy.defaultWebuiVersionId.trim() : "";
  if (!id) return null;
  let cursor = "";
  for (;;) {
    const page = await getJson<OwnerPage<EffectiveInput>>(`/api/v2/catalog/webui-versions?cursor=${encodeURIComponent(cursor)}`, { signal });
    if (!Array.isArray(page.items)) throw new Error("上传目录返回了无效的界面版本页。");
    const named = page.items.find((row) => row.id === id);
    if (named) return named;
    cursor = page.nextCursor || "";
    if (!cursor) return null;
  }
}
