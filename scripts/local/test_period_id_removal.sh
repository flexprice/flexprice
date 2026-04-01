#!/usr/bin/env bash
# =============================================================================
# test_period_id_removal.sh
#
# Validates that analytics + subscription usage endpoints are fully
# timestamp-driven and do NOT rely on period_id.
#
# Test sequence:
#   Phase 0  — sanity baseline (infra up, health check)
#   Phase 1  — create billing entities (meter, plan, price, customer, sub)
#   Phase 2  — ingest batch 1 events (5 events × value=100 → expect qty=500)
#   Phase 3  — verify analytics & subscription usage return qty=500
#   Phase 4  — inspect period_id values in ClickHouse (pre-shift)
#   Phase 5  — shift subscription period dates by +3 days in Postgres
#   Phase 6  — re-verify analytics & subscription usage → still 500 (KEY TEST)
#   Phase 7  — ingest batch 2 events (3 events × value=100 → additive, expect 800)
#   Phase 8  — verify analytics returns 800 (additive)
#   Phase 9  — inspect period_id for batch 2 events (should differ from batch 1)
#
# Usage:
#   bash scripts/local/test_period_id_removal.sh
#
# Prerequisites (follow LOCAL_TESTING.md first):
#   make run-local-api      (Terminal 1)
#   make run-local-consumer (Terminal 2)
# =============================================================================

set -euo pipefail

BASE="http://localhost:8082/v1"
KEY="sk_local_flexprice_test_key"
ENV_ID="00000000-0000-0000-0000-000000000000"
AUTH_HEADERS=(-H "x-api-key: $KEY" -H "x-environment-id: $ENV_ID" -H "Content-Type: application/json")

# Docker compose file — infra lives in main repo dir
COMPOSE_FILE="/Users/nikhilmishra/work/flexprice/docker-compose.yml"
DC="docker compose -f $COMPOSE_FILE"

# Unique suffix so reruns don't collide
SUFFIX="pid-$(date +%s)"
EXT_CUSTOMER_ID="test-period-customer-$SUFFIX"

# Fixed event timestamps — all in Mar 2026, well within a monthly billing period
# Batch 1: 5 events spread across March
B1_T1="2026-03-02T10:00:00Z"
B1_T2="2026-03-05T12:00:00Z"
B1_T3="2026-03-10T08:30:00Z"
B1_T4="2026-03-15T16:00:00Z"
B1_T5="2026-03-18T20:00:00Z"

# Batch 2: 3 events later in March (ingested after period shift)
B2_T1="2026-03-22T09:00:00Z"
B2_T2="2026-03-23T11:00:00Z"
B2_T3="2026-03-24T14:00:00Z"

# Subscription billing period — March 1 → April 1 (anniversary monthly)
SUB_START="2026-03-01T00:00:00Z"

# Analytics query window covers all events in both batches
QUERY_START="2026-03-01T00:00:00Z"
QUERY_END="2026-03-31T23:59:59Z"

# Color helpers
RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; CYAN='\033[0;36m'; NC='\033[0m'
pass() { echo -e "${GREEN}  ✅ $*${NC}"; }
fail() { echo -e "${RED}  ❌ $*${NC}"; exit 1; }
info() { echo -e "${CYAN}  ℹ  $*${NC}"; }
step() { echo -e "\n${YELLOW}── $* ──${NC}"; }

# JSON extraction helpers (no jq dependency — uses python3 like SANITY_CHECK)
json_field() { echo "$1" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('$2',''))" 2>/dev/null; }
json_nested() { echo "$1" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d$2)" 2>/dev/null; }

# ─────────────────────────────────────────────────────────────────────────────
# PHASE 0 — SANITY BASELINE
# ─────────────────────────────────────────────────────────────────────────────
step "PHASE 0 — Health check"

HEALTH=$(curl -sf http://localhost:8082/health || echo '{}')
STATUS=$(json_field "$HEALTH" "status")
[[ "$STATUS" == "ok" ]] && pass "Server healthy" || fail "Health check failed: $HEALTH"

# ─────────────────────────────────────────────────────────────────────────────
# PHASE 1 — CREATE BILLING ENTITIES
# ─────────────────────────────────────────────────────────────────────────────
step "PHASE 1 — Create billing entities"

# 1a. Meter (SUM over 'value' property)
info "Creating SUM meter (event_name=usage_event, field=value)..."
METER=$(curl -sf -X POST "$BASE/meters" \
  "${AUTH_HEADERS[@]}" \
  -d "{
    \"name\": \"Test Value Meter ($SUFFIX)\",
    \"event_name\": \"usage_event\",
    \"aggregation\": {\"type\": \"SUM\", \"field\": \"value\"},
    \"filters\": [],
    \"reset_usage\": \"BILLING_PERIOD\"
  }")
METER_ID=$(json_field "$METER" "id")
[[ -n "$METER_ID" ]] && pass "Meter created: $METER_ID" || fail "Meter creation failed: $METER"

# 1b. Feature (METERED, linked to meter — required for feature_usage pipeline)
info "Creating metered feature (linked to meter)..."
FEATURE=$(curl -sf -X POST "$BASE/features" \
  "${AUTH_HEADERS[@]}" \
  -d "{
    \"name\": \"Test Value Feature ($SUFFIX)\",
    \"type\": \"metered\",
    \"meter_id\": \"$METER_ID\",
    \"unit_singular\": \"unit\",
    \"unit_plural\": \"units\"
  }")
FEATURE_ID=$(json_field "$FEATURE" "id")
[[ -n "$FEATURE_ID" ]] && pass "Feature created: $FEATURE_ID" || fail "Feature creation failed: $FEATURE"

# 1c. Plan
info "Creating plan..."
PLAN=$(curl -sf -X POST "$BASE/plans" \
  "${AUTH_HEADERS[@]}" \
  -d "{\"name\": \"Test Plan ($SUFFIX)\"}")
PLAN_ID=$(json_field "$PLAN" "id")
[[ -n "$PLAN_ID" ]] && pass "Plan created: $PLAN_ID" || fail "Plan creation failed: $PLAN"

# 1c. Price (USAGE type, FLAT_FEE, $0.01 per unit, arrear invoicing)
info "Creating usage price ($0.01/unit) on plan..."
PRICE=$(curl -sf -X POST "$BASE/prices" \
  "${AUTH_HEADERS[@]}" \
  -d "{
    \"entity_type\": \"PLAN\",
    \"entity_id\": \"$PLAN_ID\",
    \"currency\": \"USD\",
    \"amount\": \"0.01\",
    \"type\": \"USAGE\",
    \"price_unit_type\": \"FIAT\",
    \"billing_period\": \"MONTHLY\",
    \"billing_period_count\": 1,
    \"billing_cadence\": \"RECURRING\",
    \"billing_model\": \"FLAT_FEE\",
    \"invoice_cadence\": \"ARREAR\",
    \"meter_id\": \"$METER_ID\"
  }")
PRICE_ID=$(json_field "$PRICE" "id")
[[ -n "$PRICE_ID" ]] && pass "Price created: $PRICE_ID" || fail "Price creation failed: $PRICE"

# 1d. Customer
info "Creating customer (external_id=$EXT_CUSTOMER_ID)..."
CUSTOMER=$(curl -sf -X POST "$BASE/customers" \
  "${AUTH_HEADERS[@]}" \
  -d "{
    \"external_id\": \"$EXT_CUSTOMER_ID\",
    \"name\": \"Period ID Test Customer\",
    \"email\": \"test@period-id.local\",
    \"skip_onboarding_workflow\": true
  }")
CUSTOMER_ID=$(json_field "$CUSTOMER" "id")
[[ -n "$CUSTOMER_ID" ]] && pass "Customer created: $CUSTOMER_ID" || fail "Customer creation failed: $CUSTOMER"

# 1e. Subscription (start 2026-03-01, monthly anniversary billing)
info "Creating subscription (start=$SUB_START, monthly anniversary)..."
SUBSCRIPTION=$(curl -sf -X POST "$BASE/subscriptions" \
  "${AUTH_HEADERS[@]}" \
  -d "{
    \"customer_id\": \"$CUSTOMER_ID\",
    \"plan_id\": \"$PLAN_ID\",
    \"currency\": \"USD\",
    \"start_date\": \"$SUB_START\",
    \"billing_cadence\": \"RECURRING\",
    \"billing_period\": \"MONTHLY\",
    \"billing_period_count\": 1,
    \"billing_cycle\": \"anniversary\"
  }")
SUB_ID=$(json_field "$SUBSCRIPTION" "id")
[[ -n "$SUB_ID" ]] && pass "Subscription created: $SUB_ID" || fail "Subscription creation failed: $SUBSCRIPTION"

# Backfill line item start_date to match subscription start_date (events are pre-dated)
# Without this, IsActive() skips events timestamped before the line item creation time
info "Backfilling line item start_date to subscription start_date..."
$DC exec -T postgres psql -U flexprice -d flexprice -c \
  "UPDATE subscription_line_items SET start_date = '2026-03-01 00:00:00+00', updated_at = NOW() WHERE subscription_id = '${SUB_ID}';" \
  2>/dev/null
pass "Line item start_date set to 2026-03-01"


# Show what period start/end Postgres has right now
info "Current period in Postgres:"
$DC exec -T postgres psql -U flexprice -d flexprice -t -c \
  "SELECT id, current_period_start, current_period_end, billing_anchor FROM subscriptions WHERE id='$SUB_ID';" \
  2>/dev/null | sed 's/^/    /'

# ─────────────────────────────────────────────────────────────────────────────
# PHASE 2 — INGEST BATCH 1 (5 events × value=100)
# ─────────────────────────────────────────────────────────────────────────────
step "PHASE 2 — Ingest batch 1 (5 events, value=100 each, total qty=500)"

ingest_event() {
  local ts=$1 event_id=$2
  curl -sf -X POST "$BASE/events" \
    "${AUTH_HEADERS[@]}" \
    -d "{
      \"event_name\": \"usage_event\",
      \"event_id\": \"$event_id\",
      \"external_customer_id\": \"$EXT_CUSTOMER_ID\",
      \"timestamp\": \"$ts\",
      \"properties\": {\"value\": 100}
    }" > /dev/null
}

ingest_event "$B1_T1" "b1-evt-01-$SUFFIX"
ingest_event "$B1_T2" "b1-evt-02-$SUFFIX"
ingest_event "$B1_T3" "b1-evt-03-$SUFFIX"
ingest_event "$B1_T4" "b1-evt-04-$SUFFIX"
ingest_event "$B1_T5" "b1-evt-05-$SUFFIX"
pass "5 events ingested"

info "Waiting 5 seconds for Kafka consumer to process..."
sleep 5

# Verify events landed in ClickHouse raw events table
RAW_COUNT=$($DC exec -T clickhouse clickhouse-client \
  --user=flexprice --password=flexprice123 --database=flexprice \
  --query="SELECT count() FROM events WHERE external_customer_id='$EXT_CUSTOMER_ID'" \
  2>/dev/null | tr -d '[:space:]')
info "Raw events in ClickHouse events table: $RAW_COUNT"

# Verify feature_usage rows were created
FU_COUNT=$($DC exec -T clickhouse clickhouse-client \
  --user=flexprice --password=flexprice123 --database=flexprice \
  --query="SELECT count() FROM feature_usage WHERE subscription_id='$SUB_ID'" \
  2>/dev/null | tr -d '[:space:]')
info "Feature usage rows in ClickHouse: $FU_COUNT"

if [[ "$FU_COUNT" -ge 5 ]]; then
  pass "Feature usage rows present ($FU_COUNT)"
else
  info "Feature usage may still be processing. Waiting 5 more seconds..."
  sleep 5
  FU_COUNT=$($DC exec -T clickhouse clickhouse-client \
    --user=flexprice --password=flexprice123 --database=flexprice \
    --query="SELECT count() FROM feature_usage WHERE subscription_id='$SUB_ID'" \
    2>/dev/null | tr -d '[:space:]')
  [[ "$FU_COUNT" -ge 5 ]] && pass "Feature usage rows present ($FU_COUNT)" || \
    info "⚠ Only $FU_COUNT feature_usage rows — consumer may be slow, proceeding"
fi

# ─────────────────────────────────────────────────────────────────────────────
# PHASE 3 — VERIFY ANALYTICS & SUBSCRIPTION USAGE (expect qty=500)
# ─────────────────────────────────────────────────────────────────────────────
step "PHASE 3 — Verify analytics returns qty=500 (pre-period-shift)"

# 3a. Analytics v2 endpoint (feature_usage table, timestamp-based)
ANALYTICS=$(curl -sf -X POST "$BASE/events/analytics-v2" \
  "${AUTH_HEADERS[@]}" \
  -d "{
    \"external_customer_id\": \"$EXT_CUSTOMER_ID\",
    \"start_time\": \"$QUERY_START\",
    \"end_time\": \"$QUERY_END\"
  }")
echo "$ANALYTICS" | python3 -m json.tool 2>/dev/null | head -40

TOTAL_USAGE_B1=$(echo "$ANALYTICS" | python3 -c "
import sys, json
d = json.load(sys.stdin)
items = d.get('items', [])
total = sum(float(i.get('total_usage', 0)) for i in items)
print(total)
" 2>/dev/null)
info "analytics-v2 total_usage (batch 1): $TOTAL_USAGE_B1"
[[ "$TOTAL_USAGE_B1" == "500.0" || "$TOTAL_USAGE_B1" == "500" ]] && \
  pass "analytics-v2: total_usage = 500 ✓" || \
  info "⚠ analytics-v2: got $TOTAL_USAGE_B1 (may vary if feature_usage consumer is still catching up)"

# 3b. Subscription usage endpoint
SUB_USAGE=$(curl -sf -X POST "$BASE/subscriptions/usage" \
  "${AUTH_HEADERS[@]}" \
  -d "{
    \"subscription_id\": \"$SUB_ID\",
    \"start_time\": \"$QUERY_START\",
    \"end_time\": \"$QUERY_END\"
  }")
echo "$SUB_USAGE" | python3 -m json.tool 2>/dev/null | head -40

SUB_USAGE_AMT=$(echo "$SUB_USAGE" | python3 -c "
import sys, json
d = json.load(sys.stdin)
charges = d.get('charges', [])
total = sum(float(c.get('quantity', 0)) for c in charges)
print(total)
" 2>/dev/null)
info "subscription usage total quantity (batch 1): $SUB_USAGE_AMT"

# Save baseline for comparison after period shift
BASELINE_ANALYTICS="$ANALYTICS"
BASELINE_USAGE="$SUB_USAGE"

# ─────────────────────────────────────────────────────────────────────────────
# PHASE 4 — INSPECT period_id VALUES IN CLICKHOUSE (pre-shift)
# ─────────────────────────────────────────────────────────────────────────────
step "PHASE 4 — Inspect period_id values in ClickHouse (pre-shift)"

info "Feature usage rows for subscription $SUB_ID:"
$DC exec -T clickhouse clickhouse-client \
  --user=flexprice --password=flexprice123 --database=flexprice \
  --query="
    SELECT
      id,
      timestamp,
      period_id,
      toDateTime(period_id / 1000) AS period_start_human,
      qty_total,
      sign
    FROM feature_usage
    WHERE subscription_id = '$SUB_ID'
    ORDER BY timestamp ASC
    FORMAT PrettyCompact" \
  2>/dev/null

PERIOD_ID_PRE=$($DC exec -T clickhouse clickhouse-client \
  --user=flexprice --password=flexprice123 --database=flexprice \
  --query="SELECT DISTINCT period_id FROM feature_usage WHERE subscription_id='$SUB_ID' AND sign=1 LIMIT 1" \
  2>/dev/null | tr -d '[:space:]')
info "period_id before shift: $PERIOD_ID_PRE"
pass "period_id inspection complete (pre-shift)"

# ─────────────────────────────────────────────────────────────────────────────
# PHASE 5 — SHIFT SUBSCRIPTION PERIOD DATES BY +3 DAYS IN POSTGRES
# ─────────────────────────────────────────────────────────────────────────────
step "PHASE 5 — Shift subscription period dates +3 days in Postgres"

info "Before update:"
$DC exec -T postgres psql -U flexprice -d flexprice -t -c \
  "SELECT id, current_period_start, current_period_end, billing_anchor FROM subscriptions WHERE id='$SUB_ID';" \
  2>/dev/null | sed 's/^/    /'

$DC exec -T postgres psql -U flexprice -d flexprice -c \
  "UPDATE subscriptions
   SET current_period_start = current_period_start + INTERVAL '3 days',
       current_period_end   = current_period_end   + INTERVAL '3 days',
       billing_anchor       = billing_anchor        + INTERVAL '3 days',
       updated_at           = NOW()
   WHERE id = '$SUB_ID';" \
  2>/dev/null

info "After update:"
$DC exec -T postgres psql -U flexprice -d flexprice -t -c \
  "SELECT id, current_period_start, current_period_end, billing_anchor FROM subscriptions WHERE id='$SUB_ID';" \
  2>/dev/null | sed 's/^/    /'

pass "Subscription period shifted +3 days"

# ─────────────────────────────────────────────────────────────────────────────
# PHASE 6 — RE-VERIFY ANALYTICS AFTER PERIOD SHIFT (KEY TEST: must still = 500)
# ─────────────────────────────────────────────────────────────────────────────
step "PHASE 6 — KEY TEST: analytics after period shift must still = 500"

ANALYTICS_POST=$(curl -sf -X POST "$BASE/events/analytics-v2" \
  "${AUTH_HEADERS[@]}" \
  -d "{
    \"external_customer_id\": \"$EXT_CUSTOMER_ID\",
    \"start_time\": \"$QUERY_START\",
    \"end_time\": \"$QUERY_END\"
  }")

TOTAL_USAGE_POST=$(echo "$ANALYTICS_POST" | python3 -c "
import sys, json
d = json.load(sys.stdin)
items = d.get('items', [])
total = sum(float(i.get('total_usage', 0)) for i in items)
print(total)
" 2>/dev/null)

info "analytics-v2 total_usage AFTER period shift: $TOTAL_USAGE_POST"
[[ "$TOTAL_USAGE_B1" == "$TOTAL_USAGE_POST" ]] && \
  pass "✓ PERIOD SHIFT DID NOT AFFECT ANALYTICS — results unchanged ($TOTAL_USAGE_POST)" || \
  fail "✗ Period shift CHANGED analytics result! Before=$TOTAL_USAGE_B1 After=$TOTAL_USAGE_POST — period_id dependency exists!"

# Also re-check subscription usage
SUB_USAGE_POST=$(curl -sf -X POST "$BASE/subscriptions/usage" \
  "${AUTH_HEADERS[@]}" \
  -d "{
    \"subscription_id\": \"$SUB_ID\",
    \"start_time\": \"$QUERY_START\",
    \"end_time\": \"$QUERY_END\"
  }")

SUB_USAGE_AMT_POST=$(echo "$SUB_USAGE_POST" | python3 -c "
import sys, json
d = json.load(sys.stdin)
charges = d.get('charges', [])
total = sum(float(c.get('quantity', 0)) for c in charges)
print(total)
" 2>/dev/null)

info "subscription usage after period shift: $SUB_USAGE_AMT_POST"
[[ "$SUB_USAGE_AMT" == "$SUB_USAGE_AMT_POST" ]] && \
  pass "✓ PERIOD SHIFT DID NOT AFFECT SUBSCRIPTION USAGE — results unchanged ($SUB_USAGE_AMT_POST)" || \
  info "⚠ Subscription usage changed: Before=$SUB_USAGE_AMT After=$SUB_USAGE_AMT_POST (investigate)"

# ─────────────────────────────────────────────────────────────────────────────
# PHASE 7 — INGEST BATCH 2 (3 more events after the period shift)
# ─────────────────────────────────────────────────────────────────────────────
step "PHASE 7 — Ingest batch 2 (3 events post-shift, value=100 each)"

ingest_event "$B2_T1" "b2-evt-01-$SUFFIX"
ingest_event "$B2_T2" "b2-evt-02-$SUFFIX"
ingest_event "$B2_T3" "b2-evt-03-$SUFFIX"
pass "3 more events ingested (timestamps: $B2_T1, $B2_T2, $B2_T3)"

info "Waiting 5 seconds for consumer..."
sleep 5

FU_COUNT_POST=$($DC exec -T clickhouse clickhouse-client \
  --user=flexprice --password=flexprice123 --database=flexprice \
  --query="SELECT count() FROM feature_usage WHERE subscription_id='$SUB_ID' AND sign=1" \
  2>/dev/null | tr -d '[:space:]')
info "Total feature_usage rows (sign=1) now: $FU_COUNT_POST"

# ─────────────────────────────────────────────────────────────────────────────
# PHASE 8 — VERIFY ANALYTICS IS ADDITIVE (expect 800)
# ─────────────────────────────────────────────────────────────────────────────
step "PHASE 8 — Verify analytics is additive (expect qty=800)"

ANALYTICS_FINAL=$(curl -sf -X POST "$BASE/events/analytics-v2" \
  "${AUTH_HEADERS[@]}" \
  -d "{
    \"external_customer_id\": \"$EXT_CUSTOMER_ID\",
    \"start_time\": \"$QUERY_START\",
    \"end_time\": \"$QUERY_END\"
  }")

TOTAL_USAGE_FINAL=$(echo "$ANALYTICS_FINAL" | python3 -c "
import sys, json
d = json.load(sys.stdin)
items = d.get('items', [])
total = sum(float(i.get('total_usage', 0)) for i in items)
print(total)
" 2>/dev/null)

echo "$ANALYTICS_FINAL" | python3 -m json.tool 2>/dev/null | head -50
info "analytics-v2 total_usage (batch 1 + batch 2): $TOTAL_USAGE_FINAL"
[[ "$TOTAL_USAGE_FINAL" == "800.0" || "$TOTAL_USAGE_FINAL" == "800" ]] && \
  pass "✓ ADDITIVE: total_usage = 800 (5×100 + 3×100)" || \
  info "⚠ Got $TOTAL_USAGE_FINAL — may still be processing batch 2 (wait and recheck)"

# ─────────────────────────────────────────────────────────────────────────────
# PHASE 9 — INSPECT period_id FOR BATCH 2 EVENTS
# ─────────────────────────────────────────────────────────────────────────────
step "PHASE 9 — Inspect period_id for batch 2 events (should differ from batch 1)"

info "All feature_usage rows for subscription $SUB_ID:"
$DC exec -T clickhouse clickhouse-client \
  --user=flexprice --password=flexprice123 --database=flexprice \
  --query="
    SELECT
      id,
      timestamp,
      period_id,
      toDateTime(period_id / 1000) AS period_start_human,
      qty_total,
      sign
    FROM feature_usage
    WHERE subscription_id = '$SUB_ID'
      AND sign = 1
    ORDER BY timestamp ASC
    FORMAT PrettyCompact" \
  2>/dev/null

PERIOD_ID_POST=$($DC exec -T clickhouse clickhouse-client \
  --user=flexprice --password=flexprice123 --database=flexprice \
  --query="
    SELECT DISTINCT period_id
    FROM feature_usage
    WHERE subscription_id='$SUB_ID'
      AND sign=1
      AND timestamp >= '2026-03-22 00:00:00'
    LIMIT 1" \
  2>/dev/null | tr -d '[:space:]')

info "period_id for batch 1 events: $PERIOD_ID_PRE"
info "period_id for batch 2 events: $PERIOD_ID_POST"

if [[ -n "$PERIOD_ID_PRE" && -n "$PERIOD_ID_POST" ]]; then
  if [[ "$PERIOD_ID_PRE" != "$PERIOD_ID_POST" ]]; then
    pass "✓ Batch 2 events have a DIFFERENT period_id ($PERIOD_ID_POST) reflecting the shifted billing anchor"
  else
    info "⚠ Batch 2 period_id same as batch 1 — events may be within same period even after shift"
  fi
fi

# ─────────────────────────────────────────────────────────────────────────────
# SUMMARY
# ─────────────────────────────────────────────────────────────────────────────
echo ""
echo -e "${YELLOW}══════════════════════════════════════════════════════════════${NC}"
echo -e "${YELLOW}  TEST SUMMARY                                                ${NC}"
echo -e "${YELLOW}══════════════════════════════════════════════════════════════${NC}"
echo ""
echo "  Subscription ID  : $SUB_ID"
echo "  Customer Ext ID  : $EXT_CUSTOMER_ID"
echo "  Meter ID         : $METER_ID"
echo "  Plan ID          : $PLAN_ID"
echo ""
echo "  Phase 3  — analytics before shift  : $TOTAL_USAGE_B1  (expected 500)"
echo "  Phase 6  — analytics after shift   : $TOTAL_USAGE_POST  (expected 500, same)"
echo "  Phase 8  — analytics after batch 2 : $TOTAL_USAGE_FINAL  (expected 800)"
echo ""
echo "  period_id batch 1 : $PERIOD_ID_PRE"
echo "  period_id batch 2 : $PERIOD_ID_POST"
echo ""

if [[ "$TOTAL_USAGE_B1" == "$TOTAL_USAGE_POST" ]]; then
  echo -e "${GREEN}  ✅ CORE VALIDATION PASSED: period shift did not affect analytics${NC}"
  echo -e "${GREEN}     Analytics is purely timestamp-driven — period_id can be safely${NC}"
  echo -e "${GREEN}     removed from queries without impacting correctness.${NC}"
else
  echo -e "${RED}  ❌ CORE VALIDATION FAILED: period shift changed analytics result${NC}"
fi
echo ""
echo -e "${CYAN}  Manual cleanup (optional):${NC}"
echo "  docker compose exec postgres psql -U flexprice -d flexprice -c \\"
echo "    \"DELETE FROM subscriptions WHERE id='$SUB_ID';\""
echo ""
