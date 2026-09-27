import { useEffect, useRef, useState } from "react";

import { getJson } from "../api/console-api.ts";
import type { CustomerConsoleRoute } from "../app/console-router.ts";
import { Button } from "../components/ui/index.ts";
import "./agents.css";

type Package = {
  id: string;
  name: string;
  description?: string;
  visibility?: string;
  status?: string;
  latestReadyVersionId?: string;
  createdAt?: string;
  updatedAt?: string;
};
type PackageVersion = {
  id: string;
  packageId: string;
  versionLabel?: string;
  status?: string;
  createdAt?: string;
};
type CapabilityVersion = {
  id: string;
  packageId?: string;
  packageVersionId?: string;
  buildJobId?: string;
  versionLabel?: string;
  status?: string;
  referenceCount?: number | string;
  provenance?: string;
  createdAt?: string;
};
type BuildJob = {
  id: string;
  packageVersionId?: string;
  status?: string;
  stage?: string;
  resultCapabilityVersionId?: string;
  retryAllowed?: boolean;
  errorCode?: string;
};
type Page<T> = { items: T[]; nextCursor?: string };
type AgentDetailRoute = Extract<CustomerConsoleRoute, { kind: "customer.agent-detail" }>;

const base = "/api/v2";

async function pages<T>(path: string, signal: AbortSignal): Promise<T[]> {
  const items: T[] = [];
  let cursor = "";
  do {
    const separator = path.includes("?") ? "&" : "?";
    const page = await getJson<Page<T>>(`${path}${separator}cursor=${encodeURIComponent(cursor)}`, { signal });
    items.push(...page.items);
    cursor = page.nextCursor || "";
  } while (cursor);
  return items;
}

function ownerStatus(value?: string) {
  return value || "暂不可用";
}

function AgentStatus({ value }: { value?: string }) {
  return <span className="agent-status">{ownerStatus(value)}</span>;
}

function AgentDirectory({ controller }: { controller: { navigate: (path: string) => void } }) {
  const [packages, setPackages] = useState<Package[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const abortRef = useRef<AbortController | null>(null);

  const load = () => {
    abortRef.current?.abort();
    const abort = new AbortController();
    abortRef.current = abort;
    setLoading(true);
    setError("");
    void pages<Package>(`${base}/packages`, abort.signal).then((items) => {
      setPackages(items);
      setLoading(false);
    }).catch((reason: unknown) => {
      if (!abort.signal.aborted) {
        setError(reason instanceof Error ? reason.message : "智能体目录读取失败");
        setLoading(false);
      }
    });
  };

  useEffect(() => {
    load();
    return () => abortRef.current?.abort();
  }, []);

  return <section className="panel agents-page">
    <div className="panel-title"><div><h2>我的智能体</h2><p>目录与版本状态均来自 Capability owner readback。</p></div><div className="agent-page-actions"><Button onClick={() => controller.navigate("/console/publisher")} color="primary">发布新 Package</Button><Button onClick={load} variant="outline">刷新</Button></div></div>
    {loading && <p role="status">正在读取智能体目录…</p>}
    {error && <p role="alert">{error}</p>}
    {!loading && !error && packages.length === 0 && <p role="status">暂无智能体。请从发布入口创建 Package。</p>}
    {!loading && !error && packages.length > 0 && <div className="agents-grid">
      {packages.map((agent) => <article className="agent-card" key={agent.id}>
        <div><h3>{agent.name || "未命名智能体"}</h3><p>{agent.description || "未提供用途说明"}</p></div>
        <dl><div><dt>目录状态</dt><dd><AgentStatus value={agent.status} /></dd></div><div><dt>可部署版本</dt><dd>{agent.latestReadyVersionId ? "已登记" : "暂无"}</dd></div></dl>
        <Button onClick={() => controller.navigate(`/console/agents/${encodeURIComponent(agent.id)}`)} variant="outline">查看详情</Button>
      </article>)}
    </div>}
  </section>;
}

function AgentDetail({ route, controller }: { route: AgentDetailRoute; controller: { navigate: (path: string) => void } }) {
  const [agent, setAgent] = useState<Package | null>(null);
  const [packageVersions, setPackageVersions] = useState<PackageVersion[]>([]);
  const [capabilityVersions, setCapabilityVersions] = useState<CapabilityVersion[]>([]);
  const [builds, setBuilds] = useState<Record<string, BuildJob>>({});
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const abortRef = useRef<AbortController | null>(null);

  const load = () => {
    abortRef.current?.abort();
    const abort = new AbortController();
    abortRef.current = abort;
    setLoading(true);
    setError("");
    void (async () => {
      try {
        const packageId = encodeURIComponent(route.packageId);
        const [packageRead, packageVersionRead, capabilityRead] = await Promise.all([
          getJson<Package>(`${base}/packages/${packageId}`, { signal: abort.signal }),
          pages<PackageVersion>(`${base}/packages/${packageId}/versions`, abort.signal),
          pages<CapabilityVersion>(`${base}/capability-versions?packageId=${packageId}`, abort.signal)
        ]);
        const buildEntries = await Promise.all(capabilityRead.filter((version) => version.buildJobId).map(async (version) => {
          const build = await getJson<BuildJob>(`${base}/builds/${encodeURIComponent(version.buildJobId || "")}`, { signal: abort.signal });
          return [version.id, build] as const;
        }));
        if (abort.signal.aborted) return;
        setAgent(packageRead);
        setPackageVersions(packageVersionRead);
        setCapabilityVersions(capabilityRead);
        setBuilds(Object.fromEntries(buildEntries));
        setLoading(false);
      } catch (reason) {
        if (!abort.signal.aborted) {
          setError(reason instanceof Error ? reason.message : "智能体详情读取失败");
          setLoading(false);
        }
      }
    })();
  };

  useEffect(() => {
    load();
    return () => abortRef.current?.abort();
  }, [route.packageId]);

  return <section className="panel agents-page agent-detail-page">
    <div className="panel-title"><div><Button onClick={() => controller.navigate("/console/agents")} variant="ghost">返回智能体</Button><h2>{agent?.name || "智能体详情"}</h2><p>用途、来源、版本与引用保护由各 owner 读回。</p></div><Button onClick={load} variant="outline">刷新</Button></div>
    {loading && <p role="status">正在读取智能体与版本…</p>}
    {error && <p role="alert">{error}</p>}
    {!loading && !error && agent && <>
      <section className="agent-summary" aria-label="智能体 owner readback"><h3>智能体信息</h3><dl><div><dt>Package</dt><dd><code>{agent.id}</code></dd></div><div><dt>用途</dt><dd>{agent.description || "未提供用途说明"}</dd></div><div><dt>目录状态</dt><dd><AgentStatus value={agent.status} /></dd></div><div><dt>可部署版本</dt><dd>{agent.latestReadyVersionId ? "已登记" : "暂无"}</dd></div></dl></section>
      <section aria-label="智能体版本"><h3>版本</h3><div className="agent-version-list">
        {packageVersions.length === 0 && <p>暂无上传版本。</p>}
        {packageVersions.map((version) => {
          const capability = capabilityVersions.find((item) => item.packageVersionId === version.id);
          const build = capability ? builds[capability.id] : undefined;
          const deployable = capability?.status === "ready";
          return <article className="agent-version" key={version.id}>
            <div><h4>{version.versionLabel || "未命名版本"}</h4><p>上传版本：<AgentStatus value={version.status} /></p></div>
            <dl><div><dt>可部署状态</dt><dd>{deployable ? "版本已可部署" : "尚不可部署"}</dd></div><div><dt>来源</dt><dd>{capability ? `${capability.provenance || "owner readback"}${capability.buildJobId ? ` · Build ${capability.buildJobId}` : ""}` : "尚无登记版本"}</dd></div><div><dt>引用</dt><dd>{capability ? String(capability.referenceCount ?? "暂不可用") : "暂不可用"}</dd></div>{build && <div><dt>构建</dt><dd>{build.stage || build.status || "暂不可用"}{build.errorCode ? ` · ${build.errorCode}` : ""}</dd></div>}</dl>
          </article>;
        })}
      </div></section>
      <p className="agent-boundary">目录下架与归档需要对应 owner 写接口和引用检查；当前页面只展示读回事实，不把目录移除说成 OCI 字节销毁。</p>
    </>}
  </section>;
}

export function AgentPages({ controller, route }: { controller: { navigate: (path: string) => void }; route: Extract<CustomerConsoleRoute, { kind: "customer.agents" | "customer.agent-detail" }> }) {
  return route.kind === "customer.agents" ? <AgentDirectory controller={controller} /> : <AgentDetail controller={controller} route={route} />;
}
