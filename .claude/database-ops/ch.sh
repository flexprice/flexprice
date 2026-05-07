#!/usr/bin/env bash
# ch.sh — ClickHouse query runner (read-only)
# Usage:
#   ./ch.sh "SELECT count(*) FROM events"
#   ./ch.sh < query.sql
#   echo "SELECT now()" | ./ch.sh
#   ./ch.sh -f myfile.sql
#   ./ch.sh --csv "SELECT ..."         → CSV output
#   ./ch.sh --json "SELECT ..."        → JSON (each row as JSON object)
#   ./ch.sh --pretty "SELECT ..."      → PrettyCompactMonoBlock (default)
#   ./ch.sh --tsv "SELECT ..."         → TSV

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ENV_FILE="${SCRIPT_DIR}/.env"

if [[ ! -f "$ENV_FILE" ]]; then
  echo "ERROR: .env not found at $ENV_FILE" >&2
  exit 1
fi
# shellcheck disable=SC1090
source "$ENV_FILE"

: "${FLEXPRICE_CLICKHOUSE_ADDRESS:?Missing FLEXPRICE_CLICKHOUSE_ADDRESS}"
: "${FLEXPRICE_CLICKHOUSE_DATABASE:?Missing FLEXPRICE_CLICKHOUSE_DATABASE}"
: "${FLEXPRICE_CLICKHOUSE_USERNAME:?Missing FLEXPRICE_CLICKHOUSE_USERNAME}"
: "${FLEXPRICE_CLICKHOUSE_PASSWORD:?Missing FLEXPRICE_CLICKHOUSE_PASSWORD}"

# Parse host:port from address
CH_HOST="${FLEXPRICE_CLICKHOUSE_ADDRESS%%:*}"
CH_PORT="${FLEXPRICE_CLICKHOUSE_ADDRESS##*:}"
[[ "$CH_PORT" == "$FLEXPRICE_CLICKHOUSE_ADDRESS" ]] && CH_PORT="${FLEXPRICE_CLICKHOUSE_PORT:-9000}"

FORMAT="PrettyCompactMonoBlock"
FILE_SQL=""
SQL_ARG=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --csv)      FORMAT="CSVWithNames"; shift ;;
    --tsv)      FORMAT="TabSeparatedWithNames"; shift ;;
    --json)     FORMAT="JSONEachRow"; shift ;;
    --pretty)   FORMAT="PrettyCompactMonoBlock"; shift ;;
    --vertical) FORMAT="Vertical"; shift ;;
    -f|--file)
      if [[ ! -f "$2" ]]; then
        echo "ERROR: File not found: $2" >&2
        exit 1
      fi
      FILE_SQL="$(cat "$2")"
      shift 2
      ;;
    *)          SQL_ARG="$1"; shift ;;
  esac
done

# Use array to avoid word-splitting issues with multi-word command
CH_BIN=(clickhouse client)

CH_OPTS=(
  --host "$CH_HOST"
  --port "$CH_PORT"
  --user "$FLEXPRICE_CLICKHOUSE_USERNAME"
  --password "$FLEXPRICE_CLICKHOUSE_PASSWORD"
  --database "$FLEXPRICE_CLICKHOUSE_DATABASE"
  --format "$FORMAT"
)

if [[ -n "$SQL_ARG" ]]; then
  "${CH_BIN[@]}" "${CH_OPTS[@]}" --query "$SQL_ARG"
elif [[ -n "$FILE_SQL" ]]; then
  echo "$FILE_SQL" | "${CH_BIN[@]}" "${CH_OPTS[@]}" --multiquery
else
  "${CH_BIN[@]}" "${CH_OPTS[@]}" --multiquery
fi
