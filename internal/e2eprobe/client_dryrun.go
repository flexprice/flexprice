package e2eprobe

import (
	"context"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/logger"
	flexprice "github.com/flexprice/go-sdk/v2"
	"github.com/flexprice/go-sdk/v2/models/dtos"
	"github.com/flexprice/go-sdk/v2/models/types"
)

// NewDryRunClient wraps a Client so all mutating SDK calls become logged no-ops.
// Read operations pass through to the underlying client unchanged.
func NewDryRunClient(inner Client, lg *logger.Logger) Client {
	return &dryRunClient{inner: inner, lg: lg}
}

type dryRunClient struct {
	inner Client
	lg    *logger.Logger
}

func (c *dryRunClient) Customers() CustomerOps {
	return &dryRunCustomers{inner: c.inner.Customers(), lg: c.lg}
}
func (c *dryRunClient) Plans() PlanOps {
	return &dryRunPlans{inner: c.inner.Plans(), lg: c.lg}
}
func (c *dryRunClient) Prices() PriceOps {
	return &dryRunPrices{inner: c.inner.Prices(), lg: c.lg}
}
func (c *dryRunClient) Features() FeatureOps {
	return &dryRunFeatures{inner: c.inner.Features(), lg: c.lg}
}
func (c *dryRunClient) Subscriptions() SubscriptionOps {
	return &dryRunSubscriptions{inner: c.inner.Subscriptions(), lg: c.lg}
}
func (c *dryRunClient) Wallets() WalletOps {
	return &dryRunWallets{inner: c.inner.Wallets(), lg: c.lg}
}
func (c *dryRunClient) Events() EventOps {
	return &dryRunEvents{inner: c.inner.Events(), lg: c.lg}
}

// Invoices are all read-only — delegate directly.
func (c *dryRunClient) Invoices() InvoiceOps { return c.inner.Invoices() }

func (c *dryRunClient) NewAsyncEventClient() AsyncEventClient {
	return &dryRunAsync{inner: c.inner.NewAsyncEventClient(), lg: c.lg}
}
func (c *dryRunClient) Entitlements() EntitlementOps {
	return &dryRunEntitlements{inner: c.inner.Entitlements(), lg: c.lg}
}
func (c *dryRunClient) Coupons() CouponOps {
	return &dryRunCoupons{inner: c.inner.Coupons(), lg: c.lg}
}

// CouponAssociations are read-only — delegate directly.
func (c *dryRunClient) CouponAssociations() CouponAssociationOps { return c.inner.CouponAssociations() }
func (c *dryRunClient) TaxRates() TaxRateOps {
	return &dryRunTaxRates{inner: c.inner.TaxRates(), lg: c.lg}
}
func (c *dryRunClient) TaxAssociations() TaxAssociationOps {
	return &dryRunTaxAssociations{inner: c.inner.TaxAssociations(), lg: c.lg}
}

// dryLog logs a skipped mutation at Info level.
func dryLog(ctx context.Context, lg *logger.Logger, op string, kv ...any) {
	if lg == nil {
		return
	}
	fields := append([]any{"event", "e2eprobe.dryrun.skip", "op", op}, kv...)
	lg.Info(ctx, "dry-run: skipped mutation", fields...)
}

func strPtrDryRun(s string) *string { return &s }

// ── Customers ─────────────────────────────────────────────────────────

type dryRunCustomers struct {
	inner CustomerOps
	lg    *logger.Logger
}

func (d *dryRunCustomers) Create(ctx context.Context, req types.CreateCustomerRequest) (*dtos.CreateCustomerResponse, error) {
	dryLog(ctx, d.lg, "Customers.Create", "external_id", req.ExternalID)
	return &dtos.CreateCustomerResponse{}, nil
}
func (d *dryRunCustomers) GetByExternalID(ctx context.Context, externalID string) (*dtos.GetCustomerByExternalIDResponse, error) {
	return d.inner.GetByExternalID(ctx, externalID)
}
func (d *dryRunCustomers) Get(ctx context.Context, id string) (*dtos.GetCustomerResponse, error) {
	return d.inner.Get(ctx, id)
}
func (d *dryRunCustomers) GetEntitlements(ctx context.Context, id string) (*dtos.GetCustomerEntitlementsResponse, error) {
	return d.inner.GetEntitlements(ctx, id)
}
func (d *dryRunCustomers) GetUsageSummary(ctx context.Context, req dtos.GetCustomerUsageSummaryRequest) (*dtos.GetCustomerUsageSummaryResponse, error) {
	return d.inner.GetUsageSummary(ctx, req)
}
func (d *dryRunCustomers) Update(ctx context.Context, _ types.UpdateCustomerRequest, id, _ *string) (*dtos.UpdateCustomerResponse, error) {
	idVal := ""
	if id != nil {
		idVal = *id
	}
	dryLog(ctx, d.lg, "Customers.Update", "id", idVal)
	return &dtos.UpdateCustomerResponse{}, nil
}
func (d *dryRunCustomers) Delete(ctx context.Context, id string) (*dtos.DeleteCustomerResponse, error) {
	dryLog(ctx, d.lg, "Customers.Delete", "id", id)
	return &dtos.DeleteCustomerResponse{}, nil
}
func (d *dryRunCustomers) Query(ctx context.Context, filter types.CustomerFilter) (*dtos.QueryCustomerResponse, error) {
	return d.inner.Query(ctx, filter)
}

// ── Plans ─────────────────────────────────────────────────────────────

type dryRunPlans struct {
	inner PlanOps
	lg    *logger.Logger
}

func (d *dryRunPlans) Create(ctx context.Context, req types.CreatePlanRequest) (*dtos.CreatePlanResponse, error) {
	dryLog(ctx, d.lg, "Plans.Create", "name", req.Name)
	fakeID := fmt.Sprintf("plan_dryrun_%d", time.Now().UnixNano())
	return &dtos.CreatePlanResponse{
		PlanResponse: &types.PlanResponse{ID: strPtrDryRun(fakeID)},
	}, nil
}
func (d *dryRunPlans) Query(ctx context.Context, filter types.PlanFilter) (*dtos.QueryPlanResponse, error) {
	return d.inner.Query(ctx, filter)
}
func (d *dryRunPlans) Get(ctx context.Context, id string) (*dtos.GetPlanResponse, error) {
	return d.inner.Get(ctx, id)
}

// SyncPrices mutates existing subscriptions' line items — never run it in dry run.
func (d *dryRunPlans) SyncPrices(ctx context.Context, planID string) (*dtos.SyncPlanPricesResponse, error) {
	dryLog(ctx, d.lg, "Plans.SyncPrices", "plan_id", planID)
	return &dtos.SyncPlanPricesResponse{}, nil
}

// ── Prices ────────────────────────────────────────────────────────────

type dryRunPrices struct {
	inner PriceOps
	lg    *logger.Logger
}

func (d *dryRunPrices) Create(ctx context.Context, req types.CreatePriceRequest) (*dtos.CreatePriceResponse, error) {
	dryLog(ctx, d.lg, "Prices.Create", "lookup_key", req.LookupKey)
	return &dtos.CreatePriceResponse{}, nil
}
func (d *dryRunPrices) Query(ctx context.Context, filter types.PriceFilter) (*dtos.QueryPriceResponse, error) {
	return d.inner.Query(ctx, filter)
}

// ── Features ──────────────────────────────────────────────────────────

type dryRunFeatures struct {
	inner FeatureOps
	lg    *logger.Logger
}

func (d *dryRunFeatures) Create(ctx context.Context, req types.CreateFeatureRequest) (*dtos.CreateFeatureResponse, error) {
	dryLog(ctx, d.lg, "Features.Create", "lookup_key", req.LookupKey)
	fakeID := fmt.Sprintf("feature_dryrun_%d", time.Now().UnixNano())
	fakeMeterID := fmt.Sprintf("meter_dryrun_%d", time.Now().UnixNano())
	return &dtos.CreateFeatureResponse{
		FeatureResponse: &types.FeatureResponse{
			ID:      strPtrDryRun(fakeID),
			MeterID: strPtrDryRun(fakeMeterID),
		},
	}, nil
}
func (d *dryRunFeatures) Query(ctx context.Context, filter types.FeatureFilter) (*dtos.QueryFeatureResponse, error) {
	return d.inner.Query(ctx, filter)
}

// ── Subscriptions ─────────────────────────────────────────────────────

type dryRunSubscriptions struct {
	inner SubscriptionOps
	lg    *logger.Logger
}

func (d *dryRunSubscriptions) Create(ctx context.Context, req types.CreateSubscriptionRequest) (*dtos.CreateSubscriptionResponse, error) {
	extID := ""
	if req.ExternalCustomerID != nil {
		extID = *req.ExternalCustomerID
	}
	dryLog(ctx, d.lg, "Subscriptions.Create", "external_customer_id", extID)
	fakeID := fmt.Sprintf("sub_dryrun_%d", time.Now().UnixNano())
	return &dtos.CreateSubscriptionResponse{
		SubscriptionResponse: &types.SubscriptionResponse{ID: strPtrDryRun(fakeID)},
	}, nil
}
func (d *dryRunSubscriptions) Get(ctx context.Context, id string) (*dtos.GetSubscriptionResponse, error) {
	return d.inner.Get(ctx, id)
}
func (d *dryRunSubscriptions) Cancel(ctx context.Context, id string, _ types.CancelSubscriptionRequest) (*dtos.CancelSubscriptionResponse, error) {
	dryLog(ctx, d.lg, "Subscriptions.Cancel", "id", id)
	return &dtos.CancelSubscriptionResponse{}, nil
}
func (d *dryRunSubscriptions) Query(ctx context.Context, filter types.SubscriptionFilter) (*dtos.QuerySubscriptionResponse, error) {
	return d.inner.Query(ctx, filter)
}
func (d *dryRunSubscriptions) ActivateSubscription(ctx context.Context, id string, _ types.ActivateDraftSubscriptionRequest) (*dtos.ActivateSubscriptionResponse, error) {
	dryLog(ctx, d.lg, "Subscriptions.ActivateSubscription", "id", id)
	return &dtos.ActivateSubscriptionResponse{}, nil
}
func (d *dryRunSubscriptions) GetEntitlements(ctx context.Context, id string, featureIDs []string) (*dtos.GetSubscriptionEntitlementsResponse, error) {
	return d.inner.GetEntitlements(ctx, id, featureIDs)
}
func (d *dryRunSubscriptions) GetUsage(ctx context.Context, req types.GetUsageBySubscriptionRequest) (*dtos.GetSubscriptionUsageResponse, error) {
	return d.inner.GetUsage(ctx, req)
}
func (d *dryRunSubscriptions) CreateLineItem(ctx context.Context, id string, _ types.CreateSubscriptionLineItemRequest) (*dtos.CreateSubscriptionLineItemResponse, error) {
	dryLog(ctx, d.lg, "Subscriptions.CreateLineItem", "id", id)
	return &dtos.CreateSubscriptionLineItemResponse{}, nil
}
func (d *dryRunSubscriptions) UpdateLineItem(ctx context.Context, id string, _ types.UpdateSubscriptionLineItemRequest) (*dtos.UpdateSubscriptionLineItemResponse, error) {
	dryLog(ctx, d.lg, "Subscriptions.UpdateLineItem", "id", id)
	return &dtos.UpdateSubscriptionLineItemResponse{}, nil
}

// ── Wallets ───────────────────────────────────────────────────────────

type dryRunWallets struct {
	inner WalletOps
	lg    *logger.Logger
}

func (d *dryRunWallets) Create(ctx context.Context, req types.CreateWalletRequest) (*dtos.CreateWalletResponse, error) {
	extID := ""
	if req.ExternalCustomerID != nil {
		extID = *req.ExternalCustomerID
	}
	dryLog(ctx, d.lg, "Wallets.Create", "external_customer_id", extID)
	fakeID := fmt.Sprintf("wallet_dryrun_%d", time.Now().UnixNano())
	return &dtos.CreateWalletResponse{
		WalletResponse: &types.WalletResponse{ID: strPtrDryRun(fakeID)},
	}, nil
}
func (d *dryRunWallets) Query(ctx context.Context, filter types.WalletFilter) (*dtos.QueryWalletResponse, error) {
	return d.inner.Query(ctx, filter)
}
func (d *dryRunWallets) GetWalletsByCustomerID(ctx context.Context, customerID string) (*dtos.GetWalletsByCustomerIDResponse, error) {
	return d.inner.GetWalletsByCustomerID(ctx, customerID)
}
func (d *dryRunWallets) GetBalance(ctx context.Context, id string) (*dtos.GetWalletBalanceResponse, error) {
	return d.inner.GetBalance(ctx, id)
}
func (d *dryRunWallets) TopUp(ctx context.Context, id string, _ types.TopUpWalletRequest) (*dtos.TopUpWalletResponse, error) {
	dryLog(ctx, d.lg, "Wallets.TopUp", "id", id)
	return &dtos.TopUpWalletResponse{}, nil
}

// ── Events ────────────────────────────────────────────────────────────

type dryRunEvents struct {
	inner EventOps
	lg    *logger.Logger
}

func (d *dryRunEvents) Ingest(ctx context.Context, req types.IngestEventRequest) (*dtos.IngestEventResponse, error) {
	dryLog(ctx, d.lg, "Events.Ingest", "event_name", req.EventName)
	return &dtos.IngestEventResponse{}, nil
}
func (d *dryRunEvents) GetUsageAnalytics(ctx context.Context, req types.GetUsageAnalyticsRequest) (*dtos.GetUsageAnalyticsResponse, error) {
	return d.inner.GetUsageAnalytics(ctx, req)
}
func (d *dryRunEvents) ListRaw(ctx context.Context, req types.GetEventsRequest) (*dtos.ListRawEventsResponse, error) {
	return d.inner.ListRaw(ctx, req)
}

// ── AsyncEventClient ──────────────────────────────────────────────────

type dryRunAsync struct {
	inner AsyncEventClient
	lg    *logger.Logger
}

func (d *dryRunAsync) Enqueue(eventName, externalCustomerID string, properties map[string]any) error {
	dryLog(context.Background(), d.lg, "AsyncEventClient.Enqueue", "event_name", eventName, "external_customer_id", externalCustomerID)
	return nil
}
func (d *dryRunAsync) EnqueueWithOptions(opts flexprice.EventOptions) error {
	dryLog(context.Background(), d.lg, "AsyncEventClient.EnqueueWithOptions", "event_name", opts.EventName)
	return nil
}
func (d *dryRunAsync) Flush() error {
	dryLog(context.Background(), d.lg, "AsyncEventClient.Flush")
	return nil
}
func (d *dryRunAsync) Close() error {
	dryLog(context.Background(), d.lg, "AsyncEventClient.Close")
	return nil
}

// ── Entitlements ──────────────────────────────────────────────────────

type dryRunEntitlements struct {
	inner EntitlementOps
	lg    *logger.Logger
}

func (d *dryRunEntitlements) Create(ctx context.Context, req types.CreateEntitlementRequest) (*dtos.CreateEntitlementResponse, error) {
	dryLog(ctx, d.lg, "Entitlements.Create")
	return &dtos.CreateEntitlementResponse{}, nil
}
func (d *dryRunEntitlements) Query(ctx context.Context, req types.EntitlementFilter) (*dtos.QueryEntitlementResponse, error) {
	return d.inner.Query(ctx, req)
}
func (d *dryRunEntitlements) Delete(ctx context.Context, id string) (*dtos.DeleteEntitlementResponse, error) {
	dryLog(ctx, d.lg, "Entitlements.Delete", "id", id)
	return &dtos.DeleteEntitlementResponse{}, nil
}
func (d *dryRunEntitlements) CreateWithGrant(ctx context.Context, req GrantEntitlementInput) (string, error) {
	dryLog(ctx, d.lg, "Entitlements.CreateWithGrant",
		"feature_id", req.FeatureID,
		"grant_measure", req.GrantMeasure,
		"grant_quota", req.GrantQuota,
		"aggregation_mode", req.AggregationMode,
	)
	return "grant_dryrun", nil
}
func (d *dryRunEntitlements) GetRaw(ctx context.Context, id string) (*GrantEntitlementResponse, error) {
	return d.inner.GetRaw(ctx, id)
}

// ── Coupons ───────────────────────────────────────────────────────────

type dryRunCoupons struct {
	inner CouponOps
	lg    *logger.Logger
}

func (d *dryRunCoupons) Create(ctx context.Context, req types.CreateCouponRequest) (*dtos.CreateCouponResponse, error) {
	dryLog(ctx, d.lg, "Coupons.Create", "name", req.Name)
	return &dtos.CreateCouponResponse{}, nil
}
func (d *dryRunCoupons) Query(ctx context.Context, req types.CouponFilter) (*dtos.QueryCouponResponse, error) {
	return d.inner.Query(ctx, req)
}
func (d *dryRunCoupons) GetByCode(ctx context.Context, code string) (*dtos.GetCouponByCodeResponse, error) {
	return d.inner.GetByCode(ctx, code)
}
func (d *dryRunCoupons) Delete(ctx context.Context, id string) (*dtos.DeleteCouponResponse, error) {
	dryLog(ctx, d.lg, "Coupons.Delete", "id", id)
	return &dtos.DeleteCouponResponse{}, nil
}

// ── TaxRates ──────────────────────────────────────────────────────────

type dryRunTaxRates struct {
	inner TaxRateOps
	lg    *logger.Logger
}

func (d *dryRunTaxRates) Create(ctx context.Context, req types.CreateTaxRateRequest) (*dtos.CreateTaxRateResponse, error) {
	dryLog(ctx, d.lg, "TaxRates.Create", "name", req.Name)
	return &dtos.CreateTaxRateResponse{}, nil
}
func (d *dryRunTaxRates) Get(ctx context.Context, id string) (*dtos.GetTaxRateResponse, error) {
	return d.inner.Get(ctx, id)
}
func (d *dryRunTaxRates) List(ctx context.Context, req dtos.GetTaxRatesRequest) (*dtos.GetTaxRatesResponse, error) {
	return d.inner.List(ctx, req)
}
func (d *dryRunTaxRates) Delete(ctx context.Context, id string) (*dtos.DeleteTaxRateResponse, error) {
	dryLog(ctx, d.lg, "TaxRates.Delete", "id", id)
	return &dtos.DeleteTaxRateResponse{}, nil
}

// ── TaxAssociations ───────────────────────────────────────────────────

type dryRunTaxAssociations struct {
	inner TaxAssociationOps
	lg    *logger.Logger
}

func (d *dryRunTaxAssociations) Create(ctx context.Context, req types.CreateTaxAssociationRequest) (*dtos.CreateTaxAssociationResponse, error) {
	dryLog(ctx, d.lg, "TaxAssociations.Create")
	return &dtos.CreateTaxAssociationResponse{}, nil
}
func (d *dryRunTaxAssociations) List(ctx context.Context, entityType, entityID, externalCustomerID, taxRateID *string) (*dtos.ListTaxAssociationsResponse, error) {
	return d.inner.List(ctx, entityType, entityID, externalCustomerID, taxRateID)
}
func (d *dryRunTaxAssociations) Delete(ctx context.Context, id string) (*dtos.DeleteTaxAssociationResponse, error) {
	dryLog(ctx, d.lg, "TaxAssociations.Delete", "id", id)
	return &dtos.DeleteTaxAssociationResponse{}, nil
}
