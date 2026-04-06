#!/usr/bin/env bash
#
# run_p4_forever.sh — Keeps orchestrate_p4_diff.sh running until all customers are done.
# Auto-restarts on any failure. Progress file ensures no double-processing.
#
# Usage:
#   cd scripts/bash/data_sync && source .env.backfill
#   START_DATE=2026-02-01T00:00:00Z END_DATE=2026-03-26T00:00:00Z nohup ./run_p4_forever.sh &
#
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
LOG_FILE="${SCRIPT_DIR}/reprocess_output/p4_forever.log"
PROGRESS_FILE="${SCRIPT_DIR}/reprocess_output/p4_diff_progress.log"
MAX_RESTARTS=100
RESTART_DELAY=60  # seconds between restarts

log() { printf '[%s] %s\n' "$(date '+%Y-%m-%d %H:%M:%S')" "$*" | tee -a "$LOG_FILE"; }

mkdir -p "$(dirname "$LOG_FILE")"

restart_count=0

while (( restart_count < MAX_RESTARTS )); do
  # Check if all 50 customers are done (minus the ~4 zero-gap ones)
  completed=$(wc -l < "$PROGRESS_FILE" 2>/dev/null | tr -d '[:space:]' || echo "0")
  if (( completed >= 46 )); then
    log "All customers appear complete (${completed} in progress file). Exiting."
    break
  fi

  restart_count=$((restart_count + 1))
  log "=== Starting orchestrator (attempt ${restart_count}/${MAX_RESTARTS}, ${completed}/~46 done) ==="

  "${SCRIPT_DIR}/orchestrate_p4_diff.sh" 2>&1 | tee -a "$LOG_FILE"
  exit_code=$?

  completed=$(wc -l < "$PROGRESS_FILE" 2>/dev/null | tr -d '[:space:]' || echo "0")
  log "Orchestrator exited (code=${exit_code}). ${completed} customers completed so far."

  if (( completed >= 46 )); then
    log "All customers done. Exiting."
    break
  fi

  log "Waiting ${RESTART_DELAY}s before restart..."
  sleep "$RESTART_DELAY"
done

log "=== run_p4_forever.sh finished. Total restarts: ${restart_count}. Completed: ${completed}/~46 ==="
