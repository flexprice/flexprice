# Local Sanity Check

Run this before every PR to catch regressions early.
Follow [`LOCAL_TESTING.md`](LOCAL_TESTING.md) first to get the server running.

All commands assume:
```bash
BASE="http://localhost:8080/v1"
KEY="sk_local_flexprice_test_key"
ENV="-H 'x-environment-id: 00000000-0000-0000-0000-000000000000'"
AUTH="-H 'x-api-key: sk_local_flexprice_test_key' -H 'x-environment-id: 00000000-0000-0000-0000-000000000000'"
```

---

## 0. Health

```bash
curl -s http://localhost:8080/health
# ✅ {"status":"ok"}
```

---

## 1. Customer

```bash
# Create
CUSTOMER=$(curl -s -X POST $BASE/customers \
  -H "x-api-key: $KEY" -H "x-environment-id: 00000000-0000-0000-0000-000000000000" \
  -H "Content-Type: application/json" \
  -d '{"external_id": "sanity-customer-001", "name": "Sanity Test Customer", "email": "sanity@test.local"}')
echo $CUSTOMER | python3 -m json.tool
CUSTOMER_ID=$(echo $CUSTOMER | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])")
echo "Customer ID: $CUSTOMER_ID"
# ✅ customer created with id, tenant_id, environment_id populated

# Fetch by external ID
curl -s "$BASE/customers/external/sanity-customer-001" \
  -H "x-api-key: $KEY" -H "x-environment-id: 00000000-0000-0000-0000-000000000000" \
  | python3 -m json.tool
# ✅ returns same customer
```

---

## 2. Meter

```bash
METER=$(curl -s -X POST $BASE/meters \
  -H "x-api-key: $KEY" -H "x-environment-id: 00000000-0000-0000-0000-000000000000" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "API Calls",
    "event_name": "api_call",
    "aggregation": {"type": "COUNT"},
    "filters": []
  }')
echo $METER | python3 -m json.tool
METER_ID=$(echo $METER | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])")
echo "Meter ID: $METER_ID"
# ✅ meter created with id
```

---

## 3. Plan + Price

```bash
# Create plan
PLAN=$(curl -s -X POST $BASE/plans \
  -H "x-api-key: $KEY" -H "x-environment-id: 00000000-0000-0000-0000-000000000000" \
  -H "Content-Type: application/json" \
  -d '{"name": "Sanity Plan", "description": "Local sanity test plan"}')
echo $PLAN | python3 -m json.tool
PLAN_ID=$(echo $PLAN | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])")
echo "Plan ID: $PLAN_ID"

# Create price on the plan
PRICE=$(curl -s -X POST $BASE/prices \
  -H "x-api-key: $KEY" -H "x-environment-id: 00000000-0000-0000-0000-000000000000" \
  -H "Content-Type: application/json" \
  -d "{
    \"plan_id\": \"$PLAN_ID\",
    \"currency\": \"USD\",
    \"amount\": \"0.001\",
    \"type\": \"usage\",
    \"billing_period\": \"monthly\",
    \"billing_period_count\": 1,
    \"billing_cadence\": \"recurring\",
    \"billing_model\": \"per_unit\",
    \"meter_id\": \"$METER_ID\"
  }")
echo $PRICE | python3 -m json.tool
PRICE_ID=$(echo $PRICE | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])")
echo "Price ID: $PRICE_ID"
# ✅ price created, linked to plan and meter
```

---

## 4. Subscription

```bash
SUBSCRIPTION=$(curl -s -X POST $BASE/subscriptions \
  -H "x-api-key: $KEY" -H "x-environment-id: 00000000-0000-0000-0000-000000000000" \
  -H "Content-Type: application/json" \
  -d "{
    \"customer_id\": \"$CUSTOMER_ID\",
    \"plan_id\": \"$PLAN_ID\",
    \"currency\": \"USD\",
    \"start_date\": \"$(date -u +%Y-%m-%dT%H:%M:%SZ)\"
  }")
echo $SUBSCRIPTION | python3 -m json.tool
SUB_ID=$(echo $SUBSCRIPTION | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])")
echo "Subscription ID: $SUB_ID"
# ✅ subscription created with status active/draft
```

---

## 5. Event Ingestion (standard path)

```bash
# Single event
curl -s -X POST $BASE/events \
  -H "x-api-key: $KEY" -H "x-environment-id: 00000000-0000-0000-0000-000000000000" \
  -H "Content-Type: application/json" \
  -d "{
    \"event_name\": \"api_call\",
    \"external_customer_id\": \"sanity-customer-001\",
    \"timestamp\": \"$(date -u +%Y-%m-%dT%H:%M:%SZ)\",
    \"properties\": {\"endpoint\": \"/v1/test\"}
  }"
# ✅ {"message":"Event accepted for processing","event_id":"..."}

sleep 2

# Verify event landed in ClickHouse
docker compose exec clickhouse clickhouse-client \
  --user=flexprice --password=flexprice123 --database=flexprice \
  --query="SELECT id, external_customer_id, event_name FROM events WHERE external_customer_id='sanity-customer-001' ORDER BY timestamp DESC LIMIT 3 FORMAT PrettyCompact"
# ✅ row visible
```

---

## 6. Usage Query

```bash
curl -s -X POST "$BASE/events/usage/meter" \
  -H "x-api-key: $KEY" -H "x-environment-id: 00000000-0000-0000-0000-000000000000" \
  -H "Content-Type: application/json" \
  -d "{
    \"meter_id\": \"$METER_ID\",
    \"external_customer_id\": \"sanity-customer-001\",
    \"start_time\": \"$(date -u -v-1d +%Y-%m-%dT%H:%M:%SZ 2>/dev/null || date -u -d '1 day ago' +%Y-%m-%dT%H:%M:%SZ)\",
    \"end_time\": \"$(date -u +%Y-%m-%dT%H:%M:%SZ)\"
  }" | python3 -m json.tool
# ✅ value >= 1 (the event we just ingested)
```

---

## 7. Raw Event Ingestion + Filter (VAPI flow)

```bash
# 7a. No filter — all events forwarded
curl -s -X POST "$BASE/events/raw/bulk" \
  -H "x-api-key: $KEY" -H "x-environment-id: 00000000-0000-0000-0000-000000000000" \
  -H "Content-Type: application/json" \
  -d '{
    "events": [
      {"id":"sanity-raw-001","orgId":"sanity-customer-001","methodName":"synthesizer","providerName":"deepgram","createdAt":"2026-01-01T00:00:00Z","data":{"durationMS":1000},"dataInterface":"DURATION_DATA","referenceType":"call","targetItemId":"ref-001","targetItemType":"call"},
      {"id":"sanity-raw-002","orgId":"blocked-org-999","methodName":"synthesizer","providerName":"deepgram","createdAt":"2026-01-01T00:00:01Z","data":{"durationMS":500},"dataInterface":"DURATION_DATA","referenceType":"call","targetItemId":"ref-002","targetItemType":"call"}
    ]
  }'
# ✅ {"batch_size":2,"message":"Raw events accepted for processing"}

sleep 2
# Consumer log should show: success_count=2, skip_count=0

# 7b. Enable filter — only sanity-customer-001 allowed
curl -s -X PUT "$BASE/settings/event_ingestion_filter" \
  -H "x-api-key: $KEY" -H "x-environment-id: 00000000-0000-0000-0000-000000000000" \
  -H "Content-Type: application/json" \
  -d '{"value": {"enabled": true, "allowed_external_customer_ids": ["sanity-customer-001"]}}'
# ✅ setting saved

# Re-send same batch — should filter blocked-org-999
curl -s -X POST "$BASE/events/raw/bulk" \
  -H "x-api-key: $KEY" -H "x-environment-id: 00000000-0000-0000-0000-000000000000" \
  -H "Content-Type: application/json" \
  -d '{
    "events": [
      {"id":"sanity-raw-003","orgId":"sanity-customer-001","methodName":"synthesizer","providerName":"deepgram","createdAt":"2026-01-01T00:01:00Z","data":{"durationMS":1000},"dataInterface":"DURATION_DATA","referenceType":"call","targetItemId":"ref-003","targetItemType":"call"},
      {"id":"sanity-raw-004","orgId":"blocked-org-999","methodName":"synthesizer","providerName":"deepgram","createdAt":"2026-01-01T00:01:01Z","data":{"durationMS":500},"dataInterface":"DURATION_DATA","referenceType":"call","targetItemId":"ref-004","targetItemType":"call"}
    ]
  }'

sleep 2
# ✅ Consumer log: success_count=1, skip_count=1

# Verify only allowed IDs in ClickHouse
docker compose exec clickhouse clickhouse-client \
  --user=flexprice --password=flexprice123 --database=flexprice \
  --query="SELECT id, external_customer_id FROM events WHERE id IN ('sanity-raw-003','sanity-raw-004') FORMAT PrettyCompact"
# ✅ only sanity-raw-003 appears; sanity-raw-004 (blocked) is absent

# 7c. Disable filter (clean up)
curl -s -X PUT "$BASE/settings/event_ingestion_filter" \
  -H "x-api-key: $KEY" -H "x-environment-id: 00000000-0000-0000-0000-000000000000" \
  -H "Content-Type: application/json" \
  -d '{"value": {"enabled": false, "allowed_external_customer_ids": []}}'
```

---

## 8. Settings Round-trip

```bash
# GET
curl -s "$BASE/settings/event_ingestion_filter" \
  -H "x-api-key: $KEY" -H "x-environment-id: 00000000-0000-0000-0000-000000000000" \
  | python3 -m json.tool
# ✅ enabled: false (from step 7c cleanup)
```

---

## Checklist

Copy this into your PR description or review notes:

```
- [ ] 0. Health check passes
- [ ] 1. Customer create + fetch
- [ ] 2. Meter create
- [ ] 3. Plan + price create
- [ ] 4. Subscription create
- [ ] 5. Event ingested → visible in ClickHouse
- [ ] 6. Usage query returns correct count
- [ ] 7. Raw bulk ingest — no filter (success_count=2)
- [ ] 7. Raw bulk ingest — filter enabled (success_count=1, skip_count=1)
- [ ] 7. ClickHouse confirms only allowed event present
- [ ] 8. Settings GET round-trip correct
```
