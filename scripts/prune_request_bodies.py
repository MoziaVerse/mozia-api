#!/usr/bin/env python3
"""Bounded PostgreSQL request-body retention; run beside the SQL file on the host."""
import fcntl
import json
import pathlib
import shutil
import subprocess
import time
import urllib.parse

task_dir = pathlib.Path(__file__).resolve().parent
lock = (task_dir / "request-body-retention.lock").open("w")
try:
    fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
except BlockingIOError:
    raise SystemExit(0)
if shutil.disk_usage("/").free < 50 * 1024**3:
    raise SystemExit("Deferred: at least 50 GiB free is required before pruning database payloads")
info = json.loads(subprocess.check_output(["docker", "inspect", "mozia-api"]))[0]
env = dict(item.split("=", 1) for item in info["Config"]["Env"] if "=" in item)
dsn = urllib.parse.urlsplit(env.get("LOG_SQL_DSN") or env["SQL_DSN"])
if dsn.hostname != "postgres-dev":
    raise SystemExit("Unexpected log database host; inspect configuration before proceeding")
state_file = task_dir / "request-body-retention.json"
state = json.loads(state_file.read_text()) if state_file.exists() else {"cursor": 0, "cursor_time": 0}
sql = (task_dir / "prune_request_bodies.sql").read_text()
cutoff = int(time.time()) - 7 * 86400
deadline = time.monotonic() + 90
totals = {"examined": 0, "updated": 0, "skipped": 0, "removed_text_bytes": 0}
for _ in range(100):
    cmd = ["docker", "exec", "-i", "postgres-dev", "sh", "-c",
           'IFS= read -r PGPASSWORD; export PGPASSWORD; exec psql -X -qAt -w -v ON_ERROR_STOP=1 -U "$1" -d "$2" -v cursor="$3" -v cutoff="$4" -v cursor_time="$5"',
           "sh", urllib.parse.unquote(dsn.username), dsn.path.lstrip("/"),
           str(int(state["cursor"])), str(cutoff), str(int(state["cursor_time"]))]
    result = subprocess.run(cmd, input=urllib.parse.unquote(dsn.password) + "\n" + sql,
                            text=True, capture_output=True, timeout=15)
    if result.returncode:
        raise SystemExit("Retention batch failed; transaction rolled back and cursor preserved")
    batch = json.loads(result.stdout.strip())
    state["cursor"] = batch["cursor"]
    state["cursor_time"] = batch["cursor_time"]
    for key in totals:
        totals[key] += batch[key]
    state.update(last_success=int(time.time()), last_run=totals.copy())
    temporary = state_file.with_suffix(".tmp")
    temporary.write_text(json.dumps(state) + "\n")
    temporary.chmod(0o600)
    temporary.replace(state_file)
    if not batch["examined"] or time.monotonic() >= deadline:
        break
    time.sleep(0.1)
print(json.dumps(state))
