#!/usr/bin/env bash
# Starts only an isolated qualification worker. No application volumes or ports.
set -euo pipefail
run_id="${1:?run id required}"
[[ "$run_id" =~ ^[0-9]{8}t[0-9]{6}z$ ]] || exit 64
root="/srv/jizhang/.wealth-qualification-$run_id"
[[ ! -e "$root" && -f "/tmp/jizhang-wealth-$run_id.tar.gz" ]] || exit 65
(( $(df -Pm /srv/jizhang | awk 'NR==2 {print $4}') >= 1800 )) || exit 66
umask 077
mkdir "$root"
tar -xzf "/tmp/jizhang-wealth-$run_id.tar.gz" -C "$root"
docker ps --filter 'name=^jizhang-' --format '{{.Names}} {{.Status}}' > "$root/containers-before.txt"
timeout 180s docker build --label "jizhang.wealth.qualification=$run_id" -t "jizhang-wealth-qualification:$run_id" "$root/collector" > "$root/build.log" 2>&1
mkdir "$root/evidence"
sudo chown 1001:1001 "$root/evidence"
docker run -d --name "jizhang-wealth-qualification-$run_id" --label "jizhang.wealth.qualification=$run_id" \
 --init --restart=unless-stopped --read-only --memory=768m --cpus=0.75 --pids-limit=256 --shm-size=256m \
 --cap-drop=ALL --security-opt=no-new-privileges:true --security-opt="seccomp=$root/docker/playwright-seccomp.json" \
 --tmpfs /tmp:size=256m,mode=1777 --tmpfs /home/pwuser:size=64m,uid=1001,gid=1001 \
 --mount "type=bind,src=$root/evidence,dst=/evidence" \
 --log-driver=json-file --log-opt max-size=5m --log-opt max-file=2 --no-healthcheck \
 -e WEALTH_QUALIFICATION=1 -e "WEALTH_SOURCE_VERSION=$run_id" --entrypoint node \
 "jizhang-wealth-qualification:$run_id" qualification.mjs
printf 'qualification_evidence=%s/evidence\n' "$root"
docker ps --filter 'name=^jizhang-' --format '{{.Names}} {{.Status}}' > "$root/containers-after.txt"
