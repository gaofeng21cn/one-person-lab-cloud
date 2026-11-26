BEGIN;
DO $$ BEGIN
  IF current_database() <> 'opl_resource_catalog' THEN RAISE EXCEPTION 'Wrong database: expected opl_resource_catalog, got %', current_database(); END IF;
END $$;
SET LOCAL TIME ZONE 'UTC';
SET LOCAL ROLE opl_resource_catalog_owner;

-- A renewal offer names the one Workspace it renews and carries no application
-- selection and no model selection of its own: the renewed Workspace keeps the
-- application and the models it already runs. A deploy offer names no Workspace,
-- because it creates one.
ALTER TABLE resource_catalog.quotes
  ADD CONSTRAINT quotes_purpose_workspace_binding CHECK (
    (purpose = 'renew' AND workspace_id IS NOT NULL AND model_selections = '[]'::jsonb)
    OR (purpose <> 'renew' AND workspace_id IS NULL)
  );

-- The application selection is exactly one source for a deploy offer. A renewal
-- offer has no source of its own, so it joins the constraint as the third shape
-- instead of pretending to be an Agent it does not name.
ALTER TABLE resource_catalog.quotes
  DROP CONSTRAINT quotes_application_selection_mutually_exclusive;

ALTER TABLE resource_catalog.quotes
  ADD CONSTRAINT quotes_application_selection_mutually_exclusive CHECK (
    (application_kind = 'opl_app' AND runtime_version_id IS NOT NULL AND capability_version_id IS NULL)
    OR (application_kind = 'agent' AND capability_version_id IS NOT NULL AND runtime_version_id IS NULL)
    OR (purpose = 'renew' AND application_kind IS NULL AND capability_version_id IS NULL AND runtime_version_id IS NULL)
  );

COMMIT;
