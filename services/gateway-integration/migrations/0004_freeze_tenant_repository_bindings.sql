BEGIN;
DO $$ BEGIN
 IF current_database() <> 'opl_tenant' THEN RAISE EXCEPTION 'Wrong database: expected opl_tenant, got %', current_database(); END IF;
END $$;
SET LOCAL TIME ZONE 'UTC';
SET LOCAL ROLE opl_tenant_owner;

-- Historical destinations stay readable for the bounded User-owner handover.
-- Retired runtime writers, including older replicas, may no longer reserve,
-- replace or remove a destination alongside gateway.identity_mappings.
-- Take the table lock before revoking privileges. This drains an in-flight
-- legacy writer and blocks a new statement from crossing the handover while
-- the revoke commits.
LOCK TABLE tenant.tenant_repository_bindings IN ACCESS EXCLUSIVE MODE;
REVOKE INSERT, UPDATE, DELETE ON tenant.tenant_repository_bindings FROM opl_tenant_writer;
GRANT SELECT ON tenant.tenant_repository_bindings TO opl_tenant_writer;
COMMIT;
