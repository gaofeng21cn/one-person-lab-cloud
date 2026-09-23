#!/usr/bin/env python3
"""Readable domain handoff map, not a workflow runtime. RPC types come from the actual .proto."""
from pathlib import Path
import json,re
root=Path(__file__).resolve().parents[1]
proto=(root/'contracts/internal.proto').read_text()
rpcs={}
for m in re.finditer(r'service\s+(\w+)\s*\{(.*?)\}',proto,re.S):
 for n in re.finditer(r'rpc\s+(\w+)\((\w+)\)\s+returns\s*\((\w+)\)',m[2]):rpcs[f'{m[1]}.{n[1]}']={'request':n[2],'response':n[3]}
flows=[]
def step(caller,owner,rpc,writes,proof,recovery):
 if rpc not in rpcs:raise ValueError(f'RPC not declared: {rpc}')
 return {'caller':caller,'owner':owner,'rpc':rpc,**rpcs[rpc],'writes':writes,'completionEvidence':proof,'onUnknownOrRejection':recovery}
def flow(fid,title,entry,steps,terminal,notes=''):
 flows.append({'featureId':fid,'title':title,'entryOperationIds':entry,'authorization':'CloudIdentityAuthorization.AuthorizeAction + receiving Owner resource/state check','steps':steps,'terminal':terminal,'notes':notes})
A=lambda caller:step(caller,'tenant','CloudIdentityAuthorization.AuthorizeAction',[],'session/grant/action/resource/audience/权限版本确切一致','deny不改用管理员；unknown不发后续副作用')
R=lambda caller,owner:step(caller,'ledger','LedgerCoordination.AppendReceipt',['ledger.receipts'],'返回receipt身份，另ReadReceiptByReference核对原输入','同idempotency key读回，不填假Workspace或重写receipt')
flow('F01','个人身份、Tenant和成员',['getLoginContext','login','getSession','inviteMember','updateMemberRole','removeMember'],[
 step('bff','tenant','TenantProductService.Login',['tenant.sessions'],'Gateway认证主体与Cloud成员关系确定，session无密码持久化','登录失败统一提示；不创建新Gateway钱包'),A('receiving_owner'),
 step('bff','tenant','TenantProductService.UpdateMemberRole',['tenant.tenant_members','tenant.audit_events'],'权限版本变化及最后owner保护','旧版本/越权拒绝，不只隐藏按钮')], '个人登录与账单主体分离；成员只能做被授权动作')
flow('F02','分组与私有/官方可见性',['listNamespaces','createNamespace','archiveNamespace','listPackages'],[A('capability'),
 step('bff','capability','CapabilityProductService.CreateNamespace',['capability.namespaces'],'tenant+name唯一、正确权限和状态','冲突不创建第二组；不跨Tenant读写')], '客户分组可见性正确，归档不删除制品和历史')
flow('F03','Publisher准入与资源价格目录',['createPublisherNamespace','registerRuntimeVersion','registerWebuiVersion','createComputePlan','createPricePolicyVersion'],[A('capability'),
 step('bff','capability','CapabilityProductService.CreatePublisherNamespace',['capability.publisher_namespaces'],'官方/第三方种类与Registry prefix确认','错误prefix/归属拒绝，不放进默认官方空间'),
 step('bff','capability','CapabilityProductService.RegisterRuntimeVersion',['capability.runtime_versions'],'完整PublisherContract schema+canonical Go validator+Registry读回通过','缺repository/platform/完整revision拒绝'),
 step('bff','resource_catalog','ResourceCatalogProductService.CreatePricePolicyVersion',['resource_catalog.price_policy_versions'],'套餐组合/单月/明确政策事实，pending调整策略不可报价','不补空JSON或猜默认金额')], '管理员提供实际可执行选项，客户不任意指定Runtime镜像')
flow('F04','上传与确认原字节',['createPackage','createUpload','createUploadPart','completeUpload'],[A('capability'),
 step('bff','capability','CapabilityProductService.CreateUpload',['capability.package_versions','capability.upload_sessions'],'固定包版本及uploadId/受限对象地址','同键返回同身份；过期续签同对象'),
 step('bff','capability','CapabilityProductService.CompleteUpload',['capability.upload_chunks','capability.package_versions','capability.operations'],'Storage精确对象version、字节sha256和长度通过','校验失败不得标uploaded，不信ETag=SHA256')], 'PackageVersion uploaded；此时尚未构建，必须显式createBuild','字节直接传Storage签署地址，不通过BFF代理大文件')
flow('F05','构建输入保护、输出与注册',['createBuild','getBuild','listBuildLogs','retryBuild'],[
 step('build','capability','CapabilityCoordination.ResolveBuildInput',[],'Package/WebUI/Runtime策略和完整发布描述冻结','读失败还未建业务副作用，不能选latest替代'),
 step('bff','build','BuildProductService.CreateBuild',['build.build_jobs','build.operations'],'Job queued+snapshot+Operation+幂等同事务','响应丢失取回原身份，不建第二任务'),
 step('build','capability','CapabilityCoordination.AcquireReference',['capability.reference_claims'],'PackageVersion/RuntimeVersion/WebuiVersion三target均绑定本job','任一拒绝则不读字节/不build；保留已取得claim待确定收尾'),
 step('build','capability','CapabilityCoordination.BindReference',['capability.reference_claims'],'Build本域保存claim IDs后的OwnerCommitEvidence可读回','Bind丢响应查原claim，不自动TTL释放'),
 step('capability','build','BuildCoordination.ReadArtifact',[],'repository+digest+platform+DeploymentDescriptor与远端制品相同','不是push接受就注册，unknown继续读原产物'),
 step('build','capability','DomainInbox.Deliver',['capability.capability_versions'],'Inbox事务去重后唯一版本，事件与Build真实readback一致','同eventId重复ACK，乱序不覆盖新事实'),R('build','build')], '唯一可部署CapabilityVersion；完整Job历史和输入来源保留','worker按固定recipe调用BuildKit与Registry exporter；不是新的构建框架')
flow('F06','目录归档与引用释放',['archivePackage','deleteCapabilityVersion'],[A('capability'),
 step('bff','capability','CapabilityProductService.DeleteCapabilityVersion',['capability.capability_versions','capability.reference_claims'],'本域锁定版本并确认无活跃claim；只做目录下架','仍使用返回冲突；不建议删Workspace绕过'),
 step('consumer_owner','capability','CapabilityCoordination.ReleaseReference',['capability.reference_claims'],'原claim owner真实usage/终态证据','未知不释放，历史元数据不级联')], '显示“已从目录移除”；物理镜像清除另属授权管理员操作')
flow('F07','选择Agent/套餐/模型并取得准确报价',['createQuote','getQuote'],[A('workspace'),
 step('resource_catalog','workspace','WorkspaceAdmission.CheckAdmission',[],'当前版本、模型、作用域与原Workspace义务确认','quote并不等于容量预留，提交时复查'),
 step('workspace','fabric','FabricCoordination.AdmitResources',[],'provider能力、实际资源/route CAS能力准入','不能静默换provider或降能力'),
 step('bff','resource_catalog','ResourceCatalogProductService.CreateQuote',['resource_catalog.quotes','resource_catalog.quote_items'],'purpose/kind/金额符号与DB完全同词；总额=sum正项-credit','pending/缺政策拒绝，不能零价兜底')], '客户能看到同一quote的金额、单月周期、有效期和数据规则')
flow('F08','购买到实际可用的部署',['createWorkspace','getOperation','getWorkspace'],[A('workspace'),
 step('workspace','resource_catalog','CatalogCoordination.AcceptQuote',['resource_catalog.quotes'],'quoteID/inputDigest唯一绑定原operation','冲突拒绝，不能重报价后续跑原单'),
 step('workspace','runtime_control','RuntimeCoordination.Reserve',['runtime_control.runtime_instances'],'预留稳定runtimeInstanceId而不启动','不因Key依赖Runtime ID产生循环'),
 step('workspace','tenant','CloudIdentityAuthorization.IssueAcceptedOperationGrant',['tenant.accepted_operation_grants'],'原Owner commit读回，有限actions/resource/period','不能伪造commit字符串或扩大到新Workspace'),
 step('workspace','gateway','GatewayCoordination.Debit',['gateway.wallet_operations'],'精确账户/Code/金额原单confirmed','unknown只ReadWalletAction，绝不重复扣费'),
 step('workspace','gateway','GatewayCoordination.CreateManagedKey',['gateway.key_bindings'],'原实例与允许模型/Key引用/版本一致','不盲建第二Key，不在DB/事件存明文'),
 step('workspace','fabric','FabricCoordination.EnsureResources',['fabric.resources','fabric.resource_sets','fabric.attachments'],'批准预付资源与Workspace/原请求exact匹配','unknown查原provider动作，不重购'),
 step('runtime_control','fabric','FabricCoordination.BindSecret',['fabric.secret_bindings'],'完整发布描述指定的Secret引用实际注入','不把Gateway Key当任意环境变量公开'),
 step('workspace','runtime_control','RuntimeCoordination.Deploy',['runtime_control.runtime_actions'],'完整DeploymentDescriptor送执行层并实际ready','非就绪不开放入口'),
 step('workspace','fabric','FabricRouteExecution.FenceRouteEpoch',['fabric.route_bindings','fabric.route_switches'],'provider conditional revision确认新epoch','未知旧switch先读回，不抢占'),
 step('runtime_control','fabric','FabricRouteExecution.ActivateRoute',['fabric.route_bindings','fabric.route_switches'],'epoch/revision/target精确，provider实际路由确认','旧epoch/旧revision拒绝，丢响应ObserveRoute'),R('workspace','workspace')], 'Workspace本域CAS selectedDeployment/selected generation后，receipt核对；客户可打开当前应用','Workspace.activeDeploymentId是选中业务权威，Fabric是真实路由权威；没有跨库原子提交幻觉')
flow('F09','使用、模型配置与应用登录',['getWorkspace','getWorkspaceAccess','getWorkspaceModels','updateWorkspaceModels','revealWorkspaceApplicationCredentials'],[A('workspace'),
 step('bff','workspace','WorkspaceProductService.GetWorkspaceAccess',[],'当前部署/访问策略和运行事实一致','按canonical应用登录，不新增SSO'),
 step('bff','workspace','WorkspaceProductService.RevealWorkspaceApplicationCredentials',[],'所有者权限+当前声明workspace_admin_password+实际ready，只一次性用户名/密码','no-store不缓存；不返回GatewayKey或session_secret'),
 step('workspace','runtime_control','RuntimeCoordination.ReloadModels',['runtime_control.runtime_actions'],'目标配置版本+selections实际应用','保存成功不等于reload成功'),
 step('runtime_control','fabric','FabricRuntimeExecution.ObserveRuntime',[],'appliedVersion和选择相同，运行状态真实','unknown显示应用中/待核实，不覆盖已确认配置')], '打开真实应用；模型实际生效；应用管理员凭据仅在既有授权reveal路径一次性显示')
flow('F10','更新、切换和回滚',['updateWorkspaceVersion','rollbackWorkspace'],[A('workspace'),
 step('workspace','capability','CapabilityCoordination.ResolvePublisherContract',[],'目标版本完整契约与数据兼容','不支持安全回滚的迁移拒绝，不猜semver'),
 step('workspace','fabric','FabricRouteExecution.FenceRouteEpoch',['fabric.route_switches','fabric.route_bindings'],'新epoch先在provider确认','旧未知切换不被强行覆盖'),
 step('workspace','runtime_control','RuntimeCoordination.Deploy',['runtime_control.runtime_instances','runtime_control.runtime_actions'],'新实例实际验证且不违反可写卷并发限制','保留旧选中版本/原数据义务'),
 step('runtime_control','fabric','FabricRouteExecution.ActivateRoute',['fabric.route_switches','fabric.route_bindings'],'新target/provider revision确认','丢响应原switch读回'),
 step('runtime_control','fabric','FabricRouteExecution.RollbackRoute',['fabric.route_switches','fabric.route_bindings'],'原目标+原switch证据+当前expected generation可核对','不能仅改DB指针称回滚成功')], '一个确认选中部署，无重复购买；失败时旧运行结果有实际证据')
flow('F11','立即升级、下期降配与独立原单结算',['resizeWorkspace','createQuote','listPlanChanges','getPlanChange','cancelPlanChange'],[A('workspace'),
 step('resource_catalog','workspace','WorkspacePlanChangeReadback.ReadSubscriptionPlanState',[],'原period/S/E/已接受当前月价/当前计划和资金义务版本固定','已锁定其它未来账单或财务基础变化拒绝，不退旧款改价'),
 step('resource_catalog','fabric','FabricPlanTransitionReadback.ReadApprovedPlanTransition',[],'批准可比转换，固定执行策略/数据/中断能力','mixed/no-op/不支持缩容拒绝，不按SKU名字或价格猜方向'),
 step('bff','resource_catalog','ResourceCatalogProductService.CreateQuote',['resource_catalog.quotes','resource_catalog.quote_items'],'升级仅最后ceil补差，降配当前0且下期目标价单列；原T固定','过期/改变原计划须新quote，不接收客户自报价'),
 step('bff','workspace','WorkspaceProductService.ResizeWorkspace',['workspace.plan_changes','workspace.operations'],'CAS写PlanChange与原基础；scheduled不是applied','同幂等键返回原计划，已有未完成计划不覆盖'),
 step('workspace','resource_catalog','CatalogCoordination.AcceptQuote',['resource_catalog.quotes'],'exact quote绑定本PlanChange的初次Operation','不能把其它purpose/计划quote用作新购买'),
 step('workspace','gateway','GatewayPlanChangeSettlement.DebitSupplement',['gateway.wallet_operations'],'仅upgrade正补差、原code与固定quote金额一致','0金额不调用；unknown原单读回不新收费'),
 step('gateway','workspace','WorkspacePlanChangeReadback.ReadNextPeriodObligation',[],'downgrade到期资金动作绑定唯一workspace/nextPeriod原义务与consent','没有有效付款授权则awaiting_payment，不偷开自动续费'),
 step('workspace','gateway','GatewayPlanChangeSettlement.DebitScheduledPeriod',['gateway.wallet_operations'],'仅目标下一期已接受价格，提前付款也不提前减资源','不得先旧价扣再补救；manual未授权不调用'),
 step('workspace','fabric','FabricCoordination.ResizeResources',['fabric.resource_actions','fabric.resources'],'资金/ZeroFundingEvidence和固定executionPlan，原epoch/目标/资源读回一致','unknown原请求读回，部分不可逆事实独立保留不假缩容'),
 step('workspace','runtime_control','RuntimePlanChangeControl.RestoreAfterResourceChange',['runtime_control.runtime_actions'],'现有应用实际资源限制/挂载/健康确认；裸资源为owner证明的not_applicable','已有应用不可用不得applied，不让客户skipRuntime'),
 step('workspace','ledger','LedgerPlanChangeEvidence.AppendPlanChangeReceipt',['ledger.receipts'],'本域CAS appliedAt/active计划/原E不变或下期正确周期后精确receipt','已受理/预约成功不能当资源已生效'),
 step('bff','workspace','WorkspaceProductService.CancelPlanChange',['workspace.plan_changes','workspace.operations'],'尚无新期资金accepted/执行时CAS取消；历史不改','付款或资源动作已开始拒绝，不能按低价付完再恢复高配'),
 step('workspace','gateway','GatewayPlanChangeSettlement.RefundFailure',['gateway.wallet_operations'],'确定交付失败+fence+实际资源证据，补偿原补差或原目标期款','unknown不退；部分不可逆成本归平台，不向客户擅收部分交付费'),
 step('workspace','gateway','GatewayPlanChangeSettlement.RefundSupplementOnDeletion',['gateway.wallet_operations'],'成功补差的T..E原覆盖区间、正常删除证据及原单未退余额','不用base720；每笔原单分别去重/读回')], '升级实际确认后applied且E不变；降配scheduled到原E、下期付款/资源确认后applied；失败/退款/真实资源分开', '这些是按kind/状态选择的分支，不是每次依次扣补差、扣下一期并退款。due worker、唯一period obligation/CAS和计划应用都是Workspace本Owner直接事务，不新建自调用RPC或workflow engine。')
flow('F12','续费与到期恢复',['renewWorkspace','updateRenewalSettings','getSubscription'],[A('workspace'),
 step('tenant','workspace','WorkspaceAuthorizationReadback.ReadRenewalConsent',[],'本周期explicit consent/原grant允许','关闭只阻止新周期，不丢已接受义务'),
 step('workspace','gateway','GatewayCoordination.Debit',['gateway.wallet_operations'],'workspace+period业务键唯一原单确认','重复点击/worker命中同一义务'),
 step('workspace','fabric','FabricCoordination.RenewResources',['fabric.resource_actions'],'原资源续期读回与原paidThrough续期窗口一致','过期已回收不伪造恢复、不改now重新计期'),R('workspace','workspace')], '同周期仅付一次，显示真实新账期和运行恢复；迁移原续费模式')
flow('F13','删除与原单退款',['deleteWorkspace','getWorkspaceDeletion','listWorkspaceTransactions'],[A('workspace'),
 step('workspace','runtime_control','RuntimeCoordination.Retire',['runtime_control.runtime_actions'],'确切旧Runtime停止/不存在','unknown不继续声称全环境已删'),
 step('workspace','fabric','FabricCoordination.DeleteResources',['fabric.resource_actions','fabric.resources','fabric.attachments'],'原资源/挂载/Secret及公开访问绑定确切absence','不依赖列表没看到推断不存在'),R('workspace','workspace'),
 step('workspace','gateway','GatewayCoordination.Refund',['gateway.wallet_operations'],'原单剩余可退金额+删除receipt+原钱包目标','unknown原退款读回，删除与退款分别显示')], '环境删除与退款各自可查；不删除Package/Build历史，不泄露Key')
flow('F14','钱包、Key与Token记录',['getWallet','listUsage','createGatewayKey','revealGatewayKey','revokeGatewayKey'],[A('gateway'),
 step('bff','gateway','GatewayProductService.GetWallet',[],'Sub2API当前钱包事实','不可用不返回0，不复制余额表'),
 step('bff','gateway','GatewayProductService.RevealGatewayKey',[],'本人或显式grant且非系统托管Key，private/no-store','无权限拒绝；Secret不进入幂等response缓存')], '余额/Token费与工作区费各有原始来源；Key明文只一次性内存显示')
flow('F15','暂停/重新启用/删除恢复Tenant',['suspendTenant','reenableTenant','deleteTenant','restoreTenant','getTenantLifecycleOperation'],[A('tenant'),
 step('tenant','workspace','TenantWorkspaceCoordination.SuspendTenantWorkspaces',['workspace.operations'],'每Workspace原Tenant暂停子操作可读回','Tenant权限撤销不冒充资源已暂停'),
 step('tenant','workspace','TenantWorkspaceCoordination.ResumeTenantWorkspaces',['workspace.operations'],'只恢复原暂停、仍已付、原资源存在对象','过期/其他原因/删除对象skip，不续费重购'),
 step('tenant','workspace','TenantWorkspaceCoordination.DeleteTenantWorkspaces',['workspace.operations'],'每子删除证据明确','未完成不把Tenant操作标全部完成')], 'reenable与删除后15天restore独立；权限和每个应用结果分开显示')
flow('F16','旧资源和旧应用无损迁移',['adoptWorkspace','getSubscription','getWorkspace'],[A('workspace'),
 step('bff','workspace','WorkspaceProductService.AdoptWorkspace',['workspace.workspaces','workspace.deployments','workspace.operations'],'原资源/订阅/ID沿用，新的Agent契约符合旧资源','不执行购买debit/Ensure新资源，不伪造Quote或Build')], '历史resource_only可装Agent，已有应用/账期/数据/Key不被迁移暗改','数据Owner迁移按09停写屏障和单writer，不是此命令触发生产迁移')
flow('F17','运维、证据与资格读取',['listAdminOperations','reconcileOperation','listReceipts','listQualifications'],[A('receiving_owner'),
 step('bff','owning_service','OwnerOperations.Reconcile',[],'Owner按原operation真实读回和允许动作继续','不是任意setStatus succeeded'),
 step('owner','ledger','LedgerCoordination.ReadReceiptByReference',[],'确切receipt/来源/输入output摘要','缺证据保持未验证，不发生产部署')], '只读或受限原操作恢复，无新增Instance dispatch接口')
for f in flows:
 if f['featureId']=='F13':
  f['steps'].append(step('workspace','gateway','GatewayPlanChangeSettlement.RefundSupplementOnDeletion',['gateway.wallet_operations'],'本Owner枚举每个已applied补差，按各自T..E覆盖/原单剩余可退额分别结算','不把补差合并进base720；unknown保留该原单未决退款'))
  f['notes']+=' 基础单与每笔补差单分别计算/去重/读回，最后只聚合展示，不丢原单。'
 if f['featureId']=='F12':
  f['steps'].insert(2,step('gateway','workspace','WorkspacePlanChangeReadback.ReadNextPeriodObligation',[],'确认该期是否唯一绑定scheduled计划及已接受目标价格','不得人工/自动各造一单或按旧价先扣'))
  f['steps'].insert(3,step('workspace','gateway','GatewayPlanChangeSettlement.DebitScheduledPeriod',['gateway.wallet_operations'],'仅当nextPeriodObligation绑定降配时使用目标价一次扣款','无consent则awaiting_payment，不走普通旧价Debit'))
  f['notes']+=' 无计划走常规月费；有scheduled计划转F11目标计划分支，Fabric按已批准executionPlan决定续期/调配顺序。'
guards={'GatewayPlanChangeSettlement.DebitSupplement': 'kind=upgrade_immediate '
                                                '且upgradeCharge>0且原单未确认；zero跳过',
 'WorkspacePlanChangeReadback.ReadNextPeriodObligation': 'downgrade_next_period的付款/执行分支，或其取消未付款义务',
 'GatewayPlanChangeSettlement.DebitScheduledPeriod': 'kind=downgrade_next_period且有效下期付款授权；不得提前降低资源',
 'FabricCoordination.ResizeResources': 'upgrade资金confirmed/zero；或downgrade已到E且目标期资金confirmed',
 'WorkspaceProductService.CancelPlanChange': '用户显式取消且无新期资金accepted/资源执行',
 'GatewayPlanChangeSettlement.RefundFailure': '确定失败/fence/资源证据完备；非unknown；原单有未退额',
 'GatewayPlanChangeSettlement.RefundSupplementOnDeletion': '原升级已applied，此后Workspace正常删除已确认',
 'FabricRouteExecution.RollbackRoute': '新部署明确失败且旧数据/路由允许安全回滚',
 'GatewayCoordination.Refund': '原单可退且相应删除/失败证据完整，不是unknown',
 'TenantWorkspaceCoordination.SuspendTenantWorkspaces': 'suspendTenant入口',
 'TenantWorkspaceCoordination.ResumeTenantWorkspaces': 'reenableTenant入口，原停用/已付/原资源存在',
 'TenantWorkspaceCoordination.DeleteTenantWorkspaces': 'deleteTenant入口'}
for f in flows:
 for stage in f['steps']:stage['when']=guards.get(stage['rpc'],'按当前入口/阶段与06/13状态机前置执行；不是无条件调用')
for f in flows:
 if f['featureId']=='F12':
  for stage in f['steps']:
   if stage['rpc']=='GatewayCoordination.Debit':stage['when']='该nextPeriod无scheduled PlanChange，仅常规续费分支'
   if stage['rpc']=='FabricCoordination.RenewResources':stage['when']='无计划常规续期；有计划必须遵照已批准目标executionPlan，不独立先续旧高配'
data={'schemaVersion':1,'authority':'06 business closure projection; not a runtime engine','featureCount':len(flows),'flows':flows}
(root/'contracts/domain_flows.json').write_text(json.dumps(data,ensure_ascii=False,indent=2)+'\n')
lines=['## 17. 实际Domain协议逐链索引','','> 由checks/render_domain_flows.py核对实际proto生成；输入/输出类型是现有协议中的真实消息，不是另一套伪代码。每条写入前都执行公共授权与接收Owner资源/状态检查。']
for f in flows:
 lines+=['',f"### {f['featureId']} {f['title']}",'','入口：'+', '.join('`'+x+'`' for x in f['entryOperationIds']),'','| 索引 | 触发条件 / Caller→Owner / RPC | 输入→输出 | 接收Owner写入 | 完成证据 / 不确定处理 |','|---:|---|---|---|---|']
 for i,s in enumerate(f['steps'],1):
  lines.append(f"| {i} | {s['when']}；{s['caller']}→{s['owner']} / `{s['rpc']}` | `{s['request']}` → `{s['response']}` | {', '.join(s['writes']) or '只读/由原Owner管理'} | {s['completionEvidence']}；{s['onUnknownOrRejection']} |")
 lines+=['', '**终点**：'+f['terminal']]
 if f['notes']:lines+=['',f['notes']]
md=root/'06_data_flow_and_state_machine.md';text=md.read_text();marker='\n## 17. 实际Domain协议逐链索引'
if marker in text:text=text.split(marker)[0]
md.write_text(text+'\n'+'\n'.join(lines)+'\n')
print(f'Rendered {len(flows)} business flows / {sum(len(f["steps"]) for f in flows)} typed steps')
