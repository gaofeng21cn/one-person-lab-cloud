# Fabric：API、字段与交互

> 源码快照 `7f5d05fe9b855d8af216caad714cf7fc85014d3c`；这是已定义目标的派生索引，不是业务实现完成声明。回到[总览](../15_domain_alignment.md)。

## 1. 边界与继承

模块：`services/fabric`。当前：**保留上游HTTP服务/provider执行；v2.26 gRPC和opl_fabric新schema尚未替换真实旧loader**。

provider-neutral资源/attachment/Secret/execution/route及实际观察事实、provider adapter；不决定客户价格和退款。

**继承 / 提取 / 新增：** 保留已有Local-Docker/Tencent与操作日志/幂等、容量、Secret边界；不能另起第二Fabric或清空旧资源记录。

**旧事实：** fabric_operations、machine_ownerships及provider事实；旧Control Plane资源表的缓存/账务字段需逐项归属。

**事务边界：** 本库operation/resource action记录与provider调用分离；请求身份先固定，外部结果unknown则原请求读回；fence和route generation由实际provider确认。

**业务顺序：** Workspace/Catalog preflight或执行计划→Fabric adapter→provider→确切resource/absence/route证据→所属调用方继续；不修改Wallet或Workspace权益。

**失败 / unknown：** 超时不是失败；无真实absence不报告删除完成；只允许批准预付包月。

## 2. 客户 REST 与后端 Owner

0 个规格REST操作；浏览器仅经BFF。表中的请求/响应为目标契约，不代表该RPC已挂载。字段展开见DTO目录。

本Owner没有直接客户REST；通过下方内部RPC受其他Owner调用。这不是“没有API”。

## 3. 本域数据库全字段

`opl_fabric`：12 张表，170 列。字段权威：[02](../02_database_schema_complete.md)、[SQL](../contracts/schema.sql)；以下从db_inventory派生。**表不是自动等同DDD聚合根**；事务边界见第1节。


### fabric.resource_sets

provider从批准套餐解析，不复制wallet余额或Cloud订阅价格

| 列 | SQL类型 | 可NULL | 默认值 | API血缘 / 原始来源 |
| --- | --- | --- | --- | --- |
| `id` | `text` | 否 | `—` | 02_database_schema_complete.md#fabric.resource_sets.id |
| `tenant_id` | `text` | 否 | `—` | 02_database_schema_complete.md#fabric.resource_sets.tenant_id |
| `workspace_id` | `text` | 否 | `—` | 02_database_schema_complete.md#fabric.resource_sets.workspace_id |
| `provider` | `text` | 否 | `—` | 02_database_schema_complete.md#fabric.resource_sets.provider |
| `provider_profile_ref` | `text` | 否 | `—` | 02_database_schema_complete.md#fabric.resource_sets.provider_profile_ref |
| `region` | `text` | 否 | `—` | 02_database_schema_complete.md#fabric.resource_sets.region |
| `compute_plan_id` | `text` | 否 | `—` | 02_database_schema_complete.md#fabric.resource_sets.compute_plan_id |
| `storage_plan_id` | `text` | 否 | `—` | 02_database_schema_complete.md#fabric.resource_sets.storage_plan_id |
| `accepted_quote_id` | `text` | 否 | `—` | 02_database_schema_complete.md#fabric.resource_sets.accepted_quote_id |
| `approved_specification` | `jsonb` | 否 | `—` | 02_database_schema_complete.md#fabric.resource_sets.approved_specification |
| `observation_result` | `text` | 否 | `—` | 02_database_schema_complete.md#fabric.resource_sets.observation_result |
| `observed_at` | `timestamptz` | 是 | `—` | 02_database_schema_complete.md#fabric.resource_sets.observed_at |
| `created_at` | `timestamptz` | 否 | `now()` | 02_database_schema_complete.md#fabric.resource_sets.created_at |
| `updated_at` | `timestamptz` | 否 | `now()` | 02_database_schema_complete.md#fabric.resource_sets.updated_at |

约束：
- `PRIMARY KEY (id)`
- `CHECK (observation_result IN ('confirmed','rejected','unknown'))`
- `UNIQUE (workspace_id)`

索引：
- `{"name": "resource_sets_tenant", "columns": "tenant_id, created_at DESC, id DESC", "unique": false, "where": null}`

### fabric.resources

仅预付包月/Local无费；不产生POSTPAID_BY_HOUR；provider事实带观察时间/证据

| 列 | SQL类型 | 可NULL | 默认值 | API血缘 / 原始来源 |
| --- | --- | --- | --- | --- |
| `id` | `text` | 否 | `—` | 02_database_schema_complete.md#fabric.resources.id |
| `resource_set_id` | `text` | 否 | `—` | 02_database_schema_complete.md#fabric.resources.resource_set_id |
| `kind` | `text` | 否 | `—` | 02_database_schema_complete.md#fabric.resources.kind |
| `provider_resource_ref` | `text` | 是 | `—` | 02_database_schema_complete.md#fabric.resources.provider_resource_ref |
| `provider_purchase_key` | `text` | 否 | `—` | 02_database_schema_complete.md#fabric.resources.provider_purchase_key |
| `billing_mode` | `text` | 否 | `—` | 02_database_schema_complete.md#fabric.resources.billing_mode |
| `requested_specification` | `jsonb` | 否 | `—` | 02_database_schema_complete.md#fabric.resources.requested_specification |
| `observed_specification` | `jsonb` | 是 | `—` | 02_database_schema_complete.md#fabric.resources.observed_specification |
| `observation_result` | `text` | 否 | `—` | 02_database_schema_complete.md#fabric.resources.observation_result |
| `provider_expires_at` | `timestamptz` | 是 | `—` | 02_database_schema_complete.md#fabric.resources.provider_expires_at |
| `deletion_evidence_ref` | `text` | 是 | `—` | 02_database_schema_complete.md#fabric.resources.deletion_evidence_ref |
| `deleted_at` | `timestamptz` | 是 | `—` | 02_database_schema_complete.md#fabric.resources.deleted_at |
| `observed_at` | `timestamptz` | 是 | `—` | 02_database_schema_complete.md#fabric.resources.observed_at |
| `created_at` | `timestamptz` | 否 | `now()` | 02_database_schema_complete.md#fabric.resources.created_at |
| `updated_at` | `timestamptz` | 否 | `now()` | 02_database_schema_complete.md#fabric.resources.updated_at |

约束：
- `PRIMARY KEY (id)`
- `FOREIGN KEY (resource_set_id) REFERENCES fabric.resource_sets (id) ON DELETE RESTRICT`
- `CHECK (kind IN ('compute','storage','network','execution'))`
- `CHECK (billing_mode IN ('PREPAID_MONTHLY','LOCAL_NO_CHARGE'))`
- `CHECK (observation_result IN ('confirmed','rejected','unknown'))`
- `UNIQUE (provider_purchase_key)`
- `CHECK (deleted_at IS NULL OR deletion_evidence_ref IS NOT NULL)`

索引：
- `{"name": "resources_provider_ref", "columns": "provider_resource_ref", "unique": false, "where": null}`
- `{"name": "resources_set", "columns": "resource_set_id, kind", "unique": false, "where": null}`

### fabric.attachments

Owner事务核验同resource_set和kind，更新/回滚满足卷单写挂载约束

| 列 | SQL类型 | 可NULL | 默认值 | API血缘 / 原始来源 |
| --- | --- | --- | --- | --- |
| `id` | `text` | 否 | `—` | 02_database_schema_complete.md#fabric.attachments.id |
| `resource_set_id` | `text` | 否 | `—` | 02_database_schema_complete.md#fabric.attachments.resource_set_id |
| `storage_resource_id` | `text` | 否 | `—` | 02_database_schema_complete.md#fabric.attachments.storage_resource_id |
| `execution_resource_id` | `text` | 否 | `—` | 02_database_schema_complete.md#fabric.attachments.execution_resource_id |
| `mount_path` | `text` | 否 | `—` | 02_database_schema_complete.md#fabric.attachments.mount_path |
| `access_mode` | `text` | 否 | `—` | 02_database_schema_complete.md#fabric.attachments.access_mode |
| `observation_result` | `text` | 否 | `—` | 02_database_schema_complete.md#fabric.attachments.observation_result |
| `evidence_ref` | `text` | 是 | `—` | 02_database_schema_complete.md#fabric.attachments.evidence_ref |
| `detached_at` | `timestamptz` | 是 | `—` | 02_database_schema_complete.md#fabric.attachments.detached_at |
| `created_at` | `timestamptz` | 否 | `now()` | 02_database_schema_complete.md#fabric.attachments.created_at |
| `updated_at` | `timestamptz` | 否 | `now()` | 02_database_schema_complete.md#fabric.attachments.updated_at |

约束：
- `PRIMARY KEY (id)`
- `FOREIGN KEY (resource_set_id) REFERENCES fabric.resource_sets (id) ON DELETE RESTRICT`
- `FOREIGN KEY (storage_resource_id) REFERENCES fabric.resources (id) ON DELETE RESTRICT`
- `FOREIGN KEY (execution_resource_id) REFERENCES fabric.resources (id) ON DELETE RESTRICT`
- `CHECK (observation_result IN ('confirmed','rejected','unknown'))`

索引：
- `{"name": "attachments_active", "columns": "storage_resource_id, execution_resource_id, mount_path", "unique": true, "where": "detached_at IS NULL"}`

### fabric.secret_bindings

仅Secret Store引用/版本/指纹；注入完成必须有效认证调用证据

| 列 | SQL类型 | 可NULL | 默认值 | API血缘 / 原始来源 |
| --- | --- | --- | --- | --- |
| `id` | `text` | 否 | `—` | 02_database_schema_complete.md#fabric.secret_bindings.id |
| `resource_set_id` | `text` | 否 | `—` | 02_database_schema_complete.md#fabric.secret_bindings.resource_set_id |
| `execution_resource_id` | `text` | 否 | `—` | 02_database_schema_complete.md#fabric.secret_bindings.execution_resource_id |
| `secret_ref` | `text` | 否 | `—` | 02_database_schema_complete.md#fabric.secret_bindings.secret_ref |
| `purpose` | `text` | 否 | `—` | 02_database_schema_complete.md#fabric.secret_bindings.purpose |
| `version` | `text` | 否 | `—` | 02_database_schema_complete.md#fabric.secret_bindings.version |
| `fingerprint` | `text` | 否 | `—` | 02_database_schema_complete.md#fabric.secret_bindings.fingerprint |
| `observation_result` | `text` | 否 | `—` | 02_database_schema_complete.md#fabric.secret_bindings.observation_result |
| `evidence_ref` | `text` | 是 | `—` | 02_database_schema_complete.md#fabric.secret_bindings.evidence_ref |
| `revoked_at` | `timestamptz` | 是 | `—` | 02_database_schema_complete.md#fabric.secret_bindings.revoked_at |
| `created_at` | `timestamptz` | 否 | `now()` | 02_database_schema_complete.md#fabric.secret_bindings.created_at |
| `updated_at` | `timestamptz` | 否 | `now()` | 02_database_schema_complete.md#fabric.secret_bindings.updated_at |

约束：
- `PRIMARY KEY (id)`
- `FOREIGN KEY (resource_set_id) REFERENCES fabric.resource_sets (id) ON DELETE RESTRICT`
- `FOREIGN KEY (execution_resource_id) REFERENCES fabric.resources (id) ON DELETE RESTRICT`
- `CHECK (observation_result IN ('confirmed','rejected','unknown'))`

索引：
- `{"name": "secret_bindings_active", "columns": "execution_resource_id, purpose", "unique": true, "where": "revoked_at IS NULL"}`

### fabric.resource_actions

实费/采购/续费/删除须Instance保护流程与有界授权；unknown查询原provider请求

| 列 | SQL类型 | 可NULL | 默认值 | API血缘 / 原始来源 |
| --- | --- | --- | --- | --- |
| `id` | `text` | 否 | `—` | 02_database_schema_complete.md#fabric.resource_actions.id |
| `resource_set_id` | `text` | 否 | `—` | 02_database_schema_complete.md#fabric.resource_actions.resource_set_id |
| `resource_id` | `text` | 是 | `—` | 02_database_schema_complete.md#fabric.resource_actions.resource_id |
| `command_id` | `text` | 否 | `—` | 02_database_schema_complete.md#fabric.resource_actions.command_id |
| `action` | `text` | 否 | `—` | 02_database_schema_complete.md#fabric.resource_actions.action |
| `provider_idempotency_key` | `text` | 否 | `—` | 02_database_schema_complete.md#fabric.resource_actions.provider_idempotency_key |
| `approved_input` | `jsonb` | 否 | `—` | 02_database_schema_complete.md#fabric.resource_actions.approved_input |
| `authorization_receipt_ref` | `text` | 是 | `—` | 02_database_schema_complete.md#fabric.resource_actions.authorization_receipt_ref |
| `provider_request_ref` | `text` | 是 | `—` | 02_database_schema_complete.md#fabric.resource_actions.provider_request_ref |
| `observation_result` | `text` | 否 | `—` | 02_database_schema_complete.md#fabric.resource_actions.observation_result |
| `evidence_ref` | `text` | 是 | `—` | 02_database_schema_complete.md#fabric.resource_actions.evidence_ref |
| `error_code` | `text` | 是 | `—` | 02_database_schema_complete.md#fabric.resource_actions.error_code |
| `created_at` | `timestamptz` | 否 | `now()` | 02_database_schema_complete.md#fabric.resource_actions.created_at |
| `updated_at` | `timestamptz` | 否 | `now()` | 02_database_schema_complete.md#fabric.resource_actions.updated_at |
| `execution_epoch` | `bigint` | 是 | `—` | 02_database_schema_complete.md#fabric.resource_actions.execution_epoch |
| `execution_plan_digest` | `text` | 是 | `—` | 02_database_schema_complete.md#fabric.resource_actions.execution_plan_digest |
| `execution_plan_bytes` | `bytea` | 是 | `—` | 02_database_schema_complete.md#fabric.resource_actions.execution_plan_bytes |

约束：
- `PRIMARY KEY (id)`
- `FOREIGN KEY (resource_set_id) REFERENCES fabric.resource_sets (id) ON DELETE RESTRICT`
- `FOREIGN KEY (resource_id) REFERENCES fabric.resources (id) ON DELETE RESTRICT`
- `UNIQUE (command_id)`
- `UNIQUE (provider_idempotency_key)`
- `CHECK (action IN ('allocate','attach','detach','resize','renew','suspend','resume','delete','inject_secret','prepare_resize'))`
- `CHECK (observation_result IN ('confirmed','rejected','unknown'))`
- `CHECK (execution_epoch IS NULL OR execution_epoch >= 0)`
- `CHECK ((execution_plan_digest IS NULL) = (execution_plan_bytes IS NULL))`
- `CHECK (execution_plan_digest IS NULL OR execution_plan_digest = 'sha256:' \|\| encode(sha256(execution_plan_bytes),'hex'))`
- `CHECK (action <> 'prepare_resize' OR (execution_plan_digest IS NOT NULL AND evidence_ref IS NOT NULL AND observation_result = 'confirmed'))`

索引：
- `{"name": "resource_actions_set", "columns": "resource_set_id, created_at DESC, id DESC", "unique": false, "where": null}`

### fabric.outbox_events

本Owner状态事务同写immutable事件；revision只在本Owner聚合锁内分配

| 列 | SQL类型 | 可NULL | 默认值 | API血缘 / 原始来源 |
| --- | --- | --- | --- | --- |
| `id` | `text` | 否 | `—` | 02_database_schema_complete.md#fabric.outbox_events.id |
| `event_type` | `text` | 否 | `—` | 02_database_schema_complete.md#fabric.outbox_events.event_type |
| `schema_version` | `integer` | 否 | `—` | 02_database_schema_complete.md#fabric.outbox_events.schema_version |
| `aggregate_type` | `text` | 否 | `—` | 02_database_schema_complete.md#fabric.outbox_events.aggregate_type |
| `aggregate_id` | `text` | 否 | `—` | 02_database_schema_complete.md#fabric.outbox_events.aggregate_id |
| `aggregate_revision` | `bigint` | 否 | `—` | 02_database_schema_complete.md#fabric.outbox_events.aggregate_revision |
| `tenant_id` | `text` | 是 | `—` | 02_database_schema_complete.md#fabric.outbox_events.tenant_id |
| `correlation_id` | `text` | 否 | `—` | 02_database_schema_complete.md#fabric.outbox_events.correlation_id |
| `causation_id` | `text` | 是 | `—` | 02_database_schema_complete.md#fabric.outbox_events.causation_id |
| `payload` | `jsonb` | 否 | `—` | 02_database_schema_complete.md#fabric.outbox_events.payload |
| `payload_sha256` | `text` | 否 | `—` | 02_database_schema_complete.md#fabric.outbox_events.payload_sha256 |
| `occurred_at` | `timestamptz` | 否 | `—` | 02_database_schema_complete.md#fabric.outbox_events.occurred_at |
| `created_at` | `timestamptz` | 否 | `now()` | 02_database_schema_complete.md#fabric.outbox_events.created_at |

约束：
- `PRIMARY KEY (id)`
- `CHECK (schema_version > 0 AND aggregate_revision >= 0)`
- `CHECK (payload_sha256 ~ '^[0-9a-f]{64}$')`
- `UNIQUE (aggregate_type, aggregate_id, aggregate_revision, event_type)`

索引：
- `{"name": "outbox_events_aggregate", "columns": "aggregate_type, aggregate_id, aggregate_revision", "unique": false, "where": null}`

### fabric.outbox_deliveries

各consumer独立ACK，禁止单个ACK丢其他消费者事件；lease仅本库

| 列 | SQL类型 | 可NULL | 默认值 | API血缘 / 原始来源 |
| --- | --- | --- | --- | --- |
| `id` | `text` | 否 | `—` | 02_database_schema_complete.md#fabric.outbox_deliveries.id |
| `event_id` | `text` | 否 | `—` | 02_database_schema_complete.md#fabric.outbox_deliveries.event_id |
| `consumer_owner` | `text` | 否 | `—` | 02_database_schema_complete.md#fabric.outbox_deliveries.consumer_owner |
| `attempt_count` | `integer` | 否 | `0` | 02_database_schema_complete.md#fabric.outbox_deliveries.attempt_count |
| `next_attempt_at` | `timestamptz` | 否 | `now()` | 02_database_schema_complete.md#fabric.outbox_deliveries.next_attempt_at |
| `acknowledged_at` | `timestamptz` | 是 | `—` | 02_database_schema_complete.md#fabric.outbox_deliveries.acknowledged_at |
| `last_error_code` | `text` | 是 | `—` | 02_database_schema_complete.md#fabric.outbox_deliveries.last_error_code |
| `lease_token` | `text` | 是 | `—` | 02_database_schema_complete.md#fabric.outbox_deliveries.lease_token |
| `lease_until` | `timestamptz` | 是 | `—` | 02_database_schema_complete.md#fabric.outbox_deliveries.lease_until |
| `created_at` | `timestamptz` | 否 | `now()` | 02_database_schema_complete.md#fabric.outbox_deliveries.created_at |
| `updated_at` | `timestamptz` | 否 | `now()` | 02_database_schema_complete.md#fabric.outbox_deliveries.updated_at |

约束：
- `PRIMARY KEY (id)`
- `FOREIGN KEY (event_id) REFERENCES fabric.outbox_events (id) ON DELETE RESTRICT`
- `UNIQUE (event_id, consumer_owner)`
- `CHECK (attempt_count >= 0)`
- `CHECK ((lease_token IS NULL) = (lease_until IS NULL))`

索引：
- `{"name": "outbox_deliveries_pending", "columns": "next_attempt_at, id", "unique": false, "where": "acknowledged_at IS NULL"}`

### fabric.inbox_events

去重hash/业务状态/processed_at/后续Outbox同本库事务；乱序不能覆盖新版本

| 列 | SQL类型 | 可NULL | 默认值 | API血缘 / 原始来源 |
| --- | --- | --- | --- | --- |
| `id` | `text` | 否 | `—` | 02_database_schema_complete.md#fabric.inbox_events.id |
| `source_owner` | `text` | 否 | `—` | 02_database_schema_complete.md#fabric.inbox_events.source_owner |
| `source_event_id` | `text` | 否 | `—` | 02_database_schema_complete.md#fabric.inbox_events.source_event_id |
| `event_type` | `text` | 否 | `—` | 02_database_schema_complete.md#fabric.inbox_events.event_type |
| `schema_version` | `integer` | 否 | `—` | 02_database_schema_complete.md#fabric.inbox_events.schema_version |
| `aggregate_type` | `text` | 否 | `—` | 02_database_schema_complete.md#fabric.inbox_events.aggregate_type |
| `aggregate_id` | `text` | 否 | `—` | 02_database_schema_complete.md#fabric.inbox_events.aggregate_id |
| `aggregate_revision` | `bigint` | 否 | `—` | 02_database_schema_complete.md#fabric.inbox_events.aggregate_revision |
| `payload_sha256` | `text` | 否 | `—` | 02_database_schema_complete.md#fabric.inbox_events.payload_sha256 |
| `payload` | `jsonb` | 否 | `—` | 02_database_schema_complete.md#fabric.inbox_events.payload |
| `received_at` | `timestamptz` | 否 | `now()` | 02_database_schema_complete.md#fabric.inbox_events.received_at |
| `processed_at` | `timestamptz` | 是 | `—` | 02_database_schema_complete.md#fabric.inbox_events.processed_at |
| `result_resource_id` | `text` | 是 | `—` | 02_database_schema_complete.md#fabric.inbox_events.result_resource_id |
| `error_code` | `text` | 是 | `—` | 02_database_schema_complete.md#fabric.inbox_events.error_code |

约束：
- `PRIMARY KEY (id)`
- `UNIQUE (source_owner, source_event_id)`
- `CHECK (schema_version > 0 AND aggregate_revision >= 0)`
- `CHECK (payload_sha256 ~ '^[0-9a-f]{64}$')`

索引：
- `{"name": "inbox_events_pending", "columns": "received_at, id", "unique": false, "where": "processed_at IS NULL"}`
- `{"name": "inbox_events_aggregate", "columns": "source_owner, aggregate_type, aggregate_id, aggregate_revision", "unique": false, "where": null}`

### fabric.idempotency_records

命令创建同事务；operation_name是API操作名不是新Operation ID；安全响应无Key明文；义务存续无TTL清理

| 列 | SQL类型 | 可NULL | 默认值 | API血缘 / 原始来源 |
| --- | --- | --- | --- | --- |
| `id` | `text` | 否 | `—` | 02_database_schema_complete.md#fabric.idempotency_records.id |
| `tenant_scope` | `text` | 否 | `—` | 02_database_schema_complete.md#fabric.idempotency_records.tenant_scope |
| `actor_scope` | `text` | 否 | `—` | 02_database_schema_complete.md#fabric.idempotency_records.actor_scope |
| `operation_name` | `text` | 否 | `—` | 02_database_schema_complete.md#fabric.idempotency_records.operation_name |
| `idempotency_key` | `text` | 否 | `—` | 02_database_schema_complete.md#fabric.idempotency_records.idempotency_key |
| `request_sha256` | `text` | 否 | `—` | 02_database_schema_complete.md#fabric.idempotency_records.request_sha256 |
| `resource_id` | `text` | 否 | `—` | 02_database_schema_complete.md#fabric.idempotency_records.resource_id |
| `operation_id` | `text` | 是 | `—` | 02_database_schema_complete.md#fabric.idempotency_records.operation_id |
| `response_status` | `integer` | 否 | `—` | 02_database_schema_complete.md#fabric.idempotency_records.response_status |
| `response_body` | `jsonb` | 否 | `—` | 02_database_schema_complete.md#fabric.idempotency_records.response_body |
| `created_at` | `timestamptz` | 否 | `now()` | 02_database_schema_complete.md#fabric.idempotency_records.created_at |

约束：
- `PRIMARY KEY (id)`
- `UNIQUE (tenant_scope, actor_scope, operation_name, idempotency_key)`
- `CHECK (request_sha256 ~ '^[0-9a-f]{64}$')`
- `CHECK (response_status BETWEEN 100 AND 599)`

索引：
- `{"name": "idempotency_records_resource", "columns": "resource_id", "unique": false, "where": null}`

### fabric.operations

目标Owner持异步Operation；BFF路由无中央writer；Build创建201回Job；只有Workspace配Saga

| 列 | SQL类型 | 可NULL | 默认值 | API血缘 / 原始来源 |
| --- | --- | --- | --- | --- |
| `id` | `text` | 否 | `—` | 02_database_schema_complete.md#fabric.operations.id |
| `tenant_id` | `text` | 是 | `—` | 02_database_schema_complete.md#fabric.operations.tenant_id |
| `actor_id` | `text` | 否 | `—` | 02_database_schema_complete.md#fabric.operations.actor_id |
| `kind` | `text` | 否 | `—` | 02_database_schema_complete.md#fabric.operations.kind |
| `resource_id` | `text` | 否 | `—` | 02_database_schema_complete.md#fabric.operations.resource_id |
| `status` | `text` | 否 | `'accepted'` | 02_database_schema_complete.md#fabric.operations.status |
| `stage` | `text` | 否 | `—` | 02_database_schema_complete.md#fabric.operations.stage |
| `error_code` | `text` | 是 | `—` | 02_database_schema_complete.md#fabric.operations.error_code |
| `observation_result` | `text` | 是 | `—` | 02_database_schema_complete.md#fabric.operations.observation_result |
| `request_id` | `text` | 否 | `—` | 02_database_schema_complete.md#fabric.operations.request_id |
| `accepted_input` | `jsonb` | 否 | `—` | 02_database_schema_complete.md#fabric.operations.accepted_input |
| `result` | `jsonb` | 是 | `—` | 02_database_schema_complete.md#fabric.operations.result |
| `worker_lease_token` | `text` | 是 | `—` | 02_database_schema_complete.md#fabric.operations.worker_lease_token |
| `worker_lease_until` | `timestamptz` | 是 | `—` | 02_database_schema_complete.md#fabric.operations.worker_lease_until |
| `started_at` | `timestamptz` | 是 | `—` | 02_database_schema_complete.md#fabric.operations.started_at |
| `completed_at` | `timestamptz` | 是 | `—` | 02_database_schema_complete.md#fabric.operations.completed_at |
| `created_at` | `timestamptz` | 否 | `now()` | 02_database_schema_complete.md#fabric.operations.created_at |
| `updated_at` | `timestamptz` | 否 | `now()` | 02_database_schema_complete.md#fabric.operations.updated_at |

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

### fabric.route_bindings

Fabric alone owns observed route generation; Workspace-assigned execution epoch fences stale workers; generation advances only on verified route readback

| 列 | SQL类型 | 可NULL | 默认值 | API血缘 / 原始来源 |
| --- | --- | --- | --- | --- |
| `id` | `text` | 否 | `—` | 02_database_schema_complete.md#fabric.route_bindings.id |
| `workspace_id` | `text` | 否 | `—` | 02_database_schema_complete.md#fabric.route_bindings.workspace_id |
| `route_generation` | `bigint` | 否 | `0` | 02_database_schema_complete.md#fabric.route_bindings.route_generation |
| `accepted_execution_epoch` | `bigint` | 否 | `0` | 02_database_schema_complete.md#fabric.route_bindings.accepted_execution_epoch |
| `target_execution_resource_id` | `text` | 是 | `—` | 02_database_schema_complete.md#fabric.route_bindings.target_execution_resource_id |
| `last_confirmed_switch_id` | `text` | 是 | `—` | 02_database_schema_complete.md#fabric.route_bindings.last_confirmed_switch_id |
| `observed_at` | `timestamptz` | 是 | `—` | 02_database_schema_complete.md#fabric.route_bindings.observed_at |
| `created_at` | `timestamptz` | 否 | `now()` | 02_database_schema_complete.md#fabric.route_bindings.created_at |
| `updated_at` | `timestamptz` | 否 | `now()` | 02_database_schema_complete.md#fabric.route_bindings.updated_at |
| `provider_revision` | `text` | 是 | `—` | 02_database_schema_complete.md#fabric.route_bindings.provider_revision |

约束：
- `PRIMARY KEY (id)`
- `FOREIGN KEY (target_execution_resource_id) REFERENCES fabric.resources (id) ON DELETE RESTRICT`
- `UNIQUE (workspace_id)`
- `UNIQUE (id, workspace_id)`
- `CHECK (route_generation >= 0 AND accepted_execution_epoch >= 0)`
- `FOREIGN KEY (last_confirmed_switch_id) REFERENCES fabric.route_switches (id) ON DELETE RESTRICT`

索引：
- `{"name": "route_bindings_target", "columns": "target_execution_resource_id", "unique": false, "where": null}`

### fabric.route_switches

Provider conditional revision CAS covers target plus epoch metadata; confirmed fence preserves target/generation but advances epoch/revision, then activate/rollback advances generation; any unknown blocks all new route actions

| 列 | SQL类型 | 可NULL | 默认值 | API血缘 / 原始来源 |
| --- | --- | --- | --- | --- |
| `id` | `text` | 否 | `—` | 02_database_schema_complete.md#fabric.route_switches.id |
| `route_binding_id` | `text` | 否 | `—` | 02_database_schema_complete.md#fabric.route_switches.route_binding_id |
| `workspace_id` | `text` | 否 | `—` | 02_database_schema_complete.md#fabric.route_switches.workspace_id |
| `operation_owner` | `text` | 否 | `—` | 02_database_schema_complete.md#fabric.route_switches.operation_owner |
| `operation_id` | `text` | 否 | `—` | 02_database_schema_complete.md#fabric.route_switches.operation_id |
| `expected_route_generation` | `bigint` | 否 | `—` | 02_database_schema_complete.md#fabric.route_switches.expected_route_generation |
| `execution_epoch` | `bigint` | 否 | `—` | 02_database_schema_complete.md#fabric.route_switches.execution_epoch |
| `target_execution_resource_id` | `text` | 是 | `—` | 02_database_schema_complete.md#fabric.route_switches.target_execution_resource_id |
| `previous_target_execution_resource_id` | `text` | 是 | `—` | 02_database_schema_complete.md#fabric.route_switches.previous_target_execution_resource_id |
| `provider_command_id` | `text` | 否 | `—` | 02_database_schema_complete.md#fabric.route_switches.provider_command_id |
| `provider_request_ref` | `text` | 是 | `—` | 02_database_schema_complete.md#fabric.route_switches.provider_request_ref |
| `status` | `text` | 否 | `'requested'` | 02_database_schema_complete.md#fabric.route_switches.status |
| `observed_route_generation` | `bigint` | 是 | `—` | 02_database_schema_complete.md#fabric.route_switches.observed_route_generation |
| `observed_execution_epoch` | `bigint` | 是 | `—` | 02_database_schema_complete.md#fabric.route_switches.observed_execution_epoch |
| `evidence_ref` | `text` | 是 | `—` | 02_database_schema_complete.md#fabric.route_switches.evidence_ref |
| `workspace_selection_commit_receipt_id` | `text` | 是 | `—` | 02_database_schema_complete.md#fabric.route_switches.workspace_selection_commit_receipt_id |
| `error_code` | `text` | 是 | `—` | 02_database_schema_complete.md#fabric.route_switches.error_code |
| `created_at` | `timestamptz` | 否 | `now()` | 02_database_schema_complete.md#fabric.route_switches.created_at |
| `updated_at` | `timestamptz` | 否 | `now()` | 02_database_schema_complete.md#fabric.route_switches.updated_at |
| `action_kind` | `text` | 否 | `—` | 02_database_schema_complete.md#fabric.route_switches.action_kind |
| `expected_provider_revision` | `text` | 是 | `—` | 02_database_schema_complete.md#fabric.route_switches.expected_provider_revision |
| `observed_provider_revision` | `text` | 是 | `—` | 02_database_schema_complete.md#fabric.route_switches.observed_provider_revision |
| `expected_absence_receipt_id` | `text` | 是 | `—` | 02_database_schema_complete.md#fabric.route_switches.expected_absence_receipt_id |
| `expected_absence_observed_at` | `timestamptz` | 是 | `—` | 02_database_schema_complete.md#fabric.route_switches.expected_absence_observed_at |

约束：
- `PRIMARY KEY (id)`
- `FOREIGN KEY (route_binding_id, workspace_id) REFERENCES fabric.route_bindings (id, workspace_id) ON DELETE RESTRICT`
- `FOREIGN KEY (target_execution_resource_id) REFERENCES fabric.resources (id) ON DELETE RESTRICT`
- `FOREIGN KEY (previous_target_execution_resource_id) REFERENCES fabric.resources (id) ON DELETE RESTRICT`
- `CHECK (status IN ('requested','confirmed','rejected','unknown'))`
- `CHECK (expected_route_generation >= 0 AND execution_epoch >= 0)`
- `UNIQUE (provider_command_id)`
- `CHECK (workspace_selection_commit_receipt_id IS NULL OR status = 'confirmed')`
- `CHECK (action_kind IN ('fence','activate','rollback'))`
- `CHECK (action_kind = 'fence' OR target_execution_resource_id IS NOT NULL)`
- `CHECK (expected_provider_revision IS NOT NULL OR expected_route_generation = 0)`
- `CHECK (status <> 'confirmed' OR ((observed_route_generation = expected_route_generation + CASE WHEN action_kind = 'fence' THEN 0 ELSE 1 END AND observed_execution_epoch = execution_epoch AND observed_provider_revision IS NOT NULL AND evidence_ref IS NOT NULL) IS TRUE))`
- `CHECK ((expected_provider_revision IS NOT NULL AND expected_absence_receipt_id IS NULL AND expected_absence_observed_at IS NULL) OR (expected_provider_revision IS NULL AND expected_route_generation = 0 AND expected_absence_receipt_id IS NOT NULL AND expected_absence_observed_at IS NOT NULL))`

索引：
- `{"name": "route_switches_one_pending", "columns": "route_binding_id", "unique": true, "where": "status IN ('requested','unknown')"}`
- `{"name": "route_switches_operation", "columns": "operation_owner, operation_id, created_at DESC, id DESC", "unique": false, "where": null}`

### 本Owner应实现的RPC方法全集（目标，不是挂载证据）

共享OwnerOperations/CommitReadback/Inbox分别由本域实现，不形成中央业务服务；通用Operation REST由BFF按显式Owner路由；本域只负责自己的Operation与授权。

| RPC | 请求 | 响应 |
| --- | --- | --- |
| `OwnerOperations.Read` | [OwnerOperationRequest](rpc-messages.md#owneroperationrequest) | [Operation](rpc-messages.md#operation) |
| `OwnerOperations.Reconcile` | [ReconcileOperationRpcRequest](rpc-messages.md#reconcileoperationrpcrequest) | [Operation](rpc-messages.md#operation) |
| `OwnerCommitReadback.ReadOwnerCommit` | [ReadOwnerCommitRequest](rpc-messages.md#readownercommitrequest) | [OwnerCommitEvidence](rpc-messages.md#ownercommitevidence) |
| `FabricCoordination.AdmitResources` | [ResourceAdmissionRequest](rpc-messages.md#resourceadmissionrequest) | [AdmissionResult](rpc-messages.md#admissionresult) |
| `FabricCoordination.EnsureResources` | [EnsureResourcesCommand](rpc-messages.md#ensureresourcescommand) | [Operation](rpc-messages.md#operation) |
| `FabricCoordination.ResizeResources` | [ResizeResourcesCommand](rpc-messages.md#resizeresourcescommand) | [Operation](rpc-messages.md#operation) |
| `FabricCoordination.RenewResources` | [RenewResourcesCommand](rpc-messages.md#renewresourcescommand) | [Operation](rpc-messages.md#operation) |
| `FabricCoordination.SuspendResources` | [MutateResourcesCommand](rpc-messages.md#mutateresourcescommand) | [Operation](rpc-messages.md#operation) |
| `FabricCoordination.ResumeResources` | [MutateResourcesCommand](rpc-messages.md#mutateresourcescommand) | [Operation](rpc-messages.md#operation) |
| `FabricCoordination.DeleteResources` | [MutateResourcesCommand](rpc-messages.md#mutateresourcescommand) | [Operation](rpc-messages.md#operation) |
| `FabricCoordination.ReadResources` | [ResourceReadbackRequest](rpc-messages.md#resourcereadbackrequest) | [ResourceReadback](rpc-messages.md#resourcereadback) |
| `FabricCoordination.BindSecret` | [SecretBindingCommand](rpc-messages.md#secretbindingcommand) | [SecretBindingReadback](rpc-messages.md#secretbindingreadback) |
| `FabricRuntimeExecution.ReadApplicationCredentials` | [ReadApplicationCredentialsRequest](rpc-messages.md#readapplicationcredentialsrequest) | [WorkspaceApplicationCredentials](rpc-messages.md#workspaceapplicationcredentials) |
| `FabricRuntimeExecution.StartRuntime` | [RuntimeDeployCommand](rpc-messages.md#runtimedeploycommand) | [RuntimeReadback](rpc-messages.md#runtimereadback) |
| `FabricRuntimeExecution.StopRuntime` | [RuntimeStopCommand](rpc-messages.md#runtimestopcommand) | [Operation](rpc-messages.md#operation) |
| `FabricRuntimeExecution.ReloadRuntime` | [RuntimeReloadCommand](rpc-messages.md#runtimereloadcommand) | [Operation](rpc-messages.md#operation) |
| `FabricRuntimeExecution.ObserveRuntime` | [RuntimeReadbackRequest](rpc-messages.md#runtimereadbackrequest) | [RuntimeReadback](rpc-messages.md#runtimereadback) |
| `FabricRouteExecution.FenceRouteEpoch` | [FenceRouteEpochCommand](rpc-messages.md#fencerouteepochcommand) | [RouteReadback](rpc-messages.md#routereadback) |
| `FabricRouteExecution.ActivateRoute` | [RouteActivateCommand](rpc-messages.md#routeactivatecommand) | [RouteReadback](rpc-messages.md#routereadback) |
| `FabricRouteExecution.ObserveRoute` | [RouteObserveRequest](rpc-messages.md#routeobserverequest) | [RouteReadback](rpc-messages.md#routereadback) |
| `FabricRouteExecution.RollbackRoute` | [RouteRollbackCommand](rpc-messages.md#routerollbackcommand) | [RouteReadback](rpc-messages.md#routereadback) |
| `FabricPlanTransitionReadback.ReadApprovedPlanTransition` | [PlanTransitionRequest](rpc-messages.md#plantransitionrequest) | [ApprovedPlanTransition](rpc-messages.md#approvedplantransition) |
| `FabricPlanTransitionReadback.ReadExecutionPlan` | [ReadProviderExecutionPlanRequest](rpc-messages.md#readproviderexecutionplanrequest) | [ProviderPlanChangeExecutionPlan](rpc-messages.md#providerplanchangeexecutionplan) |
| `DomainInbox.Deliver` | [DeliverEventRequest](rpc-messages.md#delivereventrequest) | [InboxAck](rpc-messages.md#inboxack) |

## 4. 跨域调用：调用者 → 拥有方 → 字段 → 结果

以下只列`domain_flows.json`声明的业务边；共享通道/尚无业务边的RPC不能推断成已经实现。


### F07.3 workspace → fabric / FabricCoordination.AdmitResources

按当前入口/阶段与06/13状态机前置执行；不是无条件调用

请求 [ResourceAdmissionRequest](rpc-messages.md#resourceadmissionrequest)：context#1: CallContext；plan#2: ResourcePlanSnapshot；workspace_id#3: string；existing_resource_set_id#4: string；purpose#5: string

返回 [AdmissionResult](rpc-messages.md#admissionresult)：outcome#1: Observation；admission_id#2: string；capability_snapshot_digest#3: string；provider_capability_version#4: string；policy_version_id#5: string；error_code#6: string；expires_at#7: Timestamp；expected_interruption#8: string

接收方写入：

完成证据：provider能力、实际资源/route CAS能力准入

失败/未知：不能静默换provider或降能力

### F08.7 workspace → fabric / FabricCoordination.EnsureResources

按当前入口/阶段与06/13状态机前置执行；不是无条件调用

请求 [EnsureResourcesCommand](rpc-messages.md#ensureresourcescommand)：context#1: CallContext；workspace_id#2: string；obligation_id#3: string；plan#4: ResourcePlanSnapshot；confirmed_charge_receipt_id#5: string；instance_authorization_reference#6: string

返回 [Operation](rpc-messages.md#operation)：operation_id#1: string；owner#2: OperationOwnerEnum；kind#3: OperationKindEnum；resource_id#4: string；status#5: OperationStatusEnum；stage#6: OperationStageEnum；observation_result#7: optional OperationObservationResultEnum；error_code#8: optional ErrorCodeEnum；request_id#9: string；created_at#10: Timestamp；updated_at#11: Timestamp；poll_after_seconds#12: optional int32

接收方写入：`fabric.resources`, `fabric.resource_sets`, `fabric.attachments`

完成证据：批准预付资源与Workspace/原请求exact匹配

失败/未知：unknown查原provider动作，不重购

### F08.8 runtime_control → fabric / FabricCoordination.BindSecret

按当前入口/阶段与06/13状态机前置执行；不是无条件调用

请求 [SecretBindingCommand](rpc-messages.md#secretbindingcommand)：context#1: CallContext；workspace_id#2: string；runtime_instance_id#3: string；key_binding_id#4: string；secret_delivery_reference#5: string；target_slot#6: string

返回 [SecretBindingReadback](rpc-messages.md#secretbindingreadback)：secret_binding_id#1: string；runtime_instance_id#2: string；fingerprint#3: string；outcome#4: Observation；receipt_id#5: string

接收方写入：`fabric.secret_bindings`

完成证据：完整发布描述指定的Secret引用实际注入

失败/未知：不把Gateway Key当任意环境变量公开

### F08.10 workspace → fabric / FabricRouteExecution.FenceRouteEpoch

按当前入口/阶段与06/13状态机前置执行；不是无条件调用

请求 [FenceRouteEpochCommand](rpc-messages.md#fencerouteepochcommand)：context#1: CallContext；workspace_id#2: string；operation_id#3: string；execution_epoch#4: int64；expected_route_generation#5: int64；provider_precondition#6: ProviderRevisionPrecondition

返回 [RouteReadback](rpc-messages.md#routereadback)：workspace_id#1: string；switch_id#2: string；observation#3: Observation；current_generation#4: int64；accepted_execution_epoch#5: int64；target_execution_resource_id#6: optional string；target_runtime_instance_id#7: optional string；target_deployment_id#8: optional string；provider_revision#9: string；provider_command_id#10: string；route_receipt_id#11: optional string；observed_at#12: Timestamp；error_code#13: optional ErrorCodeEnum

接收方写入：`fabric.route_bindings`, `fabric.route_switches`

完成证据：provider conditional revision确认新epoch

失败/未知：未知旧switch先读回，不抢占

### F08.11 runtime_control → fabric / FabricRouteExecution.ActivateRoute

按当前入口/阶段与06/13状态机前置执行；不是无条件调用

请求 [RouteActivateCommand](rpc-messages.md#routeactivatecommand)：context#1: CallContext；workspace_id#2: string；operation_id#3: string；execution_epoch#4: int64；expected_route_generation#5: int64；provider_precondition#6: ProviderRevisionPrecondition；target_execution_resource_id#7: string；target_runtime_instance_id#8: string；target_deployment_id#9: string；confirmed_readiness_receipt_id#10: string

返回 [RouteReadback](rpc-messages.md#routereadback)：workspace_id#1: string；switch_id#2: string；observation#3: Observation；current_generation#4: int64；accepted_execution_epoch#5: int64；target_execution_resource_id#6: optional string；target_runtime_instance_id#7: optional string；target_deployment_id#8: optional string；provider_revision#9: string；provider_command_id#10: string；route_receipt_id#11: optional string；observed_at#12: Timestamp；error_code#13: optional ErrorCodeEnum

接收方写入：`fabric.route_bindings`, `fabric.route_switches`

完成证据：epoch/revision/target精确，provider实际路由确认

失败/未知：旧epoch/旧revision拒绝，丢响应ObserveRoute

### F09.5 runtime_control → fabric / FabricRuntimeExecution.ObserveRuntime

按当前入口/阶段与06/13状态机前置执行；不是无条件调用

请求 [RuntimeReadbackRequest](rpc-messages.md#runtimereadbackrequest)：context#1: CallContext；runtime_instance_id#2: string；deployment_id#3: string

返回 [RuntimeReadback](rpc-messages.md#runtimereadback)：runtime_instance_id#1: string；workspace_id#2: string；deployment_id#3: string；state#4: RuntimeInstanceState；process_ready#5: bool；application_available#6: bool；credential_injection_verified#7: bool；artifact#8: ArtifactReference；applied_model_configuration_version#9: int64；readiness_receipt_id#10: string；outcome#11: Observation；observed_at#12: Timestamp；deployment_descriptor_digest#13: string；execution_epoch#14: int64；deployment_descriptor_object_ref#15: string

接收方写入：

完成证据：appliedVersion和选择相同，运行状态真实

失败/未知：unknown显示应用中/待核实，不覆盖已确认配置

### F10.3 workspace → fabric / FabricRouteExecution.FenceRouteEpoch

按当前入口/阶段与06/13状态机前置执行；不是无条件调用

请求 [FenceRouteEpochCommand](rpc-messages.md#fencerouteepochcommand)：context#1: CallContext；workspace_id#2: string；operation_id#3: string；execution_epoch#4: int64；expected_route_generation#5: int64；provider_precondition#6: ProviderRevisionPrecondition

返回 [RouteReadback](rpc-messages.md#routereadback)：workspace_id#1: string；switch_id#2: string；observation#3: Observation；current_generation#4: int64；accepted_execution_epoch#5: int64；target_execution_resource_id#6: optional string；target_runtime_instance_id#7: optional string；target_deployment_id#8: optional string；provider_revision#9: string；provider_command_id#10: string；route_receipt_id#11: optional string；observed_at#12: Timestamp；error_code#13: optional ErrorCodeEnum

接收方写入：`fabric.route_switches`, `fabric.route_bindings`

完成证据：新epoch先在provider确认

失败/未知：旧未知切换不被强行覆盖

### F10.5 runtime_control → fabric / FabricRouteExecution.ActivateRoute

按当前入口/阶段与06/13状态机前置执行；不是无条件调用

请求 [RouteActivateCommand](rpc-messages.md#routeactivatecommand)：context#1: CallContext；workspace_id#2: string；operation_id#3: string；execution_epoch#4: int64；expected_route_generation#5: int64；provider_precondition#6: ProviderRevisionPrecondition；target_execution_resource_id#7: string；target_runtime_instance_id#8: string；target_deployment_id#9: string；confirmed_readiness_receipt_id#10: string

返回 [RouteReadback](rpc-messages.md#routereadback)：workspace_id#1: string；switch_id#2: string；observation#3: Observation；current_generation#4: int64；accepted_execution_epoch#5: int64；target_execution_resource_id#6: optional string；target_runtime_instance_id#7: optional string；target_deployment_id#8: optional string；provider_revision#9: string；provider_command_id#10: string；route_receipt_id#11: optional string；observed_at#12: Timestamp；error_code#13: optional ErrorCodeEnum

接收方写入：`fabric.route_switches`, `fabric.route_bindings`

完成证据：新target/provider revision确认

失败/未知：丢响应原switch读回

### F10.6 runtime_control → fabric / FabricRouteExecution.RollbackRoute

新部署明确失败且旧数据/路由允许安全回滚

请求 [RouteRollbackCommand](rpc-messages.md#routerollbackcommand)：context#1: CallContext；workspace_id#2: string；original_switch_id#3: string；operation_id#4: string；execution_epoch#5: int64；expected_route_generation#6: int64；provider_precondition#7: ProviderRevisionPrecondition；target_execution_resource_id#8: string；target_runtime_instance_id#9: string；target_deployment_id#10: string；compatibility_receipt_id#11: string

返回 [RouteReadback](rpc-messages.md#routereadback)：workspace_id#1: string；switch_id#2: string；observation#3: Observation；current_generation#4: int64；accepted_execution_epoch#5: int64；target_execution_resource_id#6: optional string；target_runtime_instance_id#7: optional string；target_deployment_id#8: optional string；provider_revision#9: string；provider_command_id#10: string；route_receipt_id#11: optional string；observed_at#12: Timestamp；error_code#13: optional ErrorCodeEnum

接收方写入：`fabric.route_switches`, `fabric.route_bindings`

完成证据：原目标+原switch证据+当前expected generation可核对

失败/未知：不能仅改DB指针称回滚成功

### F11.3 resource_catalog → fabric / FabricPlanTransitionReadback.ReadApprovedPlanTransition

按当前入口/阶段与06/13状态机前置执行；不是无条件调用

请求 [PlanTransitionRequest](rpc-messages.md#plantransitionrequest)：context#1: CallContext；workspace_id#2: string；source_compute_plan_id#3: string；source_storage_plan_id#4: string；target_compute_plan_id#5: string；target_storage_plan_id#6: string；resource_set_id#7: string；expected_resource_version#8: string

返回 [ApprovedPlanTransition](rpc-messages.md#approvedplantransition)：id#1: string；workspace_id#2: string；kind#3: PlanChangeKindEnum；provider_profile_id#4: string；capability_class#5: string；source#6: ResourcePlanSnapshot；target#7: ResourcePlanSnapshot；provider_capability_version#8: string；storage_shrink_supported#9: bool；expected_interruption#10: string；reversibility#11: TransitionReversibility；admission_receipt_id#12: string；observed_at#13: Timestamp；expires_at#14: Timestamp；outcome#15: Observation；execution_plan#16: ProviderPlanChangeExecutionPlanReference

接收方写入：

完成证据：批准可比转换，固定执行策略/数据/中断能力

失败/未知：mixed/no-op/不支持缩容拒绝，不按SKU名字或价格猜方向

### F11.10 workspace → fabric / FabricCoordination.ResizeResources

upgrade资金confirmed/zero；或downgrade已到E且目标期资金confirmed

请求 [ResizeResourcesCommand](rpc-messages.md#resizeresourcescommand)：context#1: CallContext；workspace_id#2: string；resource_set_id#3: string；expected_resource_version#4: string；target#5: ResourcePlanSnapshot；quote_acceptance_id#6: string；funding_evidence#7: PlanChangeFundingEvidence；instance_authorization_reference#8: string；plan_change_id#9: string；transition_id#10: string；execution_epoch#11: int64；execution_plan#12: ProviderPlanChangeExecutionPlanReference

返回 [Operation](rpc-messages.md#operation)：operation_id#1: string；owner#2: OperationOwnerEnum；kind#3: OperationKindEnum；resource_id#4: string；status#5: OperationStatusEnum；stage#6: OperationStageEnum；observation_result#7: optional OperationObservationResultEnum；error_code#8: optional ErrorCodeEnum；request_id#9: string；created_at#10: Timestamp；updated_at#11: Timestamp；poll_after_seconds#12: optional int32

接收方写入：`fabric.resource_actions`, `fabric.resources`

完成证据：资金/ZeroFundingEvidence和固定executionPlan，原epoch/目标/资源读回一致

失败/未知：unknown原请求读回，部分不可逆事实独立保留不假缩容

### F12.6 workspace → fabric / FabricCoordination.RenewResources

无计划常规续期；有计划必须遵照已批准目标executionPlan，不独立先续旧高配

请求 [RenewResourcesCommand](rpc-messages.md#renewresourcescommand)：context#1: CallContext；workspace_id#2: string；resource_set_id#3: string；subscription_period_id#4: string；prepaid_months#5: int32；confirmed_charge_receipt_id#6: string；instance_authorization_reference#7: string

返回 [Operation](rpc-messages.md#operation)：operation_id#1: string；owner#2: OperationOwnerEnum；kind#3: OperationKindEnum；resource_id#4: string；status#5: OperationStatusEnum；stage#6: OperationStageEnum；observation_result#7: optional OperationObservationResultEnum；error_code#8: optional ErrorCodeEnum；request_id#9: string；created_at#10: Timestamp；updated_at#11: Timestamp；poll_after_seconds#12: optional int32

接收方写入：`fabric.resource_actions`

完成证据：原资源续期读回与原paidThrough续期窗口一致

失败/未知：过期已回收不伪造恢复、不改now重新计期

### F13.3 workspace → fabric / FabricCoordination.DeleteResources

按当前入口/阶段与06/13状态机前置执行；不是无条件调用

请求 [MutateResourcesCommand](rpc-messages.md#mutateresourcescommand)：context#1: CallContext；workspace_id#2: string；resource_set_id#3: string；expected_resource_version#4: string；instance_authorization_reference#5: string

返回 [Operation](rpc-messages.md#operation)：operation_id#1: string；owner#2: OperationOwnerEnum；kind#3: OperationKindEnum；resource_id#4: string；status#5: OperationStatusEnum；stage#6: OperationStageEnum；observation_result#7: optional OperationObservationResultEnum；error_code#8: optional ErrorCodeEnum；request_id#9: string；created_at#10: Timestamp；updated_at#11: Timestamp；poll_after_seconds#12: optional int32

接收方写入：`fabric.resource_actions`, `fabric.resources`, `fabric.attachments`

完成证据：原资源/挂载/Secret及公开访问绑定确切absence

失败/未知：不依赖列表没看到推断不存在

## 5. 事件：谁生产、谁消费、哪些字段

aggregate_type由事件精确版本的x-aggregate-identity.type派生；aggregateId须与其idPayloadField一致。revision由生产者聚合事务内分配；consumer_owner显式选择本域Inbox。字段与实现状态不得混同。


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

