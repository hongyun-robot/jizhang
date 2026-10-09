#!/usr/bin/env bash
# 远端：比对本地哈希清单与仓库实际文件，产出待同步清单。
# 用法: bash diff.sh <服务器仓库根目录>
# 输入: /tmp/jizhang_hashes.txt   格式 "md5  相对路径"，每行一条
# 输出: stdout 摘要 + /tmp/jizhang_changes.txt（"M 路径" 或 "A 路径"）
set -u

ROOT="${1:-/srv/jizhang}"
LIST=/tmp/jizhang_hashes.txt
CHANGES=/tmp/jizhang_changes.txt

[ -f "$LIST" ] || { echo "找不到哈希清单 $LIST"; exit 2; }

# Windows 侧生成的清单可能带 CRLF。路径末尾残留的 \r 会让每个文件都判成"不存在"，
# 表现为 183/183 全部缺失——所以这里无条件先去掉。
tr -d '\r' < "$LIST" > "$LIST.lf" && mv "$LIST.lf" "$LIST"

same=0; changed=0; missing=0
: > "$CHANGES"

while read -r md5 rel; do
  [ -z "$rel" ] && continue
  f="$ROOT/$rel"
  if [ ! -f "$f" ]; then
    missing=$((missing + 1))
    printf 'A %s\n' "$rel" >> "$CHANGES"
    continue
  fi
  if [ "$(md5sum "$f" | awk '{print $1}')" = "$md5" ]; then
    same=$((same + 1))
  else
    changed=$((changed + 1))
    printf 'M %s\n' "$rel" >> "$CHANGES"
  fi
done < "$LIST"

# ---------- 孤儿：本地已删、服务器还留着 ----------
# 不报的话，删掉的源文件会经 `COPY . .` 继续进 Docker 构建上下文，
# 作用域规则（exclude）与本地共用同一份 /tmp/jizhang_exclude.txt，避免两侧规则漂移。
ORPHANS=/tmp/jizhang_orphans.txt
: > "$ORPHANS"
orphans=0

if [ -f /tmp/jizhang_roots.txt ]; then
  tr -d '\r' < /tmp/jizhang_roots.txt > /tmp/_roots
  tr -d '\r' < "$LIST" > /tmp/_list_lf
  # 清单行 "md5  path" -> 纯路径，便于整行精确匹配
  awk '{ $1=""; sub(/^ +/, ""); print }' /tmp/_list_lf > /tmp/_paths
  EXCLUDE=$(tr -d '\r' < /tmp/jizhang_exclude.txt 2>/dev/null || true)

  while read -r r; do
    [ -z "$r" ] && continue
    [ -d "$ROOT/$r" ] || continue
    find "$ROOT/$r" -type f -printf '%P\n' 2>/dev/null |
    while IFS= read -r p; do
      rel="$r/$p"
      case "$EXCLUDE" in "") ;; *) printf '%s\n' "$rel" | grep -qE "$EXCLUDE" && continue ;; esac
      grep -qxF "$rel" /tmp/_paths || printf '%s\n' "$rel"
    done
  done < /tmp/_roots >> "$ORPHANS"

  rm -f /tmp/_roots /tmp/_list_lf /tmp/_paths
  orphans=$(wc -l < "$ORPHANS")
  if [ "$orphans" -gt 0 ]; then
    while IFS= read -r o; do
      [ -n "$o" ] && printf 'D %s\n' "$o"
    done < "$ORPHANS" | head -10
    [ "$orphans" -gt 10 ] && echo "D ... 其余 $((orphans - 10)) 个"
  fi
fi

# 注：以下计数只统计清单内的路径；node_modules / dist / shots / preview 这类作用域外文件两侧都不看
echo "比对: 一致=$same  内容不同=$changed  服务器缺失=$missing  孤儿=$orphans  待同步=$((changed + missing))"
if [ "$changed" -eq 0 ] && [ "$missing" -eq 0 ] && [ "$orphans" -eq 0 ]; then
  echo "DIFF: NO_CHANGES"
else
  echo "DIFF: HAS_CHANGES"
fi
