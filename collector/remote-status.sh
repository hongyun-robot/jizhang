#!/usr/bin/env bash
set -euo pipefail
cd /srv/jizhang
run_id="${1:?run id required}"
[[ "$run_id" =~ ^[0-9]{8}t[0-9]{6}z$ ]] || exit 64
probe_dir="/srv/jizhang/.wealth-probe-$run_id"
for file in pull.log build.log smoke.json smoke-stderr.txt smoke-exit-code.txt containers-after.txt; do
  if [[ -f "$probe_dir/$file" ]]; then
    printf 'file=%s\n' "$file"
    if [[ "$file" == 'smoke-stderr.txt' ]]; then head -n 24 "$probe_dir/$file"; else tail -n 12 "$probe_dir/$file"; fi
  fi
done
df -Pm /srv/jizhang
docker ps --filter 'name=^jizhang-' --format '{{.Names}} {{.Status}}'
