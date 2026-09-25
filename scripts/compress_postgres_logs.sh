#!/bin/sh
# Run as the PostgreSQL OS user. The caller must hold a single-worker lock.
set -eu
umask 077
log_dir=${1:-${PGDATA:?}/pg_log}
current_logs=${2:-${PGDATA:?}/current_logfiles}
min_age=${3:-3600}
[ -d "$log_dir" ] && [ -r "$current_logs" ]
now=$(date +%s)
count=0
saved=0
temporary=
trap '[ -z "$temporary" ] || rm -f "$temporary"' EXIT
trap 'exit 1' HUP INT TERM
for path in "$log_dir"/postgresql-*.log; do
    [ -f "$path" ] || continue
    name=${path##*/}
    # Consult the collector's authoritative list, including absolute paths.
    active=$(awk '{sub(/^.*\//, "", $2); print $2}' "$current_logs")
    if [ -z "$active" ]; then
        echo 'ERROR: current log manifest is empty; compression stopped' >&2
        exit 1
    fi
    if printf '%s\n' "$active" | grep -Fxq "$name"; then
        continue
    fi
    modified=$(stat -c %Y "$path")
    [ "$((now - modified))" -ge "$min_age" ] || continue
    [ -s "$path" ] || continue
    if [ -e "$path.gz" ]; then
        echo "ERROR: both original and archive exist: $name" >&2
        exit 1
    fi
    before=$(stat -c '%i:%s:%Y' "$path")
    bytes=$(stat -c %s "$path")
    temporary="$path.gz.partial"
    nice -n 19 ionice -c 3 gzip -1 -c "$path" > "$temporary"
    nice -n 19 ionice -c 3 gzip -t "$temporary"
    if [ "$before" != "$(stat -c '%i:%s:%Y' "$path")" ]; then
        echo "ERROR: source changed during compression: $name" >&2
        exit 1
    fi
    touch -r "$path" "$temporary"
    compressed=$(stat -c %s "$temporary")
    mv "$temporary" "$path.gz"
    temporary=
    rm "$path"
    saved=$((saved + bytes - compressed))
    count=$((count + 1))
    if [ "$((count % 25))" -eq 0 ]; then
        echo "$(date -u +%FT%TZ) compressed=$count saved_bytes=$saved"
    fi
done
echo "$(date -u +%FT%TZ) complete compressed=$count saved_bytes=$saved"
