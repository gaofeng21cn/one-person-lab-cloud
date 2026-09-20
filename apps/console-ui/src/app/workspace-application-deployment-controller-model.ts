import type { WorkspaceApplicationConfigurationDTO, WorkspaceApplicationSecretBindingDTO, WorkspaceApplicationComponentStateDTO, WorkspaceApplicationIntentDTO } from "../api/dtos.ts";

// The operator selects one image and states how it is exposed. Everything else
// a deployment needs is either the image's own declared fact (its ports, data
// paths and process identity, read by Control Plane from the pinned image) or an
// optional advanced description. No operator types an application identity, and
// no version is registered as a separate step.
export interface WorkspaceApplicationDeploymentSelection {
  platform: string;
  image: string;
  exposurePolicy: string;
  healthCheckPath: string;
  healthCheckPort: string;
  healthCheckInitialDelaySeconds: string;
}

export interface WorkspaceApplicationDeploymentForm {
  selection: WorkspaceApplicationDeploymentSelection;
  advancedJSON: string;
}

export type WorkspaceApplicationDeploymentField = keyof WorkspaceApplicationDeploymentSelection;

export interface WorkspaceApplicationDeploymentValidation {
  ok: boolean;
  fieldErrors: Partial<Record<WorkspaceApplicationDeploymentField, string>>;
  advancedJSONError: string;
}

export function emptyWorkspaceApplicationDeploymentSelection(): WorkspaceApplicationDeploymentSelection {
  return {
    platform: "linux/amd64", image: "", exposurePolicy: "application",
    healthCheckPath: "", healthCheckPort: "", healthCheckInitialDelaySeconds: ""
  };
}

const applicationPlatformPattern = /^[a-z0-9]+\/[a-z0-9._-]+$/;
const imageDigestPattern = /^[^@\s]+@sha256:[0-9a-f]{64}$/;
const exposurePolicies = ["anonymous", "application", "cloud_private"];

function validPort(value: string): boolean {
  return /^[0-9]{1,5}$/.test(value) && Number(value) >= 1 && Number(value) <= 65535;
}

// validateWorkspaceApplicationDeploymentForm keeps the operator's own mistakes
// out of the request. Control Plane stays the authority for the description it
// completes; this only refuses what the selection itself cannot express.
export function validateWorkspaceApplicationDeploymentForm(form: WorkspaceApplicationDeploymentForm): WorkspaceApplicationDeploymentValidation {
  const fieldErrors: Partial<Record<WorkspaceApplicationDeploymentField, string>> = {};
  const { selection } = form;
  if (!applicationPlatformPattern.test(selection.platform)) fieldErrors.platform = "格式为 os/arch，如 linux/amd64";
  if (!imageDigestPattern.test(selection.image)) fieldErrors.image = "先选择命名空间、repository 与 tag，解析出固定 digest";
  if (!exposurePolicies.includes(selection.exposurePolicy)) fieldErrors.exposurePolicy = "选择一种暴露策略";
  if (selection.healthCheckPath !== "" && !selection.healthCheckPath.startsWith("/")) fieldErrors.healthCheckPath = "健康检查路径需以 / 开头";
  if (selection.healthCheckPort !== "" && !validPort(selection.healthCheckPort)) fieldErrors.healthCheckPort = "端口为 1-65535 的数字";
  if (selection.healthCheckPath !== "" && selection.healthCheckPort === "") fieldErrors.healthCheckPort = "请填写健康检查端口";
  if (selection.healthCheckPort !== "" && selection.healthCheckPath === "") fieldErrors.healthCheckPath = "请填写健康检查路径";
  if (selection.healthCheckInitialDelaySeconds !== "" && !/^[0-9]{1,5}$/.test(selection.healthCheckInitialDelaySeconds)) {
    fieldErrors.healthCheckInitialDelaySeconds = "初始延迟为秒数";
  }
  let advancedJSONError = "";
  try { parseWorkspaceApplicationAdvancedJSON(form.advancedJSON); } catch (error) { advancedJSONError = (error as Error).message; }
  return { ok: Object.keys(fieldErrors).length === 0 && advancedJSONError === "", fieldErrors, advancedJSONError };
}

// Preserve the publisher's optional description. It carries the run
// requirements an image does not declare for itself, such as supporting
// components, configuration/Secret interfaces or resource limits.
export function parseWorkspaceApplicationAdvancedJSON(text: string): Record<string, unknown> {
  if (text.trim() === "") return {};
  const value = parseApplicationJSON(text);
  if (!applicationJSONObject(value)) throw new Error("高级运行描述需为 JSON 对象");
  for (const identity of ["applicationId", "version"]) {
    if (identity in value) throw new Error("高级运行描述不填写应用标识与版本；它们由平台从镜像确定");
  }
  return value;
}

// composeWorkspaceApplicationRevision builds the description this one command
// deploys. The selected image and exposure policy are the operator's visible
// choice and always win; the advanced description supplies everything else, and
// a health check the operator filled is added on top.
export function composeWorkspaceApplicationRevision(form: WorkspaceApplicationDeploymentForm): Record<string, unknown> {
  const { selection } = form;
  const revision: Record<string, unknown> = {
    schemaVersion: 1,
    ...parseWorkspaceApplicationAdvancedJSON(form.advancedJSON),
    platform: selection.platform,
    image: selection.image,
    exposurePolicy: selection.exposurePolicy
  };
  if (selection.healthCheckPath !== "" && selection.healthCheckPort !== "") {
    const healthCheck: Record<string, unknown> = { port: Number(selection.healthCheckPort), path: selection.healthCheckPath };
    if (selection.healthCheckInitialDelaySeconds !== "") healthCheck.initialDelaySeconds = Number(selection.healthCheckInitialDelaySeconds);
    revision.healthChecks = [healthCheck];
  }
  return revision;
}

export interface WorkspaceApplicationPhasePresentation {
  label: string;
  tone: "info" | "success" | "warning" | "danger";
}

const phaseOrder = ["intent", "predecessor_suspending", "runtime", "activating", "retiring", "receipt", "active"];

export function presentWorkspaceApplicationDeploymentPhase(phase: string): WorkspaceApplicationPhasePresentation {
  switch (phase) {
    case "intent":
      return { label: "准备部署", tone: "info" };
    case "predecessor_suspending":
      return { label: "暂停原应用", tone: "info" };
    case "runtime":
      return { label: "创建组件", tone: "info" };
    case "activating":
      return { label: "切换当前应用", tone: "info" };
    case "retiring":
      return { label: "清理原应用实例", tone: "info" };
    case "receipt":
      return { label: "记录部署证据", tone: "info" };
    case "active":
      return { label: "部署完成", tone: "success" };
    case "manual_review":
      return { label: "待人工处理", tone: "danger" };
    default:
      return { label: "状态待确认", tone: "warning" };
  }
}

export function workspaceApplicationDeploymentPhaseSteps(phase: string): Array<{ label: string; state: "done" | "current" | "upcoming" | "failed" }> {
  if (phase === "manual_review") return [];
  const currentIndex = phaseOrder.indexOf(phase);
  return phaseOrder.map((step, index) => ({
    label: presentWorkspaceApplicationDeploymentPhase(step).label,
    state: index < currentIndex ? "done" : index === currentIndex ? "current" : "upcoming"
  }));
}

export function presentWorkspaceApplicationComponentState(state: string): WorkspaceApplicationPhasePresentation {
  switch (state) {
    case "ready":
      return { label: "运行中", tone: "success" };
    case "pending":
      return { label: "启动中", tone: "info" };
    case "absent":
      return { label: "未创建", tone: "warning" };
    case "failed":
      return { label: "失败", tone: "danger" };
    default:
      return { label: "状态待确认", tone: "warning" };
  }
}

export interface WorkspaceApplicationIntentPresentation {
  phase: WorkspaceApplicationPhasePresentation;
  steps: Array<{ label: string; state: "done" | "current" | "upcoming" | "failed" }>;
  components: Array<WorkspaceApplicationComponentStateDTO & WorkspaceApplicationPhasePresentation>;
  isTerminal: boolean;
}

export function presentWorkspaceApplicationIntent(intent: WorkspaceApplicationIntentDTO): WorkspaceApplicationIntentPresentation {
  return {
    phase: presentWorkspaceApplicationDeploymentPhase(intent.phase),
    steps: workspaceApplicationDeploymentPhaseSteps(intent.phase),
    components: (intent.runtimeObservation?.components || []).map((component) => ({
      ...component,
      ...presentWorkspaceApplicationComponentState(component.state)
    })),
    isTerminal: intent.phase === "active" || intent.phase === "manual_review"
  };
}

function applicationJSONObject(value: unknown): value is Record<string, unknown> {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}

function parseApplicationJSON(text: string): unknown {
  try { return JSON.parse(text); } catch { throw new Error("请输入有效 JSON"); }
}

// Preserve the publisher's complete description. Admission owns domain validation.
export function parseWorkspaceApplicationRevisionJSON(text: string): Record<string, unknown> {
  const value = parseApplicationJSON(text);
  if (!applicationJSONObject(value) || typeof value.applicationId !== "string" || !value.applicationId
    || typeof value.version !== "string" || !value.version) {
    throw new Error("应用描述需为包含 applicationId 与 version 的 JSON 对象");
  }
  return value;
}

export function parseWorkspaceApplicationDeploymentJSON(configurationText: string, bindingsText: string): {
  configuration: WorkspaceApplicationConfigurationDTO;
  secretBindings: WorkspaceApplicationSecretBindingDTO[];
} {
  const configuration = parseApplicationJSON(configurationText);
  if (!applicationJSONObject(configuration) || Object.keys(configuration).some((key) => key !== "environment" && key !== "files")
    || Object.values(configuration).some((value) => !applicationJSONObject(value) || Object.values(value).some((item) => typeof item !== "string"))) {
    throw new Error("运行配置仅允许 environment/files 字符串映射；凭据必须使用 Secret 引用");
  }
  const bindings = parseApplicationJSON(bindingsText);
  const fields = ["name", "secretRef", "version", "key"];
  if (!Array.isArray(bindings) || bindings.some((binding) => !applicationJSONObject(binding)
    || Object.keys(binding).length !== fields.length || Object.keys(binding).some((key) => !fields.includes(key))
    || fields.some((key) => typeof binding[key] !== "string" || !binding[key].trim()))) {
    throw new Error("Secret 绑定须为数组，每项仅允许非空 name、secretRef、version、key 引用；不接受密钥值");
  }
  return { configuration: configuration as WorkspaceApplicationConfigurationDTO, secretBindings: bindings as WorkspaceApplicationSecretBindingDTO[] };
}
