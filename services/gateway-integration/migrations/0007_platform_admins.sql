BEGIN;
DO $$ BEGIN
 IF current_database() <> 'opl_tenant' THEN RAISE EXCEPTION 'Wrong database: expected opl_tenant, got %', current_database(); END IF;
END $$;
SET LOCAL TIME ZONE 'UTC';
SET LOCAL ROLE opl_tenant_owner;

-- Delegated platform administrators per Gateway subject (actor_id).
-- Deployment-configured OPL_PLATFORM_ADMIN_SUBJECTS remain immutable bootstrap roots.
CREATE TABLE tenant.platform_admins (
  actor_id text NOT NULL,
  enabled boolean NOT NULL DEFAULT false,
  reason text NOT NULL,
  updated_by text NOT NULL,
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (actor_id),
  CHECK (actor_id <> '' AND actor_id = btrim(actor_id)),
  CHECK (reason <> '' AND reason = btrim(reason)),
  CHECK (updated_by <> '' AND updated_by = btrim(updated_by))
);
GRANT SELECT, INSERT, UPDATE ON tenant.platform_admins TO opl_tenant_writer;
COMMIT;
