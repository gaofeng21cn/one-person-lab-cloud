# Console Gateway Usage 页面现状审计 UX-03F-A

Date: 2026-09-04

State: completed local audit record

## 审计目标与边界

UX-03F 进入 `OPL Gateway -> 用量` 页面切片。UX-03F-A 先冻结当前已经
跑通的读取链、字段语义、状态所有者和可呈现边界，再决定视觉层级；它不以补满
页面或增加图表为目标。

```text
选择 API 密钥和统计周期
-> 查看该 Key 的请求量、Token 和实际费用
-> 必要时定位单次请求的时间、模型和性能事实
```

本阶段只做静态代码、合同、测试与 fake-only 双视口运行时审计，不修改 Console、
Control Plane、Sub2API、API、DTO、业务状态机或持久化，也不将本地证据升级为
部署或生产证据。

## Reconcile

- 目标：确定 Gateway Usage 页面可以基于哪些权威事实优化 UI/UX；
- 主模块 owner：`apps/console-ui` 的 Gateway Usage Presentation；
- 当前实现事实 owner：源码、`docs/implementation-architecture.md` 和 focused tests；
- 真实调用方：`CustomerPages.UsagePage` 通过 `GatewayUsageController` 调用同源
  Control Plane 产品 API；
- 精确写集：本审计记录和 `docs/history/README.md` 历史索引；
- 完成证据：输入读取顺序、完整字段字典、展示分层、接口缺口、DDD owner 和
  不应开发项均有当前源码或测试依据。

## 运行时与证据

- 分支：`codex/workspace-task-experience`
- HEAD：`616b595b fix(console): finalize workspace ui experience`
- Runtime：仓库自有 fake-only、in-memory Console demo，
  `tools/start-console-demo.ts`
- URL：`http://127.0.0.1:5198/console/api/usage`
- 凭据：仅使用仓库 customer fixture，不触达外部网络、真实费用或真实资源
- 视口：桌面 `1280x900`，移动 `390x844`
- 截图：`/tmp/one-person-lab-cloud-ux03f-audit-2026-09-04/`
  - `desktop-viewport.png`、`desktop-annotated.png`
  - `mobile-viewport.png`、`mobile-bottom.png`、`mobile-annotated.png`

Selected PNG SHA-256 values:

| Evidence | SHA-256 |
| --- | --- |
| `desktop-viewport.png` | `9422b2713e35e05fea87f66ade985a73738b416586cee8c1c419d89972ac734e` |
| `desktop-annotated.png` | `6dac2faa593b84a1502bf07f49ee0d139542b50973b1524b24cc1132440a2baf` |
| `mobile-viewport.png` | `5ae22e95d6437276ce80bbef0f60c8a5dd3222cf7e8ae14390318504977ae457` |
| `mobile-bottom.png` | `055d926940abd6f14802d6cd6db50336b8aeafaa9694301f965c017504929d0d` |
| `mobile-annotated.png` | `75d0d32a50fe191dccb3ad1bb7cb8919264559b85ce875b1c3b6f17004700687` |

本次 fake-only 运行时观察到的请求顺序为两次 Session read、Key 第一页、选中
Key 的 Usage 与 Summary 并行读取：

```text
GET /api/auth/me
GET /api/auth/me
GET /api/gateway/keys?page=1&pageSize=20
GET /api/gateway/keys/11/usage?page=1&pageSize=20&period=month
GET /api/gateway/keys/11/usage-summary?period=month
```

切换到“今日”后只重新读取同一 Key 的 `usage` 和 `usage-summary`，两条请求均带
`period=today`。本次观察未发现外部请求或产品 API 失败。Console 中仅有本地
Vite HMR WebSocket 握手失败，不属于 Gateway Usage 产品链路。该结果是可重放的
本地运行时证据，不是未来运行或生产环境的长期保证。

捕获方法：在同一 `browse` 会话完成 fixture 登录后执行 `network --clear`，进入
上述 Usage URL，等待 `networkidle`，再读取 `network`、`console --errors` 并在
两个视口截图。

Focused browser tests：

```text
npm run test:browser:gateway-usage
6 tests passed, 0 failed
```

## 当前页面区域与可达性

### 桌面 `1280x900`

```text
Console Shell / OPL Gateway
-> Gateway tabs：服务信息 / 用量 / API 密钥
-> 用量记录标题 + 来源说明
-> API 密钥选择 + 今日 / 本周 / 本月
-> 请求次数 / 总 Token / 实际金额
-> 请求明细表：模型与路径 / Token / 费用 / 延迟 / 时间 / 请求 ID
```

主要业务结果位于首屏。面板从 `x=272` 到 `x=1248`，宽 `976px`；明细滚动容器
可用宽度为 `974px`，内部表格固定最小宽度 `1040px`。因此页面本身没有横向
溢出，但表格必须内部横向滚动。两个请求 ID 复制按钮初始 `x=1281`，均在
`1280px` 视口外，右侧技术定位动作不是直接可见。

### 移动 `390x844`

移动端改用请求卡片，没有横向溢出；文档宽度与视口均为 `390px`。三个汇总值
纵向堆叠，第一条记录从 `y=576.5` 开始，高 `328.5px`；第二条记录高
`283.5px`，页面总高 `1302px`。首屏只能看到第一条记录的一部分；缓存 Token
增加了默认卡片高度，两种延迟、时间和请求 ID 需要继续滚动。

滚到底部时最后一条记录底部位于视口 `y=730.5`，底部导航顶部位于
`y=779`，没有遮挡最后一条记录。移动端正常态没有全局“刷新”入口；失败态仍
可通过各自的“重试”动作恢复。

## 当前输入与读取顺序

页面路由为 `/console/api/usage`，读取链为：

```text
CustomerPages.UsagePage
-> useGatewayUsageController
-> console-read-api
-> Control Plane Gateway routes/service
-> Sub2API Usage client
```

首次进入页面时：

```text
GET /api/gateway/keys?page=1&pageSize=20
-> 保留仍在响应第一页中的 selectedKeyId，否则选择第一页第一条 Key
-> 并行读取：
   GET /api/gateway/keys/{keyId}/usage?page=1&pageSize=20&period=month
   GET /api/gateway/keys/{keyId}/usage-summary?period=month
```

交互后的读取规则：

| 交互 | 保留 | 重置 | 读取 |
| --- | --- | --- | --- |
| 切换 Key | 当前周期 | 明细页码回到 1 | 新 Key 的明细和汇总并行读取 |
| 切换周期 | 当前 Key | 明细页码回到 1 | 新周期的明细和汇总并行读取 |
| 明细翻页 | 当前 Key、周期 | 无 | 新页明细和同周期汇总并行读取 |
| 刷新 | 若所选 Key 仍在新返回的第一页则保留选择 | 明细页码回到 1 | 先重读 Key 集合，再读明细和汇总 |
| Key 集合权威为空 | 无 | Key、页码、明细、汇总 | 不继续读取用量 |

Usage 和 Summary 使用两个独立远程状态。任一请求失败不会隐藏另一方已经成功的
结果。路由、Session、Key、周期或 freshness generation 变化后，旧响应不能提交
到当前页面。

Control Plane 只接受 `today`、`week`、`month`。Sub2API client 按
`Asia/Shanghai` 计算日期范围：`today` 为当天，`week` 为本周一至当天，
`month` 为本月 1 日至当天；明细按 `created_at desc` 查询。

## 完整字段与业务含义

### Key 选择输入

Gateway Key DTO 包含更多管理字段，但本页选择器实际只消费：

| 字段 | 业务含义 | 当前用途 |
| --- | --- | --- |
| `id` | Sub2API Key 的权威查询身份 | 构造 Key-scoped Usage 路径，不直接展示 |
| `name` | 客户为 Key 设置的可见名称 | 选择器标签，帮助确认当前统计对象 |

### 请求明细

| 字段 | 业务含义 | 当前页面 |
| --- | --- | --- |
| `apiKeyId` | 产生请求的 Key 身份 | 未展示；Control Plane 已校验行身份与查询 Key 一致 |
| `requestId` | 单次 Gateway 请求的追踪身份 | 展示并允许复制 |
| `createdAt` | 请求发生时间 | 展示 |
| `model` | 本次请求使用的模型 | 展示 |
| `inboundEndpoint` | Gateway 接收请求的 API 路径 | 展示，空值显示 `-` |
| `requestType` | 上游记录的请求类型 | 未展示 |
| `inputTokens` | 输入 Token 数 | 展示 |
| `outputTokens` | 输出 Token 数 | 展示 |
| `cacheCreationTokens` | 写入缓存的 Token 数 | 大于 0 时展示 |
| `cacheReadTokens` | 从缓存读取的 Token 数 | 大于 0 时展示 |
| `actualCostUsdMicros` | 本次请求的实际美元成本，精度为 1/1,000,000 USD | 转换成美元展示 |
| `durationMs` | 请求总耗时，单位毫秒，可为空 | 展示 |
| `firstTokenMs` | 首个 Token 延迟，单位毫秒，可为空 | 展示 |

当前客户 DTO 明确禁止暴露 `prompt` 和 `response`。Sub2API client 还校验
`user_id`、`api_key_id`、分页、时间、模型、请求类型、Token、费用和非负延迟，
再投影为上述 13 个 camelCase 字段。

### 明细分页

| 字段 | 业务含义 | 当前页面 |
| --- | --- | --- |
| `items` | 当前页请求记录 | 展示 |
| `total` | 当前 Key 和周期内的明细总数 | 未直接展示 |
| `page` | 权威当前页 | 驱动分页 |
| `pageSize` | 权威每页数量 | 未直接展示，当前请求固定为 20 |
| `pages` | 权威总页数 | 驱动分页 |

### 周期汇总

| 字段 | 业务含义 | 当前页面 |
| --- | --- | --- |
| `totalRequests` | 当前 Key、当前周期的请求总数 | 展示 |
| `totalInputTokens` | 当前范围的输入 Token 总数 | 未展示 |
| `totalOutputTokens` | 当前范围的输出 Token 总数 | 未展示 |
| `totalTokens` | 上游权威总 Token | 展示 |
| `totalActualCostUsdMicros` | 当前范围的实际美元成本 | 转换成美元展示 |

### 来源 Envelope

| 字段 | 业务含义 | 当前页面 |
| --- | --- | --- |
| `source` | 数据来源 owner；当前为 `sub2api` | 默认层不展示 |
| `status` | `available`、`empty` 或 `unavailable` | 驱动正常、空和不可用状态 |
| `available` | 当前投影是否可用 | 驱动状态判断 |
| `fetchedAt` | Control Plane 投影读取时间 | 默认层不展示 |
| `sourceUpdatedAt` | 可选的来源更新时间 | 当前 Usage 写入未提供 |
| `reasonCode` | 来源不可用时的机器原因 | 仅供错误/诊断语义 |
| `data` | 可用时的 typed payload | 页面实际业务数据 |

所有 Source Envelope 响应统一设置 `Cache-Control: private, no-store`。

## 候选客户层与待决技术披露

业务结果要求页面回答“哪个 Key、哪个周期、用了多少、花了多少、发生了哪些
请求”。UX-03F-B 可从以下候选默认层开始比较，而不是由本审计直接批准层级：

1. 当前 Key 名称和统计周期；
2. 总请求数、总 Token、实际费用；
3. 请求模型、发生时间、输入/输出 Token 和实际费用；
4. 首字延迟、总耗时作为次级性能事实，而不是健康结论。

候选的按需披露或弱化信息：

- `requestId` 与复制动作；
- 内部查询身份 `apiKeyId`；
- 请求类型 `requestType`；
- 客户实际调用的 API 路径 `inboundEndpoint`；
- 缓存创建/读取 Token；
- `source`、`fetchedAt`、`reasonCode`；
- 原始空值和分页元数据。

`inboundEndpoint` 对排查不同 API 路径有价值，但不是每次查看费用所必需；
UX-03F-B 应结合双视口运行时密度决定是保留为明细次要信息，还是放入单条请求
的技术详情。`requestId` 作为定位字段必须保持可达，具体视觉权重由 UX-03F-B
决定。

## 当前状态矩阵与验证覆盖

| 状态 | 当前呈现或结算 | 证据 | UX-03F 后续约束 |
| --- | --- | --- | --- |
| 首次读取 | 无旧值时显示“正在读取” | `SourceState` 源码 | 保留，不制造骨架屏需求 |
| Key 可用 | 保留第一页中当前已选 Key，否则选择第一页第一条 | live + controller | 必须明确当前统计对象 |
| Key 权威为空 | 显示“暂无 API 密钥”，清空选择、Summary 和明细 | browser test | 保留清空语义 |
| Key 不可用 | 外层显示“API 密钥暂不可用”及重试 | 源码，缺 browser test | 不得让旧选择看起来仍有效 |
| Summary 与明细成功 | 两者分别显示 | live | 保留独立状态 owner |
| Summary 失败 | Summary 告警，明细继续显示 | browser test | 不得隐藏成功明细 |
| 明细失败 | 明细告警，Summary 继续显示 | browser test | 不得隐藏成功汇总 |
| 明细为空 | 应显示“暂无请求记录”，Summary 可保持为零值 | 源码，缺 browser test | 补浏览器验收 |
| 延迟为空 | `firstTokenMs`、`durationMs` 显示 `-` | live + harness | 不得伪装成 `0 ms` |
| 切 Key/周期 | 最终只提交当前 scope 的响应 | freshness tests | 还需修复加载过渡态归属 |
| 明细翻页 | 保留 Key/周期，请求目标页 | controller，缺端到端翻页 test | 补页码与内容一致性验收 |

现有六条 focused browser tests 覆盖 Key/周期/路由迟到响应、Key empty，以及
Summary/明细两种部分失败。尚未覆盖 Usage empty、Key unavailable、Session
replacement、双源同时失败后的重试恢复和真实 `page 1 -> page 2`。

## 当前接口与查询能力缺口

以下缺口分为“合同已有但 UI 未展示”和“合同本身没有”，两者不能混淆。

### DTO 已有，页面尚未使用

- 汇总的输入 Token、输出 Token；
- 明细的 `requestType`、`apiKeyId`；
- 明细总数 `total` 和 `pageSize`；
- Envelope 的 `source`、`fetchedAt`、`reasonCode`。

这些字段不因存在就必须展示。只有能降低当前任务判断成本的字段才进入默认层，
其余继续留在技术层或不渲染。

### 当前能力确实缺失

#### P1：超过 20 个 Key 时，后续 Key 在 Usage 页面不可选择

Control Plane Key API 和 Console adapter 已支持分页查询，但 Usage Controller
固定只读 `page=1&pageSize=20`，页面没有 Key 翻页或搜索。只要账户 Key 总数超过
20，后续 Key 的用量就不可达。这是现有前端查询能力缺口，不自动要求新增后端。

#### P2：响应未返回权威统计窗口

服务端实际按 `Asia/Shanghai` 计算 `startDate` 和 `endDate`，但客户 DTO 没有
`periodStart`、`periodEnd`、`timezone`。浏览器只能显示“今日/本周/本月”，不能
精确说明统计边界。

#### P2：不能支持成功率和失败请求筛选

请求明细没有成功/失败状态、HTTP 状态码或客户安全错误分类。因此当前不能设计
“成功率”“失败请求”或错误分布；这不是通过颜色或前端推断可以补出的事实。

#### P2：没有权威的单条总 Token

明细只有输入、输出和两类缓存 Token，没有 per-record `totalTokens`。若产品需要
展示单条总 Token，应先由数据 owner 明确计算语义并提供合同字段，浏览器不得
自行假定求和公式。

#### P3：没有图表所需的聚合维度

当前没有时间桶趋势、延迟分位数、按模型聚合、缓存聚合等权威事实，不能制作
趋势、P95、模型占比或缓存效率图。先证明客户决策需要，再由 owner 提供聚合，
不能在浏览器跨页或跨 Key 聚合。

`actualCostUsdMicros` 的字段名已经固定费用币种为 USD，当前美元格式化无需额外
`currency` 字段；若未来支持多币种，必须由合同演进，不能从环境猜测。

## 页面发现与优先级

### P0

未发现 P0。对于第一页内的 Key，客户可以选择周期、看到请求数、Token、实际
费用和请求明细；双视口没有页面级横向溢出，移动底部导航也没有遮挡最终记录。

### P1：第 21 个及之后的 Key 无法进入 Usage 查询

Usage Controller 固定读取 Key 第一页 20 条。后端和 Console adapter 已支持 Key
分页、搜索、状态筛选与排序，但本页没有暴露这些能力。超过 20 个 Key 后，合法
Key 会被静默隐藏，客户无法查询其用量。这是 Console 查询交互缺口，无需新增
后端接口。

### P1：切换 Key 或周期期间会把旧结果短暂归到新范围

Controller 先提交新的 Key/周期，再把 Summary 和 Usage 标为 loading；
`SourceState` 在已有 source 时继续渲染旧值，直到两条新请求完成。网络较慢时，
选择器已经显示新范围，数字和明细仍属于旧范围。现有 freshness 测试只证明
迟到响应最终不会覆盖，没有约束这一加载过渡态。

### P2：桌面默认宽度隐藏最右侧定位动作

在 `1280px` 视口，`1040px` 固定最小宽度表格装入 `974px` 滚动容器。请求 ID
文字仍部分可见，但复制按钮位于视口外。技术定位能力仍可通过横向滚动到达，
所以不是 P0；UX-03F-B 应比较信息分层、列组合等方案，不在审计阶段指定实现。

### P2：默认明细层技术信息过重，移动扫描成本高

模型、API 地址、缓存 Token、两种延迟、请求 ID 与费用同时展开。移动端第一条
记录高 `328.5px`，首屏只能看到一部分。UX-03F-B 需要比较哪些信息留在默认层、
哪些按需披露；候选默认事实是模型、时间、输入/输出 Token 和费用，候选次级
事实是 Endpoint、缓存、延迟和 Request ID。Request ID 的定位与复制能力不得
因降权而消失。

### P2：Key 上下文不足且移动正常态没有刷新入口

选择器只显示 Key 名称，不显示现成 DTO 中的 `status` 和 `kind`；重名或停用 Key
难以区分。桌面 Shell 有全局刷新，移动 Shell 没有；移动客户只能切换范围、重进
页面，或在失败态使用局部重试。后续只需调整 Console 呈现和读取交互。

### P2：页面不能精确说明统计窗口，也不能宣称请求成功

DTO 没有统计起止日期、时区、请求成功/失败、HTTP 状态码或安全错误分类。
页面只能准确表达“产生了计量使用、用了多少、实际花了多少”，不能把每条记录
命名为“成功请求”，也不能设计成功率或失败筛选。

### P3：标签和来源说明增加噪声

“汇总请求次数 / 汇总总 Token / 汇总实际金额”重复“汇总”，标题右侧“请求级
事实来自 API 服务”解释了技术来源但没有帮助客户做当前判断。UX-03F-B 可比较
更短的结果标签，以及仅在不可用或按需披露时呈现来源的方案。

### P3：明细翻页重复读取不随页码变化的 Summary

`changePage` 复用 `loadUsage`，每次同时重新读取明细和同周期 Summary。当前没有
观测到失败或性能问题，不应在审计阶段重构；UX-03F-C 若实现真实分页，可把它
纳入同一 controller 边界验证，避免额外创建请求 owner。

## DDD 与服务 Owner 边界

| Owner | 当前职责 | 不得越界 |
| --- | --- | --- |
| `UsagePage` | 呈现 controller state、选择器、周期、汇总、明细和分页 | 不发明聚合、状态或费用事实 |
| `useGatewayUsageController` | Key 集合、当前 Key、周期/页码、独立远程状态、freshness 和重读顺序 | 不拥有 Gateway/Wallet/账单 Domain |
| `console-read-api` | 同源 Control Plane 路径与 typed DTO adapter | 不直连 Sub2API |
| Control Plane | Session、账户到 Sub2API 用户映射、Key 所有权、参数校验和客户 DTO 投影 | 不拥有 Wallet、Key 或 Usage 原始事实 |
| Sub2API | Key、Gateway 路由、请求用量和实际费用权威 | 不拥有 Console 展示层 |
| Ledger | Receipt、证据、保留与对账 | 不拥有 Gateway Usage 或 Console 呈现 |
| Console Billing Presentation | 订阅、续费和账单客户视图 | 不并入 Gateway Usage Controller |

路径中的 Key ID 和当前 Session 映射共同限定读取身份。Control Plane 在读取 Usage
前验证该 Key 属于当前用户，并忽略客户伪造的 `user_id`、`api_key_id` 查询参数；
页面不得绕过这一边界直接调用 Sub2API。`Sub2API` 只作为内部权威 owner 名称
出现在架构和诊断证据中，客户页面继续统一呈现 `OPL Gateway`。

## 不应开发项

- 浏览器端趋势、成功率、P95、平均延迟、成本预测或“健康度”；
- 浏览器端跨页、跨 Key 聚合或自行推导单条总 Token；
- 第二个 Usage Controller、钱包、Gateway 或费用 owner；
- Console 直接调用 Sub2API；
- 将 Usage 与 Billing/Receipt 合并成一个状态机或页面 owner；
- 展示 prompt、response、用户邮箱、密钥值、IP 或 User-Agent；
- 没有权威字段时猜测请求成功状态、统计窗口或费用口径；
- 为填充页面增加图表、节省金额、推荐或其他无当前业务需求的装饰信息；
- 在本审计中修改 API、DTO 或后端来提前实现尚未批准的缺口。

## UX-03F-B 入口

下一步只把本审计转换成可实现的双视口交互层级，并以真实 fake-only 页面验证
当前密度和可达性：

```text
页面身份
-> Key 与周期上下文
-> 汇总结果
-> 请求明细
-> 单条技术定位信息
-> 分页与来源诊断
```

UX-03F-B 必须明确超过 20 个 Key 的可达方案、切换范围时的加载归属，并决定
`inboundEndpoint`、`requestId`、缓存 Token 和延迟在桌面/移动端的层级。除非
该设计批准接口缺口，后续实现只允许修改 `apps/console-ui` 内 Gateway Usage 的
Controller、页面呈现、样式和 focused browser tests，不修改 API DTO、Control
Plane、Sub2API 或持久化。

## 本阶段交付

- 输入、请求与状态结算顺序；
- fake-only 桌面/移动运行时截图、布局坐标和请求证据；
- Key、明细、分页、汇总和 Envelope 的完整字段字典；
- 默认客户层、技术披露层和状态矩阵；
- 已有未展示字段与真实接口/能力缺口的区分；
- P0/P1/P2/P3 页面发现与 focused test 缺口；
- DDD 和服务 owner 边界；
- 明确的不应开发项及 UX-03F-B 入口。

本记录不代表交互设计已批准、实现已完成、PR、主干合并、部署、Candidate、
Instance 资格或生产可用性。
