# FlexPrice Go SDK

Type-safe Go client for the FlexPrice API: billing, metering, and subscription management for SaaS and usage-based products.

## Requirements

- **Go 1.20+** (Go modules required)

## Installation

```bash
go get github.com/flexprice/go-sdk/v2
```

Then in your code:

```go
import "github.com/flexprice/go-sdk/v2"
```

## Environment

| Variable | Required | Description |
| -------- | -------- | ----------- |
| `FLEXPRICE_API_KEY` | Yes | API key |
| `FLEXPRICE_API_HOST` | Optional | Full base URL including `https://` and `/v1` (default: `https://us.api.flexprice.io/v1`). No trailing slash. |

**Integration tests** in [api/tests/go/test_sdk.go](../tests/go/test_sdk.go) use a different env shape (host without scheme); see [api/tests/README.md](../tests/README.md).

## Quick start

Initialize the client, create a customer, ingest an event:

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
	serverURL := os.Getenv("FLEXPRICE_API_HOST")
	if serverURL == "" {
		serverURL = "https://us.api.flexprice.io/v1"
	}
	if apiKey == "" {
		log.Fatal("Set FLEXPRICE_API_KEY in .env or environment")
	}

	client := flexprice.New(
		flexprice.WithServerURL(serverURL),
		flexprice.WithSecurity(apiKey),
	)
	ctx := context.Background()

	externalID := fmt.Sprintf("sample-customer-%d", time.Now().Unix())
	_, err := client.Customers.CreateCustomer(ctx, types.CreateCustomerRequest{
		ExternalID: externalID,
		Name:       flexprice.String("Sample Customer"),
		Email:      flexprice.String("sample@example.com"),
	})
	if err != nil {
		log.Fatalf("CreateCustomer: %v", err)
	}

	req := types.IngestEventRequest{
		EventName:          "Sample Event",
		ExternalCustomerID: externalID,
		Properties:         map[string]string{"source": "sample_app", "environment": "test"},
	}
	resp, err := client.Events.IngestEvent(ctx, req)
	if err != nil {
		log.Fatalf("IngestEvent: %v", err)
	}
	if resp != nil {
		meta := resp.GetHTTPMeta()
		if hr := meta.GetResponse(); hr != nil && hr.StatusCode == 202 {
			fmt.Println("Event created (202).")
		}
	}

	// List events: client.Events.ListRawEvents(ctx, ...)
	// See the API reference and the examples/ directory for more operations.
}
```

For more examples and all API operations, see the [API reference](https://docs.flexprice.io) and the [examples](examples/) in this repo.

## Optional fields and pointer helpers

Required fields are plain Go values; optional fields are pointers. The SDK ships helper functions so you never need a temporary variable:

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
)

func main() {
	apiKey := os.Getenv("FLEXPRICE_API_KEY")
	serverURL := os.Getenv("FLEXPRICE_API_HOST")
	if serverURL == "" {
		serverURL = "https://us.api.flexprice.io/v1"
	}
	if apiKey == "" {
		log.Fatal("Set FLEXPRICE_API_KEY in your environment")
	}

	client := flexprice.New(
		flexprice.WithServerURL(serverURL),
		flexprice.WithSecurity(apiKey),
	)
	ctx := context.Background()

	externalID := fmt.Sprintf("acme-%d", time.Now().UnixNano())
	resp, err := client.Customers.CreateCustomer(ctx, types.CreateCustomerRequest{
		ExternalID: externalID,
		Name:       flexprice.String("Acme Corp"),
		Email:      flexprice.String("billing@acme.com"),
		// Optional address fields
		AddressLine1:   flexprice.String("123 Main St"),
		AddressCity:    flexprice.String("San Francisco"),
		AddressState:   flexprice.String("CA"),
		AddressCountry: flexprice.String("US"),
		// Generic helper for any type
		Metadata: flexprice.Pointer(map[string]string{"plan_tier": "growth"}),
	})
	if err != nil {
		log.Fatalf("CreateCustomer: %v", err)
	}
	if resp != nil {
		fmt.Println("created customer")
	}
}
```

Available helpers: `flexprice.String`, `flexprice.Bool`, `flexprice.Int`, `flexprice.Int64`, `flexprice.Float32`, `flexprice.Float64`, `flexprice.Pointer[T]`.

## Nil-safe getters

Every generated type has nil-safe `Get*()` methods. Calling a getter on a nil receiver returns the zero value for that getter’s return type (for example `nil` pointers, empty strings where the getter returns `string`) — no panic:

```go
package main

import (
	"fmt"

	"github.com/flexprice/go-sdk/v2/models/dtos"
	"github.com/flexprice/go-sdk/v2/models/types"
)

func main() {
	var sub *types.Subscription // nil

	// Safe — returns nil *string instead of panicking
	fmt.Println("id on nil subscription:", sub.GetID())

	// Safe chain — returns nil instead of panicking
	fmt.Println("billing cycle on nil subscription:", sub.GetBillingCycle())

	// Prefer getters when traversing nested optional fields (e.g. after GetSubscription).
	// resp can be nil or point to a response with a nil Subscription field.
	var resp *dtos.GetSubscriptionResponse
	if s := resp.GetSubscription(); s != nil {
		fmt.Println(s.GetID(), s.GetStatus())
		if cycle := s.GetBillingCycle(); cycle != nil {
			fmt.Println(cycle.GetInterval())
		}
	}
}
```

## Error handling

Use `errors.As` for typed errors, or the `errorutils` helpers for HTTP status checks:

```go
package main

import (
	"context"
	"errors"
	"log"
	"os"

	"github.com/flexprice/go-sdk/v2"
	"github.com/flexprice/go-sdk/v2/errorutils"
	sdkerrors "github.com/flexprice/go-sdk/v2/models/errors"
	"github.com/flexprice/go-sdk/v2/models/types"
)

func main() {
	apiKey := os.Getenv("FLEXPRICE_API_KEY")
	serverURL := os.Getenv("FLEXPRICE_API_HOST")
	if serverURL == "" {
		serverURL = "https://us.api.flexprice.io/v1"
	}
	if apiKey == "" {
		log.Fatal("Set FLEXPRICE_API_KEY in your environment")
	}

	client := flexprice.New(
		flexprice.WithServerURL(serverURL),
		flexprice.WithSecurity(apiKey),
	)
	ctx := context.Background()

	req := types.CreateCustomerRequest{
		ExternalID: "acme-error-handling-example",
		Name:       flexprice.String("Acme Corp"),
	}

	resp, err := client.Customers.CreateCustomer(ctx, req)
	if err != nil {
		switch {
		case errorutils.IsConflict(err):
			// 409 — customer already exists
			log.Println("duplicate customer, continuing")
		case errorutils.IsValidation(err):
			// 400 — bad request
			var apiErr *sdkerrors.APIError
			errors.As(err, &apiErr)
			log.Fatalf("validation error: %s", apiErr.Body)
		case errorutils.IsNotFound(err):
			// 404
			log.Fatalf("not found")
		default:
			log.Fatalf("unexpected error: %v", err)
		}
	}
	if resp != nil {
		log.Println("CreateCustomer succeeded")
	}
}
```

Available helpers: `IsNotFound` (404), `IsValidation` (400), `IsConflict` (409), `IsRateLimit` (429), `IsPermissionDenied` (403), `IsServerError` (5xx).

## Async client (high-volume events)

For high-volume event ingestion, use the async client: it batches events and sends them in the background.

```go
package main

import (
	"log"
	"os"
	"time"

	"github.com/flexprice/go-sdk/v2"
)

func main() {
	apiKey := os.Getenv("FLEXPRICE_API_KEY")
	serverURL := os.Getenv("FLEXPRICE_API_HOST")
	if serverURL == "" {
		serverURL = "https://us.api.flexprice.io/v1"
	}
	if apiKey == "" {
		log.Fatal("Set FLEXPRICE_API_KEY in your environment")
	}

	client := flexprice.New(
		flexprice.WithServerURL(serverURL),
		flexprice.WithSecurity(apiKey),
	)

	asyncConfig := flexprice.DefaultAsyncConfig()
	asyncConfig.Debug = true
	asyncClient := client.NewAsyncClientWithConfig(asyncConfig)
	defer asyncClient.Close()

	// Simple event
	if err := asyncClient.Enqueue("api_request", "customer-123", map[string]interface{}{
		"path": "/api/resource", "method": "GET", "status": "200",
	}); err != nil {
		log.Fatalf("Enqueue: %v", err)
	}

	// Event with full options
	if err := asyncClient.EnqueueWithOptions(flexprice.EventOptions{
		EventName:          "file_upload",
		ExternalCustomerID: "customer-123",
		Properties:         map[string]interface{}{"file_size_bytes": 1048576},
		Source:             "upload_service",
		Timestamp:          time.Now().Format(time.RFC3339),
	}); err != nil {
		log.Fatalf("EnqueueWithOptions: %v", err)
	}
}
```

**Benefits:** Automatic batching, background sending, configurable batch size and flush interval, optional debug logging. Call `Close()` before exit to flush remaining events.

## Idempotent requests


## Authentication

- Set the API key via the `x-api-key` header. Initialize with `flexprice.New(flexprice.WithServerURL(serverURL), flexprice.WithSecurity(apiKey))`, where `serverURL` is a full URL (default `https://us.api.flexprice.io/v1`) or use `WithServerIndex` / default server list if you omit `WithServerURL`.
- Prefer environment variables; get keys from your [FlexPrice dashboard](https://app.flexprice.io) or docs.

## Features

- Full API coverage (customers, plans, events, invoices, payments, entitlements, etc.)
- Type-safe request/response models
- Built-in retries and error handling
- Optional async client for event batching

For a full list of operations, see the [API reference](https://docs.flexprice.io) and the [examples](examples/) in this repo.

## Troubleshooting

- **Missing or invalid API key:** Ensure `FLEXPRICE_API_KEY` is set and the key is active. Keys are usually server-side only; do not expose them in client-side code.
- **Wrong base URL:** Use a full URL such as `https://us.api.flexprice.io/v1` (include `/v1`; no trailing slash). For local dev use `http://localhost:8080/v1` if applicable.
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
- [SDK integration tests](../tests/README.md) — full API coverage (different `FLEXPRICE_API_HOST` shape; see that README)
