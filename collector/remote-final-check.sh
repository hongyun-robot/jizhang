#!/usr/bin/env bash
set -euo pipefail
cd /srv/jizhang
run_id="${1:-20261015t161510z}"
[[ "$run_id" =~ ^[0-9]{8}t[0-9]{6}z$ ]] || exit 64
test -z "$(docker ps -a --filter 'label=jizhang.wealth.probe' --format '{{.Names}}')"
test -z "$(docker network ls --filter 'label=jizhang.wealth.probe' --format '{{.Name}}')"
printf 'temporary_containers_and_networks_removed=true\n'
docker inspect --format '{{.Name}} started={{.State.StartedAt}} restarts={{.RestartCount}} health={{.State.Health.Status}}' \
  jizhang-web jizhang-api jizhang-redis jizhang-mysql
df -Pm /srv/jizhang
test ! -e "/srv/jizhang/.wealth-probe-$run_id/private.env"
printf 'temporary_credentials_removed=true\n'
