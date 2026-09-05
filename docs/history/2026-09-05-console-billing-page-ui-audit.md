# Console 费用页面现状审计 UX-03G-A

Date: 2026-09-05

State: completed local audit record

## 审计目标与边界

本审计检查客户 Console 的费用页面当前运行时结构、业务字段、读取顺序、状态
呈现以及桌面/移动差异，为 UX-03G-B 交互层级设计提供事实基线。业务已经跑通，
本阶段不改变费用计算、Workspace 生命周期、Ledger Receipt、Gateway/Sub2API、
Control Plane API、DTO、数据库或部署。

费用页服务两个客户结果：

```text
费用
├── 订阅与续费：我现在为每个 Workspace 买了什么、多少钱、计费到什么时候、是否续费
└── 账单记录：历史发生过哪些开通/续费/到期/退款，以及某一笔收据的完整依据
```

这不是新的账单模型，也不是把 Wallet、Gateway Usage 和 Workspace Receipt 合并成
一个 owner。页面只呈现既有 Control Plane Workspace 投影和 Ledger Receipt。

## Reconcile

- 目标：冻结费用页面可以支持的客户决策事实和当前信息缺口。
- 主模块 owner：`apps/console-ui` 的费用 Presentation。
- 当前实现 owner：`BillingPage`、`useBillingController`、Customer Workspace Read
  Controller 和 `SourceState`；源码与 focused browser tests 是实现事实。
- 产品契约 owner：`docs/history/console-display-contract-v1-2026-07-31.md` 的
  `C-BIL-01`、`C-BIL-02`、`C-BIL-03`（本历史记录只保留本轮审计结果）。
- 真实调用方：`CustomerPages` 的 `BillingPage`，通过同源 Console read API 读取
  Control Plane Workspace 投影和 Ledger Receipt。
- 精确写集：本审计记录和 `docs/history/README.md` 索引；不写入产品源码、API、
  业务状态或持久化。
- 完成证据：双视口运行时截图、布局读数、状态重放、字段映射、owner 边界和
  focused browser tests。

## 运行时与证据

- 分支：`codex/workspace-task-experience`
- HEAD：`db319fb951988b85ba53b93d88d60d1598d4ee26`
- Runtime：仓库自有 fake-only、in-memory Console demo，`tools/start-console-demo.ts`
- URL：`http://127.0.0.1:5198/console/billing`
- 凭据：仅使用仓库 customer fixture，不触达外部网络、真实费用或真实资源
- 视口：桌面 `1280x900`，移动 `390x844`
- 运行时布局读数：移动端 `innerWidth=390`、`innerHeight=844`，
  `documentElement.scrollWidth=390`；桌面同样没有页面级横向溢出。
- 截图保存在仓库外，不作为产品文件提交：
  - `/tmp/ux03g-audit-2026-09-05/desktop-terms.png`
  - `/tmp/ux03g-audit-2026-09-05/desktop-receipt-detail.png`
  - `/tmp/ux03g-audit-2026-09-05/mobile-terms.png`
  - `/tmp/ux03g-audit-2026-09-05/mobile-receipt-detail.png`

截图 SHA-256：

| 文件 | 捕获尺寸 | SHA-256 |
| --- | --- | --- |
| `desktop-terms.png` | `1134x900` capture artifact | `9def49f41e84d9994f4bd07ed014f61850145611ab65de8566695b30ea7c8518` |
| `desktop-receipt-detail.png` | `1134x900` capture artifact | `616114c462d42fe4a8bb6ef4b627aa23eaeec3cc313e9023b56fa7ad54405d45` |
| `mobile-terms.png` | `390x844` | `a6da0c55e24e2e35a46dcb84cf0f3d3417abb4860808029842cedb0073e078` |
| `mobile-receipt-detail.png` | `390x844` | `17b13273cebe1e99eaa950f48a51fa9e351e718256ac26deb9c9d7fb66f71093` |

桌面截图的物理 capture 宽度为 `1134`，而浏览器运行时 viewport 为 `1280`；这是
截图工具的缩放表现，不是页面 `scrollWidth` 溢出，也不改变页面字段存在性。

## 当前页面区域映射

### 订阅与续费

```text
Console Shell
→ 页面标题：费用
→ 页内视图：订阅与续费 / 账单记录
→ 订阅与续费
→ 每个 Workspace：名称、套餐、月度总价、计费周期、续费状态、自动续费
→ 进入 Workspace 详情
```

桌面使用表格，当前 fixture 显示：

```text
Pilot Workspace   BASIC  $52.58   2026/07/01 至 2026/08/01   手动续费   关闭
Second Workspace  PRO    $240.08  2026/07/15 至 2026/08/15   手动续费   关闭
```

移动使用可点击卡片，当前首屏只显示 Workspace 名称、套餐、月度价格和
`paidThrough`。卡片没有显示 `periodStart`、续费状态、自动续费状态或
Workspace ID。

### 账单记录与收据详情

```text
账单记录
→ 时间、类型、Workspace、金额、状态
→ 查看
→ 收据详情：类型、状态、日期、金额、计费周期、Workspace
→ 技术详情（默认收起）：Receipt ID、Workspace ID、原始 type/status、priceVersion、
  chargeReference、计算/存储组成和退款依据
```

桌面使用表格，移动使用收据卡片。移动卡片把类型、Workspace、时间、金额和状态
保留在一张可点击记录中；打开后详情按纵向字段排列，计费周期在窄视口中自然换行，
没有页面级横向溢出。

## 请求顺序与状态边界

进入费用路由时，Shell 读取 Session；费用页并行读取当前 Workspace 条款和
Ledger 收据列表：

```text
GET /api/auth/me                         Session（本地运行时可能读取两次）
GET /api/workspaces?page=1&pageSize=10   Control Plane Workspace 条款
GET /api/billing/receipts?limit=20       Ledger 收据列表
```

选择收据后追加：

```text
GET /api/billing/receipts/{receiptId}     Ledger 收据详情
```

账单列表使用 opaque cursor；翻页只改变 `cursor`，每页固定 `limit=20`。Overview
使用同一 Controller 的独立 `limit=3` 读取，不应覆盖费用页的 20 条列表。列表和
详情是独立远程状态，路由、Session、请求 generation、cursor 或所选 Receipt 改变
后，迟到响应不能提交。

### 空、不可用与失败状态重放

在同一 fake-only demo 以响应拦截逐条重放，观察到：

| 投影 | 输入 | 客户呈现 | 恢复动作 |
| --- | --- | --- | --- |
| Workspace 条款 | `status=empty` | `暂无订阅` / `当前没有记录。` | 无，空结果不是错误 |
| Workspace 条款 | `status=unavailable` | `订阅与续费暂不可用` | `重试` |
| Workspace 条款 | HTTP `503` | `订阅与续费暂不可用`，并保留“暂时无法读取订阅与续费信息”说明 | `重试` |
| 收据列表 | `status=empty` | `暂无账单记录` / `当前没有记录。` | 无，空结果不是错误 |
| 收据列表 | `status=unavailable` | `账单记录暂不可用` | `重试` |
| 收据列表 | HTTP `503` | `账单记录暂不可用`，并保留“暂时无法读取账单记录”说明 | `重试` |
| 收据详情 | HTTP `503` | `收据详情暂不可用`，详情区域独立显示 | `重试` |

因此，Ledger 失败不会伪造 Workspace 条款，Workspace 失败也不会伪造收据；详情
失败不会清空已经成功的列表。

## 当前字段字典与展示层

### Workspace 条款

| 字段 | owner 事实 | 桌面 | 移动 |
| --- | --- | --- | --- |
| `id` | Workspace 权威身份 | 未显示，名称链接到详情 | 未显示，整卡链接到详情 |
| `name` | 客户识别对象 | 默认显示 | 默认显示 |
| `packageId` | 当前套餐 | 默认显示 | 默认显示 |
| `totalUsdMicros` | 月度总价，浏览器只格式化 | 默认显示美元 | 默认显示美元 |
| `periodStart` / `paidThrough` | 当前计费周期 | 默认显示 | 仅 `paidThrough` |
| `renewalStatus` | 当前续费状态 | 默认显示 | 未显示 |
| `autoRenew` | 自动续费状态 | 默认显示 | 未显示 |

### 收据

| 字段 | owner 事实 | 当前层级 |
| --- | --- | --- |
| `receiptId` | Ledger 收据身份 | 技术详情 |
| `type` | 开通、续费、到期、退款事件类型 | 客户层映射为中文；原值在技术详情 |
| `status` | Ledger 状态 | 客户层映射；原值在技术详情 |
| `workspaceId` | 收据关联 Workspace | 桌面以 Workspace 名称展示；原值在技术详情 |
| `createdAt` | 收据创建时间 | 列表和详情默认显示 |
| `totalUsdMicros` / `refundUsdMicros` | 实际收费/退款金额 | 默认金额；退款额按类型提供 |
| `periodStart` / `paidThrough` | 收据计费周期 | 详情默认显示 |
| `priceVersion` | 价格版本 | 技术详情 |
| `chargeReference` | 支持定位的扣款引用 | 技术详情 |
| `components` | 计算/存储组成和容量 | 技术详情 |

浏览器不计算价格、不从 Workspace 条款合成收据，也不直接访问 Ledger、Gateway
或 Sub2API。美元 micros 由 owner DTO 提供，页面只做格式化。

## DDD 与模块边界

本切片仍属于 Cloud Console Presentation bounded context：

- `apps/console-ui/src/pages/CustomerPages.tsx`：费用区域的页面结构、客户文案、
  响应式呈现和技术详情 disclosure。
- `apps/console-ui/src/app/use-billing-controller.ts`：费用视图、Receipt 列表/详情
  remote state、opaque cursor、所选 Receipt、路由/Session/freshness 和重试编排。
- Customer Workspace Read Controller：提供订阅与续费所需的 Workspace 投影；它不
  拥有账单计算或 Receipt。
- `services/control-plane`：Session、Workspace 条款投影和客户 read API。
- `services/ledger`：Receipt、证据、保留与对账的权威 owner。
- `Sub2API`：Gateway、Key、Usage 和 spendable wallet 的 owner；本页不直接消费它。

BillingPage 当前仍与其他 customer page 同在 `CustomerPages.tsx` 文件中，但数据
读取和 freshness 已由独立 Controller 承担。仅为文件拆分而重构不会改善本轮客户
结果，因此不列为 UX-03G-A 的开发项。

## 发现与优先级

### P1：移动订阅卡片缺少决策所需的完整周期和续费状态

移动端用户无法仅通过费用页回答“从什么时候计费”“是否手动/自动续费”“当前续费
状态是什么”。这些字段已经在 Workspace DTO 中，问题是响应式呈现映射，不是后端
缺字段。UX-03G-B 应先定义移动卡片的信息优先级和稳定高度，再决定字段是否全部
默认显示或分组显示。

### P2：订阅条款默认没有 Workspace ID

`C-BIL-01` 要求名称和 Workspace ID 一起支持对象确认。当前默认层只有名称，进入
详情需要再依赖链接目标。UX-03G-B 应决定 ID 是客户默认事实、辅助文本还是技术
详情；不能在 UI 层另造一个 ID，也不需要新增 API。

### P2：收据身份字段默认藏在技术详情

`C-BIL-03` 需要 Receipt ID、Workspace ID、价格版本等可复核事实。当前正常详情先
展示类型、状态、日期、金额、周期和 Workspace，身份与价格证据要打开“技术详情”
才能看到。这个设计有利于客户层阅读，但与“收据可复核”契约存在层级张力；下一步
应明确哪些 ID 必须前置，哪些继续按需披露，而不是直接扩大默认字段。

### P3：技术层仍保留中英混合术语

`Receipt ID`、`Workspace ID`、`priceVersion`、`chargeReference`、`compute evidence`
等只出现在显式技术详情中，服务支持定位和证据核对。它们不是当前客户主任务，
不应为了语言统一而伪造别名；如需优化，只增加中文解释或分组，不修改机器字段。

### P3：桌面首屏留白与记录计数不是当前阻塞

`1280x900` 下列表区域右侧和下方有留白，收据页也没有额外的总记录统计。当前两
个客户结果已在首屏可达，增加图表、余额卡片或装饰模块没有业务依据，暂不开发。

## 证据排除项

- Vite HMR WebSocket `failed to connect` 只属于开发服务器噪声，不属于费用请求链路。
- fake fixture 的 `billing.workspace_purchased.v1` 使用 `status=succeeded`，而
  Control Plane `projectCustomerBillingReceipt` 对客户 Receipt 投影要求
  `status=completed`。因此本地演示显示“待确认”是 fixture fidelity 问题；不能据此
  修改生产状态映射，也不能把它当作 Ledger 业务错误。
- 截图物理宽度与 viewport 宽度不同是 capture 缩放，不是页面横向溢出。
- 本审计没有访问真实账单、真实 Ledger、真实 Gateway 或外部网络，不能推导生产
  可用性、账单正确性或部署资格。

## 结论与 UX-03G-B 入口

正常业务链路、空/不可用/失败状态和分页/迟到响应边界均已被当前实现覆盖；费用页
不需要因为审计而改费用业务。下一步只定义交互层级和响应式字段映射：

```text
L0 页面身份：费用
L1 主任务：订阅与续费 / 账单记录两个稳定页内视图
L2 订阅任务：确认 Workspace、套餐、金额、计费周期、续费状态、自动续费
L2 收据任务：定位时间、事件类型、Workspace、金额、状态
L3 收据详情：复核周期、金额和关联 Workspace
L4 技术详情：按需查看 Receipt/Workspace ID、价格版本、扣款引用和组成证据
```

UX-03G-B 的最短输入是：

1. 为移动订阅卡片决定周期、续费、自动续费和 ID 的默认/辅助层级。
2. 决定收据 ID 与 Workspace ID 的客户可复核位置。
3. 保留现有 Controller、API、DTO、Ledger、Gateway 和 Workspace owner，不新增
   费用计算、图表、筛选或第二个账单状态来源。

本阶段交付的是现状事实和下一步设计入口，不代表费用页面已经重构、冻结、Push、提
PR、合并主干、部署或取得生产资格。
