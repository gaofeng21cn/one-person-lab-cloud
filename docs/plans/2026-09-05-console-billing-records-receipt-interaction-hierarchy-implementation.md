# Console 账单记录与收据详情实现计划

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 在客户 Console 中让账单记录稳定显示交易对应的工作空间身份，并只呈现普通客户完成历史交易核对所需的收据业务字段。

**Architecture:** 只修改 `apps/console-ui` 的费用 Presentation 和现有浏览器验收。Receipt 列表、Receipt 详情、Workspace 投影和 Controller 的读取边界保持不变；`workspaceId` 继续来自已有 Receipt DTO，Workspace 名称继续来自现有 Customer Workspace Read Controller。技术证据仍保留在 Ledger/typed DTO，但从普通客户 DOM 移除。

**Tech Stack:** React 19、TypeScript、Vite、Playwright、Node test runner、现有 Console CSS。

---

## 约束

- 不修改 `apps/console-ui/src/api/dtos.ts`、`use-billing-controller.ts`、Ledger、Control Plane、Gateway、Sub2API、数据库或部署。
- 不新增 Receipt API、筛选、退款、下载、支付、Support 工单或新的 Billing owner。
- 客户文案使用“工作空间编号”，不直接显示机器字段名 `workspaceId`。
- 普通客户 DOM 不出现 `Receipt ID`、`priceVersion`、`chargeReference`、`compute evidence`、`storage evidence`、`fulfillment` 或原始 `type/status`。
- 新增或改写的测试继续使用 `BillingReceipt` 等 `packages/contracts/go` 对应的 typed 数据边界；不在测试中构造或断言 `map[string]any`。

### Task 1: 先锁定客户可见字段和技术字段不泄漏的失败断言

**Files:**
- Modify: `tests/ui/billing-controller-browser.test.ts`
- Modify: `tools/console-browser-qa.ts`

**Step 1: 更新费用 Controller browser test 的可见定位方式**

- 将 `receiptRow` 从依赖 `.receipt-row-technical-details` 改为依赖账单列表中的可见“工作空间编号”及其值；保留每个测试对 Receipt 身份的区分能力。
- 将 `openReceiptTechnicalDetails` 替换为只等待收据业务详情的 helper，使用 Workspace 编号或详情字段确认选中的 Receipt。
- 旧的迟到响应、分页、路由离开、Session reset 和列表/详情失败隔离断言改用 `workspaceId` 作为客户可见身份，不再从 DOM 读取 Receipt ID。

**Step 2: 添加本切片的客户路径断言**

- Desktop 和 Mobile 的账单记录列表必须出现“工作空间编号”和对应值。
- 收据详情必须出现类型、状态、日期、金额、计费周期、工作空间名称和工作空间编号。
- 客户费用区域中 `.receipt-technical-details` 与 `.receipt-row-technical-details` 必须不存在；`priceVersion` 等技术值不得出现在客户 DOM。

**Step 3: 让 fake-only 全流程验收断言新边界**

- 保留现有双视口截图和横向溢出检查。
- 打开账单记录后断言“工作空间编号”可见，并断言技术详情 disclosure 不存在；不再点击或读取 `pilot-usd-2026-07-v1`。

**Step 4: 运行测试确认当前实现按预期失败**

Run: `npm run test:browser:billing`

Expected: FAIL，因为当前页面尚未显示账单列表的工作空间编号，且仍渲染技术详情。

Run: `node tools/console-browser-qa.ts --network=fake-only`

Expected: FAIL at the new billing assertions for the same reasons；不得出现 API、Controller 或外部网络错误。

### Task 2: 实现账单列表和收据详情的最小 Presentation 调整

**Files:**
- Modify: `apps/console-ui/src/pages/CustomerPages.tsx:1-244`

**Step 1: 集中工作空间身份的客户呈现规则**

- 在当前页面文件内复用一个局部 Presentation 结构或等价的明确 JSX，统一输出 Workspace 名称和“工作空间编号”。
- Workspace 投影缺失时名称显示“工作空间名称暂不可用”；`workspaceId` 缺失时编号显示“暂不可用”；不从名称、URL 或其他数据推断。
- 不把原始字段名 `workspaceId`、`Workspace ID` 写进客户标签。

**Step 2: 更新 Desktop 账单表格**

- 保持现有六列和 `openReceipt(receiptId)` 行为不变。
- Workspace 单元格增加工作空间编号辅助文本，不增加新的表格列或请求。
- 移除类型单元格中的 `.receipt-row-technical-details` disclosure 及所有原始字段渲染。

**Step 3: 更新 Mobile 账单卡片**

- 保留整卡按钮和当前金额/状态布局。
- 在交易类型下显示 Workspace 名称、工作空间编号和发生日期，金额与状态继续保持结果层级。
- 使用已有 Receipt DTO 的 `workspaceId`；不为移动端创建新 API 或二次读取。

**Step 4: 更新收据业务详情**

- 保留类型、状态、日期、金额、计费周期和工作空间业务字段。
- 工作空间详情同时显示名称与工作空间编号。
- 移除 `.receipt-technical-details` disclosure、`components` 局部变量和所有技术证据 JSX。
- 保留 `SourceState` 的 loading、empty、unavailable、error、retry、关闭详情和 Receipt freshness 行为。

**Step 5: 清理不再使用的图标导入**

- 如果 `ChevronDown` 在该页面没有其他调用，删除其 import；不得顺便重排无关页面或组件。

### Task 3: 调整费用 Presentation 样式并保持双视口稳定

**Files:**
- Modify: `apps/console-ui/src/styles.css:2390-2460, 5090-5190`

**Step 1: 删除已废弃技术详情的费用专用样式**

- 移除 `.receipt-technical-details`、`.receipt-technical-details__body` 和 `.receipt-row-technical-details` 在费用页面中的专用规则。
- 保留其他 Key/Usage 技术 disclosure 的样式，不扩大删除范围。

**Step 2: 为 Workspace 身份增加受范围约束的样式**

- 为 Desktop 表格中的名称/编号和 Mobile 卡片中的名称/编号提供稳定间距、可换行和 `overflow-wrap`。
- 避免复用 `.billing-list-mobile span` 的宽泛选择器导致嵌套身份文本被强制省略；新选择器必须限定在账单记录结构内。
- 不改变订阅卡片的已冻结样式。

**Step 3: 保持尺寸和可访问性**

- Mobile `390x844` 下不得出现横向溢出；长 Workspace 名称、编号和日期可换行。
- Desktop `1280x900` 继续保持表格布局。
- 工作空间编号是辅助文本，不用颜色单独表达状态，不降低已有按钮和整卡的键盘焦点可见性。

### Task 4: 运行 focused verification 并保留切片证据

**Files:**
- Verify: `apps/console-ui/src/pages/CustomerPages.tsx`
- Verify: `apps/console-ui/src/styles.css`
- Verify: `tests/ui/billing-controller-browser.test.ts`
- Verify: `tools/console-browser-qa.ts`

**Step 1: 运行 focused browser tests**

Run: `npm run test:browser:billing`

Expected: PASS；原有列表/详情 freshness、opaque cursor、路由离开、Session reset 和错误隔离覆盖保持通过。

**Step 2: 运行 fake-only 双视口 Console QA**

Run: `node tools/console-browser-qa.ts --network=fake-only`

Expected: PASS；Desktop/Mobile 都能看到工作空间编号、客户收据字段，技术详情不存在，页面无横向溢出、外部请求或 Console 业务错误。

**Step 3: 运行 TypeScript 和本地验证**

Run: `npm run typecheck`

Expected: PASS，无未使用的 `ChevronDown`、Receipt 技术变量或样式引用。

Run: `npm run verify:local`

Expected: PASS；若验证包含与本切片无关的既有问题，记录原始失败命令和证据，不修改无关模块。

**Step 4: 检查最终差异边界**

Run: `git diff --check` and `git status --short`

Expected: 只包含 Console 账单 Presentation、账单样式和 focused browser assertions；无 API、DTO、Ledger、Gateway、Sub2API、数据库或部署文件变化。

**Step 5: 本地提交 E-C 实现**

```bash
git add apps/console-ui/src/pages/CustomerPages.tsx apps/console-ui/src/styles.css tests/ui/billing-controller-browser.test.ts tools/console-browser-qa.ts
git commit -m "feat(console): simplify billing receipt customer view"
```

实现提交后，下一阶段 UX-03G-E-D 再进行双视口视觉审阅、截图留证和账单记录切片冻结；本计划不包含 Push、PR 或主干合并。
