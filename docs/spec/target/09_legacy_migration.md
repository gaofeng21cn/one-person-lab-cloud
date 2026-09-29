# 09 旧产品路径与数据迁移

> F16唯一迁移规格。只定义后续实施动作；本轮没有读取生产数据库、创建仓库、发布、扣费或迁移任何客户资源。
>
> **已采用业务与待迁移wire分开**：2026-09-29确认的新默认`opl_app(runtimeVersionId)`+nativeUI与`agent(capabilityVersionId)`走06同一购买/Serve完整部署链，绝不是legacy_resource_only。本文件M0–M5只处理有冻结来源证明的旧对象；06第17节仍保留W01迁移前wire baseline；03/SQL/proto/events/机器flows中的新判别联合/来源约束须由W01集成负责人协调W01成套贯通，不宣称已实现。

## 1. 来源与目标，不混淆证据层

历史取证源码基线：one-person-lab-cloud main，50520e27a6b9a630eefdc2df7da3e5ec498d28a0（2026-09-20）。该历史SHA的Console固定resource_only；Control Plane/Fabric/Ledger是该基线的进程/schema Owner。这是历史实现，不是当前源码/运行态判断，更不是新客户默认规则；已有Instance证据不能自动覆盖其他SHA。

以下绝对路径是该SHA的历史读取依据，保留原文以便复核，不是当前开发写入位置。当前Cloud产品统一在`opl-cloud`仓库内按01/14的相对路径实施；更名或移动checkout不重写历史SHA、回执或来源引用。

历史读取依据（绝对路径）：

- /Users/huangrende/Documents/ChatGPT/one-person-lab-cloud/apps/console-ui/src/app/workspace-launch-controller-model.ts:105
- /Users/huangrende/Documents/ChatGPT/one-person-lab-cloud/packages/contracts/go/workspace_provisioning.go:24
- /Users/huangrende/Documents/ChatGPT/one-person-lab-cloud/services/control-plane/ent/schema/shared.go:25
- /Users/huangrende/Documents/ChatGPT/one-person-lab-cloud/services/control-plane/internal/server/workspace_launch_provisioning_mode.go:22
- /Users/huangrende/Documents/ChatGPT/one-person-lab-cloud/services/control-plane/internal/server/workspace_delete_refund.go:405
- /Users/huangrende/Documents/ChatGPT/one-person-lab-cloud/packages/contracts/go/workspace_delete.go:650
- /Users/huangrende/Documents/ChatGPT/one-person-lab-cloud/services/ledger/internal/ledger/types.go:210

目标：新客户一次确认默认OPL App+nativeUI及套餐，或选择Agent，Workspace Saga编排资源后由Serve完成可用部署；F16仅负责旧对象平滑移交。在同一`opl-cloud`仓库内把现有职责逐领域移交到01规定的独立module/服务，而不是创建领域GitHub仓库、清空重建、改ID、再次购买或由BFF兼容猜状态。`services/control-plane`仅作为未移交能力的原Owner，不与新Owner永久双写；Instance/Sub2API/Framework等外部权威不因单仓库决定并入Cloud。

## 2. 按旧对象类型明确客户迁移体验

| 旧对象 | 导入事实 | 新界面 | 第一次新动作 | 绝不做 |
|---|---|---|---|---|
| 已购买resource_only，尚无应用 | 原ID/原单/已付周期/资源绑定保持，Serve无current Deployment；仅有来源证明才标legacy_resource_only；旧nullable Capability字段只是wire baseline | 显示“资源已开通，待部署应用”、原到期/套餐 | 显式选择approved opl_app+nativeUI或Agent，在已确认原资源上Serve Reserve→Key/Secret→Deploy；沿用资源操作不执行新购debit/compute/storage阶段 | 从空Capability推断新默认已装或legacy、伪造默认Agent、重购资源/重启周期 |
| 已部署OPL App或其他应用 | 导入真实应用revision/digest/binding/选中部署和持久卷；deliveryModel=imported_application | 显示现有应用/版本、真实ready与原访问入口 | 继续使用；显式更新才进入F10 | 为迁移重新build、重启、换Key、改数据目录 |
| retained full Launch仍进行中 | 保留原provisioningMode/full阶段、idempotency keys与义务；新系统只读展示进度 | “历史操作处理中”，显示真实阶段 | 原Owner将该操作推进至确定终态，再交接Workspace写权 | 按新agent_saas stage重跑历史扣费/采购 |
| 未决扣费/续费/退款/删除 | 保留原operationId、Code、账户/周期/金额、provider action与收据 | 分开显示资源/付款/退款状态，不合成成功 | 按原单精确读回/继续原义务；完结后迁移 | 以当前余额推断、换原单ID、丢弃unknown |
| 已删/过期/停用Workspace | 保留历史、原付费/删除证据、实际剩余资源事实 | 停用/历史页；无真实资源不显示可恢复 | 需新环境时按新购买，不复活不存在资源 | 创建影子资源假装恢复 |
| 现有应用无可确认digest/持久数据契约 | 保存原始事实和失败原因，暂不切写权 | 旧路径仍负责该对象并显示迁移待处理 | 先用Owner读回补齐精确身份再迁移 | 以tag/current policy/latest猜digest |

导入旧应用OCI不虚构Package或Build。对于按旧应用迁移合同登记的CapabilityVersion，保留明确provenance=legacy_application及真实digest/原manifest，Package/Build来源可空；不能把它当新Agent Build或重标成新默认App。新Agent Build产物须具备完整Package/Runtime/Build及独立approved WebUI来源，保留三项真实输入及claim，不接受缺独立WebUI的第三种Agent组合。新默认App直接引用RuntimeRelease OCI/发布者运行合同，根本不登记CapabilityVersion，因而无需借用legacy_application来源豁免。该区别是来源事实，不是兼容分支。

保留零应用Workspace只针对冻结来源清单中的既有数据义务。新createWorkspace在接受前必须固定WorkspaceApplicationSelection判别联合：opl_app(runtimeVersionId)或agent(capabilityVersionId)；没有Agent/WebUI选择即批准默认App+nativeUI，不是“空应用”或“裸资源”。旧资源首次部署是独立的显式沿用资源操作，验证原资源/套餐/付费/Key/数据兼容与Serve真实交付；不能复用新购接口偷偷跳stage，不能在迁移时自动安装默认App。

## 3. 字段级移交表

下表列出不可丢失的旧字段、语义与目标Owner；02和contracts/db_inventory.json仍是物理列的唯一Owner，新的应用选择/来源约束须经W01与SQL/decoder成套同步并验证后才能作为可执行转换依据。读取必须用当前owner decoder，不以JSON键名相似推断同义。

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
| workspaces.runtime_id | Serve导入原运行实例引用 | 原Runtime与应用实例有对应才导入；不能创建空壳假ready |
| workspaces.runtime_service_name,runtime_service_name_root,service_name | Serve访问/实例绑定及来源档案；底层基础网络资源仍归Fabric | 保留名字/URL及实际路由revision/generation；Serve readback与原绑定一致，不由Fabric写current/路由 |
| workspaces.workspace_api_key_id | Gateway Key映射 | 原Key ID原样；无Key的resource_only不伪造已有Key |
| workspaces.access_token_status,access_account,access_username | 安全访问事实/非秘密账户展示 | 只传白名单；不把应用凭据误认Cloud Identity |
| workspaces.credential_status,credential_version,credential_secret_ref,access_requires_login | Secret绑定/访问契约 | 只迁引用/版本，不读取秘密正文；由授权运行边界核验 |
| workspaces.verification_slot_id,customer_product | 对象用途和范围 | qualification fixture与客户Workspace不得混合迁移/收费 |
| workspaces.application_binding,application_binding_version | 应用部署manifest与generation | 解码当前typed binding，保留digest/入口/挂载/credential/data映射，不能字符串覆盖 |
| 原Workspace current_application_deployment_id,reserved_application_deployment_id | Serve active Deployment与进行中的候选部署 | 存在reserved或未决切换时先由原owner收敛，不能两边同时激活 |

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
| control_plane_runtime_operations：operation_id/action/status/result/period_start | 对应Workspace/Serve/Gateway/Fabric Operation与不可变来源档案 | 按action调用当前typed decoder；原action/原key/状态义务不变；未知action须完整归档并明确owner后切换 |
| runtime_operations：resource_id/resource_kind/provider/requestID/compute/storage/attachment/runtime names | 相应operation step目标身份 | 用于继续原操作，不能只留一段“成功”日志 |
| control_plane_application_revisions：id/application_id/version/digest/payload/admitted_by_user_id | Capability legacy_application revision | 原manifest摘要固定；导入不是重新Build/重新审核虚构产物 |
| control_plane_application_data_materials：相同identity/version/digest/payload字段 | Capability来源材料与Serve运行/恢复数据引用 | 保留发布者schema与snapshot引用；不执行恢复操作 |
| fabric_operations/machine_ownerships/运行绑定及Secrets引用 | Fabric保留资源/Secret绑定与原幂等证据；应用实例/访问事实按来源映射交Serve | 不将资源搬到Workspace或清空原幂等记录；原运行/路由writer在移交后退休，不让Fabric保留第二部署writer |
| Ledger evidence_receipts/idempotency/reconciliation等 | Ledger保留权威 | append-only，不重写老类型/payload/receiptID；新索引用来源引用 |
| control_plane_admin_audit_events/archived_* | Ledger或明确审计owner只读档案 | 历史原样/原actor/时间与来源摘要，秘密按现有边界处理 |
| project_task_sync_heads/workspace_sync_events/announcements/reads/production_e2e_records | 保留在现有独立能力owner，未纳入本轮迁移则不删不改 | 新Agent链不能借“瘦身”丢掉无关客户历史与产品能力 |

这张表不授权所有表一次大搬家。每个领域移交有独立写集；未移交能力继续由原Owner负责，真实调用方切换后才退休旧writer。

## 4. 首批实施需同步的正式仓库文档

当前包描述已确认业务目标，不以本轮文档推断各模块实现程度。W01集成负责人须协调以下消费者后由实施变更落地；它们不在本轮三文件写集内：

1. 在docs/architecture.md及docs/decisions.md、00/01/12同步TKE主线与默认App/Agent双分支、一次确认完整交付；默认App直接RuntimeRelease OCI，Runtime Control仅目录、Fabric仅资源、Serve唯一部署。不新增服务，保留2026-09-17“客户只买资源”为旧Launch历史解释。
2. docs/implementation-architecture.md仍描述实际已迁移模块，不把目标提前填成现行架构。
3. docs/status.md写每个来源SHA/测试/迁移证据；docs/roadmap.md对应F功能与未完成Instance义务。
4. docs/README.md指向唯一目标/实施/迁移Owner；退休重复当前writer，不保留两套同名“最终架构”。
5. W01同步02/03、SQL/proto/events和packages/contracts中实际消费者需要的判别联合/来源约束、Build UI正规化、资源先于Serve Reserve/Key的交接；更新Console/BFF、Workspace、Catalog、Runtime Control、Capability/Build、Serve、Fabric、Gateway、Ledger及边界测试。不把inventories整体塞入机器契约。
6. 同步04/05/07/10/11/13/14及其UI/任务/traceability消费者；再协调generators与机器flows、06第17节的再生成和完整检查。新业务已采用但尚无新wire贯通证据，不复用旧checks报告宣称通过。

## 5. 可执行的逐域迁移程序

M0–M5的对象集合必须来自M0冻结的旧对象清单及同批次屏障前增量；不包含按新业务正常创建的默认App或Agent。新购TKE首链/Local回归/完整发布按08验收，不走迁移接口或赋予legacy来源。

### M0：冻结精确来源

记录sourceSHA、现有schema migration版本、目标schema版本、UTC时间、owner许可、各表行数/主键集合/规范化行hash、未决操作清单、镜像digest和provider profile来源。生产数据只在Instance授权runner内导出/校验，输出receipt仅含脱敏摘要。

逐个来源对象记录resource_only、已有应用、retained full或未决义务分类及原writer；不能只按Capability ID是否为空分类，因为新默认App本来就无CapabilityVersion。

备份必须覆盖数据、当前应用manifest、绑定与Secret引用；不导出Secret正文到开发机。恢复演练使用隔离验证环境，不用客户资源做试验。

### M1：建目标schema和decoder，不接生产写流

W01先贯通目标应用选择/来源约束及Owner wire，再执行02对应目标schema分段，验证每数据库角色只可写本域；只给M0各类旧对象建立明确typed转换和来源映射（sourceOwner/sourceTable/sourceId/sourceSHA/sourceHash→targetOwner/targetId/targetHash）。转换保留不可映射原字段的只读档案，不吞字段。

每批转换/切换由本域migration Operation持久接受，必要迁移证据追加Ledger；receipt只记录Owner已确认结果，不触发迁移或写业务状态，也不要求每轮对账poll产生receipt。失败对象记录字段与原因。不存在“尽量迁移/默认补零”模式。

### M2：只读试迁移与对账

在隔离副本跑转换：行数/ID集合、唯一性、金额/周期/quote/receipt和来源hash等值；资源绑定和OCI identity精确匹配。对原pending/unknown任务构造重启/丢响应反例，证明目标不会重扣/重购。

额外验证：有旧来源证明的legacy_resource_only可无应用；legacy_application可无Package/Build但真实digest/合同必需；新agent Build缺Build/Package/Runtime/独立WebUI拒绝，默认App内置UI不伪造独立WebUI版本。反例必须证明：新opl_app无Capability仍是完整默认应用、不得导入为legacy_resource_only；未知来源不能冒用legacy豁免。

### M3：领域写屏障，完成未决义务

只对M0及屏障前增量中的旧对象逐Tenant/聚合批次切换，不全球冻结所有功能，也不把已由新Owner接受的新默认App纳入旧owner重放。原Owner用持久写屏障拒绝该批次的新命令；已发出不可逆操作由原owner继续，直到所有在途写者/lease/Outbox/未决资金资源动作有确定归属。

新owner不能同时执行同一历史义务。若未决项尚不能交接，该对象不进入当前批次，保留完整失败/待处理清单；这不是兼容fallback，是明确尚未迁移且仍有唯一原writer。

### M4：终态增量与原子路由切换

在同一旧对象来源范围的写屏障下重取增量并做M2等值检查，不能将新Owner创建的默认App混进导入；保存迁移epoch并切换该聚合命令路由至新Owner。旧Owner检查epoch后拒绝迟到写入。BFF只通过确定路由访问，不先试新接口失败再退旧接口。

同一次切换包括客户UI入口、REST客户端、后台worker、operator和定时任务，不能仅迁前端而续费仍双写。历史只读入口按09保留，不将旧写端点作为隐式兼容别名。

### M5：真实功能验收与旧writer退休

在同一候选制品上对本批旧对象执行F16及适用的F01/F09/F10/F11/F12/F13，确认原Workspace URL/数据、账期、Key引用和访问边界不变；旧裸资源显式部署默认App或Agent均不新购，retained full由原owner完成后才交接。新默认App/Agent的F07/F08另按08验收，不作为legacy转换步骤。需要真实资金/资源的验收必须另有Instance受限授权；普通CI只测本地/隔离边界，不触发真钱。

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
| 旧resource_only显式部署App或Agent，原购买历史不变；新默认不标legacy | Workspace/Serve/Console | 浏览器剧本+资源identity读回 |
| 已有应用可继续打开，数据/模型配置/Key不改变 | Serve/Fabric/Gateway/Console | 真实应用readiness/数据探针/访问边界 |
| 权限不放大、单writer成立、旧worker被fence | Identity/各Owner | 跨Tenant/迟到worker/双写反例 |
| rollback实际可回放或不可逆边界如实声明 | Instance/数据Owner | 隔离恢复演练receipt |

只有全部功能和数据义务接受后，该批次迁移才算完成。源代码commit、生成SQL或启动新服务都不是迁移receipt。

## 8. D17变更后的历史与回滚义务

- 新补差单挂在原subscription period下，不重新生成Workspace购买历史/原Code/原paidThrough。PlanChange成功只改变当前套餐与后续续费基准。
- 已有基础单保持原退款政策；新增补差单记录自身T..E覆盖区间和policyVersion。导出/迁移/对账不能只迁一条“总价”而丢掉多笔原单。
- 预约降配是持久业务计划，不是UI草稿；迁移必须包含目标下一期价、plannedEffectiveAt、初次接受Operation、执行Operation、取消版本、下期已接受付款身份。
- 旧平台版本不能表达PlanChange/补差覆盖区间时，不允许在新系统已经接受这些义务后只切回旧二进制。必须先停止新写并完成可验证的无损反向迁移，否则按前进修复处理。
- Upgrade计算只读取原S/E的UnixMilli值，不回写截断后的历史时刻；原billingAnchorDay及UTC账期Owner保持不变，不能因短月改变锚点。
