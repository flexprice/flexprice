package hubspot_test

// NOTE: this test lives in the external hubspot_test package (rather than
// hubspot) deliberately: internal/testutil imports internal/integration
// (for the integration factory), which in turn imports
// internal/integration/hubspot, so importing testutil from the internal
// hubspot package itself would create an import cycle. Since
// DealSyncService and everything it needs are exported, the external test
// package works fine here.

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/domain/entityintegrationmapping"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/integration/hubspot"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeHubSpotClient is a minimal fake covering only the methods exercised by
// SyncLineItemCreated. Embedding the nil interface satisfies HubSpotClient for
// every other method, which must never be called in this test.
type fakeHubSpotClient struct {
	hubspot.HubSpotClient

	createDealLineItemResp *hubspot.DealLineItemResponse
	createDealLineItemErr  error

	deleteDealLineItemCalls []string
	deleteDealLineItemErr   error
}

func (f *fakeHubSpotClient) CreateDealLineItem(ctx context.Context, req *hubspot.DealLineItemCreateRequest) (*hubspot.DealLineItemResponse, error) {
	if f.createDealLineItemErr != nil {
		return nil, f.createDealLineItemErr
	}
	return f.createDealLineItemResp, nil
}

func (f *fakeHubSpotClient) DeleteDealLineItem(ctx context.Context, lineItemID string) error {
	f.deleteDealLineItemCalls = append(f.deleteDealLineItemCalls, lineItemID)
	return f.deleteDealLineItemErr
}

// failingCreateMappingRepo wraps the real repository interface, always failing
// on Create so we can exercise the compensating-delete path, while delegating
// List so the "mapping already exists" pre-check finds nothing.
type failingCreateMappingRepo struct {
	entityintegrationmapping.Repository

	createErr error
}

func (f *failingCreateMappingRepo) Create(ctx context.Context, mapping *entityintegrationmapping.EntityIntegrationMapping) error {
	return f.createErr
}

func (f *failingCreateMappingRepo) List(ctx context.Context, filter *types.EntityIntegrationMappingFilter) ([]*entityintegrationmapping.EntityIntegrationMapping, error) {
	return []*entityintegrationmapping.EntityIntegrationMapping{}, nil
}

func TestSyncLineItemCreated_CompensatesOrphanedLineItemOnMappingPersistFailure(t *testing.T) {
	ctx := types.SetTenantID(context.Background(), "test_tenant")
	ctx = types.SetEnvironmentID(ctx, "test_env")

	subscriptionRepo := testutil.NewInMemorySubscriptionStore()
	priceRepo := testutil.NewInMemoryPriceStore()

	priceObj := &price.Price{
		ID:        "price_1",
		Amount:    decimal.NewFromInt(100),
		Currency:  "usd",
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	require.NoError(t, priceRepo.Create(ctx, priceObj))

	now := time.Now().UTC()
	sub := &subscription.Subscription{
		ID:                 "sub_1",
		CustomerID:         "cust_1",
		PlanID:             "plan_1",
		SubscriptionStatus: types.SubscriptionStatusActive,
		Currency:           "usd",
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingCycle:       types.BillingCycleAnniversary,
		StartDate:          now,
		CurrentPeriodStart: now,
		CurrentPeriodEnd:   now.AddDate(0, 1, 0),
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}

	lineItem := &subscription.SubscriptionLineItem{
		ID:             "li_1",
		SubscriptionID: sub.ID,
		CustomerID:     sub.CustomerID,
		PriceID:        priceObj.ID,
		PriceType:      types.PRICE_TYPE_FIXED,
		DisplayName:    "Setup Fee",
		Quantity:       decimal.NewFromInt(1),
		Currency:       "usd",
		BillingPeriod:  types.BILLING_PERIOD_MONTHLY,
		StartDate:      now,
		EndDate:        now.AddDate(0, 1, 0),
		BaseModel:      types.GetDefaultBaseModel(ctx),
	}

	require.NoError(t, subscriptionRepo.CreateWithLineItems(ctx, sub, []*subscription.SubscriptionLineItem{lineItem}))

	client := &fakeHubSpotClient{
		createDealLineItemResp: &hubspot.DealLineItemResponse{ID: "hs_li_1"},
	}

	mappingRepo := &failingCreateMappingRepo{
		createErr: ierr.NewError("db write failed").Mark(ierr.ErrDatabase),
	}

	svc := hubspot.NewDealSyncService(
		client,
		nil, // customerRepo not used by SyncLineItemCreated
		subscriptionRepo,
		priceRepo,
		mappingRepo,
		logger.NewNoopLogger(),
	)

	err := svc.SyncLineItemCreated(ctx, sub.ID, lineItem.ID, "deal_1")

	require.Error(t, err)
	assert.Equal(t, []string{"hs_li_1"}, client.deleteDealLineItemCalls,
		"expected the just-created HubSpot line item to be deleted exactly once as compensation")
}
