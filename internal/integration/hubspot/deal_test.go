package hubspot

import (
	"context"
	"testing"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/entityintegrationmapping"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

// fakeHubSpotClient is a minimal HubSpotClient double, local to this test file
// (same import-cycle-avoidance rationale as PR1's client_test.go fakes). Only the
// methods DealSyncService actually calls need real behavior; everything else
// panics if called, which fails the test loudly rather than silently succeeding
// with wrong behavior.
type fakeHubSpotClient struct {
	HubSpotClient
	createDealLineItemFn func(ctx context.Context, req *DealLineItemCreateRequest) (*DealLineItemResponse, error)
	deleteDealLineItemFn func(ctx context.Context, lineItemID string) error
}

func (f *fakeHubSpotClient) CreateDealLineItem(ctx context.Context, req *DealLineItemCreateRequest) (*DealLineItemResponse, error) {
	return f.createDealLineItemFn(ctx, req)
}

func (f *fakeHubSpotClient) DeleteDealLineItem(ctx context.Context, lineItemID string) error {
	return f.deleteDealLineItemFn(ctx, lineItemID)
}

// fakeEIMRepo is a minimal in-memory entityintegrationmapping.Repository double.
type fakeEIMRepo struct {
	byID map[string]*entityintegrationmapping.EntityIntegrationMapping
}

func newFakeEIMRepo() *fakeEIMRepo {
	return &fakeEIMRepo{byID: make(map[string]*entityintegrationmapping.EntityIntegrationMapping)}
}

func (r *fakeEIMRepo) Create(_ context.Context, m *entityintegrationmapping.EntityIntegrationMapping) error {
	r.byID[m.ID] = m
	return nil
}

func (r *fakeEIMRepo) Get(_ context.Context, id string) (*entityintegrationmapping.EntityIntegrationMapping, error) {
	m, ok := r.byID[id]
	if !ok {
		return nil, ierr.NewError("mapping not found").Mark(ierr.ErrNotFound)
	}
	return m, nil
}

func (r *fakeEIMRepo) List(_ context.Context, filter *types.EntityIntegrationMappingFilter) ([]*entityintegrationmapping.EntityIntegrationMapping, error) {
	var out []*entityintegrationmapping.EntityIntegrationMapping
	for _, m := range r.byID {
		if filter.EntityID != "" && m.EntityID != filter.EntityID {
			continue
		}
		if filter.EntityType != "" && m.EntityType != filter.EntityType {
			continue
		}
		if len(filter.ProviderTypes) > 0 {
			match := false
			for _, pt := range filter.ProviderTypes {
				if m.ProviderType == pt {
					match = true
				}
			}
			if !match {
				continue
			}
		}
		if filter.QueryFilter != nil && filter.QueryFilter.GetStatus() != "" && string(m.Status) != filter.QueryFilter.GetStatus() {
			continue
		}
		out = append(out, m)
	}
	return out, nil
}

func (r *fakeEIMRepo) Count(ctx context.Context, filter *types.EntityIntegrationMappingFilter) (int, error) {
	items, err := r.List(ctx, filter)
	return len(items), err
}

func (r *fakeEIMRepo) Update(_ context.Context, m *entityintegrationmapping.EntityIntegrationMapping) error {
	r.byID[m.ID] = m
	return nil
}

func (r *fakeEIMRepo) Delete(_ context.Context, m *entityintegrationmapping.EntityIntegrationMapping) error {
	delete(r.byID, m.ID)
	return nil
}

// fakePriceRepo is a minimal price.Repository double exposing only Get.
type fakePriceRepo struct {
	price.Repository
	byID map[string]*price.Price
}

func (r *fakePriceRepo) Get(_ context.Context, id string) (*price.Price, error) {
	p, ok := r.byID[id]
	if !ok {
		return nil, ierr.NewError("price not found").Mark(ierr.ErrNotFound)
	}
	return p, nil
}

func testDealCtx() context.Context {
	ctx := context.Background()
	ctx = types.SetTenantID(ctx, "tenant_test")
	ctx = types.SetEnvironmentID(ctx, "env_test")
	return ctx
}

// fakeSubscriptionRepo is a minimal subscription.Repository double exposing only
// GetWithLineItems, which is all SyncLineItemCreated needs.
type fakeSubscriptionRepo struct {
	subscription.Repository
	sub       *subscription.Subscription
	lineItems []*subscription.SubscriptionLineItem
}

func (r *fakeSubscriptionRepo) GetWithLineItems(_ context.Context, id string) (*subscription.Subscription, []*subscription.SubscriptionLineItem, error) {
	if r.sub == nil || r.sub.ID != id {
		return nil, nil, ierr.NewError("subscription not found").Mark(ierr.ErrNotFound)
	}
	return r.sub, r.lineItems, nil
}

func TestSyncLineItemCreated_Success(t *testing.T) {
	priceRepo := &fakePriceRepo{byID: map[string]*price.Price{
		"price_1": {ID: "price_1", Amount: decimal.NewFromInt(100)},
	}}
	lineItem := &subscription.SubscriptionLineItem{
		ID:          "li_1",
		PriceID:     "price_1",
		Quantity:    decimal.NewFromInt(2),
		PriceType:   types.PRICE_TYPE_FIXED,
		DisplayName: "Seats",
	}
	subRepo := &fakeSubscriptionRepo{
		sub:       &subscription.Subscription{ID: "sub_1", BillingPeriod: types.BILLING_PERIOD_MONTHLY},
		lineItems: []*subscription.SubscriptionLineItem{lineItem},
	}
	eimRepo := newFakeEIMRepo()
	client := &fakeHubSpotClient{
		createDealLineItemFn: func(ctx context.Context, req *DealLineItemCreateRequest) (*DealLineItemResponse, error) {
			return &DealLineItemResponse{ID: "hs_li_123"}, nil
		},
	}
	svc := NewDealSyncService(client, nil, subRepo, priceRepo, eimRepo, testLoggerForDealTest(t))

	err := svc.SyncLineItemCreated(testDealCtx(), "sub_1", "li_1", "deal_1")
	require.NoError(t, err)

	mappings, err := eimRepo.List(testDealCtx(), &types.EntityIntegrationMappingFilter{
		QueryFilter: types.NewNoLimitQueryFilter(),
		EntityID:    "li_1",
		EntityType:  types.IntegrationEntityTypeSubscriptionLineItem,
	})
	require.NoError(t, err)
	require.Len(t, mappings, 1)
	require.Equal(t, "hs_li_123", mappings[0].ProviderEntityID)
}

func TestSyncLineItemCreated_AlreadySynced_NoOp(t *testing.T) {
	eimRepo := newFakeEIMRepo()
	existing := &entityintegrationmapping.EntityIntegrationMapping{
		ID: "eim_1", EntityID: "li_1", EntityType: types.IntegrationEntityTypeSubscriptionLineItem,
		ProviderType: string(types.SecretProviderHubSpot), ProviderEntityID: "hs_li_existing",
		BaseModel: types.BaseModel{Status: types.StatusPublished},
	}
	require.NoError(t, eimRepo.Create(testDealCtx(), existing))

	createCalled := false
	client := &fakeHubSpotClient{
		createDealLineItemFn: func(ctx context.Context, req *DealLineItemCreateRequest) (*DealLineItemResponse, error) {
			createCalled = true
			return &DealLineItemResponse{ID: "hs_li_new"}, nil
		},
	}
	// subRepo is deliberately nil-fielded/unused here: the already-synced check must
	// short-circuit before any subscription fetch is attempted.
	svc := NewDealSyncService(client, nil, nil, nil, eimRepo, testLoggerForDealTest(t))

	err := svc.SyncLineItemCreated(testDealCtx(), "sub_1", "li_1", "deal_1")
	require.NoError(t, err)
	require.False(t, createCalled, "expected no HubSpot API call for an already-synced line item")
}

func TestSyncLineItemDeleted_Success(t *testing.T) {
	eimRepo := newFakeEIMRepo()
	existing := &entityintegrationmapping.EntityIntegrationMapping{
		ID: "eim_1", EntityID: "li_1", EntityType: types.IntegrationEntityTypeSubscriptionLineItem,
		ProviderType: string(types.SecretProviderHubSpot), ProviderEntityID: "hs_li_1",
		BaseModel: types.BaseModel{Status: types.StatusPublished},
	}
	require.NoError(t, eimRepo.Create(testDealCtx(), existing))

	deletedID := ""
	client := &fakeHubSpotClient{
		deleteDealLineItemFn: func(ctx context.Context, lineItemID string) error {
			deletedID = lineItemID
			return nil
		},
	}
	svc := NewDealSyncService(client, nil, nil, nil, eimRepo, testLoggerForDealTest(t))

	err := svc.SyncLineItemDeleted(testDealCtx(), "li_1")
	require.NoError(t, err)
	require.Equal(t, "hs_li_1", deletedID)

	_, err = eimRepo.Get(testDealCtx(), "eim_1")
	require.Error(t, err, "expected mapping to be deleted")
}

func TestSyncLineItemDeleted_NotFoundInHubSpot_SelfHeals(t *testing.T) {
	eimRepo := newFakeEIMRepo()
	existing := &entityintegrationmapping.EntityIntegrationMapping{
		ID: "eim_1", EntityID: "li_1", EntityType: types.IntegrationEntityTypeSubscriptionLineItem,
		ProviderType: string(types.SecretProviderHubSpot), ProviderEntityID: "hs_li_gone",
		BaseModel: types.BaseModel{Status: types.StatusPublished},
	}
	require.NoError(t, eimRepo.Create(testDealCtx(), existing))

	client := &fakeHubSpotClient{
		deleteDealLineItemFn: func(ctx context.Context, lineItemID string) error {
			return ierr.NewError("HubSpot line item not found").Mark(ierr.ErrNotFound)
		},
	}
	svc := NewDealSyncService(client, nil, nil, nil, eimRepo, testLoggerForDealTest(t))

	err := svc.SyncLineItemDeleted(testDealCtx(), "li_1")
	require.NoError(t, err, "a 404 from HubSpot must self-heal, not error")

	_, err = eimRepo.Get(testDealCtx(), "eim_1")
	require.Error(t, err, "expected the stale mapping to still be cleaned up")
}

func testLoggerForDealTest(t *testing.T) *logger.Logger {
	t.Helper()
	cfg := &config.Configuration{Logging: config.LoggingConfig{Level: types.LogLevelInfo}}
	log, err := logger.NewLogger(cfg)
	require.NoError(t, err)
	return log
}
