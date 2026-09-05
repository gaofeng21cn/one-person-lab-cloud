# Console 客户 UX-03 总体汇总审阅

Date: 2026-09-05

State: locally reviewed; completed slices frozen; CI integration gate passed

## 审阅目的与边界

本记录把 UX-03A 至 UX-03G-E 的现状审计、交互层级、页面实现和双视口
证据汇总为一个可交付的客户体验审阅结论。它回答的是“已跑通的业务如何被
客户理解和完成”，不是重新设计产品业务，也不是发布、部署或生产验收。

本轮写集仅为 `docs/history/` 和历史索引。页面实现仍归
`apps/console-ui` Presentation bounded context；Console 继续通过既有
Control Plane typed API 读取产品数据。本轮没有修改 Control Plane、Fabric、
Ledger、Gateway、Sub2API、费用计算、权限、数据库或任何业务合同。

## 完整客户业务链

UX-03 以当前已经跑通的业务路径为固定基线：

```text
public entry
  -> sign in
  -> account state
  -> Overview
  -> Workspace
       -> configure
       -> confirm
       -> provision
       -> obtain access
       -> open
  -> OPL Gateway
       -> service information
       -> create/manage API Key
       -> inspect usage and actual cost
  -> 费用
       -> subscription and renewal
       -> billing records
       -> receipt detail
```

消息和账号信息是跨页面的辅助入口，不改变上述主链。UX-03 的判断标准是：
客户能否在每一步知道当前结果、下一步动作和必要的业务事实，而不是页面
是否展示了更多内部字段。

## 切片完成矩阵

| 切片 | 面向客户的业务结果 | 页面 owner | 数据/事实 owner | 当前状态 |
| --- | --- | --- | --- | --- |
| UX-03A | 建立 Console 客户路径、状态、术语和优先级基线 | `apps/console-ui` | 既有各领域 owner | 已完成现状审计 |
| UX-03B/C | 统一客户 Shell、Overview、品牌识别、账户表面、复选框和响应式基准 | `apps/console-ui` | Control Plane Session/客户投影；既有领域 API | 本地验证完成，参考切片完成 |
| UX-03D | 创建、查看、复制、编辑、启停、重置和删除 Gateway API Key | `apps/console-ui` | Gateway/Sub2API Key 与 Wallet 权威 | 已双视口审阅并冻结 |
| UX-03E | 配置、开通、进入和维护 Workspace | `apps/console-ui` | Control Plane 编排；Fabric Runtime/资源事实 | 已双视口审阅并冻结 |
| UX-03F | 选择 API Key，按周期查看请求记录、Token 和实际费用 | `apps/console-ui` | Gateway/Sub2API Usage、Key、实际费用 | 已双视口审阅并冻结 |
| UX-03G-D | 判断 Workspace 套餐、金额、计费周期和续费状态 | `apps/console-ui` | Control Plane Workspace/续费投影；既有费用事实 | 已双视口审阅并冻结 |
| UX-03G-E | 核对历史交易、Workspace 身份、金额、状态和收据业务详情 | `apps/console-ui` | Ledger Receipt；Control Plane Workspace 名称投影 | 已双视口审阅并冻结 |

这里的“冻结”表示该客户呈现契约和边界已有本地证据，后续新能力不能继续
堆进当前切片；它不表示已经 Push、提 PR、合并、部署或具备生产资格。

## 跨页面一致性结论

### 导航与产品身份

- 客户主导航保持四个任务入口：`概览`、`工作空间`、`OPL Gateway`、`费用`。
- 平台身份是 `OPL Cloud`，API 产品身份是 `OPL Gateway`；客户路径不显示
  内部实现名 `Sub2API`。
- API 相关页面统一使用 `API 密钥`、`用量`、`实际费用` 等任务语言；
  `Workspace`、`Gateway`、`API`、`USD`、`GB` 仅在它们是产品名、单位或
  必须保留的技术事实时出现。

### 信息层级与响应式行为

- Desktop 适合表格和并列比较，Mobile 先展示当前结果、身份、状态和主动作，
  再展开低频或技术内容。
- 参考视口为 `1280x900` 和 `390x844`；Workspace 详情另验证了 `375x812`。
  已冻结页面均无横向文档溢出，移动端固定底部导航不遮挡主内容。
- 移动端关键操作达到可用触控尺寸；Gateway Key 菜单向上展开，删除动作
  直接标为“删除”，不要求客户理解“危险操作”这种抽象分类。
- 客户默认只看到完成任务所需的字段。技术详情按需披露，收据普通路径已移除
  `Receipt ID`、`priceVersion`、`chargeReference`、原始枚举、Provider 证据和
  fulfillment 等内部字段。

### 状态、错误与敏感信息

- Loading、empty、unavailable、error 和待确认状态继续分别表达，不把未知值
  伪装成健康的零值。
- Receipt 列表与 Workspace 名称投影独立失败；Usage Summary 与 Usage 记录
  独立加载和重试；一个来源失败不会抹掉另一个来源已有事实。
- Workspace 登录密码和 API Key 默认隐藏，显式显示后保留现有页面内存边界和
  60 秒自动隐藏；没有把 Secret 变成新的持久化或业务 owner。
- 客户文案不再指向已经退休的 Support 工单能力；失败路径指向当前可执行的
  重试或技术详情。

## DDD 与服务边界复核

| bounded context / 模块 | 本轮允许的职责 | 本轮明确没有做的事 |
| --- | --- | --- |
| `apps/console-ui` | 页面层级、客户文案、响应式呈现、typed API 调用 | 不持久化、不计算费用、不调用 Provider、不拥有 Wallet/Ledger |
| Control Plane | Session、客户 DTO、Workspace 编排、续费和客户投影 | 不拥有 Gateway Wallet、Provider 资源或 Ledger 表 |
| Fabric | Runtime、计算、存储、Secret、Provider 适配和事实读回 | 不拥有客户余额、账单或页面展示 |
| Ledger | Receipt、证据、保留和对账事实 | 不执行 Provider mutation、不编排 Workspace |
| Gateway/Sub2API | Gateway、API Key、Usage、Wallet 权威 | 不在 Cloud Console 中复制第二个 Gateway、Wallet 或费用 owner |

因此，本轮“解耦”是页面展示模型与领域事实 owner 的解耦：页面可以把
`*UsdMicros` 映射为客户可读的美元，把原始枚举映射为任务语言，但边界请求、
幂等、权限、费用和状态机仍由原 owner 管理。没有绕过 Controller，也没有
新增第二个 Billing、Gateway、Wallet 或 Workspace authority。

## 验证与证据

各最终切片的聚焦验证已通过，主要结果为：

```text
npm run test:browser:billing              # 9/9
npm run test:browser:customer-experience # 2/2
npm run test:browser:gateway-usage       # 15/15
npm run test:browser:workspace-lifecycle
npm run typecheck
npm run lint
npm run build
npm run verify:local
git diff --check
```

对应切片阶段的 `npm run verify:local` 曾通过 Node source tests、Console browser
suites、TypeScript、Vite build、Go module compile/database-free tests、product
boundary 和 whitespace gate。双视口截图均保存在仓库外的 `/tmp` 证据目录，
不作为产品资源提交；各切片的截图路径和哈希留在对应历史记录中。

### Rebase 后门禁状态

本分支随后已 rebase 到最新 `origin/main` `8a160289`，当前 HEAD 为
`f15b5731`，其中已包含 acceptance selector 修复。rebase 后重新运行
`node tools/console-browser-qa.ts --network=fake-only` 结果为 `ok: true`；
Node source tests（194 条）、Billing、Gateway Usage、Console owner reads、
Customer Experience、Operator、Workspace lifecycle、TypeScript、lint、Vite
build、product boundary 和 whitespace gate 均通过。

完整 `npm run verify:local` 在当前机器曾因 Go 依赖下载受阻而未完成：Go 首次编译需要下载最新主干
引入的 `entgo.io/ent@v0.14.6`、`github.com/lib/pq@v1.12.3` 和 Atlas 版本，
`proxy.golang.org` 以及 GitHub 直连均因网络超时失败。该失败发生在依赖获取阶段，
没有产生 Go 编译或测试失败结论。随后 Draft PR #530 的 GitHub Actions
`validate` job（run `33969005460`）在标准 CI 网络中执行同一 `npm run verify:local`
并通过；`dependency-review` 也通过。该 CI 结果证明当前 PR 提交的仓库门禁通过，
但不把本机网络状态改写成代码测试结果。

## 已闭合的整链路阻塞

总体审阅首次记录时，独立重跑：

```text
node tools/console-browser-qa.ts --network=fake-only
```

曾在 Workspace 访问凭据步骤失败，退出码为 `1`：

```text
locator.click: Timeout 30000ms exceeded
waiting for locator('.workspace-access-panel dt')
  .filter({ hasText: /^API 密钥$/ }).locator('..')
  .getByRole('button', { name: '显示' })
```

根因是 acceptance owner 的旧定位器：`SecretRow` 在 `dt` 下增加客户用途
说明后，脚本仍对 `dt` 完整文本使用 `^API 密钥$` 精确匹配；Gateway Key
页面已把低频操作收进 `details.key-more-actions`，脚本也曾在折叠状态下直接
寻找操作按钮；账单页面则同时保留桌面表格和移动卡片，页面级文本断言命中
两个视口的 DOM。页面、Secret Controller 和业务接口均不是问题 owner。

本次只修改 `tools/console-browser-qa.ts`：

- Workspace Key 行改为匹配客户标签 `dt > span`；
- Gateway Key acceptance 增加按需展开且不重复收起 `key-more-actions` 的局部
  helper；
- 账单列表断言限定当前视口 surface，收据断言限定 `.receipt-detail`。

修复后重新运行同一命令，结果为 `ok: true`，`externalRequests: 0`，
`consoleErrors: []`，Workspace navigation、billing views、secret cleanup 和
高风险流程均通过。该修复没有改动 `WorkspaceDetailPage`、
`WorkspaceSecretController`、Gateway、Ledger 或任何产品业务代码。

## 交付结论与下一步

UX-03 已完成“业务路径审计 -> 交互层级 -> 页面实现 -> 双视口验证 -> 切片
冻结”的本地闭环。交付物不是一套孤立的漂亮页面，而是：

- 一条可解释的客户业务链和导航结构；
- 各页面的客户字段、默认信息和技术详情边界；
- 每个切片的页面 owner、数据 owner 和 DDD 写集；
- Desktop/Mobile 运行时证据、聚焦测试和冻结记录；
- 全量 QA 问题的 owner 判断、最小修复和复验结果。

当前不合并主干。CI 和依赖审阅已经通过，下一步是人工 PR 审阅；审阅通过后再
合并主干。最短后续链路是：

```text
CI 完整门禁通过
  -> 人工 PR 审阅
  -> 审阅通过后再合并 main
```

本记录不更新 `docs/status.md` 或 `docs/roadmap.md` 的生产/发布状态；这些
结论仍需在进入 canonical `main` 并满足各自证据层级后，由对应 owner 更新。
