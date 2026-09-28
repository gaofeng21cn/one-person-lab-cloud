BEGIN;
DO $$ BEGIN
 IF current_database() <> 'opl_tenant' THEN RAISE EXCEPTION 'Wrong database: expected opl_tenant, got %', current_database(); END IF;
END $$;
SET LOCAL TIME ZONE 'UTC';
SET LOCAL ROLE opl_tenant_owner;

-- One stable application-OCI destination per admitted Tenant. The Tenant id is
-- the authorization identity; the email local-part is only the initial slug
-- candidate. Registry host/namespace are installation facts recorded with the
-- binding so Build never accepts a destination from a request.
CREATE TABLE tenant.tenant_repository_bindings (
  tenant_id text NOT NULL,
  registry_host text NOT NULL,
  registry_namespace text NOT NULL,
  repository text NOT NULL,
  status text NOT NULL DEFAULT 'reserved',
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id),
  FOREIGN KEY (tenant_id) REFERENCES tenant.tenants (id) ON DELETE RESTRICT,
  CHECK (status IN ('reserved','active','retired')),
  CHECK (registry_host <> '' AND registry_host = lower(registry_host) AND position('://' in registry_host) = 0 AND right(registry_host,1) <> '/'),
  CHECK (registry_namespace <> '' AND registry_namespace = lower(registry_namespace)),
  CHECK (repository <> '' AND repository = lower(repository)),
  CHECK (repository !~ '[/@?#\\ ]' AND position('..' in repository) = 0)
);
CREATE UNIQUE INDEX tenant_repository_bindings_destination ON tenant.tenant_repository_bindings (registry_host, registry_namespace, repository);

-- The runtime role logs in as opl_tenant_runtime and inherits only this writer
-- privilege container. Migration 0001 granted privileges only on the tables
-- that existed then, so this owner-local table needs its own grant.
GRANT SELECT, INSERT, UPDATE, DELETE ON tenant.tenant_repository_bindings TO opl_tenant_writer;
COMMIT;
