CREATE TABLE IF NOT EXISTS control_plane_application_revisions (
    id TEXT PRIMARY KEY,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    application_id TEXT NOT NULL,
    version TEXT NOT NULL,
    digest TEXT NOT NULL,
    payload TEXT NOT NULL,
    admitted_by_user_id TEXT NOT NULL,
    CONSTRAINT control_plane_application_revisions_identity_unique
        UNIQUE (application_id, version)
);

CREATE INDEX IF NOT EXISTS control_plane_application_revisions_application_idx
    ON control_plane_application_revisions (application_id);
