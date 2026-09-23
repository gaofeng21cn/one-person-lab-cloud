# Runtime Control：API、字段与交互

> 源码快照 `7f5d05fe9b855d8af216caad714cf7fc85014d3c`；这是已定义目标的派生索引，不是业务实现完成声明。回到[总览](../15_domain_alignment.md)。

## 1. 边界与继承

模块：`services/runtime-control`。当前：**新持久化骨架；RuntimeCoordination/RuntimePlanChangeControl尚未注册**。

运行实例期望、descriptor执行、配置应用、健康/更新/回滚；不是Framework Runtime实现，也不是Workspace付费和active选择Owner。

**继承 / 提取 / 新增：** 提取旧应用部署驱动的执行协调；实际provider执行继续Fabric，客户业务选择留Workspace。

**旧事实：** Control Plane应用部署执行协调 + Fabric应用运行读回；旧runtime_id有实际应用事实才导入。

**事务边界：** runtime_instances/runtime_actions及本域Operation/Outbox同库；保留操作epoch和descriptor引用；不改Workspace.activeDeploymentId。

**业务顺序：** Workspace要求部署/配置→核验Capability描述→Fabric执行/挂载/Secret→真实应用健康与已应用配置→回报Workspace→Workspace决定选中。

**失败 / unknown：** 配置/路由/启动未知必须真实readback；数据不兼容拒绝回滚；不重购资源或扩权。

## 2. 客户 REST 与后端 Owner

0 个规格REST操作；浏览器仅经BFF。表中的请求/响应为目标契约，不代表该RPC已挂载。字段展开见DTO目录。

本Owner没有直接客户REST；通过下方内部RPC受其他Owner调用。这不是“没有API”。

## 3. 本域数据库全字段

`opl_runtime_control`：7 张表，98 列。字段权威：[02](../02_database_schema_complete.md)、[SQL](../contracts/schema.sql)；以下从db_inventory派生。**表不是自动等同DDD聚合根**；事务边界见第1节。


### runtime_control.runtime_instances

readiness/accessUrl真实回读；无active布尔、无订阅业务状态

| 列 | SQL类型 | 可NULL | 默认值 | API血缘 / 原始来源 |
| --- | --- | --- | --- | --- |
| `id` | `text` | 否 | `—` | 02_database_schema_complete.md#runtime_control.runtime_instances.id |
| `workspace_id` | `text` | 否 | `—` | 02_database_schema_complete.md#runtime_control.runtime_instances.workspace_id |
| `deployment_id` | `text` | 否 | `—` | 02_database_schema_complete.md#runtime_control.runtime_instances.deployment_id |
| `artifact_digest` | `text` | 否 | `—` | 02_database_schema_complete.md#runtime_control.runtime_instances.artifact_digest |
| `fabric_resource_set_id` | `text` | 否 | `—` | 02_database_schema_complete.md#runtime_control.runtime_instances.fabric_resource_set_id |
| `fabric_execution_ref` | `text` | 是 | `—` | 02_database_schema_complete.md#runtime_control.runtime_instances.fabric_execution_ref |
| `status` | `text` | 否 | `'pending'` | 02_database_schema_complete.md#runtime_control.runtime_instances.status |
| `access_url` | `text` | 是 | `—` | 02_database_schema_complete.md#runtime_control.runtime_instances.access_url |
| `data_attachment_contract` | `jsonb` | 否 | `—` | 02_database_schema_complete.md#runtime_control.runtime_instances.data_attachment_contract |
| `applied_model_configuration_version` | `bigint` | 否 | `0` | 02_database_schema_complete.md#runtime_control.runtime_instances.applied_model_configuration_version |
| `readiness_evidence_ref` | `text` | 是 | `—` | 02_database_schema_complete.md#runtime_control.runtime_instances.readiness_evidence_ref |
| `error_code` | `text` | 是 | `—` | 02_database_schema_complete.md#runtime_control.runtime_instances.error_code |
| `observed_at` | `timestamptz` | 是 | `—` | 02_database_schema_complete.md#runtime_control.runtime_instances.observed_at |
| `created_at` | `timestamptz` | 否 | `now()` | 02_database_schema_complete.md#runtime_control.runtime_instances.created_at |
| `updated_at` | `timestamptz` | 否 | `now()` | 02_database_schema_complete.md#runtime_control.runtime_instances.updated_at |
| `execution_epoch` | `bigint` | 否 | `—` | 02_database_schema_complete.md#runtime_control.runtime_instances.execution_epoch |
| `deployment_descriptor` | `jsonb` | 否 | `—` | 02_database_schema_complete.md#runtime_control.runtime_instances.deployment_descriptor |
| `deployment_descriptor_digest` | `text` | 否 | `—` | 02_database_schema_complete.md#runtime_control.runtime_instances.deployment_descriptor_digest |
| `deployment_descriptor_object_ref` | `text` | 否 | `—` | 02_database_schema_complete.md#runtime_control.runtime_instances.deployment_descriptor_object_ref |

约束：
- `PRIMARY KEY (id)`
- `CHECK (status IN ('pending','starting','ready','stopped','failed','terminating','terminated'))`
- `UNIQUE (deployment_id)`
- `CHECK (artifact_digest ~ '^sha256:[0-9a-f]{64}$')`
- `CHECK (applied_model_configuration_version >= 0)`
- `CHECK (status <> 'ready' OR (access_url IS NOT NULL AND readiness_evidence_ref IS NOT NULL AND observed_at IS NOT NULL))`
- `CHECK (execution_epoch >= 0)`
- `CHECK (deployment_descriptor_digest ~ '^sha256:[0-9a-f]{64}$')`
- `CHECK ((deployment_descriptor #>> '{artifact,digest}' = artifact_digest) IS TRUE)`

索引：
- `{"name": "runtime_instances_workspace", "columns": "workspace_id, created_at DESC, id DESC", "unique": false, "where": null}`

### runtime_control.runtime_actions

先持久action再调用Fabric；旧Deployment响应不能覆盖新实例

| 列 | SQL类型 | 可NULL | 默认值 | API血缘 / 原始来源 |
| --- | --- | --- | --- | --- |
| `id` | `text` | 否 | `—` | 02_database_schema_complete.md#runtime_control.runtime_actions.id |
| `runtime_instance_id` | `text` | 否 | `—` | 02_database_schema_complete.md#runtime_control.runtime_actions.runtime_instance_id |
| `command_id` | `text` | 否 | `—` | 02_database_schema_complete.md#runtime_control.runtime_actions.command_id |
| `action` | `text` | 否 | `—` | 02_database_schema_complete.md#runtime_control.runtime_actions.action |
| `expected_deployment_id` | `text` | 否 | `—` | 02_database_schema_complete.md#runtime_control.runtime_actions.expected_deployment_id |
| `input_snapshot` | `jsonb` | 否 | `—` | 02_database_schema_complete.md#runtime_control.runtime_actions.input_snapshot |
| `fabric_action_id` | `text` | 是 | `—` | 02_database_schema_complete.md#runtime_control.runtime_actions.fabric_action_id |
| `observation_result` | `text` | 否 | `—` | 02_database_schema_complete.md#runtime_control.runtime_actions.observation_result |
| `error_code` | `text` | 是 | `—` | 02_database_schema_complete.md#runtime_control.runtime_actions.error_code |
| `evidence_ref` | `text` | 是 | `—` | 02_database_schema_complete.md#runtime_control.runtime_actions.evidence_ref |
| `created_at` | `timestamptz` | 否 | `now()` | 02_database_schema_complete.md#runtime_control.runtime_actions.created_at |
| `updated_at` | `timestamptz` | 否 | `now()` | 02_database_schema_complete.md#runtime_control.runtime_actions.updated_at |

约束：
- `PRIMARY KEY (id)`
- `FOREIGN KEY (runtime_instance_id) REFERENCES runtime_control.runtime_instances (id) ON DELETE RESTRICT`
- `UNIQUE (command_id)`
- `CHECK (action IN ('start','stop','terminate','reload','verify'))`
- `CHECK (observation_result IN ('confirmed','rejected','unknown'))`

索引：
- `{"name": "runtime_actions_instance", "columns": "runtime_instance_id, created_at DESC, id DESC", "unique": false, "where": null}`

### runtime_control.outbox_events

本Owner状态事务同写immutable事件；revision只在本Owner聚合锁内分配

| 列 | SQL类型 | 可NULL | 默认值 | API血缘 / 原始来源 |
| --- | --- | --- | --- | --- |
| `id` | `text` | 否 | `—` | 02_database_schema_complete.md#runtime_control.outbox_events.id |
| `event_type` | `text` | 否 | `—` | 02_database_schema_complete.md#runtime_control.outbox_events.event_type |
| `schema_version` | `integer` | 否 | `—` | 02_database_schema_complete.md#runtime_control.outbox_events.schema_version |
| `aggregate_type` | `text` | 否 | `—` | 02_database_schema_complete.md#runtime_control.outbox_events.aggregate_type |
| `aggregate_id` | `text` | 否 | `—` | 02_database_schema_complete.md#runtime_control.outbox_events.aggregate_id |
| `aggregate_revision` | `bigint` | 否 | `—` | 02_database_schema_complete.md#runtime_control.outbox_events.aggregate_revision |
| `tenant_id` | `text` | 是 | `—` | 02_database_schema_complete.md#runtime_control.outbox_events.tenant_id |
| `correlation_id` | `text` | 否 | `—` | 02_database_schema_complete.md#runtime_control.outbox_events.correlation_id |
| `causation_id` | `text` | 是 | `—` | 02_database_schema_complete.md#runtime_control.outbox_events.causation_id |
| `payload` | `jsonb` | 否 | `—` | 02_database_schema_complete.md#runtime_control.outbox_events.payload |
| `payload_sha256` | `text` | 否 | `—` | 02_database_schema_complete.md#runtime_control.outbox_events.payload_sha256 |
| `occurred_at` | `timestamptz` | 否 | `—` | 02_database_schema_complete.md#runtime_control.outbox_events.occurred_at |
| `created_at` | `timestamptz` | 否 | `now()` | 02_database_schema_complete.md#runtime_control.outbox_events.created_at |

约束：
- `PRIMARY KEY (id)`
- `CHECK (schema_version > 0 AND aggregate_revision >= 0)`
- `CHECK (payload_sha256 ~ '^[0-9a-f]{64}$')`
- `UNIQUE (aggregate_type, aggregate_id, aggregate_revision, event_type)`

索引：
- `{"name": "outbox_events_aggregate", "columns": "aggregate_type, aggregate_id, aggregate_revision", "unique": false, "where": null}`

### runtime_control.outbox_deliveries

各consumer独立ACK，禁止单个ACK丢其他消费者事件；lease仅本库

| 列 | SQL类型 | 可NULL | 默认值 | API血缘 / 原始来源 |
| --- | --- | --- | --- | --- |
| `id` | `text` | 否 | `—` | 02_database_schema_complete.md#runtime_control.outbox_deliveries.id |
| `event_id` | `text` | 否 | `—` | 02_database_schema_complete.md#runtime_control.outbox_deliveries.event_id |
| `consumer_owner` | `text` | 否 | `—` | 02_database_schema_complete.md#runtime_control.outbox_deliveries.consumer_owner |
| `attempt_count` | `integer` | 否 | `0` | 02_database_schema_complete.md#runtime_control.outbox_deliveries.attempt_count |
| `next_attempt_at` | `timestamptz` | 否 | `now()` | 02_database_schema_complete.md#runtime_control.outbox_deliveries.next_attempt_at |
| `acknowledged_at` | `timestamptz` | 是 | `—` | 02_database_schema_complete.md#runtime_control.outbox_deliveries.acknowledged_at |
| `last_error_code` | `text` | 是 | `—` | 02_database_schema_complete.md#runtime_control.outbox_deliveries.last_error_code |
| `lease_token` | `text` | 是 | `—` | 02_database_schema_complete.md#runtime_control.outbox_deliveries.lease_token |
| `lease_until` | `timestamptz` | 是 | `—` | 02_database_schema_complete.md#runtime_control.outbox_deliveries.lease_until |
| `created_at` | `timestamptz` | 否 | `now()` | 02_database_schema_complete.md#runtime_control.outbox_deliveries.created_at |
| `updated_at` | `timestamptz` | 否 | `now()` | 02_database_schema_complete.md#runtime_control.outbox_deliveries.updated_at |

约束：
- `PRIMARY KEY (id)`
- `FOREIGN KEY (event_id) REFERENCES runtime_control.outbox_events (id) ON DELETE RESTRICT`
- `UNIQUE (event_id, consumer_owner)`
- `CHECK (attempt_count >= 0)`
- `CHECK ((lease_token IS NULL) = (lease_until IS NULL))`

索引：
- `{"name": "outbox_deliveries_pending", "columns": "next_attempt_at, id", "unique": false, "where": "acknowledged_at IS NULL"}`

### runtime_control.inbox_events

去重hash/业务状态/processed_at/后续Outbox同本库事务；乱序不能覆盖新版本

| 列 | SQL类型 | 可NULL | 默认值 | API血缘 / 原始来源 |
| --- | --- | --- | --- | --- |
| `id` | `text` | 否 | `—` | 02_database_schema_complete.md#runtime_control.inbox_events.id |
| `source_owner` | `text` | 否 | `—` | 02_database_schema_complete.md#runtime_control.inbox_events.source_owner |
| `source_event_id` | `text` | 否 | `—` | 02_database_schema_complete.md#runtime_control.inbox_events.source_event_id |
| `event_type` | `text` | 否 | `—` | 02_database_schema_complete.md#runtime_control.inbox_events.event_type |
| `schema_version` | `integer` | 否 | `—` | 02_database_schema_complete.md#runtime_control.inbox_events.schema_version |
| `aggregate_type` | `text` | 否 | `—` | 02_database_schema_complete.md#runtime_control.inbox_events.aggregate_type |
| `aggregate_id` | `text` | 否 | `—` | 02_database_schema_complete.md#runtime_control.inbox_events.aggregate_id |
| `aggregate_revision` | `bigint` | 否 | `—` | 02_database_schema_complete.md#runtime_control.inbox_events.aggregate_revision |
| `payload_sha256` | `text` | 否 | `—` | 02_database_schema_complete.md#runtime_control.inbox_events.payload_sha256 |
| `payload` | `jsonb` | 否 | `—` | 02_database_schema_complete.md#runtime_control.inbox_events.payload |
| `received_at` | `timestamptz` | 否 | `now()` | 02_database_schema_complete.md#runtime_control.inbox_events.received_at |
| `processed_at` | `timestamptz` | 是 | `—` | 02_database_schema_complete.md#runtime_control.inbox_events.processed_at |
| `result_resource_id` | `text` | 是 | `—` | 02_database_schema_complete.md#runtime_control.inbox_events.result_resource_id |
| `error_code` | `text` | 是 | `—` | 02_database_schema_complete.md#runtime_control.inbox_events.error_code |

约束：
- `PRIMARY KEY (id)`
- `UNIQUE (source_owner, source_event_id)`
- `CHECK (schema_version > 0 AND aggregate_revision >= 0)`
- `CHECK (payload_sha256 ~ '^[0-9a-f]{64}$')`

索引：
- `{"name": "inbox_events_pending", "columns": "received_at, id", "unique": false, "where": "processed_at IS NULL"}`
- `{"name": "inbox_events_aggregate", "columns": "source_owner, aggregate_type, aggregate_id, aggregate_revision", "unique": false, "where": null}`

### runtime_control.idempotency_records

命令创建同事务；operation_name是API操作名不是新Operation ID；安全响应无Key明文；义务存续无TTL清理

| 列 | SQL类型 | 可NULL | 默认值 | API血缘 / 原始来源 |
| --- | --- | --- | --- | --- |
| `id` | `text` | 否 | `—` | 02_database_schema_complete.md#runtime_control.idempotency_records.id |
| `tenant_scope` | `text` | 否 | `—` | 02_database_schema_complete.md#runtime_control.idempotency_records.tenant_scope |
| `actor_scope` | `text` | 否 | `—` | 02_database_schema_complete.md#runtime_control.idempotency_records.actor_scope |
| `operation_name` | `text` | 否 | `—` | 02_database_schema_complete.md#runtime_control.idempotency_records.operation_name |
| `idempotency_key` | `text` | 否 | `—` | 02_database_schema_complete.md#runtime_control.idempotency_records.idempotency_key |
| `request_sha256` | `text` | 否 | `—` | 02_database_schema_complete.md#runtime_control.idempotency_records.request_sha256 |
| `resource_id` | `text` | 否 | `—` | 02_database_schema_complete.md#runtime_control.idempotency_records.resource_id |
| `operation_id` | `text` | 是 | `—` | 02_database_schema_complete.md#runtime_control.idempotency_records.operation_id |
| `response_status` | `integer` | 否 | `—` | 02_database_schema_complete.md#runtime_control.idempotency_records.response_status |
| `response_body` | `jsonb` | 否 | `—` | 02_database_schema_complete.md#runtime_control.idempotency_records.response_body |
| `created_at` | `timestamptz` | 否 | `now()` | 02_database_schema_complete.md#runtime_control.idempotency_records.created_at |

约束：
- `PRIMARY KEY (id)`
- `UNIQUE (tenant_scope, actor_scope, operation_name, idempotency_key)`
- `CHECK (request_sha256 ~ '^[0-9a-f]{64}$')`
- `CHECK (response_status BETWEEN 100 AND 599)`

索引：
- `{"name": "idempotency_records_resource", "columns": "resource_id", "unique": false, "where": null}`

### runtime_control.operations

目标Owner持异步Operation；BFF路由无中央writer；Build创建201回Job；只有Workspace配Saga

| 列 | SQL类型 | 可NULL | 默认值 | API血缘 / 原始来源 |
| --- | --- | --- | --- | --- |
| `id` | `text` | 否 | `—` | 02_database_schema_complete.md#runtime_control.operations.id |
| `tenant_id` | `text` | 是 | `—` | 02_database_schema_complete.md#runtime_control.operations.tenant_id |
| `actor_id` | `text` | 否 | `—` | 02_database_schema_complete.md#runtime_control.operations.actor_id |
| `kind` | `text` | 否 | `—` | 02_database_schema_complete.md#runtime_control.operations.kind |
| `resource_id` | `text` | 否 | `—` | 02_database_schema_complete.md#runtime_control.operations.resource_id |
| `status` | `text` | 否 | `'accepted'` | 02_database_schema_complete.md#runtime_control.operations.status |
| `stage` | `text` | 否 | `—` | 02_database_schema_complete.md#runtime_control.operations.stage |
| `error_code` | `text` | 是 | `—` | 02_database_schema_complete.md#runtime_control.operations.error_code |
| `observation_result` | `text` | 是 | `—` | 02_database_schema_complete.md#runtime_control.operations.observation_result |
| `request_id` | `text` | 否 | `—` | 02_database_schema_complete.md#runtime_control.operations.request_id |
| `accepted_input` | `jsonb` | 否 | `—` | 02_database_schema_complete.md#runtime_control.operations.accepted_input |
| `result` | `jsonb` | 是 | `—` | 02_database_schema_complete.md#runtime_control.operations.result |
| `worker_lease_token` | `text` | 是 | `—` | 02_database_schema_complete.md#runtime_control.operations.worker_lease_token |
| `worker_lease_until` | `timestamptz` | 是 | `—` | 02_database_schema_complete.md#runtime_control.operations.worker_lease_until |
| `started_at` | `timestamptz` | 是 | `—` | 02_database_schema_complete.md#runtime_control.operations.started_at |
| `completed_at` | `timestamptz` | 是 | `—` | 02_database_schema_complete.md#runtime_control.operations.completed_at |
| `created_at` | `timestamptz` | 否 | `now()` | 02_database_schema_complete.md#runtime_control.operations.created_at |
| `updated_at` | `timestamptz` | 否 | `now()` | 02_database_schema_complete.md#runtime_control.operations.updated_at |

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
| `OwnerOperations.Read` | [OwnerOperationRequest](rpc-messages.md#owneroperationrequest) | [Operation](rpc-messages.md#operation) |
| `OwnerOperations.Reconcile` | [ReconcileOperationRpcRequest](rpc-messages.md#reconcileoperationrpcrequest) | [Operation](rpc-messages.md#operation) |
| `OwnerCommitReadback.ReadOwnerCommit` | [ReadOwnerCommitRequest](rpc-messages.md#readownercommitrequest) | [OwnerCommitEvidence](rpc-messages.md#ownercommitevidence) |
| `RuntimeCoordination.Reserve` | [RuntimeReservationCommand](rpc-messages.md#runtimereservationcommand) | [RuntimeReservation](rpc-messages.md#runtimereservation) |
| `RuntimeCoordination.Deploy` | [RuntimeDeployCommand](rpc-messages.md#runtimedeploycommand) | [RuntimeReadback](rpc-messages.md#runtimereadback) |
| `RuntimeCoordination.ReloadModels` | [RuntimeReloadCommand](rpc-messages.md#runtimereloadcommand) | [Operation](rpc-messages.md#operation) |
| `RuntimeCoordination.ReadRuntime` | [RuntimeReadbackRequest](rpc-messages.md#runtimereadbackrequest) | [RuntimeReadback](rpc-messages.md#runtimereadback) |
| `RuntimeCoordination.Retire` | [RuntimeStopCommand](rpc-messages.md#runtimestopcommand) | [Operation](rpc-messages.md#operation) |
| `RuntimePlanChangeControl.RestoreAfterResourceChange` | [RestorePlanChangeRuntimeCommand](rpc-messages.md#restoreplanchangeruntimecommand) | [PlanChangeRuntimeReadback](rpc-messages.md#planchangeruntimereadback) |
| `DomainInbox.Deliver` | [DeliverEventRequest](rpc-messages.md#delivereventrequest) | [InboxAck](rpc-messages.md#inboxack) |

## 4. 跨域调用：调用者 → 拥有方 → 字段 → 结果

以下只列`domain_flows.json`声明的业务边；共享通道/尚无业务边的RPC不能推断成已经实现。


### F08.3 workspace → runtime_control / RuntimeCoordination.Reserve

按当前入口/阶段与06/13状态机前置执行；不是无条件调用

请求 [RuntimeReservationCommand](rpc-messages.md#runtimereservationcommand)：context#1: CallContext；workspace_id#2: string；deployment_id#3: string；capability_version_id#4: string；artifact#5: ArtifactReference；deployment_descriptor_digest#6: string；deployment_descriptor_object_ref#7: string

返回 [RuntimeReservation](rpc-messages.md#runtimereservation)：runtime_instance_id#1: string；workspace_id#2: string；deployment_id#3: string；artifact#4: ArtifactReference；deployment_descriptor_digest#5: string；deployment_descriptor_object_ref#6: string

接收方写入：`runtime_control.runtime_instances`

完成证据：预留稳定runtimeInstanceId而不启动

失败/未知：不因Key依赖Runtime ID产生循环

### F08.8 runtime_control → fabric / FabricCoordination.BindSecret

按当前入口/阶段与06/13状态机前置执行；不是无条件调用

请求 [SecretBindingCommand](rpc-messages.md#secretbindingcommand)：context#1: CallContext；workspace_id#2: string；runtime_instance_id#3: string；key_binding_id#4: string；secret_delivery_reference#5: string；target_slot#6: string

返回 [SecretBindingReadback](rpc-messages.md#secretbindingreadback)：secret_binding_id#1: string；runtime_instance_id#2: string；fingerprint#3: string；outcome#4: Observation；receipt_id#5: string

接收方写入：`fabric.secret_bindings`

完成证据：完整发布描述指定的Secret引用实际注入

失败/未知：不把Gateway Key当任意环境变量公开

### F08.9 workspace → runtime_control / RuntimeCoordination.Deploy

按当前入口/阶段与06/13状态机前置执行；不是无条件调用

请求 [RuntimeDeployCommand](rpc-messages.md#runtimedeploycommand)：context#1: CallContext；workspace_id#2: string；deployment_id#3: string；capability_version_id#4: string；deployment_descriptor#5: DeploymentDescriptor；resource_set_id#6: string；data_attachment_id#7: string；secret_binding_id#8: string；model_configuration_version#9: int64；model_selections#10: repeated ModelSelection；data_compatibility#11: DataCompatibility；runtime_instance_id#12: string；deployment_descriptor_digest#13: string；execution_epoch#14: int64；deployment_descriptor_object_ref#15: string

返回 [RuntimeReadback](rpc-messages.md#runtimereadback)：runtime_instance_id#1: string；workspace_id#2: string；deployment_id#3: string；state#4: RuntimeInstanceState；process_ready#5: bool；application_available#6: bool；credential_injection_verified#7: bool；artifact#8: ArtifactReference；applied_model_configuration_version#9: int64；readiness_receipt_id#10: string；outcome#11: Observation；observed_at#12: Timestamp；deployment_descriptor_digest#13: string；execution_epoch#14: int64；deployment_descriptor_object_ref#15: string

接收方写入：`runtime_control.runtime_actions`

完成证据：完整DeploymentDescriptor送执行层并实际ready

失败/未知：非就绪不开放入口

### F08.11 runtime_control → fabric / FabricRouteExecution.ActivateRoute

按当前入口/阶段与06/13状态机前置执行；不是无条件调用

请求 [RouteActivateCommand](rpc-messages.md#routeactivatecommand)：context#1: CallContext；workspace_id#2: string；operation_id#3: string；execution_epoch#4: int64；expected_route_generation#5: int64；provider_precondition#6: ProviderRevisionPrecondition；target_execution_resource_id#7: string；target_runtime_instance_id#8: string；target_deployment_id#9: string；confirmed_readiness_receipt_id#10: string

返回 [RouteReadback](rpc-messages.md#routereadback)：workspace_id#1: string；switch_id#2: string；observation#3: Observation；current_generation#4: int64；accepted_execution_epoch#5: int64；target_execution_resource_id#6: optional string；target_runtime_instance_id#7: optional string；target_deployment_id#8: optional string；provider_revision#9: string；provider_command_id#10: string；route_receipt_id#11: optional string；observed_at#12: Timestamp；error_code#13: optional ErrorCodeEnum

接收方写入：`fabric.route_bindings`, `fabric.route_switches`

完成证据：epoch/revision/target精确，provider实际路由确认

失败/未知：旧epoch/旧revision拒绝，丢响应ObserveRoute

### F09.4 workspace → runtime_control / RuntimeCoordination.ReloadModels

按当前入口/阶段与06/13状态机前置执行；不是无条件调用

请求 [RuntimeReloadCommand](rpc-messages.md#runtimereloadcommand)：context#1: CallContext；runtime_instance_id#2: string；expected_applied_version#3: int64；target_version#4: int64；selections#5: repeated ModelSelection

返回 [Operation](rpc-messages.md#operation)：operation_id#1: string；owner#2: OperationOwnerEnum；kind#3: OperationKindEnum；resource_id#4: string；status#5: OperationStatusEnum；stage#6: OperationStageEnum；observation_result#7: optional OperationObservationResultEnum；error_code#8: optional ErrorCodeEnum；request_id#9: string；created_at#10: Timestamp；updated_at#11: Timestamp；poll_after_seconds#12: optional int32

接收方写入：`runtime_control.runtime_actions`

完成证据：目标配置版本+selections实际应用

失败/未知：保存成功不等于reload成功

### F09.5 runtime_control → fabric / FabricRuntimeExecution.ObserveRuntime

按当前入口/阶段与06/13状态机前置执行；不是无条件调用

请求 [RuntimeReadbackRequest](rpc-messages.md#runtimereadbackrequest)：context#1: CallContext；runtime_instance_id#2: string；deployment_id#3: string

返回 [RuntimeReadback](rpc-messages.md#runtimereadback)：runtime_instance_id#1: string；workspace_id#2: string；deployment_id#3: string；state#4: RuntimeInstanceState；process_ready#5: bool；application_available#6: bool；credential_injection_verified#7: bool；artifact#8: ArtifactReference；applied_model_configuration_version#9: int64；readiness_receipt_id#10: string；outcome#11: Observation；observed_at#12: Timestamp；deployment_descriptor_digest#13: string；execution_epoch#14: int64；deployment_descriptor_object_ref#15: string

接收方写入：

完成证据：appliedVersion和选择相同，运行状态真实

失败/未知：unknown显示应用中/待核实，不覆盖已确认配置

### F10.4 workspace → runtime_control / RuntimeCoordination.Deploy

按当前入口/阶段与06/13状态机前置执行；不是无条件调用

请求 [RuntimeDeployCommand](rpc-messages.md#runtimedeploycommand)：context#1: CallContext；workspace_id#2: string；deployment_id#3: string；capability_version_id#4: string；deployment_descriptor#5: DeploymentDescriptor；resource_set_id#6: string；data_attachment_id#7: string；secret_binding_id#8: string；model_configuration_version#9: int64；model_selections#10: repeated ModelSelection；data_compatibility#11: DataCompatibility；runtime_instance_id#12: string；deployment_descriptor_digest#13: string；execution_epoch#14: int64；deployment_descriptor_object_ref#15: string

返回 [RuntimeReadback](rpc-messages.md#runtimereadback)：runtime_instance_id#1: string；workspace_id#2: string；deployment_id#3: string；state#4: RuntimeInstanceState；process_ready#5: bool；application_available#6: bool；credential_injection_verified#7: bool；artifact#8: ArtifactReference；applied_model_configuration_version#9: int64；readiness_receipt_id#10: string；outcome#11: Observation；observed_at#12: Timestamp；deployment_descriptor_digest#13: string；execution_epoch#14: int64；deployment_descriptor_object_ref#15: string

接收方写入：`runtime_control.runtime_instances`, `runtime_control.runtime_actions`

完成证据：新实例实际验证且不违反可写卷并发限制

失败/未知：保留旧选中版本/原数据义务

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

### F11.11 workspace → runtime_control / RuntimePlanChangeControl.RestoreAfterResourceChange

按当前入口/阶段与06/13状态机前置执行；不是无条件调用

请求 [RestorePlanChangeRuntimeCommand](rpc-messages.md#restoreplanchangeruntimecommand)：context#1: CallContext；workspace_id#2: string；plan_change_id#3: string；deployment_id#4: string；runtime_instance_id#5: string；confirmed_resource_action_id#6: string；target#7: ResourcePlanSnapshot；execution_epoch#8: int64；unchanged_application_descriptor_digest#9: string；current_workspace_version#10: int64

返回 [PlanChangeRuntimeReadback](rpc-messages.md#planchangeruntimereadback)：workspace_id#1: string；plan_change_id#2: string；runtime#3: RuntimeReadback；resources#4: ResourceReadback；target_limits_confirmed#5: bool；receipt_id#6: string；outcome#7: Observation

接收方写入：`runtime_control.runtime_actions`

完成证据：现有应用实际资源限制/挂载/健康确认；裸资源为owner证明的not_applicable

失败/未知：已有应用不可用不得applied，不让客户skipRuntime

### F13.2 workspace → runtime_control / RuntimeCoordination.Retire

按当前入口/阶段与06/13状态机前置执行；不是无条件调用

请求 [RuntimeStopCommand](rpc-messages.md#runtimestopcommand)：context#1: CallContext；runtime_instance_id#2: string；deployment_id#3: string；retained_data_attachment_id#4: string

返回 [Operation](rpc-messages.md#operation)：operation_id#1: string；owner#2: OperationOwnerEnum；kind#3: OperationKindEnum；resource_id#4: string；status#5: OperationStatusEnum；stage#6: OperationStageEnum；observation_result#7: optional OperationObservationResultEnum；error_code#8: optional ErrorCodeEnum；request_id#9: string；created_at#10: Timestamp；updated_at#11: Timestamp；poll_after_seconds#12: optional int32

接收方写入：`runtime_control.runtime_actions`

完成证据：确切旧Runtime停止/不存在

失败/未知：unknown不继续声称全环境已删

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

