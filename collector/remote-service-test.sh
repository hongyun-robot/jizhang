#!/usr/bin/env bash
set -euo pipefail
cd /srv/jizhang
run_id="${1:?run id required}"
[[ "$run_id" =~ ^[0-9]{8}t[0-9]{6}z$ ]] || exit 64
probe_dir="/srv/jizhang/.wealth-probe-$run_id"
image="jizhang-wealth-probe:$run_id"
container="jizhang-wealth-service-$run_id"
network="jizhang-wealth-net-$run_id"
timeout_container="jizhang-wealth-timeout-$run_id"
[[ -d "$probe_dir" ]] || exit 65
docker image inspect "$image" >/dev/null
umask 077
python3 - "$probe_dir/private.env" <<'PY'
import os,secrets,sys
with open(os.open(sys.argv[1],os.O_WRONLY|os.O_CREAT|os.O_EXCL,0o600),'w') as f:
    f.write('WEALTH_COLLECTOR_KEY='+secrets.token_hex(32)+'\n')
PY
cleanup() {
  docker rm -f "$container" >/dev/null 2>&1 || true
  docker rm -f "$timeout_container" >/dev/null 2>&1 || true
  docker network rm "$network" >/dev/null 2>&1 || true
  rm -f -- "$probe_dir/private.env"
}
trap cleanup EXIT
export WEALTH_COLLECTOR_KEY
WEALTH_COLLECTOR_KEY=$(python3 - "$probe_dir/private.env" <<'PY'
import sys
print(open(sys.argv[1]).read().strip().split('=',1)[1])
PY
)
docker compose --project-directory "$probe_dir" -f /srv/jizhang/docker-compose.yml \
  -f "$probe_dir/docker-compose.wealth.yml" --profile wealth config --quiet
printf 'compose_config=valid\n'
docker network create --label "jizhang.wealth.probe=$run_id" "$network" >/dev/null
docker run -d --name "$container" --label "jizhang.wealth.probe=$run_id" \
  --network "$network" --network-alias wealth-collector \
  --init --read-only --memory=768m --cpus=0.75 --pids-limit=256 --shm-size=256m \
  --cap-drop=ALL --security-opt=no-new-privileges:true \
  --security-opt="seccomp=$probe_dir/docker/playwright-seccomp.json" \
  --tmpfs /tmp:size=256m,mode=1777 --tmpfs /home/pwuser:size=64m,uid=1001,gid=1001 \
  --env-file "$probe_dir/private.env" "$image" >/dev/null
timeout 40s docker exec -i "$container" node --input-type=module <<'JS'
for(let i=0;i<25;i++) {
 try { const r=await fetch('http://127.0.0.1:8091/health'); if(r.ok)process.exit(0); }catch{}
 await new Promise(r=>setTimeout(r,1000));
}
process.exit(1);
JS
timeout 15s docker run --rm -i --network "$network" --read-only --cap-drop=ALL --memory=128m --pids-limit=32 \
  --entrypoint node "$image" --input-type=module <<'JS'
const response=await fetch('http://wealth-collector:8091/health',{signal:AbortSignal.timeout(5000)});
if(response.status!==200)process.exit(1);
console.log('isolated_network_health=200');
JS
timeout 80s docker exec -i "$container" node --input-type=module > "$probe_dir/service-result.json" <<'JS'
import assert from 'node:assert/strict';
import {writeFile} from 'node:fs/promises';
const base='http://127.0.0.1:8091';
const headers={authorization:'Bearer '+process.env.WEALTH_COLLECTOR_KEY};
const get=(path,options={})=>fetch(base+path,{signal:AbortSignal.timeout(65000),...options});
const health=await get('/health'); assert.equal(health.status,200);
const unauth=await get('/v1/products/J07241/nav',{method:'POST'}); assert.equal(unauth.status,401);
const unknown=await get('/v1/products/J07242/nav',{method:'POST',headers}); assert.equal(unknown.status,400);
const body=await get('/v1/products/J07241/nav',{method:'POST',headers,body:'not-accepted'}); assert.equal(body.status,400);
const source=await get('/v1/products/J07241/nav',{method:'POST',headers});
assert([200,502].includes(source.status));
const result=await source.json();
if(source.status===200)await writeFile('/tmp/wealth-public-envelope.json',JSON.stringify(result));
const cooldown=await get('/v1/products/J07241/nav',{method:'POST',headers});assert.equal(cooldown.status,429);
console.log(JSON.stringify({health:health.status,unauthenticated:unauth.status,unknown_product:unknown.status,unexpected_body:body.status,live_source_status:source.status,live_source_error:result.error??null,live_source_count:result.response?.data?.length??0,cooldown:cooldown.status}));
JS
if python3 - "$probe_dir/service-result.json" <<'PY'
import json,sys
sys.exit(0 if json.load(open(sys.argv[1]))['live_source_status']==200 else 1)
PY
then
  docker exec "$container" cat /tmp/wealth-public-envelope.json > "$probe_dir/public-envelope.json"
fi
docker logs "$container" > "$probe_dir/service-log.txt" 2>&1
if test -s "$probe_dir/service-log.txt"; then cat "$probe_dir/service-log.txt"; fi
docker restart "$container" >/dev/null
timeout 40s docker exec -i "$container" node --input-type=module <<'JS'
for(let i=0;i<25;i++) {
 try { const r=await fetch('http://127.0.0.1:8091/health'); if(r.ok){console.log('service_restart_health=200');process.exit(0);} }catch{}
 await new Promise(r=>setTimeout(r,1000));
}
process.exit(1);
JS
timeout 40s docker run --rm -i --name "$timeout_container" --label "jizhang.wealth.probe=$run_id" \
  --network=none --init --read-only --memory=768m --cpus=0.75 --pids-limit=256 --shm-size=256m \
  --cap-drop=ALL --security-opt=no-new-privileges:true \
  --security-opt="seccomp=$probe_dir/docker/playwright-seccomp.json" \
  --tmpfs /tmp:size=256m,mode=1777 --tmpfs /home/pwuser:size=64m,uid=1001,gid=1001 \
  --entrypoint node "$image" --input-type=module <<'JS'
import assert from 'node:assert/strict';
import {collect,launch,disposeBrowser} from './collector.mjs';
const browser=await launch();const started=Date.now();
try {
 await assert.rejects(collect(browser,'J07241',{timeoutMs:1}),e=>e.code==='collection_timeout');
 assert(Date.now()-started<6000);
} finally {await disposeBrowser(browser);}
assert.equal(browser.isConnected(),false);
console.log('timeout_browser_recycled=true');
JS
cat "$probe_dir/service-result.json"
docker inspect --format 'port_bindings={{json .HostConfig.PortBindings}} readonly={{.HostConfig.ReadonlyRootfs}} memory_limit={{.HostConfig.Memory}}' "$container"
printf 'evidence_dir=%s\n' "$probe_dir"
