# FlexPrice Go SDK

Type-safe Go client for the FlexPrice API: billing, metering, and subscription management for SaaS and usage-based products.

## Requirements

- **Go 1.20+** (Go modules required)

## Installation

```bash
go get github.com/flexprice/go-sdk/v2@v2.0.15
```

Then in your code:

```go
import "github.com/flexprice/go-sdk/v2"
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

	"github.com/flexprice/go-sdk/v2"
	"github.com/flexprice/go-sdk/v2/models/types"
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

	client := flexprice.New(
		flexprice.WithServerURL(apiHost),
		flexprice.WithSecurity(apiKey),
	)
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
	if resp != nil {
		if r := resp.GetHTTPMeta().Response; r != nil && r.StatusCode == 202 {
			fmt.Println("Event created (202).")
		}
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

## Handling Webhooks

Flexprice sends webhook events to your server for async updates on payments, invoices, subscriptions, wallets, and more.

**Flow:**
1. Register your endpoint URL in the Flexprice dashboard
2. Receive `POST` with raw JSON body
3. Read `event_type` to route
4. Parse payload into typed struct
5. Handle business logic idempotently
6. Return `200` quickly

```go
package main

import (
	"encoding/json"
	"io"
	"log"
	"net/http"

	"github.com/flexprice/go-sdk/v2/models/types"
)

// envelope reads only the event_type field for cheap routing
type envelope struct {
	EventType types.WebhookEventName `json:"event_type"`
}

func handleWebhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}

	var env envelope
	if err := json.Unmarshal(body, &env); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	switch env.EventType {
	case types.WebhookEventNamePaymentSuccess,
		types.WebhookEventNamePaymentFailed,
		types.WebhookEventNamePaymentUpdated:
		var payload types.WebhookDtoPaymentWebhookPayload
		if err := json.Unmarshal(body, &payload); err != nil {
			log.Printf("parse error: %v", err)
			break
		}
		if p := payload.GetPayment(); p != nil {
			log.Printf("payment %s", p.GetID())
			// TODO: update payment record
		}

	case types.WebhookEventNameSubscriptionActivated,
		types.WebhookEventNameSubscriptionCancelled,
		types.WebhookEventNameSubscriptionUpdated:
		var payload types.WebhookDtoSubscriptionWebhookPayload
		if err := json.Unmarshal(body, &payload); err != nil {
			log.Printf("parse error: %v", err)
			break
		}
		if s := payload.GetSubscription(); s != nil {
			log.Printf("subscription %s", s.GetID())
		}

	case types.WebhookEventNameInvoiceUpdateFinalized,
		types.WebhookEventNameInvoicePaymentOverdue:
		var payload types.WebhookDtoInvoiceWebhookPayload
		if err := json.Unmarshal(body, &payload); err != nil {
			log.Printf("parse error: %v", err)
			break
		}
		if inv := payload.GetInvoice(); inv != nil {
			log.Printf("invoice %s", inv.GetID())
		}

	default:
		log.Printf("unhandled event: %s", env.EventType)
	}

	w.WriteHeader(http.StatusOK)
}
```

### Event types

| Category | Events |
|---|---|
| **Payment** | `payment.created` · `payment.updated` · `payment.success` · `payment.failed` · `payment.pending` |
| **Invoice** | `invoice.create.drafted` · `invoice.update` · `invoice.update.finalized` · `invoice.update.payment` · `invoice.update.voided` · `invoice.payment.overdue` · `invoice.communication.triggered` |
| **Subscription** | `subscription.created` · `subscription.draft.created` · `subscription.activated` · `subscription.updated` · `subscription.paused` · `subscription.resumed` · `subscription.cancelled` · `subscription.renewal.due` |
| **Subscription Phase** | `subscription.phase.created` · `subscription.phase.updated` · `subscription.phase.deleted` |
| **Customer** | `customer.created` · `customer.updated` · `customer.deleted` |
| **Wallet** | `wallet.created` · `wallet.updated` · `wallet.terminated` · `wallet.transaction.created` · `wallet.credit_balance.dropped` · `wallet.credit_balance.recovered` · `wallet.ongoing_balance.dropped` · `wallet.ongoing_balance.recovered` |
| **Feature / Entitlement** | `feature.created` · `feature.updated` · `feature.deleted` · `feature.wallet_balance.alert` · `entitlement.created` · `entitlement.updated` · `entitlement.deleted` |
| **Credit Note** | `credit_note.created` · `credit_note.updated` |

**Production rules:**
- Keep handlers idempotent — Flexprice retries on non-`2xx`
- Return `200` for unknown event types — prevents unnecessary retries
- Do heavy processing async — respond fast, queue the work

## Documentation

- [FlexPrice API documentation](https://docs.flexprice.io)
- [Go SDK examples](examples/) in this repo
