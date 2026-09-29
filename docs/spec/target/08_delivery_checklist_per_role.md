# 08 开发批次、五方交付与验收

> 本文把全部F01–F17转成可验证交付，不引入额外签字/固定评审轮次。职责按Owner明确，无需为了“具名”虚构团队成员。
>
> **2026-09-29业务决定已采用，新的wire待W01贯通**：`tencent-tke`首链，新客户一次确认后后台完整部署；未选Agent/WebUI即批准`opl-app + nativeUI`。应用判别联合、Agent必需三输入与异步交接按06；不是实现或生产可用声明。03/SQL/proto/events/机器flows仍须W01成套贯通；06第17节保留迁移前wire baseline，本轮不重新生成。W01集成负责人同步并验证后才能给出相应层的新链通过结论。

## 1. “不用二次对齐”的具体含义

所有人先读12产品主说明，产品/客户代表先看11与可点击原型，工程师再读技术契约；相关投影须按本轮已采用业务及06同步核验，不能残留“必须选Agent/三个版本”前提，不能反过来否定默认App路径。客户无需读DTO或SQL。

同一个F功能使用相同的产品名、输入字段、API operationId、DB writer/字段、状态、错误与验收。工程师只实现契约，不自行决定什么时候扣款、能否删除数据、哪些按钮可用。

不是承诺永远不会出现新需求或外部能力差异。新事实若改变已批准产品语义，必须只修改唯一Owner并同步消费者/矩阵；不能让前后端各自补一个解释。

## 2. 五方各自收到的交付物

| 角色 | 必须能直接使用的内容 | 如何验收 |
|---|---|---|
| 架构师/Tech Lead | 00决定、01权威/调用/可替换边界、02物理存储、06一致性、09迁移 | 每份状态/钱/资源只有一个writer；跨域失败有确定恢复规则 |
| 后端 | 02 SQL/字段/索引/约束；03 DTO/权限；proto/events；06每阶段读写和证据 | 真实decoder/DB/API/事件测试，不凭表名自行造字段 |
| 前端 | 03 generated types +04逐页面字段/动作/API/状态+07错误文案 | 不写第二套金额/状态逻辑；所有按钮触发存在接口 |
| 产品经理 | 00范围、04用户旅程、06业务规则、10 F矩阵 | 成功/失败/重复/越权/重启均有可验收用户结果 |
| 客户/客户代表 | 04中文页面语义、明确收费/删除/停用提示、功能验收剧本 | 能默认App+套餐一次确认部署，或选择Agent；看进度、打开使用、更新/退出且理解费用与数据后果 |

客户无需阅读SQL，后端无需猜UI。相同业务事实通过各自投影呈现，不要求五个人背相同内部术语。

## 3. 开发批次与依赖

本节定义验收层与阶段目标；任务归属仍引用14_implementation_work_packages.md的W00–W31，W01集成负责人须同步核验依赖，移除将W09 Agent Build作为所有新购前置、或将Local全链作为TKE首链工作前置的旧约束。不能用旧依赖给默认App强加Build；不在本文复制另一份Backlog。

v2.21–v2.25是规划阶段标签，不代表这些版本已经发布。以下顺序保留原路线意图，但把鉴权、金额、幂等、迁移测试放在真实依赖之前，不能等最后“加安全”。

| 批次 | 主要功能 | 先决交付 | 本批终点 |
|---|---|---|---|
| P0 目标/契约/迁移基础 | 全部跨域契约；F01最小认证/Tenant；F16来源映射 | 当前SHA/现有contract只读事实 | 正式架构Owner同步；API/SQL/proto/事件一致；旧数据反例可执行 |
| P1 Agent能力（v2.21） | F02/F03/F04/F05/F06 | P0+Storage/Registry/Runtime发布合同 | 真上传→native或独立approved WebUI构建→远端digest→唯一可部署版本；不是默认App首链前置 |
| P2 Workspace交付（v2.22） | F07/F08/F09/F10 | P0+approved RuntimeRelease/发布合同、Gateway授权/原单、TKE资源/Serve、必要Ledger证据；Agent分支另依赖P1或已有获准Agent制品 | 先默认App一次确认可打开，再Agent同链；两分支更新不重购/不破坏数据 |
| P3 商业/生命周期（v2.23） | F11/F12/F13/F14/F15 | P2+批准报价/退款/续费策略 | 周期不重复扣费，删除/退款分开、Tenant资产保留/权限闭合 |
| P4 旧路径迁移（v2.24） | F16批次切换 | P0试迁移+P1-P3能力与反例 | 每批单writer、历史不丢、已购资源沿用、回滚可验证 |
| P5 生产资格（v2.25） | F17/全链真实质量 | 同一精确Candidate、Instance授权环境 | Local/Instance真实证据齐全，按已授权流程发布同字节 |

### 验收分三层，不混首链与完整发布

| 层 | 范围/必要前置 | 接受证据 | 不作为本层前置 / 不证明 |
|---|---|---|---|
| TKE首条业务链 | `tencent-tke`批准profile；F01、RuntimeRelease准入/套餐报价、F07/F08/F09；默认`opl_app(runtimeVersionId)`+nativeUI一次确认 | 精确Candidate；Workspace原Operation→原单→Fabric资源→Serve Reserve→Key/Secret→Serve应用/访问；真实nativeUI/模型探针与必要收据。关闭浏览器、重启/丢ACK仍继续同一义务 | 不以自定义Package上传/Build、独立WebUI或Local provider全链完成为前置；不宣称F01–F17全交付或可正式发布 |
| Local回归 | 相同应用选择/Saga/Owner契约在合格Linux Local-Docker执行；默认App与Package+Runtime+独立WebUI的Agent两种组合、重放/unknown/生命周期反例 | 固定Candidate的Local证据；资金使用明确隔离authority fixture，不产生真钱副作用 | 不替代TKE采购/存储/网络/访问readback，不改变TKE主线；可按独立写集并行，不要求先Local通过才开始TKE首链工作 |
| 完整产品/发布资格 | 默认App、官方/自定义Agent、两种Build UI、F07–F13生命周期及其余F范围、F16既有对象义务；适用的Local与Instance验证均齐 | 同一精确Candidate SHA/digest的Local/Instance资格、回滚/迁移证据；获得授权后同字节晋级Release | TKE首链成功或静态wire通过均不能代替全量资格；不因先做TKE而豁免Local回归 |

TKE真实费用/采购/销毁和生产readback仍只能在另行明确授权、费用/资源范围受限的Instance保护流程内完成；本轮文档不执行、不授权这些动作。Cloud不能dispatch Instance部署，完整发布仍由已授权发布者执行。

P4生产切换在后面，不意味着P0-P3可以不考虑旧数据；ID、receipt、quote、provisioning形态从P0就必须兼容已有义务。全部Cloud开发都在同一`opl-cloud`仓库内，按01的独立module/进程映射逐个落实；没有必要同时建完所有空服务再验证首条链。

## 4. 后端按Owner的必交能力

| Owner | 必须完成的行为 | 边界测试重点 |
|---|---|---|
| Identity/Gateway Integration | Gateway委托认证；Tenant/成员/钱包主体映射；Key；原单资金读回 | 成员无钱包越权、一次性Code、unknown不重放、无密码/Key落库 |
| Capability | 上传/Package与Capability版本/claim；独立WebUI批准；注册Inbox | 上传摘要真实验证、同事件唯一版本、共享digest引用删除 |
| Build | 仅Agent固定Package/Runtime/独立WebUI三项精确输入；接受前校验并冻结；构建/推送/注册 | 三个真实claim、独立WebUI精确approved版本、发布合同、丢ACK恢复、原任务不覆盖；默认App不进Build |
| Resource Catalog | 套餐/价格版本、精确quote、AcceptQuote | 相同quote只接受一个operation，过期/输入变更拒绝 |
| Workspace | 两类应用共用业务Saga；接受目标/授权、原单/订阅、资源计划、调整/续费/删除/退款 | 跨重启、原周期唯一、unknown不逆操作；resource→Serve Reserve→Key无环；不写current deployment |
| Runtime Control | approved RuntimeRelease目录、OCI/发布合同、兼容性及精确引用；供默认App解析与Agent Build读取 | 默认App直接引用发布制品；不创建/启停实例、不写配置/路由/readiness |
| Serve | 两类应用唯一Deployment/current、实例、模型生效、更新/回滚、暂停/恢复、retirement及访问路由 | 原身份readback、真实ready、无数据卷双写、epoch/generation fencing；不购买资源、不重Build |
| Fabric | provider-neutral能力、预付资源、绑定/Secret、实际readback | 实际机器/盘/挂载/身份、确定absence、无凭据外泄 |
| Ledger | typed append-only receipt与查询、迁移证据 | 无Workspace build类型明确准入，原单/删除/退款精确绑定 |
| BFF | 会话/CSRF、REST产品DTO、owner路由 | 不跨库、不做Saga、不缓存钱包为权威、不泄露内部token |

每个新跨模块类型必须在同一Cloud commit同时更新真实consumer、测试和共享契约。消费者必要的`go.mod`/`go.sum`更新属于契约工作写集，不因触及其他服务目录就视为无关改动；应证明依赖链与受影响构建。不能为旧测试额外增加兼容字段/旁路；旧历史义务由09明确迁移。

## 5. 必须关闭的现有实现缺口（不是待拍板产品选项）

| 编号 | 实施Owner | 必交变化 | 不能替代成什么 |
|---|---|---|---|
| I01 | Console/Workspace/Serve | 新入口显式默认opl_app+nativeUI或agent+quote，一次确认完整部署；旧对象另有沿用资源动作 | 空Capability猜默认、fakePackage/Build/CapabilityVersion或继续只交付资源 |
| I02 | Gateway Integration | Tenant billing主体的授权、精确Code/交易读回与原账户映射 | 任意管理员令牌代付/复制钱包 |
| I03 | Runtime Control/Capability/Build | 正式发布合同；默认RuntimeRelease直接OCI；Agent固定Package/Runtime/独立WebUI三输入并接受前冻结 | 给默认App造WebUI版本或Build、Agent缺独立WebUI、worker执行时重选或写死Runtime启动路径 |
| I04 | Ledger | Build/目录/迁移等新证据类型及非Workspace收据准入 | 填假workspaceId或新建旁路Evidence服务 |
| I05 | Fabric/Serve | TKE优先；Fabric资源/resize/绑定readback，Serve部署/模型/路由/retirement；Runtime Control仅目录 | Fabric部署OCI/切流量、Runtime Control管实例、Local fixture冒充TKE资格 |
| I06 | 全部owner | typed gRPC/Outbox Inbox及schema权限 | NATS/HTTP描述混用，无事务发布 |
| I07 | Catalog/Workspace | 创建/续费/resize/delete准确报价/政策snapshot | 工程师自己发明退款比例或用最新价格改历史 |
| I08 | 各数据Owner/Instance | 09字段映射、未决义务、写屏障与回滚演练 | 全表复制后删旧库/永久双写 |

以上是每个功能实施验收的一部分；文档已给语义，当前代码未具备不等于方案可忽略。对应功能未完成前，不把相关入口宣传为可用。

## 6. 场景级接受标准

每个F在10中都有页面、operationId、schema.field、owner表、状态及测试向量。额外必须执行这些跨功能场景：

1. 默认App直接部署：不选Agent/WebUI正规化为精确approved RuntimeRelease+nativeUI；不用Package/Build/CapabilityVersion，quote→扣款→资源→Serve Reserve→Key/Secret→应用/访问→必要receipt对应同一Workspace；一次确认后关页仍完整交付。已有官方Agent直接部署另验同链，不误当默认App。
2. 自制Agent：上传→选择精确Runtime与独立approved WebUI→Build→版本→同一Workspace/Serve部署链；三项输入在接受前固定，缺Package或独立WebUI明确拒绝，输入与manifest一致；重试/目录默认变化不重选。
3. 刷新/断网：页面回来看到同一Operation，无重复包/Build/购买。
4. 扣费成功后RPC丢响应：先按原Code读回，无第二扣款；实际资源失败的closeout与退款分别可见。
5. 更新失败：旧购买/数据不变，确切旧Runtime/路由恢复；不只是UI显示“回滚成功”。
6. 删除已完成退款未确认：客户可区分两结果，原Key/Agent制品和历史不会被误删。
7. 租户越权：其他Tenant的ID/日志/操作/Key/reveal/报价均拒绝，前端隐藏不算授权检查。
8. legacy_resource_only仅旧对象沿用：不重购不重扣，第一次显式安装默认App或Agent后真实能打开；新默认App不能被标成legacy_resource_only。
9. legacy_application导入：无伪造Build/Package，精确digest/Key/数据/URL保持。
10. 旧未决义务：切换写权前后只有一个执行者，同Code/provider身份可恢复。
11. 数据兼容拒绝：不可安全更新的版本清楚说明原因，不尝试后再靠快照“兜底”。
12. 第三方Runtime：通过显式合约可部署；不支持能力在准入拒绝，不选用官方Runtime替代。
13. 两分支逐一跑F09模型、F10更新/回滚、F11变配、F12续费/到期恢复、F13删除/退款：Serve唯一写实例/current/access，原数据/单据/Key义务不变；默认App缺CapabilityVersion不被跳过或判legacy。
14. 06异步矩阵逐交接注入丢ACK/owner重启：原durableOperation/effectID查回，无第二采购/Key/切换/退款；Ledger不可用不重做副作用、不驱动命令，不要求每轮poll收据。

## 7. 验证层级与证据

| 验证层 | 工具/结果 | 证明什么 | 不证明什么 |
|---|---|---|---|
| 规格静态 / W01 wire | 定向文档检查；W01同步后再运行checks/validate_spec.py及相应生成/消费者检查 | 定向检查只证明本写集语义/边界；旧报告只证明其原SHA/hash的wire baseline | 不证明新判别联合/UI默认/async handoff已落API或跨文档全量一致，更不证明业务/生产 |
| 本轮隔离DDL实测 | checks/db_execution*.json（以当前schema哈希匹配的最新记录为准） | PostgreSQL建表、列类型/nullable、实际FK/唯一约束/权限和金额边界 | 服务业务事务、Saga、浏览器、生产采用 |
| owner单元/边界 | 新类型decoder/状态机/DB事务测试 | 本服务约束、拒绝/重放 | 下游实际provider能力 |
| 跨服务集成 | PostgreSQL+真实服务HTTP/gRPC，明确外部依赖测试边界 | 消息/事务/身份/错误链 | 真钱扣退款/生产资源 |
| 浏览器 | Console→真实BFF→owner→应用 | 表单/进度/刷新/权限/静态资产/SSE | 自动成为生产发布资格 |
| Local资格 | 干净合格Linux Local-Docker、固定Candidate | 本provider运行/存储/生命周期 | Tencent与实际Instance部署 |
| Instance资格 | 保护runner、原Candidate digest、实际owner readback/新receipt | 指定实例/时间/输入的采用 | 其他版本/环境普遍可用 |

源码实施按仓库规则先focused，再verify:local；跨模块/数据库/provider结构用verify:local:full。测试失败必须记录，不以“通常没问题”跳过。规格文档变更执行相应一致性检查，不以无关产品构建冒充文档验证；后续实现再按实际写集执行上述源码验证。

## 8. 并行分工与合并要求

- API/schema/共享状态是一个版本；target architecture已确定的字段不重新设计。按01的单仓库目录及独立Owner写集并行UI/DB/服务；同文件、共享contract revision、公共构建配置及canonical main由协调者串行整合。
- 同一功能的前后端不各写一份DTO，以03生成/导入typed client；02映射写入Owner，04消费这些字段。
- 一个PR按一个活能力切换真实caller，再退休旧路径；Migration/Instance切换按09单独证据。
- 不为流程强制worktree、固定TDD轮数、个人签字或每步批准；涉及真实钱/资源/生产时仍须原有授权范围。

## 9. 本轮交付与后续实现的分界

本轮交付仅06/08/09的SSOT时序、验收分层和旧对象迁移口径：默认App/Agent设计已采用，不等待产品再次确认；W01新wire、真实消费者、UI投影与完整一致性验证仍未完成。本轮不修改03/SQL/proto/events/机器flows/generators，也不更新旧checks报告为通过。历史R项/静态检查或pending_user为空不能代表此次新链已贯通，更不是业务代码包或全量交付。

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

W00收齐目标与迁移口径；W01在现有contracts module落实协议、固定工具/schema hash并验证消费者，不止生成成功；W02按01在同仓库逐域建立模块、进程及数据库隔离，不创建领域GitHub仓库。实际开始/验收依赖以14为准，不因此重复添加审批门槛。TKE默认App首链以W15/W17所需最小交付/访问能力为终点，不依赖W09构建默认制品；W09提供Agent构建纵向链，再接同一W15/W17。14中准确前置须由W01集成负责人同步；W19–W21完成生命周期；W25/W30负责试迁移与真实切换；W27–W31负责候选/资格/同字节发布，不能让Cloud代替Instance执行生产工作。
