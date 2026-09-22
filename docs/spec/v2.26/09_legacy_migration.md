# 09 旧产品路径与数据迁移

> F16唯一迁移规格。只定义后续实施动作；本轮没有读取生产数据库、创建仓库、发布、扣费或迁移任何客户资源。

## 1. 来源与目标，不混淆证据层

当前源码基线：one-person-lab-cloud main，50520e27a6b9a630eefdc2df7da3e5ec498d28a0（2026-09-20）。当前Console固定resource_only；Control Plane/Fabric/Ledger是三个现有进程/schema Owner。当前文档保留的Instance证据不能自动覆盖此SHA。

读取依据（绝对路径）：

- /Users/huangrende/Documents/ChatGPT/one-person-lab-cloud/apps/console-ui/src/app/workspace-launch-controller-model.ts:105
- /Users/huangrende/Documents/ChatGPT/one-person-lab-cloud/packages/contracts/go/workspace_provisioning.go:24
- /Users/huangrende/Documents/ChatGPT/one-person-lab-cloud/services/control-plane/ent/schema/shared.go:25
- /Users/huangrende/Documents/ChatGPT/one-person-lab-cloud/services/control-plane/internal/server/workspace_launch_provisioning_mode.go:22
- /Users/huangrende/Documents/ChatGPT/one-person-lab-cloud/services/control-plane/internal/server/workspace_delete_refund.go:405
- /Users/huangrende/Documents/ChatGPT/one-person-lab-cloud/packages/contracts/go/workspace_delete.go:650
- /Users/huangrende/Documents/ChatGPT/one-person-lab-cloud/services/ledger/internal/ledger/types.go:210

目标：客户选择Agent版本和套餐，Workspace Service编排完成可用部署。把现有职责逐领域移交，而不是清空重建、改ID、再次购买或由BFF兼容猜状态。

## 2. 按旧对象类型明确客户迁移体验

| 旧对象 | 导入事实 | 新界面 | 第一次新动作 | 绝不做 |
|---|---|---|---|---|
| 已购买resource_only，尚无应用 | 保留Workspace ID/原单/已付周期/compute/storage/attachment；capabilityVersionId/activeDeploymentId在DB为NULL、REST缺省，deliveryModel=legacy_resource_only | 显示“资源已开通，待部署应用”、原到期时间、原套餐 | 在已有资源上选择Agent并部署；报价类型为沿用资源，不执行debit/compute/storage购买阶段 | 伪造默认Agent、重购资源、改成免费/重新开始周期 |
| 已部署OPL App或其他应用 | 导入真实应用revision/digest/binding/选中部署和持久卷；deliveryModel=imported_application | 显示现有应用/版本、真实ready与原访问入口 | 继续使用；显式更新才进入F10 | 为迁移重新build、重启、换Key、改数据目录 |
| retained full Launch仍进行中 | 保留原provisioningMode/full阶段、idempotency keys与义务；新系统只读展示进度 | “历史操作处理中”，显示真实阶段 | 原Owner将该操作推进至确定终态，再交接Workspace写权 | 按新agent_saas stage重跑历史扣费/采购 |
| 未决扣费/续费/退款/删除 | 保留原operationId、Code、账户/周期/金额、provider action与收据 | 分开显示资源/付款/退款状态，不合成成功 | 按原单精确读回/继续原义务；完结后迁移 | 以当前余额推断、换原单ID、丢弃unknown |
| 已删/过期/停用Workspace | 保留历史、原付费/删除证据、实际剩余资源事实 | 停用/历史页；无真实资源不显示可恢复 | 需新环境时按新购买，不复活不存在资源 | 创建影子资源假装恢复 |
| 现有应用无可确认digest/持久数据契约 | 保存原始事实和失败原因，暂不切写权 | 旧路径仍负责该对象并显示迁移待处理 | 先用Owner读回补齐精确身份再迁移 | 以tag/current policy/latest猜digest |

导入已有OCI不需要虚构Package或Build。CapabilityVersion使用明确provenance=legacy_application，与build区分；Package/Build引用可为空，仅此来源允许。新Build产物必须具备完整Package/Runtime/WebUI/Build来源。此来源标记记录事实，不是让所有新命令走兼容分支。

保留零应用Workspace是既有数据义务，不改变新createWorkspace必须选择Agent的规则。部署到已有资源单独操作，显式验证资源/套餐/付费/Key/兼容，不能复用新购接口偷偷跳stage。

## 3. 字段级移交表

目标物理列以02和contracts/db_inventory.json为准；下表列出不可丢失的现有字段、语义与转换。读取必须用当前owner decoder，不以JSON键名相似推断同义。

### 3.1 身份与Workspace

| 原表/字段 | 目标Owner/事实 | 转换与校验 |
|---|---|---|
| control_plane_accounts.id | Tenant身份/原account关联 | 原ID原样保留或明确一对一sourceRef；不随机重新编号 |
| accounts.owner_user_id, sub2api_user_id | Gateway映射、billingSub2apiUserId、Tenant owner membership | 确认Gateway身份；旧单用户Tenant只有一个billing主体，不用邮箱重新创建钱包 |
| accounts.name,status,workspace_purchase_enabled | Tenant显示名/权限准入 | 原限制必须保留；false不能迁移成默认可购买 |
| control_plane_users.id,account_id,email,role,status | 成员身份/展示/角色 | ID保留；旧owner按明确映射保留为owner（租户所有者），其他role不得猜映射 |
| users.password_hash | 不进入目标 | 源约束要求为空；若非空记录迁移失败，不复制/展示 |
| users.disabled_*/deleted_* | 身份停用/删除证据 | 保留原时间/actor/reason；不恢复已停用权限 |
| control_plane_sessions.* | 重新建立BFF会话 | 不复制明文session/CSRF/委托凭据；发布窗口令旧会话过期并重新Gateway登录 |
| control_plane_workspaces.id,name,url | Workspace id/name/access binding原始事实 | ID、名称不变；URL只有通过实际origin/binding验证后呈现可打开 |
| workspaces.account_id,owner_account_id,owner_user_id,user_id | Tenant/actor/provenance | 同一个对象多归属字段必须用现有access policy验证一致，不用firstNonEmpty兜底 |
| workspaces.state,status | Workspace.lifecycle +迁移分类 | 先判资源/应用/付款真实事实，再按明确枚举映射；未知状态阻止该对象切换 |
| workspaces.purchase_receipt_id | 原购买Ledger receipt reference | 原ID/字节摘要/类型/账户/Workspace完整匹配 |
| workspaces.billing_state_json | 订阅/周期/原报价/费用/未决义务 | 由现行typed billing decoder展开；原payload存不可变迁移档案，不成为新服务实时读写后门；无新Quote的旧订阅用Subscription.provenance=legacy_import及legacyPurchaseId，保留精确原单/政策条款，acceptedQuoteId缺省，不伪造报价或接受记录 |
| workspaces.storage_id,current_compute_allocation_id,current_attachment_id | Fabric资源引用；Workspace仅业务绑定 | 原ID和provider实体不变；Fabric权威回读确认 |
| workspaces.runtime_id | Runtime Control导入实例引用 | 原Runtime与应用实例有对应才导入；不能创建空壳假ready |
| workspaces.runtime_service_name,runtime_service_name_root,service_name | Fabric路由/运行绑定、来源档案 | 保留现有名字/URL，不以新命名算法重新分配 |
| workspaces.workspace_api_key_id | Gateway Key映射 | 原Key ID原样；无Key的resource_only不伪造已有Key |
| workspaces.access_token_status,access_account,access_username | 安全访问事实/非秘密账户展示 | 只传白名单；不把应用凭据误认Cloud Identity |
| workspaces.credential_status,credential_version,credential_secret_ref,access_requires_login | Secret绑定/访问契约 | 只迁引用/版本，不读取秘密正文；由授权运行边界核验 |
| workspaces.verification_slot_id,customer_product | 对象用途和范围 | qualification fixture与客户Workspace不得混合迁移/收费 |
| workspaces.application_binding,application_binding_version | 应用部署manifest与generation | 解码当前typed binding，保留digest/入口/挂载/credential/data映射，不能字符串覆盖 |
| workspaces.current_application_deployment_id,reserved_application_deployment_id | activeDeploymentId与进行中的候选部署 | 存在reserved或未决切换时先由原owner收敛，不能两边同时激活 |

### 3.2 资源、操作和证据

| 原表/字段组 | 目标 | 明确约束 |
|---|---|---|
| control_plane_compute_allocations：id/account_id/workspace_id/package_id | Fabric allocation +Workspace套餐历史 | 不复制第二套可变资源owner；package_id是旧资源套餐，不可误当Agent Package |
| compute：provider/provider_resource_id/provider_request_id/operation_id/cvm_instance_id/instance_id/node_name/machine_name | Fabric provider事实 | 原provider身份完整迁移并实际读回，不外露客户DTO |
| compute：status/desired_status/provider_status/last_provider_sync_at/error/external_deleted_at | Fabric desired/observed与时间 | 状态字符串只按provider adapter明确映射 |
| compute：billing_*/pricing_version/evidence_id | Workspace订阅事实+Ledger关联 | 区分供应商账期与平台收费，保留原报价，不直接搬作钱包余额 |
| compute：cpu/memory_gb/disk_gb | Catalog/Fabric规格事实 | 单位换算必须精确可表示；非整数数量不得截断 |
| control_plane_storage_volumes：同类provider/billing/owner字段+mount_path/size_gb | Fabric volume +订阅存储项 | 原volume/数据路径/大小保持；非迁移窗口扩缩容 |
| control_plane_storage_attachments：id/workspace_id/compute_allocation_id/storage_id/volume_id/provider_request_id/status/mount_path | Fabric attachment | 实际挂载与双侧资源ID一致才能导入 |
| control_plane_runtime_operations：operation_id/action/status/result/period_start | 对应Workspace/Runtime/Gateway/Fabric Operation与不可变来源档案 | 按action调用当前typed decoder；原action/原key/状态义务不变；未知action须完整归档并明确owner后切换 |
| runtime_operations：resource_id/resource_kind/provider/requestID/compute/storage/attachment/runtime names | 相应operation step目标身份 | 用于继续原操作，不能只留一段“成功”日志 |
| control_plane_application_revisions：id/application_id/version/digest/payload/admitted_by_user_id | Capability legacy_application revision | 原manifest摘要固定；导入不是重新Build/重新审核虚构产物 |
| control_plane_application_data_materials：相同identity/version/digest/payload字段 | Capability/Runtime数据材料引用 | 保留发布者schema与snapshot引用；不执行恢复操作 |
| fabric_operations/machine_ownerships/运行绑定及Secrets引用 | Fabric保留权威 | 不搬到Workspace服务，不清空原幂等记录 |
| Ledger evidence_receipts/idempotency/reconciliation等 | Ledger保留权威 | append-only，不重写老类型/payload/receiptID；新索引用来源引用 |
| control_plane_admin_audit_events/archived_* | Ledger或明确审计owner只读档案 | 历史原样/原actor/时间与来源摘要，秘密按现有边界处理 |
| project_task_sync_heads/workspace_sync_events/announcements/reads/production_e2e_records | 保留在现有独立能力owner，未纳入本轮迁移则不删不改 | 新Agent链不能借“瘦身”丢掉无关客户历史与产品能力 |

这张表不授权所有表一次大搬家。每个领域移交有独立写集；未移交能力继续由原Owner负责，真实调用方切换后才退休旧writer。

## 4. 首批实施需同步的正式仓库文档

当前包是已确认目标的执行规格，当前源码文档是未迁移实现。实施第一个PR必须：

1. 在docs/architecture.md及docs/decisions.md记录新客户Agent+套餐目标、领域拆分和逐域移交，明确替代2026-09-17“客户只买资源”作为新客户默认入口的决定；保留该决定为历史Launch解释。
2. docs/implementation-architecture.md仍描述实际已迁移模块，不把目标提前填成现行架构。
3. docs/status.md写每个来源SHA/测试/迁移证据；docs/roadmap.md对应F功能与未完成Instance义务。
4. docs/README.md指向唯一目标/实施/迁移Owner；退休重复当前writer，不保留两套同名“最终架构”。
5. packages/contracts仅加入实际跨模块消费者需要的wire/data-integrity事实；本执行包的inventories不直接整体塞入产品机器契约。

## 5. 可执行的逐域迁移程序

### M0：冻结精确来源

记录sourceSHA、现有schema migration版本、目标schema版本、UTC时间、owner许可、各表行数/主键集合/规范化行hash、未决操作清单、镜像digest和provider profile来源。生产数据只在Instance授权runner内导出/校验，输出receipt仅含脱敏摘要。

备份必须覆盖数据、当前应用manifest、绑定与Secret引用；不导出Secret正文到开发机。恢复演练使用隔离验证环境，不用客户资源做试验。

### M1：建目标schema和decoder，不接生产写流

执行02的目标schema分段，验证每数据库角色只可写本域；给每类旧对象建立明确typed转换和来源映射（sourceOwner/sourceTable/sourceId/sourceSHA/sourceHash→targetOwner/targetId/targetHash）。转换保留不可映射原字段的只读档案，不吞字段。

每个动作写本域migration Operation与Ledger迁移证据；失败对象记录精确字段与原因。不存在“尽量迁移/默认补零”模式。

### M2：只读试迁移与对账

在隔离副本跑转换：行数/ID集合、唯一性、金额/周期/quote/receipt和来源hash等值；资源绑定和OCI identity精确匹配。对原pending/unknown任务构造重启/丢响应反例，证明目标不会重扣/重购。

额外验证：legacy_resource_only无Capability合法；legacy_application无Package/Build来源合法但digest必需；build缺Build/Package必须拒绝。

### M3：领域写屏障，完成未决义务

逐个Tenant/聚合批次切换，不全球冻结所有功能。原Owner用持久写屏障拒绝该批次的新命令；已发出不可逆操作由原owner继续，直到所有在途写者/lease/Outbox/未决资金资源动作有确定归属。

新owner不能同时执行同一历史义务。若未决项尚不能交接，该对象不进入当前批次，保留完整失败/待处理清单；这不是兼容fallback，是明确尚未迁移且仍有唯一原writer。

### M4：终态增量与原子路由切换

在写屏障下重取增量并做M2等值检查；保存迁移epoch并切换该聚合命令路由至新Owner。旧Owner检查epoch后拒绝迟到写入。BFF只通过确定路由访问，不先试新接口失败再退旧接口。

同一次切换包括客户UI入口、REST客户端、后台worker、operator和定时任务，不能仅迁前端而续费仍双写。历史只读入口按09保留，不将旧写端点作为隐式兼容别名。

### M5：真实功能验收与旧writer退休

在同一候选制品上执行F01/F07/F08/F09/F10/F12/F13/F16关键链，确认原Workspace URL/数据、账期、Key引用和访问边界不变。完整客户新购的Instance验收需另行授权限定费用/资源范围；普通CI只测本地/模拟边界，不触发真钱。

保存同SHA/digest的Local和Instance回执；再删除旧writer/退休旧路由。旧表保留只读档案和数据保留义务，不写死90天自动DROP。

## 6. 回滚不是切回旧域名

- 新Owner尚未接收业务写：校验迁移档案后可撤销路由切换，恢复原Owner写屏障；无新资金/资源副作用。
- 新Owner已写：先冻结新写，按同一来源映射导出新事实/未决义务，验证旧格式能无损表达且原Owner具备继续能力，再反向迁移/切路由。没有该证明，不允许简单切旧代码。
- 已出现旧格式无法表达的数据：保持新Owner、修复前进；不得丢新记录或把它当临时兜底。发布前必须明确不可回滚边界和Instance恢复手册。
- runtime镜像回滚与平台数据/Owner迁移回滚是两件事；前者不能自动完成后者。

## 7. F16验收断言与接收者

| 断言 | 责任方 | 证据 |
|---|---|---|
| 所有源ID与必要字段有映射/归档，未决义务无丢失 | 各数据Owner | 表/字段映射、集合/hash/金额对账 |
| 无重复付款/退款/购买/删除，原Code和provider身份保持 | Workspace/Gateway/Fabric | 原单读回、重放测试、明确absence |
| 旧resource_only沿用资源可部署，原购买历史不变 | Workspace/Runtime/Console | 浏览器剧本+资源identity读回 |
| 已有应用可继续打开，数据/模型配置/Key不改变 | Runtime/Fabric/Console | 真实应用readiness/数据探针/访问边界 |
| 权限不放大、单writer成立、旧worker被fence | Identity/各Owner | 跨Tenant/迟到worker/双写反例 |
| rollback实际可回放或不可逆边界如实声明 | Instance/数据Owner | 隔离恢复演练receipt |

只有全部功能和数据义务接受后，该批次迁移才算完成。源代码commit、生成SQL或启动新服务都不是迁移receipt。

## 8. D17变更后的历史与回滚义务

- 新补差单挂在原subscription period下，不重新生成Workspace购买历史/原Code/原paidThrough。PlanChange成功只改变当前套餐与后续续费基准。
- 已有基础单保持原退款政策；新增补差单记录自身T..E覆盖区间和policyVersion。导出/迁移/对账不能只迁一条“总价”而丢掉多笔原单。
- 预约降配是持久业务计划，不是UI草稿；迁移必须包含目标下一期价、plannedEffectiveAt、初次接受Operation、执行Operation、取消版本、下期已接受付款身份。
- 旧平台版本不能表达PlanChange/补差覆盖区间时，不允许在新系统已经接受这些义务后只切回旧二进制。必须先停止新写并完成可验证的无损反向迁移，否则按前进修复处理。
- Upgrade计算只读取原S/E的UnixMilli值，不回写截断后的历史时刻；原billingAnchorDay及UTC账期Owner保持不变，不能因短月改变锚点。
