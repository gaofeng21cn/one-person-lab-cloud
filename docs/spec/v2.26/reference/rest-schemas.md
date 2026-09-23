# REST DTO 字段目录（派生参考）

> 来源当前checkout的 `03_api_contract_complete.yaml`。字段来源映射可能是DB直接列、Owner派生或外部读回；同名不表示同权威。


## OpaqueId

类型：`string`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`
不透明string；新旧ID原样保留，不假定UUID。


对象约束：`minLength=1; maxLength=256`

## USDMicros

类型：`string`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`
微美元非负int64，JSON十进制字符串，范围0..9223372036854775807；禁止浮点/前导零。


对象约束：`pattern="^(?:0\|[1-9][0-9]{0,17}\|[1-8][0-9]{18}\|9[0-1][0-9]{17}\|92[0-1][0-9]{16}\|922[0-2][0-9]{15}\|9223[0-2][0-9]{14}\|92233[0-6][0-9]{13}\|922337[0-1][0-9]{12}\|92233720[0-2][0-9]{10}\|922337203[0-5][0-9]{9}\|9223372036[0-7][0-9]{8}\|92233720368[0-4][0-9]{7}\|922337203685[0-3][0-9]{6}\|9223372036854[0-6][0-9]{5}\|92233720368547[0-6][0-9]{4}\|922337203685477[0-4][0-9]{3}\|9223372036854775[0-7][0-9]{2}\|922337203685477580[0-6]\|9223372036854775807)$"`

## NonnegativeInt64

类型：`string`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`
非负int64的JSON十进制字符串，范围0..9223372036854775807；无前导零，不经JavaScript Number转换。


对象约束：`pattern="^(?:0\|[1-9][0-9]{0,17}\|[1-8][0-9]{18}\|9[0-1][0-9]{17}\|92[0-1][0-9]{16}\|922[0-2][0-9]{15}\|9223[0-2][0-9]{14}\|92233[0-6][0-9]{13}\|922337[0-1][0-9]{12}\|92233720[0-2][0-9]{10}\|922337203[0-5][0-9]{9}\|9223372036[0-7][0-9]{8}\|92233720368[0-4][0-9]{7}\|922337203685[0-3][0-9]{6}\|9223372036854[0-6][0-9]{5}\|92233720368547[0-6][0-9]{4}\|922337203685477[0-4][0-9]{3}\|9223372036854775[0-7][0-9]{2}\|922337203685477580[0-6]\|9223372036854775807)$"`

## Digest

类型：`string`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`



对象约束：`pattern="^sha256:[0-9a-f]{64}$"`

## ErrorCode

类型：`string`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`



对象约束：`enum=["VALIDATION_FAILED","UNAUTHENTICATED","FORBIDDEN","NOT_FOUND","CSRF_INVALID","ORIGIN_REJECTED","IDEMPOTENCY_REQUIRED","IDEMPOTENCY_CONFLICT","VERSION_CONFLICT","LAST_OWNER","INVITATION_INVALID","TENANT_INACTIVE","TENANT_RESTORE_EXPIRED","GATEWAY_UNAVAILABLE","OWNER_CAPABILITY_UNAVAILABLE","WALLET_BINDING_REQUIRED","INSUFFICIENT_BALANCE","QUOTE_EXPIRED","QUOTE_MISMATCH","POLICY_UNCONFIGURED","CAPACITY_UNAVAILABLE","PROVIDER_CAPABILITY_UNSUPPORTED","RUNTIME_REVOKED","WEBUI_INCOMPATIBLE","MODEL_NOT_ALLOWED","PACKAGE_ARCHIVED","UPLOAD_EXPIRED","UPLOAD_PART_MISMATCH","UPLOAD_CHECKSUM_MISMATCH","PACKAGE_INVALID","BUILD_INPUT_REJECTED","BUILD_FAILED","ARTIFACT_REFERENCED","ARTIFACT_UNAVAILABLE","INCOMPATIBLE_VERSION","DATA_MIGRATION_REQUIRED","ROLLBACK_UNSAFE","WORKSPACE_NOT_READY","WORKSPACE_EXPIRED","OPERATION_IN_PROGRESS","EXTERNAL_OUTCOME_UNKNOWN","RESOURCE_DELETE_UNCONFIRMED","REFUND_PENDING","KEY_REVEAL_FORBIDDEN","APP_ACCESS_UNAVAILABLE","RATE_LIMITED","DEPENDENCY_UNAVAILABLE","INSTANCE_AUTHORIZATION_REQUIRED","INTERNAL_ERROR","PUBLISHER_NAMESPACE_MISMATCH","PUBLISHER_CONTRACT_INVALID","REFERENCE_CLAIM_INVALID","AUTHORIZATION_CONTEXT_EXPIRED","AUTHORIZATION_REVOKED","AUTHORIZATION_AUDIENCE_MISMATCH","ROUTE_GENERATION_CONFLICT","STALE_EXECUTION_EPOCH","TENANT_NOT_SUSPENDED","RENEWAL_PERIOD_ELAPSED","PLAN_CHANGE_EXISTS","PLAN_CHANGE_NOT_CANCELLABLE","PLAN_CHANGE_QUOTE_STALE","PLAN_TRANSITION_NOT_SUPPORTED","PLAN_TRANSITION_MIXED","PLAN_CHANGE_NO_OP","STORAGE_SHRINK_UNSUPPORTED","SUBSCRIPTION_VERSION_CONFLICT","PLAN_CHANGE_PAYMENT_REQUIRED","PLAN_CHANGE_PERIOD_ELAPSED","REFUND_ORIGINAL_CHARGE_CONFLICT","FUTURE_PERIOD_COMMITTED","SCHEDULED_PLAN_APPLICATION_CONFLICT"]`

## Owner

类型：`string`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`



对象约束：`enum=["tenant","capability","build","workspace","runtime_control","fabric","gateway","resource_catalog","ledger"]`

## TenantRole

类型：`string`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`



对象约束：`enum=["owner","admin","member"]`

## FieldError

类型：`object`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`


| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `field` | `string` | 是 | "未在本schema声明" |  |
| `code` | [ErrorCode](rest-schemas.md#errorcode) | 是 | "未在本schema声明" |  |
| `message` | `string` | 是 | "未在本schema声明" |  |

对象约束：`additionalProperties=false`

## Error

类型：`object`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`


| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `code` | [ErrorCode](rest-schemas.md#errorcode) | 是 | "未在本schema声明" |  |
| `message` | `string` | 是 | "未在本schema声明" |  |
| `requestId` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "未在本schema声明" |  |
| `fieldErrors` | `array<FieldError>` | 否 | "未在本schema声明" |  |

对象约束：`additionalProperties=false`

## Operation

类型：`object`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`
各域独占写自己的operations；/operations/{owner}/{operationId}路由读回，无BFF业务注册表。202只接受；kind/stage取06表中固定值。 succeeded必须stage=succeeded且observationResult=confirmed；failed/needs_attention必须errorCode；awaiting_confirmation必须unknown。 pollAfterSeconds由Owner依据其worker/依赖下一检查时刻给出1..300秒；浏览器严格按秒轮询，不自选指数退避。succeeded/failed/cancelled终态省略字段且停止轮询。

| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `operationId` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "未在本schema声明" |  |
| `owner` | [OperationOwner](rest-schemas.md#operationowner) | 是 | "未在本schema声明" |  |
| `kind` | [OperationKind](rest-schemas.md#operationkind) | 是 | "未在本schema声明" |  |
| `resourceId` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "未在本schema声明" |  |
| `status` | `string` | 是 | "未在本schema声明" | enum=["accepted","running","awaiting_confirmation","succeeded","failed","needs_attention","cancelled"] |
| `stage` | [OperationStage](rest-schemas.md#operationstage) | 是 | "未在本schema声明" |  |
| `observationResult` | `string` | 否 | "未在本schema声明" | enum=["confirmed","rejected","unknown"] |
| `errorCode` | [ErrorCode](rest-schemas.md#errorcode) | 否 | "未在本schema声明" |  |
| `requestId` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "未在本schema声明" |  |
| `createdAt` | `string/date-time` | 是 | "未在本schema声明" |  |
| `updatedAt` | `string/date-time` | 是 | "未在本schema声明" |  |
| `pollAfterSeconds` | `integer` | 否 | "未在本schema声明" | minimum=1; maximum=300 |

对象约束：`additionalProperties=false; allOf=[{"oneOf":[{"properties":{"kind":{"enum":["complete_upload"]},"stage":{"enum":["upload_verification","receipt","succeeded"]}}},{"properties":{"kind":{"enum":["build"]},"stage":{"enum":["queued","validating","building","pushing","registering","succeeded"]}}},{"properties":{"kind":{"enum":["delete_capability_version"]},"stage":{"enum":["reference_check","catalog_tombstone","receipt","succeeded"]}}},{"properties":{"kind":{"enum":["create_workspace"]},"stage":{"enum":["admission","debit","key","compute","storage","attachment","runtime","activation","receipt","succeeded"]}}},{"properties":{"kind":{"enum":["update_models"]},"stage":{"enum":["admission","key","configuration","reload","verification","receipt","succeeded"]}}},{"properties":{"kind":{"enum":["update_workspace"]},"stage":{"enum":["admission","reference_check","runtime","verification","activation","retirement","receipt","succeeded"]}}},{"properties":{"kind":{"enum":["rollback_workspace"]},"stage":{"enum":["admission","compatibility","runtime","verification","activation","retirement","receipt","succeeded"]}}},{"properties":{"kind":{"enum":["resize_workspace"]},"stage":{"enum":["admission","quote_binding","plan_change_commit","schedule_commit","supplement_payment","resource_preflight","compute","storage","attachment","runtime","verification","plan_commit","receipt","succeeded"]}}},{"properties":{"kind":{"enum":["renew_workspace"]},"stage":{"enum":["admission","debit","provider_renewal","period_update","runtime","verification","receipt","succeeded"]}}},{"properties":{"kind":{"enum":["delete_workspace"]},"stage":{"enum":["admission","runtime_deletion","secret_unbinding","attachment_deletion","storage_deletion","compute_deletion","absence_verification","receipt","refund","succeeded"]}}},{"properties":{"kind":{"enum":["create_tenant"]},"stage":{"enum":["identity_verification","wallet_binding","membership","receipt","succeeded"]}}},{"properties":{"kind":{"enum":["bind_tenant_wallet"]},"stage":{"enum":["obligation_check","wallet_binding","receipt","succeeded"]}}},{"properties":{"kind":{"enum":["suspend_tenant"]},"stage":{"enum":["access_revocation","workspace_suspension","receipt","succeeded"]}}},{"properties":{"kind":{"enum":["delete_tenant"]},"stage":{"enum":["access_revocation","workspace_deletion","asset_custody","receipt","succeeded"]}}},{"properties":{"kind":{"enum":["restore_tenant"]},"stage":{"enum":["restore_window_check","membership","asset_custody","receipt","succeeded"]}}},{"properties":{"kind":{"enum":["revoke_key"]},"stage":{"enum":["key_revocation","verification","receipt","succeeded"]}}},{"properties":{"kind":{"enum":["reconcile"]},"stage":{"enum":["readback","receipt","succeeded"]}}},{"properties":{"kind":{"enum":["adopt_workspace"]},"stage":{"enum":["admission","reference_check","key","runtime","verification","activation","receipt","succeeded"]}}},{"properties":{"kind":{"enum":["resource_provision"]},"stage":{"enum":["admission","compute","storage","attachment","verification","receipt","succeeded"]}}},{"properties":{"kind":{"enum":["resource_resize"]},"stage":{"enum":["admission","compute","storage","attachment","verification","receipt","succeeded"]}}},{"properties":{"kind":{"enum":["resource_renew"]},"stage":{"enum":["admission","provider_renewal","verification","receipt","succeeded"]}}},{"properties":{"kind":{"enum":["resource_suspend"]},"stage":{"enum":["admission","provider_suspend","verification","receipt","succeeded"]}}},{"properties":{"kind":{"enum":["resource_resume"]},"stage":{"enum":["admission","provider_resume","verification","receipt","succeeded"]}}},{"properties":{"kind":{"enum":["resource_delete"]},"stage":{"enum":["admission","attachment_deletion","storage_deletion","compute_deletion","absence_verification","receipt","succeeded"]}}},{"properties":{"kind":{"enum":["runtime_deploy"]},"stage":{"enum":["runtime","verification","receipt","succeeded"]}}},{"properties":{"kind":{"enum":["runtime_reload"]},"stage":{"enum":["reload","verification","receipt","succeeded"]}}},{"properties":{"kind":{"enum":["runtime_retire"]},"stage":{"enum":["retirement","verification","receipt","succeeded"]}}},{"properties":{"kind":{"enum":["reenable_tenant"]},"stage":{"enum":["tenant_state_check","access_enablement","workspace_resumption","receipt","succeeded"]}}},{"properties":{"kind":{"enum":["migration"]},"stage":{"enum":["source_verification","write_barrier","snapshot_import","reconciliation","owner_switch","receipt","succeeded"]}}},{"properties":{"kind":{"enum":["update_renewal_settings"]},"stage":{"enum":["consent_validation","consent_commit","grant_revocation","receipt","succeeded"]}}},{"properties":{"kind":{"enum":["apply_scheduled_plan_change"]},"stage":{"enum":["period_boundary","payment_authorization","target_period_payment","resource_preflight","compute","storage","attachment","runtime","verification","plan_commit","receipt","succeeded"]}}},{"properties":{"kind":{"enum":["cancel_plan_change"]},"stage":{"enum":["cancellation_guard","schedule_cancel","receipt","succeeded"]}}},{"properties":{"kind":{"enum":["compensate_plan_change"]},"stage":{"enum":["original_action_readback","failure_fence","actual_resource_evidence","refund_original_charge","receipt","succeeded"]}}},{"properties":{"kind":{"enum":["refund_supplement_on_delete"]},"stage":{"enum":["deletion_evidence","coverage_calculation","refund_original_charge","receipt","succeeded"]}}}]},{"oneOf":[{"properties":{"status":{"enum":["succeeded"]},"stage":{"enum":["succeeded"]},"observationResult":{"enum":["confirmed"]}},"required":["observationResult"]},{"properties":{"status":{"enum":["accepted","running","awaiting_confirmation","failed","needs_attention","cancelled"]}},"not":{"properties":{"stage":{"enum":["succeeded"]}}}}]},{"anyOf":[{"properties":{"status":{"enum":["failed","needs_attention"]}},"required":["errorCode"]},{"properties":{"status":{"enum":["accepted","running","awaiting_confirmation","succeeded","cancelled"]}}}]},{"anyOf":[{"properties":{"status":{"enum":["awaiting_confirmation"]},"observationResult":{"enum":["unknown"]}},"required":["observationResult"]},{"properties":{"status":{"enum":["accepted","running","succeeded","failed","needs_attention","cancelled"]}}}]},{"oneOf":[{"properties":{"status":{"enum":["succeeded","failed","cancelled"]}},"not":{"required":["pollAfterSeconds"]}},{"properties":{"status":{"enum":["accepted","running","awaiting_confirmation","needs_attention"]}},"required":["pollAfterSeconds"]}]}]`

x-stage-values：{"complete_upload": ["upload_verification", "receipt", "succeeded"], "build": ["queued", "validating", "building", "pushing", "registering", "succeeded"], "delete_capability_version": ["reference_check", "catalog_tombstone", "receipt", "succeeded"], "create_workspace": ["admission", "debit", "key", "compute", "storage", "attachment", "runtime", "activation", "receipt", "succeeded"], "update_models": ["admission", "key", "configuration", "reload", "verification", "receipt", "succeeded"], "update_workspace": ["admission", "reference_check", "runtime", "verification", "activation", "retirement", "receipt", "succeeded"], "rollback_workspace": ["admission", "compatibility", "runtime", "verification", "activation", "retirement", "receipt", "succeeded"], "resize_workspace": ["admission", "quote_binding", "plan_change_commit", "schedule_commit", "supplement_payment", "resource_preflight", "compute", "storage", "attachment", "runtime", "verification", "plan_commit", "receipt", "succeeded"], "renew_workspace": ["admission", "debit", "provider_renewal", "period_update", "runtime", "verification", "receipt", "succeeded"], "delete_workspace": ["admission", "runtime_deletion", "secret_unbinding", "attachment_deletion", "storage_deletion", "compute_deletion", "absence_verification", "receipt", "refund", "succeeded"], "create_tenant": ["identity_verification", "wallet_binding", "membership", "receipt", "succeeded"], "bind_tenant_wallet": ["obligation_check", "wallet_binding", "receipt", "succeeded"], "suspend_tenant": ["access_revocation", "workspace_suspension", "receipt", "succeeded"], "delete_tenant": ["access_revocation", "workspace_deletion", "asset_custody", "receipt", "succeeded"], "restore_tenant": ["restore_window_check", "membership", "asset_custody", "receipt", "succeeded"], "revoke_key": ["key_revocation", "verification", "receipt", "succeeded"], "reconcile": ["readback", "receipt", "succeeded"], "adopt_workspace": ["admission", "reference_check", "key", "runtime", "verification", "activation", "receipt", "succeeded"], "resource_provision": ["admission", "compute", "storage", "attachment", "verification", "receipt", "succeeded"], "resource_resize": ["admission", "compute", "storage", "attachment", "verification", "receipt", "succeeded"], "resource_renew": ["admission", "provider_renewal", "verification", "receipt", "succeeded"], "resource_suspend": ["admission", "provider_suspend", "verification", "receipt", "succeeded"], "resource_resume": ["admission", "provider_resume", "verification", "receipt", "succeeded"], "resource_delete": ["admission", "attachment_deletion", "storage_deletion", "compute_deletion", "absence_verification", "receipt", "succeeded"], "runtime_deploy": ["runtime", "verification", "receipt", "succeeded"], "runtime_reload": ["reload", "verification", "receipt", "succeeded"], "runtime_retire": ["retirement", "verification", "receipt", "succeeded"], "reenable_tenant": ["tenant_state_check", "access_enablement", "workspace_resumption", "receipt", "succeeded"], "migration": ["source_verification", "write_barrier", "snapshot_import", "reconciliation", "owner_switch", "receipt", "succeeded"], "update_renewal_settings": ["consent_validation", "consent_commit", "grant_revocation", "receipt", "succeeded"], "apply_scheduled_plan_change": ["period_boundary", "payment_authorization", "target_period_payment", "resource_preflight", "compute", "storage", "attachment", "runtime", "verification", "plan_commit", "receipt", "succeeded"], "cancel_plan_change": ["cancellation_guard", "schedule_cancel", "receipt", "succeeded"], "compensate_plan_change": ["original_action_readback", "failure_fence", "actual_resource_evidence", "refund_original_charge", "receipt", "succeeded"], "refund_supplement_on_delete": ["deletion_evidence", "coverage_calculation", "refund_original_charge", "receipt", "succeeded"]}

## LoginContext

类型：`object`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`
匿名同源CSRF上下文；no-store。

| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `csrfToken` | `string` | 是 | "未在本schema声明" |  |
| `expiresAt` | `string/date-time` | 是 | "未在本schema声明" |  |

对象约束：`additionalProperties=false`

## LoginRequest

类型：`object`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`
TLS转发Gateway现有认证；密码不持久化，不进日志/事件。无公开注册、不假设OIDC能力。 幂等存储只保留请求去敏指纹/会话身份，绝不持久化password或完整request/response；session-cookie不进事件。

| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `username` | `string` | 是 | "未在本schema声明" | minLength=1; maxLength=256 |
| `password` | `string/password` | 是 | "未在本schema声明" |  |

对象约束：`additionalProperties=false`

## Session

类型：`object`；Owner：`tenant`；表：`—`
尚未接受Tenant邀请时tenant/role缺省，只能退出或接受绑定本人Gateway subject的邀请。HttpOnly Secure SameSite=Lax cookie。

| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `actorId` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "未在本schema声明" |  |
| `displayName` | `string` | 是 | "未在本schema声明" |  |
| `tenantId` | [OpaqueId](rest-schemas.md#opaqueid) | 否 | "未在本schema声明" |  |
| `tenantName` | `string` | 否 | "未在本schema声明" |  |
| `role` | [TenantRole](rest-schemas.md#tenantrole) | 否 | "未在本schema声明" |  |
| `permissions` | `array<AuthorizationAction>` | 是 | "未在本schema声明" |  |
| `csrfToken` | `string` | 是 | "未在本schema声明" |  |
| `expiresAt` | `string/date-time` | 是 | "未在本schema声明" |  |

对象约束：`additionalProperties=false`

## Tenant

类型：`object`；Owner：`tenant`；表：`tenant.tenants`


| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `id` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "tenant.tenants.id" |  |
| `name` | `string` | 是 | "tenant.tenants.name" |  |
| `status` | `string` | 是 | "tenant.tenants.status" | enum=["active","suspended","deleting","deleted"] |
| `billingSub2apiUserId` | [OpaqueId](rest-schemas.md#opaqueid) | 否 | {"kind": "derived", "sources": ["gateway.tenant_wallet_bindings.billing_sub2api_user_id"], "rule": "Gateway当前活动wallet binding；跨owner typed readback"} |  |
| `restoreUntil` | `string/date-time` | 否 | "tenant.tenants.restore_until" |  |
| `assetCustodyStatus` | `string` | 是 | {"kind": "derived", "sources": ["tenant.tenants.status", "tenant.operations.result"], "rule": "Tenant删除/恢复Operation及保留资产Owner读回，无counter表"} | enum=["tenant_owned","restricted","retained"] |
| `createdAt` | `string/date-time` | 是 | "tenant.tenants.created_at" |  |
| `updatedAt` | `string/date-time` | 是 | "tenant.tenants.updated_at" |  |

对象约束：`additionalProperties=false`

## CreateTenantRequest

类型：`object`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`
platform_admin创建Tenant；Gateway确认身份/钱包委托存在，不创建外部身份密码。

| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `name` | `string` | 是 | "未在本schema声明" | minLength=1; maxLength=256 |
| `billingSub2apiUserId` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "未在本schema声明" |  |
| `ownerGatewaySubjectId` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "未在本schema声明" |  |

对象约束：`additionalProperties=false`

## BindTenantWalletRequest

类型：`object`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`
有未结义务禁止直接换绑定；显式结算迁移需原付款引用和receipt，不覆盖旧义务。

| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `billingSub2apiUserId` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "未在本schema声明" |  |
| `expectedBindingVersion` | [NonnegativeInt64](rest-schemas.md#nonnegativeint64) | 是 | "未在本schema声明" |  |

对象约束：`additionalProperties=false`

## Member

类型：`object`；Owner：`tenant`；表：`tenant.tenant_members`


| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `id` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "tenant.tenant_members.id" |  |
| `actorId` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "tenant.tenant_members.actor_id" |  |
| `displayName` | `string` | 是 | {"kind": "external", "sources": ["gateway.identity_mappings.sub2api_user_id"], "rule": "Gateway按当前identity授权读取显示名"} |  |
| `role` | [TenantRole](rest-schemas.md#tenantrole) | 是 | "tenant.tenant_members.role" |  |
| `status` | `string` | 是 | {"kind": "derived", "sources": ["tenant.tenant_members.revoked_at"], "rule": "NULL=>active，否则revoked"} | enum=["active","revoked"] |
| `createdAt` | `string/date-time` | 是 | "tenant.tenant_members.created_at" |  |

对象约束：`additionalProperties=false`

## Invitation

类型：`object`；Owner：`tenant`；表：`tenant.invitations`


| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `id` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "tenant.invitations.id" |  |
| `inviteeGatewaySubjectId` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "tenant.invitations.invitee_gateway_subject_id" |  |
| `role` | `string` | 是 | "tenant.invitations.role" | enum=["admin","member"] |
| `status` | `string` | 是 | {"kind": "derived", "sources": ["tenant.invitations.accepted_at", "tenant.invitations.revoked_at", "tenant.invitations.expires_at"], "rule": "accepted_at优先accepted；revoked_at=>revoked；未完成且已过期=>expired；否则pending"} | enum=["pending","accepted","revoked","expired"] |
| `expiresAt` | `string/date-time` | 是 | "tenant.invitations.expires_at" |  |
| `createdAt` | `string/date-time` | 是 | "tenant.invitations.created_at" |  |

对象约束：`additionalProperties=false`

## InviteMemberRequest

类型：`object`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`


| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `inviteeGatewaySubjectId` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "未在本schema声明" |  |
| `role` | `string` | 是 | "未在本schema声明" | enum=["admin","member"] |

对象约束：`additionalProperties=false`

## UpdateMemberRoleRequest

类型：`object`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`
仅owner；最后owner不能降级或移除。

| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `role` | [TenantRole](rest-schemas.md#tenantrole) | 是 | "未在本schema声明" |  |

对象约束：`additionalProperties=false`

## TenantActionRequest

类型：`object`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`


| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `reason` | `string` | 是 | "未在本schema声明" | minLength=1; maxLength=1024 |

对象约束：`additionalProperties=false`

## DeleteTenantRequest

类型：`object`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`
名称精确确认；停用是reenable，删除恢复是restore。restoreUntil只在确认deleted时设为deletedAt+15天，删除处理中无恢复倒计时；先收敛未决删除子操作。

| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `confirmationName` | `string` | 是 | "未在本schema声明" | minLength=1; maxLength=256 |
| `reason` | `string` | 是 | "未在本schema声明" | minLength=1; maxLength=1024 |

对象约束：`additionalProperties=false`

## AssetCustody

类型：`object`；Owner：`tenant`；表：`tenant.operations`
不公开私有制品；恢复只恢复身份与保留资产权限，不复活已销毁数据、不重购。

| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `tenantId` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | {"kind": "derived", "sources": ["tenant.operations.result", "tenant.tenants.status", "tenant.tenants.restore_until", "capability.packages.id", "build.build_jobs.id"], "rule": "按Owner授权readback精确计数/恢复窗口与Workspace删除Operation引用；无持久counter"} |  |
| `status` | `string` | 是 | {"kind": "derived", "sources": ["tenant.operations.result", "tenant.tenants.status", "tenant.tenants.restore_until", "capability.packages.id", "build.build_jobs.id"], "rule": "按Owner授权readback精确计数/恢复窗口与Workspace删除Operation引用；无持久counter"} | enum=["restricted","retained"] |
| `packageCount` | [NonnegativeInt64](rest-schemas.md#nonnegativeint64) | 是 | {"kind": "derived", "sources": ["tenant.operations.result", "tenant.tenants.status", "tenant.tenants.restore_until", "capability.packages.id", "build.build_jobs.id"], "rule": "按Owner授权readback精确计数/恢复窗口与Workspace删除Operation引用；无持久counter"} |  |
| `buildCount` | [NonnegativeInt64](rest-schemas.md#nonnegativeint64) | 是 | {"kind": "derived", "sources": ["tenant.operations.result", "tenant.tenants.status", "tenant.tenants.restore_until", "capability.packages.id", "build.build_jobs.id"], "rule": "按Owner授权readback精确计数/恢复窗口与Workspace删除Operation引用；无持久counter"} |  |
| `restoreUntil` | `string/date-time` | 否 | {"kind": "derived", "sources": ["tenant.operations.result", "tenant.tenants.status", "tenant.tenants.restore_until", "capability.packages.id", "build.build_jobs.id"], "rule": "按Owner授权readback精确计数/恢复窗口与Workspace删除Operation引用；无持久counter"} |  |
| `workspaceDeletionOperationIds` | `array<OpaqueId>` | 是 | {"kind": "derived", "sources": ["tenant.operations.result", "tenant.tenants.status", "tenant.tenants.restore_until", "capability.packages.id", "build.build_jobs.id"], "rule": "按Owner授权readback精确计数/恢复窗口与Workspace删除Operation引用；无持久counter"} |  |

对象约束：`additionalProperties=false`

## Namespace

类型：`object`；Owner：`capability`；表：`capability.namespaces`


| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `id` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "capability.namespaces.id" |  |
| `name` | `string` | 是 | "capability.namespaces.name" |  |
| `isDefault` | `boolean` | 是 | {"kind": "derived", "sources": ["capability.namespaces.kind"], "rule": "kind=tenant_default"} |  |
| `status` | `string` | 是 | "capability.namespaces.status" | enum=["active","archived"] |
| `createdAt` | `string/date-time` | 是 | "capability.namespaces.created_at" |  |

对象约束：`additionalProperties=false`

## NamespaceWriteRequest

类型：`object`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`


| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `name` | `string` | 是 | "未在本schema声明" | minLength=1; maxLength=256 |

对象约束：`additionalProperties=false`

## Package

类型：`object`；Owner：`capability`；表：`capability.packages`


| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `id` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "capability.packages.id" |  |
| `namespaceId` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "capability.packages.namespace_id" |  |
| `name` | `string` | 是 | "capability.packages.name" |  |
| `description` | `string` | 是 | "capability.packages.description" |  |
| `visibility` | `string` | 是 | "capability.packages.visibility" | enum=["private","official"] |
| `status` | `string` | 是 | "capability.packages.status" | enum=["active","archived"] |
| `latestReadyVersionId` | [OpaqueId](rest-schemas.md#opaqueid) | 否 | {"kind": "derived", "sources": ["capability.capability_versions.package_id", "capability.capability_versions.status", "capability.capability_versions.created_at"], "rule": "同Package ready版本按created_at DESC,id DESC首项；无版本不返回"} |  |
| `createdAt` | `string/date-time` | 是 | "capability.packages.created_at" |  |
| `updatedAt` | `string/date-time` | 是 | "capability.packages.updated_at" |  |

对象约束：`additionalProperties=false`

## CreatePackageRequest

类型：`object`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`


| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `namespaceId` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "未在本schema声明" |  |
| `name` | `string` | 是 | "未在本schema声明" | minLength=1; maxLength=256 |
| `description` | `string` | 是 | "未在本schema声明" | maxLength=4000 |

对象约束：`additionalProperties=false`

## UpdatePackageRequest

类型：`object`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`
完整替换可编辑元数据，visibility仅管理员发布命令可改。

| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `namespaceId` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "未在本schema声明" |  |
| `name` | `string` | 是 | "未在本schema声明" | minLength=1; maxLength=256 |
| `description` | `string` | 是 | "未在本schema声明" | maxLength=4000 |

对象约束：`additionalProperties=false`

## PublishPackageRequest

类型：`object`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`
platform_admin准入官方Package；普通客户不发布公开Marketplace。

| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `admissionReceiptId` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "未在本schema声明" |  |

对象约束：`additionalProperties=false`

## PackageVersion

类型：`object`；Owner：`capability`；表：`capability.package_versions`
上传申报值在完成时实际校验后冻结。一个PackageVersion可配不同WebUI构建多个Build；不绑定Build状态/Runtime/WebUI。

| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `id` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "capability.package_versions.id" |  |
| `packageId` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "capability.package_versions.package_id" |  |
| `versionLabel` | `string` | 是 | "capability.package_versions.version_label" |  |
| `status` | `string` | 是 | "capability.package_versions.status" | enum=["upload_pending","uploaded","rejected"] |
| `sha256` | [Digest](rest-schemas.md#digest) | 是 | "capability.package_versions.sha256" |  |
| `sizeBytes` | [NonnegativeInt64](rest-schemas.md#nonnegativeint64) | 是 | "capability.package_versions.size_bytes" |  |
| `createdAt` | `string/date-time` | 是 | "capability.package_versions.created_at" |  |

对象约束：`additionalProperties=false`

## CreateUploadRequest

类型：`object`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`


| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `versionLabel` | `string` | 是 | "未在本schema声明" | minLength=1; maxLength=256 |
| `fileName` | `string` | 是 | "未在本schema声明" | minLength=1; maxLength=256 |
| `sizeBytes` | [NonnegativeInt64](rest-schemas.md#nonnegativeint64) | 是 | "未在本schema声明" |  |
| `sha256` | [Digest](rest-schemas.md#digest) | 是 | "未在本schema声明" |  |

对象约束：`additionalProperties=false`

## UploadPart

类型：`object`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`


| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `partNumber` | `integer/int32` | 是 | "未在本schema声明" | minimum=1 |
| `etag` | `string` | 是 | "未在本schema声明" |  |
| `sizeBytes` | [NonnegativeInt64](rest-schemas.md#nonnegativeint64) | 是 | "未在本schema声明" |  |
| `sha256` | [Digest](rest-schemas.md#digest) | 是 | "未在本schema声明" |  |

对象约束：`additionalProperties=false`

## UploadSession

类型：`object`；Owner：`capability`；表：`capability.upload_sessions`
Storage已确认parts读回，可断点恢复；页面不猜测已传字节。业务请求仅BFF，原始字节仅传Capability签署Storage URL。

| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `id` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "capability.upload_sessions.id" |  |
| `packageVersionId` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "capability.upload_sessions.package_version_id" |  |
| `status` | `string` | 是 | "capability.upload_sessions.status" | enum=["uploading","completed","expired"] |
| `sizeBytes` | [NonnegativeInt64](rest-schemas.md#nonnegativeint64) | 是 | "capability.package_versions.size_bytes" |  |
| `sha256` | [Digest](rest-schemas.md#digest) | 是 | "capability.package_versions.sha256" |  |
| `partSizeBytes` | [NonnegativeInt64](rest-schemas.md#nonnegativeint64) | 是 | "capability.upload_sessions.part_size_bytes" |  |
| `completedParts` | `array<UploadPart>` | 是 | {"kind": "derived", "sources": ["capability.upload_chunks.part_number", "capability.upload_chunks.etag", "capability.upload_chunks.sha256", "capability.upload_chunks.size_bytes", "capability.upload_chunks.observation_result"], "rule": "本session confirmed分片按part_number排列"} |  |
| `expiresAt` | `string/date-time` | 是 | "capability.upload_sessions.expires_at" |  |

对象约束：`additionalProperties=false`

## CreateUploadPartRequest

类型：`object`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`


| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `partNumber` | `integer/int32` | 是 | "未在本schema声明" | minimum=1 |
| `sizeBytes` | [NonnegativeInt64](rest-schemas.md#nonnegativeint64) | 是 | "未在本schema声明" |  |
| `sha256` | [Digest](rest-schemas.md#digest) | 是 | "未在本schema声明" |  |

对象约束：`additionalProperties=false`

## UploadPartAuthorization

类型：`object`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`
只允许PUT application/octet-stream到当前part/大小/校验和绑定的临时URL；此处无通用header map。签名URL仅内存使用不写日志，不用于业务调用。 签名URL不写持久幂等response_body；幂等重放返回同part身份并新签署有限时授权，不改变已上传字节。

| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `uploadId` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "未在本schema声明" |  |
| `partNumber` | `integer/int32` | 是 | "未在本schema声明" | minimum=1 |
| `method` | `string` | 是 | "未在本schema声明" | enum=["PUT"] |
| `url` | `string/uri` | 是 | "未在本schema声明" |  |
| `contentType` | `string` | 是 | "未在本schema声明" |  |
| `requiredChecksumHeaderName` | `string` | 是 | "未在本schema声明" |  |
| `requiredChecksumHeaderValue` | `string` | 是 | "未在本schema声明" |  |
| `expiresAt` | `string/date-time` | 是 | "未在本schema声明" |  |

对象约束：`additionalProperties=false`

## CompleteUploadRequest

类型：`object`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`
按partNumber升序且唯一，Owner逐片+完整对象实际校验；客户端etag不视为完整性证据。

| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `parts` | `array<UploadPart>` | 是 | "未在本schema声明" |  |

对象约束：`additionalProperties=false`

## ModelRequirement

类型：`object`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`


| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `slot` | `string` | 是 | "未在本schema声明" |  |
| `required` | `boolean` | 是 | "未在本schema声明" |  |
| `capability` | `string` | 是 | "未在本schema声明" | enum=["text","vision","embedding","audio"] |
| `allowedModelIds` | `array<OpaqueId>` | 是 | "未在本schema声明" |  |

对象约束：`additionalProperties=false`

## DataCompatibility

类型：`object`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`
publisher批准兼容声明；不从semver启发式猜测；数据迁移不满足则拒绝。

| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `dataSchemaVersion` | `string` | 是 | "未在本schema声明" |  |
| `compatibleFromVersions` | `array<string>` | 是 | "未在本schema声明" |  |
| `rollbackSafe` | `boolean` | 是 | "未在本schema声明" |  |
| `migrationRequired` | `boolean` | 是 | "未在本schema声明" |  |
| `migrationReceiptId` | [OpaqueId](rest-schemas.md#opaqueid) | 否 | "未在本schema声明" |  |

对象约束：`additionalProperties=false`

## CapabilityVersion

类型：`object`；Owner：`capability`；表：`capability.capability_versions`
仅Build产物readback确认后创建ready。referenceCount来自Capability reference_claims，不跨Owner JOIN。删除保留历史记录。 legacy_application仅迁移现有exact revision，保留artifactDigest和data契约，不重建镜像，不伪造Package/Build；build分支必须所有构建引用。

| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `id` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "capability.capability_versions.id" |  |
| `packageId` | [OpaqueId](rest-schemas.md#opaqueid) | 否 | "capability.capability_versions.package_id" |  |
| `packageVersionId` | [OpaqueId](rest-schemas.md#opaqueid) | 否 | "capability.capability_versions.package_version_id" |  |
| `buildJobId` | [OpaqueId](rest-schemas.md#opaqueid) | 否 | "capability.capability_versions.build_job_id" |  |
| `versionLabel` | `string` | 是 | "capability.capability_versions.version_label" |  |
| `runtimeVersionId` | [OpaqueId](rest-schemas.md#opaqueid) | 否 | "capability.capability_versions.runtime_version_id" |  |
| `webuiVersionId` | [OpaqueId](rest-schemas.md#opaqueid) | 否 | "capability.capability_versions.webui_version_id" |  |
| `artifactDigest` | [Digest](rest-schemas.md#digest) | 是 | "capability.capability_versions.artifact_digest" |  |
| `status` | `string` | 是 | "capability.capability_versions.status" | enum=["ready","deprecated","deleting","deleted"] |
| `modelRequirements` | `array<ModelRequirement>` | 是 | "capability.capability_versions.model_requirements" |  |
| `dataCompatibility` | [DataCompatibility](rest-schemas.md#datacompatibility) | 是 | "capability.capability_versions.data_compatibility" |  |
| `referenceCount` | [NonnegativeInt64](rest-schemas.md#nonnegativeint64) | 是 | {"kind": "derived", "sources": ["capability.reference_claims.capability_version_id", "capability.reference_claims.released_at"], "rule": "本版本released_at IS NULL活跃claim精确COUNT；无持久计数"} |  |
| `createdAt` | `string/date-time` | 是 | "capability.capability_versions.created_at" |  |
| `provenance` | `string` | 是 | "capability.capability_versions.provenance" | enum=["build","legacy_application"] |
| `legacyApplicationRevisionId` | [OpaqueId](rest-schemas.md#opaqueid) | 否 | "capability.capability_versions.legacy_application_revision_id" |  |
| `artifact` | [ArtifactReference](rest-schemas.md#artifactreference) | 是 | {"kind": "derived", "sources": ["capability.capability_versions.artifact_repository", "capability.capability_versions.artifact_digest", "capability.capability_versions.deployment_descriptor"], "rule": "完整产物identity与descriptor.artifact相等"} |  |
| `deploymentDescriptor` | [DeploymentDescriptor](rest-schemas.md#deploymentdescriptor) | 是 | "capability.capability_versions.deployment_descriptor" |  |
| `deploymentDescriptorDigest` | [Digest](rest-schemas.md#digest) | 是 | "capability.capability_versions.deployment_descriptor_digest" |  |
| `deploymentDescriptorObjectRef` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "capability.capability_versions.deployment_descriptor_object_ref" |  |

对象约束：`additionalProperties=false; allOf=[{"oneOf":[{"properties":{"provenance":{"enum":["build"]}},"required":["packageId","packageVersionId","buildJobId","runtimeVersionId","webuiVersionId"],"not":{"required":["legacyApplicationRevisionId"]}},{"properties":{"provenance":{"enum":["legacy_application"]}},"required":["legacyApplicationRevisionId"],"not":{"anyOf":[{"required":["packageId"]},{"required":["packageVersionId"]},{"required":["buildJobId"]},{"required":["runtimeVersionId"]},{"required":["webuiVersionId"]}]}}]}]`

## BuildJob

类型：`object`；Owner：`build`；表：`build.build_jobs`
Build operation与job同事务持久化；succeeded要求Capability注册readback。重试创建新job不覆盖原任务。

| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `id` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "build.build_jobs.id" |  |
| `operationId` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "build.build_jobs.operation_id" |  |
| `packageVersionId` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "build.build_jobs.package_version_id" |  |
| `runtimeVersionId` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "build.build_jobs.runtime_version_id" |  |
| `webuiVersionId` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "build.build_jobs.webui_version_id" |  |
| `status` | `string` | 是 | "build.build_jobs.status" | enum=["queued","validating","building","pushing","registering","succeeded","failed","needs_attention"] |
| `stage` | `string` | 是 | "build.build_jobs.stage" |  |
| `artifactDigest` | [Digest](rest-schemas.md#digest) | 否 | "build.build_jobs.artifact_digest" |  |
| `resultCapabilityVersionId` | [OpaqueId](rest-schemas.md#opaqueid) | 否 | "build.build_jobs.result_capability_version_id" |  |
| `retryOfBuildJobId` | [OpaqueId](rest-schemas.md#opaqueid) | 否 | "build.build_jobs.retry_of_build_job_id" |  |
| `errorCode` | [ErrorCode](rest-schemas.md#errorcode) | 否 | "build.build_jobs.error_code" |  |
| `createdAt` | `string/date-time` | 是 | "build.build_jobs.created_at" |  |
| `updatedAt` | `string/date-time` | 是 | "build.build_jobs.updated_at" |  |
| `retryAllowed` | `boolean` | 是 | {"kind": "derived", "sources": ["build.build_jobs.status", "build.build_jobs.error_code", "build.operations.observation_result"], "rule": "仅status=failed且errorCode为DEPENDENCY_UNAVAILABLE或ARTIFACT_UNAVAILABLE，且原push/readback不存在未决副作用时true；代码/格式失败须新输入，不猜测可重试。"} |  |
| `inputClaimIds` | `array<OpaqueId>` | 否 | {"kind": "derived", "sources": ["build.build_jobs.input_snapshot"], "rule": "typed snapshot.inputClaimIds；queued接受时可缺省，任何Storage读取/Build前必须取得并Bind三个精确输入claims"} | minItems=3; maxItems=3; uniqueItems=true |

对象约束：`additionalProperties=false`

## CreateBuildRequest

类型：`object`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`
runtimeVersionId来自管理员有效默认策略，Build创建时冻结。客户不传镜像/provider。

| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `packageVersionId` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "未在本schema声明" |  |
| `webuiVersionId` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "未在本schema声明" |  |

对象约束：`additionalProperties=false`

## BuildLog

类型：`object`；Owner：`build`；表：`build.build_logs`
结构化脱敏日志，无Key、密码、原始用户文件。

| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `id` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "build.build_logs.id" |  |
| `buildJobId` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "build.build_logs.build_job_id" |  |
| `sequence` | [NonnegativeInt64](rest-schemas.md#nonnegativeint64) | 是 | "build.build_logs.sequence" |  |
| `stage` | `string` | 是 | "build.build_logs.stage" |  |
| `level` | `string` | 是 | "build.build_logs.level" | enum=["info","warning","error"] |
| `message` | `string` | 是 | "build.build_logs.message" |  |
| `createdAt` | `string/date-time` | 是 | "build.build_logs.created_at" |  |

对象约束：`additionalProperties=false`

## RuntimeVersion

类型：`object`；Owner：`capability`；表：`capability.runtime_versions`
客户目录只渲染名称/版本/可用性；技术publisherContract供管理员及typed消费者。既有artifactDigest/ABI属性必须等于publisherContract的对应投影，不是可独立写第二事实。

| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `id` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "capability.runtime_versions.id" |  |
| `name` | `string` | 是 | "capability.runtime_versions.name" |  |
| `versionLabel` | `string` | 是 | "capability.runtime_versions.version_label" |  |
| `artifactDigest` | [Digest](rest-schemas.md#digest) | 是 | "capability.runtime_versions.artifact_digest" |  |
| `status` | `string` | 是 | "capability.runtime_versions.status" | enum=["approved","deprecated","revoked"] |
| `runtimeAbiVersion` | `string` | 是 | "capability.runtime_versions.runtime_abi_version" |  |
| `packageFormatVersions` | `array<string>` | 是 | "capability.runtime_versions.package_format_versions" |  |
| `defaultForNewBuilds` | `boolean` | 是 | {"kind": "derived", "sources": ["capability.catalog_policies.runtime_version_id", "capability.catalog_policies.effective_at"], "rule": "当前生效策略runtime_version_id等于本ID"} |  |
| `admissionReceiptId` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "capability.runtime_versions.admission_receipt_id" |  |
| `createdAt` | `string/date-time` | 是 | "capability.runtime_versions.created_at" |  |
| `publisherNamespaceId` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "capability.runtime_versions.publisher_namespace_id" |  |
| `publisherContractDigest` | [Digest](rest-schemas.md#digest) | 是 | "capability.runtime_versions.publisher_contract_digest" |  |
| `publisherContract` | [RuntimePublisherContract](rest-schemas.md#runtimepublishercontract) | 是 | "capability.runtime_versions.publisher_contract" |  |
| `publisherContractObjectRef` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "capability.runtime_versions.publisher_contract_object_ref" |  |

对象约束：`additionalProperties=false`

## WebuiVersion

类型：`object`；Owner：`capability`；表：`capability.webui_versions`
客户目录只渲染名称/版本/可用性；技术publisherContract供管理员及typed消费者。既有artifactDigest/ABI属性必须等于publisherContract的对应投影，不是可独立写第二事实。

| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `id` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "capability.webui_versions.id" |  |
| `name` | `string` | 是 | "capability.webui_versions.name" |  |
| `versionLabel` | `string` | 是 | "capability.webui_versions.version_label" |  |
| `artifactDigest` | [Digest](rest-schemas.md#digest) | 是 | "capability.webui_versions.artifact_digest" |  |
| `status` | `string` | 是 | "capability.webui_versions.status" | enum=["approved","deprecated","revoked"] |
| `runtimeAbiVersions` | `array<string>` | 是 | "capability.webui_versions.runtime_abi_versions" |  |
| `uiProtocolVersion` | `string` | 是 | "capability.webui_versions.ui_protocol_version" |  |
| `admissionReceiptId` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "capability.webui_versions.admission_receipt_id" |  |
| `createdAt` | `string/date-time` | 是 | "capability.webui_versions.created_at" |  |
| `publisherNamespaceId` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "capability.webui_versions.publisher_namespace_id" |  |
| `publisherContractDigest` | [Digest](rest-schemas.md#digest) | 是 | "capability.webui_versions.publisher_contract_digest" |  |
| `publisherContract` | [WebuiPublisherContract](rest-schemas.md#webuipublishercontract) | 是 | "capability.webui_versions.publisher_contract" |  |
| `publisherContractObjectRef` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "capability.webui_versions.publisher_contract_object_ref" |  |

对象约束：`additionalProperties=false`

## RegisterRuntimeVersionRequest

类型：`object`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`
仅管理员注册，Capability按canonical WorkspaceApplicationRevision校验+发布者schema验证，计算并保存descriptor bytes/digest，不接受客户端自报digest。publisherNamespaceId必须等于contract内字段并通过Registry路径和kind准入。

| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `name` | `string` | 是 | "未在本schema声明" | minLength=1; maxLength=256 |
| `versionLabel` | `string` | 是 | "未在本schema声明" | minLength=1; maxLength=256 |
| `publisherNamespaceId` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "未在本schema声明" |  |
| `publisherContract` | [RuntimePublisherContract](rest-schemas.md#runtimepublishercontract) | 是 | "未在本schema声明" |  |
| `admissionReceiptId` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "未在本schema声明" |  |

对象约束：`additionalProperties=false`

## RegisterWebuiVersionRequest

类型：`object`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`
仅管理员注册，Capability按canonical WorkspaceApplicationRevision校验+发布者schema验证，计算并保存descriptor bytes/digest，不接受客户端自报digest。publisherNamespaceId必须等于contract内字段并通过Registry路径和kind准入。

| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `name` | `string` | 是 | "未在本schema声明" | minLength=1; maxLength=256 |
| `versionLabel` | `string` | 是 | "未在本schema声明" | minLength=1; maxLength=256 |
| `publisherNamespaceId` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "未在本schema声明" |  |
| `publisherContract` | [WebuiPublisherContract](rest-schemas.md#webuipublishercontract) | 是 | "未在本schema声明" |  |
| `admissionReceiptId` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "未在本schema声明" |  |

对象约束：`additionalProperties=false`

## CatalogStatusRequest

类型：`object`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`
不修改版本内容。deprecated不改变已有运行；revoked阻止新引用，既有运行需显式处置不自动更新。

| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `status` | `string` | 是 | "未在本schema声明" | enum=["deprecated","revoked"] |
| `reason` | `string` | 是 | "未在本schema声明" | minLength=1; maxLength=1024 |

对象约束：`additionalProperties=false`

## ComputePlan

类型：`object`；Owner：`resource_catalog`；表：`resource_catalog.compute_plans`
 仅查询指定另一侧planId且存在唯一有效组合价时返回monthlyPriceUSDMicros/pricePolicyVersionId；否则省略，UI展示“选择完整套餐后报价”，不猜最低价。

| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `id` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "resource_catalog.compute_plans.id" |  |
| `name` | `string` | 是 | "resource_catalog.compute_plans.name" |  |
| `vcpus` | `integer/int32` | 是 | "resource_catalog.compute_plans.vcpus" | minimum=1 |
| `memoryMiB` | `integer/int32` | 是 | "resource_catalog.compute_plans.memory_mib" | minimum=1 |
| `availability` | `string` | 是 | {"kind": "derived", "sources": ["resource_catalog.compute_plans.status", "resource_catalog.compute_plans.valid_from", "resource_catalog.compute_plans.valid_until", "resource_catalog.compute_plans.provider_capability_version"], "rule": "status=revoked/deprecated=>retired；approved且有效期内且provider能力确认=>available；否则unavailable"} | enum=["available","unavailable","retired"] |
| `billingMode` | `string` | 是 | {"kind": "derived", "sources": ["resource_catalog.compute_plans.billing_mode"], "rule": "PREPAID_MONTHLY映射prepaid_monthly；LOCAL_NO_CHARGE映射local_no_charge，仅Local计划"} | enum=["prepaid_monthly","local_no_charge"] |
| `providerCapabilityVersion` | `string` | 是 | "resource_catalog.compute_plans.provider_capability_version" |  |
| `monthlyPriceUSDMicros` | [USDMicros](rest-schemas.md#usdmicros) | 否 | {"kind": "derived", "sources": ["resource_catalog.price_policy_versions.compute_plan_id", "resource_catalog.price_policy_versions.storage_plan_id", "resource_catalog.price_policy_versions.compute_monthly_usd_micros"], "rule": "指定exact compute/storage pair；按validFrom<=now最大版本确定唯一策略，再检查该版本validUntil；过期不回落旧版。未指定另一侧或未配置则省略"} |  |
| `pricePolicyVersionId` | [OpaqueId](rest-schemas.md#opaqueid) | 否 | {"kind": "derived", "sources": ["resource_catalog.price_policy_versions.id", "resource_catalog.price_policy_versions.compute_plan_id", "resource_catalog.price_policy_versions.storage_plan_id"], "rule": "与同DTO月价精确匹配的有效组合价格版本，否则省略"} |  |
| `validFrom` | `string/date-time` | 是 | "resource_catalog.compute_plans.valid_from" |  |
| `validUntil` | `string/date-time` | 否 | "resource_catalog.compute_plans.valid_until" |  |
| `createdAt` | `string/date-time` | 是 | "resource_catalog.compute_plans.created_at" |  |

对象约束：`additionalProperties=false`

## StoragePlan

类型：`object`；Owner：`resource_catalog`；表：`resource_catalog.storage_plans`
 仅查询指定另一侧planId且存在唯一有效组合价时返回monthlyPriceUSDMicros/pricePolicyVersionId；否则省略，UI展示“选择完整套餐后报价”，不猜最低价。

| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `id` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "resource_catalog.storage_plans.id" |  |
| `name` | `string` | 是 | "resource_catalog.storage_plans.name" |  |
| `capacityGiB` | `integer/int32` | 是 | "resource_catalog.storage_plans.capacity_gib" | minimum=1 |
| `availability` | `string` | 是 | {"kind": "derived", "sources": ["resource_catalog.storage_plans.status", "resource_catalog.storage_plans.valid_from", "resource_catalog.storage_plans.valid_until", "resource_catalog.storage_plans.provider_capability_version"], "rule": "status=revoked/deprecated=>retired；approved且有效期内且provider能力确认=>available；否则unavailable"} | enum=["available","unavailable","retired"] |
| `billingMode` | `string` | 是 | {"kind": "derived", "sources": ["resource_catalog.storage_plans.billing_mode"], "rule": "PREPAID_MONTHLY映射prepaid_monthly；LOCAL_NO_CHARGE映射local_no_charge，仅Local计划"} | enum=["prepaid_monthly","local_no_charge"] |
| `shrinkSupported` | `boolean` | 是 | "resource_catalog.storage_plans.shrink_supported" |  |
| `monthlyPriceUSDMicros` | [USDMicros](rest-schemas.md#usdmicros) | 否 | {"kind": "derived", "sources": ["resource_catalog.price_policy_versions.compute_plan_id", "resource_catalog.price_policy_versions.storage_plan_id", "resource_catalog.price_policy_versions.storage_monthly_usd_micros"], "rule": "指定exact compute/storage pair；按validFrom<=now最大版本确定唯一策略，再检查该版本validUntil；过期不回落旧版。未指定另一侧或未配置则省略"} |  |
| `pricePolicyVersionId` | [OpaqueId](rest-schemas.md#opaqueid) | 否 | {"kind": "derived", "sources": ["resource_catalog.price_policy_versions.id", "resource_catalog.price_policy_versions.compute_plan_id", "resource_catalog.price_policy_versions.storage_plan_id"], "rule": "与同DTO月价精确匹配的有效组合价格版本，否则省略"} |  |
| `validFrom` | `string/date-time` | 是 | "resource_catalog.storage_plans.valid_from" |  |
| `validUntil` | `string/date-time` | 否 | "resource_catalog.storage_plans.valid_until" |  |
| `createdAt` | `string/date-time` | 是 | "resource_catalog.storage_plans.created_at" |  |

对象约束：`additionalProperties=false`

## CreateComputePlanRequest

类型：`object`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`


| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `name` | `string` | 是 | "未在本schema声明" | minLength=1; maxLength=256 |
| `vcpus` | `integer/int32` | 是 | "未在本schema声明" | minimum=1 |
| `memoryMiB` | `integer/int32` | 是 | "未在本schema声明" | minimum=1 |
| `providerProfileId` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "未在本schema声明" |  |
| `providerSkuId` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "未在本schema声明" |  |
| `providerCapabilityVersion` | `string` | 是 | "未在本schema声明" |  |
| `validFrom` | `string/date-time` | 是 | "未在本schema声明" |  |
| `validUntil` | `string/date-time` | 否 | "未在本schema声明" |  |

对象约束：`additionalProperties=false`

## CreateStoragePlanRequest

类型：`object`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`


| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `name` | `string` | 是 | "未在本schema声明" | minLength=1; maxLength=256 |
| `capacityGiB` | `integer/int32` | 是 | "未在本schema声明" | minimum=1 |
| `providerProfileId` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "未在本schema声明" |  |
| `providerSkuId` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "未在本schema声明" |  |
| `shrinkSupported` | `boolean` | 是 | "未在本schema声明" |  |
| `validFrom` | `string/date-time` | 是 | "未在本schema声明" |  |
| `validUntil` | `string/date-time` | 否 | "未在本schema声明" |  |

对象约束：`additionalProperties=false`

## PlanAvailabilityRequest

类型：`object`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`


| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `availability` | `string` | 是 | "未在本schema声明" | enum=["available","unavailable","retired"] |
| `reason` | `string` | 是 | "未在本schema声明" | minLength=1; maxLength=1024 |

对象约束：`additionalProperties=false`

## PricePolicyVersion

类型：`object`；Owner：`resource_catalog`；表：`resource_catalog.price_policy_versions`
不可变Cloud套餐价格，不含Gateway模型价；无默认金额。 精确绑定computePlanId/storagePlanId组合；同pair+validFrom唯一；取validFrom<=now最大的版本后检查该版本validUntil，若过期则POLICY_UNCONFIGURED且不回落旧版，不以最新价格覆盖旧报价。先创建资源计划再创建价格，未定价不能报价。 固定已批准D17政策；旧基础退款policy和历史义务保持原样。

| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `id` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "resource_catalog.price_policy_versions.id" |  |
| `versionLabel` | `string` | 是 | "resource_catalog.price_policy_versions.version_label" |  |
| `currency` | `string` | 是 | "resource_catalog.price_policy_versions.currency" | enum=["USD"] |
| `periodMonths` | `integer` | 是 | "resource_catalog.price_policy_versions.period_months" | enum=[1] |
| `computeMonthlyUSDMicros` | [USDMicros](rest-schemas.md#usdmicros) | 是 | "resource_catalog.price_policy_versions.compute_monthly_usd_micros" |  |
| `storageMonthlyUSDMicros` | [USDMicros](rest-schemas.md#usdmicros) | 是 | "resource_catalog.price_policy_versions.storage_monthly_usd_micros" |  |
| `productMonthlyUSDMicros` | [USDMicros](rest-schemas.md#usdmicros) | 是 | "resource_catalog.price_policy_versions.product_monthly_usd_micros" |  |
| `validFrom` | `string/date-time` | 是 | "resource_catalog.price_policy_versions.valid_from" |  |
| `validUntil` | `string/date-time` | 否 | "resource_catalog.price_policy_versions.valid_until" |  |
| `createdAt` | `string/date-time` | 是 | "resource_catalog.price_policy_versions.created_at" |  |
| `computePlanId` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "resource_catalog.price_policy_versions.compute_plan_id" |  |
| `storagePlanId` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "resource_catalog.price_policy_versions.storage_plan_id" |  |
| `renewalPolicy` | [RenewalPolicy](rest-schemas.md#renewalpolicy) | 是 | "resource_catalog.price_policy_versions.renewal_rules" |  |
| `planChangePolicyVersion` | `string` | 是 | "resource_catalog.price_policy_versions.plan_change_policy_version" | enum=["workspace-plan-change-v1"] |
| `planChangePolicy` | [PlanChangePolicy](rest-schemas.md#planchangepolicy) | 是 | {"kind": "derived", "sources": ["resource_catalog.price_policy_versions.plan_change_policy_version"], "rule": "按精确version读取contracts/plan-change-policy.json中固定已批准x-policy；不重复持久化固定policy JSON/不从旧draft推测"} |  |

对象约束：`additionalProperties=false`

## CreatePricePolicyRequest

类型：`object`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`
 精确绑定computePlanId/storagePlanId组合；同pair+validFrom唯一；取validFrom<=now最大的版本后检查该版本validUntil，若过期则POLICY_UNCONFIGURED且不回落旧版，不以最新价格覆盖旧报价。先创建资源计划再创建价格，未定价不能报价。 固定已批准D17政策；旧基础退款policy和历史义务保持原样。

| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `versionLabel` | `string` | 是 | "未在本schema声明" | minLength=1; maxLength=256 |
| `periodMonths` | `integer` | 是 | "未在本schema声明" | enum=[1] |
| `computeMonthlyUSDMicros` | [USDMicros](rest-schemas.md#usdmicros) | 是 | "未在本schema声明" |  |
| `storageMonthlyUSDMicros` | [USDMicros](rest-schemas.md#usdmicros) | 是 | "未在本schema声明" |  |
| `productMonthlyUSDMicros` | [USDMicros](rest-schemas.md#usdmicros) | 是 | "未在本schema声明" |  |
| `validFrom` | `string/date-time` | 是 | "未在本schema声明" |  |
| `validUntil` | `string/date-time` | 否 | "未在本schema声明" |  |
| `computePlanId` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "未在本schema声明" |  |
| `storagePlanId` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "未在本schema声明" |  |
| `renewalPolicy` | [RenewalPolicy](rest-schemas.md#renewalpolicy) | 是 | "未在本schema声明" |  |
| `planChangePolicyVersion` | `string` | 是 | "未在本schema声明" | enum=["workspace-plan-change-v1"] |

对象约束：`additionalProperties=false`

## RefundPolicyVersion

类型：`object`；Owner：`resource_catalog`；表：`resource_catalog.refund_policy_versions`
workspace-delete-refund-v1：usedHours=ceil((deletedAt-resourceFulfilledAt)/1h),refund=floor(originalPlatformChargeUSDMicros*max(720-usedHours,0)/720)。必须deletedAt>resourceFulfilledAt，绑定本付费周期原单与确切资源删除absence receipt；用整数/有理数，不浮点。其他算法需新显式产品决定，不覆盖历史。

| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `id` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "resource_catalog.refund_policy_versions.id" |  |
| `versionLabel` | `string` | 是 | "resource_catalog.refund_policy_versions.version_label" |  |
| `algorithm` | `string` | 是 | "resource_catalog.refund_policy_versions.algorithm" | enum=["workspace-delete-refund-v1"] |
| `retentionPolicyVersionId` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "resource_catalog.refund_policy_versions.retention_policy_version_id" |  |
| `customerTerms` | `string` | 是 | "resource_catalog.refund_policy_versions.customer_terms" |  |
| `validFrom` | `string/date-time` | 是 | "resource_catalog.refund_policy_versions.valid_from" |  |
| `validUntil` | `string/date-time` | 否 | "resource_catalog.refund_policy_versions.valid_until" |  |
| `createdAt` | `string/date-time` | 是 | "resource_catalog.refund_policy_versions.created_at" |  |

对象约束：`additionalProperties=false`

## CreateRefundPolicyRequest

类型：`object`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`


| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `versionLabel` | `string` | 是 | "未在本schema声明" | minLength=1; maxLength=256 |
| `algorithm` | `string` | 是 | "未在本schema声明" | enum=["workspace-delete-refund-v1"] |
| `retentionPolicyVersionId` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "未在本schema声明" |  |
| `customerTerms` | `string` | 是 | "未在本schema声明" |  |
| `validFrom` | `string/date-time` | 是 | "未在本schema声明" |  |
| `validUntil` | `string/date-time` | 否 | "未在本schema声明" |  |

对象约束：`additionalProperties=false`

## RetentionPolicyVersion

类型：`object`；Owner：`resource_catalog`；表：`resource_catalog.retention_policy_versions`


| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `id` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "resource_catalog.retention_policy_versions.id" |  |
| `versionLabel` | `string` | 是 | "resource_catalog.retention_policy_versions.version_label" |  |
| `workspaceDataDisposition` | `string` | 是 | "resource_catalog.retention_policy_versions.workspace_data_disposition" | enum=["destroy_after_confirmed_deletion"] |
| `packageHistoryDisposition` | `string` | 是 | "resource_catalog.retention_policy_versions.package_history_disposition" | enum=["retain"] |
| `buildHistoryDisposition` | `string` | 是 | "resource_catalog.retention_policy_versions.build_history_disposition" | enum=["retain"] |
| `tenantRestoreDays` | `integer` | 是 | "resource_catalog.retention_policy_versions.tenant_restore_days" | enum=[15] |
| `customerTerms` | `string` | 是 | "resource_catalog.retention_policy_versions.customer_terms" |  |
| `createdAt` | `string/date-time` | 是 | "resource_catalog.retention_policy_versions.created_at" |  |

对象约束：`additionalProperties=false`

## CreateRetentionPolicyRequest

类型：`object`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`
固定Package/Build历史retain、Workspace数据在确认删除后销毁、Tenant 15天身份恢复；不引入自动90天清理。

| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `versionLabel` | `string` | 是 | "未在本schema声明" | minLength=1; maxLength=256 |
| `customerTerms` | `string` | 是 | "未在本schema声明" |  |

对象约束：`additionalProperties=false`

## Model

类型：`object`；Owner：`gateway`；表：`—`
Gateway模型价格实时readback；不成为Cloud price policy。

| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `id` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "未在本schema声明" |  |
| `name` | `string` | 是 | "未在本schema声明" |  |
| `capabilities` | `array<string>` | 是 | "未在本schema声明" |  |
| `available` | `boolean` | 是 | "未在本schema声明" |  |
| `inputPricePerMillionTokensUSDMicros` | [USDMicros](rest-schemas.md#usdmicros) | 是 | "未在本schema声明" |  |
| `outputPricePerMillionTokensUSDMicros` | [USDMicros](rest-schemas.md#usdmicros) | 是 | "未在本schema声明" |  |
| `priceSource` | `string` | 是 | "未在本schema声明" | enum=["gateway"] |
| `fetchedAt` | `string/date-time` | 是 | "未在本schema声明" |  |

对象约束：`additionalProperties=false`

## ModelSelection

类型：`object`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`


| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `slot` | `string` | 是 | "未在本schema声明" |  |
| `modelId` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "未在本schema声明" |  |

对象约束：`additionalProperties=false`

## QuoteRequest

类型：`object`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`
deploy无workspaceId；resize必须当前有效已付Workspace、无其他未完成PlanChange、target为批准可比较transition；renew服务端读取唯一scheduledPlanChange并按其已接受目标价生成下一期款项，不允许客户隐藏计划或先扣旧价。scheduledPlanChangeId仅renew可提供且必须匹配服务端唯一计划。客户端永不提交价格/T/计算比例。

| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `purpose` | `string` | 是 | "未在本schema声明" | enum=["deploy","resize","renew"] |
| `workspaceId` | [OpaqueId](rest-schemas.md#opaqueid) | 否 | "未在本schema声明" |  |
| `capabilityVersionId` | [OpaqueId](rest-schemas.md#opaqueid) | 否 | "未在本schema声明" |  |
| `computePlanId` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "未在本schema声明" |  |
| `storagePlanId` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "未在本schema声明" |  |
| `modelSelections` | `array<ModelSelection>` | 是 | "未在本schema声明" |  |
| `periodMonths` | `integer` | 是 | "未在本schema声明" | enum=[1] |
| `scheduledPlanChangeId` | [OpaqueId](rest-schemas.md#opaqueid) | 否 | "未在本schema声明" |  |

对象约束：`additionalProperties=false; oneOf=[{"properties":{"purpose":{"enum":["deploy"]}},"required":["capabilityVersionId"],"not":{"required":["workspaceId"]}},{"properties":{"purpose":{"enum":["resize","renew"]}},"required":["workspaceId"]}]; allOf=[{"oneOf":[{"properties":{"purpose":{"enum":["renew"]}}},{"properties":{"purpose":{"enum":["deploy","resize"]}},"not":{"required":["scheduledPlanChangeId"]}}]}]`

## QuoteLine

类型：`object`；Owner：`resource_catalog`；表：`resource_catalog.quote_items`
amountUSDMicros每项均非负且是该行总金额；quantity是解释用数量，不再二次相乘。total=sum(compute,storage,product amounts)-sum(adjustment_credit amounts)，total>=0，整数安全；不能把credit存负号。

| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `kind` | `string` | 是 | "resource_catalog.quote_items.kind" | enum=["compute","storage","product","adjustment_credit"] |
| `description` | `string` | 是 | "resource_catalog.quote_items.description" |  |
| `quantity` | `integer/int32` | 是 | "resource_catalog.quote_items.quantity" | minimum=1 |
| `amountUSDMicros` | [USDMicros](rest-schemas.md#usdmicros) | 是 | "resource_catalog.quote_items.amount_usd_micros" |  |
| `creditSource` | [CreditSource](rest-schemas.md#creditsource) | 否 | "resource_catalog.quote_items.credit_source" |  |

对象约束：`additionalProperties=false; allOf=[{"oneOf":[{"properties":{"kind":{"enum":["adjustment_credit"]}},"required":["creditSource"]},{"properties":{"kind":{"enum":["compute","storage","product"]}},"not":{"required":["creditSource"]}}]}]`

## Quote

类型：`object`；Owner：`resource_catalog`；表：`resource_catalog.quotes`
Catalog不可变报价。deploy/renew沿已批准月价；resize totalUSDMicros严格等于planChangeCalculation.chargeUSDMicros：升级固定T补差，降配当期0但nextPeriod列明目标下期价。sourceSubscriptionVersion在Workspace接受时CAS防旧报价；接受后时间推进不重算T/收费。renew存在唯一scheduledPlanChange时scheduledPlanChangeId必须绑定其目标价，不先旧价扣款。lineItems正项减非负credit，不二次乘quantity。 固定已批准D17政策；旧基础退款policy和历史义务保持原样。 升级resize只用一条kind=product、description=本期套餐升级补差的明细，amount=整体最终ceil的charge，不分组件再次ceil；downgrade当期lineItems空数组/total=0，目标下期金额仅nextPeriod。 legacy_resource_only的resize/renew可无capabilityVersionId且runtimeReadbackRequirement=not_applicable，来自Workspace而非客户跳过验证。存在未开始且已accepted/confirmed的其他周期资金义务拒绝新PlanChange FUTURE_PERIOD_COMMITTED；不重价/退旧款。

| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `id` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "resource_catalog.quotes.id" |  |
| `purpose` | `string` | 是 | "resource_catalog.quotes.purpose" | enum=["deploy","resize","renew"] |
| `workspaceId` | [OpaqueId](rest-schemas.md#opaqueid) | 否 | "resource_catalog.quotes.workspace_id" |  |
| `capabilityVersionId` | [OpaqueId](rest-schemas.md#opaqueid) | 否 | "resource_catalog.quotes.capability_version_id" |  |
| `computePlanId` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "resource_catalog.quotes.compute_plan_id" |  |
| `storagePlanId` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "resource_catalog.quotes.storage_plan_id" |  |
| `modelSelections` | `array<ModelSelection>` | 是 | "resource_catalog.quotes.model_selections" |  |
| `periodMonths` | `integer` | 是 | "resource_catalog.quotes.period_months" | enum=[1] |
| `periodStart` | `string/date-time` | 是 | "resource_catalog.quotes.period_start" |  |
| `periodEnd` | `string/date-time` | 是 | "resource_catalog.quotes.period_end" |  |
| `pricePolicyVersionId` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "resource_catalog.quotes.price_policy_version_id" |  |
| `refundPolicyVersionId` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "resource_catalog.quotes.refund_policy_version_id" |  |
| `retentionPolicyVersionId` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "resource_catalog.quotes.retention_policy_version_id" |  |
| `refundTerms` | `string` | 是 | "resource_catalog.quotes.refund_terms" |  |
| `retentionTerms` | `string` | 是 | "resource_catalog.quotes.retention_terms" |  |
| `expectedInterruption` | `string` | 是 | "resource_catalog.quotes.expected_interruption" |  |
| `lineItems` | `array<QuoteLine>` | 是 | {"kind": "derived", "sources": ["resource_catalog.quote_items.quote_id"], "rule": "本quote_items按sort_order排列"} |  |
| `totalUSDMicros` | [USDMicros](rest-schemas.md#usdmicros) | 是 | "resource_catalog.quotes.total_usd_micros" |  |
| `status` | `string` | 是 | "resource_catalog.quotes.status" | enum=["offered","accepted","expired"] |
| `expiresAt` | `string/date-time` | 是 | "resource_catalog.quotes.expires_at" |  |
| `createdAt` | `string/date-time` | 是 | "resource_catalog.quotes.created_at" |  |
| `sourceSubscriptionVersion` | [NonnegativeInt64](rest-schemas.md#nonnegativeint64) | 否 | "resource_catalog.quotes.source_subscription_version" |  |
| `planChangeCalculation` | [PlanChangeCalculation](rest-schemas.md#planchangecalculation) | 否 | "resource_catalog.quotes.plan_change_calculation" |  |
| `scheduledPlanChangeId` | [OpaqueId](rest-schemas.md#opaqueid) | 否 | "resource_catalog.quotes.scheduled_plan_change_id" |  |
| `runtimeReadbackRequirement` | `string` | 是 | {"kind": "derived", "sources": ["resource_catalog.quotes.admission_snapshot"], "rule": "typed admission_snapshot.runtimeReadbackRequirement，源自Workspace实际绑定；非客户选择"} | enum=["required","not_applicable"] |

对象约束：`additionalProperties=false; allOf=[{"oneOf":[{"properties":{"purpose":{"enum":["resize"]}},"required":["workspaceId","sourceSubscriptionVersion","planChangeCalculation"]},{"properties":{"purpose":{"enum":["renew"]}},"required":["workspaceId","sourceSubscriptionVersion"],"not":{"required":["planChangeCalculation"]}},{"properties":{"purpose":{"enum":["deploy"]}},"not":{"anyOf":[{"required":["sourceSubscriptionVersion"]},{"required":["planChangeCalculation"]},{"required":["scheduledPlanChangeId"]}]}}]},{"oneOf":[{"properties":{"purpose":{"enum":["deploy","renew"]}}},{"properties":{"purpose":{"enum":["resize"]},"planChangeCalculation":{"properties":{"kind":{"enum":["upgrade_immediate"]}}},"lineItems":{"minItems":1,"maxItems":1,"items":{"allOf":[{"$ref":"#/components/schemas/QuoteLine"},{"properties":{"kind":{"enum":["product"]},"quantity":{"enum":[1]}}}]}}}},{"properties":{"purpose":{"enum":["resize"]},"planChangeCalculation":{"properties":{"kind":{"enum":["downgrade_next_period"]}}},"lineItems":{"maxItems":0},"totalUSDMicros":{"enum":["0"]}}}]},{"oneOf":[{"properties":{"purpose":{"enum":["deploy"]},"runtimeReadbackRequirement":{"enum":["required"]}},"required":["capabilityVersionId"]},{"properties":{"purpose":{"enum":["resize","renew"]},"runtimeReadbackRequirement":{"enum":["required"]}},"required":["capabilityVersionId"]},{"properties":{"purpose":{"enum":["resize","renew"]},"runtimeReadbackRequirement":{"enum":["not_applicable"]}},"not":{"required":["capabilityVersionId"]}}]}]`

## Workspace

类型：`object`；Owner：`workspace`；表：`workspace.workspaces`
activeDeploymentId唯一选中权威。currentPeriodEnd从本Owner subscription派生；accessUrl只真实可用后出现，打开仍经getWorkspaceAccess准入。 legacy_resource_only允许capabilityVersionId缺省（不传null）且无activeDeploymentId；agent_saas/imported_application必须有capabilityVersionId。version是本Owner乐观锁版本。

| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `id` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "workspace.workspaces.id" |  |
| `name` | `string` | 是 | "workspace.workspaces.name" |  |
| `capabilityVersionId` | [OpaqueId](rest-schemas.md#opaqueid) | 否 | "workspace.workspaces.capability_version_id" |  |
| `computePlanId` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "workspace.workspaces.compute_plan_id" |  |
| `storagePlanId` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "workspace.workspaces.storage_plan_id" |  |
| `activeDeploymentId` | [OpaqueId](rest-schemas.md#opaqueid) | 否 | "workspace.workspaces.active_deployment_id" |  |
| `status` | `string` | 是 | "workspace.workspaces.status" | enum=["provisioning","active","updating","suspended","deleting","deleted","failed","needs_attention"] |
| `resourceReadiness` | `string` | 是 | {"kind": "derived", "sources": ["fabric.resource_sets.observation_result", "fabric.resources.observed_specification", "fabric.resources.observed_at"], "rule": "typed Fabric当前规格和观察结果映射，未确认unknown，不拿Workspace.status猜测"} | enum=["pending","ready","unavailable","unknown"] |
| `applicationAvailability` | `string` | 是 | {"kind": "derived", "sources": ["workspace.workspaces.active_deployment_id", "runtime_control.runtime_instances.status", "runtime_control.runtime_instances.readiness_evidence_ref"], "rule": "Runtime当前readiness及访问准入，legacy裸资源无部署显示unavailable"} | enum=["pending","available","unavailable","unknown"] |
| `modelConfigurationVersion` | [NonnegativeInt64](rest-schemas.md#nonnegativeint64) | 是 | "workspace.workspaces.model_configuration_version" |  |
| `currentPeriodEnd` | `string/date-time` | 否 | "workspace.subscriptions.current_period_end" |  |
| `accessUrl` | `string/uri` | 否 | {"kind": "derived", "sources": ["workspace.workspaces.active_deployment_id", "workspace.deployments.runtime_instance_id", "runtime_control.runtime_instances.access_url", "runtime_control.runtime_instances.status"], "rule": "按选中部署授权Runtime读回；ready且应用准入成功才返回URL"} |  |
| `createdAt` | `string/date-time` | 是 | "workspace.workspaces.created_at" |  |
| `updatedAt` | `string/date-time` | 是 | "workspace.workspaces.updated_at" |  |
| `deliveryModel` | `string` | 是 | "workspace.workspaces.delivery_model" | enum=["legacy_resource_only","imported_application","agent_saas"] |
| `version` | [NonnegativeInt64](rest-schemas.md#nonnegativeint64) | 是 | "workspace.workspaces.version" |  |

对象约束：`additionalProperties=false; allOf=[{"oneOf":[{"properties":{"deliveryModel":{"enum":["legacy_resource_only"]}}},{"properties":{"deliveryModel":{"enum":["imported_application","agent_saas"]}},"required":["capabilityVersionId"]}]}]`

## CreateWorkspaceRequest

类型：`object`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`
quote快照唯一决定版本/套餐/模型/费用；不接受自报tenant/provider/runtime/价格。 automatic必须用户显式勾选同报价续费政策授权，Owner保存consent快照并返回renewalConsentId；前端默认manual仅UI初值，不代替请求必填。

| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `name` | `string` | 是 | "未在本schema声明" | minLength=1; maxLength=256 |
| `quoteId` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "未在本schema声明" |  |
| `renewalMode` | `string` | 是 | "未在本schema声明" | enum=["manual","automatic"]; default="manual" |
| `automaticRenewalConsent` | `boolean` | 否 | "未在本schema声明" | enum=[true] |

对象约束：`additionalProperties=false; oneOf=[{"properties":{"renewalMode":{"enum":["manual"]}},"not":{"required":["automaticRenewalConsent"]}},{"properties":{"renewalMode":{"enum":["automatic"]}},"required":["automaticRenewalConsent"]}]`

## WorkspaceAccess

类型：`object`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`
严格来自canonical revision.exposurePolicy/WorkspaceApplicationEntry。application保留应用自身登录；cloud_private仅有当前provider确认受保护入口才可打开，无该能力返回APP_ACCESS_UNAVAILABLE。不新建应用SSO/session权威，不传平台Cookie/Token进应用。 applicationCredentialsAvailable只表示当前声明/实际能力存在；reveal另做owner授权，普通成员按应用自身身份登录。

| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `workspaceId` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "未在本schema声明" |  |
| `url` | `string/uri` | 是 | "未在本schema声明" |  |
| `authenticationMode` | `string` | 是 | "未在本schema声明" | enum=["application_login","cloud_private","anonymous"] |
| `expiresAt` | `string/date-time` | 是 | "未在本schema声明" |  |
| `applicationCredentialsAvailable` | `boolean` | 否 | "未在本schema声明" |  |

对象约束：`additionalProperties=false`

## ModelConfiguration

类型：`object`；Owner：`workspace`；表：`workspace.model_configurations`


| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `workspaceId` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "workspace.model_configurations.workspace_id" |  |
| `version` | [NonnegativeInt64](rest-schemas.md#nonnegativeint64) | 是 | "workspace.model_configurations.version" |  |
| `selections` | `array<ModelSelection>` | 是 | "workspace.model_configurations.selections" |  |
| `status` | `string` | 是 | {"kind": "derived", "sources": ["workspace.model_configurations.runtime_reload_observation", "workspace.operations.status", "workspace.workspaces.model_configuration_version"], "rule": "version=appliedVersion且验证confirmed=>applied；Operationfailed=>failed；unknown/needs_attention=>needs_attention；否则pending"} | enum=["pending","applied","failed","needs_attention"] |
| `appliedVersion` | [NonnegativeInt64](rest-schemas.md#nonnegativeint64) | 否 | "workspace.workspaces.model_configuration_version" |  |
| `operationId` | [OpaqueId](rest-schemas.md#opaqueid) | 否 | "workspace.model_configurations.operation_id" |  |
| `updatedAt` | `string/date-time` | 是 | "workspace.model_configurations.updated_at" |  |

对象约束：`additionalProperties=false`

## UpdateWorkspaceModelsRequest

类型：`object`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`
Runtime reload readback appliedVersion==目标version后才成功。

| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `expectedVersion` | [NonnegativeInt64](rest-schemas.md#nonnegativeint64) | 是 | "未在本schema声明" |  |
| `selections` | `array<ModelSelection>` | 是 | "未在本schema声明" |  |

对象约束：`additionalProperties=false`

## Deployment

类型：`object`；Owner：`workspace`；表：`workspace.deployments`


| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `id` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "workspace.deployments.id" |  |
| `workspaceId` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "workspace.deployments.workspace_id" |  |
| `capabilityVersionId` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "workspace.deployments.capability_version_id" |  |
| `runtimeInstanceId` | [OpaqueId](rest-schemas.md#opaqueid) | 否 | "workspace.deployments.runtime_instance_id" |  |
| `previousDeploymentId` | [OpaqueId](rest-schemas.md#opaqueid) | 否 | "workspace.deployments.previous_deployment_id" |  |
| `status` | `string` | 是 | "workspace.deployments.status" | enum=["queued","deploying","verifying","active","superseded","failed","rolling_back","rolled_back","needs_attention"] |
| `dataCompatibility` | [DataCompatibility](rest-schemas.md#datacompatibility) | 是 | "workspace.deployments.data_compatibility" |  |
| `createdAt` | `string/date-time` | 是 | "workspace.deployments.created_at" |  |
| `updatedAt` | `string/date-time` | 是 | "workspace.deployments.updated_at" |  |

对象约束：`additionalProperties=false`

## UpdateWorkspaceVersionRequest

类型：`object`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`
Runtime重建先createBuild同PackageVersion+WebUI，以管理员当前Runtime产新CapabilityVersion，再显式update。不变购买历史，不自动resize。

| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `capabilityVersionId` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "未在本schema声明" |  |
| `expectedActiveDeploymentId` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "未在本schema声明" |  |

对象约束：`additionalProperties=false`

## RollbackWorkspaceRequest

类型：`object`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`


| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `targetDeploymentId` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "未在本schema声明" |  |
| `expectedActiveDeploymentId` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "未在本schema声明" |  |

对象约束：`additionalProperties=false`

## ApplyQuoteRequest

类型：`object`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`
purpose/workspaceId必须匹配本命令；周期业务幂等防重复延期/扣费。

| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `quoteId` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "未在本schema声明" |  |

对象约束：`additionalProperties=false`

## DeleteWorkspaceRequest

类型：`object`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`
精确匹配名称，确认销毁Workspace数据；不删除Package/Build历史。

| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `confirmationName` | `string` | 是 | "未在本schema声明" | minLength=1; maxLength=256 |
| `acknowledgeDataDestruction` | `boolean` | 是 | "未在本schema声明" | enum=[true] |

对象约束：`additionalProperties=false`

## WorkspaceDeletion

类型：`object`；Owner：`workspace`；表：`workspace.operations`


| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `workspaceId` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | {"kind": "derived", "sources": ["workspace.operations.result", "workspace.saga_steps.observation_result", "gateway.wallet_operations.status"], "rule": "按workspaceId已接受删除Operation及每阶段Owner readback投影；退款独立"} |  |
| `operationId` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | {"kind": "derived", "sources": ["workspace.operations.result", "workspace.saga_steps.observation_result", "gateway.wallet_operations.status"], "rule": "按workspaceId已接受删除Operation及每阶段Owner readback投影；退款独立"} |  |
| `resourceDeletionStatus` | `string` | 是 | {"kind": "derived", "sources": ["workspace.operations.result", "workspace.saga_steps.observation_result", "gateway.wallet_operations.status"], "rule": "按workspaceId已接受删除Operation及每阶段Owner readback投影；退款独立"} | enum=["pending","confirmed","rejected","unknown"] |
| `dataDeletionStatus` | `string` | 是 | {"kind": "derived", "sources": ["workspace.operations.result", "workspace.saga_steps.observation_result", "gateway.wallet_operations.status"], "rule": "按workspaceId已接受删除Operation及每阶段Owner readback投影；退款独立"} | enum=["pending","confirmed","rejected","unknown"] |
| `refundOperationId` | [OpaqueId](rest-schemas.md#opaqueid) | 否 | {"kind": "derived", "sources": ["workspace.operations.result", "workspace.saga_steps.observation_result", "gateway.wallet_operations.status"], "rule": "按workspaceId已接受删除Operation及每阶段Owner readback投影；退款独立"} |  |
| `refundStatus` | `string` | 是 | {"kind": "derived", "sources": ["workspace.operations.result", "workspace.saga_steps.observation_result", "gateway.wallet_operations.status"], "rule": "按workspaceId已接受删除Operation及每阶段Owner readback投影；退款独立"} | enum=["not_applicable","requested","confirmed","rejected","unknown"] |
| `updatedAt` | `string/date-time` | 是 | {"kind": "derived", "sources": ["workspace.operations.result", "workspace.saga_steps.observation_result", "gateway.wallet_operations.status"], "rule": "按workspaceId已接受删除Operation及每阶段Owner readback投影；退款独立"} |  |

对象约束：`additionalProperties=false`

## Subscription

类型：`object`；Owner：`workspace`；表：`workspace.subscriptions`
保留原订阅的manual/automatic事实；automatic必须精确原授权/同周期义务，不因迁移降为manual或重新授权。新订阅显式选择，默认manual不自动收费；到期无已确认付款停止。 version是周期/成功生效计划的业务CAS版本，独立于renewalSettingsVersion。currentPricePolicyVersionId/currentMonthlyUSDMicros只保存客户已接受且成功生效的当前价；连续升级以其为Pold。scheduled目标不提前改它。 legacy原价/政策证据不足时两current价格字段缺省，F11明确POLICY_UNCONFIGURED待Owner核实，不补0/不取最新价，不影响原资源只读访问。

| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `id` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "workspace.subscriptions.id" |  |
| `workspaceId` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "workspace.subscriptions.workspace_id" |  |
| `currentPeriodStart` | `string/date-time` | 是 | "workspace.subscriptions.current_period_start" |  |
| `currentPeriodEnd` | `string/date-time` | 是 | "workspace.subscriptions.current_period_end" |  |
| `periodMonths` | `integer/int32` | 是 | "workspace.subscriptions.period_months" | minimum=1 |
| `status` | `string` | 是 | {"kind": "derived", "sources": ["workspace.subscriptions.current_period_end", "workspace.workspaces.status"], "rule": "Workspace.deleted=>terminated；当前时间>=periodEnd=>expired；已确认周期=>active；未确认周期无Subscription记录"} | enum=["pending","active","expired","terminated"] |
| `acceptedQuoteId` | [OpaqueId](rest-schemas.md#opaqueid) | 否 | "workspace.subscriptions.accepted_quote_id" |  |
| `renewalOperationId` | [OpaqueId](rest-schemas.md#opaqueid) | 否 | "workspace.subscriptions.active_change_operation_id" |  |
| `createdAt` | `string/date-time` | 是 | "workspace.subscriptions.created_at" |  |
| `provenance` | `string` | 是 | "workspace.subscriptions.provenance" | enum=["quoted","legacy_import"] |
| `legacyPurchaseId` | [OpaqueId](rest-schemas.md#opaqueid) | 否 | "workspace.subscriptions.legacy_purchase_id" |  |
| `refundPolicyVersionId` | [OpaqueId](rest-schemas.md#opaqueid) | 否 | {"kind": "derived", "sources": ["workspace.subscriptions.accepted_quote_snapshot", "workspace.subscriptions.legacy_obligation_snapshot"], "rule": "typed原义务快照.refundPolicyVersionId；quoted来自已接受Quote快照，legacy只精确原证据，不推断缺失事实"} |  |
| `retentionPolicyVersionId` | [OpaqueId](rest-schemas.md#opaqueid) | 否 | {"kind": "derived", "sources": ["workspace.subscriptions.accepted_quote_snapshot", "workspace.subscriptions.legacy_obligation_snapshot"], "rule": "typed原义务快照.retentionPolicyVersionId；quoted来自已接受Quote快照，legacy只精确原证据，不推断缺失事实"} |  |
| `refundTerms` | `string` | 否 | {"kind": "derived", "sources": ["workspace.subscriptions.accepted_quote_snapshot", "workspace.subscriptions.legacy_obligation_snapshot"], "rule": "typed原义务快照.refundTerms；quoted来自已接受Quote快照，legacy只精确原证据，不推断缺失事实"} |  |
| `retentionTerms` | `string` | 否 | {"kind": "derived", "sources": ["workspace.subscriptions.accepted_quote_snapshot", "workspace.subscriptions.legacy_obligation_snapshot"], "rule": "typed原义务快照.retentionTerms；quoted来自已接受Quote快照，legacy只精确原证据，不推断缺失事实"} |  |
| `renewalMode` | `string` | 是 | "workspace.subscriptions.renewal_mode" | enum=["manual","automatic"] |
| `renewalConsentId` | [OpaqueId](rest-schemas.md#opaqueid) | 否 | "workspace.subscriptions.renewal_consent_id" |  |
| `renewalSettingsVersion` | [NonnegativeInt64](rest-schemas.md#nonnegativeint64) | 是 | "workspace.subscriptions.renewal_settings_version" |  |
| `version` | [NonnegativeInt64](rest-schemas.md#nonnegativeint64) | 是 | "workspace.subscriptions.version" |  |
| `currentPricePolicyVersionId` | [OpaqueId](rest-schemas.md#opaqueid) | 否 | "workspace.subscriptions.current_price_policy_version_id" |  |
| `currentMonthlyUSDMicros` | [USDMicros](rest-schemas.md#usdmicros) | 否 | "workspace.subscriptions.current_monthly_usd_micros" |  |

对象约束：`additionalProperties=false; allOf=[{"oneOf":[{"properties":{"provenance":{"enum":["quoted"]}},"required":["acceptedQuoteId","refundPolicyVersionId","retentionPolicyVersionId","refundTerms","retentionTerms","currentPricePolicyVersionId","currentMonthlyUSDMicros"],"not":{"required":["legacyPurchaseId"]}},{"properties":{"provenance":{"enum":["legacy_import"]}},"required":["legacyPurchaseId"],"not":{"required":["acceptedQuoteId"]}}]},{"oneOf":[{"properties":{"renewalMode":{"enum":["manual"]}}},{"properties":{"renewalMode":{"enum":["automatic"]}},"required":["renewalConsentId"]}]}]`

## WalletOperation

类型：`object`；Owner：`gateway`；表：`gateway.wallet_operations`
非第二钱包，只存不透明原付款/幂等引用和观察结果。unknown不重扣/反向退款。 purpose区分基础周期款、升级补差、基础删除、升级确定失败全退、成功补差按自身T..E删除退款、下期计划失败全退。所有refund绑定originalChargeOperationId；unknown退款占同原charge可退额度但不改变wallet权威。

| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `id` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "gateway.wallet_operations.id" |  |
| `workspaceId` | [OpaqueId](rest-schemas.md#opaqueid) | 否 | "gateway.wallet_operations.workspace_id" |  |
| `kind` | `string` | 是 | "gateway.wallet_operations.kind" | enum=["charge","refund","recharge"] |
| `amountUSDMicros` | [USDMicros](rest-schemas.md#usdmicros) | 是 | "gateway.wallet_operations.amount_usd_micros" |  |
| `status` | `string` | 是 | "gateway.wallet_operations.status" | enum=["requested","confirmed","rejected","unknown"] |
| `externalReference` | [OpaqueId](rest-schemas.md#opaqueid) | 否 | "gateway.wallet_operations.external_reference" |  |
| `receiptId` | [OpaqueId](rest-schemas.md#opaqueid) | 否 | "gateway.wallet_operations.receipt_id" |  |
| `errorCode` | [ErrorCode](rest-schemas.md#errorcode) | 否 | "gateway.wallet_operations.error_code" |  |
| `createdAt` | `string/date-time` | 是 | "gateway.wallet_operations.created_at" |  |
| `updatedAt` | `string/date-time` | 是 | "gateway.wallet_operations.updated_at" |  |
| `purpose` | `string` | 否 | "gateway.wallet_operations.purpose" | enum=["base_period","upgrade_supplement","base_period_delete","upgrade_failure_full","supplement_delete_unused","next_period_plan_failure_full","recharge"] |
| `planChangeId` | [OpaqueId](rest-schemas.md#opaqueid) | 否 | "gateway.wallet_operations.plan_change_id" |  |
| `originalChargeOperationId` | [OpaqueId](rest-schemas.md#opaqueid) | 否 | "gateway.wallet_operations.original_wallet_operation_id" |  |
| `coverageStart` | `string/date-time` | 否 | "gateway.wallet_operations.coverage_start" |  |
| `coverageEnd` | `string/date-time` | 否 | "gateway.wallet_operations.coverage_end" |  |
| `coverageStartMilliseconds` | [NonnegativeInt64](rest-schemas.md#nonnegativeint64) | 否 | {"kind": "derived", "sources": ["gateway.wallet_operations.refund_entitlement_snapshot"], "rule": "typed原补差entitlement.coverageStartMilliseconds（或same original charge readback），不可从PG timestamptz重算"} |  |
| `coverageEndMilliseconds` | [NonnegativeInt64](rest-schemas.md#nonnegativeint64) | 否 | {"kind": "derived", "sources": ["gateway.wallet_operations.refund_entitlement_snapshot"], "rule": "typed原补差entitlement.coverageEndMilliseconds（或same original charge readback），不可从PG timestamptz重算"} |  |

对象约束：`additionalProperties=false`

## Wallet

类型：`object`；Owner：`gateway`；表：`—`
仅实时Gateway readback。失败503，不用缓存余额或0兜底。

| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `source` | `string` | 是 | "未在本schema声明" | enum=["gateway"] |
| `status` | `string` | 是 | "未在本schema声明" | enum=["available"] |
| `balanceUSDMicros` | [USDMicros](rest-schemas.md#usdmicros) | 是 | "未在本schema声明" |  |
| `currency` | `string` | 是 | "未在本schema声明" | enum=["USD"] |
| `fetchedAt` | `string/date-time` | 是 | "未在本schema声明" |  |

对象约束：`additionalProperties=false`

## Usage

类型：`object`；Owner：`gateway`；表：`—`


| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `id` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "未在本schema声明" |  |
| `modelId` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "未在本schema声明" |  |
| `periodStart` | `string/date-time` | 是 | "未在本schema声明" |  |
| `periodEnd` | `string/date-time` | 是 | "未在本schema声明" |  |
| `inputTokens` | [NonnegativeInt64](rest-schemas.md#nonnegativeint64) | 是 | "未在本schema声明" |  |
| `outputTokens` | [NonnegativeInt64](rest-schemas.md#nonnegativeint64) | 是 | "未在本schema声明" |  |
| `costUSDMicros` | [USDMicros](rest-schemas.md#usdmicros) | 是 | "未在本schema声明" |  |
| `source` | `string` | 是 | "未在本schema声明" | enum=["gateway"] |
| `createdAt` | `string/date-time` | 是 | "未在本schema声明" |  |

对象约束：`additionalProperties=false`

## GatewayKey

类型：`object`；Owner：`gateway`；表：`gateway.key_bindings`


| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `id` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "gateway.key_bindings.id" |  |
| `name` | `string` | 是 | "gateway.key_bindings.name" |  |
| `fingerprint` | `string` | 是 | "gateway.key_bindings.fingerprint" |  |
| `purpose` | `string` | 是 | "gateway.key_bindings.purpose" | enum=["personal","workspace_managed"] |
| `workspaceId` | [OpaqueId](rest-schemas.md#opaqueid) | 否 | "gateway.key_bindings.workspace_id" |  |
| `status` | `string` | 是 | {"kind": "derived", "sources": ["gateway.key_bindings.revoked_at", "gateway.key_bindings.observation_result"], "rule": "已确认binding且revoked_at NULL=>active，否则已确认撤销=>revoked；unknown拒绝伪造active"} | enum=["active","revoked"] |
| `modelIds` | `array<OpaqueId>` | 是 | "gateway.key_bindings.model_ids" |  |
| `createdAt` | `string/date-time` | 是 | "gateway.key_bindings.created_at" |  |
| `expiresAt` | `string/date-time` | 否 | "gateway.key_bindings.expires_at" |  |

对象约束：`additionalProperties=false`

## CreateGatewayKeyRequest

类型：`object`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`
只创建actor personal Key；workspace_managed仅Saga私有通道创建不可reveal。

| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `name` | `string` | 是 | "未在本schema声明" | minLength=1; maxLength=256 |
| `modelIds` | `array<OpaqueId>` | 是 | "未在本schema声明" |  |
| `expiresAt` | `string/date-time` | 否 | "未在本schema声明" |  |

对象约束：`additionalProperties=false`

## GatewayKeySecret

类型：`object`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`
仅创建或显式reveal一次响应出现，no-store，禁止DB/事件/日志/审计。幂等重放返回KEY_REVEAL_FORBIDDEN不重复发送明文，可显式reveal新key；Gateway无reveal能力返回OWNER_CAPABILITY_UNAVAILABLE。 授权必须当前actor本人personal Key或Gateway明确授权grant；Tenant成员资格不授予他人Key权限。workspace_managed一律禁止reveal。 幂等记录仅存Key引用、指纹和已交付标志，不缓存完整secret响应。

| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `key` | [GatewayKey](rest-schemas.md#gatewaykey) | 是 | "未在本schema声明" |  |
| `secret` | `string` | 是 | "未在本schema声明" |  |

对象约束：`additionalProperties=false`

## AuditEvent

类型：`object`；Owner：`tenant`；表：`tenant.audit_events`


| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `id` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "tenant.audit_events.id" |  |
| `actorId` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "tenant.audit_events.actor_id" |  |
| `action` | `string` | 是 | "tenant.audit_events.action" |  |
| `resourceId` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "tenant.audit_events.resource_id" |  |
| `outcome` | `string` | 是 | "tenant.audit_events.outcome" | enum=["confirmed","rejected","unknown"] |
| `requestId` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "tenant.audit_events.request_id" |  |
| `createdAt` | `string/date-time` | 是 | "tenant.audit_events.created_at" |  |

对象约束：`additionalProperties=false`

## Receipt

类型：`object`；Owner：`ledger`；表：`ledger.receipts`
无秘密/私网地址；Cloud静态检查、Candidate与Instance资格/部署证据分层。

| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `id` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "ledger.receipts.id" |  |
| `kind` | `string` | 是 | "ledger.receipts.kind" | enum=["source_check","candidate","qualification","deployment","rollback","release","provider_action","wallet_action","plan_change","supplemental_charge","plan_change_failure_compensation","supplemental_deletion_refund","next_period_plan_settlement"] |
| `owner` | [Owner](rest-schemas.md#owner) | 是 | "ledger.receipts.source_owner" |  |
| `sourceSha` | `string` | 否 | {"kind": "derived", "sources": ["ledger.receipts.evidence"], "rule": "typed evidence.provenance.sourceSha；该receipt不适用则省略"} |  |
| `artifactDigest` | [Digest](rest-schemas.md#digest) | 否 | {"kind": "derived", "sources": ["ledger.receipts.evidence"], "rule": "typed evidence.provenance.artifactDigest"} |  |
| `operationId` | [OpaqueId](rest-schemas.md#opaqueid) | 否 | "ledger.receipts.source_operation_id" |  |
| `workflowRunId` | [OpaqueId](rest-schemas.md#opaqueid) | 否 | {"kind": "derived", "sources": ["ledger.receipts.evidence"], "rule": "typed evidence.provenance.workflowRunId"} |  |
| `outcome` | `string` | 是 | {"kind": "derived", "sources": ["ledger.receipts.evidence"], "rule": "typed evidence.outcome"} | enum=["confirmed","rejected","unknown"] |
| `evidenceSummary` | `string` | 是 | {"kind": "derived", "sources": ["ledger.receipts.evidence"], "rule": "由安全typed evidence投影，不写第二字段"} |  |
| `createdAt` | `string/date-time` | 是 | "ledger.receipts.created_at" |  |

对象约束：`additionalProperties=false`

## ReconcileOperationRequest

类型：`object`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`
只重新读取外部Owner证据，不盲目重复扣费/采购/退款。

| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `reason` | `string` | 是 | "未在本schema声明" | minLength=1; maxLength=1024 |

对象约束：`additionalProperties=false`

## AdminOperation

类型：`object`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`


| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `operation` | [Operation](rest-schemas.md#operation) | 是 | "未在本schema声明" |  |
| `tenantId` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "未在本schema声明" |  |

对象约束：`additionalProperties=false`

## Qualification

类型：`object`；Owner：`ledger`；表：`ledger.receipts`


| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `id` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | {"kind": "derived", "sources": ["ledger.receipts.evidence"], "rule": "按exact Candidate SHA/digest联结typed receipt provenance；非新qualification状态writer"} |  |
| `candidateSourceSha` | `string` | 是 | {"kind": "derived", "sources": ["ledger.receipts.evidence"], "rule": "按exact Candidate SHA/digest联结typed receipt provenance；非新qualification状态writer"} |  |
| `artifactDigest` | [Digest](rest-schemas.md#digest) | 是 | {"kind": "derived", "sources": ["ledger.receipts.evidence"], "rule": "按exact Candidate SHA/digest联结typed receipt provenance；非新qualification状态writer"} |  |
| `localReceiptId` | [OpaqueId](rest-schemas.md#opaqueid) | 否 | {"kind": "derived", "sources": ["ledger.receipts.evidence"], "rule": "按exact Candidate SHA/digest联结typed receipt provenance；非新qualification状态writer"} |  |
| `instanceReceiptId` | [OpaqueId](rest-schemas.md#opaqueid) | 否 | {"kind": "derived", "sources": ["ledger.receipts.evidence"], "rule": "按exact Candidate SHA/digest联结typed receipt provenance；非新qualification状态writer"} |  |
| `status` | `string` | 是 | {"kind": "derived", "sources": ["ledger.receipts.evidence"], "rule": "按exact Candidate SHA/digest联结typed receipt provenance；非新qualification状态writer"} | enum=["pending","qualified","rejected"] |
| `createdAt` | `string/date-time` | 是 | {"kind": "derived", "sources": ["ledger.receipts.evidence"], "rule": "按exact Candidate SHA/digest联结typed receipt provenance；非新qualification状态writer"} |  |

对象约束：`additionalProperties=false`

## MemberPage

类型：`object`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`
createdAt DESC,id DESC；nextCursor缺省表示最后一页。

| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `items` | `array<Member>` | 是 | "未在本schema声明" |  |
| `nextCursor` | [OpaqueId](rest-schemas.md#opaqueid) | 否 | "未在本schema声明" |  |

对象约束：`additionalProperties=false`

## InvitationPage

类型：`object`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`
createdAt DESC,id DESC；nextCursor缺省表示最后一页。

| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `items` | `array<Invitation>` | 是 | "未在本schema声明" |  |
| `nextCursor` | [OpaqueId](rest-schemas.md#opaqueid) | 否 | "未在本schema声明" |  |

对象约束：`additionalProperties=false`

## NamespacePage

类型：`object`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`
createdAt DESC,id DESC；nextCursor缺省表示最后一页。

| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `items` | `array<Namespace>` | 是 | "未在本schema声明" |  |
| `nextCursor` | [OpaqueId](rest-schemas.md#opaqueid) | 否 | "未在本schema声明" |  |

对象约束：`additionalProperties=false`

## PackagePage

类型：`object`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`
createdAt DESC,id DESC；nextCursor缺省表示最后一页。

| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `items` | `array<Package>` | 是 | "未在本schema声明" |  |
| `nextCursor` | [OpaqueId](rest-schemas.md#opaqueid) | 否 | "未在本schema声明" |  |

对象约束：`additionalProperties=false`

## PackageVersionPage

类型：`object`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`
createdAt DESC,id DESC；nextCursor缺省表示最后一页。

| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `items` | `array<PackageVersion>` | 是 | "未在本schema声明" |  |
| `nextCursor` | [OpaqueId](rest-schemas.md#opaqueid) | 否 | "未在本schema声明" |  |

对象约束：`additionalProperties=false`

## CapabilityVersionPage

类型：`object`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`
createdAt DESC,id DESC；nextCursor缺省表示最后一页。

| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `items` | `array<CapabilityVersion>` | 是 | "未在本schema声明" |  |
| `nextCursor` | [OpaqueId](rest-schemas.md#opaqueid) | 否 | "未在本schema声明" |  |

对象约束：`additionalProperties=false`

## BuildJobPage

类型：`object`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`
createdAt DESC,id DESC；nextCursor缺省表示最后一页。

| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `items` | `array<BuildJob>` | 是 | "未在本schema声明" |  |
| `nextCursor` | [OpaqueId](rest-schemas.md#opaqueid) | 否 | "未在本schema声明" |  |

对象约束：`additionalProperties=false`

## BuildLogPage

类型：`object`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`
createdAt DESC,id DESC；nextCursor缺省表示最后一页。

| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `items` | `array<BuildLog>` | 是 | "未在本schema声明" |  |
| `nextCursor` | [OpaqueId](rest-schemas.md#opaqueid) | 否 | "未在本schema声明" |  |

对象约束：`additionalProperties=false`

## RuntimeVersionPage

类型：`object`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`
createdAt DESC,id DESC；nextCursor缺省表示最后一页。

| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `items` | `array<RuntimeVersion>` | 是 | "未在本schema声明" |  |
| `nextCursor` | [OpaqueId](rest-schemas.md#opaqueid) | 否 | "未在本schema声明" |  |

对象约束：`additionalProperties=false`

## WebuiVersionPage

类型：`object`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`
createdAt DESC,id DESC；nextCursor缺省表示最后一页。

| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `items` | `array<WebuiVersion>` | 是 | "未在本schema声明" |  |
| `nextCursor` | [OpaqueId](rest-schemas.md#opaqueid) | 否 | "未在本schema声明" |  |

对象约束：`additionalProperties=false`

## ComputePlanPage

类型：`object`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`
createdAt DESC,id DESC；nextCursor缺省表示最后一页。

| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `items` | `array<ComputePlan>` | 是 | "未在本schema声明" |  |
| `nextCursor` | [OpaqueId](rest-schemas.md#opaqueid) | 否 | "未在本schema声明" |  |

对象约束：`additionalProperties=false`

## StoragePlanPage

类型：`object`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`
createdAt DESC,id DESC；nextCursor缺省表示最后一页。

| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `items` | `array<StoragePlan>` | 是 | "未在本schema声明" |  |
| `nextCursor` | [OpaqueId](rest-schemas.md#opaqueid) | 否 | "未在本schema声明" |  |

对象约束：`additionalProperties=false`

## PricePolicyVersionPage

类型：`object`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`
createdAt DESC,id DESC；nextCursor缺省表示最后一页。

| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `items` | `array<PricePolicyVersion>` | 是 | "未在本schema声明" |  |
| `nextCursor` | [OpaqueId](rest-schemas.md#opaqueid) | 否 | "未在本schema声明" |  |

对象约束：`additionalProperties=false`

## RefundPolicyVersionPage

类型：`object`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`
createdAt DESC,id DESC；nextCursor缺省表示最后一页。

| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `items` | `array<RefundPolicyVersion>` | 是 | "未在本schema声明" |  |
| `nextCursor` | [OpaqueId](rest-schemas.md#opaqueid) | 否 | "未在本schema声明" |  |

对象约束：`additionalProperties=false`

## RetentionPolicyVersionPage

类型：`object`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`
createdAt DESC,id DESC；nextCursor缺省表示最后一页。

| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `items` | `array<RetentionPolicyVersion>` | 是 | "未在本schema声明" |  |
| `nextCursor` | [OpaqueId](rest-schemas.md#opaqueid) | 否 | "未在本schema声明" |  |

对象约束：`additionalProperties=false`

## ModelPage

类型：`object`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`
createdAt DESC,id DESC；nextCursor缺省表示最后一页。

| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `items` | `array<Model>` | 是 | "未在本schema声明" |  |
| `nextCursor` | [OpaqueId](rest-schemas.md#opaqueid) | 否 | "未在本schema声明" |  |

对象约束：`additionalProperties=false`

## WorkspacePage

类型：`object`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`
createdAt DESC,id DESC；nextCursor缺省表示最后一页。

| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `items` | `array<Workspace>` | 是 | "未在本schema声明" |  |
| `nextCursor` | [OpaqueId](rest-schemas.md#opaqueid) | 否 | "未在本schema声明" |  |

对象约束：`additionalProperties=false`

## DeploymentPage

类型：`object`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`
createdAt DESC,id DESC；nextCursor缺省表示最后一页。

| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `items` | `array<Deployment>` | 是 | "未在本schema声明" |  |
| `nextCursor` | [OpaqueId](rest-schemas.md#opaqueid) | 否 | "未在本schema声明" |  |

对象约束：`additionalProperties=false`

## WalletOperationPage

类型：`object`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`
createdAt DESC,id DESC；nextCursor缺省表示最后一页。

| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `items` | `array<WalletOperation>` | 是 | "未在本schema声明" |  |
| `nextCursor` | [OpaqueId](rest-schemas.md#opaqueid) | 否 | "未在本schema声明" |  |

对象约束：`additionalProperties=false`

## UsagePage

类型：`object`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`
createdAt DESC,id DESC；nextCursor缺省表示最后一页。

| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `items` | `array<Usage>` | 是 | "未在本schema声明" |  |
| `nextCursor` | [OpaqueId](rest-schemas.md#opaqueid) | 否 | "未在本schema声明" |  |

对象约束：`additionalProperties=false`

## GatewayKeyPage

类型：`object`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`
createdAt DESC,id DESC；nextCursor缺省表示最后一页。

| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `items` | `array<GatewayKey>` | 是 | "未在本schema声明" |  |
| `nextCursor` | [OpaqueId](rest-schemas.md#opaqueid) | 否 | "未在本schema声明" |  |

对象约束：`additionalProperties=false`

## TenantPage

类型：`object`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`
createdAt DESC,id DESC；nextCursor缺省表示最后一页。

| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `items` | `array<Tenant>` | 是 | "未在本schema声明" |  |
| `nextCursor` | [OpaqueId](rest-schemas.md#opaqueid) | 否 | "未在本schema声明" |  |

对象约束：`additionalProperties=false`

## AuditEventPage

类型：`object`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`
createdAt DESC,id DESC；nextCursor缺省表示最后一页。

| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `items` | `array<AuditEvent>` | 是 | "未在本schema声明" |  |
| `nextCursor` | [OpaqueId](rest-schemas.md#opaqueid) | 否 | "未在本schema声明" |  |

对象约束：`additionalProperties=false`

## ReceiptPage

类型：`object`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`
createdAt DESC,id DESC；nextCursor缺省表示最后一页。

| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `items` | `array<Receipt>` | 是 | "未在本schema声明" |  |
| `nextCursor` | [OpaqueId](rest-schemas.md#opaqueid) | 否 | "未在本schema声明" |  |

对象约束：`additionalProperties=false`

## AdminOperationPage

类型：`object`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`
createdAt DESC,id DESC；nextCursor缺省表示最后一页。

| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `items` | `array<AdminOperation>` | 是 | "未在本schema声明" |  |
| `nextCursor` | [OpaqueId](rest-schemas.md#opaqueid) | 否 | "未在本schema声明" |  |

对象约束：`additionalProperties=false`

## QualificationPage

类型：`object`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`
createdAt DESC,id DESC；nextCursor缺省表示最后一页。

| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `items` | `array<Qualification>` | 是 | "未在本schema声明" |  |
| `nextCursor` | [OpaqueId](rest-schemas.md#opaqueid) | 否 | "未在本schema声明" |  |

对象约束：`additionalProperties=false`

## AdoptWorkspaceRequest

类型：`object`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`
只用于已迁移legacy_resource_only Workspace；检查当前已付套餐/资源/数据兼容，不创建报价扣费、不重新采购。

| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `capabilityVersionId` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "未在本schema声明" |  |
| `expectedWorkspaceVersion` | [NonnegativeInt64](rest-schemas.md#nonnegativeint64) | 是 | "未在本schema声明" |  |
| `modelSelections` | `array<ModelSelection>` | 是 | "未在本schema声明" |  |

对象约束：`additionalProperties=false`

## OperationKind

类型：`string`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`



对象约束：`enum=["complete_upload","build","delete_capability_version","create_workspace","update_models","update_workspace","rollback_workspace","resize_workspace","renew_workspace","delete_workspace","create_tenant","bind_tenant_wallet","suspend_tenant","delete_tenant","restore_tenant","revoke_key","reconcile","adopt_workspace","resource_provision","resource_resize","resource_renew","resource_suspend","resource_resume","resource_delete","runtime_deploy","runtime_reload","runtime_retire","reenable_tenant","migration","update_renewal_settings","apply_scheduled_plan_change","cancel_plan_change","compensate_plan_change","refund_supplement_on_delete"]`

## OperationStage

类型：`string`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`



对象约束：`enum=["absence_verification","access_enablement","access_revocation","activation","actual_resource_evidence","admission","asset_custody","attachment","attachment_deletion","building","cancellation_guard","catalog_tombstone","compatibility","compute","compute_deletion","configuration","consent_commit","consent_validation","coverage_calculation","debit","deletion_evidence","failure_fence","grant_revocation","identity_verification","key","key_revocation","membership","obligation_check","original_action_readback","owner_switch","payment_authorization","period_boundary","period_update","plan_change_commit","plan_commit","provider_renewal","provider_resume","provider_suspend","pushing","queued","quote_binding","readback","receipt","reconciliation","reference_check","refund","refund_original_charge","registering","reload","resource_preflight","restore_window_check","retirement","runtime","runtime_deletion","schedule_cancel","schedule_commit","secret_unbinding","snapshot_import","source_verification","storage","storage_deletion","succeeded","supplement_payment","target_period_payment","tenant_state_check","upload_verification","validating","verification","wallet_binding","workspace_deletion","workspace_resumption","workspace_suspension","write_barrier"]`

## BuildRuntimePolicy

类型：`object`；Owner：`capability`；表：`capability.catalog_policies`


| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `id` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "capability.catalog_policies.id" |  |
| `runtimeVersionId` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "capability.catalog_policies.runtime_version_id" |  |
| `defaultWebuiVersionId` | [OpaqueId](rest-schemas.md#opaqueid) | 否 | "capability.catalog_policies.default_webui_version_id" |  |
| `policyVersion` | `string` | 是 | "capability.catalog_policies.policy_version" |  |
| `effectiveAt` | `string/date-time` | 是 | "capability.catalog_policies.effective_at" |  |
| `createdAt` | `string/date-time` | 是 | "capability.catalog_policies.created_at" |  |

对象约束：`additionalProperties=false`

## SetBuildRuntimePolicyRequest

类型：`object`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`
首次尚无策略时expectedPolicyVersionId可缺省；已有策略必须匹配当前id否则VERSION_CONFLICT。只允许approved兼容Runtime/WebUI；生效于新Build，不改变已有输入/运行。

| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `runtimeVersionId` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "未在本schema声明" |  |
| `defaultWebuiVersionId` | [OpaqueId](rest-schemas.md#opaqueid) | 否 | "未在本schema声明" |  |
| `expectedPolicyVersionId` | [OpaqueId](rest-schemas.md#opaqueid) | 否 | "未在本schema声明" |  |

对象约束：`additionalProperties=false`

## OperationOwner

类型：`string`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`



对象约束：`enum=["tenant","capability","build","workspace","runtime_control","fabric","gateway","resource_catalog"]`

## ImagePlatform

类型：`object`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`


| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `os` | `string` | 是 | "未在本schema声明" | enum=["linux"] |
| `architecture` | `string` | 是 | "未在本schema声明" | enum=["amd64","arm64"] |
| `variant` | `string` | 否 | "未在本schema声明" | minLength=1; maxLength=256 |

对象约束：`additionalProperties=false`

## ArtifactReference

类型：`object`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`
可定位不可变OCI image身份；读取manifest必须精确匹配三个字段，不用latest或静默跨平台。

| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `repository` | `string` | 是 | "未在本schema声明" | maxLength=512; pattern="^[a-z0-9]+(?:[.-][a-z0-9]+)*(?::[0-9]+)?/[a-z0-9]+(?:[._/-][a-z0-9]+)*$" |
| `digest` | `string` | 是 | "未在本schema声明" | pattern="^sha256:[0-9a-f]{64}$" |
| `platform` | [ImagePlatform](rest-schemas.md#imageplatform) | 是 | "未在本schema声明" |  |

对象约束：`additionalProperties=false`

## RecipeArtifact

类型：`object`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`
由发布者准入的不可变BuildKit recipe归档，不是客户任意Dockerfile；校验归档路径/recipe摘要。

| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `repository` | `string` | 是 | "未在本schema声明" | maxLength=512; pattern="^[a-z0-9]+(?:[.-][a-z0-9]+)*(?::[0-9]+)?/[a-z0-9]+(?:[._/-][a-z0-9]+)*$" |
| `digest` | `string` | 是 | "未在本schema声明" | pattern="^sha256:[0-9a-f]{64}$" |
| `mediaType` | `string` | 是 | "未在本schema声明" | enum=["application/vnd.opl.build-recipe.v1+tar"] |
| `dockerfilePath` | `string` | 是 | "未在本schema声明" | maxLength=512; pattern="^(?!/)(?!.*(?:^\|/)\\.\\.?(/\|$))[A-Za-z0-9_./-]+$" |

对象约束：`additionalProperties=false`

## PackageBuildInput

类型：`object`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`


| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `contextName` | `string` | 是 | "未在本schema声明" | enum=["agent_package"] |
| `formatVersion` | `string` | 是 | "未在本schema声明" | minLength=1; maxLength=256 |
| `sourceRoot` | `string` | 是 | "未在本schema声明" | maxLength=512; pattern="^(?!/)(?!.*(?:^\|/)\\.\\.?(/\|$))[A-Za-z0-9_./-]+$" |
| `targetPath` | `string` | 是 | "未在本schema声明" | maxLength=512; pattern="^/(?!.*(?:^\|/)\\.\\.(?:/\|$))[A-Za-z0-9_./-]*$" |
| `uid` | `integer` | 是 | "未在本schema声明" | minimum=0; maximum=2147483647 |
| `gid` | `integer` | 是 | "未在本schema声明" | minimum=0; maximum=2147483647 |

对象约束：`additionalProperties=false`

## WebuiBuildInput

类型：`object`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`


| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `contextName` | `string` | 是 | "未在本schema声明" | enum=["webui"] |
| `sourcePath` | `string` | 是 | "未在本schema声明" | maxLength=512; pattern="^/(?!.*(?:^\|/)\\.\\.(?:/\|$))[A-Za-z0-9_./-]*$" |
| `targetPath` | `string` | 是 | "未在本schema声明" | maxLength=512; pattern="^/(?!.*(?:^\|/)\\.\\.(?:/\|$))[A-Za-z0-9_./-]*$" |
| `uid` | `integer` | 是 | "未在本schema声明" | minimum=0; maximum=2147483647 |
| `gid` | `integer` | 是 | "未在本schema声明" | minimum=0; maximum=2147483647 |

对象约束：`additionalProperties=false`

## BuildRecipeContract

类型：`object`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`
BuildKit三个命名context严格runtime/agent_package/webui；发布者recipe只能消费这些输入，网络none。需要联网依赖须由发布者预置在已批准digest中。输出平台必须等于Runtime与WebUI平台；准入执行官方/第三方契约测试，不推测COPY路径。

| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `version` | `string` | 是 | "未在本schema声明" | enum=["opl-build-recipe/v1"] |
| `frontend` | [ArtifactReference](rest-schemas.md#artifactreference) | 是 | "未在本schema声明" |  |
| `recipe` | [RecipeArtifact](rest-schemas.md#recipeartifact) | 是 | "未在本schema声明" |  |
| `runtimeContextName` | `string` | 是 | "未在本schema声明" | enum=["runtime"] |
| `packageInput` | [PackageBuildInput](rest-schemas.md#packagebuildinput) | 是 | "未在本schema声明" |  |
| `webuiInput` | [WebuiBuildInput](rest-schemas.md#webuibuildinput) | 是 | "未在本schema声明" |  |
| `networkPolicy` | `string` | 是 | "未在本schema声明" | enum=["none"] |
| `outputPlatform` | [ImagePlatform](rest-schemas.md#imageplatform) | 是 | "未在本schema声明" |  |
| `outputImageCommand` | [BuildRecipeContractOutputImageCommand](rest-schemas.md#buildrecipecontractoutputimagecommand) | 是 | "未在本schema声明" |  |

对象约束：`additionalProperties=false`

## ModelConfigurationContract

类型：`object`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`
PUT applyPath提交ModelConfigurationPayload{version:int64 string,selections:[{slot,modelId}]}，GET readbackPath返回ModelConfigurationReadback；重复同version同payload返回同结果，异payload冲突；不能借此写应用任意JSON。 portName引用applicationRevisionTemplate.ports；authorizationSecretInputName引用其SecretInputs，不另发明SecretSlot或用Gateway Key代替控制凭据。

| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `protocol` | `string` | 是 | "未在本schema声明" | enum=["opl-model-config/v1"] |
| `portName` | `string` | 是 | "未在本schema声明" | minLength=1; maxLength=256 |
| `applyPath` | `string` | 是 | "未在本schema声明" | maxLength=512; pattern="^/(?!.*(?:^\|/)\\.\\.(?:/\|$))[A-Za-z0-9_./-]*$" |
| `readbackPath` | `string` | 是 | "未在本schema声明" | maxLength=512; pattern="^/(?!.*(?:^\|/)\\.\\.(?:/\|$))[A-Za-z0-9_./-]*$" |
| `requestFields` | `array<string>` | 是 | "未在本schema声明" | minItems=2; maxItems=2; uniqueItems=true |
| `readbackFields` | `array<string>` | 是 | "未在本schema声明" | minItems=2; maxItems=2; uniqueItems=true |
| `authorizationSecretInputName` | `string` | 是 | "未在本schema声明" | minLength=1; maxLength=256 |

对象约束：`additionalProperties=false`

## ApplicationAccessContract

类型：`composition`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`



对象约束：`oneOf=[{"$ref":"#/components/schemas/ApplicationOwnedAccessContract"},{"$ref":"#/components/schemas/CloudPrivateAccessContract"},{"$ref":"#/components/schemas/AnonymousAccessContract"}]`

## DataUpgradeContract

类型：`object`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`


| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `mode` | `string` | 是 | "未在本schema声明" | enum=["compatible","publisher_migration"] |
| `compatibleFromSchemaVersions` | `array<string>` | 是 | "未在本schema声明" |  |
| `migrationArtifact` | [ArtifactReference](rest-schemas.md#artifactreference) | 否 | "未在本schema声明" |  |
| `backupRequired` | `boolean` | 是 | "未在本schema声明" |  |
| `backupFormatVersion` | `string` | 否 | "未在本schema声明" | minLength=1; maxLength=256 |

对象约束：`additionalProperties=false; allOf=[{"oneOf":[{"allOf":[{"properties":{"mode":{"enum":["publisher_migration"]}}},{"required":["migrationArtifact","backupFormatVersion"],"properties":{"backupRequired":{"enum":[true]}}}]},{"not":{"properties":{"mode":{"enum":["publisher_migration"]}}}}]}]`

## DataRollbackContract

类型：`object`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`


| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `safe` | `boolean` | 是 | "未在本schema声明" |  |
| `compatibleSchemaVersions` | `array<string>` | 是 | "未在本schema声明" |  |

对象约束：`additionalProperties=false`

## DataContract

类型：`object`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`
数据schema由发布者拥有；存在read_write且不支持并发的mount时更新必须停止旧writer再挂新实例，不能双写。迁移/备份是明确artifact与格式，不启发式转换。

| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `schemaVersion` | `string` | 是 | "未在本schema声明" | minLength=1; maxLength=256 |
| `upgrade` | [DataUpgradeContract](rest-schemas.md#dataupgradecontract) | 是 | "未在本schema声明" |  |
| `rollback` | [DataRollbackContract](rest-schemas.md#datarollbackcontract) | 是 | "未在本schema声明" |  |
| `mountPolicies` | `array<DataMountPolicy>` | 是 | "未在本schema声明" |  |

对象约束：`additionalProperties=false`

## RuntimePublisherContract

类型：`object`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`


| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `schemaVersion` | `string` | 是 | "未在本schema声明" | enum=["opl-publisher-contract/v1"] |
| `publisherNamespaceId` | `string` | 是 | "未在本schema声明" | minLength=1; maxLength=256 |
| `image` | [ArtifactReference](rest-schemas.md#artifactreference) | 是 | "未在本schema声明" |  |
| `kind` | `string` | 是 | "未在本schema声明" | enum=["runtime"] |
| `runtimeAbiVersion` | `string` | 是 | "未在本schema声明" | minLength=1; maxLength=256 |
| `packageFormatVersions` | `array<string>` | 是 | "未在本schema声明" |  |
| `buildRecipe` | [BuildRecipeContract](rest-schemas.md#buildrecipecontract) | 是 | "未在本schema声明" |  |
| `modelConfiguration` | [ModelConfigurationContract](rest-schemas.md#modelconfigurationcontract) | 是 | "未在本schema声明" |  |
| `applicationAccess` | [ApplicationAccessContract](rest-schemas.md#applicationaccesscontract) | 是 | "未在本schema声明" |  |
| `data` | [DataContract](rest-schemas.md#datacontract) | 是 | "未在本schema声明" |  |
| `applicationRevisionTemplate` | [WorkspaceApplicationRevision](rest-schemas.md#workspaceapplicationrevision) | 是 | "未在本schema声明" |  |
| `packageFormatContracts` | `array<PackageFormatContractReference>` | 是 | "未在本schema声明" | minItems=1 |

对象约束：`additionalProperties=false`

## WebuiPublisherContract

类型：`object`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`
本期WebUI合入单一OCI；不支持独立服务WebUI，必须显式拒绝而非自建服务。staticRoot由Runtime buildRecipe.webuiInput.sourcePath逐字段相等；entryFile必须存在，路由/静态/SSE测试通过。

| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `schemaVersion` | `string` | 是 | "未在本schema声明" | enum=["opl-publisher-contract/v1"] |
| `publisherNamespaceId` | `string` | 是 | "未在本schema声明" | minLength=1; maxLength=256 |
| `image` | [ArtifactReference](rest-schemas.md#artifactreference) | 是 | "未在本schema声明" |  |
| `kind` | `string` | 是 | "未在本schema声明" | enum=["webui"] |
| `runtimeAbiVersions` | `array<string>` | 是 | "未在本schema声明" |  |
| `uiProtocolVersion` | `string` | 是 | "未在本schema声明" | enum=["opl-webui/v1"] |
| `integrationMode` | `string` | 是 | "未在本schema声明" | enum=["bundled_static"] |
| `staticRoot` | `string` | 是 | "未在本schema声明" | maxLength=512; pattern="^/(?!.*(?:^\|/)\\.\\.(?:/\|$))[A-Za-z0-9_./-]*$" |
| `entryFile` | `string` | 是 | "未在本schema声明" | maxLength=512; pattern="^(?!/)(?!.*(?:^\|/)\\.\\.?(/\|$))[A-Za-z0-9_./-]+$" |
| `assetBasePath` | `string` | 是 | "未在本schema声明" | maxLength=512; pattern="^/(?!.*(?:^\|/)\\.\\.(?:/\|$))[A-Za-z0-9_./-]*$" |
| `apiBasePath` | `string` | 是 | "未在本schema声明" | maxLength=512; pattern="^/(?!.*(?:^\|/)\\.\\.(?:/\|$))[A-Za-z0-9_./-]*$" |
| `authenticationProtocol` | `string` | 是 | "未在本schema声明" | enum=["opl-application-session/v1"] |
| `supportsSse` | `boolean` | 是 | "未在本schema声明" |  |
| `supportsWebsocket` | `boolean` | 是 | "未在本schema声明" |  |

对象约束：`additionalProperties=false`

## PublisherContract

类型：`composition`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`



对象约束：`oneOf=[{"$ref":"#/components/schemas/RuntimePublisherContract"},{"$ref":"#/components/schemas/WebuiPublisherContract"}]`

## PublisherContractReference

类型：`object`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`
Capability typed解析后编码一次并把确切UTF-8 bytes写入不可变Storage对象，计算SHA256；digest/objectRef都由Owner返回，客户端不报。JSONB仅结构投影，Resolver校验存储原字节，禁止重新序列化JSONB猜摘要。

| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `publisherNamespaceId` | `string` | 是 | "未在本schema声明" | minLength=1; maxLength=256 |
| `versionId` | `string` | 是 | "未在本schema声明" | minLength=1; maxLength=256 |
| `kind` | `string` | 是 | "未在本schema声明" | enum=["runtime","webui"] |
| `descriptorDigest` | `string` | 是 | "未在本schema声明" | pattern="^sha256:[0-9a-f]{64}$" |
| `descriptorObjectRef` | `string` | 是 | "未在本schema声明" | minLength=1; maxLength=256 |

对象约束：`additionalProperties=false`

## DeploymentDescriptor

类型：`object`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`
Build产物不可变快照：applicationRevision从Runtime template逐字段复制，只按Build批准转换替换image/version/applicationId及recipe声明的输入绑定；其余变动一律重新发布契约。运行只消费applicationRevision，不重复解释template执行字段。legacy直接保留既有canonical revision，不改路径/Key/入口。 legacy_application分支只保留artifact+applicationRevision+旧revision ID；禁止伪造Runtime/WebUI发布描述、Package或recipe。

| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `schemaVersion` | `string` | 是 | "未在本schema声明" | enum=["opl-deployment-descriptor/v1"] |
| `artifact` | [ArtifactReference](rest-schemas.md#artifactreference) | 是 | "未在本schema声明" |  |
| `runtimeContract` | [RuntimePublisherContract](rest-schemas.md#runtimepublishercontract) | 否 | "未在本schema声明" |  |
| `runtimeContractReference` | [PublisherContractReference](rest-schemas.md#publishercontractreference) | 否 | "未在本schema声明" |  |
| `webuiContract` | [WebuiPublisherContract](rest-schemas.md#webuipublishercontract) | 否 | "未在本schema声明" |  |
| `webuiContractReference` | [PublisherContractReference](rest-schemas.md#publishercontractreference) | 否 | "未在本schema声明" |  |
| `packageVersionId` | `string` | 否 | "未在本schema声明" | minLength=1; maxLength=256 |
| `buildInputDigest` | `string` | 否 | "未在本schema声明" | pattern="^sha256:[0-9a-f]{64}$" |
| `provenance` | `string` | 是 | "未在本schema声明" | enum=["build","legacy_application"] |
| `legacyApplicationRevisionId` | `string` | 否 | "未在本schema声明" | minLength=1; maxLength=256 |
| `applicationRevision` | [WorkspaceApplicationRevision](rest-schemas.md#workspaceapplicationrevision) | 是 | "未在本schema声明" |  |

对象约束：`additionalProperties=false; oneOf=[{"properties":{"provenance":{"enum":["build"]}},"required":["runtimeContract","runtimeContractReference","webuiContract","webuiContractReference","packageVersionId","buildInputDigest"],"not":{"required":["legacyApplicationRevisionId"]}},{"properties":{"provenance":{"enum":["legacy_application"]}},"required":["legacyApplicationRevisionId"],"not":{"anyOf":[{"required":["runtimeContract"]},{"required":["runtimeContractReference"]},{"required":["webuiContract"]},{"required":["webuiContractReference"]},{"required":["packageVersionId"]},{"required":["buildInputDigest"]}]}}]`

## PublisherNamespace

类型：`object`；Owner：`capability`；表：`capability.publisher_namespaces`


| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `id` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "capability.publisher_namespaces.id" |  |
| `name` | `string` | 是 | "capability.publisher_namespaces.name" | minLength=1; maxLength=256 |
| `kind` | `string` | 是 | "capability.publisher_namespaces.kind" | enum=["official","third_party"] |
| `registryId` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "capability.publisher_namespaces.registry_id" |  |
| `repositoryPrefix` | `string` | 是 | "capability.publisher_namespaces.repository_prefix" | maxLength=512; pattern="^[a-z0-9]+(?:[.-][a-z0-9]+)*(?::[0-9]+)?/[a-z0-9]+(?:[._/-][a-z0-9]+)*$" |
| `admissionReceiptId` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "capability.publisher_namespaces.admission_receipt_id" |  |
| `status` | `string` | 是 | "capability.publisher_namespaces.status" | enum=["approved","revoked"] |
| `createdAt` | `string/date-time` | 是 | "capability.publisher_namespaces.created_at" |  |

对象约束：`additionalProperties=false`

## CreatePublisherNamespaceRequest

类型：`object`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`
仅platform_admin；Registry配置与前缀由Instance已批准registryId验证。official/third_party不可混用，前缀发布后不可改变。

| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `name` | `string` | 是 | "未在本schema声明" | minLength=1; maxLength=256 |
| `kind` | `string` | 是 | "未在本schema声明" | enum=["official","third_party"] |
| `registryId` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "未在本schema声明" |  |
| `repositoryPrefix` | `string` | 是 | "未在本schema声明" | maxLength=512; pattern="^[a-z0-9]+(?:[.-][a-z0-9]+)*(?::[0-9]+)?/[a-z0-9]+(?:[._/-][a-z0-9]+)*$" |
| `admissionReceiptId` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "未在本schema声明" |  |

对象约束：`additionalProperties=false`

## RevokePublisherNamespaceRequest

类型：`object`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`


| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `reason` | `string` | 是 | "未在本schema声明" | minLength=1; maxLength=1024 |

对象约束：`additionalProperties=false`

## PublisherNamespacePage

类型：`object`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`


| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `items` | `array<PublisherNamespace>` | 是 | "未在本schema声明" |  |
| `nextCursor` | [OpaqueId](rest-schemas.md#opaqueid) | 否 | "未在本schema声明" |  |

对象约束：`additionalProperties=false`

## ReenableTenantRequest

类型：`object`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`
仅suspended→active；先恢复Tenant访问，再由Workspace owner仅恢复原Tenant停用Operation暂停、仍在已付周期且原资源确定存在的Workspace；不自动续费、采购、重建或恢复其他原因的suspended/deleted。子操作通过getTenantLifecycleOperation展示。

| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `reason` | `string` | 是 | "未在本schema声明" | minLength=1; maxLength=1024 |

对象约束：`additionalProperties=false`

## RenewalPolicy

类型：`object`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`
手动与已明确授权自动续费共享同subscription+period业务幂等身份；新用户默认manual，继承旧automatic不得降级。具体新周期金额必须与有效授权政策和accepted quote相符，不在worker暗中涨价。 当前owner规则：以原paidThrough和billingAnchorDay计算nextBillingMonth；now>=renewedThrough拒绝RENEWAL_PERIOD_ELAPSED，不按恢复时间加整月、不替用户补买。Quote必须展示确切原起止和剩余可用窗口。

| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `version` | `string` | 是 | "未在本schema声明" | enum=["renewal-policy/v1"] |
| `trigger` | `string` | 是 | "未在本schema声明" | enum=["manual_or_explicitly_consented_automatic"] |
| `effectiveStart` | `string` | 是 | "未在本schema声明" | enum=["previous_paid_through"] |
| `months` | `integer` | 是 | "未在本schema声明" | enum=[1] |
| `usesAcceptedPriceSnapshot` | `boolean` | 是 | "未在本schema声明" | enum=[true] |

对象约束：`additionalProperties=false`

## AuthorizationAction

类型：`string`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`



对象约束：`enum=["acceptInvitation","acquireReference","activateRoute","adoptWorkspace","appendPlanChangeEvidence","appendReceipt","archiveNamespace","archivePackage","bindManagedSecret","bindReference","bindTenantWallet","cancelAcceptedObligation","cancelPlanChange","chargeAcceptedObligation","chargePlanChangeSupplement","claimNextPeriodObligation","closeoutAcceptedObligation","completeAcceptedObligation","completeUpload","createBuild","createComputePlan","createGatewayKey","createNamespace","createPackage","createPricePolicyVersion","createPublisherNamespace","createQuote","createRefundPolicyVersion","createRetentionPolicyVersion","createStoragePlan","createTenant","createUpload","createUploadPart","createWorkspace","deleteCapabilityVersion","deleteTenant","deleteWorkspace","executeScheduledPlanChange","fenceRouteEpoch","getAdminTenant","getBuild","getBuildRuntimePolicy","getCapabilityVersion","getDeployment","getLoginContext","getOperation","getPackage","getPackageVersion","getPlanChange","getQuote","getReceipt","getSession","getSubscription","getTenant","getTenantAssetCustody","getTenantLifecycleOperation","getUpload","getWallet","getWorkspace","getWorkspaceAccess","getWorkspaceDeletion","getWorkspaceModels","inviteMember","issueAcceptedOperationGrant","listAdminOperations","listAuditEvents","listBuildLogs","listBuilds","listCapabilityVersions","listComputePlans","listDeployments","listGatewayKeys","listInvitations","listMembers","listModels","listNamespaces","listPackageVersions","listPackages","listPlanChanges","listPricePolicyVersions","listPublisherNamespaces","listQualifications","listReceipts","listRechargeRecords","listRefundPolicyVersions","listRetentionPolicyVersions","listRuntimeVersions","listStoragePlans","listTenants","listUsage","listWebuiVersions","listWorkspaceTransactions","listWorkspaces","login","logout","observeResources","observeRoute","provisionAcceptedResources","publishOfficialPackage","readApplicationCredentials","readApprovedPlanTransition","readArtifact","readAuthorizationContext","readClaimUsage","readNextPeriodObligation","readOwnerCommit","readRenewalConsent","readSubscriptionPlanState","readWalletAction","reconcileOperation","reenableTenant","refundConfirmedDeletion","refundPlanChangeCompensation","refundSupplementOnDeletion","registerRuntimeVersion","registerWebuiVersion","releaseReference","removeMember","renewAcceptedResources","renewWorkspace","reserveRuntime","resizeAcceptedResources","resizeWorkspace","resolveBuildInput","resolvePublisherContract","restoreTenant","resumeTenantWorkspaces","retireRuntime","retryBuild","revealGatewayKey","revealWorkspaceApplicationCredentials","revokeGatewayKey","revokeInvitation","revokePublisherNamespace","rollbackRoute","rollbackWorkspace","setBuildRuntimePolicy","setComputePlanAvailability","setRuntimeVersionStatus","setStoragePlanAvailability","setWebuiVersionStatus","suspendTenant","updateMemberRole","updateNamespace","updatePackage","updateRenewalSettings","updateWorkspaceModels","updateWorkspaceVersion","readExecutionPlan","restoreAfterResourceChange","debitScheduledPeriod","readPlanChangeFailure"]`

## ServiceIdentity

类型：`string`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`



对象约束：`enum=["console_bff","capability","build","workspace","runtime_control","fabric","gateway_integration","resource_catalog","ledger"]`

## WorkspaceApplicationExecution

类型：`object`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`


| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `userId` | `integer` | 否 | "未在本schema声明" | minimum=1; maximum=2147483647 |
| `groupId` | `integer` | 否 | "未在本schema声明" | minimum=0; maximum=2147483647 |
| `init` | `boolean` | 否 | "未在本schema声明" |  |
| `seccompProfile` | `string` | 否 | "未在本schema声明" | pattern="^sha256:[0-9a-f]{64}$" |

对象约束：`additionalProperties=false`

x-canonical-source："packages/contracts/go/workspace_application_execution.go#WorkspaceApplicationExecution"

## WorkspaceApplicationCompute

类型：`object`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`


| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `cpuRequestMilli` | `integer` | 否 | "未在本schema声明" | minimum=0; maximum=128000 |
| `cpuLimitMilli` | `integer` | 否 | "未在本schema声明" | minimum=0; maximum=128000 |
| `memoryRequestBytes` | `integer` | 否 | "未在本schema声明" | minimum=0; maximum=1099511627776 |
| `memoryLimitBytes` | `integer` | 否 | "未在本schema声明" | minimum=0; maximum=1099511627776 |

对象约束：`additionalProperties=false`

x-canonical-source："packages/contracts/go/workspace_application.go#WorkspaceApplicationCompute"

## WorkspaceApplicationCredential

类型：`object`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`


| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `name` | `string` | 是 | "未在本schema声明" | minLength=1; maxLength=256; pattern="^[a-z][a-z0-9-]{0,30}$" |
| `kind` | `string` | 是 | "未在本schema声明" | enum=["workspace_admin_password","workspace_session_secret","gateway_key"] |
| `target` | `string` | 是 | "未在本schema声明" | pattern="^/(?!.*(?:^\|/)\\.\\.(?:/\|$))[A-Za-z0-9_./-]*$" |
| `env` | `string` | 否 | "未在本schema声明" | pattern="^[A-Za-z_][A-Za-z0-9_.-]*$" |
| `username` | `string` | 否 | "未在本schema声明" | minLength=1; maxLength=256; pattern="^[a-zA-Z0-9][a-zA-Z0-9._@-]{0,63}$" |

对象约束：`additionalProperties=false`

x-canonical-source："packages/contracts/go/workspace_application.go#WorkspaceApplicationCredential"

## WorkspaceApplicationPort

类型：`object`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`


| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `name` | `string` | 是 | "未在本schema声明" | minLength=1; maxLength=256; pattern="^[a-z][a-z0-9-]{0,14}$" |
| `port` | `integer` | 是 | "未在本schema声明" | minimum=1; maximum=65535 |
| `protocol` | `string` | 是 | "未在本schema声明" | enum=["TCP","UDP"] |

对象约束：`additionalProperties=false`

x-canonical-source："packages/contracts/go/workspace_application.go#WorkspaceApplicationPort"

## WorkspaceApplicationHealthCheck

类型：`object`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`


| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `port` | `integer` | 是 | "未在本schema声明" | minimum=1; maximum=65535 |
| `path` | `string` | 是 | "未在本schema声明" | pattern="^/(?!.*(?:^\|/)\\.\\.(?:/\|$))[A-Za-z0-9_./-]*$" |
| `initialDelaySeconds` | `integer` | 否 | "未在本schema声明" | minimum=0; maximum=2147483647 |

对象约束：`additionalProperties=false`

x-canonical-source："packages/contracts/go/workspace_application.go#WorkspaceApplicationHealthCheck"

## WorkspaceApplicationMount

类型：`object`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`


| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `name` | `string` | 是 | "未在本schema声明" | minLength=1; maxLength=256 |
| `mountPath` | `string` | 是 | "未在本schema声明" | pattern="^/(?!.*(?:^\|/)\\.\\.(?:/\|$))[A-Za-z0-9_./-]*$" |
| `readOnly` | `boolean` | 否 | "未在本schema声明" |  |
| `mode` | `integer` | 否 | "未在本schema声明" | minimum=0; maximum=4095 |
| `userId` | `integer` | 否 | "未在本schema声明" | minimum=0; maximum=2147483647 |
| `groupId` | `integer` | 否 | "未在本schema声明" | minimum=0; maximum=2147483647 |
| `sizeBytes` | `integer` | 否 | "未在本schema声明" | minimum=0; maximum=1099511627776 |
| `executable` | `boolean` | 否 | "未在本schema声明" |  |

对象约束：`additionalProperties=false`

x-canonical-source："packages/contracts/go/workspace_application.go#WorkspaceApplicationMount"

## WorkspaceApplicationSecretInput

类型：`object`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`


| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `name` | `string` | 是 | "未在本schema声明" | minLength=1; maxLength=256 |
| `target` | `string` | 否 | "未在本schema声明" | pattern="^/(?!.*(?:^\|/)\\.\\.(?:/\|$))[A-Za-z0-9_./-]*$" |
| `env` | `string` | 否 | "未在本schema声明" | pattern="^[A-Za-z_][A-Za-z0-9_.-]*$" |

对象约束：`additionalProperties=false; oneOf=[{"required":["target"],"not":{"required":["env"]}},{"required":["env"],"not":{"required":["target"]}}]`

x-canonical-source："packages/contracts/go/workspace_application.go#WorkspaceApplicationSecretInput"

## WorkspaceApplicationConfigInput

类型：`object`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`


| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `name` | `string` | 是 | "未在本schema声明" | minLength=1; maxLength=256 |
| `target` | `string` | 是 | "未在本schema声明" | pattern="^/(?!.*(?:^\|/)\\.\\.(?:/\|$))[A-Za-z0-9_./-]*$" |

对象约束：`additionalProperties=false`

x-canonical-source："packages/contracts/go/workspace_application.go#WorkspaceApplicationConfigInput"

## WorkspaceApplicationDependencyCommand

类型：`object`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`
源Go contract允许的明确environment string map，不是任意domain JSON；Secret值禁止进入env，必须SecretInputs。

| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `entrypoint` | `array<string>` | 否 | "未在本schema声明" |  |
| `args` | `array<string>` | 否 | "未在本schema声明" |  |
| `env` | `object` | 否 | "未在本schema声明" | additionalProperties={"type":"string","minLength":0,"maxLength":4096} |

对象约束：`additionalProperties=false`

x-canonical-source："packages/contracts/go/workspace_application_dependency.go#WorkspaceApplicationDependencyCommand"

## WorkspaceApplicationDependencyHealthCheck

类型：`object`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`


| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `type` | `string` | 是 | "未在本schema声明" | enum=["tcp","http","exec"] |
| `port` | `integer` | 是 | "未在本schema声明" | minimum=0; maximum=65535 |
| `path` | `string` | 否 | "未在本schema声明" | pattern="^/(?!.*(?:^\|/)\\.\\.(?:/\|$))[A-Za-z0-9_./-]*$" |
| `command` | `array<string>` | 否 | "未在本schema声明" |  |
| `initialDelaySeconds` | `integer` | 否 | "未在本schema声明" | minimum=0; maximum=2147483647 |

对象约束：`additionalProperties=false; oneOf=[{"properties":{"type":{"enum":["exec"]},"port":{"enum":[0]},"command":{"minItems":1,"maxItems":32}},"required":["command"],"not":{"required":["path"]}},{"properties":{"type":{"enum":["tcp"]},"port":{"minimum":1}},"not":{"anyOf":[{"required":["path"]},{"required":["command"]}]}},{"properties":{"type":{"enum":["http"]},"port":{"minimum":1}},"required":["path"],"not":{"required":["command"]}}]`

x-canonical-source："packages/contracts/go/workspace_application_dependency.go#WorkspaceApplicationDependencyHealthCheck"

## WorkspaceApplicationDependency

类型：`object`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`


| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `name` | `string` | 是 | "未在本schema声明" | minLength=1; maxLength=256; pattern="^[a-z]([a-z0-9-]{0,61}[a-z0-9])?$" |
| `image` | `string` | 是 | "未在本schema声明" | pattern="^[^@]+@sha256:[0-9a-f]{64}$" |
| `execution` | [WorkspaceApplicationExecution](rest-schemas.md#workspaceapplicationexecution) | 否 | "未在本schema声明" |  |
| `dependsOn` | `array<string>` | 否 | "未在本schema声明" |  |
| `ports` | `array<WorkspaceApplicationPort>` | 否 | "未在本schema声明" |  |
| `healthChecks` | `array<WorkspaceApplicationDependencyHealthCheck>` | 否 | "未在本schema声明" |  |
| `persistentMounts` | `array<WorkspaceApplicationMount>` | 否 | "未在本schema声明" |  |
| `scratchMounts` | `array<WorkspaceApplicationMount>` | 否 | "未在本schema声明" |  |
| `command` | [WorkspaceApplicationDependencyCommand](rest-schemas.md#workspaceapplicationdependencycommand) | 否 | "未在本schema声明" |  |
| `secretInputs` | `array<WorkspaceApplicationSecretInput>` | 否 | "未在本schema声明" |  |
| `configInputs` | `array<WorkspaceApplicationConfigInput>` | 否 | "未在本schema声明" |  |
| `compute` | [WorkspaceApplicationCompute](rest-schemas.md#workspaceapplicationcompute) | 否 | "未在本schema声明" |  |

对象约束：`additionalProperties=false`

x-canonical-source："packages/contracts/go/workspace_application.go#WorkspaceApplicationDependency"

## WorkspaceApplicationRevision

类型：`object`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`
现有packages/contracts/go/workspace_application.go WorkspaceApplicationRevision的wire投影，不是平行应用格式。所有新输入必须同时通过当前Go ValidateWorkspaceApplicationRevision/StartupOrder/Inputs/Execution。image=repository@digest；platform来自ArtifactReference精确拼接。main使用entrypoint覆盖，main默认CMD来自批准镜像；依赖command.entrypoint/args/env保持原语义。

| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `schemaVersion` | `integer` | 是 | "未在本schema声明" | enum=[1] |
| `applicationId` | `string` | 是 | "未在本schema声明" | minLength=1; maxLength=256; pattern="^[a-z][a-z0-9-]{0,62}$" |
| `version` | `string` | 是 | "未在本schema声明" | minLength=1; maxLength=256; pattern="^[A-Za-z0-9][A-Za-z0-9._+-]{0,63}$" |
| `platform` | `string` | 是 | "未在本schema声明" | pattern="^[a-z0-9]+/[a-z0-9._-]+$" |
| `image` | `string` | 是 | "未在本schema声明" | pattern="^[^@]+@sha256:[0-9a-f]{64}$" |
| `execution` | [WorkspaceApplicationExecution](rest-schemas.md#workspaceapplicationexecution) | 否 | "未在本schema声明" |  |
| `credentials` | `array<WorkspaceApplicationCredential>` | 否 | "未在本schema声明" |  |
| `entrypoint` | `array<string>` | 否 | "未在本schema声明" |  |
| `ports` | `array<WorkspaceApplicationPort>` | 否 | "未在本schema声明" |  |
| `entryPort` | `string` | 否 | "未在本schema声明" | minLength=1; maxLength=256 |
| `healthChecks` | `array<WorkspaceApplicationHealthCheck>` | 否 | "未在本schema声明" |  |
| `persistentMounts` | `array<WorkspaceApplicationMount>` | 否 | "未在本schema声明" |  |
| `scratchMounts` | `array<WorkspaceApplicationMount>` | 否 | "未在本schema声明" |  |
| `secretInputs` | `array<WorkspaceApplicationSecretInput>` | 否 | "未在本schema声明" |  |
| `configInputs` | `array<WorkspaceApplicationConfigInput>` | 否 | "未在本schema声明" |  |
| `dependencies` | `array<WorkspaceApplicationDependency>` | 否 | "未在本schema声明" |  |
| `exposurePolicy` | `string` | 是 | "未在本schema声明" | enum=["anonymous","application","cloud_private"] |
| `compute` | [WorkspaceApplicationCompute](rest-schemas.md#workspaceapplicationcompute) | 否 | "未在本schema声明" |  |

对象约束：`additionalProperties=false`

x-canonical-source："packages/contracts/go/workspace_application.go#WorkspaceApplicationRevision"

## DataMountPolicy

类型：`object`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`
引用applicationRevisionTemplate.persistentMounts的name，不重复mountPath/mode事实。

| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `mountName` | `string` | 是 | "未在本schema声明" | minLength=1; maxLength=256 |
| `concurrentWritersSupported` | `boolean` | 是 | "未在本schema声明" |  |

对象约束：`additionalProperties=false`

## ApplicationOwnedAccessContract

类型：`object`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`
应用自有会话/登录协议，保持现有WorkspaceApplicationCredential密码与session-secret能力；Cloud不把平台cookie传应用。

| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `mode` | `string` | 是 | "未在本schema声明" | enum=["application"] |
| `loginPath` | `string` | 是 | "未在本schema声明" | pattern="^/(?!.*(?:^\|/)\\.\\.(?:/\|$))[A-Za-z0-9_./-]*$" |
| `logoutPath` | `string` | 是 | "未在本schema声明" | pattern="^/(?!.*(?:^\|/)\\.\\.(?:/\|$))[A-Za-z0-9_./-]*$" |
| `usernameCredentialName` | `string` | 否 | "未在本schema声明" | minLength=1; maxLength=256 |

对象约束：`additionalProperties=false`

## CloudPrivateAccessContract

类型：`object`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`
复用当前canonical exposurePolicy与WorkspaceApplicationEntry；provider未具受保护入口能力时准入拒绝，不自行发App session、GatewayOIDC或改应用登录。Source当前cloud_private不发布公网入口，不能伪装可打开。

| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `mode` | `string` | 是 | "未在本schema声明" | enum=["cloud_private"] |
| `entryContract` | `string` | 是 | "未在本schema声明" | enum=["WorkspaceApplicationEntry"] |
| `admissionReceiptId` | `string` | 是 | "未在本schema声明" | minLength=1; maxLength=512 |

对象约束：`additionalProperties=false`

## AnonymousAccessContract

类型：`object`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`
仅发布者明确声明且平台准入允许；不因访问故障降级为anonymous。

| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `mode` | `string` | 是 | "未在本schema声明" | enum=["anonymous"] |

对象约束：`additionalProperties=false`

## CreditSource

类型：`object`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`


| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `originalWalletOperationId` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "未在本schema声明" |  |
| `originalSubscriptionPeriodId` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "未在本schema声明" |  |
| `policyVersionId` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "未在本schema声明" |  |
| `creditReceiptId` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "未在本schema声明" |  |
| `amountUSDMicros` | [USDMicros](rest-schemas.md#usdmicros) | 是 | "未在本schema声明" |  |

对象约束：`additionalProperties=false`

## TenantWorkspaceAction

类型：`object`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`


| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `workspaceId` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "未在本schema声明" |  |
| `action` | `string` | 是 | "未在本schema声明" | enum=["suspend","resume","delete"] |
| `operationId` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "未在本schema声明" |  |
| `operationOwner` | `string` | 是 | "未在本schema声明" | enum=["workspace"] |
| `status` | `string` | 是 | "未在本schema声明" | enum=["accepted","running","awaiting_confirmation","succeeded","failed","needs_attention","cancelled"] |

对象约束：`additionalProperties=false`

## TenantWorkspaceSkip

类型：`object`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`


| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `workspaceId` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "未在本schema声明" |  |
| `reason` | `string` | 是 | "未在本schema声明" | enum=["not_suspended_by_tenant","paid_period_expired","already_deleted","resources_absent","resources_unknown","operation_in_progress"] |

对象约束：`additionalProperties=false`

## TenantLifecycleProgress

类型：`object`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`
tenant.operations.result保存受理的子操作身份，状态从Workspace owner读回；Tenant访问启用不等于所有应用已复机。仅恢复originalTenantSuspendOperationId暂停、原账期有效且资源confirmed的Workspace，不续费重购，不改变其他原因的停用。

| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `tenantId` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | {"kind": "derived", "sources": ["tenant.operations.result", "tenant.tenants.status", "workspace.operations.status"], "rule": "读取Tenant lifecycle子操作身份，再由Workspace owner返回原资源/账期/暂停来源及实际子Operation"} |  |
| `operation` | [Operation](rest-schemas.md#operation) | 是 | {"kind": "derived", "sources": ["tenant.operations.result", "tenant.tenants.status", "workspace.operations.status"], "rule": "读取Tenant lifecycle子操作身份，再由Workspace owner返回原资源/账期/暂停来源及实际子Operation"} |  |
| `accessStatus` | `string` | 是 | {"kind": "derived", "sources": ["tenant.operations.result", "tenant.tenants.status", "workspace.operations.status"], "rule": "读取Tenant lifecycle子操作身份，再由Workspace owner返回原资源/账期/暂停来源及实际子Operation"} | enum=["enabled","revoked"] |
| `workspaceActions` | `array<TenantWorkspaceAction>` | 是 | {"kind": "derived", "sources": ["tenant.operations.result", "tenant.tenants.status", "workspace.operations.status"], "rule": "读取Tenant lifecycle子操作身份，再由Workspace owner返回原资源/账期/暂停来源及实际子Operation"} |  |
| `skipped` | `array<TenantWorkspaceSkip>` | 是 | {"kind": "derived", "sources": ["tenant.operations.result", "tenant.tenants.status", "workspace.operations.status"], "rule": "读取Tenant lifecycle子操作身份，再由Workspace owner返回原资源/账期/暂停来源及实际子Operation"} |  |

对象约束：`additionalProperties=false`

## BuildRecipeContractOutputImageCommand

类型：`object`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`


| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `entrypoint` | `array<string>` | 是 | "未在本schema声明" |  |
| `cmd` | `array<string>` | 是 | "未在本schema声明" |  |

对象约束：`additionalProperties=false`

## UpdateRenewalSettingsRequest

类型：`object`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`
启用需当前价格/续期政策与用户明确同意快照；关闭阻止未发出扣费的未来自动周期。已确认/unknown原单只能继续精确读回，不撤回已发生支付、不盲退款。修改成功前保留原幂等身份，CloudIdentity每次automatic debit须typed读取当前consent。

| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `renewalMode` | `string` | 是 | "未在本schema声明" | enum=["manual","automatic"] |
| `expectedRenewalSettingsVersion` | [NonnegativeInt64](rest-schemas.md#nonnegativeint64) | 是 | "未在本schema声明" |  |
| `automaticRenewalConsent` | `boolean` | 否 | "未在本schema声明" | enum=[true] |

对象约束：`additionalProperties=false; oneOf=[{"properties":{"renewalMode":{"enum":["manual"]}},"not":{"required":["automaticRenewalConsent"]}},{"properties":{"renewalMode":{"enum":["automatic"]}},"required":["automaticRenewalConsent"]}]`

## WorkspaceApplicationEntry

类型：`object`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`


| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `serviceName` | `string` | 否 | "未在本schema声明" | minLength=1; maxLength=512 |
| `port` | `integer` | 否 | "未在本schema声明" | minimum=1; maximum=65535 |
| `url` | `string/uri` | 否 | "未在本schema声明" |  |

对象约束：`additionalProperties=false; oneOf=[{"required":["serviceName","port"],"not":{"required":["url"]}},{"required":["url"],"not":{"anyOf":[{"required":["serviceName"]},{"required":["port"]}]}}]`

x-canonical-source："packages/contracts/go/workspace_application_runtime.go#WorkspaceApplicationEntry"

## PackageFormatContractReference

类型：`object`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`
引用Framework/OMA当前发布者验证器与schema，不在Cloud定义另一种包格式。生产准入必须Owner release/readback与签名收据吻合；例子的formatVersion/仓库都是合成fixture，不表示已发布标准。

| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `owner` | `string` | 是 | "未在本schema声明" | enum=["framework"] |
| `formatVersion` | `string` | 是 | "未在本schema声明" | minLength=1; maxLength=512 |
| `schemaObjectRef` | `string` | 是 | "未在本schema声明" | minLength=1; maxLength=512 |
| `schemaDigest` | `string` | 是 | "未在本schema声明" | pattern="^sha256:[0-9a-f]{64}$" |
| `validatorArtifact` | [ArtifactReference](rest-schemas.md#artifactreference) | 是 | "未在本schema声明" |  |
| `admissionReceiptId` | `string` | 是 | "未在本schema声明" | minLength=1; maxLength=512 |

对象约束：`additionalProperties=false`

## WorkspaceApplicationCredentials

类型：`object`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`
投影当前workspaceCurrentApplicationCredentials→Fabric ReadWorkspaceApplicationRuntimeCredentials：仅声明workspace_admin_password且actual ready/身份匹配时，向已授权Workspace owner单次返回username/password。绝不返回workspace_session_secret或GatewayKey；不缓存完整幂等响应、不写DB/日志/事件，重复显式reveal仍实时Owner授权。

| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `workspaceId` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "未在本schema声明" |  |
| `runtimeInstanceId` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "未在本schema声明" |  |
| `username` | `string` | 是 | "未在本schema声明" | minLength=1 |
| `password` | `string` | 是 | "未在本schema声明" | minLength=1 |

对象约束：`additionalProperties=false`

x-source-interface："services/control-plane/internal/server/routes_workspace.go#runtime-credentials/reveal; workspace_application_access.go#workspaceCurrentApplicationCredentials; packages/contracts/go/workspace_application_runtime.go#WorkspaceApplicationRuntimeCredentials"

## UpgradePlanRules

类型：`object`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`


| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `kind` | `string` | 是 | "未在本schema声明" | enum=["upgrade_immediate"] |
| `effectiveWhen` | `string` | 是 | "未在本schema声明" | enum=["resource_and_runtime_readback_confirmed"] |
| `preservePaidPeriod` | `boolean` | 是 | "未在本schema声明" | enum=[true] |
| `oldPriceSource` | `string` | 是 | "未在本schema声明" | enum=["accepted_applied_subscription_plan"] |
| `chargeClock` | `string` | 是 | "未在本schema声明" | enum=["quote_pricing_basis_at_frozen_on_acceptance"] |
| `timeUnit` | `string` | 是 | "未在本schema声明" | enum=["utc_integer_milliseconds"] |
| `chargeRounding` | `string` | 是 | "未在本schema声明" | enum=["ceil_once_usd_micro"] |
| `zeroCharge` | `string` | 是 | "未在本schema声明" | enum=["record_evidence_skip_gateway"] |
| `knownFailureCompensation` | `string` | 是 | "未在本schema声明" | enum=["full_unrefunded_original_supplement"] |
| `unknownOutcome` | `string` | 是 | "未在本schema声明" | enum=["read_original_action_no_refund"] |
| `irreversibleResidualCostOwner` | `string` | 是 | "未在本schema声明" | enum=["platform"] |
| `supplementDeleteRefund` | `string` | 是 | "未在本schema声明" | enum=["floor_unused_supplement_coverage"] |

对象约束：`additionalProperties=false`

## DowngradePlanRules

类型：`object`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`


| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `kind` | `string` | 是 | "未在本schema声明" | enum=["downgrade_next_period"] |
| `plannedBoundary` | `string` | 是 | "未在本schema声明" | enum=["original_paid_through"] |
| `currentPeriodRefund` | `string` | 是 | "未在本schema声明" | enum=["none"] |
| `nextPeriodPrice` | `string` | 是 | "未在本schema声明" | enum=["accepted_target_plan_quote"] |
| `requiresConfirmedNextPeriodPayment` | `boolean` | 是 | "未在本schema声明" | enum=[true] |
| `cancelBefore` | `string` | 是 | "未在本schema声明" | enum=["next_period_payment_obligation_accepted"] |
| `earlyPaidChange` | `string` | 是 | "未在本schema声明" | enum=["do_not_reduce_resources_before_original_paid_through"] |
| `manualUnpaidBoundary` | `string` | 是 | "未在本schema声明" | enum=["awaiting_payment_suspend_unpaid_usage"] |
| `knownFailureCompensation` | `string` | 是 | "未在本schema声明" | enum=["full_unrefunded_target_period_charge"] |
| `fallback` | `string` | 是 | "未在本schema声明" | enum=["none_no_old_price_renewal_or_sku_substitution"] |

对象约束：`additionalProperties=false`

## PlanChangePolicy

类型：`object`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`
D17已由用户批准，13_plan_change_policy.md为产品语义Owner。固定政策，不接受配置任意公式、pending/draft或旧未决占位；金额算法/数值向量见contracts/plan-change-policy.json。

| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `version` | `string` | 是 | "未在本schema声明" | enum=["workspace-plan-change-v1"] |
| `approvalStatus` | `string` | 是 | "未在本schema声明" | enum=["approved"] |
| `upgrade` | [UpgradePlanRules](rest-schemas.md#upgradeplanrules) | 是 | "未在本schema声明" |  |
| `downgrade` | [DowngradePlanRules](rest-schemas.md#downgradeplanrules) | 是 | "未在本schema声明" |  |
| `classification` | `string` | 是 | "未在本schema声明" | enum=["approved_comparable_resources_not_price_or_sku_name"] |
| `mixedOrIncomparableTransition` | `string` | 是 | "未在本schema声明" | enum=["reject"] |
| `noOpTransition` | `string` | 是 | "未在本schema声明" | enum=["reject"] |
| `storageShrink` | `string` | 是 | "未在本schema声明" | enum=["reject_unless_owner_supported_transition"] |
| `concurrency` | `string` | 是 | "未在本schema声明" | enum=["one_unfinished_plan_change_per_workspace"] |
| `cancelAndReplace` | `string` | 是 | "未在本schema声明" | enum=["explicit_cancel_then_new_quote"] |
| `baseRefundPolicy` | `string` | 是 | "未在本schema声明" | enum=["preserve_original_base_order_policy"] |
| `providerExecutionPlan` | `string` | 是 | "未在本schema声明" | enum=["approved_strategy_frozen_before_quote"] |

对象约束：`additionalProperties=false`

## UpgradeProration

类型：`object`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`
D=E-S>0,R=E-T且0<R<=D,Delta=max(Pnew-Pold,0)。charge=ceil(Delta*R/D)，仅最后一次ceil；计算结果固定在Quote，不随重试时间变化。

| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `periodMilliseconds` | [NonnegativeInt64](rest-schemas.md#nonnegativeint64) | 是 | "未在本schema声明" |  |
| `remainingMilliseconds` | [NonnegativeInt64](rest-schemas.md#nonnegativeint64) | 是 | "未在本schema声明" |  |
| `priceDeltaUSDMicros` | [USDMicros](rest-schemas.md#usdmicros) | 是 | "未在本schema声明" |  |
| `rounding` | `string` | 是 | "未在本schema声明" | enum=["ceil_once_usd_micro"] |

对象约束：`additionalProperties=false`

## NextPeriodPlanQuote

类型：`object`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`
原E开始，按原billing anchor推进一个月；已接受目标价，不先扣旧价。提前付款不提前降低资源。

| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `periodStart` | `string/date-time` | 是 | "未在本schema声明" |  |
| `periodEnd` | `string/date-time` | 是 | "未在本schema声明" |  |
| `totalUSDMicros` | [USDMicros](rest-schemas.md#usdmicros) | 是 | "未在本schema声明" |  |
| `pricePolicyVersionId` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "未在本schema声明" |  |

对象约束：`additionalProperties=false`

## PlanChangeCalculation

类型：`object`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`
Catalog唯一计算Owner；source价格来自当前已成功生效且客户接受的订阅快照，不用最新目录覆写Pold。upgrade plannedEffectiveAt=T只是立即执行意图，appliedAt需实际读回；downgrade plannedEffectiveAt=E且charge=0。 原S/E时间戳不改写；计算使用其UTC UnixMilli()，T由服务器按毫秒生成。nextPeriod边界由Workspace当前billingAnchorDay/nextBillingMonth提供，Catalog不自选月份长度。 executionPlan引用在报价前由Fabric批准并冻结；可能原地调整，也可能目标pool claim+迁移计算绑定并保留CBS。执行失败不静默切另一策略；客户E/数据/Key义务不因Compute ID变化而改变。 canonical毫秒来自原Owner native时间的UnixMilli及源金融snapshot原bytes，不能从PostgreSQL已舍入timestamptz重算。原quoteAt/periodStart/End只保留原时刻用于展示/溯源，数学以三个Milliseconds字段为准。

| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `policyVersion` | `string` | 是 | "未在本schema声明" | enum=["workspace-plan-change-v1"] |
| `kind` | `string` | 是 | "未在本schema声明" | enum=["upgrade_immediate","downgrade_next_period"] |
| `transitionId` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "未在本schema声明" |  |
| `sourceSubscriptionId` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "未在本schema声明" |  |
| `sourceSubscriptionVersion` | [NonnegativeInt64](rest-schemas.md#nonnegativeint64) | 是 | "未在本schema声明" |  |
| `sourcePeriodId` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "未在本schema声明" |  |
| `sourceComputePlanId` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "未在本schema声明" |  |
| `sourceStoragePlanId` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "未在本schema声明" |  |
| `targetComputePlanId` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "未在本schema声明" |  |
| `targetStoragePlanId` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "未在本schema声明" |  |
| `sourcePricePolicyVersionId` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "未在本schema声明" |  |
| `targetPricePolicyVersionId` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "未在本schema声明" |  |
| `sourceMonthlyUSDMicros` | [USDMicros](rest-schemas.md#usdmicros) | 是 | "未在本schema声明" |  |
| `targetMonthlyUSDMicros` | [USDMicros](rest-schemas.md#usdmicros) | 是 | "未在本schema声明" |  |
| `quoteAt` | `string/date-time` | 是 | "未在本schema声明" |  |
| `periodStart` | `string/date-time` | 是 | "未在本schema声明" |  |
| `periodEnd` | `string/date-time` | 是 | "未在本schema声明" |  |
| `chargeUSDMicros` | [USDMicros](rest-schemas.md#usdmicros) | 是 | "未在本schema声明" |  |
| `plannedEffectiveAt` | `string/date-time` | 是 | "未在本schema声明" |  |
| `upgradeProration` | [UpgradeProration](rest-schemas.md#upgradeproration) | 否 | "未在本schema声明" |  |
| `nextPeriod` | [NextPeriodPlanQuote](rest-schemas.md#nextperiodplanquote) | 否 | "未在本schema声明" |  |
| `executionPlanId` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "未在本schema声明" |  |
| `executionPlanDigest` | [Digest](rest-schemas.md#digest) | 是 | "未在本schema声明" |  |
| `quoteAtMilliseconds` | [NonnegativeInt64](rest-schemas.md#nonnegativeint64) | 是 | "未在本schema声明" |  |
| `periodStartMilliseconds` | [NonnegativeInt64](rest-schemas.md#nonnegativeint64) | 是 | "未在本schema声明" |  |
| `periodEndMilliseconds` | [NonnegativeInt64](rest-schemas.md#nonnegativeint64) | 是 | "未在本schema声明" |  |
| `sourceFinancialSnapshotDigest` | [Digest](rest-schemas.md#digest) | 是 | "未在本schema声明" |  |

对象约束：`additionalProperties=false; oneOf=[{"properties":{"kind":{"enum":["upgrade_immediate"]}},"required":["upgradeProration"],"not":{"required":["nextPeriod"]}},{"properties":{"kind":{"enum":["downgrade_next_period"]},"chargeUSDMicros":{"enum":["0"]}},"required":["nextPeriod"],"not":{"required":["upgradeProration"]}}]`

## PlanChange

类型：`object`；Owner：`workspace`；表：`workspace.plan_changes`
Workspace业务计划权威；operationId固定初次受理任务。downgrade初次Operation可在计划保存后succeeded，但PlanChange仍scheduled，边界执行用executionOperationId，不复活终态Operation。只有status=applied且exact资源/应用readback后才有appliedAt。zero current charge不产生Gateway动作。 executionPlan引用在报价前由Fabric批准并冻结；可能原地调整，也可能目标pool claim+迁移计算绑定并保留CBS。执行失败不静默切另一策略；客户E/数据/Key义务不因Compute ID变化而改变。 canonical毫秒来自原Owner native时间的UnixMilli及源金融snapshot原bytes，不能从PostgreSQL已舍入timestamptz重算。原quoteAt/periodStart/End只保留原时刻用于展示/溯源，数学以三个Milliseconds字段为准。

| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `id` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "workspace.plan_changes.id" |  |
| `workspaceId` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "workspace.plan_changes.workspace_id" |  |
| `kind` | `string` | 是 | "workspace.plan_changes.kind" | enum=["upgrade_immediate","downgrade_next_period"] |
| `status` | `string` | 是 | "workspace.plan_changes.status" | enum=["requested","scheduled","awaiting_payment","applying","applied","failed","needs_attention","cancelled"] |
| `sourceComputePlanId` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "workspace.plan_changes.source_compute_plan_id" |  |
| `sourceStoragePlanId` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "workspace.plan_changes.source_storage_plan_id" |  |
| `targetComputePlanId` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "workspace.plan_changes.target_compute_plan_id" |  |
| `targetStoragePlanId` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "workspace.plan_changes.target_storage_plan_id" |  |
| `sourcePricePolicyVersionId` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "workspace.plan_changes.source_price_policy_version_id" |  |
| `targetPricePolicyVersionId` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "workspace.plan_changes.target_price_policy_version_id" |  |
| `sourceSubscriptionId` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "workspace.plan_changes.source_subscription_id" |  |
| `sourceSubscriptionVersion` | [NonnegativeInt64](rest-schemas.md#nonnegativeint64) | 是 | "workspace.plan_changes.source_subscription_version" |  |
| `sourcePeriodId` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "workspace.plan_changes.source_period_id" |  |
| `sourceMonthlyUSDMicros` | [USDMicros](rest-schemas.md#usdmicros) | 是 | "workspace.plan_changes.source_monthly_usd_micros" |  |
| `targetMonthlyUSDMicros` | [USDMicros](rest-schemas.md#usdmicros) | 是 | "workspace.plan_changes.target_monthly_usd_micros" |  |
| `quoteId` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "workspace.plan_changes.quote_id" |  |
| `policyVersion` | `string` | 是 | "workspace.plan_changes.policy_version" | enum=["workspace-plan-change-v1"] |
| `quoteAt` | `string/date-time` | 是 | "workspace.plan_changes.quote_at" |  |
| `periodStart` | `string/date-time` | 是 | "workspace.plan_changes.period_start" |  |
| `periodEnd` | `string/date-time` | 是 | "workspace.plan_changes.period_end" |  |
| `chargeUSDMicros` | [USDMicros](rest-schemas.md#usdmicros) | 是 | "workspace.plan_changes.charge_usd_micros" |  |
| `plannedEffectiveAt` | `string/date-time` | 是 | "workspace.plan_changes.planned_effective_at" |  |
| `appliedAt` | `string/date-time` | 否 | "workspace.plan_changes.applied_at" |  |
| `operationId` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "workspace.plan_changes.operation_id" |  |
| `executionOperationId` | [OpaqueId](rest-schemas.md#opaqueid) | 否 | "workspace.plan_changes.execution_operation_id" |  |
| `cancellationOperationId` | [OpaqueId](rest-schemas.md#opaqueid) | 否 | "workspace.plan_changes.cancellation_operation_id" |  |
| `chargeOperationId` | [OpaqueId](rest-schemas.md#opaqueid) | 否 | "workspace.plan_changes.charge_operation_id" |  |
| `chargeStatus` | `string` | 是 | {"kind": "derived", "sources": ["gateway.wallet_operations.status", "gateway.wallet_operations.id", "workspace.subscription_period_obligations.status"], "rule": "按原升级补差单/下一期目标款分别Owner readback；退款unknown占原单未确认额度不反复发新单，无Gateway动作显示not_required/not_requested"} | enum=["not_required","not_requested","requested","confirmed","rejected","unknown"] |
| `nextPeriodStart` | `string/date-time` | 否 | "workspace.plan_changes.next_period_start" |  |
| `nextPeriodEnd` | `string/date-time` | 否 | "workspace.plan_changes.next_period_end" |  |
| `nextPeriodChargeUSDMicros` | [USDMicros](rest-schemas.md#usdmicros) | 否 | "workspace.plan_changes.next_period_charge_usd_micros" |  |
| `nextPeriodObligationId` | [OpaqueId](rest-schemas.md#opaqueid) | 否 | "workspace.plan_changes.next_period_obligation_id" |  |
| `nextPeriodChargeOperationId` | [OpaqueId](rest-schemas.md#opaqueid) | 否 | {"kind": "derived", "sources": ["workspace.subscription_period_obligations.wallet_operation_id"], "rule": "nextPeriodObligationId权威周期义务引用的原Gateway charge，不重复存plan_changes指针"} |  |
| `nextPeriodChargeStatus` | `string` | 否 | {"kind": "derived", "sources": ["gateway.wallet_operations.status", "gateway.wallet_operations.id", "workspace.subscription_period_obligations.status"], "rule": "按原升级补差单/下一期目标款分别Owner readback；退款unknown占原单未确认额度不反复发新单，无Gateway动作显示not_required/not_requested"} | enum=["not_required","not_requested","requested","confirmed","rejected","unknown"] |
| `refundOperationIds` | `array<OpaqueId>` | 是 | {"kind": "derived", "sources": ["gateway.wallet_operations.status", "gateway.wallet_operations.id", "workspace.subscription_period_obligations.status"], "rule": "按原升级补差单/下一期目标款分别Owner readback；退款unknown占原单未确认额度不反复发新单，无Gateway动作显示not_required/not_requested"} |  |
| `scheduleVersion` | [NonnegativeInt64](rest-schemas.md#nonnegativeint64) | 是 | "workspace.plan_changes.schedule_version" |  |
| `observationResult` | `string` | 是 | {"kind": "derived", "sources": ["workspace.plan_changes.actual_outcome"], "rule": "typed actual_outcome.observationResult；money/provider unknown不能自动failed或退钱"} | enum=["confirmed","rejected","unknown"] |
| `errorCode` | [ErrorCode](rest-schemas.md#errorcode) | 否 | "workspace.plan_changes.error_code" |  |
| `cancellable` | `boolean` | 是 | {"kind": "derived", "sources": ["workspace.plan_changes.status", "workspace.subscription_period_obligations.status"], "rule": "仅scheduled/awaiting_payment且下期资金义务尚未accepted、无已发出资金/provider action；取消须scheduleVersion CAS，不靠前端猜"} |  |
| `createdAt` | `string/date-time` | 是 | "workspace.plan_changes.created_at" |  |
| `updatedAt` | `string/date-time` | 是 | "workspace.plan_changes.updated_at" |  |
| `deliveryOutcome` | `string` | 是 | {"kind": "derived", "sources": ["workspace.plan_changes.actual_outcome"], "rule": "typed actual_outcome.deliveryOutcome；delivery failed且resource irreversible_residual是已确定目标失败，可按fenced失败证据全退原补差；不能把needs_attention一律等同unknown"} | enum=["pending","applied","failed","unknown"] |
| `resourceOutcome` | `string` | 是 | {"kind": "derived", "sources": ["workspace.plan_changes.actual_outcome"], "rule": "typed actual_outcome.resourceOutcome；delivery failed且resource irreversible_residual是已确定目标失败，可按fenced失败证据全退原补差；不能把needs_attention一律等同unknown"} | enum=["unchanged","target_confirmed","restored","irreversible_residual","unknown"] |
| `runtimeReadbackRequirement` | `string` | 是 | {"kind": "derived", "sources": ["workspace.plan_changes.actual_outcome", "workspace.workspaces.active_deployment_id"], "rule": "Workspace在本次执行/重验时冻结真实绑定与当前资源需求；无app为not_applicable不伪造Runtime，scheduled不占执行锁；风险只披露不暗改价格/取消"} | enum=["required","not_applicable"] |
| `currentRequirementValidation` | `string` | 是 | {"kind": "derived", "sources": ["workspace.plan_changes.actual_outcome", "workspace.workspaces.active_deployment_id"], "rule": "Workspace在本次执行/重验时冻结真实绑定与当前资源需求；无app为not_applicable不伪造Runtime，scheduled不占执行锁；风险只披露不暗改价格/取消"} | enum=["valid","at_risk","not_checked"] |
| `riskCode` | [ErrorCode](rest-schemas.md#errorcode) | 否 | {"kind": "derived", "sources": ["workspace.plan_changes.actual_outcome", "workspace.workspaces.active_deployment_id"], "rule": "Workspace在本次执行/重验时冻结真实绑定与当前资源需求；无app为not_applicable不伪造Runtime，scheduled不占执行锁；风险只披露不暗改价格/取消"} |  |
| `lastValidatedAt` | `string/date-time` | 否 | {"kind": "derived", "sources": ["workspace.plan_changes.actual_outcome", "workspace.workspaces.active_deployment_id"], "rule": "Workspace在本次执行/重验时冻结真实绑定与当前资源需求；无app为not_applicable不伪造Runtime，scheduled不占执行锁；风险只披露不暗改价格/取消"} |  |
| `executionPlanId` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "workspace.plan_changes.execution_plan_id" |  |
| `executionPlanDigest` | [Digest](rest-schemas.md#digest) | 是 | "workspace.plan_changes.execution_plan_digest" |  |
| `quoteAtMilliseconds` | [NonnegativeInt64](rest-schemas.md#nonnegativeint64) | 是 | "workspace.plan_changes.quote_at_ms" |  |
| `periodStartMilliseconds` | [NonnegativeInt64](rest-schemas.md#nonnegativeint64) | 是 | "workspace.plan_changes.period_start_ms" |  |
| `periodEndMilliseconds` | [NonnegativeInt64](rest-schemas.md#nonnegativeint64) | 是 | "workspace.plan_changes.period_end_ms" |  |
| `sourceFinancialSnapshotDigest` | [Digest](rest-schemas.md#digest) | 是 | "workspace.plan_changes.source_financial_snapshot_digest" |  |

对象约束：`additionalProperties=false; allOf=[{"oneOf":[{"properties":{"kind":{"enum":["upgrade_immediate"]}},"not":{"required":["nextPeriodChargeUSDMicros"]}},{"properties":{"kind":{"enum":["downgrade_next_period"]},"chargeUSDMicros":{"enum":["0"]},"chargeStatus":{"enum":["not_required"]}},"required":["nextPeriodStart","nextPeriodEnd","nextPeriodChargeUSDMicros","nextPeriodChargeStatus"]}]},{"oneOf":[{"properties":{"status":{"enum":["applied"]}},"required":["appliedAt","executionOperationId"]},{"properties":{"status":{"enum":["requested","scheduled","awaiting_payment","applying","failed","needs_attention","cancelled"]}},"not":{"required":["appliedAt"]}}]},{"oneOf":[{"properties":{"status":{"enum":["applied"]},"deliveryOutcome":{"enum":["applied"]},"resourceOutcome":{"enum":["target_confirmed"]}}},{"properties":{"status":{"enum":["requested","scheduled","awaiting_payment","applying","failed","needs_attention","cancelled"]}}}]},{"oneOf":[{"properties":{"kind":{"enum":["upgrade_immediate"]},"chargeUSDMicros":{"enum":["0"]},"chargeStatus":{"enum":["not_required"]}},"not":{"required":["chargeOperationId"]}},{"properties":{"kind":{"enum":["upgrade_immediate"]},"chargeUSDMicros":{"not":{"enum":["0"]}},"status":{"enum":["applied"]},"chargeStatus":{"enum":["confirmed"]}},"required":["chargeOperationId"]},{"properties":{"kind":{"enum":["upgrade_immediate"]},"chargeUSDMicros":{"not":{"enum":["0"]}},"status":{"enum":["requested","scheduled","awaiting_payment","applying","failed","needs_attention","cancelled"]}}},{"properties":{"kind":{"enum":["downgrade_next_period"]},"status":{"enum":["applied"]},"nextPeriodChargeUSDMicros":{"enum":["0"]},"nextPeriodChargeStatus":{"enum":["not_required"]}},"required":["nextPeriodObligationId"],"not":{"required":["nextPeriodChargeOperationId"]}},{"properties":{"kind":{"enum":["downgrade_next_period"]},"status":{"enum":["applied"]},"nextPeriodChargeUSDMicros":{"not":{"enum":["0"]}},"nextPeriodChargeStatus":{"enum":["confirmed"]}},"required":["nextPeriodObligationId","nextPeriodChargeOperationId"]},{"properties":{"kind":{"enum":["downgrade_next_period"]},"status":{"enum":["requested","scheduled","awaiting_payment","applying","failed","needs_attention","cancelled"]}}}]},{"oneOf":[{"properties":{"kind":{"enum":["upgrade_immediate"]},"status":{"enum":["requested","applying","applied","failed","needs_attention"]}}},{"properties":{"kind":{"enum":["downgrade_next_period"]}}}]},{"oneOf":[{"properties":{"status":{"enum":["failed","needs_attention"]}},"required":["errorCode"]},{"properties":{"status":{"enum":["requested","scheduled","awaiting_payment","applying","applied","cancelled"]}}}]}]`

## PlanChangePage

类型：`object`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`


| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `items` | `array<PlanChange>` | 是 | "未在本schema声明" |  |
| `nextCursor` | [OpaqueId](rest-schemas.md#opaqueid) | 否 | "未在本schema声明" |  |

对象约束：`additionalProperties=false`

## CancelPlanChangeRequest

类型：`object`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`
只取消尚未接受下期付款义务的scheduled/awaiting_payment；不删除记录、不退款当前期、不取消已经发出的unknown支付/资源动作。重复key返回原取消Operation。

| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `expectedScheduleVersion` | [NonnegativeInt64](rest-schemas.md#nonnegativeint64) | 是 | "未在本schema声明" |  |
| `reason` | `string` | 是 | "未在本schema声明" | minLength=1; maxLength=1024 |

对象约束：`additionalProperties=false`

## PlanChangeEvidence

类型：`object`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`
Ledger typed证据；scheduled没有伪资源读回，applied需实际资源+runtime receipt且S/E保持，failed需终止/fence/实际资源事实，不能把unknown当失败。

| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `schemaVersion` | `integer` | 是 | "未在本schema声明" | enum=[1] |
| `planChangeId` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "未在本schema声明" |  |
| `workspaceId` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "未在本schema声明" |  |
| `kind` | `string` | 是 | "未在本schema声明" | enum=["upgrade_immediate","downgrade_next_period"] |
| `policyVersion` | `string` | 是 | "未在本schema声明" | enum=["workspace-plan-change-v1"] |
| `quoteId` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "未在本schema声明" |  |
| `sourceSubscriptionId` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "未在本schema声明" |  |
| `sourceSubscriptionVersion` | [NonnegativeInt64](rest-schemas.md#nonnegativeint64) | 是 | "未在本schema声明" |  |
| `sourcePeriodId` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "未在本schema声明" |  |
| `sourceComputePlanId` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "未在本schema声明" |  |
| `sourceStoragePlanId` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "未在本schema声明" |  |
| `targetComputePlanId` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "未在本schema声明" |  |
| `targetStoragePlanId` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "未在本schema声明" |  |
| `sourcePricePolicyVersionId` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "未在本schema声明" |  |
| `targetPricePolicyVersionId` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "未在本schema声明" |  |
| `quoteAt` | `string/date-time` | 是 | "未在本schema声明" |  |
| `periodStart` | `string/date-time` | 是 | "未在本schema声明" |  |
| `periodEnd` | `string/date-time` | 是 | "未在本schema声明" |  |
| `chargeUSDMicros` | [USDMicros](rest-schemas.md#usdmicros) | 是 | "未在本schema声明" |  |
| `chargeOperationId` | [OpaqueId](rest-schemas.md#opaqueid) | 否 | "未在本schema声明" |  |
| `nextPeriodObligationId` | [OpaqueId](rest-schemas.md#opaqueid) | 否 | "未在本schema声明" |  |
| `executionEpoch` | [NonnegativeInt64](rest-schemas.md#nonnegativeint64) | 否 | "未在本schema声明" |  |
| `resourceObservationReceiptId` | [OpaqueId](rest-schemas.md#opaqueid) | 否 | "未在本schema声明" |  |
| `runtimeReadinessReceiptId` | [OpaqueId](rest-schemas.md#opaqueid) | 否 | "未在本schema声明" |  |
| `failureReceiptId` | [OpaqueId](rest-schemas.md#opaqueid) | 否 | "未在本schema声明" |  |
| `appliedAt` | `string/date-time` | 否 | "未在本schema声明" |  |
| `outcome` | `string` | 是 | "未在本schema声明" | enum=["scheduled","applied","failed","needs_attention","cancelled"] |
| `retainedIrreversibleResources` | `boolean` | 是 | "未在本schema声明" |  |
| `deliveryOutcome` | `string` | 是 | "未在本schema声明" | enum=["pending","applied","failed","unknown"] |
| `resourceOutcome` | `string` | 是 | "未在本schema声明" | enum=["unchanged","target_confirmed","restored","irreversible_residual","unknown"] |
| `runtimeReadbackRequirement` | `string` | 是 | "未在本schema声明" | enum=["required","not_applicable"] |
| `quoteAtMilliseconds` | [NonnegativeInt64](rest-schemas.md#nonnegativeint64) | 是 | "未在本schema声明" |  |
| `periodStartMilliseconds` | [NonnegativeInt64](rest-schemas.md#nonnegativeint64) | 是 | "未在本schema声明" |  |
| `periodEndMilliseconds` | [NonnegativeInt64](rest-schemas.md#nonnegativeint64) | 是 | "未在本schema声明" |  |
| `sourceFinancialSnapshotDigest` | [Digest](rest-schemas.md#digest) | 是 | "未在本schema声明" |  |

对象约束：`additionalProperties=false; allOf=[{"oneOf":[{"properties":{"outcome":{"enum":["applied"]},"runtimeReadbackRequirement":{"enum":["required"]}},"required":["executionEpoch","appliedAt","resourceObservationReceiptId","runtimeReadinessReceiptId"]},{"properties":{"outcome":{"enum":["applied"]},"runtimeReadbackRequirement":{"enum":["not_applicable"]}},"required":["executionEpoch","appliedAt","resourceObservationReceiptId"],"not":{"required":["runtimeReadinessReceiptId"]}},{"properties":{"outcome":{"enum":["scheduled","cancelled","failed","needs_attention"]}}}]}]`

## SupplementalRefundEvidence

类型：`object`；Owner：`未在此schema单独声明；以操作Owner/来源规则为准`；表：`—`
退款用途严格判别：upgrade_failure_full/next_period_plan_failure_full需已确认未交付/fence/实际资源失败证据且退原单未退余款；supplement_delete_unused需PlanChange已成功applied和确切删除receipt，按自身T..E floor，不用base720。Gateway锁原charge验证confirmed与requested/unknown退款总额不超过原单。 退款数学只用原补差单保存的coverageStart/EndMilliseconds与删除Owner native UnixMilli，不从PG timestamp重新取值。

| 字段 | 类型 | 必填 | 来源 / 转换规则 | 约束 |
| --- | --- | --- | --- | --- |
| `schemaVersion` | `integer` | 是 | "未在本schema声明" | enum=[1] |
| `purpose` | `string` | 是 | "未在本schema声明" | enum=["upgrade_failure_full","supplement_delete_unused","next_period_plan_failure_full"] |
| `workspaceId` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "未在本schema声明" |  |
| `planChangeId` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "未在本schema声明" |  |
| `originalChargeOperationId` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "未在本schema声明" |  |
| `originalChargeReceiptId` | [OpaqueId](rest-schemas.md#opaqueid) | 是 | "未在本schema声明" |  |
| `originalConfirmedUSDMicros` | [USDMicros](rest-schemas.md#usdmicros) | 是 | "未在本schema声明" |  |
| `requestedRefundUSDMicros` | [USDMicros](rest-schemas.md#usdmicros) | 是 | "未在本schema声明" |  |
| `policyVersion` | `string` | 是 | "未在本schema声明" | enum=["workspace-plan-change-v1"] |
| `coverageStart` | `string/date-time` | 是 | "未在本schema声明" |  |
| `coverageEnd` | `string/date-time` | 是 | "未在本schema声明" |  |
| `deleteConfirmedAt` | `string/date-time` | 否 | "未在本schema声明" |  |
| `failureReceiptId` | [OpaqueId](rest-schemas.md#opaqueid) | 否 | "未在本schema声明" |  |
| `deletionReceiptId` | [OpaqueId](rest-schemas.md#opaqueid) | 否 | "未在本schema声明" |  |
| `fenceReceiptId` | [OpaqueId](rest-schemas.md#opaqueid) | 否 | "未在本schema声明" |  |
| `coverageStartMilliseconds` | [NonnegativeInt64](rest-schemas.md#nonnegativeint64) | 是 | "未在本schema声明" |  |
| `deleteConfirmedAtMilliseconds` | [NonnegativeInt64](rest-schemas.md#nonnegativeint64) | 否 | "未在本schema声明" |  |
| `coverageEndMilliseconds` | [NonnegativeInt64](rest-schemas.md#nonnegativeint64) | 是 | "未在本schema声明" |  |

对象约束：`additionalProperties=false; oneOf=[{"properties":{"purpose":{"enum":["supplement_delete_unused"]}},"required":["deleteConfirmedAt","deletionReceiptId","deleteConfirmedAtMilliseconds"],"not":{"required":["failureReceiptId"]}},{"properties":{"purpose":{"enum":["upgrade_failure_full","next_period_plan_failure_full"]}},"required":["failureReceiptId","fenceReceiptId"],"not":{"anyOf":[{"required":["deleteConfirmedAt"]},{"required":["deletionReceiptId"]}]}}]`

## BFF通用Operation路由（派生参考）

> 来源03与api_inventory；仅说明入口寻址，不建立第十个业务数据库。


### getOperation

`GET /api/v2/operations/{owner}/{operationId}`

目标Owner：`operation-path-owner`；权限：`member`

表：`{owner}.operations`（读写均在目标Owner，非BFF）。
请求：`无`；响应：`Operation`

### listAdminOperations

`GET /api/v2/admin/operations`

目标Owner：`required-owner-query`；权限：`platform_admin`

表：`{owner}.operations`（读写均在目标Owner，非BFF）。
请求：`无`；响应：`AdminOperationPage`

### reconcileOperation

`POST /api/v2/admin/operations/{owner}/{operationId}/reconcile`

目标Owner：`operation-path-owner`；权限：`platform_admin`

表：`{owner}.operations`（读写均在目标Owner，非BFF）。
请求：`ReconcileOperationRequest`；响应：`Operation`
