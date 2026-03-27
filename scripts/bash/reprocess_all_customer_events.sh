#!/usr/bin/env bash
#
# Enqueues ALL raw events for one or more customers to reprocess via the
# reprocess API — regardless of whether they already exist in the
# 'events' or 'feature_usage' tables.
#
# Use this when you need to fully reprocess every event for a customer
# (e.g., after a pricing/plan change, meter reconfiguration, etc.).
#
# Memory-safe: single query per customer dumps all IDs to a file, then
# batches API calls from the file — no repeated ClickHouse queries.
#
# Prerequisites: clickhouse (client), curl, jq
#
# Usage:
#   # Single customer
#   source .env.backfill && EXTERNAL_CUSTOMER_IDS="cust_123" ./reprocess_all_customer_events.sh
#
#   # Multiple customers (comma-separated)
#   source .env.backfill && EXTERNAL_CUSTOMER_IDS="cust_1,cust_2,cust_3" ./reprocess_all_customer_events.sh
#
#   # Multiple customers from a file (one ID per line)
#   source .env.backfill && CUSTOMERS_FILE=./customer_ids.txt ./reprocess_all_customer_events.sh
#
set -euo pipefail

###############################################################################
# Parameters
###############################################################################
TENANT_ID="${TENANT_ID:?TENANT_ID is required}"
ENVIRONMENT_ID="${ENVIRONMENT_ID:?ENVIRONMENT_ID is required}"
START_DATE="${START_DATE:?START_DATE is required (ISO-8601, e.g. 2026-02-01T00:00:00Z)}"
END_DATE="${END_DATE:?END_DATE is required (ISO-8601, e.g. 2026-02-06T00:00:00Z)}"

# Customer IDs: comma-separated env var OR file with one ID per line
EXTERNAL_CUSTOMER_IDS="${EXTERNAL_CUSTOMER_IDS:-}"
CUSTOMERS_FILE="${CUSTOMERS_FILE:-}"

API_KEY="${API_KEY:-flexprice-api-key}"
API_URL="${API_URL:-https://us.api.flexprice.io/v1/events/raw/reprocess/all}"
BATCH_SIZE="${BATCH_SIZE:-20000}"
API_CHUNK_SIZE="${API_CHUNK_SIZE:-5000}"
API_PARALLEL="${API_PARALLEL:-10}"
DRY_RUN="${DRY_RUN:-false}"
SLEEP_BETWEEN_BATCHES="${SLEEP_BETWEEN_BATCHES:-1}"

# ClickHouse memory safety — applied per query
CH_MAX_MEMORY="${CH_MAX_MEMORY:-8000000000}"  # 8 GB per query

# ClickHouse connection (matches .env.backfill)
CH_HOST="${CH_HOST:-127.0.0.1}"
CH_PORT="${CH_PORT:-9000}"
CH_USER="${CH_USER:-default}"
CH_PASSWORD="${CH_PASSWORD:-}"
CH_DB="${CH_DB:-flexprice}"

###############################################################################
# Resolve customer list
###############################################################################
CUSTOMER_LIST=()

if [[ -n "$CUSTOMERS_FILE" ]]; then
  if [[ ! -f "$CUSTOMERS_FILE" ]]; then
    echo "ERROR: CUSTOMERS_FILE='${CUSTOMERS_FILE}' does not exist." >&2
    exit 1
  fi
  while IFS= read -r line; do
    line=$(echo "$line" | tr -d '[:space:]')
    [[ -n "$line" && "$line" != \#* ]] && CUSTOMER_LIST+=("$line")
  done < "$CUSTOMERS_FILE"
elif [[ -n "$EXTERNAL_CUSTOMER_IDS" ]]; then
  IFS=',' read -ra _ids <<< "$EXTERNAL_CUSTOMER_IDS"
  for _id in "${_ids[@]}"; do
    _id=$(echo "$_id" | tr -d '[:space:]')
    [[ -n "$_id" ]] && CUSTOMER_LIST+=("$_id")
  done
else
  echo "ERROR: Provide EXTERNAL_CUSTOMER_IDS (comma-separated) or CUSTOMERS_FILE." >&2
  exit 1
fi

if [[ ${#CUSTOMER_LIST[@]} -eq 0 ]]; then
  echo "ERROR: No customer IDs found." >&2
  exit 1
fi

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

ch_safe_query() {
  local query="$1"
  ch --query "
    SET max_memory_usage = ${CH_MAX_MEMORY};
    SET max_bytes_before_external_sort = $((CH_MAX_MEMORY / 2));
    SET max_bytes_before_external_group_by = $((CH_MAX_MEMORY / 2));
    ${query}
  "
}

send_chunk() {
  local chunk_num="$1" total_chunks="$2" chunk_count="$3" payload="$4"

  local api_tmp
  api_tmp=$(mktemp)
  local api_http
  api_http=$(curl -sS -o "$api_tmp" -w '%{http_code}' \
    --request POST \
    --url "$API_URL" \
    --header 'Content-Type: application/json' \
    --header "x-api-key: ${API_KEY}" \
    --max-time 120 \
    --data "$payload" 2>/dev/null) || api_http="000"
  local api_body
  api_body=$(cat "$api_tmp" 2>/dev/null || true)
  rm -f "$api_tmp"

  if [[ "$api_http" -ge 200 && "$api_http" -lt 300 ]]; then
    local wf_id
    wf_id=$(printf '%s' "$api_body" | jq -r '.workflow_id // empty' 2>/dev/null || true)
    log "  chunk ${chunk_num}/${total_chunks} (${chunk_count} IDs) -> OK (${api_http})  wf=${wf_id:-n/a}"
    echo "ok" > "${RESULT_DIR}/${chunk_num}"
  else
    log "  chunk ${chunk_num}/${total_chunks} (${chunk_count} IDs) -> FAIL (${api_http})"
    echo "fail" > "${RESULT_DIR}/${chunk_num}"
  fi
}

for cmd in clickhouse curl jq; do
  if ! command -v "$cmd" &>/dev/null; then
    log "ERROR: '$cmd' is required but not installed."
    exit 1
  fi
done

to_ch_ts() { printf '%s' "$1" | sed 's/T/ /;s/Z$//'; }

ch_start=$(to_ch_ts "$START_DATE")
ch_end=$(to_ch_ts "$END_DATE")

RESULT_DIR=$(mktemp -d)
ALL_IDS_FILE=$(mktemp)
trap 'rm -rf "$RESULT_DIR" "$ALL_IDS_FILE"' EXIT

###############################################################################
# Build event-name skip filter
###############################################################################
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SKIP_FILE="${EVENTS_TO_SKIP_FILE:-${SCRIPT_DIR}/events_to_skip.json}"
extra_event_filter=""
if [[ -f "$SKIP_FILE" ]]; then
  skip_count=$(jq '.events_to_skip | length' "$SKIP_FILE")
  if (( skip_count > 0 )); then
    skip_event_list=$(jq -r '.events_to_skip[]' "$SKIP_FILE" \
      | awk '{printf "%s'\''%s'\''", (NR>1 ? "," : ""), $0}')
    extra_event_filter="AND event_name NOT IN (${skip_event_list})"
  fi
  log "  -> ${skip_count} event names excluded (events_to_skip.json)"
fi

###############################################################################
# Banner
###############################################################################
TOTAL_CUSTOMERS=${#CUSTOMER_LIST[@]}
log "================================================================"
log " Reprocess ALL Customer Events (no ANTI JOIN)"
log "================================================================"
log " Tenant:       ${TENANT_ID}"
log " Environment:  ${ENVIRONMENT_ID}"
log " Customers:    ${TOTAL_CUSTOMERS}"
log " Time range:   ${START_DATE}  ->  ${END_DATE}"
log " CH batch:     ${BATCH_SIZE}"
log " API chunk:    ${API_CHUNK_SIZE}  (x${API_PARALLEL} parallel)"
log " Dry run:      ${DRY_RUN}"
log " API URL:      ${API_URL}"
log " ClickHouse:   ${CH_HOST}:${CH_PORT}/${CH_DB}"
log " CH max mem:   ${CH_MAX_MEMORY} bytes per query"
log "================================================================"

###############################################################################
# Process each customer
###############################################################################
process_customer() {
  local cid="$1"
  local cust_num="$2"

  log ""
  log "================================================================"
  log " Customer ${cust_num}/${TOTAL_CUSTOMERS}: ${cid}"
  log "================================================================"

  # Step 1: Dump ALL event IDs for this customer
  log "Fetching all event IDs ..."

  local QUERY="
  SELECT id
  FROM ${CH_DB}.raw_events
  PREWHERE tenant_id  = '${TENANT_ID}'
    AND environment_id = '${ENVIRONMENT_ID}'
    AND external_customer_id = '${cid}'
    AND timestamp >= toDateTime64('${ch_start}', 3)
    AND timestamp <  toDateTime64('${ch_end}', 3)
  WHERE field4 = 'false'
    AND field1 != 'custom-llm'
    ${extra_event_filter}
  "

  local q_start q_end query_elapsed total_count
  q_start=$(date +%s)
  : > "$ALL_IDS_FILE"  # truncate
  ch_safe_query "$QUERY" > "$ALL_IDS_FILE"
  q_end=$(date +%s)
  query_elapsed=$((q_end - q_start))

  total_count=$(wc -l < "$ALL_IDS_FILE" | tr -d '[:space:]')
  log "  -> ${total_count} event IDs found in ${query_elapsed}s"

  if [[ "$total_count" == "0" ]]; then
    log "Nothing to do for this customer. Skipping."
    return 0
  fi

  # Step 2: Stream the file in batches for API calls
  log "Sending ${total_count} event IDs to API in batches of ${BATCH_SIZE} ..."

  local batch_num=0
  local total_processed=0
  local total_api_ok=0
  local total_api_fail=0
  local start_epoch
  start_epoch=$(date +%s)

  while true; do
    batch_num=$((batch_num + 1))

    local offset=$((total_processed))
    local batch_ids
    batch_ids=$(sed -n "$((offset + 1)),$((offset + BATCH_SIZE))p" "$ALL_IDS_FILE")

    if [[ -z "$batch_ids" ]]; then
      log "All IDs processed."
      break
    fi

    local batch_count
    batch_count=$(printf '%s\n' "$batch_ids" | wc -l)
    batch_count=$(echo "$batch_count" | tr -d '[:space:]')
    log "--- Batch ${batch_num}  (${total_processed}/${total_count}) — ${batch_count} IDs ---"

    local all_ids=()
    while IFS= read -r _id; do
      [[ -n "$_id" ]] && all_ids+=("$_id")
    done <<< "$batch_ids"

    rm -f "${RESULT_DIR}"/*

    local total_ids=${#all_ids[@]}
    local total_chunks=$(( (total_ids + API_CHUNK_SIZE - 1) / API_CHUNK_SIZE ))
    local chunk_num=0
    local in_flight=0

    log "Sending ${total_chunks} API chunks (${API_PARALLEL} parallel) ..."

    for (( i=0; i < total_ids; i += API_CHUNK_SIZE )); do
      chunk_num=$((chunk_num + 1))
      local chunk_arr=("${all_ids[@]:i:API_CHUNK_SIZE}")
      local chunk_count=${#chunk_arr[@]}

      local chunk_json
      chunk_json=$(printf '%s\n' "${chunk_arr[@]}" \
        | jq -R -s 'split("\n") | map(select(length > 0))')

      local cust_ids_json
      cust_ids_json=$(jq -n --arg cid "$cid" '[$cid]')

      local payload
      payload=$(jq -n \
        --arg start_date  "$START_DATE" \
        --arg end_date    "$END_DATE" \
        --argjson batch_size "$API_CHUNK_SIZE" \
        --argjson event_ids  "$chunk_json" \
        --argjson external_customer_ids "$cust_ids_json" \
        '{
          start_date:            $start_date,
          end_date:              $end_date,
          batch_size:            $batch_size,
          event_ids:             $event_ids,
          external_customer_ids: $external_customer_ids
        }')

      if [[ "$DRY_RUN" == "true" ]]; then
        log "  [DRY RUN] chunk ${chunk_num}/${total_chunks}: ${chunk_count} IDs"
      else
        send_chunk "$chunk_num" "$total_chunks" "$chunk_count" "$payload" &
        in_flight=$((in_flight + 1))

        if (( in_flight >= API_PARALLEL )); then
          wait
          in_flight=0
        fi
      fi
    done

    if (( in_flight > 0 )); then
      wait
    fi

    local batch_ok batch_fail
    batch_ok=$( (grep -rl 'ok' "${RESULT_DIR}" 2>/dev/null || true) | wc -l | tr -d '[:space:]')
    batch_fail=$( (grep -rl 'fail' "${RESULT_DIR}" 2>/dev/null || true) | wc -l | tr -d '[:space:]')
    total_api_ok=$((total_api_ok + batch_ok))
    total_api_fail=$((total_api_fail + batch_fail))

    total_processed=$((total_processed + batch_count))

    local now_epoch elapsed rate remaining remaining_min
    now_epoch=$(date +%s)
    elapsed=$((now_epoch - start_epoch))
    if (( elapsed > 0 )); then
      rate=$((total_processed * 60 / elapsed))
      remaining=$(( (total_count - total_processed) * elapsed / total_processed ))
      remaining_min=$((remaining / 60))
      log "Speed: ~${rate} events/min | ETA: ~${remaining_min}m | API: ${batch_ok} ok, ${batch_fail} fail"
    fi

    if [[ "$batch_count" -lt "$BATCH_SIZE" ]]; then
      log "Last batch."
      break
    fi

    if [[ "$SLEEP_BETWEEN_BATCHES" -gt 0 ]]; then
      sleep "$SLEEP_BETWEEN_BATCHES"
    fi
  done

  log "Customer ${cid}: ${total_processed} events, ${total_api_ok} ok, ${total_api_fail} fail"
  # Return counts via global accumulators
  GRAND_EVENTS=$((GRAND_EVENTS + total_processed))
  GRAND_OK=$((GRAND_OK + total_api_ok))
  GRAND_FAIL=$((GRAND_FAIL + total_api_fail))
}

###############################################################################
# Main loop
###############################################################################
GRAND_EVENTS=0
GRAND_OK=0
GRAND_FAIL=0
GRAND_CUSTOMERS_OK=0
GRAND_CUSTOMERS_FAIL=0
global_start=$(date +%s)

for idx in $(seq 0 $((TOTAL_CUSTOMERS - 1))); do
  cid="${CUSTOMER_LIST[$idx]}"
  cust_num=$((idx + 1))
  if process_customer "$cid" "$cust_num"; then
    GRAND_CUSTOMERS_OK=$((GRAND_CUSTOMERS_OK + 1))
  else
    GRAND_CUSTOMERS_FAIL=$((GRAND_CUSTOMERS_FAIL + 1))
    log "WARNING: Failed for customer ${cid}"
  fi
done

###############################################################################
# Grand summary
###############################################################################
global_end=$(date +%s)
global_elapsed=$(( global_end - global_start ))
global_min=$(( global_elapsed / 60 ))
global_sec=$(( global_elapsed % 60 ))

log ""
log "================================================================"
log " ALL DONE"
log "   Customers:       ${TOTAL_CUSTOMERS} (${GRAND_CUSTOMERS_OK} ok, ${GRAND_CUSTOMERS_FAIL} fail)"
log "   Total events:    ${GRAND_EVENTS}"
log "   Time:            ${global_min}m ${global_sec}s"
if [[ "$DRY_RUN" != "true" ]]; then
  log "   API successes:   ${GRAND_OK}"
  log "   API failures:    ${GRAND_FAIL}"
fi
log "================================================================"


: '
==================================================
USAGE EXAMPLES
==================================================

Single customer:
  source .env.backfill && \
  START_DATE=2026-02-01T00:00:00Z END_DATE=2026-03-12T00:00:00Z \
  EXTERNAL_CUSTOMER_IDS="cust_123" \
  ./reprocess_all_customer_events.sh

Multiple customers (comma-separated):
  source .env.backfill && \
  START_DATE=2026-02-01T00:00:00Z END_DATE=2026-03-12T00:00:00Z \
  EXTERNAL_CUSTOMER_IDS="cust_1,cust_2,cust_3" \
  ./reprocess_all_customer_events.sh

Multiple customers from a file (one ID per line, # comments allowed):
  source .env.backfill && \
  START_DATE=2026-02-01T00:00:00Z END_DATE=2026-03-12T00:00:00Z \
  CUSTOMERS_FILE=./customer_ids.txt \
  ./reprocess_all_customer_events.sh

Dry run:
  source .env.backfill && \
  START_DATE=2026-02-01T00:00:00Z END_DATE=2026-03-12T00:00:00Z \
  EXTERNAL_CUSTOMER_IDS="cust_1,cust_2" \
  DRY_RUN=true \
  ./reprocess_all_customer_events.sh

==================================================
CONFIGURABLE ENVIRONMENT VARIABLES
==================================================

Required:
  TENANT_ID              - Tenant ID
  ENVIRONMENT_ID         - Environment ID
  START_DATE             - Start date ISO-8601 (e.g. 2026-02-01T00:00:00Z)
  END_DATE               - End date ISO-8601 (e.g. 2026-03-12T00:00:00Z)
  EXTERNAL_CUSTOMER_IDS  - Comma-separated customer IDs (or use CUSTOMERS_FILE)
  CUSTOMERS_FILE         - Path to file with one customer ID per line

ClickHouse Connection:
  CH_HOST          - ClickHouse host (default: 127.0.0.1)
  CH_PORT          - ClickHouse port (default: 9000)
  CH_USER          - ClickHouse user (default: default)
  CH_PASSWORD      - ClickHouse password (default: empty)
  CH_DB            - ClickHouse database (default: flexprice)

API:
  API_KEY          - FlexPrice API key
  API_URL          - Reprocess API URL

Tuning:
  BATCH_SIZE              - IDs per ClickHouse read batch (default: 20000)
  API_CHUNK_SIZE          - IDs per API call (default: 5000)
  API_PARALLEL            - Parallel API calls (default: 10)
  SLEEP_BETWEEN_BATCHES   - Sleep seconds between batches (default: 1)
  CH_MAX_MEMORY           - ClickHouse per-query memory limit (default: 8GB)
  DRY_RUN                 - true to skip API calls (default: false)

==================================================
'
