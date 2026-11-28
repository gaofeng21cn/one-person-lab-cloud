BEGIN;
DO $$ BEGIN
 IF current_database() <> 'opl_tenant' THEN RAISE EXCEPTION 'Wrong database: expected opl_tenant, got %', current_database(); END IF;
END $$;
SET LOCAL TIME ZONE 'UTC';
SET LOCAL ROLE opl_tenant_owner;

-- Explicit per-subject restricted Software Center package whitelist.
-- Missing or expired rows fail closed regardless of package catalog visibility.
CREATE TABLE tenant.package_entitlements (
  actor_id text NOT NULL,
  package_id text NOT NULL,
  enabled boolean NOT NULL DEFAULT false,
  reason text NOT NULL DEFAULT '',
  expires_at timestamptz NULL,
  updated_by text NOT NULL,
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (actor_id, package_id),
  CHECK (actor_id <> '' AND actor_id = btrim(actor_id)),
  CHECK (package_id ~ '^[a-z0-9][a-z0-9._-]{0,63}$'),
  CHECK (updated_by <> '' AND updated_by = btrim(updated_by))
);
CREATE INDEX idx_package_entitlements_active_pkg ON tenant.package_entitlements (package_id, actor_id) WHERE enabled = true;
GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE tenant.package_entitlements TO opl_tenant_writer;
COMMIT;
