BEGIN;
DO $$ BEGIN
 IF current_database() <> 'opl_tenant' THEN RAISE EXCEPTION 'Wrong database: expected opl_tenant, got %', current_database(); END IF;
END $$;
SET LOCAL TIME ZONE 'UTC';
SET LOCAL ROLE opl_tenant_owner;

-- Canonical Workspace new-purchase eligibility per Gateway subject (actor_id).
-- Absence of a row fails closed (not eligible) for non-admin subjects.
CREATE TABLE tenant.workspace_purchase_eligibility (
  actor_id text NOT NULL,
  workspace_purchase_enabled boolean NOT NULL DEFAULT false,
  reason text NOT NULL,
  updated_by text NOT NULL,
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (actor_id),
  CHECK (actor_id <> '' AND actor_id = btrim(actor_id)),
  CHECK (reason <> '' AND reason = btrim(reason)),
  CHECK (updated_by <> '' AND updated_by = btrim(updated_by))
);
GRANT SELECT, INSERT, UPDATE ON tenant.workspace_purchase_eligibility TO opl_tenant_writer;
COMMIT;
