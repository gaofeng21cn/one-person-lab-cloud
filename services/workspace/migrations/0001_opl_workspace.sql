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

-- 当前已接受业务选择；迁移裸资源capabilityVersionId可空，UI不得称已部署；expiresAt从订阅投影
CREATE TABLE workspace.workspaces (
  id text NOT NULL,
  tenant_id text NOT NULL,
  name text NOT NULL,
  status text NOT NULL DEFAULT 'provisioning',
  compute_plan_id text NOT NULL,
  storage_plan_id text NOT NULL,
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
