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
