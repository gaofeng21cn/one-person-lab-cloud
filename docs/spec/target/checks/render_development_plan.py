#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""Render concrete work packages from frozen contracts; not an implementation or task runner."""
from pathlib import Path
import argparse,json,subprocess,re,os
P=Path(__file__).resolve().parents[1]
# Inputs always come from this checkout; OP renders out to a separate tree so a
# freshness gate can byte-compare without mutating checked-in generated artifacts.
OUT=Path(os.environ['OPL_DEVELOPMENT_PLAN_OUT']).resolve() if os.environ.get('OPL_DEVELOPMENT_PLAN_OUT') else P
C=P.parents[2]  # The containing Cloud checkout, never a sibling provenance repository.
HOME=C.parent
parser=argparse.ArgumentParser(description=__doc__)
parser.add_argument('--check',action='store_true',help='Render freshness comparison bytes without Git provenance; requires an isolated output tree.')
options=parser.parse_args()
if options.check and (not os.environ.get('OPL_DEVELOPMENT_PLAN_OUT') or OUT.is_relative_to(C)):
    parser.error('--check requires OPL_DEVELOPMENT_PLAN_OUT outside the input checkout')
def display_path(value):
    path=Path(value)
    if not path.is_absolute():
        return str(path)
    for root,label in ((C,''),(HOME/'opl-instance-medopl','../opl-instance-medopl')):
        try:
            rel=path.relative_to(root)
        except ValueError:
            continue
        return str(Path(label)/rel) if label else str(rel)
    return str(path)
api=json.loads((P/'contracts/api_inventory.json').read_text())['operations']
db=json.loads((P/'contracts/db_inventory.json').read_text())['tables']
ui=json.loads((P/'contracts/ui_inventory.json').read_text())['features']
roots={'cloud':'.','console':'apps/console-ui','bff':'apps/console-bff','gateway':'services/gateway-integration','capability':'services/capability','build':'services/build','workspace':'services/workspace','runtime_control':'services/runtime-control','serve':'services/serve','resource_catalog':'services/resource-catalog','fabric':'services/fabric','ledger':'services/ledger','tenant':'services/gateway-integration','instance':'../opl-instance-medopl'}
W=[]
def task(n,title,coordinator,owners,features,start,accept,existing,new,deliver,tests,done):
    W.append(dict(id=f'W{n:02d}',title=title,coordinator=coordinator,owners=owners,features=features,startAfter=[f'W{x:02d}' for x in start],acceptAfter=[f'W{x:02d}' for x in accept],existingReadPaths=[str(Path(x)) for x in existing],plannedWritePaths=[str(Path(roots[r])/x) for r,x in new],deliverables=list(deliver),verification=list(tests),acceptance=list(done),state='not_implemented',apiPrimary=[],apiConsumes=[]))
G=['在拥有方Go module执行go test ./...；使用实际typed DTO与decoder','Cloud结构性/持久化/跨模块修改后npm run verify:local:full；仅普通源码改变用verify:local','该命令只作实施后要求；本轮没有执行新产品实现测试']
task(0,'目标入主文档与来源冻结','Cloud架构负责人',['cloud'],[f'F{i:02d}' for i in range(1,18)],[],[],['docs/README.md','docs/architecture.md','docs/decisions.md','docs/implementation-architecture.md','docs/status.md','docs/roadmap.md'],[('cloud','docs/architecture.md'),('cloud','docs/decisions.md'),('cloud','docs/roadmap.md')],['把已确认Agent+套餐目标、D17和逐域单writer迁移写入canonical owner，历史resource_only义务保留','记录Cloud/Instance/Runtime publisher精确sourceSHA与schema版本；形成实现起点差异清单，不把目标写成现状'],['对照00/09/13核对上下层无竞争writer','git diff --check'],['同一目标只一个writer，现行实现/目标/Instance事实分层','不重新征求已批准业务决定，不改变用户现有未提交文件'])
task(1,'生产契约与同仓版本一致性','Cloud契约负责人',['cloud'],[f'F{i:02d}' for i in range(1,18)],[0],[],['packages/contracts/README.md','packages/contracts/go/go.mod'],[('cloud','packages/contracts/proto'),('cloud','packages/contracts/go'),('cloud','tests/contracts')]+[('cloud',f) for f in ['services/control-plane/go.mod','services/control-plane/go.sum','services/fabric/go.mod','services/fabric/go.sum','services/ledger/go.mod','services/ledger/go.sum']],['按真实两端caller把03/proto/events/publisher/plan-change规格进入生产owner；不是整包inventories复制到runtime','锁定schema SHA和生成工具版本；每个consumer生成/校验typed client，只有owner定义业务类型','单仓消费者与契约来自同一Cloud commit，绑定schema SHA与生成工具版本；外部Owner才按精确commit/tag或digest接入，不用moving main/latest','沿用packages/contracts/go/go.mod；生成包为packages/contracts/go/v226，不为版本子包新建module；以锁定生成配置映射proto import到实际module路径，不手改生成代码','必要消费者go.mod/go.sum调整属于契约写集：记录实际依赖链与选定版本，不为旧测试或保持diff不变增加兼容字段、强压依赖或另建模块'],['编译Go/TS/protobuf客户端，按各自协议规则验证JSON/protobuf正反例、金额string/int64及source ms精度；仅生成成功不算消费者接入完成','在同一checkout重放生成并检查无漂移，运行受影响服务回归与npm run verify:local:full；规格验证不替代消费者验证','运行已有checks/validate_spec.py与validate_d17_contract.py作规格基线，不冒充实现测试'],['双方消费者同一契约版本，金额string/int64与source ms精度一致','契约升级在一组可回放变更中完成，不让旧测试驱动兼容字段'])
task(2,'独立服务启动、DB角色及共同操作协议','各服务Owner',['gateway','tenant','capability','build','workspace','runtime_control','serve','resource_catalog','fabric','ledger'],['F01','F17'],[1],[],['services/control-plane/go.mod','services/fabric/go.mod','services/ledger/go.mod','services/internal/postgresmigrate'],[(r,part) for r in ['gateway','capability','build','workspace','runtime_control','serve','resource_catalog'] for part in ['go.mod','go.sum','cmd/server','internal/transport','migrations']]+[('gateway','internal/identity'),('fabric','cmd/fabric'),('fabric','internal/http'),('fabric','internal/fabric/ent_migrations'),('ledger','cmd/ledger'),('ledger','internal/http'),('ledger','internal/ledger/ent_migrations')],[ '按01同仓目录启动独立服务module/进程/DB角色；tenant与gateway同module同部署单元但不同DB角色，不按每个RPC拆服务','每Owner实现自己的Operation、幂等、Outbox/Inbox、健康/就绪与迁移入口；公共基础设施仅在有两个真实caller时共享','为本地测试准备各Owner独立database/角色及隔离fixture进程；不以同schema代替跨Owner权限隔离，不复制钱包或造生产test-billing入口'],G,['DB角色只写本域，无跨库FK/JOIN；同一writer无双写','readiness真实暴露依赖不就绪；未知结果不默认成功'])
task(3,'身份、Tenant成员及服务间授权','Gateway Integration/CloudIdentity',['tenant','gateway'],['F01'],[2],[],['services/control-plane/internal/server/routes_auth.go','services/control-plane/internal/clients/sub2api.go'],[('gateway','internal/identity'),('gateway','internal/authorization')],['Gateway个人登录→Cloud session/Tenant/membership；平台权限与Tenant角色分离','实现AuthorizeAction、OwnerCommitReadback验证、accepted-operation grant、权限版本/受众及撤权','实现邀请/角色/最后owner保护；开发fixture使用已知测试Tenant，不向外部自动注册钱包'],G,['跨Tenant/伪actor/伪audience/撤权拒绝；session退出不丢原已接受义务','前端不收到服务token，不保存密码，future consent不从role缓存猜'])
task(4,'Gateway资金、Key与用量适配','Gateway Integration',['gateway'],['F08','F12','F13','F14'],[3],[],['services/control-plane/internal/clients/sub2api.go','services/control-plane/internal/server/wallet_adjustment.go'],[('gateway','internal/sub2api'),('gateway','internal/wallet'),('gateway','internal/keys')],['沿用Sub2API真实接口/身份和一次性Code，完成base/supplement/period/refund原单读回','仅映射/操作证据，无余额镜像；unknown禁止新财务副作用','个人Key管理与托管Workspace Key边界明确；明文仅授权一次性响应'],G,['重复/丢响应/余额并发/错原单/错钱包/超额及unknown退款全部用真实decoder验证','不用余额差作为付款证明，不在普通CI触碰真钱'])
task(5,'Ledger类型化证据与读回','Ledger',['ledger'],['F05','F08','F11','F12','F13','F16','F17'],[2],[],['services/ledger/internal/ledger/types.go','services/control-plane/internal/clients/ledger.go','services/ledger/ent/schema'],[('ledger','internal/ledger'),('ledger','ent/schema'),('ledger','internal/ledger/ent_migrations')],['支持Build无Workspace收据、新计划变更/补差/迁移证据；精确原单和输入输出验证','保留旧receipt字节/类型/ID与幂等义务，新索引只投影','提供管理员只读证据和资格投影，不让Ledger编排业务'],G,['缺必填/伪Workspace/相同键不同hash/秘密泄露拒绝','append-only/权限/retention真实DB检查；没有运行证据不显示qualified'])
task(6,'资源、价格与批准政策目录','Resource Catalog',['resource_catalog'],['F03','F07','F11'],[2],[11],['services/control-plane/internal/server/routes_billing.go','packages/contracts/go/workspace_delete.go'],[('resource_catalog','internal/catalog'),('resource_catalog','internal/pricing'),('resource_catalog','migrations')],['资源plan先创建，组合价格后创建；版本/有效期/approved transition具有明确owner','实现D17固定政策和base退款政策分离、实际周期/单次ceil、canonical毫秒，不写任意公式框架','客户列表只投影有效已批准组合，容量仍由Fabric确认'],G+['重放13及checks/d17_acceptance_vectors.json的独立数值、纳秒进位与int64边界'],['无双重取整/浮点/毫秒从PG重算；新购买周期1个月，历史周期不改','管理员准入目录不执行资源采购'])
task(7,'发布者空间与Runtime/WebUI准入','Capability',['capability'],['F03'],[3],[],['packages/contracts/go/workspace_application.go','services/control-plane/internal/server/workspace_application_admission.go'],[('capability','internal/publishers'),('capability','internal/catalog')],['官方/第三方publisher namespace、registry prefix、完整不可变PublisherContract','复用当前WorkspaceApplicationRevision/Go validator，不另造缩减版端口/探针/Secret模型','默认构建策略只选择批准版本，新发布不自动替换旧Workspace'],G+['两类Runtime真source validator/startup DAG及schema拒绝反例'],['完整repository/digest/platform/recipe/revision来源可回读','第三方不能混入官方prefix；无新SSO或固定Runtime路径假设'])
task(8,'Namespace、Package上传及输入claims','Capability',['capability'],['F02','F04','F06'],[3,7],[],['services/control-plane/internal/server/application_revision_store.go'],[('capability','internal/packages'),('capability','internal/uploads'),('capability','internal/references')],['分组/Package/版本与受限Storage直传、实际摘要校验及续传','四类ReferenceTarget，Acquire→consumer本地保存→Bind；释放须真实Owner usage/终态','归档保留历史，目录tombstone与物理删除分开；无过期自动释放引用'],G,['首包无CapabilityVersion仍可保护Package/Runtime/WebUI输入','路径穿越/错摘要/跨Tenant/过期签名/并发删除/Bind丢响应均覆盖'])
task(9,'真实构建、注册和可部署版本','Build（协调），Capability（资产writer）',['build','capability'],['F04','F05','F06'],[5,8],[],['docs/agent-lifecycle.md'],[('build','internal/jobs'),('build','internal/builder'),('build','internal/registry'),('capability','internal/artifacts')],['冻结输入与recipe，真实BuildKit构建及Registry exporter/readback','typed完成事件→Capability Inbox事务创建唯一ready版本；重试新job保留旧历史','管理员/客户查询构建日志及版本、引用保护下架；无用户自定义任意脚本旁路'],G,['真实Storage+Registry+BuildKit隔离集成；worker重启、push已成但丢响应、注册ACK丢失','输出digest/descriptor可被后续Runtime读取；不以fixture假Build完成'])
task(10,'Runtime Release注册、准入与精确引用','Runtime Control',['runtime_control'],['F03','F04','F05'],[2,7],[],['docs/architecture.md','packages/contracts/proto/internal.proto'],[('runtime_control','internal/releases'),('runtime_control','internal/catalog'),('runtime_control','migrations')],['注册并审核OPL App/Framework Runtime Release；保存不可变artifact digest、ABI/Package兼容契约、状态与准入证据','向Build提供精确RuntimeRelease引用；只读回版本目录，不部署Workspace Agent、不拥有Runtime实例或readiness','撤销/退役只影响新Build准入，已固定Build输入和历史证据不被改写'],G,['相同digest/兼容契约/状态转换与Build引用反例','Runtime目录读回不得被解释为Agent已部署或已运行'])
task(11,'Local-Docker资源开通、绑定与资格能力','Fabric Local-Docker',['fabric'],['F07','F08','F11','F12','F13','F16','F17'],[2],[],['services/fabric/internal/fabric/local_docker_provider.go','services/fabric/internal/fabric/provider_port.go'],[('fabric','internal/fabric/local_docker_provider.go'),('fabric','internal/fabric'),('fabric','internal/http')],['只负责Compute/Storage/Network/Secret资源的admit、ensure、resize、renew、suspend、resume、delete与状态读回','实现Local-Docker资源集、挂载和配额资格；把资源引用交给Serve，不部署Agent OCI、不控制Runtime生命周期、不拥有访问路由','Linux存储/权限前提不满足明确失败，不把Mac Docker Desktop冒充Local资格'],G,['Linux隔离集成：真实volume数据、资源动作丢响应、unknown不重发','资源only路径不伪造Agent readiness，零金额路径无Gateway动作'])
task(12,'Tencent/TKE资源适配与资格读回','Fabric Tencent',['fabric'],['F07','F08','F11','F12','F13','F17'],[2],[],['services/fabric/internal/fabric/tencent_provider.go','services/fabric/internal/fabric/tencent_provider_storage.go','services/fabric/cmd/opl-tencent-provisioner'],[('fabric','internal/fabric'),('fabric','cmd/opl-tencent-provisioner')],['只负责Tencent/TKE Compute/Storage/Network/Secret资源能力、预付执行计划、provider readback与资源变更','报价前固定已资格的in-place或目标pool+原CBS重绑策略；禁止POSTPAID_BY_HOUR；把已确认资源引用交给Workspace/Serve','不负责Agent OCI部署、Runtime启停/readiness或Serve访问路由；ordinary CI无真钱采购/销毁'],G,['SDK请求和原请求读回边界使用typed tests；真正变更仅W29授权Instance验证','不能把SDK RequestId或kubectl接受当资源完成'])
task(13,'BFF和Console基础接入','Console/BFF',['bff','console','workspace'],['F01','F17'],[1,3],[],['apps/console-ui/src/app/console-router.ts','apps/console-ui/src/layout/ConsoleShell.tsx','apps/console-ui/src/api/auth-api.ts','apps/console-ui/src/app/use-console-controller.ts'],[('bff','go.mod'),('bff','go.sum'),('bff','cmd/server'),('bff','internal/session'),('bff','internal/routes'),('console','src/api'),('console','src/layout/ConsoleShell.tsx'),('console','src/app/console-router.ts')],['同源REST/session/CSRF/Origin→typed Owner RPC；Operation按owner路径路由','保留当前React/TypeScript/tokens；按11导航和状态/响应式/焦点，不按Domain铺菜单','secret no-store与内存清理；所有异步读取遵守owner提示，不自行认定成功'],["go -C apps/console-bff test ./...",'npm run typecheck','npm run test:browser:console-owner-reads','真实BFF会话/跨Tenant/退出缓存清理测试'],['BFF不写业务表、不拥有Saga/钱包；未知Owner拒绝不遍历猜测','旧/api与新/api/v2按明确切换，不请求失败再fallback'])
task(14,'智能体/上传/构建/目录前端','Console',['console'],['F02','F03','F04','F05','F06'],[13],[6,7,8,9],['apps/console-ui/src/pages/CustomerPages.tsx','apps/console-ui/src/pages/AdminPages.tsx'],[('console','src/pages/agents'),('console','src/app/agent-controllers'),('console','src/api/capability-api.ts')],['按04/11实现Agent目录、版本、分组、上传向导、构建日志/重试及publisher表单','fixtures只用于并行开发；最后换真实BFF读写并验证关闭/刷新恢复','目录下架/物理清除文案不混，技术详情只给有权限的角色'],['新增typed controller/model与browser用例','npm run typecheck','按F02-F06正常/错误/重复/越权/刷新验收'],['首包与新版本都可用；选择WebUI确实进入产物','不把字段存在、静态原型或传输100%当构建完成'])
task(15,'准确报价与Agent+套餐Launch闭环','Workspace（协调）/Catalog/Serve',['workspace','resource_catalog','serve'],['F07','F08'],[4,5,6,9,10,11],[],['services/control-plane/internal/server/routes_workspace_launch.go','services/control-plane/internal/server/workspace_launch_service.go','services/control-plane/internal/server/workspace_launch_reconciler.go'],[('workspace','internal/admission'),('workspace','internal/launch'),('resource_catalog','internal/quotes'),('serve','internal/delivery')],['Workspace只负责准入、报价接受、原单/权益/资源计划；Fabric确认资源后将OCI digest与resource refs交给Serve','Serve创建该Workspace唯一Agent delivery，执行部署/readiness/access并返回真实读回；Workspace通过ServeAgentCoordination.Reserve/Deploy发起，不写Agent deployment/current selection','本地真实链覆盖Capability→Build→Workspace→Fabric→Serve→API/Embed/Hosted UI；每阶段原身份/原单/epoch持久化，已接受报价不因执行耗时重新计价'],G+['本地真实服务链+外部权威fixture：余额不足、扣款丢响应、资源/receipt不确定、restart','同一次请求串API→Owner→DB→下一Owner→实际应用响应'],['一个Workspace一次原单且最多一个当前Agent；ready来自Serve实际Agent而非资源开通','失败收尾/unknown/退款分开展示，拒绝跨Tenant与伪价格'])
task(16,'部署向导与Workspace页面','Console',['console'],['F07','F08','F09','F10','F16'],[13],[15,17,25],['apps/console-ui/src/app/use-workspace-launch-controller.ts','apps/console-ui/src/app/workspace-launch-controller-model.ts','apps/console-ui/src/api/workspaces-api.ts'],[('console','src/pages/workspaces'),('console','src/app/workspace-launch-controller-model.ts'),('console','src/app/use-workspace-launch-controller.ts'),('console','src/api/workspaces-api.ts')],['版本/套餐/模型→固定报价→确认→真实进度；修改选项使旧quote失效','部署/版本/访问读取走Serve API，经BFF组合后呈现；Workspace UI不写Agent deployment或current selection','详情并列权益/资源/应用/资金结果，owner凭据一次性查看','原资源adopt和已部署legacy应用有独立入口，不重购'],['npm run test:browser:workspace-lifecycle','npm run typecheck','新页面controller与浏览器丢响应/刷新/窄屏/键盘测试'],['按钮条件、错误和场景与04/11完全同词','生产UI不执行价格算法，准确显示最多6位微美元'])
task(17,'Serve Agent交付、版本切换与访问回滚','Serve',['serve'],['F09','F10','F16','F17'],[9,11,15],[],['docs/opl-serve.md','docs/architecture.md','packages/contracts/go/workspace_application_runtime.go'],[('serve','internal/delivery'),('serve','internal/runtime_adapter'),('serve','internal/access'),('serve','migrations')],['Serve接收Workspace授权、OCI digest、Fabric resource refs与expected current deployment，创建唯一Agent deployment并持久化部署/运行/访问事实','通过ServeAgentCoordination、ServeRuntimeAdapter、ServeAccessControl完成部署、readiness、模型reload、Fence/Activate/Observe/Rollback；同一Workspace最多一个当前Agent','API/Embed/Hosted UI只读Serve当前Agent；失败回滚保留原路由与数据证据，Workspace不写deployment/current selection，Fabric不写readiness/access'],G+['真实HTTP根路径/资产/API/SSE/cookie隔离与数据持久化','晚到worker/旧epoch/不同target、data不可逆和多写挂载拒绝'],['一个确认选中部署，appliedVersion/readiness/access与实际读回一致','没有隐式自动升级、重新购买或第二份Workspace部署真相'])
task(18,'周期、显式续费授权和到期停用','Workspace',['workspace','gateway','fabric'],['F12'],[4,5,15],[],['services/control-plane/internal/server/workspace_renewal.go','services/control-plane/internal/server/monthly_billing.go','services/control-plane/internal/server/renewal_worker.go'],[('workspace','internal/subscriptions'),('workspace','internal/renewal')],['保留source billingAnchorDay/原paidThrough/自动授权；周期唯一资金义务','到期不免费运行；原单/原资源续期回读后提交新周期','手动/自动/恢复worker竞争同一义务，不因session过期丢原授权'],G+['Jan31→Feb28→Mar31及闰年；窗口已过拒绝；source纳秒不回写','人工/自动并发、资金unknown、停用后恢复、provider资源回收'],['一周期最多一原单；不偷偷从now重新开始一月','关闭自动续费只影响未来未接受义务'])
task(19,'D17套餐变更与补差账务','Workspace（协调）/Catalog',['workspace','resource_catalog','gateway','fabric','serve','ledger'],['F11','F12','F13'],[6,11,12,15,18],[],['services/fabric/internal/fabric/provider_port.go','services/control-plane/internal/server/workspace_renewal.go'],[('workspace','internal/plan_changes'),('resource_catalog','internal/plan_change_pricing'),('gateway','internal/settlement'),('fabric','internal/fabric')],['PlanChange第一类实体：立即upgrade与scheduled downgrade区分初次/执行Operation','固定T、canonical毫秒、单次ceil、原E不变；下期目标价及取消/锁定边界按13','supplement失败全退及成功后T..E未用退款，与base720分开；原单防超额/unknown','复用W18 worker执行due计划，不新建scheduler服务或资金预占钱包'],G+['重放13及全部D17数值/并发/PG纳秒进位/原单退款用例'],['20→40剩半期补10；31天例子19.354839；资源+运行确认才applied','scheduled当前不扣/不减；无下期授权awaiting_payment；future paid/mixed/shrink拒绝清楚'])
# W20's current implementation slice stays on the existing owner seams:
# Workspace deletion/recovery is implemented under launch, while the Gateway
# action grant is implemented by the gateway identity owner. Keep the plan
# aligned with the live caller/write set; do not move code merely to satisfy a
# speculative future directory or create a second settlement writer.
task(20,'删除、base与supplement原单结算','Workspace（协调）',['workspace','gateway','fabric','serve','ledger'],['F13'],[4,5,15,18],[19],['services/control-plane/internal/server/workspace_delete.go','services/control-plane/internal/server/workspace_delete_refund.go','services/control-plane/internal/server/wallet_adjustment.go'],[('workspace','internal/launch'),('gateway','identity')],['Workspace持久化删除意图并协调原单/退款；Serve负责Agent retirement、访问撤销与absence readback；Fabric负责资源删除事实','base保留原policy；已applied升级supplement用自身coverage，不合并当整月款','与续费/plan change/rotation串行，不撤销用户保留Key，不删Package/Build历史','当前实现按workspace与gateway两个owner写集拆分；交汇时使用互认coauthor admission和各自append-only receipt'],G+['资源已删丢响应、原身份/receipt错配、refund unknown/超额、窗口/退款上界','UI同时能显示已删+退款待确认；没有手工set success接口'],['无证据不退款，已删除不等于退款到账','不靠列表未找到或钱包余额差证明结果'])
task(21,'Tenant开通、暂停/重新启用及删除恢复','CloudIdentity（协调）',['tenant','gateway','workspace','serve'],['F01','F15'],[3,4,15,20],[],['services/control-plane/internal/server/routes_admin.go'],[('gateway','internal/tenant_lifecycle'),('workspace','internal/tenant_actions')],['创建/绑定账单主体与个人登录分开；暂停权限及子Workspace结果可追踪','reenable只恢复原暂停且仍已付/原资源存在对象；删除后15天恢复是独立操作','删除资产转平台私有托管，不公开、不自动退款全部余额或重购资源'],G+['暂停后直接reenable不要求restoreUntil；误恢复/过期/子任务unknown','成员失权、生效窗口及账户历史权限不放大'],['Tenant权限和各Workspace运行结果分开','没有跨Tenant资源管理和删除后假复活数据'])
task(22,'费用、计划、续费/退款与Key前端','Console',['console'],['F11','F12','F13','F14'],[13],[4,18,19,20],['apps/console-ui/src/app/use-billing-controller.ts','apps/console-ui/src/app/use-workspace-renewal-controller.ts','apps/console-ui/src/api/workspaces-api.ts'],[('console','src/pages/billing'),('console','src/app/plan-change-controller'),('console','src/api/plan-change-api.ts')],['立即升配quote/进度、下期计划/取消/付款边界，原E和资金/资源状态分开','精确BigInt金额展示而非业务重算；31天19.354839不可隐藏实际更多小数','订阅/交易/Token各有原始来源；Key与应用密码只内存/离页清理'],['npm run test:browser:billing','npm run test:browser:gateway-usage','D17真实BFF浏览器与320/390px回归'],['不把预约持久化当资源已降配；unknown不建议重新付款','取消/删除确认对象范围明确，旧单/新补差不混'])
task(23,'管理员目录、Tenant与运维前端','Console',['console'],['F03','F15','F17'],[13],[6,7,21,24],['apps/console-ui/src/pages/AdminPages.tsx','apps/console-ui/src/pages/OperatorRuntimeObservations.tsx'],[('console','src/pages/admin'),('console','src/app/admin-controllers')],['完整publisher schema表单/有效namespace，资源/价格版本不得改写已接受快照','Tenant子操作/访问恢复分离，技术证据仅授权详情','只读资格/操作查询，不能从Console dispatch生产部署或直接改状态'],['npm run test:browser:operator-account','新增admin目录/权限/危害确认browser tests'],['普通owner无platform_admin权限；无伪JSON表单','缺证据显示未验证，不用文案冒充资格通过'])
task(24,'Owner运维、原操作恢复和可观测性','各Owner/Console集成',['workspace','tenant','ledger','bff'],['F17'],[3,5],[15,19,20,21],['services/control-plane/internal/server/operational_alerts.go','services/control-plane/internal/server/routes_admin.go'],[('bff','internal/operations'),('workspace','internal/operations')],['每Owner依据原意图/readback实现Reconcile，BFF仅路由，不能统一setStatus','结构化request/operation/phase、unknown、队列/重试/陈旧观察告警和脱敏日志','恢复手册列精确前置/允许动作/不可逆边界，容量参数从实例配置'],G+['日志canary-secret不泄露；错误/告警只绑定脱敏身份','无个人凭据操作、越权Owner路由、过期readback拒绝'],['运营能知道真实失败点与唯一下一动作','无新全局事件总线/中央workflow/第二lock authority'])
task(25,'逐域数据转换与迁移演练','各数据Owner/Instance',['tenant','capability','build','workspace','runtime_control','serve','fabric','gateway','ledger'],['F16'],[0,2],[9,17,19,20,21],['services/control-plane/ent/schema/shared.go','services/control-plane/migrations','services/fabric/ent/schema','services/ledger/ent/schema'],[('cloud','tools/migration'),('workspace','internal/migration'),('serve','internal/migration')],['按09对每张源表/字段确定target或保留档案，不丢未知历史','legacy_resource_only/legacy_application/legacy_import保留真实ID/原单/时间、未决义务/补差coverage','Serve deployment/access/runtime observation迁移到serve Owner并退出旧部署writer，不复制Workspace current-agent字段','隔离副本试迁移、hash/金额/周期/权限对账、single-writer fence及回滚演练'],['只在隔离副本运行转换，生产数据仅Instance保护runner','row/ID集合、金额/时间/quote/receipt/user权限等值和未决操作重放'],['无假Build/Quote/重购；不做双写或失败时切旧接口','反向不保真就不宣称可回滚，原证据不能覆盖'])
task(26,'真实业务链集成与浏览器验收','集成负责人+各Owner',['bff','console','workspace','capability','build','gateway','fabric','runtime_control','serve','ledger'],[f'F{i:02d}' for i in range(1,18)],[9,13,15],[14,16,17,19,20,21,22,23,24,25],['tests/integration','tests/ui','tools/local-sub2api-authority-fixture.ts'],[('cloud','tests/integration'),('cloud','tests/ui'),('cloud','tools')],['前端真实BFF→真实Owner DB/RPC→Local provider/应用；业务链固定为Capability→Build→Workspace→Fabric→Serve→API/Embed/Hosted UI；外部财务只用明确隔离authority fixture','以17F逐项执行正常/拒绝/丢响应/重启/重复/越权/并发/取消/恢复向量','性能记录实际实例输入和测量值，不创造50人/3分钟承诺'],['npm run verify:local:full','npm run test:browser:suite','持续运行应用/上传真实样例构建及金额账期cross-tests'],['功能、接口、DB、页面呈现同一结果；原型不替代浏览器真链','每项证据说明实测和未测，不将fixture等同真实钱/provider'])
task(27,'可移植候选构建与CI契约演进','Cloud发布机制Owner',['cloud','serve'],['F17'],[1,2],[9,12,21,24,26],['Dockerfile','compose.yaml','deploy/portable','.github/workflows/build-opl-cloud-candidate.yml','.github/workflows/release-opl-cloud-image.yml','packages/contracts/opl-cloud-candidate-receipt-contract.json','packages/contracts/opl-cloud-distribution-contract.json'],[('cloud','.github/workflows/build-opl-cloud-candidate.yml'),('cloud','deploy/portable'),('cloud','packages/contracts/opl-cloud-candidate-receipt-contract.json'),('cloud','packages/contracts/opl-cloud-distribution-contract.json')],['目标多服务制品纳入同一个Cloud Candidate/安装清单与同字节资格单位；不另起release authority','固定Cloud SHA及各参与Owner sourceSHA、镜像digests/平台、契约revision、配置schema、全部assets checksum；无latest/main输入','更新现有Candidate/qualification/release消费者与实例接收校验，历史v2单镜像Candidate可读但不是第二当前writer','CI只有code/build/sandbox测试权限；构建Candidate不等于正式发布或Instance部署'],['contracts/工具生成/拒绝反例与portable Compose一致检查','假造digest/遗漏组件/漂移source/schema拒绝；构建不需要生产环境'],['候选集合是不可变单位，任何组件变更必须新Candidate重新适用资格','Release只提升已资格同字节，不rebuild；原发布者权限边界不变'])
task(28,'干净Linux Local资格','Cloud/Local qualification',['cloud','fabric'],['F17'],[27],[],['.github/workflows/clean-host-qualification.yml','tools/local-workspace-qualification.ts'],[('cloud','.github/workflows/clean-host-qualification.yml'),('cloud','tools/local-workspace-qualification.ts')],['固定Candidate在合格Linux安装，执行启动/存储/应用/生命周期与重启/数据保留','记录image/platform/config摘要与真实readback，失败不得包装PASS'],['干净Linux exact-candidate资格；不以Mac Desktop代替','本轮仅计划命令，后续实际执行再出receipt'],['Local receipt绑定同一候选及provider条件','不买真实Tencent资源、不调用真实客户钱包'])
task(29,'Instance/Tencent资格与受限业务实测','opl-instance-medopl Owner',['instance'],['F17'],[28,12,19,25],[],['docs/runtime/release.md'],[('instance','receipts'),('instance','.github/workflows')],['在既有保护workflow/授权runner中采用同一Candidate和真实provider profile','若验收需要付费/采购/销毁，先有独立明确金额/资源/账户范围授权；Cloud本机不进生产私网','验证真实Agent使用、允许调配策略、升级/下期降配、原单读回与数据恢复/迁移样本'],['Instance现有receipt validators、真实runtime/账务/provider读回','精确scope/hash/时间/cleanup；只测试获得授权的案例'],['与Local同候选字节；不能以公网404/Pod Running作完整合格','未拿授权或必要凭据则记录未执行，不产生假资格'])
task(30,'生产客户逐批切换与旧writer退休','Instance+数据Owner',['instance','workspace','gateway','fabric','ledger'],['F16','F17'],[25,26,29],[],['docs/status.md','docs/roadmap.md'],[('instance','receipts'),('cloud','docs/status.md'),('cloud','docs/roadmap.md')],['按09 M0-M5：写屏障/终态增量/路由epoch/实际用户读回/分批接收','未决对象保持唯一原Owner，不先切新再失败fallback；新写后回退须可证明无损','业务/数据接收完成后退休旧writer；旧历史只读保留义务'],['每批数据/资金/资源/权限对账和真实浏览器故事','Instance采用/回滚新不可变receipt'],['不丢原单/Key/数据，零重复收费；未完成批次明确保留','本步骤与Cloud正式发布不同，不从Cloud dispatch实例切换'])
task(31,'同字节正式发布与文档收尾','Cloud发布Owner',['cloud'],['F17'],[28,29],[],['.github/workflows/release-opl-cloud-image.yml','.github/workflows/release-opl-cloud-public-readback.yml','docs/runtime/release.md','docs/status.md','docs/roadmap.md'],[('cloud','.github/workflows/release-opl-cloud-image.yml'),('cloud','docs/status.md'),('cloud','docs/roadmap.md')],['仅在明确正式发布授权后，由允许actor从main提升同一资格制品，不重建','实际公共制品/digest/资产回读；Cloud状态和外部Instance义务分别记录','实施后更新canonical文档，完成工作清单转历史，保留未完成gap'],['当前release owner校验器与public artifact readback','Local+Instance同候选证明；没有授权不dispatch'],['只有owner及RenDeHuang符合既有发布权限，original publisher不被替换','产品发布不要求所有客户迁移已完成；但声明的迁移兼容性须有W25/W29证据'])
# The current loader chain is part of migration, not an unused SQL display copy.
W[25]['existingReadPaths'] += ['services/control-plane/internal/server/ent_state_store.go','services/control-plane/migrations/migrations.go','services/fabric/internal/fabric/ent_migrations','services/ledger/internal/ledger/ent_migrations']
W[25]['deliverables'].append('修改真实migration加载链：CP ent_state_store→migrations.Apply*；Fabric/Ledger各自internal Owner的ent_migrations。不得只改展示SQL。')
# September 29 adopted scope. Whole-W completion and useful implementation slices
# are deliberately different graphs: a slice consumes named producer outcomes,
# never a fixture-only PASS or completion of every future feature in that W.
W[0]['deliverables'][0] = '把12已确认默认App/可选Agent、TKE主线、D17和逐域single-writer移交同步canonical owner；新默认不等于历史resource_only'
W[1]['deliverables'].extend([
 '先贯通03 x-approved-wire-migration与02第0节：应用来源判别联合、Agent必选独立WebUI、descriptor provenance、claims、Quote/Serve持久化与真实消费者同版交付',
 '同步已有TenantRepositoryBinding生产RPC与规格、明确api/v226生成映射；修Runtime REST owner和per-owner Operation路由，不复制第二套合同',
 '默认App不得伪造Package/Build/CapabilityVersion/Build receipt；Agent必须保留Package/Runtime/独立WebUI三个真实输入及claim'])
W[1]['plannedWritePaths'] += ['docs/spec/target/02_database_schema_complete.md','docs/spec/target/03_api_contract_complete.yaml','docs/spec/target/contracts','services/runtime-control/migrations','services/capability/migrations','services/build/migrations','services/resource-catalog/migrations','services/workspace/migrations','services/serve/migrations']
W[1]['deliverables'][3] = '沿用packages/contracts/go/go.mod；W01核对已运行api协议与旧v226规划的精确差异后统一生产/规格/生成消费者，不机械改包名、不另建module或并行协议副本'
W[6]['acceptAfter']=['W12']
W[9]['startAfter']=['W05','W08','W10']
W[9]['deliverables'] += ['Agent固定Package+Runtime+独立WebUI三项确切输入；默认App直接用获准Release，不创建Build']
W[10]['deliverables'] += ['准入指定OPL App源的standalone/native UI发布事实，供默认App的报价/Serve读取与claim保护；批准不等于镜像已实际可用']
W[15]['title']='准确报价与应用＋套餐自动交付'
W[15]['startAfter']=['W03','W04','W05','W06','W10','W12']
W[15]['deliverables'][1]='Serve的W17.first-delivery先提供Reserve/Deploy/Observe/Activate；Workspace消费它，不把首次部署实现复制到Workspace'
W[15]['deliverables'][2]='默认App与自定义Agent共用原单/授权/资源/Serve链；默认App不依赖Build、Package或独立WebUI；已接受报价不因耗时或策略变化重新定价'
W[15]['verification'] += ['同一默认App订单：无Package/Build/CapabilityVersion也能达到真实Serve访问；另验Package+Runtime+独立WebUI构建的Agent，不以Local零收费替代Tencent新购']
W[15]['acceptance'][0]='一个Workspace一次原单、至多一个当前应用；默认App和Agent均以Serve实际可用及必需receipt确认完成，不以资源ready完成'
W[17]['startAfter']=['W01','W02','W10','W12']
W[17]['acceptAfter']=['W15']
W[17]['deliverables'] += ['先独立实现首次TKE交付供W15消费，再完成显式更新/替换/回滚；承接Fabric旧TKE应用能力并切真实caller后才退休旧writer',
 '默认App runtime_release provenance与Agent build provenance共用Serve聚合；TKE应用Deployment/Service/route由Serve写，CVM/CBS/attachment归Fabric']
W[21]['startAfter']=['W03','W04']
W[21]['acceptAfter']=['W15','W20']
W[21]['deliverables'] += ['W21.tenant-admission先交付开通/owner membership/已支持钱包主体与repository binding，供首链使用；暂停/删除/恢复作为后续切片，不以预置测试Tenant代替产品开通']
W[26]['startAfter']=['W13','W15']
W[26]['deliverables'][0]='真实Console/BFF→owner/DB/RPC→资源→Serve→应用；分别验默认App、Package+独立WebUI的Agent；每项标source/Local/TKE与真实或隔离财务边界'
W[27]['deliverables'] += ['W27.runnable-candidate提供早期可执行同字节候选供TKE首链，不等待全F验收；W27全包完成与W31发布仍满足现有综合资格']
W[29]['deliverables'] += ['早期W29.default-tke/W29.agent-tke是受限首链证据，不冒充W29全生命周期/迁移/同Candidate Local资格；Instance仅部署Cloud并运行Cloud-owned验收器，不逐用户编排']
# Correct planned destinations to current owner directories; this is not a claim
# that all code in a directory has completed the work package.
W[7]['title']='发布者空间与WebUI准入'
W[7]['plannedWritePaths']=['services/capability/catalog']
W[8]['plannedWritePaths']=['services/capability/catalog','services/capability/migrations']
W[9]['plannedWritePaths']=['services/build/internal/build','services/capability/catalog']
W[10]['plannedWritePaths']=['services/runtime-control/catalog','services/runtime-control/migrations']
W[15]['plannedWritePaths']=['services/workspace/internal/launch','services/resource-catalog/catalog','services/serve/internal/delivery']
W[4]['plannedWritePaths']=['services/gateway-integration/identity','services/gateway-integration/migrations']
W[3]['plannedWritePaths']=['services/gateway-integration/identity','services/gateway-integration/cmd/server']

slices=[]
def delivery_slice(key, window, deps, writes, input_refs, output, checks, evidence, tests=None):
    slices.append(dict(id=key,workPackage=key.split('.')[0],window=window,startAfter=deps,
        writePaths=writes,**({'testWritePaths':tests} if tests else {}),inputRefs=input_refs,deliverable=output,verification=checks,
        completionEvidence=evidence,
        onUnknown='按原owner Operation/effect identity读回；结果未明不新建副作用；已完成副作用但回执丢ACK则以原evidence key/hash重投并读回。'))
delivery_slice('W01.application-contracts','integration',[],
 ['docs/spec/target/02_database_schema_complete.md','docs/spec/target/03_api_contract_complete.yaml','docs/spec/target/contracts','packages/contracts/proto','packages/contracts/go','services/*/migrations','services/resource-catalog/catalog','services/workspace/internal/launch','services/runtime-control/catalog','services/capability/catalog','services/build/internal/build','services/serve/internal/delivery','services/ledger/internal/ledger','apps/console-bff/internal/httpapi','docs/status.md','docs/roadmap.md'],
 ['12产品组合','03 x-approved-wire-migration','02第0节','06异步交接'],
 '默认App/Agent与Agent必选独立WebUI的typed合同、SQL约束/加载链、真实decoder和全部消费者一致；含Tenant binding既有漂移',
 ['default无Package/Build/CapabilityVersion正例','混合来源/伪WebUI/错误digest/原单替换拒绝','go/proto/JSON同版消费者编译与DB约束（此切片不要求真实provider部署）'],
 '精确source/schema/generator hashes与聚焦消费者测试；同步docs/status.md和docs/roadmap.md中的本次源码事实，不把源码验收写成部署完成；移除03 pending迁移标记前须实际贯通，不只生成成功')
delivery_slice('W02.owner-readiness','integration',['W01.application-contracts'],
 ['services/internal/ownerservice','services/*/cmd','deploy/portable','Dockerfile'],
 ['各owner实际启动条件','mTLS与DB/role合同'],
 '身份/DB/peer/readiness真实，Cloud镜像含buildx及必需owner；Tenant自身授权与Operation也可用',
 ['真实PG独立角色和mTLS调用','不可用依赖不能SERVING','镜像内CLI/插件/entrypoint检查'],
 '源码/镜像工具及owner启动证据；不等于TKE部署')
delivery_slice('W03.identity','business',['W02.owner-readiness'],
 ['services/gateway-integration/identity','services/gateway-integration/cmd/server'],
 ['CloudIdentityAuthorization','实际Sub2API登录合同'],
 '个人身份、membership、accepted operation grant、跨租户/撤权边界可用',
 ['真实BFF登录到owner','session过期后原已接受操作恢复','伪actor/audience拒绝'],
 '真实身份链及拒绝测试；外部fixture明确标注')
delivery_slice('W04.payment-key','business',['W03.identity'],
 ['services/gateway-integration/identity','services/gateway-integration/migrations'],
 ['GatewayCoordination','原Code/账户/金额/Key读回','06财务阶段'],
 '受支持的Tenant钱包授权、原单扣退款/Key创建与Secret引用交接；不以用户余额读取冒充委托',
 ['丢响应原单回读不再扣','未知授权明确拒绝','原Key/Secret版本恢复无泄漏'],
 'Gateway权威合同和原单/Key读回；真实费用仅另行授权')
delivery_slice('W21.tenant-admission','business',['W03.identity','W04.payment-key'],
 ['services/gateway-integration/identity'],
 ['CreateTenant','Tenant repository binding','钱包主体准入'],
 '真实开通Tenant/owner membership/已支持钱包主体，Package构建使用稳定repository；不等删除恢复全功能',
 ['通过产品API开通而非SQL预置','重复创建不增身份','冲突/越权/委托不支持拒绝'],
 'Tenant/owner/binding原身份与授权证据')
delivery_slice('W07.publisher-admission','artifacts',['W03.identity'],
 ['services/capability/catalog'],
 ['PublisherContract','发布者namespace准入','Capability reference claims'],
 '批准发布者身份/registry来源及Runtime引用保护基础；不要求独立WebUI制品或Package上传',
 ['发布者归属/registry prefix严格检查','跨租户/未批准用途拒绝'],
 '发布者准入与claim协议证据；不是独立WebUI构建证明')
delivery_slice('W10.app-release','artifacts',['W07.publisher-admission'],
 ['services/runtime-control/catalog','services/runtime-control/migrations'],
 ['指定OPL App TCR源','RuntimePublisherContract','03默认App选择'],
 '默认App及Agent构建所需批准Release/默认策略、native UI证据、descriptor和引用保护',
 ['真实App digest/平台/入口/native UI读取','撤销新准入不破坏现有引用','选择后策略变化不改快照'],
 '同一Runtime release与native UI实际能力及合同摘要')
delivery_slice('W07.webui-catalog','artifacts',['W07.publisher-admission','W10.app-release'],
 ['services/capability/catalog'],
 ['指定WebUI TCR源','获准Runtime兼容合同'],
 '独立WebUI精确制品/平台/contract准入并可选；默认App不依赖此结果',
 ['实际WebUI manifest/config/入口兼容读回','不兼容与未批准拒绝'],
 '获准独立WebUI的exact artifact/contract readback')
delivery_slice('W08.cos-upload','artifacts',['W07.publisher-admission'],
 ['services/capability/catalog','services/capability/cmd/server'],
 ['Storage provider','上传Operation/part identity','OMA Package格式'],
 'COS直传、分片owner对账、精确对象版本/校验、提交窗口恢复',
 ['复制成功DB提交前崩溃','刷新只续传缺失分片','越权/错摘要/过期许可拒绝'],
 '真实受限COS样本readback；对象只上传完成不代表Build可部署')
delivery_slice('W09.agent-build','artifacts',['W08.cos-upload','W10.app-release','W07.webui-catalog','W05.receipts'],
 ['services/build/internal/build','services/capability/catalog'],
 ['两种产品组合','CreateBuildRequest三项必需输入','Tenant binding','BuildKit/Registry'],
 'Package+Runtime+独立WebUI构建并向Tenant TCR推送，精确输出回读，Capability唯一注册',
 ['实际指定Runtime/WebUI输入而非loopback替身','push丢ACK重启不二次export','注册/receipt丢ACK唯一版本'],
 '输入快照/recipe/输出digest/CapabilityVersion/Ledger同一链；默认App不要求该切片')
delivery_slice('W05.receipts','integration',['W02.owner-readiness'],
 ['services/ledger','packages/contracts/go'],
 ['06异步交接矩阵','现行Ledger验证器/事件合同'],
 '所需注册/交易/资源/部署证据的生产者和Ledger消费者同版，原键原hash去重及exact readback',
 ['重复ACK丢失','同键不同hash拒绝','秘密/伪producer拒绝'],
 '不可变receipt及独立消费者重放证据；不把receipt当业务状态writer')
delivery_slice('W12.tke-resources','resources',['W02.owner-readiness'],
 ['services/fabric/cmd/fabric','services/fabric/coordination','services/fabric/internal/fabric','services/fabric/cmd/opl-tencent-provisioner'],
 ['FabricCoordination','PREPAID计划/profile','原provider请求'],
 '把现有Tencent资源能力接入新coordination，CVM/CBS/挂载/网络确认可交Serve；不部署应用',
 ['非Local dispatcher实际执行路径','原请求丢ACK读回/unknown不重购','规格/节点/PV-PVC/挂载一致'],
 '资源owner原operation/provider IDs及实际绑定；真实采购须授权')
delivery_slice('W06.tke-quote','business',['W10.app-release','W12.tke-resources'],
 ['services/resource-catalog'],
 ['批准TKE资源能力','D17/price/refund政策','应用选择快照'],
 '默认App与Agent的明确准入、价格和不可变Quote；采购策略不进入前端算法',
 ['过期/撤销/不兼容拒绝','更换应用选择需新Quote','一次AcceptQuote绑定原单'],
 '准确报价、批准policy/profile和应用digest/descriptor绑定')
delivery_slice('W17.first-delivery','delivery',['W10.app-release','W12.tke-resources','W05.receipts'],
 ['services/serve/internal/delivery','services/serve/internal/runtime_adapter','services/serve/internal/access','services/serve/migrations'],
 ['Serve Reserve/Deploy/ReadRuntime','Fabric resource refs','Runtime/Build descriptor','06注入/路由顺序'],
 'Serve独立TKE应用adapter，原部署身份/Secret/config/data注入，健康及route fence/activate/readback；无CP当前绑定依赖',
 ['默认App与built descriptor正例','Pod ready无URL不得完成','旧epoch/跨租户/并发挂载拒绝','根路径/静态/API/SSE'],
 '同Deployment runtime image identity、应用探针、实际URL及路由epoch；旧Fabric writer切caller后再退')
delivery_slice('W15.first-create','business',['W21.tenant-admission','W04.payment-key','W06.tke-quote','W17.first-delivery','W05.receipts'],
 ['services/workspace/internal/launch','services/workspace/cmd/server'],
 ['已接受Quote','原单/授权','Fabric confirmed resources','Serve reserved deployment'],
 '一个持久Saga贯通默认App和Agent；先保留Serve身份再绑定Key，资金/资源/应用/receipt分阶段收敛',
 ['相同提交不二次订单','支付/采购/部署/receipt分别丢响应恢复','默认App不需CapabilityVersion/Build'],
 '原Quote/订单/资源/Deployment/必需receipt全确认，Workspace权益与Serve可用状态一致')
delivery_slice('W13.console-entry','integration',['W03.identity'],
 ['apps/console-bff','apps/console-ui/src/api/auth-api.ts','apps/console-ui/src/api/dtos.ts','apps/console-ui/src/app/console-router.ts','Dockerfile','.github/workflows/build-opl-cloud-candidate.yml'],
 ['真实CloudIdentity会话','同origin确定性路由','候选前端构建模式'],
 'Cloud Console制品、API/CSRF/owner Operation读回实际连通；旧客户入口切换不跳M0-M5',
 ['构建产物有cloud入口','不因API失败回退旧接口','刷新/退出/跨tenant隔离'],
 '确切前端制品与BFF/owner真实调用证据')
delivery_slice('W14.publisher-ui','artifacts',['W09.agent-build','W13.console-entry'],
 ['apps/console-ui/src/pages/PublisherPage.tsx','apps/console-ui/src/api/publisher-api.ts'],
 ['W09真实构建','04上传/选择/恢复'],
 'Package+Runtime+独立WebUI由真实Console构建，缺项拒绝，关闭/刷新继续原任务',
 ['选项确实进入产物','浏览器丢响应/跨tenant','非默认新增UI入口不强制用户上传'],
 '真实浏览器与Build/Capability版本同digest')
delivery_slice('W16.workspace-ui','integration',['W15.first-create','W13.console-entry'],
 ['apps/console-ui/src/app/use-workspace-launch-controller.ts','apps/console-ui/src/app/workspace-launch-controller-model.ts','apps/console-ui/src/pages/workspaces','apps/console-ui/src/api/workspaces-api.ts','apps/console-ui/src/api/delivery-api.ts'],
 ['12默认App/Agent产品形态','真实Quote/Create/Serve Access'],
 '默认App无需进入Publisher；选套餐一次确认，显示订单/资源/应用/证据各自状态并打开真实URL',
 ['无Package默认创建','同一请求刷新恢复','不以202/Pod Running/无receipt显示完成'],
 '前端动作到owner/source事实一致的浏览器证据')
# Bounded Console presentation fixes get their own accurate lane: the console
# source owner, no business producer dependency, and the existing console test
# targets. They no longer borrow the whole W16 package or the launch-only slice.
delivery_slice('W16.console-ui-fixes','integration',[],
 ['apps/console-ui/src/app','apps/console-ui/src/pages','apps/console-ui/src/styles.css'],
 ['既有Console页面/controller规格','既有Console unit/browser测试目标','真实owner readback（Workspace/Delivery读取）','docs/implementation-architecture.md当前Console/BFF路由'],
 '既有Console表现层回归的最小修复：从owner读回派生展示结论，复用现有unit/browser目标；不新增业务语义、不写后端或业务SSOT',
 ['npm run typecheck','node --test tests/ui/workspace-experience-model.test.ts','npm run test:browser:workspace-lifecycle'],
 '真实执行的既有Console unit/browser目标(0 fail/skip)与source-check receipt；不声称业务链或W16包完成',
 ['tests/ui/workspace-experience-model.test.ts','tests/ui/workspace-task-experience-browser.test.ts','tests/ui/cloud-webui-browser.test.ts'])
# Source checks consume the existing typed owner inputs on one exact baseline;
# they do not require completion of the entire purchase/upgrade/renewal program.
# Whole W20 keeps its original dependencies and business acceptance obligations.
delivery_slice('W20.workspace-deletion-source','integration',['W20.deletion-grant-source'],
 ['services/workspace/internal/launch/deletion.go','services/workspace/internal/launch/deletion_test.go','services/workspace/internal/launch/recovery.go','services/workspace/internal/launch/service.go'],
 ['现有Workspace删除源码及RED/GREEN证据','W20.deletion-grant-source实际Host receipt','同exact baseline的typed owner contracts、原单/授权/资源/钱包读回接口','隔离PostgreSQL及显式runner依赖输入','docs/invariants.md#workspace-lifecycle'],
 'Workspace owner删除/恢复与状态读回的局部源码收口：absence及删除receipt前不退款；退款非终态读回原命令而不重发；资源与退款状态分开；不接管Legacy原单',
 ['OPL_POSTGRES_TESTS=1 go -C services/workspace test -json -count=1 ./internal/launch','原单/授权/absence/金额错配拒绝、非终态恢复不重复退款','实际测试数>0且0 fail/skip/TODO；runner只使用声明的隔离输入'],
 'Host从实际owner测试生成的source-check/stage receipt，绑定exact source/输入/输出/命令统计；下游串行联合验证按hash消费；不声称整个W20、真实删除、退款到账或Instance PASS')
delivery_slice('W20.deletion-grant-source','integration',[],
 ['services/gateway-integration/identity/authorization.go','services/gateway-integration/identity/workspace_grants_postgres_test.go'],
 ['现有Gateway accepted-operation grant源码与typed接口','同exact baseline的Workspace commit/action/audience合同','隔离PostgreSQL及显式runner依赖输入','docs/invariants.md#workspace-lifecycle'],
 'Gateway删除授权边界局部源码收口：按原accepted delete obligation发放/恢复续执行权限，与create obligation分组；不扩权、不建立钱包或转移Workspace结算权',
 ['OPL_POSTGRES_TESTS=1 go -C services/gateway-integration test -json -count=1 ./identity','错误accepted action/audience/资源拒绝；删除续执行不获得开通专用权限','实际测试数>0且0 fail/skip/TODO；runner只使用声明的隔离输入'],
 'Host从实际授权测试生成的source-check/stage receipt，绑定exact source/输入/输出/命令统计；Workspace source run在requires中消费本phase的实际receipt hash，再交串行联合验证；不声称真实钱包退款、整个W20或Instance PASS')
delivery_slice('W27.runnable-candidate','integration',['W02.owner-readiness'],
 ['Dockerfile','deploy/portable','.github/workflows/build-opl-cloud-candidate.yml'],
 ['同Cloud commit合同/owner集合','buildx/buildkit接线','安装配置schema'],
 '精确SHA/digest的可测试Candidate，声明必需owner/ports/DB/identity/工具；非正式发布',
 ['镜像内可执行工具与cloud UI参数','安装配置缺失明确失败','无latest漂移'],
 'Candidate manifest/source/schema/platform/image/asset checksum；任何组件变化新候选')
# The development-governance lane is host-owned: it names where the PR gate,
# the development plan mechanics and the development documents live so a
# governance pull request has an accurate phase/owner reference. Restricted
# workers never obtain these writes; dev-session protects them separately.
delivery_slice('W27.development-governance','integration',[],
 ['.github/','tools/','tests/tools/','AGENTS.md','DEV_GUIDE.md','package.json','docs/spec/target/','docs/evidence/source-checks/'],
 ['AGENTS.md#scoped-development-contract','DEV_GUIDE.md#scoped-host-and-worker-entry','现有PR合规清单(.github/PULL_REQUEST_TEMPLATE.md)'],
 '开发治理统一入口：PR body机器校验(base SHA/DDD owner/phase/write set/source-check执行摘要)、开发SSOT phase入口与hard-entry审计；host-owned，restricted worker不获得这些写权限',
 ['npm run test:development-gates','npm run verify:development-plan','npm run verify:local:focused -- --base origin/main'],
 '实际执行的gate结果与append-only source-check receipt；不替代CI实际validate，不声称产品runtime或发布完成')
delivery_slice('W26.default-source','integration',['W16.workspace-ui','W05.receipts'],
 ['tests/integration','tests/ui','tools'],
 ['真实默认App入口','所有owner链','无Package/Build路径'],
 '本地真实服务/隔离外部权威证明默认App原单到应用；无真实provider冒充',
 ['一键到实际App','原单/effect/receipt ACK丢失','重启/数据/越权'],
 '明确source/local层证据和未执行Tencent事项')
delivery_slice('W29.default-tke','instance',['W26.default-source','W27.runnable-candidate'],
 ['../opl-instance-medopl/deploy','../opl-instance-medopl/.github/workflows','../opl-instance-medopl/receipts'],
 ['同Candidate','批准provider/COS/TCR/证书/Secrets引用','Cloud-owned验收器','限定采购/费用授权'],
 'Instance仅部署Cloud并运行其验收器，Console默认App一次确认→TKE资源→Serve→真实native UI',
 ['同App digest/平台/资源/route/receipt','全新资源采购须独立授权；复用资源只能算应用交付','不手动kubectl代替Cloud业务'],
 '安装receipt+Cloud业务receipt+真实URL/readback同链；只证明本slice，非完整W29/Release')
delivery_slice('W26.agent-source','integration',['W14.publisher-ui','W16.workspace-ui','W05.receipts'],
 ['tests/integration','tests/ui','tools'],
 ['真实Package/Runtime/UI Build','同一个Workspace/Serve链'],
 'Package+独立WebUI的Agent组合在相同业务链闭合且原操作恢复',
 ['实际构建字节/输出digest/Serve运行一致','丢响应/重启/跨租户','默认路径回归'],
 '源/本地隔离层完整组合证据，不与其他实验拼接')
delivery_slice('W29.agent-tke','instance',['W29.default-tke','W26.agent-source','W27.runnable-candidate'],
 ['../opl-instance-medopl/receipts','../opl-instance-medopl/.github/workflows'],
 ['包含Agent实现的同Candidate','指定真实输入/输出TCR和COS','Cloud-owned验收器'],
 '在已部署Cloud内从Console完成Package→真实TCR OCI→TKE Serve→实际Agent使用，独立WebUI组合',
 ['真实模型/应用按发布者合同使用','同输入输出/订单/资源/Deployment/route','重启/响应丢失原身份收敛'],
 '两种产品组合的TKE首链证据；不提前声明后续生命周期/旧客户迁移通过')
delivery_slice('W17.replace','delivery',['W15.first-create','W09.agent-build'],
 ['services/serve/internal/delivery','services/serve/internal/runtime_adapter','services/serve/internal/access'],
 ['当前Serve部署','数据兼容/原资源/显式新选择'],
 '默认App/Agent更新和跨模式替换、route fencing与兼容回滚，无重复购买或双current',
 ['旧epoch写拒绝','数据不可逆拒绝','失败恢复原路由与原数据证明'],
 '新旧Deployment/claims/data/route真实readback，不能仅改DB指针')
windows=[
 dict(id='integration',title='统筹、共享合同、Console集成与验收',scope=['docs','packages/contracts','services/internal','services/ledger','apps/console-bff','apps/console-ui共享入口/Workspace页（排除PublisherPage/publisher-api）','Dockerfile','deploy/portable','.github/workflows','tests/integration','tests/ui'],handoff='先串行W01/W02共享接口，随后吸收独立owner与同Candidate验收；不替域owner写业务状态'),
 dict(id='artifacts',title='输入目录、COS与OCI构建',scope=['services/capability','services/runtime-control','services/build','apps/console-ui/src/pages/PublisherPage.tsx','apps/console-ui/src/api/publisher-api.ts'],handoff='提供批准App/native UI descriptor与三输入Agent Build的真实制品/claims/readback'),
 dict(id='business',title='身份、交易、报价与Workspace Saga',scope=['services/gateway-integration','services/resource-catalog','services/workspace'],handoff='提供真实Tenant/原单/Key/Quote和恢复中的原Workspace操作；无current deployment副本'),
 dict(id='resources',title='Tencent资源执行',scope=['services/fabric'],handoff='原operation的CVM/CBS/网络/attachment readback，不执行应用'),
 dict(id='delivery',title='Serve TKE部署与访问',scope=['services/serve'],handoff='精确OCI→运行健康→唯一current→URL；迁移旧应用执行须统筹串行切caller'),
 dict(id='instance',title='按需启用的Instance部署窗口',scope=['../opl-instance-medopl'],handoff='只安装Cloud/配置/Secrets与运行已授权Cloud验收器，不是常驻第六个业务实现writer')]

# Dispatchable work before W01: existing contracts only, no speculative DTOs.
# These are bounded preparations inside the original W packages, not acceptance
# of a downstream slice whose startAfter producer has not finished.
parallel_preparation = [
 dict(window='artifacts', title='TKE A — 制品准入、COS与Build',
      writePaths=['services/capability','services/runtime-control','services/build','apps/console-ui/src/pages/PublisherPage.tsx','apps/console-ui/src/api/publisher-api.ts'],
      entryWork='沿现有三输入Build合同，核对并有选择吸收#689的COS实现及#684的Registry修复；完成COS成品已复制但DB未提交的崩溃恢复，以及GetUpload对真实Storage分片的对账续传。默认App只做Runtime原始发布合同/descriptor字节准入，不发明新wire。',
      currentBreakpoints=['当前main未含COS PR #689；不能假设已合并','COS Assemble在复制成品/清理staging后与DB提交之间存在恢复窗口','实际输入TCR与loopback fixture不同；列tag不等于准入'],
      nextSlices=['W07.publisher-admission','W10.app-release','W07.webui-catalog','W08.cos-upload','W09.agent-build','W14.publisher-ui'],
      commands=['go -C services/capability test ./...','go -C services/runtime-control test ./...','go -C services/build test ./...'],
      acceptance='原上传/Package/Build/版本身份恢复；错digest/跨租户/未准入拒绝。独立UI必须存在；默认App不得依赖Agent构建。真实TCR/COS未运行时明确not_run。'),
 dict(window='business', title='TKE B — 身份、交易、报价与Workspace',
      writePaths=['services/gateway-integration','services/resource-catalog','services/workspace'],
      entryWork='核对并有选择吸收#688的身份owner readiness修复；补自身OwnerOperations授权缺口；在现行typed GatewayCoordination与Fabric/Serve合同内接通Workspace启动依赖，删除强制local_no_charge作为Tencent路径的限制，不以真实扣款测试代替隔离权威测试。',
      currentBreakpoints=['Workspace cmd尚未接GatewayCoordination','launch/runtime.go强制Local zero-charge','报价缺capabilityVersionId被判not_applicable；默认App修正必须消费W01正式union，不先补nullable兜底'],
      nextSlices=['W03.identity','W04.payment-key','W21.tenant-admission','W06.tke-quote','W15.first-create'],
      commands=['go -C services/gateway-integration test ./...','go -C services/resource-catalog test ./...','go -C services/workspace test ./...'],
      acceptance='一次确认、原单/Key/Operation稳定；扣款丢ACK按原authority读回、不重复扣款；未知效果不自动退款；租户/会话撤权反例。资金测试必须明确隔离。'),
 dict(window='resources', title='TKE C — Fabric腾讯资源',
      writePaths=['services/fabric'],
      entryWork='用现有FabricCoordination接入既有Tencent资源provider，而非仅Local dispatcher；持久原provider effect身份并实现CVM/CBS/attachment/network资源readback。先做隔离provider协议测试，不实际采购。',
      currentBreakpoints=['cmd/fabric/main.go新coordination只装LocalResourceDispatcher','coordination/local_dispatch.go非Local provider无执行路径','旧Fabric TKE应用writer只能在Serve接管真实caller及义务验收后退休，不提前删'],
      nextSlices=['W12.tke-resources'],
      commands=['go -C services/fabric test ./coordination/...','go -C services/fabric test ./internal/fabric/...','go -C services/fabric test ./cmd/...'],
      acceptance='PREPAID月付合同、资源原ID和读回；purchase响应丢失/restart不再采购，unknown不当absent；Fabric不成为新应用部署owner。缺Serve所需字段只提交合同差异。'),
 dict(window='delivery', title='TKE D — Serve部署、健康与URL',
      writePaths=['services/serve'],
      entryWork='在现有ServeRuntimeAdapter/ServeAccessControl合同内实现Serve-owned TKE adapter及访问执行；借鉴旧Fabric实现但不改其目录或切生产caller。验证serviceName/port到明确内部upstream与应用origin路由，不猜URL、不以Pod Running代替ready。',
      currentBreakpoints=['当前Serve只接FabricApplicationAdapter','TKE observation给serviceName/port，旧adapter却只取Entry.URL导致ready链断','adapter拒绝Secret/model配置；真实应用注入须按现有publisher合同实现'],
      nextSlices=['W17.first-delivery','W17.replace'],
      commands=['go -C services/serve test ./...'],
      acceptance='Reserve/Deploy/Observe/Activate、持久effect与epoch、注入/readiness/route同一部署；验证HTML/资源/API/SSE及租户隔离；默认App接线等W01，旧writer切换由统筹串行执行。'),
]
for entry in parallel_preparation:
 entry.update(model='deepseek-v4.1-flash', reasoningEffort='high',
     sharedContractGate='W01.application-contracts',
     evidencePath='docs/evidence/source-checks/2026-09-29-tke-'+entry['window']+'-development.json',
     sourceBaseline='current shared checkout; exact HEAD and per-owner diff hashes recorded on start and completion',
     forbiddenWrites=['packages/contracts','docs/spec/target','docs/status.md','docs/roadmap.md','services/internal','apps/console-bff','Dockerfile','deploy','.github','AGENTS.md'],
     stopBoundary='不得猜新DTO或越界补共享合同；把确切RPC/字段/consumer/拒绝用例交统筹，继续其余独立工作。只有W01同版生成物与消费者测试交接后才接默认App新分支。')

# Sequential integration after the four current-contract preparations. These
# are acceptance stages for existing W packages, not another domain or protocol.
serial_integration = dict(
 title='TKE串行集成与上线', model='deepseek-v4.1-flash', reasoningEffort='high',
 ownership='唯一串行实施窗口接管当前未提交Cloud工作区；四领域窗口停止写入。本统筹只保留本次审计/派发证据，不再并行改同一写集。串行指执行顺序，不合并DDD业务owner。',
 scope='先完成Cloud产品代码、真实消费者、隔离整链及Candidate；随后在Instance仓库的受保护main/production流程内安装同一Candidate并实测。',
 authorization='用户2026-09-29明确要求新建串行窗口并deploy上线；必要的代码集成/分支/提交/推送/PR/受保护流程推进属于该交付。遵守分支保护、发布者身份和Instance权限，不请求重复的泛化继续确认。真钱扣款/新购/续费/销毁仍须既定独立资源及金额范围授权，不由上线意图推定。',
 auditReceipt='docs/evidence/source-checks/2026-09-29-tke-serial-integration-audit.json',
 completion='默认App和三输入Agent均在同一已安装Candidate上形成原单→资源→Serve→URL→真实使用→必需receipt闭环；安装成功、普通go test通过或文档READY都不能替代。',
 stages=[
  dict(order=1, title='接管与可复现源码基线', workPackages=['W00','W01','W02'],
   action='记录当前HEAD、tracked patch与所有untracked源码hash；保留A/B/C/D和SSOT未提交变更，不从旧HEAD开空worktree丢失成果。审计现有PR的已移植/剩余范围后在codex/分支整理，不盲目合并#684/#688/#689/#686。补Serve/Workspace的COS依赖与livebuild模块sum；恢复checksum验证，不以关闭GOSUMDB永久绕过。',
   accept='受影响普通及livebuild编译通过，生成器可重复；旧四回执保持不可变，后续动作新回执。'),
  dict(order=2, title='W01两组合真实合同与消费者迁移', workPackages=['W01','W02','W03','W13'],
   action='按03/02/06一次贯通selection union、runtime_release provenance、原始descriptor bytes/digest、Quote及接受快照、Serve预留/部署/读取、持久化迁移和严格decoder；默认App必须runtime readback required。Agent仍必需Package/Runtime/独立WebUI及三claim。统一生产/spec proto、api/v226映射、TenantRepositoryBinding和授权生成器。修CloudIdentity自身readiness、Operation对象授权及全部实际continuation动作。',
   accept='两种真实owner正例及混合/缺项/撤销/跨Tenant反例；真实migration loader执行和历史义务保真，所有消费者同版，才移除pending迁移标记。'),
  dict(order=3, title='Gateway交易、Key与Ledger资金证明', workPackages=['W04','W05','W15','W21'],
   action='在既有Gateway Integration单元实现并注册GatewayCoordination，而不是仅Workspace客户端。以Sub2API为唯一钱包，持久原扣退款/Key意图及owner readback。用已有WALLET_ACTION事实核对是否足够；补真实producer、Ledger decoder/持久化/read-by-reference和Fabric消费，不机械按B旧回执再造enum，也不按C假设伪装证据owner。准入真实Tenant/成员和钱包授权。',
   accept='隔离真实服务的Gateway→Ledger→Workspace→Fabric链；收据绑定租户/Workspace/quote/obligation、金额币种、原effect及outcome；错owner/金额/原单拒绝，丢ACK/重启不重扣、unknown不新购或自动退款。'),
  dict(order=4, title='腾讯资源与Secret注入交接', workPackages=['W06','W12','W15','W17'],
   action='复用C的Tencent dispatcher，验证真实owner资金证明、精确Catalog计划、PREPAID预检、CVM/CBS/挂载/网络读回及原provider幂等身份。打通Gateway managed key→批准Secret store→Fabric binding→Serve执行引用；仅typed opaque handle及版本跨域，raw Key不入普通DB/日志/Outbox/receipt。',
   accept='资源和注入均按原对象可回读；失败/未知不多买，不跳过Secret启动空模型应用；隔离provider测试不能代替后续TKE实测。'),
  dict(order=5, title='Serve真正接管执行、路由与运行事实', workPackages=['W17','W05','W15','W24'],
   action='D的TKEApplicationAdapter仍POST旧Fabric应用端点，只是迁移中的调用适配，不是执行writer已迁走。将TKE应用执行能力连同真实caller迁到Serve，串行切换后按09退出对应旧writer。Serve以本域access_bindings的generation/accepted epoch执行Fence/Activate/Observe与兼容Rollback，Deploy确认readiness和当前Deployment/绑定同事务提交；接入按绑定读回的access data plane，迁移实际入口和调用者并清退Control Plane应用proxy/当前路由解析器，不保留RouteProvider或路由fallback。Instance只提供稳定Ingress/DNS/TLS到Serve。补Secret配置、模型实际应用及读回、Stop/Reload/凭据获取和失败退役。不得把请求的modelConfigurationVersion直接写成applied。',
   accept='进程实际注册/启动的路径贯穿部署、readiness、route与Ledger；已发布URL不是拼字符串，真实HTML/静态/API/SSE、跨Tenant拒绝、旧epoch、unknown switch、Secret隔离及重启恢复均通过。'),
  dict(order=6, title='两组合Console到服务整链与回归', workPackages=['W07','W08','W09','W10','W13','W14','W16','W26'],
   action='先验证默认App不依赖Build，再验证OMA Package→COS→精确Runtime+独立WebUI→BuildKit→Tenant OCI digest回读→Capability→同一购买部署链。接真实BFF/Console而非fixture DTO，统一进度/Operation/receipt与访问入口。真实TCR输入的tag列表不能代替获准descriptor/字节身份。',
   accept='npm run verify:local:full及相关browser/livebuild检查，真实PG独立owner/mTLS/幂等恢复；刷新、响应丢失、余额拒绝、未准入、半选组合、错receipt/版本、路由失败均有证据。原型与规格receipt按实际测试更新，不只改hash。'),
  dict(order=7, title='同SHA Candidate与Instance安装准备', workPackages=['W02','W27','W28','W29'],
   action='补镜像内buildx、明确Console cloud模式、所需owners/DB/roles/mTLS、BuildKit隔离、COS/TCR Secret引用和Serve路由配置。经CI及受保护合入得到canonical Cloud SHA，构建不可变Candidate；按既有规则完成合格Linux验证。Instance #348仍是未合并提案，必须以当时remote main为准审核补齐安装清单，不复制每用户业务编排。',
   accept='Candidate manifest、schema与安装资产hash、index/platform digest和来源都一致；Instance受保护main流程可验证接收；本机不触达生产私网。'),
  dict(order=8, title='受保护部署、业务实测与上线收口', workPackages=['W29','W30','W31'],
   action='同一串行窗口在Instance owner范围按当前工作流执行preflight、部署、回读、必要rollback和不可变receipt；不是由Cloud workflow dispatch Instance。既有客户切流必须按09满足M0–M5，不用空新库替掉旧登录/订单。对获授权的测试对象验证两组合真实使用；新采购/真实收费/销毁如缺具体范围，只询问最小金额/对象输入，其余工作继续。正式Product Release只有符合既有发布授权及同Candidate资格时提升原字节，部署不强迫先发Release。',
   accept='同Candidate、应用输入输出digest、订单/资源/Deployment/route epoch、实际使用和必要receipt一致；失败按已证明数据兼容的回滚路径处理，不能盲回旧image。记录可访问入口、Cloud SHA、Candidate/Instance run及已验证/未验证范围，不以Pod Running、200或安装成功宣称闭环。'),
 ])

baseline={
 'AUTH':"go -C services/control-plane test ./internal/server -run '^(TestDelegatedCredentialNeverPersistsOrLeaks|TestAccountDisableRevokesSessionCredential|TestGatewayKeyOwnership|TestCloudAdminCanRevealOnlyOwnGatewayKey)$' -count=1",
 'WALLET':"go -C services/control-plane test ./internal/clients -run '^(TestAuthenticateUserReturnsDelegatedCredential|TestSub2APIFinancialHistoryUsesNativeExactNotesAndAllPages|TestSub2APIAdjustmentConnectionLossDoesNotRepeatDebit|TestSub2APIRefundRequiresReadOnlyRebatePolicy)$' -count=1",
 'LAUNCH':"go -C services/control-plane test ./internal/server -run '^(TestPricingCatalogLookupRequiresExactAcceptedVersion|TestWorkspaceLaunchMonthlyPreflightRunsBeforeDebitAndProviderStages|TestWorkspaceLaunchDebitAuthoritativeReadbackClassification|TestWorkspaceLaunchResourceOnlyReconcilerCompletesWithoutApplicationFacts)$' -count=1",
 'SETTLEMENT':"go -C services/control-plane test ./internal/server -run '^(TestWorkspaceRenewalConcurrentWorkersClaimOnce|TestWorkspaceRenewalUsesOneDebitStableProviderIDsAndOneReceipt|TestWorkspaceDeleteRefundRecoversLostResponseWithoutSecondDispatch|TestWorkspaceDeleteRefundStatusIsReportedSeparatelyFromDeletion)$' -count=1",
 'RUNTIME':"go -C services/control-plane test ./internal/server -run '^(TestApplicationRevisionAdmissionHTTP|TestWorkspaceApplicationDeploymentHTTPReplayRetainsAcceptedCredentials|TestWorkspaceApplicationBindingHTTPReplacementAndPreflight)$' -count=1",
 'ABI':"go -C packages/contracts/go test ./... -count=1",
 'CANDIDATE':"node --test tests/tools/cloud-candidate-receipt.test.ts tests/contracts/clean-host-qualification.test.ts"}
for n,keys in {1:['ABI'],3:['AUTH'],4:['WALLET'],7:['ABI'],10:['ABI','RUNTIME'],15:['LAUNCH'],17:['RUNTIME'],18:['SETTLEMENT'],19:['SETTLEMENT'],20:['SETTLEMENT'],21:['AUTH'],25:['AUTH','SETTLEMENT','RUNTIME'],27:['CANDIDATE']}.items():
 W[n]['verification'] += ['实施前现有基线（不证明新功能）：rtk proxy '+baseline[k] for k in keys]
# Every REST operation has exactly one owning backend implementation work package.
workspace_map={'createWorkspace':15,'listWorkspaces':15,'getWorkspace':15,'getWorkspaceAccess':17,'getWorkspaceModels':17,'updateWorkspaceModels':17,'listDeployments':17,'getDeployment':17,'updateWorkspaceVersion':17,'rollbackWorkspace':17,'revealWorkspaceApplicationCredentials':17,'resizeWorkspace':19,'listPlanChanges':19,'getPlanChange':19,'cancelPlanChange':19,'renewWorkspace':18,'getSubscription':18,'updateRenewalSettings':18,'deleteWorkspace':20,'getWorkspaceDeletion':20,'getOperation':13,'listAdminOperations':24,'reconcileOperation':24,'adoptWorkspace':25}
tenant_lifecycle={'listTenants','createTenant','getAdminTenant','deleteTenant','bindTenantWallet','suspendTenant','restoreTenant','getTenantAssetCustody','reenableTenant','getTenantLifecycleOperation'}
cap_catalog={'publishOfficialPackage','listRuntimeVersions','listWebuiVersions','registerRuntimeVersion','setRuntimeVersionStatus','registerWebuiVersion','setWebuiVersionStatus','getBuildRuntimePolicy','setBuildRuntimePolicy','listPublisherNamespaces','createPublisherNamespace','revokePublisherNamespace'}
cap_artifacts={'listCapabilityVersions','getCapabilityVersion','deleteCapabilityVersion'}
for op in api:
 owner=op['owner'];name=op['operationId']
 if owner=='serve':n=17
 elif owner in {'workspace','bff'}:n=workspace_map[name]
 elif owner=='runtime_control':n=10
 elif owner=='tenant':n=24 if name=='listAuditEvents' else (21 if name in tenant_lifecycle else 3)
 elif owner=='capability':n=7 if name in cap_catalog else (9 if name in cap_artifacts else 8)
 elif owner=='resource_catalog':n=15 if name in {'createQuote','getQuote'} else 6
 elif owner=='build':n=9
 elif owner=='gateway':n=4
 elif owner=='ledger':n=5
 else:raise ValueError((owner,name))
 W[n]['apiPrimary'].append(name)
# Frontend consumers derive from its actual feature inventory, not invented endpoints.
for n in [13,14,16,22,23]:
 features=set(W[n]['features']);W[n]['apiConsumes']=sorted({op for f in ui if f['featureId'] in features for op in f['operationIds']})
# Each persisted table has a primary schema/implementation task; domain writer never changes here.
table_primary={}
for t in db:
 owner=t['owner'];name=t['name'];key=t['schema']+'.'+name
 if any(x in name for x in ['outbox','inbox','idempotency']) or name=='operations':n=2
 elif owner=='tenant':n=3
 elif owner=='gateway':n=4
 elif owner=='ledger':n=5
 elif owner=='resource_catalog':n=15 if name in ['quotes','quote_items'] else 6
 elif owner=='capability':n=7 if name in ['runtime_versions','webui_versions','catalog_policies','publisher_namespaces'] else (9 if name=='capability_versions' else 8)
 elif owner=='build':n=9
 elif owner=='runtime_control':n=10
 elif owner=='serve':n=17
 elif owner=='fabric':n=12
 elif owner=='workspace':n=19 if ('plan_change' in name or 'obligation' in name) else (18 if name in ['subscriptions','subscription_periods'] else (17 if name in ['deployments','model_configurations'] else 15))
 else:raise ValueError((owner,name))
 table_primary[key]=W[n]['id'];W[n].setdefault('tablesPrimary',[]).append(key)
# Internal RPC implementation assignments are interface groups, never new service deployments.
service_tasks={'ClaimUsageReadback':[8,9,15,17], 'CapabilityCoordination':[7,8,9], 'BuildCoordination':[9], 'OwnerOperations':[2,24], 'OwnerCommitReadback':[2], 'WorkspaceAuthorizationReadback':[18], 'CloudIdentityAuthorization':[3], 'CatalogCoordination':[15], 'WorkspaceAdmission':[15], 'GatewayCoordination':[4], 'FabricCoordination':[11,12], 'ServeAgentCoordination':[17], 'ServeRuntimeAdapter':[17], 'ServeAccessControl':[17], 'TenantWorkspaceCoordination':[21], 'LedgerCoordination':[5], 'DomainInbox':[2,5,9,15,19,21], 'WorkspacePlanChangeReadback':[19], 'FabricPlanTransitionReadback':[11,12], 'GatewayPlanChangeSettlement':[4,19], 'ServePlanChangeControl':[17,19], 'LedgerPlanChangeEvidence':[5,19]}
method_primary={op['operationId'][0].upper()+op['operationId'][1:]:next(w['id'] for w in W if op['operationId'] in w['apiPrimary']) for op in api}
rpc_assignments={}
for m in re.finditer(r'service\s+(\w+)\s*\{(.*?)\}',(P/'contracts/internal.proto').read_text(),re.S):
 service=m[1]
 for method in re.findall(r'rpc\s+(\w+)',m[2]):
  key=service+'.'+method
  if service.endswith('ProductService'):ids=[method_primary[method]]
  else:ids=[f'W{i:02d}' for i in service_tasks[service]]
  rpc_assignments[key]=ids
  for wid in ids:W[int(wid[1:])].setdefault('rpcImplements',[]).append(key)
# Ensure the shared operation route and source path placeholders are honest.
for w in W:
 w['apiPrimary'].sort()
 w['riskBoundary']='只改文档所定义的代码/测试；真钱、采购、删除、生产网络、正式发布另按Owner授权执行'
 w['contractRefs']=['00_master_index.md','01_domain_ownership_matrix.md','03_api_contract_complete.yaml','06_data_flow_and_state_machine.md','08_delivery_checklist_per_role.md']
 if 'F11' in w['features']:w['contractRefs'].append('13_plan_change_policy.md')
 if 'F16' in w['features']:w['contractRefs'].append('09_legacy_migration.md')
 w['existingPathStatus']={x:(C/Path(x)).exists() for x in w['existingReadPaths']}
# Freshness ignores provenance; default generation must obtain a real Git SHA or fail.
provenance={} if options.check else {'sourceSHA':subprocess.check_output(['git','-C',str(C),'rev-parse','HEAD'],text=True).strip()}
# Instance declares an external owner; never inspect its filesystem or claim local verification.
plan={'schemaVersion':1,'status':'plan_only_not_implemented',**provenance,'sourceRoots':roots,'sourceRootStatus':{k:('external-owner/unverified' if k=='instance' else ('existing' if (C/Path(v)).exists() else 'planned_not_created')) for k,v in roots.items()},'assignees':'roles/modules are fixed; human names and calendar dates follow actual staffing, not fabricated','workPackages':W,'tablePrimary':table_primary,'rpcImplementers':rpc_assignments,'baselineCommands':baseline,'executionSlices':slices,'windows':windows,'parallelPreparation':parallel_preparation,'serialIntegration':serial_integration,'contractMigration':{'source':'03_api_contract_complete.yaml#x-approved-wire-migration','status':'approved_pending_W01_consumer_migration','entrySlice':'W01.application-contracts'},'rules':{'startDependencies':'finished producer contracts/scaffolding needed to start useful implementation','acceptDependencies':'real producers needed for integrated acceptance; no fixture-only completion','formalReleaseVsInstance':'separate authorized Owners; same qualified bytes; Cloud never dispatches Instance deployment'}}
(OUT/'checks').mkdir(parents=True,exist_ok=True)
(OUT/'checks/development_plan.json').write_text(json.dumps(plan,ensure_ascii=False,indent=2)+'\n')
lines=['# 14 完整开发执行任务书','','> 目标：把已定稿F01–F17变成可直接分工实施的工作包。主Owner为Cloud架构/各领域负责人；产品/字段/协议Owner仍是00–13，本文件不重新定义它们。','> 本文件是执行规格，不是进度账本；not_implemented仅表示本生成器不认证实施完成。实际完成情况以docs/status.md与Owner证据为准，不代表已创建目录、跑通服务、迁移或发布。','> 每个任务列真实来源、拟创建写集、依赖、前后端产物及验收。原任务记录/失败验证保留历史，不用百分比冒充交付。','','## 1. 完整方案的层次与交付','','12产品主说明回答用户得到什么；11/04回答页面怎样操作；01/02/03/05/06/13回答权威、字段、接口、规则和恢复；09回答旧数据如何迁移；本14回答谁先做、改哪里、怎么测、交给谁；08仍是各角色统一验收Owner。','','- 范围严格F01–F17，不增加Marketplace、公开注册、任意Runtime上传、新钱包或工作流框架。','- 金额、原单、D17、provider预付规则及data-loss边界不允许研发自选第二种解释。','- 设计/页面可按冻结规格并行开发，最后必须接真实BFF/Owner，不以静态prototype或mock冒充完成。','- 不预设“50人/3分钟/两周完成”等无依据承诺；人名/日历排期按实际资源填，不再改业务语义。','','## 2. 代码与仓库落点','','| 逻辑Owner | 工作根 | 当前状态 |','|---|---|---|']
for k,v in roots.items():lines.append(f"| {k} | `{display_path(v)}` | {plan['sourceRootStatus'][k]} |")
lines+=['','所有Cloud工作根均位于同一个opl-cloud GitHub仓库；路径由当前checkout推导，不绑定开发者机器。instance是外部Owner，不属于Cloud合仓写集；W29/W30仅描述其授权工作，不能由Cloud任务越界执行。具体模块/进程/数据库实施映射见01，架构决定以docs/architecture.md及docs/decisions.md为准，tenant与gateway两行共享一个部署单元。planned_not_created指目录未建，不是等待创建GitHub仓库。','','## 3. 实施顺序、并行与完成依赖','','可立即启动第一批是W00→W01→W02；随后W03/W05/W06/W07/W10/W11/W12等按各自依赖并行。按第9节可实施切片推进，不等全部W综合完成才做第一个用户结果；下表整包依赖仅表示综合收口，不能代替切片开始条件。','','- 首条TKE产品闭环优先默认App：无需Package/Build，W10批准App→W06报价→W15/W12/W17资源与应用→W16真实访问；切片依赖见第9节。','- 随后叠加Agent：W07/08/09/14上传与真实构建→同一Workspace/Serve链；只覆盖Package+Runtime+独立WebUI。Local是独立回归/资格，不取代TKE首链。','- 第三条：W17–W23，配置/切换/升降配/续费/删除与管理治理。','- 迁移转换W25在契约/DB早期开始，生产切换在W30，不等最后才考虑旧ID和原单。','- 构建/Local/Instance资格/发布是W27–W31，代码测试、候选、实例采用和正式发布分层。','','| 工作包 | 协调Owner | 可以开始的依赖 | 综合验收依赖 | 业务范围 |','|---|---|---|---|---|']
for w in W:lines.append(f"| {w['id']} {w['title']} | {w['coordinator']} | {','.join(w['startAfter']) or '无'} | {','.join(w['acceptAfter']) or '本任务依赖与边界即可'} | {','.join(w['features'])} |")
lines+=['','### 同文件与发布边界的串行点','','- W00独占canonical目标同步；W01独占同一shared contract revision及必要consumer依赖整合，消费者同commit吸收。W02并行任务不各自改写共享契约或根构建配置。','- Workspace各工作包可以并行内部独立文件，但router/main/store/schema aggregate与同一migration序号由Workspace Owner串行合入，不双写/不另建全局协调器。','- Console各页面/独立controller可并行；console-router、use-console-controller和共享DTO整合由W13/Console Owner串行吸收。','- Fabric Local/Tencent在适配器内并行；provider_port/public DTO/通用handler变更必须同版更新两端与测试。','- 数据库正式迁移、canonical main、Candidate冻结、Instance运行状态和正式发布不并行写。','- W31不等于W30。产品可以发布已资格字节，客户迁移可分批；不能据产品发布宣称所有Instance已采用。','','## 4. 工作包详单']
# Narrative that supplements a work package's deliverables. Kept in the generator
# so a regeneration reproduces it byte-for-byte instead of living only in the output.
work_notes={
 'W17':[
  "`UpdateWorkspaceModels` has one mandatory owner-separated sequence: Workspace authorizes and validates the selection, Gateway creates the exact managed-key binding, Fabric confirms the runtime Secret binding, Serve applies and reads back the publisher configuration through `RuntimeReloadCommand.managed_key_binding`, and Workspace advances `model_configuration_version` only from that confirmed readback. The public request remains `expectedVersion + selections`; no caller may supply a key binding. This sequence is shared by the default OPL App and the Agent plus independent WebUI product combinations whenever their Runtime publisher declares the model-configuration contract.",
  "For a later configuration that changes the managed key, W01 must use the explicit `FabricCoordination.RebindSecret` path: compare the expected current Fabric binding, provider-confirm the replacement, preserve one active binding per runtime purpose, apply/read back the new Runtime version, then revoke the predecessor Gateway key and retire its Fabric binding. Rejected or unknown replacement/compensation never advances Workspace or revokes the predecessor. The replacement operation is owner-local and idempotent through `CallContext`; it does not introduce a global workflow engine or a public key-binding field.",
 ],
}
for w in W:
 lines+=['',f"### {w['id']} {w['title']}",'',f"**协调Owner：** {w['coordinator']}；**参与Owner：** {', '.join(w['owners'])}。",f"**F范围：** {', '.join(w['features'])}；**开始依赖：** {', '.join(w['startAfter']) or '无'}；**验收依赖：** {', '.join(w['acceptAfter']) or '按本任务边界'}。",'', '**现有来源（当前checkout定位，不表示全部要改；原始source snapshot见09）**：']
 for x in w['existingReadPaths']:lines.append(f'- `{display_path(x)}`'+('' if w['existingPathStatus'][x] else '（需按当前source inventory定位；不虚构已存在）'))
 lines+=['','**拟写入位置（未来实施，尚未创建的路径也明确列出）**：']
 for x in w['plannedWritePaths']:lines.append('- `'+display_path(x)+'`')
 lines+=['','**必须交付**：']+['- '+x for x in w['deliverables']]
 if w['apiPrimary']:lines+=['','**主实现API（沿用03的唯一Owner，不是改变数据写权）**：'+', '.join('`'+x+'`' for x in w['apiPrimary'])]
 if w.get('tablesPrimary'):lines+=['','**主实现表（字段唯一来源02，不在此复制字段定义）**：'+', '.join('`'+x+'`' for x in w['tablesPrimary'])]
 if w.get('rpcImplements'):lines+=['','**内部协议实现/协作端口**：'+', '.join('`'+x+'`' for x in w['rpcImplements'])]
 if work_notes.get(w['id']):lines+=['','**补充说明**：']+['- '+x for x in work_notes[w['id']]]
 if w['apiConsumes']:lines+=['','**前端消费API**：'+', '.join('`'+x+'`' for x in w['apiConsumes'])]
 lines+=['','**验证**：']+['- '+x.replace(str(C)+'/','') for x in w['verification']]
 lines+=['','**完成判定**：']+['- '+x for x in w['acceptance']]
lines+=['','## 5. 首批开发任务怎样落PR','','1. W00只统一目标/实施分层和source snapshot，不先改运行逻辑。','2. W01把必要的契约及真实consumer类型接入现仓，先跑strict decoder/roundtrip/金额与epoch反例。','3. W02按01的部署单元建立可启动、可迁移、可查询operation的最小服务；同进程的tenant/gateway仍分别验证DB权限、事务与写入边界。','4. 按第9节先接默认App到TKE，再以相同业务链验证Agent独立WebUI组合；不先搬完所有Control Plane方法。','5. 每个PR有明确F/W编号、真实caller切换、保留历史义务、focused checks和来源证据；只有用户要求才在实际任务中执行commit/push/PR。','','## 6. 五方怎样协同而不再次定义业务','','| 角色 | 任务开始前的输入 | 交给下一方的产物 | 拒收条件 |','|---|---|---|---|','| 产品 | 12/13和F故事 | 冻结结果/费用/数据后果与接受用例 | 仍要求研发决定补差/退款、删除范围 |','| 架构 | 01/06/09及W依赖 | 单writer/真实调用、跨域恢复与迁移边界 | 新框架/旁路状态/跨Owner DB权限 |','| 后端 | 02/03/proto/事件、对应W写集 | 真实API/RPC/DB/状态、可重放正反例 | 类型齐全但业务断点/unknown乱重试 |','| 前端/UX | 04/07/11及原型、真实DTO | 页面、表单、完整状态/恢复、真实BFF接入 | 只做静态UI或自造价格/成功状态 |','| QA/客户代表 | 同一个F故事与W接受标准 | 正常/失败/权限/重复/恢复录屏与证据 | 只能演示happy path，客户无法解释费用/数据结果 |','','## 7. 工程验证分层与执行环境','','规格脚本本轮可以运行；上面Go/npm/浏览器/qualification命令是实施后验收要求。现有service实现不代表本次默认App新合同已实现；不伪造go test通过或执行真钱测试补空白。','','开发环境使用现有Go/TypeScript/PostgreSQL与已决定协议；各语言工具及构建器在W01锁版。服务端访问Secrets走已批准注入边界，配置/权限/仓库凭据由Instance提供真实部署值，不写进文档或Git。','','外部资金测试先用明确隔离authority fixture，真实Gateway/provider资格只按W29限定授权。生产网络只能通过Instance保护runner；Cloud不得dispatch Instance部署。','','## 8. 端点/任务覆盖与可开工判断','','`checks/development_plan.json`由本任务书同源生成，列每个W/F/operationId/Owner表/内部RPC/代码落点及依赖；`checks/validate_development_plan.py`检查全部当前API唯一主实现、Owner表和内部RPC无漏项、17F覆盖、已存在路径、依赖无环和前端消费接口存在。数量从实际契约读取，不硬编码通过。','','阶段完成要有真实交付物与对应证据；本任务书的规划状态不替代docs/status.md中的实际进展。完整开发方案已给出不代表这些工作包已经完成，正常实施/联调/Instance接受仍按08/09执行。']
lines += ['', '## 9. Tencent/TKE可直接开发的切片与窗口', '',
 '本节是上述W的细化，不新增业务Owner或平行工作包编号。每个slice只消费明确的前置产物；整个W的后续验收不倒灌为首链开工门槛。产品/字段/异步规则仍以12/03/02/06为准。',
 '当前可以直接开工W01.application-contracts；其余窗口先做已有边界内工作，新增默认App字段的接线消费W01同版合同。不得分别猜DTO。', '',
 '五个Cloud窗口：一个统筹/共享写集窗口，四个独立owner实现窗口；Instance在需要安装时单独启用，不与Cloud源码集成混成一个writer。任务派发及线程身份记录在source evidence；本节只定义唯一任务边界。', '',
 '| 窗口 | 独占实现面 | 交接结果 |', '|---|---|---|']
for window in windows:
 lines.append('| '+window['title']+' | '+', '.join('`'+p+'`' for p in window['scope'])+' | '+window['handoff']+' |')
lines += ['', '共享proto/生成代码、owner启动基础设施、Dockerfile/安装schema、Console共享router和status/roadmap都由统筹串行吸收；业务窗口不得同时改这些文件。独立文件可并行，跨域调用者切换与旧writer退休同批串行完成。', '',
 '### 9.1 切片依赖总表', '', '| 原工作包内切片 | 窗口 | 消费的已完成切片 | 实际交付 |', '|---|---|---|---|']
for item in slices:
 lines.append('| `'+item['id']+'` | '+item['window']+' | '+(', '.join(item['startAfter']) or '无')+' | '+item['deliverable']+' |')
lines += ['', '### 9.2 每个切片的输入、写集、验收与证据']
for item in slices:
 lines += ['', '#### '+item['id'], '', '**输入：** '+'；'.join(item['inputRefs']), '', '**写集：** '+', '.join('`'+p+'`' for p in item['writePaths'])]
 if item.get('testWritePaths'): lines += ['', '**测试写授权（开发SSOT，仅精确文件）：** '+', '.join('`'+p+'`' for p in item['testWritePaths'])]
 lines += ['', '**验收：** '+'；'.join(item['verification']), '', '**交付证据：** '+item['completionEvidence'], '', '**未知结果：** '+item['onUnknown']]
lines += ['', '### 9.3 首链之后的原工作包义务', '',
 'W17.replace和W18/W19/W20/W21后续治理覆盖更新回滚、续费/到期、D17升降配、删除/退款、Tenant停用/恢复；W22/W23消费对应真实owner，W24补操作恢复/告警；两种应用模式全部适用。',
 'W25/W30专门处理旧客户的M0–M5与唯一writer移交；新默认App不得冒充legacy_resource_only。W26全F验收仍包含这些结果。W28/W29整包资格及W31同字节正式发布要求不因早期首链通过而取消。',
 '早期TKE首链允许受限安装验证，不赋予新的采购/收费权限；未获授权则准确记录未执行的环节，不用已有资源结果冒充新购闭环。']
lines += ['', '## 10. 四领域窗口派发与共享合同交接', '',
 '本节由同一生成器生成，细化第9节可先做的既有边界内工作。不把W01尚未完成的事实改为READY；四窗口先执行下列独立准备，各后续业务切片仍满足9.1的真实生产者依赖。统筹并行处理W01/W02/W05、BFF/Console共享接线。', '',
 '采用同一未提交工作区、四个独占领域写集，避免旧分支和当前SSOT漂移。禁止切branch、stash/reset、git add/commit/push、自动PR、改他人文件或并行改CodeGraph索引；生成共享文件由统筹串行。各领域只跑聚焦测试，全仓full和浏览器集成由统筹在写集稳定后执行。', '',
 '旧三个session仅作审计输入，不恢复其跨域写权限。既有PR/分支只能按允许写集读取和逐项移植，不能整包cherry-pick或信任旧会话的成功声明。模块内cmd启动接线归其领域；共享启动库/根清单归统筹，W02跨目录修改先交接再串行。', '',
 '产出：可执行领域代码、聚焦检查、原effect/operation恢复证据、未执行边界，以及下述专属append-only证据文件（存在时使用新UTC后缀，不覆盖）。共享合同缺口写在本域证据中，统筹读取后统一修改；领域不另外发布权威DTO文档。']
for entry in parallel_preparation:
 lines += ['', '### '+entry['title'], '',
  '**模型：** `'+entry['model']+'` / `'+entry['reasoningEffort']+'`', '',
  '**独占写集：** '+', '.join('`'+p+'`' for p in entry['writePaths']), '',
  '**立即执行：** '+entry['entryWork'], '',
  '**已查断点（须在本次源码重验）：** '+'；'.join(entry['currentBreakpoints']), '',
  '**后续切片：** '+', '.join(entry['nextSlices']), '',
  '**验证命令：** '+ '；'.join('`'+c+'`' for c in entry['commands']), '',
  '**接受证据：** '+entry['acceptance'], '',
  '**证据落点：** `'+entry['evidencePath']+'`', '',
  '**共享边界：** '+entry['stopBoundary']]
lines += ['', '### 10.1 W01移交必须实际证明的内容', '',
 '1. 03已批准union贯穿QuoteRequest/Quote/Admission/accepted snapshot/Serve Reserve和Deploy/公开读取；新建订单两种应用均runtime readback required，不把缺CapabilityVersion当纯资源单。CreateWorkspace只消费原quoteId；原单禁止接受第二份镜像/金额。',
 '2. 默认App新增runtime_release provenance，原始descriptor bytes/digest/object reference贯通；Agent仍保留Package/Runtime/独立WebUI及三claim。Capability/Runtime的per-use准入、撤销排序和Serve提交证明按06，不以跨库锁或TTL猜释放。',
 '3. 真实新增SQL migration及loader、存量历史规则、生产proto及规格、严格publicjson、全部生成物和consumer同时对齐。保留生产TenantRepositoryBinding/owner_email，不机械改api/v226包名；唯一生产协议与生成映射在canonical owner收敛后验证。',
 '4. W05逐条配对06矩阵的producer/event或ReceiptKind/Ledger decoder/readback。Operation accepted不是effect confirmed，effect confirmed不是receipt committed；未知不重做，ACK丢失原key+hash恢复。',
 '5. 移交凭证列source/schema/generator hashes、真实decoder/consumer/SQL测试与剩余未测项。只有字段接线及持久化消费者实际通过后移除03的pending状态；仅文档、类型定义或generator成功均不够。', '',
 '### 10.2 整条线的完成顺序', '',
 '① 两组合SSOT及四个独立准备 → ② W01同版合同/消费者与W02身份启动 → ③ 默认App：Tenant→Runtime准入→报价/确认→原单→资源→Serve/URL→必要receipt → ④ Agent：COS→三输入Build→TCR原字节回读→Capability→同一业务链 → ⑤ 同Candidate隔离验证及经授权Instance TKE实测。',
 '最终TKE首链必须绑定同一应用输入/输出digest、订单、资源、Deployment、route epoch与真实应用使用及必要receipt；任何一环unknown都不是闭环。Local用作隔离验证与既有发布资格，不成为本次业务主攻替代物。',
 'Instance只安装Cloud及安装配置/Secrets/证书、进行授权的安装/资格动作；每用户编排、Build与部署都在Cloud owners内部完成。本轮派发不授权真实扣款/采购/删除、生产私网访问、Instance部署或正式发布。']
lines += ['', '## 11. 四领域交付后的串行集成与上线', '',
 '**执行模型：** `'+serial_integration['model']+'` / `'+serial_integration['reasoningEffort']+'`', '',
 serial_integration['ownership'], '', serial_integration['scope'], '',
 '**审计入口：** `'+serial_integration['auditReceipt']+'`。四窗口只完成本次独立准备，不表示各W整包完成；具体剩余硬断点以审计及真实source为准，不照抄旧session结论。', '',
 '**授权边界：** '+serial_integration['authorization'], '',
 '**最终接受：** '+serial_integration['completion']]
for stage in serial_integration['stages']:
 lines += ['', '### 11.'+str(stage['order'])+' '+stage['title'], '',
  '**对应原工作包：** '+', '.join(stage['workPackages']), '',
  '**实施：** '+stage['action'], '', '**验收后进入下一段：** '+stage['accept']]
lines += ['', '此处为同一工作包的串行收口次序，不新增业务owner、中央workflow或第二套领域合同。新串行窗口应持续实施至上线接受或精确外部条件缺失，不能完成一个局部slice后再次只交计划或请求泛化继续确认。']
(OUT/'14_implementation_work_packages.md').write_text('\n'.join(lines)+'\n')
print(f'rendered {len(W)} work packages / {len(api)} primary API assignments')
missing=[(w['id'],x) for w in W for x in w['existingReadPaths'] if not (C/Path(x)).exists()]
print('missing source paths:',missing)
