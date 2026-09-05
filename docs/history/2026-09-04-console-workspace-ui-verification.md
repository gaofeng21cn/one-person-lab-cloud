# Console Workspace 页面 UX-03E-C 实现验证

Date: 2026-09-04

State: locally verified

## 目标与边界

UX-03E-C 将已批准的 Workspace 列表、开通和详情交互层级落到客户
Console。实现按 Workspace 展示领域拆分，目标是降低页面耦合并让代码结构与
客户任务一致；它没有重新定义已经跑通的 Workspace 业务链。

本记录是该 UI 行为变更的持久本地验证记录，不是产品架构、部署或生产状态
的权威来源。

## DDD 展示边界

本次 DDD 划分限定在 `apps/console-ui` 的 Presentation bounded context：

- `CustomerPages.tsx` 在 Workspace 范围内只保留路由分发和现有 Controller 注入；
- `components/workspaces/WorkspaceListPage.tsx` 负责查找与进入 Workspace；
- `components/workspaces/WorkspaceLaunchPage.tsx` 负责配置、核对和开通结果；
- `components/workspaces/WorkspaceDetailPage.tsx` 负责进入、凭据、续费、预算、
  维护、删除和技术证据的展示；
- `components/workspaces/workspace-shared.tsx` 只承载该展示领域内复用的导航、
  分页和格式辅助；
- 页面通过窄 `Pick<ConsoleController, ...>` 或既有 Workspace Controller 类型
  消费能力，不取得整个 Console 的业务所有权；
- `workspace-experience-model.ts` 继续拥有客户状态映射，既有 Controller 继续
  拥有交互状态、幂等意图和权威回读。

Control Plane、Fabric、Gateway、Wallet、Billing 和 Sub2API 的 API、DTO、
状态机、权限、持久化与权威边界均未迁移。这里的 DDD 是按客户能力组织前端
展示，不是在 Console 内复制后端 Domain。

UX-03E-B 原最小写集只列出 `CustomerPages.tsx`。用户随后明确要求 UX-03E-C
按 DDD 解耦，因此实现将原文件内的 Workspace 页面提取到上述领域目录；这是
展示代码的所有权细化，不改变业务调用或外部合同。

## 实现结果

- 列表用客户语言“当前状态”代替字段元语言“生命周期状态”，每行增加明确的
  “查看详情”，同时保留整行进入行为；
- 开通页面结构和 Controller 调用保持不变，继续保留配置、核对、一次提交、
  幂等和权威结果回读；
- 详情页为登录账号、登录密码和 API 密钥标明使用目的；
- “预算设置”只保存预算，“用量维护”只执行两类用量重置；
- 删除工作空间成为独立高风险区域，不再与预算配置共用分组；
- 技术详情独立且默认关闭，原始状态、Runtime、预算和错误证据仍只在其中显示；
- 移动端凭据按钮至少为 `44×44px`，展开的维护和删除动作不会被固定底部导航
  遮挡；
- OPL 图标和其他客户页面未修改。

## 双视口证据

- Desktop：`1280×900`
- Mobile：`390×844`
- 截图保存在仓库外，不进入产品提交。

| 文件 | SHA-256 |
| --- | --- |
| `/tmp/ux03e-c-workspace-list-desktop-viewport.png` | `9ef9a217a1163474af8549d841750c0ebe51d46c4cf5e7e016783bea8a56653b` |
| `/tmp/ux03e-c-workspace-detail-desktop-viewport.png` | `5b5d35100f84b62fde187583d6dae140797965ca0cdedf7ff451cec0acaa6129` |
| `/tmp/ux03e-c-workspace-list-mobile-viewport.png` | `1e3a446273c59d6c21d343401ea14e64862db9ae3c505d0f1c5da296d09232a9` |
| `/tmp/ux03e-c-workspace-detail-mobile-viewport.png` | `2216fd86ec88d30d3aa6350c998df2a8b8133d0fa43b61c7c01decfd4945a2eb` |
| `/tmp/ux03e-c-workspace-management-mobile-viewport.png` | `77eea0c4cf8bd4f9d34157ad44fbaa6f3002c1aad44693852cd63609fda5e86e` |
| `/tmp/ux03e-c-workspace-delete-mobile-viewport.png` | `c422547eebaa6114ab5d63f22dcc7553c54b9b67d9622920ce17e14ff6440c53` |

运行时确认：

- 桌面和移动端均能识别名称、套餐、状态、权益截止和查看详情入口；
- 详情区顺序为身份、访问凭据、续费与存储、预算与维护、删除、技术详情；
- 两个视口均无横向溢出，移动端操作不被底部导航遮挡；
- 无 page error、应用 Console error 或外部请求。

## 验证命令

以下命令均通过：

```text
npm run typecheck
npm run lint
node --test tests/ui/workspace-task-experience-browser.test.ts
node --test tests/ui/customer-workspace-read-navigation-browser.test.ts tests/ui/fabric-runtime-read-controller-browser.test.ts
npm run test:browser:workspace-lifecycle
npm run build
npm run verify:local
git diff --check
```

结果：

- Workspace 页面端到端：10/10；
- Workspace/Fabric owner-read 边界：9/9；
- Workspace 生命周期：18/18；
- `verify:local`：产品边界、193 条 Node source tests、所有 Console 浏览器
  套件、TypeScript、Vite build、Go contracts 与 Control Plane、Fabric、Ledger
  编译和数据库无关测试全部通过。

## 保留的业务与权威边界

- Workspace 列表、详情、Runtime 与预算仍通过现有同源 Control Plane adapter
  读取；
- Runtime URL 只来自 Fabric 权威回读；
- Sub2API 仍是 Gateway、Wallet、Key 和 Usage 的内部权威；
- Secret 仍显式 reveal、只保存在页面内存、互斥显示并在 60 秒后隐藏；
- 开通、续费、预算和删除仍保留既有 CSRF、幂等、请求顺序和权威回读；
- 未修改后端、合同、DTO、数据库、权限或部署资产。

## 交付状态

UX-03E-C 已在本地 fake-only 运行时实现并验证。本记录不代表 PR、主干合并、
Push、部署、Candidate、Instance 资格或生产可用性。
