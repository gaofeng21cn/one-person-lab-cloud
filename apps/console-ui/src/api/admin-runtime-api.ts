import { isImmutableDigest } from "../app/cloud-webui-model.ts";
import { getJson, postJson } from "./console-api.ts";

// Projections of the existing Runtime Control public JSON contract. The owner
// validates the complete publisher contract against its approved schema.
export type RuntimePublisherContract = Record<string, unknown> & {
  publisherNamespaceId: string;
  image: { repository: string; digest: string } & Record<string, unknown>;
};

export interface RuntimeAdmission {
  name: string;
  versionLabel: string;
  publisherNamespaceId: string;
  admissionReceiptId: string;
  publisherContract: RuntimePublisherContract;
}

export interface AdminRuntimeVersion extends RuntimeAdmission {
  id: string;
  artifactDigest: string;
  status: "approved" | "deprecated" | "revoked";
  runtimeAbiVersion: string;
  defaultForNewBuilds?: boolean;
  publisherContractDigest: string;
  publisherContractObjectRef: string;
  createdAt: string;
}

export interface RuntimeAdmissionDraft {
  name: string;
  versionLabel: string;
  publisherNamespaceId: string;
  admissionReceiptId: string;
  publisherContractJson: string;
}

function object(value: unknown): value is Record<string, unknown> {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}

function publisherContract(value: unknown): value is RuntimePublisherContract {
  return object(value) && typeof value.publisherNamespaceId === "string"
    && object(value.image) && typeof value.image.repository === "string" && value.image.repository.length > 0
    && typeof value.image.digest === "string" && isImmutableDigest(value.image.digest);
}

export function parseRuntimeAdmission(draft: RuntimeAdmissionDraft): RuntimeAdmission {
  const name = /^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$/;
  if (!name.test(draft.name) || !name.test(draft.versionLabel)) {
    throw new Error("名称和版本需以字母或数字开头，仅含字母、数字、点、下划线或连字符，最长 128 字符。");
  }
  if (!draft.publisherNamespaceId.trim() || !draft.admissionReceiptId.trim()) {
    throw new Error("请填写已批准的发布者命名空间 ID 和准入回执 ID。");
  }
  let contract: unknown;
  try { contract = JSON.parse(draft.publisherContractJson); }
  catch { throw new Error("发布契约不是有效 JSON，请粘贴发布者提供的完整 RuntimePublisherContract。"); }
  if (!publisherContract(contract) || contract.publisherNamespaceId !== draft.publisherNamespaceId) {
    throw new Error("请核对契约中的 publisherNamespaceId、image.repository 和不可变 sha256 digest；命名空间须与表单一致。");
  }
  return {
    name: draft.name, versionLabel: draft.versionLabel,
    publisherNamespaceId: draft.publisherNamespaceId, admissionReceiptId: draft.admissionReceiptId,
    publisherContract: contract
  };
}

function decodeRuntimeVersion(value: unknown): AdminRuntimeVersion {
  if (!object(value) || !["id", "name", "versionLabel", "runtimeAbiVersion", "admissionReceiptId", "publisherNamespaceId", "publisherContractObjectRef", "createdAt"]
    .every(key => typeof value[key] === "string" && value[key] !== "")
    || !["approved", "deprecated", "revoked"].includes(String(value.status))
    || typeof value.artifactDigest !== "string" || !isImmutableDigest(value.artifactDigest)
    || typeof value.publisherContractDigest !== "string" || !isImmutableDigest(value.publisherContractDigest)
    || (value.defaultForNewBuilds !== undefined && typeof value.defaultForNewBuilds !== "boolean")
    || !publisherContract(value.publisherContract) || value.publisherContract.image.digest !== value.artifactDigest
    || value.publisherContract.publisherNamespaceId !== value.publisherNamespaceId) {
    throw new Error("Runtime Control 返回的版本事实不完整或不一致，请刷新目录并核查服务读回。");
  }
  return value as unknown as AdminRuntimeVersion;
}

export async function listAdminRuntimeVersions(signal: AbortSignal): Promise<AdminRuntimeVersion[]> {
  const versions: AdminRuntimeVersion[] = [];
  const cursors = new Set<string>();
  const ids = new Set<string>();
  let cursor = "";
  for (;;) {
    cursors.add(cursor);
    const page = await getJson<unknown>(`/api/v2/catalog/runtime-versions?cursor=${encodeURIComponent(cursor)}`, { signal });
    if (!object(page) || !Array.isArray(page.items) || (page.nextCursor !== undefined && typeof page.nextCursor !== "string")) {
      throw new Error("Runtime Control 返回了无效版本页，请刷新目录并核查服务读回。");
    }
    for (const item of page.items) {
      const version = decodeRuntimeVersion(item);
      if (ids.has(version.id)) throw new Error("Runtime 目录出现重复版本 ID，请刷新目录并核查服务读回。");
      ids.add(version.id);
      versions.push(version);
    }
    if (typeof page.nextCursor !== "string" || !page.nextCursor) return versions;
    cursor = page.nextCursor;
    if (cursors.has(cursor)) throw new Error("Runtime 目录分页游标重复，请核查服务分页后重试。");
  }
}

export async function admitAdminRuntime(input: RuntimeAdmission, csrfToken: string, key: string, signal: AbortSignal): Promise<AdminRuntimeVersion> {
  const version = decodeRuntimeVersion(await postJson<unknown>("/api/v2/admin/catalog/runtime-versions", input, csrfToken, key, 10_000, signal));
  if (!admissionMatches(version, input)) throw new Error("准入响应与提交的不可变版本不一致，请核查目录后重试同一请求。");
  return version;
}

function admissionMatches(version: AdminRuntimeVersion, input: RuntimeAdmission): boolean {
  return version.name === input.name && version.versionLabel === input.versionLabel
    && version.publisherNamespaceId === input.publisherNamespaceId && version.admissionReceiptId === input.admissionReceiptId
    && version.artifactDigest === input.publisherContract.image.digest
    && version.publisherContract.image.repository === input.publisherContract.image.repository;
}

export function confirmRuntimeAdmission(versions: AdminRuntimeVersion[], admitted: AdminRuntimeVersion): AdminRuntimeVersion {
  const readback = versions.find(version => version.id === admitted.id);
  if (!readback || !admissionMatches(readback, admitted) || readback.publisherContractDigest !== admitted.publisherContractDigest
    || readback.publisherContractObjectRef !== admitted.publisherContractObjectRef) {
    throw new Error("新版本尚未从 Runtime 目录确认，请重试读回；不要另建版本重复准入。");
  }
  return readback;
}
