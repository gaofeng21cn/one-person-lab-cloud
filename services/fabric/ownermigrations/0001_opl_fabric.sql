-- BEGIN DATABASE opl_fabric
BEGIN;
DO $$ BEGIN
 IF current_database() <> 'opl_fabric' THEN RAISE EXCEPTION 'Wrong database: expected opl_fabric, got %', current_database(); END IF;
END $$;
SET LOCAL TIME ZONE 'UTC';
CREATE SCHEMA fabric AUTHORIZATION opl_fabric_owner;
REVOKE ALL ON SCHEMA fabric FROM PUBLIC;
REVOKE CREATE ON SCHEMA public FROM PUBLIC;
REVOKE ALL ON DATABASE opl_fabric FROM PUBLIC;
GRANT CONNECT ON DATABASE opl_fabric TO opl_fabric_writer;
SET LOCAL ROLE opl_fabric_owner;

-- provider从批准套餐解析，不复制wallet余额或Cloud订阅价格
CREATE TABLE fabric.resource_sets (
  id text NOT NULL,
  tenant_id text NOT NULL,
  workspace_id text NOT NULL,
  provider text NOT NULL,
  provider_profile_ref text NOT NULL,
  region text NOT NULL,
  compute_plan_id text NOT NULL,
  storage_plan_id text NOT NULL,
  accepted_quote_id text NOT NULL,
  approved_specification jsonb NOT NULL,
  observation_result text NOT NULL,
  observed_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (id),
  CHECK (observation_result IN ('confirmed','rejected','unknown')),
  UNIQUE (workspace_id)
);
CREATE INDEX resource_sets_tenant ON fabric.resource_sets (tenant_id, created_at DESC, id DESC);

-- 仅预付包月/Local无费；不产生POSTPAID_BY_HOUR；provider事实带观察时间/证据
CREATE TABLE fabric.resources (
  id text NOT NULL,
  resource_set_id text NOT NULL,
  kind text NOT NULL,
  provider_resource_ref text,
  provider_purchase_key text NOT NULL,
  billing_mode text NOT NULL,
  requested_specification jsonb NOT NULL,
  observed_specification jsonb,
  observation_result text NOT NULL,
  provider_expires_at timestamptz,
  deletion_evidence_ref text,
  deleted_at timestamptz,
  observed_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (id),
  FOREIGN KEY (resource_set_id) REFERENCES fabric.resource_sets (id) ON DELETE RESTRICT,
  CHECK (kind IN ('compute','storage','network','execution')),
  CHECK (billing_mode IN ('PREPAID_MONTHLY','LOCAL_NO_CHARGE')),
  CHECK (observation_result IN ('confirmed','rejected','unknown')),
  UNIQUE (provider_purchase_key),
  CHECK (deleted_at IS NULL OR deletion_evidence_ref IS NOT NULL)
);
CREATE INDEX resources_provider_ref ON fabric.resources (provider_resource_ref);
CREATE INDEX resources_set ON fabric.resources (resource_set_id, kind);

-- Owner事务核验同resource_set和kind，更新/回滚满足卷单写挂载约束
CREATE TABLE fabric.attachments (
  id text NOT NULL,
  resource_set_id text NOT NULL,
  storage_resource_id text NOT NULL,
  execution_resource_id text NOT NULL,
  mount_path text NOT NULL,
  access_mode text NOT NULL,
  observation_result text NOT NULL,
  evidence_ref text,
  detached_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (id),
  FOREIGN KEY (resource_set_id) REFERENCES fabric.resource_sets (id) ON DELETE RESTRICT,
  FOREIGN KEY (storage_resource_id) REFERENCES fabric.resources (id) ON DELETE RESTRICT,
  FOREIGN KEY (execution_resource_id) REFERENCES fabric.resources (id) ON DELETE RESTRICT,
  CHECK (observation_result IN ('confirmed','rejected','unknown'))
);
CREATE UNIQUE INDEX attachments_active ON fabric.attachments (storage_resource_id, execution_resource_id, mount_path) WHERE detached_at IS NULL;

-- 仅Secret Store引用/版本/指纹；注入完成必须有效认证调用证据
CREATE TABLE fabric.secret_bindings (
  id text NOT NULL,
  resource_set_id text NOT NULL,
  execution_resource_id text NOT NULL,
  secret_ref text NOT NULL,
  purpose text NOT NULL,
  version text NOT NULL,
  fingerprint text NOT NULL,
  observation_result text NOT NULL,
  evidence_ref text,
  revoked_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (id),
  FOREIGN KEY (resource_set_id) REFERENCES fabric.resource_sets (id) ON DELETE RESTRICT,
  FOREIGN KEY (execution_resource_id) REFERENCES fabric.resources (id) ON DELETE RESTRICT,
  CHECK (observation_result IN ('confirmed','rejected','unknown'))
);
CREATE UNIQUE INDEX secret_bindings_active ON fabric.secret_bindings (execution_resource_id, purpose) WHERE revoked_at IS NULL;

-- 实费/采购/续费/删除须Instance保护流程与有界授权；unknown查询原provider请求
CREATE TABLE fabric.resource_actions (
  id text NOT NULL,
  resource_set_id text NOT NULL,
  resource_id text,
  command_id text NOT NULL,
  action text NOT NULL,
  provider_idempotency_key text NOT NULL,
  approved_input jsonb NOT NULL,
  authorization_receipt_ref text,
  provider_request_ref text,
  observation_result text NOT NULL,
  evidence_ref text,
  error_code text,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  execution_epoch bigint,
  execution_plan_digest text,
  execution_plan_bytes bytea,
  PRIMARY KEY (id),
  FOREIGN KEY (resource_set_id) REFERENCES fabric.resource_sets (id) ON DELETE RESTRICT,
  FOREIGN KEY (resource_id) REFERENCES fabric.resources (id) ON DELETE RESTRICT,
  UNIQUE (command_id),
  UNIQUE (provider_idempotency_key),
  CHECK (action IN ('allocate','attach','detach','resize','renew','suspend','resume','delete','inject_secret','prepare_resize')),
  CHECK (observation_result IN ('confirmed','rejected','unknown')),
  CHECK (execution_epoch IS NULL OR execution_epoch >= 0),
  CHECK ((execution_plan_digest IS NULL) = (execution_plan_bytes IS NULL)),
  CHECK (execution_plan_digest IS NULL OR execution_plan_digest = 'sha256:' || encode(sha256(execution_plan_bytes),'hex')),
  CHECK (action <> 'prepare_resize' OR (execution_plan_digest IS NOT NULL AND evidence_ref IS NOT NULL AND observation_result = 'confirmed'))
);
CREATE INDEX resource_actions_set ON fabric.resource_actions (resource_set_id, created_at DESC, id DESC);

-- 本Owner状态事务同写immutable事件；revision只在本Owner聚合锁内分配
CREATE TABLE fabric.outbox_events (
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
CREATE INDEX outbox_events_aggregate ON fabric.outbox_events (aggregate_type, aggregate_id, aggregate_revision);

-- 各consumer独立ACK，禁止单个ACK丢其他消费者事件；lease仅本库
CREATE TABLE fabric.outbox_deliveries (
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
  FOREIGN KEY (event_id) REFERENCES fabric.outbox_events (id) ON DELETE RESTRICT,
  UNIQUE (event_id, consumer_owner),
  CHECK (attempt_count >= 0),
  CHECK ((lease_token IS NULL) = (lease_until IS NULL))
);
CREATE INDEX outbox_deliveries_pending ON fabric.outbox_deliveries (next_attempt_at, id) WHERE acknowledged_at IS NULL;

-- 去重hash/业务状态/processed_at/后续Outbox同本库事务；乱序不能覆盖新版本
CREATE TABLE fabric.inbox_events (
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
CREATE INDEX inbox_events_pending ON fabric.inbox_events (received_at, id) WHERE processed_at IS NULL;
CREATE INDEX inbox_events_aggregate ON fabric.inbox_events (source_owner, aggregate_type, aggregate_id, aggregate_revision);

-- 命令创建同事务；operation_name是API操作名不是新Operation ID；安全响应无Key明文；义务存续无TTL清理
CREATE TABLE fabric.idempotency_records (
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
CREATE INDEX idempotency_records_resource ON fabric.idempotency_records (resource_id);

-- 目标Owner持异步Operation；BFF路由无中央writer；Build创建201回Job；只有Workspace配Saga
CREATE TABLE fabric.operations (
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
CREATE INDEX operations_resource ON fabric.operations (resource_id, created_at DESC, id DESC);
CREATE INDEX operations_tenant_list ON fabric.operations (tenant_id, created_at DESC, id DESC);
CREATE INDEX operations_recovery ON fabric.operations (status, updated_at);


GRANT USAGE ON SCHEMA fabric TO opl_fabric_writer;
GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA fabric TO opl_fabric_writer;
REVOKE UPDATE, DELETE ON fabric.outbox_events FROM opl_fabric_writer;
COMMIT;
-- END DATABASE opl_fabric
