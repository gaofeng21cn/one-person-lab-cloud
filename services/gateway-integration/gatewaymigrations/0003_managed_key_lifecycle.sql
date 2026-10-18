BEGIN;
DO $$ BEGIN
 IF current_database() <> 'opl_gateway' THEN RAISE EXCEPTION 'Wrong database: expected opl_gateway, got %', current_database(); END IF;
END $$;
SET LOCAL TIME ZONE 'UTC';
SET LOCAL ROLE opl_gateway_owner;

-- Reversible disablement is a distinct fact from permanent revocation. The
-- column records only the transition the external authority confirmed; a refused
-- or unobserved request never writes it. lifecycle_version advances once per
-- confirmed transition, so a reader can tell two owner answers apart without
-- reading a provider again.
ALTER TABLE gateway.key_bindings
  ADD COLUMN disabled_at timestamptz,
  ADD COLUMN lifecycle_version bigint NOT NULL DEFAULT 0,
  ADD COLUMN lifecycle_updated_at timestamptz,
  ADD CONSTRAINT key_bindings_lifecycle_version_nonnegative CHECK (lifecycle_version >= 0);
COMMIT;
