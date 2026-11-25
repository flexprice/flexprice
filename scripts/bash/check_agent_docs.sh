#!/usr/bin/env bash
# Every repo path an AGENTS.md names in backticks must exist, so the map
# agents navigate by cannot drift from the tree.
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

missing_paths() {
  local doc path base
  while IFS= read -r doc; do
    # Backticked tokens that start with a top-level directory and contain a slash.
    grep -oE '`(cmd|internal|ent|docs|tools|scripts|api|migrations|\.github|\.agents|\.claude|integration-testing-suite)/[A-Za-z0-9_./*-]*`' "$doc" \
      | tr -d '`' | sort -u | while IFS= read -r path; do
        # A glob or "..." names a directory; check the part before the first one.
        base="${path%%\**}"; base="${base%%...*}"; base="${base%/}"
        [ -e "$base" ] || echo "$doc: \`$path\` does not exist"
      done || true
  done < <(git ls-files '*AGENTS.md' 'AGENTS.md')
}

missing="$(missing_paths)"
if [ -n "$missing" ]; then
  echo "$missing"
  echo "Fix the path or the document; agents follow these literally."
  exit 1
fi
echo "agent docs name real paths"
