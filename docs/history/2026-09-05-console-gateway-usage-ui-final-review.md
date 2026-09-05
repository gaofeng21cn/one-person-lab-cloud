# Console Gateway Usage 页面 UX-03F-D 最终双视口审阅

Date: 2026-09-05

State: locally verified and frozen

## 审阅目的

UX-03F-D 对 UX-03F-C 已实现的 Gateway Usage 客户页面做最终双视口收敛。
目标是确认结果优先层级、Key/周期选择、请求记录阅读和技术详情披露在桌面与
移动端都能稳定理解和操作；本阶段不新增业务能力。

## Reconcile

- 目标：冻结 Gateway Usage 这一完整客户 UI/UX 切片；
- 主模块 owner：`apps/console-ui`；
- 交互输入：UX-03F-A 现状审计、UX-03F-B 交互层级、UX-03F-C 本地实现；
- 真实调用方：Gateway Usage 页面通过既有 Console read API 和 Controller 读取
  Sub2API/Gateway 权威事实；
- 本阶段精确写集：最终视觉审阅记录和历史索引；未修改产品源码；
- 完成证据：双视口运行时读数、最终截图哈希、Gateway Usage focused tests 和
  `verify:local`。

## 最终审阅结论

本轮没有发现需要修复的视觉或交互阻断项，UX-03F-C 的实现可以冻结：

- 桌面端以真实语义化 table 呈现时间、模型、Token、实际费用和操作，扫描顺序
  与结果优先设计一致；
- 移动端折叠侧栏并保留顶部产品识别，当前 Key、统计周期、三项汇总和第一条
  请求在首屏可见；
- 桌面和移动端均无页面横向溢出，移动请求卡片保持稳定的单列信息层级；
- 技术详情默认关闭，展开后显示 API 路径、缓存 Token、延迟和 Request ID，
  不让低频字段抢占业务结果；
- Key 选择弹窗的搜索、分页、Escape 关闭、焦点陷阱和关闭后焦点返回均可用；
- Key 第 21 条及以后可通过查询分页到达，错误页身份不会覆盖当前请求记录；
- 客户界面继续使用 `OPL Gateway`，不显示内部实现名 `Sub2API`；
- Summary 与 Usage 的加载、失败和重试仍彼此独立，不改变任何费用或业务事实。

## DDD 与服务边界

本次冻结继续留在 Cloud Console Presentation bounded context：

- `GatewayUsagePage` 负责页面层级、客户文案、响应式呈现和技术详情披露；
- `useGatewayUsageController` 负责当前 Key、查询分页、周期、Usage page 和
  freshness，不拥有 Gateway、费用、数据库或服务状态；
- Sub2API/Gateway 仍是 Key、Usage 和实际费用的权威来源；
- Control Plane API、DTO、Ledger、数据库、Gateway/Sub2API 后端和部署仍保持
  不变。

因此，本阶段没有把 UI 审阅误扩展成业务重构，也没有创建第二个 Gateway、Wallet
或 Billing owner。

## 双视口证据

截图由 `tests/ui/gateway-usage-controller-browser.test.ts` 在 fake-only demo 中
生成，截图保存在仓库外，不进入产品提交。

| 文件 | Dimensions | SHA-256 |
| --- | --- | --- |
| `/tmp/ux03f-d-current/gateway-usage-desktop.png` | `1280x900` | `6fedec0bd9449ea72b926d5353705ab6f7043bbf052c49b7ec08090ee90df351` |
| `/tmp/ux03f-d-current/gateway-usage-mobile.png` | `390x844` | `89aaed2f78e498b7c179a13e253784522670731cd0f5db1255dd58a0f091a4c2` |

运行时确认：

- Desktop 首屏显示 OPL Gateway、当前 Key、周期、汇总和请求记录；
- Mobile 首屏显示当前 Key、周期、汇总和第一条请求，底部导航不遮挡内容；
- `documentElement.scrollWidth === clientWidth` 且 `body.scrollWidth === body.clientWidth`；
- 移动端详情、搜索框、Key 选项和分页控件保持可操作尺寸；
- 技术详情默认关闭，可通过真实详情控件展开；
- 页面没有对外部网络发起请求。

## 验证结果

以下命令通过：

```text
npm run test:browser:gateway-usage  # 15/15
npm run verify:local
git diff --check
```

最终 `verify:local` 通过 product boundary、194 条 Node source tests、全部 Browser
suites、TypeScript typecheck/lint、Vite production build、所有 Go module
compile/database-free tests 和 Git whitespace gate。

## 继承项

共享 Console Shell 的移动端消息和账号图标按钮仍由 Shell owner 统一治理。本次
审阅没有在 Gateway Usage 页面私自覆盖共享导航规则，避免把 Shell 问题伪装成
Usage 局部修复。

独立的完整 fake-only journey 仍可能在既有 Workspace access 步骤等待“API 密钥
-> 显示”按钮时超时；该上游阻断不属于 Gateway Usage，Usage 专属 15 条测试和
双视口证据均已通过。

## 交付状态

UX-03F-A（现状审计）、UX-03F-B（交互层级）、UX-03F-C（DDD 页面实现）和
UX-03F-D（最终双视口审阅与冻结）已完成本地闭环。Gateway Usage 页面切片冻结，
后续新增 Workspace、Gateway Usage 以外的费用或其他模块时，应从各自的 owner
和独立 UX 切片重新开始，不在本切片继续堆叠功能。

本阶段未 Push、未提 PR、未合并主干、未部署，不代表 Candidate、Instance 资格或
生产可用性。
