# Gateway Integration：API、字段与交互

> 源码快照 `7f5d05fe9b855d8af216caad714cf7fc85014d3c`；这是已定义目标的派生索引，不是业务实现完成声明。回到[总览](../15_domain_alignment.md)。

## 1. 边界与继承

模块：`services/gateway-integration/internal/gateway`。当前：**新双Owner部署骨架；资金/Key实际适配仍来自旧Control Plane client**。

Sub2API身份映射、Tenant钱包主体绑定、Key引用、扣退款命令身份和观察结果；钱包余额/记账权威始终Sub2API。

**继承 / 提取 / 新增：** 复用旧Sub2API typed client、原单/幂等读回和一次性Code；不造第二钱包，不把身份存在等同产品准入。

**旧事实：** Sub2API client；旧account/user精确映射；wallet_adjustment及workspace_api_key_id。

**事务边界：** 本域请求身份/观察/Outbox本地提交；远端钱包结果依原单读回；tenant与gateway即使同进程也不共享pool/事务。

**业务顺序：** Tenant提供受众绑定授权→Gateway核准账单主体→Sub2API原单动作/读回→Workspace继续；Key仅引用/指纹入库，明文只授权单次展示/注入。

**失败 / unknown：** 余额差不是付款凭证；unknown不再次扣款、超额退款或换原单。

## 2. 客户 REST 与后端 Owner

9 个规格REST操作；浏览器仅经BFF。表中的请求/响应为目标契约，不代表该RPC已挂载。字段展开见DTO目录。


### listWorkspaceTransactions

`GET /api/v2/workspaces/{workspaceId}/transactions`

权限：`admin, owner`；F：`F08, F12, F13`；主要成功状态：`200`。
| 入参位置 | 字段 | 类型 | 必填 |
| --- | --- | --- | --- |
| path | `workspaceId` | `OpaqueId` | 是 |
| query | `cursor` | `OpaqueId` | 否 |
| query | `limit` | `integer` | 否 |

Body：无独立命名body，见该操作schema；Response：[WalletOperationPage](rest-schemas.md#walletoperationpage)

响应顶层字段：`items`: array<WalletOperation>（必填）；`nextCursor`: OpaqueId（可选）

涉及表（规格声明，不自动等于每次都写）：`gateway.wallet_operations`
错误响应：`400`, `401`, `403`, `404`, `409`, `422`, `429`, `500`, `503`

### getWallet

`GET /api/v2/wallet`

权限：`admin, owner`；F：`F14`；主要成功状态：`200`。

Body：无独立命名body，见该操作schema；Response：[Wallet](rest-schemas.md#wallet)

响应顶层字段：`source`: string（必填）；`status`: string（必填）；`balanceUSDMicros`: USDMicros（必填）；`currency`: string（必填）；`fetchedAt`: string/date-time（必填）

涉及表（规格声明，不自动等于每次都写）：
错误响应：`400`, `401`, `403`, `404`, `409`, `422`, `429`, `500`, `503`

### listUsage

`GET /api/v2/usage`

权限：`admin, owner`；F：`F14`；主要成功状态：`200`。
| 入参位置 | 字段 | 类型 | 必填 |
| --- | --- | --- | --- |
| query | `cursor` | `OpaqueId` | 否 |
| query | `limit` | `integer` | 否 |
| query | `from` | `string/date-time` | 是 |
| query | `until` | `string/date-time` | 是 |
| query | `modelId` | `OpaqueId` | 否 |

Body：无独立命名body，见该操作schema；Response：[UsagePage](rest-schemas.md#usagepage)

响应顶层字段：`items`: array<Usage>（必填）；`nextCursor`: OpaqueId（可选）

涉及表（规格声明，不自动等于每次都写）：
错误响应：`400`, `401`, `403`, `404`, `409`, `422`, `429`, `500`, `503`

### listGatewayKeys

`GET /api/v2/gateway-keys`

权限：`member`；F：`F14`；主要成功状态：`200`。
| 入参位置 | 字段 | 类型 | 必填 |
| --- | --- | --- | --- |
| query | `cursor` | `OpaqueId` | 否 |
| query | `limit` | `integer` | 否 |

Body：无独立命名body，见该操作schema；Response：[GatewayKeyPage](rest-schemas.md#gatewaykeypage)

响应顶层字段：`items`: array<GatewayKey>（必填）；`nextCursor`: OpaqueId（可选）

涉及表（规格声明，不自动等于每次都写）：`gateway.key_bindings`
错误响应：`400`, `401`, `403`, `404`, `409`, `422`, `429`, `500`, `503`

### createGatewayKey

`POST /api/v2/gateway-keys`

权限：`member`；F：`F14`；主要成功状态：`201`。
| 入参位置 | 字段 | 类型 | 必填 |
| --- | --- | --- | --- |
| header | `Idempotency-Key` | `string` | 是 |
| header | `X-CSRF-Token` | `string` | 是 |

Body：[CreateGatewayKeyRequest](rest-schemas.md#creategatewaykeyrequest)；Response：[GatewayKeySecret](rest-schemas.md#gatewaykeysecret)

请求顶层字段：`name`: string（必填）；`modelIds`: array<OpaqueId>（必填）；`expiresAt`: string/date-time（可选）

响应顶层字段：`key`: GatewayKey（必填）；`secret`: string（必填）

涉及表（规格声明，不自动等于每次都写）：`gateway.key_bindings`
错误响应：`400`, `401`, `403`, `404`, `409`, `422`, `429`, `500`, `503`

### revealGatewayKey

`POST /api/v2/gateway-keys/{keyId}/reveal`

权限：`member`；F：`F14`；主要成功状态：`200`。
| 入参位置 | 字段 | 类型 | 必填 |
| --- | --- | --- | --- |
| path | `keyId` | `OpaqueId` | 是 |
| header | `Idempotency-Key` | `string` | 是 |
| header | `X-CSRF-Token` | `string` | 是 |

Body：无独立命名body，见该操作schema；Response：[GatewayKeySecret](rest-schemas.md#gatewaykeysecret)

响应顶层字段：`key`: GatewayKey（必填）；`secret`: string（必填）

涉及表（规格声明，不自动等于每次都写）：`gateway.key_bindings`
错误响应：`400`, `401`, `403`, `404`, `409`, `422`, `429`, `500`, `503`

### revokeGatewayKey

`POST /api/v2/gateway-keys/{keyId}/revoke`

权限：`member`；F：`F14`；主要成功状态：`202`。
| 入参位置 | 字段 | 类型 | 必填 |
| --- | --- | --- | --- |
| path | `keyId` | `OpaqueId` | 是 |
| header | `Idempotency-Key` | `string` | 是 |
| header | `X-CSRF-Token` | `string` | 是 |

Body：无独立命名body，见该操作schema；Response：[Operation](rest-schemas.md#operation)

响应顶层字段：`operationId`: OpaqueId（必填）；`owner`: OperationOwner（必填）；`kind`: OperationKind（必填）；`resourceId`: OpaqueId（必填）；`status`: string（必填）；`stage`: OperationStage（必填）；`observationResult`: string（可选）；`errorCode`: ErrorCode（可选）；`requestId`: OpaqueId（必填）；`createdAt`: string/date-time（必填）；`updatedAt`: string/date-time（必填）；`pollAfterSeconds`: integer（可选）

涉及表（规格声明，不自动等于每次都写）：`gateway.key_bindings`, `gateway.operations`
错误响应：`400`, `401`, `403`, `404`, `409`, `422`, `429`, `500`, `503`

### listRechargeRecords

`GET /api/v2/admin/recharge-records`

权限：`platform_admin`；F：`F14`；主要成功状态：`200`。
| 入参位置 | 字段 | 类型 | 必填 |
| --- | --- | --- | --- |
| query | `cursor` | `OpaqueId` | 否 |
| query | `limit` | `integer` | 否 |
| query | `tenantId` | `OpaqueId` | 否 |

Body：无独立命名body，见该操作schema；Response：[WalletOperationPage](rest-schemas.md#walletoperationpage)

响应顶层字段：`items`: array<WalletOperation>（必填）；`nextCursor`: OpaqueId（可选）

涉及表（规格声明，不自动等于每次都写）：`gateway.wallet_operations`
错误响应：`400`, `401`, `403`, `404`, `409`, `422`, `429`, `500`, `503`

### listModels

`GET /api/v2/catalog/models`

权限：`member`；F：`F03, F07`；主要成功状态：`200`。
| 入参位置 | 字段 | 类型 | 必填 |
| --- | --- | --- | --- |
| query | `cursor` | `OpaqueId` | 否 |
| query | `limit` | `integer` | 否 |

Body：无独立命名body，见该操作schema；Response：[ModelPage](rest-schemas.md#modelpage)

响应顶层字段：`items`: array<Model>（必填）；`nextCursor`: OpaqueId（可选）

涉及表（规格声明，不自动等于每次都写）：
错误响应：`400`, `401`, `403`, `404`, `409`, `422`, `429`, `500`, `503`

## 3. 本域数据库全字段

`opl_gateway`：9 张表，122 列。字段权威：[02](../02_database_schema_complete.md)、[SQL](../contracts/schema.sql)；以下从db_inventory派生。**表不是自动等同DDD聚合根**；事务边界见第1节。


### gateway.identity_mappings

每成员独立Gateway身份；与Tenant钱包付款主体分开

| 列 | SQL类型 | 可NULL | 默认值 | API血缘 / 原始来源 |
| --- | --- | --- | --- | --- |
| `id` | `text` | 否 | `—` | 02_database_schema_complete.md#gateway.identity_mappings.id |
| `actor_id` | `text` | 否 | `—` | 02_database_schema_complete.md#gateway.identity_mappings.actor_id |
| `sub2api_user_id` | `text` | 否 | `—` | 02_database_schema_complete.md#gateway.identity_mappings.sub2api_user_id |
| `external_identity_ref` | `text` | 否 | `—` | 02_database_schema_complete.md#gateway.identity_mappings.external_identity_ref |
| `verified_at` | `timestamptz` | 否 | `—` | 02_database_schema_complete.md#gateway.identity_mappings.verified_at |
| `created_at` | `timestamptz` | 否 | `now()` | 02_database_schema_complete.md#gateway.identity_mappings.created_at |
| `updated_at` | `timestamptz` | 否 | `now()` | 02_database_schema_complete.md#gateway.identity_mappings.updated_at |

约束：
- `PRIMARY KEY (id)`
- `UNIQUE (actor_id)`
- `UNIQUE (sub2api_user_id)`

索引：
- `{"name": "identity_mappings_external", "columns": "external_identity_ref", "unique": false, "where": null}`

### gateway.tenant_wallet_bindings

唯一活动钱包主体映射；跨成员费用动作必须外部Gateway明确委托能力，未证实不可擅自模拟

| 列 | SQL类型 | 可NULL | 默认值 | API血缘 / 原始来源 |
| --- | --- | --- | --- | --- |
| `id` | `text` | 否 | `—` | 02_database_schema_complete.md#gateway.tenant_wallet_bindings.id |
| `tenant_id` | `text` | 否 | `—` | 02_database_schema_complete.md#gateway.tenant_wallet_bindings.tenant_id |
| `billing_sub2api_user_id` | `text` | 否 | `—` | 02_database_schema_complete.md#gateway.tenant_wallet_bindings.billing_sub2api_user_id |
| `delegation_ref` | `text` | 否 | `—` | 02_database_schema_complete.md#gateway.tenant_wallet_bindings.delegation_ref |
| `verification_evidence_ref` | `text` | 否 | `—` | 02_database_schema_complete.md#gateway.tenant_wallet_bindings.verification_evidence_ref |
| `bound_at` | `timestamptz` | 否 | `—` | 02_database_schema_complete.md#gateway.tenant_wallet_bindings.bound_at |
| `revoked_at` | `timestamptz` | 是 | `—` | 02_database_schema_complete.md#gateway.tenant_wallet_bindings.revoked_at |
| `created_at` | `timestamptz` | 否 | `now()` | 02_database_schema_complete.md#gateway.tenant_wallet_bindings.created_at |
| `updated_at` | `timestamptz` | 否 | `now()` | 02_database_schema_complete.md#gateway.tenant_wallet_bindings.updated_at |

约束：
- `PRIMARY KEY (id)`

索引：
- `{"name": "wallet_binding_tenant", "columns": "tenant_id", "unique": true, "where": "revoked_at IS NULL"}`
- `{"name": "wallet_binding_subject", "columns": "billing_sub2api_user_id", "unique": true, "where": "revoked_at IS NULL"}`

### gateway.key_bindings

轮换新Key验证后撤旧Key，允许短时两绑定；Secret不进事件/日志/数据库；不储Key明文

| 列 | SQL类型 | 可NULL | 默认值 | API血缘 / 原始来源 |
| --- | --- | --- | --- | --- |
| `id` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/GatewayKey/properties/id |
| `tenant_id` | `text` | 否 | `—` | 02_database_schema_complete.md#gateway.key_bindings.tenant_id |
| `workspace_id` | `text` | 是 | `—` | 03_api_contract_complete.yaml#/components/schemas/GatewayKey/properties/workspaceId |
| `actor_id` | `text` | 是 | `—` | 02_database_schema_complete.md#gateway.key_bindings.actor_id |
| `external_key_id` | `text` | 否 | `—` | 02_database_schema_complete.md#gateway.key_bindings.external_key_id |
| `fingerprint` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/GatewayKey/properties/fingerprint |
| `secret_ref` | `text` | 是 | `—` | 02_database_schema_complete.md#gateway.key_bindings.secret_ref |
| `purpose` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/GatewayKey/properties/purpose |
| `model_ids` | `text[]` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/GatewayKey/properties/modelIds |
| `rotation_of_key_binding_id` | `text` | 是 | `—` | 02_database_schema_complete.md#gateway.key_bindings.rotation_of_key_binding_id |
| `observation_result` | `text` | 否 | `—` | 02_database_schema_complete.md#gateway.key_bindings.observation_result |
| `revoked_at` | `timestamptz` | 是 | `—` | 02_database_schema_complete.md#gateway.key_bindings.revoked_at |
| `created_at` | `timestamptz` | 否 | `now()` | 03_api_contract_complete.yaml#/components/schemas/GatewayKey/properties/createdAt |
| `updated_at` | `timestamptz` | 否 | `now()` | 02_database_schema_complete.md#gateway.key_bindings.updated_at |
| `name` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/GatewayKey/properties/name |
| `expires_at` | `timestamptz` | 是 | `—` | 03_api_contract_complete.yaml#/components/schemas/GatewayKey/properties/expiresAt |

约束：
- `PRIMARY KEY (id)`
- `FOREIGN KEY (rotation_of_key_binding_id) REFERENCES gateway.key_bindings (id) ON DELETE RESTRICT`
- `UNIQUE (external_key_id)`
- `CHECK (purpose IN ('workspace_managed','personal'))`
- `CHECK (observation_result IN ('confirmed','rejected','unknown'))`
- `CHECK ((purpose = 'workspace_managed') = (workspace_id IS NOT NULL))`
- `CHECK (purpose <> 'workspace_managed' OR secret_ref IS NOT NULL)`

索引：
- `{"name": "key_bindings_workspace", "columns": "workspace_id", "unique": false, "where": null}`
- `{"name": "key_bindings_tenant", "columns": "tenant_id, created_at DESC, id DESC", "unique": false, "where": null}`

### gateway.wallet_operations

Gateway请求事实不是wallet；unknown不得重复扣费或逆向退款；refund依原charge及资格

| 列 | SQL类型 | 可NULL | 默认值 | API血缘 / 原始来源 |
| --- | --- | --- | --- | --- |
| `id` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/WalletOperation/properties/id |
| `tenant_id` | `text` | 否 | `—` | 02_database_schema_complete.md#gateway.wallet_operations.tenant_id |
| `wallet_binding_id` | `text` | 否 | `—` | 02_database_schema_complete.md#gateway.wallet_operations.wallet_binding_id |
| `workspace_id` | `text` | 是 | `—` | 03_api_contract_complete.yaml#/components/schemas/WalletOperation/properties/workspaceId |
| `kind` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/WalletOperation/properties/kind |
| `status` | `text` | 否 | `'requested'` | 03_api_contract_complete.yaml#/components/schemas/WalletOperation/properties/status |
| `amount_usd_micros` | `bigint` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/WalletOperation/properties/amountUSDMicros |
| `original_wallet_operation_id` | `text` | 是 | `—` | 03_api_contract_complete.yaml#/components/schemas/WalletOperation/properties/originalChargeOperationId |
| `business_idempotency_key` | `text` | 否 | `—` | 02_database_schema_complete.md#gateway.wallet_operations.business_idempotency_key |
| `request_fingerprint` | `text` | 否 | `—` | 02_database_schema_complete.md#gateway.wallet_operations.request_fingerprint |
| `external_reference` | `text` | 是 | `—` | 03_api_contract_complete.yaml#/components/schemas/WalletOperation/properties/externalReference |
| `receipt_id` | `text` | 是 | `—` | 03_api_contract_complete.yaml#/components/schemas/WalletOperation/properties/receiptId |
| `refund_entitlement_ref` | `text` | 是 | `—` | 02_database_schema_complete.md#gateway.wallet_operations.refund_entitlement_ref |
| `authorization_receipt_ref` | `text` | 是 | `—` | 02_database_schema_complete.md#gateway.wallet_operations.authorization_receipt_ref |
| `error_code` | `text` | 是 | `—` | 03_api_contract_complete.yaml#/components/schemas/WalletOperation/properties/errorCode |
| `confirmed_at` | `timestamptz` | 是 | `—` | 02_database_schema_complete.md#gateway.wallet_operations.confirmed_at |
| `created_at` | `timestamptz` | 否 | `now()` | 03_api_contract_complete.yaml#/components/schemas/WalletOperation/properties/createdAt |
| `updated_at` | `timestamptz` | 否 | `now()` | 03_api_contract_complete.yaml#/components/schemas/WalletOperation/properties/updatedAt |
| `refund_entitlement_snapshot` | `jsonb` | 是 | `—` | 02_database_schema_complete.md#gateway.wallet_operations.refund_entitlement_snapshot |
| `purpose` | `text` | 是 | `—` | 03_api_contract_complete.yaml#/components/schemas/WalletOperation/properties/purpose |
| `plan_change_id` | `text` | 是 | `—` | 03_api_contract_complete.yaml#/components/schemas/WalletOperation/properties/planChangeId |
| `coverage_start` | `timestamptz` | 是 | `—` | 03_api_contract_complete.yaml#/components/schemas/WalletOperation/properties/coverageStart |
| `coverage_end` | `timestamptz` | 是 | `—` | 03_api_contract_complete.yaml#/components/schemas/WalletOperation/properties/coverageEnd |

约束：
- `PRIMARY KEY (id)`
- `FOREIGN KEY (wallet_binding_id) REFERENCES gateway.tenant_wallet_bindings (id) ON DELETE RESTRICT`
- `FOREIGN KEY (original_wallet_operation_id) REFERENCES gateway.wallet_operations (id) ON DELETE RESTRICT`
- `CHECK (kind IN ('charge','refund','recharge'))`
- `CHECK (status IN ('requested','confirmed','rejected','unknown'))`
- `UNIQUE (business_idempotency_key)`
- `CHECK (amount_usd_micros > 0)`
- `CHECK (kind <> 'refund' OR (original_wallet_operation_id IS NOT NULL AND refund_entitlement_ref IS NOT NULL))`
- `CHECK (status <> 'confirmed' OR (external_reference IS NOT NULL AND confirmed_at IS NOT NULL))`
- `CHECK (purpose IN ('base_period','upgrade_supplement','base_period_delete','upgrade_failure_full','supplement_delete_unused','next_period_plan_failure_full','recharge'))`
- `CHECK ((coverage_start IS NULL AND coverage_end IS NULL) OR (coverage_start IS NOT NULL AND coverage_end > coverage_start))`
- `CHECK (purpose <> 'upgrade_supplement' OR (kind = 'charge' AND plan_change_id IS NOT NULL AND coverage_start IS NOT NULL AND coverage_end IS NOT NULL))`
- `CHECK (purpose NOT IN ('base_period_delete','upgrade_failure_full','supplement_delete_unused','next_period_plan_failure_full') OR (kind = 'refund' AND original_wallet_operation_id IS NOT NULL AND refund_entitlement_snapshot IS NOT NULL))`

索引：
- `{"name": "wallet_operations_external", "columns": "external_reference", "unique": true, "where": "external_reference IS NOT NULL"}`
- `{"name": "wallet_operations_tenant", "columns": "tenant_id, created_at DESC, id DESC", "unique": false, "where": null}`
- `{"name": "wallet_operations_original", "columns": "original_wallet_operation_id", "unique": false, "where": null}`

### gateway.outbox_events

本Owner状态事务同写immutable事件；revision只在本Owner聚合锁内分配

| 列 | SQL类型 | 可NULL | 默认值 | API血缘 / 原始来源 |
| --- | --- | --- | --- | --- |
| `id` | `text` | 否 | `—` | 02_database_schema_complete.md#gateway.outbox_events.id |
| `event_type` | `text` | 否 | `—` | 02_database_schema_complete.md#gateway.outbox_events.event_type |
| `schema_version` | `integer` | 否 | `—` | 02_database_schema_complete.md#gateway.outbox_events.schema_version |
| `aggregate_type` | `text` | 否 | `—` | 02_database_schema_complete.md#gateway.outbox_events.aggregate_type |
| `aggregate_id` | `text` | 否 | `—` | 02_database_schema_complete.md#gateway.outbox_events.aggregate_id |
| `aggregate_revision` | `bigint` | 否 | `—` | 02_database_schema_complete.md#gateway.outbox_events.aggregate_revision |
| `tenant_id` | `text` | 是 | `—` | 02_database_schema_complete.md#gateway.outbox_events.tenant_id |
| `correlation_id` | `text` | 否 | `—` | 02_database_schema_complete.md#gateway.outbox_events.correlation_id |
| `causation_id` | `text` | 是 | `—` | 02_database_schema_complete.md#gateway.outbox_events.causation_id |
| `payload` | `jsonb` | 否 | `—` | 02_database_schema_complete.md#gateway.outbox_events.payload |
| `payload_sha256` | `text` | 否 | `—` | 02_database_schema_complete.md#gateway.outbox_events.payload_sha256 |
| `occurred_at` | `timestamptz` | 否 | `—` | 02_database_schema_complete.md#gateway.outbox_events.occurred_at |
| `created_at` | `timestamptz` | 否 | `now()` | 02_database_schema_complete.md#gateway.outbox_events.created_at |

约束：
- `PRIMARY KEY (id)`
- `CHECK (schema_version > 0 AND aggregate_revision >= 0)`
- `CHECK (payload_sha256 ~ '^[0-9a-f]{64}$')`
- `UNIQUE (aggregate_type, aggregate_id, aggregate_revision, event_type)`

索引：
- `{"name": "outbox_events_aggregate", "columns": "aggregate_type, aggregate_id, aggregate_revision", "unique": false, "where": null}`

### gateway.outbox_deliveries

各consumer独立ACK，禁止单个ACK丢其他消费者事件；lease仅本库

| 列 | SQL类型 | 可NULL | 默认值 | API血缘 / 原始来源 |
| --- | --- | --- | --- | --- |
| `id` | `text` | 否 | `—` | 02_database_schema_complete.md#gateway.outbox_deliveries.id |
| `event_id` | `text` | 否 | `—` | 02_database_schema_complete.md#gateway.outbox_deliveries.event_id |
| `consumer_owner` | `text` | 否 | `—` | 02_database_schema_complete.md#gateway.outbox_deliveries.consumer_owner |
| `attempt_count` | `integer` | 否 | `0` | 02_database_schema_complete.md#gateway.outbox_deliveries.attempt_count |
| `next_attempt_at` | `timestamptz` | 否 | `now()` | 02_database_schema_complete.md#gateway.outbox_deliveries.next_attempt_at |
| `acknowledged_at` | `timestamptz` | 是 | `—` | 02_database_schema_complete.md#gateway.outbox_deliveries.acknowledged_at |
| `last_error_code` | `text` | 是 | `—` | 02_database_schema_complete.md#gateway.outbox_deliveries.last_error_code |
| `lease_token` | `text` | 是 | `—` | 02_database_schema_complete.md#gateway.outbox_deliveries.lease_token |
| `lease_until` | `timestamptz` | 是 | `—` | 02_database_schema_complete.md#gateway.outbox_deliveries.lease_until |
| `created_at` | `timestamptz` | 否 | `now()` | 02_database_schema_complete.md#gateway.outbox_deliveries.created_at |
| `updated_at` | `timestamptz` | 否 | `now()` | 02_database_schema_complete.md#gateway.outbox_deliveries.updated_at |

约束：
- `PRIMARY KEY (id)`
- `FOREIGN KEY (event_id) REFERENCES gateway.outbox_events (id) ON DELETE RESTRICT`
- `UNIQUE (event_id, consumer_owner)`
- `CHECK (attempt_count >= 0)`
- `CHECK ((lease_token IS NULL) = (lease_until IS NULL))`

索引：
- `{"name": "outbox_deliveries_pending", "columns": "next_attempt_at, id", "unique": false, "where": "acknowledged_at IS NULL"}`

### gateway.inbox_events

去重hash/业务状态/processed_at/后续Outbox同本库事务；乱序不能覆盖新版本

| 列 | SQL类型 | 可NULL | 默认值 | API血缘 / 原始来源 |
| --- | --- | --- | --- | --- |
| `id` | `text` | 否 | `—` | 02_database_schema_complete.md#gateway.inbox_events.id |
| `source_owner` | `text` | 否 | `—` | 02_database_schema_complete.md#gateway.inbox_events.source_owner |
| `source_event_id` | `text` | 否 | `—` | 02_database_schema_complete.md#gateway.inbox_events.source_event_id |
| `event_type` | `text` | 否 | `—` | 02_database_schema_complete.md#gateway.inbox_events.event_type |
| `schema_version` | `integer` | 否 | `—` | 02_database_schema_complete.md#gateway.inbox_events.schema_version |
| `aggregate_type` | `text` | 否 | `—` | 02_database_schema_complete.md#gateway.inbox_events.aggregate_type |
| `aggregate_id` | `text` | 否 | `—` | 02_database_schema_complete.md#gateway.inbox_events.aggregate_id |
| `aggregate_revision` | `bigint` | 否 | `—` | 02_database_schema_complete.md#gateway.inbox_events.aggregate_revision |
| `payload_sha256` | `text` | 否 | `—` | 02_database_schema_complete.md#gateway.inbox_events.payload_sha256 |
| `payload` | `jsonb` | 否 | `—` | 02_database_schema_complete.md#gateway.inbox_events.payload |
| `received_at` | `timestamptz` | 否 | `now()` | 02_database_schema_complete.md#gateway.inbox_events.received_at |
| `processed_at` | `timestamptz` | 是 | `—` | 02_database_schema_complete.md#gateway.inbox_events.processed_at |
| `result_resource_id` | `text` | 是 | `—` | 02_database_schema_complete.md#gateway.inbox_events.result_resource_id |
| `error_code` | `text` | 是 | `—` | 02_database_schema_complete.md#gateway.inbox_events.error_code |

约束：
- `PRIMARY KEY (id)`
- `UNIQUE (source_owner, source_event_id)`
- `CHECK (schema_version > 0 AND aggregate_revision >= 0)`
- `CHECK (payload_sha256 ~ '^[0-9a-f]{64}$')`

索引：
- `{"name": "inbox_events_pending", "columns": "received_at, id", "unique": false, "where": "processed_at IS NULL"}`
- `{"name": "inbox_events_aggregate", "columns": "source_owner, aggregate_type, aggregate_id, aggregate_revision", "unique": false, "where": null}`

### gateway.idempotency_records

命令创建同事务；operation_name是API操作名不是新Operation ID；安全响应无Key明文；义务存续无TTL清理

| 列 | SQL类型 | 可NULL | 默认值 | API血缘 / 原始来源 |
| --- | --- | --- | --- | --- |
| `id` | `text` | 否 | `—` | 02_database_schema_complete.md#gateway.idempotency_records.id |
| `tenant_scope` | `text` | 否 | `—` | 02_database_schema_complete.md#gateway.idempotency_records.tenant_scope |
| `actor_scope` | `text` | 否 | `—` | 02_database_schema_complete.md#gateway.idempotency_records.actor_scope |
| `operation_name` | `text` | 否 | `—` | 02_database_schema_complete.md#gateway.idempotency_records.operation_name |
| `idempotency_key` | `text` | 否 | `—` | 02_database_schema_complete.md#gateway.idempotency_records.idempotency_key |
| `request_sha256` | `text` | 否 | `—` | 02_database_schema_complete.md#gateway.idempotency_records.request_sha256 |
| `resource_id` | `text` | 否 | `—` | 02_database_schema_complete.md#gateway.idempotency_records.resource_id |
| `operation_id` | `text` | 是 | `—` | 02_database_schema_complete.md#gateway.idempotency_records.operation_id |
| `response_status` | `integer` | 否 | `—` | 02_database_schema_complete.md#gateway.idempotency_records.response_status |
| `response_body` | `jsonb` | 否 | `—` | 02_database_schema_complete.md#gateway.idempotency_records.response_body |
| `created_at` | `timestamptz` | 否 | `now()` | 02_database_schema_complete.md#gateway.idempotency_records.created_at |

约束：
- `PRIMARY KEY (id)`
- `UNIQUE (tenant_scope, actor_scope, operation_name, idempotency_key)`
- `CHECK (request_sha256 ~ '^[0-9a-f]{64}$')`
- `CHECK (response_status BETWEEN 100 AND 599)`

索引：
- `{"name": "idempotency_records_resource", "columns": "resource_id", "unique": false, "where": null}`

### gateway.operations

目标Owner持异步Operation；BFF路由无中央writer；Build创建201回Job；只有Workspace配Saga

| 列 | SQL类型 | 可NULL | 默认值 | API血缘 / 原始来源 |
| --- | --- | --- | --- | --- |
| `id` | `text` | 否 | `—` | 02_database_schema_complete.md#gateway.operations.id |
| `tenant_id` | `text` | 是 | `—` | 02_database_schema_complete.md#gateway.operations.tenant_id |
| `actor_id` | `text` | 否 | `—` | 02_database_schema_complete.md#gateway.operations.actor_id |
| `kind` | `text` | 否 | `—` | 02_database_schema_complete.md#gateway.operations.kind |
| `resource_id` | `text` | 否 | `—` | 02_database_schema_complete.md#gateway.operations.resource_id |
| `status` | `text` | 否 | `'accepted'` | 02_database_schema_complete.md#gateway.operations.status |
| `stage` | `text` | 否 | `—` | 02_database_schema_complete.md#gateway.operations.stage |
| `error_code` | `text` | 是 | `—` | 02_database_schema_complete.md#gateway.operations.error_code |
| `observation_result` | `text` | 是 | `—` | 02_database_schema_complete.md#gateway.operations.observation_result |
| `request_id` | `text` | 否 | `—` | 02_database_schema_complete.md#gateway.operations.request_id |
| `accepted_input` | `jsonb` | 否 | `—` | 02_database_schema_complete.md#gateway.operations.accepted_input |
| `result` | `jsonb` | 是 | `—` | 02_database_schema_complete.md#gateway.operations.result |
| `worker_lease_token` | `text` | 是 | `—` | 02_database_schema_complete.md#gateway.operations.worker_lease_token |
| `worker_lease_until` | `timestamptz` | 是 | `—` | 02_database_schema_complete.md#gateway.operations.worker_lease_until |
| `started_at` | `timestamptz` | 是 | `—` | 02_database_schema_complete.md#gateway.operations.started_at |
| `completed_at` | `timestamptz` | 是 | `—` | 02_database_schema_complete.md#gateway.operations.completed_at |
| `created_at` | `timestamptz` | 否 | `now()` | 02_database_schema_complete.md#gateway.operations.created_at |
| `updated_at` | `timestamptz` | 否 | `now()` | 02_database_schema_complete.md#gateway.operations.updated_at |

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

### 本Owner应实现的RPC方法全集（目标，不是挂载证据）

共享OwnerOperations/CommitReadback/Inbox分别由本域实现，不形成中央业务服务；通用Operation REST由BFF按显式Owner路由；本域只负责自己的Operation与授权。

| RPC | 请求 | 响应 |
| --- | --- | --- |
| `GatewayProductService.ListWorkspaceTransactions` | [ListWorkspaceTransactionsRpcRequest](rpc-messages.md#listworkspacetransactionsrpcrequest) | [WalletOperationPage](rpc-messages.md#walletoperationpage) |
| `GatewayProductService.GetWallet` | [GetWalletRpcRequest](rpc-messages.md#getwalletrpcrequest) | [Wallet](rpc-messages.md#wallet) |
| `GatewayProductService.ListUsage` | [ListUsageRpcRequest](rpc-messages.md#listusagerpcrequest) | [UsagePage](rpc-messages.md#usagepage) |
| `GatewayProductService.ListGatewayKeys` | [ListGatewayKeysRpcRequest](rpc-messages.md#listgatewaykeysrpcrequest) | [GatewayKeyPage](rpc-messages.md#gatewaykeypage) |
| `GatewayProductService.CreateGatewayKey` | [CreateGatewayKeyRpcRequest](rpc-messages.md#creategatewaykeyrpcrequest) | [GatewayKeySecret](rpc-messages.md#gatewaykeysecret) |
| `GatewayProductService.RevealGatewayKey` | [RevealGatewayKeyRpcRequest](rpc-messages.md#revealgatewaykeyrpcrequest) | [GatewayKeySecret](rpc-messages.md#gatewaykeysecret) |
| `GatewayProductService.RevokeGatewayKey` | [RevokeGatewayKeyRpcRequest](rpc-messages.md#revokegatewaykeyrpcrequest) | [Operation](rpc-messages.md#operation) |
| `GatewayProductService.ListRechargeRecords` | [ListRechargeRecordsRpcRequest](rpc-messages.md#listrechargerecordsrpcrequest) | [WalletOperationPage](rpc-messages.md#walletoperationpage) |
| `GatewayProductService.ListModels` | [ListModelsRpcRequest](rpc-messages.md#listmodelsrpcrequest) | [ModelPage](rpc-messages.md#modelpage) |
| `OwnerOperations.Read` | [OwnerOperationRequest](rpc-messages.md#owneroperationrequest) | [Operation](rpc-messages.md#operation) |
| `OwnerOperations.Reconcile` | [ReconcileOperationRpcRequest](rpc-messages.md#reconcileoperationrpcrequest) | [Operation](rpc-messages.md#operation) |
| `OwnerCommitReadback.ReadOwnerCommit` | [ReadOwnerCommitRequest](rpc-messages.md#readownercommitrequest) | [OwnerCommitEvidence](rpc-messages.md#ownercommitevidence) |
| `GatewayCoordination.BindWallet` | [WalletBindingCommand](rpc-messages.md#walletbindingcommand) | [WalletBindingReadback](rpc-messages.md#walletbindingreadback) |
| `GatewayCoordination.Debit` | [WalletDebitCommand](rpc-messages.md#walletdebitcommand) | [WalletOperation](rpc-messages.md#walletoperation) |
| `GatewayCoordination.Refund` | [WalletRefundCommand](rpc-messages.md#walletrefundcommand) | [WalletOperation](rpc-messages.md#walletoperation) |
| `GatewayCoordination.ReadWalletAction` | [WalletReadbackRequest](rpc-messages.md#walletreadbackrequest) | [WalletOperation](rpc-messages.md#walletoperation) |
| `GatewayCoordination.CreateManagedKey` | [ManagedKeyCommand](rpc-messages.md#managedkeycommand) | [ManagedKeyBinding](rpc-messages.md#managedkeybinding) |
| `GatewayCoordination.RevokeManagedKey` | [ManagedKeyRevoke](rpc-messages.md#managedkeyrevoke) | [Operation](rpc-messages.md#operation) |
| `GatewayPlanChangeSettlement.DebitSupplement` | [PlanChangeSupplementChargeCommand](rpc-messages.md#planchangesupplementchargecommand) | [WalletOperation](rpc-messages.md#walletoperation) |
| `GatewayPlanChangeSettlement.DebitScheduledPeriod` | [ScheduledPeriodChargeCommand](rpc-messages.md#scheduledperiodchargecommand) | [WalletOperation](rpc-messages.md#walletoperation) |
| `GatewayPlanChangeSettlement.RefundFailure` | [PlanChangeFailureRefundCommand](rpc-messages.md#planchangefailurerefundcommand) | [WalletOperation](rpc-messages.md#walletoperation) |
| `GatewayPlanChangeSettlement.RefundSupplementOnDeletion` | [SupplementDeletionRefundCommand](rpc-messages.md#supplementdeletionrefundcommand) | [WalletOperation](rpc-messages.md#walletoperation) |
| `DomainInbox.Deliver` | [DeliverEventRequest](rpc-messages.md#delivereventrequest) | [InboxAck](rpc-messages.md#inboxack) |

## 4. 跨域调用：调用者 → 拥有方 → 字段 → 结果

以下只列`domain_flows.json`声明的业务边；共享通道/尚无业务边的RPC不能推断成已经实现。


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

### F14.1 gateway → tenant / CloudIdentityAuthorization.AuthorizeAction

按当前入口/阶段与06/13状态机前置执行；不是无条件调用

请求 [AuthorizationRequest](rpc-messages.md#authorizationrequest)：scope#1: AuthorizationScope；actor_id#2: string；session_id#3: optional string；authorization_context_id#4: optional string；accepted_operation_grant_id#5: optional string；audience_owner#6: OwnerEnum；action#7: AuthorizationActionEnum；resource#8: AuthorizationResource；expected_permission_version#9: optional int64；request_id#10: string

返回 [AuthorizationDecision](rpc-messages.md#authorizationdecision)：result#1: AuthorizationResult；issuer#2: AuthorizationIssuer；authorization_context_id#3: optional string；scope#4: AuthorizationScope；actor_id#5: string；session_id#6: optional string；audience_owner#7: OwnerEnum；action#8: AuthorizationActionEnum；resource#9: AuthorizationResource；permission_version#10: int64；issued_at#11: Timestamp；expires_at#12: Timestamp；denial_code#13: optional ErrorCodeEnum；accepted_operation_grant_id#14: optional string

接收方写入：

完成证据：session/grant/action/resource/audience/权限版本确切一致

失败/未知：deny不改用管理员；unknown不发后续副作用

### F14.2 bff → gateway / GatewayProductService.GetWallet

按当前入口/阶段与06/13状态机前置执行；不是无条件调用

请求 [GetWalletRpcRequest](rpc-messages.md#getwalletrpcrequest)：context#1: CallContext

返回 [Wallet](rpc-messages.md#wallet)：source#1: WalletSourceEnum；status#2: WalletStatusEnum；balance_usd_micros#3: int64；currency#4: WalletCurrencyEnum；fetched_at#5: Timestamp

接收方写入：

完成证据：Sub2API当前钱包事实

失败/未知：不可用不返回0，不复制余额表

### F14.3 bff → gateway / GatewayProductService.RevealGatewayKey

按当前入口/阶段与06/13状态机前置执行；不是无条件调用

请求 [RevealGatewayKeyRpcRequest](rpc-messages.md#revealgatewaykeyrpcrequest)：context#1: CallContext；key_id#2: string

返回 [GatewayKeySecret](rpc-messages.md#gatewaykeysecret)：key#1: GatewayKey；secret#2: string

接收方写入：

完成证据：本人或显式grant且非系统托管Key，private/no-store

失败/未知：无权限拒绝；Secret不进入幂等response缓存

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

