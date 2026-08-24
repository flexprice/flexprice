#!/usr/bin/env bash
# dbmate records only the version, never the file contents, so editing a migration
# that already ran on a client database is silent. This adds the missing guarantee.
set -euo pipefail
DIR="${1:-migrations/versioned}"
LOCK="${2:-migrations/versioned/.hashes}"
NEW="$(mktemp)"
find "$DIR" -name '*.sql' | sort | xargs shasum -a 256 > "$NEW"
if [ ! -f "$LOCK" ]; then cp "$NEW" "$LOCK"; echo "seeded $LOCK"; exit 0; fi
CHANGED="$(comm -23 <(sort "$LOCK") <(sort "$NEW") || true)"
if [ -z "$CHANGED" ]; then cp "$NEW" "$LOCK"; echo "checksums ok"; exit 0; fi
echo "FAIL: a previously-committed migration was modified:" >&2
echo "$CHANGED" >&2
exit 1
