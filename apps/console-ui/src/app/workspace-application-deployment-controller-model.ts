import type { WorkspaceApplicationComponentStateDTO, WorkspaceApplicationIntentDTO } from "../api/dtos.ts";

export interface WorkspaceApplicationRevisionMountDraft {
  name: string;
  mountPath: string;
}

export interface WorkspaceApplicationRevisionDependencyDraft {
  name: string;
  image: string;
}

// The structured registration draft: every field an administrator fills by
// hand. The model composes the authoritative revision object from it, so the
// form never touches revision JSON directly.
export interface WorkspaceApplicationRevisionDraft {
  applicationId: string;
  version: string;
  platform: string;
  image: string;
  exposurePolicy: string;
  healthCheckPath: string;
  healthCheckPort: string;
  cpu: string;
  memoryGb: string;
  persistentMounts: WorkspaceApplicationRevisionMountDraft[];
  scratchMounts: WorkspaceApplicationRevisionMountDraft[];
  dependencies: WorkspaceApplicationRevisionDependencyDraft[];
}

export function emptyWorkspaceApplicationRevisionDraft(): WorkspaceApplicationRevisionDraft {
  return {
    applicationId: "", version: "", platform: "linux/amd64", image: "",
    exposurePolicy: "application", healthCheckPath: "/healthz", healthCheckPort: "8080",
    cpu: "2", memoryGb: "4",
    persistentMounts: [{ name: "data", mountPath: "/data" }],
    scratchMounts: [], dependencies: []
  };
}

const applicationIdPattern = /^[a-z][a-z0-9-]{0,62}$/;
const applicationVersionPattern = /^[A-Za-z0-9][A-Za-z0-9._+-]{0,63}$/;
const applicationPlatformPattern = /^[a-z0-9]+\/[a-z0-9._-]+$/;
const imageDigestPattern = /^[^@\s]+@sha256:[0-9a-f]{64}$/;
const configurationDigestPattern = /^[a-f0-9]{64}$/;
const exposurePolicies = ["anonymous", "application", "cloud_private"];
const mountNamePattern = /^[a-z][a-z0-9-]{0,30}$/;
const mountPathPattern = /^\/[A-Za-z0-9._/-]+$/;

export type WorkspaceApplicationRevisionField = keyof Pick<
  WorkspaceApplicationRevisionDraft,
  "applicationId" | "version" | "platform" | "image" | "exposurePolicy" | "healthCheckPort" | "cpu" | "memoryGb"
>;

export interface WorkspaceApplicationRevisionDraftValidation {
  ok: boolean;
  fieldErrors: Partial<Record<WorkspaceApplicationRevisionField, string>>;
  mountErrors: Record<number, string>;
  scratchMountErrors: Record<number, string>;
  dependencyErrors: Record<number, string>;
}

const dependencyImagePattern = /^[^@\s]+(@sha256:[0-9a-f]{64})?$/;

function validateMountDrafts(mounts: WorkspaceApplicationRevisionMountDraft[]): Record<number, string> {
  const errors: Record<number, string> = {};
  const seen = new Set<string>();
  mounts.forEach((mount, index) => {
    if (!mountNamePattern.test(mount.name)) errors[index] = "挂载名需为小写字母开头的短标识";
    else if (!mountPathPattern.test(mount.mountPath)) errors[index] = "挂载路径需为绝对路径";
    else if (seen.has(mount.name)) errors[index] = "挂载名重复";
    seen.add(mount.name);
  });
  return errors;
}

// validateWorkspaceApplicationRevisionDraft mirrors the authoritative Control
// Plane admission contract for every field the administrator fills. The
// backend stays the admission authority; per-field errors keep mistakes out of
// the browser request.
export function validateWorkspaceApplicationRevisionDraft(draft: WorkspaceApplicationRevisionDraft): WorkspaceApplicationRevisionDraftValidation {
  const fieldErrors: Partial<Record<WorkspaceApplicationRevisionField, string>> = {};
  if (!applicationIdPattern.test(draft.applicationId)) fieldErrors.applicationId = "小写字母开头，仅含小写字母、数字或连字符";
  if (!applicationVersionPattern.test(draft.version)) fieldErrors.version = "字母或数字开头，仅含字母、数字与 . _ + -";
  if (!applicationPlatformPattern.test(draft.platform)) fieldErrors.platform = "格式为 os/arch，如 linux/amd64";
  if (!imageDigestPattern.test(draft.image)) fieldErrors.image = "需为 repository@sha256:<64位十六进制>";
  if (!exposurePolicies.includes(draft.exposurePolicy)) fieldErrors.exposurePolicy = "选择一种暴露策略";
  if (draft.healthCheckPath !== "" && !mountPathPattern.test(draft.healthCheckPath)) fieldErrors.healthCheckPort = "健康检查路径需以 / 开头";
  if (draft.healthCheckPort !== "" && !/^[0-9]{1,5}$/.test(draft.healthCheckPort)) fieldErrors.healthCheckPort = "端口为 1-65535 的数字";
  if (!/^[1-9][0-9]*$/.test(draft.cpu)) fieldErrors.cpu = "CPU 为正整数核数";
  if (!/^[1-9][0-9]*$/.test(draft.memoryGb)) fieldErrors.memoryGb = "内存为正整数 GB";
  const mountErrors = validateMountDrafts(draft.persistentMounts);
  const scratchMountErrors = validateMountDrafts(draft.scratchMounts);
  const dependencyErrors: Record<number, string> = {};
  const seenDependencies = new Set<string>();
  draft.dependencies.forEach((dependency, index) => {
    if (!mountNamePattern.test(dependency.name)) dependencyErrors[index] = "服务名需为小写字母开头的短标识";
    else if (seenDependencies.has(dependency.name)) dependencyErrors[index] = "服务名重复";
    else if (!dependencyImagePattern.test(dependency.image)) dependencyErrors[index] = "镜像需为仓库引用（建议 digest 钉死）";
    seenDependencies.add(dependency.name);
  });
  const ok = Object.keys(fieldErrors).length === 0 && Object.keys(mountErrors).length === 0
    && Object.keys(scratchMountErrors).length === 0 && Object.keys(dependencyErrors).length === 0;
  return { ok, fieldErrors, mountErrors, scratchMountErrors, dependencyErrors };
}

// composeWorkspaceApplicationRevision builds the authoritative revision
// object from the structured draft: the main component carries the declared
// readiness probe and resources; dependencies become private services.
export function composeWorkspaceApplicationRevision(draft: WorkspaceApplicationRevisionDraft): Record<string, unknown> {
  const revision: Record<string, unknown> = {
    schemaVersion: 1,
    applicationId: draft.applicationId,
    version: draft.version,
    platform: draft.platform,
    image: draft.image,
    exposurePolicy: draft.exposurePolicy,
    resources: { cpu: Number(draft.cpu), memoryGb: Number(draft.memoryGb) }
  };
  if (draft.healthCheckPath !== "" && draft.healthCheckPort !== "") {
    revision.healthChecks = [{ port: Number(draft.healthCheckPort), path: draft.healthCheckPath, initialDelaySeconds: 5 }];
  }
  if (draft.persistentMounts.length > 0) {
    revision.persistentMounts = draft.persistentMounts;
  }
  if (draft.scratchMounts.length > 0) {
    revision.scratchMounts = draft.scratchMounts;
  }
  if (draft.dependencies.length > 0) {
    revision.dependencies = draft.dependencies;
  }
  return revision;
}

export function workspaceApplicationDeploymentConfigurationDigestValid(configurationDigest: string): boolean {
  return configurationDigestPattern.test(configurationDigest);
}

export interface WorkspaceApplicationPhasePresentation {
  label: string;
  tone: "info" | "success" | "warning" | "danger";
}

const phaseOrder = ["intent", "runtime", "activating", "receipt", "active"];

export function presentWorkspaceApplicationDeploymentPhase(phase: string): WorkspaceApplicationPhasePresentation {
  switch (phase) {
    case "intent":
      return { label: "准备部署", tone: "info" };
    case "runtime":
      return { label: "创建组件", tone: "info" };
    case "activating":
      return { label: "原子切换应用绑定", tone: "info" };
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
  const currentIndex = phase === "manual_review" ? phaseOrder.indexOf("receipt") : phaseOrder.indexOf(phase);
  return phaseOrder.map((step, index) => ({
    label: presentWorkspaceApplicationDeploymentPhase(step).label,
    state: index < currentIndex ? "done" : index === currentIndex ? (phase === "manual_review" ? "failed" : "current") : "upcoming"
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
