#!/usr/bin/env bash
set -euo pipefail

# ---------------------------------------------------------------
# validate_cost_analytics.sh
# Validates cost analytics: per-customer ClickHouse vs API comparison
#
# Required env vars:
#   CH_HOST       ClickHouse host
#   CH_PASSWORD   ClickHouse password
#   API_KEY       Flexprice API key
#
# Optional env vars (have defaults):
#   CH_PORT, CH_USER, CH_DB, API_BASE, CUSTOMERS_FILE
# ---------------------------------------------------------------

CH_HOST="${CH_HOST:-}"
CH_PORT="${CH_PORT:-9000}"
CH_USER="${CH_USER:-default}"
CH_PASSWORD="${CH_PASSWORD:-}"
CH_DB="${CH_DB:-flexprice}"

API_BASE="${API_BASE:-http://localhost:8080}"
API_KEY="${API_KEY:-}"

TENANT_ID="tenant_01KF5GXB4S7YKWH2Y3YQ1TEMQ3"
ENV_ID="env_01KG4E6FR5YCNW0742N6CA1YD1"
COSTSHEET_ID="cost_01KK7JTWKMBWJ0Q9RMHR77G12V"

START_TIME="2026-03-01T00:00:00Z"
END_TIME="2026-04-01T00:00:00Z"

TOLERANCE="0.02"

CUSTOMERS_FILE="${CUSTOMERS_FILE:-scripts/bash/enterprise-customer-ids.json}"

if [ -z "$CH_HOST" ] || [ -z "$CH_PASSWORD" ] || [ -z "$API_KEY" ]; then
  echo "ERROR: Set CH_HOST, CH_PASSWORD, and API_KEY as environment variables before running."
  exit 1
fi

ch() {
  clickhouse client \
    --host "$CH_HOST" --port "$CH_PORT" \
    --user "$CH_USER" --password "$CH_PASSWORD" \
    --database "$CH_DB" \
    "$@" 2>/dev/null
}

get_ch_cost() {
  local cid="$1"
  ch --query "
WITH costsheet_prices AS (
    SELECT id AS price_id, meter_id, amount
    FROM prices FINAL
    WHERE tenant_id = '$TENANT_ID'
      AND environment_id = '$ENV_ID'
      AND entity_type = 'COSTSHEET'
      AND entity_id = '$COSTSHEET_ID'
      AND status = 'published'
      AND meter_id IS NOT NULL AND meter_id != ''
),
meter_totals AS (
    SELECT meter_id, SUM(qty_total) AS total_qty
    FROM meter_usage FINAL
    WHERE tenant_id = '$TENANT_ID'
      AND environment_id = '$ENV_ID'
      AND external_customer_id = '$cid'
      AND timestamp >= '$START_TIME'
      AND timestamp < '$END_TIME'
    GROUP BY meter_id
    SETTINGS do_not_merge_across_partitions_select_final = 1
)
SELECT SUM(mt.total_qty * cp.amount) AS total_cost
FROM meter_totals mt
INNER JOIN costsheet_prices cp ON mt.meter_id = cp.meter_id
FORMAT TSV
"
}

get_api_cost() {
  local cid="$1"
  curl -s --max-time 120 -X POST "$API_BASE/v1/costs/analytics-v2" \
    -H "x-api-key: $API_KEY" \
    -H "Content-Type: application/json" \
    -d "{
      \"external_customer_id\": \"$cid\",
      \"start_time\": \"$START_TIME\",
      \"end_time\": \"$END_TIME\"
    }" 2>/dev/null | python3 -c "
import sys, json
try:
    d = json.load(sys.stdin)
    print(d.get('total_cost', '0'))
except:
    print('ERROR')
" 2>/dev/null
}

# Read customer IDs
CUSTOMER_IDS=$(python3 -c "import json; [print(c) for c in json.load(open('$CUSTOMERS_FILE'))]")
total=$(echo "$CUSTOMER_IDS" | wc -l | tr -d ' ')

echo "Validating $total customers for March 2026"
echo ""
printf "%-45s %15s %15s %15s %10s\n" "Customer ID" "CH Cost" "API Cost" "Diff" "Status"
echo "-----------------------------------------------------------------------------------------------------------"

matches=0
mismatches=0
errors=0
count=0

while IFS= read -r cid; do
    count=$((count + 1))

    # Get costs from both sources
    ch_cost=$(get_ch_cost "$cid" | tr -d '[:space:]')
    api_cost=$(get_api_cost "$cid" | tr -d '[:space:]')

    # Handle empty/null
    if [ -z "$ch_cost" ] || [ "$ch_cost" = "\\N" ]; then
        ch_cost="0"
    fi

    if [ -z "$api_cost" ] || [ "$api_cost" = "ERROR" ]; then
        printf "%-45s %15s %15s %15s %10s\n" "$cid" "$ch_cost" "ERROR" "N/A" "ERROR"
        errors=$((errors + 1))
        continue
    fi

    # Compare
    result=$(python3 -c "
ch = float('$ch_cost')
api = float('$api_cost')
diff = abs(ch - api)
if diff <= $TOLERANCE:
    print(f'{ch:.6f}\t{api:.6f}\t{diff:.6f}\tOK')
else:
    print(f'{ch:.6f}\t{api:.6f}\t{diff:.6f}\tMISMATCH')
" 2>/dev/null)

    ch_fmt=$(echo "$result" | cut -f1)
    api_fmt=$(echo "$result" | cut -f2)
    diff_fmt=$(echo "$result" | cut -f3)
    status=$(echo "$result" | cut -f4)

    printf "%-45s %15s %15s %15s %10s\n" "$cid" "$ch_fmt" "$api_fmt" "$diff_fmt" "$status"

    if [ "$status" = "OK" ]; then
        matches=$((matches + 1))
    else
        mismatches=$((mismatches + 1))
    fi
done <<< "$CUSTOMER_IDS"

echo "-----------------------------------------------------------------------------------------------------------"
echo ""
echo "Summary:"
echo "  Total: $total | Matches: $matches | Mismatches: $mismatches | Errors: $errors"
echo ""
if [ "$mismatches" -gt 0 ]; then
    echo "VALIDATION FAILED - $mismatches mismatches found"
    exit 1
else
    echo "VALIDATION PASSED - all customers match!"
fi
