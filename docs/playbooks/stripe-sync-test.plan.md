# Stripe Integration – End-to-End Validation Playbook

This playbook lets you exercise the full **FlexPrice ↔ Stripe** data flow ― from initial tenant setup to the hourly cron sync. Follow the sections in order; you can stop once the step you are interested in passes.

---

## Table Legend

| Table                             | Purpose                                                              | Typical access in sync workflow                                       |
| --------------------------------- | -------------------------------------------------------------------- | --------------------------------------------------------------------- |
| `stripe_tenant_configs`           | Stores the **encrypted Stripe API key** and per-tenant sync settings | READ once per workflow run (SyncToStripeActivity)                     |
| `meter_provider_mappings`         | Maps FlexPrice `meter_id` → Stripe **Meter (event_name)**            | READ per aggregation (SyncToStripeActivity)                           |
| `customer_integration_mappings`   | Maps FlexPrice `customer_id` → Stripe **Customer ID**                | READ per aggregation (AggregateEventsActivity & SyncToStripeActivity) |
| `events_processed` _(ClickHouse)_ | All usage that has been priced by FlexPrice                          | READ once per workflow run (AggregateEventsActivity)                  |
| `stripe_sync_batches`             | Tracks every push attempt to Stripe                                  | INSERT / UPDATE after every push (TrackSyncBatchActivity)             |

---

## 0 Prerequisites (one-time)

1. **Local stack up** – `make up` or `docker-compose up -d`.
2. Stripe **Test-mode** account.
3. Public ingress for your API; easiest: `ngrok http 8080` → note `https://<id>.ngrok.io`.
4. Environment flags (export or `.env`):
   ```bash
   STRIPE_INTEGRATION_ENABLED=true
   STRIPE_WEBHOOK_SECRET=<stripe webhook secret>
   STRIPE_WEBHOOK_PUBLIC=https://<id>.ngrok.io/webhooks/stripe/{tenant_id}/{environment_id}
   ```
5. At least **one Tenant** & **one Environment** created – keep the UUIDs (`tenant_id`, `environment_id`).
6. Temporal worker running: `./local-worker.sh`.

---

## 1 Save the tenant's Stripe config

1. **PUT /v1/stripe/config** _(auth: tenant-admin JWT)_
   ```http
   PUT /v1/stripe/config
   {
     "api_key": "sk_test_51…",           // Stripe secret key
     "sync_enabled": true,
     "aggregation_window_minutes": 60
   }
   ```
   ✨ **Effect**
   - Inserts / updates a row in **`stripe_tenant_configs`** with the encrypted key.
   - Field `updated_at` is bumped on every change.
2. **Smoke-test the key**
   ```http
   POST /v1/stripe/config/test  → 200 { "status": "connected" }
   ```
   - Endpoint decrypts the key and performs `GET /v1/balance` against Stripe.

> Repeat step 1 for every tenant+environment you want to test; verify N rows exist in `stripe_tenant_configs`.

---

## 2 Create Meter → Stripe mapping

FlexPrice must know which **Stripe Meter (event_name)** corresponds to a FlexPrice `meter_id`.

1. **POST /v1/stripe/meter-mappings** _(fictional endpoint – replace with the real one when implemented)_

   ```http
   POST /v1/stripe/meters/mapping
   {
     "meter_id":            "meter_storage",
     "provider_type":       "stripe",
     "provider_meter_id":   "meter.storage",   // Stripe event_name or meter id
     "sync_enabled":        true
   }
   ```

   ✨ **Effect**

   - Row inserted into **`meter_provider_mappings`**.
   - Unique index (`tenant_id`, `environment_id`, `meter_id`) enforces 1-to-1 mapping. Duplicate → 409.

If the public API is not available yet you can **seed directly**:

```sql
INSERT INTO meter_provider_mappings
  (meter_id, provider_type, provider_meter_id, tenant_id, environment_id, sync_enabled)
VALUES ('meter_storage','stripe','meter.storage','<tenant>','<env>',true);
```

---

## 3 (Optionally) Customer mapping

Most tenants will rely on the **`customer.created`** webhook (next section) to populate `customer_integration_mappings`. For manual testing you can pre-seed:

```sql
INSERT INTO customer_integration_mappings
  (customer_id, provider_type, provider_customer_id, tenant_id, environment_id)
VALUES ('client_123','stripe','cus_test123','<tenant>','<env>');
```

---

## 4 Webhook endpoint sanity check (customer.created)

1. In Stripe **Developers → Webhooks** create an endpoint:
   - URL `https://<id>.ngrok.io/webhooks/stripe/<tenant_id>/<environment_id>`
   - Events: `customer.created`
2. Click **Send test webhook**.
3. Expect **HTTP 200**; server logs show handler ran; DB row appears in `customer_integration_mappings`.

---

## 5 Insert mock usage into ClickHouse

```sql
INSERT INTO events_processed
  (customer_id, meter_id, event_type, qty_billable, processed_at, tenant_id, environment_id)
VALUES ('client_123', 'meter_storage', 'increment', 42,
        now() - interval 10 minute, '<tenant>', '<env>');
```

---

## 6 Trigger a **manual** sync

```bash
curl -X POST http://localhost:8080/v1/stripe/sync/manual \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer <tenant-admin JWT>" \
  -d '{
        "entity_id":   "client_123",              // optional – limits to one customer
        "entity_type": "customer",
        "time_from":   "2025-06-21T04:00:00Z",
        "time_to":     "2025-06-22T04:00:00Z",
        "force_rerun": false
      }'
```

Response contains Temporal **workflow_id** & **run_id**.

### 6.1 Internal workflow execution order

| Step | Temporal activity         | Source / target tables                                                                   | Notes                                                                                                                                                         |
| ---- | ------------------------- | ---------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 1    | `AggregateEventsActivity` | READ `events_processed`                                                                  | Adds `AND customer_id = ?` if `entity_id` given. Groups by `(customer_id,meter_id,event_type)` and returns aggregated rows.                                   |
| 2    | `SyncToStripeActivity`    | READ `stripe_tenant_configs`, `customer_integration_mappings`, `meter_provider_mappings` | Builds **idempotency key** (`flexprice_<sha256>`), converts each aggregation into a Stripe _meter event_ call.                                                |
| 2a   | _(external)_              | —> Stripe API `/v1/billing/meter_events`                                                 | One request per aggregation. On success returns `id` (event) ✔️.                                                                                              |
| 3    | `TrackSyncBatchActivity`  | INSERT into `stripe_sync_batches`                                                        | Columns set: `aggregated_quantity`, `event_count`, `stripe_event_id`, `sync_status`, `window_start`, `window_end`, etc. Duplicate key → ignored (idempotent). |

### 6.2 Verify outcome

```sql
SELECT sync_status, stripe_event_id, error_message
FROM stripe_sync_batches
WHERE tenant_id='<tenant>'
  AND window_start = '2025-06-21 04:00:00';
```

Expect `sync_status = completed` and a non-null `stripe_event_id`.

---

## 7 Hourly **cron** sync

The worker registers `StripeEventSyncWorkflow` with schedule **"hourly + 5-min grace"** (default). To observe without waiting:

```bash
# Fire a backfill run covering the last hour
temporal workflow start \
  --task-queue stripe-sync \
  --workflow StripeEventSyncWorkflow \
  --workflow-id test-cron-$RANDOM \
  --cron "0 * * * *"          # fires on the next minute 0
```

- The same activity chain (steps 1–3 above) executes automatically.
- Query `stripe_sync_batches` after the cron fires; you should see rows for the hourly window.

> **Tip**: set env `STRIPE_SYNC_GRACE_PERIOD_MINUTES=1` and restart the worker to shorten test cycles.

---

## 8 Negative-path checks

- Wrong API key → `POST /stripe/config/test` returns 401, `stripe_sync_batches.error_message` starts with "auth error".
- Missing meter mapping → `SyncToStripeActivity` marks batch **failed** with `error_message = "meter mapping not found"`.
- Duplicate idempotency key → 409 from Stripe; activity retries then inserts batch with `sync_status = failed`.

---

## 9 Metrics & health

```
GET /v1/stripe/sync/status   → {
  "success_rate": 0.97,
  "open_circuit_breakers": 0,
  "average_latency_ms": 380
}
```

Prometheus metrics exposed under `/metrics` include `stripe_sync_batches_total{status="failed"}`.

---

## 10 Clean-up

```bash
# Danger zone – wipe all test data
TRUNCATE TABLE stripe_sync_batches;
TRUNCATE TABLE customer_integration_mappings;
TRUNCATE TABLE meter_provider_mappings;
DELETE FROM stripe_tenant_configs WHERE tenant_id='<tenant>';  -- optional
```

---

Running through this playbook validates **configuration → mapping → manual sync → hourly cron sync**, plus error paths, exactly covering the Stripe Integration workflow and all supporting tables.
