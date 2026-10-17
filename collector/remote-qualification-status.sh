#!/usr/bin/env bash
set -euo pipefail
run_id="${1:?run id required}"
[[ "$run_id" =~ ^[0-9]{8}t[0-9]{6}z$ ]] || exit 64
container="jizhang-wealth-qualification-$run_id"
docker inspect --format '{{.Name}} status={{.State.Status}} restarts={{.RestartCount}} image={{.Image}}' "$container"
docker exec "$container" node -e 'const s=JSON.parse(require("fs").readFileSync("/evidence/state.json","utf8")); delete s.baseline; console.log(JSON.stringify(s,null,2))'
docker logs --tail 15 "$container"
docker ps --filter 'name=^jizhang-' --format '{{.Names}} {{.Status}}'
