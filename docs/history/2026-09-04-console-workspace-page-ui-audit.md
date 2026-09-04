# Console Workspace 页面现状审计 UX-03E-A

Date: 2026-09-04

State: completed local audit record

## 审计目标与边界

本审计检查客户 Console 的 Workspace 列表、Workspace 开通和 Workspace 详情三个当前页面，确认真实运行时的页面结构、业务动作、信息层级以及桌面/移动差异。业务已经跑通，本阶段不改变 Workspace、Fabric、Gateway、Wallet、Billing、权限、Secret 生命周期或任何后端合同。

当前审计只为下一步交互层级定义提供事实，不授权页面重构。

## 运行时与证据

- 分支：`codex/workspace-task-experience`
- HEAD：`4c3ab6ab docs(console): clarify gateway key delete grouping`
- Runtime：仓库自有 fake-only in-memory Console demo，`tools/start-console-demo.ts`
- 页面：
  - `http://127.0.0.1:<port>/console/workspaces`
  - `http://127.0.0.1:<port>/console/workspaces/ws-1`
- 凭据：仅使用仓库 fixture，不触达外部网络、真实账单或真实资源
- 视口：桌面 `1280×900`，移动 `390×844`
- 截图：
  - `/tmp/ux03e-workspace-list-desktop.png`
  - `/tmp/ux03e-workspace-list-mobile.png`
  - `/tmp/ux03e-workspace-detail-desktop.png`
  - `/tmp/ux03e-workspace-detail-mobile.png`

SHA-256：

| 文件 | SHA-256 |
| --- | --- |
| `ux03e-workspace-list-desktop.png` | `10a2e9ad752314e0cc9f6f185bf188808c82f35c756d9871a06d742721f8e250` |
| `ux03e-workspace-list-mobile.png` | `6911accacbfac10e1ffcd6459ef457f9310f143026fa8889ae8ad900eb37bcc2` |
| `ux03e-workspace-detail-desktop.png` | `7384820c2c6af636eabd89e1a5f3f1ccb1e16d4a3333fe42d4bd63827e721d6a` |
| `ux03e-workspace-detail-mobile.png` | `5443ffc5e0cfa898dd2e979f4a082450fc11d3b73c41949d24624b92b7e6a073` |

## 当前页面区域映射

### Workspace 列表

```text
Console Shell
→ 页面标题：工作空间
→ 工作空间总数
→ 新建工作空间（页面主动作）
→ 工作空间列表
→ 每个 Workspace：名称、套餐/存储、生命周期状态、权益截止、进入详情
→ 分页
```

桌面运行时：

- 顶部工具栏约 `y=92–156`；
- 列表容器约 `x=272, y=178, w=976, h=196`；
- 两个 Workspace 行各约 `78px` 高，名称和状态清晰可见；
- 主动作“新建工作空间”位于右上角，未与列表结果混淆。

移动运行时：

- 工具栏约 `x=16, y=66, w=358, h=68`；
- 列表容器约 `x=16, y=150, w=358, h=328`；
- 每个 Workspace 以纵向卡片式行呈现，名称、套餐、存储、状态、权益截止和进入箭头均可见；
- `390×844` 首屏可以看到两个 Workspace 和固定底部导航；无横向溢出。

### Workspace 开通

```text
返回工作空间列表
→ 步骤：配置 → 核对 → 开通状态
→ 新建工作空间
→ 工作空间名称
→ 选择套餐
→ 计算/存储/月度总额
→ 自动续费（预付模式）
→ 订单摘要
→ 核对开通信息
→ 明确确认后提交
→ 开通结果与当前阶段
→ 刷新状态 / 查看工作空间
```

当前业务动作保持为：选择套餐、查看权威报价、确认月度总额、提交一次开通、等待权威回读、进入 Workspace。开通失败、结果未知、多个未完成操作和缺少 Workspace 身份均有单独的状态呈现，原始 operation/status/phase/checks 默认仅在“技术详情”中显示。

### Workspace 详情

```text
返回工作空间列表
→ Workspace 名称与可用性
→ 打开工作空间（主动作）/ 刷新
→ 套餐、实际月费、权益截止
→ 访问凭据：登录账号、登录密码、API 密钥、轮换密码
→ 续费与存储：续费方式/自动续费、持久存储
→ 高级设置：模型预算、删除工作空间
→ 技术详情：Runtime、Workspace、预算、删除/续费原始证据
```

桌面运行时：

- 详情首屏约从 `y=146` 开始；
- 身份卡片高度约 `212px`，主动作“打开工作空间”在右上；
- 访问凭据紧随其后，约 `282px`；
- 续费与存储约 `152px`；
- 高级设置和技术详情默认折叠，位于页面下方；
- 无横向溢出。

移动运行时：

- 身份卡片约 `y=114–570`，名称、可用性、打开和刷新动作、套餐/月费/权益截止按纵向排列；
- 访问凭据从约 `y=586` 开始，登录账号、密码、API 密钥和轮换密码纵向排列；
- 续费与存储、高级设置、技术详情继续后置；
- 固定底部导航覆盖页面底部，但页面有额外底部 padding；
- 无横向溢出。

## 当前业务动作与权威边界

### 列表

- 读取 Workspace 列表和分页；
- 进入指定 Workspace 详情；
- 进入新建 Workspace；
- 空、不可用、错误状态均保持区分。

### 开通

- 价格目录和报价来自当前 Control Plane 读取；
- 预付模式明确展示实际应付金额和确认勾选；
- 创建操作使用幂等键；
- 成功后必须经过 Workspace 权威回读和 Fabric Runtime 读取才能进入可打开状态；
- 未知结果不得重复购买；多个未完成操作会阻止继续。

### 详情

- “打开工作空间”使用 Fabric Runtime 权威 URL；
- 登录密码和 Workspace API Key 显式点击后才显示；
- Secret 只保存在当前页面内存，60 秒自动隐藏，并且两个 Secret 互斥显示；
- 轮换密码、自动续费、预算更新、预算用量重置、删除均调用已有 Controller 和 Control Plane adapter；
- Workspace 系统 Key 和权限仍由后端事实决定；
- 技术详情默认关闭。

### 权威所有者

- `apps/console-ui`：页面呈现和 Controller 调用编排；
- `services/control-plane`：Session、Workspace 编排、报价、续费、预算、删除协调和客户 DTO；
- `services/fabric`：Runtime 入口、凭据绑定和运行时事实；
- `Sub2API`：Gateway、Wallet、Key、Usage 内部权威；
- `apps/console-ui` 不拥有 Workspace 持久化、资源采购、计费或 Secret 权威。

## 发现与优先级

### P1：Workspace 详情在移动端首屏只展示身份和凭据起点，主任务仍需继续滚动

移动端身份卡片本身较高，访问凭据紧随其后；如果用户的首要目标是“打开工作空间”，按钮已在首屏可见，这是正确的结果优先。但如果目标是复制密码或 API Key，凭据操作需要继续滚动。该问题更适合通过交互层级和操作可见性优化解决，而不是压缩字体或删除信息。

### P1：Workspace 列表没有明确的“进入”语义文字

桌面依赖整行可点击和右侧箭头，移动同样依赖箭头。可访问名称存在，但视觉上没有“查看详情/进入工作空间”的文字动作提示。对首次用户而言，整行点击语义不如明确入口。

### P2：列表字段使用“生命周期状态”，客户真正关心的是当前可用结果

“运行中”是可理解的，但“生命周期状态”是字段元语言。它在每个移动卡片中重复出现，增加认知噪音。后续可考虑保留状态值、弱化字段说明或改为客户任务语言，但不能改变服务端状态映射。

### P2：详情的“高级设置”混合了预算和删除

当前高级设置内同时包含：模型预算、预算用量重置、删除工作空间。预算属于使用控制，删除属于不可逆资源操作。它们虽然都被折叠，但业务后果不同，后续应在交互层级上分组并给删除独立警示，而不是继续使用同一标题承载。

### P2：详情中“访问凭据”包含两类不同目的

登录密码用于进入 Workspace，API 密钥用于 Gateway 调用。两者都属于敏感信息，但用户目标不同。当前都放在一个凭据表内，安全边界正确，任务语义仍可进一步区分。

### P2：开通流程的订单摘要信息密度较高

开通页面同时展示计算、存储、总额、余额/权益、计费周期和续费。信息完整且来自权威报价，但移动端需要较多滚动才能完成“确认应付金额”这一核心决策。后续应优化阅读顺序，不应删除价格构成或绕过确认。

### P3：中英混杂主要集中在技术披露层

默认客户层已使用“工作空间、套餐、实际月费、权益截止、访问凭据”等中文。`Workspace ID`、`Runtime URL`、`priceVersion`、`operation ID`、`checks` 等仍在技术详情中，属于诊断证据，不应上移到客户主路径。必要时可增加中文解释，但不应伪造或重命名权威字段。

### P3：桌面端有较多留白，但不构成当前任务阻塞

列表和详情在 `1280×900` 下有明显空白区域。它保证了内容呼吸和后续扩展空间，目前没有证据表明需要通过增加装饰性模块填充。

## 结论：下一步只定义 Workspace 交互层级

基于运行时事实，下一步应定义 Workspace 的交互层级，而不是立即改代码：

```text
L0 页面身份：工作空间
L1 列表主任务：找到一个 Workspace 并进入
L1 列表主动作：新建工作空间
L1 详情主任务：确认是否可用并打开
L2 详情高频动作：查看/复制访问凭据、刷新
L3 详情管理：轮换密码、自动续费、预算设置
L4 详情维护：重置预算用量
L5 详情高风险：删除工作空间
L6 辅助/技术层：分页、报价构成、技术详情、原始状态和诊断字段
```

开通链路的层级应保持：

```text
配置 → 核对价格 → 明确确认 → 提交一次 → 权威回读 → 打开
```

所有动作继续经过现有 Controller 和 Control Plane adapter；不新增 Workspace、Runtime、Gateway、Wallet、Billing API，不改变 Fabric、Sub2API、Secret、权限或持久化边界。

## 本阶段交付

- 双视口运行时截图；
- 列表、开通、详情页面区域映射；
- 业务动作、请求和权威所有者边界；
- P1/P2/P3 体验问题及不改业务结论；
- UX-03E-B 交互层级定义的输入。

本记录不代表交互设计已批准、实现已完成、PR、主干合并、部署、Candidate 或生产资格。
