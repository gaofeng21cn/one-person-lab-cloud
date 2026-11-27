BEGIN;
DO $$ BEGIN
 IF current_database() <> 'opl_tenant' THEN RAISE EXCEPTION 'Wrong database: expected opl_tenant, got %', current_database(); END IF;
END $$;
SET LOCAL TIME ZONE 'UTC';
SET LOCAL ROLE opl_tenant_owner;

-- Canonical platform announcements. One row is one publication every
-- authenticated Cloud User may read; there is no Tenant column, because an
-- announcement is platform content and not a Tenant-private fact.
CREATE TABLE tenant.announcements (
  id text NOT NULL,
  title text NOT NULL,
  body text NOT NULL,
  status text NOT NULL,
  starts_at timestamptz,
  ends_at timestamptz,
  published_at timestamptz,
  withdrawn_at timestamptz,
  published_by text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (id),
  CHECK (id <> '' AND id = btrim(id)),
  CHECK (title <> '' AND title = btrim(title)),
  CHECK (body <> '' AND body = btrim(body)),
  CHECK (status IN ('published', 'withdrawn')),
  CHECK (published_by <> '' AND published_by = btrim(published_by)),
  CHECK (ends_at IS NULL OR starts_at IS NULL OR ends_at > starts_at)
);

-- The read receipt is unique per announcement and reader. A repeated mark
-- therefore keeps the original read_at instead of writing a second row.
CREATE TABLE tenant.announcement_reads (
  announcement_id text NOT NULL REFERENCES tenant.announcements(id) ON DELETE CASCADE,
  actor_id text NOT NULL,
  read_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (announcement_id, actor_id),
  CHECK (actor_id <> '' AND actor_id = btrim(actor_id))
);

CREATE INDEX announcement_reads_actor_idx ON tenant.announcement_reads (actor_id);
GRANT SELECT, INSERT, UPDATE ON tenant.announcements TO opl_tenant_writer;
GRANT SELECT, INSERT ON tenant.announcement_reads TO opl_tenant_writer;
COMMIT;
