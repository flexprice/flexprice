#!/usr/bin/env bash
set -euo pipefail

CH_HOST="${CH_HOST:-}"
CH_PORT="${CH_PORT:-9000}"
CH_USER="${CH_USER:-default}"
CH_PASSWORD="${CH_PASSWORD:-}"
CH_DB="${CH_DB:-flexprice}"
API_KEY="${API_KEY:-}"
API_BASE="${API_BASE:-http://localhost:8080}"

TENANT="tenant_01KF5GXB4S7YKWH2Y3YQ1TEMQ3"
ENV="env_01KG4E6FR5YCNW0742N6CA1YD1"
CS="cost_01KK7JTWKMBWJ0Q9RMHR77G12V"

if [ -z "$CH_PASSWORD" ] || [ -z "$API_KEY" ] || [ -z "$CH_HOST" ]; then
  echo "ERROR: Set CH_HOST, CH_PASSWORD, and API_KEY as environment variables before running."
  exit 1
fi

CUSTOMERS_FILE="${CUSTOMERS_FILE:-scripts/bash/enterprise-customer-ids.json}"
CUSTOMERS=$(python3 -c "import json; [print(c) for c in json.load(open('$CUSTOMERS_FILE'))]")

printf "%-45s %15s %15s %15s %10s\n" "Customer" "CH" "API" "Diff" "Status"
echo "-----------------------------------------------------------------------------------------------------------"

matches=0
mismatches=0
errors=0

while IFS= read -r cid; do
    # ClickHouse direct cost
    ch_cost=$(clickhouse client \
        --host "$CH_HOST" --port "$CH_PORT" \
        --user "$CH_USER" --password "$CH_PASSWORD" \
        --database "$CH_DB" \
        --query "
WITH cp AS (
    SELECT meter_id, amount FROM prices FINAL
    WHERE tenant_id = '$TENANT' AND environment_id = '$ENV'
      AND entity_type = 'COSTSHEET' AND entity_id = '$CS'
      AND status = 'published' AND meter_id IS NOT NULL AND meter_id != ''
),
mt AS (
    SELECT meter_id, SUM(qty_total) AS total_qty FROM meter_usage FINAL
    WHERE tenant_id = '$TENANT' AND environment_id = '$ENV'
      AND external_customer_id = '$cid'
      AND timestamp >= '2026-03-01' AND timestamp < '2026-04-01'
    GROUP BY meter_id SETTINGS do_not_merge_across_partitions_select_final = 1
)
SELECT COALESCE(SUM(mt.total_qty * cp.amount), 0) FROM mt INNER JOIN cp ON mt.meter_id = cp.meter_id FORMAT TSV
" 2>/dev/null | tr -d '[:space:]')

    # API cost
    api_response=$(curl -s --max-time 120 -X POST "$API_BASE/v1/costs/analytics-v2" \
        -H "x-api-key: $API_KEY" \
        -H "Content-Type: application/json" \
        -d "{\"external_customer_id\":\"$cid\",\"start_time\":\"2026-03-01T00:00:00Z\",\"end_time\":\"2026-04-01T00:00:00Z\"}" 2>/dev/null)

    api_cost=$(echo "$api_response" | python3 -c "
import sys, json
try:
    d = json.load(sys.stdin)
    print(d.get('total_cost', '0'))
except:
    print('ERROR')
" 2>/dev/null | tr -d '[:space:]')

    # Handle empty
    ch_cost="${ch_cost:-0}"
    api_cost="${api_cost:-ERROR}"

    if [ "$api_cost" = "ERROR" ]; then
        printf "%-45s %15s %15s %15s %10s\n" "$cid" "$ch_cost" "ERROR" "N/A" "ERROR"
        errors=$((errors + 1))
        continue
    fi

    # Compare
    python3 -c "
ch = float('$ch_cost')
api = float('$api_cost')
diff = abs(ch - api)
status = 'OK' if diff <= 0.02 else 'MISMATCH'
print(f'%-45s %15.6f %15.6f %15.6f %10s' % ('$cid', ch, api, diff, status))
" 2>/dev/null

    diff_check=$(python3 -c "print('OK' if abs(float('$ch_cost') - float('$api_cost')) <= 0.02 else 'MISMATCH')" 2>/dev/null)
    if [ "$diff_check" = "OK" ]; then
        matches=$((matches + 1))
    else
        mismatches=$((mismatches + 1))
    fi
done <<< "$CUSTOMERS"

echo "-----------------------------------------------------------------------------------------------------------"
echo ""
echo "Summary: Matches=$matches  Mismatches=$mismatches  Errors=$errors"
if [ "$mismatches" -gt 0 ]; then
    echo "VALIDATION FAILED"
    exit 1
else
    echo "VALIDATION PASSED"
fi
