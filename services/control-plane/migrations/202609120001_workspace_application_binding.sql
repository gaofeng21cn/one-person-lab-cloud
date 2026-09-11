ALTER TABLE control_plane_workspaces
  ADD COLUMN IF NOT EXISTS application_binding TEXT NOT NULL DEFAULT '';

-- Retained workspaces were all provisioned with the fixed OPL App Runtime;
-- the empty binding exists only for resource-only provisioning going forward.
UPDATE control_plane_workspaces
  SET application_binding = 'opl_app'
  WHERE application_binding = '' AND runtime_id <> '';
