#!/usr/bin/env bash
#
# diff_and_reprocess_p4.sh — Find events missing from feature_usage and reprocess them
#
# Approach: Direct ID diff (avoids the broken ANTI JOIN in FindUnprocessedEventsFromFeatureUsage)
#   1. Query distinct event IDs from `events` table (per customer + date range)
#   2. Query distinct event IDs from `feature_usage` table (same scope)
#   3. Diff locally (comm -23) to find IDs in events but not in feature_usage
#   4. POST missing IDs to /v1/events/raw/reprocess/all (which re-publishes through
#      the full pipeline: raw_events -> transform -> events topic -> feature_usage)
#
# Why /raw/reprocess/all and not /v1/events/reprocess?
#   The latter uses FindUnprocessedEventsFromFeatureUsage which has a broken ANTI JOIN
#   (loads ALL feature_usage without DISTINCT on ReplacingMergeTree — false 0 results).
#   /raw/reprocess/all accepts event_ids directly and re-publishes to the events Kafka
#   topic, which feeds both the events consumer (P3) and feature_usage tracking (P4).
#   Events already in the events table get deduped by ReplacingMergeTree.
#
# Prerequisites: clickhouse (client), curl, jq
#
# Usage:
#   cd scripts/bash/data_sync && source .env.backfill
#   EXTERNAL_CUSTOMER_ID=<uuid> START_DATE=2026-02-01T00:00:00Z END_DATE=2026-03-01T00:00:00Z \
#     ./diff_and_reprocess_p4.sh
#
set -euo pipefail

###############################################################################
# Parameters
###############################################################################
TENANT_ID="${TENANT_ID:?TENANT_ID is required}"
ENVIRONMENT_ID="${ENVIRONMENT_ID:?ENVIRONMENT_ID is required}"
START_DATE="${START_DATE:?START_DATE is required (e.g. 2026-02-01T00:00:00Z)}"
END_DATE="${END_DATE:?END_DATE is required (e.g. 2026-03-01T00:00:00Z)}"
EXTERNAL_CUSTOMER_ID="${EXTERNAL_CUSTOMER_ID:?EXTERNAL_CUSTOMER_ID is required}"

API_KEY="${API_KEY:-flexprice-api-key}"
API_URL="${API_URL:-https://us.api.flexprice.io/v1/events/raw/reprocess/all}"
API_CHUNK_SIZE="${API_CHUNK_SIZE:-5000}"
API_PARALLEL="${API_PARALLEL:-1}"        # Keep low — each triggers Temporal workflows
DRY_RUN="${DRY_RUN:-false}"
SLEEP_BETWEEN_CHUNKS="${SLEEP_BETWEEN_CHUNKS:-5}"

# ClickHouse
CH_HOST="${CH_HOST:-127.0.0.1}"
CH_PORT="${CH_PORT:-9000}"
CH_USER="${CH_USER:-default}"
CH_PASSWORD="${CH_PASSWORD:-}"
CH_DB="${CH_DB:-flexprice}"
CH_MAX_MEMORY="${CH_MAX_MEMORY:-4000000000}"  # 4 GB per query (reduced from 8)

# Memory safety — pod memory threshold (MiB). Script pauses if pod exceeds this.
# Default 60000 MiB = ~60 GiB = ~50% of a 128 GiB node
CH_POD_MEMORY_THRESHOLD="${CH_POD_MEMORY_THRESHOLD:-60000}"
CH_POD_NAMESPACE="${CH_POD_NAMESPACE:-clickhousev2-mafga}"
CH_POD_NAME_GREP="${CH_POD_NAME_GREP:-clickhousev2-mafga }"
MEMORY_WAIT_INTERVAL="${MEMORY_WAIT_INTERVAL:-30}"
MEMORY_WAIT_MAX_ATTEMPTS="${MEMORY_WAIT_MAX_ATTEMPTS:-60}"  # 60 × 30s = 30 min max wait

# Output
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
OUTPUT_DIR="${OUTPUT_DIR:-${SCRIPT_DIR}/reprocess_output/p4_diff}"
mkdir -p "$OUTPUT_DIR"

###############################################################################
# Helpers
###############################################################################
log() { printf '[%s] %s\n' "$(date '+%Y-%m-%d %H:%M:%S')" "$*"; }

# Get CH pod memory in MiB. Returns 0 if kubectl unavailable.
get_pod_memory_mib() {
  if ! command -v kubectl &>/dev/null; then echo "0"; return; fi
  kubectl top pod -n "$CH_POD_NAMESPACE" --containers 2>/dev/null \
    | grep "$CH_POD_NAME_GREP" \
    | awk '{gsub(/Mi/,"",$4); print $4}' 2>/dev/null || echo "0"
}

# Wait until pod memory drops below threshold. Returns 1 if timed out.
wait_for_memory() {
  local threshold="$1"
  local context="${2:-}"
  for attempt in $(seq 1 "$MEMORY_WAIT_MAX_ATTEMPTS"); do
    local mem
    mem=$(get_pod_memory_mib)
    if (( ${mem:-0} < threshold )); then
      return 0
    fi
    if (( attempt == 1 )); then
      log "    [MEMORY GATE] Pod at ${mem}Mi (threshold: ${threshold}Mi). Pausing... ${context}"
    elif (( attempt % 4 == 0 )); then
      log "    [MEMORY GATE] Still waiting... Pod at ${mem}Mi (attempt ${attempt}/${MEMORY_WAIT_MAX_ATTEMPTS})"
    fi
    sleep "$MEMORY_WAIT_INTERVAL"
  done
  log "    [MEMORY GATE] Timed out waiting for memory to drop. Continuing anyway."
  return 1
}

ch_query() {
  local query="$1"
  clickhouse client \
    --host "$CH_HOST" --port "$CH_PORT" \
    --user "$CH_USER" --password "$CH_PASSWORD" \
    --database "$CH_DB" \
    --format TSV \
    --max_execution_time 600 \
    --query "
      SET max_memory_usage = ${CH_MAX_MEMORY};
      SET max_bytes_before_external_sort = $((CH_MAX_MEMORY / 2));
      SET max_bytes_before_external_group_by = $((CH_MAX_MEMORY / 2));
      SET max_execution_time = 600;
      ${query}
    "
}

# Convert ISO-8601 to ClickHouse DateTime string
to_ch_ts() { printf '%s' "$1" | sed 's/T/ /;s/Z$//'; }

ch_start=$(to_ch_ts "$START_DATE")
ch_end=$(to_ch_ts "$END_DATE")

for cmd in clickhouse curl jq; do
  if ! command -v "$cmd" &>/dev/null; then
    log "ERROR: '$cmd' is required but not installed."
    exit 1
  fi
done

###############################################################################
# Banner
###############################################################################
log "================================================================"
log " P4 Direct Diff & Reprocess"
log " (events vs feature_usage — local diff, bypasses broken ANTI JOIN)"
log "================================================================"
log " Customer:     ${EXTERNAL_CUSTOMER_ID}"
log " Time range:   ${START_DATE} -> ${END_DATE}"
log " API URL:      ${API_URL}"
log " API chunk:    ${API_CHUNK_SIZE} (x${API_PARALLEL} parallel)"
log " Dry run:      ${DRY_RUN}"
log " ClickHouse:   ${CH_HOST}:${CH_PORT}/${CH_DB}"
log " CH max mem:   ${CH_MAX_MEMORY} bytes per query"
log " Pod mem cap:  ${CH_POD_MEMORY_THRESHOLD}Mi (~50% of node)"
log " Output:       ${OUTPUT_DIR}"
log "================================================================"

###############################################################################
# Date windowing helpers — split big date ranges into weekly chunks to avoid OOM
###############################################################################
# Portable date arithmetic (works on macOS + Linux)
epoch_of() {
  if date -j -f '%Y-%m-%d' "$1" '+%s' 2>/dev/null; then return; fi
  date -d "$1" '+%s' 2>/dev/null
}
date_from_epoch() {
  if date -j -f '%s' "$1" '+%Y-%m-%d' 2>/dev/null; then return; fi
  date -d "@$1" '+%Y-%m-%d' 2>/dev/null
}

# Window size in days for chunked queries (7 = weekly)
WINDOW_DAYS="${WINDOW_DAYS:-7}"

###############################################################################
# Steps 1-3: Get IDs from both tables and diff — chunked by weekly windows
###############################################################################
EVENTS_IDS_FILE="${OUTPUT_DIR}/events_ids_${EXTERNAL_CUSTOMER_ID}.txt"
FU_IDS_FILE="${OUTPUT_DIR}/fu_ids_${EXTERNAL_CUSTOMER_ID}.txt"
MISSING_IDS_FILE="${OUTPUT_DIR}/missing_ids_${EXTERNAL_CUSTOMER_ID}.txt"

# Clear output files
> "$EVENTS_IDS_FILE"
> "$FU_IDS_FILE"

# Parse start/end as dates (strip time part)
range_start="${ch_start%% *}"
range_end="${ch_end%% *}"

start_epoch=$(epoch_of "$range_start")
end_epoch_ts=$(epoch_of "$range_end")

log ""
log "Steps 1-2: Querying IDs in ${WINDOW_DAYS}-day windows..."

window_num=0
window_start_epoch="$start_epoch"

while (( window_start_epoch < end_epoch_ts )); do
  window_num=$((window_num + 1))
  window_end_epoch=$((window_start_epoch + WINDOW_DAYS * 86400))
  if (( window_end_epoch > end_epoch_ts )); then
    window_end_epoch="$end_epoch_ts"
  fi

  w_start=$(date_from_epoch "$window_start_epoch")
  w_end=$(date_from_epoch "$window_end_epoch")

  log "  Window ${window_num}: ${w_start} -> ${w_end}"

  # Query events IDs for this window
  q_start=$(date +%s)
  ch_query "
SELECT id
FROM ${CH_DB}.events
PREWHERE tenant_id = '${TENANT_ID}'
  AND environment_id = '${ENVIRONMENT_ID}'
  AND timestamp >= toDateTime64('${w_start} 00:00:00', 3)
  AND timestamp <  toDateTime64('${w_end} 00:00:00', 3)
WHERE external_customer_id = '${EXTERNAL_CUSTOMER_ID}'
GROUP BY id
" >> "$EVENTS_IDS_FILE" 2>/dev/null || log "    WARN: events query failed for window ${w_start}"
  q_mid=$(date +%s)

  # Query feature_usage IDs for this window
  ch_query "
SELECT id
FROM ${CH_DB}.feature_usage
PREWHERE tenant_id = '${TENANT_ID}'
  AND environment_id = '${ENVIRONMENT_ID}'
WHERE external_customer_id = '${EXTERNAL_CUSTOMER_ID}'
  AND timestamp >= toDateTime64('${w_start} 00:00:00', 3)
  AND timestamp <  toDateTime64('${w_end} 00:00:00', 3)
GROUP BY id
" >> "$FU_IDS_FILE" 2>/dev/null || log "    WARN: feature_usage query failed for window ${w_start}"
  q_end=$(date +%s)

  log "    events: $((q_mid - q_start))s | feature_usage: $((q_end - q_mid))s"

  window_start_epoch="$window_end_epoch"
done

# Sort and deduplicate (windows may overlap at boundaries with ReplacingMergeTree)
sort -u "$EVENTS_IDS_FILE" -o "$EVENTS_IDS_FILE"
sort -u "$FU_IDS_FILE" -o "$FU_IDS_FILE"

events_count=$(wc -l < "$EVENTS_IDS_FILE" | tr -d '[:space:]')
fu_count=$(wc -l < "$FU_IDS_FILE" | tr -d '[:space:]')
log ""
log "  Total: ${events_count} distinct events, ${fu_count} distinct feature_usage"

###############################################################################
# Step 3: Local diff — find IDs in events but not in feature_usage
###############################################################################
log ""
log "Step 3: Computing diff (events - feature_usage)..."

comm -23 "$EVENTS_IDS_FILE" "$FU_IDS_FILE" > "$MISSING_IDS_FILE"

missing_count=$(wc -l < "$MISSING_IDS_FILE" | tr -d '[:space:]')
log "  -> ${missing_count} missing event IDs (in events but not in feature_usage)"
if (( events_count > 0 )); then
  log "  -> Gap: ${missing_count} / ${events_count} = $(echo "scale=1; ${missing_count} * 100 / ${events_count}" | bc 2>/dev/null || echo "?")%"
fi

if [[ "$missing_count" == "0" ]]; then
  log "No missing events. Nothing to do."
  exit 0
fi

# Save a summary
log ""
log "Missing IDs saved to: ${MISSING_IDS_FILE}"
log "First 5 missing IDs:"
head -5 "$MISSING_IDS_FILE" | while read -r mid; do log "  ${mid}"; done

###############################################################################
# Step 4: Send missing IDs to reprocess API
###############################################################################
if [[ "$DRY_RUN" == "true" ]]; then
  total_chunks=$(( (missing_count + API_CHUNK_SIZE - 1) / API_CHUNK_SIZE ))
  log ""
  log "[DRY RUN] Would send ${missing_count} IDs in ${total_chunks} chunks of ${API_CHUNK_SIZE}"
  log "[DRY RUN] API URL: ${API_URL}"
  log "[DRY RUN] No API calls made."
  log ""
  log "================================================================"
  log " DRY RUN COMPLETE"
  log "   Events:          ${events_count}"
  log "   Feature usage:   ${fu_count}"
  log "   Missing:         ${missing_count}"
  log "   Would send:      ${total_chunks} API chunks"
  log "================================================================"
  exit 0
fi

log ""
log "Step 4: Sending ${missing_count} missing IDs to reprocess API..."
log "  Memory threshold: ${CH_POD_MEMORY_THRESHOLD}Mi (checking every 10 chunks)"

# Wait for memory to be below threshold before starting
wait_for_memory "$CH_POD_MEMORY_THRESHOLD" "pre-send gate"

# Read all missing IDs into array
all_ids=()
while IFS= read -r _id; do
  [[ -n "$_id" ]] && all_ids+=("$_id")
done < "$MISSING_IDS_FILE"

total_ids=${#all_ids[@]}
total_chunks=$(( (total_ids + API_CHUNK_SIZE - 1) / API_CHUNK_SIZE ))
chunk_num=0
total_ok=0
total_fail=0
RESULT_DIR=$(mktemp -d)
trap 'rm -rf "$RESULT_DIR"' EXIT

start_epoch=$(date +%s)

for (( i=0; i < total_ids; i += API_CHUNK_SIZE )); do
  chunk_num=$((chunk_num + 1))
  chunk_arr=("${all_ids[@]:i:API_CHUNK_SIZE}")
  chunk_count=${#chunk_arr[@]}

  # Build JSON payload
  chunk_json=$(printf '%s\n' "${chunk_arr[@]}" \
    | jq -R -s 'split("\n") | map(select(length > 0))')

  payload=$(jq -n \
    --arg start_date  "$START_DATE" \
    --arg end_date    "$END_DATE" \
    --argjson batch_size "$API_CHUNK_SIZE" \
    --argjson event_ids  "$chunk_json" \
    --arg cust_id "$EXTERNAL_CUSTOMER_ID" \
    '{
      start_date:            $start_date,
      end_date:              $end_date,
      batch_size:            $batch_size,
      event_ids:             $event_ids,
      external_customer_ids: [$cust_id]
    }')

  log "  Chunk ${chunk_num}/${total_chunks} (${chunk_count} IDs)..."

  api_tmp=$(mktemp)
  http_code=$(curl -sS -o "$api_tmp" -w '%{http_code}' \
    --request POST \
    --url "$API_URL" \
    --header 'Content-Type: application/json' \
    --header "x-api-key: ${API_KEY}" \
    --max-time 120 \
    --data "$payload" 2>/dev/null) || http_code="000"

  api_body=$(cat "$api_tmp" 2>/dev/null || true)
  rm -f "$api_tmp"

  if [[ "$http_code" -ge 200 && "$http_code" -lt 300 ]]; then
    wf_id=$(printf '%s' "$api_body" | jq -r '.workflow_id // .data.workflow_id // empty' 2>/dev/null || true)
    log "    -> OK (${http_code}) workflow=${wf_id:-n/a}"
    total_ok=$((total_ok + 1))
  else
    log "    -> FAIL (${http_code}): $(echo "$api_body" | head -c 200)"
    total_fail=$((total_fail + 1))
  fi

  # Memory-aware gating between chunks
  if (( chunk_num < total_chunks )); then
    # Adaptive sleep: bigger batches need more breathing room
    local_sleep="$SLEEP_BETWEEN_CHUNKS"
    if (( total_chunks > 100 )); then
      local_sleep=10  # Big customer (>500K IDs): 10s between chunks
    fi
    if (( total_chunks > 500 )); then
      local_sleep=15  # Very big customer (>2.5M IDs): 15s between chunks
    fi

    sleep "$local_sleep"

    # Every 10 chunks, check pod memory and wait if above threshold
    if (( chunk_num % 10 == 0 )); then
      wait_for_memory "$CH_POD_MEMORY_THRESHOLD" "chunk ${chunk_num}/${total_chunks}"
    fi

    # Every 50 chunks, log progress
    if (( chunk_num % 50 == 0 )); then
      now_epoch=$(date +%s)
      elapsed=$((now_epoch - start_epoch))
      rate=$(( chunk_num * 60 / (elapsed > 0 ? elapsed : 1) ))
      remaining_chunks=$((total_chunks - chunk_num))
      eta_sec=$(( remaining_chunks * elapsed / chunk_num ))
      log "    Progress: ${chunk_num}/${total_chunks} chunks | ${rate} chunks/min | ETA: $((eta_sec/60))m"
      pod_mem=$(get_pod_memory_mib)
      log "    Pod memory: ${pod_mem}Mi (threshold: ${CH_POD_MEMORY_THRESHOLD}Mi)"
    fi
  fi
done

###############################################################################
# Summary
###############################################################################
end_epoch=$(date +%s)
total_elapsed=$((end_epoch - start_epoch))

log ""
log "================================================================"
log " P4 DIFF & REPROCESS COMPLETE"
log "================================================================"
log "   Customer:        ${EXTERNAL_CUSTOMER_ID}"
log "   Events:          ${events_count}"
log "   Feature usage:   ${fu_count}"
log "   Missing:         ${missing_count}"
log "   API chunks OK:   ${total_ok}"
log "   API chunks FAIL: ${total_fail}"
log "   Time:            $((total_elapsed / 60))m $((total_elapsed % 60))s"
log "   Missing IDs:     ${MISSING_IDS_FILE}"
log "================================================================"
