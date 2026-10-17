#!/usr/bin/env bash
# Isolated, public read-only probe. Never runs the production Compose stack.
set -euo pipefail
cd /srv/jizhang
run_id="${1:?run id required}"
[[ "$run_id" =~ ^[0-9]{8}t[0-9]{6}z$ ]] || exit 64
probe_dir="/srv/jizhang/.wealth-probe-$run_id"
bundle="/tmp/jizhang-wealth-$run_id.tar.gz"
image="jizhang-wealth-probe:$run_id"
container="jizhang-wealth-probe-$run_id"
[[ -f "$bundle" && ! -e "$probe_dir" ]] || exit 65
free_mib=$(df -Pm /srv/jizhang | awk 'NR==2 {print $4}')
if docker image inspect mcr.microsoft.com/playwright:v1.63.0-noble >/dev/null 2>&1; then
  (( free_mib >= 1800 )) || { printf 'probe refused: disk reserve reached\n'; exit 66; }
else
  (( free_mib >= 5500 )) || { printf 'probe refused: less than 5500 MiB free\n'; exit 66; }
fi
umask 077
mkdir "$probe_dir"
tar -xzf "$bundle" -C "$probe_dir"
cleanup() {
  docker rm -f "$container" >/dev/null 2>&1 || true
  docker ps --filter 'name=^jizhang-' --format '{{.Names}} {{.Status}}' > "$probe_dir/containers-after.txt"
}
trap cleanup EXIT
docker ps --filter 'name=^jizhang-' --format '{{.Names}} {{.Status}}' > "$probe_dir/containers-before.txt"
if ! docker image inspect mcr.microsoft.com/playwright:v1.63.0-noble >/dev/null 2>&1; then
  printf 'phase=pull\n'
  if ! timeout 480s docker pull mcr.microsoft.com/playwright:v1.63.0-noble > "$probe_dir/pull.log" 2>&1; then
    tail -n 15 "$probe_dir/pull.log"
    exit 67
  fi
fi
free_mib=$(df -Pm /srv/jizhang | awk 'NR==2 {print $4}')
printf 'disk_free_mib_after_pull=%s\n' "$free_mib"
(( free_mib >= 1500 )) || { printf 'probe refused: disk reserve reached\n'; exit 68; }
printf 'phase=build\n'
if ! timeout 180s docker build --label "jizhang.wealth.probe=$run_id" -t "$image" "$probe_dir/collector" > "$probe_dir/build.log" 2>&1; then
  tail -n 20 "$probe_dir/build.log"
  exit 69
fi
printf 'phase=smoke\n'
set +e
timeout 75s docker run --name "$container" --label "jizhang.wealth.probe=$run_id" \
  --init --read-only --memory=768m --cpus=0.75 --pids-limit=256 --shm-size=256m \
  --cap-drop=ALL --security-opt=no-new-privileges:true \
  --security-opt="seccomp=$probe_dir/docker/playwright-seccomp.json" \
  --tmpfs /tmp:size=256m,mode=1777 --tmpfs /home/pwuser:size=64m,uid=1001,gid=1001 \
  -e WEALTH_LIVE_SMOKE=1 --entrypoint node "$image" smoke.mjs \
  > "$probe_dir/smoke.json" 2> "$probe_dir/smoke-stderr.txt"
status=$?
set -e
printf '%s\n' "$status" > "$probe_dir/smoke-exit-code.txt"
cat "$probe_dir/smoke.json"
if [[ -s "$probe_dir/smoke-stderr.txt" ]]; then tail -n 30 "$probe_dir/smoke-stderr.txt"; fi
printf 'smoke_exit_code=%s\n' "$status"
printf 'evidence_dir=%s\n' "$probe_dir"
exit "$status"
