# Ledger：API、字段与交互

> 源码快照 `7f5d05fe9b855d8af216caad714cf7fc85014d3c`；这是已定义目标的派生索引，不是业务实现完成声明。回到[总览](../15_domain_alignment.md)。

## 1. 边界与继承

模块：`services/ledger`。当前：**保留上游HTTP/旧schema；v2.26 Ledger RPC与新证据类型未完整接入**。

不可变Receipt、opaque provenance、保留/对账证据；不执行provider、不持有可花费余额、不编排Saga。

**继承 / 提取 / 新增：** 保留原receipt字节/ID/类型/幂等与evidence index；新Build无Workspace收据由本Owner显式准入。

**旧事实：** evidence_receipts、idempotency_keys、reconciliation_reports、evidence_index_entries及保留的review_policies。

**事务边界：** append receipt、幂等记录与本地证据索引原子提交；不为通用外观新增空Operation业务。

**业务顺序：** 各业务Owner提交其事实→Ledger验证形状与身份、append→调用方精确读回；ledger事件只是证据已记录，不取代资源/资金Owner读回。

**失败 / unknown：** 不能凭receipt字符串证明未核实副作用；append-only历史不重写。

## 2. 客户 REST 与后端 Owner

3 个规格REST操作；浏览器仅经BFF。表中的请求/响应为目标契约，不代表该RPC已挂载。字段展开见DTO目录。


### listReceipts

`GET /api/v2/admin/receipts`

权限：`platform_admin`；F：`F17`；主要成功状态：`200`。
| 入参位置 | 字段 | 类型 | 必填 |
| --- | --- | --- | --- |
| query | `cursor` | `OpaqueId` | 否 |
| query | `limit` | `integer` | 否 |
| query | `artifactDigest` | `Digest` | 否 |

Body：无独立命名body，见该操作schema；Response：[ReceiptPage](rest-schemas.md#receiptpage)

响应顶层字段：`items`: array<Receipt>（必填）；`nextCursor`: OpaqueId（可选）

涉及表（规格声明，不自动等于每次都写）：`ledger.receipts`
错误响应：`400`, `401`, `403`, `404`, `409`, `422`, `429`, `500`, `503`

### getReceipt

`GET /api/v2/admin/receipts/{receiptId}`

权限：`platform_admin`；F：`F17`；主要成功状态：`200`。
| 入参位置 | 字段 | 类型 | 必填 |
| --- | --- | --- | --- |
| path | `receiptId` | `OpaqueId` | 是 |

Body：无独立命名body，见该操作schema；Response：[Receipt](rest-schemas.md#receipt)

响应顶层字段：`id`: OpaqueId（必填）；`kind`: string（必填）；`owner`: Owner（必填）；`sourceSha`: string（可选）；`artifactDigest`: Digest（可选）；`operationId`: OpaqueId（可选）；`workflowRunId`: OpaqueId（可选）；`outcome`: string（必填）；`evidenceSummary`: string（必填）；`createdAt`: string/date-time（必填）

涉及表（规格声明，不自动等于每次都写）：`ledger.receipts`
错误响应：`400`, `401`, `403`, `404`, `409`, `422`, `429`, `500`, `503`

### listQualifications

`GET /api/v2/admin/qualifications`

权限：`platform_admin`；F：`F17`；主要成功状态：`200`。
| 入参位置 | 字段 | 类型 | 必填 |
| --- | --- | --- | --- |
| query | `cursor` | `OpaqueId` | 否 |
| query | `limit` | `integer` | 否 |
| query | `artifactDigest` | `Digest` | 否 |

Body：无独立命名body，见该操作schema；Response：[QualificationPage](rest-schemas.md#qualificationpage)

响应顶层字段：`items`: array<Qualification>（必填）；`nextCursor`: OpaqueId（可选）

涉及表（规格声明，不自动等于每次都写）：`ledger.receipts`
错误响应：`400`, `401`, `403`, `404`, `409`, `422`, `429`, `500`, `503`

## 3. 本域数据库全字段

`opl_ledger`：6 张表，73 列。字段权威：[02](../02_database_schema_complete.md)、[SQL](../contracts/schema.sql)；以下从db_inventory派生。**表不是自动等同DDD聚合根**；事务边界见第1节。


### ledger.receipts

append-only证据/hash/provenance；修正append新receipt，不覆写原事实或第二钱包

| 列 | SQL类型 | 可NULL | 默认值 | API血缘 / 原始来源 |
| --- | --- | --- | --- | --- |
| `id` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/Receipt/properties/id |
| `tenant_id` | `text` | 是 | `—` | 02_database_schema_complete.md#ledger.receipts.tenant_id |
| `source_owner` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/Receipt/properties/owner |
| `source_event_id` | `text` | 否 | `—` | 02_database_schema_complete.md#ledger.receipts.source_event_id |
| `kind` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/Receipt/properties/kind |
| `subject_type` | `text` | 否 | `—` | 02_database_schema_complete.md#ledger.receipts.subject_type |
| `subject_id` | `text` | 否 | `—` | 02_database_schema_complete.md#ledger.receipts.subject_id |
| `source_operation_id` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/Receipt/properties/operationId |
| `request_id` | `text` | 否 | `—` | 02_database_schema_complete.md#ledger.receipts.request_id |
| `evidence_sha256` | `text` | 否 | `—` | 02_database_schema_complete.md#ledger.receipts.evidence_sha256 |
| `evidence` | `jsonb` | 否 | `—` | 02_database_schema_complete.md#ledger.receipts.evidence |
| `previous_receipt_id` | `text` | 是 | `—` | 02_database_schema_complete.md#ledger.receipts.previous_receipt_id |
| `source_occurred_at` | `timestamptz` | 否 | `—` | 02_database_schema_complete.md#ledger.receipts.source_occurred_at |
| `created_at` | `timestamptz` | 否 | `now()` | 03_api_contract_complete.yaml#/components/schemas/Receipt/properties/createdAt |

约束：
- `PRIMARY KEY (id)`
- `FOREIGN KEY (previous_receipt_id) REFERENCES ledger.receipts (id) ON DELETE RESTRICT`
- `UNIQUE (source_owner, source_event_id)`
- `CHECK (evidence_sha256 ~ '^[0-9a-f]{64}$')`

索引：
- `{"name": "receipts_subject", "columns": "subject_type, subject_id, created_at DESC, id DESC", "unique": false, "where": null}`
- `{"name": "receipts_tenant", "columns": "tenant_id, created_at DESC, id DESC", "unique": false, "where": null}`
- `{"name": "receipts_operation", "columns": "source_owner, source_operation_id", "unique": false, "where": null}`

### ledger.reconciliations

对账结论只记录，不直接更改Workspace/钱包/provider

| 列 | SQL类型 | 可NULL | 默认值 | API血缘 / 原始来源 |
| --- | --- | --- | --- | --- |
| `id` | `text` | 否 | `—` | 02_database_schema_complete.md#ledger.reconciliations.id |
| `subject_type` | `text` | 否 | `—` | 02_database_schema_complete.md#ledger.reconciliations.subject_type |
| `subject_id` | `text` | 否 | `—` | 02_database_schema_complete.md#ledger.reconciliations.subject_id |
| `source_owner` | `text` | 否 | `—` | 02_database_schema_complete.md#ledger.reconciliations.source_owner |
| `owner_readback_ref` | `text` | 否 | `—` | 02_database_schema_complete.md#ledger.reconciliations.owner_readback_ref |
| `receipt_id` | `text` | 否 | `—` | 02_database_schema_complete.md#ledger.reconciliations.receipt_id |
| `result` | `text` | 否 | `—` | 02_database_schema_complete.md#ledger.reconciliations.result |
| `safe_difference` | `jsonb` | 否 | `—` | 02_database_schema_complete.md#ledger.reconciliations.safe_difference |
| `observed_at` | `timestamptz` | 否 | `—` | 02_database_schema_complete.md#ledger.reconciliations.observed_at |
| `created_at` | `timestamptz` | 否 | `now()` | 02_database_schema_complete.md#ledger.reconciliations.created_at |

约束：
- `PRIMARY KEY (id)`
- `FOREIGN KEY (receipt_id) REFERENCES ledger.receipts (id) ON DELETE RESTRICT`
- `CHECK (result IN ('matched','mismatch','unknown'))`

索引：
- `{"name": "reconciliations_subject", "columns": "subject_type, subject_id, created_at DESC, id DESC", "unique": false, "where": null}`

### ledger.outbox_events

本Owner状态事务同写immutable事件；revision只在本Owner聚合锁内分配

| 列 | SQL类型 | 可NULL | 默认值 | API血缘 / 原始来源 |
| --- | --- | --- | --- | --- |
| `id` | `text` | 否 | `—` | 02_database_schema_complete.md#ledger.outbox_events.id |
| `event_type` | `text` | 否 | `—` | 02_database_schema_complete.md#ledger.outbox_events.event_type |
| `schema_version` | `integer` | 否 | `—` | 02_database_schema_complete.md#ledger.outbox_events.schema_version |
| `aggregate_type` | `text` | 否 | `—` | 02_database_schema_complete.md#ledger.outbox_events.aggregate_type |
| `aggregate_id` | `text` | 否 | `—` | 02_database_schema_complete.md#ledger.outbox_events.aggregate_id |
| `aggregate_revision` | `bigint` | 否 | `—` | 02_database_schema_complete.md#ledger.outbox_events.aggregate_revision |
| `tenant_id` | `text` | 是 | `—` | 02_database_schema_complete.md#ledger.outbox_events.tenant_id |
| `correlation_id` | `text` | 否 | `—` | 02_database_schema_complete.md#ledger.outbox_events.correlation_id |
| `causation_id` | `text` | 是 | `—` | 02_database_schema_complete.md#ledger.outbox_events.causation_id |
| `payload` | `jsonb` | 否 | `—` | 02_database_schema_complete.md#ledger.outbox_events.payload |
| `payload_sha256` | `text` | 否 | `—` | 02_database_schema_complete.md#ledger.outbox_events.payload_sha256 |
| `occurred_at` | `timestamptz` | 否 | `—` | 02_database_schema_complete.md#ledger.outbox_events.occurred_at |
| `created_at` | `timestamptz` | 否 | `now()` | 02_database_schema_complete.md#ledger.outbox_events.created_at |

约束：
- `PRIMARY KEY (id)`
- `CHECK (schema_version > 0 AND aggregate_revision >= 0)`
- `CHECK (payload_sha256 ~ '^[0-9a-f]{64}$')`
- `UNIQUE (aggregate_type, aggregate_id, aggregate_revision, event_type)`

索引：
- `{"name": "outbox_events_aggregate", "columns": "aggregate_type, aggregate_id, aggregate_revision", "unique": false, "where": null}`

### ledger.outbox_deliveries

各consumer独立ACK，禁止单个ACK丢其他消费者事件；lease仅本库

| 列 | SQL类型 | 可NULL | 默认值 | API血缘 / 原始来源 |
| --- | --- | --- | --- | --- |
| `id` | `text` | 否 | `—` | 02_database_schema_complete.md#ledger.outbox_deliveries.id |
| `event_id` | `text` | 否 | `—` | 02_database_schema_complete.md#ledger.outbox_deliveries.event_id |
| `consumer_owner` | `text` | 否 | `—` | 02_database_schema_complete.md#ledger.outbox_deliveries.consumer_owner |
| `attempt_count` | `integer` | 否 | `0` | 02_database_schema_complete.md#ledger.outbox_deliveries.attempt_count |
| `next_attempt_at` | `timestamptz` | 否 | `now()` | 02_database_schema_complete.md#ledger.outbox_deliveries.next_attempt_at |
| `acknowledged_at` | `timestamptz` | 是 | `—` | 02_database_schema_complete.md#ledger.outbox_deliveries.acknowledged_at |
| `last_error_code` | `text` | 是 | `—` | 02_database_schema_complete.md#ledger.outbox_deliveries.last_error_code |
| `lease_token` | `text` | 是 | `—` | 02_database_schema_complete.md#ledger.outbox_deliveries.lease_token |
| `lease_until` | `timestamptz` | 是 | `—` | 02_database_schema_complete.md#ledger.outbox_deliveries.lease_until |
| `created_at` | `timestamptz` | 否 | `now()` | 02_database_schema_complete.md#ledger.outbox_deliveries.created_at |
| `updated_at` | `timestamptz` | 否 | `now()` | 02_database_schema_complete.md#ledger.outbox_deliveries.updated_at |

约束：
- `PRIMARY KEY (id)`
- `FOREIGN KEY (event_id) REFERENCES ledger.outbox_events (id) ON DELETE RESTRICT`
- `UNIQUE (event_id, consumer_owner)`
- `CHECK (attempt_count >= 0)`
- `CHECK ((lease_token IS NULL) = (lease_until IS NULL))`

索引：
- `{"name": "outbox_deliveries_pending", "columns": "next_attempt_at, id", "unique": false, "where": "acknowledged_at IS NULL"}`

### ledger.inbox_events

去重hash/业务状态/processed_at/后续Outbox同本库事务；乱序不能覆盖新版本

| 列 | SQL类型 | 可NULL | 默认值 | API血缘 / 原始来源 |
| --- | --- | --- | --- | --- |
| `id` | `text` | 否 | `—` | 02_database_schema_complete.md#ledger.inbox_events.id |
| `source_owner` | `text` | 否 | `—` | 02_database_schema_complete.md#ledger.inbox_events.source_owner |
| `source_event_id` | `text` | 否 | `—` | 02_database_schema_complete.md#ledger.inbox_events.source_event_id |
| `event_type` | `text` | 否 | `—` | 02_database_schema_complete.md#ledger.inbox_events.event_type |
| `schema_version` | `integer` | 否 | `—` | 02_database_schema_complete.md#ledger.inbox_events.schema_version |
| `aggregate_type` | `text` | 否 | `—` | 02_database_schema_complete.md#ledger.inbox_events.aggregate_type |
| `aggregate_id` | `text` | 否 | `—` | 02_database_schema_complete.md#ledger.inbox_events.aggregate_id |
| `aggregate_revision` | `bigint` | 否 | `—` | 02_database_schema_complete.md#ledger.inbox_events.aggregate_revision |
| `payload_sha256` | `text` | 否 | `—` | 02_database_schema_complete.md#ledger.inbox_events.payload_sha256 |
| `payload` | `jsonb` | 否 | `—` | 02_database_schema_complete.md#ledger.inbox_events.payload |
| `received_at` | `timestamptz` | 否 | `now()` | 02_database_schema_complete.md#ledger.inbox_events.received_at |
| `processed_at` | `timestamptz` | 是 | `—` | 02_database_schema_complete.md#ledger.inbox_events.processed_at |
| `result_resource_id` | `text` | 是 | `—` | 02_database_schema_complete.md#ledger.inbox_events.result_resource_id |
| `error_code` | `text` | 是 | `—` | 02_database_schema_complete.md#ledger.inbox_events.error_code |

约束：
- `PRIMARY KEY (id)`
- `UNIQUE (source_owner, source_event_id)`
- `CHECK (schema_version > 0 AND aggregate_revision >= 0)`
- `CHECK (payload_sha256 ~ '^[0-9a-f]{64}$')`

索引：
- `{"name": "inbox_events_pending", "columns": "received_at, id", "unique": false, "where": "processed_at IS NULL"}`
- `{"name": "inbox_events_aggregate", "columns": "source_owner, aggregate_type, aggregate_id, aggregate_revision", "unique": false, "where": null}`

### ledger.idempotency_records

命令创建同事务；operation_name是API操作名不是新Operation ID；安全响应无Key明文；义务存续无TTL清理

| 列 | SQL类型 | 可NULL | 默认值 | API血缘 / 原始来源 |
| --- | --- | --- | --- | --- |
| `id` | `text` | 否 | `—` | 02_database_schema_complete.md#ledger.idempotency_records.id |
| `tenant_scope` | `text` | 否 | `—` | 02_database_schema_complete.md#ledger.idempotency_records.tenant_scope |
| `actor_scope` | `text` | 否 | `—` | 02_database_schema_complete.md#ledger.idempotency_records.actor_scope |
| `operation_name` | `text` | 否 | `—` | 02_database_schema_complete.md#ledger.idempotency_records.operation_name |
| `idempotency_key` | `text` | 否 | `—` | 02_database_schema_complete.md#ledger.idempotency_records.idempotency_key |
| `request_sha256` | `text` | 否 | `—` | 02_database_schema_complete.md#ledger.idempotency_records.request_sha256 |
| `resource_id` | `text` | 否 | `—` | 02_database_schema_complete.md#ledger.idempotency_records.resource_id |
| `operation_id` | `text` | 是 | `—` | 02_database_schema_complete.md#ledger.idempotency_records.operation_id |
| `response_status` | `integer` | 否 | `—` | 02_database_schema_complete.md#ledger.idempotency_records.response_status |
| `response_body` | `jsonb` | 否 | `—` | 02_database_schema_complete.md#ledger.idempotency_records.response_body |
| `created_at` | `timestamptz` | 否 | `now()` | 02_database_schema_complete.md#ledger.idempotency_records.created_at |

约束：
- `PRIMARY KEY (id)`
- `UNIQUE (tenant_scope, actor_scope, operation_name, idempotency_key)`
- `CHECK (request_sha256 ~ '^[0-9a-f]{64}$')`
- `CHECK (response_status BETWEEN 100 AND 599)`

索引：
- `{"name": "idempotency_records_resource", "columns": "resource_id", "unique": false, "where": null}`

### 本Owner应实现的RPC方法全集（目标，不是挂载证据）

共享OwnerOperations/CommitReadback/Inbox分别由本域实现，不形成中央业务服务；通用Operation REST由BFF按显式Owner路由；本域只负责自己的Operation与授权。

| RPC | 请求 | 响应 |
| --- | --- | --- |
| `LedgerProductService.ListReceipts` | [ListReceiptsRpcRequest](rpc-messages.md#listreceiptsrpcrequest) | [ReceiptPage](rpc-messages.md#receiptpage) |
| `LedgerProductService.GetReceipt` | [GetReceiptRpcRequest](rpc-messages.md#getreceiptrpcrequest) | [Receipt](rpc-messages.md#receipt) |
| `LedgerProductService.ListQualifications` | [ListQualificationsRpcRequest](rpc-messages.md#listqualificationsrpcrequest) | [QualificationPage](rpc-messages.md#qualificationpage) |
| `LedgerCoordination.AppendReceipt` | [AppendReceiptRequest](rpc-messages.md#appendreceiptrequest) | [Receipt](rpc-messages.md#receipt) |
| `LedgerCoordination.ReadReceiptByReference` | [GetReceiptByReferenceRequest](rpc-messages.md#getreceiptbyreferencerequest) | [Receipt](rpc-messages.md#receipt) |
| `LedgerPlanChangeEvidence.AppendPlanChangeReceipt` | [AppendPlanChangeReceiptRequest](rpc-messages.md#appendplanchangereceiptrequest) | [Receipt](rpc-messages.md#receipt) |
| `LedgerPlanChangeEvidence.AppendPlanChangeRefundReceipt` | [AppendPlanChangeRefundReceiptRequest](rpc-messages.md#appendplanchangerefundreceiptrequest) | [Receipt](rpc-messages.md#receipt) |
| `DomainInbox.Deliver` | [DeliverEventRequest](rpc-messages.md#delivereventrequest) | [InboxAck](rpc-messages.md#inboxack) |

## 4. 跨域调用：调用者 → 拥有方 → 字段 → 结果

以下只列`domain_flows.json`声明的业务边；共享通道/尚无业务边的RPC不能推断成已经实现。


### F05.7 build → ledger / LedgerCoordination.AppendReceipt

按当前入口/阶段与06/13状态机前置执行；不是无条件调用

请求 [AppendReceiptRequest](rpc-messages.md#appendreceiptrequest)：context#1: CallContext；receipt#2: Receipt；evidence_digest#3: string；owner_evidence_reference#4: string

返回 [Receipt](rpc-messages.md#receipt)：id#1: string；kind#2: ReceiptKindEnum；owner#3: OwnerEnum；source_sha#4: optional string；artifact_digest#5: optional string；operation_id#6: optional string；workflow_run_id#7: optional string；outcome#8: ReceiptOutcomeEnum；evidence_summary#9: string；created_at#10: Timestamp

接收方写入：`ledger.receipts`

完成证据：返回receipt身份，另ReadReceiptByReference核对原输入

失败/未知：同idempotency key读回，不填假Workspace或重写receipt

### F08.12 workspace → ledger / LedgerCoordination.AppendReceipt

按当前入口/阶段与06/13状态机前置执行；不是无条件调用

请求 [AppendReceiptRequest](rpc-messages.md#appendreceiptrequest)：context#1: CallContext；receipt#2: Receipt；evidence_digest#3: string；owner_evidence_reference#4: string

返回 [Receipt](rpc-messages.md#receipt)：id#1: string；kind#2: ReceiptKindEnum；owner#3: OwnerEnum；source_sha#4: optional string；artifact_digest#5: optional string；operation_id#6: optional string；workflow_run_id#7: optional string；outcome#8: ReceiptOutcomeEnum；evidence_summary#9: string；created_at#10: Timestamp

接收方写入：`ledger.receipts`

完成证据：返回receipt身份，另ReadReceiptByReference核对原输入

失败/未知：同idempotency key读回，不填假Workspace或重写receipt

### F11.12 workspace → ledger / LedgerPlanChangeEvidence.AppendPlanChangeReceipt

按当前入口/阶段与06/13状态机前置执行；不是无条件调用

请求 [AppendPlanChangeReceiptRequest](rpc-messages.md#appendplanchangereceiptrequest)：context#1: CallContext；kind#2: ReceiptKindEnum；evidence#3: PlanChangeEvidence；accepted_calculation#4: PlanChangeCalculation；owner_evidence_reference#5: string

返回 [Receipt](rpc-messages.md#receipt)：id#1: string；kind#2: ReceiptKindEnum；owner#3: OwnerEnum；source_sha#4: optional string；artifact_digest#5: optional string；operation_id#6: optional string；workflow_run_id#7: optional string；outcome#8: ReceiptOutcomeEnum；evidence_summary#9: string；created_at#10: Timestamp

接收方写入：`ledger.receipts`

完成证据：本域CAS appliedAt/active计划/原E不变或下期正确周期后精确receipt

失败/未知：已受理/预约成功不能当资源已生效

### F12.7 workspace → ledger / LedgerCoordination.AppendReceipt

按当前入口/阶段与06/13状态机前置执行；不是无条件调用

请求 [AppendReceiptRequest](rpc-messages.md#appendreceiptrequest)：context#1: CallContext；receipt#2: Receipt；evidence_digest#3: string；owner_evidence_reference#4: string

返回 [Receipt](rpc-messages.md#receipt)：id#1: string；kind#2: ReceiptKindEnum；owner#3: OwnerEnum；source_sha#4: optional string；artifact_digest#5: optional string；operation_id#6: optional string；workflow_run_id#7: optional string；outcome#8: ReceiptOutcomeEnum；evidence_summary#9: string；created_at#10: Timestamp

接收方写入：`ledger.receipts`

完成证据：返回receipt身份，另ReadReceiptByReference核对原输入

失败/未知：同idempotency key读回，不填假Workspace或重写receipt

### F13.4 workspace → ledger / LedgerCoordination.AppendReceipt

按当前入口/阶段与06/13状态机前置执行；不是无条件调用

请求 [AppendReceiptRequest](rpc-messages.md#appendreceiptrequest)：context#1: CallContext；receipt#2: Receipt；evidence_digest#3: string；owner_evidence_reference#4: string

返回 [Receipt](rpc-messages.md#receipt)：id#1: string；kind#2: ReceiptKindEnum；owner#3: OwnerEnum；source_sha#4: optional string；artifact_digest#5: optional string；operation_id#6: optional string；workflow_run_id#7: optional string；outcome#8: ReceiptOutcomeEnum；evidence_summary#9: string；created_at#10: Timestamp

接收方写入：`ledger.receipts`

完成证据：返回receipt身份，另ReadReceiptByReference核对原输入

失败/未知：同idempotency key读回，不填假Workspace或重写receipt

### F17.3 owner → ledger / LedgerCoordination.ReadReceiptByReference

按当前入口/阶段与06/13状态机前置执行；不是无条件调用

请求 [GetReceiptByReferenceRequest](rpc-messages.md#getreceiptbyreferencerequest)：context#1: CallContext；owner#2: string；owner_evidence_reference#3: string

返回 [Receipt](rpc-messages.md#receipt)：id#1: string；kind#2: ReceiptKindEnum；owner#3: OwnerEnum；source_sha#4: optional string；artifact_digest#5: optional string；operation_id#6: optional string；workflow_run_id#7: optional string；outcome#8: ReceiptOutcomeEnum；evidence_summary#9: string；created_at#10: Timestamp

接收方写入：

完成证据：确切receipt/来源/输入output摘要

失败/未知：缺证据保持未验证，不发生产部署

## 5. 事件：谁生产、谁消费、哪些字段

aggregate_type由事件精确版本的x-aggregate-identity.type派生；aggregateId须与其idPayloadField一致。revision由生产者聚合事务内分配；consumer_owner显式选择本域Inbox。字段与实现状态不得混同。


### package.uploaded.v1

`capability` → `ledger`

聚合类型：`package_version`；ID来源：`payload.packageVersionId`（须等于aggregateId）

Envelope字段：`eventId`, `eventType`, `schemaVersion`, `owner`, `tenantId`, `aggregateId`, `aggregateVersion`, `occurredAt`, `requestId`, `payload`, `scope`

| payload字段 | 类型 | 必填 | 约束 |
| --- | --- | --- | --- |
| `packageVersionId` | `string` | 是 | minLength=1 |
| `packageId` | `string` | 是 | minLength=1 |
| `sha256` | `string` | 是 | pattern="^sha256:[0-9a-f]{64}$" |
| `sizeBytes` | `string` | 是 | pattern="^(?:0\|[1-9][0-9]{0,17}\|[1-8][0-9]{18}\|9[0-1][0-9]{17}\|92[0-1][0-9]{16}\|922[0-2][0-9]{15}\|9223[0-2][0-9]{14}\|92233[0-6][0-9]{13}\|922337[0-1][0-9]{12}\|92233720[0-2][0-9]{10}\|922337203[0-5][0-9]{9}\|9223372036[0-7][0-9]{8}\|92233720368[0-4][0-9]{7}\|922337203685[0-3][0-9]{6}\|9223372036854[0-6][0-9]{5}\|92233720368547[0-6][0-9]{4}\|922337203685477[0-4][0-9]{3}\|9223372036854775[0-7][0-9]{2}\|922337203685477580[0-6]\|9223372036854775807)$" |

Package上传实际校验通过
仅审计，不自动创建Build；客户必须显式createBuild。

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
