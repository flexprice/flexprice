package e2eprobe

import (
	"context"
	"sync/atomic"
	"testing"

	flexprice "github.com/flexprice/go-sdk/v2"
	"github.com/flexprice/go-sdk/v2/models/dtos"
	"github.com/flexprice/go-sdk/v2/models/types"
)

// ── minimal fake ops ──────────────────────────────────────────────────

type fakeCustomerOps struct{ createCalled int32 }

func (f *fakeCustomerOps) Create(_ context.Context, _ types.DtoCreateCustomerRequest) (*dtos.CreateCustomerResponse, error) {
	atomic.AddInt32(&f.createCalled, 1)
	return &dtos.CreateCustomerResponse{}, nil
}
func (f *fakeCustomerOps) GetByExternalID(_ context.Context, _ string) (*dtos.GetCustomerByExternalIDResponse, error) {
	return &dtos.GetCustomerByExternalIDResponse{}, nil
}
func (f *fakeCustomerOps) Get(_ context.Context, _ string) (*dtos.GetCustomerResponse, error) {
	return &dtos.GetCustomerResponse{}, nil
}
func (f *fakeCustomerOps) GetEntitlements(_ context.Context, _ string) (*dtos.GetCustomerEntitlementsResponse, error) {
	return &dtos.GetCustomerEntitlementsResponse{}, nil
}
func (f *fakeCustomerOps) GetUsageSummary(_ context.Context, _ dtos.GetCustomerUsageSummaryRequest) (*dtos.GetCustomerUsageSummaryResponse, error) {
	return &dtos.GetCustomerUsageSummaryResponse{}, nil
}
func (f *fakeCustomerOps) Update(_ context.Context, _ types.DtoUpdateCustomerRequest, _, _ *string) (*dtos.UpdateCustomerResponse, error) {
	atomic.AddInt32(&f.createCalled, 1)
	return &dtos.UpdateCustomerResponse{}, nil
}
func (f *fakeCustomerOps) Delete(_ context.Context, _ string) (*dtos.DeleteCustomerResponse, error) {
	atomic.AddInt32(&f.createCalled, 1)
	return &dtos.DeleteCustomerResponse{}, nil
}
func (f *fakeCustomerOps) Query(_ context.Context, _ types.CustomerFilter) (*dtos.QueryCustomerResponse, error) {
	return &dtos.QueryCustomerResponse{}, nil
}

type fakePlanOps struct{ createCalled int32 }

func (f *fakePlanOps) Create(_ context.Context, _ types.DtoCreatePlanRequest) (*dtos.CreatePlanResponse, error) {
	atomic.AddInt32(&f.createCalled, 1)
	return &dtos.CreatePlanResponse{}, nil
}
func (f *fakePlanOps) Query(_ context.Context, _ types.PlanFilter) (*dtos.QueryPlanResponse, error) {
	return &dtos.QueryPlanResponse{}, nil
}
func (f *fakePlanOps) Get(_ context.Context, _ string) (*dtos.GetPlanResponse, error) {
	return &dtos.GetPlanResponse{}, nil
}

type fakeFeatureOps struct{ createCalled int32 }

func (f *fakeFeatureOps) Create(_ context.Context, _ types.DtoCreateFeatureRequest) (*dtos.CreateFeatureResponse, error) {
	atomic.AddInt32(&f.createCalled, 1)
	return &dtos.CreateFeatureResponse{}, nil
}
func (f *fakeFeatureOps) Query(_ context.Context, _ types.FeatureFilter) (*dtos.QueryFeatureResponse, error) {
	return &dtos.QueryFeatureResponse{}, nil
}

type fakeSubOps struct{ createCalled int32 }

func (f *fakeSubOps) Create(_ context.Context, _ types.DtoCreateSubscriptionRequest) (*dtos.CreateSubscriptionResponse, error) {
	atomic.AddInt32(&f.createCalled, 1)
	return &dtos.CreateSubscriptionResponse{}, nil
}
func (f *fakeSubOps) Get(_ context.Context, _ string) (*dtos.GetSubscriptionResponse, error) {
	return &dtos.GetSubscriptionResponse{}, nil
}
func (f *fakeSubOps) Cancel(_ context.Context, _ string, _ types.DtoCancelSubscriptionRequest) (*dtos.CancelSubscriptionResponse, error) {
	atomic.AddInt32(&f.createCalled, 1)
	return &dtos.CancelSubscriptionResponse{}, nil
}
func (f *fakeSubOps) Query(_ context.Context, _ types.SubscriptionFilter) (*dtos.QuerySubscriptionResponse, error) {
	return &dtos.QuerySubscriptionResponse{}, nil
}
func (f *fakeSubOps) ActivateSubscription(_ context.Context, _ string, _ types.DtoActivateDraftSubscriptionRequest) (*dtos.ActivateSubscriptionResponse, error) {
	atomic.AddInt32(&f.createCalled, 1)
	return &dtos.ActivateSubscriptionResponse{}, nil
}
func (f *fakeSubOps) GetEntitlements(_ context.Context, _ string, _ []string) (*dtos.GetSubscriptionEntitlementsResponse, error) {
	return &dtos.GetSubscriptionEntitlementsResponse{}, nil
}
func (f *fakeSubOps) GetUsage(_ context.Context, _ types.DtoGetUsageBySubscriptionRequest) (*dtos.GetSubscriptionUsageResponse, error) {
	return &dtos.GetSubscriptionUsageResponse{}, nil
}
func (f *fakeSubOps) CreateLineItem(_ context.Context, _ string, _ types.DtoCreateSubscriptionLineItemRequest) (*dtos.CreateSubscriptionLineItemResponse, error) {
	atomic.AddInt32(&f.createCalled, 1)
	return &dtos.CreateSubscriptionLineItemResponse{}, nil
}
func (f *fakeSubOps) UpdateLineItem(_ context.Context, _ string, _ types.DtoUpdateSubscriptionLineItemRequest) (*dtos.UpdateSubscriptionLineItemResponse, error) {
	atomic.AddInt32(&f.createCalled, 1)
	return &dtos.UpdateSubscriptionLineItemResponse{}, nil
}

type fakeWalletOps struct{ createCalled int32 }

func (f *fakeWalletOps) Create(_ context.Context, _ types.DtoCreateWalletRequest) (*dtos.CreateWalletResponse, error) {
	atomic.AddInt32(&f.createCalled, 1)
	return &dtos.CreateWalletResponse{}, nil
}
func (f *fakeWalletOps) Query(_ context.Context, _ types.WalletFilter) (*dtos.QueryWalletResponse, error) {
	return &dtos.QueryWalletResponse{}, nil
}
func (f *fakeWalletOps) GetWalletsByCustomerID(_ context.Context, _ string) (*dtos.GetWalletsByCustomerIDResponse, error) {
	return &dtos.GetWalletsByCustomerIDResponse{}, nil
}
func (f *fakeWalletOps) GetBalance(_ context.Context, _ string) (*dtos.GetWalletBalanceResponse, error) {
	return &dtos.GetWalletBalanceResponse{}, nil
}
func (f *fakeWalletOps) TopUp(_ context.Context, _ string, _ types.DtoTopUpWalletRequest) (*dtos.TopUpWalletResponse, error) {
	atomic.AddInt32(&f.createCalled, 1)
	return &dtos.TopUpWalletResponse{}, nil
}

type fakeEventOps struct{ ingestCalled int32 }

func (f *fakeEventOps) Ingest(_ context.Context, _ types.DtoIngestEventRequest) (*dtos.IngestEventResponse, error) {
	atomic.AddInt32(&f.ingestCalled, 1)
	return &dtos.IngestEventResponse{}, nil
}
func (f *fakeEventOps) GetUsageAnalytics(_ context.Context, _ types.DtoGetUsageAnalyticsRequest) (*dtos.GetUsageAnalyticsResponse, error) {
	return &dtos.GetUsageAnalyticsResponse{}, nil
}

type fakeInvoiceOps struct{ queryCalled int32 }

func (f *fakeInvoiceOps) Query(_ context.Context, _ types.InvoiceFilter) (*dtos.QueryInvoiceResponse, error) {
	atomic.AddInt32(&f.queryCalled, 1)
	return &dtos.QueryInvoiceResponse{}, nil
}
func (f *fakeInvoiceOps) Get(_ context.Context, _ string) (*dtos.GetInvoiceResponse, error) {
	atomic.AddInt32(&f.queryCalled, 1)
	return &dtos.GetInvoiceResponse{}, nil
}

type fakePriceOps struct{ createCalled int32 }

func (f *fakePriceOps) Create(_ context.Context, _ types.DtoCreatePriceRequest) (*dtos.CreatePriceResponse, error) {
	atomic.AddInt32(&f.createCalled, 1)
	return &dtos.CreatePriceResponse{}, nil
}
func (f *fakePriceOps) Query(_ context.Context, _ types.PriceFilter) (*dtos.QueryPriceResponse, error) {
	return &dtos.QueryPriceResponse{}, nil
}

type fakeAsyncOps struct{ enqueueCalled int32 }

func (f *fakeAsyncOps) Enqueue(_, _ string, _ map[string]any) error {
	atomic.AddInt32(&f.enqueueCalled, 1)
	return nil
}
func (f *fakeAsyncOps) EnqueueWithOptions(_ flexprice.EventOptions) error {
	atomic.AddInt32(&f.enqueueCalled, 1)
	return nil
}
func (f *fakeAsyncOps) Flush() error  { return nil }
func (f *fakeAsyncOps) Close() error  { return nil }

// ── fake Client assembling the ops above ─────────────────────────────

type fakeInnerClient struct {
	customers *fakeCustomerOps
	plans     *fakePlanOps
	prices    *fakePriceOps
	features  *fakeFeatureOps
	subs      *fakeSubOps
	wallets   *fakeWalletOps
	events    *fakeEventOps
	invoices  *fakeInvoiceOps
	async     *fakeAsyncOps
}

func newFakeInnerClient() *fakeInnerClient {
	return &fakeInnerClient{
		customers: &fakeCustomerOps{},
		plans:     &fakePlanOps{},
		prices:    &fakePriceOps{},
		features:  &fakeFeatureOps{},
		subs:      &fakeSubOps{},
		wallets:   &fakeWalletOps{},
		events:    &fakeEventOps{},
		invoices:  &fakeInvoiceOps{},
		async:     &fakeAsyncOps{},
	}
}

func (c *fakeInnerClient) Customers() CustomerOps         { return c.customers }
func (c *fakeInnerClient) Plans() PlanOps                 { return c.plans }
func (c *fakeInnerClient) Prices() PriceOps               { return c.prices }
func (c *fakeInnerClient) Features() FeatureOps           { return c.features }
func (c *fakeInnerClient) Subscriptions() SubscriptionOps { return c.subs }
func (c *fakeInnerClient) Wallets() WalletOps             { return c.wallets }
func (c *fakeInnerClient) Events() EventOps               { return c.events }
func (c *fakeInnerClient) Invoices() InvoiceOps           { return c.invoices }
func (c *fakeInnerClient) NewAsyncEventClient() AsyncEventClient { return c.async }

// ── Tests ─────────────────────────────────────────────────────────────

func TestDryRunClient_MutatingMethodsAreNoOps(t *testing.T) {
	inner := newFakeInnerClient()
	dry := NewDryRunClient(inner, nil)
	ctx := context.Background()

	// Call all mutating methods; none should reach inner.
	if _, err := dry.Customers().Create(ctx, types.DtoCreateCustomerRequest{ExternalID: "x"}); err != nil {
		t.Fatalf("Customers.Create: %v", err)
	}
	if _, err := dry.Plans().Create(ctx, types.DtoCreatePlanRequest{Name: "p"}); err != nil {
		t.Fatalf("Plans.Create: %v", err)
	}
	if _, err := dry.Prices().Create(ctx, types.DtoCreatePriceRequest{}); err != nil {
		t.Fatalf("Prices.Create: %v", err)
	}
	if _, err := dry.Features().Create(ctx, types.DtoCreateFeatureRequest{Name: "f"}); err != nil {
		t.Fatalf("Features.Create: %v", err)
	}
	if _, err := dry.Subscriptions().Create(ctx, types.DtoCreateSubscriptionRequest{}); err != nil {
		t.Fatalf("Subscriptions.Create: %v", err)
	}
	if _, err := dry.Wallets().Create(ctx, types.DtoCreateWalletRequest{}); err != nil {
		t.Fatalf("Wallets.Create: %v", err)
	}
	if _, err := dry.Events().Ingest(ctx, types.DtoIngestEventRequest{EventName: "e"}); err != nil {
		t.Fatalf("Events.Ingest: %v", err)
	}
	asyncClient := dry.NewAsyncEventClient()
	if err := asyncClient.Enqueue("e", "c", nil); err != nil {
		t.Fatalf("Async.Enqueue: %v", err)
	}

	// Assert inner was never reached.
	if inner.customers.createCalled != 0 {
		t.Errorf("inner.customers.Create was called %d times; want 0", inner.customers.createCalled)
	}
	if inner.plans.createCalled != 0 {
		t.Errorf("inner.plans.Create was called %d times; want 0", inner.plans.createCalled)
	}
	if inner.prices.createCalled != 0 {
		t.Errorf("inner.prices.Create was called %d times; want 0", inner.prices.createCalled)
	}
	if inner.features.createCalled != 0 {
		t.Errorf("inner.features.Create was called %d times; want 0", inner.features.createCalled)
	}
	if inner.subs.createCalled != 0 {
		t.Errorf("inner.subs.Create was called %d times; want 0", inner.subs.createCalled)
	}
	if inner.wallets.createCalled != 0 {
		t.Errorf("inner.wallets.Create was called %d times; want 0", inner.wallets.createCalled)
	}
	if inner.events.ingestCalled != 0 {
		t.Errorf("inner.events.Ingest was called %d times; want 0", inner.events.ingestCalled)
	}
	if inner.async.enqueueCalled != 0 {
		t.Errorf("inner.async.Enqueue was called %d times; want 0", inner.async.enqueueCalled)
	}
}

func TestDryRunClient_ReadsPassThrough(t *testing.T) {
	inner := newFakeInnerClient()
	dry := NewDryRunClient(inner, nil)
	ctx := context.Background()

	// Read-only calls must reach the inner client.
	if _, err := dry.Customers().GetByExternalID(ctx, "ext-1"); err != nil {
		t.Fatalf("GetByExternalID: %v", err)
	}
	if _, err := dry.Plans().Query(ctx, types.PlanFilter{}); err != nil {
		t.Fatalf("Plans.Query: %v", err)
	}
	if _, err := dry.Wallets().GetBalance(ctx, "wallet-1"); err != nil {
		t.Fatalf("Wallets.GetBalance: %v", err)
	}
	if _, err := dry.Subscriptions().Query(ctx, types.SubscriptionFilter{}); err != nil {
		t.Fatalf("Subscriptions.Query: %v", err)
	}
	// InvoiceOps is delegated directly to inner; calling it should reach inner.
	if _, err := dry.Invoices().Query(ctx, types.InvoiceFilter{}); err != nil {
		t.Fatalf("Invoices.Query: %v", err)
	}
	if inner.invoices.queryCalled == 0 {
		t.Error("inner.invoices.Query was never called; reads should pass through")
	}
}

func TestDryRunClient_PopulatedIDs(t *testing.T) {
	inner := newFakeInnerClient()
	dry := NewDryRunClient(inner, nil)
	ctx := context.Background()

	walletResp, err := dry.Wallets().Create(ctx, types.DtoCreateWalletRequest{})
	if err != nil {
		t.Fatalf("Wallets.Create: %v", err)
	}
	if walletResp.DtoWalletResponse == nil || walletResp.DtoWalletResponse.ID == nil || *walletResp.DtoWalletResponse.ID == "" {
		t.Error("Wallets.Create dry-run response should have a non-empty fake ID")
	}

	subResp, err := dry.Subscriptions().Create(ctx, types.DtoCreateSubscriptionRequest{})
	if err != nil {
		t.Fatalf("Subscriptions.Create: %v", err)
	}
	if subResp.DtoSubscriptionResponse == nil || subResp.DtoSubscriptionResponse.ID == nil || *subResp.DtoSubscriptionResponse.ID == "" {
		t.Error("Subscriptions.Create dry-run response should have a non-empty fake ID")
	}

	planResp, err := dry.Plans().Create(ctx, types.DtoCreatePlanRequest{Name: "p"})
	if err != nil {
		t.Fatalf("Plans.Create: %v", err)
	}
	if planResp.DtoPlanResponse == nil || planResp.DtoPlanResponse.ID == nil || *planResp.DtoPlanResponse.ID == "" {
		t.Error("Plans.Create dry-run response should have a non-empty fake ID")
	}

	featureResp, err := dry.Features().Create(ctx, types.DtoCreateFeatureRequest{Name: "f"})
	if err != nil {
		t.Fatalf("Features.Create: %v", err)
	}
	if featureResp.DtoFeatureResponse == nil || featureResp.DtoFeatureResponse.ID == nil || *featureResp.DtoFeatureResponse.ID == "" {
		t.Error("Features.Create dry-run response should have a non-empty fake ID")
	}
	if featureResp.DtoFeatureResponse.MeterID == nil || *featureResp.DtoFeatureResponse.MeterID == "" {
		t.Error("Features.Create dry-run response should have a non-empty fake MeterID")
	}
}
