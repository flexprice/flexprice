#!/usr/bin/env bash
# Verifies generated SDK artifacts compile before pushing to client repos.
# Must be run after 'make sdk-all' has generated api/go, api/typescript, api/python.
set -euo pipefail

echo "=== Verifying Go SDK builds ==="
(cd api/go && go build ./...)
echo "Go SDK: OK"

echo "=== Verifying TypeScript SDK builds ==="
(cd api/typescript && npm install --ignore-scripts && npm run build)
echo "TypeScript SDK: OK"

echo "=== Verifying Python SDK syntax ==="
find api/python/src -name "*.py" | xargs python3 -m py_compile
echo "Python SDK: OK"

echo "=== Checking for residual Flexprice branding in WL build ==="
# Only run this check in WL context (WL_SDK_CLASS_NAME will be set and differ from Flexprice)
if [ -n "${WL_SDK_CLASS_NAME:-}" ] && [ "${WL_SDK_CLASS_NAME}" != "Flexprice" ]; then
  RESIDUAL=$(grep -rn \
    --include="*.go" \
    --include="*.ts" \
    --include="*.py" \
    --include="*.toml" \
    --include="*.json" \
    -e "package flexprice" \
    -e '\*Flexprice\b' \
    -e '"@flexprice/mcp-server"' \
    -e 'for the FlexPrice API' \
    api/go api/typescript api/python api/mcp 2>/dev/null || true)
  if [ -n "$RESIDUAL" ]; then
    echo "::error::Residual Flexprice branding found after apply-wl-custom-branding.sh:"
    echo "$RESIDUAL"
    exit 1
  fi
  echo "Branding residue check: OK"
fi

echo "=== All SDK builds verified ==="
