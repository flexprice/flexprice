package checks

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"

	"github.com/flexprice/flexprice/internal/e2eprobe"
	flexprice "github.com/flexprice/go-sdk/v2"
	"github.com/flexprice/go-sdk/v2/models/dtos"
	sdkerrors "github.com/flexprice/go-sdk/v2/models/errors"
	"github.com/flexprice/go-sdk/v2/models/types"
)


type fakeClient struct {
	customers fakeCustomers
	plans     fakePlans
	prices    fakePrices
	features  fakeFeatures
	subs      fakeSubscriptions
	wallets   fakeWallets
	events    fakeEvents
	invoices  fakeInvoices
	async     *fakeAsyncEvents
}

func newFakeClient() *fakeClient {
	return &fakeClient{
		customers: fakeCustomers{byExt: map[string]string{}},
		async:     &fakeAsyncEvents{},
	}
}

func (c *fakeClient) Customers() e2eprobe.CustomerOps         { return &c.customers }
func (c *fakeClient) Plans() e2eprobe.PlanOps                 { return &c.plans }
func (c *fakeClient) Prices() e2eprobe.PriceOps               { return &c.prices }
func (c *fakeClient) Features() e2eprobe.FeatureOps           { return &c.features }
func (c *fakeClient) Subscriptions() e2eprobe.SubscriptionOps { return &c.subs }
func (c *fakeClient) Wallets() e2eprobe.WalletOps             { return &c.wallets }
func (c *fakeClient) Events() e2eprobe.EventOps               { return &c.events }
func (c *fakeClient) Invoices() e2eprobe.InvoiceOps           { return &c.invoices }
func (c *fakeClient) NewAsyncEventClient() e2eprobe.AsyncEventClient {
	return c.async
}

// --- Customers ---

type fakeCustomers struct {
	mu          sync.Mutex
	created     []types.CreateCustomerRequest
	byExt       map[string]string
	getErr      error
	deleted     []string // internal customer IDs passed to Delete
	queryResult []types.CustomerResponse
}

func (f *fakeCustomers) Create(_ context.Context, req types.CreateCustomerRequest) (*dtos.CreateCustomerResponse, error) {
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
	// Check byExt first — entries added by Create always succeed.
	id, ok := f.byExt[ext]
	if ok {
		return &dtos.GetCustomerByExternalIDResponse{
			CustomerResponse: &types.CustomerResponse{ID: &id},
		}, nil
	}
	// getErr simulates an injected error from tests.
	if f.getErr != nil {
		return nil, f.getErr
	}
	// Default not-found: surface as a proper *sdkerrors.APIError so production
	// callers can use errors.As(err, &apiErr) + apiErr.StatusCode == 404.
	return nil, &sdkerrors.APIError{StatusCode: http.StatusNotFound, Message: "not found"}
}

// ensure errors import is exercised (used by seed_ensure_test).
var _ = errors.New
func (f *fakeCustomers) Get(_ context.Context, _ string) (*dtos.GetCustomerResponse, error) {
	return &dtos.GetCustomerResponse{}, nil
}
func (f *fakeCustomers) GetEntitlements(_ context.Context, _ string) (*dtos.GetCustomerEntitlementsResponse, error) {
	return &dtos.GetCustomerEntitlementsResponse{}, nil
}
func (f *fakeCustomers) GetUsageSummary(_ context.Context, _ dtos.GetCustomerUsageSummaryRequest) (*dtos.GetCustomerUsageSummaryResponse, error) {
	return &dtos.GetCustomerUsageSummaryResponse{}, nil
}
func (f *fakeCustomers) Update(_ context.Context, _ types.UpdateCustomerRequest, _, _ *string) (*dtos.UpdateCustomerResponse, error) {
	return &dtos.UpdateCustomerResponse{}, nil
}
func (f *fakeCustomers) Delete(_ context.Context, id string) (*dtos.DeleteCustomerResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleted = append(f.deleted, id)
	return &dtos.DeleteCustomerResponse{}, nil
}
func (f *fakeCustomers) Query(_ context.Context, _ types.CustomerFilter) (*dtos.QueryCustomerResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.queryResult) == 0 {
		return &dtos.QueryCustomerResponse{}, nil
	}
	return &dtos.QueryCustomerResponse{
		ListCustomersResponse: &types.ListCustomersResponse{Items: f.queryResult},
	}, nil
}

// --- Plans ---

type fakePlans struct {
	mu      sync.Mutex
	created []types.CreatePlanRequest
	plans   []types.PlanResponse
}

func (f *fakePlans) Create(_ context.Context, req types.CreatePlanRequest) (*dtos.CreatePlanResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	id := fmt.Sprintf("plan_%d", len(f.plans)+1)
	f.created = append(f.created, req)
	plan := types.PlanResponse{ID: &id, LookupKey: req.LookupKey}
	f.plans = append(f.plans, plan)
	return &dtos.CreatePlanResponse{PlanResponse: &types.PlanResponse{ID: &id, LookupKey: req.LookupKey}}, nil
}
func (f *fakePlans) Query(_ context.Context, filter types.PlanFilter) (*dtos.QueryPlanResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var matched []types.PlanResponse
	for _, p := range f.plans {
		if filter.LookupKey != nil && p.LookupKey != nil && *filter.LookupKey != *p.LookupKey {
			continue
		}
		matched = append(matched, p)
	}
	if len(matched) == 0 {
		return &dtos.QueryPlanResponse{}, nil
	}
	return &dtos.QueryPlanResponse{
		ListPlansResponse: &types.ListPlansResponse{Items: matched},
	}, nil
}
func (f *fakePlans) Get(_ context.Context, _ string) (*dtos.GetPlanResponse, error) {
	return &dtos.GetPlanResponse{}, nil
}

// --- Prices ---

type fakePrices struct {
	mu      sync.Mutex
	created []types.CreatePriceRequest
}

func (f *fakePrices) Create(_ context.Context, req types.CreatePriceRequest) (*dtos.CreatePriceResponse, error) {
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
	mu       sync.Mutex
	created  []types.CreateFeatureRequest
	features []types.FeatureResponse
}

func (f *fakeFeatures) Create(_ context.Context, req types.CreateFeatureRequest) (*dtos.CreateFeatureResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	id := fmt.Sprintf("feat_%d", len(f.features)+1)
	meterID := fmt.Sprintf("meter_%d", len(f.features)+1)
	feat := types.FeatureResponse{ID: &id, LookupKey: req.LookupKey, MeterID: &meterID}
	f.features = append(f.features, feat)
	f.created = append(f.created, req)
	return &dtos.CreateFeatureResponse{FeatureResponse: &feat}, nil
}
func (f *fakeFeatures) Query(_ context.Context, filter types.FeatureFilter) (*dtos.QueryFeatureResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	// Build a set of lookup keys to match.
	wantKeys := map[string]bool{}
	for _, k := range filter.LookupKeys {
		wantKeys[k] = true
	}
	if filter.LookupKey != nil {
		wantKeys[*filter.LookupKey] = true
	}
	var matched []types.FeatureResponse
	for _, feat := range f.features {
		if len(wantKeys) == 0 {
			matched = append(matched, feat)
			continue
		}
		if feat.LookupKey != nil && wantKeys[*feat.LookupKey] {
			matched = append(matched, feat)
		}
	}
	if len(matched) == 0 {
		return &dtos.QueryFeatureResponse{}, nil
	}
	return &dtos.QueryFeatureResponse{
		ListFeaturesResponse: &types.ListFeaturesResponse{Items: matched},
	}, nil
}

// --- Subscriptions ---

type fakeSubscriptions struct {
	mu        sync.Mutex
	created   []types.CreateSubscriptionRequest
	cancelled []string
	gets      int
	nextID    int
	subs      map[string]types.SubscriptionResponse
	subErr    error
	cancelErr error
}

func (f *fakeSubscriptions) Create(_ context.Context, req types.CreateSubscriptionRequest) (*dtos.CreateSubscriptionResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.subErr != nil {
		return nil, f.subErr
	}
	f.nextID++
	id := fmt.Sprintf("sub_%d", f.nextID)
	if f.subs == nil {
		f.subs = map[string]types.SubscriptionResponse{}
	}
	f.subs[id] = types.SubscriptionResponse{ID: &id}
	f.created = append(f.created, req)
	return &dtos.CreateSubscriptionResponse{SubscriptionResponse: &types.SubscriptionResponse{ID: &id}}, nil
}
func (f *fakeSubscriptions) Get(_ context.Context, id string) (*dtos.GetSubscriptionResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.gets++
	// Allow tests to inject a specific error (e.g. *sdkerrors.APIError{404}) via subErr.
	if f.subErr != nil {
		return nil, f.subErr
	}
	if f.subs == nil {
		return nil, errors.New("subscription not found")
	}
	sub, ok := f.subs[id]
	if !ok {
		return nil, errors.New("subscription not found")
	}
	return &dtos.GetSubscriptionResponse{SubscriptionResponse: &sub}, nil
}
func (f *fakeSubscriptions) Cancel(_ context.Context, id string, _ types.CancelSubscriptionRequest) (*dtos.CancelSubscriptionResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.cancelErr != nil {
		return nil, f.cancelErr
	}
	f.cancelled = append(f.cancelled, id)
	return &dtos.CancelSubscriptionResponse{}, nil
}
func (f *fakeSubscriptions) Query(_ context.Context, filter types.SubscriptionFilter) (*dtos.QuerySubscriptionResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var matched []types.SubscriptionResponse
	for _, sub := range f.subs {
		if filter.ExternalCustomerID != nil || filter.PlanID != nil {
			// Only return if it matches all provided filters; since fakeSubscriptions
			// stores by ID and doesn't track ExternalCustomerID/PlanID, return empty
			// unless the caller pre-populated desired subs via direct map manipulation.
			continue
		}
		matched = append(matched, sub)
	}
	if len(matched) == 0 {
		return &dtos.QuerySubscriptionResponse{}, nil
	}
	return &dtos.QuerySubscriptionResponse{
		ListSubscriptionsResponse: &types.ListSubscriptionsResponse{Items: matched},
	}, nil
}
func (f *fakeSubscriptions) ActivateSubscription(_ context.Context, _ string, _ types.ActivateDraftSubscriptionRequest) (*dtos.ActivateSubscriptionResponse, error) {
	return &dtos.ActivateSubscriptionResponse{}, nil
}
func (f *fakeSubscriptions) GetEntitlements(_ context.Context, _ string, _ []string) (*dtos.GetSubscriptionEntitlementsResponse, error) {
	return &dtos.GetSubscriptionEntitlementsResponse{}, nil
}
func (f *fakeSubscriptions) GetUsage(_ context.Context, _ types.GetUsageBySubscriptionRequest) (*dtos.GetSubscriptionUsageResponse, error) {
	return &dtos.GetSubscriptionUsageResponse{}, nil
}
func (f *fakeSubscriptions) CreateLineItem(_ context.Context, _ string, _ types.CreateSubscriptionLineItemRequest) (*dtos.CreateSubscriptionLineItemResponse, error) {
	return &dtos.CreateSubscriptionLineItemResponse{}, nil
}
func (f *fakeSubscriptions) UpdateLineItem(_ context.Context, _ string, _ types.UpdateSubscriptionLineItemRequest) (*dtos.UpdateSubscriptionLineItemResponse, error) {
	return &dtos.UpdateSubscriptionLineItemResponse{}, nil
}

// --- Wallets ---

type fakeWallets struct {
	mu      sync.Mutex
	created []types.CreateWalletRequest
	// walletItems allows tests to populate wallets returned by Query.
	walletItems []types.WalletResponse
	// walletsByCustomerID maps internal customer ID → wallets (for GetWalletsByCustomerID).
	walletsByCustomerID map[string][]types.WalletResponse
	balance             string
	balErr              error
	topUpErr            error
	// topUpCalls records amounts passed to TopUp for test assertions.
	topUpCalls []string
	// incrementBalanceOnTopUp, when true, adds the TopUp amount to balance.
	incrementBalanceOnTopUp bool
}

func (f *fakeWallets) Create(_ context.Context, req types.CreateWalletRequest) (*dtos.CreateWalletResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	walletID := fmt.Sprintf("wallet_%d", len(f.created)+1)
	f.created = append(f.created, req)
	return &dtos.CreateWalletResponse{
		WalletResponse: &types.WalletResponse{ID: &walletID},
	}, nil
}
func (f *fakeWallets) GetWalletsByCustomerID(_ context.Context, customerID string) (*dtos.GetWalletsByCustomerIDResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.walletsByCustomerID != nil {
		if wallets, ok := f.walletsByCustomerID[customerID]; ok {
			return &dtos.GetWalletsByCustomerIDResponse{WalletResponses: wallets}, nil
		}
	}
	return &dtos.GetWalletsByCustomerIDResponse{}, nil
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
		WalletBalanceResponse: &types.WalletBalanceResponse{Balance: &f.balance},
	}, nil
}
func (f *fakeWallets) TopUp(_ context.Context, _ string, req types.TopUpWalletRequest) (*dtos.TopUpWalletResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.topUpErr != nil {
		return nil, f.topUpErr
	}
	if req.Amount != nil {
		f.topUpCalls = append(f.topUpCalls, *req.Amount)
	}
	if f.incrementBalanceOnTopUp && req.Amount != nil {
		var amt, cur float64
		fmt.Sscanf(*req.Amount, "%f", &amt)
		fmt.Sscanf(f.balance, "%f", &cur)
		f.balance = fmt.Sprintf("%.4f", cur+amt)
	}
	return &dtos.TopUpWalletResponse{}, nil
}

// --- Events ---

type fakeEvents struct {
	mu           sync.Mutex
	ingested     []types.IngestEventRequest
	analytics    int
	anaErr       error
	// analyticsItems, when set, is returned in GetUsageAnalytics responses.
	analyticsItems []types.UsageAnalyticItem
	// listRawItems, when set, is returned in ListRaw responses. Otherwise
	// ListRaw echoes back the ingested events that match the filter.
	listRawItems []types.Event
	listRawErr   error
}

func (f *fakeEvents) Ingest(_ context.Context, req types.IngestEventRequest) (*dtos.IngestEventResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ingested = append(f.ingested, req)
	return &dtos.IngestEventResponse{}, nil
}
func (f *fakeEvents) GetUsageAnalytics(_ context.Context, _ types.GetUsageAnalyticsRequest) (*dtos.GetUsageAnalyticsResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.analytics++
	if f.anaErr != nil {
		return nil, f.anaErr
	}
	if len(f.analyticsItems) > 0 {
		return &dtos.GetUsageAnalyticsResponse{
			GetUsageAnalyticsResponse: &types.GetUsageAnalyticsResponse{
				Items: f.analyticsItems,
			},
		}, nil
	}
	return &dtos.GetUsageAnalyticsResponse{}, nil
}
func (f *fakeEvents) ListRaw(_ context.Context, req types.GetEventsRequest) (*dtos.ListRawEventsResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.listRawErr != nil {
		return nil, f.listRawErr
	}
	if f.listRawItems != nil {
		return &dtos.ListRawEventsResponse{
			GetEventsResponse: &types.GetEventsResponse{Events: f.listRawItems},
		}, nil
	}
	// Default: echo back ingested events matching the property filters.
	var matched []types.Event
	for _, in := range f.ingested {
		if req.ExternalCustomerID != nil && in.ExternalCustomerID != *req.ExternalCustomerID {
			continue
		}
		if req.EventName != nil && in.EventName != *req.EventName {
			continue
		}
		ok := true
		for k, vs := range req.PropertyFilters {
			pv, found := in.Properties[k]
			if !found {
				ok = false
				break
			}
			matchAny := false
			for _, want := range vs {
				if pv == want {
					matchAny = true
					break
				}
			}
			if !matchAny {
				ok = false
				break
			}
		}
		if !ok {
			continue
		}
		ev := types.Event{EventName: strPtr(in.EventName)}
		if in.EventID != nil {
			ev.ID = in.EventID
		}
		matched = append(matched, ev)
	}
	return &dtos.ListRawEventsResponse{
		GetEventsResponse: &types.GetEventsResponse{Events: matched},
	}, nil
}

// --- Invoices ---

type fakeInvoices struct {
	mu         sync.Mutex
	queries    int
	queryErr   error
	invoices   []types.InvoiceResponse
	lastFilter types.InvoiceFilter
}

func (f *fakeInvoices) Query(_ context.Context, filter types.InvoiceFilter) (*dtos.QueryInvoiceResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.queries++
	f.lastFilter = filter
	if f.queryErr != nil {
		return nil, f.queryErr
	}
	if len(f.invoices) == 0 {
		return &dtos.QueryInvoiceResponse{}, nil
	}
	return &dtos.QueryInvoiceResponse{
		ListInvoicesResponse: &types.ListInvoicesResponse{Items: f.invoices},
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
