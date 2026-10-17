#!/usr/bin/env bash
set -euo pipefail
cd /srv/jizhang
run_id="${1:?run id required}"
[[ "$run_id" =~ ^[0-9]{8}t[0-9]{6}z$ ]] || exit 64
probe_dir="/srv/jizhang/.wealth-probe-$run_id"
container="jizhang-wealth-challenge-$run_id"
image="jizhang-wealth-probe:$run_id"
test -d "$probe_dir"
cleanup() { docker rm -f "$container" >/dev/null 2>&1 || true; }
trap cleanup EXIT
timeout 80s docker run --name "$container" --label "jizhang.wealth.probe=$run_id" \
  --init --read-only --memory=768m --cpus=0.75 --pids-limit=256 --shm-size=256m \
  --cap-drop=ALL --security-opt=no-new-privileges:true \
  --security-opt="seccomp=$probe_dir/docker/playwright-seccomp.json" \
  --tmpfs /tmp:size=256m,mode=1777 --tmpfs /home/pwuser:size=64m,uid=1001,gid=1001 \
  -e WEALTH_LIVE_SMOKE=1 --entrypoint node "$image" challenge-diagnostic.mjs normal-ua --http-replay \
  > "$probe_dir/challenge-report.json" 2> "$probe_dir/challenge-stderr.txt"
python3 - "$probe_dir/challenge-report.json" <<'PY'
import json,sys
r=json.load(open(sys.argv[1]))
print(json.dumps({k:r.get(k) for k in ['mode','reached_product','nav_result','http_replay','direct_http']},ensure_ascii=False))
PY
