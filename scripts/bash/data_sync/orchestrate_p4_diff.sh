#!/usr/bin/env bash
#
# orchestrate_p4_diff.sh — Run diff_and_reprocess_p4.sh across all enterprise customers
#
# Processes customers smallest-gap-first for quick wins.
# Monitors CH/Temporal health between customers.
# Resume-safe via progress file.
#
# Usage:
#   cd scripts/bash/data_sync && source .env.backfill
#   START_DATE=2026-02-01T00:00:00Z END_DATE=2026-03-26T00:00:00Z ./orchestrate_p4_diff.sh
#
#   # Dry run (diffs only, no API calls)
#   DRY_RUN=true START_DATE=2026-02-01T00:00:00Z END_DATE=2026-03-26T00:00:00Z ./orchestrate_p4_diff.sh
#
set -euo pipefail

###############################################################################
# Configuration
###############################################################################
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CUSTOMER_FILE="${CUSTOMER_FILE:-${SCRIPT_DIR}/../enterprise-customer-ids.json}"
OUTPUT_DIR="${OUTPUT_DIR:-${SCRIPT_DIR}/reprocess_output}"
PROGRESS_FILE="${OUTPUT_DIR}/p4_diff_progress.log"
LOG_FILE="${OUTPUT_DIR}/p4_diff_orchestrate.log"

# Sizing data for tiering
FEB_SIZING_DIR="${FEB_SIZING_DIR:-${SCRIPT_DIR}/sizing_output}"
MAR_SIZING_DIR="${MAR_SIZING_DIR:-${SCRIPT_DIR}/sizing_output_march}"

# Date range
START_DATE="${START_DATE:?START_DATE required (e.g. 2026-02-01T00:00:00Z)}"
END_DATE="${END_DATE:?END_DATE required (e.g. 2026-03-26T00:00:00Z)}"

# API settings passed to diff_and_reprocess_p4.sh
export API_CHUNK_SIZE="${API_CHUNK_SIZE:-5000}"
export API_PARALLEL="${API_PARALLEL:-1}"
export DRY_RUN="${DRY_RUN:-false}"
export SLEEP_BETWEEN_CHUNKS="${SLEEP_BETWEEN_CHUNKS:-5}"

# Orchestrator settings
COOLDOWN_BETWEEN_CUSTOMERS="${COOLDOWN_BETWEEN_CUSTOMERS:-15}"
MAX_TEMPORAL_FAILURES="${MAX_TEMPORAL_FAILURES:-5}"

# CH connection (for monitoring)
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
trap 'log "SIGINT — will exit after current customer."; SHUTDOWN=true' INT TERM

# Progress tracking
is_completed() {
  local cust="$1"
  [[ -f "$PROGRESS_FILE" ]] && grep -qF "${cust}|DONE" "$PROGRESS_FILE" 2>/dev/null
}

record_progress() {
  local cust="$1" events="$2" fu="$3" missing="$4" api_status="$5"
  printf '%s|DONE|events=%s|fu=%s|missing=%s|api=%s|%s\n' \
    "$cust" "$events" "$fu" "$missing" "$api_status" "$(date -u '+%Y-%m-%dT%H:%M:%SZ')" >> "$PROGRESS_FILE"
}

# Get P4 gap from sizing data
get_p4_gap() {
  local cust_id="$1"
  local gap=0
  for dir in "$FEB_SIZING_DIR" "$MAR_SIZING_DIR"; do
    local f="${dir}/per_customer/${cust_id}.csv"
    if [[ -f "$f" ]]; then
      local g
      g=$(awk -F',' 'NR>1 { gap += ($5 - $6) } END { print int(gap) }' "$f")
      gap=$((gap + g))
    fi
  done
  echo "$gap"
}

# CH health check
ch_health() {
  if [[ -z "$CH_HOST" ]]; then return; fi
  local active
  active=$(clickhouse client \
    --host "$CH_HOST" --port "$CH_PORT" \
    --user "$CH_USER" --password "$CH_PASSWORD" \
    --database "$CH_DB" --format TSV \
    --query "SELECT count() FROM system.processes WHERE query NOT LIKE '%system.processes%'" 2>/dev/null || echo "?")
  local mem
  mem=$(clickhouse client \
    --host "$CH_HOST" --port "$CH_PORT" \
    --user "$CH_USER" --password "$CH_PASSWORD" \
    --database "$CH_DB" --format TSV \
    --query "SELECT formatReadableSize(sum(memory_usage)) FROM system.processes WHERE query NOT LIKE '%system.processes%'" 2>/dev/null || echo "?")
  log "  CH health: ${active} queries, ${mem} memory"
}

# K8s pod check
k8s_health() {
  if ! command -v kubectl &>/dev/null; then return; fi
  local pod_info
  pod_info=$(kubectl top pod -n clickhousev2-mafga --containers 2>/dev/null | grep "clickhousev2-mafga " | awk '{print "CPU="$3, "MEM="$4}' || echo "N/A")
  log "  K8s pod: ${pod_info}"
}

# Temporal failure check
TEMPORAL_BASELINE_FAILURES=0

init_temporal() {
  if ! command -v temporal &>/dev/null || [[ -z "${FLEXPRICE_TEMPORAL_ADDRESS:-}" ]]; then return; fi
  local count
  count=$(TEMPORAL_ADDRESS="$FLEXPRICE_TEMPORAL_ADDRESS" TEMPORAL_NAMESPACE="$FLEXPRICE_TEMPORAL_NAMESPACE" TEMPORAL_API_KEY="$FLEXPRICE_TEMPORAL_API_KEY" TEMPORAL_TLS=true \
    temporal workflow count --query "WorkflowType='ReprocessRawEventsWorkflow' AND ExecutionStatus='Failed'" 2>/dev/null | sed -n 's/Total: //p' || echo "0")
  TEMPORAL_BASELINE_FAILURES="${count:-0}"
  log "Temporal baseline failures: ${TEMPORAL_BASELINE_FAILURES}"
}

check_temporal_failures() {
  if ! command -v temporal &>/dev/null || [[ -z "${FLEXPRICE_TEMPORAL_ADDRESS:-}" ]]; then return 0; fi
  local count
  count=$(TEMPORAL_ADDRESS="$FLEXPRICE_TEMPORAL_ADDRESS" TEMPORAL_NAMESPACE="$FLEXPRICE_TEMPORAL_NAMESPACE" TEMPORAL_API_KEY="$FLEXPRICE_TEMPORAL_API_KEY" TEMPORAL_TLS=true \
    temporal workflow count --query "WorkflowType='ReprocessRawEventsWorkflow' AND ExecutionStatus='Failed'" 2>/dev/null | sed -n 's/Total: //p' || echo "0")
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

###############################################################################
# Validation
###############################################################################
for cmd in python3 clickhouse curl jq; do
  if ! command -v "$cmd" &>/dev/null; then
    log "ERROR: '$cmd' is required"
    exit 1
  fi
done

if [[ ! -f "$CUSTOMER_FILE" ]]; then
  log "ERROR: Customer file not found: $CUSTOMER_FILE"
  exit 1
fi

if [[ ! -f "${SCRIPT_DIR}/diff_and_reprocess_p4.sh" ]]; then
  log "ERROR: diff_and_reprocess_p4.sh not found in ${SCRIPT_DIR}"
  exit 1
fi

###############################################################################
# Setup
###############################################################################
mkdir -p "$OUTPUT_DIR"
touch "$LOG_FILE"

# Load and sort customers by P4 gap (smallest first)
CUSTOMERS=()
while IFS= read -r cid; do
  CUSTOMERS+=("$cid")
done < <(python3 -c "import json; [print(x) for x in json.load(open('$CUSTOMER_FILE'))]")

total_customers=${#CUSTOMERS[@]}

SORTED_ENTRIES=()
for cust_id in "${CUSTOMERS[@]}"; do
  gap=$(get_p4_gap "$cust_id")
  SORTED_ENTRIES+=("${gap}|${cust_id}")
done
IFS=$'\n' SORTED_ENTRIES=($(printf '%s\n' "${SORTED_ENTRIES[@]}" | sort -t'|' -k1 -n)); unset IFS

SORTED_CUSTOMERS=()
for entry in "${SORTED_ENTRIES[@]}"; do
  SORTED_CUSTOMERS+=("${entry#*|}")
done

###############################################################################
# Banner
###############################################################################
log "================================================================"
log " P4 Direct Diff & Reprocess — Full Orchestration"
log "================================================================"
log " Customers:      ${total_customers}"
log " Date range:     ${START_DATE} -> ${END_DATE}"
log " Dry run:        ${DRY_RUN}"
log " API chunk:      ${API_CHUNK_SIZE}"
log " Cool-down:      ${COOLDOWN_BETWEEN_CUSTOMERS}s between customers"
log " Progress file:  ${PROGRESS_FILE}"
log " Log file:       ${LOG_FILE}"
log "================================================================"

init_temporal

###############################################################################
# Main loop
###############################################################################
global_start=$(date +%s)
cust_done=0
cust_skipped=0
cust_zero_gap=0
cust_failed=0
total_missing=0
total_events=0
total_fu=0

for cust_id in "${SORTED_CUSTOMERS[@]}"; do
  if [[ "$SHUTDOWN" == "true" ]]; then
    log "Shutdown requested. Exiting."
    break
  fi

  cust_done=$((cust_done + 1))
  p4_gap=$(get_p4_gap "$cust_id")

  # Skip zero-gap customers
  if (( p4_gap <= 0 )); then
    log "[${cust_done}/${total_customers}] ${cust_id} — P4 gap=0, skipping"
    cust_zero_gap=$((cust_zero_gap + 1))
    continue
  fi

  # Skip already completed
  if is_completed "$cust_id"; then
    log "[${cust_done}/${total_customers}] ${cust_id} — already completed, skipping"
    cust_skipped=$((cust_skipped + 1))
    continue
  fi

  log ""
  log "============================================================"
  log " [${cust_done}/${total_customers}] ${cust_id}"
  log " Sizing P4 gap: ${p4_gap}"
  log "============================================================"

  # Health check before each customer
  ch_health
  k8s_health

  # Run the diff script
  diff_output=$(EXTERNAL_CUSTOMER_ID="$cust_id" \
    START_DATE="$START_DATE" \
    END_DATE="$END_DATE" \
    "${SCRIPT_DIR}/diff_and_reprocess_p4.sh" 2>&1) || true

  echo "$diff_output" >> "$LOG_FILE"

  # Parse results from the summary block at the end of diff output
  events_count=$(echo "$diff_output" | grep 'Events:' | tail -1 | awk '{print $NF}' || echo "0")
  fu_count=$(echo "$diff_output" | grep 'Feature usage:' | tail -1 | awk '{print $NF}' || echo "0")
  missing_count=$(echo "$diff_output" | grep 'Missing:' | tail -1 | awk '{print $NF}' || echo "0")
  api_ok=$(echo "$diff_output" | grep 'API chunks OK:' | tail -1 | awk '{print $NF}' || echo "0")
  api_fail=$(echo "$diff_output" | grep 'API chunks FAIL:' | tail -1 | awk '{print $NF}' || echo "0")
  # For dry run, "Would send" instead of API chunks
  if [[ "$DRY_RUN" == "true" ]]; then
    api_ok="dry_run"
    api_fail="0"
  fi
  # Default to 0 if empty
  events_count="${events_count:-0}"
  fu_count="${fu_count:-0}"
  missing_count="${missing_count:-0}"
  api_ok="${api_ok:-0}"
  api_fail="${api_fail:-0}"

  log "  Result: events=${events_count} fu=${fu_count} missing=${missing_count} api_ok=${api_ok} api_fail=${api_fail}"

  total_events=$((total_events + ${events_count:-0}))
  total_fu=$((total_fu + ${fu_count:-0}))
  total_missing=$((total_missing + ${missing_count:-0}))

  if [[ "${api_fail:-0}" == "0" || "$DRY_RUN" == "true" ]]; then
    record_progress "$cust_id" "${events_count:-0}" "${fu_count:-0}" "${missing_count:-0}" "ok"
  else
    cust_failed=$((cust_failed + 1))
    record_progress "$cust_id" "${events_count:-0}" "${fu_count:-0}" "${missing_count:-0}" "fail"
  fi

  # ETA
  now_epoch=$(date +%s)
  elapsed=$((now_epoch - global_start))
  processed=$((cust_done - cust_zero_gap - cust_skipped))
  if (( elapsed > 0 && processed > 0 )); then
    remaining=$((total_customers - cust_done))
    eta_sec=$((remaining * elapsed / processed))
    log "  Progress: ${cust_done}/${total_customers} | Elapsed: $((elapsed/60))m | ETA: ~$((eta_sec / 60))m"
  fi

  # Temporal health
  if ! check_temporal_failures; then
    log "Aborting due to excessive Temporal failures."
    SHUTDOWN=true
    break
  fi

  # Adaptive cool-down: bigger customers need more breathing room
  if (( cust_done < total_customers )) && [[ "$SHUTDOWN" != "true" && "$DRY_RUN" != "true" ]]; then
    cooldown="$COOLDOWN_BETWEEN_CUSTOMERS"
    if (( ${missing_count:-0} > 100000 )); then
      cooldown=120  # 2 min for 100K+ gap customers
    elif (( ${missing_count:-0} > 50000 )); then
      cooldown=90   # 1.5 min for 50K+ gap customers
    elif (( ${missing_count:-0} > 10000 )); then
      cooldown=60   # 1 min for 10K+ gap customers
    fi
    log "  Cool-down: ${cooldown}s (missing=${missing_count:-0})"
    sleep "$cooldown"

    # After cool-down, wait for pod memory to drop below 50% (60 GiB)
    if command -v kubectl &>/dev/null; then
      local pod_mem_threshold=60000  # 60 GiB in MiB
      for attempt in $(seq 1 60); do
        pod_mem=$(kubectl top pod -n clickhousev2-mafga --containers 2>/dev/null \
          | grep "clickhousev2-mafga " | awk '{gsub(/Mi/,"",$4); print $4}' || echo "0")
        if (( ${pod_mem:-0} < pod_mem_threshold )); then
          break
        fi
        if (( attempt == 1 )); then
          log "  [MEMORY GATE] Pod at ${pod_mem}Mi (threshold: ${pod_mem_threshold}Mi). Waiting..."
        elif (( attempt % 4 == 0 )); then
          log "  [MEMORY GATE] Still waiting... Pod at ${pod_mem}Mi (attempt ${attempt}/60)"
        fi
        sleep 30
      done
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
log " P4 DIFF & REPROCESS — ORCHESTRATION COMPLETE"
log "================================================================"
log "  Customers processed: ${cust_done} / ${total_customers}"
log "  Customers skipped:   ${cust_skipped} (already completed)"
log "  Customers zero gap:  ${cust_zero_gap}"
log "  Customers failed:    ${cust_failed}"
log "  Total events:        ${total_events}"
log "  Total feature_usage: ${total_fu}"
log "  Total missing:       ${total_missing}"
log "  Total time:          $((total_elapsed / 3600))h $(( (total_elapsed % 3600) / 60 ))m $((total_elapsed % 60))s"
log "  Progress file:       ${PROGRESS_FILE}"
log "  Log file:            ${LOG_FILE}"
if [[ "$SHUTDOWN" == "true" ]]; then
  log "  NOTE: Stopped early. Re-run to resume."
fi
log "================================================================"
