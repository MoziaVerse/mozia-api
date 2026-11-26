#!/usr/bin/env bash
set -euo pipefail

# Renders Prometheus file_sd_configs target JSON files from .env.monitoring so
# Plan A (separate monitoring host) and single-host testing share one prometheus.yml.

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
MON_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
ENV_FILE="${1:-${MON_DIR}/.env.monitoring}"

if [[ -f "$ENV_FILE" ]]; then
  set -a
  # shellcheck disable=SC1090
  source "$ENV_FILE"
  set +a
fi

TARGET_DIR="${MON_DIR}/prometheus/targets"
mkdir -p "$TARGET_DIR"
chmod 755 "$TARGET_DIR"

HOST_IP="${BUSINESS_HOST_IP:-172.16.10.110}"
ENV_NAME="${ALERT_ENV:-prod}"
PRIMARY_PORT="${MOZIA_PRIMARY_METRICS_PORT:-9091}"
REPLICA_PORT="${MOZIA_REPLICA_METRICS_PORT:-9092}"
NODE_PORT="${NODE_EXPORTER_PORT:-9100}"
CADVISOR_PORT="${CADVISOR_PORT:-8088}"
PG_PORT="${POSTGRES_EXPORTER_PORT:-9187}"
REDIS_PORT="${REDIS_EXPORTER_PORT:-9121}"

cat > "${TARGET_DIR}/mozia_api.json" <<EOF
[
  {
    "targets": ["${HOST_IP}:${PRIMARY_PORT}"],
    "labels": {
      "env": "${ENV_NAME}",
      "node_name": "mozia-api-primary",
      "role": "master"
    }
  },
  {
    "targets": ["${HOST_IP}:${REPLICA_PORT}"],
    "labels": {
      "env": "${ENV_NAME}",
      "node_name": "mozia-api-replica",
      "role": "slave"
    }
  }
]
EOF

cat > "${TARGET_DIR}/node_exporter.json" <<EOF
[
  {
    "targets": ["${HOST_IP}:${NODE_PORT}"],
    "labels": {
      "env": "${ENV_NAME}",
      "host": "mozia-api-host"
    }
  }
]
EOF

cat > "${TARGET_DIR}/cadvisor.json" <<EOF
[
  {
    "targets": ["${HOST_IP}:${CADVISOR_PORT}"],
    "labels": {
      "env": "${ENV_NAME}",
      "host": "mozia-api-host"
    }
  }
]
EOF

cat > "${TARGET_DIR}/postgres_exporter.json" <<EOF
[
  {
    "targets": ["${HOST_IP}:${PG_PORT}"],
    "labels": {
      "env": "${ENV_NAME}",
      "host": "mozia-api-host"
    }
  }
]
EOF

cat > "${TARGET_DIR}/redis_exporter.json" <<EOF
[
  {
    "targets": ["${HOST_IP}:${REDIS_PORT}"],
    "labels": {
      "env": "${ENV_NAME}",
      "host": "mozia-api-host"
    }
  }
]
EOF

# Always create metrics_token file (empty when METRICS_TOKEN is unset;
# Prometheus handles empty credentials_file cleanly, or switch off auth if needed).
umask 077
printf '%s' "${METRICS_TOKEN:-}" > "${TARGET_DIR}/metrics_token"

# If METRICS_TOKEN is empty, strip the authorization block from active runtime config
# by generating prometheus.runtime.yml so Prometheus never sends an empty "Bearer " header.
if [[ -z "${METRICS_TOKEN:-}" ]]; then
  awk '
    /^[[:space:]]*authorization:[[:space:]]*$/ { skip=1; next }
    skip && /^[[:space:]]*(type|credentials_file):/ { next }
    skip { skip=0 }
    { print }
  ' "${MON_DIR}/prometheus/prometheus.yml" > "${TARGET_DIR}/prometheus.runtime.yml"
else
  cp "${MON_DIR}/prometheus/prometheus.yml" "${TARGET_DIR}/prometheus.runtime.yml"
fi

chmod 644 "${TARGET_DIR}"/*.json "${TARGET_DIR}/prometheus.runtime.yml" "${TARGET_DIR}/metrics_token"
echo "[render-targets] Rendered Prometheus targets for ${HOST_IP} (env=${ENV_NAME}) into ${TARGET_DIR}"
