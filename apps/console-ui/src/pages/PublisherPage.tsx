import { useEffect, useRef, useState } from "react";
import { getJson, postJson } from "../api/console-api.ts";
import { uploadPackagePart, type UploadPermit } from "../api/publisher-api.ts";
import { Button } from "../components/ui/index.ts";
import "./publisher.css";

type Session = { actorId: string; tenantId?: string; csrfToken: string };
type Entry = { id: string; name: string; versionLabel?: string; status?: string };
type Part = { partNumber: number; etag: string; sizeBytes: number; sha256: string };
type Upload = { id: string; packageVersionId: string; partSizeBytes: number; completedParts: Part[] };
type Build = { id: string; status: string; stage: string; artifactDigest?: string; resultCapabilityVersionId?: string };
type Version = { id: string; status: string; artifactDigest: string; deploymentDescriptorDigest: string };
type Selection = { namespaceId: string; namespaceName: string; packageId: string; packageName: string; versionLabel: string; webuiId: string };
type Work = { selection?: Selection; fingerprint: string; key: string; packageId?: string; uploadId?: string; packageVersionId?: string; buildId?: string };

const base = "/api/v2";
const stageLabel: Record<string, string> = { queued: "等待构建", validating: "校验输入", building: "正在构建", pushing: "上传镜像", registering: "登记版本", succeeded: "构建完成", failed: "构建失败", needs_attention: "结果待确认", cancelled: "已取消" };
async function sha256(bytes: ArrayBuffer) {
  const hash = await crypto.subtle.digest("SHA-256", bytes);
  return "sha256:" + Array.from(new Uint8Array(hash), (v) => v.toString(16).padStart(2, "0")).join("");
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
  const [namespaceId, setNamespaceId] = useState("");
  const [namespaceName, setNamespaceName] = useState("");
  const [packageId, setPackageId] = useState("");
  const [packageName, setPackageName] = useState("");
  const [versionLabel, setVersionLabel] = useState("");
  const [webuiId, setWebuiId] = useState("");
  const [file, setFile] = useState<File | null>(null);
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState("正在读取发布权限和目录…");
  const [error, setError] = useState("");
  const [build, setBuild] = useState<Build | null>(null);
  const [version, setVersion] = useState<Version | null>(null);
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
        const active = await getJson<Session>(`${base}/session`, { signal: abort.signal });
        const [ns, ui] = await Promise.all([pages(`${base}/namespaces`, abort.signal), pages(`${base}/catalog/webui-versions`, abort.signal)]);
        if (abort.signal.aborted) return;
        setSession(active); setNamespaces(ns); setWebuis(ui.filter((v) => v.status === "approved"));
        setNamespaceId(ns[0]?.id || ""); setWebuiId(ui.find((v) => v.status === "approved")?.id || "");
        try { work.current = JSON.parse(sessionStorage.getItem(storageKey(active)) || "null") as Work | null; } catch { work.current = null; }
        const selected = work.current?.selection;
        if (selected) {
          setNamespaceId(selected.namespaceId); setNamespaceName(selected.namespaceName);
          setPackageId(selected.packageId); setPackageName(selected.packageName);
          setVersionLabel(selected.versionLabel); setWebuiId(selected.webuiId);
        }
        if (work.current?.buildId) {
          const job = await getJson<Build>(`${base}/builds/${encodeURIComponent(work.current.buildId)}`, { signal: abort.signal });
          if (!abort.signal.aborted) setBuild(job);
        }
        if (!abort.signal.aborted) setMessage("选择 ZIP Package 与已批准的 WebUI，构建不可变版本。");
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
      const active = await getJson<Session>(`${base}/session`, { signal: abort.signal });
      if (storageKey(active) !== storageKey(session)) throw new Error("会话已切换，请重新打开发布页面。");
      if (resumeBuild && work.current?.buildId) { await readBuild(work.current.buildId, abort.signal); return; }
      if (!file || !versionLabel || !webuiId || (!namespaceId && !namespaceName) || (!packageId && !packageName)) throw new Error("请填写发布信息并选择 ZIP 文件。");
      setMessage("正在校验 ZIP 文件…");
      const bytes = await file.arrayBuffer(); const hash = await sha256(bytes);
      const fingerprint = JSON.stringify([namespaceId, namespaceName, packageId, packageName, versionLabel, webuiId, hash]);
      let current = work.current;
      if (!current || current.fingerprint !== fingerprint) { current = { fingerprint, key: crypto.randomUUID(), selection: { namespaceId, namespaceName, packageId, packageName, versionLabel, webuiId } }; setBuild(null); setVersion(null); }
      save(active, current);
      const command = <T,>(path: string, body: unknown, key: string) => postJson<T>(base + path, body, active.csrfToken, `${current.key}:${key}`, 30_000, abort.signal);
      if (!current.packageId) {
        const ns = namespaceId || (await command<Entry>("/namespaces", { name: namespaceName }, "namespace")).id;
        current.packageId = packageId || (await command<Entry>("/packages", { namespaceId: ns, name: packageName, description: "" }, "package")).id;
        save(active, current);
      }
      let upload: Upload;
      if (current.uploadId) upload = await getJson<Upload>(`${base}/uploads/${encodeURIComponent(current.uploadId)}`, { signal: abort.signal });
      else {
        upload = await command<Upload>(`/packages/${encodeURIComponent(current.packageId)}/uploads`, { versionLabel, fileName: file.name, sizeBytes: file.size, sha256: hash }, "upload");
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
        const permit = await command<UploadPermit>(`/uploads/${encodeURIComponent(upload.id)}/parts`, { partNumber: number, sizeBytes: chunk.byteLength, sha256: checksum }, `part-${number}`);
        const etag = await uploadPackagePart(permit, chunk, checksum, abort.signal);
        parts.push({ partNumber: number, etag, sizeBytes: chunk.byteLength, sha256: checksum });
      }
      await command(`/uploads/${encodeURIComponent(upload.id)}/complete`, { parts }, "complete");
      const job = await command<Build>("/builds", { packageVersionId: upload.packageVersionId, webuiVersionId: webuiId }, "build");
      current.buildId = job.id; save(active, current); await readBuild(job.id, abort.signal);
    } catch (e) { if (!abort.signal.aborted) setError(e instanceof Error ? e.message : "发布失败，可重试读取或继续上传。"); }
    finally { if (!abort.signal.aborted) setBusy(false); }
  }

  return <section className="panel publisher-page">
    <div className="panel-title"><div><h2>发布 Package</h2><p>上传源码包，选择 WebUI，构建可供部署的版本。</p></div></div>
    <form onSubmit={(e) => { e.preventDefault(); void run(); }}>
      <fieldset disabled={busy || !session}>
        <label>命名空间<select aria-label="命名空间" value={namespaceId} onChange={(e) => setNamespaceId(e.target.value)}><option value="">新建命名空间</option>{namespaces.map((v) => <option key={v.id} value={v.id}>{v.name}</option>)}</select></label>
        {!namespaceId && <label>命名空间名称<input value={namespaceName} onChange={(e) => setNamespaceName(e.target.value)} required /></label>}
        <label>Package<select aria-label="Package" value={packageId} onChange={(e) => setPackageId(e.target.value)}><option value="">新建 Package</option>{packages.map((v) => <option key={v.id} value={v.id}>{v.name}</option>)}</select></label>
        {!packageId && <label>Package 名称<input value={packageName} onChange={(e) => setPackageName(e.target.value)} required /></label>}
        <label>版本名称<input value={versionLabel} onChange={(e) => setVersionLabel(e.target.value)} placeholder="例如 0.1.0" required /></label>
        <label>WebUI<select aria-label="WebUI" value={webuiId} onChange={(e) => setWebuiId(e.target.value)} required><option value="">选择已批准的 WebUI</option>{webuis.map((v) => <option key={v.id} value={v.id}>{v.name} · {v.versionLabel}</option>)}</select></label>
        <label>ZIP 文件<input type="file" accept=".zip,application/zip" onChange={(e) => setFile(e.target.files?.[0] || null)} required /></label>
        <Button type="submit" disabled={!webuis.length}>上传并构建 / 继续上传</Button>
      </fieldset>
    </form>
    <p role="status">{message}</p>
    {error && <p role="alert">{error}</p>}
    {build && <section aria-label="构建结果"><dl><dt>构建</dt><dd>{build.id}</dd><dt>状态</dt><dd>{stageLabel[build.status] || build.status}</dd>{build.artifactDigest && <><dt>镜像摘要</dt><dd><code>{build.artifactDigest}</code></dd></>}</dl><Button disabled={busy} variant="outline" onClick={() => void run(true)}>刷新构建状态</Button></section>}
    {version && <section aria-label="已就绪版本"><h3>版本已就绪</h3><p>{version.id}</p><p>版本与构建结果一致</p><code>{version.deploymentDescriptorDigest}</code></section>}
  </section>;
}
