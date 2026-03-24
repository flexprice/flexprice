#!/usr/bin/env bash
# Output the next SDK version (patch bump) without writing. Used by CI and Makefile
# so every generate uses a unique version and publish never fails.
# Usage: ./scripts/next-sdk-version.sh [major|minor|patch] [baseVersion]
# Default: patch. If baseVersion is omitted, read from .speakeasy/sdk-version.json.
# CI can pass baseVersion from registry (e.g. npm view flexprice-ts version) so the next
# version is always above the last published one.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
VERSION_FILE="$REPO_ROOT/.speakeasy/sdk-version.json"
BUMP="${1:-patch}"
CURRENT="${2:-}"

if [ -z "$CURRENT" ]; then
  CURRENT="0.0.1"
  if [ -f "$VERSION_FILE" ]; then
    V=$(jq -r .version "$VERSION_FILE")
    if [ -n "$V" ] && [ "$V" != "null" ]; then
      CURRENT="$V"
    fi
  fi
fi

# Parse semver (supports x.y.z and x.y.z-prerelease; strip suffix for bump)
if [[ "$CURRENT" =~ ^([0-9]+)\.([0-9]+)\.([0-9]+) ]]; then
  MAJOR="${BASH_REMATCH[1]}"
  MINOR="${BASH_REMATCH[2]}"
  PATCH="${BASH_REMATCH[3]}"
else
  MAJOR=0
  MINOR=0
  PATCH=1
fi

case "$BUMP" in
  major) MAJOR=$((MAJOR + 1)); MINOR=0; PATCH=0 ;;
  minor) MINOR=$((MINOR + 1)); PATCH=0 ;;
  patch) PATCH=$((PATCH + 1)) ;;
  *) echo "ERROR: Use major, minor, or patch" >&2; exit 1 ;;
esac

echo "${MAJOR}.${MINOR}.${PATCH}"
