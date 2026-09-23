# 10 全量功能交付与字段追踪矩阵

> 自动派生索引，不是第二份产品或字段定义。更新03/02/04后运行 checks/render_traceability.py；随后运行 checks/validate_spec.py。
> 每个F编号贯穿客户场景、页面、后端operationId、DTO字段、持久化Owner与接受标准。

## 总表

| 功能 | 页面 | API操作数 | 数据Owner |
|---|---|---:|---|
| F01 登录、Tenant与成员权限 | /login、/console/settings/account、/console/settings/members、/admin/tenants/new | 16 | tenant, workspace |
| F02 分组与官方/私有Agent可见性 | /console/agents、/console/settings/groups | 9 | capability |
| F03 管理员Runtime/WebUI与资源价格目录 | /admin/catalog/runtime、/admin/catalog/webui、/admin/catalog/publishers、/admin/catalog/build-policy、/admin/catalog/plans | 25 | capability, gateway, resource_catalog |
| F04 上传Package与后续版本、确认构建 | /console/agents/upload、/console/agents/:packageId/upload | 12 | build, capability, workspace |
| F05 构建进度、日志与失败重试 | /console/agents/builds、/console/agents/builds/:buildJobId | 7 | build, capability, workspace |
| F06 智能体详情、版本下架与引用保护 | /console/agents/:packageId | 9 | capability, workspace |
| F07 部署选择、准入与报价 | /console/workspaces/new | 10 | capability, gateway, resource_catalog |
| F08 创建Workspace及部署结果 | /console/operations/:owner/:operationId | 5 | gateway, workspace |
| F09 Workspace查询、打开与模型配置 | /console/workspaces、/console/workspaces/:workspaceId、/console/workspaces/:workspaceId/models | 10 | gateway, workspace |
| F10 Agent更新、Runtime重建与回滚 | /console/workspaces/:workspaceId/update、/console/workspaces/:workspaceId/deployments | 9 | capability, workspace |
| F11 立即升级补差与下期降配计划 | /console/workspaces/:workspaceId?tab=plan-changes、/console/workspaces/:workspaceId?tab=plan-changes&planChangeId=:planChangeId | 14 | gateway, resource_catalog, workspace |
| F12 续费、到期停用与恢复 | /console/billing、/console/workspaces/:workspaceId/billing | 9 | gateway, resource_catalog, workspace |
| F13 删除Workspace、资源确认与退款 | /console/workspaces/:workspaceId/settings | 7 | gateway, resource_catalog, workspace |
| F14 钱包、用量、Key及管理员充值记录 | /console/api、/console/api/usage、/console/api/keys、/admin/recharge-records | 8 | gateway |
| F15 Tenant停用、删除与窗口内恢复 | /admin/tenants/:tenantId | 12 | tenant, workspace |
| F16 旧资源与应用迁移后的可见状态和采用Agent | /console/workspaces/:workspaceId、/console/workspaces/:workspaceId/adopt | 10 | capability, gateway, workspace |
| F17 管理员操作、审计与实例资格读回 | /admin/operations、/admin/qualifications | 7 | ledger, tenant, workspace |

## F01 登录、Tenant与成员权限

**页面**：`/login`（登录）、`/console/settings/account`（个人账户）、`/console/settings/members`（成员）、`/admin/tenants/new`（开通Tenant）

| operationId | 请求 | 请求DTO | 成功响应DTO | 唯一Owner |
|---|---|---|---|---|
| `acceptInvitation` | `POST /api/v2/invitations/{invitationId}/accept` | — | 200 Member | tenant |
| `bindTenantWallet` | `PUT /api/v2/admin/tenants/{tenantId}/wallet-binding` | BindTenantWalletRequest | 202 Operation | tenant |
| `createTenant` | `POST /api/v2/admin/tenants` | CreateTenantRequest | 202 Operation | tenant |
| `getAdminTenant` | `GET /api/v2/admin/tenants/{tenantId}` | — | 200 Tenant | tenant |
| `getLoginContext` | `GET /api/v2/auth/context` | — | 200 LoginContext | tenant |
| `getOperation` | `GET /api/v2/operations/{owner}/{operationId}` | — | 200 Operation | workspace |
| `getSession` | `GET /api/v2/auth/session` | — | 200 Session | tenant |
| `getTenant` | `GET /api/v2/tenant` | — | 200 Tenant | tenant |
| `inviteMember` | `POST /api/v2/tenant/invitations` | InviteMemberRequest | 201 Invitation | tenant |
| `listInvitations` | `GET /api/v2/tenant/invitations` | — | 200 InvitationPage | tenant |
| `listMembers` | `GET /api/v2/tenant/members` | — | 200 MemberPage | tenant |
| `login` | `POST /api/v2/auth/login` | LoginRequest | 200 Session | tenant |
| `logout` | `POST /api/v2/auth/logout` | — | 204 无响应体 | tenant |
| `removeMember` | `DELETE /api/v2/tenant/members/{memberId}` | — | 204 无响应体 | tenant |
| `revokeInvitation` | `POST /api/v2/tenant/invitations/{invitationId}/revoke` | — | 200 Invitation | tenant |
| `updateMemberRole` | `PUT /api/v2/tenant/members/{memberId}` | UpdateMemberRoleRequest | 200 Member | tenant |

**显示字段来源**（精确Schema.field，包含正常/处理中/失败页字段；可选性按03）：

- `Invitation`：`createdAt`、`expiresAt`、`id`、`inviteeGatewaySubjectId`、`role`、`status`
- `Member`：`actorId`、`createdAt`、`displayName`、`id`、`role`、`status`
- `Operation`：`createdAt`、`errorCode`、`kind`、`observationResult`、`operationId`、`owner`、`pollAfterSeconds`、`requestId`、`resourceId`、`stage`、`status`、`updatedAt`
- `Session`：`actorId`、`displayName`、`expiresAt`、`permissions`、`role`、`tenantId`、`tenantName`
- `Tenant`：`assetCustodyStatus`、`billingSub2apiUserId`、`createdAt`、`id`、`name`、`restoreUntil`、`status`、`updatedAt`

**数据表及写入Owner**：`build.idempotency_records`、`build.operations`、`build.outbox_events`、`capability.idempotency_records`、`capability.operations`、`capability.outbox_events`、`fabric.idempotency_records`、`fabric.operations`、`fabric.outbox_events`、`gateway.idempotency_records`、`gateway.identity_mappings`、`gateway.operations`、`gateway.outbox_events`、`gateway.tenant_wallet_bindings`、`ledger.idempotency_records`、`ledger.outbox_events`、`resource_catalog.idempotency_records`、`resource_catalog.operations`、`resource_catalog.outbox_events`、`runtime_control.idempotency_records`、`runtime_control.operations`、`runtime_control.outbox_events`、`tenant.audit_events`、`tenant.authorization_contexts`、`tenant.idempotency_records`、`tenant.invitations`、`tenant.operations`、`tenant.outbox_events`、`tenant.sessions`、`tenant.tenant_members`、`tenant.tenants`、`workspace.idempotency_records`、`workspace.operations`、`workspace.outbox_events`

**验收向量**：

- **success**：自己的Gateway身份成功登录，个人身份与Tenant钱包主体清楚分开；成员进入被授权的活动Tenant。
- **failure**：登录失败显示统一身份校验失败，不透露账户是否存在；Tenant不可访问时显示原因与允许的管理读回，不能沿用旧权限。
- **duplicate**：同一提交意图保留 Idempotency-Key；重发相同请求只显示原资源/操作，不新增副作用。修改输入产生新意图，不复用旧 key。
- **unauthorized**：缺少页面动作权限时不提供按钮；服务端仍独立授权。访问其他 Tenant 对象显示不存在，不暴露是否存在。
- **restart**：刷新、切页或登录恢复后从权威 GET 重建视图；异步命令以原 Operation 继续查询，页面不会重新发起命令。

跨域状态/恢复执行见06；输入约束、按钮条件和中文错误见04/07。涉及旧对象同时执行09迁移接受标准。

## F02 分组与官方/私有Agent可见性

**页面**：`/console/agents`（智能体目录）、`/console/settings/groups`（分组）

| operationId | 请求 | 请求DTO | 成功响应DTO | 唯一Owner |
|---|---|---|---|---|
| `archiveNamespace` | `POST /api/v2/namespaces/{namespaceId}/archive` | — | 200 Namespace | capability |
| `createNamespace` | `POST /api/v2/namespaces` | NamespaceWriteRequest | 201 Namespace | capability |
| `createPackage` | `POST /api/v2/packages` | CreatePackageRequest | 201 Package | capability |
| `getPackage` | `GET /api/v2/packages/{packageId}` | — | 200 Package | capability |
| `listNamespaces` | `GET /api/v2/namespaces` | — | 200 NamespacePage | capability |
| `listPackages` | `GET /api/v2/packages` | — | 200 PackagePage | capability |
| `publishOfficialPackage` | `POST /api/v2/admin/packages/{packageId}/publish` | PublishPackageRequest | 200 Package | capability |
| `updateNamespace` | `PUT /api/v2/namespaces/{namespaceId}` | NamespaceWriteRequest | 200 Namespace | capability |
| `updatePackage` | `PUT /api/v2/packages/{packageId}` | UpdatePackageRequest | 200 Package | capability |

**显示字段来源**（精确Schema.field，包含正常/处理中/失败页字段；可选性按03）：

- `Namespace`：`createdAt`、`id`、`isDefault`、`name`、`status`
- `Package`：`createdAt`、`description`、`id`、`latestReadyVersionId`、`name`、`namespaceId`、`status`、`updatedAt`、`visibility`

**数据表及写入Owner**：`capability.namespaces`、`capability.packages`

**验收向量**：

- **success**：客户可在官方与自己Tenant的私有Agent中选择，默认分组稳定可用。
- **failure**：无数据显示“还没有智能体”；读取失败显示“无法读取智能体，请重试”，不伪造空列表。
- **duplicate**：同一提交意图保留 Idempotency-Key；重发相同请求只显示原资源/操作，不新增副作用。修改输入产生新意图，不复用旧 key。
- **unauthorized**：缺少页面动作权限时不提供按钮；服务端仍独立授权。访问其他 Tenant 对象显示不存在，不暴露是否存在。
- **restart**：刷新、切页或登录恢复后从权威 GET 重建视图；异步命令以原 Operation 继续查询，页面不会重新发起命令。

跨域状态/恢复执行见06；输入约束、按钮条件和中文错误见04/07。涉及旧对象同时执行09迁移接受标准。

## F03 管理员Runtime/WebUI与资源价格目录

**页面**：`/admin/catalog/runtime`（运行底座）、`/admin/catalog/webui`（WebUI）、`/admin/catalog/publishers`（发布者空间）、`/admin/catalog/build-policy`（新构建默认策略）、`/admin/catalog/plans`（资源与价格政策）

| operationId | 请求 | 请求DTO | 成功响应DTO | 唯一Owner |
|---|---|---|---|---|
| `createComputePlan` | `POST /api/v2/admin/catalog/compute-plans` | CreateComputePlanRequest | 201 ComputePlan | resource_catalog |
| `createPricePolicyVersion` | `POST /api/v2/admin/catalog/price-policies` | CreatePricePolicyRequest | 201 PricePolicyVersion | resource_catalog |
| `createPublisherNamespace` | `POST /api/v2/admin/catalog/publisher-namespaces` | CreatePublisherNamespaceRequest | 201 PublisherNamespace | capability |
| `createRefundPolicyVersion` | `POST /api/v2/admin/catalog/refund-policies` | CreateRefundPolicyRequest | 201 RefundPolicyVersion | resource_catalog |
| `createRetentionPolicyVersion` | `POST /api/v2/admin/catalog/retention-policies` | CreateRetentionPolicyRequest | 201 RetentionPolicyVersion | resource_catalog |
| `createStoragePlan` | `POST /api/v2/admin/catalog/storage-plans` | CreateStoragePlanRequest | 201 StoragePlan | resource_catalog |
| `getBuildRuntimePolicy` | `GET /api/v2/admin/catalog/build-policy` | — | 200 BuildRuntimePolicy | capability |
| `listComputePlans` | `GET /api/v2/catalog/compute-plans` | — | 200 ComputePlanPage | resource_catalog |
| `listModels` | `GET /api/v2/catalog/models` | — | 200 ModelPage | gateway |
| `listPricePolicyVersions` | `GET /api/v2/admin/catalog/price-policies` | — | 200 PricePolicyVersionPage | resource_catalog |
| `listPublisherNamespaces` | `GET /api/v2/admin/catalog/publisher-namespaces` | — | 200 PublisherNamespacePage | capability |
| `listRefundPolicyVersions` | `GET /api/v2/admin/catalog/refund-policies` | — | 200 RefundPolicyVersionPage | resource_catalog |
| `listRetentionPolicyVersions` | `GET /api/v2/admin/catalog/retention-policies` | — | 200 RetentionPolicyVersionPage | resource_catalog |
| `listRuntimeVersions` | `GET /api/v2/catalog/runtime-versions` | — | 200 RuntimeVersionPage | capability |
| `listStoragePlans` | `GET /api/v2/catalog/storage-plans` | — | 200 StoragePlanPage | resource_catalog |
| `listWebuiVersions` | `GET /api/v2/catalog/webui-versions` | — | 200 WebuiVersionPage | capability |
| `publishOfficialPackage` | `POST /api/v2/admin/packages/{packageId}/publish` | PublishPackageRequest | 200 Package | capability |
| `registerRuntimeVersion` | `POST /api/v2/admin/catalog/runtime-versions` | RegisterRuntimeVersionRequest | 201 RuntimeVersion | capability |
| `registerWebuiVersion` | `POST /api/v2/admin/catalog/webui-versions` | RegisterWebuiVersionRequest | 201 WebuiVersion | capability |
| `revokePublisherNamespace` | `POST /api/v2/admin/catalog/publisher-namespaces/{publisherNamespaceId}/revoke` | RevokePublisherNamespaceRequest | 200 PublisherNamespace | capability |
| `setBuildRuntimePolicy` | `PUT /api/v2/admin/catalog/build-policy` | SetBuildRuntimePolicyRequest | 200 BuildRuntimePolicy | capability |
| `setComputePlanAvailability` | `PUT /api/v2/admin/catalog/compute-plans/{planId}/availability` | PlanAvailabilityRequest | 200 ComputePlan | resource_catalog |
| `setRuntimeVersionStatus` | `PUT /api/v2/admin/catalog/runtime-versions/{versionId}/status` | CatalogStatusRequest | 200 RuntimeVersion | capability |
| `setStoragePlanAvailability` | `PUT /api/v2/admin/catalog/storage-plans/{planId}/availability` | PlanAvailabilityRequest | 200 StoragePlan | resource_catalog |
| `setWebuiVersionStatus` | `PUT /api/v2/admin/catalog/webui-versions/{versionId}/status` | CatalogStatusRequest | 200 WebuiVersion | capability |

**显示字段来源**（精确Schema.field，包含正常/处理中/失败页字段；可选性按03）：

- `AnonymousAccessContract`：`mode`
- `ApplicationOwnedAccessContract`：`loginPath`、`logoutPath`、`mode`、`usernameCredentialName`
- `ArtifactReference`：`digest`、`platform`、`repository`
- `BuildRecipeContract`：`frontend`、`networkPolicy`、`outputImageCommand`、`outputPlatform`、`packageInput`、`recipe`、`runtimeContextName`、`version`、`webuiInput`
- `BuildRecipeContractOutputImageCommand`：`cmd`、`entrypoint`
- `BuildRuntimePolicy`：`createdAt`、`defaultWebuiVersionId`、`effectiveAt`、`id`、`policyVersion`、`runtimeVersionId`
- `CloudPrivateAccessContract`：`admissionReceiptId`、`entryContract`、`mode`
- `ComputePlan`：`availability`、`billingMode`、`createdAt`、`id`、`memoryMiB`、`monthlyPriceUSDMicros`、`name`、`pricePolicyVersionId`、`providerCapabilityVersion`、`validFrom`、`validUntil`、`vcpus`
- `DataContract`：`mountPolicies`、`rollback`、`schemaVersion`、`upgrade`
- `DataMountPolicy`：`concurrentWritersSupported`、`mountName`
- `DataRollbackContract`：`compatibleSchemaVersions`、`safe`
- `DataUpgradeContract`：`backupFormatVersion`、`backupRequired`、`compatibleFromSchemaVersions`、`migrationArtifact`、`mode`
- `DowngradePlanRules`：`cancelBefore`、`currentPeriodRefund`、`earlyPaidChange`、`fallback`、`kind`、`knownFailureCompensation`、`manualUnpaidBoundary`、`nextPeriodPrice`、`plannedBoundary`、`requiresConfirmedNextPeriodPayment`
- `ImagePlatform`：`architecture`、`os`、`variant`
- `Model`：`available`、`capabilities`、`fetchedAt`、`id`、`inputPricePerMillionTokensUSDMicros`、`name`、`outputPricePerMillionTokensUSDMicros`、`priceSource`
- `ModelConfigurationContract`：`applyPath`、`authorizationSecretInputName`、`portName`、`protocol`、`readbackFields`、`readbackPath`、`requestFields`
- `Package`：`createdAt`、`description`、`id`、`latestReadyVersionId`、`name`、`namespaceId`、`status`、`updatedAt`、`visibility`
- `PackageBuildInput`：`contextName`、`formatVersion`、`gid`、`sourceRoot`、`targetPath`、`uid`
- `PackageFormatContractReference`：`admissionReceiptId`、`formatVersion`、`owner`、`schemaDigest`、`schemaObjectRef`、`validatorArtifact`
- `PlanChangePolicy`：`approvalStatus`、`baseRefundPolicy`、`cancelAndReplace`、`classification`、`concurrency`、`downgrade`、`mixedOrIncomparableTransition`、`noOpTransition`、`providerExecutionPlan`、`storageShrink`、`upgrade`、`version`
- `PricePolicyVersion`：`computeMonthlyUSDMicros`、`computePlanId`、`createdAt`、`currency`、`id`、`periodMonths`、`planChangePolicy`、`planChangePolicyVersion`、`productMonthlyUSDMicros`、`renewalPolicy`、`storageMonthlyUSDMicros`、`storagePlanId`、`validFrom`、`validUntil`、`versionLabel`
- `PublisherNamespace`：`admissionReceiptId`、`createdAt`、`id`、`kind`、`name`、`registryId`、`repositoryPrefix`、`status`
- `RecipeArtifact`：`digest`、`dockerfilePath`、`mediaType`、`repository`
- `RefundPolicyVersion`：`algorithm`、`createdAt`、`customerTerms`、`id`、`retentionPolicyVersionId`、`validFrom`、`validUntil`、`versionLabel`
- `RenewalPolicy`：`effectiveStart`、`months`、`trigger`、`usesAcceptedPriceSnapshot`、`version`
- `RetentionPolicyVersion`：`buildHistoryDisposition`、`createdAt`、`customerTerms`、`id`、`packageHistoryDisposition`、`tenantRestoreDays`、`versionLabel`、`workspaceDataDisposition`
- `RuntimePublisherContract`：`applicationAccess`、`applicationRevisionTemplate`、`buildRecipe`、`data`、`image`、`kind`、`modelConfiguration`、`packageFormatContracts`、`packageFormatVersions`、`publisherNamespaceId`、`runtimeAbiVersion`、`schemaVersion`
- `RuntimeVersion`：`admissionReceiptId`、`artifactDigest`、`createdAt`、`defaultForNewBuilds`、`id`、`name`、`packageFormatVersions`、`publisherContract`、`publisherContractDigest`、`publisherContractObjectRef`、`publisherNamespaceId`、`runtimeAbiVersion`、`status`、`versionLabel`
- `StoragePlan`：`availability`、`billingMode`、`capacityGiB`、`createdAt`、`id`、`monthlyPriceUSDMicros`、`name`、`pricePolicyVersionId`、`shrinkSupported`、`validFrom`、`validUntil`
- `UpgradePlanRules`：`chargeClock`、`chargeRounding`、`effectiveWhen`、`irreversibleResidualCostOwner`、`kind`、`knownFailureCompensation`、`oldPriceSource`、`preservePaidPeriod`、`supplementDeleteRefund`、`timeUnit`、`unknownOutcome`、`zeroCharge`
- `WebuiBuildInput`：`contextName`、`gid`、`sourcePath`、`targetPath`、`uid`
- `WebuiPublisherContract`：`apiBasePath`、`assetBasePath`、`authenticationProtocol`、`entryFile`、`image`、`integrationMode`、`kind`、`publisherNamespaceId`、`runtimeAbiVersions`、`schemaVersion`、`staticRoot`、`supportsSse`、`supportsWebsocket`、`uiProtocolVersion`
- `WebuiVersion`：`admissionReceiptId`、`artifactDigest`、`createdAt`、`id`、`name`、`publisherContract`、`publisherContractDigest`、`publisherContractObjectRef`、`publisherNamespaceId`、`runtimeAbiVersions`、`status`、`uiProtocolVersion`、`versionLabel`
- `WorkspaceApplicationCompute`：`cpuLimitMilli`、`cpuRequestMilli`、`memoryLimitBytes`、`memoryRequestBytes`
- `WorkspaceApplicationConfigInput`：`name`、`target`
- `WorkspaceApplicationCredential`：`env`、`kind`、`name`、`target`、`username`
- `WorkspaceApplicationDependency`：`command`、`compute`、`configInputs`、`dependsOn`、`execution`、`healthChecks`、`image`、`name`、`persistentMounts`、`ports`、`scratchMounts`、`secretInputs`
- `WorkspaceApplicationDependencyCommand`：`args`、`entrypoint`、`env`
- `WorkspaceApplicationDependencyHealthCheck`：`command`、`initialDelaySeconds`、`path`、`port`、`type`
- `WorkspaceApplicationExecution`：`groupId`、`init`、`seccompProfile`、`userId`
- `WorkspaceApplicationHealthCheck`：`initialDelaySeconds`、`path`、`port`
- `WorkspaceApplicationMount`：`executable`、`groupId`、`mode`、`mountPath`、`name`、`readOnly`、`sizeBytes`、`userId`
- `WorkspaceApplicationPort`：`name`、`port`、`protocol`
- `WorkspaceApplicationRevision`：`applicationId`、`compute`、`configInputs`、`credentials`、`dependencies`、`entryPort`、`entrypoint`、`execution`、`exposurePolicy`、`healthChecks`、`image`、`persistentMounts`、`platform`、`ports`、`schemaVersion`、`scratchMounts`、`secretInputs`、`version`
- `WorkspaceApplicationSecretInput`：`env`、`name`、`target`

**数据表及写入Owner**：`build.idempotency_records`、`build.operations`、`capability.capability_versions`、`capability.catalog_policies`、`capability.idempotency_records`、`capability.operations`、`capability.packages`、`capability.publisher_namespaces`、`capability.runtime_versions`、`capability.webui_versions`、`fabric.idempotency_records`、`fabric.operations`、`gateway.idempotency_records`、`gateway.operations`、`ledger.idempotency_records`、`resource_catalog.compute_plans`、`resource_catalog.idempotency_records`、`resource_catalog.operations`、`resource_catalog.price_policy_versions`、`resource_catalog.refund_policy_versions`、`resource_catalog.retention_policy_versions`、`resource_catalog.storage_plans`、`runtime_control.idempotency_records`、`runtime_control.operations`、`tenant.idempotency_records`、`tenant.operations`、`workspace.idempotency_records`、`workspace.operations`

**验收向量**：

- **success**：客户目录呈现已批准且兼容的版本和套餐，管理员可读回批准记录及不可变标识。
- **failure**：目录缺失/失效时明确不可选择，不内置默认runtime、provider或价格。
- **duplicate**：同一提交意图保留 Idempotency-Key；重发相同请求只显示原资源/操作，不新增副作用。修改输入产生新意图，不复用旧 key。
- **unauthorized**：缺少页面动作权限时不提供按钮；服务端仍独立授权。访问其他 Tenant 对象显示不存在，不暴露是否存在。
- **restart**：刷新、切页或登录恢复后从权威 GET 重建视图；异步命令以原 Operation 继续查询，页面不会重新发起命令。

跨域状态/恢复执行见06；输入约束、按钮条件和中文错误见04/07。涉及旧对象同时执行09迁移接受标准。

## F04 上传Package与后续版本、确认构建

**页面**：`/console/agents/upload`（上传向导）、`/console/agents/:packageId/upload`（新包版本）

| operationId | 请求 | 请求DTO | 成功响应DTO | 唯一Owner |
|---|---|---|---|---|
| `completeUpload` | `POST /api/v2/uploads/{uploadId}/complete` | CompleteUploadRequest | 202 Operation | capability |
| `createBuild` | `POST /api/v2/builds` | CreateBuildRequest | 201 BuildJob | build |
| `createPackage` | `POST /api/v2/packages` | CreatePackageRequest | 201 Package | capability |
| `createUpload` | `POST /api/v2/packages/{packageId}/uploads` | CreateUploadRequest | 201 UploadSession | capability |
| `createUploadPart` | `POST /api/v2/uploads/{uploadId}/parts` | CreateUploadPartRequest | 200 UploadPartAuthorization | capability |
| `getOperation` | `GET /api/v2/operations/{owner}/{operationId}` | — | 200 Operation | workspace |
| `getPackage` | `GET /api/v2/packages/{packageId}` | — | 200 Package | capability |
| `getPackageVersion` | `GET /api/v2/package-versions/{packageVersionId}` | — | 200 PackageVersion | capability |
| `getUpload` | `GET /api/v2/uploads/{uploadId}` | — | 200 UploadSession | capability |
| `listNamespaces` | `GET /api/v2/namespaces` | — | 200 NamespacePage | capability |
| `listPackageVersions` | `GET /api/v2/packages/{packageId}/versions` | — | 200 PackageVersionPage | capability |
| `listWebuiVersions` | `GET /api/v2/catalog/webui-versions` | — | 200 WebuiVersionPage | capability |

**显示字段来源**（精确Schema.field，包含正常/处理中/失败页字段；可选性按03）：

- `ArtifactReference`：`digest`、`platform`、`repository`
- `BuildJob`：`artifactDigest`、`createdAt`、`errorCode`、`id`、`inputClaimIds`、`operationId`、`packageVersionId`、`resultCapabilityVersionId`、`retryAllowed`、`retryOfBuildJobId`、`runtimeVersionId`、`stage`、`status`、`updatedAt`、`webuiVersionId`
- `ImagePlatform`：`architecture`、`os`、`variant`
- `Namespace`：`createdAt`、`id`、`isDefault`、`name`、`status`
- `Operation`：`createdAt`、`errorCode`、`kind`、`observationResult`、`operationId`、`owner`、`pollAfterSeconds`、`requestId`、`resourceId`、`stage`、`status`、`updatedAt`
- `Package`：`createdAt`、`description`、`id`、`latestReadyVersionId`、`name`、`namespaceId`、`status`、`updatedAt`、`visibility`
- `PackageVersion`：`createdAt`、`id`、`packageId`、`sha256`、`sizeBytes`、`status`、`versionLabel`
- `UploadPart`：`etag`、`partNumber`、`sha256`、`sizeBytes`
- `UploadSession`：`completedParts`、`expiresAt`、`id`、`packageVersionId`、`partSizeBytes`、`sha256`、`sizeBytes`、`status`
- `WebuiPublisherContract`：`apiBasePath`、`assetBasePath`、`authenticationProtocol`、`entryFile`、`image`、`integrationMode`、`kind`、`publisherNamespaceId`、`runtimeAbiVersions`、`schemaVersion`、`staticRoot`、`supportsSse`、`supportsWebsocket`、`uiProtocolVersion`
- `WebuiVersion`：`admissionReceiptId`、`artifactDigest`、`createdAt`、`id`、`name`、`publisherContract`、`publisherContractDigest`、`publisherContractObjectRef`、`publisherNamespaceId`、`runtimeAbiVersions`、`status`、`uiProtocolVersion`、`versionLabel`

**数据表及写入Owner**：`build.build_jobs`、`build.idempotency_records`、`build.operations`、`build.outbox_events`、`capability.catalog_policies`、`capability.idempotency_records`、`capability.namespaces`、`capability.operations`、`capability.outbox_events`、`capability.package_versions`、`capability.packages`、`capability.runtime_versions`、`capability.upload_chunks`、`capability.upload_sessions`、`capability.webui_versions`、`fabric.idempotency_records`、`fabric.operations`、`fabric.outbox_events`、`gateway.idempotency_records`、`gateway.operations`、`gateway.outbox_events`、`ledger.idempotency_records`、`ledger.outbox_events`、`resource_catalog.idempotency_records`、`resource_catalog.operations`、`resource_catalog.outbox_events`、`runtime_control.idempotency_records`、`runtime_control.operations`、`runtime_control.outbox_events`、`tenant.idempotency_records`、`tenant.operations`、`tenant.outbox_events`、`workspace.idempotency_records`、`workspace.operations`、`workspace.outbox_events`

**验收向量**：

- **success**：先看到“上传已验证”，确认构建后看到独立任务ID和“查看构建进度”按钮。
- **failure**：摘要/包校验失败保留具体字段错误；修正文件需新上传会话。上传成功但构建创建失败保留已上传版本，可重新提交原构建意图。
- **duplicate**：同一提交意图保留 Idempotency-Key；重发相同请求只显示原资源/操作，不新增副作用。修改输入产生新意图，不复用旧 key。
- **unauthorized**：缺少页面动作权限时不提供按钮；服务端仍独立授权。访问其他 Tenant 对象显示不存在，不暴露是否存在。
- **restart**：刷新、切页或登录恢复后从权威 GET 重建视图；异步命令以原 Operation 继续查询，页面不会重新发起命令。

跨域状态/恢复执行见06；输入约束、按钮条件和中文错误见04/07。涉及旧对象同时执行09迁移接受标准。

## F05 构建进度、日志与失败重试

**页面**：`/console/agents/builds`（构建记录）、`/console/agents/builds/:buildJobId`（构建详情）

| operationId | 请求 | 请求DTO | 成功响应DTO | 唯一Owner |
|---|---|---|---|---|
| `createBuild` | `POST /api/v2/builds` | CreateBuildRequest | 201 BuildJob | build |
| `getBuild` | `GET /api/v2/builds/{buildId}` | — | 200 BuildJob | build |
| `getCapabilityVersion` | `GET /api/v2/capability-versions/{capabilityVersionId}` | — | 200 CapabilityVersion | capability |
| `getOperation` | `GET /api/v2/operations/{owner}/{operationId}` | — | 200 Operation | workspace |
| `listBuildLogs` | `GET /api/v2/builds/{buildId}/logs` | — | 200 BuildLogPage | build |
| `listBuilds` | `GET /api/v2/builds` | — | 200 BuildJobPage | build |
| `retryBuild` | `POST /api/v2/builds/{buildId}/retry` | — | 201 BuildJob | build |

**显示字段来源**（精确Schema.field，包含正常/处理中/失败页字段；可选性按03）：

- `AnonymousAccessContract`：`mode`
- `ApplicationOwnedAccessContract`：`loginPath`、`logoutPath`、`mode`、`usernameCredentialName`
- `ArtifactReference`：`digest`、`platform`、`repository`
- `BuildJob`：`artifactDigest`、`createdAt`、`errorCode`、`id`、`inputClaimIds`、`operationId`、`packageVersionId`、`resultCapabilityVersionId`、`retryAllowed`、`retryOfBuildJobId`、`runtimeVersionId`、`stage`、`status`、`updatedAt`、`webuiVersionId`
- `BuildLog`：`buildJobId`、`createdAt`、`id`、`level`、`message`、`sequence`、`stage`
- `BuildRecipeContract`：`frontend`、`networkPolicy`、`outputImageCommand`、`outputPlatform`、`packageInput`、`recipe`、`runtimeContextName`、`version`、`webuiInput`
- `BuildRecipeContractOutputImageCommand`：`cmd`、`entrypoint`
- `CapabilityVersion`：`artifact`、`artifactDigest`、`buildJobId`、`createdAt`、`dataCompatibility`、`deploymentDescriptor`、`deploymentDescriptorDigest`、`deploymentDescriptorObjectRef`、`id`、`legacyApplicationRevisionId`、`modelRequirements`、`packageId`、`packageVersionId`、`provenance`、`referenceCount`、`runtimeVersionId`、`status`、`versionLabel`、`webuiVersionId`
- `CloudPrivateAccessContract`：`admissionReceiptId`、`entryContract`、`mode`
- `DataCompatibility`：`compatibleFromVersions`、`dataSchemaVersion`、`migrationReceiptId`、`migrationRequired`、`rollbackSafe`
- `DataContract`：`mountPolicies`、`rollback`、`schemaVersion`、`upgrade`
- `DataMountPolicy`：`concurrentWritersSupported`、`mountName`
- `DataRollbackContract`：`compatibleSchemaVersions`、`safe`
- `DataUpgradeContract`：`backupFormatVersion`、`backupRequired`、`compatibleFromSchemaVersions`、`migrationArtifact`、`mode`
- `DeploymentDescriptor`：`applicationRevision`、`artifact`、`buildInputDigest`、`legacyApplicationRevisionId`、`packageVersionId`、`provenance`、`runtimeContract`、`runtimeContractReference`、`schemaVersion`、`webuiContract`、`webuiContractReference`
- `ImagePlatform`：`architecture`、`os`、`variant`
- `ModelConfigurationContract`：`applyPath`、`authorizationSecretInputName`、`portName`、`protocol`、`readbackFields`、`readbackPath`、`requestFields`
- `ModelRequirement`：`allowedModelIds`、`capability`、`required`、`slot`
- `Operation`：`createdAt`、`errorCode`、`kind`、`observationResult`、`operationId`、`owner`、`pollAfterSeconds`、`requestId`、`resourceId`、`stage`、`status`、`updatedAt`
- `PackageBuildInput`：`contextName`、`formatVersion`、`gid`、`sourceRoot`、`targetPath`、`uid`
- `PackageFormatContractReference`：`admissionReceiptId`、`formatVersion`、`owner`、`schemaDigest`、`schemaObjectRef`、`validatorArtifact`
- `PublisherContractReference`：`descriptorDigest`、`descriptorObjectRef`、`kind`、`publisherNamespaceId`、`versionId`
- `RecipeArtifact`：`digest`、`dockerfilePath`、`mediaType`、`repository`
- `RuntimePublisherContract`：`applicationAccess`、`applicationRevisionTemplate`、`buildRecipe`、`data`、`image`、`kind`、`modelConfiguration`、`packageFormatContracts`、`packageFormatVersions`、`publisherNamespaceId`、`runtimeAbiVersion`、`schemaVersion`
- `WebuiBuildInput`：`contextName`、`gid`、`sourcePath`、`targetPath`、`uid`
- `WebuiPublisherContract`：`apiBasePath`、`assetBasePath`、`authenticationProtocol`、`entryFile`、`image`、`integrationMode`、`kind`、`publisherNamespaceId`、`runtimeAbiVersions`、`schemaVersion`、`staticRoot`、`supportsSse`、`supportsWebsocket`、`uiProtocolVersion`
- `WorkspaceApplicationCompute`：`cpuLimitMilli`、`cpuRequestMilli`、`memoryLimitBytes`、`memoryRequestBytes`
- `WorkspaceApplicationConfigInput`：`name`、`target`
- `WorkspaceApplicationCredential`：`env`、`kind`、`name`、`target`、`username`
- `WorkspaceApplicationDependency`：`command`、`compute`、`configInputs`、`dependsOn`、`execution`、`healthChecks`、`image`、`name`、`persistentMounts`、`ports`、`scratchMounts`、`secretInputs`
- `WorkspaceApplicationDependencyCommand`：`args`、`entrypoint`、`env`
- `WorkspaceApplicationDependencyHealthCheck`：`command`、`initialDelaySeconds`、`path`、`port`、`type`
- `WorkspaceApplicationExecution`：`groupId`、`init`、`seccompProfile`、`userId`
- `WorkspaceApplicationHealthCheck`：`initialDelaySeconds`、`path`、`port`
- `WorkspaceApplicationMount`：`executable`、`groupId`、`mode`、`mountPath`、`name`、`readOnly`、`sizeBytes`、`userId`
- `WorkspaceApplicationPort`：`name`、`port`、`protocol`
- `WorkspaceApplicationRevision`：`applicationId`、`compute`、`configInputs`、`credentials`、`dependencies`、`entryPort`、`entrypoint`、`execution`、`exposurePolicy`、`healthChecks`、`image`、`persistentMounts`、`platform`、`ports`、`schemaVersion`、`scratchMounts`、`secretInputs`、`version`
- `WorkspaceApplicationSecretInput`：`env`、`name`、`target`

**数据表及写入Owner**：`build.build_artifacts`、`build.build_jobs`、`build.build_logs`、`build.idempotency_records`、`build.inbox_events`、`build.operations`、`build.outbox_deliveries`、`build.outbox_events`、`capability.capability_versions`、`capability.idempotency_records`、`capability.inbox_events`、`capability.outbox_deliveries`、`capability.outbox_events`、`capability.package_versions`、`capability.reference_claims`、`fabric.idempotency_records`、`fabric.inbox_events`、`fabric.outbox_deliveries`、`fabric.outbox_events`、`gateway.idempotency_records`、`gateway.inbox_events`、`gateway.outbox_deliveries`、`gateway.outbox_events`、`ledger.idempotency_records`、`ledger.inbox_events`、`ledger.outbox_deliveries`、`ledger.outbox_events`、`ledger.receipts`、`resource_catalog.idempotency_records`、`resource_catalog.inbox_events`、`resource_catalog.outbox_deliveries`、`resource_catalog.outbox_events`、`runtime_control.idempotency_records`、`runtime_control.inbox_events`、`runtime_control.outbox_deliveries`、`runtime_control.outbox_events`、`tenant.idempotency_records`、`tenant.inbox_events`、`tenant.outbox_deliveries`、`tenant.outbox_events`、`workspace.idempotency_records`、`workspace.inbox_events`、`workspace.outbox_deliveries`、`workspace.outbox_events`

**验收向量**：

- **success**：构建及Capability登记均确认后显示“版本已可部署”；用户主动进入部署向导。
- **failure**：failed按错误码显示可修正原因；needs_attention显示“构建结果需要处理”，不能重新覆盖原job。日志读取失败不把任务判失败。
- **duplicate**：同一提交意图保留 Idempotency-Key；重发相同请求只显示原资源/操作，不新增副作用。修改输入产生新意图，不复用旧 key。
- **unauthorized**：缺少页面动作权限时不提供按钮；服务端仍独立授权。访问其他 Tenant 对象显示不存在，不暴露是否存在。
- **restart**：刷新、切页或登录恢复后从权威 GET 重建视图；异步命令以原 Operation 继续查询，页面不会重新发起命令。

跨域状态/恢复执行见06；输入约束、按钮条件和中文错误见04/07。涉及旧对象同时执行09迁移接受标准。

## F06 智能体详情、版本下架与引用保护

**页面**：`/console/agents/:packageId`（智能体详情）

| operationId | 请求 | 请求DTO | 成功响应DTO | 唯一Owner |
|---|---|---|---|---|
| `archivePackage` | `POST /api/v2/packages/{packageId}/archive` | — | 200 Package | capability |
| `deleteCapabilityVersion` | `DELETE /api/v2/capability-versions/{capabilityVersionId}` | — | 202 Operation | capability |
| `getCapabilityVersion` | `GET /api/v2/capability-versions/{capabilityVersionId}` | — | 200 CapabilityVersion | capability |
| `getOperation` | `GET /api/v2/operations/{owner}/{operationId}` | — | 200 Operation | workspace |
| `getPackage` | `GET /api/v2/packages/{packageId}` | — | 200 Package | capability |
| `getPackageVersion` | `GET /api/v2/package-versions/{packageVersionId}` | — | 200 PackageVersion | capability |
| `listCapabilityVersions` | `GET /api/v2/capability-versions` | — | 200 CapabilityVersionPage | capability |
| `listPackageVersions` | `GET /api/v2/packages/{packageId}/versions` | — | 200 PackageVersionPage | capability |
| `listPackages` | `GET /api/v2/packages` | — | 200 PackagePage | capability |

**显示字段来源**（精确Schema.field，包含正常/处理中/失败页字段；可选性按03）：

- `AnonymousAccessContract`：`mode`
- `ApplicationOwnedAccessContract`：`loginPath`、`logoutPath`、`mode`、`usernameCredentialName`
- `ArtifactReference`：`digest`、`platform`、`repository`
- `BuildRecipeContract`：`frontend`、`networkPolicy`、`outputImageCommand`、`outputPlatform`、`packageInput`、`recipe`、`runtimeContextName`、`version`、`webuiInput`
- `BuildRecipeContractOutputImageCommand`：`cmd`、`entrypoint`
- `CapabilityVersion`：`artifact`、`artifactDigest`、`buildJobId`、`createdAt`、`dataCompatibility`、`deploymentDescriptor`、`deploymentDescriptorDigest`、`deploymentDescriptorObjectRef`、`id`、`legacyApplicationRevisionId`、`modelRequirements`、`packageId`、`packageVersionId`、`provenance`、`referenceCount`、`runtimeVersionId`、`status`、`versionLabel`、`webuiVersionId`
- `CloudPrivateAccessContract`：`admissionReceiptId`、`entryContract`、`mode`
- `DataCompatibility`：`compatibleFromVersions`、`dataSchemaVersion`、`migrationReceiptId`、`migrationRequired`、`rollbackSafe`
- `DataContract`：`mountPolicies`、`rollback`、`schemaVersion`、`upgrade`
- `DataMountPolicy`：`concurrentWritersSupported`、`mountName`
- `DataRollbackContract`：`compatibleSchemaVersions`、`safe`
- `DataUpgradeContract`：`backupFormatVersion`、`backupRequired`、`compatibleFromSchemaVersions`、`migrationArtifact`、`mode`
- `DeploymentDescriptor`：`applicationRevision`、`artifact`、`buildInputDigest`、`legacyApplicationRevisionId`、`packageVersionId`、`provenance`、`runtimeContract`、`runtimeContractReference`、`schemaVersion`、`webuiContract`、`webuiContractReference`
- `ImagePlatform`：`architecture`、`os`、`variant`
- `ModelConfigurationContract`：`applyPath`、`authorizationSecretInputName`、`portName`、`protocol`、`readbackFields`、`readbackPath`、`requestFields`
- `ModelRequirement`：`allowedModelIds`、`capability`、`required`、`slot`
- `Operation`：`createdAt`、`errorCode`、`kind`、`observationResult`、`operationId`、`owner`、`pollAfterSeconds`、`requestId`、`resourceId`、`stage`、`status`、`updatedAt`
- `Package`：`createdAt`、`description`、`id`、`latestReadyVersionId`、`name`、`namespaceId`、`status`、`updatedAt`、`visibility`
- `PackageBuildInput`：`contextName`、`formatVersion`、`gid`、`sourceRoot`、`targetPath`、`uid`
- `PackageFormatContractReference`：`admissionReceiptId`、`formatVersion`、`owner`、`schemaDigest`、`schemaObjectRef`、`validatorArtifact`
- `PackageVersion`：`createdAt`、`id`、`packageId`、`sha256`、`sizeBytes`、`status`、`versionLabel`
- `PublisherContractReference`：`descriptorDigest`、`descriptorObjectRef`、`kind`、`publisherNamespaceId`、`versionId`
- `RecipeArtifact`：`digest`、`dockerfilePath`、`mediaType`、`repository`
- `RuntimePublisherContract`：`applicationAccess`、`applicationRevisionTemplate`、`buildRecipe`、`data`、`image`、`kind`、`modelConfiguration`、`packageFormatContracts`、`packageFormatVersions`、`publisherNamespaceId`、`runtimeAbiVersion`、`schemaVersion`
- `WebuiBuildInput`：`contextName`、`gid`、`sourcePath`、`targetPath`、`uid`
- `WebuiPublisherContract`：`apiBasePath`、`assetBasePath`、`authenticationProtocol`、`entryFile`、`image`、`integrationMode`、`kind`、`publisherNamespaceId`、`runtimeAbiVersions`、`schemaVersion`、`staticRoot`、`supportsSse`、`supportsWebsocket`、`uiProtocolVersion`
- `WorkspaceApplicationCompute`：`cpuLimitMilli`、`cpuRequestMilli`、`memoryLimitBytes`、`memoryRequestBytes`
- `WorkspaceApplicationConfigInput`：`name`、`target`
- `WorkspaceApplicationCredential`：`env`、`kind`、`name`、`target`、`username`
- `WorkspaceApplicationDependency`：`command`、`compute`、`configInputs`、`dependsOn`、`execution`、`healthChecks`、`image`、`name`、`persistentMounts`、`ports`、`scratchMounts`、`secretInputs`
- `WorkspaceApplicationDependencyCommand`：`args`、`entrypoint`、`env`
- `WorkspaceApplicationDependencyHealthCheck`：`command`、`initialDelaySeconds`、`path`、`port`、`type`
- `WorkspaceApplicationExecution`：`groupId`、`init`、`seccompProfile`、`userId`
- `WorkspaceApplicationHealthCheck`：`initialDelaySeconds`、`path`、`port`
- `WorkspaceApplicationMount`：`executable`、`groupId`、`mode`、`mountPath`、`name`、`readOnly`、`sizeBytes`、`userId`
- `WorkspaceApplicationPort`：`name`、`port`、`protocol`
- `WorkspaceApplicationRevision`：`applicationId`、`compute`、`configInputs`、`credentials`、`dependencies`、`entryPort`、`entrypoint`、`execution`、`exposurePolicy`、`healthChecks`、`image`、`persistentMounts`、`platform`、`ports`、`schemaVersion`、`scratchMounts`、`secretInputs`、`version`
- `WorkspaceApplicationSecretInput`：`env`、`name`、`target`

**数据表及写入Owner**：`build.build_artifacts`、`build.build_jobs`、`build.operations`、`capability.capability_versions`、`capability.operations`、`capability.package_versions`、`capability.packages`、`capability.reference_claims`、`fabric.operations`、`gateway.operations`、`resource_catalog.operations`、`resource_catalog.refund_policy_versions`、`resource_catalog.retention_policy_versions`、`runtime_control.operations`、`tenant.operations`、`workspace.operations`

**验收向量**：

- **success**：目录版本变为deleted墓碑并不可被新部署选择；原Build/Package/部署证据保留；没有宣称OCI字节已物理清除。
- **failure**：ARTIFACT_REFERENCED阻止下架；不绕过引用检查、不通过删客户资源作为前端补救。
- **duplicate**：同一提交意图保留 Idempotency-Key；重发相同请求只显示原资源/操作，不新增副作用。修改输入产生新意图，不复用旧 key。
- **unauthorized**：缺少页面动作权限时不提供按钮；服务端仍独立授权。访问其他 Tenant 对象显示不存在，不暴露是否存在。
- **restart**：刷新、切页或登录恢复后从权威 GET 重建视图；异步命令以原 Operation 继续查询，页面不会重新发起命令。

跨域状态/恢复执行见06；输入约束、按钮条件和中文错误见04/07。涉及旧对象同时执行09迁移接受标准。

## F07 部署选择、准入与报价

**页面**：`/console/workspaces/new`（部署选择与报价）

| operationId | 请求 | 请求DTO | 成功响应DTO | 唯一Owner |
|---|---|---|---|---|
| `createQuote` | `POST /api/v2/quotes` | QuoteRequest | 201 Quote | resource_catalog |
| `getCapabilityVersion` | `GET /api/v2/capability-versions/{capabilityVersionId}` | — | 200 CapabilityVersion | capability |
| `getQuote` | `GET /api/v2/quotes/{quoteId}` | — | 200 Quote | resource_catalog |
| `getWallet` | `GET /api/v2/wallet` | — | 200 Wallet | gateway |
| `listCapabilityVersions` | `GET /api/v2/capability-versions` | — | 200 CapabilityVersionPage | capability |
| `listComputePlans` | `GET /api/v2/catalog/compute-plans` | — | 200 ComputePlanPage | resource_catalog |
| `listModels` | `GET /api/v2/catalog/models` | — | 200 ModelPage | gateway |
| `listRuntimeVersions` | `GET /api/v2/catalog/runtime-versions` | — | 200 RuntimeVersionPage | capability |
| `listStoragePlans` | `GET /api/v2/catalog/storage-plans` | — | 200 StoragePlanPage | resource_catalog |
| `listWebuiVersions` | `GET /api/v2/catalog/webui-versions` | — | 200 WebuiVersionPage | capability |

**显示字段来源**（精确Schema.field，包含正常/处理中/失败页字段；可选性按03）：

- `AnonymousAccessContract`：`mode`
- `ApplicationOwnedAccessContract`：`loginPath`、`logoutPath`、`mode`、`usernameCredentialName`
- `ArtifactReference`：`digest`、`platform`、`repository`
- `BuildRecipeContract`：`frontend`、`networkPolicy`、`outputImageCommand`、`outputPlatform`、`packageInput`、`recipe`、`runtimeContextName`、`version`、`webuiInput`
- `BuildRecipeContractOutputImageCommand`：`cmd`、`entrypoint`
- `CapabilityVersion`：`artifact`、`artifactDigest`、`buildJobId`、`createdAt`、`dataCompatibility`、`deploymentDescriptor`、`deploymentDescriptorDigest`、`deploymentDescriptorObjectRef`、`id`、`legacyApplicationRevisionId`、`modelRequirements`、`packageId`、`packageVersionId`、`provenance`、`referenceCount`、`runtimeVersionId`、`status`、`versionLabel`、`webuiVersionId`
- `CloudPrivateAccessContract`：`admissionReceiptId`、`entryContract`、`mode`
- `ComputePlan`：`availability`、`billingMode`、`createdAt`、`id`、`memoryMiB`、`monthlyPriceUSDMicros`、`name`、`pricePolicyVersionId`、`providerCapabilityVersion`、`validFrom`、`validUntil`、`vcpus`
- `CreditSource`：`amountUSDMicros`、`creditReceiptId`、`originalSubscriptionPeriodId`、`originalWalletOperationId`、`policyVersionId`
- `DataCompatibility`：`compatibleFromVersions`、`dataSchemaVersion`、`migrationReceiptId`、`migrationRequired`、`rollbackSafe`
- `DataContract`：`mountPolicies`、`rollback`、`schemaVersion`、`upgrade`
- `DataMountPolicy`：`concurrentWritersSupported`、`mountName`
- `DataRollbackContract`：`compatibleSchemaVersions`、`safe`
- `DataUpgradeContract`：`backupFormatVersion`、`backupRequired`、`compatibleFromSchemaVersions`、`migrationArtifact`、`mode`
- `DeploymentDescriptor`：`applicationRevision`、`artifact`、`buildInputDigest`、`legacyApplicationRevisionId`、`packageVersionId`、`provenance`、`runtimeContract`、`runtimeContractReference`、`schemaVersion`、`webuiContract`、`webuiContractReference`
- `ImagePlatform`：`architecture`、`os`、`variant`
- `Model`：`available`、`capabilities`、`fetchedAt`、`id`、`inputPricePerMillionTokensUSDMicros`、`name`、`outputPricePerMillionTokensUSDMicros`、`priceSource`
- `ModelConfigurationContract`：`applyPath`、`authorizationSecretInputName`、`portName`、`protocol`、`readbackFields`、`readbackPath`、`requestFields`
- `ModelRequirement`：`allowedModelIds`、`capability`、`required`、`slot`
- `ModelSelection`：`modelId`、`slot`
- `NextPeriodPlanQuote`：`periodEnd`、`periodStart`、`pricePolicyVersionId`、`totalUSDMicros`
- `PackageBuildInput`：`contextName`、`formatVersion`、`gid`、`sourceRoot`、`targetPath`、`uid`
- `PackageFormatContractReference`：`admissionReceiptId`、`formatVersion`、`owner`、`schemaDigest`、`schemaObjectRef`、`validatorArtifact`
- `PlanChangeCalculation`：`chargeUSDMicros`、`executionPlanDigest`、`executionPlanId`、`kind`、`nextPeriod`、`periodEnd`、`periodEndMilliseconds`、`periodStart`、`periodStartMilliseconds`、`plannedEffectiveAt`、`policyVersion`、`quoteAt`、`quoteAtMilliseconds`、`sourceComputePlanId`、`sourceFinancialSnapshotDigest`、`sourceMonthlyUSDMicros`、`sourcePeriodId`、`sourcePricePolicyVersionId`、`sourceStoragePlanId`、`sourceSubscriptionId`、`sourceSubscriptionVersion`、`targetComputePlanId`、`targetMonthlyUSDMicros`、`targetPricePolicyVersionId`、`targetStoragePlanId`、`transitionId`、`upgradeProration`
- `PublisherContractReference`：`descriptorDigest`、`descriptorObjectRef`、`kind`、`publisherNamespaceId`、`versionId`
- `Quote`：`capabilityVersionId`、`computePlanId`、`createdAt`、`expectedInterruption`、`expiresAt`、`id`、`lineItems`、`modelSelections`、`periodEnd`、`periodMonths`、`periodStart`、`planChangeCalculation`、`pricePolicyVersionId`、`purpose`、`refundPolicyVersionId`、`refundTerms`、`retentionPolicyVersionId`、`retentionTerms`、`runtimeReadbackRequirement`、`scheduledPlanChangeId`、`sourceSubscriptionVersion`、`status`、`storagePlanId`、`totalUSDMicros`、`workspaceId`
- `QuoteLine`：`amountUSDMicros`、`creditSource`、`description`、`kind`、`quantity`
- `RecipeArtifact`：`digest`、`dockerfilePath`、`mediaType`、`repository`
- `RuntimePublisherContract`：`applicationAccess`、`applicationRevisionTemplate`、`buildRecipe`、`data`、`image`、`kind`、`modelConfiguration`、`packageFormatContracts`、`packageFormatVersions`、`publisherNamespaceId`、`runtimeAbiVersion`、`schemaVersion`
- `RuntimeVersion`：`admissionReceiptId`、`artifactDigest`、`createdAt`、`defaultForNewBuilds`、`id`、`name`、`packageFormatVersions`、`publisherContract`、`publisherContractDigest`、`publisherContractObjectRef`、`publisherNamespaceId`、`runtimeAbiVersion`、`status`、`versionLabel`
- `StoragePlan`：`availability`、`billingMode`、`capacityGiB`、`createdAt`、`id`、`monthlyPriceUSDMicros`、`name`、`pricePolicyVersionId`、`shrinkSupported`、`validFrom`、`validUntil`
- `UpgradeProration`：`periodMilliseconds`、`priceDeltaUSDMicros`、`remainingMilliseconds`、`rounding`
- `Wallet`：`balanceUSDMicros`、`currency`、`fetchedAt`、`source`、`status`
- `WebuiBuildInput`：`contextName`、`gid`、`sourcePath`、`targetPath`、`uid`
- `WebuiPublisherContract`：`apiBasePath`、`assetBasePath`、`authenticationProtocol`、`entryFile`、`image`、`integrationMode`、`kind`、`publisherNamespaceId`、`runtimeAbiVersions`、`schemaVersion`、`staticRoot`、`supportsSse`、`supportsWebsocket`、`uiProtocolVersion`
- `WebuiVersion`：`admissionReceiptId`、`artifactDigest`、`createdAt`、`id`、`name`、`publisherContract`、`publisherContractDigest`、`publisherContractObjectRef`、`publisherNamespaceId`、`runtimeAbiVersions`、`status`、`uiProtocolVersion`、`versionLabel`
- `WorkspaceApplicationCompute`：`cpuLimitMilli`、`cpuRequestMilli`、`memoryLimitBytes`、`memoryRequestBytes`
- `WorkspaceApplicationConfigInput`：`name`、`target`
- `WorkspaceApplicationCredential`：`env`、`kind`、`name`、`target`、`username`
- `WorkspaceApplicationDependency`：`command`、`compute`、`configInputs`、`dependsOn`、`execution`、`healthChecks`、`image`、`name`、`persistentMounts`、`ports`、`scratchMounts`、`secretInputs`
- `WorkspaceApplicationDependencyCommand`：`args`、`entrypoint`、`env`
- `WorkspaceApplicationDependencyHealthCheck`：`command`、`initialDelaySeconds`、`path`、`port`、`type`
- `WorkspaceApplicationExecution`：`groupId`、`init`、`seccompProfile`、`userId`
- `WorkspaceApplicationHealthCheck`：`initialDelaySeconds`、`path`、`port`
- `WorkspaceApplicationMount`：`executable`、`groupId`、`mode`、`mountPath`、`name`、`readOnly`、`sizeBytes`、`userId`
- `WorkspaceApplicationPort`：`name`、`port`、`protocol`
- `WorkspaceApplicationRevision`：`applicationId`、`compute`、`configInputs`、`credentials`、`dependencies`、`entryPort`、`entrypoint`、`execution`、`exposurePolicy`、`healthChecks`、`image`、`persistentMounts`、`platform`、`ports`、`schemaVersion`、`scratchMounts`、`secretInputs`、`version`
- `WorkspaceApplicationSecretInput`：`env`、`name`、`target`

**数据表及写入Owner**：`capability.capability_versions`、`capability.reference_claims`、`capability.runtime_versions`、`capability.webui_versions`、`resource_catalog.compute_plans`、`resource_catalog.price_policy_versions`、`resource_catalog.quote_items`、`resource_catalog.quotes`、`resource_catalog.refund_policy_versions`、`resource_catalog.retention_policy_versions`、`resource_catalog.storage_plans`、`workspace.model_configurations`

**验收向量**：

- **success**：客户知道将部署的确切版本、承担的费用周期与数据政策，后续提交引用已接受报价。
- **failure**：余额不足显示本次报价与权威余额，提示“请联系管理员充值后重新检查”；无自助支付接口不显示“立即付款”。
- **duplicate**：同一提交意图保留 Idempotency-Key；重发相同请求只显示原资源/操作，不新增副作用。修改输入产生新意图，不复用旧 key。
- **unauthorized**：缺少页面动作权限时不提供按钮；服务端仍独立授权。访问其他 Tenant 对象显示不存在，不暴露是否存在。
- **restart**：刷新、切页或登录恢复后从权威 GET 重建视图；异步命令以原 Operation 继续查询，页面不会重新发起命令。

跨域状态/恢复执行见06；输入约束、按钮条件和中文错误见04/07。涉及旧对象同时执行09迁移接受标准。

## F08 创建Workspace及部署结果

**页面**：`/console/operations/:owner/:operationId`（部署进度）

| operationId | 请求 | 请求DTO | 成功响应DTO | 唯一Owner |
|---|---|---|---|---|
| `createWorkspace` | `POST /api/v2/workspaces` | CreateWorkspaceRequest | 202 Operation | workspace |
| `getDeployment` | `GET /api/v2/workspaces/{workspaceId}/deployments/{deploymentId}` | — | 200 Deployment | workspace |
| `getOperation` | `GET /api/v2/operations/{owner}/{operationId}` | — | 200 Operation | workspace |
| `getWorkspace` | `GET /api/v2/workspaces/{workspaceId}` | — | 200 Workspace | workspace |
| `listWorkspaceTransactions` | `GET /api/v2/workspaces/{workspaceId}/transactions` | — | 200 WalletOperationPage | gateway |

**显示字段来源**（精确Schema.field，包含正常/处理中/失败页字段；可选性按03）：

- `DataCompatibility`：`compatibleFromVersions`、`dataSchemaVersion`、`migrationReceiptId`、`migrationRequired`、`rollbackSafe`
- `Deployment`：`capabilityVersionId`、`createdAt`、`dataCompatibility`、`id`、`previousDeploymentId`、`runtimeInstanceId`、`status`、`updatedAt`、`workspaceId`
- `Operation`：`createdAt`、`errorCode`、`kind`、`observationResult`、`operationId`、`owner`、`pollAfterSeconds`、`requestId`、`resourceId`、`stage`、`status`、`updatedAt`
- `WalletOperation`：`amountUSDMicros`、`coverageEnd`、`coverageEndMilliseconds`、`coverageStart`、`coverageStartMilliseconds`、`createdAt`、`errorCode`、`externalReference`、`id`、`kind`、`originalChargeOperationId`、`planChangeId`、`purpose`、`receiptId`、`status`、`updatedAt`、`workspaceId`
- `Workspace`：`accessUrl`、`activeDeploymentId`、`applicationAvailability`、`capabilityVersionId`、`computePlanId`、`createdAt`、`currentPeriodEnd`、`deliveryModel`、`id`、`modelConfigurationVersion`、`name`、`resourceReadiness`、`status`、`storagePlanId`、`updatedAt`、`version`

**数据表及写入Owner**：`build.idempotency_records`、`build.inbox_events`、`build.operations`、`build.outbox_deliveries`、`build.outbox_events`、`capability.idempotency_records`、`capability.inbox_events`、`capability.operations`、`capability.outbox_deliveries`、`capability.outbox_events`、`capability.reference_claims`、`fabric.attachments`、`fabric.idempotency_records`、`fabric.inbox_events`、`fabric.operations`、`fabric.outbox_deliveries`、`fabric.outbox_events`、`fabric.resource_actions`、`fabric.resource_sets`、`fabric.resources`、`fabric.route_bindings`、`fabric.route_switches`、`fabric.secret_bindings`、`gateway.idempotency_records`、`gateway.inbox_events`、`gateway.key_bindings`、`gateway.operations`、`gateway.outbox_deliveries`、`gateway.outbox_events`、`gateway.tenant_wallet_bindings`、`gateway.wallet_operations`、`ledger.idempotency_records`、`ledger.inbox_events`、`ledger.outbox_deliveries`、`ledger.outbox_events`、`ledger.receipts`、`ledger.reconciliations`、`resource_catalog.idempotency_records`、`resource_catalog.inbox_events`、`resource_catalog.operations`、`resource_catalog.outbox_deliveries`、`resource_catalog.outbox_events`、`resource_catalog.quotes`、`runtime_control.idempotency_records`、`runtime_control.inbox_events`、`runtime_control.operations`、`runtime_control.outbox_deliveries`、`runtime_control.outbox_events`、`runtime_control.runtime_actions`、`runtime_control.runtime_instances`、`tenant.accepted_operation_grants`、`tenant.authorization_contexts`、`tenant.idempotency_records`、`tenant.inbox_events`、`tenant.operations`、`tenant.outbox_deliveries`、`tenant.outbox_events`、`workspace.deployments`、`workspace.idempotency_records`、`workspace.inbox_events`、`workspace.model_configurations`、`workspace.operations`、`workspace.outbox_deliveries`、`workspace.outbox_events`、`workspace.saga_steps`、`workspace.subscription_periods`、`workspace.subscriptions`、`workspace.workspaces`

**验收向量**：

- **success**：扣费和资源/运行/Secret注入/证据都取得所需确认，客户主动点击打开。
- **failure**：unknown显示“结果正在核实，请勿重复提交”；needs_attention显示需要管理员处理。失败页独立展示是否已扣费、资源是否已清理、退款是否确认，不说“正在自动清理并退款”除非独立操作确有记录。
- **duplicate**：同一提交意图保留 Idempotency-Key；重发相同请求只显示原资源/操作，不新增副作用。修改输入产生新意图，不复用旧 key。
- **unauthorized**：缺少页面动作权限时不提供按钮；服务端仍独立授权。访问其他 Tenant 对象显示不存在，不暴露是否存在。
- **restart**：刷新、切页或登录恢复后从权威 GET 重建视图；异步命令以原 Operation 继续查询，页面不会重新发起命令。

跨域状态/恢复执行见06；输入约束、按钮条件和中文错误见04/07。涉及旧对象同时执行09迁移接受标准。

## F09 Workspace查询、打开与模型配置

**页面**：`/console/workspaces`（工作区列表）、`/console/workspaces/:workspaceId`（工作区详情）、`/console/workspaces/:workspaceId/models`（模型配置）

| operationId | 请求 | 请求DTO | 成功响应DTO | 唯一Owner |
|---|---|---|---|---|
| `adoptWorkspace` | `POST /api/v2/workspaces/{workspaceId}/adopt` | AdoptWorkspaceRequest | 202 Operation | workspace |
| `getDeployment` | `GET /api/v2/workspaces/{workspaceId}/deployments/{deploymentId}` | — | 200 Deployment | workspace |
| `getOperation` | `GET /api/v2/operations/{owner}/{operationId}` | — | 200 Operation | workspace |
| `getWorkspace` | `GET /api/v2/workspaces/{workspaceId}` | — | 200 Workspace | workspace |
| `getWorkspaceAccess` | `POST /api/v2/workspaces/{workspaceId}/access` | — | 200 WorkspaceAccess | workspace |
| `getWorkspaceModels` | `GET /api/v2/workspaces/{workspaceId}/models` | — | 200 ModelConfiguration | workspace |
| `listModels` | `GET /api/v2/catalog/models` | — | 200 ModelPage | gateway |
| `listWorkspaces` | `GET /api/v2/workspaces` | — | 200 WorkspacePage | workspace |
| `revealWorkspaceApplicationCredentials` | `POST /api/v2/workspaces/{workspaceId}/application-credentials/reveal` | — | 200 WorkspaceApplicationCredentials | workspace |
| `updateWorkspaceModels` | `PUT /api/v2/workspaces/{workspaceId}/models` | UpdateWorkspaceModelsRequest | 202 Operation | workspace |

**显示字段来源**（精确Schema.field，包含正常/处理中/失败页字段；可选性按03）：

- `DataCompatibility`：`compatibleFromVersions`、`dataSchemaVersion`、`migrationReceiptId`、`migrationRequired`、`rollbackSafe`
- `Deployment`：`capabilityVersionId`、`createdAt`、`dataCompatibility`、`id`、`previousDeploymentId`、`runtimeInstanceId`、`status`、`updatedAt`、`workspaceId`
- `Model`：`available`、`capabilities`、`fetchedAt`、`id`、`inputPricePerMillionTokensUSDMicros`、`name`、`outputPricePerMillionTokensUSDMicros`、`priceSource`
- `ModelConfiguration`：`appliedVersion`、`operationId`、`selections`、`status`、`updatedAt`、`version`、`workspaceId`
- `ModelSelection`：`modelId`、`slot`
- `Operation`：`createdAt`、`errorCode`、`kind`、`observationResult`、`operationId`、`owner`、`pollAfterSeconds`、`requestId`、`resourceId`、`stage`、`status`、`updatedAt`
- `Workspace`：`accessUrl`、`activeDeploymentId`、`applicationAvailability`、`capabilityVersionId`、`computePlanId`、`createdAt`、`currentPeriodEnd`、`deliveryModel`、`id`、`modelConfigurationVersion`、`name`、`resourceReadiness`、`status`、`storagePlanId`、`updatedAt`、`version`
- `WorkspaceAccess`：`applicationCredentialsAvailable`、`authenticationMode`、`expiresAt`、`url`、`workspaceId`
- `WorkspaceApplicationCredentials`：`password`、`runtimeInstanceId`、`username`、`workspaceId`

**数据表及写入Owner**：`build.operations`、`capability.operations`、`fabric.operations`、`fabric.route_bindings`、`fabric.secret_bindings`、`gateway.key_bindings`、`gateway.operations`、`resource_catalog.operations`、`runtime_control.operations`、`runtime_control.runtime_actions`、`runtime_control.runtime_instances`、`tenant.operations`、`workspace.deployments`、`workspace.model_configurations`、`workspace.operations`、`workspace.workspaces`

**验收向量**：

- **success**：打开的是当前选中部署，不是最初Launch的历史Runtime；模型配置可确认实际加载。
- **failure**：运行不可用时不展示可点击“已运行”；访问拒绝有具体文案。配置保存但reload未确认显示分阶段结果。
- **duplicate**：同一提交意图保留 Idempotency-Key；重发相同请求只显示原资源/操作，不新增副作用。修改输入产生新意图，不复用旧 key。
- **unauthorized**：缺少页面动作权限时不提供按钮；服务端仍独立授权。访问其他 Tenant 对象显示不存在，不暴露是否存在。
- **restart**：刷新、切页或登录恢复后从权威 GET 重建视图；异步命令以原 Operation 继续查询，页面不会重新发起命令。

跨域状态/恢复执行见06；输入约束、按钮条件和中文错误见04/07。涉及旧对象同时执行09迁移接受标准。

## F10 Agent更新、Runtime重建与回滚

**页面**：`/console/workspaces/:workspaceId/update`（版本更新）、`/console/workspaces/:workspaceId/deployments`（部署历史）

| operationId | 请求 | 请求DTO | 成功响应DTO | 唯一Owner |
|---|---|---|---|---|
| `getCapabilityVersion` | `GET /api/v2/capability-versions/{capabilityVersionId}` | — | 200 CapabilityVersion | capability |
| `getDeployment` | `GET /api/v2/workspaces/{workspaceId}/deployments/{deploymentId}` | — | 200 Deployment | workspace |
| `getOperation` | `GET /api/v2/operations/{owner}/{operationId}` | — | 200 Operation | workspace |
| `getWorkspace` | `GET /api/v2/workspaces/{workspaceId}` | — | 200 Workspace | workspace |
| `listCapabilityVersions` | `GET /api/v2/capability-versions` | — | 200 CapabilityVersionPage | capability |
| `listDeployments` | `GET /api/v2/workspaces/{workspaceId}/deployments` | — | 200 DeploymentPage | workspace |
| `listRuntimeVersions` | `GET /api/v2/catalog/runtime-versions` | — | 200 RuntimeVersionPage | capability |
| `rollbackWorkspace` | `POST /api/v2/workspaces/{workspaceId}/rollback` | RollbackWorkspaceRequest | 202 Operation | workspace |
| `updateWorkspaceVersion` | `POST /api/v2/workspaces/{workspaceId}/update` | UpdateWorkspaceVersionRequest | 202 Operation | workspace |

**显示字段来源**（精确Schema.field，包含正常/处理中/失败页字段；可选性按03）：

- `AnonymousAccessContract`：`mode`
- `ApplicationOwnedAccessContract`：`loginPath`、`logoutPath`、`mode`、`usernameCredentialName`
- `ArtifactReference`：`digest`、`platform`、`repository`
- `BuildRecipeContract`：`frontend`、`networkPolicy`、`outputImageCommand`、`outputPlatform`、`packageInput`、`recipe`、`runtimeContextName`、`version`、`webuiInput`
- `BuildRecipeContractOutputImageCommand`：`cmd`、`entrypoint`
- `CapabilityVersion`：`artifact`、`artifactDigest`、`buildJobId`、`createdAt`、`dataCompatibility`、`deploymentDescriptor`、`deploymentDescriptorDigest`、`deploymentDescriptorObjectRef`、`id`、`legacyApplicationRevisionId`、`modelRequirements`、`packageId`、`packageVersionId`、`provenance`、`referenceCount`、`runtimeVersionId`、`status`、`versionLabel`、`webuiVersionId`
- `CloudPrivateAccessContract`：`admissionReceiptId`、`entryContract`、`mode`
- `DataCompatibility`：`compatibleFromVersions`、`dataSchemaVersion`、`migrationReceiptId`、`migrationRequired`、`rollbackSafe`
- `DataContract`：`mountPolicies`、`rollback`、`schemaVersion`、`upgrade`
- `DataMountPolicy`：`concurrentWritersSupported`、`mountName`
- `DataRollbackContract`：`compatibleSchemaVersions`、`safe`
- `DataUpgradeContract`：`backupFormatVersion`、`backupRequired`、`compatibleFromSchemaVersions`、`migrationArtifact`、`mode`
- `Deployment`：`capabilityVersionId`、`createdAt`、`dataCompatibility`、`id`、`previousDeploymentId`、`runtimeInstanceId`、`status`、`updatedAt`、`workspaceId`
- `DeploymentDescriptor`：`applicationRevision`、`artifact`、`buildInputDigest`、`legacyApplicationRevisionId`、`packageVersionId`、`provenance`、`runtimeContract`、`runtimeContractReference`、`schemaVersion`、`webuiContract`、`webuiContractReference`
- `ImagePlatform`：`architecture`、`os`、`variant`
- `ModelConfigurationContract`：`applyPath`、`authorizationSecretInputName`、`portName`、`protocol`、`readbackFields`、`readbackPath`、`requestFields`
- `ModelRequirement`：`allowedModelIds`、`capability`、`required`、`slot`
- `Operation`：`createdAt`、`errorCode`、`kind`、`observationResult`、`operationId`、`owner`、`pollAfterSeconds`、`requestId`、`resourceId`、`stage`、`status`、`updatedAt`
- `PackageBuildInput`：`contextName`、`formatVersion`、`gid`、`sourceRoot`、`targetPath`、`uid`
- `PackageFormatContractReference`：`admissionReceiptId`、`formatVersion`、`owner`、`schemaDigest`、`schemaObjectRef`、`validatorArtifact`
- `PublisherContractReference`：`descriptorDigest`、`descriptorObjectRef`、`kind`、`publisherNamespaceId`、`versionId`
- `RecipeArtifact`：`digest`、`dockerfilePath`、`mediaType`、`repository`
- `RuntimePublisherContract`：`applicationAccess`、`applicationRevisionTemplate`、`buildRecipe`、`data`、`image`、`kind`、`modelConfiguration`、`packageFormatContracts`、`packageFormatVersions`、`publisherNamespaceId`、`runtimeAbiVersion`、`schemaVersion`
- `RuntimeVersion`：`admissionReceiptId`、`artifactDigest`、`createdAt`、`defaultForNewBuilds`、`id`、`name`、`packageFormatVersions`、`publisherContract`、`publisherContractDigest`、`publisherContractObjectRef`、`publisherNamespaceId`、`runtimeAbiVersion`、`status`、`versionLabel`
- `WebuiBuildInput`：`contextName`、`gid`、`sourcePath`、`targetPath`、`uid`
- `WebuiPublisherContract`：`apiBasePath`、`assetBasePath`、`authenticationProtocol`、`entryFile`、`image`、`integrationMode`、`kind`、`publisherNamespaceId`、`runtimeAbiVersions`、`schemaVersion`、`staticRoot`、`supportsSse`、`supportsWebsocket`、`uiProtocolVersion`
- `Workspace`：`accessUrl`、`activeDeploymentId`、`applicationAvailability`、`capabilityVersionId`、`computePlanId`、`createdAt`、`currentPeriodEnd`、`deliveryModel`、`id`、`modelConfigurationVersion`、`name`、`resourceReadiness`、`status`、`storagePlanId`、`updatedAt`、`version`
- `WorkspaceApplicationCompute`：`cpuLimitMilli`、`cpuRequestMilli`、`memoryLimitBytes`、`memoryRequestBytes`
- `WorkspaceApplicationConfigInput`：`name`、`target`
- `WorkspaceApplicationCredential`：`env`、`kind`、`name`、`target`、`username`
- `WorkspaceApplicationDependency`：`command`、`compute`、`configInputs`、`dependsOn`、`execution`、`healthChecks`、`image`、`name`、`persistentMounts`、`ports`、`scratchMounts`、`secretInputs`
- `WorkspaceApplicationDependencyCommand`：`args`、`entrypoint`、`env`
- `WorkspaceApplicationDependencyHealthCheck`：`command`、`initialDelaySeconds`、`path`、`port`、`type`
- `WorkspaceApplicationExecution`：`groupId`、`init`、`seccompProfile`、`userId`
- `WorkspaceApplicationHealthCheck`：`initialDelaySeconds`、`path`、`port`
- `WorkspaceApplicationMount`：`executable`、`groupId`、`mode`、`mountPath`、`name`、`readOnly`、`sizeBytes`、`userId`
- `WorkspaceApplicationPort`：`name`、`port`、`protocol`
- `WorkspaceApplicationRevision`：`applicationId`、`compute`、`configInputs`、`credentials`、`dependencies`、`entryPort`、`entrypoint`、`execution`、`exposurePolicy`、`healthChecks`、`image`、`persistentMounts`、`platform`、`ports`、`schemaVersion`、`scratchMounts`、`secretInputs`、`version`
- `WorkspaceApplicationSecretInput`：`env`、`name`、`target`

**数据表及写入Owner**：`build.idempotency_records`、`build.inbox_events`、`build.operations`、`build.outbox_deliveries`、`build.outbox_events`、`capability.capability_versions`、`capability.idempotency_records`、`capability.inbox_events`、`capability.operations`、`capability.outbox_deliveries`、`capability.outbox_events`、`capability.reference_claims`、`capability.runtime_versions`、`capability.webui_versions`、`fabric.attachments`、`fabric.idempotency_records`、`fabric.inbox_events`、`fabric.operations`、`fabric.outbox_deliveries`、`fabric.outbox_events`、`fabric.resource_actions`、`fabric.route_bindings`、`fabric.route_switches`、`gateway.idempotency_records`、`gateway.inbox_events`、`gateway.operations`、`gateway.outbox_deliveries`、`gateway.outbox_events`、`ledger.idempotency_records`、`ledger.inbox_events`、`ledger.outbox_deliveries`、`ledger.outbox_events`、`ledger.receipts`、`resource_catalog.idempotency_records`、`resource_catalog.inbox_events`、`resource_catalog.operations`、`resource_catalog.outbox_deliveries`、`resource_catalog.outbox_events`、`runtime_control.idempotency_records`、`runtime_control.inbox_events`、`runtime_control.operations`、`runtime_control.outbox_deliveries`、`runtime_control.outbox_events`、`runtime_control.runtime_actions`、`runtime_control.runtime_instances`、`tenant.accepted_operation_grants`、`tenant.idempotency_records`、`tenant.inbox_events`、`tenant.operations`、`tenant.outbox_deliveries`、`tenant.outbox_events`、`workspace.deployments`、`workspace.idempotency_records`、`workspace.inbox_events`、`workspace.operations`、`workspace.outbox_deliveries`、`workspace.outbox_events`、`workspace.saga_steps`、`workspace.workspaces`

**验收向量**：

- **success**：目标运行验证和选中绑定切换确认后显示新版本；回滚读回确认后才显示“已恢复到所选版本”。
- **failure**：不兼容更新明确拒绝，不通过保留旧字段/复制数据目录硬凑兼容；回滚失败显示需要处理，不承诺自动恢复。
- **duplicate**：同一提交意图保留 Idempotency-Key；重发相同请求只显示原资源/操作，不新增副作用。修改输入产生新意图，不复用旧 key。
- **unauthorized**：缺少页面动作权限时不提供按钮；服务端仍独立授权。访问其他 Tenant 对象显示不存在，不暴露是否存在。
- **restart**：刷新、切页或登录恢复后从权威 GET 重建视图；异步命令以原 Operation 继续查询，页面不会重新发起命令。

跨域状态/恢复执行见06；输入约束、按钮条件和中文错误见04/07。涉及旧对象同时执行09迁移接受标准。

## F11 立即升级补差与下期降配计划

**页面**：`/console/workspaces/:workspaceId?tab=plan-changes`（套餐变更列表与当前生效计划）、`/console/workspaces/:workspaceId?tab=plan-changes&planChangeId=:planChangeId`（PlanChange详情与执行/取消结果）

| operationId | 请求 | 请求DTO | 成功响应DTO | 唯一Owner |
|---|---|---|---|---|
| `cancelPlanChange` | `POST /api/v2/workspaces/{workspaceId}/plan-changes/{planChangeId}/cancel` | CancelPlanChangeRequest | 202 Operation | workspace |
| `createQuote` | `POST /api/v2/quotes` | QuoteRequest | 201 Quote | resource_catalog |
| `getOperation` | `GET /api/v2/operations/{owner}/{operationId}` | — | 200 Operation | workspace |
| `getPlanChange` | `GET /api/v2/workspaces/{workspaceId}/plan-changes/{planChangeId}` | — | 200 PlanChange | workspace |
| `getQuote` | `GET /api/v2/quotes/{quoteId}` | — | 200 Quote | resource_catalog |
| `getSubscription` | `GET /api/v2/workspaces/{workspaceId}/subscription` | — | 200 Subscription | workspace |
| `getWorkspace` | `GET /api/v2/workspaces/{workspaceId}` | — | 200 Workspace | workspace |
| `getWorkspaceModels` | `GET /api/v2/workspaces/{workspaceId}/models` | — | 200 ModelConfiguration | workspace |
| `listComputePlans` | `GET /api/v2/catalog/compute-plans` | — | 200 ComputePlanPage | resource_catalog |
| `listPlanChanges` | `GET /api/v2/workspaces/{workspaceId}/plan-changes` | — | 200 PlanChangePage | workspace |
| `listStoragePlans` | `GET /api/v2/catalog/storage-plans` | — | 200 StoragePlanPage | resource_catalog |
| `listWorkspaceTransactions` | `GET /api/v2/workspaces/{workspaceId}/transactions` | — | 200 WalletOperationPage | gateway |
| `resizeWorkspace` | `POST /api/v2/workspaces/{workspaceId}/resize` | ApplyQuoteRequest | 202 Operation | workspace |
| `updateRenewalSettings` | `PUT /api/v2/workspaces/{workspaceId}/subscription/renewal` | UpdateRenewalSettingsRequest | 202 Operation | workspace |

**显示字段来源**（精确Schema.field，包含正常/处理中/失败页字段；可选性按03）：

- `ComputePlan`：`availability`、`billingMode`、`createdAt`、`id`、`memoryMiB`、`monthlyPriceUSDMicros`、`name`、`pricePolicyVersionId`、`providerCapabilityVersion`、`validFrom`、`validUntil`、`vcpus`
- `CreditSource`：`amountUSDMicros`、`creditReceiptId`、`originalSubscriptionPeriodId`、`originalWalletOperationId`、`policyVersionId`
- `ModelConfiguration`：`appliedVersion`、`operationId`、`selections`、`status`、`updatedAt`、`version`、`workspaceId`
- `ModelSelection`：`modelId`、`slot`
- `NextPeriodPlanQuote`：`periodEnd`、`periodStart`、`pricePolicyVersionId`、`totalUSDMicros`
- `Operation`：`createdAt`、`errorCode`、`kind`、`observationResult`、`operationId`、`owner`、`pollAfterSeconds`、`requestId`、`resourceId`、`stage`、`status`、`updatedAt`
- `PlanChange`：`appliedAt`、`cancellable`、`cancellationOperationId`、`chargeOperationId`、`chargeStatus`、`chargeUSDMicros`、`createdAt`、`currentRequirementValidation`、`deliveryOutcome`、`errorCode`、`executionOperationId`、`executionPlanDigest`、`executionPlanId`、`id`、`kind`、`lastValidatedAt`、`nextPeriodChargeOperationId`、`nextPeriodChargeStatus`、`nextPeriodChargeUSDMicros`、`nextPeriodEnd`、`nextPeriodObligationId`、`nextPeriodStart`、`observationResult`、`operationId`、`periodEnd`、`periodEndMilliseconds`、`periodStart`、`periodStartMilliseconds`、`plannedEffectiveAt`、`policyVersion`、`quoteAt`、`quoteAtMilliseconds`、`quoteId`、`refundOperationIds`、`resourceOutcome`、`riskCode`、`runtimeReadbackRequirement`、`scheduleVersion`、`sourceComputePlanId`、`sourceFinancialSnapshotDigest`、`sourceMonthlyUSDMicros`、`sourcePeriodId`、`sourcePricePolicyVersionId`、`sourceStoragePlanId`、`sourceSubscriptionId`、`sourceSubscriptionVersion`、`status`、`targetComputePlanId`、`targetMonthlyUSDMicros`、`targetPricePolicyVersionId`、`targetStoragePlanId`、`updatedAt`、`workspaceId`
- `PlanChangeCalculation`：`chargeUSDMicros`、`executionPlanDigest`、`executionPlanId`、`kind`、`nextPeriod`、`periodEnd`、`periodEndMilliseconds`、`periodStart`、`periodStartMilliseconds`、`plannedEffectiveAt`、`policyVersion`、`quoteAt`、`quoteAtMilliseconds`、`sourceComputePlanId`、`sourceFinancialSnapshotDigest`、`sourceMonthlyUSDMicros`、`sourcePeriodId`、`sourcePricePolicyVersionId`、`sourceStoragePlanId`、`sourceSubscriptionId`、`sourceSubscriptionVersion`、`targetComputePlanId`、`targetMonthlyUSDMicros`、`targetPricePolicyVersionId`、`targetStoragePlanId`、`transitionId`、`upgradeProration`
- `Quote`：`capabilityVersionId`、`computePlanId`、`createdAt`、`expectedInterruption`、`expiresAt`、`id`、`lineItems`、`modelSelections`、`periodEnd`、`periodMonths`、`periodStart`、`planChangeCalculation`、`pricePolicyVersionId`、`purpose`、`refundPolicyVersionId`、`refundTerms`、`retentionPolicyVersionId`、`retentionTerms`、`runtimeReadbackRequirement`、`scheduledPlanChangeId`、`sourceSubscriptionVersion`、`status`、`storagePlanId`、`totalUSDMicros`、`workspaceId`
- `QuoteLine`：`amountUSDMicros`、`creditSource`、`description`、`kind`、`quantity`
- `StoragePlan`：`availability`、`billingMode`、`capacityGiB`、`createdAt`、`id`、`monthlyPriceUSDMicros`、`name`、`pricePolicyVersionId`、`shrinkSupported`、`validFrom`、`validUntil`
- `Subscription`：`acceptedQuoteId`、`createdAt`、`currentMonthlyUSDMicros`、`currentPeriodEnd`、`currentPeriodStart`、`currentPricePolicyVersionId`、`id`、`legacyPurchaseId`、`periodMonths`、`provenance`、`refundPolicyVersionId`、`refundTerms`、`renewalConsentId`、`renewalMode`、`renewalOperationId`、`renewalSettingsVersion`、`retentionPolicyVersionId`、`retentionTerms`、`status`、`version`、`workspaceId`
- `UpgradeProration`：`periodMilliseconds`、`priceDeltaUSDMicros`、`remainingMilliseconds`、`rounding`
- `WalletOperation`：`amountUSDMicros`、`coverageEnd`、`coverageEndMilliseconds`、`coverageStart`、`coverageStartMilliseconds`、`createdAt`、`errorCode`、`externalReference`、`id`、`kind`、`originalChargeOperationId`、`planChangeId`、`purpose`、`receiptId`、`status`、`updatedAt`、`workspaceId`
- `Workspace`：`accessUrl`、`activeDeploymentId`、`applicationAvailability`、`capabilityVersionId`、`computePlanId`、`createdAt`、`currentPeriodEnd`、`deliveryModel`、`id`、`modelConfigurationVersion`、`name`、`resourceReadiness`、`status`、`storagePlanId`、`updatedAt`、`version`

**数据表及写入Owner**：`build.idempotency_records`、`build.inbox_events`、`build.operations`、`build.outbox_deliveries`、`build.outbox_events`、`capability.idempotency_records`、`capability.inbox_events`、`capability.operations`、`capability.outbox_deliveries`、`capability.outbox_events`、`fabric.idempotency_records`、`fabric.inbox_events`、`fabric.operations`、`fabric.outbox_deliveries`、`fabric.outbox_events`、`fabric.resource_actions`、`fabric.resource_sets`、`fabric.resources`、`gateway.idempotency_records`、`gateway.inbox_events`、`gateway.operations`、`gateway.outbox_deliveries`、`gateway.outbox_events`、`gateway.wallet_operations`、`ledger.idempotency_records`、`ledger.inbox_events`、`ledger.outbox_deliveries`、`ledger.outbox_events`、`ledger.receipts`、`resource_catalog.compute_plans`、`resource_catalog.idempotency_records`、`resource_catalog.inbox_events`、`resource_catalog.operations`、`resource_catalog.outbox_deliveries`、`resource_catalog.outbox_events`、`resource_catalog.price_policy_versions`、`resource_catalog.quote_items`、`resource_catalog.quotes`、`resource_catalog.storage_plans`、`runtime_control.idempotency_records`、`runtime_control.inbox_events`、`runtime_control.operations`、`runtime_control.outbox_deliveries`、`runtime_control.outbox_events`、`tenant.accepted_operation_grants`、`tenant.idempotency_records`、`tenant.inbox_events`、`tenant.operations`、`tenant.outbox_deliveries`、`tenant.outbox_events`、`workspace.idempotency_records`、`workspace.inbox_events`、`workspace.model_configurations`、`workspace.operations`、`workspace.outbox_deliveries`、`workspace.outbox_events`、`workspace.plan_changes`、`workspace.saga_steps`、`workspace.subscription_period_obligations`、`workspace.subscription_periods`、`workspace.subscriptions`、`workspace.supplemental_charges`、`workspace.workspaces`

**验收向量**：

- **success**：固定示例S=2026-09-01T00:00Z、E=2026-10-01T00:00Z、T=09-16T00:00Z，20→40美元/月剩半期补10；资源+Runtime确认才applied，E不变。降配当期不变不退款，E后按目标下期价与有效授权执行。
- **failure**：CBS缩容、混合/no-op/不支持转换明确拒绝。付款拒绝无资源动作；unknown原单不重发。确定失败按原补差全额补偿，实际不可逆资源差异独立needs_attention，不宣称原盘缩回。
- **duplicate**：重复quote/PlanChange接受返回同身份；补差原单/nextPeriodStart义务各只发一次。已有计划必须先显式取消再new，不能覆盖旧目标。
- **unauthorized**：修改限owner/admin，读按对象归属；自动续费需当前有效consent。手动无授权到E转awaiting_payment，停止未付款使用，不自动扣旧高价。
- **restart**：按PlanChange.id恢复；初次计划保存Operation与executionOperationId/cancellationOperationId分开。中断只读取原资金/资源action，不重购或重算已接受T。

跨域状态/恢复执行见06；输入约束、按钮条件和中文错误见04/07。涉及旧对象同时执行09迁移接受标准。

## F12 续费、到期停用与恢复

**页面**：`/console/billing`（订阅与周期）、`/console/workspaces/:workspaceId/billing`（续费设置与结果）

| operationId | 请求 | 请求DTO | 成功响应DTO | 唯一Owner |
|---|---|---|---|---|
| `createQuote` | `POST /api/v2/quotes` | QuoteRequest | 201 Quote | resource_catalog |
| `getOperation` | `GET /api/v2/operations/{owner}/{operationId}` | — | 200 Operation | workspace |
| `getQuote` | `GET /api/v2/quotes/{quoteId}` | — | 200 Quote | resource_catalog |
| `getSubscription` | `GET /api/v2/workspaces/{workspaceId}/subscription` | — | 200 Subscription | workspace |
| `getWallet` | `GET /api/v2/wallet` | — | 200 Wallet | gateway |
| `getWorkspace` | `GET /api/v2/workspaces/{workspaceId}` | — | 200 Workspace | workspace |
| `listWorkspaceTransactions` | `GET /api/v2/workspaces/{workspaceId}/transactions` | — | 200 WalletOperationPage | gateway |
| `renewWorkspace` | `POST /api/v2/workspaces/{workspaceId}/renew` | ApplyQuoteRequest | 202 Operation | workspace |
| `updateRenewalSettings` | `PUT /api/v2/workspaces/{workspaceId}/subscription/renewal` | UpdateRenewalSettingsRequest | 202 Operation | workspace |

**显示字段来源**（精确Schema.field，包含正常/处理中/失败页字段；可选性按03）：

- `CreditSource`：`amountUSDMicros`、`creditReceiptId`、`originalSubscriptionPeriodId`、`originalWalletOperationId`、`policyVersionId`
- `ModelSelection`：`modelId`、`slot`
- `NextPeriodPlanQuote`：`periodEnd`、`periodStart`、`pricePolicyVersionId`、`totalUSDMicros`
- `Operation`：`createdAt`、`errorCode`、`kind`、`observationResult`、`operationId`、`owner`、`pollAfterSeconds`、`requestId`、`resourceId`、`stage`、`status`、`updatedAt`
- `PlanChangeCalculation`：`chargeUSDMicros`、`executionPlanDigest`、`executionPlanId`、`kind`、`nextPeriod`、`periodEnd`、`periodEndMilliseconds`、`periodStart`、`periodStartMilliseconds`、`plannedEffectiveAt`、`policyVersion`、`quoteAt`、`quoteAtMilliseconds`、`sourceComputePlanId`、`sourceFinancialSnapshotDigest`、`sourceMonthlyUSDMicros`、`sourcePeriodId`、`sourcePricePolicyVersionId`、`sourceStoragePlanId`、`sourceSubscriptionId`、`sourceSubscriptionVersion`、`targetComputePlanId`、`targetMonthlyUSDMicros`、`targetPricePolicyVersionId`、`targetStoragePlanId`、`transitionId`、`upgradeProration`
- `Quote`：`capabilityVersionId`、`computePlanId`、`createdAt`、`expectedInterruption`、`expiresAt`、`id`、`lineItems`、`modelSelections`、`periodEnd`、`periodMonths`、`periodStart`、`planChangeCalculation`、`pricePolicyVersionId`、`purpose`、`refundPolicyVersionId`、`refundTerms`、`retentionPolicyVersionId`、`retentionTerms`、`runtimeReadbackRequirement`、`scheduledPlanChangeId`、`sourceSubscriptionVersion`、`status`、`storagePlanId`、`totalUSDMicros`、`workspaceId`
- `QuoteLine`：`amountUSDMicros`、`creditSource`、`description`、`kind`、`quantity`
- `Subscription`：`acceptedQuoteId`、`createdAt`、`currentMonthlyUSDMicros`、`currentPeriodEnd`、`currentPeriodStart`、`currentPricePolicyVersionId`、`id`、`legacyPurchaseId`、`periodMonths`、`provenance`、`refundPolicyVersionId`、`refundTerms`、`renewalConsentId`、`renewalMode`、`renewalOperationId`、`renewalSettingsVersion`、`retentionPolicyVersionId`、`retentionTerms`、`status`、`version`、`workspaceId`
- `UpgradeProration`：`periodMilliseconds`、`priceDeltaUSDMicros`、`remainingMilliseconds`、`rounding`
- `Wallet`：`balanceUSDMicros`、`currency`、`fetchedAt`、`source`、`status`
- `WalletOperation`：`amountUSDMicros`、`coverageEnd`、`coverageEndMilliseconds`、`coverageStart`、`coverageStartMilliseconds`、`createdAt`、`errorCode`、`externalReference`、`id`、`kind`、`originalChargeOperationId`、`planChangeId`、`purpose`、`receiptId`、`status`、`updatedAt`、`workspaceId`
- `Workspace`：`accessUrl`、`activeDeploymentId`、`applicationAvailability`、`capabilityVersionId`、`computePlanId`、`createdAt`、`currentPeriodEnd`、`deliveryModel`、`id`、`modelConfigurationVersion`、`name`、`resourceReadiness`、`status`、`storagePlanId`、`updatedAt`、`version`

**数据表及写入Owner**：`build.idempotency_records`、`build.inbox_events`、`build.operations`、`build.outbox_deliveries`、`build.outbox_events`、`capability.idempotency_records`、`capability.inbox_events`、`capability.operations`、`capability.outbox_deliveries`、`capability.outbox_events`、`fabric.idempotency_records`、`fabric.inbox_events`、`fabric.operations`、`fabric.outbox_deliveries`、`fabric.outbox_events`、`fabric.resource_actions`、`fabric.resource_sets`、`fabric.resources`、`gateway.idempotency_records`、`gateway.inbox_events`、`gateway.operations`、`gateway.outbox_deliveries`、`gateway.outbox_events`、`gateway.wallet_operations`、`ledger.idempotency_records`、`ledger.inbox_events`、`ledger.outbox_deliveries`、`ledger.outbox_events`、`ledger.receipts`、`ledger.reconciliations`、`resource_catalog.compute_plans`、`resource_catalog.idempotency_records`、`resource_catalog.inbox_events`、`resource_catalog.operations`、`resource_catalog.outbox_deliveries`、`resource_catalog.outbox_events`、`resource_catalog.price_policy_versions`、`resource_catalog.quote_items`、`resource_catalog.quotes`、`resource_catalog.storage_plans`、`runtime_control.idempotency_records`、`runtime_control.inbox_events`、`runtime_control.operations`、`runtime_control.outbox_deliveries`、`runtime_control.outbox_events`、`runtime_control.runtime_actions`、`runtime_control.runtime_instances`、`tenant.accepted_operation_grants`、`tenant.idempotency_records`、`tenant.inbox_events`、`tenant.operations`、`tenant.outbox_deliveries`、`tenant.outbox_events`、`workspace.idempotency_records`、`workspace.inbox_events`、`workspace.operations`、`workspace.outbox_deliveries`、`workspace.outbox_events`、`workspace.plan_changes`、`workspace.saga_steps`、`workspace.subscription_period_obligations`、`workspace.subscription_periods`、`workspace.subscriptions`、`workspace.workspaces`

**验收向量**：

- **success**：付款确认、周期推进和真实运行恢复分别显示；只有全链完成才恢复“可打开”。
- **failure**：支付unknown只核对原交易；资源已删除时明确不能通过续费复原，不承诺备份或重新购买。
- **duplicate**：同一提交意图保留 Idempotency-Key；重发相同请求只显示原资源/操作，不新增副作用。修改输入产生新意图，不复用旧 key。
- **unauthorized**：缺少页面动作权限时不提供按钮；服务端仍独立授权。访问其他 Tenant 对象显示不存在，不暴露是否存在。
- **restart**：刷新、切页或登录恢复后从权威 GET 重建视图；异步命令以原 Operation 继续查询，页面不会重新发起命令。

跨域状态/恢复执行见06；输入约束、按钮条件和中文错误见04/07。涉及旧对象同时执行09迁移接受标准。

## F13 删除Workspace、资源确认与退款

**页面**：`/console/workspaces/:workspaceId/settings`（危险操作与删除结果）

| operationId | 请求 | 请求DTO | 成功响应DTO | 唯一Owner |
|---|---|---|---|---|
| `deleteWorkspace` | `DELETE /api/v2/workspaces/{workspaceId}` | DeleteWorkspaceRequest | 202 Operation | workspace |
| `getOperation` | `GET /api/v2/operations/{owner}/{operationId}` | — | 200 Operation | workspace |
| `getQuote` | `GET /api/v2/quotes/{quoteId}` | — | 200 Quote | resource_catalog |
| `getSubscription` | `GET /api/v2/workspaces/{workspaceId}/subscription` | — | 200 Subscription | workspace |
| `getWorkspace` | `GET /api/v2/workspaces/{workspaceId}` | — | 200 Workspace | workspace |
| `getWorkspaceDeletion` | `GET /api/v2/workspaces/{workspaceId}/deletion` | — | 200 WorkspaceDeletion | workspace |
| `listWorkspaceTransactions` | `GET /api/v2/workspaces/{workspaceId}/transactions` | — | 200 WalletOperationPage | gateway |

**显示字段来源**（精确Schema.field，包含正常/处理中/失败页字段；可选性按03）：

- `CreditSource`：`amountUSDMicros`、`creditReceiptId`、`originalSubscriptionPeriodId`、`originalWalletOperationId`、`policyVersionId`
- `ModelSelection`：`modelId`、`slot`
- `NextPeriodPlanQuote`：`periodEnd`、`periodStart`、`pricePolicyVersionId`、`totalUSDMicros`
- `Operation`：`createdAt`、`errorCode`、`kind`、`observationResult`、`operationId`、`owner`、`pollAfterSeconds`、`requestId`、`resourceId`、`stage`、`status`、`updatedAt`
- `PlanChangeCalculation`：`chargeUSDMicros`、`executionPlanDigest`、`executionPlanId`、`kind`、`nextPeriod`、`periodEnd`、`periodEndMilliseconds`、`periodStart`、`periodStartMilliseconds`、`plannedEffectiveAt`、`policyVersion`、`quoteAt`、`quoteAtMilliseconds`、`sourceComputePlanId`、`sourceFinancialSnapshotDigest`、`sourceMonthlyUSDMicros`、`sourcePeriodId`、`sourcePricePolicyVersionId`、`sourceStoragePlanId`、`sourceSubscriptionId`、`sourceSubscriptionVersion`、`targetComputePlanId`、`targetMonthlyUSDMicros`、`targetPricePolicyVersionId`、`targetStoragePlanId`、`transitionId`、`upgradeProration`
- `Quote`：`capabilityVersionId`、`computePlanId`、`createdAt`、`expectedInterruption`、`expiresAt`、`id`、`lineItems`、`modelSelections`、`periodEnd`、`periodMonths`、`periodStart`、`planChangeCalculation`、`pricePolicyVersionId`、`purpose`、`refundPolicyVersionId`、`refundTerms`、`retentionPolicyVersionId`、`retentionTerms`、`runtimeReadbackRequirement`、`scheduledPlanChangeId`、`sourceSubscriptionVersion`、`status`、`storagePlanId`、`totalUSDMicros`、`workspaceId`
- `QuoteLine`：`amountUSDMicros`、`creditSource`、`description`、`kind`、`quantity`
- `Subscription`：`acceptedQuoteId`、`createdAt`、`currentMonthlyUSDMicros`、`currentPeriodEnd`、`currentPeriodStart`、`currentPricePolicyVersionId`、`id`、`legacyPurchaseId`、`periodMonths`、`provenance`、`refundPolicyVersionId`、`refundTerms`、`renewalConsentId`、`renewalMode`、`renewalOperationId`、`renewalSettingsVersion`、`retentionPolicyVersionId`、`retentionTerms`、`status`、`version`、`workspaceId`
- `UpgradeProration`：`periodMilliseconds`、`priceDeltaUSDMicros`、`remainingMilliseconds`、`rounding`
- `WalletOperation`：`amountUSDMicros`、`coverageEnd`、`coverageEndMilliseconds`、`coverageStart`、`coverageStartMilliseconds`、`createdAt`、`errorCode`、`externalReference`、`id`、`kind`、`originalChargeOperationId`、`planChangeId`、`purpose`、`receiptId`、`status`、`updatedAt`、`workspaceId`
- `Workspace`：`accessUrl`、`activeDeploymentId`、`applicationAvailability`、`capabilityVersionId`、`computePlanId`、`createdAt`、`currentPeriodEnd`、`deliveryModel`、`id`、`modelConfigurationVersion`、`name`、`resourceReadiness`、`status`、`storagePlanId`、`updatedAt`、`version`
- `WorkspaceDeletion`：`dataDeletionStatus`、`operationId`、`refundOperationId`、`refundStatus`、`resourceDeletionStatus`、`updatedAt`、`workspaceId`

**数据表及写入Owner**：`build.idempotency_records`、`build.inbox_events`、`build.operations`、`build.outbox_deliveries`、`build.outbox_events`、`capability.idempotency_records`、`capability.inbox_events`、`capability.operations`、`capability.outbox_deliveries`、`capability.outbox_events`、`capability.reference_claims`、`fabric.attachments`、`fabric.idempotency_records`、`fabric.inbox_events`、`fabric.operations`、`fabric.outbox_deliveries`、`fabric.outbox_events`、`fabric.resource_actions`、`fabric.resource_sets`、`fabric.resources`、`fabric.route_bindings`、`fabric.route_switches`、`gateway.idempotency_records`、`gateway.inbox_events`、`gateway.key_bindings`、`gateway.operations`、`gateway.outbox_deliveries`、`gateway.outbox_events`、`gateway.wallet_operations`、`ledger.idempotency_records`、`ledger.inbox_events`、`ledger.outbox_deliveries`、`ledger.outbox_events`、`ledger.receipts`、`ledger.reconciliations`、`resource_catalog.idempotency_records`、`resource_catalog.inbox_events`、`resource_catalog.operations`、`resource_catalog.outbox_deliveries`、`resource_catalog.outbox_events`、`resource_catalog.quote_items`、`resource_catalog.quotes`、`resource_catalog.refund_policy_versions`、`resource_catalog.retention_policy_versions`、`runtime_control.idempotency_records`、`runtime_control.inbox_events`、`runtime_control.operations`、`runtime_control.outbox_deliveries`、`runtime_control.outbox_events`、`runtime_control.runtime_actions`、`runtime_control.runtime_instances`、`tenant.accepted_operation_grants`、`tenant.idempotency_records`、`tenant.inbox_events`、`tenant.operations`、`tenant.outbox_deliveries`、`tenant.outbox_events`、`workspace.idempotency_records`、`workspace.inbox_events`、`workspace.operations`、`workspace.outbox_deliveries`、`workspace.outbox_events`、`workspace.plan_changes`、`workspace.saga_steps`、`workspace.subscription_period_obligations`、`workspace.subscriptions`、`workspace.supplemental_charges`、`workspace.workspaces`

**验收向量**：

- **success**：资源确认删除与退款confirmed分别取得证据；无退款权益时显示政策实际结果，而不是统一承诺退款或不退款。
- **failure**：资源结果unknown时显示“资源删除结果待核实”，不宣称已销毁也不提前退款。退款unknown显示“退款结果待核实”，禁止再次点击触发同款。
- **duplicate**：同一提交意图保留 Idempotency-Key；重发相同请求只显示原资源/操作，不新增副作用。修改输入产生新意图，不复用旧 key。
- **unauthorized**：缺少页面动作权限时不提供按钮；服务端仍独立授权。访问其他 Tenant 对象显示不存在，不暴露是否存在。
- **restart**：刷新、切页或登录恢复后从权威 GET 重建视图；异步命令以原 Operation 继续查询，页面不会重新发起命令。

跨域状态/恢复执行见06；输入约束、按钮条件和中文错误见04/07。涉及旧对象同时执行09迁移接受标准。

## F14 钱包、用量、Key及管理员充值记录

**页面**：`/console/api`（服务与钱包）、`/console/api/usage`（模型用量）、`/console/api/keys`（API密钥）、`/admin/recharge-records`（充值记录）

| operationId | 请求 | 请求DTO | 成功响应DTO | 唯一Owner |
|---|---|---|---|---|
| `createGatewayKey` | `POST /api/v2/gateway-keys` | CreateGatewayKeyRequest | 201 GatewayKeySecret | gateway |
| `getWallet` | `GET /api/v2/wallet` | — | 200 Wallet | gateway |
| `listGatewayKeys` | `GET /api/v2/gateway-keys` | — | 200 GatewayKeyPage | gateway |
| `listRechargeRecords` | `GET /api/v2/admin/recharge-records` | — | 200 WalletOperationPage | gateway |
| `listUsage` | `GET /api/v2/usage` | — | 200 UsagePage | gateway |
| `listWorkspaceTransactions` | `GET /api/v2/workspaces/{workspaceId}/transactions` | — | 200 WalletOperationPage | gateway |
| `revealGatewayKey` | `POST /api/v2/gateway-keys/{keyId}/reveal` | — | 200 GatewayKeySecret | gateway |
| `revokeGatewayKey` | `POST /api/v2/gateway-keys/{keyId}/revoke` | — | 202 Operation | gateway |

**显示字段来源**（精确Schema.field，包含正常/处理中/失败页字段；可选性按03）：

- `GatewayKey`：`createdAt`、`expiresAt`、`fingerprint`、`id`、`modelIds`、`name`、`purpose`、`status`、`workspaceId`
- `GatewayKeySecret`：`key`、`secret`
- `Operation`：`createdAt`、`errorCode`、`kind`、`observationResult`、`operationId`、`owner`、`pollAfterSeconds`、`requestId`、`resourceId`、`stage`、`status`、`updatedAt`
- `Usage`：`costUSDMicros`、`createdAt`、`id`、`inputTokens`、`modelId`、`outputTokens`、`periodEnd`、`periodStart`、`source`
- `Wallet`：`balanceUSDMicros`、`currency`、`fetchedAt`、`source`、`status`
- `WalletOperation`：`amountUSDMicros`、`coverageEnd`、`coverageEndMilliseconds`、`coverageStart`、`coverageStartMilliseconds`、`createdAt`、`errorCode`、`externalReference`、`id`、`kind`、`originalChargeOperationId`、`planChangeId`、`purpose`、`receiptId`、`status`、`updatedAt`、`workspaceId`

**数据表及写入Owner**：`build.idempotency_records`、`build.operations`、`capability.idempotency_records`、`capability.operations`、`fabric.idempotency_records`、`fabric.operations`、`fabric.secret_bindings`、`gateway.idempotency_records`、`gateway.identity_mappings`、`gateway.key_bindings`、`gateway.operations`、`gateway.tenant_wallet_bindings`、`gateway.wallet_operations`、`ledger.idempotency_records`、`ledger.receipts`、`resource_catalog.idempotency_records`、`resource_catalog.operations`、`runtime_control.idempotency_records`、`runtime_control.operations`、`tenant.idempotency_records`、`tenant.operations`、`workspace.idempotency_records`、`workspace.operations`

**验收向量**：

- **success**：金额/用量来自权威读回；管理员充值列表仅在confirmed记录存在时显示已到账，不把记录读取当执行充值。
- **failure**：上游不可用显示“余额暂时无法读取”，不显示0或缓存余额为实时；没有权限时不提供reveal。
- **duplicate**：同一提交意图保留 Idempotency-Key；重发相同请求只显示原资源/操作，不新增副作用。修改输入产生新意图，不复用旧 key。
- **unauthorized**：缺少页面动作权限时不提供按钮；服务端仍独立授权。访问其他 Tenant 对象显示不存在，不暴露是否存在。
- **restart**：刷新、切页或登录恢复后从权威 GET 重建视图；异步命令以原 Operation 继续查询，页面不会重新发起命令。

跨域状态/恢复执行见06；输入约束、按钮条件和中文错误见04/07。涉及旧对象同时执行09迁移接受标准。

## F15 Tenant停用、删除与窗口内恢复

**页面**：`/admin/tenants/:tenantId`（Tenant访问/子操作/托管）

| operationId | 请求 | 请求DTO | 成功响应DTO | 唯一Owner |
|---|---|---|---|---|
| `bindTenantWallet` | `PUT /api/v2/admin/tenants/{tenantId}/wallet-binding` | BindTenantWalletRequest | 202 Operation | tenant |
| `createTenant` | `POST /api/v2/admin/tenants` | CreateTenantRequest | 202 Operation | tenant |
| `deleteTenant` | `DELETE /api/v2/admin/tenants/{tenantId}` | DeleteTenantRequest | 202 Operation | tenant |
| `getAdminTenant` | `GET /api/v2/admin/tenants/{tenantId}` | — | 200 Tenant | tenant |
| `getOperation` | `GET /api/v2/operations/{owner}/{operationId}` | — | 200 Operation | workspace |
| `getTenant` | `GET /api/v2/tenant` | — | 200 Tenant | tenant |
| `getTenantAssetCustody` | `GET /api/v2/admin/tenants/{tenantId}/asset-custody` | — | 200 AssetCustody | tenant |
| `getTenantLifecycleOperation` | `GET /api/v2/admin/tenants/{tenantId}/operations/{operationId}` | — | 200 TenantLifecycleProgress | tenant |
| `listTenants` | `GET /api/v2/admin/tenants` | — | 200 TenantPage | tenant |
| `reenableTenant` | `POST /api/v2/admin/tenants/{tenantId}/reenable` | ReenableTenantRequest | 202 Operation | tenant |
| `restoreTenant` | `POST /api/v2/admin/tenants/{tenantId}/restore` | TenantActionRequest | 202 Operation | tenant |
| `suspendTenant` | `POST /api/v2/admin/tenants/{tenantId}/suspend` | TenantActionRequest | 202 Operation | tenant |

**显示字段来源**（精确Schema.field，包含正常/处理中/失败页字段；可选性按03）：

- `AssetCustody`：`buildCount`、`packageCount`、`restoreUntil`、`status`、`tenantId`、`workspaceDeletionOperationIds`
- `Operation`：`createdAt`、`errorCode`、`kind`、`observationResult`、`operationId`、`owner`、`pollAfterSeconds`、`requestId`、`resourceId`、`stage`、`status`、`updatedAt`
- `Tenant`：`assetCustodyStatus`、`billingSub2apiUserId`、`createdAt`、`id`、`name`、`restoreUntil`、`status`、`updatedAt`
- `TenantLifecycleProgress`：`accessStatus`、`operation`、`skipped`、`tenantId`、`workspaceActions`
- `TenantWorkspaceAction`：`action`、`operationId`、`operationOwner`、`status`、`workspaceId`
- `TenantWorkspaceSkip`：`reason`、`workspaceId`

**数据表及写入Owner**：`build.build_jobs`、`build.idempotency_records`、`build.inbox_events`、`build.operations`、`build.outbox_deliveries`、`build.outbox_events`、`capability.idempotency_records`、`capability.inbox_events`、`capability.namespaces`、`capability.operations`、`capability.outbox_deliveries`、`capability.outbox_events`、`capability.packages`、`capability.reference_claims`、`fabric.idempotency_records`、`fabric.inbox_events`、`fabric.operations`、`fabric.outbox_deliveries`、`fabric.outbox_events`、`gateway.idempotency_records`、`gateway.inbox_events`、`gateway.operations`、`gateway.outbox_deliveries`、`gateway.outbox_events`、`gateway.tenant_wallet_bindings`、`ledger.idempotency_records`、`ledger.inbox_events`、`ledger.outbox_deliveries`、`ledger.outbox_events`、`ledger.receipts`、`resource_catalog.idempotency_records`、`resource_catalog.inbox_events`、`resource_catalog.operations`、`resource_catalog.outbox_deliveries`、`resource_catalog.outbox_events`、`resource_catalog.refund_policy_versions`、`resource_catalog.retention_policy_versions`、`runtime_control.idempotency_records`、`runtime_control.inbox_events`、`runtime_control.operations`、`runtime_control.outbox_deliveries`、`runtime_control.outbox_events`、`tenant.accepted_operation_grants`、`tenant.audit_events`、`tenant.authorization_contexts`、`tenant.idempotency_records`、`tenant.inbox_events`、`tenant.operations`、`tenant.outbox_deliveries`、`tenant.outbox_events`、`tenant.sessions`、`tenant.tenant_members`、`tenant.tenants`、`workspace.idempotency_records`、`workspace.inbox_events`、`workspace.operations`、`workspace.outbox_deliveries`、`workspace.outbox_events`、`workspace.saga_steps`

**验收向量**：

- **success**：reenable只恢复原Tenant暂停的合格子环境并独立显示结果；restore只恢复deleted窗口内身份/保留资产，不复活已删CVM/CBS。
- **failure**：窗口结束明确不可恢复；部分Workspace仍待核对时显示独立任务，不把Tenant状态代替资源删除结果。
- **duplicate**：同一提交意图保留 Idempotency-Key；重发相同请求只显示原资源/操作，不新增副作用。修改输入产生新意图，不复用旧 key。
- **unauthorized**：缺少页面动作权限时不提供按钮；服务端仍独立授权。访问其他 Tenant 对象显示不存在，不暴露是否存在。
- **restart**：刷新、切页或登录恢复后从权威 GET 重建视图；异步命令以原 Operation 继续查询，页面不会重新发起命令。

跨域状态/恢复执行见06；输入约束、按钮条件和中文错误见04/07。涉及旧对象同时执行09迁移接受标准。

## F16 旧资源与应用迁移后的可见状态和采用Agent

**页面**：`/console/workspaces/:workspaceId`（旧环境详情）、`/console/workspaces/:workspaceId/adopt`（在已有资源采用Agent）

| operationId | 请求 | 请求DTO | 成功响应DTO | 唯一Owner |
|---|---|---|---|---|
| `adoptWorkspace` | `POST /api/v2/workspaces/{workspaceId}/adopt` | AdoptWorkspaceRequest | 202 Operation | workspace |
| `getCapabilityVersion` | `GET /api/v2/capability-versions/{capabilityVersionId}` | — | 200 CapabilityVersion | capability |
| `getDeployment` | `GET /api/v2/workspaces/{workspaceId}/deployments/{deploymentId}` | — | 200 Deployment | workspace |
| `getOperation` | `GET /api/v2/operations/{owner}/{operationId}` | — | 200 Operation | workspace |
| `getSubscription` | `GET /api/v2/workspaces/{workspaceId}/subscription` | — | 200 Subscription | workspace |
| `getWorkspace` | `GET /api/v2/workspaces/{workspaceId}` | — | 200 Workspace | workspace |
| `listCapabilityVersions` | `GET /api/v2/capability-versions` | — | 200 CapabilityVersionPage | capability |
| `listDeployments` | `GET /api/v2/workspaces/{workspaceId}/deployments` | — | 200 DeploymentPage | workspace |
| `listModels` | `GET /api/v2/catalog/models` | — | 200 ModelPage | gateway |
| `listWorkspaceTransactions` | `GET /api/v2/workspaces/{workspaceId}/transactions` | — | 200 WalletOperationPage | gateway |

**显示字段来源**（精确Schema.field，包含正常/处理中/失败页字段；可选性按03）：

- `AnonymousAccessContract`：`mode`
- `ApplicationOwnedAccessContract`：`loginPath`、`logoutPath`、`mode`、`usernameCredentialName`
- `ArtifactReference`：`digest`、`platform`、`repository`
- `BuildRecipeContract`：`frontend`、`networkPolicy`、`outputImageCommand`、`outputPlatform`、`packageInput`、`recipe`、`runtimeContextName`、`version`、`webuiInput`
- `BuildRecipeContractOutputImageCommand`：`cmd`、`entrypoint`
- `CapabilityVersion`：`artifact`、`artifactDigest`、`buildJobId`、`createdAt`、`dataCompatibility`、`deploymentDescriptor`、`deploymentDescriptorDigest`、`deploymentDescriptorObjectRef`、`id`、`legacyApplicationRevisionId`、`modelRequirements`、`packageId`、`packageVersionId`、`provenance`、`referenceCount`、`runtimeVersionId`、`status`、`versionLabel`、`webuiVersionId`
- `CloudPrivateAccessContract`：`admissionReceiptId`、`entryContract`、`mode`
- `DataCompatibility`：`compatibleFromVersions`、`dataSchemaVersion`、`migrationReceiptId`、`migrationRequired`、`rollbackSafe`
- `DataContract`：`mountPolicies`、`rollback`、`schemaVersion`、`upgrade`
- `DataMountPolicy`：`concurrentWritersSupported`、`mountName`
- `DataRollbackContract`：`compatibleSchemaVersions`、`safe`
- `DataUpgradeContract`：`backupFormatVersion`、`backupRequired`、`compatibleFromSchemaVersions`、`migrationArtifact`、`mode`
- `Deployment`：`capabilityVersionId`、`createdAt`、`dataCompatibility`、`id`、`previousDeploymentId`、`runtimeInstanceId`、`status`、`updatedAt`、`workspaceId`
- `DeploymentDescriptor`：`applicationRevision`、`artifact`、`buildInputDigest`、`legacyApplicationRevisionId`、`packageVersionId`、`provenance`、`runtimeContract`、`runtimeContractReference`、`schemaVersion`、`webuiContract`、`webuiContractReference`
- `ImagePlatform`：`architecture`、`os`、`variant`
- `Model`：`available`、`capabilities`、`fetchedAt`、`id`、`inputPricePerMillionTokensUSDMicros`、`name`、`outputPricePerMillionTokensUSDMicros`、`priceSource`
- `ModelConfigurationContract`：`applyPath`、`authorizationSecretInputName`、`portName`、`protocol`、`readbackFields`、`readbackPath`、`requestFields`
- `ModelRequirement`：`allowedModelIds`、`capability`、`required`、`slot`
- `Operation`：`createdAt`、`errorCode`、`kind`、`observationResult`、`operationId`、`owner`、`pollAfterSeconds`、`requestId`、`resourceId`、`stage`、`status`、`updatedAt`
- `PackageBuildInput`：`contextName`、`formatVersion`、`gid`、`sourceRoot`、`targetPath`、`uid`
- `PackageFormatContractReference`：`admissionReceiptId`、`formatVersion`、`owner`、`schemaDigest`、`schemaObjectRef`、`validatorArtifact`
- `PublisherContractReference`：`descriptorDigest`、`descriptorObjectRef`、`kind`、`publisherNamespaceId`、`versionId`
- `RecipeArtifact`：`digest`、`dockerfilePath`、`mediaType`、`repository`
- `RuntimePublisherContract`：`applicationAccess`、`applicationRevisionTemplate`、`buildRecipe`、`data`、`image`、`kind`、`modelConfiguration`、`packageFormatContracts`、`packageFormatVersions`、`publisherNamespaceId`、`runtimeAbiVersion`、`schemaVersion`
- `Subscription`：`acceptedQuoteId`、`createdAt`、`currentMonthlyUSDMicros`、`currentPeriodEnd`、`currentPeriodStart`、`currentPricePolicyVersionId`、`id`、`legacyPurchaseId`、`periodMonths`、`provenance`、`refundPolicyVersionId`、`refundTerms`、`renewalConsentId`、`renewalMode`、`renewalOperationId`、`renewalSettingsVersion`、`retentionPolicyVersionId`、`retentionTerms`、`status`、`version`、`workspaceId`
- `WalletOperation`：`amountUSDMicros`、`coverageEnd`、`coverageEndMilliseconds`、`coverageStart`、`coverageStartMilliseconds`、`createdAt`、`errorCode`、`externalReference`、`id`、`kind`、`originalChargeOperationId`、`planChangeId`、`purpose`、`receiptId`、`status`、`updatedAt`、`workspaceId`
- `WebuiBuildInput`：`contextName`、`gid`、`sourcePath`、`targetPath`、`uid`
- `WebuiPublisherContract`：`apiBasePath`、`assetBasePath`、`authenticationProtocol`、`entryFile`、`image`、`integrationMode`、`kind`、`publisherNamespaceId`、`runtimeAbiVersions`、`schemaVersion`、`staticRoot`、`supportsSse`、`supportsWebsocket`、`uiProtocolVersion`
- `Workspace`：`accessUrl`、`activeDeploymentId`、`applicationAvailability`、`capabilityVersionId`、`computePlanId`、`createdAt`、`currentPeriodEnd`、`deliveryModel`、`id`、`modelConfigurationVersion`、`name`、`resourceReadiness`、`status`、`storagePlanId`、`updatedAt`、`version`
- `WorkspaceApplicationCompute`：`cpuLimitMilli`、`cpuRequestMilli`、`memoryLimitBytes`、`memoryRequestBytes`
- `WorkspaceApplicationConfigInput`：`name`、`target`
- `WorkspaceApplicationCredential`：`env`、`kind`、`name`、`target`、`username`
- `WorkspaceApplicationDependency`：`command`、`compute`、`configInputs`、`dependsOn`、`execution`、`healthChecks`、`image`、`name`、`persistentMounts`、`ports`、`scratchMounts`、`secretInputs`
- `WorkspaceApplicationDependencyCommand`：`args`、`entrypoint`、`env`
- `WorkspaceApplicationDependencyHealthCheck`：`command`、`initialDelaySeconds`、`path`、`port`、`type`
- `WorkspaceApplicationExecution`：`groupId`、`init`、`seccompProfile`、`userId`
- `WorkspaceApplicationHealthCheck`：`initialDelaySeconds`、`path`、`port`
- `WorkspaceApplicationMount`：`executable`、`groupId`、`mode`、`mountPath`、`name`、`readOnly`、`sizeBytes`、`userId`
- `WorkspaceApplicationPort`：`name`、`port`、`protocol`
- `WorkspaceApplicationRevision`：`applicationId`、`compute`、`configInputs`、`credentials`、`dependencies`、`entryPort`、`entrypoint`、`execution`、`exposurePolicy`、`healthChecks`、`image`、`persistentMounts`、`platform`、`ports`、`schemaVersion`、`scratchMounts`、`secretInputs`、`version`
- `WorkspaceApplicationSecretInput`：`env`、`name`、`target`

**数据表及写入Owner**：`build.idempotency_records`、`build.outbox_events`、`capability.capability_versions`、`capability.idempotency_records`、`capability.outbox_events`、`capability.reference_claims`、`fabric.idempotency_records`、`fabric.outbox_events`、`fabric.resource_sets`、`fabric.resources`、`gateway.idempotency_records`、`gateway.identity_mappings`、`gateway.key_bindings`、`gateway.outbox_events`、`gateway.tenant_wallet_bindings`、`gateway.wallet_operations`、`ledger.idempotency_records`、`ledger.outbox_events`、`ledger.receipts`、`ledger.reconciliations`、`resource_catalog.idempotency_records`、`resource_catalog.outbox_events`、`resource_catalog.quotes`、`runtime_control.idempotency_records`、`runtime_control.outbox_events`、`tenant.idempotency_records`、`tenant.outbox_events`、`tenant.tenants`、`workspace.deployments`、`workspace.idempotency_records`、`workspace.operations`、`workspace.outbox_events`、`workspace.saga_steps`、`workspace.subscription_periods`、`workspace.subscriptions`、`workspace.workspaces`

**验收向量**：

- **success**：已有资源采用Agent后activeDeploymentId及deliveryModel更新，历史账户/Workspace/资源/周期/交易/收据ID不变，无新增debit或资源订单。
- **failure**：无法证明历史关联或资源状态时显示需要处理；不猜旧数据绑定、不合成ready Runtime或Build。
- **duplicate**：相同采用命令同key返回同一Operation，不能再次部署/采购；保留原外部幂等键。
- **unauthorized**：历史Workspace同样按当前Tenant与角色授权；迁移不得放开旧ID的跨Tenant读取。
- **restart**：重启后从原资源/部署/义务身份读回同一操作；不把缺少新记录解释成重新开通需要。

跨域状态/恢复执行见06；输入约束、按钮条件和中文错误见04/07。涉及旧对象同时执行09迁移接受标准。

## F17 管理员操作、审计与实例资格读回

**页面**：`/admin/operations`（操作与审计）、`/admin/qualifications`（资格与发布证据）

| operationId | 请求 | 请求DTO | 成功响应DTO | 唯一Owner |
|---|---|---|---|---|
| `getOperation` | `GET /api/v2/operations/{owner}/{operationId}` | — | 200 Operation | workspace |
| `getReceipt` | `GET /api/v2/admin/receipts/{receiptId}` | — | 200 Receipt | ledger |
| `listAdminOperations` | `GET /api/v2/admin/operations` | — | 200 AdminOperationPage | workspace |
| `listAuditEvents` | `GET /api/v2/admin/audit-events` | — | 200 AuditEventPage | tenant |
| `listQualifications` | `GET /api/v2/admin/qualifications` | — | 200 QualificationPage | ledger |
| `listReceipts` | `GET /api/v2/admin/receipts` | — | 200 ReceiptPage | ledger |
| `reconcileOperation` | `POST /api/v2/admin/operations/{owner}/{operationId}/reconcile` | ReconcileOperationRequest | 202 Operation | workspace |

**显示字段来源**（精确Schema.field，包含正常/处理中/失败页字段；可选性按03）：

- `AdminOperation`：`operation`、`tenantId`
- `AuditEvent`：`action`、`actorId`、`createdAt`、`id`、`outcome`、`requestId`、`resourceId`
- `Operation`：`createdAt`、`errorCode`、`kind`、`observationResult`、`operationId`、`owner`、`pollAfterSeconds`、`requestId`、`resourceId`、`stage`、`status`、`updatedAt`
- `Qualification`：`artifactDigest`、`candidateSourceSha`、`createdAt`、`id`、`instanceReceiptId`、`localReceiptId`、`status`
- `Receipt`：`artifactDigest`、`createdAt`、`evidenceSummary`、`id`、`kind`、`operationId`、`outcome`、`owner`、`sourceSha`、`workflowRunId`

**数据表及写入Owner**：`build.build_artifacts`、`build.idempotency_records`、`build.inbox_events`、`build.operations`、`build.outbox_deliveries`、`build.outbox_events`、`capability.idempotency_records`、`capability.inbox_events`、`capability.operations`、`capability.outbox_deliveries`、`capability.outbox_events`、`fabric.idempotency_records`、`fabric.inbox_events`、`fabric.operations`、`fabric.outbox_deliveries`、`fabric.outbox_events`、`fabric.resource_actions`、`gateway.idempotency_records`、`gateway.inbox_events`、`gateway.operations`、`gateway.outbox_deliveries`、`gateway.outbox_events`、`ledger.idempotency_records`、`ledger.inbox_events`、`ledger.outbox_deliveries`、`ledger.outbox_events`、`ledger.receipts`、`ledger.reconciliations`、`resource_catalog.idempotency_records`、`resource_catalog.inbox_events`、`resource_catalog.operations`、`resource_catalog.outbox_deliveries`、`resource_catalog.outbox_events`、`runtime_control.idempotency_records`、`runtime_control.inbox_events`、`runtime_control.operations`、`runtime_control.outbox_deliveries`、`runtime_control.outbox_events`、`tenant.audit_events`、`tenant.authorization_contexts`、`tenant.idempotency_records`、`tenant.inbox_events`、`tenant.operations`、`tenant.outbox_deliveries`、`tenant.outbox_events`、`workspace.idempotency_records`、`workspace.inbox_events`、`workspace.operations`、`workspace.outbox_deliveries`、`workspace.outbox_events`

**验收向量**：

- **success**：能沿同一不可变Candidate追到资格、实际运行和发布证据；只读UI不会触发生产部署。
- **failure**：证据缺失显示“未取得本层证据”，不能显示运行通过；needs_attention保留requestId与允许核对入口。
- **duplicate**：同一提交意图保留 Idempotency-Key；重发相同请求只显示原资源/操作，不新增副作用。修改输入产生新意图，不复用旧 key。
- **unauthorized**：缺少页面动作权限时不提供按钮；服务端仍独立授权。访问其他 Tenant 对象显示不存在，不暴露是否存在。
- **restart**：刷新、切页或登录恢复后从权威 GET 重建视图；异步命令以原 Operation 继续查询，页面不会重新发起命令。

跨域状态/恢复执行见06；输入约束、按钮条件和中文错误见04/07。涉及旧对象同时执行09迁移接受标准。

## 阅读与完成判断

- 架构师检查本功能Owner与单writer；后端从请求DTO追到表字段/状态与事件；前端从operationId读取同一DTO；产品和客户按验收向量判断结果。
- 金额、Key、真实provider身份等敏感/权威字段的derived projection不是新的持久writer。具体字段来源在03 x-field-map与02说明。
- 矩阵包含的接口不代表当前仓库已实现。实现状态、源码测试、Local/Instance采用必须分别留证。
