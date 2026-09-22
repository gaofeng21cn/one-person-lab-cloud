# 07 错误与恢复交接矩阵

> 稳定字符串code以03 ErrorCode和x-error-http-status为唯一机器定义。本文件负责中文解释和允许动作；HTTP状态不是业务code。原型演示不证明运行已验收。

## 1. 共用规则

- 同步拒绝返回Error.code/requestId/fieldErrors；前端按code分支，不解析英文message或原始上游报文。
- 已受理命令的errorCode与observationResult保留在原Operation。GET成功只是读取成功；unknown不是failed。
- 写请求响应丢失保留同一Idempotency-Key和规范化body以取回原身份；取到Operation后只读其进度，不发新副作用。
- 外部unknown只由原Owner按原身份读回，不自动再次扣款/采购、反向退款或宣布资源已不存在。
- 202和非终态getOperation必须给相同秒值的Retry-After/pollAfterSeconds。终态succeeded/failed/cancelled省略两者并停轮询；needs_attention仍按Owner提示查询。429/503严格遵守Retry-After，不自选固定间隔/指数兜底，不转WebSocket。
- 字段错误聚焦当前公开表单字段；越权对象404，平台权限与Tenant角色分开。错误/日志不含密码、Key、SQL、堆栈、服务token或私网地址。
- Build只有retryAllowed=true才显示重试，Owner重验原结果无未决副作用；原Build历史不覆盖。
- 目录版本下架仅墓碑，不称物理制品删除；Tenant重新启用suspended与窗口内restore deleted是不同操作。

## 2. 状态展示

| 事实 | 中文与允许动作 |
|---|---|
| accepted/running | 已受理/处理中；只GET原Operation |
| awaiting_confirmation + unknown | 结果待核实；不重新付费、采购或退款 |
| needs_attention | 需要处理；保留requestId与Owner允许的核对动作 |
| succeeded | 本次操作完成；仍重新读取独立资源、应用、账务与证据事实 |
| failed | 本次操作失败；按下表修正，不默认认为已经退款或清理 |
| cancelled | 本次命令已取消；不能由此推导从未产生费用 |
| 删除/退款 | 资源absence与退款confirmed分开；不把删除受理当全部成功 |
| reenable Tenant | 恢复访问不等于子Workspace全部复机，显示workspaceActions/skipped |

## 3. 完整错误映射

HTTP/异步范围取03，D17已批准。错误不得触发新费用、假回滚或隐式覆盖计划。

| code | HTTP/结果面 | Owner边界与接口 | 中文文案 | 允许动作 |
|---|---|---|---|---|
| `VALIDATION_FAILED` | 422 / 异步 | 公开输入校验；`login`、`createPackage`、`createUpload`、`createQuote`、`createWorkspace` | 输入内容不符合要求，请修改标出的字段。 | 仅按fieldErrors定位；修改后新意图提交 |
| `UNAUTHENTICATED` | 401 | BFF会话校验；`getSession`、`getTenant`、`getOperation` | 登录状态已失效，请重新登录。 | 清理身份缓存并跳安全站内登录入口；不重发业务命令 |
| `FORBIDDEN` | 403 / 异步 | Owner角色/委托授权；`inviteMember`、`deleteWorkspace`、`revealGatewayKey` | 你没有执行此操作的权限。 | 不提供越权重试；联系有权限的管理员 |
| `NOT_FOUND` | 404 / 异步 | Owner对象查询/归属；`getPackage`、`getWorkspace`、`getBuild`、`getOperation` | 找不到此记录，或你无权访问。 | 返回列表；不透露其他Tenant对象是否存在 |
| `CSRF_INVALID` | 403 | BFF写请求校验；`login`、`logout`、`createWorkspace` | 安全校验未通过，请重新读取页面后再操作。 | 刷新CSRF上下文，未知提交保留原key |
| `ORIGIN_REJECTED` | 403 | BFF来源校验；`login`、`createWorkspace`、`deleteWorkspace` | 此页面来源不能提交该操作。 | 回到正式Console入口；不放宽Origin |
| `IDEMPOTENCY_REQUIRED` | 400 | BFF命令准入；`createBuild`、`createWorkspace`、`renewWorkspace` | 请求缺少操作标识，尚未执行。 | 修复客户端提交；不能自动随机重做未知副作用 |
| `IDEMPOTENCY_CONFLICT` | 409 | Owner命令摘要校验；`createBuild`、`createWorkspace`、`deleteWorkspace` | 本次内容与原提交不一致，请核对原操作。 | 保留原记录；明确修改后建立新意图，不能偷偷换key |
| `VERSION_CONFLICT` | 409 / 异步 | Owner版本/CAS；`updateMemberRole`、`updateWorkspaceModels`、`updateWorkspaceVersion`、`adoptWorkspace` | 记录已被其他操作更新，请重新读取后核对。 | 先GET权威版本，再由用户重新确认 |
| `LAST_OWNER` | 409 / 异步 | Tenant成员不变量；`updateMemberRole`、`removeMember` | 必须保留至少一位所有者。 | 先按权限指定另一所有者，不循环删除 |
| `INVITATION_INVALID` | 409 / 异步 | Tenant邀请校验；`acceptInvitation`、`revokeInvitation` | 此邀请已失效或不适用于当前账户。 | 登录受邀Gateway身份或请求管理员重新邀请 |
| `TENANT_INACTIVE` | 403 / 异步 | Tenant业务准入；`createPackage`、`createBuild`、`createWorkspace` | 此客户空间已停用，当前不能进行业务操作。 | 保留允许的状态查询；platform_admin对suspended可reenable，对deleted仅窗口内restore；不要求先删资源 |
| `TENANT_RESTORE_EXPIRED` | 409 / 异步 | Tenant恢复期限；`restoreTenant` | 恢复期限已结束，不能恢复此客户空间。 | 只读托管和删除证据；不能自动重购资源 |
| `GATEWAY_UNAVAILABLE` | 503 / 异步 | Gateway Integration权威访问；`login`、`getWallet`、`listUsage`、`createGatewayKey`、`createWorkspace` | 账户或钱包服务暂时无法读取，请稍后核对。 | 读取可重试；已受理资金操作只GET原Operation |
| `OWNER_CAPABILITY_UNAVAILABLE` | 503 / 异步 | 跨Owner所需能力缺失；`createTenant`、`bindTenantWallet`、`createWorkspace`、`revealGatewayKey` | 当前安装尚未提供此操作所需的授权能力。 | 明确阻止；平台管理员核对Owner契约，不模拟成功 |
| `WALLET_BINDING_REQUIRED` | 409 / 异步 | Tenant账单主体准入；`createQuote`、`createWorkspace`、`bindTenantWallet` | 尚未绑定可用的账单账户，请联系管理员。 | 平台管理员通过授权绑定流程处理；不填自报余额 |
| `INSUFFICIENT_BALANCE` | 422 / 异步 | Gateway余额与报价核验；`createQuote`、`createWorkspace`、`renewWorkspace`、`resizeWorkspace` | 可用余额不足，请联系管理员充值后重新检查。 | 仅在真实Quote/Wallet可读时显示所需和可用金额；无自助付款按钮 |
| `QUOTE_EXPIRED` | 409 / 异步 | Workspace报价准入；`createWorkspace`、`renewWorkspace`、`resizeWorkspace` | 报价已过期，请重新检查费用并确认。 | 新createQuote，不复用过期快照 |
| `QUOTE_MISMATCH` | 409 / 异步 | Workspace报价绑定校验；`createWorkspace`、`renewWorkspace`、`resizeWorkspace` | 当前选择与报价不一致，请重新获取报价。 | 核对对象、套餐、版本与用途；禁止客户端改金额 |
| `POLICY_UNCONFIGURED` | 503 / 异步 | 目录/安装政策准入；`createQuote`、`createComputePlan`、`createStoragePlan` | 所需政策或原已接受价格证据尚不完整，暂不能执行。 | 由对应Owner补齐/核实原证据；不补0、不用新目录价格覆盖原价，不内置默认保留期。 |
| `CAPACITY_UNAVAILABLE` | 422 / 异步 | Fabric资源准入；`createQuote`、`createWorkspace`、`resizeWorkspace` | 当前资源容量不足，无法按所选套餐执行。 | 重新读取可用套餐；不切provider或重复采购 |
| `PROVIDER_CAPABILITY_UNSUPPORTED` | 422 / 异步 | Fabric能力约束；`createUploadPart`、`createQuote`、`createWorkspace`、`resizeWorkspace` | 当前资源提供方式不支持所需能力。 | 选择明确支持的批准选项；不忽略限制 |
| `RUNTIME_REVOKED` | 422 / 异步 | Runtime批准目录；`createBuild`、`createQuote`、`updateWorkspaceVersion` | 所需运行底座已停止准入，请选择可用版本。 | 重新读批准目录；不使用任意镜像绕过 |
| `WEBUI_INCOMPATIBLE` | 422 / 异步 | Build界面/Runtime兼容；`createBuild`、`createQuote` | 所选界面与运行底座不兼容，请重新选择。 | 读取兼容的已批准WebUI |
| `MODEL_NOT_ALLOWED` | 422 / 异步 | Gateway模型准入；`createQuote`、`updateWorkspaceModels`、`createGatewayKey` | 所选模型不在当前允许范围内。 | 读取模型目录并修改选择 |
| `PACKAGE_ARCHIVED` | 409 / 异步 | Capability包生命周期；`createUpload`、`createBuild`、`createQuote` | 此智能体已归档，不能进行该操作。 | 查看保留历史或选择活动包；不隐式恢复 |
| `UPLOAD_EXPIRED` | 409 / 异步 | Capability上传会话；`createUploadPart`、`completeUpload` | 此上传会话已失效，请重新开始上传。 | 区分会话过期与part签名过期；签名过期仅重取同part授权 |
| `UPLOAD_PART_MISMATCH` | 409 / 异步 | Capability/Storage分片完整性；`createUploadPart`、`completeUpload` | 上传分片信息不匹配，请重新核对已确认分片。 | getUpload读回partNumber/etag/size/sha256，只补确切缺失片 |
| `UPLOAD_CHECKSUM_MISMATCH` | 422 / 异步 | Capability对象校验；`completeUpload`、`getPackageVersion` | 文件校验未通过，请使用原始文件重新上传。 | 保留被拒版本/操作证据；不把传输完成视为uploaded |
| `PACKAGE_INVALID` | 422 / 异步 | Package格式权威校验；`completeUpload`、`createBuild` | 智能体包不符合支持的格式，请按校验提示修正。 | 只展示安全校验项，不猜manifest文件名或格式要求 |
| `BUILD_INPUT_REJECTED` | 422 / 异步 | Build不可变输入准入；`createBuild`、`retryBuild` | 构建输入未通过校验，请修正包或已批准的界面选择。 | 新上传或新构建请求；不改原冻结输入 |
| `BUILD_FAILED` | 异步 | Build执行结果；`getBuild`、`retryBuild`、`getOperation` | 构建未成功，请查看已脱敏日志和失败阶段。 | 同输入不允许重试；修正Package后上传新版本再构建，不覆盖原BuildJob |
| `ARTIFACT_REFERENCED` | 409 / 异步 | Capability引用保护；`deleteCapabilityVersion` | 此版本仍有活跃引用，暂不能下架目录版本。 | 读取referenceCount与Owner引用保护；不物理清理OCI字节、不级联删除Workspace |
| `ARTIFACT_UNAVAILABLE` | 503 / 异步 | Build/Capability制品读回；`createBuild`、`getCapabilityVersion`、`createWorkspace` | 所需制品暂时无法读取，尚不能确认可用。 | 按原不可变身份核对；不改tag或构造另一镜像；构建场景仅在BuildJob.retryAllowed=true时显式retryBuild生成新job |
| `INCOMPATIBLE_VERSION` | 422 / 异步 | Workspace版本兼容准入；`updateWorkspaceVersion`、`adoptWorkspace` | 目标版本与当前环境或数据不兼容。 | 选择明确兼容版本；不复制旧字段伪装兼容 |
| `DATA_MIGRATION_REQUIRED` | 422 / 异步 | Workspace数据迁移前置；`updateWorkspaceVersion`、`adoptWorkspace` | 此更新需要已验证的数据迁移，当前不能直接执行。 | 按发布者迁移规格完成独立授权验证；不就地覆盖数据 |
| `ROLLBACK_UNSAFE` | 409 / 异步 | Workspace回滚准入；`rollbackWorkspace` | 当前数据不能安全回到所选版本。 | 保留当前事实并联系管理员，不强制回滚 |
| `WORKSPACE_NOT_READY` | 409 / 异步 | Workspace生命周期准入；`getWorkspaceAccess`、`updateWorkspaceModels`、`resizeWorkspace`、`adoptWorkspace` | 工作区尚不具备执行此操作的条件。 | 读取Workspace与当前Operation，不盲重发 |
| `WORKSPACE_EXPIRED` | 409 / 异步 | Workspace付费权益准入；`getWorkspaceAccess`、`updateWorkspaceVersion` | 当前服务周期已结束，请核对续费和资源状态。 | 有效报价续费；已删除资源不能通过续费复活 |
| `OPERATION_IN_PROGRESS` | 409 / 异步 | Workspace/Owner并发保留；`updateWorkspaceVersion`、`resizeWorkspace`、`deleteWorkspace` | 已有操作正在处理，请先查看其结果。 | GET原Operation，不能创建竞争操作 |
| `EXTERNAL_OUTCOME_UNKNOWN` | 异步 | 资金/资源Owner权威读回；`getOperation`、`listWorkspaceTransactions`、`reconcileOperation` | 外部处理结果正在核实，请勿重复提交。 | 只核对原动作身份；不扣第二次、不反向退款 |
| `RESOURCE_DELETE_UNCONFIRMED` | 异步 | Fabric删除读回；`getWorkspaceDeletion`、`getOperation`、`reconcileOperation` | 资源删除结果尚未确认，正在核对。 | 保留needs_attention与证据；不得先退款或说数据已销毁 |
| `REFUND_PENDING` | 异步 | Gateway退款读回；`getWorkspaceDeletion`、`listWorkspaceTransactions`、`getOperation` | 退款尚未确认，请查看原退款记录。 | 只跟踪原refundOperationId；不承诺到账时间或再发同款 |
| `KEY_REVEAL_FORBIDDEN` | 403 / 异步 | Gateway Key权限/类型；`revealGatewayKey`、`revokeGatewayKey` | 此密钥不允许通过当前入口查看或操作。 | 保留指纹；系统托管Key走所属工作区流程 |
| `APP_ACCESS_UNAVAILABLE` | 409 / 异步 | Runtime访问准入/readback；`getWorkspaceAccess` | 当前应用暂时无法打开，请查看部署与运行状态。 | 读取选中Deployment和资源/应用两类状态；不拼接历史URL |
| `RATE_LIMITED` | 429 / 异步 | BFF/Owner请求准入；`getOperation`、`listBuildLogs`、`createUploadPart` | 请求过于频繁，请按服务提示稍后重试。 | 严格遵守响应Retry-After秒值；一次只在途一个GET；不自选退避、不转WebSocket |
| `DEPENDENCY_UNAVAILABLE` | 503 / 异步 | 跨Owner依赖读取；`getWorkspace`、`getOperation`、`listQualifications` | 所需服务暂时不可用，当前结果无法确认。 | 读取按503 Retry-After重试；已受理命令保持原身份；构建仅retryAllowed=true时可显式重试新job |
| `INSTANCE_AUTHORIZATION_REQUIRED` | 403 / 异步 | Instance高后果边界；`reconcileOperation`、`registerRuntimeVersion` | 此动作需要受保护的实例授权，不能在此页面直接执行。 | 进入已有授权流程；UI不能代替生产批准 |
| `INTERNAL_ERROR` | 500 / 异步 | Owner内部失败安全投影；`getOperation`、`createWorkspace`、`createBuild` | 操作未完成，请提供请求编号以便核对。 | 保留requestId；未知外部动作先核对，不能猜自动重试 |
| `PUBLISHER_NAMESPACE_MISMATCH` | 422 / 异步 | PublisherNamespace归属/路径段准入；`createPublisherNamespace`、`registerRuntimeVersion`、`registerWebuiVersion` | 发布者空间与制品仓库不匹配，请选择批准的独立空间。 | 重新读取空间/Registry范围；不得放宽前缀或跨Tenant共享私有制品 |
| `PUBLISHER_CONTRACT_INVALID` | 422 / 异步 | Publisher契约schema与兼容校验；`registerRuntimeVersion`、`registerWebuiVersion`、`createBuild` | 发布描述未通过校验，请修改标出的技术字段。 | 按正式schema修正完整契约，不猜端口/路径/探针，不忽略未知属性 |
| `REFERENCE_CLAIM_INVALID` | 409 / 异步 | Capability跨Owner引用保护；`createBuild`、`deleteCapabilityVersion`、`createWorkspace` | 所需版本引用未被确认，当前操作不能继续。 | Owner核对原claim身份和释放证据，浏览器不合成引用或强删 |
| `AUTHORIZATION_CONTEXT_EXPIRED` | 403 / 异步 | CloudIdentity授权上下文期限；`createBuild`、`createWorkspace`、`renewWorkspace` | 本次权限上下文已过期，请重新核对权限。 | 重新读取当前权限与原Operation；已受理义务由Owner裁决，不换key重扣 |
| `AUTHORIZATION_REVOKED` | 403 / 异步 | CloudIdentity当前授权/同意读回；`createWorkspace`、`renewWorkspace`、`updateRenewalSettings` | 该操作的授权已被撤销，不能继续新的受保护动作。 | 展示当前授权结果；原已确认/unknown支付仍只核对，不盲目退款 |
| `AUTHORIZATION_AUDIENCE_MISMATCH` | 403 / 异步 | 跨服务受众授权校验；`createWorkspace`、`getOperation` | 服务权限上下文不匹配，请提供请求编号核对。 | 管理员修复Owner调用配置；客户不能绕过权限或自报授权 |
| `ROUTE_GENERATION_CONFLICT` | 409 / 异步 | Fabric路由CAS；`updateWorkspaceVersion`、`rollbackWorkspace`、`getOperation` | 当前部署已变化，旧切换请求未被采用。 | 重新读取当前Deployment与原Operation，不覆盖新路由 |
| `STALE_EXECUTION_EPOCH` | 409 / 异步 | Owner执行epoch fence；`getOperation`、`reconcileOperation` | 旧执行响应已被拒绝，请查看当前操作进度。 | 只采用当前epoch的Owner读回；不让浏览器重放旧provider写入 |
| `TENANT_NOT_SUSPENDED` | 409 / 异步 | Tenant重新启用前置；`reenableTenant`、`getAdminTenant` | 此客户空间不是停用状态，不能执行重新启用。 | 重新读取Tenant；deleted只能走窗口内restore，不把两种恢复混用 |
| `RENEWAL_PERIOD_ELAPSED` | 422 / 异步 | 原paidThrough续费窗口；`createQuote`、`renewWorkspace` | 原周期的本次续费窗口已结束，不能从恢复当天重新获得完整新月。 | 显示Quote.periodStart/periodEnd及剩余可用时间；明确新建或由原Owner处理，不改周期、不补买、不误报余额不足 |
| `PLAN_CHANGE_EXISTS` | 409 / 异步 | Workspace唯一未完成计划；`createQuote`、`resizeWorkspace` | 已有套餐变更计划，请先查看原计划。 | 只有原计划允许取消时显式cancel后再new；不覆盖旧目标 |
| `PLAN_CHANGE_NOT_CANCELLABLE` | 409 / 异步 | 计划取消窗口与CAS；`cancelPlanChange` | 该计划的下期付款或执行已开始，不能简单取消。 | 读取原计划/原单；不删记录、不退本期、不撤回未知付款 |
| `PLAN_CHANGE_QUOTE_STALE` | 409 / 异步 | 原报价/计划版本前置；`createQuote`、`resizeWorkspace` | 原报价与当前计划或有效期不再匹配，请重新报价。 | 清除旧确认，读取当前来源；已接受原单不随时间重算 |
| `PLAN_TRANSITION_NOT_SUPPORTED` | 422 / 异步 | 批准transition/provider能力；`createQuote`、`resizeWorkspace` | 目标不在批准的可比较转换范围内。 | 选择明确批准的目标，不按价格或SKU名称猜测、不换provider |
| `PLAN_TRANSITION_MIXED` | 422 / 异步 | 资源维度方向校验；`createQuote`、`resizeWorkspace` | 本次同时包含升配和降配，当前不支持混合变更。 | 改选明确单向且批准的转换，不自动拆成两次收费 |
| `PLAN_CHANGE_NO_OP` | 422 / 异步 | 相同配置准入；`createQuote`、`resizeWorkspace` | 目标与当前配置相同，不创建收费操作。 | 保持当前计划，选择确实不同且批准的配置 |
| `STORAGE_SHRINK_UNSUPPORTED` | 422 / 异步 | 原盘缩容能力；`createQuote`、`resizeWorkspace` | 当前存储不支持缩容，下周期降配也不能绕过。 | 保留原盘或选择支持的目标；不能只降价却伪造缩容 |
| `SUBSCRIPTION_VERSION_CONFLICT` | 409 / 异步 | Subscription业务CAS；`createQuote`、`resizeWorkspace`、`renewWorkspace` | 当前周期或已生效价格已变化，请重新读取。 | 用当前已接受/成功生效价重新报价，不用目录新价覆盖原价 |
| `PLAN_CHANGE_PAYMENT_REQUIRED` | 409 / 异步 | 下一期目标付款授权；`getPlanChange`、`renewWorkspace` | 降配已预约，但下一账期还需要明确付款授权。 | 显示awaiting_payment与目标价；停止未付款使用，不自动扣旧高价 |
| `PLAN_CHANGE_PERIOD_ELAPSED` | 422 / 异步 | 当前已付周期窗口；`createQuote`、`resizeWorkspace` | 原已付周期已结束，不能再按中途升级处理。 | 使用原续费/明确新建规则，不让resize重置一个月 |
| `REFUND_ORIGINAL_CHARGE_CONFLICT` | 409 / 异步 | 原单退款余额与资格；`getPlanChange`、`listWorkspaceTransactions` | 退款原单或可退金额不一致，需要核对。 | 只核对原补差/目标周期款及失败/删除证据，unknown不发第二退款 |
| `FUTURE_PERIOD_COMMITTED` | 409 / 异步 | 非本计划的未来已接受账单；`createQuote`、`resizeWorkspace` | 下一期账单已锁定，请进入该周期后再调整。 | 不重价、不退旧款；本scheduled计划绑定的提前付款按其原版本继续 |
| `SCHEDULED_PLAN_APPLICATION_CONFLICT` | 409 / 异步 | 拟应用与scheduled目标兼容；`updateWorkspaceVersion`、`updateWorkspaceModels`、`adoptWorkspace`、`rollbackWorkspace` | 拟应用或模型不兼容下期目标资源，请先处理预约计划。 | 先取消仍可取消的计划再改应用；兼容变更允许，不设全局执行锁 |

## 4. 不允许的自动处理

不把旧报价带入新选择、不用0替代不可读取余额、不用最新policy覆盖原义务、不从日志猜retryAllowed、不通过清理客户资源解除引用、不将关闭自动续费变成取消已发出的原单、不将Tenant active当所有应用已恢复。不提供“强制成功”或“换key重试”按钮。

D17按workspace-plan-change-v1已确认。scheduled只代表计划保存；资源与Runtime确认后才applied。delivery失败、不可逆资源差异与原单补偿分别呈现，不因退款确认就假缩容。基础单720小时政策不用于升级补差。

## 5. 阶段中文词汇（唯一人工展示Owner）

合法kind/stage取03。计划保存、边界执行、取消分别读取对应Operation；scheduled不等于applied。

| stage | 中文 |
|---|---|
| `absence_verification` | 确认资源已不存在 |
| `access_enablement` | 恢复成员业务访问 |
| `access_revocation` | 撤销业务访问 |
| `activation` | 确认选中部署与权益 |
| `actual_resource_evidence` | 记录实际资源及不可逆差异 |
| `admission` | 检查执行条件 |
| `asset_custody` | 确认保留资产托管 |
| `attachment` | 关联计算与存储 |
| `attachment_deletion` | 解除资源关联 |
| `building` | 构建智能体 |
| `cancellation_guard` | 校验取消窗口和版本 |
| `catalog_tombstone` | 下架目录版本 |
| `compatibility` | 检查数据与版本兼容 |
| `compute` | 处理计算资源 |
| `compute_deletion` | 删除计算资源 |
| `configuration` | 写入目标配置 |
| `consent_commit` | 记录续费设置与同意 |
| `consent_validation` | 验证独立续费同意 |
| `coverage_calculation` | 按原单覆盖区间核算 |
| `debit` | 确认扣费 |
| `deletion_evidence` | 核对已确认删除证据 |
| `failure_fence` | 阻止失败后的迟到执行 |
| `grant_revocation` | 撤销未来自动扣费授权 |
| `identity_verification` | 核对个人身份 |
| `key` | 配置所需密钥 |
| `key_revocation` | 撤销密钥 |
| `membership` | 恢复或建立成员授权 |
| `obligation_check` | 核对现有业务义务 |
| `original_action_readback` | 核对原资金和资源动作 |
| `owner_switch` | 切换唯一写入Owner |
| `payment_authorization` | 检查下期付款授权 |
| `period_boundary` | 核对原账期边界 |
| `period_update` | 更新已确认服务周期 |
| `plan_change_commit` | 提交实际生效的套餐 |
| `plan_commit` | 保存业务计划事实 |
| `provider_renewal` | 确认资源续期 |
| `provider_resume` | 恢复资源运行 |
| `provider_suspend` | 停止资源运行 |
| `pushing` | 推送构建制品 |
| `queued` | 等待构建执行 |
| `quote_binding` | 绑定确切原报价 |
| `readback` | 核对原操作结果 |
| `receipt` | 记录并核对业务证据 |
| `reconciliation` | 核对迁移结果 |
| `reference_check` | 核对版本与资源引用 |
| `refund` | 核对退款结果 |
| `refund_original_charge` | 核对原单补偿退款 |
| `registering` | 登记可部署版本 |
| `reload` | 等待配置重新加载 |
| `resource_preflight` | 确认资源与调整能力 |
| `restore_window_check` | 检查恢复期限 |
| `retirement` | 退役被替换的运行实例 |
| `runtime` | 部署并启动应用 |
| `runtime_deletion` | 删除应用运行实例 |
| `schedule_cancel` | 取消未执行的下期计划 |
| `schedule_commit` | 保存下期计划 |
| `secret_unbinding` | 解除注入Secret绑定 |
| `snapshot_import` | 导入真实原义务快照 |
| `source_verification` | 验证原Owner来源 |
| `storage` | 处理存储资源 |
| `storage_deletion` | 删除存储资源 |
| `succeeded` | 本次操作已完成 |
| `supplement_payment` | 核对升级补差原单 |
| `target_period_payment` | 核对目标下一期原单 |
| `tenant_state_check` | 检查Tenant当前状态 |
| `upload_verification` | 校验完整上传对象 |
| `validating` | 校验构建输入 |
| `verification` | 核对实际运行结果 |
| `wallet_binding` | 核对账单主体绑定 |
| `workspace_deletion` | 跟踪各工作区删除 |
| `workspace_resumption` | 跟踪原停用工作区恢复 |
| `workspace_suspension` | 跟踪各工作区停用 |
| `write_barrier` | 确认旧写入屏障 |
