BEGIN;
DO $$ BEGIN
 IF current_database() <> 'opl_tenant' THEN RAISE EXCEPTION 'Wrong database: expected opl_tenant, got %', current_database(); END IF;
END $$;
SET LOCAL TIME ZONE 'UTC';
SET LOCAL ROLE opl_tenant_owner;

-- Customer account fact owned by CloudIdentity. The Gateway identity binding
-- itself stays in the Gateway owner's gateway.identity_mappings; this row is the
-- Cloud account that binding belongs to. Absence of a row fails closed: the
-- owner knows no such customer account.
CREATE TABLE tenant.customer_accounts (
  id text NOT NULL,
  actor_id text NOT NULL,
  email text NOT NULL,
  display_name text NOT NULL DEFAULT '',
  admission text NOT NULL,
  status text NOT NULL DEFAULT 'active',
  opened_at timestamptz NOT NULL DEFAULT now(),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (id),
  UNIQUE (actor_id),
  UNIQUE (email),
  CHECK (id <> '' AND id = btrim(id)),
  CHECK (actor_id <> '' AND actor_id = btrim(actor_id)),
  CHECK (email <> '' AND email = btrim(email) AND email = lower(email)),
  CHECK (display_name = btrim(display_name)),
  CHECK (status IN ('active','disabled')),
  CHECK (admission IN ('full_cloud_customer','gateway_only'))
);
CREATE INDEX customer_accounts_opened ON tenant.customer_accounts (opened_at DESC, id DESC);
GRANT SELECT, INSERT, UPDATE ON tenant.customer_accounts TO opl_tenant_writer;
COMMIT;
