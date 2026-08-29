#!/usr/bin/env bash
# Draft the next migration from the Ent schema.
#
# Builds a throwaway database from the committed migrations, asks Ent what is still
# missing, and writes that DDL into a new dbmate file. The result is a DRAFT: it has
# no CONCURRENTLY, no lock timeouts, and no lane placement. Review and edit before
# committing.
#
#   ./generate.sh add_currency_to_invoices
set -euo pipefail
NAME="${1:?usage: generate.sh <migration_name>}"
DIR="${MIGRATIONS_PG:-migrations/versioned/postgres}"
PGHOST_="${PGHOST_:-localhost}"; PGPORT_="${PGPORT_:-5432}"
PGUSER_="${PGUSER_:-flexprice}"; PGPASS_="${PGPASS_:-flexprice123}"
# Password via PGPASSWORD only — a URL passed as an argument is visible in
# `ps aux`, CI logs and Kubernetes audit records.
BASE="postgres://$PGUSER_@$PGHOST_:$PGPORT_"
export PGPASSWORD="$PGPASS_"

# ent/ is generated from ent/schema/, and everything below compares against the
# generated ent/migrate/schema.go. A stale ent/ makes `go run ./cmd/migrate` panic
# with an index-out-of-range that names nothing useful. Regenerating is idempotent —
# a no-op when ent/ is already current — so just do it rather than making it a step
# the developer has to remember.
echo "regenerating ent/ …" >&2
if ! make generate-ent >/dev/null 2>&1; then
  echo "FAIL: 'make generate-ent' failed — fix ent/schema/ first" >&2
  exit 1
fi

psql "$BASE/postgres?sslmode=disable" -q -c "DROP DATABASE IF EXISTS mig_draft;" \
                                     -c "CREATE DATABASE mig_draft;" >/dev/null
DATABASE_URL="$BASE/mig_draft?sslmode=disable" dbmate --migrations-dir "$DIR" --no-dump-schema up >/dev/null

# Keep stderr, but only show it if the command actually failed — the structured
# logger writes several JSON lines on every run and they are not useful here.
RAW="$(mktemp)"; ERR="$(mktemp)"; trap 'rm -f "$RAW" "$ERR"' EXIT
if ! FLEXPRICE_POSTGRES_HOST="$PGHOST_" FLEXPRICE_POSTGRES_PORT="$PGPORT_" \
       FLEXPRICE_POSTGRES_USER="$PGUSER_" FLEXPRICE_POSTGRES_PASSWORD="$PGPASS_" \
       FLEXPRICE_POSTGRES_DBNAME="mig_draft" FLEXPRICE_POSTGRES_SSLMODE="disable" \
       FLEXPRICE_MIGRATE_UNSAFE=1 \
       go run ./cmd/migrate postgres --dry-run --allow-index-changes > "$RAW" 2>"$ERR"; then
  echo "FAIL: could not read the Ent schema" >&2; sed 's/^/  /' "$ERR" >&2; exit 1
fi
DDL="$(grep -v '^$' "$RAW" || true)"

# Make the draft re-runnable where that is unambiguously safe. Deployments hold
# different schemas, so a migration may meet a column or table that already exists.
#
# Columns and tables only. Index creation is deliberately left alone: the draft is
# meant to gain CONCURRENTLY by hand, and IF NOT EXISTS on a concurrent build
# silently skips an INVALID index left by an earlier failure — Postgres reports
# CREATE INDEX and the index stays broken forever.
DDL="$(printf '%s\n' "$DDL" \
  | sed -e 's/ADD COLUMN "/ADD COLUMN IF NOT EXISTS "/g' \
        -e 's/^CREATE TABLE "/CREATE TABLE IF NOT EXISTS "/')"

if [ -z "$DDL" ]; then
  echo "nothing to generate — migrations already cover the Ent schema"
  exit 0
fi

# Before drafting: apply what Ent wants to the scratch database, then ask again.
# Anything it STILL proposes is a no-op it will keep proposing forever — a predicate
# written in a form Postgres does not store. Those statements are indistinguishable
# from a real predicate change once they are sitting in a draft, so warn here rather
# than let them be pasted in.
psql -X -q "$BASE/mig_draft?sslmode=disable" -c "$DDL" >/dev/null 2>&1 || true
RESIDUE="$(FLEXPRICE_POSTGRES_HOST="$PGHOST_" FLEXPRICE_POSTGRES_PORT="$PGPORT_" \
  FLEXPRICE_POSTGRES_USER="$PGUSER_" FLEXPRICE_POSTGRES_PASSWORD="$PGPASS_" \
  FLEXPRICE_POSTGRES_DBNAME="mig_draft" FLEXPRICE_POSTGRES_SSLMODE="disable" \
  FLEXPRICE_MIGRATE_UNSAFE=1 \
  go run ./cmd/migrate postgres --dry-run --allow-index-changes 2>/dev/null | grep -v '^$' || true)"

if [ -n "$RESIDUE" ]; then
  # Refuse to write the file. A draft containing no-op DDL is a trap: the
  # DROP+CREATE pair is indistinguishable from a real predicate change once it is
  # sitting in a migration, and keeping it ships a pointless index rebuild on a hot
  # table. Fixing the annotation takes two lines and the exact string is printed
  # below, so blocking is cheaper than a draft nobody can safely review.
  echo >&2
  echo "REFUSING TO DRAFT — an index predicate is written in a form Postgres does" >&2
  echo "not store, so Ent proposes rebuilding that index forever." >&2
  echo >&2
  echo "These statements are NO-OPs. In a draft they are indistinguishable from a" >&2
  echo "real predicate change:" >&2
  echo >&2
  echo "$RESIDUE" | sed 's/^/  /' >&2
  echo >&2
  echo "Fix the annotation in ent/schema/, run 'make generate-ent', and try again:" >&2
  echo >&2
  for idx in $(echo "$RESIDUE" | grep -oE 'INDEX "[a-z0-9_]+"' | grep -oE '"[a-z0-9_]+"' | tr -d '"' | sort -u); do
    want="$(psql -X -tAc "SELECT substring(indexdef from 'WHERE (.*)\$') FROM pg_indexes WHERE indexname='$idx';" \
            "$BASE/mig_draft?sslmode=disable" 2>/dev/null || true)"
    # No attempt to name the source file: Ent derives the index name from a hash,
    # so it does not appear in ent/schema/ to grep for. The table name in the
    # statement above is enough to find it.
    [ -n "$want" ] && printf '    entsql.IndexWhere("%s")\n\n' "$want" >&2
  done
  echo "No file was written." >&2
  exit 1
fi

FILE="$DIR/$(date -u +%Y%m%d%H%M%S)_${NAME}.sql"
{
  echo "-- migrate:up"
  echo "-- DRAFT generated by scripts/migrations/generate.sh — review before merging."
  echo "-- Columns and tables already carry IF NOT EXISTS. Index creation does not:"
  echo "-- add CONCURRENTLY by hand, and note that IF NOT EXISTS on a concurrent build"
  echo "-- silently skips an INVALID index left by an earlier failure."
  echo "-- Then choose the lane and write the down block."
  echo "SET lock_timeout = '3s';"
  echo "SET statement_timeout = '30s';"
  echo
  echo "$DDL"
  echo
  echo "-- migrate:down"
  echo "-- TODO: write the reversal, or state why it is unsafe."
} > "$FILE"
echo "drafted $FILE"
echo "--- review it ---"
sed -n '1,20p' "$FILE"
