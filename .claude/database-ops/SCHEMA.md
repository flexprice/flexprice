# FlexPrice Database Schema Reference

---

## PostgreSQL — Live Schema Introspection

PostgreSQL schema is **authoritative via Ent**. Never maintain a hand-written copy — use this instead:

```bash
# Full schema for all entities
go run entgo.io/ent/cmd/ent describe ./ent/schema

# Single entity (e.g. subscriptions)
go run entgo.io/ent/cmd/ent describe ./ent/schema | grep -A 40 "^Subscription:"
```

**Common table names** (all have `tenant_id`, `environment_id`, `status`, `created_at`, `updated_at`):

| Ent Entity | Postgres Table | Key Columns |
|---|---|---|
| Customer | customers | `external_id`, `name`, `email` |
| Subscription | subscriptions | `customer_id`, `plan_id`, `subscription_status` (active\|paused\|cancelled\|draft\|trialing), `current_period_start/end`, `billing_period`, `billing_cadence` |
| SubscriptionLineItem | subscription_line_items | `subscription_id`, `price_id`, `meter_id`, `quantity`, `currency` |
| Invoice | invoices | `customer_id`, `subscription_id`, `invoice_status` (DRAFT\|FINALIZED\|VOIDED\|SKIPPED), `payment_status`, `total`, `subtotal`, `finalized_at` |
| InvoiceLineItem | invoice_line_items | `invoice_id`, `price_id`, `amount`, `period_start/end` |
| Plan | plans | `lookup_key`, `name` |
| Price | prices | `type` (FIXED\|USAGE), `billing_model` (FLAT_FEE\|PACKAGE\|TIERED), `meter_id`, `entity_type/id` |
| Meter | meters | `event_name`, `aggregation` (JSON: type/field/expression) |
| Feature | features | `lookup_key`, `type` (metered\|entitlement\|static), `meter_id` |
| Entitlement | entitlements | `entity_type/id`, `feature_id`, `usage_limit`, `is_enabled` |
| Wallet | wallets | `customer_id`, `balance`, `credit_balance`, `wallet_status` |
| WalletTransaction | wallet_transactions | balance change records |
| CreditGrant | credit_grants | `scope` (PLAN\|SUBSCRIPTION), `credits`, `cadence` (ONETIME\|RECURRING) |
| Coupon | coupons | `type` (fixed\|percentage), `amount_off`, `percentage_off`, `cadence` |
| CouponApplication | coupon_applications | audit of discounts applied per invoice |
| CreditNote | credit_notes | `invoice_id`, `credit_note_type` (REFUND\|ADJUSTMENT\|CREDIT_MEMO), `total_amount` |
| Addon | addons | `lookup_key`, `type` (single_instance\|multi_instance) |
| SubscriptionPause | subscription_pauses | `pause_status`, `pause_start/end` |

**Quick field reference for common queries:**

```sql
-- Filter published rows (soft delete):       WHERE status = 'published'
-- Active subscriptions:                       WHERE subscription_status = 'active'
-- Finalized invoices in a period:             WHERE invoice_status = 'FINALIZED' AND finalized_at BETWEEN ...
-- Customer by external_id:                    WHERE external_id = '<id>' AND status = 'published'
```

---

## ClickHouse Tables

> **Important:** Do NOT use `FINAL` keyword — very expensive on large ReplacingMergeTree tables. Always filter on `timestamp`. Accept minor duplicate tolerance from background merges.

### events  *(primary events table)*

Engine: `ReplacingMergeTree(ingested_at)` | Partition: `toYYYYMMDD(timestamp)`
Order by: `(tenant_id, environment_id, timestamp, id)`

| Column | Type | Notes |
|---|---|---|
| id | String | UUID |
| tenant_id, environment_id | String | |
| external_customer_id | String | Bloom filter index |
| event_name | String | Set index |
| customer_id | Nullable(String) | resolved from external_id |
| source | Nullable(String) | |
| timestamp | DateTime64(3) | event time — **use for filters** |
| ingested_at | DateTime64(3) | arrival time |
| properties | String | JSON payload |

### raw_events

Engine: `ReplacingMergeTree(version)` | Partition: `toYYYYMMDD(timestamp)`
Order by: `(tenant_id, environment_id, external_customer_id, timestamp, id)`

| Column | Type | Notes |
|---|---|---|
| id, tenant_id, environment_id, external_customer_id, event_name | String | |
| source | Nullable(String) | |
| payload | String CODEC(ZSTD(3)) | full compressed payload |
| field1..field10 | Nullable(String) | flexible schema (field4 = 'false' = non-custom-llm) |
| timestamp, ingested_at | DateTime64(3) | |
| version | UInt64 | |
| sign | Int8 | |

### meter_usage  *(lightweight meter-level events)*

Engine: `ReplacingMergeTree(ingested_at)` | Partition: `toYYYYMMDD(timestamp)`
Order by: `(tenant_id, environment_id, external_customer_id, meter_id, timestamp, id)`

| Column | Type | Notes |
|---|---|---|
| id | String CODEC(ZSTD(1)) | |
| tenant_id, environment_id, external_customer_id, meter_id, event_name | LowCardinality(String) | |
| timestamp | DateTime (no millis) | **use for filters** |
| ingested_at | DateTime64(3) | |
| qty_total | Decimal(18,8) | |
| unique_hash | String | dedup key |
| source | LowCardinality(String) | |
| properties | String CODEC(ZSTD(3)) | |

### feature_usage

Engine: `ReplacingMergeTree(version)` | Partition: `toYYYYMMDD(timestamp)`

| Column | Type | Notes |
|---|---|---|
| id, tenant_id, environment_id, external_customer_id, event_name | String | |
| customer_id, subscription_id, sub_line_item_id, price_id, feature_id | String | |
| meter_id | Nullable(String) | |
| period_id | UInt64 | billing period start (epoch-ms) |
| timestamp, ingested_at, processed_at | DateTime64(3) | |
| qty_total | Decimal(25,15) | |
| unique_hash | Nullable(String) | |
| version | UInt64 | sign | Int8 | |

### events_processed

Engine: `ReplacingMergeTree(version)` | Partition: `toYYYYMM(timestamp)`

Extends events with billing resolution: `subscription_id`, `sub_line_item_id`, `price_id`, `meter_id`, `feature_id`, `period_id`, `qty_total`, `qty_billable`, `qty_free_applied`, `unit_cost`, `cost`, `currency`.

Materialized view `agg_usage_period_totals` pre-aggregates by `(tenant_id, environment_id, customer_id, period_id, feature_id, sub_line_item_id)`.

---

## Common Query Patterns

### PostgreSQL

```sql
-- Active subscriptions count
SELECT count(*) FROM subscriptions
WHERE subscription_status = 'active' AND status = 'published';

-- New subscriptions in UTC window
SELECT count(*) FROM subscriptions
WHERE created_at BETWEEN '<utc_start>' AND '<utc_end>'
  AND status = 'published';

-- Invoice summary for a period
SELECT invoice_status, payment_status, count(*), sum(total)
FROM invoices
WHERE finalized_at BETWEEN '<start>' AND '<end>'
  AND status = 'published'
GROUP BY invoice_status, payment_status;

-- Customer wallet balances
SELECT c.external_id, w.currency, w.balance, w.credit_balance
FROM wallets w JOIN customers c ON c.id = w.customer_id
WHERE w.tenant_id = '<tid>' AND w.status = 'published'
  AND w.wallet_status = 'active';
```

### ClickHouse

```sql
-- Event count by date
SELECT toDate(timestamp) as day, count() as events
FROM events
WHERE tenant_id = '<tid>' AND environment_id = '<eid>'
  AND timestamp BETWEEN toDateTime64('2026-01-01 00:00:00', 3)
                    AND toDateTime64('2026-01-31 23:59:59', 3)
GROUP BY day ORDER BY day;

-- Top customers by event volume (last 7 days)
SELECT external_customer_id, count() as cnt
FROM events
WHERE tenant_id = '<tid>' AND timestamp >= now() - INTERVAL 7 DAY
GROUP BY external_customer_id ORDER BY cnt DESC LIMIT 20;

-- Meter usage for a customer
SELECT meter_id, sum(qty_total) as total_qty,
       min(timestamp) as first_event, max(timestamp) as last_event
FROM meter_usage
WHERE tenant_id = '<tid>' AND external_customer_id = '<ext_cid>'
  AND timestamp >= toDateTime('2026-04-01 00:00:00')
GROUP BY meter_id;

-- Hourly event rate
SELECT toStartOfHour(timestamp) as hour, count() as events
FROM events
WHERE timestamp >= now() - INTERVAL 24 HOUR
GROUP BY hour ORDER BY hour;
```

---

## Environment Variables (`.env`)

```dotenv
FLEXPRICE_POSTGRES_READER_HOST   # Read replica host
FLEXPRICE_POSTGRES_DBNAME        # Database name
FLEXPRICE_POSTGRES_USER          # Username
FLEXPRICE_POSTGRES_PASSWORD      # Password
FLEXPRICE_POSTGRES_PORT          # 5432
FLEXPRICE_POSTGRES_SSLMODE       # require

FLEXPRICE_CLICKHOUSE_ADDRESS     # host:port (e.g. internal-lb.aws.com:9000)
FLEXPRICE_CLICKHOUSE_DATABASE    # flexprice
FLEXPRICE_CLICKHOUSE_USERNAME    # readonly
FLEXPRICE_CLICKHOUSE_PASSWORD    # ...
```

Scripts auto-load `.env` from their own directory. All scripts use read-only credentials.
