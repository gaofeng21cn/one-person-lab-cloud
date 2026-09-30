# 06 功能链、状态转换与副作用证据

> 每条功能链对应00的F编号；API/DTO在03，字段/约束在02，UI在04。本文不再复制另一套DTO/状态枚举。
>
> **业务已采用，新的wire仍待W01贯通**：2026-09-29确认`tencent-tke`为首条交付主线；新客户一次确认后由后台完成部署，不选Package/独立WebUI即批准默认`opl-app`及其发布者声明的内置UI。第1–16节明确此次业务时序，不宣称当前API、SQL、proto、events、机器flows或运行时代码已实现。**第17节原样保留为W01迁移前wire baseline**：其中仅Capability输入、Runtime Control实例/Secret caller、Fabric部署/路由等残留不是新业务授权；须由W01集成负责人协调W01及真实消费者后再生成，不能用其静态通过证明新业务已贯通。

## 1. 全部写操作的共同执行语义

1. 在Owner事务内验证权限/版本前置、保存请求hash、幂等身份、Operation及业务意图；提交后才返回接受响应；异步操作返回202 Operation，创建BuildJob可返回201及其operationId，不代表构建完成。
2. 同一Workspace只允许一个已接受且未达确定终态的变更操作；未决旧操作不能被新请求抢占。完成/确定取消后才分配下一epoch，旧网络请求由副作用Owner（Serve运行/路由、Fabric资源）及其执行端fence拒绝。同一Owner聚合用数据库行版本CAS/锁串行；每次执行记录epoch。旧worker失去lease后不得提交结果或发新的副作用。
3. 外部调用前持久记录原始actionId/idempotencyKey/请求摘要/目标身份；调用后保存confirmed/rejected/unknown及观察证据。
4. 对unknown先按原身份权威读回；不把timeout当failed，不自动逆操作。Operation可awaiting_confirmation，无法自动推进时needs_attention且给精确原因/允许动作。
5. HTTP202只是接受；Operation.succeeded必须满足本功能终点证据。页面恢复只读Operation，重复点击复用相同键，不产生第二购买。
6. 跨域靠typed调用、引用claim与Outbox/Inbox；没有跨库事务、全局锁、补偿引擎或用余额差猜付款。Workspace拥有同一购买/生命周期Saga；Serve唯一拥有两类应用的Deployment、实例、配置生效版本与访问路由；Fabric只写资源/绑定/readback；Runtime Control只写获准Runtime Release目录/不可变发布合同，不管实例。不增加服务。
7. durableOperation是接收Owner持久接受的操作身份，effectID是其在副作用发出前固定的确切执行身份；父Operation保存这些不透明引用与请求摘要，不复制下游状态writer。接收ACK不代表副作用完成，Ledger receipt也不是命令、授权或业务状态writer。仅对契约要求的持久证据追加收据，不要求每轮poll/每次健康观察写Ledger。本文语义名的具体字段和读回端口由W01在既有共享contracts内贯通，不在本次文档中冒称已存在。

Operation取消只在尚未发出不可逆动作且Owner确认可取消时允许；不是每个页面都提供取消。客户/管理员不能凭修改状态字段强行标成功。

## 2. F01/F02：身份、租户与分组

### 登录与授权

BFF获得同源登录上下文/CSRF → 将用户凭据短暂委托Gateway认证 → 读取Gateway用户身份 → Gateway Integration查Cloud成员/Tenant角色 → 建立HttpOnly会话。密码不落库不发事件不记日志；退出销毁会话并取消该浏览器轮询，不停止已接受后端任务。

Tenant Admin可管理本Tenant成员/Namespace；平台管理员可创建Tenant、绑定wallet主体和批准目录。普通成员可按明确package权限查看/协作，不因Package成员身份获得Build、购买或钱包操作权限。一个用户初期最多一个active Tenant。

官方目录属于平台发布命名空间，通过可见性策略提供“可读/可部署”权利；不是任意跨租户私有OCI共享。私有Namespace只属于其Tenant。

### 变更

创建Namespace先校验角色和Tenant active，在唯一(tenantId,name)约束下写入。归档Namespace禁止新上传/Build，已有Workspace继续运行；不物理删除其Package。成员移除/角色变化递增权限版本，新的写命令即时拒绝旧版本；已接受业务Operation按原授权和当前停用策略处理，不靠前端隐藏按钮。

## 3. F03/F06：目录、版本与引用

管理员由Runtime Control准入RuntimeRelease、由Capability准入独立WebUI；均须验证不可变digest、平台、发布合同/Package格式/数据兼容；批准资源套餐必须有Fabric能力与实例采购策略；批准产品价格须给版本、币种、金额、有效期和退款政策。

approved→deprecated：停止新选择，不停止已有Workspace；approved/deprecated→revoked：禁止新Build/部署，已有资源是否强制停止必须独立安全操作，不由目录编辑静默执行。不能修改现有版本digest；变更创建新版本。

Package active→archived：从默认列表隐藏并禁止新上传/Build；历史详情和Build证据保留。Capability ready→deprecated→deleting→deleted；deleting前同事务验证所有活跃claims，冻结新增引用。物理镜像删除另需管理员受限命令及repository+digest共享引用检查。失败保留deleting与明确原因，不恢复可选且继续删除。

### 3.1 Runtime准入与跨域claim的并发次序

默认App的Serve与Agent的Build都必须取得Runtime Control对**这次用途**的持久准入证据，绑定release ID/digest、目录revision、claimant owner/operation、accepted input digest与有限授权。Runtime Control在自身Release事务/CAS中把准入和撤销排序；在撤销之后接受的新用途拒绝。不是读一次approved后就认为跨域原子安全，也不是由Runtime Control预留运行实例。

Capability的runtime_version claim消费并权威读回上述准入证据；consumer在本库保存claim后，以原OwnerCommitEvidence Bind。若准入已确认但Bind/ACK未知，保留占用并恢复原身份；不得按TTL释放。已接受用途按冻结证据完成或显式收尾，不因目录变化自动换Release；不同Workspace/新Build/新部署需要各自准入，不能复用旧grant扩大权限。紧急停止已运行应用是独立授权的Serve动作，不由目录撤销暗中完成。

物理清除前Runtime owner先fence新增用途并收敛在途准入，Capability确认同一artifact全部相关claim已释放，再按既有共享digest保护进行授权删除/readback；没有跨库锁或可变refcount副本。普通目录下架不删除镜像，普通首链也不启动GC。W01须落实typed准入/读回与三方消费者，覆盖撤销竞争、Bind丢ACK和unknown释放拒绝。

## 4. F04/F05：上传、构建、注册

```text
UI→Capability：创建Package/后续版本上传会话
UI→Storage：授权对象直传
UI→Capability：complete；服务端验证精确对象摘要
Capability：PackageVersion=uploaded
UI→Build：选择packageVersionId、获准runtimeVersionId、获准独立webuiVersionId三项
Build接受前：三项均为显式确切版本；非法半选组合（缺独立WebUI或缺Package）直接拒绝
Build：固定精确RuntimeRelease、独立WebUI及发布合同摘要，不接受缺省或“内置UI”替身
Build：以三项不可变输入持久Job+Operation，取得Package/Runtime/WebUI三个输入引用claim
Build worker：validating→building→pushing→registering
Build同事务：输出事实 + build.artifact_confirmed.v1 Outbox
Capability Inbox同事务：去重 + ready CapabilityVersion
Build读回版本ID与digest：succeeded
UI查询Operation/Build：展示“可部署”，链接新版本
```

每个同步断点都有持久身份：UploadSession、PackageVersion、BuildJob、Operation、CapabilityVersion。客户端不靠固定延时猜后端已创建ID。自定义Package必须同时选中独立approved WebUI，没有“保留Runtime内置UI”的Agent构建分支：三项输入必须在接受前解析为确切获准版本并冻结到Build输入与产物manifest，重试读取原快照，不随目录策略变化重选；缺独立WebUI、Package，或出现非法半选组合时拒绝接受，不自动补输入、不替换选择、不降级为默认App。再次构建新WebUI/新Runtime组合创建新Job；retryOf保留原失败记录。详细安全/推送读回见05；W01须同步验证三输入在真实wire与产物中的表达。

默认OPL App不经过此构建链：直接使用Runtime Control目录中approved RuntimeRelease的OCI digest及发布者运行合同，不造fakePackage、Build或CapabilityVersion。官方现成Agent则读取已有ready CapabilityVersion，无需客户再次Build。

## 5. F07：报价与准入（尚不扣钱）

新业务输入为`WorkspaceApplicationSelection`判别联合：`opl_app(runtimeVersionId) | agent(capabilityVersionId)`，加computePlanId、storagePlanId和模型配置。两分支互斥且引用必须完整；不是用可空capabilityVersionId推断默认或legacy。Workspace保存授权目标/接受快照，Serve保存实际当前部署，两者不是第二份current selection。

- **默认App**：不选Package/独立WebUI时，在报价前解析获准的精确runtimeVersionId、RuntimeRelease OCI与发布者运行合同，并显式展示`opl-app`及该Release的内置UI。客户一次确认批准这份快照；后续不再询问是否安装，也不临时追随latest。不创建Package/Build/CapabilityVersion来适配旧接口。
- **Agent**：从Capability读取可部署版本与冻结OCI/Runtime/WebUI/发布合同；自定义Package必须按F04/F05同时选定独立approved WebUI并构建，没有保留内置UI的Agent分支，不能在部署时另换UI。选择RuntimeRelease不是运行实例操作。
- 两分支都验证模型接口、凭据、数据/挂载、健康与入口合同。`tencent-tke`是实例批准的主线profile，不提供客户provider切换；Fabric preflight返回确切能力/容量；缺能力明确拒绝，不降级到Local或裸资源交付。Gateway返回余额和钱包主体授权事实；Catalog生成不可变Quote，绑定应用选择/合同摘要、模型配置、套餐/价格版本、币种总额、周期、有效期及退款政策。
- Quote不是容量预留，也不是扣费；提交时重新确认容量和目录准入。余额预览不等于扣款成功，余额不足只显示补款入口，不开通资源。
- 报价expired、版本撤销、模型不兼容、provider不同或资源能力不足时明确失败并保留选择；需要变更已确认输入时重新报价确认，后台不能自行替换版本。
- 购买请求只接受quoteId+用户确认值/名称，不接受客户价格/外部资源ID。Catalog通过AcceptQuote(quoteId,operationId,inputDigest)绑定唯一操作；Workspace保存返回的原快照，不写Catalog状态。接受后的目录调价/默认变化不修改原义务。

## 6. F08：客户一次确认，后台完整交付

新购默认App与Agent使用同一Workspace Saga、Fabric resources和Serve唯一部署权威；不是两套Launch，也不复用旧resource_only终点。客户获得持久Operation后可关页，后台完成扣款、资源、凭据、部署、验证和证据。

**无环顺序**：接受/授权→原单扣款→Fabric资源确认→Serve Reserve→Gateway Key→Fabric Secret绑定→Serve Deploy/访问确认。Serve Reserve仅在确切resource refs存在后分配稳定deployment/runtime身份，不启动应用、不要求Key或ready；Key绑定此身份，Secret绑定同时引用既有资源/实例。Fabric资源开通不依赖Key、Serve实例或应用健康；Secret绑定不能反过来触发资源采购。Reserve不由Runtime Control执行。

| 顺序/阶段 | Owner/动作 | 进入下一阶段所需证据 | 不确定时 |
|---|---|---|---|
| admission | Workspace持久接受应用选择/Quote/模型/资源方案及Operation；Catalog绑定Quote，CloudIdentity核验已接受义务grant；按分支保留RuntimeRelease或Capability引用 | 唯一Workspace/Operation、原快照/合同/引用及有限授权 | 尚未发钱/资源动作；恢复原接受，不伪造grant |
| debit | Gateway Integration对Tenant钱包主体执行原单扣款 | 原Code/账户/金额/状态精确confirmed | 原单读回，禁止新扣款 |
| resources | Fabric按批准预付方案获取compute/storage/network并挂载 | 资源属于本Operation/Workspace；精确provider identity、配置、volume及attachment读回 | 查原provider effect，不重复采购、不拿应用ready当资源前置 |
| reserve | Workspace将冻结OCI/合同和已确认resource refs交给Serve；Serve持久预留部署/实例身份 | 唯一reservation/deployment/runtime身份与输入摘要匹配；无运行副作用 | 按原请求查Serve，不生成第二实例身份 |
| key | Gateway创建/取得本Workspace托管Key，绑定Serve预留身份 | exact Key身份、模型/group/预算、Secret引用/版本；无明文持久化 | 原Key读回，不盲建第二Key |
| secret | Serve按合同请求Fabric在确切资源绑定Secret引用 | 原资源/实例/Key引用/Secret版本匹配，运行边界可用 | 查原绑定；不是重购或重新生成Key |
| deploy | Serve在Fabric已确认资源上应用冻结OCI/合同、config/data，读回实例 | exact digest、环境/挂载、应用健康、凭据和UI可用 | 不宣称active；保持原部署操作 |
| activation | Serve按Workspace授权及expected predecessor CAS当前Deployment与access binding；以accepted epoch/generation读回 | 唯一current deployment、目标/绑定代际、鉴权/应用探针一致 | 不宣称可打开，不重复收费；原switch读回 |
| evidence/finish | 各副作用Owner提交必要证据，Ledger append/readback；Workspace核对完成事实，CAS entitlement=active及Operation=succeeded | 原单、应用分支/版本/digest、资源、Serve readiness/access与必要receipt精确关联 | 可展示已经确认的局部事实，证据未齐不得宣称整体完成 |

### 6.1 具体async handoff矩阵

以下为**已采用业务语义、待W01落实的交接义务**，不是新增RPC/receipt类型清单。各行通过既有Owner的typed命令/读回和必要Outbox交付；不借Ledger驱动下一阶段。原身份重发仅用于接收Owner已证明幂等/能权威定位的命令；下游不支持安全重放时保留unknown，不盲发。每个effect绑定父operation、输入摘要、目标、epoch/版本及被接受授权；receipt只保存脱敏引用/摘要，不含Key。

| 交接 / 唯一Owner | durableOperation / effectID / 权威readback | 必要Ledger receipt | 丢ACK/重启恢复 | 完成证据 | 拒绝 / unknown |
|---|---|---|---|---|---|
| Agent Build→Capability登记（仅Agent） | BuildJob/Operation；固定输出digest与Outbox eventID；Capability Inbox登记及版本读回 | 构建产物/来源登记证据；默认App不适用，不造空receipt | 同eventID投递、Inbox去重，按原Build查唯一版本 | ready CapabilityVersion及OCI/输入来源全部相等 | 摘要冲突明确拒绝；登记未知不建第二版本、不标Build成功 |
| Workspace→Catalog接受 | Workspace已提交Operation；Catalog持久接受记录、原quote接受effectID及输入摘要；Catalog绑定读回 | 接受快照引用进入购买证据，不为查询另写receipt | 查原quote绑定，只继续同一Operation | Quote唯一绑定本Operation及已确认快照 | 绑定冲突拒绝；unknown不发扣款 |
| Workspace→CloudIdentity grant | CloudIdentity持久授权操作/原grant effectID；原Workspace提交事实与有限grant读回 | 授权审计按契约留证，购买证据关联grant；不逐次鉴权写receipt | 按原已接受义务查grant，不伪造commit或扩权 | action/resource/期限与本Operation的已接受义务相符 | 权限/来源不符拒绝；unknown不发下游副作用 |
| Workspace→Gateway扣款（F08/F11/F12） | Gateway durableOperation、原Code/effectID、钱包/金额/周期；Sub2API原单读回 | 每笔已确认资金动作证据，补差/续费各自原单 | 查原Code，不以余额变化或新HTTP key代替原单 | 原账户/币种/金额/用途/周期confirmed | 明确拒绝停止下游；unknown保留原义务，不重扣/先退款 |
| Workspace→Fabric资源（F08/F11/F12） | Fabric resource operation；每个采购/绑定/resize/续期effectID与provider requestID；确切资源/action读回 | 采购/变更/续期的必要履约证据；普通观察不追加 | 原action/provider ID读回；跨worker按epoch fence | 原资源身份/规格/挂载及目标周期实际满足 | 拒绝保留已执行部分；unknown不重购、不假缩容/延长权益 |
| Workspace→Serve Reserve | Serve durableOperation及原reserve effectID；reservation/deployment/runtime身份读回 | 不强制独立reservation receipt；引用进入交付证据 | 由父操作/请求摘要定位原reservation，不重新分配身份 | 已确认resource refs+OCI/合同与唯一稳定身份一致 | 无资源/引用冲突拒绝；unknown不发Key命令 |
| Workspace→Gateway Key | Gateway durableOperation、原create/bind effectID；按操作与Key ID读回 | Key绑定/安全审计按契约持证，交付证据仅引用指纹/版本 | 查原Key与绑定；需要正文时仅授权Secret运行边界获取 | 模型/用途/实例绑定及Secret引用一致 | 确定拒绝停部署；unknown不盲建第二Key |
| Serve→Fabric Secret绑定 | Fabric binding operation/effectID；资源/实例/Secret版本读回 | 必要绑定证据并入交付证据；无Secret正文 | 原binding读回并继续相同版本，不重发新凭据 | 合同声明的确切绑定可供Serve运行边界使用 | 绑定不符拒绝；unknown不启动未经确认配置 |
| Workspace→Serve Deploy/更新/模型（F08–F10） | Serve durableOperation；deploy/reload effectID、实例/配置版本；Serve runtime adapter实际读回 | 必要部署/配置生效证据；不为每轮health/poll建收据 | 原动作查询applied digest/configuration version，原epoch恢复 | 合同、镜像、持久数据、模型生效版本及应用健康相符 | 确定失败按兼容规则收尾；unknown保留最后确认事实，禁止报ready |
| Serve→Serve Access切换/回滚（F08/F10） | Serve switch operation/effectID；expected route generation/epoch；Serve access-binding readback | 选中/切换或回滚完成证据；独立记录经Instance入口的真实请求验收 | 原switch读回后完成同一Serve本地CAS；不发新switch抢占unknown | Serve唯一route binding、current Deployment与readiness一致；TKE请求到达该binding目标 | 旧epoch/generation拒绝；unknown阻止后续切换；Ingress配置和Serve DB不形成双writer |
| Workspace→Serve暂停/恢复（F11/F12/F15） | Serve durableOperation及stop/start effectID；原实例/资源/授权周期读回 | 必要生命周期转换证据，不把停用字段当停机证明 | 原动作/实例读回；不创建替代资源或续费单 | 访问关闭/运行停止，或原资源上应用与访问实际恢复 | 确定不具恢复能力明示；unknown不报已暂停/恢复 |
| Workspace→Serve Retire（F13/closeout） | Serve retirement operation/effectID；原deployment/runtime/access读回 | retirement/访问关闭证据，关联后续删除 | 查原route撤销/stop动作；先封入口再停止并确认absence | 不再有当前可访问/运行实例及有效注入使用者 | unknown不得把资源/Workspace标已删或提前退款 |
| Workspace→Fabric释放（F13/closeout） | Fabric delete operation及逐项effectID；Secret binding/attachment/volume/compute/network精确读回 | 资源删除/absence证据，绑定原资源身份 | 原provider delete ID读回；执行依赖顺序，不凭列表缺项重试采购 | Serve已退役；绑定/指定资源absence，保留义务明确 | 部分失败逐项保留；unknown不宣称完整删除 |
| Workspace→Gateway退款 | 独立refund Operation、原charge与refund effectID；原钱包/剩余可退额/退款原单读回 | 每笔确认退款证据，与删除/失败closeout证据关联 | 原退款读回；unknown额度仍占原单可退空间 | 原目标/原金额政策/confirmed退款匹配 | 拒绝有原因；unknown单列，不覆盖已完成的删除状态 |
| 副作用Owner→Ledger→父Operation核对 | 生产证据Owner持久交付记录/必要Outbox，固定evidence reference与摘要；Ledger按引用精确读回 | 只追加以上契约必要的终态/审计证据；不按poll次数生成 | append ACK丢失按原reference查相同receipt；不重做已确认副作用 | receipt匹配已确认Owner事实；父Owner自行CAS完成 | 摘要冲突拒绝并排查；Ledger不可用仅使必要证据待确认，不倒写资源/资金状态 |

### 6.2 证据实现边界：Operation、观察引用与Ledger收据不是同义词

现有`runtime_deploy/runtime_reload/runtime_retire`及`update_workspace/rollback_workspace`操作种类可以承载两种应用，不新增仅按默认App命名的操作体系。已有`serve.agent_readiness_observed.v1`生产者/Outbox/Ledger消费者应优先扩展复用；`deployment/rollback` enum或`readinessReceiptId`字段存在不证明相应写入/验证已实现。当前Ledger coordination的AppendReceipt仅实现Workspace的`local_no_charge`，不能直接当Tencent交易/Serve通用证据接口。

W05必须逐条将上表义务映射到真实事件/ReceiptKind、producer身份、严格payload decoder、原evidence key/hash、不可变存储与授权readback；需要新增类型时同版改两端和拒绝测试。`fabric-application-readback:...`等外部观察引用不是Ledger分配的receipt ID。默认App不发送Build/Capability注册事件，也不为其伪造WebUI字段；Agent的三输入注册事件按真实独立WebUI版本登记，不塞假字段让旧Ledger测试通过。

客户只读本Tenant对象的owner产品证据投影，BFF按既有会话授权聚合所需结果/receipt引用；不能为了进度显示开放platform_admin的全局Ledger查询。receipt写入失败只恢复原证据交付，不重新执行已confirmed的采购/扣款/部署。每轮GET/健康poll无需新receipt；需要收据的确定转换则在收据确认前保留“证据待确认”，不显示整体成功。

### 6.3 确定失败与释放

- 扣款发出前准入确定失败：无购买，释放引用，Workspace记录失败。扣款明确拒绝：不建资源/Key，保留原错误。
- 扣款已确认且后续可恢复：原Operation继续，不重新收费；客户关页不影响。任何unknown先按矩阵原身份读回，不自动反向操作。
- 确定无法交付才进入有授权的closeout：Serve关闭访问/退役实例（仅reserve则释放原预留）、Fabric释放Secret绑定→挂载→按依赖释放存储/计算/网络；每步确切absence与必要Ledger证据确认后，按原单政策单独退款。
- 资源尚未交付不等于无采购；部分资源/Key/数据义务必须保留。任一资源或资金unknown，Operation保持awaiting_confirmation或needs_attention，不假称failed/已删/已退。
- Key归Gateway；删除注入物不等于撤销原Key。Key留存与显式撤销沿原义务，不能将撤销暗加为退款前置。

## 7. F09：详情、访问与配置更新

列表/详情聚合Workspace业务状态、Serve当前Deployment/实例/访问观测、Fabric资源观测、Gateway余额/用量；每个来源保留observedAt与不可用原因，不用其他来源替代事实。

“打开”仅在当前绑定、授权与实时ready满足时返回accessUrl；服务端仍逐请求鉴权。应用使用自己的origin/cookies，平台session/service token不得传入应用。404/502/静态资产/SSE断流按实际错误呈现，不能用SPA首页替代资源响应。

模型配置更新对默认App和Agent都只接受其冻结发布合同声明的模型字段与expectedConfigurationVersion；Workspace校验权益/目标授权并持久化模型选择意图/配置版本，串行发起变更；Serve接收该版本，持久reload动作并通过发布者声明接口执行、读回实际applied version，随后CAS记录运行生效事实。Workspace不复制Serve实例状态，Serve不另写一份Workspace模型意图。Runtime Control不写实例配置，Fabric不执行reload；缺模型能力在准入拒绝，不能以默认App绕过校验。失败保留最后确认配置，不在UI乐观显示未生效模型。任意应用业务配置不进入Cloud数据库，应用数据/完整config仍由Runtime持久卷和发布者schema拥有。

## 8. F10：版本更新/回滚

1. 默认App选择获准RuntimeRelease，直接固定新OCI/发布者运行合同及其内置UI；Agent选择同Tenant可访问或官方CapabilityVersion，其三项Package/Runtime/独立WebUI组合已在Build冻结。更新请求显式携带目标应用选择，不把opl_app转换成伪Capability；跨分支变更也需显式选择及下述兼容验证。Serve读取并校验当前Deployment及其generation作为前置，Workspace核验授权并占用同一变更序列。
2. 验证数据兼容、镜像平台、资源容量、模型与Secret契约；不可逆数据迁移必须已有发布者批准的迁移/备份与恢复规则，否则准入拒绝。
3. 按分支取得RuntimeRelease或Capability版本引用并由Serve持久新Deployment；Serve保留旧current，Workspace保留原购买事实，不复制部署指针。
4. Serve创建新实例或按发布者支持的停止旧实例后替换路径执行；同一可写数据卷不得同时被两个不支持并发的实例挂载写入。
5. Serve分配execution_epoch并在Serve owner内按route generation/epoch串行化切换；unknown旧switch先按原operation/switch id读回，不抢占。验证新实例真实readiness后，Serve在同一owner事务中CAS更新access binding及current Deployment；只有该事务/readback确认后才将新Deployment置为active并supersede旧Deployment。TKE Ingress是稳定安装入口，不参与每Workspace切换；对外流量经Serve access entry按已提交binding转发。
6. 失败且旧数据仍兼容时进入rolling_back，由Serve以新的原义switch CAS恢复旧binding，并读回旧目标仍ready；不能仅改Deployment字段称回滚成功。独立的TKE HTTP验收证明实际请求到达已确认目标，不构成第二路由writer。
7. 旧Runtime的非活动保留至多1天是原讨论要求；执行清理前必须读回非选中、无数据/恢复义务，再删除运行资源并释放claim。超时不能强删仍活动实例。
8. 更新镜像不再次扣Workspace购买费；若资源调整必要先走独立F11报价确认。新Runtime发布只显示可更新提示，不自动替换。

## 9. F11：升级与下一期降配（D17已确认）

完整规则唯一Owner为13_plan_change_policy.md及固定typed政策。Catalog计算报价；Workspace拥有PlanChange与订阅原单；Gateway执行补差/退款；Fabric只执行资源变更，Serve协调应用排空/停止、恢复及运行readback；Ledger持证。默认App和Agent都按各自已冻结发布合同验证，不以是否有CapabilityVersion跳过运行验证；resource-only不适用项仅限F16已证明的旧对象。

**立即升级链**：准入当前周期/原计划版本→固定T/S/E及旧新月价报价→Workspace CAS接受PlanChange→绑定Quote→原单一次扣补差（0金额不调Gateway）→必要时Serve按执行计划确认排空/停止→Fabric执行确切资源计划→Fabric挂载/文件系统与Serve应用恢复实际验证→Workspace CAS写新active套餐及appliedAt、原E不变→Ledger回执。

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

Gateway确切扣费确认后，Fabric执行必要provider续期并读回；默认App与Agent均由Serve核对当前部署/原OCI/数据/Key和新周期授权，必要时恢复同一实例及访问并确认readiness；Workspace才CAS新的expiresAt/周期并核对必要续费证据。续费不重Build、不换RuntimeRelease/UI、不产生第二部署或收费义务。有预约降配按F11唯一目标期执行，不先续旧高配。资源或应用恢复unknown，局部付款事实仍可展示，但不把整个续费/恢复显示为完成。

自动续费必须有显式renewalMode/consent快照，关闭仅影响尚未接受的新周期；不得因用户退出登录撤销已经确认的财务义务。迁移原订阅模式和授权，不统一改为手动。

余额不足：不发起采购/续期，显示到期时间和补款入口；到期时Workspace按计费边界持久暂停义务，由Serve关闭访问/停止运行并读回，Workspace保存suspended及原因；Runtime Control不参与启停。充值不自动宣称恢复；客户或明确授权的自动续费策略重新提交同一周期义务，确认资源仍可用后恢复。

不能承诺固定CBS可恢复期：只有实际资源仍在且授权可用时才恢复。已销毁则告知必须新建，不把Tenant十五天恢复当数据恢复承诺。

## 11. F13：删除与退款两条可观察结果

客户确认“删除运行环境和资源；持久数据可能不可恢复；RuntimeRelease/Agent制品及Package/Build历史保留”→Workspace持久delete Operation并禁止新变更→Serve先撤销访问、退役当前/保留实例并确认absence→Fabric按真实依赖释放注入Secret绑定、挂载、存储、计算/网络资源→每步Owner权威absence及必要Ledger删除receipt精确读回→Workspace deleted。默认App和Agent同链；默认App没有Package/Build历史可删，不因缺CapabilityVersion跳过Serve retirement。Serve管理访问路由删除，Fabric不把基础网络absence冒充应用入口撤销；按真实引用释放版本claim，不删除共享RuntimeRelease/OCI。

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

active→suspended：CloudIdentity先撤销新交互写授权，持久Tenant Operation，再让Workspace owner按原Tenant停用operation对旗下Workspace创建受限暂停子操作。每个子操作独立读取Serve访问/运行暂停结果及必要Fabric资源事实，保留原付费到期日和资源身份，不能仅改Tenant字段声称应用停了。

suspended→active是独立reenableTenant，不检查删除恢复窗口，也不先删除任何Workspace。恢复访问授权后，Workspace owner只恢复被此次Tenant停用操作暂停、仍在原已付周期、确切原资源存在的工作区；其他原因停用、到期、已删除或身份不确定的对象只给明确skip/error，不续费、不重购、不重建。Tenant权限恢复和各应用复机分开显示，通过getTenantLifecycleOperation可读每个子结果。

active/suspended→deleting：平台管理员确认影响清单；冻结新购/Build/部署，枚举Workspace并逐个创建F13删除操作。资产进入平台管理员私有托管，保留原tenantId/owner provenance，不变成Public/official。任何未完成子操作都保留在Tenant Operation，不先标deleted。

全部子操作和权限关闭证据完成→deleted，记录deletedAt及restoreUntil=deletedAt+15天。恢复只重启Tenant/成员/保留资产的授权，不恢复已删除CVM/CBS，不自动退款/采购/创建Workspace。恢复期间已发出的删除不能反向假取消，必须先得到确定子操作结果。

钱包本身仍由Gateway拥有；Tenant删除不清空或删除Sub2API账户，也不自动将全部余额转账。只把各Workspace符合原单政策的退款退回原钱包；其他提现/关闭钱包属于Gateway显式功能，不在Cloud杜撰。

## 14. F16/F17：迁移与运维

按09的M0–M5只迁移冻结来源清单中的旧账户、resource_only/retained full Launch、已部署应用和未决资金/删除义务；旧schema/provider IDs/原Code/receipt身份不能重造。新默认opl_app走F07/F08完整交付，不进入F16、不标legacy_resource_only。旧裸资源第一次显式部署可选择获准默认App或Agent，但沿原资源/账期，不重购；旧已部署App保留真实来源，不为套用新默认而重分类。Instance运行变更另有授权和不可变回执。

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

`contracts/domain_flows.json`及第17节目前只绑定W01迁移前的RPC/REST/Owner表基线；既有检查器能验证该基线的引用/类型，不代表此次应用判别联合、UI正规化、resource→Serve Reserve→Key顺序与新handoff已落wire。W01集成负责人须在W01同步03/SQL/proto/events、真实caller及生成器/机器flows后再生成此索引并验证；本轮保持后半区逐字节不变，不新建工作流引擎。

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
| 3 | 按当前入口/阶段与06/13状态机前置执行；不是无条件调用；workspace→fabric / `FabricCoordination.AdmitResources` | `ResourceAdmissionRequest` → `AdmissionResult` | 只读/由原Owner管理 | provider资源能力与实际执行容量准入；Serve路由由Serve独立核验；不能静默换provider或降能力 |
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
| 10 | 按当前入口/阶段与06/13状态机前置执行；不是无条件调用；serve→serve / `ServeAccessControl.FenceRouteEpoch` | `FenceRouteEpochCommand` → `RouteReadback` | serve.access_bindings, serve.access_switches | Serve本地CAS确认新epoch，target/generation不变；未知旧switch先读回，不抢占 |
| 11 | 按当前入口/阶段与06/13状态机前置执行；不是无条件调用；serve→serve / `ServeAccessControl.ActivateRoute` | `RouteActivateCommand` → `RouteReadback` | serve.access_bindings, serve.access_switches | epoch/generation/target精确，Serve本地CAS/readback确认；旧epoch拒绝，丢响应ObserveRoute |
| 12 | 按当前入口/阶段与06/13状态机前置执行；不是无条件调用；workspace→ledger / `LedgerCoordination.AppendReceipt` | `AppendReceiptRequest` → `Receipt` | ledger.receipts | 返回receipt身份，另ReadReceiptByReference核对原输入；同idempotency key读回，不填假Workspace或重写receipt |

**终点**：Serve本域CAS active Deployment/访问generation后，receipt核对；客户可打开当前应用

Serve的active Deployment、readiness和access binding是当前应用交付权威；Fabric仅有资源事实，Workspace仅有授权和业务目标，没有跨库原子提交幻觉

### F09 使用、模型配置与应用登录

入口：`getWorkspace`, `getWorkspaceAccess`, `getWorkspaceModels`, `updateWorkspaceModels`, `revealWorkspaceApplicationCredentials`

| 索引 | 触发条件 / Caller→Owner / RPC | 输入→输出 | 接收Owner写入 | 完成证据 / 不确定处理 |
|---:|---|---|---|---|
| 1 | 按当前入口/阶段与06/13状态机前置执行；不是无条件调用；workspace→tenant / `CloudIdentityAuthorization.AuthorizeAction` | `AuthorizationRequest` → `AuthorizationDecision` | 只读/由原Owner管理 | session/grant/action/resource/audience/权限版本确切一致；deny不改用管理员；unknown不发后续副作用 |
| 2 | 按当前入口/阶段与06/13状态机前置执行；不是无条件调用；bff→workspace / `ServeProductService.GetWorkspaceAccess` | `GetWorkspaceAccessRpcRequest` → `WorkspaceAccess` | 只读/由原Owner管理 | 当前部署/访问策略和运行事实一致；按canonical应用登录，不新增SSO |
| 3 | 按当前入口/阶段与06/13状态机前置执行；不是无条件调用；bff→workspace / `WorkspaceProductService.RevealWorkspaceApplicationCredentials` | `RevealWorkspaceApplicationCredentialsRpcRequest` → `WorkspaceApplicationCredentials` | 只读/由原Owner管理 | 所有者权限+当前声明workspace_admin_password+实际ready，只一次性用户名/密码；no-store不缓存；不返回GatewayKey或session_secret |
| 4 | 按当前入口/阶段与06/13状态机前置执行；不是无条件调用；workspace→gateway / `GatewayCoordination.CreateManagedKey` | `ManagedKeyCommand` → `ManagedKeyBinding` | gateway.key_bindings | exact Workspace/runtime/model set；仅返回opaque binding与Secret delivery reference；不返回明文Key；unknown按Gateway action readback处理，不盲建第二Key |
| 5 | 按当前入口/阶段与06/13状态机前置执行；不是无条件调用；workspace→fabric / `FabricCoordination.BindSecret` | `SecretBindingCommand` → `SecretBindingReadback` | fabric.secret_bindings | Gateway返回的opaque Secret reference实际绑定到目标runtime并读回confirmed version；unknown不进入Serve；不把Gateway Key当任意环境变量公开 |
| 6 | 按当前入口/阶段与06/13状态机前置执行；不是无条件调用；workspace→serve / `ServeAgentCoordination.ReloadModels` | `RuntimeReloadCommand` → `Operation` | serve.agent_runtime_actions | 目标配置版本+selections及opaque RuntimeManagedKeyBinding实际应用；Serve不铸造Gateway Key；保存成功不等于reload成功 |
| 7 | 按当前入口/阶段与06/13状态机前置执行；不是无条件调用；serve→serve / `ServeRuntimeAdapter.ObserveRuntime` | `RuntimeReadbackRequest` → `RuntimeReadback` | 只读/由原Owner管理 | appliedVersion和选择相同，运行状态真实；unknown显示应用中/待核实，不覆盖已确认配置 |

**终点**：打开真实应用；模型实际生效；应用管理员凭据仅在既有授权reveal路径一次性显示

### F10 更新、切换和回滚

入口：`updateWorkspaceVersion`, `rollbackWorkspace`

| 索引 | 触发条件 / Caller→Owner / RPC | 输入→输出 | 接收Owner写入 | 完成证据 / 不确定处理 |
|---:|---|---|---|---|
| 1 | 按当前入口/阶段与06/13状态机前置执行；不是无条件调用；workspace→tenant / `CloudIdentityAuthorization.AuthorizeAction` | `AuthorizationRequest` → `AuthorizationDecision` | 只读/由原Owner管理 | session/grant/action/resource/audience/权限版本确切一致；deny不改用管理员；unknown不发后续副作用 |
| 2 | 按当前入口/阶段与06/13状态机前置执行；不是无条件调用；workspace→capability / `CapabilityCoordination.ResolvePublisherContract` | `ResolvePublisherContractRequest` → `ResolvedPublisherContract` | 只读/由原Owner管理 | 目标版本完整契约与数据兼容；不支持安全回滚的迁移拒绝，不猜semver |
| 3 | 按当前入口/阶段与06/13状态机前置执行；不是无条件调用；serve→serve / `ServeAccessControl.FenceRouteEpoch` | `FenceRouteEpochCommand` → `RouteReadback` | serve.access_switches, serve.access_bindings | 新epoch先在Serve owner内确认；旧未知切换不被强行覆盖 |
| 4 | 按当前入口/阶段与06/13状态机前置执行；不是无条件调用；workspace→serve / `ServeAgentCoordination.Deploy` | `RuntimeDeployCommand` → `RuntimeReadback` | serve.agent_runtime_instances, serve.agent_runtime_actions | 新实例实际验证且不违反可写卷并发限制；保留旧选中版本/原数据义务 |
| 5 | 按当前入口/阶段与06/13状态机前置执行；不是无条件调用；serve→serve / `ServeAccessControl.ActivateRoute` | `RouteActivateCommand` → `RouteReadback` | serve.access_switches, serve.access_bindings | 新target通过Serve本地CAS确认；丢响应按原switch读回 |
| 6 | 新部署明确失败且旧数据/路由允许安全回滚；serve→serve / `ServeAccessControl.RollbackRoute` | `RouteRollbackCommand` → `RouteReadback` | serve.access_switches, serve.access_bindings | 原目标+原switch身份+当前expected generation可核对；不能仅改Deployment字段称回滚成功 |

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
