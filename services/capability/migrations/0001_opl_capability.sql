BEGIN;
DO $$ BEGIN
 IF current_database() <> 'opl_capability' THEN RAISE EXCEPTION 'Wrong database: expected opl_capability, got %', current_database(); END IF;
END $$;
SET LOCAL TIME ZONE 'UTC';
CREATE SCHEMA capability AUTHORIZATION opl_capability_owner;
REVOKE ALL ON SCHEMA capability FROM PUBLIC;
REVOKE CREATE ON SCHEMA public FROM PUBLIC;
REVOKE ALL ON DATABASE opl_capability FROM PUBLIC;
GRANT CONNECT ON DATABASE opl_capability TO opl_capability_writer;
SET LOCAL ROLE opl_capability_owner;

-- 官方空间无零UUID伪Tenant；私有空间经Tenant owner授权
CREATE TABLE capability.namespaces (
  id text NOT NULL,
  tenant_id text,
  name text NOT NULL,
  kind text NOT NULL,
  description text,
  status text NOT NULL DEFAULT 'active',
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (id),
  CHECK (kind IN ('official','tenant_default','tenant_custom')),
  CHECK (status IN ('active','archived')),
  CHECK ((kind = 'official') = (tenant_id IS NULL))
);
CREATE UNIQUE INDEX namespaces_tenant_name ON capability.namespaces (tenant_id, name) WHERE tenant_id IS NOT NULL;
CREATE UNIQUE INDEX namespaces_official_name ON capability.namespaces (name) WHERE tenant_id IS NULL;
CREATE UNIQUE INDEX namespaces_default ON capability.namespaces (tenant_id) WHERE kind = 'tenant_default' AND status = 'active';

-- visibility与namespace.kind同事务校验；不存最新对象或Build状态
CREATE TABLE capability.packages (
  id text NOT NULL,
  namespace_id text NOT NULL,
  name text NOT NULL,
  description text,
  visibility text NOT NULL,
  status text NOT NULL DEFAULT 'active',
  created_by text NOT NULL,
  archived_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (id),
  FOREIGN KEY (namespace_id) REFERENCES capability.namespaces (id) ON DELETE RESTRICT,
  CHECK (visibility IN ('official','private')),
  CHECK (status IN ('active','archived')),
  UNIQUE (namespace_id, name),
  CHECK ((status = 'archived') = (archived_at IS NOT NULL))
);
CREATE INDEX packages_namespace_list ON capability.packages (namespace_id, created_at DESC, id DESC);


-- Strict PublisherContract schema; repository/digest must match contract and admitted namespace; descriptor is propagated unchanged into Build and execution
CREATE TABLE capability.webui_versions (
  id text NOT NULL,
  name text NOT NULL,
  version_label text NOT NULL,
  artifact_repository text NOT NULL,
  artifact_digest text NOT NULL,
  status text NOT NULL DEFAULT 'approved',
  approved_by text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  runtime_abi_versions text[] NOT NULL,
  ui_protocol_version text NOT NULL,
  admission_receipt_id text NOT NULL,
  publisher_namespace_id text NOT NULL,
  publisher_contract_digest text NOT NULL,
  publisher_contract jsonb NOT NULL,
  publisher_contract_object_ref text NOT NULL,
  PRIMARY KEY (id),
  CHECK (status IN ('approved','deprecated','revoked')),
  UNIQUE (name, version_label),
  CHECK (artifact_digest ~ '^sha256:[0-9a-f]{64}$'),
  CHECK (publisher_contract_digest ~ '^sha256:[0-9a-f]{64}$'),
  CHECK ((jsonb_typeof(publisher_contract) = 'object') IS TRUE),
  CHECK ((publisher_contract->>'schemaVersion' = 'opl-publisher-contract/v1') IS TRUE),
  CHECK ((publisher_contract->>'kind' = 'webui') IS TRUE),
  CHECK ((publisher_contract->>'publisherNamespaceId' = publisher_namespace_id) IS TRUE),
  CHECK ((publisher_contract #>> '{image,repository}' = artifact_repository) IS TRUE),
  CHECK ((publisher_contract #>> '{image,digest}' = artifact_digest) IS TRUE),
  CHECK ((publisher_contract #>> '{image,platform,os}' = 'linux') IS TRUE),
  CHECK ((publisher_contract #>> '{image,platform,architecture}' IN ('amd64','arm64')) IS TRUE),
  CHECK ((publisher_contract->'runtimeAbiVersions' = to_jsonb(runtime_abi_versions)) IS TRUE),
  CHECK ((publisher_contract->>'uiProtocolVersion' = ui_protocol_version) IS TRUE)
);
CREATE INDEX webui_versions_status ON capability.webui_versions (status, created_at DESC, id DESC);
CREATE INDEX webui_versions_publisher ON capability.webui_versions (publisher_namespace_id);


-- 上传申请冻结sha256/size，实测相符才uploaded；构建状态只在Build
CREATE TABLE capability.package_versions (
  id text NOT NULL,
  package_id text NOT NULL,
  version_label text NOT NULL,
  status text NOT NULL DEFAULT 'upload_pending',
  sha256 text NOT NULL,
  size_bytes bigint NOT NULL,
  object_ref text,
  manifest jsonb,
  validation_error_code text,
  created_by text NOT NULL,
  verified_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (id),
  FOREIGN KEY (package_id) REFERENCES capability.packages (id) ON DELETE RESTRICT,
  CHECK (status IN ('upload_pending','uploaded','rejected')),
  UNIQUE (package_id, version_label),
  UNIQUE (id, package_id),
  CHECK (sha256 ~ '^sha256:[0-9a-f]{64}$'),
  CHECK (size_bytes > 0),
  CHECK (status <> 'uploaded' OR (object_ref IS NOT NULL AND verified_at IS NOT NULL AND manifest IS NOT NULL))
);
CREATE INDEX package_versions_list ON capability.package_versions (package_id, created_at DESC, id DESC);

-- Storage multipart直传凭据由Capability签发；sizeBytes/sha256从PackageVersion、completedParts从已确认upload_chunks读回，不双写数组
CREATE TABLE capability.upload_sessions (
  id text NOT NULL,
  package_version_id text NOT NULL,
  status text NOT NULL DEFAULT 'uploading',
  part_size_bytes bigint NOT NULL,
  object_ref text NOT NULL,
  provider_upload_ref text NOT NULL,
  expires_at timestamptz NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (id),
  FOREIGN KEY (package_version_id) REFERENCES capability.package_versions (id) ON DELETE RESTRICT,
  CHECK (status IN ('uploading','completed','expired')),
  CHECK (part_size_bytes > 0)
);
CREATE UNIQUE INDEX upload_sessions_active ON capability.upload_sessions (package_version_id) WHERE status = 'uploading';

-- 同session/partNumber固定sha256与size；确认后保存etag；unknown读取同Provider part，重签URL不创建第二分片
CREATE TABLE capability.upload_chunks (
  id text NOT NULL,
  upload_session_id text NOT NULL,
  size_bytes bigint NOT NULL,
  sha256 text NOT NULL,
  provider_part_ref text NOT NULL,
  observation_result text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  part_number integer NOT NULL,
  etag text,
  PRIMARY KEY (id),
  FOREIGN KEY (upload_session_id) REFERENCES capability.upload_sessions (id) ON DELETE RESTRICT,
  CHECK (sha256 ~ '^sha256:[0-9a-f]{64}$'),
  CHECK (observation_result IN ('confirmed','rejected','unknown')),
  UNIQUE (upload_session_id, part_number),
  CHECK (part_number > 0),
  CHECK (size_bytes > 0),
  CHECK (observation_result <> 'confirmed' OR etag IS NOT NULL)
);
CREATE INDEX upload_chunks_session ON capability.upload_chunks (upload_session_id);

-- build引用五项齐全才ready；legacy_application保留旧exact revision/digest且五个构建引用全空，不伪造Package/Build；deleted仅墓碑
CREATE TABLE capability.capability_versions (
  id text NOT NULL,
  package_id text,
  package_version_id text,
  build_job_id text,
  version_label text NOT NULL,
  runtime_version_id text,
  webui_version_id text,
  artifact_repository text NOT NULL,
  artifact_digest text NOT NULL,
  status text NOT NULL DEFAULT 'ready',
  model_requirements jsonb NOT NULL,
  data_compatibility jsonb NOT NULL,
  provenance_evidence jsonb NOT NULL,
  deletion_operation_id text,
  deleted_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  provenance text NOT NULL,
  legacy_application_revision_id text,
  deployment_descriptor jsonb NOT NULL,
  deployment_descriptor_digest text NOT NULL,
  deployment_descriptor_object_ref text NOT NULL,
  PRIMARY KEY (id),
  FOREIGN KEY (package_id) REFERENCES capability.packages (id) ON DELETE RESTRICT,
  FOREIGN KEY (package_version_id, package_id) REFERENCES capability.package_versions (id, package_id) ON DELETE RESTRICT,
  FOREIGN KEY (webui_version_id) REFERENCES capability.webui_versions (id) ON DELETE RESTRICT,
  CHECK (status IN ('ready','deprecated','deleting','deleted')),
  UNIQUE (build_job_id),
  CHECK (artifact_digest ~ '^sha256:[0-9a-f]{64}$'),
  CHECK ((status = 'deleted') = (deleted_at IS NOT NULL)),
  CHECK (provenance IN ('build','legacy_application')),
  CHECK ((provenance = 'build' AND num_nonnulls(package_id, package_version_id, build_job_id, runtime_version_id, webui_version_id) = 5 AND legacy_application_revision_id IS NULL) OR (provenance = 'legacy_application' AND num_nonnulls(package_id, package_version_id, build_job_id, runtime_version_id, webui_version_id) = 0 AND legacy_application_revision_id IS NOT NULL)),
  CHECK (deployment_descriptor_digest ~ '^sha256:[0-9a-f]{64}$'),
  CHECK ((deployment_descriptor->>'schemaVersion' = 'opl-deployment-descriptor/v1') IS TRUE),
  CHECK ((deployment_descriptor #>> '{artifact,repository}' = artifact_repository) IS TRUE),
  CHECK ((deployment_descriptor #>> '{artifact,digest}' = artifact_digest) IS TRUE),
  CHECK ((deployment_descriptor #>> '{applicationRevision,image}' = artifact_repository || '@' || artifact_digest) IS TRUE),
  CHECK ((deployment_descriptor->>'provenance' = provenance) IS TRUE),
  CHECK (provenance <> 'legacy_application' OR ((deployment_descriptor->>'legacyApplicationRevisionId' = legacy_application_revision_id AND NOT (deployment_descriptor ?| ARRAY['packageVersionId','buildInputDigest','runtimeContract','runtimeContractReference','webuiContract','webuiContractReference'])) IS TRUE)),
  CHECK (provenance <> 'build' OR ((deployment_descriptor->>'packageVersionId' = package_version_id AND deployment_descriptor #>> '{runtimeContractReference,versionId}' = runtime_version_id AND deployment_descriptor #>> '{webuiContractReference,versionId}' = webui_version_id) IS TRUE))
);
CREATE INDEX capability_versions_list ON capability.capability_versions (package_id, created_at DESC, id DESC);
CREATE INDEX capability_versions_artifact ON capability.capability_versions (artifact_repository, artifact_digest);
CREATE INDEX capability_versions_input ON capability.capability_versions (package_version_id);
CREATE UNIQUE INDEX capability_versions_legacy ON capability.capability_versions (legacy_application_revision_id) WHERE legacy_application_revision_id IS NOT NULL;

-- Four-way ReferenceTarget maps to exactly one local FK; Bind records original operation/input digest; Release requires typed owner terminal receipt and confirmed readback
CREATE TABLE capability.reference_claims (
  id text NOT NULL,
  target_type text NOT NULL,
  package_version_id text,
  capability_version_id text,
  runtime_version_id text,
  webui_version_id text,
  claimant_owner text NOT NULL,
  claimant_resource_id text NOT NULL,
  purpose text NOT NULL,
  request_id text NOT NULL,
  bound_at timestamptz,
  released_at timestamptz,
  release_evidence_ref text,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  bound_operation_id text,
  bound_input_digest text,
  release_evidence jsonb,
  PRIMARY KEY (id),
  CHECK (target_type IN ('package_version','capability_version','runtime_version','webui_version')),
  CHECK (num_nonnulls(package_version_id, capability_version_id, runtime_version_id, webui_version_id) = 1),
  CHECK (released_at IS NULL OR release_evidence_ref IS NOT NULL),
  FOREIGN KEY (package_version_id) REFERENCES capability.package_versions (id) ON DELETE RESTRICT,
  CHECK ((target_type = 'package_version') = (package_version_id IS NOT NULL)),
  FOREIGN KEY (capability_version_id) REFERENCES capability.capability_versions (id) ON DELETE RESTRICT,
  CHECK ((target_type = 'capability_version') = (capability_version_id IS NOT NULL)),
  CHECK ((target_type = 'runtime_version') = (runtime_version_id IS NOT NULL)),
  FOREIGN KEY (webui_version_id) REFERENCES capability.webui_versions (id) ON DELETE RESTRICT,
  CHECK ((target_type = 'webui_version') = (webui_version_id IS NOT NULL)),
  CHECK ((bound_at IS NULL AND bound_operation_id IS NULL AND bound_input_digest IS NULL) OR (bound_at IS NOT NULL AND bound_operation_id IS NOT NULL AND bound_input_digest IS NOT NULL)),
  CHECK (bound_input_digest IS NULL OR bound_input_digest ~ '^sha256:[0-9a-f]{64}$'),
  CHECK ((released_at IS NULL) = (release_evidence IS NULL))
);
CREATE UNIQUE INDEX reference_claims_package_version ON capability.reference_claims (package_version_id, claimant_owner, claimant_resource_id, purpose) WHERE released_at IS NULL AND package_version_id IS NOT NULL;
CREATE UNIQUE INDEX reference_claims_capability_version ON capability.reference_claims (capability_version_id, claimant_owner, claimant_resource_id, purpose) WHERE released_at IS NULL AND capability_version_id IS NOT NULL;
CREATE UNIQUE INDEX reference_claims_runtime_version ON capability.reference_claims (runtime_version_id, claimant_owner, claimant_resource_id, purpose) WHERE released_at IS NULL AND runtime_version_id IS NOT NULL;
CREATE UNIQUE INDEX reference_claims_webui_version ON capability.reference_claims (webui_version_id, claimant_owner, claimant_resource_id, purpose) WHERE released_at IS NULL AND webui_version_id IS NOT NULL;

-- 本Owner状态事务同写immutable事件；revision只在本Owner聚合锁内分配
CREATE TABLE capability.outbox_events (
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
CREATE INDEX outbox_events_aggregate ON capability.outbox_events (aggregate_type, aggregate_id, aggregate_revision);

-- 各consumer独立ACK，禁止单个ACK丢其他消费者事件；lease仅本库
CREATE TABLE capability.outbox_deliveries (
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
  FOREIGN KEY (event_id) REFERENCES capability.outbox_events (id) ON DELETE RESTRICT,
  UNIQUE (event_id, consumer_owner),
  CHECK (attempt_count >= 0),
  CHECK ((lease_token IS NULL) = (lease_until IS NULL))
);
CREATE INDEX outbox_deliveries_pending ON capability.outbox_deliveries (next_attempt_at, id) WHERE acknowledged_at IS NULL;

-- 去重hash/业务状态/processed_at/后续Outbox同本库事务；乱序不能覆盖新版本
CREATE TABLE capability.inbox_events (
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
CREATE INDEX inbox_events_pending ON capability.inbox_events (received_at, id) WHERE processed_at IS NULL;
CREATE INDEX inbox_events_aggregate ON capability.inbox_events (source_owner, aggregate_type, aggregate_id, aggregate_revision);

-- 命令创建同事务；operation_name是API操作名不是新Operation ID；安全响应无Key明文；义务存续无TTL清理
CREATE TABLE capability.idempotency_records (
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
CREATE INDEX idempotency_records_resource ON capability.idempotency_records (resource_id);

-- 目标Owner持异步Operation；BFF路由无中央writer；Build创建201回Job；只有Workspace配Saga
CREATE TABLE capability.operations (
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
CREATE INDEX operations_resource ON capability.operations (resource_id, created_at DESC, id DESC);
CREATE INDEX operations_tenant_list ON capability.operations (tenant_id, created_at DESC, id DESC);
CREATE INDEX operations_recovery ON capability.operations (status, updated_at);

-- Publisher Registry namespaces are distinct from Tenant Agent groups; third-party publishers use separately admitted prefixes
CREATE TABLE capability.publisher_namespaces (
  id text NOT NULL,
  name text NOT NULL,
  kind text NOT NULL,
  registry_id text NOT NULL,
  repository_prefix text NOT NULL,
  admission_receipt_id text NOT NULL,
  status text NOT NULL DEFAULT 'approved',
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (id),
  CHECK (kind IN ('official','third_party')),
  CHECK (status IN ('approved','revoked')),
  UNIQUE (registry_id, repository_prefix),
  UNIQUE (name),
  CHECK (repository_prefix <> '' AND repository_prefix NOT LIKE '%..%' AND repository_prefix NOT LIKE '%@%' AND repository_prefix NOT LIKE '%://%')
);
CREATE INDEX publisher_namespaces_catalog ON capability.publisher_namespaces (kind, status, created_at DESC, id DESC);
ALTER TABLE capability.capability_versions ADD CONSTRAINT capability_versions_deletion_operation_fk FOREIGN KEY (deletion_operation_id) REFERENCES capability.operations (id) ON DELETE RESTRICT;
ALTER TABLE capability.webui_versions ADD CONSTRAINT webui_versions_publisher_namespace_fk FOREIGN KEY (publisher_namespace_id) REFERENCES capability.publisher_namespaces (id) ON DELETE RESTRICT;
GRANT USAGE ON SCHEMA capability TO opl_capability_writer;
GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA capability TO opl_capability_writer;
REVOKE UPDATE, DELETE ON capability.outbox_events FROM opl_capability_writer;
REVOKE UPDATE, DELETE ON capability.webui_versions FROM opl_capability_writer;
GRANT UPDATE (status, updated_at) ON capability.webui_versions TO opl_capability_writer;
REVOKE UPDATE, DELETE ON capability.publisher_namespaces FROM opl_capability_writer;
GRANT UPDATE (status, updated_at) ON capability.publisher_namespaces TO opl_capability_writer;
COMMIT;
-- END DATABASE opl_capability
