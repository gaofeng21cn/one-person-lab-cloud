# v2.26 Domain Reconciliation Implementation Plan

> 本文是待实施修改方案，不是已修改的契约或实现证据。执行时在原始Owner修改规则及其派生物，不让本计划成为第二字段规格。

**Goal:** 在继承的Cloud实现上收敛v2.26领域契约与真实交互，删除推断归属/虚假证据，按最小业务闭环开发，最终经用户验收合回上游。

**Architecture:** 九个数据Owner、八个领域后端部署单元及BFF的既定目标保留。`opl-cloud`是`one-person-lab-cloud`的开发fork，不是平行产品。代码可重构；产品规则和既有客户义务不能随代码重构被默默改变。

**Tech Stack:** Existing Go modules, PostgreSQL, protobuf/gRPC, React/TypeScript and current specification validators. No new orchestration framework, event bus, central registry or shared business framework.

## 1. 精确现状

- 上游本地共同基线：`50520e27a6b9a630eefdc2df7da3e5ec498d28a0`。
- fork HEAD：`7f5d05fe9b855d8af216caad714cf7fc85014d3c`。
- 继承的Control Plane/Fabric/Ledger业务代码尚未提取或替换；部分module依赖清单已变化。
- 六个新增服务module及七段目标Owner schema已提交；这是持久化/传输骨架，不是所有业务完成。
- 新业务ProductService未注册；Inbox业务dispatch返回Unimplemented，Reconcile主要读本地状态，提交证据缺真实digest/version，mTLS/授权仍未接入。
- 当前工作树已有上一轮盘点、reference、状态/计划修订及验证器变更，尚未提交。本次不得覆盖它们。
- `15_domain_alignment.md`与reference为核查导航；最终字段仍在02/03/proto/events。现有handoff=`needs_correction`，不是无条件全量READY。

## 2. 不变项与允许变化项

保持：F01–F17产品范围、批准的D17金额/周期/退款规则、Sub2API资金权威、Instance部署权限、原ID/订单/Key/Receipt/资源/未决义务、单writer、无跨域SQL/事务。

允许重构：所有fork代码、内部类型、handler/store分工、生成脚本、无实际用途的抽象、错误实现及其测试。六个已有骨架不是必须原样保留的资产。

暂不调整：九域职责、tenant/gateway同部署不同数据Owner、BFF入口、内部gRPC与本地Outbox机制。当前发现不足以证明这些边界应合并或增加服务。

不新增：中央Operation注册库、通用Saga服务、动态事件注册中心、全域共享Repository/ORM、全域聚合基类、没有真实调用的兼容/fallback分支。

## 3. 修改包A：职责与字段权威收敛

**Files:**
- `docs/architecture.md`, `docs/decisions.md`, `AGENTS.md`
- `docs/spec/v2.26/00_master_index.md`, `01_domain_ownership_matrix.md`
- `docs/spec/v2.26/15_domain_alignment.md`, `reference/domain-*.md`

**修改：**
1. 明确fork开发→验收→上游增量Issue/PR，区分代码合回、客户数据转换及Instance采用。
2. 每域以用例说明“独占事实/禁止项/本域事务/跨域port/失败义务”，不以一张表机械等同DDD聚合。
3. 同名外域字段必须归类为opaque reference、不可变接受快照、只读projection或本域事实。只有最后一类可以成为本域权威writer。
4. 明确BFF transport/application/domain/repository各层职责，但不强迫无复杂规则的读取建立空domain对象。
5. 收敛目录中重复的业务规则：reference是生成视图，人工边界只保留在其规定区域；机器字段不在新文档重复手写。

**验收：** 所有九域有单一责任；旧能力的保留/提取/新增/未移交状态明确；没有“另起产品”或“空库建表=客户迁移”的表述。

## 4. 修改包B：操作寻址与BFF元数据（W01/W02/W13）

**Files:**
- `docs/spec/v2.26/contracts/internal.proto`
- `docs/spec/v2.26/03_api_contract_complete.yaml`, `contracts/api_inventory.json`
- `docs/spec/v2.26/01_domain_ownership_matrix.md`, `02_database_schema_complete.md`
- `packages/contracts/proto/internal.proto`, generated `packages/contracts/go/v226/*`
- six service `internal/transport/server.go`, corresponding tests and typed clients
- traceability/domain-flow/development-plan generators and their generated projections

**方案：**
- 给现有`OwnerOperationRequest`追加`OperationOwnerEnum owner = 3`，不新建第三种Operation查询模型，不改已有字段号。现有GetOperationRpcRequest已有owner，但字段号布局不同，不能为复用而直接替换wire消息。
- owner在语义层必填；UNSPECIFIED/未知值/该部署不服务的Owner拒绝；只进入指定Owner的应用port/store。
- 删除gateway Read的两库扫描以及验证此行为的旧测试；保留两Owner相同operation ID的合法性。
- 所有单Owner服务也校验Read/Reconcile/ReadOwnerCommit目标，不忽略已有owner字段。
- BFF通用operation查询区分入口route owner与目标数据owner。修正03及inventory中workspace归类歧义，并同步实际使用该元数据的脚本；不引入运行时中央目录。
- 授权独立于寻址：有效owner不能代替peer、audience、Tenant和resource校验。

**验收：** 两库同ID仍准确返回指定域；另一个域DB故障不触发对它的读取；错误owner零store调用；找不到仅返回指定域NotFound；所有旧调用方在同一变更更新。

## 5. 修改包C：事件身份与接收方（W01/W02 + 活事件Owner）

**Files:**
- `docs/spec/v2.26/contracts/events.json`, `contracts/internal.proto`
- `02_database_schema_complete.md`, `06_data_flow_and_state_machine.md`
- event/protobuf cases, current decoder/adapter, owner Outbox/Inbox and focused tests
- SQL/inventory/migrations仅当确定存在持久化语义缺口时修改；不能为了列齐表而改库

### 5.1 aggregate_type：先规范已有信息，不默认增加wire字段

当前18类事件先列“producer/event version/业务对象类型/ID来源/revision/consumers”完整映射和未决项。需要给首条业务链用到的事件完成可执行定义，其他事件明确关联其工作包，不能假装全部已闭合，也不阻塞无依赖的工作。

首选：如`eventType+schemaVersion`及已有明确discriminator可以唯一确定aggregate_type，则在events.json按精确事件版本定义固定映射，两端从同一源生成/消费协议校验，Outbox/Inbox持久化同一值。禁止前缀/字符串split启发式和各域自行命名。这是协议解码，不是全域业务政策层。

如果某事件确实允许多种对象且现有字段不能唯一判定，先由该producer收敛事件语义；只有真实业务需要仍要求显式类型时才增加aggregateType/aggregate_type并同步JSON/proto/DB映射。显式字段与固定映射二选一成为该事件的权威规则，不能同时接收两个可能矛盾的自由输入。

必须一起定义aggregateId与payload对象ID一致性、revision分配者/串行边界/重放语义；不能只给类型词表。不得把routeGeneration、permissionVersion或时间戳无条件当通用事件revision。已有不可变事件身份随重试保持不变。

### 5.2 consumer owner：本例确有信息缺失

为`DeliverEventRequest`追加`OwnerEnum consumer_owner = 3`。同endpoint上的tenant和gateway存在共同订阅，event producer不足以定位此次接收方。一次请求一个Owner、一份Inbox事务、独立ACK；不靠receiver查业务表推断。

producer身份由认证peer验证，不能相信body中的authenticated_producer。合法目标还必须在该事件的静态订阅/允许集合内。

### 5.3 Inbox验收

去重绑定source owner/event ID以及不可变的event type/schema/aggregate identity/revision/payload；同ID元数据变化不得当duplicate。明确已接收、待处理、已处理和业务提交ACK的区别；不可只见row存在就称业务已应用。增加当前真实typed事件样本，规格外事件fixture只证明generic机制，不冒充生产协议。

**验收：** 实际Outbox→编码→解码→目标Inbox identity一致；失配拒绝；两consumer分别提交/ACK；重复/乱序/事务回滚/重启行为按06规定；不得新建全局事件服务。

## 6. 修改包D：诚实的实现与验收证据

**Files:**
- `docs/status.md`, `docs/roadmap.md`, `docs/implementation-architecture.md`
- `08_delivery_checklist_per_role.md`, `14_implementation_work_packages.md`
- `checks/render_development_plan.py`, `validate_domain_reference.py`, `validate_handoff.py`

**修改：**
1. W01分清协议定义/生成验证与消费者真正接入；W02分清schema/持久化/port骨架与业务应用。
2. ReadOwnerCommit只有真实accepted input/digest/version一致时可当claim/grant证据；证据未实现的路径拒绝或不暴露，不返回伪成功对象。具体证据从本域真实提交产生，不为凑协议引入无用全局版本。
3. Reconcile的恢复逻辑由实际动作Owner实现；纯读状态就是纯readback，不包装为已完成恢复引擎。
4. 骨架若无实际安全调用条件，明确只用于隔离开发，不能以监听成功当可生产暴露；W03把peer和用户权限链接起来。
5. 当前字段验证器把“必须有aggregateType wire字段”硬编码为验收条件，需要改为验证选定的规范表达是否完整且一致。不能让上一轮建议限制合理精简设计。
6. handoff分别报告reference coverage、契约缺口、工作包实现和运行证据；不能一项新缺口就无差别否定旧R01–R10全部测试，也不能通过允许known gaps把报告刷绿。

**验收：** 未实现路径不宣称完成；绿色编译/DB/静态测试不越层；字段映射方案可被测试验证而非按字段存在机械判断。

## 7. 修改包E：按最小用户闭环重新组织执行

保留W00–W31编号和产品范围；把每个大工作包按当前用例交付，不强迫先全量实现所有域。

### 第一条链：登录授权→上传→构建→唯一可部署版本→Console展示

需要的最小切片：
- W03：真实会话/成员授权、服务身份及当前调用需要的权限，不提前做完整Tenant治理。
- W05：该链需要的Build/注册证据类型与读回，不提前完成所有退款/迁移证据。
- W07/W08：批准publisher/runtime/webui、受限上传、digest和三类引用保护。
- W09：BuildKit/Storage/Registry真实链与Build→Capability注册回路，不mock生成成功。
- W13/W14：实际BFF会话/调用与必要页面，不先做所有管理员目录/资源价格UI。
- W06等如因原W14批次过宽形成依赖，只保留本切片实际需要的依赖；资源价格UI归第二条链，不能把新最小链依赖误写成“W06全量已经完成”。

尚未需要的Workspace购买/套餐变更/续费/删除用例不提前实现；其既有能力继续由继承服务承担。

### 第二条链：选版本+套餐→报价接受→原单付费→真实可用Workspace

按W04/W06/W10/W11/W15/W16提取原资金、价格、资源、Runtime能力；保留原ID/账务/资源，不实现第二套钱包/Provider逻辑。后续生命周期按原W17–W24推进。

### 迁移与上游集成

W25从第一批能力提取就建立字段/decoder/义务映射，09 M0–M5保持不变；客户切换仍W30。合回上游按小而完整的能力增量与用户授权处理，不一次性大覆盖、不要求生产部署才能审代码。

每个切片都必须声明：入口与调用方、domain不变量、事务内写集、跨域请求/响应字段、unknown/补偿边界、旧路径切换/退休、验证证据。仅有表、接口和空handler不算完成。

## 8. 执行顺序和验证

1. 先完成A及真实调用梳理；保留当前有效证据，不修改上游源码。
2. B与C按一份共享契约revision串行整合，两端实现可按独立文件并行；不在多个分支各自发明同一协议字段。
3. D同步完成，不等到业务发布才修状态口径。
4. E中不依赖未定契约的授权/上传等工作可以并行；禁止把所有W03工作冻结到所有18事件完整实现之后。
5. 先运行focused decoder/transport/DB身份边界测试，再契约生成无漂移、inventory/traceability/reference重生成和规格校验。
6. 实际涉及跨模块契约或持久化代码时执行`npm run verify:local:full`，如实记录失败/环境缺失，不以文档校验替代。
7. 涉及业务字段变更，所有消费者/测试同组更新；已有部署或数据使用过的迁移不得改写历史文件，追加owner迁移并验证。仅隔离未部署fixture才能按明确基线重建，不能猜测无客户数据。

## 9. 完成条件与明确不做

完成：职责/字段/交互只有一个权威；操作/事件可确定寻址；真实producer/consumer保持身份；首条链可正常、失败、重复、越权、重启后回读；继承义务保留；代码没有重复writer和不必要框架。

不做：产品范围扩大、收费规则再设计、全量字段强制新增、按表机械拆聚合、九域全部一次重写、为了统一形式给Ledger造Operation、生产访问/扣费/采购/迁移、未经授权的commit/push/PR/merge。

本计划需要的是技术执行与验收，不再要求用户批准字段号、代码文件名或例行测试。只有产品行为、费用、权限或历史客户义务发生实质变化时提出具体决策问题。
