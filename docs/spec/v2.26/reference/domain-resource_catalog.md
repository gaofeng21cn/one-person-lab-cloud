# Resource Catalog：API、字段与交互

> 源码快照 `7f5d05fe9b855d8af216caad714cf7fc85014d3c`；这是已定义目标的派生索引，不是业务实现完成声明。回到[总览](../15_domain_alignment.md)。

## 1. 边界与继承

模块：`services/resource-catalog`。当前：**新持久化骨架；目录/报价业务RPC未注册；旧pricing仍在Control Plane**。

资源套餐、平台价格/退款/保留政策版本与不可变Quote；不拥有Token价格、容量权威或已接受Subscription。

**继承 / 提取 / 新增：** 把旧pricing.go/approved plans从Control Plane提取；Fabric提供能力事实，客户费用政策不进入provider adapter。

**旧事实：** Control Plane pricing.go/priceVersion及原billing快照；新quote不能伪造旧购买接受行为。

**事务边界：** Quote生成固定输入/政策snapshot；AcceptQuote本库锁校验并绑定唯一Workspace Operation；Workspace只保存接受事实。

**业务顺序：** BFF/Workspace询价→Capability可部署性/Workspace原账期/ Fabric可行性→Catalog精确报价→Workspace显式接受→后续不能以最新价格覆盖。

**失败 / unknown：** 政策/能力缺失拒绝；不能静默换套餐/provider，D17实际毫秒与单次ceil不可改。

## 2. 客户 REST 与后端 Owner

14 个规格REST操作；浏览器仅经BFF。表中的请求/响应为目标契约，不代表该RPC已挂载。字段展开见DTO目录。


### createQuote

`POST /api/v2/quotes`

权限：`admin, owner`；F：`F07`；主要成功状态：`201`。
| 入参位置 | 字段 | 类型 | 必填 |
| --- | --- | --- | --- |
| header | `Idempotency-Key` | `string` | 是 |
| header | `X-CSRF-Token` | `string` | 是 |

Body：[QuoteRequest](rest-schemas.md#quoterequest)；Response：[Quote](rest-schemas.md#quote)

请求顶层字段：`purpose`: string（必填）；`workspaceId`: OpaqueId（可选）；`capabilityVersionId`: OpaqueId（可选）；`computePlanId`: OpaqueId（必填）；`storagePlanId`: OpaqueId（必填）；`modelSelections`: array<ModelSelection>（必填）；`periodMonths`: integer（必填）；`scheduledPlanChangeId`: OpaqueId（可选）

响应顶层字段：`id`: OpaqueId（必填）；`purpose`: string（必填）；`workspaceId`: OpaqueId（可选）；`capabilityVersionId`: OpaqueId（可选）；`computePlanId`: OpaqueId（必填）；`storagePlanId`: OpaqueId（必填）；`modelSelections`: array<ModelSelection>（必填）；`periodMonths`: integer（必填）；`periodStart`: string/date-time（必填）；`periodEnd`: string/date-time（必填）；`pricePolicyVersionId`: OpaqueId（必填）；`refundPolicyVersionId`: OpaqueId（必填）；`retentionPolicyVersionId`: OpaqueId（必填）；`refundTerms`: string（必填）；`retentionTerms`: string（必填）；`expectedInterruption`: string（必填）；`lineItems`: array<QuoteLine>（必填）；`totalUSDMicros`: USDMicros（必填）；`status`: string（必填）；`expiresAt`: string/date-time（必填）；`createdAt`: string/date-time（必填）；`sourceSubscriptionVersion`: NonnegativeInt64（可选）；`planChangeCalculation`: PlanChangeCalculation（可选）；`scheduledPlanChangeId`: OpaqueId（可选）；`runtimeReadbackRequirement`: string（必填）

涉及表（规格声明，不自动等于每次都写）：`resource_catalog.quotes`, `resource_catalog.quote_items`
错误响应：`400`, `401`, `403`, `404`, `409`, `422`, `429`, `500`, `503`

### getQuote

`GET /api/v2/quotes/{quoteId}`

权限：`admin, owner`；F：`F07`；主要成功状态：`200`。
| 入参位置 | 字段 | 类型 | 必填 |
| --- | --- | --- | --- |
| path | `quoteId` | `OpaqueId` | 是 |

Body：无独立命名body，见该操作schema；Response：[Quote](rest-schemas.md#quote)

响应顶层字段：`id`: OpaqueId（必填）；`purpose`: string（必填）；`workspaceId`: OpaqueId（可选）；`capabilityVersionId`: OpaqueId（可选）；`computePlanId`: OpaqueId（必填）；`storagePlanId`: OpaqueId（必填）；`modelSelections`: array<ModelSelection>（必填）；`periodMonths`: integer（必填）；`periodStart`: string/date-time（必填）；`periodEnd`: string/date-time（必填）；`pricePolicyVersionId`: OpaqueId（必填）；`refundPolicyVersionId`: OpaqueId（必填）；`retentionPolicyVersionId`: OpaqueId（必填）；`refundTerms`: string（必填）；`retentionTerms`: string（必填）；`expectedInterruption`: string（必填）；`lineItems`: array<QuoteLine>（必填）；`totalUSDMicros`: USDMicros（必填）；`status`: string（必填）；`expiresAt`: string/date-time（必填）；`createdAt`: string/date-time（必填）；`sourceSubscriptionVersion`: NonnegativeInt64（可选）；`planChangeCalculation`: PlanChangeCalculation（可选）；`scheduledPlanChangeId`: OpaqueId（可选）；`runtimeReadbackRequirement`: string（必填）

涉及表（规格声明，不自动等于每次都写）：`resource_catalog.quotes`, `resource_catalog.quote_items`
错误响应：`400`, `401`, `403`, `404`, `409`, `422`, `429`, `500`, `503`

### listComputePlans

`GET /api/v2/catalog/compute-plans`

权限：`member`；F：`F03, F07`；主要成功状态：`200`。
| 入参位置 | 字段 | 类型 | 必填 |
| --- | --- | --- | --- |
| query | `cursor` | `OpaqueId` | 否 |
| query | `limit` | `integer` | 否 |
| query | `storagePlanId` | `OpaqueId` | 否 |

Body：无独立命名body，见该操作schema；Response：[ComputePlanPage](rest-schemas.md#computeplanpage)

响应顶层字段：`items`: array<ComputePlan>（必填）；`nextCursor`: OpaqueId（可选）

涉及表（规格声明，不自动等于每次都写）：`resource_catalog.compute_plans`
错误响应：`400`, `401`, `403`, `404`, `409`, `422`, `429`, `500`, `503`

### listStoragePlans

`GET /api/v2/catalog/storage-plans`

权限：`member`；F：`F03, F07`；主要成功状态：`200`。
| 入参位置 | 字段 | 类型 | 必填 |
| --- | --- | --- | --- |
| query | `cursor` | `OpaqueId` | 否 |
| query | `limit` | `integer` | 否 |
| query | `computePlanId` | `OpaqueId` | 否 |

Body：无独立命名body，见该操作schema；Response：[StoragePlanPage](rest-schemas.md#storageplanpage)

响应顶层字段：`items`: array<StoragePlan>（必填）；`nextCursor`: OpaqueId（可选）

涉及表（规格声明，不自动等于每次都写）：`resource_catalog.storage_plans`
错误响应：`400`, `401`, `403`, `404`, `409`, `422`, `429`, `500`, `503`

### createComputePlan

`POST /api/v2/admin/catalog/compute-plans`

权限：`platform_admin`；F：`F03`；主要成功状态：`201`。
| 入参位置 | 字段 | 类型 | 必填 |
| --- | --- | --- | --- |
| header | `Idempotency-Key` | `string` | 是 |
| header | `X-CSRF-Token` | `string` | 是 |

Body：[CreateComputePlanRequest](rest-schemas.md#createcomputeplanrequest)；Response：[ComputePlan](rest-schemas.md#computeplan)

请求顶层字段：`name`: string（必填）；`vcpus`: integer/int32（必填）；`memoryMiB`: integer/int32（必填）；`providerProfileId`: OpaqueId（必填）；`providerSkuId`: OpaqueId（必填）；`providerCapabilityVersion`: string（必填）；`validFrom`: string/date-time（必填）；`validUntil`: string/date-time（可选）

响应顶层字段：`id`: OpaqueId（必填）；`name`: string（必填）；`vcpus`: integer/int32（必填）；`memoryMiB`: integer/int32（必填）；`availability`: string（必填）；`billingMode`: string（必填）；`providerCapabilityVersion`: string（必填）；`monthlyPriceUSDMicros`: USDMicros（可选）；`pricePolicyVersionId`: OpaqueId（可选）；`validFrom`: string/date-time（必填）；`validUntil`: string/date-time（可选）；`createdAt`: string/date-time（必填）

涉及表（规格声明，不自动等于每次都写）：`resource_catalog.compute_plans`
错误响应：`400`, `401`, `403`, `404`, `409`, `422`, `429`, `500`, `503`

### setComputePlanAvailability

`PUT /api/v2/admin/catalog/compute-plans/{planId}/availability`

权限：`platform_admin`；F：`F03`；主要成功状态：`200`。
| 入参位置 | 字段 | 类型 | 必填 |
| --- | --- | --- | --- |
| path | `planId` | `OpaqueId` | 是 |
| header | `Idempotency-Key` | `string` | 是 |
| header | `X-CSRF-Token` | `string` | 是 |

Body：[PlanAvailabilityRequest](rest-schemas.md#planavailabilityrequest)；Response：[ComputePlan](rest-schemas.md#computeplan)

请求顶层字段：`availability`: string（必填）；`reason`: string（必填）

响应顶层字段：`id`: OpaqueId（必填）；`name`: string（必填）；`vcpus`: integer/int32（必填）；`memoryMiB`: integer/int32（必填）；`availability`: string（必填）；`billingMode`: string（必填）；`providerCapabilityVersion`: string（必填）；`monthlyPriceUSDMicros`: USDMicros（可选）；`pricePolicyVersionId`: OpaqueId（可选）；`validFrom`: string/date-time（必填）；`validUntil`: string/date-time（可选）；`createdAt`: string/date-time（必填）

涉及表（规格声明，不自动等于每次都写）：`resource_catalog.compute_plans`
错误响应：`400`, `401`, `403`, `404`, `409`, `422`, `429`, `500`, `503`

### createStoragePlan

`POST /api/v2/admin/catalog/storage-plans`

权限：`platform_admin`；F：`F03`；主要成功状态：`201`。
| 入参位置 | 字段 | 类型 | 必填 |
| --- | --- | --- | --- |
| header | `Idempotency-Key` | `string` | 是 |
| header | `X-CSRF-Token` | `string` | 是 |

Body：[CreateStoragePlanRequest](rest-schemas.md#createstorageplanrequest)；Response：[StoragePlan](rest-schemas.md#storageplan)

请求顶层字段：`name`: string（必填）；`capacityGiB`: integer/int32（必填）；`providerProfileId`: OpaqueId（必填）；`providerSkuId`: OpaqueId（必填）；`shrinkSupported`: boolean（必填）；`validFrom`: string/date-time（必填）；`validUntil`: string/date-time（可选）

响应顶层字段：`id`: OpaqueId（必填）；`name`: string（必填）；`capacityGiB`: integer/int32（必填）；`availability`: string（必填）；`billingMode`: string（必填）；`shrinkSupported`: boolean（必填）；`monthlyPriceUSDMicros`: USDMicros（可选）；`pricePolicyVersionId`: OpaqueId（可选）；`validFrom`: string/date-time（必填）；`validUntil`: string/date-time（可选）；`createdAt`: string/date-time（必填）

涉及表（规格声明，不自动等于每次都写）：`resource_catalog.storage_plans`
错误响应：`400`, `401`, `403`, `404`, `409`, `422`, `429`, `500`, `503`

### setStoragePlanAvailability

`PUT /api/v2/admin/catalog/storage-plans/{planId}/availability`

权限：`platform_admin`；F：`F03`；主要成功状态：`200`。
| 入参位置 | 字段 | 类型 | 必填 |
| --- | --- | --- | --- |
| path | `planId` | `OpaqueId` | 是 |
| header | `Idempotency-Key` | `string` | 是 |
| header | `X-CSRF-Token` | `string` | 是 |

Body：[PlanAvailabilityRequest](rest-schemas.md#planavailabilityrequest)；Response：[StoragePlan](rest-schemas.md#storageplan)

请求顶层字段：`availability`: string（必填）；`reason`: string（必填）

响应顶层字段：`id`: OpaqueId（必填）；`name`: string（必填）；`capacityGiB`: integer/int32（必填）；`availability`: string（必填）；`billingMode`: string（必填）；`shrinkSupported`: boolean（必填）；`monthlyPriceUSDMicros`: USDMicros（可选）；`pricePolicyVersionId`: OpaqueId（可选）；`validFrom`: string/date-time（必填）；`validUntil`: string/date-time（可选）；`createdAt`: string/date-time（必填）

涉及表（规格声明，不自动等于每次都写）：`resource_catalog.storage_plans`
错误响应：`400`, `401`, `403`, `404`, `409`, `422`, `429`, `500`, `503`

### listPricePolicyVersions

`GET /api/v2/admin/catalog/price-policies`

权限：`platform_admin`；F：`F03`；主要成功状态：`200`。
| 入参位置 | 字段 | 类型 | 必填 |
| --- | --- | --- | --- |
| query | `cursor` | `OpaqueId` | 否 |
| query | `limit` | `integer` | 否 |

Body：无独立命名body，见该操作schema；Response：[PricePolicyVersionPage](rest-schemas.md#pricepolicyversionpage)

响应顶层字段：`items`: array<PricePolicyVersion>（必填）；`nextCursor`: OpaqueId（可选）

涉及表（规格声明，不自动等于每次都写）：`resource_catalog.price_policy_versions`
错误响应：`400`, `401`, `403`, `404`, `409`, `422`, `429`, `500`, `503`

### createPricePolicyVersion

`POST /api/v2/admin/catalog/price-policies`

权限：`platform_admin`；F：`F03`；主要成功状态：`201`。
| 入参位置 | 字段 | 类型 | 必填 |
| --- | --- | --- | --- |
| header | `Idempotency-Key` | `string` | 是 |
| header | `X-CSRF-Token` | `string` | 是 |

Body：[CreatePricePolicyRequest](rest-schemas.md#createpricepolicyrequest)；Response：[PricePolicyVersion](rest-schemas.md#pricepolicyversion)

请求顶层字段：`versionLabel`: string（必填）；`periodMonths`: integer（必填）；`computeMonthlyUSDMicros`: USDMicros（必填）；`storageMonthlyUSDMicros`: USDMicros（必填）；`productMonthlyUSDMicros`: USDMicros（必填）；`validFrom`: string/date-time（必填）；`validUntil`: string/date-time（可选）；`computePlanId`: OpaqueId（必填）；`storagePlanId`: OpaqueId（必填）；`renewalPolicy`: RenewalPolicy（必填）；`planChangePolicyVersion`: string（必填）

响应顶层字段：`id`: OpaqueId（必填）；`versionLabel`: string（必填）；`currency`: string（必填）；`periodMonths`: integer（必填）；`computeMonthlyUSDMicros`: USDMicros（必填）；`storageMonthlyUSDMicros`: USDMicros（必填）；`productMonthlyUSDMicros`: USDMicros（必填）；`validFrom`: string/date-time（必填）；`validUntil`: string/date-time（可选）；`createdAt`: string/date-time（必填）；`computePlanId`: OpaqueId（必填）；`storagePlanId`: OpaqueId（必填）；`renewalPolicy`: RenewalPolicy（必填）；`planChangePolicyVersion`: string（必填）；`planChangePolicy`: PlanChangePolicy（必填）

涉及表（规格声明，不自动等于每次都写）：`resource_catalog.price_policy_versions`
错误响应：`400`, `401`, `403`, `404`, `409`, `422`, `429`, `500`, `503`

### listRefundPolicyVersions

`GET /api/v2/admin/catalog/refund-policies`

权限：`platform_admin`；F：`F03`；主要成功状态：`200`。
| 入参位置 | 字段 | 类型 | 必填 |
| --- | --- | --- | --- |
| query | `cursor` | `OpaqueId` | 否 |
| query | `limit` | `integer` | 否 |

Body：无独立命名body，见该操作schema；Response：[RefundPolicyVersionPage](rest-schemas.md#refundpolicyversionpage)

响应顶层字段：`items`: array<RefundPolicyVersion>（必填）；`nextCursor`: OpaqueId（可选）

涉及表（规格声明，不自动等于每次都写）：`resource_catalog.refund_policy_versions`
错误响应：`400`, `401`, `403`, `404`, `409`, `422`, `429`, `500`, `503`

### createRefundPolicyVersion

`POST /api/v2/admin/catalog/refund-policies`

权限：`platform_admin`；F：`F03`；主要成功状态：`201`。
| 入参位置 | 字段 | 类型 | 必填 |
| --- | --- | --- | --- |
| header | `Idempotency-Key` | `string` | 是 |
| header | `X-CSRF-Token` | `string` | 是 |

Body：[CreateRefundPolicyRequest](rest-schemas.md#createrefundpolicyrequest)；Response：[RefundPolicyVersion](rest-schemas.md#refundpolicyversion)

请求顶层字段：`versionLabel`: string（必填）；`algorithm`: string（必填）；`retentionPolicyVersionId`: OpaqueId（必填）；`customerTerms`: string（必填）；`validFrom`: string/date-time（必填）；`validUntil`: string/date-time（可选）

响应顶层字段：`id`: OpaqueId（必填）；`versionLabel`: string（必填）；`algorithm`: string（必填）；`retentionPolicyVersionId`: OpaqueId（必填）；`customerTerms`: string（必填）；`validFrom`: string/date-time（必填）；`validUntil`: string/date-time（可选）；`createdAt`: string/date-time（必填）

涉及表（规格声明，不自动等于每次都写）：`resource_catalog.refund_policy_versions`
错误响应：`400`, `401`, `403`, `404`, `409`, `422`, `429`, `500`, `503`

### listRetentionPolicyVersions

`GET /api/v2/admin/catalog/retention-policies`

权限：`platform_admin`；F：`F03`；主要成功状态：`200`。
| 入参位置 | 字段 | 类型 | 必填 |
| --- | --- | --- | --- |
| query | `cursor` | `OpaqueId` | 否 |
| query | `limit` | `integer` | 否 |

Body：无独立命名body，见该操作schema；Response：[RetentionPolicyVersionPage](rest-schemas.md#retentionpolicyversionpage)

响应顶层字段：`items`: array<RetentionPolicyVersion>（必填）；`nextCursor`: OpaqueId（可选）

涉及表（规格声明，不自动等于每次都写）：`resource_catalog.retention_policy_versions`
错误响应：`400`, `401`, `403`, `404`, `409`, `422`, `429`, `500`, `503`

### createRetentionPolicyVersion

`POST /api/v2/admin/catalog/retention-policies`

权限：`platform_admin`；F：`F03`；主要成功状态：`201`。
| 入参位置 | 字段 | 类型 | 必填 |
| --- | --- | --- | --- |
| header | `Idempotency-Key` | `string` | 是 |
| header | `X-CSRF-Token` | `string` | 是 |

Body：[CreateRetentionPolicyRequest](rest-schemas.md#createretentionpolicyrequest)；Response：[RetentionPolicyVersion](rest-schemas.md#retentionpolicyversion)

请求顶层字段：`versionLabel`: string（必填）；`customerTerms`: string（必填）

响应顶层字段：`id`: OpaqueId（必填）；`versionLabel`: string（必填）；`workspaceDataDisposition`: string（必填）；`packageHistoryDisposition`: string（必填）；`buildHistoryDisposition`: string（必填）；`tenantRestoreDays`: integer（必填）；`customerTerms`: string（必填）；`createdAt`: string/date-time（必填）

涉及表（规格声明，不自动等于每次都写）：`resource_catalog.retention_policy_versions`
错误响应：`400`, `401`, `403`, `404`, `409`, `422`, `429`, `500`, `503`

## 3. 本域数据库全字段

`opl_resource_catalog`：12 张表，177 列。字段权威：[02](../02_database_schema_complete.md)、[SQL](../contracts/schema.sql)；以下从db_inventory派生。**表不是自动等同DDD聚合根**；事务边界见第1节。


### resource_catalog.compute_plans

不可变发布规格/Provider能力；不接受客户任选provider；报价不是模型定价

| 列 | SQL类型 | 可NULL | 默认值 | API血缘 / 原始来源 |
| --- | --- | --- | --- | --- |
| `id` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/ComputePlan/properties/id |
| `name` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/ComputePlan/properties/name |
| `version_label` | `text` | 否 | `—` | 02_database_schema_complete.md#resource_catalog.compute_plans.version_label |
| `provider` | `text` | 否 | `—` | 02_database_schema_complete.md#resource_catalog.compute_plans.provider |
| `provider_profile_ref` | `text` | 否 | `—` | 02_database_schema_complete.md#resource_catalog.compute_plans.provider_profile_ref |
| `region` | `text` | 否 | `—` | 02_database_schema_complete.md#resource_catalog.compute_plans.region |
| `status` | `text` | 否 | `'approved'` | 02_database_schema_complete.md#resource_catalog.compute_plans.status |
| `billing_mode` | `text` | 否 | `—` | 02_database_schema_complete.md#resource_catalog.compute_plans.billing_mode |
| `valid_from` | `timestamptz` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/ComputePlan/properties/validFrom |
| `valid_until` | `timestamptz` | 是 | `—` | 03_api_contract_complete.yaml#/components/schemas/ComputePlan/properties/validUntil |
| `published_by` | `text` | 否 | `—` | 02_database_schema_complete.md#resource_catalog.compute_plans.published_by |
| `created_at` | `timestamptz` | 否 | `now()` | 03_api_contract_complete.yaml#/components/schemas/ComputePlan/properties/createdAt |
| `updated_at` | `timestamptz` | 否 | `now()` | 02_database_schema_complete.md#resource_catalog.compute_plans.updated_at |
| `provider_capability_version` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/ComputePlan/properties/providerCapabilityVersion |
| `provider_specification` | `jsonb` | 否 | `—` | 02_database_schema_complete.md#resource_catalog.compute_plans.provider_specification |
| `vcpus` | `integer` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/ComputePlan/properties/vcpus |
| `memory_mib` | `integer` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/ComputePlan/properties/memoryMiB |

约束：
- `PRIMARY KEY (id)`
- `CHECK (status IN ('approved','deprecated','revoked'))`
- `CHECK (billing_mode IN ('PREPAID_MONTHLY','LOCAL_NO_CHARGE'))`
- `UNIQUE (name, version_label, provider, region)`
- `CHECK (valid_until IS NULL OR valid_until > valid_from)`
- `CHECK (vcpus > 0 AND memory_mib > 0)`

索引：
- `{"name": "compute_plans_available", "columns": "status, valid_from DESC, id DESC", "unique": false, "where": null}`

### resource_catalog.storage_plans

不可变发布规格/Provider能力；不接受客户任选provider；报价不是模型定价

| 列 | SQL类型 | 可NULL | 默认值 | API血缘 / 原始来源 |
| --- | --- | --- | --- | --- |
| `id` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/StoragePlan/properties/id |
| `name` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/StoragePlan/properties/name |
| `version_label` | `text` | 否 | `—` | 02_database_schema_complete.md#resource_catalog.storage_plans.version_label |
| `provider` | `text` | 否 | `—` | 02_database_schema_complete.md#resource_catalog.storage_plans.provider |
| `provider_profile_ref` | `text` | 否 | `—` | 02_database_schema_complete.md#resource_catalog.storage_plans.provider_profile_ref |
| `region` | `text` | 否 | `—` | 02_database_schema_complete.md#resource_catalog.storage_plans.region |
| `status` | `text` | 否 | `'approved'` | 02_database_schema_complete.md#resource_catalog.storage_plans.status |
| `billing_mode` | `text` | 否 | `—` | 02_database_schema_complete.md#resource_catalog.storage_plans.billing_mode |
| `valid_from` | `timestamptz` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/StoragePlan/properties/validFrom |
| `valid_until` | `timestamptz` | 是 | `—` | 03_api_contract_complete.yaml#/components/schemas/StoragePlan/properties/validUntil |
| `published_by` | `text` | 否 | `—` | 02_database_schema_complete.md#resource_catalog.storage_plans.published_by |
| `created_at` | `timestamptz` | 否 | `now()` | 03_api_contract_complete.yaml#/components/schemas/StoragePlan/properties/createdAt |
| `updated_at` | `timestamptz` | 否 | `now()` | 02_database_schema_complete.md#resource_catalog.storage_plans.updated_at |
| `provider_capability_version` | `text` | 否 | `—` | 02_database_schema_complete.md#resource_catalog.storage_plans.provider_capability_version |
| `provider_specification` | `jsonb` | 否 | `—` | 02_database_schema_complete.md#resource_catalog.storage_plans.provider_specification |
| `capacity_gib` | `integer` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/StoragePlan/properties/capacityGiB |
| `shrink_supported` | `boolean` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/StoragePlan/properties/shrinkSupported |

约束：
- `PRIMARY KEY (id)`
- `CHECK (status IN ('approved','deprecated','revoked'))`
- `CHECK (billing_mode IN ('PREPAID_MONTHLY','LOCAL_NO_CHARGE'))`
- `UNIQUE (name, version_label, provider, region)`
- `CHECK (valid_until IS NULL OR valid_until > valid_from)`
- `CHECK (capacity_gib > 0)`

索引：
- `{"name": "storage_plans_available", "columns": "status, valid_from DESC, id DESC", "unique": false, "where": null}`

### resource_catalog.price_policy_versions

管理员实际批准价格、变更、续费规则；不设默认费率

| 列 | SQL类型 | 可NULL | 默认值 | API血缘 / 原始来源 |
| --- | --- | --- | --- | --- |
| `id` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/ComputePlan/properties/pricePolicyVersionId; 03_api_contract_complete.yaml#/components/schemas/StoragePlan/properties/pricePolicyVersionId; 03_api_contract_complete.yaml#/components/schemas/PricePolicyVersion/properties/id |
| `version_label` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/PricePolicyVersion/properties/versionLabel |
| `compute_plan_id` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/PricePolicyVersion/properties/computePlanId |
| `storage_plan_id` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/PricePolicyVersion/properties/storagePlanId |
| `compute_monthly_usd_micros` | `bigint` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/ComputePlan/properties/monthlyPriceUSDMicros; 03_api_contract_complete.yaml#/components/schemas/PricePolicyVersion/properties/computeMonthlyUSDMicros |
| `storage_monthly_usd_micros` | `bigint` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/StoragePlan/properties/monthlyPriceUSDMicros; 03_api_contract_complete.yaml#/components/schemas/PricePolicyVersion/properties/storageMonthlyUSDMicros |
| `product_monthly_usd_micros` | `bigint` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/PricePolicyVersion/properties/productMonthlyUSDMicros |
| `currency` | `text` | 否 | `'USD'` | 03_api_contract_complete.yaml#/components/schemas/PricePolicyVersion/properties/currency |
| `renewal_rules` | `jsonb` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/PricePolicyVersion/properties/renewalPolicy |
| `valid_from` | `timestamptz` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/PricePolicyVersion/properties/validFrom |
| `valid_until` | `timestamptz` | 是 | `—` | 03_api_contract_complete.yaml#/components/schemas/PricePolicyVersion/properties/validUntil |
| `published_by` | `text` | 否 | `—` | 02_database_schema_complete.md#resource_catalog.price_policy_versions.published_by |
| `created_at` | `timestamptz` | 否 | `now()` | 03_api_contract_complete.yaml#/components/schemas/PricePolicyVersion/properties/createdAt |
| `period_months` | `integer` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/PricePolicyVersion/properties/periodMonths |
| `plan_change_policy_version` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/PricePolicyVersion/properties/planChangePolicyVersion |

约束：
- `PRIMARY KEY (id)`
- `FOREIGN KEY (compute_plan_id) REFERENCES resource_catalog.compute_plans (id) ON DELETE RESTRICT`
- `FOREIGN KEY (storage_plan_id) REFERENCES resource_catalog.storage_plans (id) ON DELETE RESTRICT`
- `UNIQUE (version_label, compute_plan_id, storage_plan_id)`
- `CHECK (currency = 'USD')`
- `CHECK (compute_monthly_usd_micros >= 0 AND storage_monthly_usd_micros >= 0 AND product_monthly_usd_micros >= 0)`
- `CHECK (valid_until IS NULL OR valid_until > valid_from)`
- `CHECK (period_months > 0)`
- `UNIQUE (compute_plan_id, storage_plan_id, valid_from)`
- `CHECK ((renewal_rules->>'version' = 'renewal-policy/v1' AND renewal_rules->>'trigger' = 'manual_or_explicitly_consented_automatic' AND (renewal_rules->>'months')::integer = period_months AND renewal_rules->>'usesAcceptedPriceSnapshot' = 'true') IS TRUE)`
- `CHECK (plan_change_policy_version IN ('workspace-plan-change-v1'))`
- `CHECK (period_months = 1)`

索引：
- `{"name": "price_policy_versions_scope", "columns": "compute_plan_id, storage_plan_id, valid_from DESC, id DESC", "unique": false, "where": null}`

### resource_catalog.refund_policy_versions

实际批准版本化规则，不发明退款比例或自动清除期限；历史无级联

| 列 | SQL类型 | 可NULL | 默认值 | API血缘 / 原始来源 |
| --- | --- | --- | --- | --- |
| `id` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/RefundPolicyVersion/properties/id |
| `version_label` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/RefundPolicyVersion/properties/versionLabel |
| `rules` | `jsonb` | 否 | `—` | 02_database_schema_complete.md#resource_catalog.refund_policy_versions.rules |
| `customer_terms` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/RefundPolicyVersion/properties/customerTerms |
| `valid_from` | `timestamptz` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/RefundPolicyVersion/properties/validFrom |
| `valid_until` | `timestamptz` | 是 | `—` | 03_api_contract_complete.yaml#/components/schemas/RefundPolicyVersion/properties/validUntil |
| `published_by` | `text` | 否 | `—` | 02_database_schema_complete.md#resource_catalog.refund_policy_versions.published_by |
| `created_at` | `timestamptz` | 否 | `now()` | 03_api_contract_complete.yaml#/components/schemas/RefundPolicyVersion/properties/createdAt |
| `algorithm` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/RefundPolicyVersion/properties/algorithm |
| `retention_policy_version_id` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/RefundPolicyVersion/properties/retentionPolicyVersionId |

约束：
- `PRIMARY KEY (id)`
- `UNIQUE (version_label)`
- `CHECK (valid_until IS NULL OR valid_until > valid_from)`
- `CHECK (algorithm IN ('workspace-delete-refund-v1'))`
- `FOREIGN KEY (retention_policy_version_id) REFERENCES resource_catalog.retention_policy_versions (id) ON DELETE RESTRICT`

索引：
- `{"name": "refund_policy_versions_effective", "columns": "valid_from DESC, id DESC", "unique": false, "where": null}`

### resource_catalog.retention_policy_versions

实际批准版本化规则，不发明退款比例或自动清除期限；历史无级联

| 列 | SQL类型 | 可NULL | 默认值 | API血缘 / 原始来源 |
| --- | --- | --- | --- | --- |
| `id` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/RetentionPolicyVersion/properties/id |
| `version_label` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/RetentionPolicyVersion/properties/versionLabel |
| `rules` | `jsonb` | 否 | `—` | 02_database_schema_complete.md#resource_catalog.retention_policy_versions.rules |
| `customer_terms` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/RetentionPolicyVersion/properties/customerTerms |
| `valid_from` | `timestamptz` | 否 | `—` | 02_database_schema_complete.md#resource_catalog.retention_policy_versions.valid_from |
| `valid_until` | `timestamptz` | 是 | `—` | 02_database_schema_complete.md#resource_catalog.retention_policy_versions.valid_until |
| `published_by` | `text` | 否 | `—` | 02_database_schema_complete.md#resource_catalog.retention_policy_versions.published_by |
| `created_at` | `timestamptz` | 否 | `now()` | 03_api_contract_complete.yaml#/components/schemas/RetentionPolicyVersion/properties/createdAt |
| `workspace_data_disposition` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/RetentionPolicyVersion/properties/workspaceDataDisposition |
| `package_history_disposition` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/RetentionPolicyVersion/properties/packageHistoryDisposition |
| `build_history_disposition` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/RetentionPolicyVersion/properties/buildHistoryDisposition |
| `tenant_restore_days` | `integer` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/RetentionPolicyVersion/properties/tenantRestoreDays |

约束：
- `PRIMARY KEY (id)`
- `UNIQUE (version_label)`
- `CHECK (valid_until IS NULL OR valid_until > valid_from)`
- `CHECK (workspace_data_disposition IN ('destroy_after_confirmed_deletion'))`
- `CHECK (package_history_disposition IN ('retain'))`
- `CHECK (build_history_disposition IN ('retain'))`
- `CHECK (tenant_restore_days = 15)`

索引：
- `{"name": "retention_policy_versions_effective", "columns": "valid_from DESC, id DESC", "unique": false, "where": null}`

### resource_catalog.quotes

Catalog唯一writer，AcceptQuote本库锁row校验并绑定唯一Workspace Operation；Workspace只存已接受快照

| 列 | SQL类型 | 可NULL | 默认值 | API血缘 / 原始来源 |
| --- | --- | --- | --- | --- |
| `id` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/Quote/properties/id |
| `tenant_id` | `text` | 否 | `—` | 02_database_schema_complete.md#resource_catalog.quotes.tenant_id |
| `actor_id` | `text` | 否 | `—` | 02_database_schema_complete.md#resource_catalog.quotes.actor_id |
| `purpose` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/Quote/properties/purpose |
| `workspace_id` | `text` | 是 | `—` | 03_api_contract_complete.yaml#/components/schemas/Quote/properties/workspaceId |
| `capability_version_id` | `text` | 是 | `—` | 03_api_contract_complete.yaml#/components/schemas/Quote/properties/capabilityVersionId |
| `compute_plan_id` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/Quote/properties/computePlanId |
| `storage_plan_id` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/Quote/properties/storagePlanId |
| `price_policy_version_id` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/Quote/properties/pricePolicyVersionId |
| `refund_policy_version_id` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/Quote/properties/refundPolicyVersionId |
| `retention_policy_version_id` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/Quote/properties/retentionPolicyVersionId |
| `total_usd_micros` | `bigint` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/Quote/properties/totalUSDMicros |
| `period_start` | `timestamptz` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/Quote/properties/periodStart |
| `period_end` | `timestamptz` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/Quote/properties/periodEnd |
| `status` | `text` | 否 | `'offered'` | 03_api_contract_complete.yaml#/components/schemas/Quote/properties/status |
| `input_digest` | `text` | 否 | `—` | 02_database_schema_complete.md#resource_catalog.quotes.input_digest |
| `admission_snapshot` | `jsonb` | 否 | `—` | 02_database_schema_complete.md#resource_catalog.quotes.admission_snapshot |
| `accepted_by_operation_id` | `text` | 是 | `—` | 02_database_schema_complete.md#resource_catalog.quotes.accepted_by_operation_id |
| `accepted_at` | `timestamptz` | 是 | `—` | 02_database_schema_complete.md#resource_catalog.quotes.accepted_at |
| `expires_at` | `timestamptz` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/Quote/properties/expiresAt |
| `created_at` | `timestamptz` | 否 | `now()` | 03_api_contract_complete.yaml#/components/schemas/Quote/properties/createdAt |
| `model_selections` | `jsonb` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/Quote/properties/modelSelections |
| `period_months` | `integer` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/Quote/properties/periodMonths |
| `refund_terms` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/Quote/properties/refundTerms |
| `retention_terms` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/Quote/properties/retentionTerms |
| `expected_interruption` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/Quote/properties/expectedInterruption |
| `plan_change_calculation` | `jsonb` | 是 | `—` | 03_api_contract_complete.yaml#/components/schemas/Quote/properties/planChangeCalculation |
| `source_subscription_version` | `bigint` | 是 | `—` | 03_api_contract_complete.yaml#/components/schemas/Quote/properties/sourceSubscriptionVersion |
| `scheduled_plan_change_id` | `text` | 是 | `—` | 03_api_contract_complete.yaml#/components/schemas/Quote/properties/scheduledPlanChangeId |

约束：
- `PRIMARY KEY (id)`
- `FOREIGN KEY (compute_plan_id) REFERENCES resource_catalog.compute_plans (id) ON DELETE RESTRICT`
- `FOREIGN KEY (storage_plan_id) REFERENCES resource_catalog.storage_plans (id) ON DELETE RESTRICT`
- `FOREIGN KEY (price_policy_version_id) REFERENCES resource_catalog.price_policy_versions (id) ON DELETE RESTRICT`
- `FOREIGN KEY (refund_policy_version_id) REFERENCES resource_catalog.refund_policy_versions (id) ON DELETE RESTRICT`
- `FOREIGN KEY (retention_policy_version_id) REFERENCES resource_catalog.retention_policy_versions (id) ON DELETE RESTRICT`
- `CHECK (purpose IN ('deploy','resize','renew'))`
- `CHECK (status IN ('offered','accepted','expired'))`
- `CHECK (total_usd_micros >= 0)`
- `CHECK (period_end > period_start)`
- `CHECK (input_digest ~ '^sha256:[0-9a-f]{64}$')`
- `CHECK ((status = 'accepted') = (accepted_at IS NOT NULL AND accepted_by_operation_id IS NOT NULL))`
- `CHECK (period_months > 0)`
- `CHECK ((purpose = 'resize') = (plan_change_calculation IS NOT NULL))`
- `CHECK (purpose <> 'resize' OR source_subscription_version IS NOT NULL)`

索引：
- `{"name": "quotes_tenant_list", "columns": "tenant_id, created_at DESC, id DESC", "unique": false, "where": null}`
- `{"name": "quotes_accepting_operation", "columns": "accepted_by_operation_id", "unique": true, "where": "accepted_by_operation_id IS NOT NULL"}`

### resource_catalog.quote_items

All line amounts are nonnegative; total=sum(compute/storage/product)-sum(adjustment_credit); credit_source binds the exact paid period, transaction and policy

| 列 | SQL类型 | 可NULL | 默认值 | API血缘 / 原始来源 |
| --- | --- | --- | --- | --- |
| `id` | `text` | 否 | `—` | 02_database_schema_complete.md#resource_catalog.quote_items.id |
| `quote_id` | `text` | 否 | `—` | 02_database_schema_complete.md#resource_catalog.quote_items.quote_id |
| `kind` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/QuoteLine/properties/kind |
| `description` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/QuoteLine/properties/description |
| `amount_usd_micros` | `bigint` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/QuoteLine/properties/amountUSDMicros |
| `calculation` | `jsonb` | 否 | `—` | 02_database_schema_complete.md#resource_catalog.quote_items.calculation |
| `sort_order` | `integer` | 否 | `—` | 02_database_schema_complete.md#resource_catalog.quote_items.sort_order |
| `created_at` | `timestamptz` | 否 | `now()` | 02_database_schema_complete.md#resource_catalog.quote_items.created_at |
| `quantity` | `integer` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/QuoteLine/properties/quantity |
| `credit_source` | `jsonb` | 是 | `—` | 03_api_contract_complete.yaml#/components/schemas/QuoteLine/properties/creditSource |

约束：
- `PRIMARY KEY (id)`
- `FOREIGN KEY (quote_id) REFERENCES resource_catalog.quotes (id) ON DELETE RESTRICT`
- `UNIQUE (quote_id, sort_order)`
- `CHECK (sort_order >= 0)`
- `CHECK (quantity > 0)`
- `CHECK (kind IN ('compute','storage','product','adjustment_credit'))`
- `CHECK (amount_usd_micros >= 0)`
- `CHECK ((kind = 'adjustment_credit') = (credit_source IS NOT NULL))`
- `CHECK (credit_source IS NULL OR ((jsonb_typeof(credit_source) = 'object' AND credit_source ?& ARRAY['originalWalletOperationId','originalSubscriptionPeriodId','policyVersionId','creditReceiptId','amountUSDMicros'] AND (credit_source->>'amountUSDMicros')::bigint = amount_usd_micros) IS TRUE))`

索引：
- `{"name": "quote_items_quote", "columns": "quote_id, sort_order", "unique": false, "where": null}`

### resource_catalog.outbox_events

本Owner状态事务同写immutable事件；revision只在本Owner聚合锁内分配

| 列 | SQL类型 | 可NULL | 默认值 | API血缘 / 原始来源 |
| --- | --- | --- | --- | --- |
| `id` | `text` | 否 | `—` | 02_database_schema_complete.md#resource_catalog.outbox_events.id |
| `event_type` | `text` | 否 | `—` | 02_database_schema_complete.md#resource_catalog.outbox_events.event_type |
| `schema_version` | `integer` | 否 | `—` | 02_database_schema_complete.md#resource_catalog.outbox_events.schema_version |
| `aggregate_type` | `text` | 否 | `—` | 02_database_schema_complete.md#resource_catalog.outbox_events.aggregate_type |
| `aggregate_id` | `text` | 否 | `—` | 02_database_schema_complete.md#resource_catalog.outbox_events.aggregate_id |
| `aggregate_revision` | `bigint` | 否 | `—` | 02_database_schema_complete.md#resource_catalog.outbox_events.aggregate_revision |
| `tenant_id` | `text` | 是 | `—` | 02_database_schema_complete.md#resource_catalog.outbox_events.tenant_id |
| `correlation_id` | `text` | 否 | `—` | 02_database_schema_complete.md#resource_catalog.outbox_events.correlation_id |
| `causation_id` | `text` | 是 | `—` | 02_database_schema_complete.md#resource_catalog.outbox_events.causation_id |
| `payload` | `jsonb` | 否 | `—` | 02_database_schema_complete.md#resource_catalog.outbox_events.payload |
| `payload_sha256` | `text` | 否 | `—` | 02_database_schema_complete.md#resource_catalog.outbox_events.payload_sha256 |
| `occurred_at` | `timestamptz` | 否 | `—` | 02_database_schema_complete.md#resource_catalog.outbox_events.occurred_at |
| `created_at` | `timestamptz` | 否 | `now()` | 02_database_schema_complete.md#resource_catalog.outbox_events.created_at |

约束：
- `PRIMARY KEY (id)`
- `CHECK (schema_version > 0 AND aggregate_revision >= 0)`
- `CHECK (payload_sha256 ~ '^[0-9a-f]{64}$')`
- `UNIQUE (aggregate_type, aggregate_id, aggregate_revision, event_type)`

索引：
- `{"name": "outbox_events_aggregate", "columns": "aggregate_type, aggregate_id, aggregate_revision", "unique": false, "where": null}`

### resource_catalog.outbox_deliveries

各consumer独立ACK，禁止单个ACK丢其他消费者事件；lease仅本库

| 列 | SQL类型 | 可NULL | 默认值 | API血缘 / 原始来源 |
| --- | --- | --- | --- | --- |
| `id` | `text` | 否 | `—` | 02_database_schema_complete.md#resource_catalog.outbox_deliveries.id |
| `event_id` | `text` | 否 | `—` | 02_database_schema_complete.md#resource_catalog.outbox_deliveries.event_id |
| `consumer_owner` | `text` | 否 | `—` | 02_database_schema_complete.md#resource_catalog.outbox_deliveries.consumer_owner |
| `attempt_count` | `integer` | 否 | `0` | 02_database_schema_complete.md#resource_catalog.outbox_deliveries.attempt_count |
| `next_attempt_at` | `timestamptz` | 否 | `now()` | 02_database_schema_complete.md#resource_catalog.outbox_deliveries.next_attempt_at |
| `acknowledged_at` | `timestamptz` | 是 | `—` | 02_database_schema_complete.md#resource_catalog.outbox_deliveries.acknowledged_at |
| `last_error_code` | `text` | 是 | `—` | 02_database_schema_complete.md#resource_catalog.outbox_deliveries.last_error_code |
| `lease_token` | `text` | 是 | `—` | 02_database_schema_complete.md#resource_catalog.outbox_deliveries.lease_token |
| `lease_until` | `timestamptz` | 是 | `—` | 02_database_schema_complete.md#resource_catalog.outbox_deliveries.lease_until |
| `created_at` | `timestamptz` | 否 | `now()` | 02_database_schema_complete.md#resource_catalog.outbox_deliveries.created_at |
| `updated_at` | `timestamptz` | 否 | `now()` | 02_database_schema_complete.md#resource_catalog.outbox_deliveries.updated_at |

约束：
- `PRIMARY KEY (id)`
- `FOREIGN KEY (event_id) REFERENCES resource_catalog.outbox_events (id) ON DELETE RESTRICT`
- `UNIQUE (event_id, consumer_owner)`
- `CHECK (attempt_count >= 0)`
- `CHECK ((lease_token IS NULL) = (lease_until IS NULL))`

索引：
- `{"name": "outbox_deliveries_pending", "columns": "next_attempt_at, id", "unique": false, "where": "acknowledged_at IS NULL"}`

### resource_catalog.inbox_events

去重hash/业务状态/processed_at/后续Outbox同本库事务；乱序不能覆盖新版本

| 列 | SQL类型 | 可NULL | 默认值 | API血缘 / 原始来源 |
| --- | --- | --- | --- | --- |
| `id` | `text` | 否 | `—` | 02_database_schema_complete.md#resource_catalog.inbox_events.id |
| `source_owner` | `text` | 否 | `—` | 02_database_schema_complete.md#resource_catalog.inbox_events.source_owner |
| `source_event_id` | `text` | 否 | `—` | 02_database_schema_complete.md#resource_catalog.inbox_events.source_event_id |
| `event_type` | `text` | 否 | `—` | 02_database_schema_complete.md#resource_catalog.inbox_events.event_type |
| `schema_version` | `integer` | 否 | `—` | 02_database_schema_complete.md#resource_catalog.inbox_events.schema_version |
| `aggregate_type` | `text` | 否 | `—` | 02_database_schema_complete.md#resource_catalog.inbox_events.aggregate_type |
| `aggregate_id` | `text` | 否 | `—` | 02_database_schema_complete.md#resource_catalog.inbox_events.aggregate_id |
| `aggregate_revision` | `bigint` | 否 | `—` | 02_database_schema_complete.md#resource_catalog.inbox_events.aggregate_revision |
| `payload_sha256` | `text` | 否 | `—` | 02_database_schema_complete.md#resource_catalog.inbox_events.payload_sha256 |
| `payload` | `jsonb` | 否 | `—` | 02_database_schema_complete.md#resource_catalog.inbox_events.payload |
| `received_at` | `timestamptz` | 否 | `now()` | 02_database_schema_complete.md#resource_catalog.inbox_events.received_at |
| `processed_at` | `timestamptz` | 是 | `—` | 02_database_schema_complete.md#resource_catalog.inbox_events.processed_at |
| `result_resource_id` | `text` | 是 | `—` | 02_database_schema_complete.md#resource_catalog.inbox_events.result_resource_id |
| `error_code` | `text` | 是 | `—` | 02_database_schema_complete.md#resource_catalog.inbox_events.error_code |

约束：
- `PRIMARY KEY (id)`
- `UNIQUE (source_owner, source_event_id)`
- `CHECK (schema_version > 0 AND aggregate_revision >= 0)`
- `CHECK (payload_sha256 ~ '^[0-9a-f]{64}$')`

索引：
- `{"name": "inbox_events_pending", "columns": "received_at, id", "unique": false, "where": "processed_at IS NULL"}`
- `{"name": "inbox_events_aggregate", "columns": "source_owner, aggregate_type, aggregate_id, aggregate_revision", "unique": false, "where": null}`

### resource_catalog.idempotency_records

命令创建同事务；operation_name是API操作名不是新Operation ID；安全响应无Key明文；义务存续无TTL清理

| 列 | SQL类型 | 可NULL | 默认值 | API血缘 / 原始来源 |
| --- | --- | --- | --- | --- |
| `id` | `text` | 否 | `—` | 02_database_schema_complete.md#resource_catalog.idempotency_records.id |
| `tenant_scope` | `text` | 否 | `—` | 02_database_schema_complete.md#resource_catalog.idempotency_records.tenant_scope |
| `actor_scope` | `text` | 否 | `—` | 02_database_schema_complete.md#resource_catalog.idempotency_records.actor_scope |
| `operation_name` | `text` | 否 | `—` | 02_database_schema_complete.md#resource_catalog.idempotency_records.operation_name |
| `idempotency_key` | `text` | 否 | `—` | 02_database_schema_complete.md#resource_catalog.idempotency_records.idempotency_key |
| `request_sha256` | `text` | 否 | `—` | 02_database_schema_complete.md#resource_catalog.idempotency_records.request_sha256 |
| `resource_id` | `text` | 否 | `—` | 02_database_schema_complete.md#resource_catalog.idempotency_records.resource_id |
| `operation_id` | `text` | 是 | `—` | 02_database_schema_complete.md#resource_catalog.idempotency_records.operation_id |
| `response_status` | `integer` | 否 | `—` | 02_database_schema_complete.md#resource_catalog.idempotency_records.response_status |
| `response_body` | `jsonb` | 否 | `—` | 02_database_schema_complete.md#resource_catalog.idempotency_records.response_body |
| `created_at` | `timestamptz` | 否 | `now()` | 02_database_schema_complete.md#resource_catalog.idempotency_records.created_at |

约束：
- `PRIMARY KEY (id)`
- `UNIQUE (tenant_scope, actor_scope, operation_name, idempotency_key)`
- `CHECK (request_sha256 ~ '^[0-9a-f]{64}$')`
- `CHECK (response_status BETWEEN 100 AND 599)`

索引：
- `{"name": "idempotency_records_resource", "columns": "resource_id", "unique": false, "where": null}`

### resource_catalog.operations

目标Owner持异步Operation；BFF路由无中央writer；Build创建201回Job；只有Workspace配Saga

| 列 | SQL类型 | 可NULL | 默认值 | API血缘 / 原始来源 |
| --- | --- | --- | --- | --- |
| `id` | `text` | 否 | `—` | 02_database_schema_complete.md#resource_catalog.operations.id |
| `tenant_id` | `text` | 是 | `—` | 02_database_schema_complete.md#resource_catalog.operations.tenant_id |
| `actor_id` | `text` | 否 | `—` | 02_database_schema_complete.md#resource_catalog.operations.actor_id |
| `kind` | `text` | 否 | `—` | 02_database_schema_complete.md#resource_catalog.operations.kind |
| `resource_id` | `text` | 否 | `—` | 02_database_schema_complete.md#resource_catalog.operations.resource_id |
| `status` | `text` | 否 | `'accepted'` | 02_database_schema_complete.md#resource_catalog.operations.status |
| `stage` | `text` | 否 | `—` | 02_database_schema_complete.md#resource_catalog.operations.stage |
| `error_code` | `text` | 是 | `—` | 02_database_schema_complete.md#resource_catalog.operations.error_code |
| `observation_result` | `text` | 是 | `—` | 02_database_schema_complete.md#resource_catalog.operations.observation_result |
| `request_id` | `text` | 否 | `—` | 02_database_schema_complete.md#resource_catalog.operations.request_id |
| `accepted_input` | `jsonb` | 否 | `—` | 02_database_schema_complete.md#resource_catalog.operations.accepted_input |
| `result` | `jsonb` | 是 | `—` | 02_database_schema_complete.md#resource_catalog.operations.result |
| `worker_lease_token` | `text` | 是 | `—` | 02_database_schema_complete.md#resource_catalog.operations.worker_lease_token |
| `worker_lease_until` | `timestamptz` | 是 | `—` | 02_database_schema_complete.md#resource_catalog.operations.worker_lease_until |
| `started_at` | `timestamptz` | 是 | `—` | 02_database_schema_complete.md#resource_catalog.operations.started_at |
| `completed_at` | `timestamptz` | 是 | `—` | 02_database_schema_complete.md#resource_catalog.operations.completed_at |
| `created_at` | `timestamptz` | 否 | `now()` | 02_database_schema_complete.md#resource_catalog.operations.created_at |
| `updated_at` | `timestamptz` | 否 | `now()` | 02_database_schema_complete.md#resource_catalog.operations.updated_at |

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
| `ResourceCatalogProductService.CreateQuote` | [CreateQuoteRpcRequest](rpc-messages.md#createquoterpcrequest) | [Quote](rpc-messages.md#quote) |
| `ResourceCatalogProductService.GetQuote` | [GetQuoteRpcRequest](rpc-messages.md#getquoterpcrequest) | [Quote](rpc-messages.md#quote) |
| `ResourceCatalogProductService.ListComputePlans` | [ListComputePlansRpcRequest](rpc-messages.md#listcomputeplansrpcrequest) | [ComputePlanPage](rpc-messages.md#computeplanpage) |
| `ResourceCatalogProductService.ListStoragePlans` | [ListStoragePlansRpcRequest](rpc-messages.md#liststorageplansrpcrequest) | [StoragePlanPage](rpc-messages.md#storageplanpage) |
| `ResourceCatalogProductService.CreateComputePlan` | [CreateComputePlanRpcRequest](rpc-messages.md#createcomputeplanrpcrequest) | [ComputePlan](rpc-messages.md#computeplan) |
| `ResourceCatalogProductService.SetComputePlanAvailability` | [SetComputePlanAvailabilityRpcRequest](rpc-messages.md#setcomputeplanavailabilityrpcrequest) | [ComputePlan](rpc-messages.md#computeplan) |
| `ResourceCatalogProductService.CreateStoragePlan` | [CreateStoragePlanRpcRequest](rpc-messages.md#createstorageplanrpcrequest) | [StoragePlan](rpc-messages.md#storageplan) |
| `ResourceCatalogProductService.SetStoragePlanAvailability` | [SetStoragePlanAvailabilityRpcRequest](rpc-messages.md#setstorageplanavailabilityrpcrequest) | [StoragePlan](rpc-messages.md#storageplan) |
| `ResourceCatalogProductService.ListPricePolicyVersions` | [ListPricePolicyVersionsRpcRequest](rpc-messages.md#listpricepolicyversionsrpcrequest) | [PricePolicyVersionPage](rpc-messages.md#pricepolicyversionpage) |
| `ResourceCatalogProductService.CreatePricePolicyVersion` | [CreatePricePolicyVersionRpcRequest](rpc-messages.md#createpricepolicyversionrpcrequest) | [PricePolicyVersion](rpc-messages.md#pricepolicyversion) |
| `ResourceCatalogProductService.ListRefundPolicyVersions` | [ListRefundPolicyVersionsRpcRequest](rpc-messages.md#listrefundpolicyversionsrpcrequest) | [RefundPolicyVersionPage](rpc-messages.md#refundpolicyversionpage) |
| `ResourceCatalogProductService.CreateRefundPolicyVersion` | [CreateRefundPolicyVersionRpcRequest](rpc-messages.md#createrefundpolicyversionrpcrequest) | [RefundPolicyVersion](rpc-messages.md#refundpolicyversion) |
| `ResourceCatalogProductService.ListRetentionPolicyVersions` | [ListRetentionPolicyVersionsRpcRequest](rpc-messages.md#listretentionpolicyversionsrpcrequest) | [RetentionPolicyVersionPage](rpc-messages.md#retentionpolicyversionpage) |
| `ResourceCatalogProductService.CreateRetentionPolicyVersion` | [CreateRetentionPolicyVersionRpcRequest](rpc-messages.md#createretentionpolicyversionrpcrequest) | [RetentionPolicyVersion](rpc-messages.md#retentionpolicyversion) |
| `OwnerOperations.Read` | [OwnerOperationRequest](rpc-messages.md#owneroperationrequest) | [Operation](rpc-messages.md#operation) |
| `OwnerOperations.Reconcile` | [ReconcileOperationRpcRequest](rpc-messages.md#reconcileoperationrpcrequest) | [Operation](rpc-messages.md#operation) |
| `OwnerCommitReadback.ReadOwnerCommit` | [ReadOwnerCommitRequest](rpc-messages.md#readownercommitrequest) | [OwnerCommitEvidence](rpc-messages.md#ownercommitevidence) |
| `CatalogCoordination.AcceptQuote` | [AcceptQuoteRequest](rpc-messages.md#acceptquoterequest) | [QuoteAcceptance](rpc-messages.md#quoteacceptance) |
| `DomainInbox.Deliver` | [DeliverEventRequest](rpc-messages.md#delivereventrequest) | [InboxAck](rpc-messages.md#inboxack) |

## 4. 跨域调用：调用者 → 拥有方 → 字段 → 结果

以下只列`domain_flows.json`声明的业务边；共享通道/尚无业务边的RPC不能推断成已经实现。


### F03.4 bff → resource_catalog / ResourceCatalogProductService.CreatePricePolicyVersion

按当前入口/阶段与06/13状态机前置执行；不是无条件调用

请求 [CreatePricePolicyVersionRpcRequest](rpc-messages.md#createpricepolicyversionrpcrequest)：context#1: CallContext；body#2: CreatePricePolicyRequest

返回 [PricePolicyVersion](rpc-messages.md#pricepolicyversion)：id#1: string；version_label#2: string；currency#3: PricePolicyVersionCurrencyEnum；period_months#4: int32；compute_monthly_usd_micros#5: int64；storage_monthly_usd_micros#6: int64；product_monthly_usd_micros#7: int64；valid_from#8: Timestamp；valid_until#9: Timestamp；created_at#10: Timestamp；compute_plan_id#11: string；storage_plan_id#12: string；renewal_policy#13: RenewalPolicy；plan_change_policy_version#14: PricePolicyVersionPlanChangePolicyVersionEnum；plan_change_policy#15: PlanChangePolicy

接收方写入：`resource_catalog.price_policy_versions`

完成证据：套餐组合/单月/明确政策事实，pending调整策略不可报价

失败/未知：不补空JSON或猜默认金额

### F07.2 resource_catalog → workspace / WorkspaceAdmission.CheckAdmission

按当前入口/阶段与06/13状态机前置执行；不是无条件调用

请求 [AdmissionRequest](rpc-messages.md#admissionrequest)：context#1: CallContext；workspace_id#2: string；capability_version_id#3: string；compute_plan_id#4: string；storage_plan_id#5: string；model_selections#6: repeated ModelSelection；purpose#7: string

返回 [AdmissionResult](rpc-messages.md#admissionresult)：outcome#1: Observation；admission_id#2: string；capability_snapshot_digest#3: string；provider_capability_version#4: string；policy_version_id#5: string；error_code#6: string；expires_at#7: Timestamp；expected_interruption#8: string

接收方写入：

完成证据：当前版本、模型、作用域与原Workspace义务确认

失败/未知：quote并不等于容量预留，提交时复查

### F07.4 bff → resource_catalog / ResourceCatalogProductService.CreateQuote

按当前入口/阶段与06/13状态机前置执行；不是无条件调用

请求 [CreateQuoteRpcRequest](rpc-messages.md#createquoterpcrequest)：context#1: CallContext；body#2: QuoteRequest

返回 [Quote](rpc-messages.md#quote)：id#1: string；purpose#2: QuotePurposeEnum；workspace_id#3: optional string；capability_version_id#4: optional string；compute_plan_id#5: string；storage_plan_id#6: string；model_selections#7: repeated ModelSelection；period_months#8: int32；period_start#9: Timestamp；period_end#10: Timestamp；price_policy_version_id#11: string；refund_policy_version_id#12: string；retention_policy_version_id#13: string；refund_terms#14: string；retention_terms#15: string；expected_interruption#16: string；line_items#17: repeated QuoteLine；total_usd_micros#18: int64；status#19: QuoteStatusEnum；expires_at#20: Timestamp；created_at#21: Timestamp；source_subscription_version#22: optional int64；plan_change_calculation#23: PlanChangeCalculation；scheduled_plan_change_id#24: optional string；runtime_readback_requirement#25: QuoteRuntimeReadbackRequirementEnum

接收方写入：`resource_catalog.quotes`, `resource_catalog.quote_items`

完成证据：purpose/kind/金额符号与DB完全同词；总额=sum正项-credit

失败/未知：pending/缺政策拒绝，不能零价兜底

### F08.2 workspace → resource_catalog / CatalogCoordination.AcceptQuote

按当前入口/阶段与06/13状态机前置执行；不是无条件调用

请求 [AcceptQuoteRequest](rpc-messages.md#acceptquoterequest)：context#1: CallContext；quote_id#2: string；workspace_id#3: string；obligation_id#4: string；admission_id#5: string；plan_change_id#6: optional string；source_subscription_version#7: optional int64；subscription_period_obligation_id#8: optional string

返回 [QuoteAcceptance](rpc-messages.md#quoteacceptance)：quote#1: Quote；obligation_id#2: string；acceptance_id#3: string；snapshot_digest#4: string；source_financial_snapshot_bytes#5: optional bytes；source_financial_snapshot_digest#6: optional string

接收方写入：`resource_catalog.quotes`

完成证据：quoteID/inputDigest唯一绑定原operation

失败/未知：冲突拒绝，不能重报价后续跑原单

### F11.2 resource_catalog → workspace / WorkspacePlanChangeReadback.ReadSubscriptionPlanState

按当前入口/阶段与06/13状态机前置执行；不是无条件调用

请求 [ReadSubscriptionPlanStateRequest](rpc-messages.md#readsubscriptionplanstaterequest)：context#1: CallContext；workspace_id#2: string

返回 [SubscriptionPlanState](rpc-messages.md#subscriptionplanstate)：subscription_id#1: string；workspace_id#2: string；subscription_version#3: int64；current_period_id#4: string；period_start#5: Timestamp；period_end#6: Timestamp；billing_anchor_day#7: int32；compute_plan_id#8: string；storage_plan_id#9: string；accepted_price_policy_version_id#10: optional string；accepted_monthly_usd_micros#11: optional int64；unfinished_plan_change_id#12: optional string；next_period_obligation_id#13: optional string；next_period_start#14: Timestamp；next_period_end#15: Timestamp；renewal_mode#16: SubscriptionRenewalModeEnum；renewal_consent_id#17: optional string；outcome#18: Observation；other_future_committed_obligation_id#19: optional string；runtime_readback_required#20: bool；current_application_deployment_id#21: optional string；period_start_milliseconds#22: int64；period_end_milliseconds#23: int64；source_financial_snapshot_bytes#24: bytes；source_financial_snapshot_digest#25: string；source_financial_snapshot#26: SourceFinancialSnapshot

接收方写入：

完成证据：原period/S/E/已接受当前月价/当前计划和资金义务版本固定

失败/未知：已锁定其它未来账单或财务基础变化拒绝，不退旧款改价

### F11.3 resource_catalog → fabric / FabricPlanTransitionReadback.ReadApprovedPlanTransition

按当前入口/阶段与06/13状态机前置执行；不是无条件调用

请求 [PlanTransitionRequest](rpc-messages.md#plantransitionrequest)：context#1: CallContext；workspace_id#2: string；source_compute_plan_id#3: string；source_storage_plan_id#4: string；target_compute_plan_id#5: string；target_storage_plan_id#6: string；resource_set_id#7: string；expected_resource_version#8: string

返回 [ApprovedPlanTransition](rpc-messages.md#approvedplantransition)：id#1: string；workspace_id#2: string；kind#3: PlanChangeKindEnum；provider_profile_id#4: string；capability_class#5: string；source#6: ResourcePlanSnapshot；target#7: ResourcePlanSnapshot；provider_capability_version#8: string；storage_shrink_supported#9: bool；expected_interruption#10: string；reversibility#11: TransitionReversibility；admission_receipt_id#12: string；observed_at#13: Timestamp；expires_at#14: Timestamp；outcome#15: Observation；execution_plan#16: ProviderPlanChangeExecutionPlanReference

接收方写入：

完成证据：批准可比转换，固定执行策略/数据/中断能力

失败/未知：mixed/no-op/不支持缩容拒绝，不按SKU名字或价格猜方向

### F11.4 bff → resource_catalog / ResourceCatalogProductService.CreateQuote

按当前入口/阶段与06/13状态机前置执行；不是无条件调用

请求 [CreateQuoteRpcRequest](rpc-messages.md#createquoterpcrequest)：context#1: CallContext；body#2: QuoteRequest

返回 [Quote](rpc-messages.md#quote)：id#1: string；purpose#2: QuotePurposeEnum；workspace_id#3: optional string；capability_version_id#4: optional string；compute_plan_id#5: string；storage_plan_id#6: string；model_selections#7: repeated ModelSelection；period_months#8: int32；period_start#9: Timestamp；period_end#10: Timestamp；price_policy_version_id#11: string；refund_policy_version_id#12: string；retention_policy_version_id#13: string；refund_terms#14: string；retention_terms#15: string；expected_interruption#16: string；line_items#17: repeated QuoteLine；total_usd_micros#18: int64；status#19: QuoteStatusEnum；expires_at#20: Timestamp；created_at#21: Timestamp；source_subscription_version#22: optional int64；plan_change_calculation#23: PlanChangeCalculation；scheduled_plan_change_id#24: optional string；runtime_readback_requirement#25: QuoteRuntimeReadbackRequirementEnum

接收方写入：`resource_catalog.quotes`, `resource_catalog.quote_items`

完成证据：升级仅最后ceil补差，降配当前0且下期目标价单列；原T固定

失败/未知：过期/改变原计划须新quote，不接收客户自报价

### F11.6 workspace → resource_catalog / CatalogCoordination.AcceptQuote

按当前入口/阶段与06/13状态机前置执行；不是无条件调用

请求 [AcceptQuoteRequest](rpc-messages.md#acceptquoterequest)：context#1: CallContext；quote_id#2: string；workspace_id#3: string；obligation_id#4: string；admission_id#5: string；plan_change_id#6: optional string；source_subscription_version#7: optional int64；subscription_period_obligation_id#8: optional string

返回 [QuoteAcceptance](rpc-messages.md#quoteacceptance)：quote#1: Quote；obligation_id#2: string；acceptance_id#3: string；snapshot_digest#4: string；source_financial_snapshot_bytes#5: optional bytes；source_financial_snapshot_digest#6: optional string

接收方写入：`resource_catalog.quotes`

完成证据：exact quote绑定本PlanChange的初次Operation

失败/未知：不能把其它purpose/计划quote用作新购买

## 5. 事件：谁生产、谁消费、哪些字段

aggregate_type由事件精确版本的x-aggregate-identity.type派生；aggregateId须与其idPayloadField一致。revision由生产者聚合事务内分配；consumer_owner显式选择本域Inbox。字段与实现状态不得混同。


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

