#!/usr/bin/env bash
set -euo pipefail

# ---------------------------
# migrate_feature_usage_to_meter_usage.sh
#
# Migrates rows from flexprice.feature_usage → flexprice.meter_usage
# for a specific tenant+environment, day by day.
#
# USAGE:
#   # Option A — script auto-loads scripts/bash/.env.backfill
#   START_DATE=2026-03-01 END_DATE_EXCL=2026-03-02 ./scripts/bash/migrate_feature_usage_to_meter_usage.sh
#
#   # Option B — export everything from the env file first (set -a / set +a)
#   set -a; source scripts/bash/.env.backfill; set +a
#   START_DATE=2026-03-01 END_DATE_EXCL=2026-03-02 ./scripts/bash/migrate_feature_usage_to_meter_usage.sh
#
#   # Filter to one event / customer (re-run with FORCE=1 if dst already has rows)
#   ./scripts/bash/migrate_feature_usage_to_meter_usage.sh \
#     --event-name api_call --external-customer-id cust_abc \
#     --start-date 2026-03-01 --end-date-excl 2026-03-02
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
#   properties      → direct
# ---------------------------

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ENV_FILE="${ENV_FILE:-${SCRIPT_DIR}/.env.backfill}"

show_help() {
  cat <<EOF
Usage: $(basename "$0") [OPTIONS]

Migrates feature_usage → meter_usage day-by-day for TENANT_ID + ENVIRONMENT_ID.

Options (override env vars):
  --event-name, -e NAME              Only migrate rows with this event_name
  --external-customer-id, -c ID      Only migrate rows for this external_customer_id
  --source, -s SOURCE                Only migrate rows with this source value
  --start-date DATE                  START_DATE (YYYY-MM-DD)
  --end-date-excl DATE               END_DATE_EXCL, exclusive (YYYY-MM-DD)
  --no-env-file                      Skip auto-loading ${ENV_FILE}
  --help, -h                         Show this help

Env file:
  By default, sources ${ENV_FILE} when present (set -a / set +a).
  Or pre-load yourself:  set -a; source scripts/bash/.env.backfill; set +a

Common env vars:
  CH_HOST, CH_PORT, CH_USER, CH_PASSWORD, CH_DB
  TENANT_ID, ENVIRONMENT_ID
  START_DATE, END_DATE_EXCL
  EVENT_NAME, EXTERNAL_CUSTOMER_ID, SOURCE  (same as CLI flags)
  PARALLEL, FORCE, LOG_DIR

Examples:
  START_DATE=2026-03-01 END_DATE_EXCL=2026-03-02 $0
  FORCE=1 $0 -e api_call -c cust_abc --start-date 2026-03-01 --end-date-excl 2026-03-02
EOF
}

# ---- Optional CLI flags (applied after env file) ----
EVENT_NAME="${EVENT_NAME:-}"
EXTERNAL_CUSTOMER_ID="${EXTERNAL_CUSTOMER_ID:-}"
SOURCE="${SOURCE:-}"
SKIP_ENV_FILE="${SKIP_ENV_FILE:-0}"

ORIG_ARGS=("$@")
for arg in "${ORIG_ARGS[@]}"; do
  [[ "$arg" == "--no-env-file" ]] && SKIP_ENV_FILE=1
done

# Auto-load .env.backfill unless disabled
if [[ "$SKIP_ENV_FILE" != "1" && -f "$ENV_FILE" ]]; then
  set -a
  # shellcheck source=/dev/null
  source "$ENV_FILE"
  set +a
fi

set -- "${ORIG_ARGS[@]}"
while [[ $# -gt 0 ]]; do
  case "$1" in
    --event-name|-e)
      EVENT_NAME="$2"
      shift 2
      ;;
    --external-customer-id|-c)
      EXTERNAL_CUSTOMER_ID="$2"
      shift 2
      ;;
    --source|-s)
      SOURCE="$2"
      shift 2
      ;;
    --start-date)
      START_DATE="$2"
      shift 2
      ;;
    --end-date-excl)
      END_DATE_EXCL="$2"
      shift 2
      ;;
    --no-env-file)
      shift
      ;;
    --help|-h)
      show_help
      exit 0
      ;;
    *)
      echo "Unknown argument: $1 (try --help)" >&2
      exit 1
      ;;
  esac
done

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
sql_escape() {
  printf '%s' "$1" | sed "s/'/''/g"
}

# Extra WHERE fragments shared by src/dst counts and INSERT
sql_extra_where_src() {
  local extra=""
  if [[ -n "${EVENT_NAME}" ]]; then
    extra+=" AND event_name = '$(sql_escape "${EVENT_NAME}")'"
  fi
  if [[ -n "${EXTERNAL_CUSTOMER_ID}" ]]; then
    extra+=" AND external_customer_id = '$(sql_escape "${EXTERNAL_CUSTOMER_ID}")'"
  fi
  if [[ -n "${SOURCE}" ]]; then
    extra+=" AND COALESCE(source, '') = '$(sql_escape "${SOURCE}")'"
  fi
  printf '%s' "$extra"
}

sql_extra_where_dst() {
  local extra=""
  if [[ -n "${EVENT_NAME}" ]]; then
    extra+=" AND event_name = '$(sql_escape "${EVENT_NAME}")'"
  fi
  if [[ -n "${EXTERNAL_CUSTOMER_ID}" ]]; then
    extra+=" AND external_customer_id = '$(sql_escape "${EXTERNAL_CUSTOMER_ID}")'"
  fi
  if [[ -n "${SOURCE}" ]]; then
    extra+=" AND source = '$(sql_escape "${SOURCE}")'"
  fi
  printf '%s' "$extra"
}

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
# </dev/null prevents terminal XTGETTCAP responses from leaking into the query
dst_count_for_day() {
  local day="$1"
  local extra_where
  extra_where="$(sql_extra_where_dst)"
  ch --query "
    SELECT count()
    FROM ${DST_TABLE} FINAL
    WHERE tenant_id     = '${TENANT_ID}'
      AND environment_id = '${ENVIRONMENT_ID}'
      AND timestamp >= toDateTime('${day} 00:00:00')
      AND timestamp <  toDateTime('${day} 00:00:00') + INTERVAL 1 DAY
      ${extra_where}
  " </dev/null 2>/dev/null | tr -d '\r\n '
}

# Count rows in source for this tenant+env+day
src_count_for_day() {
  local day="$1"
  local extra_where
  extra_where="$(sql_extra_where_src)"
  ch --query "
    SELECT count()
    FROM ${SRC_TABLE}
    WHERE tenant_id      = '${TENANT_ID}'
      AND environment_id = '${ENVIRONMENT_ID}'
      AND timestamp >= toDateTime64('${day} 00:00:00', 3)
      AND timestamp <  toDateTime64('${day} 00:00:00', 3) + INTERVAL 1 DAY
      ${extra_where}
  " </dev/null 2>/dev/null | tr -d '\r\n '
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

    local extra_where
    extra_where="$(sql_extra_where_src)"

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
    properties                               AS properties
FROM ${SRC_TABLE}
WHERE tenant_id      = '${TENANT_ID}'
  AND environment_id = '${ENVIRONMENT_ID}'
  AND timestamp >= toDateTime64('${day} 00:00:00', 3)
  AND timestamp <  toDateTime64('${day} 00:00:00', 3) + INTERVAL 1 DAY
  ${extra_where}
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
export -f sql_extra_where_src
export -f sql_extra_where_dst
export -f sql_escape
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
[[ -n "${EVENT_NAME}" ]] && echo "  Event name:  ${EVENT_NAME}"
[[ -n "${EXTERNAL_CUSTOMER_ID}" ]] && echo "  Customer:    ${EXTERNAL_CUSTOMER_ID}"
[[ -n "${SOURCE}" ]] && echo "  Source:      ${SOURCE}"
echo "  Env file:    ${ENV_FILE} ($([[ -f "$ENV_FILE" ]] && echo loaded || echo not found))"
echo "  Logs:        ${LOG_DIR}"
echo "============================================="

export CH_HOST CH_PORT CH_USER CH_PASSWORD CH_DB
export TENANT_ID ENVIRONMENT_ID
export SRC_TABLE DST_TABLE
export MAX_RETRIES BASE_BACKOFF_SEC MAX_EXEC_TIME VERIFY_SLEEP_SEC
export CONNECT_TIMEOUT_SEC SEND_TIMEOUT_SEC RECEIVE_TIMEOUT_SEC
export FORCE
export LOG_DIR
export EVENT_NAME EXTERNAL_CUSTOMER_ID SOURCE

if command -v parallel &>/dev/null && [[ "${PARALLEL}" -gt 1 ]]; then
  export -f migrate_day dst_count_for_day src_count_for_day sql_extra_where_src sql_extra_where_dst sql_escape ch date_add now_ts
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
  START_DATE=2026-03-01 END_DATE_EXCL=2026-03-02 bash scripts/bash/migrate_feature_usage_to_meter_usage.sh

Full March (env auto-loaded from scripts/bash/.env.backfill):
  START_DATE=2026-03-01 END_DATE_EXCL=2026-04-01 bash scripts/bash/migrate_feature_usage_to_meter_usage.sh

Explicit env export (set -a exports all assignments from the file):
  set -a; source scripts/bash/.env.backfill; set +a
  START_DATE=2026-03-01 END_DATE_EXCL=2026-04-01 bash scripts/bash/migrate_feature_usage_to_meter_usage.sh

Filter to one event + customer:
  bash scripts/bash/migrate_feature_usage_to_meter_usage.sh \
    --event-name api_call --external-customer-id cust_abc \
    --start-date 2026-03-01 --end-date-excl 2026-03-02

Re-insert filtered rows when destination already has data:
  FORCE=1 bash scripts/bash/migrate_feature_usage_to_meter_usage.sh -e api_call -c cust_abc \
    --start-date 2026-03-01 --end-date-excl 2026-03-02

Lower parallelism for safety (sequential):
  PARALLEL=1 START_DATE=2026-03-01 END_DATE_EXCL=2026-04-01 bash scripts/bash/migrate_feature_usage_to_meter_usage.sh

Override tenant/env at runtime:
  TENANT_ID=tenant_xxx ENVIRONMENT_ID=env_yyy START_DATE=... END_DATE_EXCL=... bash scripts/bash/migrate_feature_usage_to_meter_usage.sh

==================================================
CONFIGURABLE ENVIRONMENT VARIABLES
==================================================

ClickHouse:   CH_HOST, CH_PORT, CH_USER, CH_PASSWORD, CH_DB
Scope:        TENANT_ID, ENVIRONMENT_ID
Filters:      EVENT_NAME, EXTERNAL_CUSTOMER_ID, SOURCE (or --event-name / --external-customer-id / --source)
Dates:        START_DATE, END_DATE_EXCL (exclusive end)
Execution:    PARALLEL (default 3), MAX_RETRIES (default 5),
              BASE_BACKOFF_SEC (default 10), MAX_EXEC_TIME (default 3600)
              VERIFY_SLEEP_SEC (default 5), FORCE (default 0)
Env file:     ENV_FILE (default scripts/bash/.env.backfill), SKIP_ENV_FILE=1 to disable auto-load
Logs:         LOG_DIR (default ./logs/migrate_feature_usage_to_meter_usage)
'
