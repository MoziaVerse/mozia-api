#!/bin/sh
# Run in the same Alpine image as PostgreSQL, passing the compressor's path.
set -eu
compressor=$1
fixture=$(mktemp -d)
trap 'rm -rf "$fixture"' EXIT
for name in old active young; do
    printf 'synthetic audit evidence\n' > "$fixture/postgresql-$name.log"
done
touch -t 202001010000 "$fixture/postgresql-old.log" "$fixture/postgresql-active.log"
printf 'stderr %s/postgresql-active.log\n' "$fixture" > "$fixture/current_logfiles"
sh "$compressor" "$fixture" "$fixture/current_logfiles" 3600
[ ! -e "$fixture/postgresql-old.log" ]
[ "$(gzip -dc "$fixture/postgresql-old.log.gz")" = 'synthetic audit evidence' ]
[ "$(stat -c %Y "$fixture/postgresql-old.log.gz")" = "$(stat -c %Y "$fixture/postgresql-active.log")" ]
[ -f "$fixture/postgresql-active.log" ]
[ ! -e "$fixture/postgresql-active.log.gz" ]
[ -f "$fixture/postgresql-young.log" ]
# A second run is safe; even old archives are preserved without a verified copy.
sh "$compressor" "$fixture" "$fixture/current_logfiles" 3600
[ -f "$fixture/postgresql-old.log.gz" ]
printf 'original evidence\n' > "$fixture/postgresql-conflict.log"
touch -t 202001010000 "$fixture/postgresql-conflict.log"
printf 'existing archive\n' > "$fixture/postgresql-conflict.log.gz"
if sh "$compressor" "$fixture" "$fixture/current_logfiles" 3600; then
    echo 'ERROR: conflicting archive was not rejected' >&2
    exit 1
fi
[ "$(cat "$fixture/postgresql-conflict.log")" = 'original evidence' ]
[ "$(cat "$fixture/postgresql-conflict.log.gz")" = 'existing archive' ]
echo 'PASS: active/young files, archive integrity, timestamps, repeat runs and conflicts'
