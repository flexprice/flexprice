#!/usr/bin/env bash
#
# monitor_reprocess_p4.sh — Monitor Pipeline 4 reprocessing vitals
#
# Tracks: ClickHouse queries/memory, k8s pod resources, Temporal workflow status
#
# Usage:
#   cd scripts/bash/data_sync && source .env.backfill
#   ./monitor_reprocess_p4.sh                    # continuous (every 30s)
#   INTERVAL=60 ./monitor_reprocess_p4.sh        # every 60s
#   ONCE=true ./monitor_reprocess_p4.sh          # single snapshot
#
set -euo pipefail

INTERVAL="${INTERVAL:-30}"
ONCE="${ONCE:-false}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
LOG_FILE="${SCRIPT_DIR}/reprocess_output/monitor.log"
mkdir -p "$(dirname "$LOG_FILE")"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
CYAN='\033[0;36m'
NC='\033[0m'

CH_HOST="${CH_HOST:-}"
CH_PORT="${CH_PORT:-9000}"
CH_USER="${CH_USER:-default}"
CH_PASSWORD="${CH_PASSWORD:-}"
CH_DB="${CH_DB:-flexprice}"

ch_query() {
  local query="$1"
  if [[ -z "$CH_HOST" ]]; then
    echo "N/A"
    return
  fi
  clickhouse client \
    --host "$CH_HOST" --port "$CH_PORT" \
    --user "$CH_USER" --password "$CH_PASSWORD" \
    --database "$CH_DB" \
    --format TSV \
    --query "$query" 2>/dev/null || echo "ERR"
}

temporal_query() {
  if ! command -v temporal &>/dev/null || [[ -z "${FLEXPRICE_TEMPORAL_ADDRESS:-}" ]]; then
    echo "N/A"
    return
  fi
  TEMPORAL_ADDRESS="$FLEXPRICE_TEMPORAL_ADDRESS" \
  TEMPORAL_NAMESPACE="$FLEXPRICE_TEMPORAL_NAMESPACE" \
  TEMPORAL_API_KEY="$FLEXPRICE_TEMPORAL_API_KEY" \
  TEMPORAL_TLS=true \
  temporal "$@" 2>/dev/null
}

snapshot() {
  local ts
  ts=$(date '+%Y-%m-%d %H:%M:%S')

  echo ""
  echo -e "${CYAN}================================================================${NC}"
  echo -e "${CYAN} P4 REPROCESSING MONITOR — ${ts}${NC}"
  echo -e "${CYAN}================================================================${NC}"

  # 1. ClickHouse active queries
  echo -e "\n${YELLOW}--- ClickHouse Active Queries ---${NC}"
  local active_queries
  active_queries=$(ch_query "SELECT count() FROM system.processes WHERE query NOT LIKE '%system.processes%'")
  local fu_queries
  fu_queries=$(ch_query "SELECT count() FROM system.processes WHERE query LIKE '%feature_usage%' AND query NOT LIKE '%system.processes%'")
  echo "  Total active queries: ${active_queries}"
  echo "  Feature_usage queries: ${fu_queries}"

  # Show heavy queries
  local heavy
  heavy=$(ch_query "
    SELECT
      formatReadableSize(memory_usage) AS mem,
      round(elapsed, 1) AS secs,
      substring(query, 1, 100) AS q
    FROM system.processes
    WHERE query NOT LIKE '%system.processes%'
      AND memory_usage > 100000000
    ORDER BY memory_usage DESC
    LIMIT 5
  ")
  if [[ -n "$heavy" && "$heavy" != "ERR" ]]; then
    echo "  Heavy queries (>100MB):"
    echo "$heavy" | while IFS=$'\t' read -r mem secs q; do
      echo "    ${mem} | ${secs}s | ${q}"
    done
  else
    echo -e "  Heavy queries: ${GREEN}none${NC}"
  fi

  # 2. ClickHouse memory
  echo -e "\n${YELLOW}--- ClickHouse Memory ---${NC}"
  local total_query_mem
  total_query_mem=$(ch_query "SELECT formatReadableSize(sum(memory_usage)) FROM system.processes WHERE query NOT LIKE '%system.processes%'")
  echo "  Total query memory: ${total_query_mem}"

  # 3. K8s pod resources (if kubectl available)
  echo -e "\n${YELLOW}--- K8s ClickHouse Pod ---${NC}"
  if command -v kubectl &>/dev/null; then
    local pod_resources
    pod_resources=$(kubectl top pod -n clickhousev2-mafga --containers 2>/dev/null | grep clickhousev2-mafga || echo "N/A")
    if [[ "$pod_resources" != "N/A" ]]; then
      echo "$pod_resources" | while read -r pod container cpu mem; do
        if [[ "$container" == "clickhousev2-mafga" ]]; then
          # Check if memory is dangerously high (>90GB)
          local mem_val=${mem%Mi}
          if [[ "$mem_val" =~ ^[0-9]+$ ]] && (( mem_val > 92000 )); then
            echo -e "  ${RED}CPU: ${cpu} | Memory: ${mem} — DANGER: HIGH MEMORY${NC}"
          elif [[ "$mem_val" =~ ^[0-9]+$ ]] && (( mem_val > 80000 )); then
            echo -e "  ${YELLOW}CPU: ${cpu} | Memory: ${mem} — WARNING: elevated${NC}"
          else
            echo -e "  CPU: ${cpu} | Memory: ${mem}"
          fi
        fi
      done
    else
      echo "  kubectl not configured or pod not found"
    fi
  else
    echo "  kubectl not available"
  fi

  # 4. Temporal workflows
  echo -e "\n${YELLOW}--- Temporal ReprocessEventsWorkflow ---${NC}"
  local running
  running=$(temporal_query workflow count \
    --query "WorkflowType='ReprocessEventsWorkflow' AND ExecutionStatus='Running'" 2>/dev/null \
    | sed -n 's/Total: //p' || echo "N/A")
  local failed
  failed=$(temporal_query workflow count \
    --query "WorkflowType='ReprocessEventsWorkflow' AND ExecutionStatus='Failed'" 2>/dev/null \
    | sed -n 's/Total: //p' || echo "N/A")
  local completed
  completed=$(temporal_query workflow count \
    --query "WorkflowType='ReprocessEventsWorkflow' AND ExecutionStatus='Completed'" 2>/dev/null \
    | sed -n 's/Total: //p' || echo "N/A")

  if [[ "$running" != "0" && "$running" != "N/A" ]]; then
    echo -e "  Running: ${YELLOW}${running}${NC}"
  else
    echo -e "  Running: ${GREEN}${running}${NC}"
  fi
  echo "  Completed: ${completed}"
  if [[ "$failed" != "0" && "$failed" != "N/A" ]]; then
    echo -e "  Failed: ${RED}${failed}${NC}"
  else
    echo -e "  Failed: ${GREEN}${failed}${NC}"
  fi

  # 5. Reprocess progress (if file exists)
  local progress_file="${SCRIPT_DIR}/reprocess_output/progress.log"
  if [[ -f "$progress_file" ]]; then
    echo -e "\n${YELLOW}--- Reprocess Script Progress ---${NC}"
    local total_ok
    total_ok=$(grep -c '|OK|' "$progress_file" 2>/dev/null || echo "0")
    local total_fail
    total_fail=$(grep -c '|FAIL|' "$progress_file" 2>/dev/null || echo "0")
    local last_entry
    last_entry=$(tail -1 "$progress_file" 2>/dev/null || echo "none")
    echo "  Windows OK: ${total_ok}"
    echo "  Windows FAIL: ${total_fail}"
    echo "  Last entry: ${last_entry}"
  fi

  echo -e "\n${CYAN}================================================================${NC}"

  # Log to file
  printf '[%s] active=%s fu_queries=%s query_mem=%s running=%s failed=%s completed=%s\n' \
    "$ts" "$active_queries" "$fu_queries" "$total_query_mem" "$running" "$failed" "$completed" >> "$LOG_FILE"
}

# Main loop
snapshot

if [[ "$ONCE" != "true" ]]; then
  echo ""
  echo "Monitoring every ${INTERVAL}s. Ctrl+C to stop."
  while true; do
    sleep "$INTERVAL"
    snapshot
  done
fi
