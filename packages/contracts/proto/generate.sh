#!/usr/bin/env bash
# Regenerate the v2.26 Go bindings from the production proto source.
#
# Pinned tools (see docs/spec/v2.26/01_domain_ownership_matrix.md §3.3):
#   grpcio-tools 1.80.0  -> libprotoc 31.1
#   protoc-gen-go v1.36.6
#   protoc-gen-go-grpc v1.5.1
#
# The spec proto carries the logical go_package `opl.cloud/contracts/v226;v226`.
# The generated package must live at opl-cloud/packages/contracts/go/v226, so the
# generator overrides it with a fixed protobuf import mapping instead of editing
# the message definitions or the specification bytes.
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
root="$(cd "${here}/../../.." && pwd)"
out="${root}/packages/contracts/go"

py="${PYTHON:-python3}"

if ! "${py}" -c "import grpc_tools" 2>/dev/null; then
  echo "grpc_tools missing: install the pinned tools from docs/spec/v2.26/checks/requirements.txt" >&2
  exit 1
fi
genv="$(dirname "$("${py}" -c 'import grpc_tools,sys;sys.stdout.write(grpc_tools.__file__)')")/_proto"

gen_go="$(command -v protoc-gen-go)"
gen_grpc="$(command -v protoc-gen-go-grpc)"
mapping="Minternal.proto=opl-cloud/packages/contracts/go/v226"

"${py}" -m grpc_tools.protoc \
  -I"${here}" \
  -I"${genv}" \
  --plugin=protoc-gen-go="${gen_go}" \
  --plugin=protoc-gen-go-grpc="${gen_grpc}" \
  --go_out="${out}" --go_opt="${mapping}" --go_opt="module=opl-cloud/packages/contracts/go" \
  --go-grpc_out="${out}" --go-grpc_opt="${mapping}" --go-grpc_opt="module=opl-cloud/packages/contracts/go" \
  "${here}/internal.proto"
