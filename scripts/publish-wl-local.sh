#!/usr/bin/env bash
# ============================================================================
# publish-wl-local.sh — Full local run of the generate-wl-sdks.yml pipeline
#
# Mirrors every step of the CI workflow exactly:
#   validate → swagger → configs → generate → brand → verify →
#   version → push (Go/Python/TS/MCP) → GitHub release → npm → PyPI
#
# Usage:
#   export SPEAKEASY_API_KEY=...
#   export WL_SDK_CLASS_NAME=Tirdad
#   ... (all WL_* vars)
#   export WL_SDK_DEPLOY_GIT_TOKEN=github_pat_...
#   export WL_NPM_TOKEN=npm_...          # only if WL_PUBLISH_NPM=true
#   export WL_PYPI_TOKEN=pypi-...        # only if WL_PUBLISH_PYPI=true
#
#   bash scripts/publish-wl-local.sh 2.1.0
#   #                                ^^^^^ version tag (without v prefix)
# ============================================================================
set -euo pipefail

# ── Colours ────────────────────────────────────────────────────────────────
RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'
CYAN='\033[0;36m'; BOLD='\033[1m'; RESET='\033[0m'
step()  { echo -e "\n${CYAN}${BOLD}▶  $*${RESET}"; }
ok()    { echo -e "  ${GREEN}✓${RESET} $*"; }
warn()  { echo -e "  ${YELLOW}⚠${RESET}  $*"; }
fail()  { echo -e "\n${RED}✗ FAILED: $*${RESET}\n"; exit 1; }
hr()    { echo -e "${BOLD}$(printf '─%.0s' {1..72})${RESET}"; }

# ── Version arg ────────────────────────────────────────────────────────────
VERSION="${1:-}"
[ -z "$VERSION" ] && fail "Usage: bash scripts/publish-wl-local.sh <version>  e.g. 2.1.0"

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

# ── Cleanup .speakeasy on exit ──────────────────────────────────────────────
_cleanup() {
  echo ""
  step "Restoring .speakeasy/ to original state"
  git checkout -- .speakeasy/gen .speakeasy/workflow.yaml .speakeasy/overlays 2>/dev/null \
    && ok ".speakeasy/ restored" \
    || warn "Run manually: git checkout -- .speakeasy/"
}
trap _cleanup EXIT

hr
echo -e "${BOLD}  White-Label SDK Full Local Publish${RESET}"
echo -e "  Version : ${VERSION}"
echo -e "  Client  : ${WL_SDK_CLASS_NAME:-NOT SET}"
hr

# ── Check required vars ────────────────────────────────────────────────────
step "Checking required environment variables"
MISSING=()
for VAR in SPEAKEASY_API_KEY WL_SDK_CLASS_NAME WL_GO_MODULE_PATH WL_GO_PACKAGE_NAME \
           WL_TS_PACKAGE_NAME WL_PYTHON_PACKAGE_NAME WL_MCP_PACKAGE_NAME \
           WL_AUTHOR_NAME WL_API_BASE_URL \
           WL_GO_REPO WL_PYTHON_REPO WL_TS_REPO WL_MCP_REPO \
           WL_SDK_DEPLOY_GIT_TOKEN; do
  [ -z "${!VAR:-}" ] && MISSING+=("$VAR")
done

export WL_PUBLISH_NPM="${WL_PUBLISH_NPM:-false}"
export WL_PUBLISH_PYPI="${WL_PUBLISH_PYPI:-false}"

if [ "${WL_PUBLISH_NPM}" = "true" ] && [ -z "${WL_NPM_TOKEN:-}" ]; then
  MISSING+=("WL_NPM_TOKEN (required when WL_PUBLISH_NPM=true)")
fi
if [ "${WL_PUBLISH_PYPI}" = "true" ] && [ -z "${WL_PYPI_TOKEN:-}" ]; then
  MISSING+=("WL_PYPI_TOKEN (required when WL_PUBLISH_PYPI=true)")
fi

if [ ${#MISSING[@]} -gt 0 ]; then
  fail "Missing required variables:\n$(printf '    %s\n' "${MISSING[@]}")"
fi
ok "All required variables set"

# ── Verify client repo access ───────────────────────────────────────────────
step "Verifying client repo accessibility"
FAIL=0
for VAR in WL_GO_REPO WL_PYTHON_REPO WL_TS_REPO WL_MCP_REPO; do
  REPO="${!VAR}"
  HTTP=$(curl -s -o /dev/null -w "%{http_code}" \
    -H "Authorization: token $WL_SDK_DEPLOY_GIT_TOKEN" \
    "https://api.github.com/repos/$REPO")
  if [ "$HTTP" = "200" ]; then
    ok "$REPO"
  else
    warn "$REPO — HTTP $HTTP (repo missing or token has no access)"
    FAIL=1
  fi
done
[ "$FAIL" -eq 0 ] || fail "One or more repos are inaccessible. Check WL_SDK_DEPLOY_GIT_TOKEN permissions."

# ── Validate white-label config ────────────────────────────────────────────
step "Validating white-label config"
WL_SDK_DEPLOY_GIT_TOKEN="$WL_SDK_DEPLOY_GIT_TOKEN" bash scripts/validate-wl-config.sh
ok "Config valid"

# ── Generate OpenAPI spec ──────────────────────────────────────────────────
step "Generating OpenAPI spec (make swagger)"
make swagger
[ -f "docs/swagger/swagger-3-0.json" ] || fail "docs/swagger/swagger-3-0.json not found after make swagger"
ok "OpenAPI spec: $(wc -c < docs/swagger/swagger-3-0.json | tr -d ' ') bytes"

# ── Apply white-label configs ──────────────────────────────────────────────
step "Applying white-label configs to .speakeasy/"
bash scripts/generate-wl-configs.sh
ok "sdkClassName in go.yaml:         $(grep 'sdkClassName' .speakeasy/gen/go.yaml | head -1 | awk '{print $2}')"
ok "sdkClassName in typescript.yaml: $(grep 'sdkClassName' .speakeasy/gen/typescript.yaml | head -1 | awk '{print $2}')"
ok "sdkClassName in python.yaml:     $(grep 'sdkClassName' .speakeasy/gen/python.yaml | head -1 | awk '{print $2}')"
ok "moduleName in python.yaml:       $(grep 'moduleName' .speakeasy/gen/python.yaml | head -1 | awk '{print $2}')"
ok "sdkClassName in mcp.yaml:        $(grep 'sdkClassName' .speakeasy/gen/mcp.yaml | head -1 | awk '{print $2}')"

# ── Generate SDKs ──────────────────────────────────────────────────────────
step "Generating SDKs via Speakeasy (VERSION=${VERSION}) — takes 2-5 min"
make sdk-all VERSION="${VERSION}"
ok "SDK generation complete"

# ── Apply white-label branding ─────────────────────────────────────────────
step "Applying white-label branding to custom files"
bash scripts/apply-wl-custom-branding.sh
ok "Custom branding applied"

# ── Verify SDK builds ──────────────────────────────────────────────────────
step "Verifying SDK builds + residual branding check"
bash scripts/verify-sdk-builds.sh
ok "All builds verified, no residual Flexprice branding"

# ── Apply version to all artifacts ────────────────────────────────────────
step "Applying resolved version ${VERSION} to all artifacts"
bash scripts/force-sdk-version.sh "${VERSION}"
ok "Version ${VERSION} applied"

# ── Helper: push one SDK to its GitHub repo ────────────────────────────────
push_sdk() {
  local LANG="$1" REPO="$2" SRC_DIR="$3" TMP_DIR="$4"

  step "Pushing ${LANG} SDK → ${REPO}"

  git clone "https://x-access-token:${WL_SDK_DEPLOY_GIT_TOKEN}@github.com/${REPO}.git" "$TMP_DIR"
  cd "$TMP_DIR"
  find . -mindepth 1 -maxdepth 1 ! -name '.git' -exec rm -rf {} +
  cp -r "${REPO_ROOT}/${SRC_DIR}/." .
  [ -f "${REPO_ROOT}/LICENSE" ] && cp "${REPO_ROOT}/LICENSE" .

  git config user.name "Local Publish Bot"
  git config user.email "bot@local"

  if [ -n "$(git status --porcelain)" ]; then
    git add .
    git commit -m "Release ${LANG} SDK v${VERSION}"
    git tag -a "v${VERSION}" -m "${LANG} SDK v${VERSION}" 2>/dev/null || true
    git push origin main
    git push origin "v${VERSION}" 2>/dev/null || true
    ok "Pushed to ${REPO} @ v${VERSION}"
  else
    ok "No changes to push for ${REPO}"
  fi
  cd "$REPO_ROOT"
}

push_sdk "Go"         "$WL_GO_REPO"     "api/go"         "/tmp/wl-go-sdk"
push_sdk "Python"     "$WL_PYTHON_REPO" "api/python"     "/tmp/wl-python-sdk"
push_sdk "TypeScript" "$WL_TS_REPO"     "api/typescript" "/tmp/wl-ts-sdk"
push_sdk "MCP"        "$WL_MCP_REPO"    "api/mcp"        "/tmp/wl-mcp"

# ── Create GitHub release in Go repo ──────────────────────────────────────
step "Creating GitHub release in Go repo (${WL_GO_REPO})"
TAG="v${VERSION}"
GH_TOKEN="$WL_SDK_DEPLOY_GIT_TOKEN" gh release view "$TAG" --repo "${WL_GO_REPO}" \
  > /dev/null 2>&1 && \
  warn "Release $TAG already exists in ${WL_GO_REPO} — skipping" || \
  GH_TOKEN="$WL_SDK_DEPLOY_GIT_TOKEN" gh release create "$TAG" \
    --repo "${WL_GO_REPO}" \
    --title "Go SDK ${TAG}" \
    --notes "Automated release for Go SDK ${TAG}."
ok "GitHub release done"

# ── Publish TypeScript SDK to npm ─────────────────────────────────────────
if [ "${WL_PUBLISH_NPM}" = "true" ]; then
  step "Publishing TypeScript SDK to npm (${WL_TS_PACKAGE_NAME})"
  REGISTRY="${WL_NPM_REGISTRY:-https://registry.npmjs.org}"
  cd "${REPO_ROOT}/api/typescript"
  echo "//${REGISTRY#https://}/:_authToken=${WL_NPM_TOKEN}" >> .npmrc
  npm install --ignore-scripts
  npm run build
  npm publish --access public --registry "$REGISTRY" \
    || warn "TypeScript SDK version already published — skipping"
  rm -f .npmrc
  cd "$REPO_ROOT"
  ok "TypeScript SDK published → ${WL_TS_PACKAGE_NAME}"

  step "Publishing MCP to npm (${WL_MCP_PACKAGE_NAME})"
  cd "${REPO_ROOT}/api/mcp"
  echo "//${REGISTRY#https://}/:_authToken=${WL_NPM_TOKEN}" >> .npmrc
  npm install --ignore-scripts
  npm run build
  npm publish --access public --registry "$REGISTRY" \
    || warn "MCP version already published — skipping"
  rm -f .npmrc
  cd "$REPO_ROOT"
  ok "MCP published → ${WL_MCP_PACKAGE_NAME}"
else
  warn "WL_PUBLISH_NPM=false — skipping npm publish"
fi

# ── Publish Python SDK to PyPI ────────────────────────────────────────────
if [ "${WL_PUBLISH_PYPI}" = "true" ]; then
  step "Publishing Python SDK to PyPI (${WL_PYTHON_PACKAGE_NAME})"
  cd "${REPO_ROOT}/api/python"
  pip install --upgrade pip build twine --quiet
  python -m build
  twine check dist/*
  TWINE_USERNAME=__token__ TWINE_PASSWORD="${WL_PYPI_TOKEN}" \
    twine upload --skip-existing dist/*
  cd "$REPO_ROOT"
  ok "Python SDK published → ${WL_PYTHON_PACKAGE_NAME}"
else
  warn "WL_PUBLISH_PYPI=false — skipping PyPI publish"
fi

# ── Summary ────────────────────────────────────────────────────────────────
echo ""
hr
echo -e "${BOLD}  Publish Complete — v${VERSION}${RESET}"
hr
echo ""
echo -e "  ${BOLD}GitHub repos updated:${RESET}"
echo "    Go:         https://github.com/${WL_GO_REPO}"
echo "    TypeScript: https://github.com/${WL_TS_REPO}"
echo "    Python:     https://github.com/${WL_PYTHON_REPO}"
echo "    MCP:        https://github.com/${WL_MCP_REPO}"

if [ "${WL_PUBLISH_NPM}" = "true" ]; then
  echo ""
  echo -e "  ${BOLD}npm packages:${RESET}"
  echo "    https://www.npmjs.com/package/${WL_TS_PACKAGE_NAME}"
  echo "    https://www.npmjs.com/package/${WL_MCP_PACKAGE_NAME}"
fi

if [ "${WL_PUBLISH_PYPI}" = "true" ]; then
  echo ""
  echo -e "  ${BOLD}PyPI:${RESET}"
  echo "    https://pypi.org/project/${WL_PYTHON_PACKAGE_NAME}"
fi
echo ""
