# 08 开发批次、五方交付与验收

> 本文把全部F01–F17转成可验证交付，不引入额外签字/固定评审轮次。职责按Owner明确，无需为了“具名”虚构团队成员。

## 1. “不用二次对齐”的具体含义

所有人先读12产品主说明，产品/客户代表先看11与可点击原型，工程师再读技术契约。客户无需读DTO或SQL。

同一个F功能使用相同的产品名、输入字段、API operationId、DB writer/字段、状态、错误与验收。工程师只实现契约，不自行决定什么时候扣款、能否删除数据、哪些按钮可用。

不是承诺永远不会出现新需求或外部能力差异。新事实若改变已批准产品语义，必须只修改唯一Owner并同步消费者/矩阵；不能让前后端各自补一个解释。

## 2. 五方各自收到的交付物

| 角色 | 必须能直接使用的内容 | 如何验收 |
|---|---|---|
| 架构师/Tech Lead | 00决定、01权威/调用/可替换边界、02物理存储、06一致性、09迁移 | 每份状态/钱/资源只有一个writer；跨域失败有确定恢复规则 |
| 后端 | 02 SQL/字段/索引/约束；03 DTO/权限；proto/events；06每阶段读写和证据 | 真实decoder/DB/API/事件测试，不凭表名自行造字段 |
| 前端 | 03 generated types +04逐页面字段/动作/API/状态+07错误文案 | 不写第二套金额/状态逻辑；所有按钮触发存在接口 |
| 产品经理 | 00范围、04用户旅程、06业务规则、10 F矩阵 | 成功/失败/重复/越权/重启均有可验收用户结果 |
| 客户/客户代表 | 04中文页面语义、明确收费/删除/停用提示、功能验收剧本 | 能选Agent+套餐部署、看进度、打开使用、更新/退出且理解费用与数据后果 |

客户无需阅读SQL，后端无需猜UI。相同业务事实通过各自投影呈现，不要求五个人背相同内部术语。

## 3. 开发批次与依赖

本节定义阶段目标；实际分工和开始/验收依赖以14_implementation_work_packages.md的W00–W31为准。不得只拿阶段名称当开发任务，或把本节复制成另一份维护中的Backlog。

v2.21–v2.25是规划阶段标签，不代表这些版本已经发布。以下顺序保留原路线意图，但把鉴权、金额、幂等、迁移测试放在真实依赖之前，不能等最后“加安全”。

| 批次 | 主要功能 | 先决交付 | 本批终点 |
|---|---|---|---|
| P0 目标/契约/迁移基础 | 全部跨域契约；F01首链所需认证/Tenant；F16从首批能力开始的来源映射 | 当前SHA/现有contract只读事实 | 正式架构Owner同步；API/SQL/proto/事件一致；真实调用消费协议；旧数据反例可执行 |
| P1 Agent平台（v2.21） | F02/F03/F04/F05/F06的首条链切片 | P0+Build收据类型+Storage/Registry/Runtime发布合约；不要求全量价格/资源目录 | 登录→真上传→构建→远端digest→唯一可部署版本→Console回读；F03价格目录未完成不称全F03/P1完成 |
| P2 Workspace平台（v2.22） | F07/F08/F09/F10 | P1+Gateway原单授权/扣费、Fabric能力、Ledger新receipt支持 | 客户Agent+套餐部署可用；更新不重购不破坏数据 |
| P3 商业/生命周期（v2.23） | F11/F12/F13/F14/F15 | P2+批准报价/退款/续费策略 | 周期不重复扣费，删除/退款分开、Tenant资产保留/权限闭合 |
| P4 旧路径迁移（v2.24） | F16批次切换 | P0试迁移+P1-P3能力与反例 | 每批单writer、历史不丢、已购资源沿用、回滚可验证 |
| P5 生产资格（v2.25） | F17/全链真实质量 | 同一精确Candidate、Instance授权环境 | Local/Instance真实证据齐全，按已授权流程发布同字节 |

首条链的最小切片只证明Package/Build/ready版本/Console，不能宣称完整F03或完整W14；W06资源套餐和价格目录随第二条购买链验收。

P4生产切换在后面，不意味着P0-P3可以不考虑旧数据；ID、receipt、quote、provisioning形态从P0就必须兼容已有义务。全部Cloud开发都在同一`opl-cloud`仓库内，按01的独立module/进程映射逐个落实；没有必要同时建完所有空服务再验证首条链。

## 4. 后端按Owner的必交能力

| Owner | 必须完成的行为 | 边界测试重点 |
|---|---|---|
| Identity/Gateway Integration | Gateway委托认证；Tenant/成员/钱包主体映射；Key；原单资金读回 | 成员无钱包越权、一次性Code、unknown不重放、无密码/Key落库 |
| Capability | 上传/目录/版本/claim；Runtime/WebUI批准；注册Inbox | 上传摘要真实验证、同事件唯一版本、共享digest引用删除 |
| Build | 固定输入、worker隔离、构建/推送/注册、进度和重试 | 发布者契约、推送丢响应、注册ack丢失、原任务历史不覆盖 |
| Resource Catalog | 套餐/价格版本、精确quote、AcceptQuote | 相同quote只接受一个operation，过期/输入变更拒绝 |
| Workspace | 业务Saga、原单/订阅、选中部署、调整/续费/删除/退款 | 跨重启、并发点击、原周期唯一、unknown不逆操作、单选中部署 |
| Runtime Control | 发布契约准入、实例/配置版本、切换/回滚 | 真实ready、无数据卷双写、旧epoch不能切流量 |
| Fabric | provider-neutral能力、预付资源、绑定/Secret、实际readback | 实际机器/盘/挂载/身份、确定absence、无凭据外泄 |
| Ledger | typed append-only receipt与查询、迁移证据 | 无Workspace build类型明确准入，原单/删除/退款精确绑定 |
| BFF | 会话/CSRF、REST产品DTO、owner路由 | 不跨库、不做Saga、不缓存钱包为权威、不泄露内部token |

每个新跨模块类型必须在同一Cloud commit同时更新真实consumer、测试和共享契约。消费者必要的`go.mod`/`go.sum`更新属于契约工作写集，不因触及其他服务目录就视为无关改动；应证明依赖链与受影响构建。不能为旧测试额外增加兼容字段/旁路；旧历史义务由09明确迁移。

## 5. 必须关闭的现有实现缺口（不是待拍板产品选项）

| 编号 | 实施Owner | 必交变化 | 不能替代成什么 |
|---|---|---|---|
| I01 | Console/Workspace | resource_only客户入口→Agent+quote新入口；旧对象有沿用资源操作 | 给旧接口加隐藏默认Agent |
| I02 | Gateway Integration | Tenant billing主体的授权、精确Code/交易读回与原账户映射 | 任意管理员令牌代付/复制钱包 |
| I03 | Capability/Build | 正式Package/WebUI/Runtime集成contract、真实构建与登记 | 写死所有Runtime启动路径 |
| I04 | Ledger | Build/目录/迁移等新证据类型及非Workspace收据准入 | 填假workspaceId或新建旁路Evidence服务 |
| I05 | Fabric/Runtime | 每provider的部署/更新/config/resize实际能力与readback | 测试fixture成功冒充腾讯执行成功 |
| I06 | 全部owner | typed gRPC/Outbox Inbox及schema权限 | NATS/HTTP描述混用，无事务发布 |
| I07 | Catalog/Workspace | 创建/续费/resize/delete准确报价/政策snapshot | 工程师自己发明退款比例或用最新价格改历史 |
| I08 | 各数据Owner/Instance | 09字段映射、未决义务、写屏障与回滚演练 | 全表复制后删旧库/永久双写 |

以上是每个功能实施验收的一部分；文档已给语义，当前代码未具备不等于方案可忽略。对应功能未完成前，不把相关入口宣传为可用。

## 6. 场景级接受标准

每个F在10中都有页面、operationId、schema.field、owner表、状态及测试向量。额外必须执行这些跨功能场景：

1. 官方版本直接部署：不用上传/Build；quote/扣款/Key/资源/运行/receipt对应同一Workspace。
2. 自制Agent：上传→构建→选择版本→部署；选择WebUI在Build输入与产物manifest中精确相同。
3. 刷新/断网：页面回来看到同一Operation，无重复包/Build/购买。
4. 扣费成功后RPC丢响应：先按原Code读回，无第二扣款；实际资源失败的closeout与退款分别可见。
5. 更新失败：旧购买/数据不变，确切旧Runtime/路由恢复；不只是UI显示“回滚成功”。
6. 删除已完成退款未确认：客户可区分两结果，原Key/Agent制品和历史不会被误删。
7. 租户越权：其他Tenant的ID/日志/操作/Key/reveal/报价均拒绝，前端隐藏不算授权检查。
8. legacy_resource_only沿用：不重购不重扣，第一次安装Agent后真实能打开。
9. legacy_application导入：无伪造Build/Package，精确digest/Key/数据/URL保持。
10. 旧未决义务：切换写权前后只有一个执行者，同Code/provider身份可恢复。
11. 数据兼容拒绝：不可安全更新的版本清楚说明原因，不尝试后再靠快照“兜底”。
12. 第三方Runtime：通过显式合约可部署；不支持能力在准入拒绝，不选用官方Runtime替代。

## 7. 验证层级与证据

| 验证层 | 工具/结果 | 证明什么 | 不证明什么 |
|---|---|---|---|
| 本轮规格静态 | checks/validate_spec.py及verification.json | OpenAPI/ref/schema、proto编译、SQL解析、跨文档引用/状态/字段一致 | DB迁移/业务实现/客户可用 |
| 本轮隔离DDL实测 | checks/db_execution*.json（以当前schema哈希匹配的最新记录为准） | PostgreSQL建表、列类型/nullable、实际FK/唯一约束/权限和金额边界 | 服务业务事务、Saga、浏览器、生产采用 |
| owner单元/边界 | 新类型decoder/状态机/DB事务测试 | 本服务约束、拒绝/重放 | 下游实际provider能力 |
| 跨服务集成 | PostgreSQL+真实服务HTTP/gRPC，明确外部依赖测试边界 | 消息/事务/身份/错误链 | 真钱扣退款/生产资源 |
| 浏览器 | Console→真实BFF→owner→应用 | 表单/进度/刷新/权限/静态资产/SSE | 自动成为生产发布资格 |
| Local资格 | 干净合格Linux Local-Docker、固定Candidate | 本provider运行/存储/生命周期 | Tencent与实际Instance部署 |
| Instance资格 | 保护runner、原Candidate digest、实际owner readback/新receipt | 指定实例/时间/输入的采用 | 其他版本/环境普遍可用 |

源码实施按仓库规则先focused，再verify:local；跨模块/数据库/provider结构用verify:local:full。测试失败必须记录，不以“通常没问题”跳过。规格文档变更执行相应一致性检查，不以无关产品构建冒充文档验证；后续实现再按实际写集执行上述源码验证。

## 8. 并行分工与合并要求

- API/schema/共享状态是一个版本；v2.26已确定的字段不重新设计。按01的单仓库目录及独立Owner写集并行UI/DB/服务；同文件、共享contract revision、公共构建配置及canonical main由协调者串行整合。
- 同一功能的前后端不各写一份DTO，以03生成/导入typed client；02映射写入Owner，04消费这些字段。
- 一个PR按一个活能力切换真实caller，再退休旧路径；Migration/Instance切换按09单独证据。
- 不为流程强制worktree、固定TDD轮数、个人签字或每步批准；涉及真实钱/资源/生产时仍须原有授权范围。

## 9. 本轮交付与后续实现的分界

本轮交付：在原稿上关闭交接审计R01–R10，保留00–10各自Owner，补11页面级UI/UX及可点击原型、12产品主说明，并增加真实跨层语义验证。D17现已确认；仍须所有R项关闭、checks/decision_status.json无pending_user且检查通过，才标全量可开工；此前的字段/解析通过不替代这个判断。不是新实现代码包。

实际开发完成需要：代码/迁移/文档同变更、owner与边界测试、浏览器真实功能证据、适用的Local/Instance回执。没有这些，不能把本包的“可开发”称为“已交付生产”。

## 10. 全量交接的角色签收，不让大家各补一套定义

| 角色 | 本次必须交付 | 不允许留给对方的决定 |
|---|---|---|
| 产品 | F场景、角色、收费/数据后果、异常结果；D17已确认规则及数值案例 | 让后端猜补差/退款、生效日或把缺功能说成失败处理 |
| UI/UX | 页面线框/原型、导航、首要动作、表单控件、步骤/返回/草稿/确认/响应式 | 只给字段清单让前端决定整个用户流程 |
| 前端 | 逐页面请求/响应映射、状态/文案、完整操作恢复和键盘/窄屏行为 | 自造成功状态、硬编码等待时间、重新计算业务金额 |
| 后端 | 每个跨Domain typed命令/响应、持久字段、Owner事务/读回、幂等恢复 | 私下转换deploy/create，发明授权context或切流量顺序 |
| 架构 | 有caller的边、单writer、依赖与故障隔离、迁移/回滚边界 | 每个RPC另建服务、万能事件总线或第二钱包 |
| 客户代表 | 能走通12的故事并解释价格/版本/数据结果 | 被迫理解SQL、ABI、Saga或猜系统是否扣过钱 |

技术细节的实现自由不意味着产品语义自由。颜色、排版可在现有设计体系内细化，但必须遵守11的信息层级与交互；技术选型不再新增基础框架。D17已由用户确认；不重复追问已经批准的收费规则、领域边界和客户主流程。

## 11. 开工使用方式

每个实施任务从14领取W编号；F编号、operationId、表及RPC直接引用冻结规格。进入任务前确认对应source/write set和依赖；完成时交代码、focused结果、真实caller证据和本层未验证项。每个PR保持一个可验收能力的切换，不强制额外worktree/审批轮次，不一次重写所有Owner。

W00收齐目标与迁移口径；W01在现有contracts module落实协议、固定工具/schema hash并验证消费者，不止生成成功；W02按01在同仓库逐域建立模块、进程及数据库隔离，不创建领域GitHub仓库。实际开始/验收依赖以14为准，不因此重复添加审批门槛。W09与W15形成前两条真实纵向链；W19–W21完成生命周期；W25/W30负责试迁移与真实切换；W27–W31负责候选/资格/同字节发布，不能让Cloud代替Instance执行生产工作。
