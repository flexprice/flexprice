#!/usr/bin/env bash
# Migrations apply in filename order. A file merged from a parallel branch with an
# older timestamp than one already applied would silently never run on databases
# that are past it. Reject at PR time.
set -euo pipefail
DIR="${1:-migrations/versioned/postgres}"
BASE="${2:-origin/develop}"
# No xargs: GNU xargs runs its command even on empty input (BSD does not), so
# `xargs -n1 basename` with no committed migrations exits 123 and set -e kills the
# script before the guard below can return 0. sed strips the directory portably
# and is a no-op when the input is empty.
newest="$(git ls-tree -r --name-only "$BASE" -- "$DIR" 2>/dev/null \
          | sed 's#.*/##' | cut -d_ -f1 | sort -n | tail -1 || true)"
[ -n "$newest" ] || { echo "no committed migrations yet"; exit 0; }
# Versions already present on the base branch, whatever they are named there. A
# rename at an unchanged version is not an insertion, and flagging it would block
# any tidy-up of an existing migration's filename.
EXISTING="$(git ls-tree -r --name-only "$BASE" -- "$DIR" 2>/dev/null \
            | sed 's#.*/##' | cut -d_ -f1 || true)"

rc=0
for f in $(git diff --name-only --diff-filter=A "$BASE"...HEAD -- "$DIR"); do
  v="$(basename "$f" | cut -d_ -f1)"
  if printf '%s\n' "$EXISTING" | grep -qx "$v"; then continue; fi
  if [ "$v" -lt "$newest" ]; then
    echo "FAIL: $f ($v) predates already-committed $newest" >&2; rc=1
  fi
done
[ $rc -eq 0 ] && echo "ordering ok"
exit $rc
