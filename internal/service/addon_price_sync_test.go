package service

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/domain/addon"
	"github.com/flexprice/flexprice/internal/domain/addonpricesync"
	domainPrice "github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/suite"
)

// ---------------------------------------------------------------------------
// Local mock: addonpricesync.Repository
// ---------------------------------------------------------------------------

// mockAddonPriceSyncRepo is a controllable in-process stub.
type mockAddonPriceSyncRepo struct {
	// TerminateExpiredAddonPricesLineItems
	terminateCalls  int
	terminateReturn []int // successive return values; last one is repeated
	terminateErr    error

	// ListAddonLineItemsToCreate
	listCalls  int
	listReturn [][]addonpricesync.AddonLineItemCreationDelta
	listErr    error

	// GetLastSubscriptionIDInBatch
	getCalls  int
	getReturn []*string
	getErr    error
}

func (m *mockAddonPriceSyncRepo) TerminateExpiredAddonPricesLineItems(
	_ context.Context,
	_ addonpricesync.TerminateExpiredAddonPricesLineItemsParams,
) (int, error) {
	if m.terminateErr != nil {
		return 0, m.terminateErr
	}
	idx := m.terminateCalls
	m.terminateCalls++
	if idx >= len(m.terminateReturn) {
		idx = len(m.terminateReturn) - 1
	}
	return m.terminateReturn[idx], nil
}

func (m *mockAddonPriceSyncRepo) ListAddonLineItemsToTerminate(
	_ context.Context,
	_ addonpricesync.ListAddonLineItemsToTerminateParams,
) ([]addonpricesync.AddonLineItemTerminationDelta, error) {
	return nil, nil
}

func (m *mockAddonPriceSyncRepo) ListAddonLineItemsToCreate(
	_ context.Context,
	_ addonpricesync.ListAddonLineItemsToCreateParams,
) ([]addonpricesync.AddonLineItemCreationDelta, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	idx := m.listCalls
	m.listCalls++
	if idx >= len(m.listReturn) {
		idx = len(m.listReturn) - 1
	}
	return m.listReturn[idx], nil
}

func (m *mockAddonPriceSyncRepo) GetLastSubscriptionIDInBatch(
	_ context.Context,
	_ addonpricesync.ListAddonLineItemsToCreateParams,
) (*string, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	idx := m.getCalls
	m.getCalls++
	if idx >= len(m.getReturn) {
		idx = len(m.getReturn) - 1
	}
	return m.getReturn[idx], nil
}

// ---------------------------------------------------------------------------
// Local mock: addon.Repository (minimal — only GetByID is exercised by SyncAddonPrices)
// ---------------------------------------------------------------------------

type mockAddonRepo struct {
	addon *addon.Addon
	err   error
}

func (m *mockAddonRepo) Create(_ context.Context, _ *addon.Addon) error { return nil }
func (m *mockAddonRepo) GetByID(_ context.Context, _ string) (*addon.Addon, error) {
	return m.addon, m.err
}
func (m *mockAddonRepo) GetByLookupKey(_ context.Context, _ string) (*addon.Addon, error) {
	return m.addon, m.err
}
func (m *mockAddonRepo) Update(_ context.Context, _ *addon.Addon) error  { return nil }
func (m *mockAddonRepo) Delete(_ context.Context, _ string) error        { return nil }
func (m *mockAddonRepo) List(_ context.Context, _ *types.AddonFilter) ([]*addon.Addon, error) {
	return nil, nil
}
func (m *mockAddonRepo) Count(_ context.Context, _ *types.AddonFilter) (int, error) {
	return 0, nil
}

// ---------------------------------------------------------------------------
// Test suite
// ---------------------------------------------------------------------------

type AddonPriceSyncSuite struct {
	testutil.BaseServiceTestSuite
	service AddonService

	// repos we swap per test
	addonPriceSyncRepo *mockAddonPriceSyncRepo
	addonRepo          *mockAddonRepo
}

func TestSyncAddonPrices(t *testing.T) {
	suite.Run(t, new(AddonPriceSyncSuite))
}

// newService (re)creates the service with the current mock repos.
func (s *AddonPriceSyncSuite) newService() AddonService {
	params := ServiceParams{
		Logger:                   s.GetLogger(),
		Config:                   s.GetConfig(),
		DB:                       s.GetDB(),
		AddonRepo:                s.addonRepo,
		AddonPriceSyncRepo:       s.addonPriceSyncRepo,
		PriceRepo:                s.GetStores().PriceRepo,
		SubRepo:                  s.GetStores().SubscriptionRepo,
		SubscriptionLineItemRepo: s.GetStores().SubscriptionLineItemRepo,
		// Other repos that sub-services may touch:
		MeterRepo:            s.GetStores().MeterRepo,
		CustomerRepo:         s.GetStores().CustomerRepo,
		EntitlementRepo:      s.GetStores().EntitlementRepo,
		EnvironmentRepo:      s.GetStores().EnvironmentRepo,
		FeatureRepo:          s.GetStores().FeatureRepo,
		AddonAssociationRepo: s.GetStores().AddonAssociationRepo,
		EventPublisher:       s.GetPublisher(),
		WebhookPublisher:     s.GetWebhookPublisher(),
		IntegrationFactory:   s.GetIntegrationFactory(),
		ConnectionRepo:       s.GetStores().ConnectionRepo,
	}
	return NewAddonService(params)
}

// validAddon returns a minimal, valid Addon domain object.
func validAddon(addonID string) *addon.Addon {
	return &addon.Addon{
		ID:   addonID,
		Name: "Test Addon",
		Type: types.AddonTypeOnetime,
		BaseModel: types.BaseModel{
			TenantID:  types.DefaultTenantID,
			Status:    types.StatusPublished,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		},
	}
}

// validPrice returns a minimal addon Price.
func validAddonPrice(priceID, addonID string) *domainPrice.Price {
	return &domainPrice.Price{
		ID:                 priceID,
		Amount:             decimal.NewFromInt(10),
		Currency:           "usd",
		EntityType:         types.PRICE_ENTITY_TYPE_ADDON,
		EntityID:           addonID,
		Type:               types.PRICE_TYPE_FIXED,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_FLAT_FEE,
		BaseModel:          types.GetDefaultBaseModel(s_ctx()),
	}
}

// validSub returns a minimal active Subscription.
func validSub(subID string) *subscription.Subscription {
	return &subscription.Subscription{
		ID:                 subID,
		PlanID:             "plan-1",
		CustomerID:         "cust-1",
		SubscriptionStatus: types.SubscriptionStatusActive,
		Currency:           "usd",
		StartDate:          time.Now().UTC().AddDate(0, 0, -30),
		BaseModel:          types.GetDefaultBaseModel(s_ctx()),
	}
}

// s_ctx returns a minimal background context with tenant/user IDs.
func s_ctx() context.Context {
	ctx := context.Background()
	ctx = context.WithValue(ctx, types.CtxTenantID, types.DefaultTenantID)
	ctx = context.WithValue(ctx, types.CtxUserID, types.DefaultUserID)
	return ctx
}

// ---------------------------------------------------------------------------
// TC-1: BasicCreation — new addon price → line items created on active subs
// ---------------------------------------------------------------------------

func (s *AddonPriceSyncSuite) TestSyncAddonPrices_BasicCreation() {
	const addonID = "addon-basic"
	const priceID = "price-basic"
	const subID = "sub-basic"

	// Addon exists
	s.addonRepo = &mockAddonRepo{addon: validAddon(addonID)}

	// No expired line items to terminate
	// ListAddonLineItemsToCreate returns 1 delta on first call, 0 on second
	delta := addonpricesync.AddonLineItemCreationDelta{
		SubscriptionID: subID,
		PriceID:        priceID,
		CustomerID:     "cust-1",
	}
	s.addonPriceSyncRepo = &mockAddonPriceSyncRepo{
		terminateReturn: []int{0},
		listReturn: [][]addonpricesync.AddonLineItemCreationDelta{
			{delta},
			{}, // second call — cursor exhausted
		},
		getReturn: []*string{nil}, // single batch
	}

	// Seed PriceRepo and SubRepo so the service can hydrate the delta
	ctx := s.GetContext()
	p := validAddonPrice(priceID, addonID)
	err := s.GetStores().PriceRepo.Create(ctx, p)
	s.Require().NoError(err)

	sub := validSub(subID)
	err = s.GetStores().SubscriptionRepo.Create(ctx, sub)
	s.Require().NoError(err)

	s.service = s.newService()
	resp, err := s.service.SyncAddonPrices(ctx, addonID)

	s.Require().NoError(err)
	s.Require().NotNil(resp)
	s.Equal(addonID, resp.AddonID)
	s.Equal(1, resp.Summary.LineItemsFoundForCreation)
	s.Equal(1, resp.Summary.LineItemsCreated)
	s.Equal(0, resp.Summary.LineItemsTerminated)

	// Verify line item actually landed in the repo
	lineItems, liErr := s.GetStores().SubscriptionLineItemRepo.List(ctx, &types.SubscriptionLineItemFilter{
		SubscriptionIDs: []string{subID},
		QueryFilter:     types.NewNoLimitQueryFilter(),
	})
	s.Require().NoError(liErr)
	s.Require().Len(lineItems, 1)
	s.Equal(priceID, lineItems[0].PriceID)
}

// ---------------------------------------------------------------------------
// TC-2: Termination — expired addon price end_date → line items terminated
// ---------------------------------------------------------------------------

func (s *AddonPriceSyncSuite) TestSyncAddonPrices_Termination() {
	const addonID = "addon-term"

	s.addonRepo = &mockAddonRepo{addon: validAddon(addonID)}

	// Terminate: 3 items on first call, 0 on second (batch done)
	s.addonPriceSyncRepo = &mockAddonPriceSyncRepo{
		terminateReturn: []int{3, 0},
		listReturn:      [][]addonpricesync.AddonLineItemCreationDelta{{}},
		getReturn:       []*string{nil},
	}

	s.service = s.newService()
	resp, err := s.service.SyncAddonPrices(s.GetContext(), addonID)

	s.Require().NoError(err)
	s.Require().NotNil(resp)
	s.Equal(3, resp.Summary.LineItemsTerminated)
	s.Equal(0, resp.Summary.LineItemsCreated)
	s.Equal(0, resp.Summary.LineItemsFoundForCreation)
}

// ---------------------------------------------------------------------------
// TC-3: IdempotencyNoDuplicates — re-run → no duplicate line items
// ---------------------------------------------------------------------------

func (s *AddonPriceSyncSuite) TestSyncAddonPrices_IdempotencyNoDuplicates() {
	const addonID = "addon-idem"
	const priceID = "price-idem"
	const subID = "sub-idem"

	s.addonRepo = &mockAddonRepo{addon: validAddon(addonID)}

	// First run: creates 1 line item
	delta := addonpricesync.AddonLineItemCreationDelta{
		SubscriptionID: subID,
		PriceID:        priceID,
		CustomerID:     "cust-1",
	}
	s.addonPriceSyncRepo = &mockAddonPriceSyncRepo{
		terminateReturn: []int{0},
		listReturn: [][]addonpricesync.AddonLineItemCreationDelta{
			{delta},
			{},
		},
		getReturn: []*string{nil},
	}

	ctx := s.GetContext()
	err := s.GetStores().PriceRepo.Create(ctx, validAddonPrice(priceID, addonID))
	s.Require().NoError(err)
	err = s.GetStores().SubscriptionRepo.Create(ctx, validSub(subID))
	s.Require().NoError(err)

	s.service = s.newService()
	resp1, err := s.service.SyncAddonPrices(ctx, addonID)
	s.Require().NoError(err)
	s.Equal(1, resp1.Summary.LineItemsCreated)

	// Second run: ListAddonLineItemsToCreate returns empty (DB already has line item)
	s.addonPriceSyncRepo = &mockAddonPriceSyncRepo{
		terminateReturn: []int{0},
		listReturn:      [][]addonpricesync.AddonLineItemCreationDelta{{}},
		getReturn:       []*string{nil},
	}
	s.service = s.newService()
	resp2, err := s.service.SyncAddonPrices(ctx, addonID)
	s.Require().NoError(err)
	s.Equal(0, resp2.Summary.LineItemsCreated, "second run must not create duplicates")
	s.Equal(0, resp2.Summary.LineItemsFoundForCreation)

	// The in-memory line item repo should still have exactly 1 item
	lineItems, liErr := s.GetStores().SubscriptionLineItemRepo.List(ctx, &types.SubscriptionLineItemFilter{
		SubscriptionIDs: []string{subID},
		QueryFilter:     types.NewNoLimitQueryFilter(),
	})
	s.Require().NoError(liErr)
	s.Len(lineItems, 1)
}

// ---------------------------------------------------------------------------
// TC-4: NoSubscriptions — no active associations → no-op, returns zeros
// ---------------------------------------------------------------------------

func (s *AddonPriceSyncSuite) TestSyncAddonPrices_NoSubscriptions() {
	const addonID = "addon-nosubs"

	s.addonRepo = &mockAddonRepo{addon: validAddon(addonID)}

	// Everything empty
	s.addonPriceSyncRepo = &mockAddonPriceSyncRepo{
		terminateReturn: []int{0},
		listReturn:      [][]addonpricesync.AddonLineItemCreationDelta{{}},
		getReturn:       []*string{nil},
	}

	s.service = s.newService()
	resp, err := s.service.SyncAddonPrices(s.GetContext(), addonID)

	s.Require().NoError(err)
	s.Require().NotNil(resp)
	s.Equal(addonID, resp.AddonID)
	s.Equal(0, resp.Summary.LineItemsCreated)
	s.Equal(0, resp.Summary.LineItemsTerminated)
	s.Equal(0, resp.Summary.LineItemsFoundForCreation)
}

// ---------------------------------------------------------------------------
// TC-5: AddonNotFound — GetByID returns error → SyncAddonPrices propagates it
// ---------------------------------------------------------------------------

func (s *AddonPriceSyncSuite) TestSyncAddonPrices_AddonNotFound() {
	const addonID = "addon-missing"

	s.addonRepo = &mockAddonRepo{
		addon: nil,
		err:   ierr.NewError("addon not found").Mark(ierr.ErrNotFound),
	}
	s.addonPriceSyncRepo = &mockAddonPriceSyncRepo{
		terminateReturn: []int{0},
		listReturn:      [][]addonpricesync.AddonLineItemCreationDelta{{}},
		getReturn:       []*string{nil},
	}

	s.service = s.newService()
	resp, err := s.service.SyncAddonPrices(s.GetContext(), addonID)

	s.Error(err)
	s.Nil(resp)
}
