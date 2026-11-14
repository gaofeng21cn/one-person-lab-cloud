#!/bin/sh
set -eu

# Every owner in this topology owns one database and one schema. The
# owner's own DDL creates the schema under a schema owner role and grants
# its writes to a writer role, so this initialization creates both roles
# and lets the owner's migration install the schema. The owner process
# connects as the schema owner, because an owner installs its own
# migrations at startup.
owners="capability build runtime_control workspace serve resource_catalog fabric tenant gateway"

require_password() {
  if [ "${#2}" -lt 32 ]; then
    echo "PostgreSQL password for $1 must contain at least 32 characters" >&2
    exit 1
  fi
}
require_password postgres "$POSTGRES_PASSWORD"
require_password opl_control_plane "$OPL_CONTROL_PLANE_DATABASE_PASSWORD"
require_password opl_fabric "$OPL_FABRIC_DATABASE_PASSWORD"
require_password opl_ledger "$OPL_LEDGER_DATABASE_PASSWORD"
require_password opl_capability "$OPL_CAPABILITY_DATABASE_PASSWORD"
require_password opl_build "$OPL_BUILD_DATABASE_PASSWORD"
require_password opl_runtime_control "$OPL_RUNTIME_CONTROL_DATABASE_PASSWORD"
require_password opl_workspace "$OPL_WORKSPACE_DATABASE_PASSWORD"
require_password opl_serve "$OPL_SERVE_DATABASE_PASSWORD"
require_password opl_resource_catalog "$OPL_RESOURCE_CATALOG_DATABASE_PASSWORD"
require_password opl_tenant "$OPL_TENANT_DATABASE_PASSWORD"
require_password opl_gateway "$OPL_GATEWAY_DATABASE_PASSWORD"

duplicates="$(printf '%s\n' \
  "$POSTGRES_PASSWORD" \
  "$OPL_CONTROL_PLANE_DATABASE_PASSWORD" \
  "$OPL_FABRIC_DATABASE_PASSWORD" \
  "$OPL_LEDGER_DATABASE_PASSWORD" \
  "$OPL_CAPABILITY_DATABASE_PASSWORD" \
  "$OPL_BUILD_DATABASE_PASSWORD" \
  "$OPL_RUNTIME_CONTROL_DATABASE_PASSWORD" \
  "$OPL_WORKSPACE_DATABASE_PASSWORD" \
  "$OPL_SERVE_DATABASE_PASSWORD" \
  "$OPL_RESOURCE_CATALOG_DATABASE_PASSWORD" \
  "$OPL_TENANT_DATABASE_PASSWORD" \
  "$OPL_GATEWAY_DATABASE_PASSWORD" | sort | uniq -d)"
if [ -n "$duplicates" ]; then
  echo "PostgreSQL passwords must be distinct, and this installation reuses one" >&2
  exit 1
fi

sql="$(mktemp)"
: >"$sql"

for owner in $owners; do
  upper="$(printf '%s' "$owner" | tr 'a-z' 'A-Z')"
  eval "password=\${OPL_${upper}_DATABASE_PASSWORD}"
  # The password reaches PostgreSQL as a SQL literal. format's %L quotes
  # it, and the literal is escaped first so an unusual character cannot
  # end it early.
  escaped="$(printf '%s' "$password" | sed "s/'/''/g")"
  printf "SELECT format('CREATE ROLE opl_%s_owner LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION PASSWORD %%L', '%s') WHERE NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'opl_%s_owner') \\gexec\n" "$owner" "$escaped" "$owner" >>"$sql"
  printf "SELECT format('CREATE ROLE opl_%s_writer NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION', '') WHERE NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'opl_%s_writer') \\gexec\n" "$owner" "$owner" >>"$sql"
  printf "GRANT opl_%s_writer TO opl_%s_owner WITH ADMIN OPTION;\n" "$owner" "$owner" >>"$sql"
  printf "SELECT 'CREATE DATABASE opl_%s OWNER opl_%s_owner' WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = 'opl_%s') \\gexec\n" "$owner" "$owner" "$owner" >>"$sql"
  printf "REVOKE ALL ON DATABASE opl_%s FROM PUBLIC;\n" "$owner" >>"$sql"
  printf "GRANT CONNECT ON DATABASE opl_%s TO opl_%s_writer;\n" "$owner" "$owner" >>"$sql"
done

# The Ledger and the retained Control Plane keep the flat login role their
# own DDL expects. They own no owner schema and no writer boundary.
for owner in ledger control_plane; do
  upper="$(printf '%s' "$owner" | tr 'a-z' 'A-Z')"
  eval "password=\${OPL_${upper}_DATABASE_PASSWORD}"
  escaped="$(printf '%s' "$password" | sed "s/'/''/g")"
  printf "SELECT format('CREATE ROLE opl_%s LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION PASSWORD %%L', '%s') WHERE NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'opl_%s') \\gexec\n" "$owner" "$escaped" "$owner" >>"$sql"
  printf "SELECT 'CREATE DATABASE opl_%s OWNER opl_%s' WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = 'opl_%s') \\gexec\n" "$owner" "$owner" "$owner" >>"$sql"
  printf "REVOKE ALL ON DATABASE opl_%s FROM PUBLIC;\n" "$owner" >>"$sql"
done

psql --set=ON_ERROR_STOP=1 \
  --username "$POSTGRES_USER" \
  --dbname "$POSTGRES_DB" \
  -f "$sql"
