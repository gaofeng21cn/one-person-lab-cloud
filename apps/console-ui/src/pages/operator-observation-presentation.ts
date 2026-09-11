import type { OperatorFabricHealthDTO } from "../api/dtos.ts";

const observationLabels: Record<string, string> = {
  ready: "已就绪",
  running: "运行中",
  stopped: "已停止",
  suspended: "已暂停",
  attached: "已挂载",
  detached: "未挂载",
  pending: "处理中",
  pending_deletion: "停止待销毁",
  absent: "已不存在",
  unknown: "尚未核验",
  attention: "需处理",
  provisioning: "开通中",
  deleting: "销毁中",
  expired_unpaid: "到期未续费",
  verified: "归属已核验",
  unregistered: "未登记",
  conflict: "归属冲突"
};

export function observationLabel(value?: string): string {
  return value ? observationLabels[value] || value : "暂不可用";
}

const reasonLabels: Record<string, string> = {
  compute_provider_partial_identity_machine_missing_tke_instance_missing: "CVM 仍存在，但已无 TKE 节点池 Machine 和集群实例关联",
  compute_provider_partial_identity_machine_missing: "CVM 仍存在，节点池 Machine 关联缺失，集群实例关联尚未核验",
  compute_provider_partial_identity_machine_missing_tke_instance_ambiguous: "CVM 仍存在，但 TKE 集群返回多个关联候选，需要核对归属",
  compute_provider_partial_identity_machine_missing_tke_instance_identity_mismatch: "CVM 仍存在，但 TKE 集群关联身份不一致",
  compute_provider_partial_identity_machine_missing_tke_instance_wrong_pool: "CVM 仍存在，但 TKE 实例不在原节点池",
  compute_provider_partial_identity_machine_missing_machine_inventory_missing: "CVM 与 TKE 集群实例仍有关联，但节点池 Machine 清单缺失",
  compute_provider_partial_identity_cvm_missing: "节点池 Machine 仍存在，但原 CVM 已不存在",
  fabric_resource_observation_unavailable: "资源现态尚未核验",
  runtime_unmatched_workspace: "实物没有对应的当前 Workspace 记录",
  runtime_operation_without_workspace: "运行对象仍有关联业务操作，需核对 Workspace 记录",
  runtime_ownership_unregistered: "运行对象尚未登记归属",
  runtime_ownership_conflict: "运行对象归属冲突",
  runtime_binding_mismatch: "运行对象与 Workspace 绑定不一致",
  runtime_multiple_objects: "同一 Workspace 对应多个运行对象",
  runtime_missing: "应运行的 Workspace 缺少运行对象",
  runtime_unexpected_suspension: "应运行的 Workspace 被暂停",
  runtime_suspend_incomplete: "Workspace 暂停尚未完成",
  runtime_not_ready: "运行对象尚未就绪",
  runtime_absent_while_suspended: "已暂停 Workspace 的运行对象不存在",
  workspace_delete_in_progress: "Workspace 正在删除",
  workspace_delete_incomplete: "Workspace 删除尚未完成",
  workspace_delete_state_invalid: "Workspace 删除状态需要核对",
  workspace_launch_state_invalid: "Workspace 开通状态需要核对",
  workspace_renewal_state_invalid: "Workspace 续费状态需要核对",
  workspace_runtime_not_created: "开通尚未创建运行对象",
  workspace_billing_state_invalid: "Workspace 权益状态需要核对",
  workspace_billing_manual_review: "Workspace 计费结果待核对",
  workspace_billing_period_expired: "Workspace 权益已到期",
  workspace_image_id: "Workspace 运行镜像尚未确认与安装默认一致",
  cloud_image_id: "Cloud 服务镜像未通过发布目标校验"
};

export function workspaceImageStatusMessage(status: NonNullable<OperatorFabricHealthDTO["workspaceImageStatus"]>): string {
  switch (status) {
    case "installed_target_matches": return "Workspace 运行镜像与安装目标一致。";
    case "workspace_targets_verified": return "存量 Workspace 与安装默认版本不同；各自固定目标与实际运行镜像已核验。";
    case "no_running_sample": return "暂无可核验的运行 Workspace，安装目标一致性尚未验证。";
    case "identity_unverified": return "Workspace 镜像身份尚未核验，需要核对固定目标、实际镜像和控制器归属。";
  }
}

export function observationReason(reasonCode?: string): string {
  if (!reasonCode) return "";
  return reasonLabels[reasonCode] ? `${reasonLabels[reasonCode]}（${reasonCode}）` : `原因代码：${reasonCode}`;
}
