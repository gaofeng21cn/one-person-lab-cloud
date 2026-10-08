#!/usr/bin/env bash
# Regenerate actual contract-derived outputs from their existing source owners.
# Default: production Go bindings + target-derived public JSON (existing entry).
# Named chains let freshness run each independently in an empty isolated tree.
#
# Pinned tools:
#   grpcio-tools 1.80.0 -> libprotoc 31.1
#   PyYAML 6.0.3
#   protoc-gen-go v1.36.6
#   protoc-gen-go-grpc v1.5.1
#
# The production proto's logical go_package maps into the existing shared
# contracts module. Target/production parity is a migration question, not a
# generation prerequisite.
set -euo pipefail

if [[ "$#" -gt 1 ]]; then
  echo "usage: generate.sh [contracts|bindings|public-json|event-identity|policy]" >&2
  exit 1
fi
chain="${1:-contracts}"
case "${chain}" in
  contracts|bindings|public-json|event-identity|policy) ;;
  *) echo "unknown generation chain: ${chain}" >&2; exit 1 ;;
esac

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
root="$(cd "${here}/../../.." && pwd)"
out="${root}/packages/contracts/go"
py="${PYTHON:-python3}"

if ! command -v "${py}" >/dev/null 2>&1; then
  echo "missing Python: ${py}" >&2
  exit 1
fi

require_package() {
  "${py}" - "$1" "$2" <<'PY'
from importlib.metadata import PackageNotFoundError, version
import sys
package, expected = sys.argv[1:]
try:
    actual = version(package)
except PackageNotFoundError:
    raise SystemExit(f"missing {package}=={expected}; install docs/spec/target/checks/requirements.txt")
if actual != expected:
    raise SystemExit(f"{package} version mismatch: expected {expected}, got {actual}")
PY
}

require_version() {
  local tool="$1" expected="$2" actual
  if ! command -v "${tool}" >/dev/null 2>&1; then
    echo "missing generator: ${tool}; expected ${expected}" >&2
    exit 1
  fi
  actual="$("${tool}" --version)"
  if [[ "${actual}" != "${expected}" ]]; then
    echo "${tool} version mismatch: expected ${expected}, got ${actual}" >&2
    exit 1
  fi
}

# Preflight only the real dependencies of the selected chain, before any writes.
if [[ "${chain}" == contracts || "${chain}" == bindings || "${chain}" == public-json ]]; then
  require_package grpcio-tools 1.80.0
fi
if [[ "${chain}" == contracts || "${chain}" == public-json || "${chain}" == policy ]]; then
  require_package PyYAML 6.0.3
fi
if [[ "${chain}" == contracts || "${chain}" == bindings ]]; then
  require_version protoc-gen-go "protoc-gen-go v1.36.6"
  require_version protoc-gen-go-grpc "protoc-gen-go-grpc 1.5.1"
fi
if [[ "${chain}" == contracts || "${chain}" == public-json || "${chain}" == policy ]] && ! command -v gofmt >/dev/null 2>&1; then
  echo "missing formatter: gofmt" >&2
  exit 1
fi
if [[ "${chain}" == contracts || "${chain}" == bindings || "${chain}" == public-json ]]; then
  protoc_version="$("${py}" -m grpc_tools.protoc --version)"
  if [[ "${protoc_version}" != "libprotoc 31.1" ]]; then
    echo "protoc version mismatch: expected libprotoc 31.1, got ${protoc_version}" >&2
    exit 1
  fi
fi

if [[ "${chain}" == contracts || "${chain}" == bindings ]]; then
  genv="$(dirname "$("${py}" -c 'import grpc_tools,sys;sys.stdout.write(grpc_tools.__file__)')")/_proto"
  gen_go="$(command -v protoc-gen-go)"
  gen_grpc="$(command -v protoc-gen-go-grpc)"
  "${py}" -m grpc_tools.protoc \
    -I"${here}" \
    -I"${genv}" \
    --plugin=protoc-gen-go="${gen_go}" \
    --plugin=protoc-gen-go-grpc="${gen_grpc}" \
    --go_out="${out}" --go_opt=module=opl-cloud/packages/contracts/go \
    --go-grpc_out="${out}" --go-grpc_opt=module=opl-cloud/packages/contracts/go \
    "${here}/internal.proto"
fi
if [[ "${chain}" == contracts || "${chain}" == public-json ]]; then
  "${py}" "${here}/generate_public_json_shape.py"
fi
if [[ "${chain}" == event-identity ]]; then
  "${py}" "${here}/generate_event_identity.py" "${here}/events.json" "${out}/event_identity.go"
fi
if [[ "${chain}" == policy ]]; then
  # One existing generator emits both the CloudIdentity policy and BFF statuses.
  "${py}" "${root}/services/gateway-integration/identity/generate_policy.py"
fi
