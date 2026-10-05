import { AlertCircle, ChevronLeft, RefreshCw, Save } from "lucide-react";
import { useEffect, useState } from "react";

import type { ConsoleController } from "../../app/use-console-controller.ts";
import type { LaunchModelDTO } from "../../api/dtos.ts";
import { listAvailableLaunchModels } from "../../api/workspaces-api.ts";
import {
  presentWorkspaceModelStatus,
  workspaceModelDraft,
  workspaceModelDraftDirty,
  workspaceModelDraftReady,
  workspaceModelDraftSelections
} from "../../app/workspace-models-controller-model.ts";
import { formatDate } from "../../console-model.ts";
import { Alert, Badge, Button, Select } from "../ui/index.ts";
import { sourceData } from "./workspace-shared.tsx";

// The model configuration page renders the Workspace owner's own facts: the
// persisted intent version, the version the runtime confirmed, and the owner's
// operation that carries the reload. A submitted change stays "等待应用" until
// that original operation and the owner's readback confirm it; the page never
// advances the applied version itself.
export function WorkspaceModelsPage({ controller }: { controller: ConsoleController }) {
  const models = controller.workspaceModels;
  const workspace = sourceData(controller.customerWorkspaceRead.detail.value);
  const configurationSource = models.configuration.value;
  const configuration = sourceData(configurationSource);
  const role = controller.session?.user.role || "";
  const mayUpdate = role === "owner" || role === "admin";
  const [draft, setDraft] = useState<Record<string, string>>({});
  const [availableModels, setAvailableModels] = useState<LaunchModelDTO[] | null>(null);
  const [catalogError, setCatalogError] = useState("");

  const configurationKey = configuration ? `${configuration.workspaceId}:${configuration.version}:${configuration.updatedAt}` : "";
  useEffect(() => {
    setDraft(configuration ? workspaceModelDraft(configuration) : {});
  }, [configurationKey, configuration]);

  useEffect(() => {
    if (!configuration || configuration.selections.length === 0) return;
    let cancelled = false;
    setCatalogError("");
    void listAvailableLaunchModels()
      .then((items) => { if (!cancelled) setAvailableModels(items.filter((model) => model.available)); })
      .catch(() => {
        if (cancelled) return;
        setAvailableModels(null);
        setCatalogError("模型目录暂不可用，请稍后重试。");
      });
    return () => { cancelled = true; };
  }, [configurationKey, configuration?.selections.length]);

  const dirty = Boolean(configuration && workspaceModelDraftDirty(configuration, draft));
  useEffect(() => {
    if (!dirty) return;
    const onBeforeUnload = (event: BeforeUnloadEvent) => { event.preventDefault(); event.returnValue = ""; };
    window.addEventListener("beforeunload", onBeforeUnload);
    return () => window.removeEventListener("beforeunload", onBeforeUnload);
  }, [dirty]);

  const backToDetail = () => {
    if (workspace && dirty && !window.confirm("模型配置有未提交的修改，离开将丢弃。是否离开？")) return;
    const workspaceId = workspace?.id || configuration?.workspaceId || "";
    controller.navigate(workspaceId ? `/console/workspaces/${encodeURIComponent(workspaceId)}` : "/console/workspaces");
  };

  const save = () => {
    if (!configuration || !workspaceModelDraftReady(configuration, draft)) return;
    void models.update(workspaceModelDraftSelections(configuration, draft));
  };

  const statusPresentation = configuration ? presentWorkspaceModelStatus(configuration) : null;
  const retry = <Button onClick={() => void models.refresh()} size="sm" variant="outline"><RefreshCw aria-hidden size={14} />重试</Button>;

  return (
    <section className="workspace-detail-page" data-slide="C-WS-06">
      <Button onClick={backToDetail} size="sm" variant="ghost"><ChevronLeft aria-hidden size={16} />工作空间详情</Button>
      <div className="workspace-detail-content">
        <section className="panel workspace-models-panel">
          <div className="workspace-heading">
            <div>
              <h2>模型配置</h2>
              <p className="workspace-models-subtitle">{workspace ? workspace.name || "未命名工作空间" : "工作空间"}</p>
            </div>
            <div className="workspace-entry-actions">
              <Button busy={models.busy} onClick={() => void models.refresh()} variant="outline"><RefreshCw aria-hidden size={16} />刷新配置</Button>
            </div>
          </div>

          {models.issue === "unconfirmed" ? <Alert
            color="warning"
            indicator={<AlertCircle size={18} />}
            title="模型配置结果待确认"
            description="配置已提交，运行中的应用尚未确认加载。按原操作读回确认前不显示为已生效；请勿重复提交。"
            actions={<div className="workspace-actions"><Button busy={models.busy} onClick={() => void models.refreshOperation()} size="sm" variant="outline"><RefreshCw aria-hidden size={14} />刷新原操作</Button></div>}
          /> : null}
          {models.issue === "failed" ? <Alert
            color="danger"
            title="模型配置未生效"
            description="本次模型配置未获得运行应用确认，运行中的应用保留上一次已确认的配置。"
          /> : null}

          {configurationSource && configurationSource.available === false ? <Alert
            color="warning"
            indicator={<AlertCircle size={18} />}
            title="模型配置暂不可用"
            description="暂时无法从 Workspace 所有者确认模型配置，请稍后重试。"
            actions={retry}
          /> : null}
          {models.configuration.error && configurationSource?.available !== false ? <Alert color="danger" title="读取模型配置失败" description={models.configuration.error} actions={retry} /> : null}
          {models.operation.error ? <Alert color="warning" title="原操作读回不可用" description="暂时无法确认原操作结果，请稍后重试。" actions={retry} /> : null}

          {!configurationSource ? <div className="source-loading" aria-live="polite"><span className="spinner" />正在读取</div> : null}
          {configuration ? <div className="workspace-models-body">
            <dl className="data-list">
              <div><dt>配置状态</dt><dd>{statusPresentation ? <Badge color={statusPresentation.tone === "good" ? "success" : statusPresentation.tone === "warning" ? "warning" : "danger"}>{statusPresentation.label}</Badge> : "-"}</dd></div>
              <div><dt>目标版本</dt><dd><code>{configuration.version}</code></dd></div>
              <div><dt>已生效版本</dt><dd><code>{configuration.appliedVersion || "未确认"}</code></dd></div>
              <div><dt>更新时间</dt><dd>{formatDate(configuration.updatedAt, true)}</dd></div>
            </dl>
            {statusPresentation ? <p className="workspace-models-note">{statusPresentation.description}</p> : null}

            {configuration.selections.length === 0 ? <Alert
              color="info"
              title="没有可调整的模型槽位"
              description="当前应用使用发布者声明的内置模型配置，不存在可按槽位调整的模型。"
            /> : <>
              <div className="workspace-models-slots">
                {configuration.selections.map((selection) => <Select
                  block
                  description={mayUpdate ? undefined : "仅所有者或管理员可更新模型配置"}
                  disabled={!mayUpdate || models.busy}
                  key={selection.slot}
                  label={selection.slot}
                  onChange={(modelId) => setDraft((current) => ({ ...current, [selection.slot]: modelId }))}
                  options={[
                    ...(availableModels || []).map((model) => ({ value: model.id, label: model.name })),
                    ...((availableModels || []).some((model) => model.id === selection.modelId) ? [] : [{ value: selection.modelId, label: `${selection.modelId}（当前配置）` }])
                  ]}
                  placeholder="请选择模型"
                  value={draft[selection.slot] ?? ""}
                />)}
              </div>
              {catalogError ? <Alert color="warning" title="模型目录暂不可用" description={catalogError} /> : null}
              {models.updateError ? <Alert color="danger" title="提交失败" description={models.updateError} /> : null}
              <div className="workspace-actions">
                <Button busy={models.busy} color="primary" disabled={!mayUpdate || !workspaceModelDraftReady(configuration, draft) || !dirty} onClick={save}><Save aria-hidden size={16} />保存模型配置</Button>
                {!mayUpdate ? <span className="workspace-models-note">当前角色仅可查看模型配置。</span> : null}
              </div>
            </>}
          </div> : null}
        </section>
      </div>
    </section>
  );
}
