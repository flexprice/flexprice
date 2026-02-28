# FlexPrice Go SDK

Type-safe Go client for the FlexPrice API: billing, metering, and subscription management for SaaS and usage-based products.

## Requirements

- **Go 1.20+** (Go modules required)

## Installation

```bash
go get github.com/flexprice/flexprice-go/v2
```

Then in your code:

```go
import "github.com/flexprice/flexprice-go/v2"
```

## Quick start

Initialize the client with your base URL and API key, then create a customer, ingest an event, and list events:

```go
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/flexprice/flexprice-go/v2"
	"github.com/flexprice/flexprice-go/v2/models/types"
	"github.com/joho/godotenv"
)

func main() {
	godotenv.Load()

	apiKey := os.Getenv("FLEXPRICE_API_KEY")
	apiHost := os.Getenv("FLEXPRICE_API_HOST")
	if apiHost == "" {
		apiHost = "https://us.api.flexprice.io/v1"
	}
	// Base URL must include /v1 (no trailing space or slash).
	if apiKey == "" {
		log.Fatal("Set FLEXPRICE_API_KEY in .env or environment")
	}

	client := flexprice.New(apiHost, flexprice.WithSecurity(apiKey))
	ctx := context.Background()

	customerID := fmt.Sprintf("sample-customer-%d", time.Now().Unix())

	// Ingest an event
	req := types.DtoIngestEventRequest{
		EventName:          "Sample Event",
		ExternalCustomerID: customerID,
		Properties:         map[string]string{"source": "sample_app", "environment": "test"},
	}
	resp, err := client.Events.IngestEvent(ctx, req)
	if err != nil {
		log.Fatalf("IngestEvent: %v", err)
	}
	if resp != nil && resp.RawResponse != nil && resp.RawResponse.StatusCode == 202 {
		fmt.Println("Event created (202).")
	}

	// List events: use client.Events.ListRawEvents(ctx, ...) with optional filters
	// See the API reference and the examples/ directory for more operations.
}
```

For more examples and all API operations, see the [API reference](https://docs.flexprice.io) and the [examples](examples/) in this repo.

## Async client (high-volume events)

For high-volume event ingestion, use the async client: it batches events and sends them in the background.

```go
asyncConfig := flexprice.DefaultAsyncConfig()
asyncConfig.Debug = true
asyncClient := client.NewAsyncClientWithConfig(asyncConfig)
defer asyncClient.Close()

// Simple event
err := asyncClient.Enqueue("api_request", "customer-123", map[string]interface{}{
	"path": "/api/resource", "method": "GET", "status": "200",
})

// Event with full options
err = asyncClient.EnqueueWithOptions(flexprice.EventOptions{
	EventName:          "file_upload",
	ExternalCustomerID: "customer-123",
	Properties:         map[string]interface{}{"file_size_bytes": 1048576},
	Source:             "upload_service",
	Timestamp:          time.Now().Format(time.RFC3339),
})
```

**Benefits:** Automatic batching, background sending, configurable batch size and flush interval, optional debug logging. Call `Close()` before exit to flush remaining events.

## Authentication

- Set the API key via the `x-api-key` header. The SDK uses `flexprice.WithSecurity(apiKey)` when initializing.
- Prefer environment variables (e.g. `FLEXPRICE_API_KEY`); get keys from your [FlexPrice dashboard](https://app.flexprice.io) or docs.

## Features

- Full API coverage (customers, plans, events, invoices, payments, entitlements, etc.)
- Type-safe request/response models
- Built-in retries and error handling
- Optional async client for event batching

For a full list of operations, see the [API reference](https://docs.flexprice.io) and the [examples](examples/) in this repo.

## Troubleshooting

- **Missing or invalid API key:** Ensure `FLEXPRICE_API_KEY` is set and the key is active. Keys are usually server-side only; do not expose them in client-side code.
- **Wrong base URL:** Use `https://us.api.flexprice.io/v1` (or your tenant host with `/v1`). Always include `/v1`; no trailing space or slash.
- **Non-202 on ingest:** Event ingest returns 202 Accepted; if you get 4xx/5xx, check request shape (e.g. `EventName`, `ExternalCustomerID`, `Properties`) and [API docs](https://docs.flexprice.io).

## Documentation

- [FlexPrice API documentation](https://docs.flexprice.io)
- [Go SDK examples](examples/) in this repo
