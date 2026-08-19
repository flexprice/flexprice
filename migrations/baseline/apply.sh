#!/usr/bin/env bash
#
# Apply the Flexprice schema baseline to a fresh Postgres and/or ClickHouse.
#
# The baseline recreates the full schema from empty. It is NOT idempotent on
# Postgres: run it against a database with no flexprice tables in it.
#
#   ./apply.sh --postgres                 # Postgres only
#   ./apply.sh --clickhouse               # ClickHouse only (single-node engines)
#   ./apply.sh --clickhouse-replicated    # ClickHouse, Replicated* engines, hot tables only
#   ./apply.sh --all                      # Postgres + single-node ClickHouse
#   ./apply.sh --all --dry-run            # print what would run, touch nothing
#
# Connection details are read from the environment, using the same variable
# names the application itself uses, so you can source one of the .env files
# and run this directly:
#
#   set -a && source .env.staging && set +a
#   ./migrations/baseline/apply.sh --all
#
# Postgres:    FLEXPRICE_POSTGRES_{HOST,PORT,USER,PASSWORD,DBNAME,SSLMODE}
# ClickHouse:  FLEXPRICE_CLICKHOUSE_{ADDRESS,USERNAME,PASSWORD,DATABASE}
#              CH_HTTP_PORT overrides the HTTP port (default 8123; the ADDRESS
#              variable carries the native port 9000, which this script ignores).

set -euo pipefail

DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PG_FILE="${DIR}/postgres_baseline_20260819.sql"
CH_FILE="${DIR}/clickhouse_baseline_20260819.sql"
CH_REPLICATED_FILE="${DIR}/clickhouse_baseline_replicated_20260819.sql"

DO_PG=false
DO_CH=false
DRY_RUN=false

while [ $# -gt 0 ]; do
  case "$1" in
    --postgres)   DO_PG=true ;;
    --clickhouse) DO_CH=true ;;
    --clickhouse-replicated) DO_CH=true; CH_FILE="$CH_REPLICATED_FILE" ;;
    --all)        DO_PG=true; DO_CH=true ;;
    --dry-run)    DRY_RUN=true ;;
    -h|--help)    sed -n '2,25p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *)            echo "unknown argument: $1" >&2; exit 2 ;;
  esac
  shift
done

if [ "$DO_PG" = false ] && [ "$DO_CH" = false ]; then
  echo "nothing to do: pass --postgres, --clickhouse, or --all" >&2
  exit 2
fi

require() {
  for var in "$@"; do
    if [ -z "${!var:-}" ]; then
      echo "missing required environment variable: ${var}" >&2
      exit 1
    fi
  done
}

# --------------------------------------------------------------------------
# Postgres
# --------------------------------------------------------------------------
apply_postgres() {
  require FLEXPRICE_POSTGRES_HOST FLEXPRICE_POSTGRES_USER \
          FLEXPRICE_POSTGRES_PASSWORD FLEXPRICE_POSTGRES_DBNAME

  local port="${FLEXPRICE_POSTGRES_PORT:-5432}"
  local sslmode="${FLEXPRICE_POSTGRES_SSLMODE:-require}"
  local conn="host=${FLEXPRICE_POSTGRES_HOST} port=${port} user=${FLEXPRICE_POSTGRES_USER} dbname=${FLEXPRICE_POSTGRES_DBNAME} sslmode=${sslmode}"

  echo "==> Postgres: ${FLEXPRICE_POSTGRES_HOST}:${port}/${FLEXPRICE_POSTGRES_DBNAME}"

  if [ "$DRY_RUN" = true ]; then
    echo "    would run: psql \"<conn>\" -v ON_ERROR_STOP=1 -f ${PG_FILE}"
    return
  fi

  export PGPASSWORD="${FLEXPRICE_POSTGRES_PASSWORD}"

  # Refuse to run over an existing schema rather than half-applying onto it.
  local existing
  existing=$(psql "$conn" -tAc \
    "select count(*) from information_schema.tables where table_schema='public' and table_type='BASE TABLE'")
  if [ "$existing" != "0" ]; then
    echo "    ABORT: target already has ${existing} tables in schema public." >&2
    echo "    The baseline expects an empty database. Point at a fresh one, or drop the schema first." >&2
    exit 1
  fi

  # stdout is just SET/set_config chatter; errors go to stderr and
  # ON_ERROR_STOP makes psql exit non-zero on the first failure.
  psql "$conn" -v ON_ERROR_STOP=1 -q -f "$PG_FILE" > /dev/null

  psql "$conn" -tAc "
    select '    tables:    '||count(*) from information_schema.tables where table_schema='public' and table_type='BASE TABLE'
    union all select '    indexes:   '||count(*) from pg_indexes where schemaname='public'
    union all select '    sequences: '||count(*) from information_schema.sequences where sequence_schema='public'
    union all select '    functions: '||count(*) from information_schema.routines where routine_schema='public'"
  echo "    Postgres baseline applied."
}

# --------------------------------------------------------------------------
# ClickHouse
# --------------------------------------------------------------------------
apply_clickhouse() {
  require FLEXPRICE_CLICKHOUSE_ADDRESS FLEXPRICE_CLICKHOUSE_USERNAME \
          FLEXPRICE_CLICKHOUSE_PASSWORD

  local host="${FLEXPRICE_CLICKHOUSE_ADDRESS%%:*}"
  local port="${CH_HTTP_PORT:-8123}"
  local db="${FLEXPRICE_CLICKHOUSE_DATABASE:-flexprice}"
  local endpoint="http://${host}:${port}/"

  echo "==> ClickHouse: ${host}:${port}/${db}"
  if [ "$CH_FILE" = "$CH_REPLICATED_FILE" ]; then
    echo "    using REPLICATED baseline (events, meter_usage only)"
    echo "    requires {cluster}, {shard}, {replica} macros on every node"
  fi

  if [ "$DRY_RUN" = true ]; then
    echo "    would apply ${CH_FILE} statement by statement"
    return
  fi

  # The dump qualifies every object as flexprice.<table>; retarget if the
  # deployment uses a different database name.
  local sql_file="$CH_FILE"
  if [ "$db" != "flexprice" ]; then
    sql_file="$(mktemp)"
    sed "s/\bflexprice\./${db}./g; s/CREATE DATABASE IF NOT EXISTS flexprice;/CREATE DATABASE IF NOT EXISTS ${db};/" \
      "$CH_FILE" > "$sql_file"
    echo "    retargeted database name: flexprice -> ${db}"
  fi

  CH_ENDPOINT="$endpoint" CH_SQL="$sql_file" python3 - <<'PYEOF'
import os, re, sys, urllib.request

endpoint = os.environ["CH_ENDPOINT"]
sql = open(os.environ["CH_SQL"]).read()
headers = {
    "X-ClickHouse-User": os.environ["FLEXPRICE_CLICKHOUSE_USERNAME"],
    "X-ClickHouse-Key": os.environ["FLEXPRICE_CLICKHOUSE_PASSWORD"],
}

# ClickHouse rejects multi-statement bodies, so each statement is sent alone.
# No ClickHouse DDL in this file contains a semicolon inside a statement body.
statements = [s.strip() for s in re.split(r";\s*\n", sql) if s.strip()]

applied = 0
for statement in statements:
    body = statement.rstrip().rstrip(";")
    match = re.search(r"(?im)^\s*(CREATE\s+(?:DATABASE|TABLE|VIEW|MATERIALIZED VIEW)[^\n(]*)", body)
    if not match:
        continue
    request = urllib.request.Request(endpoint, data=body.encode(), headers=headers)
    try:
        urllib.request.urlopen(request, timeout=120)
        applied += 1
    except Exception as exc:
        detail = getattr(exc, "read", lambda: b"")().decode()[:300]
        sys.exit("    FAILED: %s\n    %s" % (match.group(1).strip()[:80], detail))

print("    applied %d statements." % applied)
PYEOF

  curl -sS --max-time 30 \
    -H "X-ClickHouse-User: ${FLEXPRICE_CLICKHOUSE_USERNAME}" \
    -H "X-ClickHouse-Key: ${FLEXPRICE_CLICKHOUSE_PASSWORD}" \
    --data-binary "SELECT concat('    ', name, '  ', engine, '  ', partition_key)
                   FROM system.tables WHERE database='${db}' ORDER BY name FORMAT TSV" \
    "$endpoint"
  echo "    ClickHouse baseline applied."
}

[ "$DO_PG" = true ] && apply_postgres
[ "$DO_CH" = true ] && apply_clickhouse

echo "Done."
