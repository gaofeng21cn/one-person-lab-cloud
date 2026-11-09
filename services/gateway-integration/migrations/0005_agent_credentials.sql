BEGIN;
DO $$ BEGIN
 IF current_database() <> 'opl_tenant' THEN RAISE EXCEPTION 'Wrong database: expected opl_tenant, got %', current_database(); END IF;
END $$;
SET LOCAL TIME ZONE 'UTC';
SET LOCAL ROLE opl_tenant_owner;

-- A short-lived MCP credential delegates an existing browser session. The child
-- session preserves the existing authorization-context foreign key; neither its
-- reference nor its random CSRF digest is a browser cookie.
CREATE TABLE tenant.agent_credentials (
  id text PRIMARY KEY,
  credential_hash text NOT NULL UNIQUE CHECK (credential_hash ~ '^[0-9a-f]{64}$'),
  label text NOT NULL CHECK (length(label) BETWEEN 1 AND 128),
  parent_session_id text NOT NULL REFERENCES tenant.sessions(id) ON DELETE RESTRICT,
  session_id text NOT NULL UNIQUE REFERENCES tenant.sessions(id) ON DELETE RESTRICT,
  actor_id text NOT NULL,
  tenant_id text REFERENCES tenant.tenants(id) ON DELETE RESTRICT,
  audience text NOT NULL CHECK (audience = 'opl-cloud-mcp'),
  issued_at timestamptz NOT NULL,
  expires_at timestamptz NOT NULL,
  revoked_at timestamptz,
  CHECK (expires_at > issued_at AND expires_at <= issued_at + interval '1 hour'),
  CHECK (session_id = 'agent:' || id),
  CHECK (parent_session_id NOT LIKE 'agent:%')
);
CREATE INDEX agent_credentials_actor ON tenant.agent_credentials(actor_id, issued_at DESC, id DESC);
CREATE INDEX agent_credentials_parent ON tenant.agent_credentials(parent_session_id);
GRANT SELECT, INSERT, UPDATE, DELETE ON tenant.agent_credentials TO opl_tenant_writer;
COMMIT;
