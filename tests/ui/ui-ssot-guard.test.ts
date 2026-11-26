import assert from "node:assert/strict";
import { readdirSync, readFileSync } from "node:fs";
import path from "node:path";
import test from "node:test";

import { customerSafeMessage } from "../../apps/console-ui/src/api/console-api.ts";
import {
  presentAgentStatus,
  presentWorkspaceApplicationAvailability,
  presentWorkspaceResourceReadiness
} from "../../apps/console-ui/src/app/workspace-experience-model.ts";

const CONSOLE_UI_SRC = path.resolve(import.meta.dirname, "../../apps/console-ui/src");

function listFiles(dir: string, exts: string[]): string[] {
  const results: string[] = [];
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    const full = path.join(dir, entry.name);
    if (entry.isDirectory()) {
      results.push(...listFiles(full, exts));
    } else if (exts.some((ext) => entry.name.endsWith(ext))) {
      results.push(full);
    }
  }
  return results;
}

test("Design Token SSOT: only components/ui/tokens.css declares :root custom properties", () => {
  const cssFiles = listFiles(CONSOLE_UI_SRC, [".css"]);
  const allowed = path.join(CONSOLE_UI_SRC, "components/ui/tokens.css");
  const violations: string[] = [];

  for (const file of cssFiles) {
    if (file === allowed) continue;
    const content = readFileSync(file, "utf8");
    if (/^:root\b/m.test(content)) {
      violations.push(path.relative(CONSOLE_UI_SRC, file));
    }
  }

  assert.deepEqual(
    violations,
    [],
    `competing :root blocks found outside components/ui/tokens.css: ${violations.join(", ")}`
  );
});

test("UI Primitive SSOT: CSS does not use brittle hashed SDK _Badge_ class selectors", () => {
  const cssFiles = listFiles(CONSOLE_UI_SRC, [".css"]);
  const violations: string[] = [];

  for (const file of cssFiles) {
    const content = readFileSync(file, "utf8");
    if (/_Badge_/.test(content)) {
      violations.push(path.relative(CONSOLE_UI_SRC, file));
    }
  }

  assert.deepEqual(
    violations,
    [],
    `brittle hashed _Badge_ selectors must use .ui-badge instead: ${violations.join(", ")}`
  );
});

test("Customer copy SSOT: no internal manual jargon or fabricated ETA in customer UI sources", () => {
  const uiFiles = [
    "pages/PublicPages.tsx",
    "pages/AgentPages.tsx",
    "pages/PublisherPage.tsx",
    "components/workspaces/WorkspaceListPage.tsx",
    "components/workspaces/WorkspaceDetailPage.tsx",
    "components/workspaces/WorkspaceLaunchPage.tsx",
    "api/console-api.ts"
  ];
  const forbidden = [
    "Control Plane 读取",
    "Capability owner readback",
    "OCI 字节销毁",
    "预计 3-5 分钟",
    "没有 operator 权限",
    "defaultForNewBuilds",
    "按负责服务展示当前"
  ];
  const violations: string[] = [];

  for (const rel of uiFiles) {
    const content = readFileSync(path.join(CONSOLE_UI_SRC, rel), "utf8");
    for (const phrase of forbidden) {
      if (content.includes(phrase)) {
        violations.push(`${rel}: ${phrase}`);
      }
    }
  }

  assert.deepEqual(violations, [], `forbidden internal jargon found:\n${violations.join("\n")}`);
});

test("Shared status presenters translate readiness, availability and agent statuses to Chinese", () => {
  assert.equal(presentWorkspaceResourceReadiness("pending"), "处理中");
  assert.equal(presentWorkspaceResourceReadiness("ready"), "已就绪");
  assert.equal(presentWorkspaceResourceReadiness("unavailable"), "不可用");
  assert.equal(presentWorkspaceResourceReadiness(undefined), "暂不可用");
  assert.equal(presentWorkspaceResourceReadiness("future_state"), "待确认");

  assert.equal(presentWorkspaceApplicationAvailability("pending"), "处理中");
  assert.equal(presentWorkspaceApplicationAvailability("available"), "可用");
  assert.equal(presentWorkspaceApplicationAvailability("unavailable"), "不可用");
  assert.equal(presentWorkspaceApplicationAvailability(undefined), "暂不可用");
  assert.equal(presentWorkspaceApplicationAvailability("future_state"), "待确认");

  assert.equal(presentAgentStatus("active"), "已启用");
  assert.equal(presentAgentStatus("ready"), "可部署");
  assert.equal(presentAgentStatus("uploaded"), "已上传");
  assert.equal(presentAgentStatus("registering"), "登记中");
  assert.equal(presentAgentStatus("succeeded"), "已完成");
  assert.equal(presentAgentStatus("failed"), "失败");
  assert.equal(presentAgentStatus(undefined), "暂不可用");
});

test("customerSafeMessage omits Docker jargon and fabricated ETAs on runtime-not-ready errors", () => {
  const msg = customerSafeMessage({ error: "workspace_runtime_not_ready" });
  assert.equal(msg, "工作空间正在启动，请稍后刷新查看。");
  assert.doesNotMatch(msg, /Docker|URL|3-5/);
});

test("ConsoleShell registers the Bot icon referenced by customerMenu", () => {
  const shell = readFileSync(path.join(CONSOLE_UI_SRC, "layout/ConsoleShell.tsx"), "utf8");
  const model = readFileSync(path.join(CONSOLE_UI_SRC, "console-model.ts"), "utf8");
  assert.match(model, /icon:\s*"Bot"/);
  assert.match(shell, /\bBot\b/);
});

test("UI Primitive SSOT: zero native window.confirm or window.prompt calls in console-ui", () => {
  const tsFiles = listFiles(CONSOLE_UI_SRC, [".ts", ".tsx"]);
  const violations: string[] = [];

  for (const file of tsFiles) {
    const content = readFileSync(file, "utf8");
    if (/\bwindow\.(confirm|prompt)\s*\(/.test(content)) {
      violations.push(path.relative(CONSOLE_UI_SRC, file));
    }
  }

  assert.deepEqual(
    violations,
    [],
    `native window.confirm/prompt must be replaced by requestConfirm/requestPrompt Modal: ${violations.join(", ")}`
  );
});
