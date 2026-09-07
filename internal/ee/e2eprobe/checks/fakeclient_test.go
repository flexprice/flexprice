package checks

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/flexprice/flexprice/internal/ee/e2eprobe"
	flexprice "github.com/flexprice/go-sdk/v2"
	"github.com/flexprice/go-sdk/v2/models/dtos"
	sdkerrors "github.com/flexprice/go-sdk/v2/models/errors"
	"github.com/flexprice/go-sdk/v2/models/types"
)

type fakeClient struct {
	customers          fakeCustomers
	plans              fakePlans
	prices             fakePrices
	features           fakeFeatures
	subs               fakeSubscriptions
	wallets            fakeWallets
	events             fakeEvents
	invoices           fakeInvoices
	entitlements       fakeEntitlements
	coupons            fakeCoupons
	couponAssociations fakeCouponAssociations
	taxRates           fakeTaxRates
	taxAssociations    fakeTaxAssociations
	async              *fakeAsyncEvents
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
func (c *fakeClient) Entitlements() e2eprobe.EntitlementOps             { return &c.entitlements }
func (c *fakeClient) Coupons() e2eprobe.CouponOps                       { return &c.coupons }
func (c *fakeClient) CouponAssociations() e2eprobe.CouponAssociationOps { return &c.couponAssociations }
func (c *fakeClient) TaxRates() e2eprobe.TaxRateOps                     { return &c.taxRates }
func (c *fakeClient) TaxAssociations() e2eprobe.TaxAssociationOps       { return &c.taxAssociations }

// --- Customers ---

type fakeCustomers struct {
	mu           sync.Mutex
	created      []types.CreateCustomerRequest
	byExt        map[string]string
	getErr       error
	deleted      []string // internal customer IDs passed to Delete
	queryResult  []types.CustomerResponse
	usageSummary *dtos.GetCustomerUsageSummaryResponse
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
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.usageSummary != nil {
		return f.usageSummary, nil
	}
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
	mu            sync.Mutex
	created       []types.CreatePlanRequest
	plans         []types.PlanResponse
	syncedPlanIDs []string
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

func (f *fakePlans) SyncPrices(_ context.Context, planID string) (*dtos.SyncPlanPricesResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.syncedPlanIDs = append(f.syncedPlanIDs, planID)
	return &dtos.SyncPlanPricesResponse{}, nil
}

// --- Prices ---

type fakePrices struct {
	mu      sync.Mutex
	created []types.CreatePriceRequest
	// ids parallels created — one fabricated fake id per create call, so
	// Query can return items with realistic IDs. Callers looking up
	// created[i] can pair it with ids[i].
	ids []string
	// bucketSizes records the price-level bucket passed alongside each created
	// price, keyed by lookup key. Empty string means the price is unbucketed.
	bucketSizes map[string]string
}

func (f *fakePrices) nextID() string {
	// Callers already hold f.mu.
	return fmt.Sprintf("price_fake_%d", len(f.created)+1)
}

func (f *fakePrices) Create(_ context.Context, req types.CreatePriceRequest) (*dtos.CreatePriceResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	id := f.nextID()
	f.created = append(f.created, req)
	f.ids = append(f.ids, id)
	return &dtos.CreatePriceResponse{}, nil
}
func (f *fakePrices) CreateBucketed(_ context.Context, req types.CreatePriceRequest, bucketSize string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	id := f.nextID()
	f.created = append(f.created, req)
	f.ids = append(f.ids, id)
	if f.bucketSizes == nil {
		f.bucketSizes = map[string]string{}
	}
	if req.LookupKey != nil {
		f.bucketSizes[*req.LookupKey] = bucketSize
	}
	return id, nil
}
func (f *fakePrices) Query(_ context.Context, filter types.PriceFilter) (*dtos.QueryPriceResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	// Filter by PlanIds when provided (the seed's multi-cadence lookup uses
	// this). Callers that pass no filter get every created price back.
	var items []types.PriceResponse
	for i, req := range f.created {
		if len(filter.PlanIds) > 0 {
			if req.EntityID == "" {
				continue
			}
			matched := false
			for _, pid := range filter.PlanIds {
				if req.EntityID == pid {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
		}
		id := f.ids[i]
		items = append(items, types.PriceResponse{ID: &id})
	}
	return &dtos.QueryPriceResponse{
		ListPricesResponse: &types.ListPricesResponse{Items: items},
	}, nil
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
	mu                  sync.Mutex
	created             []types.CreateSubscriptionRequest
	cancelled           []string
	gets                int
	nextID              int
	subs                map[string]types.SubscriptionResponse
	subErr              error
	cancelErr           error
	getEntitlementsResp *dtos.GetSubscriptionEntitlementsResponse
	getEntitlementsErr  error
	// queryReturnsAll makes Query ignore the filter and return every stored
	// sub, for tests that look subs up by external customer id (which this
	// fake does not index).
	queryReturnsAll bool
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
		if !f.queryReturnsAll && (filter.ExternalCustomerID != nil || filter.PlanID != nil) {
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
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.getEntitlementsErr != nil {
		return nil, f.getEntitlementsErr
	}
	if f.getEntitlementsResp != nil {
		return f.getEntitlementsResp, nil
	}
	return &dtos.GetSubscriptionEntitlementsResponse{}, nil
}
func (f *fakeSubscriptions) GetUsage(_ context.Context, _ types.GetUsageBySubscriptionRequest) (*dtos.GetSubscriptionUsageResponse, error) {
	// Return a large enough Amount that probe usage-threshold gates always
	// pass in unit tests — the polling loop is exercised in staging.
	amt := 1e12
	return &dtos.GetSubscriptionUsageResponse{
		GetUsageBySubscriptionResponse: &types.GetUsageBySubscriptionResponse{Amount: &amt},
	}, nil
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
	mu        sync.Mutex
	ingested  []types.IngestEventRequest
	analytics int
	anaErr    error
	// analyticsItems, when set, is returned in GetUsageAnalytics responses.
	analyticsItems []types.UsageAnalyticItem

	// analyticsEcho makes GetUsageAnalytics append an item whose points carry
	// the timestamps of the events ingested so far — the shape a real bucketed
	// meter read has. Any analyticsItems are returned ahead of it, so a test can
	// stand in a second subscription's item.
	analyticsEcho bool
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
	items := f.analyticsItems
	if f.analyticsEcho {
		points := make([]types.UsageAnalyticPoint, 0, len(f.ingested))
		for _, ev := range f.ingested {
			if ev.Timestamp == nil {
				continue
			}
			ts := *ev.Timestamp
			points = append(points, types.UsageAnalyticPoint{Timestamp: &ts})
		}
		items = append(append([]types.UsageAnalyticItem{}, items...),
			types.UsageAnalyticItem{Points: points})
	}
	if len(items) > 0 {
		return &dtos.GetUsageAnalyticsResponse{
			GetUsageAnalyticsResponse: &types.GetUsageAnalyticsResponse{
				Items: items,
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
	// Preview support
	previewResp   *dtos.GetInvoicePreviewResponse // default response
	previewErr    error
	previewForSub map[string]*dtos.GetInvoicePreviewResponse // per-sub override, keyed by SubscriptionID
	previewCalls  []types.GetPreviewInvoiceRequest
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
func (f *fakeInvoices) GetPreview(_ context.Context, req types.GetPreviewInvoiceRequest) (*dtos.GetInvoicePreviewResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.previewCalls = append(f.previewCalls, req)
	if f.previewErr != nil {
		return nil, f.previewErr
	}
	if resp, ok := f.previewForSub[req.SubscriptionID]; ok {
		return resp, nil
	}
	if f.previewResp != nil {
		return f.previewResp, nil
	}
	return &dtos.GetInvoicePreviewResponse{}, nil
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

// --- Entitlements ---

type fakeEntitlements struct {
	mu        sync.Mutex
	created   []types.CreateEntitlementRequest
	createErr error
	queryResp *dtos.QueryEntitlementResponse
	queryErr  error
	deleted   []string

	// Grant-related fields (Task 3 of entitlement-grants plan)
	createdWithGrant   []e2eprobe.GrantEntitlementInput
	createWithGrantErr error
	createWithGrantID  string // returned by CreateWithGrant when non-empty; else auto-generated
	getRawResp         *e2eprobe.GrantEntitlementResponse
	getRawRespByID     map[string]*e2eprobe.GrantEntitlementResponse // per-id lookup takes priority over getRawResp
	getRawErr          error
	getRawCalls        []string
}

func (f *fakeEntitlements) Create(_ context.Context, req types.CreateEntitlementRequest) (*dtos.CreateEntitlementResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.createErr != nil {
		return nil, f.createErr
	}
	f.created = append(f.created, req)
	id := fmt.Sprintf("ent_%d", len(f.created))
	return &dtos.CreateEntitlementResponse{
		EntitlementResponse: &types.EntitlementResponse{ID: &id, FeatureID: &req.FeatureID},
	}, nil
}
func (f *fakeEntitlements) Query(_ context.Context, _ types.EntitlementFilter) (*dtos.QueryEntitlementResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.queryErr != nil {
		return nil, f.queryErr
	}
	if f.queryResp != nil {
		return f.queryResp, nil
	}
	return &dtos.QueryEntitlementResponse{}, nil
}
func (f *fakeEntitlements) Delete(_ context.Context, id string) (*dtos.DeleteEntitlementResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleted = append(f.deleted, id)
	return &dtos.DeleteEntitlementResponse{}, nil
}

func (f *fakeEntitlements) CreateWithGrant(_ context.Context, req e2eprobe.GrantEntitlementInput) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.createWithGrantErr != nil {
		return "", f.createWithGrantErr
	}
	f.createdWithGrant = append(f.createdWithGrant, req)
	if f.createWithGrantID != "" {
		return f.createWithGrantID, nil
	}
	return fmt.Sprintf("ent_grant_%d", len(f.createdWithGrant)), nil
}

func (f *fakeEntitlements) GetRaw(_ context.Context, id string) (*e2eprobe.GrantEntitlementResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.getRawCalls = append(f.getRawCalls, id)
	if f.getRawErr != nil {
		return nil, f.getRawErr
	}
	if resp, ok := f.getRawRespByID[id]; ok {
		return resp, nil
	}
	if f.getRawResp != nil {
		return f.getRawResp, nil
	}
	// Default: well-formed response matching the seed's config-echo assertion,
	// so happy-path tests don't need to inject.
	dv := 1
	return &e2eprobe.GrantEntitlementResponse{
		ID:                 id,
		GrantMeasure:       "quantity",
		GrantQuota:         "1000",
		GrantDurationValue: &dv,
		GrantDurationUnit:  "hour",
		AggregationMode:    "additive",
		IsEnabled:          true,
	}, nil
}

// --- Coupons ---

type fakeCoupons struct {
	mu        sync.Mutex
	created   []types.CreateCouponRequest
	createErr error
	byCode    map[string]string // code -> id
}

// normalizeCouponCode mirrors internal/repository/ent/coupon.go: the real
// repo lowercases/trims coupon codes on INSERT and stores the normalized
// form, so the fake must do the same for the CouponCodes filter to hit.
func normalizeCouponCode(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

func (f *fakeCoupons) Create(_ context.Context, req types.CreateCouponRequest) (*dtos.CreateCouponResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.createErr != nil {
		return nil, f.createErr
	}
	f.created = append(f.created, req)
	id := fmt.Sprintf("coupon_%d", len(f.created))
	if req.CouponCode != nil {
		if f.byCode == nil {
			f.byCode = map[string]string{}
		}
		f.byCode[normalizeCouponCode(*req.CouponCode)] = id
	}
	return &dtos.CreateCouponResponse{
		CouponResponse: &types.CouponResponse{ID: &id, CouponCode: req.CouponCode, Name: &req.Name},
	}, nil
}
func (f *fakeCoupons) Query(_ context.Context, filter types.CouponFilter) (*dtos.QueryCouponResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var items []types.CouponResponse
	for _, code := range filter.CouponCodes {
		norm := normalizeCouponCode(code)
		if id, ok := f.byCode[norm]; ok {
			c := norm
			i := id
			items = append(items, types.CouponResponse{ID: &i, CouponCode: &c})
		}
	}
	if len(items) == 0 {
		return &dtos.QueryCouponResponse{}, nil
	}
	return &dtos.QueryCouponResponse{
		ListCouponsResponse: &types.ListCouponsResponse{Items: items},
	}, nil
}
func (f *fakeCoupons) GetByCode(_ context.Context, code string) (*dtos.GetCouponByCodeResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	norm := normalizeCouponCode(code)
	id, ok := f.byCode[norm]
	if !ok {
		return nil, &sdkerrors.ErrorResponse{
			HTTPStatusCode: int64Ptr(http.StatusNotFound),
		}
	}
	c := norm
	return &dtos.GetCouponByCodeResponse{
		CouponResponse: &types.CouponResponse{ID: &id, CouponCode: &c},
	}, nil
}
func (f *fakeCoupons) Delete(_ context.Context, _ string) (*dtos.DeleteCouponResponse, error) {
	return &dtos.DeleteCouponResponse{}, nil
}

// --- Coupon associations ---

type fakeCouponAssociations struct {
	mu   sync.Mutex
	resp *dtos.ListCouponAssociationsResponse
}

func (f *fakeCouponAssociations) List(_ context.Context, _ dtos.ListCouponAssociationsRequest) (*dtos.ListCouponAssociationsResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.resp != nil {
		return f.resp, nil
	}
	return &dtos.ListCouponAssociationsResponse{}, nil
}

// --- Tax rates ---

type fakeTaxRates struct {
	mu        sync.Mutex
	created   []types.CreateTaxRateRequest
	createErr error
	byCode    map[string]string // code -> id
	listResp  *dtos.GetTaxRatesResponse
	listErr   error
}

func (f *fakeTaxRates) Create(_ context.Context, req types.CreateTaxRateRequest) (*dtos.CreateTaxRateResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.createErr != nil {
		return nil, f.createErr
	}
	f.created = append(f.created, req)
	id := fmt.Sprintf("taxrate_%d", len(f.created))
	if f.byCode == nil {
		f.byCode = map[string]string{}
	}
	f.byCode[req.Code] = id
	c := req.Code
	return &dtos.CreateTaxRateResponse{
		TaxRateResponse: &types.TaxRateResponse{ID: &id, Code: &c},
	}, nil
}
func (f *fakeTaxRates) Get(_ context.Context, id string) (*dtos.GetTaxRateResponse, error) {
	return &dtos.GetTaxRateResponse{
		TaxRateResponse: &types.TaxRateResponse{ID: &id},
	}, nil
}
func (f *fakeTaxRates) List(_ context.Context, _ dtos.GetTaxRatesRequest) (*dtos.GetTaxRatesResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.listErr != nil {
		return nil, f.listErr
	}
	if f.listResp != nil {
		return f.listResp, nil
	}
	return &dtos.GetTaxRatesResponse{}, nil
}
func (f *fakeTaxRates) Delete(_ context.Context, _ string) (*dtos.DeleteTaxRateResponse, error) {
	return &dtos.DeleteTaxRateResponse{}, nil
}

// --- Tax associations ---

type fakeTaxAssociations struct {
	mu        sync.Mutex
	created   []types.CreateTaxAssociationRequest
	createErr error
	listResp  *dtos.ListTaxAssociationsResponse
	listErr   error
	deleted   []string
	deleteErr error
}

func (f *fakeTaxAssociations) Create(_ context.Context, req types.CreateTaxAssociationRequest) (*dtos.CreateTaxAssociationResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.createErr != nil {
		return nil, f.createErr
	}
	f.created = append(f.created, req)
	id := fmt.Sprintf("taxassoc_%d", len(f.created))
	return &dtos.CreateTaxAssociationResponse{
		TaxAssociationResponse: &types.TaxAssociationResponse{ID: &id},
	}, nil
}
func (f *fakeTaxAssociations) List(_ context.Context, _, _, _, _ *string) (*dtos.ListTaxAssociationsResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.listErr != nil {
		return nil, f.listErr
	}
	if f.listResp != nil {
		return f.listResp, nil
	}
	return &dtos.ListTaxAssociationsResponse{}, nil
}
func (f *fakeTaxAssociations) Delete(_ context.Context, id string) (*dtos.DeleteTaxAssociationResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.deleteErr != nil {
		return nil, f.deleteErr
	}
	f.deleted = append(f.deleted, id)
	return &dtos.DeleteTaxAssociationResponse{}, nil
}

// --- helpers (strPtr is defined in seed_ensure.go) ---

// dateP returns a pointer to a time.Time value constructed from year, month, day (UTC).
// Used by multi_cadence_invoice_probe_test.go and any future test that needs a *time.Time.
func dateP(y int, m time.Month, d int) *time.Time {
	t := time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	return &t
}
