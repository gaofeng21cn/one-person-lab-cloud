CREATE TABLE IF NOT EXISTS control_plane_accounts (
  id TEXT NOT NULL PRIMARY KEY,
  created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  owner_user_id TEXT NOT NULL UNIQUE CHECK (owner_user_id <> ''),
  sub2api_user_id BIGINT NOT NULL UNIQUE CHECK (sub2api_user_id > 0),
  name TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT 'active',
  workspace_purchase_enabled BOOLEAN NOT NULL DEFAULT FALSE
);

CREATE TABLE IF NOT EXISTS control_plane_admin_audit_events (
  id TEXT NOT NULL PRIMARY KEY,
  created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  actor_user_id TEXT NOT NULL DEFAULT '',
  actor_role TEXT NOT NULL DEFAULT '',
  actor_account_id TEXT NOT NULL DEFAULT '',
  target_account_id TEXT NOT NULL DEFAULT '',
  action TEXT NOT NULL DEFAULT '',
  resource_kind TEXT NOT NULL DEFAULT '',
  resource_id TEXT NOT NULL DEFAULT '',
  ip_address TEXT NOT NULL DEFAULT '',
  user_agent TEXT NOT NULL DEFAULT '',
  before_json TEXT NOT NULL DEFAULT '',
  after_json TEXT NOT NULL DEFAULT '',
  result TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS control_plane_announcements (
  id TEXT NOT NULL PRIMARY KEY,
  created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  title TEXT NOT NULL CHECK (title <> ''),
  body TEXT NOT NULL CHECK (body <> ''),
  status TEXT NOT NULL DEFAULT 'draft',
  starts_at TEXT NOT NULL DEFAULT '',
  ends_at TEXT NOT NULL DEFAULT '',
  published_at TEXT NOT NULL DEFAULT '',
  created_by_user_id TEXT NOT NULL CHECK (created_by_user_id <> ''),
  updated_by_user_id TEXT NOT NULL CHECK (updated_by_user_id <> '')
);

CREATE TABLE IF NOT EXISTS control_plane_announcement_reads (
  id TEXT NOT NULL PRIMARY KEY,
  created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  announcement_id TEXT NOT NULL CHECK (announcement_id <> ''),
  user_id TEXT NOT NULL CHECK (user_id <> ''),
  read_at TEXT NOT NULL CHECK (read_at <> '')
);

CREATE TABLE IF NOT EXISTS control_plane_application_data_materials (
  id TEXT NOT NULL PRIMARY KEY,
  created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  application_id TEXT NOT NULL CHECK (application_id <> ''),
  version TEXT NOT NULL CHECK (version <> ''),
  digest TEXT NOT NULL CHECK (digest <> ''),
  payload TEXT NOT NULL CHECK (payload <> ''),
  admitted_by_user_id TEXT NOT NULL CHECK (admitted_by_user_id <> '')
);
CREATE UNIQUE INDEX IF NOT EXISTS applicationdatamaterial_application_id_version
  ON control_plane_application_data_materials (application_id, version);

CREATE TABLE IF NOT EXISTS control_plane_application_revisions (
  id TEXT NOT NULL PRIMARY KEY,
  created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  application_id TEXT NOT NULL CHECK (application_id <> ''),
  version TEXT NOT NULL CHECK (version <> ''),
  digest TEXT NOT NULL CHECK (digest <> ''),
  payload TEXT NOT NULL CHECK (payload <> ''),
  admitted_by_user_id TEXT NOT NULL CHECK (admitted_by_user_id <> '')
);
CREATE UNIQUE INDEX IF NOT EXISTS applicationrevision_application_id_version
  ON control_plane_application_revisions (application_id, version);

CREATE TABLE IF NOT EXISTS control_plane_archived_admin_audit_events (
  id TEXT NOT NULL PRIMARY KEY,
  created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  actor_user_id TEXT NOT NULL DEFAULT '',
  actor_role TEXT NOT NULL DEFAULT '',
  actor_account_id TEXT NOT NULL DEFAULT '',
  target_account_id TEXT NOT NULL DEFAULT '',
  action TEXT NOT NULL DEFAULT '',
  resource_kind TEXT NOT NULL DEFAULT '',
  resource_id TEXT NOT NULL DEFAULT '',
  ip_address TEXT NOT NULL DEFAULT '',
  user_agent TEXT NOT NULL DEFAULT '',
  before_json TEXT NOT NULL DEFAULT '',
  after_json TEXT NOT NULL DEFAULT '',
  result TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS control_plane_auth_attempts (
  id TEXT NOT NULL PRIMARY KEY,
  created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  email TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT '',
  reason TEXT NOT NULL DEFAULT '',
  ip_address TEXT NOT NULL DEFAULT '',
  user_agent TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS control_plane_billing_reconciliation (
  id TEXT NOT NULL PRIMARY KEY,
  created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  status TEXT NOT NULL DEFAULT '',
  guard_status TEXT NOT NULL DEFAULT '',
  guard_reason TEXT NOT NULL DEFAULT '',
  message_author TEXT NOT NULL DEFAULT '',
  message_text TEXT NOT NULL DEFAULT '',
  message_created_at TEXT NOT NULL DEFAULT '',
  guard_block_new_workspaces BOOLEAN NOT NULL DEFAULT FALSE,
  reports BIGINT NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS control_plane_compute_allocations (
  id TEXT NOT NULL PRIMARY KEY,
  created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  account_id TEXT NOT NULL CHECK (account_id <> ''),
  owner_user_id TEXT NOT NULL DEFAULT '',
  workspace_id TEXT NOT NULL DEFAULT '',
  name TEXT NOT NULL DEFAULT '',
  package_id TEXT NOT NULL DEFAULT '',
  provider TEXT NOT NULL DEFAULT '',
  provider_resource_id TEXT NOT NULL DEFAULT '',
  provider_request_id TEXT NOT NULL DEFAULT '',
  operation_id TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT '',
  desired_status TEXT NOT NULL DEFAULT '',
  provider_status TEXT NOT NULL DEFAULT '',
  last_provider_sync_at TEXT NOT NULL DEFAULT '',
  last_provider_sync_error TEXT NOT NULL DEFAULT '',
  external_deleted_at TEXT NOT NULL DEFAULT '',
  billing_status TEXT NOT NULL DEFAULT '',
  pricing_version TEXT NOT NULL DEFAULT '',
  billing_operation_id TEXT NOT NULL DEFAULT '',
  billing_state_json TEXT NOT NULL DEFAULT '{}',
  evidence_id TEXT NOT NULL DEFAULT '',
  cvm_instance_id TEXT NOT NULL DEFAULT '',
  instance_id TEXT NOT NULL DEFAULT '',
  node_name TEXT NOT NULL DEFAULT '',
  machine_name TEXT NOT NULL DEFAULT '',
  cpu DOUBLE PRECISION NOT NULL DEFAULT 0,
  memory_gb DOUBLE PRECISION NOT NULL DEFAULT 0,
  disk_gb DOUBLE PRECISION NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS control_plane_production_e2e_records (
  id TEXT NOT NULL PRIMARY KEY,
  created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  account_id TEXT NOT NULL DEFAULT '',
  workspace_id TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT '',
  result TEXT NOT NULL DEFAULT '',
  reason TEXT NOT NULL DEFAULT '',
  url TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS control_plane_project_task_sync_heads (
  id TEXT NOT NULL PRIMARY KEY,
  created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  kind TEXT NOT NULL CHECK (kind <> ''),
  organization_id TEXT NOT NULL CHECK (organization_id <> ''),
  workspace_id TEXT NOT NULL CHECK (workspace_id <> ''),
  project_id TEXT NOT NULL DEFAULT '',
  local_alias_id TEXT NOT NULL DEFAULT '',
  version BIGINT NOT NULL DEFAULT 1,
  status TEXT NOT NULL DEFAULT 'active'
);

CREATE TABLE IF NOT EXISTS control_plane_runtime_operations (
  id TEXT NOT NULL PRIMARY KEY,
  created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  operation_id TEXT NOT NULL DEFAULT '',
  account_id TEXT NOT NULL DEFAULT '',
  workspace_id TEXT NOT NULL DEFAULT '',
  period_start TEXT NOT NULL DEFAULT '',
  resource_id TEXT NOT NULL DEFAULT '',
  resource_kind TEXT NOT NULL DEFAULT '',
  action TEXT NOT NULL DEFAULT '',
  provider TEXT NOT NULL DEFAULT '',
  provider_request_id TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT '',
  result TEXT NOT NULL DEFAULT '',
  compute_allocation_id TEXT NOT NULL DEFAULT '',
  storage_id TEXT NOT NULL DEFAULT '',
  attachment_id TEXT NOT NULL DEFAULT '',
  runtime_service_name TEXT NOT NULL DEFAULT '',
  cvm_instance_id TEXT NOT NULL DEFAULT '',
  instance_id TEXT NOT NULL DEFAULT '',
  node_name TEXT NOT NULL DEFAULT '',
  machine_name TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS runtimeoperation_action_status_created_at_id
  ON control_plane_runtime_operations (action, status, created_at, id);
CREATE INDEX IF NOT EXISTS runtimeoperation_account_id_action_status_created_at_id
  ON control_plane_runtime_operations (account_id, action, status, created_at, id);
CREATE INDEX IF NOT EXISTS runtimeoperation_workspace_id_action_status_period_start_created_at_id
  ON control_plane_runtime_operations (workspace_id, action, status, period_start, created_at, id);

CREATE TABLE IF NOT EXISTS control_plane_sessions (
  id TEXT NOT NULL PRIMARY KEY,
  created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  user_id TEXT NOT NULL CHECK (user_id <> ''),
  csrf TEXT NOT NULL CHECK (csrf <> ''),
  expires_at TEXT NOT NULL CHECK (expires_at <> '')
);
CREATE INDEX IF NOT EXISTS session_user_id
  ON control_plane_sessions (user_id);

CREATE TABLE IF NOT EXISTS control_plane_storage_attachments (
  id TEXT NOT NULL PRIMARY KEY,
  created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  account_id TEXT NOT NULL CHECK (account_id <> ''),
  workspace_id TEXT NOT NULL DEFAULT '',
  compute_allocation_id TEXT NOT NULL DEFAULT '',
  storage_id TEXT NOT NULL DEFAULT '',
  volume_id TEXT NOT NULL DEFAULT '',
  operation_id TEXT NOT NULL DEFAULT '',
  provider TEXT NOT NULL DEFAULT '',
  provider_request_id TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT '',
  mount_path TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS control_plane_storage_volumes (
  id TEXT NOT NULL PRIMARY KEY,
  created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  account_id TEXT NOT NULL CHECK (account_id <> ''),
  owner_user_id TEXT NOT NULL DEFAULT '',
  workspace_id TEXT NOT NULL DEFAULT '',
  name TEXT NOT NULL DEFAULT '',
  package_id TEXT NOT NULL DEFAULT '',
  provider TEXT NOT NULL DEFAULT '',
  provider_resource_id TEXT NOT NULL DEFAULT '',
  provider_request_id TEXT NOT NULL DEFAULT '',
  operation_id TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT '',
  desired_status TEXT NOT NULL DEFAULT '',
  provider_status TEXT NOT NULL DEFAULT '',
  last_provider_sync_at TEXT NOT NULL DEFAULT '',
  last_provider_sync_error TEXT NOT NULL DEFAULT '',
  external_deleted_at TEXT NOT NULL DEFAULT '',
  billing_status TEXT NOT NULL DEFAULT '',
  pricing_version TEXT NOT NULL DEFAULT '',
  billing_operation_id TEXT NOT NULL DEFAULT '',
  billing_state_json TEXT NOT NULL DEFAULT '{}',
  mount_path TEXT NOT NULL DEFAULT '',
  size_gb DOUBLE PRECISION NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS control_plane_users (
  id TEXT NOT NULL PRIMARY KEY,
  created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  account_id TEXT NOT NULL UNIQUE CHECK (account_id <> ''),
  email TEXT NOT NULL UNIQUE CHECK (email <> ''),
  role TEXT NOT NULL DEFAULT 'owner',
  status TEXT NOT NULL DEFAULT 'active',
  password_hash TEXT NOT NULL DEFAULT '' CHECK (password_hash = ''),
  disabled_at TEXT NOT NULL DEFAULT '',
  disabled_by TEXT NOT NULL DEFAULT '',
  disabled_reason TEXT NOT NULL DEFAULT '',
  deleted_at TEXT NOT NULL DEFAULT '',
  deleted_by TEXT NOT NULL DEFAULT '',
  delete_reason TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS control_plane_workspaces (
  id TEXT NOT NULL PRIMARY KEY,
  created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  account_id TEXT NOT NULL DEFAULT '',
  owner_account_id TEXT NOT NULL DEFAULT '',
  owner_user_id TEXT NOT NULL DEFAULT '',
  user_id TEXT NOT NULL DEFAULT '',
  name TEXT NOT NULL DEFAULT '',
  url TEXT NOT NULL DEFAULT '',
  state TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT '',
  purchase_receipt_id TEXT NOT NULL DEFAULT '',
  billing_state_json TEXT NOT NULL DEFAULT '{}',
  storage_id TEXT NOT NULL DEFAULT '',
  current_compute_allocation_id TEXT NOT NULL DEFAULT '',
  current_attachment_id TEXT NOT NULL DEFAULT '',
  runtime_id TEXT NOT NULL DEFAULT '',
  runtime_service_name TEXT NOT NULL DEFAULT '',
  runtime_service_name_root TEXT NOT NULL DEFAULT '',
  service_name TEXT NOT NULL DEFAULT '',
  workspace_api_key_id BIGINT NULL CHECK (workspace_api_key_id IS NULL OR workspace_api_key_id > 0),
  access_token_status TEXT NOT NULL DEFAULT '',
  access_account TEXT NOT NULL DEFAULT '',
  access_username TEXT NOT NULL DEFAULT '',
  credential_status TEXT NOT NULL DEFAULT '',
  credential_version TEXT NOT NULL DEFAULT '',
  credential_secret_ref TEXT NOT NULL DEFAULT '',
  access_requires_login BOOLEAN NOT NULL DEFAULT FALSE,
  verification_slot_id TEXT NOT NULL DEFAULT '',
  customer_product BOOLEAN NOT NULL DEFAULT TRUE,
  application_binding TEXT NOT NULL DEFAULT '',
  application_binding_version BIGINT NOT NULL DEFAULT 0,
  current_application_deployment_id TEXT NOT NULL DEFAULT '',
  reserved_application_deployment_id TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS workspace_customer_product_id
  ON control_plane_workspaces (customer_product, id);

CREATE TABLE IF NOT EXISTS control_plane_workspace_sync_events (
  id TEXT NOT NULL PRIMARY KEY,
  created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  operation_id TEXT NOT NULL CHECK (operation_id <> ''),
  workspace_id TEXT NOT NULL CHECK (workspace_id <> ''),
  cursor BIGINT NOT NULL,
  entity_kind TEXT NOT NULL CHECK (entity_kind <> ''),
  project_id TEXT NOT NULL CHECK (project_id <> ''),
  task_id TEXT NOT NULL DEFAULT '',
  client_id TEXT NOT NULL CHECK (client_id <> ''),
  actor_user_id TEXT NOT NULL CHECK (actor_user_id <> ''),
  base_version BIGINT NOT NULL,
  server_version BIGINT NOT NULL,
  operation TEXT NOT NULL CHECK (operation <> ''),
  status TEXT NOT NULL CHECK (status <> ''),
  payload_json TEXT NOT NULL DEFAULT '{}',
  content_digest TEXT NOT NULL DEFAULT '',
  idempotency_key TEXT NOT NULL UNIQUE CHECK (idempotency_key <> ''),
  request_hash TEXT NOT NULL CHECK (request_hash <> ''),
  conflict_id TEXT NOT NULL DEFAULT '',
  occurred_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE UNIQUE INDEX IF NOT EXISTS workspacesyncevent_workspace_id_cursor
  ON control_plane_workspace_sync_events (workspace_id, cursor);
CREATE UNIQUE INDEX IF NOT EXISTS workspacesyncevent_workspace_id_operation_id
  ON control_plane_workspace_sync_events (workspace_id, operation_id);
CREATE INDEX IF NOT EXISTS workspacesyncevent_workspace_id_entity_kind_project_id_task_id_cursor
  ON control_plane_workspace_sync_events (workspace_id, entity_kind, project_id, task_id, cursor);
CREATE INDEX IF NOT EXISTS workspacesyncevent_workspace_id_conflict_id
  ON control_plane_workspace_sync_events (workspace_id, conflict_id);
