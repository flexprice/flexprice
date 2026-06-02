#!/usr/bin/env bash
# Post-processes white-label SDK artifacts after 'make sdk-all'.
#
# 'make sdk-all' runs merge-custom (copies api/custom/<lang>/ → api/<lang>/)
# and fix-mcp-package-name (hardcodes @flexprice/mcp-server). Those steps
# embed hardcoded Flexprice names that this script replaces with WL values.
#
# Run order in CI:
#   generate-wl-configs.sh → make sdk-all → apply-wl-custom-branding.sh → verify-sdk-builds.sh
#
# Reads from environment (set by the 'Parse and export client config' step):
#   WL_SDK_CLASS_NAME, WL_GO_MODULE_PATH, WL_GO_PACKAGE_NAME,
#   WL_TS_PACKAGE_NAME, WL_MCP_PACKAGE_NAME
set -euo pipefail

# Portable sed -i: GNU sed uses -i alone; BSD/macOS sed requires -i ''
if sed --version 2>/dev/null | grep -q GNU; then
  _sed_i() { sed -i "$@"; }
else
  _sed_i() { sed -i '' "$@"; }
fi

: "${WL_SDK_CLASS_NAME:?WL_SDK_CLASS_NAME is required}"
: "${WL_GO_MODULE_PATH:?WL_GO_MODULE_PATH is required}"
: "${WL_GO_PACKAGE_NAME:?WL_GO_PACKAGE_NAME is required}"
: "${WL_TS_PACKAGE_NAME:?WL_TS_PACKAGE_NAME is required}"
: "${WL_MCP_PACKAGE_NAME:?WL_MCP_PACKAGE_NAME is required}"

# Guard: skip entirely for standard (non-WL) builds
if [ "${WL_SDK_CLASS_NAME}" = "Flexprice" ]; then
  echo "WL_SDK_CLASS_NAME is 'Flexprice' — standard build, skipping white-label branding."
  exit 0
fi

echo "=== Applying white-label branding to merged custom files ==="
echo "    SDK class:    ${WL_SDK_CLASS_NAME}"
echo "    Go module:    ${WL_GO_MODULE_PATH}"
echo "    Go package:   ${WL_GO_PACKAGE_NAME}"
echo "    TS package:   ${WL_TS_PACKAGE_NAME}"
echo "    MCP package:  ${WL_MCP_PACKAGE_NAME}"

# ─── Go: api/go/**  ───────────────────────────────────────────────────────────
echo ""
echo "--- Go SDK ---"

GO_DIR="api/go"
if [ ! -d "$GO_DIR" ]; then
  echo "  WARNING: $GO_DIR not found — was 'make sdk-all' run first?"
else
  # 1. Fix package declarations: 'package flexprice' → 'package <wl>'
  #    Only custom-merged files will still have 'package flexprice' after
  #    Speakeasy generates with sdkPackageName = WL_GO_PACKAGE_NAME.
  while IFS= read -r -d '' f; do
    if grep -q "^package flexprice$" "$f" 2>/dev/null; then
      _sed_i "s/^package flexprice$/package ${WL_GO_PACKAGE_NAME}/" "$f"
      echo "  [go] package decl: $f"
    fi
  done < <(find "$GO_DIR" -name "*.go" -print0)

  # 2. Fix *Flexprice type references (receiver type, struct field)
  #    Use -E with character class to avoid \b portability issues across sed variants.
  while IFS= read -r -d '' f; do
    if grep -q '\*Flexprice' "$f" 2>/dev/null; then
      _sed_i -E "s/\*Flexprice([^[:alnum:]_]|$)/*${WL_SDK_CLASS_NAME}\1/g" "$f"
      echo "  [go] *Flexprice type: $f"
    fi
  done < <(find "$GO_DIR" -name "*.go" -print0)

  # 3. Fix hardcoded import path 'github.com/flexprice/go-sdk'
  #    This prefix appears in import strings and go.mod files.
  #    WL_GO_MODULE_PATH must NOT include a /v2 suffix — the /v2 is preserved.
  FLEXPRICE_MODULE="github.com/flexprice/go-sdk"
  # Escape & and \ so sed treats the replacement as a literal string
  WL_GO_MODULE_PATH_ESC=$(printf '%s\n' "${WL_GO_MODULE_PATH}" | sed 's/[&\]/\\&/g')
  while IFS= read -r -d '' f; do
    if grep -q "${FLEXPRICE_MODULE}" "$f" 2>/dev/null; then
      _sed_i "s|${FLEXPRICE_MODULE}|${WL_GO_MODULE_PATH_ESC}|g" "$f"
      echo "  [go] module path: $f"
    fi
  done < <(find "$GO_DIR" \( -name "*.go" -o -name "go.mod" \) -print0)

  # 4. Fix FlexPrice branding strings in Go files:
  #    - [FlexPrice Debug] / [FlexPrice Error] log prefixes
  #    - "for the FlexPrice API" in doc comments (e.g. async.go)
  while IFS= read -r -d '' f; do
    if grep -qE '\[FlexPrice |for the FlexPrice API' "$f" 2>/dev/null; then
      _sed_i "s/\[FlexPrice Debug\]/[${WL_SDK_CLASS_NAME} Debug]/g" "$f"
      _sed_i "s/\[FlexPrice Error\]/[${WL_SDK_CLASS_NAME} Error]/g" "$f"
      _sed_i "s/for the FlexPrice API/for the ${WL_SDK_CLASS_NAME} API/g" "$f"
      echo "  [go] branding strings: $f"
    fi
  done < <(find "$GO_DIR" -name "*.go" -print0)

  echo "Go custom branding applied."
fi

# ─── TypeScript: api/typescript/src/sdk/customer-portal.ts + index.extras.ts ─
echo ""
echo "--- TypeScript SDK ---"

PORTAL="api/typescript/src/sdk/customer-portal.ts"
EXTRAS="api/typescript/src/index.extras.ts"

if [ -f "$PORTAL" ]; then
  # customer-portal.ts only contains 'Flexprice' (no 'FlexPrice' variant).
  # Replace all occurrences: imports, type aliases, field decls, constructor calls.
  # No compound identifiers (e.g. FlexpriceClient) exist in this file, so a plain
  # replace is safe and avoids \b portability issues across sed variants.
  # If new compound identifiers are added, update to word-boundary match.
  _sed_i "s/Flexprice/${WL_SDK_CLASS_NAME}/g" "$PORTAL"
  echo "  [ts] customer-portal.ts → ${WL_SDK_CLASS_NAME}"
else
  echo "  WARNING: $PORTAL not found"
fi

if [ -f "$EXTRAS" ]; then
  # index.extras.ts has 'Flexprice' and 'FlexPriceError' in a comment block.
  # Replace FlexPriceError first to avoid double-substitution, then plain Flexprice.
  _sed_i "s/FlexPriceError/${WL_SDK_CLASS_NAME}Error/g" "$EXTRAS"
  _sed_i "s/Flexprice/${WL_SDK_CLASS_NAME}/g" "$EXTRAS"
  # Update the example import package name in the comment.
  _sed_i "s|\"@flexprice/sdk\"|\"${WL_TS_PACKAGE_NAME}\"|g" "$EXTRAS"
  echo "  [ts] index.extras.ts → ${WL_SDK_CLASS_NAME}, ${WL_TS_PACKAGE_NAME}"
else
  echo "  WARNING: $EXTRAS not found (may be OK if not present)"
fi

echo "TypeScript custom branding applied."

# ─── MCP: api/mcp/package.json ───────────────────────────────────────────────
echo ""
echo "--- MCP ---"

MCP_PKG="api/mcp/package.json"
if [ -f "$MCP_PKG" ]; then
  # fix-mcp-package-name hardcoded @flexprice/mcp-server — override with WL name.
  trap 'rm -f "${MCP_PKG}.tmp"' ERR
  jq --arg name "${WL_MCP_PACKAGE_NAME}" '.name = $name' "$MCP_PKG" > "${MCP_PKG}.tmp"
  mv "${MCP_PKG}.tmp" "$MCP_PKG"
  trap - ERR
  echo "  [mcp] package.json name → ${WL_MCP_PACKAGE_NAME}"
else
  echo "  WARNING: $MCP_PKG not found"
fi

# Also replace "@flexprice/mcp-server" string in any other JSON files (e.g. examples/).
# Escape replacement so & and \ aren't interpreted by sed.
WL_MCP_ESC=$(printf '%s\n' "${WL_MCP_PACKAGE_NAME}" | sed 's/[&\]/\\&/g')
while IFS= read -r -d '' f; do
  if grep -q '"@flexprice/mcp-server"' "$f" 2>/dev/null; then
    _sed_i "s|\"@flexprice/mcp-server\"|\"${WL_MCP_ESC}\"|g" "$f"
    echo "  [mcp] json ref: $f"
  fi
done < <(find "api/mcp" -name "*.json" -print0)

echo "MCP branding applied."

# ─── Python: api/python/pyproject.toml ───────────────────────────────────────
echo ""
echo "--- Python SDK ---"

PYPROJECT="api/python/pyproject.toml"
if [ -f "$PYPROJECT" ]; then
  # merge-custom's sed set: 'for the FlexPrice API.' — replace with WL brand.
  _sed_i "s/for the FlexPrice API\./for the ${WL_SDK_CLASS_NAME} API./g" "$PYPROJECT"
  echo "  [python] pyproject.toml description → for the ${WL_SDK_CLASS_NAME} API."
else
  echo "  WARNING: $PYPROJECT not found"
fi

echo "Python branding applied."

# ─── Documentation: all *.md files in every SDK output dir ───────────────────
echo ""
echo "--- Documentation ---"

# Derive UPPERCASE class name for env var substitution (Omkar → OMKAR)
WL_SDK_CLASS_NAME_UPPER=$(echo "${WL_SDK_CLASS_NAME}" | tr '[:lower:]' '[:upper:]')
# Escape WL_GO_MODULE_PATH for sed replacement (used in Go README imports)
WL_MOD_ESC=$(printf '%s\n' "${WL_GO_MODULE_PATH}" | sed 's/[&\]/\\&/g')
# Escape WL_API_BASE_URL for sed (replacing the hardcoded default API URL)
WL_URL_ESC=$(printf '%s\n' "${WL_API_BASE_URL}" | sed 's/[&\]/\\&/g')

for SDK_DIR in api/go api/typescript api/python api/mcp; do
  [ -d "$SDK_DIR" ] || continue
  while IFS= read -r -d '' f; do
    CHANGED=0
    # Order matters — compound forms first to avoid partial substitution

    # 1. Error class names (before plain Flexprice to avoid double-sub)
    if grep -qE 'FlexpriceError|FlexPriceError' "$f" 2>/dev/null; then
      _sed_i "s/FlexpriceError/${WL_SDK_CLASS_NAME}Error/g" "$f"
      _sed_i "s/FlexPriceError/${WL_SDK_CLASS_NAME}Error/g" "$f"
      CHANGED=1
    fi
    # 2. PascalCase and lowercase-p brand name (FlexPrice, Flexprice)
    if grep -qE 'FlexPrice|Flexprice' "$f" 2>/dev/null; then
      _sed_i "s/FlexPrice/${WL_SDK_CLASS_NAME}/g" "$f"
      _sed_i "s/Flexprice/${WL_SDK_CLASS_NAME}/g" "$f"
      CHANGED=1
    fi
    # 3. UPPERCASE brand name → used for env var prefixes (FLEXPRICE_API_KEY → OMKAR_API_KEY)
    if grep -q 'FLEXPRICE' "$f" 2>/dev/null; then
      _sed_i "s/FLEXPRICE/${WL_SDK_CLASS_NAME_UPPER}/g" "$f"
      CHANGED=1
    fi
    # 4. TypeScript package name in install/import examples
    if grep -q '@flexprice/sdk' "$f" 2>/dev/null; then
      _sed_i "s|@flexprice/sdk|${WL_TS_PACKAGE_NAME}|g" "$f"
      CHANGED=1
    fi
    # 5. MCP package name in config examples
    if grep -q '@flexprice/mcp-server' "$f" 2>/dev/null; then
      _sed_i "s|@flexprice/mcp-server|${WL_MCP_PACKAGE_NAME}|g" "$f"
      CHANGED=1
    fi
    # 6. Go module path in import examples
    if grep -q 'github\.com/flexprice/go-sdk' "$f" 2>/dev/null; then
      _sed_i "s|github\.com/flexprice/go-sdk|${WL_MOD_ESC}|g" "$f"
      CHANGED=1
    fi
    # 7. Default API URL (us.api.flexprice.io/v1 → WL_API_BASE_URL)
    if grep -q 'us\.api\.flexprice\.io' "$f" 2>/dev/null; then
      _sed_i "s|https://us\.api\.flexprice\.io/v1|${WL_URL_ESC}|g" "$f"
      CHANGED=1
    fi
    [ "$CHANGED" -eq 1 ] && echo "  [docs] $f"
  done < <(find "$SDK_DIR" -name "*.md" -print0)
done

echo "Documentation branding applied."

echo ""
echo "=== White-label custom branding complete ==="
