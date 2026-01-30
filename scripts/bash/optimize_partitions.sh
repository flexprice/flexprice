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

TABLE_NAME="${TABLE_NAME:-feature_usage}"
PARTITION_COL="${PARTITION_COL:-timestamp}"  # column used for partition key

START_DATE="${START_DATE:-2026-01-01}"
END_DATE_EXCL="${END_DATE_EXCL:-2026-01-29}"

PARALLEL="${PARALLEL:-10}"
MAX_RETRIES="${MAX_RETRIES:-3}"
BASE_BACKOFF_SEC="${BASE_BACKOFF_SEC:-5}"

# Timeouts
CONNECT_TIMEOUT_SEC="${CONNECT_TIMEOUT_SEC:-10}"
SEND_TIMEOUT_SEC="${SEND_TIMEOUT_SEC:-30}"
RECEIVE_TIMEOUT_SEC="${RECEIVE_TIMEOUT_SEC:-600}"  # OPTIMIZE can take longer

# For large partitions, allow more time
MAX_EXEC_TIME="${MAX_EXEC_TIME:-0}"  # 0 = no limit

LOG_DIR="${LOG_DIR:-./logs/optimize_partitions}"
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
date_add() { gdate -d "$1 +1 day" +"%Y-%m-%d"; }

# Convert YYYY-MM-DD to YYYYMMDD partition format
date_to_partition() {
  echo "$1" | tr -d '-'
}

optimize_partition() {
  local day="$1"
  local partition="$(date_to_partition "$day")"
  local log="$LOG_DIR/${day}.log"

  echo "[$(gdate -Iseconds)] Starting OPTIMIZE for ${day} (partition ${partition})" | tee -a "$log"

  local attempt=1
  while (( attempt <= MAX_RETRIES )); do
    echo "[$(gdate -Iseconds)] Attempt ${attempt}/${MAX_RETRIES} for ${day}" | tee -a "$log"

    # Run OPTIMIZE TABLE for the specific partition
    if ch --query "
      OPTIMIZE TABLE ${TABLE_NAME}
      PARTITION ${partition}
      FINAL
      SETTINGS
        max_execution_time = ${MAX_EXEC_TIME}
    " >>"$log" 2>&1; then
      echo "[$(gdate -Iseconds)] DONE ${day} (partition ${partition})" | tee -a "$log"
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
export -f optimize_partition
export -f date_to_partition
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

echo "Will optimize ${#days[@]} partitions for table ${TABLE_NAME}"
echo "Date range: ${START_DATE} to $(gdate -d "${END_DATE_EXCL} -1 day" +%Y-%m-%d 2>/dev/null || echo "${END_DATE_EXCL}(exclusive)")"
echo "Parallelism: ${PARALLEL}"
echo "Logs: ${LOG_DIR}"

# Export all necessary variables for subshells
export PATH
export CH_HOST CH_PORT CH_USER CH_PASSWORD CH_DB
export TABLE_NAME PARTITION_COL
export MAX_RETRIES BASE_BACKOFF_SEC
export CONNECT_TIMEOUT_SEC SEND_TIMEOUT_SEC RECEIVE_TIMEOUT_SEC MAX_EXEC_TIME
export LOG_DIR

# Run up to PARALLEL jobs at a time
if command -v parallel &> /dev/null; then
  export -f ch date_to_partition date_add optimize_partition
  printf "%s\n" "${days[@]}" | parallel -j "$PARALLEL" optimize_partition \
    1> "$LOG_DIR/run.stdout.log" 2> "$LOG_DIR/run.stderr.log"
else
  # Sequential fallback
  for day in "${days[@]}"; do
    optimize_partition "$day"
  done
fi

echo "All done. Check $LOG_DIR for per-day logs."



: '
==================================================
USAGE EXAMPLES
==================================================

Basic usage (optimize feature_usage for January 29th):
  source .env.backfill && START_DATE=2026-01-29 END_DATE_EXCL=2026-01-30 ./optimize_partitions.sh

For a different table (e.g., raw_events):
  source .env.backfill && TABLE_NAME=raw_events START_DATE=2026-01-29 END_DATE_EXCL=2026-01-30 ./optimize_partitions.sh

For a date range:
  source .env.backfill && START_DATE=2026-01-01 END_DATE_EXCL=2026-01-30 ./optimize_partitions.sh

With custom parallelism:
  source .env.backfill && PARALLEL=5 START_DATE=2026-01-29 END_DATE_EXCL=2026-01-30 ./optimize_partitions.sh

Multiple tables sequentially:
  source .env.backfill && TABLE_NAME=feature_usage START_DATE=2026-01-29 END_DATE_EXCL=2026-01-30 ./optimize_partitions.sh
  source .env.backfill && TABLE_NAME=raw_events START_DATE=2026-01-29 END_DATE_EXCL=2026-01-30 ./optimize_partitions.sh

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
  TABLE_NAME       - Table to optimize (default: feature_usage)
  PARTITION_COL    - Partition column name (default: timestamp)

Date Range:
  START_DATE       - Start date YYYY-MM-DD (default: 2026-01-01)
  END_DATE_EXCL    - End date YYYY-MM-DD, exclusive (default: 2026-01-29)

Execution Control:
  PARALLEL         - Number of parallel jobs (default: 10)
  MAX_RETRIES      - Retry attempts per partition (default: 3)
  BASE_BACKOFF_SEC - Base backoff seconds for retries (default: 5)
  LOG_DIR          - Log directory (default: ./logs/optimize_partitions)

==================================================
'
