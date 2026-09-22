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
