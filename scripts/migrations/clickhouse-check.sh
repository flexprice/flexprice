#!/usr/bin/env bash
# ClickHouse rejects multi-statement request bodies, so each migration file must
# contain exactly one statement. Enforce at PR time rather than at deploy time.
set -euo pipefail
DIRS=("$@")
[ ${#DIRS[@]} -gt 0 ] || DIRS=(migrations/versioned/clickhouse migrations/versioned/clickhouse/replicated)
rc=0
for DIR in "${DIRS[@]}"; do
  for f in "$DIR"/*.sql; do
    [ -e "$f" ] || continue
    body="$(sed -n '/^-- migrate:up/,/^-- migrate:down/p' "$f" | grep -v '^--' | tr -d '\n')"
    n="$(printf '%s' "$body" | tr -cd ';' | wc -c | tr -d ' ')"
    if [ "$n" -gt 1 ]; then
      echo "FAIL: $f has $((n)) statements; ClickHouse allows one per request" >&2
      rc=1
    fi
  done
done
[ $rc -eq 0 ] && echo "clickhouse: one statement per file, ok"
exit $rc
