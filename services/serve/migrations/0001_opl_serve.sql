BEGIN;
DO $$ BEGIN
 IF current_database() <> 'opl_serve' THEN RAISE EXCEPTION 'Wrong database: expected opl_serve, got %', current_database(); END IF;
END $$;
SET LOCAL TIME ZONE 'UTC';
CREATE SCHEMA serve AUTHORIZATION opl_serve_owner;
REVOKE ALL ON SCHEMA serve FROM PUBLIC;
REVOKE CREATE ON SCHEMA public FROM PUBLIC;
REVOKE ALL ON DATABASE opl_serve FROM PUBLIC;
GRANT CONNECT ON DATABASE opl_serve TO opl_serve_writer;
SET LOCAL ROLE opl_serve_owner;

CREATE TABLE serve.agent_deployments (
  id text NOT NULL,
  workspace_id text NOT NULL,
  capability_version_id text NOT NULL,
  artifact_digest text NOT NULL,
  reference_claim_id text NOT NULL,
  runtime_instance_id text,
  previous_deployment_id text,
  operation_id text NOT NULL,
  status text NOT NULL DEFAULT 'queued',
  data_compatibility jsonb NOT NULL,
  data_migration_evidence_ref text,
  verification_evidence_ref text,
  error_code text,
  activated_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  execution_epoch bigint NOT NULL,
  confirmed_route_switch_id text,
  selection_commit_receipt_id text,
  PRIMARY KEY (id),
  FOREIGN KEY (previous_deployment_id) REFERENCES serve.agent_deployments (id) ON DELETE RESTRICT,
  CHECK (status IN ('queued','deploying','verifying','active','superseded','failed','rolling_back','rolled_back','needs_attention')),
  UNIQUE (id, workspace_id),
  CHECK (artifact_digest ~ '^sha256:[0-9a-f]{64}$'),
  CHECK (status <> 'active' OR (runtime_instance_id IS NOT NULL AND verification_evidence_ref IS NOT NULL AND activated_at IS NOT NULL)),
  CHECK (execution_epoch >= 0)
);
CREATE INDEX agent_deployments_workspace_list ON serve.agent_deployments (workspace_id, created_at DESC, id DESC);
CREATE INDEX agent_deployments_operation ON serve.agent_deployments (operation_id);
-- readiness/accessUrl真实回读；无active布尔、无订阅业务状态
CREATE TABLE serve.agent_runtime_instances (
  id text NOT NULL,
  workspace_id text NOT NULL,
  deployment_id text NOT NULL,
  artifact_digest text NOT NULL,
  fabric_resource_set_id text NOT NULL,
  fabric_execution_ref text,
  status text NOT NULL DEFAULT 'pending',
  access_url text,
  data_attachment_contract jsonb NOT NULL,
  applied_model_configuration_version bigint NOT NULL DEFAULT 0,
  readiness_evidence_ref text,
  error_code text,
  observed_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  execution_epoch bigint NOT NULL,
  deployment_descriptor jsonb NOT NULL,
  deployment_descriptor_digest text NOT NULL,
  deployment_descriptor_object_ref text NOT NULL,
  PRIMARY KEY (id),
  CHECK (status IN ('pending','starting','ready','stopped','failed','terminating','terminated')),
  UNIQUE (deployment_id),
  CHECK (artifact_digest ~ '^sha256:[0-9a-f]{64}$'),
  CHECK (applied_model_configuration_version >= 0),
  CHECK (status <> 'ready' OR (access_url IS NOT NULL AND readiness_evidence_ref IS NOT NULL AND observed_at IS NOT NULL)),
  CHECK (execution_epoch >= 0),
  CHECK (deployment_descriptor_digest ~ '^sha256:[0-9a-f]{64}$'),
  CHECK ((deployment_descriptor #>> '{artifact,digest}' = artifact_digest) IS TRUE)
);
CREATE INDEX agent_runtime_instances_workspace ON serve.agent_runtime_instances (workspace_id, created_at DESC, id DESC);
-- 先持久action再调用Fabric；旧Deployment响应不能覆盖新实例
CREATE TABLE serve.agent_runtime_actions (
  id text NOT NULL,
  runtime_instance_id text NOT NULL,
  command_id text NOT NULL,
  action text NOT NULL,
  expected_deployment_id text NOT NULL,
  input_snapshot jsonb NOT NULL,
  fabric_action_id text,
  observation_result text NOT NULL,
  error_code text,
  evidence_ref text,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (id),
  FOREIGN KEY (runtime_instance_id) REFERENCES serve.agent_runtime_instances (id) ON DELETE RESTRICT,
  UNIQUE (command_id),
  CHECK (action IN ('start','stop','terminate','reload','verify')),
  CHECK (observation_result IN ('confirmed','rejected','unknown'))
);
CREATE INDEX agent_runtime_actions_instance ON serve.agent_runtime_actions (runtime_instance_id, created_at DESC, id DESC);
-- Fabric alone owns observed route generation; Workspace-assigned execution epoch fences stale workers; generation advances only on verified route readback
CREATE TABLE serve.access_bindings (
  id text NOT NULL,
  workspace_id text NOT NULL,
  route_generation bigint NOT NULL DEFAULT 0,
  accepted_execution_epoch bigint NOT NULL DEFAULT 0,
  target_execution_resource_id text,
  last_confirmed_switch_id text,
  observed_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  provider_revision text,
  PRIMARY KEY (id),
  UNIQUE (workspace_id),
  UNIQUE (id, workspace_id),
  CHECK (route_generation >= 0 AND accepted_execution_epoch >= 0)
);
CREATE INDEX access_bindings_target ON serve.access_bindings (target_execution_resource_id);
-- Provider conditional revision CAS covers target plus epoch metadata; confirmed fence preserves target/generation but advances epoch/revision, then activate/rollback advances generation; any unknown blocks all new route actions
CREATE TABLE serve.access_switches (
  id text NOT NULL,
  route_binding_id text NOT NULL,
  workspace_id text NOT NULL,
  operation_owner text NOT NULL,
  operation_id text NOT NULL,
  expected_route_generation bigint NOT NULL,
  execution_epoch bigint NOT NULL,
  target_execution_resource_id text,
  previous_target_execution_resource_id text,
  provider_command_id text NOT NULL,
  provider_request_ref text,
  status text NOT NULL DEFAULT 'requested',
  observed_route_generation bigint,
  observed_execution_epoch bigint,
  evidence_ref text,
  workspace_selection_commit_receipt_id text,
  error_code text,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  action_kind text NOT NULL,
  expected_provider_revision text,
  observed_provider_revision text,
  expected_absence_receipt_id text,
  expected_absence_observed_at timestamptz,
  PRIMARY KEY (id),
  FOREIGN KEY (route_binding_id, workspace_id) REFERENCES serve.access_bindings (id, workspace_id) ON DELETE RESTRICT,
  CHECK (status IN ('requested','confirmed','rejected','unknown')),
  CHECK (expected_route_generation >= 0 AND execution_epoch >= 0),
  UNIQUE (provider_command_id),
  CHECK (workspace_selection_commit_receipt_id IS NULL OR status = 'confirmed'),
  CHECK (action_kind IN ('fence','activate','rollback')),
  CHECK (action_kind = 'fence' OR target_execution_resource_id IS NOT NULL),
  CHECK (expected_provider_revision IS NOT NULL OR expected_route_generation = 0),
  CHECK (status <> 'confirmed' OR ((observed_route_generation = expected_route_generation + CASE WHEN action_kind = 'fence' THEN 0 ELSE 1 END AND observed_execution_epoch = execution_epoch AND observed_provider_revision IS NOT NULL AND evidence_ref IS NOT NULL) IS TRUE)),
  CHECK ((expected_provider_revision IS NOT NULL AND expected_absence_receipt_id IS NULL AND expected_absence_observed_at IS NULL) OR (expected_provider_revision IS NULL AND expected_route_generation = 0 AND expected_absence_receipt_id IS NOT NULL AND expected_absence_observed_at IS NOT NULL))
);
CREATE UNIQUE INDEX route_switches_one_pending ON serve.access_switches (route_binding_id) WHERE status IN ('requested','unknown');
CREATE INDEX access_switches_operation ON serve.access_switches (operation_owner, operation_id, created_at DESC, id DESC);
ALTER TABLE serve.access_bindings ADD CONSTRAINT route_bindings_last_switch_fk FOREIGN KEY (last_confirmed_switch_id) REFERENCES serve.access_switches (id) ON DELETE RESTRICT;
-- 本Owner状态事务同写immutable事件；revision只在本Owner聚合锁内分配
CREATE TABLE serve.outbox_events (
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
CREATE INDEX outbox_events_aggregate ON serve.outbox_events (aggregate_type, aggregate_id, aggregate_revision);
-- 各consumer独立ACK，禁止单个ACK丢其他消费者事件；lease仅本库
CREATE TABLE serve.outbox_deliveries (
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
  FOREIGN KEY (event_id) REFERENCES serve.outbox_events (id) ON DELETE RESTRICT,
  UNIQUE (event_id, consumer_owner),
  CHECK (attempt_count >= 0),
  CHECK ((lease_token IS NULL) = (lease_until IS NULL))
);
CREATE INDEX outbox_deliveries_pending ON serve.outbox_deliveries (next_attempt_at, id) WHERE acknowledged_at IS NULL;
-- 去重hash/业务状态/processed_at/后续Outbox同本库事务；乱序不能覆盖新版本
CREATE TABLE serve.inbox_events (
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
CREATE INDEX inbox_events_pending ON serve.inbox_events (received_at, id) WHERE processed_at IS NULL;
CREATE INDEX inbox_events_aggregate ON serve.inbox_events (source_owner, aggregate_type, aggregate_id, aggregate_revision);
-- 命令创建同事务；operation_name是API操作名不是新Operation ID；安全响应无Key明文；义务存续无TTL清理
CREATE TABLE serve.idempotency_records (
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
CREATE INDEX idempotency_records_resource ON serve.idempotency_records (resource_id);
-- 目标Owner持异步Operation；BFF路由无中央writer；Build创建201回Job；只有Workspace配Saga
CREATE TABLE serve.operations (
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
CREATE INDEX operations_resource ON serve.operations (resource_id, created_at DESC, id DESC);
CREATE INDEX operations_tenant_list ON serve.operations (tenant_id, created_at DESC, id DESC);
CREATE INDEX operations_recovery ON serve.operations (status, updated_at);

GRANT USAGE ON SCHEMA serve TO opl_serve_writer;
GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA serve TO opl_serve_writer;
REVOKE UPDATE, DELETE ON serve.outbox_events FROM opl_serve_writer;
COMMIT;
-- END DATABASE opl_serve
