import { useEffect, useRef, useState } from "react";
import { getJson, postJson } from "../api/console-api.ts";
import { uploadPackagePart, type UploadPermit } from "../api/publisher-api.ts";
import { isImmutableDigest } from "../app/cloud-webui-model.ts";
import { Button } from "../components/ui/index.ts";
import "./publisher.css";

type Session = { actorId: string; tenantId?: string; csrfToken: string };
type Entry = { id: string; name: string; versionLabel?: string; status?: string; artifactDigest?: string; admissionReceiptId?: string; publisherContractDigest?: string; publisherContractObjectRef?: string };
type Part = { partNumber: number; etag: string; sizeBytes: number; sha256: string };
type Upload = { id: string; packageVersionId: string; partSizeBytes: number; completedParts: Part[] };
type UploadWire = { id: string; packageVersionId: string; partSizeBytes: string; completedParts: Array<{ partNumber: number; etag: string; sizeBytes: string; sha256: string }> };
type Build = { id: string; status: string; stage: string; artifactDigest?: string; resultCapabilityVersionId?: string };
type Version = { id: string; status: string; artifactDigest: string; deploymentDescriptorDigest: string };
type PackageVersion = { id: string; status?: string; versionLabel?: string; sha256?: string; sizeBytes?: number };
type Selection = { namespaceId: string; namespaceName: string; packageId: string; packageName: string; versionLabel: string; runtimeId: string; webuiId: string };
type Work = { selection?: Selection; fingerprint: string; key: string; packageId?: string; uploadId?: string; packageVersionId?: string; buildId?: string };

const base = "/api/v2";
const stageLabel: Record<string, string> = { queued: "等待构建", validating: "校验输入", building: "正在构建", pushing: "上传镜像", registering: "登记版本", succeeded: "构建完成", failed: "构建失败", needs_attention: "结果待确认", cancelled: "已取消" };
async function sha256(bytes: ArrayBuffer) {
  const hash = await crypto.subtle.digest("SHA-256", bytes);
  return "sha256:" + Array.from(new Uint8Array(hash), (v) => v.toString(16).padStart(2, "0")).join("");
}
function decodeUpload(value: UploadWire): Upload {
  const partSizeBytes = Number(value.partSizeBytes);
  if (!Number.isSafeInteger(partSizeBytes) || partSizeBytes <= 0) throw new Error("上传服务返回了无效的分片大小。");
  const completedParts = value.completedParts.map((part) => {
    const sizeBytes = Number(part.sizeBytes);
    if (!Number.isSafeInteger(sizeBytes) || sizeBytes <= 0) throw new Error("上传服务返回了无效的分片读回。");
    return { ...part, sizeBytes };
  });
  return { ...value, partSizeBytes, completedParts };
}
function delay(signal: AbortSignal) {
  return new Promise<void>((resolve, reject) => {
    const abort = () => { clearTimeout(timer); reject(new DOMException("Aborted", "AbortError")); };
    const timer = setTimeout(() => { signal.removeEventListener("abort", abort); resolve(); }, 1500);
    signal.addEventListener("abort", abort, { once: true });
    if (signal.aborted) abort();
  });
}
async function pages(path: string, signal: AbortSignal): Promise<Entry[]> {
  const entries: Entry[] = [];
  let cursor = "";
  do {
    const page = await getJson<{ items: Entry[]; nextCursor?: string }>(`${path}${path.includes("?") ? "&" : "?"}cursor=${encodeURIComponent(cursor)}`, { signal });
    entries.push(...page.items);
    cursor = page.nextCursor || "";
  } while (cursor);
  return entries;
}

export function PublisherPage() {
  const [session, setSession] = useState<Session | null>(null);
  const [namespaces, setNamespaces] = useState<Entry[]>([]);
  const [packages, setPackages] = useState<Entry[]>([]);
  const [webuis, setWebuis] = useState<Entry[]>([]);
  const [runtimes, setRuntimes] = useState<Entry[]>([]);
  const [namespaceId, setNamespaceId] = useState("");
  const [namespaceName, setNamespaceName] = useState("");
  const [packageId, setPackageId] = useState("");
  const [packageName, setPackageName] = useState("");
  const [versionLabel, setVersionLabel] = useState("");
  const [webuiId, setWebuiId] = useState("");
  const [runtimeId, setRuntimeId] = useState("");
  const [file, setFile] = useState<File | null>(null);
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState("正在读取发布权限和目录…");
  const [error, setError] = useState("");
  const [build, setBuild] = useState<Build | null>(null);
  const [version, setVersion] = useState<Version | null>(null);
  const [packageVersion, setPackageVersion] = useState<PackageVersion | null>(null);
  const work = useRef<Work | null>(null);
  const execution = useRef<AbortController | null>(null);
  const storageKey = (s: Session) => `opl-publisher:${s.actorId}:${s.tenantId || "platform"}`;
  const save = (s: Session, value: Work) => {
    work.current = value;
    // Recovery metadata only: no ZIP contents, signed permits or session tokens.
    sessionStorage.setItem(storageKey(s), JSON.stringify(value));
  };

  useEffect(() => {
    const abort = new AbortController();
    void (async () => {
      try {
        const active = await getJson<Session>(`${base}/auth/session`, { signal: abort.signal });
        const [ns, ui, runtimeRows] = await Promise.all([
          pages(`${base}/namespaces`, abort.signal),
          pages(`${base}/catalog/webui-versions`, abort.signal),
          pages(`${base}/catalog/runtime-versions`, abort.signal)
        ]);
        if (abort.signal.aborted) return;
        setSession(active); setNamespaces(ns); setWebuis(ui.filter((v) => v.status === "approved"));
        setRuntimes(runtimeRows.filter((v) => v.status === "approved" && isImmutableDigest(v.artifactDigest)));
        setNamespaceId(ns[0]?.id || ""); setWebuiId(ui.find((v) => v.status === "approved")?.id || "");
        setRuntimeId(runtimeRows.find((v) => v.status === "approved" && isImmutableDigest(v.artifactDigest))?.id || "");
        try { work.current = JSON.parse(sessionStorage.getItem(storageKey(active)) || "null") as Work | null; } catch { work.current = null; }
        const selected = work.current?.selection;
        if (selected) {
          setNamespaceId(selected.namespaceId); setNamespaceName(selected.namespaceName);
          setPackageId(selected.packageId); setPackageName(selected.packageName);
          setVersionLabel(selected.versionLabel); setRuntimeId(selected.runtimeId); setWebuiId(selected.webuiId);
        }
        if (work.current?.buildId) {
          const job = await getJson<Build>(`${base}/builds/${encodeURIComponent(work.current.buildId)}`, { signal: abort.signal });
          if (!abort.signal.aborted) setBuild(job);
        }
        if (work.current?.packageVersionId) {
          const readback = await getJson<PackageVersion>(`${base}/package-versions/${encodeURIComponent(work.current.packageVersionId)}`, { signal: abort.signal });
          if (!abort.signal.aborted) setPackageVersion(readback);
        }
        if (!abort.signal.aborted) setMessage("选择 Package、精确 Runtime Release 与 Agent WebUI 版本，构建不可变版本。");
      } catch (e) { if (!abort.signal.aborted) setError(e instanceof Error ? e.message : "发布目录读取失败"); }
    })();
    return () => { abort.abort(); execution.current?.abort(); };
  }, []);

  useEffect(() => {
    const abort = new AbortController(); setPackages([]); setPackageId(work.current?.selection?.namespaceId === namespaceId ? work.current.selection.packageId : "");
    if (namespaceId) void pages(`${base}/packages?namespaceId=${encodeURIComponent(namespaceId)}`, abort.signal).then((items) => { if (!abort.signal.aborted) setPackages(items); }).catch((e: unknown) => { if (!abort.signal.aborted) setError(e instanceof Error ? e.message : "Package 列表读取失败"); });
    return () => abort.abort();
  }, [namespaceId]);

  async function readBuild(id: string, signal: AbortSignal) {
    for (;;) {
      const job = await getJson<Build>(`${base}/builds/${encodeURIComponent(id)}`, { signal });
      if (signal.aborted) return;
      setBuild(job); setMessage(stageLabel[job.stage] || "正在读取构建状态");
      if (job.status === "succeeded" && job.resultCapabilityVersionId) {
        const result = await getJson<Version>(`${base}/capability-versions/${encodeURIComponent(job.resultCapabilityVersionId)}`, { signal });
        if (result.status !== "ready" || result.artifactDigest !== job.artifactDigest) throw new Error("构建与版本回读尚未一致，请刷新状态。");
        if (!signal.aborted) { setVersion(result); setMessage("版本已就绪，镜像摘要与构建记录一致。"); }
        return;
      }
      if (["failed", "needs_attention", "cancelled"].includes(job.status)) return;
      await delay(signal);
    }
  }

  async function run(resumeBuild = false) {
    if (!session || busy) return;
    const abort = new AbortController(); execution.current?.abort(); execution.current = abort;
    setBusy(true); setError("");
    try {
      const active = await getJson<Session>(`${base}/auth/session`, { signal: abort.signal });
      if (storageKey(active) !== storageKey(session)) throw new Error("会话已切换，请重新打开发布页面。");
      if (resumeBuild && work.current?.buildId) { await readBuild(work.current.buildId, abort.signal); return; }
      if (!file || !versionLabel || !runtimeId || !webuiId || (!namespaceId && !namespaceName) || (!packageId && !packageName)) throw new Error("请填写发布信息，选择精确 Runtime/WebUI 版本并选择 Package 文件。");
      setMessage("正在校验 ZIP 文件…");
      const bytes = await file.arrayBuffer(); const hash = await sha256(bytes);
      const fingerprint = JSON.stringify([namespaceId, namespaceName, packageId, packageName, versionLabel, runtimeId, webuiId, hash]);
      let current = work.current;
      if (!current || current.fingerprint !== fingerprint) { current = { fingerprint, key: crypto.randomUUID(), selection: { namespaceId, namespaceName, packageId, packageName, versionLabel, runtimeId, webuiId } }; setBuild(null); setVersion(null); }
      save(active, current);
      const command = <T,>(path: string, body: unknown, key: string) => postJson<T>(base + path, body, active.csrfToken, `${current.key}:${key}`, 30_000, abort.signal);
      if (!current.packageId) {
        const ns = namespaceId || (await command<Entry>("/namespaces", { name: namespaceName }, "namespace")).id;
        current.packageId = packageId || (await command<Entry>("/packages", { namespaceId: ns, name: packageName, description: "" }, "package")).id;
        save(active, current);
      }
      let upload: Upload;
      if (current.uploadId) upload = decodeUpload(await getJson<UploadWire>(`${base}/uploads/${encodeURIComponent(current.uploadId)}`, { signal: abort.signal }));
      else {
        upload = decodeUpload(await command<UploadWire>(`/packages/${encodeURIComponent(current.packageId)}/uploads`, { versionLabel, fileName: file.name, sizeBytes: String(file.size), sha256: hash }, "upload"));
        current.uploadId = upload.id; current.packageVersionId = upload.packageVersionId; save(active, current);
      }
      const parts: Part[] = [];
      if (!(upload.partSizeBytes > 0)) throw new Error("上传服务没有返回有效分片大小。");
      for (let offset = 0, number = 1; offset < bytes.byteLength; offset += upload.partSizeBytes, number++) {
        const chunk = bytes.slice(offset, Math.min(bytes.byteLength, offset + upload.partSizeBytes));
        const checksum = await sha256(chunk);
        const done = upload.completedParts.find((p) => p.partNumber === number && p.sha256 === checksum && p.sizeBytes === chunk.byteLength);
        if (done) { parts.push(done); continue; }
        setMessage(`正在上传分片 ${number}…`);
        const permit = await command<UploadPermit>(`/uploads/${encodeURIComponent(upload.id)}/parts`, { partNumber: number, sizeBytes: String(chunk.byteLength), sha256: checksum }, `part-${number}`);
        const etag = await uploadPackagePart(permit, chunk, checksum, abort.signal);
        parts.push({ partNumber: number, etag, sizeBytes: chunk.byteLength, sha256: checksum });
      }
      await command(`/uploads/${encodeURIComponent(upload.id)}/complete`, { parts: parts.map((part) => ({ ...part, sizeBytes: String(part.sizeBytes) })) }, "complete");
      const job = await command<Build>("/builds", { packageVersionId: upload.packageVersionId, runtimeVersionId: runtimeId, webuiVersionId: webuiId }, "build");
      current.buildId = job.id; save(active, current); await readBuild(job.id, abort.signal);
      const readback = await getJson<PackageVersion>(`${base}/package-versions/${encodeURIComponent(upload.packageVersionId)}`, { signal: abort.signal });
      if (!abort.signal.aborted) setPackageVersion(readback);
    } catch (e) { if (!abort.signal.aborted) setError(e instanceof Error ? e.message : "发布失败，可重试读取或继续上传。"); }
    finally { if (!abort.signal.aborted) setBusy(false); }
  }

  const selectableWebuis = webuis.filter((item) => item.status === "approved" && isImmutableDigest(item.artifactDigest));
  const selectableRuntimes = runtimes.filter((item) => item.status === "approved" && isImmutableDigest(item.artifactDigest));
  return <section className="panel publisher-page">
    <div className="panel-title"><div><h2>Cloud WebUI / Agent Package</h2><p>读取 Package，固定已批准 Runtime Release 与 Agent WebUI 版本，提交 Build 并回读 owner 结果。</p></div><span className="publisher-draft-badge">Draft / fixture-aware</span></div>
    <div className="publisher-boundary" role="note"><strong>Fail-closed boundary</strong><span>Quote/Workspace/Deploy readback 与 Secret/model owner API 尚未在当前 BFF 契约中提供；Build 只接受 owner 读回的精确获批输入。</span></div>
    <form onSubmit={(e) => { e.preventDefault(); void run(); }}>
      <fieldset disabled={busy || !session}>
        <label>命名空间<select aria-label="命名空间" value={namespaceId} onChange={(e) => setNamespaceId(e.target.value)}><option value="">新建命名空间</option>{namespaces.map((v) => <option key={v.id} value={v.id}>{v.name}</option>)}</select></label>
        {!namespaceId && <label>命名空间名称<input value={namespaceName} onChange={(e) => setNamespaceName(e.target.value)} required /></label>}
        <label>Package<select aria-label="Package" value={packageId} onChange={(e) => setPackageId(e.target.value)}><option value="">新建 Package</option>{packages.map((v) => <option key={v.id} value={v.id}>{v.name}</option>)}</select></label>
        {!packageId && <label>Package 名称<input value={packageName} onChange={(e) => setPackageName(e.target.value)} required /></label>}
        <label>版本名称<input value={versionLabel} onChange={(e) => setVersionLabel(e.target.value)} placeholder="例如 0.1.0" required /></label>
        <label>Runtime Release<select aria-label="Runtime Release" value={runtimeId} onChange={(e) => setRuntimeId(e.target.value)} required><option value="">选择已批准且 digest 固定的 Runtime</option>{runtimes.map((v) => <option key={v.id} value={v.id}>{v.name} · {v.versionLabel} · {v.artifactDigest}</option>)}</select></label>
        <label>WebUI<select aria-label="WebUI" value={webuiId} onChange={(e) => setWebuiId(e.target.value)} required><option value="">选择已批准且 digest 固定的 WebUI</option>{webuis.map((v) => <option disabled={v.status !== "approved" || !isImmutableDigest(v.artifactDigest)} key={v.id} value={v.id}>{v.name} · {v.versionLabel} · {v.status === "approved" && isImmutableDigest(v.artifactDigest) ? "可选" : "拒绝"}</option>)}</select></label>
        <div className="publisher-selection-readback" aria-label="WebUI owner readback"><span>artifact digest：<code>{webuis.find((v) => v.id === webuiId)?.artifactDigest || "暂不可用"}</code></span><span>Runtime catalog：<code>{(runtimeId ? runtimes.find((v) => v.id === runtimeId)?.artifactDigest : "请选择精确版本") || "unavailable"}</code></span><span>admission receipt：<code>{webuis.find((v) => v.id === webuiId)?.admissionReceiptId || "暂不可用"}</code></span></div>
        <label>ZIP 文件<input type="file" accept=".zip,application/zip" onChange={(e) => setFile(e.target.files?.[0] || null)} required /></label>
        <Button type="submit" disabled={!selectableWebuis.length || !selectableRuntimes.length}>上传并构建 / 继续上传</Button>
      </fieldset>
    </form>
    <p role="status">{message}</p>
    {error && <p role="alert">{error}</p>}
    {packageVersion && <section aria-label="Package 版本读回"><h3>Package owner readback</h3><dl><dt>Package Version</dt><dd><code>{packageVersion.id}</code></dd><dt>状态</dt><dd>{packageVersion.status || "暂不可用"}</dd><dt>Package bytes sha256</dt><dd><code>{packageVersion.sha256 || "暂不可用"}</code></dd></dl></section>}
    {build && <section aria-label="构建结果"><dl><dt>构建</dt><dd>{build.id}</dd><dt>状态</dt><dd>{stageLabel[build.status] || build.status}</dd><dt>Package ref</dt><dd><code>{work.current?.packageVersionId || "暂不可用"}</code></dd><dt>Runtime Release ref</dt><dd><code>{runtimeId || "暂不可用"}</code></dd><dt>WebUI ref</dt><dd><code>{webuiId || "暂不可用"}</code></dd>{build.artifactDigest && <><dt>OCI digest（Build owner）</dt><dd><code>{build.artifactDigest}</code></dd></>}</dl><Button disabled={busy} variant="outline" onClick={() => void run(true)}>刷新构建状态</Button></section>}
    {version && <section aria-label="已就绪版本"><h3>版本已就绪</h3><p>{version.id}</p><p>版本与构建结果一致</p><code>{version.deploymentDescriptorDigest}</code></section>}
  </section>;
}
