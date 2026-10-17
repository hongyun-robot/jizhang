#!/usr/bin/env bash
set -euo pipefail
run_id="${1:?run id required}"
[[ "$run_id" =~ ^[0-9]{8}t[0-9]{6}z$ ]] || exit 64
timeout 65s docker exec -i "jizhang-wealth-qualification-$run_id" node --input-type=module < /tmp/identity-diagnostic.mjs
