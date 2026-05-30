#!/usr/bin/env bash
# ---------------------------
# migrate_all_tenants.sh
#
# Wrapper around migrate_feature_usage_to_meter_usage.sh that:
#   1. Discovers all distinct (tenant_id, environment_id) pairs in feature_usage
#      for the given date range.
#   2. Runs the per-tenant migration sequentially.
#   3. Continues to the next tenant if one fails (logs the failure).
#
# USAGE (typical, on bastion):
#   set -a; source scripts/bash/.env.backfill; set +a
#   START_DATE=2025-01-01 END_DATE_EXCL=2026-05-28 \
#     bash scripts/bash/migrate_all_tenants.sh
#
# Resume / re-run is safe: per-day idempotency in the inner script will SKIP
# any (tenant, env, day) already populated in meter_usage.
# ---------------------------

set -uo pipefail  # NOTE: no -e — we want to continue past a failed tenant

# ---- ClickHouse connection (from .env.backfill, already exported) ----
CH_HOST="${CH_HOST:?CH_HOST not set — did you 'set -a; source .env.backfill; set +a'?}"
CH_PORT="${CH_PORT:-9000}"
CH_USER="${CH_USER:-default}"
CH_PASSWORD="${CH_PASSWORD:-}"
CH_DB="${CH_DB:-flexprice}"

# ---- Date range ----
START_DATE="${START_DATE:-2025-01-01}"
END_DATE_EXCL="${END_DATE_EXCL:-2026-05-28}"

# ---- Inner-script knobs (passed through) ----
PARALLEL="${PARALLEL:-1}"        # keep sequential by default
FORCE="${FORCE:-0}"

# ---- Wrapper logging ----
ROOT_LOG_DIR="${ROOT_LOG_DIR:-./logs/migrate_all_tenants_$(date +%Y%m%d_%H%M%S)}"
mkdir -p "$ROOT_LOG_DIR"
SUMMARY="$ROOT_LOG_DIR/SUMMARY.log"
PAIRS_FILE="$ROOT_LOG_DIR/tenant_env_pairs.tsv"

log_summary() { echo "[$(date -Iseconds)] $*" | tee -a "$SUMMARY"; }

log_summary "============================================="
log_summary "  MULTI-TENANT feature_usage → meter_usage"
log_summary "  Range:       ${START_DATE} → ${END_DATE_EXCL} (exclusive)"
log_summary "  Parallelism: ${PARALLEL} (per tenant; tenants run serially)"
log_summary "  Force:       ${FORCE}"
log_summary "  Root logs:   ${ROOT_LOG_DIR}"
log_summary "============================================="

# ---- Discover (tenant_id, environment_id) pairs ----
log_summary "Discovering tenants from feature_usage..."
clickhouse client \
  --host "$CH_HOST" --port "$CH_PORT" \
  --user "$CH_USER" --password "$CH_PASSWORD" \
  --database "$CH_DB" \
  --query "
    SELECT DISTINCT tenant_id, environment_id
    FROM feature_usage
    WHERE timestamp >= toDateTime64('${START_DATE} 00:00:00', 3)
      AND timestamp <  toDateTime64('${END_DATE_EXCL} 00:00:00', 3)
    ORDER BY tenant_id, environment_id
    FORMAT TSV
  " </dev/null > "$PAIRS_FILE"

discovery_exit=$?
if [[ $discovery_exit -ne 0 ]]; then
  log_summary "ERROR: discovery query failed (exit=$discovery_exit). Aborting."
  exit 1
fi

total=$(wc -l < "$PAIRS_FILE" | tr -d ' ')
log_summary "Found ${total} (tenant, env) pairs"
log_summary "Pair list: ${PAIRS_FILE}"
log_summary "---"

if [[ "$total" -eq 0 ]]; then
  log_summary "Nothing to do. Exiting."
  exit 0
fi

# ---- Loop over pairs ----
i=0
ok_count=0
fail_count=0
failed_pairs=()

while IFS=$'\t' read -r tenant env; do
  i=$((i+1))
  [[ -z "$tenant" || -z "$env" ]] && continue

  log_summary "[${i}/${total}] START  tenant=${tenant}  env=${env}"

  # Sanitise env id for filesystem (env_xxx is fine but defensive)
  safe_tenant="$(echo "$tenant" | tr -c 'A-Za-z0-9._-' '_')"
  safe_env="$(echo "$env" | tr -c 'A-Za-z0-9._-' '_')"
  per_tenant_log_dir="${ROOT_LOG_DIR}/${safe_tenant}__${safe_env}"

  TENANT_ID="$tenant" \
  ENVIRONMENT_ID="$env" \
  PARALLEL="$PARALLEL" \
  FORCE="$FORCE" \
  START_DATE="$START_DATE" \
  END_DATE_EXCL="$END_DATE_EXCL" \
  LOG_DIR="$per_tenant_log_dir" \
    bash "$(dirname "$0")/migrate_feature_usage_to_meter_usage.sh" \
    >> "${ROOT_LOG_DIR}/run.stdout.log" 2>> "${ROOT_LOG_DIR}/run.stderr.log"
  rc=$?

  if [[ $rc -eq 0 ]]; then
    ok_count=$((ok_count+1))
    log_summary "[${i}/${total}] OK     tenant=${tenant}  env=${env}"
  else
    fail_count=$((fail_count+1))
    failed_pairs+=("${tenant}	${env}")
    log_summary "[${i}/${total}] FAIL   tenant=${tenant}  env=${env}  exit=${rc} (continuing)"
  fi
done < "$PAIRS_FILE"

log_summary "---"
log_summary "Total:   ${total}"
log_summary "OK:      ${ok_count}"
log_summary "FAILED:  ${fail_count}"

if [[ $fail_count -gt 0 ]]; then
  failed_file="${ROOT_LOG_DIR}/failed_pairs.tsv"
  printf "%s\n" "${failed_pairs[@]}" > "$failed_file"
  log_summary "Failed pairs written to: ${failed_file}"
  log_summary "Re-run those tenants with:  (loop over $failed_file and call the inner script)"
fi

log_summary "============================================="
log_summary "Done. Logs: ${ROOT_LOG_DIR}"
log_summary "============================================="
