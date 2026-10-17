#!/usr/bin/env bash
set -euo pipefail
run_id="${1:?run id required}"
[[ "$run_id" =~ ^[0-9]{8}t[0-9]{6}z$ ]] || exit 64
container="jizhang-wealth-qualification-$run_id"
[[ "$(docker inspect --format '{{index .Config.Labels "jizhang.wealth.qualification"}}' "$container")" == "$run_id" ]] || exit 65
docker exec "$container" node -e 'const fs=require("fs"),p="/evidence/state.json",s=JSON.parse(fs.readFileSync(p));s.status="failed";s.aborted_at=new Date().toISOString();s.abort_reason="Restart precheck failed; preserved before a new versioned qualification run";fs.writeFileSync(p,JSON.stringify(s,null,2)+"\n");'
docker stop --time 15 "$container"
