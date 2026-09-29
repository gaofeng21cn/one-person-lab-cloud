BEGIN;
DO $$ BEGIN
 IF current_database() <> 'opl_gateway' THEN RAISE EXCEPTION 'Wrong database: expected opl_gateway, got %', current_database(); END IF;
END $$;
SET LOCAL TIME ZONE 'UTC';
CREATE SCHEMA gateway AUTHORIZATION opl_gateway_owner;
REVOKE ALL ON SCHEMA gateway FROM PUBLIC;
REVOKE CREATE ON SCHEMA public FROM PUBLIC;
REVOKE ALL ON DATABASE opl_gateway FROM PUBLIC;
GRANT CONNECT ON DATABASE opl_gateway TO opl_gateway_writer;
SET LOCAL ROLE opl_gateway_owner;

-- 每成员独立Gateway身份；与Tenant钱包付款主体分开
CREATE TABLE gateway.identity_mappings (
  id text NOT NULL,
  actor_id text NOT NULL,
  sub2api_user_id text NOT NULL,
  external_identity_ref text NOT NULL,
  verified_at timestamptz NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (id),
  UNIQUE (actor_id),
  UNIQUE (sub2api_user_id)
);
CREATE INDEX identity_mappings_external ON gateway.identity_mappings (external_identity_ref);

-- 唯一活动钱包主体映射；跨成员费用动作必须外部Gateway明确委托能力，未证实不可擅自模拟
CREATE TABLE gateway.tenant_wallet_bindings (
  id text NOT NULL,
  tenant_id text NOT NULL,
  billing_sub2api_user_id text NOT NULL,
  delegation_ref text NOT NULL,
  verification_evidence_ref text NOT NULL,
  bound_at timestamptz NOT NULL,
  revoked_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (id)
);
CREATE UNIQUE INDEX wallet_binding_tenant ON gateway.tenant_wallet_bindings (tenant_id) WHERE revoked_at IS NULL;
CREATE UNIQUE INDEX wallet_binding_subject ON gateway.tenant_wallet_bindings (billing_sub2api_user_id) WHERE revoked_at IS NULL;

-- 轮换新Key验证后撤旧Key，允许短时两绑定；Secret不进事件/日志/数据库；不储Key明文
CREATE TABLE gateway.key_bindings (
  id text NOT NULL,
  tenant_id text NOT NULL,
  workspace_id text,
  actor_id text,
  external_key_id text NOT NULL,
  fingerprint text NOT NULL,
  secret_ref text,
  purpose text NOT NULL,
  model_ids text[] NOT NULL,
  rotation_of_key_binding_id text,
  observation_result text NOT NULL,
  revoked_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  name text NOT NULL,
  expires_at timestamptz,
  PRIMARY KEY (id),
  FOREIGN KEY (rotation_of_key_binding_id) REFERENCES gateway.key_bindings (id) ON DELETE RESTRICT,
  UNIQUE (external_key_id),
  CHECK (purpose IN ('workspace_managed','personal')),
  CHECK (observation_result IN ('confirmed','rejected','unknown')),
  CHECK ((purpose = 'workspace_managed') = (workspace_id IS NOT NULL)),
  CHECK (purpose <> 'workspace_managed' OR secret_ref IS NOT NULL)
);
CREATE INDEX key_bindings_workspace ON gateway.key_bindings (workspace_id);
CREATE INDEX key_bindings_tenant ON gateway.key_bindings (tenant_id, created_at DESC, id DESC);

-- Gateway请求事实不是wallet；unknown不得重复扣费或逆向退款；refund依原charge及资格
CREATE TABLE gateway.wallet_operations (
  id text NOT NULL,
  tenant_id text NOT NULL,
  wallet_binding_id text NOT NULL,
  workspace_id text,
  kind text NOT NULL,
  status text NOT NULL DEFAULT 'requested',
  amount_usd_micros bigint NOT NULL,
  original_wallet_operation_id text,
  business_idempotency_key text NOT NULL,
  request_fingerprint text NOT NULL,
  external_reference text,
  receipt_id text,
  refund_entitlement_ref text,
  authorization_receipt_ref text,
  error_code text,
  confirmed_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  refund_entitlement_snapshot jsonb,
  purpose text,
  plan_change_id text,
  coverage_start timestamptz,
  coverage_end timestamptz,
  PRIMARY KEY (id),
  FOREIGN KEY (wallet_binding_id) REFERENCES gateway.tenant_wallet_bindings (id) ON DELETE RESTRICT,
  FOREIGN KEY (original_wallet_operation_id) REFERENCES gateway.wallet_operations (id) ON DELETE RESTRICT,
  CHECK (kind IN ('charge','refund','recharge')),
  CHECK (status IN ('requested','confirmed','rejected','unknown')),
  UNIQUE (business_idempotency_key),
  CHECK (amount_usd_micros > 0),
  CHECK (kind <> 'refund' OR (original_wallet_operation_id IS NOT NULL AND refund_entitlement_ref IS NOT NULL)),
  CHECK (status <> 'confirmed' OR (external_reference IS NOT NULL AND confirmed_at IS NOT NULL)),
  CHECK (purpose IN ('base_period','upgrade_supplement','base_period_delete','upgrade_failure_full','supplement_delete_unused','next_period_plan_failure_full','recharge')),
  CHECK ((coverage_start IS NULL AND coverage_end IS NULL) OR (coverage_start IS NOT NULL AND coverage_end > coverage_start)),
  CHECK (purpose <> 'upgrade_supplement' OR (kind = 'charge' AND plan_change_id IS NOT NULL AND coverage_start IS NOT NULL AND coverage_end IS NOT NULL)),
  CHECK (purpose NOT IN ('base_period_delete','upgrade_failure_full','supplement_delete_unused','next_period_plan_failure_full') OR (kind = 'refund' AND original_wallet_operation_id IS NOT NULL AND refund_entitlement_snapshot IS NOT NULL))
);
CREATE UNIQUE INDEX wallet_operations_external ON gateway.wallet_operations (external_reference) WHERE external_reference IS NOT NULL;
CREATE INDEX wallet_operations_tenant ON gateway.wallet_operations (tenant_id, created_at DESC, id DESC);
CREATE INDEX wallet_operations_original ON gateway.wallet_operations (original_wallet_operation_id);

-- 本Owner状态事务同写immutable事件；revision只在本Owner聚合锁内分配
CREATE TABLE gateway.outbox_events (
  id text NOT NULL,
  event_type text NOT NULL,
  schema_version integer NOT NULL,
  aggregate_type text NOT NULL,
  aggregate_id text NOT NULL,
  aggregate_revision bigint NOT NULL,
  tenant_id text,
  correlation_id text NOT NULL,
  causation_id text,
  payload jsonb NOT NULL,
  payload_sha256 text NOT NULL,
  occurred_at timestamptz NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (id),
  CHECK (schema_version > 0 AND aggregate_revision >= 0),
  CHECK (payload_sha256 ~ '^[0-9a-f]{64}$'),
  UNIQUE (aggregate_type, aggregate_id, aggregate_revision, event_type)
);
CREATE INDEX outbox_events_aggregate ON gateway.outbox_events (aggregate_type, aggregate_id, aggregate_revision);

-- 各consumer独立ACK，禁止单个ACK丢其他消费者事件；lease仅本库
CREATE TABLE gateway.outbox_deliveries (
  id text NOT NULL,
  event_id text NOT NULL,
  consumer_owner text NOT NULL,
  attempt_count integer NOT NULL DEFAULT 0,
  next_attempt_at timestamptz NOT NULL DEFAULT now(),
  acknowledged_at timestamptz,
  last_error_code text,
  lease_token text,
  lease_until timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (id),
  FOREIGN KEY (event_id) REFERENCES gateway.outbox_events (id) ON DELETE RESTRICT,
  UNIQUE (event_id, consumer_owner),
  CHECK (attempt_count >= 0),
  CHECK ((lease_token IS NULL) = (lease_until IS NULL))
);
CREATE INDEX outbox_deliveries_pending ON gateway.outbox_deliveries (next_attempt_at, id) WHERE acknowledged_at IS NULL;

-- 去重hash/业务状态/processed_at/后续Outbox同本库事务；乱序不能覆盖新版本
CREATE TABLE gateway.inbox_events (
  id text NOT NULL,
  source_owner text NOT NULL,
  source_event_id text NOT NULL,
  event_type text NOT NULL,
  schema_version integer NOT NULL,
  aggregate_type text NOT NULL,
  aggregate_id text NOT NULL,
  aggregate_revision bigint NOT NULL,
  payload_sha256 text NOT NULL,
  payload jsonb NOT NULL,
  received_at timestamptz NOT NULL DEFAULT now(),
  processed_at timestamptz,
  result_resource_id text,
  error_code text,
  PRIMARY KEY (id),
  UNIQUE (source_owner, source_event_id),
  CHECK (schema_version > 0 AND aggregate_revision >= 0),
  CHECK (payload_sha256 ~ '^[0-9a-f]{64}$')
);
CREATE INDEX inbox_events_pending ON gateway.inbox_events (received_at, id) WHERE processed_at IS NULL;
CREATE INDEX inbox_events_aggregate ON gateway.inbox_events (source_owner, aggregate_type, aggregate_id, aggregate_revision);

-- 命令创建同事务；operation_name是API操作名不是新Operation ID；安全响应无Key明文；义务存续无TTL清理
CREATE TABLE gateway.idempotency_records (
  id text NOT NULL,
  tenant_scope text NOT NULL,
  actor_scope text NOT NULL,
  operation_name text NOT NULL,
  idempotency_key text NOT NULL,
  request_sha256 text NOT NULL,
  resource_id text NOT NULL,
  operation_id text,
  response_status integer NOT NULL,
  response_body jsonb NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (id),
  UNIQUE (tenant_scope, actor_scope, operation_name, idempotency_key),
  CHECK (request_sha256 ~ '^[0-9a-f]{64}$'),
  CHECK (response_status BETWEEN 100 AND 599)
);
CREATE INDEX idempotency_records_resource ON gateway.idempotency_records (resource_id);

-- 目标Owner持异步Operation；BFF路由无中央writer；Build创建201回Job；只有Workspace配Saga
CREATE TABLE gateway.operations (
  id text NOT NULL,
  tenant_id text,
  actor_id text NOT NULL,
  kind text NOT NULL,
  resource_id text NOT NULL,
  status text NOT NULL DEFAULT 'accepted',
  stage text NOT NULL,
  error_code text,
  observation_result text,
  request_id text NOT NULL,
  accepted_input jsonb NOT NULL,
  result jsonb,
  worker_lease_token text,
  worker_lease_until timestamptz,
  started_at timestamptz,
  completed_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (id),
  CHECK (status IN ('accepted','running','awaiting_confirmation','succeeded','failed','needs_attention','cancelled')),
  CHECK (observation_result IN ('confirmed','rejected','unknown')),
  CHECK ((worker_lease_token IS NULL) = (worker_lease_until IS NULL)),
  CHECK (status <> 'succeeded' OR completed_at IS NOT NULL)
);
CREATE INDEX operations_resource ON gateway.operations (resource_id, created_at DESC, id DESC);
CREATE INDEX operations_tenant_list ON gateway.operations (tenant_id, created_at DESC, id DESC);
CREATE INDEX operations_recovery ON gateway.operations (status, updated_at);
GRANT USAGE ON SCHEMA gateway TO opl_gateway_writer;
GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA gateway TO opl_gateway_writer;
REVOKE UPDATE, DELETE ON gateway.outbox_events FROM opl_gateway_writer;
COMMIT;
