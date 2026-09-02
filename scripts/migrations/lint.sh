#!/usr/bin/env bash
# Lock-safety lint for migrations added by this branch.
#
# Only ADDED files are linted. The baseline is a 3,700-line pg_dump of production
# and reports 1,363 findings — all describing history rather than a decision anyone
# is making now. Linting it would train everyone to ignore the output.
#
# Two tiers. Most findings are advice and print without failing. A short list
# BLOCKS, because each entry maps to a failure that already happened here:
#
#   adding-required-field
#     ADD COLUMN ... NOT NULL with no DEFAULT. Postgres rejects it outright on
#     any table that has rows -- "contains null values" -- but succeeds on an
#     empty one, so the replay-from-zero gate passes it and every real database
#     fails. Shipped to main on 2026-08-31 and was caught by hand.
#
#   ban-concurrent-index-creation-in-transaction
#     A CONCURRENTLY statement that is not alone in its file. Anything else in
#     the body gives Postgres an implicit transaction and the statement is
#     rejected at deploy time. Requires --assume-in-transaction to detect.
#
# Everything else stays advisory on purpose: a wall of blocking style rules gets
# switched off within a month, and then the two that matter go with it.
#
#   ./lint.sh [base-ref]     default: origin/develop
set -euo pipefail
BASE="${1:-origin/develop}"
CONF="${SQUAWK_CONFIG:-.squawk.toml}"
BLOCKING="adding-required-field ban-concurrent-index-creation-in-transaction"

command -v squawk >/dev/null 2>&1 || {
  echo "squawk not installed — https://squawkhq.com/docs/installation" >&2
  echo "  brew: not packaged. Download the binary for your platform from" >&2
  echo "  https://github.com/sbdchd/squawk/releases" >&2
  exit 127
}

FILES="$(git diff --name-only --diff-filter=A "$BASE"...HEAD -- \
         'migrations/versioned/postgres/*.sql' 2>/dev/null || true)"

if [ -z "$FILES" ]; then
  echo "lint: no new migrations to check"
  exit 0
fi

echo "lint: checking $(echo "$FILES" | wc -l | tr -d ' ') new migration(s) against $BASE"

# --assume-in-transaction: without it squawk cannot know the statement will be
# wrapped, and ban-concurrent-index-creation-in-transaction never fires. It is
# correct here because dbmate sends a file body as one string, so any migration
# with more than one statement runs inside Postgres' implicit transaction.
OUT="$(mktemp)"; trap 'rm -f "$OUT"' EXIT
advisory=0
for f in $FILES; do
  [ -f "$f" ] || continue
  squawk -c "$CONF" --assume-in-transaction "$f" >>"$OUT" 2>&1 || advisory=1
done
cat "$OUT"

# Strip ANSI and hyperlink escapes before matching, or the rule name never
# matches and a blocking finding is reported as advisory.
CLEAN="$(sed 's/\x1b\[[0-9;]*m//g; s/\x1b]8;;[^\]*\\//g' "$OUT")"
blocked=""
for r in $BLOCKING; do
  echo "$CLEAN" | grep -q "\[$r\]" && blocked="$blocked $r"
done

if [ -n "$blocked" ]; then
  echo >&2
  echo "lint: BLOCKING findings —$blocked" >&2
  echo "  These are not style preferences. Each one fails at deploy time against a" >&2
  echo "  database that has data, while passing CI's replay against empty tables." >&2
  exit 1
fi

if [ "$advisory" -eq 0 ]; then
  echo "lint: OK"
else
  echo
  echo "lint: advisory findings above — not blocking. Each links to an explanation"
  echo "of the lock it takes. Rules disabled for this repo are in .squawk.toml."
fi
exit 0
