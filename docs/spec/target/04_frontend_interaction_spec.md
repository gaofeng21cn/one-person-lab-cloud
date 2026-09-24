# 04 前端功能与交互交接

> 这是前端工程规格，UI布局/可点击原型见11。接口/DTO唯一权威为03；完整字段、输入约束、来源与协议消费在`contracts/ui_inventory.json`。本文件不再复制长int64正则，也不把全部技术DTO塞进客户页面。

## 1. 框架、导航与真实来源

生产沿用React/TypeScript与现有Console API adapter。主导航为概览、智能体、工作区、OPL Gateway、费用；构建记录归智能体，Key/用量归Gateway，个人/成员/分组归设置。平台管理员独立区不能由Tenant owner自动取得。概览只聚合现有API，不新增服务。

下列为目标Console路由；当前router的源代码迁移仍由原Owner完成。原型hash只是离线预览。新UI不直达Fabric/Gateway/Build；唯一传输例外是Capability签署的Storage分片URL，只发送原始字节、绝不带Console凭据。

身份、角色、价格、provider和执行结果取Owner DTO；内部ID不因存在于API就成为普通客户主任务。金额为JSON十进制字符串，BigInt定点显示；所有完整金额/周期可读，精确范围直接引用03 USDMicros/NonnegativeInt64。时间UTC RFC3339按本地时区显示。

## 2. 共用交互契约

| 情形 | 前端必须做 | 不得做 |
|---|---|---|
| 表单未提交 | 仅保存非敏感意图草稿；明确未提交 | 保存密码、Key、签名URL或危险确认 |
| 报价选择/版本变化 | 丢弃旧quote及勾选同意，重新检查后重新确认 | 复用旧价格、默认补差、静默同意 |
| 写响应丢失 | 同key同规范化body取回原身份 | 换key重做扣费/采购/退款 |
| 202已接受 | 绑定owner+operationId，展示已受理 | 视为部署成功、已删除或已退款 |
| 非终态Operation | 严格按pollAfterSeconds/Retry-After相同秒值查询，一次一个GET | 固定间隔、自选指数兜底、转WebSocket |
| 终态succeeded/failed/cancelled | 停轮询，重新读取各业务事实 | 把一个操作终态覆盖独立资源/钱包结果 |
| 429/503 | 遵守明确Retry-After；保留原命令身份 | 高频轮询或创建第二命令 |
| 刷新/返回 | GET原任务/Operation；create与adopt草稿隔离 | 重新发出购买/删除或取消后端任务 |
| 部分读取失败 | 只标不可用区域，保留其他已验证来源 | 0余额、空列表、假ready替代失败 |
| 权限变化 | 服务端重验，清旧身份缓存与内存Secret | 依靠隐藏按钮作为唯一授权 |
| 目录下架 | 明确tombstone，不再可新部署 | 宣称物理OCI字节已清理 |
| Tenant恢复 | reenable原停用与restore已删除分开；逐项子操作 | Tenant active即全部应用可用、复活已删资源 |

needs_attention/awaiting_confirmation仍是非终态并服从Owner轮询提示；外部unknown不是failed。D17按13的已批准规则；原型示例金额不是实际Catalog定价。没有3秒自动跳转或固定完成时间。

## 3. 逐功能页面与控件

字段唯一来源为03，完整机读映射在ui_inventory。F11按已批准的13政策，只展示Catalog返回的计费结果。

### F01 登录、Tenant与成员权限

页面：`/login` 登录；`/console/settings/account` 个人账户；`/console/settings/members` 成员；`/admin/tenants/new` 开通Tenant
原型：`#login`、`#settings`、`#admin-tenants`

| 交接面 | 确定行为 |
|---|---|
| 布局/优先级 | 登录页单列；登录后的个人身份在顶部账户菜单，成员表在设置/成员。主区先显示当前活动Tenant与个人角色，内部身份ID只在管理诊断层。 |
| 控件/标注 | 用户名、密码输入；显式登录/退出；成员角色选择、邀请状态、独立撤销邀请按钮。普通成员只读，最后owner的移除/降级由服务端拒绝。 |
| 步骤/业务链 | getLoginContext后提交同源登录；成功先getSession再进入原安全站内目标。无Tenant的受邀身份只显示接受邀请。邀请、角色变更和移除各自确认对象，不共享钱包密码。 |
| 返回/草稿/刷新 | 密码/CSRF不进入持久草稿。登录失败保留用户名、清除密码；会话失效保留安全returnTo，不自动重发业务写入。退出清理身份缓存与单次Key。 |
| 失败/权限/不可用 | 会话读取失败独立显示；成员列表失败不退出有效会话。跨Tenant对象404；失去平台权限不能靠旧页面提交。邀请失效给出重新邀请而非自动注册。 |
| 窄屏/焦点 | 登录控件单列；成员卡先身份/角色/状态，低频动作进更多菜单。账户菜单和移动导航互斥，关闭回到打开按钮。 |

数据来源：`getLoginContext`、`login`、`getSession`、`logout`、`getTenant`、`listMembers`、`inviteMember`、`acceptInvitation`、`revokeInvitation`、`updateMemberRole`、`removeMember`、`listInvitations`、`bindTenantWallet`、`getAdminTenant`、`getOperation`、`createTenant`

| 动作 | API / 请求schema | 输入与条件 |
|---|---|---|
| 登录 | `login` / `LoginRequest` | `LoginRequest.username`、`LoginRequest.password`；权限 anonymous；CSRF上下文有效且必填输入完整 |
| 退出登录 | `logout` | 无body输入；对象和前置取当前Owner读回；权限 authenticated；存在当前会话 |
| 邀请成员 | `inviteMember` / `InviteMemberRequest` | `InviteMemberRequest.inviteeGatewaySubjectId`、`InviteMemberRequest.role`；权限 admin/owner；当前角色允许管理成员；不能授予高于自己授权的角色 |
| 接受邀请 | `acceptInvitation` | 无body输入；对象和前置取当前Owner读回；权限 invitee；会话身份与邀请接收人匹配 |
| 撤销邀请 | `revokeInvitation` | 无body输入；对象和前置取当前Owner读回；权限 admin/owner；邀请未生效且允许管理 |
| 修改角色 | `updateMemberRole` / `UpdateMemberRoleRequest` | `UpdateMemberRoleRequest.role`；权限 owner；服务端权限允许且不会移除最后owner |
| 移除成员 | `removeMember` | 无body输入；对象和前置取当前Owner读回；权限 owner；二次确认；服务端保护最后owner |
| 开通Tenant | `createTenant` / `CreateTenantRequest` | `CreateTenantRequest.name`、`CreateTenantRequest.billingSub2apiUserId`、`CreateTenantRequest.ownerGatewaySubjectId`；权限 platform_admin；核对已有Gateway owner身份与账单主体；跨域受理后跟踪Operation |
| 绑定账单主体 | `bindTenantWallet` / `BindTenantWalletRequest` | `BindTenantWalletRequest.billingSub2apiUserId`、`BindTenantWalletRequest.expectedBindingVersion`；权限 platform_admin；核对expectedBindingVersion及Gateway委托权限，不允许自报余额 |

成功：自己的Gateway身份成功登录，个人身份与Tenant钱包主体清楚分开；成员进入被授权的活动Tenant。

失败：登录失败显示统一身份校验失败，不透露账户是否存在；Tenant不可访问时显示原因与允许的管理读回，不能沿用旧权限。

重复/越权/重启按机读本F的具体场景验证。

### F02 分组与官方/私有Agent可见性

页面：`/console/agents` 智能体目录；`/console/settings/groups` 分组
原型：`#agents`、`#settings`

| 交接面 | 确定行为 |
|---|---|
| 布局/优先级 | 智能体目录主区卡片展示名称/官方或本Tenant私有/可部署版本/状态/用途。顶部搜索与范围筛选；构建记录为本页子导航。分组编辑移到设置/分组，不把领域名做主菜单。 |
| 控件/标注 | 可见范围select、搜索、分组select、上传主按钮；卡片详情和部署。默认分组只展示标记，不提供归档。官方准入动作仅平台区出现。 |
| 步骤/业务链 | 改变范围/分组/搜索重置cursor；卡片部署携带确切CapabilityVersion进入向导。无ready版本时引导查看上传/Build记录，不编造一个可部署状态。 |
| 返回/草稿/刷新 | 筛选可在当前URL或会话保存，切换Tenant清除。元数据编辑草稿不含Secret，提交前确认当前资源版本与权限。 |
| 失败/权限/不可用 | 空目录给浏览官方/上传入口；无搜索结果给清除筛选。源读取失败不是0个结果。归档包仍能按权限查看历史，已有部署不随卡片隐藏消失。 |
| 窄屏/焦点 | 结果先于低频筛选；卡片单列，主操作与次操作在同一底栏。筛选展开后保留焦点，收起返回筛选按钮。 |

数据来源：`listNamespaces`、`createNamespace`、`updateNamespace`、`archiveNamespace`、`listPackages`、`createPackage`、`updatePackage`、`getPackage`、`publishOfficialPackage`

| 动作 | API / 请求schema | 输入与条件 |
|---|---|---|
| 创建分组 | `createNamespace` / `NamespaceWriteRequest` | `NamespaceWriteRequest.name`；权限 admin/owner；Tenant活动且具备管理权限 |
| 编辑分组 | `updateNamespace` / `NamespaceWriteRequest` | `NamespaceWriteRequest.name`；权限 admin/owner；可管理且未归档 |
| 归档分组 | `archiveNamespace` | 无body输入；对象和前置取当前Owner读回；权限 admin/owner；二次确认；不删除包或历史 |
| 创建智能体 | `createPackage` / `CreatePackageRequest` | `CreatePackageRequest.namespaceId`、`CreatePackageRequest.name`、`CreatePackageRequest.description`；权限 admin/owner；所选分组可写 |
| 编辑智能体信息 | `updatePackage` / `UpdatePackageRequest` | `UpdatePackageRequest.namespaceId`、`UpdatePackageRequest.name`、`UpdatePackageRequest.description`；权限 admin/owner；私有包可写且active |
| 发布为官方智能体 | `publishOfficialPackage` / `PublishPackageRequest` | `PublishPackageRequest.admissionReceiptId`；权限 platform_admin；已有admissionReceiptId并满足官方可见性准入；不是公共Marketplace |

成功：客户可在官方与自己Tenant的私有Agent中选择，默认分组稳定可用。

失败：无数据显示“还没有智能体”；读取失败显示“无法读取智能体，请重试”，不伪造空列表。

重复/越权/重启按机读本F的具体场景验证。

### F03 管理员Runtime/WebUI与资源价格目录

页面：`/admin/catalog/runtime` 运行底座；`/admin/catalog/webui` WebUI；`/admin/catalog/publishers` 发布者空间；`/admin/catalog/build-policy` 新构建默认策略；`/admin/catalog/plans` 资源与价格政策
原型：`#admin-catalog`、`#admin-pricing`

| 交接面 | 确定行为 |
|---|---|
| 布局/优先级 | 平台/运行底座与界面含Runtime、WebUI、发布者空间三个tab；默认构建策略是Runtime页独立卡。平台/资源与价格政策按计算、存储、价格、退款、保留分区。 |
| 控件/标注 | 选择PublisherNamespace、不可变完整镜像引用、发布契约结构化字段和可展开完整JSON；显示schema校验错误。第三方必须独立third_party前缀。资源计划先规格，组合价格再选择两侧plan。 |
| 步骤/业务链 | 先创建/核对发布者空间，准入版本，再选择默认构建策略；弃用/撤销需展示在用影响且不自动更新实例。默认策略带expectedPolicyVersionId，只影响新Build。中途变更使用已批准workspace-plan-change-v1，不接受自定义公式；实际资源价格仍来自批准目录。 |
| 返回/草稿/刷新 | 技术契约可保存当前标签页的非敏感草稿；离开前说明未提交。只保存引用不存Registry凭据。恢复后重读发布者状态/政策版本，失效选择需重新准入。 |
| 失败/权限/不可用 | 缺发布者权限/仓库前缀不匹配给字段错误；目录已撤销禁止新选择。组合未定价只显示规格，“选择完整套餐后报价”，不显示最低价或0。 |
| 窄屏/焦点 | 技术参数分组折叠，错误摘要链接具体字段；JSON编辑区横向仅自身滚动，不撑开页面。危险动作独立确认，焦点先放标题/影响。 |

数据来源：`listRuntimeVersions`、`listWebuiVersions`、`listComputePlans`、`listStoragePlans`、`listModels`、`publishOfficialPackage`、`registerRuntimeVersion`、`setRuntimeVersionStatus`、`registerWebuiVersion`、`setWebuiVersionStatus`、`createComputePlan`、`setComputePlanAvailability`、`createStoragePlan`、`setStoragePlanAvailability`、`listPricePolicyVersions`、`createPricePolicyVersion`、`listRefundPolicyVersions`、`createRefundPolicyVersion`、`listRetentionPolicyVersions`、`createRetentionPolicyVersion`、`setBuildRuntimePolicy`、`getBuildRuntimePolicy`、`createPublisherNamespace`、`revokePublisherNamespace`、`listPublisherNamespaces`

| 动作 | API / 请求schema | 输入与条件 |
|---|---|---|
| 准入运行底座 | `registerRuntimeVersion` / `RegisterRuntimeVersionRequest` | `RegisterRuntimeVersionRequest.name`、`RegisterRuntimeVersionRequest.versionLabel`、`RegisterRuntimeVersionRequest.publisherNamespaceId`、`RegisterRuntimeVersionRequest.publisherContract`、`RegisterRuntimeVersionRequest.admissionReceiptId`；权限 platform_admin；先选approved PublisherNamespace，提交对应完整publisherContract与准入receipt；Owner计算契约digest，客户不自报；结构化/JSON编辑共用同一schema |
| 弃用/撤销运行底座 | `setRuntimeVersionStatus` / `CatalogStatusRequest` | `CatalogStatusRequest.status`、`CatalogStatusRequest.reason`；权限 platform_admin；明确状态与reason；不自动更新现有Workspace |
| 准入界面 | `registerWebuiVersion` / `RegisterWebuiVersionRequest` | `RegisterWebuiVersionRequest.name`、`RegisterWebuiVersionRequest.versionLabel`、`RegisterWebuiVersionRequest.publisherNamespaceId`、`RegisterWebuiVersionRequest.publisherContract`、`RegisterWebuiVersionRequest.admissionReceiptId`；权限 platform_admin；先选approved PublisherNamespace，提交对应完整publisherContract与准入receipt；Owner计算契约digest，客户不自报；结构化/JSON编辑共用同一schema |
| 弃用/撤销界面 | `setWebuiVersionStatus` / `CatalogStatusRequest` | `CatalogStatusRequest.status`、`CatalogStatusRequest.reason`；权限 platform_admin；确认已有使用影响；不隐式改版本 |
| 新增计算套餐 | `createComputePlan` / `CreateComputePlanRequest` | `CreateComputePlanRequest.name`、`CreateComputePlanRequest.vcpus`、`CreateComputePlanRequest.memoryMiB`、`CreateComputePlanRequest.providerProfileId`、`CreateComputePlanRequest.providerSkuId`、`CreateComputePlanRequest.providerCapabilityVersion`、`CreateComputePlanRequest.validFrom`、`CreateComputePlanRequest.validUntil`；权限 platform_admin；先创建provider profile/SKU与规格；随后按exact计算+存储组合新建价格策略，未定价不得报价 |
| 设置计算可用性 | `setComputePlanAvailability` / `PlanAvailabilityRequest` | `PlanAvailabilityRequest.availability`、`PlanAvailabilityRequest.reason`；权限 platform_admin；显示原因，不能改历史订阅义务 |
| 新增存储套餐 | `createStoragePlan` / `CreateStoragePlanRequest` | `CreateStoragePlanRequest.name`、`CreateStoragePlanRequest.capacityGiB`、`CreateStoragePlanRequest.providerProfileId`、`CreateStoragePlanRequest.providerSkuId`、`CreateStoragePlanRequest.shrinkSupported`、`CreateStoragePlanRequest.validFrom`、`CreateStoragePlanRequest.validUntil`；权限 platform_admin；先声明容量和shrinkSupported再创建组合价格；不从旧价格猜新套餐 |
| 设置存储可用性 | `setStoragePlanAvailability` / `PlanAvailabilityRequest` | `PlanAvailabilityRequest.availability`、`PlanAvailabilityRequest.reason`；权限 platform_admin；只更新目录准入，不删除在用资源 |
| 新增价格版本 | `createPricePolicyVersion` / `CreatePricePolicyRequest` | `CreatePricePolicyRequest.versionLabel`、`CreatePricePolicyRequest.periodMonths`、`CreatePricePolicyRequest.computeMonthlyUSDMicros`、`CreatePricePolicyRequest.storageMonthlyUSDMicros`、`CreatePricePolicyRequest.productMonthlyUSDMicros`、`CreatePricePolicyRequest.validFrom`、`CreatePricePolicyRequest.validUntil`、`CreatePricePolicyRequest.computePlanId`、`CreatePricePolicyRequest.storagePlanId`、`CreatePricePolicyRequest.renewalPolicy`、`CreatePricePolicyRequest.planChangePolicyVersion`；权限 platform_admin；绑定确切computePlanId+storagePlanId；整数金额/周期/有效期合法且无重叠，只追加版本 |
| 新增退款政策版本 | `createRefundPolicyVersion` / `CreateRefundPolicyRequest` | `CreateRefundPolicyRequest.versionLabel`、`CreateRefundPolicyRequest.algorithm`、`CreateRefundPolicyRequest.retentionPolicyVersionId`、`CreateRefundPolicyRequest.customerTerms`、`CreateRefundPolicyRequest.validFrom`、`CreateRefundPolicyRequest.validUntil`；权限 platform_admin；算法与保留策略绑定，有明确customerTerms和有效期 |
| 新增保留政策版本 | `createRetentionPolicyVersion` / `CreateRetentionPolicyRequest` | `CreateRetentionPolicyRequest.versionLabel`、`CreateRetentionPolicyRequest.customerTerms`；权限 platform_admin；规则由批准契约固定；不在表单放任意保留天数 |
| 设定新构建的默认底座与界面 | `setBuildRuntimePolicy` / `SetBuildRuntimePolicyRequest` | `SetBuildRuntimePolicyRequest.runtimeVersionId`、`SetBuildRuntimePolicyRequest.defaultWebuiVersionId`、`SetBuildRuntimePolicyRequest.expectedPolicyVersionId`；权限 platform_admin；只选approved兼容Runtime/WebUI；已有策略必须匹配expectedPolicyVersionId，新策略只影响新Build，不改历史输入或已有Workspace |
| 新增发布者空间 | `createPublisherNamespace` / `CreatePublisherNamespaceRequest` | `CreatePublisherNamespaceRequest.name`、`CreatePublisherNamespaceRequest.kind`、`CreatePublisherNamespaceRequest.registryId`、`CreatePublisherNamespaceRequest.repositoryPrefix`、`CreatePublisherNamespaceRequest.admissionReceiptId`；权限 platform_admin；批准Registry、完整仓库前缀、official/third_party类型及准入receipt；按路径段校验，不使用模糊前缀 |
| 撤销发布者空间准入 | `revokePublisherNamespace` / `RevokePublisherNamespaceRequest` | `RevokePublisherNamespaceRequest.reason`；权限 platform_admin；核对在用影响并说明reason；阻止新准入，不静默删已有镜像/资源 |

成功：客户目录呈现已批准且兼容的版本和套餐，管理员可读回批准记录及不可变标识。

失败：目录缺失/失效时明确不可选择，不内置默认runtime、provider或价格。

重复/越权/重启按机读本F的具体场景验证。

### F04 上传Package与后续版本、确认构建

页面：`/console/agents/upload` 上传向导；`/console/agents/:packageId/upload` 新包版本
原型：`#upload`

| 交接面 | 确定行为 |
|---|---|
| 布局/优先级 | 上传向导三步：选择包与版本→上传并验证→确认构建。右侧摘要区显示目标包/版本/已确认字节与所选WebUI；不混入部署报价。 |
| 控件/标注 | 文件选择/拖放、版本输入、已有Package和分组选择、上传暂停/继续、批准WebUI选择。文件扩展/大小限制服从owner政策，UI不自设500MB。 |
| 步骤/业务链 | createUpload→逐part获取签名授权→仅字节直传Storage→getUpload确认分片→completeUpload取得Operation→getPackageVersion确认uploaded→用户显式createBuild。上传100%不等于verified；没有3秒自动跳转。 |
| 返回/草稿/刷新 | 保留非敏感uploadId、packageVersionId、已选文件元信息；签名URL/授权头不保存。刷新要求用户重新选同一原文件，按原part身份与摘要核对；只补缺失片，不新建成功版本。 |
| 失败/权限/不可用 | 网络中断不猜分片存在；先读回。签名过期可重取原part授权，session过期按owner继续原未完成身份的协议。校验失败不可覆盖已确认内容。 |
| 窄屏/焦点 | 文件区与表单单列；长文件名换行，传输/验证分别有标签。步间标题获得焦点，错误聚焦字段；返回不丢已确认上传身份。 |

数据来源：`listNamespaces`、`getPackage`、`listWebuiVersions`、`createUpload`、`getUpload`、`completeUpload`、`listPackageVersions`、`getPackageVersion`、`createBuild`、`createUploadPart`、`getOperation`、`createPackage`

| 动作 | API / 请求schema | 输入与条件 |
|---|---|---|
| 开始上传 | `createUpload` / `CreateUploadRequest` | `CreateUploadRequest.versionLabel`、`CreateUploadRequest.fileName`、`CreateUploadRequest.sizeBytes`、`CreateUploadRequest.sha256`；权限 admin/owner；Package可写、版本未重复且文件元信息合法 |
| 获取分片上传授权 | `createUploadPart` / `CreateUploadPartRequest` | `CreateUploadPartRequest.partNumber`、`CreateUploadPartRequest.sizeBytes`、`CreateUploadPartRequest.sha256`；权限 admin/owner；partNumber/sizeBytes/sha256与会话限制匹配；使用返回method/contentType/校验头直传，不附加BFF cookie或token |
| 验证上传 | `completeUpload` / `CompleteUploadRequest` | `CompleteUploadRequest.parts`；权限 admin/owner；全部分片已确认；提交parts后202只表示验证已受理，GET Operation成功且PackageVersion uploaded才通过 |
| 确认构建 | `createBuild` / `CreateBuildRequest` | `CreateBuildRequest.packageVersionId`、`CreateBuildRequest.webuiVersionId`；权限 admin/owner；PackageVersion uploaded且WebUI选择有效 |

成功：先看到“上传已验证”，确认构建后看到独立任务ID和“查看构建进度”按钮。

失败：摘要/包校验失败保留具体字段错误；修正文件需新上传会话。上传成功但构建创建失败保留已上传版本，可重新提交原构建意图。

重复/越权/重启按机读本F的具体场景验证。

### F05 构建进度、日志与失败重试

页面：`/console/agents/builds` 构建记录；`/console/agents/builds/:buildJobId` 构建详情
原型：`#build`、`#agents`

| 交接面 | 确定行为 |
|---|---|
| 布局/优先级 | 智能体/构建记录列任务、输入版本、当前状态、创建时间；详情左侧阶段，右侧固定输入；日志默认收起，状态与下一步高于技术日志。 |
| 控件/标注 | 查看原Operation、展开/分页日志；只有BuildJob.retryAllowed=true才出现重试。构建成功后显示部署按钮，不自动导航。 |
| 步骤/业务链 | getBuild定位Operation；非终态只getOperation轮询；终态再读Build和CapabilityVersion。pushing完成不等于ready登记完成。重试新job关联原job，输入错误应上传新版本。 |
| 返回/草稿/刷新 | 只保存job/operation身份，不保存日志或Secret；刷新继续原记录。离页停止页面GET但不停止后端构建。 |
| 失败/权限/不可用 | 日志读取失败不判任务失败；needs_attention显示原外部结果核对。failed明确可修正的包/契约原因，不承诺退配额。 |
| 窄屏/焦点 | 阶段纵向排列；日志内部滚动，收起时焦点回日志按钮。成功入口与失败修正入口保持在状态标题附近。 |

数据来源：`listBuilds`、`getBuild`、`listBuildLogs`、`retryBuild`、`getOperation`、`getCapabilityVersion`、`createBuild`

| 动作 | API / 请求schema | 输入与条件 |
|---|---|---|
| 查看日志 | `listBuildLogs` | 无body输入；对象和前置取当前Owner读回；权限 member；有任务读权限；按cursor按需读取 |
| 重试构建 | `retryBuild` | 无body输入；对象和前置取当前Owner读回；权限 admin/owner；BuildJob.retryAllowed=true才展示；Owner确认failed且允许错误类别及外部副作用已核清，retryBuild创建新job关联原任务 |

成功：构建及Capability登记均确认后显示“版本已可部署”；用户主动进入部署向导。

失败：failed按错误码显示可修正原因；needs_attention显示“构建结果需要处理”，不能重新覆盖原job。日志读取失败不把任务判失败。

重复/越权/重启按机读本F的具体场景验证。

### F06 智能体详情、版本下架与引用保护

页面：`/console/agents/:packageId` 智能体详情
原型：`#agent-detail`

| 交接面 | 确定行为 |
|---|---|
| 布局/优先级 | 智能体详情先用途、来源与当前可部署版本，再版本表和数据兼容说明。目录维护放次级管理区，普通客户不默认看digest/内部Receipt。 |
| 控件/标注 | 选择明确版本、看构建来源、部署；归档Package与下架CapabilityVersion分别命名/确认。下架只目录墓碑，不用“删除制品字节”文案。 |
| 步骤/业务链 | 查看版本status/referenceCount/provenance；build来源可追Build，legacy_application明确“历史应用导入”。活跃claim阻止下架；下架后历史和原部署身份保留。 |
| 返回/草稿/刷新 | 没有隐式版本选择草稿。页面重读后再次确认目标id与引用事实；重复下架返回原Operation。 |
| 失败/权限/不可用 | ARTIFACT_REFERENCED展示引用保护，不建议通过删客户环境绕过。已下架不能重新部署；物理purge只在独立批准工作流，不在普通客户按钮。 |
| 窄屏/焦点 | 版本行转卡片，当前版本与来源先呈现；破坏性按钮移至独立区，不与部署并排同权重。 |

数据来源：`getPackage`、`listPackageVersions`、`getPackageVersion`、`listCapabilityVersions`、`getCapabilityVersion`、`archivePackage`、`deleteCapabilityVersion`、`getOperation`、`listPackages`

| 动作 | API / 请求schema | 输入与条件 |
|---|---|---|
| 归档智能体 | `archivePackage` | 无body输入；对象和前置取当前Owner读回；权限 admin/owner；有写权限；确认仅归档，不级联 |
| 下架目录版本 | `deleteCapabilityVersion` | 无body输入；对象和前置取当前Owner读回；权限 admin/owner；明确目录下架范围；Capability引用保护通过，受理后只跟踪原Operation；不执行物理Registry删除 |

成功：目录版本变为deleted墓碑并不可被新部署选择；原Build/Package/部署证据保留；没有宣称OCI字节已物理清除。

失败：ARTIFACT_REFERENCED阻止下架；不绕过引用检查、不通过删客户资源作为前端补救。

重复/越权/重启按机读本F的具体场景验证。

### F07 部署选择、准入与报价

页面：`/console/workspaces/new` 部署选择与报价
原型：`#deploy`

| 交接面 | 确定行为 |
|---|---|
| 布局/优先级 | 部署向导四步：Agent→套餐/模型/续费方式→确切报价与条款→操作结果。桌面右侧当前选择摘要，窄屏摘要在步骤内容之后、主确认之前。 |
| 控件/标注 | 已批准版本radio，计算radio卡，存储和模型select，名称输入；renewalMode默认manual但提交必填。automatic需要独立未勾选的明确同意。 |
| 步骤/业务链 | 步骤1/2只编辑意图；进入3获取新Quote。变更版本/套餐/模型/周期/续费设置、返回编辑或报价过期，都废弃旧quote和确认checkbox。重新报价后必须重新同意，不自动提交。新报价周期固定1个月。 |
| 返回/草稿/刷新 | 按Tenant+用户+create/adopt分开保留非敏感选择；不保存密码、Key、签名URL。恢复草稿重新读取目录/余额/报价；旧quote不直接恢复为有效。 |
| 失败/权限/不可用 | 目录/余额/政策读取失败禁止付款确认但保留选择；余额不足展示权威required/available，没有自助支付API就提示联系管理员。价格与规则按批准Catalog真实报价；示例金额不作为生产价格。 |
| 窄屏/焦点 | 每步单列，标题聚焦；radio整卡可点。关键费用/周期/退款保留条款不能折叠隐藏；确认按钮不被固定底栏遮挡。 |

数据来源：`listCapabilityVersions`、`getCapabilityVersion`、`listComputePlans`、`listStoragePlans`、`listModels`、`getWallet`、`createQuote`、`getQuote`、`listRuntimeVersions`、`listWebuiVersions`

| 动作 | API / 请求schema | 输入与条件 |
|---|---|---|
| 检查并获取报价 | `createQuote` / `QuoteRequest` | `QuoteRequest.purpose`、`QuoteRequest.workspaceId`、`QuoteRequest.capabilityVersionId`、`QuoteRequest.computePlanId`、`QuoteRequest.storagePlanId`、`QuoteRequest.modelSelections`、`QuoteRequest.periodMonths`、`QuoteRequest.scheduledPlanChangeId`；权限 admin/owner；所选版本/套餐/模型完整且有效 |
| 重新读取报价 | `getQuote` | 无body输入；对象和前置取当前Owner读回；权限 admin/owner；保留现有quoteId核对有效性，不重写快照 |

成功：客户知道将部署的确切版本、承担的费用周期与数据政策，后续提交引用已接受报价。

失败：余额不足显示本次报价与权威余额，提示“请联系管理员充值后重新检查”；无自助支付接口不显示“立即付款”。

重复/越权/重启按机读本F的具体场景验证。

### F08 创建Workspace及部署结果

页面：`/console/operations/:owner/:operationId` 部署进度
原型：`#deploy`

| 交接面 | 确定行为 |
|---|---|
| 布局/优先级 | 结果页先操作是否受理/真实当前stage/下一步；下方分别展示扣费、资源、应用和Receipt事实，不能合成一个running灯。 |
| 控件/标注 | 查看原操作、返回列表、在明确成功后打开当前环境；不提供“重新创建”来绕过unknown。示例原型的阶段推进按钮不进入生产组件。 |
| 步骤/业务链 | createWorkspace202后绑定owner+operationId；轮询严格按Operation.pollAfterSeconds和Retry-After，成功后读Workspace/Deployment/账务。应用和凭据验证及所需证据都确认后可打开。 |
| 返回/草稿/刷新 | 接受后草稿只持久保存原Operation定位，不再次发送创建；响应丢失保留原key+同body取回原身份。新建与历史adopt的草稿及操作分开，防止混用购买流程。 |
| 失败/权限/不可用 | unknown/needs_attention明确等待核实，不自动补偿或退款。resources ready但app unavailable可同时出现。Receipt写入未确认只恢复那一写入。 |
| 窄屏/焦点 | 当前阶段和允许动作先于全部历史阶段；长ID按需披露/复制，不撑宽页面。离开、返回都回到同一操作。 |

数据来源：`createWorkspace`、`getOperation`、`getWorkspace`、`getDeployment`、`listWorkspaceTransactions`

| 动作 | API / 请求schema | 输入与条件 |
|---|---|---|
| 确认部署 | `createWorkspace` / `CreateWorkspaceRequest` | `CreateWorkspaceRequest.name`、`CreateWorkspaceRequest.quoteId`、`CreateWorkspaceRequest.renewalMode`、`CreateWorkspaceRequest.automaticRenewalConsent`；权限 admin/owner；有效quoteId、完整用户配置、显式确认价格/数据政策 |
| 查看处理进度 | `getOperation` | 无body输入；对象和前置取当前Owner读回；权限 member；原owner+operationId；不重发createWorkspace |

成功：扣费和资源/运行/Secret注入/证据都取得所需确认，客户主动点击打开。

失败：unknown显示“结果正在核实，请勿重复提交”；needs_attention显示需要管理员处理。失败页独立展示是否已扣费、资源是否已清理、退款是否确认，不说“正在自动清理并退款”除非独立操作确有记录。

重复/越权/重启按机读本F的具体场景验证。

### F09 Workspace查询、打开与模型配置

页面：`/console/workspaces` 工作区列表；`/console/workspaces/:workspaceId` 工作区详情；`/console/workspaces/:workspaceId/models` 模型配置
原型：`#workspaces`、`#workspace`

| 交接面 | 确定行为 |
|---|---|
| 布局/优先级 | 工作区列表是名称、资源/应用两态、完整周期、打开/详情。详情内概览、模型、部署历史、账单周期、危险操作分tab；主操作Open优先。 |
| 控件/标注 | 搜索/cursor分页、打开、模型select+保存；模型配置同时显示目标version与appliedVersion。只有当前授权访问结果提供URL，客户端不拼地址。 应用登录信息按钮只给owner，且必须当前应用支持并实际ready；普通成员用应用自己的账户登录。 |
| 步骤/业务链 | getWorkspaceAccess重新准入后打开，应用保留自己的登录。更新模型带expectedVersion，Operation及reload读回完成前不显示已生效。 |
| 返回/草稿/刷新 | 列表筛选可保存；模型未提交选择离页提示并可丢弃。已提交用原Operation恢复，不重新保存；切换Tenant清除旧配置草稿。 应用username/password只留在显式单次对话框，关闭立即清除DOM与内存。 |
| 失败/权限/不可用 | 部分源失败单独禁用对应动作，不隐藏有效资源事实。expired/suspended不同于deleted；没有选中应用的旧资源显示待部署。 拟模型/应用若不兼容scheduled目标资源，变更前返回SCHEDULED_PLAN_APPLICATION_CONFLICT，先取消可取消计划再改；兼容变更不因scheduled而一律锁死。 |
| 窄屏/焦点 | 列表转结果优先卡；打开按钮先于筛选。模型错误内联；多tab可横向局部滚动，不出现整页横向滚动。 |

数据来源：`listWorkspaces`、`getWorkspace`、`getWorkspaceAccess`、`listModels`、`updateWorkspaceModels`、`getOperation`、`getDeployment`、`getWorkspaceModels`、`adoptWorkspace`、`revealWorkspaceApplicationCredentials`

| 动作 | API / 请求schema | 输入与条件 |
|---|---|---|
| 打开工作区 | `getWorkspaceAccess` | 无body输入；对象和前置取当前Owner读回；权限 member；当前授权与应用访问政策允许，实时读回可用 |
| 更新模型配置 | `updateWorkspaceModels` / `UpdateWorkspaceModelsRequest` | `UpdateWorkspaceModelsRequest.expectedVersion`、`UpdateWorkspaceModelsRequest.selections`；权限 admin/owner；模型在允许目录且Workspace生命周期允许更新 |
| 显示应用登录信息 | `revealWorkspaceApplicationCredentials` | 无body输入；对象和前置取当前Owner读回；权限 owner；仅Workspace owner且WorkspaceAccess.applicationCredentialsAvailable=true；单次private/no-store显示username/password；关闭/离页清除，不返回session_secret或Gateway Key |

成功：打开的是当前选中部署，不是最初Launch的历史Runtime；模型配置可确认实际加载。

失败：运行不可用时不展示可点击“已运行”；访问拒绝有具体文案。配置保存但reload未确认显示分阶段结果。

重复/越权/重启按机读本F的具体场景验证。

### F10 Agent更新、Runtime重建与回滚

页面：`/console/workspaces/:workspaceId/update` 版本更新；`/console/workspaces/:workspaceId/deployments` 部署历史
原型：`#workspace`

| 交接面 | 确定行为 |
|---|---|
| 布局/优先级 | 从工作区详情进入更新表单：当前版本、目标版本、数据/可用性影响、确认。部署历史保留前驱关系，回滚入口挂在可恢复的历史部署上。 |
| 控件/标注 | 批准Capability版本select、同版本重建选项（依API支持）、兼容性说明；expectedActiveDeploymentId自动取当前读回不让客户填写。 |
| 步骤/业务链 | 先检查目标兼容/资源/数据迁移；缺验证证据拒绝。明确确认后独立Deployment；只在路由/运行读回一致后显示切换。回滚是单独命令与确认，不是改DB指针。 已有scheduled目标时先验证拟版本/回滚与目标资源兼容；冲突在应用变更前拒绝，不能等下期扣款后才失败。 |
| 返回/草稿/刷新 | 目标选择可临时保留；重开必须重读当前选中与版本。已接受操作恢复其原id；前驱变更使旧确认失效。 |
| 失败/权限/不可用 | 不安全回滚显示ROLLBACK_UNSAFE并阻止动作；旧active指针保留不等于旧应用仍可访问。不能承诺自动回滚一定成功。 |
| 窄屏/焦点 | 当前/目标并排卡转上下比较；影响与数据保护始终靠近确认，不把高风险说明藏在tooltip。 |

数据来源：`listCapabilityVersions`、`getCapabilityVersion`、`listRuntimeVersions`、`listDeployments`、`getDeployment`、`updateWorkspaceVersion`、`rollbackWorkspace`、`getOperation`、`getWorkspace`

| 动作 | API / 请求schema | 输入与条件 |
|---|---|---|
| 更新版本或重建 | `updateWorkspaceVersion` / `UpdateWorkspaceVersionRequest` | `UpdateWorkspaceVersionRequest.capabilityVersionId`、`UpdateWorkspaceVersionRequest.expectedActiveDeploymentId`；权限 admin/owner；目标已批准且兼容；无冲突操作；用户显式确认 |
| 回滚到所选部署 | `rollbackWorkspace` / `RollbackWorkspaceRequest` | `RollbackWorkspaceRequest.targetDeploymentId`、`RollbackWorkspaceRequest.expectedActiveDeploymentId`；权限 admin/owner；目标可恢复且数据兼容；再次确认影响 |

成功：目标运行验证和选中绑定切换确认后显示新版本；回滚读回确认后才显示“已恢复到所选版本”。

失败：不兼容更新明确拒绝，不通过保留旧字段/复制数据目录硬凑兼容；回滚失败显示需要处理，不承诺自动恢复。

重复/越权/重启按机读本F的具体场景验证。

### F11 立即升级补差与下期降配计划

页面：`/console/workspaces/:workspaceId?tab=plan-changes` 套餐变更列表与当前生效计划；`/console/workspaces/:workspaceId?tab=plan-changes&planChangeId=:planChangeId` PlanChange详情与执行/取消结果
原型：`#workspace`

| 交接面 | 确定行为 |
|---|---|
| 布局/优先级 | 复用工作区详情“套餐变更”tab。列表先当前业务套餐/实际资源/原周期，再未完成计划与历史。报价确认独立卡先原/目标规格、Pold/Pnew、T、S/E、剩余毫秒、补差/下期价和执行中断；不把新旧事实混成一个数字。 |
| 控件/标注 | 选择批准的可比较目标，客户端不按价格或SKU名称猜方向。upgrade_immediate立即处理，downgrade_next_period只预约E。Quote确认checkbox明确金额/影响；有计划先显式cancel再new。取消要求reason与expectedScheduleVersion，不靠前端按钮猜窗口。 调整报价所需当前modelSelections从getWorkspaceModels读取，不填默认空数组；periodMonths按本期接口固定1，应用身份沿当前Workspace。 付款确认保留精确到最多6位小数、至少2位；19354839微美元显示US$19.354839而非19.35。只展示Quote返回的金额/剩余毫秒，不从JavaScript Date重算收费。 |
| 步骤/业务链 | createQuote purpose=resize→resizeWorkspace只提交quoteId→Operation.resourceId定位PlanChange→getPlanChange/listPlanChanges。升级原单补差confirmed且资源/挂载/Runtime读回全部通过才applied；S/E不变。降配保存scheduled，初次Operation可succeeded但只是保存；E边界用独立executionOperationId，不复活原终态任务。 |
| 返回/草稿/刷新 | 草稿保留非敏感目标，不保存可复用的旧报价确认。报价过期或sourceSubscriptionVersion/原计划变化重新报价；接受后固定T/价格/原单，刷新只读同PlanChange，不因时间推进重算补差。scheduled无执行任务时按页面重开/聚焦/明确刷新读取计划；发现executionOperationId才按其Operation Retry-After查询。 |
| 失败/权限/不可用 | requested、scheduled、awaiting_payment、applying、applied、failed、needs_attention、cancelled分别显示。scheduled不写已降配，资源单独confirmed不写已升级。deliveryOutcome=failed且resourceOutcome=irreversible_residual时可按fenced失败证据退原补差，但仍展示资源差异/阻止后续变更，不假缩容。unknown不重扣、不新购、不盲退款。 legacy原价证据不足时显示POLICY_UNCONFIGURED并保留只读访问，不补0、不取当前目录新价作为Pold。 |
| 窄屏/焦点 | 当前/目标、原E和应付金额在确认前完整可见，窄屏双列转单列；取消原因错误聚焦textarea。计划历史转卡；模拟边界/推进按钮只在原型fixture区，生产页面没有手改状态控件。 |

数据来源：`getWorkspace`、`listComputePlans`、`listStoragePlans`、`createQuote`、`getQuote`、`resizeWorkspace`、`getOperation`、`listWorkspaceTransactions`、`listPlanChanges`、`getPlanChange`、`cancelPlanChange`、`getSubscription`、`updateRenewalSettings`、`getWorkspaceModels`

| 动作 | API / 请求schema | 输入与条件 |
|---|---|---|
| 计算调整报价 | `createQuote` / `QuoteRequest` | `QuoteRequest.purpose`、`QuoteRequest.workspaceId`、`QuoteRequest.capabilityVersionId`、`QuoteRequest.computePlanId`、`QuoteRequest.storagePlanId`、`QuoteRequest.modelSelections`、`QuoteRequest.periodMonths`、`QuoteRequest.scheduledPlanChangeId`；权限 admin/owner；报价用途为套餐调整且引用目标Workspace |
| 接受立即升级报价 / 保存下期降配计划 | `resizeWorkspace` / `ApplyQuoteRequest` | `ApplyQuoteRequest.quoteId`；权限 admin/owner；只提交quoteId；原计划与Subscription业务版本匹配。Operation.resourceId是PlanChange.id，受理不是applied；降配当期total=0只保存scheduled，不提前变更资源。 |
| 取消尚未执行的下期计划 | `cancelPlanChange` / `CancelPlanChangeRequest` | `CancelPlanChangeRequest.expectedScheduleVersion`、`CancelPlanChangeRequest.reason`；权限 admin/owner；仅PlanChange.cancellable=true；expectedScheduleVersion和非空reason必填；下期资金义务accepted或资金/provider动作已发出后不可简单取消。重复命中原取消Operation。 |

成功：固定示例S=2026-09-01T00:00Z、E=2026-10-01T00:00Z、T=09-16T00:00Z，20→40美元/月剩半期补10；资源+Runtime确认才applied，E不变。降配当期不变不退款，E后按目标下期价与有效授权执行。

失败：CBS缩容、混合/no-op/不支持转换明确拒绝。付款拒绝无资源动作；unknown原单不重发。确定失败按原补差全额补偿，实际不可逆资源差异独立needs_attention，不宣称原盘缩回。

重复/越权/重启按机读本F的具体场景验证。

### F12 续费、到期停用与恢复

页面：`/console/billing` 订阅与周期；`/console/workspaces/:workspaceId/billing` 续费设置与结果
原型：`#workspace`、`#billing`

| 交接面 | 确定行为 |
|---|---|
| 布局/优先级 | 费用/订阅与工作区账单tab均展示完整周期、续费模式、授权状态、原义务和续费结果；交易在单独tab。 |
| 控件/标注 | 手动续费、读取续费设置；automatic单独同意、撤销/改manual通过明确API而非本地开关。 |
| 步骤/业务链 | 续费同一周期唯一义务，人工点击与worker命中同一键。新周期1个月；旧legacy历史期数原样展示。余额不足不推进周期；provider续期未确认不显示新到期已生效。 Quote.periodStart固定原paidThrough，periodEnd为nextBillingMonth(paidThrough,billingAnchorDay)。确认页展示完整起止与实际剩余可用时间；now>=periodEnd返回RENEWAL_PERIOD_ELAPSED，不改成恢复后新整月。 存在scheduled降配时下一期报价绑定该计划目标价，手动/自动/边界worker共享唯一nextPeriodStart义务，不先扣旧价。提前付目标下一期也必须等原E才降资源；取消自动续费不删除计划，缺授权在E等待付款。 |
| 返回/草稿/刷新 | 充值不自动宣称恢复；刷新读取subscription与原续费operation。对自动续费授权变更确认后必须读回生效。 |
| 失败/权限/不可用 | 显示到期停止与资源实际存在性；删除后的资源不能续费复活。资金unknown只核对原单，不能再发一笔同周期扣费。 已超原续费窗口明确区别于余额不足；需要明确新建或Owner处理，不暗中补购。 |
| 窄屏/焦点 | 完整起止时间上下排版；当前模式与关闭自动续费动作在同区域，不藏在长条款底部。 |

数据来源：`getWorkspace`、`createQuote`、`getQuote`、`renewWorkspace`、`getOperation`、`listWorkspaceTransactions`、`getWallet`、`getSubscription`、`updateRenewalSettings`

| 动作 | API / 请求schema | 输入与条件 |
|---|---|---|
| 获取续费报价 | `createQuote` / `QuoteRequest` | `QuoteRequest.purpose`、`QuoteRequest.workspaceId`、`QuoteRequest.capabilityVersionId`、`QuoteRequest.computePlanId`、`QuoteRequest.storagePlanId`、`QuoteRequest.modelSelections`、`QuoteRequest.periodMonths`、`QuoteRequest.scheduledPlanChangeId`；权限 admin/owner；引用Workspace与目标续费义务 |
| 确认续费 | `renewWorkspace` / `ApplyQuoteRequest` | `ApplyQuoteRequest.quoteId`；权限 admin/owner；有效报价、当前周期和生命周期允许 |
| 设置后续续费方式 | `updateRenewalSettings` / `UpdateRenewalSettingsRequest` | `UpdateRenewalSettingsRequest.renewalMode`、`UpdateRenewalSettingsRequest.expectedRenewalSettingsVersion`、`UpdateRenewalSettingsRequest.automaticRenewalConsent`；权限 admin/owner；expectedRenewalSettingsVersion匹配；automatic需独立同意，manual不带consent字段；关闭只阻止未发出扣费的未来周期，不取消当前原单 |

成功：付款确认、周期推进和真实运行恢复分别显示；只有全链完成才恢复“可打开”。

失败：支付unknown只核对原交易；资源已删除时明确不能通过续费复原，不承诺备份或重新购买。

重复/越权/重启按机读本F的具体场景验证。

### F13 删除Workspace、资源确认与退款

页面：`/console/workspaces/:workspaceId/settings` 危险操作与删除结果
原型：`#workspace`、`#billing`

| 交接面 | 确定行为 |
|---|---|
| 布局/优先级 | 删除在独立危险区。确认先对象/运行资源/数据范围，再原退款保留条款，最后名称输入与销毁确认。结果页资源销毁和退款两块并列。 |
| 控件/标注 | confirmationName必须准确；acknowledgeDataDestruction未勾选不可提交。quoted链接原Quote；legacy_import显示原purchase与原条款，不造Quote。 |
| 步骤/业务链 | 先getSubscription核对原政策，deleteWorkspace受理后追getWorkspaceDeletion和独立wallet operation。只有absence和原单资格确认才进入允许退款，环境deleted不代表退款confirmed。 删除原子取消尚未执行的scheduled计划；已发出资金/资源动作先收敛，不一边resize一边destroy。基础单保留原720小时删除政策；成功升级补差按自身quoteT..E剩余覆盖向下取整，原单分别去重读回；升级确定失败补偿是原补差全退，不套720。 |
| 返回/草稿/刷新 | 不保存危险确认输入。刷新只恢复已受理Operation，不重复DELETE；任何未知结果保留原命令身份。 |
| 失败/权限/不可用 | 缺原条款证据显示待核实，不能默认0或100%退款。数据/资源unknown不说已销毁，退款unknown不说已到账。Package/Build历史保留。 |
| 窄屏/焦点 | 危险区与日常操作隔离；确认对话框可滚动但影响和提交始终可达。Escape关闭未提交确认并归还焦点，已受理任务不取消。 |

数据来源：`getWorkspace`、`getQuote`、`deleteWorkspace`、`getOperation`、`listWorkspaceTransactions`、`getSubscription`、`getWorkspaceDeletion`

| 动作 | API / 请求schema | 输入与条件 |
|---|---|---|
| 确认删除工作区 | `deleteWorkspace` / `DeleteWorkspaceRequest` | `DeleteWorkspaceRequest.confirmationName`、`DeleteWorkspaceRequest.acknowledgeDataDestruction`；权限 admin/owner；确认名称与数据销毁；Owner按Subscription真实来源和原不可变政策裁决，不接受客户退款金额；原政策缺证据先核实 |

成功：资源确认删除与退款confirmed分别取得证据；无退款权益时显示政策实际结果，而不是统一承诺退款或不退款。

失败：资源结果unknown时显示“资源删除结果待核实”，不宣称已销毁也不提前退款。退款unknown显示“退款结果待核实”，禁止再次点击触发同款。

重复/越权/重启按机读本F的具体场景验证。

### F14 钱包、用量、Key及管理员充值记录

页面：`/console/api` 服务与钱包；`/console/api/usage` 模型用量；`/console/api/keys` API密钥；`/admin/recharge-records` 充值记录
原型：`#gateway`

| 交接面 | 确定行为 |
|---|---|
| 布局/优先级 | Gateway内服务信息、用量、API Key子页；费用内订阅、交易；平台充值记录只读。Token使用和Workspace月费不能揉成同一个未说明金额。 |
| 控件/标注 | Key名称/允许模型/到期；创建、显式reveal、撤销独立动作，托管Key转工作区管理。用量按Key与周期筛选。 |
| 步骤/业务链 | 余额/用量读取来自Gateway，错误不当0。Key单次no-store响应在模态内存，关闭/离页/退出清除；列表只指纹。撤销需明确影响与Owner读回。 |
| 返回/草稿/刷新 | 不持久存任何Key值。丢失创建响应先取回原结果引用，再在Gateway允许时显式reveal，不能盲建第二Key。 |
| 失败/权限/不可用 | Key reveal能力不可用明确拒绝；托管Key不能通过普通入口操作。充值accepted不等于到账，真实充值不在本原型执行。 |
| 窄屏/焦点 | 先Key身份/状态/模型/限额，低频动作更多菜单；一次性Secret可换行和复制，关闭后DOM清除。 |

数据来源：`getWallet`、`listUsage`、`listGatewayKeys`、`createGatewayKey`、`revealGatewayKey`、`revokeGatewayKey`、`listWorkspaceTransactions`、`listRechargeRecords`

| 动作 | API / 请求schema | 输入与条件 |
|---|---|---|
| 创建API密钥 | `createGatewayKey` / `CreateGatewayKeyRequest` | `CreateGatewayKeyRequest.name`、`CreateGatewayKeyRequest.modelIds`、`CreateGatewayKeyRequest.expiresAt`；权限 member；通过Gateway委托权限，仅允许普通用户Key |
| 显示密钥 | `revealGatewayKey` | 无body输入；对象和前置取当前Owner读回；权限 member；显式用户动作及Gateway授权；内存单次展示 |
| 撤销密钥 | `revokeGatewayKey` | 无body输入；对象和前置取当前Owner读回；权限 member；二次确认且非系统托管Key |

成功：金额/用量来自权威读回；管理员充值列表仅在confirmed记录存在时显示已到账，不把记录读取当执行充值。

失败：上游不可用显示“余额暂时无法读取”，不显示0或缓存余额为实时；没有权限时不提供reveal。

重复/越权/重启按机读本F的具体场景验证。

### F15 Tenant停用、删除与窗口内恢复

页面：`/admin/tenants/:tenantId` Tenant访问/子操作/托管
原型：`#admin-tenants`

| 交接面 | 确定行为 |
|---|---|
| 布局/优先级 | 平台Tenant详情先访问状态，再关联Workspace子操作，最后资产托管/恢复期限。active、suspended、deleted显示不同动作，不共用一个恢复按钮。 |
| 控件/标注 | suspend、reenable、delete、restore四种命名与说明；getTenantLifecycleOperation展示accessStatus、workspaceActions和skipped原因。 |
| 步骤/业务链 | reenable恢复访问后，只Resume被原Tenant suspend暂停、仍付费且原资源确认存在的Workspace；不续费/重购，不影响其他停用原因。deleted恢复只在deletedAt+15天内恢复身份和保留资产权限，已删资源不复活。 |
| 返回/草稿/刷新 | 不保存危险确认；状态读取过期必须重新确认。Tenant active不覆盖尚未恢复的子应用状态，每个子operation独立查询。 |
| 失败/权限/不可用 | 子项过期/资源未知/其他停用原因列入skipped说明；删除子任务未完成不得先宣布删除完成。到期恢复按钮拒绝而不是重建Tenant。 |
| 窄屏/焦点 | 访问状态摘要在上，子操作逐卡列状态与原因；确认中不能混淆重新启用和删除恢复的按钮文案。 |

数据来源：`getTenant`、`suspendTenant`、`deleteTenant`、`restoreTenant`、`getOperation`、`listTenants`、`createTenant`、`getAdminTenant`、`bindTenantWallet`、`getTenantAssetCustody`、`reenableTenant`、`getTenantLifecycleOperation`

| 动作 | API / 请求schema | 输入与条件 |
|---|---|---|
| 停用Tenant | `suspendTenant` / `TenantActionRequest` | `TenantActionRequest.reason`；权限 platform_admin；危险操作确认及授权；成员访问立即失效 |
| 删除Tenant | `deleteTenant` / `DeleteTenantRequest` | `DeleteTenantRequest.confirmationName`、`DeleteTenantRequest.reason`；权限 platform_admin；确认影响与托管规则；依API权限服务端最终裁决 |
| 恢复Tenant | `restoreTenant` / `TenantActionRequest` | `TenantActionRequest.reason`；权限 platform_admin；当前时间严格早于restoreUntil；仅恢复权限而非资源 |
| 重新启用已停用Tenant | `reenableTenant` / `ReenableTenantRequest` | `ReenableTenantRequest.reason`；权限 platform_admin；只从suspended恢复访问；只恢复原Tenant停用导致、仍付费且原资源存在的Workspace，列出跳过项；不续费/重购 |

成功：reenable只恢复原Tenant暂停的合格子环境并独立显示结果；restore只恢复deleted窗口内身份/保留资产，不复活已删CVM/CBS。

失败：窗口结束明确不可恢复；部分Workspace仍待核对时显示独立任务，不把Tenant状态代替资源删除结果。

重复/越权/重启按机读本F的具体场景验证。

### F16 旧资源与应用迁移后的可见状态和采用Agent

页面：`/console/workspaces/:workspaceId` 旧环境详情；`/console/workspaces/:workspaceId/adopt` 在已有资源采用Agent
原型：`#adopt`

| 交接面 | 确定行为 |
|---|---|
| 布局/优先级 | 旧工作区仍在同一列表，详情明确“资源已开通，待部署”或“历史应用导入”。只增加adopt入口，不建立平行旧控制台。 |
| 控件/标注 | adopt选批准Agent和模型，资源/周期只读；expectedWorkspaceVersion由当前Workspace.version带入。 |
| 步骤/业务链 | legacy_resource_only采用Agent在现有资源，不createWorkspace、不quote/debit、不重购。imported_application保留原digest/数据/业务身份，不能伪造Build成功。 |
| 返回/草稿/刷新 | create和adopt草稿分开；刷新按原Workspace/operation恢复。历史数据不能用缺字段作为新建/重购理由。 |
| 失败/权限/不可用 | legacy无acceptedQuoteId是合法分支。缺原义务证据明确核实，不能合成客户同意或ready状态。 |
| 窄屏/焦点 | 旧路径来源说明紧邻主操作；“部署到已有资源”不同于“购买并部署”，窄屏也不能截短成同一按钮。 |

数据来源：`getWorkspace`、`getSubscription`、`listDeployments`、`getDeployment`、`getCapabilityVersion`、`listCapabilityVersions`、`adoptWorkspace`、`getOperation`、`listWorkspaceTransactions`、`listModels`

| 动作 | API / 请求schema | 输入与条件 |
|---|---|---|
| 在现有资源上部署智能体 | `adoptWorkspace` / `AdoptWorkspaceRequest` | `AdoptWorkspaceRequest.capabilityVersionId`、`AdoptWorkspaceRequest.expectedWorkspaceVersion`、`AdoptWorkspaceRequest.modelSelections`；权限 admin/owner；仅已迁移legacy_resource_only；现有已付资源权益与所选Agent/模型/数据兼容；AdoptWorkspaceRequest.expectedWorkspaceVersion匹配Workspace.version，不新报价、不扣费、不重购 |

成功：已有资源采用Agent后Serve产生唯一active Deployment并更新deliveryModel，历史账户/Workspace/资源/周期/交易/收据ID不变，无新增debit或资源订单。

失败：无法证明历史关联或资源状态时显示需要处理；不猜旧数据绑定、不合成ready Runtime或Build。

重复/越权/重启按机读本F的具体场景验证。

### F17 管理员操作、审计与实例资格读回

页面：`/admin/operations` 操作与审计；`/admin/qualifications` 资格与发布证据
原型：`#admin-operations`、`#admin-qualification`

| 交接面 | 确定行为 |
|---|---|
| 布局/优先级 | 平台操作与审计按owner/对象过滤；资格页并排源码、Candidate、Instance、Release四层证据，不用一个总绿灯。 |
| 控件/标注 | 复制安全requestId、读回原Operation、查看不可变receipt/制品身份；高后果动作引向已有Instance保护流程，不在BFF加执行器。 |
| 步骤/业务链 | reconcile仅核对/继续原义务，由Owner裁决，不能强制状态为succeeded。完整digest和源SHA需一致，正式发布提升同已合格字节。 |
| 返回/草稿/刷新 | 管理筛选可保存，敏感凭据/私网地址不进入URL或浏览器。刷新保留原owner+operationId，不创建新管理任务。 |
| 失败/权限/不可用 | 未取得某层证据就显示未取得，不能用源码测试代替Instance运行；未知资金/资源结果给独立读回。 |
| 窄屏/焦点 | 证据层卡片纵排；长digest/ID在技术披露区换行，不让普通客户信息被诊断字段挤出。 |

数据来源：`getOperation`、`listAdminOperations`、`reconcileOperation`、`listAuditEvents`、`listReceipts`、`getReceipt`、`listQualifications`

| 动作 | API / 请求schema | 输入与条件 |
|---|---|---|
| 核对原操作 | `reconcileOperation` / `ReconcileOperationRequest` | `ReconcileOperationRequest.reason`；权限 platform_admin；显式reason、原owner+operationId；只按Owner执行原身份读回，不成为新的扣费/采购授权 |

成功：能沿同一不可变Candidate追到资格、实际运行和发布证据；只读UI不会触发生产部署。

失败：证据缺失显示“未取得本层证据”，不能显示运行通过；needs_attention保留requestId与允许核对入口。

重复/越权/重启按机读本F的具体场景验证。

## 4. 输入组件与校验绑定

`features[].actions[].editorFields`逐字段声明control、required与schemaRef。枚举渲染为批准选项；boolean同意控件默认不勾选；array用重复行/多选；PublisherContract等object用结构化分组并允许同schema的JSON技术编辑；时间显示时区再转换；金额不能转Number。所有跨字段oneOf/必填条件由API schema统一约束，前端只提前给出同语义错误，不削弱服务端校验。

`displayFields/fieldSources`为完整响应命名字段集合，`protocolFields`单列CSRF、签名上传和分页控制，`inputFields/inputConstraints`保留完整请求绑定。它们不是要求客户页面显示每个技术字段。普通Receipt页不披露内部Ledger结构，平台技术详情按诊断目的展开。

## 5. 可访问性与完成证据

原型的桌面/窄屏布局和状态走查见11；实现必须继续进行真实React页面与Owner API集成验收。输入有label，错误可定位，状态aria-live；移动抽屉关闭时inert，dialog焦点可关闭/归还，长名称/金额/周期不产生整页横向滚动。只读查看不能触发高后果资源或资金动作。

静态字段/operationId验证、离线可点击原型和截图分别证明自己的层次，不能宣称生产已采用，也不替代待批准的商业规则。
