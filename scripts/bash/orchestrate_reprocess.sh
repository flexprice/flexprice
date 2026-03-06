#!/usr/bin/env bash
#
# Orchestrates reprocess_missing_feature_usage.sh across many customers
# with tiered time-window chunking, adaptive sleeps, and resume support.
#
# Prerequisites: same as reprocess_missing_feature_usage.sh + the CSV file
#
# Usage:
#   source .env.backfill && ./orchestrate_reprocess.sh
#
set -euo pipefail

###############################################################################
# Configuration
###############################################################################
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPROCESS_SCRIPT="${SCRIPT_DIR}/reprocess_missing_feature_usage.sh"

CSV_FILE="${CSV_FILE:-/Users/nikhilmishra/Downloads/vapi-top-1000-cust-by-events-feb.csv}"
PROGRESS_FILE="${PROGRESS_FILE:-${SCRIPT_DIR}/reprocess_progress.log}"

# February 2026
MONTH_START="2026-02-01T00:00:00Z"
MONTH_END="2026-03-01T00:00:00Z"

# Reduced concurrency for safety — tuned to keep CH memory under 80GB
# KEY INSIGHT: Each API call creates 1 Temporal workflow. Workflows run concurrently
# on ClickHouse. With 200+ batches, we need to throttle enqueue rate so only ~5-10
# workflows are active at any time.
export BATCH_SIZE="${BATCH_SIZE:-10000}"
export API_CHUNK_SIZE="${API_CHUNK_SIZE:-2000}"
export API_PARALLEL="${API_PARALLEL:-2}"
export DRY_RUN="${DRY_RUN:-false}"
export SLEEP_BETWEEN_BATCHES="${SLEEP_BETWEEN_BATCHES:-10}"

# Sleep between time-window chunks within a customer (let Temporal drain)
CHUNK_SLEEP="${CHUNK_SLEEP:-60}"

# Abort thresholds
MAX_TEMPORAL_FAILURES="${MAX_TEMPORAL_FAILURES:-20}"  # abort if >20 NEW failures in this run

# Customers to skip
SKIP_CUSTOMERS="${SKIP_CUSTOMERS:-UNKNOWN ORG}"

###############################################################################
# Helpers
###############################################################################
log() { printf '[%s] %s\n' "$(date '+%Y-%m-%d %H:%M:%S')" "$*"; }

# Graceful shutdown
SHUTDOWN=false
trap 'log "SIGINT received — finishing current customer then exiting."; SHUTDOWN=true' INT
trap 'log "SIGTERM received — finishing current customer then exiting."; SHUTDOWN=true' TERM

# Check if a customer+window is already completed in progress file
is_completed() {
  local cust="$1" ws="$2" we="$3"
  if [[ -f "$PROGRESS_FILE" ]]; then
    grep -qF "${cust}|${ws}|${we}|OK" "$PROGRESS_FILE" 2>/dev/null
  else
    return 1
  fi
}

# Record progress
record_progress() {
  local cust="$1" ws="$2" we="$3" status="$4" info="$5"
  printf '%s|%s|%s|%s|%s|%s\n' "$cust" "$ws" "$we" "$status" "$(date -u '+%Y-%m-%dT%H:%M:%SZ')" "$info" >> "$PROGRESS_FILE"
}

# Determine tier for a customer based on event count
# Returns: chunk_days|sleep_after
get_tier() {
  local events="$1"
  if   (( events > 10000000 )); then echo "2|120"
  elif (( events > 2000000  )); then echo "7|60"
  elif (( events > 500000   )); then echo "14|20"
  else                                echo "28|5"
  fi
}

# Generate ISO-8601 date windows for February 2026
# Args: chunk_days
# Outputs: lines of "start_date|end_date"
generate_windows() {
  local chunk_days="$1"

  # February 2026 has 28 days
  local day=1
  while (( day <= 28 )); do
    local end_day=$((day + chunk_days))
    if (( end_day > 28 )); then
      end_day=29  # March 1
    fi

    local ws we
    ws=$(printf '2026-02-%02dT00:00:00Z' "$day")
    if (( end_day <= 28 )); then
      we=$(printf '2026-02-%02dT00:00:00Z' "$end_day")
    else
      we="2026-03-01T00:00:00Z"
    fi

    echo "${ws}|${we}"
    day=$end_day
  done
}

###############################################################################
# Validation
###############################################################################
if [[ ! -f "$CSV_FILE" ]]; then
  log "ERROR: CSV file not found: $CSV_FILE"
  exit 1
fi

if [[ ! -f "$REPROCESS_SCRIPT" ]]; then
  log "ERROR: Reprocess script not found: $REPROCESS_SCRIPT"
  exit 1
fi

if [[ ! -x "$REPROCESS_SCRIPT" ]]; then
  chmod +x "$REPROCESS_SCRIPT"
fi

for cmd in curl jq; do
  if ! command -v "$cmd" &>/dev/null; then
    log "ERROR: '$cmd' is required but not installed."
    exit 1
  fi
done

###############################################################################
# Load customers from CSV
###############################################################################
CUSTOMERS=()
CUSTOMER_EVENTS=()

while IFS=',' read -r cust_id events; do
  # Skip header
  [[ "$cust_id" == "external_customer_id" ]] && continue
  # Skip empty lines
  [[ -z "$cust_id" ]] && continue
  # Skip configured customers
  if echo "$SKIP_CUSTOMERS" | grep -qF "$cust_id"; then
    log "Skipping customer: $cust_id (in SKIP_CUSTOMERS)"
    continue
  fi

  CUSTOMERS+=("$cust_id")
  CUSTOMER_EVENTS+=("$events")
done < "$CSV_FILE"

total_customers=${#CUSTOMERS[@]}
log "Loaded ${total_customers} customers from CSV (already sorted largest-first)"

###############################################################################
# Banner
###############################################################################
log "================================================================"
log " Orchestrated Reprocess — Missing Feature Usage"
log "================================================================"
log " Customers:      ${total_customers}"
log " Date range:     ${MONTH_START} -> ${MONTH_END}"
log " Batch size:     ${BATCH_SIZE}"
log " API chunk:      ${API_CHUNK_SIZE} (x${API_PARALLEL} parallel)"
log " Dry run:        ${DRY_RUN}"
log " Progress file:  ${PROGRESS_FILE}"
log "================================================================"

###############################################################################
# Temporal failure tracking
###############################################################################
# Record the baseline failure count at start (from prior runs)
TEMPORAL_BASELINE_FAILURES=0
if command -v temporal &>/dev/null && [[ -n "${FLEXPRICE_TEMPORAL_ADDRESS:-}" ]]; then
  TEMPORAL_BASELINE_FAILURES=$(
    TEMPORAL_ADDRESS="$FLEXPRICE_TEMPORAL_ADDRESS" \
    TEMPORAL_NAMESPACE="$FLEXPRICE_TEMPORAL_NAMESPACE" \
    TEMPORAL_API_KEY="$FLEXPRICE_TEMPORAL_API_KEY" \
    TEMPORAL_TLS=true \
    temporal workflow count \
      --query "WorkflowType='ReprocessRawEventsWorkflow' AND ExecutionStatus='Failed'" \
      2>/dev/null | sed -n 's/Total: //p' || echo "0"
  )
  log "Temporal baseline failures: ${TEMPORAL_BASELINE_FAILURES}"
fi

check_temporal_failures() {
  if ! command -v temporal &>/dev/null || [[ -z "${FLEXPRICE_TEMPORAL_ADDRESS:-}" ]]; then
    return 0  # skip check if no temporal CLI
  fi
  local current_failures
  current_failures=$(
    TEMPORAL_ADDRESS="$FLEXPRICE_TEMPORAL_ADDRESS" \
    TEMPORAL_NAMESPACE="$FLEXPRICE_TEMPORAL_NAMESPACE" \
    TEMPORAL_API_KEY="$FLEXPRICE_TEMPORAL_API_KEY" \
    TEMPORAL_TLS=true \
    temporal workflow count \
      --query "WorkflowType='ReprocessRawEventsWorkflow' AND ExecutionStatus='Failed'" \
      2>/dev/null | sed -n 's/Total: //p' || echo "0"
  )
  local new_failures=$((current_failures - TEMPORAL_BASELINE_FAILURES))
  if (( new_failures > MAX_TEMPORAL_FAILURES )); then
    log "ABORT: ${new_failures} new Temporal failures (threshold: ${MAX_TEMPORAL_FAILURES})"
    log "  Total failures: ${current_failures} (baseline was ${TEMPORAL_BASELINE_FAILURES})"
    return 1
  fi
  if (( new_failures > 0 )); then
    log "  Temporal: ${new_failures} new failures so far (threshold: ${MAX_TEMPORAL_FAILURES})"
  fi
  return 0
}

###############################################################################
# Main loop
###############################################################################
global_start=$(date +%s)
customers_done=0
customers_skipped=0
customers_failed=0
total_windows_done=0

for cust_idx in "${!CUSTOMERS[@]}"; do
  cust_id="${CUSTOMERS[$cust_idx]}"

  if [[ "$SHUTDOWN" == "true" ]]; then
    log "Shutdown requested. Stopping after ${customers_done} customers."
    break
  fi

  events="${CUSTOMER_EVENTS[$cust_idx]}"
  tier_info=$(get_tier "$events")
  chunk_days="${tier_info%%|*}"
  sleep_after="${tier_info##*|}"

  customers_done=$((customers_done + 1))

  log ""
  log "============================================================"
  log " Customer ${customers_done}/${total_customers}: ${cust_id}"
  log " Events: ${events} | Chunk: ${chunk_days}-day | Sleep after: ${sleep_after}s"
  log "============================================================"

  # Generate time windows for this customer
  windows=()
  while IFS= read -r w; do
    windows+=("$w")
  done < <(generate_windows "$chunk_days")

  all_windows_ok=true
  windows_completed=0
  windows_skipped=0

  for window in "${windows[@]}"; do
    if [[ "$SHUTDOWN" == "true" ]]; then
      log "Shutdown requested during customer ${cust_id}. Stopping."
      break 2
    fi

    ws="${window%%|*}"
    we="${window##*|}"

    # Check resume
    if is_completed "$cust_id" "$ws" "$we"; then
      windows_skipped=$((windows_skipped + 1))
      continue
    fi

    log "  Window: ${ws} -> ${we}"

    # Run the reprocess script (no pipe — avoids SIGPIPE / exit 141)
    window_log=$(mktemp)
    set +e
    EXTERNAL_CUSTOMER_ID="$cust_id" \
    START_DATE="$ws" \
    END_DATE="$we" \
    BATCH_SIZE="$BATCH_SIZE" \
    API_CHUNK_SIZE="$API_CHUNK_SIZE" \
    API_PARALLEL="$API_PARALLEL" \
    DRY_RUN="$DRY_RUN" \
    SLEEP_BETWEEN_BATCHES="$SLEEP_BETWEEN_BATCHES" \
      "$REPROCESS_SCRIPT" > "$window_log" 2>&1
    exit_code=$?
    set -e

    # Show key lines from the log (summary only)
    if [[ -f "$window_log" ]]; then
      grep -E '(missing event IDs found|Sending|Done!|Total events|API successes|API failures|Nothing to do|Speed:.*API:)' "$window_log" \
        | while IFS= read -r line; do printf '    %s\n' "$line"; done
    fi
    rm -f "$window_log"

    if [[ "$exit_code" -eq 0 ]]; then
      record_progress "$cust_id" "$ws" "$we" "OK" "exit=0"
      windows_completed=$((windows_completed + 1))
      total_windows_done=$((total_windows_done + 1))
    else
      record_progress "$cust_id" "$ws" "$we" "FAIL" "exit=${exit_code}"
      log "  WARNING: Window ${ws}-${we} failed (exit=${exit_code}). Continuing."
      all_windows_ok=false
    fi

    # Sleep between chunks (skip if last window)
    last_window="${windows[${#windows[@]}-1]}"
    if [[ "$window" != "$last_window" ]]; then
      sleep "$CHUNK_SLEEP"
      # Check Temporal failures between windows
      if ! check_temporal_failures; then
        log "Aborting due to excessive Temporal failures."
        SHUTDOWN=true
        break 2
      fi
    fi
  done

  if [[ "$windows_skipped" -gt 0 ]]; then
    log "  Skipped ${windows_skipped} already-completed windows"
  fi

  if [[ "$all_windows_ok" == "true" ]]; then
    log "  Customer ${cust_id}: ALL windows OK (${windows_completed} completed, ${windows_skipped} skipped)"
  else
    log "  Customer ${cust_id}: Some windows FAILED"
    customers_failed=$((customers_failed + 1))
  fi

  # Progress + ETA
  now=$(date +%s)
  elapsed=$((now - global_start))
  if (( elapsed > 0 && customers_done > 0 )); then
    remaining_customers=$((total_customers - customers_done))
    avg_per_customer=$((elapsed / customers_done))
    eta_seconds=$((remaining_customers * avg_per_customer))
    eta_hours=$((eta_seconds / 3600))
    eta_min=$(( (eta_seconds % 3600) / 60 ))
    log "  Progress: ${customers_done}/${total_customers} | Elapsed: $((elapsed/3600))h$(( (elapsed%3600)/60 ))m | ETA: ~${eta_hours}h${eta_min}m"
  fi

  # Adaptive sleep between customers
  if [[ "$customers_done" -lt "$total_customers" && "$SHUTDOWN" != "true" ]]; then
    log "  Sleeping ${sleep_after}s before next customer ..."
    sleep "$sleep_after"
    # Check Temporal failures between customers
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
total_hours=$((total_elapsed / 3600))
total_min=$(( (total_elapsed % 3600) / 60 ))
total_sec=$((total_elapsed % 60))

log ""
log "================================================================"
log " ORCHESTRATION COMPLETE"
log "================================================================"
log "  Customers processed: ${customers_done} / ${total_customers}"
log "  Customers failed:    ${customers_failed}"
log "  Total windows:       ${total_windows_done}"
log "  Total time:          ${total_hours}h ${total_min}m ${total_sec}s"
log "  Progress file:       ${PROGRESS_FILE}"
if [[ "$SHUTDOWN" == "true" ]]; then
  log "  NOTE: Stopped early due to signal. Re-run to resume."
fi
log "================================================================"
