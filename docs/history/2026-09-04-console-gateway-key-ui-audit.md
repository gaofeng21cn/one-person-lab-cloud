# Console Gateway Key 页面现状审计 UX-03D-A

Date: 2026-09-04

State: completed local audit record

## 审计目标与边界

本审计只检查客户 Console 的 `OPL Gateway → API 密钥` 页面当前呈现、可达性、请求和状态映射，为后续交互层级定义提供事实基线。业务链路已经跑通，本阶段不改变 Gateway、API Key、Wallet、Usage、权限、Reveal 生命周期或任何后端合同。

```text
OPL Gateway → API 密钥
读取 Endpoint / Groups / Keys → 查看与管理客户 Key
```

保留的业务能力：显示/隐藏 API 密钥、使用说明、编辑、启用/停用、重置配额用量、重置消费限额用量、删除、创建、搜索、筛选、排序、分页。敏感值默认隐藏，Reveal 仍由当前页面内存承载并在 60 秒后自动隐藏。

## 运行时与证据

- 分支：`codex/workspace-task-experience`
- HEAD：`b3e04e27 docs(console): record customer visual verification`
- Runtime：仓库自有 fake-only in-memory Console demo，`tools/start-console-demo.ts`
- URL：`http://127.0.0.1:5197/console/api/keys`
- 凭据：仅使用仓库 fixture，不触达外部网络、真实账单或真实资源
- 视口：桌面 `1280×900`，移动 `390×844`
- 截图与 JSON：`/tmp/one-person-lab-cloud-ux03d-audit-2026-09-04/`
  - `desktop-viewport.png`、`desktop-full.png`
  - `mobile-viewport.png`、`mobile-full.png`
  - `desktop-use-dialog.png`
  - `audit.json`

Selected PNG SHA-256 values:

| Evidence | SHA-256 |
| --- | --- |
| `desktop-viewport.png` | `b4a48a1716fa4a18c68e13f24fb2cd819410089e09c0d1471fd238e849dd9ef2` |
| `mobile-viewport.png` | `c1490c095223aebcd114ae1369ffd340f0d4a4fea0211be024e21aeb9d5a2ee2` |
| `mobile-full.png` | `91d5ac24e570d709eb4befc9a6e54ec25c83b4f6c4ee8b9b2b07ae31f9585519` |
| `desktop-use-dialog.png` | `8514f03b15c6017aed91dbff1146d84a6c3b1c75d090bfe23588077d408b2475` |

浏览器记录：两个视口均无横向溢出、无 page error、无应用 Console error、无外部请求。页面请求全部为同源 Control Plane API。

## 当前页面区域映射

### 桌面首屏

```text
Console Shell / OPL Gateway
→ Gateway tabs：服务信息 / 用量 / API 密钥
→ API 密钥标题 + API 地址 + 复制
→ 刷新 + 创建 API 密钥
→ 搜索、状态筛选、排序、顺序、每页、技术筛选、查询
→ Key 表格：名称 / 状态 / 总额度与已用 / 有效期 / 最近使用 / 操作
→ 分页
```

首屏关键位置（浏览器布局坐标）：标题 y=92，Endpoint y=210，筛选 y≈297–335，第一条 Key 容器 y=398–496。页面高度 900，无纵向滚动。

### 移动首屏

```text
顶部 Shell + Gateway tabs
→ API 密钥标题 + API 地址
→ 刷新 + 创建 API 密钥
→ 搜索
→ 状态筛选
→ 排序
→ 顺序
→ 每页
→ 技术筛选
→ 查询
→ 第一条 Key 卡片
```

第一条 Key 卡片从 y≈784 开始，首屏底部 844 只露出标题的一小部分。Key 卡片完整高度约 377px，底部操作栏约 y=1113，必须滚动后才能看到。页面总高 1319px。

## 当前操作清单与可达性

当前一般 Key 的 7 个行内操作均存在且可用：

1. 显示 API 密钥
2. 使用说明
3. 编辑
4. 停用（停用后标签切换为启用）
5. 重置配额用量
6. 重置消费限额用量
7. 删除

桌面端 7 个操作在同一行以图标按钮呈现，均有 ARIA 名称。移动端同一组图标位于卡片底部，所有操作需要先滚动到卡片底部。Tooltip 是主要的视觉解释，图标本身没有可见文字分组。

创建入口在标题区域，刷新为图标按钮。查询按钮在筛选区域。分页按钮有上一页、下一页的 ARIA 名称。

技术详情默认关闭，技术筛选默认关闭。技术详情字段包含 `key ID`、`kind`、`status`、`group ID`、平台、倍率、并发、限额/用量、最近使用 IP、`createdAt` 等原始技术字段；它不是默认客户信息层。

敏感值初始不渲染。点击“显示 API 密钥”后：

- 发送 `POST /api/gateway/keys/11/reveal`；
- 在页面显示完整值、复制和隐藏；
- 文案说明“仅保存在当前页面内存，60 秒后自动隐藏”；
- 使用说明对话框复用已 reveal 值，不重复请求。

## 请求、状态与权威所有者

页面初次读取的同源请求：

```text
GET /api/auth/me                 Console Session 身份
GET /api/gateway/endpoint        Gateway Endpoint
GET /api/gateway/groups          Gateway 服务分组
GET /api/gateway/keys?page=1&pageSize=20&sortBy=createdAt&sortOrder=desc
```

Shell 还会并行读取当前客户概览所需的 Workspace、Wallet、Usage、Billing receipts、Announcements。进入 Key 页面后未发现外部网络请求。

写入和读取边界：

- `apps/console-ui/src/components/keys/KeysPanel.tsx`：当前页面展示、局部查询状态、对话框、Reveal 60 秒计时、调用编排。
- `apps/console-ui/src/api/console-read-api.ts`：浏览器 API adapter，所有路径均为同源 `/api/...`。
- `services/control-plane/internal/server/routes_gateway.go`：唯一浏览器-facing Gateway 路由。
- `Control Plane`：Session/CSRF、客户 DTO、请求边界和回读确认。
- `Sub2API`：Gateway、Wallet、Key、Usage 的内部权威；Control Plane 通过 typed client 读取/写入，页面不直接调用。
- `manageable` / `deletable`：由服务端根据保留的 Workspace 系统 Key 决定，`kind === "workspace"` 时为不可编辑、不可删除；页面只消费 DTO，不创建第二个状态来源。

## 发现与优先级

### P1：移动端核心对象被筛选器推离首屏

移动端首屏高度中，标题、Endpoint、刷新/创建和 5 个筛选控件先后占据约 780px；第一条 Key 仅从 y≈784 开始，七个关键操作直到 y≈1113 才可见。用户进入页面的第一任务是查看、复制或管理现有 Key，但当前第一屏优先展示低频筛选配置。

**影响**：查看 Key、判断状态、进入使用说明、Reveal 的发现成本高，移动端核心任务需要额外滚动。

### P1：7 个图标操作没有可见的优先级和分组

显示/隐藏、使用说明、编辑、启停、两种重置和删除在桌面同一操作栏、移动同一底栏并列。虽然 ARIA 名称和 Tooltip 使其可访问，但视觉上无法区分“查看/使用”“日常管理”“高风险操作”。停用、重置、删除的后果不同，却承担相同视觉权重。

**影响**：误操作风险和发现成本增加，移动端尤其明显。

### P2：筛选区域的默认密度高于客户当前任务

搜索、状态、排序、顺序、每页、技术筛选、查询全部直接出现。服务分组筛选已被折叠，但 5 个控件仍在移动端垂直堆叠。桌面没有空间问题，但层级仍把列表结果压在筛选之后。

### P2：Endpoint 与 Key 结果在信息层级上并列

Endpoint 是调用前置事实，Key 列表是当前页面主任务。当前 Endpoint 区域与刷新/创建入口共同占据标题区，导致“查看已有 Key”与“创建新 Key”没有明确主次。该结论只涉及呈现层级，不要求移除 Endpoint 或创建能力。

### P2：技术字段可以继续留在显式披露层

技术详情默认关闭，边界正确；但展开后字段使用 `key ID`、`kind`、`group ID`、`rate multiplier` 等中英混杂术语。它们服务诊断和配置，不应上移到默认客户层。后续若优化，只做可读分组或中文解释，不改变字段和请求合同。

### P3：刷新与查询的语义区别不够显眼

刷新是重新读取 Endpoint、Groups、Keys，查询是按当前筛选条件读取 Keys。两者均以动作按钮出现，当前没有额外的结果摘要说明。该问题不阻塞任务，后续交互定义时再决定是否强化。

## 结论：不改业务的交互层级入口

本审计不直接实现改动。基于运行时证据，下一步应只定义页面交互层级：

```text
L0 页面身份：OPL Gateway / API 密钥
L1 主任务：看到现有 Key，查看状态、额度和最近使用
L1 主动作：创建 API 密钥
L2 常用 Key 动作：显示/隐藏、使用说明、编辑、启用/停用
L3 维护动作：重置配额用量、重置消费限额用量
L4 高风险动作：删除
L5 辅助/技术层：搜索、筛选、排序、分页、技术详情、Endpoint 复制、刷新
```

这只是下一阶段的呈现优先级输入。所有动作仍通过现有 Control Plane API，权限和敏感值策略不变。

## 本阶段交付

- 运行时桌面/移动截图
- 页面区域与首屏位置映射
- 操作和可达性清单
- 请求、状态、权威所有者映射
- P1/P2/P3 问题及不改业务边界结论

本记录不代表 PR、主干合并、部署、Candidate 或生产资格。
