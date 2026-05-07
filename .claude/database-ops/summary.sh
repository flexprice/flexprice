#!/usr/bin/env bash
# summary.sh — Daily operational summary (Postgres + ClickHouse)
# Usage:
#   ./summary.sh                  → today in IST
#   ./summary.sh 2026-04-13       → specific IST date
#   ./summary.sh 2026-04-13 Asia/Kolkata

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ENV_FILE="${SCRIPT_DIR}/.env"

if [[ ! -f "$ENV_FILE" ]]; then
  echo "ERROR: .env not found at $ENV_FILE" >&2
  exit 1
fi
# shellcheck disable=SC1090
source "$ENV_FILE"

# ── Date handling ──────────────────────────────────────────────────────────────
TZ_NAME="${2:-Asia/Kolkata}"   # second arg (timezone); default IST
IST_DATE="${1:-}"

if [[ -z "$IST_DATE" ]]; then
  # Today in IST
  IST_DATE=$(TZ="$TZ_NAME" date +%Y-%m-%d)
fi

# Convert IST day boundaries → UTC timestamps (IST = UTC+5:30 = 19800s ahead)
# IST 00:00:00 → UTC prev-day 18:30:00
UTC_START=$(python3 -c "
from datetime import datetime, timedelta, timezone
IST = timezone(timedelta(hours=5, minutes=30))
local = datetime.strptime('${IST_DATE} 00:00:00', '%Y-%m-%d %H:%M:%S').replace(tzinfo=IST)
print(local.astimezone(timezone.utc).strftime('%Y-%m-%d %H:%M:%S'))
")
UTC_END=$(python3 -c "
from datetime import datetime, timedelta, timezone
IST = timezone(timedelta(hours=5, minutes=30))
local = datetime.strptime('${IST_DATE} 23:59:59', '%Y-%m-%d %H:%M:%S').replace(tzinfo=IST)
print(local.astimezone(timezone.utc).strftime('%Y-%m-%d %H:%M:%S'))
")

echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo " FlexPrice Daily Summary — ${IST_DATE} IST"
echo " UTC window: ${UTC_START} → ${UTC_END}"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

# ── PostgreSQL helpers ─────────────────────────────────────────────────────────
PSQL_BIN="$(command -v psql 2>/dev/null || echo /opt/homebrew/opt/libpq/bin/psql)"
if [[ ! -x "$PSQL_BIN" ]]; then
  echo "ERROR: psql not found. Run: brew install libpq && export PATH=/opt/homebrew/opt/libpq/bin:\$PATH" >&2
  exit 1
fi
PG_URL="postgresql://${FLEXPRICE_POSTGRES_USER}:${FLEXPRICE_POSTGRES_PASSWORD}@${FLEXPRICE_POSTGRES_READER_HOST}:${FLEXPRICE_POSTGRES_PORT:-5432}/${FLEXPRICE_POSTGRES_DBNAME}?sslmode=${FLEXPRICE_POSTGRES_SSLMODE:-require}&connect_timeout=10"

pg() {
  "$PSQL_BIN" "$PG_URL" -v ON_ERROR_STOP=1 -t -A -c "$1"
}

# ── ClickHouse helpers ─────────────────────────────────────────────────────────
CH_HOST="${FLEXPRICE_CLICKHOUSE_ADDRESS%%:*}"
CH_PORT="${FLEXPRICE_CLICKHOUSE_ADDRESS##*:}"
[[ "$CH_PORT" == "$FLEXPRICE_CLICKHOUSE_ADDRESS" ]] && CH_PORT="${FLEXPRICE_CLICKHOUSE_PORT:-9000}"

CH_BIN=(clickhouse client)

ch() {
  "${CH_BIN[@]}" \
    --host "$CH_HOST" --port "$CH_PORT" \
    --user "$FLEXPRICE_CLICKHOUSE_USERNAME" \
    --password "$FLEXPRICE_CLICKHOUSE_PASSWORD" \
    --database "$FLEXPRICE_CLICKHOUSE_DATABASE" \
    --format TabSeparated \
    --query "$1"
}

# ── PostgreSQL metrics ─────────────────────────────────────────────────────────
echo ""
echo "▶ PostgreSQL"
echo "─────────────────────────────────────────────"

NEW_SUBS=$(pg "
  SELECT count(*)
  FROM subscriptions
  WHERE created_at >= '${UTC_START}'
    AND created_at <= '${UTC_END}'
    AND status = 'published'
" 2>/dev/null || echo "ERR")

ACTIVE_SUBS=$(pg "
  SELECT count(*)
  FROM subscriptions
  WHERE subscription_status = 'active'
    AND status = 'published'
" 2>/dev/null || echo "ERR")

NEW_CUSTOMERS=$(pg "
  SELECT count(*)
  FROM customers
  WHERE created_at >= '${UTC_START}'
    AND created_at <= '${UTC_END}'
    AND status = 'published'
" 2>/dev/null || echo "ERR")

NEW_INVOICES=$(pg "
  SELECT count(*)
  FROM invoices
  WHERE created_at >= '${UTC_START}'
    AND created_at <= '${UTC_END}'
    AND status = 'published'
" 2>/dev/null || echo "ERR")

FINALIZED_INVOICES=$(pg "
  SELECT count(*)
  FROM invoices
  WHERE finalized_at >= '${UTC_START}'
    AND finalized_at <= '${UTC_END}'
    AND invoice_status = 'FINALIZED'
    AND status = 'published'
" 2>/dev/null || echo "ERR")

INVOICE_AMOUNT=$(pg "
  SELECT COALESCE(SUM(total), 0)::numeric(20,2)
  FROM invoices
  WHERE finalized_at >= '${UTC_START}'
    AND finalized_at <= '${UTC_END}'
    AND invoice_status = 'FINALIZED'
    AND status = 'published'
" 2>/dev/null || echo "ERR")

printf "  New subscriptions created : %s\n" "$NEW_SUBS"
printf "  Total active subscriptions: %s\n" "$ACTIVE_SUBS"
printf "  New customers             : %s\n" "$NEW_CUSTOMERS"
printf "  New invoices created      : %s\n" "$NEW_INVOICES"
printf "  Invoices finalized        : %s\n" "$FINALIZED_INVOICES"
printf "  Finalized invoice total   : \$%s\n" "$INVOICE_AMOUNT"

# Sub status breakdown
echo ""
echo "  Subscription status breakdown (all time):"
pg "
  SELECT subscription_status, count(*) as cnt
  FROM subscriptions
  WHERE status = 'published'
  GROUP BY subscription_status
  ORDER BY cnt DESC
" 2>/dev/null | while IFS=$'\t' read -r s c; do
  printf "    %-20s : %s\n" "$s" "$c"
done

# ── ClickHouse metrics ─────────────────────────────────────────────────────────
echo ""
echo "▶ ClickHouse"
echo "─────────────────────────────────────────────"

EVENTS_COUNT=$(ch "
  SELECT count()
  FROM events
  WHERE timestamp >= toDateTime64('${UTC_START}', 3)
    AND timestamp <= toDateTime64('${UTC_END}', 3)
" 2>/dev/null || echo "ERR")

RAW_EVENTS_COUNT=$(ch "
  SELECT count()
  FROM raw_events
  WHERE timestamp >= toDateTime64('${UTC_START}', 3)
    AND timestamp <= toDateTime64('${UTC_END}', 3)
" 2>/dev/null || echo "ERR")

UNIQUE_TENANTS=$(ch "
  SELECT uniq(tenant_id)
  FROM events
  WHERE timestamp >= toDateTime64('${UTC_START}', 3)
    AND timestamp <= toDateTime64('${UTC_END}', 3)
" 2>/dev/null || echo "ERR")

printf "  Events (by timestamp)  : %s\n" "$EVENTS_COUNT"
printf "  Raw events             : %s\n" "$RAW_EVENTS_COUNT"
printf "  Unique tenants         : %s\n" "$UNIQUE_TENANTS"

# Top event names
echo ""
echo "  Top 5 event names:"
ch "
  SELECT event_name, count() as cnt
  FROM events
  WHERE timestamp >= toDateTime64('${UTC_START}', 3)
    AND timestamp <= toDateTime64('${UTC_END}', 3)
  GROUP BY event_name
  ORDER BY cnt DESC
  LIMIT 5
" 2>/dev/null | while IFS=$'\t' read -r name cnt; do
  printf "    %-35s : %s\n" "$name" "$cnt"
done

echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo " Done."
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
