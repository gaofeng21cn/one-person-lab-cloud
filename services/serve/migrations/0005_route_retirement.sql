BEGIN;
DO $$ BEGIN
 IF current_database() <> 'opl_serve' THEN RAISE EXCEPTION 'Wrong database: expected opl_serve, got %', current_database(); END IF;
END $$;
SET LOCAL TIME ZONE 'UTC';
SET LOCAL ROLE opl_serve_owner;

-- Retirement is a route revision that selects no execution target. Keep its
-- conditional generation, revision and evidence in the existing route journal.
ALTER TABLE serve.access_switches
  DROP CONSTRAINT access_switches_action_kind_check,
  DROP CONSTRAINT access_switches_check2,
  DROP CONSTRAINT access_switches_target_instance;
ALTER TABLE serve.access_switches
  ADD CONSTRAINT access_switches_action_kind_check CHECK (action_kind IN ('fence','activate','rollback','retire')),
  ADD CONSTRAINT access_switches_target_required CHECK (action_kind IN ('fence','retire') OR target_execution_resource_id IS NOT NULL),
  ADD CONSTRAINT access_switches_target_instance CHECK (action_kind IN ('fence','retire') OR (target_runtime_instance_id IS NOT NULL AND target_deployment_id IS NOT NULL)) NOT VALID;
COMMIT;
