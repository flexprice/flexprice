#!/bin/bash
set -euo pipefail

# Only run in remote Claude Code on the web sessions
if [ "${CLAUDE_CODE_REMOTE:-}" != "true" ]; then
  exit 0
fi

echo "Installing Go dependencies..."
cd "${CLAUDE_PROJECT_DIR:-$(dirname "$(dirname "$(realpath "$0")")")}"
go mod download

echo "Session start hook complete."
