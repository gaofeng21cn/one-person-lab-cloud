import { ChevronLeft, LockKeyhole, Plus, Rocket } from "lucide-react";
import { useEffect, useRef, useState } from "react";

import { getJson } from "../api/console-api.ts";
import type { AuthSession } from "../api/dtos.ts";
import { hasCloudTenantAccess, isPermissionDeniedError } from "../app/authorization-error.ts";
import type { CustomerConsoleRoute } from "../app/console-router.ts";
import { presentAgentStatus } from "../app/workspace-experience-model.ts";
import { Alert, Badge, Button } from "../components/ui/index.ts";
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

function AgentStatus({ value }: { value?: string }) {
  return <span className="agent-status">{presentAgentStatus(value)}</span>;
}

type AgentController = { navigate: (path: string) => void; session?: AuthSession | null };

function AgentDirectory({ controller }: { controller: AgentController }) {
  const [packages, setPackages] = useState<Package[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [restricted, setRestricted] = useState(false);
  const abortRef = useRef<AbortController | null>(null);
  const tenantAllowed = controller.session === undefined || hasCloudTenantAccess(controller.session);

  const load = () => {
    abortRef.current?.abort();
    if (!tenantAllowed) {
      setPackages([]);
      setError("");
      setRestricted(true);
      setLoading(false);
      return;
    }
    const abort = new AbortController();
    abortRef.current = abort;
    setRestricted(false);
    setLoading(true);
    setError("");
    void pages<Package>(`${base}/packages`, abort.signal).then((items) => {
      setPackages(items);
      setLoading(false);
    }).catch((reason: unknown) => {
      if (!abort.signal.aborted) {
        if (isPermissionDeniedError(reason)) {
          setRestricted(true);
          setError("");
        } else {
          setError(reason instanceof Error ? reason.message : "暂时无法读取智能体列表，请稍后重试。");
        }
        setLoading(false);
      }
    });
  };

  useEffect(() => {
    load();
    return () => abortRef.current?.abort();
  }, [tenantAllowed]);

  return <section className="panel agents-page">
    <div className="panel-title">
      <div><h2>我的智能体</h2></div>
      <div className="agent-page-actions">
        {restricted ? (
          <Button onClick={() => controller.navigate("/console/api")} size="sm" variant="ghost">前往 OPL Gateway</Button>
        ) : (
          <>
            <Button color="primary" onClick={() => controller.navigate("/console/publisher")}><Plus aria-hidden size={16} />上传智能体</Button>
            <Button onClick={load} variant="outline">刷新</Button>
          </>
        )}
      </div>
    </div>
    {loading && <p role="status">正在加载智能体…</p>}
    {restricted && !loading && (
      <div className="source-notice" data-notice-kind="permission" role="status">
        <span className="source-notice__icon" aria-hidden="true"><LockKeyhole size={20} /></span>
        <h3>智能体功能尚未开通</h3>
        <p>当前账号尚未开通工作空间与智能体白名单，请联系管理员；OPL Gateway 与 API 余额仍可正常使用。</p>
        <div className="source-notice__action"><Button onClick={() => controller.navigate("/console/overview")} size="sm" variant="outline">返回概览</Button></div>
      </div>
    )}
    {error && !restricted && <Alert color="warning" description={error} title="读取失败" />}
    {!loading && !error && !restricted && packages.length === 0 && <p role="status">还没有上传智能体，点击右上角上传你的第一个智能体。</p>}
    {!loading && !error && !restricted && packages.length > 0 && <div className="agents-grid">
      {packages.map((agent) => <article className="agent-card" key={agent.id}>
        <div><h3>{agent.name || "未命名智能体"}</h3><p>{agent.description || "暂无简介"}</p></div>
        <dl>
          <div><dt>状态</dt><dd><AgentStatus value={agent.status} /></dd></div>
          <div><dt>可用版本</dt><dd>{agent.latestReadyVersionId ? "已就绪" : "暂无"}</dd></div>
        </dl>
        <div className="agent-card__actions">
          <Button onClick={() => controller.navigate(`/console/agents/${encodeURIComponent(agent.id)}`)} variant="outline">查看详情</Button>
        </div>
      </article>)}
    </div>}
  </section>;
}

function AgentDetail({ route, controller }: { route: AgentDetailRoute; controller: AgentController }) {
  const [agent, setAgent] = useState<Package | null>(null);
  const [packageVersions, setPackageVersions] = useState<PackageVersion[]>([]);
  const [capabilityVersions, setCapabilityVersions] = useState<CapabilityVersion[]>([]);
  const [builds, setBuilds] = useState<Record<string, BuildJob>>({});
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [restricted, setRestricted] = useState(false);
  const abortRef = useRef<AbortController | null>(null);
  const tenantAllowed = controller.session === undefined || hasCloudTenantAccess(controller.session);

  const load = () => {
    abortRef.current?.abort();
    if (!tenantAllowed) {
      setAgent(null);
      setError("");
      setRestricted(true);
      setLoading(false);
      return;
    }
    const abort = new AbortController();
    abortRef.current = abort;
    setRestricted(false);
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
          if (isPermissionDeniedError(reason)) {
            setRestricted(true);
            setError("");
          } else {
            setError(reason instanceof Error ? reason.message : "暂时无法读取智能体详情，请稍后重试。");
          }
          setLoading(false);
        }
      }
    })();
  };

  useEffect(() => {
    load();
    return () => abortRef.current?.abort();
  }, [route.packageId, tenantAllowed]);

  return <section className="panel agents-page agent-detail-page">
    <div className="panel-title">
      <div>
        <Button onClick={() => controller.navigate("/console/agents")} size="sm" variant="ghost"><ChevronLeft aria-hidden size={16} />返回智能体</Button>
        <h2>{agent?.name || "智能体详情"}</h2>
      </div>
      <div className="agent-page-actions">
        {restricted ? null : (
          <>
            <Button color="primary" onClick={() => controller.navigate(`/console/publisher?packageId=${encodeURIComponent(route.packageId)}`)}><Plus aria-hidden size={16} />上传新版本</Button>
            <Button onClick={load} variant="outline">刷新</Button>
          </>
        )}
      </div>
    </div>
    {loading && <p role="status">正在加载智能体版本…</p>}
    {restricted && !loading && (
      <div className="source-notice" data-notice-kind="permission" role="status">
        <span className="source-notice__icon" aria-hidden="true"><LockKeyhole size={20} /></span>
        <h3>智能体功能尚未开通</h3>
        <p>当前账号尚未开通工作空间与智能体白名单，请联系管理员；OPL Gateway 与 API 余额仍可正常使用。</p>
        <div className="source-notice__action"><Button onClick={() => controller.navigate("/console/overview")} size="sm" variant="outline">返回概览</Button></div>
      </div>
    )}
    {error && !restricted && <Alert color="warning" description={error} title="读取失败" />}
    {!loading && !error && !restricted && agent && <>
      <section className="agent-summary" aria-label="智能体信息">
        <h3>基本信息</h3>
        <dl>
          <div><dt>简介</dt><dd>{agent.description || "未填写"}</dd></div>
          <div><dt>状态</dt><dd><AgentStatus value={agent.status} /></dd></div>
          <div><dt>可用版本</dt><dd>{agent.latestReadyVersionId ? "已就绪" : "暂无"}</dd></div>
          <div><dt>智能体编号</dt><dd><code>{agent.id}</code></dd></div>
        </dl>
      </section>
      <section aria-label="智能体版本">
        <h3>版本记录</h3>
        <div className="agent-version-list">
          {packageVersions.length === 0 && <p>暂无上传版本，点击右上角「上传新版本」开始构建。</p>}
          {packageVersions.map((version) => {
            const capability = capabilityVersions.find((item) => item.packageVersionId === version.id);
            const build = capability ? builds[capability.id] : undefined;
            const deployable = capability?.status === "ready";
            return <article className="agent-version" key={version.id}>
              <div className="agent-version__head">
                <div>
                  <h4>{version.versionLabel || "未命名版本"}</h4>
                  <p>上传状态：<AgentStatus value={version.status} /></p>
                </div>
                <div className="agent-version__actions">
                  <Badge color={deployable ? "success" : "secondary"}>{deployable ? "版本已可部署" : "暂不可部署"}</Badge>
                  {deployable && capability ? (
                    <Button color="primary" onClick={() => controller.navigate(`/console/workspaces/new?capabilityVersionId=${encodeURIComponent(capability.id)}`)} size="sm">
                      <Rocket aria-hidden size={14} />创建工作空间
                    </Button>
                  ) : null}
                </div>
              </div>
              <dl>
                <div><dt>来源</dt><dd>{capability ? `${capability.provenance || "平台构建"}${capability.buildJobId ? ` · 构建 ${capability.buildJobId}` : ""}` : "未构建"}</dd></div>
                <div><dt>使用中的工作空间</dt><dd>{capability ? String(capability.referenceCount ?? "-") : "-"}</dd></div>
                {build && <div><dt>构建进度</dt><dd>{presentAgentStatus(build.stage || build.status)}{build.errorCode ? ` · ${build.errorCode}` : ""}</dd></div>}
              </dl>
            </article>;
          })}
        </div>
      </section>
    </>}
  </section>;
}

export function AgentPages({ controller, route }: { controller: AgentController; route: Extract<CustomerConsoleRoute, { kind: "customer.agents" | "customer.agent-detail" }> }) {
  return route.kind === "customer.agents" ? <AgentDirectory controller={controller} /> : <AgentDetail controller={controller} route={route} />;
}
