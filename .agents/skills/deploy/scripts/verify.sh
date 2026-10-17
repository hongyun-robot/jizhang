#!/usr/bin/env bash
# 远端：部署后验证。四项全绿才判通过。
# 用法: bash verify.sh <服务器仓库根目录> <站点基址>
set -u

ROOT="${1:-/srv/jizhang}"
BASE="${2:-https://jizhang.zhanghongyun.cn}"
LIST=/tmp/jizhang_hashes.txt
fail=0

ck() { # ck <描述> <实际> <期望>
  if [ "$2" = "$3" ]; then echo "  PASS  $1 [$2]"
  else echo "  FAIL  $1 实际=[$2] 期望=[$3]"; fail=$((fail + 1)); fi
}

echo "===== 1. 源码同步（目标 0 不同 / 0 缺失）====="
if [ -f "$LIST" ]; then
  tr -d '\r' < "$LIST" > /tmp/_vlist
  same=0; changed=0; missing=0
  while read -r md5 rel; do
    [ -z "$rel" ] && continue
    f="$ROOT/$rel"
    if [ ! -f "$f" ]; then missing=$((missing + 1)); echo "      A $rel"; continue; fi
    if [ "$(md5sum "$f" | awk '{print $1}')" = "$md5" ]; then
      same=$((same + 1))
    else
      changed=$((changed + 1)); echo "      M $rel"
    fi
  done < /tmp/_vlist
  rm -f /tmp/_vlist
  echo "      一致=$same"
  ck "内容不同" "$changed" "0"
  ck "服务器缺失" "$missing" "0"
else
  echo "  SKIP  无哈希清单（这一步只在 diff 之后才有意义）"
fi

echo "===== 2. 容器健康 ====="
for c in jizhang-web jizhang-api jizhang-mysql jizhang-redis; do
  s=$(docker inspect --format '{{.State.Status}}/{{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}}' "$c" 2>/dev/null || echo missing)
  ck "$c" "$s" "running/healthy"
done
echo "  本机其余容器（不属本项目，仅确认没被误伤）:"
docker ps --format '       {{.Names}}  {{.Status}}' | grep -v '^       jizhang-' || true

echo "===== 3. HTTP ====="
for p in / /api/v1/health; do
  ck "$p" "$(curl -s -o /dev/null -m 20 -w '%{http_code}' "$BASE$p")" "200"
done

echo "===== 4. 线上 index.html 引用的 favicon 必须都取得到 ====="
curl -s "$BASE/" | grep -o 'href="/favicon[^"]*"' | sed 's/^href="//; s/"$//' > /tmp/_fav
if [ -s /tmp/_fav ]; then
  while read -r f; do
    [ -z "$f" ] && continue
    code=$(curl -s -o /dev/null -m 20 -w '%{http_code}' "$BASE$f")
    ct=$(curl -s -o /dev/null -m 20 -w '%{content_type}' "$BASE$f")
    ck "$f" "$code/$ct" "200/$(case "$f" in *.svg) echo image/svg+xml;; *.png) echo image/png;; *.webp) echo image/webp;; *.jpg|*.jpeg) echo image/jpeg;; *) echo image/x-icon;; esac)"
  done < /tmp/_fav
else
  echo "  SKIP  index.html 未引用 favicon"
fi
rm -f /tmp/_fav

echo "===== 5. 磁盘 ====="
df -h / | tail -1

echo
if [ "$fail" -eq 0 ]; then echo "VERIFY: ALL_GREEN"
else echo "VERIFY: $fail FAILED"; exit 1; fi
