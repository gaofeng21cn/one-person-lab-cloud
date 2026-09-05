# Console 账单记录与收据详情交互层级设计

Date: 2026-09-05

State: approved design record

## 目标

账单记录页面服务的客户业务结果是：

```text
查看历史交易
→ 识别交易类型和对应 Workspace
→ 核对发生时间、金额、状态和计费周期
```

普通客户只需要完成这条“历史交易核对”路径。收据业务详情属于客户路径；Ledger
原始字段、价格版本、扣款引用和资源组成属于技术证据，不属于当前普通客户 UI 的
默认交互，也不因为 DTO 中存在就自动呈现。

## 边界与 Reconcile

- 主模块 owner：`apps/console-ui` 的费用 Presentation。
- 页面 owner：`BillingPage`、`ReceiptDetail` 和现有 `useBillingController`。
- Receipt 权威 owner：`services/ledger`；Console 只消费已有的 typed Receipt API。
- Workspace 名称 owner：现有 Customer Workspace Read Controller；Receipt 的
  `workspaceId` 仍是稳定身份后备。
- 精确实现写集：账单列表和收据详情的 Presentation JSX、必要的 Console 样式和
  focused browser assertions。
- 不修改：Receipt API/DTO、Ledger、Control Plane、费用计算、续费状态机、数据库、
  Gateway、Sub2API、权限、部署和生产环境。

## 方案决策

采用“客户核对信息前置，技术证据不进入当前客户 UI”的方案。

### 不采用的方案

1. **技术详情折叠保留在客户页面**：虽然实现成本低，但会继续暴露
   `priceVersion`、`chargeReference`、`compute evidence` 等低频机器字段；当前没有
   Support 工单或客户技术排查业务，不能证明这段 UI 是必要的。
2. **把 Receipt ID、价格版本等全部前置**：会让普通账单列表变成 Ledger 诊断页，增加
   中英混合和专业术语，破坏移动端扫描路径。
3. **新增客户支持或退款操作**：超出账单历史核对结果，也没有当前 API、权限和 owner
   支撑。

## 交互层级

### 账单记录列表

列表保留现有的只读记录和 opaque cursor 分页。桌面继续使用表格，移动端继续使用
整卡打开收据详情。

默认阅读顺序为：

```text
交易类型
→ Workspace 名称
→ Workspace ID（辅助身份）
→ 发生时间
→ 金额
→ 状态
→ 查看收据详情
```

桌面不新增技术列；Workspace 单元格在名称下增加已有 `workspaceId`，不引入新的
API 或查询参数。移动卡片将 Workspace 名称和 ID 作为同一对象识别区，金额和状态
保持明显的结果层级，整卡仍然只触发既有 `openReceipt(receiptId)`。

### 收据业务详情

点击列表中的“查看”或移动卡片后，在当前费用页面显示收据详情区域。默认只显示客户
完成核对所需的事实：

| 客户字段 | 来源 | 展示规则 |
| --- | --- | --- |
| 类型 | `BillingReceipt.type` | 使用现有客户化中文映射 |
| 状态 | `BillingReceipt.status` | 使用现有 `presentBillingStatus`，未知仍为“待确认” |
| 日期 | `createdAt` | 使用现有日期格式化 |
| 金额 | `chargeUsdMicros` / `totalUsdMicros` / `refundUsdMicros` | 使用现有金额格式化，不在浏览器重新计算 |
| Workspace | `workspaceId` + Workspace 投影 | 名称优先，ID 作为辅助身份 |
| 计费周期 | `periodStart` + `paidThrough` | 两端齐全才显示完整周期 |

当前普通客户 UI 不显示以下技术字段：

```text
Receipt ID
resourceId
priceVersion
chargeReference
components.compute
components.storage
fulfillment
refund evidence 的原始字段
```

这些字段继续由 Ledger 和已有 typed DTO 保留，供未来明确授权的财务、支持或运营
读路径使用。当前没有为它们新建客户页面、导出、复制、退款或下载操作。

## 数据与状态规则

- Receipt 列表和 Receipt 详情继续使用现有独立 read state；详情加载失败不能清空已
  成功的列表。
- Workspace 投影失败不能阻塞 Receipt；名称不可用时仍显示已有 `workspaceId`，并将
  名称标为“工作空间名称暂不可用”，不把两个远程状态合成一个账单错误。
- Receipt 列表为空与读取失败继续区分为“暂无账单记录”和“账单记录暂不可用”。
- Receipt 详情为空、加载中、失败继续由现有 `SourceState` 呈现；不使用旧详情填充
  新选择的收据。
- 任意字段缺失都显示明确的“暂不可用”或现有空值规则，不以零金额、旧响应或另一
  个来源的字段推断。
- Receipt 原始状态未知仍显示“待确认”；本切片不修改 `succeeded` 与 `completed`
  的映射关系。

## 响应式与可访问性

- `1280x900` 桌面保持表格布局，不为技术字段增加列。
- `390x844` 移动端保持纵向整卡，不引入横向滚动。
- Workspace 名称、ID、日期和状态允许换行，不能覆盖相邻内容。
- 查看入口和关闭详情入口必须有明确的可访问名称；整卡保持现有键盘可达性。
- 不以颜色单独表达状态；不新增高风险写操作。
- 客户页面不得出现没有业务用途的英文机器字段或中英混合技术标签。

## DDD 与解耦

```text
Console Presentation
├─ BillingPage：账单列表和收据业务详情的结构与客户文案
├─ useBillingController：列表/详情读取、Session、route、generation 和 cursor freshness
└─ customerWorkspaceRead：Workspace 名称投影和独立失败状态

Ledger：Receipt 权威数据和证据
Control Plane：Workspace 客户投影
Gateway/Sub2API：不参与账单页面改造
```

Console 不复制 Receipt、Wallet 或 Billing owner，也不为了隐藏技术字段新建 DTO、
服务或全局 i18n 层。未来如果支持/财务确实需要技术证据，应在对应的授权 bounded
context 中设计，不把内部字段重新塞回普通客户路径。

## UX-03G-E-C 最小实现范围

1. 在账单记录列表中把已有 `workspaceId` 作为 Workspace 的辅助身份呈现，尤其覆盖
   移动卡片和 Workspace 名称不可用场景。
2. 保留收据详情的客户核对字段，移除普通客户页面的技术详情 disclosure 及其机器
   字段展示。
3. 保持现有 Receipt 请求顺序、列表/详情错误隔离、opaque cursor 和状态映射。
4. 更新 focused browser assertions，覆盖桌面、移动端、Workspace 投影失败和技术字段
   不出现在客户 DOM 的边界。
5. 完成双视口检查后冻结账单记录与收据详情切片。

## 验收结果

E-C 完成后，客户无需理解 Ledger 字段即可完成：

```text
找到一笔历史交易
→ 确认属于哪个 Workspace
→ 核对时间、金额、状态和计费周期
→ 按需打开业务收据详情
```

验收不包括生产部署、Instance 资格、Ledger 数据迁移、退款能力或新的客户支持
流程。
