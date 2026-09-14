ALTER TABLE control_plane_workspaces
  ADD COLUMN IF NOT EXISTS current_application_deployment_id TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS reserved_application_deployment_id TEXT NOT NULL DEFAULT '';

LOCK TABLE control_plane_runtime_operations IN SHARE ROW EXCLUSIVE MODE;

-- Retained selections are resolved only by their exact committed binding and
-- version. Multiple or missing matches require owner reconciliation.
DO $$
BEGIN
  IF EXISTS (
    SELECT 1 FROM control_plane_workspaces w
    WHERE w.application_binding LIKE '%@%'
	  AND w.current_application_deployment_id = ''
      AND 1 <> (
        SELECT count(*) FROM control_plane_runtime_operations o
        WHERE o.workspace_id = w.id AND o.action = 'workspace.application.deploy'
          AND o.result::jsonb->>'applicationId' || '@' || (o.result::jsonb->>'targetRevision') = w.application_binding
          AND (o.result::jsonb->>'expectedWorkspaceVersion')::bigint + 1 = w.application_binding_version
          AND o.result::jsonb->>'phase' IN ('receipt', 'active')
      )
  ) THEN RAISE EXCEPTION 'workspace_application_selection_unresolvable'; END IF;

  IF EXISTS (
    SELECT 1 FROM control_plane_workspaces w
    WHERE w.reserved_application_deployment_id = ''
      AND 1 < (
        SELECT count(*) FROM control_plane_runtime_operations o
        WHERE o.workspace_id = w.id AND o.action = 'workspace.application.deploy'
          AND (o.result::jsonb->>'version')::integer = 1
          AND o.result::jsonb->>'currentBinding' = w.application_binding
          AND (o.result::jsonb->>'expectedWorkspaceVersion')::bigint = w.application_binding_version
          AND o.result::jsonb->>'phase' IN ('intent', 'runtime', 'activating', 'manual_review')
      )
  ) THEN RAISE EXCEPTION 'workspace_application_reservation_unresolvable'; END IF;
END $$;

UPDATE control_plane_workspaces w SET current_application_deployment_id = o.id
FROM control_plane_runtime_operations o
WHERE o.workspace_id = w.id AND o.action = 'workspace.application.deploy'
  AND w.current_application_deployment_id = ''
  AND o.result::jsonb->>'applicationId' || '@' || (o.result::jsonb->>'targetRevision') = w.application_binding
  AND (o.result::jsonb->>'expectedWorkspaceVersion')::bigint + 1 = w.application_binding_version
  AND o.result::jsonb->>'phase' IN ('receipt', 'active');

-- An unfinished v1 command still owns the frozen Workspace binding/version.
-- It takes precedence over the already-selected application. Completed
-- selections retain their reservation until the current owner releases it.
UPDATE control_plane_workspaces w SET reserved_application_deployment_id = COALESCE((
  SELECT o.id FROM control_plane_runtime_operations o
  WHERE o.workspace_id = w.id AND o.action = 'workspace.application.deploy'
    AND (o.result::jsonb->>'version')::integer = 1
    AND o.result::jsonb->>'currentBinding' = w.application_binding
    AND (o.result::jsonb->>'expectedWorkspaceVersion')::bigint = w.application_binding_version
    AND o.result::jsonb->>'phase' IN ('intent', 'runtime', 'activating', 'manual_review')
), w.current_application_deployment_id)
WHERE w.reserved_application_deployment_id = '';
