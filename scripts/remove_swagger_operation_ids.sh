#!/bin/bash

# Script to remove all operationId fields from OpenAPI 3.0 swagger spec.
# Run after docs/swagger/swagger-3-0.json is generated (e.g. by make swagger).

# Configuration
SWAGGER_FILE="docs/swagger/swagger-3-0.json"
BACKUP_FILE="${SWAGGER_FILE}.bak"

# Check if the swagger file exists
if [ ! -f "$SWAGGER_FILE" ]; then
    echo "Error: Swagger file not found at $SWAGGER_FILE"
    exit 1
fi

# Create a backup of the original file
cp "$SWAGGER_FILE" "$BACKUP_FILE"
echo "Created backup at $BACKUP_FILE"

# Create a temporary Python script to handle the JSON processing
cat > /tmp/remove_operation_ids.py << 'EOF'
import json
import sys

swagger_file = sys.argv[1]

with open(swagger_file, 'r') as f:
    data = json.load(f)

paths = data.get("paths", {})
removed = 0
for path, methods in paths.items():
    if not isinstance(methods, dict):
        continue
    for method, op in methods.items():
        if not isinstance(op, dict):
            continue
        if op.pop("operationId", None) is not None:
            removed += 1

with open(swagger_file, 'w') as f:
    json.dump(data, f, indent=2)

print(f'Removed {removed} operationId(s) from path operations')
EOF

# Run the Python script
python3 /tmp/remove_operation_ids.py "$SWAGGER_FILE" || {
    echo "Error: Failed to process the file with Python"
    cp "$BACKUP_FILE" "$SWAGGER_FILE"
    exit 1
}

# Clean up
rm /tmp/remove_operation_ids.py
rm "$BACKUP_FILE"

echo "Processed $SWAGGER_FILE"
echo "Done! operationIds have been removed from the OpenAPI 3.0 spec."
