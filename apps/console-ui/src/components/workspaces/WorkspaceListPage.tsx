import { ChevronRight, Plus, RefreshCw } from "lucide-react";

import type { ConsoleController } from "../../app/use-console-controller.ts";
import { presentWorkspaceLifecycle } from "../../app/workspace-experience-model.ts";
import type { WorkspaceDTO } from "../../api/dtos.ts";
import { SourceState } from "../source/SourceState.tsx";
import { Badge, Button } from "../ui/index.ts";
import { formatCount, formatDate } from "../../console-model.ts";
import { LaunchOperation } from "./WorkspaceLaunchPage.tsx";
import { PageLink, Pagination } from "./workspace-shared.tsx";

type WorkspaceSummaryController = Pick<ConsoleController, "navigate">;
type WorkspaceListController = Pick<ConsoleController, "customerWorkspaceRead" | "navigate" | "refreshCurrentPage" | "workspaceLaunch">;

export function WorkspaceSummaryRow({ controller, workspace }: { controller: WorkspaceSummaryController; workspace: WorkspaceDTO }) {
  const path = `/console/workspaces/${encodeURIComponent(workspace.id)}`;
  return <tr><td><PageLink controller={controller} path={path}><strong>{workspace.name || "未命名工作空间"}</strong></PageLink></td><td>{workspace.packageId?.toUpperCase() || "暂不可用"}</td><td><Badge color="secondary">{presentWorkspaceLifecycle(workspace.state).label}</Badge></td><td>{formatDate(workspace.paidThrough)}</td><td><PageLink controller={controller} path={path}><span className="workspace-detail-link">查看详情</span><ChevronRight aria-hidden size={17} /></PageLink></td></tr>;
}

export function WorkspaceListPage({ controller }: { controller: WorkspaceListController }) {
  const workspaceRead = controller.customerWorkspaceRead;
  const workspacesUnavailable = workspaceRead.workspaces.value?.status === "unavailable" || Boolean(workspaceRead.workspaces.error);
  const workspacesPending = !workspaceRead.workspaces.value || workspaceRead.workspaces.loading;
  return (
    <section className="workspace-list-page" data-slide="C-WS-01">
      <div className="page-toolbar"><p>工作空间总数：{workspaceRead.workspaces.value?.available ? formatCount(workspaceRead.workspaces.value.data.total) : "暂不可用"}</p><Button color="primary" disabled={workspacesPending && !workspacesUnavailable} onClick={() => workspacesUnavailable ? void workspaceRead.refresh() : controller.navigate("/console/workspaces/new")}>{workspacesUnavailable ? <RefreshCw aria-hidden size={16} /> : <Plus aria-hidden size={16} />}{workspacesUnavailable ? "重试读取" : workspacesPending ? "正在读取" : "新建工作空间"}</Button></div>
      {controller.workspaceLaunch.launchOperation && !["succeeded", "failed", "refunded"].includes(controller.workspaceLaunch.launchOperation.status) ? (
        <LaunchOperation controller={controller.workspaceLaunch} compact onBack={() => controller.navigate("/console/workspaces")} onRefresh={controller.refreshCurrentPage} />
      ) : null}
      <section className="panel workspace-list-panel">
        <div className="workspace-list-head"><span>工作空间</span><span>套餐</span><span>当前状态</span><span>已付至</span><span /></div>
        <SourceState
          emptyTitle="暂无工作空间"
          emptyDescription="当前账号尚未开通工作空间。"
          error={workspaceRead.workspaces.error}
          errorDescription="暂时无法读取工作空间，请稍后重试。"
          loading={workspaceRead.workspaces.loading}
          onRetry={() => void workspaceRead.refresh()}
          source={workspaceRead.workspaces.value}
          unavailableDescription="暂时无法读取工作空间，请稍后重试。"
        >
          {(data) => <div className="workspace-list" role="list">{data.items.map((workspace) => (
            <PageLink className="workspace-list-row" controller={controller} key={workspace.id} path={`/console/workspaces/${encodeURIComponent(workspace.id)}`}>
              <span className="workspace-list-name"><strong>{workspace.name || "未命名工作空间"}</strong></span>
              <span><strong>{workspace.packageId?.toUpperCase() || "暂不可用"}</strong><small>{workspace.storageGb ? `${workspace.storageGb} GB` : "规格暂不可用"}</small></span>
              <span><strong>{presentWorkspaceLifecycle(workspace.state).label}</strong><small>当前状态</small></span>
              <span><strong>{formatDate(workspace.paidThrough)}</strong><small>权益截止</small></span>
              <span className="workspace-detail-link">查看详情</span>
              <ChevronRight aria-hidden size={18} />
            </PageLink>
          ))}</div>}
        </SourceState>
        <Pagination current={workspaceRead.page} label="工作空间分页" onChange={(page) => void workspaceRead.changePage(page)} pages={workspaceRead.pages} />
      </section>
    </section>
  );
}
