#!/usr/bin/env bash
# Adopt an EXISTING database: record migrations as applied without running them.
# dbmate has no --baseline, so this is the equivalent. Executes zero DDL.
#
#   ./adopt.sh <database-url> <migrations-dir> <up-to-version> [--reference <url>]
#
# Recording a version is a CLAIM that the database already contains everything those
# migrations would have created. Nothing can verify it afterwards, and if the claim
# is wrong dbmate will skip the DDL forever and the schema stays permanently short.
# So this refuses to run blind: pass --reference pointing at a scratch database built
# from the same migration set, and the schema fingerprints must match.
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
  if ! diff -q "$A" "$B" >/dev/null; then
    echo "FAIL: this database does not match the reference — adopting would record a false claim." >&2
    diff "$A" "$B" | grep '^[<>]' | head -20 >&2
    exit 1
  fi
  echo "fingerprint matches the reference"
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
