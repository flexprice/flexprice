#!/bin/bash

# Updates the servers block in the OpenAPI 3.0 spec (swagger-3-0.json)
# Run from repository root.

set -e

SWAGGER_3_FILE="docs/swagger/swagger-3-0.json"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

cd "$REPO_ROOT"

if [ ! -f "$SWAGGER_3_FILE" ]; then
    echo "Error: $SWAGGER_3_FILE not found. Run 'make swagger-3-0' first."
    exit 1
fi

python3 << 'PYEOF'
import json
import os

# Script is run from repo root (see cd above)
path = os.path.join(os.getcwd(), "docs", "swagger", "swagger-3-0.json")

with open(path) as f:
    spec = json.load(f)

spec["servers"] = [
    {"url": "https://us.api.flexprice.io/v1", "description": "US Region"},
    {"url": "https://api.cloud.flexprice.io/v1", "description": "India Region"}
]

with open(path, "w") as f:
    json.dump(spec, f, indent=2)

print("Updated servers block in", path)
PYEOF
