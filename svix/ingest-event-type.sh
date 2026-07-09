#!/bin/bash

SVIX_URL="<host>"
SVIX_TOKEN="<token>"
INPUT_FILE="data.json"

jq -c '.data[]' "$INPUT_FILE" | while read -r item; do
  name=$(echo "$item" | jq -r '.name')
  description=$(echo "$item" | jq -r '.description')

  payload=$(jq -n \
    --arg name "$name" \
    --arg description "$description" \
    '{name: $name, description: $description}')

  echo "Creating: $name"

  response=$(curl -s -w "\n%{http_code}" -X POST "$SVIX_URL/api/v1/event-type/" \
    -H "Authorization: Bearer $SVIX_TOKEN" \
    -H "Content-Type: application/json" \
    -H "idempotency-key: $name" \
    -d "$payload")

  http_code=$(echo "$response" | tail -1)
  body=$(echo "$response" | sed '$d')

  if [[ "$http_code" == "201" ]]; then
    echo "  ✓ Created ($http_code)"
  else
    echo "  ✗ Failed ($http_code): $body"
  fi
done
