#!/usr/bin/env bash
# dbmate records only the version, never the file contents, so editing a migration
# that already ran on a client database is silent. This adds the missing guarantee.
#
# The manifest is authoritative and is NOT rewritten here: if it were, CI would
# quietly accept whatever it found, and a migration added in one PR would never be
# recorded for the next one to compare against. Regenerate deliberately with
#   ./checksum-check.sh <dir> <lock> --update
set -euo pipefail
DIR="${1:-migrations/versioned}"
LOCK="${2:-migrations/versioned/.hashes}"
UPDATE="${3:-}"

NEW="$(mktemp)"
find "$DIR" -name '*.sql' -print0 2>/dev/null | sort -z \
  | xargs -0 -I{} shasum -a 256 {} > "$NEW" 2>/dev/null || true

if [ "$UPDATE" = "--update" ] || [ ! -f "$LOCK" ]; then
  cp "$NEW" "$LOCK"; echo "wrote $LOCK — commit it"; exit 0
fi

# Any line in the manifest that is not still present means a tracked file changed.
CHANGED="$(comm -23 <(sort "$LOCK") <(sort "$NEW") || true)"
if [ -n "$CHANGED" ]; then
  echo "FAIL: a previously-committed migration was modified:" >&2
  echo "$CHANGED" >&2
  exit 1
fi

# And any file not in the manifest was added without recording it, so a later edit
# would have nothing to compare against.
MISSING="$(comm -13 <(sort "$LOCK") <(sort "$NEW") || true)"
if [ -n "$MISSING" ]; then
  echo "FAIL: migrations are not recorded in $LOCK:" >&2
  echo "$MISSING" >&2
  echo "run: ./scripts/migrations/checksum-check.sh $DIR $LOCK --update && git add $LOCK" >&2
  exit 1
fi

echo "checksums ok"
