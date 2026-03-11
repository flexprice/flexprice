#!/usr/bin/env bash
#
# Enqueues ALL raw events for a given customer to reprocess via the
# reprocess API — regardless of whether they already exist in the
# 'events' or 'feature_usage' tables.
#
# Use this when you need to fully reprocess every event for a customer
# (e.g., after a pricing/plan change, meter reconfiguration, etc.).
#
# Memory-safe: single query dumps all IDs to a file, then batches API
# calls from the file — no repeated ClickHouse queries.
#
# Prerequisites: clickhouse (client), curl, jq
#
# Usage:
#   source .env.backfill && ./reprocess_all_customer_events.sh
#
set -euo pipefail

###############################################################################
# Parameters
###############################################################################
TENANT_ID="${TENANT_ID:?TENANT_ID is required}"
ENVIRONMENT_ID="${ENVIRONMENT_ID:?ENVIRONMENT_ID is required}"
EXTERNAL_CUSTOMER_ID="${EXTERNAL_CUSTOMER_ID:?EXTERNAL_CUSTOMER_ID is required}"
START_DATE="${START_DATE:?START_DATE is required (ISO-8601, e.g. 2026-02-01T00:00:00Z)}"
END_DATE="${END_DATE:?END_DATE is required (ISO-8601, e.g. 2026-02-06T00:00:00Z)}"

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

# Memory-capped ClickHouse query
ch_safe_query() {
  local query="$1"
  ch --query "
    SET max_memory_usage = ${CH_MAX_MEMORY};
    SET max_bytes_before_external_sort = $((CH_MAX_MEMORY / 2));
    SET max_bytes_before_external_group_by = $((CH_MAX_MEMORY / 2));
    ${query}
  "
}

# Sends one API chunk. Writes "ok" or "fail" to $RESULT_DIR/<chunk_num>.
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

# Convert ISO-8601 timestamp to ClickHouse DateTime string
to_ch_ts() { printf '%s' "$1" | sed 's/T/ /;s/Z$//'; }

ch_start=$(to_ch_ts "$START_DATE")
ch_end=$(to_ch_ts "$END_DATE")

# Temp files
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
log "================================================================"
log " Reprocess ALL Customer Events (no ANTI JOIN)"
log "================================================================"
log " Tenant:       ${TENANT_ID}"
log " Environment:  ${ENVIRONMENT_ID}"
log " Customer:     ${EXTERNAL_CUSTOMER_ID}"
log " Time range:   ${START_DATE}  ->  ${END_DATE}"
log " CH batch:     ${BATCH_SIZE}"
log " API chunk:    ${API_CHUNK_SIZE}  (x${API_PARALLEL} parallel)"
log " Dry run:      ${DRY_RUN}"
log " API URL:      ${API_URL}"
log " ClickHouse:   ${CH_HOST}:${CH_PORT}/${CH_DB}"
log " CH max mem:   ${CH_MAX_MEMORY} bytes per query"
log "================================================================"

###############################################################################
# Step 1: Dump ALL event IDs for this customer
#
# Straight SELECT on raw_events — no ANTI JOIN.
# raw_events ORDER BY: (tenant_id, environment_id, external_customer_id, timestamp, id)
#   → PREWHERE on tenant+env+customer+ts is fully index-aligned.
###############################################################################
log ""
log "Fetching all event IDs for customer '${EXTERNAL_CUSTOMER_ID}' ..."

QUERY="
SELECT id
FROM ${CH_DB}.raw_events
PREWHERE tenant_id  = '${TENANT_ID}'
  AND environment_id = '${ENVIRONMENT_ID}'
  AND external_customer_id = '${EXTERNAL_CUSTOMER_ID}'
  AND timestamp >= toDateTime64('${ch_start}', 3)
  AND timestamp <  toDateTime64('${ch_end}', 3)
WHERE field4 = 'false'
  AND field1 != 'custom-llm'
  ${extra_event_filter}
"

log "  Source: raw_events  PREWHERE tenant+env+customer+ts  WHERE field4='false'"
log ""

q_start=$(date +%s)
ch_safe_query "$QUERY" > "$ALL_IDS_FILE"
q_end=$(date +%s)
query_elapsed=$((q_end - q_start))

total_count=$(wc -l < "$ALL_IDS_FILE" | tr -d '[:space:]')
log "  -> ${total_count} event IDs found in ${query_elapsed}s"

if [[ "$total_count" == "0" ]]; then
  log "Nothing to do. Exiting."
  exit 0
fi

###############################################################################
# Step 2: Stream the file in batches for API calls
###############################################################################
log ""
log "Sending ${total_count} event IDs to API in batches of ${BATCH_SIZE} ..."

batch_num=0
total_processed=0
total_api_ok=0
total_api_fail=0
start_epoch=$(date +%s)

while true; do
  batch_num=$((batch_num + 1))

  # Read next BATCH_SIZE lines from the file
  offset=$((total_processed))
  batch_ids=$(sed -n "$((offset + 1)),$((offset + BATCH_SIZE))p" "$ALL_IDS_FILE")

  if [[ -z "$batch_ids" ]]; then
    log "All IDs processed. Done."
    break
  fi

  batch_count=$(printf '%s\n' "$batch_ids" | wc -l)
  batch_count=$(echo "$batch_count" | tr -d '[:space:]')
  log "--- Batch ${batch_num}  (${total_processed}/${total_count}) — ${batch_count} IDs ---"

  # Load IDs into array
  all_ids=()
  while IFS= read -r _id; do
    [[ -n "$_id" ]] && all_ids+=("$_id")
  done <<< "$batch_ids"

  # Clear result dir for this batch
  rm -f "${RESULT_DIR}"/*

  total_ids=${#all_ids[@]}
  total_chunks=$(( (total_ids + API_CHUNK_SIZE - 1) / API_CHUNK_SIZE ))
  chunk_num=0
  in_flight=0

  log "Sending ${total_chunks} API chunks (${API_PARALLEL} parallel) ..."

  for (( i=0; i < total_ids; i += API_CHUNK_SIZE )); do
    chunk_num=$((chunk_num + 1))
    chunk_arr=("${all_ids[@]:i:API_CHUNK_SIZE}")
    chunk_count=${#chunk_arr[@]}

    chunk_json=$(printf '%s\n' "${chunk_arr[@]}" \
      | jq -R -s 'split("\n") | map(select(length > 0))')

    cust_ids_json=$(jq -n --arg cid "$EXTERNAL_CUSTOMER_ID" '[$cid]')

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

  # Wait for any remaining in-flight requests
  if (( in_flight > 0 )); then
    wait
  fi

  # Tally results from this batch
  batch_ok=$( (grep -rl 'ok' "${RESULT_DIR}" 2>/dev/null || true) | wc -l | tr -d '[:space:]')
  batch_fail=$( (grep -rl 'fail' "${RESULT_DIR}" 2>/dev/null || true) | wc -l | tr -d '[:space:]')
  total_api_ok=$((total_api_ok + batch_ok))
  total_api_fail=$((total_api_fail + batch_fail))

  total_processed=$((total_processed + batch_count))

  # Calculate speed
  now_epoch=$(date +%s)
  elapsed=$((now_epoch - start_epoch))
  if (( elapsed > 0 )); then
    rate=$((total_processed * 60 / elapsed))
    remaining=$(( (total_count - total_processed) * elapsed / total_processed ))
    remaining_min=$((remaining / 60))
    log "Speed: ~${rate} events/min | ETA: ~${remaining_min}m | API: ${batch_ok} ok, ${batch_fail} fail"
  fi

  if [[ "$batch_count" -lt "$BATCH_SIZE" ]]; then
    log "Last batch. Done."
    break
  fi

  if [[ "$SLEEP_BETWEEN_BATCHES" -gt 0 ]]; then
    sleep "$SLEEP_BETWEEN_BATCHES"
  fi
done

###############################################################################
# Summary
###############################################################################
end_epoch=$(date +%s)
total_elapsed=$(( end_epoch - start_epoch ))
total_min=$(( total_elapsed / 60 ))
total_sec=$(( total_elapsed % 60 ))

log "================================================================"
log " Done!"
log "   Total events:    ${total_processed}"
log "   Batches:         ${batch_num}"
log "   Time:            ${total_min}m ${total_sec}s"
if [[ "$DRY_RUN" != "true" ]]; then
  log "   API successes:   ${total_api_ok}"
  log "   API failures:    ${total_api_fail}"
fi
log "================================================================"
