#!/usr/bin/env bash
# dbmate records only the version, never the file contents, so editing a migration
# that already ran on a client database is silent. This adds the missing guarantee.
set -euo pipefail
DIR="${1:-migrations/versioned}"
LOCK="${2:-migrations/versioned/.hashes}"
NEW="$(mktemp)"
# Same GNU/BSD empty-input divergence as order-check.sh.
find "$DIR" -name '*.sql' -print0 2>/dev/null | sort -z \
  | xargs -0 -I{} shasum -a 256 {} > "$NEW" 2>/dev/null || true
if [ ! -f "$LOCK" ]; then cp "$NEW" "$LOCK"; echo "seeded $LOCK"; exit 0; fi
CHANGED="$(comm -23 <(sort "$LOCK") <(sort "$NEW") || true)"
if [ -z "$CHANGED" ]; then cp "$NEW" "$LOCK"; echo "checksums ok"; exit 0; fi
echo "FAIL: a previously-committed migration was modified:" >&2
echo "$CHANGED" >&2
exit 1
