# OPL Cloud v2.26 — 完整开发方案总索引

> 定稿产品范围为F01–F17，D17升级/降配规则已获确认。规格当前字段闭合以checks/validate_spec.py和validate_domain_reference.py的绑定结果为准；整包交接状态仍以checks/handoff_readiness.json及其绑定证据为准。任一通过都不是业务可用、客户迁移或生产资格证明。
> 本轮目标：客户选择 Agent 版本和计算/存储套餐，完成 SaaS 部署；旧资源购买路径有明确迁移。
> 本次对齐范围：修正现有权威契约和W任务书供架构/前后端实施；尚未修改产品业务代码、部署、扣费或采购。

## 当前开发与最终集成位置

`opl-cloud`是`one-person-lab-cloud`的开发fork，v2.26在继承实现上演进，用户验收后经Issue/PR合回上游；不是另起产品。代码合回、09的客户数据迁移、Instance生产采用分别验证和授权。[15领域字段对齐](15_domain_alignment.md)列出继承API/DB、各域目标字段、交互和已发现契约断点；字段权威仍在02/03/proto/events，不由派生参考重复定义。本次已在这些权威文件修复操作/事件身份和BFF路由元数据；生成代码、真实服务消费者尚未同版接入，见14及status。

## 开发 Index：W00–W31 的一处入口

**执行入口**：[14 工作包与依赖](14_implementation_work_packages.md)；按功能追调用链看[10 功能追踪](10_feature_traceability.md)、[06 跨域状态机](06_data_flow_and_state_machine.md)，按 Owner 查[15 字段/继承对齐](15_domain_alignment.md)。14 的每包列主实现 API、Owner 表、RPC、前端消费 API、落点、验证与验收；它们是任务指针而非第二套字段定义。下表只作导航，不重新写 DTO/SQL。

| 阶段 | 工作包（每项直达实施任务） | 可观察结果 |
|---|---|---|
| 基线与生产协议 | [W00 目标/来源](14_implementation_work_packages.md#w00) · [W01 契约/生成/消费者](14_implementation_work_packages.md#w01) · [W02 服务/独立 DB/操作协议](14_implementation_work_packages.md#w02) | 目标与现状分层，同版协议由真实 Owner 消费；已有骨架只补未闭合项 |
| 首链基础 | [W03 身份授权](14_implementation_work_packages.md#w03) · [W05 Ledger 证据](14_implementation_work_packages.md#w05) · [W07 Publisher 准入](14_implementation_work_packages.md#w07) · [W08 Package 上传](14_implementation_work_packages.md#w08) · [W09 真实构建/注册](14_implementation_work_packages.md#w09) · [W13 BFF 基础](14_implementation_work_packages.md#w13) · [W14 Agent/Build 前端](14_implementation_work_packages.md#w14) | 登录→上传→真实 Build→唯一 ready 版本→BFF/Console 回读；W14 首切片不等于全包 |
| 购买与部署 | [W04 Gateway 资金/Key](14_implementation_work_packages.md#w04) · [W06 资源/价格目录](14_implementation_work_packages.md#w06) · [W10 Runtime Control](14_implementation_work_packages.md#w10) · [W11 Local-Docker Fabric](14_implementation_work_packages.md#w11) · [W12 Tencent/TKE Fabric](14_implementation_work_packages.md#w12) · [W15 报价/Launch](14_implementation_work_packages.md#w15) · [W16 部署/Workspace 前端](14_implementation_work_packages.md#w16) | 精确报价→接受→原单→资源与运行读回；Local 和 Tencent 分别资格化 |
| 生命周期与治理 | [W17 应用变更/回滚](14_implementation_work_packages.md#w17) · [W18 周期/续费](14_implementation_work_packages.md#w18) · [W19 D17 套餐变更](14_implementation_work_packages.md#w19) · [W20 删除/退款原单](14_implementation_work_packages.md#w20) · [W21 Tenant 生命周期](14_implementation_work_packages.md#w21) · [W22 费用/Key 前端](14_implementation_work_packages.md#w22) · [W23 管理员前端](14_implementation_work_packages.md#w23) · [W24 Owner 运维恢复](14_implementation_work_packages.md#w24) | 变更、续费、删除、Tenant 及运维各有唯一 Owner、原单与未知结果恢复 |
| 迁移/验证/发布 | [W25 逐域迁移演练](14_implementation_work_packages.md#w25) · [W26 业务链/浏览器验收](14_implementation_work_packages.md#w26) · [W27 Candidate/CI](14_implementation_work_packages.md#w27) · [W28 Linux Local 资格](14_implementation_work_packages.md#w28) · [W29 Instance/Tencent 资格](14_implementation_work_packages.md#w29) · [W30 客户切换/旧 writer 退出](14_implementation_work_packages.md#w30) · [W31 同字节正式发布](14_implementation_work_packages.md#w31) | 旧 ID/原单先试迁移，真实链与环境分别验收；客户切换与正式发布不是同一动作 |

**逐包实施规则**：从14选可开始的包→在01/02/03/proto/events/04/06/07/09/13核对该包真实 Owner、DB 列、API/消息字段、前端状态和失败/unknown 恢复→修改源 Owner 与真实调用者→按08验收并写状态证据。10/15/reference/development_plan 是读视图，不是另外一套业务字段。发现字段不存在、调用端未真正使用、外部能力不符或旧数据无法保真时，在权威 Owner 修改并同步消费者/任务书，不默许局部猜测。各包按14的 `startAfter` 与 `acceptAfter` 推进，**不是按数字机械串行**。

**当前可开工但非可直接宣称完成**：W00 已有目标文档，W01 的协议字段已在规格中补齐但生产生成绑定/消费者未同版接入；W02 六域骨架不等于业务实现。既有 W25 来源映射需要随第一批能力推进。当前 `checks/handoff_readiness.json` 唯一规格交接阻点是 UI 原型回执仍绑定旧 OpenAPI hash；前端必须重测后留新证据，不能改旧回执冒充。`checks/development_plan.json` 的 `not_implemented` 是计划工具不追踪进度，不应据此重做已有代码；当前代码与测试事实看 `docs/status.md`。W29/W30 的 Instance 动作及 W31 正式发布均有独立授权与证据，开发任务书不自动授权执行。

## 1. 一句话产品与边界

面向普通 SaaS 用户提供官方和私有 Agent 的构建、部署、运行与更新体验；Gateway 提供模型能力，Workspace 提供持续运行环境。商业主线是 AI 能力/Token 服务，Workspace 按批准套餐收费。

- Agent Package 是标准能力包；OCI 是可部署制品；Workspace 是付费环境；Runtime Instance 是某次运行实例。四者不是同义词。
- 客户选择 Agent 版本、计算/存储套餐、可用模型；上传自己的 Package 时可以选择 WebUI。Runtime 版本由管理员批准的目录策略固定，不接受客户任意镜像地址。
- 一个 Workspace 同时选中一个 Capability Version；同一版本可被多个 Workspace 使用。更新必须客户显式发起，不自动替换已有实例。
- 初期支持 Tencent/TKE 与 Local-Docker；提供商通过 Fabric 能力契约体现。某提供商不具备能力时明确拒绝，不静默换提供商。
- Sub2API 是身份认证、可消费钱包、Key、Token 用量权威；Cloud 不存密码、不复制可消费余额、不创建第二钱包。
- Cloud 拥有 Workspace 套餐价格/报价/订阅编排，Gateway 拥有模型价格与钱包记账。资源套餐报价不是 Token 定价。
- Ledger 保存不可变业务证据，不重复维护可消费钱包。
- Framework/OPL App 拥有 Runtime 实现；Cloud 只做 Runtime Control。Instance 拥有生产配置、Secrets、部署/回滚和资格回执。
- 单一GitHub仓库只覆盖Cloud产品：`apps/`、`services/`和`packages/`共同演进；不合并Instance、Sub2API、Framework/OPL App等外部Owner。领域服务独立不等于独立仓库，目录/module/进程映射以01第3节为准。

## 2. 本次收敛决定（不是等待再次选择的选项）

| ID | 决定 | 依据/约束 |
|---|---|---|
| D01 | 新客户入口是选 Agent 版本+套餐后部署，不再仅购买裸资源 | 当前用户明确确认；旧路径按 09 迁移 |
| D02 | 全部Cloud产品代码位于唯一GitHub仓库`opl-cloud`；领域服务在仓库内保持独立Go module、进程和数据写入边界，CloudIdentity与Gateway Integration按01共module/进程但分数据库/角色；Fabric/Ledger保留权威 | 2026-09-22用户明确单仓库决定；替代原多仓库落点，不改变v2.26领域职责或字段 |
| D03 | 每个数据 Owner 独立 PostgreSQL database/独立角色；可共 PostgreSQL 实例；跨 Owner 只传不透明 ID，不建跨域 FK/JOIN/事务 | 消除旧稿独立数据库与跨 schema FK 矛盾 |
| D04 | 外部浏览器 API 统一由 Console BFF 提供 REST；内部 typed gRPC/protobuf；可靠事件用本域 PostgreSQL Outbox→消费者 gRPC Inbox | 落实已讨论的内部协议和 Outbox；当前链路无须增加 NATS 运行依赖 |
| D05 | BFF 仅鉴权、授权上下文与产品 DTO 聚合；Workspace Service 拥有业务 Saga | 不把前端聚合层变第二编排器 |
| D05a | 通用Operation REST的`x-owner=bff`只标识入口；必填`owner`选择唯一领域Operation writer。BFF没有Operation表、不得遍历域猜ID；同进程tenant/gateway也按逻辑Owner访问各自数据库 | 03的路由元数据与proto OwnerOperationRequest/DeliverEventRequest必须保留目标身份 |
| D06 | Capability 拥有 Package/版本/目录元数据；对象字节存 Storage Provider；Build 只读不可变引用并写自己的任务/制品证据 | 每份数据一个 writer；Build完成事件触发Capability唯一writer创建版本 |
| D07 | Build 成功后才创建 ready Capability Version；构建中/失败只属于 Build Job | 不再要求没有digest的pending版本 |
| D08 | Package、OCI引用和Build历史不随Workspace删除；Package归档不物理级联历史；无自动90天清除 | 遵守用户已明确的保留决定；显式制品删除需引用保护，历史记录不删除 |
| D09 | 新ID为不透明string，API不强制UUID；已有ID、原扣费键、原收据和时间原样保留 | 历史迁移不能重新生成身份或付款义务 |
| D10 | 金额统一USDMicros；PostgreSQL bigint、protobuf int64、JSON十进制字符串；时间UTC RFC3339；客户按本地时区显示 | 不用JS浮点货币；整数上限int64 |
| D11 | 登录身份与Tenant钱包主体分开：成员使用自己的Gateway身份，Tenant绑定唯一billingSub2apiUserId；用户初期只属于一个活动Tenant | 企业多人不共享登录密码；钱包委托只在Gateway owner核准边界内 |
| D12 | UI沿用现仓React/TypeScript；不迁移Vue；不要求客户理解OCI、Saga、Pod | 旧稿.vue仅误写，不构成技术迁移决定 |
| D13 | Public Marketplace、自助第三方WebUI发布、跨Tenant私有OCI共享、任意Runtime自选、公开注册不在本次交付 | 管理员仍可准入第三方Runtime；不得用未来功能遮盖本期缺口 |
| D14 | 资源申请遵循现有批准的预付包月政策；文档/CI/本机验证不授权真实费用、采购、销毁或生产访问 | Instance保护流程独占高后果动作 |
| D15 | 本规格不设未获依据的50人容量承诺、3分钟上线承诺、10包配额、自动保留期或退款百分比 | 实例策略显式配置且UI展示；必要策略缺失则准入拒绝 |
| D16 | 保留期、报价和退款规则均版本化；按用户接受的报价快照执行，不能用最新价格覆盖既有义务 | Cloud产品价格与Gateway模型价格分权 |

## 3. 唯一权威文件与执行顺序

| 文件 | 唯一负责内容 |
|---|---|
| 00（本文） | 范围、决定、术语、统一状态词与功能ID |
| 01_domain_ownership_matrix.md | Owner、单仓库内目录/module/服务边界、真实调用、插件/适配契约 |
| 02_database_schema_complete.md + contracts/schema.sql | 字段、约束、索引、删除保护和保留；SQL是字段的可执行投影 |
| 03_api_contract_complete.yaml | 所有客户/管理员REST操作、DTO、权限、错误及幂等 |
| contracts/internal.proto + contracts/events.json | 内部调用与Outbox事件的机器可检查规格 |
| 04_frontend_interaction_spec.md | 页面/字段/动作/API/状态/错误/文案映射 |
| 05_build_service_technical_spec.md | 构建执行、不可变输入、隔离、凭据与制品读回 |
| 06_data_flow_and_state_machine.md | 跨域时序、状态转换、恢复与副作用证据 |
| 07_error_handling_matrix.md | 错误码→接口/操作状态→中文文案→允许动作 |
| 08_delivery_checklist_per_role.md | 五方验收、开发批次、交付责任和证据层级 |
| 09_legacy_migration.md | 旧路径与数据/接口迁移、停写切换、回滚、生产边界 |
| 10_feature_traceability.md + contracts/traceability.json | 功能ID贯穿页面/API/表/状态/验收；派生索引，不是第二份规则 |
| 11_ui_ux_design.md + ui-prototype/index.html | 页面布局、控件、完整交互、响应式/焦点与可点击业务原型 |
| 12_product_spec.md | 五方共同阅读的产品主说明、客户故事与角色/费用/数据承诺 |
| 13_plan_change_policy.md + contracts/plan-change-policy.json | D17已确认的升级/预约降配、精确补差、失败与原单结算规则 |
| 14_implementation_work_packages.md + checks/development_plan.json | 完整实施任务、真实/拟建代码落点、前后端分工、依赖、验证及交付顺序 |
| 15_domain_alignment.md + reference/ | 继承实现→目标领域的字段/交互对照、规格层已关闭和消费者仍未完成的缺口；派生导航，不是第二契约 |
| checks/development_plan_validation.json | API/表/RPC/F任务覆盖、路径真实性及无环依赖验证 |
| contracts/domain_flows.json + checks/render_domain_flows.py | 17类业务链与实际typed RPC/Owner写入/终态证据映射 |
| checks/validate_cross_domain.py + checks/semantics_cases.json | 真实API/protobuf→隔离PostgreSQL→读回的语义反例 |
| checks/handoff_readiness.json | 结构、语义、canonical Go、UI证据与未决用户决定的合并结论 |
| checks/validate_spec.py + checks/verification.json | 本次静态一致性验证及范围；失败如实保留 |

原Claude版本保存在同级 history/opl_v226_execution_spec-before-alignment-*，不再作为当前writer。当前仓库源码以本次读取SHA为迁移起点，不因本规格写成而视为已采用新架构。产品仓库正式architecture/decisions在实施首批变更时按09同步，不能长期维持两套已采用声明。

工作顺序：统一目标/接口词汇 → 并行修API、DB、UI → 串起业务链和迁移 → 跨文档校验/反例检查 → 交付。没有新增人工逐项审批门槛。

## 4. 全量功能范围与编号

| ID | 用户/管理员场景 | 交付结果 |
|---|---|---|
| F01 | 登录/退出、当前账户、Tenant/成员权限 | Gateway认证，Cloud权限与钱包主体映射 |
| F02 | 默认/自建分组、私有与官方Agent可见性 | Tenant隔离、官方版本可部署的显式可见性 |
| F03 | 管理员准入Runtime/WebUI与资源/价格目录 | 不可变版本、兼容性、有效期和可用性 |
| F04 | 上传Package与后续版本、选择WebUI、确认构建 | 可恢复上传、明确任务ID、不可变构建输入 |
| F05 | 构建进度/日志/失败重试 | 原任务不覆盖，重试新任务，无自动收费 |
| F06 | Agent详情/版本、归档与制品删除保护 | 引用中的版本不可删除，历史保留 |
| F07 | 选择Agent/套餐/模型、检查与报价 | 钱包/资源/兼容性准入，接受确切报价 |
| F08 | 部署、扣费、Key、资源、运行、证据 | 只在真实可用与凭据注入验证后成功 |
| F09 | Workspace列表/详情/打开、模型配置更新 | 归属校验、应用访问边界、配置reload状态 |
| F10 | 手动更换Agent版本/Runtime重建、回滚 | 不改购买历史，保留数据契约，单选中部署 |
| F11 | 计算/存储套餐调整 | 新报价、能力准入、可审计资源变更 |
| F12 | 续费、余额不足、到期停用与恢复 | 原周期幂等，无余额不开通，客户可见 |
| F13 | 删除Workspace、资源确认与退款 | 明确数据销毁提示，确认删除证据后退款 |
| F14 | 钱包/用量/Key管理、管理员充值记录 | Gateway唯一资金/Key权威，UI不伪造余额 |
| F15 | Tenant停用/删除/15天内恢复与资产托管 | 成员访问立即撤销，Workspace删除独立可追踪，私有制品不公开 |
| F16 | 旧Workspace/账户/义务/运行路径迁移 | 历史完整、单writer、零重复扣费/重购 |
| F17 | 运维操作/审计/资格与同制品发布 | 运行证据与源码测试分层；Instance执行 |

## 5. 统一状态与事实（其他文件不得新增同义状态）

- Package：active / archived；归档保留历史和对象，不触发级联。
- PackageVersion：upload_pending / uploaded / rejected；构建状态只在BuildJob，不挂在PackageVersion。
- BuildJob：queued / validating / building / pushing / registering / succeeded / failed / needs_attention。
- CapabilityVersion：ready / deprecated / deleting / deleted；成功构建且确认注册后才创建ready记录，digest必填。
- Workspace：provisioning / active / updating / suspended / deleting / deleted / failed / needs_attention。
- Deployment：queued / deploying / verifying / active / superseded / failed / rolling_back / rolled_back / needs_attention。
- RuntimeInstance：pending / starting / ready / stopped / failed / terminating / terminated。选中谁由Workspace.activeDeploymentId权威，不用Runtime.active布尔值制造第二writer。
- Operation：accepted / running / awaiting_confirmation / succeeded / failed / needs_attention / cancelled。
- 外部动作观察结果：confirmed / rejected / unknown；unknown不是failed，不基于unknown做重复扣费、反向退款或重复采购。
- Tenant：active / suspended / deleting / deleted；恢复仅在restoreUntil内恢复Tenant和保留资产权限，不复活已删除CBS、不自动重购Workspace。
- Namespace：active / archived；Runtime/WebUI目录：approved / deprecated / revoked；Quote：offered / accepted / expired。
- 钱包交易操作：requested / confirmed / rejected / unknown；删除/退款结果分别展示。
- PlanChange：requested / scheduled / awaiting_payment / applying / applied / failed / needs_attention / cancelled；保存scheduled计划的Operation成功不代表资源已applied。升级只在完整实际读回后生效，降配在原账期边界执行。

进度展示使用真实stage标签，不虚构线性百分比/固定完成时间。客户页面将needs_attention显示为“需要处理”，不得显示成“已退款”或“已部署”。

## 6. 共用请求规则

- 外部业务API base path /api/v2；浏览器业务请求只访问BFF。上传字节仅允许直传Capability签署的受限Storage URL，不能访问任意内部服务。现有 /api/* 不是隐式别名，迁移需显式路由切换。
- 所有写入业务命令必带Idempotency-Key；相同key+规范化相同body返回同一身份，body不同返回IDEMPOTENCY_CONFLICT；作用域tenant+actor+operationId，保存时间覆盖业务义务，不用短TTL破坏去重。
- 异步命令返回202 Operation（operationId、owner、resourceId、status、stage、requestId）；同步创建BuildJob返回201及operationId也只证明任务已登记，不把HTTP接受当业务成功。轮询GET操作详情；不要求WebSocket或伪成功回退。
- 写请求不接受客户自报tenantId、价格、provider、runtime镜像digest或wallet身份；从会话/批准目录/报价解析。管理员操作才可显式指定目标Tenant。
- 浏览器HttpOnly Secure SameSite session cookie；写操作校验CSRF与Origin；不把service token/Key送给应用或前端日志。
- 每个业务错误有稳定字符串code、requestId、可选fieldErrors；Operation持久记录errorCode与观察结果。前端不解析英文message分支。
- 角色：owner=租户所有者，admin=租户管理员，member=成员，platform_admin=平台管理员；anonymous/authenticated只表达会话态，不是可授予的业务角色。
- 外部实体ID不透明字符串；金额JSON string；不可变OCI引用使用sha256 digest而非可移动tag。带数据的更新必须验证发布者兼容性/迁移规格。
- 读取列表使用有界cursor分页，返回items/nextCursor；排序createdAt DESC,id DESC；越权对象404。
- Gateway Key明文只在创建/显式reveal的单次响应出现，Cache-Control:no-store；DB/事件/审计只存引用与指纹。

## 7. 五方一致的完成定义

每个F编号必须对应：客户能完成什么、页面展示哪些字段和动作、API operationId与DTO、唯一数据writer和字段、状态转移与副作用证据、成功/失败/重复/越权/重启验收。任何一环缺失则该功能不算完成。

本轮交付的是可核查方案。静态通过不等于后端已实现、浏览器已跑通或生产已合格。后续工程师不应再决定产品语义，但真实外部Owner能力仍需以其契约/运行读回验证，发现缺口不能静默改设计。

## 8. 正式交付文件清单与阅读顺序

| 文件 | 负责内容 | 优先读者 |
|---|---|---|
| 00_master_index.md | 本索引；范围、冻结决定、功能ID和来源权威 | 所有人 |
| 01_domain_ownership_matrix.md | 各Domain、内部lane、唯一writer、调用边界和Owner | 架构师、前后端 |
| 02_database_schema_complete.md | 全字段、约束、索引、原义务和事务边界 | 后端 |
| 03_api_contract_complete.yaml | 外部API、DTO、必填/错误/权限/幂等与字段来源 | 前后端 |
| 04_frontend_interaction_spec.md | 各页面控件/数据/按钮/请求/状态/错误与恢复 | 前端、产品 |
| 05_build_service_technical_spec.md | Package+Runtime+WebUI构建、保护、注册与证据 | 后端、平台 |
| 06_data_flow_and_state_machine.md | 17业务链的实际RPC、数据写入、终点与失败闭环 | 架构师、前后端 |
| 07_error_handling_matrix.md | 稳定错误码/阶段、中文话术、允许动作 | 前后端、产品 |
| 08_delivery_checklist_per_role.md | 角色交付、开发顺序、验收与证据层级 | Tech Lead、所有角色 |
| 09_legacy_migration.md | 旧资源/应用/身份/付费义务的字段级迁移与单writer切换 | 后端、架构师、Instance |
| 10_feature_traceability.md | 功能→页面→API→DTO→Owner表→验收的派生导航 | 所有人按F查找 |
| 11_ui_ux_design.md | 布局、控件、向导、草稿、响应式/焦点与原型映射 | 前端、设计、产品 |
| 12_product_spec.md | 共同产品理解、角色、客户故事、费用/数据承诺 | 所有人先读，客户优先 |
| 13_plan_change_policy.md | 已批准D17：升配/降配、补差精度、期边界、取消/失败/退款 | 产品、前后端、架构师 |
| 14_implementation_work_packages.md | 32个可分工工作包、启动/验收依赖、代码路径、首批PR与完整实施收尾 | Tech Lead、前后端、QA、Instance |

**配套目录**：

- ui-prototype/index.html：可点击离线原型，示例数据，不请求实际业务后台；screenshots/为已验证页面。
- contracts/schema.sql：分Owner数据库的DDL；禁止用一条生产连接直接执行全部分段。
- contracts/internal.proto：真正跨Domain的typed协议；service分组不等于新部署进程。
- contracts/events.json：版本事件和指定消费者。
- contracts/publisher-contract.schema.json：基于现有应用Revision合约的构建/发布描述。
- contracts/plan-change-policy.json：固定的已批准D17政策，不是新通用策略引擎。
- contracts/*inventory.json、domain_flows.json、traceability.json：可验证映射；不作为平行的业务writer。
- checks/：静态、契约、真实隔离DB、源Go与原型验证；逐次证据保存在runs/，失败历史不冒充最终通过。

**统一阅读路径**：12产品主说明 → 11与原型 → 按F编号读06闭环 → 前后端查03/02/04 → 14分工与实施顺序 → 08验收 → 09实施迁移。客户不必读SQL，工程师不必自行发明产品规则。

## 9. 什么叫“全量定稿”

本次确认范围内，正常/失败/unknown/重复/越权/重启/取消/账期竞争及明确不支持的结果都有Owner、接口、字段和客户表现。实施中不再重新决定D17怎么收费、哪个Domain写什么或页面意味着什么；正常实现/联调/运行验收仍必须完成，规格检查不替代它们。

当前Cloud源码仍是迁移起点；具体provider可执行能力、代码交付、Instance资格和上线分别按08/09验证。不能因为本包定稿，就说生产已经有这些新服务或每种CVM/CBS都支持任意变更。

旧稿和上一轮评审保留在同级history及已有审计记录，不再作为当前产品规则。无新工作流引擎、事件总线、第二钱包、第二Registry或万能插件Host。

## 10. 方案完整性与实施状态分开

这不是只有架构图、API列表或一条演示链：产品、UI/UX、字段、接口、跨Domain闭环、迁移、角色验收和开发执行任务均在同一包内。14为全部当前API、Owner表和内部RPC分配实现任务；每个任务可开始的依赖和最终验收依赖分开，不用所有人互相等待。

本任务书描述计划覆盖而非进度账本；`not_implemented`仅表示生成器不认证实施完成，不覆盖后续实施证据。实际完成状态以当前`docs/status.md`及Owner绑定的源码、验证与回执为准。本轮只修正规划落点；后续按实际代码/数据库/浏览器/Instance证据逐项交付，不把规划完成说成软件完成。原始source路径及SHA只描述取证时的迁移基线，不是当前写入指令；Cloud当前执行路径统一为同一`opl-cloud` checkout内的相对路径，按01/14映射。外部Owner仍使用各自checkout与授权流程；实施时如source SHA已变，W00核对真实caller差异，但不重复讨论已批准产品规则。
