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

if ! "${py}" -c "import grpc_tools" 2>/dev/null; then
  echo "grpc_tools missing: install the pinned tools from docs/spec/target/checks/requirements.txt" >&2
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
"${py}" "${here}/generate_publisher_shape.py"
