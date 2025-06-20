# Stripe Integration – Manual Test Plan

Use this checklist to validate the entire Stripe ↔ FlexPrice flow by hand.  
It is ordered, so you can run top-to-bottom and stop whenever something fails.

---

## 0 Prerequisites (once)

- FlexPrice stack running locally (`make up` or docker-compose).
- Stripe account in **Test mode**.
- Expose your API port with ngrok  
  `ngrok http 8080  →  https://abc123.ngrok.io`.
- ENV variables
  ```bash
  STRIPE_INTEGRATION_ENABLED=true
  STRIPE_WEBHOOK_SECRET=<from Stripe>
  STRIPE_WEBHOOK_PUBLIC=https://abc123.ngrok.io/webhooks/stripe/{tenant_id}/{environment_id}
  ```
- Two tenants (+ environments) created; keep the UUIDs.

---

## 1 Stripe connection check (per tenant + env)

1. Save Stripe keys
   ```http
   PUT /v1/stripe/config         (Bearer tenant-admin JWT)
   {
     "api_key": "<sk_test_…>",
     "sync_enabled": true,
     "aggregation_window_minutes": 60
   }
   ```
2. Test connection
   ```http
   POST /v1/stripe/config/test   → 200 { "status": "connected" }
   ```

---

## 2 On-board multiple tenants

Repeat **Step 1** for Tenant-A & Tenant-B.  
Verify two rows exist in `stripe_tenant_configs`.

---

## 3 Bulk customer import from Stripe

1. Stripe → **Customers → Export CSV**.
2. Add `external_id` column; fill with your internal IDs.
3. Upload:

   ```bash
   curl -X POST https://abc123.ngrok.io/v1/stripe/migrate/upload \
        -H "Authorization: Bearer <tenant-admin JWT>" \
        -F file=@import.csv \
        -F dry_run=false \
        -F skip_existing=true \
        -F batch_size=200
   ```

   → 202 Accepted with `migration_id`.

4. Poll status  
   `GET /v1/stripe/migrate/{migration_id}` until `status=completed`.
5. DB table `customer_integration_mappings` contains the new rows.

---

## 4 Webhook test (customer.created)

1. Stripe → **Developers → Webhooks → Add endpoint**  
   URL `https://abc123.ngrok.io/webhooks/stripe/<tenant-uuid>/<environment-uuid>`  
   Events `customer.created`.
2. Click **Send test webhook** (no special metadata required, you can still include `external_id`).
3. Expect: HTTP 200, log shows mapping created, DB row added.

---

## 5 Event aggregation & sync

1. Insert sample processed events:

   ```sql
   INSERT INTO events_processed
   (customer_id, meter_id, event_type, qty_billable, processed_at,
    tenant_id, environment_id)
   VALUES ('client_123','meter_storage','increment',42,
           now()-interval 10 minute,'<tenantA>','<env>');
   ```

2. Trigger workflow manually
   ```bash
   temporal workflow start \
     --task-queue stripe-sync \
     --workflow StripeEventSyncWorkflow \
     --workflow-id test-sync-$(uuidgen)
   ```
3. Logs: batch sent, Stripe 200, DB `stripe_sync_batches` status = success.

---

## 6 Negative-path smoke tests

- Wrong API key → `POST /stripe/config/test` fails, metrics show rate-limit.
- Malformed webhook signature → 400.
- CSV with duplicate `external_id` → validation error.

---

## 7 Metrics & health endpoint

`GET /stripe/sync/status`  
Returns success-rate, error-rate, circuit-breaker states, etc.

---

## 8 Rate-limit & retry (simulate 429)

Point `api.stripe.com` to a stub that always returns 429, run sync; check
back-offs (1 s → 32 s) and batch marked **failed** after 5 attempts.

---

## 9 Circuit-breaker

Leave stub returning 500. Invoke sync 6×.  
Breaker opens (`status=open` in `/sync/status`), closes after timeout.

---

## 10 Migration lifecycle

- Start large CSV import (50 k rows).
- `POST /stripe/migrate/{id}/pause` → status `paused`.
- `POST /stripe/migrate/{id}/resume` → processing continues.
- After completion `POST /stripe/migrate/{id}/rollback`
  → rollback migration created, mappings removed/flagged.

---

## 11 Webhook replay / idempotency

Trigger the same `customer.created` twice.  
Second call ignored; `webhook_failed` metric not incremented.

---

## 12 Security checks

- Bad signature → 400.
- Timestamp older than 5 min → 409 replay.
- Call migration API with wrong-tenant JWT → 403.

---

## 13 Performance sanity

Insert 10 k events, run workflow; ensure hour-sync < 15 min.

---

## 14 Clean-up

- Stop ngrok.
- Delete test API keys / webhooks in Stripe.
- Truncate test data in Postgres / ClickHouse if desired.

---

_Running through this script exercises connection, onboarding, import,
webhooks, aggregation, retry logic, circuit breaker, monitoring,
security, and rollback – fully covering Tasks 9 & 10 of the PRD._
