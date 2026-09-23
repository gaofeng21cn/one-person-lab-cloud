# CloudIdentity / Tenant：API、字段与交互

> 源码快照 `7f5d05fe9b855d8af216caad714cf7fc85014d3c`；这是已定义目标的派生索引，不是业务实现完成声明。回到[总览](../15_domain_alignment.md)。

## 1. 边界与继承

模块：`services/gateway-integration/internal/tenant`。当前：**新持久化骨架；身份 seam 返回未实现；旧登录仍在 Control Plane**。

Cloud会话授权、Tenant/成员/角色、权限版本、授权上下文和已接受义务grant；不保存密码、不拥有可消费钱包。

**继承 / 提取 / 新增：** 从旧 accounts/users/sessions 与准入政策提取；Sub2API认证适配留给gateway，不把旧account字段无条件升级成Tenant权限。

**旧事实：** control_plane_accounts；control_plane_users；会话重新登录，不复制委托凭据。

**事务边界：** 成员/租户权限变化与permission_version、审计及Outbox同本库事务；成员不是独立认证系统。

**业务顺序：** BFF登录→Gateway核验个人身份→Tenant确认成员和权限；每次特权调用验证audience/action/resource；停用撤销新操作，已有义务只允许受限收尾。

**失败 / unknown：** 授权不可用必须拒绝，不用缓存角色或service token顶替；不删除已经接受的债务。

## 2. 客户 REST 与后端 Owner

23 个规格REST操作；浏览器仅经BFF。表中的请求/响应为目标契约，不代表该RPC已挂载。字段展开见DTO目录。


### getLoginContext

`GET /api/v2/auth/context`

权限：`anonymous`；F：`F01`；主要成功状态：`200`。

Body：无独立命名body，见该操作schema；Response：[LoginContext](rest-schemas.md#logincontext)

响应顶层字段：`csrfToken`: string（必填）；`expiresAt`: string/date-time（必填）

涉及表（规格声明，不自动等于每次都写）：
错误响应：`400`, `401`, `403`, `404`, `409`, `422`, `429`, `500`, `503`

### login

`POST /api/v2/auth/login`

权限：`anonymous`；F：`F01`；主要成功状态：`200`。
| 入参位置 | 字段 | 类型 | 必填 |
| --- | --- | --- | --- |
| header | `Idempotency-Key` | `string` | 是 |
| header | `X-CSRF-Token` | `string` | 是 |

Body：[LoginRequest](rest-schemas.md#loginrequest)；Response：[Session](rest-schemas.md#session)

请求顶层字段：`username`: string（必填）；`password`: string/password（必填）

响应顶层字段：`actorId`: OpaqueId（必填）；`displayName`: string（必填）；`tenantId`: OpaqueId（可选）；`tenantName`: string（可选）；`role`: TenantRole（可选）；`permissions`: array<AuthorizationAction>（必填）；`csrfToken`: string（必填）；`expiresAt`: string/date-time（必填）

涉及表（规格声明，不自动等于每次都写）：
错误响应：`400`, `401`, `403`, `404`, `409`, `422`, `429`, `500`, `503`

### getSession

`GET /api/v2/auth/session`

权限：`authenticated`；F：`F01`；主要成功状态：`200`。

Body：无独立命名body，见该操作schema；Response：[Session](rest-schemas.md#session)

响应顶层字段：`actorId`: OpaqueId（必填）；`displayName`: string（必填）；`tenantId`: OpaqueId（可选）；`tenantName`: string（可选）；`role`: TenantRole（可选）；`permissions`: array<AuthorizationAction>（必填）；`csrfToken`: string（必填）；`expiresAt`: string/date-time（必填）

涉及表（规格声明，不自动等于每次都写）：`tenant.tenant_members`
错误响应：`400`, `401`, `403`, `404`, `409`, `422`, `429`, `500`, `503`

### logout

`POST /api/v2/auth/logout`

权限：`authenticated`；F：`F01`；主要成功状态：`204`。
| 入参位置 | 字段 | 类型 | 必填 |
| --- | --- | --- | --- |
| header | `Idempotency-Key` | `string` | 是 |
| header | `X-CSRF-Token` | `string` | 是 |

Body：无独立命名body，见该操作schema；Response：无独立命名response，见该操作schema

涉及表（规格声明，不自动等于每次都写）：
错误响应：`400`, `401`, `403`, `404`, `409`, `422`, `429`, `500`, `503`

### getTenant

`GET /api/v2/tenant`

权限：`member`；F：`F01, F15`；主要成功状态：`200`。

Body：无独立命名body，见该操作schema；Response：[Tenant](rest-schemas.md#tenant)

响应顶层字段：`id`: OpaqueId（必填）；`name`: string（必填）；`status`: string（必填）；`billingSub2apiUserId`: OpaqueId（可选）；`restoreUntil`: string/date-time（可选）；`assetCustodyStatus`: string（必填）；`createdAt`: string/date-time（必填）；`updatedAt`: string/date-time（必填）

涉及表（规格声明，不自动等于每次都写）：`tenant.tenants`
错误响应：`400`, `401`, `403`, `404`, `409`, `422`, `429`, `500`, `503`

### listMembers

`GET /api/v2/tenant/members`

权限：`admin, owner`；F：`F01`；主要成功状态：`200`。
| 入参位置 | 字段 | 类型 | 必填 |
| --- | --- | --- | --- |
| query | `cursor` | `OpaqueId` | 否 |
| query | `limit` | `integer` | 否 |

Body：无独立命名body，见该操作schema；Response：[MemberPage](rest-schemas.md#memberpage)

响应顶层字段：`items`: array<Member>（必填）；`nextCursor`: OpaqueId（可选）

涉及表（规格声明，不自动等于每次都写）：`tenant.tenant_members`
错误响应：`400`, `401`, `403`, `404`, `409`, `422`, `429`, `500`, `503`

### listInvitations

`GET /api/v2/tenant/invitations`

权限：`admin, owner`；F：`F01`；主要成功状态：`200`。
| 入参位置 | 字段 | 类型 | 必填 |
| --- | --- | --- | --- |
| query | `cursor` | `OpaqueId` | 否 |
| query | `limit` | `integer` | 否 |

Body：无独立命名body，见该操作schema；Response：[InvitationPage](rest-schemas.md#invitationpage)

响应顶层字段：`items`: array<Invitation>（必填）；`nextCursor`: OpaqueId（可选）

涉及表（规格声明，不自动等于每次都写）：`tenant.invitations`
错误响应：`400`, `401`, `403`, `404`, `409`, `422`, `429`, `500`, `503`

### inviteMember

`POST /api/v2/tenant/invitations`

权限：`admin, owner`；F：`F01`；主要成功状态：`201`。
| 入参位置 | 字段 | 类型 | 必填 |
| --- | --- | --- | --- |
| header | `Idempotency-Key` | `string` | 是 |
| header | `X-CSRF-Token` | `string` | 是 |

Body：[InviteMemberRequest](rest-schemas.md#invitememberrequest)；Response：[Invitation](rest-schemas.md#invitation)

请求顶层字段：`inviteeGatewaySubjectId`: OpaqueId（必填）；`role`: string（必填）

响应顶层字段：`id`: OpaqueId（必填）；`inviteeGatewaySubjectId`: OpaqueId（必填）；`role`: string（必填）；`status`: string（必填）；`expiresAt`: string/date-time（必填）；`createdAt`: string/date-time（必填）

涉及表（规格声明，不自动等于每次都写）：`tenant.invitations`
错误响应：`400`, `401`, `403`, `404`, `409`, `422`, `429`, `500`, `503`

### acceptInvitation

`POST /api/v2/invitations/{invitationId}/accept`

权限：`invitee`；F：`F01`；主要成功状态：`200`。
| 入参位置 | 字段 | 类型 | 必填 |
| --- | --- | --- | --- |
| path | `invitationId` | `OpaqueId` | 是 |
| header | `Idempotency-Key` | `string` | 是 |
| header | `X-CSRF-Token` | `string` | 是 |

Body：无独立命名body，见该操作schema；Response：[Member](rest-schemas.md#member)

响应顶层字段：`id`: OpaqueId（必填）；`actorId`: OpaqueId（必填）；`displayName`: string（必填）；`role`: TenantRole（必填）；`status`: string（必填）；`createdAt`: string/date-time（必填）

涉及表（规格声明，不自动等于每次都写）：`tenant.invitations`, `tenant.tenant_members`
错误响应：`400`, `401`, `403`, `404`, `409`, `422`, `429`, `500`, `503`

### revokeInvitation

`POST /api/v2/tenant/invitations/{invitationId}/revoke`

权限：`admin, owner`；F：`F01`；主要成功状态：`200`。
| 入参位置 | 字段 | 类型 | 必填 |
| --- | --- | --- | --- |
| path | `invitationId` | `OpaqueId` | 是 |
| header | `Idempotency-Key` | `string` | 是 |
| header | `X-CSRF-Token` | `string` | 是 |

Body：无独立命名body，见该操作schema；Response：[Invitation](rest-schemas.md#invitation)

响应顶层字段：`id`: OpaqueId（必填）；`inviteeGatewaySubjectId`: OpaqueId（必填）；`role`: string（必填）；`status`: string（必填）；`expiresAt`: string/date-time（必填）；`createdAt`: string/date-time（必填）

涉及表（规格声明，不自动等于每次都写）：`tenant.invitations`
错误响应：`400`, `401`, `403`, `404`, `409`, `422`, `429`, `500`, `503`

### updateMemberRole

`PUT /api/v2/tenant/members/{memberId}`

权限：`owner`；F：`F01`；主要成功状态：`200`。
| 入参位置 | 字段 | 类型 | 必填 |
| --- | --- | --- | --- |
| path | `memberId` | `OpaqueId` | 是 |
| header | `Idempotency-Key` | `string` | 是 |
| header | `X-CSRF-Token` | `string` | 是 |

Body：[UpdateMemberRoleRequest](rest-schemas.md#updatememberrolerequest)；Response：[Member](rest-schemas.md#member)

请求顶层字段：`role`: TenantRole（必填）

响应顶层字段：`id`: OpaqueId（必填）；`actorId`: OpaqueId（必填）；`displayName`: string（必填）；`role`: TenantRole（必填）；`status`: string（必填）；`createdAt`: string/date-time（必填）

涉及表（规格声明，不自动等于每次都写）：`tenant.tenant_members`
错误响应：`400`, `401`, `403`, `404`, `409`, `422`, `429`, `500`, `503`

### removeMember

`DELETE /api/v2/tenant/members/{memberId}`

权限：`owner`；F：`F01`；主要成功状态：`204`。
| 入参位置 | 字段 | 类型 | 必填 |
| --- | --- | --- | --- |
| path | `memberId` | `OpaqueId` | 是 |
| header | `Idempotency-Key` | `string` | 是 |
| header | `X-CSRF-Token` | `string` | 是 |

Body：无独立命名body，见该操作schema；Response：无独立命名response，见该操作schema

涉及表（规格声明，不自动等于每次都写）：`tenant.tenant_members`
错误响应：`400`, `401`, `403`, `404`, `409`, `422`, `429`, `500`, `503`

### listTenants

`GET /api/v2/admin/tenants`

权限：`platform_admin`；F：`F15`；主要成功状态：`200`。
| 入参位置 | 字段 | 类型 | 必填 |
| --- | --- | --- | --- |
| query | `cursor` | `OpaqueId` | 否 |
| query | `limit` | `integer` | 否 |

Body：无独立命名body，见该操作schema；Response：[TenantPage](rest-schemas.md#tenantpage)

响应顶层字段：`items`: array<Tenant>（必填）；`nextCursor`: OpaqueId（可选）

涉及表（规格声明，不自动等于每次都写）：`tenant.tenants`
错误响应：`400`, `401`, `403`, `404`, `409`, `422`, `429`, `500`, `503`

### createTenant

`POST /api/v2/admin/tenants`

权限：`platform_admin`；F：`F15`；主要成功状态：`202`。
| 入参位置 | 字段 | 类型 | 必填 |
| --- | --- | --- | --- |
| header | `Idempotency-Key` | `string` | 是 |
| header | `X-CSRF-Token` | `string` | 是 |

Body：[CreateTenantRequest](rest-schemas.md#createtenantrequest)；Response：[Operation](rest-schemas.md#operation)

请求顶层字段：`name`: string（必填）；`billingSub2apiUserId`: OpaqueId（必填）；`ownerGatewaySubjectId`: OpaqueId（必填）

响应顶层字段：`operationId`: OpaqueId（必填）；`owner`: OperationOwner（必填）；`kind`: OperationKind（必填）；`resourceId`: OpaqueId（必填）；`status`: string（必填）；`stage`: OperationStage（必填）；`observationResult`: string（可选）；`errorCode`: ErrorCode（可选）；`requestId`: OpaqueId（必填）；`createdAt`: string/date-time（必填）；`updatedAt`: string/date-time（必填）；`pollAfterSeconds`: integer（可选）

涉及表（规格声明，不自动等于每次都写）：`tenant.tenants`, `tenant.tenant_members`, `tenant.operations`
错误响应：`400`, `401`, `403`, `404`, `409`, `422`, `429`, `500`, `503`

### getAdminTenant

`GET /api/v2/admin/tenants/{tenantId}`

权限：`platform_admin`；F：`F15`；主要成功状态：`200`。
| 入参位置 | 字段 | 类型 | 必填 |
| --- | --- | --- | --- |
| path | `tenantId` | `OpaqueId` | 是 |

Body：无独立命名body，见该操作schema；Response：[Tenant](rest-schemas.md#tenant)

响应顶层字段：`id`: OpaqueId（必填）；`name`: string（必填）；`status`: string（必填）；`billingSub2apiUserId`: OpaqueId（可选）；`restoreUntil`: string/date-time（可选）；`assetCustodyStatus`: string（必填）；`createdAt`: string/date-time（必填）；`updatedAt`: string/date-time（必填）

涉及表（规格声明，不自动等于每次都写）：`tenant.tenants`
错误响应：`400`, `401`, `403`, `404`, `409`, `422`, `429`, `500`, `503`

### deleteTenant

`DELETE /api/v2/admin/tenants/{tenantId}`

权限：`platform_admin`；F：`F15`；主要成功状态：`202`。
| 入参位置 | 字段 | 类型 | 必填 |
| --- | --- | --- | --- |
| path | `tenantId` | `OpaqueId` | 是 |
| header | `Idempotency-Key` | `string` | 是 |
| header | `X-CSRF-Token` | `string` | 是 |

Body：[DeleteTenantRequest](rest-schemas.md#deletetenantrequest)；Response：[Operation](rest-schemas.md#operation)

请求顶层字段：`confirmationName`: string（必填）；`reason`: string（必填）

响应顶层字段：`operationId`: OpaqueId（必填）；`owner`: OperationOwner（必填）；`kind`: OperationKind（必填）；`resourceId`: OpaqueId（必填）；`status`: string（必填）；`stage`: OperationStage（必填）；`observationResult`: string（可选）；`errorCode`: ErrorCode（可选）；`requestId`: OpaqueId（必填）；`createdAt`: string/date-time（必填）；`updatedAt`: string/date-time（必填）；`pollAfterSeconds`: integer（可选）

涉及表（规格声明，不自动等于每次都写）：`tenant.tenants`, `tenant.operations`, `capability.packages`, `build.build_jobs`
错误响应：`400`, `401`, `403`, `404`, `409`, `422`, `429`, `500`, `503`

### bindTenantWallet

`PUT /api/v2/admin/tenants/{tenantId}/wallet-binding`

权限：`platform_admin`；F：`F01, F15`；主要成功状态：`202`。
| 入参位置 | 字段 | 类型 | 必填 |
| --- | --- | --- | --- |
| path | `tenantId` | `OpaqueId` | 是 |
| header | `Idempotency-Key` | `string` | 是 |
| header | `X-CSRF-Token` | `string` | 是 |

Body：[BindTenantWalletRequest](rest-schemas.md#bindtenantwalletrequest)；Response：[Operation](rest-schemas.md#operation)

请求顶层字段：`billingSub2apiUserId`: OpaqueId（必填）；`expectedBindingVersion`: NonnegativeInt64（必填）

响应顶层字段：`operationId`: OpaqueId（必填）；`owner`: OperationOwner（必填）；`kind`: OperationKind（必填）；`resourceId`: OpaqueId（必填）；`status`: string（必填）；`stage`: OperationStage（必填）；`observationResult`: string（可选）；`errorCode`: ErrorCode（可选）；`requestId`: OpaqueId（必填）；`createdAt`: string/date-time（必填）；`updatedAt`: string/date-time（必填）；`pollAfterSeconds`: integer（可选）

涉及表（规格声明，不自动等于每次都写）：`tenant.operations`, `gateway.tenant_wallet_bindings`
错误响应：`400`, `401`, `403`, `404`, `409`, `422`, `429`, `500`, `503`

### suspendTenant

`POST /api/v2/admin/tenants/{tenantId}/suspend`

权限：`platform_admin`；F：`F15`；主要成功状态：`202`。
| 入参位置 | 字段 | 类型 | 必填 |
| --- | --- | --- | --- |
| path | `tenantId` | `OpaqueId` | 是 |
| header | `Idempotency-Key` | `string` | 是 |
| header | `X-CSRF-Token` | `string` | 是 |

Body：[TenantActionRequest](rest-schemas.md#tenantactionrequest)；Response：[Operation](rest-schemas.md#operation)

请求顶层字段：`reason`: string（必填）

响应顶层字段：`operationId`: OpaqueId（必填）；`owner`: OperationOwner（必填）；`kind`: OperationKind（必填）；`resourceId`: OpaqueId（必填）；`status`: string（必填）；`stage`: OperationStage（必填）；`observationResult`: string（可选）；`errorCode`: ErrorCode（可选）；`requestId`: OpaqueId（必填）；`createdAt`: string/date-time（必填）；`updatedAt`: string/date-time（必填）；`pollAfterSeconds`: integer（可选）

涉及表（规格声明，不自动等于每次都写）：`tenant.tenants`, `tenant.operations`
错误响应：`400`, `401`, `403`, `404`, `409`, `422`, `429`, `500`, `503`

### restoreTenant

`POST /api/v2/admin/tenants/{tenantId}/restore`

权限：`platform_admin`；F：`F15`；主要成功状态：`202`。
| 入参位置 | 字段 | 类型 | 必填 |
| --- | --- | --- | --- |
| path | `tenantId` | `OpaqueId` | 是 |
| header | `Idempotency-Key` | `string` | 是 |
| header | `X-CSRF-Token` | `string` | 是 |

Body：[TenantActionRequest](rest-schemas.md#tenantactionrequest)；Response：[Operation](rest-schemas.md#operation)

请求顶层字段：`reason`: string（必填）

响应顶层字段：`operationId`: OpaqueId（必填）；`owner`: OperationOwner（必填）；`kind`: OperationKind（必填）；`resourceId`: OpaqueId（必填）；`status`: string（必填）；`stage`: OperationStage（必填）；`observationResult`: string（可选）；`errorCode`: ErrorCode（可选）；`requestId`: OpaqueId（必填）；`createdAt`: string/date-time（必填）；`updatedAt`: string/date-time（必填）；`pollAfterSeconds`: integer（可选）

涉及表（规格声明，不自动等于每次都写）：`tenant.tenants`, `tenant.operations`, `capability.packages`, `build.build_jobs`
错误响应：`400`, `401`, `403`, `404`, `409`, `422`, `429`, `500`, `503`

### getTenantAssetCustody

`GET /api/v2/admin/tenants/{tenantId}/asset-custody`

权限：`platform_admin`；F：`F15`；主要成功状态：`200`。
| 入参位置 | 字段 | 类型 | 必填 |
| --- | --- | --- | --- |
| path | `tenantId` | `OpaqueId` | 是 |

Body：无独立命名body，见该操作schema；Response：[AssetCustody](rest-schemas.md#assetcustody)

响应顶层字段：`tenantId`: OpaqueId（必填）；`status`: string（必填）；`packageCount`: NonnegativeInt64（必填）；`buildCount`: NonnegativeInt64（必填）；`restoreUntil`: string/date-time（可选）；`workspaceDeletionOperationIds`: array<OpaqueId>（必填）

涉及表（规格声明，不自动等于每次都写）：`tenant.operations`, `capability.packages`, `build.build_jobs`
错误响应：`400`, `401`, `403`, `404`, `409`, `422`, `429`, `500`, `503`

### listAuditEvents

`GET /api/v2/admin/audit-events`

权限：`platform_admin`；F：`F17`；主要成功状态：`200`。
| 入参位置 | 字段 | 类型 | 必填 |
| --- | --- | --- | --- |
| query | `cursor` | `OpaqueId` | 否 |
| query | `limit` | `integer` | 否 |
| query | `tenantId` | `OpaqueId` | 否 |

Body：无独立命名body，见该操作schema；Response：[AuditEventPage](rest-schemas.md#auditeventpage)

响应顶层字段：`items`: array<AuditEvent>（必填）；`nextCursor`: OpaqueId（可选）

涉及表（规格声明，不自动等于每次都写）：`tenant.audit_events`
错误响应：`400`, `401`, `403`, `404`, `409`, `422`, `429`, `500`, `503`

### reenableTenant

`POST /api/v2/admin/tenants/{tenantId}/reenable`

权限：`platform_admin`；F：`F15`；主要成功状态：`202`。
| 入参位置 | 字段 | 类型 | 必填 |
| --- | --- | --- | --- |
| path | `tenantId` | `OpaqueId` | 是 |
| header | `Idempotency-Key` | `string` | 是 |
| header | `X-CSRF-Token` | `string` | 是 |

Body：[ReenableTenantRequest](rest-schemas.md#reenabletenantrequest)；Response：[Operation](rest-schemas.md#operation)

请求顶层字段：`reason`: string（必填）

响应顶层字段：`operationId`: OpaqueId（必填）；`owner`: OperationOwner（必填）；`kind`: OperationKind（必填）；`resourceId`: OpaqueId（必填）；`status`: string（必填）；`stage`: OperationStage（必填）；`observationResult`: string（可选）；`errorCode`: ErrorCode（可选）；`requestId`: OpaqueId（必填）；`createdAt`: string/date-time（必填）；`updatedAt`: string/date-time（必填）；`pollAfterSeconds`: integer（可选）

涉及表（规格声明，不自动等于每次都写）：`tenant.tenants`, `tenant.operations`
错误响应：`400`, `401`, `403`, `404`, `409`, `422`, `429`, `500`, `503`

### getTenantLifecycleOperation

`GET /api/v2/admin/tenants/{tenantId}/operations/{operationId}`

权限：`platform_admin`；F：`F15`；主要成功状态：`200`。
| 入参位置 | 字段 | 类型 | 必填 |
| --- | --- | --- | --- |
| path | `tenantId` | `OpaqueId` | 是 |
| path | `operationId` | `OpaqueId` | 是 |

Body：无独立命名body，见该操作schema；Response：[TenantLifecycleProgress](rest-schemas.md#tenantlifecycleprogress)

响应顶层字段：`tenantId`: OpaqueId（必填）；`operation`: Operation（必填）；`accessStatus`: string（必填）；`workspaceActions`: array<TenantWorkspaceAction>（必填）；`skipped`: array<TenantWorkspaceSkip>（必填）

涉及表（规格声明，不自动等于每次都写）：`tenant.operations`, `workspace.operations`
错误响应：`401`, `403`, `404`, `503`

## 3. 本域数据库全字段

`opl_tenant`：12 张表，148 列。字段权威：[02](../02_database_schema_complete.md)、[SQL](../contracts/schema.sql)；以下从db_inventory派生。**表不是自动等同DDD聚合根**；事务边界见第1节。


### tenant.tenants

CloudIdentity owns Tenant authorization; permission_version advances on access changes; deleted restore window starts at actual deleted_at, not request time

| 列 | SQL类型 | 可NULL | 默认值 | API血缘 / 原始来源 |
| --- | --- | --- | --- | --- |
| `id` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/Tenant/properties/id |
| `name` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/Tenant/properties/name |
| `status` | `text` | 否 | `'active'` | 03_api_contract_complete.yaml#/components/schemas/Tenant/properties/status |
| `suspended_at` | `timestamptz` | 是 | `—` | 02_database_schema_complete.md#tenant.tenants.suspended_at |
| `deletion_requested_at` | `timestamptz` | 是 | `—` | 02_database_schema_complete.md#tenant.tenants.deletion_requested_at |
| `restore_until` | `timestamptz` | 是 | `—` | 03_api_contract_complete.yaml#/components/schemas/Tenant/properties/restoreUntil |
| `deleted_at` | `timestamptz` | 是 | `—` | 02_database_schema_complete.md#tenant.tenants.deleted_at |
| `created_at` | `timestamptz` | 否 | `now()` | 03_api_contract_complete.yaml#/components/schemas/Tenant/properties/createdAt |
| `updated_at` | `timestamptz` | 否 | `now()` | 03_api_contract_complete.yaml#/components/schemas/Tenant/properties/updatedAt |
| `permission_version` | `bigint` | 否 | `0` | 02_database_schema_complete.md#tenant.tenants.permission_version |

约束：
- `PRIMARY KEY (id)`
- `CHECK (status IN ('active','suspended','deleting','deleted'))`
- `CHECK (permission_version >= 0)`
- `CHECK ((deleted_at IS NULL AND restore_until IS NULL) OR (deleted_at IS NOT NULL AND restore_until = deleted_at + interval '15 days'))`
- `CHECK (status <> 'deleted' OR deleted_at IS NOT NULL)`

索引：
- `{"name": "tenants_status_list", "columns": "status, created_at DESC, id DESC", "unique": false, "where": null}`

### tenant.tenant_members

同actor至多一个未撤销membership；更改锁Tenant行且不得删除最后owner

| 列 | SQL类型 | 可NULL | 默认值 | API血缘 / 原始来源 |
| --- | --- | --- | --- | --- |
| `id` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/Member/properties/id |
| `tenant_id` | `text` | 否 | `—` | 02_database_schema_complete.md#tenant.tenant_members.tenant_id |
| `actor_id` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/Member/properties/actorId |
| `role` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/Member/properties/role |
| `revoked_at` | `timestamptz` | 是 | `—` | 02_database_schema_complete.md#tenant.tenant_members.revoked_at |
| `created_at` | `timestamptz` | 否 | `now()` | 03_api_contract_complete.yaml#/components/schemas/Member/properties/createdAt |
| `updated_at` | `timestamptz` | 否 | `now()` | 02_database_schema_complete.md#tenant.tenant_members.updated_at |

约束：
- `PRIMARY KEY (id)`
- `FOREIGN KEY (tenant_id) REFERENCES tenant.tenants (id) ON DELETE RESTRICT`
- `CHECK (role IN ('owner','admin','member'))`
- `UNIQUE (tenant_id, actor_id)`

索引：
- `{"name": "tenant_members_one_active", "columns": "actor_id", "unique": true, "where": "revoked_at IS NULL"}`
- `{"name": "tenant_members_tenant", "columns": "tenant_id, created_at DESC, id DESC", "unique": false, "where": null}`

### tenant.invitations

邀请token只存hash；接受时锁invite并校验actor活动Tenant

| 列 | SQL类型 | 可NULL | 默认值 | API血缘 / 原始来源 |
| --- | --- | --- | --- | --- |
| `id` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/Invitation/properties/id |
| `tenant_id` | `text` | 否 | `—` | 02_database_schema_complete.md#tenant.invitations.tenant_id |
| `invitee_gateway_subject_id` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/Invitation/properties/inviteeGatewaySubjectId |
| `role` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/Invitation/properties/role |
| `token_hash` | `text` | 否 | `—` | 02_database_schema_complete.md#tenant.invitations.token_hash |
| `invited_by` | `text` | 否 | `—` | 02_database_schema_complete.md#tenant.invitations.invited_by |
| `accepted_by` | `text` | 是 | `—` | 02_database_schema_complete.md#tenant.invitations.accepted_by |
| `expires_at` | `timestamptz` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/Invitation/properties/expiresAt |
| `accepted_at` | `timestamptz` | 是 | `—` | 02_database_schema_complete.md#tenant.invitations.accepted_at |
| `revoked_at` | `timestamptz` | 是 | `—` | 02_database_schema_complete.md#tenant.invitations.revoked_at |
| `created_at` | `timestamptz` | 否 | `now()` | 03_api_contract_complete.yaml#/components/schemas/Invitation/properties/createdAt |
| `updated_at` | `timestamptz` | 否 | `now()` | 02_database_schema_complete.md#tenant.invitations.updated_at |

约束：
- `PRIMARY KEY (id)`
- `FOREIGN KEY (tenant_id) REFERENCES tenant.tenants (id) ON DELETE RESTRICT`
- `CHECK (role IN ('admin','member'))`
- `UNIQUE (token_hash)`
- `CHECK (NOT (accepted_at IS NOT NULL AND revoked_at IS NOT NULL))`

索引：
- `{"name": "member_invites_tenant", "columns": "tenant_id, created_at DESC, id DESC", "unique": false, "where": null}`

### tenant.sessions

只存cookie/CSRF摘要及Gateway会话Secret Store引用；每次请求复验Tenant/member

| 列 | SQL类型 | 可NULL | 默认值 | API血缘 / 原始来源 |
| --- | --- | --- | --- | --- |
| `id` | `text` | 否 | `—` | 02_database_schema_complete.md#tenant.sessions.id |
| `session_hash` | `text` | 否 | `—` | 02_database_schema_complete.md#tenant.sessions.session_hash |
| `actor_id` | `text` | 否 | `—` | 02_database_schema_complete.md#tenant.sessions.actor_id |
| `tenant_id` | `text` | 是 | `—` | 02_database_schema_complete.md#tenant.sessions.tenant_id |
| `gateway_session_ref` | `text` | 否 | `—` | 02_database_schema_complete.md#tenant.sessions.gateway_session_ref |
| `csrf_hash` | `text` | 否 | `—` | 02_database_schema_complete.md#tenant.sessions.csrf_hash |
| `expires_at` | `timestamptz` | 否 | `—` | 02_database_schema_complete.md#tenant.sessions.expires_at |
| `revoked_at` | `timestamptz` | 是 | `—` | 02_database_schema_complete.md#tenant.sessions.revoked_at |
| `created_at` | `timestamptz` | 否 | `now()` | 02_database_schema_complete.md#tenant.sessions.created_at |
| `updated_at` | `timestamptz` | 否 | `now()` | 02_database_schema_complete.md#tenant.sessions.updated_at |

约束：
- `PRIMARY KEY (id)`
- `FOREIGN KEY (tenant_id) REFERENCES tenant.tenants (id) ON DELETE RESTRICT`
- `UNIQUE (session_hash)`

索引：
- `{"name": "sessions_actor", "columns": "actor_id", "unique": false, "where": null}`
- `{"name": "sessions_tenant", "columns": "tenant_id", "unique": false, "where": null}`

### tenant.audit_events

append-only权限审计，不含token/Key/密码

| 列 | SQL类型 | 可NULL | 默认值 | API血缘 / 原始来源 |
| --- | --- | --- | --- | --- |
| `id` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/AuditEvent/properties/id |
| `tenant_id` | `text` | 是 | `—` | 02_database_schema_complete.md#tenant.audit_events.tenant_id |
| `actor_id` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/AuditEvent/properties/actorId |
| `action` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/AuditEvent/properties/action |
| `resource_type` | `text` | 否 | `—` | 02_database_schema_complete.md#tenant.audit_events.resource_type |
| `resource_id` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/AuditEvent/properties/resourceId |
| `request_id` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/AuditEvent/properties/requestId |
| `safe_details` | `jsonb` | 否 | `—` | 02_database_schema_complete.md#tenant.audit_events.safe_details |
| `created_at` | `timestamptz` | 否 | `now()` | 03_api_contract_complete.yaml#/components/schemas/AuditEvent/properties/createdAt |
| `outcome` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/AuditEvent/properties/outcome |

约束：
- `PRIMARY KEY (id)`
- `FOREIGN KEY (tenant_id) REFERENCES tenant.tenants (id) ON DELETE RESTRICT`

索引：
- `{"name": "tenant_audit_list", "columns": "tenant_id, created_at DESC, id DESC", "unique": false, "where": null}`
- `{"name": "tenant_audit_request", "columns": "request_id", "unique": false, "where": null}`

### tenant.outbox_events

本Owner状态事务同写immutable事件；revision只在本Owner聚合锁内分配

| 列 | SQL类型 | 可NULL | 默认值 | API血缘 / 原始来源 |
| --- | --- | --- | --- | --- |
| `id` | `text` | 否 | `—` | 02_database_schema_complete.md#tenant.outbox_events.id |
| `event_type` | `text` | 否 | `—` | 02_database_schema_complete.md#tenant.outbox_events.event_type |
| `schema_version` | `integer` | 否 | `—` | 02_database_schema_complete.md#tenant.outbox_events.schema_version |
| `aggregate_type` | `text` | 否 | `—` | 02_database_schema_complete.md#tenant.outbox_events.aggregate_type |
| `aggregate_id` | `text` | 否 | `—` | 02_database_schema_complete.md#tenant.outbox_events.aggregate_id |
| `aggregate_revision` | `bigint` | 否 | `—` | 02_database_schema_complete.md#tenant.outbox_events.aggregate_revision |
| `tenant_id` | `text` | 是 | `—` | 02_database_schema_complete.md#tenant.outbox_events.tenant_id |
| `correlation_id` | `text` | 否 | `—` | 02_database_schema_complete.md#tenant.outbox_events.correlation_id |
| `causation_id` | `text` | 是 | `—` | 02_database_schema_complete.md#tenant.outbox_events.causation_id |
| `payload` | `jsonb` | 否 | `—` | 02_database_schema_complete.md#tenant.outbox_events.payload |
| `payload_sha256` | `text` | 否 | `—` | 02_database_schema_complete.md#tenant.outbox_events.payload_sha256 |
| `occurred_at` | `timestamptz` | 否 | `—` | 02_database_schema_complete.md#tenant.outbox_events.occurred_at |
| `created_at` | `timestamptz` | 否 | `now()` | 02_database_schema_complete.md#tenant.outbox_events.created_at |

约束：
- `PRIMARY KEY (id)`
- `CHECK (schema_version > 0 AND aggregate_revision >= 0)`
- `CHECK (payload_sha256 ~ '^[0-9a-f]{64}$')`
- `UNIQUE (aggregate_type, aggregate_id, aggregate_revision, event_type)`

索引：
- `{"name": "outbox_events_aggregate", "columns": "aggregate_type, aggregate_id, aggregate_revision", "unique": false, "where": null}`

### tenant.outbox_deliveries

各consumer独立ACK，禁止单个ACK丢其他消费者事件；lease仅本库

| 列 | SQL类型 | 可NULL | 默认值 | API血缘 / 原始来源 |
| --- | --- | --- | --- | --- |
| `id` | `text` | 否 | `—` | 02_database_schema_complete.md#tenant.outbox_deliveries.id |
| `event_id` | `text` | 否 | `—` | 02_database_schema_complete.md#tenant.outbox_deliveries.event_id |
| `consumer_owner` | `text` | 否 | `—` | 02_database_schema_complete.md#tenant.outbox_deliveries.consumer_owner |
| `attempt_count` | `integer` | 否 | `0` | 02_database_schema_complete.md#tenant.outbox_deliveries.attempt_count |
| `next_attempt_at` | `timestamptz` | 否 | `now()` | 02_database_schema_complete.md#tenant.outbox_deliveries.next_attempt_at |
| `acknowledged_at` | `timestamptz` | 是 | `—` | 02_database_schema_complete.md#tenant.outbox_deliveries.acknowledged_at |
| `last_error_code` | `text` | 是 | `—` | 02_database_schema_complete.md#tenant.outbox_deliveries.last_error_code |
| `lease_token` | `text` | 是 | `—` | 02_database_schema_complete.md#tenant.outbox_deliveries.lease_token |
| `lease_until` | `timestamptz` | 是 | `—` | 02_database_schema_complete.md#tenant.outbox_deliveries.lease_until |
| `created_at` | `timestamptz` | 否 | `now()` | 02_database_schema_complete.md#tenant.outbox_deliveries.created_at |
| `updated_at` | `timestamptz` | 否 | `now()` | 02_database_schema_complete.md#tenant.outbox_deliveries.updated_at |

约束：
- `PRIMARY KEY (id)`
- `FOREIGN KEY (event_id) REFERENCES tenant.outbox_events (id) ON DELETE RESTRICT`
- `UNIQUE (event_id, consumer_owner)`
- `CHECK (attempt_count >= 0)`
- `CHECK ((lease_token IS NULL) = (lease_until IS NULL))`

索引：
- `{"name": "outbox_deliveries_pending", "columns": "next_attempt_at, id", "unique": false, "where": "acknowledged_at IS NULL"}`

### tenant.inbox_events

去重hash/业务状态/processed_at/后续Outbox同本库事务；乱序不能覆盖新版本

| 列 | SQL类型 | 可NULL | 默认值 | API血缘 / 原始来源 |
| --- | --- | --- | --- | --- |
| `id` | `text` | 否 | `—` | 02_database_schema_complete.md#tenant.inbox_events.id |
| `source_owner` | `text` | 否 | `—` | 02_database_schema_complete.md#tenant.inbox_events.source_owner |
| `source_event_id` | `text` | 否 | `—` | 02_database_schema_complete.md#tenant.inbox_events.source_event_id |
| `event_type` | `text` | 否 | `—` | 02_database_schema_complete.md#tenant.inbox_events.event_type |
| `schema_version` | `integer` | 否 | `—` | 02_database_schema_complete.md#tenant.inbox_events.schema_version |
| `aggregate_type` | `text` | 否 | `—` | 02_database_schema_complete.md#tenant.inbox_events.aggregate_type |
| `aggregate_id` | `text` | 否 | `—` | 02_database_schema_complete.md#tenant.inbox_events.aggregate_id |
| `aggregate_revision` | `bigint` | 否 | `—` | 02_database_schema_complete.md#tenant.inbox_events.aggregate_revision |
| `payload_sha256` | `text` | 否 | `—` | 02_database_schema_complete.md#tenant.inbox_events.payload_sha256 |
| `payload` | `jsonb` | 否 | `—` | 02_database_schema_complete.md#tenant.inbox_events.payload |
| `received_at` | `timestamptz` | 否 | `now()` | 02_database_schema_complete.md#tenant.inbox_events.received_at |
| `processed_at` | `timestamptz` | 是 | `—` | 02_database_schema_complete.md#tenant.inbox_events.processed_at |
| `result_resource_id` | `text` | 是 | `—` | 02_database_schema_complete.md#tenant.inbox_events.result_resource_id |
| `error_code` | `text` | 是 | `—` | 02_database_schema_complete.md#tenant.inbox_events.error_code |

约束：
- `PRIMARY KEY (id)`
- `UNIQUE (source_owner, source_event_id)`
- `CHECK (schema_version > 0 AND aggregate_revision >= 0)`
- `CHECK (payload_sha256 ~ '^[0-9a-f]{64}$')`

索引：
- `{"name": "inbox_events_pending", "columns": "received_at, id", "unique": false, "where": "processed_at IS NULL"}`
- `{"name": "inbox_events_aggregate", "columns": "source_owner, aggregate_type, aggregate_id, aggregate_revision", "unique": false, "where": null}`

### tenant.idempotency_records

命令创建同事务；operation_name是API操作名不是新Operation ID；安全响应无Key明文；义务存续无TTL清理

| 列 | SQL类型 | 可NULL | 默认值 | API血缘 / 原始来源 |
| --- | --- | --- | --- | --- |
| `id` | `text` | 否 | `—` | 02_database_schema_complete.md#tenant.idempotency_records.id |
| `tenant_scope` | `text` | 否 | `—` | 02_database_schema_complete.md#tenant.idempotency_records.tenant_scope |
| `actor_scope` | `text` | 否 | `—` | 02_database_schema_complete.md#tenant.idempotency_records.actor_scope |
| `operation_name` | `text` | 否 | `—` | 02_database_schema_complete.md#tenant.idempotency_records.operation_name |
| `idempotency_key` | `text` | 否 | `—` | 02_database_schema_complete.md#tenant.idempotency_records.idempotency_key |
| `request_sha256` | `text` | 否 | `—` | 02_database_schema_complete.md#tenant.idempotency_records.request_sha256 |
| `resource_id` | `text` | 否 | `—` | 02_database_schema_complete.md#tenant.idempotency_records.resource_id |
| `operation_id` | `text` | 是 | `—` | 02_database_schema_complete.md#tenant.idempotency_records.operation_id |
| `response_status` | `integer` | 否 | `—` | 02_database_schema_complete.md#tenant.idempotency_records.response_status |
| `response_body` | `jsonb` | 否 | `—` | 02_database_schema_complete.md#tenant.idempotency_records.response_body |
| `created_at` | `timestamptz` | 否 | `now()` | 02_database_schema_complete.md#tenant.idempotency_records.created_at |

约束：
- `PRIMARY KEY (id)`
- `UNIQUE (tenant_scope, actor_scope, operation_name, idempotency_key)`
- `CHECK (request_sha256 ~ '^[0-9a-f]{64}$')`
- `CHECK (response_status BETWEEN 100 AND 599)`

索引：
- `{"name": "idempotency_records_resource", "columns": "resource_id", "unique": false, "where": null}`

### tenant.operations

目标Owner持异步Operation；BFF路由无中央writer；Build创建201回Job；只有Workspace配Saga

| 列 | SQL类型 | 可NULL | 默认值 | API血缘 / 原始来源 |
| --- | --- | --- | --- | --- |
| `id` | `text` | 否 | `—` | 02_database_schema_complete.md#tenant.operations.id |
| `tenant_id` | `text` | 是 | `—` | 02_database_schema_complete.md#tenant.operations.tenant_id |
| `actor_id` | `text` | 否 | `—` | 02_database_schema_complete.md#tenant.operations.actor_id |
| `kind` | `text` | 否 | `—` | 02_database_schema_complete.md#tenant.operations.kind |
| `resource_id` | `text` | 否 | `—` | 02_database_schema_complete.md#tenant.operations.resource_id |
| `status` | `text` | 否 | `'accepted'` | 02_database_schema_complete.md#tenant.operations.status |
| `stage` | `text` | 否 | `—` | 02_database_schema_complete.md#tenant.operations.stage |
| `error_code` | `text` | 是 | `—` | 02_database_schema_complete.md#tenant.operations.error_code |
| `observation_result` | `text` | 是 | `—` | 02_database_schema_complete.md#tenant.operations.observation_result |
| `request_id` | `text` | 否 | `—` | 02_database_schema_complete.md#tenant.operations.request_id |
| `accepted_input` | `jsonb` | 否 | `—` | 02_database_schema_complete.md#tenant.operations.accepted_input |
| `result` | `jsonb` | 是 | `—` | 02_database_schema_complete.md#tenant.operations.result |
| `worker_lease_token` | `text` | 是 | `—` | 02_database_schema_complete.md#tenant.operations.worker_lease_token |
| `worker_lease_until` | `timestamptz` | 是 | `—` | 02_database_schema_complete.md#tenant.operations.worker_lease_until |
| `started_at` | `timestamptz` | 是 | `—` | 02_database_schema_complete.md#tenant.operations.started_at |
| `completed_at` | `timestamptz` | 是 | `—` | 02_database_schema_complete.md#tenant.operations.completed_at |
| `created_at` | `timestamptz` | 否 | `now()` | 02_database_schema_complete.md#tenant.operations.created_at |
| `updated_at` | `timestamptz` | 否 | `now()` | 02_database_schema_complete.md#tenant.operations.updated_at |

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

### tenant.authorization_contexts

Opaque context introspected by CloudIdentity; authenticated mTLS caller, audience, action, resource and current permission version all bound; no bearer-token body

| 列 | SQL类型 | 可NULL | 默认值 | API血缘 / 原始来源 |
| --- | --- | --- | --- | --- |
| `id` | `text` | 否 | `—` | 02_database_schema_complete.md#tenant.authorization_contexts.id |
| `scope_type` | `text` | 否 | `—` | 02_database_schema_complete.md#tenant.authorization_contexts.scope_type |
| `tenant_id` | `text` | 是 | `—` | 02_database_schema_complete.md#tenant.authorization_contexts.tenant_id |
| `actor_id` | `text` | 否 | `—` | 02_database_schema_complete.md#tenant.authorization_contexts.actor_id |
| `session_id` | `text` | 是 | `—` | 02_database_schema_complete.md#tenant.authorization_contexts.session_id |
| `permission_version` | `bigint` | 否 | `—` | 02_database_schema_complete.md#tenant.authorization_contexts.permission_version |
| `audience_owner` | `text` | 否 | `—` | 02_database_schema_complete.md#tenant.authorization_contexts.audience_owner |
| `action` | `text` | 否 | `—` | 02_database_schema_complete.md#tenant.authorization_contexts.action |
| `resource_kind` | `text` | 否 | `—` | 02_database_schema_complete.md#tenant.authorization_contexts.resource_kind |
| `resource_id` | `text` | 是 | `—` | 02_database_schema_complete.md#tenant.authorization_contexts.resource_id |
| `issuer` | `text` | 否 | `—` | 02_database_schema_complete.md#tenant.authorization_contexts.issuer |
| `issued_at` | `timestamptz` | 否 | `—` | 02_database_schema_complete.md#tenant.authorization_contexts.issued_at |
| `expires_at` | `timestamptz` | 否 | `—` | 02_database_schema_complete.md#tenant.authorization_contexts.expires_at |
| `revoked_at` | `timestamptz` | 是 | `—` | 02_database_schema_complete.md#tenant.authorization_contexts.revoked_at |
| `accepted_operation_grant_id` | `text` | 是 | `—` | 02_database_schema_complete.md#tenant.authorization_contexts.accepted_operation_grant_id |

约束：
- `PRIMARY KEY (id)`
- `FOREIGN KEY (tenant_id) REFERENCES tenant.tenants (id) ON DELETE RESTRICT`
- `FOREIGN KEY (session_id) REFERENCES tenant.sessions (id) ON DELETE RESTRICT`
- `CHECK (scope_type IN ('tenant','platform'))`
- `CHECK (issuer IN ('cloud_identity'))`
- `CHECK ((scope_type = 'tenant') = (tenant_id IS NOT NULL))`
- `CHECK (permission_version >= 0)`
- `CHECK (expires_at > issued_at)`
- `CHECK (num_nonnulls(session_id,accepted_operation_grant_id) = 1)`
- `FOREIGN KEY (accepted_operation_grant_id) REFERENCES tenant.accepted_operation_grants (id) ON DELETE RESTRICT`

索引：
- `{"name": "authorization_contexts_session", "columns": "session_id", "unique": false, "where": null}`
- `{"name": "authorization_contexts_actor_scope", "columns": "actor_id, tenant_id, expires_at", "unique": false, "where": null}`

### tenant.accepted_operation_grants

Bounded original accepted-operation obligation; revocation permits only approved completion/cancel/closeout, never new procurement

| 列 | SQL类型 | 可NULL | 默认值 | API血缘 / 原始来源 |
| --- | --- | --- | --- | --- |
| `id` | `text` | 否 | `—` | 02_database_schema_complete.md#tenant.accepted_operation_grants.id |
| `scope_type` | `text` | 否 | `—` | 02_database_schema_complete.md#tenant.accepted_operation_grants.scope_type |
| `tenant_id` | `text` | 是 | `—` | 02_database_schema_complete.md#tenant.accepted_operation_grants.tenant_id |
| `actor_id` | `text` | 否 | `—` | 02_database_schema_complete.md#tenant.accepted_operation_grants.actor_id |
| `accepted_operation_owner` | `text` | 否 | `—` | 02_database_schema_complete.md#tenant.accepted_operation_grants.accepted_operation_owner |
| `accepted_operation_id` | `text` | 否 | `—` | 02_database_schema_complete.md#tenant.accepted_operation_grants.accepted_operation_id |
| `accepted_action` | `text` | 否 | `—` | 02_database_schema_complete.md#tenant.accepted_operation_grants.accepted_action |
| `resource_id` | `text` | 否 | `—` | 02_database_schema_complete.md#tenant.accepted_operation_grants.resource_id |
| `accepted_permission_version` | `bigint` | 否 | `—` | 02_database_schema_complete.md#tenant.accepted_operation_grants.accepted_permission_version |
| `allowed_actions` | `text[]` | 否 | `—` | 02_database_schema_complete.md#tenant.accepted_operation_grants.allowed_actions |
| `issued_at` | `timestamptz` | 否 | `—` | 02_database_schema_complete.md#tenant.accepted_operation_grants.issued_at |
| `expires_at` | `timestamptz` | 是 | `—` | 02_database_schema_complete.md#tenant.accepted_operation_grants.expires_at |
| `revoked_at` | `timestamptz` | 是 | `—` | 02_database_schema_complete.md#tenant.accepted_operation_grants.revoked_at |
| `obligation_completed_at` | `timestamptz` | 是 | `—` | 02_database_schema_complete.md#tenant.accepted_operation_grants.obligation_completed_at |
| `mode` | `text` | 否 | `—` | 02_database_schema_complete.md#tenant.accepted_operation_grants.mode |
| `renewal_consent_id` | `text` | 是 | `—` | 02_database_schema_complete.md#tenant.accepted_operation_grants.renewal_consent_id |
| `subscription_period_id` | `text` | 是 | `—` | 02_database_schema_complete.md#tenant.accepted_operation_grants.subscription_period_id |

约束：
- `PRIMARY KEY (id)`
- `FOREIGN KEY (tenant_id) REFERENCES tenant.tenants (id) ON DELETE RESTRICT`
- `CHECK (scope_type IN ('tenant','platform'))`
- `CHECK ((scope_type = 'tenant') = (tenant_id IS NOT NULL))`
- `CHECK (accepted_permission_version >= 0)`
- `CHECK (cardinality(allowed_actions) > 0 AND array_position(allowed_actions,NULL) IS NULL)`
- `CHECK (expires_at IS NULL OR expires_at > issued_at)`
- `UNIQUE (accepted_operation_owner, accepted_operation_id)`
- `CHECK (mode IN ('continue_original','closeout_only','revoked'))`

索引：
- `{"name": "operation_grants_resource", "columns": "tenant_id, resource_id", "unique": false, "where": null}`
- `{"name": "operation_grants_open", "columns": "issued_at", "unique": false, "where": "obligation_completed_at IS NULL"}`

### 本Owner应实现的RPC方法全集（目标，不是挂载证据）

共享OwnerOperations/CommitReadback/Inbox分别由本域实现，不形成中央业务服务；通用Operation REST由BFF按显式Owner路由；本域只负责自己的Operation与授权。

| RPC | 请求 | 响应 |
| --- | --- | --- |
| `TenantProductService.GetLoginContext` | [GetLoginContextRpcRequest](rpc-messages.md#getlogincontextrpcrequest) | [LoginContext](rpc-messages.md#logincontext) |
| `TenantProductService.Login` | [LoginRpcRequest](rpc-messages.md#loginrpcrequest) | [Session](rpc-messages.md#session) |
| `TenantProductService.GetSession` | [GetSessionRpcRequest](rpc-messages.md#getsessionrpcrequest) | [Session](rpc-messages.md#session) |
| `TenantProductService.Logout` | [LogoutRpcRequest](rpc-messages.md#logoutrpcrequest) | [Empty](rpc-messages.md#empty) |
| `TenantProductService.GetTenant` | [GetTenantRpcRequest](rpc-messages.md#gettenantrpcrequest) | [Tenant](rpc-messages.md#tenant) |
| `TenantProductService.ListMembers` | [ListMembersRpcRequest](rpc-messages.md#listmembersrpcrequest) | [MemberPage](rpc-messages.md#memberpage) |
| `TenantProductService.ListInvitations` | [ListInvitationsRpcRequest](rpc-messages.md#listinvitationsrpcrequest) | [InvitationPage](rpc-messages.md#invitationpage) |
| `TenantProductService.InviteMember` | [InviteMemberRpcRequest](rpc-messages.md#invitememberrpcrequest) | [Invitation](rpc-messages.md#invitation) |
| `TenantProductService.AcceptInvitation` | [AcceptInvitationRpcRequest](rpc-messages.md#acceptinvitationrpcrequest) | [Member](rpc-messages.md#member) |
| `TenantProductService.RevokeInvitation` | [RevokeInvitationRpcRequest](rpc-messages.md#revokeinvitationrpcrequest) | [Invitation](rpc-messages.md#invitation) |
| `TenantProductService.UpdateMemberRole` | [UpdateMemberRoleRpcRequest](rpc-messages.md#updatememberrolerpcrequest) | [Member](rpc-messages.md#member) |
| `TenantProductService.RemoveMember` | [RemoveMemberRpcRequest](rpc-messages.md#removememberrpcrequest) | [Empty](rpc-messages.md#empty) |
| `TenantProductService.ListTenants` | [ListTenantsRpcRequest](rpc-messages.md#listtenantsrpcrequest) | [TenantPage](rpc-messages.md#tenantpage) |
| `TenantProductService.CreateTenant` | [CreateTenantRpcRequest](rpc-messages.md#createtenantrpcrequest) | [Operation](rpc-messages.md#operation) |
| `TenantProductService.GetAdminTenant` | [GetAdminTenantRpcRequest](rpc-messages.md#getadmintenantrpcrequest) | [Tenant](rpc-messages.md#tenant) |
| `TenantProductService.DeleteTenant` | [DeleteTenantRpcRequest](rpc-messages.md#deletetenantrpcrequest) | [Operation](rpc-messages.md#operation) |
| `TenantProductService.BindTenantWallet` | [BindTenantWalletRpcRequest](rpc-messages.md#bindtenantwalletrpcrequest) | [Operation](rpc-messages.md#operation) |
| `TenantProductService.SuspendTenant` | [SuspendTenantRpcRequest](rpc-messages.md#suspendtenantrpcrequest) | [Operation](rpc-messages.md#operation) |
| `TenantProductService.RestoreTenant` | [RestoreTenantRpcRequest](rpc-messages.md#restoretenantrpcrequest) | [Operation](rpc-messages.md#operation) |
| `TenantProductService.GetTenantAssetCustody` | [GetTenantAssetCustodyRpcRequest](rpc-messages.md#gettenantassetcustodyrpcrequest) | [AssetCustody](rpc-messages.md#assetcustody) |
| `TenantProductService.ListAuditEvents` | [ListAuditEventsRpcRequest](rpc-messages.md#listauditeventsrpcrequest) | [AuditEventPage](rpc-messages.md#auditeventpage) |
| `TenantProductService.ReenableTenant` | [ReenableTenantRpcRequest](rpc-messages.md#reenabletenantrpcrequest) | [Operation](rpc-messages.md#operation) |
| `TenantProductService.GetTenantLifecycleOperation` | [GetTenantLifecycleOperationRpcRequest](rpc-messages.md#gettenantlifecycleoperationrpcrequest) | [TenantLifecycleProgress](rpc-messages.md#tenantlifecycleprogress) |
| `OwnerOperations.Read` | [OwnerOperationRequest](rpc-messages.md#owneroperationrequest) | [Operation](rpc-messages.md#operation) |
| `OwnerOperations.Reconcile` | [ReconcileOperationRpcRequest](rpc-messages.md#reconcileoperationrpcrequest) | [Operation](rpc-messages.md#operation) |
| `OwnerCommitReadback.ReadOwnerCommit` | [ReadOwnerCommitRequest](rpc-messages.md#readownercommitrequest) | [OwnerCommitEvidence](rpc-messages.md#ownercommitevidence) |
| `CloudIdentityAuthorization.AuthorizeAction` | [AuthorizationRequest](rpc-messages.md#authorizationrequest) | [AuthorizationDecision](rpc-messages.md#authorizationdecision) |
| `CloudIdentityAuthorization.GetAuthorizationContext` | [GetAuthorizationContextRequest](rpc-messages.md#getauthorizationcontextrequest) | [AuthorizationDecision](rpc-messages.md#authorizationdecision) |
| `CloudIdentityAuthorization.IssueAcceptedOperationGrant` | [AcceptedOperationGrantRequest](rpc-messages.md#acceptedoperationgrantrequest) | [AcceptedOperationGrant](rpc-messages.md#acceptedoperationgrant) |
| `DomainInbox.Deliver` | [DeliverEventRequest](rpc-messages.md#delivereventrequest) | [InboxAck](rpc-messages.md#inboxack) |

## 4. 跨域调用：调用者 → 拥有方 → 字段 → 结果

以下只列`domain_flows.json`声明的业务边；共享通道/尚无业务边的RPC不能推断成已经实现。


### F01.1 bff → tenant / TenantProductService.Login

按当前入口/阶段与06/13状态机前置执行；不是无条件调用

请求 [LoginRpcRequest](rpc-messages.md#loginrpcrequest)：context#1: CallContext；body#2: LoginRequest

返回 [Session](rpc-messages.md#session)：actor_id#1: string；display_name#2: string；tenant_id#3: optional string；tenant_name#4: optional string；role#5: optional TenantRoleEnum；permissions#6: repeated AuthorizationActionEnum；csrf_token#7: string；expires_at#8: Timestamp

接收方写入：`tenant.sessions`

完成证据：Gateway认证主体与Cloud成员关系确定，session无密码持久化

失败/未知：登录失败统一提示；不创建新Gateway钱包

### F01.2 receiving_owner → tenant / CloudIdentityAuthorization.AuthorizeAction

按当前入口/阶段与06/13状态机前置执行；不是无条件调用

请求 [AuthorizationRequest](rpc-messages.md#authorizationrequest)：scope#1: AuthorizationScope；actor_id#2: string；session_id#3: optional string；authorization_context_id#4: optional string；accepted_operation_grant_id#5: optional string；audience_owner#6: OwnerEnum；action#7: AuthorizationActionEnum；resource#8: AuthorizationResource；expected_permission_version#9: optional int64；request_id#10: string

返回 [AuthorizationDecision](rpc-messages.md#authorizationdecision)：result#1: AuthorizationResult；issuer#2: AuthorizationIssuer；authorization_context_id#3: optional string；scope#4: AuthorizationScope；actor_id#5: string；session_id#6: optional string；audience_owner#7: OwnerEnum；action#8: AuthorizationActionEnum；resource#9: AuthorizationResource；permission_version#10: int64；issued_at#11: Timestamp；expires_at#12: Timestamp；denial_code#13: optional ErrorCodeEnum；accepted_operation_grant_id#14: optional string

接收方写入：

完成证据：session/grant/action/resource/audience/权限版本确切一致

失败/未知：deny不改用管理员；unknown不发后续副作用

### F01.3 bff → tenant / TenantProductService.UpdateMemberRole

按当前入口/阶段与06/13状态机前置执行；不是无条件调用

请求 [UpdateMemberRoleRpcRequest](rpc-messages.md#updatememberrolerpcrequest)：context#1: CallContext；body#2: UpdateMemberRoleRequest；member_id#3: string

返回 [Member](rpc-messages.md#member)：id#1: string；actor_id#2: string；display_name#3: string；role#4: TenantRoleEnum；status#5: MemberStatusEnum；created_at#6: Timestamp

接收方写入：`tenant.tenant_members`, `tenant.audit_events`

完成证据：权限版本变化及最后owner保护

失败/未知：旧版本/越权拒绝，不只隐藏按钮

### F02.1 capability → tenant / CloudIdentityAuthorization.AuthorizeAction

按当前入口/阶段与06/13状态机前置执行；不是无条件调用

请求 [AuthorizationRequest](rpc-messages.md#authorizationrequest)：scope#1: AuthorizationScope；actor_id#2: string；session_id#3: optional string；authorization_context_id#4: optional string；accepted_operation_grant_id#5: optional string；audience_owner#6: OwnerEnum；action#7: AuthorizationActionEnum；resource#8: AuthorizationResource；expected_permission_version#9: optional int64；request_id#10: string

返回 [AuthorizationDecision](rpc-messages.md#authorizationdecision)：result#1: AuthorizationResult；issuer#2: AuthorizationIssuer；authorization_context_id#3: optional string；scope#4: AuthorizationScope；actor_id#5: string；session_id#6: optional string；audience_owner#7: OwnerEnum；action#8: AuthorizationActionEnum；resource#9: AuthorizationResource；permission_version#10: int64；issued_at#11: Timestamp；expires_at#12: Timestamp；denial_code#13: optional ErrorCodeEnum；accepted_operation_grant_id#14: optional string

接收方写入：

完成证据：session/grant/action/resource/audience/权限版本确切一致

失败/未知：deny不改用管理员；unknown不发后续副作用

### F03.1 capability → tenant / CloudIdentityAuthorization.AuthorizeAction

按当前入口/阶段与06/13状态机前置执行；不是无条件调用

请求 [AuthorizationRequest](rpc-messages.md#authorizationrequest)：scope#1: AuthorizationScope；actor_id#2: string；session_id#3: optional string；authorization_context_id#4: optional string；accepted_operation_grant_id#5: optional string；audience_owner#6: OwnerEnum；action#7: AuthorizationActionEnum；resource#8: AuthorizationResource；expected_permission_version#9: optional int64；request_id#10: string

返回 [AuthorizationDecision](rpc-messages.md#authorizationdecision)：result#1: AuthorizationResult；issuer#2: AuthorizationIssuer；authorization_context_id#3: optional string；scope#4: AuthorizationScope；actor_id#5: string；session_id#6: optional string；audience_owner#7: OwnerEnum；action#8: AuthorizationActionEnum；resource#9: AuthorizationResource；permission_version#10: int64；issued_at#11: Timestamp；expires_at#12: Timestamp；denial_code#13: optional ErrorCodeEnum；accepted_operation_grant_id#14: optional string

接收方写入：

完成证据：session/grant/action/resource/audience/权限版本确切一致

失败/未知：deny不改用管理员；unknown不发后续副作用

### F04.1 capability → tenant / CloudIdentityAuthorization.AuthorizeAction

按当前入口/阶段与06/13状态机前置执行；不是无条件调用

请求 [AuthorizationRequest](rpc-messages.md#authorizationrequest)：scope#1: AuthorizationScope；actor_id#2: string；session_id#3: optional string；authorization_context_id#4: optional string；accepted_operation_grant_id#5: optional string；audience_owner#6: OwnerEnum；action#7: AuthorizationActionEnum；resource#8: AuthorizationResource；expected_permission_version#9: optional int64；request_id#10: string

返回 [AuthorizationDecision](rpc-messages.md#authorizationdecision)：result#1: AuthorizationResult；issuer#2: AuthorizationIssuer；authorization_context_id#3: optional string；scope#4: AuthorizationScope；actor_id#5: string；session_id#6: optional string；audience_owner#7: OwnerEnum；action#8: AuthorizationActionEnum；resource#9: AuthorizationResource；permission_version#10: int64；issued_at#11: Timestamp；expires_at#12: Timestamp；denial_code#13: optional ErrorCodeEnum；accepted_operation_grant_id#14: optional string

接收方写入：

完成证据：session/grant/action/resource/audience/权限版本确切一致

失败/未知：deny不改用管理员；unknown不发后续副作用

### F06.1 capability → tenant / CloudIdentityAuthorization.AuthorizeAction

按当前入口/阶段与06/13状态机前置执行；不是无条件调用

请求 [AuthorizationRequest](rpc-messages.md#authorizationrequest)：scope#1: AuthorizationScope；actor_id#2: string；session_id#3: optional string；authorization_context_id#4: optional string；accepted_operation_grant_id#5: optional string；audience_owner#6: OwnerEnum；action#7: AuthorizationActionEnum；resource#8: AuthorizationResource；expected_permission_version#9: optional int64；request_id#10: string

返回 [AuthorizationDecision](rpc-messages.md#authorizationdecision)：result#1: AuthorizationResult；issuer#2: AuthorizationIssuer；authorization_context_id#3: optional string；scope#4: AuthorizationScope；actor_id#5: string；session_id#6: optional string；audience_owner#7: OwnerEnum；action#8: AuthorizationActionEnum；resource#9: AuthorizationResource；permission_version#10: int64；issued_at#11: Timestamp；expires_at#12: Timestamp；denial_code#13: optional ErrorCodeEnum；accepted_operation_grant_id#14: optional string

接收方写入：

完成证据：session/grant/action/resource/audience/权限版本确切一致

失败/未知：deny不改用管理员；unknown不发后续副作用

### F07.1 workspace → tenant / CloudIdentityAuthorization.AuthorizeAction

按当前入口/阶段与06/13状态机前置执行；不是无条件调用

请求 [AuthorizationRequest](rpc-messages.md#authorizationrequest)：scope#1: AuthorizationScope；actor_id#2: string；session_id#3: optional string；authorization_context_id#4: optional string；accepted_operation_grant_id#5: optional string；audience_owner#6: OwnerEnum；action#7: AuthorizationActionEnum；resource#8: AuthorizationResource；expected_permission_version#9: optional int64；request_id#10: string

返回 [AuthorizationDecision](rpc-messages.md#authorizationdecision)：result#1: AuthorizationResult；issuer#2: AuthorizationIssuer；authorization_context_id#3: optional string；scope#4: AuthorizationScope；actor_id#5: string；session_id#6: optional string；audience_owner#7: OwnerEnum；action#8: AuthorizationActionEnum；resource#9: AuthorizationResource；permission_version#10: int64；issued_at#11: Timestamp；expires_at#12: Timestamp；denial_code#13: optional ErrorCodeEnum；accepted_operation_grant_id#14: optional string

接收方写入：

完成证据：session/grant/action/resource/audience/权限版本确切一致

失败/未知：deny不改用管理员；unknown不发后续副作用

### F08.1 workspace → tenant / CloudIdentityAuthorization.AuthorizeAction

按当前入口/阶段与06/13状态机前置执行；不是无条件调用

请求 [AuthorizationRequest](rpc-messages.md#authorizationrequest)：scope#1: AuthorizationScope；actor_id#2: string；session_id#3: optional string；authorization_context_id#4: optional string；accepted_operation_grant_id#5: optional string；audience_owner#6: OwnerEnum；action#7: AuthorizationActionEnum；resource#8: AuthorizationResource；expected_permission_version#9: optional int64；request_id#10: string

返回 [AuthorizationDecision](rpc-messages.md#authorizationdecision)：result#1: AuthorizationResult；issuer#2: AuthorizationIssuer；authorization_context_id#3: optional string；scope#4: AuthorizationScope；actor_id#5: string；session_id#6: optional string；audience_owner#7: OwnerEnum；action#8: AuthorizationActionEnum；resource#9: AuthorizationResource；permission_version#10: int64；issued_at#11: Timestamp；expires_at#12: Timestamp；denial_code#13: optional ErrorCodeEnum；accepted_operation_grant_id#14: optional string

接收方写入：

完成证据：session/grant/action/resource/audience/权限版本确切一致

失败/未知：deny不改用管理员；unknown不发后续副作用

### F08.4 workspace → tenant / CloudIdentityAuthorization.IssueAcceptedOperationGrant

按当前入口/阶段与06/13状态机前置执行；不是无条件调用

请求 [AcceptedOperationGrantRequest](rpc-messages.md#acceptedoperationgrantrequest)：authorization_context_id#1: string；owner_commit_evidence#2: OwnerCommitEvidence；allowed_actions#3: repeated AuthorizationActionEnum；renewal_consent_id#4: optional string；subscription_period_id#5: optional string

返回 [AcceptedOperationGrant](rpc-messages.md#acceptedoperationgrant)：id#1: string；scope#2: AuthorizationScope；actor_id#3: string；accepted_operation_owner#4: OwnerEnum；accepted_operation_id#5: string；accepted_action#6: AuthorizationActionEnum；resource_id#7: string；accepted_permission_version#8: int64；allowed_actions#9: repeated AuthorizationActionEnum；mode#10: AcceptedGrantMode；issued_at#11: Timestamp；expires_at#12: optional Timestamp；revoked_at#13: optional Timestamp；obligation_completed_at#14: optional Timestamp

接收方写入：`tenant.accepted_operation_grants`

完成证据：原Owner commit读回，有限actions/resource/period

失败/未知：不能伪造commit字符串或扩大到新Workspace

### F09.1 workspace → tenant / CloudIdentityAuthorization.AuthorizeAction

按当前入口/阶段与06/13状态机前置执行；不是无条件调用

请求 [AuthorizationRequest](rpc-messages.md#authorizationrequest)：scope#1: AuthorizationScope；actor_id#2: string；session_id#3: optional string；authorization_context_id#4: optional string；accepted_operation_grant_id#5: optional string；audience_owner#6: OwnerEnum；action#7: AuthorizationActionEnum；resource#8: AuthorizationResource；expected_permission_version#9: optional int64；request_id#10: string

返回 [AuthorizationDecision](rpc-messages.md#authorizationdecision)：result#1: AuthorizationResult；issuer#2: AuthorizationIssuer；authorization_context_id#3: optional string；scope#4: AuthorizationScope；actor_id#5: string；session_id#6: optional string；audience_owner#7: OwnerEnum；action#8: AuthorizationActionEnum；resource#9: AuthorizationResource；permission_version#10: int64；issued_at#11: Timestamp；expires_at#12: Timestamp；denial_code#13: optional ErrorCodeEnum；accepted_operation_grant_id#14: optional string

接收方写入：

完成证据：session/grant/action/resource/audience/权限版本确切一致

失败/未知：deny不改用管理员；unknown不发后续副作用

### F10.1 workspace → tenant / CloudIdentityAuthorization.AuthorizeAction

按当前入口/阶段与06/13状态机前置执行；不是无条件调用

请求 [AuthorizationRequest](rpc-messages.md#authorizationrequest)：scope#1: AuthorizationScope；actor_id#2: string；session_id#3: optional string；authorization_context_id#4: optional string；accepted_operation_grant_id#5: optional string；audience_owner#6: OwnerEnum；action#7: AuthorizationActionEnum；resource#8: AuthorizationResource；expected_permission_version#9: optional int64；request_id#10: string

返回 [AuthorizationDecision](rpc-messages.md#authorizationdecision)：result#1: AuthorizationResult；issuer#2: AuthorizationIssuer；authorization_context_id#3: optional string；scope#4: AuthorizationScope；actor_id#5: string；session_id#6: optional string；audience_owner#7: OwnerEnum；action#8: AuthorizationActionEnum；resource#9: AuthorizationResource；permission_version#10: int64；issued_at#11: Timestamp；expires_at#12: Timestamp；denial_code#13: optional ErrorCodeEnum；accepted_operation_grant_id#14: optional string

接收方写入：

完成证据：session/grant/action/resource/audience/权限版本确切一致

失败/未知：deny不改用管理员；unknown不发后续副作用

### F11.1 workspace → tenant / CloudIdentityAuthorization.AuthorizeAction

按当前入口/阶段与06/13状态机前置执行；不是无条件调用

请求 [AuthorizationRequest](rpc-messages.md#authorizationrequest)：scope#1: AuthorizationScope；actor_id#2: string；session_id#3: optional string；authorization_context_id#4: optional string；accepted_operation_grant_id#5: optional string；audience_owner#6: OwnerEnum；action#7: AuthorizationActionEnum；resource#8: AuthorizationResource；expected_permission_version#9: optional int64；request_id#10: string

返回 [AuthorizationDecision](rpc-messages.md#authorizationdecision)：result#1: AuthorizationResult；issuer#2: AuthorizationIssuer；authorization_context_id#3: optional string；scope#4: AuthorizationScope；actor_id#5: string；session_id#6: optional string；audience_owner#7: OwnerEnum；action#8: AuthorizationActionEnum；resource#9: AuthorizationResource；permission_version#10: int64；issued_at#11: Timestamp；expires_at#12: Timestamp；denial_code#13: optional ErrorCodeEnum；accepted_operation_grant_id#14: optional string

接收方写入：

完成证据：session/grant/action/resource/audience/权限版本确切一致

失败/未知：deny不改用管理员；unknown不发后续副作用

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

### F13.1 workspace → tenant / CloudIdentityAuthorization.AuthorizeAction

按当前入口/阶段与06/13状态机前置执行；不是无条件调用

请求 [AuthorizationRequest](rpc-messages.md#authorizationrequest)：scope#1: AuthorizationScope；actor_id#2: string；session_id#3: optional string；authorization_context_id#4: optional string；accepted_operation_grant_id#5: optional string；audience_owner#6: OwnerEnum；action#7: AuthorizationActionEnum；resource#8: AuthorizationResource；expected_permission_version#9: optional int64；request_id#10: string

返回 [AuthorizationDecision](rpc-messages.md#authorizationdecision)：result#1: AuthorizationResult；issuer#2: AuthorizationIssuer；authorization_context_id#3: optional string；scope#4: AuthorizationScope；actor_id#5: string；session_id#6: optional string；audience_owner#7: OwnerEnum；action#8: AuthorizationActionEnum；resource#9: AuthorizationResource；permission_version#10: int64；issued_at#11: Timestamp；expires_at#12: Timestamp；denial_code#13: optional ErrorCodeEnum；accepted_operation_grant_id#14: optional string

接收方写入：

完成证据：session/grant/action/resource/audience/权限版本确切一致

失败/未知：deny不改用管理员；unknown不发后续副作用

### F14.1 gateway → tenant / CloudIdentityAuthorization.AuthorizeAction

按当前入口/阶段与06/13状态机前置执行；不是无条件调用

请求 [AuthorizationRequest](rpc-messages.md#authorizationrequest)：scope#1: AuthorizationScope；actor_id#2: string；session_id#3: optional string；authorization_context_id#4: optional string；accepted_operation_grant_id#5: optional string；audience_owner#6: OwnerEnum；action#7: AuthorizationActionEnum；resource#8: AuthorizationResource；expected_permission_version#9: optional int64；request_id#10: string

返回 [AuthorizationDecision](rpc-messages.md#authorizationdecision)：result#1: AuthorizationResult；issuer#2: AuthorizationIssuer；authorization_context_id#3: optional string；scope#4: AuthorizationScope；actor_id#5: string；session_id#6: optional string；audience_owner#7: OwnerEnum；action#8: AuthorizationActionEnum；resource#9: AuthorizationResource；permission_version#10: int64；issued_at#11: Timestamp；expires_at#12: Timestamp；denial_code#13: optional ErrorCodeEnum；accepted_operation_grant_id#14: optional string

接收方写入：

完成证据：session/grant/action/resource/audience/权限版本确切一致

失败/未知：deny不改用管理员；unknown不发后续副作用

### F15.1 tenant → tenant / CloudIdentityAuthorization.AuthorizeAction

按当前入口/阶段与06/13状态机前置执行；不是无条件调用

请求 [AuthorizationRequest](rpc-messages.md#authorizationrequest)：scope#1: AuthorizationScope；actor_id#2: string；session_id#3: optional string；authorization_context_id#4: optional string；accepted_operation_grant_id#5: optional string；audience_owner#6: OwnerEnum；action#7: AuthorizationActionEnum；resource#8: AuthorizationResource；expected_permission_version#9: optional int64；request_id#10: string

返回 [AuthorizationDecision](rpc-messages.md#authorizationdecision)：result#1: AuthorizationResult；issuer#2: AuthorizationIssuer；authorization_context_id#3: optional string；scope#4: AuthorizationScope；actor_id#5: string；session_id#6: optional string；audience_owner#7: OwnerEnum；action#8: AuthorizationActionEnum；resource#9: AuthorizationResource；permission_version#10: int64；issued_at#11: Timestamp；expires_at#12: Timestamp；denial_code#13: optional ErrorCodeEnum；accepted_operation_grant_id#14: optional string

接收方写入：

完成证据：session/grant/action/resource/audience/权限版本确切一致

失败/未知：deny不改用管理员；unknown不发后续副作用

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

### F17.1 receiving_owner → tenant / CloudIdentityAuthorization.AuthorizeAction

按当前入口/阶段与06/13状态机前置执行；不是无条件调用

请求 [AuthorizationRequest](rpc-messages.md#authorizationrequest)：scope#1: AuthorizationScope；actor_id#2: string；session_id#3: optional string；authorization_context_id#4: optional string；accepted_operation_grant_id#5: optional string；audience_owner#6: OwnerEnum；action#7: AuthorizationActionEnum；resource#8: AuthorizationResource；expected_permission_version#9: optional int64；request_id#10: string

返回 [AuthorizationDecision](rpc-messages.md#authorizationdecision)：result#1: AuthorizationResult；issuer#2: AuthorizationIssuer；authorization_context_id#3: optional string；scope#4: AuthorizationScope；actor_id#5: string；session_id#6: optional string；audience_owner#7: OwnerEnum；action#8: AuthorizationActionEnum；resource#9: AuthorizationResource；permission_version#10: int64；issued_at#11: Timestamp；expires_at#12: Timestamp；denial_code#13: optional ErrorCodeEnum；accepted_operation_grant_id#14: optional string

接收方写入：

完成证据：session/grant/action/resource/audience/权限版本确切一致

失败/未知：deny不改用管理员；unknown不发后续副作用

## 5. 事件：谁生产、谁消费、哪些字段

aggregate_type由事件精确版本的x-aggregate-identity.type派生；aggregateId须与其idPayloadField一致。revision由生产者聚合事务内分配；consumer_owner显式选择本域Inbox。字段与实现状态不得混同。


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


### tenant.restored.v1

`tenant` → `capability`, `build`, `gateway`, `ledger`

聚合类型：`tenant`；ID来源：`payload.targetTenantId`（须等于aggregateId）

Envelope字段：`eventId`, `eventType`, `schemaVersion`, `owner`, `tenantId`, `aggregateId`, `aggregateVersion`, `occurredAt`, `requestId`, `payload`, `scope`

| payload字段 | 类型 | 必填 | 约束 |
| --- | --- | --- | --- |
| `targetTenantId` | `string` | 是 | minLength=1 |
| `tenantOperationId` | `string` | 是 | minLength=1 |
| `restoredAt` | `string/date-time` | 是 |  |

恢复身份和保留制品权限，不复活资源


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

