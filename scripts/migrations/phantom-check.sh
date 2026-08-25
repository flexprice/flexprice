#!/usr/bin/env bash
# Reject index predicates written in a form Postgres does not store.
#
# Ent compares index predicates as STRINGS. Write `status = 'published'` when
# Postgres stores `((status)::text = 'published'::text)` and Ent proposes rebuilding
# that index on every run, forever. The rebuild is a no-op, so nothing breaks — but
# `make migrate-generate` then drafts a DROP+CREATE pair that looks exactly like a
# real predicate change, and a reviewer cannot tell them apart. Keep one and you
# ship a pointless index rebuild on a hot table.
#
# The rule this enforces needs no knowledge of what "canonical" means:
#
#   if the migrations already satisfy Ent, Ent must have nothing left to propose.
#
# So: build a database from the migrations, apply Ent to a copy, confirm the two
# match (that is the sync check), then ask Ent what it would still change. Anything
# it names is a no-op it will keep proposing forever.
#
# Run this AFTER the sync check. The rule only holds once the migrations satisfy
# Ent — otherwise the residue is a genuinely missing migration, not a phantom, and
# the advice below would be wrong.
set -euo pipefail
DIR="${1:-migrations/versioned/postgres}"
PGHOST_="${PGHOST_:-localhost}"; PGPORT_="${PGPORT_:-5440}"
PGUSER_="${PGUSER_:-flexprice}"; PGPASS_="${PGPASS_:-flexprice123}"
BASE="postgres://$PGUSER_@$PGHOST_:$PGPORT_"
export PGPASSWORD="$PGPASS_"

psql "$BASE/postgres?sslmode=disable" -q -c "DROP DATABASE IF EXISTS phantom_probe;" \
                                     -c "CREATE DATABASE phantom_probe;" >/dev/null
DATABASE_URL="$BASE/phantom_probe?sslmode=disable" \
  dbmate --migrations-dir "$DIR" --no-dump-schema up >/dev/null

# Capture to a file and check the command's OWN status. Piping straight into grep
# would report grep's status instead — and grep exits 1 when it matches nothing,
# which under `set -e` + pipefail kills the script on the success path.
OUT="$(mktemp)"; trap 'rm -f "$OUT"' EXIT
if ! FLEXPRICE_POSTGRES_HOST="$PGHOST_" FLEXPRICE_POSTGRES_PORT="$PGPORT_" \
  FLEXPRICE_POSTGRES_USER="$PGUSER_" FLEXPRICE_POSTGRES_PASSWORD="$PGPASS_" \
  FLEXPRICE_POSTGRES_DBNAME="phantom_probe" FLEXPRICE_POSTGRES_SSLMODE="disable" \
  FLEXPRICE_MIGRATE_UNSAFE=1 \
  go run ./cmd/migrate postgres --dry-run --allow-index-changes > "$OUT" 2>/dev/null; then
  echo "phantom check: FAIL — could not run the Ent dry run" >&2; exit 1
fi
RESIDUE="$(grep -v '^$' "$OUT" || true)"

[ -z "$RESIDUE" ] && { echo "phantom check: OK — no no-op churn"; exit 0; }

echo "phantom check: FAIL — Ent proposes changes the migrations already satisfy." >&2
echo "These rebuild indexes that need nothing, forever, and pollute every draft:" >&2
echo "$RESIDUE" | sed 's/^/  /' >&2
echo >&2
echo "Cause: an entsql.IndexWhere predicate is written in a form Postgres does not" >&2
echo "store. Replace it with what Postgres actually stores:" >&2
echo >&2
for idx in $(echo "$RESIDUE" | grep -oE 'INDEX "[a-z0-9_]+"' | grep -oE '"[a-z0-9_]+"' | tr -d '"' | sort -u); do
  want="$(psql -X -tAc "SELECT substring(indexdef from 'WHERE (.*)\$') FROM pg_indexes WHERE indexname='$idx';" \
          "$BASE/phantom_probe?sslmode=disable" 2>/dev/null || true)"
  [ -n "$want" ] && printf '  %s\n    entsql.IndexWhere("%s")\n\n' "$idx" "$want" >&2
done
exit 1
