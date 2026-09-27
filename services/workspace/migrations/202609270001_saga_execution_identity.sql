BEGIN;
DO $$ BEGIN
 IF current_database() <> 'opl_workspace' THEN RAISE EXCEPTION 'Wrong database: expected opl_workspace, got %', current_database(); END IF;
END $$;
SET LOCAL TIME ZONE 'UTC';
SET LOCAL ROLE opl_workspace_owner;

-- Every replayable owner call keeps the exact normalized input digest and the
-- execution epoch used by the external owner. A zero epoch means the target
-- owner allocates it in its own reservation response; Workspace never guesses
-- a later epoch while replaying the original identity.
ALTER TABLE workspace.saga_steps
  ADD COLUMN input_digest text NOT NULL DEFAULT 'sha256:' || repeat('0', 64),
  ADD COLUMN execution_epoch bigint NOT NULL DEFAULT 0;

ALTER TABLE workspace.saga_steps
  ADD CONSTRAINT saga_steps_input_digest_format CHECK (input_digest ~ '^sha256:[0-9a-f]{64}$'),
  ADD CONSTRAINT saga_steps_execution_epoch_nonnegative CHECK (execution_epoch >= 0);

COMMIT;
