#!/usr/bin/env bash
# Does the migration set give the code everything it needs?
#
#   A = migrations only            (what a deployment will look like)
#   B = migrations + Ent           (what the code actually needs)
#
# Identical fingerprints = nothing was forgotten. Differing = a schema change
# shipped without a migration, and the PR must fail.
#
# Compares END STATES, not proposed statements: Ent emits permanent noise for any
# index predicate whose spelling differs from Postgres' canonical form, so
# "is the diff empty?" can never be a pass/fail test.
#
# Ent is used here purely as an oracle for the desired schema. It is not the
# deploy mechanism.
set -euo pipefail

PGHOST_="${PGHOST_:-localhost}"; PGPORT_="${PGPORT_:-5432}"
PGUSER_="${PGUSER_:-flexprice}"; PGPASS_="${PGPASS_:-flexprice123}"
DIR="${1:-migrations/versioned/postgres}"
# Password via PGPASSWORD only — never in argv.
BASE="postgres://$PGUSER_@$PGHOST_:$PGPORT_"
export PGPASSWORD="$PGPASS_"

for db in sync_a sync_b; do
  psql "$BASE/postgres?sslmode=disable" -q -c "DROP DATABASE IF EXISTS $db;" \
                                        -c "CREATE DATABASE $db;" >/dev/null
done

# Both databases are built the way a FRESH INSTALL is built: the baseline snapshot
# first, then the versioned migrations. The baseline lives outside the timeline —
# dbmate never runs it — so applying only the migrations would leave an empty
# schema and every column would read as "missing".
BASELINE="$(ls -1 migrations/baseline/postgres_baseline_ent_*.sql 2>/dev/null | tail -1)"
[ -n "$BASELINE" ] || { echo "no Ent baseline found in migrations/baseline/" >&2; exit 1; }

for db in sync_a sync_b; do
  psql -X -q -v ON_ERROR_STOP=1 "$BASE/$db?sslmode=disable" -f "$BASELINE" >/dev/null
  DATABASE_URL="$BASE/$db?sslmode=disable" dbmate --migrations-dir "$DIR" --no-dump-schema up >/dev/null
done

FLEXPRICE_POSTGRES_HOST="$PGHOST_" FLEXPRICE_POSTGRES_PORT="$PGPORT_" \
FLEXPRICE_POSTGRES_USER="$PGUSER_" FLEXPRICE_POSTGRES_PASSWORD="$PGPASS_" \
FLEXPRICE_POSTGRES_DBNAME="sync_b" FLEXPRICE_POSTGRES_SSLMODE="disable" \
FLEXPRICE_MIGRATE_UNSAFE=1 \
  go run ./cmd/migrate postgres --allow-index-changes >/dev/null 2>&1

A="$(mktemp)"; B="$(mktemp)"
psql -X -q "$BASE/sync_a?sslmode=disable" -f scripts/migrations/fingerprint.sql > "$A"
psql -X -q "$BASE/sync_b?sslmode=disable" -f scripts/migrations/fingerprint.sql > "$B"

if diff -q "$A" "$B" >/dev/null; then
  echo "sync check: OK — migrations cover the Ent schema"
  exit 0
fi
echo "sync check: FAIL — the Ent schema needs changes no migration provides:" >&2
diff "$A" "$B" | grep '^[<>]' | head -20 >&2
exit 1
