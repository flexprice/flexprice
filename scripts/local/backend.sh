#!/usr/bin/env bash
set -euo pipefail

repo_root="$(pwd -P)"
cache="$HOME/.cache/flexprice-local-backend"
if [[ -z "${COMPOSE_PROJECT_NAME:-}" ]]; then
  unset COMPOSE_PROJECT_NAME
fi

docker_host="$(docker context inspect --format '{{.Endpoints.docker.Host}}')"
if [[ "$docker_host" != unix://* ]]; then
  echo "The local backend requires a Docker context with a local Unix socket." >&2
  exit 1
fi
compose=(docker compose --env-file /dev/null -f docker-compose.yml -f docker-compose.local.yml)
if curl -fsS --max-time 2 http://127.0.0.1:8080/health >/dev/null 2>&1; then
  echo "An API is already healthy on port 8080. Stop it before rebuilding the local backend." >&2
  exit 1
fi

mkdir -p "$cache/bin" "$cache/runtime/internal"
ln -sfn "$repo_root/internal/config" "$cache/runtime/internal/config"
ln -sfn "$repo_root/.env.local" "$cache/runtime/.env"
ln -sfn "$repo_root/assets" "$cache/runtime/assets"
dbmate_version=v2.35.0
dbmate_bin="$cache/$dbmate_version"
if [[ ! -x "$dbmate_bin/dbmate" ]]; then
  GOBIN="$dbmate_bin" go install "github.com/amacneil/dbmate/v2@$dbmate_version"
fi
go build -o "$cache/bin/" ./cmd/server ./cmd/migrate

"${compose[@]}" up -d --wait --wait-timeout 90 postgres kafka clickhouse redis temporal temporal-ui
for attempt in {1..30}; do
  if "${compose[@]}" exec -T temporal temporal operator cluster health --address temporal:7233 2>/dev/null | grep -qx SERVING; then
    break
  fi
  if [[ "$attempt" == 30 ]]; then
    echo "Temporal did not become ready." >&2
    exit 1
  fi
  sleep 1
done

"${compose[@]}" exec -T postgres psql -U flexprice -d postgres -v ON_ERROR_STOP=1 <<'SQL'
SELECT 'CREATE DATABASE flexprice_local'
WHERE NOT EXISTS (SELECT 1 FROM pg_database WHERE datname = 'flexprice_local') \gexec
SQL

export PATH="$dbmate_bin:$PATH"
export FLEXPRICE_POSTGRES_DBNAME=flexprice_local FLEXPRICE_TEMPORAL_ENABLED=true

# The normal config loader reads .env.local through the runtime .env symlink.
cd "$cache/runtime"
"$cache/bin/migrate" postgres up --dir "$repo_root/migrations/versioned/postgres"
"$cache/bin/migrate" clickhouse --clickhouse-dir "$repo_root/migrations/clickhouse"

local_topics='{
  "defaults": {"replicationFactor": 1, "retentionMs": 604800000},
  "topics": {
    "events": {"partitions": 1},
    "events_lazy": {"partitions": 1},
    "events_backfill": {"partitions": 1},
    "events_dlq": {"partitions": 1},
    "event_processing_dlq": {"partitions": 1},
    "event_processing_lazy_dlq": {"partitions": 1},
    "meter_usage_tracking_service_dlq": {"partitions": 1},
    "meter_usage_tracking_service_lazy_dlq": {"partitions": 1},
    "flexprice_system_events": {"partitions": 1},
    "onboarding_events": {"partitions": 1},
    "raw_events": {"partitions": 1},
    "wallet_alert": {"partitions": 1}
  }
}'
FLEXPRICE_KAFKA_TOPICS="$local_topics" "$cache/bin/migrate" kafka
echo "Starting Flexprice at http://127.0.0.1:8080 (Ctrl-C to stop the backend)."
exec "$cache/bin/server"
