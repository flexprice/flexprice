#!/usr/bin/env bash
#
# Finds events in VAPI billing_entries that are missing from flexprice raw_events,
# then pushes them via the POST /v1/events/raw/bulk API endpoint.
#
# The API transforms BentoInput payloads and publishes to the events topic.
#
# Prerequisites:
#   - curl, jq
#
# Usage:
#   ./backfill_via_api.sh
#
set -euo pipefail

source "/Users/nikhilmishra/work/flexprice/scripts/bash/.env.backfill"

###############################################################################
# Configuration
###############################################################################
START_DATE="${START_DATE:-2026-02-01 00:00:00}"
END_DATE="${END_DATE:-2026-03-12 23:59:59}"

CUSTOMER_IDS="${CUSTOMER_IDS:-}"

# API config
API_BASE_URL="${API_BASE_URL:-http://localhost:8080}"
API_KEY="${API_KEY:-sk_01KG4PESKBDPBESR4YSH9ZZ88D}"

# Batching
BATCH_SIZE="${BATCH_SIZE:-500}"
ID_FETCH_BATCH="${ID_FETCH_BATCH:-50000}"
DRY_RUN="${DRY_RUN:-false}"
SLEEP_BETWEEN_BATCHES="${SLEEP_BETWEEN_BATCHES:-0}"

###############################################################################
# Helpers
###############################################################################
log() { printf '[%s] %s\n' "$(date '+%Y-%m-%d %H:%M:%S')" "$*"; }

fp_query() {
  local query="$1"
  curl -s "http://${CH_HOST}:8123/?database=${CH_DB}" \
    --user "${CH_USER}:${CH_PASSWORD}" \
    -d "$query"
}

vapi_query() {
  local query="$1"
  curl -s "https://${VAPI_CH_HOST}:${VAPI_CH_PORT}/?database=${VAPI_CH_DB}" \
    --user "${VAPI_CH_USER}:${VAPI_CH_PASSWORD}" \
    -d "$query"
}

for cmd in curl jq; do
  if ! command -v "$cmd" &>/dev/null; then
    log "ERROR: '$cmd' is required but not installed."
    exit 1
  fi
done

###############################################################################
# Temp files
###############################################################################
FP_IDS_FILE=$(mktemp)
VAPI_IDS_FILE=$(mktemp)
MISSING_IDS_FILE=$(mktemp)
VAPI_BATCH_FILE=$(mktemp)
trap 'rm -f "$FP_IDS_FILE" "$VAPI_IDS_FILE" "$MISSING_IDS_FILE" "$VAPI_BATCH_FILE"' EXIT

###############################################################################
# Process each customer
###############################################################################
process_customer() {
  local cust_id="$1"

  log ""
  log "============================================================"
  log " Processing customer: ${cust_id}"
  log "============================================================"

  # --- Step 1: Get IDs from flexprice raw_events ---
  log "  [1/4] Fetching IDs from flexprice raw_events..."
  fp_query "
    SELECT id
    FROM flexprice.raw_events FINAL
    WHERE external_customer_id = '${cust_id}'
      AND tenant_id = '${TENANT_ID}'
      AND environment_id = '${ENVIRONMENT_ID}'
      AND timestamp >= toDateTime64('${START_DATE}', 3)
      AND timestamp <= toDateTime64('${END_DATE}', 3)
      AND sign = 1
    ORDER BY id
    FORMAT TabSeparated
  " | sort > "$FP_IDS_FILE"
  local fp_count
  fp_count=$(wc -l < "$FP_IDS_FILE" | tr -d '[:space:]')
  log "    -> ${fp_count} IDs in flexprice"

  # --- Step 2: Get IDs from VAPI billing_entries ---
  log "  [2/4] Fetching IDs from VAPI billing_entries..."
  vapi_query "
    SELECT toString(id)
    FROM default.billing_entries
    WHERE org_id = '${cust_id}'
      AND created_at >= toDateTime64('${START_DATE}', 3)
      AND created_at <= toDateTime64('${END_DATE}', 3)
    ORDER BY toString(id)
    FORMAT TabSeparated
  " | sort > "$VAPI_IDS_FILE"
  local vapi_count
  vapi_count=$(wc -l < "$VAPI_IDS_FILE" | tr -d '[:space:]')
  log "    -> ${vapi_count} IDs in VAPI"

  # --- Step 3: Find missing IDs ---
  log "  [3/4] Computing diff..."
  comm -23 "$VAPI_IDS_FILE" "$FP_IDS_FILE" > "$MISSING_IDS_FILE"
  local missing_count
  missing_count=$(wc -l < "$MISSING_IDS_FILE" | tr -d '[:space:]')
  log "    -> ${missing_count} missing events to backfill"

  if [[ "$missing_count" == "0" ]]; then
    log "  Nothing to do for this customer."
    return 0
  fi

  # --- Step 4: Fetch full records from VAPI and call API ---
  log "  [4/4] Fetching and pushing missing events via API..."

  local total_success=0
  local total_skipped=0
  local total_errors=0
  local offset=0

  while (( offset < missing_count )); do
    local chunk_ids
    chunk_ids=$(sed -n "$((offset + 1)),$((offset + ID_FETCH_BATCH))p" "$MISSING_IDS_FILE")
    local chunk_size
    chunk_size=$(echo "$chunk_ids" | wc -l | tr -d '[:space:]')

    log "    Fetching chunk: offset=${offset}, size=${chunk_size}"

    local ids_sql
    ids_sql=$(echo "$chunk_ids" | awk '{printf "%s\x27%s\x27", (NR>1 ? "," : ""), $0}')

    local fetch_query="
      SELECT
        toString(id) AS id,
        org_id AS orgId,
        ifNull(method_name, '') AS methodName,
        ifNull(provider_name, '') AS providerName,
        '' AS serviceName,
        formatDateTime(created_at, '%Y-%m-%dT%H:%i:%S.000Z') AS createdAt,
        ifNull(target_item_id, '') AS targetItemId,
        byok AS byok,
        ifNull(data_interface, '') AS dataInterface,
        ifNull(reference_cost, 0) AS referenceCost,
        ifNull(target_item_type, '') AS targetItemType,
        ifNull(reference_type, '') AS referenceType,
        ifNull(reference_id, '') AS referenceId,
        if(isNull(started_at), '', formatDateTime(started_at, '%Y-%m-%dT%H:%i:%S.000Z')) AS startedAt,
        if(isNull(updated_at), '', formatDateTime(updated_at, '%Y-%m-%dT%H:%i:%S.000Z')) AS updatedAt,
        if(isNull(ended_at), '', formatDateTime(ended_at, '%Y-%m-%dT%H:%i:%S.000Z')) AS endedAt,
        data AS data
      FROM default.billing_entries
      WHERE id IN (${ids_sql})
        AND org_id = '${cust_id}'
      FORMAT JSONEachRow
    "

    vapi_query "$fetch_query" > "$VAPI_BATCH_FILE"

    local fetched_count
    fetched_count=$(wc -l < "$VAPI_BATCH_FILE" | tr -d '[:space:]')
    log "    Fetched ${fetched_count} records from VAPI"

    # Split into BATCH_SIZE chunks and POST to API
    local batch_offset=0
    while (( batch_offset < fetched_count )); do
      local batch_lines
      batch_lines=$(sed -n "$((batch_offset + 1)),$((batch_offset + BATCH_SIZE))p" "$VAPI_BATCH_FILE")
      local batch_count
      batch_count=$(echo "$batch_lines" | wc -l | tr -d '[:space:]')

      # Build the request body: {"data": [<BentoInput objects>]}
      local data_array
      data_array=$(echo "$batch_lines" | jq -s -c '.')

      local request_body
      request_body=$(jq -c -n --argjson data "$data_array" '{ data: $data }')

      if [[ "$DRY_RUN" == "true" ]]; then
        log "    [DRY RUN] Would POST batch: ${batch_count} events (offset ${batch_offset})"
        total_success=$((total_success + batch_count))
      else
        # POST to API
        local api_response
        api_response=$(curl -s -w "\n%{http_code}" \
          --max-time 120 \
          -X POST "${API_BASE_URL}/v1/events/raw/bulk" \
          -H "Content-Type: application/json" \
          -H "x-api-key: ${API_KEY}" \
          -H "X-Environment-ID: ${ENVIRONMENT_ID}" \
          -d "$request_body")

        local http_code
        http_code=$(echo "$api_response" | tail -1)
        local response_body
        response_body=$(echo "$api_response" | sed '$d')

        if [[ "$http_code" == "202" ]]; then
          local batch_success batch_skip batch_err
          batch_success=$(echo "$response_body" | jq -r '.success_count // 0')
          batch_skip=$(echo "$response_body" | jq -r '.skip_count // 0')
          batch_err=$(echo "$response_body" | jq -r '.error_count // 0')
          total_success=$((total_success + batch_success))
          total_skipped=$((total_skipped + batch_skip))
          total_errors=$((total_errors + batch_err))
          log "    Batch ok: success=${batch_success} skip=${batch_skip} err=${batch_err}"
        else
          total_errors=$((total_errors + batch_count))
          log "    WARNING: API returned HTTP ${http_code}: ${response_body}"
        fi
      fi

      batch_offset=$((batch_offset + BATCH_SIZE))

      if (( SLEEP_BETWEEN_BATCHES > 0 )); then
        sleep "$SLEEP_BETWEEN_BATCHES"
      fi
    done

    offset=$((offset + chunk_size))
  done

  log "  Customer ${cust_id}: success=${total_success}, skipped=${total_skipped}, errors=${total_errors}"
  return 0
}

###############################################################################
# Banner
###############################################################################
log "============================================================"
log " Backfill Missing Events via API (VAPI -> /v1/events/raw/bulk)"
log "============================================================"
log " Tenant:          ${TENANT_ID}"
log " Environment:     ${ENVIRONMENT_ID}"
log " Time range:      ${START_DATE} to ${END_DATE}"
log " API base URL:    ${API_BASE_URL}"
log " Batch size:      ${BATCH_SIZE} events per request"
log " ID fetch batch:  ${ID_FETCH_BATCH}"
log " Dry run:         ${DRY_RUN}"
log "============================================================"

###############################################################################
# Default customer list
###############################################################################
if [[ -z "$CUSTOMER_IDS" ]]; then
  CUSTOMER_IDS="2223d30b-de9a-4832-8441-7f832509f8eb 22de896a-8c30-4155-b96e-ca3bf26e9870 9b4b815a-ef75-4e81-a3ec-5005d58edcc4 54371f35-4cce-4956-9c50-11faa1976425 55df69d9-3758-4400-8986-d5ac9a5d80f9 7b816f43-58d4-421d-9921-a080ce2b4ff4 a6e07aa0-aed3-4cf1-bb2f-0e8d683b9c20 b3ce3a2e-eecc-4231-b684-81412f7b2899 52fd687a-62ef-4d31-93b5-c5afa0fbd0d2 f19e0049-f5da-43c1-9915-06081ea213d6 843d7884-255c-47e1-b519-f58c3ac076f3 d62fb741-0d77-4e72-a7b3-68e2b498a45d"
fi

###############################################################################
# Main loop
###############################################################################
global_start=$(date +%s)
total_customers=0

for cust_id in $CUSTOMER_IDS; do
  total_customers=$((total_customers + 1))
  process_customer "$cust_id"
done

###############################################################################
# Summary
###############################################################################
end_epoch=$(date +%s)
total_elapsed=$((end_epoch - global_start))
total_min=$((total_elapsed / 60))
total_sec=$((total_elapsed % 60))

log ""
log "============================================================"
log " COMPLETE"
log "============================================================"
log " Customers processed: ${total_customers}"
log " Total time:          ${total_min}m ${total_sec}s"
log "============================================================"
