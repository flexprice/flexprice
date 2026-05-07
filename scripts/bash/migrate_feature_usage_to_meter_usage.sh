#!/usr/bin/env bash
set -euo pipefail

# ---------------------------
# migrate_feature_usage_to_meter_usage.sh
#
# Migrates rows from flexprice.feature_usage → flexprice.meter_usage
# for a specific tenant+environment, day by day.
#
# USAGE:
#   source scripts/bash/.env.backfill
#   START_DATE=2026-03-01 END_DATE_EXCL=2026-03-02 ./scripts/bash/migrate_feature_usage_to_meter_usage.sh
#
# COLUMN MAPPING:
#   id, tenant_id, environment_id, external_customer_id → direct
#   meter_id        → COALESCE(meter_id, '')
#   event_name      → direct
#   timestamp       → toDateTime(timestamp)  [drops ms precision]
#   ingested_at     → direct
#   qty_total       → CAST(qty_total, 'Decimal(18,8)')
#   unique_hash     → COALESCE(unique_hash, '')
#   source          → COALESCE(source, '')
#   properties      → '' (skipped per requirement)
# ---------------------------

# ---- ClickHouse connection (from .env.backfill) ----
CH_HOST="${CH_HOST:-127.0.0.1}"
CH_PORT="${CH_PORT:-9000}"
CH_USER="${CH_USER:-default}"
CH_PASSWORD="${CH_PASSWORD:-}"
CH_DB="${CH_DB:-flexprice}"

# ---- Scope: tenant + environment ----
TENANT_ID="${TENANT_ID:-tenant_01KF5GXB4S7YKWH2Y3YQ1TEMQ3}"
ENVIRONMENT_ID="${ENVIRONMENT_ID:-env_01KG4E6FR5YCNW0742N6CA1YD1}"

# ---- Date range (END_DATE_EXCL is exclusive) ----
START_DATE="${START_DATE:-2026-03-01}"
END_DATE_EXCL="${END_DATE_EXCL:-2026-04-01}"

# ---- Execution settings ----
PARALLEL="${PARALLEL:-3}"               # keep low — destination parts_to_throw_insert=300
MAX_RETRIES="${MAX_RETRIES:-5}"
BASE_BACKOFF_SEC="${BASE_BACKOFF_SEC:-10}"
MAX_EXEC_TIME="${MAX_EXEC_TIME:-3600}"  # 1h per day-batch
VERIFY_SLEEP_SEC="${VERIFY_SLEEP_SEC:-5}"
FORCE="${FORCE:-0}"                     # set to 1 to skip idempotency check and re-insert even if dst has rows

CONNECT_TIMEOUT_SEC="${CONNECT_TIMEOUT_SEC:-10}"
SEND_TIMEOUT_SEC="${SEND_TIMEOUT_SEC:-60}"
RECEIVE_TIMEOUT_SEC="${RECEIVE_TIMEOUT_SEC:-3600}"

LOG_DIR="${LOG_DIR:-./logs/migrate_feature_usage_to_meter_usage}"
mkdir -p "$LOG_DIR"

SRC_TABLE="feature_usage"
DST_TABLE="meter_usage"

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
# macOS: requires coreutils (gdate). Linux: use date -d.
date_add() {
  if command -v gdate &>/dev/null; then
    gdate -d "$1 +1 day" +"%Y-%m-%d"
  else
    date -d "$1 +1 day" +"%Y-%m-%d"
  fi
}

now_ts() {
  if command -v gdate &>/dev/null; then gdate -Iseconds; else date -Iseconds; fi
}

# Count rows already in destination for this tenant+env+day
dst_count_for_day() {
  local day="$1"
  ch --query "
    SELECT count()
    FROM ${DST_TABLE} FINAL
    WHERE tenant_id     = '${TENANT_ID}'
      AND environment_id = '${ENVIRONMENT_ID}'
      AND timestamp >= toDateTime('${day} 00:00:00')
      AND timestamp <  toDateTime('${day} 00:00:00') + INTERVAL 1 DAY
  " 2>/dev/null | tr -d '\r\n '
}

# Count rows in source for this tenant+env+day
src_count_for_day() {
  local day="$1"
  ch --query "
    SELECT count()
    FROM ${SRC_TABLE}
    WHERE tenant_id      = '${TENANT_ID}'
      AND environment_id = '${ENVIRONMENT_ID}'
      AND timestamp >= toDateTime64('${day} 00:00:00', 3)
      AND timestamp <  toDateTime64('${day} 00:00:00', 3) + INTERVAL 1 DAY
  " 2>/dev/null | tr -d '\r\n '
}

# ---------------------------
# CORE: migrate one day
# ---------------------------
migrate_day() {
  local day="$1"
  local log="$LOG_DIR/${day}.log"

  echo "[$(now_ts)] ===== START ${day} =====" | tee -a "$log"

  # -- idempotency check (bypassed when FORCE=1) --
  local already
  already="$(dst_count_for_day "$day" || echo "0")"
  already="$(echo "$already" | tr -d '\r\n ')"
  if [[ -n "$already" && "$already" != "0" ]]; then
    if [[ "${FORCE:-0}" == "1" ]]; then
      echo "[$(now_ts)] FORCE mode — destination has ${already} rows but re-inserting anyway" | tee -a "$log"
    else
      echo "[$(now_ts)] SKIP ${day} — destination already has ${already} rows (use FORCE=1 to override)" | tee -a "$log"
      return 0
    fi
  fi

  # -- source row count (skip empty days silently) --
  local src_cnt
  src_cnt="$(src_count_for_day "$day" || echo "0")"
  src_cnt="$(echo "$src_cnt" | tr -d '\r\n ')"
  echo "[$(now_ts)] Source rows for ${day}: ${src_cnt}" | tee -a "$log"

  if [[ "${src_cnt}" == "0" ]]; then
    echo "[$(now_ts)] SKIP ${day} — no source rows" | tee -a "$log"
    return 0
  fi

  # -- retry loop --
  local attempt=1
  while (( attempt <= MAX_RETRIES )); do
    echo "[$(now_ts)] Attempt ${attempt}/${MAX_RETRIES} for ${day}" | tee -a "$log"

    local query
    query="INSERT INTO ${DST_TABLE}
    (
        id,
        tenant_id,
        environment_id,
        external_customer_id,
        meter_id,
        event_name,
        timestamp,
        ingested_at,
        qty_total,
        unique_hash,
        source,
        properties
    )
SELECT
    id,
    tenant_id,
    environment_id,
    external_customer_id,
    COALESCE(meter_id, '')                   AS meter_id,
    event_name,
    toDateTime(timestamp)                    AS timestamp,
    processed_at                             AS ingested_at,
    CAST(qty_total, 'Decimal(18,8)')         AS qty_total,
    ''                                       AS unique_hash,
    COALESCE(source, '')                     AS source,
    ''                                       AS properties
FROM ${SRC_TABLE}
WHERE tenant_id      = '${TENANT_ID}'
  AND environment_id = '${ENVIRONMENT_ID}'
  AND timestamp >= toDateTime64('${day} 00:00:00', 3)
  AND timestamp <  toDateTime64('${day} 00:00:00', 3) + INTERVAL 1 DAY
SETTINGS
    max_execution_time     = ${MAX_EXEC_TIME},
    max_memory_usage       = 8000000000,
    max_insert_block_size  = 1048576,
    max_threads            = 4,
    max_insert_threads     = 2"

    echo "$query" >> "$log"
    echo "---" >> "$log"

    local result exit_code
    result=$(echo "$query" | ch 2>&1)
    exit_code=$?
    echo "$result" >> "$log"
    echo "[$(now_ts)] INSERT exit code: ${exit_code}" | tee -a "$log"

    if [[ $exit_code -eq 0 ]]; then
      [[ "${VERIFY_SLEEP_SEC:-0}" -gt 0 ]] && sleep "${VERIFY_SLEEP_SEC}"
      local dst_cnt
      dst_cnt="$(dst_count_for_day "$day" || echo "unknown")"
      echo "[$(now_ts)] DONE ${day} — src=${src_cnt} dst=${dst_cnt}" | tee -a "$log"
      return 0
    fi

    local sleep_for=$(( BASE_BACKOFF_SEC * attempt ))
    echo "[$(now_ts)] FAIL ${day} attempt ${attempt}. Sleeping ${sleep_for}s then retry..." | tee -a "$log"
    sleep "$sleep_for"
    attempt=$(( attempt + 1 ))
  done

  echo "[$(now_ts)] ERROR: Giving up on ${day} after ${MAX_RETRIES} attempts" | tee -a "$log"
  return 1
}

export -f migrate_day
export -f dst_count_for_day
export -f src_count_for_day
export -f ch
export -f date_add
export -f now_ts

# ---------------------------
# MAIN: build day list and run
# ---------------------------
days=()
d="$START_DATE"
while [[ "$d" != "$END_DATE_EXCL" ]]; do
  days+=("$d")
  d="$(date_add "$d")"
done

FORCE_LABEL="no (use FORCE=1 to re-insert existing days)"
[[ "${FORCE:-0}" == "1" ]] && FORCE_LABEL="YES — will re-insert even if destination has rows"
echo "============================================="
echo "  feature_usage → meter_usage migration"
echo "  Tenant:      ${TENANT_ID}"
echo "  Environment: ${ENVIRONMENT_ID}"
echo "  Range:       ${START_DATE} → ${END_DATE_EXCL} (exclusive)"
echo "  Days:        ${#days[@]}"
echo "  Parallelism: ${PARALLEL}"
echo "  Force:       ${FORCE_LABEL}"
echo "  Logs:        ${LOG_DIR}"
echo "============================================="

export CH_HOST CH_PORT CH_USER CH_PASSWORD CH_DB
export TENANT_ID ENVIRONMENT_ID
export SRC_TABLE DST_TABLE
export MAX_RETRIES BASE_BACKOFF_SEC MAX_EXEC_TIME VERIFY_SLEEP_SEC
export CONNECT_TIMEOUT_SEC SEND_TIMEOUT_SEC RECEIVE_TIMEOUT_SEC
export FORCE
export LOG_DIR

if command -v parallel &>/dev/null && [[ "${PARALLEL}" -gt 1 ]]; then
  export -f migrate_day dst_count_for_day src_count_for_day ch date_add now_ts
  printf "%s\n" "${days[@]}" | parallel -j "$PARALLEL" migrate_day \
    1> "$LOG_DIR/run.stdout.log" 2> "$LOG_DIR/run.stderr.log"
  echo "Parallel run complete. Check ${LOG_DIR}/"
else
  for day in "${days[@]}"; do
    migrate_day "$day"
  done
fi

echo ""
echo "All done. Logs: ${LOG_DIR}/"

: '
==================================================
USAGE EXAMPLES
==================================================

Dry run for 2 days (Mar 1 → Mar 2, so just Mar 1):
  cd scripts/bash
  source .env.backfill
  START_DATE=2026-03-01 END_DATE_EXCL=2026-03-02 bash migrate_feature_usage_to_meter_usage.sh

Full March:
  source .env.backfill
  START_DATE=2026-03-01 END_DATE_EXCL=2026-04-01 bash migrate_feature_usage_to_meter_usage.sh

Lower parallelism for safety (sequential):
  PARALLEL=1 START_DATE=2026-03-01 END_DATE_EXCL=2026-04-01 bash migrate_feature_usage_to_meter_usage.sh

Override tenant/env at runtime:
  TENANT_ID=tenant_xxx ENVIRONMENT_ID=env_yyy START_DATE=... END_DATE_EXCL=... bash migrate_feature_usage_to_meter_usage.sh

==================================================
CONFIGURABLE ENVIRONMENT VARIABLES
==================================================

ClickHouse:   CH_HOST, CH_PORT, CH_USER, CH_PASSWORD, CH_DB
Scope:        TENANT_ID, ENVIRONMENT_ID
Dates:        START_DATE, END_DATE_EXCL (exclusive end)
Execution:    PARALLEL (default 3), MAX_RETRIES (default 5),
              BASE_BACKOFF_SEC (default 10), MAX_EXEC_TIME (default 3600)
              VERIFY_SLEEP_SEC (default 5)
Logs:         LOG_DIR (default ./logs/migrate_feature_usage_to_meter_usage)
'
