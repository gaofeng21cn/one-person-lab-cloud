# Console Workspace 页面 UX-03E-D 最终双视口审阅

Date: 2026-09-04

State: locally verified

## 审阅目的

UX-03E-D 对 UX-03E-C 的 Workspace 列表、开通和详情页面做最终双视口
收敛。目标是确认已批准的任务优先层级在真实 fake-only Console 运行时成立，
并修复会让客户误操作或无法稳定操作的呈现问题。

本阶段只允许修改 `apps/console-ui` 的 Workspace Presentation bounded
context 和对应测试，不重新设计 Workspace 业务，不修改 API、DTO、Controller、
后端、持久化或权威关系。

## Reconcile

- 目标：冻结 Workspace 列表、开通、详情这一完整客户 UI/UX 切片；
- 主模块 owner：`apps/console-ui`；
- 当前产品边界：`docs/product/console-workspace-v1.md`；
- 交互输入：UX-03E-A 现状审计、UX-03E-B 交互层级、UX-03E-C 本地实现；
- 真实调用方：Workspace 页面通过既有 Controller 调用 Control Plane 产品 API；
- 精确写集：Workspace 展示模型、详情页、Workspace 响应式样式、focused UI tests
  和本审阅记录；
- 完成证据：双视口运行时读数、截图哈希、聚焦测试和 `verify:local`。

## 审阅发现与收敛

### 1. 客户表单暴露内部金额单位

预算表单直接展示 `micros` 并要求客户输入整数。该单位属于 API/存储精度，
不是客户决策语言；客户需要自行换算，且容易把 10 美元输入成 10 micros。

收敛方式：

- 表单标签统一改为“美元”；
- DTO 和 PATCH 请求继续使用原有 `*UsdMicros` 整数；
- 在 `workspace-experience-model.ts` 中以 `BigInt` 完成十进制字符串与 micros
  的精确双向转换，不经过浮点计算；
- 支持最多 6 位小数，并受现有 JavaScript safe integer 请求边界约束；
- `12.345678` 美元的实际 PATCH 值验证为 `12345678` micros；
- 页面不再显示 `micros`，但没有修改任何服务合同或后端语义。

### 2. 移动端关键控件低于可用触控尺寸

Workspace 详情返回按钮和预算输入框原高度分别为 `32px` 和 `38px`，会增加
误触和输入困难。虽然共享移动端规则覆盖 `.ui-button` 和
`.ui-field__control`，Apps SDK 的 size 选择器与实际 Input DOM 类名使这两个
控件没有取得 `44px` 计算高度。

收敛方式：

- 仅在移动端为 Workspace 返回按钮和预算输入设置更具体的 `min-height: 44px`；
- focused browser test 同时验证控件高度不低于 `44px`，并且滚动后不被固定
  底部导航遮挡；
- 实机读数：返回按钮 `343x44px`，四个预算输入均为 `309x44px`；
- `documentElement` 和 `body` 的 `scrollWidth` 均等于 `clientWidth`，无横向
  溢出。

## DDD 展示边界

本次修复继续留在 Workspace Presentation bounded context：

- `workspace-experience-model.ts` 负责客户金额输入与 API 精度之间的展示映射；
- `WorkspaceDetailPage.tsx` 负责预算表单的客户语言和交互；
- Controller 继续拥有命令、busy/error 状态、幂等意图和权威回读；
- Control Plane 继续是 Console 产品 API 和 Workspace 编排 owner；
- Sub2API 继续是 Gateway、Wallet、Key 和 Usage 的内部权威；
- Fabric 继续拥有 Runtime 和 provider 资源事实。

因此，这次“解耦”是展示模型与业务合同的分离，不是在 Console 中复制金额、
预算或 Gateway Domain。页面接收美元，边界适配器仍发送既有 micros 合同。

## 双视口证据

- Desktop：`1280x900`；完整详情长图为 `1280x1981`；
- Mobile 列表/开通：`390x844`；详情修复复核：`375x812`；
- 截图保存在仓库外，不进入产品提交。

| 文件 | SHA-256 |
| --- | --- |
| `/tmp/ux03e-d-workspace-list-desktop.png` | `9ef9a217a1163474af8549d841750c0ebe51d46c4cf5e7e016783bea8a56653b` |
| `/tmp/ux03e-d-workspace-launch-desktop.png` | `e753c7d0e1b24f1f5f08a6c5f91b6d2646424b65d7a9510200e57c1a6fcfe754` |
| `/tmp/ux03e-d-workspace-management-desktop-final.png` | `0e0b90bb332169b75e597efae599de07e7dd46f650a05611a8d7737b1b5db8ef` |
| `/tmp/ux03e-d-workspace-list-mobile.png` | `1e3a446273c59d6c21d343401ea14e64862db9ae3c505d0f1c5da296d09232a9` |
| `/tmp/ux03e-d-workspace-launch-mobile-viewport.png` | `352de567f8c1a163e474173e11d09d267750241bfd0a6ad8bf1c5ccabfedfe6f` |
| `/tmp/ux03e-d-workspace-launch-mobile-bottom.png` | `c05573f8ff471978622336bc7177174a0b5c323a15a2c8bbb933767b8832304a` |
| `/tmp/ux03e-d-workspace-detail-mobile-final-top.png` | `b14d8d8b2788dce5cfc2805bcf0e85a2870a90effcc6ce992aa353599e4d397b` |
| `/tmp/ux03e-d-workspace-budget-mobile-final.png` | `4910703a68fcbf2b267bd1b8f10a7ac3c639b57014181b0a46798653e5b2f578` |
| `/tmp/ux03e-d-workspace-delete-mobile-final.png` | `48fa6313bc2afe905be2d8984d038ece768936cb656064c22e92c07c0ceb382d` |

运行时确认：

- 列表仍以名称、套餐、当前状态和权益截止支持查找与进入；
- 开通仍按配置、核对、开通状态推进，并保留原有一次提交和权威回读；
- 详情仍按身份、访问凭据、续费与存储、预算与维护、删除、技术详情排序；
- 预算显示客户金额，保存时精确转换回现有 API 单位；
- 移动端操作不被底部导航遮挡，桌面和移动端均无横向溢出；
- fake-only Vite HMR WebSocket 有测试宿主端口连接失败日志，不属于应用请求
  或产品错误；自动化浏览器套件关闭 HMR 后通过。

## 验证命令

以下命令均通过：

```text
npm run typecheck
npm run lint
node --test tests/ui/workspace-experience-model.test.ts
node --test tests/ui/workspace-task-experience-browser.test.ts
npm run test:browser:workspace-lifecycle
npm run build
npm run verify:local
git diff --check
```

聚焦结果：

- Workspace 展示模型：`11/11`；
- Workspace 双视口浏览器链：`10/10`；
- 精确金额转换、真实 PATCH 值、移动触控尺寸和无横向溢出均有自动化断言。

## 继承项

共享 Console Shell 的消息和账号图标按钮在移动端仍为 `32px`。它们不属于
Workspace 切片，UX-03E-D 不跨边界修改；后续应在 Shell owner 的独立审阅中
统一处理，避免一个页面私自覆盖共享导航行为。

## 业务与权威边界

本阶段没有修改：

- Workspace API、DTO、Controller、Control Plane、Fabric、Ledger 或 Sub2API；
- Workspace 开通、续费、预算和删除的权限、CSRF、幂等键、请求顺序和权威回读；
- Runtime URL、Secret reveal、页面内存保存、互斥显示和 60 秒自动隐藏；
- Gateway、Wallet、Key、Usage、计费、数据库、部署或 Instance 状态。

## 交付状态

UX-03E-A（现状审计）、UX-03E-B（交互层级）、UX-03E-C（DDD 页面实现）和
UX-03E-D（最终双视口审阅与收敛）已完成本地闭环。Workspace 页面切片可以
冻结，不再继续追加非必要功能。

本阶段未 Push、未提 PR、未合并主干、未部署，不代表 Candidate、Instance
资格或生产可用性。
