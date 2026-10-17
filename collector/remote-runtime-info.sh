#!/usr/bin/env bash
set -euo pipefail
cd /srv/jizhang
run_id="${1:?run id required}"
[[ "$run_id" =~ ^[0-9]{8}t[0-9]{6}z$ ]] || exit 64
docker run --rm --read-only --memory=128m --pids-limit=32 \
  --tmpfs /tmp:size=16m,mode=1777 --tmpfs /home/pwuser:size=16m,uid=1001,gid=1001 \
  --entrypoint node "jizhang-wealth-probe:$run_id" --input-type=module -e '
import os from "node:os";import fs from "node:fs";
const folders=[os.homedir(),"/home/pwuser","/tmp"].map(path=>{let writable=false;try{fs.accessSync(path,fs.constants.W_OK);writable=true;}catch{}return {path,writable,uid:fs.statSync(path).uid,mode:fs.statSync(path).mode.toString(8)};});
console.log(JSON.stringify({uid:process.getuid(),gid:process.getgid(),home:process.env.HOME,config:process.env.XDG_CONFIG_HOME,cache:process.env.XDG_CACHE_HOME,folders}));'
