#!/usr/bin/env bash
# Verifies generated SDK artifacts compile before pushing to client repos.
# Must be run after 'make sdk-all' has generated api/go, api/typescript, api/python.
set -euo pipefail

echo "=== Verifying Go SDK builds ==="
# go mod tidy updates go.sum for any deps introduced by custom files (e.g. ajson)
# that Speakeasy didn't see during generation.
(cd api/go && go mod tidy && go build ./...)
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
    -e 'github\.com/flexprice/go-sdk' \
    api/go api/typescript api/python api/mcp 2>/dev/null || true)

  # Also check documentation files for brand name residuals
  RESIDUAL_MD=$(grep -rn \
    --include="*.md" \
    -e 'FlexPrice' \
    -e 'Flexprice' \
    -e 'FLEXPRICE' \
    -e '@flexprice/sdk' \
    -e 'pip install flexprice' \
    -e 'from flexprice' \
    -e 'import flexprice' \
    api/go api/typescript api/python api/mcp 2>/dev/null || true)
  RESIDUAL="${RESIDUAL}${RESIDUAL_MD}"
  if [ -n "$RESIDUAL" ]; then
    echo "::error::Residual Flexprice branding found after apply-wl-custom-branding.sh:"
    echo "$RESIDUAL"
    exit 1
  fi
  echo "Branding residue check: OK"
fi

echo "=== All SDK builds verified ==="
