BEGIN;
DO $$ BEGIN
 IF current_database() <> 'opl_serve' THEN RAISE EXCEPTION 'Wrong database: expected opl_serve, got %', current_database(); END IF;
END $$;
SET LOCAL TIME ZONE 'UTC';
SET LOCAL ROLE opl_serve_owner;

-- Serve owns the Workspace's access data plane, so the RouteReadback it returns
-- and the proxy that serves the entry both need the exact running instance the
-- binding points at. The execution resource identity alone is not enough: only
-- Serve's own runtime-instance row carries the readiness and the in-cluster
-- destination, and a reader that had to look it up from another owner's data
-- would create a second route resolver.
ALTER TABLE serve.access_bindings
  ADD COLUMN target_runtime_instance_id text,
  ADD COLUMN target_deployment_id text;

ALTER TABLE serve.access_switches
  ADD COLUMN target_runtime_instance_id text,
  ADD COLUMN target_deployment_id text;

-- A switch that selects a target must name the instance it routes to; a fence
-- keeps whatever the binding already points at. The constraint is added NOT
-- VALID so the rows an already installed owner recorded before this migration
-- are not retroactively rewritten; every new row is enforced.
ALTER TABLE serve.access_switches
  ADD CONSTRAINT access_switches_target_instance CHECK (
    action_kind = 'fence'
    OR (target_runtime_instance_id IS NOT NULL AND target_deployment_id IS NOT NULL)
  ) NOT VALID;

-- The gateway destination the executing provider created. It is the in-cluster
-- Service name and port that the installation's gateway proxies to; a runtime
-- that publishes its own external endpoint reports a URL and no upstream. The
-- two are mutually exclusive facts about one entry, and a Service name that is
-- not a single DNS label is never recorded.
ALTER TABLE serve.agent_runtime_instances
  ADD COLUMN access_upstream_service text,
  ADD COLUMN access_upstream_port integer;

ALTER TABLE serve.agent_runtime_instances
  ADD CONSTRAINT agent_runtime_instances_access_upstream CHECK (
    (access_upstream_service IS NULL AND access_upstream_port IS NULL)
    OR (access_upstream_service IS NOT NULL
        AND access_upstream_service ~ '^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$'
        AND access_upstream_port IS NOT NULL
        AND access_upstream_port BETWEEN 1 AND 65535)
  );

COMMIT;
