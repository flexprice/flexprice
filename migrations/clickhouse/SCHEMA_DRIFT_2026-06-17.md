# ClickHouse schema drift — production vs migrations (2026-06-17)

Surfaced by the AWS→GCP prod-scale ClickHouse migration rehearsal. The live
production CH (`clickhousev2-mafga`, us-west-2) has drifted from the migrations
in this directory via hand-applied `ALTER`s that were never committed. The
existing **staging** CH still matches the migrations, so this is true
out-of-band production drift, not a partial migration apply.

## Why it matters

`clickhouse-backup restore --data` ATTACHes physical parts, which requires
byte-identical column types, CODECs, and projections between the backup source
(production) and the restore target. The AWS→GCP cutover restores production
parts into the **new GCP CH**, so the GCP table schemas must match production —
not the (older) migration schema. A migration-built `events` table fails the
ATTACH.

**Action taken:** `000010_align_events_with_prod_schema` captures the production
`events` schema as the repo source of truth (fresh CREATE matches prod;
in-place reconciliation ALTERs included but commented — heavy, run deliberately).

## Drift inventory (production `SHOW CREATE` vs migrations)

| Table | Drift observed on prod (not in migrations) |
|---|---|
Verified by comparing live-prod `SHOW CREATE` against the committed migration
for each table (codec / index / projection / LowCardinality / engine counts):

| Table | Drift? | repo → prod |
|---|---|---|
| `events` | 🔴 **MAJOR** | LowCardinality 0→2, CODEC 0→10, **PROJECTION 0→1** (`proj_by_customer_event`), **INDEX 3→0** (all dropped); `parts_to_*` raised; `deduplicate_merge_projection_mode='rebuild'` |
| `feature_usage` | 🔴 **YES** | CODEC 1→9, INDEX 3→4 (one extra) |
| `raw_events` | 🟠 **YES** | INDEX 3→1 (two dropped) |
| `analytics_benchmark` | 🟠 **minor** | LowCardinality 5→7 (two more cols) |
| `costsheet_usage` | ✅ match | identical (CODEC 1, INDEX 7, MATERIALIZED 1) |
| `meter_usage` | ✅ match | identical (LowCardinality 6, CODEC 6) |
| `usage_benchmark` | ✅ match | identical (`MergeTree()` vs `MergeTree` is cosmetic) |
| `events_processed` | ⚪ N/A | not present on prod source CH |

**4 of 7 in-scope tables drift** (events, feature_usage, raw_events,
analytics_benchmark); 3 match; `events_processed` is GCP/staging-only.

Each drifted attribute independently breaks `clickhouse-backup restore --data`
ATTACH (codecs/projections/indexes must be byte-identical). Only `events` is
reconciled in `000010` (billing-critical, verified, dominant restore table).
The other 3 drifted tables must be reconciled the same way before the GCP
cutover — dump live-prod `SHOW CREATE` and make the repo match, OR build the GCP
target schema directly from `clickhouse-backup` backup metadata. See
infrastructure `docs/CH-MIGRATION-PROD-TO-STAGING-REHEARSAL.md` (finding R10) and
`docs/CH-MIGRATION-DETAIL.md` §1.1.
