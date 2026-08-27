#!/usr/bin/env bash
# Print the schema fingerprint of a database.
#
# Deliberately NOT `psql ... | shasum`: a pipeline reports the LAST command's
# status, so a psql that could not connect still exits 0 and shasum happily hashes
# the empty string —
#   e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855
# Two unreachable databases then produce identical hashes and compare as equal.
# This command exists to verify a production database before adopting it, so a
# false match is the worst possible failure.
set -euo pipefail
URL="${1:?usage: fingerprint.sh <database-url>}"
HERE="$(cd "$(dirname "$0")" && pwd)"
OUT="$(mktemp)"
trap 'rm -f "$OUT"' EXIT

if ! psql -X -q "$URL" -f "$HERE/fingerprint.sql" > "$OUT" 2>/tmp/fingerprint.err; then
  echo "FAIL: could not read the schema" >&2
  sed 's/^/  /' /tmp/fingerprint.err >&2
  exit 1
fi

# A reachable but empty database also hashes to the empty-string digest. Nothing
# downstream should ever treat that as a schema.
if [ ! -s "$OUT" ]; then
  echo "FAIL: the query returned nothing — is this the right database?" >&2
  exit 1
fi

shasum -a 256 < "$OUT" | cut -d' ' -f1
