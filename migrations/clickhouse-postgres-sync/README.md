# ClickHouse <-> PostgreSQL Sync

Syncs dimension tables (prices, subscriptions, subscription_line_items) from PostgreSQL to ClickHouse
so analytics queries can JOIN them with `feature_usage` / `events_processed` without round-tripping to PG.

## Tables

| Table | PG Rows (approx) | Join key in feature_usage | CH RMT version col |
|-------|-------------------|--------------------------|---------------------|
| prices | ~thousands | `price_id` | `version` |
| subscriptions | ~thousands | `subscription_id` | `_version` (PG `version` is a business field) |
| subscription_line_items | ~120M+ | `sub_line_item_id` | `version` |

## Upgrading existing ClickHouse (trial column rename)

PostgreSQL uses `trial_period_days` on `prices` and `subscription_line_items`. If your ClickHouse tables still have `trial_period`, rename before running updated sync SQL:

```sql
ALTER TABLE flexprice.prices RENAME COLUMN trial_period TO trial_period_days;
ALTER TABLE flexprice.subscription_line_items RENAME COLUMN trial_period TO trial_period_days;
```

New deployments should apply `001_schema_*.sql` / `003_schema_*.sql` as-is.

## Setup

1. Create tables in ClickHouse:
   ```bash
   clickhouse-client --multiquery < 001_schema_prices.sql
   clickhouse-client --multiquery < 002_schema_subscriptions.sql
   clickhouse-client --multiquery < 003_schema_subscription_line_items.sql
   ```

2. Set environment variables:
   ```bash
   export FLEXPRICE_POSTGRES_HOST=localhost
   export FLEXPRICE_POSTGRES_PORT=5432
   export FLEXPRICE_POSTGRES_DBNAME=flexprice
   export FLEXPRICE_POSTGRES_USER=flexprice
   export FLEXPRICE_POSTGRES_PASSWORD=flexprice
   ```

3. Run sync:
   ```bash
   # Full sync (first time)
   ./sync.sh

   # Incremental sync (only rows updated after a date)
   ./sync.sh --after "2026-01-01 00:00:00"

   # Sync a specific table only
   ./sync.sh --table prices
   ./sync.sh --table subscription_line_items --after "2026-03-01 00:00:00"

   # Dry run (prints commands without executing)
   ./sync.sh --dry-run
   ```

## Scalability

- **prices & subscriptions**: Small tables, synced in a single `INSERT...SELECT FROM postgresql()` pass
- **subscription_line_items (~120M+ rows)**: Batched by monthly `updated_at` windows to avoid OOM on both PG and CH sides. Each batch is `WHERE updated_at >= X AND updated_at < Y` with ~1 month intervals.

## How it works

- Uses ClickHouse `postgresql()` table function to read directly from PG
- `ReplacingMergeTree(version)` handles upserts: re-syncing the same row is safe, newer version wins
- Re-running sync is idempotent — no need to TRUNCATE before re-sync
- Use `SELECT ... FROM table FINAL` for reads, or `OPTIMIZE TABLE table FINAL` after sync

## Manual sync (without sync.sh)

If the `postgresql()` table function isn't available or you prefer direct queries:

```sql
-- Example: sync prices directly
INSERT INTO flexprice.prices (...)
SELECT ...
FROM postgresql('PG_HOST:5432', 'DB_NAME', 'prices', 'USER', 'PASS');
```

See `sync_prices.sql`, `sync_subscriptions.sql`, `sync_subscription_line_items.sql` for the full column lists.
