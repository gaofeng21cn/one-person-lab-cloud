import { resolveEffectiveBuildDefaults, type EffectiveBuildDefaults, type EffectiveRuntimeRow } from "../app/cloud-webui-model.ts";
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
 * effective policy onto the member-authorized catalog. The effective default
 * WebUI is the same policy's other half, and no route serves it to a customer
 * session yet, so this reader passes null for it: the caller must present the
 * missing fact and keep the Build command unavailable instead of substituting
 * an approved catalog row.
 */
export async function readEffectiveBuildDefaults(signal: AbortSignal): Promise<EffectiveBuildDefaults> {
  return resolveEffectiveBuildDefaults(await readRuntimeCatalog(signal), null);
}
