# Console 移动订阅卡片实现计划

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 按已批准的 UX-03G-B，让移动订阅卡片默认呈现完成订阅核对所需的 Workspace 身份、套餐、金额、完整计费周期和续费状态，同时保持所有费用业务和读取边界不变。

**Architecture:** 只调整 `BillingPage` 的移动卡片 JSX 和现有费用 CSS；桌面表格继续使用当前结构。卡片继续复用 Customer Workspace Read Controller、现有 Workspace DTO 和 `presentWorkspaceRenewal`，不增加状态管理、API、DTO 或写操作。

**Tech Stack:** React 19, TypeScript, existing Console UI components, existing CSS tokens, Playwright browser tests, Vite.

---

### Task 1: 写移动订阅字段层级的失败测试

**Files:**
- Modify: `tests/ui/customer-console-task-experience-browser.test.ts`
- Test target: `apps/console-ui/src/pages/CustomerPages.tsx` 的 `.billing-list-mobile`

**Step 1: Write the failing test**

在已有双视口费用测试中，为移动视图增加断言：第一张订阅卡片默认包含 `Pilot Workspace`、`Workspace ID: ws-1`、`BASIC`、`$52.58`、`2026/07/01`、`2026/08/01`、`手动续费`、`自动续费` 和 `关闭`；断言卡片仍是链接并指向 `/console/workspaces/ws-1`。桌面测试继续断言当前表格字段。

测试只使用现有 customer fixture 和 `WorkspaceDTO` 返回值，不构造 `map[string]any`，不增加新的合同类型。

**Step 2: Run test to verify it fails**

Run: `node --test tests/ui/customer-console-task-experience-browser.test.ts`

Expected: FAIL because the current mobile card only renders name, package, amount and `paidThrough`.

---

### Task 2: 调整移动订阅卡片 JSX

**Files:**
- Modify: `apps/console-ui/src/pages/CustomerPages.tsx:217`

**Step 1: Implement the minimal structure**

- 保留桌面 `.billing-table-desktop` 的表头、列顺序、金额格式化和续费映射。
- 在 `.billing-list-mobile` 的每个 `PageLink` 内分成四个语义区域：对象身份、套餐与金额、计费周期、续费判断。
- 对象身份显示 Workspace 名称和 `Workspace ID: <id>`；没有有效 ID 时显示 `Workspace ID 暂不可用`，不构造详情路径。
- 套餐与金额继续使用 `packageId` 和 `formatUsdMicros(item.totalUsdMicros)`；缺失值显示 `暂不可用`，不在浏览器计算金额。
- 计费周期同时使用 `periodStart` 和 `paidThrough`；任一缺失时整体显示 `计费周期：暂不可用`，不只保留截止日期。
- 续费状态复用 `presentWorkspaceRenewal(item.renewalStatus).label`；`autoRenew` 只接受 `true/false`，其他值显示 `暂不可用`。
- 保留 `ChevronRight` 作为方向提示和 `PageLink` 的既有导航行为，不增加续费开关、刷新、删除或其他写操作。

**Step 2: Run typecheck**

Run: `npm run typecheck`

Expected: PASS。

---

### Task 3: 调整移动卡片样式和稳定布局

**Files:**
- Modify: `apps/console-ui/src/styles.css:2413-2455`

**Step 1: Implement the minimal styles**

- 仅在移动断点调整 `.billing-list-mobile > a` 的 grid/flex 结构，保持卡片宽度受父容器约束。
- 为对象、套餐金额、周期和续费区域建立稳定的纵向行间距；允许长名称和日期换行，不使用截断隐藏业务事实。
- 让金额保持右侧强调，但周期和续费信息占据完整可读宽度。
- 保留既有 hover/focus/active 样式；整卡 focus ring 仍可见。
- 保证整卡和文本触控区域至少 `44px` 高，不使用颜色作为唯一状态表达。
- 不改变桌面表格、收据列表、收据详情或其他页面的 CSS。

**Step 2: Run CSS/source checks**

Run: `git diff --check && npm run lint`

Expected: PASS。

---

### Task 4: 运行费用回归与双视口验证

**Files:**
- Modify: `tests/ui/customer-console-task-experience-browser.test.ts` only if selectors need refinement
- Create outside repository: `/tmp/ux03g-c-2026-09-05/`

**Step 1: Run focused browser tests**

Run: `npm run test:browser:customer-experience`

Expected: PASS for desktop and mobile customer hierarchy, terminology, billing fields and no horizontal overflow。

**Step 2: Run billing Controller regression**

Run: `npm run test:browser:billing`

Expected: 9/9 PASS; list/detail freshness, cursor pagination, route/session reset and independent failure behavior unchanged。

**Step 3: Capture visual evidence**

Use the fake-only demo at `1280x900` and `390x844` to capture the subscription view and confirm:

- 移动第一张卡片在无需进入详情时包含全部决策字段；
- 长文本不重叠，页面 `scrollWidth === clientWidth`；
- 桌面表格保持现有业务列；
- 无外部请求、应用 page error 或新增 HMR 以外的 Console error。

**Step 4: Run repository verification**

Run: `npm run verify:local`

Expected: PASS。若出现与本改动无关的既有环境故障，只记录证据，不扩大实现范围。

---

### Task 5: 写 UX-03G-C 验证记录并本地提交

**Files:**
- Create: `docs/history/2026-09-05-console-billing-mobile-subscription-ui-verification.md`
- Modify: `docs/history/README.md`

**Step 1: Record evidence**

记录实现 commit、双视口截图路径和哈希、字段可见性、异常状态、无横向溢出、focused browser tests、请求边界和未解决的 Receipt ID 层级问题。明确本地 fake-only 证据不代表生产资格。

**Step 2: Commit**

```bash
git add apps/console-ui/src/pages/CustomerPages.tsx apps/console-ui/src/styles.css tests/ui/customer-console-task-experience-browser.test.ts docs/history/2026-09-05-console-billing-mobile-subscription-ui-verification.md docs/history/README.md
git commit -m "feat(console): complete mobile subscription hierarchy"
```

不要 Push、提 PR 或合并主干；整个 UI/UX 重建完成后统一处理。
