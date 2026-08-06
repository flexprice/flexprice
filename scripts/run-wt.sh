#!/usr/bin/env bash
# scripts/run-wt.sh — Run the Flexprice server with a worktree-specific port.
#
# Computes a deterministic port from the current branch name (range 8100-8899)
# so each worktree always binds the same port without touching .env.
# The shared .env is loaded from the main worktree — no per-worktree copies.
#
# Usage:
#   make run-wt            (from any worktree)
#   bash scripts/run-wt.sh

set -euo pipefail

REPO_ROOT="$(git rev-parse --show-toplevel)"
BRANCH="$(git rev-parse --abbrev-ref HEAD 2>/dev/null || echo "detached")"

# Deterministic port: cksum of branch name, mapped to 8100–8899 (800 slots).
# Same branch = same port across restarts. main/master keeps its original 8080.
if [[ "$BRANCH" == "main" || "$BRANCH" == "master" ]]; then
  PORT=8080
else
  PORT=$((8100 + $(printf '%s' "$BRANCH" | cksum | awk '{print $1}') % 800))
fi

# Always load .env from the main (original) worktree — first entry in the list.
MAIN_WORKTREE="$(git worktree list --porcelain | awk '/^worktree /{print $2; exit}')"
ENV_FILE="$MAIN_WORKTREE/.env"

echo "┌─────────────────────────────────────────────┐"
printf "│  branch : %-34s│\n" "$BRANCH"
printf "│  port   : %-34s│\n" "$PORT"
printf "│  url    : %-34s│\n" "http://localhost:$PORT"
printf "│  .env   : %-34s│\n" "$ENV_FILE"
echo "└─────────────────────────────────────────────┘"
echo

if [[ ! -f "$ENV_FILE" ]]; then
  echo "ERROR: .env not found at $ENV_FILE" >&2
  exit 1
fi

# Load the shared .env (no .env.local overlay — we want cloud resources)
set -a
# shellcheck disable=SC1090
source "$ENV_FILE"
set +a

# Override only the port; everything else (DB, Kafka, etc.) unchanged
export FLEXPRICE_SERVER_ADDRESS=":$PORT"

exec go run "$REPO_ROOT/cmd/server/main.go"
