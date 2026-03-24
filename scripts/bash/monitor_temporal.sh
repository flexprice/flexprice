#!/usr/bin/env bash
#
# Monitors Temporal ReprocessRawEventsWorkflow status.
# Shows counts, recent failures, and error details.
#
# Usage:
#   source .env.backfill && ./monitor_temporal.sh [--loop]
#
set -euo pipefail

###############################################################################
# Parameters
###############################################################################
FLEXPRICE_TEMPORAL_ADDRESS="${FLEXPRICE_TEMPORAL_ADDRESS:?FLEXPRICE_TEMPORAL_ADDRESS required}"
FLEXPRICE_TEMPORAL_NAMESPACE="${FLEXPRICE_TEMPORAL_NAMESPACE:?FLEXPRICE_TEMPORAL_NAMESPACE required}"
FLEXPRICE_TEMPORAL_API_KEY="${FLEXPRICE_TEMPORAL_API_KEY:?FLEXPRICE_TEMPORAL_API_KEY required}"

export TEMPORAL_ADDRESS="$FLEXPRICE_TEMPORAL_ADDRESS"
export TEMPORAL_NAMESPACE="$FLEXPRICE_TEMPORAL_NAMESPACE"
export TEMPORAL_API_KEY="$FLEXPRICE_TEMPORAL_API_KEY"
export TEMPORAL_TLS=true

WF_TYPE="ReprocessRawEventsWorkflow"
LOOP="${1:-}"

###############################################################################
# Helpers
###############################################################################
log() { printf '[%s] %s\n' "$(date '+%Y-%m-%d %H:%M:%S')" "$*"; }

tc() {
  # temporal count helper
  temporal workflow count --query "$1" 2>/dev/null | sed -n 's/Total: //p'
}

###############################################################################
# Monitor
###############################################################################
run_monitor() {
  log "================================================================"
  log " Temporal Workflow Monitor: ${WF_TYPE}"
  log "================================================================"
  log ""

  # Counts
  completed=$(tc "WorkflowType='${WF_TYPE}' AND ExecutionStatus='Completed'")
  failed=$(tc "WorkflowType='${WF_TYPE}' AND ExecutionStatus='Failed'")
  running=$(tc "WorkflowType='${WF_TYPE}' AND ExecutionStatus='Running'")
  timed_out=$(tc "WorkflowType='${WF_TYPE}' AND ExecutionStatus='TimedOut'")
  canceled=$(tc "WorkflowType='${WF_TYPE}' AND ExecutionStatus='Canceled'")
  terminated=$(tc "WorkflowType='${WF_TYPE}' AND ExecutionStatus='Terminated'")

  total=$((completed + failed + running + timed_out + canceled + terminated))
  if (( total > 0 )); then
    success_pct=$((completed * 100 / total))
  else
    success_pct=0
  fi

  log "  Completed:   ${completed}"
  log "  Failed:      ${failed}"
  log "  Running:     ${running}"
  log "  Timed Out:   ${timed_out}"
  log "  Canceled:    ${canceled}"
  log "  Terminated:  ${terminated}"
  log "  --------------------------------"
  log "  Total:       ${total}"
  log "  Success:     ${success_pct}%"
  log ""

  # Recent completions (last 5)
  log "--- Recent Completed (last 5) ---"
  temporal workflow list \
    --query "WorkflowType='${WF_TYPE}' AND ExecutionStatus='Completed'" \
    --limit 5 2>/dev/null || true
  log ""

  # Recent failures (last 5)
  if (( failed > 0 )); then
    log "--- Recent Failed (last 5) ---"
    temporal workflow list \
      --query "WorkflowType='${WF_TYPE}' AND ExecutionStatus='Failed'" \
      --limit 5 2>/dev/null || true
    log ""

    # Get error from the most recent failure
    log "--- Most Recent Failure Detail ---"
    latest_failed_id=$(temporal workflow list \
      --query "WorkflowType='${WF_TYPE}' AND ExecutionStatus='Failed'" \
      --limit 1 -o json 2>/dev/null | jq -r '.[0].execution.workflowId // empty' 2>/dev/null || true)

    if [[ -n "$latest_failed_id" ]]; then
      log "  Workflow: ${latest_failed_id}"
      # Get failure reason
      temporal workflow show \
        --workflow-id "$latest_failed_id" \
        -o json 2>/dev/null \
        | jq -r '[.events[] | select(.eventType == "EVENT_TYPE_ACTIVITY_TASK_FAILED" or .eventType == "EVENT_TYPE_WORKFLOW_EXECUTION_FAILED")] | last | .activityTaskFailedEventAttributes.failure.message // .workflowExecutionFailedEventAttributes.failure.message // "unknown"' 2>/dev/null || true

      # Check if external_customer_ids was set
      log "  Input external_customer_ids:"
      temporal workflow show \
        --workflow-id "$latest_failed_id" \
        -o json 2>/dev/null \
        | jq -r '.events[0].workflowExecutionStartedEventAttributes.input.payloads[0].data' 2>/dev/null \
        | base64 -d 2>/dev/null \
        | jq '.external_customer_ids' 2>/dev/null || echo "  (unable to decode)"
    fi
    log ""
  fi

  # Running workflows detail
  if (( running > 0 )); then
    log "--- Currently Running ---"
    temporal workflow list \
      --query "WorkflowType='${WF_TYPE}' AND ExecutionStatus='Running'" \
      --limit 10 2>/dev/null || true
    log ""
  fi

  log "================================================================"
}

###############################################################################
# Main
###############################################################################
if ! command -v temporal &>/dev/null; then
  log "ERROR: 'temporal' CLI not found. Install with: brew install temporal"
  exit 1
fi

if [[ "$LOOP" == "--loop" ]]; then
  while true; do
    run_monitor
    log "Next check in 30s... (Ctrl+C to stop)"
    sleep 30
  done
else
  run_monitor
fi
