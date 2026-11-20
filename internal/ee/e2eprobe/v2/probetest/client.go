// Package probetest is a stateful in-memory stand-in for the probe's Client,
// for unit tests of provisioning and scenarios. Exported fields steer and
// inspect it the way httptest exposes a recorder.
package probetest

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/flexprice/flexprice/internal/ee/e2eprobe/v2"
	"github.com/flexprice/go-sdk/v2/models/dtos"
	sdkerrors "github.com/flexprice/go-sdk/v2/models/errors"
	"github.com/flexprice/go-sdk/v2/models/types"
	"github.com/shopspring/decimal"
)

// Client implements e2eprobe.Client over in-memory stores.
type Client struct {
	Customer     *Customers
	Plan         *Plans
	Price        *Prices
	Feature      *Features
	Subscription *Subscriptions
	Wallet       *Wallets
	Event        *Events
	Invoice      *Invoices
	Entitlement  *Entitlements
	Coupon       *Coupons
	CouponAssoc  *CouponAssociations
	TaxRate      *TaxRates
	TaxAssoc     *TaxAssociations
	Payment      *Payments
	Alert        *Alerts
	TenantOps    *Tenant
	RawOps       *Raw

	// Clock stamps created_at fields; tests replace it for deterministic ages.
	Clock func() time.Time
}

func New() *Client {
	c := &Client{Clock: time.Now}
	c.Customer = &Customers{client: c, Items: map[string]types.CustomerResponse{}}
	c.Plan = &Plans{client: c}
	c.Price = &Prices{client: c, Buckets: map[string]string{}}
	c.Feature = &Features{client: c}
	c.Coupon = &Coupons{client: c, ByCode: map[string]string{}}
	c.CouponAssoc = &CouponAssociations{client: c}
	c.Subscription = &Subscriptions{client: c, Items: map[string]types.SubscriptionResponse{}, extByID: map[string]string{}}
	c.Wallet = &Wallets{client: c, Items: map[string]types.WalletResponse{}, AlertStates: map[string]types.AlertState{}}
	c.Event = &Events{}
	c.Invoice = &Invoices{ByID: map[string]types.InvoiceResponse{}}
	c.Entitlement = &Entitlements{Grants: map[string]e2eprobe.GrantEntitlementResponse{}}
	c.TaxRate = &TaxRates{client: c, ByCode: map[string]string{}}
	c.TaxAssoc = &TaxAssociations{client: c}
	c.Payment = &Payments{}
	c.Alert = &Alerts{}
	c.TenantOps = &Tenant{Settings: map[string]map[string]any{}}
	c.RawOps = &Raw{}
	return c
}

func (c *Client) Customers() e2eprobe.CustomerOps                   { return c.Customer }
func (c *Client) Plans() e2eprobe.PlanOps                           { return c.Plan }
func (c *Client) Prices() e2eprobe.PriceOps                         { return c.Price }
func (c *Client) Features() e2eprobe.FeatureOps                     { return c.Feature }
func (c *Client) Subscriptions() e2eprobe.SubscriptionOps           { return c.Subscription }
func (c *Client) Wallets() e2eprobe.WalletOps                       { return c.Wallet }
func (c *Client) Events() e2eprobe.EventOps                         { return c.Event }
func (c *Client) Invoices() e2eprobe.InvoiceOps                     { return c.Invoice }
func (c *Client) Entitlements() e2eprobe.EntitlementOps             { return c.Entitlement }
func (c *Client) Coupons() e2eprobe.CouponOps                       { return c.Coupon }
func (c *Client) CouponAssociations() e2eprobe.CouponAssociationOps { return c.CouponAssoc }
func (c *Client) TaxRates() e2eprobe.TaxRateOps                     { return c.TaxRate }
func (c *Client) TaxAssociations() e2eprobe.TaxAssociationOps       { return c.TaxAssoc }
func (c *Client) Payments() e2eprobe.PaymentOps                     { return c.Payment }
func (c *Client) Alerts() e2eprobe.AlertOps                         { return c.Alert }
func (c *Client) Tenant() e2eprobe.TenantOps                        { return c.TenantOps }
func (c *Client) Raw() e2eprobe.RawOps                              { return c.RawOps }

var _ e2eprobe.Client = (*Client)(nil)

func (c *Client) now() time.Time { return c.Clock().UTC() }

// NotFound is the 404 the SDK surfaces for explicit not-found branches.
func NotFound() error {
	status := int64(http.StatusNotFound)
	code := types.ErrorCodeNotFound
	return &sdkerrors.ErrorResponse{HTTPStatusCode: &status, Code: &code}
}

// Conflict is the 409 already_exists the SDK surfaces on duplicate creates.
func Conflict() error {
	status := int64(http.StatusConflict)
	code := types.ErrorCodeAlreadyExists
	return &sdkerrors.ErrorResponse{HTTPStatusCode: &status, Code: &code}
}

// ServerError is a 5xx APIError, the shape infrastructure failures take.
func ServerError(status int) error {
	return sdkerrors.NewAPIError("server error", status, "", nil)
}

func Ptr[T any](v T) *T { return &v }

func ptrTime(t time.Time) *time.Time { return &t }

func addDecimal(a, b string) string {
	x, _ := decimal.NewFromString(a)
	y, _ := decimal.NewFromString(b)
	return x.Add(y).StringFixed(2)
}

// ── Customers ─────────────────────────────────────────────────────────

type Customers struct {
	client *Client
	mu     sync.Mutex
	nextID int

	Items   map[string]types.CustomerResponse // by external id
	Created []types.CreateCustomerRequest
	Deleted []string // internal ids

	CreateErr        error
	GetErr           error
	QueryErr         error
	DeleteErr        error
	EntitlementsResp *dtos.GetCustomerEntitlementsResponse
	UsageSummary     func(req dtos.GetCustomerUsageSummaryRequest) (*dtos.GetCustomerUsageSummaryResponse, error)
}

// Add seeds a customer as if it existed before the probe ran.
func (f *Customers) Add(ext, id string, metadata map[string]string, createdAt time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Items[ext] = types.CustomerResponse{ID: Ptr(id), ExternalID: Ptr(ext), Name: Ptr(ext), Metadata: metadata, CreatedAt: ptrTime(createdAt)}
}

// Internal returns the internal id of a stored customer, or "".
func (f *Customers) Internal(ext string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if c, ok := f.Items[ext]; ok && c.ID != nil {
		return *c.ID
	}
	return ""
}

func (f *Customers) Create(_ context.Context, req types.CreateCustomerRequest) (*dtos.CreateCustomerResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.CreateErr != nil {
		return nil, f.CreateErr
	}
	if _, exists := f.Items[req.ExternalID]; exists {
		return nil, Conflict()
	}
	f.nextID++
	id := fmt.Sprintf("cust_%d", f.nextID)
	cust := types.CustomerResponse{ID: Ptr(id), ExternalID: Ptr(req.ExternalID), Name: Ptr(req.Name), Metadata: req.Metadata, CreatedAt: ptrTime(f.client.now())}
	f.Items[req.ExternalID] = cust
	f.Created = append(f.Created, req)
	return &dtos.CreateCustomerResponse{CustomerResponse: &cust}, nil
}

func (f *Customers) GetByExternalID(_ context.Context, ext string) (*dtos.GetCustomerByExternalIDResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.GetErr != nil {
		return nil, f.GetErr
	}
	cust, ok := f.Items[ext]
	if !ok {
		return nil, NotFound()
	}
	return &dtos.GetCustomerByExternalIDResponse{CustomerResponse: &cust}, nil
}

func (f *Customers) Get(_ context.Context, id string) (*dtos.GetCustomerResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, cust := range f.Items {
		if cust.ID != nil && *cust.ID == id {
			return &dtos.GetCustomerResponse{CustomerResponse: &cust}, nil
		}
	}
	return nil, NotFound()
}

func (f *Customers) GetEntitlements(_ context.Context, _ string) (*dtos.GetCustomerEntitlementsResponse, error) {
	if f.EntitlementsResp != nil {
		return f.EntitlementsResp, nil
	}
	return &dtos.GetCustomerEntitlementsResponse{}, nil
}

func (f *Customers) GetUsageSummary(_ context.Context, req dtos.GetCustomerUsageSummaryRequest) (*dtos.GetCustomerUsageSummaryResponse, error) {
	if f.UsageSummary != nil {
		return f.UsageSummary(req)
	}
	return &dtos.GetCustomerUsageSummaryResponse{CustomerUsageSummaryResponse: &types.CustomerUsageSummaryResponse{}}, nil
}

func (f *Customers) Update(_ context.Context, _ types.UpdateCustomerRequest, _, _ *string) (*dtos.UpdateCustomerResponse, error) {
	return &dtos.UpdateCustomerResponse{}, nil
}

func (f *Customers) Delete(_ context.Context, id string) (*dtos.DeleteCustomerResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.DeleteErr != nil {
		return nil, f.DeleteErr
	}
	for ext, cust := range f.Items {
		if cust.ID != nil && *cust.ID == id {
			delete(f.Items, ext)
			f.Deleted = append(f.Deleted, id)
			return &dtos.DeleteCustomerResponse{}, nil
		}
	}
	return nil, NotFound()
}

func (f *Customers) Query(_ context.Context, filter types.CustomerFilter) (*dtos.QueryCustomerResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.QueryErr != nil {
		return nil, f.QueryErr
	}
	want := map[string]bool{}
	for _, ext := range filter.ExternalIds {
		want[ext] = true
	}
	var items []types.CustomerResponse
	for ext, cust := range f.Items {
		if len(want) > 0 && !want[ext] {
			continue
		}
		items = append(items, cust)
	}
	return &dtos.QueryCustomerResponse{ListCustomersResponse: &types.ListCustomersResponse{Items: items}}, nil
}

// ── Plans ─────────────────────────────────────────────────────────────

type Plans struct {
	client *Client
	mu     sync.Mutex

	Items   []types.PlanResponse
	Created []types.CreatePlanRequest
	Synced  []string
	SyncErr error
}

func (f *Plans) Create(_ context.Context, req types.CreatePlanRequest) (*dtos.CreatePlanResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	id := fmt.Sprintf("plan_%d", len(f.Items)+1)
	plan := types.PlanResponse{ID: Ptr(id), LookupKey: req.LookupKey, Name: Ptr(req.Name), Metadata: req.Metadata}
	f.Items = append(f.Items, plan)
	f.Created = append(f.Created, req)
	return &dtos.CreatePlanResponse{PlanResponse: &plan}, nil
}

func (f *Plans) Query(_ context.Context, filter types.PlanFilter) (*dtos.QueryPlanResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var matched []types.PlanResponse
	for _, p := range f.Items {
		if filter.LookupKey != nil && (p.LookupKey == nil || *p.LookupKey != *filter.LookupKey) {
			continue
		}
		matched = append(matched, p)
	}
	return &dtos.QueryPlanResponse{ListPlansResponse: &types.ListPlansResponse{Items: matched}}, nil
}

func (f *Plans) Get(_ context.Context, id string) (*dtos.GetPlanResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, p := range f.Items {
		if p.ID != nil && *p.ID == id {
			return &dtos.GetPlanResponse{PlanResponse: &p}, nil
		}
	}
	return nil, NotFound()
}

func (f *Plans) SyncPrices(_ context.Context, planID string) (*dtos.SyncPlanPricesResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.SyncErr != nil {
		return nil, f.SyncErr
	}
	f.Synced = append(f.Synced, planID)
	return &dtos.SyncPlanPricesResponse{}, nil
}

// ── Prices ────────────────────────────────────────────────────────────

type Prices struct {
	client *Client
	mu     sync.Mutex

	Items     []types.PriceResponse
	Created   []types.CreatePriceRequest
	Buckets   map[string]string // lookup key → price-level bucket
	CreateErr error
}

func (f *Prices) add(req types.CreatePriceRequest) string {
	id := fmt.Sprintf("price_%d", len(f.Items)+1)
	entityType := req.EntityType
	f.Items = append(f.Items, types.PriceResponse{ID: Ptr(id), EntityID: Ptr(req.EntityID), EntityType: &entityType, MeterID: req.MeterID, LookupKey: req.LookupKey, Currency: Ptr(req.Currency)})
	f.Created = append(f.Created, req)
	return id
}

func (f *Prices) Create(_ context.Context, req types.CreatePriceRequest) (*dtos.CreatePriceResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.CreateErr != nil {
		return nil, f.CreateErr
	}
	id := f.add(req)
	return &dtos.CreatePriceResponse{PriceResponse: &types.PriceResponse{ID: Ptr(id)}}, nil
}

func (f *Prices) CreateBucketed(_ context.Context, req types.CreatePriceRequest, bucketSize string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.CreateErr != nil {
		return "", f.CreateErr
	}
	id := f.add(req)
	if req.LookupKey != nil {
		f.Buckets[*req.LookupKey] = bucketSize
	}
	return id, nil
}

func (f *Prices) Query(_ context.Context, filter types.PriceFilter) (*dtos.QueryPriceResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	want := map[string]bool{}
	for _, id := range append(append([]string{}, filter.EntityIds...), filter.PlanIds...) {
		want[id] = true
	}
	var items []types.PriceResponse
	for _, p := range f.Items {
		if len(want) > 0 && (p.EntityID == nil || !want[*p.EntityID]) {
			continue
		}
		items = append(items, p)
	}
	return &dtos.QueryPriceResponse{ListPricesResponse: &types.ListPricesResponse{Items: items}}, nil
}

// ── Features ──────────────────────────────────────────────────────────

type Features struct {
	client *Client
	mu     sync.Mutex

	Items     []types.FeatureResponse
	Created   []types.CreateFeatureRequest
	CreateErr error
}

func (f *Features) Create(_ context.Context, req types.CreateFeatureRequest) (*dtos.CreateFeatureResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.CreateErr != nil {
		return nil, f.CreateErr
	}
	n := len(f.Items) + 1
	feat := types.FeatureResponse{ID: Ptr(fmt.Sprintf("feat_%d", n)), LookupKey: req.LookupKey, MeterID: Ptr(fmt.Sprintf("meter_%d", n)), Name: Ptr(req.Name)}
	f.Items = append(f.Items, feat)
	f.Created = append(f.Created, req)
	return &dtos.CreateFeatureResponse{FeatureResponse: &feat}, nil
}

func (f *Features) Query(_ context.Context, filter types.FeatureFilter) (*dtos.QueryFeatureResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	want := map[string]bool{}
	for _, k := range filter.LookupKeys {
		want[k] = true
	}
	if filter.LookupKey != nil {
		want[*filter.LookupKey] = true
	}
	var matched []types.FeatureResponse
	for _, feat := range f.Items {
		if len(want) == 0 || (feat.LookupKey != nil && want[*feat.LookupKey]) {
			matched = append(matched, feat)
		}
	}
	return &dtos.QueryFeatureResponse{ListFeaturesResponse: &types.ListFeaturesResponse{Items: matched}}, nil
}

// ── Subscriptions ─────────────────────────────────────────────────────

type Subscriptions struct {
	client  *Client
	mu      sync.Mutex
	nextID  int
	extByID map[string]string

	Items     map[string]types.SubscriptionResponse
	Created   []types.CreateSubscriptionRequest
	Cancelled []string
	Activated []string
	LineItems []types.CreateSubscriptionLineItemRequest

	// CreateStatus is the status a new subscription starts in; Draft forces an activation.
	CreateStatus types.SubscriptionStatus
	CreateErr    error
	ActivateErr  error
	GetErr       error
	CancelErr    error
	Entitlements func(subID string) (*dtos.GetSubscriptionEntitlementsResponse, error)
	Usage        func(req types.GetUsageBySubscriptionRequest) (*dtos.GetSubscriptionUsageResponse, error)

	// CancelStatus is the status Cancel leaves a subscription in; empty means cancelled.
	CancelStatus types.SubscriptionStatus
	// CreateLineItemFn replaces CreateLineItem when set; the stored subscription is left untouched.
	CreateLineItemFn func(id string, req types.CreateSubscriptionLineItemRequest) error
}

// Add seeds a subscription for ext as if it existed before the probe ran.
func (f *Subscriptions) Add(ext string, sub types.SubscriptionResponse) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if sub.ID == nil {
		f.nextID++
		sub.ID = Ptr(fmt.Sprintf("sub_%d", f.nextID))
	}
	if sub.CustomerID == nil {
		if id := f.client.Customer.Internal(ext); id != "" {
			sub.CustomerID = Ptr(id)
		}
	}
	f.Items[*sub.ID] = sub
	f.extByID[*sub.ID] = ext
}

func (f *Subscriptions) Create(_ context.Context, req types.CreateSubscriptionRequest) (*dtos.CreateSubscriptionResponse, error) {
	f.mu.Lock()
	if f.CreateErr != nil {
		f.mu.Unlock()
		return nil, f.CreateErr
	}
	f.nextID++
	id := fmt.Sprintf("sub_%d", f.nextID)
	status := f.CreateStatus
	if status == "" {
		status = types.SubscriptionStatusActive
	}
	ext := ""
	if req.ExternalCustomerID != nil {
		ext = *req.ExternalCustomerID
	}
	period := req.BillingPeriod
	now := f.client.now()
	sub := types.SubscriptionResponse{
		ID:                 Ptr(id),
		PlanID:             Ptr(req.PlanID),
		BillingPeriod:      &period,
		BillingPeriodCount: req.BillingPeriodCount,
		SubscriptionStatus: &status,
		Metadata:           req.Metadata,
		CommitmentAmount:   req.CommitmentAmount,
		OverageFactor:      req.OverageFactor,
		EnableTrueUp:       req.EnableTrueUp,
		StartDate:          ptrTime(now),
		CreatedAt:          ptrTime(now),
		Currency:           Ptr(req.Currency),
	}
	if req.CustomerID != nil {
		sub.CustomerID = req.CustomerID
	} else if custID := f.client.Customer.Internal(ext); custID != "" {
		sub.CustomerID = Ptr(custID)
	}
	f.Items[id] = sub
	f.extByID[id] = ext
	f.Created = append(f.Created, req)
	f.mu.Unlock()

	// Line items snapshot the plan's prices, as the real API does.
	prices, _ := f.client.Price.Query(context.Background(), types.PriceFilter{EntityIds: []string{req.PlanID}})
	f.mu.Lock()
	for _, p := range prices.ListPricesResponse.Items {
		sub.LineItems = append(sub.LineItems, types.SubscriptionSubscriptionLineItem{ID: Ptr("li_" + *p.ID), PriceID: p.ID, MeterID: p.MeterID})
	}
	f.Items[id] = sub
	f.mu.Unlock()
	for _, c := range req.SubscriptionCoupons {
		f.client.CouponAssoc.attach(id, f.client.Coupon.idFor(c.CouponCode))
	}
	return &dtos.CreateSubscriptionResponse{SubscriptionResponse: &sub}, nil
}

func (f *Subscriptions) Get(_ context.Context, id string) (*dtos.GetSubscriptionResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.GetErr != nil {
		return nil, f.GetErr
	}
	sub, ok := f.Items[id]
	if !ok {
		return nil, NotFound()
	}
	return &dtos.GetSubscriptionResponse{SubscriptionResponse: &sub}, nil
}

func (f *Subscriptions) Cancel(_ context.Context, id string, _ types.CancelSubscriptionRequest) (*dtos.CancelSubscriptionResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.CancelErr != nil {
		return nil, f.CancelErr
	}
	sub, ok := f.Items[id]
	if !ok {
		return nil, NotFound()
	}
	status := f.CancelStatus
	if status == "" {
		status = types.SubscriptionStatusCancelled
	}
	sub.SubscriptionStatus = &status
	f.Items[id] = sub
	f.Cancelled = append(f.Cancelled, id)
	return &dtos.CancelSubscriptionResponse{}, nil
}

func (f *Subscriptions) Query(_ context.Context, filter types.SubscriptionFilter) (*dtos.QuerySubscriptionResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	wantIDs := map[string]bool{}
	for _, id := range filter.SubscriptionIds {
		wantIDs[id] = true
	}
	var matched []types.SubscriptionResponse
	for id, sub := range f.Items {
		if filter.ExternalCustomerID != nil && f.extByID[id] != *filter.ExternalCustomerID {
			continue
		}
		if filter.PlanID != nil && (sub.PlanID == nil || *sub.PlanID != *filter.PlanID) {
			continue
		}
		if filter.CustomerID != nil && (sub.CustomerID == nil || *sub.CustomerID != *filter.CustomerID) {
			continue
		}
		if len(wantIDs) > 0 && !wantIDs[id] {
			continue
		}
		matched = append(matched, sub)
	}
	return &dtos.QuerySubscriptionResponse{ListSubscriptionsResponse: &types.ListSubscriptionsResponse{Items: matched}}, nil
}

func (f *Subscriptions) ActivateSubscription(_ context.Context, id string, _ types.ActivateDraftSubscriptionRequest) (*dtos.ActivateSubscriptionResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.ActivateErr != nil {
		return nil, f.ActivateErr
	}
	sub, ok := f.Items[id]
	if !ok {
		return nil, NotFound()
	}
	status := types.SubscriptionStatusActive
	sub.SubscriptionStatus = &status
	f.Items[id] = sub
	f.Activated = append(f.Activated, id)
	return &dtos.ActivateSubscriptionResponse{}, nil
}

func (f *Subscriptions) GetEntitlements(_ context.Context, id string, _ []string) (*dtos.GetSubscriptionEntitlementsResponse, error) {
	if f.Entitlements != nil {
		return f.Entitlements(id)
	}
	return &dtos.GetSubscriptionEntitlementsResponse{}, nil
}

func (f *Subscriptions) GetUsage(_ context.Context, req types.GetUsageBySubscriptionRequest) (*dtos.GetSubscriptionUsageResponse, error) {
	if f.Usage != nil {
		return f.Usage(req)
	}
	amount := 0.0
	return &dtos.GetSubscriptionUsageResponse{GetUsageBySubscriptionResponse: &types.GetUsageBySubscriptionResponse{Amount: &amount}}, nil
}

func (f *Subscriptions) CreateLineItem(_ context.Context, id string, req types.CreateSubscriptionLineItemRequest) (*dtos.CreateSubscriptionLineItemResponse, error) {
	f.mu.Lock()
	fn := f.CreateLineItemFn
	f.mu.Unlock()
	if fn != nil {
		if err := fn(id, req); err != nil {
			return nil, err
		}
		return &dtos.CreateSubscriptionLineItemResponse{}, nil
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	sub, ok := f.Items[id]
	if !ok {
		return nil, NotFound()
	}
	sub.LineItems = append(sub.LineItems, types.SubscriptionSubscriptionLineItem{ID: Ptr(fmt.Sprintf("li_new_%d", len(sub.LineItems)+1)), PriceID: req.PriceID})
	f.Items[id] = sub
	f.LineItems = append(f.LineItems, req)
	return &dtos.CreateSubscriptionLineItemResponse{}, nil
}

func (f *Subscriptions) UpdateLineItem(_ context.Context, _ string, _ types.UpdateSubscriptionLineItemRequest) (*dtos.UpdateSubscriptionLineItemResponse, error) {
	return &dtos.UpdateSubscriptionLineItemResponse{}, nil
}

// ── Wallets ───────────────────────────────────────────────────────────

type TopUp struct {
	WalletID string
	Amount   string
}

type Wallets struct {
	client *Client
	mu     sync.Mutex
	nextID int

	Items       map[string]types.WalletResponse // by wallet id
	Created     []types.CreateWalletRequest
	TopUps      []TopUp
	AlertStates map[string]types.AlertState

	CreateErr  error
	BalanceErr error
	TopUpErr   error
	// Balance overrides GetBalance when set.
	Balance func(walletID string) (*dtos.GetWalletBalanceResponse, error)
}

func (f *Wallets) Create(_ context.Context, req types.CreateWalletRequest) (*dtos.CreateWalletResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.CreateErr != nil {
		return nil, f.CreateErr
	}
	f.nextID++
	id := fmt.Sprintf("wal_%d", f.nextID)
	custID := ""
	if req.CustomerID != nil {
		custID = *req.CustomerID
	} else if req.ExternalCustomerID != nil {
		custID = f.client.Customer.Internal(*req.ExternalCustomerID)
	}
	w := types.WalletResponse{ID: Ptr(id), CustomerID: Ptr(custID), Currency: Ptr(req.Currency), Balance: Ptr("0.00"), RealTimeBalance: Ptr("0.00"), AlertSettings: req.AlertSettings, Metadata: req.Metadata, CreatedAt: ptrTime(f.client.now())}
	f.Items[id] = w
	f.Created = append(f.Created, req)
	return &dtos.CreateWalletResponse{WalletResponse: &w}, nil
}

func (f *Wallets) Query(_ context.Context, _ types.WalletFilter) (*dtos.QueryWalletResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	items := make([]types.WalletResponse, 0, len(f.Items))
	for _, w := range f.Items {
		items = append(items, w)
	}
	return &dtos.QueryWalletResponse{ListResponseDtoWalletResponse: &types.ListResponseDtoWalletResponse{Items: items}}, nil
}

func (f *Wallets) GetWalletsByCustomerID(_ context.Context, customerID string) (*dtos.GetWalletsByCustomerIDResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var items []types.WalletResponse
	for _, w := range f.Items {
		if w.CustomerID != nil && *w.CustomerID == customerID {
			items = append(items, w)
		}
	}
	return &dtos.GetWalletsByCustomerIDResponse{WalletResponses: items}, nil
}

func (f *Wallets) GetBalance(_ context.Context, id string) (*dtos.GetWalletBalanceResponse, error) {
	if f.Balance != nil {
		return f.Balance(id)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.BalanceErr != nil {
		return nil, f.BalanceErr
	}
	w, ok := f.Items[id]
	if !ok {
		return nil, NotFound()
	}
	state, has := f.AlertStates[id]
	if !has {
		state = types.AlertStateOk
	}
	return &dtos.GetWalletBalanceResponse{WalletBalanceResponse: &types.WalletBalanceResponse{
		ID: w.ID, CustomerID: w.CustomerID, Balance: w.Balance, RealTimeBalance: w.RealTimeBalance, AlertState: &state, AlertSettings: w.AlertSettings,
	}}, nil
}

func (f *Wallets) TopUp(_ context.Context, id string, req types.TopUpWalletRequest) (*dtos.TopUpWalletResponse, error) {
	if req.Checkout != nil {
		return f.checkoutTopUp(id, req)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.TopUpErr != nil {
		return nil, f.TopUpErr
	}
	w, ok := f.Items[id]
	if !ok {
		return nil, NotFound()
	}
	amount := "0"
	if req.Amount != nil {
		amount = *req.Amount
	}
	w.Balance = Ptr(addDecimal(*w.Balance, amount))
	w.RealTimeBalance = Ptr(addDecimal(*w.RealTimeBalance, amount))
	f.Items[id] = w
	f.TopUps = append(f.TopUps, TopUp{WalletID: id, Amount: amount})
	return &dtos.TopUpWalletResponse{}, nil
}

// Debit lowers a wallet's real-time balance, as pending usage charges would.
func (f *Wallets) Debit(id, amount string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	w := f.Items[id]
	w.RealTimeBalance = Ptr(addDecimal(*w.RealTimeBalance, "-"+amount))
	f.Items[id] = w
}

// checkoutTopUp opens a checkout session for a pay-first top-up, as the server
// does; credits land only when a test hook settles the payment.
func (f *Wallets) checkoutTopUp(id string, req types.TopUpWalletRequest) (*dtos.TopUpWalletResponse, error) {
	f.mu.Lock()
	err := f.TopUpErr
	_, ok := f.Items[id]
	f.mu.Unlock()
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, NotFound()
	}
	s, err := f.client.Payment.startSession("topup", req.Checkout)
	if err != nil {
		return nil, err
	}
	return &dtos.TopUpWalletResponse{TopUpWalletResponse: &types.TopUpWalletResponse{CheckoutSession: s}}, nil
}

// ── Events ────────────────────────────────────────────────────────────

type Events struct {
	mu sync.Mutex

	Ingested          []types.IngestEventRequest
	Batches           [][]types.IngestEventRequest
	AnalyticsRequests []types.GetUsageAnalyticsRequest

	IngestErr error
	Analytics func(req types.GetUsageAnalyticsRequest) (*dtos.GetUsageAnalyticsResponse, error)
}

func (f *Events) Ingest(_ context.Context, req types.IngestEventRequest) (*dtos.IngestEventResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.IngestErr != nil {
		return nil, f.IngestErr
	}
	f.Ingested = append(f.Ingested, req)
	return &dtos.IngestEventResponse{}, nil
}

func (f *Events) IngestBulk(_ context.Context, req types.BulkIngestEventRequest) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.IngestErr != nil {
		return f.IngestErr
	}
	f.Ingested = append(f.Ingested, req.Events...)
	f.Batches = append(f.Batches, req.Events)
	return nil
}

func (f *Events) GetUsageAnalytics(_ context.Context, req types.GetUsageAnalyticsRequest) (*dtos.GetUsageAnalyticsResponse, error) {
	f.mu.Lock()
	f.AnalyticsRequests = append(f.AnalyticsRequests, req)
	fn := f.Analytics
	f.mu.Unlock()
	if fn != nil {
		return fn(req)
	}
	return &dtos.GetUsageAnalyticsResponse{GetUsageAnalyticsResponse: &types.GetUsageAnalyticsResponse{}}, nil
}

// ── Invoices ──────────────────────────────────────────────────────────

type Invoices struct {
	mu sync.Mutex

	Items        []types.InvoiceResponse // newest first, as the API lists them
	ByID         map[string]types.InvoiceResponse
	PreviewCalls []types.GetPreviewInvoiceRequest
	Queries      []types.InvoiceFilter

	QueryErr error
	GetErr   error
	Preview  func(req types.GetPreviewInvoiceRequest) (*dtos.GetInvoicePreviewResponse, error)

	// SubscriptionFilterEmpty makes a query by subscription id return no items,
	// as the server has for subscriptions that do have invoices.
	SubscriptionFilterEmpty bool
}

func (f *Invoices) Query(_ context.Context, filter types.InvoiceFilter) (*dtos.QueryInvoiceResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Queries = append(f.Queries, filter)
	if f.QueryErr != nil {
		return nil, f.QueryErr
	}
	if f.SubscriptionFilterEmpty && filter.SubscriptionID != nil {
		return &dtos.QueryInvoiceResponse{ListInvoicesResponse: &types.ListInvoicesResponse{}}, nil
	}
	var items []types.InvoiceResponse
	for _, inv := range f.Items {
		if filter.CustomerID != nil && (inv.CustomerID == nil || *inv.CustomerID != *filter.CustomerID) {
			continue
		}
		if filter.SubscriptionID != nil && (inv.SubscriptionID == nil || *inv.SubscriptionID != *filter.SubscriptionID) {
			continue
		}
		if len(filter.InvoiceStatus) > 0 && (inv.InvoiceStatus == nil || !containsStatus(filter.InvoiceStatus, *inv.InvoiceStatus)) {
			continue
		}
		items = append(items, inv)
	}
	if filter.Order != nil && *filter.Order == types.InvoiceFilterOrderAsc {
		for i, j := 0, len(items)-1; i < j; i, j = i+1, j-1 {
			items[i], items[j] = items[j], items[i]
		}
	}
	if filter.Limit != nil && *filter.Limit > 0 && int(*filter.Limit) < len(items) {
		items = items[:*filter.Limit]
	}
	return &dtos.QueryInvoiceResponse{ListInvoicesResponse: &types.ListInvoicesResponse{Items: items}}, nil
}

func containsStatus(list []types.InvoiceStatus, s types.InvoiceStatus) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func (f *Invoices) Get(_ context.Context, id string) (*dtos.GetInvoiceResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.GetErr != nil {
		return nil, f.GetErr
	}
	if inv, ok := f.ByID[id]; ok {
		return &dtos.GetInvoiceResponse{InvoiceResponse: &inv}, nil
	}
	for _, inv := range f.Items {
		if inv.ID != nil && *inv.ID == id {
			return &dtos.GetInvoiceResponse{InvoiceResponse: &inv}, nil
		}
	}
	return nil, NotFound()
}

func (f *Invoices) GetPreview(_ context.Context, req types.GetPreviewInvoiceRequest) (*dtos.GetInvoicePreviewResponse, error) {
	f.mu.Lock()
	f.PreviewCalls = append(f.PreviewCalls, req)
	fn := f.Preview
	f.mu.Unlock()
	if fn != nil {
		return fn(req)
	}
	return &dtos.GetInvoicePreviewResponse{InvoiceResponse: &types.InvoiceResponse{}}, nil
}

// ── Entitlements ──────────────────────────────────────────────────────

type Entitlements struct {
	mu sync.Mutex

	Items             []types.EntitlementResponse
	Created           []types.CreateEntitlementRequest
	CreatedWithGrant  []e2eprobe.GrantEntitlementInput
	Grants            map[string]e2eprobe.GrantEntitlementResponse
	Deleted           []string
	GetRawCalls       []string
	CreateErr         error
	CreateGrantErr    error
	GetRawErr         error
	GetRawFn          func(id string) (*e2eprobe.GrantEntitlementResponse, error)
	grantSeq, itemSeq int
}

func (f *Entitlements) Create(_ context.Context, req types.CreateEntitlementRequest) (*dtos.CreateEntitlementResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.CreateErr != nil {
		return nil, f.CreateErr
	}
	f.itemSeq++
	id := fmt.Sprintf("ent_%d", f.itemSeq)
	ent := types.EntitlementResponse{ID: Ptr(id), FeatureID: req.FeatureID, PlanID: req.PlanID, EntityID: req.EntityID, UsageLimit: req.UsageLimit, IsSoftLimit: req.IsSoftLimit, IsEnabled: req.IsEnabled}
	f.Items = append(f.Items, ent)
	f.Created = append(f.Created, req)
	return &dtos.CreateEntitlementResponse{EntitlementResponse: &ent}, nil
}

func (f *Entitlements) Query(_ context.Context, filter types.EntitlementFilter) (*dtos.QueryEntitlementResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	plans := map[string]bool{}
	for _, id := range filter.PlanIds {
		plans[id] = true
	}
	features := map[string]bool{}
	for _, id := range filter.FeatureIds {
		features[id] = true
	}
	var items []types.EntitlementResponse
	for _, e := range f.Items {
		if len(plans) > 0 && (e.PlanID == nil || !plans[*e.PlanID]) {
			continue
		}
		if len(features) > 0 && (e.FeatureID == nil || !features[*e.FeatureID]) {
			continue
		}
		items = append(items, e)
	}
	return &dtos.QueryEntitlementResponse{ListEntitlementsResponse: &types.ListEntitlementsResponse{Items: items}}, nil
}

func (f *Entitlements) Delete(_ context.Context, id string) (*dtos.DeleteEntitlementResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	kept := f.Items[:0]
	for _, e := range f.Items {
		if e.ID == nil || *e.ID != id {
			kept = append(kept, e)
		}
	}
	f.Items = kept
	f.Deleted = append(f.Deleted, id)
	return &dtos.DeleteEntitlementResponse{}, nil
}

func (f *Entitlements) CreateWithGrant(_ context.Context, req e2eprobe.GrantEntitlementInput) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.CreateGrantErr != nil {
		return "", f.CreateGrantErr
	}
	f.grantSeq++
	id := fmt.Sprintf("ent_grant_%d", f.grantSeq)
	dv := req.GrantDurationValue
	f.Grants[id] = e2eprobe.GrantEntitlementResponse{ID: id, FeatureID: req.FeatureID, PlanID: req.PlanID, GrantMeasure: req.GrantMeasure, GrantQuota: req.GrantQuota, GrantDurationValue: &dv, GrantDurationUnit: req.GrantDurationUnit, AggregationMode: req.AggregationMode, IsEnabled: req.IsEnabled}
	f.Items = append(f.Items, types.EntitlementResponse{ID: Ptr(id), FeatureID: Ptr(req.FeatureID), PlanID: Ptr(req.PlanID), EntityID: Ptr(req.EntityID), IsEnabled: Ptr(req.IsEnabled)})
	f.CreatedWithGrant = append(f.CreatedWithGrant, req)
	return id, nil
}

func (f *Entitlements) GetRaw(_ context.Context, id string) (*e2eprobe.GrantEntitlementResponse, error) {
	f.mu.Lock()
	f.GetRawCalls = append(f.GetRawCalls, id)
	fn := f.GetRawFn
	err := f.GetRawErr
	grant, ok := f.Grants[id]
	f.mu.Unlock()
	if err != nil {
		return nil, err
	}
	if fn != nil {
		return fn(id)
	}
	if ok {
		return &grant, nil
	}
	return &e2eprobe.GrantEntitlementResponse{ID: id}, nil
}

// ── Coupons ───────────────────────────────────────────────────────────

type Coupons struct {
	client *Client
	mu     sync.Mutex

	ByCode    map[string]string
	Created   []types.CreateCouponRequest
	CreateErr error
}

func (f *Coupons) idFor(code string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.ByCode[code]
}

func (f *Coupons) Create(_ context.Context, req types.CreateCouponRequest) (*dtos.CreateCouponResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.CreateErr != nil {
		return nil, f.CreateErr
	}
	code := ""
	if req.CouponCode != nil {
		code = *req.CouponCode
	}
	if _, exists := f.ByCode[code]; exists {
		return nil, Conflict()
	}
	id := fmt.Sprintf("coupon_%d", len(f.ByCode)+1)
	f.ByCode[code] = id
	f.Created = append(f.Created, req)
	return &dtos.CreateCouponResponse{CouponResponse: &types.CouponResponse{ID: Ptr(id), CouponCode: req.CouponCode, Name: Ptr(req.Name)}}, nil
}

func (f *Coupons) Query(_ context.Context, filter types.CouponFilter) (*dtos.QueryCouponResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var items []types.CouponResponse
	for _, code := range filter.CouponCodes {
		if id, ok := f.ByCode[code]; ok {
			items = append(items, types.CouponResponse{ID: Ptr(id), CouponCode: Ptr(code)})
		}
	}
	return &dtos.QueryCouponResponse{ListCouponsResponse: &types.ListCouponsResponse{Items: items}}, nil
}

func (f *Coupons) GetByCode(_ context.Context, code string) (*dtos.GetCouponByCodeResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	id, ok := f.ByCode[code]
	if !ok {
		return nil, NotFound()
	}
	return &dtos.GetCouponByCodeResponse{CouponResponse: &types.CouponResponse{ID: Ptr(id), CouponCode: Ptr(code)}}, nil
}

func (f *Coupons) Delete(_ context.Context, _ string) (*dtos.DeleteCouponResponse, error) {
	return &dtos.DeleteCouponResponse{}, nil
}

// ── Coupon associations ───────────────────────────────────────────────

type CouponAssociations struct {
	client *Client
	mu     sync.Mutex

	Items   []types.CouponAssociationResponse
	ListErr error
}

func (f *CouponAssociations) attach(subID, couponID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Items = append(f.Items, types.CouponAssociationResponse{ID: Ptr(fmt.Sprintf("ca_%d", len(f.Items)+1)), SubscriptionID: Ptr(subID), CouponID: Ptr(couponID)})
}

func (f *CouponAssociations) List(_ context.Context, req dtos.ListCouponAssociationsRequest) (*dtos.ListCouponAssociationsResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.ListErr != nil {
		return nil, f.ListErr
	}
	subs := map[string]bool{}
	for _, id := range req.SubscriptionIds {
		subs[id] = true
	}
	var items []types.CouponAssociationResponse
	for _, ca := range f.Items {
		if len(subs) > 0 && (ca.SubscriptionID == nil || !subs[*ca.SubscriptionID]) {
			continue
		}
		items = append(items, ca)
	}
	return &dtos.ListCouponAssociationsResponse{ListCouponAssociationsResponse: &types.ListCouponAssociationsResponse{Items: items}}, nil
}

// ── Tax rates ─────────────────────────────────────────────────────────

type TaxRates struct {
	client *Client
	mu     sync.Mutex

	ByCode    map[string]string
	Created   []types.CreateTaxRateRequest
	CreateErr error
}

func (f *TaxRates) Create(_ context.Context, req types.CreateTaxRateRequest) (*dtos.CreateTaxRateResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.CreateErr != nil {
		return nil, f.CreateErr
	}
	if _, exists := f.ByCode[req.Code]; exists {
		return nil, Conflict()
	}
	id := fmt.Sprintf("taxrate_%d", len(f.ByCode)+1)
	f.ByCode[req.Code] = id
	f.Created = append(f.Created, req)
	return &dtos.CreateTaxRateResponse{TaxRateResponse: &types.TaxRateResponse{ID: Ptr(id), Code: Ptr(req.Code)}}, nil
}

func (f *TaxRates) Get(_ context.Context, id string) (*dtos.GetTaxRateResponse, error) {
	return &dtos.GetTaxRateResponse{TaxRateResponse: &types.TaxRateResponse{ID: Ptr(id)}}, nil
}

func (f *TaxRates) List(_ context.Context, _ dtos.GetTaxRatesRequest) (*dtos.GetTaxRatesResponse, error) {
	return &dtos.GetTaxRatesResponse{}, nil
}

func (f *TaxRates) Delete(_ context.Context, _ string) (*dtos.DeleteTaxRateResponse, error) {
	return &dtos.DeleteTaxRateResponse{}, nil
}

// ── Tax associations ──────────────────────────────────────────────────

type TaxAssociations struct {
	client *Client
	mu     sync.Mutex

	Items     []types.TaxAssociationResponse
	Created   []types.CreateTaxAssociationRequest
	Deleted   []string
	CreateErr error
	ListErr   error
	DeleteErr error
}

func (f *TaxAssociations) Create(_ context.Context, req types.CreateTaxAssociationRequest) (*dtos.CreateTaxAssociationResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.CreateErr != nil {
		return nil, f.CreateErr
	}
	rateID := f.client.TaxRate.idFor(req.TaxRateCode)
	ta := types.TaxAssociationResponse{
		ID:         Ptr(fmt.Sprintf("taxassoc_%d", len(f.Items)+1)),
		EntityID:   req.EntityID,
		EntityType: req.EntityType,
		TaxRateID:  Ptr(rateID),
		TaxRate:    &types.TaxRateResponse{ID: Ptr(rateID), Code: Ptr(req.TaxRateCode)},
		StartDate:  ptrTime(f.client.now()),
		CreatedAt:  ptrTime(f.client.now()),
		Metadata:   req.Metadata,
	}
	f.Items = append(f.Items, ta)
	f.Created = append(f.Created, req)
	return &dtos.CreateTaxAssociationResponse{TaxAssociationResponse: &ta}, nil
}

func (f *TaxRates) idFor(code string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.ByCode[code]
}

func (f *TaxAssociations) List(_ context.Context, entityType, entityID, _, taxRateID *string) (*dtos.ListTaxAssociationsResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.ListErr != nil {
		return nil, f.ListErr
	}
	var items []types.TaxAssociationResponse
	for _, ta := range f.Items {
		if entityType != nil && (ta.EntityType == nil || string(*ta.EntityType) != *entityType) {
			continue
		}
		if entityID != nil && (ta.EntityID == nil || *ta.EntityID != *entityID) {
			continue
		}
		if taxRateID != nil && (ta.TaxRateID == nil || *ta.TaxRateID != *taxRateID) {
			continue
		}
		items = append(items, ta)
	}
	return &dtos.ListTaxAssociationsResponse{ListTaxAssociationsResponse: &types.ListTaxAssociationsResponse{Items: items}}, nil
}

func (f *TaxAssociations) Delete(_ context.Context, id string) (*dtos.DeleteTaxAssociationResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.DeleteErr != nil {
		return nil, f.DeleteErr
	}
	kept := f.Items[:0]
	found := false
	for _, ta := range f.Items {
		if ta.ID != nil && *ta.ID == id {
			found = true
			continue
		}
		kept = append(kept, ta)
	}
	f.Items = kept
	if !found {
		return nil, NotFound()
	}
	f.Deleted = append(f.Deleted, id)
	return &dtos.DeleteTaxAssociationResponse{}, nil
}

// ── Alerts ────────────────────────────────────────────────────────────

type Alerts struct {
	mu sync.Mutex

	Logs    []types.AlertLogResponse
	Queries []types.AlertLogFilter
	Err     error
}

func (f *Alerts) QueryLogs(_ context.Context, filter types.AlertLogFilter) ([]types.AlertLogResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Queries = append(f.Queries, filter)
	if f.Err != nil {
		return nil, f.Err
	}
	var out []types.AlertLogResponse
	for _, l := range f.Logs {
		if filter.EntityID != nil && (l.EntityID == nil || *l.EntityID != *filter.EntityID) {
			continue
		}
		if filter.CustomerID != nil && (l.CustomerID == nil || *l.CustomerID != *filter.CustomerID) {
			continue
		}
		if filter.StartTime != nil && l.CreatedAt != nil && l.CreatedAt.Before(*filter.StartTime) {
			continue
		}
		out = append(out, l)
	}
	return out, nil
}

// ── Tenant ────────────────────────────────────────────────────────────

type Tenant struct {
	mu sync.Mutex

	Connections    []e2eprobe.Connection
	Settings       map[string]map[string]any
	ConnectionsErr error
	SettingsErr    error
}

func (f *Tenant) ListConnections(_ context.Context) ([]e2eprobe.Connection, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.ConnectionsErr != nil {
		return nil, f.ConnectionsErr
	}
	return append([]e2eprobe.Connection(nil), f.Connections...), nil
}

func (f *Tenant) GetSetting(_ context.Context, key string) (map[string]any, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.SettingsErr != nil {
		return nil, f.SettingsErr
	}
	v, ok := f.Settings[key]
	if !ok {
		return nil, NotFound()
	}
	return v, nil
}

// ── Raw ───────────────────────────────────────────────────────────────

type Raw struct {
	mu sync.Mutex

	Calls []string // "METHOD path"
	DoFn  func(method, path string, body, out any) error
}

func (f *Raw) Do(_ context.Context, method, path string, body, out any) error {
	f.mu.Lock()
	f.Calls = append(f.Calls, method+" "+path)
	fn := f.DoFn
	f.mu.Unlock()
	if fn != nil {
		return fn(method, path, body, out)
	}
	return nil
}
