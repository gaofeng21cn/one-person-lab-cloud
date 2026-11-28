import { ChevronRight, Plus, RefreshCw } from "lucide-react";

import type { ConsoleController } from "../../app/use-console-controller.ts";
import { cloudIdentity } from "../../app/console-identity.ts";
import { presentWorkspaceDeliveryModel, presentWorkspaceLifecycle, presentWorkspaceResourceReadiness } from "../../app/workspace-experience-model.ts";
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
  // The Workspace owner delivers the Workspace and states the delivery model;
  // the Control Plane projection instead states the purchased plan.
  const delivery = cloudIdentity ? presentWorkspaceDeliveryModel(workspace.deliveryModel) : workspace.packageId?.toUpperCase() || "暂不可用";
  return <tr><td><PageLink controller={controller} path={path}><strong>{workspace.name || "未命名工作空间"}</strong></PageLink></td><td>{delivery}</td><td><Badge color="secondary">{presentWorkspaceLifecycle(workspace.state).label}</Badge></td><td>{formatDate(workspace.paidThrough)}</td><td><PageLink controller={controller} path={path}><span className="workspace-detail-link">查看详情</span><ChevronRight aria-hidden size={17} /></PageLink></td></tr>;
}

export function WorkspaceListPage({ controller }: { controller: WorkspaceListController }) {
  const workspaceRead = controller.customerWorkspaceRead;
  const workspacesForbidden = workspaceRead.access === "forbidden";
  const workspacesRestricted = workspaceRead.access === "not_enabled" || workspacesForbidden;
  const workspacesUnavailable = !workspacesRestricted && (workspaceRead.workspaces.value?.status === "unavailable" || Boolean(workspaceRead.workspaces.error));
  const workspacesPending = !workspacesRestricted && (!workspaceRead.workspaces.value || workspaceRead.workspaces.loading);
  return (
    <section className="workspace-list-page" data-slide="C-WS-01">
      <div className="page-toolbar">
        <p>{workspacesRestricted
          ? (workspacesForbidden ? "工作空间状态：无访问权限" : "工作空间状态：尚未开通白名单")
          : `工作空间总数：${workspaceRead.workspaces.value?.available ? formatCount(workspaceRead.workspaces.value.data.total) : "暂不可用"}`}</p>
        <div className="workspace-actions">
          {cloudIdentity && !workspacesRestricted ? <Button onClick={() => controller.navigate("/console/publisher")} variant="outline">上传智能体</Button> : null}
          {(cloudIdentity && !workspacesRestricted) || workspacesUnavailable ? <Button color="primary" disabled={workspacesPending && !workspacesUnavailable} onClick={() => workspacesUnavailable ? void workspaceRead.refresh() : controller.navigate("/console/workspaces/new")}>{workspacesUnavailable ? <RefreshCw aria-hidden size={16} /> : <Plus aria-hidden size={16} />}{workspacesUnavailable ? "重试读取" : workspacesPending ? "正在读取" : "新建工作空间"}</Button> : null}
        </div>
      </div>
      {controller.workspaceLaunch.launchOperation && !["succeeded", "failed", "refunded"].includes(controller.workspaceLaunch.launchOperation.status) ? (
        <LaunchOperation controller={controller.workspaceLaunch} compact onBack={() => controller.navigate("/console/workspaces")} onRefresh={controller.refreshCurrentPage} />
      ) : null}
      <section className="panel workspace-list-panel">
        <div className="workspace-list-head"><span>工作空间</span><span>{cloudIdentity ? "交付模式" : "套餐"}</span><span>当前状态</span><span>已付至</span><span /></div>
        <SourceState
          emptyTitle="暂无工作空间"
          emptyDescription="当前账号尚未开通工作空间。"
          error={workspaceRead.workspaces.error}
          errorDescription="暂时无法读取工作空间，请稍后重试。"
          loading={workspaceRead.workspaces.loading}
          notice={workspacesRestricted}
          noticeDescription={workspacesForbidden ? "当前账号角色暂无工作空间访问权限，请联系管理员。" : "工作空间按白名单开通，当前账号可正常使用 OPL Gateway 与 API 余额；如需开通请联系管理员。"}
          noticeKind="permission"
          noticeTitle={workspacesForbidden ? "无权查看工作空间" : "工作空间尚未开通"}
          onRetry={() => void workspaceRead.refresh()}
          source={workspaceRead.workspaces.value}
          unavailableDescription="暂时无法读取工作空间，请稍后重试。"
        >
          {(data) => <div className="workspace-list" role="list">{data.items.map((workspace) => (
            <PageLink className="workspace-list-row" controller={controller} key={workspace.id} path={`/console/workspaces/${encodeURIComponent(workspace.id)}`}>
              <span className="workspace-list-name"><strong>{workspace.name || "未命名工作空间"}</strong></span>
              {cloudIdentity
                ? <span><strong>{presentWorkspaceDeliveryModel(workspace.deliveryModel)}</strong><small>资源：{presentWorkspaceResourceReadiness(workspace.resourceReadiness)}</small></span>
                : <span><strong>{workspace.packageId?.toUpperCase() || "暂不可用"}</strong><small>{workspace.storageGb ? `${workspace.storageGb} GB` : "规格暂不可用"}</small></span>}
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
