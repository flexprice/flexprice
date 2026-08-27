#!/usr/bin/env bash
# Adopt an EXISTING database: record migrations as applied without running them.
# dbmate has no --baseline, so this is the equivalent. Executes zero DDL.
#
#   ./adopt.sh <database-url> <migrations-dir> <up-to-version> [--reference <url>]
#
# Recording a version claims only: "this database does not need anything before that
# point". It does NOT claim the schema matches any reference — it cannot. Deployments
# have different lineages (AutoMigrate, DMS migration, hand-run DDL) and measured
# 610 differing catalog lines between GCP staging and a prod-derived baseline.
#
# --reference therefore prints a DIFF for you to read, and does not block. Read it:
# it is the record of what this deployment carries that the migration set does not,
# and it is the only time anyone will look.
set -euo pipefail
URL="${1:?database url}"; DIR="${2:?migrations dir}"; UPTO="${3:?version}"
REF=""; [ "${4:-}" = "--reference" ] && REF="${5:?reference url}"
HERE="$(cd "$(dirname "$0")" && pwd)"

# UPTO must name a migration that actually exists, or a typo silently adopts
# everything (or nothing) and reports success either way.
case "$UPTO" in
  *[!0-9]*|"") echo "FAIL: version must be numeric, got '$UPTO'" >&2; exit 1 ;;
esac
ls "$DIR"/"$UPTO"_*.sql >/dev/null 2>&1 || {
  echo "FAIL: no migration in $DIR matches version $UPTO" >&2; exit 1; }

if [ -n "$REF" ]; then
  A="$(mktemp)"; B="$(mktemp)"
  psql -X -q "$URL" -f "$HERE/fingerprint.sql" > "$A"
  psql -X -q "$REF" -f "$HERE/fingerprint.sql" > "$B"
  # Two empty results compare equal. Without this, a connection that reached the
  # wrong (empty) database would look like a perfect match and adopt on nothing.
  [ -s "$A" ] || { echo "FAIL: target database returned no schema" >&2; exit 1; }
  [ -s "$B" ] || { echo "FAIL: reference database returned no schema" >&2; exit 1; }
  if diff -q "$A" "$B" >/dev/null; then
    echo "fingerprint matches the reference exactly"
  else
    n="$(diff "$A" "$B" | grep -c '^[<>]' || true)"
    echo "NOTE: $n catalog lines differ from the reference." >&2
    echo "      Expected — deployments have separate lineages. Adoption records the" >&2
    echo "      line, not a claim of equality. Review before continuing:" >&2
    diff "$A" "$B" | grep '^[<>]' | head -15 | sed 's/^/        /' >&2
    echo "      (full diff: diff $A $B)" >&2
  fi
else
  echo "WARNING: no --reference given. Nothing has verified that this database" >&2
  echo "         actually contains what these migrations would have created." >&2
fi

psql "$URL" -v ON_ERROR_STOP=1 -q -c \
  "CREATE TABLE IF NOT EXISTS schema_migrations (version varchar(255) PRIMARY KEY);"

n=0
for f in "$DIR"/*.sql; do
  v="$(basename "$f" | cut -d_ -f1)"
  if [ "$v" -gt "$UPTO" ] 2>/dev/null; then continue; fi
  psql "$URL" -v ON_ERROR_STOP=1 -q -c \
    "INSERT INTO schema_migrations (version) VALUES ('$v') ON CONFLICT DO NOTHING;"
  n=$((n+1))
done
echo "adopted $n migration(s) up to $UPTO — zero statements executed"
