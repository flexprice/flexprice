package hubspot_test

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/entityintegrationmapping"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/integration/hubspot"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

// fakeHubSpotDealClient is a minimal hubspot.HubSpotClient fake for DealSyncService unit
// tests -- only the methods DealSyncService actually calls are meaningfully implemented.
type fakeHubSpotDealClient struct {
	hubspot.HubSpotClient
	createDealLineItemFn func(ctx context.Context, req *hubspot.DealLineItemCreateRequest) (*hubspot.DealLineItemResponse, error)
	deleteDealLineItemFn func(ctx context.Context, lineItemID string) error
}

func (f *fakeHubSpotDealClient) CreateDealLineItem(ctx context.Context, req *hubspot.DealLineItemCreateRequest) (*hubspot.DealLineItemResponse, error) {
	return f.createDealLineItemFn(ctx, req)
}
func (f *fakeHubSpotDealClient) DeleteDealLineItem(ctx context.Context, lineItemID string) error {
	return f.deleteDealLineItemFn(ctx, lineItemID)
}

func newTestDealSyncService(t *testing.T, client hubspot.HubSpotClient) (*hubspot.DealSyncService, subscription.Repository, *testutil.InMemorySubscriptionLineItemStore, price.Repository, entityintegrationmapping.Repository) {
	t.Helper()
	subRepo := testutil.NewInMemorySubscriptionStore()
	lineItemRepo := testutil.NewInMemorySubscriptionLineItemStore()
	subRepo.SetLineItemStore(lineItemRepo)
	priceRepo := testutil.NewInMemoryPriceStore()
	mappingRepo := testutil.NewInMemoryEntityIntegrationMappingStore()
	customerRepo := testutil.NewInMemoryCustomerStore()

	log, err := logger.NewLogger(&config.Configuration{Logging: config.LoggingConfig{Level: types.LogLevelInfo}})
	require.NoError(t, err)

	svc := hubspot.NewDealSyncService(client, customerRepo, subRepo, priceRepo, mappingRepo, log)
	return svc, subRepo, lineItemRepo, priceRepo, mappingRepo
}

func TestSyncLineItemCreated_SkipsNonFixedLineItem(t *testing.T) {
	ctx := context.Background()
	svc, subRepo, lineItemRepo, _, _ := newTestDealSyncService(t, &fakeHubSpotDealClient{})

	sub := &subscription.Subscription{ID: "sub_1", CustomerID: "cust_1", BillingPeriod: types.BILLING_PERIOD_MONTHLY}
	require.NoError(t, subRepo.Create(ctx, sub))

	usageLineItem := &subscription.SubscriptionLineItem{
		ID: "li_1", SubscriptionID: "sub_1", CustomerID: "cust_1", PriceType: types.PRICE_TYPE_USAGE,
		BaseModel: types.BaseModel{Status: types.StatusPublished},
		StartDate: time.Now().Add(-time.Hour),
	}
	require.NoError(t, lineItemRepo.Create(ctx, usageLineItem))

	err := svc.SyncLineItemCreated(ctx, "sub_1", "li_1", "deal_1")
	require.NoError(t, err, "expected a silent skip for a non-FIXED line item, not an error")
}

func TestSyncLineItemCreated_IsIdempotent(t *testing.T) {
	ctx := context.Background()
	created := 0
	client := &fakeHubSpotDealClient{
		createDealLineItemFn: func(ctx context.Context, req *hubspot.DealLineItemCreateRequest) (*hubspot.DealLineItemResponse, error) {
			created++
			return &hubspot.DealLineItemResponse{ID: "hs_li_999"}, nil
		},
	}
	svc, subRepo, lineItemRepo, priceRepo, mappingRepo := newTestDealSyncService(t, client)

	sub := &subscription.Subscription{ID: "sub_1", CustomerID: "cust_1", BillingPeriod: types.BILLING_PERIOD_MONTHLY}
	require.NoError(t, subRepo.Create(ctx, sub))

	pr := &price.Price{ID: "price_1", Amount: decimal.NewFromInt(10)}
	require.NoError(t, priceRepo.Create(ctx, pr))

	li := &subscription.SubscriptionLineItem{
		ID: "li_1", SubscriptionID: "sub_1", CustomerID: "cust_1", PriceID: "price_1", PriceType: types.PRICE_TYPE_FIXED,
		Quantity: decimal.NewFromInt(2), DisplayName: "Seats",
		BaseModel: types.BaseModel{Status: types.StatusPublished},
		StartDate: time.Now().Add(-time.Hour),
	}
	require.NoError(t, lineItemRepo.Create(ctx, li))

	require.NoError(t, svc.SyncLineItemCreated(ctx, "sub_1", "li_1", "deal_1"))
	require.NoError(t, svc.SyncLineItemCreated(ctx, "sub_1", "li_1", "deal_1"))
	require.Equal(t, 1, created, "expected exactly 1 HubSpot create call across two invocations")

	filter := types.NewNoLimitEntityIntegrationMappingFilter()
	filter.EntityID = "li_1"
	filter.EntityType = types.IntegrationEntityTypeSubscriptionLineItem
	mappings, err := mappingRepo.List(ctx, filter)
	require.NoError(t, err)
	require.Len(t, mappings, 1)
	require.Equal(t, "hs_li_999", mappings[0].ProviderEntityID)
}

func TestSyncLineItemDeleted_NoMappingIsNoOp(t *testing.T) {
	ctx := context.Background()
	svc, _, _, _, _ := newTestDealSyncService(t, &fakeHubSpotDealClient{})
	require.NoError(t, svc.SyncLineItemDeleted(ctx, "li_never_synced"))
}

func TestSyncLineItemDeleted_SelfHealsOn404(t *testing.T) {
	ctx := context.Background()
	deleteCalled := false
	client := &fakeHubSpotDealClient{
		deleteDealLineItemFn: func(ctx context.Context, lineItemID string) error {
			deleteCalled = true
			return ierr.NewError("not found").Mark(ierr.ErrNotFound)
		},
	}
	svc, _, _, _, mappingRepo := newTestDealSyncService(t, client)

	mapping := &entityintegrationmapping.EntityIntegrationMapping{
		ID:               types.GenerateUUIDWithPrefix(types.UUID_PREFIX_ENTITY_INTEGRATION_MAPPING),
		EntityID:         "li_1",
		EntityType:       types.IntegrationEntityTypeSubscriptionLineItem,
		ProviderType:     string(types.SecretProviderHubSpot),
		ProviderEntityID: "hs_li_stale",
		BaseModel:        types.BaseModel{Status: types.StatusPublished},
	}
	require.NoError(t, mappingRepo.Create(ctx, mapping))

	require.NoError(t, svc.SyncLineItemDeleted(ctx, "li_1"))
	require.True(t, deleteCalled)

	filter := types.NewNoLimitEntityIntegrationMappingFilter()
	filter.EntityID = "li_1"
	filter.EntityType = types.IntegrationEntityTypeSubscriptionLineItem
	remaining, err := mappingRepo.List(ctx, filter)
	require.NoError(t, err)
	require.Len(t, remaining, 0, "expected the stale mapping row to be deleted")
}
