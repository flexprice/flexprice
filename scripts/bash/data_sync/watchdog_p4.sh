#!/usr/bin/env bash
#
# watchdog_p4.sh — Monitors CH health during P4 reprocessing
# Kills orchestrate_p4_diff.sh if memory stays above threshold for 5+ minutes
#
# Usage: ./watchdog_p4.sh &
#
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
LOG_FILE="${SCRIPT_DIR}/reprocess_output/watchdog.log"
mkdir -p "$(dirname "$LOG_FILE")"

# Thresholds
MEM_THRESHOLD_MI=95000    # 95 GiB pod memory = danger zone
QUERY_MEM_THRESHOLD_GB=20 # 20 GiB query memory = danger zone
CONSECUTIVE_THRESHOLD=10  # 10 x 30s = 5 minutes sustained
CHECK_INTERVAL=30         # seconds between checks

# CH connection
CH_HOST="${CH_HOST:-}"
CH_PORT="${CH_PORT:-9000}"
CH_USER="${CH_USER:-default}"
CH_PASSWORD="${CH_PASSWORD:-}"
CH_DB="${CH_DB:-flexprice}"

log() {
  local msg
  msg=$(printf '[%s] %s' "$(date '+%Y-%m-%d %H:%M:%S')" "$*")
  echo "$msg"
  echo "$msg" >> "$LOG_FILE"
}

consecutive_high=0
total_checks=0
start_time=$(date +%s)

log "Watchdog started. Threshold: pod>${MEM_THRESHOLD_MI}Mi OR query>${QUERY_MEM_THRESHOLD_GB}GiB for ${CONSECUTIVE_THRESHOLD} consecutive checks ($(( CONSECUTIVE_THRESHOLD * CHECK_INTERVAL ))s)"

while true; do
  total_checks=$((total_checks + 1))

  # Get pod memory
  pod_mem="?"
  if command -v kubectl &>/dev/null; then
    pod_mem=$(kubectl top pod -n clickhousev2-mafga --containers 2>/dev/null \
      | grep "clickhousev2-mafga " \
      | awk '{gsub(/Mi/,"",$4); print $4}' || echo "0")
  fi

  # Get query memory
  query_mem_bytes="0"
  active_queries="0"
  if [[ -n "$CH_HOST" ]]; then
    query_mem_bytes=$(clickhouse client \
      --host "$CH_HOST" --port "$CH_PORT" \
      --user "$CH_USER" --password "$CH_PASSWORD" \
      --database "$CH_DB" --format TSV \
      --query "SELECT sum(memory_usage) FROM system.processes WHERE query NOT LIKE '%system.processes%'" 2>/dev/null || echo "0")
    active_queries=$(clickhouse client \
      --host "$CH_HOST" --port "$CH_PORT" \
      --user "$CH_USER" --password "$CH_PASSWORD" \
      --database "$CH_DB" --format TSV \
      --query "SELECT count() FROM system.processes WHERE query NOT LIKE '%system.processes%'" 2>/dev/null || echo "0")
  fi

  query_mem_gib=$(( ${query_mem_bytes:-0} / 1073741824 ))
  pod_mem_int="${pod_mem:-0}"

  # Check if above threshold
  is_high=false
  if (( pod_mem_int > MEM_THRESHOLD_MI )) || (( query_mem_gib > QUERY_MEM_THRESHOLD_GB )); then
    is_high=true
    consecutive_high=$((consecutive_high + 1))
  else
    if (( consecutive_high > 0 )); then
      log "Memory back to normal after ${consecutive_high} high checks. Pod=${pod_mem_int}Mi QueryMem=${query_mem_gib}GiB Queries=${active_queries}"
    fi
    consecutive_high=0
  fi

  # Log every check if high, or every 2 minutes if normal
  if [[ "$is_high" == "true" ]] || (( total_checks % 4 == 0 )); then
    status="OK"
    if [[ "$is_high" == "true" ]]; then
      status="HIGH(${consecutive_high}/${CONSECUTIVE_THRESHOLD})"
    fi
    log "[${status}] Pod=${pod_mem_int}Mi QueryMem=${query_mem_gib}GiB Queries=${active_queries}"
  fi

  # Kill if sustained high
  if (( consecutive_high >= CONSECUTIVE_THRESHOLD )); then
    log "DANGER: Memory above threshold for $(( consecutive_high * CHECK_INTERVAL ))s!"
    log "KILLING orchestrate_p4_diff.sh processes..."

    # Kill the orchestrator and diff scripts
    pkill -f "orchestrate_p4_diff.sh" 2>/dev/null || true
    pkill -f "diff_and_reprocess_p4.sh" 2>/dev/null || true

    log "Processes killed. Watchdog will continue monitoring..."
    consecutive_high=0

    # Wait 5 minutes before resuming monitoring
    sleep 300
  fi

  # Check if orchestrator is still running
  if ! pgrep -f "orchestrate_p4_diff.sh" &>/dev/null; then
    elapsed=$(( $(date +%s) - start_time ))
    log "Orchestrator not running. Watchdog exiting after $((elapsed/60))m."
    exit 0
  fi

  sleep "$CHECK_INTERVAL"
done
