BEGIN;
SET LOCAL ROLE opl_build_owner;
-- One admission intent per confirmed build artifact. The workspace image trust
-- source is the admission decision for a published repository, and the Build
-- owner is the only owner that knows a repository was published by one of its
-- own jobs. The intent is written in the same transaction as the artifact, so a
-- confirmed artifact always carries exactly one intent and a failed build
-- never carries one.
--
-- This table records no repository grammar and no version grammar on purpose:
-- the Build owner accepts a repository its own worker accepts, and a name the
-- trust source refuses is a registration failure the Build owner retries and
-- reports. A constraint here would fail the build transaction instead.
CREATE TABLE build.build_trust_source_registrations (
  id text NOT NULL,
  build_job_id text NOT NULL,
  release_repository text NOT NULL,
  release_version text NOT NULL,
  status text NOT NULL DEFAULT 'pending',
  trust_source_revision integer,
  attempt_count integer NOT NULL DEFAULT 0,
  last_error_code text,
  next_attempt_at timestamptz NOT NULL DEFAULT now(),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (id),
  FOREIGN KEY (build_job_id) REFERENCES build.build_jobs (id) ON DELETE RESTRICT,
  UNIQUE (build_job_id),
  CHECK (status IN ('pending','registered')),
  CHECK (attempt_count >= 0),
  CHECK (trust_source_revision IS NULL OR trust_source_revision >= 1)
);
CREATE INDEX build_trust_source_registrations_pending ON build.build_trust_source_registrations (next_attempt_at, id) WHERE status <> 'registered';
-- The writer role owns this owner's writes. The artifact table keeps its write
-- boundary: this table is the only one the Build owner updates after it was
-- written, because an admission intent is closed when the Control Plane
-- confirms it.
GRANT SELECT, INSERT, UPDATE, DELETE ON build.build_trust_source_registrations TO opl_build_writer;
COMMIT;
