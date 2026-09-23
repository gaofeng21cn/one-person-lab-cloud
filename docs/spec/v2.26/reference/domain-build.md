# Build：API、字段与交互

> 源码快照 `7f5d05fe9b855d8af216caad714cf7fc85014d3c`；这是已定义目标的派生索引，不是业务实现完成声明。回到[总览](../15_domain_alignment.md)。

## 1. 边界与继承

模块：`services/build`。当前：**新持久化骨架；真实BuildKit/Storage/Registry链未实现**。

BuildJob、固定输入snapshot、worker lease、产物和日志证据；不拥有Package/CapabilityVersion writer。

**继承 / 提取 / 新增：** 上游没有完整Agent生产链；复用现有发布描述，不另造缩减Runtime格式。

**旧事实：** 新增能力；复用packages/contracts/go里的Publisher/WorkspaceApplication约束。

**事务边界：** BuildJob与本域Operation创建同事务；保存claims后再Bind；任务状态+Outbox同事务。跨Capability不做共享事务。

**业务顺序：** 客户CreateBuild→Capability解析输入→保存job→Acquire/Bind三类输入claim→受限读取→BuildKit→push+digest读回→事件→Capability注册→原job succeeded。

**失败 / unknown：** push或注册ACK未知先按原job/digest读回；重试新job不覆盖历史，不自动再次收费。

## 2. 客户 REST 与后端 Owner

5 个规格REST操作；浏览器仅经BFF。表中的请求/响应为目标契约，不代表该RPC已挂载。字段展开见DTO目录。


### createBuild

`POST /api/v2/builds`

权限：`admin, owner`；F：`F04, F05`；主要成功状态：`201`。
| 入参位置 | 字段 | 类型 | 必填 |
| --- | --- | --- | --- |
| header | `Idempotency-Key` | `string` | 是 |
| header | `X-CSRF-Token` | `string` | 是 |

Body：[CreateBuildRequest](rest-schemas.md#createbuildrequest)；Response：[BuildJob](rest-schemas.md#buildjob)

请求顶层字段：`packageVersionId`: OpaqueId（必填）；`webuiVersionId`: OpaqueId（必填）

响应顶层字段：`id`: OpaqueId（必填）；`operationId`: OpaqueId（必填）；`packageVersionId`: OpaqueId（必填）；`runtimeVersionId`: OpaqueId（必填）；`webuiVersionId`: OpaqueId（必填）；`status`: string（必填）；`stage`: string（必填）；`artifactDigest`: Digest（可选）；`resultCapabilityVersionId`: OpaqueId（可选）；`retryOfBuildJobId`: OpaqueId（可选）；`errorCode`: ErrorCode（可选）；`createdAt`: string/date-time（必填）；`updatedAt`: string/date-time（必填）；`retryAllowed`: boolean（必填）；`inputClaimIds`: array<OpaqueId>（可选）

涉及表（规格声明，不自动等于每次都写）：`build.build_jobs`, `build.operations`
错误响应：`400`, `401`, `403`, `404`, `409`, `422`, `429`, `500`, `503`

### listBuilds

`GET /api/v2/builds`

权限：`member`；F：`F05`；主要成功状态：`200`。
| 入参位置 | 字段 | 类型 | 必填 |
| --- | --- | --- | --- |
| query | `cursor` | `OpaqueId` | 否 |
| query | `limit` | `integer` | 否 |
| query | `packageVersionId` | `OpaqueId` | 否 |

Body：无独立命名body，见该操作schema；Response：[BuildJobPage](rest-schemas.md#buildjobpage)

响应顶层字段：`items`: array<BuildJob>（必填）；`nextCursor`: OpaqueId（可选）

涉及表（规格声明，不自动等于每次都写）：`build.build_jobs`
错误响应：`400`, `401`, `403`, `404`, `409`, `422`, `429`, `500`, `503`

### getBuild

`GET /api/v2/builds/{buildId}`

权限：`member`；F：`F05`；主要成功状态：`200`。
| 入参位置 | 字段 | 类型 | 必填 |
| --- | --- | --- | --- |
| path | `buildId` | `OpaqueId` | 是 |

Body：无独立命名body，见该操作schema；Response：[BuildJob](rest-schemas.md#buildjob)

响应顶层字段：`id`: OpaqueId（必填）；`operationId`: OpaqueId（必填）；`packageVersionId`: OpaqueId（必填）；`runtimeVersionId`: OpaqueId（必填）；`webuiVersionId`: OpaqueId（必填）；`status`: string（必填）；`stage`: string（必填）；`artifactDigest`: Digest（可选）；`resultCapabilityVersionId`: OpaqueId（可选）；`retryOfBuildJobId`: OpaqueId（可选）；`errorCode`: ErrorCode（可选）；`createdAt`: string/date-time（必填）；`updatedAt`: string/date-time（必填）；`retryAllowed`: boolean（必填）；`inputClaimIds`: array<OpaqueId>（可选）

涉及表（规格声明，不自动等于每次都写）：`build.build_jobs`
错误响应：`400`, `401`, `403`, `404`, `409`, `422`, `429`, `500`, `503`

### listBuildLogs

`GET /api/v2/builds/{buildId}/logs`

权限：`member`；F：`F05`；主要成功状态：`200`。
| 入参位置 | 字段 | 类型 | 必填 |
| --- | --- | --- | --- |
| path | `buildId` | `OpaqueId` | 是 |
| query | `cursor` | `OpaqueId` | 否 |
| query | `limit` | `integer` | 否 |

Body：无独立命名body，见该操作schema；Response：[BuildLogPage](rest-schemas.md#buildlogpage)

响应顶层字段：`items`: array<BuildLog>（必填）；`nextCursor`: OpaqueId（可选）

涉及表（规格声明，不自动等于每次都写）：`build.build_logs`
错误响应：`400`, `401`, `403`, `404`, `409`, `422`, `429`, `500`, `503`

### retryBuild

`POST /api/v2/builds/{buildId}/retry`

权限：`admin, owner`；F：`F05`；主要成功状态：`201`。
| 入参位置 | 字段 | 类型 | 必填 |
| --- | --- | --- | --- |
| path | `buildId` | `OpaqueId` | 是 |
| header | `Idempotency-Key` | `string` | 是 |
| header | `X-CSRF-Token` | `string` | 是 |

Body：无独立命名body，见该操作schema；Response：[BuildJob](rest-schemas.md#buildjob)

响应顶层字段：`id`: OpaqueId（必填）；`operationId`: OpaqueId（必填）；`packageVersionId`: OpaqueId（必填）；`runtimeVersionId`: OpaqueId（必填）；`webuiVersionId`: OpaqueId（必填）；`status`: string（必填）；`stage`: string（必填）；`artifactDigest`: Digest（可选）；`resultCapabilityVersionId`: OpaqueId（可选）；`retryOfBuildJobId`: OpaqueId（可选）；`errorCode`: ErrorCode（可选）；`createdAt`: string/date-time（必填）；`updatedAt`: string/date-time（必填）；`retryAllowed`: boolean（必填）；`inputClaimIds`: array<OpaqueId>（可选）

涉及表（规格声明，不自动等于每次都写）：`build.build_jobs`, `build.operations`
错误响应：`400`, `401`, `403`, `404`, `409`, `422`, `429`, `500`, `503`

## 3. 本域数据库全字段

`opl_build`：8 张表，109 列。字段权威：[02](../02_database_schema_complete.md)、[SQL](../contracts/schema.sql)；以下从db_inventory派生。**表不是自动等同DDD聚合根**；事务边界见第1节。


### build.build_jobs

createBuild(packageVersionId,webuiVersionId)冻结批准Runtime/catalogPolicy/digests/claims；Operation与Job同库创建，注册确认后succeeded

| 列 | SQL类型 | 可NULL | 默认值 | API血缘 / 原始来源 |
| --- | --- | --- | --- | --- |
| `id` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/BuildJob/properties/id |
| `tenant_id` | `text` | 是 | `—` | 02_database_schema_complete.md#build.build_jobs.tenant_id |
| `package_version_id` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/BuildJob/properties/packageVersionId |
| `runtime_version_id` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/BuildJob/properties/runtimeVersionId |
| `webui_version_id` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/BuildJob/properties/webuiVersionId |
| `input_digest` | `text` | 否 | `—` | 02_database_schema_complete.md#build.build_jobs.input_digest |
| `input_snapshot` | `jsonb` | 否 | `—` | 02_database_schema_complete.md#build.build_jobs.input_snapshot |
| `status` | `text` | 否 | `'queued'` | 03_api_contract_complete.yaml#/components/schemas/BuildJob/properties/status |
| `stage` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/BuildJob/properties/stage |
| `artifact_digest` | `text` | 是 | `—` | 03_api_contract_complete.yaml#/components/schemas/BuildJob/properties/artifactDigest |
| `result_capability_version_id` | `text` | 是 | `—` | 03_api_contract_complete.yaml#/components/schemas/BuildJob/properties/resultCapabilityVersionId |
| `retry_of_build_job_id` | `text` | 是 | `—` | 03_api_contract_complete.yaml#/components/schemas/BuildJob/properties/retryOfBuildJobId |
| `executor_ref` | `text` | 是 | `—` | 02_database_schema_complete.md#build.build_jobs.executor_ref |
| `worker_lease_token` | `text` | 是 | `—` | 02_database_schema_complete.md#build.build_jobs.worker_lease_token |
| `worker_lease_until` | `timestamptz` | 是 | `—` | 02_database_schema_complete.md#build.build_jobs.worker_lease_until |
| `error_code` | `text` | 是 | `—` | 03_api_contract_complete.yaml#/components/schemas/BuildJob/properties/errorCode |
| `request_id` | `text` | 否 | `—` | 02_database_schema_complete.md#build.build_jobs.request_id |
| `created_by` | `text` | 否 | `—` | 02_database_schema_complete.md#build.build_jobs.created_by |
| `started_at` | `timestamptz` | 是 | `—` | 02_database_schema_complete.md#build.build_jobs.started_at |
| `finished_at` | `timestamptz` | 是 | `—` | 02_database_schema_complete.md#build.build_jobs.finished_at |
| `created_at` | `timestamptz` | 否 | `now()` | 03_api_contract_complete.yaml#/components/schemas/BuildJob/properties/createdAt |
| `updated_at` | `timestamptz` | 否 | `now()` | 03_api_contract_complete.yaml#/components/schemas/BuildJob/properties/updatedAt |
| `operation_id` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/BuildJob/properties/operationId |
| `catalog_policy_id` | `text` | 否 | `—` | 02_database_schema_complete.md#build.build_jobs.catalog_policy_id |

约束：
- `PRIMARY KEY (id)`
- `FOREIGN KEY (retry_of_build_job_id) REFERENCES build.build_jobs (id) ON DELETE RESTRICT`
- `CHECK (status IN ('queued','validating','building','pushing','registering','succeeded','failed','needs_attention'))`
- `CHECK (input_digest ~ '^sha256:[0-9a-f]{64}$')`
- `CHECK (artifact_digest IS NULL OR artifact_digest ~ '^sha256:[0-9a-f]{64}$')`
- `CHECK (status <> 'succeeded' OR (artifact_digest IS NOT NULL AND result_capability_version_id IS NOT NULL AND finished_at IS NOT NULL))`
- `CHECK ((worker_lease_token IS NULL) = (worker_lease_until IS NULL))`
- `FOREIGN KEY (operation_id) REFERENCES build.operations (id) ON DELETE RESTRICT`

索引：
- `{"name": "build_jobs_tenant_list", "columns": "tenant_id, created_at DESC, id DESC", "unique": false, "where": null}`
- `{"name": "build_jobs_dispatch", "columns": "status, created_at", "unique": false, "where": null}`
- `{"name": "build_jobs_retry", "columns": "retry_of_build_job_id", "unique": false, "where": null}`

### build.build_artifacts

Build输出不可变证据，非第二Registry/版本可见性writer

| 列 | SQL类型 | 可NULL | 默认值 | API血缘 / 原始来源 |
| --- | --- | --- | --- | --- |
| `id` | `text` | 否 | `—` | 02_database_schema_complete.md#build.build_artifacts.id |
| `build_job_id` | `text` | 否 | `—` | 02_database_schema_complete.md#build.build_artifacts.build_job_id |
| `artifact_repository` | `text` | 否 | `—` | 02_database_schema_complete.md#build.build_artifacts.artifact_repository |
| `artifact_digest` | `text` | 否 | `—` | 02_database_schema_complete.md#build.build_artifacts.artifact_digest |
| `size_bytes` | `bigint` | 否 | `—` | 02_database_schema_complete.md#build.build_artifacts.size_bytes |
| `provenance` | `jsonb` | 否 | `—` | 02_database_schema_complete.md#build.build_artifacts.provenance |
| `verification_evidence_ref` | `text` | 否 | `—` | 02_database_schema_complete.md#build.build_artifacts.verification_evidence_ref |
| `created_at` | `timestamptz` | 否 | `now()` | 02_database_schema_complete.md#build.build_artifacts.created_at |
| `deployment_descriptor` | `jsonb` | 否 | `—` | 02_database_schema_complete.md#build.build_artifacts.deployment_descriptor |
| `deployment_descriptor_digest` | `text` | 否 | `—` | 02_database_schema_complete.md#build.build_artifacts.deployment_descriptor_digest |
| `deployment_descriptor_object_ref` | `text` | 否 | `—` | 02_database_schema_complete.md#build.build_artifacts.deployment_descriptor_object_ref |

约束：
- `PRIMARY KEY (id)`
- `FOREIGN KEY (build_job_id) REFERENCES build.build_jobs (id) ON DELETE RESTRICT`
- `UNIQUE (build_job_id)`
- `CHECK (artifact_digest ~ '^sha256:[0-9a-f]{64}$')`
- `CHECK (size_bytes > 0)`
- `CHECK (deployment_descriptor_digest ~ '^sha256:[0-9a-f]{64}$')`
- `CHECK ((deployment_descriptor->>'schemaVersion' = 'opl-deployment-descriptor/v1') IS TRUE)`
- `CHECK ((deployment_descriptor #>> '{artifact,repository}' = artifact_repository) IS TRUE)`
- `CHECK ((deployment_descriptor #>> '{artifact,digest}' = artifact_digest) IS TRUE)`
- `CHECK ((deployment_descriptor #>> '{applicationRevision,image}' = artifact_repository \|\| '@' \|\| artifact_digest) IS TRUE)`

索引：
- `{"name": "build_artifacts_digest", "columns": "artifact_repository, artifact_digest", "unique": false, "where": null}`

### build.build_logs

真实日志顺序分页，写入前按明确敏感字段清单去密，不记录凭据

| 列 | SQL类型 | 可NULL | 默认值 | API血缘 / 原始来源 |
| --- | --- | --- | --- | --- |
| `id` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/BuildLog/properties/id |
| `build_job_id` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/BuildLog/properties/buildJobId |
| `sequence` | `bigint` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/BuildLog/properties/sequence |
| `stage` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/BuildLog/properties/stage |
| `message` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/BuildLog/properties/message |
| `created_at` | `timestamptz` | 否 | `now()` | 03_api_contract_complete.yaml#/components/schemas/BuildLog/properties/createdAt |
| `level` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/BuildLog/properties/level |

约束：
- `PRIMARY KEY (id)`
- `FOREIGN KEY (build_job_id) REFERENCES build.build_jobs (id) ON DELETE RESTRICT`
- `UNIQUE (build_job_id, sequence)`
- `CHECK (sequence >= 0)`
- `CHECK (level IN ('info','warning','error'))`

索引：
- `{"name": "build_logs_cursor", "columns": "build_job_id, sequence", "unique": false, "where": null}`

### build.outbox_events

本Owner状态事务同写immutable事件；revision只在本Owner聚合锁内分配

| 列 | SQL类型 | 可NULL | 默认值 | API血缘 / 原始来源 |
| --- | --- | --- | --- | --- |
| `id` | `text` | 否 | `—` | 02_database_schema_complete.md#build.outbox_events.id |
| `event_type` | `text` | 否 | `—` | 02_database_schema_complete.md#build.outbox_events.event_type |
| `schema_version` | `integer` | 否 | `—` | 02_database_schema_complete.md#build.outbox_events.schema_version |
| `aggregate_type` | `text` | 否 | `—` | 02_database_schema_complete.md#build.outbox_events.aggregate_type |
| `aggregate_id` | `text` | 否 | `—` | 02_database_schema_complete.md#build.outbox_events.aggregate_id |
| `aggregate_revision` | `bigint` | 否 | `—` | 02_database_schema_complete.md#build.outbox_events.aggregate_revision |
| `tenant_id` | `text` | 是 | `—` | 02_database_schema_complete.md#build.outbox_events.tenant_id |
| `correlation_id` | `text` | 否 | `—` | 02_database_schema_complete.md#build.outbox_events.correlation_id |
| `causation_id` | `text` | 是 | `—` | 02_database_schema_complete.md#build.outbox_events.causation_id |
| `payload` | `jsonb` | 否 | `—` | 02_database_schema_complete.md#build.outbox_events.payload |
| `payload_sha256` | `text` | 否 | `—` | 02_database_schema_complete.md#build.outbox_events.payload_sha256 |
| `occurred_at` | `timestamptz` | 否 | `—` | 02_database_schema_complete.md#build.outbox_events.occurred_at |
| `created_at` | `timestamptz` | 否 | `now()` | 02_database_schema_complete.md#build.outbox_events.created_at |

约束：
- `PRIMARY KEY (id)`
- `CHECK (schema_version > 0 AND aggregate_revision >= 0)`
- `CHECK (payload_sha256 ~ '^[0-9a-f]{64}$')`
- `UNIQUE (aggregate_type, aggregate_id, aggregate_revision, event_type)`

索引：
- `{"name": "outbox_events_aggregate", "columns": "aggregate_type, aggregate_id, aggregate_revision", "unique": false, "where": null}`

### build.outbox_deliveries

各consumer独立ACK，禁止单个ACK丢其他消费者事件；lease仅本库

| 列 | SQL类型 | 可NULL | 默认值 | API血缘 / 原始来源 |
| --- | --- | --- | --- | --- |
| `id` | `text` | 否 | `—` | 02_database_schema_complete.md#build.outbox_deliveries.id |
| `event_id` | `text` | 否 | `—` | 02_database_schema_complete.md#build.outbox_deliveries.event_id |
| `consumer_owner` | `text` | 否 | `—` | 02_database_schema_complete.md#build.outbox_deliveries.consumer_owner |
| `attempt_count` | `integer` | 否 | `0` | 02_database_schema_complete.md#build.outbox_deliveries.attempt_count |
| `next_attempt_at` | `timestamptz` | 否 | `now()` | 02_database_schema_complete.md#build.outbox_deliveries.next_attempt_at |
| `acknowledged_at` | `timestamptz` | 是 | `—` | 02_database_schema_complete.md#build.outbox_deliveries.acknowledged_at |
| `last_error_code` | `text` | 是 | `—` | 02_database_schema_complete.md#build.outbox_deliveries.last_error_code |
| `lease_token` | `text` | 是 | `—` | 02_database_schema_complete.md#build.outbox_deliveries.lease_token |
| `lease_until` | `timestamptz` | 是 | `—` | 02_database_schema_complete.md#build.outbox_deliveries.lease_until |
| `created_at` | `timestamptz` | 否 | `now()` | 02_database_schema_complete.md#build.outbox_deliveries.created_at |
| `updated_at` | `timestamptz` | 否 | `now()` | 02_database_schema_complete.md#build.outbox_deliveries.updated_at |

约束：
- `PRIMARY KEY (id)`
- `FOREIGN KEY (event_id) REFERENCES build.outbox_events (id) ON DELETE RESTRICT`
- `UNIQUE (event_id, consumer_owner)`
- `CHECK (attempt_count >= 0)`
- `CHECK ((lease_token IS NULL) = (lease_until IS NULL))`

索引：
- `{"name": "outbox_deliveries_pending", "columns": "next_attempt_at, id", "unique": false, "where": "acknowledged_at IS NULL"}`

### build.inbox_events

去重hash/业务状态/processed_at/后续Outbox同本库事务；乱序不能覆盖新版本

| 列 | SQL类型 | 可NULL | 默认值 | API血缘 / 原始来源 |
| --- | --- | --- | --- | --- |
| `id` | `text` | 否 | `—` | 02_database_schema_complete.md#build.inbox_events.id |
| `source_owner` | `text` | 否 | `—` | 02_database_schema_complete.md#build.inbox_events.source_owner |
| `source_event_id` | `text` | 否 | `—` | 02_database_schema_complete.md#build.inbox_events.source_event_id |
| `event_type` | `text` | 否 | `—` | 02_database_schema_complete.md#build.inbox_events.event_type |
| `schema_version` | `integer` | 否 | `—` | 02_database_schema_complete.md#build.inbox_events.schema_version |
| `aggregate_type` | `text` | 否 | `—` | 02_database_schema_complete.md#build.inbox_events.aggregate_type |
| `aggregate_id` | `text` | 否 | `—` | 02_database_schema_complete.md#build.inbox_events.aggregate_id |
| `aggregate_revision` | `bigint` | 否 | `—` | 02_database_schema_complete.md#build.inbox_events.aggregate_revision |
| `payload_sha256` | `text` | 否 | `—` | 02_database_schema_complete.md#build.inbox_events.payload_sha256 |
| `payload` | `jsonb` | 否 | `—` | 02_database_schema_complete.md#build.inbox_events.payload |
| `received_at` | `timestamptz` | 否 | `now()` | 02_database_schema_complete.md#build.inbox_events.received_at |
| `processed_at` | `timestamptz` | 是 | `—` | 02_database_schema_complete.md#build.inbox_events.processed_at |
| `result_resource_id` | `text` | 是 | `—` | 02_database_schema_complete.md#build.inbox_events.result_resource_id |
| `error_code` | `text` | 是 | `—` | 02_database_schema_complete.md#build.inbox_events.error_code |

约束：
- `PRIMARY KEY (id)`
- `UNIQUE (source_owner, source_event_id)`
- `CHECK (schema_version > 0 AND aggregate_revision >= 0)`
- `CHECK (payload_sha256 ~ '^[0-9a-f]{64}$')`

索引：
- `{"name": "inbox_events_pending", "columns": "received_at, id", "unique": false, "where": "processed_at IS NULL"}`
- `{"name": "inbox_events_aggregate", "columns": "source_owner, aggregate_type, aggregate_id, aggregate_revision", "unique": false, "where": null}`

### build.idempotency_records

命令创建同事务；operation_name是API操作名不是新Operation ID；安全响应无Key明文；义务存续无TTL清理

| 列 | SQL类型 | 可NULL | 默认值 | API血缘 / 原始来源 |
| --- | --- | --- | --- | --- |
| `id` | `text` | 否 | `—` | 02_database_schema_complete.md#build.idempotency_records.id |
| `tenant_scope` | `text` | 否 | `—` | 02_database_schema_complete.md#build.idempotency_records.tenant_scope |
| `actor_scope` | `text` | 否 | `—` | 02_database_schema_complete.md#build.idempotency_records.actor_scope |
| `operation_name` | `text` | 否 | `—` | 02_database_schema_complete.md#build.idempotency_records.operation_name |
| `idempotency_key` | `text` | 否 | `—` | 02_database_schema_complete.md#build.idempotency_records.idempotency_key |
| `request_sha256` | `text` | 否 | `—` | 02_database_schema_complete.md#build.idempotency_records.request_sha256 |
| `resource_id` | `text` | 否 | `—` | 02_database_schema_complete.md#build.idempotency_records.resource_id |
| `operation_id` | `text` | 是 | `—` | 02_database_schema_complete.md#build.idempotency_records.operation_id |
| `response_status` | `integer` | 否 | `—` | 02_database_schema_complete.md#build.idempotency_records.response_status |
| `response_body` | `jsonb` | 否 | `—` | 02_database_schema_complete.md#build.idempotency_records.response_body |
| `created_at` | `timestamptz` | 否 | `now()` | 02_database_schema_complete.md#build.idempotency_records.created_at |

约束：
- `PRIMARY KEY (id)`
- `UNIQUE (tenant_scope, actor_scope, operation_name, idempotency_key)`
- `CHECK (request_sha256 ~ '^[0-9a-f]{64}$')`
- `CHECK (response_status BETWEEN 100 AND 599)`

索引：
- `{"name": "idempotency_records_resource", "columns": "resource_id", "unique": false, "where": null}`

### build.operations

目标Owner持异步Operation；BFF路由无中央writer；Build创建201回Job；只有Workspace配Saga

| 列 | SQL类型 | 可NULL | 默认值 | API血缘 / 原始来源 |
| --- | --- | --- | --- | --- |
| `id` | `text` | 否 | `—` | 02_database_schema_complete.md#build.operations.id |
| `tenant_id` | `text` | 是 | `—` | 02_database_schema_complete.md#build.operations.tenant_id |
| `actor_id` | `text` | 否 | `—` | 02_database_schema_complete.md#build.operations.actor_id |
| `kind` | `text` | 否 | `—` | 02_database_schema_complete.md#build.operations.kind |
| `resource_id` | `text` | 否 | `—` | 02_database_schema_complete.md#build.operations.resource_id |
| `status` | `text` | 否 | `'accepted'` | 02_database_schema_complete.md#build.operations.status |
| `stage` | `text` | 否 | `—` | 02_database_schema_complete.md#build.operations.stage |
| `error_code` | `text` | 是 | `—` | 02_database_schema_complete.md#build.operations.error_code |
| `observation_result` | `text` | 是 | `—` | 02_database_schema_complete.md#build.operations.observation_result |
| `request_id` | `text` | 否 | `—` | 02_database_schema_complete.md#build.operations.request_id |
| `accepted_input` | `jsonb` | 否 | `—` | 02_database_schema_complete.md#build.operations.accepted_input |
| `result` | `jsonb` | 是 | `—` | 02_database_schema_complete.md#build.operations.result |
| `worker_lease_token` | `text` | 是 | `—` | 02_database_schema_complete.md#build.operations.worker_lease_token |
| `worker_lease_until` | `timestamptz` | 是 | `—` | 02_database_schema_complete.md#build.operations.worker_lease_until |
| `started_at` | `timestamptz` | 是 | `—` | 02_database_schema_complete.md#build.operations.started_at |
| `completed_at` | `timestamptz` | 是 | `—` | 02_database_schema_complete.md#build.operations.completed_at |
| `created_at` | `timestamptz` | 否 | `now()` | 02_database_schema_complete.md#build.operations.created_at |
| `updated_at` | `timestamptz` | 否 | `now()` | 02_database_schema_complete.md#build.operations.updated_at |

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
| `BuildProductService.CreateBuild` | [CreateBuildRpcRequest](rpc-messages.md#createbuildrpcrequest) | [BuildJob](rpc-messages.md#buildjob) |
| `BuildProductService.ListBuilds` | [ListBuildsRpcRequest](rpc-messages.md#listbuildsrpcrequest) | [BuildJobPage](rpc-messages.md#buildjobpage) |
| `BuildProductService.GetBuild` | [GetBuildRpcRequest](rpc-messages.md#getbuildrpcrequest) | [BuildJob](rpc-messages.md#buildjob) |
| `BuildProductService.ListBuildLogs` | [ListBuildLogsRpcRequest](rpc-messages.md#listbuildlogsrpcrequest) | [BuildLogPage](rpc-messages.md#buildlogpage) |
| `BuildProductService.RetryBuild` | [RetryBuildRpcRequest](rpc-messages.md#retrybuildrpcrequest) | [BuildJob](rpc-messages.md#buildjob) |
| `ClaimUsageReadback.ReadClaimUsage` | [ReadClaimUsageRequest](rpc-messages.md#readclaimusagerequest) | [ClaimUsageEvidence](rpc-messages.md#claimusageevidence) |
| `BuildCoordination.ReadArtifact` | [ReadBuildArtifactRequest](rpc-messages.md#readbuildartifactrequest) | [BuildArtifactReadback](rpc-messages.md#buildartifactreadback) |
| `OwnerOperations.Read` | [OwnerOperationRequest](rpc-messages.md#owneroperationrequest) | [Operation](rpc-messages.md#operation) |
| `OwnerOperations.Reconcile` | [ReconcileOperationRpcRequest](rpc-messages.md#reconcileoperationrpcrequest) | [Operation](rpc-messages.md#operation) |
| `OwnerCommitReadback.ReadOwnerCommit` | [ReadOwnerCommitRequest](rpc-messages.md#readownercommitrequest) | [OwnerCommitEvidence](rpc-messages.md#ownercommitevidence) |
| `DomainInbox.Deliver` | [DeliverEventRequest](rpc-messages.md#delivereventrequest) | [InboxAck](rpc-messages.md#inboxack) |

## 4. 跨域调用：调用者 → 拥有方 → 字段 → 结果

以下只列`domain_flows.json`声明的业务边；共享通道/尚无业务边的RPC不能推断成已经实现。


### F05.1 build → capability / CapabilityCoordination.ResolveBuildInput

按当前入口/阶段与06/13状态机前置执行；不是无条件调用

请求 [BuildInputRequest](rpc-messages.md#buildinputrequest)：context#1: CallContext；package_version_id#2: string；webui_version_id#3: string

返回 [BuildInputSnapshot](rpc-messages.md#buildinputsnapshot)：package_id#1: string；package_version_id#2: string；package_object#3: SourceObjectReference；runtime_version_id#4: string；runtime_artifact#5: ArtifactReference；webui_version_id#6: string；webui_artifact#7: ArtifactReference；runtime_contract#8: RuntimePublisherContract；webui_contract#9: WebuiPublisherContract；snapshot_digest#10: string；runtime_contract_reference#11: PublisherContractReference；webui_contract_reference#12: PublisherContractReference；package_claim_id#13: string；runtime_claim_id#14: string；webui_claim_id#15: string

接收方写入：

完成证据：Package/WebUI/Runtime策略和完整发布描述冻结

失败/未知：读失败还未建业务副作用，不能选latest替代

### F05.2 bff → build / BuildProductService.CreateBuild

按当前入口/阶段与06/13状态机前置执行；不是无条件调用

请求 [CreateBuildRpcRequest](rpc-messages.md#createbuildrpcrequest)：context#1: CallContext；body#2: CreateBuildRequest

返回 [BuildJob](rpc-messages.md#buildjob)：id#1: string；operation_id#2: string；package_version_id#3: string；runtime_version_id#4: string；webui_version_id#5: string；status#6: BuildJobStatusEnum；stage#7: string；artifact_digest#8: optional string；result_capability_version_id#9: optional string；retry_of_build_job_id#10: optional string；error_code#11: optional ErrorCodeEnum；created_at#12: Timestamp；updated_at#13: Timestamp；retry_allowed#14: bool；input_claim_ids#15: repeated string

接收方写入：`build.build_jobs`, `build.operations`

完成证据：Job queued+snapshot+Operation+幂等同事务

失败/未知：响应丢失取回原身份，不建第二任务

### F05.3 build → capability / CapabilityCoordination.AcquireReference

按当前入口/阶段与06/13状态机前置执行；不是无条件调用

请求 [ReferenceClaimRequest](rpc-messages.md#referenceclaimrequest)：context#1: CallContext；target#2: ReferenceTarget；claimant_owner#3: OwnerEnum；claimant_resource_id#4: string

返回 [ReferenceClaim](rpc-messages.md#referenceclaim)：id#1: string；target#2: ReferenceTarget；claimant_owner#3: OwnerEnum；claimant_resource_id#4: string；state#5: ReferenceClaimState；bound_operation_id#6: optional string；bound_input_digest#7: optional string；acquired_at#8: Timestamp；released_at#9: optional Timestamp

接收方写入：`capability.reference_claims`

完成证据：PackageVersion/RuntimeVersion/WebuiVersion三target均绑定本job

失败/未知：任一拒绝则不读字节/不build；保留已取得claim待确定收尾

### F05.4 build → capability / CapabilityCoordination.BindReference

按当前入口/阶段与06/13状态机前置执行；不是无条件调用

请求 [BindReferenceRequest](rpc-messages.md#bindreferencerequest)：context#1: CallContext；claim_id#2: string；owner_commit_evidence#3: OwnerCommitEvidence

返回 [ReferenceClaim](rpc-messages.md#referenceclaim)：id#1: string；target#2: ReferenceTarget；claimant_owner#3: OwnerEnum；claimant_resource_id#4: string；state#5: ReferenceClaimState；bound_operation_id#6: optional string；bound_input_digest#7: optional string；acquired_at#8: Timestamp；released_at#9: optional Timestamp

接收方写入：`capability.reference_claims`

完成证据：Build本域保存claim IDs后的OwnerCommitEvidence可读回

失败/未知：Bind丢响应查原claim，不自动TTL释放

### F05.5 capability → build / BuildCoordination.ReadArtifact

按当前入口/阶段与06/13状态机前置执行；不是无条件调用

请求 [ReadBuildArtifactRequest](rpc-messages.md#readbuildartifactrequest)：context#1: CallContext；build_job_id#2: string

返回 [BuildArtifactReadback](rpc-messages.md#buildartifactreadback)：build_job_id#1: string；input#2: BuildInputSnapshot；artifact#3: ArtifactReference；artifact_receipt_id#4: string；version_label#5: string；model_requirements#6: repeated ModelRequirement；data_compatibility#7: DataCompatibility；outcome#8: Observation；deployment_descriptor#9: DeploymentDescriptor；deployment_descriptor_digest#10: string；deployment_descriptor_object_ref#11: string

接收方写入：

完成证据：repository+digest+platform+DeploymentDescriptor与远端制品相同

失败/未知：不是push接受就注册，unknown继续读原产物

### F05.6 build → capability / DomainInbox.Deliver

按当前入口/阶段与06/13状态机前置执行；不是无条件调用

请求 [DeliverEventRequest](rpc-messages.md#delivereventrequest)：event#1: EventEnvelope；authenticated_producer#2: string；consumer_owner#3: OwnerEnum

返回 [InboxAck](rpc-messages.md#inboxack)：event_id#1: string；consumer#2: string；committed#3: bool；duplicate#4: bool；applied_aggregate_version#5: int64；rejection_code#6: string

接收方写入：`capability.capability_versions`

完成证据：Inbox事务去重后唯一版本，事件与Build真实readback一致

失败/未知：同eventId重复ACK，乱序不覆盖新事实

### F05.7 build → ledger / LedgerCoordination.AppendReceipt

按当前入口/阶段与06/13状态机前置执行；不是无条件调用

请求 [AppendReceiptRequest](rpc-messages.md#appendreceiptrequest)：context#1: CallContext；receipt#2: Receipt；evidence_digest#3: string；owner_evidence_reference#4: string

返回 [Receipt](rpc-messages.md#receipt)：id#1: string；kind#2: ReceiptKindEnum；owner#3: OwnerEnum；source_sha#4: optional string；artifact_digest#5: optional string；operation_id#6: optional string；workflow_run_id#7: optional string；outcome#8: ReceiptOutcomeEnum；evidence_summary#9: string；created_at#10: Timestamp

接收方写入：`ledger.receipts`

完成证据：返回receipt身份，另ReadReceiptByReference核对原输入

失败/未知：同idempotency key读回，不填假Workspace或重写receipt

## 5. 事件：谁生产、谁消费、哪些字段

aggregate_type由事件精确版本的x-aggregate-identity.type派生；aggregateId须与其idPayloadField一致。revision由生产者聚合事务内分配；consumer_owner显式选择本域Inbox。字段与实现状态不得混同。


### build.artifact_confirmed.v1

`build` → `capability`, `ledger`

聚合类型：`build_job`；ID来源：`payload.buildJobId`（须等于aggregateId）

Envelope字段：`eventId`, `eventType`, `schemaVersion`, `owner`, `tenantId`, `aggregateId`, `aggregateVersion`, `occurredAt`, `requestId`, `payload`, `scope`

| payload字段 | 类型 | 必填 | 约束 |
| --- | --- | --- | --- |
| `buildJobId` | `string` | 是 | minLength=1 |
| `packageVersionId` | `string` | 是 | minLength=1 |
| `runtimeVersionId` | `string` | 是 | minLength=1 |
| `webuiVersionId` | `string` | 是 | minLength=1 |
| `artifactDigest` | `string` | 是 | pattern="^sha256:[0-9a-f]{64}$" |
| `artifactReceiptId` | `string` | 是 | minLength=1 |
| `deploymentDescriptorDigest` | `string` | 是 | pattern="^sha256:[0-9a-f]{64}$" |

制品已push并读回，Capability唯一writer登记version


### capability.version_registered.v1

`capability` → `build`, `ledger`

聚合类型：`capability_version`；ID来源：`payload.capabilityVersionId`（须等于aggregateId）

Envelope字段：`eventId`, `eventType`, `schemaVersion`, `owner`, `tenantId`, `aggregateId`, `aggregateVersion`, `occurredAt`, `requestId`, `payload`, `scope`

| payload字段 | 类型 | 必填 | 约束 |
| --- | --- | --- | --- |
| `capabilityVersionId` | `string` | 是 | minLength=1 |
| `buildJobId` | `string` | 是 | minLength=1 |
| `artifactDigest` | `string` | 是 | pattern="^sha256:[0-9a-f]{64}$" |

Capability ready已提交，Build据此确认succeeded


### build.failed.v1

`build` → `ledger`

聚合类型：`build_job`；ID来源：`payload.buildJobId`（须等于aggregateId）

Envelope字段：`eventId`, `eventType`, `schemaVersion`, `owner`, `tenantId`, `aggregateId`, `aggregateVersion`, `occurredAt`, `requestId`, `payload`, `scope`

| payload字段 | 类型 | 必填 | 约束 |
| --- | --- | --- | --- |
| `buildJobId` | `string` | 是 | minLength=1 |
| `errorCode` | `string` | 是 | enum=["VALIDATION_FAILED","UNAUTHENTICATED","FORBIDDEN","NOT_FOUND","CSRF_INVALID","ORIGIN_REJECTED","IDEMPOTENCY_REQUIRED","IDEMPOTENCY_CONFLICT","VERSION_CONFLICT","LAST_OWNER","INVITATION_INVALID","TENANT_INACTIVE","TENANT_RESTORE_EXPIRED","GATEWAY_UNAVAILABLE","OWNER_CAPABILITY_UNAVAILABLE","WALLET_BINDING_REQUIRED","INSUFFICIENT_BALANCE","QUOTE_EXPIRED","QUOTE_MISMATCH","POLICY_UNCONFIGURED","CAPACITY_UNAVAILABLE","PROVIDER_CAPABILITY_UNSUPPORTED","RUNTIME_REVOKED","WEBUI_INCOMPATIBLE","MODEL_NOT_ALLOWED","PACKAGE_ARCHIVED","UPLOAD_EXPIRED","UPLOAD_PART_MISMATCH","UPLOAD_CHECKSUM_MISMATCH","PACKAGE_INVALID","BUILD_INPUT_REJECTED","BUILD_FAILED","ARTIFACT_REFERENCED","ARTIFACT_UNAVAILABLE","INCOMPATIBLE_VERSION","DATA_MIGRATION_REQUIRED","ROLLBACK_UNSAFE","WORKSPACE_NOT_READY","WORKSPACE_EXPIRED","OPERATION_IN_PROGRESS","EXTERNAL_OUTCOME_UNKNOWN","RESOURCE_DELETE_UNCONFIRMED","REFUND_PENDING","KEY_REVEAL_FORBIDDEN","APP_ACCESS_UNAVAILABLE","RATE_LIMITED","DEPENDENCY_UNAVAILABLE","INSTANCE_AUTHORIZATION_REQUIRED","INTERNAL_ERROR","PUBLISHER_NAMESPACE_MISMATCH","PUBLISHER_CONTRACT_INVALID","REFERENCE_CLAIM_INVALID","AUTHORIZATION_CONTEXT_EXPIRED","AUTHORIZATION_REVOKED","AUTHORIZATION_AUDIENCE_MISMATCH","ROUTE_GENERATION_CONFLICT","STALE_EXECUTION_EPOCH","TENANT_NOT_SUSPENDED","RENEWAL_PERIOD_ELAPSED","PLAN_CHANGE_EXISTS","PLAN_CHANGE_NOT_CANCELLABLE","PLAN_CHANGE_QUOTE_STALE","PLAN_TRANSITION_NOT_SUPPORTED","PLAN_TRANSITION_MIXED","PLAN_CHANGE_NO_OP","STORAGE_SHRINK_UNSUPPORTED","SUBSCRIPTION_VERSION_CONFLICT","PLAN_CHANGE_PAYMENT_REQUIRED","PLAN_CHANGE_PERIOD_ELAPSED","REFUND_ORIGINAL_CHARGE_CONFLICT","FUTURE_PERIOD_COMMITTED","SCHEDULED_PLAN_APPLICATION_CONFLICT"]; minLength=1 |

确定性构建失败；unknown不发布此事件


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
