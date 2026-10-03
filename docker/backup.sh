#!/bin/sh
# 每日备份：mysqldump 全库 → gzip → 本机保留 KEEP_DAYS 天 → rclone 推送 OSS。
# 状态写入 $DIR/last-backup.json，由 /api/v1/health 暴露（任务 11.3：
# 备份失败无人知晓 → health 暴露最近备份时间与错误）。
#
# 用法：sh docker/backup.sh [输出目录]      （默认 ./backups，可用 BACKUP_DIR 覆盖）
# 保留：KEEP_DAYS 天（默认 30）
# OSS： 设置 RCLONE_REMOTE（如 jizhang-oss-crypt:  表示 crypt 远端根，前缀已在
#       rclone 配置里）即启用推送；未设置则仅本机备份（push=skipped）。
#       凭据/密码在 rclone 配置文件（~/.config/rclone/rclone.conf），不进 git。
# cron：0 3 * * * cd /srv/jizhang && sh docker/backup.sh >> backups/backup.log 2>&1
set -u

DIR="${1:-${BACKUP_DIR:-./backups}}"
KEEP_DAYS="${KEEP_DAYS:-30}"
STAMP="$(date +%Y%m%d-%H%M%S)"
RAW="$DIR/jizhang-$STAMP.sql"
FILE="$RAW.gz"
STATUS="$DIR/last-backup.json"
PUSH_REMOTE="${RCLONE_REMOTE:-}"
RCLONE_BIN="${RCLONE_BIN:-rclone}"

mkdir -p "$DIR"

now_iso() { date +%Y-%m-%dT%H:%M:%S%z; }

# 上一次成功时间（状态文件可能不存在 → 空）
prev_success=""
if [ -f "$STATUS" ]; then
  prev_success=$(sed -n 's/.*"last_success_at": *"\([^"]*\)".*/\1/p' "$STATUS" | head -1)
fi

# write_status <ok:true|false> <push:ok|failed|skipped> <error> <file> <size> <success_at>
write_status() {
  esc=$(printf '%s' "$3" | tr '\n\r\t' '   ' | sed 's/\\/\\\\/g; s/"/\\"/g')
  tmp="$STATUS.tmp"
  cat > "$tmp" <<EOF
{
  "last_run_at": "$(now_iso)",
  "last_success_at": "$6",
  "ok": $1,
  "file": "$4",
  "size": "$5",
  "push": "$2",
  "error": "$esc"
}
EOF
  mv "$tmp" "$STATUS"
}

fail() { # fail <msg>：dump 阶段失败，保留上次成功时间
  write_status false skipped "$1" "" "" "$prev_success"
  echo "backup failed: $1" >&2
  exit 1
}

# ---- 1. dump（docker exec 失败即退出，不产生假备份）----
if ! docker exec jizhang-mysql sh -c \
  'exec mysqldump --single-transaction --routines --triggers -uroot -p"$MYSQL_ROOT_PASSWORD" "$MYSQL_DATABASE"' \
  > "$RAW" 2>/dev/null; then
  rm -f "$RAW"
  fail "mysqldump failed"
fi
[ -s "$RAW" ] || { rm -f "$RAW"; fail "empty dump"; }

# ---- 2. gzip（-f：同秒重复运行会撞名，强制覆盖不产生假失败）----
gzip -f "$RAW" || { rm -f "$RAW"; fail "gzip failed"; }
[ -s "$FILE" ] || fail "gzip produced empty file"

# ---- 3. 清理过期（mtime 超过 KEEP_DAYS 天）+ 残留的未压缩 .sql ----
find "$DIR" -name 'jizhang-*.sql.gz' -mtime "+$KEEP_DAYS" -delete 2>/dev/null || true
find "$DIR" -name 'jizhang-*.sql' -mtime "+1" -delete 2>/dev/null || true

SIZE=$(du -h "$FILE" | cut -f1)
NAME=$(basename "$FILE")

# ---- 4. rclone 推送 OSS（配置了才推；推失败 = 整体失败，health 可见）----
push=skipped
push_err=""
if [ -n "$PUSH_REMOTE" ]; then
  if ! command -v "$RCLONE_BIN" >/dev/null 2>&1; then
    push=failed
    push_err="rclone not installed"
  else
    # --s3-no-check-bucket：OSS 的 HeadBucket 行为会让 rclone 误判 bucket 不存在
    # 从而走 CreateBucket（409 BucketAlreadyExists），跳过该检查
    if out=$("$RCLONE_BIN" copy --retries 2 --s3-no-check-bucket "$FILE" "$PUSH_REMOTE" 2>&1); then
      push=ok
    else
      push=failed
      push_err="rclone push failed: $(printf '%s' "$out" | tr '\n\r' '  ' | tail -c 300)"
    fi
  fi
fi

if [ "$push" = "failed" ]; then
  write_status false "$push" "$push_err" "$NAME" "$SIZE" "$prev_success"
  echo "backup push failed: $push_err" >&2
  exit 1
fi

write_status true "$push" "" "$NAME" "$SIZE" "$(now_iso)"
echo "backup ok: $FILE ($SIZE) push=$push"
