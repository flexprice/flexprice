// Flexprice Go SDK — comprehensive example.
//
// Run from api/go/examples/:
//
//	cp .env.example .env   # set FLEXPRICE_API_KEY (and optionally FLEXPRICE_API_HOST)
//	go run main.go
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"time"

	flexprice "github.com/flexprice/go-sdk/v2"
	"github.com/flexprice/go-sdk/v2/errorutils"
	"github.com/flexprice/go-sdk/v2/models/dtos"
	sdkerrors "github.com/flexprice/go-sdk/v2/models/errors"
	"github.com/flexprice/go-sdk/v2/models/types"
	"github.com/joho/godotenv"
)

// ── Globals ─────────────────────────────────────────────────────────────────

var client *flexprice.Flexprice

func main() {
	// Load .env file if present (silently ignored if missing).
	_ = godotenv.Load()

	// ── Client initialization ────────────────────────────────────────────────
	apiKey := os.Getenv("FLEXPRICE_API_KEY")
	if apiKey == "" {
		log.Fatalf("FLEXPRICE_API_KEY environment variable is required")
	}

	apiHost := os.Getenv("FLEXPRICE_API_HOST")
	if apiHost == "" {
		apiHost = "https://us.api.flexprice.io/v1"
	}

	// flexprice.New accepts variadic SDKOption values.
	// WithServerURL sets the base URL; WithSecurity sets the API key.
	client = flexprice.New(
		flexprice.WithServerURL(apiHost),
		flexprice.WithSecurity(apiKey),
	)

	ctx := context.Background()

	// Work through each feature area in sequence.
	customer := createOrGetCustomer(ctx)
	if customer == nil {
		return
	}

	plan := createPlan(ctx)
	if plan == nil {
		return
	}

	subscription := createSubscription(ctx, customer, plan)
	if subscription == nil {
		return
	}

	ingestEvents(ctx, customer)

	listInvoices(ctx, customer)

	cancelSubscription(ctx, subscription)
}

// ── Create or get customer ───────────────────────────────────────────────────

func createOrGetCustomer(ctx context.Context) *types.Customer {
	externalID := fmt.Sprintf("acme-corp-%d", time.Now().UnixMilli()%100000)

	req := types.CreateCustomerRequest{
		// ExternalID is the identifier from your system (required, plain string).
		ExternalID: externalID,

		// Optional fields use pointer helpers so zero-values are never sent.
		Name:           flexprice.String("Acme Corporation"),
		Email:          flexprice.String("billing@acme.example.com"),
		AddressLine1:   flexprice.String("100 Market Street"),
		AddressCity:    flexprice.String("San Francisco"),
		AddressState:   flexprice.String("CA"),
		AddressCountry: flexprice.String("US"),

		// flexprice.Pointer[T] works for any type not covered by the typed helpers.
		AddressPostalCode: flexprice.Pointer("94105"),

		Metadata: map[string]string{
			"plan_tier": "enterprise",
			"source":    "go-sdk-example",
		},
	}

	// Idempotency key ensures duplicate POSTs are safe to retry.
	idempotencyKey := "create-customer-" + externalID

	resp, err := client.Customers.CreateCustomer(
		ctx,
		req,
		flexprice.WithIdempotencyKey(idempotencyKey),
	)
	if err != nil {
		// ── Error handling ─────────────────────────────────────────────────
		if errorutils.IsConflict(err) {
			log.Printf("customer already exists (409 conflict) — continuing")
			// Fall through; the caller can query for the existing record.
			return nil
		}

		// Inspect the typed error for more detail.
		var apiErr *sdkerrors.APIError
		if errors.As(err, &apiErr) {
			log.Printf("API error creating customer: status=%d body=%s",
				apiErr.StatusCode, apiErr.Body)
		} else {
			log.Printf("unexpected error creating customer: %v", err)
		}
		return nil
	}

	// ── Nil-safe getter chain ────────────────────────────────────────────────
	// Every Get*() method is safe to call even if the receiver is nil.
	customer := resp.GetCustomer()
	if customer == nil {
		log.Printf("CreateCustomer returned a nil customer")
		return nil
	}

	log.Printf("created customer id=%s name=%s email=%s",
		ptrStr(customer.GetID()),
		ptrStr(customer.GetName()),
		ptrStr(customer.GetEmail()),
	)

	// ── Query with pagination ────────────────────────────────────────────────
	listCustomers(ctx)

	return customer
}

// listCustomers demonstrates paginated customer queries.
func listCustomers(ctx context.Context) {
	const pageSize = 5
	var offset int64

	for page := 0; ; page++ {
		// Per-request timeout: applied only to this single call.
		resp, err := client.Customers.QueryCustomer(
			ctx,
			types.CustomerFilter{
				Limit:  flexprice.Int64(pageSize),
				Offset: flexprice.Int64(offset),
			},
			dtos.WithOperationTimeout(15*time.Second),
		)
		if err != nil {
			log.Printf("QueryCustomer error: %v", err)
			return
		}

		listResp := resp.GetListCustomersResponse()
		items := listResp.GetItems()
		pagination := listResp.GetPagination()

		for _, c := range items {
			log.Printf("  customer id=%s external_id=%s",
				ptrStr(c.GetID()),
				ptrStr(c.GetExternalID()),
			)
		}

		// Use nil-safe pagination getters to decide whether to continue.
		total := int64(0)
		if t := pagination.GetTotal(); t != nil {
			total = *t
		}
		log.Printf("page=%d fetched=%d total=%d", page, len(items), total)

		offset += int64(len(items))
		if offset >= total || len(items) == 0 {
			break
		}
	}
}

// ── Create plan ──────────────────────────────────────────────────────────────

func createPlan(ctx context.Context) *types.Plan {
	req := types.CreatePlanRequest{
		Name:        "Pro Monthly",
		Description: flexprice.String("Full-featured monthly subscription plan"),
		LookupKey:   flexprice.String("pro-monthly"),
		Metadata: map[string]string{
			"tier": "pro",
		},
	}

	resp, err := client.Plans.CreatePlan(ctx, req,
		flexprice.WithIdempotencyKey("create-plan-pro-monthly-v1"),
	)
	if err != nil {
		if errorutils.IsConflict(err) {
			log.Printf("plan already exists — continuing")
			return nil
		}
		log.Printf("CreatePlan error: %v", err)
		return nil
	}

	plan := resp.GetPlan()
	if plan == nil {
		log.Printf("CreatePlan returned a nil plan")
		return nil
	}

	log.Printf("created plan id=%s name=%s", ptrStr(plan.GetID()), ptrStr(plan.GetName()))
	return plan
}

// ── Create subscription ──────────────────────────────────────────────────────

func createSubscription(ctx context.Context, customer *types.Customer, plan *types.Plan) *types.Subscription {
	customerID := customer.GetID()
	planID := plan.GetID()

	if customerID == nil || planID == nil {
		log.Printf("createSubscription: customer or plan ID is nil")
		return nil
	}

	now := time.Now()

	req := types.CreateSubscriptionRequest{
		// Required fields.
		PlanID:   *planID,
		Currency: "USD",

		// Use the customer's FlexPrice internal ID.
		CustomerID: customerID,

		// Enum types — use the generated constants (not raw strings).
		BillingCadence: types.BillingCadenceRecurring,
		BillingPeriod:  types.BillingPeriodMonthly,

		// Pointer helpers for optional scalar fields.
		BillingPeriodCount: flexprice.Int64(1),
		CollectionMethod:   types.CollectionMethodChargeAutomatically.ToPointer(),
		StartDate:          flexprice.Pointer(now),
	}

	resp, err := client.Subscriptions.CreateSubscription(ctx, req,
		flexprice.WithIdempotencyKey(fmt.Sprintf("sub-%s-%s", *customerID, *planID)),
	)
	if err != nil {
		if errorutils.IsConflict(err) {
			log.Printf("subscription already exists — continuing")
			return nil
		}
		log.Printf("CreateSubscription error: %v", err)
		return nil
	}

	sub := resp.GetSubscription()
	if sub == nil {
		log.Printf("CreateSubscription returned a nil subscription")
		return nil
	}

	log.Printf("created subscription id=%s billing_period=%s",
		ptrStr(sub.GetID()),
		ptrBillingPeriod(sub.GetBillingPeriod()),
	)
	return sub
}

// ── Ingest events ────────────────────────────────────────────────────────────

func ingestEvents(ctx context.Context, customer *types.Customer) {
	externalCustomerID := ptrStr(customer.GetExternalID())
	if externalCustomerID == "" {
		log.Printf("ingestEvents: customer has no external_id")
		return
	}

	// ── Single event (sync) ──────────────────────────────────────────────────
	_, err := client.Events.IngestEvent(ctx, types.IngestEventRequest{
		EventName:          "api_request",
		ExternalCustomerID: externalCustomerID,
		Properties: map[string]string{
			"endpoint": "/v1/inference",
			"model":    "gpt-4o",
			"tokens":   "512",
		},
	})
	if err != nil {
		log.Printf("IngestEvent error: %v", err)
	} else {
		log.Printf("single event ingested for customer %s", externalCustomerID)
	}

	// ── Bulk event ingestion (sync) ──────────────────────────────────────────
	now := time.Now().UTC().Format(time.RFC3339)
	_, err = client.Events.IngestEventsBulk(ctx, types.BulkIngestEventRequest{
		Events: []types.IngestEventRequest{
			{
				EventName:          "storage_write",
				ExternalCustomerID: externalCustomerID,
				Properties:         map[string]string{"bytes": "1048576", "bucket": "user-data"},
				Timestamp:          flexprice.String(now),
			},
			{
				EventName:          "storage_read",
				ExternalCustomerID: externalCustomerID,
				Properties:         map[string]string{"bytes": "524288", "bucket": "user-data"},
				Timestamp:          flexprice.String(now),
			},
			{
				EventName:          "api_request",
				ExternalCustomerID: externalCustomerID,
				Properties:         map[string]string{"endpoint": "/v1/embed", "tokens": "128"},
				Timestamp:          flexprice.String(now),
			},
		},
	})
	if err != nil {
		log.Printf("IngestEventsBulk error: %v", err)
	} else {
		log.Printf("bulk events ingested for customer %s", externalCustomerID)
	}

	// ── Async event client ───────────────────────────────────────────────────
	// The async client batches events and flushes them in a background goroutine.
	asyncConfig := flexprice.DefaultAsyncConfig()
	asyncConfig.BatchSize = 25
	asyncConfig.FlushInterval = 200 * time.Millisecond
	asyncConfig.Debug = false

	asyncClient := client.NewAsyncClientWithConfig(asyncConfig)
	defer asyncClient.Close() // flushes remaining events and stops background goroutine

	// Enqueue is a fire-and-forget helper (event name + customer ID + properties).
	if err := asyncClient.Enqueue("async_api_call", externalCustomerID, map[string]interface{}{
		"endpoint": "/v1/completions",
		"tokens":   1024,
		"latency":  "142ms",
	}); err != nil {
		log.Printf("Enqueue error: %v", err)
	}

	// EnqueueWithOptions provides full control over every event field.
	if err := asyncClient.EnqueueWithOptions(flexprice.EventOptions{
		EventName:          "async_storage_write",
		ExternalCustomerID: externalCustomerID,
		EventID:            fmt.Sprintf("evt-%d", time.Now().UnixNano()),
		Source:             "go-sdk-example",
		Properties: map[string]interface{}{
			"bytes":  2097152,
			"region": "us-east-1",
		},
	}); err != nil {
		log.Printf("EnqueueWithOptions error: %v", err)
	}

	log.Printf("async events enqueued for customer %s", externalCustomerID)
}

// ── List invoices ────────────────────────────────────────────────────────────

func listInvoices(ctx context.Context, customer *types.Customer) {
	customerID := customer.GetID()
	if customerID == nil {
		log.Printf("listInvoices: customer has no ID")
		return
	}

	resp, err := client.Invoices.QueryInvoice(ctx, types.InvoiceFilter{
		CustomerID:    customerID,
		Limit:         flexprice.Int64(10),
		Offset:        flexprice.Int64(0),
		SkipLineItems: flexprice.Bool(true),
	})
	if err != nil {
		if errorutils.IsNotFound(err) {
			log.Printf("no invoices found for customer %s", *customerID)
			return
		}
		log.Printf("QueryInvoice error: %v", err)
		return
	}

	listResp := resp.GetListInvoicesResponse()
	invoices := listResp.GetItems()
	pagination := listResp.GetPagination()

	total := int64(0)
	if t := pagination.GetTotal(); t != nil {
		total = *t
	}
	log.Printf("invoices: total=%d fetched=%d", total, len(invoices))

	for _, inv := range invoices {
		status := ""
		if inv.InvoiceStatus != nil {
			status = string(*inv.InvoiceStatus)
		}
		log.Printf("  invoice id=%s status=%s amount_due=%s currency=%s",
			ptrStr(inv.GetID()),
			status,
			ptrStr(inv.AmountDue),
			ptrStr(inv.Currency),
		)
	}
}

// ── Cancel subscription ──────────────────────────────────────────────────────

func cancelSubscription(ctx context.Context, sub *types.Subscription) {
	subID := sub.GetID()
	if subID == nil {
		log.Printf("cancelSubscription: subscription has no ID")
		return
	}

	_, err := client.Subscriptions.CancelSubscription(
		ctx,
		*subID,
		types.CancelSubscriptionRequest{
			// CancellationType is required — use the generated enum constant.
			CancellationType: types.CancellationTypeEndOfPeriod,
			Reason:           flexprice.String("demo teardown"),
		},
	)
	if err != nil {
		log.Printf("CancelSubscription error: %v", err)
		return
	}

	log.Printf("subscription %s scheduled for cancellation at end of period", *subID)
}

// ── Helpers ──────────────────────────────────────────────────────────────────

// ptrStr dereferences a *string safely, returning "" for nil.
func ptrStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// ptrBillingPeriod dereferences a *BillingPeriod safely.
func ptrBillingPeriod(bp *types.BillingPeriod) string {
	if bp == nil {
		return ""
	}
	return string(*bp)
}
