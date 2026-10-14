BEGIN;
DO $$ BEGIN
 IF current_database() <> 'opl_tenant' THEN RAISE EXCEPTION 'Wrong database: expected opl_tenant, got %', current_database(); END IF;
END $$;
SET LOCAL TIME ZONE 'UTC';
SET LOCAL ROLE opl_tenant_owner;

-- A session has to answer on whichever replica receives the request. The
-- display name is captured at login from the identity owner response, so the
-- session check can read it from this row instead of asking the Gateway owner
-- for a token that only the replica which performed the login still holds.
-- This stores no secret: it is the same email the owner already returned in the
-- login response, and the table still carries no token, key or password.
ALTER TABLE tenant.sessions ADD COLUMN display_name text;

-- Existing rows predate this column and have no captured name. Backfill from the
-- Gateway actor id, which is what the display fell back to before, and then make
-- the column required so a new session can never be written without one.
UPDATE tenant.sessions SET display_name = actor_id WHERE display_name IS NULL;
ALTER TABLE tenant.sessions ALTER COLUMN display_name SET NOT NULL;

GRANT SELECT, INSERT, UPDATE, DELETE ON tenant.sessions TO opl_tenant_writer;
COMMIT;
