#!/usr/bin/env bash
# Force the resolved SDK version into all generated SDK artifacts (package.json, pyproject.toml).
# Use this after Speakeasy generate so the pipeline uses the resolved version strictly.
# Usage: ./scripts/force-sdk-version.sh <VERSION>
set -euo pipefail

VERSION="${1:-}"
if [ -z "$VERSION" ]; then
  echo "Usage: $0 <VERSION>" >&2
  echo "Example: $0 0.0.28" >&2
  exit 1
fi

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
cd "$REPO_ROOT"

# TypeScript: package.json
if [ -f "api/typescript/package.json" ]; then
  jq --arg v "$VERSION" '.version = $v' api/typescript/package.json > api/typescript/package.json.tmp
  mv api/typescript/package.json.tmp api/typescript/package.json
  echo "Set api/typescript/package.json version -> $VERSION"
fi

# Python: pyproject.toml
if [ -f "api/python/pyproject.toml" ]; then
  sed -i.bak "s/^version = .*/version = \"$VERSION\"/" api/python/pyproject.toml
  rm -f api/python/pyproject.toml.bak
  echo "Set api/python/pyproject.toml version -> $VERSION"
fi

# MCP: package.json
if [ -f "api/mcp/package.json" ]; then
  jq --arg v "$VERSION" '.version = $v' api/mcp/package.json > api/mcp/package.json.tmp
  mv api/mcp/package.json.tmp api/mcp/package.json
  echo "Set api/mcp/package.json version -> $VERSION"
fi

echo "Forced version $VERSION into all SDK artifacts"
