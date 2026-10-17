#!/usr/bin/env bash
# Reads a verified official-image archive from stdin; keeps no large server tar.
set -euo pipefail
cd /srv/jizhang
free_mib=$(df -Pm /srv/jizhang | awk 'NR==2 {print $4}')
(( free_mib >= 5000 )) || { printf 'image load refused: disk reserve\n'; exit 66; }
printf 'phase=load-official-image\n'
timeout 300s docker load
expected='sha256:2c1f4e0fd6450f43ddb46d60c2a6df30855a8588e165b1f2559fb0eda8d7ff35'
actual=$(docker image inspect --format '{{.Id}}' mcr.microsoft.com/playwright:i-was-a-digest)
[[ "$actual" == "$expected" ]] || { printf 'image config digest mismatch\n'; exit 67; }
docker tag "$actual" mcr.microsoft.com/playwright:v1.63.0-noble
docker image rm mcr.microsoft.com/playwright:i-was-a-digest
printf 'verified_image_config=%s\n' "$actual"
df -Pm /srv/jizhang
