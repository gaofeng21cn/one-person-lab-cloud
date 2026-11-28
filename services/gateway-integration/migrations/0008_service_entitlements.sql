BEGIN;
DO $$ BEGIN
 IF current_database() <> 'opl_tenant' THEN RAISE EXCEPTION 'Wrong database: expected opl_tenant, got %', current_database(); END IF;
END $$;
SET LOCAL TIME ZONE 'UTC';
SET LOCAL ROLE opl_tenant_owner;

-- Canonical per-user companion service entitlements (such as netfleet_cloud and link_relay).
-- Evaluated alongside the instance Service Catalog admission mode by CloudIdentity.
CREATE TABLE tenant.service_entitlements (
  actor_id text NOT NULL,
  service_id text NOT NULL,
  enabled boolean NOT NULL,
  role text NOT NULL DEFAULT 'user',
  origin_override text,
  limits_json jsonb NOT NULL DEFAULT '{}'::jsonb,
  expires_at timestamptz,
  reason text NOT NULL,
  updated_by text NOT NULL,
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (actor_id, service_id),
  CHECK (actor_id <> '' AND actor_id = btrim(actor_id)),
  CHECK (service_id ~ '^[a-z][a-z0-9_]{1,63}$'),
  CHECK (role <> '' AND role = btrim(role)),
  CHECK (origin_override IS NULL OR (origin_override <> '' AND origin_override = btrim(origin_override))),
  CHECK (jsonb_typeof(limits_json) = 'object'),
  CHECK (reason <> '' AND reason = btrim(reason)),
  CHECK (updated_by <> '' AND updated_by = btrim(updated_by))
);
GRANT SELECT, INSERT, UPDATE ON tenant.service_entitlements TO opl_tenant_writer;
COMMIT;
