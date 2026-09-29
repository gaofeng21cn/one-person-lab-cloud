BEGIN;
DO $$ BEGIN
 IF current_database() <> 'opl_resource_catalog' THEN RAISE EXCEPTION 'Wrong database: expected opl_resource_catalog, got %', current_database(); END IF;
END $$;
SET LOCAL TIME ZONE 'UTC';
SET LOCAL ROLE opl_resource_catalog_owner;

-- W01 default-OPL-App application selection. A deploy quote freezes exactly one
-- application source: the default OPL App (an approved Runtime Release with
-- runtime_version_id set) or a built Agent (capability_version_id set). The two
-- ids are mutually exclusive and there is no implicit default.
ALTER TABLE resource_catalog.quotes
  ADD COLUMN application_kind text,
  ADD COLUMN runtime_version_id text;

ALTER TABLE resource_catalog.quotes
  ADD CONSTRAINT quotes_application_selection_mutually_exclusive CHECK (
    (application_kind = 'opl_app' AND runtime_version_id IS NOT NULL AND capability_version_id IS NULL)
    OR (application_kind = 'agent' AND capability_version_id IS NOT NULL AND runtime_version_id IS NULL)
  );

COMMIT;
