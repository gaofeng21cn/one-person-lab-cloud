#!/usr/bin/env bash
set -euo pipefail
exec node "$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)/install.mjs" "$@"
