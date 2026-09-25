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
