#!/usr/bin/env bash
# The legacy, versioned and replicated ClickHouse sets must describe the same
# schema: tables, column names, skip indexes and projections. Engines, ON CLUSTER,
# types, codecs and settings legitimately differ and are ignored.
set -euo pipefail
exec python3 -I "$(dirname "$0")/clickhouse_parity.py"
