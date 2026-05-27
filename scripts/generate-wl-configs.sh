#!/usr/bin/env bash
# Applies white-label branding to .speakeasy/ config files in-place.
# Reads WL_* from environment variables (set before calling this script).
# Writes wl-server-url.yaml overlay, then runs scoped envsubst on all 5 templates.
set -euo pipefail

TEMPLATES="configs/white-label/templates"

# Verify templates directory exists
[ -d "$TEMPLATES" ] || { echo "ERROR: $TEMPLATES not found. Run from repo root."; exit 1; }

# Verify all required env vars are present (quick check — full validation is in validate-wl-config.sh)
: "${WL_SDK_CLASS_NAME:?WL_SDK_CLASS_NAME is required}"
: "${WL_API_BASE_URL:?WL_API_BASE_URL is required}"

# Step 1: Write the server URL overlay (used by workflow.yaml.tmpl)
cat > .speakeasy/overlays/wl-server-url.yaml <<EOF
overlay: 1.0.0
info:
  title: White-label server URL override
  version: 1.0.0
actions:
  - target: '$.servers[0]'
    update:
      url: ${WL_API_BASE_URL}
EOF
echo "Written: .speakeasy/overlays/wl-server-url.yaml (url: ${WL_API_BASE_URL})"

# Step 2: Scoped envsubst — only substitute WL_* variables
# This prevents envsubst from touching Speakeasy's own $npm_token/$pypi_token in workflow.yaml
WL_VARS='${WL_SDK_CLASS_NAME} ${WL_GO_MODULE_PATH} ${WL_GO_PACKAGE_NAME}
         ${WL_TS_PACKAGE_NAME} ${WL_PYTHON_PACKAGE_NAME} ${WL_MCP_PACKAGE_NAME}
         ${WL_AUTHOR_NAME} ${WL_API_BASE_URL}
         ${WL_GO_REPO} ${WL_PYTHON_REPO} ${WL_TS_REPO} ${WL_MCP_REPO}'

envsubst "$WL_VARS" < "$TEMPLATES/go.yaml.tmpl"         > .speakeasy/gen/go.yaml
envsubst "$WL_VARS" < "$TEMPLATES/typescript.yaml.tmpl" > .speakeasy/gen/typescript.yaml
envsubst "$WL_VARS" < "$TEMPLATES/python.yaml.tmpl"     > .speakeasy/gen/python.yaml
envsubst "$WL_VARS" < "$TEMPLATES/mcp.yaml.tmpl"        > .speakeasy/gen/mcp.yaml
envsubst "$WL_VARS" < "$TEMPLATES/workflow.yaml.tmpl"   > .speakeasy/workflow.yaml

echo "White-label configs applied:"
echo "  .speakeasy/gen/go.yaml         sdkClassName=$(grep 'sdkClassName' .speakeasy/gen/go.yaml | head -1 | awk '{print $2}')"
echo "  .speakeasy/gen/typescript.yaml sdkClassName=$(grep 'sdkClassName' .speakeasy/gen/typescript.yaml | head -1 | awk '{print $2}')"
echo "  .speakeasy/gen/python.yaml     sdkClassName=$(grep 'sdkClassName' .speakeasy/gen/python.yaml | head -1 | awk '{print $2}')"
echo "  .speakeasy/gen/mcp.yaml        sdkClassName=$(grep 'sdkClassName' .speakeasy/gen/mcp.yaml | head -1 | awk '{print $2}')"
echo "  .speakeasy/workflow.yaml       server url overlay added"
