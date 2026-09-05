# Console Billing 移动订阅卡片 UX-03G-C 实现验证

Date: 2026-09-05

State: locally verified

## 目标与边界

UX-03G-C 将已批准的 UX-03G-B 移动订阅字段层级落到客户 Console 费用页：卡片默认
呈现 Workspace 身份、套餐、月度总价、完整计费周期、续费状态和自动续费状态，用户
无需进入详情即可完成订阅核对。

本阶段只修改 `apps/console-ui` 的费用 Presentation、移动 CSS 和客户体验浏览器断言。
桌面费用表格、Control Plane API、Workspace DTO、费用计算、续费状态机、Ledger、
数据库、Gateway/Sub2API、部署和生产环境均未修改。

本记录证明本地源码和 fake-only 浏览器行为，不代表 PR、主干合并、Push、Candidate、
Instance qualification、部署或生产可用性。

## 源码身份

- Branch: `codex/workspace-task-experience`
- Verified parent: `2f113ab5` (`docs(console): plan mobile subscription implementation`)
- UX-03G-A audit: `618e24ae`
- UX-03G-B interaction hierarchy: `78154223`
- UX-03G-C implementation: `5c5b05a4` (`feat(console): complete mobile subscription hierarchy`)
- 本验证记录与历史索引在实现提交之后单独提交，便于引用可复现的实现 SHA。

## 已交付结果

- 移动订阅卡片按四个 Presentation 区域排列：Workspace 身份、套餐与月度总价、完整
  计费周期、续费判断。
- Workspace 名称下方显示 `Workspace ID`；有效 ID 保留整卡进入
  `/console/workspaces/<id>`，缺失 ID 时显示 `暂不可用` 且不生成详情 URL。
- 套餐和金额继续复用 `packageId`、`formatUsdMicros` 和既有 DTO；页面不计算费用。
- 计费周期同时要求 `periodStart` 与 `paidThrough`，缺任一字段时整体显示
  `暂不可用`，不再只显示截止日期。
- 续费状态继续复用 `presentWorkspaceRenewal`；自动续费只接受布尔值，其他值显示
  `暂不可用`。
- 移动卡片允许名称、身份和日期换行；金额保持右侧强调；整卡最小触控高度为 `44px`，
  focus ring 可见，页面不产生横向溢出。
- 桌面端继续使用原有表格列和顺序；账单记录、收据详情、Gateway Usage 和 Workspace
  页面没有随本切片改动。

## DDD 与服务边界

本次实现仍限定在 Cloud Console Presentation bounded context：

- `BillingPage` 负责移动/桌面呈现、客户文案和技术字段的默认可见性；
- `customerWorkspaceRead` 继续负责 Workspace 列表读取、分页和 freshness；
- `workspace-experience-model.ts` 继续负责续费状态的客户化映射；
- Console 只消费现有 Control Plane typed API/DTO，不拥有费用计算、续费决策、持久化
  或 Gateway/Sub2API 状态。

因此 UX-03G-C 是显示层级收敛，不是业务重构，也没有创建第二个 Billing、Wallet 或
Gateway owner。

## 双视口证据

截图由仓库自带 fake-only demo 生成，保存在仓库外，不进入产品提交。

| 文件 | Dimensions | SHA-256 |
| --- | --- | --- |
| `/tmp/ux03g-c-2026-09-05/billing-subscription-desktop.png` | `1280x900` | `0967be294f25e8476d3cf2168a8b41e3ca521314fed7d13586686b5e15450aed` |
| `/tmp/ux03g-c-2026-09-05/billing-subscription-mobile.png` | `390x844` | `22201ef84b9de4a3bf9b2bd60fa4c92c2fa20e42778678773e735a472caf6fee` |

运行时确认：

- 桌面端继续显示工作空间、套餐、月度总价、计费周期、续费状态和自动续费六列；
- 移动端第一张卡片文本为 `Pilot Workspace`、`Workspace ID: ws-1`、`BASIC`、
  `$52.58`、`2026/07/01 至 2026/08/01`、`手动续费`、`自动续费`、`关闭`，并指向
  `/console/workspaces/ws-1`；
- 移动第一张卡片布局框为 `x=17, y=181, width=356, height≈218`；
- Desktop 和 Mobile 均满足 `documentElement.scrollWidth === clientWidth`（分别为
  `1280/1280`、`390/390`），没有横向溢出；
- 没有对外部网络发起请求；应用代码没有 page error。浏览器控制台仅记录 fake demo
  的 Vite HMR WebSocket handshake/close 提示，不属于应用业务错误；
- Receipt ID、原始 receipt status 和价格版本仍保持在收据“技术详情”按需披露层，未把
  技术字段混入移动订阅核对卡片；fake receipt 使用 `succeeded`，而 Control Plane
  Receipt 投影要求 `completed`，因此演示收据继续显示 `待确认`，本阶段没有修改上游
  状态映射或 fixture。

## 验证结果

以下命令均通过：

```text
npm run typecheck
npm run lint
git diff --check
npm run test:browser:customer-experience  # 2/2
npm run test:browser:billing              # 9/9
npm run verify:local
```

`verify:local` 通过 product boundary、194 条 Node source tests、全部 Console browser
suites、TypeScript、Vite production build、所有 Go module compile/database-free tests
和 Git whitespace gate。

## 交付状态

UX-03G-A（费用页面现状审计）、UX-03G-B（移动订阅字段层级）和 UX-03G-C（实现与双视口
验证）已完成本地闭环；Gateway Key、Workspace、Gateway Usage 的既有切片不受影响。

本阶段未 Push、未提 PR、未合并主干、未部署。后续费用页面切片应从新的审计和 owner
边界开始，不在本卡片中继续堆叠收据、计费计算或续费业务能力。
