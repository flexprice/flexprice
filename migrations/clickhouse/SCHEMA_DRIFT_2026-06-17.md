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
| `events` | column CODECs (ZSTD/Delta); `event_name`/`source` → `LowCardinality`; `customer_id` → `String DEFAULT ''`; **PROJECTION `proj_by_customer_event`**; **3 secondary indexes DROPPED**; `parts_to_*` raised; `deduplicate_merge_projection_mode='rebuild'` |
| `feature_usage` | column CODECs; indexes `bf_feature_id`/`mm_ts`/`bf_external_customer_id`/`bf_id`; `ReplacingMergeTree(version)` with `version`/`sign` cols |
| `meter_usage` | heavy `LowCardinality` typing; `DoubleDelta`/`ZSTD` codecs; `min_bytes_for_wide_part`, `enable_mixed_granularity_parts` settings |
| `costsheet_usage` | `processing_lag_ms` MATERIALIZED column; 6 bloom_filter + set indexes; codecs |
| `raw_events` | `field1..field10` Nullable columns; `bf_id` index; `version`/`sign` |
| `analytics_benchmark` | extensive `LowCardinality` + array columns + codecs |
| `usage_benchmark` | `LowCardinality` + Delta/ZSTD codecs |

Only `events` is reconciled in `000010` (billing-critical, verified, the
dominant restore table). The remaining tables should be reconciled the same way
before the GCP cutover — dump live-prod `SHOW CREATE` and make the repo match,
OR build the GCP target schema directly from `clickhouse-backup` backup
metadata. See infrastructure `docs/CH-MIGRATION-PROD-TO-STAGING-REHEARSAL.md`
(finding R10) and `docs/CH-MIGRATION-DETAIL.md` §1.1.
