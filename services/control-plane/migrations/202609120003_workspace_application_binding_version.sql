ALTER TABLE control_plane_workspaces
  ADD COLUMN IF NOT EXISTS application_binding_version BIGINT NOT NULL DEFAULT 0;
