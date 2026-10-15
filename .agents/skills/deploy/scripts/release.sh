#!/usr/bin/env bash
# Build before pausing writes. Validate pending migrations on a private restore.
set -euo pipefail
ROOT="${1:?project root required}"
LATEST="${2:?latest migration required}"
shift 2
services=("$@")
cd "$ROOT"
[[ "$ROOT" == /srv/jizhang && "$LATEST" =~ ^[0-9]+$ ]] || exit 2
umask 077
backup=""
check_db=""
paused=0
initial_version=""
db_query() {
  docker exec -e RELEASE_QUERY="$1" jizhang-mysql sh -c 'MYSQL_PWD="$MYSQL_ROOT_PASSWORD" exec mysql -uroot -N -e "$RELEASE_QUERY" "$MYSQL_DATABASE"'
}
cleanup() {
  result=$?
  trap - EXIT
  if [[ -n "$check_db" && "$check_db" =~ ^jizhang_release_check_[0-9_]+$ ]]; then
    db_query "DROP DATABASE IF EXISTS \`$check_db\`" >/dev/null || true
  fi
  if (( result != 0 && paused == 1 )); then
    current=$(db_query 'SELECT CONCAT(version,":",dirty) FROM schema_migrations' 2>/dev/null || true)
    if [[ "$current" == "$initial_version:0" ]]; then
      docker start jizhang-api >/dev/null || true
      echo 'RELEASE: previous API resumed; live schema unchanged' >&2
    else
      echo 'RELEASE: schema changed; preserve backup and inspect migration before recovery' >&2
    fi
  fi
  exit "$result"
}
trap cleanup EXIT

# A failed build leaves the original containers serving.
docker compose --profile app build "${services[@]}"
read -r initial_version dirty < <(db_query 'SELECT version,dirty FROM schema_migrations')
[[ "$dirty" == 0 ]] || { echo 'RELEASE: database migration is dirty' >&2; exit 1; }
if (( initial_version < LATEST )); then
  echo "RELEASE: migration $initial_version -> $LATEST; pause API writes and workers"
  docker compose --profile app stop api
  paused=1
  stamp=$(date +%Y%m%d-%H%M%S)
  mkdir -p backups
  backup="backups/jizhang-pre-release-$stamp-$$.sql.gz"
  docker exec jizhang-mysql sh -c 'MYSQL_PWD="$MYSQL_ROOT_PASSWORD" exec mysqldump --single-transaction --routines --triggers --set-gtid-purged=OFF -uroot "$MYSQL_DATABASE"' | gzip > "$backup"
  gzip -t "$backup"
  [[ -s "$backup" ]] || exit 1
  echo "RELEASE: backup $ROOT/$backup ($(stat -c %s "$backup") bytes)"

  check_db="jizhang_release_check_$(date +%Y%m%d%H%M%S)_$$"
  db_query "CREATE DATABASE \`$check_db\` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci" >/dev/null
  gzip -dc "$backup" | docker exec -i -e CHECK_DB="$check_db" jizhang-mysql sh -c 'MYSQL_PWD="$MYSQL_ROOT_PASSWORD" exec mysql -uroot "$CHECK_DB"'
  mapfile -t tables < <(db_query "SELECT TABLE_NAME FROM information_schema.tables WHERE TABLE_SCHEMA='$check_db' AND TABLE_TYPE='BASE TABLE' AND TABLE_NAME NOT IN ('users','voucher_platforms','schema_migrations') ORDER BY TABLE_NAME")
  financial_hash() {
    docker exec -e CHECK_DB="$check_db" jizhang-mysql sh -c 'MYSQL_PWD="$MYSQL_ROOT_PASSWORD" exec mysqldump --skip-comments --compact --no-create-info --skip-extended-insert --order-by-primary --set-gtid-purged=OFF -uroot "${CHECK_DB:-$MYSQL_DATABASE}" "$@"' sh "${tables[@]}" | sha256sum | cut -d' ' -f1
  }
  before=$(financial_hash)
  log="backups/jizhang-release-check-$stamp-$$.log"
  migrate_restore() {
    docker compose --profile app run --rm --no-deps -e DB_NAME="$check_db" --entrypoint /bin/sh api -c 'exec ./migrate -path ./migrations -database "mysql://${DB_USER}:${DB_PASSWORD}@tcp(${DB_HOST}:${DB_PORT})/${DB_NAME}" "$@"' sh "$@" >>"$log" 2>&1
  }
  if ! migrate_restore up; then
    echo "RELEASE: restore migration failed; inspect $ROOT/$log" >&2
    exit 1
  fi
  after=$(financial_hash)
  [[ "$before" == "$after" ]] || { echo 'RELEASE: restore financial data changed' >&2; exit 1; }
  restored=$(db_query "SELECT CONCAT(version,':',dirty) FROM \`$check_db\`.schema_migrations")
  [[ "$restored" == "$LATEST:0" ]] || exit 1
  if ! migrate_restore goto "$initial_version"; then
    echo "RELEASE: restore rollback failed; inspect $ROOT/$log" >&2
    exit 1
  fi
  [[ "$(db_query "SELECT CONCAT(version,':',dirty) FROM \`$check_db\`.schema_migrations")" == "$initial_version:0" ]] || exit 1
  [[ "$before" == "$(financial_hash)" ]] || { echo 'RELEASE: restore rollback financial data changed' >&2; exit 1; }
  if ! migrate_restore up; then
    echo "RELEASE: restore replay failed; inspect $ROOT/$log" >&2
    exit 1
  fi
  [[ "$(db_query "SELECT CONCAT(version,':',dirty) FROM \`$check_db\`.schema_migrations")" == "$LATEST:0" ]] || exit 1
  [[ "$before" == "$(financial_hash)" ]] || { echo 'RELEASE: restore replay financial data changed' >&2; exit 1; }
  db_query "DROP DATABASE \`$check_db\`" >/dev/null
  check_db=""
  echo 'RELEASE: isolated restore up/down/up passed; original financial tables unchanged'
  [[ "$before" == "$(financial_hash)" ]] || { echo 'RELEASE: live financial data changed while API paused' >&2; exit 1; }
  if ! docker compose --profile app run --rm --no-deps --entrypoint /bin/sh api -c 'exec ./migrate -path ./migrations -database "mysql://${DB_USER}:${DB_PASSWORD}@tcp(${DB_HOST}:${DB_PORT})/${DB_NAME}" up' >>"$log" 2>&1; then
    echo "RELEASE: live migration failed; inspect $ROOT/$log" >&2
    exit 1
  fi
  [[ "$(db_query 'SELECT CONCAT(version,":",dirty) FROM schema_migrations')" == "$LATEST:0" ]] || exit 1
  [[ "$before" == "$(financial_hash)" ]] || { echo 'RELEASE: live migration financial data changed' >&2; exit 1; }
  echo 'RELEASE: live migration passed; original financial tables unchanged'
fi
docker compose --profile app up -d --no-build --wait --wait-timeout 180 "${services[@]}"
paused=0
echo "RELEASE: schema $(db_query 'SELECT CONCAT(version,":",dirty) FROM schema_migrations')"
if [[ -n "$backup" ]]; then echo "RELEASE: verified backup $ROOT/$backup"; fi
