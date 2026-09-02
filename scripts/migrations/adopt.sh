#!/usr/bin/env bash
# Adopt an EXISTING database: record migrations as applied without running them.
# dbmate has no --baseline, so this is the equivalent. Executes zero DDL.
#
#   ./adopt.sh <database-url> <migrations-dir> <version|head> [--reference <url>] [--dry-run]
#
# --dry-run prints what WOULD be recorded and exits without writing anything, not
# even the schema_migrations table. Use it first on every database — this is run
# once per deployment and per client, by hand, against production.
#
# "head" adopts at the newest migration in the directory: everything already written
# is recorded as applied and nothing runs, so only migrations added AFTER this point
# ever execute on that database. That is the normal choice for an existing
# deployment — its schema already reflects the history, whatever route it took.
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

# --dry-run is accepted anywhere so it can be appended to a command already typed
# out, rather than retyped into the middle of the positional arguments.
DRY=""
ARGS=()
for a in "$@"; do
  if [ "$a" = "--dry-run" ]; then DRY=1; else ARGS+=("$a"); fi
done
set -- ${ARGS[@]+"${ARGS[@]}"}

URL="${1:?database url}"; DIR="${2:?migrations dir}"; UPTO="${3:?version or 'head'}"
if [ "$UPTO" = "head" ]; then
  UPTO="$(ls -1 "$DIR"/*.sql 2>/dev/null | sed 's#.*/##' | cut -d_ -f1 | sort -n | tail -1)"
  [ -n "$UPTO" ] || { echo "FAIL: no migrations in $DIR" >&2; exit 1; }
  echo "head is $UPTO"
fi
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

# Versions at or below UPTO — the ones adoption claims this database already lived
# through. Shared by the dry run and the real run so they cannot disagree.
PLANNED=()
for f in "$DIR"/*.sql; do
  v="$(basename "$f" | cut -d_ -f1)"
  [ "$v" -gt "$UPTO" ] 2>/dev/null && continue
  PLANNED+=("$v")
done

if [ -n "$DRY" ]; then
  # A missing schema_migrations table is the normal case and means "nothing
  # recorded". Every OTHER failure -- unreachable host, bad password, no such
  # database -- must NOT be reported as an empty ledger: `|| true` would print a
  # confident adoption plan and exit 0 against a database it never reached.
  ERRF="$(mktemp)"
  if RECORDED="$(psql -X -tAq -v ON_ERROR_STOP=1 "$URL" -c \
       "SELECT version FROM schema_migrations" 2>"$ERRF")"; then
    :
  elif grep -q "schema_migrations" "$ERRF" && grep -q "does not exist" "$ERRF"; then
    RECORDED=""
  else
    echo "FAIL: could not read schema_migrations:" >&2
    sed 's/^/  /' "$ERRF" >&2
    rm -f "$ERRF"; exit 1
  fi
  rm -f "$ERRF"
  if [ -z "$RECORDED" ]; then
    echo "schema_migrations: absent or empty — would be created"
  else
    echo "schema_migrations: $(echo "$RECORDED" | grep -c .) version(s) already recorded"
  fi
  new=0
  for v in ${PLANNED[@]+"${PLANNED[@]}"}; do
    if echo "$RECORDED" | grep -qx "$v"; then
      echo "  skip   $v  (already recorded)"
    else
      echo "  insert $v"
      new=$((new+1))
    fi
  done
  echo "DRY RUN: would record $new new version(s) up to $UPTO — nothing written, zero DDL"
  exit 0
fi

psql "$URL" -v ON_ERROR_STOP=1 -q -c \
  "CREATE TABLE IF NOT EXISTS schema_migrations (version varchar(255) PRIMARY KEY);"

n=0
for v in ${PLANNED[@]+"${PLANNED[@]}"}; do
  psql "$URL" -v ON_ERROR_STOP=1 -q -c \
    "INSERT INTO schema_migrations (version) VALUES ('$v') ON CONFLICT DO NOTHING;"
  n=$((n+1))
done
echo "adopted $n migration(s) up to $UPTO — zero statements executed"
