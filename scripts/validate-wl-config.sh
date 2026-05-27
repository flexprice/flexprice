#!/usr/bin/env bash
# Validates all WL_* env vars required for white-label SDK generation.
# Reads from environment variables. Reports ALL failures at once.
# Exit 0: all valid. Exit 1: one or more missing/invalid fields.
set -euo pipefail

MISSING=""
INVALID=""
INDEX="${WL_CLIENT_INDEX:-?}"

check() {
  local name=$1
  local value=$2
  local pattern=$3
  if [ -z "$value" ]; then
    MISSING="$MISSING\n  - $name"
  elif [ -n "$pattern" ] && [[ ! "$value" =~ $pattern ]]; then
    INVALID="$INVALID\n  - $name: expected pattern $pattern, got: $value"
  fi
}

# Required fields with format validation
check WL_SDK_CLASS_NAME       "${WL_SDK_CLASS_NAME:-}"       "^[A-Z][a-zA-Z0-9]+$"
check WL_GO_MODULE_PATH       "${WL_GO_MODULE_PATH:-}"       "^github\.com/.+/.+"
check WL_GO_PACKAGE_NAME      "${WL_GO_PACKAGE_NAME:-}"      "^[a-z][a-z0-9_]+$"
check WL_TS_PACKAGE_NAME      "${WL_TS_PACKAGE_NAME:-}"      "^@[a-z0-9-]+/[a-z0-9-]+"
check WL_PYTHON_PACKAGE_NAME  "${WL_PYTHON_PACKAGE_NAME:-}"  "^[a-z][a-z0-9_-]+$"
check WL_MCP_PACKAGE_NAME     "${WL_MCP_PACKAGE_NAME:-}"     "^@[a-z0-9-]+/[a-z0-9-]+"
check WL_AUTHOR_NAME          "${WL_AUTHOR_NAME:-}"          ""
check WL_API_BASE_URL         "${WL_API_BASE_URL:-}"         "^https?://.+"
check WL_GO_REPO              "${WL_GO_REPO:-}"              "^[^/]+/[^/]+"
check WL_PYTHON_REPO          "${WL_PYTHON_REPO:-}"          "^[^/]+/[^/]+"
check WL_TS_REPO              "${WL_TS_REPO:-}"              "^[^/]+/[^/]+"
check WL_MCP_REPO             "${WL_MCP_REPO:-}"             "^[^/]+/[^/]+"
check WL_SDK_DEPLOY_GIT_TOKEN "${WL_SDK_DEPLOY_GIT_TOKEN:-}" ""

# Conditional: tokens only required if publishing is enabled
[ "${WL_PUBLISH_NPM:-false}"  = "true" ] && check WL_NPM_TOKEN  "${WL_NPM_TOKEN:-}"  ""
[ "${WL_PUBLISH_PYPI:-false}" = "true" ] && check WL_PYPI_TOKEN "${WL_PYPI_TOKEN:-}" ""

if [ -n "$MISSING" ]; then
  printf "::error::Client[%s] missing required fields:%b\n" "$INDEX" "$MISSING"
fi
if [ -n "$INVALID" ]; then
  printf "::error::Client[%s] invalid field format:%b\n" "$INDEX" "$INVALID"
fi
if [ -n "$MISSING$INVALID" ]; then
  exit 1
fi

echo "Client[$INDEX] config validation passed."
