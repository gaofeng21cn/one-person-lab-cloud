# Console Gateway Key 页面 UX-03D-D 最终双视口审阅

Date: 2026-09-04

State: locally verified

## 审阅目的

UX-03D-D 对 UX-03D-C 的 Gateway Key 页面做最终双视口收敛，验证已批准的结果优先、移动端筛选折叠、低频操作分组是否在真实 fake-only Console 运行时成立。审阅只允许修改呈现层和 focused browser test，不改变业务、API、权限或数据所有者。

## 审阅发现与收敛

发现移动端展开“更多操作”时，菜单若按文档流向下展开，可能进入固定底部导航区域。该问题会遮挡管理、维护和删除动作，属于交互可见性缺陷。

收敛方式：

- 移动端 `.mobile-key-actions` 建立定位上下文；
- “更多操作”菜单改为向上展开，保持在固定底部导航上方；
- 菜单内最后一组从“危险操作”改为直接标明“删除”，让用户无需理解抽象分类即可知道动作结果；
- 桌面端继续保留原有向下展开和表格高密度操作；
- 新增浏览器断言，确保移动端展开菜单底部不超过 `.mobile-bottom-nav` 顶部。

## 双视口证据

- Desktop: `1280×900`
- Mobile: `390×844`
- 截图：
  - `/tmp/ux03d-d-desktop.png`
  - `/tmp/ux03d-d-mobile.png`

SHA-256：

| 文件 | SHA-256 |
| --- | --- |
| `ux03d-d-desktop.png` | `9f31367e54904259be995415c181db94b6a14ac94dcaec34e087870d966eabd1` |
| `ux03d-d-mobile.png` | `50f4c767a5ed5d3b76083fdc8e96376d980d60af05d489b886415ea8ef093c15` |

运行时读数：

- Desktop：菜单位于 `x=1001.5, y=354.4, w=218, h=146`；无横向溢出。
- Mobile：菜单位于 `x=33, y=482.4, w=324, h=146`；固定底部导航从 `y=779` 开始，菜单完全位于其上方；无横向溢出。
- 两个视口均无外部请求。
- fake-only Vite HMR WebSocket 在独立脚本运行时会出现连接失败日志，这是测试宿主的 HMR 端口，不属于应用请求或产品错误；既有浏览器测试关闭 HMR 后通过。

## 验证命令

通过：

```text
node --test tests/ui/customer-console-task-experience-browser.test.ts
node --test tests/ui/workspace-mvp-flow.test.ts
npm run typecheck
npm run lint
npm run build
npm run verify:local
```

`npm run verify:local` 的默认 Node、Billing、Gateway usage、Console owner-read、Announcement、Customer experience、Operator、Workspace lifecycle、TypeScript、Vite build、Go 编译和边界检查均通过；其中 Workspace lifecycle 在首次全量并发运行时出现一次独立时序波动，单独重跑 `tests/ui/workspace-mvp-flow.test.ts` 通过，随后整体默认门禁通过。

## 业务边界

本阶段没有修改：

- Gateway API、DTO、Control Plane、Sub2API；
- Gateway、Wallet、Key、Usage 的权威关系；
- 权限、Workspace 系统 Key 限制；
- Reveal 的 POST 边界、页面内存保存和 60 秒自动隐藏；
- 创建、编辑、启停、重置、删除的 CSRF、幂等键、回读确认和删除确认。

## 交付状态

UX-03D-A（现状审计）、UX-03D-B（交互层级）、UX-03D-C（页面实现）、UX-03D-D（双视口最终审阅与收敛）已完成本地闭环。

当前 commit：`4b8b79a5 fix(console): keep mobile gateway key actions visible`。

本阶段未 Push、未提 PR、未合并主干、未部署，不代表 Candidate、Instance 资格或生产可用性。
