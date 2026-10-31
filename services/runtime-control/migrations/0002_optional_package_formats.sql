BEGIN;
DO $$ BEGIN
 IF current_database() <> 'opl_runtime_control' THEN RAISE EXCEPTION 'Wrong database: expected opl_runtime_control, got %', current_database(); END IF;
END $$;
SET LOCAL TIME ZONE 'UTC';
SET LOCAL ROLE opl_runtime_control_owner;

-- An omitted optional capability projects to an empty SQL array. JSON null is
-- still a declared value and must not match; immutable contracts stay unchanged.
ALTER TABLE runtime_control.runtime_releases
  DROP CONSTRAINT IF EXISTS runtime_releases_check5,
  DROP CONSTRAINT IF EXISTS runtime_releases_package_formats_match,
  ADD CONSTRAINT runtime_releases_package_formats_match CHECK (
    (COALESCE(publisher_contract->'packageFormatVersions', '[]'::jsonb) = to_jsonb(package_format_versions)) IS TRUE
  );

COMMIT;
