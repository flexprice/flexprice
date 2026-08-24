#!/usr/bin/env bash
# Adopt an EXISTING database: record migrations as applied without running them.
# dbmate has no --baseline, so this is the equivalent. Executes zero DDL.
#
#   ./adopt.sh <database-url> <migrations-dir> <up-to-version>
set -euo pipefail
URL="${1:?database url}"; DIR="${2:?migrations dir}"; UPTO="${3:?version}"

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
