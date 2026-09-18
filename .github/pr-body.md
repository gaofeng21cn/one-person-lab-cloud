### 布局对齐复核（2026-09-18，`dd796c8b`）

对全部 15 条 Console 路由做了整页截图与几何复核（1440 / 1280 / 1024 / 390 四个视口，另加 390–1600 共 18 档宽度扫描），逐项检查“内容是否跑出所在容器”。截图见仓库外 `output/console-alignment-20260918/`（desktop-1440 / mobile-390 各 18 页整页图，`output/` 已在 .gitignore 内）。

发现并修复的对齐缺陷（均已在上游 `822a010b` 复现）：

| 页面 | 缺陷 | 数值 |
| --- | --- | --- |
| 8 个含表格的面板（客户概览/API 用量/费用、运维概览/账户/资源状态×2…） | 面板内表格容器 `width:100%` 叠加左右 margin，向右溢出面板块；窄屏面板 `overflow:hidden` 时被裁掉 | +16～18px |
| 资源状态 · Workspace 资源列表 | 末列「查看资源」按钮被挤出单元格（也是本次问题的直接来源） | 6px |
| 资源状态 · Workspace 资源列表 | 表头「Key 累计费用」压到相邻列 | 6～29px |
| API 密钥 | `.data-list` 固定 260px 标签列把数值列压到 0 宽，数值溢出卡片 | 最大 155px |
| 资源状态 · 详情（移动端） | 镜像 digest 不换行，页面级横向滚动 | 528 > 390px |
| 新建工作空间 | 套餐选项最小内容宽度 718px 落在 628px 列内 | 92px |
| 工作空间详情 | 身份卡内边距 18px 与同级面板标题 14/18px 不一致 | 标题错位 4px |
| 客户与计费账户 | 状态胶囊被 132px 上限截断、身份邮箱被省略号截断 | 5.8～57.7px |
| 工作空间列表 | 六列行布局撑破面板（821–1095px 视口） | 72～196px |
| API 用量 | 「操作」列被压到按钮宽度以下，且容器 `overflow:hidden` 时无法滚动查看 | 21.9px |
| API 密钥 | 密钥表最小内容宽度把面板栅格列撑到 962px，被面板裁掉 | 67px |
| API 用量 / API 密钥（1181–1280px） | 当前密钥名称被压到 0 宽；筛选行把「查询」按钮挤出面板 | 14px / 68px |

修复方式遵循“内容自适应 + 容器滚动”，不使用固定像素兜底：表格列宽改回内容驱动（窄容器由 `.table-wrap` 横向滚动承载）、带左右 margin 的表格容器改为 `width:auto`、套餐选项列数改用容器查询、`.data-list` 标签列改为 `min(260px,45%)` 并允许换行、长标识符允许断行。

回归测试：新增 `tests/ui/console-layout-alignment-browser.test.ts`（已注册到 `test:browser:suite`，全部 62 个测试文件仍被脚本覆盖），对 15 条路由 × 4 视口断言“文档无横向溢出 / 非滚动容器内无内容溢出 / 表格单元格无文字或子元素溢出 / 同列面板标题左边界一致”。该测试在修复前的 `822a010b` 上失败（可复现上述全部条目），修复后通过。
# 决策结论

Console UI（`apps/console-ui`）完成一轮系统性整改（以呈现层为主）：统一现代 SaaS 视觉（teal 主色 + 胶囊徽章 + 卡片化信息架构）、修复客户侧与管理侧共 10 处排版缺陷、降低运维页面的信息密度，并修复 SDK 组件 theme 变量未被编译的基建缺陷。

本 PR 以呈现层为主，但不是纯样式改动：客户概览的费用趋势聚合、余额操作的确认交互属于 Console 侧逻辑，已按下述口径修正并在本 PR 内补齐测试。API 合同、后端语义、测试断言的业务文本零改动。

## 当前问题

整改前的 Console 存在四类问题：

1. **排版缺陷**：客户首页中部 400px 空洞、工作空间详情页事实卡露出整条灰色空块、Gateway 指标条 2 列网格装 3 个指标、概览表「查看详情」折行、消息横幅被面板裁切、免责声明贴边、「自动续费」复选框与文字重叠、移动端概览按钮失衡。
2. **基建缺陷**：`vite.config.ts` 未挂任何 Tailwind v4 插件，`@openai/apps-sdk-ui` 的 `@theme` 变量（`--alert-gutter`、`--radius-xl`、`--spacing` 等）从未编译进产物 CSS，依赖该链路的 SDK 组件样式（Alert 内边距/圆角等）全部静默退化为 0。
3. **信息密度**：计费复核为 10 列裸字段表格；资源状态页 13 列详情表 + 12 列工作台表垂直堆叠；登录页为左右分栏旧结构。
4. **视觉风格**：全站零图表、KPI 数字仅 22px、主色只出现在链接上，无品牌记忆点。

## 改动内容

### 基建

- `vite.config.ts` 挂载 `@tailwindcss/vite`，使 SDK `@theme` 变量真正编译进产物（产物 CSS 相应变化：index 214KB→201KB，新增 vendor 87KB 为 Tailwind 预检与主题层）。

### 公开页（home / login）

- home：居中 hero + 产品全景图（原仓库 `assets/branding` 插画，压缩为 230KB JPEG）+ 三特性卡；移除 `min-height: 100vh` 硬拉伸。
- login：移除左右分栏，改为柔和双色渐变背景上的居中单卡片，卡内保留原表单字段与文案。

### 客户侧

- 概览：KPI 数字提升至 30px/700 加粗负字距；新增「近期费用趋势」卡——14 天纯 CSS 条形图（无图表库依赖，桌面与移动端都渲染完整 14 天）；消息横幅在窄面板内动作按钮换行，不再裁切。
- 概览费用趋势口径：金额只读 Ledger 投影的 typed micros 字段（`totalUsdMicros` / `refundUsdMicros` / `chargeUsdMicros`），不从展示文案反解；只有 `completed` 回执产生已确认金额，`billing.workspace_expired.v1` 与 `chargeUsdMicros: 0` 的结案回执按未扣款计零，其余状态与类型一律计入「金额待确认」且不进入汇总；按本地日历日聚合扣款减退款净额，退款日以独立配色标出。
- 概览费用趋势范围：概览额外沿 Ledger cursor 读取费用回执（`limit=100`），逐页读到早于窗口起点的回执为止，覆盖完整 14 天；读取失败、来源不可用或不完整时该卡显示对应状态与重试入口，不显示「无消费」或完整汇总。
- 工作空间详情：事实卡由"背景 + 1px gap"分隔改为逐项边框（消除条目不满时的灰块）；免责声明加内边距；轮换密码按钮对齐。
- Gateway 服务信息：指标条改 3 列等分。
- 新建工作空间：SDK Checkbox 对齐修正；移动端概览按钮布局改为弹性分配。

### 管理侧（五页全部重排）

- 运维概览：健康指标卡内嵌可视化进度条；「注意事项」由三行零散 notice 改为结构化行动卡（状态点 + 标题 + 说明 + 跳转按钮）；来源状态表加行首状态色条。
- 客户与计费账户：首列首字母头像块（teal 渐变）；行首业务状态色条（由 `data-state` 驱动，映射沿用现有 `statusTone` 语义表）；金额列等宽数字。
- 计费复核：10 列裸字段表格改为复核卡片（头部 = 账户 + 计费操作 + 状态徽章；中部三列 = 阶段/错误码/允许动作；底部 = 两个引用 ID 等宽字体）。
- 资源状态：登记表单收进可折叠 details（默认展开，测试可见性不受影响）；13 列详情表改为资源卡行（保留 `<tr>` DOM）；工作台列表 12 列瘦身为 9 列（删除与详情重复的创建时间/续费状态/Receipt ID 列）+ 百分比列宽规则化；行内重复的"来源 · 读回时间"小字只在详情展示。
- 系统状态：6 列服务表改为服务卡行（teal 图标块按健康度着色 + 状态胶囊 + 诊断铺开 + 时间戳退居次要）。
- 余额操作浮窗：移除「再次确认 Account ID」手抄步骤——控制器提交时自动填入 `confirmationAccountId`（API 请求体字段与值不变），确认改为提交时原生弹窗（含目标账户 ID 与操作类型）；结果区改为状态横幅 + 可折叠证据。

### 术语统一

- `phase→阶段`、`errorCode→错误码`、`operation reference→操作引用`、`Receipt reference→回执引用`、`billing operation→计费操作`、`allowedActions→允许动作`、`owner Account/User→归属账户/归属用户`、`paidThrough→权益截止`。
- 保留英文的仅有两类：架构产品名（Control Plane / Gateway / Fabric / Ledger / Sub2API）与代码值原文（如 `manual_review`，以 `<code>` 呈现）。

### 新增样式层

- `apps/console-ui/src/components/ui/sub2api-style.css`（630 行，独立可回滚）：胶囊徽章、16px 卡片圆角、柔和分层阴影、彩色统计图标块、弹窗模糊遮罩、按钮同色系阴影、悬停上浮。以 unlayered 规则覆盖 SDK `@layer components` 内求值异常的 `padding: var(--alert-gutter)`。

## 必须分开的事实

| 类别 | 是否变更 | 说明 |
| --- | --- | --- |
| API 请求/响应形状 | 否 | `confirmationAccountId` 仍随请求体发送，值由控制器从目标账户参数自动填入 |
| 测试断言的业务文本 | 否 | `运行中/已停止/待人工确认/安装镜像目标一致性` 等全部原样保留 |
| 测试定位器依赖的 DOM | 否 | `.operator-*-table tbody tr`、heading 层级、`getByLabel` 字段标签均在 |
| 概览的费用读取 | 是 | 概览新增一次覆盖完整 14 天窗口的费用回执读取（`limit=100` + cursor 分页）；「最近费用」列表仍读取原先的 `limit=3` 第一页 |
| 趋势金额口径 | 是 | 由解析 `presentBillingReceiptAmount` 的展示文案改为读取 typed micros；`扣款 $52.58` 不再被算成 0 |
| 唯一删除的 QA 步骤 | 是 | `tools/console-browser-qa.ts` 中对已移除的「再次确认 Account ID」输入框的一行 fill |

## 验收标准

- [x] `npm test` 217/217 通过（较上一版新增 2 组 typing 口径与 2 组窗口/边界测试）。
- [x] `npm run test:browser:suite` 110/110 通过（含桌面 1440 与移动 390 双视口、无横向溢出断言；新增 4 条概览趋势链路：typed 金额、cursor 覆盖完整窗口、来源不可用、金额待确认）。
- [x] `npm run verify:local` 通过。
- [x] `tests/ui/console-browser-acceptance.test.ts` 3/3 通过（含完整钱包提交流程，验证自动填入的字段值随请求发出）。
- [x] `npm run typecheck` / `npm run lint` / `npm run build` / `git diff --check` / `npm run validate:product-boundary` 通过。
- [x] 资源列表在 1440 视口内列宽规则化后末列按钮完整可见，页面级横向溢出为 0。
- [x] 首页产品图由 1.7MB PNG 瘦身为 230KB JPEG（1280px/质量 70）。
- [ ] 白皮书构建（`node --experimental-strip-types scripts/build-opl-cloud-whitepaper.ts`）：需要 `OPL_FRAMEWORK_REPO` 指向本地框架仓库与 xelatex；与本次 UI 改动无关，CI 环境自带 TeX 工具链。

## 审核后的修正（2026-09-18）

上一轮审核在 `25b26ee6` 上复现了三个问题，均已在本次修正中处理：

1. 趋势图通过 `presentBillingReceiptAmount(...).startsWith("$")` 反解展示文案，而正常回执返回 `扣款 $52.58`，导致真实扣款被算成 0。现改为读取 typed micros，并明确扣款/退款/未扣款/待确认的统计口径。
2. 趋势图只使用概览的 `limit=3` 第一页，无法代表 14 天。现按 Ledger cursor 分页读到窗口起点，覆盖完整范围并明确本地日历日边界。
3. 来源不可用或读取失败时图表仍渲染 14 个零值并标注「无消费」。现区分加载、不可用、读取失败、成功空结果、金额待确认与已确认零金额。

新测试先在 `25b26ee6` 上失败（4 条浏览器用例超时；探针显示完成态扣款回执的图表仍为「最高 无记录 / 无消费」），修正后全部通过。

### 费用趋势范围重做（2026-09-18，`24714cc4`）

概览新增的趋势卡已按产品范围明确为「工作空间净扣款趋势」，不再是 Console 侧按回执拼出的费用近似：

- **范围**：仅工作空间开通/续费的实际扣款 + 关联原订单的实际退款。不含 API 消费、充值、赠送、无订单归属的人工调整、腾讯云采购成本；不把预付月费摊销到天。
- **数据来源**：只读 `GET /api/billing/workspace-settlements`。复用 owner 的 `projectBillingSettlements`，再用 Sub2API 余额变动记录（存储 code、钱包用户、带符号金额、`used_at`）核对每笔资金变动；退役的 `workspace.delete.v1` 退款按其保留行计入。
- **时间口径**：按权威资金记录的生效时间归日，时区固定 Asia/Shanghai；14 天窗口与 `asOf` 由服务端给出，前端不自行分日，不使用回执 `createdAt` / 服务周期 / operation 更新时间。
- **不完整不伪装**：账户无法确认的变动、缺少生效时间、订单属于别的账户、退款合计超过原订单已确认扣款 → `unconfirmedCount` 且 `complete=false`；窗口内无范围数据才是 `empty`；窗口外变动单独计数。
- **职责边界**：无新增服务/计费模块/钱包账本；未改扣款、退款、退款额度或幂等规则；读取不产生任何资金或资源 mutation；前端只渲染 owner 返回的金额与天数。

验收：`TestWorkspaceSettlementTrend*` 覆盖 8 条要求（正常开通续费、失败续费同日全退、跨日/跨窗口退款、部分与多笔关联退款、重复读取、已删除工作空间、不可用/时间缺失/真实零、跨账户隔离且无 mutation）；浏览器覆盖 owner 投影、失败续费扣退、服务端 days 原样呈现、四态区分与“不再从回执重算”。`verify:local` 通过；`verify:local:full` 仅 `services/fabric` Local-Docker 失败，该失败在未改动基线 `25b26ee6` 上同样复现。

## PR 终态

当且仅当 CI 全绿、视觉验收通过且上述验收标准全部确认后合并。
