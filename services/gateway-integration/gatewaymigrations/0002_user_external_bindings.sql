BEGIN;
DO $$ BEGIN
 IF current_database() <> 'opl_gateway' THEN RAISE EXCEPTION 'Wrong database: expected opl_gateway, got %', current_database(); END IF;
END $$;
SET LOCAL TIME ZONE 'UTC';
SET LOCAL ROLE opl_gateway_owner;

-- Keep User identity and OCI destination in the existing Gateway mapping owner.
ALTER TABLE gateway.identity_mappings
  ALTER COLUMN verified_at DROP NOT NULL,
  ADD COLUMN registry_host text,
  ADD COLUMN registry_namespace text,
  ADD COLUMN repository text,
  ADD COLUMN destination_verified_at timestamptz,
  ADD COLUMN revoked_at timestamptz,
  ADD CONSTRAINT identity_mappings_identity_nonempty CHECK (
    actor_id <> '' AND actor_id = btrim(actor_id) AND
    sub2api_user_id <> '' AND sub2api_user_id = btrim(sub2api_user_id)),
  ADD CONSTRAINT identity_mappings_destination_complete CHECK (
    (registry_host IS NULL AND registry_namespace IS NULL AND repository IS NULL AND destination_verified_at IS NULL) OR
    (registry_host IS NOT NULL AND registry_namespace IS NOT NULL AND repository IS NOT NULL)),
  ADD CONSTRAINT identity_mappings_host_safe CHECK (
    registry_host IS NULL OR registry_host ~ '^[a-z0-9]([a-z0-9.-]*[a-z0-9])?(:[0-9]{1,5})?$'),
  ADD CONSTRAINT identity_mappings_namespace_safe CHECK (
    registry_namespace IS NULL OR (registry_namespace ~ '^[a-z0-9]([a-z0-9._-]{0,126}[a-z0-9])?$' AND position('..' in registry_namespace) = 0)),
  ADD CONSTRAINT identity_mappings_repository_safe CHECK (
    repository IS NULL OR (repository ~ '^[a-z0-9]([a-z0-9._-]{0,126}[a-z0-9])?$' AND position('..' in repository) = 0));

-- Reservations remain unique after revocation; identity cannot be reassigned.
CREATE UNIQUE INDEX identity_mappings_destination
 ON gateway.identity_mappings (registry_host, registry_namespace, repository);
COMMIT;
