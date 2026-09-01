import assert from "node:assert/strict";
import test from "node:test";

import type {
  WorkspaceDTO,
  WorkspaceGatewayBudgetDTO,
  WorkspaceLaunchResponse,
  WorkspaceRuntimeDTO
} from "../../apps/console-ui/src/api/dtos.ts";
import {
  presentWorkspaceBudget,
  presentWorkspaceLaunch,
  presentWorkspaceLaunchStage,
  presentWorkspaceQuote,
  presentWorkspaceRenewal,
  presentWorkspaceRuntime
} from "../../apps/console-ui/src/app/workspace-experience-model.ts";

function launch(overrides: Partial<WorkspaceLaunchResponse> = {}): WorkspaceLaunchResponse {
  return {
    operationId: "launch-alpha",
    status: "pending",
    phase: "key",
    accountId: "account-alpha",
    name: "Research Alpha",
    packageId: "basic",
    sizeGb: 50,
    autoRenew: true,
    priceVersion: "2026-09",
    currency: "USD",
    totalChargeUsdMicros: 9_000_000,
    ...overrides
  };
}

function runtime(overrides: Partial<WorkspaceRuntimeDTO> = {}): WorkspaceRuntimeDTO {
  return {
    workspaceId: "workspace-alpha",
    status: "running",
    ready: true,
    url: "https://workspace.example.invalid/alpha",
    checks: [],
    ...overrides
  };
}

function workspace(renewalStatus: string): WorkspaceDTO {
  return {
    id: "workspace-alpha",
    ownerAccountId: "account-alpha",
    ownerUserId: "user-alpha",
    state: "active",
    createdAt: "2026-09-01T00:00:00Z",
    updatedAt: "2026-09-01T00:00:00Z",
    renewalStatus
  };
}

function budget(status: WorkspaceGatewayBudgetDTO["status"]): WorkspaceGatewayBudgetDTO {
  return {
    workspaceId: "workspace-alpha",
    keyId: "key-alpha",
    status,
    quotaUsdMicros: "9000000",
    quotaUsedUsdMicros: "0",
    rateLimit5hUsdMicros: "500000",
    rateLimit1dUsdMicros: "1000000",
    rateLimit7dUsdMicros: "4000000",
    usage5hUsdMicros: "0",
    usage1dUsdMicros: "0",
    usage7dUsdMicros: "0",
    enabled: status === "active",
    updatedAt: "2026-09-01T00:00:00Z"
  };
}

test("Workspace launch statuses produce exact customer outcomes", () => {
  const cases: ReadonlyArray<{
    status: string;
    expected: {
      kind: string;
      title: string;
      summary: string;
      tone: string;
      canOpenWorkspace: boolean;
    };
  }> = [
    {
      status: "pending",
      expected: {
        kind: "pending",
        title: "正在准备工作空间",
        summary: "系统正在准备所需资源，请稍后刷新状态。",
        tone: "info",
        canOpenWorkspace: false
      }
    },
    {
      status: "manual_review",
      expected: {
        kind: "manual_review",
        title: "需要人工处理",
        summary: "订单已保留，工作人员正在处理，请稍后刷新状态。",
        tone: "warning",
        canOpenWorkspace: false
      }
    },
    {
      status: "succeeded",
      expected: {
        kind: "succeeded",
        title: "工作空间已可使用",
        summary: "工作空间已完成开通，可以继续查看并进入。",
        tone: "success",
        canOpenWorkspace: true
      }
    },
    {
      status: "failed",
      expected: {
        kind: "failed",
        title: "开通失败",
        summary: "工作空间未能完成开通，请查看技术详情或联系支持。",
        tone: "danger",
        canOpenWorkspace: false
      }
    },
    {
      status: "refunded",
      expected: {
        kind: "refunded",
        title: "开通失败，费用已退回",
        summary: "工作空间未能完成开通，本次费用已退回。",
        tone: "danger",
        canOpenWorkspace: false
      }
    }
  ];

  for (const { status, expected } of cases) {
    const operation = launch({
      status,
      workspaceId: status === "succeeded" ? "workspace-alpha" : undefined
    });
    assert.deepEqual(presentWorkspaceLaunch(operation), expected);
  }
});

test("unknown Workspace launch status is explicitly unconfirmed and cannot open", () => {
  assert.deepEqual(presentWorkspaceLaunch(launch({
    status: "future_status",
    workspaceId: "workspace-alpha"
  })), {
    kind: "unconfirmed",
    title: "结果待确认",
    summary: "当前开通结果尚未确认，请刷新状态，暂勿重复购买。",
    tone: "warning",
    canOpenWorkspace: false,
    rawValue: "future_status"
  });
});

test("a succeeded launch without a Workspace identity cannot open", () => {
  assert.equal(presentWorkspaceLaunch(launch({ status: "succeeded" })).canOpenWorkspace, false);
});

test("Workspace launch stages use only exact current stage values", () => {
  const cases: ReadonlyArray<{ stage: string; label: string }> = [
    { stage: "key", label: "准备访问凭据" },
    { stage: "debit", label: "确认费用" },
    { stage: "ensure_compute_allocation", label: "准备计算资源" },
    { stage: "storage", label: "准备存储空间" },
    { stage: "attachment", label: "连接存储空间" },
    { stage: "secret", label: "配置登录凭据" },
    { stage: "runtime", label: "启动工作空间" },
    { stage: "activation", label: "确认工作空间可用" },
    { stage: "receipt", label: "记录开通结果" },
    { stage: "succeeded", label: "开通完成" }
  ];

  for (const { stage, label } of cases) {
    assert.deepEqual(presentWorkspaceLaunchStage(launch({ phase: stage }).phase), {
      known: true,
      kind: stage,
      label
    });
  }

  assert.deepEqual(presentWorkspaceLaunchStage(launch({ phase: "runtime_future" }).phase), {
    known: false,
    kind: "unknown",
    label: "等待服务更新处理阶段",
    rawValue: "runtime_future"
  });
});

test("Workspace Runtime presentation separates every availability outcome", () => {
  const cases: ReadonlyArray<{
    value: WorkspaceRuntimeDTO;
    expected: {
      kind: string;
      label: string;
      description: string;
      canOpen: boolean;
      url: string | null;
    };
  }> = [
    {
      value: runtime(),
      expected: {
        kind: "ready",
        label: "可使用",
        description: "工作空间已就绪，可以打开。",
        canOpen: true,
        url: "https://workspace.example.invalid/alpha"
      }
    },
    {
      value: runtime({ status: "unready", ready: false, url: undefined }),
      expected: {
        kind: "unready",
        label: "正在准备",
        description: "运行环境尚未就绪，请稍后刷新。",
        canOpen: false,
        url: null
      }
    },
    {
      value: runtime({ status: "not_found", ready: false, url: undefined }),
      expected: {
        kind: "not_found",
        label: "运行环境不存在",
        description: "尚未找到该工作空间的运行环境。",
        canOpen: false,
        url: null
      }
    },
    {
      value: runtime({ status: "destroyed", ready: false, url: undefined }),
      expected: {
        kind: "destroyed",
        label: "已停止",
        description: "工作空间的运行环境已销毁，无法打开。",
        canOpen: false,
        url: null
      }
    },
    {
      value: runtime({ url: undefined }),
      expected: {
        kind: "unconfirmed",
        label: "入口待确认",
        description: "运行环境已就绪，但访问入口尚未确认，请刷新状态。",
        canOpen: false,
        url: null
      }
    }
  ];

  for (const { value, expected } of cases) {
    assert.deepEqual(presentWorkspaceRuntime(value), expected);
  }
});

test("Workspace renewal statuses have exact labels and an explicit unknown", () => {
  const cases: ReadonlyArray<{ status: string; label: string }> = [
    { status: "active", label: "有效" },
    { status: "not_applicable", label: "不适用" },
    { status: "expired_unpaid", label: "已到期，未续费" },
    { status: "manual", label: "手动续费" }
  ];

  for (const { status, label } of cases) {
    assert.deepEqual(presentWorkspaceRenewal(workspace(status).renewalStatus), {
      known: true,
      kind: status,
      label
    });
  }

  assert.deepEqual(presentWorkspaceRenewal(workspace("future_renewal").renewalStatus), {
    known: false,
    kind: "unknown",
    label: "待确认",
    rawValue: "future_renewal"
  });
});

test("Workspace budget statuses have exact labels and an explicit unknown", () => {
  const cases: ReadonlyArray<{
    status: WorkspaceGatewayBudgetDTO["status"];
    label: string;
  }> = [
    { status: "active", label: "已启用" },
    { status: "disabled", label: "已停用" },
    { status: "quota_exhausted", label: "额度已用尽" },
    { status: "expired", label: "已过期" }
  ];

  for (const { status, label } of cases) {
    assert.deepEqual(presentWorkspaceBudget(budget(status).status), {
      known: true,
      kind: status,
      label
    });
  }

  assert.deepEqual(presentWorkspaceBudget("future_budget"), {
    known: false,
    kind: "unknown",
    label: "待确认",
    rawValue: "future_budget"
  });
});

test("Workspace quote presentation preserves unavailable, included zero, and prepaid totals", () => {
  assert.deepEqual(presentWorkspaceQuote({
    selectedPriceUsdMicros: null,
    customerOwned: false
  }), {
    kind: "unavailable",
    totalUsdMicros: null,
    confirmationLabel: "价格确认后才能开通工作空间",
    submitLabel: "价格暂不可用",
    requiresPrepayment: false,
    canConfirm: false
  });

  assert.deepEqual(presentWorkspaceQuote({
    selectedPriceUsdMicros: 0,
    customerOwned: true
  }), {
    kind: "included",
    totalUsdMicros: 0,
    confirmationLabel: "我确认使用当前客户权益开通工作空间（无需预付）",
    submitLabel: "确认并开通",
    requiresPrepayment: false,
    canConfirm: true
  });

  assert.deepEqual(presentWorkspaceQuote({
    selectedPriceUsdMicros: 9_000_000,
    customerOwned: false
  }), {
    kind: "prepaid",
    totalUsdMicros: 9_000_000,
    confirmationLabel: "我确认一次性预付工作空间月度总额并开通",
    submitLabel: "确认预付并开通",
    requiresPrepayment: true,
    canConfirm: true
  });
});

test("Workspace quote presentation rejects price and ownership contradictions", () => {
  const unconfirmed = {
    kind: "unconfirmed",
    totalUsdMicros: null,
    confirmationLabel: "价格结果待确认，暂不能开通工作空间",
    submitLabel: "价格待确认",
    requiresPrepayment: false,
    canConfirm: false
  } as const;

  assert.deepEqual(presentWorkspaceQuote({
    selectedPriceUsdMicros: 9_000_000,
    customerOwned: true
  }), unconfirmed);

  assert.deepEqual(presentWorkspaceQuote({
    selectedPriceUsdMicros: 0,
    customerOwned: false
  }), unconfirmed);
});
