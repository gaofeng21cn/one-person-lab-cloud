# 02 数据库字段、权威与事务边界（2026-09-21 修订）

> **目标可开发规格；未执行数据库迁移。** 替换上一版单库跨域FK、UUID默认值、级联删除、计数双写与重复钱包。外部DTO唯一权威为03，`contracts/schema.sql`是本文件可执行字段投影；`contracts/db_inventory.json`含每表字段、NULL/default、约束、索引、业务功能和来源，供自动核对。

## 1. 数据库与写入身份

| 数据Owner | 独立database/schema | 所属业务服务 |
|---|---|---|
| tenant | opl_tenant / tenant | Gateway Integration内CloudIdentity子模块；不是新增第九个业务服务 |
| capability | opl_capability / capability | Capability |
| build | opl_build / build | Build |
| workspace | opl_workspace / workspace | Workspace |
| runtime_control | opl_runtime_control / runtime_control | Runtime Control |
| fabric | opl_fabric / fabric | Fabric |
| gateway | opl_gateway / gateway | Gateway Integration的外部Gateway适配模块 |
| resource_catalog | opl_resource_catalog / resource_catalog | Resource Catalog |
| ledger | opl_ledger / ledger | Ledger |

- 可共PostgreSQL实例，但每database必须独立schema owner role、writer role与runtime login。部署Owner创建NOLOGIN `opl_<owner>_owner`（DDL）与`opl_<owner>_writer`（DML），runtime login仅获得对应writer，不获得owner。CloudIdentity与Gateway同进程也使用各自受限连接池，不能借共享进程跨库查表。
- `schema.sql`是**按BEGIN DATABASE/END DATABASE标记分段的源码包，不是可在一个连接顺序执行的迁移脚本**。执行器分段，分别连到指定database；每段`current_database()` guard拒绝错库。Owner/role/database创建是部署配置，不硬编码生产地址、Secret或登录密码。
- 同Owner FK全部`ON DELETE RESTRICT`；跨Owner ID仅`text`，通过typed RPC鉴权、存在性、版本与Owner readback校验。没有跨库FK/JOIN、跨域SQL、跨库CAS/事务、级联删除；BFF不写任何业务表。
- ID没有UUID/identity DEFAULT。新ID由Owner生成不透明string；旧string ID、时间、原wallet请求键与receipt ID原样保留，不把数字字符串转换成数值、不强制UUID。
- 金额`bigint`（int64）/USDMicros，REST十进制string。价格和总报价非负；所有quote明细amount均非负；adjustment_credit表示从总额扣除的额度，不存负号。不得JS浮点计算、复制可消费余额或把交易流水求和当钱包。
- 时间`timestamptz`，输入为UTC RFC3339，客户本地时区只在UI转换。`updated_at DEFAULT now()`只用于INSERT；Owner状态命令在同事务更新updated_at，不能声称数据库自动更新时间。
- JSONB仅用于版本化typed快照、完整性证据和Owner内部规格：必须按本Owner decoder拒绝非法/未知必需字段、错误类型和不兼容版本。不接受任意外部响应dump；不保存密码、raw Key、会话token、service token。共享字段不等于共享policy框架。

## 2. 必须实现的Owner事务规则

### 2.1 Tenant、成员和钱包委托

- `tenant_members`唯一活动actor索引约束“初期一个活动Tenant”；成员操作锁Tenant行，禁止撤销最后一位owner。每次业务请求复验Tenant与membership，停用/撤销立即拒绝，不等待旧session自然过期。
- Invitation状态从accepted_at/revoked_at/expires_at确定，不双写status。接受时锁邀请和目标actor的活动membership约束。令牌只存hash，邀请目标是明确Gateway subject；Tenant钱包主体不从邀请身份推导。
- `gateway.identity_mappings`属于逐个成员；`gateway.tenant_wallet_bindings`属于Tenant付款主体。Cloud不认证密码、不复制Sub2API余额、不用Cloud服务令牌冒充成员。**当前Gateway是否支持跨身份钱包委托必须以Gateway Owner契约与运行证据确认**；缺失时多人涉及资金动作明确拒绝，不能假装已具备。迁移旧账户的1:1身份/钱包映射保持原样。
- Tenant删除请求先设置`deletion_requested_at`；正式删除状态确认时写`deleted_at`及`restore_until=deleted_at+15天`，撤销成员访问并记录Tenant Operation关联的Workspace删除Operation。Workspace逐项销毁/退款仍由Workspace编排并独立读回。Tenant恢复只恢复仍保留的私有制品与权限；不公开私有包、不复活已删除硬盘、不自动重购或续费。
- `AssetCustody`是Tenant Operation、Capability/Build资产列表及Workspace删除进度的授权聚合DTO，不新增计数表。Package/Build数量实时按各Owner确定查询计算，不把count用作删除安全依据。

### 2.2 直传、不可变输入、构建注册

- 申请上传在Capability事务创建PackageVersion(upload_pending)+UploadSession+幂等响应。PackageVersion只包含原始包的版本、期望digest/size，不在上传时绑定Runtime或WebUI。
- 浏览器业务请求仅BFF；原始字节仅发送到Capability签发的精确对象/partNumber/大小/hash范围的短时Storage URL。`upload_chunks.part_number`从1开始；同session+partNumber固定size/hash，确认后写etag，unknown调用Storage读回原分片。续签上传URL不创建第二个包或第二分片；不持久化URL/临时凭据。
- UploadSession的sizeBytes/sha256读取PackageVersion；completedParts读取本session的confirmed chunks，按part_number排序。不持久化completedParts数组或offset两套事实。完成接口验证Storage分片、最终实测size/digest与manifest，匹配后才uploaded；ETag不等于SHA-256。
- `createBuild(packageVersionId,webuiVersionId)`显式触发构建；服务器冻结当前批准的Runtime/catalog policy并先创建queued Job。Worker再分别取得Package/Runtime/WebUI引用claim、在Build本库提交三个ID，然后Bind OwnerCommitEvidence；全部确认后才读包/执行Build，避免先claim却没有调用者Job身份。Build.input_snapshot包含精确objectRef/digest、Runtime/WebUI digest、构建器版本、接口版本和claim IDs；input_digest覆盖规范化全部输入。Job与本域Operation在同事务创建，回真实jobId。
- Build成功推送且按digest读回后，进入registering并发事件；Capability Inbox事务按唯一build_job_id创建ready CapabilityVersion并回注册事件；Build确认resultCapabilityVersionId才succeeded。失败重试新Job保留retry_of_build_job_id，旧输入/日志/失败不覆盖。
- BuildJob.status是执行状态，Operation只跟踪这个命令；状态映射在Build同事务更新，不能由其他Owner根据HTTP响应写它。单个worker lease仅防本库同时执行，不代替外部副作用幂等。

### 2.3 引用、墓碑与共享digest

- Capability唯一拥有`reference_claims`。Acquire在**本库事务锁目标版本行**、核验status/可见性后插入唯一活跃claim；没有跨Owner refcount。调用者先Acquire、再本Owner事务保存claimId、再Bind；中途崩溃留下的占用不按TTL猜测释放，只能由claimant Owner确认未形成/已结束引用并给release_evidence_ref。
- 删除客户CapabilityVersion在Capability同事务锁版本、检查无活跃claim、置deleting并禁止新claim；完成后为deleted墓碑，历史与Build/Package保留。归档Package不级联删除，不影响已经批准的Workspace续费/保留回滚引用；归档空间/包不再创建新上传。
- **墓碑不声称OCI bytes被物理删除。** 多CapabilityVersion、Runtime、WebUI、构建输入、Candidate/Release可能共享repository+digest；客户删除一版本不得删公共digest。物理清除只有管理员显式授权与全部引用释放后，锁定同repository+digest全局对象的准入记录、冻结所有新增引用，再由Registry权限Owner执行并读回。当前普通产品路径不自动GC、不自动90天清除。
- 本期没有新增客户物理GC endpoint/worker；墓碑已满足deleteCapabilityVersion语义。后续若实施物理清除，必须在当前Owner增加完整共享对象保护契约与焦点并发测试，不能拿COUNT查询后直接删Registry作为实现。
- Runtime/WebUI撤销禁止新Build/部署引用；旧Workspace按已有引用和安全处置规则显式处理，不凭目录状态自动换Runtime。

### 2.4 报价与账务义务

- ResourceCatalog唯一写Quote。创建事务读取有效不可变计划与price/refund/retention版本，写quote+quote_items并校验整数total=sum(compute/storage/product)-sum(adjustment_credit)、期数>0、输入摘要和准入readback。计算/存储计划列表的价格来自有效price policy投影，不在plan双写monthly_price。
- 先建计算/存储计划，再发布绑定exact compute+storage pair的价格版本；计划创建不要求尚未存在的pricePolicyVersionId。客户计划列表仅在另一侧plan已选且存在该pair当前策略时展示精确月价；否则只显示规格并要求Quote，不选最低价、不捏造免费。发布价格时锁同pair，拒绝相同validFrom；当前政策取validFrom不晚于now的最新版，再检查该版validUntil。新版显式替代当前选择，不改旧行；新版过期则当前不可报价，不回落旧价。已接受Quote和Subscription仍引用其原版本。
- Workspace先持久Operation，再调用`AcceptQuote(quoteId,operationId,inputDigest)`。**Catalog本库事务**锁Quote、验证tenant/actor/expiry/输入、绑定唯一accepted_by_operation_id并返回已接受快照；同Operation重放同快照，另一Operation冲突。Workspace只保存不可变接受快照和周期义务，不跨库UPDATE Quote，不成为第二个status writer。
- `subscriptions`与`subscription_periods`显式区分provenance：quoted必须有Quote ID和已接受快照、不得有legacy字段；legacy_import必须保留原legacy_purchase_id与legacy_obligation_snapshot、Quote ID/接受快照必须为空。迁移不得为满足NOT NULL伪造新Quote或新扣费；历史缺原policy/receipt证据时快照明确记录缺口，对依赖该证据的退款/续费返回POLICY_UNCONFIGURED，不推断同意。退款/保留条款按分支从对应原快照精确投影，不另建可写policy副本。
- 订阅当前周期及renewal_settings_version在subscriptions；客户可按版本撤销未来automatic同意，不抹除已经接受的本期义务。不可变付费周期历史在subscription_periods。Workspace.currentPeriodEnd读取subscription；新订阅默认manual；已有明确automatic同意必须保留renewal_mode、renewal_consent_id及精确原同意快照，迁移不降级。手动和已授权自动续费共享同subscription+period幂等身份，无已确认付款到期停用；余额存在不是自动扣款授权。已接受周期的价格、原billing_key、wallet_operation和receipt不重新生成。
- Local零费用报价不发零金额WalletOperation，不伪造扣款事实；仍保留零费Receipt。普通源码/CI/E2E不调用真实扣费/采购/续费/删除；这些必须在独立显式授权的Instance保护工作流执行。
- Gateway wallet_operations仅记录请求/confirmed/rejected/unknown，不是余额/第二钱包。固定business_idempotency_key写前持久；unknown按原externalReference或业务键查询，不换key重扣、不对未知扣款反向退款。
- 现行删除退款规则保留`workspace-delete-refund-v1`：`usedHours=ceil((deletedAt-resourceFulfilledAt)/1h)`，`refund=floor(originalPlatformChargeUSDMicros * max(720-usedHours,0)/720)`，必须`deletedAt > resourceFulfilledAt`；使用整数/有理数。来源为当前产品owner `packages/contracts/go/workspace_delete.go` 的原算法，不是本轮发明比例。Quote与Subscription快照绑定这个不可变版本、原周期、原平台扣款、resourceFulfilledAt和confirmed deletion absence receipt。退款请求必须引用原charge和资格；不能拿Provider采购金额或最新价格当原平台扣款。
- 资源销毁和退款分别呈现结果；Operation成功或Workspace.deleted不等于Wallet退款已确认。零退款记录资格/receipt，不伪造正金额交易。

### 2.5 Workspace、Deployment、配置与迁移

- workspaces保存当前已接受业务选择及version；互斥变更锁Workspace行，校验expectedVersion与active_operation_id，创建Operation/Saga同事务推进版本。目标版本放Deployment/Operation；失败不能先覆盖当前选择。
- `active_deployment_id`唯一选中事实；复合FK保证指向同Workspace的Deployment。Runtime没有active布尔。新部署经真实挂载、应用健康、Gateway认证/模型调用验证后，Workspace同事务切换指针、更新所选Capability、supersede旧Deployment；不能由Runtime服务反向双写选择。
- model_configurations持新version与typed selections(slot,modelId)。Runtime确认真实reload后，Workspace同事务推进model_configuration_version；appliedVersion从Workspace字段派生，status从Operation及确认结果派生。请求的新值不提前显示为生效值。
- Workspace.resourceReadiness/applicationAvailability/accessUrl通过当前选中Deployment、Fabric/Runtime授权readback聚合；不存在这些字段的第二份持久副本。readback未知显示unknown，不把Workspace.status=active硬解释为模型可用。
- legacy_resource_only：Capability/activeDeployment均空，资源和原订阅义务保留；adoptWorkspace在原资源部署，不产生资源重购或套餐扣款。imported_application：引用以exact legacyApplicationRevisionId+artifactDigest导入的CapabilityVersion，不伪造Package/Build；agent_saas走完整构建/目录链。
- CapabilityVersion.provenance=build时五个Package/Build/Runtime/WebUI引用全部必填；legacy_application时这五项必须全部NULL，legacy_application_revision_id必填。SQL CHECK覆盖两分支，legacy字段不能成为绕过新Build验证的用户上传口。
- 旧运行/账务迁移以09原始Owner字段readback为依据；无法验证exact digest/原付款键的记录标明确证据缺口并停止对应不可逆动作，不通过编造默认值宣称已迁移。

### 2.6 Outbox/Inbox/幂等/Operation的必要性与边界

每个Owner只有因当前消费者需要的独立物理表；它们不是共享框架、全局事件总线或中央工作流服务：

| Owner | Outbox当前计划事件消费者 | Inbox/幂等当前命令或事件消费者 |
|---|---|---|
| tenant | 权限/删除/恢复→Capability、Workspace、Gateway、Ledger | Gateway身份结果、成员/恢复命令、Workspace删除结果 |
| capability | Package完成/版本注册/引用变更→Build、Workspace、Ledger | 上传命令、Build注册、Tenant可见性事件、引用命令 |
| build | 构建注册请求/结果→Capability、Ledger | create/retry命令、Capability注册结果、Tenant隔离指令 |
| workspace | 生命周期/商业动作证据→Ledger、Tenant | Catalog接受、Gateway资金、Runtime/Fabric结果与用户命令 |
| runtime_control | readiness/动作证据→Workspace、Ledger | 部署/更新/停止/reload命令、Fabric结果 |
| fabric | 资源/挂载/Secret动作结果→Runtime/Workspace、Ledger | typed资源命令与受保护workflow结果 |
| gateway | 身份/资金/Key结果→Tenant、Workspace、Ledger | 资金/Key幂等命令、Tenant停用、Gateway owner读回 |
| resource_catalog | 目录发布/报价接受→Workspace、Ledger | 管理员目录命令、Create/AcceptQuote与Provider能力验证结果 |
| ledger | 对账差异/receipt可读回通知→来源Owner | 所有业务Owner receipt事件及对账命令 |

- Outbox_events与业务变化同事务写；outbox_deliveries按每消费者分别ACK。不可用一个消费者成功标记整个事件已投递；投递至少一次，consumer必须幂等。
- Inbox唯一(source_owner,source_event_id)，同ID异hash拒绝；去重记录、本Owner业务变更、processed_at、后续Outbox同事务。乱序按来源Owner版本与readback处理，不覆盖新事实，不静默丢事件。
- Aggregate revision仅在本Owner聚合锁内从原序列分配，不是跨库CAS。worker行锁/lease只作用本库；网络调用不占数据库长事务。持久command先于副作用，Owner readback后才在新事务确认。
- Idempotency作用域tenant_scope+actor_scope+**稳定API operationId名称**+key，不是每次新建的Operation实体ID。规范化body hash一致重放同resourceId/operationId，不同hash返回冲突；义务未结束不以短TTL清理。
- Idempotency response_body仅安全DTO；accepted_input亦只允许去密后的领域输入，禁止原始login密码、session cookie、恢复token、签名Upload URL或Storage临时凭据。Key创建/reveal响应不得把明文入表；只保留原Owner结果引用，单次Secret交付与重试语义由03明确。service credentials/Key不进入事件payload、日志或receipt。
- `Operation.kind/stage`以03的OperationKind、OperationStage和每kind允许stage映射为唯一词汇；Owner写入前使用同typed契约验证，不能把任意provider英文日志当stage。数据库text不另复制一份过时枚举，SQL parser通过不等于应用校验已实现。
- Operations每Owner独立；BFF通过owner+operationId路由。除Ledger同步append receipt外，各异步Owner有当前命令消费者；只有Workspace有Saga_steps，不制造中央Operation注册服务。Saga每步固定command/补偿command和owner观察结果；unknown回到原Owner读回。
- 不自动清理未确认Outbox/Inbox/幂等记录。Ledger、Build产物/日志、付费周期与发布的price/refund/retention事实仅append，runtime writer无UPDATE/DELETE权限；纠正需要新版本或新Receipt。

## 3. 约束验证范围

SQL约束证明单行/同库结构，不自动证明所有Owner业务事务。需要实施时覆盖：并发claim/墓碑、legacy两个分支、同Workspace指针范围、重复quote接受、unknown扣款/退款、成员撤销、上传重放、注册乱序、配置旧响应、Tenant恢复不重购。静态DDL parser+字段清单校验不等于实际PostgreSQL部署、服务实现、浏览器端到端或生产资格通过。

### 前一轮DDL历史证据（由本轮跨层验证补充）

`checks/db_execution.json`保留本轮执行时间、PostgreSQL精确版本、镜像digest、SQL/inventory哈希、57项结果、重放脚本与清理读回。使用本机已有postgres:16-alpine，`--network none`、tmpfs、无volume/bind mount：9个空database成功应用DDL，88表1145字段与真实information_schema类型/nullable逐项相符；反例验证legacy分支、string IDs、同Workspace选择FK、claim保护、活动Tenant唯一、正金额/int64溢出、Quote接受shape/重复绑定、禁止小时后付费、Ledger append-only及跨域CONNECT拒绝。容器已停止且确认自动删除。

这些结果仅证明DDL/权限/约束；Quote body/expiry校验、总价sum、状态机、Owner网络readback、Saga恢复、生产迁移仍需真实实现与对应层验收，不能升级为业务端到端通过。

以下字段表与SQL由同一显式清单生成；`—`为无DEFAULT，`NULL`为可空。跨文档派生来源由03的`x-field-map`标明，不为展示字段发明第二持久writer。





## 4. 开发交接复核R01–R09的精确修订

### R01：同一报价对象贯通REST、PostgreSQL和客户金额

`quotes.purpose`唯一词汇是`deploy/resize/renew`。`quote_items.kind`唯一词汇是`compute/storage/product/adjustment_credit`，不存在create/service/adjustment别名。每行amount是该行总额，quantity只供解释，不二次相乘；所有金额均为非负int64。总额为三类正项之和减credit之和，必须非负且不溢出。

每个credit必须有typed `credit_source`：originalWalletOperationId、originalSubscriptionPeriodId、policyVersionId、creditReceiptId、amountUSDMicros。DB检查source形状与金额相等；Catalog通过Gateway、Workspace、Ledger各自typed readback确认原交易/周期/批准政策和抵扣receipt属于同一原义务，不能跨库JOIN或相信客户提交一个receipt字符串。尚未批准的resize策略不能凭空创造credit。

D17已由用户确认，当前唯一政策是13与`contracts/plan-change-policy.json`的`workspace-plan-change-v1`。`price_policy_versions.plan_change_policy_version`只引用该固定已批准版本；`renewal_rules`保存typed RenewalPolicy。没有adjustment_rules任意JSON或pending/draft收费占位。缺旧原单事实、provider能力或实际资格仍须明确拒绝，不等同业务政策未确定。

### R02：输入保护四类目标及绑定/释放证据

内部ReferenceTarget oneof与本库字段精确对应：packageVersionId→package_version_id，runtimeVersionId→runtime_version_id，webuiVersionId→webui_version_id，capabilityVersionId→capability_version_id，target_type取对应snake_case；零个或多个目标均拒绝。FK只落Capability同库，claimant operation/resource ID保持不透明跨Owner引用。

BindReference要求OwnerCommitEvidence(owner/operationId/resourceId/acceptedInputDigest/committedVersion/acceptedAt)，Capability通过mTLS准确Owner的ReadOwnerCommit/ReadClaimUsage确认已提交相同输入。确认后写bound_operation_id、bound_input_digest、bound_at；同身份异digest不得覆盖。ReleaseReference要求ReleaseEvidence含真实终态与terminalReceiptId，且来源Owner读回observation=confirmed、activelyRequired=false；Runtime/Workspace资源需要确切absence证据。unknown、仅超时或自报receipt不释放。

### R03/R09：发布者描述和命名空间不是新Runtime框架

publisher_namespaces独立于用户Agent分组：平台管理员准入official或third_party Registry namespace、registryId、repositoryPrefix与receipt。仓库归属按完整路径段匹配，不能把`acme-evil`误认`acme`子空间；第三方不得借官方namespace准入。

Runtime/WebUI保存publisher_namespace_id、publisher_contract_object_ref、publisher_contract_digest与publisher_contract投影。对象引用指向Capability编码一次保存的精确不可变Storage bytes，Resolver读取原对象校验digest；JSONB不是重新序列化计算相同digest的依据。Registry repository/digest、ABI以及canonical applicationRevisionTemplate.image/platform由同一typed PublisherContract派生并由CHECK拒绝不一致。Runtime模板直接采用现有WorkspaceApplicationRevision契约；Build只把产物image换为最终Capability ArtifactReference，保留其它已批准执行/挂载/Secret/探针字段，不另造一套运行规格。

BuildInputSnapshot保存三种输入claim、完整Runtime/WebUI descriptor引用及digest；Build结果、Capability版本、RuntimeDeployCommand的deployment_descriptor贯通同一template与最终镜像。runtime_versions/webui_versions的runtime writer只能UPDATE status/updated_at，不能原位改发布者内容；新内容必须新版本。

### R04：CloudIdentity只在Gateway Integration内提供真实授权上下文

新增authorization_contexts和accepted_operation_grants是当前typed调用所需的数据，不是新服务。scope_type为tenant或platform；tenant作用域必须有真tenant_id，platform为空，不造伪Tenant。邀请接受前/平台账号的session.tenant_id同样可空。authorization context来自交互session或原accepted grant，二者恰一；平台管理员的登录Tenant不要求等于其明确管理目标Tenant。

GetAuthorizationContext/AuthorizeAction在每次特权调用同时核对：连接证书对应的service allowlist、确切audience/action/resource、scope、actor、session、当前Tenant/member或平台角色、permission_version和过期/撤销事实。请求body不能携带可被信任的service identity/role；不可用时拒绝，不缓存旧allow冒充实时授权。

accepted_operation_grants绑定原Owner/Operation/resource、原permission version、固定allowed_actions、mode及必要续费consent/period身份；不是任意action数组授权。撤权/停用转为closeout_only时只允许已获准的原义务核对/完成/撤销/退款/清理，不得新增采购、扣款、resize或延期。自动续费另用WorkspaceAuthorizationReadback验证最新consent及同周期身份，客户撤销未来授权不能被历史grant绕过。

### R05：Workspace选择提交和真实路由的两个提交点

Workspace唯一分配execution_epoch；Fabric唯一拥有route_generation、accepted_execution_epoch、provider_revision和实际target。Workspace.selected_route_generation/selected_execution_epoch仅保存最终选择提交对应的已确认Fabric结果，不成为第二路由writer。

1. Workspace本库锁定当前意图并递增execution_epoch，持久化Operation/目标Deployment。
2. Fabric先锁route_bindings，检查expected generation/provider revision并写唯一非终态route_switches(action_kind=fence)。真正调用provider的同一路由对象conditional revision CAS，同时更新epoch metadata并保持target不变；读回确认后更新accepted_execution_epoch/provider_revision，generation不变。
3. Activate/Rollback要求已确认fence的epoch、同provider revision、预期generation与精确目标/ready或compatibility receipt。先持久命令再调用provider CAS；成功读回后generation+1。旧provider请求即使晚到，也因同对象revision已变化而被拒绝，不能只在Cloud DB里检查epoch。
4. unknown的fence/activate/rollback占该Workspace唯一非终态位置；只能按原provider_command_id读回，禁止以更高epoch抢占/新命令重试。
5. Workspace核验原操作、当前epoch、原选中及Fabric目标读回后，以本库CAS提交active_deployment_id/selected generation/epoch并发selection receipt。Fabric记录workspace_selection_commit_receipt_id后才允许退休旧实例。提交响应丢失读取原身份；选择CAS失败为needs_attention，完成原提交或明确fence+rollback，禁止last-writer-wins。

ProviderRevisionPrecondition为exactRevision或ConfirmedRouteAbsence(receiptId,observedAt) oneof。首次require_absent只允许generation0且有真实absence证据；DB保存expected_absence_receipt_id/time，不用空串当通配条件。数据库事务不声称与外部router原子。

### R07：重新启用和删除后恢复分开

reenableTenant只能suspended→active，撤销suspended_at并递增权限版本，不需要restoreUntil、不删除/重购资源。restoreTenant只能对正式deleted在deleted_at+15天内恢复身份/保留资产权限；删除请求较早不缩短窗口，也不复活已经删除的CBS。两类命令的Workspace效果必须来自对应原停用/删除事实及typed读回，不能在UI统一成一个无条件“恢复”。

### 本轮语义验证的证明层

`checks/semantics_cases.json`提供跨层正反例，`checks/validate_cross_domain.py`编译当前protobuf、用03真实schema解码、执行明确Owner规格适配器、写隔离PostgreSQL并精确读回。它还直接比较API字段类型/enum与SQL映射，不能用“字段名字存在”代替值语义一致。检查记录按次写入checks/runs，绑定API/proto/publisher schema/SQL/inventory/用例/脚本全部hash。

最终跨层执行记录：`checks/runs/cross-domain-20260921T175006.611769Z.json`。当前9库93表1258字段真实应用与类型/nullability读回一致，259个direct API字段类型/枚举映射核对通过，57个协议→持久化→读回正反例通过；全部输入hash在运行中未变，临时容器已确认删除。重放命令：`python3 checks/validate_cross_domain.py --image <本地已存在的PostgreSQL16精确image-ID>`；脚本不pull镜像，不开宿主端口，生成新的不可变run记录而不覆盖旧receipt。这份较早记录仅证明当时57个案例；D17随后获用户确认，本轮新套餐规则以匹配当前全部sourceHashes的最新cross-domain run为准，不能拿旧hash宣称新规则通过。

Storage原对象与provider路由在此为明确的隔离fixture；这证明规格转换/约束及反例，不证明未来服务handler、真实provider能力、生产资金/资源或Instance资格已通过。此前69项DDL receipt对应旧schema保留历史；本轮以匹配当前hash的新cross-domain receipt为准。




## 5. D17已批准：PlanChange、唯一周期义务与独立补差原单

产品规则唯一Owner是13。此处只固定持久化/原子边界和可检验投影，不另定费率、日历或provider策略。

### 5.1 三份有当前调用者的数据，不是新框架

- `workspace.plan_changes`：第一类升级/下期降配身份。锁定Workspace与原Subscription version后保存Quote固定计算输入、source/target计划与已接受价格、T/S/E、原已付period、policyVersion、executionPlanId/digest、金额、计划与实际生效时间、各阶段Operation。partial unique保证一个Workspace至多一个requested/scheduled/awaiting_payment/applying/needs_attention变更。初次受理、边界执行、取消Operation分别持久，不能把scheduled受理已成功的Operation复活。
- `workspace.subscription_period_obligations`：资金确认前就建立唯一(subscription_id,period_start)原周期义务；manual/automatic/boundary worker共享同一id/billing_key。先在本库锁定/接受付款义务，再调用Gateway，关闭取消与发款之间的竞态。Gateway仍唯一写真实付款状态；此表不是wallet或跨账户资金预占系统。
- `workspace.supplemental_charges`：已成功升级的不可变补差原单、原Gateway交易/receipt和费用覆盖T..E。0补差记录零费证据、无Gateway charge id；不可把补差算成第二次购买整月Workspace。部分失败但未成功交付的补差以PlanChange原charge及失败证据全额补偿，不伪造成功supplement覆盖。

`subscriptions.version`是金融/周期/生效套餐的CAS版本，独立于renewal_settings_version；模型或应用更新不递增金融版本。current_price_policy_version_id/current_monthly_usd_micros是已成功生效且客户接受的价格快照，Pold只读它，不读最新目录价。升级提交仅改当前计划/价格/version，原currentPeriodStart/End及旧period/Quote/receipt不改写。新周期确实生效时才按原anchor切换当前周期，并保留旧period不可变历史。

### 5.2 时间精度与日历不能二次推断

原Go Owner保存的纳秒字符串保留在源financial snapshot；PostgreSQL timestamptz是微秒投影，不能用它重新决定新账期或计费毫秒。PlanChange保存quote_at_ms/period_start_ms/period_end_ms以及source_financial_snapshot_bytes/digest。它们由真实Subscription Owner从原始时刻一次计算UnixMilli，Scope包含原subscription/version/period/已接受价格；DB校验原bytes SHA256。SQL补差CHECK只使用这些bigint与numeric中间运算，不从PG舍入后的timestamp重算，不使用任意tolerance。

这个区别有真实反例：`.999999999Z`进入PG可能进位到下一秒，再取毫秒会多1ms，可能让最终ceil多收1微美元。新输入只在公式层按ms，不能把原Subscription E覆盖为截断值。剩余不足1ms导致R=0时明确不再存在可计费剩余周期，不新开一个月。

`subscriptions.billing_anchor_day`保留原Owner已证anchor(1..31)。nextPeriodStart/End由原Workspace月期Owner提供，沿用`monthly_billing.go:nextBillingMonth(current, anchorDay)`：next calendar month、day=min(original anchor,next month last day)，保留UTC时刻；Jan31→Feb28/29后仍回Mar31，不拿被截短的28/29覆盖原anchor。legacy缺anchor不能从Feb28猜31。

### 5.3 Quote、补差与计划的准确提交边界

Quote接受后固定source金融快照、policyVersion、原/目标月价、T/S/E、canonical ms、精确execution plan和最终一次ceil结果。升级明细只用一行`product`，description为“本期套餐升级补差”，amount=整体ceil，不按CPU/存储分别ceil。0补差可显示明确0，不调用Gateway。降配本期lineItems为空、total=0，nextPeriod单独显示目标价；真正renew时用目标原计算/存储/产品三项明细，不先收旧高价再退款。

同一Quote/意图重发返回原PlanChange、原Operation与原charge code，即使时间推进或当前价格已变化，也不重算T、不新扣款。新的不同请求先检查唯一未完成计划，用户必须显式取消旧计划后重新报价。原金融版本变化、Quote过期、到期/混合或不可比较转换/不支持缩盘/低于运行需求均在报价或接受时拒绝。

目标provider执行策略由Fabric冻结的executionPlanId/digest指向`resource_actions(action=prepare_resize)`，approved_input保存typed计划、execution_plan_bytes保存小型计划原字节、SHA256验证digest，evidence_ref为批准receipt。执行时另一resize action只能引用该计划，不能中途在in_place_resize与claim_target_pool_and_rebind_existing_storage间偷偷切换。策略允许compute identity改变不等于可以重买/丢弃CBS、数据、Key；必须逐一真实读回。

### 5.4 下期降配、取消、并发与预付边界

scheduled阶段不动当前资源，不扣/退本期钱，不占长期资源执行锁。模型/应用仍可更新；执行前重读当前application binding和最低资源。legacy_resource_only不伪造Runtime，runtimeReadbackRequirement=not_applicable；有真实应用时必须确认资源限制、镜像/挂载/Secret和健康。新应用使目标不满足需求时标at_risk/具体risk，不暗中取消、换价或升回原SKU；E前及执行前重新验证。

到E，manual无授权进入awaiting_payment并停止未付款使用；有明确consent或客户付款时，三个actor先竞争同一Workspace/Subscription锁和唯一周期行，固定目标价原单，再发Gateway。Gateway先幂等登记requested，再以原Code执行/读回；不能让三个线程各自先confirmed INSERT并仅依赖其中一个唯一索引。边界执行使用新的稳定executionOperation，不复活初次保存scheduled的成功任务。

取消与付款接受共用相同本库锁顺序。只有payment_accepted_at、wallet_operation_id、resource_execution_started_at全部为空才可取消。尚未付款的awaiting_payment可以经显式cancel+新Quote、version CAS重绑同一个obligation id/billing_key的未付款快照，旧Quote/PlanChange不改；首次资金accepted后永久冻结，不能造第二原单或覆盖价格。删除原子取消未执行计划；已applying/资金已接受的先收敛原操作，不同时destroy。

已有其它accepted/confirmed下一期资金义务时，新升/降配返回FUTURE_PERIOD_COMMITTED，不扩成多段预付重新定价。自己scheduled计划的提前付款仅允许原source_version→该绑定obligation.confirmed_subscription_version的一步桥接；别的版本变化仍stale。取消automatic只阻止未发出的未来自动扣款，不删计划或已接受原单。

### 5.5 补偿、成功后删除和防超退

`gateway.wallet_operations.purpose`是唯一交易用途：base_period、upgrade_supplement、base_period_delete、upgrade_failure_full、supplement_delete_unused、next_period_plan_failure_full、recharge。退款指向已有original_wallet_operation_id；没有第二个同义original_charge字段，也没有本地余额。

- 确认目标无法交付、执行已fence且目标未激活：升级补差退原单尚未退的全额，purpose=upgrade_failure_full；不套基础删除720小时算法。
- 磁盘已扩等不可逆残余：actual_outcome区分deliveryOutcome=failed和resourceOutcome=irreversible_residual。确定失败的补差依然全额补偿，Workspace remains needs_attention，平台承担残余成本，不向用户收部分交付费、不假缩容。unknown不是已失败，不据此退款。
- 成功升级后正常删除：supplemental_charges的coverage_start_ms/end_ms定义本补差自身区间，refund=floor(originalSupplement*max(E-deleteMs,0)/(E-T))，不能再把半个月款当整月/720小时。基础单继续其原政策；每个原单各自读回、去重、留证。
- 降配下一期已付但确定无法执行：按nextPeriodObligation实际目标付款原单全额补偿，purpose=next_period_plan_failure_full，不能误读本期chargeUSDMicros=0就不退，也不能给旧未付款权益自动延期。

Gateway每次退款在**自己的原charge行锁**下计算original confirmed amount减去同原单requested/unknown/confirmed refunds。相同key先返回原身份；不同key超过剩余额度拒绝，unknown占原单未确认额度但不复制钱包。原单、原用途、amount、entitlement与证据必须匹配。先核对原身份，不能因网络未知创建新退款。

### 5.6 本轮执行与历史证据

`checks/semantics_cases.json`中的D17预期金额独立写定，不从policy JSON的expected examples导入；脚本用真实API schema/protobuf、任意精度整数、隔离PG CHECK/唯一索引/事务和实际并发连接检验。覆盖20/31天、整周期/最后1ms、0及负价差、连升、原E原文、纳秒进位、同key、manual/automatic/boundary唯一目标付款、取消/删除/续费冲突、未知款项不退款、不可逆残余全额补偿、自身补差覆盖和原单防超退。

结果仍只证明可执行规格适配器及数据库，不是实现代码、真实Gateway资金或provider/Instance资格。历史run不覆盖；当前结论只采用sourceHashes与最新API/proto/13/policy/SQL/inventory/cases/脚本一致的新run。

## 字段级清单

### tenant

Database `opl_tenant` · Schema `tenant` · Writer `opl_tenant_writer`。

#### tenant.tenants

CloudIdentity owns Tenant authorization; permission_version advances on access changes; deleted restore window starts at actual deleted_at, not request time。覆盖 F01, F15, F16。

| 字段 | 类型 | 可空 | 默认 | 字段来源 |
|---|---|---|---|---|
| `id` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/Tenant/properties/id` |
| `name` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/Tenant/properties/name` |
| `status` | `text` | 否 | `'active'` | `03_api_contract_complete.yaml#/components/schemas/Tenant/properties/status` |
| `suspended_at` | `timestamptz` | NULL | `—` | `02_database_schema_complete.md#tenant.tenants.suspended_at` |
| `deletion_requested_at` | `timestamptz` | NULL | `—` | `02_database_schema_complete.md#tenant.tenants.deletion_requested_at` |
| `restore_until` | `timestamptz` | NULL | `—` | `03_api_contract_complete.yaml#/components/schemas/Tenant/properties/restoreUntil` |
| `deleted_at` | `timestamptz` | NULL | `—` | `02_database_schema_complete.md#tenant.tenants.deleted_at` |
| `created_at` | `timestamptz` | 否 | `now()` | `03_api_contract_complete.yaml#/components/schemas/Tenant/properties/createdAt` |
| `updated_at` | `timestamptz` | 否 | `now()` | `03_api_contract_complete.yaml#/components/schemas/Tenant/properties/updatedAt` |
| `permission_version` | `bigint` | 否 | `0` | `02_database_schema_complete.md#tenant.tenants.permission_version` |

约束：
- `PRIMARY KEY (id)`
- `CHECK (status IN ('active','suspended','deleting','deleted'))`
- `CHECK (permission_version >= 0)`
- `CHECK ((deleted_at IS NULL AND restore_until IS NULL) OR (deleted_at IS NOT NULL AND restore_until = deleted_at + interval '15 days'))`
- `CHECK (status <> 'deleted' OR deleted_at IS NOT NULL)`

索引：
- `tenants_status_list`: `(status, created_at DESC, id DESC)`

#### tenant.tenant_members

同actor至多一个未撤销membership；更改锁Tenant行且不得删除最后owner。覆盖 F01, F15。

| 字段 | 类型 | 可空 | 默认 | 字段来源 |
|---|---|---|---|---|
| `id` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/Member/properties/id` |
| `tenant_id` | `text` | 否 | `—` | `02_database_schema_complete.md#tenant.tenant_members.tenant_id` |
| `actor_id` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/Member/properties/actorId` |
| `role` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/Member/properties/role` |
| `revoked_at` | `timestamptz` | NULL | `—` | `02_database_schema_complete.md#tenant.tenant_members.revoked_at` |
| `created_at` | `timestamptz` | 否 | `now()` | `03_api_contract_complete.yaml#/components/schemas/Member/properties/createdAt` |
| `updated_at` | `timestamptz` | 否 | `now()` | `02_database_schema_complete.md#tenant.tenant_members.updated_at` |

约束：
- `PRIMARY KEY (id)`
- `FOREIGN KEY (tenant_id) REFERENCES tenant.tenants (id) ON DELETE RESTRICT`
- `CHECK (role IN ('owner','admin','member'))`
- `UNIQUE (tenant_id, actor_id)`

索引：
- `tenant_members_one_active`: UNIQUE `(actor_id)` WHERE `revoked_at IS NULL`
- `tenant_members_tenant`: `(tenant_id, created_at DESC, id DESC)`

#### tenant.invitations

邀请token只存hash；接受时锁invite并校验actor活动Tenant。覆盖 F01。

| 字段 | 类型 | 可空 | 默认 | 字段来源 |
|---|---|---|---|---|
| `id` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/Invitation/properties/id` |
| `tenant_id` | `text` | 否 | `—` | `02_database_schema_complete.md#tenant.invitations.tenant_id` |
| `invitee_gateway_subject_id` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/Invitation/properties/inviteeGatewaySubjectId` |
| `role` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/Invitation/properties/role` |
| `token_hash` | `text` | 否 | `—` | `02_database_schema_complete.md#tenant.invitations.token_hash` |
| `invited_by` | `text` | 否 | `—` | `02_database_schema_complete.md#tenant.invitations.invited_by` |
| `accepted_by` | `text` | NULL | `—` | `02_database_schema_complete.md#tenant.invitations.accepted_by` |
| `expires_at` | `timestamptz` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/Invitation/properties/expiresAt` |
| `accepted_at` | `timestamptz` | NULL | `—` | `02_database_schema_complete.md#tenant.invitations.accepted_at` |
| `revoked_at` | `timestamptz` | NULL | `—` | `02_database_schema_complete.md#tenant.invitations.revoked_at` |
| `created_at` | `timestamptz` | 否 | `now()` | `03_api_contract_complete.yaml#/components/schemas/Invitation/properties/createdAt` |
| `updated_at` | `timestamptz` | 否 | `now()` | `02_database_schema_complete.md#tenant.invitations.updated_at` |

约束：
- `PRIMARY KEY (id)`
- `FOREIGN KEY (tenant_id) REFERENCES tenant.tenants (id) ON DELETE RESTRICT`
- `CHECK (role IN ('admin','member'))`
- `UNIQUE (token_hash)`
- `CHECK (NOT (accepted_at IS NOT NULL AND revoked_at IS NOT NULL))`

索引：
- `member_invites_tenant`: `(tenant_id, created_at DESC, id DESC)`

#### tenant.sessions

只存cookie/CSRF摘要及Gateway会话Secret Store引用；每次请求复验Tenant/member。覆盖 F01, F15。

| 字段 | 类型 | 可空 | 默认 | 字段来源 |
|---|---|---|---|---|
| `id` | `text` | 否 | `—` | `02_database_schema_complete.md#tenant.sessions.id` |
| `session_hash` | `text` | 否 | `—` | `02_database_schema_complete.md#tenant.sessions.session_hash` |
| `actor_id` | `text` | 否 | `—` | `02_database_schema_complete.md#tenant.sessions.actor_id` |
| `tenant_id` | `text` | NULL | `—` | `02_database_schema_complete.md#tenant.sessions.tenant_id` |
| `gateway_session_ref` | `text` | 否 | `—` | `02_database_schema_complete.md#tenant.sessions.gateway_session_ref` |
| `csrf_hash` | `text` | 否 | `—` | `02_database_schema_complete.md#tenant.sessions.csrf_hash` |
| `expires_at` | `timestamptz` | 否 | `—` | `02_database_schema_complete.md#tenant.sessions.expires_at` |
| `revoked_at` | `timestamptz` | NULL | `—` | `02_database_schema_complete.md#tenant.sessions.revoked_at` |
| `created_at` | `timestamptz` | 否 | `now()` | `02_database_schema_complete.md#tenant.sessions.created_at` |
| `updated_at` | `timestamptz` | 否 | `now()` | `02_database_schema_complete.md#tenant.sessions.updated_at` |

约束：
- `PRIMARY KEY (id)`
- `FOREIGN KEY (tenant_id) REFERENCES tenant.tenants (id) ON DELETE RESTRICT`
- `UNIQUE (session_hash)`

索引：
- `sessions_actor`: `(actor_id)`
- `sessions_tenant`: `(tenant_id)`

#### tenant.audit_events

append-only权限审计，不含token/Key/密码。覆盖 F01, F15, F17。

| 字段 | 类型 | 可空 | 默认 | 字段来源 |
|---|---|---|---|---|
| `id` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/AuditEvent/properties/id` |
| `tenant_id` | `text` | NULL | `—` | `02_database_schema_complete.md#tenant.audit_events.tenant_id` |
| `actor_id` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/AuditEvent/properties/actorId` |
| `action` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/AuditEvent/properties/action` |
| `resource_type` | `text` | 否 | `—` | `02_database_schema_complete.md#tenant.audit_events.resource_type` |
| `resource_id` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/AuditEvent/properties/resourceId` |
| `request_id` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/AuditEvent/properties/requestId` |
| `safe_details` | `jsonb` | 否 | `—` | `02_database_schema_complete.md#tenant.audit_events.safe_details` |
| `created_at` | `timestamptz` | 否 | `now()` | `03_api_contract_complete.yaml#/components/schemas/AuditEvent/properties/createdAt` |
| `outcome` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/AuditEvent/properties/outcome` |

约束：
- `PRIMARY KEY (id)`
- `FOREIGN KEY (tenant_id) REFERENCES tenant.tenants (id) ON DELETE RESTRICT`

索引：
- `tenant_audit_list`: `(tenant_id, created_at DESC, id DESC)`
- `tenant_audit_request`: `(request_id)`

#### tenant.outbox_events

本Owner状态事务同写immutable事件；revision只在本Owner聚合锁内分配。覆盖 F01, F04, F05, F08, F10, F11, F12, F13, F15, F16, F17。

| 字段 | 类型 | 可空 | 默认 | 字段来源 |
|---|---|---|---|---|
| `id` | `text` | 否 | `—` | `02_database_schema_complete.md#tenant.outbox_events.id` |
| `event_type` | `text` | 否 | `—` | `02_database_schema_complete.md#tenant.outbox_events.event_type` |
| `schema_version` | `integer` | 否 | `—` | `02_database_schema_complete.md#tenant.outbox_events.schema_version` |
| `aggregate_type` | `text` | 否 | `—` | `02_database_schema_complete.md#tenant.outbox_events.aggregate_type` |
| `aggregate_id` | `text` | 否 | `—` | `02_database_schema_complete.md#tenant.outbox_events.aggregate_id` |
| `aggregate_revision` | `bigint` | 否 | `—` | `02_database_schema_complete.md#tenant.outbox_events.aggregate_revision` |
| `tenant_id` | `text` | NULL | `—` | `02_database_schema_complete.md#tenant.outbox_events.tenant_id` |
| `correlation_id` | `text` | 否 | `—` | `02_database_schema_complete.md#tenant.outbox_events.correlation_id` |
| `causation_id` | `text` | NULL | `—` | `02_database_schema_complete.md#tenant.outbox_events.causation_id` |
| `payload` | `jsonb` | 否 | `—` | `02_database_schema_complete.md#tenant.outbox_events.payload` |
| `payload_sha256` | `text` | 否 | `—` | `02_database_schema_complete.md#tenant.outbox_events.payload_sha256` |
| `occurred_at` | `timestamptz` | 否 | `—` | `02_database_schema_complete.md#tenant.outbox_events.occurred_at` |
| `created_at` | `timestamptz` | 否 | `now()` | `02_database_schema_complete.md#tenant.outbox_events.created_at` |

约束：
- `PRIMARY KEY (id)`
- `CHECK (schema_version > 0 AND aggregate_revision >= 0)`
- `CHECK (payload_sha256 ~ '^[0-9a-f]{64}$')`
- `UNIQUE (aggregate_type, aggregate_id, aggregate_revision, event_type)`

索引：
- `outbox_events_aggregate`: `(aggregate_type, aggregate_id, aggregate_revision)`

#### tenant.outbox_deliveries

各consumer独立ACK，禁止单个ACK丢其他消费者事件；lease仅本库。覆盖 F05, F08, F10, F11, F12, F13, F15, F17。

| 字段 | 类型 | 可空 | 默认 | 字段来源 |
|---|---|---|---|---|
| `id` | `text` | 否 | `—` | `02_database_schema_complete.md#tenant.outbox_deliveries.id` |
| `event_id` | `text` | 否 | `—` | `02_database_schema_complete.md#tenant.outbox_deliveries.event_id` |
| `consumer_owner` | `text` | 否 | `—` | `02_database_schema_complete.md#tenant.outbox_deliveries.consumer_owner` |
| `attempt_count` | `integer` | 否 | `0` | `02_database_schema_complete.md#tenant.outbox_deliveries.attempt_count` |
| `next_attempt_at` | `timestamptz` | 否 | `now()` | `02_database_schema_complete.md#tenant.outbox_deliveries.next_attempt_at` |
| `acknowledged_at` | `timestamptz` | NULL | `—` | `02_database_schema_complete.md#tenant.outbox_deliveries.acknowledged_at` |
| `last_error_code` | `text` | NULL | `—` | `02_database_schema_complete.md#tenant.outbox_deliveries.last_error_code` |
| `lease_token` | `text` | NULL | `—` | `02_database_schema_complete.md#tenant.outbox_deliveries.lease_token` |
| `lease_until` | `timestamptz` | NULL | `—` | `02_database_schema_complete.md#tenant.outbox_deliveries.lease_until` |
| `created_at` | `timestamptz` | 否 | `now()` | `02_database_schema_complete.md#tenant.outbox_deliveries.created_at` |
| `updated_at` | `timestamptz` | 否 | `now()` | `02_database_schema_complete.md#tenant.outbox_deliveries.updated_at` |

约束：
- `PRIMARY KEY (id)`
- `FOREIGN KEY (event_id) REFERENCES tenant.outbox_events (id) ON DELETE RESTRICT`
- `UNIQUE (event_id, consumer_owner)`
- `CHECK (attempt_count >= 0)`
- `CHECK ((lease_token IS NULL) = (lease_until IS NULL))`

索引：
- `outbox_deliveries_pending`: `(next_attempt_at, id)` WHERE `acknowledged_at IS NULL`

#### tenant.inbox_events

去重hash/业务状态/processed_at/后续Outbox同本库事务；乱序不能覆盖新版本。覆盖 F05, F08, F10, F11, F12, F13, F15, F17。

| 字段 | 类型 | 可空 | 默认 | 字段来源 |
|---|---|---|---|---|
| `id` | `text` | 否 | `—` | `02_database_schema_complete.md#tenant.inbox_events.id` |
| `source_owner` | `text` | 否 | `—` | `02_database_schema_complete.md#tenant.inbox_events.source_owner` |
| `source_event_id` | `text` | 否 | `—` | `02_database_schema_complete.md#tenant.inbox_events.source_event_id` |
| `event_type` | `text` | 否 | `—` | `02_database_schema_complete.md#tenant.inbox_events.event_type` |
| `schema_version` | `integer` | 否 | `—` | `02_database_schema_complete.md#tenant.inbox_events.schema_version` |
| `aggregate_type` | `text` | 否 | `—` | `02_database_schema_complete.md#tenant.inbox_events.aggregate_type` |
| `aggregate_id` | `text` | 否 | `—` | `02_database_schema_complete.md#tenant.inbox_events.aggregate_id` |
| `aggregate_revision` | `bigint` | 否 | `—` | `02_database_schema_complete.md#tenant.inbox_events.aggregate_revision` |
| `payload_sha256` | `text` | 否 | `—` | `02_database_schema_complete.md#tenant.inbox_events.payload_sha256` |
| `payload` | `jsonb` | 否 | `—` | `02_database_schema_complete.md#tenant.inbox_events.payload` |
| `received_at` | `timestamptz` | 否 | `now()` | `02_database_schema_complete.md#tenant.inbox_events.received_at` |
| `processed_at` | `timestamptz` | NULL | `—` | `02_database_schema_complete.md#tenant.inbox_events.processed_at` |
| `result_resource_id` | `text` | NULL | `—` | `02_database_schema_complete.md#tenant.inbox_events.result_resource_id` |
| `error_code` | `text` | NULL | `—` | `02_database_schema_complete.md#tenant.inbox_events.error_code` |

约束：
- `PRIMARY KEY (id)`
- `UNIQUE (source_owner, source_event_id)`
- `CHECK (schema_version > 0 AND aggregate_revision >= 0)`
- `CHECK (payload_sha256 ~ '^[0-9a-f]{64}$')`

索引：
- `inbox_events_pending`: `(received_at, id)` WHERE `processed_at IS NULL`
- `inbox_events_aggregate`: `(source_owner, aggregate_type, aggregate_id, aggregate_revision)`

#### tenant.idempotency_records

命令创建同事务；operation_name是API操作名不是新Operation ID；安全响应无Key明文；义务存续无TTL清理。覆盖 F01, F03, F04, F05, F08, F10, F11, F12, F13, F14, F15, F16, F17。

| 字段 | 类型 | 可空 | 默认 | 字段来源 |
|---|---|---|---|---|
| `id` | `text` | 否 | `—` | `02_database_schema_complete.md#tenant.idempotency_records.id` |
| `tenant_scope` | `text` | 否 | `—` | `02_database_schema_complete.md#tenant.idempotency_records.tenant_scope` |
| `actor_scope` | `text` | 否 | `—` | `02_database_schema_complete.md#tenant.idempotency_records.actor_scope` |
| `operation_name` | `text` | 否 | `—` | `02_database_schema_complete.md#tenant.idempotency_records.operation_name` |
| `idempotency_key` | `text` | 否 | `—` | `02_database_schema_complete.md#tenant.idempotency_records.idempotency_key` |
| `request_sha256` | `text` | 否 | `—` | `02_database_schema_complete.md#tenant.idempotency_records.request_sha256` |
| `resource_id` | `text` | 否 | `—` | `02_database_schema_complete.md#tenant.idempotency_records.resource_id` |
| `operation_id` | `text` | NULL | `—` | `02_database_schema_complete.md#tenant.idempotency_records.operation_id` |
| `response_status` | `integer` | 否 | `—` | `02_database_schema_complete.md#tenant.idempotency_records.response_status` |
| `response_body` | `jsonb` | 否 | `—` | `02_database_schema_complete.md#tenant.idempotency_records.response_body` |
| `created_at` | `timestamptz` | 否 | `now()` | `02_database_schema_complete.md#tenant.idempotency_records.created_at` |

约束：
- `PRIMARY KEY (id)`
- `UNIQUE (tenant_scope, actor_scope, operation_name, idempotency_key)`
- `CHECK (request_sha256 ~ '^[0-9a-f]{64}$')`
- `CHECK (response_status BETWEEN 100 AND 599)`

索引：
- `idempotency_records_resource`: `(resource_id)`

#### tenant.operations

目标Owner持异步Operation；BFF路由无中央writer；Build创建201回Job；只有Workspace配Saga。覆盖 F01, F03, F04, F06, F08, F09, F10, F11, F12, F13, F14, F15, F17。

| 字段 | 类型 | 可空 | 默认 | 字段来源 |
|---|---|---|---|---|
| `id` | `text` | 否 | `—` | `02_database_schema_complete.md#tenant.operations.id` |
| `tenant_id` | `text` | NULL | `—` | `02_database_schema_complete.md#tenant.operations.tenant_id` |
| `actor_id` | `text` | 否 | `—` | `02_database_schema_complete.md#tenant.operations.actor_id` |
| `kind` | `text` | 否 | `—` | `02_database_schema_complete.md#tenant.operations.kind` |
| `resource_id` | `text` | 否 | `—` | `02_database_schema_complete.md#tenant.operations.resource_id` |
| `status` | `text` | 否 | `'accepted'` | `02_database_schema_complete.md#tenant.operations.status` |
| `stage` | `text` | 否 | `—` | `02_database_schema_complete.md#tenant.operations.stage` |
| `error_code` | `text` | NULL | `—` | `02_database_schema_complete.md#tenant.operations.error_code` |
| `observation_result` | `text` | NULL | `—` | `02_database_schema_complete.md#tenant.operations.observation_result` |
| `request_id` | `text` | 否 | `—` | `02_database_schema_complete.md#tenant.operations.request_id` |
| `accepted_input` | `jsonb` | 否 | `—` | `02_database_schema_complete.md#tenant.operations.accepted_input` |
| `result` | `jsonb` | NULL | `—` | `02_database_schema_complete.md#tenant.operations.result` |
| `worker_lease_token` | `text` | NULL | `—` | `02_database_schema_complete.md#tenant.operations.worker_lease_token` |
| `worker_lease_until` | `timestamptz` | NULL | `—` | `02_database_schema_complete.md#tenant.operations.worker_lease_until` |
| `started_at` | `timestamptz` | NULL | `—` | `02_database_schema_complete.md#tenant.operations.started_at` |
| `completed_at` | `timestamptz` | NULL | `—` | `02_database_schema_complete.md#tenant.operations.completed_at` |
| `created_at` | `timestamptz` | 否 | `now()` | `02_database_schema_complete.md#tenant.operations.created_at` |
| `updated_at` | `timestamptz` | 否 | `now()` | `02_database_schema_complete.md#tenant.operations.updated_at` |

约束：
- `PRIMARY KEY (id)`
- `CHECK (status IN ('accepted','running','awaiting_confirmation','succeeded','failed','needs_attention','cancelled'))`
- `CHECK (observation_result IN ('confirmed','rejected','unknown'))`
- `CHECK ((worker_lease_token IS NULL) = (worker_lease_until IS NULL))`
- `CHECK (status <> 'succeeded' OR completed_at IS NOT NULL)`

索引：
- `operations_resource`: `(resource_id, created_at DESC, id DESC)`
- `operations_tenant_list`: `(tenant_id, created_at DESC, id DESC)`
- `operations_recovery`: `(status, updated_at)`

#### tenant.authorization_contexts

Opaque context introspected by CloudIdentity; authenticated mTLS caller, audience, action, resource and current permission version all bound; no bearer-token body。覆盖 F01, F08, F15, F17。

| 字段 | 类型 | 可空 | 默认 | 字段来源 |
|---|---|---|---|---|
| `id` | `text` | 否 | `—` | `02_database_schema_complete.md#tenant.authorization_contexts.id` |
| `scope_type` | `text` | 否 | `—` | `02_database_schema_complete.md#tenant.authorization_contexts.scope_type` |
| `tenant_id` | `text` | NULL | `—` | `02_database_schema_complete.md#tenant.authorization_contexts.tenant_id` |
| `actor_id` | `text` | 否 | `—` | `02_database_schema_complete.md#tenant.authorization_contexts.actor_id` |
| `session_id` | `text` | NULL | `—` | `02_database_schema_complete.md#tenant.authorization_contexts.session_id` |
| `permission_version` | `bigint` | 否 | `—` | `02_database_schema_complete.md#tenant.authorization_contexts.permission_version` |
| `audience_owner` | `text` | 否 | `—` | `02_database_schema_complete.md#tenant.authorization_contexts.audience_owner` |
| `action` | `text` | 否 | `—` | `02_database_schema_complete.md#tenant.authorization_contexts.action` |
| `resource_kind` | `text` | 否 | `—` | `02_database_schema_complete.md#tenant.authorization_contexts.resource_kind` |
| `resource_id` | `text` | NULL | `—` | `02_database_schema_complete.md#tenant.authorization_contexts.resource_id` |
| `issuer` | `text` | 否 | `—` | `02_database_schema_complete.md#tenant.authorization_contexts.issuer` |
| `issued_at` | `timestamptz` | 否 | `—` | `02_database_schema_complete.md#tenant.authorization_contexts.issued_at` |
| `expires_at` | `timestamptz` | 否 | `—` | `02_database_schema_complete.md#tenant.authorization_contexts.expires_at` |
| `revoked_at` | `timestamptz` | NULL | `—` | `02_database_schema_complete.md#tenant.authorization_contexts.revoked_at` |
| `accepted_operation_grant_id` | `text` | NULL | `—` | `02_database_schema_complete.md#tenant.authorization_contexts.accepted_operation_grant_id` |

约束：
- `PRIMARY KEY (id)`
- `FOREIGN KEY (tenant_id) REFERENCES tenant.tenants (id) ON DELETE RESTRICT`
- `FOREIGN KEY (session_id) REFERENCES tenant.sessions (id) ON DELETE RESTRICT`
- `CHECK (scope_type IN ('tenant','platform'))`
- `CHECK (issuer IN ('cloud_identity'))`
- `CHECK ((scope_type = 'tenant') = (tenant_id IS NOT NULL))`
- `CHECK (permission_version >= 0)`
- `CHECK (expires_at > issued_at)`
- `CHECK (num_nonnulls(session_id,accepted_operation_grant_id) = 1)`
- `FOREIGN KEY (accepted_operation_grant_id) REFERENCES tenant.accepted_operation_grants (id) ON DELETE RESTRICT`

索引：
- `authorization_contexts_session`: `(session_id)`
- `authorization_contexts_actor_scope`: `(actor_id, tenant_id, expires_at)`

#### tenant.accepted_operation_grants

Bounded original accepted-operation obligation; revocation permits only approved completion/cancel/closeout, never new procurement。覆盖 F08, F10, F11, F12, F13, F15。

| 字段 | 类型 | 可空 | 默认 | 字段来源 |
|---|---|---|---|---|
| `id` | `text` | 否 | `—` | `02_database_schema_complete.md#tenant.accepted_operation_grants.id` |
| `scope_type` | `text` | 否 | `—` | `02_database_schema_complete.md#tenant.accepted_operation_grants.scope_type` |
| `tenant_id` | `text` | NULL | `—` | `02_database_schema_complete.md#tenant.accepted_operation_grants.tenant_id` |
| `actor_id` | `text` | 否 | `—` | `02_database_schema_complete.md#tenant.accepted_operation_grants.actor_id` |
| `accepted_operation_owner` | `text` | 否 | `—` | `02_database_schema_complete.md#tenant.accepted_operation_grants.accepted_operation_owner` |
| `accepted_operation_id` | `text` | 否 | `—` | `02_database_schema_complete.md#tenant.accepted_operation_grants.accepted_operation_id` |
| `accepted_action` | `text` | 否 | `—` | `02_database_schema_complete.md#tenant.accepted_operation_grants.accepted_action` |
| `resource_id` | `text` | 否 | `—` | `02_database_schema_complete.md#tenant.accepted_operation_grants.resource_id` |
| `accepted_permission_version` | `bigint` | 否 | `—` | `02_database_schema_complete.md#tenant.accepted_operation_grants.accepted_permission_version` |
| `allowed_actions` | `text[]` | 否 | `—` | `02_database_schema_complete.md#tenant.accepted_operation_grants.allowed_actions` |
| `issued_at` | `timestamptz` | 否 | `—` | `02_database_schema_complete.md#tenant.accepted_operation_grants.issued_at` |
| `expires_at` | `timestamptz` | NULL | `—` | `02_database_schema_complete.md#tenant.accepted_operation_grants.expires_at` |
| `revoked_at` | `timestamptz` | NULL | `—` | `02_database_schema_complete.md#tenant.accepted_operation_grants.revoked_at` |
| `obligation_completed_at` | `timestamptz` | NULL | `—` | `02_database_schema_complete.md#tenant.accepted_operation_grants.obligation_completed_at` |
| `mode` | `text` | 否 | `—` | `02_database_schema_complete.md#tenant.accepted_operation_grants.mode` |
| `renewal_consent_id` | `text` | NULL | `—` | `02_database_schema_complete.md#tenant.accepted_operation_grants.renewal_consent_id` |
| `subscription_period_id` | `text` | NULL | `—` | `02_database_schema_complete.md#tenant.accepted_operation_grants.subscription_period_id` |

约束：
- `PRIMARY KEY (id)`
- `FOREIGN KEY (tenant_id) REFERENCES tenant.tenants (id) ON DELETE RESTRICT`
- `CHECK (scope_type IN ('tenant','platform'))`
- `CHECK ((scope_type = 'tenant') = (tenant_id IS NOT NULL))`
- `CHECK (accepted_permission_version >= 0)`
- `CHECK (cardinality(allowed_actions) > 0 AND array_position(allowed_actions,NULL) IS NULL)`
- `CHECK (expires_at IS NULL OR expires_at > issued_at)`
- `UNIQUE (accepted_operation_owner, accepted_operation_id)`
- `CHECK (mode IN ('continue_original','closeout_only','revoked'))`

索引：
- `operation_grants_resource`: `(tenant_id, resource_id)`
- `operation_grants_open`: `(issued_at)` WHERE `obligation_completed_at IS NULL`

### capability

Database `opl_capability` · Schema `capability` · Writer `opl_capability_writer`。

#### capability.namespaces

官方空间无零UUID伪Tenant；私有空间经Tenant owner授权。覆盖 F02, F15。

| 字段 | 类型 | 可空 | 默认 | 字段来源 |
|---|---|---|---|---|
| `id` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/Namespace/properties/id` |
| `tenant_id` | `text` | NULL | `—` | `02_database_schema_complete.md#capability.namespaces.tenant_id` |
| `name` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/Namespace/properties/name` |
| `kind` | `text` | 否 | `—` | `02_database_schema_complete.md#capability.namespaces.kind` |
| `description` | `text` | NULL | `—` | `02_database_schema_complete.md#capability.namespaces.description` |
| `status` | `text` | 否 | `'active'` | `03_api_contract_complete.yaml#/components/schemas/Namespace/properties/status` |
| `created_at` | `timestamptz` | 否 | `now()` | `03_api_contract_complete.yaml#/components/schemas/Namespace/properties/createdAt` |
| `updated_at` | `timestamptz` | 否 | `now()` | `02_database_schema_complete.md#capability.namespaces.updated_at` |

约束：
- `PRIMARY KEY (id)`
- `CHECK (kind IN ('official','tenant_default','tenant_custom'))`
- `CHECK (status IN ('active','archived'))`
- `CHECK ((kind = 'official') = (tenant_id IS NULL))`

索引：
- `namespaces_tenant_name`: UNIQUE `(tenant_id, name)` WHERE `tenant_id IS NOT NULL`
- `namespaces_official_name`: UNIQUE `(name)` WHERE `tenant_id IS NULL`
- `namespaces_default`: UNIQUE `(tenant_id)` WHERE `kind = 'tenant_default' AND status = 'active'`

#### capability.packages

visibility与namespace.kind同事务校验；不存最新对象或Build状态。覆盖 F02, F04, F06, F15。

| 字段 | 类型 | 可空 | 默认 | 字段来源 |
|---|---|---|---|---|
| `id` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/Package/properties/id` |
| `namespace_id` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/Package/properties/namespaceId` |
| `name` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/Package/properties/name` |
| `description` | `text` | NULL | `—` | `03_api_contract_complete.yaml#/components/schemas/Package/properties/description` |
| `visibility` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/Package/properties/visibility` |
| `status` | `text` | 否 | `'active'` | `03_api_contract_complete.yaml#/components/schemas/Package/properties/status` |
| `created_by` | `text` | 否 | `—` | `02_database_schema_complete.md#capability.packages.created_by` |
| `archived_at` | `timestamptz` | NULL | `—` | `02_database_schema_complete.md#capability.packages.archived_at` |
| `created_at` | `timestamptz` | 否 | `now()` | `03_api_contract_complete.yaml#/components/schemas/Package/properties/createdAt` |
| `updated_at` | `timestamptz` | 否 | `now()` | `03_api_contract_complete.yaml#/components/schemas/Package/properties/updatedAt` |

约束：
- `PRIMARY KEY (id)`
- `FOREIGN KEY (namespace_id) REFERENCES capability.namespaces (id) ON DELETE RESTRICT`
- `CHECK (visibility IN ('official','private'))`
- `CHECK (status IN ('active','archived'))`
- `UNIQUE (namespace_id, name)`
- `CHECK ((status = 'archived') = (archived_at IS NOT NULL))`

索引：
- `packages_namespace_list`: `(namespace_id, created_at DESC, id DESC)`

#### capability.runtime_versions

Strict PublisherContract schema; repository/digest must match contract and admitted namespace; descriptor is propagated unchanged into Build and execution。覆盖 F03, F04, F10。

| 字段 | 类型 | 可空 | 默认 | 字段来源 |
|---|---|---|---|---|
| `id` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/RuntimeVersion/properties/id` |
| `name` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/RuntimeVersion/properties/name` |
| `version_label` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/RuntimeVersion/properties/versionLabel` |
| `artifact_repository` | `text` | 否 | `—` | `02_database_schema_complete.md#capability.runtime_versions.artifact_repository` |
| `artifact_digest` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/RuntimeVersion/properties/artifactDigest` |
| `status` | `text` | 否 | `'approved'` | `03_api_contract_complete.yaml#/components/schemas/RuntimeVersion/properties/status` |
| `approved_by` | `text` | 否 | `—` | `02_database_schema_complete.md#capability.runtime_versions.approved_by` |
| `created_at` | `timestamptz` | 否 | `now()` | `03_api_contract_complete.yaml#/components/schemas/RuntimeVersion/properties/createdAt` |
| `updated_at` | `timestamptz` | 否 | `now()` | `02_database_schema_complete.md#capability.runtime_versions.updated_at` |
| `runtime_abi_version` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/RuntimeVersion/properties/runtimeAbiVersion` |
| `package_format_versions` | `text[]` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/RuntimeVersion/properties/packageFormatVersions` |
| `admission_receipt_id` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/RuntimeVersion/properties/admissionReceiptId` |
| `publisher_namespace_id` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/RuntimeVersion/properties/publisherNamespaceId` |
| `publisher_contract_digest` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/RuntimeVersion/properties/publisherContractDigest` |
| `publisher_contract` | `jsonb` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/RuntimeVersion/properties/publisherContract` |
| `publisher_contract_object_ref` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/RuntimeVersion/properties/publisherContractObjectRef` |

约束：
- `PRIMARY KEY (id)`
- `CHECK (status IN ('approved','deprecated','revoked'))`
- `UNIQUE (name, version_label)`
- `CHECK (artifact_digest ~ '^sha256:[0-9a-f]{64}$')`
- `CHECK (publisher_contract_digest ~ '^sha256:[0-9a-f]{64}$')`
- `FOREIGN KEY (publisher_namespace_id) REFERENCES capability.publisher_namespaces (id) ON DELETE RESTRICT`
- `CHECK ((jsonb_typeof(publisher_contract) = 'object') IS TRUE)`
- `CHECK ((publisher_contract->>'schemaVersion' = 'opl-publisher-contract/v1') IS TRUE)`
- `CHECK ((publisher_contract->>'kind' = 'runtime') IS TRUE)`
- `CHECK ((publisher_contract->>'publisherNamespaceId' = publisher_namespace_id) IS TRUE)`
- `CHECK ((publisher_contract #>> '{image,repository}' = artifact_repository) IS TRUE)`
- `CHECK ((publisher_contract #>> '{image,digest}' = artifact_digest) IS TRUE)`
- `CHECK ((publisher_contract #>> '{image,platform,os}' = 'linux') IS TRUE)`
- `CHECK ((publisher_contract #>> '{image,platform,architecture}' IN ('amd64','arm64')) IS TRUE)`
- `CHECK ((publisher_contract #>> '{applicationRevisionTemplate,image}' = artifact_repository || '@' || artifact_digest) IS TRUE)`
- `CHECK ((publisher_contract #>> '{applicationRevisionTemplate,platform}' = (publisher_contract #>> '{image,platform,os}') || '/' || (publisher_contract #>> '{image,platform,architecture}') || CASE WHEN publisher_contract #>> '{image,platform,variant}' IS NULL THEN '' ELSE '/' || (publisher_contract #>> '{image,platform,variant}') END) IS TRUE)`
- `CHECK ((publisher_contract->>'runtimeAbiVersion' = runtime_abi_version) IS TRUE)`
- `CHECK ((publisher_contract->'packageFormatVersions' = to_jsonb(package_format_versions)) IS TRUE)`

索引：
- `runtime_versions_status`: `(status, created_at DESC, id DESC)`
- `runtime_versions_publisher`: `(publisher_namespace_id)`

#### capability.webui_versions

Strict PublisherContract schema; repository/digest must match contract and admitted namespace; descriptor is propagated unchanged into Build and execution。覆盖 F03, F04, F10。

| 字段 | 类型 | 可空 | 默认 | 字段来源 |
|---|---|---|---|---|
| `id` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/WebuiVersion/properties/id` |
| `name` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/WebuiVersion/properties/name` |
| `version_label` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/WebuiVersion/properties/versionLabel` |
| `artifact_repository` | `text` | 否 | `—` | `02_database_schema_complete.md#capability.webui_versions.artifact_repository` |
| `artifact_digest` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/WebuiVersion/properties/artifactDigest` |
| `status` | `text` | 否 | `'approved'` | `03_api_contract_complete.yaml#/components/schemas/WebuiVersion/properties/status` |
| `approved_by` | `text` | 否 | `—` | `02_database_schema_complete.md#capability.webui_versions.approved_by` |
| `created_at` | `timestamptz` | 否 | `now()` | `03_api_contract_complete.yaml#/components/schemas/WebuiVersion/properties/createdAt` |
| `updated_at` | `timestamptz` | 否 | `now()` | `02_database_schema_complete.md#capability.webui_versions.updated_at` |
| `runtime_abi_versions` | `text[]` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/WebuiVersion/properties/runtimeAbiVersions` |
| `ui_protocol_version` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/WebuiVersion/properties/uiProtocolVersion` |
| `admission_receipt_id` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/WebuiVersion/properties/admissionReceiptId` |
| `publisher_namespace_id` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/WebuiVersion/properties/publisherNamespaceId` |
| `publisher_contract_digest` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/WebuiVersion/properties/publisherContractDigest` |
| `publisher_contract` | `jsonb` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/WebuiVersion/properties/publisherContract` |
| `publisher_contract_object_ref` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/WebuiVersion/properties/publisherContractObjectRef` |

约束：
- `PRIMARY KEY (id)`
- `CHECK (status IN ('approved','deprecated','revoked'))`
- `UNIQUE (name, version_label)`
- `CHECK (artifact_digest ~ '^sha256:[0-9a-f]{64}$')`
- `CHECK (publisher_contract_digest ~ '^sha256:[0-9a-f]{64}$')`
- `FOREIGN KEY (publisher_namespace_id) REFERENCES capability.publisher_namespaces (id) ON DELETE RESTRICT`
- `CHECK ((jsonb_typeof(publisher_contract) = 'object') IS TRUE)`
- `CHECK ((publisher_contract->>'schemaVersion' = 'opl-publisher-contract/v1') IS TRUE)`
- `CHECK ((publisher_contract->>'kind' = 'webui') IS TRUE)`
- `CHECK ((publisher_contract->>'publisherNamespaceId' = publisher_namespace_id) IS TRUE)`
- `CHECK ((publisher_contract #>> '{image,repository}' = artifact_repository) IS TRUE)`
- `CHECK ((publisher_contract #>> '{image,digest}' = artifact_digest) IS TRUE)`
- `CHECK ((publisher_contract #>> '{image,platform,os}' = 'linux') IS TRUE)`
- `CHECK ((publisher_contract #>> '{image,platform,architecture}' IN ('amd64','arm64')) IS TRUE)`
- `CHECK ((publisher_contract->'runtimeAbiVersions' = to_jsonb(runtime_abi_versions)) IS TRUE)`
- `CHECK ((publisher_contract->>'uiProtocolVersion' = ui_protocol_version) IS TRUE)`

索引：
- `webui_versions_status`: `(status, created_at DESC, id DESC)`
- `webui_versions_publisher`: `(publisher_namespace_id)`

#### capability.catalog_policies

按当前生效不可变策略选择默认Runtime/WebUI，不多处写is_default。覆盖 F03, F04。

| 字段 | 类型 | 可空 | 默认 | 字段来源 |
|---|---|---|---|---|
| `id` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/BuildRuntimePolicy/properties/id` |
| `runtime_version_id` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/BuildRuntimePolicy/properties/runtimeVersionId` |
| `default_webui_version_id` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/BuildRuntimePolicy/properties/defaultWebuiVersionId` |
| `policy_version` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/BuildRuntimePolicy/properties/policyVersion` |
| `published_by` | `text` | 否 | `—` | `02_database_schema_complete.md#capability.catalog_policies.published_by` |
| `effective_at` | `timestamptz` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/BuildRuntimePolicy/properties/effectiveAt` |
| `created_at` | `timestamptz` | 否 | `now()` | `03_api_contract_complete.yaml#/components/schemas/BuildRuntimePolicy/properties/createdAt` |

约束：
- `PRIMARY KEY (id)`
- `FOREIGN KEY (runtime_version_id) REFERENCES capability.runtime_versions (id) ON DELETE RESTRICT`
- `FOREIGN KEY (default_webui_version_id) REFERENCES capability.webui_versions (id) ON DELETE RESTRICT`
- `UNIQUE (policy_version)`

索引：
- `catalog_policies_effective`: `(effective_at DESC, id DESC)`

#### capability.package_versions

上传申请冻结sha256/size，实测相符才uploaded；构建状态只在Build。覆盖 F04, F05, F06。

| 字段 | 类型 | 可空 | 默认 | 字段来源 |
|---|---|---|---|---|
| `id` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/PackageVersion/properties/id` |
| `package_id` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/PackageVersion/properties/packageId` |
| `version_label` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/PackageVersion/properties/versionLabel` |
| `status` | `text` | 否 | `'upload_pending'` | `03_api_contract_complete.yaml#/components/schemas/PackageVersion/properties/status` |
| `sha256` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/PackageVersion/properties/sha256` |
| `size_bytes` | `bigint` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/PackageVersion/properties/sizeBytes` |
| `object_ref` | `text` | NULL | `—` | `02_database_schema_complete.md#capability.package_versions.object_ref` |
| `manifest` | `jsonb` | NULL | `—` | `02_database_schema_complete.md#capability.package_versions.manifest` |
| `validation_error_code` | `text` | NULL | `—` | `02_database_schema_complete.md#capability.package_versions.validation_error_code` |
| `created_by` | `text` | 否 | `—` | `02_database_schema_complete.md#capability.package_versions.created_by` |
| `verified_at` | `timestamptz` | NULL | `—` | `02_database_schema_complete.md#capability.package_versions.verified_at` |
| `created_at` | `timestamptz` | 否 | `now()` | `03_api_contract_complete.yaml#/components/schemas/PackageVersion/properties/createdAt` |
| `updated_at` | `timestamptz` | 否 | `now()` | `02_database_schema_complete.md#capability.package_versions.updated_at` |

约束：
- `PRIMARY KEY (id)`
- `FOREIGN KEY (package_id) REFERENCES capability.packages (id) ON DELETE RESTRICT`
- `CHECK (status IN ('upload_pending','uploaded','rejected'))`
- `UNIQUE (package_id, version_label)`
- `UNIQUE (id, package_id)`
- `CHECK (sha256 ~ '^sha256:[0-9a-f]{64}$')`
- `CHECK (size_bytes > 0)`
- `CHECK (status <> 'uploaded' OR (object_ref IS NOT NULL AND verified_at IS NOT NULL AND manifest IS NOT NULL))`

索引：
- `package_versions_list`: `(package_id, created_at DESC, id DESC)`

#### capability.upload_sessions

Storage multipart直传凭据由Capability签发；sizeBytes/sha256从PackageVersion、completedParts从已确认upload_chunks读回，不双写数组。覆盖 F04。

| 字段 | 类型 | 可空 | 默认 | 字段来源 |
|---|---|---|---|---|
| `id` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/UploadSession/properties/id` |
| `package_version_id` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/UploadSession/properties/packageVersionId` |
| `status` | `text` | 否 | `'uploading'` | `03_api_contract_complete.yaml#/components/schemas/UploadSession/properties/status` |
| `part_size_bytes` | `bigint` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/UploadSession/properties/partSizeBytes` |
| `object_ref` | `text` | 否 | `—` | `02_database_schema_complete.md#capability.upload_sessions.object_ref` |
| `provider_upload_ref` | `text` | 否 | `—` | `02_database_schema_complete.md#capability.upload_sessions.provider_upload_ref` |
| `expires_at` | `timestamptz` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/UploadSession/properties/expiresAt` |
| `created_at` | `timestamptz` | 否 | `now()` | `02_database_schema_complete.md#capability.upload_sessions.created_at` |
| `updated_at` | `timestamptz` | 否 | `now()` | `02_database_schema_complete.md#capability.upload_sessions.updated_at` |

约束：
- `PRIMARY KEY (id)`
- `FOREIGN KEY (package_version_id) REFERENCES capability.package_versions (id) ON DELETE RESTRICT`
- `CHECK (status IN ('uploading','completed','expired'))`
- `CHECK (part_size_bytes > 0)`

索引：
- `upload_sessions_active`: UNIQUE `(package_version_id)` WHERE `status = 'uploading'`

#### capability.upload_chunks

同session/partNumber固定sha256与size；确认后保存etag；unknown读取同Provider part，重签URL不创建第二分片。覆盖 F04。

| 字段 | 类型 | 可空 | 默认 | 字段来源 |
|---|---|---|---|---|
| `id` | `text` | 否 | `—` | `02_database_schema_complete.md#capability.upload_chunks.id` |
| `upload_session_id` | `text` | 否 | `—` | `02_database_schema_complete.md#capability.upload_chunks.upload_session_id` |
| `size_bytes` | `bigint` | 否 | `—` | `02_database_schema_complete.md#capability.upload_chunks.size_bytes` |
| `sha256` | `text` | 否 | `—` | `02_database_schema_complete.md#capability.upload_chunks.sha256` |
| `provider_part_ref` | `text` | 否 | `—` | `02_database_schema_complete.md#capability.upload_chunks.provider_part_ref` |
| `observation_result` | `text` | 否 | `—` | `02_database_schema_complete.md#capability.upload_chunks.observation_result` |
| `created_at` | `timestamptz` | 否 | `now()` | `02_database_schema_complete.md#capability.upload_chunks.created_at` |
| `part_number` | `integer` | 否 | `—` | `02_database_schema_complete.md#capability.upload_chunks.part_number` |
| `etag` | `text` | NULL | `—` | `02_database_schema_complete.md#capability.upload_chunks.etag` |

约束：
- `PRIMARY KEY (id)`
- `FOREIGN KEY (upload_session_id) REFERENCES capability.upload_sessions (id) ON DELETE RESTRICT`
- `CHECK (sha256 ~ '^sha256:[0-9a-f]{64}$')`
- `CHECK (observation_result IN ('confirmed','rejected','unknown'))`
- `UNIQUE (upload_session_id, part_number)`
- `CHECK (part_number > 0)`
- `CHECK (size_bytes > 0)`
- `CHECK (observation_result <> 'confirmed' OR etag IS NOT NULL)`

索引：
- `upload_chunks_session`: `(upload_session_id)`

#### capability.capability_versions

build引用五项齐全才ready；legacy_application保留旧exact revision/digest且五个构建引用全空，不伪造Package/Build；deleted仅墓碑。覆盖 F03, F05, F06, F07, F10。

| 字段 | 类型 | 可空 | 默认 | 字段来源 |
|---|---|---|---|---|
| `id` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/CapabilityVersion/properties/id` |
| `package_id` | `text` | NULL | `—` | `03_api_contract_complete.yaml#/components/schemas/CapabilityVersion/properties/packageId` |
| `package_version_id` | `text` | NULL | `—` | `03_api_contract_complete.yaml#/components/schemas/CapabilityVersion/properties/packageVersionId` |
| `build_job_id` | `text` | NULL | `—` | `03_api_contract_complete.yaml#/components/schemas/CapabilityVersion/properties/buildJobId` |
| `version_label` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/CapabilityVersion/properties/versionLabel` |
| `runtime_version_id` | `text` | NULL | `—` | `03_api_contract_complete.yaml#/components/schemas/CapabilityVersion/properties/runtimeVersionId` |
| `webui_version_id` | `text` | NULL | `—` | `03_api_contract_complete.yaml#/components/schemas/CapabilityVersion/properties/webuiVersionId` |
| `artifact_repository` | `text` | 否 | `—` | `02_database_schema_complete.md#capability.capability_versions.artifact_repository` |
| `artifact_digest` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/CapabilityVersion/properties/artifactDigest` |
| `status` | `text` | 否 | `'ready'` | `03_api_contract_complete.yaml#/components/schemas/CapabilityVersion/properties/status` |
| `model_requirements` | `jsonb` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/CapabilityVersion/properties/modelRequirements` |
| `data_compatibility` | `jsonb` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/CapabilityVersion/properties/dataCompatibility` |
| `provenance_evidence` | `jsonb` | 否 | `—` | `02_database_schema_complete.md#capability.capability_versions.provenance_evidence` |
| `deletion_operation_id` | `text` | NULL | `—` | `02_database_schema_complete.md#capability.capability_versions.deletion_operation_id` |
| `deleted_at` | `timestamptz` | NULL | `—` | `02_database_schema_complete.md#capability.capability_versions.deleted_at` |
| `created_at` | `timestamptz` | 否 | `now()` | `03_api_contract_complete.yaml#/components/schemas/CapabilityVersion/properties/createdAt` |
| `updated_at` | `timestamptz` | 否 | `now()` | `02_database_schema_complete.md#capability.capability_versions.updated_at` |
| `provenance` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/CapabilityVersion/properties/provenance` |
| `legacy_application_revision_id` | `text` | NULL | `—` | `03_api_contract_complete.yaml#/components/schemas/CapabilityVersion/properties/legacyApplicationRevisionId` |
| `deployment_descriptor` | `jsonb` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/CapabilityVersion/properties/deploymentDescriptor` |
| `deployment_descriptor_digest` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/CapabilityVersion/properties/deploymentDescriptorDigest` |
| `deployment_descriptor_object_ref` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/CapabilityVersion/properties/deploymentDescriptorObjectRef` |

约束：
- `PRIMARY KEY (id)`
- `FOREIGN KEY (package_id) REFERENCES capability.packages (id) ON DELETE RESTRICT`
- `FOREIGN KEY (package_version_id, package_id) REFERENCES capability.package_versions (id, package_id) ON DELETE RESTRICT`
- `FOREIGN KEY (runtime_version_id) REFERENCES capability.runtime_versions (id) ON DELETE RESTRICT`
- `FOREIGN KEY (webui_version_id) REFERENCES capability.webui_versions (id) ON DELETE RESTRICT`
- `CHECK (status IN ('ready','deprecated','deleting','deleted'))`
- `UNIQUE (build_job_id)`
- `CHECK (artifact_digest ~ '^sha256:[0-9a-f]{64}$')`
- `CHECK ((status = 'deleted') = (deleted_at IS NOT NULL))`
- `FOREIGN KEY (deletion_operation_id) REFERENCES capability.operations (id) ON DELETE RESTRICT`
- `CHECK (provenance IN ('build','legacy_application'))`
- `CHECK ((provenance = 'build' AND num_nonnulls(package_id, package_version_id, build_job_id, runtime_version_id, webui_version_id) = 5 AND legacy_application_revision_id IS NULL) OR (provenance = 'legacy_application' AND num_nonnulls(package_id, package_version_id, build_job_id, runtime_version_id, webui_version_id) = 0 AND legacy_application_revision_id IS NOT NULL))`
- `CHECK (deployment_descriptor_digest ~ '^sha256:[0-9a-f]{64}$')`
- `CHECK ((deployment_descriptor->>'schemaVersion' = 'opl-deployment-descriptor/v1') IS TRUE)`
- `CHECK ((deployment_descriptor #>> '{artifact,repository}' = artifact_repository) IS TRUE)`
- `CHECK ((deployment_descriptor #>> '{artifact,digest}' = artifact_digest) IS TRUE)`
- `CHECK ((deployment_descriptor #>> '{applicationRevision,image}' = artifact_repository || '@' || artifact_digest) IS TRUE)`
- `CHECK ((deployment_descriptor->>'provenance' = provenance) IS TRUE)`
- `CHECK (provenance <> 'legacy_application' OR ((deployment_descriptor->>'legacyApplicationRevisionId' = legacy_application_revision_id AND NOT (deployment_descriptor ?| ARRAY['packageVersionId','buildInputDigest','runtimeContract','runtimeContractReference','webuiContract','webuiContractReference'])) IS TRUE))`
- `CHECK (provenance <> 'build' OR ((deployment_descriptor->>'packageVersionId' = package_version_id AND deployment_descriptor #>> '{runtimeContractReference,versionId}' = runtime_version_id AND deployment_descriptor #>> '{webuiContractReference,versionId}' = webui_version_id) IS TRUE))`

索引：
- `capability_versions_list`: `(package_id, created_at DESC, id DESC)`
- `capability_versions_artifact`: `(artifact_repository, artifact_digest)`
- `capability_versions_input`: `(package_version_id)`
- `capability_versions_legacy`: UNIQUE `(legacy_application_revision_id)` WHERE `legacy_application_revision_id IS NOT NULL`

#### capability.reference_claims

Four-way ReferenceTarget maps to exactly one local FK; Bind records original operation/input digest; Release requires typed owner terminal receipt and confirmed readback。覆盖 F05, F06, F08, F10, F13, F15。

| 字段 | 类型 | 可空 | 默认 | 字段来源 |
|---|---|---|---|---|
| `id` | `text` | 否 | `—` | `02_database_schema_complete.md#capability.reference_claims.id` |
| `target_type` | `text` | 否 | `—` | `02_database_schema_complete.md#capability.reference_claims.target_type` |
| `package_version_id` | `text` | NULL | `—` | `02_database_schema_complete.md#capability.reference_claims.package_version_id` |
| `capability_version_id` | `text` | NULL | `—` | `02_database_schema_complete.md#capability.reference_claims.capability_version_id` |
| `runtime_version_id` | `text` | NULL | `—` | `02_database_schema_complete.md#capability.reference_claims.runtime_version_id` |
| `webui_version_id` | `text` | NULL | `—` | `02_database_schema_complete.md#capability.reference_claims.webui_version_id` |
| `claimant_owner` | `text` | 否 | `—` | `02_database_schema_complete.md#capability.reference_claims.claimant_owner` |
| `claimant_resource_id` | `text` | 否 | `—` | `02_database_schema_complete.md#capability.reference_claims.claimant_resource_id` |
| `purpose` | `text` | 否 | `—` | `02_database_schema_complete.md#capability.reference_claims.purpose` |
| `request_id` | `text` | 否 | `—` | `02_database_schema_complete.md#capability.reference_claims.request_id` |
| `bound_at` | `timestamptz` | NULL | `—` | `02_database_schema_complete.md#capability.reference_claims.bound_at` |
| `released_at` | `timestamptz` | NULL | `—` | `02_database_schema_complete.md#capability.reference_claims.released_at` |
| `release_evidence_ref` | `text` | NULL | `—` | `02_database_schema_complete.md#capability.reference_claims.release_evidence_ref` |
| `created_at` | `timestamptz` | 否 | `now()` | `02_database_schema_complete.md#capability.reference_claims.created_at` |
| `updated_at` | `timestamptz` | 否 | `now()` | `02_database_schema_complete.md#capability.reference_claims.updated_at` |
| `bound_operation_id` | `text` | NULL | `—` | `02_database_schema_complete.md#capability.reference_claims.bound_operation_id` |
| `bound_input_digest` | `text` | NULL | `—` | `02_database_schema_complete.md#capability.reference_claims.bound_input_digest` |
| `release_evidence` | `jsonb` | NULL | `—` | `02_database_schema_complete.md#capability.reference_claims.release_evidence` |

约束：
- `PRIMARY KEY (id)`
- `CHECK (target_type IN ('package_version','capability_version','runtime_version','webui_version'))`
- `CHECK (num_nonnulls(package_version_id, capability_version_id, runtime_version_id, webui_version_id) = 1)`
- `CHECK (released_at IS NULL OR release_evidence_ref IS NOT NULL)`
- `FOREIGN KEY (package_version_id) REFERENCES capability.package_versions (id) ON DELETE RESTRICT`
- `CHECK ((target_type = 'package_version') = (package_version_id IS NOT NULL))`
- `FOREIGN KEY (capability_version_id) REFERENCES capability.capability_versions (id) ON DELETE RESTRICT`
- `CHECK ((target_type = 'capability_version') = (capability_version_id IS NOT NULL))`
- `FOREIGN KEY (runtime_version_id) REFERENCES capability.runtime_versions (id) ON DELETE RESTRICT`
- `CHECK ((target_type = 'runtime_version') = (runtime_version_id IS NOT NULL))`
- `FOREIGN KEY (webui_version_id) REFERENCES capability.webui_versions (id) ON DELETE RESTRICT`
- `CHECK ((target_type = 'webui_version') = (webui_version_id IS NOT NULL))`
- `CHECK ((bound_at IS NULL AND bound_operation_id IS NULL AND bound_input_digest IS NULL) OR (bound_at IS NOT NULL AND bound_operation_id IS NOT NULL AND bound_input_digest IS NOT NULL))`
- `CHECK (bound_input_digest IS NULL OR bound_input_digest ~ '^sha256:[0-9a-f]{64}$')`
- `CHECK ((released_at IS NULL) = (release_evidence IS NULL))`

索引：
- `reference_claims_package_version`: UNIQUE `(package_version_id, claimant_owner, claimant_resource_id, purpose)` WHERE `released_at IS NULL AND package_version_id IS NOT NULL`
- `reference_claims_capability_version`: UNIQUE `(capability_version_id, claimant_owner, claimant_resource_id, purpose)` WHERE `released_at IS NULL AND capability_version_id IS NOT NULL`
- `reference_claims_runtime_version`: UNIQUE `(runtime_version_id, claimant_owner, claimant_resource_id, purpose)` WHERE `released_at IS NULL AND runtime_version_id IS NOT NULL`
- `reference_claims_webui_version`: UNIQUE `(webui_version_id, claimant_owner, claimant_resource_id, purpose)` WHERE `released_at IS NULL AND webui_version_id IS NOT NULL`

#### capability.outbox_events

本Owner状态事务同写immutable事件；revision只在本Owner聚合锁内分配。覆盖 F01, F04, F05, F08, F10, F11, F12, F13, F15, F16, F17。

| 字段 | 类型 | 可空 | 默认 | 字段来源 |
|---|---|---|---|---|
| `id` | `text` | 否 | `—` | `02_database_schema_complete.md#capability.outbox_events.id` |
| `event_type` | `text` | 否 | `—` | `02_database_schema_complete.md#capability.outbox_events.event_type` |
| `schema_version` | `integer` | 否 | `—` | `02_database_schema_complete.md#capability.outbox_events.schema_version` |
| `aggregate_type` | `text` | 否 | `—` | `02_database_schema_complete.md#capability.outbox_events.aggregate_type` |
| `aggregate_id` | `text` | 否 | `—` | `02_database_schema_complete.md#capability.outbox_events.aggregate_id` |
| `aggregate_revision` | `bigint` | 否 | `—` | `02_database_schema_complete.md#capability.outbox_events.aggregate_revision` |
| `tenant_id` | `text` | NULL | `—` | `02_database_schema_complete.md#capability.outbox_events.tenant_id` |
| `correlation_id` | `text` | 否 | `—` | `02_database_schema_complete.md#capability.outbox_events.correlation_id` |
| `causation_id` | `text` | NULL | `—` | `02_database_schema_complete.md#capability.outbox_events.causation_id` |
| `payload` | `jsonb` | 否 | `—` | `02_database_schema_complete.md#capability.outbox_events.payload` |
| `payload_sha256` | `text` | 否 | `—` | `02_database_schema_complete.md#capability.outbox_events.payload_sha256` |
| `occurred_at` | `timestamptz` | 否 | `—` | `02_database_schema_complete.md#capability.outbox_events.occurred_at` |
| `created_at` | `timestamptz` | 否 | `now()` | `02_database_schema_complete.md#capability.outbox_events.created_at` |

约束：
- `PRIMARY KEY (id)`
- `CHECK (schema_version > 0 AND aggregate_revision >= 0)`
- `CHECK (payload_sha256 ~ '^[0-9a-f]{64}$')`
- `UNIQUE (aggregate_type, aggregate_id, aggregate_revision, event_type)`

索引：
- `outbox_events_aggregate`: `(aggregate_type, aggregate_id, aggregate_revision)`

#### capability.outbox_deliveries

各consumer独立ACK，禁止单个ACK丢其他消费者事件；lease仅本库。覆盖 F05, F08, F10, F11, F12, F13, F15, F17。

| 字段 | 类型 | 可空 | 默认 | 字段来源 |
|---|---|---|---|---|
| `id` | `text` | 否 | `—` | `02_database_schema_complete.md#capability.outbox_deliveries.id` |
| `event_id` | `text` | 否 | `—` | `02_database_schema_complete.md#capability.outbox_deliveries.event_id` |
| `consumer_owner` | `text` | 否 | `—` | `02_database_schema_complete.md#capability.outbox_deliveries.consumer_owner` |
| `attempt_count` | `integer` | 否 | `0` | `02_database_schema_complete.md#capability.outbox_deliveries.attempt_count` |
| `next_attempt_at` | `timestamptz` | 否 | `now()` | `02_database_schema_complete.md#capability.outbox_deliveries.next_attempt_at` |
| `acknowledged_at` | `timestamptz` | NULL | `—` | `02_database_schema_complete.md#capability.outbox_deliveries.acknowledged_at` |
| `last_error_code` | `text` | NULL | `—` | `02_database_schema_complete.md#capability.outbox_deliveries.last_error_code` |
| `lease_token` | `text` | NULL | `—` | `02_database_schema_complete.md#capability.outbox_deliveries.lease_token` |
| `lease_until` | `timestamptz` | NULL | `—` | `02_database_schema_complete.md#capability.outbox_deliveries.lease_until` |
| `created_at` | `timestamptz` | 否 | `now()` | `02_database_schema_complete.md#capability.outbox_deliveries.created_at` |
| `updated_at` | `timestamptz` | 否 | `now()` | `02_database_schema_complete.md#capability.outbox_deliveries.updated_at` |

约束：
- `PRIMARY KEY (id)`
- `FOREIGN KEY (event_id) REFERENCES capability.outbox_events (id) ON DELETE RESTRICT`
- `UNIQUE (event_id, consumer_owner)`
- `CHECK (attempt_count >= 0)`
- `CHECK ((lease_token IS NULL) = (lease_until IS NULL))`

索引：
- `outbox_deliveries_pending`: `(next_attempt_at, id)` WHERE `acknowledged_at IS NULL`

#### capability.inbox_events

去重hash/业务状态/processed_at/后续Outbox同本库事务；乱序不能覆盖新版本。覆盖 F05, F08, F10, F11, F12, F13, F15, F17。

| 字段 | 类型 | 可空 | 默认 | 字段来源 |
|---|---|---|---|---|
| `id` | `text` | 否 | `—` | `02_database_schema_complete.md#capability.inbox_events.id` |
| `source_owner` | `text` | 否 | `—` | `02_database_schema_complete.md#capability.inbox_events.source_owner` |
| `source_event_id` | `text` | 否 | `—` | `02_database_schema_complete.md#capability.inbox_events.source_event_id` |
| `event_type` | `text` | 否 | `—` | `02_database_schema_complete.md#capability.inbox_events.event_type` |
| `schema_version` | `integer` | 否 | `—` | `02_database_schema_complete.md#capability.inbox_events.schema_version` |
| `aggregate_type` | `text` | 否 | `—` | `02_database_schema_complete.md#capability.inbox_events.aggregate_type` |
| `aggregate_id` | `text` | 否 | `—` | `02_database_schema_complete.md#capability.inbox_events.aggregate_id` |
| `aggregate_revision` | `bigint` | 否 | `—` | `02_database_schema_complete.md#capability.inbox_events.aggregate_revision` |
| `payload_sha256` | `text` | 否 | `—` | `02_database_schema_complete.md#capability.inbox_events.payload_sha256` |
| `payload` | `jsonb` | 否 | `—` | `02_database_schema_complete.md#capability.inbox_events.payload` |
| `received_at` | `timestamptz` | 否 | `now()` | `02_database_schema_complete.md#capability.inbox_events.received_at` |
| `processed_at` | `timestamptz` | NULL | `—` | `02_database_schema_complete.md#capability.inbox_events.processed_at` |
| `result_resource_id` | `text` | NULL | `—` | `02_database_schema_complete.md#capability.inbox_events.result_resource_id` |
| `error_code` | `text` | NULL | `—` | `02_database_schema_complete.md#capability.inbox_events.error_code` |

约束：
- `PRIMARY KEY (id)`
- `UNIQUE (source_owner, source_event_id)`
- `CHECK (schema_version > 0 AND aggregate_revision >= 0)`
- `CHECK (payload_sha256 ~ '^[0-9a-f]{64}$')`

索引：
- `inbox_events_pending`: `(received_at, id)` WHERE `processed_at IS NULL`
- `inbox_events_aggregate`: `(source_owner, aggregate_type, aggregate_id, aggregate_revision)`

#### capability.idempotency_records

命令创建同事务；operation_name是API操作名不是新Operation ID；安全响应无Key明文；义务存续无TTL清理。覆盖 F01, F03, F04, F05, F08, F10, F11, F12, F13, F14, F15, F16, F17。

| 字段 | 类型 | 可空 | 默认 | 字段来源 |
|---|---|---|---|---|
| `id` | `text` | 否 | `—` | `02_database_schema_complete.md#capability.idempotency_records.id` |
| `tenant_scope` | `text` | 否 | `—` | `02_database_schema_complete.md#capability.idempotency_records.tenant_scope` |
| `actor_scope` | `text` | 否 | `—` | `02_database_schema_complete.md#capability.idempotency_records.actor_scope` |
| `operation_name` | `text` | 否 | `—` | `02_database_schema_complete.md#capability.idempotency_records.operation_name` |
| `idempotency_key` | `text` | 否 | `—` | `02_database_schema_complete.md#capability.idempotency_records.idempotency_key` |
| `request_sha256` | `text` | 否 | `—` | `02_database_schema_complete.md#capability.idempotency_records.request_sha256` |
| `resource_id` | `text` | 否 | `—` | `02_database_schema_complete.md#capability.idempotency_records.resource_id` |
| `operation_id` | `text` | NULL | `—` | `02_database_schema_complete.md#capability.idempotency_records.operation_id` |
| `response_status` | `integer` | 否 | `—` | `02_database_schema_complete.md#capability.idempotency_records.response_status` |
| `response_body` | `jsonb` | 否 | `—` | `02_database_schema_complete.md#capability.idempotency_records.response_body` |
| `created_at` | `timestamptz` | 否 | `now()` | `02_database_schema_complete.md#capability.idempotency_records.created_at` |

约束：
- `PRIMARY KEY (id)`
- `UNIQUE (tenant_scope, actor_scope, operation_name, idempotency_key)`
- `CHECK (request_sha256 ~ '^[0-9a-f]{64}$')`
- `CHECK (response_status BETWEEN 100 AND 599)`

索引：
- `idempotency_records_resource`: `(resource_id)`

#### capability.operations

目标Owner持异步Operation；BFF路由无中央writer；Build创建201回Job；只有Workspace配Saga。覆盖 F01, F03, F04, F06, F08, F09, F10, F11, F12, F13, F14, F15, F17。

| 字段 | 类型 | 可空 | 默认 | 字段来源 |
|---|---|---|---|---|
| `id` | `text` | 否 | `—` | `02_database_schema_complete.md#capability.operations.id` |
| `tenant_id` | `text` | NULL | `—` | `02_database_schema_complete.md#capability.operations.tenant_id` |
| `actor_id` | `text` | 否 | `—` | `02_database_schema_complete.md#capability.operations.actor_id` |
| `kind` | `text` | 否 | `—` | `02_database_schema_complete.md#capability.operations.kind` |
| `resource_id` | `text` | 否 | `—` | `02_database_schema_complete.md#capability.operations.resource_id` |
| `status` | `text` | 否 | `'accepted'` | `02_database_schema_complete.md#capability.operations.status` |
| `stage` | `text` | 否 | `—` | `02_database_schema_complete.md#capability.operations.stage` |
| `error_code` | `text` | NULL | `—` | `02_database_schema_complete.md#capability.operations.error_code` |
| `observation_result` | `text` | NULL | `—` | `02_database_schema_complete.md#capability.operations.observation_result` |
| `request_id` | `text` | 否 | `—` | `02_database_schema_complete.md#capability.operations.request_id` |
| `accepted_input` | `jsonb` | 否 | `—` | `02_database_schema_complete.md#capability.operations.accepted_input` |
| `result` | `jsonb` | NULL | `—` | `02_database_schema_complete.md#capability.operations.result` |
| `worker_lease_token` | `text` | NULL | `—` | `02_database_schema_complete.md#capability.operations.worker_lease_token` |
| `worker_lease_until` | `timestamptz` | NULL | `—` | `02_database_schema_complete.md#capability.operations.worker_lease_until` |
| `started_at` | `timestamptz` | NULL | `—` | `02_database_schema_complete.md#capability.operations.started_at` |
| `completed_at` | `timestamptz` | NULL | `—` | `02_database_schema_complete.md#capability.operations.completed_at` |
| `created_at` | `timestamptz` | 否 | `now()` | `02_database_schema_complete.md#capability.operations.created_at` |
| `updated_at` | `timestamptz` | 否 | `now()` | `02_database_schema_complete.md#capability.operations.updated_at` |

约束：
- `PRIMARY KEY (id)`
- `CHECK (status IN ('accepted','running','awaiting_confirmation','succeeded','failed','needs_attention','cancelled'))`
- `CHECK (observation_result IN ('confirmed','rejected','unknown'))`
- `CHECK ((worker_lease_token IS NULL) = (worker_lease_until IS NULL))`
- `CHECK (status <> 'succeeded' OR completed_at IS NOT NULL)`

索引：
- `operations_resource`: `(resource_id, created_at DESC, id DESC)`
- `operations_tenant_list`: `(tenant_id, created_at DESC, id DESC)`
- `operations_recovery`: `(status, updated_at)`

#### capability.publisher_namespaces

Publisher Registry namespaces are distinct from Tenant Agent groups; third-party publishers use separately admitted prefixes。覆盖 F03。

| 字段 | 类型 | 可空 | 默认 | 字段来源 |
|---|---|---|---|---|
| `id` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/PublisherNamespace/properties/id` |
| `name` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/PublisherNamespace/properties/name` |
| `kind` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/PublisherNamespace/properties/kind` |
| `registry_id` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/PublisherNamespace/properties/registryId` |
| `repository_prefix` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/PublisherNamespace/properties/repositoryPrefix` |
| `admission_receipt_id` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/PublisherNamespace/properties/admissionReceiptId` |
| `status` | `text` | 否 | `'approved'` | `03_api_contract_complete.yaml#/components/schemas/PublisherNamespace/properties/status` |
| `created_at` | `timestamptz` | 否 | `now()` | `03_api_contract_complete.yaml#/components/schemas/PublisherNamespace/properties/createdAt` |
| `updated_at` | `timestamptz` | 否 | `now()` | `02_database_schema_complete.md#capability.publisher_namespaces.updated_at` |

约束：
- `PRIMARY KEY (id)`
- `CHECK (kind IN ('official','third_party'))`
- `CHECK (status IN ('approved','revoked'))`
- `UNIQUE (registry_id, repository_prefix)`
- `UNIQUE (name)`
- `CHECK (repository_prefix <> '' AND repository_prefix NOT LIKE '%..%' AND repository_prefix NOT LIKE '%@%' AND repository_prefix NOT LIKE '%://%')`

索引：
- `publisher_namespaces_catalog`: `(kind, status, created_at DESC, id DESC)`

### build

Database `opl_build` · Schema `build` · Writer `opl_build_writer`。

#### build.build_jobs

createBuild(packageVersionId,webuiVersionId)冻结批准Runtime/catalogPolicy/digests/claims；Operation与Job同库创建，注册确认后succeeded。覆盖 F04, F05, F06。

| 字段 | 类型 | 可空 | 默认 | 字段来源 |
|---|---|---|---|---|
| `id` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/BuildJob/properties/id` |
| `tenant_id` | `text` | NULL | `—` | `02_database_schema_complete.md#build.build_jobs.tenant_id` |
| `package_version_id` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/BuildJob/properties/packageVersionId` |
| `runtime_version_id` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/BuildJob/properties/runtimeVersionId` |
| `webui_version_id` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/BuildJob/properties/webuiVersionId` |
| `input_digest` | `text` | 否 | `—` | `02_database_schema_complete.md#build.build_jobs.input_digest` |
| `input_snapshot` | `jsonb` | 否 | `—` | `02_database_schema_complete.md#build.build_jobs.input_snapshot` |
| `status` | `text` | 否 | `'queued'` | `03_api_contract_complete.yaml#/components/schemas/BuildJob/properties/status` |
| `stage` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/BuildJob/properties/stage` |
| `artifact_digest` | `text` | NULL | `—` | `03_api_contract_complete.yaml#/components/schemas/BuildJob/properties/artifactDigest` |
| `result_capability_version_id` | `text` | NULL | `—` | `03_api_contract_complete.yaml#/components/schemas/BuildJob/properties/resultCapabilityVersionId` |
| `retry_of_build_job_id` | `text` | NULL | `—` | `03_api_contract_complete.yaml#/components/schemas/BuildJob/properties/retryOfBuildJobId` |
| `executor_ref` | `text` | NULL | `—` | `02_database_schema_complete.md#build.build_jobs.executor_ref` |
| `worker_lease_token` | `text` | NULL | `—` | `02_database_schema_complete.md#build.build_jobs.worker_lease_token` |
| `worker_lease_until` | `timestamptz` | NULL | `—` | `02_database_schema_complete.md#build.build_jobs.worker_lease_until` |
| `error_code` | `text` | NULL | `—` | `03_api_contract_complete.yaml#/components/schemas/BuildJob/properties/errorCode` |
| `request_id` | `text` | 否 | `—` | `02_database_schema_complete.md#build.build_jobs.request_id` |
| `created_by` | `text` | 否 | `—` | `02_database_schema_complete.md#build.build_jobs.created_by` |
| `started_at` | `timestamptz` | NULL | `—` | `02_database_schema_complete.md#build.build_jobs.started_at` |
| `finished_at` | `timestamptz` | NULL | `—` | `02_database_schema_complete.md#build.build_jobs.finished_at` |
| `created_at` | `timestamptz` | 否 | `now()` | `03_api_contract_complete.yaml#/components/schemas/BuildJob/properties/createdAt` |
| `updated_at` | `timestamptz` | 否 | `now()` | `03_api_contract_complete.yaml#/components/schemas/BuildJob/properties/updatedAt` |
| `operation_id` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/BuildJob/properties/operationId` |
| `catalog_policy_id` | `text` | 否 | `—` | `02_database_schema_complete.md#build.build_jobs.catalog_policy_id` |

约束：
- `PRIMARY KEY (id)`
- `FOREIGN KEY (retry_of_build_job_id) REFERENCES build.build_jobs (id) ON DELETE RESTRICT`
- `CHECK (status IN ('queued','validating','building','pushing','registering','succeeded','failed','needs_attention'))`
- `CHECK (input_digest ~ '^sha256:[0-9a-f]{64}$')`
- `CHECK (artifact_digest IS NULL OR artifact_digest ~ '^sha256:[0-9a-f]{64}$')`
- `CHECK (status <> 'succeeded' OR (artifact_digest IS NOT NULL AND result_capability_version_id IS NOT NULL AND finished_at IS NOT NULL))`
- `CHECK ((worker_lease_token IS NULL) = (worker_lease_until IS NULL))`
- `FOREIGN KEY (operation_id) REFERENCES build.operations (id) ON DELETE RESTRICT`

索引：
- `build_jobs_tenant_list`: `(tenant_id, created_at DESC, id DESC)`
- `build_jobs_dispatch`: `(status, created_at)`
- `build_jobs_retry`: `(retry_of_build_job_id)`

#### build.build_artifacts

Build输出不可变证据，非第二Registry/版本可见性writer。覆盖 F05, F06, F17。

| 字段 | 类型 | 可空 | 默认 | 字段来源 |
|---|---|---|---|---|
| `id` | `text` | 否 | `—` | `02_database_schema_complete.md#build.build_artifacts.id` |
| `build_job_id` | `text` | 否 | `—` | `02_database_schema_complete.md#build.build_artifacts.build_job_id` |
| `artifact_repository` | `text` | 否 | `—` | `02_database_schema_complete.md#build.build_artifacts.artifact_repository` |
| `artifact_digest` | `text` | 否 | `—` | `02_database_schema_complete.md#build.build_artifacts.artifact_digest` |
| `size_bytes` | `bigint` | 否 | `—` | `02_database_schema_complete.md#build.build_artifacts.size_bytes` |
| `provenance` | `jsonb` | 否 | `—` | `02_database_schema_complete.md#build.build_artifacts.provenance` |
| `verification_evidence_ref` | `text` | 否 | `—` | `02_database_schema_complete.md#build.build_artifacts.verification_evidence_ref` |
| `created_at` | `timestamptz` | 否 | `now()` | `02_database_schema_complete.md#build.build_artifacts.created_at` |
| `deployment_descriptor` | `jsonb` | 否 | `—` | `02_database_schema_complete.md#build.build_artifacts.deployment_descriptor` |
| `deployment_descriptor_digest` | `text` | 否 | `—` | `02_database_schema_complete.md#build.build_artifacts.deployment_descriptor_digest` |
| `deployment_descriptor_object_ref` | `text` | 否 | `—` | `02_database_schema_complete.md#build.build_artifacts.deployment_descriptor_object_ref` |

约束：
- `PRIMARY KEY (id)`
- `FOREIGN KEY (build_job_id) REFERENCES build.build_jobs (id) ON DELETE RESTRICT`
- `UNIQUE (build_job_id)`
- `CHECK (artifact_digest ~ '^sha256:[0-9a-f]{64}$')`
- `CHECK (size_bytes > 0)`
- `CHECK (deployment_descriptor_digest ~ '^sha256:[0-9a-f]{64}$')`
- `CHECK ((deployment_descriptor->>'schemaVersion' = 'opl-deployment-descriptor/v1') IS TRUE)`
- `CHECK ((deployment_descriptor #>> '{artifact,repository}' = artifact_repository) IS TRUE)`
- `CHECK ((deployment_descriptor #>> '{artifact,digest}' = artifact_digest) IS TRUE)`
- `CHECK ((deployment_descriptor #>> '{applicationRevision,image}' = artifact_repository || '@' || artifact_digest) IS TRUE)`

索引：
- `build_artifacts_digest`: `(artifact_repository, artifact_digest)`

#### build.build_logs

真实日志顺序分页，写入前按明确敏感字段清单去密，不记录凭据。覆盖 F05。

| 字段 | 类型 | 可空 | 默认 | 字段来源 |
|---|---|---|---|---|
| `id` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/BuildLog/properties/id` |
| `build_job_id` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/BuildLog/properties/buildJobId` |
| `sequence` | `bigint` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/BuildLog/properties/sequence` |
| `stage` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/BuildLog/properties/stage` |
| `message` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/BuildLog/properties/message` |
| `created_at` | `timestamptz` | 否 | `now()` | `03_api_contract_complete.yaml#/components/schemas/BuildLog/properties/createdAt` |
| `level` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/BuildLog/properties/level` |

约束：
- `PRIMARY KEY (id)`
- `FOREIGN KEY (build_job_id) REFERENCES build.build_jobs (id) ON DELETE RESTRICT`
- `UNIQUE (build_job_id, sequence)`
- `CHECK (sequence >= 0)`
- `CHECK (level IN ('info','warning','error'))`

索引：
- `build_logs_cursor`: `(build_job_id, sequence)`

#### build.outbox_events

本Owner状态事务同写immutable事件；revision只在本Owner聚合锁内分配。覆盖 F01, F04, F05, F08, F10, F11, F12, F13, F15, F16, F17。

| 字段 | 类型 | 可空 | 默认 | 字段来源 |
|---|---|---|---|---|
| `id` | `text` | 否 | `—` | `02_database_schema_complete.md#build.outbox_events.id` |
| `event_type` | `text` | 否 | `—` | `02_database_schema_complete.md#build.outbox_events.event_type` |
| `schema_version` | `integer` | 否 | `—` | `02_database_schema_complete.md#build.outbox_events.schema_version` |
| `aggregate_type` | `text` | 否 | `—` | `02_database_schema_complete.md#build.outbox_events.aggregate_type` |
| `aggregate_id` | `text` | 否 | `—` | `02_database_schema_complete.md#build.outbox_events.aggregate_id` |
| `aggregate_revision` | `bigint` | 否 | `—` | `02_database_schema_complete.md#build.outbox_events.aggregate_revision` |
| `tenant_id` | `text` | NULL | `—` | `02_database_schema_complete.md#build.outbox_events.tenant_id` |
| `correlation_id` | `text` | 否 | `—` | `02_database_schema_complete.md#build.outbox_events.correlation_id` |
| `causation_id` | `text` | NULL | `—` | `02_database_schema_complete.md#build.outbox_events.causation_id` |
| `payload` | `jsonb` | 否 | `—` | `02_database_schema_complete.md#build.outbox_events.payload` |
| `payload_sha256` | `text` | 否 | `—` | `02_database_schema_complete.md#build.outbox_events.payload_sha256` |
| `occurred_at` | `timestamptz` | 否 | `—` | `02_database_schema_complete.md#build.outbox_events.occurred_at` |
| `created_at` | `timestamptz` | 否 | `now()` | `02_database_schema_complete.md#build.outbox_events.created_at` |

约束：
- `PRIMARY KEY (id)`
- `CHECK (schema_version > 0 AND aggregate_revision >= 0)`
- `CHECK (payload_sha256 ~ '^[0-9a-f]{64}$')`
- `UNIQUE (aggregate_type, aggregate_id, aggregate_revision, event_type)`

索引：
- `outbox_events_aggregate`: `(aggregate_type, aggregate_id, aggregate_revision)`

#### build.outbox_deliveries

各consumer独立ACK，禁止单个ACK丢其他消费者事件；lease仅本库。覆盖 F05, F08, F10, F11, F12, F13, F15, F17。

| 字段 | 类型 | 可空 | 默认 | 字段来源 |
|---|---|---|---|---|
| `id` | `text` | 否 | `—` | `02_database_schema_complete.md#build.outbox_deliveries.id` |
| `event_id` | `text` | 否 | `—` | `02_database_schema_complete.md#build.outbox_deliveries.event_id` |
| `consumer_owner` | `text` | 否 | `—` | `02_database_schema_complete.md#build.outbox_deliveries.consumer_owner` |
| `attempt_count` | `integer` | 否 | `0` | `02_database_schema_complete.md#build.outbox_deliveries.attempt_count` |
| `next_attempt_at` | `timestamptz` | 否 | `now()` | `02_database_schema_complete.md#build.outbox_deliveries.next_attempt_at` |
| `acknowledged_at` | `timestamptz` | NULL | `—` | `02_database_schema_complete.md#build.outbox_deliveries.acknowledged_at` |
| `last_error_code` | `text` | NULL | `—` | `02_database_schema_complete.md#build.outbox_deliveries.last_error_code` |
| `lease_token` | `text` | NULL | `—` | `02_database_schema_complete.md#build.outbox_deliveries.lease_token` |
| `lease_until` | `timestamptz` | NULL | `—` | `02_database_schema_complete.md#build.outbox_deliveries.lease_until` |
| `created_at` | `timestamptz` | 否 | `now()` | `02_database_schema_complete.md#build.outbox_deliveries.created_at` |
| `updated_at` | `timestamptz` | 否 | `now()` | `02_database_schema_complete.md#build.outbox_deliveries.updated_at` |

约束：
- `PRIMARY KEY (id)`
- `FOREIGN KEY (event_id) REFERENCES build.outbox_events (id) ON DELETE RESTRICT`
- `UNIQUE (event_id, consumer_owner)`
- `CHECK (attempt_count >= 0)`
- `CHECK ((lease_token IS NULL) = (lease_until IS NULL))`

索引：
- `outbox_deliveries_pending`: `(next_attempt_at, id)` WHERE `acknowledged_at IS NULL`

#### build.inbox_events

去重hash/业务状态/processed_at/后续Outbox同本库事务；乱序不能覆盖新版本。覆盖 F05, F08, F10, F11, F12, F13, F15, F17。

| 字段 | 类型 | 可空 | 默认 | 字段来源 |
|---|---|---|---|---|
| `id` | `text` | 否 | `—` | `02_database_schema_complete.md#build.inbox_events.id` |
| `source_owner` | `text` | 否 | `—` | `02_database_schema_complete.md#build.inbox_events.source_owner` |
| `source_event_id` | `text` | 否 | `—` | `02_database_schema_complete.md#build.inbox_events.source_event_id` |
| `event_type` | `text` | 否 | `—` | `02_database_schema_complete.md#build.inbox_events.event_type` |
| `schema_version` | `integer` | 否 | `—` | `02_database_schema_complete.md#build.inbox_events.schema_version` |
| `aggregate_type` | `text` | 否 | `—` | `02_database_schema_complete.md#build.inbox_events.aggregate_type` |
| `aggregate_id` | `text` | 否 | `—` | `02_database_schema_complete.md#build.inbox_events.aggregate_id` |
| `aggregate_revision` | `bigint` | 否 | `—` | `02_database_schema_complete.md#build.inbox_events.aggregate_revision` |
| `payload_sha256` | `text` | 否 | `—` | `02_database_schema_complete.md#build.inbox_events.payload_sha256` |
| `payload` | `jsonb` | 否 | `—` | `02_database_schema_complete.md#build.inbox_events.payload` |
| `received_at` | `timestamptz` | 否 | `now()` | `02_database_schema_complete.md#build.inbox_events.received_at` |
| `processed_at` | `timestamptz` | NULL | `—` | `02_database_schema_complete.md#build.inbox_events.processed_at` |
| `result_resource_id` | `text` | NULL | `—` | `02_database_schema_complete.md#build.inbox_events.result_resource_id` |
| `error_code` | `text` | NULL | `—` | `02_database_schema_complete.md#build.inbox_events.error_code` |

约束：
- `PRIMARY KEY (id)`
- `UNIQUE (source_owner, source_event_id)`
- `CHECK (schema_version > 0 AND aggregate_revision >= 0)`
- `CHECK (payload_sha256 ~ '^[0-9a-f]{64}$')`

索引：
- `inbox_events_pending`: `(received_at, id)` WHERE `processed_at IS NULL`
- `inbox_events_aggregate`: `(source_owner, aggregate_type, aggregate_id, aggregate_revision)`

#### build.idempotency_records

命令创建同事务；operation_name是API操作名不是新Operation ID；安全响应无Key明文；义务存续无TTL清理。覆盖 F01, F03, F04, F05, F08, F10, F11, F12, F13, F14, F15, F16, F17。

| 字段 | 类型 | 可空 | 默认 | 字段来源 |
|---|---|---|---|---|
| `id` | `text` | 否 | `—` | `02_database_schema_complete.md#build.idempotency_records.id` |
| `tenant_scope` | `text` | 否 | `—` | `02_database_schema_complete.md#build.idempotency_records.tenant_scope` |
| `actor_scope` | `text` | 否 | `—` | `02_database_schema_complete.md#build.idempotency_records.actor_scope` |
| `operation_name` | `text` | 否 | `—` | `02_database_schema_complete.md#build.idempotency_records.operation_name` |
| `idempotency_key` | `text` | 否 | `—` | `02_database_schema_complete.md#build.idempotency_records.idempotency_key` |
| `request_sha256` | `text` | 否 | `—` | `02_database_schema_complete.md#build.idempotency_records.request_sha256` |
| `resource_id` | `text` | 否 | `—` | `02_database_schema_complete.md#build.idempotency_records.resource_id` |
| `operation_id` | `text` | NULL | `—` | `02_database_schema_complete.md#build.idempotency_records.operation_id` |
| `response_status` | `integer` | 否 | `—` | `02_database_schema_complete.md#build.idempotency_records.response_status` |
| `response_body` | `jsonb` | 否 | `—` | `02_database_schema_complete.md#build.idempotency_records.response_body` |
| `created_at` | `timestamptz` | 否 | `now()` | `02_database_schema_complete.md#build.idempotency_records.created_at` |

约束：
- `PRIMARY KEY (id)`
- `UNIQUE (tenant_scope, actor_scope, operation_name, idempotency_key)`
- `CHECK (request_sha256 ~ '^[0-9a-f]{64}$')`
- `CHECK (response_status BETWEEN 100 AND 599)`

索引：
- `idempotency_records_resource`: `(resource_id)`

#### build.operations

目标Owner持异步Operation；BFF路由无中央writer；Build创建201回Job；只有Workspace配Saga。覆盖 F01, F03, F04, F06, F08, F09, F10, F11, F12, F13, F14, F15, F17。

| 字段 | 类型 | 可空 | 默认 | 字段来源 |
|---|---|---|---|---|
| `id` | `text` | 否 | `—` | `02_database_schema_complete.md#build.operations.id` |
| `tenant_id` | `text` | NULL | `—` | `02_database_schema_complete.md#build.operations.tenant_id` |
| `actor_id` | `text` | 否 | `—` | `02_database_schema_complete.md#build.operations.actor_id` |
| `kind` | `text` | 否 | `—` | `02_database_schema_complete.md#build.operations.kind` |
| `resource_id` | `text` | 否 | `—` | `02_database_schema_complete.md#build.operations.resource_id` |
| `status` | `text` | 否 | `'accepted'` | `02_database_schema_complete.md#build.operations.status` |
| `stage` | `text` | 否 | `—` | `02_database_schema_complete.md#build.operations.stage` |
| `error_code` | `text` | NULL | `—` | `02_database_schema_complete.md#build.operations.error_code` |
| `observation_result` | `text` | NULL | `—` | `02_database_schema_complete.md#build.operations.observation_result` |
| `request_id` | `text` | 否 | `—` | `02_database_schema_complete.md#build.operations.request_id` |
| `accepted_input` | `jsonb` | 否 | `—` | `02_database_schema_complete.md#build.operations.accepted_input` |
| `result` | `jsonb` | NULL | `—` | `02_database_schema_complete.md#build.operations.result` |
| `worker_lease_token` | `text` | NULL | `—` | `02_database_schema_complete.md#build.operations.worker_lease_token` |
| `worker_lease_until` | `timestamptz` | NULL | `—` | `02_database_schema_complete.md#build.operations.worker_lease_until` |
| `started_at` | `timestamptz` | NULL | `—` | `02_database_schema_complete.md#build.operations.started_at` |
| `completed_at` | `timestamptz` | NULL | `—` | `02_database_schema_complete.md#build.operations.completed_at` |
| `created_at` | `timestamptz` | 否 | `now()` | `02_database_schema_complete.md#build.operations.created_at` |
| `updated_at` | `timestamptz` | 否 | `now()` | `02_database_schema_complete.md#build.operations.updated_at` |

约束：
- `PRIMARY KEY (id)`
- `CHECK (status IN ('accepted','running','awaiting_confirmation','succeeded','failed','needs_attention','cancelled'))`
- `CHECK (observation_result IN ('confirmed','rejected','unknown'))`
- `CHECK ((worker_lease_token IS NULL) = (worker_lease_until IS NULL))`
- `CHECK (status <> 'succeeded' OR completed_at IS NOT NULL)`

索引：
- `operations_resource`: `(resource_id, created_at DESC, id DESC)`
- `operations_tenant_list`: `(tenant_id, created_at DESC, id DESC)`
- `operations_recovery`: `(status, updated_at)`

### workspace

Database `opl_workspace` · Schema `workspace` · Writer `opl_workspace_writer`。

#### workspace.workspaces

当前已接受业务选择；迁移裸资源capabilityVersionId可空，UI不得称已部署；expiresAt从订阅投影。覆盖 F08, F09, F10, F11, F12, F13, F16。

| 字段 | 类型 | 可空 | 默认 | 字段来源 |
|---|---|---|---|---|
| `id` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/Workspace/properties/id` |
| `tenant_id` | `text` | 否 | `—` | `02_database_schema_complete.md#workspace.workspaces.tenant_id` |
| `name` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/Workspace/properties/name` |
| `status` | `text` | 否 | `'provisioning'` | `03_api_contract_complete.yaml#/components/schemas/Workspace/properties/status` |
| `capability_version_id` | `text` | NULL | `—` | `03_api_contract_complete.yaml#/components/schemas/Workspace/properties/capabilityVersionId` |
| `compute_plan_id` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/Workspace/properties/computePlanId` |
| `storage_plan_id` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/Workspace/properties/storagePlanId` |
| `active_deployment_id` | `text` | NULL | `—` | `03_api_contract_complete.yaml#/components/schemas/Workspace/properties/activeDeploymentId` |
| `model_configuration_version` | `bigint` | 否 | `0` | `03_api_contract_complete.yaml#/components/schemas/Workspace/properties/modelConfigurationVersion` |
| `active_operation_id` | `text` | NULL | `—` | `02_database_schema_complete.md#workspace.workspaces.active_operation_id` |
| `legacy_origin_id` | `text` | NULL | `—` | `02_database_schema_complete.md#workspace.workspaces.legacy_origin_id` |
| `created_by` | `text` | 否 | `—` | `02_database_schema_complete.md#workspace.workspaces.created_by` |
| `deleted_at` | `timestamptz` | NULL | `—` | `02_database_schema_complete.md#workspace.workspaces.deleted_at` |
| `created_at` | `timestamptz` | 否 | `now()` | `03_api_contract_complete.yaml#/components/schemas/Workspace/properties/createdAt` |
| `updated_at` | `timestamptz` | 否 | `now()` | `03_api_contract_complete.yaml#/components/schemas/Workspace/properties/updatedAt` |
| `delivery_model` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/Workspace/properties/deliveryModel` |
| `version` | `bigint` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/Workspace/properties/version` |
| `execution_epoch` | `bigint` | 否 | `0` | `02_database_schema_complete.md#workspace.workspaces.execution_epoch` |
| `selected_route_generation` | `bigint` | NULL | `—` | `02_database_schema_complete.md#workspace.workspaces.selected_route_generation` |
| `selected_execution_epoch` | `bigint` | NULL | `—` | `02_database_schema_complete.md#workspace.workspaces.selected_execution_epoch` |

约束：
- `PRIMARY KEY (id)`
- `CHECK (status IN ('provisioning','active','updating','suspended','deleting','deleted','failed','needs_attention'))`
- `CHECK (model_configuration_version >= 0)`
- `CHECK ((status = 'deleted') = (deleted_at IS NOT NULL))`
- `FOREIGN KEY (active_deployment_id, id) REFERENCES workspace.deployments (id, workspace_id) ON DELETE RESTRICT`
- `FOREIGN KEY (active_operation_id) REFERENCES workspace.operations (id) ON DELETE RESTRICT`
- `CHECK (delivery_model IN ('legacy_resource_only','imported_application','agent_saas'))`
- `CHECK (version >= 0)`
- `CHECK ((delivery_model = 'legacy_resource_only' AND capability_version_id IS NULL AND active_deployment_id IS NULL) OR (delivery_model IN ('imported_application','agent_saas') AND capability_version_id IS NOT NULL))`
- `CHECK (execution_epoch >= 0)`
- `CHECK ((selected_route_generation IS NULL) = (selected_execution_epoch IS NULL))`
- `CHECK (selected_route_generation IS NULL OR selected_route_generation >= 0)`
- `CHECK (selected_execution_epoch IS NULL OR (selected_execution_epoch >= 0 AND selected_execution_epoch <= execution_epoch))`
- `UNIQUE (id, tenant_id)`

索引：
- `workspaces_tenant_list`: `(tenant_id, created_at DESC, id DESC)`
- `workspaces_legacy`: UNIQUE `(legacy_origin_id)` WHERE `legacy_origin_id IS NOT NULL`

#### workspace.deployments

workspaces.active_deployment_id唯一选中；同事务切换指针和supersede旧部署，不由Runtime写。覆盖 F08, F09, F10。

| 字段 | 类型 | 可空 | 默认 | 字段来源 |
|---|---|---|---|---|
| `id` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/Deployment/properties/id` |
| `workspace_id` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/Deployment/properties/workspaceId` |
| `capability_version_id` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/Deployment/properties/capabilityVersionId` |
| `artifact_digest` | `text` | 否 | `—` | `02_database_schema_complete.md#workspace.deployments.artifact_digest` |
| `reference_claim_id` | `text` | 否 | `—` | `02_database_schema_complete.md#workspace.deployments.reference_claim_id` |
| `runtime_instance_id` | `text` | NULL | `—` | `03_api_contract_complete.yaml#/components/schemas/Deployment/properties/runtimeInstanceId` |
| `previous_deployment_id` | `text` | NULL | `—` | `03_api_contract_complete.yaml#/components/schemas/Deployment/properties/previousDeploymentId` |
| `operation_id` | `text` | 否 | `—` | `02_database_schema_complete.md#workspace.deployments.operation_id` |
| `status` | `text` | 否 | `'queued'` | `03_api_contract_complete.yaml#/components/schemas/Deployment/properties/status` |
| `data_compatibility` | `jsonb` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/Deployment/properties/dataCompatibility` |
| `data_migration_evidence_ref` | `text` | NULL | `—` | `02_database_schema_complete.md#workspace.deployments.data_migration_evidence_ref` |
| `verification_evidence_ref` | `text` | NULL | `—` | `02_database_schema_complete.md#workspace.deployments.verification_evidence_ref` |
| `error_code` | `text` | NULL | `—` | `02_database_schema_complete.md#workspace.deployments.error_code` |
| `activated_at` | `timestamptz` | NULL | `—` | `02_database_schema_complete.md#workspace.deployments.activated_at` |
| `created_at` | `timestamptz` | 否 | `now()` | `03_api_contract_complete.yaml#/components/schemas/Deployment/properties/createdAt` |
| `updated_at` | `timestamptz` | 否 | `now()` | `03_api_contract_complete.yaml#/components/schemas/Deployment/properties/updatedAt` |
| `execution_epoch` | `bigint` | 否 | `—` | `02_database_schema_complete.md#workspace.deployments.execution_epoch` |
| `confirmed_route_switch_id` | `text` | NULL | `—` | `02_database_schema_complete.md#workspace.deployments.confirmed_route_switch_id` |
| `selection_commit_receipt_id` | `text` | NULL | `—` | `02_database_schema_complete.md#workspace.deployments.selection_commit_receipt_id` |

约束：
- `PRIMARY KEY (id)`
- `FOREIGN KEY (workspace_id) REFERENCES workspace.workspaces (id) ON DELETE RESTRICT`
- `FOREIGN KEY (previous_deployment_id) REFERENCES workspace.deployments (id) ON DELETE RESTRICT`
- `CHECK (status IN ('queued','deploying','verifying','active','superseded','failed','rolling_back','rolled_back','needs_attention'))`
- `UNIQUE (id, workspace_id)`
- `CHECK (artifact_digest ~ '^sha256:[0-9a-f]{64}$')`
- `CHECK (status <> 'active' OR (runtime_instance_id IS NOT NULL AND verification_evidence_ref IS NOT NULL AND activated_at IS NOT NULL))`
- `FOREIGN KEY (operation_id) REFERENCES workspace.operations (id) ON DELETE RESTRICT`
- `CHECK (execution_epoch >= 0)`

索引：
- `deployments_workspace_list`: `(workspace_id, created_at DESC, id DESC)`
- `deployments_operation`: `(operation_id)`

#### workspace.subscriptions

当前已付周期权威；到期默认停用不自动扣款；status从周期/Workspace生命周期派生；续费Operation幂等创建新周期，不改旧历史；quoted仅存接受Quote快照，legacy_import保留原purchase ID及原义务证据，禁止造Quote/重新扣费；缺原policy或receipt须标明确缺口并拒绝受影响动作。覆盖 F08, F11, F12, F13, F16。

| 字段 | 类型 | 可空 | 默认 | 字段来源 |
|---|---|---|---|---|
| `id` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/Subscription/properties/id` |
| `workspace_id` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/Subscription/properties/workspaceId` |
| `accepted_quote_id` | `text` | NULL | `—` | `03_api_contract_complete.yaml#/components/schemas/Subscription/properties/acceptedQuoteId` |
| `accepted_quote_snapshot` | `jsonb` | NULL | `—` | `02_database_schema_complete.md#workspace.subscriptions.accepted_quote_snapshot` |
| `billing_subject_ref` | `text` | 否 | `—` | `02_database_schema_complete.md#workspace.subscriptions.billing_subject_ref` |
| `current_period_start` | `timestamptz` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/Subscription/properties/currentPeriodStart` |
| `current_period_end` | `timestamptz` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/Workspace/properties/currentPeriodEnd` |
| `last_charge_wallet_operation_id` | `text` | NULL | `—` | `02_database_schema_complete.md#workspace.subscriptions.last_charge_wallet_operation_id` |
| `active_change_operation_id` | `text` | NULL | `—` | `03_api_contract_complete.yaml#/components/schemas/Subscription/properties/renewalOperationId` |
| `created_at` | `timestamptz` | 否 | `now()` | `03_api_contract_complete.yaml#/components/schemas/Subscription/properties/createdAt` |
| `updated_at` | `timestamptz` | 否 | `now()` | `02_database_schema_complete.md#workspace.subscriptions.updated_at` |
| `period_months` | `integer` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/Subscription/properties/periodMonths` |
| `provenance` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/Subscription/properties/provenance` |
| `legacy_purchase_id` | `text` | NULL | `—` | `03_api_contract_complete.yaml#/components/schemas/Subscription/properties/legacyPurchaseId` |
| `legacy_obligation_snapshot` | `jsonb` | NULL | `—` | `02_database_schema_complete.md#workspace.subscriptions.legacy_obligation_snapshot` |
| `renewal_mode` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/Subscription/properties/renewalMode` |
| `renewal_consent_id` | `text` | NULL | `—` | `03_api_contract_complete.yaml#/components/schemas/Subscription/properties/renewalConsentId` |
| `renewal_consent_snapshot` | `jsonb` | NULL | `—` | `02_database_schema_complete.md#workspace.subscriptions.renewal_consent_snapshot` |
| `renewal_settings_version` | `bigint` | 否 | `0` | `03_api_contract_complete.yaml#/components/schemas/Subscription/properties/renewalSettingsVersion` |
| `version` | `bigint` | 否 | `0` | `03_api_contract_complete.yaml#/components/schemas/Subscription/properties/version` |
| `current_price_policy_version_id` | `text` | NULL | `—` | `03_api_contract_complete.yaml#/components/schemas/Subscription/properties/currentPricePolicyVersionId` |
| `current_monthly_usd_micros` | `bigint` | NULL | `—` | `03_api_contract_complete.yaml#/components/schemas/Subscription/properties/currentMonthlyUSDMicros` |
| `billing_anchor_day` | `integer` | NULL | `—` | `02_database_schema_complete.md#workspace.subscriptions.billing_anchor_day` |

约束：
- `PRIMARY KEY (id)`
- `FOREIGN KEY (workspace_id) REFERENCES workspace.workspaces (id) ON DELETE RESTRICT`
- `UNIQUE (workspace_id)`
- `CHECK (current_period_end > current_period_start)`
- `FOREIGN KEY (active_change_operation_id) REFERENCES workspace.operations (id) ON DELETE RESTRICT`
- `CHECK (period_months > 0)`
- `CHECK (provenance IN ('quoted','legacy_import'))`
- `CHECK ((provenance = 'quoted' AND accepted_quote_id IS NOT NULL AND accepted_quote_snapshot IS NOT NULL AND legacy_purchase_id IS NULL AND legacy_obligation_snapshot IS NULL) OR (provenance = 'legacy_import' AND accepted_quote_id IS NULL AND accepted_quote_snapshot IS NULL AND legacy_purchase_id IS NOT NULL AND legacy_obligation_snapshot IS NOT NULL))`
- `CHECK (renewal_mode IN ('manual','automatic'))`
- `CHECK (renewal_mode <> 'automatic' OR (renewal_consent_id IS NOT NULL AND renewal_consent_snapshot IS NOT NULL))`
- `CHECK (renewal_settings_version >= 0)`
- `CHECK (version >= 0)`
- `CHECK (current_monthly_usd_micros IS NULL OR current_monthly_usd_micros >= 0)`
- `CHECK (billing_anchor_day IS NULL OR billing_anchor_day BETWEEN 1 AND 31)`
- `CHECK (provenance <> 'quoted' OR (current_price_policy_version_id IS NOT NULL AND current_monthly_usd_micros IS NOT NULL AND billing_anchor_day IS NOT NULL))`
- `UNIQUE (id, workspace_id)`

索引：
- `subscriptions_expiry`: `(current_period_end, id)`

#### workspace.subscription_periods

确认付费周期不可变历史；Local零报价wallet operation可空，receipt记录零费事实不假造扣款；quoted仅存接受Quote快照，legacy_import保留原purchase ID及原义务证据，禁止造Quote/重新扣费；缺原policy或receipt须标明确缺口并拒绝受影响动作。覆盖 F08, F11, F12, F16。

| 字段 | 类型 | 可空 | 默认 | 字段来源 |
|---|---|---|---|---|
| `id` | `text` | 否 | `—` | `02_database_schema_complete.md#workspace.subscription_periods.id` |
| `subscription_id` | `text` | 否 | `—` | `02_database_schema_complete.md#workspace.subscription_periods.subscription_id` |
| `quote_id` | `text` | NULL | `—` | `02_database_schema_complete.md#workspace.subscription_periods.quote_id` |
| `accepted_quote_snapshot` | `jsonb` | NULL | `—` | `02_database_schema_complete.md#workspace.subscription_periods.accepted_quote_snapshot` |
| `period_start` | `timestamptz` | 否 | `—` | `02_database_schema_complete.md#workspace.subscription_periods.period_start` |
| `period_end` | `timestamptz` | 否 | `—` | `02_database_schema_complete.md#workspace.subscription_periods.period_end` |
| `billing_key` | `text` | 否 | `—` | `02_database_schema_complete.md#workspace.subscription_periods.billing_key` |
| `charge_wallet_operation_id` | `text` | NULL | `—` | `02_database_schema_complete.md#workspace.subscription_periods.charge_wallet_operation_id` |
| `charge_receipt_id` | `text` | NULL | `—` | `02_database_schema_complete.md#workspace.subscription_periods.charge_receipt_id` |
| `created_at` | `timestamptz` | 否 | `now()` | `02_database_schema_complete.md#workspace.subscription_periods.created_at` |
| `provenance` | `text` | 否 | `—` | `02_database_schema_complete.md#workspace.subscription_periods.provenance` |
| `legacy_purchase_id` | `text` | NULL | `—` | `02_database_schema_complete.md#workspace.subscription_periods.legacy_purchase_id` |
| `legacy_obligation_snapshot` | `jsonb` | NULL | `—` | `02_database_schema_complete.md#workspace.subscription_periods.legacy_obligation_snapshot` |

约束：
- `PRIMARY KEY (id)`
- `FOREIGN KEY (subscription_id) REFERENCES workspace.subscriptions (id) ON DELETE RESTRICT`
- `UNIQUE (subscription_id, period_start)`
- `UNIQUE (billing_key)`
- `UNIQUE (charge_wallet_operation_id)`
- `CHECK (period_end > period_start)`
- `CHECK (provenance IN ('quoted','legacy_import'))`
- `CHECK ((provenance = 'quoted' AND quote_id IS NOT NULL AND accepted_quote_snapshot IS NOT NULL AND legacy_purchase_id IS NULL AND legacy_obligation_snapshot IS NULL) OR (provenance = 'legacy_import' AND quote_id IS NULL AND accepted_quote_snapshot IS NULL AND legacy_purchase_id IS NOT NULL AND legacy_obligation_snapshot IS NOT NULL))`
- `CHECK (provenance <> 'quoted' OR charge_receipt_id IS NOT NULL)`
- `UNIQUE (id, subscription_id)`

索引：
- `subscription_periods_list`: `(subscription_id, period_start DESC, id DESC)`

#### workspace.model_configurations

新配置新version；Runtime有效调用验证后事务推进Workspace.modelConfigurationVersion。覆盖 F07, F08, F09。

| 字段 | 类型 | 可空 | 默认 | 字段来源 |
|---|---|---|---|---|
| `id` | `text` | 否 | `—` | `02_database_schema_complete.md#workspace.model_configurations.id` |
| `workspace_id` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/ModelConfiguration/properties/workspaceId` |
| `version` | `bigint` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/ModelConfiguration/properties/version` |
| `gateway_key_binding_id` | `text` | 否 | `—` | `02_database_schema_complete.md#workspace.model_configurations.gateway_key_binding_id` |
| `operation_id` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/ModelConfiguration/properties/operationId` |
| `runtime_reload_observation` | `text` | 否 | `—` | `02_database_schema_complete.md#workspace.model_configurations.runtime_reload_observation` |
| `verification_evidence_ref` | `text` | NULL | `—` | `02_database_schema_complete.md#workspace.model_configurations.verification_evidence_ref` |
| `created_by` | `text` | 否 | `—` | `02_database_schema_complete.md#workspace.model_configurations.created_by` |
| `created_at` | `timestamptz` | 否 | `now()` | `02_database_schema_complete.md#workspace.model_configurations.created_at` |
| `selections` | `jsonb` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/ModelConfiguration/properties/selections` |
| `updated_at` | `timestamptz` | 否 | `now()` | `03_api_contract_complete.yaml#/components/schemas/ModelConfiguration/properties/updatedAt` |

约束：
- `PRIMARY KEY (id)`
- `FOREIGN KEY (workspace_id) REFERENCES workspace.workspaces (id) ON DELETE RESTRICT`
- `UNIQUE (workspace_id, version)`
- `CHECK (version > 0)`
- `CHECK (runtime_reload_observation IN ('confirmed','rejected','unknown'))`
- `FOREIGN KEY (operation_id) REFERENCES workspace.operations (id) ON DELETE RESTRICT`

索引：
- `model_configurations_workspace`: `(workspace_id, version DESC)`

#### workspace.saga_steps

固定commandId/幂等键重试；unknown读原Owner，不制造新副作用或逆向补偿。覆盖 F08, F10, F11, F12, F13, F15, F16。

| 字段 | 类型 | 可空 | 默认 | 字段来源 |
|---|---|---|---|---|
| `id` | `text` | 否 | `—` | `02_database_schema_complete.md#workspace.saga_steps.id` |
| `operation_id` | `text` | 否 | `—` | `02_database_schema_complete.md#workspace.saga_steps.operation_id` |
| `step_key` | `text` | 否 | `—` | `02_database_schema_complete.md#workspace.saga_steps.step_key` |
| `sequence` | `integer` | 否 | `—` | `02_database_schema_complete.md#workspace.saga_steps.sequence` |
| `target_owner` | `text` | 否 | `—` | `02_database_schema_complete.md#workspace.saga_steps.target_owner` |
| `command_id` | `text` | 否 | `—` | `02_database_schema_complete.md#workspace.saga_steps.command_id` |
| `idempotency_key` | `text` | 否 | `—` | `02_database_schema_complete.md#workspace.saga_steps.idempotency_key` |
| `input_snapshot` | `jsonb` | 否 | `—` | `02_database_schema_complete.md#workspace.saga_steps.input_snapshot` |
| `observation_result` | `text` | NULL | `—` | `02_database_schema_complete.md#workspace.saga_steps.observation_result` |
| `owner_result_ref` | `text` | NULL | `—` | `02_database_schema_complete.md#workspace.saga_steps.owner_result_ref` |
| `compensation_command_id` | `text` | NULL | `—` | `02_database_schema_complete.md#workspace.saga_steps.compensation_command_id` |
| `compensation_observation` | `text` | NULL | `—` | `02_database_schema_complete.md#workspace.saga_steps.compensation_observation` |
| `attempt_count` | `integer` | 否 | `0` | `02_database_schema_complete.md#workspace.saga_steps.attempt_count` |
| `next_attempt_at` | `timestamptz` | NULL | `—` | `02_database_schema_complete.md#workspace.saga_steps.next_attempt_at` |
| `error_code` | `text` | NULL | `—` | `02_database_schema_complete.md#workspace.saga_steps.error_code` |
| `confirmed_at` | `timestamptz` | NULL | `—` | `02_database_schema_complete.md#workspace.saga_steps.confirmed_at` |
| `created_at` | `timestamptz` | 否 | `now()` | `02_database_schema_complete.md#workspace.saga_steps.created_at` |
| `updated_at` | `timestamptz` | 否 | `now()` | `02_database_schema_complete.md#workspace.saga_steps.updated_at` |

约束：
- `PRIMARY KEY (id)`
- `UNIQUE (operation_id, step_key)`
- `UNIQUE (command_id)`
- `CHECK (sequence >= 0 AND attempt_count >= 0)`
- `CHECK (observation_result IN ('confirmed','rejected','unknown'))`
- `CHECK (compensation_observation IN ('confirmed','rejected','unknown'))`
- `FOREIGN KEY (operation_id) REFERENCES workspace.operations (id) ON DELETE RESTRICT`

索引：
- `saga_steps_recovery`: `(next_attempt_at)` WHERE `confirmed_at IS NULL`
- `saga_steps_operation`: `(operation_id, sequence)`

#### workspace.outbox_events

本Owner状态事务同写immutable事件；revision只在本Owner聚合锁内分配。覆盖 F01, F04, F05, F08, F10, F11, F12, F13, F15, F16, F17。

| 字段 | 类型 | 可空 | 默认 | 字段来源 |
|---|---|---|---|---|
| `id` | `text` | 否 | `—` | `02_database_schema_complete.md#workspace.outbox_events.id` |
| `event_type` | `text` | 否 | `—` | `02_database_schema_complete.md#workspace.outbox_events.event_type` |
| `schema_version` | `integer` | 否 | `—` | `02_database_schema_complete.md#workspace.outbox_events.schema_version` |
| `aggregate_type` | `text` | 否 | `—` | `02_database_schema_complete.md#workspace.outbox_events.aggregate_type` |
| `aggregate_id` | `text` | 否 | `—` | `02_database_schema_complete.md#workspace.outbox_events.aggregate_id` |
| `aggregate_revision` | `bigint` | 否 | `—` | `02_database_schema_complete.md#workspace.outbox_events.aggregate_revision` |
| `tenant_id` | `text` | NULL | `—` | `02_database_schema_complete.md#workspace.outbox_events.tenant_id` |
| `correlation_id` | `text` | 否 | `—` | `02_database_schema_complete.md#workspace.outbox_events.correlation_id` |
| `causation_id` | `text` | NULL | `—` | `02_database_schema_complete.md#workspace.outbox_events.causation_id` |
| `payload` | `jsonb` | 否 | `—` | `02_database_schema_complete.md#workspace.outbox_events.payload` |
| `payload_sha256` | `text` | 否 | `—` | `02_database_schema_complete.md#workspace.outbox_events.payload_sha256` |
| `occurred_at` | `timestamptz` | 否 | `—` | `02_database_schema_complete.md#workspace.outbox_events.occurred_at` |
| `created_at` | `timestamptz` | 否 | `now()` | `02_database_schema_complete.md#workspace.outbox_events.created_at` |

约束：
- `PRIMARY KEY (id)`
- `CHECK (schema_version > 0 AND aggregate_revision >= 0)`
- `CHECK (payload_sha256 ~ '^[0-9a-f]{64}$')`
- `UNIQUE (aggregate_type, aggregate_id, aggregate_revision, event_type)`

索引：
- `outbox_events_aggregate`: `(aggregate_type, aggregate_id, aggregate_revision)`

#### workspace.outbox_deliveries

各consumer独立ACK，禁止单个ACK丢其他消费者事件；lease仅本库。覆盖 F05, F08, F10, F11, F12, F13, F15, F17。

| 字段 | 类型 | 可空 | 默认 | 字段来源 |
|---|---|---|---|---|
| `id` | `text` | 否 | `—` | `02_database_schema_complete.md#workspace.outbox_deliveries.id` |
| `event_id` | `text` | 否 | `—` | `02_database_schema_complete.md#workspace.outbox_deliveries.event_id` |
| `consumer_owner` | `text` | 否 | `—` | `02_database_schema_complete.md#workspace.outbox_deliveries.consumer_owner` |
| `attempt_count` | `integer` | 否 | `0` | `02_database_schema_complete.md#workspace.outbox_deliveries.attempt_count` |
| `next_attempt_at` | `timestamptz` | 否 | `now()` | `02_database_schema_complete.md#workspace.outbox_deliveries.next_attempt_at` |
| `acknowledged_at` | `timestamptz` | NULL | `—` | `02_database_schema_complete.md#workspace.outbox_deliveries.acknowledged_at` |
| `last_error_code` | `text` | NULL | `—` | `02_database_schema_complete.md#workspace.outbox_deliveries.last_error_code` |
| `lease_token` | `text` | NULL | `—` | `02_database_schema_complete.md#workspace.outbox_deliveries.lease_token` |
| `lease_until` | `timestamptz` | NULL | `—` | `02_database_schema_complete.md#workspace.outbox_deliveries.lease_until` |
| `created_at` | `timestamptz` | 否 | `now()` | `02_database_schema_complete.md#workspace.outbox_deliveries.created_at` |
| `updated_at` | `timestamptz` | 否 | `now()` | `02_database_schema_complete.md#workspace.outbox_deliveries.updated_at` |

约束：
- `PRIMARY KEY (id)`
- `FOREIGN KEY (event_id) REFERENCES workspace.outbox_events (id) ON DELETE RESTRICT`
- `UNIQUE (event_id, consumer_owner)`
- `CHECK (attempt_count >= 0)`
- `CHECK ((lease_token IS NULL) = (lease_until IS NULL))`

索引：
- `outbox_deliveries_pending`: `(next_attempt_at, id)` WHERE `acknowledged_at IS NULL`

#### workspace.inbox_events

去重hash/业务状态/processed_at/后续Outbox同本库事务；乱序不能覆盖新版本。覆盖 F05, F08, F10, F11, F12, F13, F15, F17。

| 字段 | 类型 | 可空 | 默认 | 字段来源 |
|---|---|---|---|---|
| `id` | `text` | 否 | `—` | `02_database_schema_complete.md#workspace.inbox_events.id` |
| `source_owner` | `text` | 否 | `—` | `02_database_schema_complete.md#workspace.inbox_events.source_owner` |
| `source_event_id` | `text` | 否 | `—` | `02_database_schema_complete.md#workspace.inbox_events.source_event_id` |
| `event_type` | `text` | 否 | `—` | `02_database_schema_complete.md#workspace.inbox_events.event_type` |
| `schema_version` | `integer` | 否 | `—` | `02_database_schema_complete.md#workspace.inbox_events.schema_version` |
| `aggregate_type` | `text` | 否 | `—` | `02_database_schema_complete.md#workspace.inbox_events.aggregate_type` |
| `aggregate_id` | `text` | 否 | `—` | `02_database_schema_complete.md#workspace.inbox_events.aggregate_id` |
| `aggregate_revision` | `bigint` | 否 | `—` | `02_database_schema_complete.md#workspace.inbox_events.aggregate_revision` |
| `payload_sha256` | `text` | 否 | `—` | `02_database_schema_complete.md#workspace.inbox_events.payload_sha256` |
| `payload` | `jsonb` | 否 | `—` | `02_database_schema_complete.md#workspace.inbox_events.payload` |
| `received_at` | `timestamptz` | 否 | `now()` | `02_database_schema_complete.md#workspace.inbox_events.received_at` |
| `processed_at` | `timestamptz` | NULL | `—` | `02_database_schema_complete.md#workspace.inbox_events.processed_at` |
| `result_resource_id` | `text` | NULL | `—` | `02_database_schema_complete.md#workspace.inbox_events.result_resource_id` |
| `error_code` | `text` | NULL | `—` | `02_database_schema_complete.md#workspace.inbox_events.error_code` |

约束：
- `PRIMARY KEY (id)`
- `UNIQUE (source_owner, source_event_id)`
- `CHECK (schema_version > 0 AND aggregate_revision >= 0)`
- `CHECK (payload_sha256 ~ '^[0-9a-f]{64}$')`

索引：
- `inbox_events_pending`: `(received_at, id)` WHERE `processed_at IS NULL`
- `inbox_events_aggregate`: `(source_owner, aggregate_type, aggregate_id, aggregate_revision)`

#### workspace.idempotency_records

命令创建同事务；operation_name是API操作名不是新Operation ID；安全响应无Key明文；义务存续无TTL清理。覆盖 F01, F03, F04, F05, F08, F10, F11, F12, F13, F14, F15, F16, F17。

| 字段 | 类型 | 可空 | 默认 | 字段来源 |
|---|---|---|---|---|
| `id` | `text` | 否 | `—` | `02_database_schema_complete.md#workspace.idempotency_records.id` |
| `tenant_scope` | `text` | 否 | `—` | `02_database_schema_complete.md#workspace.idempotency_records.tenant_scope` |
| `actor_scope` | `text` | 否 | `—` | `02_database_schema_complete.md#workspace.idempotency_records.actor_scope` |
| `operation_name` | `text` | 否 | `—` | `02_database_schema_complete.md#workspace.idempotency_records.operation_name` |
| `idempotency_key` | `text` | 否 | `—` | `02_database_schema_complete.md#workspace.idempotency_records.idempotency_key` |
| `request_sha256` | `text` | 否 | `—` | `02_database_schema_complete.md#workspace.idempotency_records.request_sha256` |
| `resource_id` | `text` | 否 | `—` | `02_database_schema_complete.md#workspace.idempotency_records.resource_id` |
| `operation_id` | `text` | NULL | `—` | `02_database_schema_complete.md#workspace.idempotency_records.operation_id` |
| `response_status` | `integer` | 否 | `—` | `02_database_schema_complete.md#workspace.idempotency_records.response_status` |
| `response_body` | `jsonb` | 否 | `—` | `02_database_schema_complete.md#workspace.idempotency_records.response_body` |
| `created_at` | `timestamptz` | 否 | `now()` | `02_database_schema_complete.md#workspace.idempotency_records.created_at` |

约束：
- `PRIMARY KEY (id)`
- `UNIQUE (tenant_scope, actor_scope, operation_name, idempotency_key)`
- `CHECK (request_sha256 ~ '^[0-9a-f]{64}$')`
- `CHECK (response_status BETWEEN 100 AND 599)`

索引：
- `idempotency_records_resource`: `(resource_id)`

#### workspace.operations

目标Owner持异步Operation；BFF路由无中央writer；Build创建201回Job；只有Workspace配Saga。覆盖 F01, F03, F04, F06, F08, F09, F10, F11, F12, F13, F14, F15, F17。

| 字段 | 类型 | 可空 | 默认 | 字段来源 |
|---|---|---|---|---|
| `id` | `text` | 否 | `—` | `02_database_schema_complete.md#workspace.operations.id` |
| `tenant_id` | `text` | NULL | `—` | `02_database_schema_complete.md#workspace.operations.tenant_id` |
| `actor_id` | `text` | 否 | `—` | `02_database_schema_complete.md#workspace.operations.actor_id` |
| `kind` | `text` | 否 | `—` | `02_database_schema_complete.md#workspace.operations.kind` |
| `resource_id` | `text` | 否 | `—` | `02_database_schema_complete.md#workspace.operations.resource_id` |
| `status` | `text` | 否 | `'accepted'` | `02_database_schema_complete.md#workspace.operations.status` |
| `stage` | `text` | 否 | `—` | `02_database_schema_complete.md#workspace.operations.stage` |
| `error_code` | `text` | NULL | `—` | `02_database_schema_complete.md#workspace.operations.error_code` |
| `observation_result` | `text` | NULL | `—` | `02_database_schema_complete.md#workspace.operations.observation_result` |
| `request_id` | `text` | 否 | `—` | `02_database_schema_complete.md#workspace.operations.request_id` |
| `accepted_input` | `jsonb` | 否 | `—` | `02_database_schema_complete.md#workspace.operations.accepted_input` |
| `result` | `jsonb` | NULL | `—` | `02_database_schema_complete.md#workspace.operations.result` |
| `worker_lease_token` | `text` | NULL | `—` | `02_database_schema_complete.md#workspace.operations.worker_lease_token` |
| `worker_lease_until` | `timestamptz` | NULL | `—` | `02_database_schema_complete.md#workspace.operations.worker_lease_until` |
| `started_at` | `timestamptz` | NULL | `—` | `02_database_schema_complete.md#workspace.operations.started_at` |
| `completed_at` | `timestamptz` | NULL | `—` | `02_database_schema_complete.md#workspace.operations.completed_at` |
| `created_at` | `timestamptz` | 否 | `now()` | `02_database_schema_complete.md#workspace.operations.created_at` |
| `updated_at` | `timestamptz` | 否 | `now()` | `02_database_schema_complete.md#workspace.operations.updated_at` |

约束：
- `PRIMARY KEY (id)`
- `CHECK (status IN ('accepted','running','awaiting_confirmation','succeeded','failed','needs_attention','cancelled'))`
- `CHECK (observation_result IN ('confirmed','rejected','unknown'))`
- `CHECK ((worker_lease_token IS NULL) = (worker_lease_until IS NULL))`
- `CHECK (status <> 'succeeded' OR completed_at IS NOT NULL)`

索引：
- `operations_resource`: `(resource_id, created_at DESC, id DESC)`
- `operations_tenant_list`: `(tenant_id, created_at DESC, id DESC)`
- `operations_recovery`: `(status, updated_at)`

#### workspace.plan_changes

D17 PlanChange is the sole active/scheduled resource-change identity; upgrade uses fixed UnixMilli quote basis, downgrade is inert until E and paid next-period obligation; Operation IDs do not revive terminal operations。覆盖 F11, F12, F13。

| 字段 | 类型 | 可空 | 默认 | 字段来源 |
|---|---|---|---|---|
| `id` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/PlanChange/properties/id` |
| `workspace_id` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/PlanChange/properties/workspaceId` |
| `tenant_id` | `text` | 否 | `—` | `02_database_schema_complete.md#workspace.plan_changes.tenant_id` |
| `kind` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/PlanChange/properties/kind` |
| `status` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/PlanChange/properties/status` |
| `source_compute_plan_id` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/PlanChange/properties/sourceComputePlanId` |
| `source_storage_plan_id` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/PlanChange/properties/sourceStoragePlanId` |
| `target_compute_plan_id` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/PlanChange/properties/targetComputePlanId` |
| `target_storage_plan_id` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/PlanChange/properties/targetStoragePlanId` |
| `source_price_policy_version_id` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/PlanChange/properties/sourcePricePolicyVersionId` |
| `target_price_policy_version_id` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/PlanChange/properties/targetPricePolicyVersionId` |
| `source_subscription_id` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/PlanChange/properties/sourceSubscriptionId` |
| `source_subscription_version` | `bigint` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/PlanChange/properties/sourceSubscriptionVersion` |
| `source_period_id` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/PlanChange/properties/sourcePeriodId` |
| `quote_id` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/PlanChange/properties/quoteId` |
| `policy_version` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/PlanChange/properties/policyVersion` |
| `quote_at` | `timestamptz` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/PlanChange/properties/quoteAt` |
| `period_start` | `timestamptz` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/PlanChange/properties/periodStart` |
| `period_end` | `timestamptz` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/PlanChange/properties/periodEnd` |
| `source_monthly_usd_micros` | `bigint` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/PlanChange/properties/sourceMonthlyUSDMicros` |
| `target_monthly_usd_micros` | `bigint` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/PlanChange/properties/targetMonthlyUSDMicros` |
| `charge_usd_micros` | `bigint` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/PlanChange/properties/chargeUSDMicros` |
| `planned_effective_at` | `timestamptz` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/PlanChange/properties/plannedEffectiveAt` |
| `applied_at` | `timestamptz` | NULL | `—` | `03_api_contract_complete.yaml#/components/schemas/PlanChange/properties/appliedAt` |
| `operation_id` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/PlanChange/properties/operationId` |
| `execution_operation_id` | `text` | NULL | `—` | `03_api_contract_complete.yaml#/components/schemas/PlanChange/properties/executionOperationId` |
| `cancellation_operation_id` | `text` | NULL | `—` | `03_api_contract_complete.yaml#/components/schemas/PlanChange/properties/cancellationOperationId` |
| `charge_operation_id` | `text` | NULL | `—` | `03_api_contract_complete.yaml#/components/schemas/PlanChange/properties/chargeOperationId` |
| `next_period_obligation_id` | `text` | NULL | `—` | `03_api_contract_complete.yaml#/components/schemas/PlanChange/properties/nextPeriodObligationId` |
| `next_period_start` | `timestamptz` | NULL | `—` | `03_api_contract_complete.yaml#/components/schemas/PlanChange/properties/nextPeriodStart` |
| `next_period_end` | `timestamptz` | NULL | `—` | `03_api_contract_complete.yaml#/components/schemas/PlanChange/properties/nextPeriodEnd` |
| `next_period_charge_usd_micros` | `bigint` | NULL | `—` | `03_api_contract_complete.yaml#/components/schemas/PlanChange/properties/nextPeriodChargeUSDMicros` |
| `schedule_version` | `bigint` | 否 | `0` | `03_api_contract_complete.yaml#/components/schemas/PlanChange/properties/scheduleVersion` |
| `observation_result` | `text` | 否 | `—` | `02_database_schema_complete.md#workspace.plan_changes.observation_result` |
| `accepted_calculation` | `jsonb` | 否 | `—` | `02_database_schema_complete.md#workspace.plan_changes.accepted_calculation` |
| `actual_outcome` | `jsonb` | NULL | `—` | `02_database_schema_complete.md#workspace.plan_changes.actual_outcome` |
| `error_code` | `text` | NULL | `—` | `03_api_contract_complete.yaml#/components/schemas/PlanChange/properties/errorCode` |
| `cancelled_at` | `timestamptz` | NULL | `—` | `02_database_schema_complete.md#workspace.plan_changes.cancelled_at` |
| `created_at` | `timestamptz` | 否 | `now()` | `03_api_contract_complete.yaml#/components/schemas/PlanChange/properties/createdAt` |
| `updated_at` | `timestamptz` | 否 | `now()` | `03_api_contract_complete.yaml#/components/schemas/PlanChange/properties/updatedAt` |
| `execution_plan_id` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/PlanChange/properties/executionPlanId` |
| `execution_plan_digest` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/PlanChange/properties/executionPlanDigest` |
| `quote_at_ms` | `bigint` | 否 | `—` | `02_database_schema_complete.md#workspace.plan_changes.quote_at_ms` |
| `period_start_ms` | `bigint` | 否 | `—` | `02_database_schema_complete.md#workspace.plan_changes.period_start_ms` |
| `period_end_ms` | `bigint` | 否 | `—` | `02_database_schema_complete.md#workspace.plan_changes.period_end_ms` |
| `source_financial_snapshot_digest` | `text` | 否 | `—` | `02_database_schema_complete.md#workspace.plan_changes.source_financial_snapshot_digest` |
| `source_financial_snapshot_bytes` | `bytea` | 否 | `—` | `02_database_schema_complete.md#workspace.plan_changes.source_financial_snapshot_bytes` |

约束：
- `PRIMARY KEY (id)`
- `FOREIGN KEY (workspace_id, tenant_id) REFERENCES workspace.workspaces (id, tenant_id) ON DELETE RESTRICT`
- `FOREIGN KEY (source_subscription_id, workspace_id) REFERENCES workspace.subscriptions (id, workspace_id) ON DELETE RESTRICT`
- `FOREIGN KEY (source_period_id, source_subscription_id) REFERENCES workspace.subscription_periods (id, subscription_id) ON DELETE RESTRICT`
- `FOREIGN KEY (operation_id) REFERENCES workspace.operations (id) ON DELETE RESTRICT`
- `FOREIGN KEY (execution_operation_id) REFERENCES workspace.operations (id) ON DELETE RESTRICT`
- `FOREIGN KEY (cancellation_operation_id) REFERENCES workspace.operations (id) ON DELETE RESTRICT`
- `CHECK (kind IN ('upgrade_immediate','downgrade_next_period'))`
- `CHECK (status IN ('requested','scheduled','awaiting_payment','applying','applied','failed','needs_attention','cancelled'))`
- `CHECK (policy_version IN ('workspace-plan-change-v1'))`
- `CHECK (observation_result IN ('confirmed','rejected','unknown'))`
- `CHECK (source_subscription_version >= 0 AND schedule_version >= 0)`
- `CHECK (source_monthly_usd_micros >= 0 AND target_monthly_usd_micros >= 0 AND charge_usd_micros >= 0)`
- `CHECK ((kind = 'upgrade_immediate' AND planned_effective_at = quote_at AND num_nonnulls(next_period_start,next_period_end,next_period_charge_usd_micros) = 0) OR (kind = 'downgrade_next_period' AND planned_effective_at = period_end AND next_period_start = period_end AND next_period_end > next_period_start AND next_period_charge_usd_micros = target_monthly_usd_micros AND charge_usd_micros = 0))`
- `CHECK ((status = 'applied') = (applied_at IS NOT NULL))`
- `CHECK ((status = 'cancelled') = (cancelled_at IS NOT NULL))`
- `UNIQUE (quote_id)`
- `UNIQUE (id, workspace_id)`
- `FOREIGN KEY (next_period_obligation_id) REFERENCES workspace.subscription_period_obligations (id) ON DELETE RESTRICT`
- `CHECK (date_trunc('milliseconds',quote_at) = quote_at)`
- `CHECK (execution_plan_digest ~ '^sha256:[0-9a-f]{64}$')`
- `CHECK (period_end > period_start)`
- `CHECK (period_end_ms > period_start_ms AND period_end_ms-period_start_ms <= 2678400000)`
- `CHECK (quote_at_ms >= period_start_ms AND quote_at_ms < period_end_ms)`
- `CHECK (source_financial_snapshot_digest = 'sha256:' || encode(sha256(source_financial_snapshot_bytes),'hex'))`
- `CHECK (kind <> 'upgrade_immediate' OR charge_usd_micros = ceil(greatest(target_monthly_usd_micros-source_monthly_usd_micros,0)::numeric * (period_end_ms-quote_at_ms)::numeric / (period_end_ms-period_start_ms)::numeric))`

索引：
- `plan_changes_one_unfinished`: UNIQUE `(workspace_id)` WHERE `status IN ('requested','scheduled','awaiting_payment','applying','needs_attention')`
- `plan_changes_schedule`: `(planned_effective_at, id)` WHERE `status IN ('scheduled','awaiting_payment')`
- `plan_changes_workspace`: `(workspace_id, created_at DESC, id DESC)`

#### workspace.subscription_period_obligations

One original next-period obligation before payment confirmation, shared by manual/automatic/boundary actors; target accepted price is fixed; not a wallet or funds-reservation service。覆盖 F11, F12, F13。

| 字段 | 类型 | 可空 | 默认 | 字段来源 |
|---|---|---|---|---|
| `id` | `text` | 否 | `—` | `02_database_schema_complete.md#workspace.subscription_period_obligations.id` |
| `subscription_id` | `text` | 否 | `—` | `02_database_schema_complete.md#workspace.subscription_period_obligations.subscription_id` |
| `workspace_id` | `text` | 否 | `—` | `02_database_schema_complete.md#workspace.subscription_period_obligations.workspace_id` |
| `plan_change_id` | `text` | NULL | `—` | `02_database_schema_complete.md#workspace.subscription_period_obligations.plan_change_id` |
| `period_start` | `timestamptz` | 否 | `—` | `02_database_schema_complete.md#workspace.subscription_period_obligations.period_start` |
| `period_end` | `timestamptz` | 否 | `—` | `02_database_schema_complete.md#workspace.subscription_period_obligations.period_end` |
| `target_compute_plan_id` | `text` | 否 | `—` | `02_database_schema_complete.md#workspace.subscription_period_obligations.target_compute_plan_id` |
| `target_storage_plan_id` | `text` | 否 | `—` | `02_database_schema_complete.md#workspace.subscription_period_obligations.target_storage_plan_id` |
| `target_price_policy_version_id` | `text` | 否 | `—` | `02_database_schema_complete.md#workspace.subscription_period_obligations.target_price_policy_version_id` |
| `amount_usd_micros` | `bigint` | 否 | `—` | `02_database_schema_complete.md#workspace.subscription_period_obligations.amount_usd_micros` |
| `accepted_pricing_snapshot` | `jsonb` | 否 | `—` | `02_database_schema_complete.md#workspace.subscription_period_obligations.accepted_pricing_snapshot` |
| `status` | `text` | 否 | `—` | `02_database_schema_complete.md#workspace.subscription_period_obligations.status` |
| `operation_id` | `text` | NULL | `—` | `02_database_schema_complete.md#workspace.subscription_period_obligations.operation_id` |
| `wallet_operation_id` | `text` | NULL | `—` | `02_database_schema_complete.md#workspace.subscription_period_obligations.wallet_operation_id` |
| `billing_key` | `text` | 否 | `—` | `02_database_schema_complete.md#workspace.subscription_period_obligations.billing_key` |
| `payment_accepted_at` | `timestamptz` | NULL | `—` | `02_database_schema_complete.md#workspace.subscription_period_obligations.payment_accepted_at` |
| `resource_execution_started_at` | `timestamptz` | NULL | `—` | `02_database_schema_complete.md#workspace.subscription_period_obligations.resource_execution_started_at` |
| `confirmed_at` | `timestamptz` | NULL | `—` | `02_database_schema_complete.md#workspace.subscription_period_obligations.confirmed_at` |
| `error_code` | `text` | NULL | `—` | `02_database_schema_complete.md#workspace.subscription_period_obligations.error_code` |
| `created_at` | `timestamptz` | 否 | `now()` | `02_database_schema_complete.md#workspace.subscription_period_obligations.created_at` |
| `updated_at` | `timestamptz` | 否 | `now()` | `02_database_schema_complete.md#workspace.subscription_period_obligations.updated_at` |
| `version` | `bigint` | 否 | `0` | `02_database_schema_complete.md#workspace.subscription_period_obligations.version` |
| `quote_id` | `text` | 否 | `—` | `02_database_schema_complete.md#workspace.subscription_period_obligations.quote_id` |
| `confirmed_subscription_version` | `bigint` | NULL | `—` | `02_database_schema_complete.md#workspace.subscription_period_obligations.confirmed_subscription_version` |

约束：
- `PRIMARY KEY (id)`
- `FOREIGN KEY (subscription_id, workspace_id) REFERENCES workspace.subscriptions (id, workspace_id) ON DELETE RESTRICT`
- `FOREIGN KEY (plan_change_id, workspace_id) REFERENCES workspace.plan_changes (id, workspace_id) ON DELETE RESTRICT`
- `FOREIGN KEY (operation_id) REFERENCES workspace.operations (id) ON DELETE RESTRICT`
- `UNIQUE (subscription_id, period_start)`
- `UNIQUE (billing_key)`
- `CHECK (period_end > period_start)`
- `CHECK (amount_usd_micros >= 0)`
- `CHECK (status IN ('awaiting_payment','accepted','confirmed','failed','needs_attention'))`
- `CHECK (status <> 'confirmed' OR confirmed_at IS NOT NULL)`
- `CHECK (wallet_operation_id IS NULL OR payment_accepted_at IS NOT NULL)`
- `CHECK (version >= 0)`
- `CHECK (confirmed_subscription_version IS NULL OR (confirmed_subscription_version >= 0 AND status = 'confirmed'))`

索引：
- `period_obligations_workspace`: `(workspace_id, period_start)`
- `period_obligations_pending`: `(period_start, id)` WHERE `status IN ('awaiting_payment','accepted','needs_attention')`

#### workspace.supplemental_charges

Immutable successful-upgrade supplement coverage T..E; original Gateway charge identity retained; later deletion refunds this coverage, not base-order 720-hour policy。覆盖 F11, F13。

| 字段 | 类型 | 可空 | 默认 | 字段来源 |
|---|---|---|---|---|
| `id` | `text` | 否 | `—` | `02_database_schema_complete.md#workspace.supplemental_charges.id` |
| `plan_change_id` | `text` | 否 | `—` | `02_database_schema_complete.md#workspace.supplemental_charges.plan_change_id` |
| `subscription_period_id` | `text` | 否 | `—` | `02_database_schema_complete.md#workspace.supplemental_charges.subscription_period_id` |
| `workspace_id` | `text` | 否 | `—` | `02_database_schema_complete.md#workspace.supplemental_charges.workspace_id` |
| `quote_id` | `text` | 否 | `—` | `02_database_schema_complete.md#workspace.supplemental_charges.quote_id` |
| `policy_version` | `text` | 否 | `—` | `02_database_schema_complete.md#workspace.supplemental_charges.policy_version` |
| `coverage_start` | `timestamptz` | 否 | `—` | `02_database_schema_complete.md#workspace.supplemental_charges.coverage_start` |
| `coverage_end` | `timestamptz` | 否 | `—` | `02_database_schema_complete.md#workspace.supplemental_charges.coverage_end` |
| `confirmed_amount_usd_micros` | `bigint` | 否 | `—` | `02_database_schema_complete.md#workspace.supplemental_charges.confirmed_amount_usd_micros` |
| `original_wallet_operation_id` | `text` | NULL | `—` | `02_database_schema_complete.md#workspace.supplemental_charges.original_wallet_operation_id` |
| `charge_receipt_id` | `text` | 否 | `—` | `02_database_schema_complete.md#workspace.supplemental_charges.charge_receipt_id` |
| `confirmed_at` | `timestamptz` | 否 | `—` | `02_database_schema_complete.md#workspace.supplemental_charges.confirmed_at` |
| `created_at` | `timestamptz` | 否 | `now()` | `02_database_schema_complete.md#workspace.supplemental_charges.created_at` |
| `coverage_start_ms` | `bigint` | 否 | `—` | `02_database_schema_complete.md#workspace.supplemental_charges.coverage_start_ms` |
| `coverage_end_ms` | `bigint` | 否 | `—` | `02_database_schema_complete.md#workspace.supplemental_charges.coverage_end_ms` |
| `source_financial_snapshot_digest` | `text` | 否 | `—` | `02_database_schema_complete.md#workspace.supplemental_charges.source_financial_snapshot_digest` |

约束：
- `PRIMARY KEY (id)`
- `FOREIGN KEY (plan_change_id, workspace_id) REFERENCES workspace.plan_changes (id, workspace_id) ON DELETE RESTRICT`
- `FOREIGN KEY (subscription_period_id) REFERENCES workspace.subscription_periods (id) ON DELETE RESTRICT`
- `CHECK (policy_version IN ('workspace-plan-change-v1'))`
- `UNIQUE (plan_change_id)`
- `UNIQUE (original_wallet_operation_id)`
- `CHECK (coverage_end > coverage_start)`
- `CHECK (confirmed_amount_usd_micros >= 0)`
- `CHECK ((confirmed_amount_usd_micros = 0) = (original_wallet_operation_id IS NULL))`
- `CHECK (coverage_end_ms > coverage_start_ms)`
- `CHECK (source_financial_snapshot_digest ~ '^sha256:[0-9a-f]{64}$')`

索引：
- `supplements_workspace`: `(workspace_id, created_at DESC, id DESC)`
- `supplements_period`: `(subscription_period_id)`

### runtime_control

Database `opl_runtime_control` · Schema `runtime_control` · Writer `opl_runtime_control_writer`。

#### runtime_control.runtime_instances

readiness/accessUrl真实回读；无active布尔、无订阅业务状态。覆盖 F08, F09, F10, F12, F13。

| 字段 | 类型 | 可空 | 默认 | 字段来源 |
|---|---|---|---|---|
| `id` | `text` | 否 | `—` | `02_database_schema_complete.md#runtime_control.runtime_instances.id` |
| `workspace_id` | `text` | 否 | `—` | `02_database_schema_complete.md#runtime_control.runtime_instances.workspace_id` |
| `deployment_id` | `text` | 否 | `—` | `02_database_schema_complete.md#runtime_control.runtime_instances.deployment_id` |
| `artifact_digest` | `text` | 否 | `—` | `02_database_schema_complete.md#runtime_control.runtime_instances.artifact_digest` |
| `fabric_resource_set_id` | `text` | 否 | `—` | `02_database_schema_complete.md#runtime_control.runtime_instances.fabric_resource_set_id` |
| `fabric_execution_ref` | `text` | NULL | `—` | `02_database_schema_complete.md#runtime_control.runtime_instances.fabric_execution_ref` |
| `status` | `text` | 否 | `'pending'` | `02_database_schema_complete.md#runtime_control.runtime_instances.status` |
| `access_url` | `text` | NULL | `—` | `02_database_schema_complete.md#runtime_control.runtime_instances.access_url` |
| `data_attachment_contract` | `jsonb` | 否 | `—` | `02_database_schema_complete.md#runtime_control.runtime_instances.data_attachment_contract` |
| `applied_model_configuration_version` | `bigint` | 否 | `0` | `02_database_schema_complete.md#runtime_control.runtime_instances.applied_model_configuration_version` |
| `readiness_evidence_ref` | `text` | NULL | `—` | `02_database_schema_complete.md#runtime_control.runtime_instances.readiness_evidence_ref` |
| `error_code` | `text` | NULL | `—` | `02_database_schema_complete.md#runtime_control.runtime_instances.error_code` |
| `observed_at` | `timestamptz` | NULL | `—` | `02_database_schema_complete.md#runtime_control.runtime_instances.observed_at` |
| `created_at` | `timestamptz` | 否 | `now()` | `02_database_schema_complete.md#runtime_control.runtime_instances.created_at` |
| `updated_at` | `timestamptz` | 否 | `now()` | `02_database_schema_complete.md#runtime_control.runtime_instances.updated_at` |
| `execution_epoch` | `bigint` | 否 | `—` | `02_database_schema_complete.md#runtime_control.runtime_instances.execution_epoch` |
| `deployment_descriptor` | `jsonb` | 否 | `—` | `02_database_schema_complete.md#runtime_control.runtime_instances.deployment_descriptor` |
| `deployment_descriptor_digest` | `text` | 否 | `—` | `02_database_schema_complete.md#runtime_control.runtime_instances.deployment_descriptor_digest` |
| `deployment_descriptor_object_ref` | `text` | 否 | `—` | `02_database_schema_complete.md#runtime_control.runtime_instances.deployment_descriptor_object_ref` |

约束：
- `PRIMARY KEY (id)`
- `CHECK (status IN ('pending','starting','ready','stopped','failed','terminating','terminated'))`
- `UNIQUE (deployment_id)`
- `CHECK (artifact_digest ~ '^sha256:[0-9a-f]{64}$')`
- `CHECK (applied_model_configuration_version >= 0)`
- `CHECK (status <> 'ready' OR (access_url IS NOT NULL AND readiness_evidence_ref IS NOT NULL AND observed_at IS NOT NULL))`
- `CHECK (execution_epoch >= 0)`
- `CHECK (deployment_descriptor_digest ~ '^sha256:[0-9a-f]{64}$')`
- `CHECK ((deployment_descriptor #>> '{artifact,digest}' = artifact_digest) IS TRUE)`

索引：
- `runtime_instances_workspace`: `(workspace_id, created_at DESC, id DESC)`

#### runtime_control.runtime_actions

先持久action再调用Fabric；旧Deployment响应不能覆盖新实例。覆盖 F08, F09, F10, F12, F13。

| 字段 | 类型 | 可空 | 默认 | 字段来源 |
|---|---|---|---|---|
| `id` | `text` | 否 | `—` | `02_database_schema_complete.md#runtime_control.runtime_actions.id` |
| `runtime_instance_id` | `text` | 否 | `—` | `02_database_schema_complete.md#runtime_control.runtime_actions.runtime_instance_id` |
| `command_id` | `text` | 否 | `—` | `02_database_schema_complete.md#runtime_control.runtime_actions.command_id` |
| `action` | `text` | 否 | `—` | `02_database_schema_complete.md#runtime_control.runtime_actions.action` |
| `expected_deployment_id` | `text` | 否 | `—` | `02_database_schema_complete.md#runtime_control.runtime_actions.expected_deployment_id` |
| `input_snapshot` | `jsonb` | 否 | `—` | `02_database_schema_complete.md#runtime_control.runtime_actions.input_snapshot` |
| `fabric_action_id` | `text` | NULL | `—` | `02_database_schema_complete.md#runtime_control.runtime_actions.fabric_action_id` |
| `observation_result` | `text` | 否 | `—` | `02_database_schema_complete.md#runtime_control.runtime_actions.observation_result` |
| `error_code` | `text` | NULL | `—` | `02_database_schema_complete.md#runtime_control.runtime_actions.error_code` |
| `evidence_ref` | `text` | NULL | `—` | `02_database_schema_complete.md#runtime_control.runtime_actions.evidence_ref` |
| `created_at` | `timestamptz` | 否 | `now()` | `02_database_schema_complete.md#runtime_control.runtime_actions.created_at` |
| `updated_at` | `timestamptz` | 否 | `now()` | `02_database_schema_complete.md#runtime_control.runtime_actions.updated_at` |

约束：
- `PRIMARY KEY (id)`
- `FOREIGN KEY (runtime_instance_id) REFERENCES runtime_control.runtime_instances (id) ON DELETE RESTRICT`
- `UNIQUE (command_id)`
- `CHECK (action IN ('start','stop','terminate','reload','verify'))`
- `CHECK (observation_result IN ('confirmed','rejected','unknown'))`

索引：
- `runtime_actions_instance`: `(runtime_instance_id, created_at DESC, id DESC)`

#### runtime_control.outbox_events

本Owner状态事务同写immutable事件；revision只在本Owner聚合锁内分配。覆盖 F01, F04, F05, F08, F10, F11, F12, F13, F15, F16, F17。

| 字段 | 类型 | 可空 | 默认 | 字段来源 |
|---|---|---|---|---|
| `id` | `text` | 否 | `—` | `02_database_schema_complete.md#runtime_control.outbox_events.id` |
| `event_type` | `text` | 否 | `—` | `02_database_schema_complete.md#runtime_control.outbox_events.event_type` |
| `schema_version` | `integer` | 否 | `—` | `02_database_schema_complete.md#runtime_control.outbox_events.schema_version` |
| `aggregate_type` | `text` | 否 | `—` | `02_database_schema_complete.md#runtime_control.outbox_events.aggregate_type` |
| `aggregate_id` | `text` | 否 | `—` | `02_database_schema_complete.md#runtime_control.outbox_events.aggregate_id` |
| `aggregate_revision` | `bigint` | 否 | `—` | `02_database_schema_complete.md#runtime_control.outbox_events.aggregate_revision` |
| `tenant_id` | `text` | NULL | `—` | `02_database_schema_complete.md#runtime_control.outbox_events.tenant_id` |
| `correlation_id` | `text` | 否 | `—` | `02_database_schema_complete.md#runtime_control.outbox_events.correlation_id` |
| `causation_id` | `text` | NULL | `—` | `02_database_schema_complete.md#runtime_control.outbox_events.causation_id` |
| `payload` | `jsonb` | 否 | `—` | `02_database_schema_complete.md#runtime_control.outbox_events.payload` |
| `payload_sha256` | `text` | 否 | `—` | `02_database_schema_complete.md#runtime_control.outbox_events.payload_sha256` |
| `occurred_at` | `timestamptz` | 否 | `—` | `02_database_schema_complete.md#runtime_control.outbox_events.occurred_at` |
| `created_at` | `timestamptz` | 否 | `now()` | `02_database_schema_complete.md#runtime_control.outbox_events.created_at` |

约束：
- `PRIMARY KEY (id)`
- `CHECK (schema_version > 0 AND aggregate_revision >= 0)`
- `CHECK (payload_sha256 ~ '^[0-9a-f]{64}$')`
- `UNIQUE (aggregate_type, aggregate_id, aggregate_revision, event_type)`

索引：
- `outbox_events_aggregate`: `(aggregate_type, aggregate_id, aggregate_revision)`

#### runtime_control.outbox_deliveries

各consumer独立ACK，禁止单个ACK丢其他消费者事件；lease仅本库。覆盖 F05, F08, F10, F11, F12, F13, F15, F17。

| 字段 | 类型 | 可空 | 默认 | 字段来源 |
|---|---|---|---|---|
| `id` | `text` | 否 | `—` | `02_database_schema_complete.md#runtime_control.outbox_deliveries.id` |
| `event_id` | `text` | 否 | `—` | `02_database_schema_complete.md#runtime_control.outbox_deliveries.event_id` |
| `consumer_owner` | `text` | 否 | `—` | `02_database_schema_complete.md#runtime_control.outbox_deliveries.consumer_owner` |
| `attempt_count` | `integer` | 否 | `0` | `02_database_schema_complete.md#runtime_control.outbox_deliveries.attempt_count` |
| `next_attempt_at` | `timestamptz` | 否 | `now()` | `02_database_schema_complete.md#runtime_control.outbox_deliveries.next_attempt_at` |
| `acknowledged_at` | `timestamptz` | NULL | `—` | `02_database_schema_complete.md#runtime_control.outbox_deliveries.acknowledged_at` |
| `last_error_code` | `text` | NULL | `—` | `02_database_schema_complete.md#runtime_control.outbox_deliveries.last_error_code` |
| `lease_token` | `text` | NULL | `—` | `02_database_schema_complete.md#runtime_control.outbox_deliveries.lease_token` |
| `lease_until` | `timestamptz` | NULL | `—` | `02_database_schema_complete.md#runtime_control.outbox_deliveries.lease_until` |
| `created_at` | `timestamptz` | 否 | `now()` | `02_database_schema_complete.md#runtime_control.outbox_deliveries.created_at` |
| `updated_at` | `timestamptz` | 否 | `now()` | `02_database_schema_complete.md#runtime_control.outbox_deliveries.updated_at` |

约束：
- `PRIMARY KEY (id)`
- `FOREIGN KEY (event_id) REFERENCES runtime_control.outbox_events (id) ON DELETE RESTRICT`
- `UNIQUE (event_id, consumer_owner)`
- `CHECK (attempt_count >= 0)`
- `CHECK ((lease_token IS NULL) = (lease_until IS NULL))`

索引：
- `outbox_deliveries_pending`: `(next_attempt_at, id)` WHERE `acknowledged_at IS NULL`

#### runtime_control.inbox_events

去重hash/业务状态/processed_at/后续Outbox同本库事务；乱序不能覆盖新版本。覆盖 F05, F08, F10, F11, F12, F13, F15, F17。

| 字段 | 类型 | 可空 | 默认 | 字段来源 |
|---|---|---|---|---|
| `id` | `text` | 否 | `—` | `02_database_schema_complete.md#runtime_control.inbox_events.id` |
| `source_owner` | `text` | 否 | `—` | `02_database_schema_complete.md#runtime_control.inbox_events.source_owner` |
| `source_event_id` | `text` | 否 | `—` | `02_database_schema_complete.md#runtime_control.inbox_events.source_event_id` |
| `event_type` | `text` | 否 | `—` | `02_database_schema_complete.md#runtime_control.inbox_events.event_type` |
| `schema_version` | `integer` | 否 | `—` | `02_database_schema_complete.md#runtime_control.inbox_events.schema_version` |
| `aggregate_type` | `text` | 否 | `—` | `02_database_schema_complete.md#runtime_control.inbox_events.aggregate_type` |
| `aggregate_id` | `text` | 否 | `—` | `02_database_schema_complete.md#runtime_control.inbox_events.aggregate_id` |
| `aggregate_revision` | `bigint` | 否 | `—` | `02_database_schema_complete.md#runtime_control.inbox_events.aggregate_revision` |
| `payload_sha256` | `text` | 否 | `—` | `02_database_schema_complete.md#runtime_control.inbox_events.payload_sha256` |
| `payload` | `jsonb` | 否 | `—` | `02_database_schema_complete.md#runtime_control.inbox_events.payload` |
| `received_at` | `timestamptz` | 否 | `now()` | `02_database_schema_complete.md#runtime_control.inbox_events.received_at` |
| `processed_at` | `timestamptz` | NULL | `—` | `02_database_schema_complete.md#runtime_control.inbox_events.processed_at` |
| `result_resource_id` | `text` | NULL | `—` | `02_database_schema_complete.md#runtime_control.inbox_events.result_resource_id` |
| `error_code` | `text` | NULL | `—` | `02_database_schema_complete.md#runtime_control.inbox_events.error_code` |

约束：
- `PRIMARY KEY (id)`
- `UNIQUE (source_owner, source_event_id)`
- `CHECK (schema_version > 0 AND aggregate_revision >= 0)`
- `CHECK (payload_sha256 ~ '^[0-9a-f]{64}$')`

索引：
- `inbox_events_pending`: `(received_at, id)` WHERE `processed_at IS NULL`
- `inbox_events_aggregate`: `(source_owner, aggregate_type, aggregate_id, aggregate_revision)`

#### runtime_control.idempotency_records

命令创建同事务；operation_name是API操作名不是新Operation ID；安全响应无Key明文；义务存续无TTL清理。覆盖 F01, F03, F04, F05, F08, F10, F11, F12, F13, F14, F15, F16, F17。

| 字段 | 类型 | 可空 | 默认 | 字段来源 |
|---|---|---|---|---|
| `id` | `text` | 否 | `—` | `02_database_schema_complete.md#runtime_control.idempotency_records.id` |
| `tenant_scope` | `text` | 否 | `—` | `02_database_schema_complete.md#runtime_control.idempotency_records.tenant_scope` |
| `actor_scope` | `text` | 否 | `—` | `02_database_schema_complete.md#runtime_control.idempotency_records.actor_scope` |
| `operation_name` | `text` | 否 | `—` | `02_database_schema_complete.md#runtime_control.idempotency_records.operation_name` |
| `idempotency_key` | `text` | 否 | `—` | `02_database_schema_complete.md#runtime_control.idempotency_records.idempotency_key` |
| `request_sha256` | `text` | 否 | `—` | `02_database_schema_complete.md#runtime_control.idempotency_records.request_sha256` |
| `resource_id` | `text` | 否 | `—` | `02_database_schema_complete.md#runtime_control.idempotency_records.resource_id` |
| `operation_id` | `text` | NULL | `—` | `02_database_schema_complete.md#runtime_control.idempotency_records.operation_id` |
| `response_status` | `integer` | 否 | `—` | `02_database_schema_complete.md#runtime_control.idempotency_records.response_status` |
| `response_body` | `jsonb` | 否 | `—` | `02_database_schema_complete.md#runtime_control.idempotency_records.response_body` |
| `created_at` | `timestamptz` | 否 | `now()` | `02_database_schema_complete.md#runtime_control.idempotency_records.created_at` |

约束：
- `PRIMARY KEY (id)`
- `UNIQUE (tenant_scope, actor_scope, operation_name, idempotency_key)`
- `CHECK (request_sha256 ~ '^[0-9a-f]{64}$')`
- `CHECK (response_status BETWEEN 100 AND 599)`

索引：
- `idempotency_records_resource`: `(resource_id)`

#### runtime_control.operations

目标Owner持异步Operation；BFF路由无中央writer；Build创建201回Job；只有Workspace配Saga。覆盖 F01, F03, F04, F06, F08, F09, F10, F11, F12, F13, F14, F15, F17。

| 字段 | 类型 | 可空 | 默认 | 字段来源 |
|---|---|---|---|---|
| `id` | `text` | 否 | `—` | `02_database_schema_complete.md#runtime_control.operations.id` |
| `tenant_id` | `text` | NULL | `—` | `02_database_schema_complete.md#runtime_control.operations.tenant_id` |
| `actor_id` | `text` | 否 | `—` | `02_database_schema_complete.md#runtime_control.operations.actor_id` |
| `kind` | `text` | 否 | `—` | `02_database_schema_complete.md#runtime_control.operations.kind` |
| `resource_id` | `text` | 否 | `—` | `02_database_schema_complete.md#runtime_control.operations.resource_id` |
| `status` | `text` | 否 | `'accepted'` | `02_database_schema_complete.md#runtime_control.operations.status` |
| `stage` | `text` | 否 | `—` | `02_database_schema_complete.md#runtime_control.operations.stage` |
| `error_code` | `text` | NULL | `—` | `02_database_schema_complete.md#runtime_control.operations.error_code` |
| `observation_result` | `text` | NULL | `—` | `02_database_schema_complete.md#runtime_control.operations.observation_result` |
| `request_id` | `text` | 否 | `—` | `02_database_schema_complete.md#runtime_control.operations.request_id` |
| `accepted_input` | `jsonb` | 否 | `—` | `02_database_schema_complete.md#runtime_control.operations.accepted_input` |
| `result` | `jsonb` | NULL | `—` | `02_database_schema_complete.md#runtime_control.operations.result` |
| `worker_lease_token` | `text` | NULL | `—` | `02_database_schema_complete.md#runtime_control.operations.worker_lease_token` |
| `worker_lease_until` | `timestamptz` | NULL | `—` | `02_database_schema_complete.md#runtime_control.operations.worker_lease_until` |
| `started_at` | `timestamptz` | NULL | `—` | `02_database_schema_complete.md#runtime_control.operations.started_at` |
| `completed_at` | `timestamptz` | NULL | `—` | `02_database_schema_complete.md#runtime_control.operations.completed_at` |
| `created_at` | `timestamptz` | 否 | `now()` | `02_database_schema_complete.md#runtime_control.operations.created_at` |
| `updated_at` | `timestamptz` | 否 | `now()` | `02_database_schema_complete.md#runtime_control.operations.updated_at` |

约束：
- `PRIMARY KEY (id)`
- `CHECK (status IN ('accepted','running','awaiting_confirmation','succeeded','failed','needs_attention','cancelled'))`
- `CHECK (observation_result IN ('confirmed','rejected','unknown'))`
- `CHECK ((worker_lease_token IS NULL) = (worker_lease_until IS NULL))`
- `CHECK (status <> 'succeeded' OR completed_at IS NOT NULL)`

索引：
- `operations_resource`: `(resource_id, created_at DESC, id DESC)`
- `operations_tenant_list`: `(tenant_id, created_at DESC, id DESC)`
- `operations_recovery`: `(status, updated_at)`

### fabric

Database `opl_fabric` · Schema `fabric` · Writer `opl_fabric_writer`。

#### fabric.resource_sets

provider从批准套餐解析，不复制wallet余额或Cloud订阅价格。覆盖 F08, F11, F12, F13, F16。

| 字段 | 类型 | 可空 | 默认 | 字段来源 |
|---|---|---|---|---|
| `id` | `text` | 否 | `—` | `02_database_schema_complete.md#fabric.resource_sets.id` |
| `tenant_id` | `text` | 否 | `—` | `02_database_schema_complete.md#fabric.resource_sets.tenant_id` |
| `workspace_id` | `text` | 否 | `—` | `02_database_schema_complete.md#fabric.resource_sets.workspace_id` |
| `provider` | `text` | 否 | `—` | `02_database_schema_complete.md#fabric.resource_sets.provider` |
| `provider_profile_ref` | `text` | 否 | `—` | `02_database_schema_complete.md#fabric.resource_sets.provider_profile_ref` |
| `region` | `text` | 否 | `—` | `02_database_schema_complete.md#fabric.resource_sets.region` |
| `compute_plan_id` | `text` | 否 | `—` | `02_database_schema_complete.md#fabric.resource_sets.compute_plan_id` |
| `storage_plan_id` | `text` | 否 | `—` | `02_database_schema_complete.md#fabric.resource_sets.storage_plan_id` |
| `accepted_quote_id` | `text` | 否 | `—` | `02_database_schema_complete.md#fabric.resource_sets.accepted_quote_id` |
| `approved_specification` | `jsonb` | 否 | `—` | `02_database_schema_complete.md#fabric.resource_sets.approved_specification` |
| `observation_result` | `text` | 否 | `—` | `02_database_schema_complete.md#fabric.resource_sets.observation_result` |
| `observed_at` | `timestamptz` | NULL | `—` | `02_database_schema_complete.md#fabric.resource_sets.observed_at` |
| `created_at` | `timestamptz` | 否 | `now()` | `02_database_schema_complete.md#fabric.resource_sets.created_at` |
| `updated_at` | `timestamptz` | 否 | `now()` | `02_database_schema_complete.md#fabric.resource_sets.updated_at` |

约束：
- `PRIMARY KEY (id)`
- `CHECK (observation_result IN ('confirmed','rejected','unknown'))`
- `UNIQUE (workspace_id)`

索引：
- `resource_sets_tenant`: `(tenant_id, created_at DESC, id DESC)`

#### fabric.resources

仅预付包月/Local无费；不产生POSTPAID_BY_HOUR；provider事实带观察时间/证据。覆盖 F08, F11, F12, F13, F16。

| 字段 | 类型 | 可空 | 默认 | 字段来源 |
|---|---|---|---|---|
| `id` | `text` | 否 | `—` | `02_database_schema_complete.md#fabric.resources.id` |
| `resource_set_id` | `text` | 否 | `—` | `02_database_schema_complete.md#fabric.resources.resource_set_id` |
| `kind` | `text` | 否 | `—` | `02_database_schema_complete.md#fabric.resources.kind` |
| `provider_resource_ref` | `text` | NULL | `—` | `02_database_schema_complete.md#fabric.resources.provider_resource_ref` |
| `provider_purchase_key` | `text` | 否 | `—` | `02_database_schema_complete.md#fabric.resources.provider_purchase_key` |
| `billing_mode` | `text` | 否 | `—` | `02_database_schema_complete.md#fabric.resources.billing_mode` |
| `requested_specification` | `jsonb` | 否 | `—` | `02_database_schema_complete.md#fabric.resources.requested_specification` |
| `observed_specification` | `jsonb` | NULL | `—` | `02_database_schema_complete.md#fabric.resources.observed_specification` |
| `observation_result` | `text` | 否 | `—` | `02_database_schema_complete.md#fabric.resources.observation_result` |
| `provider_expires_at` | `timestamptz` | NULL | `—` | `02_database_schema_complete.md#fabric.resources.provider_expires_at` |
| `deletion_evidence_ref` | `text` | NULL | `—` | `02_database_schema_complete.md#fabric.resources.deletion_evidence_ref` |
| `deleted_at` | `timestamptz` | NULL | `—` | `02_database_schema_complete.md#fabric.resources.deleted_at` |
| `observed_at` | `timestamptz` | NULL | `—` | `02_database_schema_complete.md#fabric.resources.observed_at` |
| `created_at` | `timestamptz` | 否 | `now()` | `02_database_schema_complete.md#fabric.resources.created_at` |
| `updated_at` | `timestamptz` | 否 | `now()` | `02_database_schema_complete.md#fabric.resources.updated_at` |

约束：
- `PRIMARY KEY (id)`
- `FOREIGN KEY (resource_set_id) REFERENCES fabric.resource_sets (id) ON DELETE RESTRICT`
- `CHECK (kind IN ('compute','storage','network','execution'))`
- `CHECK (billing_mode IN ('PREPAID_MONTHLY','LOCAL_NO_CHARGE'))`
- `CHECK (observation_result IN ('confirmed','rejected','unknown'))`
- `UNIQUE (provider_purchase_key)`
- `CHECK (deleted_at IS NULL OR deletion_evidence_ref IS NOT NULL)`

索引：
- `resources_provider_ref`: `(provider_resource_ref)`
- `resources_set`: `(resource_set_id, kind)`

#### fabric.attachments

Owner事务核验同resource_set和kind，更新/回滚满足卷单写挂载约束。覆盖 F08, F10, F13。

| 字段 | 类型 | 可空 | 默认 | 字段来源 |
|---|---|---|---|---|
| `id` | `text` | 否 | `—` | `02_database_schema_complete.md#fabric.attachments.id` |
| `resource_set_id` | `text` | 否 | `—` | `02_database_schema_complete.md#fabric.attachments.resource_set_id` |
| `storage_resource_id` | `text` | 否 | `—` | `02_database_schema_complete.md#fabric.attachments.storage_resource_id` |
| `execution_resource_id` | `text` | 否 | `—` | `02_database_schema_complete.md#fabric.attachments.execution_resource_id` |
| `mount_path` | `text` | 否 | `—` | `02_database_schema_complete.md#fabric.attachments.mount_path` |
| `access_mode` | `text` | 否 | `—` | `02_database_schema_complete.md#fabric.attachments.access_mode` |
| `observation_result` | `text` | 否 | `—` | `02_database_schema_complete.md#fabric.attachments.observation_result` |
| `evidence_ref` | `text` | NULL | `—` | `02_database_schema_complete.md#fabric.attachments.evidence_ref` |
| `detached_at` | `timestamptz` | NULL | `—` | `02_database_schema_complete.md#fabric.attachments.detached_at` |
| `created_at` | `timestamptz` | 否 | `now()` | `02_database_schema_complete.md#fabric.attachments.created_at` |
| `updated_at` | `timestamptz` | 否 | `now()` | `02_database_schema_complete.md#fabric.attachments.updated_at` |

约束：
- `PRIMARY KEY (id)`
- `FOREIGN KEY (resource_set_id) REFERENCES fabric.resource_sets (id) ON DELETE RESTRICT`
- `FOREIGN KEY (storage_resource_id) REFERENCES fabric.resources (id) ON DELETE RESTRICT`
- `FOREIGN KEY (execution_resource_id) REFERENCES fabric.resources (id) ON DELETE RESTRICT`
- `CHECK (observation_result IN ('confirmed','rejected','unknown'))`

索引：
- `attachments_active`: UNIQUE `(storage_resource_id, execution_resource_id, mount_path)` WHERE `detached_at IS NULL`

#### fabric.secret_bindings

仅Secret Store引用/版本/指纹；注入完成必须有效认证调用证据。覆盖 F08, F09, F14。

| 字段 | 类型 | 可空 | 默认 | 字段来源 |
|---|---|---|---|---|
| `id` | `text` | 否 | `—` | `02_database_schema_complete.md#fabric.secret_bindings.id` |
| `resource_set_id` | `text` | 否 | `—` | `02_database_schema_complete.md#fabric.secret_bindings.resource_set_id` |
| `execution_resource_id` | `text` | 否 | `—` | `02_database_schema_complete.md#fabric.secret_bindings.execution_resource_id` |
| `secret_ref` | `text` | 否 | `—` | `02_database_schema_complete.md#fabric.secret_bindings.secret_ref` |
| `purpose` | `text` | 否 | `—` | `02_database_schema_complete.md#fabric.secret_bindings.purpose` |
| `version` | `text` | 否 | `—` | `02_database_schema_complete.md#fabric.secret_bindings.version` |
| `fingerprint` | `text` | 否 | `—` | `02_database_schema_complete.md#fabric.secret_bindings.fingerprint` |
| `observation_result` | `text` | 否 | `—` | `02_database_schema_complete.md#fabric.secret_bindings.observation_result` |
| `evidence_ref` | `text` | NULL | `—` | `02_database_schema_complete.md#fabric.secret_bindings.evidence_ref` |
| `revoked_at` | `timestamptz` | NULL | `—` | `02_database_schema_complete.md#fabric.secret_bindings.revoked_at` |
| `created_at` | `timestamptz` | 否 | `now()` | `02_database_schema_complete.md#fabric.secret_bindings.created_at` |
| `updated_at` | `timestamptz` | 否 | `now()` | `02_database_schema_complete.md#fabric.secret_bindings.updated_at` |

约束：
- `PRIMARY KEY (id)`
- `FOREIGN KEY (resource_set_id) REFERENCES fabric.resource_sets (id) ON DELETE RESTRICT`
- `FOREIGN KEY (execution_resource_id) REFERENCES fabric.resources (id) ON DELETE RESTRICT`
- `CHECK (observation_result IN ('confirmed','rejected','unknown'))`

索引：
- `secret_bindings_active`: UNIQUE `(execution_resource_id, purpose)` WHERE `revoked_at IS NULL`

#### fabric.resource_actions

实费/采购/续费/删除须Instance保护流程与有界授权；unknown查询原provider请求。覆盖 F08, F10, F11, F12, F13, F17。

| 字段 | 类型 | 可空 | 默认 | 字段来源 |
|---|---|---|---|---|
| `id` | `text` | 否 | `—` | `02_database_schema_complete.md#fabric.resource_actions.id` |
| `resource_set_id` | `text` | 否 | `—` | `02_database_schema_complete.md#fabric.resource_actions.resource_set_id` |
| `resource_id` | `text` | NULL | `—` | `02_database_schema_complete.md#fabric.resource_actions.resource_id` |
| `command_id` | `text` | 否 | `—` | `02_database_schema_complete.md#fabric.resource_actions.command_id` |
| `action` | `text` | 否 | `—` | `02_database_schema_complete.md#fabric.resource_actions.action` |
| `provider_idempotency_key` | `text` | 否 | `—` | `02_database_schema_complete.md#fabric.resource_actions.provider_idempotency_key` |
| `approved_input` | `jsonb` | 否 | `—` | `02_database_schema_complete.md#fabric.resource_actions.approved_input` |
| `authorization_receipt_ref` | `text` | NULL | `—` | `02_database_schema_complete.md#fabric.resource_actions.authorization_receipt_ref` |
| `provider_request_ref` | `text` | NULL | `—` | `02_database_schema_complete.md#fabric.resource_actions.provider_request_ref` |
| `observation_result` | `text` | 否 | `—` | `02_database_schema_complete.md#fabric.resource_actions.observation_result` |
| `evidence_ref` | `text` | NULL | `—` | `02_database_schema_complete.md#fabric.resource_actions.evidence_ref` |
| `error_code` | `text` | NULL | `—` | `02_database_schema_complete.md#fabric.resource_actions.error_code` |
| `created_at` | `timestamptz` | 否 | `now()` | `02_database_schema_complete.md#fabric.resource_actions.created_at` |
| `updated_at` | `timestamptz` | 否 | `now()` | `02_database_schema_complete.md#fabric.resource_actions.updated_at` |
| `execution_epoch` | `bigint` | NULL | `—` | `02_database_schema_complete.md#fabric.resource_actions.execution_epoch` |
| `execution_plan_digest` | `text` | NULL | `—` | `02_database_schema_complete.md#fabric.resource_actions.execution_plan_digest` |
| `execution_plan_bytes` | `bytea` | NULL | `—` | `02_database_schema_complete.md#fabric.resource_actions.execution_plan_bytes` |

约束：
- `PRIMARY KEY (id)`
- `FOREIGN KEY (resource_set_id) REFERENCES fabric.resource_sets (id) ON DELETE RESTRICT`
- `FOREIGN KEY (resource_id) REFERENCES fabric.resources (id) ON DELETE RESTRICT`
- `UNIQUE (command_id)`
- `UNIQUE (provider_idempotency_key)`
- `CHECK (action IN ('allocate','attach','detach','resize','renew','suspend','resume','delete','inject_secret','prepare_resize'))`
- `CHECK (observation_result IN ('confirmed','rejected','unknown'))`
- `CHECK (execution_epoch IS NULL OR execution_epoch >= 0)`
- `CHECK ((execution_plan_digest IS NULL) = (execution_plan_bytes IS NULL))`
- `CHECK (execution_plan_digest IS NULL OR execution_plan_digest = 'sha256:' || encode(sha256(execution_plan_bytes),'hex'))`
- `CHECK (action <> 'prepare_resize' OR (execution_plan_digest IS NOT NULL AND evidence_ref IS NOT NULL AND observation_result = 'confirmed'))`

索引：
- `resource_actions_set`: `(resource_set_id, created_at DESC, id DESC)`

#### fabric.outbox_events

本Owner状态事务同写immutable事件；revision只在本Owner聚合锁内分配。覆盖 F01, F04, F05, F08, F10, F11, F12, F13, F15, F16, F17。

| 字段 | 类型 | 可空 | 默认 | 字段来源 |
|---|---|---|---|---|
| `id` | `text` | 否 | `—` | `02_database_schema_complete.md#fabric.outbox_events.id` |
| `event_type` | `text` | 否 | `—` | `02_database_schema_complete.md#fabric.outbox_events.event_type` |
| `schema_version` | `integer` | 否 | `—` | `02_database_schema_complete.md#fabric.outbox_events.schema_version` |
| `aggregate_type` | `text` | 否 | `—` | `02_database_schema_complete.md#fabric.outbox_events.aggregate_type` |
| `aggregate_id` | `text` | 否 | `—` | `02_database_schema_complete.md#fabric.outbox_events.aggregate_id` |
| `aggregate_revision` | `bigint` | 否 | `—` | `02_database_schema_complete.md#fabric.outbox_events.aggregate_revision` |
| `tenant_id` | `text` | NULL | `—` | `02_database_schema_complete.md#fabric.outbox_events.tenant_id` |
| `correlation_id` | `text` | 否 | `—` | `02_database_schema_complete.md#fabric.outbox_events.correlation_id` |
| `causation_id` | `text` | NULL | `—` | `02_database_schema_complete.md#fabric.outbox_events.causation_id` |
| `payload` | `jsonb` | 否 | `—` | `02_database_schema_complete.md#fabric.outbox_events.payload` |
| `payload_sha256` | `text` | 否 | `—` | `02_database_schema_complete.md#fabric.outbox_events.payload_sha256` |
| `occurred_at` | `timestamptz` | 否 | `—` | `02_database_schema_complete.md#fabric.outbox_events.occurred_at` |
| `created_at` | `timestamptz` | 否 | `now()` | `02_database_schema_complete.md#fabric.outbox_events.created_at` |

约束：
- `PRIMARY KEY (id)`
- `CHECK (schema_version > 0 AND aggregate_revision >= 0)`
- `CHECK (payload_sha256 ~ '^[0-9a-f]{64}$')`
- `UNIQUE (aggregate_type, aggregate_id, aggregate_revision, event_type)`

索引：
- `outbox_events_aggregate`: `(aggregate_type, aggregate_id, aggregate_revision)`

#### fabric.outbox_deliveries

各consumer独立ACK，禁止单个ACK丢其他消费者事件；lease仅本库。覆盖 F05, F08, F10, F11, F12, F13, F15, F17。

| 字段 | 类型 | 可空 | 默认 | 字段来源 |
|---|---|---|---|---|
| `id` | `text` | 否 | `—` | `02_database_schema_complete.md#fabric.outbox_deliveries.id` |
| `event_id` | `text` | 否 | `—` | `02_database_schema_complete.md#fabric.outbox_deliveries.event_id` |
| `consumer_owner` | `text` | 否 | `—` | `02_database_schema_complete.md#fabric.outbox_deliveries.consumer_owner` |
| `attempt_count` | `integer` | 否 | `0` | `02_database_schema_complete.md#fabric.outbox_deliveries.attempt_count` |
| `next_attempt_at` | `timestamptz` | 否 | `now()` | `02_database_schema_complete.md#fabric.outbox_deliveries.next_attempt_at` |
| `acknowledged_at` | `timestamptz` | NULL | `—` | `02_database_schema_complete.md#fabric.outbox_deliveries.acknowledged_at` |
| `last_error_code` | `text` | NULL | `—` | `02_database_schema_complete.md#fabric.outbox_deliveries.last_error_code` |
| `lease_token` | `text` | NULL | `—` | `02_database_schema_complete.md#fabric.outbox_deliveries.lease_token` |
| `lease_until` | `timestamptz` | NULL | `—` | `02_database_schema_complete.md#fabric.outbox_deliveries.lease_until` |
| `created_at` | `timestamptz` | 否 | `now()` | `02_database_schema_complete.md#fabric.outbox_deliveries.created_at` |
| `updated_at` | `timestamptz` | 否 | `now()` | `02_database_schema_complete.md#fabric.outbox_deliveries.updated_at` |

约束：
- `PRIMARY KEY (id)`
- `FOREIGN KEY (event_id) REFERENCES fabric.outbox_events (id) ON DELETE RESTRICT`
- `UNIQUE (event_id, consumer_owner)`
- `CHECK (attempt_count >= 0)`
- `CHECK ((lease_token IS NULL) = (lease_until IS NULL))`

索引：
- `outbox_deliveries_pending`: `(next_attempt_at, id)` WHERE `acknowledged_at IS NULL`

#### fabric.inbox_events

去重hash/业务状态/processed_at/后续Outbox同本库事务；乱序不能覆盖新版本。覆盖 F05, F08, F10, F11, F12, F13, F15, F17。

| 字段 | 类型 | 可空 | 默认 | 字段来源 |
|---|---|---|---|---|
| `id` | `text` | 否 | `—` | `02_database_schema_complete.md#fabric.inbox_events.id` |
| `source_owner` | `text` | 否 | `—` | `02_database_schema_complete.md#fabric.inbox_events.source_owner` |
| `source_event_id` | `text` | 否 | `—` | `02_database_schema_complete.md#fabric.inbox_events.source_event_id` |
| `event_type` | `text` | 否 | `—` | `02_database_schema_complete.md#fabric.inbox_events.event_type` |
| `schema_version` | `integer` | 否 | `—` | `02_database_schema_complete.md#fabric.inbox_events.schema_version` |
| `aggregate_type` | `text` | 否 | `—` | `02_database_schema_complete.md#fabric.inbox_events.aggregate_type` |
| `aggregate_id` | `text` | 否 | `—` | `02_database_schema_complete.md#fabric.inbox_events.aggregate_id` |
| `aggregate_revision` | `bigint` | 否 | `—` | `02_database_schema_complete.md#fabric.inbox_events.aggregate_revision` |
| `payload_sha256` | `text` | 否 | `—` | `02_database_schema_complete.md#fabric.inbox_events.payload_sha256` |
| `payload` | `jsonb` | 否 | `—` | `02_database_schema_complete.md#fabric.inbox_events.payload` |
| `received_at` | `timestamptz` | 否 | `now()` | `02_database_schema_complete.md#fabric.inbox_events.received_at` |
| `processed_at` | `timestamptz` | NULL | `—` | `02_database_schema_complete.md#fabric.inbox_events.processed_at` |
| `result_resource_id` | `text` | NULL | `—` | `02_database_schema_complete.md#fabric.inbox_events.result_resource_id` |
| `error_code` | `text` | NULL | `—` | `02_database_schema_complete.md#fabric.inbox_events.error_code` |

约束：
- `PRIMARY KEY (id)`
- `UNIQUE (source_owner, source_event_id)`
- `CHECK (schema_version > 0 AND aggregate_revision >= 0)`
- `CHECK (payload_sha256 ~ '^[0-9a-f]{64}$')`

索引：
- `inbox_events_pending`: `(received_at, id)` WHERE `processed_at IS NULL`
- `inbox_events_aggregate`: `(source_owner, aggregate_type, aggregate_id, aggregate_revision)`

#### fabric.idempotency_records

命令创建同事务；operation_name是API操作名不是新Operation ID；安全响应无Key明文；义务存续无TTL清理。覆盖 F01, F03, F04, F05, F08, F10, F11, F12, F13, F14, F15, F16, F17。

| 字段 | 类型 | 可空 | 默认 | 字段来源 |
|---|---|---|---|---|
| `id` | `text` | 否 | `—` | `02_database_schema_complete.md#fabric.idempotency_records.id` |
| `tenant_scope` | `text` | 否 | `—` | `02_database_schema_complete.md#fabric.idempotency_records.tenant_scope` |
| `actor_scope` | `text` | 否 | `—` | `02_database_schema_complete.md#fabric.idempotency_records.actor_scope` |
| `operation_name` | `text` | 否 | `—` | `02_database_schema_complete.md#fabric.idempotency_records.operation_name` |
| `idempotency_key` | `text` | 否 | `—` | `02_database_schema_complete.md#fabric.idempotency_records.idempotency_key` |
| `request_sha256` | `text` | 否 | `—` | `02_database_schema_complete.md#fabric.idempotency_records.request_sha256` |
| `resource_id` | `text` | 否 | `—` | `02_database_schema_complete.md#fabric.idempotency_records.resource_id` |
| `operation_id` | `text` | NULL | `—` | `02_database_schema_complete.md#fabric.idempotency_records.operation_id` |
| `response_status` | `integer` | 否 | `—` | `02_database_schema_complete.md#fabric.idempotency_records.response_status` |
| `response_body` | `jsonb` | 否 | `—` | `02_database_schema_complete.md#fabric.idempotency_records.response_body` |
| `created_at` | `timestamptz` | 否 | `now()` | `02_database_schema_complete.md#fabric.idempotency_records.created_at` |

约束：
- `PRIMARY KEY (id)`
- `UNIQUE (tenant_scope, actor_scope, operation_name, idempotency_key)`
- `CHECK (request_sha256 ~ '^[0-9a-f]{64}$')`
- `CHECK (response_status BETWEEN 100 AND 599)`

索引：
- `idempotency_records_resource`: `(resource_id)`

#### fabric.operations

目标Owner持异步Operation；BFF路由无中央writer；Build创建201回Job；只有Workspace配Saga。覆盖 F01, F03, F04, F06, F08, F09, F10, F11, F12, F13, F14, F15, F17。

| 字段 | 类型 | 可空 | 默认 | 字段来源 |
|---|---|---|---|---|
| `id` | `text` | 否 | `—` | `02_database_schema_complete.md#fabric.operations.id` |
| `tenant_id` | `text` | NULL | `—` | `02_database_schema_complete.md#fabric.operations.tenant_id` |
| `actor_id` | `text` | 否 | `—` | `02_database_schema_complete.md#fabric.operations.actor_id` |
| `kind` | `text` | 否 | `—` | `02_database_schema_complete.md#fabric.operations.kind` |
| `resource_id` | `text` | 否 | `—` | `02_database_schema_complete.md#fabric.operations.resource_id` |
| `status` | `text` | 否 | `'accepted'` | `02_database_schema_complete.md#fabric.operations.status` |
| `stage` | `text` | 否 | `—` | `02_database_schema_complete.md#fabric.operations.stage` |
| `error_code` | `text` | NULL | `—` | `02_database_schema_complete.md#fabric.operations.error_code` |
| `observation_result` | `text` | NULL | `—` | `02_database_schema_complete.md#fabric.operations.observation_result` |
| `request_id` | `text` | 否 | `—` | `02_database_schema_complete.md#fabric.operations.request_id` |
| `accepted_input` | `jsonb` | 否 | `—` | `02_database_schema_complete.md#fabric.operations.accepted_input` |
| `result` | `jsonb` | NULL | `—` | `02_database_schema_complete.md#fabric.operations.result` |
| `worker_lease_token` | `text` | NULL | `—` | `02_database_schema_complete.md#fabric.operations.worker_lease_token` |
| `worker_lease_until` | `timestamptz` | NULL | `—` | `02_database_schema_complete.md#fabric.operations.worker_lease_until` |
| `started_at` | `timestamptz` | NULL | `—` | `02_database_schema_complete.md#fabric.operations.started_at` |
| `completed_at` | `timestamptz` | NULL | `—` | `02_database_schema_complete.md#fabric.operations.completed_at` |
| `created_at` | `timestamptz` | 否 | `now()` | `02_database_schema_complete.md#fabric.operations.created_at` |
| `updated_at` | `timestamptz` | 否 | `now()` | `02_database_schema_complete.md#fabric.operations.updated_at` |

约束：
- `PRIMARY KEY (id)`
- `CHECK (status IN ('accepted','running','awaiting_confirmation','succeeded','failed','needs_attention','cancelled'))`
- `CHECK (observation_result IN ('confirmed','rejected','unknown'))`
- `CHECK ((worker_lease_token IS NULL) = (worker_lease_until IS NULL))`
- `CHECK (status <> 'succeeded' OR completed_at IS NOT NULL)`

索引：
- `operations_resource`: `(resource_id, created_at DESC, id DESC)`
- `operations_tenant_list`: `(tenant_id, created_at DESC, id DESC)`
- `operations_recovery`: `(status, updated_at)`

#### fabric.route_bindings

Fabric alone owns observed route generation; Workspace-assigned execution epoch fences stale workers; generation advances only on verified route readback。覆盖 F08, F09, F10, F13。

| 字段 | 类型 | 可空 | 默认 | 字段来源 |
|---|---|---|---|---|
| `id` | `text` | 否 | `—` | `02_database_schema_complete.md#fabric.route_bindings.id` |
| `workspace_id` | `text` | 否 | `—` | `02_database_schema_complete.md#fabric.route_bindings.workspace_id` |
| `route_generation` | `bigint` | 否 | `0` | `02_database_schema_complete.md#fabric.route_bindings.route_generation` |
| `accepted_execution_epoch` | `bigint` | 否 | `0` | `02_database_schema_complete.md#fabric.route_bindings.accepted_execution_epoch` |
| `target_execution_resource_id` | `text` | NULL | `—` | `02_database_schema_complete.md#fabric.route_bindings.target_execution_resource_id` |
| `last_confirmed_switch_id` | `text` | NULL | `—` | `02_database_schema_complete.md#fabric.route_bindings.last_confirmed_switch_id` |
| `observed_at` | `timestamptz` | NULL | `—` | `02_database_schema_complete.md#fabric.route_bindings.observed_at` |
| `created_at` | `timestamptz` | 否 | `now()` | `02_database_schema_complete.md#fabric.route_bindings.created_at` |
| `updated_at` | `timestamptz` | 否 | `now()` | `02_database_schema_complete.md#fabric.route_bindings.updated_at` |
| `provider_revision` | `text` | NULL | `—` | `02_database_schema_complete.md#fabric.route_bindings.provider_revision` |

约束：
- `PRIMARY KEY (id)`
- `FOREIGN KEY (target_execution_resource_id) REFERENCES fabric.resources (id) ON DELETE RESTRICT`
- `UNIQUE (workspace_id)`
- `UNIQUE (id, workspace_id)`
- `CHECK (route_generation >= 0 AND accepted_execution_epoch >= 0)`
- `FOREIGN KEY (last_confirmed_switch_id) REFERENCES fabric.route_switches (id) ON DELETE RESTRICT`

索引：
- `route_bindings_target`: `(target_execution_resource_id)`

#### fabric.route_switches

Provider conditional revision CAS covers target plus epoch metadata; confirmed fence preserves target/generation but advances epoch/revision, then activate/rollback advances generation; any unknown blocks all new route actions。覆盖 F08, F10, F13。

| 字段 | 类型 | 可空 | 默认 | 字段来源 |
|---|---|---|---|---|
| `id` | `text` | 否 | `—` | `02_database_schema_complete.md#fabric.route_switches.id` |
| `route_binding_id` | `text` | 否 | `—` | `02_database_schema_complete.md#fabric.route_switches.route_binding_id` |
| `workspace_id` | `text` | 否 | `—` | `02_database_schema_complete.md#fabric.route_switches.workspace_id` |
| `operation_owner` | `text` | 否 | `—` | `02_database_schema_complete.md#fabric.route_switches.operation_owner` |
| `operation_id` | `text` | 否 | `—` | `02_database_schema_complete.md#fabric.route_switches.operation_id` |
| `expected_route_generation` | `bigint` | 否 | `—` | `02_database_schema_complete.md#fabric.route_switches.expected_route_generation` |
| `execution_epoch` | `bigint` | 否 | `—` | `02_database_schema_complete.md#fabric.route_switches.execution_epoch` |
| `target_execution_resource_id` | `text` | NULL | `—` | `02_database_schema_complete.md#fabric.route_switches.target_execution_resource_id` |
| `previous_target_execution_resource_id` | `text` | NULL | `—` | `02_database_schema_complete.md#fabric.route_switches.previous_target_execution_resource_id` |
| `provider_command_id` | `text` | 否 | `—` | `02_database_schema_complete.md#fabric.route_switches.provider_command_id` |
| `provider_request_ref` | `text` | NULL | `—` | `02_database_schema_complete.md#fabric.route_switches.provider_request_ref` |
| `status` | `text` | 否 | `'requested'` | `02_database_schema_complete.md#fabric.route_switches.status` |
| `observed_route_generation` | `bigint` | NULL | `—` | `02_database_schema_complete.md#fabric.route_switches.observed_route_generation` |
| `observed_execution_epoch` | `bigint` | NULL | `—` | `02_database_schema_complete.md#fabric.route_switches.observed_execution_epoch` |
| `evidence_ref` | `text` | NULL | `—` | `02_database_schema_complete.md#fabric.route_switches.evidence_ref` |
| `workspace_selection_commit_receipt_id` | `text` | NULL | `—` | `02_database_schema_complete.md#fabric.route_switches.workspace_selection_commit_receipt_id` |
| `error_code` | `text` | NULL | `—` | `02_database_schema_complete.md#fabric.route_switches.error_code` |
| `created_at` | `timestamptz` | 否 | `now()` | `02_database_schema_complete.md#fabric.route_switches.created_at` |
| `updated_at` | `timestamptz` | 否 | `now()` | `02_database_schema_complete.md#fabric.route_switches.updated_at` |
| `action_kind` | `text` | 否 | `—` | `02_database_schema_complete.md#fabric.route_switches.action_kind` |
| `expected_provider_revision` | `text` | NULL | `—` | `02_database_schema_complete.md#fabric.route_switches.expected_provider_revision` |
| `observed_provider_revision` | `text` | NULL | `—` | `02_database_schema_complete.md#fabric.route_switches.observed_provider_revision` |
| `expected_absence_receipt_id` | `text` | NULL | `—` | `02_database_schema_complete.md#fabric.route_switches.expected_absence_receipt_id` |
| `expected_absence_observed_at` | `timestamptz` | NULL | `—` | `02_database_schema_complete.md#fabric.route_switches.expected_absence_observed_at` |

约束：
- `PRIMARY KEY (id)`
- `FOREIGN KEY (route_binding_id, workspace_id) REFERENCES fabric.route_bindings (id, workspace_id) ON DELETE RESTRICT`
- `FOREIGN KEY (target_execution_resource_id) REFERENCES fabric.resources (id) ON DELETE RESTRICT`
- `FOREIGN KEY (previous_target_execution_resource_id) REFERENCES fabric.resources (id) ON DELETE RESTRICT`
- `CHECK (status IN ('requested','confirmed','rejected','unknown'))`
- `CHECK (expected_route_generation >= 0 AND execution_epoch >= 0)`
- `UNIQUE (provider_command_id)`
- `CHECK (workspace_selection_commit_receipt_id IS NULL OR status = 'confirmed')`
- `CHECK (action_kind IN ('fence','activate','rollback'))`
- `CHECK (action_kind = 'fence' OR target_execution_resource_id IS NOT NULL)`
- `CHECK (expected_provider_revision IS NOT NULL OR expected_route_generation = 0)`
- `CHECK (status <> 'confirmed' OR ((observed_route_generation = expected_route_generation + CASE WHEN action_kind = 'fence' THEN 0 ELSE 1 END AND observed_execution_epoch = execution_epoch AND observed_provider_revision IS NOT NULL AND evidence_ref IS NOT NULL) IS TRUE))`
- `CHECK ((expected_provider_revision IS NOT NULL AND expected_absence_receipt_id IS NULL AND expected_absence_observed_at IS NULL) OR (expected_provider_revision IS NULL AND expected_route_generation = 0 AND expected_absence_receipt_id IS NOT NULL AND expected_absence_observed_at IS NOT NULL))`

索引：
- `route_switches_one_pending`: UNIQUE `(route_binding_id)` WHERE `status IN ('requested','unknown')`
- `route_switches_operation`: `(operation_owner, operation_id, created_at DESC, id DESC)`

### gateway

Database `opl_gateway` · Schema `gateway` · Writer `opl_gateway_writer`。

#### gateway.identity_mappings

每成员独立Gateway身份；与Tenant钱包付款主体分开。覆盖 F01, F14, F16。

| 字段 | 类型 | 可空 | 默认 | 字段来源 |
|---|---|---|---|---|
| `id` | `text` | 否 | `—` | `02_database_schema_complete.md#gateway.identity_mappings.id` |
| `actor_id` | `text` | 否 | `—` | `02_database_schema_complete.md#gateway.identity_mappings.actor_id` |
| `sub2api_user_id` | `text` | 否 | `—` | `02_database_schema_complete.md#gateway.identity_mappings.sub2api_user_id` |
| `external_identity_ref` | `text` | 否 | `—` | `02_database_schema_complete.md#gateway.identity_mappings.external_identity_ref` |
| `verified_at` | `timestamptz` | 否 | `—` | `02_database_schema_complete.md#gateway.identity_mappings.verified_at` |
| `created_at` | `timestamptz` | 否 | `now()` | `02_database_schema_complete.md#gateway.identity_mappings.created_at` |
| `updated_at` | `timestamptz` | 否 | `now()` | `02_database_schema_complete.md#gateway.identity_mappings.updated_at` |

约束：
- `PRIMARY KEY (id)`
- `UNIQUE (actor_id)`
- `UNIQUE (sub2api_user_id)`

索引：
- `identity_mappings_external`: `(external_identity_ref)`

#### gateway.tenant_wallet_bindings

唯一活动钱包主体映射；跨成员费用动作必须外部Gateway明确委托能力，未证实不可擅自模拟。覆盖 F01, F08, F14, F15, F16。

| 字段 | 类型 | 可空 | 默认 | 字段来源 |
|---|---|---|---|---|
| `id` | `text` | 否 | `—` | `02_database_schema_complete.md#gateway.tenant_wallet_bindings.id` |
| `tenant_id` | `text` | 否 | `—` | `02_database_schema_complete.md#gateway.tenant_wallet_bindings.tenant_id` |
| `billing_sub2api_user_id` | `text` | 否 | `—` | `02_database_schema_complete.md#gateway.tenant_wallet_bindings.billing_sub2api_user_id` |
| `delegation_ref` | `text` | 否 | `—` | `02_database_schema_complete.md#gateway.tenant_wallet_bindings.delegation_ref` |
| `verification_evidence_ref` | `text` | 否 | `—` | `02_database_schema_complete.md#gateway.tenant_wallet_bindings.verification_evidence_ref` |
| `bound_at` | `timestamptz` | 否 | `—` | `02_database_schema_complete.md#gateway.tenant_wallet_bindings.bound_at` |
| `revoked_at` | `timestamptz` | NULL | `—` | `02_database_schema_complete.md#gateway.tenant_wallet_bindings.revoked_at` |
| `created_at` | `timestamptz` | 否 | `now()` | `02_database_schema_complete.md#gateway.tenant_wallet_bindings.created_at` |
| `updated_at` | `timestamptz` | 否 | `now()` | `02_database_schema_complete.md#gateway.tenant_wallet_bindings.updated_at` |

约束：
- `PRIMARY KEY (id)`

索引：
- `wallet_binding_tenant`: UNIQUE `(tenant_id)` WHERE `revoked_at IS NULL`
- `wallet_binding_subject`: UNIQUE `(billing_sub2api_user_id)` WHERE `revoked_at IS NULL`

#### gateway.key_bindings

轮换新Key验证后撤旧Key，允许短时两绑定；Secret不进事件/日志/数据库；不储Key明文。覆盖 F08, F09, F13, F14, F16。

| 字段 | 类型 | 可空 | 默认 | 字段来源 |
|---|---|---|---|---|
| `id` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/GatewayKey/properties/id` |
| `tenant_id` | `text` | 否 | `—` | `02_database_schema_complete.md#gateway.key_bindings.tenant_id` |
| `workspace_id` | `text` | NULL | `—` | `03_api_contract_complete.yaml#/components/schemas/GatewayKey/properties/workspaceId` |
| `actor_id` | `text` | NULL | `—` | `02_database_schema_complete.md#gateway.key_bindings.actor_id` |
| `external_key_id` | `text` | 否 | `—` | `02_database_schema_complete.md#gateway.key_bindings.external_key_id` |
| `fingerprint` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/GatewayKey/properties/fingerprint` |
| `secret_ref` | `text` | NULL | `—` | `02_database_schema_complete.md#gateway.key_bindings.secret_ref` |
| `purpose` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/GatewayKey/properties/purpose` |
| `model_ids` | `text[]` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/GatewayKey/properties/modelIds` |
| `rotation_of_key_binding_id` | `text` | NULL | `—` | `02_database_schema_complete.md#gateway.key_bindings.rotation_of_key_binding_id` |
| `observation_result` | `text` | 否 | `—` | `02_database_schema_complete.md#gateway.key_bindings.observation_result` |
| `revoked_at` | `timestamptz` | NULL | `—` | `02_database_schema_complete.md#gateway.key_bindings.revoked_at` |
| `created_at` | `timestamptz` | 否 | `now()` | `03_api_contract_complete.yaml#/components/schemas/GatewayKey/properties/createdAt` |
| `updated_at` | `timestamptz` | 否 | `now()` | `02_database_schema_complete.md#gateway.key_bindings.updated_at` |
| `name` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/GatewayKey/properties/name` |
| `expires_at` | `timestamptz` | NULL | `—` | `03_api_contract_complete.yaml#/components/schemas/GatewayKey/properties/expiresAt` |

约束：
- `PRIMARY KEY (id)`
- `FOREIGN KEY (rotation_of_key_binding_id) REFERENCES gateway.key_bindings (id) ON DELETE RESTRICT`
- `UNIQUE (external_key_id)`
- `CHECK (purpose IN ('workspace_managed','personal'))`
- `CHECK (observation_result IN ('confirmed','rejected','unknown'))`
- `CHECK ((purpose = 'workspace_managed') = (workspace_id IS NOT NULL))`
- `CHECK (purpose <> 'workspace_managed' OR secret_ref IS NOT NULL)`

索引：
- `key_bindings_workspace`: `(workspace_id)`
- `key_bindings_tenant`: `(tenant_id, created_at DESC, id DESC)`

#### gateway.wallet_operations

Gateway请求事实不是wallet；unknown不得重复扣费或逆向退款；refund依原charge及资格。覆盖 F08, F11, F12, F13, F14, F16。

| 字段 | 类型 | 可空 | 默认 | 字段来源 |
|---|---|---|---|---|
| `id` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/WalletOperation/properties/id` |
| `tenant_id` | `text` | 否 | `—` | `02_database_schema_complete.md#gateway.wallet_operations.tenant_id` |
| `wallet_binding_id` | `text` | 否 | `—` | `02_database_schema_complete.md#gateway.wallet_operations.wallet_binding_id` |
| `workspace_id` | `text` | NULL | `—` | `03_api_contract_complete.yaml#/components/schemas/WalletOperation/properties/workspaceId` |
| `kind` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/WalletOperation/properties/kind` |
| `status` | `text` | 否 | `'requested'` | `03_api_contract_complete.yaml#/components/schemas/WalletOperation/properties/status` |
| `amount_usd_micros` | `bigint` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/WalletOperation/properties/amountUSDMicros` |
| `original_wallet_operation_id` | `text` | NULL | `—` | `03_api_contract_complete.yaml#/components/schemas/WalletOperation/properties/originalChargeOperationId` |
| `business_idempotency_key` | `text` | 否 | `—` | `02_database_schema_complete.md#gateway.wallet_operations.business_idempotency_key` |
| `request_fingerprint` | `text` | 否 | `—` | `02_database_schema_complete.md#gateway.wallet_operations.request_fingerprint` |
| `external_reference` | `text` | NULL | `—` | `03_api_contract_complete.yaml#/components/schemas/WalletOperation/properties/externalReference` |
| `receipt_id` | `text` | NULL | `—` | `03_api_contract_complete.yaml#/components/schemas/WalletOperation/properties/receiptId` |
| `refund_entitlement_ref` | `text` | NULL | `—` | `02_database_schema_complete.md#gateway.wallet_operations.refund_entitlement_ref` |
| `authorization_receipt_ref` | `text` | NULL | `—` | `02_database_schema_complete.md#gateway.wallet_operations.authorization_receipt_ref` |
| `error_code` | `text` | NULL | `—` | `03_api_contract_complete.yaml#/components/schemas/WalletOperation/properties/errorCode` |
| `confirmed_at` | `timestamptz` | NULL | `—` | `02_database_schema_complete.md#gateway.wallet_operations.confirmed_at` |
| `created_at` | `timestamptz` | 否 | `now()` | `03_api_contract_complete.yaml#/components/schemas/WalletOperation/properties/createdAt` |
| `updated_at` | `timestamptz` | 否 | `now()` | `03_api_contract_complete.yaml#/components/schemas/WalletOperation/properties/updatedAt` |
| `refund_entitlement_snapshot` | `jsonb` | NULL | `—` | `02_database_schema_complete.md#gateway.wallet_operations.refund_entitlement_snapshot` |
| `purpose` | `text` | NULL | `—` | `03_api_contract_complete.yaml#/components/schemas/WalletOperation/properties/purpose` |
| `plan_change_id` | `text` | NULL | `—` | `03_api_contract_complete.yaml#/components/schemas/WalletOperation/properties/planChangeId` |
| `coverage_start` | `timestamptz` | NULL | `—` | `03_api_contract_complete.yaml#/components/schemas/WalletOperation/properties/coverageStart` |
| `coverage_end` | `timestamptz` | NULL | `—` | `03_api_contract_complete.yaml#/components/schemas/WalletOperation/properties/coverageEnd` |

约束：
- `PRIMARY KEY (id)`
- `FOREIGN KEY (wallet_binding_id) REFERENCES gateway.tenant_wallet_bindings (id) ON DELETE RESTRICT`
- `FOREIGN KEY (original_wallet_operation_id) REFERENCES gateway.wallet_operations (id) ON DELETE RESTRICT`
- `CHECK (kind IN ('charge','refund','recharge'))`
- `CHECK (status IN ('requested','confirmed','rejected','unknown'))`
- `UNIQUE (business_idempotency_key)`
- `CHECK (amount_usd_micros > 0)`
- `CHECK (kind <> 'refund' OR (original_wallet_operation_id IS NOT NULL AND refund_entitlement_ref IS NOT NULL))`
- `CHECK (status <> 'confirmed' OR (external_reference IS NOT NULL AND confirmed_at IS NOT NULL))`
- `CHECK (purpose IN ('base_period','upgrade_supplement','base_period_delete','upgrade_failure_full','supplement_delete_unused','next_period_plan_failure_full','recharge'))`
- `CHECK ((coverage_start IS NULL AND coverage_end IS NULL) OR (coverage_start IS NOT NULL AND coverage_end > coverage_start))`
- `CHECK (purpose <> 'upgrade_supplement' OR (kind = 'charge' AND plan_change_id IS NOT NULL AND coverage_start IS NOT NULL AND coverage_end IS NOT NULL))`
- `CHECK (purpose NOT IN ('base_period_delete','upgrade_failure_full','supplement_delete_unused','next_period_plan_failure_full') OR (kind = 'refund' AND original_wallet_operation_id IS NOT NULL AND refund_entitlement_snapshot IS NOT NULL))`

索引：
- `wallet_operations_external`: UNIQUE `(external_reference)` WHERE `external_reference IS NOT NULL`
- `wallet_operations_tenant`: `(tenant_id, created_at DESC, id DESC)`
- `wallet_operations_original`: `(original_wallet_operation_id)`

#### gateway.outbox_events

本Owner状态事务同写immutable事件；revision只在本Owner聚合锁内分配。覆盖 F01, F04, F05, F08, F10, F11, F12, F13, F15, F16, F17。

| 字段 | 类型 | 可空 | 默认 | 字段来源 |
|---|---|---|---|---|
| `id` | `text` | 否 | `—` | `02_database_schema_complete.md#gateway.outbox_events.id` |
| `event_type` | `text` | 否 | `—` | `02_database_schema_complete.md#gateway.outbox_events.event_type` |
| `schema_version` | `integer` | 否 | `—` | `02_database_schema_complete.md#gateway.outbox_events.schema_version` |
| `aggregate_type` | `text` | 否 | `—` | `02_database_schema_complete.md#gateway.outbox_events.aggregate_type` |
| `aggregate_id` | `text` | 否 | `—` | `02_database_schema_complete.md#gateway.outbox_events.aggregate_id` |
| `aggregate_revision` | `bigint` | 否 | `—` | `02_database_schema_complete.md#gateway.outbox_events.aggregate_revision` |
| `tenant_id` | `text` | NULL | `—` | `02_database_schema_complete.md#gateway.outbox_events.tenant_id` |
| `correlation_id` | `text` | 否 | `—` | `02_database_schema_complete.md#gateway.outbox_events.correlation_id` |
| `causation_id` | `text` | NULL | `—` | `02_database_schema_complete.md#gateway.outbox_events.causation_id` |
| `payload` | `jsonb` | 否 | `—` | `02_database_schema_complete.md#gateway.outbox_events.payload` |
| `payload_sha256` | `text` | 否 | `—` | `02_database_schema_complete.md#gateway.outbox_events.payload_sha256` |
| `occurred_at` | `timestamptz` | 否 | `—` | `02_database_schema_complete.md#gateway.outbox_events.occurred_at` |
| `created_at` | `timestamptz` | 否 | `now()` | `02_database_schema_complete.md#gateway.outbox_events.created_at` |

约束：
- `PRIMARY KEY (id)`
- `CHECK (schema_version > 0 AND aggregate_revision >= 0)`
- `CHECK (payload_sha256 ~ '^[0-9a-f]{64}$')`
- `UNIQUE (aggregate_type, aggregate_id, aggregate_revision, event_type)`

索引：
- `outbox_events_aggregate`: `(aggregate_type, aggregate_id, aggregate_revision)`

#### gateway.outbox_deliveries

各consumer独立ACK，禁止单个ACK丢其他消费者事件；lease仅本库。覆盖 F05, F08, F10, F11, F12, F13, F15, F17。

| 字段 | 类型 | 可空 | 默认 | 字段来源 |
|---|---|---|---|---|
| `id` | `text` | 否 | `—` | `02_database_schema_complete.md#gateway.outbox_deliveries.id` |
| `event_id` | `text` | 否 | `—` | `02_database_schema_complete.md#gateway.outbox_deliveries.event_id` |
| `consumer_owner` | `text` | 否 | `—` | `02_database_schema_complete.md#gateway.outbox_deliveries.consumer_owner` |
| `attempt_count` | `integer` | 否 | `0` | `02_database_schema_complete.md#gateway.outbox_deliveries.attempt_count` |
| `next_attempt_at` | `timestamptz` | 否 | `now()` | `02_database_schema_complete.md#gateway.outbox_deliveries.next_attempt_at` |
| `acknowledged_at` | `timestamptz` | NULL | `—` | `02_database_schema_complete.md#gateway.outbox_deliveries.acknowledged_at` |
| `last_error_code` | `text` | NULL | `—` | `02_database_schema_complete.md#gateway.outbox_deliveries.last_error_code` |
| `lease_token` | `text` | NULL | `—` | `02_database_schema_complete.md#gateway.outbox_deliveries.lease_token` |
| `lease_until` | `timestamptz` | NULL | `—` | `02_database_schema_complete.md#gateway.outbox_deliveries.lease_until` |
| `created_at` | `timestamptz` | 否 | `now()` | `02_database_schema_complete.md#gateway.outbox_deliveries.created_at` |
| `updated_at` | `timestamptz` | 否 | `now()` | `02_database_schema_complete.md#gateway.outbox_deliveries.updated_at` |

约束：
- `PRIMARY KEY (id)`
- `FOREIGN KEY (event_id) REFERENCES gateway.outbox_events (id) ON DELETE RESTRICT`
- `UNIQUE (event_id, consumer_owner)`
- `CHECK (attempt_count >= 0)`
- `CHECK ((lease_token IS NULL) = (lease_until IS NULL))`

索引：
- `outbox_deliveries_pending`: `(next_attempt_at, id)` WHERE `acknowledged_at IS NULL`

#### gateway.inbox_events

去重hash/业务状态/processed_at/后续Outbox同本库事务；乱序不能覆盖新版本。覆盖 F05, F08, F10, F11, F12, F13, F15, F17。

| 字段 | 类型 | 可空 | 默认 | 字段来源 |
|---|---|---|---|---|
| `id` | `text` | 否 | `—` | `02_database_schema_complete.md#gateway.inbox_events.id` |
| `source_owner` | `text` | 否 | `—` | `02_database_schema_complete.md#gateway.inbox_events.source_owner` |
| `source_event_id` | `text` | 否 | `—` | `02_database_schema_complete.md#gateway.inbox_events.source_event_id` |
| `event_type` | `text` | 否 | `—` | `02_database_schema_complete.md#gateway.inbox_events.event_type` |
| `schema_version` | `integer` | 否 | `—` | `02_database_schema_complete.md#gateway.inbox_events.schema_version` |
| `aggregate_type` | `text` | 否 | `—` | `02_database_schema_complete.md#gateway.inbox_events.aggregate_type` |
| `aggregate_id` | `text` | 否 | `—` | `02_database_schema_complete.md#gateway.inbox_events.aggregate_id` |
| `aggregate_revision` | `bigint` | 否 | `—` | `02_database_schema_complete.md#gateway.inbox_events.aggregate_revision` |
| `payload_sha256` | `text` | 否 | `—` | `02_database_schema_complete.md#gateway.inbox_events.payload_sha256` |
| `payload` | `jsonb` | 否 | `—` | `02_database_schema_complete.md#gateway.inbox_events.payload` |
| `received_at` | `timestamptz` | 否 | `now()` | `02_database_schema_complete.md#gateway.inbox_events.received_at` |
| `processed_at` | `timestamptz` | NULL | `—` | `02_database_schema_complete.md#gateway.inbox_events.processed_at` |
| `result_resource_id` | `text` | NULL | `—` | `02_database_schema_complete.md#gateway.inbox_events.result_resource_id` |
| `error_code` | `text` | NULL | `—` | `02_database_schema_complete.md#gateway.inbox_events.error_code` |

约束：
- `PRIMARY KEY (id)`
- `UNIQUE (source_owner, source_event_id)`
- `CHECK (schema_version > 0 AND aggregate_revision >= 0)`
- `CHECK (payload_sha256 ~ '^[0-9a-f]{64}$')`

索引：
- `inbox_events_pending`: `(received_at, id)` WHERE `processed_at IS NULL`
- `inbox_events_aggregate`: `(source_owner, aggregate_type, aggregate_id, aggregate_revision)`

#### gateway.idempotency_records

命令创建同事务；operation_name是API操作名不是新Operation ID；安全响应无Key明文；义务存续无TTL清理。覆盖 F01, F03, F04, F05, F08, F10, F11, F12, F13, F14, F15, F16, F17。

| 字段 | 类型 | 可空 | 默认 | 字段来源 |
|---|---|---|---|---|
| `id` | `text` | 否 | `—` | `02_database_schema_complete.md#gateway.idempotency_records.id` |
| `tenant_scope` | `text` | 否 | `—` | `02_database_schema_complete.md#gateway.idempotency_records.tenant_scope` |
| `actor_scope` | `text` | 否 | `—` | `02_database_schema_complete.md#gateway.idempotency_records.actor_scope` |
| `operation_name` | `text` | 否 | `—` | `02_database_schema_complete.md#gateway.idempotency_records.operation_name` |
| `idempotency_key` | `text` | 否 | `—` | `02_database_schema_complete.md#gateway.idempotency_records.idempotency_key` |
| `request_sha256` | `text` | 否 | `—` | `02_database_schema_complete.md#gateway.idempotency_records.request_sha256` |
| `resource_id` | `text` | 否 | `—` | `02_database_schema_complete.md#gateway.idempotency_records.resource_id` |
| `operation_id` | `text` | NULL | `—` | `02_database_schema_complete.md#gateway.idempotency_records.operation_id` |
| `response_status` | `integer` | 否 | `—` | `02_database_schema_complete.md#gateway.idempotency_records.response_status` |
| `response_body` | `jsonb` | 否 | `—` | `02_database_schema_complete.md#gateway.idempotency_records.response_body` |
| `created_at` | `timestamptz` | 否 | `now()` | `02_database_schema_complete.md#gateway.idempotency_records.created_at` |

约束：
- `PRIMARY KEY (id)`
- `UNIQUE (tenant_scope, actor_scope, operation_name, idempotency_key)`
- `CHECK (request_sha256 ~ '^[0-9a-f]{64}$')`
- `CHECK (response_status BETWEEN 100 AND 599)`

索引：
- `idempotency_records_resource`: `(resource_id)`

#### gateway.operations

目标Owner持异步Operation；BFF路由无中央writer；Build创建201回Job；只有Workspace配Saga。覆盖 F01, F03, F04, F06, F08, F09, F10, F11, F12, F13, F14, F15, F17。

| 字段 | 类型 | 可空 | 默认 | 字段来源 |
|---|---|---|---|---|
| `id` | `text` | 否 | `—` | `02_database_schema_complete.md#gateway.operations.id` |
| `tenant_id` | `text` | NULL | `—` | `02_database_schema_complete.md#gateway.operations.tenant_id` |
| `actor_id` | `text` | 否 | `—` | `02_database_schema_complete.md#gateway.operations.actor_id` |
| `kind` | `text` | 否 | `—` | `02_database_schema_complete.md#gateway.operations.kind` |
| `resource_id` | `text` | 否 | `—` | `02_database_schema_complete.md#gateway.operations.resource_id` |
| `status` | `text` | 否 | `'accepted'` | `02_database_schema_complete.md#gateway.operations.status` |
| `stage` | `text` | 否 | `—` | `02_database_schema_complete.md#gateway.operations.stage` |
| `error_code` | `text` | NULL | `—` | `02_database_schema_complete.md#gateway.operations.error_code` |
| `observation_result` | `text` | NULL | `—` | `02_database_schema_complete.md#gateway.operations.observation_result` |
| `request_id` | `text` | 否 | `—` | `02_database_schema_complete.md#gateway.operations.request_id` |
| `accepted_input` | `jsonb` | 否 | `—` | `02_database_schema_complete.md#gateway.operations.accepted_input` |
| `result` | `jsonb` | NULL | `—` | `02_database_schema_complete.md#gateway.operations.result` |
| `worker_lease_token` | `text` | NULL | `—` | `02_database_schema_complete.md#gateway.operations.worker_lease_token` |
| `worker_lease_until` | `timestamptz` | NULL | `—` | `02_database_schema_complete.md#gateway.operations.worker_lease_until` |
| `started_at` | `timestamptz` | NULL | `—` | `02_database_schema_complete.md#gateway.operations.started_at` |
| `completed_at` | `timestamptz` | NULL | `—` | `02_database_schema_complete.md#gateway.operations.completed_at` |
| `created_at` | `timestamptz` | 否 | `now()` | `02_database_schema_complete.md#gateway.operations.created_at` |
| `updated_at` | `timestamptz` | 否 | `now()` | `02_database_schema_complete.md#gateway.operations.updated_at` |

约束：
- `PRIMARY KEY (id)`
- `CHECK (status IN ('accepted','running','awaiting_confirmation','succeeded','failed','needs_attention','cancelled'))`
- `CHECK (observation_result IN ('confirmed','rejected','unknown'))`
- `CHECK ((worker_lease_token IS NULL) = (worker_lease_until IS NULL))`
- `CHECK (status <> 'succeeded' OR completed_at IS NOT NULL)`

索引：
- `operations_resource`: `(resource_id, created_at DESC, id DESC)`
- `operations_tenant_list`: `(tenant_id, created_at DESC, id DESC)`
- `operations_recovery`: `(status, updated_at)`

### resource_catalog

Database `opl_resource_catalog` · Schema `resource_catalog` · Writer `opl_resource_catalog_writer`。

#### resource_catalog.compute_plans

不可变发布规格/Provider能力；不接受客户任选provider；报价不是模型定价。覆盖 F03, F07, F11, F12。

| 字段 | 类型 | 可空 | 默认 | 字段来源 |
|---|---|---|---|---|
| `id` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/ComputePlan/properties/id` |
| `name` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/ComputePlan/properties/name` |
| `version_label` | `text` | 否 | `—` | `02_database_schema_complete.md#resource_catalog.compute_plans.version_label` |
| `provider` | `text` | 否 | `—` | `02_database_schema_complete.md#resource_catalog.compute_plans.provider` |
| `provider_profile_ref` | `text` | 否 | `—` | `02_database_schema_complete.md#resource_catalog.compute_plans.provider_profile_ref` |
| `region` | `text` | 否 | `—` | `02_database_schema_complete.md#resource_catalog.compute_plans.region` |
| `status` | `text` | 否 | `'approved'` | `02_database_schema_complete.md#resource_catalog.compute_plans.status` |
| `billing_mode` | `text` | 否 | `—` | `02_database_schema_complete.md#resource_catalog.compute_plans.billing_mode` |
| `valid_from` | `timestamptz` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/ComputePlan/properties/validFrom` |
| `valid_until` | `timestamptz` | NULL | `—` | `03_api_contract_complete.yaml#/components/schemas/ComputePlan/properties/validUntil` |
| `published_by` | `text` | 否 | `—` | `02_database_schema_complete.md#resource_catalog.compute_plans.published_by` |
| `created_at` | `timestamptz` | 否 | `now()` | `03_api_contract_complete.yaml#/components/schemas/ComputePlan/properties/createdAt` |
| `updated_at` | `timestamptz` | 否 | `now()` | `02_database_schema_complete.md#resource_catalog.compute_plans.updated_at` |
| `provider_capability_version` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/ComputePlan/properties/providerCapabilityVersion` |
| `provider_specification` | `jsonb` | 否 | `—` | `02_database_schema_complete.md#resource_catalog.compute_plans.provider_specification` |
| `vcpus` | `integer` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/ComputePlan/properties/vcpus` |
| `memory_mib` | `integer` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/ComputePlan/properties/memoryMiB` |

约束：
- `PRIMARY KEY (id)`
- `CHECK (status IN ('approved','deprecated','revoked'))`
- `CHECK (billing_mode IN ('PREPAID_MONTHLY','LOCAL_NO_CHARGE'))`
- `UNIQUE (name, version_label, provider, region)`
- `CHECK (valid_until IS NULL OR valid_until > valid_from)`
- `CHECK (vcpus > 0 AND memory_mib > 0)`

索引：
- `compute_plans_available`: `(status, valid_from DESC, id DESC)`

#### resource_catalog.storage_plans

不可变发布规格/Provider能力；不接受客户任选provider；报价不是模型定价。覆盖 F03, F07, F11, F12。

| 字段 | 类型 | 可空 | 默认 | 字段来源 |
|---|---|---|---|---|
| `id` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/StoragePlan/properties/id` |
| `name` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/StoragePlan/properties/name` |
| `version_label` | `text` | 否 | `—` | `02_database_schema_complete.md#resource_catalog.storage_plans.version_label` |
| `provider` | `text` | 否 | `—` | `02_database_schema_complete.md#resource_catalog.storage_plans.provider` |
| `provider_profile_ref` | `text` | 否 | `—` | `02_database_schema_complete.md#resource_catalog.storage_plans.provider_profile_ref` |
| `region` | `text` | 否 | `—` | `02_database_schema_complete.md#resource_catalog.storage_plans.region` |
| `status` | `text` | 否 | `'approved'` | `02_database_schema_complete.md#resource_catalog.storage_plans.status` |
| `billing_mode` | `text` | 否 | `—` | `02_database_schema_complete.md#resource_catalog.storage_plans.billing_mode` |
| `valid_from` | `timestamptz` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/StoragePlan/properties/validFrom` |
| `valid_until` | `timestamptz` | NULL | `—` | `03_api_contract_complete.yaml#/components/schemas/StoragePlan/properties/validUntil` |
| `published_by` | `text` | 否 | `—` | `02_database_schema_complete.md#resource_catalog.storage_plans.published_by` |
| `created_at` | `timestamptz` | 否 | `now()` | `03_api_contract_complete.yaml#/components/schemas/StoragePlan/properties/createdAt` |
| `updated_at` | `timestamptz` | 否 | `now()` | `02_database_schema_complete.md#resource_catalog.storage_plans.updated_at` |
| `provider_capability_version` | `text` | 否 | `—` | `02_database_schema_complete.md#resource_catalog.storage_plans.provider_capability_version` |
| `provider_specification` | `jsonb` | 否 | `—` | `02_database_schema_complete.md#resource_catalog.storage_plans.provider_specification` |
| `capacity_gib` | `integer` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/StoragePlan/properties/capacityGiB` |
| `shrink_supported` | `boolean` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/StoragePlan/properties/shrinkSupported` |

约束：
- `PRIMARY KEY (id)`
- `CHECK (status IN ('approved','deprecated','revoked'))`
- `CHECK (billing_mode IN ('PREPAID_MONTHLY','LOCAL_NO_CHARGE'))`
- `UNIQUE (name, version_label, provider, region)`
- `CHECK (valid_until IS NULL OR valid_until > valid_from)`
- `CHECK (capacity_gib > 0)`

索引：
- `storage_plans_available`: `(status, valid_from DESC, id DESC)`

#### resource_catalog.price_policy_versions

管理员实际批准价格、变更、续费规则；不设默认费率。覆盖 F03, F07, F11, F12。

| 字段 | 类型 | 可空 | 默认 | 字段来源 |
|---|---|---|---|---|
| `id` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/ComputePlan/properties/pricePolicyVersionId` |
| `version_label` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/PricePolicyVersion/properties/versionLabel` |
| `compute_plan_id` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/PricePolicyVersion/properties/computePlanId` |
| `storage_plan_id` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/PricePolicyVersion/properties/storagePlanId` |
| `compute_monthly_usd_micros` | `bigint` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/ComputePlan/properties/monthlyPriceUSDMicros` |
| `storage_monthly_usd_micros` | `bigint` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/StoragePlan/properties/monthlyPriceUSDMicros` |
| `product_monthly_usd_micros` | `bigint` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/PricePolicyVersion/properties/productMonthlyUSDMicros` |
| `currency` | `text` | 否 | `'USD'` | `03_api_contract_complete.yaml#/components/schemas/PricePolicyVersion/properties/currency` |
| `renewal_rules` | `jsonb` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/PricePolicyVersion/properties/renewalPolicy` |
| `valid_from` | `timestamptz` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/PricePolicyVersion/properties/validFrom` |
| `valid_until` | `timestamptz` | NULL | `—` | `03_api_contract_complete.yaml#/components/schemas/PricePolicyVersion/properties/validUntil` |
| `published_by` | `text` | 否 | `—` | `02_database_schema_complete.md#resource_catalog.price_policy_versions.published_by` |
| `created_at` | `timestamptz` | 否 | `now()` | `03_api_contract_complete.yaml#/components/schemas/PricePolicyVersion/properties/createdAt` |
| `period_months` | `integer` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/PricePolicyVersion/properties/periodMonths` |
| `plan_change_policy_version` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/PricePolicyVersion/properties/planChangePolicyVersion` |

约束：
- `PRIMARY KEY (id)`
- `FOREIGN KEY (compute_plan_id) REFERENCES resource_catalog.compute_plans (id) ON DELETE RESTRICT`
- `FOREIGN KEY (storage_plan_id) REFERENCES resource_catalog.storage_plans (id) ON DELETE RESTRICT`
- `UNIQUE (version_label, compute_plan_id, storage_plan_id)`
- `CHECK (currency = 'USD')`
- `CHECK (compute_monthly_usd_micros >= 0 AND storage_monthly_usd_micros >= 0 AND product_monthly_usd_micros >= 0)`
- `CHECK (valid_until IS NULL OR valid_until > valid_from)`
- `CHECK (period_months > 0)`
- `UNIQUE (compute_plan_id, storage_plan_id, valid_from)`
- `CHECK ((renewal_rules->>'version' = 'renewal-policy/v1' AND renewal_rules->>'trigger' = 'manual_or_explicitly_consented_automatic' AND (renewal_rules->>'months')::integer = period_months AND renewal_rules->>'usesAcceptedPriceSnapshot' = 'true') IS TRUE)`
- `CHECK (plan_change_policy_version IN ('workspace-plan-change-v1'))`
- `CHECK (period_months = 1)`

索引：
- `price_policy_versions_scope`: `(compute_plan_id, storage_plan_id, valid_from DESC, id DESC)`

#### resource_catalog.refund_policy_versions

实际批准版本化规则，不发明退款比例或自动清除期限；历史无级联。覆盖 F03, F06, F07, F13, F15。

| 字段 | 类型 | 可空 | 默认 | 字段来源 |
|---|---|---|---|---|
| `id` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/RefundPolicyVersion/properties/id` |
| `version_label` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/RefundPolicyVersion/properties/versionLabel` |
| `rules` | `jsonb` | 否 | `—` | `02_database_schema_complete.md#resource_catalog.refund_policy_versions.rules` |
| `customer_terms` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/RefundPolicyVersion/properties/customerTerms` |
| `valid_from` | `timestamptz` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/RefundPolicyVersion/properties/validFrom` |
| `valid_until` | `timestamptz` | NULL | `—` | `03_api_contract_complete.yaml#/components/schemas/RefundPolicyVersion/properties/validUntil` |
| `published_by` | `text` | 否 | `—` | `02_database_schema_complete.md#resource_catalog.refund_policy_versions.published_by` |
| `created_at` | `timestamptz` | 否 | `now()` | `03_api_contract_complete.yaml#/components/schemas/RefundPolicyVersion/properties/createdAt` |
| `algorithm` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/RefundPolicyVersion/properties/algorithm` |
| `retention_policy_version_id` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/RefundPolicyVersion/properties/retentionPolicyVersionId` |

约束：
- `PRIMARY KEY (id)`
- `UNIQUE (version_label)`
- `CHECK (valid_until IS NULL OR valid_until > valid_from)`
- `CHECK (algorithm IN ('workspace-delete-refund-v1'))`
- `FOREIGN KEY (retention_policy_version_id) REFERENCES resource_catalog.retention_policy_versions (id) ON DELETE RESTRICT`

索引：
- `refund_policy_versions_effective`: `(valid_from DESC, id DESC)`

#### resource_catalog.retention_policy_versions

实际批准版本化规则，不发明退款比例或自动清除期限；历史无级联。覆盖 F03, F06, F07, F13, F15。

| 字段 | 类型 | 可空 | 默认 | 字段来源 |
|---|---|---|---|---|
| `id` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/RetentionPolicyVersion/properties/id` |
| `version_label` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/RetentionPolicyVersion/properties/versionLabel` |
| `rules` | `jsonb` | 否 | `—` | `02_database_schema_complete.md#resource_catalog.retention_policy_versions.rules` |
| `customer_terms` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/RetentionPolicyVersion/properties/customerTerms` |
| `valid_from` | `timestamptz` | 否 | `—` | `02_database_schema_complete.md#resource_catalog.retention_policy_versions.valid_from` |
| `valid_until` | `timestamptz` | NULL | `—` | `02_database_schema_complete.md#resource_catalog.retention_policy_versions.valid_until` |
| `published_by` | `text` | 否 | `—` | `02_database_schema_complete.md#resource_catalog.retention_policy_versions.published_by` |
| `created_at` | `timestamptz` | 否 | `now()` | `03_api_contract_complete.yaml#/components/schemas/RetentionPolicyVersion/properties/createdAt` |
| `workspace_data_disposition` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/RetentionPolicyVersion/properties/workspaceDataDisposition` |
| `package_history_disposition` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/RetentionPolicyVersion/properties/packageHistoryDisposition` |
| `build_history_disposition` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/RetentionPolicyVersion/properties/buildHistoryDisposition` |
| `tenant_restore_days` | `integer` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/RetentionPolicyVersion/properties/tenantRestoreDays` |

约束：
- `PRIMARY KEY (id)`
- `UNIQUE (version_label)`
- `CHECK (valid_until IS NULL OR valid_until > valid_from)`
- `CHECK (workspace_data_disposition IN ('destroy_after_confirmed_deletion'))`
- `CHECK (package_history_disposition IN ('retain'))`
- `CHECK (build_history_disposition IN ('retain'))`
- `CHECK (tenant_restore_days = 15)`

索引：
- `retention_policy_versions_effective`: `(valid_from DESC, id DESC)`

#### resource_catalog.quotes

Catalog唯一writer，AcceptQuote本库锁row校验并绑定唯一Workspace Operation；Workspace只存已接受快照。覆盖 F07, F08, F11, F12, F16。

| 字段 | 类型 | 可空 | 默认 | 字段来源 |
|---|---|---|---|---|
| `id` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/Quote/properties/id` |
| `tenant_id` | `text` | 否 | `—` | `02_database_schema_complete.md#resource_catalog.quotes.tenant_id` |
| `actor_id` | `text` | 否 | `—` | `02_database_schema_complete.md#resource_catalog.quotes.actor_id` |
| `purpose` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/Quote/properties/purpose` |
| `workspace_id` | `text` | NULL | `—` | `03_api_contract_complete.yaml#/components/schemas/Quote/properties/workspaceId` |
| `capability_version_id` | `text` | NULL | `—` | `03_api_contract_complete.yaml#/components/schemas/Quote/properties/capabilityVersionId` |
| `compute_plan_id` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/Quote/properties/computePlanId` |
| `storage_plan_id` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/Quote/properties/storagePlanId` |
| `price_policy_version_id` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/Quote/properties/pricePolicyVersionId` |
| `refund_policy_version_id` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/Quote/properties/refundPolicyVersionId` |
| `retention_policy_version_id` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/Quote/properties/retentionPolicyVersionId` |
| `total_usd_micros` | `bigint` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/Quote/properties/totalUSDMicros` |
| `period_start` | `timestamptz` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/Quote/properties/periodStart` |
| `period_end` | `timestamptz` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/Quote/properties/periodEnd` |
| `status` | `text` | 否 | `'offered'` | `03_api_contract_complete.yaml#/components/schemas/Quote/properties/status` |
| `input_digest` | `text` | 否 | `—` | `02_database_schema_complete.md#resource_catalog.quotes.input_digest` |
| `admission_snapshot` | `jsonb` | 否 | `—` | `02_database_schema_complete.md#resource_catalog.quotes.admission_snapshot` |
| `accepted_by_operation_id` | `text` | NULL | `—` | `02_database_schema_complete.md#resource_catalog.quotes.accepted_by_operation_id` |
| `accepted_at` | `timestamptz` | NULL | `—` | `02_database_schema_complete.md#resource_catalog.quotes.accepted_at` |
| `expires_at` | `timestamptz` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/Quote/properties/expiresAt` |
| `created_at` | `timestamptz` | 否 | `now()` | `03_api_contract_complete.yaml#/components/schemas/Quote/properties/createdAt` |
| `model_selections` | `jsonb` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/Quote/properties/modelSelections` |
| `period_months` | `integer` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/Quote/properties/periodMonths` |
| `refund_terms` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/Quote/properties/refundTerms` |
| `retention_terms` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/Quote/properties/retentionTerms` |
| `expected_interruption` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/Quote/properties/expectedInterruption` |
| `plan_change_calculation` | `jsonb` | NULL | `—` | `03_api_contract_complete.yaml#/components/schemas/Quote/properties/planChangeCalculation` |
| `source_subscription_version` | `bigint` | NULL | `—` | `03_api_contract_complete.yaml#/components/schemas/Quote/properties/sourceSubscriptionVersion` |
| `scheduled_plan_change_id` | `text` | NULL | `—` | `03_api_contract_complete.yaml#/components/schemas/Quote/properties/scheduledPlanChangeId` |

约束：
- `PRIMARY KEY (id)`
- `FOREIGN KEY (compute_plan_id) REFERENCES resource_catalog.compute_plans (id) ON DELETE RESTRICT`
- `FOREIGN KEY (storage_plan_id) REFERENCES resource_catalog.storage_plans (id) ON DELETE RESTRICT`
- `FOREIGN KEY (price_policy_version_id) REFERENCES resource_catalog.price_policy_versions (id) ON DELETE RESTRICT`
- `FOREIGN KEY (refund_policy_version_id) REFERENCES resource_catalog.refund_policy_versions (id) ON DELETE RESTRICT`
- `FOREIGN KEY (retention_policy_version_id) REFERENCES resource_catalog.retention_policy_versions (id) ON DELETE RESTRICT`
- `CHECK (purpose IN ('deploy','resize','renew'))`
- `CHECK (status IN ('offered','accepted','expired'))`
- `CHECK (total_usd_micros >= 0)`
- `CHECK (period_end > period_start)`
- `CHECK (input_digest ~ '^sha256:[0-9a-f]{64}$')`
- `CHECK ((status = 'accepted') = (accepted_at IS NOT NULL AND accepted_by_operation_id IS NOT NULL))`
- `CHECK (period_months > 0)`
- `CHECK ((purpose = 'resize') = (plan_change_calculation IS NOT NULL))`
- `CHECK (purpose <> 'resize' OR source_subscription_version IS NOT NULL)`

索引：
- `quotes_tenant_list`: `(tenant_id, created_at DESC, id DESC)`
- `quotes_accepting_operation`: UNIQUE `(accepted_by_operation_id)` WHERE `accepted_by_operation_id IS NOT NULL`

#### resource_catalog.quote_items

All line amounts are nonnegative; total=sum(compute/storage/product)-sum(adjustment_credit); credit_source binds the exact paid period, transaction and policy。覆盖 F07, F11, F12。

| 字段 | 类型 | 可空 | 默认 | 字段来源 |
|---|---|---|---|---|
| `id` | `text` | 否 | `—` | `02_database_schema_complete.md#resource_catalog.quote_items.id` |
| `quote_id` | `text` | 否 | `—` | `02_database_schema_complete.md#resource_catalog.quote_items.quote_id` |
| `kind` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/QuoteLine/properties/kind` |
| `description` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/QuoteLine/properties/description` |
| `amount_usd_micros` | `bigint` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/QuoteLine/properties/amountUSDMicros` |
| `calculation` | `jsonb` | 否 | `—` | `02_database_schema_complete.md#resource_catalog.quote_items.calculation` |
| `sort_order` | `integer` | 否 | `—` | `02_database_schema_complete.md#resource_catalog.quote_items.sort_order` |
| `created_at` | `timestamptz` | 否 | `now()` | `02_database_schema_complete.md#resource_catalog.quote_items.created_at` |
| `quantity` | `integer` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/QuoteLine/properties/quantity` |
| `credit_source` | `jsonb` | NULL | `—` | `03_api_contract_complete.yaml#/components/schemas/QuoteLine/properties/creditSource` |

约束：
- `PRIMARY KEY (id)`
- `FOREIGN KEY (quote_id) REFERENCES resource_catalog.quotes (id) ON DELETE RESTRICT`
- `UNIQUE (quote_id, sort_order)`
- `CHECK (sort_order >= 0)`
- `CHECK (quantity > 0)`
- `CHECK (kind IN ('compute','storage','product','adjustment_credit'))`
- `CHECK (amount_usd_micros >= 0)`
- `CHECK ((kind = 'adjustment_credit') = (credit_source IS NOT NULL))`
- `CHECK (credit_source IS NULL OR ((jsonb_typeof(credit_source) = 'object' AND credit_source ?& ARRAY['originalWalletOperationId','originalSubscriptionPeriodId','policyVersionId','creditReceiptId','amountUSDMicros'] AND (credit_source->>'amountUSDMicros')::bigint = amount_usd_micros) IS TRUE))`

索引：
- `quote_items_quote`: `(quote_id, sort_order)`

#### resource_catalog.outbox_events

本Owner状态事务同写immutable事件；revision只在本Owner聚合锁内分配。覆盖 F01, F04, F05, F08, F10, F11, F12, F13, F15, F16, F17。

| 字段 | 类型 | 可空 | 默认 | 字段来源 |
|---|---|---|---|---|
| `id` | `text` | 否 | `—` | `02_database_schema_complete.md#resource_catalog.outbox_events.id` |
| `event_type` | `text` | 否 | `—` | `02_database_schema_complete.md#resource_catalog.outbox_events.event_type` |
| `schema_version` | `integer` | 否 | `—` | `02_database_schema_complete.md#resource_catalog.outbox_events.schema_version` |
| `aggregate_type` | `text` | 否 | `—` | `02_database_schema_complete.md#resource_catalog.outbox_events.aggregate_type` |
| `aggregate_id` | `text` | 否 | `—` | `02_database_schema_complete.md#resource_catalog.outbox_events.aggregate_id` |
| `aggregate_revision` | `bigint` | 否 | `—` | `02_database_schema_complete.md#resource_catalog.outbox_events.aggregate_revision` |
| `tenant_id` | `text` | NULL | `—` | `02_database_schema_complete.md#resource_catalog.outbox_events.tenant_id` |
| `correlation_id` | `text` | 否 | `—` | `02_database_schema_complete.md#resource_catalog.outbox_events.correlation_id` |
| `causation_id` | `text` | NULL | `—` | `02_database_schema_complete.md#resource_catalog.outbox_events.causation_id` |
| `payload` | `jsonb` | 否 | `—` | `02_database_schema_complete.md#resource_catalog.outbox_events.payload` |
| `payload_sha256` | `text` | 否 | `—` | `02_database_schema_complete.md#resource_catalog.outbox_events.payload_sha256` |
| `occurred_at` | `timestamptz` | 否 | `—` | `02_database_schema_complete.md#resource_catalog.outbox_events.occurred_at` |
| `created_at` | `timestamptz` | 否 | `now()` | `02_database_schema_complete.md#resource_catalog.outbox_events.created_at` |

约束：
- `PRIMARY KEY (id)`
- `CHECK (schema_version > 0 AND aggregate_revision >= 0)`
- `CHECK (payload_sha256 ~ '^[0-9a-f]{64}$')`
- `UNIQUE (aggregate_type, aggregate_id, aggregate_revision, event_type)`

索引：
- `outbox_events_aggregate`: `(aggregate_type, aggregate_id, aggregate_revision)`

#### resource_catalog.outbox_deliveries

各consumer独立ACK，禁止单个ACK丢其他消费者事件；lease仅本库。覆盖 F05, F08, F10, F11, F12, F13, F15, F17。

| 字段 | 类型 | 可空 | 默认 | 字段来源 |
|---|---|---|---|---|
| `id` | `text` | 否 | `—` | `02_database_schema_complete.md#resource_catalog.outbox_deliveries.id` |
| `event_id` | `text` | 否 | `—` | `02_database_schema_complete.md#resource_catalog.outbox_deliveries.event_id` |
| `consumer_owner` | `text` | 否 | `—` | `02_database_schema_complete.md#resource_catalog.outbox_deliveries.consumer_owner` |
| `attempt_count` | `integer` | 否 | `0` | `02_database_schema_complete.md#resource_catalog.outbox_deliveries.attempt_count` |
| `next_attempt_at` | `timestamptz` | 否 | `now()` | `02_database_schema_complete.md#resource_catalog.outbox_deliveries.next_attempt_at` |
| `acknowledged_at` | `timestamptz` | NULL | `—` | `02_database_schema_complete.md#resource_catalog.outbox_deliveries.acknowledged_at` |
| `last_error_code` | `text` | NULL | `—` | `02_database_schema_complete.md#resource_catalog.outbox_deliveries.last_error_code` |
| `lease_token` | `text` | NULL | `—` | `02_database_schema_complete.md#resource_catalog.outbox_deliveries.lease_token` |
| `lease_until` | `timestamptz` | NULL | `—` | `02_database_schema_complete.md#resource_catalog.outbox_deliveries.lease_until` |
| `created_at` | `timestamptz` | 否 | `now()` | `02_database_schema_complete.md#resource_catalog.outbox_deliveries.created_at` |
| `updated_at` | `timestamptz` | 否 | `now()` | `02_database_schema_complete.md#resource_catalog.outbox_deliveries.updated_at` |

约束：
- `PRIMARY KEY (id)`
- `FOREIGN KEY (event_id) REFERENCES resource_catalog.outbox_events (id) ON DELETE RESTRICT`
- `UNIQUE (event_id, consumer_owner)`
- `CHECK (attempt_count >= 0)`
- `CHECK ((lease_token IS NULL) = (lease_until IS NULL))`

索引：
- `outbox_deliveries_pending`: `(next_attempt_at, id)` WHERE `acknowledged_at IS NULL`

#### resource_catalog.inbox_events

去重hash/业务状态/processed_at/后续Outbox同本库事务；乱序不能覆盖新版本。覆盖 F05, F08, F10, F11, F12, F13, F15, F17。

| 字段 | 类型 | 可空 | 默认 | 字段来源 |
|---|---|---|---|---|
| `id` | `text` | 否 | `—` | `02_database_schema_complete.md#resource_catalog.inbox_events.id` |
| `source_owner` | `text` | 否 | `—` | `02_database_schema_complete.md#resource_catalog.inbox_events.source_owner` |
| `source_event_id` | `text` | 否 | `—` | `02_database_schema_complete.md#resource_catalog.inbox_events.source_event_id` |
| `event_type` | `text` | 否 | `—` | `02_database_schema_complete.md#resource_catalog.inbox_events.event_type` |
| `schema_version` | `integer` | 否 | `—` | `02_database_schema_complete.md#resource_catalog.inbox_events.schema_version` |
| `aggregate_type` | `text` | 否 | `—` | `02_database_schema_complete.md#resource_catalog.inbox_events.aggregate_type` |
| `aggregate_id` | `text` | 否 | `—` | `02_database_schema_complete.md#resource_catalog.inbox_events.aggregate_id` |
| `aggregate_revision` | `bigint` | 否 | `—` | `02_database_schema_complete.md#resource_catalog.inbox_events.aggregate_revision` |
| `payload_sha256` | `text` | 否 | `—` | `02_database_schema_complete.md#resource_catalog.inbox_events.payload_sha256` |
| `payload` | `jsonb` | 否 | `—` | `02_database_schema_complete.md#resource_catalog.inbox_events.payload` |
| `received_at` | `timestamptz` | 否 | `now()` | `02_database_schema_complete.md#resource_catalog.inbox_events.received_at` |
| `processed_at` | `timestamptz` | NULL | `—` | `02_database_schema_complete.md#resource_catalog.inbox_events.processed_at` |
| `result_resource_id` | `text` | NULL | `—` | `02_database_schema_complete.md#resource_catalog.inbox_events.result_resource_id` |
| `error_code` | `text` | NULL | `—` | `02_database_schema_complete.md#resource_catalog.inbox_events.error_code` |

约束：
- `PRIMARY KEY (id)`
- `UNIQUE (source_owner, source_event_id)`
- `CHECK (schema_version > 0 AND aggregate_revision >= 0)`
- `CHECK (payload_sha256 ~ '^[0-9a-f]{64}$')`

索引：
- `inbox_events_pending`: `(received_at, id)` WHERE `processed_at IS NULL`
- `inbox_events_aggregate`: `(source_owner, aggregate_type, aggregate_id, aggregate_revision)`

#### resource_catalog.idempotency_records

命令创建同事务；operation_name是API操作名不是新Operation ID；安全响应无Key明文；义务存续无TTL清理。覆盖 F01, F03, F04, F05, F08, F10, F11, F12, F13, F14, F15, F16, F17。

| 字段 | 类型 | 可空 | 默认 | 字段来源 |
|---|---|---|---|---|
| `id` | `text` | 否 | `—` | `02_database_schema_complete.md#resource_catalog.idempotency_records.id` |
| `tenant_scope` | `text` | 否 | `—` | `02_database_schema_complete.md#resource_catalog.idempotency_records.tenant_scope` |
| `actor_scope` | `text` | 否 | `—` | `02_database_schema_complete.md#resource_catalog.idempotency_records.actor_scope` |
| `operation_name` | `text` | 否 | `—` | `02_database_schema_complete.md#resource_catalog.idempotency_records.operation_name` |
| `idempotency_key` | `text` | 否 | `—` | `02_database_schema_complete.md#resource_catalog.idempotency_records.idempotency_key` |
| `request_sha256` | `text` | 否 | `—` | `02_database_schema_complete.md#resource_catalog.idempotency_records.request_sha256` |
| `resource_id` | `text` | 否 | `—` | `02_database_schema_complete.md#resource_catalog.idempotency_records.resource_id` |
| `operation_id` | `text` | NULL | `—` | `02_database_schema_complete.md#resource_catalog.idempotency_records.operation_id` |
| `response_status` | `integer` | 否 | `—` | `02_database_schema_complete.md#resource_catalog.idempotency_records.response_status` |
| `response_body` | `jsonb` | 否 | `—` | `02_database_schema_complete.md#resource_catalog.idempotency_records.response_body` |
| `created_at` | `timestamptz` | 否 | `now()` | `02_database_schema_complete.md#resource_catalog.idempotency_records.created_at` |

约束：
- `PRIMARY KEY (id)`
- `UNIQUE (tenant_scope, actor_scope, operation_name, idempotency_key)`
- `CHECK (request_sha256 ~ '^[0-9a-f]{64}$')`
- `CHECK (response_status BETWEEN 100 AND 599)`

索引：
- `idempotency_records_resource`: `(resource_id)`

#### resource_catalog.operations

目标Owner持异步Operation；BFF路由无中央writer；Build创建201回Job；只有Workspace配Saga。覆盖 F01, F03, F04, F06, F08, F09, F10, F11, F12, F13, F14, F15, F17。

| 字段 | 类型 | 可空 | 默认 | 字段来源 |
|---|---|---|---|---|
| `id` | `text` | 否 | `—` | `02_database_schema_complete.md#resource_catalog.operations.id` |
| `tenant_id` | `text` | NULL | `—` | `02_database_schema_complete.md#resource_catalog.operations.tenant_id` |
| `actor_id` | `text` | 否 | `—` | `02_database_schema_complete.md#resource_catalog.operations.actor_id` |
| `kind` | `text` | 否 | `—` | `02_database_schema_complete.md#resource_catalog.operations.kind` |
| `resource_id` | `text` | 否 | `—` | `02_database_schema_complete.md#resource_catalog.operations.resource_id` |
| `status` | `text` | 否 | `'accepted'` | `02_database_schema_complete.md#resource_catalog.operations.status` |
| `stage` | `text` | 否 | `—` | `02_database_schema_complete.md#resource_catalog.operations.stage` |
| `error_code` | `text` | NULL | `—` | `02_database_schema_complete.md#resource_catalog.operations.error_code` |
| `observation_result` | `text` | NULL | `—` | `02_database_schema_complete.md#resource_catalog.operations.observation_result` |
| `request_id` | `text` | 否 | `—` | `02_database_schema_complete.md#resource_catalog.operations.request_id` |
| `accepted_input` | `jsonb` | 否 | `—` | `02_database_schema_complete.md#resource_catalog.operations.accepted_input` |
| `result` | `jsonb` | NULL | `—` | `02_database_schema_complete.md#resource_catalog.operations.result` |
| `worker_lease_token` | `text` | NULL | `—` | `02_database_schema_complete.md#resource_catalog.operations.worker_lease_token` |
| `worker_lease_until` | `timestamptz` | NULL | `—` | `02_database_schema_complete.md#resource_catalog.operations.worker_lease_until` |
| `started_at` | `timestamptz` | NULL | `—` | `02_database_schema_complete.md#resource_catalog.operations.started_at` |
| `completed_at` | `timestamptz` | NULL | `—` | `02_database_schema_complete.md#resource_catalog.operations.completed_at` |
| `created_at` | `timestamptz` | 否 | `now()` | `02_database_schema_complete.md#resource_catalog.operations.created_at` |
| `updated_at` | `timestamptz` | 否 | `now()` | `02_database_schema_complete.md#resource_catalog.operations.updated_at` |

约束：
- `PRIMARY KEY (id)`
- `CHECK (status IN ('accepted','running','awaiting_confirmation','succeeded','failed','needs_attention','cancelled'))`
- `CHECK (observation_result IN ('confirmed','rejected','unknown'))`
- `CHECK ((worker_lease_token IS NULL) = (worker_lease_until IS NULL))`
- `CHECK (status <> 'succeeded' OR completed_at IS NOT NULL)`

索引：
- `operations_resource`: `(resource_id, created_at DESC, id DESC)`
- `operations_tenant_list`: `(tenant_id, created_at DESC, id DESC)`
- `operations_recovery`: `(status, updated_at)`

### ledger

Database `opl_ledger` · Schema `ledger` · Writer `opl_ledger_writer`。

#### ledger.receipts

append-only证据/hash/provenance；修正append新receipt，不覆写原事实或第二钱包。覆盖 F05, F08, F10, F11, F12, F13, F14, F15, F16, F17。

| 字段 | 类型 | 可空 | 默认 | 字段来源 |
|---|---|---|---|---|
| `id` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/Receipt/properties/id` |
| `tenant_id` | `text` | NULL | `—` | `02_database_schema_complete.md#ledger.receipts.tenant_id` |
| `source_owner` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/Receipt/properties/owner` |
| `source_event_id` | `text` | 否 | `—` | `02_database_schema_complete.md#ledger.receipts.source_event_id` |
| `kind` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/Receipt/properties/kind` |
| `subject_type` | `text` | 否 | `—` | `02_database_schema_complete.md#ledger.receipts.subject_type` |
| `subject_id` | `text` | 否 | `—` | `02_database_schema_complete.md#ledger.receipts.subject_id` |
| `source_operation_id` | `text` | 否 | `—` | `03_api_contract_complete.yaml#/components/schemas/Receipt/properties/operationId` |
| `request_id` | `text` | 否 | `—` | `02_database_schema_complete.md#ledger.receipts.request_id` |
| `evidence_sha256` | `text` | 否 | `—` | `02_database_schema_complete.md#ledger.receipts.evidence_sha256` |
| `evidence` | `jsonb` | 否 | `—` | `02_database_schema_complete.md#ledger.receipts.evidence` |
| `previous_receipt_id` | `text` | NULL | `—` | `02_database_schema_complete.md#ledger.receipts.previous_receipt_id` |
| `source_occurred_at` | `timestamptz` | 否 | `—` | `02_database_schema_complete.md#ledger.receipts.source_occurred_at` |
| `created_at` | `timestamptz` | 否 | `now()` | `03_api_contract_complete.yaml#/components/schemas/Receipt/properties/createdAt` |

约束：
- `PRIMARY KEY (id)`
- `FOREIGN KEY (previous_receipt_id) REFERENCES ledger.receipts (id) ON DELETE RESTRICT`
- `UNIQUE (source_owner, source_event_id)`
- `CHECK (evidence_sha256 ~ '^[0-9a-f]{64}$')`

索引：
- `receipts_subject`: `(subject_type, subject_id, created_at DESC, id DESC)`
- `receipts_tenant`: `(tenant_id, created_at DESC, id DESC)`
- `receipts_operation`: `(source_owner, source_operation_id)`

#### ledger.reconciliations

对账结论只记录，不直接更改Workspace/钱包/provider。覆盖 F08, F12, F13, F16, F17。

| 字段 | 类型 | 可空 | 默认 | 字段来源 |
|---|---|---|---|---|
| `id` | `text` | 否 | `—` | `02_database_schema_complete.md#ledger.reconciliations.id` |
| `subject_type` | `text` | 否 | `—` | `02_database_schema_complete.md#ledger.reconciliations.subject_type` |
| `subject_id` | `text` | 否 | `—` | `02_database_schema_complete.md#ledger.reconciliations.subject_id` |
| `source_owner` | `text` | 否 | `—` | `02_database_schema_complete.md#ledger.reconciliations.source_owner` |
| `owner_readback_ref` | `text` | 否 | `—` | `02_database_schema_complete.md#ledger.reconciliations.owner_readback_ref` |
| `receipt_id` | `text` | 否 | `—` | `02_database_schema_complete.md#ledger.reconciliations.receipt_id` |
| `result` | `text` | 否 | `—` | `02_database_schema_complete.md#ledger.reconciliations.result` |
| `safe_difference` | `jsonb` | 否 | `—` | `02_database_schema_complete.md#ledger.reconciliations.safe_difference` |
| `observed_at` | `timestamptz` | 否 | `—` | `02_database_schema_complete.md#ledger.reconciliations.observed_at` |
| `created_at` | `timestamptz` | 否 | `now()` | `02_database_schema_complete.md#ledger.reconciliations.created_at` |

约束：
- `PRIMARY KEY (id)`
- `FOREIGN KEY (receipt_id) REFERENCES ledger.receipts (id) ON DELETE RESTRICT`
- `CHECK (result IN ('matched','mismatch','unknown'))`

索引：
- `reconciliations_subject`: `(subject_type, subject_id, created_at DESC, id DESC)`

#### ledger.outbox_events

本Owner状态事务同写immutable事件；revision只在本Owner聚合锁内分配。覆盖 F01, F04, F05, F08, F10, F11, F12, F13, F15, F16, F17。

| 字段 | 类型 | 可空 | 默认 | 字段来源 |
|---|---|---|---|---|
| `id` | `text` | 否 | `—` | `02_database_schema_complete.md#ledger.outbox_events.id` |
| `event_type` | `text` | 否 | `—` | `02_database_schema_complete.md#ledger.outbox_events.event_type` |
| `schema_version` | `integer` | 否 | `—` | `02_database_schema_complete.md#ledger.outbox_events.schema_version` |
| `aggregate_type` | `text` | 否 | `—` | `02_database_schema_complete.md#ledger.outbox_events.aggregate_type` |
| `aggregate_id` | `text` | 否 | `—` | `02_database_schema_complete.md#ledger.outbox_events.aggregate_id` |
| `aggregate_revision` | `bigint` | 否 | `—` | `02_database_schema_complete.md#ledger.outbox_events.aggregate_revision` |
| `tenant_id` | `text` | NULL | `—` | `02_database_schema_complete.md#ledger.outbox_events.tenant_id` |
| `correlation_id` | `text` | 否 | `—` | `02_database_schema_complete.md#ledger.outbox_events.correlation_id` |
| `causation_id` | `text` | NULL | `—` | `02_database_schema_complete.md#ledger.outbox_events.causation_id` |
| `payload` | `jsonb` | 否 | `—` | `02_database_schema_complete.md#ledger.outbox_events.payload` |
| `payload_sha256` | `text` | 否 | `—` | `02_database_schema_complete.md#ledger.outbox_events.payload_sha256` |
| `occurred_at` | `timestamptz` | 否 | `—` | `02_database_schema_complete.md#ledger.outbox_events.occurred_at` |
| `created_at` | `timestamptz` | 否 | `now()` | `02_database_schema_complete.md#ledger.outbox_events.created_at` |

约束：
- `PRIMARY KEY (id)`
- `CHECK (schema_version > 0 AND aggregate_revision >= 0)`
- `CHECK (payload_sha256 ~ '^[0-9a-f]{64}$')`
- `UNIQUE (aggregate_type, aggregate_id, aggregate_revision, event_type)`

索引：
- `outbox_events_aggregate`: `(aggregate_type, aggregate_id, aggregate_revision)`

#### ledger.outbox_deliveries

各consumer独立ACK，禁止单个ACK丢其他消费者事件；lease仅本库。覆盖 F05, F08, F10, F11, F12, F13, F15, F17。

| 字段 | 类型 | 可空 | 默认 | 字段来源 |
|---|---|---|---|---|
| `id` | `text` | 否 | `—` | `02_database_schema_complete.md#ledger.outbox_deliveries.id` |
| `event_id` | `text` | 否 | `—` | `02_database_schema_complete.md#ledger.outbox_deliveries.event_id` |
| `consumer_owner` | `text` | 否 | `—` | `02_database_schema_complete.md#ledger.outbox_deliveries.consumer_owner` |
| `attempt_count` | `integer` | 否 | `0` | `02_database_schema_complete.md#ledger.outbox_deliveries.attempt_count` |
| `next_attempt_at` | `timestamptz` | 否 | `now()` | `02_database_schema_complete.md#ledger.outbox_deliveries.next_attempt_at` |
| `acknowledged_at` | `timestamptz` | NULL | `—` | `02_database_schema_complete.md#ledger.outbox_deliveries.acknowledged_at` |
| `last_error_code` | `text` | NULL | `—` | `02_database_schema_complete.md#ledger.outbox_deliveries.last_error_code` |
| `lease_token` | `text` | NULL | `—` | `02_database_schema_complete.md#ledger.outbox_deliveries.lease_token` |
| `lease_until` | `timestamptz` | NULL | `—` | `02_database_schema_complete.md#ledger.outbox_deliveries.lease_until` |
| `created_at` | `timestamptz` | 否 | `now()` | `02_database_schema_complete.md#ledger.outbox_deliveries.created_at` |
| `updated_at` | `timestamptz` | 否 | `now()` | `02_database_schema_complete.md#ledger.outbox_deliveries.updated_at` |

约束：
- `PRIMARY KEY (id)`
- `FOREIGN KEY (event_id) REFERENCES ledger.outbox_events (id) ON DELETE RESTRICT`
- `UNIQUE (event_id, consumer_owner)`
- `CHECK (attempt_count >= 0)`
- `CHECK ((lease_token IS NULL) = (lease_until IS NULL))`

索引：
- `outbox_deliveries_pending`: `(next_attempt_at, id)` WHERE `acknowledged_at IS NULL`

#### ledger.inbox_events

去重hash/业务状态/processed_at/后续Outbox同本库事务；乱序不能覆盖新版本。覆盖 F05, F08, F10, F11, F12, F13, F15, F17。

| 字段 | 类型 | 可空 | 默认 | 字段来源 |
|---|---|---|---|---|
| `id` | `text` | 否 | `—` | `02_database_schema_complete.md#ledger.inbox_events.id` |
| `source_owner` | `text` | 否 | `—` | `02_database_schema_complete.md#ledger.inbox_events.source_owner` |
| `source_event_id` | `text` | 否 | `—` | `02_database_schema_complete.md#ledger.inbox_events.source_event_id` |
| `event_type` | `text` | 否 | `—` | `02_database_schema_complete.md#ledger.inbox_events.event_type` |
| `schema_version` | `integer` | 否 | `—` | `02_database_schema_complete.md#ledger.inbox_events.schema_version` |
| `aggregate_type` | `text` | 否 | `—` | `02_database_schema_complete.md#ledger.inbox_events.aggregate_type` |
| `aggregate_id` | `text` | 否 | `—` | `02_database_schema_complete.md#ledger.inbox_events.aggregate_id` |
| `aggregate_revision` | `bigint` | 否 | `—` | `02_database_schema_complete.md#ledger.inbox_events.aggregate_revision` |
| `payload_sha256` | `text` | 否 | `—` | `02_database_schema_complete.md#ledger.inbox_events.payload_sha256` |
| `payload` | `jsonb` | 否 | `—` | `02_database_schema_complete.md#ledger.inbox_events.payload` |
| `received_at` | `timestamptz` | 否 | `now()` | `02_database_schema_complete.md#ledger.inbox_events.received_at` |
| `processed_at` | `timestamptz` | NULL | `—` | `02_database_schema_complete.md#ledger.inbox_events.processed_at` |
| `result_resource_id` | `text` | NULL | `—` | `02_database_schema_complete.md#ledger.inbox_events.result_resource_id` |
| `error_code` | `text` | NULL | `—` | `02_database_schema_complete.md#ledger.inbox_events.error_code` |

约束：
- `PRIMARY KEY (id)`
- `UNIQUE (source_owner, source_event_id)`
- `CHECK (schema_version > 0 AND aggregate_revision >= 0)`
- `CHECK (payload_sha256 ~ '^[0-9a-f]{64}$')`

索引：
- `inbox_events_pending`: `(received_at, id)` WHERE `processed_at IS NULL`
- `inbox_events_aggregate`: `(source_owner, aggregate_type, aggregate_id, aggregate_revision)`

#### ledger.idempotency_records

命令创建同事务；operation_name是API操作名不是新Operation ID；安全响应无Key明文；义务存续无TTL清理。覆盖 F01, F03, F04, F05, F08, F10, F11, F12, F13, F14, F15, F16, F17。

| 字段 | 类型 | 可空 | 默认 | 字段来源 |
|---|---|---|---|---|
| `id` | `text` | 否 | `—` | `02_database_schema_complete.md#ledger.idempotency_records.id` |
| `tenant_scope` | `text` | 否 | `—` | `02_database_schema_complete.md#ledger.idempotency_records.tenant_scope` |
| `actor_scope` | `text` | 否 | `—` | `02_database_schema_complete.md#ledger.idempotency_records.actor_scope` |
| `operation_name` | `text` | 否 | `—` | `02_database_schema_complete.md#ledger.idempotency_records.operation_name` |
| `idempotency_key` | `text` | 否 | `—` | `02_database_schema_complete.md#ledger.idempotency_records.idempotency_key` |
| `request_sha256` | `text` | 否 | `—` | `02_database_schema_complete.md#ledger.idempotency_records.request_sha256` |
| `resource_id` | `text` | 否 | `—` | `02_database_schema_complete.md#ledger.idempotency_records.resource_id` |
| `operation_id` | `text` | NULL | `—` | `02_database_schema_complete.md#ledger.idempotency_records.operation_id` |
| `response_status` | `integer` | 否 | `—` | `02_database_schema_complete.md#ledger.idempotency_records.response_status` |
| `response_body` | `jsonb` | 否 | `—` | `02_database_schema_complete.md#ledger.idempotency_records.response_body` |
| `created_at` | `timestamptz` | 否 | `now()` | `02_database_schema_complete.md#ledger.idempotency_records.created_at` |

约束：
- `PRIMARY KEY (id)`
- `UNIQUE (tenant_scope, actor_scope, operation_name, idempotency_key)`
- `CHECK (request_sha256 ~ '^[0-9a-f]{64}$')`
- `CHECK (response_status BETWEEN 100 AND 599)`

索引：
- `idempotency_records_resource`: `(resource_id)`
