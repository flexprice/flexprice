package service

import (
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/domain/plan"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type SubscriptionChangeServiceTestSuite struct {
	testutil.BaseServiceTestSuite
	subscriptionChangeService *subscriptionChangeService
	subscriptionService       *subscriptionService
	planService               *planService
	priceService              *priceService
}

func (s *SubscriptionChangeServiceTestSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	s.setupServices()
}

func (s *SubscriptionChangeServiceTestSuite) TearDownTest() {
	s.BaseServiceTestSuite.TearDownTest()
}

func (s *SubscriptionChangeServiceTestSuite) setupServices() {
	serviceParams := ServiceParams{
		Logger:                     s.GetLogger(),
		Config:                     s.GetConfig(),
		DB:                         s.GetDB(),
		TaxAssociationRepo:         s.GetStores().TaxAssociationRepo,
		TaxRateRepo:                s.GetStores().TaxRateRepo,
		AuthRepo:                   s.GetStores().AuthRepo,
		UserRepo:                   s.GetStores().UserRepo,
		EventRepo:                  s.GetStores().EventRepo,
		MeterRepo:                  s.GetStores().MeterRepo,
		PriceRepo:                  s.GetStores().PriceRepo,
		CustomerRepo:               s.GetStores().CustomerRepo,
		PlanRepo:                   s.GetStores().PlanRepo,
		SubRepo:                    s.GetStores().SubscriptionRepo,
		WalletRepo:                 s.GetStores().WalletRepo,
		TenantRepo:                 s.GetStores().TenantRepo,
		InvoiceRepo:                s.GetStores().InvoiceRepo,
		FeatureRepo:                s.GetStores().FeatureRepo,
		EntitlementRepo:            s.GetStores().EntitlementRepo,
		PaymentRepo:                s.GetStores().PaymentRepo,
		SecretRepo:                 s.GetStores().SecretRepo,
		EnvironmentRepo:            s.GetStores().EnvironmentRepo,
		TaskRepo:                   s.GetStores().TaskRepo,
		CreditGrantRepo:            s.GetStores().CreditGrantRepo,
		CreditGrantApplicationRepo: s.GetStores().CreditGrantApplicationRepo,
		CouponRepo:                 s.GetStores().CouponRepo,
		CouponAssociationRepo:      s.GetStores().CouponAssociationRepo,
		CouponApplicationRepo:      s.GetStores().CouponApplicationRepo,
		AddonAssociationRepo:       s.GetStores().AddonAssociationRepo,
		TaxAppliedRepo:             s.GetStores().TaxAppliedRepo,
		CreditNoteRepo:             s.GetStores().CreditNoteRepo,
		CreditNoteLineItemRepo:     s.GetStores().CreditNoteLineItemRepo,
		ConnectionRepo:             s.GetStores().ConnectionRepo,
		SettingsRepo:               s.GetStores().SettingsRepo,
		EventPublisher:             s.GetPublisher(),
		WebhookPublisher:           s.GetWebhookPublisher(),
		ProrationCalculator:        s.GetCalculator(), // Use the correct method from BaseServiceTestSuite
	}

	s.subscriptionChangeService = NewSubscriptionChangeService(serviceParams).(*subscriptionChangeService)
	s.subscriptionService = NewSubscriptionService(serviceParams).(*subscriptionService)
	s.planService = NewPlanService(serviceParams).(*planService)
	s.priceService = NewPriceService(serviceParams).(*priceService)
}

func (s *SubscriptionChangeServiceTestSuite) createTestPlan(name string, amount decimal.Decimal) *plan.Plan {
	ctx := s.GetContext()

	planReq := dto.CreatePlanRequest{
		Name:        name,
		Description: "Test plan for subscription changes",
	}

	planResponse, err := s.planService.CreatePlan(ctx, planReq)
	require.NoError(s.T(), err)

	// Create a price for the plan
	amt := amount
	priceReq := dto.CreatePriceRequest{
		Amount:             &amt,
		Currency:           "usd",
		Type:               types.PRICE_TYPE_FIXED,
		BillingModel:       types.BILLING_MODEL_FLAT_FEE,
		BillingCadence:     types.BILLING_CADENCE_RECURRING,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		InvoiceCadence:     types.InvoiceCadenceAdvance,
		EntityType:         types.PRICE_ENTITY_TYPE_PLAN,
		EntityID:           planResponse.Plan.ID,
	}

	_, err = s.priceService.CreatePrice(ctx, priceReq)
	require.NoError(s.T(), err)

	return planResponse.Plan
}

func (s *SubscriptionChangeServiceTestSuite) createTestSubscription(planID, customerID string) *subscription.Subscription {
	ctx := s.GetContext()

	subReq := dto.CreateSubscriptionRequest{
		CustomerID:         customerID,
		PlanID:             planID,
		Currency:           "usd",
		BillingCadence:     types.BILLING_CADENCE_RECURRING,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingCycle:       types.BillingCycleAnniversary,
	}

	subResponse, err := s.subscriptionService.CreateSubscription(ctx, subReq)
	require.NoError(s.T(), err)

	// Get the subscription with line items
	sub, _, err := s.GetStores().SubscriptionRepo.GetWithLineItems(ctx, subResponse.Subscription.ID)
	require.NoError(s.T(), err)

	return sub
}

// Helper method to create test customer
func (s *SubscriptionChangeServiceTestSuite) createTestCustomer() *customer.Customer {
	ctx := s.GetContext()

	customer := &customer.Customer{
		ID:         s.GetUUID(),
		ExternalID: "ext_" + s.GetUUID(),
		Name:       "Test Customer",
		Email:      "test@example.com",
		BaseModel:  types.GetDefaultBaseModel(ctx),
	}

	err := s.GetStores().CustomerRepo.Create(ctx, customer)
	require.NoError(s.T(), err)

	return customer
}

// Helper method to create subscription change request with all required fields
func (s *SubscriptionChangeServiceTestSuite) createSubscriptionChangeRequest(targetPlanID string, prorationBehavior types.ProrationBehavior) dto.SubscriptionChangeRequest {
	return dto.SubscriptionChangeRequest{
		TargetPlanID:       targetPlanID,
		ProrationBehavior:  prorationBehavior,
		BillingCadence:     types.BILLING_CADENCE_RECURRING,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingCycle:       types.BillingCycleAnniversary,
	}
}

// Helper method to create test plan with specific billing period
func (s *SubscriptionChangeServiceTestSuite) createTestPlanWithBilling(name string, amount decimal.Decimal, billingPeriod types.BillingPeriod) *plan.Plan {
	ctx := s.GetContext()

	planReq := dto.CreatePlanRequest{
		Name:        name,
		Description: "Test plan for subscription changes",
	}

	planResponse, err := s.planService.CreatePlan(ctx, planReq)
	require.NoError(s.T(), err)

	// Create a price for the plan
	amt := amount
	priceReq := dto.CreatePriceRequest{
		Amount:             &amt,
		Currency:           "usd",
		Type:               types.PRICE_TYPE_FIXED,
		BillingModel:       types.BILLING_MODEL_FLAT_FEE,
		BillingCadence:     types.BILLING_CADENCE_RECURRING,
		BillingPeriod:      billingPeriod,
		BillingPeriodCount: 1,
		InvoiceCadence:     types.InvoiceCadenceAdvance,
		EntityType:         types.PRICE_ENTITY_TYPE_PLAN,
		EntityID:           planResponse.Plan.ID,
	}

	_, err = s.priceService.CreatePrice(ctx, priceReq)
	require.NoError(s.T(), err)

	return planResponse.Plan
}

// Helper method to create subscription with specific billing cycle
func (s *SubscriptionChangeServiceTestSuite) createTestSubscriptionWithCycle(planID, customerID string, billingCycle types.BillingCycle, billingPeriod types.BillingPeriod) *subscription.Subscription {
	ctx := s.GetContext()

	subReq := dto.CreateSubscriptionRequest{
		CustomerID:         customerID,
		PlanID:             planID,
		Currency:           "usd",
		BillingCadence:     types.BILLING_CADENCE_RECURRING,
		BillingPeriod:      billingPeriod,
		BillingPeriodCount: 1,
		BillingCycle:       billingCycle,
	}

	subResponse, err := s.subscriptionService.CreateSubscription(ctx, subReq)
	require.NoError(s.T(), err)

	// Get the subscription with line items
	sub, _, err := s.GetStores().SubscriptionRepo.GetWithLineItems(ctx, subResponse.Subscription.ID)
	require.NoError(s.T(), err)

	return sub
}

// Helper method to create usage-based plan
func (s *SubscriptionChangeServiceTestSuite) createUsageBasedPlan(name string, fixedAmount decimal.Decimal, usageAmount decimal.Decimal) (*plan.Plan, *meter.Meter) {
	ctx := s.GetContext()

	// Create meter for usage tracking
	meter := &meter.Meter{
		ID:        s.GetUUID(),
		Name:      "API Calls",
		EventName: "api_call",
		Aggregation: meter.Aggregation{
			Type: types.AggregationCount,
		},
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	err := s.GetStores().MeterRepo.CreateMeter(ctx, meter)
	require.NoError(s.T(), err)

	// Create plan
	planReq := dto.CreatePlanRequest{
		Name:        name,
		Description: "Usage-based test plan",
	}

	planResponse, err := s.planService.CreatePlan(ctx, planReq)
	require.NoError(s.T(), err)

	// Create fixed price
	if !fixedAmount.IsZero() {
		amt := fixedAmount
		fixedPriceReq := dto.CreatePriceRequest{
			Amount:             &amt,
			Currency:           "usd",
			Type:               types.PRICE_TYPE_FIXED,
			BillingModel:       types.BILLING_MODEL_FLAT_FEE,
			BillingCadence:     types.BILLING_CADENCE_RECURRING,
			BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
			BillingPeriodCount: 1,
			InvoiceCadence:     types.InvoiceCadenceAdvance,
			EntityType:         types.PRICE_ENTITY_TYPE_PLAN,
			EntityID:           planResponse.Plan.ID,
		}

		_, err = s.priceService.CreatePrice(ctx, fixedPriceReq)
		require.NoError(s.T(), err)
	}

	// Create usage price
	usageAmt := usageAmount
	usagePriceReq := dto.CreatePriceRequest{
		Amount:             &usageAmt,
		Currency:           "usd",
		Type:               types.PRICE_TYPE_USAGE,
		BillingModel:       types.BILLING_MODEL_FLAT_FEE,
		BillingCadence:     types.BILLING_CADENCE_RECURRING,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		InvoiceCadence:     types.InvoiceCadenceArrear,
		EntityType:         types.PRICE_ENTITY_TYPE_PLAN,
		EntityID:           planResponse.Plan.ID,
		MeterID:            meter.ID,
	}

	_, err = s.priceService.CreatePrice(ctx, usagePriceReq)
	require.NoError(s.T(), err)

	return planResponse.Plan, meter
}

// Helper method to create usage-based plan with multiple meters
func (s *SubscriptionChangeServiceTestSuite) createMultiMeterUsagePlan(name string, fixedAmount decimal.Decimal, meters []struct {
	name        string
	eventName   string
	amount      decimal.Decimal
	aggregation types.AggregationType
}) (*plan.Plan, []*meter.Meter) {
	ctx := s.GetContext()

	// Create meters for usage tracking
	createdMeters := make([]*meter.Meter, len(meters))
	for i, meterSpec := range meters {
		meter := &meter.Meter{
			ID:        s.GetUUID(),
			Name:      meterSpec.name,
			EventName: meterSpec.eventName,
			Aggregation: meter.Aggregation{
				Type: meterSpec.aggregation,
			},
			BaseModel: types.GetDefaultBaseModel(ctx),
		}
		err := s.GetStores().MeterRepo.CreateMeter(ctx, meter)
		require.NoError(s.T(), err)
		createdMeters[i] = meter
	}

	// Create plan
	planReq := dto.CreatePlanRequest{
		Name:        name,
		Description: "Multi-meter usage-based test plan",
	}

	planResponse, err := s.planService.CreatePlan(ctx, planReq)
	require.NoError(s.T(), err)

	// Create fixed price component if specified
	if !fixedAmount.IsZero() {
		amt := fixedAmount
		fixedPriceReq := dto.CreatePriceRequest{
			Amount:             &amt,
			Currency:           "usd",
			Type:               types.PRICE_TYPE_FIXED,
			BillingModel:       types.BILLING_MODEL_FLAT_FEE,
			BillingCadence:     types.BILLING_CADENCE_RECURRING,
			BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
			BillingPeriodCount: 1,
			InvoiceCadence:     types.InvoiceCadenceAdvance,
			EntityType:         types.PRICE_ENTITY_TYPE_PLAN,
			EntityID:           planResponse.Plan.ID,
		}

		_, err = s.priceService.CreatePrice(ctx, fixedPriceReq)
		require.NoError(s.T(), err)
	}

	// Create usage prices for each meter
	for i, meterSpec := range meters {
		usageAmt := meterSpec.amount
		usagePriceReq := dto.CreatePriceRequest{
			Amount:             &usageAmt,
			Currency:           "usd",
			Type:               types.PRICE_TYPE_USAGE,
			BillingModel:       types.BILLING_MODEL_FLAT_FEE,
			BillingCadence:     types.BILLING_CADENCE_RECURRING,
			BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
			BillingPeriodCount: 1,
			InvoiceCadence:     types.InvoiceCadenceArrear,
			EntityType:         types.PRICE_ENTITY_TYPE_PLAN,
			EntityID:           planResponse.Plan.ID,
			MeterID:            createdMeters[i].ID,
		}

		_, err = s.priceService.CreatePrice(ctx, usagePriceReq)
		require.NoError(s.T(), err)
	}

	return planResponse.Plan, createdMeters
}

func (s *SubscriptionChangeServiceTestSuite) TestPreviewSubscriptionUpgrade() {
	ctx := s.GetContext()

	// Create test data
	customer := s.createTestCustomer()
	basicPlan := s.createTestPlan("Basic", decimal.NewFromFloat(10.00))
	premiumPlan := s.createTestPlan("Premium", decimal.NewFromFloat(20.00))
	testSub := s.createTestSubscription(basicPlan.ID, customer.ID)

	// Create preview request
	req := s.createSubscriptionChangeRequest(premiumPlan.ID, types.ProrationBehaviorCreateProrations)

	// Test preview
	response, err := s.subscriptionChangeService.PreviewSubscriptionChange(ctx, testSub.ID, req)

	// Assertions
	require.NoError(s.T(), err)
	assert.NotNil(s.T(), response)
	assert.Equal(s.T(), testSub.ID, response.SubscriptionID)
	assert.Equal(s.T(), basicPlan.ID, response.CurrentPlan.ID)
	assert.Equal(s.T(), premiumPlan.ID, response.TargetPlan.ID)
	assert.Equal(s.T(), types.SubscriptionChangeTypeUpgrade, response.ChangeType)
	assert.NotNil(s.T(), response.ProrationDetails)
	assert.NotNil(s.T(), response.NextInvoicePreview)
}

func (s *SubscriptionChangeServiceTestSuite) TestPreviewSubscriptionDowngrade() {
	ctx := s.GetContext()

	// Create test data
	customer := s.createTestCustomer()
	basicPlan := s.createTestPlan("Basic", decimal.NewFromFloat(10.00))
	premiumPlan := s.createTestPlan("Premium", decimal.NewFromFloat(20.00))
	testSub := s.createTestSubscription(premiumPlan.ID, customer.ID)

	// Create preview request
	req := s.createSubscriptionChangeRequest(basicPlan.ID, types.ProrationBehaviorCreateProrations)

	// Test preview
	response, err := s.subscriptionChangeService.PreviewSubscriptionChange(ctx, testSub.ID, req)

	// Assertions
	require.NoError(s.T(), err)
	assert.NotNil(s.T(), response)
	assert.Equal(s.T(), testSub.ID, response.SubscriptionID)
	assert.Equal(s.T(), premiumPlan.ID, response.CurrentPlan.ID)
	assert.Equal(s.T(), basicPlan.ID, response.TargetPlan.ID)
	assert.Equal(s.T(), types.SubscriptionChangeTypeDowngrade, response.ChangeType)
	assert.Contains(s.T(), response.Warnings, "This is a downgrade. You may lose access to certain features.")
}

func (s *SubscriptionChangeServiceTestSuite) TestPreviewSubscriptionLateral() {
	ctx := s.GetContext()

	// Create test data
	customer := s.createTestCustomer()
	plan1 := s.createTestPlan("Plan A", decimal.NewFromFloat(15.00))
	plan2 := s.createTestPlan("Plan B", decimal.NewFromFloat(15.00))
	testSub := s.createTestSubscription(plan1.ID, customer.ID)

	// Create preview request
	req := s.createSubscriptionChangeRequest(plan2.ID, types.ProrationBehaviorCreateProrations)

	// Test preview
	response, err := s.subscriptionChangeService.PreviewSubscriptionChange(ctx, testSub.ID, req)

	// Assertions
	require.NoError(s.T(), err)
	assert.NotNil(s.T(), response)
	assert.Equal(s.T(), types.SubscriptionChangeTypeLateral, response.ChangeType)
}

func (s *SubscriptionChangeServiceTestSuite) TestExecuteSubscriptionUpgrade() {
	ctx := s.GetContext()

	// Create test data
	customer := s.createTestCustomer()
	basicPlan := s.createTestPlan("Basic", decimal.NewFromFloat(10.00))
	premiumPlan := s.createTestPlan("Premium", decimal.NewFromFloat(20.00))
	testSub := s.createTestSubscription(basicPlan.ID, customer.ID)
	originalSubID := testSub.ID

	// Create execute request
	req := s.createSubscriptionChangeRequest(premiumPlan.ID, types.ProrationBehaviorCreateProrations)

	// Test execution
	response, err := s.subscriptionChangeService.ExecuteSubscriptionChange(ctx, testSub.ID, req)

	// Assertions
	require.NoError(s.T(), err)
	assert.NotNil(s.T(), response)
	assert.Equal(s.T(), types.SubscriptionChangeTypeUpgrade, response.ChangeType)
	assert.Equal(s.T(), originalSubID, response.OldSubscription.ID)
	assert.NotEqual(s.T(), originalSubID, response.NewSubscription.ID)
	assert.Equal(s.T(), types.SubscriptionStatusCancelled, response.OldSubscription.Status)
	assert.Equal(s.T(), types.SubscriptionStatusActive, response.NewSubscription.Status)
	assert.Equal(s.T(), premiumPlan.ID, response.NewSubscription.PlanID)

	// Verify old subscription is archived
	oldSub, err := s.GetStores().SubscriptionRepo.Get(ctx, originalSubID)
	require.NoError(s.T(), err)
	assert.Equal(s.T(), types.SubscriptionStatusCancelled, oldSub.SubscriptionStatus)
	assert.NotNil(s.T(), oldSub.CancelledAt)

	// Verify new subscription exists
	newSub, err := s.GetStores().SubscriptionRepo.Get(ctx, response.NewSubscription.ID)
	require.NoError(s.T(), err)
	assert.Equal(s.T(), types.SubscriptionStatusActive, newSub.SubscriptionStatus)
	assert.Equal(s.T(), premiumPlan.ID, newSub.PlanID)
	assert.Equal(s.T(), customer.ID, newSub.CustomerID)
}

func (s *SubscriptionChangeServiceTestSuite) TestExecuteSubscriptionChangeWithoutProration() {
	ctx := s.GetContext()

	// Create test data
	customer := s.createTestCustomer()
	basicPlan := s.createTestPlan("Basic", decimal.NewFromFloat(10.00))
	premiumPlan := s.createTestPlan("Premium", decimal.NewFromFloat(20.00))
	testSub := s.createTestSubscription(basicPlan.ID, customer.ID)

	// Create execute request without proration
	req := s.createSubscriptionChangeRequest(premiumPlan.ID, types.ProrationBehaviorNone)

	// Test execution
	response, err := s.subscriptionChangeService.ExecuteSubscriptionChange(ctx, testSub.ID, req)

	// Assertions
	require.NoError(s.T(), err)
	assert.NotNil(s.T(), response)
	assert.Nil(s.T(), response.ProrationApplied)
	assert.Nil(s.T(), response.Invoice)
}

func (s *SubscriptionChangeServiceTestSuite) TestPreviewSubscriptionChangeValidation() {
	ctx := s.GetContext()

	// Test with invalid subscription ID
	req := s.createSubscriptionChangeRequest("invalid-plan-id", types.ProrationBehaviorCreateProrations)

	_, err := s.subscriptionChangeService.PreviewSubscriptionChange(ctx, "invalid-sub-id", req)
	assert.Error(s.T(), err)
}

func (s *SubscriptionChangeServiceTestSuite) TestExecuteSubscriptionChangeValidation() {
	ctx := s.GetContext()

	// Test with invalid subscription ID
	req := s.createSubscriptionChangeRequest("invalid-plan-id", types.ProrationBehaviorCreateProrations)

	_, err := s.subscriptionChangeService.ExecuteSubscriptionChange(ctx, "invalid-sub-id", req)
	assert.Error(s.T(), err)
}

func (s *SubscriptionChangeServiceTestSuite) TestCalculatePeriodEndHelper() {
	service := s.subscriptionChangeService
	start := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)

	// Test daily
	end := service.calculatePeriodEnd(start, types.BILLING_PERIOD_DAILY, 7)
	expected := start.AddDate(0, 0, 7)
	assert.Equal(s.T(), expected, end)

	// Test weekly
	end = service.calculatePeriodEnd(start, types.BILLING_PERIOD_WEEKLY, 2)
	expected = start.AddDate(0, 0, 14)
	assert.Equal(s.T(), expected, end)

	// Test monthly
	end = service.calculatePeriodEnd(start, types.BILLING_PERIOD_MONTHLY, 3)
	expected = start.AddDate(0, 3, 0)
	assert.Equal(s.T(), expected, end)

	// Test annual
	end = service.calculatePeriodEnd(start, types.BILLING_PERIOD_ANNUAL, 1)
	expected = start.AddDate(1, 0, 0)
	assert.Equal(s.T(), expected, end)
}

func (s *SubscriptionChangeServiceTestSuite) TestGenerateWarningsHelper() {
	service := s.subscriptionChangeService

	// Create test subscription with trial end after the hardcoded date in the service
	futureTime := time.Date(2025, 12, 25, 0, 0, 0, 0, time.UTC)
	testSub := &subscription.Subscription{
		TrialEnd: &futureTime,
	}

	// Create test plan
	testPlan := &plan.Plan{
		Name: "Test Plan",
	}

	// Test downgrade warnings
	warnings := service.generateWarnings(testSub, testPlan, types.SubscriptionChangeTypeDowngrade, types.ProrationBehaviorCreateProrations)
	assert.Contains(s.T(), warnings, "This is a downgrade. You may lose access to certain features.")
	assert.Contains(s.T(), warnings, "Changing plans during trial period may end your trial immediately.")
	assert.Contains(s.T(), warnings, "Proration charges or credits will be applied to your next invoice.")

	// Test upgrade warnings (no downgrade warning)
	warnings = service.generateWarnings(testSub, testPlan, types.SubscriptionChangeTypeUpgrade, types.ProrationBehaviorNone)
	assert.NotContains(s.T(), warnings, "This is a downgrade. You may lose access to certain features.")
	assert.Contains(s.T(), warnings, "Changing plans during trial period may end your trial immediately.")
	assert.NotContains(s.T(), warnings, "Proration charges or credits will be applied to your next invoice.")
}

// ========================================
// BASIC TEST CASES
// ========================================

// TC-001: Upgrade from Basic Plan to Pro Plan
func (s *SubscriptionChangeServiceTestSuite) TestUpgradeBasicToPro() {
	ctx := s.GetContext()

	// Create test data
	customer := s.createTestCustomer()
	basicPlan := s.createTestPlan("Basic", decimal.NewFromFloat(10.00))
	proPlan := s.createTestPlan("Pro", decimal.NewFromFloat(30.00))
	testSub := s.createTestSubscription(basicPlan.ID, customer.ID)

	// Create upgrade request
	req := s.createSubscriptionChangeRequest(proPlan.ID, types.ProrationBehaviorCreateProrations)

	// Test preview first
	previewReq := req
	previewResponse, err := s.subscriptionChangeService.PreviewSubscriptionChange(ctx, testSub.ID, previewReq)

	// Assertions for preview
	require.NoError(s.T(), err)
	assert.NotNil(s.T(), previewResponse)
	assert.Equal(s.T(), types.SubscriptionChangeTypeUpgrade, previewResponse.ChangeType)
	assert.Equal(s.T(), basicPlan.ID, previewResponse.CurrentPlan.ID)
	assert.Equal(s.T(), proPlan.ID, previewResponse.TargetPlan.ID)
	assert.NotNil(s.T(), previewResponse.ProrationDetails)

	// Execute the change
	executeResponse, err := s.subscriptionChangeService.ExecuteSubscriptionChange(ctx, testSub.ID, req)

	// Assertions for execution
	require.NoError(s.T(), err)
	assert.NotNil(s.T(), executeResponse)
	assert.Equal(s.T(), types.SubscriptionChangeTypeUpgrade, executeResponse.ChangeType)
	assert.NotEqual(s.T(), testSub.ID, executeResponse.NewSubscription.ID)
	assert.Equal(s.T(), proPlan.ID, executeResponse.NewSubscription.PlanID)

	// Verify old subscription is archived
	oldSub, err := s.GetStores().SubscriptionRepo.Get(ctx, testSub.ID)
	require.NoError(s.T(), err)
	assert.Equal(s.T(), types.SubscriptionStatusCancelled, oldSub.SubscriptionStatus)
	assert.NotNil(s.T(), oldSub.CancelledAt)
}

// TC-002: Downgrade from Pro Plan to Basic Plan
func (s *SubscriptionChangeServiceTestSuite) TestDowngradeProToBasic() {
	ctx := s.GetContext()

	// Create test data
	customer := s.createTestCustomer()
	basicPlan := s.createTestPlan("Basic", decimal.NewFromFloat(10.00))
	proPlan := s.createTestPlan("Pro", decimal.NewFromFloat(30.00))
	testSub := s.createTestSubscription(proPlan.ID, customer.ID)

	// Create downgrade request
	req := s.createSubscriptionChangeRequest(basicPlan.ID, types.ProrationBehaviorCreateProrations)

	// Test preview
	previewReq := req
	previewResponse, err := s.subscriptionChangeService.PreviewSubscriptionChange(ctx, testSub.ID, previewReq)

	// Assertions for preview
	require.NoError(s.T(), err)
	assert.NotNil(s.T(), previewResponse)
	assert.Equal(s.T(), types.SubscriptionChangeTypeDowngrade, previewResponse.ChangeType)
	assert.Contains(s.T(), previewResponse.Warnings, "This is a downgrade. You may lose access to certain features.")

	// Execute the change
	executeResponse, err := s.subscriptionChangeService.ExecuteSubscriptionChange(ctx, testSub.ID, req)

	// Assertions for execution
	require.NoError(s.T(), err)
	assert.NotNil(s.T(), executeResponse)
	assert.Equal(s.T(), types.SubscriptionChangeTypeDowngrade, executeResponse.ChangeType)
	assert.Equal(s.T(), basicPlan.ID, executeResponse.NewSubscription.PlanID)
}

// ========================================
// BILLING PERIOD CHANGE TEST CASES
// ========================================

// TC-005: Monthly to Yearly Plan Change
func (s *SubscriptionChangeServiceTestSuite) TestMonthlyToYearlyChange() {
	ctx := s.GetContext()

	// Create test data
	customer := s.createTestCustomer()
	monthlyPlan := s.createTestPlanWithBilling("Basic Monthly", decimal.NewFromFloat(10.00), types.BILLING_PERIOD_MONTHLY)
	yearlyPlan := s.createTestPlanWithBilling("Basic Yearly", decimal.NewFromFloat(100.00), types.BILLING_PERIOD_ANNUAL)
	testSub := s.createTestSubscriptionWithCycle(monthlyPlan.ID, customer.ID, types.BillingCycleAnniversary, types.BILLING_PERIOD_MONTHLY)

	// Create change request
	req := s.createSubscriptionChangeRequest(yearlyPlan.ID, types.ProrationBehaviorCreateProrations)

	// Test preview
	previewReq := req
	previewResponse, err := s.subscriptionChangeService.PreviewSubscriptionChange(ctx, testSub.ID, previewReq)

	// Assertions
	require.NoError(s.T(), err)
	assert.NotNil(s.T(), previewResponse)
	assert.Equal(s.T(), types.SubscriptionChangeTypeUpgrade, previewResponse.ChangeType)
	assert.NotNil(s.T(), previewResponse.ProrationDetails)

	// Execute the change
	executeResponse, err := s.subscriptionChangeService.ExecuteSubscriptionChange(ctx, testSub.ID, req)

	// Assertions
	require.NoError(s.T(), err)
	assert.NotNil(s.T(), executeResponse)
	assert.Equal(s.T(), yearlyPlan.ID, executeResponse.NewSubscription.PlanID)
	// Note: BillingPeriod is not exposed in SubscriptionSummary DTO, but we can verify the plan change
	assert.Equal(s.T(), yearlyPlan.ID, executeResponse.NewSubscription.PlanID)
}

// TC-006: Weekly to Monthly Plan Change
func (s *SubscriptionChangeServiceTestSuite) TestWeeklyToMonthlyChange() {
	ctx := s.GetContext()

	// Create test data
	customer := s.createTestCustomer()
	weeklyPlan := s.createTestPlanWithBilling("Pro Weekly", decimal.NewFromFloat(8.00), types.BILLING_PERIOD_WEEKLY)
	monthlyPlan := s.createTestPlanWithBilling("Pro Monthly", decimal.NewFromFloat(30.00), types.BILLING_PERIOD_MONTHLY)
	testSub := s.createTestSubscriptionWithCycle(weeklyPlan.ID, customer.ID, types.BillingCycleAnniversary, types.BILLING_PERIOD_WEEKLY)

	// Create change request
	req := s.createSubscriptionChangeRequest(monthlyPlan.ID, types.ProrationBehaviorCreateProrations)

	// Execute the change
	executeResponse, err := s.subscriptionChangeService.ExecuteSubscriptionChange(ctx, testSub.ID, req)

	// Assertions
	require.NoError(s.T(), err)
	assert.NotNil(s.T(), executeResponse)
	assert.Equal(s.T(), monthlyPlan.ID, executeResponse.NewSubscription.PlanID)
	// Note: BillingPeriod is not exposed in SubscriptionSummary DTO, but we can verify the plan change
	assert.Equal(s.T(), monthlyPlan.ID, executeResponse.NewSubscription.PlanID)
}

// ========================================
// PRORATION TEST CASES
// ========================================

// TC-008: Anniversary Billing Proration
func (s *SubscriptionChangeServiceTestSuite) TestAnniversaryBillingProration() {
	ctx := s.GetContext()

	// Create test data
	customer := s.createTestCustomer()
	basicPlan := s.createTestPlan("Basic", decimal.NewFromFloat(20.00))
	proPlan := s.createTestPlan("Pro", decimal.NewFromFloat(50.00))
	testSub := s.createTestSubscriptionWithCycle(basicPlan.ID, customer.ID, types.BillingCycleAnniversary, types.BILLING_PERIOD_MONTHLY)

	// Create change request
	req := s.createSubscriptionChangeRequest(proPlan.ID, types.ProrationBehaviorCreateProrations)

	// Test preview to verify proration calculation
	previewReq := req
	previewResponse, err := s.subscriptionChangeService.PreviewSubscriptionChange(ctx, testSub.ID, previewReq)

	// Assertions
	require.NoError(s.T(), err)
	assert.NotNil(s.T(), previewResponse)
	assert.NotNil(s.T(), previewResponse.ProrationDetails)
	assert.Equal(s.T(), types.BillingCycleAnniversary, testSub.BillingCycle)
}

// TC-009: Calendar Billing Proration
func (s *SubscriptionChangeServiceTestSuite) TestCalendarBillingProration() {
	ctx := s.GetContext()

	// Create test data
	customer := s.createTestCustomer()
	basicPlan := s.createTestPlan("Basic", decimal.NewFromFloat(30.00))
	proPlan := s.createTestPlan("Pro", decimal.NewFromFloat(60.00))
	testSub := s.createTestSubscriptionWithCycle(basicPlan.ID, customer.ID, types.BillingCycleCalendar, types.BILLING_PERIOD_MONTHLY)

	// Create change request
	req := s.createSubscriptionChangeRequest(proPlan.ID, types.ProrationBehaviorCreateProrations)

	// Test preview to verify proration calculation
	previewReq := req
	previewResponse, err := s.subscriptionChangeService.PreviewSubscriptionChange(ctx, testSub.ID, previewReq)

	// Assertions
	require.NoError(s.T(), err)
	assert.NotNil(s.T(), previewResponse)
	assert.NotNil(s.T(), previewResponse.ProrationDetails)
	assert.Equal(s.T(), types.BillingCycleCalendar, testSub.BillingCycle)
}

// ========================================
// ADVANCED TEST CASES
// ========================================

// TC-010: Mid-Period Upgrade with Usage Charges
func (s *SubscriptionChangeServiceTestSuite) TestMidPeriodUpgradeWithUsage() {
	ctx := s.GetContext()

	// Create test data
	customer := s.createTestCustomer()
	starterPlan, _ := s.createUsageBasedPlan("Starter", decimal.NewFromFloat(10.00), decimal.NewFromFloat(0.10))
	proPlan, _ := s.createUsageBasedPlan("Pro", decimal.NewFromFloat(30.00), decimal.NewFromFloat(0.05))
	testSub := s.createTestSubscription(starterPlan.ID, customer.ID)

	// Create change request
	req := s.createSubscriptionChangeRequest(proPlan.ID, types.ProrationBehaviorCreateProrations)

	// Execute the change
	executeResponse, err := s.subscriptionChangeService.ExecuteSubscriptionChange(ctx, testSub.ID, req)

	// Assertions
	require.NoError(s.T(), err)
	assert.NotNil(s.T(), executeResponse)
	assert.Equal(s.T(), proPlan.ID, executeResponse.NewSubscription.PlanID)
	assert.NotNil(s.T(), executeResponse.ProrationApplied)
}

// ========================================
// USAGE-BASED PRICING TEST CASES
// ========================================

// TC-011: Fixed Plan to Usage Plan Transition
func (s *SubscriptionChangeServiceTestSuite) TestFixedToUsagePlanTransition() {
	ctx := s.GetContext()

	// Create test data
	customer := s.createTestCustomer()
	fixedPlan := s.createTestPlan("Fixed Plan", decimal.NewFromFloat(50.00))
	usagePlan, _ := s.createUsageBasedPlan("Usage Plan", decimal.NewFromFloat(10.00), decimal.NewFromFloat(0.05))
	testSub := s.createTestSubscription(fixedPlan.ID, customer.ID)

	// Create change request
	req := s.createSubscriptionChangeRequest(usagePlan.ID, types.ProrationBehaviorCreateProrations)

	// Test preview
	previewResponse, err := s.subscriptionChangeService.PreviewSubscriptionChange(ctx, testSub.ID, req)
	require.NoError(s.T(), err)
	assert.NotNil(s.T(), previewResponse)
	assert.Equal(s.T(), types.SubscriptionChangeTypeDowngrade, previewResponse.ChangeType) // Assuming usage plans are considered downgrades from fixed high-value plans
	assert.NotNil(s.T(), previewResponse.ProrationDetails)

	// Execute the change
	executeResponse, err := s.subscriptionChangeService.ExecuteSubscriptionChange(ctx, testSub.ID, req)
	require.NoError(s.T(), err)
	assert.NotNil(s.T(), executeResponse)
	assert.Equal(s.T(), usagePlan.ID, executeResponse.NewSubscription.PlanID)
	assert.NotNil(s.T(), executeResponse.ProrationApplied)
}

// // TC-012: Usage Plan to Fixed Plan Transition
// func (s *SubscriptionChangeServiceTestSuite) TestUsageToFixedPlanTransition() {
// 	ctx := s.GetContext()

// 	// Create test data
// 	customer := s.createTestCustomer()
// 	usagePlan, _ := s.createUsageBasedPlan("Usage Plan", decimal.NewFromFloat(5.00), decimal.NewFromFloat(0.10))
// 	fixedPlan := s.createTestPlan("Premium Fixed", decimal.NewFromFloat(100.00))
// 	testSub := s.createTestSubscription(usagePlan.ID, customer.ID)

// 	// Create change request
// 	req := s.createSubscriptionChangeRequest(fixedPlan.ID, types.ProrationBehaviorCreateProrations)

// 	// Test preview
// 	previewResponse, err := s.subscriptionChangeService.PreviewSubscriptionChange(ctx, testSub.ID, req)
// 	require.NoError(s.T(), err)
// 	assert.NotNil(s.T(), previewResponse)
// 	assert.Equal(s.T(), types.SubscriptionChangeTypeUpgrade, previewResponse.ChangeType)
// 	assert.NotNil(s.T(), previewResponse.ProrationDetails)

// 	// Execute the change
// 	executeResponse, err := s.subscriptionChangeService.ExecuteSubscriptionChange(ctx, testSub.ID, req)
// 	require.NoError(s.T(), err)
// 	assert.NotNil(s.T(), executeResponse)
// 	assert.Equal(s.T(), fixedPlan.ID, executeResponse.NewSubscription.PlanID)
// }

// // TC-013: Usage-Only Plan (No Fixed Component)
// func (s *SubscriptionChangeServiceTestSuite) TestUsageOnlyPlanTransition() {
// 	ctx := s.GetContext()

// 	// Create test data
// 	customer := s.createTestCustomer()
// 	fixedPlan := s.createTestPlan("Fixed Plan", decimal.NewFromFloat(25.00))
// 	usageOnlyPlan, _ := s.createUsageBasedPlan("Usage Only", decimal.Zero, decimal.NewFromFloat(0.02)) // No fixed amount
// 	testSub := s.createTestSubscription(fixedPlan.ID, customer.ID)

// 	// Create change request
// 	req := s.createSubscriptionChangeRequest(usageOnlyPlan.ID, types.ProrationBehaviorCreateProrations)

// 	// Execute the change
// 	executeResponse, err := s.subscriptionChangeService.ExecuteSubscriptionChange(ctx, testSub.ID, req)
// 	require.NoError(s.T(), err)
// 	assert.NotNil(s.T(), executeResponse)
// 	assert.Equal(s.T(), usageOnlyPlan.ID, executeResponse.NewSubscription.PlanID)
// 	assert.NotNil(s.T(), executeResponse.ProrationApplied)
// }

// // TC-014: Different Usage Pricing Models Transition
// func (s *SubscriptionChangeServiceTestSuite) TestDifferentUsagePricingTransition() {
// 	ctx := s.GetContext()

// 	// Create test data
// 	customer := s.createTestCustomer()
// 	lowUsagePlan, _ := s.createUsageBasedPlan("Low Usage", decimal.NewFromFloat(10.00), decimal.NewFromFloat(0.10))
// 	highUsagePlan, _ := s.createUsageBasedPlan("High Usage", decimal.NewFromFloat(50.00), decimal.NewFromFloat(0.01))
// 	testSub := s.createTestSubscription(lowUsagePlan.ID, customer.ID)

// 	// Create change request
// 	req := s.createSubscriptionChangeRequest(highUsagePlan.ID, types.ProrationBehaviorCreateProrations)

// 	// Test preview
// 	previewResponse, err := s.subscriptionChangeService.PreviewSubscriptionChange(ctx, testSub.ID, req)
// 	require.NoError(s.T(), err)
// 	assert.NotNil(s.T(), previewResponse)
// 	assert.Equal(s.T(), types.SubscriptionChangeTypeUpgrade, previewResponse.ChangeType) // Higher fixed fee typically means upgrade
// 	assert.NotNil(s.T(), previewResponse.ProrationDetails)

// 	// Execute the change
// 	executeResponse, err := s.subscriptionChangeService.ExecuteSubscriptionChange(ctx, testSub.ID, req)
// 	require.NoError(s.T(), err)
// 	assert.NotNil(s.T(), executeResponse)
// 	assert.Equal(s.T(), highUsagePlan.ID, executeResponse.NewSubscription.PlanID)
// }

// // TC-015: Usage Plan with Multiple Meters
// func (s *SubscriptionChangeServiceTestSuite) TestUsagePlanWithMultipleMeters() {
// 	ctx := s.GetContext()

// 	// Create customer
// 	customer := s.createTestCustomer()

// 	// Create a simple fixed plan for comparison
// 	simplePlan := s.createTestPlan("Simple Plan", decimal.NewFromFloat(20.00))

// 	// Create a complex usage plan with multiple meters using the new helper
// 	meterSpecs := []struct {
// 		name        string
// 		eventName   string
// 		amount      decimal.Decimal
// 		aggregation types.AggregationType
// 	}{
// 		{"API Calls", "api_call", decimal.NewFromFloat(0.01), types.AggregationCount},
// 		{"Data Transfer", "data_transfer", decimal.NewFromFloat(0.05), types.AggregationSum},
// 		{"Storage Usage", "storage_usage", decimal.NewFromFloat(0.10), types.AggregationMax},
// 	}

// 	complexPlan, createdMeters := s.createMultiMeterUsagePlan("Complex Multi-Meter Plan", decimal.NewFromFloat(50.00), meterSpecs)

// 	// Create subscription with simple plan first
// 	testSub := s.createTestSubscription(simplePlan.ID, customer.ID)

// 	// Create change request to complex usage plan
// 	req := s.createSubscriptionChangeRequest(complexPlan.ID, types.ProrationBehaviorCreateProrations)

// 	// Execute the change
// 	executeResponse, err := s.subscriptionChangeService.ExecuteSubscriptionChange(ctx, testSub.ID, req)
// 	require.NoError(s.T(), err)
// 	assert.NotNil(s.T(), executeResponse)
// 	assert.Equal(s.T(), complexPlan.ID, executeResponse.NewSubscription.PlanID)

// 	// Verify the new subscription has the complex pricing structure
// 	newSub, lineItems, err := s.GetStores().SubscriptionRepo.GetWithLineItems(ctx, executeResponse.NewSubscription.ID)
// 	require.NoError(s.T(), err)
// 	assert.NotNil(s.T(), newSub)
// 	assert.True(s.T(), len(lineItems) >= 4) // Should have fixed + 3 usage line items
// 	assert.Len(s.T(), createdMeters, 3)     // Verify we created the expected number of meters

// 	// Verify that each meter has different aggregation types
// 	aggregationTypes := make(map[types.AggregationType]bool)
// 	for _, meter := range createdMeters {
// 		aggregationTypes[meter.Aggregation.Type] = true
// 	}
// 	assert.Contains(s.T(), aggregationTypes, types.AggregationCount)
// 	assert.Contains(s.T(), aggregationTypes, types.AggregationSum)
// 	assert.Contains(s.T(), aggregationTypes, types.AggregationMax)
// }

// // TC-016: Usage Plan Billing Period Changes
// func (s *SubscriptionChangeServiceTestSuite) TestUsagePlanBillingPeriodChange() {
// 	ctx := s.GetContext()

// 	// Create test data
// 	customer := s.createTestCustomer()

// 	// Create monthly usage plan
// 	monthlyUsagePlan, _ := s.createUsageBasedPlan("Monthly Usage", decimal.NewFromFloat(15.00), decimal.NewFromFloat(0.08))

// 	// Create annual usage plan
// 	annualUsagePlan := s.createTestPlanWithBilling("Annual Usage", decimal.NewFromFloat(150.00), types.BILLING_PERIOD_ANNUAL)

// 	testSub := s.createTestSubscription(monthlyUsagePlan.ID, customer.ID)

// 	// Create change request
// 	req := s.createSubscriptionChangeRequest(annualUsagePlan.ID, types.ProrationBehaviorCreateProrations)

// 	// Execute the change
// 	executeResponse, err := s.subscriptionChangeService.ExecuteSubscriptionChange(ctx, testSub.ID, req)
// 	require.NoError(s.T(), err)
// 	assert.NotNil(s.T(), executeResponse)
// 	assert.Equal(s.T(), annualUsagePlan.ID, executeResponse.NewSubscription.PlanID)
// 	assert.NotNil(s.T(), executeResponse.ProrationApplied)
// }

// // TC-017: Usage Plan Without Proration
// func (s *SubscriptionChangeServiceTestSuite) TestUsagePlanChangeWithoutProration() {
// 	ctx := s.GetContext()

// 	// Create test data
// 	customer := s.createTestCustomer()
// 	basicUsagePlan, _ := s.createUsageBasedPlan("Basic Usage", decimal.NewFromFloat(10.00), decimal.NewFromFloat(0.05))
// 	premiumUsagePlan, _ := s.createUsageBasedPlan("Premium Usage", decimal.NewFromFloat(30.00), decimal.NewFromFloat(0.03))
// 	testSub := s.createTestSubscription(basicUsagePlan.ID, customer.ID)

// 	// Create change request without proration
// 	req := s.createSubscriptionChangeRequest(premiumUsagePlan.ID, types.ProrationBehaviorNone)

// 	// Execute the change
// 	executeResponse, err := s.subscriptionChangeService.ExecuteSubscriptionChange(ctx, testSub.ID, req)
// 	require.NoError(s.T(), err)
// 	assert.NotNil(s.T(), executeResponse)
// 	assert.Equal(s.T(), premiumUsagePlan.ID, executeResponse.NewSubscription.PlanID)
// 	assert.Nil(s.T(), executeResponse.ProrationApplied) // No proration should be applied
// }

// // TC-018: Complex Usage Scenario with Edge Cases
// func (s *SubscriptionChangeServiceTestSuite) TestComplexUsageScenarioEdgeCases() {
// 	ctx := s.GetContext()

// 	// Create test data
// 	customer := s.createTestCustomer()

// 	// Create plan with zero fixed cost but high usage cost
// 	highUsagePlan, _ := s.createUsageBasedPlan("High Per-Unit", decimal.Zero, decimal.NewFromFloat(1.00))

// 	// Create plan with high fixed cost but low usage cost
// 	lowUsagePlan, _ := s.createUsageBasedPlan("Low Per-Unit", decimal.NewFromFloat(100.00), decimal.NewFromFloat(0.001))

// 	testSub := s.createTestSubscription(highUsagePlan.ID, customer.ID)

// 	// Test transition from high per-unit to low per-unit pricing
// 	req := s.createSubscriptionChangeRequest(lowUsagePlan.ID, types.ProrationBehaviorCreateProrations)

// 	// Test preview
// 	previewResponse, err := s.subscriptionChangeService.PreviewSubscriptionChange(ctx, testSub.ID, req)
// 	require.NoError(s.T(), err)
// 	assert.NotNil(s.T(), previewResponse)
// 	// This should be considered an upgrade due to higher fixed costs
// 	assert.Equal(s.T(), types.SubscriptionChangeTypeUpgrade, previewResponse.ChangeType)

// 	// Execute the change
// 	executeResponse, err := s.subscriptionChangeService.ExecuteSubscriptionChange(ctx, testSub.ID, req)
// 	require.NoError(s.T(), err)
// 	assert.NotNil(s.T(), executeResponse)
// 	assert.Equal(s.T(), lowUsagePlan.ID, executeResponse.NewSubscription.PlanID)
// }

// // ========================================
// // VALIDATION TEST CASES
// // ========================================

// // TC-021: Invalid Plan Transition
// func (s *SubscriptionChangeServiceTestSuite) TestInvalidPlanTransition() {
// 	ctx := s.GetContext()

// 	// Create test data
// 	customer := s.createTestCustomer()
// 	basicPlan := s.createTestPlan("Basic", decimal.NewFromFloat(10.00))
// 	testSub := s.createTestSubscription(basicPlan.ID, customer.ID)

// 	// Try to change to the same plan
// 	req := s.createSubscriptionChangeRequest(basicPlan.ID, types.ProrationBehaviorCreateProrations)

// 	// This should fail
// 	_, err := s.subscriptionChangeService.ExecuteSubscriptionChange(ctx, testSub.ID, req)
// 	assert.Error(s.T(), err)
// 	assert.Contains(s.T(), err.Error(), "cannot change subscription to the same plan")
// }

// // TC-022: Cancelled Subscription Change Attempt
// func (s *SubscriptionChangeServiceTestSuite) TestCancelledSubscriptionChangeAttempt() {
// 	ctx := s.GetContext()

// 	// Create test data
// 	customer := s.createTestCustomer()
// 	basicPlan := s.createTestPlan("Basic", decimal.NewFromFloat(10.00))
// 	proPlan := s.createTestPlan("Pro", decimal.NewFromFloat(30.00))
// 	testSub := s.createTestSubscription(basicPlan.ID, customer.ID)

// 	// Cancel the subscription first
// 	testSub.SubscriptionStatus = types.SubscriptionStatusCancelled
// 	now := time.Now().UTC()
// 	testSub.CancelledAt = &now
// 	err := s.GetStores().SubscriptionRepo.Update(ctx, testSub)
// 	require.NoError(s.T(), err)

// 	// Try to change the cancelled subscription
// 	req := s.createSubscriptionChangeRequest(proPlan.ID, types.ProrationBehaviorCreateProrations)

// 	// This should fail
// 	_, err = s.subscriptionChangeService.ExecuteSubscriptionChange(ctx, testSub.ID, req)
// 	assert.Error(s.T(), err)
// }

// // ========================================
// // EDGE CASES
// // ========================================

// // TC-023: No Proration Behavior
// func (s *SubscriptionChangeServiceTestSuite) TestNoProrationBehavior() {
// 	ctx := s.GetContext()

// 	// Create test data
// 	customer := s.createTestCustomer()
// 	basicPlan := s.createTestPlan("Basic", decimal.NewFromFloat(10.00))
// 	proPlan := s.createTestPlan("Pro", decimal.NewFromFloat(30.00))
// 	testSub := s.createTestSubscription(basicPlan.ID, customer.ID)

// 	// Create change request without proration
// 	req := s.createSubscriptionChangeRequest(proPlan.ID, types.ProrationBehaviorNone)

// 	// Execute the change
// 	executeResponse, err := s.subscriptionChangeService.ExecuteSubscriptionChange(ctx, testSub.ID, req)

// 	// Assertions
// 	require.NoError(s.T(), err)
// 	assert.NotNil(s.T(), executeResponse)
// 	assert.Equal(s.T(), proPlan.ID, executeResponse.NewSubscription.PlanID)
// 	// No proration should be applied
// 	assert.Nil(s.T(), executeResponse.ProrationApplied)
// }

// // TC-024: Lateral Plan Change
// func (s *SubscriptionChangeServiceTestSuite) TestLateralPlanChange() {
// 	ctx := s.GetContext()

// 	// Create test data
// 	customer := s.createTestCustomer()
// 	plan1 := s.createTestPlan("Plan A", decimal.NewFromFloat(15.00))
// 	plan2 := s.createTestPlan("Plan B", decimal.NewFromFloat(15.00))
// 	testSub := s.createTestSubscription(plan1.ID, customer.ID)

// 	// Create change request
// 	req := s.createSubscriptionChangeRequest(plan2.ID, types.ProrationBehaviorCreateProrations)

// 	// Test preview
// 	previewReq := req
// 	previewResponse, err := s.subscriptionChangeService.PreviewSubscriptionChange(ctx, testSub.ID, previewReq)

// 	// Assertions
// 	require.NoError(s.T(), err)
// 	assert.NotNil(s.T(), previewResponse)
// 	assert.Equal(s.T(), types.SubscriptionChangeTypeLateral, previewResponse.ChangeType)
// }

// // ========================================
// // HELPER METHOD TESTS
// // ========================================

// // Test the determine change type functionality
// func (s *SubscriptionChangeServiceTestSuite) TestDetermineChangeType() {
// 	ctx := s.GetContext()

// 	// Create test plans with different prices
// 	basicPlan := s.createTestPlan("Basic", decimal.NewFromFloat(10.00))
// 	proPlan := s.createTestPlan("Pro", decimal.NewFromFloat(30.00))
// 	enterprisePlan := s.createTestPlan("Enterprise", decimal.NewFromFloat(100.00))
// 	samePricePlan := s.createTestPlan("Alternative", decimal.NewFromFloat(10.00))

// 	service := s.subscriptionChangeService

// 	// Test upgrade
// 	changeType, err := service.determineChangeType(ctx, basicPlan, proPlan)
// 	require.NoError(s.T(), err)
// 	assert.Equal(s.T(), types.SubscriptionChangeTypeUpgrade, changeType)

// 	// Test major upgrade
// 	changeType, err = service.determineChangeType(ctx, basicPlan, enterprisePlan)
// 	require.NoError(s.T(), err)
// 	assert.Equal(s.T(), types.SubscriptionChangeTypeUpgrade, changeType)

// 	// Test downgrade
// 	changeType, err = service.determineChangeType(ctx, proPlan, basicPlan)
// 	require.NoError(s.T(), err)
// 	assert.Equal(s.T(), types.SubscriptionChangeTypeDowngrade, changeType)

// 	// Test lateral change
// 	changeType, err = service.determineChangeType(ctx, basicPlan, samePricePlan)
// 	require.NoError(s.T(), err)
// 	assert.Equal(s.T(), types.SubscriptionChangeTypeLateral, changeType)
// }

// // Test subscription validation
// func (s *SubscriptionChangeServiceTestSuite) TestValidateSubscriptionForChange() {
// 	_ = s.GetContext()

// 	// Create test data
// 	customer := s.createTestCustomer()
// 	basicPlan := s.createTestPlan("Basic", decimal.NewFromFloat(10.00))
// 	testSub := s.createTestSubscription(basicPlan.ID, customer.ID)

// 	service := s.subscriptionChangeService

// 	// Test with active subscription (should pass)
// 	testSub.SubscriptionStatus = types.SubscriptionStatusActive
// 	err := service.validateSubscriptionForChange(testSub)
// 	assert.NoError(s.T(), err)

// 	// Test with cancelled subscription (should fail)
// 	testSub.SubscriptionStatus = types.SubscriptionStatusCancelled
// 	err = service.validateSubscriptionForChange(testSub)
// 	assert.Error(s.T(), err)
// }

// // ========================================
// // PERFORMANCE TEST CASES
// // ========================================

// // TC-025: Multiple Subscription Changes
// func (s *SubscriptionChangeServiceTestSuite) TestMultipleSubscriptionChanges() {
// 	ctx := s.GetContext()

// 	// Create test data
// 	customer := s.createTestCustomer()
// 	basicPlan := s.createTestPlan("Basic", decimal.NewFromFloat(10.00))
// 	proPlan := s.createTestPlan("Pro", decimal.NewFromFloat(30.00))
// 	enterprisePlan := s.createTestPlan("Enterprise", decimal.NewFromFloat(100.00))

// 	// Create multiple subscriptions
// 	subscriptions := make([]*subscription.Subscription, 5)
// 	for i := 0; i < 5; i++ {
// 		subscriptions[i] = s.createTestSubscription(basicPlan.ID, customer.ID)
// 	}

// 	// Perform changes on all subscriptions
// 	for i, sub := range subscriptions {
// 		targetPlan := proPlan
// 		if i%2 == 0 {
// 			targetPlan = enterprisePlan
// 		}

// 		req := s.createSubscriptionChangeRequest(targetPlan.ID, types.ProrationBehaviorCreateProrations)

// 		// Execute the change
// 		executeResponse, err := s.subscriptionChangeService.ExecuteSubscriptionChange(ctx, sub.ID, req)

// 		// Assertions
// 		require.NoError(s.T(), err)
// 		assert.NotNil(s.T(), executeResponse)
// 		assert.Equal(s.T(), targetPlan.ID, executeResponse.NewSubscription.PlanID)
// 	}
// }

// func TestSubscriptionChangeServiceTestSuite(t *testing.T) {
// 	suite.Run(t, new(SubscriptionChangeServiceTestSuite))
// }
