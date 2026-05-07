#!/usr/bin/env bash
# pg.sh — PostgreSQL query runner (read-only)
# Usage:
#   ./pg.sh "SELECT count(*) FROM subscriptions"
#   ./pg.sh < query.sql
#   echo "SELECT now()" | ./pg.sh
#   ./pg.sh -f myfile.sql
#   ./pg.sh --csv "SELECT ..."         → CSV output
#   ./pg.sh --table "SELECT ..."       → aligned table output (default)

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ENV_FILE="${SCRIPT_DIR}/.env"

if [[ ! -f "$ENV_FILE" ]]; then
  echo "ERROR: .env not found at $ENV_FILE" >&2
  exit 1
fi
# shellcheck disable=SC1090
source "$ENV_FILE"

: "${FLEXPRICE_POSTGRES_READER_HOST:?Missing FLEXPRICE_POSTGRES_READER_HOST}"
: "${FLEXPRICE_POSTGRES_DBNAME:?Missing FLEXPRICE_POSTGRES_DBNAME}"
: "${FLEXPRICE_POSTGRES_USER:?Missing FLEXPRICE_POSTGRES_USER}"
: "${FLEXPRICE_POSTGRES_PASSWORD:?Missing FLEXPRICE_POSTGRES_PASSWORD}"
: "${FLEXPRICE_POSTGRES_PORT:=5432}"
: "${FLEXPRICE_POSTGRES_SSLMODE:=require}"

PG_URL="postgresql://${FLEXPRICE_POSTGRES_USER}:${FLEXPRICE_POSTGRES_PASSWORD}@${FLEXPRICE_POSTGRES_READER_HOST}:${FLEXPRICE_POSTGRES_PORT}/${FLEXPRICE_POSTGRES_DBNAME}?sslmode=${FLEXPRICE_POSTGRES_SSLMODE}&connect_timeout=10"

PSQL_BIN="$(command -v psql 2>/dev/null || echo /opt/homebrew/opt/libpq/bin/psql)"
if [[ ! -x "$PSQL_BIN" ]]; then
  echo "ERROR: psql not found. Run: brew install libpq && export PATH=/opt/homebrew/opt/libpq/bin:\$PATH" >&2
  exit 1
fi

FORMAT_FLAG=""
FILE_FLAG=""
SQL_ARG=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --csv)    FORMAT_FLAG="--csv"; shift ;;
    --table)  FORMAT_FLAG=""; shift ;;
    -f|--file)
      if [[ ! -f "$2" ]]; then
        echo "ERROR: File not found: $2" >&2
        exit 1
      fi
      FILE_FLAG="$2"
      shift 2
      ;;
    *)        SQL_ARG="$1"; shift ;;
  esac
done

PSQL_OPTS=("$PG_URL" -v ON_ERROR_STOP=1)
[[ -n "$FORMAT_FLAG" ]] && PSQL_OPTS+=("$FORMAT_FLAG")

if [[ -n "$SQL_ARG" ]]; then
  "$PSQL_BIN" "${PSQL_OPTS[@]}" -c "$SQL_ARG"
elif [[ -n "$FILE_FLAG" ]]; then
  "$PSQL_BIN" "${PSQL_OPTS[@]}" -f "$FILE_FLAG"
else
  "$PSQL_BIN" "${PSQL_OPTS[@]}"
fi
