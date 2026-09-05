# Console Billing 账单记录与收据详情 UX-03G-E-A 现状审计

Date: 2026-09-05

State: completed local audit record

## 审计目标与边界

本审计检查已冻结的“订阅与续费”之后，费用页第二条客户业务链：账单记录列表和
收据详情。目标是确认用户能否核对历史交易、关联 Workspace、确认金额与状态，并在
需要时查看收据证据。

本阶段只读取当前 Console Presentation、Controller、typed DTO、Ledger read contract
和 fake-only 运行时；不修改源码、API、DTO、费用计算、Receipt 状态映射、数据库、
Gateway/Sub2API、部署或生产环境。

## Reconcile

- 目标：为 UX-03G-E-B 交互层级设计提供当前事实、问题优先级和最小写集。
- 主模块 owner：`apps/console-ui` 的费用 Presentation。
- 当前页面 owner：`BillingPage`、`ReceiptDetail` 和 `useBillingController`。
- Workspace 关联读取 owner：`customerWorkspaceRead`，只提供已有 Workspace 投影。
- Receipt 权威 owner：`services/ledger`；Console 不直接访问 Ledger。
- 当前实现 HEAD：`7c0ebeebb78693fc370c92b25c2b9fbf185e9bbd`。
- 精确写集：本审计记录和 `docs/history/README.md` 索引；没有产品代码变更。

## 客户业务结果

账单记录服务的结果不同于已冻结的订阅与续费：

```text
查看历史交易
→ 识别交易类型和对应 Workspace
→ 核对发生时间、金额、状态
→ 必要时打开收据详情和技术证据
```

因此它是“历史交易核对”能力，不是新的费用计算、Wallet、Gateway Usage 或续费
状态机。

## 运行时证据

使用仓库自有 fake-only、in-memory Console demo，以 customer fixture 登录后访问
`/console/billing`，在分段控件进入“账单记录”，再打开第一条收据。

| 视口 | 列表区域 | 详情区域 | 页面宽度 |
| --- | --- | --- | --- |
| Desktop `1280x900` | `x=273, y=207, width=974, height≈114` | `x=272, y=340, width=976, height=405` | `scrollWidth/clientWidth = 1280/1280` |
| Mobile `390x844` | `x=17, y=181, width=356, height=78` | `x=16, y=278, width=358, height=408` | `scrollWidth/clientWidth = 390/390` |

截图保存在仓库外，不作为产品文件提交：

| 文件 | SHA-256 |
| --- | --- |
| `/tmp/ux03g-e-audit-2026-09-05/billing-records-desktop.png` | `9db97340cf298dae147ebabe6538c4f4ba7d5068306bdaa4bfcf5735bfd3fd96` |
| `/tmp/ux03g-e-audit-2026-09-05/billing-records-mobile.png` | `3c48429cf77b72242a81b49561e9aaa66c41be527f26cf7e2604b0a44c051413` |
| `/tmp/ux03g-e-audit-2026-09-05/receipt-detail-desktop.png` | `bac9c97496e91a6b28e4ce04b745b1516415458eda4465cde5a88b10c487a54c` |
| `/tmp/ux03g-e-audit-2026-09-05/receipt-detail-mobile.png` | `62df563104cd67bc3ee271f432a73fd0a4c20fa6b1e068e27d3a890535559c04` |

当前正常 fixture 的客户可见事实为：

```text
工作空间开通 · Pilot Workspace · 2026/07/19 20:00 · $52.58 · 待确认
收据详情：类型、状态、日期、金额、计费周期、工作空间
```

技术详情默认收起；展开后才显示 Receipt ID、Workspace ID、原始 `type/status`、
`priceVersion`、`chargeReference`、计算/存储组成和退款依据。

## 当前页面与请求顺序

```text
费用
→ 账单记录
→ 列表（按时间顺序分页）
→ 选择一条收据
→ 收据详情
→ 技术详情（默认收起）
```

进入费用路由时，当前 read path 为：

```text
GET /api/workspaces?page=1&pageSize=10   Control Plane Workspace 投影
GET /api/billing/receipts?limit=20       Ledger 收据列表
```

打开收据后追加：

```text
GET /api/billing/receipts/{receiptId}     Ledger 收据详情
```

列表使用 opaque cursor；下一页/上一页不会把 Cursor 解释为业务字段。列表和详情
分别拥有 generation、Session、route、Receipt 身份保护，详情关闭、路由离开、Session
重置或切换另一条收据后，迟到响应不能覆盖当前页面。

## 字段与状态现状

### 账单记录列表

| 事实 | Desktop | Mobile | 当前来源 |
| --- | --- | --- | --- |
| 交易类型 | 默认显示 | 默认显示 | `BillingReceipt.type` 的客户化映射 |
| Workspace | 名称默认显示 | 名称默认显示 | Workspace 投影按 `workspaceId` 关联 |
| 发生时间 | 默认显示 | 默认显示 | `createdAt` |
| 金额 | 默认显示 | 默认显示 | `chargeUsdMicros` / `totalUsdMicros` / `refundUsdMicros` |
| 状态 | 默认显示 | 默认显示 | `presentBillingStatus(status)` |
| Receipt ID | 技术详情 | 不在列表显示 | Ledger `receiptId` |
| 操作 | `查看` 按钮 | 整卡按钮 | `openReceipt(receiptId)` |

### 收据详情

默认显示类型、状态、日期、金额、计费周期和 Workspace；技术详情默认收起。详情
没有重新读取或合成 Workspace/费用事实，仍使用已有 Receipt DTO 和 Workspace 名称
投影。

### 状态与异常

| 场景 | 当前客户呈现 | 审计结论 |
| --- | --- | --- |
| 收据列表为空 | `暂无账单记录` | 空结果与错误区分正确 |
| 收据列表不可用/HTTP 503 | `账单记录暂不可用`，可重试 | Ledger 失败不会伪造交易 |
| 收据详情不可用/HTTP 503 | 详情区域独立显示 `收据详情暂不可用` | 不清空已成功的列表 |
| Workspace 投影不可用、Receipt 正常 | 列表 Workspace 显示 `暂不可用`，Receipt 仍可打开 | 读取 owner 解藕正确，但交易身份可读性下降 |
| Receipt 原始状态未知 | `待确认` | 不把未知值冒充已完成 |

fake receipt 使用 `status=succeeded`，而客户 Presentation 目前只把
`completed` 映射为 `已完成`，因此演示显示 `待确认`。这是 fixture/上游状态事实，
不是本切片可以修改的 UI fallback。

## 发现与优先级

### P1：移动列表缺少稳定的 Workspace 身份后备

移动卡片只显示 Workspace 名称，不显示 `Workspace ID`。当名称重复，或 Workspace
投影暂时不可用时，用户无法仅凭列表确认这笔历史交易属于哪个对象；当前仍能看到
交易类型、时间、金额和状态，但“对应哪个 Workspace”这个核心核对结果不稳定。

这不是新增 API 的问题：Receipt 已有 `workspaceId`，Workspace DTO 也已有 `id`。E-B
需要决定 ID 是默认辅助身份、仅在名称不可用时显示，还是继续留在详情层。

### P2：收据证据层级需要明确“核对”和“技术追溯”的边界

详情默认字段足够完成一般客户核对，Receipt ID、价格版本、扣款引用和组成金额默认
收起有利于阅读；但当用户需要提交支付争议或定位一笔重复交易时，必须先展开技术
详情。E-B 应明确是否将一个稳定的“收据编号/交易编号”前置，同时继续后置原始枚举、
版本和组成证据。

### P2：Workspace 读取失败时列表可用，但缺少解释性身份

当前列表与 Workspace 投影独立结算，避免 Ledger 故障扩大，这是正确的 owner 边界；
但 `暂不可用` 没有告诉用户这是关联 Workspace 读取失败，而不是交易数据缺失。E-B
应在不阻塞列表的前提下决定客户文案和身份显示，不把两个远程状态强行合并成一个
错误状态。

### P3：技术详情仍保留中英混合字段名

`Receipt ID`、`Workspace ID`、`priceVersion`、`chargeReference`、`compute evidence`
和 `storage evidence` 只在按需披露层出现，不影响默认客户路径；如果该层面向客户
支持或财务复核，E-B 可把客户可读标签与原始字段并列，而不修改底层字段或合同。

### P3：当前只有时间顺序分页，没有筛选入口

Ledger contract 支持 opaque cursor，Console 当前只消费固定 `limit=20` 和 cursor。
类型、日期和 Workspace 筛选不是已确认的客户业务结果；除非后续业务明确需要，不在
E-C 中新增查询参数、API 或筛选状态。

## E-B 设计入口建议

本审计结论是：账单记录与收据详情仍是已暴露客户任务，且已有真实 Ledger read path，
不建议直接移除；下一步进入交互层级设计，但限定为 Presentation 取舍：

```text
账单列表：交易类型 + 稳定 Workspace 身份 + 时间/金额/状态
→ 收据详情：客户核对字段
→ 技术详情：低频追溯证据
```

E-B 必须先确定 Workspace ID 和收据编号的默认层级，再决定 E-C 的最小 JSX/CSS/test
写集。不得因为本审计新增 Receipt API、费用计算、Ledger mutation、退款操作或
续费业务。

## 验证结果

```text
npm run test:browser:billing  # 9/9
```

9 条费用 Controller browser tests 全部通过，覆盖列表/详情迟到响应、关闭失效、
opaque cursor 前后翻页、路由离开、Session 重置以及列表/详情失败隔离。双视口 fake-only
运行时无外部请求、无页面级横向溢出；浏览器控制台仅有 Vite HMR WebSocket
handshake/close 提示。

本记录不代表生产数据资格、Candidate、Instance、部署或正式发布。
