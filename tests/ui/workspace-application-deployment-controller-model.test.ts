import assert from "node:assert/strict";
import test from "node:test";

import {
  composeWorkspaceApplicationRevision,
  emptyWorkspaceApplicationRevisionDraft,
  presentWorkspaceApplicationComponentState,
  presentWorkspaceApplicationDeploymentPhase,
  validateWorkspaceApplicationRevisionDraft,
  workspaceApplicationDeploymentConfigurationDigestValid
} from "../../apps/console-ui/src/app/workspace-application-deployment-controller-model.ts";

function draft(overrides: Partial<Parameters<typeof validateWorkspaceApplicationRevisionDraft>[0]> = {}) {
  return { ...emptyWorkspaceApplicationRevisionDraft(), ...overrides };
}

function filledDraft(overrides: Partial<Parameters<typeof validateWorkspaceApplicationRevisionDraft>[0]> = {}) {
  return draft({
    applicationId: "knowledge-app", version: "1.0.0", platform: "linux/amd64",
    image: "repo.example/apps/knowledge@sha256:" + "a".repeat(64), ...overrides
  });
}

test("A blank draft fails so the submit button stays disabled", () => {
  assert.equal(validateWorkspaceApplicationRevisionDraft(emptyWorkspaceApplicationRevisionDraft()).ok, false);
});

test("Structured registration draft passes when every field matches the contract", () => {
  const validation = validateWorkspaceApplicationRevisionDraft(filledDraft());
  assert.equal(validation.ok, true);
  assert.deepEqual(validation.fieldErrors, {});
  assert.deepEqual(validation.mountErrors, {});
  assert.deepEqual(validation.dependencyErrors, {});
});

test("Per-field errors keep mistakes out of the request", () => {
  const validation = validateWorkspaceApplicationRevisionDraft(draft({
    applicationId: "Knowledge App", version: "", platform: "linux", image: "repo.example/app:latest", exposurePolicy: "public"
  }));
  assert.equal(validation.ok, false);
  assert.match(validation.fieldErrors.applicationId ?? "", /小写字母/);
  assert.match(validation.fieldErrors.version ?? "", /字母或数字开头/);
  assert.match(validation.fieldErrors.platform ?? "", /os\/arch/);
  assert.match(validation.fieldErrors.image ?? "", /sha256/);
  assert.match(validation.fieldErrors.exposurePolicy ?? "", /暴露策略/);
});

test("Mount and dependency rows validate names, paths and uniqueness", () => {
  const validation = validateWorkspaceApplicationRevisionDraft(filledDraft({
    persistentMounts: [{ name: "data", mountPath: "/data" }, { name: "data", mountPath: "/data2" }],
    scratchMounts: [{ name: "", mountPath: "" }],
    dependencies: [{ name: "retrieval", image: "repo.example/svc" }, { name: "retrieval", image: "" }]
  }));
  assert.match(validation.mountErrors[1] ?? "", /重复/);
  assert.match(validation.scratchMountErrors[0] ?? "", /挂载名/);
  assert.match(validation.dependencyErrors[1] ?? "", /重复/);
});

test("The composed revision carries only what the administrator declared", () => {
  const base = composeWorkspaceApplicationRevision(filledDraft());
  assert.equal(base.schemaVersion, 1);
  assert.equal(base.exposurePolicy, "application");
  assert.deepEqual(base.persistentMounts, [{ name: "data", mountPath: "/data" }]);
  assert.equal("dependencies" in base, false);
  assert.deepEqual(base.healthChecks, [{ port: 8080, path: "/healthz", initialDelaySeconds: 5 }]);
  const withDependency = composeWorkspaceApplicationRevision(filledDraft({
    dependencies: [{ name: "retrieval", image: "repo.example/svc@sha256:" + "b".repeat(64) }]
  }));
  assert.deepEqual(withDependency.dependencies, [{ name: "retrieval", image: "repo.example/svc@sha256:" + "b".repeat(64) }]);
});

test("Deployment configuration digest requires 64 hex characters", () => {
  assert.equal(workspaceApplicationDeploymentConfigurationDigestValid("a".repeat(64)), true);
  assert.equal(workspaceApplicationDeploymentConfigurationDigestValid("a".repeat(63)), false);
  assert.equal(workspaceApplicationDeploymentConfigurationDigestValid("A".repeat(64)), false);
});

test("Deployment phases present in the operator's language", () => {
  assert.deepEqual(presentWorkspaceApplicationDeploymentPhase("intent"), { label: "准备部署", tone: "info" });
  assert.deepEqual(presentWorkspaceApplicationDeploymentPhase("runtime"), { label: "创建组件", tone: "info" });
  assert.deepEqual(presentWorkspaceApplicationDeploymentPhase("activating"), { label: "原子切换应用绑定", tone: "info" });
  assert.deepEqual(presentWorkspaceApplicationDeploymentPhase("receipt"), { label: "记录部署证据", tone: "info" });
  assert.deepEqual(presentWorkspaceApplicationDeploymentPhase("active"), { label: "部署完成", tone: "success" });
  assert.deepEqual(presentWorkspaceApplicationDeploymentPhase("manual_review"), { label: "待人工处理", tone: "danger" });
  assert.deepEqual(presentWorkspaceApplicationDeploymentPhase("mystery"), { label: "状态待确认", tone: "warning" });
});

test("Component states present with the operator's vocabulary", () => {
  assert.deepEqual(presentWorkspaceApplicationComponentState("ready"), { label: "运行中", tone: "success" });
  assert.deepEqual(presentWorkspaceApplicationComponentState("pending"), { label: "启动中", tone: "info" });
  assert.deepEqual(presentWorkspaceApplicationComponentState("absent"), { label: "未创建", tone: "warning" });
  assert.deepEqual(presentWorkspaceApplicationComponentState("failed"), { label: "失败", tone: "danger" });
  assert.deepEqual(presentWorkspaceApplicationComponentState("other"), { label: "状态待确认", tone: "warning" });
});
