package service

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/domain/plan"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/suite"
)

// PlanSyncTestSuite is a test suite for the plan sync functionality
type PlanSyncTestSuite struct {
	testutil.BaseServiceTestSuite
	service PlanService
	params  ServiceParams
}

func TestPlanSync(t *testing.T) {
	suite.Run(t, new(PlanSyncTestSuite))
}

func (s *PlanSyncTestSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	s.params = ServiceParams{
		Logger:                   s.GetLogger(),
		Config:                   s.GetConfig(),
		DB:                       s.GetDB(),
		SubRepo:                  s.GetStores().SubscriptionRepo,
		SubscriptionLineItemRepo: s.GetStores().SubscriptionLineItemRepo,
		PlanRepo:                 s.GetStores().PlanRepo,
		PriceRepo:                s.GetStores().PriceRepo,
		EventRepo:                s.GetStores().EventRepo,
		MeterRepo:                s.GetStores().MeterRepo,
		CustomerRepo:             s.GetStores().CustomerRepo,
		InvoiceRepo:              s.GetStores().InvoiceRepo,
		EntitlementRepo:          s.GetStores().EntitlementRepo,
		EnvironmentRepo:          s.GetStores().EnvironmentRepo,
		FeatureRepo:              s.GetStores().FeatureRepo,
		TenantRepo:               s.GetStores().TenantRepo,
		UserRepo:                 s.GetStores().UserRepo,
		AuthRepo:                 s.GetStores().AuthRepo,
		WalletRepo:               s.GetStores().WalletRepo,
		PaymentRepo:              s.GetStores().PaymentRepo,
		CreditGrantRepo:          s.GetStores().CreditGrantRepo,
		EventPublisher:           s.GetPublisher(),
		WebhookPublisher:         s.GetWebhookPublisher(),
	}
	s.service = NewPlanService(s.params)
}

// Helper function to create a test customer
func (s *PlanSyncTestSuite) createTestCustomer(id string) *customer.Customer {
	testCustomer := &customer.Customer{
		ID:         id,
		ExternalID: "ext_" + id,
		Name:       "Test Customer " + id,
		Email:      id + "@example.com",
		BaseModel:  types.GetDefaultBaseModel(s.GetContext()),
	}
	err := s.GetStores().CustomerRepo.Create(s.GetContext(), testCustomer)
	s.NoError(err)
	return testCustomer
}

// Helper function to create a test plan
func (s *PlanSyncTestSuite) createTestPlan(id string, name string) *plan.Plan {
	testPlan := &plan.Plan{
		ID:          id,
		Name:        name,
		Description: "Test plan " + id,
		BaseModel:   types.GetDefaultBaseModel(s.GetContext()),
	}
	err := s.GetStores().PlanRepo.Create(s.GetContext(), testPlan)
	s.NoError(err)
	return testPlan
}

// Helper function to create a test price
func (s *PlanSyncTestSuite) createTestPrice(id string, amount int64, entityType types.PriceEntityType, entityID string, parentPriceID string) *price.Price {
	testPrice := &price.Price{
		ID:                 id,
		Amount:             decimal.NewFromInt(amount),
		Currency:           "usd",
		EntityType:         entityType,
		EntityID:           entityID,
		Type:               types.PRICE_TYPE_USAGE,
		MeterID:            "meter-test", // Added MeterID for usage prices
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_FLAT_FEE,
		ParentPriceID:      parentPriceID,
		BaseModel:          types.GetDefaultBaseModel(s.GetContext()),
	}
	err := s.GetStores().PriceRepo.Create(s.GetContext(), testPrice)
	s.NoError(err)
	return testPrice
}

// Helper function to create a test subscription
func (s *PlanSyncTestSuite) createTestSubscription(id string, planID string, customerID string) *subscription.Subscription {
	startDate := time.Now().UTC().AddDate(0, 0, -30)
	endDate := startDate.AddDate(0, 1, 0)

	testSub := &subscription.Subscription{
		ID:                 id,
		PlanID:             planID,
		CustomerID:         customerID,
		SubscriptionStatus: types.SubscriptionStatusActive,
		StartDate:          startDate,
		Currency:           "usd",
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingCadence:     types.BILLING_CADENCE_RECURRING,
		BillingCycle:       types.BillingCycleAnniversary,
		CurrentPeriodStart: startDate,
		CurrentPeriodEnd:   endDate,
		BaseModel:          types.GetDefaultBaseModel(s.GetContext()),
	}
	err := s.GetStores().SubscriptionRepo.Create(s.GetContext(), testSub)
	s.NoError(err)
	return testSub
}

// Helper function to create a test line item
func (s *PlanSyncTestSuite) createTestLineItem(id string, subscriptionID string, customerID string, entityID string, entityType types.SubscriptionLineItemEntityType, priceID string) *subscription.SubscriptionLineItem {
	lineItem := &subscription.SubscriptionLineItem{
		ID:             id,
		SubscriptionID: subscriptionID,
		CustomerID:     customerID,
		EntityID:       entityID,
		EntityType:     entityType,
		PriceID:        priceID,
		Quantity:       decimal.NewFromInt(1),
		Currency:       "usd",
		BillingPeriod:  types.BILLING_PERIOD_MONTHLY,
		StartDate:      time.Now().UTC().AddDate(0, 0, -30),
		BaseModel:      types.GetDefaultBaseModel(s.GetContext()),
	}
	err := s.GetStores().SubscriptionLineItemRepo.Create(s.GetContext(), lineItem)
	s.NoError(err)
	return lineItem
}

// Helper function to terminate a line item
func (s *PlanSyncTestSuite) terminateLineItem(lineItem *subscription.SubscriptionLineItem) {
	lineItem.EndDate = time.Now().UTC()
	err := s.GetStores().SubscriptionLineItemRepo.Update(s.GetContext(), lineItem)
	s.NoError(err)
}

// Helper function to terminate a price
func (s *PlanSyncTestSuite) terminatePrice(price *price.Price) {
	price.EndDate = lo.ToPtr(time.Now().UTC())
	err := s.GetStores().PriceRepo.Update(s.GetContext(), price)
	s.NoError(err)
}

// Helper function to get line items for a subscription
func (s *PlanSyncTestSuite) getLineItems(subscriptionID string) []*subscription.SubscriptionLineItem {
	sub := &subscription.Subscription{ID: subscriptionID}
	lineItems, err := s.GetStores().SubscriptionLineItemRepo.ListBySubscription(s.GetContext(), sub)
	s.NoError(err)
	return lineItems
}

// Helper function to count active line items for a subscription
func (s *PlanSyncTestSuite) countActiveLineItems(subscriptionID string) int {
	lineItems := s.getLineItems(subscriptionID)
	activeCount := 0
	now := time.Now().UTC()
	for _, item := range lineItems {
		if item.IsActive(now) {
			activeCount++
		}
	}
	return activeCount
}

// Helper function to create a test meter
func (s *PlanSyncTestSuite) createTestMeter(id string, name string) *meter.Meter {
	testMeter := &meter.Meter{
		ID:        id,
		Name:      name,
		EventName: name,
		Aggregation: meter.Aggregation{
			Type:  types.AggregationSum,
			Field: "value",
		},
		EnvironmentID: types.GetEnvironmentID(s.GetContext()),
		BaseModel:     types.GetDefaultBaseModel(s.GetContext()),
	}
	err := s.GetStores().MeterRepo.CreateMeter(s.GetContext(), testMeter)
	s.NoError(err)
	return testMeter
}

// Test Scenario 1: Simple price update
func (s *PlanSyncTestSuite) TestScenario1_SimplePriceUpdate() {
	// Create test data
	s.ClearStores()

	// Create meter first to get the actual ID
	testMeter := s.createTestMeter("meter-id-1", "Test Meter 1")

	// Create customer, plan, price, subscription, and line item
	customer := s.createTestCustomer("customer-1")
	plan := s.createTestPlan("plan-1", "Plan 1")
	price1 := s.createTestPrice("price-1", 100, types.PRICE_ENTITY_TYPE_PLAN, plan.ID, "")

	// Update the price to use the actual meter ID
	price1.MeterID = testMeter.ID
	err := s.GetStores().PriceRepo.Update(s.GetContext(), price1)
	s.NoError(err)

	sub := s.createTestSubscription("sub-1", plan.ID, customer.ID)
	lineItem1 := s.createTestLineItem("line-1", sub.ID, customer.ID, plan.ID, types.SubscriptionLineItemEntityTypePlan, price1.ID)

	// Verify initial state
	s.Equal(1, s.countActiveLineItems(sub.ID))

	// Update price P1 to P2
	s.terminatePrice(price1)
	price2 := s.createTestPrice("price-2", 200, types.PRICE_ENTITY_TYPE_PLAN, plan.ID, price1.ID)

	// Update the price to use the actual meter ID
	price2.MeterID = testMeter.ID
	err = s.GetStores().PriceRepo.Update(s.GetContext(), price2)
	s.NoError(err)

	// Run plan sync
	result, err := s.service.SyncPlanPrices(s.GetContext(), plan.ID)
	s.NoError(err)
	s.NotNil(result)

	// Verify results
	s.Equal(1, result.SynchronizationSummary.SubscriptionsProcessed)
	s.Equal(1, result.SynchronizationSummary.LineItemsTerminated)
	s.Equal(1, result.SynchronizationSummary.LineItemsCreated)

	// Verify line items
	lineItems := s.getLineItems(sub.ID)
	s.Equal(2, len(lineItems))

	// Find the original line item and verify it's terminated
	for _, item := range lineItems {
		if item.ID == lineItem1.ID {
			s.False(item.EndDate.IsZero(), "Line item 1 should be terminated")
		} else {
			s.True(item.EndDate.IsZero(), "New line item should be active")
			s.Equal(price2.ID, item.PriceID, "New line item should use price 2")
		}
	}
}

// Test Scenario 2: Price override during subscription creation
func (s *PlanSyncTestSuite) TestScenario2_PriceOverrideDuringSubscriptionCreation() {
	// Create test data
	s.ClearStores()

	// Create meter first to get the actual ID
	testMeter := s.createTestMeter("meter-id-2", "Test Meter 2")

	// Create customer, plan, prices, subscription, and line item
	customer := s.createTestCustomer("customer-2")
	plan := s.createTestPlan("plan-2", "Plan 2")
	price1 := s.createTestPrice("price-1-2", 100, types.PRICE_ENTITY_TYPE_PLAN, plan.ID, "")

	// Update the price to use the actual meter ID
	price1.MeterID = testMeter.ID
	err := s.GetStores().PriceRepo.Update(s.GetContext(), price1)
	s.NoError(err)

	sub := s.createTestSubscription("sub-2", plan.ID, customer.ID)

	// Create subscription-specific price P2 that overrides P1
	price2 := s.createTestPrice("price-2-2", 150, types.PRICE_ENTITY_TYPE_SUBSCRIPTION, sub.ID, price1.ID)

	// Update the price to use the actual meter ID
	price2.MeterID = testMeter.ID
	err = s.GetStores().PriceRepo.Update(s.GetContext(), price2)
	s.NoError(err)

	lineItem1 := s.createTestLineItem("line-1-2", sub.ID, customer.ID, plan.ID, types.SubscriptionLineItemEntityTypePlan, price2.ID)

	// Verify initial state
	s.Equal(1, s.countActiveLineItems(sub.ID))

	// Update plan price P1 to P3
	s.terminatePrice(price1)
	price3 := s.createTestPrice("price-3-2", 200, types.PRICE_ENTITY_TYPE_PLAN, plan.ID, price1.ID)

	// Update the price to use the actual meter ID
	price3.MeterID = testMeter.ID
	err = s.GetStores().PriceRepo.Update(s.GetContext(), price3)
	s.NoError(err)

	// Run plan sync
	result, err := s.service.SyncPlanPrices(s.GetContext(), plan.ID)
	s.NoError(err)
	s.NotNil(result)

	// Verify results
	s.Equal(1, result.SynchronizationSummary.SubscriptionsProcessed)
	s.Equal(0, result.SynchronizationSummary.LineItemsTerminated)
	s.Equal(0, result.SynchronizationSummary.LineItemsCreated)
	s.Equal(2, result.SynchronizationSummary.LineItemsSkipped)
	s.Equal(1, result.SynchronizationSummary.SkippedAlreadyTerminated)
	s.Equal(1, result.SynchronizationSummary.SkippedOverridden)

	// Verify line items - should still have only the original line item with P2
	lineItems := s.getLineItems(sub.ID)
	s.Equal(1, len(lineItems))
	s.Equal(lineItem1.ID, lineItems[0].ID)
	s.Equal(price2.ID, lineItems[0].PriceID)
	s.True(lineItems[0].EndDate.IsZero(), "Line item should still be active")
}

// Test Scenario 3: Manual line item update followed by plan price update
func (s *PlanSyncTestSuite) TestScenario3_ManualLineItemUpdateFollowedByPlanPriceUpdate() {
	// Create test data
	s.ClearStores()

	// Create meter first to get the actual ID
	testMeter := s.createTestMeter("meter-id-3", "Test Meter 3")

	// Create customer, plan, price, subscription, and line item
	customer := s.createTestCustomer("customer-3")
	plan := s.createTestPlan("plan-3", "Plan 3")
	price1 := s.createTestPrice("price-1-3", 100, types.PRICE_ENTITY_TYPE_PLAN, plan.ID, "")

	// Update the price to use the actual meter ID
	price1.MeterID = testMeter.ID
	err := s.GetStores().PriceRepo.Update(s.GetContext(), price1)
	s.NoError(err)

	sub := s.createTestSubscription("sub-3", plan.ID, customer.ID)
	lineItem1 := s.createTestLineItem("line-1-3", sub.ID, customer.ID, plan.ID, types.SubscriptionLineItemEntityTypePlan, price1.ID)

	// Verify initial state
	s.Equal(1, s.countActiveLineItems(sub.ID))

	// Manual update: L1 (with P1) is terminated, L2 (with P2) is created
	// Create a new subscription-specific price P2
	price2 := s.createTestPrice("price-2-3", 150, types.PRICE_ENTITY_TYPE_SUBSCRIPTION, sub.ID, price1.ID)

	// Update the price to use the actual meter ID
	price2.MeterID = testMeter.ID
	err = s.GetStores().PriceRepo.Update(s.GetContext(), price2)
	s.NoError(err)

	// Terminate L1
	s.terminateLineItem(lineItem1)

	// Create L2 with P2
	lineItem2 := s.createTestLineItem("line-2-3", sub.ID, customer.ID, plan.ID, types.SubscriptionLineItemEntityTypePlan, price2.ID)

	// Verify state after manual update
	s.Equal(1, s.countActiveLineItems(sub.ID))

	// Update plan price P1 to P3
	s.terminatePrice(price1)
	price3 := s.createTestPrice("price-3-3", 200, types.PRICE_ENTITY_TYPE_PLAN, plan.ID, price1.ID)

	// Update the price to use the actual meter ID
	price3.MeterID = testMeter.ID
	err = s.GetStores().PriceRepo.Update(s.GetContext(), price3)
	s.NoError(err)

	// Run plan sync
	result, err := s.service.SyncPlanPrices(s.GetContext(), plan.ID)
	s.NoError(err)
	s.NotNil(result)

	// Verify results
	s.Equal(1, result.SynchronizationSummary.SubscriptionsProcessed)
	s.Equal(0, result.SynchronizationSummary.LineItemsTerminated)
	s.Equal(0, result.SynchronizationSummary.LineItemsCreated)
	s.Equal(2, result.SynchronizationSummary.LineItemsSkipped)
	s.Equal(1, result.SynchronizationSummary.SkippedAlreadyTerminated)
	s.Equal(1, result.SynchronizationSummary.SkippedOverridden)

	// Verify line items - should still have L1 (terminated) and L2 (active)
	lineItems := s.getLineItems(sub.ID)
	s.Equal(2, len(lineItems))

	// Find L1 and L2 and verify their states
	for _, item := range lineItems {
		if item.ID == lineItem1.ID {
			s.False(item.EndDate.IsZero(), "Line item 1 should be terminated")
			s.Equal(price1.ID, item.PriceID)
		} else if item.ID == lineItem2.ID {
			s.True(item.EndDate.IsZero(), "Line item 2 should be active")
			s.Equal(price2.ID, item.PriceID)
		}
	}
}

// Test Scenario 4: Complex case with line item update and plan price update
// This is similar to Scenario 3 but with different expectations
func (s *PlanSyncTestSuite) TestScenario4_ComplexCaseWithLineItemUpdateAndPlanPriceUpdate() {
	// Create test data
	s.ClearStores()

	// Create meter first to get the actual ID
	testMeter := s.createTestMeter("meter-id-4", "Test Meter 4")

	// Create customer, plan, price, subscription, and line item
	customer := s.createTestCustomer("customer-4")
	plan := s.createTestPlan("plan-4", "Plan 4")
	price1 := s.createTestPrice("price-1-4", 100, types.PRICE_ENTITY_TYPE_PLAN, plan.ID, "")

	// Update the price to use the actual meter ID
	price1.MeterID = testMeter.ID
	err := s.GetStores().PriceRepo.Update(s.GetContext(), price1)
	s.NoError(err)

	sub := s.createTestSubscription("sub-4", plan.ID, customer.ID)
	lineItem1 := s.createTestLineItem("line-1-4", sub.ID, customer.ID, plan.ID, types.SubscriptionLineItemEntityTypePlan, price1.ID)

	// Verify initial state
	s.Equal(1, s.countActiveLineItems(sub.ID))

	// Manual update: L1 (with P1) is terminated, L2 (with P2) is created
	// Create a new subscription-specific price P2
	price2 := s.createTestPrice("price-2-4", 150, types.PRICE_ENTITY_TYPE_SUBSCRIPTION, sub.ID, price1.ID)

	// Update the price to use the actual meter ID
	price2.MeterID = testMeter.ID
	err = s.GetStores().PriceRepo.Update(s.GetContext(), price2)
	s.NoError(err)

	// Terminate L1
	s.terminateLineItem(lineItem1)

	// Create L2 with P2
	lineItem2 := s.createTestLineItem("line-2-4", sub.ID, customer.ID, plan.ID, types.SubscriptionLineItemEntityTypePlan, price2.ID)

	// Verify state after manual update
	s.Equal(1, s.countActiveLineItems(sub.ID))

	// Update plan price P1 to P3
	s.terminatePrice(price1)
	price3 := s.createTestPrice("price-3-4", 200, types.PRICE_ENTITY_TYPE_PLAN, plan.ID, price1.ID)

	// Update the price to use the actual meter ID
	price3.MeterID = testMeter.ID
	err = s.GetStores().PriceRepo.Update(s.GetContext(), price3)
	s.NoError(err)

	// Run plan sync
	result, err := s.service.SyncPlanPrices(s.GetContext(), plan.ID)
	s.NoError(err)
	s.NotNil(result)

	// Verify results
	s.Equal(1, result.SynchronizationSummary.SubscriptionsProcessed)
	s.Equal(0, result.SynchronizationSummary.LineItemsTerminated)
	s.Equal(0, result.SynchronizationSummary.LineItemsCreated)
	s.Equal(2, result.SynchronizationSummary.LineItemsSkipped)
	s.Equal(1, result.SynchronizationSummary.SkippedAlreadyTerminated)
	s.Equal(1, result.SynchronizationSummary.SkippedOverridden)

	// Verify line items - should still have L1 (terminated) and L2 (active)
	lineItems := s.getLineItems(sub.ID)
	s.Equal(2, len(lineItems))

	// Find L1 and L2 and verify their states
	for _, item := range lineItems {
		if item.ID == lineItem1.ID {
			s.False(item.EndDate.IsZero(), "Line item 1 should be terminated")
			s.Equal(price1.ID, item.PriceID)
		} else if item.ID == lineItem2.ID {
			s.True(item.EndDate.IsZero(), "Line item 2 should be active")
			s.Equal(price2.ID, item.PriceID)
		}
	}
}

// Test Scenario 5: Most complex case with subscription override and multiple updates
func (s *PlanSyncTestSuite) TestScenario5_MostComplexCaseWithSubscriptionOverrideAndMultipleUpdates() {
	// Create test data
	s.ClearStores()

	// Create meter first to get the actual ID
	testMeter := s.createTestMeter("meter-id-5", "Test Meter 5")

	// Create customer, plan, price, subscription
	customer := s.createTestCustomer("customer-5")
	plan := s.createTestPlan("plan-5", "Plan 5")
	price1 := s.createTestPrice("price-1-5", 100, types.PRICE_ENTITY_TYPE_PLAN, plan.ID, "")

	// Update the price to use the actual meter ID
	price1.MeterID = testMeter.ID
	err := s.GetStores().PriceRepo.Update(s.GetContext(), price1)
	s.NoError(err)

	sub := s.createTestSubscription("sub-5", plan.ID, customer.ID)

	// During subscription creation, P1 is overridden to P2
	price2 := s.createTestPrice("price-2-5", 150, types.PRICE_ENTITY_TYPE_SUBSCRIPTION, sub.ID, price1.ID)

	// Update the price to use the actual meter ID
	price2.MeterID = testMeter.ID
	err = s.GetStores().PriceRepo.Update(s.GetContext(), price2)
	s.NoError(err)

	lineItem1 := s.createTestLineItem("line-1-5", sub.ID, customer.ID, plan.ID, types.SubscriptionLineItemEntityTypePlan, price2.ID)

	// Verify initial state
	s.Equal(1, s.countActiveLineItems(sub.ID))

	// Manual update: L1 (with P2) is terminated, L2 (with P3) is created
	// Create a new subscription-specific price P3
	price3 := s.createTestPrice("price-3-5", 175, types.PRICE_ENTITY_TYPE_SUBSCRIPTION, sub.ID, price1.ID)

	// Update the price to use the actual meter ID
	price3.MeterID = testMeter.ID
	err = s.GetStores().PriceRepo.Update(s.GetContext(), price3)
	s.NoError(err)

	// Terminate L1
	s.terminateLineItem(lineItem1)

	// Create L2 with P3
	lineItem2 := s.createTestLineItem("line-2-5", sub.ID, customer.ID, plan.ID, types.SubscriptionLineItemEntityTypePlan, price3.ID)

	// Verify state after manual update
	s.Equal(1, s.countActiveLineItems(sub.ID))

	// Update plan price P1 to P4
	s.terminatePrice(price1)
	price4 := s.createTestPrice("price-4-5", 200, types.PRICE_ENTITY_TYPE_PLAN, plan.ID, price1.ID)

	// Update the price to use the actual meter ID
	price4.MeterID = testMeter.ID
	err = s.GetStores().PriceRepo.Update(s.GetContext(), price4)
	s.NoError(err)

	// Run plan sync
	result, err := s.service.SyncPlanPrices(s.GetContext(), plan.ID)
	s.NoError(err)
	s.NotNil(result)

	// Verify results
	s.Equal(1, result.SynchronizationSummary.SubscriptionsProcessed)
	s.Equal(0, result.SynchronizationSummary.LineItemsTerminated)
	s.Equal(0, result.SynchronizationSummary.LineItemsCreated)
	s.Equal(2, result.SynchronizationSummary.LineItemsSkipped)
	s.Equal(1, result.SynchronizationSummary.SkippedAlreadyTerminated)
	s.Equal(1, result.SynchronizationSummary.SkippedOverridden)

	// Verify line items - should still have L1 (terminated) and L2 (active)
	lineItems := s.getLineItems(sub.ID)
	s.Equal(2, len(lineItems))

	// Find L1 and L2 and verify their states
	for _, item := range lineItems {
		if item.ID == lineItem1.ID {
			s.False(item.EndDate.IsZero(), "Line item 1 should be terminated")
			s.Equal(price2.ID, item.PriceID)
		} else if item.ID == lineItem2.ID {
			s.True(item.EndDate.IsZero(), "Line item 2 should be active")
			s.Equal(price3.ID, item.PriceID)
		}
	}
}
