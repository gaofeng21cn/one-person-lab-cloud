import assert from "node:assert/strict";
import test from "node:test";

import {
  composeWorkspaceApplicationRevision,
  parseWorkspaceApplicationRevisionJSON,
  parseWorkspaceApplicationDeploymentJSON,
  emptyWorkspaceApplicationRevisionDraft,
  presentWorkspaceApplicationComponentState,
  presentWorkspaceApplicationDeploymentPhase,
  validateWorkspaceApplicationRevisionDraft,
  workspaceApplicationDeploymentPhaseSteps
} from "../../apps/console-ui/src/app/workspace-application-deployment-controller-model.ts";

function draft(overrides: Partial<Parameters<typeof validateWorkspaceApplicationRevisionDraft>[0]> = {}) {
  return { ...emptyWorkspaceApplicationRevisionDraft(), ...overrides };
}

function filledDraft(overrides: Partial<Parameters<typeof validateWorkspaceApplicationRevisionDraft>[0]> = {}) {
  return draft({
    applicationId: "knowledge-app", version: "1.0.0", platform: "linux/amd64",
    image: "repo.example/apps/knowledge@sha256:" + "a".repeat(64),
    // The draft declares the facts it needs; nothing is prefilled for it.
    httpPort: "8080", healthCheckPath: "/healthz", healthCheckPort: "8080",
    persistentMounts: [{ name: "data", mountPath: "/data" }], ...overrides
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
    applicationId: "Knowledge App", version: "-1.0", platform: "linux", image: "repo.example/app:latest", exposurePolicy: "public"
  }));
  assert.equal(validation.ok, false);
  assert.match(validation.fieldErrors.applicationId ?? "", /小写字母/);
  assert.match(validation.fieldErrors.version ?? "", /字母或数字开头/);
  assert.match(validation.fieldErrors.platform ?? "", /os\/arch/);
  assert.match(validation.fieldErrors.image ?? "", /sha256/);
  assert.match(validation.fieldErrors.exposurePolicy ?? "", /暴露策略/);
  // An omitted identity is not an error: the platform derives it from the digest.
  const anonymousInline = validateWorkspaceApplicationRevisionDraft(draft({
    applicationId: "", version: "", image: "repo.example/app@sha256:" + "f".repeat(64), httpPort: "8080"
  }));
  assert.equal(anonymousInline.fieldErrors.applicationId, undefined);
  assert.equal(anonymousInline.fieldErrors.version, undefined);
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
  const reserved = validateWorkspaceApplicationRevisionDraft(filledDraft({
    dependencies: [{ name: "main", image: "" }, { name: "redis-", image: "" }]
  }));
  assert.match(reserved.dependencyErrors[0] ?? "", /保留/);
  assert.match(reserved.dependencyErrors[1] ?? "", /服务名/);
});

test("The composed revision carries only what the administrator declared", () => {
  const base = composeWorkspaceApplicationRevision(filledDraft());
  assert.equal(base.schemaVersion, 1);
  assert.equal(base.exposurePolicy, "application");
  assert.deepEqual(base.persistentMounts, [{ name: "data", mountPath: "/data" }]);
  assert.equal("dependencies" in base, false);
  assert.deepEqual(base.healthChecks, [{ port: 8080, path: "/healthz", initialDelaySeconds: 5 }]);
  assert.deepEqual(base.ports, [{ name: "http", port: 8080, protocol: "TCP" }]);
  assert.equal(base.entryPort, "http");
  const withDependency = composeWorkspaceApplicationRevision(filledDraft({
    dependencies: [{ name: "retrieval", image: "repo.example/svc@sha256:" + "b".repeat(64) }]
  }));
  assert.deepEqual(withDependency.dependencies, [{ name: "retrieval", image: "repo.example/svc@sha256:" + "b".repeat(64) }]);
});

test("HTTP entry and health checks are independent declarations", () => {
  const revision = composeWorkspaceApplicationRevision(filledDraft({ httpPort: "8082", healthCheckPort: "9000" }));
  assert.deepEqual(revision.ports, [{ name: "http", port: 8082, protocol: "TCP" }]);
  assert.deepEqual(revision.healthChecks, [{ port: 9000, path: "/healthz", initialDelaySeconds: 5 }]);
  const worker = composeWorkspaceApplicationRevision(filledDraft({ httpPort: "" }));
  assert.equal("ports" in worker, false);
  assert.equal("entryPort" in worker, false);
  assert.ok(worker.healthChecks);
});

test("Invalid ports, partial probes and mutable dependency images cannot be submitted", () => {
  for (const port of ["0", "65536", "99999", "1.5", "-1"]) {
    assert.equal(validateWorkspaceApplicationRevisionDraft(filledDraft({ httpPort: port })).ok, false);
    assert.equal(validateWorkspaceApplicationRevisionDraft(filledDraft({ healthCheckPort: port })).ok, false);
  }
  assert.equal(validateWorkspaceApplicationRevisionDraft(filledDraft({ healthCheckPort: "" })).ok, false);
  assert.equal(validateWorkspaceApplicationRevisionDraft(filledDraft({ healthCheckPath: "" })).ok, false);
  assert.equal(validateWorkspaceApplicationRevisionDraft(filledDraft({ healthCheckPath: "", healthCheckPort: "" })).ok, true);
  assert.equal(validateWorkspaceApplicationRevisionDraft(filledDraft({ dependencies: [{ name: "cache", image: "repo.example/cache:latest" }] })).ok, false);
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

test("A registry-resolved digest-pinned reference passes the draft validation unchanged", () => {
  const digest = "b".repeat(64);
  const resolved = draft({
    applicationId: "chaokang-agent-ibd", version: "1.0.0",
    image: `uswccr.ccs.tencentyun.com/oplcloud/chaokang_agent_ibd@sha256:${digest}`,
    // This image publishes an entry, so its real port is declared rather than
    // inherited from a platform default.
    httpPort: "8082"
  });
  const validation = validateWorkspaceApplicationRevisionDraft(resolved);
  assert.equal(validation.ok, true, JSON.stringify(validation.fieldErrors));
  const revision = composeWorkspaceApplicationRevision(resolved) as { image: string };
  assert.equal(revision.image, `uswccr.ccs.tencentyun.com/oplcloud/chaokang_agent_ibd@sha256:${digest}`);
});

test("A tag reference from the registry never passes validation as an image", () => {
  const validation = validateWorkspaceApplicationRevisionDraft(draft({
    applicationId: "chaokang-agent-ibd", version: "1.0.0",
    image: "uswccr.ccs.tencentyun.com/oplcloud/chaokang_agent_ibd:v1.0.0"
  }));
  assert.equal(validation.ok, false);
  assert.match(String(validation.fieldErrors.image), /sha256/);
});


test("Publisher JSON preserves complete component inputs and execution without re-composition", () => {
  const revision = { applicationId: "knowledge-app", version: "1", image: "repo/app@sha256:" + "a".repeat(64),
    entrypoint: ["app", "serve"], execution: { userId: 1000, init: true }, configInputs: [{ name: "settings", target: "/etc/app/settings" }],
    dependencies: [{ name: "database", image: "repo/db@sha256:" + "b".repeat(64), dependsOn: ["bootstrap"],
      command: { argv: ["database"], env: { MODE: "stable" } }, execution: { userId: 1001 },
      secretInputs: [{ name: "password", env: "DB_PASSWORD" }], persistentMounts: [{ name: "db", mountPath: "/var/lib/db", readOnly: false }] }] };
  assert.deepEqual(parseWorkspaceApplicationRevisionJSON(JSON.stringify(revision)), revision);
  for (const text of ["{", "[]", "null", "{}", '{"applicationId":"app","version":1}']) {
    assert.throws(() => parseWorkspaceApplicationRevisionJSON(text));
  }
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

test("A blank draft declares nothing, so no port, probe or data mount is fabricated", () => {
  const blank = emptyWorkspaceApplicationRevisionDraft();
  // An image decides its own port, health check and data directory. Prefilling
  // 8080, /healthz or /data would silently deploy a fabricated target.
  assert.equal(blank.httpPort, "");
  assert.equal(blank.healthCheckPath, "");
  assert.equal(blank.healthCheckPort, "");
  assert.deepEqual(blank.persistentMounts, []);
  assert.deepEqual(blank.dependencies, []);
});

test("A simple image composes only the facts it declares", () => {
  const simple = draft({
    applicationId: "simple-app", version: "1.0.0", image: "repo.example/simple@sha256:" + "c".repeat(64),
    httpPort: "3000"
  });
  assert.equal(validateWorkspaceApplicationRevisionDraft(simple).ok, true);
  const composed = composeWorkspaceApplicationRevision(simple);
  assert.deepEqual(composed.ports, [{ name: "http", port: 3000, protocol: "TCP" }]);
  assert.equal(composed.entryPort, "http");
  // The image needs no probe, no data directory and no dependencies, so the
  // revision carries none of them.
  assert.equal("healthChecks" in composed, false);
  assert.equal("persistentMounts" in composed, false);
  assert.equal("scratchMounts" in composed, false);
  assert.equal("dependencies" in composed, false);
});

test("A published exposure without its real port is an explicit error, never a default", () => {
  for (const exposurePolicy of ["anonymous", "application"]) {
    const validation = validateWorkspaceApplicationRevisionDraft(draft({
      applicationId: "simple-app", version: "1.0.0", image: "repo.example/simple@sha256:" + "d".repeat(64),
      exposurePolicy, httpPort: ""
    }));
    assert.equal(validation.ok, false);
    // The message asks for the image's real port instead of naming a default.
    assert.match(validation.fieldErrors.httpPort ?? "", /实际监听/);
    assert.doesNotMatch(validation.fieldErrors.httpPort ?? "", /8080/);
  }
  // An image that publishes nothing declares no port and stays valid.
  const privateImage = draft({
    applicationId: "batch-job", version: "1", image: "repo.example/batch@sha256:" + "e".repeat(64),
    exposurePolicy: "cloud_private", httpPort: ""
  });
  assert.equal(validateWorkspaceApplicationRevisionDraft(privateImage).ok, true);
  assert.equal("ports" in composeWorkspaceApplicationRevision(privateImage), false);
});
