#!/usr/bin/env bash
#
# Monitors reprocessing progress by comparing raw_events vs events table.
# Shows per-customer missing event counts and overall progress.
#
# Usage:
#   source .env.backfill && ./monitor_reprocess.sh [--loop]
#
set -euo pipefail

###############################################################################
# Parameters
###############################################################################
TENANT_ID="${TENANT_ID:?TENANT_ID is required}"
ENVIRONMENT_ID="${ENVIRONMENT_ID:?ENVIRONMENT_ID is required}"

CH_HOST="${CH_HOST:-127.0.0.1}"
CH_PORT="${CH_PORT:-9000}"
CH_USER="${CH_USER:-default}"
CH_PASSWORD="${CH_PASSWORD:-}"
CH_DB="${CH_DB:-flexprice}"

CSV_FILE="${CSV_FILE:-/Users/nikhilmishra/Downloads/vapi-top-1000-cust-by-events-feb.csv}"
LOOP="${1:-}"  # pass --loop to repeat every 60s

###############################################################################
# Helpers
###############################################################################
log() { printf '[%s] %s\n' "$(date '+%Y-%m-%d %H:%M:%S')" "$*"; }

ch() {
  clickhouse client \
    --host "$CH_HOST" --port "$CH_PORT" \
    --user "$CH_USER" --password "$CH_PASSWORD" \
    --database "$CH_DB" \
    --format TSV \
    "$@"
}

ch_pretty() {
  clickhouse client \
    --host "$CH_HOST" --port "$CH_PORT" \
    --user "$CH_USER" --password "$CH_PASSWORD" \
    --database "$CH_DB" \
    --format PrettyCompact \
    "$@"
}

###############################################################################
# Monitoring queries
###############################################################################
run_monitor() {
  log "================================================================"
  log " Reprocessing Progress Monitor"
  log "================================================================"
  log ""

  # 1. Overall counts for February
  log "--- Overall February 2026 Event Counts ---"
  ch_pretty --query "
    SELECT
      'raw_events' AS source,
      count() AS total_count
    FROM ${CH_DB}.raw_events
    PREWHERE tenant_id = '${TENANT_ID}'
      AND environment_id = '${ENVIRONMENT_ID}'
      AND timestamp >= toDateTime64('2026-02-01 00:00:00', 3)
      AND timestamp <  toDateTime64('2026-03-01 00:00:00', 3)
    WHERE field4 = 'false'
      AND field1 != 'custom-llm'

    UNION ALL

    SELECT
      'events' AS source,
      count() AS total_count
    FROM ${CH_DB}.events
    PREWHERE tenant_id = '${TENANT_ID}'
      AND environment_id = '${ENVIRONMENT_ID}'
      AND timestamp >= toDateTime64('2026-02-01 00:00:00', 3)
      AND timestamp <  toDateTime64('2026-03-01 00:00:00', 3)

    SETTINGS max_memory_usage = 4000000000
  "

  log ""

  # 2. Per-customer missing counts (top 10 by missing)
  log "--- Top 10 Customers by Missing Events ---"
  ch_pretty --query "
    SELECT
      r.external_customer_id,
      r.raw_count,
      e.events_count,
      (r.raw_count - e.events_count) AS missing,
      round(e.events_count * 100.0 / r.raw_count, 1) AS pct_complete
    FROM (
      SELECT
        external_customer_id,
        count() AS raw_count
      FROM ${CH_DB}.raw_events
      PREWHERE tenant_id = '${TENANT_ID}'
        AND environment_id = '${ENVIRONMENT_ID}'
        AND timestamp >= toDateTime64('2026-02-01 00:00:00', 3)
        AND timestamp <  toDateTime64('2026-03-01 00:00:00', 3)
      WHERE field4 = 'false'
        AND field1 != 'custom-llm'
      GROUP BY external_customer_id
    ) r
    LEFT JOIN (
      SELECT
        external_customer_id,
        count() AS events_count
      FROM ${CH_DB}.events
      PREWHERE tenant_id = '${TENANT_ID}'
        AND environment_id = '${ENVIRONMENT_ID}'
        AND timestamp >= toDateTime64('2026-02-01 00:00:00', 3)
        AND timestamp <  toDateTime64('2026-03-01 00:00:00', 3)
      GROUP BY external_customer_id
    ) e ON r.external_customer_id = e.external_customer_id
    ORDER BY missing DESC
    LIMIT 10

    SETTINGS max_memory_usage = 4000000000
  "

  log ""

  # 3. Recent insert rate (events table, last 5 minutes)
  log "--- Recent Events Table Insert Rate (last 5m windows) ---"
  ch_pretty --query "
    SELECT
      toStartOfFiveMinutes(ingested_at) AS window,
      count() AS inserts
    FROM ${CH_DB}.events
    PREWHERE tenant_id = '${TENANT_ID}'
      AND environment_id = '${ENVIRONMENT_ID}'
      AND timestamp >= toDateTime64('2026-02-01 00:00:00', 3)
      AND timestamp <  toDateTime64('2026-03-01 00:00:00', 3)
    WHERE ingested_at >= now() - INTERVAL 30 MINUTE
    GROUP BY window
    ORDER BY window DESC
    LIMIT 10

    SETTINGS max_memory_usage = 4000000000
  "

  log ""

  # 4. ClickHouse resource usage (active queries + memory)
  log "--- ClickHouse Active Queries ---"
  ch_pretty --query "
    SELECT
      query_id,
      user,
      elapsed,
      read_rows,
      formatReadableSize(memory_usage) AS mem,
      substring(query, 1, 100) AS query_prefix
    FROM system.processes
    WHERE user != 'system'
    ORDER BY memory_usage DESC
    LIMIT 10
  "

  log ""
  log "================================================================"
}

###############################################################################
# Main
###############################################################################
if [[ "$LOOP" == "--loop" ]]; then
  while true; do
    run_monitor
    log "Next check in 60s... (Ctrl+C to stop)"
    sleep 60
  done
else
  run_monitor
fi
