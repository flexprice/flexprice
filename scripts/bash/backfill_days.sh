#!/usr/bin/env bash
set -euo pipefail

# ---------------------------
# USER CONFIG
# ---------------------------
CH_HOST="${CH_HOST:-127.0.0.1}"
CH_PORT="${CH_PORT:-9000}"
CH_USER="${CH_USER:-default}"
CH_PASSWORD="${CH_PASSWORD:-}"
CH_DB="${CH_DB:-flexprice}"

SRC_TABLE="${SRC_TABLE:-feature_usage}"
DST_TABLE="${DST_TABLE:-feature_usage_v2}"

TS_COL="${TS_COL:-timestamp}"          # <-- CHANGE THIS if needed
START_DATE="${START_DATE:-2026-01-01}"
END_DATE_EXCL="${END_DATE_EXCL:-2026-01-29}"  # exclude 2026-01-29 per your note

PARALLEL="${PARALLEL:-10}"
MAX_RETRIES="${MAX_RETRIES:-8}"
BASE_BACKOFF_SEC="${BASE_BACKOFF_SEC:-5}"

# extra safety against long-running / stuck queries from flaky networks
CONNECT_TIMEOUT_SEC="${CONNECT_TIMEOUT_SEC:-10}"
SEND_TIMEOUT_SEC="${SEND_TIMEOUT_SEC:-30}"
RECEIVE_TIMEOUT_SEC="${RECEIVE_TIMEOUT_SEC:-30}"

# For big days, allow server-side execution time.
# (This is NOT your local TCP timeout; it's ClickHouse query max execution.)
MAX_EXEC_TIME="${MAX_EXEC_TIME:-0}" # 0 = no limit; set e.g. 7200 if you want

LOG_DIR="${LOG_DIR:-./logs/backfill_days}"
mkdir -p "$LOG_DIR"

# ---------------------------
# CLICKHOUSE CLIENT WRAPPER
# ---------------------------
ch() {
  clickhouse client \
    --host "$CH_HOST" --port "$CH_PORT" \
    --user "$CH_USER" --password "$CH_PASSWORD" \
    --database "$CH_DB" \
    --connect_timeout "$CONNECT_TIMEOUT_SEC" \
    --send_timeout "$SEND_TIMEOUT_SEC" \
    --receive_timeout "$RECEIVE_TIMEOUT_SEC" \
    --multiquery \
    --format=TSV \
    "$@"
}

# ---------------------------
# HELPERS
# ---------------------------
# date_add() {
#   # portable date add (GNU date). If on macOS, install coreutils and use gdate.
#   date -d "$1 +1 day" +"%Y-%m-%d"
# }

# If you are on macOS, uncomment this and install coreutils: brew install coreutils
date_add() { gdate -d "$1 +1 day" +"%Y-%m-%d"; }

exists_day_in_dst() {
  local day="$1"
  # checks if destination already has rows for that day
  ch --query "
    SELECT count()
    FROM ${DST_TABLE}
    WHERE ${TS_COL} >= toDateTime('${day} 00:00:00')
      AND ${TS_COL} <  toDateTime('${day} 00:00:00') + INTERVAL 1 DAY
  " 2>/dev/null | tr -d '\r'
}

copy_day() {
  local day="$1"
  local log="$LOG_DIR/${day}.log"

  echo "[$(gdate -Iseconds)] Starting ${day}" | tee -a "$log"

  # idempotency guard (skip if already copied)
  local already
  already="$(exists_day_in_dst "$day" || echo "0")"
  already="$(echo "$already" | tr -d '\r\n ')"  # clean whitespace
  echo "[$(gdate -Iseconds)] DEBUG: already='$already' for ${day}" | tee -a "$log"
  if [[ -n "$already" && "$already" != "0" ]]; then
    echo "[$(gdate -Iseconds)] SKIP ${day} (dst already has ${already} rows)" | tee -a "$log"
    return 0
  fi

  local attempt=1
  while (( attempt <= MAX_RETRIES )); do
    echo "[$(gdate -Iseconds)] Attempt ${attempt}/${MAX_RETRIES} for ${day}" | tee -a "$log"

    # Run as a single query; server does the heavy lift.
    # Add settings for resilience and parallel read where helpful.
    if ch --query "
      INSERT INTO ${DST_TABLE}
      SELECT *
      FROM ${SRC_TABLE}
      WHERE ${TS_COL} >= toDateTime('${day} 00:00:00')
        AND ${TS_COL} <  toDateTime('${day} 00:00:00') + INTERVAL 1 DAY
      SETTINGS
        max_execution_time = ${MAX_EXEC_TIME},
        max_threads = 0,
        max_insert_threads = 0,
        insert_distributed_sync = 1
    " >>"$log" 2>&1; then
      # verify
      local cnt
      cnt="$(exists_day_in_dst "$day" || echo "0")"
      echo "[$(gdate -Iseconds)] DONE ${day} (dst rows=${cnt})" | tee -a "$log"
      return 0
    fi

    # failed: backoff + retry
    local sleep_for=$(( BASE_BACKOFF_SEC * attempt ))
    echo "[$(gdate -Iseconds)] FAIL ${day} attempt ${attempt}. Sleeping ${sleep_for}s then retry..." | tee -a "$log"
    sleep "$sleep_for"
    attempt=$(( attempt + 1 ))
  done

  echo "[$(gdate -Iseconds)] ERROR: Giving up on ${day} after ${MAX_RETRIES} attempts" | tee -a "$log"
  return 1
}
export -f copy_day
export -f exists_day_in_dst
export -f ch
export -f date_add

# ---------------------------
# MAIN: build list of days and run parallel
# ---------------------------
days=()
d="$START_DATE"
while [[ "$d" != "$END_DATE_EXCL" ]]; do
  days+=("$d")
  d="$(date_add "$d")"
done

echo "Will backfill ${#days[@]} days from ${START_DATE} to $(gdate -d "${END_DATE_EXCL} -1 day" +%Y-%m-%d 2>/dev/null || echo "${END_DATE_EXCL}(exclusive)")"
echo "Parallelism: ${PARALLEL}"
echo "Logs: ${LOG_DIR}"

# Export all necessary variables for subshells
export PATH
export CH_HOST CH_PORT CH_USER CH_PASSWORD CH_DB
export SRC_TABLE DST_TABLE TS_COL
export MAX_RETRIES BASE_BACKOFF_SEC
export CONNECT_TIMEOUT_SEC SEND_TIMEOUT_SEC RECEIVE_TIMEOUT_SEC MAX_EXEC_TIME
export LOG_DIR

# Run up to PARALLEL jobs at a time
# Use GNU parallel if available, otherwise fall back to sequential
if command -v parallel &> /dev/null; then
  export -f ch exists_day_in_dst date_add copy_day
  printf "%s\n" "${days[@]}" | parallel -j "$PARALLEL" copy_day \
    1> "$LOG_DIR/run.stdout.log" 2> "$LOG_DIR/run.stderr.log"
else
  # Sequential fallback
  for day in "${days[@]}"; do
    copy_day "$day"
  done
fi

echo "All done. Check $LOG_DIR for per-day logs."


: '
==================================================
USAGE EXAMPLES
==================================================

Basic usage (backfill feature_usage to feature_usage_v2 for January 29th):
  source .env.backfill && START_DATE=2026-01-29 END_DATE_EXCL=2026-01-30 ./backfill_days.sh

For a date range:
  source .env.backfill && START_DATE=2026-01-01 END_DATE_EXCL=2026-01-30 ./backfill_days.sh

For different tables:
  source .env.backfill && SRC_TABLE=raw_events DST_TABLE=raw_events_v2 START_DATE=2026-01-29 END_DATE_EXCL=2026-01-30 ./backfill_days.sh

With custom parallelism:
  source .env.backfill && PARALLEL=5 START_DATE=2026-01-29 END_DATE_EXCL=2026-01-30 ./backfill_days.sh

With custom timestamp column:
  source .env.backfill && TS_COL=created_at START_DATE=2026-01-29 END_DATE_EXCL=2026-01-30 ./backfill_days.sh

==================================================
CONFIGURABLE ENVIRONMENT VARIABLES
==================================================

ClickHouse Connection:
  CH_HOST          - ClickHouse host (default: 127.0.0.1)
  CH_PORT          - ClickHouse port (default: 9000)
  CH_USER          - ClickHouse user (default: default)
  CH_PASSWORD      - ClickHouse password (default: empty)
  CH_DB            - ClickHouse database (default: flexprice)

Table Configuration:
  SRC_TABLE        - Source table (default: feature_usage)
  DST_TABLE        - Destination table (default: feature_usage_v2)
  TS_COL           - Timestamp column for filtering (default: timestamp)

Date Range:
  START_DATE       - Start date YYYY-MM-DD (default: 2026-01-01)
  END_DATE_EXCL    - End date YYYY-MM-DD, exclusive (default: 2026-01-29)

Execution Control:
  PARALLEL         - Number of parallel jobs (default: 10)
  MAX_RETRIES      - Retry attempts per day (default: 8)
  BASE_BACKOFF_SEC - Base backoff seconds for retries (default: 5)
  MAX_EXEC_TIME    - Max query execution time in seconds (default: 0 = no limit)
  LOG_DIR          - Log directory (default: ./logs/backfill_days)

==================================================
'
