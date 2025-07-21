# Stripe Sync Demo Video Script

*Reference: [Stripe Sync Implementation Report](./implementation-report-stripe-sync.md)*

## Video Introduction

**Duration: ~15-20 minutes**  
**Style: Informal walkthrough with code demos**

---

## Opening (30 seconds)

Hey everyone! Today I'm going to walk you through our new Stripe synchronization feature that we built for FlexPrice. This is pretty exciting stuff - we can now sync usage-based billing data between FlexPrice and Stripe in real-time.

We built this entire integration in about 5 days, and it's designed to handle 25 million events per month. Let me show you how it all works end-to-end.

---

## Demo Overview (1 minute)

So here's what we're going to cover today:

1. **Setting up the webhook** in Stripe to listen for customer events
2. **Configuring FlexPrice** to store our Stripe API credentials
3. **Creating a customer** in Stripe and watching it sync to FlexPrice
4. **Manually mapping** subscriptions and meters (for now)
5. **Sending usage events** through FlexPrice
6. **Running the sync job** to push everything to Stripe

The cool thing is, this whole process is designed to be bi-directional and handle massive scale. Alright, let's dive in!

---

## Step 1: Stripe Webhook Setup (2 minutes)

First things first - we need to set up our webhook in Stripe. 

*[Screen: Stripe Dashboard]*

I'm going to jump into the Stripe dashboard here. We're using test mode for this demo, obviously.

Going to **Developers → Webhooks** and clicking **Add endpoint**.

The webhook URL follows this pattern:
```
https://your-domain.com/webhooks/stripe/{tenant_id}/{environment_id}
```

For our demo, I'll use:
```
https://demo.flexprice.com/webhooks/stripe/tenant_123/env_prod
```

*[Point to screen]* Notice how we embed the tenant and environment IDs right in the URL path? This is how we handle multi-tenancy - each tenant gets their own webhook endpoint.

For events, I'm selecting **customer.created** - that's the main one we care about for this demo.

*[Show webhook secret]* Stripe gives us a webhook signing secret - we'll need this for validation. Copy that...

---

## Step 2: Configure FlexPrice with Stripe Credentials (3 minutes)

Now let's configure FlexPrice to talk to Stripe. 

*[Screen: Terminal/API client]*

We have a dedicated endpoint for this. Looking at our implementation, it's:

```http
PUT /v1/stripe/config
```

Let me craft this request:

```bash
curl -X PUT http://localhost:8080/v1/stripe/config \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer YOUR_TENANT_JWT" \
  -d '{
    "api_key": "sk_test_51...",
    "sync_enabled": true,
    "aggregation_window_minutes": 60
  }'
```

*[Explain while typing]*
- **api_key**: This is our Stripe secret key - it gets encrypted at rest
- **sync_enabled**: Turns on/off sync for this tenant
- **aggregation_window_minutes**: How often we batch events (60 minutes by default)

*[Send request]*

Great! Let's verify the connection works:

```bash
curl -X POST http://localhost:8080/v1/stripe/config/test \
  -H "Authorization: Bearer YOUR_TENANT_JWT"
```

*[Show response]* Perfect! We get back `{"status": "connected"}` which means our API key is valid.

Behind the scenes, this endpoint is doing a `GET /v1/balance` call to Stripe to verify the credentials. Pretty neat!

---

## Step 3: Create Customer in Stripe (2 minutes)

Now for the fun part - let's create a customer in Stripe and watch it automatically sync to FlexPrice.

*[Screen: Terminal with Stripe CLI]*

I'm using the Stripe CLI for this because it's quick, but you could also use the dashboard.

```bash
stripe customers create \
  --email="demo@example.com" \
  --name="Demo Customer" \
  --metadata[external_id]="flexprice_customer_123"
```

*[Explain metadata]* That `external_id` in metadata is important - it tells our webhook handler which FlexPrice customer this maps to.

*[Show response]* Great! We get back a customer with ID `cus_xxxxx`.

Now, because we set up the webhook, Stripe should automatically send a `customer.created` event to our FlexPrice instance.

*[Screen: FlexPrice logs]*

Let's check our logs... *[point to logs]* Awesome! I can see the webhook was received and processed. 

Looking at our database:

```sql
SELECT * FROM entity_integration_mappings 
WHERE provider_type = 'stripe' 
AND entity_type = 'customer';
```

*[Show result]* Perfect! The mapping was created automatically. FlexPrice customer `flexprice_customer_123` is now mapped to Stripe customer `cus_xxxxx`.

---

## Step 4: Manual Subscription and Meter Mapping (3 minutes)

For this demo, we're doing subscription and meter mapping manually, but this could be automated in the future.

*[Screen: API client]*

First, let's create a meter mapping. This tells FlexPrice which of our meters corresponds to which Stripe meter:

```bash
curl -X POST http://localhost:8080/v1/stripe/meters/mapping \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer YOUR_TENANT_JWT" \
  -d '{
    "meter_id": "api_calls",
    "provider_type": "stripe", 
    "provider_meter_id": "meter.api_calls",
    "sync_enabled": true
  }'
```

*[Explain]* So now when we see usage for our `api_calls` meter in FlexPrice, we know to sync it to Stripe's `meter.api_calls`.

*[Show response]* Great, mapping created!

For subscriptions, in a real implementation you'd probably create these through your normal FlexPrice subscription endpoints, but the key thing is making sure the customer mapping exists first.

---

## Step 5: Generate Usage Events (3 minutes)

Now let's generate some usage events in FlexPrice and see them flow through the system.

*[Screen: API client]*

I'll send a few API usage events:

```bash
# Event 1
curl -X POST http://localhost:8080/v1/events \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer YOUR_TENANT_JWT" \
  -d '{
    "customer_id": "flexprice_customer_123",
    "event_name": "api_call",
    "timestamp": "2025-01-08T10:00:00Z",
    "properties": {
      "endpoint": "/users",
      "method": "GET"
    }
  }'

# Event 2  
curl -X POST http://localhost:8080/v1/events \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer YOUR_TENANT_JWT" \
  -d '{
    "customer_id": "flexprice_customer_123", 
    "event_name": "api_call",
    "timestamp": "2025-01-08T10:05:00Z",
    "properties": {
      "endpoint": "/orders",
      "method": "POST"
    }
  }'
```

*[Show responses]* Perfect! Events are being ingested.

Now, FlexPrice processes these events and stores them in ClickHouse with billing information. Let's check:

*[Screen: ClickHouse query]*

```sql
SELECT 
  customer_id,
  meter_id, 
  qty_billable,
  timestamp
FROM events_processed 
WHERE customer_id = 'flexprice_customer_123'
ORDER BY timestamp DESC;
```

*[Show results]* Excellent! We can see our events have been processed and have `qty_billable` values calculated based on our pricing rules.

---

## Step 6: Manual Sync to Stripe (4 minutes)

Now comes the exciting part - let's manually trigger the sync job to push these events to Stripe.

*[Screen: API client]*

In production, this runs automatically every hour via a Temporal cron job, but for the demo, we'll trigger it manually:

```bash
curl -X POST http://localhost:8080/v1/stripe/sync/manual \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer YOUR_TENANT_JWT" \
  -d '{
    "entity_id": "flexprice_customer_123",
    "entity_type": "customer", 
    "time_from": "2025-01-08T09:00:00Z",
    "time_to": "2025-01-08T11:00:00Z",
    "force_rerun": false
  }'
```

*[Show response]* Great! We get back a workflow ID and run ID from Temporal.

*[Explain the workflow]* Behind the scenes, this kicks off a three-step Temporal workflow:

1. **AggregateEventsActivity**: Queries ClickHouse to aggregate our events by customer and meter
2. **SyncToStripeActivity**: Sends the aggregated data to Stripe's meter events API  
3. **TrackSyncBatchActivity**: Records the sync attempt in our database

Let's check the status:

```bash
curl -X GET http://localhost:8080/v1/stripe/sync/status \
  -H "Authorization: Bearer YOUR_TENANT_JWT"
```

*[Show status response]* Perfect! We can see the sync completed successfully.

Let's verify in our database:

```sql
SELECT 
  entity_id,
  meter_id,
  aggregated_quantity,
  event_count,
  sync_status,
  stripe_event_id
FROM stripe_sync_batches 
WHERE entity_id = 'flexprice_customer_123'
ORDER BY created_at DESC;
```

*[Show results]* Awesome! We can see:
- 2 events were aggregated 
- Sync status is "completed"
- We have a Stripe event ID confirming it was received

---

## Step 7: Verify in Stripe (2 minutes)

Let's hop over to Stripe to confirm the data made it through.

*[Screen: Stripe Dashboard]*

Going to **Billing → Meter events** in the Stripe dashboard...

*[Show meter events]* Perfect! Here we can see our aggregated usage data:
- Customer: `cus_xxxxx` (our demo customer)
- Meter: `meter.api_calls` 
- Quantity: 2 (our aggregated events)
- Timestamp: matches our sync window

The cool thing is, each sync batch gets an idempotency key based on the customer, meter, and time window, so if we run the sync again, it won't create duplicate data.

---

## Technical Deep Dive (2 minutes)

Let me quickly show you some of the cool technical stuff happening under the hood.

*[Screen: Code/Architecture diagram]*

This whole system is built with clean architecture principles:

- **Domain Layer**: Handles business logic and validation
- **Repository Layer**: Uses Facebook's Ent framework for type-safe database operations  
- **Service Layer**: Orchestrates business workflows with encrypted API key management
- **API Layer**: REST endpoints with comprehensive validation

The ClickHouse aggregation query is particularly interesting:

```sql
SELECT 
    ep.customer_id,
    ep.meter_id,
    COALESCE(cim.provider_entity_id, '') AS provider_customer_id,
    sum(ep.qty_billable * ep.sign) AS aggregated_quantity,
    count(DISTINCT ep.id) AS event_count
FROM events_processed FINAL ep
LEFT JOIN entity_integration_mappings cim ON (...)  
WHERE ep.qty_billable > 0 AND ep.sign != 0
GROUP BY ep.customer_id, ep.meter_id, cim.provider_entity_id
```

*[Explain]* The `FINAL` modifier ensures consistency, and we join with our integration mappings to resolve Stripe customer IDs on the fly.

We also have comprehensive error handling with circuit breakers, retry strategies, and monitoring metrics throughout the entire pipeline.

---

## Wrap Up (1 minute)

So there you have it! We just walked through the complete FlexPrice to Stripe sync workflow:

✅ **Webhook setup** for real-time customer sync  
✅ **Configuration management** with encrypted API keys  
✅ **Automatic customer mapping** via webhooks  
✅ **Usage event processing** through ClickHouse  
✅ **Batch synchronization** to Stripe via Temporal workflows  

This system is designed to handle 25 million events per month with enterprise-grade reliability, security, and monitoring.

In the future, we'll be adding support for multiple providers, real-time sync capabilities, and automated subscription management.

Pretty cool stuff! Let me know if you have any questions about the implementation. The full technical details are in our [implementation report](./implementation-report-stripe-sync.md).

Thanks for watching!

---

## Demo Checklist

### Pre-Demo Setup
- [ ] FlexPrice instance running locally
- [ ] Temporal worker running (`./local-worker.sh`)
- [ ] Stripe CLI installed and configured  
- [ ] Test Stripe account with webhook endpoint
- [ ] API client (Postman/curl) ready
- [ ] Database access for verification queries

### Environment Variables
```bash
STRIPE_INTEGRATION_ENABLED=true
STRIPE_WEBHOOK_SECRET=whsec_xxxxx
STRIPE_SYNC_GRACE_PERIOD_MINUTES=5
```

### Test Data
- Tenant ID: `tenant_123`
- Environment ID: `env_prod`  
- Customer ID: `flexprice_customer_123`
- Meter ID: `api_calls`

### Key Files to Reference
- `/internal/api/v1/stripe_config.go` - Configuration endpoints
- `/internal/api/v1/stripe_sync.go` - Manual sync endpoints  
- `/internal/webhook/handler/stripe_handler.go` - Webhook processing
- `/internal/temporal/workflows/stripe_sync_workflow.go` - Sync workflow
- `/docs/playbooks/stripe-sync-test.plan.md` - Detailed test procedures

### Backup Commands
```bash
# Check webhook status
curl -X GET http://localhost:8080/v1/stripe/sync/batches

# Retry failed batches  
curl -X POST http://localhost:8080/v1/stripe/batches/retry

# View sync metrics
curl -X GET http://localhost:8080/v1/stripe/sync/status
```