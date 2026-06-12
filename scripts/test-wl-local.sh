#!/usr/bin/env bash
# ============================================================================
# test-wl-local.sh — Local white-label SDK generation test harness
#
# Runs the full WL pipeline (validate → swagger → configs → generate →
# brand → verify) and prints a spot-check of each SDK's identity fields.
# .speakeasy/ is restored to its original state on exit (pass or fail).
#
# Usage:
#   export SPEAKEASY_API_KEY=<your key>
#   export WL_SDK_CLASS_NAME=Acme
#   export WL_GO_MODULE_PATH=github.com/acme/go-sdk
#   ... (see REQUIRED VARS section below)
#   bash scripts/test-wl-local.sh
#
# Optional overrides:
#   VERSION=2.1.0 bash scripts/test-wl-local.sh   # default: 2.0.0-local
# ============================================================================
set -euo pipefail

# ── Colours ────────────────────────────────────────────────────────────────
RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'
CYAN='\033[0;36m'; BOLD='\033[1m'; RESET='\033[0m'

step()  { echo -e "\n${CYAN}${BOLD}▶ $*${RESET}"; }
ok()    { echo -e "  ${GREEN}✓${RESET} $*"; }
warn()  { echo -e "  ${YELLOW}⚠${RESET}  $*"; }
fail()  { echo -e "  ${RED}✗${RESET} $*"; }
hr()    { echo -e "${BOLD}$(printf '─%.0s' {1..72})${RESET}"; }

VERSION="${VERSION:-2.0.0-local}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
cd "$REPO_ROOT"

# ── Restore .speakeasy on exit ──────────────────────────────────────────────
_cleanup() {
  echo ""
  step "Restoring .speakeasy/ to original state"
  git checkout -- .speakeasy/gen .speakeasy/workflow.yaml .speakeasy/overlays 2>/dev/null \
    && ok ".speakeasy/ restored" \
    || warn ".speakeasy/ restore failed — run: git checkout -- .speakeasy/"
}
trap _cleanup EXIT

hr
echo -e "${BOLD}  White-Label SDK Local Test Harness${RESET}"
echo -e "  Version: ${VERSION}"
hr

# ── Step 1: Check required env vars ────────────────────────────────────────
step "Checking required environment variables"

MISSING=()
for VAR in \
  WL_SDK_CLASS_NAME \
  WL_GO_MODULE_PATH \
  WL_GO_PACKAGE_NAME \
  WL_TS_PACKAGE_NAME \
  WL_PYTHON_PACKAGE_NAME \
  WL_MCP_PACKAGE_NAME \
  WL_AUTHOR_NAME \
  WL_API_BASE_URL \
  WL_GO_REPO \
  WL_PYTHON_REPO \
  WL_TS_REPO \
  WL_MCP_REPO \
  SPEAKEASY_API_KEY; do
  if [ -z "${!VAR:-}" ]; then
    MISSING+=("$VAR")
  fi
done

# WL_SDK_DEPLOY_GIT_TOKEN: required by validate-wl-config.sh but not used locally
export WL_SDK_DEPLOY_GIT_TOKEN="${WL_SDK_DEPLOY_GIT_TOKEN:-local-test-dummy-token}"
export WL_PUBLISH_NPM="${WL_PUBLISH_NPM:-false}"
export WL_PUBLISH_PYPI="${WL_PUBLISH_PYPI:-false}"

if [ ${#MISSING[@]} -gt 0 ]; then
  fail "Missing required environment variables:"
  for VAR in "${MISSING[@]}"; do
    echo "    export ${VAR}=<value>"
  done
  echo ""
  echo "  Example setup:"
  cat <<'EOF'
    export SPEAKEASY_API_KEY=your_key_here
    export WL_SDK_CLASS_NAME=Acme
    export WL_GO_MODULE_PATH=github.com/acme/go-sdk
    export WL_GO_PACKAGE_NAME=acmesdk
    export WL_TS_PACKAGE_NAME=@acme/sdk
    export WL_PYTHON_PACKAGE_NAME=acme-sdk
    export WL_MCP_PACKAGE_NAME=@acme/mcp
    export WL_AUTHOR_NAME="Acme Corp"
    export WL_API_BASE_URL=https://api.acme.com
    export WL_GO_REPO=acme-org/go-sdk
    export WL_PYTHON_REPO=acme-org/python-sdk
    export WL_TS_REPO=acme-org/typescript-sdk
    export WL_MCP_REPO=acme-org/mcp-sdk
EOF
  exit 1
fi

ok "SDK class:   ${WL_SDK_CLASS_NAME}"
ok "Go module:   ${WL_GO_MODULE_PATH}"
ok "TS package:  ${WL_TS_PACKAGE_NAME}"
ok "MCP package: ${WL_MCP_PACKAGE_NAME}"
ok "API base:    ${WL_API_BASE_URL}"

# ── Step 2: Check prerequisites ─────────────────────────────────────────────
step "Checking tool prerequisites"

PREREQ_FAIL=0
for tool in go node python3 jq speakeasy; do
  if command -v "$tool" &>/dev/null; then
    ok "$tool  ($(${tool} --version 2>&1 | head -1))"
  else
    fail "$tool not found — install it first"
    PREREQ_FAIL=1
  fi
done
[ "$PREREQ_FAIL" -eq 0 ] || exit 1

# ── Step 3: Validate white-label config ────────────────────────────────────
step "Validating white-label config"
bash scripts/validate-wl-config.sh
ok "Config valid"

# ── Step 4: Generate OpenAPI spec ──────────────────────────────────────────
step "Generating OpenAPI spec (make swagger)"
make swagger
SPEC="docs/swagger/swagger-3-0.json"
[ -f "$SPEC" ] || { fail "$SPEC not found"; exit 1; }
ok "OpenAPI spec: $(wc -c < "$SPEC" | tr -d ' ') bytes"

# ── Step 5: Apply white-label configs to .speakeasy/ ───────────────────────
step "Applying white-label configs to .speakeasy/"
bash scripts/generate-wl-configs.sh
ok "go.yaml:         sdkClassName=$(grep 'sdkClassName' .speakeasy/gen/go.yaml | head -1 | awk '{print $2}')"
ok "typescript.yaml: sdkClassName=$(grep 'sdkClassName' .speakeasy/gen/typescript.yaml | head -1 | awk '{print $2}')"
ok "python.yaml:     sdkClassName=$(grep 'sdkClassName' .speakeasy/gen/python.yaml | head -1 | awk '{print $2}')"
ok "python.yaml:     moduleName=$(grep 'moduleName' .speakeasy/gen/python.yaml | head -1 | awk '{print $2}')"
ok "mcp.yaml:        sdkClassName=$(grep 'sdkClassName' .speakeasy/gen/mcp.yaml | head -1 | awk '{print $2}')"
ok "server url:      $(grep 'url:' .speakeasy/overlays/wl-server-url.yaml | awk '{print $2}')"

# ── Step 6: Generate SDKs via Speakeasy ────────────────────────────────────
# NOTE: VERSION must be 2.x to get 'module .../v2' in go.mod, which is
# required so internal imports (github.com/.../v2/models/...) resolve correctly.
step "Generating SDKs via Speakeasy (VERSION=${VERSION}) — this takes 2-5 min"
make sdk-all VERSION="${VERSION}"
ok "SDK generation complete"

# ── Step 7: Apply white-label branding to custom files ─────────────────────
step "Applying white-label branding to custom files"
bash scripts/apply-wl-custom-branding.sh
ok "Custom branding applied"

# ── Step 8: Verify all SDK builds ──────────────────────────────────────────
step "Verifying SDK builds + residual branding check"
bash scripts/verify-sdk-builds.sh
ok "All SDK builds verified"

# ── Spot-check: print key identity fields ──────────────────────────────────
echo ""
hr
echo -e "${BOLD}  SDK Identity Spot-Check${RESET}"
hr

echo ""
echo -e "  ${BOLD}Go${RESET}"
echo "    go.mod module:  $(head -1 api/go/go.mod)"
echo "    package decl:   $(grep '^package ' api/go/sdk.go 2>/dev/null | head -1 || echo 'n/a')"

echo ""
echo -e "  ${BOLD}TypeScript${RESET}"
echo "    package name:   $(jq -r '.name' api/typescript/package.json 2>/dev/null || echo 'n/a')"
echo "    version:        $(jq -r '.version' api/typescript/package.json 2>/dev/null || echo 'n/a')"

echo ""
echo -e "  ${BOLD}Python${RESET}"
echo "    package name:   $(grep '^name' api/python/pyproject.toml 2>/dev/null | head -1 || echo 'n/a')"
echo "    module name:    $(grep 'moduleName\|module_name' api/python/pyproject.toml 2>/dev/null | head -1 || echo 'n/a')"

echo ""
echo -e "  ${BOLD}MCP${RESET}"
echo "    package name:   $(jq -r '.name' api/mcp/package.json 2>/dev/null || echo 'n/a')"

echo ""
hr
echo -e "  ${GREEN}${BOLD}✓ Pipeline complete — all checks passed${RESET}"
hr
echo ""
echo "  Generated SDKs are in: api/go/  api/typescript/  api/python/  api/mcp/"
echo "  (.speakeasy/ will be restored to original state on exit)"
echo ""
