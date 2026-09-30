BEGIN;
DO $$ BEGIN
 IF current_database() <> 'opl_serve' THEN RAISE EXCEPTION 'Wrong database: expected opl_serve, got %', current_database(); END IF;
END $$;
SET LOCAL TIME ZONE 'UTC';
SET LOCAL ROLE opl_serve_owner;

-- Serve owns the route object: serve.access_bindings is the authoritative current
-- route, so the revision a switch conditions on is the binding's own last
-- confirmed route revision and never a provider token. The columns are renamed to
-- the fact they actually hold, and the code that reads and writes them changes in
-- the same commit.
ALTER TABLE serve.access_bindings RENAME COLUMN provider_revision TO route_revision;
ALTER TABLE serve.access_switches RENAME COLUMN expected_provider_revision TO expected_route_revision;
ALTER TABLE serve.access_switches RENAME COLUMN observed_provider_revision TO observed_route_revision;

-- The switch identity is the row identity. routeProviderCommandID derived a second
-- identity from the switch id and the UNIQUE constraint on it was therefore
-- equivalent to the primary key, while provider_request_ref named a request to an
-- external router command that Serve no longer issues. Both are retired rather
-- than renamed: a reader already resumes from switch_id, and a replay of the same
-- logical switch already conflicts on the primary key.
ALTER TABLE serve.access_switches DROP CONSTRAINT access_switches_provider_command_id_key;
ALTER TABLE serve.access_switches DROP COLUMN provider_command_id;
ALTER TABLE serve.access_switches DROP COLUMN provider_request_ref;

-- The first route's precondition is the caller's assertion that it read this
-- binding and found no confirmed route. The columns recorded an external router's
-- absence receipt and its observation time; with no external router there is no
-- such receipt, and the switch row states the same fact by carrying no expected
-- revision at generation zero. The check that enforced the receipt shape is
-- dropped with the columns it referenced, and the surviving "no revision only at
-- generation zero" check already states the local rule.
DO $$
DECLARE constraint_name text;
BEGIN
 FOR constraint_name IN
  SELECT con.conname FROM pg_constraint con
  JOIN pg_class rel ON rel.oid = con.conrelid
  JOIN pg_namespace nsp ON nsp.oid = rel.relnamespace
  WHERE nsp.nspname = 'serve' AND rel.relname = 'access_switches' AND con.contype = 'c'
    AND pg_get_constraintdef(con.oid) LIKE '%expected_absence_receipt_id%'
 LOOP
  EXECUTE format('ALTER TABLE serve.access_switches DROP CONSTRAINT %I', constraint_name);
 END LOOP;
END $$;
ALTER TABLE serve.access_switches DROP COLUMN expected_absence_receipt_id;
ALTER TABLE serve.access_switches DROP COLUMN expected_absence_observed_at;

COMMIT;
