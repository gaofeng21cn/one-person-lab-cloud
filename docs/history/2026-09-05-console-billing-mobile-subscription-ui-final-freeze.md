# Console Billing 移动订阅卡片 UX-03G-D 最终冻结

Date: 2026-09-05

State: locally verified and frozen

## 冻结目的

UX-03G-D 对 UX-03G-C 已实现并验证的“订阅与续费”客户体验做最终审阅。冻结对象是
页面的业务结果、信息层级、交互边界、双视口表现和数据 owner，不是永久禁止修改代码。

冻结后，任何新增收据、退款、价格版本、扣款引用、Gateway 用量或续费业务需求，都必须
作为新的 UX 切片重新审计和定义，不能直接堆进当前订阅卡片。

## 审阅输入与源码身份

- 主模块 owner：`apps/console-ui`
- UX-03G-A 费用页面现状审计：`618e24ae`
- UX-03G-B 移动订阅字段层级：`78154223`
- UX-03G-C 实现：`5c5b05a4`
- UX-03G-C 验证记录：`c7ff8053`
- Branch: `codex/workspace-task-experience`

审阅只使用已提交的实现、focused browser tests、`verify:local` 和 fake-only 双视口
证据，没有引入新的 API、DTO、业务状态或生产读取。

## 冻结后的页面契约

移动订阅卡片默认按以下顺序服务一次订阅核对：

```text
Workspace 身份
→ 套餐与月度总价
→ 完整计费周期
→ 续费状态与自动续费状态
```

冻结内容：

- Workspace 名称下方显示 `Workspace ID`；有效 ID 的整卡入口为
  `/console/workspaces/<id>`，无 ID 时显示 `暂不可用` 且不生成详情 URL；
- 套餐和金额直接来自既有 Workspace DTO，金额继续使用 `formatUsdMicros`；
- `periodStart` 和 `paidThrough` 必须同时存在，缺任一项时周期整体显示 `暂不可用`；
- 续费状态继续使用 `presentWorkspaceRenewal`，自动续费只接受布尔值；
- 移动端允许长名称和日期换行，金额右侧强调，整卡触控高度至少 `44px`；
- 桌面端继续使用原有六列表格：工作空间、套餐、月度总价、计费周期、续费状态、
  自动续费；
- Receipt ID、原始枚举、价格版本和扣款引用继续留在收据“技术详情”按需披露层。

## 最终审阅结论

本轮没有发现需要修复的视觉、交互或边界阻断项，UX-03G-C 可以冻结：

- 桌面表格仍保持原有业务列、顺序和金额格式；
- 移动首张卡片无需进入详情即可完成身份、套餐、金额、周期和续费判断；
- 移动卡片的周期和续费区域不会被短标签或省略号截断；
- 有效卡片整卡导航和 focus ring 可操作，缺失身份不会产生伪造导航；
- 两个视口均无横向溢出，固定底部导航不遮挡订阅内容；
- 没有新增刷新、续费、删除或其他写操作，业务链和幂等边界保持不变；
- Console 仍是 Presentation owner，Control Plane、Ledger、Gateway/Sub2API 和费用
  计算 owner 没有被复制或越界。

## 双视口证据

截图由仓库自带 fake-only demo 生成，保存在仓库外，不进入产品提交。

| 文件 | Dimensions | SHA-256 |
| --- | --- | --- |
| `/tmp/ux03g-c-2026-09-05/billing-subscription-desktop.png` | `1280x900` | `0967be294f25e8476d3cf2168a8b41e3ca521314fed7d13586686b5e15450aed` |
| `/tmp/ux03g-c-2026-09-05/billing-subscription-mobile.png` | `390x844` | `22201ef84b9de4a3bf9b2bd60fa4c92c2fa20e42778678773e735a472caf6fee` |

运行时读数：

- Desktop：`scrollWidth/clientWidth = 1280/1280`；
- Mobile：`scrollWidth/clientWidth = 390/390`；
- Mobile 第一张卡片布局框：`x=17, y=181, width=356, height≈218`；
- Mobile 第一张卡片继续指向 `/console/workspaces/ws-1`；
- 未发起外部请求；fake demo 仅有 Vite HMR WebSocket handshake/close 提示，没有应用
  业务错误。

## 业务与技术边界

冻结不改变以下事实：

- Console 通过既有 Control Plane typed API/DTO 读取 Workspace 条款；
- `customerWorkspaceRead` 继续拥有列表分页、Session/route freshness 和读取状态；
- `workspace-experience-model.ts` 继续拥有续费状态的客户化映射；
- Console 不拥有费用计算、续费状态机、Ledger 证据、数据库或 Gateway/Sub2API 状态；
- fake receipt 的 `succeeded` 与 Receipt 投影要求的 `completed` 不一致，演示仍显示
  `待确认`；该上游事实不属于本切片的 UI 修复范围。

## 验证与交付

以下验证已经在 UX-03G-C 完成并作为本次冻结依据：

```text
npm run test:browser:customer-experience  # 2/2
npm run test:browser:billing              # 9/9
npm run typecheck
npm run lint
npm run verify:local
git diff --check
```

实现工作区 `codex/workspace-task-experience` 当前干净。UX-03G-A、UX-03G-B、UX-03G-C、
UX-03G-D 已形成费用订阅的本地闭环；本冻结不代表 Push、PR、主干合并、Candidate、
Instance qualification、部署或生产可用性。
