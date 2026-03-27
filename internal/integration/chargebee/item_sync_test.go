package chargebee

import (
	"context"
	"testing"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/entityintegrationmapping"
	"github.com/flexprice/flexprice/internal/domain/plan"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

type fakeItemFamilyService struct{}

func (f *fakeItemFamilyService) CreateItemFamily(_ context.Context, _ *ItemFamilyCreateRequest) (*ItemFamilyResponse, error) {
	return nil, nil
}

func (f *fakeItemFamilyService) ListItemFamilies(_ context.Context) ([]*ItemFamilyResponse, error) {
	return []*ItemFamilyResponse{{ID: "family_latest", Name: "Latest Family"}}, nil
}

func (f *fakeItemFamilyService) GetLatestItemFamily(_ context.Context) (*ItemFamilyResponse, error) {
	return &ItemFamilyResponse{ID: "family_latest", Name: "Latest Family"}, nil
}

type fakeItemService struct {
	created []*ItemCreateRequest
}

func (f *fakeItemService) CreateItem(_ context.Context, req *ItemCreateRequest) (*ItemResponse, error) {
	f.created = append(f.created, req)
	return &ItemResponse{ID: req.ID, Name: req.Name, ItemFamilyID: req.ItemFamilyID}, nil
}

func (f *fakeItemService) RetrieveItem(_ context.Context, itemID string) (*ItemResponse, error) {
	return &ItemResponse{ID: itemID}, nil
}

type fakeItemPriceService struct {
	created []*ItemPriceCreateRequest
}

func (f *fakeItemPriceService) CreateItemPrice(_ context.Context, req *ItemPriceCreateRequest) (*ItemPriceResponse, error) {
	f.created = append(f.created, req)
	return &ItemPriceResponse{
		ID:           req.ID,
		ItemID:       req.ItemID,
		PricingModel: req.PricingModel,
		Price:        req.Price,
		CurrencyCode: req.CurrencyCode,
	}, nil
}

func (f *fakeItemPriceService) RetrieveItemPrice(_ context.Context, itemPriceID string) (*ItemPriceResponse, error) {
	return &ItemPriceResponse{ID: itemPriceID}, nil
}

func TestSyncPlanToChargebee_SyncsThreePricesAndCreatesMappings(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	ctx = context.WithValue(ctx, types.CtxTenantID, "tenant_test")
	ctx = context.WithValue(ctx, types.CtxEnvironmentID, "env_test")
	ctx = context.WithValue(ctx, types.CtxUserID, "user_test")

	log := mustTestLogger(t)
	mappingRepo := newMemMappingRepo()
	itemSvc := &fakeItemService{}
	itemPriceSvc := &fakeItemPriceService{}

	svc := &PlanSyncService{
		PlanSyncServiceParams: PlanSyncServiceParams{
			EntityIntegrationMappingRepo: mappingRepo,
			MeterRepo:                    nil,
			FeatureRepo:                  nil,
			Logger:                       log,
		},
		itemFamilyService: &fakeItemFamilyService{},
		itemService:       itemSvc,
		itemPriceService:  itemPriceSvc,
	}

	p := &plan.Plan{
		ID:          "plan_pro",
		Name:        "Pro",
		Description: "Pro plan",
	}

	upTo100 := uint64(100)
	prices := []*price.Price{
		{
			ID:           "price_flat",
			Currency:     "USD",
			BillingModel: types.BILLING_MODEL_FLAT_FEE,
			Amount:       decimal.NewFromInt(10),
			EnvironmentID: "env_test",
			BaseModel:    types.GetDefaultBaseModel(ctx),
		},
		{
			ID:           "price_package",
			Currency:     "USD",
			BillingModel: types.BILLING_MODEL_PACKAGE,
			Amount:       decimal.NewFromInt(20),
			TransformQuantity: price.JSONBTransformQuantity{
				DivideBy: 5,
				Round:    types.ROUND_UP,
			},
			EnvironmentID: "env_test",
			BaseModel:     types.GetDefaultBaseModel(ctx),
		},
		{
			ID:           "price_tiered",
			Currency:     "USD",
			BillingModel: types.BILLING_MODEL_TIERED,
			TierMode:     types.BILLING_TIER_SLAB,
			Tiers: price.JSONBTiers{
				{UpTo: &upTo100, UnitAmount: decimal.NewFromFloat(0.10)},
				{UpTo: nil, UnitAmount: decimal.NewFromFloat(0.05)},
			},
			EnvironmentID: "env_test",
			BaseModel:     types.GetDefaultBaseModel(ctx),
		},
	}

	err := svc.SyncPlanToChargebee(ctx, p, prices)
	require.NoError(t, err)

	require.Len(t, itemSvc.created, 3)
	require.Len(t, itemPriceSvc.created, 3)

	byID := map[string]*ItemPriceCreateRequest{}
	for _, req := range itemPriceSvc.created {
		byID[req.ID] = req
	}

	require.Contains(t, byID, "price_flat")
	require.Equal(t, "flat_fee", byID["price_flat"].PricingModel)
	require.Equal(t, "USD", byID["price_flat"].CurrencyCode)
	require.Equal(t, int64(1000), byID["price_flat"].Price)

	require.Contains(t, byID, "price_package")
	require.Equal(t, "package", byID["price_package"].PricingModel)
	require.NotNil(t, byID["price_package"].Period)
	require.Equal(t, 5, *byID["price_package"].Period)

	require.Contains(t, byID, "price_tiered")
	require.Equal(t, "tiered", byID["price_tiered"].PricingModel)
	require.Len(t, byID["price_tiered"].Tiers, 2)
	require.Equal(t, int64(1), byID["price_tiered"].Tiers[0].StartingUnit)
	require.Equal(t, int64(100), *byID["price_tiered"].Tiers[0].EndingUnit)
	require.Equal(t, int64(101), byID["price_tiered"].Tiers[1].StartingUnit)
	require.Nil(t, byID["price_tiered"].Tiers[1].EndingUnit)

	mappings := mappingRepo.listByProvider(string(types.SecretProviderChargebee))
	require.Len(t, mappings, 3)

	for _, m := range mappings {
		require.Equal(t, types.IntegrationEntityTypeItemPrice, m.EntityType)
		require.NotEmpty(t, m.ProviderEntityID)
		require.Equal(t, "tenant_test", m.TenantID)
		require.Equal(t, "env_test", m.EnvironmentID)
		require.Equal(t, string(types.SecretProviderChargebee), m.ProviderType)
		require.NotEmpty(t, m.Metadata["chargebee_charge_item_id"])
	}
}

func TestSyncPlanToChargebee_ContinuesWhenOneItemPriceFails(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	ctx = context.WithValue(ctx, types.CtxTenantID, "tenant_test")
	ctx = context.WithValue(ctx, types.CtxEnvironmentID, "env_test")
	ctx = context.WithValue(ctx, types.CtxUserID, "user_test")

	log := mustTestLogger(t)
	mappingRepo := newMemMappingRepo()
	itemSvc := &fakeItemService{}
	itemPriceSvc := &fakeItemPriceServiceWithFailure{failPriceID: "price_fail"}

	svc := &PlanSyncService{
		PlanSyncServiceParams: PlanSyncServiceParams{
			EntityIntegrationMappingRepo: mappingRepo,
			MeterRepo:                    nil,
			FeatureRepo:                  nil,
			Logger:                       log,
		},
		itemFamilyService: &fakeItemFamilyService{},
		itemService:       itemSvc,
		itemPriceService:  itemPriceSvc,
	}

	p := &plan.Plan{ID: "plan_pro", Name: "Pro"}
	prices := []*price.Price{
		{ID: "price_ok_1", Currency: "USD", BillingModel: types.BILLING_MODEL_FLAT_FEE, Amount: decimal.NewFromInt(10), EnvironmentID: "env_test", BaseModel: types.GetDefaultBaseModel(ctx)},
		{ID: "price_fail", Currency: "USD", BillingModel: types.BILLING_MODEL_FLAT_FEE, Amount: decimal.NewFromInt(20), EnvironmentID: "env_test", BaseModel: types.GetDefaultBaseModel(ctx)},
		{ID: "price_ok_2", Currency: "USD", BillingModel: types.BILLING_MODEL_FLAT_FEE, Amount: decimal.NewFromInt(30), EnvironmentID: "env_test", BaseModel: types.GetDefaultBaseModel(ctx)},
	}

	err := svc.SyncPlanToChargebee(ctx, p, prices)
	require.NoError(t, err)
	require.Len(t, itemSvc.created, 3)
	require.Len(t, itemPriceSvc.created, 3)

	mappings := mappingRepo.listByProvider(string(types.SecretProviderChargebee))
	require.Len(t, mappings, 2)

	seen := map[string]bool{}
	for _, m := range mappings {
		seen[m.EntityID] = true
	}
	require.True(t, seen["price_ok_1"])
	require.True(t, seen["price_ok_2"])
	require.False(t, seen["price_fail"])
}

type fakeItemPriceServiceWithFailure struct {
	created     []*ItemPriceCreateRequest
	failPriceID string
}

func (f *fakeItemPriceServiceWithFailure) CreateItemPrice(_ context.Context, req *ItemPriceCreateRequest) (*ItemPriceResponse, error) {
	f.created = append(f.created, req)
	if req.ID == f.failPriceID {
		return nil, context.DeadlineExceeded
	}
	return &ItemPriceResponse{ID: req.ID, ItemID: req.ItemID, CurrencyCode: req.CurrencyCode}, nil
}

func (f *fakeItemPriceServiceWithFailure) RetrieveItemPrice(_ context.Context, itemPriceID string) (*ItemPriceResponse, error) {
	return &ItemPriceResponse{ID: itemPriceID}, nil
}

var _ ChargebeeItemFamilyService = (*fakeItemFamilyService)(nil)
var _ ChargebeeItemService = (*fakeItemService)(nil)
var _ ChargebeeItemPriceService = (*fakeItemPriceService)(nil)
var _ ChargebeeItemPriceService = (*fakeItemPriceServiceWithFailure)(nil)
var _ entityintegrationmapping.Repository = (*memMappingRepo)(nil)

type memMappingRepo struct {
	byID map[string]*entityintegrationmapping.EntityIntegrationMapping
}

func newMemMappingRepo() *memMappingRepo {
	return &memMappingRepo{byID: map[string]*entityintegrationmapping.EntityIntegrationMapping{}}
}

func (m *memMappingRepo) Create(_ context.Context, mapping *entityintegrationmapping.EntityIntegrationMapping) error {
	m.byID[mapping.ID] = mapping
	return nil
}

func (m *memMappingRepo) Get(_ context.Context, id string) (*entityintegrationmapping.EntityIntegrationMapping, error) {
	return m.byID[id], nil
}

func (m *memMappingRepo) List(_ context.Context, _ *types.EntityIntegrationMappingFilter) ([]*entityintegrationmapping.EntityIntegrationMapping, error) {
	out := make([]*entityintegrationmapping.EntityIntegrationMapping, 0, len(m.byID))
	for _, v := range m.byID {
		out = append(out, v)
	}
	return out, nil
}

func (m *memMappingRepo) Count(_ context.Context, _ *types.EntityIntegrationMappingFilter) (int, error) {
	return len(m.byID), nil
}

func (m *memMappingRepo) Update(_ context.Context, mapping *entityintegrationmapping.EntityIntegrationMapping) error {
	m.byID[mapping.ID] = mapping
	return nil
}

func (m *memMappingRepo) Delete(_ context.Context, mapping *entityintegrationmapping.EntityIntegrationMapping) error {
	delete(m.byID, mapping.ID)
	return nil
}

func (m *memMappingRepo) listByProvider(provider string) []*entityintegrationmapping.EntityIntegrationMapping {
	out := make([]*entityintegrationmapping.EntityIntegrationMapping, 0)
	for _, v := range m.byID {
		if v.ProviderType == provider {
			out = append(out, v)
		}
	}
	return out
}

func mustTestLogger(t *testing.T) *logger.Logger {
	t.Helper()
	cfg := &config.Configuration{
		Logging: config.LoggingConfig{Level: types.LogLevelInfo},
	}
	log, err := logger.NewLogger(cfg)
	require.NoError(t, err)
	return log
}
