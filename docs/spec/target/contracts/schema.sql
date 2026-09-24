-- OPL Cloud target architecture schema, 2026-09-21. DESIGN ONLY; NOT APPLIED.
-- Multi-database source bundle, NOT a single-connection migration.
-- Split BEGIN DATABASE ... END DATABASE blocks; execute each with its own named DB connection.
-- Deployment owner provisions databases + distinct NOLOGIN writer roles and runtime LOGIN roles.
-- Login/Secret/role creation is deliberately not in this product schema. Do not run on production locally.
-- No cross-database FK/JOIN/transaction; all opaque IDs supplied as text, no UUID regeneration.
-- Each block has current_database() guard. Amount bigint => REST decimal string; timestamptz => UTC.


-- BEGIN DATABASE opl_tenant
BEGIN;
DO $$ BEGIN
 IF current_database() <> 'opl_tenant' THEN RAISE EXCEPTION 'Wrong database: expected opl_tenant, got %', current_database(); END IF;
END $$;
SET LOCAL TIME ZONE 'UTC';
CREATE SCHEMA tenant AUTHORIZATION opl_tenant_owner;
REVOKE ALL ON SCHEMA tenant FROM PUBLIC;
REVOKE CREATE ON SCHEMA public FROM PUBLIC;
REVOKE ALL ON DATABASE opl_tenant FROM PUBLIC;
GRANT CONNECT ON DATABASE opl_tenant TO opl_tenant_writer;
SET LOCAL ROLE opl_tenant_owner;

-- CloudIdentity owns Tenant authorization; permission_version advances on access changes; deleted restore window starts at actual deleted_at, not request time
CREATE TABLE tenant.tenants (
  id text NOT NULL,
  name text NOT NULL,
  status text NOT NULL DEFAULT 'active',
  suspended_at timestamptz,
  deletion_requested_at timestamptz,
  restore_until timestamptz,
  deleted_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  permission_version bigint NOT NULL DEFAULT 0,
  PRIMARY KEY (id),
  CHECK (status IN ('active','suspended','deleting','deleted')),
  CHECK (permission_version >= 0),
  CHECK ((deleted_at IS NULL AND restore_until IS NULL) OR (deleted_at IS NOT NULL AND restore_until = deleted_at + interval '15 days')),
  CHECK (status <> 'deleted' OR deleted_at IS NOT NULL)
);
CREATE INDEX tenants_status_list ON tenant.tenants (status, created_at DESC, id DESC);

-- 同actor至多一个未撤销membership；更改锁Tenant行且不得删除最后owner
CREATE TABLE tenant.tenant_members (
  id text NOT NULL,
  tenant_id text NOT NULL,
  actor_id text NOT NULL,
  role text NOT NULL,
  revoked_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (id),
  FOREIGN KEY (tenant_id) REFERENCES tenant.tenants (id) ON DELETE RESTRICT,
  CHECK (role IN ('owner','admin','member')),
  UNIQUE (tenant_id, actor_id)
);
CREATE UNIQUE INDEX tenant_members_one_active ON tenant.tenant_members (actor_id) WHERE revoked_at IS NULL;
CREATE INDEX tenant_members_tenant ON tenant.tenant_members (tenant_id, created_at DESC, id DESC);

-- 邀请token只存hash；接受时锁invite并校验actor活动Tenant
CREATE TABLE tenant.invitations (
  id text NOT NULL,
  tenant_id text NOT NULL,
  invitee_gateway_subject_id text NOT NULL,
  role text NOT NULL,
  token_hash text NOT NULL,
  invited_by text NOT NULL,
  accepted_by text,
  expires_at timestamptz NOT NULL,
  accepted_at timestamptz,
  revoked_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (id),
  FOREIGN KEY (tenant_id) REFERENCES tenant.tenants (id) ON DELETE RESTRICT,
  CHECK (role IN ('admin','member')),
  UNIQUE (token_hash),
  CHECK (NOT (accepted_at IS NOT NULL AND revoked_at IS NOT NULL))
);
CREATE INDEX member_invites_tenant ON tenant.invitations (tenant_id, created_at DESC, id DESC);

-- 只存cookie/CSRF摘要及Gateway会话Secret Store引用；每次请求复验Tenant/member
CREATE TABLE tenant.sessions (
  id text NOT NULL,
  session_hash text NOT NULL,
  actor_id text NOT NULL,
  tenant_id text,
  gateway_session_ref text NOT NULL,
  csrf_hash text NOT NULL,
  expires_at timestamptz NOT NULL,
  revoked_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (id),
  FOREIGN KEY (tenant_id) REFERENCES tenant.tenants (id) ON DELETE RESTRICT,
  UNIQUE (session_hash)
);
CREATE INDEX sessions_actor ON tenant.sessions (actor_id);
CREATE INDEX sessions_tenant ON tenant.sessions (tenant_id);

-- append-only权限审计，不含token/Key/密码
CREATE TABLE tenant.audit_events (
  id text NOT NULL,
  tenant_id text,
  actor_id text NOT NULL,
  action text NOT NULL,
  resource_type text NOT NULL,
  resource_id text NOT NULL,
  request_id text NOT NULL,
  safe_details jsonb NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  outcome text NOT NULL,
  PRIMARY KEY (id),
  FOREIGN KEY (tenant_id) REFERENCES tenant.tenants (id) ON DELETE RESTRICT
);
CREATE INDEX tenant_audit_list ON tenant.audit_events (tenant_id, created_at DESC, id DESC);
CREATE INDEX tenant_audit_request ON tenant.audit_events (request_id);

-- 本Owner状态事务同写immutable事件；revision只在本Owner聚合锁内分配
CREATE TABLE tenant.outbox_events (
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
CREATE INDEX outbox_events_aggregate ON tenant.outbox_events (aggregate_type, aggregate_id, aggregate_revision);

-- 各consumer独立ACK，禁止单个ACK丢其他消费者事件；lease仅本库
CREATE TABLE tenant.outbox_deliveries (
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
  FOREIGN KEY (event_id) REFERENCES tenant.outbox_events (id) ON DELETE RESTRICT,
  UNIQUE (event_id, consumer_owner),
  CHECK (attempt_count >= 0),
  CHECK ((lease_token IS NULL) = (lease_until IS NULL))
);
CREATE INDEX outbox_deliveries_pending ON tenant.outbox_deliveries (next_attempt_at, id) WHERE acknowledged_at IS NULL;

-- 去重hash/业务状态/processed_at/后续Outbox同本库事务；乱序不能覆盖新版本
CREATE TABLE tenant.inbox_events (
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
CREATE INDEX inbox_events_pending ON tenant.inbox_events (received_at, id) WHERE processed_at IS NULL;
CREATE INDEX inbox_events_aggregate ON tenant.inbox_events (source_owner, aggregate_type, aggregate_id, aggregate_revision);

-- 命令创建同事务；operation_name是API操作名不是新Operation ID；安全响应无Key明文；义务存续无TTL清理
CREATE TABLE tenant.idempotency_records (
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
CREATE INDEX idempotency_records_resource ON tenant.idempotency_records (resource_id);

-- 目标Owner持异步Operation；BFF路由无中央writer；Build创建201回Job；只有Workspace配Saga
CREATE TABLE tenant.operations (
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
CREATE INDEX operations_resource ON tenant.operations (resource_id, created_at DESC, id DESC);
CREATE INDEX operations_tenant_list ON tenant.operations (tenant_id, created_at DESC, id DESC);
CREATE INDEX operations_recovery ON tenant.operations (status, updated_at);

-- Opaque context introspected by CloudIdentity; authenticated mTLS caller, audience, action, resource and current permission version all bound; no bearer-token body
CREATE TABLE tenant.authorization_contexts (
  id text NOT NULL,
  scope_type text NOT NULL,
  tenant_id text,
  actor_id text NOT NULL,
  session_id text,
  permission_version bigint NOT NULL,
  audience_owner text NOT NULL,
  action text NOT NULL,
  resource_kind text NOT NULL,
  resource_id text,
  issuer text NOT NULL,
  issued_at timestamptz NOT NULL,
  expires_at timestamptz NOT NULL,
  revoked_at timestamptz,
  accepted_operation_grant_id text,
  PRIMARY KEY (id),
  FOREIGN KEY (tenant_id) REFERENCES tenant.tenants (id) ON DELETE RESTRICT,
  FOREIGN KEY (session_id) REFERENCES tenant.sessions (id) ON DELETE RESTRICT,
  CHECK (scope_type IN ('tenant','platform')),
  CHECK (issuer IN ('cloud_identity')),
  CHECK ((scope_type = 'tenant') = (tenant_id IS NOT NULL)),
  CHECK (permission_version >= 0),
  CHECK (expires_at > issued_at),
  CHECK (num_nonnulls(session_id,accepted_operation_grant_id) = 1)
);
CREATE INDEX authorization_contexts_session ON tenant.authorization_contexts (session_id);
CREATE INDEX authorization_contexts_actor_scope ON tenant.authorization_contexts (actor_id, tenant_id, expires_at);

-- Bounded original accepted-operation obligation; revocation permits only approved completion/cancel/closeout, never new procurement
CREATE TABLE tenant.accepted_operation_grants (
  id text NOT NULL,
  scope_type text NOT NULL,
  tenant_id text,
  actor_id text NOT NULL,
  accepted_operation_owner text NOT NULL,
  accepted_operation_id text NOT NULL,
  accepted_action text NOT NULL,
  resource_id text NOT NULL,
  accepted_permission_version bigint NOT NULL,
  allowed_actions text[] NOT NULL,
  issued_at timestamptz NOT NULL,
  expires_at timestamptz,
  revoked_at timestamptz,
  obligation_completed_at timestamptz,
  mode text NOT NULL,
  renewal_consent_id text,
  subscription_period_id text,
  PRIMARY KEY (id),
  FOREIGN KEY (tenant_id) REFERENCES tenant.tenants (id) ON DELETE RESTRICT,
  CHECK (scope_type IN ('tenant','platform')),
  CHECK ((scope_type = 'tenant') = (tenant_id IS NOT NULL)),
  CHECK (accepted_permission_version >= 0),
  CHECK (cardinality(allowed_actions) > 0 AND array_position(allowed_actions,NULL) IS NULL),
  CHECK (expires_at IS NULL OR expires_at > issued_at),
  UNIQUE (accepted_operation_owner, accepted_operation_id),
  CHECK (mode IN ('continue_original','closeout_only','revoked'))
);
CREATE INDEX operation_grants_resource ON tenant.accepted_operation_grants (tenant_id, resource_id);
CREATE INDEX operation_grants_open ON tenant.accepted_operation_grants (issued_at) WHERE obligation_completed_at IS NULL;
ALTER TABLE tenant.authorization_contexts ADD CONSTRAINT authorization_context_grant_fk FOREIGN KEY (accepted_operation_grant_id) REFERENCES tenant.accepted_operation_grants (id) ON DELETE RESTRICT;
GRANT USAGE ON SCHEMA tenant TO opl_tenant_writer;
GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA tenant TO opl_tenant_writer;
REVOKE UPDATE, DELETE ON tenant.outbox_events FROM opl_tenant_writer;
REVOKE UPDATE, DELETE ON tenant.audit_events FROM opl_tenant_writer;
COMMIT;
-- END DATABASE opl_tenant

-- BEGIN DATABASE opl_capability
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

-- BEGIN DATABASE opl_build
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
-- END DATABASE opl_build

-- BEGIN DATABASE opl_workspace
BEGIN;
DO $$ BEGIN
 IF current_database() <> 'opl_workspace' THEN RAISE EXCEPTION 'Wrong database: expected opl_workspace, got %', current_database(); END IF;
END $$;
SET LOCAL TIME ZONE 'UTC';
CREATE SCHEMA workspace AUTHORIZATION opl_workspace_owner;
REVOKE ALL ON SCHEMA workspace FROM PUBLIC;
REVOKE CREATE ON SCHEMA public FROM PUBLIC;
REVOKE ALL ON DATABASE opl_workspace FROM PUBLIC;
GRANT CONNECT ON DATABASE opl_workspace TO opl_workspace_writer;
SET LOCAL ROLE opl_workspace_owner;

-- 当前已接受业务选择；迁移裸资源capabilityVersionId可空，UI不得称已部署；Serve负责Deployment/current selection，expiresAt从订阅投影
CREATE TABLE workspace.workspaces (
  id text NOT NULL,
  tenant_id text NOT NULL,
  name text NOT NULL,
  status text NOT NULL DEFAULT 'provisioning',
  compute_plan_id text NOT NULL,
  storage_plan_id text NOT NULL,
  capability_version_id text,
  delivery_model text NOT NULL,
  model_configuration_version bigint NOT NULL DEFAULT 0,
  active_operation_id text,
  legacy_origin_id text,
  created_by text NOT NULL,
  deleted_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  version bigint NOT NULL,
  PRIMARY KEY (id),
  CHECK (status IN ('provisioning','active','updating','suspended','deleting','deleted','failed','needs_attention')),
  CHECK ((status = 'deleted') = (deleted_at IS NOT NULL)),
  CHECK ((delivery_model = 'legacy_resource_only' AND capability_version_id IS NULL) OR (delivery_model IN ('imported_application','agent_saas') AND capability_version_id IS NOT NULL)),
  CHECK (model_configuration_version >= 0),
  CHECK (version >= 0),
  UNIQUE (id, tenant_id)
);
CREATE INDEX workspaces_tenant_list ON workspace.workspaces (tenant_id, created_at DESC, id DESC);
CREATE UNIQUE INDEX workspaces_legacy ON workspace.workspaces (legacy_origin_id) WHERE legacy_origin_id IS NOT NULL;


-- 当前已付周期权威；到期默认停用不自动扣款；status从周期/Workspace生命周期派生；续费Operation幂等创建新周期，不改旧历史；quoted仅存接受Quote快照，legacy_import保留原purchase ID及原义务证据，禁止造Quote/重新扣费；缺原policy或receipt须标明确缺口并拒绝受影响动作
CREATE TABLE workspace.subscriptions (
  id text NOT NULL,
  workspace_id text NOT NULL,
  accepted_quote_id text,
  accepted_quote_snapshot jsonb,
  billing_subject_ref text NOT NULL,
  current_period_start timestamptz NOT NULL,
  current_period_end timestamptz NOT NULL,
  last_charge_wallet_operation_id text,
  active_change_operation_id text,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  period_months integer NOT NULL,
  provenance text NOT NULL,
  legacy_purchase_id text,
  legacy_obligation_snapshot jsonb,
  renewal_mode text NOT NULL,
  renewal_consent_id text,
  renewal_consent_snapshot jsonb,
  renewal_settings_version bigint NOT NULL DEFAULT 0,
  version bigint NOT NULL DEFAULT 0,
  current_price_policy_version_id text,
  current_monthly_usd_micros bigint,
  billing_anchor_day integer,
  PRIMARY KEY (id),
  FOREIGN KEY (workspace_id) REFERENCES workspace.workspaces (id) ON DELETE RESTRICT,
  UNIQUE (workspace_id),
  CHECK (current_period_end > current_period_start),
  CHECK (period_months > 0),
  CHECK (provenance IN ('quoted','legacy_import')),
  CHECK ((provenance = 'quoted' AND accepted_quote_id IS NOT NULL AND accepted_quote_snapshot IS NOT NULL AND legacy_purchase_id IS NULL AND legacy_obligation_snapshot IS NULL) OR (provenance = 'legacy_import' AND accepted_quote_id IS NULL AND accepted_quote_snapshot IS NULL AND legacy_purchase_id IS NOT NULL AND legacy_obligation_snapshot IS NOT NULL)),
  CHECK (renewal_mode IN ('manual','automatic')),
  CHECK (renewal_mode <> 'automatic' OR (renewal_consent_id IS NOT NULL AND renewal_consent_snapshot IS NOT NULL)),
  CHECK (renewal_settings_version >= 0),
  CHECK (version >= 0),
  CHECK (current_monthly_usd_micros IS NULL OR current_monthly_usd_micros >= 0),
  CHECK (billing_anchor_day IS NULL OR billing_anchor_day BETWEEN 1 AND 31),
  CHECK (provenance <> 'quoted' OR (current_price_policy_version_id IS NOT NULL AND current_monthly_usd_micros IS NOT NULL AND billing_anchor_day IS NOT NULL)),
  UNIQUE (id, workspace_id)
);
CREATE INDEX subscriptions_expiry ON workspace.subscriptions (current_period_end, id);

-- 确认付费周期不可变历史；Local零报价wallet operation可空，receipt记录零费事实不假造扣款；quoted仅存接受Quote快照，legacy_import保留原purchase ID及原义务证据，禁止造Quote/重新扣费；缺原policy或receipt须标明确缺口并拒绝受影响动作
CREATE TABLE workspace.subscription_periods (
  id text NOT NULL,
  subscription_id text NOT NULL,
  quote_id text,
  accepted_quote_snapshot jsonb,
  period_start timestamptz NOT NULL,
  period_end timestamptz NOT NULL,
  billing_key text NOT NULL,
  charge_wallet_operation_id text,
  charge_receipt_id text,
  created_at timestamptz NOT NULL DEFAULT now(),
  provenance text NOT NULL,
  legacy_purchase_id text,
  legacy_obligation_snapshot jsonb,
  PRIMARY KEY (id),
  FOREIGN KEY (subscription_id) REFERENCES workspace.subscriptions (id) ON DELETE RESTRICT,
  UNIQUE (subscription_id, period_start),
  UNIQUE (billing_key),
  UNIQUE (charge_wallet_operation_id),
  CHECK (period_end > period_start),
  CHECK (provenance IN ('quoted','legacy_import')),
  CHECK ((provenance = 'quoted' AND quote_id IS NOT NULL AND accepted_quote_snapshot IS NOT NULL AND legacy_purchase_id IS NULL AND legacy_obligation_snapshot IS NULL) OR (provenance = 'legacy_import' AND quote_id IS NULL AND accepted_quote_snapshot IS NULL AND legacy_purchase_id IS NOT NULL AND legacy_obligation_snapshot IS NOT NULL)),
  CHECK (provenance <> 'quoted' OR charge_receipt_id IS NOT NULL),
  UNIQUE (id, subscription_id)
);
CREATE INDEX subscription_periods_list ON workspace.subscription_periods (subscription_id, period_start DESC, id DESC);

-- 新配置新version；Runtime有效调用验证后事务推进Workspace.modelConfigurationVersion
CREATE TABLE workspace.model_configurations (
  id text NOT NULL,
  workspace_id text NOT NULL,
  version bigint NOT NULL,
  gateway_key_binding_id text NOT NULL,
  operation_id text NOT NULL,
  runtime_reload_observation text NOT NULL,
  verification_evidence_ref text,
  created_by text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  selections jsonb NOT NULL,
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (id),
  FOREIGN KEY (workspace_id) REFERENCES workspace.workspaces (id) ON DELETE RESTRICT,
  UNIQUE (workspace_id, version),
  CHECK (version > 0),
  CHECK (runtime_reload_observation IN ('confirmed','rejected','unknown'))
);
CREATE INDEX model_configurations_workspace ON workspace.model_configurations (workspace_id, version DESC);

-- 固定commandId/幂等键重试；unknown读原Owner，不制造新副作用或逆向补偿
CREATE TABLE workspace.saga_steps (
  id text NOT NULL,
  operation_id text NOT NULL,
  step_key text NOT NULL,
  sequence integer NOT NULL,
  target_owner text NOT NULL,
  command_id text NOT NULL,
  idempotency_key text NOT NULL,
  input_snapshot jsonb NOT NULL,
  observation_result text,
  owner_result_ref text,
  compensation_command_id text,
  compensation_observation text,
  attempt_count integer NOT NULL DEFAULT 0,
  next_attempt_at timestamptz,
  error_code text,
  confirmed_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (id),
  UNIQUE (operation_id, step_key),
  UNIQUE (command_id),
  CHECK (sequence >= 0 AND attempt_count >= 0),
  CHECK (observation_result IN ('confirmed','rejected','unknown')),
  CHECK (compensation_observation IN ('confirmed','rejected','unknown'))
);
CREATE INDEX saga_steps_recovery ON workspace.saga_steps (next_attempt_at) WHERE confirmed_at IS NULL;
CREATE INDEX saga_steps_operation ON workspace.saga_steps (operation_id, sequence);

-- 本Owner状态事务同写immutable事件；revision只在本Owner聚合锁内分配
CREATE TABLE workspace.outbox_events (
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
CREATE INDEX outbox_events_aggregate ON workspace.outbox_events (aggregate_type, aggregate_id, aggregate_revision);

-- 各consumer独立ACK，禁止单个ACK丢其他消费者事件；lease仅本库
CREATE TABLE workspace.outbox_deliveries (
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
  FOREIGN KEY (event_id) REFERENCES workspace.outbox_events (id) ON DELETE RESTRICT,
  UNIQUE (event_id, consumer_owner),
  CHECK (attempt_count >= 0),
  CHECK ((lease_token IS NULL) = (lease_until IS NULL))
);
CREATE INDEX outbox_deliveries_pending ON workspace.outbox_deliveries (next_attempt_at, id) WHERE acknowledged_at IS NULL;

-- 去重hash/业务状态/processed_at/后续Outbox同本库事务；乱序不能覆盖新版本
CREATE TABLE workspace.inbox_events (
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
CREATE INDEX inbox_events_pending ON workspace.inbox_events (received_at, id) WHERE processed_at IS NULL;
CREATE INDEX inbox_events_aggregate ON workspace.inbox_events (source_owner, aggregate_type, aggregate_id, aggregate_revision);

-- 命令创建同事务；operation_name是API操作名不是新Operation ID；安全响应无Key明文；义务存续无TTL清理
CREATE TABLE workspace.idempotency_records (
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
CREATE INDEX idempotency_records_resource ON workspace.idempotency_records (resource_id);

-- 目标Owner持异步Operation；BFF路由无中央writer；Build创建201回Job；只有Workspace配Saga
CREATE TABLE workspace.operations (
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
CREATE INDEX operations_resource ON workspace.operations (resource_id, created_at DESC, id DESC);
CREATE INDEX operations_tenant_list ON workspace.operations (tenant_id, created_at DESC, id DESC);
CREATE INDEX operations_recovery ON workspace.operations (status, updated_at);

-- D17 PlanChange is the sole active/scheduled resource-change identity; upgrade uses fixed UnixMilli quote basis, downgrade is inert until E and paid next-period obligation; Operation IDs do not revive terminal operations
CREATE TABLE workspace.plan_changes (
  id text NOT NULL,
  workspace_id text NOT NULL,
  tenant_id text NOT NULL,
  kind text NOT NULL,
  status text NOT NULL,
  source_compute_plan_id text NOT NULL,
  source_storage_plan_id text NOT NULL,
  target_compute_plan_id text NOT NULL,
  target_storage_plan_id text NOT NULL,
  source_price_policy_version_id text NOT NULL,
  target_price_policy_version_id text NOT NULL,
  source_subscription_id text NOT NULL,
  source_subscription_version bigint NOT NULL,
  source_period_id text NOT NULL,
  quote_id text NOT NULL,
  policy_version text NOT NULL,
  quote_at timestamptz NOT NULL,
  period_start timestamptz NOT NULL,
  period_end timestamptz NOT NULL,
  source_monthly_usd_micros bigint NOT NULL,
  target_monthly_usd_micros bigint NOT NULL,
  charge_usd_micros bigint NOT NULL,
  planned_effective_at timestamptz NOT NULL,
  applied_at timestamptz,
  operation_id text NOT NULL,
  execution_operation_id text,
  cancellation_operation_id text,
  charge_operation_id text,
  next_period_obligation_id text,
  next_period_start timestamptz,
  next_period_end timestamptz,
  next_period_charge_usd_micros bigint,
  schedule_version bigint NOT NULL DEFAULT 0,
  observation_result text NOT NULL,
  accepted_calculation jsonb NOT NULL,
  actual_outcome jsonb,
  error_code text,
  cancelled_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  execution_plan_id text NOT NULL,
  execution_plan_digest text NOT NULL,
  quote_at_ms bigint NOT NULL,
  period_start_ms bigint NOT NULL,
  period_end_ms bigint NOT NULL,
  source_financial_snapshot_digest text NOT NULL,
  source_financial_snapshot_bytes bytea NOT NULL,
  PRIMARY KEY (id),
  FOREIGN KEY (workspace_id, tenant_id) REFERENCES workspace.workspaces (id, tenant_id) ON DELETE RESTRICT,
  FOREIGN KEY (source_subscription_id, workspace_id) REFERENCES workspace.subscriptions (id, workspace_id) ON DELETE RESTRICT,
  FOREIGN KEY (source_period_id, source_subscription_id) REFERENCES workspace.subscription_periods (id, subscription_id) ON DELETE RESTRICT,
  FOREIGN KEY (operation_id) REFERENCES workspace.operations (id) ON DELETE RESTRICT,
  FOREIGN KEY (execution_operation_id) REFERENCES workspace.operations (id) ON DELETE RESTRICT,
  FOREIGN KEY (cancellation_operation_id) REFERENCES workspace.operations (id) ON DELETE RESTRICT,
  CHECK (kind IN ('upgrade_immediate','downgrade_next_period')),
  CHECK (status IN ('requested','scheduled','awaiting_payment','applying','applied','failed','needs_attention','cancelled')),
  CHECK (policy_version IN ('workspace-plan-change-v1')),
  CHECK (observation_result IN ('confirmed','rejected','unknown')),
  CHECK (source_subscription_version >= 0 AND schedule_version >= 0),
  CHECK (source_monthly_usd_micros >= 0 AND target_monthly_usd_micros >= 0 AND charge_usd_micros >= 0),
  CHECK ((kind = 'upgrade_immediate' AND planned_effective_at = quote_at AND num_nonnulls(next_period_start,next_period_end,next_period_charge_usd_micros) = 0) OR (kind = 'downgrade_next_period' AND planned_effective_at = period_end AND next_period_start = period_end AND next_period_end > next_period_start AND next_period_charge_usd_micros = target_monthly_usd_micros AND charge_usd_micros = 0)),
  CHECK ((status = 'applied') = (applied_at IS NOT NULL)),
  CHECK ((status = 'cancelled') = (cancelled_at IS NOT NULL)),
  UNIQUE (quote_id),
  UNIQUE (id, workspace_id),
  CHECK (date_trunc('milliseconds',quote_at) = quote_at),
  CHECK (execution_plan_digest ~ '^sha256:[0-9a-f]{64}$'),
  CHECK (period_end > period_start),
  CHECK (period_end_ms > period_start_ms AND period_end_ms-period_start_ms <= 2678400000),
  CHECK (quote_at_ms >= period_start_ms AND quote_at_ms < period_end_ms),
  CHECK (source_financial_snapshot_digest = 'sha256:' || encode(sha256(source_financial_snapshot_bytes),'hex')),
  CHECK (kind <> 'upgrade_immediate' OR charge_usd_micros = ceil(greatest(target_monthly_usd_micros-source_monthly_usd_micros,0)::numeric * (period_end_ms-quote_at_ms)::numeric / (period_end_ms-period_start_ms)::numeric))
);
CREATE UNIQUE INDEX plan_changes_one_unfinished ON workspace.plan_changes (workspace_id) WHERE status IN ('requested','scheduled','awaiting_payment','applying','needs_attention');
CREATE INDEX plan_changes_schedule ON workspace.plan_changes (planned_effective_at, id) WHERE status IN ('scheduled','awaiting_payment');
CREATE INDEX plan_changes_workspace ON workspace.plan_changes (workspace_id, created_at DESC, id DESC);

-- One original next-period obligation before payment confirmation, shared by manual/automatic/boundary actors; target accepted price is fixed; not a wallet or funds-reservation service
CREATE TABLE workspace.subscription_period_obligations (
  id text NOT NULL,
  subscription_id text NOT NULL,
  workspace_id text NOT NULL,
  plan_change_id text,
  period_start timestamptz NOT NULL,
  period_end timestamptz NOT NULL,
  target_compute_plan_id text NOT NULL,
  target_storage_plan_id text NOT NULL,
  target_price_policy_version_id text NOT NULL,
  amount_usd_micros bigint NOT NULL,
  accepted_pricing_snapshot jsonb NOT NULL,
  status text NOT NULL,
  operation_id text,
  wallet_operation_id text,
  billing_key text NOT NULL,
  payment_accepted_at timestamptz,
  resource_execution_started_at timestamptz,
  confirmed_at timestamptz,
  error_code text,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  version bigint NOT NULL DEFAULT 0,
  quote_id text NOT NULL,
  confirmed_subscription_version bigint,
  PRIMARY KEY (id),
  FOREIGN KEY (subscription_id, workspace_id) REFERENCES workspace.subscriptions (id, workspace_id) ON DELETE RESTRICT,
  FOREIGN KEY (plan_change_id, workspace_id) REFERENCES workspace.plan_changes (id, workspace_id) ON DELETE RESTRICT,
  FOREIGN KEY (operation_id) REFERENCES workspace.operations (id) ON DELETE RESTRICT,
  UNIQUE (subscription_id, period_start),
  UNIQUE (billing_key),
  CHECK (period_end > period_start),
  CHECK (amount_usd_micros >= 0),
  CHECK (status IN ('awaiting_payment','accepted','confirmed','failed','needs_attention')),
  CHECK (status <> 'confirmed' OR confirmed_at IS NOT NULL),
  CHECK (wallet_operation_id IS NULL OR payment_accepted_at IS NOT NULL),
  CHECK (version >= 0),
  CHECK (confirmed_subscription_version IS NULL OR (confirmed_subscription_version >= 0 AND status = 'confirmed'))
);
CREATE INDEX period_obligations_workspace ON workspace.subscription_period_obligations (workspace_id, period_start);
CREATE INDEX period_obligations_pending ON workspace.subscription_period_obligations (period_start, id) WHERE status IN ('awaiting_payment','accepted','needs_attention');

-- Immutable successful-upgrade supplement coverage T..E; original Gateway charge identity retained; later deletion refunds this coverage, not base-order 720-hour policy
CREATE TABLE workspace.supplemental_charges (
  id text NOT NULL,
  plan_change_id text NOT NULL,
  subscription_period_id text NOT NULL,
  workspace_id text NOT NULL,
  quote_id text NOT NULL,
  policy_version text NOT NULL,
  coverage_start timestamptz NOT NULL,
  coverage_end timestamptz NOT NULL,
  confirmed_amount_usd_micros bigint NOT NULL,
  original_wallet_operation_id text,
  charge_receipt_id text NOT NULL,
  confirmed_at timestamptz NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  coverage_start_ms bigint NOT NULL,
  coverage_end_ms bigint NOT NULL,
  source_financial_snapshot_digest text NOT NULL,
  PRIMARY KEY (id),
  FOREIGN KEY (plan_change_id, workspace_id) REFERENCES workspace.plan_changes (id, workspace_id) ON DELETE RESTRICT,
  FOREIGN KEY (subscription_period_id) REFERENCES workspace.subscription_periods (id) ON DELETE RESTRICT,
  CHECK (policy_version IN ('workspace-plan-change-v1')),
  UNIQUE (plan_change_id),
  UNIQUE (original_wallet_operation_id),
  CHECK (coverage_end > coverage_start),
  CHECK (confirmed_amount_usd_micros >= 0),
  CHECK ((confirmed_amount_usd_micros = 0) = (original_wallet_operation_id IS NULL)),
  CHECK (coverage_end_ms > coverage_start_ms),
  CHECK (source_financial_snapshot_digest ~ '^sha256:[0-9a-f]{64}$')
);
CREATE INDEX supplements_workspace ON workspace.supplemental_charges (workspace_id, created_at DESC, id DESC);
CREATE INDEX supplements_period ON workspace.supplemental_charges (subscription_period_id);
ALTER TABLE workspace.workspaces ADD CONSTRAINT workspaces_active_operation_fk FOREIGN KEY (active_operation_id) REFERENCES workspace.operations (id) ON DELETE RESTRICT;
ALTER TABLE workspace.model_configurations ADD CONSTRAINT model_configurations_operation_fk FOREIGN KEY (operation_id) REFERENCES workspace.operations (id) ON DELETE RESTRICT;
ALTER TABLE workspace.saga_steps ADD CONSTRAINT saga_steps_operation_fk FOREIGN KEY (operation_id) REFERENCES workspace.operations (id) ON DELETE RESTRICT;
ALTER TABLE workspace.subscriptions ADD CONSTRAINT subscriptions_change_operation_fk FOREIGN KEY (active_change_operation_id) REFERENCES workspace.operations (id) ON DELETE RESTRICT;
ALTER TABLE workspace.plan_changes ADD CONSTRAINT plan_change_next_period_obligation_fk FOREIGN KEY (next_period_obligation_id) REFERENCES workspace.subscription_period_obligations (id) ON DELETE RESTRICT;
GRANT USAGE ON SCHEMA workspace TO opl_workspace_writer;
GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA workspace TO opl_workspace_writer;
REVOKE UPDATE, DELETE ON workspace.outbox_events FROM opl_workspace_writer;
REVOKE UPDATE, DELETE ON workspace.subscription_periods FROM opl_workspace_writer;
REVOKE UPDATE, DELETE ON workspace.supplemental_charges FROM opl_workspace_writer;
REVOKE UPDATE, DELETE ON workspace.plan_changes FROM opl_workspace_writer;
GRANT UPDATE (status, applied_at, execution_operation_id, cancellation_operation_id, charge_operation_id, next_period_obligation_id, schedule_version, observation_result, actual_outcome, error_code, cancelled_at, updated_at) ON workspace.plan_changes TO opl_workspace_writer;
COMMIT;
-- END DATABASE opl_workspace

-- BEGIN DATABASE opl_runtime_control
BEGIN;
DO $$ BEGIN
 IF current_database() <> 'opl_runtime_control' THEN RAISE EXCEPTION 'Wrong database: expected opl_runtime_control, got %', current_database(); END IF;
END $$;
SET LOCAL TIME ZONE 'UTC';
CREATE SCHEMA runtime_control AUTHORIZATION opl_runtime_control_owner;
REVOKE ALL ON SCHEMA runtime_control FROM PUBLIC;
REVOKE CREATE ON SCHEMA public FROM PUBLIC;
REVOKE ALL ON DATABASE opl_runtime_control FROM PUBLIC;
GRANT CONNECT ON DATABASE opl_runtime_control TO opl_runtime_control_writer;
SET LOCAL ROLE opl_runtime_control_owner;



-- 本Owner状态事务同写immutable事件；revision只在本Owner聚合锁内分配
-- Strict PublisherContract schema; repository/digest must match contract and admitted namespace; descriptor is propagated unchanged into Build and execution
CREATE TABLE runtime_control.runtime_releases (
  id text NOT NULL,
  name text NOT NULL,
  version_label text NOT NULL,
  artifact_repository text NOT NULL,
  artifact_digest text NOT NULL,
  status text NOT NULL DEFAULT 'approved',
  approved_by text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  runtime_abi_version text NOT NULL,
  package_format_versions text[] NOT NULL,
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
  CHECK ((publisher_contract->>'kind' = 'runtime') IS TRUE),
  CHECK ((publisher_contract->>'publisherNamespaceId' = publisher_namespace_id) IS TRUE),
  CHECK ((publisher_contract #>> '{image,repository}' = artifact_repository) IS TRUE),
  CHECK ((publisher_contract #>> '{image,digest}' = artifact_digest) IS TRUE),
  CHECK ((publisher_contract #>> '{image,platform,os}' = 'linux') IS TRUE),
  CHECK ((publisher_contract #>> '{image,platform,architecture}' IN ('amd64','arm64')) IS TRUE),
  CHECK ((publisher_contract #>> '{applicationRevisionTemplate,image}' = artifact_repository || '@' || artifact_digest) IS TRUE),
  CHECK ((publisher_contract #>> '{applicationRevisionTemplate,platform}' = (publisher_contract #>> '{image,platform,os}') || '/' || (publisher_contract #>> '{image,platform,architecture}') || CASE WHEN publisher_contract #>> '{image,platform,variant}' IS NULL THEN '' ELSE '/' || (publisher_contract #>> '{image,platform,variant}') END) IS TRUE),
  CHECK ((publisher_contract->>'runtimeAbiVersion' = runtime_abi_version) IS TRUE),
  CHECK ((publisher_contract->'packageFormatVersions' = to_jsonb(package_format_versions)) IS TRUE)
);
CREATE INDEX runtime_releases_status ON runtime_control.runtime_releases (status, created_at DESC, id DESC);
CREATE INDEX runtime_releases_publisher ON runtime_control.runtime_releases (publisher_namespace_id);

-- 按当前生效不可变策略选择默认Runtime/WebUI，不多处写is_default
CREATE TABLE runtime_control.catalog_policies (
  id text NOT NULL,
  runtime_version_id text NOT NULL,
  default_webui_version_id text NOT NULL,
  policy_version text NOT NULL,
  published_by text NOT NULL,
  effective_at timestamptz NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (id),
  UNIQUE (policy_version)
);
CREATE INDEX catalog_policies_effective ON runtime_control.catalog_policies (effective_at DESC, id DESC);

CREATE TABLE runtime_control.outbox_events (
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
CREATE INDEX outbox_events_aggregate ON runtime_control.outbox_events (aggregate_type, aggregate_id, aggregate_revision);

-- 各consumer独立ACK，禁止单个ACK丢其他消费者事件；lease仅本库
CREATE TABLE runtime_control.outbox_deliveries (
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
  FOREIGN KEY (event_id) REFERENCES runtime_control.outbox_events (id) ON DELETE RESTRICT,
  UNIQUE (event_id, consumer_owner),
  CHECK (attempt_count >= 0),
  CHECK ((lease_token IS NULL) = (lease_until IS NULL))
);
CREATE INDEX outbox_deliveries_pending ON runtime_control.outbox_deliveries (next_attempt_at, id) WHERE acknowledged_at IS NULL;

-- 去重hash/业务状态/processed_at/后续Outbox同本库事务；乱序不能覆盖新版本
CREATE TABLE runtime_control.inbox_events (
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
CREATE INDEX inbox_events_pending ON runtime_control.inbox_events (received_at, id) WHERE processed_at IS NULL;
CREATE INDEX inbox_events_aggregate ON runtime_control.inbox_events (source_owner, aggregate_type, aggregate_id, aggregate_revision);

-- 命令创建同事务；operation_name是API操作名不是新Operation ID；安全响应无Key明文；义务存续无TTL清理
CREATE TABLE runtime_control.idempotency_records (
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
CREATE INDEX idempotency_records_resource ON runtime_control.idempotency_records (resource_id);

-- 目标Owner持异步Operation；BFF路由无中央writer；Build创建201回Job；只有Workspace配Saga
CREATE TABLE runtime_control.operations (
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
CREATE INDEX operations_resource ON runtime_control.operations (resource_id, created_at DESC, id DESC);
CREATE INDEX operations_tenant_list ON runtime_control.operations (tenant_id, created_at DESC, id DESC);
CREATE INDEX operations_recovery ON runtime_control.operations (status, updated_at);
GRANT USAGE ON SCHEMA runtime_control TO opl_runtime_control_writer;
GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA runtime_control TO opl_runtime_control_writer;
REVOKE UPDATE, DELETE ON runtime_control.outbox_events FROM opl_runtime_control_writer;
COMMIT;
-- END DATABASE opl_runtime_control

-- BEGIN DATABASE opl_serve
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
CREATE UNIQUE INDEX agent_deployments_one_active ON serve.agent_deployments (workspace_id) WHERE status = 'active';
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
-- Serve owns the delivery execution epoch; Fabric owns observed route generation; generation advances only on verified route readback
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
  selection_commit_receipt_id text,
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
  CHECK (selection_commit_receipt_id IS NULL OR status = 'confirmed'),
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

-- BEGIN DATABASE opl_gateway
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
-- END DATABASE opl_gateway

-- BEGIN DATABASE opl_resource_catalog
BEGIN;
DO $$ BEGIN
 IF current_database() <> 'opl_resource_catalog' THEN RAISE EXCEPTION 'Wrong database: expected opl_resource_catalog, got %', current_database(); END IF;
END $$;
SET LOCAL TIME ZONE 'UTC';
CREATE SCHEMA resource_catalog AUTHORIZATION opl_resource_catalog_owner;
REVOKE ALL ON SCHEMA resource_catalog FROM PUBLIC;
REVOKE CREATE ON SCHEMA public FROM PUBLIC;
REVOKE ALL ON DATABASE opl_resource_catalog FROM PUBLIC;
GRANT CONNECT ON DATABASE opl_resource_catalog TO opl_resource_catalog_writer;
SET LOCAL ROLE opl_resource_catalog_owner;

-- 不可变发布规格/Provider能力；不接受客户任选provider；报价不是模型定价
CREATE TABLE resource_catalog.compute_plans (
  id text NOT NULL,
  name text NOT NULL,
  version_label text NOT NULL,
  provider text NOT NULL,
  provider_profile_ref text NOT NULL,
  region text NOT NULL,
  status text NOT NULL DEFAULT 'approved',
  billing_mode text NOT NULL,
  valid_from timestamptz NOT NULL,
  valid_until timestamptz,
  published_by text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  provider_capability_version text NOT NULL,
  provider_specification jsonb NOT NULL,
  vcpus integer NOT NULL,
  memory_mib integer NOT NULL,
  PRIMARY KEY (id),
  CHECK (status IN ('approved','deprecated','revoked')),
  CHECK (billing_mode IN ('PREPAID_MONTHLY','LOCAL_NO_CHARGE')),
  UNIQUE (name, version_label, provider, region),
  CHECK (valid_until IS NULL OR valid_until > valid_from),
  CHECK (vcpus > 0 AND memory_mib > 0)
);
CREATE INDEX compute_plans_available ON resource_catalog.compute_plans (status, valid_from DESC, id DESC);

-- 不可变发布规格/Provider能力；不接受客户任选provider；报价不是模型定价
CREATE TABLE resource_catalog.storage_plans (
  id text NOT NULL,
  name text NOT NULL,
  version_label text NOT NULL,
  provider text NOT NULL,
  provider_profile_ref text NOT NULL,
  region text NOT NULL,
  status text NOT NULL DEFAULT 'approved',
  billing_mode text NOT NULL,
  valid_from timestamptz NOT NULL,
  valid_until timestamptz,
  published_by text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  provider_capability_version text NOT NULL,
  provider_specification jsonb NOT NULL,
  capacity_gib integer NOT NULL,
  shrink_supported boolean NOT NULL,
  PRIMARY KEY (id),
  CHECK (status IN ('approved','deprecated','revoked')),
  CHECK (billing_mode IN ('PREPAID_MONTHLY','LOCAL_NO_CHARGE')),
  UNIQUE (name, version_label, provider, region),
  CHECK (valid_until IS NULL OR valid_until > valid_from),
  CHECK (capacity_gib > 0)
);
CREATE INDEX storage_plans_available ON resource_catalog.storage_plans (status, valid_from DESC, id DESC);

-- 管理员实际批准价格、变更、续费规则；不设默认费率
CREATE TABLE resource_catalog.price_policy_versions (
  id text NOT NULL,
  version_label text NOT NULL,
  compute_plan_id text NOT NULL,
  storage_plan_id text NOT NULL,
  compute_monthly_usd_micros bigint NOT NULL,
  storage_monthly_usd_micros bigint NOT NULL,
  product_monthly_usd_micros bigint NOT NULL,
  currency text NOT NULL DEFAULT 'USD',
  renewal_rules jsonb NOT NULL,
  valid_from timestamptz NOT NULL,
  valid_until timestamptz,
  published_by text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  period_months integer NOT NULL,
  plan_change_policy_version text NOT NULL,
  PRIMARY KEY (id),
  FOREIGN KEY (compute_plan_id) REFERENCES resource_catalog.compute_plans (id) ON DELETE RESTRICT,
  FOREIGN KEY (storage_plan_id) REFERENCES resource_catalog.storage_plans (id) ON DELETE RESTRICT,
  UNIQUE (version_label, compute_plan_id, storage_plan_id),
  CHECK (currency = 'USD'),
  CHECK (compute_monthly_usd_micros >= 0 AND storage_monthly_usd_micros >= 0 AND product_monthly_usd_micros >= 0),
  CHECK (valid_until IS NULL OR valid_until > valid_from),
  CHECK (period_months > 0),
  UNIQUE (compute_plan_id, storage_plan_id, valid_from),
  CHECK ((renewal_rules->>'version' = 'renewal-policy/v1' AND renewal_rules->>'trigger' = 'manual_or_explicitly_consented_automatic' AND (renewal_rules->>'months')::integer = period_months AND renewal_rules->>'usesAcceptedPriceSnapshot' = 'true') IS TRUE),
  CHECK (plan_change_policy_version IN ('workspace-plan-change-v1')),
  CHECK (period_months = 1)
);
CREATE INDEX price_policy_versions_scope ON resource_catalog.price_policy_versions (compute_plan_id, storage_plan_id, valid_from DESC, id DESC);

-- 实际批准版本化规则，不发明退款比例或自动清除期限；历史无级联
CREATE TABLE resource_catalog.refund_policy_versions (
  id text NOT NULL,
  version_label text NOT NULL,
  rules jsonb NOT NULL,
  customer_terms text NOT NULL,
  valid_from timestamptz NOT NULL,
  valid_until timestamptz,
  published_by text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  algorithm text NOT NULL,
  retention_policy_version_id text NOT NULL,
  PRIMARY KEY (id),
  UNIQUE (version_label),
  CHECK (valid_until IS NULL OR valid_until > valid_from),
  CHECK (algorithm IN ('workspace-delete-refund-v1'))
);
CREATE INDEX refund_policy_versions_effective ON resource_catalog.refund_policy_versions (valid_from DESC, id DESC);

-- 实际批准版本化规则，不发明退款比例或自动清除期限；历史无级联
CREATE TABLE resource_catalog.retention_policy_versions (
  id text NOT NULL,
  version_label text NOT NULL,
  rules jsonb NOT NULL,
  customer_terms text NOT NULL,
  valid_from timestamptz NOT NULL,
  valid_until timestamptz,
  published_by text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  workspace_data_disposition text NOT NULL,
  package_history_disposition text NOT NULL,
  build_history_disposition text NOT NULL,
  tenant_restore_days integer NOT NULL,
  PRIMARY KEY (id),
  UNIQUE (version_label),
  CHECK (valid_until IS NULL OR valid_until > valid_from),
  CHECK (workspace_data_disposition IN ('destroy_after_confirmed_deletion')),
  CHECK (package_history_disposition IN ('retain')),
  CHECK (build_history_disposition IN ('retain')),
  CHECK (tenant_restore_days = 15)
);
CREATE INDEX retention_policy_versions_effective ON resource_catalog.retention_policy_versions (valid_from DESC, id DESC);

-- Catalog唯一writer，AcceptQuote本库锁row校验并绑定唯一Workspace Operation；Workspace只存已接受快照
CREATE TABLE resource_catalog.quotes (
  id text NOT NULL,
  tenant_id text NOT NULL,
  actor_id text NOT NULL,
  purpose text NOT NULL,
  workspace_id text,
  capability_version_id text,
  compute_plan_id text NOT NULL,
  storage_plan_id text NOT NULL,
  price_policy_version_id text NOT NULL,
  refund_policy_version_id text NOT NULL,
  retention_policy_version_id text NOT NULL,
  total_usd_micros bigint NOT NULL,
  period_start timestamptz NOT NULL,
  period_end timestamptz NOT NULL,
  status text NOT NULL DEFAULT 'offered',
  input_digest text NOT NULL,
  admission_snapshot jsonb NOT NULL,
  accepted_by_operation_id text,
  accepted_at timestamptz,
  expires_at timestamptz NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  model_selections jsonb NOT NULL,
  period_months integer NOT NULL,
  refund_terms text NOT NULL,
  retention_terms text NOT NULL,
  expected_interruption text NOT NULL,
  plan_change_calculation jsonb,
  source_subscription_version bigint,
  scheduled_plan_change_id text,
  PRIMARY KEY (id),
  FOREIGN KEY (compute_plan_id) REFERENCES resource_catalog.compute_plans (id) ON DELETE RESTRICT,
  FOREIGN KEY (storage_plan_id) REFERENCES resource_catalog.storage_plans (id) ON DELETE RESTRICT,
  FOREIGN KEY (price_policy_version_id) REFERENCES resource_catalog.price_policy_versions (id) ON DELETE RESTRICT,
  FOREIGN KEY (refund_policy_version_id) REFERENCES resource_catalog.refund_policy_versions (id) ON DELETE RESTRICT,
  FOREIGN KEY (retention_policy_version_id) REFERENCES resource_catalog.retention_policy_versions (id) ON DELETE RESTRICT,
  CHECK (purpose IN ('deploy','resize','renew')),
  CHECK (status IN ('offered','accepted','expired')),
  CHECK (total_usd_micros >= 0),
  CHECK (period_end > period_start),
  CHECK (input_digest ~ '^sha256:[0-9a-f]{64}$'),
  CHECK ((status = 'accepted') = (accepted_at IS NOT NULL AND accepted_by_operation_id IS NOT NULL)),
  CHECK (period_months > 0),
  CHECK ((purpose = 'resize') = (plan_change_calculation IS NOT NULL)),
  CHECK (purpose <> 'resize' OR source_subscription_version IS NOT NULL)
);
CREATE INDEX quotes_tenant_list ON resource_catalog.quotes (tenant_id, created_at DESC, id DESC);
CREATE UNIQUE INDEX quotes_accepting_operation ON resource_catalog.quotes (accepted_by_operation_id) WHERE accepted_by_operation_id IS NOT NULL;

-- All line amounts are nonnegative; total=sum(compute/storage/product)-sum(adjustment_credit); credit_source binds the exact paid period, transaction and policy
CREATE TABLE resource_catalog.quote_items (
  id text NOT NULL,
  quote_id text NOT NULL,
  kind text NOT NULL,
  description text NOT NULL,
  amount_usd_micros bigint NOT NULL,
  calculation jsonb NOT NULL,
  sort_order integer NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  quantity integer NOT NULL,
  credit_source jsonb,
  PRIMARY KEY (id),
  FOREIGN KEY (quote_id) REFERENCES resource_catalog.quotes (id) ON DELETE RESTRICT,
  UNIQUE (quote_id, sort_order),
  CHECK (sort_order >= 0),
  CHECK (quantity > 0),
  CHECK (kind IN ('compute','storage','product','adjustment_credit')),
  CHECK (amount_usd_micros >= 0),
  CHECK ((kind = 'adjustment_credit') = (credit_source IS NOT NULL)),
  CHECK (credit_source IS NULL OR ((jsonb_typeof(credit_source) = 'object' AND credit_source ?& ARRAY['originalWalletOperationId','originalSubscriptionPeriodId','policyVersionId','creditReceiptId','amountUSDMicros'] AND (credit_source->>'amountUSDMicros')::bigint = amount_usd_micros) IS TRUE))
);
CREATE INDEX quote_items_quote ON resource_catalog.quote_items (quote_id, sort_order);

-- 本Owner状态事务同写immutable事件；revision只在本Owner聚合锁内分配
CREATE TABLE resource_catalog.outbox_events (
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
CREATE INDEX outbox_events_aggregate ON resource_catalog.outbox_events (aggregate_type, aggregate_id, aggregate_revision);

-- 各consumer独立ACK，禁止单个ACK丢其他消费者事件；lease仅本库
CREATE TABLE resource_catalog.outbox_deliveries (
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
  FOREIGN KEY (event_id) REFERENCES resource_catalog.outbox_events (id) ON DELETE RESTRICT,
  UNIQUE (event_id, consumer_owner),
  CHECK (attempt_count >= 0),
  CHECK ((lease_token IS NULL) = (lease_until IS NULL))
);
CREATE INDEX outbox_deliveries_pending ON resource_catalog.outbox_deliveries (next_attempt_at, id) WHERE acknowledged_at IS NULL;

-- 去重hash/业务状态/processed_at/后续Outbox同本库事务；乱序不能覆盖新版本
CREATE TABLE resource_catalog.inbox_events (
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
CREATE INDEX inbox_events_pending ON resource_catalog.inbox_events (received_at, id) WHERE processed_at IS NULL;
CREATE INDEX inbox_events_aggregate ON resource_catalog.inbox_events (source_owner, aggregate_type, aggregate_id, aggregate_revision);

-- 命令创建同事务；operation_name是API操作名不是新Operation ID；安全响应无Key明文；义务存续无TTL清理
CREATE TABLE resource_catalog.idempotency_records (
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
CREATE INDEX idempotency_records_resource ON resource_catalog.idempotency_records (resource_id);

-- 目标Owner持异步Operation；BFF路由无中央writer；Build创建201回Job；只有Workspace配Saga
CREATE TABLE resource_catalog.operations (
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
CREATE INDEX operations_resource ON resource_catalog.operations (resource_id, created_at DESC, id DESC);
CREATE INDEX operations_tenant_list ON resource_catalog.operations (tenant_id, created_at DESC, id DESC);
CREATE INDEX operations_recovery ON resource_catalog.operations (status, updated_at);
ALTER TABLE resource_catalog.refund_policy_versions ADD CONSTRAINT refund_retention_policy_fk FOREIGN KEY (retention_policy_version_id) REFERENCES resource_catalog.retention_policy_versions (id) ON DELETE RESTRICT;
GRANT USAGE ON SCHEMA resource_catalog TO opl_resource_catalog_writer;
GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA resource_catalog TO opl_resource_catalog_writer;
REVOKE UPDATE, DELETE ON resource_catalog.outbox_events FROM opl_resource_catalog_writer;
REVOKE UPDATE, DELETE ON resource_catalog.price_policy_versions FROM opl_resource_catalog_writer;
REVOKE UPDATE, DELETE ON resource_catalog.refund_policy_versions FROM opl_resource_catalog_writer;
REVOKE UPDATE, DELETE ON resource_catalog.retention_policy_versions FROM opl_resource_catalog_writer;
REVOKE UPDATE, DELETE ON resource_catalog.quote_items FROM opl_resource_catalog_writer;
COMMIT;
-- END DATABASE opl_resource_catalog

-- BEGIN DATABASE opl_ledger
BEGIN;
DO $$ BEGIN
 IF current_database() <> 'opl_ledger' THEN RAISE EXCEPTION 'Wrong database: expected opl_ledger, got %', current_database(); END IF;
END $$;
SET LOCAL TIME ZONE 'UTC';
CREATE SCHEMA ledger AUTHORIZATION opl_ledger_owner;
REVOKE ALL ON SCHEMA ledger FROM PUBLIC;
REVOKE CREATE ON SCHEMA public FROM PUBLIC;
REVOKE ALL ON DATABASE opl_ledger FROM PUBLIC;
GRANT CONNECT ON DATABASE opl_ledger TO opl_ledger_writer;
SET LOCAL ROLE opl_ledger_owner;

-- append-only证据/hash/provenance；修正append新receipt，不覆写原事实或第二钱包
CREATE TABLE ledger.receipts (
  id text NOT NULL,
  tenant_id text,
  source_owner text NOT NULL,
  source_event_id text NOT NULL,
  kind text NOT NULL,
  subject_type text NOT NULL,
  subject_id text NOT NULL,
  source_operation_id text NOT NULL,
  request_id text NOT NULL,
  evidence_sha256 text NOT NULL,
  evidence jsonb NOT NULL,
  previous_receipt_id text,
  source_occurred_at timestamptz NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (id),
  FOREIGN KEY (previous_receipt_id) REFERENCES ledger.receipts (id) ON DELETE RESTRICT,
  UNIQUE (source_owner, source_event_id),
  CHECK (evidence_sha256 ~ '^[0-9a-f]{64}$')
);
CREATE INDEX receipts_subject ON ledger.receipts (subject_type, subject_id, created_at DESC, id DESC);
CREATE INDEX receipts_tenant ON ledger.receipts (tenant_id, created_at DESC, id DESC);
CREATE INDEX receipts_operation ON ledger.receipts (source_owner, source_operation_id);

-- 对账结论只记录，不直接更改Workspace/钱包/provider
CREATE TABLE ledger.reconciliations (
  id text NOT NULL,
  subject_type text NOT NULL,
  subject_id text NOT NULL,
  source_owner text NOT NULL,
  owner_readback_ref text NOT NULL,
  receipt_id text NOT NULL,
  result text NOT NULL,
  safe_difference jsonb NOT NULL,
  observed_at timestamptz NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (id),
  FOREIGN KEY (receipt_id) REFERENCES ledger.receipts (id) ON DELETE RESTRICT,
  CHECK (result IN ('matched','mismatch','unknown'))
);
CREATE INDEX reconciliations_subject ON ledger.reconciliations (subject_type, subject_id, created_at DESC, id DESC);

-- 本Owner状态事务同写immutable事件；revision只在本Owner聚合锁内分配
CREATE TABLE ledger.outbox_events (
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
CREATE INDEX outbox_events_aggregate ON ledger.outbox_events (aggregate_type, aggregate_id, aggregate_revision);

-- 各consumer独立ACK，禁止单个ACK丢其他消费者事件；lease仅本库
CREATE TABLE ledger.outbox_deliveries (
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
  FOREIGN KEY (event_id) REFERENCES ledger.outbox_events (id) ON DELETE RESTRICT,
  UNIQUE (event_id, consumer_owner),
  CHECK (attempt_count >= 0),
  CHECK ((lease_token IS NULL) = (lease_until IS NULL))
);
CREATE INDEX outbox_deliveries_pending ON ledger.outbox_deliveries (next_attempt_at, id) WHERE acknowledged_at IS NULL;

-- 去重hash/业务状态/processed_at/后续Outbox同本库事务；乱序不能覆盖新版本
CREATE TABLE ledger.inbox_events (
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
CREATE INDEX inbox_events_pending ON ledger.inbox_events (received_at, id) WHERE processed_at IS NULL;
CREATE INDEX inbox_events_aggregate ON ledger.inbox_events (source_owner, aggregate_type, aggregate_id, aggregate_revision);

-- 命令创建同事务；operation_name是API操作名不是新Operation ID；安全响应无Key明文；义务存续无TTL清理
CREATE TABLE ledger.idempotency_records (
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
CREATE INDEX idempotency_records_resource ON ledger.idempotency_records (resource_id);
GRANT USAGE ON SCHEMA ledger TO opl_ledger_writer;
GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA ledger TO opl_ledger_writer;
REVOKE UPDATE, DELETE ON ledger.outbox_events FROM opl_ledger_writer;
REVOKE UPDATE, DELETE ON ledger.receipts FROM opl_ledger_writer;
REVOKE UPDATE, DELETE ON ledger.reconciliations FROM opl_ledger_writer;
COMMIT;
-- END DATABASE opl_ledger
