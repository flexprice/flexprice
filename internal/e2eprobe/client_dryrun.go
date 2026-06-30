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

func (d *dryRunCustomers) Create(ctx context.Context, req types.DtoCreateCustomerRequest) (*dtos.CreateCustomerResponse, error) {
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
func (d *dryRunCustomers) Update(ctx context.Context, _ types.DtoUpdateCustomerRequest, id, _ *string) (*dtos.UpdateCustomerResponse, error) {
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

func (d *dryRunPlans) Create(ctx context.Context, req types.DtoCreatePlanRequest) (*dtos.CreatePlanResponse, error) {
	dryLog(ctx, d.lg, "Plans.Create", "name", req.Name)
	fakeID := fmt.Sprintf("plan_dryrun_%d", time.Now().UnixNano())
	return &dtos.CreatePlanResponse{
		DtoPlanResponse: &types.DtoPlanResponse{ID: strPtrDryRun(fakeID)},
	}, nil
}
func (d *dryRunPlans) Query(ctx context.Context, filter types.PlanFilter) (*dtos.QueryPlanResponse, error) {
	return d.inner.Query(ctx, filter)
}
func (d *dryRunPlans) Get(ctx context.Context, id string) (*dtos.GetPlanResponse, error) {
	return d.inner.Get(ctx, id)
}

// ── Prices ────────────────────────────────────────────────────────────

type dryRunPrices struct {
	inner PriceOps
	lg    *logger.Logger
}

func (d *dryRunPrices) Create(ctx context.Context, req types.DtoCreatePriceRequest) (*dtos.CreatePriceResponse, error) {
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

func (d *dryRunFeatures) Create(ctx context.Context, req types.DtoCreateFeatureRequest) (*dtos.CreateFeatureResponse, error) {
	dryLog(ctx, d.lg, "Features.Create", "lookup_key", req.LookupKey)
	fakeID := fmt.Sprintf("feature_dryrun_%d", time.Now().UnixNano())
	fakeMeterID := fmt.Sprintf("meter_dryrun_%d", time.Now().UnixNano())
	return &dtos.CreateFeatureResponse{
		DtoFeatureResponse: &types.DtoFeatureResponse{
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

func (d *dryRunSubscriptions) Create(ctx context.Context, req types.DtoCreateSubscriptionRequest) (*dtos.CreateSubscriptionResponse, error) {
	extID := ""
	if req.ExternalCustomerID != nil {
		extID = *req.ExternalCustomerID
	}
	dryLog(ctx, d.lg, "Subscriptions.Create", "external_customer_id", extID)
	fakeID := fmt.Sprintf("sub_dryrun_%d", time.Now().UnixNano())
	return &dtos.CreateSubscriptionResponse{
		DtoSubscriptionResponse: &types.DtoSubscriptionResponse{ID: strPtrDryRun(fakeID)},
	}, nil
}
func (d *dryRunSubscriptions) Get(ctx context.Context, id string) (*dtos.GetSubscriptionResponse, error) {
	return d.inner.Get(ctx, id)
}
func (d *dryRunSubscriptions) Cancel(ctx context.Context, id string, _ types.DtoCancelSubscriptionRequest) (*dtos.CancelSubscriptionResponse, error) {
	dryLog(ctx, d.lg, "Subscriptions.Cancel", "id", id)
	return &dtos.CancelSubscriptionResponse{}, nil
}
func (d *dryRunSubscriptions) Query(ctx context.Context, filter types.SubscriptionFilter) (*dtos.QuerySubscriptionResponse, error) {
	return d.inner.Query(ctx, filter)
}
func (d *dryRunSubscriptions) ActivateSubscription(ctx context.Context, id string, _ types.DtoActivateDraftSubscriptionRequest) (*dtos.ActivateSubscriptionResponse, error) {
	dryLog(ctx, d.lg, "Subscriptions.ActivateSubscription", "id", id)
	return &dtos.ActivateSubscriptionResponse{}, nil
}
func (d *dryRunSubscriptions) GetEntitlements(ctx context.Context, id string, featureIDs []string) (*dtos.GetSubscriptionEntitlementsResponse, error) {
	return d.inner.GetEntitlements(ctx, id, featureIDs)
}
func (d *dryRunSubscriptions) GetUsage(ctx context.Context, req types.DtoGetUsageBySubscriptionRequest) (*dtos.GetSubscriptionUsageResponse, error) {
	return d.inner.GetUsage(ctx, req)
}
func (d *dryRunSubscriptions) CreateLineItem(ctx context.Context, id string, _ types.DtoCreateSubscriptionLineItemRequest) (*dtos.CreateSubscriptionLineItemResponse, error) {
	dryLog(ctx, d.lg, "Subscriptions.CreateLineItem", "id", id)
	return &dtos.CreateSubscriptionLineItemResponse{}, nil
}
func (d *dryRunSubscriptions) UpdateLineItem(ctx context.Context, id string, _ types.DtoUpdateSubscriptionLineItemRequest) (*dtos.UpdateSubscriptionLineItemResponse, error) {
	dryLog(ctx, d.lg, "Subscriptions.UpdateLineItem", "id", id)
	return &dtos.UpdateSubscriptionLineItemResponse{}, nil
}

// ── Wallets ───────────────────────────────────────────────────────────

type dryRunWallets struct {
	inner WalletOps
	lg    *logger.Logger
}

func (d *dryRunWallets) Create(ctx context.Context, req types.DtoCreateWalletRequest) (*dtos.CreateWalletResponse, error) {
	extID := ""
	if req.ExternalCustomerID != nil {
		extID = *req.ExternalCustomerID
	}
	dryLog(ctx, d.lg, "Wallets.Create", "external_customer_id", extID)
	fakeID := fmt.Sprintf("wallet_dryrun_%d", time.Now().UnixNano())
	return &dtos.CreateWalletResponse{
		DtoWalletResponse: &types.DtoWalletResponse{ID: strPtrDryRun(fakeID)},
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
func (d *dryRunWallets) TopUp(ctx context.Context, id string, _ types.DtoTopUpWalletRequest) (*dtos.TopUpWalletResponse, error) {
	dryLog(ctx, d.lg, "Wallets.TopUp", "id", id)
	return &dtos.TopUpWalletResponse{}, nil
}

// ── Events ────────────────────────────────────────────────────────────

type dryRunEvents struct {
	inner EventOps
	lg    *logger.Logger
}

func (d *dryRunEvents) Ingest(ctx context.Context, req types.DtoIngestEventRequest) (*dtos.IngestEventResponse, error) {
	dryLog(ctx, d.lg, "Events.Ingest", "event_name", req.EventName)
	return &dtos.IngestEventResponse{}, nil
}
func (d *dryRunEvents) GetUsageAnalytics(ctx context.Context, req types.DtoGetUsageAnalyticsRequest) (*dtos.GetUsageAnalyticsResponse, error) {
	return d.inner.GetUsageAnalytics(ctx, req)
}
func (d *dryRunEvents) ListRaw(ctx context.Context, req types.DtoGetEventsRequest) (*dtos.ListRawEventsResponse, error) {
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
