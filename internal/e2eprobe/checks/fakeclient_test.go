package checks

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/flexprice/flexprice/internal/e2eprobe"
	flexprice "github.com/flexprice/go-sdk/v2"
	"github.com/flexprice/go-sdk/v2/models/dtos"
	"github.com/flexprice/go-sdk/v2/models/types"
)

type fakeClient struct {
	customers fakeCustomers
	plans     fakePlans
	prices    fakePrices
	features  fakeFeatures
	meters    fakeMeters
	subs      fakeSubscriptions
	wallets   fakeWallets
	events    fakeEvents
	invoices  fakeInvoices
	async     *fakeAsyncEvents
}

func newFakeClient() *fakeClient {
	return &fakeClient{
		customers: fakeCustomers{byExt: map[string]string{}},
		meters:    fakeMeters{},
		async:     &fakeAsyncEvents{},
	}
}

func (c *fakeClient) Customers() e2eprobe.CustomerOps         { return &c.customers }
func (c *fakeClient) Plans() e2eprobe.PlanOps                 { return &c.plans }
func (c *fakeClient) Prices() e2eprobe.PriceOps               { return &c.prices }
func (c *fakeClient) Features() e2eprobe.FeatureOps           { return &c.features }
func (c *fakeClient) Meters() e2eprobe.MeterOps               { return &c.meters }
func (c *fakeClient) Subscriptions() e2eprobe.SubscriptionOps { return &c.subs }
func (c *fakeClient) Wallets() e2eprobe.WalletOps             { return &c.wallets }
func (c *fakeClient) Events() e2eprobe.EventOps               { return &c.events }
func (c *fakeClient) Invoices() e2eprobe.InvoiceOps           { return &c.invoices }
func (c *fakeClient) NewAsyncEventClient() e2eprobe.AsyncEventClient {
	return c.async
}

// --- Customers ---

type fakeCustomers struct {
	mu      sync.Mutex
	created []types.DtoCreateCustomerRequest
	byExt   map[string]string
	getErr  error
}

func (f *fakeCustomers) Create(_ context.Context, req types.DtoCreateCustomerRequest) (*dtos.CreateCustomerResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	id := "cust_" + req.ExternalID
	f.byExt[req.ExternalID] = id
	f.created = append(f.created, req)
	return &dtos.CreateCustomerResponse{}, nil
}
func (f *fakeCustomers) GetByExternalID(_ context.Context, ext string) (*dtos.GetCustomerByExternalIDResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.getErr != nil {
		return nil, f.getErr
	}
	id, ok := f.byExt[ext]
	if !ok {
		return nil, errors.New("not found")
	}
	return &dtos.GetCustomerByExternalIDResponse{
		DtoCustomerResponse: &types.DtoCustomerResponse{ID: &id},
	}, nil
}
func (f *fakeCustomers) Get(_ context.Context, _ string) (*dtos.GetCustomerResponse, error) {
	return &dtos.GetCustomerResponse{}, nil
}
func (f *fakeCustomers) GetEntitlements(_ context.Context, _ string) (*dtos.GetCustomerEntitlementsResponse, error) {
	return &dtos.GetCustomerEntitlementsResponse{}, nil
}
func (f *fakeCustomers) GetUsageSummary(_ context.Context, _ dtos.GetCustomerUsageSummaryRequest) (*dtos.GetCustomerUsageSummaryResponse, error) {
	return &dtos.GetCustomerUsageSummaryResponse{}, nil
}
func (f *fakeCustomers) Update(_ context.Context, _ types.DtoUpdateCustomerRequest, _, _ *string) (*dtos.UpdateCustomerResponse, error) {
	return &dtos.UpdateCustomerResponse{}, nil
}
func (f *fakeCustomers) Delete(_ context.Context, _ string) (*dtos.DeleteCustomerResponse, error) {
	return &dtos.DeleteCustomerResponse{}, nil
}

// --- Plans ---

type fakePlans struct {
	mu      sync.Mutex
	created []types.DtoCreatePlanRequest
	plans   []types.DtoPlanResponse
}

func (f *fakePlans) Create(_ context.Context, req types.DtoCreatePlanRequest) (*dtos.CreatePlanResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	id := fmt.Sprintf("plan_%d", len(f.plans)+1)
	f.created = append(f.created, req)
	f.plans = append(f.plans, types.DtoPlanResponse{ID: &id})
	return &dtos.CreatePlanResponse{DtoPlanResponse: &types.DtoPlanResponse{ID: &id}}, nil
}
func (f *fakePlans) Query(_ context.Context, _ types.PlanFilter) (*dtos.QueryPlanResponse, error) {
	return &dtos.QueryPlanResponse{}, nil
}
func (f *fakePlans) Get(_ context.Context, _ string) (*dtos.GetPlanResponse, error) {
	return &dtos.GetPlanResponse{}, nil
}

// --- Prices ---

type fakePrices struct {
	mu      sync.Mutex
	created []types.DtoCreatePriceRequest
}

func (f *fakePrices) Create(_ context.Context, req types.DtoCreatePriceRequest) (*dtos.CreatePriceResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.created = append(f.created, req)
	return &dtos.CreatePriceResponse{}, nil
}
func (f *fakePrices) Query(_ context.Context, _ types.PriceFilter) (*dtos.QueryPriceResponse, error) {
	return &dtos.QueryPriceResponse{}, nil
}

// --- Features ---

type fakeFeatures struct {
	mu      sync.Mutex
	created []types.DtoCreateFeatureRequest
}

func (f *fakeFeatures) Create(_ context.Context, req types.DtoCreateFeatureRequest) (*dtos.CreateFeatureResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.created = append(f.created, req)
	return &dtos.CreateFeatureResponse{}, nil
}
func (f *fakeFeatures) Query(_ context.Context, _ types.FeatureFilter) (*dtos.QueryFeatureResponse, error) {
	return &dtos.QueryFeatureResponse{}, nil
}

// --- Meters ---

type fakeMeters struct {
	mu      sync.Mutex
	meters  []e2eprobe.Meter
	created []e2eprobe.CreateMeterRequest
}

func (f *fakeMeters) List(_ context.Context) ([]e2eprobe.Meter, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]e2eprobe.Meter, len(f.meters))
	copy(out, f.meters)
	return out, nil
}
func (f *fakeMeters) Create(_ context.Context, req e2eprobe.CreateMeterRequest) (*e2eprobe.Meter, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	m := e2eprobe.Meter{
		ID:          "meter_" + req.EventName,
		EventName:   req.EventName,
		Name:        req.Name,
		Aggregation: req.Aggregation,
		Filters:     req.Filters,
		Metadata:    req.Metadata,
	}
	f.meters = append(f.meters, m)
	f.created = append(f.created, req)
	return &m, nil
}
func (f *fakeMeters) Get(_ context.Context, id string) (*e2eprobe.Meter, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, m := range f.meters {
		if m.ID == id {
			return &m, nil
		}
	}
	return nil, errors.New("not found")
}

// --- Subscriptions ---

type fakeSubscriptions struct {
	mu        sync.Mutex
	created   []types.DtoCreateSubscriptionRequest
	cancelled []string
	nextID    int
	subs      map[string]types.DtoSubscriptionResponse
	subErr    error
}

func (f *fakeSubscriptions) Create(_ context.Context, req types.DtoCreateSubscriptionRequest) (*dtos.CreateSubscriptionResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.subErr != nil {
		return nil, f.subErr
	}
	f.nextID++
	id := fmt.Sprintf("sub_%d", f.nextID)
	if f.subs == nil {
		f.subs = map[string]types.DtoSubscriptionResponse{}
	}
	f.subs[id] = types.DtoSubscriptionResponse{ID: &id}
	f.created = append(f.created, req)
	return &dtos.CreateSubscriptionResponse{DtoSubscriptionResponse: &types.DtoSubscriptionResponse{ID: &id}}, nil
}
func (f *fakeSubscriptions) Get(_ context.Context, id string) (*dtos.GetSubscriptionResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if sub, ok := f.subs[id]; ok {
		return &dtos.GetSubscriptionResponse{DtoSubscriptionResponse: &sub}, nil
	}
	return &dtos.GetSubscriptionResponse{}, nil
}
func (f *fakeSubscriptions) Cancel(_ context.Context, id string, _ types.DtoCancelSubscriptionRequest) (*dtos.CancelSubscriptionResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cancelled = append(f.cancelled, id)
	return &dtos.CancelSubscriptionResponse{}, nil
}
func (f *fakeSubscriptions) Query(_ context.Context, _ types.SubscriptionFilter) (*dtos.QuerySubscriptionResponse, error) {
	return &dtos.QuerySubscriptionResponse{}, nil
}
func (f *fakeSubscriptions) GetEntitlements(_ context.Context, _ string, _ []string) (*dtos.GetSubscriptionEntitlementsResponse, error) {
	return &dtos.GetSubscriptionEntitlementsResponse{}, nil
}
func (f *fakeSubscriptions) GetUsage(_ context.Context, _ types.DtoGetUsageBySubscriptionRequest) (*dtos.GetSubscriptionUsageResponse, error) {
	return &dtos.GetSubscriptionUsageResponse{}, nil
}
func (f *fakeSubscriptions) CreateLineItem(_ context.Context, _ string, _ types.DtoCreateSubscriptionLineItemRequest) (*dtos.CreateSubscriptionLineItemResponse, error) {
	return &dtos.CreateSubscriptionLineItemResponse{}, nil
}
func (f *fakeSubscriptions) UpdateLineItem(_ context.Context, _ string, _ types.DtoUpdateSubscriptionLineItemRequest) (*dtos.UpdateSubscriptionLineItemResponse, error) {
	return &dtos.UpdateSubscriptionLineItemResponse{}, nil
}

// --- Wallets ---

type fakeWallets struct {
	mu      sync.Mutex
	created []types.DtoCreateWalletRequest
	// walletItems allows tests to populate wallets returned by Query.
	walletItems []types.DtoWalletResponse
	balance     string
	balErr      error
}

func (f *fakeWallets) Create(_ context.Context, req types.DtoCreateWalletRequest) (*dtos.CreateWalletResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.created = append(f.created, req)
	return &dtos.CreateWalletResponse{}, nil
}
func (f *fakeWallets) Query(_ context.Context, _ types.WalletFilter) (*dtos.QueryWalletResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.walletItems) == 0 {
		return &dtos.QueryWalletResponse{}, nil
	}
	return &dtos.QueryWalletResponse{
		ListResponseDtoWalletResponse: &types.ListResponseDtoWalletResponse{
			Items: f.walletItems,
		},
	}, nil
}
func (f *fakeWallets) GetBalance(_ context.Context, _ string) (*dtos.GetWalletBalanceResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.balErr != nil {
		return nil, f.balErr
	}
	if f.balance == "" {
		return &dtos.GetWalletBalanceResponse{}, nil
	}
	return &dtos.GetWalletBalanceResponse{
		DtoWalletBalanceResponse: &types.DtoWalletBalanceResponse{Balance: &f.balance},
	}, nil
}
func (f *fakeWallets) TopUp(_ context.Context, _ string, _ types.DtoTopUpWalletRequest) (*dtos.TopUpWalletResponse, error) {
	return &dtos.TopUpWalletResponse{}, nil
}

// --- Events ---

type fakeEvents struct {
	mu        sync.Mutex
	ingested  []types.DtoIngestEventRequest
	analytics int
	anaErr    error
}

func (f *fakeEvents) Ingest(_ context.Context, req types.DtoIngestEventRequest) (*dtos.IngestEventResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ingested = append(f.ingested, req)
	return &dtos.IngestEventResponse{}, nil
}
func (f *fakeEvents) GetUsageAnalytics(_ context.Context, _ types.DtoGetUsageAnalyticsRequest) (*dtos.GetUsageAnalyticsResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.analytics++
	if f.anaErr != nil {
		return nil, f.anaErr
	}
	return &dtos.GetUsageAnalyticsResponse{}, nil
}

// --- Invoices ---

type fakeInvoices struct {
	mu       sync.Mutex
	queries  int
	queryErr error
	invoices []types.DtoInvoiceResponse
}

func (f *fakeInvoices) Query(_ context.Context, _ types.InvoiceFilter) (*dtos.QueryInvoiceResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.queries++
	if f.queryErr != nil {
		return nil, f.queryErr
	}
	if len(f.invoices) == 0 {
		return &dtos.QueryInvoiceResponse{}, nil
	}
	return &dtos.QueryInvoiceResponse{
		DtoListInvoicesResponse: &types.DtoListInvoicesResponse{Items: f.invoices},
	}, nil
}
func (f *fakeInvoices) Get(_ context.Context, _ string) (*dtos.GetInvoiceResponse, error) {
	return &dtos.GetInvoiceResponse{}, nil
}

// --- Async events ---

type fakeAsyncEvents struct {
	mu      sync.Mutex
	queued  int
	flushed int
	closed  bool
}

func (f *fakeAsyncEvents) Enqueue(_ string, _ string, _ map[string]any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.queued++
	return nil
}
func (f *fakeAsyncEvents) EnqueueWithOptions(_ flexprice.EventOptions) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.queued++
	return nil
}
func (f *fakeAsyncEvents) Flush() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.flushed++
	return nil
}
func (f *fakeAsyncEvents) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
	return nil
}

// --- helpers (strPtr is defined in seed_ensure.go) ---
