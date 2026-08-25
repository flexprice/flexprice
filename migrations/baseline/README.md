# Schema baseline

A from-empty snapshot of the full Flexprice schema — Postgres and ClickHouse — for
standing up a brand new database. Run this once on a fresh database instead of
replaying the incremental migration history.

| File | What it builds |
|---|---|
| `postgres_baseline_20260819.sql` | 56 tables, 196 indexes, 1 sequence, 3 functions |
| `clickhouse_baseline_20260819.sql` | 11 tables (8 ReplacingMergeTree, 3 MergeTree) |
| `clickhouse_baseline_replicated_20260819.sql` | `events` + `meter_usage` only, Replicated\* engines |
| `apply.sh` | Applies either or both, with a guard against non-empty targets |

## Usage

```bash
set -a && source .env.staging && set +a     # or any env file / exported vars
./migrations/baseline/apply.sh --all
```

`--postgres` and `--clickhouse` run one side only; `--clickhouse-replicated`
substitutes the replicated ClickHouse baseline; `--dry-run` prints what would
happen without touching anything. Connection details come from the same
`FLEXPRICE_POSTGRES_*` / `FLEXPRICE_CLICKHOUSE_*` variables the application uses.

One extra variable: `CH_HTTP_PORT` (default `8123`). `FLEXPRICE_CLICKHOUSE_ADDRESS`
carries the native protocol port `9000`, which the script ignores — it applies DDL
over the HTTP interface.

If `FLEXPRICE_CLICKHOUSE_DATABASE` is anything other than `flexprice`, the script
rewrites the qualified object names before applying.

## Applying by hand

```bash
psql "$CONN" -v ON_ERROR_STOP=1 -f postgres_baseline_20260819.sql
```

ClickHouse rejects multi-statement request bodies, so the CH file cannot be piped
in one shot — `apply.sh` splits it and sends each statement individually.

## Replicated ClickHouse

`clickhouse_baseline_replicated_20260819.sql` covers the two hot-path tables —
`events` and `meter_usage` — with `ReplicatedReplacingMergeTree` engines and
`ON CLUSTER`, for a multi-replica cluster backed by Keeper. Columns, skip indexes,
the `proj_by_customer_event` projection, partitioning, ordering, and per-table
settings are identical to the single-node file; only the engine line and the
`ON CLUSTER` clause differ.

Every node must define three macros — `{cluster}`, `{shard}`, `{replica}` — with
`{replica}` unique within a shard. The Altinity operator sets these; a hand-rolled
cluster needs them in `config.d/macros.xml`. Check before applying:

```sql
SELECT * FROM system.macros;
SELECT cluster, shard_num, replica_num, host_name FROM system.clusters;
```

Applying per-node instead of through the DDL queue works too — strip the
`ON CLUSTER` clauses and run the file once against each replica. Replicas are
joined by their Keeper path, not by `ON CLUSTER`.

Verified against a real two-replica cluster (ClickHouse 26.3 + embedded Keeper):
`ON CLUSTER` propagated both tables to both nodes, `system.replicas` reported
`total_replicas = 2 / active_replicas = 2`, rows inserted on replica 1 appeared on
replica 2 after `SYSTEM SYNC REPLICA`, and the projection and all three skip
indexes survived on the replica.

Operational note: dropping a replicated table leaves its Keeper path behind for
`database_atomic_delay_before_drop_table_sec` (default 480s). Recreating the same
table inside that window fails with "Replica already exists" — clear it with
`SYSTEM DROP REPLICA` rather than waiting, if you need the path back sooner.

## Idempotency

- **ClickHouse** is safe to re-run, single-node and replicated alike: every
  statement is `CREATE ... IF NOT EXISTS`.
- **Postgres is not.** `pg_dump` output has no `IF NOT EXISTS`, so a second run
  fails on the first `CREATE TABLE`. `apply.sh` checks the target first and aborts
  if `public` already has tables, rather than half-applying over an existing schema.

## Provenance

Dumped 2026-08-19 from AWS India production:

- Postgres — Aurora `fp-prod-rds-v1`, database `prod_flexprice`, PG 16.6
- ClickHouse — cluster behind the `ap-south-1` internal ELB, database `flexprice`, v26.3.3.20

Verified by replaying both into empty containers (postgres:16, clickhouse-server:26.3)
and running `go run ./cmd/migrate postgres --dry-run` against the result. The dry-run
emitted no DDL, which means the baseline reproduces `main`'s Ent schema exactly
(verified at `ba513ce52`).

## Deliberate differences from the source database

Each one is commented inline in the SQL. Search for `DEVIATION FROM PROD` and
`RENAMED FROM PROD`.

**Portability fixes.** The dump was taken with a `pg_dump` 18 client against a PG 16
server, which emits three things PG 16 cannot replay: `\restrict` / `\unrestrict`
(psql 18 meta-commands, removed), `SET transaction_timeout` (a PG 17+ GUC, commented
out), and a bare `CREATE SCHEMA public` (given `IF NOT EXISTS`, since a stock database
already has that schema). Without these the replay aborts at line 12 and creates nothing.

**Two indexes renamed to Ent's names.** Production still uses the older names:

| Production | Baseline (what Ent expects) |
|---|---|
| `subscription_plan_synced_price_sequence_id_idx` | `subscription_tenant_id_environ_c25981d…` |
| `subscriptionlineitem_tenant_id_environment_id_subscription_id_p` | `subscriptionlineitem_tenant_id_f0ae9089e4…` |

Same columns, same partial predicates — only the names differ. Ent matches indexes by
name alone, so keeping the production names would make every `--dry-run` report drift
that isn't real.

**`events` is partitioned daily.** Production still partitions it monthly
(`toYYYYMM(timestamp)`); the baseline uses `toYYYYMMDD(timestamp)` as intended.
`meter_usage` is already daily in both. ClickHouse cannot `ALTER` a partition key, so
changing production means a new table, a backfill, and `EXCHANGE TABLES`.

## Known issue in India production

`subscription_line_items` carries an invalid index — a `CREATE INDEX CONCURRENTLY`
that failed partway and left a dead stub:

```
subscriptionlineitem_tenant_id_f0ae9089e4afd8b35c340e4fa52379c2
  indisvalid = f   indisready = f
```

`pg_dump` skips invalid indexes, so it is absent from this baseline — correctly. But
Ent matches by name and counts it as present, which masks real drift in any dry-run
against that database. Drop it before recreating anything under that name:

```sql
DROP INDEX CONCURRENTLY subscriptionlineitem_tenant_id_f0ae9089e4afd8b35c340e4fa52379c2;
```

## Refreshing

The generator scripts are not committed. Regenerate with `pg_dump --schema-only
--no-owner --no-privileges --schema=public` for Postgres, and `SHOW CREATE TABLE` per
table over the HTTP interface for ClickHouse — then re-apply the deviations above, or
consciously drop them.
