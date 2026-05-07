#!/usr/bin/env bash
# =============================================================================
# migrate_feature_usage_to_meter_usage.sh
#
# Migrates data from ${CH_DB}.feature_usage → ${CH_DB}.meter_usage
# in day-by-day batches.  Safe to re-run (destination is ReplacingMergeTree).
#
# Usage:
#   ./migrate_feature_usage_to_meter_usage.sh <start_date> <end_date>
#
# Examples:
#   ./migrate_feature_usage_to_meter_usage.sh 2024-01-01 2024-03-31
#   ./migrate_feature_usage_to_meter_usage.sh 2024-03-01 2024-03-31
#
# Env vars (override defaults):
#   CH_HOST, CH_PORT, CH_USER, CH_PASS, CH_DB
#   MAX_MEMORY_BYTES  – per-query memory cap (default 12 GiB)
#   MAX_THREADS       – ClickHouse worker threads per query (default 8)
#   DRY_RUN           – set to "1" to only COUNT, never INSERT
# =============================================================================
set -euo pipefail

# ── Connection ────────────────────────────────────────────────────────────────
CH_HOST="${CH_HOST:-localhost}"
CH_PORT="${CH_PORT:-9000}"
CH_DB="${CH_DB:-flexprice}"

# CH_USER and CH_PASS must be supplied via env — no insecure defaults
: "${CH_USER:?CH_USER env var is required}"
: "${CH_PASS:?CH_PASS env var is required}"

# ── Tuning ────────────────────────────────────────────────────────────────────
MAX_MEMORY_BYTES=96636764160   # 90 GiB — hardcoded per project guidelines
MAX_THREADS="${MAX_THREADS:-8}"
DRY_RUN="${DRY_RUN:-0}"

# ── Args ──────────────────────────────────────────────────────────────────────
if [[ $# -lt 2 ]]; then
    echo "Usage: $0 <start_date YYYY-MM-DD> <end_date YYYY-MM-DD>"
    exit 1
fi

START_DATE="$1"
END_DATE="$2"

# ── Helpers ───────────────────────────────────────────────────────────────────
log() { echo "[$(date '+%Y-%m-%d %H:%M:%S')] $*"; }

ch() {
    clickhouse client \
        --host="$CH_HOST" \
        --port="$CH_PORT" \
        --user="$CH_USER" \
        --password="$CH_PASS" \
        --database="$CH_DB" \
        --query="$1"
}

# Portable next-day calculation (GNU date / BSD date)
next_day() {
    date -d "$1 + 1 day" +%Y-%m-%d 2>/dev/null \
    || date -j -v+1d -f "%Y-%m-%d" "$1" +%Y-%m-%d
}

date_to_key() {
    # Convert YYYY-MM-DD → YYYYMMDD integer for ClickHouse
    echo "${1//-/}"
}

# ── Main loop ─────────────────────────────────────────────────────────────────
TOTAL_INSERTED=0
current="$START_DATE"

log "Starting migration  $START_DATE → $END_DATE  (dry_run=$DRY_RUN)"
[[ "$DRY_RUN" == "1" ]] && log "DRY RUN — no data will be written"

while [[ "$current" < "$END_DATE" || "$current" == "$END_DATE" ]]; do
    key=$(date_to_key "$current")

    # ── 1. Count eligible rows for the day ───────────────────────────────────
    COUNT=$(ch "
        SELECT count()
        FROM ${CH_DB}.feature_usage FINAL
        WHERE toYYYYMMDD(timestamp) = $key
          AND meter_id IS NOT NULL
          AND sign = 1
        SETTINGS max_memory_usage = $MAX_MEMORY_BYTES
    ")

    log "[$current | key=$key]  eligible rows = $COUNT"

    if [[ "$COUNT" -gt 0 && "$DRY_RUN" != "1" ]]; then
        # ── 2. INSERT-SELECT for the day ─────────────────────────────────────
        ch "
            INSERT INTO ${CH_DB}.meter_usage
            SELECT
                id,
                tenant_id,
                environment_id,
                external_customer_id,

                -- meter_id: Nullable → LowCardinality(String) NOT NULL
                -- (NULLs already excluded by WHERE clause above)
                meter_id,

                -- event_name: direct mapping
                event_name,

                -- timestamp: DateTime64(3) → DateTime  (sub-second precision dropped)
                toDateTime(timestamp)               AS timestamp,

                ingested_at,

                -- qty_total: Decimal(25,15) → Decimal(18,8)
                CAST(qty_total AS Decimal(18, 8))   AS qty_total,

                -- unique_hash: Nullable → String NOT NULL
                coalesce(unique_hash, '')            AS unique_hash,

                -- source: Nullable → LowCardinality(String) NOT NULL
                coalesce(source, '')                AS source,

                properties

            FROM ${CH_DB}.feature_usage FINAL
            WHERE toYYYYMMDD(timestamp) = $key
              AND meter_id IS NOT NULL
              AND sign = 1

            SETTINGS
                max_memory_usage        = $MAX_MEMORY_BYTES,
                max_threads             = $MAX_THREADS,
                max_insert_block_size   = 1048576,
                -- Respect destination merge-tree limits
                max_partitions_per_insert_block = 100
        "
        log "  → inserted $COUNT rows for $current"
        TOTAL_INSERTED=$((TOTAL_INSERTED + COUNT))
    elif [[ "$COUNT" -eq 0 ]]; then
        log "  → skipping (no eligible rows)"
    fi

    current=$(next_day "$current")
done

log "Migration complete.  Total rows inserted: $TOTAL_INSERTED"
