#!/usr/bin/env bash
# Copy central gen config (.speakeasy/gen/*.yaml) into api/<lang>/.speakeasy/gen.yaml
# so Speakeasy CLI finds config in each target output dir. Run before speakeasy run.
# Usage: ./scripts/sync-gen-to-output.sh
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
cd "$REPO_ROOT"

for lang in go typescript python mcp; do
  src=".speakeasy/gen/${lang}.yaml"
  dest_dir="api/${lang}/.speakeasy"
  if [ -f "$src" ]; then
    mkdir -p "$dest_dir"
    cp "$src" "$dest_dir/gen.yaml"
    echo "Copied $src -> $dest_dir/gen.yaml"
  fi
done

echo "Synced central gen to api/*/.speakeasy/gen.yaml"
