# Console Gateway Key 页面 UX-03D-C 实现验证

Date: 2026-09-04

State: locally verified

## 目标与边界

UX-03D-C 将已批准的 Gateway Key 交互层级落到客户 Console 页面：结果优先、移动端筛选折叠、查看/使用与管理/维护/危险操作分组。没有改变 Gateway 业务、API、DTO、权限、状态所有者或敏感值生命周期。

## 实现内容

- `KeysPanel` 保留现有 Endpoint、创建、刷新、Key 读取、Reveal、创建、编辑、启停、两种重置、删除、分页和技术详情。
- 移动端先渲染 Key 结果，再渲染默认关闭的“筛选与排序”；第一条 Key 在 `390×844` 首屏进入。
- 查看/使用动作保持直接可见；编辑、启停、两种重置和删除进入每条 Key 的“更多操作”，并按管理、维护、危险操作分组。
- 桌面端仍使用表格，操作列保留高密度使用方式并增加分组菜单。
- Workspace 系统 Key 继续由服务端 `kind`、`manageable`、`deletable` 限制。

## 双视口证据

- Desktop：`1280×900`
- Mobile：`390×844`
- 截图保存在仓库外：
  - `/tmp/ux03d-c-final-desktop.png`
  - `/tmp/ux03d-c-final-mobile.png`
  - `/tmp/ux03d-more-desktop-2.png`
  - `/tmp/ux03d-more-mobile-2.png`
  - `/tmp/ux03d-filter.png`

SHA-256：

| 文件 | SHA-256 |
| --- | --- |
| `ux03d-c-final-desktop.png` | `e0c16a2d754e9842f5ff04e05cfd0c5dc0d54392e44ca83b539018c275793e55` |
| `ux03d-c-final-mobile.png` | `d240d03d1bf166db643d6584a95653de4067ba65b48d79813b9ab6c690e864c4` |
| `ux03d-more-desktop-2.png` | `e063d4a53ea69c5b4d87d17571edf338f04efa883ab4368b1b380d238d34f8f8` |
| `ux03d-more-mobile-2.png` | `f252b82c3eafa7adc8a6087654e842a0ec0fd15a5adc74bbb0d907bb99cf4eca` |
| `ux03d-filter.png` | `1f373a55ed94692196e4320445d54c835f3be57e52bd87b95bb03c23179763b2` |

运行时确认：

- 移动端第一条 Key 卡片 `y=305`，底部 `y≈694`，名称和状态均在首屏。
- 移动端筛选折叠入口 `y≈755`，默认内容不展开。
- 两个视口 `scrollWidth === clientWidth`，无横向溢出。
- “更多操作”菜单在桌面和移动端均可见，移动端菜单宽度 `324px`，未超出卡片。
- 无 page error、应用 Console error 或外部请求。

## 验证命令

以下命令均通过：

```text
node --test tests/ui/customer-console-task-experience-browser.test.ts
npm run typecheck
npm run lint
npm run build
npm run verify:local
```

`npm run verify:local` 通过产品边界、193 个 Node source tests、Billing、Gateway usage、所有 Console owner-read、Announcement、Customer experience、Operator、Workspace browser suites、TypeScript、Vite build、Go 编译/数据库无关测试和 Git whitespace gate。

## 保留的业务与权威边界

- 所有浏览器请求仍通过同源 Control Plane API adapter。
- Sub2API 仍是 Gateway、Wallet、Key、Usage 的内部权威。
- 没有新增 API、DTO、状态来源、持久化、权限或后端路由。
- Reveal 仍为显式 `POST /api/gateway/keys/{keyId}/reveal`，页面内存保存，60 秒自动隐藏。
- 技术详情和敏感值仍默认关闭/隐藏。
- 创建、编辑、启停、重置、删除仍保留 CSRF、Idempotency-Key、回读确认和删除确认。

## 交付状态

本记录证明 UX-03D-C 已在本地 fake-only 运行时实现并验证。它不代表 PR、主干合并、部署、Candidate、Instance 资格或生产可用性。
