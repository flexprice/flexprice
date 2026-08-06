#!/usr/bin/env bash
# scripts/wt-ports.sh — Show all worktrees, their computed ports, and whether
# a server is currently listening on that port.
#
# Usage:
#   make wt-ports

set -euo pipefail

echo "┌──────────────────────────────────────────────────────────────────┐"
echo "│  Worktree port map                                               │"
echo "├──────────────────────────────────────────────────────────────────┤"
printf "│  %-30s  %6s  %-14s  %s\n" "BRANCH" "PORT" "STATUS" "PATH"
echo "├──────────────────────────────────────────────────────────────────┤"

git worktree list --porcelain | awk '
  /^worktree / { path=$2 }
  /^branch /   { branch=$2; sub(/^refs\/heads\//, "", branch) }
  /^HEAD / && !branch { branch="(detached)" }
  /^$/ {
    if (path != "") {
      print path "\t" branch
      path=""; branch=""
    }
  }
  END {
    if (path != "") print path "\t" branch
  }
' | while IFS=$'\t' read -r wt_path branch; do
  if [[ "$branch" == "main" || "$branch" == "master" ]]; then
    port=8080
  else
    port=$((8100 + $(printf '%s' "$branch" | cksum | awk '{print $1}') % 800))
  fi

  # Check if something is listening on that port
  if 2>/dev/null bash -c "echo >/dev/tcp/localhost/$port"; then
    status="● running"
  else
    status="○ stopped"
  fi

  printf "│  %-30s  %6s  %-14s  %s\n" "${branch:0:30}" "$port" "$status" "${wt_path/$HOME/\~}"
done

echo "└──────────────────────────────────────────────────────────────────┘"
echo
echo "  Run server in any worktree:  make run-wt"
echo "  Integration tests:           FLEXPRICE_TARGETS_FILE=... make test-suite"
