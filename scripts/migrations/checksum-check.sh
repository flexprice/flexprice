#!/usr/bin/env bash
# dbmate records only the version, never the file contents, so editing a migration
# that already ran on a client database is silent. This adds the missing guarantee.
#
# It covers migrations/baseline/ as well as migrations/versioned/. The baseline is
# FROZEN once any deployment has adopted: regenerating it would put a schema change
# into fresh installs while every existing deployment silently misses it, since
# nothing in the timeline carries the change. New schema goes in a migration.
#
# The manifest is authoritative and is NOT rewritten here: if it were, CI would
# quietly accept whatever it found, and a migration added in one PR would never be
# recorded for the next one to compare against. Regenerate deliberately with
#   ./checksum-check.sh <dir> <lock> --update
#   ./checksum-check.sh [--update] [glob ...]
#
# Postgres only, and named explicitly. The legacy migrations/postgres/V*.sql and
# migrations/clickhouse/ sets are not part of the versioned timeline. The
# ClickHouse set under migrations/versioned/ is inert — its gate is disabled and
# the set is known not to describe the real cluster — so freezing it would block
# the fix rather than protect anything.
set -euo pipefail
UPDATE=""
if [ "${1:-}" = "--update" ]; then UPDATE="--update"; shift; fi

# --update anywhere but first used to be silently treated as a path, so the script
# ran in CHECK mode against nothing and reported every tracked file as modified.
for a in "$@"; do
  if [ "$a" = "--update" ]; then
    echo "usage: $0 [--update] [path ...]   (--update must come FIRST)" >&2
    exit 2
  fi
  case "$a" in
    *.hashes)
      echo "The manifest path is no longer an argument — it is migrations/.hashes." >&2
      echo "usage: $0 [--update] [path ...]" >&2
      exit 2 ;;
  esac
done
LOCK="${LOCK:-migrations/.hashes}"
TARGETS=("$@")
[ ${#TARGETS[@]} -gt 0 ] || TARGETS=(
  "migrations/versioned/postgres"
  "migrations/baseline/postgres_baseline_ent_*.sql"
)

NEW="$(mktemp)"
for t in "${TARGETS[@]}"; do
  if [ -d "$t" ]; then
    find "$t" -name '*.sql' -print0 2>/dev/null \
      | xargs -0 -I{} shasum -a 256 {} >> "$NEW" 2>/dev/null || true
  else
    for f in $t; do
      [ -f "$f" ] && shasum -a 256 "$f" >> "$NEW" 2>/dev/null || true
    done
  fi
done
# sorted by PATH, not by hash — the file is read by humans when the gate fails
sort -k2 -o "$NEW" "$NEW"

if [ "$UPDATE" = "--update" ] || [ ! -f "$LOCK" ]; then
  cp "$NEW" "$LOCK"; echo "wrote $LOCK — commit it"; exit 0
fi

# Any line in the manifest that is not still present means a tracked file changed.
CHANGED="$(comm -23 <(sort -k2 "$LOCK") <(sort -k2 "$NEW") || true)"
if [ -n "$CHANGED" ]; then
  echo "FAIL: a previously-committed migration was modified:" >&2
  echo "$CHANGED" >&2
  exit 1
fi

# And any file not in the manifest was added without recording it, so a later edit
# would have nothing to compare against.
MISSING="$(comm -13 <(sort -k2 "$LOCK") <(sort -k2 "$NEW") || true)"
if [ -n "$MISSING" ]; then
  echo "FAIL: migrations are not recorded in $LOCK:" >&2
  echo "$MISSING" >&2
  echo "run: ./scripts/migrations/checksum-check.sh --update && git add $LOCK" >&2
  exit 1
fi

echo "checksums ok"
