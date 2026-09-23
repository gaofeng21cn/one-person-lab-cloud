# Workspace：API、字段与交互

> 源码快照 `7f5d05fe9b855d8af216caad714cf7fc85014d3c`；这是已定义目标的派生索引，不是业务实现完成声明。回到[总览](../15_domain_alignment.md)。

## 1. 边界与继承

模块：`services/workspace`。当前：**新持久化骨架；旧购买/续费/删除/应用选择仍在Control Plane**。

客户权益、Workspace、Deployment的业务选择、Subscription/period/PlanChange和Saga；不拥有provider资源、钱包、Runtime实现。

**继承 / 提取 / 新增：** 从旧workspace_launch/renewal/delete/application_deployment等逐活能力提取；保留购买receipt、原扣款键、旧资源-only义务。

**旧事实：** control_plane_workspaces及billing_state_json；runtime_operations按action拆分；compute/storage/attachment里的购买义务保留。

**事务边界：** Workspace业务选择、订阅版本与Saga步骤在本库；外域结果只存精确引用/接受快照，跨域无事务。

**业务顺序：** Catalog报价/接受→Gateway原单资金→Fabric资源→Runtime Control执行→Fabric路由读回→Workspace CAS选择→Ledger证据；未知动作回所属Owner读回。

**失败 / unknown：** unknown不新扣费不反向退款；ready不能以Pod Running代替实际应用/凭据/路由证据。

## 2. 客户 REST 与后端 Owner

21 个规格REST操作；浏览器仅经BFF。表中的请求/响应为目标契约，不代表该RPC已挂载。字段展开见DTO目录。


### createWorkspace

`POST /api/v2/workspaces`

权限：`admin, owner`；F：`F08`；主要成功状态：`202`。
| 入参位置 | 字段 | 类型 | 必填 |
| --- | --- | --- | --- |
| header | `Idempotency-Key` | `string` | 是 |
| header | `X-CSRF-Token` | `string` | 是 |

Body：[CreateWorkspaceRequest](rest-schemas.md#createworkspacerequest)；Response：[Operation](rest-schemas.md#operation)

请求顶层字段：`name`: string（必填）；`quoteId`: OpaqueId（必填）；`renewalMode`: string（必填）；`automaticRenewalConsent`: boolean（可选）

响应顶层字段：`operationId`: OpaqueId（必填）；`owner`: OperationOwner（必填）；`kind`: OperationKind（必填）；`resourceId`: OpaqueId（必填）；`status`: string（必填）；`stage`: OperationStage（必填）；`observationResult`: string（可选）；`errorCode`: ErrorCode（可选）；`requestId`: OpaqueId（必填）；`createdAt`: string/date-time（必填）；`updatedAt`: string/date-time（必填）；`pollAfterSeconds`: integer（可选）

涉及表（规格声明，不自动等于每次都写）：`workspace.workspaces`, `workspace.subscriptions`, `workspace.operations`, `workspace.saga_steps`
错误响应：`400`, `401`, `403`, `404`, `409`, `422`, `429`, `500`, `503`

### listWorkspaces

`GET /api/v2/workspaces`

权限：`member`；F：`F09`；主要成功状态：`200`。
| 入参位置 | 字段 | 类型 | 必填 |
| --- | --- | --- | --- |
| query | `cursor` | `OpaqueId` | 否 |
| query | `limit` | `integer` | 否 |

Body：无独立命名body，见该操作schema；Response：[WorkspacePage](rest-schemas.md#workspacepage)

响应顶层字段：`items`: array<Workspace>（必填）；`nextCursor`: OpaqueId（可选）

涉及表（规格声明，不自动等于每次都写）：`workspace.workspaces`
错误响应：`400`, `401`, `403`, `404`, `409`, `422`, `429`, `500`, `503`

### getWorkspace

`GET /api/v2/workspaces/{workspaceId}`

权限：`member`；F：`F09`；主要成功状态：`200`。
| 入参位置 | 字段 | 类型 | 必填 |
| --- | --- | --- | --- |
| path | `workspaceId` | `OpaqueId` | 是 |

Body：无独立命名body，见该操作schema；Response：[Workspace](rest-schemas.md#workspace)

响应顶层字段：`id`: OpaqueId（必填）；`name`: string（必填）；`capabilityVersionId`: OpaqueId（可选）；`computePlanId`: OpaqueId（必填）；`storagePlanId`: OpaqueId（必填）；`activeDeploymentId`: OpaqueId（可选）；`status`: string（必填）；`resourceReadiness`: string（必填）；`applicationAvailability`: string（必填）；`modelConfigurationVersion`: NonnegativeInt64（必填）；`currentPeriodEnd`: string/date-time（可选）；`accessUrl`: string/uri（可选）；`createdAt`: string/date-time（必填）；`updatedAt`: string/date-time（必填）；`deliveryModel`: string（必填）；`version`: NonnegativeInt64（必填）

涉及表（规格声明，不自动等于每次都写）：`workspace.workspaces`
错误响应：`400`, `401`, `403`, `404`, `409`, `422`, `429`, `500`, `503`

### deleteWorkspace

`DELETE /api/v2/workspaces/{workspaceId}`

权限：`admin, owner`；F：`F13`；主要成功状态：`202`。
| 入参位置 | 字段 | 类型 | 必填 |
| --- | --- | --- | --- |
| path | `workspaceId` | `OpaqueId` | 是 |
| header | `Idempotency-Key` | `string` | 是 |
| header | `X-CSRF-Token` | `string` | 是 |

Body：[DeleteWorkspaceRequest](rest-schemas.md#deleteworkspacerequest)；Response：[Operation](rest-schemas.md#operation)

请求顶层字段：`confirmationName`: string（必填）；`acknowledgeDataDestruction`: boolean（必填）

响应顶层字段：`operationId`: OpaqueId（必填）；`owner`: OperationOwner（必填）；`kind`: OperationKind（必填）；`resourceId`: OpaqueId（必填）；`status`: string（必填）；`stage`: OperationStage（必填）；`observationResult`: string（可选）；`errorCode`: ErrorCode（可选）；`requestId`: OpaqueId（必填）；`createdAt`: string/date-time（必填）；`updatedAt`: string/date-time（必填）；`pollAfterSeconds`: integer（可选）

涉及表（规格声明，不自动等于每次都写）：`workspace.workspaces`, `workspace.operations`, `workspace.saga_steps`, `gateway.wallet_operations`
错误响应：`400`, `401`, `403`, `404`, `409`, `422`, `429`, `500`, `503`

### getWorkspaceAccess

`POST /api/v2/workspaces/{workspaceId}/access`

权限：`member`；F：`F09`；主要成功状态：`200`。
| 入参位置 | 字段 | 类型 | 必填 |
| --- | --- | --- | --- |
| path | `workspaceId` | `OpaqueId` | 是 |
| header | `Idempotency-Key` | `string` | 是 |
| header | `X-CSRF-Token` | `string` | 是 |

Body：无独立命名body，见该操作schema；Response：[WorkspaceAccess](rest-schemas.md#workspaceaccess)

响应顶层字段：`workspaceId`: OpaqueId（必填）；`url`: string/uri（必填）；`authenticationMode`: string（必填）；`expiresAt`: string/date-time（必填）；`applicationCredentialsAvailable`: boolean（可选）

涉及表（规格声明，不自动等于每次都写）：`workspace.workspaces`
错误响应：`400`, `401`, `403`, `404`, `409`, `422`, `429`, `500`, `503`

### getWorkspaceModels

`GET /api/v2/workspaces/{workspaceId}/models`

权限：`member`；F：`F09`；主要成功状态：`200`。
| 入参位置 | 字段 | 类型 | 必填 |
| --- | --- | --- | --- |
| path | `workspaceId` | `OpaqueId` | 是 |

Body：无独立命名body，见该操作schema；Response：[ModelConfiguration](rest-schemas.md#modelconfiguration)

响应顶层字段：`workspaceId`: OpaqueId（必填）；`version`: NonnegativeInt64（必填）；`selections`: array<ModelSelection>（必填）；`status`: string（必填）；`appliedVersion`: NonnegativeInt64（可选）；`operationId`: OpaqueId（可选）；`updatedAt`: string/date-time（必填）

涉及表（规格声明，不自动等于每次都写）：`workspace.model_configurations`
错误响应：`400`, `401`, `403`, `404`, `409`, `422`, `429`, `500`, `503`

### updateWorkspaceModels

`PUT /api/v2/workspaces/{workspaceId}/models`

权限：`admin, owner`；F：`F09`；主要成功状态：`202`。
| 入参位置 | 字段 | 类型 | 必填 |
| --- | --- | --- | --- |
| path | `workspaceId` | `OpaqueId` | 是 |
| header | `Idempotency-Key` | `string` | 是 |
| header | `X-CSRF-Token` | `string` | 是 |

Body：[UpdateWorkspaceModelsRequest](rest-schemas.md#updateworkspacemodelsrequest)；Response：[Operation](rest-schemas.md#operation)

请求顶层字段：`expectedVersion`: NonnegativeInt64（必填）；`selections`: array<ModelSelection>（必填）

响应顶层字段：`operationId`: OpaqueId（必填）；`owner`: OperationOwner（必填）；`kind`: OperationKind（必填）；`resourceId`: OpaqueId（必填）；`status`: string（必填）；`stage`: OperationStage（必填）；`observationResult`: string（可选）；`errorCode`: ErrorCode（可选）；`requestId`: OpaqueId（必填）；`createdAt`: string/date-time（必填）；`updatedAt`: string/date-time（必填）；`pollAfterSeconds`: integer（可选）

涉及表（规格声明，不自动等于每次都写）：`workspace.model_configurations`, `workspace.operations`
错误响应：`400`, `401`, `403`, `404`, `409`, `422`, `429`, `500`, `503`

### listDeployments

`GET /api/v2/workspaces/{workspaceId}/deployments`

权限：`member`；F：`F10`；主要成功状态：`200`。
| 入参位置 | 字段 | 类型 | 必填 |
| --- | --- | --- | --- |
| path | `workspaceId` | `OpaqueId` | 是 |
| query | `cursor` | `OpaqueId` | 否 |
| query | `limit` | `integer` | 否 |

Body：无独立命名body，见该操作schema；Response：[DeploymentPage](rest-schemas.md#deploymentpage)

响应顶层字段：`items`: array<Deployment>（必填）；`nextCursor`: OpaqueId（可选）

涉及表（规格声明，不自动等于每次都写）：`workspace.deployments`
错误响应：`400`, `401`, `403`, `404`, `409`, `422`, `429`, `500`, `503`

### getDeployment

`GET /api/v2/workspaces/{workspaceId}/deployments/{deploymentId}`

权限：`member`；F：`F10`；主要成功状态：`200`。
| 入参位置 | 字段 | 类型 | 必填 |
| --- | --- | --- | --- |
| path | `workspaceId` | `OpaqueId` | 是 |
| path | `deploymentId` | `OpaqueId` | 是 |

Body：无独立命名body，见该操作schema；Response：[Deployment](rest-schemas.md#deployment)

响应顶层字段：`id`: OpaqueId（必填）；`workspaceId`: OpaqueId（必填）；`capabilityVersionId`: OpaqueId（必填）；`runtimeInstanceId`: OpaqueId（可选）；`previousDeploymentId`: OpaqueId（可选）；`status`: string（必填）；`dataCompatibility`: DataCompatibility（必填）；`createdAt`: string/date-time（必填）；`updatedAt`: string/date-time（必填）

涉及表（规格声明，不自动等于每次都写）：`workspace.deployments`
错误响应：`400`, `401`, `403`, `404`, `409`, `422`, `429`, `500`, `503`

### updateWorkspaceVersion

`POST /api/v2/workspaces/{workspaceId}/update`

权限：`admin, owner`；F：`F10`；主要成功状态：`202`。
| 入参位置 | 字段 | 类型 | 必填 |
| --- | --- | --- | --- |
| path | `workspaceId` | `OpaqueId` | 是 |
| header | `Idempotency-Key` | `string` | 是 |
| header | `X-CSRF-Token` | `string` | 是 |

Body：[UpdateWorkspaceVersionRequest](rest-schemas.md#updateworkspaceversionrequest)；Response：[Operation](rest-schemas.md#operation)

请求顶层字段：`capabilityVersionId`: OpaqueId（必填）；`expectedActiveDeploymentId`: OpaqueId（必填）

响应顶层字段：`operationId`: OpaqueId（必填）；`owner`: OperationOwner（必填）；`kind`: OperationKind（必填）；`resourceId`: OpaqueId（必填）；`status`: string（必填）；`stage`: OperationStage（必填）；`observationResult`: string（可选）；`errorCode`: ErrorCode（可选）；`requestId`: OpaqueId（必填）；`createdAt`: string/date-time（必填）；`updatedAt`: string/date-time（必填）；`pollAfterSeconds`: integer（可选）

涉及表（规格声明，不自动等于每次都写）：`workspace.workspaces`, `workspace.deployments`, `workspace.operations`
错误响应：`400`, `401`, `403`, `404`, `409`, `422`, `429`, `500`, `503`

### rollbackWorkspace

`POST /api/v2/workspaces/{workspaceId}/rollback`

权限：`admin, owner`；F：`F10`；主要成功状态：`202`。
| 入参位置 | 字段 | 类型 | 必填 |
| --- | --- | --- | --- |
| path | `workspaceId` | `OpaqueId` | 是 |
| header | `Idempotency-Key` | `string` | 是 |
| header | `X-CSRF-Token` | `string` | 是 |

Body：[RollbackWorkspaceRequest](rest-schemas.md#rollbackworkspacerequest)；Response：[Operation](rest-schemas.md#operation)

请求顶层字段：`targetDeploymentId`: OpaqueId（必填）；`expectedActiveDeploymentId`: OpaqueId（必填）

响应顶层字段：`operationId`: OpaqueId（必填）；`owner`: OperationOwner（必填）；`kind`: OperationKind（必填）；`resourceId`: OpaqueId（必填）；`status`: string（必填）；`stage`: OperationStage（必填）；`observationResult`: string（可选）；`errorCode`: ErrorCode（可选）；`requestId`: OpaqueId（必填）；`createdAt`: string/date-time（必填）；`updatedAt`: string/date-time（必填）；`pollAfterSeconds`: integer（可选）

涉及表（规格声明，不自动等于每次都写）：`workspace.workspaces`, `workspace.deployments`, `workspace.operations`
错误响应：`400`, `401`, `403`, `404`, `409`, `422`, `429`, `500`, `503`

### resizeWorkspace

`POST /api/v2/workspaces/{workspaceId}/resize`

权限：`admin, owner`；F：`F11`；主要成功状态：`202`。
| 入参位置 | 字段 | 类型 | 必填 |
| --- | --- | --- | --- |
| path | `workspaceId` | `OpaqueId` | 是 |
| header | `Idempotency-Key` | `string` | 是 |
| header | `X-CSRF-Token` | `string` | 是 |

Body：[ApplyQuoteRequest](rest-schemas.md#applyquoterequest)；Response：[Operation](rest-schemas.md#operation)

请求顶层字段：`quoteId`: OpaqueId（必填）

响应顶层字段：`operationId`: OpaqueId（必填）；`owner`: OperationOwner（必填）；`kind`: OperationKind（必填）；`resourceId`: OpaqueId（必填）；`status`: string（必填）；`stage`: OperationStage（必填）；`observationResult`: string（可选）；`errorCode`: ErrorCode（可选）；`requestId`: OpaqueId（必填）；`createdAt`: string/date-time（必填）；`updatedAt`: string/date-time（必填）；`pollAfterSeconds`: integer（可选）

涉及表（规格声明，不自动等于每次都写）：`workspace.workspaces`, `workspace.operations`, `workspace.saga_steps`, `workspace.plan_changes`, `workspace.subscription_period_obligations`
错误响应：`400`, `401`, `403`, `404`, `409`, `422`, `429`, `500`, `503`

### renewWorkspace

`POST /api/v2/workspaces/{workspaceId}/renew`

权限：`admin, owner`；F：`F12`；主要成功状态：`202`。
| 入参位置 | 字段 | 类型 | 必填 |
| --- | --- | --- | --- |
| path | `workspaceId` | `OpaqueId` | 是 |
| header | `Idempotency-Key` | `string` | 是 |
| header | `X-CSRF-Token` | `string` | 是 |

Body：[ApplyQuoteRequest](rest-schemas.md#applyquoterequest)；Response：[Operation](rest-schemas.md#operation)

请求顶层字段：`quoteId`: OpaqueId（必填）

响应顶层字段：`operationId`: OpaqueId（必填）；`owner`: OperationOwner（必填）；`kind`: OperationKind（必填）；`resourceId`: OpaqueId（必填）；`status`: string（必填）；`stage`: OperationStage（必填）；`observationResult`: string（可选）；`errorCode`: ErrorCode（可选）；`requestId`: OpaqueId（必填）；`createdAt`: string/date-time（必填）；`updatedAt`: string/date-time（必填）；`pollAfterSeconds`: integer（可选）

涉及表（规格声明，不自动等于每次都写）：`workspace.subscriptions`, `workspace.operations`, `workspace.saga_steps`
错误响应：`400`, `401`, `403`, `404`, `409`, `422`, `429`, `500`, `503`

### getSubscription

`GET /api/v2/workspaces/{workspaceId}/subscription`

权限：`member`；F：`F12`；主要成功状态：`200`。
| 入参位置 | 字段 | 类型 | 必填 |
| --- | --- | --- | --- |
| path | `workspaceId` | `OpaqueId` | 是 |

Body：无独立命名body，见该操作schema；Response：[Subscription](rest-schemas.md#subscription)

响应顶层字段：`id`: OpaqueId（必填）；`workspaceId`: OpaqueId（必填）；`currentPeriodStart`: string/date-time（必填）；`currentPeriodEnd`: string/date-time（必填）；`periodMonths`: integer/int32（必填）；`status`: string（必填）；`acceptedQuoteId`: OpaqueId（可选）；`renewalOperationId`: OpaqueId（可选）；`createdAt`: string/date-time（必填）；`provenance`: string（必填）；`legacyPurchaseId`: OpaqueId（可选）；`refundPolicyVersionId`: OpaqueId（可选）；`retentionPolicyVersionId`: OpaqueId（可选）；`refundTerms`: string（可选）；`retentionTerms`: string（可选）；`renewalMode`: string（必填）；`renewalConsentId`: OpaqueId（可选）；`renewalSettingsVersion`: NonnegativeInt64（必填）；`version`: NonnegativeInt64（必填）；`currentPricePolicyVersionId`: OpaqueId（可选）；`currentMonthlyUSDMicros`: USDMicros（可选）

涉及表（规格声明，不自动等于每次都写）：`workspace.subscriptions`
错误响应：`400`, `401`, `403`, `404`, `409`, `422`, `429`, `500`, `503`

### getWorkspaceDeletion

`GET /api/v2/workspaces/{workspaceId}/deletion`

权限：`member`；F：`F13`；主要成功状态：`200`。
| 入参位置 | 字段 | 类型 | 必填 |
| --- | --- | --- | --- |
| path | `workspaceId` | `OpaqueId` | 是 |

Body：无独立命名body，见该操作schema；Response：[WorkspaceDeletion](rest-schemas.md#workspacedeletion)

响应顶层字段：`workspaceId`: OpaqueId（必填）；`operationId`: OpaqueId（必填）；`resourceDeletionStatus`: string（必填）；`dataDeletionStatus`: string（必填）；`refundOperationId`: OpaqueId（可选）；`refundStatus`: string（必填）；`updatedAt`: string/date-time（必填）

涉及表（规格声明，不自动等于每次都写）：`workspace.operations`, `workspace.saga_steps`, `gateway.wallet_operations`
错误响应：`400`, `401`, `403`, `404`, `409`, `422`, `429`, `500`, `503`

### adoptWorkspace

`POST /api/v2/workspaces/{workspaceId}/adopt`

权限：`admin, owner`；F：`F16, F09`；主要成功状态：`202`。
| 入参位置 | 字段 | 类型 | 必填 |
| --- | --- | --- | --- |
| path | `workspaceId` | `OpaqueId` | 是 |
| header | `Idempotency-Key` | `string` | 是 |
| header | `X-CSRF-Token` | `string` | 是 |

Body：[AdoptWorkspaceRequest](rest-schemas.md#adoptworkspacerequest)；Response：[Operation](rest-schemas.md#operation)

请求顶层字段：`capabilityVersionId`: OpaqueId（必填）；`expectedWorkspaceVersion`: NonnegativeInt64（必填）；`modelSelections`: array<ModelSelection>（必填）

响应顶层字段：`operationId`: OpaqueId（必填）；`owner`: OperationOwner（必填）；`kind`: OperationKind（必填）；`resourceId`: OpaqueId（必填）；`status`: string（必填）；`stage`: OperationStage（必填）；`observationResult`: string（可选）；`errorCode`: ErrorCode（可选）；`requestId`: OpaqueId（必填）；`createdAt`: string/date-time（必填）；`updatedAt`: string/date-time（必填）；`pollAfterSeconds`: integer（可选）

涉及表（规格声明，不自动等于每次都写）：`workspace.workspaces`, `workspace.deployments`, `workspace.operations`
错误响应：`400`, `401`, `403`, `404`, `409`, `422`, `429`, `500`, `503`

### updateRenewalSettings

`PUT /api/v2/workspaces/{workspaceId}/subscription/renewal`

权限：`admin, owner`；F：`F12`；主要成功状态：`202`。
| 入参位置 | 字段 | 类型 | 必填 |
| --- | --- | --- | --- |
| path | `workspaceId` | `OpaqueId` | 是 |
| header | `Idempotency-Key` | `string` | 是 |
| header | `X-CSRF-Token` | `string` | 是 |

Body：[UpdateRenewalSettingsRequest](rest-schemas.md#updaterenewalsettingsrequest)；Response：[Operation](rest-schemas.md#operation)

请求顶层字段：`renewalMode`: string（必填）；`expectedRenewalSettingsVersion`: NonnegativeInt64（必填）；`automaticRenewalConsent`: boolean（可选）

响应顶层字段：`operationId`: OpaqueId（必填）；`owner`: OperationOwner（必填）；`kind`: OperationKind（必填）；`resourceId`: OpaqueId（必填）；`status`: string（必填）；`stage`: OperationStage（必填）；`observationResult`: string（可选）；`errorCode`: ErrorCode（可选）；`requestId`: OpaqueId（必填）；`createdAt`: string/date-time（必填）；`updatedAt`: string/date-time（必填）；`pollAfterSeconds`: integer（可选）

涉及表（规格声明，不自动等于每次都写）：`workspace.subscriptions`, `workspace.operations`
错误响应：`400`, `401`, `403`, `404`, `409`, `422`, `429`, `500`, `503`

### revealWorkspaceApplicationCredentials

`POST /api/v2/workspaces/{workspaceId}/application-credentials/reveal`

权限：`owner`；F：`F09`；主要成功状态：`200`。
| 入参位置 | 字段 | 类型 | 必填 |
| --- | --- | --- | --- |
| path | `workspaceId` | `OpaqueId` | 是 |
| header | `Idempotency-Key` | `string` | 是 |
| header | `X-CSRF-Token` | `string` | 是 |

Body：无独立命名body，见该操作schema；Response：[WorkspaceApplicationCredentials](rest-schemas.md#workspaceapplicationcredentials)

响应顶层字段：`workspaceId`: OpaqueId（必填）；`runtimeInstanceId`: OpaqueId（必填）；`username`: string（必填）；`password`: string（必填）

涉及表（规格声明，不自动等于每次都写）：`workspace.workspaces`, `workspace.deployments`
错误响应：`400`, `401`, `403`, `404`, `409`, `422`, `429`, `500`, `503`

### listPlanChanges

`GET /api/v2/workspaces/{workspaceId}/plan-changes`

权限：`member`；F：`F11`；主要成功状态：`200`。
| 入参位置 | 字段 | 类型 | 必填 |
| --- | --- | --- | --- |
| path | `workspaceId` | `OpaqueId` | 是 |
| query | `cursor` | `OpaqueId` | 否 |
| query | `limit` | `integer` | 否 |

Body：无独立命名body，见该操作schema；Response：[PlanChangePage](rest-schemas.md#planchangepage)

响应顶层字段：`items`: array<PlanChange>（必填）；`nextCursor`: OpaqueId（可选）

涉及表（规格声明，不自动等于每次都写）：`workspace.plan_changes`
错误响应：`400`, `401`, `403`, `404`, `409`, `422`, `429`, `500`, `503`

### getPlanChange

`GET /api/v2/workspaces/{workspaceId}/plan-changes/{planChangeId}`

权限：`member`；F：`F11`；主要成功状态：`200`。
| 入参位置 | 字段 | 类型 | 必填 |
| --- | --- | --- | --- |
| path | `workspaceId` | `OpaqueId` | 是 |
| path | `planChangeId` | `OpaqueId` | 是 |

Body：无独立命名body，见该操作schema；Response：[PlanChange](rest-schemas.md#planchange)

响应顶层字段：`id`: OpaqueId（必填）；`workspaceId`: OpaqueId（必填）；`kind`: string（必填）；`status`: string（必填）；`sourceComputePlanId`: OpaqueId（必填）；`sourceStoragePlanId`: OpaqueId（必填）；`targetComputePlanId`: OpaqueId（必填）；`targetStoragePlanId`: OpaqueId（必填）；`sourcePricePolicyVersionId`: OpaqueId（必填）；`targetPricePolicyVersionId`: OpaqueId（必填）；`sourceSubscriptionId`: OpaqueId（必填）；`sourceSubscriptionVersion`: NonnegativeInt64（必填）；`sourcePeriodId`: OpaqueId（必填）；`sourceMonthlyUSDMicros`: USDMicros（必填）；`targetMonthlyUSDMicros`: USDMicros（必填）；`quoteId`: OpaqueId（必填）；`policyVersion`: string（必填）；`quoteAt`: string/date-time（必填）；`periodStart`: string/date-time（必填）；`periodEnd`: string/date-time（必填）；`chargeUSDMicros`: USDMicros（必填）；`plannedEffectiveAt`: string/date-time（必填）；`appliedAt`: string/date-time（可选）；`operationId`: OpaqueId（必填）；`executionOperationId`: OpaqueId（可选）；`cancellationOperationId`: OpaqueId（可选）；`chargeOperationId`: OpaqueId（可选）；`chargeStatus`: string（必填）；`nextPeriodStart`: string/date-time（可选）；`nextPeriodEnd`: string/date-time（可选）；`nextPeriodChargeUSDMicros`: USDMicros（可选）；`nextPeriodObligationId`: OpaqueId（可选）；`nextPeriodChargeOperationId`: OpaqueId（可选）；`nextPeriodChargeStatus`: string（可选）；`refundOperationIds`: array<OpaqueId>（必填）；`scheduleVersion`: NonnegativeInt64（必填）；`observationResult`: string（必填）；`errorCode`: ErrorCode（可选）；`cancellable`: boolean（必填）；`createdAt`: string/date-time（必填）；`updatedAt`: string/date-time（必填）；`deliveryOutcome`: string（必填）；`resourceOutcome`: string（必填）；`runtimeReadbackRequirement`: string（必填）；`currentRequirementValidation`: string（必填）；`riskCode`: ErrorCode（可选）；`lastValidatedAt`: string/date-time（可选）；`executionPlanId`: OpaqueId（必填）；`executionPlanDigest`: Digest（必填）；`quoteAtMilliseconds`: NonnegativeInt64（必填）；`periodStartMilliseconds`: NonnegativeInt64（必填）；`periodEndMilliseconds`: NonnegativeInt64（必填）；`sourceFinancialSnapshotDigest`: Digest（必填）

涉及表（规格声明，不自动等于每次都写）：`workspace.plan_changes`
错误响应：`400`, `401`, `403`, `404`, `409`, `422`, `429`, `500`, `503`

### cancelPlanChange

`POST /api/v2/workspaces/{workspaceId}/plan-changes/{planChangeId}/cancel`

权限：`admin, owner`；F：`F11`；主要成功状态：`202`。
| 入参位置 | 字段 | 类型 | 必填 |
| --- | --- | --- | --- |
| path | `workspaceId` | `OpaqueId` | 是 |
| path | `planChangeId` | `OpaqueId` | 是 |
| header | `Idempotency-Key` | `string` | 是 |
| header | `X-CSRF-Token` | `string` | 是 |

Body：[CancelPlanChangeRequest](rest-schemas.md#cancelplanchangerequest)；Response：[Operation](rest-schemas.md#operation)

请求顶层字段：`expectedScheduleVersion`: NonnegativeInt64（必填）；`reason`: string（必填）

响应顶层字段：`operationId`: OpaqueId（必填）；`owner`: OperationOwner（必填）；`kind`: OperationKind（必填）；`resourceId`: OpaqueId（必填）；`status`: string（必填）；`stage`: OperationStage（必填）；`observationResult`: string（可选）；`errorCode`: ErrorCode（可选）；`requestId`: OpaqueId（必填）；`createdAt`: string/date-time（必填）；`updatedAt`: string/date-time（必填）；`pollAfterSeconds`: integer（可选）

涉及表（规格声明，不自动等于每次都写）：`workspace.plan_changes`, `workspace.operations`, `workspace.subscription_period_obligations`
错误响应：`400`, `401`, `403`, `404`, `409`, `422`, `429`, `500`, `503`

## 3. 本域数据库全字段

`opl_workspace`：14 张表，258 列。字段权威：[02](../02_database_schema_complete.md)、[SQL](../contracts/schema.sql)；以下从db_inventory派生。**表不是自动等同DDD聚合根**；事务边界见第1节。


### workspace.workspaces

当前已接受业务选择；迁移裸资源capabilityVersionId可空，UI不得称已部署；expiresAt从订阅投影

| 列 | SQL类型 | 可NULL | 默认值 | API血缘 / 原始来源 |
| --- | --- | --- | --- | --- |
| `id` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/Workspace/properties/id |
| `tenant_id` | `text` | 否 | `—` | 02_database_schema_complete.md#workspace.workspaces.tenant_id |
| `name` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/Workspace/properties/name |
| `status` | `text` | 否 | `'provisioning'` | 03_api_contract_complete.yaml#/components/schemas/Workspace/properties/status |
| `capability_version_id` | `text` | 是 | `—` | 03_api_contract_complete.yaml#/components/schemas/Workspace/properties/capabilityVersionId |
| `compute_plan_id` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/Workspace/properties/computePlanId |
| `storage_plan_id` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/Workspace/properties/storagePlanId |
| `active_deployment_id` | `text` | 是 | `—` | 03_api_contract_complete.yaml#/components/schemas/Workspace/properties/activeDeploymentId |
| `model_configuration_version` | `bigint` | 否 | `0` | 03_api_contract_complete.yaml#/components/schemas/Workspace/properties/modelConfigurationVersion; 03_api_contract_complete.yaml#/components/schemas/ModelConfiguration/properties/appliedVersion |
| `active_operation_id` | `text` | 是 | `—` | 02_database_schema_complete.md#workspace.workspaces.active_operation_id |
| `legacy_origin_id` | `text` | 是 | `—` | 02_database_schema_complete.md#workspace.workspaces.legacy_origin_id |
| `created_by` | `text` | 否 | `—` | 02_database_schema_complete.md#workspace.workspaces.created_by |
| `deleted_at` | `timestamptz` | 是 | `—` | 02_database_schema_complete.md#workspace.workspaces.deleted_at |
| `created_at` | `timestamptz` | 否 | `now()` | 03_api_contract_complete.yaml#/components/schemas/Workspace/properties/createdAt |
| `updated_at` | `timestamptz` | 否 | `now()` | 03_api_contract_complete.yaml#/components/schemas/Workspace/properties/updatedAt |
| `delivery_model` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/Workspace/properties/deliveryModel |
| `version` | `bigint` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/Workspace/properties/version |
| `execution_epoch` | `bigint` | 否 | `0` | 02_database_schema_complete.md#workspace.workspaces.execution_epoch |
| `selected_route_generation` | `bigint` | 是 | `—` | 02_database_schema_complete.md#workspace.workspaces.selected_route_generation |
| `selected_execution_epoch` | `bigint` | 是 | `—` | 02_database_schema_complete.md#workspace.workspaces.selected_execution_epoch |

约束：
- `PRIMARY KEY (id)`
- `CHECK (status IN ('provisioning','active','updating','suspended','deleting','deleted','failed','needs_attention'))`
- `CHECK (model_configuration_version >= 0)`
- `CHECK ((status = 'deleted') = (deleted_at IS NOT NULL))`
- `FOREIGN KEY (active_deployment_id, id) REFERENCES workspace.deployments (id, workspace_id) ON DELETE RESTRICT`
- `FOREIGN KEY (active_operation_id) REFERENCES workspace.operations (id) ON DELETE RESTRICT`
- `CHECK (delivery_model IN ('legacy_resource_only','imported_application','agent_saas'))`
- `CHECK (version >= 0)`
- `CHECK ((delivery_model = 'legacy_resource_only' AND capability_version_id IS NULL AND active_deployment_id IS NULL) OR (delivery_model IN ('imported_application','agent_saas') AND capability_version_id IS NOT NULL))`
- `CHECK (execution_epoch >= 0)`
- `CHECK ((selected_route_generation IS NULL) = (selected_execution_epoch IS NULL))`
- `CHECK (selected_route_generation IS NULL OR selected_route_generation >= 0)`
- `CHECK (selected_execution_epoch IS NULL OR (selected_execution_epoch >= 0 AND selected_execution_epoch <= execution_epoch))`
- `UNIQUE (id, tenant_id)`

索引：
- `{"name": "workspaces_tenant_list", "columns": "tenant_id, created_at DESC, id DESC", "unique": false, "where": null}`
- `{"name": "workspaces_legacy", "columns": "legacy_origin_id", "unique": true, "where": "legacy_origin_id IS NOT NULL"}`

### workspace.deployments

workspaces.active_deployment_id唯一选中；同事务切换指针和supersede旧部署，不由Runtime写

| 列 | SQL类型 | 可NULL | 默认值 | API血缘 / 原始来源 |
| --- | --- | --- | --- | --- |
| `id` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/Deployment/properties/id |
| `workspace_id` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/Deployment/properties/workspaceId |
| `capability_version_id` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/Deployment/properties/capabilityVersionId |
| `artifact_digest` | `text` | 否 | `—` | 02_database_schema_complete.md#workspace.deployments.artifact_digest |
| `reference_claim_id` | `text` | 否 | `—` | 02_database_schema_complete.md#workspace.deployments.reference_claim_id |
| `runtime_instance_id` | `text` | 是 | `—` | 03_api_contract_complete.yaml#/components/schemas/Deployment/properties/runtimeInstanceId |
| `previous_deployment_id` | `text` | 是 | `—` | 03_api_contract_complete.yaml#/components/schemas/Deployment/properties/previousDeploymentId |
| `operation_id` | `text` | 否 | `—` | 02_database_schema_complete.md#workspace.deployments.operation_id |
| `status` | `text` | 否 | `'queued'` | 03_api_contract_complete.yaml#/components/schemas/Deployment/properties/status |
| `data_compatibility` | `jsonb` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/Deployment/properties/dataCompatibility |
| `data_migration_evidence_ref` | `text` | 是 | `—` | 02_database_schema_complete.md#workspace.deployments.data_migration_evidence_ref |
| `verification_evidence_ref` | `text` | 是 | `—` | 02_database_schema_complete.md#workspace.deployments.verification_evidence_ref |
| `error_code` | `text` | 是 | `—` | 02_database_schema_complete.md#workspace.deployments.error_code |
| `activated_at` | `timestamptz` | 是 | `—` | 02_database_schema_complete.md#workspace.deployments.activated_at |
| `created_at` | `timestamptz` | 否 | `now()` | 03_api_contract_complete.yaml#/components/schemas/Deployment/properties/createdAt |
| `updated_at` | `timestamptz` | 否 | `now()` | 03_api_contract_complete.yaml#/components/schemas/Deployment/properties/updatedAt |
| `execution_epoch` | `bigint` | 否 | `—` | 02_database_schema_complete.md#workspace.deployments.execution_epoch |
| `confirmed_route_switch_id` | `text` | 是 | `—` | 02_database_schema_complete.md#workspace.deployments.confirmed_route_switch_id |
| `selection_commit_receipt_id` | `text` | 是 | `—` | 02_database_schema_complete.md#workspace.deployments.selection_commit_receipt_id |

约束：
- `PRIMARY KEY (id)`
- `FOREIGN KEY (workspace_id) REFERENCES workspace.workspaces (id) ON DELETE RESTRICT`
- `FOREIGN KEY (previous_deployment_id) REFERENCES workspace.deployments (id) ON DELETE RESTRICT`
- `CHECK (status IN ('queued','deploying','verifying','active','superseded','failed','rolling_back','rolled_back','needs_attention'))`
- `UNIQUE (id, workspace_id)`
- `CHECK (artifact_digest ~ '^sha256:[0-9a-f]{64}$')`
- `CHECK (status <> 'active' OR (runtime_instance_id IS NOT NULL AND verification_evidence_ref IS NOT NULL AND activated_at IS NOT NULL))`
- `FOREIGN KEY (operation_id) REFERENCES workspace.operations (id) ON DELETE RESTRICT`
- `CHECK (execution_epoch >= 0)`

索引：
- `{"name": "deployments_workspace_list", "columns": "workspace_id, created_at DESC, id DESC", "unique": false, "where": null}`
- `{"name": "deployments_operation", "columns": "operation_id", "unique": false, "where": null}`

### workspace.subscriptions

当前已付周期权威；到期默认停用不自动扣款；status从周期/Workspace生命周期派生；续费Operation幂等创建新周期，不改旧历史；quoted仅存接受Quote快照，legacy_import保留原purchase ID及原义务证据，禁止造Quote/重新扣费；缺原policy或receipt须标明确缺口并拒绝受影响动作

| 列 | SQL类型 | 可NULL | 默认值 | API血缘 / 原始来源 |
| --- | --- | --- | --- | --- |
| `id` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/Subscription/properties/id |
| `workspace_id` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/Subscription/properties/workspaceId |
| `accepted_quote_id` | `text` | 是 | `—` | 03_api_contract_complete.yaml#/components/schemas/Subscription/properties/acceptedQuoteId |
| `accepted_quote_snapshot` | `jsonb` | 是 | `—` | 02_database_schema_complete.md#workspace.subscriptions.accepted_quote_snapshot |
| `billing_subject_ref` | `text` | 否 | `—` | 02_database_schema_complete.md#workspace.subscriptions.billing_subject_ref |
| `current_period_start` | `timestamptz` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/Subscription/properties/currentPeriodStart |
| `current_period_end` | `timestamptz` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/Workspace/properties/currentPeriodEnd; 03_api_contract_complete.yaml#/components/schemas/Subscription/properties/currentPeriodEnd |
| `last_charge_wallet_operation_id` | `text` | 是 | `—` | 02_database_schema_complete.md#workspace.subscriptions.last_charge_wallet_operation_id |
| `active_change_operation_id` | `text` | 是 | `—` | 03_api_contract_complete.yaml#/components/schemas/Subscription/properties/renewalOperationId |
| `created_at` | `timestamptz` | 否 | `now()` | 03_api_contract_complete.yaml#/components/schemas/Subscription/properties/createdAt |
| `updated_at` | `timestamptz` | 否 | `now()` | 02_database_schema_complete.md#workspace.subscriptions.updated_at |
| `period_months` | `integer` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/Subscription/properties/periodMonths |
| `provenance` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/Subscription/properties/provenance |
| `legacy_purchase_id` | `text` | 是 | `—` | 03_api_contract_complete.yaml#/components/schemas/Subscription/properties/legacyPurchaseId |
| `legacy_obligation_snapshot` | `jsonb` | 是 | `—` | 02_database_schema_complete.md#workspace.subscriptions.legacy_obligation_snapshot |
| `renewal_mode` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/Subscription/properties/renewalMode |
| `renewal_consent_id` | `text` | 是 | `—` | 03_api_contract_complete.yaml#/components/schemas/Subscription/properties/renewalConsentId |
| `renewal_consent_snapshot` | `jsonb` | 是 | `—` | 02_database_schema_complete.md#workspace.subscriptions.renewal_consent_snapshot |
| `renewal_settings_version` | `bigint` | 否 | `0` | 03_api_contract_complete.yaml#/components/schemas/Subscription/properties/renewalSettingsVersion |
| `version` | `bigint` | 否 | `0` | 03_api_contract_complete.yaml#/components/schemas/Subscription/properties/version |
| `current_price_policy_version_id` | `text` | 是 | `—` | 03_api_contract_complete.yaml#/components/schemas/Subscription/properties/currentPricePolicyVersionId |
| `current_monthly_usd_micros` | `bigint` | 是 | `—` | 03_api_contract_complete.yaml#/components/schemas/Subscription/properties/currentMonthlyUSDMicros |
| `billing_anchor_day` | `integer` | 是 | `—` | 02_database_schema_complete.md#workspace.subscriptions.billing_anchor_day |

约束：
- `PRIMARY KEY (id)`
- `FOREIGN KEY (workspace_id) REFERENCES workspace.workspaces (id) ON DELETE RESTRICT`
- `UNIQUE (workspace_id)`
- `CHECK (current_period_end > current_period_start)`
- `FOREIGN KEY (active_change_operation_id) REFERENCES workspace.operations (id) ON DELETE RESTRICT`
- `CHECK (period_months > 0)`
- `CHECK (provenance IN ('quoted','legacy_import'))`
- `CHECK ((provenance = 'quoted' AND accepted_quote_id IS NOT NULL AND accepted_quote_snapshot IS NOT NULL AND legacy_purchase_id IS NULL AND legacy_obligation_snapshot IS NULL) OR (provenance = 'legacy_import' AND accepted_quote_id IS NULL AND accepted_quote_snapshot IS NULL AND legacy_purchase_id IS NOT NULL AND legacy_obligation_snapshot IS NOT NULL))`
- `CHECK (renewal_mode IN ('manual','automatic'))`
- `CHECK (renewal_mode <> 'automatic' OR (renewal_consent_id IS NOT NULL AND renewal_consent_snapshot IS NOT NULL))`
- `CHECK (renewal_settings_version >= 0)`
- `CHECK (version >= 0)`
- `CHECK (current_monthly_usd_micros IS NULL OR current_monthly_usd_micros >= 0)`
- `CHECK (billing_anchor_day IS NULL OR billing_anchor_day BETWEEN 1 AND 31)`
- `CHECK (provenance <> 'quoted' OR (current_price_policy_version_id IS NOT NULL AND current_monthly_usd_micros IS NOT NULL AND billing_anchor_day IS NOT NULL))`
- `UNIQUE (id, workspace_id)`

索引：
- `{"name": "subscriptions_expiry", "columns": "current_period_end, id", "unique": false, "where": null}`

### workspace.subscription_periods

确认付费周期不可变历史；Local零报价wallet operation可空，receipt记录零费事实不假造扣款；quoted仅存接受Quote快照，legacy_import保留原purchase ID及原义务证据，禁止造Quote/重新扣费；缺原policy或receipt须标明确缺口并拒绝受影响动作

| 列 | SQL类型 | 可NULL | 默认值 | API血缘 / 原始来源 |
| --- | --- | --- | --- | --- |
| `id` | `text` | 否 | `—` | 02_database_schema_complete.md#workspace.subscription_periods.id |
| `subscription_id` | `text` | 否 | `—` | 02_database_schema_complete.md#workspace.subscription_periods.subscription_id |
| `quote_id` | `text` | 是 | `—` | 02_database_schema_complete.md#workspace.subscription_periods.quote_id |
| `accepted_quote_snapshot` | `jsonb` | 是 | `—` | 02_database_schema_complete.md#workspace.subscription_periods.accepted_quote_snapshot |
| `period_start` | `timestamptz` | 否 | `—` | 02_database_schema_complete.md#workspace.subscription_periods.period_start |
| `period_end` | `timestamptz` | 否 | `—` | 02_database_schema_complete.md#workspace.subscription_periods.period_end |
| `billing_key` | `text` | 否 | `—` | 02_database_schema_complete.md#workspace.subscription_periods.billing_key |
| `charge_wallet_operation_id` | `text` | 是 | `—` | 02_database_schema_complete.md#workspace.subscription_periods.charge_wallet_operation_id |
| `charge_receipt_id` | `text` | 是 | `—` | 02_database_schema_complete.md#workspace.subscription_periods.charge_receipt_id |
| `created_at` | `timestamptz` | 否 | `now()` | 02_database_schema_complete.md#workspace.subscription_periods.created_at |
| `provenance` | `text` | 否 | `—` | 02_database_schema_complete.md#workspace.subscription_periods.provenance |
| `legacy_purchase_id` | `text` | 是 | `—` | 02_database_schema_complete.md#workspace.subscription_periods.legacy_purchase_id |
| `legacy_obligation_snapshot` | `jsonb` | 是 | `—` | 02_database_schema_complete.md#workspace.subscription_periods.legacy_obligation_snapshot |

约束：
- `PRIMARY KEY (id)`
- `FOREIGN KEY (subscription_id) REFERENCES workspace.subscriptions (id) ON DELETE RESTRICT`
- `UNIQUE (subscription_id, period_start)`
- `UNIQUE (billing_key)`
- `UNIQUE (charge_wallet_operation_id)`
- `CHECK (period_end > period_start)`
- `CHECK (provenance IN ('quoted','legacy_import'))`
- `CHECK ((provenance = 'quoted' AND quote_id IS NOT NULL AND accepted_quote_snapshot IS NOT NULL AND legacy_purchase_id IS NULL AND legacy_obligation_snapshot IS NULL) OR (provenance = 'legacy_import' AND quote_id IS NULL AND accepted_quote_snapshot IS NULL AND legacy_purchase_id IS NOT NULL AND legacy_obligation_snapshot IS NOT NULL))`
- `CHECK (provenance <> 'quoted' OR charge_receipt_id IS NOT NULL)`
- `UNIQUE (id, subscription_id)`

索引：
- `{"name": "subscription_periods_list", "columns": "subscription_id, period_start DESC, id DESC", "unique": false, "where": null}`

### workspace.model_configurations

新配置新version；Runtime有效调用验证后事务推进Workspace.modelConfigurationVersion

| 列 | SQL类型 | 可NULL | 默认值 | API血缘 / 原始来源 |
| --- | --- | --- | --- | --- |
| `id` | `text` | 否 | `—` | 02_database_schema_complete.md#workspace.model_configurations.id |
| `workspace_id` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/ModelConfiguration/properties/workspaceId |
| `version` | `bigint` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/ModelConfiguration/properties/version |
| `gateway_key_binding_id` | `text` | 否 | `—` | 02_database_schema_complete.md#workspace.model_configurations.gateway_key_binding_id |
| `operation_id` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/ModelConfiguration/properties/operationId |
| `runtime_reload_observation` | `text` | 否 | `—` | 02_database_schema_complete.md#workspace.model_configurations.runtime_reload_observation |
| `verification_evidence_ref` | `text` | 是 | `—` | 02_database_schema_complete.md#workspace.model_configurations.verification_evidence_ref |
| `created_by` | `text` | 否 | `—` | 02_database_schema_complete.md#workspace.model_configurations.created_by |
| `created_at` | `timestamptz` | 否 | `now()` | 02_database_schema_complete.md#workspace.model_configurations.created_at |
| `selections` | `jsonb` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/ModelConfiguration/properties/selections |
| `updated_at` | `timestamptz` | 否 | `now()` | 03_api_contract_complete.yaml#/components/schemas/ModelConfiguration/properties/updatedAt |

约束：
- `PRIMARY KEY (id)`
- `FOREIGN KEY (workspace_id) REFERENCES workspace.workspaces (id) ON DELETE RESTRICT`
- `UNIQUE (workspace_id, version)`
- `CHECK (version > 0)`
- `CHECK (runtime_reload_observation IN ('confirmed','rejected','unknown'))`
- `FOREIGN KEY (operation_id) REFERENCES workspace.operations (id) ON DELETE RESTRICT`

索引：
- `{"name": "model_configurations_workspace", "columns": "workspace_id, version DESC", "unique": false, "where": null}`

### workspace.saga_steps

固定commandId/幂等键重试；unknown读原Owner，不制造新副作用或逆向补偿

| 列 | SQL类型 | 可NULL | 默认值 | API血缘 / 原始来源 |
| --- | --- | --- | --- | --- |
| `id` | `text` | 否 | `—` | 02_database_schema_complete.md#workspace.saga_steps.id |
| `operation_id` | `text` | 否 | `—` | 02_database_schema_complete.md#workspace.saga_steps.operation_id |
| `step_key` | `text` | 否 | `—` | 02_database_schema_complete.md#workspace.saga_steps.step_key |
| `sequence` | `integer` | 否 | `—` | 02_database_schema_complete.md#workspace.saga_steps.sequence |
| `target_owner` | `text` | 否 | `—` | 02_database_schema_complete.md#workspace.saga_steps.target_owner |
| `command_id` | `text` | 否 | `—` | 02_database_schema_complete.md#workspace.saga_steps.command_id |
| `idempotency_key` | `text` | 否 | `—` | 02_database_schema_complete.md#workspace.saga_steps.idempotency_key |
| `input_snapshot` | `jsonb` | 否 | `—` | 02_database_schema_complete.md#workspace.saga_steps.input_snapshot |
| `observation_result` | `text` | 是 | `—` | 02_database_schema_complete.md#workspace.saga_steps.observation_result |
| `owner_result_ref` | `text` | 是 | `—` | 02_database_schema_complete.md#workspace.saga_steps.owner_result_ref |
| `compensation_command_id` | `text` | 是 | `—` | 02_database_schema_complete.md#workspace.saga_steps.compensation_command_id |
| `compensation_observation` | `text` | 是 | `—` | 02_database_schema_complete.md#workspace.saga_steps.compensation_observation |
| `attempt_count` | `integer` | 否 | `0` | 02_database_schema_complete.md#workspace.saga_steps.attempt_count |
| `next_attempt_at` | `timestamptz` | 是 | `—` | 02_database_schema_complete.md#workspace.saga_steps.next_attempt_at |
| `error_code` | `text` | 是 | `—` | 02_database_schema_complete.md#workspace.saga_steps.error_code |
| `confirmed_at` | `timestamptz` | 是 | `—` | 02_database_schema_complete.md#workspace.saga_steps.confirmed_at |
| `created_at` | `timestamptz` | 否 | `now()` | 02_database_schema_complete.md#workspace.saga_steps.created_at |
| `updated_at` | `timestamptz` | 否 | `now()` | 02_database_schema_complete.md#workspace.saga_steps.updated_at |

约束：
- `PRIMARY KEY (id)`
- `UNIQUE (operation_id, step_key)`
- `UNIQUE (command_id)`
- `CHECK (sequence >= 0 AND attempt_count >= 0)`
- `CHECK (observation_result IN ('confirmed','rejected','unknown'))`
- `CHECK (compensation_observation IN ('confirmed','rejected','unknown'))`
- `FOREIGN KEY (operation_id) REFERENCES workspace.operations (id) ON DELETE RESTRICT`

索引：
- `{"name": "saga_steps_recovery", "columns": "next_attempt_at", "unique": false, "where": "confirmed_at IS NULL"}`
- `{"name": "saga_steps_operation", "columns": "operation_id, sequence", "unique": false, "where": null}`

### workspace.outbox_events

本Owner状态事务同写immutable事件；revision只在本Owner聚合锁内分配

| 列 | SQL类型 | 可NULL | 默认值 | API血缘 / 原始来源 |
| --- | --- | --- | --- | --- |
| `id` | `text` | 否 | `—` | 02_database_schema_complete.md#workspace.outbox_events.id |
| `event_type` | `text` | 否 | `—` | 02_database_schema_complete.md#workspace.outbox_events.event_type |
| `schema_version` | `integer` | 否 | `—` | 02_database_schema_complete.md#workspace.outbox_events.schema_version |
| `aggregate_type` | `text` | 否 | `—` | 02_database_schema_complete.md#workspace.outbox_events.aggregate_type |
| `aggregate_id` | `text` | 否 | `—` | 02_database_schema_complete.md#workspace.outbox_events.aggregate_id |
| `aggregate_revision` | `bigint` | 否 | `—` | 02_database_schema_complete.md#workspace.outbox_events.aggregate_revision |
| `tenant_id` | `text` | 是 | `—` | 02_database_schema_complete.md#workspace.outbox_events.tenant_id |
| `correlation_id` | `text` | 否 | `—` | 02_database_schema_complete.md#workspace.outbox_events.correlation_id |
| `causation_id` | `text` | 是 | `—` | 02_database_schema_complete.md#workspace.outbox_events.causation_id |
| `payload` | `jsonb` | 否 | `—` | 02_database_schema_complete.md#workspace.outbox_events.payload |
| `payload_sha256` | `text` | 否 | `—` | 02_database_schema_complete.md#workspace.outbox_events.payload_sha256 |
| `occurred_at` | `timestamptz` | 否 | `—` | 02_database_schema_complete.md#workspace.outbox_events.occurred_at |
| `created_at` | `timestamptz` | 否 | `now()` | 02_database_schema_complete.md#workspace.outbox_events.created_at |

约束：
- `PRIMARY KEY (id)`
- `CHECK (schema_version > 0 AND aggregate_revision >= 0)`
- `CHECK (payload_sha256 ~ '^[0-9a-f]{64}$')`
- `UNIQUE (aggregate_type, aggregate_id, aggregate_revision, event_type)`

索引：
- `{"name": "outbox_events_aggregate", "columns": "aggregate_type, aggregate_id, aggregate_revision", "unique": false, "where": null}`

### workspace.outbox_deliveries

各consumer独立ACK，禁止单个ACK丢其他消费者事件；lease仅本库

| 列 | SQL类型 | 可NULL | 默认值 | API血缘 / 原始来源 |
| --- | --- | --- | --- | --- |
| `id` | `text` | 否 | `—` | 02_database_schema_complete.md#workspace.outbox_deliveries.id |
| `event_id` | `text` | 否 | `—` | 02_database_schema_complete.md#workspace.outbox_deliveries.event_id |
| `consumer_owner` | `text` | 否 | `—` | 02_database_schema_complete.md#workspace.outbox_deliveries.consumer_owner |
| `attempt_count` | `integer` | 否 | `0` | 02_database_schema_complete.md#workspace.outbox_deliveries.attempt_count |
| `next_attempt_at` | `timestamptz` | 否 | `now()` | 02_database_schema_complete.md#workspace.outbox_deliveries.next_attempt_at |
| `acknowledged_at` | `timestamptz` | 是 | `—` | 02_database_schema_complete.md#workspace.outbox_deliveries.acknowledged_at |
| `last_error_code` | `text` | 是 | `—` | 02_database_schema_complete.md#workspace.outbox_deliveries.last_error_code |
| `lease_token` | `text` | 是 | `—` | 02_database_schema_complete.md#workspace.outbox_deliveries.lease_token |
| `lease_until` | `timestamptz` | 是 | `—` | 02_database_schema_complete.md#workspace.outbox_deliveries.lease_until |
| `created_at` | `timestamptz` | 否 | `now()` | 02_database_schema_complete.md#workspace.outbox_deliveries.created_at |
| `updated_at` | `timestamptz` | 否 | `now()` | 02_database_schema_complete.md#workspace.outbox_deliveries.updated_at |

约束：
- `PRIMARY KEY (id)`
- `FOREIGN KEY (event_id) REFERENCES workspace.outbox_events (id) ON DELETE RESTRICT`
- `UNIQUE (event_id, consumer_owner)`
- `CHECK (attempt_count >= 0)`
- `CHECK ((lease_token IS NULL) = (lease_until IS NULL))`

索引：
- `{"name": "outbox_deliveries_pending", "columns": "next_attempt_at, id", "unique": false, "where": "acknowledged_at IS NULL"}`

### workspace.inbox_events

去重hash/业务状态/processed_at/后续Outbox同本库事务；乱序不能覆盖新版本

| 列 | SQL类型 | 可NULL | 默认值 | API血缘 / 原始来源 |
| --- | --- | --- | --- | --- |
| `id` | `text` | 否 | `—` | 02_database_schema_complete.md#workspace.inbox_events.id |
| `source_owner` | `text` | 否 | `—` | 02_database_schema_complete.md#workspace.inbox_events.source_owner |
| `source_event_id` | `text` | 否 | `—` | 02_database_schema_complete.md#workspace.inbox_events.source_event_id |
| `event_type` | `text` | 否 | `—` | 02_database_schema_complete.md#workspace.inbox_events.event_type |
| `schema_version` | `integer` | 否 | `—` | 02_database_schema_complete.md#workspace.inbox_events.schema_version |
| `aggregate_type` | `text` | 否 | `—` | 02_database_schema_complete.md#workspace.inbox_events.aggregate_type |
| `aggregate_id` | `text` | 否 | `—` | 02_database_schema_complete.md#workspace.inbox_events.aggregate_id |
| `aggregate_revision` | `bigint` | 否 | `—` | 02_database_schema_complete.md#workspace.inbox_events.aggregate_revision |
| `payload_sha256` | `text` | 否 | `—` | 02_database_schema_complete.md#workspace.inbox_events.payload_sha256 |
| `payload` | `jsonb` | 否 | `—` | 02_database_schema_complete.md#workspace.inbox_events.payload |
| `received_at` | `timestamptz` | 否 | `now()` | 02_database_schema_complete.md#workspace.inbox_events.received_at |
| `processed_at` | `timestamptz` | 是 | `—` | 02_database_schema_complete.md#workspace.inbox_events.processed_at |
| `result_resource_id` | `text` | 是 | `—` | 02_database_schema_complete.md#workspace.inbox_events.result_resource_id |
| `error_code` | `text` | 是 | `—` | 02_database_schema_complete.md#workspace.inbox_events.error_code |

约束：
- `PRIMARY KEY (id)`
- `UNIQUE (source_owner, source_event_id)`
- `CHECK (schema_version > 0 AND aggregate_revision >= 0)`
- `CHECK (payload_sha256 ~ '^[0-9a-f]{64}$')`

索引：
- `{"name": "inbox_events_pending", "columns": "received_at, id", "unique": false, "where": "processed_at IS NULL"}`
- `{"name": "inbox_events_aggregate", "columns": "source_owner, aggregate_type, aggregate_id, aggregate_revision", "unique": false, "where": null}`

### workspace.idempotency_records

命令创建同事务；operation_name是API操作名不是新Operation ID；安全响应无Key明文；义务存续无TTL清理

| 列 | SQL类型 | 可NULL | 默认值 | API血缘 / 原始来源 |
| --- | --- | --- | --- | --- |
| `id` | `text` | 否 | `—` | 02_database_schema_complete.md#workspace.idempotency_records.id |
| `tenant_scope` | `text` | 否 | `—` | 02_database_schema_complete.md#workspace.idempotency_records.tenant_scope |
| `actor_scope` | `text` | 否 | `—` | 02_database_schema_complete.md#workspace.idempotency_records.actor_scope |
| `operation_name` | `text` | 否 | `—` | 02_database_schema_complete.md#workspace.idempotency_records.operation_name |
| `idempotency_key` | `text` | 否 | `—` | 02_database_schema_complete.md#workspace.idempotency_records.idempotency_key |
| `request_sha256` | `text` | 否 | `—` | 02_database_schema_complete.md#workspace.idempotency_records.request_sha256 |
| `resource_id` | `text` | 否 | `—` | 02_database_schema_complete.md#workspace.idempotency_records.resource_id |
| `operation_id` | `text` | 是 | `—` | 02_database_schema_complete.md#workspace.idempotency_records.operation_id |
| `response_status` | `integer` | 否 | `—` | 02_database_schema_complete.md#workspace.idempotency_records.response_status |
| `response_body` | `jsonb` | 否 | `—` | 02_database_schema_complete.md#workspace.idempotency_records.response_body |
| `created_at` | `timestamptz` | 否 | `now()` | 02_database_schema_complete.md#workspace.idempotency_records.created_at |

约束：
- `PRIMARY KEY (id)`
- `UNIQUE (tenant_scope, actor_scope, operation_name, idempotency_key)`
- `CHECK (request_sha256 ~ '^[0-9a-f]{64}$')`
- `CHECK (response_status BETWEEN 100 AND 599)`

索引：
- `{"name": "idempotency_records_resource", "columns": "resource_id", "unique": false, "where": null}`

### workspace.operations

目标Owner持异步Operation；BFF路由无中央writer；Build创建201回Job；只有Workspace配Saga

| 列 | SQL类型 | 可NULL | 默认值 | API血缘 / 原始来源 |
| --- | --- | --- | --- | --- |
| `id` | `text` | 否 | `—` | 02_database_schema_complete.md#workspace.operations.id |
| `tenant_id` | `text` | 是 | `—` | 02_database_schema_complete.md#workspace.operations.tenant_id |
| `actor_id` | `text` | 否 | `—` | 02_database_schema_complete.md#workspace.operations.actor_id |
| `kind` | `text` | 否 | `—` | 02_database_schema_complete.md#workspace.operations.kind |
| `resource_id` | `text` | 否 | `—` | 02_database_schema_complete.md#workspace.operations.resource_id |
| `status` | `text` | 否 | `'accepted'` | 02_database_schema_complete.md#workspace.operations.status |
| `stage` | `text` | 否 | `—` | 02_database_schema_complete.md#workspace.operations.stage |
| `error_code` | `text` | 是 | `—` | 02_database_schema_complete.md#workspace.operations.error_code |
| `observation_result` | `text` | 是 | `—` | 02_database_schema_complete.md#workspace.operations.observation_result |
| `request_id` | `text` | 否 | `—` | 02_database_schema_complete.md#workspace.operations.request_id |
| `accepted_input` | `jsonb` | 否 | `—` | 02_database_schema_complete.md#workspace.operations.accepted_input |
| `result` | `jsonb` | 是 | `—` | 02_database_schema_complete.md#workspace.operations.result |
| `worker_lease_token` | `text` | 是 | `—` | 02_database_schema_complete.md#workspace.operations.worker_lease_token |
| `worker_lease_until` | `timestamptz` | 是 | `—` | 02_database_schema_complete.md#workspace.operations.worker_lease_until |
| `started_at` | `timestamptz` | 是 | `—` | 02_database_schema_complete.md#workspace.operations.started_at |
| `completed_at` | `timestamptz` | 是 | `—` | 02_database_schema_complete.md#workspace.operations.completed_at |
| `created_at` | `timestamptz` | 否 | `now()` | 02_database_schema_complete.md#workspace.operations.created_at |
| `updated_at` | `timestamptz` | 否 | `now()` | 02_database_schema_complete.md#workspace.operations.updated_at |

约束：
- `PRIMARY KEY (id)`
- `CHECK (status IN ('accepted','running','awaiting_confirmation','succeeded','failed','needs_attention','cancelled'))`
- `CHECK (observation_result IN ('confirmed','rejected','unknown'))`
- `CHECK ((worker_lease_token IS NULL) = (worker_lease_until IS NULL))`
- `CHECK (status <> 'succeeded' OR completed_at IS NOT NULL)`

索引：
- `{"name": "operations_resource", "columns": "resource_id, created_at DESC, id DESC", "unique": false, "where": null}`
- `{"name": "operations_tenant_list", "columns": "tenant_id, created_at DESC, id DESC", "unique": false, "where": null}`
- `{"name": "operations_recovery", "columns": "status, updated_at", "unique": false, "where": null}`

### workspace.plan_changes

D17 PlanChange is the sole active/scheduled resource-change identity; upgrade uses fixed UnixMilli quote basis, downgrade is inert until E and paid next-period obligation; Operation IDs do not revive terminal operations

| 列 | SQL类型 | 可NULL | 默认值 | API血缘 / 原始来源 |
| --- | --- | --- | --- | --- |
| `id` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/PlanChange/properties/id |
| `workspace_id` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/PlanChange/properties/workspaceId |
| `tenant_id` | `text` | 否 | `—` | 02_database_schema_complete.md#workspace.plan_changes.tenant_id |
| `kind` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/PlanChange/properties/kind |
| `status` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/PlanChange/properties/status |
| `source_compute_plan_id` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/PlanChange/properties/sourceComputePlanId |
| `source_storage_plan_id` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/PlanChange/properties/sourceStoragePlanId |
| `target_compute_plan_id` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/PlanChange/properties/targetComputePlanId |
| `target_storage_plan_id` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/PlanChange/properties/targetStoragePlanId |
| `source_price_policy_version_id` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/PlanChange/properties/sourcePricePolicyVersionId |
| `target_price_policy_version_id` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/PlanChange/properties/targetPricePolicyVersionId |
| `source_subscription_id` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/PlanChange/properties/sourceSubscriptionId |
| `source_subscription_version` | `bigint` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/PlanChange/properties/sourceSubscriptionVersion |
| `source_period_id` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/PlanChange/properties/sourcePeriodId |
| `quote_id` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/PlanChange/properties/quoteId |
| `policy_version` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/PlanChange/properties/policyVersion |
| `quote_at` | `timestamptz` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/PlanChange/properties/quoteAt |
| `period_start` | `timestamptz` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/PlanChange/properties/periodStart |
| `period_end` | `timestamptz` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/PlanChange/properties/periodEnd |
| `source_monthly_usd_micros` | `bigint` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/PlanChange/properties/sourceMonthlyUSDMicros |
| `target_monthly_usd_micros` | `bigint` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/PlanChange/properties/targetMonthlyUSDMicros |
| `charge_usd_micros` | `bigint` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/PlanChange/properties/chargeUSDMicros |
| `planned_effective_at` | `timestamptz` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/PlanChange/properties/plannedEffectiveAt |
| `applied_at` | `timestamptz` | 是 | `—` | 03_api_contract_complete.yaml#/components/schemas/PlanChange/properties/appliedAt |
| `operation_id` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/PlanChange/properties/operationId |
| `execution_operation_id` | `text` | 是 | `—` | 03_api_contract_complete.yaml#/components/schemas/PlanChange/properties/executionOperationId |
| `cancellation_operation_id` | `text` | 是 | `—` | 03_api_contract_complete.yaml#/components/schemas/PlanChange/properties/cancellationOperationId |
| `charge_operation_id` | `text` | 是 | `—` | 03_api_contract_complete.yaml#/components/schemas/PlanChange/properties/chargeOperationId |
| `next_period_obligation_id` | `text` | 是 | `—` | 03_api_contract_complete.yaml#/components/schemas/PlanChange/properties/nextPeriodObligationId |
| `next_period_start` | `timestamptz` | 是 | `—` | 03_api_contract_complete.yaml#/components/schemas/PlanChange/properties/nextPeriodStart |
| `next_period_end` | `timestamptz` | 是 | `—` | 03_api_contract_complete.yaml#/components/schemas/PlanChange/properties/nextPeriodEnd |
| `next_period_charge_usd_micros` | `bigint` | 是 | `—` | 03_api_contract_complete.yaml#/components/schemas/PlanChange/properties/nextPeriodChargeUSDMicros |
| `schedule_version` | `bigint` | 否 | `0` | 03_api_contract_complete.yaml#/components/schemas/PlanChange/properties/scheduleVersion |
| `observation_result` | `text` | 否 | `—` | 02_database_schema_complete.md#workspace.plan_changes.observation_result |
| `accepted_calculation` | `jsonb` | 否 | `—` | 02_database_schema_complete.md#workspace.plan_changes.accepted_calculation |
| `actual_outcome` | `jsonb` | 是 | `—` | 02_database_schema_complete.md#workspace.plan_changes.actual_outcome |
| `error_code` | `text` | 是 | `—` | 03_api_contract_complete.yaml#/components/schemas/PlanChange/properties/errorCode |
| `cancelled_at` | `timestamptz` | 是 | `—` | 02_database_schema_complete.md#workspace.plan_changes.cancelled_at |
| `created_at` | `timestamptz` | 否 | `now()` | 03_api_contract_complete.yaml#/components/schemas/PlanChange/properties/createdAt |
| `updated_at` | `timestamptz` | 否 | `now()` | 03_api_contract_complete.yaml#/components/schemas/PlanChange/properties/updatedAt |
| `execution_plan_id` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/PlanChange/properties/executionPlanId |
| `execution_plan_digest` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/PlanChange/properties/executionPlanDigest |
| `quote_at_ms` | `bigint` | 否 | `—` | 02_database_schema_complete.md#workspace.plan_changes.quote_at_ms |
| `period_start_ms` | `bigint` | 否 | `—` | 02_database_schema_complete.md#workspace.plan_changes.period_start_ms |
| `period_end_ms` | `bigint` | 否 | `—` | 02_database_schema_complete.md#workspace.plan_changes.period_end_ms |
| `source_financial_snapshot_digest` | `text` | 否 | `—` | 02_database_schema_complete.md#workspace.plan_changes.source_financial_snapshot_digest |
| `source_financial_snapshot_bytes` | `bytea` | 否 | `—` | 02_database_schema_complete.md#workspace.plan_changes.source_financial_snapshot_bytes |

约束：
- `PRIMARY KEY (id)`
- `FOREIGN KEY (workspace_id, tenant_id) REFERENCES workspace.workspaces (id, tenant_id) ON DELETE RESTRICT`
- `FOREIGN KEY (source_subscription_id, workspace_id) REFERENCES workspace.subscriptions (id, workspace_id) ON DELETE RESTRICT`
- `FOREIGN KEY (source_period_id, source_subscription_id) REFERENCES workspace.subscription_periods (id, subscription_id) ON DELETE RESTRICT`
- `FOREIGN KEY (operation_id) REFERENCES workspace.operations (id) ON DELETE RESTRICT`
- `FOREIGN KEY (execution_operation_id) REFERENCES workspace.operations (id) ON DELETE RESTRICT`
- `FOREIGN KEY (cancellation_operation_id) REFERENCES workspace.operations (id) ON DELETE RESTRICT`
- `CHECK (kind IN ('upgrade_immediate','downgrade_next_period'))`
- `CHECK (status IN ('requested','scheduled','awaiting_payment','applying','applied','failed','needs_attention','cancelled'))`
- `CHECK (policy_version IN ('workspace-plan-change-v1'))`
- `CHECK (observation_result IN ('confirmed','rejected','unknown'))`
- `CHECK (source_subscription_version >= 0 AND schedule_version >= 0)`
- `CHECK (source_monthly_usd_micros >= 0 AND target_monthly_usd_micros >= 0 AND charge_usd_micros >= 0)`
- `CHECK ((kind = 'upgrade_immediate' AND planned_effective_at = quote_at AND num_nonnulls(next_period_start,next_period_end,next_period_charge_usd_micros) = 0) OR (kind = 'downgrade_next_period' AND planned_effective_at = period_end AND next_period_start = period_end AND next_period_end > next_period_start AND next_period_charge_usd_micros = target_monthly_usd_micros AND charge_usd_micros = 0))`
- `CHECK ((status = 'applied') = (applied_at IS NOT NULL))`
- `CHECK ((status = 'cancelled') = (cancelled_at IS NOT NULL))`
- `UNIQUE (quote_id)`
- `UNIQUE (id, workspace_id)`
- `FOREIGN KEY (next_period_obligation_id) REFERENCES workspace.subscription_period_obligations (id) ON DELETE RESTRICT`
- `CHECK (date_trunc('milliseconds',quote_at) = quote_at)`
- `CHECK (execution_plan_digest ~ '^sha256:[0-9a-f]{64}$')`
- `CHECK (period_end > period_start)`
- `CHECK (period_end_ms > period_start_ms AND period_end_ms-period_start_ms <= 2678400000)`
- `CHECK (quote_at_ms >= period_start_ms AND quote_at_ms < period_end_ms)`
- `CHECK (source_financial_snapshot_digest = 'sha256:' \|\| encode(sha256(source_financial_snapshot_bytes),'hex'))`
- `CHECK (kind <> 'upgrade_immediate' OR charge_usd_micros = ceil(greatest(target_monthly_usd_micros-source_monthly_usd_micros,0)::numeric * (period_end_ms-quote_at_ms)::numeric / (period_end_ms-period_start_ms)::numeric))`

索引：
- `{"name": "plan_changes_one_unfinished", "columns": "workspace_id", "unique": true, "where": "status IN ('requested','scheduled','awaiting_payment','applying','needs_attention')"}`
- `{"name": "plan_changes_schedule", "columns": "planned_effective_at, id", "unique": false, "where": "status IN ('scheduled','awaiting_payment')"}`
- `{"name": "plan_changes_workspace", "columns": "workspace_id, created_at DESC, id DESC", "unique": false, "where": null}`

### workspace.subscription_period_obligations

One original next-period obligation before payment confirmation, shared by manual/automatic/boundary actors; target accepted price is fixed; not a wallet or funds-reservation service

| 列 | SQL类型 | 可NULL | 默认值 | API血缘 / 原始来源 |
| --- | --- | --- | --- | --- |
| `id` | `text` | 否 | `—` | 02_database_schema_complete.md#workspace.subscription_period_obligations.id |
| `subscription_id` | `text` | 否 | `—` | 02_database_schema_complete.md#workspace.subscription_period_obligations.subscription_id |
| `workspace_id` | `text` | 否 | `—` | 02_database_schema_complete.md#workspace.subscription_period_obligations.workspace_id |
| `plan_change_id` | `text` | 是 | `—` | 02_database_schema_complete.md#workspace.subscription_period_obligations.plan_change_id |
| `period_start` | `timestamptz` | 否 | `—` | 02_database_schema_complete.md#workspace.subscription_period_obligations.period_start |
| `period_end` | `timestamptz` | 否 | `—` | 02_database_schema_complete.md#workspace.subscription_period_obligations.period_end |
| `target_compute_plan_id` | `text` | 否 | `—` | 02_database_schema_complete.md#workspace.subscription_period_obligations.target_compute_plan_id |
| `target_storage_plan_id` | `text` | 否 | `—` | 02_database_schema_complete.md#workspace.subscription_period_obligations.target_storage_plan_id |
| `target_price_policy_version_id` | `text` | 否 | `—` | 02_database_schema_complete.md#workspace.subscription_period_obligations.target_price_policy_version_id |
| `amount_usd_micros` | `bigint` | 否 | `—` | 02_database_schema_complete.md#workspace.subscription_period_obligations.amount_usd_micros |
| `accepted_pricing_snapshot` | `jsonb` | 否 | `—` | 02_database_schema_complete.md#workspace.subscription_period_obligations.accepted_pricing_snapshot |
| `status` | `text` | 否 | `—` | 02_database_schema_complete.md#workspace.subscription_period_obligations.status |
| `operation_id` | `text` | 是 | `—` | 02_database_schema_complete.md#workspace.subscription_period_obligations.operation_id |
| `wallet_operation_id` | `text` | 是 | `—` | 02_database_schema_complete.md#workspace.subscription_period_obligations.wallet_operation_id |
| `billing_key` | `text` | 否 | `—` | 02_database_schema_complete.md#workspace.subscription_period_obligations.billing_key |
| `payment_accepted_at` | `timestamptz` | 是 | `—` | 02_database_schema_complete.md#workspace.subscription_period_obligations.payment_accepted_at |
| `resource_execution_started_at` | `timestamptz` | 是 | `—` | 02_database_schema_complete.md#workspace.subscription_period_obligations.resource_execution_started_at |
| `confirmed_at` | `timestamptz` | 是 | `—` | 02_database_schema_complete.md#workspace.subscription_period_obligations.confirmed_at |
| `error_code` | `text` | 是 | `—` | 02_database_schema_complete.md#workspace.subscription_period_obligations.error_code |
| `created_at` | `timestamptz` | 否 | `now()` | 02_database_schema_complete.md#workspace.subscription_period_obligations.created_at |
| `updated_at` | `timestamptz` | 否 | `now()` | 02_database_schema_complete.md#workspace.subscription_period_obligations.updated_at |
| `version` | `bigint` | 否 | `0` | 02_database_schema_complete.md#workspace.subscription_period_obligations.version |
| `quote_id` | `text` | 否 | `—` | 02_database_schema_complete.md#workspace.subscription_period_obligations.quote_id |
| `confirmed_subscription_version` | `bigint` | 是 | `—` | 02_database_schema_complete.md#workspace.subscription_period_obligations.confirmed_subscription_version |

约束：
- `PRIMARY KEY (id)`
- `FOREIGN KEY (subscription_id, workspace_id) REFERENCES workspace.subscriptions (id, workspace_id) ON DELETE RESTRICT`
- `FOREIGN KEY (plan_change_id, workspace_id) REFERENCES workspace.plan_changes (id, workspace_id) ON DELETE RESTRICT`
- `FOREIGN KEY (operation_id) REFERENCES workspace.operations (id) ON DELETE RESTRICT`
- `UNIQUE (subscription_id, period_start)`
- `UNIQUE (billing_key)`
- `CHECK (period_end > period_start)`
- `CHECK (amount_usd_micros >= 0)`
- `CHECK (status IN ('awaiting_payment','accepted','confirmed','failed','needs_attention'))`
- `CHECK (status <> 'confirmed' OR confirmed_at IS NOT NULL)`
- `CHECK (wallet_operation_id IS NULL OR payment_accepted_at IS NOT NULL)`
- `CHECK (version >= 0)`
- `CHECK (confirmed_subscription_version IS NULL OR (confirmed_subscription_version >= 0 AND status = 'confirmed'))`

索引：
- `{"name": "period_obligations_workspace", "columns": "workspace_id, period_start", "unique": false, "where": null}`
- `{"name": "period_obligations_pending", "columns": "period_start, id", "unique": false, "where": "status IN ('awaiting_payment','accepted','needs_attention')"}`

### workspace.supplemental_charges

Immutable successful-upgrade supplement coverage T..E; original Gateway charge identity retained; later deletion refunds this coverage, not base-order 720-hour policy

| 列 | SQL类型 | 可NULL | 默认值 | API血缘 / 原始来源 |
| --- | --- | --- | --- | --- |
| `id` | `text` | 否 | `—` | 02_database_schema_complete.md#workspace.supplemental_charges.id |
| `plan_change_id` | `text` | 否 | `—` | 02_database_schema_complete.md#workspace.supplemental_charges.plan_change_id |
| `subscription_period_id` | `text` | 否 | `—` | 02_database_schema_complete.md#workspace.supplemental_charges.subscription_period_id |
| `workspace_id` | `text` | 否 | `—` | 02_database_schema_complete.md#workspace.supplemental_charges.workspace_id |
| `quote_id` | `text` | 否 | `—` | 02_database_schema_complete.md#workspace.supplemental_charges.quote_id |
| `policy_version` | `text` | 否 | `—` | 02_database_schema_complete.md#workspace.supplemental_charges.policy_version |
| `coverage_start` | `timestamptz` | 否 | `—` | 02_database_schema_complete.md#workspace.supplemental_charges.coverage_start |
| `coverage_end` | `timestamptz` | 否 | `—` | 02_database_schema_complete.md#workspace.supplemental_charges.coverage_end |
| `confirmed_amount_usd_micros` | `bigint` | 否 | `—` | 02_database_schema_complete.md#workspace.supplemental_charges.confirmed_amount_usd_micros |
| `original_wallet_operation_id` | `text` | 是 | `—` | 02_database_schema_complete.md#workspace.supplemental_charges.original_wallet_operation_id |
| `charge_receipt_id` | `text` | 否 | `—` | 02_database_schema_complete.md#workspace.supplemental_charges.charge_receipt_id |
| `confirmed_at` | `timestamptz` | 否 | `—` | 02_database_schema_complete.md#workspace.supplemental_charges.confirmed_at |
| `created_at` | `timestamptz` | 否 | `now()` | 02_database_schema_complete.md#workspace.supplemental_charges.created_at |
| `coverage_start_ms` | `bigint` | 否 | `—` | 02_database_schema_complete.md#workspace.supplemental_charges.coverage_start_ms |
| `coverage_end_ms` | `bigint` | 否 | `—` | 02_database_schema_complete.md#workspace.supplemental_charges.coverage_end_ms |
| `source_financial_snapshot_digest` | `text` | 否 | `—` | 02_database_schema_complete.md#workspace.supplemental_charges.source_financial_snapshot_digest |

约束：
- `PRIMARY KEY (id)`
- `FOREIGN KEY (plan_change_id, workspace_id) REFERENCES workspace.plan_changes (id, workspace_id) ON DELETE RESTRICT`
- `FOREIGN KEY (subscription_period_id) REFERENCES workspace.subscription_periods (id) ON DELETE RESTRICT`
- `CHECK (policy_version IN ('workspace-plan-change-v1'))`
- `UNIQUE (plan_change_id)`
- `UNIQUE (original_wallet_operation_id)`
- `CHECK (coverage_end > coverage_start)`
- `CHECK (confirmed_amount_usd_micros >= 0)`
- `CHECK ((confirmed_amount_usd_micros = 0) = (original_wallet_operation_id IS NULL))`
- `CHECK (coverage_end_ms > coverage_start_ms)`
- `CHECK (source_financial_snapshot_digest ~ '^sha256:[0-9a-f]{64}$')`

索引：
- `{"name": "supplements_workspace", "columns": "workspace_id, created_at DESC, id DESC", "unique": false, "where": null}`
- `{"name": "supplements_period", "columns": "subscription_period_id", "unique": false, "where": null}`

### 本Owner应实现的RPC方法全集（目标，不是挂载证据）

共享OwnerOperations/CommitReadback/Inbox分别由本域实现，不形成中央业务服务；通用Operation REST由BFF按显式Owner路由；本域只负责自己的Operation与授权。

| RPC | 请求 | 响应 |
| --- | --- | --- |
| `WorkspaceProductService.CreateWorkspace` | [CreateWorkspaceRpcRequest](rpc-messages.md#createworkspacerpcrequest) | [Operation](rpc-messages.md#operation) |
| `WorkspaceProductService.ListWorkspaces` | [ListWorkspacesRpcRequest](rpc-messages.md#listworkspacesrpcrequest) | [WorkspacePage](rpc-messages.md#workspacepage) |
| `WorkspaceProductService.GetWorkspace` | [GetWorkspaceRpcRequest](rpc-messages.md#getworkspacerpcrequest) | [Workspace](rpc-messages.md#workspace) |
| `WorkspaceProductService.DeleteWorkspace` | [DeleteWorkspaceRpcRequest](rpc-messages.md#deleteworkspacerpcrequest) | [Operation](rpc-messages.md#operation) |
| `WorkspaceProductService.GetWorkspaceAccess` | [GetWorkspaceAccessRpcRequest](rpc-messages.md#getworkspaceaccessrpcrequest) | [WorkspaceAccess](rpc-messages.md#workspaceaccess) |
| `WorkspaceProductService.GetWorkspaceModels` | [GetWorkspaceModelsRpcRequest](rpc-messages.md#getworkspacemodelsrpcrequest) | [ModelConfiguration](rpc-messages.md#modelconfiguration) |
| `WorkspaceProductService.UpdateWorkspaceModels` | [UpdateWorkspaceModelsRpcRequest](rpc-messages.md#updateworkspacemodelsrpcrequest) | [Operation](rpc-messages.md#operation) |
| `WorkspaceProductService.ListDeployments` | [ListDeploymentsRpcRequest](rpc-messages.md#listdeploymentsrpcrequest) | [DeploymentPage](rpc-messages.md#deploymentpage) |
| `WorkspaceProductService.GetDeployment` | [GetDeploymentRpcRequest](rpc-messages.md#getdeploymentrpcrequest) | [Deployment](rpc-messages.md#deployment) |
| `WorkspaceProductService.UpdateWorkspaceVersion` | [UpdateWorkspaceVersionRpcRequest](rpc-messages.md#updateworkspaceversionrpcrequest) | [Operation](rpc-messages.md#operation) |
| `WorkspaceProductService.RollbackWorkspace` | [RollbackWorkspaceRpcRequest](rpc-messages.md#rollbackworkspacerpcrequest) | [Operation](rpc-messages.md#operation) |
| `WorkspaceProductService.ResizeWorkspace` | [ResizeWorkspaceRpcRequest](rpc-messages.md#resizeworkspacerpcrequest) | [Operation](rpc-messages.md#operation) |
| `WorkspaceProductService.RenewWorkspace` | [RenewWorkspaceRpcRequest](rpc-messages.md#renewworkspacerpcrequest) | [Operation](rpc-messages.md#operation) |
| `WorkspaceProductService.GetSubscription` | [GetSubscriptionRpcRequest](rpc-messages.md#getsubscriptionrpcrequest) | [Subscription](rpc-messages.md#subscription) |
| `WorkspaceProductService.GetWorkspaceDeletion` | [GetWorkspaceDeletionRpcRequest](rpc-messages.md#getworkspacedeletionrpcrequest) | [WorkspaceDeletion](rpc-messages.md#workspacedeletion) |
| `WorkspaceProductService.GetOperation` | [GetOperationRpcRequest](rpc-messages.md#getoperationrpcrequest) | [Operation](rpc-messages.md#operation) |
| `WorkspaceProductService.ListAdminOperations` | [ListAdminOperationsRpcRequest](rpc-messages.md#listadminoperationsrpcrequest) | [AdminOperationPage](rpc-messages.md#adminoperationpage) |
| `WorkspaceProductService.ReconcileOperation` | [ReconcileOperationRpcRequest](rpc-messages.md#reconcileoperationrpcrequest) | [Operation](rpc-messages.md#operation) |
| `WorkspaceProductService.AdoptWorkspace` | [AdoptWorkspaceRpcRequest](rpc-messages.md#adoptworkspacerpcrequest) | [Operation](rpc-messages.md#operation) |
| `WorkspaceProductService.UpdateRenewalSettings` | [UpdateRenewalSettingsRpcRequest](rpc-messages.md#updaterenewalsettingsrpcrequest) | [Operation](rpc-messages.md#operation) |
| `WorkspaceProductService.RevealWorkspaceApplicationCredentials` | [RevealWorkspaceApplicationCredentialsRpcRequest](rpc-messages.md#revealworkspaceapplicationcredentialsrpcrequest) | [WorkspaceApplicationCredentials](rpc-messages.md#workspaceapplicationcredentials) |
| `WorkspaceProductService.ListPlanChanges` | [ListPlanChangesRpcRequest](rpc-messages.md#listplanchangesrpcrequest) | [PlanChangePage](rpc-messages.md#planchangepage) |
| `WorkspaceProductService.GetPlanChange` | [GetPlanChangeRpcRequest](rpc-messages.md#getplanchangerpcrequest) | [PlanChange](rpc-messages.md#planchange) |
| `WorkspaceProductService.CancelPlanChange` | [CancelPlanChangeRpcRequest](rpc-messages.md#cancelplanchangerpcrequest) | [Operation](rpc-messages.md#operation) |
| `ClaimUsageReadback.ReadClaimUsage` | [ReadClaimUsageRequest](rpc-messages.md#readclaimusagerequest) | [ClaimUsageEvidence](rpc-messages.md#claimusageevidence) |
| `OwnerOperations.Read` | [OwnerOperationRequest](rpc-messages.md#owneroperationrequest) | [Operation](rpc-messages.md#operation) |
| `OwnerOperations.Reconcile` | [ReconcileOperationRpcRequest](rpc-messages.md#reconcileoperationrpcrequest) | [Operation](rpc-messages.md#operation) |
| `OwnerCommitReadback.ReadOwnerCommit` | [ReadOwnerCommitRequest](rpc-messages.md#readownercommitrequest) | [OwnerCommitEvidence](rpc-messages.md#ownercommitevidence) |
| `WorkspaceAuthorizationReadback.ReadRenewalConsent` | [ReadRenewalConsentRequest](rpc-messages.md#readrenewalconsentrequest) | [RenewalConsentReadback](rpc-messages.md#renewalconsentreadback) |
| `WorkspaceAdmission.CheckAdmission` | [AdmissionRequest](rpc-messages.md#admissionrequest) | [AdmissionResult](rpc-messages.md#admissionresult) |
| `TenantWorkspaceCoordination.SuspendTenantWorkspaces` | [TenantWorkspaceLifecycleCommand](rpc-messages.md#tenantworkspacelifecyclecommand) | [TenantWorkspaceLifecycleReadback](rpc-messages.md#tenantworkspacelifecyclereadback) |
| `TenantWorkspaceCoordination.DeleteTenantWorkspaces` | [TenantWorkspaceLifecycleCommand](rpc-messages.md#tenantworkspacelifecyclecommand) | [TenantWorkspaceLifecycleReadback](rpc-messages.md#tenantworkspacelifecyclereadback) |
| `TenantWorkspaceCoordination.ResumeTenantWorkspaces` | [ResumeTenantWorkspacesRequest](rpc-messages.md#resumetenantworkspacesrequest) | [TenantWorkspaceLifecycleReadback](rpc-messages.md#tenantworkspacelifecyclereadback) |
| `WorkspacePlanChangeReadback.ReadSubscriptionPlanState` | [ReadSubscriptionPlanStateRequest](rpc-messages.md#readsubscriptionplanstaterequest) | [SubscriptionPlanState](rpc-messages.md#subscriptionplanstate) |
| `WorkspacePlanChangeReadback.ReadPlanChange` | [ReadPlanChangeRequest](rpc-messages.md#readplanchangerequest) | [PlanChange](rpc-messages.md#planchange) |
| `WorkspacePlanChangeReadback.ReadNextPeriodObligation` | [ReadNextPeriodObligationRequest](rpc-messages.md#readnextperiodobligationrequest) | [NextPeriodObligation](rpc-messages.md#nextperiodobligation) |
| `WorkspacePlanChangeReadback.ReadPlanChangeFailure` | [ReadPlanChangeFailureRequest](rpc-messages.md#readplanchangefailurerequest) | [PlanChangeEvidence](rpc-messages.md#planchangeevidence) |
| `DomainInbox.Deliver` | [DeliverEventRequest](rpc-messages.md#delivereventrequest) | [InboxAck](rpc-messages.md#inboxack) |

## 4. 跨域调用：调用者 → 拥有方 → 字段 → 结果

以下只列`domain_flows.json`声明的业务边；共享通道/尚无业务边的RPC不能推断成已经实现。


### F07.1 workspace → tenant / CloudIdentityAuthorization.AuthorizeAction

按当前入口/阶段与06/13状态机前置执行；不是无条件调用

请求 [AuthorizationRequest](rpc-messages.md#authorizationrequest)：scope#1: AuthorizationScope；actor_id#2: string；session_id#3: optional string；authorization_context_id#4: optional string；accepted_operation_grant_id#5: optional string；audience_owner#6: OwnerEnum；action#7: AuthorizationActionEnum；resource#8: AuthorizationResource；expected_permission_version#9: optional int64；request_id#10: string

返回 [AuthorizationDecision](rpc-messages.md#authorizationdecision)：result#1: AuthorizationResult；issuer#2: AuthorizationIssuer；authorization_context_id#3: optional string；scope#4: AuthorizationScope；actor_id#5: string；session_id#6: optional string；audience_owner#7: OwnerEnum；action#8: AuthorizationActionEnum；resource#9: AuthorizationResource；permission_version#10: int64；issued_at#11: Timestamp；expires_at#12: Timestamp；denial_code#13: optional ErrorCodeEnum；accepted_operation_grant_id#14: optional string

接收方写入：

完成证据：session/grant/action/resource/audience/权限版本确切一致

失败/未知：deny不改用管理员；unknown不发后续副作用

### F07.2 resource_catalog → workspace / WorkspaceAdmission.CheckAdmission

按当前入口/阶段与06/13状态机前置执行；不是无条件调用

请求 [AdmissionRequest](rpc-messages.md#admissionrequest)：context#1: CallContext；workspace_id#2: string；capability_version_id#3: string；compute_plan_id#4: string；storage_plan_id#5: string；model_selections#6: repeated ModelSelection；purpose#7: string

返回 [AdmissionResult](rpc-messages.md#admissionresult)：outcome#1: Observation；admission_id#2: string；capability_snapshot_digest#3: string；provider_capability_version#4: string；policy_version_id#5: string；error_code#6: string；expires_at#7: Timestamp；expected_interruption#8: string

接收方写入：

完成证据：当前版本、模型、作用域与原Workspace义务确认

失败/未知：quote并不等于容量预留，提交时复查

### F07.3 workspace → fabric / FabricCoordination.AdmitResources

按当前入口/阶段与06/13状态机前置执行；不是无条件调用

请求 [ResourceAdmissionRequest](rpc-messages.md#resourceadmissionrequest)：context#1: CallContext；plan#2: ResourcePlanSnapshot；workspace_id#3: string；existing_resource_set_id#4: string；purpose#5: string

返回 [AdmissionResult](rpc-messages.md#admissionresult)：outcome#1: Observation；admission_id#2: string；capability_snapshot_digest#3: string；provider_capability_version#4: string；policy_version_id#5: string；error_code#6: string；expires_at#7: Timestamp；expected_interruption#8: string

接收方写入：

完成证据：provider能力、实际资源/route CAS能力准入

失败/未知：不能静默换provider或降能力

### F08.1 workspace → tenant / CloudIdentityAuthorization.AuthorizeAction

按当前入口/阶段与06/13状态机前置执行；不是无条件调用

请求 [AuthorizationRequest](rpc-messages.md#authorizationrequest)：scope#1: AuthorizationScope；actor_id#2: string；session_id#3: optional string；authorization_context_id#4: optional string；accepted_operation_grant_id#5: optional string；audience_owner#6: OwnerEnum；action#7: AuthorizationActionEnum；resource#8: AuthorizationResource；expected_permission_version#9: optional int64；request_id#10: string

返回 [AuthorizationDecision](rpc-messages.md#authorizationdecision)：result#1: AuthorizationResult；issuer#2: AuthorizationIssuer；authorization_context_id#3: optional string；scope#4: AuthorizationScope；actor_id#5: string；session_id#6: optional string；audience_owner#7: OwnerEnum；action#8: AuthorizationActionEnum；resource#9: AuthorizationResource；permission_version#10: int64；issued_at#11: Timestamp；expires_at#12: Timestamp；denial_code#13: optional ErrorCodeEnum；accepted_operation_grant_id#14: optional string

接收方写入：

完成证据：session/grant/action/resource/audience/权限版本确切一致

失败/未知：deny不改用管理员；unknown不发后续副作用

### F08.2 workspace → resource_catalog / CatalogCoordination.AcceptQuote

按当前入口/阶段与06/13状态机前置执行；不是无条件调用

请求 [AcceptQuoteRequest](rpc-messages.md#acceptquoterequest)：context#1: CallContext；quote_id#2: string；workspace_id#3: string；obligation_id#4: string；admission_id#5: string；plan_change_id#6: optional string；source_subscription_version#7: optional int64；subscription_period_obligation_id#8: optional string

返回 [QuoteAcceptance](rpc-messages.md#quoteacceptance)：quote#1: Quote；obligation_id#2: string；acceptance_id#3: string；snapshot_digest#4: string；source_financial_snapshot_bytes#5: optional bytes；source_financial_snapshot_digest#6: optional string

接收方写入：`resource_catalog.quotes`

完成证据：quoteID/inputDigest唯一绑定原operation

失败/未知：冲突拒绝，不能重报价后续跑原单

### F08.3 workspace → runtime_control / RuntimeCoordination.Reserve

按当前入口/阶段与06/13状态机前置执行；不是无条件调用

请求 [RuntimeReservationCommand](rpc-messages.md#runtimereservationcommand)：context#1: CallContext；workspace_id#2: string；deployment_id#3: string；capability_version_id#4: string；artifact#5: ArtifactReference；deployment_descriptor_digest#6: string；deployment_descriptor_object_ref#7: string

返回 [RuntimeReservation](rpc-messages.md#runtimereservation)：runtime_instance_id#1: string；workspace_id#2: string；deployment_id#3: string；artifact#4: ArtifactReference；deployment_descriptor_digest#5: string；deployment_descriptor_object_ref#6: string

接收方写入：`runtime_control.runtime_instances`

完成证据：预留稳定runtimeInstanceId而不启动

失败/未知：不因Key依赖Runtime ID产生循环

### F08.4 workspace → tenant / CloudIdentityAuthorization.IssueAcceptedOperationGrant

按当前入口/阶段与06/13状态机前置执行；不是无条件调用

请求 [AcceptedOperationGrantRequest](rpc-messages.md#acceptedoperationgrantrequest)：authorization_context_id#1: string；owner_commit_evidence#2: OwnerCommitEvidence；allowed_actions#3: repeated AuthorizationActionEnum；renewal_consent_id#4: optional string；subscription_period_id#5: optional string

返回 [AcceptedOperationGrant](rpc-messages.md#acceptedoperationgrant)：id#1: string；scope#2: AuthorizationScope；actor_id#3: string；accepted_operation_owner#4: OwnerEnum；accepted_operation_id#5: string；accepted_action#6: AuthorizationActionEnum；resource_id#7: string；accepted_permission_version#8: int64；allowed_actions#9: repeated AuthorizationActionEnum；mode#10: AcceptedGrantMode；issued_at#11: Timestamp；expires_at#12: optional Timestamp；revoked_at#13: optional Timestamp；obligation_completed_at#14: optional Timestamp

接收方写入：`tenant.accepted_operation_grants`

完成证据：原Owner commit读回，有限actions/resource/period

失败/未知：不能伪造commit字符串或扩大到新Workspace

### F08.5 workspace → gateway / GatewayCoordination.Debit

按当前入口/阶段与06/13状态机前置执行；不是无条件调用

请求 [WalletDebitCommand](rpc-messages.md#walletdebitcommand)：context#1: CallContext；workspace_id#2: string；obligation_id#3: string；quote_acceptance_id#4: string；amount_usd_micros#5: int64；currency#6: string；subscription_period_id#7: string

返回 [WalletOperation](rpc-messages.md#walletoperation)：id#1: string；workspace_id#2: optional string；kind#3: WalletOperationKindEnum；amount_usd_micros#4: int64；status#5: WalletOperationStatusEnum；external_reference#6: optional string；receipt_id#7: optional string；error_code#8: optional ErrorCodeEnum；created_at#9: Timestamp；updated_at#10: Timestamp；purpose#11: optional WalletOperationPurposeEnum；plan_change_id#12: optional string；original_charge_operation_id#13: optional string；coverage_start#14: Timestamp；coverage_end#15: Timestamp；coverage_start_milliseconds#16: optional int64；coverage_end_milliseconds#17: optional int64

接收方写入：`gateway.wallet_operations`

完成证据：精确账户/Code/金额原单confirmed

失败/未知：unknown只ReadWalletAction，绝不重复扣费

### F08.6 workspace → gateway / GatewayCoordination.CreateManagedKey

按当前入口/阶段与06/13状态机前置执行；不是无条件调用

请求 [ManagedKeyCommand](rpc-messages.md#managedkeycommand)：context#1: CallContext；workspace_id#2: string；model_ids#3: repeated string；target_runtime_instance_id#4: string

返回 [ManagedKeyBinding](rpc-messages.md#managedkeybinding)：key_binding_id#1: string；fingerprint#2: string；secret_delivery_reference#3: string；workspace_id#4: string；target_runtime_instance_id#5: string；expires_at#6: Timestamp

接收方写入：`gateway.key_bindings`

完成证据：原实例与允许模型/Key引用/版本一致

失败/未知：不盲建第二Key，不在DB/事件存明文

### F08.7 workspace → fabric / FabricCoordination.EnsureResources

按当前入口/阶段与06/13状态机前置执行；不是无条件调用

请求 [EnsureResourcesCommand](rpc-messages.md#ensureresourcescommand)：context#1: CallContext；workspace_id#2: string；obligation_id#3: string；plan#4: ResourcePlanSnapshot；confirmed_charge_receipt_id#5: string；instance_authorization_reference#6: string

返回 [Operation](rpc-messages.md#operation)：operation_id#1: string；owner#2: OperationOwnerEnum；kind#3: OperationKindEnum；resource_id#4: string；status#5: OperationStatusEnum；stage#6: OperationStageEnum；observation_result#7: optional OperationObservationResultEnum；error_code#8: optional ErrorCodeEnum；request_id#9: string；created_at#10: Timestamp；updated_at#11: Timestamp；poll_after_seconds#12: optional int32

接收方写入：`fabric.resources`, `fabric.resource_sets`, `fabric.attachments`

完成证据：批准预付资源与Workspace/原请求exact匹配

失败/未知：unknown查原provider动作，不重购

### F08.9 workspace → runtime_control / RuntimeCoordination.Deploy

按当前入口/阶段与06/13状态机前置执行；不是无条件调用

请求 [RuntimeDeployCommand](rpc-messages.md#runtimedeploycommand)：context#1: CallContext；workspace_id#2: string；deployment_id#3: string；capability_version_id#4: string；deployment_descriptor#5: DeploymentDescriptor；resource_set_id#6: string；data_attachment_id#7: string；secret_binding_id#8: string；model_configuration_version#9: int64；model_selections#10: repeated ModelSelection；data_compatibility#11: DataCompatibility；runtime_instance_id#12: string；deployment_descriptor_digest#13: string；execution_epoch#14: int64；deployment_descriptor_object_ref#15: string

返回 [RuntimeReadback](rpc-messages.md#runtimereadback)：runtime_instance_id#1: string；workspace_id#2: string；deployment_id#3: string；state#4: RuntimeInstanceState；process_ready#5: bool；application_available#6: bool；credential_injection_verified#7: bool；artifact#8: ArtifactReference；applied_model_configuration_version#9: int64；readiness_receipt_id#10: string；outcome#11: Observation；observed_at#12: Timestamp；deployment_descriptor_digest#13: string；execution_epoch#14: int64；deployment_descriptor_object_ref#15: string

接收方写入：`runtime_control.runtime_actions`

完成证据：完整DeploymentDescriptor送执行层并实际ready

失败/未知：非就绪不开放入口

### F08.10 workspace → fabric / FabricRouteExecution.FenceRouteEpoch

按当前入口/阶段与06/13状态机前置执行；不是无条件调用

请求 [FenceRouteEpochCommand](rpc-messages.md#fencerouteepochcommand)：context#1: CallContext；workspace_id#2: string；operation_id#3: string；execution_epoch#4: int64；expected_route_generation#5: int64；provider_precondition#6: ProviderRevisionPrecondition

返回 [RouteReadback](rpc-messages.md#routereadback)：workspace_id#1: string；switch_id#2: string；observation#3: Observation；current_generation#4: int64；accepted_execution_epoch#5: int64；target_execution_resource_id#6: optional string；target_runtime_instance_id#7: optional string；target_deployment_id#8: optional string；provider_revision#9: string；provider_command_id#10: string；route_receipt_id#11: optional string；observed_at#12: Timestamp；error_code#13: optional ErrorCodeEnum

接收方写入：`fabric.route_bindings`, `fabric.route_switches`

完成证据：provider conditional revision确认新epoch

失败/未知：未知旧switch先读回，不抢占

### F08.12 workspace → ledger / LedgerCoordination.AppendReceipt

按当前入口/阶段与06/13状态机前置执行；不是无条件调用

请求 [AppendReceiptRequest](rpc-messages.md#appendreceiptrequest)：context#1: CallContext；receipt#2: Receipt；evidence_digest#3: string；owner_evidence_reference#4: string

返回 [Receipt](rpc-messages.md#receipt)：id#1: string；kind#2: ReceiptKindEnum；owner#3: OwnerEnum；source_sha#4: optional string；artifact_digest#5: optional string；operation_id#6: optional string；workflow_run_id#7: optional string；outcome#8: ReceiptOutcomeEnum；evidence_summary#9: string；created_at#10: Timestamp

接收方写入：`ledger.receipts`

完成证据：返回receipt身份，另ReadReceiptByReference核对原输入

失败/未知：同idempotency key读回，不填假Workspace或重写receipt

### F09.1 workspace → tenant / CloudIdentityAuthorization.AuthorizeAction

按当前入口/阶段与06/13状态机前置执行；不是无条件调用

请求 [AuthorizationRequest](rpc-messages.md#authorizationrequest)：scope#1: AuthorizationScope；actor_id#2: string；session_id#3: optional string；authorization_context_id#4: optional string；accepted_operation_grant_id#5: optional string；audience_owner#6: OwnerEnum；action#7: AuthorizationActionEnum；resource#8: AuthorizationResource；expected_permission_version#9: optional int64；request_id#10: string

返回 [AuthorizationDecision](rpc-messages.md#authorizationdecision)：result#1: AuthorizationResult；issuer#2: AuthorizationIssuer；authorization_context_id#3: optional string；scope#4: AuthorizationScope；actor_id#5: string；session_id#6: optional string；audience_owner#7: OwnerEnum；action#8: AuthorizationActionEnum；resource#9: AuthorizationResource；permission_version#10: int64；issued_at#11: Timestamp；expires_at#12: Timestamp；denial_code#13: optional ErrorCodeEnum；accepted_operation_grant_id#14: optional string

接收方写入：

完成证据：session/grant/action/resource/audience/权限版本确切一致

失败/未知：deny不改用管理员；unknown不发后续副作用

### F09.2 bff → workspace / WorkspaceProductService.GetWorkspaceAccess

按当前入口/阶段与06/13状态机前置执行；不是无条件调用

请求 [GetWorkspaceAccessRpcRequest](rpc-messages.md#getworkspaceaccessrpcrequest)：context#1: CallContext；workspace_id#2: string

返回 [WorkspaceAccess](rpc-messages.md#workspaceaccess)：workspace_id#1: string；url#2: string；authentication_mode#3: WorkspaceAccessAuthenticationModeEnum；expires_at#4: Timestamp；application_credentials_available#5: optional bool

接收方写入：

完成证据：当前部署/访问策略和运行事实一致

失败/未知：按canonical应用登录，不新增SSO

### F09.3 bff → workspace / WorkspaceProductService.RevealWorkspaceApplicationCredentials

按当前入口/阶段与06/13状态机前置执行；不是无条件调用

请求 [RevealWorkspaceApplicationCredentialsRpcRequest](rpc-messages.md#revealworkspaceapplicationcredentialsrpcrequest)：context#1: CallContext；workspace_id#2: string

返回 [WorkspaceApplicationCredentials](rpc-messages.md#workspaceapplicationcredentials)：workspace_id#1: string；runtime_instance_id#2: string；username#3: string；password#4: string

接收方写入：

完成证据：所有者权限+当前声明workspace_admin_password+实际ready，只一次性用户名/密码

失败/未知：no-store不缓存；不返回GatewayKey或session_secret

### F09.4 workspace → runtime_control / RuntimeCoordination.ReloadModels

按当前入口/阶段与06/13状态机前置执行；不是无条件调用

请求 [RuntimeReloadCommand](rpc-messages.md#runtimereloadcommand)：context#1: CallContext；runtime_instance_id#2: string；expected_applied_version#3: int64；target_version#4: int64；selections#5: repeated ModelSelection

返回 [Operation](rpc-messages.md#operation)：operation_id#1: string；owner#2: OperationOwnerEnum；kind#3: OperationKindEnum；resource_id#4: string；status#5: OperationStatusEnum；stage#6: OperationStageEnum；observation_result#7: optional OperationObservationResultEnum；error_code#8: optional ErrorCodeEnum；request_id#9: string；created_at#10: Timestamp；updated_at#11: Timestamp；poll_after_seconds#12: optional int32

接收方写入：`runtime_control.runtime_actions`

完成证据：目标配置版本+selections实际应用

失败/未知：保存成功不等于reload成功

### F10.1 workspace → tenant / CloudIdentityAuthorization.AuthorizeAction

按当前入口/阶段与06/13状态机前置执行；不是无条件调用

请求 [AuthorizationRequest](rpc-messages.md#authorizationrequest)：scope#1: AuthorizationScope；actor_id#2: string；session_id#3: optional string；authorization_context_id#4: optional string；accepted_operation_grant_id#5: optional string；audience_owner#6: OwnerEnum；action#7: AuthorizationActionEnum；resource#8: AuthorizationResource；expected_permission_version#9: optional int64；request_id#10: string

返回 [AuthorizationDecision](rpc-messages.md#authorizationdecision)：result#1: AuthorizationResult；issuer#2: AuthorizationIssuer；authorization_context_id#3: optional string；scope#4: AuthorizationScope；actor_id#5: string；session_id#6: optional string；audience_owner#7: OwnerEnum；action#8: AuthorizationActionEnum；resource#9: AuthorizationResource；permission_version#10: int64；issued_at#11: Timestamp；expires_at#12: Timestamp；denial_code#13: optional ErrorCodeEnum；accepted_operation_grant_id#14: optional string

接收方写入：

完成证据：session/grant/action/resource/audience/权限版本确切一致

失败/未知：deny不改用管理员；unknown不发后续副作用

### F10.2 workspace → capability / CapabilityCoordination.ResolvePublisherContract

按当前入口/阶段与06/13状态机前置执行；不是无条件调用

请求 [ResolvePublisherContractRequest](rpc-messages.md#resolvepublishercontractrequest)：context#1: CallContext；reference#2: PublisherContractReference

返回 [ResolvedPublisherContract](rpc-messages.md#resolvedpublishercontract)：reference#1: PublisherContractReference；contract#2: PublisherContract；image#3: ArtifactReference；outcome#4: Observation

接收方写入：

完成证据：目标版本完整契约与数据兼容

失败/未知：不支持安全回滚的迁移拒绝，不猜semver

### F10.3 workspace → fabric / FabricRouteExecution.FenceRouteEpoch

按当前入口/阶段与06/13状态机前置执行；不是无条件调用

请求 [FenceRouteEpochCommand](rpc-messages.md#fencerouteepochcommand)：context#1: CallContext；workspace_id#2: string；operation_id#3: string；execution_epoch#4: int64；expected_route_generation#5: int64；provider_precondition#6: ProviderRevisionPrecondition

返回 [RouteReadback](rpc-messages.md#routereadback)：workspace_id#1: string；switch_id#2: string；observation#3: Observation；current_generation#4: int64；accepted_execution_epoch#5: int64；target_execution_resource_id#6: optional string；target_runtime_instance_id#7: optional string；target_deployment_id#8: optional string；provider_revision#9: string；provider_command_id#10: string；route_receipt_id#11: optional string；observed_at#12: Timestamp；error_code#13: optional ErrorCodeEnum

接收方写入：`fabric.route_switches`, `fabric.route_bindings`

完成证据：新epoch先在provider确认

失败/未知：旧未知切换不被强行覆盖

### F10.4 workspace → runtime_control / RuntimeCoordination.Deploy

按当前入口/阶段与06/13状态机前置执行；不是无条件调用

请求 [RuntimeDeployCommand](rpc-messages.md#runtimedeploycommand)：context#1: CallContext；workspace_id#2: string；deployment_id#3: string；capability_version_id#4: string；deployment_descriptor#5: DeploymentDescriptor；resource_set_id#6: string；data_attachment_id#7: string；secret_binding_id#8: string；model_configuration_version#9: int64；model_selections#10: repeated ModelSelection；data_compatibility#11: DataCompatibility；runtime_instance_id#12: string；deployment_descriptor_digest#13: string；execution_epoch#14: int64；deployment_descriptor_object_ref#15: string

返回 [RuntimeReadback](rpc-messages.md#runtimereadback)：runtime_instance_id#1: string；workspace_id#2: string；deployment_id#3: string；state#4: RuntimeInstanceState；process_ready#5: bool；application_available#6: bool；credential_injection_verified#7: bool；artifact#8: ArtifactReference；applied_model_configuration_version#9: int64；readiness_receipt_id#10: string；outcome#11: Observation；observed_at#12: Timestamp；deployment_descriptor_digest#13: string；execution_epoch#14: int64；deployment_descriptor_object_ref#15: string

接收方写入：`runtime_control.runtime_instances`, `runtime_control.runtime_actions`

完成证据：新实例实际验证且不违反可写卷并发限制

失败/未知：保留旧选中版本/原数据义务

### F11.1 workspace → tenant / CloudIdentityAuthorization.AuthorizeAction

按当前入口/阶段与06/13状态机前置执行；不是无条件调用

请求 [AuthorizationRequest](rpc-messages.md#authorizationrequest)：scope#1: AuthorizationScope；actor_id#2: string；session_id#3: optional string；authorization_context_id#4: optional string；accepted_operation_grant_id#5: optional string；audience_owner#6: OwnerEnum；action#7: AuthorizationActionEnum；resource#8: AuthorizationResource；expected_permission_version#9: optional int64；request_id#10: string

返回 [AuthorizationDecision](rpc-messages.md#authorizationdecision)：result#1: AuthorizationResult；issuer#2: AuthorizationIssuer；authorization_context_id#3: optional string；scope#4: AuthorizationScope；actor_id#5: string；session_id#6: optional string；audience_owner#7: OwnerEnum；action#8: AuthorizationActionEnum；resource#9: AuthorizationResource；permission_version#10: int64；issued_at#11: Timestamp；expires_at#12: Timestamp；denial_code#13: optional ErrorCodeEnum；accepted_operation_grant_id#14: optional string

接收方写入：

完成证据：session/grant/action/resource/audience/权限版本确切一致

失败/未知：deny不改用管理员；unknown不发后续副作用

### F11.2 resource_catalog → workspace / WorkspacePlanChangeReadback.ReadSubscriptionPlanState

按当前入口/阶段与06/13状态机前置执行；不是无条件调用

请求 [ReadSubscriptionPlanStateRequest](rpc-messages.md#readsubscriptionplanstaterequest)：context#1: CallContext；workspace_id#2: string

返回 [SubscriptionPlanState](rpc-messages.md#subscriptionplanstate)：subscription_id#1: string；workspace_id#2: string；subscription_version#3: int64；current_period_id#4: string；period_start#5: Timestamp；period_end#6: Timestamp；billing_anchor_day#7: int32；compute_plan_id#8: string；storage_plan_id#9: string；accepted_price_policy_version_id#10: optional string；accepted_monthly_usd_micros#11: optional int64；unfinished_plan_change_id#12: optional string；next_period_obligation_id#13: optional string；next_period_start#14: Timestamp；next_period_end#15: Timestamp；renewal_mode#16: SubscriptionRenewalModeEnum；renewal_consent_id#17: optional string；outcome#18: Observation；other_future_committed_obligation_id#19: optional string；runtime_readback_required#20: bool；current_application_deployment_id#21: optional string；period_start_milliseconds#22: int64；period_end_milliseconds#23: int64；source_financial_snapshot_bytes#24: bytes；source_financial_snapshot_digest#25: string；source_financial_snapshot#26: SourceFinancialSnapshot

接收方写入：

完成证据：原period/S/E/已接受当前月价/当前计划和资金义务版本固定

失败/未知：已锁定其它未来账单或财务基础变化拒绝，不退旧款改价

### F11.5 bff → workspace / WorkspaceProductService.ResizeWorkspace

按当前入口/阶段与06/13状态机前置执行；不是无条件调用

请求 [ResizeWorkspaceRpcRequest](rpc-messages.md#resizeworkspacerpcrequest)：context#1: CallContext；body#2: ApplyQuoteRequest；workspace_id#3: string

返回 [Operation](rpc-messages.md#operation)：operation_id#1: string；owner#2: OperationOwnerEnum；kind#3: OperationKindEnum；resource_id#4: string；status#5: OperationStatusEnum；stage#6: OperationStageEnum；observation_result#7: optional OperationObservationResultEnum；error_code#8: optional ErrorCodeEnum；request_id#9: string；created_at#10: Timestamp；updated_at#11: Timestamp；poll_after_seconds#12: optional int32

接收方写入：`workspace.plan_changes`, `workspace.operations`

完成证据：CAS写PlanChange与原基础；scheduled不是applied

失败/未知：同幂等键返回原计划，已有未完成计划不覆盖

### F11.6 workspace → resource_catalog / CatalogCoordination.AcceptQuote

按当前入口/阶段与06/13状态机前置执行；不是无条件调用

请求 [AcceptQuoteRequest](rpc-messages.md#acceptquoterequest)：context#1: CallContext；quote_id#2: string；workspace_id#3: string；obligation_id#4: string；admission_id#5: string；plan_change_id#6: optional string；source_subscription_version#7: optional int64；subscription_period_obligation_id#8: optional string

返回 [QuoteAcceptance](rpc-messages.md#quoteacceptance)：quote#1: Quote；obligation_id#2: string；acceptance_id#3: string；snapshot_digest#4: string；source_financial_snapshot_bytes#5: optional bytes；source_financial_snapshot_digest#6: optional string

接收方写入：`resource_catalog.quotes`

完成证据：exact quote绑定本PlanChange的初次Operation

失败/未知：不能把其它purpose/计划quote用作新购买

### F11.7 workspace → gateway / GatewayPlanChangeSettlement.DebitSupplement

kind=upgrade_immediate 且upgradeCharge>0且原单未确认；zero跳过

请求 [PlanChangeSupplementChargeCommand](rpc-messages.md#planchangesupplementchargecommand)：context#1: CallContext；workspace_id#2: string；plan_change_id#3: string；subscription_period_id#4: string；quote_acceptance_id#5: string；original_charge_identity#6: string；amount_usd_micros#7: int64；coverage_start#8: Timestamp；coverage_end#9: Timestamp；policy_version#10: string；source_subscription_version#11: int64；coverage_start_milliseconds#12: int64；coverage_end_milliseconds#13: int64；source_financial_snapshot_digest#14: string

返回 [WalletOperation](rpc-messages.md#walletoperation)：id#1: string；workspace_id#2: optional string；kind#3: WalletOperationKindEnum；amount_usd_micros#4: int64；status#5: WalletOperationStatusEnum；external_reference#6: optional string；receipt_id#7: optional string；error_code#8: optional ErrorCodeEnum；created_at#9: Timestamp；updated_at#10: Timestamp；purpose#11: optional WalletOperationPurposeEnum；plan_change_id#12: optional string；original_charge_operation_id#13: optional string；coverage_start#14: Timestamp；coverage_end#15: Timestamp；coverage_start_milliseconds#16: optional int64；coverage_end_milliseconds#17: optional int64

接收方写入：`gateway.wallet_operations`

完成证据：仅upgrade正补差、原code与固定quote金额一致

失败/未知：0金额不调用；unknown原单读回不新收费

### F11.8 gateway → workspace / WorkspacePlanChangeReadback.ReadNextPeriodObligation

downgrade_next_period的付款/执行分支，或其取消未付款义务

请求 [ReadNextPeriodObligationRequest](rpc-messages.md#readnextperiodobligationrequest)：context#1: CallContext；subscription_id#2: string；period_start#3: Timestamp；obligation_id#4: optional string

返回 [NextPeriodObligation](rpc-messages.md#nextperiodobligation)：id#1: string；workspace_id#2: string；subscription_id#3: string；period_start#4: Timestamp；period_end#5: Timestamp；plan_change_id#6: optional string；target_compute_plan_id#7: string；target_storage_plan_id#8: string；accepted_target_price_policy_version_id#9: string；amount_usd_micros#10: int64；accepted_quote_id#11: string；operation_id#12: string；status#13: PeriodObligationStatus；wallet_operation_id#14: optional string；confirmed_charge_receipt_id#15: optional string；zero_amount_receipt_id#16: optional string；source_subscription_version#17: int64；confirmed_subscription_version#18: optional int64；renewal_consent_id#19: optional string；outcome#20: Observation；version#21: int64

接收方写入：

完成证据：downgrade到期资金动作绑定唯一workspace/nextPeriod原义务与consent

失败/未知：没有有效付款授权则awaiting_payment，不偷开自动续费

### F11.9 workspace → gateway / GatewayPlanChangeSettlement.DebitScheduledPeriod

kind=downgrade_next_period且有效下期付款授权；不得提前降低资源

请求 [ScheduledPeriodChargeCommand](rpc-messages.md#scheduledperiodchargecommand)：context#1: CallContext；workspace_id#2: string；plan_change_id#3: string；subscription_id#4: string；period_obligation_id#5: string；period_start#6: Timestamp；period_end#7: Timestamp；accepted_target_quote_id#8: string；target_price_policy_version_id#9: string；amount_usd_micros#10: int64；renewal_consent_id#11: optional string

返回 [WalletOperation](rpc-messages.md#walletoperation)：id#1: string；workspace_id#2: optional string；kind#3: WalletOperationKindEnum；amount_usd_micros#4: int64；status#5: WalletOperationStatusEnum；external_reference#6: optional string；receipt_id#7: optional string；error_code#8: optional ErrorCodeEnum；created_at#9: Timestamp；updated_at#10: Timestamp；purpose#11: optional WalletOperationPurposeEnum；plan_change_id#12: optional string；original_charge_operation_id#13: optional string；coverage_start#14: Timestamp；coverage_end#15: Timestamp；coverage_start_milliseconds#16: optional int64；coverage_end_milliseconds#17: optional int64

接收方写入：`gateway.wallet_operations`

完成证据：仅目标下一期已接受价格，提前付款也不提前减资源

失败/未知：不得先旧价扣再补救；manual未授权不调用

### F11.10 workspace → fabric / FabricCoordination.ResizeResources

upgrade资金confirmed/zero；或downgrade已到E且目标期资金confirmed

请求 [ResizeResourcesCommand](rpc-messages.md#resizeresourcescommand)：context#1: CallContext；workspace_id#2: string；resource_set_id#3: string；expected_resource_version#4: string；target#5: ResourcePlanSnapshot；quote_acceptance_id#6: string；funding_evidence#7: PlanChangeFundingEvidence；instance_authorization_reference#8: string；plan_change_id#9: string；transition_id#10: string；execution_epoch#11: int64；execution_plan#12: ProviderPlanChangeExecutionPlanReference

返回 [Operation](rpc-messages.md#operation)：operation_id#1: string；owner#2: OperationOwnerEnum；kind#3: OperationKindEnum；resource_id#4: string；status#5: OperationStatusEnum；stage#6: OperationStageEnum；observation_result#7: optional OperationObservationResultEnum；error_code#8: optional ErrorCodeEnum；request_id#9: string；created_at#10: Timestamp；updated_at#11: Timestamp；poll_after_seconds#12: optional int32

接收方写入：`fabric.resource_actions`, `fabric.resources`

完成证据：资金/ZeroFundingEvidence和固定executionPlan，原epoch/目标/资源读回一致

失败/未知：unknown原请求读回，部分不可逆事实独立保留不假缩容

### F11.11 workspace → runtime_control / RuntimePlanChangeControl.RestoreAfterResourceChange

按当前入口/阶段与06/13状态机前置执行；不是无条件调用

请求 [RestorePlanChangeRuntimeCommand](rpc-messages.md#restoreplanchangeruntimecommand)：context#1: CallContext；workspace_id#2: string；plan_change_id#3: string；deployment_id#4: string；runtime_instance_id#5: string；confirmed_resource_action_id#6: string；target#7: ResourcePlanSnapshot；execution_epoch#8: int64；unchanged_application_descriptor_digest#9: string；current_workspace_version#10: int64

返回 [PlanChangeRuntimeReadback](rpc-messages.md#planchangeruntimereadback)：workspace_id#1: string；plan_change_id#2: string；runtime#3: RuntimeReadback；resources#4: ResourceReadback；target_limits_confirmed#5: bool；receipt_id#6: string；outcome#7: Observation

接收方写入：`runtime_control.runtime_actions`

完成证据：现有应用实际资源限制/挂载/健康确认；裸资源为owner证明的not_applicable

失败/未知：已有应用不可用不得applied，不让客户skipRuntime

### F11.12 workspace → ledger / LedgerPlanChangeEvidence.AppendPlanChangeReceipt

按当前入口/阶段与06/13状态机前置执行；不是无条件调用

请求 [AppendPlanChangeReceiptRequest](rpc-messages.md#appendplanchangereceiptrequest)：context#1: CallContext；kind#2: ReceiptKindEnum；evidence#3: PlanChangeEvidence；accepted_calculation#4: PlanChangeCalculation；owner_evidence_reference#5: string

返回 [Receipt](rpc-messages.md#receipt)：id#1: string；kind#2: ReceiptKindEnum；owner#3: OwnerEnum；source_sha#4: optional string；artifact_digest#5: optional string；operation_id#6: optional string；workflow_run_id#7: optional string；outcome#8: ReceiptOutcomeEnum；evidence_summary#9: string；created_at#10: Timestamp

接收方写入：`ledger.receipts`

完成证据：本域CAS appliedAt/active计划/原E不变或下期正确周期后精确receipt

失败/未知：已受理/预约成功不能当资源已生效

### F11.13 bff → workspace / WorkspaceProductService.CancelPlanChange

用户显式取消且无新期资金accepted/资源执行

请求 [CancelPlanChangeRpcRequest](rpc-messages.md#cancelplanchangerpcrequest)：context#1: CallContext；body#2: CancelPlanChangeRequest；workspace_id#3: string；plan_change_id#4: string

返回 [Operation](rpc-messages.md#operation)：operation_id#1: string；owner#2: OperationOwnerEnum；kind#3: OperationKindEnum；resource_id#4: string；status#5: OperationStatusEnum；stage#6: OperationStageEnum；observation_result#7: optional OperationObservationResultEnum；error_code#8: optional ErrorCodeEnum；request_id#9: string；created_at#10: Timestamp；updated_at#11: Timestamp；poll_after_seconds#12: optional int32

接收方写入：`workspace.plan_changes`, `workspace.operations`

完成证据：尚无新期资金accepted/执行时CAS取消；历史不改

失败/未知：付款或资源动作已开始拒绝，不能按低价付完再恢复高配

### F11.14 workspace → gateway / GatewayPlanChangeSettlement.RefundFailure

确定失败/fence/资源证据完备；非unknown；原单有未退额

请求 [PlanChangeFailureRefundCommand](rpc-messages.md#planchangefailurerefundcommand)：context#1: CallContext；evidence#2: SupplementalRefundEvidence；original_external_payment_reference#3: string；refund_identity#4: string

返回 [WalletOperation](rpc-messages.md#walletoperation)：id#1: string；workspace_id#2: optional string；kind#3: WalletOperationKindEnum；amount_usd_micros#4: int64；status#5: WalletOperationStatusEnum；external_reference#6: optional string；receipt_id#7: optional string；error_code#8: optional ErrorCodeEnum；created_at#9: Timestamp；updated_at#10: Timestamp；purpose#11: optional WalletOperationPurposeEnum；plan_change_id#12: optional string；original_charge_operation_id#13: optional string；coverage_start#14: Timestamp；coverage_end#15: Timestamp；coverage_start_milliseconds#16: optional int64；coverage_end_milliseconds#17: optional int64

接收方写入：`gateway.wallet_operations`

完成证据：确定交付失败+fence+实际资源证据，补偿原补差或原目标期款

失败/未知：unknown不退；部分不可逆成本归平台，不向客户擅收部分交付费

### F11.15 workspace → gateway / GatewayPlanChangeSettlement.RefundSupplementOnDeletion

原升级已applied，此后Workspace正常删除已确认

请求 [SupplementDeletionRefundCommand](rpc-messages.md#supplementdeletionrefundcommand)：context#1: CallContext；evidence#2: SupplementalRefundEvidence；original_external_payment_reference#3: string；refund_identity#4: string

返回 [WalletOperation](rpc-messages.md#walletoperation)：id#1: string；workspace_id#2: optional string；kind#3: WalletOperationKindEnum；amount_usd_micros#4: int64；status#5: WalletOperationStatusEnum；external_reference#6: optional string；receipt_id#7: optional string；error_code#8: optional ErrorCodeEnum；created_at#9: Timestamp；updated_at#10: Timestamp；purpose#11: optional WalletOperationPurposeEnum；plan_change_id#12: optional string；original_charge_operation_id#13: optional string；coverage_start#14: Timestamp；coverage_end#15: Timestamp；coverage_start_milliseconds#16: optional int64；coverage_end_milliseconds#17: optional int64

接收方写入：`gateway.wallet_operations`

完成证据：成功补差的T..E原覆盖区间、正常删除证据及原单未退余额

失败/未知：不用base720；每笔原单分别去重/读回

### F12.1 workspace → tenant / CloudIdentityAuthorization.AuthorizeAction

按当前入口/阶段与06/13状态机前置执行；不是无条件调用

请求 [AuthorizationRequest](rpc-messages.md#authorizationrequest)：scope#1: AuthorizationScope；actor_id#2: string；session_id#3: optional string；authorization_context_id#4: optional string；accepted_operation_grant_id#5: optional string；audience_owner#6: OwnerEnum；action#7: AuthorizationActionEnum；resource#8: AuthorizationResource；expected_permission_version#9: optional int64；request_id#10: string

返回 [AuthorizationDecision](rpc-messages.md#authorizationdecision)：result#1: AuthorizationResult；issuer#2: AuthorizationIssuer；authorization_context_id#3: optional string；scope#4: AuthorizationScope；actor_id#5: string；session_id#6: optional string；audience_owner#7: OwnerEnum；action#8: AuthorizationActionEnum；resource#9: AuthorizationResource；permission_version#10: int64；issued_at#11: Timestamp；expires_at#12: Timestamp；denial_code#13: optional ErrorCodeEnum；accepted_operation_grant_id#14: optional string

接收方写入：

完成证据：session/grant/action/resource/audience/权限版本确切一致

失败/未知：deny不改用管理员；unknown不发后续副作用

### F12.2 tenant → workspace / WorkspaceAuthorizationReadback.ReadRenewalConsent

按当前入口/阶段与06/13状态机前置执行；不是无条件调用

请求 [ReadRenewalConsentRequest](rpc-messages.md#readrenewalconsentrequest)：context#1: CallContext；subscription_id#2: string；consent_id#3: string；subscription_period_id#4: string

返回 [RenewalConsentReadback](rpc-messages.md#renewalconsentreadback)：subscription_id#1: string；consent_id#2: string；subscription_period_id#3: string；active#4: bool；settings_version#5: int64；accepted_policy_version_id#6: string；actor_id#7: string；accepted_at#8: Timestamp；outcome#9: Observation

接收方写入：

完成证据：本周期explicit consent/原grant允许

失败/未知：关闭只阻止新周期，不丢已接受义务

### F12.3 gateway → workspace / WorkspacePlanChangeReadback.ReadNextPeriodObligation

downgrade_next_period的付款/执行分支，或其取消未付款义务

请求 [ReadNextPeriodObligationRequest](rpc-messages.md#readnextperiodobligationrequest)：context#1: CallContext；subscription_id#2: string；period_start#3: Timestamp；obligation_id#4: optional string

返回 [NextPeriodObligation](rpc-messages.md#nextperiodobligation)：id#1: string；workspace_id#2: string；subscription_id#3: string；period_start#4: Timestamp；period_end#5: Timestamp；plan_change_id#6: optional string；target_compute_plan_id#7: string；target_storage_plan_id#8: string；accepted_target_price_policy_version_id#9: string；amount_usd_micros#10: int64；accepted_quote_id#11: string；operation_id#12: string；status#13: PeriodObligationStatus；wallet_operation_id#14: optional string；confirmed_charge_receipt_id#15: optional string；zero_amount_receipt_id#16: optional string；source_subscription_version#17: int64；confirmed_subscription_version#18: optional int64；renewal_consent_id#19: optional string；outcome#20: Observation；version#21: int64

接收方写入：

完成证据：确认该期是否唯一绑定scheduled计划及已接受目标价格

失败/未知：不得人工/自动各造一单或按旧价先扣

### F12.4 workspace → gateway / GatewayPlanChangeSettlement.DebitScheduledPeriod

kind=downgrade_next_period且有效下期付款授权；不得提前降低资源

请求 [ScheduledPeriodChargeCommand](rpc-messages.md#scheduledperiodchargecommand)：context#1: CallContext；workspace_id#2: string；plan_change_id#3: string；subscription_id#4: string；period_obligation_id#5: string；period_start#6: Timestamp；period_end#7: Timestamp；accepted_target_quote_id#8: string；target_price_policy_version_id#9: string；amount_usd_micros#10: int64；renewal_consent_id#11: optional string

返回 [WalletOperation](rpc-messages.md#walletoperation)：id#1: string；workspace_id#2: optional string；kind#3: WalletOperationKindEnum；amount_usd_micros#4: int64；status#5: WalletOperationStatusEnum；external_reference#6: optional string；receipt_id#7: optional string；error_code#8: optional ErrorCodeEnum；created_at#9: Timestamp；updated_at#10: Timestamp；purpose#11: optional WalletOperationPurposeEnum；plan_change_id#12: optional string；original_charge_operation_id#13: optional string；coverage_start#14: Timestamp；coverage_end#15: Timestamp；coverage_start_milliseconds#16: optional int64；coverage_end_milliseconds#17: optional int64

接收方写入：`gateway.wallet_operations`

完成证据：仅当nextPeriodObligation绑定降配时使用目标价一次扣款

失败/未知：无consent则awaiting_payment，不走普通旧价Debit

### F12.5 workspace → gateway / GatewayCoordination.Debit

该nextPeriod无scheduled PlanChange，仅常规续费分支

请求 [WalletDebitCommand](rpc-messages.md#walletdebitcommand)：context#1: CallContext；workspace_id#2: string；obligation_id#3: string；quote_acceptance_id#4: string；amount_usd_micros#5: int64；currency#6: string；subscription_period_id#7: string

返回 [WalletOperation](rpc-messages.md#walletoperation)：id#1: string；workspace_id#2: optional string；kind#3: WalletOperationKindEnum；amount_usd_micros#4: int64；status#5: WalletOperationStatusEnum；external_reference#6: optional string；receipt_id#7: optional string；error_code#8: optional ErrorCodeEnum；created_at#9: Timestamp；updated_at#10: Timestamp；purpose#11: optional WalletOperationPurposeEnum；plan_change_id#12: optional string；original_charge_operation_id#13: optional string；coverage_start#14: Timestamp；coverage_end#15: Timestamp；coverage_start_milliseconds#16: optional int64；coverage_end_milliseconds#17: optional int64

接收方写入：`gateway.wallet_operations`

完成证据：workspace+period业务键唯一原单确认

失败/未知：重复点击/worker命中同一义务

### F12.6 workspace → fabric / FabricCoordination.RenewResources

无计划常规续期；有计划必须遵照已批准目标executionPlan，不独立先续旧高配

请求 [RenewResourcesCommand](rpc-messages.md#renewresourcescommand)：context#1: CallContext；workspace_id#2: string；resource_set_id#3: string；subscription_period_id#4: string；prepaid_months#5: int32；confirmed_charge_receipt_id#6: string；instance_authorization_reference#7: string

返回 [Operation](rpc-messages.md#operation)：operation_id#1: string；owner#2: OperationOwnerEnum；kind#3: OperationKindEnum；resource_id#4: string；status#5: OperationStatusEnum；stage#6: OperationStageEnum；observation_result#7: optional OperationObservationResultEnum；error_code#8: optional ErrorCodeEnum；request_id#9: string；created_at#10: Timestamp；updated_at#11: Timestamp；poll_after_seconds#12: optional int32

接收方写入：`fabric.resource_actions`

完成证据：原资源续期读回与原paidThrough续期窗口一致

失败/未知：过期已回收不伪造恢复、不改now重新计期

### F12.7 workspace → ledger / LedgerCoordination.AppendReceipt

按当前入口/阶段与06/13状态机前置执行；不是无条件调用

请求 [AppendReceiptRequest](rpc-messages.md#appendreceiptrequest)：context#1: CallContext；receipt#2: Receipt；evidence_digest#3: string；owner_evidence_reference#4: string

返回 [Receipt](rpc-messages.md#receipt)：id#1: string；kind#2: ReceiptKindEnum；owner#3: OwnerEnum；source_sha#4: optional string；artifact_digest#5: optional string；operation_id#6: optional string；workflow_run_id#7: optional string；outcome#8: ReceiptOutcomeEnum；evidence_summary#9: string；created_at#10: Timestamp

接收方写入：`ledger.receipts`

完成证据：返回receipt身份，另ReadReceiptByReference核对原输入

失败/未知：同idempotency key读回，不填假Workspace或重写receipt

### F13.1 workspace → tenant / CloudIdentityAuthorization.AuthorizeAction

按当前入口/阶段与06/13状态机前置执行；不是无条件调用

请求 [AuthorizationRequest](rpc-messages.md#authorizationrequest)：scope#1: AuthorizationScope；actor_id#2: string；session_id#3: optional string；authorization_context_id#4: optional string；accepted_operation_grant_id#5: optional string；audience_owner#6: OwnerEnum；action#7: AuthorizationActionEnum；resource#8: AuthorizationResource；expected_permission_version#9: optional int64；request_id#10: string

返回 [AuthorizationDecision](rpc-messages.md#authorizationdecision)：result#1: AuthorizationResult；issuer#2: AuthorizationIssuer；authorization_context_id#3: optional string；scope#4: AuthorizationScope；actor_id#5: string；session_id#6: optional string；audience_owner#7: OwnerEnum；action#8: AuthorizationActionEnum；resource#9: AuthorizationResource；permission_version#10: int64；issued_at#11: Timestamp；expires_at#12: Timestamp；denial_code#13: optional ErrorCodeEnum；accepted_operation_grant_id#14: optional string

接收方写入：

完成证据：session/grant/action/resource/audience/权限版本确切一致

失败/未知：deny不改用管理员；unknown不发后续副作用

### F13.2 workspace → runtime_control / RuntimeCoordination.Retire

按当前入口/阶段与06/13状态机前置执行；不是无条件调用

请求 [RuntimeStopCommand](rpc-messages.md#runtimestopcommand)：context#1: CallContext；runtime_instance_id#2: string；deployment_id#3: string；retained_data_attachment_id#4: string

返回 [Operation](rpc-messages.md#operation)：operation_id#1: string；owner#2: OperationOwnerEnum；kind#3: OperationKindEnum；resource_id#4: string；status#5: OperationStatusEnum；stage#6: OperationStageEnum；observation_result#7: optional OperationObservationResultEnum；error_code#8: optional ErrorCodeEnum；request_id#9: string；created_at#10: Timestamp；updated_at#11: Timestamp；poll_after_seconds#12: optional int32

接收方写入：`runtime_control.runtime_actions`

完成证据：确切旧Runtime停止/不存在

失败/未知：unknown不继续声称全环境已删

### F13.3 workspace → fabric / FabricCoordination.DeleteResources

按当前入口/阶段与06/13状态机前置执行；不是无条件调用

请求 [MutateResourcesCommand](rpc-messages.md#mutateresourcescommand)：context#1: CallContext；workspace_id#2: string；resource_set_id#3: string；expected_resource_version#4: string；instance_authorization_reference#5: string

返回 [Operation](rpc-messages.md#operation)：operation_id#1: string；owner#2: OperationOwnerEnum；kind#3: OperationKindEnum；resource_id#4: string；status#5: OperationStatusEnum；stage#6: OperationStageEnum；observation_result#7: optional OperationObservationResultEnum；error_code#8: optional ErrorCodeEnum；request_id#9: string；created_at#10: Timestamp；updated_at#11: Timestamp；poll_after_seconds#12: optional int32

接收方写入：`fabric.resource_actions`, `fabric.resources`, `fabric.attachments`

完成证据：原资源/挂载/Secret及公开访问绑定确切absence

失败/未知：不依赖列表没看到推断不存在

### F13.4 workspace → ledger / LedgerCoordination.AppendReceipt

按当前入口/阶段与06/13状态机前置执行；不是无条件调用

请求 [AppendReceiptRequest](rpc-messages.md#appendreceiptrequest)：context#1: CallContext；receipt#2: Receipt；evidence_digest#3: string；owner_evidence_reference#4: string

返回 [Receipt](rpc-messages.md#receipt)：id#1: string；kind#2: ReceiptKindEnum；owner#3: OwnerEnum；source_sha#4: optional string；artifact_digest#5: optional string；operation_id#6: optional string；workflow_run_id#7: optional string；outcome#8: ReceiptOutcomeEnum；evidence_summary#9: string；created_at#10: Timestamp

接收方写入：`ledger.receipts`

完成证据：返回receipt身份，另ReadReceiptByReference核对原输入

失败/未知：同idempotency key读回，不填假Workspace或重写receipt

### F13.5 workspace → gateway / GatewayCoordination.Refund

原单可退且相应删除/失败证据完整，不是unknown

请求 [WalletRefundCommand](rpc-messages.md#walletrefundcommand)：context#1: CallContext；workspace_id#2: string；original_wallet_operation_id#3: string；original_external_payment_reference#4: string；amount_usd_micros#5: int64；refund_policy_version_id#6: string；confirmed_deletion_receipt_id#7: string；subscription_period_id#8: string

返回 [WalletOperation](rpc-messages.md#walletoperation)：id#1: string；workspace_id#2: optional string；kind#3: WalletOperationKindEnum；amount_usd_micros#4: int64；status#5: WalletOperationStatusEnum；external_reference#6: optional string；receipt_id#7: optional string；error_code#8: optional ErrorCodeEnum；created_at#9: Timestamp；updated_at#10: Timestamp；purpose#11: optional WalletOperationPurposeEnum；plan_change_id#12: optional string；original_charge_operation_id#13: optional string；coverage_start#14: Timestamp；coverage_end#15: Timestamp；coverage_start_milliseconds#16: optional int64；coverage_end_milliseconds#17: optional int64

接收方写入：`gateway.wallet_operations`

完成证据：原单剩余可退金额+删除receipt+原钱包目标

失败/未知：unknown原退款读回，删除与退款分别显示

### F13.6 workspace → gateway / GatewayPlanChangeSettlement.RefundSupplementOnDeletion

原升级已applied，此后Workspace正常删除已确认

请求 [SupplementDeletionRefundCommand](rpc-messages.md#supplementdeletionrefundcommand)：context#1: CallContext；evidence#2: SupplementalRefundEvidence；original_external_payment_reference#3: string；refund_identity#4: string

返回 [WalletOperation](rpc-messages.md#walletoperation)：id#1: string；workspace_id#2: optional string；kind#3: WalletOperationKindEnum；amount_usd_micros#4: int64；status#5: WalletOperationStatusEnum；external_reference#6: optional string；receipt_id#7: optional string；error_code#8: optional ErrorCodeEnum；created_at#9: Timestamp；updated_at#10: Timestamp；purpose#11: optional WalletOperationPurposeEnum；plan_change_id#12: optional string；original_charge_operation_id#13: optional string；coverage_start#14: Timestamp；coverage_end#15: Timestamp；coverage_start_milliseconds#16: optional int64；coverage_end_milliseconds#17: optional int64

接收方写入：`gateway.wallet_operations`

完成证据：本Owner枚举每个已applied补差，按各自T..E覆盖/原单剩余可退额分别结算

失败/未知：不把补差合并进base720；unknown保留该原单未决退款

### F15.2 tenant → workspace / TenantWorkspaceCoordination.SuspendTenantWorkspaces

suspendTenant入口

请求 [TenantWorkspaceLifecycleCommand](rpc-messages.md#tenantworkspacelifecyclecommand)：context#1: CallContext；target_tenant_id#2: string；tenant_operation_id#3: string；reason#4: string

返回 [TenantWorkspaceLifecycleReadback](rpc-messages.md#tenantworkspacelifecyclereadback)：target_tenant_id#1: string；workspace_actions#2: repeated TenantWorkspaceAction；outcome#3: Observation；skipped#4: repeated TenantWorkspaceSkip

接收方写入：`workspace.operations`

完成证据：每Workspace原Tenant暂停子操作可读回

失败/未知：Tenant权限撤销不冒充资源已暂停

### F15.3 tenant → workspace / TenantWorkspaceCoordination.ResumeTenantWorkspaces

reenableTenant入口，原停用/已付/原资源存在

请求 [ResumeTenantWorkspacesRequest](rpc-messages.md#resumetenantworkspacesrequest)：context#1: CallContext；target_tenant_id#2: string；reenable_operation_id#3: string；original_tenant_suspend_operation_id#4: string

返回 [TenantWorkspaceLifecycleReadback](rpc-messages.md#tenantworkspacelifecyclereadback)：target_tenant_id#1: string；workspace_actions#2: repeated TenantWorkspaceAction；outcome#3: Observation；skipped#4: repeated TenantWorkspaceSkip

接收方写入：`workspace.operations`

完成证据：只恢复原暂停、仍已付、原资源存在对象

失败/未知：过期/其他原因/删除对象skip，不续费重购

### F15.4 tenant → workspace / TenantWorkspaceCoordination.DeleteTenantWorkspaces

deleteTenant入口

请求 [TenantWorkspaceLifecycleCommand](rpc-messages.md#tenantworkspacelifecyclecommand)：context#1: CallContext；target_tenant_id#2: string；tenant_operation_id#3: string；reason#4: string

返回 [TenantWorkspaceLifecycleReadback](rpc-messages.md#tenantworkspacelifecyclereadback)：target_tenant_id#1: string；workspace_actions#2: repeated TenantWorkspaceAction；outcome#3: Observation；skipped#4: repeated TenantWorkspaceSkip

接收方写入：`workspace.operations`

完成证据：每子删除证据明确

失败/未知：未完成不把Tenant操作标全部完成

### F16.1 workspace → tenant / CloudIdentityAuthorization.AuthorizeAction

按当前入口/阶段与06/13状态机前置执行；不是无条件调用

请求 [AuthorizationRequest](rpc-messages.md#authorizationrequest)：scope#1: AuthorizationScope；actor_id#2: string；session_id#3: optional string；authorization_context_id#4: optional string；accepted_operation_grant_id#5: optional string；audience_owner#6: OwnerEnum；action#7: AuthorizationActionEnum；resource#8: AuthorizationResource；expected_permission_version#9: optional int64；request_id#10: string

返回 [AuthorizationDecision](rpc-messages.md#authorizationdecision)：result#1: AuthorizationResult；issuer#2: AuthorizationIssuer；authorization_context_id#3: optional string；scope#4: AuthorizationScope；actor_id#5: string；session_id#6: optional string；audience_owner#7: OwnerEnum；action#8: AuthorizationActionEnum；resource#9: AuthorizationResource；permission_version#10: int64；issued_at#11: Timestamp；expires_at#12: Timestamp；denial_code#13: optional ErrorCodeEnum；accepted_operation_grant_id#14: optional string

接收方写入：

完成证据：session/grant/action/resource/audience/权限版本确切一致

失败/未知：deny不改用管理员；unknown不发后续副作用

### F16.2 bff → workspace / WorkspaceProductService.AdoptWorkspace

按当前入口/阶段与06/13状态机前置执行；不是无条件调用

请求 [AdoptWorkspaceRpcRequest](rpc-messages.md#adoptworkspacerpcrequest)：context#1: CallContext；body#2: AdoptWorkspaceRequest；workspace_id#3: string

返回 [Operation](rpc-messages.md#operation)：operation_id#1: string；owner#2: OperationOwnerEnum；kind#3: OperationKindEnum；resource_id#4: string；status#5: OperationStatusEnum；stage#6: OperationStageEnum；observation_result#7: optional OperationObservationResultEnum；error_code#8: optional ErrorCodeEnum；request_id#9: string；created_at#10: Timestamp；updated_at#11: Timestamp；poll_after_seconds#12: optional int32

接收方写入：`workspace.workspaces`, `workspace.deployments`, `workspace.operations`

完成证据：原资源/订阅/ID沿用，新的Agent契约符合旧资源

失败/未知：不执行购买debit/Ensure新资源，不伪造Quote或Build

## 5. 事件：谁生产、谁消费、哪些字段

aggregate_type由事件精确版本的x-aggregate-identity.type派生；aggregateId须与其idPayloadField一致。revision由生产者聚合事务内分配；consumer_owner显式选择本域Inbox。字段与实现状态不得混同。


### wallet.operation_observed.v1

`gateway` → `workspace`, `ledger`

聚合类型：`wallet_operation`；ID来源：`payload.walletOperationId`（须等于aggregateId）

Envelope字段：`eventId`, `eventType`, `schemaVersion`, `owner`, `tenantId`, `aggregateId`, `aggregateVersion`, `occurredAt`, `requestId`, `payload`, `scope`

| payload字段 | 类型 | 必填 | 约束 |
| --- | --- | --- | --- |
| `walletOperationId` | `string` | 是 | minLength=1 |
| `workspaceId` | `string` | 是 | minLength=1 |
| `kind` | `string` | 是 | enum=["charge","refund"]; minLength=1 |
| `status` | `string` | 是 | enum=["requested","confirmed","rejected","unknown"]; minLength=1 |
| `amountUSDMicros` | `string` | 是 | pattern="^(?:0\|[1-9][0-9]{0,17}\|[1-8][0-9]{18}\|9[0-1][0-9]{17}\|92[0-1][0-9]{16}\|922[0-2][0-9]{15}\|9223[0-2][0-9]{14}\|92233[0-6][0-9]{13}\|922337[0-1][0-9]{12}\|92233720[0-2][0-9]{10}\|922337203[0-5][0-9]{9}\|9223372036[0-7][0-9]{8}\|92233720368[0-4][0-9]{7}\|922337203685[0-3][0-9]{6}\|9223372036854[0-6][0-9]{5}\|92233720368547[0-6][0-9]{4}\|922337203685477[0-4][0-9]{3}\|9223372036854775[0-7][0-9]{2}\|922337203685477580[0-6]\|9223372036854775807)$" |
| `externalReference` | `string` | 否 | minLength=1 |
| `receiptId` | `string` | 否 | minLength=1 |
| `purpose` | `string` | 否 | enum=["base_period","upgrade_supplement","base_period_delete","upgrade_failure_full","supplement_delete_unused","next_period_plan_failure_full","recharge"]; minLength=1 |
| `planChangeId` | `string` | 否 | minLength=1 |
| `originalChargeOperationId` | `string` | 否 | minLength=1 |
| `coverageStart` | `string/date-time` | 否 |  |
| `coverageEnd` | `string/date-time` | 否 |  |

原付款/退款观察结果，unknown不是失败


### fabric.resources_observed.v1

`fabric` → `workspace`, `runtime_control`, `ledger`

聚合类型：`resource_set`；ID来源：`payload.resourceSetId`（须等于aggregateId）

Envelope字段：`eventId`, `eventType`, `schemaVersion`, `owner`, `tenantId`, `aggregateId`, `aggregateVersion`, `occurredAt`, `requestId`, `payload`, `scope`

| payload字段 | 类型 | 必填 | 约束 |
| --- | --- | --- | --- |
| `resourceSetId` | `string` | 是 | minLength=1 |
| `workspaceId` | `string` | 是 | minLength=1 |
| `resourceActionId` | `string` | 是 | minLength=1 |
| `outcome` | `string` | 是 | enum=["confirmed","rejected","unknown"]; minLength=1 |
| `absenceConfirmed` | `boolean` | 是 |  |
| `receiptId` | `string` | 否 | minLength=1 |

实际资源或删除absence结果


### runtime.readiness_observed.v1

`runtime_control` → `workspace`, `ledger`

聚合类型：`runtime_instance`；ID来源：`payload.runtimeInstanceId`（须等于aggregateId）

Envelope字段：`eventId`, `eventType`, `schemaVersion`, `owner`, `tenantId`, `aggregateId`, `aggregateVersion`, `occurredAt`, `requestId`, `payload`, `scope`

| payload字段 | 类型 | 必填 | 约束 |
| --- | --- | --- | --- |
| `runtimeInstanceId` | `string` | 是 | minLength=1 |
| `workspaceId` | `string` | 是 | minLength=1 |
| `deploymentId` | `string` | 是 | minLength=1 |
| `outcome` | `string` | 是 | enum=["confirmed","rejected","unknown"]; minLength=1 |
| `applicationAvailable` | `boolean` | 是 |  |
| `credentialInjectionVerified` | `boolean` | 是 |  |
| `appliedModelConfigurationVersion` | `string` | 是 | pattern="^(?:0\|[1-9][0-9]{0,17}\|[1-8][0-9]{18}\|9[0-1][0-9]{17}\|92[0-1][0-9]{16}\|922[0-2][0-9]{15}\|9223[0-2][0-9]{14}\|92233[0-6][0-9]{13}\|922337[0-1][0-9]{12}\|92233720[0-2][0-9]{10}\|922337203[0-5][0-9]{9}\|9223372036[0-7][0-9]{8}\|92233720368[0-4][0-9]{7}\|922337203685[0-3][0-9]{6}\|9223372036854[0-6][0-9]{5}\|92233720368547[0-6][0-9]{4}\|922337203685477[0-4][0-9]{3}\|9223372036854775[0-7][0-9]{2}\|922337203685477580[0-6]\|9223372036854775807)$" |
| `receiptId` | `string` | 否 | minLength=1 |

Runtime应用及凭据注入真实读回


### workspace.state_changed.v1

`workspace` → `ledger`

聚合类型：`workspace`；ID来源：`payload.workspaceId`（须等于aggregateId）

Envelope字段：`eventId`, `eventType`, `schemaVersion`, `owner`, `tenantId`, `aggregateId`, `aggregateVersion`, `occurredAt`, `requestId`, `payload`, `scope`

| payload字段 | 类型 | 必填 | 约束 |
| --- | --- | --- | --- |
| `workspaceId` | `string` | 是 | minLength=1 |
| `operationId` | `string` | 是 | minLength=1 |
| `status` | `string` | 是 | enum=["provisioning","active","updating","suspended","deleting","deleted","failed","needs_attention"]; minLength=1 |
| `activeDeploymentId` | `string` | 否 | minLength=1 |

Workspace权威状态提交


### workspace.deletion_confirmed.v1

`workspace` → `tenant`, `ledger`

聚合类型：`workspace`；ID来源：`payload.workspaceId`（须等于aggregateId）

Envelope字段：`eventId`, `eventType`, `schemaVersion`, `owner`, `tenantId`, `aggregateId`, `aggregateVersion`, `occurredAt`, `requestId`, `payload`, `scope`

| payload字段 | 类型 | 必填 | 约束 |
| --- | --- | --- | --- |
| `workspaceId` | `string` | 是 | minLength=1 |
| `operationId` | `string` | 是 | minLength=1 |
| `resourceDeletionReceiptId` | `string` | 是 | minLength=1 |
| `dataDeletionReceiptId` | `string` | 是 | minLength=1 |
| `deletedAt` | `string/date-time` | 是 |  |

删除确认不代表退款成功


### tenant.access_revoked.v1

`tenant` → `capability`, `build`, `workspace`, `gateway`, `ledger`

聚合类型：`tenant`；ID来源：`payload.targetTenantId`（须等于aggregateId）

Envelope字段：`eventId`, `eventType`, `schemaVersion`, `owner`, `tenantId`, `aggregateId`, `aggregateVersion`, `occurredAt`, `requestId`, `payload`, `scope`

| payload字段 | 类型 | 必填 | 约束 |
| --- | --- | --- | --- |
| `targetTenantId` | `string` | 是 | minLength=1 |
| `tenantOperationId` | `string` | 是 | minLength=1 |
| `status` | `string` | 是 | enum=["suspended","deleting","deleted"]; minLength=1 |
| `restoreUntil` | `string/date-time` | 否 |  |

Tenant访问立即撤销，消费者清除访问准入


### ledger.receipt_recorded.v1

`ledger` → `workspace`

聚合类型：`receipt`；ID来源：`payload.receiptId`（须等于aggregateId）

Envelope字段：`eventId`, `eventType`, `schemaVersion`, `owner`, `tenantId`, `aggregateId`, `aggregateVersion`, `occurredAt`, `requestId`, `payload`, `scope`

| payload字段 | 类型 | 必填 | 约束 |
| --- | --- | --- | --- |
| `receiptId` | `string` | 是 | minLength=1 |
| `sourceOwner` | `string` | 是 | minLength=1 |
| `ownerEvidenceReference` | `string` | 是 | minLength=1 |
| `evidenceDigest` | `string` | 是 | pattern="^sha256:[0-9a-f]{64}$" |

不可变证据已落盘


### catalog.policy_changed.v1

`resource_catalog` → `workspace`, `ledger`

聚合类型：`catalog_policy_version`；ID来源：`payload.policyVersionId`（须等于aggregateId）

Envelope字段：`eventId`, `eventType`, `schemaVersion`, `owner`, `tenantId`, `aggregateId`, `aggregateVersion`, `occurredAt`, `requestId`, `payload`, `scope`

| payload字段 | 类型 | 必填 | 约束 |
| --- | --- | --- | --- |
| `policyVersionId` | `string` | 是 | minLength=1 |
| `policyKind` | `string` | 是 | minLength=1 |
| `validFrom` | `string/date-time` | 是 |  |

新准入采用新policy，不覆盖既有Quote


### tenant.reenabled.v1

`tenant` → `workspace`, `gateway`, `ledger`

聚合类型：`tenant`；ID来源：`payload.targetTenantId`（须等于aggregateId）

Envelope字段：`eventId`, `eventType`, `schemaVersion`, `owner`, `tenantId`, `aggregateId`, `aggregateVersion`, `occurredAt`, `requestId`, `payload`, `scope`

| payload字段 | 类型 | 必填 | 约束 |
| --- | --- | --- | --- |
| `targetTenantId` | `string` | 是 | minLength=1 |
| `tenantOperationId` | `string` | 是 | minLength=1 |
| `originalTenantSuspendOperationId` | `string` | 是 | minLength=1 |
| `enabledAt` | `string/date-time` | 是 |  |

访问恢复与受限Workspace复机分开，不进入删除restore窗口


### subscription.renewal_settings_changed.v1

`workspace` → `tenant`, `gateway`, `ledger`

聚合类型：`subscription`；ID来源：`payload.subscriptionId`（须等于aggregateId）

Envelope字段：`eventId`, `eventType`, `schemaVersion`, `owner`, `tenantId`, `aggregateId`, `aggregateVersion`, `occurredAt`, `requestId`, `payload`, `scope`

| payload字段 | 类型 | 必填 | 约束 |
| --- | --- | --- | --- |
| `workspaceId` | `string` | 是 | minLength=1 |
| `subscriptionId` | `string` | 是 | minLength=1 |
| `consentId` | `string` | 否 | minLength=1 |
| `renewalMode` | `string` | 是 | enum=["manual","automatic"]; minLength=1 |
| `settingsVersion` | `string` | 是 | pattern="^(?:0\|[1-9][0-9]{0,17}\|[1-8][0-9]{18}\|9[0-1][0-9]{17}\|92[0-1][0-9]{16}\|922[0-2][0-9]{15}\|9223[0-2][0-9]{14}\|92233[0-6][0-9]{13}\|922337[0-1][0-9]{12}\|92233720[0-2][0-9]{10}\|922337203[0-5][0-9]{9}\|9223372036[0-7][0-9]{8}\|92233720368[0-4][0-9]{7}\|922337203685[0-3][0-9]{6}\|9223372036854[0-6][0-9]{5}\|92233720368547[0-6][0-9]{4}\|922337203685477[0-4][0-9]{3}\|9223372036854775[0-7][0-9]{2}\|922337203685477580[0-6]\|9223372036854775807)$" |

仅原明确consent及设置版本；新的扣费还需同步读取当前consent，不以事件延迟当仍授权


### fabric.route_observed.v1

`fabric` → `workspace`, `ledger`

聚合类型：`route_binding`；ID来源：`payload.routeBindingId`（须等于aggregateId）

Envelope字段：`eventId`, `eventType`, `schemaVersion`, `owner`, `tenantId`, `aggregateId`, `aggregateVersion`, `occurredAt`, `requestId`, `payload`, `scope`

| payload字段 | 类型 | 必填 | 约束 |
| --- | --- | --- | --- |
| `routeBindingId` | `string` | 是 | minLength=1 |
| `workspaceId` | `string` | 是 | minLength=1 |
| `switchId` | `string` | 是 | minLength=1 |
| `actionKind` | `string` | 是 | enum=["fence","activate","rollback"]; minLength=1 |
| `routeGeneration` | `string` | 是 | pattern="^(?:0\|[1-9][0-9]{0,17}\|[1-8][0-9]{18}\|9[0-1][0-9]{17}\|92[0-1][0-9]{16}\|922[0-2][0-9]{15}\|9223[0-2][0-9]{14}\|92233[0-6][0-9]{13}\|922337[0-1][0-9]{12}\|92233720[0-2][0-9]{10}\|922337203[0-5][0-9]{9}\|9223372036[0-7][0-9]{8}\|92233720368[0-4][0-9]{7}\|922337203685[0-3][0-9]{6}\|9223372036854[0-6][0-9]{5}\|92233720368547[0-6][0-9]{4}\|922337203685477[0-4][0-9]{3}\|9223372036854775[0-7][0-9]{2}\|922337203685477580[0-6]\|9223372036854775807)$" |
| `executionEpoch` | `string` | 是 | pattern="^(?:0\|[1-9][0-9]{0,17}\|[1-8][0-9]{18}\|9[0-1][0-9]{17}\|92[0-1][0-9]{16}\|922[0-2][0-9]{15}\|9223[0-2][0-9]{14}\|92233[0-6][0-9]{13}\|922337[0-1][0-9]{12}\|92233720[0-2][0-9]{10}\|922337203[0-5][0-9]{9}\|9223372036[0-7][0-9]{8}\|92233720368[0-4][0-9]{7}\|922337203685[0-3][0-9]{6}\|9223372036854[0-6][0-9]{5}\|92233720368547[0-6][0-9]{4}\|922337203685477[0-4][0-9]{3}\|9223372036854775[0-7][0-9]{2}\|922337203685477580[0-6]\|9223372036854775807)$" |
| `providerRevision` | `string` | 是 | minLength=1 |
| `targetExecutionResourceId` | `string` | 否 | minLength=1 |
| `outcome` | `string` | 是 | enum=["confirmed","rejected","unknown"]; minLength=1 |
| `routeReceiptId` | `string` | 否 | minLength=1 |

路由fence/CAS实际读回，Workspace选中提交仍由Workspace owner负责


### workspace.plan_change_state_changed.v1

`workspace` → `ledger`

聚合类型：`plan_change`；ID来源：`payload.planChangeId`（须等于aggregateId）

Envelope字段：`eventId`, `eventType`, `schemaVersion`, `owner`, `tenantId`, `aggregateId`, `aggregateVersion`, `occurredAt`, `requestId`, `payload`, `scope`

| payload字段 | 类型 | 必填 | 约束 |
| --- | --- | --- | --- |
| `planChangeId` | `string` | 是 | minLength=1 |
| `workspaceId` | `string` | 是 | minLength=1 |
| `kind` | `string` | 是 | enum=["upgrade_immediate","downgrade_next_period"]; minLength=1 |
| `status` | `string` | 是 | enum=["requested","scheduled","awaiting_payment","applying","applied","failed","needs_attention","cancelled"]; minLength=1 |
| `scheduleVersion` | `string` | 是 | pattern="^(?:0\|[1-9][0-9]{0,17}\|[1-8][0-9]{18}\|9[0-1][0-9]{17}\|92[0-1][0-9]{16}\|922[0-2][0-9]{15}\|9223[0-2][0-9]{14}\|92233[0-6][0-9]{13}\|922337[0-1][0-9]{12}\|92233720[0-2][0-9]{10}\|922337203[0-5][0-9]{9}\|9223372036[0-7][0-9]{8}\|92233720368[0-4][0-9]{7}\|922337203685[0-3][0-9]{6}\|9223372036854[0-6][0-9]{5}\|92233720368547[0-6][0-9]{4}\|922337203685477[0-4][0-9]{3}\|9223372036854775[0-7][0-9]{2}\|922337203685477580[0-6]\|9223372036854775807)$" |
| `deliveryOutcome` | `string` | 是 | enum=["pending","applied","failed","unknown"]; minLength=1 |
| `resourceOutcome` | `string` | 是 | enum=["unchanged","target_confirmed","restored","irreversible_residual","unknown"]; minLength=1 |
| `operationId` | `string` | 是 | minLength=1 |
| `executionOperationId` | `string` | 否 | minLength=1 |
| `appliedAt` | `string/date-time` | 否 |  |
| `policyVersion` | `string` | 是 | enum=["workspace-plan-change-v1"]; minLength=1 |

PlanChange业务状态与初次/执行Operation分别记录；只有真实目标读回才applied


### workspace.period_obligation_changed.v1

`workspace` → `gateway`, `ledger`

聚合类型：`period_obligation`；ID来源：`payload.obligationId`（须等于aggregateId）

Envelope字段：`eventId`, `eventType`, `schemaVersion`, `owner`, `tenantId`, `aggregateId`, `aggregateVersion`, `occurredAt`, `requestId`, `payload`, `scope`

| payload字段 | 类型 | 必填 | 约束 |
| --- | --- | --- | --- |
| `obligationId` | `string` | 是 | minLength=1 |
| `workspaceId` | `string` | 是 | minLength=1 |
| `subscriptionId` | `string` | 是 | minLength=1 |
| `periodStart` | `string/date-time` | 是 |  |
| `periodEnd` | `string/date-time` | 是 |  |
| `planChangeId` | `string` | 否 | minLength=1 |
| `status` | `string` | 是 | enum=["awaiting_payment","accepted","confirmed","failed","needs_attention"]; minLength=1 |
| `targetPricePolicyVersionId` | `string` | 是 | minLength=1 |
| `amountUSDMicros` | `string` | 是 | pattern="^(?:0\|[1-9][0-9]{0,17}\|[1-8][0-9]{18}\|9[0-1][0-9]{17}\|92[0-1][0-9]{16}\|922[0-2][0-9]{15}\|9223[0-2][0-9]{14}\|92233[0-6][0-9]{13}\|922337[0-1][0-9]{12}\|92233720[0-2][0-9]{10}\|922337203[0-5][0-9]{9}\|9223372036[0-7][0-9]{8}\|92233720368[0-4][0-9]{7}\|922337203685[0-3][0-9]{6}\|9223372036854[0-6][0-9]{5}\|92233720368547[0-6][0-9]{4}\|922337203685477[0-4][0-9]{3}\|9223372036854775[0-7][0-9]{2}\|922337203685477580[0-6]\|9223372036854775807)$" |
| `walletOperationId` | `string` | 否 | minLength=1 |
| `receiptId` | `string` | 否 | minLength=1 |

manual/automatic/boundary共用原周期义务，目标价一次性付款；零金额无Gateway动作

