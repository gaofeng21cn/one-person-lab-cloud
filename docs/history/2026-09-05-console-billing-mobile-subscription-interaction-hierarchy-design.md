# Console 费用移动订阅字段层级设计 UX-03G-B

Date: 2026-09-05

State: approved design record

## 目标与边界

UX-03G-B 将 UX-03G-A 的移动端订阅信息缺口转换成可实现的字段层级。目标是让
用户在费用页直接完成一次订阅核对：确认 Workspace、套餐、月度总价、计费周期、
续费状态和自动续费状态。

```text
识别 Workspace → 确认套餐和金额 → 确认权益周期 → 判断续费
```

本设计只改变 `apps/console-ui` 的移动呈现顺序和信息分组，不改变费用计算、
Workspace 生命周期、续费状态机、Control Plane API/DTO、Ledger、Gateway、
Sub2API、数据库、权限或部署。卡片继续只读，整卡继续进入既有 Workspace 详情。

## Reconcile

- 目标：定义移动订阅卡片的默认字段、辅助字段、技术字段和异常状态。
- 主模块 owner：`apps/console-ui` 的费用 Presentation。
- 产品契约 owner：`docs/history/console-display-contract-v1-2026-07-31.md` 的
  `C-BIL-01`。
- 当前实现 owner：`BillingPage`、Customer Workspace Read Controller、现有客户
  Presentation model 和 `SourceState`。
- 真实调用方：`CustomerPages` 的费用页，通过 `/api/workspaces` 读取 Control
  Plane Workspace 条款投影。
- 精确写集（后续实现）：费用移动卡片组件/样式和针对该字段层级的 focused browser
  assertions；不修改 API、DTO、Controller 状态机和后端服务。
- 本设计交付：字段映射、交互层级、状态规则、双视口验收条件和 DDD 边界。

## 输入证据

- UX-03G-A：`docs/history/2026-09-05-console-billing-page-ui-audit.md`
- 运行时：fake-only Console demo，`http://127.0.0.1:5198/console/billing`
- 视口：桌面 `1280x900`，移动 `390x844`
- 当前移动卡片只显示名称、套餐、月度价格和 `paidThrough`；
  `periodStart`、`renewalStatus`、`autoRenew`、Workspace ID 未进入默认卡片。
- `C-BIL-01` 要求 Workspace 名称和 ID、套餐、月度总价、完整计费周期、续费状态
  和自动续费状态。

## 方案决策

### 选定：完整决策信息默认可见，技术证据继续后置

移动端每张订阅卡片默认展示所有完成订阅核对所需的业务事实；`Workspace ID` 按已
确认的决策放在名称下方作为辅助信息。Receipt ID、价格版本、source、原始枚举和
其他诊断字段仍不进入订阅卡片。

选择逻辑是：

```text
业务结果
→ 完成结果所需事实
→ 事实按识别/金额/权益/续费分组
→ 技术证据继续单独披露
```

方案 B（隐藏周期和续费）会把核心判断推迟到点击后；方案 C（把细节全部放到
Workspace 详情）会使费用页退化为金额列表。方案 A 是满足 `C-BIL-01` 的最短客户
路径，不是增加字段数量本身。

## 移动卡片层级

卡片保持单一可点击区域，推荐的语义顺序如下：

```text
┌────────────────────────────────────┐
│ Pilot Workspace                  →  │  L1 对象识别
│ Workspace ID: ws-1                  │  L2 辅助身份
│ BASIC                    $52.58/月  │  L1 套餐与金额
│ 计费周期                             │  L2 字段标签
│ 2026/07/01 至 2026/08/01            │  L2 权益周期
│ 续费状态：手动续费                  │  L2 续费判断
│ 自动续费：关闭                      │  L2 续费判断
└────────────────────────────────────┘
```

### L1：进入前必须看见

- Workspace 名称：主识别文本；缺失时使用现有 `未命名工作空间`。
- 套餐：`packageId` 的客户展示值。
- 月度总价：`totalUsdMicros` 由页面格式化为美元；不在浏览器重新计算。

### L2：完成订阅判断所需

- Workspace ID：名称下方辅助文本 `Workspace ID: <id>`；不使用截断值，不从 URL
  或名称猜测。
- 计费周期：`periodStart` 至 `paidThrough`，保持完整日期范围。
- 续费状态：使用现有 `presentWorkspaceRenewal` 的中文结果，例如“手动续费”“有效”
  或“已到期，未续费”。
- 自动续费：`autoRenew=true` 显示“开启”，`false` 显示“关闭”。

### L3：行为入口

- 整张卡片继续链接到 `/console/workspaces/{id}`。
- 卡片不增加修改续费、取消订阅、刷新或删除动作。
- 箭头保留为方向提示；可访问名称必须包含 Workspace 和进入详情语义。

## 字段展示契约

| 字段 | 默认移动层级 | 缺失/未知规则 | 不能做的事 |
| --- | --- | --- | --- |
| `name` | L1 主文本 | `未命名工作空间` | 不用 ID 替代客户名称 |
| `id` | L2 名称下辅助文本 | `暂不可用`；无有效 ID 时不构造详情链接 | 不从 URL 或名称反推 |
| `packageId` | L1 套餐 | `暂不可用` | 不从价格或 Workspace 状态反推套餐 |
| `totalUsdMicros` | L1 金额 | `暂不可用` | 不在浏览器相加计算 |
| `periodStart` + `paidThrough` | L2 完整周期 | 任一缺失则周期整体 `暂不可用` | 不只显示截止日期冒充完整周期 |
| `renewalStatus` | L2 续费状态 | 未知值显示 `待确认` | 不把未知当作有效或手动 |
| `autoRenew` | L2 自动续费 | 缺失显示 `暂不可用` | 不根据 `renewalStatus` 推断 |

`Workspace ID` 是可复核的对象身份，但不是新的客户状态；它只用于确认对象和进入
详情。技术详情仍可在 Workspace 页面按既有规则披露 Runtime、operation、raw status
等字段。

## 异常与状态行为

### 数据源状态

- 正常：渲染完整卡片；字段按上表处理。
- Workspace 列表为空：使用既有 `暂无订阅` 与空结果说明，不生成空卡片。
- Workspace 投影 `unavailable` 或请求失败：费用区域显示
  `订阅与续费暂不可用`、现有说明和 `重试`；不显示旧的或推测的卡片。
- 加载中：沿用 `SourceState` 的读取状态；不以空值填充卡片。

### 部分字段状态

字段缺失不能阻断其他已知字段，但必须使该字段明确不可用；不得用 `-`、零金额、
上一次响应或另一个来源的值掩盖缺失。未知的 `renewalStatus` 使用 `待确认`，保留
原始值到既有技术详情边界，不将其渲染为健康状态。

## 响应式与可访问性规则

- 移动断点继续使用纵向卡片，不能引入横向滚动表格。
- 名称、ID、日期和状态按固定字段行排列；长名称或日期允许换行，不覆盖相邻内容。
- 卡片宽度受容器约束，`390x844` 下 `documentElement.scrollWidth` 必须等于
  `clientWidth`。
- 卡片作为一个链接保留可见焦点；可访问名称应包含名称、套餐、金额和进入详情
  语义，不能只依赖箭头。
- 触控目标至少 `44x44px`；不新增仅靠颜色表达的状态。
- 桌面 `1280x900` 继续使用现有表格，不因移动字段补齐而改变桌面业务顺序或
  API 请求。
- 技术字段继续使用显式 `技术详情` disclosure，不把中英混合机器字段移到默认
  客户层。

## DDD 与解耦边界

本设计仍位于 Cloud Console Presentation bounded context：

- `BillingPage` 负责页面结构和移动卡片的客户呈现；
- Customer Workspace Read Controller 负责 `/api/workspaces` 的读取、分页、
  Session/route/request freshness；
- Control Plane 负责 Workspace 条款投影；
- Ledger 继续负责 Receipt；
- Sub2API 不参与订阅卡片读取；
- 卡片不拥有续费状态机、费用计算、持久化或任何写操作。

后续实现只允许在费用 Presentation、必要的 Console style 和 focused browser test
范围内完成。不要为了移动卡片新建 Billing API、复制 Workspace DTO、引入通用 i18n
框架或拆出第二个费用 Controller。

## UX-03G-C 验收条件

1. 在 `390x844` 下，每张可见订阅卡片默认包含名称、Workspace ID、套餐、月度总价、
   完整计费周期、续费状态和自动续费状态。
2. 第一张卡片无需进入 Workspace 详情即可完成订阅核对。
3. 卡片整卡进入详情，键盘焦点和屏幕阅读器名称明确。
4. 任何长名称、日期或未知状态不会造成文字重叠或页面横向溢出。
5. 空、不可用、失败和单字段缺失状态遵循本设计，不引入猜测性默认值。
6. 桌面表格、数据源、请求顺序、费用金额和续费业务行为保持不变。
7. focused browser test 覆盖移动字段可见性、字段顺序、整卡可达性和无横向溢出；
   不增加真实费用、续费或后端写操作。

## 本阶段交付与后续

本阶段交付：移动订阅字段层级设计、字段契约、异常规则、响应式/可访问性验收和
DDD 边界。它是 UX-03G-C 页面实现的唯一输入，不代表页面代码已修改或视觉切片已
冻结。

UX-03G-C 的最短实现顺序是：

```text
复用现有 Workspace DTO
→ 调整移动卡片 JSX 分组
→ 调整移动样式与稳定布局
→ 更新 focused browser assertions
→ 双视口验证
```

不在 UX-03G-C 中新增订阅管理、账单筛选、价格计算、Receipt 合并或 Sub2API 适配。
