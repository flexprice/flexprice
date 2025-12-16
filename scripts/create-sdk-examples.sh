#!/bin/bash

# Script to create/restore SDK examples after generation
# This ensures examples are always available even after clean-sdk

set -e

SDK_TYPE="$1"

case "$SDK_TYPE" in
    "go")
        EXAMPLES_DIR="api/go/examples"
        echo "Creating Go SDK examples..."
        mkdir -p "$EXAMPLES_DIR"
        
        # Create main.go if it doesn't exist
        if [ ! -f "$EXAMPLES_DIR/main.go" ]; then
            cat > "$EXAMPLES_DIR/main.go" << 'EOF'
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	gosdk "github.com/flexprice/go-sdk"
	"github.com/flexprice/go-sdk/models/components"
	"github.com/flexprice/go-sdk/models/operations"
	"github.com/joho/godotenv"
)

func main() {
	_ = godotenv.Load()
	apiKey := os.Getenv("FLEXPRICE_API_KEY")
	apiHost := os.Getenv("FLEXPRICE_API_HOST")
	if apiKey == "" {
		log.Fatal("FLEXPRICE_API_KEY environment variable is required")
	}
	if apiHost == "" {
		apiHost = "https://api.cloud.flexprice.io"
	}

	fmt.Println("FlexPrice Go SDK Example")
	fmt.Println("========================")
	fmt.Printf("API Host: %s\n\n", apiHost)

	client := gosdk.New(apiHost, gosdk.WithSecurity(apiKey))
	ctx := context.Background()

	// Example 1: Send event
	fmt.Println("1. Sending event...")
	event := components.DtoIngestEventRequest{
		ExternalCustomerID: "customer_123",
		EventName:          "api_call",
		Properties: map[string]string{
			"method": "GET",
		},
		Timestamp: gosdk.String(time.Now().Format(time.RFC3339)),
	}
	res, err := client.Events.Ingest(ctx, event)
	if err != nil {
		log.Printf("Error: %v\n", err)
	} else {
		fmt.Printf("✓ Event sent: %+v\n", res.Object)
	}

	// Example 2: List invoices
	fmt.Println("\n2. Listing invoices...")
	limit := int64(5)
	invoicesReq := operations.GetInvoicesRequest{Limit: &limit}
	invoicesRes, err := client.Invoices.List(ctx, invoicesReq)
	if err != nil {
		log.Printf("Error: %v\n", err)
	} else if invoicesRes.DtoListInvoicesResponse != nil {
		fmt.Printf("✓ Found %d invoices\n", len(invoicesRes.DtoListInvoicesResponse.Items))
	}

	fmt.Println("\nExample completed!")
}
EOF
        fi
        
        # Create go.mod
        cat > "$EXAMPLES_DIR/go.mod" << 'EOF'
module github.com/flexprice/go-sdk/examples

go 1.23

require (
	github.com/flexprice/go-sdk v1.0.12
	github.com/joho/godotenv v1.5.1
)

// Use local SDK for testing
replace github.com/flexprice/go-sdk => ../
EOF
        
        # Create .env.example
        cat > "$EXAMPLES_DIR/.env.example" << 'EOF'
FLEXPRICE_API_KEY=your_api_key_here
FLEXPRICE_API_HOST=https://api.cloud.flexprice.io
EOF
        
        # Create README
        cat > "$EXAMPLES_DIR/README.md" << 'EOF'
# FlexPrice Go SDK Examples

## Setup
1. Copy `.env.example` to `.env` and add your API key
2. Run: `go run main.go`
EOF
        
        echo "✓ Go examples created"
        ;;
        
    "python")
        EXAMPLES_DIR="api/python/examples"
        echo "Creating Python SDK examples..."
        mkdir -p "$EXAMPLES_DIR"
        
        # Create example.py if it doesn't exist
        if [ ! -f "$EXAMPLES_DIR/example.py" ]; then
            cat > "$EXAMPLES_DIR/example.py" << 'EOF'
import os
from datetime import datetime
from flexprice import Flexprice
from flexprice.models import components

api_key = os.getenv("FLEXPRICE_API_KEY")
api_host = os.getenv("FLEXPRICE_API_HOST", "https://api.cloud.flexprice.io")

if not api_key:
    print("Error: FLEXPRICE_API_KEY required")
    exit(1)

print("FlexPrice Python SDK Example")
print("=" * 50)

sdk = Flexprice(api_key_auth=api_key, server_url=api_host)

# Send event
print("\n1. Sending event...")
try:
    event = components.DtoIngestEventRequest(
        external_customer_id="customer_123",
        event_name="api_call",
        properties={"method": "GET"},
        timestamp=datetime.now().isoformat()
    )
    result = sdk.events.ingest(request=event)
    print(f"✓ Event sent: {result.object}")
except Exception as e:
    print(f"Error: {e}")

print("\nExample completed!")
EOF
        fi
        
        # Create README
        cat > "$EXAMPLES_DIR/README.md" << 'EOF'
# FlexPrice Python SDK Examples

## Setup
1. Install: `pip install -e ..`
2. Set: `export FLEXPRICE_API_KEY=your_key`
3. Run: `python example.py`
EOF
        
        echo "✓ Python examples created"
        ;;
        
    "javascript")
        EXAMPLES_DIR="api/javascript/examples"
        echo "Creating TypeScript SDK examples..."
        mkdir -p "$EXAMPLES_DIR"
        
        # Create example.ts if it doesn't exist
        if [ ! -f "$EXAMPLES_DIR/example.ts" ]; then
            cat > "$EXAMPLES_DIR/example.ts" << 'EOF'
import { Flexprice } from "@flexprice/sdk";

const apiKey = process.env.FLEXPRICE_API_KEY;
const apiHost = process.env.FLEXPRICE_API_HOST || "https://api.cloud.flexprice.io";

if (!apiKey) {
  console.error("Error: FLEXPRICE_API_KEY required");
  process.exit(1);
}

console.log("FlexPrice TypeScript SDK Example");
console.log("=".repeat(50));

const sdk = new Flexprice({
  apiKeyAuth: apiKey,
  serverURL: apiHost,
});

// Send event
console.log("\n1. Sending event...");
sdk.events.ingest({
  externalCustomerId: "customer_123",
  eventName: "api_call",
  properties: { method: "GET" },
  timestamp: new Date().toISOString(),
}).then(result => {
  console.log("✓ Event sent:", result.object);
}).catch(error => {
  console.error("Error:", error);
});

console.log("\nExample completed!");
EOF
        fi
        
        # Create README
        cat > "$EXAMPLES_DIR/README.md" << 'EOF'
# FlexPrice TypeScript SDK Examples

## Setup
1. Install: `npm install tsx`
2. Set: `export FLEXPRICE_API_KEY=your_key`
3. Run: `npx tsx example.ts`
EOF
        
        echo "✓ TypeScript examples created"
        ;;
        
    *)
        echo "Usage: $0 {go|python|javascript}"
        exit 1
        ;;
esac
