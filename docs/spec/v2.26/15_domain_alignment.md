# 15 领域对齐：继承实现、API/数据库字段与跨域契约

> 本文是架构/实施对齐与导航，不是第二份字段规格。字段仍由02/SQL、03、proto/events定义，业务顺序由06/13定义，执行工作包由14定义。发现不一致时在原Owner修正；第6节区分已修的规格与尚待接入的真实消费者，不把契约字段存在当业务完成。
>
> 核对基线：上游`one-person-lab-cloud`本地`main`=`50520e27a6b9a630eefdc2df7da3e5ec498d28a0`；开发fork `opl-cloud`=`7f5d05fe9b855d8af216caad714cf7fc85014d3c`。未访问生产数据库、未拉取远端新提交，不把本地快照称为上游最新线上事实。

## 1. 一条产品演进线，不是两个产品

```text
one-person-lab-cloud / 50520e27     上游、继承代码与历史义务
                │ fork
                ▼
opl-cloud / v2.26                  当前开发与验证位置
                │ 用户验收后，增量Issue/PR合回（本次未执行）
                ▼
one-person-lab-cloud              最终承载产品代码
```

本次`git merge-base`确认共同基线就是50520e27；fork至7f5d05fe对继承的Control Plane、Fabric、Ledger业务源码无diff，仅Control Plane/Ledger依赖清单变化。**不能把新建目录当作真实旧能力已迁出。**

必须分开：

- **代码集成**：fork开发、审查、测试，再合回上游；不是把仓库当部署单位。
- **能力移交**：旧handler/application service/数据writer逐能力切给新Owner，切换真实caller后退休旧写路径。
- **客户数据迁移**：09的M0–M5、W25/W30；原ID、订单、Key、Receipt、资源和未决义务不可丢。空库建表不能代替它。
- **生产采用**：Instance部署、资格和回执；不由Cloud本机或普通CI代行。

## 2. 先看继承基线：当前到底有哪些API与DB字段

采用真实构造器可达注册函数的Go AST抽取路由（包括常量/固定循环展开），不是扫字符串。数据库是在一次性PostgreSQL 16中调用三个真实迁移加载链后读取`information_schema`/`pg_constraint`/`pg_indexes`，不是累加所有历史SQL。

| 继承模块 | 挂载HTTP patterns | 空库实际表 | 实际列 | 当前角色与详细字段 |
|---|---:|---:|---:|---|
| Control Plane | 97 | 22 | 290 | 会话/账户、Workspace购买生命周期、应用选择、价格、Gateway代理、管理员/公告等；[全部API handler与DB列](reference/legacy-control-plane.md) |
| Fabric | 63 | 5 | 58 | provider资源、运行/Secret/挂载/路由及操作事实；[全部API handler与DB列](reference/legacy-fabric.md) |
| Ledger | 11 | 6 | 64 | Receipt/索引/对账/幂等；[全部API handler与DB列](reference/legacy-ledger.md) |

计数口径：171个挂载pattern包括健康、静态/应用代理及显式404入口，不等于171个业务API；33张物理表/412列包括每库各一张`opl_schema_migrations`，去除journal为30张/403列。代理通配入口不枚举其后应用自行提供的端点。

### 2.1 当前表的责任不能按名字误搬

- Control Plane：账户、用户、会话、工作空间、compute/storage/attachment、runtime_operations、application revisions/data materials、billing reconciliation、audit、announcement、sync、verification记录。
- `control_plane_organizations`、`control_plane_memberships`仍出现在空库加载结果，不可因为未出现在当前Ent实体里就宣布已不存在。客户历史/旧版本下条件迁移结果须另作M0/M2盘点。
- Fabric：`fabric_operations`、`machine_ownerships`及保留的content-transfer表；不能把目标`fabric.resource_sets`已写进规格当成当前loader已经使用它。
- Ledger：`evidence_receipts`、`idempotency_keys`、`reconciliation_reports`、`evidence_index_entries`、`review_policies`；旧证据不可按新表形状重写。
- JSON/text字段（如`billing_state_json`、`application_binding`、`result`、receipt payload）必须用当前typed decoder解释。列名字相似不是迁移映射证据。

[旧JSON类型字段目录](reference/legacy-wire-types.md)保留Go类型、嵌入字段和JSON tag。旧API大量动态map投影没有独立完整schema，因此参考页保留真实handler和委托来源，不伪造“已完整生成的OpenAPI”。这是需要在活能力提取时收敛的技术债，不是让v2.26新增兼容字段的理由。

### 2.2 真实加载链

| 模块 | 真正执行路径 | 本次验证的范围 |
|---|---|---|
| Control Plane | `internal/server/ent_state_store.go` → `controlPlaneMigrations` → 显式SQL Apply + Ent Schema.Create +后续迁移 | 精确源码的空库结果，包含Ent以外的SQL列/约束 |
| Fabric | `internal/fabric/operation_store.go` → `fabricEmbeddedMigrations` → 内嵌`ent_migrations/*.sql` | 加载内部目录，不把外层展示SQL当writer |
| Ledger | `internal/ledger/postgres_store.go` → `ledgerEmbeddedMigrations` → 内嵌`ent_migrations/*.sql` | 同上；索引/历史custody也计入 |

回执：[真实空库schema读回](checks/runs/legacy-schema-audit-20260923.json)。这不是生产表清点，也未演练有数据的版本升级。

## 3. 目标九个Owner：每个都有独立的字段与交互页

下列REST数量来自03的`x-owner`，是**规格入口归类**；三条通用Operation归BFF入口，实际数据Owner由必填owner参数决定。不是统计已上线端点。所有客户REST都由BFF暴露，领域后端消费typed RPC。Runtime Control/Fabric没有直接客户REST不代表没有API。

| Owner | 核心独占事实 | REST归类 | 目标表 / 列 | 字段、后端与交互 |
|---|---|---:|---:|---|
| tenant / CloudIdentity | 成员权限、Tenant生命周期、授权上下文/grant | 23 | 12 / 148 | [完整说明](reference/domain-tenant.md) |
| capability | Package/版本/目录、可部署版本、引用claims | 30 | 16 / 204 | [完整说明](reference/domain-capability.md) |
| build | BuildJob、固定输入、构建产物/日志事实 | 5 | 8 / 109 | [完整说明](reference/domain-build.md) |
| workspace | 客户权益、订阅/周期义务、业务Deployment选择、Saga | 21 | 14 / 258 | [完整说明](reference/domain-workspace.md) |
| runtime_control | 实例期望、应用执行/配置/健康、执行协调 | 0 | 7 / 98 | [完整说明](reference/domain-runtime_control.md) |
| fabric | provider执行与资源/路由实际读回 | 0 | 12 / 170 | [完整说明](reference/domain-fabric.md) |
| gateway | 身份/账单主体/Key映射、钱包命令及观察 | 9 | 9 / 122 | [完整说明](reference/domain-gateway.md) |
| resource_catalog | 套餐、平台政策版本、不可变报价/接受 | 14 | 12 / 177 | [完整说明](reference/domain-resource_catalog.md) |
| ledger | 不可变收据、证据索引、对账 | 3 | 6 / 73 | [完整说明](reference/domain-ledger.md) |
| BFF（入口，非数据Owner） | 会话/授权上下文与通用Operation路由；不持有Operation表 | 3 | 0 / 0 | [Operation路由](reference/rest-schemas.md#bff通用operation路由派生参考) |

合计：108 REST、九个数据Owner的96表/1359列；BFF不是第十个数据Owner。另有172内部RPC、391消息、18事件、89条业务流步骤；RPC/proto service数量**不是进程数**。

每域页面依次列：职责与禁止项 → 继承/提取/新增 → 本域事务与失败逻辑 → 每个REST路径/参数/请求响应字段 → 所有数据库列/约束/索引 → 调用/被调用RPC及字段、接收方写入、完成证据与unknown处理 → 生产/消费事件及payload字段。

- [全部REST DTO字段及DB血缘](reference/rest-schemas.md)：包含必填、类型、组合schema和derived映射；不按扁平字段猜`oneOf`。
- [全部RPC签名、消息字段号与枚举](reference/rpc-messages.md)：包括尚未在89条业务流中具名出现的协议方法；有声明不表示有真实caller。
- 共用传输协议与业务模型分离。不得把上面目录变成“全域共享ORM”或统一领域基类。

## 4. DDD解耦应该落在哪一层

### 4.1 不按目录或数据表机械划聚合

Bounded Context由业务语言、权限、规则和写入权决定；一个表不必是一个聚合，一整个服务也不必是一个大聚合。已有九Owner作为本期上下文边界保留，不因这次审计增加微服务、中央Operation、全局EventBus或第二钱包。

每项能力的调用依赖应为：

```text
HTTP/gRPC adapter → 本域应用用例 → 本域业务规则/事务边界
                         │ ports
                         ├→ 本域Repository/Outbox（基础设施实现）
                         └→ 外域typed client（仅合同，不导入外域实现）
```

- transport处理解码、传输身份、授权上下文与错误映射，不直接变成通用跨库业务路由器。
- application use case协调本域事务与外域port；跨域进度只有Workspace业务Saga拥有。
- domain承载真正不变量；没有复杂规则的读取无需为“DDD形式”堆空class/文件。
- repository属于本域；共用基础设施只共享机制，不共享价格、退款、权限或重试业务决策。
- DB entity、domain对象、wire DTO不是自动同一个模型；跨边界做显式语义转换，不透传ORM/map来规避契约。
- 同步RPC调用可以双向发生；禁止的是跨域代码依赖环、共享写权和分布式事务，不是简单禁止调用图出现回读边。

### 4.2 最容易混淆的事实

| 事实 | 唯一Owner | 其他域只能持有什么 |
|---|---|---|
| Package上传成功 | Capability | Build只持不可变输入/claim引用，不改Package状态 |
| 构建产物真实存在 | Build | Capability读回后决定自己的版本是否ready |
| 可部署CapabilityVersion | Capability | Build保存注册结果ID，不自行插入版本表 |
| 当前选中哪个Deployment | Workspace | Runtime报告候选执行事实，Fabric报告路由事实 |
| 实际资源、挂载、路由、Secret绑定 | Fabric | Workspace/Runtime只持引用与观察证据 |
| 平台套餐价/Quote | Resource Catalog | Workspace保存用户接受的不可变快照 |
| 已接受订阅/周期/补差义务 | Workspace | Catalog不能用新价改历史义务 |
| 可消费钱包/真实资金结果 | Sub2API；Gateway负责受限适配 | Ledger仅证据，Workspace仅义务与引用 |
| 用户成员权限/授权版本 | tenant | BFF/其他服务须验证，不从Key存在推导 |
| 不可变Receipt | Ledger | 来源Owner仍要对它自己的资源/资金事实负责 |

同一字段名如`workspaceId`贯穿多域不意味着多Owner；外域字段必须能归类为opaque reference、接受快照或readback projection，不能变成第二个可变权威。

## 5. 必须看懂的跨域链与传递字段

### 5.1 Package → Build → 唯一可部署版本

1. BFF→Capability：上传/完成输入，写PackageVersion；PackageUploaded只给Ledger审计，不自动Build。
2. BFF→Build：`CreateBuildRpcRequest(context, body)`，body的`packageVersionId`、`webuiVersionId`等按03，不能接受任意Runtime镜像。
3. Build→Capability：`ResolveBuildInput(package_version_id, webui_version_id)`取得Package对象引用、Runtime/WebUI artifact和publisher契约；Acquire/Bind以`claimant_owner/resource_id`和`OwnerCommitEvidence`确认真实固定输入。
4. Build本域：保存输入snapshot/claims、构建并读回digest；事件含`buildJobId/packageVersionId/runtimeVersionId/webuiVersionId/artifactDigest/artifactReceiptId/deploymentDescriptorDigest`。
5. Capability→Build：按`build_job_id`调用`ReadArtifact`，取得完整`DeploymentDescriptor`、claims、制品与证据；Capability唯一writer创建ready版本并发`capabilityVersionId/buildJobId/artifactDigest`。
6. Build消费注册结果后原job成功。任何ACK丢失恢复原身份，不跨库事务、不伪造ready。

### 5.2 接受报价 → 付费 → 可用Workspace

BFF选择`capabilityVersionId/computePlanId/storagePlanId/modelSelections`；Catalog核验Capability/Fabric事实、固定政策和报价；Workspace保存接受的`quoteId/inputDigest/价格快照`和原Operation，再调用Gateway资金port、Fabric资源port、Runtime执行port。选择提交需`deploymentId/descriptorDigest/executionEpoch/routeGeneration/providerRevision`等相符；最终Ledger收据绑定原Operation/Workspace。不能把requestId或资源已创建当应用可用。

### 5.3 模型配置 / 更新 / 套餐调整

Workspace持业务变更Operation与期望配置版本，Runtime Control执行并报告`appliedModelConfigurationVersion`；Fabric执行配额/挂载/路由并读回。`executionEpoch`防旧worker；`routeGeneration`只代表确认过的路由代次，不等同所有领域的事件revision。D17的金额/周期/一次ceil仍按13，不因拆服务重算政策。

### 5.4 Tenant治理与原义务收尾

tenant持`permission_version`及访问状态，签发受众/动作/资源绑定的授权；Gateway同进程但必须指定逻辑Owner。停用事件传`targetTenantId/tenantOperationId/status/restoreUntil`，Workspace/Gateway等各自处理。恢复Tenant不重买已删资源；退出会话不抹除已接受债务。

其余每条RPC与事件的完整字段、调用方向、写表和错误行为已在各域页逐条列出；这几条只解释核心业务，不取代06。

## 6. 真实发现：哪里混了，哪里尚未闭合

| ID | 证据与问题 | 为什么违反边界/证据层 | 修复归属与建议 |
|---|---|---|---|
| ALIGN-01 | 已在规格`OwnerOperationRequest.owner=3`；gateway transport仍可能查两个store | 规格已闭合寻址，真实消费者尚未接入；跨域同ID合法且另一库故障不应影响读取 | W01生成绑定/W02所有Owner transport同版消费；删两库扫描，拒绝错Owner、仅访问目标库 |
| ALIGN-02 | 18事件已在events.json按精确(eventType,schemaVersion)定义`x-aggregate-identity.type/idPayloadField`；route事件的payload及proto均补`routeBindingId` | 规格不需要额外自由`aggregate_type` wire字段；DB依旧需要Outbox/Inbox身份一致，现有store尚未据此拒绝自由字符串/失配 | W01/W02真实producer/consumer按固定映射验证payload ID和本域revision；更新生成绑定、事件fixture、重复/冲突/乱序反例 |
| ALIGN-03 | 规格已在`DeliverEventRequest`追加`consumer_owner=3`；renewal_settings同时订阅tenant/gateway | 真实dispatcher/Inbox尚未使用目标字段，仍可能把两个逻辑Owner当一个进程ACK | W01/W02按静态订阅列表各投递一次、各自DB事务/ACK；认证peer与producer body一致性核验 |
| ALIGN-04 | 03与api_inventory已将三条通用Operation REST标成`x-owner=bff`/`x-target-owner`；目标表仍`{owner}.operations` | 入口Owner与数据Owner已区分，真实BFF尚未实施；不得把Workspace当其他域Operation的writer | W13/W24实现有限枚举定向路由和目标Owner授权；派生10/14/reference同步，不建中央库 |
| ALIGN-05 | 六新server的Deliver返回Unimplemented；Reconcile只读当前行；ProductService/业务Coordination没注册 | 存储基础通过不证明跨域业务链完成 | status/W02明确已交付层；对应W03–W24实现真实用例/worker后再验收，禁止空ACK/假恢复 |
| ALIGN-06 | ReadOwnerCommit返回空accepted_input_digest、committed_version=0 | 01/02要求exact committed input证据，空字段不能用于claim/grant授权 | W02/W03/各Owner：从真实本域提交读出；缺证据明确拒绝，不编造版本或摘要 |
| ALIGN-07 | 新server `grpc.NewServer()`无mTLS/interceptor，context未用于当前Read权限验证 | 这是未接入的授权基础，不是已可安全暴露的服务 | W03：认证peer+受众/action/resource/Tenant校验和拒绝测试；本次不部署骨架 |
| ALIGN-08 | Inbox去重只比较payload hash，未比较event_type/schema/aggregate身份 | 同source eventId改元数据仍可能被当duplicate；无损身份契约未验收 | 本域Inbox基础/W01边界：比较不可变身份与payload；明确processed/ACK事务语义 |
| ALIGN-09 | 新服务测试使用规格外`resource_catalog.quote_accepted.v1`等 | 仅证明泛型存储机制，不能当18类生产事件验证 | 更换/补充真实typed契约用例；generic测试可保留但标清层次，不反向发明事件 |
| ALIGN-10 | 旧控制平面仍含公告/sync/验证等保留能力，09明确未迁则不删 | 不能为了“九域整洁”删除现有客户能力，也不能永久第二writer | W25逐能力映射与状态owner；未映射项保留旧owner，不先造新领域 |

这些不是新增产品需求。**ALIGN-01–04的权威规格现已修正，生产绑定和真实消费者仍未同版接入，不能把规格层关闭当实现层关闭。**同源规格`internal.proto`修改后，`packages/contracts/proto/internal.proto`及生成包需按固定工具重生并与真实服务一次性验证；不得覆盖旧失败回执来制造绿灯。

## 7. 对落地计划的结论：需要调整验收与顺序，不推翻九域

保留W00–W31编号，不另起一套计划，不重写已继承能力。14增加以下工作项：

1. **W01契约闭合修复**：先处理ALIGN-01–04及事件身份/字段来源，更新真实两端适配和正反例。工具生成成功只是子证据。
2. **W02基础验收补齐**：保留六模块已有工作；区分空库/schema、共同操作port与业务完成。修正跨Owner读取、提交证据/Inbox语义，Fabric/Ledger目标接入不能凭新DDL存在宣称完成。
3. **W03授权纵切**：一个真实调用从BFF/服务身份到tenant授权、目标Owner校验、DB读回；不得依赖空OwnerCommitEvidence签grant。
4. **按活能力提取旧代码**：W04资金、W05证据、W06价格、W10/11运行资源等优先复用原owner实现，再以typed适配切caller；不是重新从表字段拼六套业务。
5. **首条完整业务链**：仍按W07/08/09/13/14上传→构建→唯一版本；再W15的购买部署。每条都有正常/失败/重复/越权/重启断言。
6. **W25早期开始**：建立原字段/decoder→目标Owner事实→转换/拒绝/保留的映射，空库schema可并行但不能当历史转换。
7. **合回上游与Instance采用分别验收**：用户批准后增量PR/Issue合回；W29/W30独立负责运行资格/客户切换，不因代码merge自动执行。

## 8. 本次交付与限制

- 本次改动了现有v2.26的REST/proto/events权威规格与任务书，未修改产品Go/TS/SQL、生成绑定或真实服务消费者；不建新服务，不执行生产迁移，不提交/推送。
- 目标字段目录忠实投影当前规格；已关闭ALIGN-01–04的规格缺口，不把尚未完成的消费者接入/业务闭环修饰为已实现。
- 旧路由基于精确源码；动态map handler附原文和委托位置，尚不声称所有分支/授权已做浏览器黑盒重放。
- 本次隔离DB实测为继承模块空库install，不是全部W02 DB套件复跑，不证明旧数据转移正确。
- 规范校验绿色、当前原型实测、生成绑定存在、服务可启动、真实业务通过、生产采用是独立证据层，禁止相互替代。
