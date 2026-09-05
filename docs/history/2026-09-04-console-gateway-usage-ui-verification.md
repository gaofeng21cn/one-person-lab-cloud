# Console Gateway Usage UI Verification UX-03F-C

Date: 2026-09-04

State: locally verified

## 目标与边界

UX-03F-C 将已批准的 UX-03F-B 交互层级落到 Cloud Console 的 Gateway Usage
页面：先明确当前 API 密钥和周期，再呈现请求次数、总 Token、实际费用与请求
记录；低频技术事实按需展开。

本阶段只修改 `one-person-lab-cloud` 的 Console presentation、Console-owned
query state、focused browser tests 和 fake-only QA 断言。部署相同 Cloud 产物后，
这些变化才会体现在 `cloud.medopl.com`。本阶段没有修改 OPL Gateway/Sub2API、
Control Plane API 或 DTO、Ledger、数据库、费用逻辑、部署配置和线上环境。

本记录证明本地源码和 fake-only 浏览器行为，不代表 PR、主干合并、Candidate、
Instance qualification、部署或生产可用性。

## 源码身份

- Branch: `codex/workspace-task-experience`
- Verified parent: `d61ca8df` (`docs(console): define gateway usage interaction hierarchy`)
- UX-03F-A audit: `c9bc1dc3`
- UX-03F-B design: `d61ca8df`
- UX-03F-C implementation and this verification record are committed together.

## 已交付结果

- Gateway Usage presentation 从 `CustomerPages.tsx` 拆到专属组件；页面只消费
  窄化的 `GatewayUsageController`，不持有 Gateway、费用或持久化 authority。
- Key 查询列表与当前 Usage 范围解耦，支持现有 API 的显式搜索和分页，第 21 个
  及以后的合法 Key 可达。
- 页面刷新通过现有单 Key readback 确认当前 Key；只有
  `404 gateway_key_not_found` 清空范围，transient failure 保留 Key 身份并隐藏
  旧结果。
- Session、路由、Key、周期、Key query 和 Usage page 各自受 generation/scope
  约束，迟到响应不能覆盖当前结果。
- Summary 与 Usage 独立加载、失败和重试；明细翻页不重复读取 Summary，失败不
  推进已提交页。
- 默认请求记录只显示时间、模型、输入/输出 Token 和实际费用；API 路径、缓存
  Token、延迟和 Request ID 进入按需技术详情。
- 桌面保留真实 table/column header 语义；移动端使用稳定单列请求卡片。Key
  选择器使用普通 list + button 语义，并复用既有 Modal 的 Escape、焦点陷阱和
  焦点返回。
- 客户界面继续显示 `OPL Gateway`，不显示内部实现名 `Sub2API`。

## 审查修正

独立代码审查发现并关闭了 4 项问题：

1. 桌面请求记录从无表格语义的网格恢复为真实 `<table>`，详情由带
   `aria-expanded`/`aria-controls` 的按钮控制。
2. Key Modal 每次打开同步清空搜索 draft，输入值与实际查询结果保持一致。
3. Key 列表从未实现完整键盘模型的 `listbox/option` 改为与真实交互一致的
   `list/button`。
4. Usage available 响应在提交前严格核对请求页与固定 `pageSize`；错误分页身份
   不再进入当前结果或推进已提交页，并可按原目标页重试。

审查未发现越过 Cloud Console owner、创建第二个 Gateway/Wallet/Billing owner
或进行不必要后端开发。

## 双视口证据

截图由 `tests/ui/gateway-usage-controller-browser.test.ts` 在 fake-only demo 中
生成。移动截图为 `390x844` viewport 的 full-page 输出。

| Evidence | Dimensions | SHA-256 |
| --- | --- | --- |
| `/tmp/ux03f-c/gateway-usage-desktop.png` | `1280x900` | `241316db7f94983821eef710d8e20aaa6a8275bfd6305fbbf1fbe970e53f473e` |
| `/tmp/ux03f-c/gateway-usage-mobile.png` | `390x844` | `b084776fd484b172f309acaa7839487e5b77de3aa32c1f9fc8132cad5ab53853` |

双视口 focused test 验证：无页面横向溢出；移动首屏可见当前范围、三项汇总和
第一条请求；技术详情默认关闭且可展开；Modal 支持 Escape 和焦点返回；移动
交互目标不小于 `44x44px`；无外部请求。development demo 仍会产生仓库已记录
的 Vite HMR WebSocket page/Console error；不使用字符串白名单把它伪装成零错误。
production-dist browser test 验证正式构建在两个视口无 page error、Console
error、资源失败和外部请求。

## 验证结果

以下命令通过：

```text
npm run test:browser:gateway-usage  # 15/15
npm run lint
npm run build
node --test tests/ui/console-production-dist.test.ts  # 1/1
node --test --test-name-pattern="leaves one endpoint owner" tests/ui/gateway-account-read-controller-browser.test.ts  # 1/1
node --test --test-name-pattern="customer task pages use customer language" tests/ui/customer-console-task-experience-browser.test.ts  # 1/1
npm run verify:local
git diff --check
```

最终 `npm run verify:local` 通过 product boundary、194 条 Node source tests、
全部 Browser suites、TypeScript typecheck/lint、Vite production build、所有 Go
module compile/database-free tests 和 Git whitespace gate。

## 已知上游阻断

独立运行以下完整 fake-only journey 未到达 Gateway Usage 步骤：

```text
node tools/console-browser-qa.ts --network=fake-only
```

它在既有 Workspace access 步骤等待“API 密钥 -> 显示”按钮时超时：

```text
.workspace-access-panel dt /^API 密钥$/ -> parent -> button "显示"
```

该定位和 Workspace 页面不在 UX-03F-C 写集。Usage 专属 15 条 browser tests、
双视口错误/网络断言、customer experience、owner-read 和完整 `verify:local` 已
通过；因此本记录不把未执行到 Usage 的完整 journey 写成通过，也不把该上游
阻断误归因为 Gateway Usage 失败。

## 结论与下一步

UX-03F-C 已完成本地实现和工程验证。下一阶段是 UX-03F-D：只对这一完整 Gateway
Usage UI/UX 切片做最终双视口审阅和冻结，必要时修正 presentation/test，不扩展
业务、不修改 Gateway/Sub2API 或后端，也不执行部署。
