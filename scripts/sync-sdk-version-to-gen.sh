#!/usr/bin/env bash
# Sync the SDK version into central gen.yaml files (.speakeasy/gen/*.yaml) and
# .speakeasy/sdk-version.json so the next run starts from the bumped version.
# Run after 'make sdk-all'. Source of truth for gen is .speakeasy/gen/.
# Usage: ./scripts/sync-sdk-version-to-gen.sh <VERSION>
set -euo pipefail

VERSION="${1:-}"
if [ -z "$VERSION" ]; then
  echo "Usage: $0 <VERSION>" >&2
  echo "Example: $0 0.0.7" >&2
  exit 1
fi

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
cd "$REPO_ROOT"

# Update central gen.yaml files: replace the first "  version: ..." in each.
for path in .speakeasy/gen/go.yaml .speakeasy/gen/typescript.yaml .speakeasy/gen/python.yaml .speakeasy/gen/mcp.yaml; do
  if [ -f "$path" ]; then
    if sed -i.bak "s/^  version: .*$/  version: $VERSION/" "$path"; then
      rm -f "${path}.bak"
      echo "Updated $path -> $VERSION"
    fi
  fi
done

# Update .speakeasy/sdk-version.json
VERSION_JSON="$REPO_ROOT/.speakeasy/sdk-version.json"
if [ -f "$VERSION_JSON" ]; then
  jq --arg v "$VERSION" '.version = $v' "$VERSION_JSON" > "${VERSION_JSON}.tmp" && mv "${VERSION_JSON}.tmp" "$VERSION_JSON"
  echo "Updated $VERSION_JSON -> $VERSION"
fi

echo "Synced version $VERSION to all gen.yaml and sdk-version.json"
