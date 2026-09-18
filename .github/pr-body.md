# 决策结论

Console UI（`apps/console-ui`）的呈现层完成一轮系统性整改：统一现代 SaaS 视觉（teal 主色 + 胶囊徽章 + 卡片化信息架构）、修复客户侧与管理侧共 10 处排版缺陷、降低运维页面的信息密度，并修复 SDK 组件 theme 变量未被编译的基建缺陷。

本 PR 只改呈现层。API 合同、业务逻辑、后端语义、测试断言的业务文本零改动。

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

- 概览：KPI 数字提升至 30px/700 加粗负字距；新增「近期费用趋势」卡——由费用回执按日聚合的 14 天纯 CSS 条形图（无图表库依赖，移动端自动收成 7 天）；消息横幅在窄面板内动作按钮换行，不再裁切。
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
| 唯一删除的 QA 步骤 | 是 | `tools/console-browser-qa.ts` 中对已移除的「再次确认 Account ID」输入框的一行 fill |

## 验收标准

- [x] `npm test` 213/213 通过。
- [x] `npm run test:browser:suite` 106/106 通过（含桌面 1440 与移动 390 双视口、无横向溢出断言）。
- [x] `tests/ui/console-browser-acceptance.test.ts` 3/3 通过（含完整钱包提交流程，验证自动填入的字段值随请求发出）。
- [x] `npm run typecheck` / `npm run lint` / `npm run build` / `git diff --check` / `npm run validate:product-boundary` 通过。
- [x] 资源列表在 1440 视口内列宽规则化后末列按钮完整可见，页面级横向溢出为 0。
- [x] 首页产品图由 1.7MB PNG 瘦身为 230KB JPEG（1280px/质量 70）。
- [ ] 白皮书构建（`node --experimental-strip-types scripts/build-opl-cloud-whitepaper.ts`）：需要 `OPL_FRAMEWORK_REPO` 指向本地框架仓库与 xelatex；与本次 UI 改动无关，CI 环境自带 TeX 工具链。

## PR 终态

当且仅当 CI 全绿、视觉验收通过且上述验收标准全部确认后合并。
