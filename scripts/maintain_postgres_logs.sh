#!/usr/bin/env bash
set -euo pipefail
umask 077
task_dir=$(cd -- "$(dirname -- "$0")" && pwd)
exec 9>"$task_dir/postgres-log-maintenance.lock"
flock -n 9 || exit 0
trap 'logger -p user.err -t postgres-log-maintenance "Maintenance failed; check the maintenance journal"' ERR
docker exec -i -u postgres postgres-dev sh -s < "$task_dir/compress_postgres_logs.sh"
date -u +%FT%TZ > "$task_dir/postgres-log-maintenance.last-success"
# Expired logs are intentionally retained until off-host archival is configured
# and the remote copy has been verified. Compression alone never drops evidence.
