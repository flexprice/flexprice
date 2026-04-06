#!/usr/bin/env bash
#
# reprocess_feature_usage.sh — Pipeline 4 Reprocessing (Controlled)
#
# Orchestrates POST /v1/events/reprocess across enterprise customers.
# Each API call triggers a Temporal ReprocessEventsWorkflow that:
#   1. FindUnprocessedEventsFromFeatureUsage (events ANTI JOIN feature_usage)
#   2. Publishes missing events to feature_usage_backfill Kafka topic
#   3. Feature usage consumer processes them
#
# ⚠️  CRITICAL: The ANTI JOIN in step 1 is VERY expensive:
#   - Inner subquery scans ALL feature_usage rows for the tenant (no time filter)
#   - Each batch of events triggers another ANTI JOIN
#   - Multiple parallel workflows = catastrophic CH CPU/memory
#
# Therefore this script enforces:
#   - STRICTLY ONE workflow at a time (never parallel)
#   - Tight date windows (1-3 days max for big customers)
#   - Wait for workflow completion before next request
#   - Monitor Temporal + CH health between requests
#   - Conservative batch sizes (100-200)
#
# Usage:
#   cd scripts/bash/data_sync && source .env.backfill && ./reprocess_feature_usage.sh
#
set -euo pipefail

###############################################################################
# Configuration
###############################################################################
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CUSTOMER_FILE="${CUSTOMER_FILE:-${SCRIPT_DIR}/../enterprise-customer-ids.json}"
OUTPUT_DIR="${OUTPUT_DIR:-${SCRIPT_DIR}/reprocess_output}"
PROGRESS_FILE="${OUTPUT_DIR}/progress.log"
LOG_FILE="${OUTPUT_DIR}/reprocess.log"

# Sizing data directories (for tiering)
FEB_SIZING_DIR="${FEB_SIZING_DIR:-${SCRIPT_DIR}/sizing_output}"
MAR_SIZING_DIR="${MAR_SIZING_DIR:-${SCRIPT_DIR}/sizing_output_march}"

# Date range
START_DATE="${START_DATE:-2026-02-01}"
END_DATE="${END_DATE:-2026-03-01}"

# FlexPrice API
API_URL="${API_URL:-https://us.api.flexprice.io/v1/events/reprocess}"
API_KEY="${API_KEY:?API_KEY required}"

# Batch size INSIDE the Temporal workflow (events per ANTI JOIN batch)
# Keep LOW to reduce per-query load on CH
WORKFLOW_BATCH_SIZE="${WORKFLOW_BATCH_SIZE:-200}"

# DRY_RUN mode
DRY_RUN="${DRY_RUN:-false}"

# STRICTLY SEQUENTIAL: wait this long after each API call for workflow to finish
# before checking completion. The ANTI JOIN is expensive — give it breathing room.
POLL_INTERVAL="${POLL_INTERVAL:-30}"

# Max time to wait for a single workflow (seconds) before moving on
MAX_WORKFLOW_WAIT="${MAX_WORKFLOW_WAIT:-3600}"

# Sleep AFTER a workflow completes before starting next (cool-down)
COOLDOWN_BETWEEN_WINDOWS="${COOLDOWN_BETWEEN_WINDOWS:-30}"
COOLDOWN_BETWEEN_CUSTOMERS="${COOLDOWN_BETWEEN_CUSTOMERS:-60}"

# Abort thresholds
MAX_TEMPORAL_FAILURES="${MAX_TEMPORAL_FAILURES:-5}"

# ClickHouse connection (for monitoring)
CH_HOST="${CH_HOST:-}"
CH_PORT="${CH_PORT:-9000}"
CH_USER="${CH_USER:-default}"
CH_PASSWORD="${CH_PASSWORD:-}"
CH_DB="${CH_DB:-flexprice}"

###############################################################################
# Helpers
###############################################################################
log() {
  local msg
  msg=$(printf '[%s] %s' "$(date '+%Y-%m-%d %H:%M:%S')" "$*")
  echo "$msg"
  echo "$msg" >> "$LOG_FILE"
}

SHUTDOWN=false
trap 'log "SIGINT — finishing current operation then exiting."; SHUTDOWN=true' INT TERM

###############################################################################
# Progress tracking
###############################################################################
is_completed() {
  local cust="$1" ws="$2" we="$3"
  [[ -f "$PROGRESS_FILE" ]] && grep -qF "${cust}|${ws}|${we}|OK" "$PROGRESS_FILE" 2>/dev/null
}

record_progress() {
  local cust="$1" ws="$2" we="$3" status="$4" info="$5"
  printf '%s|%s|%s|%s|%s|%s\n' "$cust" "$ws" "$we" "$status" "$(date -u '+%Y-%m-%dT%H:%M:%SZ')" "$info" >> "$PROGRESS_FILE"
}

###############################################################################
# ClickHouse monitoring (optional — if CH_HOST is set)
###############################################################################
ch_active_queries() {
  if [[ -z "$CH_HOST" ]]; then
    echo "0"
    return
  fi
  clickhouse client \
    --host "$CH_HOST" --port "$CH_PORT" \
    --user "$CH_USER" --password "$CH_PASSWORD" \
    --database "$CH_DB" \
    --format TSV \
    --query "SELECT count() FROM system.processes WHERE query LIKE '%feature_usage%' AND query NOT LIKE '%system.processes%'" \
    2>/dev/null || echo "0"
}

ch_memory_usage() {
  if [[ -z "$CH_HOST" ]]; then
    echo "0"
    return
  fi
  clickhouse client \
    --host "$CH_HOST" --port "$CH_PORT" \
    --user "$CH_USER" --password "$CH_PASSWORD" \
    --database "$CH_DB" \
    --format TSV \
    --query "SELECT formatReadableSize(sum(memory_usage)) FROM system.processes WHERE query LIKE '%feature_usage%'" \
    2>/dev/null || echo "0"
}

wait_for_ch_calm() {
  # Wait until no heavy feature_usage queries are running
  if [[ -z "$CH_HOST" ]]; then
    return
  fi
  local max_wait=120
  local waited=0
  while (( waited < max_wait )); do
    local active
    active=$(ch_active_queries)
    if (( active <= 1 )); then
      return
    fi
    log "    CH has ${active} active feature_usage queries — waiting 10s..."
    sleep 10
    waited=$((waited + 10))
  done
  log "    WARNING: CH still busy after ${max_wait}s — proceeding anyway"
}

###############################################################################
# Temporal workflow monitoring
###############################################################################
TEMPORAL_BASELINE_FAILURES=0

temporal_cmd() {
  if ! command -v temporal &>/dev/null || [[ -z "${FLEXPRICE_TEMPORAL_ADDRESS:-}" ]]; then
    return 1
  fi
  TEMPORAL_ADDRESS="$FLEXPRICE_TEMPORAL_ADDRESS" \
  TEMPORAL_NAMESPACE="$FLEXPRICE_TEMPORAL_NAMESPACE" \
  TEMPORAL_API_KEY="$FLEXPRICE_TEMPORAL_API_KEY" \
  TEMPORAL_TLS=true \
  temporal "$@" 2>/dev/null
}

init_temporal_monitoring() {
  local count
  count=$(temporal_cmd workflow count \
    --query "WorkflowType='ReprocessEventsWorkflow' AND ExecutionStatus='Failed'" \
    2>/dev/null | sed -n 's/Total: //p' || echo "0")
  TEMPORAL_BASELINE_FAILURES="${count:-0}"
  log "Temporal baseline failures: ${TEMPORAL_BASELINE_FAILURES}"
}

check_temporal_failures() {
  local count
  count=$(temporal_cmd workflow count \
    --query "WorkflowType='ReprocessEventsWorkflow' AND ExecutionStatus='Failed'" \
    2>/dev/null | sed -n 's/Total: //p' || echo "0")
  local new_failures=$(( ${count:-0} - TEMPORAL_BASELINE_FAILURES ))
  if (( new_failures > MAX_TEMPORAL_FAILURES )); then
    log "ABORT: ${new_failures} new Temporal failures (threshold: ${MAX_TEMPORAL_FAILURES})"
    return 1
  fi
  if (( new_failures > 0 )); then
    log "  Temporal: ${new_failures} new failures (threshold: ${MAX_TEMPORAL_FAILURES})"
  fi
  return 0
}

# Count currently running ReprocessEventsWorkflow workflows
temporal_running_count() {
  local count
  count=$(temporal_cmd workflow count \
    --query "WorkflowType='ReprocessEventsWorkflow' AND ExecutionStatus='Running'" \
    2>/dev/null | sed -n 's/Total: //p' || echo "0")
  echo "${count:-0}"
}

# Wait for all running reprocess workflows to complete
wait_for_workflows_complete() {
  local max_wait="$MAX_WORKFLOW_WAIT"
  local waited=0
  while (( waited < max_wait )); do
    local running
    running=$(temporal_running_count)
    if (( running == 0 )); then
      log "    All ReprocessEventsWorkflow workflows completed."
      return 0
    fi
    log "    ${running} workflows still running — waiting ${POLL_INTERVAL}s... (${waited}/${max_wait}s)"
    sleep "$POLL_INTERVAL"
    waited=$((waited + POLL_INTERVAL))
  done
  log "    WARNING: Workflows still running after ${max_wait}s — proceeding."
  return 0
}

###############################################################################
# Tiering
###############################################################################
get_p4_gap() {
  local cust_id="$1"
  local gap=0
  local feb_file="${FEB_SIZING_DIR}/per_customer/${cust_id}.csv"
  local mar_file="${MAR_SIZING_DIR}/per_customer/${cust_id}.csv"
  if [[ -f "$feb_file" ]]; then
    local g
    g=$(awk -F',' 'NR>1 { gap += ($5 - $6) } END { print int(gap) }' "$feb_file")
    gap=$((gap + g))
  fi
  if [[ -f "$mar_file" ]]; then
    local g
    g=$(awk -F',' 'NR>1 { gap += ($5 - $6) } END { print int(gap) }' "$mar_file")
    gap=$((gap + g))
  fi
  echo "$gap"
}

# Returns: chunk_days
# Tight windows to keep the ANTI JOIN scope small
get_chunk_days() {
  local gap="$1"
  if   (( gap > 2000000 )); then echo "1"   # 1-day chunks for huge customers
  elif (( gap > 500000  )); then echo "2"   # 2-day chunks
  elif (( gap > 100000  )); then echo "3"   # 3-day chunks
  elif (( gap > 10000   )); then echo "7"   # 7-day chunks
  else                           echo "14"  # 14-day chunks
  fi
}

###############################################################################
# Date arithmetic (macOS compatible)
###############################################################################
date_add_days() {
  local base_date="$1" days="$2"
  if command -v gdate &>/dev/null; then
    gdate -d "${base_date} +${days} days" +"%Y-%m-%d"
  elif [[ "$(uname)" == "Darwin" ]]; then
    date -j -v+${days}d -f "%Y-%m-%d" "$base_date" +"%Y-%m-%d"
  else
    date -d "${base_date} +${days} days" +"%Y-%m-%d"
  fi
}

date_lt() {
  # Returns 0 (true) if $1 < $2
  [[ "$1" < "$2" ]]
}

generate_windows() {
  local start="$1" end="$2" chunk_days="$3"
  local current="$start"
  while date_lt "$current" "$end"; do
    local next
    next=$(date_add_days "$current" "$chunk_days")
    # Cap at end date
    if ! date_lt "$next" "$end" && [[ "$next" != "$end" ]]; then
      next="$end"
    fi
    echo "${current}T00:00:00Z|${next}T00:00:00Z"
    current="$next"
  done
}

###############################################################################
# Call reprocess API
###############################################################################
call_reprocess_api() {
  local cust_id="$1" start="$2" end="$3"

  if [[ "$DRY_RUN" == "true" ]]; then
    log "    [DRY RUN] POST ${API_URL} — ${cust_id} ${start} -> ${end} batch=${WORKFLOW_BATCH_SIZE}"
    return 0
  fi

  local api_tmp
  api_tmp=$(mktemp)
  local http_code
  http_code=$(curl -sS -o "$api_tmp" -w '%{http_code}' \
    --request POST \
    --url "$API_URL" \
    --header 'Content-Type: application/json' \
    --header "x-api-key: ${API_KEY}" \
    --max-time 120 \
    --data "{
      \"external_customer_id\": \"${cust_id}\",
      \"start_date\": \"${start}\",
      \"end_date\": \"${end}\",
      \"batch_size\": ${WORKFLOW_BATCH_SIZE}
    }" 2>/dev/null) || http_code="000"

  local body
  body=$(cat "$api_tmp" 2>/dev/null || true)
  rm -f "$api_tmp"

  if [[ "$http_code" -ge 200 && "$http_code" -lt 300 ]]; then
    local wf_id
    wf_id=$(echo "$body" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('workflow_id', d.get('data',{}).get('workflow_id','n/a')))" 2>/dev/null || echo "n/a")
    log "    -> OK (${http_code}) workflow=${wf_id}"
    return 0
  else
    log "    -> FAIL (${http_code}): $(echo "$body" | head -c 200)"
    return 1
  fi
}

###############################################################################
# Validation
###############################################################################
for cmd in curl python3; do
  if ! command -v "$cmd" &>/dev/null; then
    log "ERROR: '$cmd' is required"
    exit 1
  fi
done

if [[ ! -f "$CUSTOMER_FILE" ]]; then
  log "ERROR: Customer file not found: $CUSTOMER_FILE"
  exit 1
fi

###############################################################################
# Setup
###############################################################################
mkdir -p "$OUTPUT_DIR"
touch "$LOG_FILE"

# Load customer IDs
CUSTOMERS=()
while IFS= read -r cid; do
  CUSTOMERS+=("$cid")
done < <(python3 -c "import json; [print(x) for x in json.load(open('$CUSTOMER_FILE'))]")

total_customers=${#CUSTOMERS[@]}

# Sort customers by P4 gap (smallest first — quick wins)
SORTED_CUSTOMERS=()
CUSTOMER_GAP_MAP=()
for cust_id in "${CUSTOMERS[@]}"; do
  gap=$(get_p4_gap "$cust_id")
  CUSTOMER_GAP_MAP+=("${gap}|${cust_id}")
done
IFS=$'\n' SORTED_ENTRIES=($(printf '%s\n' "${CUSTOMER_GAP_MAP[@]}" | sort -t'|' -k1 -n)); unset IFS
for entry in "${SORTED_ENTRIES[@]}"; do
  SORTED_CUSTOMERS+=("${entry#*|}")
done

###############################################################################
# Banner
###############################################################################
log "================================================================"
log " Pipeline 4 Reprocessing — Feature Usage Backfill (CONTROLLED)"
log "================================================================"
log " Customers:        ${total_customers}"
log " Date range:       ${START_DATE} -> ${END_DATE}"
log " API URL:          ${API_URL}"
log " Batch size:       ${WORKFLOW_BATCH_SIZE} (per ANTI JOIN query)"
log " Dry run:          ${DRY_RUN}"
log " Progress file:    ${PROGRESS_FILE}"
log " Log file:         ${LOG_FILE}"
log " SAFETY:"
log "   - Strictly ONE workflow at a time"
log "   - Wait for completion before next"
log "   - Cool-down: ${COOLDOWN_BETWEEN_WINDOWS}s/window, ${COOLDOWN_BETWEEN_CUSTOMERS}s/customer"
log "   - Max workflow wait: ${MAX_WORKFLOW_WAIT}s"
log "   - Abort at ${MAX_TEMPORAL_FAILURES} Temporal failures"
log "================================================================"

init_temporal_monitoring

###############################################################################
# Main loop — STRICTLY SEQUENTIAL, ONE WORKFLOW AT A TIME
###############################################################################
global_start=$(date +%s)
cust_done=0
cust_skipped=0
cust_failed=0
total_windows_ok=0
total_windows_fail=0

for cust_id in "${SORTED_CUSTOMERS[@]}"; do
  if [[ "$SHUTDOWN" == "true" ]]; then
    log "Shutdown requested. Exiting."
    break
  fi

  cust_done=$((cust_done + 1))
  p4_gap=$(get_p4_gap "$cust_id")

  if (( p4_gap <= 0 )); then
    log "[${cust_done}/${total_customers}] ${cust_id} — P4 gap=0, skipping"
    cust_skipped=$((cust_skipped + 1))
    continue
  fi

  chunk_days=$(get_chunk_days "$p4_gap")

  log ""
  log "============================================================"
  log " [${cust_done}/${total_customers}] ${cust_id}"
  log " P4 gap: ${p4_gap} | Chunk: ${chunk_days}-day"
  log "============================================================"

  # Generate time windows
  windows=()
  while IFS= read -r w; do
    [[ -n "$w" ]] && windows+=("$w")
  done < <(generate_windows "$START_DATE" "$END_DATE" "$chunk_days")

  all_ok=true
  win_done=0
  win_skipped=0

  for window in "${windows[@]}"; do
    if [[ "$SHUTDOWN" == "true" ]]; then
      break 2
    fi

    ws="${window%%|*}"
    we="${window##*|}"

    # Resume check
    if is_completed "$cust_id" "$ws" "$we"; then
      win_skipped=$((win_skipped + 1))
      continue
    fi

    log "  Window: ${ws} -> ${we}"

    # STEP 1: Wait for any running workflows to finish first
    if [[ "$DRY_RUN" != "true" ]]; then
      wait_for_workflows_complete
      wait_for_ch_calm
    fi

    # STEP 2: Call the API (triggers ONE Temporal workflow)
    if call_reprocess_api "$cust_id" "$ws" "$we"; then
      record_progress "$cust_id" "$ws" "$we" "OK" "api_success"
      win_done=$((win_done + 1))
      total_windows_ok=$((total_windows_ok + 1))
    else
      record_progress "$cust_id" "$ws" "$we" "FAIL" "api_error"
      all_ok=false
      total_windows_fail=$((total_windows_fail + 1))
    fi

    # STEP 3: Wait for this workflow to complete before starting next
    if [[ "$DRY_RUN" != "true" ]]; then
      log "    Waiting for workflow to complete..."
      wait_for_workflows_complete

      # Cool-down between windows
      log "    Cool-down: ${COOLDOWN_BETWEEN_WINDOWS}s"
      sleep "$COOLDOWN_BETWEEN_WINDOWS"

      # Check Temporal health
      if ! check_temporal_failures; then
        log "Aborting due to excessive Temporal failures."
        SHUTDOWN=true
        break 2
      fi
    fi
  done

  if (( win_skipped > 0 )); then
    log "  Skipped ${win_skipped} already-completed windows"
  fi

  if [[ "$all_ok" == "true" ]]; then
    log "  Customer ${cust_id}: ALL ${win_done} windows submitted OK"
  else
    log "  Customer ${cust_id}: Some windows FAILED"
    cust_failed=$((cust_failed + 1))
  fi

  # ETA
  now_epoch=$(date +%s)
  elapsed=$((now_epoch - global_start))
  if (( elapsed > 0 && cust_done > 0 )); then
    remaining=$((total_customers - cust_done))
    eta_sec=$((remaining * elapsed / cust_done))
    log "  Progress: ${cust_done}/${total_customers} | Elapsed: $((elapsed/60))m | ETA: ~$((eta_sec / 60))m"
  fi

  # Cool-down between customers
  if (( cust_done < total_customers )) && [[ "$SHUTDOWN" != "true" && "$DRY_RUN" != "true" ]]; then
    log "  Customer cool-down: ${COOLDOWN_BETWEEN_CUSTOMERS}s"
    sleep "$COOLDOWN_BETWEEN_CUSTOMERS"

    if ! check_temporal_failures; then
      log "Aborting due to excessive Temporal failures."
      SHUTDOWN=true
    fi
  fi
done

###############################################################################
# Summary
###############################################################################
end_epoch=$(date +%s)
total_elapsed=$((end_epoch - global_start))

log ""
log "================================================================"
log " PIPELINE 4 REPROCESSING COMPLETE"
log "================================================================"
log "  Customers processed: ${cust_done} / ${total_customers}"
log "  Customers skipped:   ${cust_skipped} (no P4 gap)"
log "  Customers failed:    ${cust_failed}"
log "  Windows OK:          ${total_windows_ok}"
log "  Windows failed:      ${total_windows_fail}"
log "  Total time:          $((total_elapsed / 3600))h $(( (total_elapsed % 3600) / 60 ))m $((total_elapsed % 60))s"
log "  Progress file:       ${PROGRESS_FILE}"
log "  Log file:            ${LOG_FILE}"
if [[ "$SHUTDOWN" == "true" ]]; then
  log "  NOTE: Stopped early. Re-run to resume."
fi
log "================================================================"
