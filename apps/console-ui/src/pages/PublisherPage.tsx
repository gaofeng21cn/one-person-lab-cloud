import { ChevronLeft } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { getJson, postJson } from "../api/console-api.ts";
import { readEffectiveBuildDefaults, uploadPackagePart, type UploadPermit } from "../api/publisher-api.ts";
import type { WorkspaceOwnerOperationDTO as Operation } from "../api/dtos.ts";
import type { EffectiveBuildDefaults } from "../app/cloud-webui-model.ts";
import { Alert, Button } from "../components/ui/index.ts";
import "./publisher.css";

type Session = { actorId: string; tenantId?: string; csrfToken: string };
type Entry = { id: string; name: string; versionLabel?: string; status?: string; artifactDigest?: string; admissionReceiptId?: string; publisherContractDigest?: string; publisherContractObjectRef?: string };
type Part = { partNumber: number; etag: string; sizeBytes: number; sha256: string };
type Upload = { id: string; packageVersionId: string; partSizeBytes: number; completedParts: Part[] };
type UploadWire = { id: string; packageVersionId: string; partSizeBytes: string; completedParts: Array<{ partNumber: number; etag: string; sizeBytes: string; sha256: string }> };
type Build = { id: string; status: string; stage: string; artifactDigest?: string; resultCapabilityVersionId?: string; packageVersionId?: string; runtimeVersionId?: string; webuiVersionId?: string };
type BuildLog = { id: string; buildJobId: string; sequence: number; stage: string; level: string; message: string; createdAt: string };
type Version = { id: string; status: string; artifactDigest: string; deploymentDescriptorDigest: string };
export type PackageVersion = { id: string; packageId: string; status: string; versionLabel: string; sha256: string; sizeBytes: string };
type Selection = { namespaceId: string; namespaceName: string; packageId: string; packageName: string; versionLabel: string; runtimeId: string; webuiId: string };
type Work = { selection?: Selection; fingerprint: string; key: string; packageId?: string; uploadId?: string; packageVersionId?: string; buildId?: string };

const base = "/api/v2";
const stageLabel: Record<string, string> = { queued: "等待构建", validating: "校验文件", building: "正在构建", pushing: "保存产物", registering: "登记版本", succeeded: "构建完成", failed: "构建失败", needs_attention: "结果待确认", cancelled: "已取消" };
const logLevelText: Record<string, string> = { info: "信息", warning: "警告", error: "错误" };
const buildLogPageLimit = 200;
const buildLogPageCap = 20;
// One customer sentence per owner fact the Console cannot supply. The Build
// stays unavailable until the platform's effective default policy resolves the
// exact inputs, so a missing policy is never replaced by a catalog row, an older
// release or a plausible guess.
const blockedReasonText: Record<string, string> = {
  effective_default_runtime_missing: "平台尚未启用默认运行底座",
  effective_default_runtime_ambiguous: "平台存在多个默认运行底座，需要管理员确认",
  effective_default_runtime_not_approved: "默认运行底座当前不可用",
  effective_default_runtime_digest_not_immutable: "默认运行底座版本校验未通过",
  effective_default_webui_not_readable: "默认交互界面尚未就绪"
};
// Where a Build stops, the customer needs the next action rather than the code.
const failureText: Record<string, string> = {
  validation_failed: "文件校验未通过，请确认压缩包内容、名称与版本号后重试。",
  build_failed: "构建失败，请修改压缩包后重试。",
  build_result_unknown: "构建结果待确认，请稍后刷新状态。",
  runtime_revoked: "当前运行底座已停用，请联系管理员。",
  webui_incompatible: "当前交互界面与运行底座不兼容，请联系管理员。",
  policy_unconfigured: "平台尚未完成默认构建配置，请联系管理员。"
};
function blockedSentence(reasons: string[]) {
  const facts = reasons.map((reason) => blockedReasonText[reason] || reason).join("；");
  return `暂时无法开始构建：${facts || "构建环境暂不可用"}。请联系管理员完成构建配置后再试。`;
}
async function sha256(bytes: ArrayBuffer) {
  const hash = await crypto.subtle.digest("SHA-256", bytes);
  return "sha256:" + Array.from(new Uint8Array(hash), (v) => v.toString(16).padStart(2, "0")).join("");
}
function decodeUpload(value: UploadWire): Upload {
  const partSizeBytes = Number(value.partSizeBytes);
  if (!Number.isSafeInteger(partSizeBytes) || partSizeBytes <= 0) throw new Error("上传服务返回了无效的分片大小。");
  const completedParts = value.completedParts.map((part) => {
    const sizeBytes = Number(part.sizeBytes);
    if (!Number.isSafeInteger(sizeBytes) || sizeBytes <= 0) throw new Error("上传服务返回了无效的分片信息。");
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
async function pages<T>(path: string, signal: AbortSignal): Promise<T[]> {
  const entries: T[] = [];
  let cursor = "";
  do {
    const page = await getJson<{ items: T[]; nextCursor?: string }>(`${path}${path.includes("?") ? "&" : "?"}cursor=${encodeURIComponent(cursor)}`, { signal });
    entries.push(...page.items);
    cursor = page.nextCursor || "";
  } while (cursor);
  return entries;
}

async function buildLogs(buildId: string, signal: AbortSignal): Promise<{ items: BuildLog[]; truncated: boolean }> {
  const items: BuildLog[] = [];
  let cursor = "";
  for (let page = 0; page < buildLogPageCap; page++) {
    const separator = page === 0 ? "?" : "&";
    const read = await getJson<{ items: BuildLog[]; nextCursor?: string }>(
      `${base}/builds/${encodeURIComponent(buildId)}/logs${separator}limit=${buildLogPageLimit}${cursor ? `&cursor=${encodeURIComponent(cursor)}` : ""}`,
      { signal }
    );
    items.push(...(read.items || []));
    cursor = read.nextCursor || "";
    if (!cursor) return { items, truncated: false };
  }
  return { items, truncated: true };
}

function PublisherSteps({ current }: { current: "select" | "building" | "ready" }) {
  const steps = [
    ["select", "填写信息"],
    ["building", "上传并构建"],
    ["ready", "构建完成"]
  ] as const;
  return (
    <ol aria-label="上传智能体步骤" className="publisher-steps">
      {steps.map(([step, label], index) => (
        <li aria-current={current === step ? "step" : undefined} className={current === step ? "active" : ""} key={step}>
          <span>{index + 1}</span><strong>{label}</strong>
        </li>
      ))}
    </ol>
  );
}

export function PublisherPage({ onNavigate }: { onNavigate?: (path: string) => void }) {
  const navigate = (path: string) => {
    if (onNavigate) { onNavigate(path); return; }
    window.history.pushState(null, "", path);
    window.dispatchEvent(new PopStateEvent("popstate"));
  };
  const requestedPackageId = useRef(new URLSearchParams(window.location.search).get("packageId")?.trim() || "");
  const [session, setSession] = useState<Session | null>(null);
  const [namespaces, setNamespaces] = useState<Entry[]>([]);
  const [packages, setPackages] = useState<Entry[]>([]);
  const [defaults, setDefaults] = useState<EffectiveBuildDefaults | null>(null);
  const [namespaceId, setNamespaceId] = useState("");
  const [namespaceName, setNamespaceName] = useState("");
  const [packageId, setPackageId] = useState("");
  const [packageName, setPackageName] = useState("");
  const [versionLabel, setVersionLabel] = useState("");
  const [file, setFile] = useState<File | null>(null);
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState("");
  const [error, setError] = useState("");
  const [build, setBuild] = useState<Build | null>(null);
  const [logs, setLogs] = useState<BuildLog[]>([]);
  const [logsTruncated, setLogsTruncated] = useState(false);
  const [logRevision, setLogRevision] = useState(0);
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
        const ns = await pages<Entry>(`${base}/namespaces`, abort.signal);
        if (abort.signal.aborted) return;
        setSession(active); setNamespaces(ns); setNamespaceId(ns[0]?.id || "");
        try { work.current = JSON.parse(sessionStorage.getItem(storageKey(active)) || "null") as Work | null; } catch { work.current = null; }
        const selected = work.current?.selection;
        if (selected && !requestedPackageId.current) {
          setNamespaceId(selected.namespaceId); setNamespaceName(selected.namespaceName);
          setPackageId(selected.packageId); setPackageName(selected.packageName);
          setVersionLabel(selected.versionLabel);
        }
        if (work.current?.buildId) {
          const job = await getJson<Build>(`${base}/builds/${encodeURIComponent(work.current.buildId)}`, { signal: abort.signal });
          if (!abort.signal.aborted) setBuild(job);
        }
        if (work.current?.packageVersionId) {
          const readback = await getJson<PackageVersion>(`${base}/package-versions/${encodeURIComponent(work.current.packageVersionId)}`, { signal: abort.signal });
          if (!abort.signal.aborted) setPackageVersion(readback);
        }
      } catch (e) { if (!abort.signal.aborted) setError(e instanceof Error ? e.message : "暂时无法加载智能体列表，请刷新重试。"); }
    })();
    const resolveAbort = new AbortController();
    void readEffectiveBuildDefaults(resolveAbort.signal)
      .then((resolved) => { if (!resolveAbort.signal.aborted) setDefaults(resolved); })
      .catch((e: unknown) => { if (!resolveAbort.signal.aborted) setError(e instanceof Error ? e.message : "暂时无法检查构建环境，请稍后重试。"); });
    return () => { abort.abort(); resolveAbort.abort(); execution.current?.abort(); };
  }, []);

  useEffect(() => {
    const abort = new AbortController();
    setPackages([]);
    const resumedPackageId = !requestedPackageId.current && work.current?.selection?.namespaceId === namespaceId
      ? work.current.selection.packageId
      : "";
    setPackageId(resumedPackageId);
    if (namespaceId) {
      void pages<Entry>(`${base}/packages?namespaceId=${encodeURIComponent(namespaceId)}`, abort.signal).then((items) => {
        if (abort.signal.aborted) return;
        setPackages(items);
        if (requestedPackageId.current && items.some((item) => item.id === requestedPackageId.current)) {
          setPackageId(requestedPackageId.current);
          requestedPackageId.current = "";
        }
      }).catch((e: unknown) => {
        if (!abort.signal.aborted) setError(e instanceof Error ? e.message : "智能体列表读取失败，请刷新重试。");
      });
    }
    return () => abort.abort();
  }, [namespaceId]);

  useEffect(() => {
    const id = build?.id;
    if (!id) { setLogs([]); setLogsTruncated(false); return; }
    const abort = new AbortController();
    void (async () => {
      try {
        const read = await buildLogs(id, abort.signal);
        if (!abort.signal.aborted) { setLogs(read.items); setLogsTruncated(read.truncated); }
      } catch { if (!abort.signal.aborted) { setLogs([]); setLogsTruncated(false); } }
    })();
    return () => abort.abort();
  }, [build?.id, build?.status, logRevision]);

  async function readBuild(id: string, signal: AbortSignal) {
    for (;;) {
      const job = await getJson<Build>(`${base}/builds/${encodeURIComponent(id)}`, { signal });
      if (signal.aborted) return;
      setBuild(job); setMessage(stageLabel[job.stage] || "正在查询构建进度…");
      if (job.status === "succeeded" && job.resultCapabilityVersionId) {
        const result = await getJson<Version>(`${base}/capability-versions/${encodeURIComponent(job.resultCapabilityVersionId)}`, { signal });
        if (result.status !== "ready" || result.artifactDigest !== job.artifactDigest) throw new Error("构建结果仍在同步，请稍后刷新状态。");
        if (!signal.aborted) { setVersion(result); setMessage("构建完成，现在可以用来创建工作空间。"); }
        return;
      }
      if (["failed", "needs_attention", "cancelled"].includes(job.status)) {
        const code = (job as Build & { errorCode?: string }).errorCode;
        if (!signal.aborted && code) setError(failureText[code] || `构建未完成（${code}）。`);
        return;
      }
      await delay(signal);
    }
  }

  async function run(resumeBuild = false) {
    if (!session || busy) return;
    const abort = new AbortController(); execution.current?.abort(); execution.current = abort;
    setBusy(true); setError("");
    try {
      const active = await getJson<Session>(`${base}/auth/session`, { signal: abort.signal });
      if (storageKey(active) !== storageKey(session)) throw new Error("登录状态已变化，请重新打开此页面。");
      if (resumeBuild && work.current?.buildId) { await readBuild(work.current.buildId, abort.signal); return; }
      // The three frozen inputs are Package/Runtime/WebUI, but only the Package
      // comes from this form: Runtime and WebUI must be the platform's effective
      // defaults, so an unresolved default blocks the command instead of being
      // replaced by a catalog row.
      if (!defaults?.runtime || !defaults?.webui) throw new Error(blockedSentence(defaults?.reasons || []));
      const runtimeId = defaults.runtime.id; const webuiId = defaults.webui.id;
      if (!file || !versionLabel || (!namespaceId && !namespaceName) || (!packageId && !packageName)) throw new Error("请填写完整信息并选择智能体压缩包。");
      setMessage("正在校验压缩包…");
      const bytes = await file.arrayBuffer(); const hash = await sha256(bytes);
      const fingerprint = JSON.stringify([namespaceId, namespaceName, packageId, packageName, versionLabel, runtimeId, webuiId, hash]);
      let current = work.current;
      if (!current || current.fingerprint !== fingerprint) { current = { fingerprint, key: crypto.randomUUID(), selection: { namespaceId, namespaceName, packageId, packageName, versionLabel, runtimeId, webuiId } }; setBuild(null); setVersion(null); setPackageVersion(null); }
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
      if (!upload.packageVersionId || upload.packageVersionId !== current.packageVersionId) throw new Error("上传记录与当前版本不匹配，未开始构建，请重试。");
      const parts: Part[] = [];
      if (!(upload.partSizeBytes > 0)) throw new Error("上传服务没有返回有效分片大小。");
      for (let offset = 0, number = 1; offset < bytes.byteLength; offset += upload.partSizeBytes, number++) {
        const chunk = bytes.slice(offset, Math.min(bytes.byteLength, offset + upload.partSizeBytes));
        const checksum = await sha256(chunk);
        const done = upload.completedParts.find((p) => p.partNumber === number && p.sha256 === checksum && p.sizeBytes === chunk.byteLength);
        if (done) { parts.push(done); continue; }
        setMessage(`正在上传文件（第 ${number} 段）…`);
        const permit = await command<UploadPermit>(`/uploads/${encodeURIComponent(upload.id)}/parts`, { partNumber: number, sizeBytes: String(chunk.byteLength), sha256: checksum }, `part-${number}`);
        const etag = await uploadPackagePart(permit, chunk, checksum, abort.signal);
        parts.push({ partNumber: number, etag, sizeBytes: chunk.byteLength, sha256: checksum });
      }
      setMessage("正在校验上传结果…");
      // CompleteUpload currently commits verification and returns its terminal
      // Operation synchronously. HTTP acceptance alone cannot authorize Build.
      const completed = await command<Operation>(`/uploads/${encodeURIComponent(upload.id)}/complete`, { parts: parts.map((part) => ({ ...part, sizeBytes: String(part.sizeBytes) })) }, "complete");
      if (!completed?.operationId || completed.owner !== "capability" || completed.kind !== "complete_upload" || completed.resourceId !== upload.packageVersionId) throw new Error("上传结果与当前版本不匹配，未开始构建。");
      if (completed.status !== "succeeded" || (completed.observationResult !== undefined && completed.observationResult !== "confirmed")) throw new Error("上传尚未确认成功，未开始构建，请检查后重试。");
      setMessage("正在确认版本信息…");
      const readback = await getJson<PackageVersion>(`${base}/package-versions/${encodeURIComponent(upload.packageVersionId)}`, { signal: abort.signal });
      if (readback?.id !== upload.packageVersionId || readback.packageId !== current.packageId || readback.versionLabel !== versionLabel || readback.sha256 !== hash || readback.sizeBytes !== String(file.size)) throw new Error("上传文件校验不一致，未开始构建。");
      if (readback.status !== "uploaded") throw new Error("文件上传尚未就绪，未开始构建。");
      if (abort.signal.aborted) return;
      setPackageVersion(readback);
      const job = await command<Build>("/builds", { packageVersionId: upload.packageVersionId, runtimeVersionId: runtimeId, webuiVersionId: webuiId }, "build");
      current.buildId = job.id; save(active, current); await readBuild(job.id, abort.signal);
    } catch (e) { if (!abort.signal.aborted) setError(e instanceof Error ? e.message : "上传或构建失败，请稍后重试。"); }
    finally { if (!abort.signal.aborted) setBusy(false); }
  }

  const defaultsReady = Boolean(defaults?.runtime && defaults?.webui);
  const loadingDefaults = session !== null && defaults === null && !error;
  const canSubmit = Boolean(session && !busy && defaultsReady && file && versionLabel.trim() && (namespaceId || namespaceName.trim()) && (packageId || packageName.trim()));
  const hasResumableUpload = Boolean(work.current?.uploadId && !build);
  const currentStep = version ? "ready" : (busy || build) ? "building" : "select";
  const resolvedPackageId = packageId || work.current?.packageId || "";
  const readback = (input: { name?: string; versionLabel?: string; artifactDigest: string } | undefined, label: string) => input
    ? `${input.name || label} · ${input.versionLabel || "未标注版本"} · ${input.artifactDigest}`
    : "暂不可用";
  return <section className="panel publisher-page">
    <div className="publisher-breadcrumb">
      <Button onClick={() => navigate("/console/agents")} size="sm" variant="ghost"><ChevronLeft aria-hidden size={16} />返回我的智能体</Button>
    </div>
    <div className="panel-title"><div><h2>上传智能体</h2><p>上传智能体压缩包并填写版本号，系统将自动完成构建。</p></div></div>
    <PublisherSteps current={currentStep} />
    <form onSubmit={(e) => { e.preventDefault(); void run(); }}>
      <fieldset disabled={busy || !session}>
        <label>所属分组<select aria-label="所属分组" value={namespaceId} onChange={(e) => setNamespaceId(e.target.value)}><option value="">新建分组</option>{namespaces.map((v) => <option key={v.id} value={v.id}>{v.name}</option>)}</select></label>
        {!namespaceId && <label>分组名称<input value={namespaceName} onChange={(e) => setNamespaceName(e.target.value)} placeholder="例如：my-team" required /></label>}
        <label>智能体<select aria-label="智能体" value={packageId} onChange={(e) => setPackageId(e.target.value)}><option value="">新建智能体</option>{packages.map((v) => <option key={v.id} value={v.id}>{v.name}</option>)}</select></label>
        {!packageId && <label>智能体名称<input value={packageName} onChange={(e) => setPackageName(e.target.value)} placeholder="例如：研究助手" required /></label>}
        <label>版本号<input value={versionLabel} onChange={(e) => setVersionLabel(e.target.value)} placeholder="例如 0.1.0" required /></label>
        <label>智能体压缩包（.zip）<input type="file" accept=".zip,application/zip" onChange={(e) => setFile(e.target.files?.[0] || null)} required /></label>
        <Button busy={busy} color="primary" disabled={!canSubmit} type="submit">{hasResumableUpload ? "继续上传并构建" : "上传并构建"}</Button>
      </fieldset>
    </form>
    {message && <p role="status">{message}</p>}
    {loadingDefaults && <p role="status">正在检查构建环境…</p>}
    {defaults && !defaultsReady && <p role="status">{blockedSentence(defaults.reasons)}</p>}
    {error && <Alert color="danger" description={error} title="无法继续" />}
    {build && <section aria-label="构建进度" className="publisher-progress">
      <h3>{stageLabel[build.status] || build.status}</h3>
      <p>{build.status === "succeeded" ? "构建已完成，可以直接创建工作空间。" : "正在后台构建中，可点击刷新查看最新进度。"}</p>
      {logs.length > 0 && <section aria-label="构建日志" className="publisher-build-logs">
        <h4>构建日志</h4>
        <ol>{logs.map((log) => <li data-level={log.level} key={log.id}><span className="publisher-build-logs__level">{logLevelText[log.level] || log.level}</span><span className="publisher-build-logs__stage">{log.stage}</span><span className="publisher-build-logs__message">{log.message}</span></li>)}</ol>
        {logsTruncated ? <p className="publisher-build-logs__note">日志条数较多，以上为前 {logs.length} 条。</p> : null}
      </section>}
      <div className="publisher-progress__actions">
        <Button disabled={busy} variant="outline" onClick={() => { setLogRevision((value) => value + 1); void run(true); }}>刷新构建状态</Button>
      </div>
    </section>}
    {version && <section aria-label="已就绪版本" className="publisher-ready-card">
      <h3>版本已就绪</h3>
      <p>智能体已构建完成，现在可以直接用来创建工作空间。</p>
      <div className="publisher-ready-actions">
        <Button color="primary" onClick={() => navigate(`/console/workspaces/new?capabilityVersionId=${encodeURIComponent(version.id)}`)}>使用此智能体创建工作空间</Button>
        <Button onClick={() => navigate(resolvedPackageId ? `/console/agents/${encodeURIComponent(resolvedPackageId)}` : "/console/agents")} variant="outline">查看智能体详情</Button>
      </div>
    </section>}
    <details className="publisher-technical-details">
      <summary>技术详情</summary>
      <div className="publisher-technical-details__body">
        <section aria-label="平台默认构建配置">
          <h3>平台默认构建配置</h3>
          <dl>
            <dt>运行底座</dt><dd><code>{readback(defaults?.runtime, "默认运行底座")}</code></dd>
            <dt>交互界面</dt><dd><code>{readback(defaults?.webui, "默认界面")}</code></dd>
            <dt>配置来源</dt><dd><code>平台默认构建策略</code></dd>
          </dl>
        </section>
        {build && <section aria-label="构建结果"><h3>构建记录</h3><dl><dt>构建编号</dt><dd>{build.id}</dd><dt>状态</dt><dd>{stageLabel[build.status] || build.status}</dd><dt>Package ref</dt><dd><code>{build.packageVersionId || work.current?.packageVersionId || "暂不可用"}</code></dd><dt>Runtime Release ref</dt><dd><code>{build.runtimeVersionId || "暂不可用"}</code></dd><dt>WebUI ref</dt><dd><code>{build.webuiVersionId || "暂不可用"}</code></dd>{build.artifactDigest && <><dt>OCI digest</dt><dd><code>{build.artifactDigest}</code></dd></>}</dl></section>}
        {packageVersion && <section aria-label="Package 版本读回"><h3>上传版本记录</h3><dl><dt>Package Version</dt><dd><code>{packageVersion.id}</code></dd><dt>状态</dt><dd>{packageVersion.status || "暂不可用"}</dd><dt>Package bytes sha256</dt><dd><code>{packageVersion.sha256 || "暂不可用"}</code></dd></dl></section>}
        {version && <section aria-label="已就绪版本读回"><h3>就绪版本记录</h3><dl><dt>CapabilityVersion</dt><dd><code>{version.id}</code></dd><dt>deployment descriptor digest</dt><dd><code>{version.deploymentDescriptorDigest}</code></dd></dl></section>}
      </div>
    </details>
  </section>;
}
