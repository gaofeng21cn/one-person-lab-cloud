#!/usr/bin/env bash
# Regenerate the Go bindings for the Cloud internal API from the production proto source.
#
# Pinned tools:
#   grpcio-tools 1.80.0      -> libprotoc 31.1
#   protoc-gen-go v1.36.6
#   protoc-gen-go-grpc v1.5.1
#
# The proto source carries the logical go_package `opl-cloud/packages/contracts/go/api;api`,
# which maps directly onto the existing shared contracts module. The generated package is
# a package inside `packages/contracts/go`, not another Go module.
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
root="$(cd "${here}/../../.." && pwd)"
out="${root}/packages/contracts/go"

py="${PYTHON:-python3}"

if ! command -v "${py}" >/dev/null 2>&1; then
  echo "missing Python: ${py}" >&2
  exit 1
fi
"${py}" - <<'PY'
from importlib.metadata import PackageNotFoundError, version
for package, expected in (("grpcio-tools", "1.80.0"), ("PyYAML", "6.0.3")):
    try:
        actual = version(package)
    except PackageNotFoundError:
        raise SystemExit(f"missing {package}=={expected}; install docs/spec/target/checks/requirements.txt")
    if actual != expected:
        raise SystemExit(f"{package} version mismatch: expected {expected}, got {actual}")
PY

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

require_version protoc-gen-go "protoc-gen-go v1.36.6"
require_version protoc-gen-go-grpc "protoc-gen-go-grpc 1.5.1"
if ! command -v gofmt >/dev/null 2>&1; then
  echo "missing formatter: gofmt" >&2
  exit 1
fi
protoc_version="$("${py}" -m grpc_tools.protoc --version)"
if [[ "${protoc_version}" != "libprotoc 31.1" ]]; then
  echo "protoc version mismatch: expected libprotoc 31.1, got ${protoc_version}" >&2
  exit 1
fi

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

# Public JSON enum names and required defaults are owned by the publisher schema.
"${py}" "${here}/generate_public_json_shape.py"
