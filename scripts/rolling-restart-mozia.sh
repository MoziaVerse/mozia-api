#!/usr/bin/env bash
set -euo pipefail

# Zero-downtime rolling restart for mozia-api (replica first, then primary).
# Works with Caddy active health checks on /readyz and graceful SIGTERM draining.
#
# Usage:
#   ./scripts/rolling-restart-mozia.sh [--build] [--compose-file docker-compose.mozia.yml]

COMPOSE_FILE="${COMPOSE_FILE:-docker-compose.mozia.yml}"
DO_BUILD=0
WAIT_TIMEOUT="${WAIT_TIMEOUT:-90}"
PRIMARY_URL="${PRIMARY_READY_URL:-http://127.0.0.1:3369/readyz}"
REPLICA_URL="${REPLICA_READY_URL:-http://127.0.0.1:3371/readyz}"
SETTLE_SECONDS="${SETTLE_SECONDS:-5}"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --build)
      DO_BUILD=1
      shift
      ;;
    --compose-file|-f)
      COMPOSE_FILE="$2"
      shift 2
      ;;
    --timeout)
      WAIT_TIMEOUT="$2"
      shift 2
      ;;
    *)
      echo "Unknown option: $1" >&2
      echo "Usage: $0 [--build] [--compose-file FILE] [--timeout SECONDS]" >&2
      exit 1
      ;;
  esac
done

wait_ready() {
  local name="$1"
  local url="$2"
  local deadline=$((SECONDS + WAIT_TIMEOUT))
  echo "[rolling-restart] Waiting for ${name} (${url}) to become ready..."
  while (( SECONDS < deadline )); do
    if curl -fsS --max-time 3 "$url" >/dev/null 2>&1; then
      echo "[rolling-restart] ${name} is READY."
      return 0
    fi
    sleep 2
  done
  echo "[rolling-restart] ERROR: ${name} did not become ready within ${WAIT_TIMEOUT}s" >&2
  return 1
}

echo "[rolling-restart] Verifying primary is healthy before touching replica..."
wait_ready "mozia-api (primary)" "$PRIMARY_URL"

if [[ "$DO_BUILD" -eq 1 ]]; then
  echo "[rolling-restart] Building updated mozia-api image..."
  docker compose -f "$COMPOSE_FILE" build mozia-api
fi

echo "[rolling-restart] Step 1/2: Rolling mozia-api-replica..."
docker compose -f "$COMPOSE_FILE" up -d --no-deps --force-recreate mozia-api-replica
wait_ready "mozia-api-replica" "$REPLICA_URL"

echo "[rolling-restart] Waiting ${SETTLE_SECONDS}s for Caddy health check to re-admit replica..."
sleep "$SETTLE_SECONDS"

echo "[rolling-restart] Step 2/2: Rolling mozia-api (primary)..."
docker compose -f "$COMPOSE_FILE" up -d --no-deps --force-recreate mozia-api
wait_ready "mozia-api (primary)" "$PRIMARY_URL"

echo "[rolling-restart] Rolling restart completed cleanly with zero downtime."
