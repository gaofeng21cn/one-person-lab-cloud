#!/usr/bin/env bash
# Install OPL Cloud from one portable Release or Candidate bundle.
#
# The bundle is the unit of trust. SHA256SUMS names every file the bundle
# holds, and this script refuses to run when a named file is missing, when a
# file does not match its checksum, or when the directory holds a regular file
# the checksum manifest does not name. Nothing in the bundle is used before it
# is verified, and no file outside the bundle is used at all.
#
# The version fact is one fact: the immutable image digest the bundle's own
# manifest declares. This script reads it from that manifest and writes it into
# the environment file, so the image an installation runs is the image the
# release names and not a string somebody typed.
#
# The environment file is derived from the bundle's own template. A placeholder
# that names a secret this installation owns is filled with a fresh random
# value, a placeholder that names a fact the bundle declares is filled from
# that declaration, and a placeholder that needs a decision only the operator
# can make is reported by name and stops the installation. An unfilled fact is
# never handed to Compose as a raw placeholder, because a "<" is a redirection
# operator to any shell that reads the file.
#
# The first run writes the environment file and reports the facts the operator
# still owns. The second run, after those facts are filled in, pulls the images
# and starts the stack.
#
# Usage:
#   bash install.sh [--deployment NAME] [--fabric NAME] [--env-file PATH]
#                   [--data-dir PATH] [--without-workspace] [--dry-run]
#
#   --deployment NAME   platform-owned | managed-tke | customer-owned
#   --fabric NAME       local-docker | tencent-tke
#   --env-file PATH     the environment file to write and read (default: .env)
#   --data-dir PATH     the installation's own data directory (default: ./data)
#   --without-workspace do not add the Workspace overlay to the stack
#   --dry-run           derive and verify everything, start nothing
#
# Exit codes:
#   0  the stack is installed
#   1  the installation is refused
#   2  the installation waits for facts only the operator can supply
set -euo pipefail

readonly SCRIPT_NAME="install.sh"

deployment="platform-owned"
fabric="local-docker"
env_file=""
data_dir=""
with_workspace="yes"
dry_run="no"

# The Compose project name. compose.yaml declares "name: opl-cloud", and the
# Local-Docker Fabric provider addresses the Control Plane by container name,
# so the installation pins both to the same spelling.
project="opl-cloud"

bundle_dir=""
template_file=""
checksum_file=""
manifest_file=""
image_ref=""
source_sha=""
source_tree=""
compose_files=""

fail() {
  printf '%s: %s\n' "$SCRIPT_NAME" "$1" >&2
  exit 1
}

info() {
  printf '%s\n' "$1"
}

need_operands() {
  printf '%s: %s\n' "$SCRIPT_NAME" "$1" >&2
  printf '  %s\n' "${@:2}" >&2
  exit 2
}

usage() {
  sed -n '/^# Usage:/,/^# Exit codes:/p' "$0" | sed 's/^# \{0,1\}//'
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --deployment) [ "$#" -ge 2 ] || fail "--deployment needs a value"; deployment="$2"; shift 2 ;;
    --fabric) [ "$#" -ge 2 ] || fail "--fabric needs a value"; fabric="$2"; shift 2 ;;
    --env-file) [ "$#" -ge 2 ] || fail "--env-file needs a value"; env_file="$2"; shift 2 ;;
    --data-dir) [ "$#" -ge 2 ] || fail "--data-dir needs a value"; data_dir="$2"; shift 2 ;;
    --without-workspace) with_workspace="no"; shift ;;
    --dry-run) dry_run="yes"; shift ;;
    --help|-h) usage; exit 0 ;;
    *) fail "unknown argument: $1" ;;
  esac
done

# ---------------------------------------------------------------- the bundle

bundle_dir="$(cd "$(dirname "$0")" && pwd)"
template_file="$bundle_dir/opl-cloud.env.example"
checksum_file="$bundle_dir/SHA256SUMS"

[ -f "$template_file" ] || fail "not an OPL Cloud bundle: $template_file is missing"
[ -f "$checksum_file" ] || fail "not an OPL Cloud bundle: $checksum_file is missing"
[ -f "$bundle_dir/compose.yaml" ] || fail "not an OPL Cloud bundle: compose.yaml is missing"

[ -n "$env_file" ] || env_file="$bundle_dir/.env"
[ -n "$data_dir" ] || data_dir="$bundle_dir/data"

# The checksum manifest names every file of the bundle. A file it does not name
# is a file this installation did not verify, so it is refused rather than
# ignored: an operator who drops a compose override next to the bundle must
# not have it silently change the stack.
verified_names=""
while read -r _checksum name; do
  [ -n "$name" ] || continue
  verified_names="$verified_names $name"
  [ -f "$bundle_dir/$name" ] || fail "bundle file is missing: $name"
  actual="$(
    if command -v sha256sum >/dev/null 2>&1; then
      sha256sum "$bundle_dir/$name" | cut -d ' ' -f 1
    else
      shasum -a 256 "$bundle_dir/$name" | cut -d ' ' -f 1
    fi
  )"
  [ "$actual" = "$_checksum" ] || fail "bundle file does not match its checksum: $name"
done < "$checksum_file"

[ -n "$verified_names" ] || fail "the checksum manifest names no file"

for entry in "$bundle_dir"/* "$bundle_dir"/.[!.]*; do
  [ -e "$entry" ] || continue
  [ -f "$entry" ] || continue
  name="$(basename "$entry")"
  [ "$name" = "SHA256SUMS" ] && continue
  [ "$name" = "$(basename "$env_file")" ] && continue
  case " $verified_names " in
    *" $name "*) ;;
    *) fail "bundle holds an unverified file: $name" ;;
  esac
done

# -------------------------------------------------------------- the manifest

if [ -f "$bundle_dir/opl-cloud-release.json" ] && [ -f "$bundle_dir/opl-cloud-candidate.json" ]; then
  fail "the bundle holds both a release manifest and a candidate manifest"
fi
if [ -f "$bundle_dir/opl-cloud-release.json" ]; then
  manifest_file="$bundle_dir/opl-cloud-release.json"
  image_key="ref"
elif [ -f "$bundle_dir/opl-cloud-candidate.json" ]; then
  manifest_file="$bundle_dir/opl-cloud-candidate.json"
  image_key="indexRef"
else
  fail "the bundle holds no manifest"
fi

# The image reference is read as one string, "repository@sha256:digest", so it
# cannot be mistaken for a digest of something else. The manifest must name
# exactly one, and a manifest that names none or several is refused rather
# than guessed at.
image_ref="$(
  grep -oE "\"$image_key\"[[:space:]]*:[[:space:]]*\"[^\"]+@sha256:[0-9a-f]{64}\"" "$manifest_file" \
    | sed -E 's/.*"([^"]+)".*/\1/' \
    | sort -u
)"
[ -n "$image_ref" ] || fail "the manifest names no immutable image reference"
[ "$(printf '%s\n' "$image_ref" | wc -l | tr -d ' ')" = "1" ] || fail "the manifest names more than one image reference"

source_sha="$(
  grep -oE '"sha"[[:space:]]*:[[:space:]]*"[0-9a-f]{40}"' "$manifest_file" \
    | grep -oE '[0-9a-f]{40}' | sort -u
)"
[ "$(printf '%s\n' "$source_sha" | wc -l | tr -d ' ')" = "1" ] || fail "the manifest does not name exactly one product source"
source_tree="$(
  grep -oE '"tree"[[:space:]]*:[[:space:]]*"[0-9a-f]{40}"' "$manifest_file" \
    | grep -oE '[0-9a-f]{40}' | sort -u
)"
[ "$(printf '%s\n' "$source_tree" | wc -l | tr -d ' ')" = "1" ] || fail "the manifest does not name exactly one product tree"

# ------------------------------------------------------------ the compose set

deployment_file="compose.deployment-$deployment.yaml"
fabric_file="compose.fabric-$fabric.yaml"
[ -f "$bundle_dir/$deployment_file" ] || fail "unknown deployment profile: $deployment"
[ -f "$bundle_dir/$fabric_file" ] || fail "unknown fabric provider: $fabric"

compose_files="compose.yaml $deployment_file $fabric_file"
if [ "$with_workspace" = "yes" ]; then
  [ -f "$bundle_dir/compose.local-workspace.yaml" ] || fail "the bundle has no workspace overlay"
  compose_files="$compose_files compose.local-workspace.yaml"
fi

# -------------------------------------------------------------- the preflight

command -v docker >/dev/null 2>&1 || fail "docker is not installed"
docker compose version >/dev/null 2>&1 || fail "docker compose v2 is not available"

# ------------------------------------------------- the variables the stack reads

# A "$$" pair is a literal "$" to the Compose reader, so the text inside it is
# not a variable reference. The scan drops every such pair before it looks for
# references, which keeps a shell expression in an inline configuration from
# being read as a fact the stack needs.
stack_variables() {
  # Every variable the selected stack reads, defaulted ones included.
  for file in $compose_files; do
    sed 's/\$\$//g' "$bundle_dir/$file" \
      | grep -oE '\$\{[A-Za-z_][A-Za-z0-9_]*(:[-?][^}]*)?\}' \
      | grep -oE '\$\{[A-Za-z_][A-Za-z0-9_]*' \
      | sed 's/^\${//'
  done | sort -u
}

required_variables() {
  # The subset the stack refuses to start without.
  for file in $compose_files; do
    sed 's/\$\$//g' "$bundle_dir/$file" \
      | grep -oE '\$\{[A-Za-z_][A-Za-z0-9_]*(:\?[^}]*)?\}' \
      | grep -oE '\$\{[A-Za-z_][A-Za-z0-9_]*' \
      | sed 's/^\${//'
  done | sort -u
}

stack_vars="$(stack_variables)"
required_vars="$(required_variables)"
[ -n "$required_vars" ] || fail "the selected stack reads no variable"

# ------------------------------------------------------ the environment file

random_hex() {
  # 32 bytes of entropy, hex encoded: 64 characters, which satisfies the
  # PostgreSQL initialization's 32 character minimum and needs no escaping in
  # a database URL.
  if command -v openssl >/dev/null 2>&1; then
    openssl rand -hex 32
  else
    od -An -tx1 -N32 /dev/urandom | tr -d ' \n'
  fi
}

is_placeholder() {
  case "$1" in
    *"<"*">"*) return 0 ;;
    *) return 1 ;;
  esac
}

is_sentinel() {
  case "$1" in
    unset-*) return 0 ;;
    *) return 1 ;;
  esac
}

existing_value() {
  [ -f "$env_file" ] || return 0
  awk -v name="$1" 'index($0, name "=") == 1 { value = substr($0, length(name) + 2) } END { print value }' "$env_file"
}

template_note() {
  # The template documents every fact in the comment lines above it. The first
  # line of that comment is the one that says what the fact is. A blank line
  # between the comment and the fact does not end the note, because the
  # template separates facts with blank lines and the note still belongs to the
  # fact below it.
  awk -v name="$1" '
    /^[[:space:]]*#/ { if (note == "") note = $0; next }
    /^[[:space:]]*$/ { next }
    {
      if (index($0, name "=") == 1) { print note; exit }
      note = ""
    }
  ' "$template_file"
}

# The value this installation supplies for one name.
#
# A name the operator already filled in keeps its value: the environment file
# belongs to the installation, and re-running the installer must not overwrite
# a fact somebody decided. Everything else is derived: the image and the
# product revision come from the bundle's own manifest, the paths come from the
# installation's own directories, the secrets come from fresh entropy, and a
# fact that needs a decision keeps a sentinel that the required check reports.
derive_value() {
  name="$1"
  template_value="$2"

  existing="$(existing_value "$name")"
  if [ -n "$existing" ] && ! is_placeholder "$existing" && ! is_sentinel "$existing"; then
    printf '%s' "$existing"
    return 0
  fi

  case "$name" in
    OPL_CLOUD_IMAGE) printf '%s' "$image_ref" ;;
    OPL_RELEASE_SHA|OPL_LOCAL_V2_RELEASE_SHA|OPL_QUALIFICATION_SOURCE_SHA) printf '%s' "$source_sha" ;;
    *RELEASE_SHA|*SOURCE_SHA) printf '%s' "$source_sha" ;;
    *PRODUCT_TREE) printf '%s' "$source_tree" ;;
    OPL_POSTGRES_DATA_ROOT) printf '%s' "$data_dir/postgres" ;;
    OPL_FABRIC_LOCAL_DOCKER_STORAGE_ROOT) printf '%s' "$data_dir/fabric-storage" ;;
    OPL_FABRIC_LOCAL_DOCKER_SECRET_ROOT) printf '%s' "$data_dir/fabric-secrets" ;;
    OPL_FABRIC_LOCAL_DOCKER_GATEWAY_CONTAINER) printf '%s' "$project-control-plane" ;;
    *_DATABASE_PASSWORD|OPL_POSTGRES_ADMIN_PASSWORD|*_SERVICE_TOKEN|*_CAPABILITY_KEY|*_ADMIN_PASSWORD_SEED)
      random_hex ;;
    *)
      # A template value that is not a placeholder is a fact the product
      # declares, and it stays. A placeholder is an unfilled fact, and it
      # becomes a sentinel the required check reports by name.
      if is_placeholder "$template_value"; then
        printf 'unset-%s' "$(printf '%s' "$name" | tr '[:upper:]' '[:lower:]')"
      else
        printf '%s' "$template_value"
      fi
      ;;
  esac
}

derived_env=""
while IFS= read -r line; do
  name="${line%%=*}"
  case "$name" in
    [A-Za-z_]*) ;;
    *) continue ;;
  esac
  case "$line" in
    *=*) ;;
    *) continue ;;
  esac
  template_value="${line#*=}"
  # The environment file holds the facts this installation has, not every fact
  # the template knows about. A name the selected stack does not read is left
  # out unless the operator already decided it, in which case their value
  # stays: the file belongs to the installation, not to the profile.
  if ! printf '%s\n' "$stack_vars" | grep -qx "$name"; then
    decided="$(existing_value "$name")"
    if [ -z "$decided" ] || is_sentinel "$decided" || is_placeholder "$decided"; then
      continue
    fi
    derived_env="$derived_env$name=$decided
"
    continue
  fi
  value="$(derive_value "$name" "$template_value")"
  derived_env="$derived_env$name=$value
"
done < "$template_file"

[ -n "$derived_env" ] || fail "the environment template declares no variable"

# ------------------------------------------------------- the facts still owed

# The environment file is written before the facts are reported, so the
# operator finds them in the file they are asked to edit.
umask 077
printf '%s' "$derived_env" > "$env_file"
chmod 600 "$env_file"

info "$SCRIPT_NAME: verified the bundle."
info "  image     $image_ref"
info "  product   sha $source_sha tree $source_tree"
info "  profile   deployment=$deployment fabric=$fabric workspace=$with_workspace"
info "  env file  $env_file"

missing=""
for name in $required_vars; do
  value="$(printf '%s' "$derived_env" | sed -n "s/^$name=//p" | head -n 1)"
  if [ -z "$value" ] || is_placeholder "$value" || is_sentinel "$value"; then
    missing="$missing $name"
  fi
done

if [ -n "$missing" ]; then
  info "$SCRIPT_NAME: the environment file needs facts only you can supply."
  for name in $missing; do
    note="$(template_note "$name")"
    if [ -n "$note" ]; then
      printf '  %s\n    %s\n' "$name" "$note" >&2
    else
      printf '  %s\n' "$name" >&2
    fi
  done
  printf 'Fill them in %s and run %s again.\n' "$env_file" "$SCRIPT_NAME" >&2
  exit 2
fi

# The PostgreSQL initialization refuses a short password and refuses two roles
# that share one. The installer refuses the same two shapes before Docker
# does, because the failure otherwise appears as a crashed container.
passwords="$(
  printf '%s\n' "$derived_env" \
    | sed -n -E 's/^(OPL_POSTGRES_ADMIN_PASSWORD|[A-Za-z_]*_DATABASE_PASSWORD)=(.*)$/\2/p'
)"
short=""
for password in $passwords; do
  [ "${#password}" -ge 32 ] || short="$short ${#password}"
done
[ -z "$short" ] || fail "a database password is shorter than the 32 characters PostgreSQL requires"
distinct="$(printf '%s\n' "$passwords" | grep . | sort -u | wc -l | tr -d ' ')"
total="$(printf '%s\n' "$passwords" | grep -c . || true)"
[ "$distinct" = "$total" ] || fail "two database roles share one password"

# The paths the Local-Docker provider serves and the PostgreSQL data root are
# the installation's own directories. The installer creates them so the
# operator does not have to know they exist.
if [ "$with_workspace" = "yes" ] || [ "$fabric" = "local-docker" ]; then
  mkdir -p "$data_dir/postgres" "$data_dir/fabric-storage" "$data_dir/fabric-secrets"
  chmod 700 "$data_dir/fabric-storage" "$data_dir/fabric-secrets"
fi

# ------------------------------------------------------------- the stack

compose_args=""
for file in $compose_files; do
  compose_args="$compose_args -f $file"
done

# The Compose file names are relative to the bundle, so the installation runs
# in the bundle whatever directory the operator invoked it from.
cd "$bundle_dir"

if [ "$dry_run" = "yes" ]; then
  info "  dry run: no image was pulled and no container was started"
  exit 0
fi
# The derived environment is checked by Compose itself before anything is
# pulled: an interpolation the stack would refuse is reported here, while the
# installation can still be corrected, rather than as a container that fails to
# start.
# shellcheck disable=SC2086
docker compose --env-file "$env_file" $compose_args config --quiet

# shellcheck disable=SC2086
docker compose --env-file "$env_file" $compose_args pull
# shellcheck disable=SC2086
docker compose --env-file "$env_file" $compose_args up -d

public_url="$(printf '%s' "$derived_env" | sed -n 's/^OPL_PUBLIC_URL=//p' | head -n 1)"
info "$SCRIPT_NAME: the stack is up."
info "  services  postgres ledger fabric control-plane"
[ -n "$public_url" ] && info "  url       $public_url"
exit 0
