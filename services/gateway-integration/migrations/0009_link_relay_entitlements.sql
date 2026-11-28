BEGIN;
DO $$ BEGIN
 IF current_database() <> 'opl_tenant' THEN RAISE EXCEPTION 'Wrong database: expected opl_tenant, got %', current_database(); END IF;
END $$;
SET LOCAL TIME ZONE 'UTC';
SET LOCAL ROLE opl_tenant_owner;

-- Explicit per-subject admission allowlist for OPL Link Relay control-plane routes.
-- Missing rows fail closed to platform administrators unless backfilled for active Gateway users.
CREATE TABLE tenant.link_relay_entitlements (
  actor_id text NOT NULL,
  relay_enabled boolean NOT NULL DEFAULT false,
  reason text NOT NULL DEFAULT '',
  updated_by text NOT NULL,
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (actor_id),
  CHECK (actor_id <> '' AND actor_id = btrim(actor_id)),
  CHECK (updated_by <> '' AND updated_by = btrim(updated_by))
);
GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE tenant.link_relay_entitlements TO opl_tenant_writer;
COMMIT;
