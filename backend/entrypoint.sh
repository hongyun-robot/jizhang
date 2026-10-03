#!/bin/sh
# 容器入口：等待数据库 → golang-migrate 幂等迁移 → 启动服务
set -eu

DSN="mysql://${DB_USER}:${DB_PASSWORD}@tcp(${DB_HOST}:${DB_PORT})/${DB_NAME}"

i=0
until ./migrate -path ./migrations -database "$DSN" up; do
  i=$((i + 1))
  if [ "$i" -ge 30 ]; then
    echo "[entrypoint] migrate failed after $i attempts" >&2
    exit 1
  fi
  echo "[entrypoint] waiting for database ($i)..."
  sleep 2
done

exec ./server
