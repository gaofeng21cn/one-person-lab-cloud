import assert from "node:assert/strict";
import test from "node:test";

import {
  composeWorkspaceApplicationRevision,
  emptyWorkspaceApplicationDeploymentSelection,
  parseWorkspaceApplicationAdvancedJSON,
  parseWorkspaceApplicationDeploymentJSON,
  presentWorkspaceApplicationComponentState,
  presentWorkspaceApplicationDeploymentPhase,
  validateWorkspaceApplicationDeploymentForm,
  workspaceApplicationDeploymentPhaseSteps
} from "../../apps/console-ui/src/app/workspace-application-deployment-controller-model.ts";

function form(overrides: {
  selection?: Partial<ReturnType<typeof emptyWorkspaceApplicationDeploymentSelection>>;
  advancedJSON?: string;
} = {}) {
  return { selection: { ...emptyWorkspaceApplicationDeploymentSelection(), ...overrides.selection }, advancedJSON: overrides.advancedJSON ?? "" };
}

function selectedForm(overrides: {
  selection?: Partial<ReturnType<typeof emptyWorkspaceApplicationDeploymentSelection>>;
  advancedJSON?: string;
} = {}) {
  return form({
    ...overrides,
    selection: { image: "uswccr.ccs.tencentyun.com/oplcloud/chaokang_agent_ibd@sha256:" + "a".repeat(64), ...overrides.selection }
  });
}

test("An unfilled selection cannot be deployed", () => {
  const validation = validateWorkspaceApplicationDeploymentForm(form());
  assert.equal(validation.ok, false);
  assert.match(String(validation.fieldErrors.image), /digest/);
});

test("A selected image with an exposure policy is deployable without any registration", () => {
  const validation = validateWorkspaceApplicationDeploymentForm(selectedForm());
  assert.equal(validation.ok, true, JSON.stringify(validation));
  assert.deepEqual(validation.fieldErrors, {});
  assert.equal(validation.advancedJSONError, "");
});

test("The composed description carries the selection and only what the operator added", () => {
  const revision = composeWorkspaceApplicationRevision(selectedForm());
  assert.deepEqual(revision, {
    schemaVersion: 1,
    platform: "linux/amd64",
    image: "uswccr.ccs.tencentyun.com/oplcloud/chaokang_agent_ibd@sha256:" + "a".repeat(64),
    exposurePolicy: "application"
  });
  // Run requirements the image declares for itself are Control Plane's to read:
  // the operator states none of them here.
  for (const derived of ["ports", "entryPort", "persistentMounts", "execution", "applicationId", "version"]) {
    assert.equal(derived in revision, false, `${derived} must not be invented by the form`);
  }
});

test("A filled health check and an advanced description join the same command", () => {
  const revision = composeWorkspaceApplicationRevision(selectedForm({
    selection: { exposurePolicy: "anonymous", healthCheckPath: "/api/health", healthCheckPort: "8082", healthCheckInitialDelaySeconds: "60" },
    advancedJSON: JSON.stringify({ dependencies: [{ name: "retrieval", image: "repo.example/svc@sha256:" + "b".repeat(64) }], compute: { memoryRequestBytes: 2147483648 } })
  }));
  assert.deepEqual(revision.healthChecks, [{ port: 8082, path: "/api/health", initialDelaySeconds: 60 }]);
  assert.deepEqual(revision.dependencies, [{ name: "retrieval", image: "repo.example/svc@sha256:" + "b".repeat(64) }]);
  assert.deepEqual(revision.compute, { memoryRequestBytes: 2147483648 });
  // The visible selection wins over a description that disagrees with it.
  const conflicting = composeWorkspaceApplicationRevision(selectedForm({
    selection: { exposurePolicy: "cloud_private" }, advancedJSON: JSON.stringify({ exposurePolicy: "anonymous", image: "other.example/app@sha256:" + "c".repeat(64) })
  }));
  assert.equal(conflicting.exposurePolicy, "cloud_private");
  assert.equal(conflicting.image, "uswccr.ccs.tencentyun.com/oplcloud/chaokang_agent_ibd@sha256:" + "a".repeat(64));
});

test("An advanced description never carries the identity or the version", () => {
  for (const text of ['{"applicationId":"app"}', '{"version":"1.0.0"}']) {
    assert.throws(() => parseWorkspaceApplicationAdvancedJSON(text));
  }
  assert.deepEqual(parseWorkspaceApplicationAdvancedJSON("   "), {});
  for (const text of ["{", "[]", "null"]) {
    assert.throws(() => parseWorkspaceApplicationAdvancedJSON(text));
  }
  assert.equal(validateWorkspaceApplicationDeploymentForm(selectedForm({ advancedJSON: "{" })).ok, false);
});

test("Invalid ports and partial probes cannot be submitted", () => {
  for (const port of ["0", "65536", "99999", "1.5", "-1"]) {
    assert.equal(validateWorkspaceApplicationDeploymentForm(selectedForm({ selection: { healthCheckPort: port } })).ok, false);
  }
  assert.equal(validateWorkspaceApplicationDeploymentForm(selectedForm({ selection: { healthCheckPath: "/api/health" } })).ok, false);
  assert.equal(validateWorkspaceApplicationDeploymentForm(selectedForm({ selection: { healthCheckPort: "8082" } })).ok, false);
  assert.equal(validateWorkspaceApplicationDeploymentForm(selectedForm({ selection: { healthCheckInitialDelaySeconds: "later" } })).ok, false);
  assert.equal(validateWorkspaceApplicationDeploymentForm(selectedForm({ selection: { exposurePolicy: "public" } })).ok, false);
  assert.equal(validateWorkspaceApplicationDeploymentForm(selectedForm({ selection: { platform: "linux" } })).ok, false);
});

test("A tag reference from the registry never passes validation as an image", () => {
  const validation = validateWorkspaceApplicationDeploymentForm(form({
    selection: { image: "uswccr.ccs.tencentyun.com/oplcloud/chaokang_agent_ibd:single-20260919-modelcfg" }
  }));
  assert.equal(validation.ok, false);
  assert.match(String(validation.fieldErrors.image), /digest/);
});

test("Manual review does not claim that an unknown failed stage reached receipt", () => {
  assert.deepEqual(workspaceApplicationDeploymentPhaseSteps("manual_review"), []);
});

test("Deployment phases present in the operator's language", () => {
  assert.deepEqual(presentWorkspaceApplicationDeploymentPhase("intent"), { label: "准备部署", tone: "info" });
  assert.deepEqual(presentWorkspaceApplicationDeploymentPhase("runtime"), { label: "创建组件", tone: "info" });
  assert.deepEqual(presentWorkspaceApplicationDeploymentPhase("predecessor_suspending"), { label: "暂停原应用", tone: "info" });
  assert.deepEqual(presentWorkspaceApplicationDeploymentPhase("activating"), { label: "切换当前应用", tone: "info" });
  assert.deepEqual(presentWorkspaceApplicationDeploymentPhase("retiring"), { label: "清理原应用实例", tone: "info" });
  assert.deepEqual(presentWorkspaceApplicationDeploymentPhase("receipt"), { label: "记录部署证据", tone: "info" });
  assert.deepEqual(presentWorkspaceApplicationDeploymentPhase("active"), { label: "部署完成", tone: "success" });
  assert.deepEqual(presentWorkspaceApplicationDeploymentPhase("manual_review"), { label: "待人工处理", tone: "danger" });
  assert.deepEqual(presentWorkspaceApplicationDeploymentPhase("mystery"), { label: "状态待确认", tone: "warning" });
});

test("Replacement progress does not report completion before previous instances are retired", () => {
  const steps = workspaceApplicationDeploymentPhaseSteps("retiring");
  assert.equal(steps.find((step) => step.label === "切换当前应用")?.state, "done");
  assert.equal(steps.find((step) => step.label === "清理原应用实例")?.state, "current");
  assert.equal(steps.find((step) => step.label === "部署完成")?.state, "upcoming");
});

test("Component states present with the operator's vocabulary", () => {
  assert.deepEqual(presentWorkspaceApplicationComponentState("ready"), { label: "运行中", tone: "success" });
  assert.deepEqual(presentWorkspaceApplicationComponentState("pending"), { label: "启动中", tone: "info" });
  assert.deepEqual(presentWorkspaceApplicationComponentState("absent"), { label: "未创建", tone: "warning" });
  assert.deepEqual(presentWorkspaceApplicationComponentState("failed"), { label: "失败", tone: "danger" });
  assert.deepEqual(presentWorkspaceApplicationComponentState("other"), { label: "状态待确认", tone: "warning" });
});

test("Manual review does not claim that an unknown failed stage reached receipt", () => {
  assert.deepEqual(workspaceApplicationDeploymentPhaseSteps("manual_review"), []);
});

test("Deployment phases present in the operator's language", () => {
  assert.deepEqual(presentWorkspaceApplicationDeploymentPhase("intent"), { label: "准备部署", tone: "info" });
  assert.deepEqual(presentWorkspaceApplicationDeploymentPhase("runtime"), { label: "创建组件", tone: "info" });
  assert.deepEqual(presentWorkspaceApplicationDeploymentPhase("predecessor_suspending"), { label: "暂停原应用", tone: "info" });
  assert.deepEqual(presentWorkspaceApplicationDeploymentPhase("activating"), { label: "切换当前应用", tone: "info" });
  assert.deepEqual(presentWorkspaceApplicationDeploymentPhase("retiring"), { label: "清理原应用实例", tone: "info" });
  assert.deepEqual(presentWorkspaceApplicationDeploymentPhase("receipt"), { label: "记录部署证据", tone: "info" });
  assert.deepEqual(presentWorkspaceApplicationDeploymentPhase("active"), { label: "部署完成", tone: "success" });
  assert.deepEqual(presentWorkspaceApplicationDeploymentPhase("manual_review"), { label: "待人工处理", tone: "danger" });
  assert.deepEqual(presentWorkspaceApplicationDeploymentPhase("mystery"), { label: "状态待确认", tone: "warning" });
});

test("Replacement progress does not report completion before previous instances are retired", () => {
  const steps = workspaceApplicationDeploymentPhaseSteps("retiring");
  assert.equal(steps.find((step) => step.label === "切换当前应用")?.state, "done");
  assert.equal(steps.find((step) => step.label === "清理原应用实例")?.state, "current");
  assert.equal(steps.find((step) => step.label === "部署完成")?.state, "upcoming");
});

test("Component states present with the operator's vocabulary", () => {
  assert.deepEqual(presentWorkspaceApplicationComponentState("ready"), { label: "运行中", tone: "success" });
  assert.deepEqual(presentWorkspaceApplicationComponentState("pending"), { label: "启动中", tone: "info" });
  assert.deepEqual(presentWorkspaceApplicationComponentState("absent"), { label: "未创建", tone: "warning" });
  assert.deepEqual(presentWorkspaceApplicationComponentState("failed"), { label: "失败", tone: "danger" });
  assert.deepEqual(presentWorkspaceApplicationComponentState("other"), { label: "状态待确认", tone: "warning" });
});

test("An advanced description preserves complete component inputs without re-composition", () => {
  const revision = { entrypoint: ["app", "serve"], execution: { userId: 1000, init: true }, configInputs: [{ name: "settings", target: "/etc/app/settings" }],
    dependencies: [{ name: "database", image: "repo/db@sha256:" + "b".repeat(64), dependsOn: ["bootstrap"],
      command: { argv: ["database"], env: { MODE: "stable" } }, execution: { userId: 1001 },
      secretInputs: [{ name: "password", env: "DB_PASSWORD" }], persistentMounts: [{ name: "db", mountPath: "/var/lib/db", readOnly: false }] }] };
  assert.deepEqual(parseWorkspaceApplicationAdvancedJSON(JSON.stringify(revision)), revision);
  const composed = composeWorkspaceApplicationRevision(selectedForm({ advancedJSON: JSON.stringify(revision) }));
  assert.deepEqual(composed.dependencies, revision.dependencies);
  assert.deepEqual(composed.configInputs, revision.configInputs);
});

test("Deployment JSON preserves files and only accepts immutable Secret reference fields", () => {
  const configuration = { environment: { APP_MODE: "stable" }, files: { settings: "line 1\nline 2" } };
  const secretBindings = [{ name: "database", secretRef: "secret-db", version: "v1", key: "password" }];
  assert.deepEqual(parseWorkspaceApplicationDeploymentJSON(JSON.stringify(configuration), JSON.stringify(secretBindings)), { configuration, secretBindings });
  for (const value of [{ files: { settings: 1 } }, { environment: [] }, { credentialVersion: "x" }, { files: null }]) {
    assert.throws(() => parseWorkspaceApplicationDeploymentJSON(JSON.stringify(value), "[]"));
  }
  for (const value of [null, {}, [{ ...secretBindings[0], value: "not-accepted" }], [{ ...secretBindings[0], key: "" }], [{ name: "database", secretRef: "db" }]]) {
    assert.throws(() => parseWorkspaceApplicationDeploymentJSON("{}", JSON.stringify(value)));
  }
});
