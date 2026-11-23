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

set -a
source ./.env.local
set +a
export PATH="$dbmate_bin:$PATH"
export FLEXPRICE_SERVER_ADDRESS=127.0.0.1:8080 FLEXPRICE_DEPLOYMENT_MODE=local
export FLEXPRICE_POSTGRES_HOST=127.0.0.1 FLEXPRICE_POSTGRES_READER_HOST=127.0.0.1 FLEXPRICE_POSTGRES_DBNAME=flexprice_local
export FLEXPRICE_CLICKHOUSE_ADDRESS=127.0.0.1:9000 FLEXPRICE_KAFKA_BROKERS=127.0.0.1:29092
export FLEXPRICE_KAFKA_TOPIC_DLQ=events_dlq FLEXPRICE_RAW_EVENTS_REPROCESSING_OUTPUT_TOPIC=events
export FLEXPRICE_REDIS_HOST=127.0.0.1 FLEXPRICE_REDIS_CLUSTER_MODE=false
export FLEXPRICE_TEMPORAL_ENABLED=true FLEXPRICE_TEMPORAL_ADDRESS=127.0.0.1:7233
export FLEXPRICE_AUTH_PROVIDER=flexprice FLEXPRICE_OTEL_ENABLED=false FLEXPRICE_LOGGING_LEVEL=info

# Run outside the checkout so the config loader cannot pick up its .env.
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
