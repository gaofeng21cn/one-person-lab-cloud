BEGIN;
DO $$ BEGIN
 IF current_database() <> 'opl_build' THEN RAISE EXCEPTION 'Wrong database: expected opl_build, got %', current_database(); END IF;
END $$;
SET LOCAL TIME ZONE 'UTC';
CREATE SCHEMA build AUTHORIZATION opl_build_owner;
REVOKE ALL ON SCHEMA build FROM PUBLIC;
REVOKE CREATE ON SCHEMA public FROM PUBLIC;
REVOKE ALL ON DATABASE opl_build FROM PUBLIC;
GRANT CONNECT ON DATABASE opl_build TO opl_build_writer;
SET LOCAL ROLE opl_build_owner;

-- createBuild(packageVersionId,webuiVersionId)冻结批准Runtime/catalogPolicy/digests/claims；Operation与Job同库创建，注册确认后succeeded
CREATE TABLE build.build_jobs (
  id text NOT NULL,
  tenant_id text,
  package_version_id text NOT NULL,
  runtime_version_id text NOT NULL,
  webui_version_id text NOT NULL,
  input_digest text NOT NULL,
  input_snapshot jsonb NOT NULL,
  status text NOT NULL DEFAULT 'queued',
  stage text NOT NULL,
  artifact_digest text,
  result_capability_version_id text,
  retry_of_build_job_id text,
  executor_ref text,
  worker_lease_token text,
  worker_lease_until timestamptz,
  error_code text,
  request_id text NOT NULL,
  created_by text NOT NULL,
  started_at timestamptz,
  finished_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  operation_id text NOT NULL,
  catalog_policy_id text NOT NULL,
  PRIMARY KEY (id),
  FOREIGN KEY (retry_of_build_job_id) REFERENCES build.build_jobs (id) ON DELETE RESTRICT,
  CHECK (status IN ('queued','validating','building','pushing','registering','succeeded','failed','needs_attention')),
  CHECK (input_digest ~ '^sha256:[0-9a-f]{64}$'),
  CHECK (artifact_digest IS NULL OR artifact_digest ~ '^sha256:[0-9a-f]{64}$'),
  CHECK (status <> 'succeeded' OR (artifact_digest IS NOT NULL AND result_capability_version_id IS NOT NULL AND finished_at IS NOT NULL)),
  CHECK ((worker_lease_token IS NULL) = (worker_lease_until IS NULL))
);
CREATE INDEX build_jobs_tenant_list ON build.build_jobs (tenant_id, created_at DESC, id DESC);
CREATE INDEX build_jobs_dispatch ON build.build_jobs (status, created_at);
CREATE INDEX build_jobs_retry ON build.build_jobs (retry_of_build_job_id);

-- Build输出不可变证据，非第二Registry/版本可见性writer
CREATE TABLE build.build_artifacts (
  id text NOT NULL,
  build_job_id text NOT NULL,
  artifact_repository text NOT NULL,
  artifact_digest text NOT NULL,
  size_bytes bigint NOT NULL,
  provenance jsonb NOT NULL,
  verification_evidence_ref text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  deployment_descriptor jsonb NOT NULL,
  deployment_descriptor_digest text NOT NULL,
  deployment_descriptor_object_ref text NOT NULL,
  PRIMARY KEY (id),
  FOREIGN KEY (build_job_id) REFERENCES build.build_jobs (id) ON DELETE RESTRICT,
  UNIQUE (build_job_id),
  CHECK (artifact_digest ~ '^sha256:[0-9a-f]{64}$'),
  CHECK (size_bytes > 0),
  CHECK (deployment_descriptor_digest ~ '^sha256:[0-9a-f]{64}$'),
  CHECK ((deployment_descriptor->>'schemaVersion' = 'opl-deployment-descriptor/v1') IS TRUE),
  CHECK ((deployment_descriptor #>> '{artifact,repository}' = artifact_repository) IS TRUE),
  CHECK ((deployment_descriptor #>> '{artifact,digest}' = artifact_digest) IS TRUE),
  CHECK ((deployment_descriptor #>> '{applicationRevision,image}' = artifact_repository || '@' || artifact_digest) IS TRUE)
);
CREATE INDEX build_artifacts_digest ON build.build_artifacts (artifact_repository, artifact_digest);

-- 真实日志顺序分页，写入前按明确敏感字段清单去密，不记录凭据
CREATE TABLE build.build_logs (
  id text NOT NULL,
  build_job_id text NOT NULL,
  sequence bigint NOT NULL,
  stage text NOT NULL,
  message text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  level text NOT NULL,
  PRIMARY KEY (id),
  FOREIGN KEY (build_job_id) REFERENCES build.build_jobs (id) ON DELETE RESTRICT,
  UNIQUE (build_job_id, sequence),
  CHECK (sequence >= 0),
  CHECK (level IN ('info','warning','error'))
);
CREATE INDEX build_logs_cursor ON build.build_logs (build_job_id, sequence);

-- 本Owner状态事务同写immutable事件；revision只在本Owner聚合锁内分配
CREATE TABLE build.outbox_events (
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
CREATE INDEX outbox_events_aggregate ON build.outbox_events (aggregate_type, aggregate_id, aggregate_revision);

-- 各consumer独立ACK，禁止单个ACK丢其他消费者事件；lease仅本库
CREATE TABLE build.outbox_deliveries (
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
  FOREIGN KEY (event_id) REFERENCES build.outbox_events (id) ON DELETE RESTRICT,
  UNIQUE (event_id, consumer_owner),
  CHECK (attempt_count >= 0),
  CHECK ((lease_token IS NULL) = (lease_until IS NULL))
);
CREATE INDEX outbox_deliveries_pending ON build.outbox_deliveries (next_attempt_at, id) WHERE acknowledged_at IS NULL;

-- 去重hash/业务状态/processed_at/后续Outbox同本库事务；乱序不能覆盖新版本
CREATE TABLE build.inbox_events (
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
CREATE INDEX inbox_events_pending ON build.inbox_events (received_at, id) WHERE processed_at IS NULL;
CREATE INDEX inbox_events_aggregate ON build.inbox_events (source_owner, aggregate_type, aggregate_id, aggregate_revision);

-- 命令创建同事务；operation_name是API操作名不是新Operation ID；安全响应无Key明文；义务存续无TTL清理
CREATE TABLE build.idempotency_records (
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
CREATE INDEX idempotency_records_resource ON build.idempotency_records (resource_id);

-- 目标Owner持异步Operation；BFF路由无中央writer；Build创建201回Job；只有Workspace配Saga
CREATE TABLE build.operations (
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
CREATE INDEX operations_resource ON build.operations (resource_id, created_at DESC, id DESC);
CREATE INDEX operations_tenant_list ON build.operations (tenant_id, created_at DESC, id DESC);
CREATE INDEX operations_recovery ON build.operations (status, updated_at);
ALTER TABLE build.build_jobs ADD CONSTRAINT build_jobs_operation_fk FOREIGN KEY (operation_id) REFERENCES build.operations (id) ON DELETE RESTRICT;
GRANT USAGE ON SCHEMA build TO opl_build_writer;
GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA build TO opl_build_writer;
REVOKE UPDATE, DELETE ON build.outbox_events FROM opl_build_writer;
REVOKE UPDATE, DELETE ON build.build_artifacts FROM opl_build_writer;
REVOKE UPDATE, DELETE ON build.build_logs FROM opl_build_writer;
COMMIT;
