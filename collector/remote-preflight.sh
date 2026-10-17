#!/usr/bin/env bash
set -euo pipefail
cd /srv/jizhang
python3 - <<'PY'
import json, shutil, subprocess

def read(args):
    result = subprocess.run(args, text=True, capture_output=True, timeout=20)
    return {"exit_code": result.returncode, "output": result.stdout.strip()[:4000]}

print(json.dumps({
    "docker": read(["docker", "version", "--format", "{{.Server.Version}}"]),
    "disk_free_mib": shutil.disk_usage("/srv/jizhang").free // (1024 * 1024),
    "memory": read(["free", "-m"]),
    "jizhang_containers": read(["docker", "ps", "--filter", "name=^jizhang-", "--format", "{{.Names}} {{.Status}}"]),
    "playwright_images": read(["docker", "images", "mcr.microsoft.com/playwright", "--format", "{{.Repository}}:{{.Tag}} {{.Size}}"]),
}, ensure_ascii=False))
PY
