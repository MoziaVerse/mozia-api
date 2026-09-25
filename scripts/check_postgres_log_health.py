#!/usr/bin/env python3
"""Local disk/maintenance alerts; never emits database contents or credentials."""
import datetime
import json
import pathlib
import shutil
import subprocess
import time

task_dir = pathlib.Path(__file__).resolve().parent
state_file = task_dir / "postgres-log-health.json"
disk = shutil.disk_usage("/")
used_percent = round(100 * disk.used / (disk.used + disk.free), 1)
now = time.time()
alerts = []
if used_percent >= 90 or disk.free < 30 * 1024**3:
    alerts.append("critical_disk_capacity")
elif used_percent >= 80:
    alerts.append("high_disk_usage")
success = task_dir / "postgres-log-maintenance.last-success"
if success.exists() and now - success.stat().st_mtime > 7200:
    alerts.append("maintenance_overdue")
elif not success.exists() and now - (task_dir / "maintain_postgres_logs.sh").stat().st_mtime > 7200:
    alerts.append("maintenance_never_completed")
result = subprocess.run(
    ["docker", "exec", "-u", "postgres", "postgres-dev", "sh", "-c",
     'du -sk "$PGDATA/pg_log"'], capture_output=True, text=True, timeout=30)
log_bytes = None
if result.returncode:
    alerts.append("log_directory_check_failed")
else:
    log_bytes = int(result.stdout.split()[0]) * 1024
    if log_bytes > 10 * 1024**3:
        alerts.append("log_directory_over_budget")
previous = json.loads(state_file.read_text()) if state_file.exists() else {}
state = {"checked_at": datetime.datetime.now(datetime.timezone.utc).isoformat(),
         "disk_used_percent": used_percent, "disk_free_bytes": disk.free,
         "postgres_log_bytes": log_bytes, "alerts": alerts,
         "last_alert_at": previous.get("last_alert_at", 0)}
if alerts != previous.get("alerts") or (alerts and now - state["last_alert_at"] >= 1800):
    subprocess.run(["logger", "-p", "user.err" if alerts else "user.notice",
                    "-t", "postgres-log-health", json.dumps(state)], check=True)
    state["last_alert_at"] = now
temporary = state_file.with_suffix(".tmp")
temporary.write_text(json.dumps(state) + "\n")
temporary.chmod(0o600)
temporary.replace(state_file)
print(json.dumps(state))
