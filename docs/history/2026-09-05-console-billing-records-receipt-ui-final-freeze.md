# Console Billing 账单记录与收据详情 UX-03G-E-D 最终冻结

Date: 2026-09-05

State: locally verified and frozen

## 冻结目的

UX-03G-E-D 对 UX-03G-E-C 已实现的“账单记录与收据详情”客户体验做最终双视口审阅。
冻结对象是历史交易核对的业务结果、字段层级、异常边界、响应式表现和 owner 责任，
不是永久禁止后续修改代码。

冻结后，新增退款、下载、支付争议、客户支持、价格版本、扣款引用或 Ledger 技术证据
需求，都必须重新审计并建立独立 UX 切片，不能直接堆进当前普通客户账单路径。

## 审阅输入与源码身份

- 主模块 owner：`apps/console-ui`
- UX-03G-E-A 现状审计：`49cc70fe`
- UX-03G-E-B 交互层级设计：`a8db953a`、`3e91204a`
- UX-03G-E-C 实现：`4069c01e`
- Branch：`codex/workspace-task-experience`

本次审阅只使用已提交的 Console 实现、typed DTO、focused browser tests、`verify:local`
和 fake-only 账单双视口运行证据。没有新增 API、DTO、Controller、Ledger、Control
Plane、Gateway、Sub2API 或生产读取。

## 冻结后的客户页面契约

### 账单记录列表

账单列表按以下路径服务客户核对：

```text
交易类型
→ 工作空间名称
→ 工作空间编号
→ 发生时间
→ 金额
→ 状态
→ 查看收据详情
```

冻结内容：

- Desktop 继续使用现有六列表格，不增加技术列；
- Mobile 继续使用整卡进入既有收据详情读取；
- Workspace 名称来自 Customer Workspace Read 投影；
- “工作空间编号”使用 Receipt 已有 `workspaceId`，不显示原始字段名；
- Workspace 名称读取失败时仍保留编号，不把 Workspace 投影失败伪装成 Receipt 失败；
- 列表继续使用既有时间顺序和 opaque cursor 分页；
- 不增加筛选、排序、退款、下载、重试扣款或其他写操作。

### 收据业务详情

点击“查看”后，详情区域默认只显示普通客户完成交易核对所需的字段：

```text
类型
状态
日期
金额
计费周期
工作空间名称
工作空间编号
```

普通客户 DOM 不再显示以下 Ledger 技术字段或 disclosure：

```text
Receipt ID
resourceId
priceVersion
chargeReference
components.compute
components.storage
fulfillment
原始 type/status
```

这些字段仍由 Ledger 和已有 typed DTO 保留，但当前没有客户支持或财务复核业务要求，
因此不进入普通客户 UI。未来如需披露，应在明确授权的支持/财务 bounded context 中
单独设计，不复活当前技术详情折叠。

## 最终审阅结论

本轮没有发现需要修复的 P1/P2 视觉、交互或 DDD 边界问题，UX-03G-E-C 可以冻结：

- Desktop 表格的交易类型、Workspace、时间、金额、状态和查看入口保持清晰；
- Mobile 卡片先呈现交易类型和 Workspace 身份，再呈现金额、状态和日期；
- 收据详情的默认字段足以完成一次历史交易核对；
- 技术字段不再压过客户路径，也不会因中英混合机器字段增加阅读负担；
- Mobile 收据详情高度约 `339px`，固定底部导航从 `y=779px` 开始，不发生遮挡；
- Desktop 和 Mobile 均无横向溢出；
- 详情失败不会清空列表，Workspace 投影失败不会阻塞 Receipt；
- 没有新增客户写操作、费用计算、Ledger mutation 或第二个 Billing owner。

## 双视口证据

截图由仓库自有 fake-only Console demo 生成，保存在仓库外，不进入产品提交。

| 文件 | Dimensions | SHA-256 |
| --- | --- | --- |
| `/tmp/ux03g-e-d-2026-09-05/billing-desktop.png` | `1280x900` | `4b3a5871ce02d6797caf1d3f3541a2ecd3fc29acbbcb5de2862da0dbb94f018c` |
| `/tmp/ux03g-e-d-2026-09-05/billing-mobile.png` | `390x844` | `e95b7aa4e457f03567f2c636e6647645040c7b6b3d765533d77ddf73aaf281cf` |

运行时读数：

- Desktop：`scrollWidth/clientWidth = 1280/1280`；详情区域
  `x=272, y≈339, width=976, height=336`；
- Mobile：`scrollWidth/clientWidth = 390/390`；详情区域
  `x=16, y≈314, width=358, height=339`；
- Mobile 固定导航：`y=779, height=65`，详情区域底部约 `653`，没有遮挡；
- `details.receipt-technical-details` 和 `details.receipt-row-technical-details` 数量均为 `0`；
- 客户详情中原始 `pilot-usd-2026-07-v1` 数量为 `0`。

## DDD 与服务边界

本切片仍限定在 Cloud Console Presentation bounded context：

- `BillingPage` 负责账单列表、收据业务详情和客户文案；
- `useBillingController` 继续负责 Receipt 列表/详情请求、Session、route、generation、
  cursor 和 freshness；
- `customerWorkspaceRead` 继续负责 Workspace 名称投影和独立失败状态；
- Ledger 继续拥有 Receipt 权威数据和技术证据；
- Control Plane 继续拥有 Workspace 客户投影；
- Gateway/Sub2API 不参与本切片；
- Console 不拥有费用计算、续费状态机、Wallet、数据库或 Ledger 证据。

## 验证与交付

以下验证已通过：

```text
npm run test:browser:billing              # 9/9
npm run test:browser:customer-experience  # 2/2
npm run typecheck
npm run lint
npm run build
npm run verify:local
git diff --check
```

`verify:local` 通过 product boundary、194 条 Node source tests、Console browser suites、
TypeScript、Vite production build、Go modules compile/database-free tests 和 whitespace
gate。

仓库自带的全量 `node tools/console-browser-qa.ts --network=fake-only` 仍会在本切片之前
的 Workspace 访问面板“显示 API 密钥”断言处失败；该失败在 E-C 修改前后均可复现，未
触及账单路径，本次账单冻结以独立双视口验收和 `verify:local` 为准。

实现工作区当前保持干净。本冻结不代表 Push、PR、主干合并、Candidate、Instance
qualification、部署或生产可用性。
