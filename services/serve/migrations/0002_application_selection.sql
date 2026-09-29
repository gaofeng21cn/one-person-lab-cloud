BEGIN;
DO $$ BEGIN
 IF current_database() <> 'opl_serve' THEN RAISE EXCEPTION 'Wrong database: expected opl_serve, got %', current_database(); END IF;
END $$;
SET LOCAL TIME ZONE 'UTC';
SET LOCAL ROLE opl_serve_owner;

-- W01 default-OPL-App source. Serve previously required a CapabilityVersion for
-- every deployment, so the default App had no durable provenance. application_kind
-- records the source: a built Agent names its CapabilityVersion, while the default
-- OPL App names an approved Runtime Release and has no CapabilityVersion. The
-- existing NOT NULL column keeps the empty string as the explicit "no capability"
-- sentinel, so the two sources stay mutually exclusive.
ALTER TABLE serve.agent_deployments
  ADD COLUMN application_kind text,
  ADD COLUMN runtime_version_id text;

ALTER TABLE serve.agent_deployments
  ADD CONSTRAINT agent_deployments_application_source CHECK (
    (application_kind IS NULL AND capability_version_id <> '' AND runtime_version_id IS NULL)
    OR (application_kind = 'agent' AND capability_version_id <> '' AND runtime_version_id IS NULL)
    OR (application_kind = 'opl_app' AND capability_version_id = '' AND runtime_version_id IS NOT NULL AND runtime_version_id <> '')
  );

COMMIT;
