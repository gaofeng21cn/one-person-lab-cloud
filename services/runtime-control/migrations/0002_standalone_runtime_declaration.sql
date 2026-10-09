BEGIN;
DO $$ BEGIN
 IF current_database() <> 'opl_runtime_control' THEN RAISE EXCEPTION 'Wrong database: expected opl_runtime_control, got %', current_database(); END IF;
END $$;
SET LOCAL TIME ZONE 'UTC';
SET LOCAL ROLE opl_runtime_control_owner;

-- A standalone Runtime Release declares only the capabilities it actually has, so it
-- may omit packageFormatVersions entirely instead of inventing a Package format. The
-- stored list then means "this release approves no Package format", and it must still
-- equal the contract's own declaration: an absent array and an empty one are the same
-- fact, while a release that does declare formats keeps the exact equality with its
-- immutable contract bytes.
DO $$ DECLARE constraint_name text;
BEGIN
  SELECT conname INTO constraint_name FROM pg_constraint
    WHERE conrelid = 'runtime_control.runtime_releases'::regclass AND contype = 'c'
      AND pg_get_constraintdef(oid) LIKE '%package_format_versions%';
  IF constraint_name IS NOT NULL THEN
    EXECUTE format('ALTER TABLE runtime_control.runtime_releases DROP CONSTRAINT %I', constraint_name);
  END IF;
END $$;

ALTER TABLE runtime_control.runtime_releases
  ADD CONSTRAINT runtime_releases_package_format_versions_check CHECK (to_jsonb(package_format_versions) = COALESCE(publisher_contract->'packageFormatVersions', '[]'::jsonb));

COMMIT;
