BEGIN;
DO $$ BEGIN
 IF current_database() <> 'opl_workspace' THEN RAISE EXCEPTION 'Wrong database: expected opl_workspace, got %', current_database(); END IF;
END $$;
SET LOCAL TIME ZONE 'UTC';
SET LOCAL ROLE opl_workspace_owner;

-- The model configuration the Workspace's runtime actually applied. Runtime
-- confirms a real reload first; the Workspace advances this value in the same
-- transaction that records that confirmation, so a requested version is never
-- shown as the applied one and the column is never a copy of Serve's runtime
-- state. The launch configuration is version zero: no change was requested yet.
ALTER TABLE workspace.workspaces
  ADD COLUMN model_configuration_version bigint NOT NULL DEFAULT 0;

ALTER TABLE workspace.workspaces
  ADD CONSTRAINT workspaces_model_configuration_version_nonnegative CHECK (model_configuration_version >= 0);

COMMIT;
