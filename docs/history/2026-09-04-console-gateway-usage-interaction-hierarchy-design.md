# Console Gateway Usage 交互层级设计 UX-03F-B

Date: 2026-09-04

State: approved local design record

## 目标与产品边界

UX-03F-B 将 UX-03F-A 的 Gateway Usage 页面审计转换为可实现的交互层级。
客户业务结果保持不变：

```text
选择 API 密钥和统计周期
-> 判断是否产生计量使用
-> 查看用了多少 Token、实际花了多少
-> 必要时定位单次请求的模型、时间、性能和 Request ID
```

本设计只作用于本仓库 `apps/console-ui` 的 Cloud Console 客户界面。Instance
采用相同产品字节后，用户会在其 `cloud.medopl.com` Console 看到该页面；域名、
部署和生产状态仍由 Instance owner 管理。当前阶段只记录本地设计，不直接修改
线上站点。

不修改：

- OPL Gateway 或 Sub2API 的服务、路由、计量和费用逻辑；
- Control Plane 产品 API、客户 DTO、Session、权限和账户映射；
- Ledger、Billing、Workspace、Wallet 或持久化；
- OPL 图标、客户导航和 Gateway 品牌。

客户界面统一使用 `OPL Gateway`。`Sub2API` 仅是 Gateway、Key、Usage 和实际
费用的内部权威实现，不在客户页面显示。

## 输入证据

- UX-03F-A：`docs/history/2026-09-04-console-gateway-usage-boundary-audit.md`
- 审计提交：`c9bc1dc3 docs(console): audit gateway usage experience`
- 运行时视口：桌面 `1280x900`，移动 `390x844`
- 业务 P1：Usage Controller 只读取前 20 个 Key，后续合法 Key 不可达
- 状态 P1：切换 Key 或周期后，旧结果会短暂显示在新范围下
- 呈现 P2：桌面表格内部横向滚动，Request ID 复制动作初始不可见
- 呈现 P2：移动端单条请求最高约 `329px`，技术字段占用默认扫描路径

## 方案比较与决定

### 方案 A：结果优先、完整 Key 选择、请求详情按需披露

页面先显示当前 Key、周期和三项结果。Key 通过可搜索、可分页的选择对话框切换；
请求列表默认只显示判断用量需要的事实，诊断字段在单条详情中展开。

优点：同时解决两个 P1，消除默认横向滚动，保留所有权威字段和定位能力；只需
使用现有 Console API。代价：需要拆分 Usage Presentation，并调整 Controller
内部的 Key 查询、范围切换和明细分页状态。

**决定：采用。** 用户已批准方案 A。

### 方案 B：在页面顶部直接展开 Key 列表和分页

优点：所有 Key 一直可见。代价：Key 多时再次把结果推离首屏，重复 Gateway Key
管理页的信息结构，不适合高频切换。拒绝。

### 方案 C：跳转到 API 密钥页选择，再通过 URL 返回 Usage

优点：复用 Key 管理页。代价：一次查询被拆成跨页任务，还要增加 URL 选择状态和
页面间耦合，不是最短链路。拒绝。

## 设计原则

1. **范围先明确**：任何数字都必须明确属于哪个 Key、哪个周期。
2. **结果先于诊断**：请求次数、Token 和实际费用先于 Endpoint、缓存、延迟和
   Request ID。
3. **旧结果不冒充新结果**：切换范围后立即停止呈现旧范围数据。
4. **完整可达**：超过 20 个 Key 时仍能通过搜索和分页选中，不增加后端接口。
5. **权威事实不推断**：不生成成功率、失败原因、统计起止日期或单条总 Token。
6. **状态独立结算**：Summary 使用 available/unavailable，零请求仍是 available
   零值；请求明细独立使用 available/empty/unavailable。
7. **技术信息不消失**：按需披露降低默认密度，但 Request ID 和性能事实仍可达。

## 总体交互层级

```text
L0 页面身份：OPL Gateway / 用量
L1 查询范围：当前 API 密钥 + 状态/类型 + 今日/本周/本月 + 刷新
L1 业务结果：请求次数 + 总 Token + 实际费用
L2 请求明细：时间 + 模型 + 输入/输出 Token + 实际费用
L3 单条技术详情：API 路径 + 缓存 Token + 延迟 + Request ID
L4 辅助状态：Key 搜索/分页 + 明细分页 + empty/unavailable/retry
```

L1 的范围和结果是同一个判断上下文，不能被拆到不同页面。L3 只改变披露方式，
不删除现有字段，也不改变请求 DTO。

## 页面结构

页面从上到下固定为：

```text
Gateway 局部导航
-> 用量标题
-> 当前统计范围
-> 三项汇总结果
-> 请求记录标题与总条数
-> 请求明细
-> 明细分页
```

- 删除“请求级事实来自 API 服务”。来源名称不帮助客户完成当前判断。
- 汇总标签改为“请求次数”“总 Token”“实际费用”，不重复“汇总”。
- “请求记录”使用权威 `total` 显示当前范围的记录数量。
- 页面不增加趋势图、比例图、健康分、预测或推荐。

## API 密钥选择

### 当前范围

页面直接显示当前 Key 的：

- 名称；
- 中文状态，例如“启用”“停用”“额度用尽”“已过期”；
- 中文类型，例如“通用”“工作空间”；
- “更换 API 密钥”按钮。

不默认显示内部 Key ID。状态和类型只映射现有 DTO，不创建新的权限或可用性判断。
停用 Key 仍可查询其历史用量，不能因为停用而从选择结果中删除。

### 选择对话框

点击“更换 API 密钥”打开同一个响应式 Modal：

```text
选择 API 密钥
-> 搜索名称
-> 搜索 / 清除
-> 当前页 Key：名称 + 状态 + 类型 + 当前选中标记
-> 共 N 个 + 上一页 / 下一页
-> 取消
```

- 搜索和 Enter 显式提交现有 `search` 查询，不按每个按键发送请求。
- 新搜索重置到 Key 第 1 页；分页保留当前搜索词。
- 每页使用现有 `pageSize=20`，通过权威 `total/page/pages` 分页。
- 选择 Key 后关闭 Modal，保留当前周期，明细回到第 1 页。
- Dialog 获取焦点，Escape 或取消后焦点返回“更换 API 密钥”。
- Key 查询失败只在 Dialog 内显示失败和重试，不抹掉当前已提交的用量结果。
- Key 搜索与分页拥有独立 generation；连续搜索、快速翻页、关闭后重开 Dialog
  时，旧查询响应不得覆盖最新搜索词和页码，也不得修改当前已选 Key 或 Usage。

该设计复用现有 `GET /api/gateway/keys` 的分页和搜索能力，不增加第二套 Key
管理页面，不在 Usage 中加入状态筛选、排序或 Key 编辑。

## 周期、刷新与范围提交

- 周期继续使用“今日 / 本周 / 本月”分段控制。
- “刷新”作为当前页面的明确动作，在桌面和移动端都可见。
- 更换 Key 或周期时，新的范围立即可见，旧 Summary 和旧明细立即停止呈现；
  两个区域分别显示“正在读取”。
- 只有 Session、路由、Key、周期和 generation 均匹配的响应才能提交。
- 快速连续切换仍允许，现有 freshness 机制负责拒绝旧响应。
- 刷新保留当前已选 Key，不因该 Key 不在选择器当前页而切回第一页第一条。
- 刷新固定通过现有 `getGatewayKey(selectedKeyId)` 做单 Key 权威 readback；
  readback 成功后再读取当前周期 Summary 和第 1 页明细，不扫描所有 Key。
- 只有 `404 gateway_key_not_found` 才表示当前 Key 已不存在或不再属于账户；此时
  清空当前范围和结果，不自动切换到第一页第一条。
- transient error、timeout 或 unavailable 只表示当前 Key 暂时无法确认；保留已选
  Key 身份，停止呈现旧结果并提供重试，不把它伪装成 Key 不存在。
- Summary 局部重试只读取当前 Key/周期的 Summary，成功明细保持可见且不进入
  loading；明细局部重试只读取当前 Key/周期/已提交页的 Usage，成功 Summary
  保持可见且不重复请求。
- 页面“刷新”才确认当前 Key，并同时重新读取当前周期 Summary 和第 1 页明细。

Key 选择列表与当前已选 Key 是两个状态：列表负责查找，已选 Key 负责 Usage
范围。二者不能因为分页而互相覆盖。

## 汇总结果

汇总只显示现有三个权威字段：

| 标签 | DTO |
| --- | --- |
| 请求次数 | `totalRequests` |
| 总 Token | `totalTokens` |
| 实际费用 | `totalActualCostUsdMicros` |

- 桌面为同一条三列结果带。
- 移动使用两列加一行：请求次数和总 Token 各占半行，实际费用独占下一行；
  完整金额不得截断，字号保持固定。
- 数字为零时显示 `0` 或 `$0.00`，不能转成“暂无数据”。
- Summary unavailable 只替换汇总区域，请求明细继续显示。

## 请求明细与技术详情

### 默认明细

桌面默认表格使用五个可在面板内完整显示的区域：

```text
时间 | 模型 | Token（输入 / 输出） | 实际费用 | 查看详情
```

移动端每条请求默认显示：

```text
模型 + 时间
输入 / 输出 Token + 实际费用
查看详情
```

默认层不显示“成功”或“失败”。DTO 没有该事实，存在 Usage 记录只表示产生了
计量记录。

### 单条技术详情

每条请求使用可聚焦的展开控制，在当前行或卡片内披露：

- `inboundEndpoint`，客户文案为“API 路径”；
- `cacheReadTokens`，客户文案为“缓存读取 Token”；
- `cacheCreationTokens`，客户文案为“缓存写入 Token”；
- `firstTokenMs`，客户文案为“首个 Token 延迟”；
- `durationMs`，客户文案为“总耗时”；
- `requestId`，客户文案为“请求 ID”，保留复制按钮。

技术详情默认关闭。缓存值为零仍可在详情中显示 `0`，避免展开后字段结构跳变；
延迟为空显示 `-`，不能显示 `0 ms`。`requestType` 和内部 `apiKeyId` 不展示，
因为当前客户任务没有使用它们。

桌面不得依赖横向滚动查看详情；移动端详情在卡片内部纵向展开，不能使用横向
滚动或嵌套卡片。展开状态只属于 Presentation，不写入 URL 或持久化。

## 明细分页

- 分页保留当前 Key 和周期。
- 翻页只重新读取目标页 Usage，不重复读取不随页码变化的 Summary。
- 当前页只在目标页成功后提交；失败时保留已提交页码，并在明细区域提供重试。
- 切 Key、切周期和刷新均回到明细第 1 页。
- `total` 可以作为“共 N 条请求记录”显示，`pageSize` 和原始分页元数据不展示。

## 状态与错误层级

| 状态 | 呈现规则 |
| --- | --- |
| 初次进入 | Key 范围读取；确定 Key 后分别读取 Summary 和明细 |
| 无 Key | 显示“暂无 API 密钥”和“前往 API 密钥”，不读取 Usage |
| Key 查询失败且无选择 | 显示 Key 不可用和重试，不呈现零值 |
| Key Dialog 查询失败 | 当前结果不变，只在 Dialog 内失败和重试 |
| 当前 Key 刷新确认失败 | 保留已选 Key，隐藏旧结果，显示“当前 API 密钥暂时无法确认”和重试 |
| 当前 Key 权威不存在 | 清空范围和结果，不自动选择其他 Key |
| 更换 Key/周期 | 新范围 + 两个独立读取状态；旧结果隐藏 |
| Summary 失败 | 汇总失败和重试；成功明细保留 |
| 明细失败 | 明细失败和重试；成功 Summary 保留 |
| 明细为空 | Summary 正常显示，明细显示“当前范围暂无请求记录” |
| 两者失败 | 两个区域分别说明不可用，共享页面刷新动作 |
| Session/路由改变 | 清空页面范围和所有异步提交资格 |

错误状态不显示内部 `sub2api` 名称、raw error、reason code 或 HTTP 状态码。诊断
证据仍留在技术边界和日志，不进入默认客户文案。

## 响应式与可访问性

### 桌面 `1280x900`

- 当前范围、周期和刷新位于同一工具区；
- 三项结果和第一条请求必须在首屏可见；
- 默认表格不产生内部或页面横向滚动；
- 查看详情和复制均有 ARIA 名称、Tooltip 和可见焦点。

### 移动 `390x844`

- Key 范围按钮、周期、刷新、三项结果和第一条请求的模型/费用在首屏可见；
- Key Modal、搜索、列表项和分页触控目标至少 `44x44px`；
- 汇总保持稳定的两列加一行结构，文字和完整金额不得溢出；
- 请求卡片默认紧凑，详情展开后仍不被固定底部导航遮挡；
- 所有内容无页面横向溢出。

## DDD 与实现边界

| Owner | UX-03F-C 职责 | 保持不变 |
| --- | --- | --- |
| Gateway Usage Presentation | 页面层级、Key Modal、明细展开、客户文案和响应式布局 | 不拥有请求或费用事实 |
| `useGatewayUsageController` | Key query、selected Key、period/page、独立远程状态和 freshness | 不拥有 Gateway、Wallet 或 Billing Domain |
| `console-read-api` | 继续提供现有 typed Control Plane adapter | 不新增路径，不直连 Sub2API |
| Control Plane | Session、Key 所有权、参数校验和客户 DTO | API/DTO 不变 |
| Sub2API | Key、Gateway Usage 和实际费用权威 | 服务和数据不变 |

Presentation 从 `CustomerPages.tsx` 中拆为 Gateway Usage 专属页面组件，仅用于
提高页面内聚度。Controller 仍位于 `app` 层；API adapter 仍位于 `api` 层；不得
创建共享业务服务、第二个 Gateway owner 或跨页面全局状态。

## UX-03F-C 最小写集

后续实现只允许触及：

- `apps/console-ui/src/components/gateway-usage/GatewayUsagePage.tsx`：专属呈现；
- `apps/console-ui/src/pages/CustomerPages.tsx`：移除内嵌 Usage JSX，保留薄路由装配；
- `apps/console-ui/src/app/use-gateway-usage-controller.ts`：Key 查询、范围加载和分页；
- `apps/console-ui/src/app/console-controller-types.ts`：必要的内部 controller 类型；
- `apps/console-ui/src/styles.css`：Gateway Usage 专属选择器和响应式布局；
- `tests/ui/gateway-usage-controller-browser.test.ts`：状态与交互验收；
- `tools/console-browser-qa.ts`：现有桌面/移动 Usage acceptance 更新；
- `docs/history/**`：实现与验证记录。

不修改 `apps/console-ui/src/api/dtos.ts`、`packages/contracts`、Control Plane、
Sub2API、Ledger、数据库或部署配置。若实现发现必须改变这些 owner，停止 UX-03F-C
并回到边界评审。

## UX-03F-C 验收条件

### 业务结果

- 当前 Key、中文状态/类型和周期始终明确；
- 请求次数、总 Token、实际费用优先呈现且来源于现有 Summary DTO；
- 默认请求明细完整显示时间、模型、输入/输出 Token 和实际费用；
- 不出现成功率、失败结论、推测时间窗或浏览器计算的单条总 Token。

### 关键交互

- 至少 21 个 Key 的 fixture 能通过搜索或分页选择第 21 个 Key；
- Key 搜索显式提交，分页保留搜索词，选择后保留周期并回到明细第 1 页；
- 连续搜索、快速切换 Key 页码、关闭后重开 Dialog 时，旧 Key 查询不得覆盖最新
  query，也不得改变当前已选 Key 和 Usage；
- 更换 Key/周期后旧结果立即隐藏，不会挂在新范围下；
- 选中第一页以外的 Key 后刷新，不会静默切回第一页第一条；
- 页面刷新先执行现有单 Key readback；`404 gateway_key_not_found` 清空范围，
  transient unavailable 保留 Key 身份并显示确认失败，两者不得互相冒充；
- 明细 `page 1 -> page 2` 只重读 Usage，失败不推进已提交页；
- Summary 重试不隐藏或重复读取成功明细，明细重试不隐藏或重复读取成功 Summary；
- Summary 的 available/unavailable 与明细的 available/empty/unavailable 独立结算，
  Summary 零值保持 available；
- Session、路由、Key、周期和分页的迟到响应均不能覆盖当前结果。

### 双视口

- 桌面 `1280x900` 和移动 `390x844` 无页面或明细横向溢出；
- 移动首屏可识别当前范围、三项结果和第一条请求；
- Key Modal 和详情展开支持键盘、Escape、焦点返回和 `44x44px` 触控目标；
- Request ID 复制保持可达，空延迟继续显示 `-`；
- 无 page error、应用 Console error 和外部请求。

### 工程验证

- focused browser tests 先覆盖上述关键交互和状态矩阵；
- `npm run test:browser:gateway-usage`；
- `npm run typecheck`、`npm run lint`、`npm run build`；
- `npm run verify:local`；
- fake-only 双视口截图、尺寸检查和验证记录。

## 交付与下一步

UX-03F-B 交付的是可实现、可验收的交互层级，不是页面代码。下一步是
UX-03F-C：先写 focused browser acceptance，再按上述最小写集实现一个 Gateway
Usage Presentation 切片，最后进入 UX-03F-D 双视口最终审阅。

本设计不代表实现、PR、主干合并、部署、Candidate、Instance 资格或生产状态。
