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
-- Gateway actor id, which is what the display fell back to before.
UPDATE tenant.sessions SET display_name = actor_id WHERE display_name IS NULL;
-- The column stays nullable on purpose. A replica still running the previous image
-- writes sessions with the old INSERT, which omits this column, and an image-only
-- rollback runs that same INSERT; a NOT NULL column without a usable default would
-- refuse both with SQLSTATE 23502 and strand every remaining old writer. The
-- session read resolves a missing name from the actor id, so an old writer's row
-- displays exactly what it displayed before this column existed.
--
-- Boundary: making the column required is a later migration (0004), and it may only
-- be applied once no replica can still write the old INSERT: after every replica
-- runs this image and after the image-rollback window is closed. Rolling the image
-- back while 0004 is applied would bring the failing old writer straight back, so
-- the order is schema-expand now, image rollout now, contract later. Nothing here
-- has been adopted on any Instance; that adoption is what decides when 0004 may run.

GRANT SELECT, INSERT, UPDATE, DELETE ON tenant.sessions TO opl_tenant_writer;
COMMIT;
