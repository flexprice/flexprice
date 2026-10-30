#!/usr/bin/env bash
# Deploys one Flexprice image to the UAT host: migrations, Temporal namespace, then
# the api / consumer / worker containers. Every step is idempotent and safe to re-run.
# Usage: deploy.sh <image:tag> <env-file> <api-host-port>
set -euo pipefail

IMAGE="$1"
ENV_FILE="$2"
API_PORT="$3"

NAME_PREFIX="flexprice-uat"
PSQL_IMAGE="postgres:16-alpine"
TEMPORAL_ADMIN_IMAGE="temporalio/admin-tools:1.29.7-tctl-1.18.4-cli-1.7.2"
# Lets containers reach Postgres, Redis, ClickHouse, Kafka and Temporal published on this host.
HOST_GATEWAY="--add-host=host.docker.internal:host-gateway"

env_value() {
  grep -E "^$1=" "$ENV_FILE" | tail -n1 | cut -d= -f2-
}

run_once() {
  docker run --rm $HOST_GATEWAY --env-file "$ENV_FILE" "$@"
}

echo "=== Pull $IMAGE ==="
docker pull "$IMAGE"

echo "=== Postgres prerequisites ==="
run_once "$PSQL_IMAGE" sh -c '
  PGPASSWORD="$FLEXPRICE_POSTGRES_PASSWORD" psql -v ON_ERROR_STOP=1 \
    "host=$FLEXPRICE_POSTGRES_HOST port=$FLEXPRICE_POSTGRES_PORT user=$FLEXPRICE_POSTGRES_USER dbname=$FLEXPRICE_POSTGRES_DBNAME sslmode=${FLEXPRICE_POSTGRES_SSLMODE:-disable}" \
    -c "CREATE SCHEMA IF NOT EXISTS extensions;" \
    -c "CREATE EXTENSION IF NOT EXISTS \"uuid-ossp\" SCHEMA extensions;"'

echo "=== Postgres migrations ==="
run_once "$IMAGE" ./migrate postgres up

echo "=== ClickHouse migrations ==="
run_once "$IMAGE" ./migrate clickhouse

echo "=== Kafka topics ==="
run_once "$IMAGE" ./migrate kafka

echo "=== Temporal namespace ==="
TEMPORAL_ADDRESS="$(env_value FLEXPRICE_TEMPORAL_ADDRESS)"
TEMPORAL_NAMESPACE="$(env_value FLEXPRICE_TEMPORAL_NAMESPACE)"
TEMPORAL_RETENTION="$(env_value TEMPORAL_NAMESPACE_RETENTION)"
TEMPORAL_TLS_FLAG=""
if [ "$(env_value FLEXPRICE_TEMPORAL_TLS)" = "true" ]; then
  TEMPORAL_TLS_FLAG="--tls"
fi
if docker run --rm $HOST_GATEWAY "$TEMPORAL_ADMIN_IMAGE" temporal --address "$TEMPORAL_ADDRESS" $TEMPORAL_TLS_FLAG \
    operator namespace describe --namespace "$TEMPORAL_NAMESPACE" >/dev/null 2>&1; then
  echo "Namespace $TEMPORAL_NAMESPACE already exists"
else
  docker run --rm $HOST_GATEWAY "$TEMPORAL_ADMIN_IMAGE" temporal --address "$TEMPORAL_ADDRESS" $TEMPORAL_TLS_FLAG \
    operator namespace create --namespace "$TEMPORAL_NAMESPACE" \
    --retention "${TEMPORAL_RETENTION:-168h}" --description "Flexprice UAT"
  echo "Created namespace $TEMPORAL_NAMESPACE"
fi

replace_container() {
  local role="$1" mode="$2"
  shift 2
  local name="$NAME_PREFIX-$role"
  docker rm -f "$name" >/dev/null 2>&1 || true
  docker run -d \
    --name "$name" \
    --restart unless-stopped \
    $HOST_GATEWAY \
    --env-file "$ENV_FILE" \
    -e FLEXPRICE_DEPLOYMENT_MODE="$mode" \
    --log-opt max-size=50m --log-opt max-file=5 \
    "$@" \
    "$IMAGE"
  echo "Started $name ($mode)"
}

echo "=== Containers ==="
# Worker first so workflows started by the new API have a matching worker.
replace_container worker temporal_worker
replace_container consumer consumer
replace_container api api -p "$API_PORT:8080"

echo "=== Health check ==="
for attempt in $(seq 1 30); do
  if curl -fsS "http://127.0.0.1:$API_PORT/health" >/dev/null; then
    echo "API healthy on port $API_PORT"
    break
  fi
  if [ "$attempt" -eq 30 ]; then
    echo "API did not become healthy; last logs:"
    docker logs --tail 100 "$NAME_PREFIX-api" || true
    exit 1
  fi
  sleep 2
done

for role in worker consumer; do
  if [ "$(docker inspect -f '{{.State.Running}}' "$NAME_PREFIX-$role")" != "true" ]; then
    echo "$NAME_PREFIX-$role is not running; last logs:"
    docker logs --tail 100 "$NAME_PREFIX-$role" || true
    exit 1
  fi
done

docker ps --filter "name=$NAME_PREFIX-"
docker image prune -af --filter 'until=48h' >/dev/null || true
