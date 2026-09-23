# 06 功能链、状态转换与副作用证据

> 每条功能链对应00的F编号；API/DTO在03，字段/约束在02，UI在04。本文不再复制另一套DTO/状态枚举。

## 1. 全部写操作的共同执行语义

1. 在Owner事务内验证权限/版本前置、保存请求hash、幂等身份、Operation及业务意图；提交后才返回接受响应；异步操作返回202 Operation，创建BuildJob可返回201及其operationId，不代表构建完成。
2. 同一Workspace只允许一个已接受且未达确定终态的变更操作；未决旧操作不能被新请求抢占。完成/确定取消后才分配下一epoch，旧网络请求仍由Fabric/provider fence拒绝。同一Owner聚合用数据库行版本CAS/锁串行；每次执行记录epoch。旧worker失去lease后不得提交结果或发新的副作用。
3. 外部调用前持久记录原始actionId/idempotencyKey/请求摘要/目标身份；调用后保存confirmed/rejected/unknown及观察证据。
4. 对unknown先按原身份权威读回；不把timeout当failed，不自动逆操作。Operation可awaiting_confirmation，无法自动推进时needs_attention且给精确原因/允许动作。
5. HTTP202只是接受；Operation.succeeded必须满足本功能终点证据。页面恢复只读Operation，重复点击复用相同键，不产生第二购买。
6. 跨域靠typed调用、引用claim与Outbox/Inbox；没有跨库事务、全局锁、补偿引擎或用余额差猜付款。

Operation取消只在尚未发出不可逆动作且Owner确认可取消时允许；不是每个页面都提供取消。客户/管理员不能凭修改状态字段强行标成功。

## 2. F01/F02：身份、租户与分组

### 登录与授权

BFF获得同源登录上下文/CSRF → 将用户凭据短暂委托Gateway认证 → 读取Gateway用户身份 → Gateway Integration查Cloud成员/Tenant角色 → 建立HttpOnly会话。密码不落库不发事件不记日志；退出销毁会话并取消该浏览器轮询，不停止已接受后端任务。

Tenant Admin可管理本Tenant成员/Namespace；平台管理员可创建Tenant、绑定wallet主体和批准目录。普通成员可按明确package权限查看/协作，不因Package成员身份获得Build、购买或钱包操作权限。一个用户初期最多一个active Tenant。

官方目录属于平台发布命名空间，通过可见性策略提供“可读/可部署”权利；不是任意跨租户私有OCI共享。私有Namespace只属于其Tenant。

### 变更

创建Namespace先校验角色和Tenant active，在唯一(tenantId,name)约束下写入。归档Namespace禁止新上传/Build，已有Workspace继续运行；不物理删除其Package。成员移除/角色变化递增权限版本，新的写命令即时拒绝旧版本；已接受业务Operation按原授权和当前停用策略处理，不靠前端隐藏按钮。

## 3. F03/F06：目录、版本与引用

管理员准入Runtime/WebUI须验证不可变digest、平台、契约/Package格式/数据兼容；批准资源套餐必须有Fabric能力与实例采购策略；批准产品价格须给版本、币种、金额、有效期和退款政策。

approved→deprecated：停止新选择，不停止已有Workspace；approved/deprecated→revoked：禁止新Build/部署，已有资源是否强制停止必须独立安全操作，不由目录编辑静默执行。不能修改现有版本digest；变更创建新版本。

Package active→archived：从默认列表隐藏并禁止新上传/Build；历史详情和Build证据保留。Capability ready→deprecated→deleting→deleted；deleting前同事务验证所有活跃claims，冻结新增引用。物理镜像删除另需管理员受限命令及repository+digest共享引用检查。失败保留deleting与明确原因，不恢复可选且继续删除。

## 4. F04/F05：上传、构建、注册

```text
UI→Capability：创建Package/后续版本上传会话
UI→Storage：授权对象直传
UI→Capability：complete；服务端验证精确对象摘要
Capability：PackageVersion=uploaded
UI→Build：createBuild(packageVersionId,webuiVersionId)
Build：固定Runtime策略/digests，持久Job+Operation，取得输入引用claim
Build worker：validating→building→pushing→registering
Build同事务：输出事实 + build.artifact_confirmed.v1 Outbox
Capability Inbox同事务：去重 + ready CapabilityVersion
Build读回版本ID与digest：succeeded
UI查询Operation/Build：展示“可部署”，链接新版本
```

每个同步断点都有持久身份：UploadSession、PackageVersion、BuildJob、Operation、CapabilityVersion。客户端不靠固定延时猜后端已创建ID。再次构建新WebUI/新Runtime组合创建新Job；retryOf保留原失败记录。详细安全/推送读回见05。

## 5. F07：报价与准入（尚不扣钱）

输入为选定CapabilityVersion、computePlanId、storagePlanId、model配置；Tenant/钱包/provider由认证和实例目录确定。

Workspace先向Capability取得可部署事实，检查Runtime/WebUI/模型和数据能力；Fabric preflight返回确切provider/profile、能力与容量观察；Gateway返回余额和钱包主体授权事实；Catalog生成不可变Quote（产品价格版本、套餐版本、币种/总额、周期、到期时间、退款政策与必要限制）。

- Quote不是容量预留，也不是扣费；提交时必须重新确认容量和目录准入。
- 余额不足只显示充值/联系管理员，不开通资源；余额预览不能当扣款成功证据。
- 报价expired、版本撤销、模型不兼容、provider不同、资源能力不支持时明确失败；UI保留选择、要求重新报价。
- 购买请求只接受quoteId+用户确认值/名称，不接受客户传来的价格/外部资源ID。Catalog通过AcceptQuote(quoteId,operationId,inputDigest)事务性绑定唯一操作；Workspace仅保存返回的原快照，不写Catalog quote状态。对已接受quote，Workspace保存原快照；后续目录调价不能修改订阅事实。

## 6. F08：客户一次部署，内部不同事实仍分权

admission阶段先由Runtime Control Reserve持久生成runtimeInstanceId但不启动实例，随后Key/Secret引用绑定该确切身份，消除“部署需要Key、Key又需要尚未生成Runtime身份”的环形依赖。

客户一键提交并不等于数据库一笔跨服务事务。新路径不复用旧resource_only的终点条件。

| 顺序/阶段 | Owner/动作 | 进入下一阶段所需证据 | 不确定时 |
|---|---|---|---|
| admission | Workspace验证会话、Tenant、Quote、版本/模型、provider能力；保留Capability claim | 原报价与版本快照、claim、唯一workspace/operation | 不扣钱，报告准入失败 |
| debit | Gateway Integration对Tenant钱包主体执行批准原单扣款 | 原Code/账户/金额/状态精确读回；保存不可变关联 | 原单读回，禁止新扣款 |
| key | Gateway创建/取得本Workspace专属Key，交付Secret引用 | exact Key身份、允许模型/group/预算、Secret版本；不含明文持久化 | 原Key读回，不盲建第二Key |
| compute | Fabric依批准预付套餐获取计算 | 资源确属本operation/Workspace，provider身份和实际配置 | 查原provider动作，不重复采购 |
| storage | Fabric创建/绑定批准存储 | 精确volume/大小/生命周期与owner读回 | 不用“未看到”推断可重建 |
| attachment | Fabric挂载 | 精确compute-volume绑定、可读写检查 | 保留事实继续读回 |
| runtime | Runtime Control创建Deployment/Runtime；Fabric部署冻结镜像/Secret/config/data | exact digest、环境、资源绑定、健康就绪、凭据可用 | 不显示active；保持可恢复操作 |
| activation | Workspace选中Deployment，Fabric按generation确认对外路由 | 唯一选中部署、访问URL、鉴权/应用探针读回一致 | 不放开入口，不重复收费 |
| receipt | Ledger保存购买/部署证据并精确读回 | 原单与workspace、quote/digest/provider/readiness关联完整 | 页面显示证据确认中，不宣称最终完成 |
| succeeded | Workspace=active，Operation=succeeded | 上述全部事实存在且仍满足交付 | 客户能打开实际应用 |

### 确定失败与释放

- debit之前失败：无购买，释放引用claim，Workspace failed。
- debit被明确拒绝：不创建Key/CVM/CBS，UI显示原错误。
- debit已确认且后续可恢复：保持原Operation继续，不重新扣款；用户关浏览器不影响。
- 确定无法交付且进入已定义closeout：持久释放意图，按Runtime→Secret→挂载→存储/计算的实际依赖次序执行；每步记录确切absence，Ledger记录closeout，才按原单发起允许退款。
- 任一资源/扣费unknown：不自动宣布failed/资源已删/已退款。原Operation awaiting_confirmation或needs_attention，退款单独状态。
- Key归Gateway；删除注入物不等于撤销原Key。保留现行Gateway Key留存义务；新建Key是否撤销必须明确Key操作与回读，不把撤销当退款前置的隐含规则。

## 7. F09：详情、访问与配置更新

列表/详情聚合Workspace业务状态、当前Deployment、Runtime/Fabric观测、Gateway余额/用量；每个来源保留observedAt与不可用原因，不用其他来源替代事实。

“打开”仅在当前绑定、授权与实时ready满足时返回accessUrl；服务端仍逐请求鉴权。应用使用自己的origin/cookies，平台session/service token不得传入应用。404/502/静态资产/SSE断流按实际错误呈现，不能用SPA首页替代资源响应。

模型配置更新只接受契约列出的模型选择字段与expectedConfigurationVersion；Runtime Control写配置版本意图，Fabric/Runtime通过声明接口reload并读回，CAS成功后显示新值。失败保留最后确认配置，不在UI乐观显示未生效模型。任意应用业务配置不进入Cloud数据库，应用数据/完整config仍由Runtime持久卷和发布者schema拥有。

## 8. F10：版本更新/回滚

1. 用户选择同Tenant可访问版本或官方版本；现Workspace generation/currentAgentDeploymentId作为前置。
2. 验证数据兼容、镜像平台、资源容量、模型与Secret契约；不可逆数据迁移必须已有发布者批准的迁移/备份与恢复规则，否则准入拒绝。
3. 取得新版本claim并持久新Deployment；保留旧active指针和原购买事实。
4. Runtime Control创建新实例或按发布者支持的停止旧实例后替换路径执行；同一可写数据卷不得同时被两个不支持并发的实例挂载写入。
5. Workspace分配execution_epoch后，先调用FenceRouteEpoch并确认provider条件版本CAS更新epoch，再允许该epoch进行Activate/Rollback。首次路由须使用有证据的requireAbsent前置，不用空串表示任意版本；其余使用Fence返回的精确providerRevision。未知旧switch先按原id读回，不抢占。Fabric按workspace generation/epoch防旧worker竞争，验证新实例真实readiness后切换路由；Workspace在明确切换读回后更新currentAgentDeploymentId，旧Deployment superseded。
6. 失败且旧数据仍兼容时进入rolling_back，重新验证旧实例/路由，确认后rolled_back；不能仅改DB指针当回滚成功。
7. 旧Runtime的非活动保留至多1天是原讨论要求；执行清理前必须读回非选中、无数据/恢复义务，再删除运行资源并释放claim。超时不能强删仍活动实例。
8. 更新镜像不再次扣Workspace购买费；若资源调整必要先走独立F11报价确认。新Runtime发布只显示可更新提示，不自动替换。

## 9. F11：升级与下一期降配（D17已确认）

完整规则唯一Owner为13_plan_change_policy.md及固定typed政策。Catalog计算报价；Workspace拥有PlanChange与订阅原单；Gateway执行补差/退款；Fabric/Runtime负责实际资源和应用；Ledger持证。

**立即升级链**：准入当前周期/原计划版本→固定T/S/E及旧新月价报价→Workspace CAS接受PlanChange→绑定Quote→原单一次扣补差（0金额不调Gateway）→Fabric执行确切资源计划→Runtime/挂载/文件系统实际验证→Workspace CAS写新active套餐及appliedAt、原E不变→Ledger回执。

**降配预约链**：检查允许的downward transition和未来目标价→存scheduled PlanChange及plannedEffectiveAt=原E→当前周期不动资源/不退款→既有续费worker在账期边界使用该唯一计划和有效consent→只按目标下一期价格扣一次→不早于E执行变更→真实资源/运行确认后applied。无付款授权进入awaiting_payment，不自动开续费。

- 两种流程的generic Operation受理/完成与PlanChange业务状态分别展示。保存预约不是资源已降配。
- 报价使用实际已付周期毫秒长度和最后一步微美元ceil，不机械使用720小时；重试沿原Quote基准不重新滚动扣款。
- 不可比/混合变更、原盘缩容、不满足Runtime最小资源或provider不可安全执行的目标明确拒绝，不假装有另一套配置完成。
- 已有未完成变更或计划需先显式取消可取消计划，新命令不能静默覆盖；下期资金或资源动作已开始后不能简单取消。
- 已确认失败有明确原单补偿和资源事实；unknown先读回。不可逆扩容不假缩容/删数据，业务计划未完整交付不标applied。
- 正常删除升级后的Workspace时，补差款按其原覆盖区间结算，与基础订单退款分开；不能把一笔半月补差再次当作整月款项。
- 下期降配执行失败不得无提示按旧高价续费；新期款项按有证据的失败收尾处理。续费、删除、取消与边界worker都受同一Workspace/周期CAS约束。

对应API/表/消息及数值反例在03/02/13和cross-domain测试中同步，不再把未定义JSON当已批准政策。

## 10. F12：续费、到期与恢复

新建/续费产品本期周期只允许periodMonths=1，避免多月预付与720小时月退款政策产生未定义组合；旧订阅历史按原正整数周期保留。

续费以workspaceId+原periodStart/paidThrough+用户接受订阅政策定位唯一义务；人工点击与worker必须命中同一业务唯一键。不能以新HTTP key创造第二个同周期扣费。

续费周期延续现有owner：newWorkspaceRenewalOperation从原paidThrough按billingAnchorDay计算下一账期，而不是max(now,paidThrough)；now已越过该下一账期终点时拒绝该续费义务，不偷偷改为购买新周期。确认页必须展示真实起止和剩余可用时间，不能承诺到期后任意时刻恢复都赠送完整新月。

Gateway确切扣费确认后，Fabric执行必要provider续期并读回；Workspace更新新的expiresAt、周期和购买/续费receipt。若provider续期unknown，业务状态等待确认，不先把客户日期延长成已完成。

自动续费必须有显式renewalMode/consent快照，关闭仅影响尚未接受的新周期；不得因用户退出登录撤销已经确认的财务义务。迁移原订阅模式和授权，不统一改为手动。

余额不足：不发起采购/续期，显示到期时间和补款入口；到期时按owner计费边界停止未付款运行，保存suspended及原因。充值不自动宣称恢复；客户或明确授权的自动续费策略重新提交同一周期义务，确认资源仍可用后恢复。

不能承诺固定CBS可恢复期：只有实际资源仍在且授权可用时才恢复。已销毁则告知必须新建，不把Tenant十五天恢复当数据恢复承诺。

## 11. F13：删除与退款两条可观察结果

客户确认“删除运行环境和资源；持久数据可能不可恢复；Agent镜像/Package/Build历史保留”→Workspace持久delete Operation并禁止新变更→按真实绑定逐阶段删除Runtime、注入Secret、挂载、存储、计算/公开访问绑定→每步权威absence→Ledger删除receipt精确读回→Workspace deleted。

删除前通过getSubscription展示该环境原已接受退款/保留条款；quoted来源可链接acceptedQuoteId，legacy_import从legacyPurchaseId及原义务快照展示，不临时重报价。

退款是独立Operation：原单必须已确认、未超额退款、钱包目标来自原单、确切删除证据已完成。删除完成但退款unknown时，UI显示“环境已删除，退款确认中”，不能合成“全部成功”。

### 保留的现行退款政策，不新造比例

迁移起点packages/contracts/go/workspace_delete.go:91/650定义workspace-delete-refund-v1：

```text
usedHours = ceil((workspaceDeletedAt - paidPeriodResourceFulfilledAt) / 1 hour)
refundHours = max(720 - usedHours, 0)
refundUSDMicros = floor(originalPlatformChargeUSDMicros * refundHours / 720)
```

原单金额>0、结束时间严格晚于开始时间；退款不得超过原单剩余可退金额；计算使用不溢出的整数乘除。当前付费周期是续费支付的，原单绑定该续费而不是永远绑定首次购买。此平台退款不冒充腾讯实际退款；供应商退款需要自己的权威证据。新产品若批准新政策必须新版本，历史沿用原policyVersion。

Key删除由F14独立授权；不为了删Workspace擅自撤销用户保留的Gateway Key。普通CI不能执行上述真实销毁/退款。

## 12. F14：钱包、Key与用量

所有可花费余额/Key/Token用量由Gateway Integration通过Sub2API读回。Cloud只保存交易操作身份、授权、观察结果和Ledger关联，不对余额加减维护第二钱包。

Key创建/reveal单次返回明文且no-store；之后列表只指纹/模型组/状态。创建后的response丢失先查原操作，再显式reveal，不能盲建第二Key。撤销是独立幂等操作，必须读回Gateway失效。

管理员充值/退款必须有平台权限、目标wallet主体、金额和原因，使用一次性原单身份；禁止将“余额增加了”作为本次调整成功证据。用户的用量页面区分Token消费与Workspace月费，不合并为一条无法追溯的数字。

## 13. F15：Tenant生命周期

active→suspended：CloudIdentity先撤销新交互写授权，持久Tenant Operation，再让Workspace owner按原Tenant停用operation对旗下Workspace创建受限暂停子操作。每个子操作独立读取Fabric/Runtime暂停结果，保留原付费到期日和资源身份，不能仅改Tenant字段声称应用停了。

suspended→active是独立reenableTenant，不检查删除恢复窗口，也不先删除任何Workspace。恢复访问授权后，Workspace owner只恢复被此次Tenant停用操作暂停、仍在原已付周期、确切原资源存在的工作区；其他原因停用、到期、已删除或身份不确定的对象只给明确skip/error，不续费、不重购、不重建。Tenant权限恢复和各应用复机分开显示，通过getTenantLifecycleOperation可读每个子结果。

active/suspended→deleting：平台管理员确认影响清单；冻结新购/Build/部署，枚举Workspace并逐个创建F13删除操作。资产进入平台管理员私有托管，保留原tenantId/owner provenance，不变成Public/official。任何未完成子操作都保留在Tenant Operation，不先标deleted。

全部子操作和权限关闭证据完成→deleted，记录deletedAt及restoreUntil=deletedAt+15天。恢复只重启Tenant/成员/保留资产的授权，不恢复已删除CVM/CBS，不自动退款/采购/创建Workspace。恢复期间已发出的删除不能反向假取消，必须先得到确定子操作结果。

钱包本身仍由Gateway拥有；Tenant删除不清空或删除Sub2API账户，也不自动将全部余额转账。只把各Workspace符合原单政策的退款退回原钱包；其他提现/关闭钱包属于Gateway显式功能，不在Cloud杜撰。

## 14. F16/F17：迁移与运维

按09迁移现有账户、资源-only/retained full Launch、已部署应用、未决扣费/退款/删除义务；旧schema/provider IDs/原Code/receipt身份不能重造。Instance运行变更另有审批和不可变回执。

管理员操作页显示精确stage、observationResult、lastObservedAt、errorCode、requestId、关联资源/原单/脱敏证据；允许动作由owner根据状态授权。只读列表不带原始Secret/provider payload。管理员“重试”只能继续同一义务或创建明确新意图，不能强制改为成功。

## 15. 每条链共同验收向量

各F必须至少覆盖：正常成功；确定拒绝；请求重复；请求同key不同body；非法/跨Tenant访问；浏览器刷新；owner重启；下游已执行但响应丢失；乱序/重复事件；缺失必要能力。涉及钱/资源还覆盖unknown不反向、原单不变、删除证据过期/身份错误、余额变化非证明。

字段级断言与接口operationId/页面/表对应见10的机器可校验矩阵。静态规格校验不能替代上述实际集成/浏览器/Instance验收。

## 16. 工程闭环的统一验收口径

每条链必须同时回答下面九项。任何一格只能写“以后由服务决定/某JSON/某插件处理”，该链就不算已定义。

| 项 | 必须写清楚的事实 |
|---|---|
| 输入 | 客户/管理员哪个页面、哪个operationId、哪些字段、哪个已批准政策 |
| 权限 | 哪个个人/服务身份、action/resource/audience、交互授权还是已接受义务grant |
| 接受点 | 哪个Owner本地事务存请求hash/Operation/快照，何时向浏览器返回身份 |
| 下游命令 | 实际proto service.method及完整typed输入，不只是阶段名称 |
| 写入面 | 每一步只写自己的哪些表；跨域引用只能通过Owner |
| 证据 | 什么owner读回/receipt证明这一步完成，哪些事实不能互相代替 |
| 输出 | 客户得到哪个DTO字段/按钮/状态；未来版本不能反改本次输入 |
| 拒绝/未知 | 明确拒绝、已执行丢响应、晚到worker分别怎样读回/重放/停止 |
| 终点 | 成功条件或有证据的失败收尾，原单、资源、数据义务无丢失 |

`contracts/domain_flows.json`把这些阶段绑定到实际RPC/REST操作和Owner表；检查器以实际协议描述符、SQL和DTO验证引用，并用跨层数据样例测试枚举/金额/ID/版本/来源语义。它是本文的可验证索引，不是新的工作流执行引擎。

## 17. 实际Domain协议逐链索引

> 由checks/render_domain_flows.py核对实际proto生成；输入/输出类型是现有协议中的真实消息，不是另一套伪代码。每条写入前都执行公共授权与接收Owner资源/状态检查。

### F01 个人身份、Tenant和成员

入口：`getLoginContext`, `login`, `getSession`, `inviteMember`, `updateMemberRole`, `removeMember`

| 索引 | 触发条件 / Caller→Owner / RPC | 输入→输出 | 接收Owner写入 | 完成证据 / 不确定处理 |
|---:|---|---|---|---|
| 1 | 按当前入口/阶段与06/13状态机前置执行；不是无条件调用；bff→tenant / `TenantProductService.Login` | `LoginRpcRequest` → `Session` | tenant.sessions | Gateway认证主体与Cloud成员关系确定，session无密码持久化；登录失败统一提示；不创建新Gateway钱包 |
| 2 | 按当前入口/阶段与06/13状态机前置执行；不是无条件调用；receiving_owner→tenant / `CloudIdentityAuthorization.AuthorizeAction` | `AuthorizationRequest` → `AuthorizationDecision` | 只读/由原Owner管理 | session/grant/action/resource/audience/权限版本确切一致；deny不改用管理员；unknown不发后续副作用 |
| 3 | 按当前入口/阶段与06/13状态机前置执行；不是无条件调用；bff→tenant / `TenantProductService.UpdateMemberRole` | `UpdateMemberRoleRpcRequest` → `Member` | tenant.tenant_members, tenant.audit_events | 权限版本变化及最后owner保护；旧版本/越权拒绝，不只隐藏按钮 |

**终点**：个人登录与账单主体分离；成员只能做被授权动作

### F02 分组与私有/官方可见性

入口：`listNamespaces`, `createNamespace`, `archiveNamespace`, `listPackages`

| 索引 | 触发条件 / Caller→Owner / RPC | 输入→输出 | 接收Owner写入 | 完成证据 / 不确定处理 |
|---:|---|---|---|---|
| 1 | 按当前入口/阶段与06/13状态机前置执行；不是无条件调用；capability→tenant / `CloudIdentityAuthorization.AuthorizeAction` | `AuthorizationRequest` → `AuthorizationDecision` | 只读/由原Owner管理 | session/grant/action/resource/audience/权限版本确切一致；deny不改用管理员；unknown不发后续副作用 |
| 2 | 按当前入口/阶段与06/13状态机前置执行；不是无条件调用；bff→capability / `CapabilityProductService.CreateNamespace` | `CreateNamespaceRpcRequest` → `Namespace` | capability.namespaces | tenant+name唯一、正确权限和状态；冲突不创建第二组；不跨Tenant读写 |

**终点**：客户分组可见性正确，归档不删除制品和历史

### F03 Publisher准入与资源价格目录

入口：`createPublisherNamespace`, `registerRuntimeVersion`, `registerWebuiVersion`, `createComputePlan`, `createPricePolicyVersion`

| 索引 | 触发条件 / Caller→Owner / RPC | 输入→输出 | 接收Owner写入 | 完成证据 / 不确定处理 |
|---:|---|---|---|---|
| 1 | 按当前入口/阶段与06/13状态机前置执行；不是无条件调用；capability→tenant / `CloudIdentityAuthorization.AuthorizeAction` | `AuthorizationRequest` → `AuthorizationDecision` | 只读/由原Owner管理 | session/grant/action/resource/audience/权限版本确切一致；deny不改用管理员；unknown不发后续副作用 |
| 2 | 按当前入口/阶段与06/13状态机前置执行；不是无条件调用；bff→capability / `CapabilityProductService.CreatePublisherNamespace` | `CreatePublisherNamespaceRpcRequest` → `PublisherNamespace` | capability.publisher_namespaces | 官方/第三方种类与Registry prefix确认；错误prefix/归属拒绝，不放进默认官方空间 |
| 3 | 按当前入口/阶段与06/13状态机前置执行；不是无条件调用；bff→runtime_control / `RuntimeControlProductService.RegisterRuntimeVersion` | `RegisterRuntimeVersionRpcRequest` → `RuntimeVersion` | runtime_control.runtime_releases | 完整PublisherContract schema+canonical Go validator+Registry读回通过；缺repository/platform/完整revision拒绝 |
| 4 | 按当前入口/阶段与06/13状态机前置执行；不是无条件调用；bff→resource_catalog / `ResourceCatalogProductService.CreatePricePolicyVersion` | `CreatePricePolicyVersionRpcRequest` → `PricePolicyVersion` | resource_catalog.price_policy_versions | 套餐组合/单月/明确政策事实，pending调整策略不可报价；不补空JSON或猜默认金额 |

**终点**：管理员提供实际可执行选项，客户不任意指定Runtime镜像

### F04 上传与确认原字节

入口：`createPackage`, `createUpload`, `createUploadPart`, `completeUpload`

| 索引 | 触发条件 / Caller→Owner / RPC | 输入→输出 | 接收Owner写入 | 完成证据 / 不确定处理 |
|---:|---|---|---|---|
| 1 | 按当前入口/阶段与06/13状态机前置执行；不是无条件调用；capability→tenant / `CloudIdentityAuthorization.AuthorizeAction` | `AuthorizationRequest` → `AuthorizationDecision` | 只读/由原Owner管理 | session/grant/action/resource/audience/权限版本确切一致；deny不改用管理员；unknown不发后续副作用 |
| 2 | 按当前入口/阶段与06/13状态机前置执行；不是无条件调用；bff→capability / `CapabilityProductService.CreateUpload` | `CreateUploadRpcRequest` → `UploadSession` | capability.package_versions, capability.upload_sessions | 固定包版本及uploadId/受限对象地址；同键返回同身份；过期续签同对象 |
| 3 | 按当前入口/阶段与06/13状态机前置执行；不是无条件调用；bff→capability / `CapabilityProductService.CompleteUpload` | `CompleteUploadRpcRequest` → `Operation` | capability.upload_chunks, capability.package_versions, capability.operations | Storage精确对象version、字节sha256和长度通过；校验失败不得标uploaded，不信ETag=SHA256 |

**终点**：PackageVersion uploaded；此时尚未构建，必须显式createBuild

字节直接传Storage签署地址，不通过BFF代理大文件

### F05 构建输入保护、输出与注册

入口：`createBuild`, `getBuild`, `listBuildLogs`, `retryBuild`

| 索引 | 触发条件 / Caller→Owner / RPC | 输入→输出 | 接收Owner写入 | 完成证据 / 不确定处理 |
|---:|---|---|---|---|
| 1 | 按当前入口/阶段与06/13状态机前置执行；不是无条件调用；build→capability / `CapabilityCoordination.ResolveBuildInput` | `BuildInputRequest` → `BuildInputSnapshot` | 只读/由原Owner管理 | Package/WebUI/Runtime策略和完整发布描述冻结；读失败还未建业务副作用，不能选latest替代 |
| 2 | 按当前入口/阶段与06/13状态机前置执行；不是无条件调用；bff→build / `BuildProductService.CreateBuild` | `CreateBuildRpcRequest` → `BuildJob` | build.build_jobs, build.operations | Job queued+snapshot+Operation+幂等同事务；响应丢失取回原身份，不建第二任务 |
| 3 | 按当前入口/阶段与06/13状态机前置执行；不是无条件调用；build→capability / `CapabilityCoordination.AcquireReference` | `ReferenceClaimRequest` → `ReferenceClaim` | capability.reference_claims | PackageVersion/RuntimeVersion/WebuiVersion三target均绑定本job；任一拒绝则不读字节/不build；保留已取得claim待确定收尾 |
| 4 | 按当前入口/阶段与06/13状态机前置执行；不是无条件调用；build→capability / `CapabilityCoordination.BindReference` | `BindReferenceRequest` → `ReferenceClaim` | capability.reference_claims | Build本域保存claim IDs后的OwnerCommitEvidence可读回；Bind丢响应查原claim，不自动TTL释放 |
| 5 | 按当前入口/阶段与06/13状态机前置执行；不是无条件调用；capability→build / `BuildCoordination.ReadArtifact` | `ReadBuildArtifactRequest` → `BuildArtifactReadback` | 只读/由原Owner管理 | repository+digest+platform+DeploymentDescriptor与远端制品相同；不是push接受就注册，unknown继续读原产物 |
| 6 | 按当前入口/阶段与06/13状态机前置执行；不是无条件调用；build→capability / `DomainInbox.Deliver` | `DeliverEventRequest` → `InboxAck` | capability.capability_versions | Inbox事务去重后唯一版本，事件与Build真实readback一致；同eventId重复ACK，乱序不覆盖新事实 |
| 7 | 按当前入口/阶段与06/13状态机前置执行；不是无条件调用；build→ledger / `LedgerCoordination.AppendReceipt` | `AppendReceiptRequest` → `Receipt` | ledger.receipts | 返回receipt身份，另ReadReceiptByReference核对原输入；同idempotency key读回，不填假Workspace或重写receipt |

**终点**：唯一可部署CapabilityVersion；完整Job历史和输入来源保留

worker按固定recipe调用BuildKit与Registry exporter；不是新的构建框架

### F06 目录归档与引用释放

入口：`archivePackage`, `deleteCapabilityVersion`

| 索引 | 触发条件 / Caller→Owner / RPC | 输入→输出 | 接收Owner写入 | 完成证据 / 不确定处理 |
|---:|---|---|---|---|
| 1 | 按当前入口/阶段与06/13状态机前置执行；不是无条件调用；capability→tenant / `CloudIdentityAuthorization.AuthorizeAction` | `AuthorizationRequest` → `AuthorizationDecision` | 只读/由原Owner管理 | session/grant/action/resource/audience/权限版本确切一致；deny不改用管理员；unknown不发后续副作用 |
| 2 | 按当前入口/阶段与06/13状态机前置执行；不是无条件调用；bff→capability / `CapabilityProductService.DeleteCapabilityVersion` | `DeleteCapabilityVersionRpcRequest` → `Operation` | capability.capability_versions, capability.reference_claims | 本域锁定版本并确认无活跃claim；只做目录下架；仍使用返回冲突；不建议删Workspace绕过 |
| 3 | 按当前入口/阶段与06/13状态机前置执行；不是无条件调用；consumer_owner→capability / `CapabilityCoordination.ReleaseReference` | `ReleaseReferenceRequest` → `ReferenceClaim` | capability.reference_claims | 原claim owner真实usage/终态证据；未知不释放，历史元数据不级联 |

**终点**：显示“已从目录移除”；物理镜像清除另属授权管理员操作

### F07 选择Agent/套餐/模型并取得准确报价

入口：`createQuote`, `getQuote`

| 索引 | 触发条件 / Caller→Owner / RPC | 输入→输出 | 接收Owner写入 | 完成证据 / 不确定处理 |
|---:|---|---|---|---|
| 1 | 按当前入口/阶段与06/13状态机前置执行；不是无条件调用；workspace→tenant / `CloudIdentityAuthorization.AuthorizeAction` | `AuthorizationRequest` → `AuthorizationDecision` | 只读/由原Owner管理 | session/grant/action/resource/audience/权限版本确切一致；deny不改用管理员；unknown不发后续副作用 |
| 2 | 按当前入口/阶段与06/13状态机前置执行；不是无条件调用；resource_catalog→workspace / `WorkspaceAdmission.CheckAdmission` | `AdmissionRequest` → `AdmissionResult` | 只读/由原Owner管理 | 当前版本、模型、作用域与原Workspace义务确认；quote并不等于容量预留，提交时复查 |
| 3 | 按当前入口/阶段与06/13状态机前置执行；不是无条件调用；workspace→fabric / `FabricCoordination.AdmitResources` | `ResourceAdmissionRequest` → `AdmissionResult` | 只读/由原Owner管理 | provider能力、实际资源/route CAS能力准入；不能静默换provider或降能力 |
| 4 | 按当前入口/阶段与06/13状态机前置执行；不是无条件调用；bff→resource_catalog / `ResourceCatalogProductService.CreateQuote` | `CreateQuoteRpcRequest` → `Quote` | resource_catalog.quotes, resource_catalog.quote_items | purpose/kind/金额符号与DB完全同词；总额=sum正项-credit；pending/缺政策拒绝，不能零价兜底 |

**终点**：客户能看到同一quote的金额、单月周期、有效期和数据规则

### F08 购买到实际可用的部署

入口：`createWorkspace`, `getOperation`, `getWorkspace`

| 索引 | 触发条件 / Caller→Owner / RPC | 输入→输出 | 接收Owner写入 | 完成证据 / 不确定处理 |
|---:|---|---|---|---|
| 1 | 按当前入口/阶段与06/13状态机前置执行；不是无条件调用；workspace→tenant / `CloudIdentityAuthorization.AuthorizeAction` | `AuthorizationRequest` → `AuthorizationDecision` | 只读/由原Owner管理 | session/grant/action/resource/audience/权限版本确切一致；deny不改用管理员；unknown不发后续副作用 |
| 2 | 按当前入口/阶段与06/13状态机前置执行；不是无条件调用；workspace→resource_catalog / `CatalogCoordination.AcceptQuote` | `AcceptQuoteRequest` → `QuoteAcceptance` | resource_catalog.quotes | quoteID/inputDigest唯一绑定原operation；冲突拒绝，不能重报价后续跑原单 |
| 3 | 按当前入口/阶段与06/13状态机前置执行；不是无条件调用；workspace→serve / `ServeAgentCoordination.Reserve` | `RuntimeReservationCommand` → `RuntimeReservation` | serve.agent_runtime_instances | 预留稳定runtimeInstanceId而不启动；不因Key依赖Runtime ID产生循环 |
| 4 | 按当前入口/阶段与06/13状态机前置执行；不是无条件调用；workspace→tenant / `CloudIdentityAuthorization.IssueAcceptedOperationGrant` | `AcceptedOperationGrantRequest` → `AcceptedOperationGrant` | tenant.accepted_operation_grants | 原Owner commit读回，有限actions/resource/period；不能伪造commit字符串或扩大到新Workspace |
| 5 | 按当前入口/阶段与06/13状态机前置执行；不是无条件调用；workspace→gateway / `GatewayCoordination.Debit` | `WalletDebitCommand` → `WalletOperation` | gateway.wallet_operations | 精确账户/Code/金额原单confirmed；unknown只ReadWalletAction，绝不重复扣费 |
| 6 | 按当前入口/阶段与06/13状态机前置执行；不是无条件调用；workspace→gateway / `GatewayCoordination.CreateManagedKey` | `ManagedKeyCommand` → `ManagedKeyBinding` | gateway.key_bindings | 原实例与允许模型/Key引用/版本一致；不盲建第二Key，不在DB/事件存明文 |
| 7 | 按当前入口/阶段与06/13状态机前置执行；不是无条件调用；workspace→fabric / `FabricCoordination.EnsureResources` | `EnsureResourcesCommand` → `Operation` | fabric.resources, fabric.resource_sets, fabric.attachments | 批准预付资源与Workspace/原请求exact匹配；unknown查原provider动作，不重购 |
| 8 | 按当前入口/阶段与06/13状态机前置执行；不是无条件调用；runtime_control→fabric / `FabricCoordination.BindSecret` | `SecretBindingCommand` → `SecretBindingReadback` | fabric.secret_bindings | 完整发布描述指定的Secret引用实际注入；不把Gateway Key当任意环境变量公开 |
| 9 | 按当前入口/阶段与06/13状态机前置执行；不是无条件调用；workspace→serve / `ServeAgentCoordination.Deploy` | `RuntimeDeployCommand` → `RuntimeReadback` | serve.agent_runtime_actions | 完整DeploymentDescriptor送执行层并实际ready；非就绪不开放入口 |
| 10 | 按当前入口/阶段与06/13状态机前置执行；不是无条件调用；serve→serve / `ServeAccessControl.FenceRouteEpoch` | `FenceRouteEpochCommand` → `RouteReadback` | serve.access_bindings, serve.access_switches | provider conditional revision确认新epoch；未知旧switch先读回，不抢占 |
| 11 | 按当前入口/阶段与06/13状态机前置执行；不是无条件调用；serve→serve / `ServeAccessControl.ActivateRoute` | `RouteActivateCommand` → `RouteReadback` | serve.access_bindings, serve.access_switches | epoch/revision/target精确，provider实际路由确认；旧epoch/旧revision拒绝，丢响应ObserveRoute |
| 12 | 按当前入口/阶段与06/13状态机前置执行；不是无条件调用；workspace→ledger / `LedgerCoordination.AppendReceipt` | `AppendReceiptRequest` → `Receipt` | ledger.receipts | 返回receipt身份，另ReadReceiptByReference核对原输入；同idempotency key读回，不填假Workspace或重写receipt |

**终点**：Serve本域CAS currentAgentDeployment/访问generation后，receipt核对；客户可打开当前应用

Workspace.currentAgentDeploymentId是选中业务权威，Fabric是真实路由权威；没有跨库原子提交幻觉

### F09 使用、模型配置与应用登录

入口：`getWorkspace`, `getWorkspaceAccess`, `getWorkspaceModels`, `updateWorkspaceModels`, `revealWorkspaceApplicationCredentials`

| 索引 | 触发条件 / Caller→Owner / RPC | 输入→输出 | 接收Owner写入 | 完成证据 / 不确定处理 |
|---:|---|---|---|---|
| 1 | 按当前入口/阶段与06/13状态机前置执行；不是无条件调用；workspace→tenant / `CloudIdentityAuthorization.AuthorizeAction` | `AuthorizationRequest` → `AuthorizationDecision` | 只读/由原Owner管理 | session/grant/action/resource/audience/权限版本确切一致；deny不改用管理员；unknown不发后续副作用 |
| 2 | 按当前入口/阶段与06/13状态机前置执行；不是无条件调用；bff→workspace / `ServeProductService.GetWorkspaceAccess` | `GetWorkspaceAccessRpcRequest` → `WorkspaceAccess` | 只读/由原Owner管理 | 当前部署/访问策略和运行事实一致；按canonical应用登录，不新增SSO |
| 3 | 按当前入口/阶段与06/13状态机前置执行；不是无条件调用；bff→workspace / `WorkspaceProductService.RevealWorkspaceApplicationCredentials` | `RevealWorkspaceApplicationCredentialsRpcRequest` → `WorkspaceApplicationCredentials` | 只读/由原Owner管理 | 所有者权限+当前声明workspace_admin_password+实际ready，只一次性用户名/密码；no-store不缓存；不返回GatewayKey或session_secret |
| 4 | 按当前入口/阶段与06/13状态机前置执行；不是无条件调用；workspace→serve / `ServeAgentCoordination.ReloadModels` | `RuntimeReloadCommand` → `Operation` | serve.agent_runtime_actions | 目标配置版本+selections实际应用；保存成功不等于reload成功 |
| 5 | 按当前入口/阶段与06/13状态机前置执行；不是无条件调用；serve→serve / `ServeRuntimeAdapter.ObserveRuntime` | `RuntimeReadbackRequest` → `RuntimeReadback` | 只读/由原Owner管理 | appliedVersion和选择相同，运行状态真实；unknown显示应用中/待核实，不覆盖已确认配置 |

**终点**：打开真实应用；模型实际生效；应用管理员凭据仅在既有授权reveal路径一次性显示

### F10 更新、切换和回滚

入口：`updateWorkspaceVersion`, `rollbackWorkspace`

| 索引 | 触发条件 / Caller→Owner / RPC | 输入→输出 | 接收Owner写入 | 完成证据 / 不确定处理 |
|---:|---|---|---|---|
| 1 | 按当前入口/阶段与06/13状态机前置执行；不是无条件调用；workspace→tenant / `CloudIdentityAuthorization.AuthorizeAction` | `AuthorizationRequest` → `AuthorizationDecision` | 只读/由原Owner管理 | session/grant/action/resource/audience/权限版本确切一致；deny不改用管理员；unknown不发后续副作用 |
| 2 | 按当前入口/阶段与06/13状态机前置执行；不是无条件调用；workspace→capability / `CapabilityCoordination.ResolvePublisherContract` | `ResolvePublisherContractRequest` → `ResolvedPublisherContract` | 只读/由原Owner管理 | 目标版本完整契约与数据兼容；不支持安全回滚的迁移拒绝，不猜semver |
| 3 | 按当前入口/阶段与06/13状态机前置执行；不是无条件调用；serve→serve / `ServeAccessControl.FenceRouteEpoch` | `FenceRouteEpochCommand` → `RouteReadback` | serve.access_switches, serve.access_bindings | 新epoch先在provider确认；旧未知切换不被强行覆盖 |
| 4 | 按当前入口/阶段与06/13状态机前置执行；不是无条件调用；workspace→serve / `ServeAgentCoordination.Deploy` | `RuntimeDeployCommand` → `RuntimeReadback` | serve.agent_runtime_instances, serve.agent_runtime_actions | 新实例实际验证且不违反可写卷并发限制；保留旧选中版本/原数据义务 |
| 5 | 按当前入口/阶段与06/13状态机前置执行；不是无条件调用；serve→serve / `ServeAccessControl.ActivateRoute` | `RouteActivateCommand` → `RouteReadback` | serve.access_switches, serve.access_bindings | 新target/provider revision确认；丢响应原switch读回 |
| 6 | 新部署明确失败且旧数据/路由允许安全回滚；serve→serve / `ServeAccessControl.RollbackRoute` | `RouteRollbackCommand` → `RouteReadback` | serve.access_switches, serve.access_bindings | 原目标+原switch证据+当前expected generation可核对；不能仅改DB指针称回滚成功 |

**终点**：一个确认选中部署，无重复购买；失败时旧运行结果有实际证据

### F11 立即升级、下期降配与独立原单结算

入口：`resizeWorkspace`, `createQuote`, `listPlanChanges`, `getPlanChange`, `cancelPlanChange`

| 索引 | 触发条件 / Caller→Owner / RPC | 输入→输出 | 接收Owner写入 | 完成证据 / 不确定处理 |
|---:|---|---|---|---|
| 1 | 按当前入口/阶段与06/13状态机前置执行；不是无条件调用；workspace→tenant / `CloudIdentityAuthorization.AuthorizeAction` | `AuthorizationRequest` → `AuthorizationDecision` | 只读/由原Owner管理 | session/grant/action/resource/audience/权限版本确切一致；deny不改用管理员；unknown不发后续副作用 |
| 2 | 按当前入口/阶段与06/13状态机前置执行；不是无条件调用；resource_catalog→workspace / `WorkspacePlanChangeReadback.ReadSubscriptionPlanState` | `ReadSubscriptionPlanStateRequest` → `SubscriptionPlanState` | 只读/由原Owner管理 | 原period/S/E/已接受当前月价/当前计划和资金义务版本固定；已锁定其它未来账单或财务基础变化拒绝，不退旧款改价 |
| 3 | 按当前入口/阶段与06/13状态机前置执行；不是无条件调用；resource_catalog→fabric / `FabricPlanTransitionReadback.ReadApprovedPlanTransition` | `PlanTransitionRequest` → `ApprovedPlanTransition` | 只读/由原Owner管理 | 批准可比转换，固定执行策略/数据/中断能力；mixed/no-op/不支持缩容拒绝，不按SKU名字或价格猜方向 |
| 4 | 按当前入口/阶段与06/13状态机前置执行；不是无条件调用；bff→resource_catalog / `ResourceCatalogProductService.CreateQuote` | `CreateQuoteRpcRequest` → `Quote` | resource_catalog.quotes, resource_catalog.quote_items | 升级仅最后ceil补差，降配当前0且下期目标价单列；原T固定；过期/改变原计划须新quote，不接收客户自报价 |
| 5 | 按当前入口/阶段与06/13状态机前置执行；不是无条件调用；bff→workspace / `WorkspaceProductService.ResizeWorkspace` | `ResizeWorkspaceRpcRequest` → `Operation` | workspace.plan_changes, workspace.operations | CAS写PlanChange与原基础；scheduled不是applied；同幂等键返回原计划，已有未完成计划不覆盖 |
| 6 | 按当前入口/阶段与06/13状态机前置执行；不是无条件调用；workspace→resource_catalog / `CatalogCoordination.AcceptQuote` | `AcceptQuoteRequest` → `QuoteAcceptance` | resource_catalog.quotes | exact quote绑定本PlanChange的初次Operation；不能把其它purpose/计划quote用作新购买 |
| 7 | kind=upgrade_immediate 且upgradeCharge>0且原单未确认；zero跳过；workspace→gateway / `GatewayPlanChangeSettlement.DebitSupplement` | `PlanChangeSupplementChargeCommand` → `WalletOperation` | gateway.wallet_operations | 仅upgrade正补差、原code与固定quote金额一致；0金额不调用；unknown原单读回不新收费 |
| 8 | downgrade_next_period的付款/执行分支，或其取消未付款义务；gateway→workspace / `WorkspacePlanChangeReadback.ReadNextPeriodObligation` | `ReadNextPeriodObligationRequest` → `NextPeriodObligation` | 只读/由原Owner管理 | downgrade到期资金动作绑定唯一workspace/nextPeriod原义务与consent；没有有效付款授权则awaiting_payment，不偷开自动续费 |
| 9 | kind=downgrade_next_period且有效下期付款授权；不得提前降低资源；workspace→gateway / `GatewayPlanChangeSettlement.DebitScheduledPeriod` | `ScheduledPeriodChargeCommand` → `WalletOperation` | gateway.wallet_operations | 仅目标下一期已接受价格，提前付款也不提前减资源；不得先旧价扣再补救；manual未授权不调用 |
| 10 | upgrade资金confirmed/zero；或downgrade已到E且目标期资金confirmed；workspace→fabric / `FabricCoordination.ResizeResources` | `ResizeResourcesCommand` → `Operation` | fabric.resource_actions, fabric.resources | 资金/ZeroFundingEvidence和固定executionPlan，原epoch/目标/资源读回一致；unknown原请求读回，部分不可逆事实独立保留不假缩容 |
| 11 | 按当前入口/阶段与06/13状态机前置执行；不是无条件调用；workspace→serve / `ServePlanChangeControl.RestoreAfterResourceChange` | `RestorePlanChangeRuntimeCommand` → `PlanChangeRuntimeReadback` | serve.agent_runtime_actions | 现有应用实际资源限制/挂载/健康确认；裸资源为owner证明的not_applicable；已有应用不可用不得applied，不让客户skipRuntime |
| 12 | 按当前入口/阶段与06/13状态机前置执行；不是无条件调用；workspace→ledger / `LedgerPlanChangeEvidence.AppendPlanChangeReceipt` | `AppendPlanChangeReceiptRequest` → `Receipt` | ledger.receipts | 本域CAS appliedAt/active计划/原E不变或下期正确周期后精确receipt；已受理/预约成功不能当资源已生效 |
| 13 | 用户显式取消且无新期资金accepted/资源执行；bff→workspace / `WorkspaceProductService.CancelPlanChange` | `CancelPlanChangeRpcRequest` → `Operation` | workspace.plan_changes, workspace.operations | 尚无新期资金accepted/执行时CAS取消；历史不改；付款或资源动作已开始拒绝，不能按低价付完再恢复高配 |
| 14 | 确定失败/fence/资源证据完备；非unknown；原单有未退额；workspace→gateway / `GatewayPlanChangeSettlement.RefundFailure` | `PlanChangeFailureRefundCommand` → `WalletOperation` | gateway.wallet_operations | 确定交付失败+fence+实际资源证据，补偿原补差或原目标期款；unknown不退；部分不可逆成本归平台，不向客户擅收部分交付费 |
| 15 | 原升级已applied，此后Workspace正常删除已确认；workspace→gateway / `GatewayPlanChangeSettlement.RefundSupplementOnDeletion` | `SupplementDeletionRefundCommand` → `WalletOperation` | gateway.wallet_operations | 成功补差的T..E原覆盖区间、正常删除证据及原单未退余额；不用base720；每笔原单分别去重/读回 |

**终点**：升级实际确认后applied且E不变；降配scheduled到原E、下期付款/资源确认后applied；失败/退款/真实资源分开

这些是按kind/状态选择的分支，不是每次依次扣补差、扣下一期并退款。due worker、唯一period obligation/CAS和计划应用都是Workspace本Owner直接事务，不新建自调用RPC或workflow engine。

### F12 续费与到期恢复

入口：`renewWorkspace`, `updateRenewalSettings`, `getSubscription`

| 索引 | 触发条件 / Caller→Owner / RPC | 输入→输出 | 接收Owner写入 | 完成证据 / 不确定处理 |
|---:|---|---|---|---|
| 1 | 按当前入口/阶段与06/13状态机前置执行；不是无条件调用；workspace→tenant / `CloudIdentityAuthorization.AuthorizeAction` | `AuthorizationRequest` → `AuthorizationDecision` | 只读/由原Owner管理 | session/grant/action/resource/audience/权限版本确切一致；deny不改用管理员；unknown不发后续副作用 |
| 2 | 按当前入口/阶段与06/13状态机前置执行；不是无条件调用；tenant→workspace / `WorkspaceAuthorizationReadback.ReadRenewalConsent` | `ReadRenewalConsentRequest` → `RenewalConsentReadback` | 只读/由原Owner管理 | 本周期explicit consent/原grant允许；关闭只阻止新周期，不丢已接受义务 |
| 3 | downgrade_next_period的付款/执行分支，或其取消未付款义务；gateway→workspace / `WorkspacePlanChangeReadback.ReadNextPeriodObligation` | `ReadNextPeriodObligationRequest` → `NextPeriodObligation` | 只读/由原Owner管理 | 确认该期是否唯一绑定scheduled计划及已接受目标价格；不得人工/自动各造一单或按旧价先扣 |
| 4 | kind=downgrade_next_period且有效下期付款授权；不得提前降低资源；workspace→gateway / `GatewayPlanChangeSettlement.DebitScheduledPeriod` | `ScheduledPeriodChargeCommand` → `WalletOperation` | gateway.wallet_operations | 仅当nextPeriodObligation绑定降配时使用目标价一次扣款；无consent则awaiting_payment，不走普通旧价Debit |
| 5 | 该nextPeriod无scheduled PlanChange，仅常规续费分支；workspace→gateway / `GatewayCoordination.Debit` | `WalletDebitCommand` → `WalletOperation` | gateway.wallet_operations | workspace+period业务键唯一原单确认；重复点击/worker命中同一义务 |
| 6 | 无计划常规续期；有计划必须遵照已批准目标executionPlan，不独立先续旧高配；workspace→fabric / `FabricCoordination.RenewResources` | `RenewResourcesCommand` → `Operation` | fabric.resource_actions | 原资源续期读回与原paidThrough续期窗口一致；过期已回收不伪造恢复、不改now重新计期 |
| 7 | 按当前入口/阶段与06/13状态机前置执行；不是无条件调用；workspace→ledger / `LedgerCoordination.AppendReceipt` | `AppendReceiptRequest` → `Receipt` | ledger.receipts | 返回receipt身份，另ReadReceiptByReference核对原输入；同idempotency key读回，不填假Workspace或重写receipt |

**终点**：同周期仅付一次，显示真实新账期和运行恢复；迁移原续费模式

 无计划走常规月费；有scheduled计划转F11目标计划分支，Fabric按已批准executionPlan决定续期/调配顺序。

### F13 删除与原单退款

入口：`deleteWorkspace`, `getWorkspaceDeletion`, `listWorkspaceTransactions`

| 索引 | 触发条件 / Caller→Owner / RPC | 输入→输出 | 接收Owner写入 | 完成证据 / 不确定处理 |
|---:|---|---|---|---|
| 1 | 按当前入口/阶段与06/13状态机前置执行；不是无条件调用；workspace→tenant / `CloudIdentityAuthorization.AuthorizeAction` | `AuthorizationRequest` → `AuthorizationDecision` | 只读/由原Owner管理 | session/grant/action/resource/audience/权限版本确切一致；deny不改用管理员；unknown不发后续副作用 |
| 2 | 按当前入口/阶段与06/13状态机前置执行；不是无条件调用；workspace→serve / `ServeAgentCoordination.Retire` | `RuntimeStopCommand` → `Operation` | serve.agent_runtime_actions | 确切旧Runtime停止/不存在；unknown不继续声称全环境已删 |
| 3 | 按当前入口/阶段与06/13状态机前置执行；不是无条件调用；workspace→fabric / `FabricCoordination.DeleteResources` | `MutateResourcesCommand` → `Operation` | fabric.resource_actions, fabric.resources, fabric.attachments | 原资源/挂载/Secret及公开访问绑定确切absence；不依赖列表没看到推断不存在 |
| 4 | 按当前入口/阶段与06/13状态机前置执行；不是无条件调用；workspace→ledger / `LedgerCoordination.AppendReceipt` | `AppendReceiptRequest` → `Receipt` | ledger.receipts | 返回receipt身份，另ReadReceiptByReference核对原输入；同idempotency key读回，不填假Workspace或重写receipt |
| 5 | 原单可退且相应删除/失败证据完整，不是unknown；workspace→gateway / `GatewayCoordination.Refund` | `WalletRefundCommand` → `WalletOperation` | gateway.wallet_operations | 原单剩余可退金额+删除receipt+原钱包目标；unknown原退款读回，删除与退款分别显示 |
| 6 | 原升级已applied，此后Workspace正常删除已确认；workspace→gateway / `GatewayPlanChangeSettlement.RefundSupplementOnDeletion` | `SupplementDeletionRefundCommand` → `WalletOperation` | gateway.wallet_operations | 本Owner枚举每个已applied补差，按各自T..E覆盖/原单剩余可退额分别结算；不把补差合并进base720；unknown保留该原单未决退款 |

**终点**：环境删除与退款各自可查；不删除Package/Build历史，不泄露Key

 基础单与每笔补差单分别计算/去重/读回，最后只聚合展示，不丢原单。

### F14 钱包、Key与Token记录

入口：`getWallet`, `listUsage`, `createGatewayKey`, `revealGatewayKey`, `revokeGatewayKey`

| 索引 | 触发条件 / Caller→Owner / RPC | 输入→输出 | 接收Owner写入 | 完成证据 / 不确定处理 |
|---:|---|---|---|---|
| 1 | 按当前入口/阶段与06/13状态机前置执行；不是无条件调用；gateway→tenant / `CloudIdentityAuthorization.AuthorizeAction` | `AuthorizationRequest` → `AuthorizationDecision` | 只读/由原Owner管理 | session/grant/action/resource/audience/权限版本确切一致；deny不改用管理员；unknown不发后续副作用 |
| 2 | 按当前入口/阶段与06/13状态机前置执行；不是无条件调用；bff→gateway / `GatewayProductService.GetWallet` | `GetWalletRpcRequest` → `Wallet` | 只读/由原Owner管理 | Sub2API当前钱包事实；不可用不返回0，不复制余额表 |
| 3 | 按当前入口/阶段与06/13状态机前置执行；不是无条件调用；bff→gateway / `GatewayProductService.RevealGatewayKey` | `RevealGatewayKeyRpcRequest` → `GatewayKeySecret` | 只读/由原Owner管理 | 本人或显式grant且非系统托管Key，private/no-store；无权限拒绝；Secret不进入幂等response缓存 |

**终点**：余额/Token费与工作区费各有原始来源；Key明文只一次性内存显示

### F15 暂停/重新启用/删除恢复Tenant

入口：`suspendTenant`, `reenableTenant`, `deleteTenant`, `restoreTenant`, `getTenantLifecycleOperation`

| 索引 | 触发条件 / Caller→Owner / RPC | 输入→输出 | 接收Owner写入 | 完成证据 / 不确定处理 |
|---:|---|---|---|---|
| 1 | 按当前入口/阶段与06/13状态机前置执行；不是无条件调用；tenant→tenant / `CloudIdentityAuthorization.AuthorizeAction` | `AuthorizationRequest` → `AuthorizationDecision` | 只读/由原Owner管理 | session/grant/action/resource/audience/权限版本确切一致；deny不改用管理员；unknown不发后续副作用 |
| 2 | suspendTenant入口；tenant→workspace / `TenantWorkspaceCoordination.SuspendTenantWorkspaces` | `TenantWorkspaceLifecycleCommand` → `TenantWorkspaceLifecycleReadback` | workspace.operations | 每Workspace原Tenant暂停子操作可读回；Tenant权限撤销不冒充资源已暂停 |
| 3 | reenableTenant入口，原停用/已付/原资源存在；tenant→workspace / `TenantWorkspaceCoordination.ResumeTenantWorkspaces` | `ResumeTenantWorkspacesRequest` → `TenantWorkspaceLifecycleReadback` | workspace.operations | 只恢复原暂停、仍已付、原资源存在对象；过期/其他原因/删除对象skip，不续费重购 |
| 4 | deleteTenant入口；tenant→workspace / `TenantWorkspaceCoordination.DeleteTenantWorkspaces` | `TenantWorkspaceLifecycleCommand` → `TenantWorkspaceLifecycleReadback` | workspace.operations | 每子删除证据明确；未完成不把Tenant操作标全部完成 |

**终点**：reenable与删除后15天restore独立；权限和每个应用结果分开显示

### F16 旧资源和旧应用无损迁移

入口：`adoptWorkspace`, `getSubscription`, `getWorkspace`

| 索引 | 触发条件 / Caller→Owner / RPC | 输入→输出 | 接收Owner写入 | 完成证据 / 不确定处理 |
|---:|---|---|---|---|
| 1 | 按当前入口/阶段与06/13状态机前置执行；不是无条件调用；workspace→tenant / `CloudIdentityAuthorization.AuthorizeAction` | `AuthorizationRequest` → `AuthorizationDecision` | 只读/由原Owner管理 | session/grant/action/resource/audience/权限版本确切一致；deny不改用管理员；unknown不发后续副作用 |
| 2 | 按当前入口/阶段与06/13状态机前置执行；不是无条件调用；bff→workspace / `WorkspaceProductService.AdoptWorkspace` | `AdoptWorkspaceRpcRequest` → `Operation` | workspace.workspaces, serve.agent_deployments, workspace.operations | 原资源/订阅/ID沿用，新的Agent契约符合旧资源；不执行购买debit/Ensure新资源，不伪造Quote或Build |

**终点**：历史resource_only可装Agent，已有应用/账期/数据/Key不被迁移暗改

数据Owner迁移按09停写屏障和单writer，不是此命令触发生产迁移

### F17 运维、证据与资格读取

入口：`listAdminOperations`, `reconcileOperation`, `listReceipts`, `listQualifications`

| 索引 | 触发条件 / Caller→Owner / RPC | 输入→输出 | 接收Owner写入 | 完成证据 / 不确定处理 |
|---:|---|---|---|---|
| 1 | 按当前入口/阶段与06/13状态机前置执行；不是无条件调用；receiving_owner→tenant / `CloudIdentityAuthorization.AuthorizeAction` | `AuthorizationRequest` → `AuthorizationDecision` | 只读/由原Owner管理 | session/grant/action/resource/audience/权限版本确切一致；deny不改用管理员；unknown不发后续副作用 |
| 2 | 按当前入口/阶段与06/13状态机前置执行；不是无条件调用；bff→owning_service / `OwnerOperations.Reconcile` | `ReconcileOperationRpcRequest` → `Operation` | 只读/由原Owner管理 | Owner按原operation真实读回和允许动作继续；不是任意setStatus succeeded |
| 3 | 按当前入口/阶段与06/13状态机前置执行；不是无条件调用；owner→ledger / `LedgerCoordination.ReadReceiptByReference` | `GetReceiptByReferenceRequest` → `Receipt` | 只读/由原Owner管理 | 确切receipt/来源/输入output摘要；缺证据保持未验证，不发生产部署 |

**终点**：只读或受限原操作恢复，无新增Instance dispatch接口
