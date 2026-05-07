#!/usr/bin/env bash
set -euo pipefail

# =============================================================================
# PG -> ClickHouse Sync Script
#
# Syncs prices, subscriptions, subscription_line_items from PostgreSQL to CH.
# subscription_line_items (~120M+ rows) is batched by monthly updated_at windows.
# prices and subscriptions are synced in a single pass.
#
# Required env vars:
#   FLEXPRICE_POSTGRES_HOST, FLEXPRICE_POSTGRES_PORT, FLEXPRICE_POSTGRES_DBNAME,
#   FLEXPRICE_POSTGRES_USER, FLEXPRICE_POSTGRES_PASSWORD
#
# Optional env vars:
#   CLICKHOUSE_HOST     (default: localhost)
#   CLICKHOUSE_PORT     (default: 9000)
#   CLICKHOUSE_USER     (default: flexprice)
#   CLICKHOUSE_PASSWORD (default: flexprice123)
#   CLICKHOUSE_DB       (default: flexprice)
#
# Usage:
#   ./sync.sh                                    # Full sync, all tables
#   ./sync.sh --after "2026-01-01 00:00:00"      # Incremental sync
#   ./sync.sh --table prices                     # Single table
#   ./sync.sh --table subscription_line_items --after "2026-03-01 00:00:00"
#   ./sync.sh --dry-run                          # Print commands without executing
# =============================================================================

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# --- PG connection (required) ---
PG_HOST="${FLEXPRICE_POSTGRES_HOST:?Set FLEXPRICE_POSTGRES_HOST}"
PG_PORT="${FLEXPRICE_POSTGRES_PORT:?Set FLEXPRICE_POSTGRES_PORT}"
PG_DB="${FLEXPRICE_POSTGRES_DBNAME:?Set FLEXPRICE_POSTGRES_DBNAME}"
PG_USER="${FLEXPRICE_POSTGRES_USER:?Set FLEXPRICE_POSTGRES_USER}"
PG_PASS="${FLEXPRICE_POSTGRES_PASSWORD:?Set FLEXPRICE_POSTGRES_PASSWORD}"

# --- CH connection (optional, defaults for local dev) ---
CH_HOST="${CLICKHOUSE_HOST:-localhost}"
CH_PORT="${CLICKHOUSE_PORT:-9000}"
CH_USER="${CLICKHOUSE_USER:-flexprice}"
CH_PASS="${CLICKHOUSE_PASSWORD:-flexprice123}"
CH_DB="${CLICKHOUSE_DB:-flexprice}"

# --- Parse args ---
AFTER="1970-01-01 00:00:00"
TABLE="all"
DRY_RUN=false

while [[ $# -gt 0 ]]; do
    case "$1" in
        --after)  AFTER="$2"; shift 2 ;;
        --table)  TABLE="$2"; shift 2 ;;
        --dry-run) DRY_RUN=true; shift ;;
        *) echo "Unknown arg: $1"; exit 1 ;;
    esac
done

# --- Helpers ---
ch_client() {
    clickhouse-client \
        --host "$CH_HOST" \
        --port "$CH_PORT" \
        --user "$CH_USER" \
        --password "$CH_PASS" \
        --database "$CH_DB" \
        "$@"
}

run_sql() {
    local sql_file="$1"
    shift
    local params=(
        --multiquery
        --param_pg_host="$PG_HOST"
        --param_pg_port="$PG_PORT"
        --param_pg_db="$PG_DB"
        --param_pg_user="$PG_USER"
        --param_pg_pass="$PG_PASS"
        "$@"
    )

    if $DRY_RUN; then
        echo "[DRY RUN] clickhouse-client ${params[*]} < $sql_file"
    else
        ch_client "${params[@]}" < "$sql_file"
    fi
}

log() {
    echo "[$(date '+%Y-%m-%d %H:%M:%S')] $*"
}

# --- Sync functions ---

sync_prices() {
    log "Syncing prices (after: $AFTER)..."
    run_sql "$SCRIPT_DIR/sync_prices.sql" --param_after="$AFTER"
    log "Prices sync complete."
}

sync_subscriptions() {
    log "Syncing subscriptions (after: $AFTER)..."
    run_sql "$SCRIPT_DIR/sync_subscriptions.sql" --param_after="$AFTER"
    log "Subscriptions sync complete."
}

sync_subscription_line_items() {
    log "Syncing subscription_line_items (after: $AFTER) in monthly batches..."

    # Determine date range: from AFTER to now+1month, chunked by month
    # Use portable date parsing (macOS + Linux)
    if date -j -f "%Y-%m-%d %H:%M:%S" "$AFTER" "+%Y" &>/dev/null; then
        # macOS
        start_year=$(date -j -f "%Y-%m-%d %H:%M:%S" "$AFTER" "+%Y")
        start_month=$(date -j -f "%Y-%m-%d %H:%M:%S" "$AFTER" "+%m")
    else
        # Linux
        start_year=$(date -d "$AFTER" "+%Y")
        start_month=$(date -d "$AFTER" "+%m")
    fi

    end_year=$(date "+%Y")
    end_month=$(date "+%m")
    # Go one month past current to catch everything
    end_month=$((10#$end_month + 1))
    if [ "$end_month" -gt 12 ]; then
        end_month=1
        end_year=$((end_year + 1))
    fi

    local cur_year=$((10#$start_year))
    local cur_month=$((10#$start_month))
    local batch=0

    while true; do
        local next_month=$((cur_month + 1))
        local next_year=$cur_year
        if [ "$next_month" -gt 12 ]; then
            next_month=1
            next_year=$((next_year + 1))
        fi

        local batch_after
        local batch_before
        batch_after=$(printf "%04d-%02d-01 00:00:00" "$cur_year" "$cur_month")
        batch_before=$(printf "%04d-%02d-01 00:00:00" "$next_year" "$next_month")

        # For the first batch, use the actual AFTER timestamp
        if [ "$batch" -eq 0 ]; then
            batch_after="$AFTER"
        fi

        batch=$((batch + 1))
        log "  Batch $batch: [$batch_after, $batch_before)"

        run_sql "$SCRIPT_DIR/sync_subscription_line_items.sql" \
            --param_after="$batch_after" \
            --param_before="$batch_before"

        # Check if we've passed the end
        if [ "$next_year" -gt "$end_year" ] || \
           { [ "$next_year" -eq "$end_year" ] && [ "$next_month" -gt "$end_month" ]; }; then
            break
        fi

        cur_year=$next_year
        cur_month=$next_month
    done

    log "Subscription line items sync complete ($batch batches)."
}

# --- Main ---

log "=== PG -> ClickHouse Sync ==="
log "PG: $PG_USER@$PG_HOST:$PG_PORT/$PG_DB"
log "CH: $CH_USER@$CH_HOST:$CH_PORT/$CH_DB"
log "After: $AFTER | Table: $TABLE"
log ""

case "$TABLE" in
    all)
        sync_prices
        sync_subscriptions
        sync_subscription_line_items
        ;;
    prices)
        sync_prices
        ;;
    subscriptions)
        sync_subscriptions
        ;;
    subscription_line_items)
        sync_subscription_line_items
        ;;
    *)
        echo "Unknown table: $TABLE"
        echo "Options: prices, subscriptions, subscription_line_items, all"
        exit 1
        ;;
esac

log ""
log "=== Sync finished ==="
log "Tip: run 'OPTIMIZE TABLE flexprice.<table> FINAL' if you need immediate dedup."
