#!/usr/bin/env bash
# The replicated set must be fully replicated and the single-node sets must not
# be, so neither runner ever applies the wrong flavour.
set -euo pipefail
REPL="${1:-migrations/versioned/clickhouse/replicated}"
SINGLE=("migrations/clickhouse" "migrations/versioned/clickhouse")
rc=0

code() { grep -v '^[[:space:]]*--' "$1"; }

for f in "$REPL"/*.sql; do
  [ -e "$f" ] || continue
  for section in up down; do
    if [ "$section" = up ]; then
      body="$(sed -n '/^-- migrate:up/,/^-- migrate:down/p' "$f" | grep -v '^[[:space:]]*--' || true)"
    else
      body="$(sed -n '/^-- migrate:down/,$p' "$f" | grep -v '^[[:space:]]*--' || true)"
    fi
    [ -n "$(printf '%s' "$body" | tr -d '[:space:]')" ] || continue
    if ! printf '%s' "$body" | grep -qi 'ON CLUSTER'; then
      echo "FAIL: $(basename "$f") ($section) lacks ON CLUSTER" >&2; rc=1
    fi
    if [ "$section" = up ] && printf '%s' "$body" | grep -qi 'CREATE TABLE' \
       && ! printf '%s' "$body" | grep -Eqi 'ENGINE[[:space:]]*=[[:space:]]*Replicated'; then
      echo "FAIL: $(basename "$f") CREATE TABLE without a Replicated engine" >&2; rc=1
    fi
  done
done

for d in "${SINGLE[@]}"; do
  for f in "$d"/*.sql; do
    [ -e "$f" ] || continue
    if code "$f" | grep -Eqi 'ON CLUSTER|ENGINE[[:space:]]*=[[:space:]]*Replicated'; then
      echo "FAIL: $f is single-node but uses ON CLUSTER or a Replicated engine" >&2; rc=1
    fi
  done
done
[ $rc -eq 0 ] && echo "clickhouse: replication flavour ok"
exit $rc
