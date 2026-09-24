BEGIN;
SET LOCAL ROLE opl_build_owner;
ALTER TABLE build.build_jobs ADD COLUMN call_context jsonb NOT NULL DEFAULT '{}'::jsonb;
ALTER TABLE build.build_artifacts ADD COLUMN descriptor_bytes bytea;
COMMIT;
