#!/usr/bin/env bash
# Apply pending migrations with the connection settings the migration files rely on
# but cannot set themselves.
#
#   lock_timeout=3s      a blocked ALTER gives up rather than queueing every query
#                        behind it (RCA: prod DDL-lock incident 2026-06-25)
#   statement_timeout=0  a CREATE INDEX CONCURRENTLY killed by a timeout leaves an
#                        INVALID index that nothing retries
#
# These cannot live in the SQL: a `transaction:false` file may contain exactly one
# statement, so there is no room for a SET. They have to come from the connection,
# which is why applying by hand with a bare `dbmate up` is not equivalent.
set -euo pipefail
URL="${DATABASE_URL:?DATABASE_URL is required}"
DIR="${1:?usage: apply.sh <migrations-dir>}"
OPTS="-c%20lock_timeout%3D3s%20-c%20statement_timeout%3D0"

case "$URL" in
  *\?*) SEP="&" ;;
  *)    SEP="?" ;;
esac

DATABASE_URL="${URL}${SEP}options=${OPTS}" \
  dbmate --migrations-dir "$DIR" --no-dump-schema up
