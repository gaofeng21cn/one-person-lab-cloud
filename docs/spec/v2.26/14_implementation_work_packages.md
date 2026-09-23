# 14 完整开发执行任务书

> 目标：把已定稿F01–F17变成可直接分工实施的工作包。主Owner为Cloud架构/各领域负责人；产品/字段/协议Owner仍是00–13，本文件不重新定义它们。
> 本文件是执行规格，不是进度账本；not_implemented仅表示本生成器不认证实施完成。实际完成情况以docs/status.md与Owner证据为准，不代表已创建目录、跑通服务、迁移或发布。
> 每个任务列真实来源、拟创建写集、依赖、前后端产物及验收。原任务记录/失败验证保留历史，不用百分比冒充交付。

## 1. 完整方案的层次与交付

12产品主说明回答用户得到什么；11/04回答页面怎样操作；01/02/03/05/06/13回答权威、字段、接口、规则和恢复；09回答旧数据如何迁移；本14回答谁先做、改哪里、怎么测、交给谁；08仍是各角色统一验收Owner。

- 范围严格F01–F17，不增加Marketplace、公开注册、任意Runtime上传、新钱包或工作流框架。
- 金额、原单、D17、provider预付规则及data-loss边界不允许研发自选第二种解释。
- 设计/页面可按冻结规格并行开发，最后必须接真实BFF/Owner，不以静态prototype或mock冒充完成。
- 不预设“50人/3分钟/两周完成”等无依据承诺；人名/日历排期按实际资源填，不再改业务语义。

## 2. 代码与仓库落点

| 逻辑Owner | 工作根 | 当前状态 |
|---|---|---|
| cloud | `/Users/huangrende/Documents/ChatGPT/opl-cloud` | existing |
| console | `/Users/huangrende/Documents/ChatGPT/opl-cloud/apps/console-ui` | existing |
| bff | `/Users/huangrende/Documents/ChatGPT/opl-cloud/apps/console-bff` | planned_not_created |
| gateway | `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/gateway-integration` | existing |
| capability | `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/capability` | existing |
| build | `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/build` | existing |
| workspace | `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/workspace` | existing |
| runtime_control | `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/runtime-control` | existing |
| resource_catalog | `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/resource-catalog` | existing |
| fabric | `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/fabric` | existing |
| ledger | `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/ledger` | existing |
| tenant | `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/gateway-integration` | existing |
| instance | `/Users/huangrende/Documents/ChatGPT/opl-instance-medopl` | existing |

所有Cloud工作根均位于同一个opl-cloud GitHub仓库；路径由当前checkout推导，不绑定开发者机器。instance是外部Owner，不属于Cloud合仓写集；W29/W30仅描述其授权工作，不能由Cloud任务越界执行。具体模块/进程/数据库实施映射见01，架构决定以docs/architecture.md及docs/decisions.md为准，tenant与gateway两行共享一个部署单元。planned_not_created指目录未建，不是等待创建GitHub仓库。

## 3. 实施顺序、并行与完成依赖

先读[15领域字段与继承对齐](15_domain_alignment.md)。当前六模块已存在不代表W02全部接受标准满足；先收齐W01/W02跨Owner身份与证据断点，再进入依赖这些契约的真实业务链。保留W编号及原产品依赖，不新增人工审批轮。通用Operation接口是BFF入口（x-owner=bff），路径/必填查询owner才是数据Owner；W13/W24维护入口，各域实现本域读回。

现有W00→W02已具文档/绑定/六域骨架，不重建空服务；W01协议补齐与W02消费者适配仍未闭合。之后W03最小授权、W05当前Build证据、W07/08/09、W13/14按首切片先行；W06与其他不在首条链上的工作可按各自依赖推进，但不阻塞首切片。不是先做完所有微服务再看第一个用户结果。

- 第一条真实纵向闭环：W01协议修复/W02消费者边界→W03最小授权与W05 Build证据→W07/08/09→W13/14首切片，登录→上传→真实构建→唯一ready版本→真实Console。W14的价格/资源目录不由此切片冒充完成。
- 第二条真实纵向闭环：W04/05/06/10/11/15/16，Agent+套餐→付费fixture→真实Local应用。
- 第三条：W17–W23，配置/切换/升降配/续费/删除与管理治理。
- 迁移转换W25在契约/DB早期开始，生产切换在W30，不等最后才考虑旧ID和原单。
- 构建/Local/Instance资格/发布是W27–W31，代码测试、候选、实例采用和正式发布分层。

| 工作包 | 协调Owner | 可以开始的依赖 | 综合验收依赖 | 业务范围 |
|---|---|---|---|---|
| W00 目标入主文档与来源冻结 | Cloud架构负责人 | 无 | 本任务依赖与边界即可 | F01,F02,F03,F04,F05,F06,F07,F08,F09,F10,F11,F12,F13,F14,F15,F16,F17 |
| W01 生产契约与同仓版本一致性 | Cloud契约负责人 | W00 | 本任务依赖与边界即可 | F01,F02,F03,F04,F05,F06,F07,F08,F09,F10,F11,F12,F13,F14,F15,F16,F17 |
| W02 独立服务启动、DB角色及共同操作协议 | 各服务Owner | W01 | 本任务依赖与边界即可 | F01,F17 |
| W03 身份、Tenant成员及服务间授权 | Gateway Integration/CloudIdentity | W02 | 本任务依赖与边界即可 | F01 |
| W04 Gateway资金、Key与用量适配 | Gateway Integration | W03 | 本任务依赖与边界即可 | F08,F12,F13,F14 |
| W05 Ledger类型化证据与读回 | Ledger | W02 | 本任务依赖与边界即可 | F05,F08,F11,F12,F13,F16,F17 |
| W06 资源、价格与批准政策目录 | Resource Catalog | W02 | W11 | F03,F07,F11 |
| W07 发布者空间与Runtime/WebUI准入 | Capability | W03 | 本任务依赖与边界即可 | F03 |
| W08 Namespace、Package上传及输入claims | Capability | W03,W07 | 本任务依赖与边界即可 | F02,F04,F06 |
| W09 真实构建、注册和可部署版本 | Build（协调），Capability（资产writer） | W05,W08 | 本任务依赖与边界即可 | F04,F05,F06 |
| W10 Runtime描述执行与控制 | Runtime Control | W02,W07 | W11 | F08,F09,F10,F11,F16 |
| W11 Local-Docker执行/存储/路由资格能力 | Fabric Local-Docker | W02 | 本任务依赖与边界即可 | F08,F09,F10,F11,F12,F13,F16,F17 |
| W12 Tencent/TKE预付执行与计划变更 | Fabric Tencent | W02 | 本任务依赖与边界即可 | F08,F10,F11,F12,F13,F17 |
| W13 BFF和Console基础接入 | Console/BFF | W01,W03 | 本任务依赖与边界即可 | F01,F17 |
| W14 智能体/上传/构建/目录前端 | Console | W13 | W06,W07,W08,W09 | F02,F03,F04,F05,F06 |
| W15 准确报价与Agent+套餐Launch闭环 | Workspace（协调）/Catalog | W04,W05,W06,W09,W10,W11 | 本任务依赖与边界即可 | F07,F08 |
| W16 部署向导与Workspace页面 | Console | W13 | W15,W17,W25 | F07,F08,F09,F10,F16 |
| W17 应用配置、版本切换与回滚 | Workspace/Runtime Control | W10,W11,W15 | 本任务依赖与边界即可 | F09,F10 |
| W18 周期、显式续费授权和到期停用 | Workspace | W04,W05,W15 | 本任务依赖与边界即可 | F12 |
| W19 D17套餐变更与补差账务 | Workspace（协调）/Catalog | W06,W11,W12,W15,W18 | 本任务依赖与边界即可 | F11,F12,F13 |
| W20 删除、base与supplement原单结算 | Workspace（协调） | W04,W05,W15,W18 | W19 | F13 |
| W21 Tenant开通、暂停/重新启用及删除恢复 | CloudIdentity（协调） | W03,W04,W15,W20 | 本任务依赖与边界即可 | F01,F15 |
| W22 费用、计划、续费/退款与Key前端 | Console | W13 | W04,W18,W19,W20 | F11,F12,F13,F14 |
| W23 管理员目录、Tenant与运维前端 | Console | W13 | W06,W07,W21,W24 | F03,F15,F17 |
| W24 Owner运维、原操作恢复和可观测性 | 各Owner/Console集成 | W03,W05 | W15,W19,W20,W21 | F17 |
| W25 逐域数据转换与迁移演练 | 各数据Owner/Instance | W00,W02 | W09,W17,W19,W20,W21 | F16 |
| W26 真实业务链集成与浏览器验收 | 集成负责人+各Owner | W09,W13,W15 | W14,W16,W17,W19,W20,W21,W22,W23,W24,W25 | F01,F02,F03,F04,F05,F06,F07,F08,F09,F10,F11,F12,F13,F14,F15,F16,F17 |
| W27 可移植候选构建与CI契约演进 | Cloud发布机制Owner | W01,W02 | W09,W12,W21,W24,W26 | F17 |
| W28 干净Linux Local资格 | Cloud/Local qualification | W27 | 本任务依赖与边界即可 | F17 |
| W29 Instance/Tencent资格与受限业务实测 | opl-instance-medopl Owner | W28,W12,W19,W25 | 本任务依赖与边界即可 | F17 |
| W30 生产客户逐批切换与旧writer退休 | Instance+数据Owner | W25,W26,W29 | 本任务依赖与边界即可 | F16,F17 |
| W31 同字节正式发布与文档收尾 | Cloud发布Owner | W28,W29 | 本任务依赖与边界即可 | F17 |

首链切片单独门槛：W14 `firstSliceAcceptAfter=W07,W08,W09,W13`；W14全包验收仍依赖W06/W07/W08/W09。任何切片通过不得把整包标为完成。

### 同文件与发布边界的串行点

- W00独占canonical目标同步；W01独占同一shared contract revision及必要consumer依赖整合，消费者同commit吸收。W02并行任务不各自改写共享契约或根构建配置。
- Workspace各工作包可以并行内部独立文件，但router/main/store/schema aggregate与同一migration序号由Workspace Owner串行合入，不双写/不另建全局协调器。
- Console各页面/独立controller可并行；console-router、use-console-controller和共享DTO整合由W13/Console Owner串行吸收。
- Fabric Local/Tencent在适配器内并行；provider_port/public DTO/通用handler变更必须同版更新两端与测试。
- 数据库正式迁移、canonical main、Candidate冻结、Instance运行状态和正式发布不并行写。
- W31不等于W30。产品可以发布已资格字节，客户迁移可分批；不能据产品发布宣称所有Instance已采用。

## 4. 工作包详单

### W00 目标入主文档与来源冻结

**协调Owner：** Cloud架构负责人；**参与Owner：** cloud。
**F范围：** F01, F02, F03, F04, F05, F06, F07, F08, F09, F10, F11, F12, F13, F14, F15, F16, F17；**开始依赖：** 无；**验收依赖：** 按本任务边界。

**现有来源（当前checkout定位，不表示全部要改；原始source snapshot见09）**：
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/docs/README.md`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/docs/architecture.md`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/docs/decisions.md`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/docs/implementation-architecture.md`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/docs/status.md`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/docs/roadmap.md`

**拟写入位置（未来实施，尚未创建的路径也明确列出）**：
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/docs/architecture.md`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/docs/decisions.md`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/docs/roadmap.md`

**必须交付**：
- 把已确认Agent+套餐目标、D17和逐域单writer迁移写入canonical owner，历史resource_only义务保留
- 记录Cloud/Instance/Runtime publisher精确sourceSHA与schema版本；形成实现起点差异清单，不把目标写成现状
- 明确fork→upstream集成关系：opl-cloud为当前开发checkout，最终经用户验收的Issue/PR合回one-person-lab-cloud；代码集成不等于数据迁移/Instance部署

**验证**：
- 对照00/09/13核对上下层无竞争writer
- git diff --check

**完成判定**：
- 同一目标只一个writer，现行实现/目标/Instance事实分层
- 不重新征求已批准业务决定，不改变用户现有未提交文件

### W01 生产契约与同仓版本一致性

**协调Owner：** Cloud契约负责人；**参与Owner：** cloud。
**F范围：** F01, F02, F03, F04, F05, F06, F07, F08, F09, F10, F11, F12, F13, F14, F15, F16, F17；**开始依赖：** W00；**验收依赖：** 按本任务边界。

**现有来源（当前checkout定位，不表示全部要改；原始source snapshot见09）**：
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/packages/contracts/README.md`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/packages/contracts/go/go.mod`

**拟写入位置（未来实施，尚未创建的路径也明确列出）**：
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/packages/contracts/proto`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/packages/contracts/go`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/tests/contracts`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/control-plane/go.mod`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/control-plane/go.sum`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/fabric/go.mod`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/fabric/go.sum`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/ledger/go.mod`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/ledger/go.sum`

**必须交付**：
- 按真实两端caller把03/proto/events/publisher/plan-change规格进入生产owner；不是整包inventories复制到runtime
- 锁定schema SHA和生成工具版本；每个consumer生成/校验typed client，只有owner定义业务类型
- 单仓消费者与契约来自同一Cloud commit，绑定schema SHA与生成工具版本；外部Owner才按精确commit/tag或digest接入，不用moving main/latest
- 沿用packages/contracts/go/go.mod；生成包为packages/contracts/go/v226，不为版本子包新建module；以锁定生成配置映射proto import到实际module路径，不手改生成代码
- 必要消费者go.mod/go.sum调整属于契约写集：记录实际依赖链与选定版本，不为旧测试或保持diff不变增加兼容字段、强压依赖或另建模块
- ALIGN-01–04在规格层已定义，实施同源proto生成绑定/事件映射与真实消费者，完整传递owner+operationId/consumer及aggregate身份；BFF仅路由不持数据
- 为每个活跨域API建立字段来源→wire→接收Owner验证→本域写入/读回映射；更新原02/03/proto/events与所有消费者，不另造平行DTO
- 既有OwnerOperationRequest追加owner=3、DeliverEventRequest追加consumer_owner=3；18事件按精确事件版本固定aggregate_type及payload ID来源，不增加无必要的wire自由字符串，route_observed补真实routeBindingId。BFF三条通用Operation接口的x-owner是bff而不是Workspace writer。

**验证**：
- 编译Go/TS/protobuf客户端，按各自协议规则验证JSON/protobuf正反例、金额string/int64及source ms精度；仅生成成功不算消费者接入完成
- 在同一checkout重放生成并检查无漂移，运行受影响服务回归与npm run verify:local:full；规格验证不替代消费者验证
- 运行已有checks/validate_spec.py与validate_d17_contract.py作规格基线，不冒充实现测试
- 真实decoder/gRPC适配边界：同ID不同Owner、错Owner零store调用、事件身份无损/冲突拒绝；字段目录完整不等于此链通过
- 实施前现有基线（不证明新功能）：rtk proxy go -C /Users/huangrende/Documents/ChatGPT/opl-cloud/packages/contracts/go test ./... -count=1

**完成判定**：
- 双方消费者同一契约版本，金额string/int64与source ms精度一致
- 契约升级在一组可回放变更中完成，不让旧测试驱动兼容字段
- 规范定义与受影响消费者在同版接入；只有字段和生成包不算闭合，未接入的真实服务仍标未完成。

### W02 独立服务启动、DB角色及共同操作协议

**协调Owner：** 各服务Owner；**参与Owner：** gateway, tenant, capability, build, workspace, runtime_control, resource_catalog, fabric, ledger。
**F范围：** F01, F17；**开始依赖：** W01；**验收依赖：** 按本任务边界。

**现有来源（当前checkout定位，不表示全部要改；原始source snapshot见09）**：
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/control-plane/go.mod`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/fabric/go.mod`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/ledger/go.mod`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/internal/postgresmigrate`

**拟写入位置（未来实施，尚未创建的路径也明确列出）**：
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/gateway-integration/go.mod`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/gateway-integration/go.sum`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/gateway-integration/cmd/server`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/gateway-integration/internal/transport`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/gateway-integration/migrations`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/capability/go.mod`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/capability/go.sum`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/capability/cmd/server`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/capability/internal/transport`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/capability/migrations`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/build/go.mod`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/build/go.sum`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/build/cmd/server`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/build/internal/transport`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/build/migrations`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/workspace/go.mod`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/workspace/go.sum`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/workspace/cmd/server`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/workspace/internal/transport`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/workspace/migrations`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/runtime-control/go.mod`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/runtime-control/go.sum`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/runtime-control/cmd/server`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/runtime-control/internal/transport`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/runtime-control/migrations`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/resource-catalog/go.mod`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/resource-catalog/go.sum`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/resource-catalog/cmd/server`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/resource-catalog/internal/transport`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/resource-catalog/migrations`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/gateway-integration/internal/identity`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/fabric/cmd/fabric`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/fabric/internal/http`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/fabric/internal/fabric/ent_migrations`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/ledger/cmd/ledger`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/ledger/internal/http`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/ledger/internal/ledger/ent_migrations`

**必须交付**：
- 按01同仓目录启动独立服务module/进程/DB角色；tenant与gateway同module同部署单元但不同DB角色，不按每个RPC拆服务
- 每Owner实现自己的Operation、幂等、Outbox/Inbox、健康/就绪与迁移入口；公共基础设施仅在有两个真实caller时共享
- 为本地测试准备各Owner独立database/角色及隔离fixture进程；不以同schema代替跨Owner权限隔离，不复制钱包或造生产test-billing入口
- 已有六模块骨架保留；逐项区分schema/存储协议、已注册port、Unimplemented与业务用例；修正跨Owner读取、空提交证据和Inbox元数据去重，不把纯readback叫完整reconcile

**主实现表（字段唯一来源02，不在此复制字段定义）**：`tenant.outbox_events`, `tenant.outbox_deliveries`, `tenant.inbox_events`, `tenant.idempotency_records`, `capability.outbox_events`, `capability.outbox_deliveries`, `capability.inbox_events`, `capability.idempotency_records`, `build.outbox_events`, `build.outbox_deliveries`, `build.inbox_events`, `build.idempotency_records`, `workspace.outbox_events`, `workspace.outbox_deliveries`, `workspace.inbox_events`, `workspace.idempotency_records`, `runtime_control.outbox_events`, `runtime_control.outbox_deliveries`, `runtime_control.inbox_events`, `runtime_control.idempotency_records`, `fabric.outbox_events`, `fabric.outbox_deliveries`, `fabric.inbox_events`, `fabric.idempotency_records`, `gateway.outbox_events`, `gateway.outbox_deliveries`, `gateway.inbox_events`, `gateway.idempotency_records`, `resource_catalog.outbox_events`, `resource_catalog.outbox_deliveries`, `resource_catalog.inbox_events`, `resource_catalog.idempotency_records`, `ledger.outbox_events`, `ledger.outbox_deliveries`, `ledger.inbox_events`, `ledger.idempotency_records`, `tenant.operations`, `capability.operations`, `build.operations`, `workspace.operations`, `gateway.operations`, `resource_catalog.operations`, `runtime_control.operations`, `fabric.operations`

**内部协议实现/协作端口**：`OwnerOperations.Read`, `OwnerOperations.Reconcile`, `OwnerCommitReadback.ReadOwnerCommit`, `DomainInbox.Deliver`

**验证**：
- 在拥有方Go module执行go test ./...；使用实际typed DTO与decoder
- Cloud结构性/持久化/跨模块修改后npm run verify:local:full；仅普通源码改变用verify:local
- 该命令只作实施后要求；本轮没有执行新产品实现测试

**完成判定**：
- DB角色只写本域，无跨库FK/JOIN；同一writer无双写
- readiness真实暴露依赖不就绪；未知结果不默认成功
- 新DDL不表示Fabric/Ledger旧loader已迁移；每域真实迁移入口/角色/请求与协议一致；未完成的业务dispatch明确归后续Owner工作包

### W03 身份、Tenant成员及服务间授权

**协调Owner：** Gateway Integration/CloudIdentity；**参与Owner：** tenant, gateway。
**F范围：** F01；**开始依赖：** W02；**验收依赖：** 按本任务边界。

**现有来源（当前checkout定位，不表示全部要改；原始source snapshot见09）**：
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/control-plane/internal/server/routes_auth.go`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/control-plane/internal/clients/sub2api.go`

**拟写入位置（未来实施，尚未创建的路径也明确列出）**：
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/gateway-integration/internal/identity`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/gateway-integration/internal/authorization`

**必须交付**：
- Gateway个人登录→Cloud session/Tenant/membership；平台权限与Tenant角色分离
- 实现AuthorizeAction、OwnerCommitReadback验证、accepted-operation grant、权限版本/受众及撤权
- 实现邀请/角色/最后owner保护；开发fixture使用已知测试Tenant，不向外部自动注册钱包
- 把mTLS peer、受众/action/resource/Tenant校验接入真实服务边界；grant不得依据空accepted_input_digest/虚构committed_version签发

**主实现API（沿用03的唯一Owner，不是改变数据写权）**：`acceptInvitation`, `getLoginContext`, `getSession`, `getTenant`, `inviteMember`, `listInvitations`, `listMembers`, `login`, `logout`, `removeMember`, `revokeInvitation`, `updateMemberRole`

**主实现表（字段唯一来源02，不在此复制字段定义）**：`tenant.tenants`, `tenant.tenant_members`, `tenant.invitations`, `tenant.sessions`, `tenant.audit_events`, `tenant.authorization_contexts`, `tenant.accepted_operation_grants`

**内部协议实现/协作端口**：`TenantProductService.GetLoginContext`, `TenantProductService.Login`, `TenantProductService.GetSession`, `TenantProductService.Logout`, `TenantProductService.GetTenant`, `TenantProductService.ListMembers`, `TenantProductService.ListInvitations`, `TenantProductService.InviteMember`, `TenantProductService.AcceptInvitation`, `TenantProductService.RevokeInvitation`, `TenantProductService.UpdateMemberRole`, `TenantProductService.RemoveMember`, `CloudIdentityAuthorization.AuthorizeAction`, `CloudIdentityAuthorization.GetAuthorizationContext`, `CloudIdentityAuthorization.IssueAcceptedOperationGrant`

**验证**：
- 在拥有方Go module执行go test ./...；使用实际typed DTO与decoder
- Cloud结构性/持久化/跨模块修改后npm run verify:local:full；仅普通源码改变用verify:local
- 该命令只作实施后要求；本轮没有执行新产品实现测试
- 实施前现有基线（不证明新功能）：rtk proxy go -C /Users/huangrende/Documents/ChatGPT/opl-cloud/services/control-plane test ./internal/server -run '^(TestDelegatedCredentialNeverPersistsOrLeaks|TestAccountDisableRevokesSessionCredential|TestGatewayKeyOwnership|TestCloudAdminCanRevealOnlyOwnGatewayKey)$' -count=1

**完成判定**：
- 跨Tenant/伪actor/伪audience/撤权拒绝；session退出不丢原已接受义务
- 前端不收到服务token，不保存密码，future consent不从role缓存猜

### W04 Gateway资金、Key与用量适配

**协调Owner：** Gateway Integration；**参与Owner：** gateway。
**F范围：** F08, F12, F13, F14；**开始依赖：** W03；**验收依赖：** 按本任务边界。

**现有来源（当前checkout定位，不表示全部要改；原始source snapshot见09）**：
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/control-plane/internal/clients/sub2api.go`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/control-plane/internal/server/wallet_adjustment.go`

**拟写入位置（未来实施，尚未创建的路径也明确列出）**：
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/gateway-integration/internal/sub2api`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/gateway-integration/internal/wallet`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/gateway-integration/internal/keys`

**必须交付**：
- 沿用Sub2API真实接口/身份和一次性Code，完成base/supplement/period/refund原单读回
- 仅映射/操作证据，无余额镜像；unknown禁止新财务副作用
- 个人Key管理与托管Workspace Key边界明确；明文仅授权一次性响应
- 以15及reference继承盘点为基线，声明保留/提取/新增的真实caller和writer；优先复用现行owner decoder/规则，不按新表逐字段重写一套业务

**主实现API（沿用03的唯一Owner，不是改变数据写权）**：`createGatewayKey`, `getWallet`, `listGatewayKeys`, `listModels`, `listRechargeRecords`, `listUsage`, `listWorkspaceTransactions`, `revealGatewayKey`, `revokeGatewayKey`

**主实现表（字段唯一来源02，不在此复制字段定义）**：`gateway.identity_mappings`, `gateway.tenant_wallet_bindings`, `gateway.key_bindings`, `gateway.wallet_operations`

**内部协议实现/协作端口**：`GatewayProductService.ListWorkspaceTransactions`, `GatewayProductService.GetWallet`, `GatewayProductService.ListUsage`, `GatewayProductService.ListGatewayKeys`, `GatewayProductService.CreateGatewayKey`, `GatewayProductService.RevealGatewayKey`, `GatewayProductService.RevokeGatewayKey`, `GatewayProductService.ListRechargeRecords`, `GatewayProductService.ListModels`, `GatewayCoordination.BindWallet`, `GatewayCoordination.Debit`, `GatewayCoordination.Refund`, `GatewayCoordination.ReadWalletAction`, `GatewayCoordination.CreateManagedKey`, `GatewayCoordination.RevokeManagedKey`, `GatewayPlanChangeSettlement.DebitSupplement`, `GatewayPlanChangeSettlement.DebitScheduledPeriod`, `GatewayPlanChangeSettlement.RefundFailure`, `GatewayPlanChangeSettlement.RefundSupplementOnDeletion`

**验证**：
- 在拥有方Go module执行go test ./...；使用实际typed DTO与decoder
- Cloud结构性/持久化/跨模块修改后npm run verify:local:full；仅普通源码改变用verify:local
- 该命令只作实施后要求；本轮没有执行新产品实现测试
- 实施前现有基线（不证明新功能）：rtk proxy go -C /Users/huangrende/Documents/ChatGPT/opl-cloud/services/control-plane test ./internal/clients -run '^(TestAuthenticateUserReturnsDelegatedCredential|TestSub2APIFinancialHistoryUsesNativeExactNotesAndAllPages|TestSub2APIAdjustmentConnectionLossDoesNotRepeatDebit|TestSub2APIRefundRequiresReadOnlyRebatePolicy)$' -count=1

**完成判定**：
- 重复/丢响应/余额并发/错原单/错钱包/超额及unknown退款全部用真实decoder验证
- 不用余额差作为付款证明，不在普通CI触碰真钱

### W05 Ledger类型化证据与读回

**协调Owner：** Ledger；**参与Owner：** ledger。
**F范围：** F05, F08, F11, F12, F13, F16, F17；**开始依赖：** W02；**验收依赖：** 按本任务边界。

**现有来源（当前checkout定位，不表示全部要改；原始source snapshot见09）**：
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/ledger/internal/ledger/types.go`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/control-plane/internal/clients/ledger.go`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/ledger/ent/schema`

**拟写入位置（未来实施，尚未创建的路径也明确列出）**：
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/ledger/internal/ledger`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/ledger/ent/schema`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/ledger/internal/ledger/ent_migrations`

**必须交付**：
- 支持Build无Workspace收据、新计划变更/补差/迁移证据；精确原单和输入输出验证
- 保留旧receipt字节/类型/ID与幂等义务，新索引只投影
- 提供管理员只读证据和资格投影，不让Ledger编排业务
- 以15及reference继承盘点为基线，声明保留/提取/新增的真实caller和writer；优先复用现行owner decoder/规则，不按新表逐字段重写一套业务

**主实现API（沿用03的唯一Owner，不是改变数据写权）**：`getReceipt`, `listQualifications`, `listReceipts`

**主实现表（字段唯一来源02，不在此复制字段定义）**：`ledger.receipts`, `ledger.reconciliations`

**内部协议实现/协作端口**：`LedgerProductService.ListReceipts`, `LedgerProductService.GetReceipt`, `LedgerProductService.ListQualifications`, `LedgerCoordination.AppendReceipt`, `LedgerCoordination.ReadReceiptByReference`, `LedgerPlanChangeEvidence.AppendPlanChangeReceipt`, `LedgerPlanChangeEvidence.AppendPlanChangeRefundReceipt`, `DomainInbox.Deliver`

**验证**：
- 在拥有方Go module执行go test ./...；使用实际typed DTO与decoder
- Cloud结构性/持久化/跨模块修改后npm run verify:local:full；仅普通源码改变用verify:local
- 该命令只作实施后要求；本轮没有执行新产品实现测试

**完成判定**：
- 缺必填/伪Workspace/相同键不同hash/秘密泄露拒绝
- append-only/权限/retention真实DB检查；没有运行证据不显示qualified

### W06 资源、价格与批准政策目录

**协调Owner：** Resource Catalog；**参与Owner：** resource_catalog。
**F范围：** F03, F07, F11；**开始依赖：** W02；**验收依赖：** W11。

**现有来源（当前checkout定位，不表示全部要改；原始source snapshot见09）**：
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/control-plane/internal/server/routes_billing.go`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/packages/contracts/go/workspace_delete.go`

**拟写入位置（未来实施，尚未创建的路径也明确列出）**：
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/resource-catalog/internal/catalog`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/resource-catalog/internal/pricing`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/resource-catalog/migrations`

**必须交付**：
- 资源plan先创建，组合价格后创建；版本/有效期/approved transition具有明确owner
- 实现D17固定政策和base退款政策分离、实际周期/单次ceil、canonical毫秒，不写任意公式框架
- 客户列表只投影有效已批准组合，容量仍由Fabric确认
- 以15及reference继承盘点为基线，声明保留/提取/新增的真实caller和writer；优先复用现行owner decoder/规则，不按新表逐字段重写一套业务

**主实现API（沿用03的唯一Owner，不是改变数据写权）**：`createComputePlan`, `createPricePolicyVersion`, `createRefundPolicyVersion`, `createRetentionPolicyVersion`, `createStoragePlan`, `listComputePlans`, `listPricePolicyVersions`, `listRefundPolicyVersions`, `listRetentionPolicyVersions`, `listStoragePlans`, `setComputePlanAvailability`, `setStoragePlanAvailability`

**主实现表（字段唯一来源02，不在此复制字段定义）**：`resource_catalog.compute_plans`, `resource_catalog.storage_plans`, `resource_catalog.price_policy_versions`, `resource_catalog.refund_policy_versions`, `resource_catalog.retention_policy_versions`

**内部协议实现/协作端口**：`ResourceCatalogProductService.ListComputePlans`, `ResourceCatalogProductService.ListStoragePlans`, `ResourceCatalogProductService.CreateComputePlan`, `ResourceCatalogProductService.SetComputePlanAvailability`, `ResourceCatalogProductService.CreateStoragePlan`, `ResourceCatalogProductService.SetStoragePlanAvailability`, `ResourceCatalogProductService.ListPricePolicyVersions`, `ResourceCatalogProductService.CreatePricePolicyVersion`, `ResourceCatalogProductService.ListRefundPolicyVersions`, `ResourceCatalogProductService.CreateRefundPolicyVersion`, `ResourceCatalogProductService.ListRetentionPolicyVersions`, `ResourceCatalogProductService.CreateRetentionPolicyVersion`

**验证**：
- 在拥有方Go module执行go test ./...；使用实际typed DTO与decoder
- Cloud结构性/持久化/跨模块修改后npm run verify:local:full；仅普通源码改变用verify:local
- 该命令只作实施后要求；本轮没有执行新产品实现测试
- 重放13及checks/d17_acceptance_vectors.json的独立数值、纳秒进位与int64边界

**完成判定**：
- 无双重取整/浮点/毫秒从PG重算；新购买周期1个月，历史周期不改
- 管理员准入目录不执行资源采购

### W07 发布者空间与Runtime/WebUI准入

**协调Owner：** Capability；**参与Owner：** capability。
**F范围：** F03；**开始依赖：** W03；**验收依赖：** 按本任务边界。

**现有来源（当前checkout定位，不表示全部要改；原始source snapshot见09）**：
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/packages/contracts/go/workspace_application.go`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/control-plane/internal/server/workspace_application_admission.go`

**拟写入位置（未来实施，尚未创建的路径也明确列出）**：
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/capability/internal/publishers`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/capability/internal/catalog`

**必须交付**：
- 官方/第三方publisher namespace、registry prefix、完整不可变PublisherContract
- 复用当前WorkspaceApplicationRevision/Go validator，不另造缩减版端口/探针/Secret模型
- 默认构建策略只选择批准版本，新发布不自动替换旧Workspace

**主实现API（沿用03的唯一Owner，不是改变数据写权）**：`createPublisherNamespace`, `getBuildRuntimePolicy`, `listPublisherNamespaces`, `listRuntimeVersions`, `listWebuiVersions`, `publishOfficialPackage`, `registerRuntimeVersion`, `registerWebuiVersion`, `revokePublisherNamespace`, `setBuildRuntimePolicy`, `setRuntimeVersionStatus`, `setWebuiVersionStatus`

**主实现表（字段唯一来源02，不在此复制字段定义）**：`capability.runtime_versions`, `capability.webui_versions`, `capability.catalog_policies`, `capability.publisher_namespaces`

**内部协议实现/协作端口**：`CapabilityProductService.PublishOfficialPackage`, `CapabilityProductService.ListRuntimeVersions`, `CapabilityProductService.ListWebuiVersions`, `CapabilityProductService.RegisterRuntimeVersion`, `CapabilityProductService.SetRuntimeVersionStatus`, `CapabilityProductService.RegisterWebuiVersion`, `CapabilityProductService.SetWebuiVersionStatus`, `CapabilityProductService.GetBuildRuntimePolicy`, `CapabilityProductService.SetBuildRuntimePolicy`, `CapabilityProductService.ListPublisherNamespaces`, `CapabilityProductService.CreatePublisherNamespace`, `CapabilityProductService.RevokePublisherNamespace`, `CapabilityCoordination.ResolveBuildInput`, `CapabilityCoordination.ResolvePublisherContract`, `CapabilityCoordination.AcquireReference`, `CapabilityCoordination.BindReference`, `CapabilityCoordination.ReleaseReference`

**验证**：
- 在拥有方Go module执行go test ./...；使用实际typed DTO与decoder
- Cloud结构性/持久化/跨模块修改后npm run verify:local:full；仅普通源码改变用verify:local
- 该命令只作实施后要求；本轮没有执行新产品实现测试
- 两类Runtime真source validator/startup DAG及schema拒绝反例
- 实施前现有基线（不证明新功能）：rtk proxy go -C /Users/huangrende/Documents/ChatGPT/opl-cloud/packages/contracts/go test ./... -count=1

**完成判定**：
- 完整repository/digest/platform/recipe/revision来源可回读
- 第三方不能混入官方prefix；无新SSO或固定Runtime路径假设

### W08 Namespace、Package上传及输入claims

**协调Owner：** Capability；**参与Owner：** capability。
**F范围：** F02, F04, F06；**开始依赖：** W03, W07；**验收依赖：** 按本任务边界。

**现有来源（当前checkout定位，不表示全部要改；原始source snapshot见09）**：
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/control-plane/internal/server/application_revision_store.go`

**拟写入位置（未来实施，尚未创建的路径也明确列出）**：
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/capability/internal/packages`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/capability/internal/uploads`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/capability/internal/references`

**必须交付**：
- 分组/Package/版本与受限Storage直传、实际摘要校验及续传
- 四类ReferenceTarget，Acquire→consumer本地保存→Bind；释放须真实Owner usage/终态
- 归档保留历史，目录tombstone与物理删除分开；无过期自动释放引用

**主实现API（沿用03的唯一Owner，不是改变数据写权）**：`archiveNamespace`, `archivePackage`, `completeUpload`, `createNamespace`, `createPackage`, `createUpload`, `createUploadPart`, `getPackage`, `getPackageVersion`, `getUpload`, `listNamespaces`, `listPackageVersions`, `listPackages`, `updateNamespace`, `updatePackage`

**主实现表（字段唯一来源02，不在此复制字段定义）**：`capability.namespaces`, `capability.packages`, `capability.package_versions`, `capability.upload_sessions`, `capability.upload_chunks`, `capability.reference_claims`

**内部协议实现/协作端口**：`CapabilityProductService.ListNamespaces`, `CapabilityProductService.CreateNamespace`, `CapabilityProductService.UpdateNamespace`, `CapabilityProductService.ArchiveNamespace`, `CapabilityProductService.ListPackages`, `CapabilityProductService.CreatePackage`, `CapabilityProductService.GetPackage`, `CapabilityProductService.UpdatePackage`, `CapabilityProductService.ArchivePackage`, `CapabilityProductService.CreateUpload`, `CapabilityProductService.GetUpload`, `CapabilityProductService.CreateUploadPart`, `CapabilityProductService.CompleteUpload`, `CapabilityProductService.ListPackageVersions`, `CapabilityProductService.GetPackageVersion`, `ClaimUsageReadback.ReadClaimUsage`, `CapabilityCoordination.ResolveBuildInput`, `CapabilityCoordination.ResolvePublisherContract`, `CapabilityCoordination.AcquireReference`, `CapabilityCoordination.BindReference`, `CapabilityCoordination.ReleaseReference`

**验证**：
- 在拥有方Go module执行go test ./...；使用实际typed DTO与decoder
- Cloud结构性/持久化/跨模块修改后npm run verify:local:full；仅普通源码改变用verify:local
- 该命令只作实施后要求；本轮没有执行新产品实现测试

**完成判定**：
- 首包无CapabilityVersion仍可保护Package/Runtime/WebUI输入
- 路径穿越/错摘要/跨Tenant/过期签名/并发删除/Bind丢响应均覆盖

### W09 真实构建、注册和可部署版本

**协调Owner：** Build（协调），Capability（资产writer）；**参与Owner：** build, capability。
**F范围：** F04, F05, F06；**开始依赖：** W05, W08；**验收依赖：** 按本任务边界。

**现有来源（当前checkout定位，不表示全部要改；原始source snapshot见09）**：
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/docs/agent-lifecycle.md`

**拟写入位置（未来实施，尚未创建的路径也明确列出）**：
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/build/internal/jobs`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/build/internal/builder`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/build/internal/registry`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/capability/internal/artifacts`

**必须交付**：
- 冻结输入与recipe，真实BuildKit构建及Registry exporter/readback
- typed完成事件→Capability Inbox事务创建唯一ready版本；重试新job保留旧历史
- 管理员/客户查询构建日志及版本、引用保护下架；无用户自定义任意脚本旁路

**主实现API（沿用03的唯一Owner，不是改变数据写权）**：`createBuild`, `deleteCapabilityVersion`, `getBuild`, `getCapabilityVersion`, `listBuildLogs`, `listBuilds`, `listCapabilityVersions`, `retryBuild`

**主实现表（字段唯一来源02，不在此复制字段定义）**：`capability.capability_versions`, `build.build_jobs`, `build.build_artifacts`, `build.build_logs`

**内部协议实现/协作端口**：`CapabilityProductService.ListCapabilityVersions`, `CapabilityProductService.GetCapabilityVersion`, `CapabilityProductService.DeleteCapabilityVersion`, `BuildProductService.CreateBuild`, `BuildProductService.ListBuilds`, `BuildProductService.GetBuild`, `BuildProductService.ListBuildLogs`, `BuildProductService.RetryBuild`, `ClaimUsageReadback.ReadClaimUsage`, `CapabilityCoordination.ResolveBuildInput`, `CapabilityCoordination.ResolvePublisherContract`, `CapabilityCoordination.AcquireReference`, `CapabilityCoordination.BindReference`, `CapabilityCoordination.ReleaseReference`, `BuildCoordination.ReadArtifact`, `DomainInbox.Deliver`

**验证**：
- 在拥有方Go module执行go test ./...；使用实际typed DTO与decoder
- Cloud结构性/持久化/跨模块修改后npm run verify:local:full；仅普通源码改变用verify:local
- 该命令只作实施后要求；本轮没有执行新产品实现测试

**完成判定**：
- 真实Storage+Registry+BuildKit隔离集成；worker重启、push已成但丢响应、注册ACK丢失
- 输出digest/descriptor可被后续Runtime读取；不以fixture假Build完成

### W10 Runtime描述执行与控制

**协调Owner：** Runtime Control；**参与Owner：** runtime_control。
**F范围：** F08, F09, F10, F11, F16；**开始依赖：** W02, W07；**验收依赖：** W11。

**现有来源（当前checkout定位，不表示全部要改；原始source snapshot见09）**：
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/control-plane/internal/server/workspace_application_deployment.go`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/packages/contracts/go/workspace_application_runtime.go`

**拟写入位置（未来实施，尚未创建的路径也明确列出）**：
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/runtime-control/internal/deployments`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/runtime-control/internal/configuration`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/runtime-control/internal/access`

**必须交付**：
- Reserve稳定实例身份；完整descriptor贯通部署/模型reload/retire
- 应用原登录与owner-only应用凭据reveal沿现行能力；不新增强制SSO
- 数据兼容/单写挂载、健康/已应用配置与实际资源改配后的恢复
- 以15及reference继承盘点为基线，声明保留/提取/新增的真实caller和writer；优先复用现行owner decoder/规则，不按新表逐字段重写一套业务

**主实现表（字段唯一来源02，不在此复制字段定义）**：`runtime_control.runtime_instances`, `runtime_control.runtime_actions`

**内部协议实现/协作端口**：`RuntimeCoordination.Reserve`, `RuntimeCoordination.Deploy`, `RuntimeCoordination.ReloadModels`, `RuntimeCoordination.ReadRuntime`, `RuntimeCoordination.Retire`, `RuntimePlanChangeControl.RestoreAfterResourceChange`

**验证**：
- 在拥有方Go module执行go test ./...；使用实际typed DTO与decoder
- Cloud结构性/持久化/跨模块修改后npm run verify:local:full；仅普通源码改变用verify:local
- 该命令只作实施后要求；本轮没有执行新产品实现测试
- 实施前现有基线（不证明新功能）：rtk proxy go -C /Users/huangrende/Documents/ChatGPT/opl-cloud/packages/contracts/go test ./... -count=1
- 实施前现有基线（不证明新功能）：rtk proxy go -C /Users/huangrende/Documents/ChatGPT/opl-cloud/services/control-plane test ./internal/server -run '^(TestApplicationRevisionAdmissionHTTP|TestWorkspaceApplicationDeploymentHTTPReplayRetainsAcceptedCredentials|TestWorkspaceApplicationBindingHTTPReplacementAndPreflight)$' -count=1

**完成判定**：
- 非默认端口/Secret/依赖应用真实运行；无应用legacy为not_applicable而不是伪ready
- 配置版本/镜像/资源/entry/readback一致才确认

### W11 Local-Docker执行/存储/路由资格能力

**协调Owner：** Fabric Local-Docker；**参与Owner：** fabric。
**F范围：** F08, F09, F10, F11, F12, F13, F16, F17；**开始依赖：** W02；**验收依赖：** 按本任务边界。

**现有来源（当前checkout定位，不表示全部要改；原始source snapshot见09）**：
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/fabric/internal/fabric/local_docker_provider.go`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/fabric/internal/fabric/provider_port.go`

**拟写入位置（未来实施，尚未创建的路径也明确列出）**：
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/fabric/internal/fabric/local_docker_provider.go`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/fabric/internal/fabric`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/fabric/internal/http`

**必须交付**：
- 适配类型化执行计划、资源/Secret/挂载/运行、D17允许转换及实际quota
- provider-side fence/conditional revision与实际路由读回，旧epoch不可生效
- Linux存储/权限前提不满足明确失败，不把Mac Docker Desktop冒充Local资格
- 以15及reference继承盘点为基线，声明保留/提取/新增的真实caller和writer；优先复用现行owner decoder/规则，不按新表逐字段重写一套业务

**主实现表（字段唯一来源02，不在此复制字段定义）**：`fabric.resource_sets`, `fabric.resources`, `fabric.attachments`, `fabric.secret_bindings`, `fabric.resource_actions`, `fabric.route_bindings`, `fabric.route_switches`

**内部协议实现/协作端口**：`FabricCoordination.AdmitResources`, `FabricCoordination.EnsureResources`, `FabricCoordination.ResizeResources`, `FabricCoordination.RenewResources`, `FabricCoordination.SuspendResources`, `FabricCoordination.ResumeResources`, `FabricCoordination.DeleteResources`, `FabricCoordination.ReadResources`, `FabricCoordination.BindSecret`, `FabricRuntimeExecution.ReadApplicationCredentials`, `FabricRuntimeExecution.StartRuntime`, `FabricRuntimeExecution.StopRuntime`, `FabricRuntimeExecution.ReloadRuntime`, `FabricRuntimeExecution.ObserveRuntime`, `FabricRouteExecution.FenceRouteEpoch`, `FabricRouteExecution.ActivateRoute`, `FabricRouteExecution.ObserveRoute`, `FabricRouteExecution.RollbackRoute`, `FabricPlanTransitionReadback.ReadApprovedPlanTransition`, `FabricPlanTransitionReadback.ReadExecutionPlan`

**验证**：
- 在拥有方Go module执行go test ./...；使用实际typed DTO与decoder
- Cloud结构性/持久化/跨模块修改后npm run verify:local:full；仅普通源码改变用verify:local
- 该命令只作实施后要求；本轮没有执行新产品实现测试

**完成判定**：
- Linux隔离集成：真实volume数据、重启、升级失败、route丢响应、unknown不重发
- 零金额路径无Gateway动作，资源-only不伪造应用

### W12 Tencent/TKE预付执行与计划变更

**协调Owner：** Fabric Tencent；**参与Owner：** fabric。
**F范围：** F08, F10, F11, F12, F13, F17；**开始依赖：** W02；**验收依赖：** 按本任务边界。

**现有来源（当前checkout定位，不表示全部要改；原始source snapshot见09）**：
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/fabric/internal/fabric/tencent_provider.go`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/fabric/internal/fabric/tencent_provider_storage.go`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/fabric/cmd/opl-tencent-provisioner`

**拟写入位置（未来实施，尚未创建的路径也明确列出）**：
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/fabric/internal/fabric`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/fabric/cmd/opl-tencent-provisioner`

**必须交付**：
- 报价前固定已资格的in-place或目标pool+原CBS重绑策略；预付包月，禁止POSTPAID_BY_HOUR
- SDK询价/允许机型/异步变更/描述读回、TKE排空/恢复、磁盘/文件系统验证
- 不支持缩容/性能类别/原子路由能力明确拒绝；ordinary CI无真钱采购/销毁
- 以15及reference继承盘点为基线，声明保留/提取/新增的真实caller和writer；优先复用现行owner decoder/规则，不按新表逐字段重写一套业务

**内部协议实现/协作端口**：`FabricCoordination.AdmitResources`, `FabricCoordination.EnsureResources`, `FabricCoordination.ResizeResources`, `FabricCoordination.RenewResources`, `FabricCoordination.SuspendResources`, `FabricCoordination.ResumeResources`, `FabricCoordination.DeleteResources`, `FabricCoordination.ReadResources`, `FabricCoordination.BindSecret`, `FabricRuntimeExecution.ReadApplicationCredentials`, `FabricRuntimeExecution.StartRuntime`, `FabricRuntimeExecution.StopRuntime`, `FabricRuntimeExecution.ReloadRuntime`, `FabricRuntimeExecution.ObserveRuntime`, `FabricRouteExecution.FenceRouteEpoch`, `FabricRouteExecution.ActivateRoute`, `FabricRouteExecution.ObserveRoute`, `FabricRouteExecution.RollbackRoute`, `FabricPlanTransitionReadback.ReadApprovedPlanTransition`, `FabricPlanTransitionReadback.ReadExecutionPlan`

**验证**：
- 在拥有方Go module执行go test ./...；使用实际typed DTO与decoder
- Cloud结构性/持久化/跨模块修改后npm run verify:local:full；仅普通源码改变用verify:local
- 该命令只作实施后要求；本轮没有执行新产品实现测试

**完成判定**：
- SDK请求和原请求读回边界使用typed tests；真正变更仅W29授权Instance验证
- 不能把SDK RequestId或kubectl接受当资源完成

### W13 BFF和Console基础接入

**协调Owner：** Console/BFF；**参与Owner：** bff, console, workspace。
**F范围：** F01, F17；**开始依赖：** W01, W03；**验收依赖：** 按本任务边界。

**现有来源（当前checkout定位，不表示全部要改；原始source snapshot见09）**：
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/apps/console-ui/src/app/console-router.ts`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/apps/console-ui/src/layout/ConsoleShell.tsx`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/apps/console-ui/src/api/auth-api.ts`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/apps/console-ui/src/app/use-console-controller.ts`

**拟写入位置（未来实施，尚未创建的路径也明确列出）**：
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/apps/console-bff/go.mod`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/apps/console-bff/go.sum`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/apps/console-bff/cmd/server`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/apps/console-bff/internal/session`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/apps/console-bff/internal/routes`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/apps/console-ui/src/api`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/apps/console-ui/src/layout/ConsoleShell.tsx`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/apps/console-ui/src/app/console-router.ts`

**必须交付**：
- 同源REST/session/CSRF/Origin→typed Owner RPC；Operation按owner路径路由
- 保留当前React/TypeScript/tokens；按11导航和状态/响应式/焦点，不按Domain铺菜单
- secret no-store与内存清理；所有异步读取遵守owner提示，不自行认定成功
- 通用Operation聚合只是BFF路由，不归Workspace数据writer；按显式owner访问唯一后端，不遍历库/服务猜归属

**主实现API（沿用03的唯一Owner，不是改变数据写权）**：`getOperation`

**内部协议实现/协作端口**：`WorkspaceProductService.GetOperation`

**前端消费API**：`acceptInvitation`, `bindTenantWallet`, `createTenant`, `getAdminTenant`, `getLoginContext`, `getOperation`, `getReceipt`, `getSession`, `getTenant`, `inviteMember`, `listAdminOperations`, `listAuditEvents`, `listInvitations`, `listMembers`, `listQualifications`, `listReceipts`, `login`, `logout`, `reconcileOperation`, `removeMember`, `revokeInvitation`, `updateMemberRole`

**验证**：
- go -C /Users/huangrende/Documents/ChatGPT/opl-cloud/apps/console-bff test ./...
- npm run typecheck
- npm run test:browser:console-owner-reads
- 真实BFF会话/跨Tenant/退出缓存清理测试

**完成判定**：
- BFF不写业务表、不拥有Saga/钱包；未知Owner拒绝不遍历猜测
- 旧/api与新/api/v2按明确切换，不请求失败再fallback

### W14 智能体/上传/构建/目录前端

**协调Owner：** Console；**参与Owner：** console。
**F范围：** F02, F03, F04, F05, F06；**开始依赖：** W13；**验收依赖：** W06, W07, W08, W09。

**现有来源（当前checkout定位，不表示全部要改；原始source snapshot见09）**：
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/apps/console-ui/src/pages/CustomerPages.tsx`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/apps/console-ui/src/pages/AdminPages.tsx`

**拟写入位置（未来实施，尚未创建的路径也明确列出）**：
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/apps/console-ui/src/pages/agents`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/apps/console-ui/src/app/agent-controllers`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/apps/console-ui/src/api/capability-api.ts`

**必须交付**：
- 按04/11实现Agent目录、版本、分组、上传向导、构建日志/重试及publisher表单
- fixtures只用于并行开发；最后换真实BFF读写并验证关闭/刷新恢复
- 目录下架/物理清除文案不混，技术详情只给有权限的角色
- 本包首条可验收切片仅为上传/构建/唯一版本/Console展示；资源套餐与价格目录属于W06及第二条购买链。全量F03目录体验仍须W06再验收，不拿首条切片宣称W14全完成。

**前端消费API**：`archiveNamespace`, `archivePackage`, `completeUpload`, `createBuild`, `createComputePlan`, `createNamespace`, `createPackage`, `createPricePolicyVersion`, `createPublisherNamespace`, `createRefundPolicyVersion`, `createRetentionPolicyVersion`, `createStoragePlan`, `createUpload`, `createUploadPart`, `deleteCapabilityVersion`, `getBuild`, `getBuildRuntimePolicy`, `getCapabilityVersion`, `getOperation`, `getPackage`, `getPackageVersion`, `getUpload`, `listBuildLogs`, `listBuilds`, `listCapabilityVersions`, `listComputePlans`, `listModels`, `listNamespaces`, `listPackageVersions`, `listPackages`, `listPricePolicyVersions`, `listPublisherNamespaces`, `listRefundPolicyVersions`, `listRetentionPolicyVersions`, `listRuntimeVersions`, `listStoragePlans`, `listWebuiVersions`, `publishOfficialPackage`, `registerRuntimeVersion`, `registerWebuiVersion`, `retryBuild`, `revokePublisherNamespace`, `setBuildRuntimePolicy`, `setComputePlanAvailability`, `setRuntimeVersionStatus`, `setStoragePlanAvailability`, `setWebuiVersionStatus`, `updateNamespace`, `updatePackage`

**验证**：
- 新增typed controller/model与browser用例
- npm run typecheck
- 按F02-F06正常/错误/重复/越权/刷新验收

**完成判定**：
- 首包与新版本都可用；选择WebUI确实进入产物
- 不把字段存在、静态原型或传输100%当构建完成
- 首切片端到端经真实BFF与Owner回读，上传分片/Build进度/ready版本/权限/刷新可复现；全包仍须W06价格目录与其余F03页面独立验收。

### W15 准确报价与Agent+套餐Launch闭环

**协调Owner：** Workspace（协调）/Catalog；**参与Owner：** workspace, resource_catalog。
**F范围：** F07, F08；**开始依赖：** W04, W05, W06, W09, W10, W11；**验收依赖：** 按本任务边界。

**现有来源（当前checkout定位，不表示全部要改；原始source snapshot见09）**：
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/control-plane/internal/server/routes_workspace_launch.go`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/control-plane/internal/server/workspace_launch_service.go`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/control-plane/internal/server/workspace_launch_reconciler.go`

**拟写入位置（未来实施，尚未创建的路径也明确列出）**：
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/workspace/internal/admission`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/workspace/internal/launch`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/resource-catalog/internal/quotes`

**必须交付**：
- 只读准入→固定quote→CAS接受→原单扣款→Key→资源/挂载→完整运行→route确认→Ledger
- 新入口必须有Agent；旧resource_only不能被默认Agent暗改
- 每阶段原身份/原单/epoch持久化，已接受报价不因执行耗时重新计价
- 以15及reference继承盘点为基线，声明保留/提取/新增的真实caller和writer；优先复用现行owner decoder/规则，不按新表逐字段重写一套业务

**主实现API（沿用03的唯一Owner，不是改变数据写权）**：`createQuote`, `createWorkspace`, `getQuote`, `getWorkspace`, `listWorkspaces`

**主实现表（字段唯一来源02，不在此复制字段定义）**：`workspace.workspaces`, `workspace.saga_steps`, `resource_catalog.quotes`, `resource_catalog.quote_items`, `workspace.supplemental_charges`

**内部协议实现/协作端口**：`ResourceCatalogProductService.CreateQuote`, `ResourceCatalogProductService.GetQuote`, `WorkspaceProductService.CreateWorkspace`, `WorkspaceProductService.ListWorkspaces`, `WorkspaceProductService.GetWorkspace`, `ClaimUsageReadback.ReadClaimUsage`, `CatalogCoordination.AcceptQuote`, `WorkspaceAdmission.CheckAdmission`, `DomainInbox.Deliver`

**验证**：
- 在拥有方Go module执行go test ./...；使用实际typed DTO与decoder
- Cloud结构性/持久化/跨模块修改后npm run verify:local:full；仅普通源码改变用verify:local
- 该命令只作实施后要求；本轮没有执行新产品实现测试
- 本地真实服务链+外部权威fixture：余额不足、扣款丢响应、资源/receipt不确定、restart
- 同一次请求串API→Owner→DB→下一Owner→实际应用响应
- 实施前现有基线（不证明新功能）：rtk proxy go -C /Users/huangrende/Documents/ChatGPT/opl-cloud/services/control-plane test ./internal/server -run '^(TestPricingCatalogLookupRequiresExactAcceptedVersion|TestWorkspaceLaunchMonthlyPreflightRunsBeforeDebitAndProviderStages|TestWorkspaceLaunchDebitAuthoritativeReadbackClassification|TestWorkspaceLaunchResourceOnlyReconcilerCompletesWithoutApplicationFacts)$' -count=1

**完成判定**：
- 一个Workspace一次原单；ready来自实际当前应用而非资源开通
- 失败收尾/unknown/退款分开展示，拒绝跨Tenant与伪价格

### W16 部署向导与Workspace页面

**协调Owner：** Console；**参与Owner：** console。
**F范围：** F07, F08, F09, F10, F16；**开始依赖：** W13；**验收依赖：** W15, W17, W25。

**现有来源（当前checkout定位，不表示全部要改；原始source snapshot见09）**：
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/apps/console-ui/src/app/use-workspace-launch-controller.ts`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/apps/console-ui/src/app/workspace-launch-controller-model.ts`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/apps/console-ui/src/api/workspaces-api.ts`

**拟写入位置（未来实施，尚未创建的路径也明确列出）**：
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/apps/console-ui/src/pages/workspaces`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/apps/console-ui/src/app/workspace-launch-controller-model.ts`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/apps/console-ui/src/app/use-workspace-launch-controller.ts`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/apps/console-ui/src/api/workspaces-api.ts`

**必须交付**：
- 版本/套餐/模型→固定报价→确认→真实进度；修改选项使旧quote失效
- 详情并列权益/资源/应用/资金结果，owner凭据一次性查看
- 原资源adopt和已部署legacy应用有独立入口，不重购

**前端消费API**：`adoptWorkspace`, `createQuote`, `createWorkspace`, `getCapabilityVersion`, `getDeployment`, `getOperation`, `getQuote`, `getSubscription`, `getWallet`, `getWorkspace`, `getWorkspaceAccess`, `getWorkspaceModels`, `listCapabilityVersions`, `listComputePlans`, `listDeployments`, `listModels`, `listRuntimeVersions`, `listStoragePlans`, `listWebuiVersions`, `listWorkspaceTransactions`, `listWorkspaces`, `revealWorkspaceApplicationCredentials`, `rollbackWorkspace`, `updateWorkspaceModels`, `updateWorkspaceVersion`

**验证**：
- npm run test:browser:workspace-lifecycle
- npm run typecheck
- 新页面controller与浏览器丢响应/刷新/窄屏/键盘测试

**完成判定**：
- 按钮条件、错误和场景与04/11完全同词
- 生产UI不执行价格算法，准确显示最多6位微美元

### W17 应用配置、版本切换与回滚

**协调Owner：** Workspace/Runtime Control；**参与Owner：** workspace, runtime_control, fabric。
**F范围：** F09, F10；**开始依赖：** W10, W11, W15；**验收依赖：** 按本任务边界。

**现有来源（当前checkout定位，不表示全部要改；原始source snapshot见09）**：
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/control-plane/internal/server/workspace_application_deployment.go`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/control-plane/internal/server/workspace_gateway.go`

**拟写入位置（未来实施，尚未创建的路径也明确列出）**：
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/workspace/internal/deployments`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/workspace/internal/configuration`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/runtime-control/internal/deployments`

**必须交付**：
- API→完整descriptor/引用保护→独立实例验证→Fence/Activate/Observe→本域CAS选择
- 失败回滚须数据兼容/原route证据，不能只改active指针
- 配置只更新允许模型；原购买历史/数据/Key边界保留
- 以15及reference继承盘点为基线，声明保留/提取/新增的真实caller和writer；优先复用现行owner decoder/规则，不按新表逐字段重写一套业务

**主实现API（沿用03的唯一Owner，不是改变数据写权）**：`getDeployment`, `getWorkspaceAccess`, `getWorkspaceModels`, `listDeployments`, `revealWorkspaceApplicationCredentials`, `rollbackWorkspace`, `updateWorkspaceModels`, `updateWorkspaceVersion`

**主实现表（字段唯一来源02，不在此复制字段定义）**：`workspace.deployments`, `workspace.model_configurations`

**内部协议实现/协作端口**：`WorkspaceProductService.GetWorkspaceAccess`, `WorkspaceProductService.GetWorkspaceModels`, `WorkspaceProductService.UpdateWorkspaceModels`, `WorkspaceProductService.ListDeployments`, `WorkspaceProductService.GetDeployment`, `WorkspaceProductService.UpdateWorkspaceVersion`, `WorkspaceProductService.RollbackWorkspace`, `WorkspaceProductService.RevealWorkspaceApplicationCredentials`

**验证**：
- 在拥有方Go module执行go test ./...；使用实际typed DTO与decoder
- Cloud结构性/持久化/跨模块修改后npm run verify:local:full；仅普通源码改变用verify:local
- 该命令只作实施后要求；本轮没有执行新产品实现测试
- 真实HTTP根路径/资产/API/SSE/cookie隔离与数据持久化
- 晚到worker/旧epoch/不同target、data不可逆和多写挂载拒绝
- 实施前现有基线（不证明新功能）：rtk proxy go -C /Users/huangrende/Documents/ChatGPT/opl-cloud/services/control-plane test ./internal/server -run '^(TestApplicationRevisionAdmissionHTTP|TestWorkspaceApplicationDeploymentHTTPReplayRetainsAcceptedCredentials|TestWorkspaceApplicationBindingHTTPReplacementAndPreflight)$' -count=1

**完成判定**：
- 一个确认选中部署，配置appliedVersion与实际一致
- 没有隐式自动升级、重新购买或新SSO

### W18 周期、显式续费授权和到期停用

**协调Owner：** Workspace；**参与Owner：** workspace, gateway, fabric。
**F范围：** F12；**开始依赖：** W04, W05, W15；**验收依赖：** 按本任务边界。

**现有来源（当前checkout定位，不表示全部要改；原始source snapshot见09）**：
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/control-plane/internal/server/workspace_renewal.go`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/control-plane/internal/server/monthly_billing.go`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/control-plane/internal/server/renewal_worker.go`

**拟写入位置（未来实施，尚未创建的路径也明确列出）**：
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/workspace/internal/subscriptions`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/workspace/internal/renewal`

**必须交付**：
- 保留source billingAnchorDay/原paidThrough/自动授权；周期唯一资金义务
- 到期不免费运行；原单/原资源续期回读后提交新周期
- 手动/自动/恢复worker竞争同一义务，不因session过期丢原授权
- 以15及reference继承盘点为基线，声明保留/提取/新增的真实caller和writer；优先复用现行owner decoder/规则，不按新表逐字段重写一套业务

**主实现API（沿用03的唯一Owner，不是改变数据写权）**：`getSubscription`, `renewWorkspace`, `updateRenewalSettings`

**主实现表（字段唯一来源02，不在此复制字段定义）**：`workspace.subscriptions`, `workspace.subscription_periods`

**内部协议实现/协作端口**：`WorkspaceProductService.RenewWorkspace`, `WorkspaceProductService.GetSubscription`, `WorkspaceProductService.UpdateRenewalSettings`, `WorkspaceAuthorizationReadback.ReadRenewalConsent`

**验证**：
- 在拥有方Go module执行go test ./...；使用实际typed DTO与decoder
- Cloud结构性/持久化/跨模块修改后npm run verify:local:full；仅普通源码改变用verify:local
- 该命令只作实施后要求；本轮没有执行新产品实现测试
- Jan31→Feb28→Mar31及闰年；窗口已过拒绝；source纳秒不回写
- 人工/自动并发、资金unknown、停用后恢复、provider资源回收
- 实施前现有基线（不证明新功能）：rtk proxy go -C /Users/huangrende/Documents/ChatGPT/opl-cloud/services/control-plane test ./internal/server -run '^(TestWorkspaceRenewalConcurrentWorkersClaimOnce|TestWorkspaceRenewalUsesOneDebitStableProviderIDsAndOneReceipt|TestWorkspaceDeleteRefundRecoversLostResponseWithoutSecondDispatch|TestWorkspaceDeleteRefundStatusIsReportedSeparatelyFromDeletion)$' -count=1

**完成判定**：
- 一周期最多一原单；不偷偷从now重新开始一月
- 关闭自动续费只影响未来未接受义务

### W19 D17套餐变更与补差账务

**协调Owner：** Workspace（协调）/Catalog；**参与Owner：** workspace, resource_catalog, gateway, fabric, runtime_control, ledger。
**F范围：** F11, F12, F13；**开始依赖：** W06, W11, W12, W15, W18；**验收依赖：** 按本任务边界。

**现有来源（当前checkout定位，不表示全部要改；原始source snapshot见09）**：
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/fabric/internal/fabric/provider_port.go`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/control-plane/internal/server/workspace_renewal.go`

**拟写入位置（未来实施，尚未创建的路径也明确列出）**：
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/workspace/internal/plan_changes`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/resource-catalog/internal/plan_change_pricing`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/gateway-integration/internal/settlement`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/fabric/internal/fabric`

**必须交付**：
- PlanChange第一类实体：立即upgrade与scheduled downgrade区分初次/执行Operation
- 固定T、canonical毫秒、单次ceil、原E不变；下期目标价及取消/锁定边界按13
- supplement失败全退及成功后T..E未用退款，与base720分开；原单防超额/unknown
- 复用W18 worker执行due计划，不新建scheduler服务或资金预占钱包
- 以15及reference继承盘点为基线，声明保留/提取/新增的真实caller和writer；优先复用现行owner decoder/规则，不按新表逐字段重写一套业务

**主实现API（沿用03的唯一Owner，不是改变数据写权）**：`cancelPlanChange`, `getPlanChange`, `listPlanChanges`, `resizeWorkspace`

**主实现表（字段唯一来源02，不在此复制字段定义）**：`workspace.plan_changes`, `workspace.subscription_period_obligations`

**内部协议实现/协作端口**：`WorkspaceProductService.ResizeWorkspace`, `WorkspaceProductService.ListPlanChanges`, `WorkspaceProductService.GetPlanChange`, `WorkspaceProductService.CancelPlanChange`, `WorkspacePlanChangeReadback.ReadSubscriptionPlanState`, `WorkspacePlanChangeReadback.ReadPlanChange`, `WorkspacePlanChangeReadback.ReadNextPeriodObligation`, `WorkspacePlanChangeReadback.ReadPlanChangeFailure`, `GatewayPlanChangeSettlement.DebitSupplement`, `GatewayPlanChangeSettlement.DebitScheduledPeriod`, `GatewayPlanChangeSettlement.RefundFailure`, `GatewayPlanChangeSettlement.RefundSupplementOnDeletion`, `RuntimePlanChangeControl.RestoreAfterResourceChange`, `LedgerPlanChangeEvidence.AppendPlanChangeReceipt`, `LedgerPlanChangeEvidence.AppendPlanChangeRefundReceipt`, `DomainInbox.Deliver`

**验证**：
- 在拥有方Go module执行go test ./...；使用实际typed DTO与decoder
- Cloud结构性/持久化/跨模块修改后npm run verify:local:full；仅普通源码改变用verify:local
- 该命令只作实施后要求；本轮没有执行新产品实现测试
- 重放13及全部D17数值/并发/PG纳秒进位/原单退款用例
- 实施前现有基线（不证明新功能）：rtk proxy go -C /Users/huangrende/Documents/ChatGPT/opl-cloud/services/control-plane test ./internal/server -run '^(TestWorkspaceRenewalConcurrentWorkersClaimOnce|TestWorkspaceRenewalUsesOneDebitStableProviderIDsAndOneReceipt|TestWorkspaceDeleteRefundRecoversLostResponseWithoutSecondDispatch|TestWorkspaceDeleteRefundStatusIsReportedSeparatelyFromDeletion)$' -count=1

**完成判定**：
- 20→40剩半期补10；31天例子19.354839；资源+运行确认才applied
- scheduled当前不扣/不减；无下期授权awaiting_payment；future paid/mixed/shrink拒绝清楚

### W20 删除、base与supplement原单结算

**协调Owner：** Workspace（协调）；**参与Owner：** workspace, gateway, fabric, ledger。
**F范围：** F13；**开始依赖：** W04, W05, W15, W18；**验收依赖：** W19。

**现有来源（当前checkout定位，不表示全部要改；原始source snapshot见09）**：
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/control-plane/internal/server/workspace_delete.go`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/control-plane/internal/server/workspace_delete_refund.go`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/control-plane/internal/server/wallet_adjustment.go`

**拟写入位置（未来实施，尚未创建的路径也明确列出）**：
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/workspace/internal/deletion`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/gateway-integration/internal/settlement`

**必须交付**：
- 持久删除意图→逐资源确切absence→删除receipt→每个原单独立退款
- base保留原policy；已applied升级supplement用自身coverage，不合并当整月款
- 与续费/plan change/rotation串行，不撤销用户保留Key，不删Package/Build历史
- 以15及reference继承盘点为基线，声明保留/提取/新增的真实caller和writer；优先复用现行owner decoder/规则，不按新表逐字段重写一套业务

**主实现API（沿用03的唯一Owner，不是改变数据写权）**：`deleteWorkspace`, `getWorkspaceDeletion`

**内部协议实现/协作端口**：`WorkspaceProductService.DeleteWorkspace`, `WorkspaceProductService.GetWorkspaceDeletion`

**验证**：
- 在拥有方Go module执行go test ./...；使用实际typed DTO与decoder
- Cloud结构性/持久化/跨模块修改后npm run verify:local:full；仅普通源码改变用verify:local
- 该命令只作实施后要求；本轮没有执行新产品实现测试
- 资源已删丢响应、原身份/receipt错配、refund unknown/超额、窗口/退款上界
- UI同时能显示已删+退款待确认；没有手工set success接口
- 实施前现有基线（不证明新功能）：rtk proxy go -C /Users/huangrende/Documents/ChatGPT/opl-cloud/services/control-plane test ./internal/server -run '^(TestWorkspaceRenewalConcurrentWorkersClaimOnce|TestWorkspaceRenewalUsesOneDebitStableProviderIDsAndOneReceipt|TestWorkspaceDeleteRefundRecoversLostResponseWithoutSecondDispatch|TestWorkspaceDeleteRefundStatusIsReportedSeparatelyFromDeletion)$' -count=1

**完成判定**：
- 无证据不退款，已删除不等于退款到账
- 不靠列表未找到或钱包余额差证明结果

### W21 Tenant开通、暂停/重新启用及删除恢复

**协调Owner：** CloudIdentity（协调）；**参与Owner：** tenant, gateway, workspace。
**F范围：** F01, F15；**开始依赖：** W03, W04, W15, W20；**验收依赖：** 按本任务边界。

**现有来源（当前checkout定位，不表示全部要改；原始source snapshot见09）**：
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/control-plane/internal/server/routes_admin.go`

**拟写入位置（未来实施，尚未创建的路径也明确列出）**：
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/gateway-integration/internal/tenant_lifecycle`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/workspace/internal/tenant_actions`

**必须交付**：
- 创建/绑定账单主体与个人登录分开；暂停权限及子Workspace结果可追踪
- reenable只恢复原暂停且仍已付/原资源存在对象；删除后15天恢复是独立操作
- 删除资产转平台私有托管，不公开、不自动退款全部余额或重购资源
- 以15及reference继承盘点为基线，声明保留/提取/新增的真实caller和writer；优先复用现行owner decoder/规则，不按新表逐字段重写一套业务

**主实现API（沿用03的唯一Owner，不是改变数据写权）**：`bindTenantWallet`, `createTenant`, `deleteTenant`, `getAdminTenant`, `getTenantAssetCustody`, `getTenantLifecycleOperation`, `listTenants`, `reenableTenant`, `restoreTenant`, `suspendTenant`

**内部协议实现/协作端口**：`TenantProductService.ListTenants`, `TenantProductService.CreateTenant`, `TenantProductService.GetAdminTenant`, `TenantProductService.DeleteTenant`, `TenantProductService.BindTenantWallet`, `TenantProductService.SuspendTenant`, `TenantProductService.RestoreTenant`, `TenantProductService.GetTenantAssetCustody`, `TenantProductService.ReenableTenant`, `TenantProductService.GetTenantLifecycleOperation`, `TenantWorkspaceCoordination.SuspendTenantWorkspaces`, `TenantWorkspaceCoordination.DeleteTenantWorkspaces`, `TenantWorkspaceCoordination.ResumeTenantWorkspaces`, `DomainInbox.Deliver`

**验证**：
- 在拥有方Go module执行go test ./...；使用实际typed DTO与decoder
- Cloud结构性/持久化/跨模块修改后npm run verify:local:full；仅普通源码改变用verify:local
- 该命令只作实施后要求；本轮没有执行新产品实现测试
- 暂停后直接reenable不要求restoreUntil；误恢复/过期/子任务unknown
- 成员失权、生效窗口及账户历史权限不放大
- 实施前现有基线（不证明新功能）：rtk proxy go -C /Users/huangrende/Documents/ChatGPT/opl-cloud/services/control-plane test ./internal/server -run '^(TestDelegatedCredentialNeverPersistsOrLeaks|TestAccountDisableRevokesSessionCredential|TestGatewayKeyOwnership|TestCloudAdminCanRevealOnlyOwnGatewayKey)$' -count=1

**完成判定**：
- Tenant权限和各Workspace运行结果分开
- 没有跨Tenant资源管理和删除后假复活数据

### W22 费用、计划、续费/退款与Key前端

**协调Owner：** Console；**参与Owner：** console。
**F范围：** F11, F12, F13, F14；**开始依赖：** W13；**验收依赖：** W04, W18, W19, W20。

**现有来源（当前checkout定位，不表示全部要改；原始source snapshot见09）**：
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/apps/console-ui/src/app/use-billing-controller.ts`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/apps/console-ui/src/app/use-workspace-renewal-controller.ts`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/apps/console-ui/src/api/workspaces-api.ts`

**拟写入位置（未来实施，尚未创建的路径也明确列出）**：
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/apps/console-ui/src/pages/billing`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/apps/console-ui/src/app/plan-change-controller`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/apps/console-ui/src/api/plan-change-api.ts`

**必须交付**：
- 立即升配quote/进度、下期计划/取消/付款边界，原E和资金/资源状态分开
- 精确BigInt金额展示而非业务重算；31天19.354839不可隐藏实际更多小数
- 订阅/交易/Token各有原始来源；Key与应用密码只内存/离页清理

**前端消费API**：`cancelPlanChange`, `createGatewayKey`, `createQuote`, `deleteWorkspace`, `getOperation`, `getPlanChange`, `getQuote`, `getSubscription`, `getWallet`, `getWorkspace`, `getWorkspaceDeletion`, `getWorkspaceModels`, `listComputePlans`, `listGatewayKeys`, `listPlanChanges`, `listRechargeRecords`, `listStoragePlans`, `listUsage`, `listWorkspaceTransactions`, `renewWorkspace`, `resizeWorkspace`, `revealGatewayKey`, `revokeGatewayKey`, `updateRenewalSettings`

**验证**：
- npm run test:browser:billing
- npm run test:browser:gateway-usage
- D17真实BFF浏览器与320/390px回归

**完成判定**：
- 不把预约持久化当资源已降配；unknown不建议重新付款
- 取消/删除确认对象范围明确，旧单/新补差不混

### W23 管理员目录、Tenant与运维前端

**协调Owner：** Console；**参与Owner：** console。
**F范围：** F03, F15, F17；**开始依赖：** W13；**验收依赖：** W06, W07, W21, W24。

**现有来源（当前checkout定位，不表示全部要改；原始source snapshot见09）**：
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/apps/console-ui/src/pages/AdminPages.tsx`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/apps/console-ui/src/pages/OperatorRuntimeObservations.tsx`

**拟写入位置（未来实施，尚未创建的路径也明确列出）**：
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/apps/console-ui/src/pages/admin`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/apps/console-ui/src/app/admin-controllers`

**必须交付**：
- 完整publisher schema表单/有效namespace，资源/价格版本不得改写已接受快照
- Tenant子操作/访问恢复分离，技术证据仅授权详情
- 只读资格/操作查询，不能从Console dispatch生产部署或直接改状态

**前端消费API**：`bindTenantWallet`, `createComputePlan`, `createPricePolicyVersion`, `createPublisherNamespace`, `createRefundPolicyVersion`, `createRetentionPolicyVersion`, `createStoragePlan`, `createTenant`, `deleteTenant`, `getAdminTenant`, `getBuildRuntimePolicy`, `getOperation`, `getReceipt`, `getTenant`, `getTenantAssetCustody`, `getTenantLifecycleOperation`, `listAdminOperations`, `listAuditEvents`, `listComputePlans`, `listModels`, `listPricePolicyVersions`, `listPublisherNamespaces`, `listQualifications`, `listReceipts`, `listRefundPolicyVersions`, `listRetentionPolicyVersions`, `listRuntimeVersions`, `listStoragePlans`, `listTenants`, `listWebuiVersions`, `publishOfficialPackage`, `reconcileOperation`, `reenableTenant`, `registerRuntimeVersion`, `registerWebuiVersion`, `restoreTenant`, `revokePublisherNamespace`, `setBuildRuntimePolicy`, `setComputePlanAvailability`, `setRuntimeVersionStatus`, `setStoragePlanAvailability`, `setWebuiVersionStatus`, `suspendTenant`

**验证**：
- npm run test:browser:operator-account
- 新增admin目录/权限/危害确认browser tests

**完成判定**：
- 普通owner无platform_admin权限；无伪JSON表单
- 缺证据显示未验证，不用文案冒充资格通过

### W24 Owner运维、原操作恢复和可观测性

**协调Owner：** 各Owner/Console集成；**参与Owner：** workspace, tenant, ledger, bff。
**F范围：** F17；**开始依赖：** W03, W05；**验收依赖：** W15, W19, W20, W21。

**现有来源（当前checkout定位，不表示全部要改；原始source snapshot见09）**：
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/control-plane/internal/server/operational_alerts.go`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/control-plane/internal/server/routes_admin.go`

**拟写入位置（未来实施，尚未创建的路径也明确列出）**：
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/apps/console-bff/internal/operations`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/workspace/internal/operations`

**必须交付**：
- 每Owner依据原意图/readback实现Reconcile，BFF仅路由，不能统一setStatus
- 结构化request/operation/phase、unknown、队列/重试/陈旧观察告警和脱敏日志
- 恢复手册列精确前置/允许动作/不可逆边界，容量参数从实例配置

**主实现API（沿用03的唯一Owner，不是改变数据写权）**：`listAdminOperations`, `listAuditEvents`, `reconcileOperation`

**内部协议实现/协作端口**：`TenantProductService.ListAuditEvents`, `WorkspaceProductService.ListAdminOperations`, `WorkspaceProductService.ReconcileOperation`, `OwnerOperations.Read`, `OwnerOperations.Reconcile`

**验证**：
- 在拥有方Go module执行go test ./...；使用实际typed DTO与decoder
- Cloud结构性/持久化/跨模块修改后npm run verify:local:full；仅普通源码改变用verify:local
- 该命令只作实施后要求；本轮没有执行新产品实现测试
- 日志canary-secret不泄露；错误/告警只绑定脱敏身份
- 无个人凭据操作、越权Owner路由、过期readback拒绝

**完成判定**：
- 运营能知道真实失败点与唯一下一动作
- 无新全局事件总线/中央workflow/第二lock authority
- 管理员通用Operation列表必填owner仅查指定域；游标绑定Owner不可跨域复用。

### W25 逐域数据转换与迁移演练

**协调Owner：** 各数据Owner/Instance；**参与Owner：** tenant, capability, build, workspace, runtime_control, fabric, gateway, ledger。
**F范围：** F16；**开始依赖：** W00, W02；**验收依赖：** W09, W17, W19, W20, W21。

**现有来源（当前checkout定位，不表示全部要改；原始source snapshot见09）**：
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/control-plane/ent/schema/shared.go`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/control-plane/migrations`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/fabric/ent/schema`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/ledger/ent/schema`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/control-plane/internal/server/ent_state_store.go`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/control-plane/migrations/migrations.go`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/fabric/internal/fabric/ent_migrations`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/ledger/internal/ledger/ent_migrations`

**拟写入位置（未来实施，尚未创建的路径也明确列出）**：
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/tools/migration`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/services/workspace/internal/migration`

**必须交付**：
- 按09对每张源表/字段确定target或保留档案，不丢未知历史
- legacy_resource_only/legacy_application/legacy_import保留真实ID/原单/时间、未决义务/补差coverage
- 隔离副本试迁移、hash/金额/周期/权限对账、single-writer fence及回滚演练
- 逐原字段/typed payload建立目标Owner、转换、保留及拒绝映射；区分空库安装与客户数据转换；未纳入迁移的公告/sync/审计等保留原Owner不删
- 修改真实migration加载链：CP ent_state_store→migrations.Apply*；Fabric/Ledger各自internal Owner的ent_migrations。不得只改展示SQL。

**主实现API（沿用03的唯一Owner，不是改变数据写权）**：`adoptWorkspace`

**内部协议实现/协作端口**：`WorkspaceProductService.AdoptWorkspace`

**验证**：
- 只在隔离副本运行转换，生产数据仅Instance保护runner
- row/ID集合、金额/时间/quote/receipt/user权限等值和未决操作重放
- 实施前现有基线（不证明新功能）：rtk proxy go -C /Users/huangrende/Documents/ChatGPT/opl-cloud/services/control-plane test ./internal/server -run '^(TestDelegatedCredentialNeverPersistsOrLeaks|TestAccountDisableRevokesSessionCredential|TestGatewayKeyOwnership|TestCloudAdminCanRevealOnlyOwnGatewayKey)$' -count=1
- 实施前现有基线（不证明新功能）：rtk proxy go -C /Users/huangrende/Documents/ChatGPT/opl-cloud/services/control-plane test ./internal/server -run '^(TestWorkspaceRenewalConcurrentWorkersClaimOnce|TestWorkspaceRenewalUsesOneDebitStableProviderIDsAndOneReceipt|TestWorkspaceDeleteRefundRecoversLostResponseWithoutSecondDispatch|TestWorkspaceDeleteRefundStatusIsReportedSeparatelyFromDeletion)$' -count=1
- 实施前现有基线（不证明新功能）：rtk proxy go -C /Users/huangrende/Documents/ChatGPT/opl-cloud/services/control-plane test ./internal/server -run '^(TestApplicationRevisionAdmissionHTTP|TestWorkspaceApplicationDeploymentHTTPReplayRetainsAcceptedCredentials|TestWorkspaceApplicationBindingHTTPReplacementAndPreflight)$' -count=1

**完成判定**：
- 无假Build/Quote/重购；不做双写或失败时切旧接口
- 反向不保真就不宣称可回滚，原证据不能覆盖

### W26 真实业务链集成与浏览器验收

**协调Owner：** 集成负责人+各Owner；**参与Owner：** bff, console, workspace, capability, build, gateway, fabric, runtime_control, ledger。
**F范围：** F01, F02, F03, F04, F05, F06, F07, F08, F09, F10, F11, F12, F13, F14, F15, F16, F17；**开始依赖：** W09, W13, W15；**验收依赖：** W14, W16, W17, W19, W20, W21, W22, W23, W24, W25。

**现有来源（当前checkout定位，不表示全部要改；原始source snapshot见09）**：
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/tests/integration`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/tests/ui`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/tools/local-sub2api-authority-fixture.ts`

**拟写入位置（未来实施，尚未创建的路径也明确列出）**：
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/tests/integration`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/tests/ui`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/tools`

**必须交付**：
- 前端真实BFF→真实Owner DB/RPC→Local provider/应用；外部财务只用明确隔离authority fixture
- 以17F逐项执行正常/拒绝/丢响应/重启/重复/越权/并发/取消/恢复向量
- 性能记录实际实例输入和测量值，不创造50人/3分钟承诺

**验证**：
- npm run verify:local:full
- npm run test:browser:suite
- 持续运行应用/上传真实样例构建及金额账期cross-tests

**完成判定**：
- 功能、接口、DB、页面呈现同一结果；原型不替代浏览器真链
- 每项证据说明实测和未测，不将fixture等同真实钱/provider

### W27 可移植候选构建与CI契约演进

**协调Owner：** Cloud发布机制Owner；**参与Owner：** cloud。
**F范围：** F17；**开始依赖：** W01, W02；**验收依赖：** W09, W12, W21, W24, W26。

**现有来源（当前checkout定位，不表示全部要改；原始source snapshot见09）**：
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/Dockerfile`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/compose.yaml`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/deploy/portable`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/.github/workflows/build-opl-cloud-candidate.yml`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/.github/workflows/release-opl-cloud-image.yml`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/packages/contracts/opl-cloud-candidate-receipt-contract.json`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/packages/contracts/opl-cloud-distribution-contract.json`

**拟写入位置（未来实施，尚未创建的路径也明确列出）**：
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/.github/workflows/build-opl-cloud-candidate.yml`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/deploy/portable`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/packages/contracts/opl-cloud-candidate-receipt-contract.json`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/packages/contracts/opl-cloud-distribution-contract.json`

**必须交付**：
- 目标多服务制品纳入同一个Cloud Candidate/安装清单与同字节资格单位；不另起release authority
- 固定Cloud SHA及各参与Owner sourceSHA、镜像digests/平台、契约revision、配置schema、全部assets checksum；无latest/main输入
- 更新现有Candidate/qualification/release消费者与实例接收校验，历史v2单镜像Candidate可读但不是第二当前writer
- CI只有code/build/sandbox测试权限；构建Candidate不等于正式发布或Instance部署

**验证**：
- contracts/工具生成/拒绝反例与portable Compose一致检查
- 假造digest/遗漏组件/漂移source/schema拒绝；构建不需要生产环境
- 实施前现有基线（不证明新功能）：rtk proxy node --test /Users/huangrende/Documents/ChatGPT/opl-cloud/tests/tools/cloud-candidate-receipt.test.ts /Users/huangrende/Documents/ChatGPT/opl-cloud/tests/contracts/clean-host-qualification.test.ts

**完成判定**：
- 候选集合是不可变单位，任何组件变更必须新Candidate重新适用资格
- Release只提升已资格同字节，不rebuild；原发布者权限边界不变

### W28 干净Linux Local资格

**协调Owner：** Cloud/Local qualification；**参与Owner：** cloud, fabric。
**F范围：** F17；**开始依赖：** W27；**验收依赖：** 按本任务边界。

**现有来源（当前checkout定位，不表示全部要改；原始source snapshot见09）**：
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/.github/workflows/clean-host-qualification.yml`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/tools/local-workspace-qualification.ts`

**拟写入位置（未来实施，尚未创建的路径也明确列出）**：
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/.github/workflows/clean-host-qualification.yml`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/tools/local-workspace-qualification.ts`

**必须交付**：
- 固定Candidate在合格Linux安装，执行启动/存储/应用/生命周期与重启/数据保留
- 记录image/platform/config摘要与真实readback，失败不得包装PASS

**验证**：
- 干净Linux exact-candidate资格；不以Mac Desktop代替
- 本轮仅计划命令，后续实际执行再出receipt

**完成判定**：
- Local receipt绑定同一候选及provider条件
- 不买真实Tencent资源、不调用真实客户钱包

### W29 Instance/Tencent资格与受限业务实测

**协调Owner：** opl-instance-medopl Owner；**参与Owner：** instance。
**F范围：** F17；**开始依赖：** W28, W12, W19, W25；**验收依赖：** 按本任务边界。

**现有来源（当前checkout定位，不表示全部要改；原始source snapshot见09）**：
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/docs/runtime/release.md`

**拟写入位置（未来实施，尚未创建的路径也明确列出）**：
- `/Users/huangrende/Documents/ChatGPT/opl-instance-medopl/receipts`
- `/Users/huangrende/Documents/ChatGPT/opl-instance-medopl/.github/workflows`

**必须交付**：
- 在既有保护workflow/授权runner中采用同一Candidate和真实provider profile
- 若验收需要付费/采购/销毁，先有独立明确金额/资源/账户范围授权；Cloud本机不进生产私网
- 验证真实Agent使用、允许调配策略、升级/下期降配、原单读回与数据恢复/迁移样本

**验证**：
- Instance现有receipt validators、真实runtime/账务/provider读回
- 精确scope/hash/时间/cleanup；只测试获得授权的案例

**完成判定**：
- 与Local同候选字节；不能以公网404/Pod Running作完整合格
- 未拿授权或必要凭据则记录未执行，不产生假资格

### W30 生产客户逐批切换与旧writer退休

**协调Owner：** Instance+数据Owner；**参与Owner：** instance, workspace, gateway, fabric, ledger。
**F范围：** F16, F17；**开始依赖：** W25, W26, W29；**验收依赖：** 按本任务边界。

**现有来源（当前checkout定位，不表示全部要改；原始source snapshot见09）**：
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/docs/status.md`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/docs/roadmap.md`

**拟写入位置（未来实施，尚未创建的路径也明确列出）**：
- `/Users/huangrende/Documents/ChatGPT/opl-instance-medopl/receipts`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/docs/status.md`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/docs/roadmap.md`

**必须交付**：
- 按09 M0-M5：写屏障/终态增量/路由epoch/实际用户读回/分批接收
- 未决对象保持唯一原Owner，不先切新再失败fallback；新写后回退须可证明无损
- 业务/数据接收完成后退休旧writer；旧历史只读保留义务

**验证**：
- 每批数据/资金/资源/权限对账和真实浏览器故事
- Instance采用/回滚新不可变receipt

**完成判定**：
- 不丢原单/Key/数据，零重复收费；未完成批次明确保留
- 本步骤与Cloud正式发布不同，不从Cloud dispatch实例切换

### W31 同字节正式发布与文档收尾

**协调Owner：** Cloud发布Owner；**参与Owner：** cloud。
**F范围：** F17；**开始依赖：** W28, W29；**验收依赖：** 按本任务边界。

**现有来源（当前checkout定位，不表示全部要改；原始source snapshot见09）**：
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/.github/workflows/release-opl-cloud-image.yml`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/.github/workflows/release-opl-cloud-public-readback.yml`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/docs/runtime/release.md`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/docs/status.md`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/docs/roadmap.md`

**拟写入位置（未来实施，尚未创建的路径也明确列出）**：
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/.github/workflows/release-opl-cloud-image.yml`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/docs/status.md`
- `/Users/huangrende/Documents/ChatGPT/opl-cloud/docs/roadmap.md`

**必须交付**：
- 仅在明确正式发布授权后，由允许actor从main提升同一资格制品，不重建
- 实际公共制品/digest/资产回读；Cloud状态和外部Instance义务分别记录
- 实施后更新canonical文档，完成工作清单转历史，保留未完成gap
- fork代码按用户明确授权的Issue/PR策略合回上游；代码merge、Product Release和Instance采用分别验证，互不自动授权

**验证**：
- 当前release owner校验器与public artifact readback
- Local+Instance同候选证明；没有授权不dispatch

**完成判定**：
- 只有owner及RenDeHuang符合既有发布权限，original publisher不被替换
- 产品发布不要求所有客户迁移已完成；但声明的迁移兼容性须有W25/W29证据

## 5. 首批开发任务怎样落PR

1. W00只统一目标/实施分层和source snapshot，不先改运行逻辑。
2. W01把必要的契约及真实consumer类型接入现仓，先跑strict decoder/roundtrip/金额与epoch反例。
3. W02按01的部署单元建立可启动、可迁移、可查询operation的最小服务；同进程的tenant/gateway仍分别验证DB权限、事务与写入边界。
4. W03只接入首链会话/成员/服务授权，W05只完成Build所需证据；W07/08/09与W13/14首切片形成真实纵向闭环。W14中的资源价格UI依赖W06第二链；首切片不宣称W14全包完成。不要先同时搬完所有Control Plane方法。
5. 每个用例列入口与caller、唯一事实Owner及本域事务、字段来源/消息/校验/读回、unknown恢复、旧writer切换/退出和验证证据；同仓共享契约串行落一版，领域实现按互不冲突文件分工。只有用户要求才执行commit/push/PR。

## 6. 五方怎样协同而不再次定义业务

| 角色 | 任务开始前的输入 | 交给下一方的产物 | 拒收条件 |
|---|---|---|---|
| 产品 | 12/13和F故事 | 冻结结果/费用/数据后果与接受用例 | 仍要求研发决定补差/退款、删除范围 |
| 架构 | 01/06/09及W依赖 | 单writer/真实调用、跨域恢复与迁移边界 | 新框架/旁路状态/跨Owner DB权限 |
| 后端 | 02/03/proto/事件、对应W写集 | 真实API/RPC/DB/状态、可重放正反例 | 类型齐全但业务断点/unknown乱重试 |
| 前端/UX | 04/07/11及原型、真实DTO | 页面、表单、完整状态/恢复、真实BFF接入 | 只做静态UI或自造价格/成功状态 |
| QA/客户代表 | 同一个F故事与W接受标准 | 正常/失败/权限/重复/恢复录屏与证据 | 只能演示happy path，客户无法解释费用/数据结果 |

## 7. 工程验证分层与执行环境

规格脚本本轮可以运行；上面Go/npm/浏览器/qualification命令是实施后验收要求。当前没有新service实现时，不伪造go test通过或执行真钱测试补空白。

开发环境使用现有Go/TypeScript/PostgreSQL与已决定协议；各语言工具及构建器在W01锁版。服务端访问Secrets走已批准注入边界，配置/权限/仓库凭据由Instance提供真实部署值，不写进文档或Git。

外部资金测试先用明确隔离authority fixture，真实Gateway/provider资格只按W29限定授权。生产网络只能通过Instance保护runner；Cloud不得dispatch Instance部署。

## 8. 端点/任务覆盖与可开工判断

`checks/development_plan.json`由本任务书同源生成，列每个W/F/operationId/Owner表/内部RPC/代码落点及依赖；`checks/validate_development_plan.py`检查全部当前API唯一主实现、Owner表和内部RPC无漏项、17F覆盖、已存在路径、依赖无环和前端消费接口存在。数量从实际契约读取，不硬编码通过。

阶段完成要有真实交付物与对应证据；本任务书的规划状态不替代docs/status.md中的实际进展。完整开发方案已给出不代表这些工作包已经完成，正常实施/联调/Instance接受仍按08/09执行。
