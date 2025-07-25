#!/usr/bin/env bash
#
# Spin up local Temporal + Kafka (via docker-compose) and start the
# Temporal worker (billing-task-queue) on your laptop.
#
# Remote resources (Postgres, ClickHouse) stay unchanged.
# Adjust the PG creds block below to match prod.

set -euo pipefail

echo "▶ starting Temporal dev-server + Kafka ..."
docker compose up -d temporal temporal-ui kafka >/dev/null

echo "▶ exporting local-dev runtime variables"
# run mode
export FLEXPRICE_DEPLOYMENT_MODE="temporal_worker"

# Temporal – local dev, no TLS, no API key
export FLEXPRICE_TEMPORAL_ADDRESS="127.0.0.1:7233"
export FLEXPRICE_TEMPORAL_NAMESPACE="default"
export FLEXPRICE_TEMPORAL_TLS="false"
export FLEXPRICE_TEMPORAL_API_KEY=""

# Kafka – local docker-compose broker (PLAINTEXT)
export FLEXPRICE_KAFKA_BROKERS="127.0.0.1:29092"
export FLEXPRICE_KAFKA_USE_SASL="false"

# ClickHouse local-compose (optional; comment if unused)
# export FLEXPRICE_CLICKHOUSE_ADDRESS="127.0.0.1:9000"

echo "▶ running worker (ctrl-c to stop)"
exec go run ./cmd/server