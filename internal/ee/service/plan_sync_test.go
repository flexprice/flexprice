package service

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/plan"
	"github.com/flexprice/flexprice/internal/domain/planpricesync"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/suite"
)

// failingPlanPriceSyncRepo wraps a real planpricesync.Repository and fails a
// single named method, to exercise error propagation in the sync loops.
type failingPlanPriceSyncRepo struct {
	planpricesync.Repository
	failOn string
}

func (r *failingPlanPriceSyncRepo) fail(method string) error {
	return ierr.NewError("injected failure in " + method).Mark(ierr.ErrDatabase)
}

func (r *failingPlanPriceSyncRepo) TerminateExpiredPlanPricesLineItems(ctx context.Context, p planpricesync.TerminateExpiredPlanPricesLineItemsParams) (int, error) {
	if r.failOn == "TerminateExpiredPlanPricesLineItems" {
		return 0, r.fail(r.failOn)
	}
	return r.Repository.TerminateExpiredPlanPricesLineItems(ctx, p)
}

func (r *failingPlanPriceSyncRepo) ListPlanLineItemsToCreate(ctx context.Context, p planpricesync.ListPlanLineItemsToCreateParams) ([]planpricesync.PlanLineItemCreationDelta, error) {
	if r.failOn == "ListPlanLineItemsToCreate" {
		return nil, r.fail(r.failOn)
	}
	return r.Repository.ListPlanLineItemsToCreate(ctx, p)
}

func (r *failingPlanPriceSyncRepo) GetLastSubscriptionIDInBatch(ctx context.Context, p planpricesync.ListPlanLineItemsToCreateParams) (*string, error) {
	if r.failOn == "GetLastSubscriptionIDInBatch" {
		return nil, r.fail(r.failOn)
	}
	return r.Repository.GetLastSubscriptionIDInBatch(ctx, p)
}

func (r *failingPlanPriceSyncRepo) CurrentPlanSequence(ctx context.Context, planID string) (int64, error) {
	if r.failOn == "CurrentPlanSequence" {
		return 0, r.fail(r.failOn)
	}
	return r.Repository.CurrentPlanSequence(ctx, planID)
}

func (r *failingPlanPriceSyncRepo) ListPlanLineItemsToCreateV2(ctx context.Context, p planpricesync.ListPlanLineItemsToCreateV2Params) ([]planpricesync.PlanLineItemCreationDelta, []string, error) {
	if r.failOn == "ListPlanLineItemsToCreateV2" {
		return nil, nil, r.fail(r.failOn)
	}
	return r.Repository.ListPlanLineItemsToCreateV2(ctx, p)
}

func (r *failingPlanPriceSyncRepo) TerminatePlanPricesLineItemsV2(ctx context.Context, p planpricesync.TerminatePlanPricesLineItemsV2Params) (int, error) {
	if r.failOn == "TerminatePlanPricesLineItemsV2" {
		return 0, r.fail(r.failOn)
	}
	return r.Repository.TerminatePlanPricesLineItemsV2(ctx, p)
}

func (r *failingPlanPriceSyncRepo) StampSubsAsSynced(ctx context.Context, p planpricesync.StampSubsAsSyncedParams) (int, error) {
	if r.failOn == "StampSubsAsSynced" {
		return 0, r.fail(r.failOn)
	}
	return r.Repository.StampSubsAsSynced(ctx, p)
}

// PlanSyncServiceSuite covers SyncPlanPrices (v1) and SyncPlanPricesV2,
// which synchronize plan usage prices to subscription line items.
type PlanSyncServiceSuite struct {
	testutil.BaseServiceTestSuite
	service  PlanService
	testData struct {
		plan     *plan.Plan
		customer *customer.Customer
		now      time.Time
	}
}

func TestPlanSyncService(t *testing.T) {
	suite.Run(t, new(PlanSyncServiceSuite))
}

func (s *PlanSyncServiceSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	s.service = NewPlanService(newTestServiceParams(&s.BaseServiceTestSuite))
	s.setupTestData()
}

func (s *PlanSyncServiceSuite) setupTestData() {
	s.testData.now = time.Now().UTC()

	s.testData.plan = &plan.Plan{
		ID:        "plan_sync",
		Name:      "Sync Plan",
		LookupKey: "plan-sync",
		BaseModel: types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().PlanRepo.Create(s.GetContext(), s.testData.plan))

	s.testData.customer = &customer.Customer{
		ID:         "cust_sync",
		ExternalID: "ext_cust_sync",
		Name:       "Sync Customer",
		BaseModel:  types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().CustomerRepo.Create(s.GetContext(), s.testData.customer))
}

// createUsagePlanPrice creates a published USAGE plan price with the given
// sequence and optional end date.
func (s *PlanSyncServiceSuite) createUsagePlanPrice(id, currency string, seq int64, endDate *time.Time) *price.Price {
	startDate := s.testData.now.Add(-60 * 24 * time.Hour)
	p := &price.Price{
		ID:                 id,
		Amount:             decimal.NewFromFloat(0.05),
		Currency:           currency,
		EntityType:         types.PRICE_ENTITY_TYPE_PLAN,
		EntityID:           s.testData.plan.ID,
		Type:               types.PRICE_TYPE_USAGE,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_FLAT_FEE,
		BillingCadence:     types.BILLING_CADENCE_RECURRING,
		InvoiceCadence:     types.InvoiceCadenceArrear,
		MeterID:            "meter_sync",
		DisplayName:        "Sync Usage " + id,
		Metadata:           price.JSONBMetadata{"seeded_by": "fixture"},
		Sequence:           seq,
		StartDate:          &startDate,
		EndDate:            endDate,
		BaseModel:          types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().PriceRepo.Create(s.GetContext(), p))
	return p
}

// createSubscription creates a published active standalone subscription on the test plan.
func (s *PlanSyncServiceSuite) createSubscription(id, currency string) *subscription.Subscription {
	sub := &subscription.Subscription{
		ID:                 id,
		PlanID:             s.testData.plan.ID,
		CustomerID:         s.testData.customer.ID,
		Currency:           currency,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		SubscriptionStatus: types.SubscriptionStatusActive,
		SubscriptionType:   types.SubscriptionTypeStandalone,
		StartDate:          s.testData.now.Add(-30 * 24 * time.Hour),
		CurrentPeriodStart: s.testData.now.Add(-24 * time.Hour),
		CurrentPeriodEnd:   s.testData.now.Add(29 * 24 * time.Hour),
		BillingAnchor:      s.testData.now.Add(-24 * time.Hour),
		BaseModel:          types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().SubscriptionRepo.Create(s.GetContext(), sub))
	return sub
}

// lineItemsForSub returns published plan-derived line items for a subscription.
func (s *PlanSyncServiceSuite) lineItemsForSub(subID string) []*subscription.SubscriptionLineItem {
	sub, err := s.GetStores().SubscriptionRepo.Get(s.GetContext(), subID)
	s.NoError(err)
	items, err := s.GetStores().SubscriptionLineItemRepo.ListBySubscription(s.GetContext(), sub)
	s.NoError(err)
	return items
}

func (s *PlanSyncServiceSuite) TestSyncPlanPricesCreatesMissingLineItems() {
	planPrice := s.createUsagePlanPrice("price_sync_1", "usd", 1, nil)
	sub := s.createSubscription("sub_sync_1", "usd")

	resp, err := s.service.SyncPlanPrices(s.GetContext(), s.testData.plan.ID)
	s.NoError(err)
	s.Equal(s.testData.plan.ID, resp.PlanID)
	s.Equal(1, resp.Summary.LineItemsFoundForCreation)
	s.Equal(1, resp.Summary.LineItemsCreated)
	s.Equal(0, resp.Summary.LineItemsTerminated)

	// Read back: line item must exist with plan-sync metadata and correct linkage
	items := s.lineItemsForSub(sub.ID)
	s.Len(items, 1)
	li := items[0]
	s.Equal(planPrice.ID, li.PriceID)
	s.Equal(types.SubscriptionLineItemEntityTypePlan, li.EntityType)
	s.Equal(s.testData.plan.ID, li.EntityID)
	s.Equal(s.testData.plan.Name, li.PlanDisplayName)
	s.Equal(sub.CustomerID, li.CustomerID)
	s.Equal(types.PRICE_TYPE_USAGE, li.PriceType)
	s.True(li.Quantity.Equal(decimal.Zero))
	s.Equal("plan_sync_api", li.Metadata["added_by"])
	// Price metadata is merged into the line item metadata
	s.Equal("fixture", li.Metadata["seeded_by"])
	s.Equal(planPrice.DisplayName, li.DisplayName)
}

func (s *PlanSyncServiceSuite) TestSyncPlanPricesIsIdempotent() {
	s.createUsagePlanPrice("price_sync_idem", "usd", 1, nil)
	sub := s.createSubscription("sub_sync_idem", "usd")

	first, err := s.service.SyncPlanPrices(s.GetContext(), s.testData.plan.ID)
	s.NoError(err)
	s.Equal(1, first.Summary.LineItemsCreated)

	// Second run must not create duplicates
	second, err := s.service.SyncPlanPrices(s.GetContext(), s.testData.plan.ID)
	s.NoError(err)
	s.Equal(0, second.Summary.LineItemsCreated)
	s.Equal(0, second.Summary.LineItemsTerminated)

	s.Len(s.lineItemsForSub(sub.ID), 1)
}

func (s *PlanSyncServiceSuite) TestSyncPlanPricesSkipsIncompatibleAndOverriddenSubs() {
	planPrice := s.createUsagePlanPrice("price_sync_skip", "usd", 1, nil)

	// Currency-mismatched subscription must be skipped entirely
	eurSub := s.createSubscription("sub_sync_eur", "eur")

	// Overridden subscription: has a subscription-scoped price whose
	// parent_price_id points at the plan price
	overriddenSub := s.createSubscription("sub_sync_override", "usd")
	overridePrice := &price.Price{
		ID:                 "price_sync_override",
		Amount:             decimal.NewFromFloat(0.01),
		Currency:           "usd",
		EntityType:         types.PRICE_ENTITY_TYPE_SUBSCRIPTION,
		EntityID:           overriddenSub.ID,
		ParentPriceID:      planPrice.ID,
		Type:               types.PRICE_TYPE_USAGE,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_FLAT_FEE,
		BillingCadence:     types.BILLING_CADENCE_RECURRING,
		InvoiceCadence:     types.InvoiceCadenceArrear,
		MeterID:            "meter_sync",
		BaseModel:          types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().PriceRepo.Create(s.GetContext(), overridePrice))

	resp, err := s.service.SyncPlanPrices(s.GetContext(), s.testData.plan.ID)
	s.NoError(err)
	s.Equal(0, resp.Summary.LineItemsCreated)

	s.Len(s.lineItemsForSub(eurSub.ID), 0)
	s.Len(s.lineItemsForSub(overriddenSub.ID), 0)
}

func (s *PlanSyncServiceSuite) TestSyncPlanPricesTerminatesLineItemsForEndedPrices() {
	// Price already ended a week ago
	endDate := s.testData.now.Add(-7 * 24 * time.Hour)
	endedPrice := s.createUsagePlanPrice("price_sync_ended", "usd", 1, &endDate)
	sub := s.createSubscription("sub_sync_term", "usd")

	// Existing live line item pointing at the ended price
	li := &subscription.SubscriptionLineItem{
		ID:             "li_sync_term",
		SubscriptionID: sub.ID,
		CustomerID:     sub.CustomerID,
		EntityID:       s.testData.plan.ID,
		EntityType:     types.SubscriptionLineItemEntityTypePlan,
		PriceID:        endedPrice.ID,
		PriceType:      types.PRICE_TYPE_USAGE,
		Currency:       "usd",
		BillingPeriod:  types.BILLING_PERIOD_MONTHLY,
		InvoiceCadence: types.InvoiceCadenceArrear,
		Quantity:       decimal.Zero,
		StartDate:      sub.StartDate,
		BaseModel:      types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().SubscriptionLineItemRepo.Create(s.GetContext(), li))

	resp, err := s.service.SyncPlanPrices(s.GetContext(), s.testData.plan.ID)
	s.NoError(err)
	s.Equal(1, resp.Summary.LineItemsTerminated)
	s.Equal(0, resp.Summary.LineItemsCreated)

	// Read back: the line item must carry the price's end date and not be recreated
	items := s.lineItemsForSub(sub.ID)
	s.Len(items, 1)
	s.False(items[0].EndDate.IsZero())
	s.True(items[0].EndDate.Equal(endDate), "line item end date should equal price end date")

	// Idempotent: re-running terminates nothing new
	again, err := s.service.SyncPlanPrices(s.GetContext(), s.testData.plan.ID)
	s.NoError(err)
	s.Equal(0, again.Summary.LineItemsTerminated)
	s.Equal(0, again.Summary.LineItemsCreated)
}

func (s *PlanSyncServiceSuite) TestSyncPlanPricesPlanNotFound() {
	_, err := s.service.SyncPlanPrices(s.GetContext(), "plan_missing")
	s.Error(err)
}

func (s *PlanSyncServiceSuite) TestSyncPlanPricesV2CreatesAndStampsSubs() {
	planPrice := s.createUsagePlanPrice("price_v2_1", "usd", 5, nil)
	sub := s.createSubscription("sub_v2_1", "usd")
	s.Equal(int64(0), sub.SyncedPriceSequence)

	resp, err := s.service.SyncPlanPricesV2(s.GetContext(), s.testData.plan.ID)
	s.NoError(err)
	s.Equal(s.testData.plan.ID, resp.PlanID)
	s.Equal(1, resp.Summary.LineItemsFoundForCreation)
	s.Equal(1, resp.Summary.LineItemsCreated)
	s.Equal(0, resp.Summary.LineItemsTerminated)

	// Read back: line item created for the plan price
	items := s.lineItemsForSub(sub.ID)
	s.Len(items, 1)
	s.Equal(planPrice.ID, items[0].PriceID)
	s.Equal(types.SubscriptionLineItemEntityTypePlan, items[0].EntityType)
	s.Equal("plan_sync_api", items[0].Metadata["added_by"])

	// Read back: subscription stamped to the plan's current sequence
	storedSub, err := s.GetStores().SubscriptionRepo.Get(s.GetContext(), sub.ID)
	s.NoError(err)
	s.Equal(int64(5), storedSub.SyncedPriceSequence)
}

func (s *PlanSyncServiceSuite) TestSyncPlanPricesV2IsIdempotent() {
	s.createUsagePlanPrice("price_v2_idem", "usd", 3, nil)
	sub := s.createSubscription("sub_v2_idem", "usd")

	first, err := s.service.SyncPlanPricesV2(s.GetContext(), s.testData.plan.ID)
	s.NoError(err)
	s.Equal(1, first.Summary.LineItemsCreated)

	// Second run: sub already stamped, so it is no longer stale — nothing happens
	second, err := s.service.SyncPlanPricesV2(s.GetContext(), s.testData.plan.ID)
	s.NoError(err)
	s.Equal(0, second.Summary.LineItemsCreated)
	s.Equal(0, second.Summary.LineItemsFoundForCreation)

	s.Len(s.lineItemsForSub(sub.ID), 1)
}

func (s *PlanSyncServiceSuite) TestSyncPlanPricesV2NoPricesToSync() {
	// Plan exists but has no usage prices → target sequence is 0
	resp, err := s.service.SyncPlanPricesV2(s.GetContext(), s.testData.plan.ID)
	s.NoError(err)
	s.Equal(s.testData.plan.ID, resp.PlanID)
	s.Equal("No plan prices to sync", resp.Message)
	s.Equal(0, resp.Summary.LineItemsCreated)
}

func (s *PlanSyncServiceSuite) TestSyncPlanPricesV2PlanNotFound() {
	_, err := s.service.SyncPlanPricesV2(s.GetContext(), "plan_missing_v2")
	s.Error(err)
}

func (s *PlanSyncServiceSuite) TestSyncPlanPricesV2TerminatesEndedPricesInPage() {
	// One live price keeps the plan sequence above the sub's stamp,
	// one ended price has a live line item that must be terminated.
	livePrice := s.createUsagePlanPrice("price_v2_live", "usd", 7, nil)
	endDate := s.testData.now.Add(-3 * 24 * time.Hour)
	endedPrice := s.createUsagePlanPrice("price_v2_ended", "usd", 6, &endDate)
	sub := s.createSubscription("sub_v2_term", "usd")

	li := &subscription.SubscriptionLineItem{
		ID:             "li_v2_term",
		SubscriptionID: sub.ID,
		CustomerID:     sub.CustomerID,
		EntityID:       s.testData.plan.ID,
		EntityType:     types.SubscriptionLineItemEntityTypePlan,
		PriceID:        endedPrice.ID,
		PriceType:      types.PRICE_TYPE_USAGE,
		Currency:       "usd",
		BillingPeriod:  types.BILLING_PERIOD_MONTHLY,
		InvoiceCadence: types.InvoiceCadenceArrear,
		Quantity:       decimal.Zero,
		StartDate:      sub.StartDate,
		BaseModel:      types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().SubscriptionLineItemRepo.Create(s.GetContext(), li))

	resp, err := s.service.SyncPlanPricesV2(s.GetContext(), s.testData.plan.ID)
	s.NoError(err)
	s.Equal(1, resp.Summary.LineItemsTerminated)

	items := s.lineItemsForSub(sub.ID)
	itemsByPrice := lo.KeyBy(items, func(i *subscription.SubscriptionLineItem) string { return i.PriceID })

	// Ended price's line item got an end date
	terminated, ok := itemsByPrice[endedPrice.ID]
	s.True(ok)
	s.False(terminated.EndDate.IsZero())
	s.True(terminated.EndDate.Equal(endDate))

	// Live price got a fresh line item
	created, ok := itemsByPrice[livePrice.ID]
	s.True(ok)
	s.True(created.EndDate.IsZero())

	// Sub stamped to the max sequence (7)
	storedSub, err := s.GetStores().SubscriptionRepo.Get(s.GetContext(), sub.ID)
	s.NoError(err)
	s.Equal(int64(7), storedSub.SyncedPriceSequence)
}

// newServiceWithFailingSyncRepo builds a plan service whose PlanPriceSyncRepo
// fails on the given method but otherwise delegates to the real in-memory store.
func (s *PlanSyncServiceSuite) newServiceWithFailingSyncRepo(failOn string) PlanService {
	params := newTestServiceParams(&s.BaseServiceTestSuite)
	params.PlanPriceSyncRepo = &failingPlanPriceSyncRepo{
		Repository: s.GetStores().PlanPriceSyncRepo,
		failOn:     failOn,
	}
	return NewPlanService(params)
}

func (s *PlanSyncServiceSuite) TestSyncPlanPricesRepoErrorsPropagate() {
	s.createUsagePlanPrice("price_v1_err", "usd", 1, nil)
	s.createSubscription("sub_v1_err", "usd")

	testCases := []struct {
		name   string
		failOn string
	}{
		{name: "termination_failure_aborts_sync", failOn: "TerminateExpiredPlanPricesLineItems"},
		{name: "list_line_items_to_create_failure_aborts_sync", failOn: "ListPlanLineItemsToCreate"},
		{name: "get_last_subscription_id_failure_aborts_sync", failOn: "GetLastSubscriptionIDInBatch"},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			service := s.newServiceWithFailingSyncRepo(tc.failOn)
			_, err := service.SyncPlanPrices(s.GetContext(), s.testData.plan.ID)
			s.Error(err)
			s.True(ierr.IsDatabase(err))
		})
	}
}

func (s *PlanSyncServiceSuite) TestSyncPlanPricesV2RepoErrorsPropagate() {
	s.createUsagePlanPrice("price_v2_err", "usd", 4, nil)
	s.createSubscription("sub_v2_err", "usd")

	testCases := []struct {
		name   string
		failOn string
	}{
		{name: "current_sequence_failure_aborts_sync", failOn: "CurrentPlanSequence"},
		{name: "discovery_failure_aborts_sync", failOn: "ListPlanLineItemsToCreateV2"},
		{name: "termination_failure_aborts_sync", failOn: "TerminatePlanPricesLineItemsV2"},
		{name: "stamping_failure_aborts_sync", failOn: "StampSubsAsSynced"},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			service := s.newServiceWithFailingSyncRepo(tc.failOn)
			_, err := service.SyncPlanPricesV2(s.GetContext(), s.testData.plan.ID)
			s.Error(err)
			s.True(ierr.IsDatabase(err))
		})
	}
}

func (s *PlanSyncServiceSuite) TestSyncPlanPricesV2StampsIncompatibleSubsWithoutCreating() {
	s.createUsagePlanPrice("price_v2_ccy", "usd", 2, nil)
	eurSub := s.createSubscription("sub_v2_eur", "eur")

	resp, err := s.service.SyncPlanPricesV2(s.GetContext(), s.testData.plan.ID)
	s.NoError(err)
	s.Equal(0, resp.Summary.LineItemsCreated)
	s.Len(s.lineItemsForSub(eurSub.ID), 0)

	// The incompatible sub is still stamped as caught up so it drops out of discovery
	storedSub, err := s.GetStores().SubscriptionRepo.Get(s.GetContext(), eurSub.ID)
	s.NoError(err)
	s.Equal(int64(2), storedSub.SyncedPriceSequence)
}
