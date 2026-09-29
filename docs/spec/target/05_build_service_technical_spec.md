# 05 Build执行规格

> 对应F03–F06/F10，并说明F07–F09默认应用为何不经过Build。不是部署脚本。既有输入字段/接口见03，存储字段见02，状态词以00为准；下面的Build三输入与Workspace应用选择是**2026-09-29已采用、待W01贯通的合同迁移**，产品定义见[12](12_product_spec.md)，跨Owner合同与实施验收见[14](14_implementation_work_packages.md)。本轮不修改03/SQL/proto/机器inventory/生成器，不宣称新联合类型已被API或运行时代码支持。

## 1. 两种产品组合与Build边界

| 组合 | 精确选择与执行 | Build责任 |
|---|---|---|
| 不选Package且不选独立WebUI（默认App） | `WorkspaceApplicationSelection = opl_app(runtimeVersionId)`；Runtime Control提供获准RuntimeRelease的不可变OCI及发布者声明的内置UI，Workspace购买后交Serve部署 | 不调用Build，不伪造Package、BuildJob或CapabilityVersion |
| Package + 精确Runtime + 独立approved WebUI | 显式三项真实输入`packageVersionId`、`runtimeVersionId`、`webuiVersionId` | Build固定三项输入并生成CapabilityVersion，部署时选择`agent(capabilityVersionId)` |

以上选择的合同定义统一引用12/14，本文不另立wire字段名。本次严格只有这两种组合，没有第三种，也不存在“Package＋Runtime＋保留Runtime内置UI”的Build分支。非法半选组合——只选Package不选独立WebUI、只选独立WebUI不选Package、在默认App上叠加独立WebUI——一律由服务端明确拒绝：不自动补Package、不自动补独立WebUI、不静默降级为默认App，也不为非法组合伪造空包或空界面。Agent制作路径的独立WebUI必须是获准目录中的确切版本，不能用缺省、空值或“Runtime内置UI”表达。

下文Package上传、Build任务及Capability注册仅适用于单一的agent制作路径：用户创建/选择Package → 创建上传会话 → 将字节直接传入Storage Provider → complete上传并确认摘要 → 选择获准精确RuntimeRelease与获准独立WebUI确切版本 → createBuild → 返回持久BuildJob/ownerOperation身份 → 查询真实阶段与receipt → ready CapabilityVersion → 可部署。

默认与自选最终共用Workspace资源购买、Fabric交付、Serve运行/readiness/access及Ledger证据链；默认不是resource_only，新Workspace一次确认应用+套餐后后台自动部署。不增加独立服务，也不改变Build/Runtime Control/Workspace/Serve各自的数据写权。

上传和构建不是同一个动作：一个PackageVersion可组合不同独立WebUI或重新批准的Runtime，产生多个不可变OCI版本。上传完成不自动声称构建完成。前端在任务ID返回前不能跳“构建成功”；关闭浏览器不影响已接受任务。

## 2. 构建输入冻结

| 输入事实 | 来源 | 验证/存储规则 |
|---|---|---|
| tenantId/actor | 经认证的请求上下文 | 非客户自由提交；权限在Build和Capability双边检查 |
| packageVersionId | 已完成上传的Package版本 | Capability owner读回uploaded、objectRef/sha256/sizeBytes；由worker取得精确输入target的引用claim |
| webuiVersionId（W01待贯通） | 用户明确确认的获准独立WebUI确切版本 | 固定批准WebUI完整artifact及兼容协议版本，不能是任意URL，也不能为空 |
| runtimeVersionId | 用户确认的获准精确RuntimeRelease；策略仅提供尚未选择时的候选 | 接受任务时按该ID读回并固定repository/digest/platform和发布契约，不能由管理员默认策略覆盖，也不能执行时改用latest |
| builderContractVersion | 本Build服务批准的构建契约 | 记录模板/构建前端版本digest；不靠字符串猜路径 |
| targetPlatform | Runtime/Package/独立WebUI三输入的兼容结果 | 精确os/arch与镜像平台；不静默跨架构 |
| targetRepository | Tenant Namespace→批准Registry配置 | 服务端派生，用户不可指定他人repository |
| inputDigest | 上述规范化不可变输入的摘要 | 用作审计/重放一致性，不替代租户授权 |
| registry/storageCredentialRef | Instance核准的短期credential绑定 | 只在执行边界获取，任务表/日志无明文 |

请求接受采用固定顺序：先只读ResolveBuildInput校验客户提交的三项精确版本，固定目录版本/完整发布描述；Build本域事务写Job queued、input snapshot、Operation与幂等身份后返回201（任务创建而非构建完成）。worker读取实际Package字节或调用构建器前，取得PackageVersion、RuntimeVersion、WebuiVersion三个输入target的claim。保存全部claim IDs并以可读回的Owner提交证据Bind。若期间任一版本撤销/删除，明确失败，不先构建再补引用，不换成另一个批准版本。

03的CreateBuildRequest已经要求packageVersionId、runtimeVersionId与独立webuiVersionId三项，即本案的三输入合同；输入快照、claim规则、事件/manifest及注册消费者仍须由W01按三输入贯通。不能通过可空字段、虚构ID或defaultWebuiVersionId伪装成合法三输入。Runtime默认策略只影响尚未确认的候选，不能改写用户显式选择、已接受任务或已有Workspace；策略变化后必须维持原合法选择，已不合法则明确拒绝。

客户输入与管理员输入分离。构建不接受Dockerfile、任意基础镜像、任意外网脚本作为未验证Package字段执行；允许的Runtime构建扩展由其发布契约明确、管理员批准。

## 3. Package上传协议

- 文件大小上限、分片大小、会话有效期来自实例批准的UploadPolicy；首次创建会话返回这些明确值，UI据此校验，不硬编码500MB/10个包。
- BFF签发受限上传许可而不代理大文件；凭据只可写本次对象/分片，不可读列其他租户对象。
- complete只接受已登记分片序号/etag/字节摘要等typed事实；服务端读取Storage Provider精确对象版本，验证实际字节SHA-256与总大小，不能仅信任客户声明或把ETag当SHA-256。
- 同一个Package内versionLabel唯一；版本确认后字节不可变。重复complete相同输入返回同一版本；不同摘要返回幂等/版本冲突。
- 过期会话续传要重新授权同一未完成版本和对象身份；已拒绝版本不能被覆盖成另一包。重新上传创建新的upload identity，不修改成功版本字节。
- 解包验证拒绝绝对路径、..穿越、危险软/硬链接、设备文件、过度展开；按发布者Package schema检查manifest、资源限制与受支持格式。不能以“OMA生成”跳过不可信输入验证。
- 取消/过期上传对象只能由受限对象存储清理流程处理，不触及已确认Package、Build引用或客户运行资源。

## 4. 状态机与每阶段证据

| 当前状态 | 执行 | 成功条件/下一状态 | 明确失败或不确定 |
|---|---|---|---|
| queued | DB原子claim任务和lease；读取冻结输入 | 成功持有执行epoch → validating | worker没拿到lease不执行；不靠进程内锁 |
| validating | 权限、目录、digest、包格式、Storage对象、目标平台检查 | 所有owner确认 → building | 输入非法failed；读取不可用继续原任务读取，不重新建任务 |
| building | 在隔离worker调用批准BuildKit recipe | builder输出精确构建结果 → pushing | 构建确定报错failed；超时先查worker结果，未知needs_attention而非新建并发任务 |
| pushing | 使用builder的Registry exporter推送同一产物 | 远端manifest/digest读回一致 → registering | 不手写逐layer重试引擎；已推送但响应丢失按digest读回 |
| registering | 同事务写build输出+build.artifact_confirmed.v1 Outbox；请求Capability注册结果 | consumer事务完成且精确CapabilityVersion回读 → succeeded | consumer不可用则保留registering，不能重新build或假成功 |
| succeeded | 只读终态 | resultCapabilityVersionId存在且input/output一致 | 后续目录deprecated不改原Build历史 |
| failed | 保存确定的失败阶段/错误/日志引用；发送失败证据 | 客户显式retry创建新job | 原job/input不可改写；释放claim要提供本Owner实际终态/usage读回证据 |
| needs_attention | 保存未知动作身份、最后读回、阻止重复外部动作 | 授权恢复先用原identity读回，再进入确定阶段 | 禁止以默认重试次数到期推断已不存在 |

PackageVersion状态只反映上传。CapabilityVersion只在注册成功时创建ready；不创建缺digest的pending行。Build注册事件重复消费必须返回同一CapabilityVersion。

## 5. 执行隔离与版本组合

- 使用BuildKit作为构建执行器；调用其正式build/export功能，不重新实现OCI layer去重、压缩或推送协议。
- Build coordinator与worker分开职责但同Build服务Owner；worker无Cloud数据库、Sub2API、客户节点管理员凭据。
- Build worker只能访问批准Storage对象、批准基础镜像和目标repository；限制网络、CPU、内存、磁盘、并发及最大展开量，实例配置不得为空。
- PublisherContract采用contracts/publisher-contract.schema.json：构建扩展+现有WorkspaceApplicationRevision的完整模板。它传递Registry repository/digest/platform、构建输入位置/合成方式及原revision入口、端口、探针、依赖、配置、Secret和数据契约。Build只按批准recipe固定替换产物image/revision identity，不改写其他发布者事实。OPL App与第三方各需一个可校验描述，不能全局硬编码/opt/agent或/dist。
- 基础Runtime固定完整repository/digest/platform；独立WebUI作为第三项输入固定完整artifact与兼容协议版本。Agent构建不保留Runtime内置UI作为可选分支，必须显式注入获准独立WebUI。
- Build manifest记录Package sha256、精确runtimeVersionId及Runtime artifact、精确webuiVersionId及WebUI artifact、recipe digest、platform、输出digest与来源。inputDigest覆盖三项输入与版本，不把不同WebUI或不同Runtime合并去重；版本字段供兼容检查，digest供身份检查。
- agent分支更新Runtime或独立WebUI需要重新构建产生新CapabilityVersion，由用户选择部署；opl_app分支由用户确认新的获准RuntimeRelease后直接部署其OCI，不制造Build历史。管理员发布Runtime或修改默认策略均不自动重建、替换已有选择或现有Workspace。
- 数据兼容不是Build成功即可证明：产物必须携带dataCompatibility与升级/回滚条件，Workspace部署F10单独验证。

## 6. 可靠结果交接

Build完成是事实，不是Capability表写权限。Build同事务提交产物+Outbox事件；Capability Inbox唯一键去重，同事务创建版本+记录事件处理结果。现有完成事件build.artifact_confirmed.v1列有buildJobId/packageVersionId/runtimeVersionId/webuiVersionId/artifactDigest/artifactReceiptId，正好表达本Agent构建的三输入；W01须按三输入贯通typed选择、事件版本、strict schema及两端消费者并验证，不假称现事件已被运行时支持。接收者通过Build/receipt typed读回核对精确Package/Runtime/WebUI、inputDigest/platform/manifest，不携带密钥/包原文。

Capability注册成功后ack含版本身份；Build记录关联或经typed查询读回同一结果。ack丢失仅重发同eventId，不产生第二个版本。重试新BuildJob同输入可以产出相同digest，不代表可以覆盖原版本或删除共享OCI。

UI通过原ownerOperation跟踪任务，receipt结果由BFF/所属Owner按Tenant与Build对象授权投影（待W01贯通），不调用平台管理员收据接口；终态后读回Build、CapabilityVersion与artifact receipt，三者匹配才显示“版本可部署”。已收到operationId后刷新/返回只GET原任务。初次createBuild响应丢失而ID未知时，仅在拥有方已实现持久幂等的原接受端点用同key同body取回原Job/Operation；不创建新的Build意图，且不能越过owner重发BuildKit副作用。无法保证该语义时保持unknown并修复owner，不另造只读lookup端点或换key重建。后台原Outbox同eventId交付是Owner内部可靠投递，不是浏览器重发命令；receipt缺失只恢复证据写入，不重跑构建/推送。

失败证据需要Ledger接受明确的build类型且允许无workspaceId。现Ledger当前验证器对无workspaceId有更严格限制；这是F05的owner实现项，必须更新typed contract/validator/consumer测试，不能填假Workspace ID。

## 7. 重试、删除与保留

- 仅Owner确认原任务failed、外部副作用已核清且retryAllowed时，客户显式retry创建新job，保存retryOfBuildJobId；新idempotency key表示新意图，原key重放返回同一新job。旧失败状态/日志不覆盖。
- 自动网络投递重试只作用于幂等Outbox/Inbox或同一digest可读回操作；不自动重跑不确定build、采购或扣费。
- Package归档、Workspace删除都不删除Build历史。Build输入/日志保留策略作为实例配置明确展示；审计必需manifest和任务终态永久保留，日志不得承诺永不清除或含秘密。
- 对物理OCI删除，检查repository+digest全部引用（包括Runtime/WebUI及仍运行实例），不能只按一个CapabilityVersion判断。
- 不把日志脱敏正则当秘密保护的主措施：敏感值不进入日志API；结构化allowlist为主，测试注入canary secret验证不外泄。

## 8. 容量、运维与验收

容量、超时和限额是实例参数，不写“50人必达”“3分钟完成”。必须记录workerCPU/内存/磁盘、并发、排队上限、最大包/展开大小、任务deadline。超出容量返回可见队列/准入错误，不悄悄换构建平台。

F04/F05验收必须包含：首包/新版本、Package＋精确Runtime＋独立WebUI三输入构建、精确Runtime/WebUI自选、策略变化不覆盖选择、重复complete、同key不同输入、过期URL、断点续传、错误摘要/路径穿越、worker重启、推送成功丢响应、注册ack丢失、同digest多版本、目录撤销、跨Tenant日志请求。成功证据包括远端manifest读回、唯一CapabilityVersion、浏览器正确显示；仅YAML解析不算构建运行验收。

## 9. 跨Domain输入/输出闭环验收

Agent Build做端到端测试：唯一构建形态携带Package/Runtime/独立WebUI三个输入target的claim，连同完整ArtifactReference穿过ResolveBuildInput→Job持久化→Acquire/Bind→构建→远端artifact读回→版本事件→Capability注册→新版本查询。断言三项确切版本、输入摘要与产物一致，不能只验证JSON能解析，也不能省略任一输入claim。

另验默认opl_app完全绕过上传/Build/Capability产物注册，直接固定获准RuntimeRelease OCI及内置UI并进入同一Workspace→Fabric→Serve链。主攻`tencent-tke`：两种组合都要在目标provider验收实际镜像、应用响应、内置/独立UI、持久数据及ownerOperation/receipt读回一致；仅Build成功、YAML解析或Local-Docker成功不能替代TKE交付。Instance资格须由`opl-instance-medopl`授权保护流程绑定同一Candidate SHA/digest和provider读回；本轮仅修订规格，不访问生产，不执行真实采购、扣费或资源删除。

反例必须覆盖：只选Package不选独立WebUI；只选独立WebUI不选Package；在默认App上叠加独立WebUI；三输入缺失任一版本ID；独立WebUI与Runtime协议不兼容；策略试图覆盖明确Runtime/UI选择；只有Package、还没有CapabilityVersion的首次构建；第三方Runtime使用非默认端口/Secret路径；repository缺失；Package claim冒充Runtime claim；Bind的Owner提交证据不匹配；释放仍使用中的claim；相同digest来自不同repository；删除/撤销与排队Build竞争。

## 10. 描述与输入摘要的字节身份

DeploymentDescriptor和PublisherContract的内容摘要绑定发布者/Build保存的**确切UTF-8 JSON对象字节**；Storage引用必须带对象版本和SHA-256。JSON对象字段顺序/空白改变产生新字节身份，不要求不同语言重序列化出相同文件。接收方从对象引用读回原字节，验证sha256后使用strict schema解码，并核对typed消息内容与解码结果相同；不得通过任意json.dumps重新算digest后声称原对象改变。

现有WorkspaceApplicationRevision的语义digest继续调用现行Go领域validator/digest逻辑；它与上述完整Build扩展制品的字节digest是不同字段，不能混用。Recipe、输入对象、输出镜像、描述文件各有自己的不可变digest和来源，不用一个“版本号”代替所有身份。
