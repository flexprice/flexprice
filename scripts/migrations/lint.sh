#!/usr/bin/env bash
# Lock-safety lint for migrations added by this branch.
#
# Only ADDED files are linted. The baseline is a 3,700-line pg_dump of production
# and reports 1,363 findings — all describing history rather than a decision anyone
# is making now. Linting it would train everyone to ignore the output.
#
#   ./lint.sh [base-ref]     default: origin/develop
set -euo pipefail
BASE="${1:-origin/develop}"
CONF="${SQUAWK_CONFIG:-.squawk.toml}"

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
rc=0
for f in $FILES; do
  [ -f "$f" ] || continue
  squawk -c "$CONF" "$f" || rc=1
done

if [ $rc -eq 0 ]; then
  echo "lint: OK"
else
  echo >&2
  echo "lint: findings above. Each links to an explanation of the lock it takes." >&2
  echo "Rules deliberately disabled for this repo are listed in .squawk.toml." >&2
fi
exit $rc
