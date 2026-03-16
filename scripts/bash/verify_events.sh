#!/bin/bash
# Verify event counts between flexprice raw_events and vapi billing_entries
# Compares unique event IDs per customer for a given time range

set -euo pipefail

source "/Users/nikhilmishra/work/flexprice/scripts/bash/.env.backfill"

# --- Config ---
START_DATE="2026-02-01 00:00:00"
END_DATE="2026-03-12 23:59:59"

CUSTOMER_IDS=(
  "2223d30b-de9a-4832-8441-7f832509f8eb"
  "22de896a-8c30-4155-b96e-ca3bf26e9870"
  "9b4b815a-ef75-4e81-a3ec-5005d58edcc4"
  "54371f35-4cce-4956-9c50-11faa1976425"
  "55df69d9-3758-4400-8986-d5ac9a5d80f9"
  "7b816f43-58d4-421d-9921-a080ce2b4ff4"
  "a6e07aa0-aed3-4cf1-bb2f-0e8d683b9c20"
  "b3ce3a2e-eecc-4231-b684-81412f7b2899"
  "52fd687a-62ef-4d31-93b5-c5afa0fbd0d2"
  "f19e0049-f5da-43c1-9915-06081ea213d6"
  "843d7884-255c-47e1-b519-f58c3ac076f3"
  "d62fb741-0d77-4e72-a7b3-68e2b498a45d"
)

# Build comma-separated quoted list for SQL IN clause
IDS_SQL=$(printf "'%s'," "${CUSTOMER_IDS[@]}")
IDS_SQL="${IDS_SQL%,}"  # trim trailing comma

echo "============================================="
echo "Event Verification: Flexprice vs VAPI"
echo "Period: $START_DATE to $END_DATE"
echo "Customers: ${#CUSTOMER_IDS[@]}"
echo "============================================="
echo ""

# --- Step 1: Query Flexprice (raw_events) ---
# Using HTTP interface (port 8123, no TLS for this host)
echo ">>> Querying Flexprice raw_events..."

FLEXPRICE_QUERY="
SELECT
    external_customer_id,
    uniqExact(id) AS unique_events
FROM flexprice.raw_events FINAL
WHERE external_customer_id IN ($IDS_SQL)
  AND tenant_id = '$TENANT_ID'
  AND environment_id = '$ENVIRONMENT_ID'
  AND timestamp >= toDateTime64('$START_DATE', 3)
  AND timestamp <= toDateTime64('$END_DATE', 3)
  AND sign = 1
GROUP BY external_customer_id
ORDER BY external_customer_id
FORMAT TabSeparatedWithNames
"

FLEXPRICE_RESULT=$(curl -s \
  "http://${CH_HOST}:8123/?database=${CH_DB}" \
  --user "${CH_USER}:${CH_PASSWORD}" \
  -d "$FLEXPRICE_QUERY" 2>&1)

if echo "$FLEXPRICE_RESULT" | grep -q "DB::Exception"; then
  echo "Flexprice query failed: $FLEXPRICE_RESULT"
  exit 1
fi

echo "Flexprice results:"
echo "$FLEXPRICE_RESULT"
echo ""

# --- Step 2: Query VAPI (billing_entries) ---
# Using HTTPS (port 8443)
echo ">>> Querying VAPI billing_entries..."

VAPI_QUERY="
SELECT
    org_id AS external_customer_id,
    uniqExact(id) AS unique_events
FROM default.billing_entries
WHERE org_id IN ($IDS_SQL)
  AND created_at >= toDateTime64('$START_DATE', 3)
  AND created_at <= toDateTime64('$END_DATE', 3)
GROUP BY org_id
ORDER BY org_id
FORMAT TabSeparatedWithNames
"

VAPI_RESULT=$(curl -s \
  "https://${VAPI_CH_HOST}:${VAPI_CH_PORT}/?database=${VAPI_CH_DB}" \
  --user "${VAPI_CH_USER}:${VAPI_CH_PASSWORD}" \
  -d "$VAPI_QUERY" 2>&1)

if echo "$VAPI_RESULT" | grep -q "DB::Exception"; then
  echo "VAPI query failed: $VAPI_RESULT"
  exit 1
fi

echo "VAPI results:"
echo "$VAPI_RESULT"
echo ""

# --- Step 3: Compare using awk ---
echo "============================================="
echo "COMPARISON"
echo "============================================="

# Combine both results and compare with awk
# Write to temp files, join, and compare
FP_FILE=$(mktemp)
VAPI_FILE=$(mktemp)
trap "rm -f $FP_FILE $VAPI_FILE" EXIT

echo "$FLEXPRICE_RESULT" | tail -n +2 | sort > "$FP_FILE"
echo "$VAPI_RESULT" | tail -n +2 | sort > "$VAPI_FILE"

printf "%-40s %12s %12s %12s %s\n" "customer_id" "flexprice" "vapi" "diff" "status"
printf "%-40s %12s %12s %12s %s\n" "----------------------------------------" "------------" "------------" "------------" "------"

join -t$'\t' -a1 -a2 -o 0,1.2,2.2 -e 0 "$FP_FILE" "$VAPI_FILE" | while IFS=$'\t' read -r cid fp vapi; do
  diff=$((fp - vapi))
  if [ "$diff" -eq 0 ]; then
    status="OK"
  else
    status="MISMATCH"
  fi
  printf "%-40s %12d %12d %12d %s\n" "$cid" "$fp" "$vapi" "$diff" "$status"
done

echo ""
# Totals
paste "$FP_FILE" "$VAPI_FILE" | awk -F'\t' '
{
  fp += $2; vapi += $4; if ($2 != $4) mm++
}
END {
  printf "%-40s %12d %12d %12d\n", "TOTAL", fp, vapi, fp - vapi
  printf "\n"
  if (mm == 0) print "All customers match!"
  else printf "%d customer(s) have mismatches.\n", mm
}'
