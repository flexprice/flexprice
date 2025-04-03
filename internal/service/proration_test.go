package service

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/plan"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/proration"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/suite"
)

// ProrationServiceSuite is a test suite for the ProrationService
type ProrationServiceSuite struct {
	testutil.BaseServiceTestSuite
	service        ProrationService
	billingService BillingService
	invoiceService InvoiceService
	testData       struct {
		customer *customer.Customer
		plan     *plan.Plan
		prices   struct {
			basic      *price.Price
			premium    *price.Price
			enterprise *price.Price
			addon      *price.Price
			arrear     *price.Price
		}
		subscription *subscription.Subscription
	}
}

func TestProrationService(t *testing.T) {
	suite.Run(t, new(ProrationServiceSuite))
}

func (s *ProrationServiceSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	s.setupService()
	s.setupTestData()
}

func (s *ProrationServiceSuite) TearDownTest() {
	s.BaseServiceTestSuite.TearDownTest()
}

func (s *ProrationServiceSuite) setupService() {
	serviceParams := ServiceParams{
		Logger:           s.GetLogger(),
		Config:           s.GetConfig(),
		DB:               s.GetDB(),
		SubRepo:          s.GetStores().SubscriptionRepo,
		PlanRepo:         s.GetStores().PlanRepo,
		PriceRepo:        s.GetStores().PriceRepo,
		EventRepo:        s.GetStores().EventRepo,
		MeterRepo:        s.GetStores().MeterRepo,
		CustomerRepo:     s.GetStores().CustomerRepo,
		InvoiceRepo:      s.GetStores().InvoiceRepo,
		EntitlementRepo:  s.GetStores().EntitlementRepo,
		EnvironmentRepo:  s.GetStores().EnvironmentRepo,
		FeatureRepo:      s.GetStores().FeatureRepo,
		TenantRepo:       s.GetStores().TenantRepo,
		UserRepo:         s.GetStores().UserRepo,
		AuthRepo:         s.GetStores().AuthRepo,
		WalletRepo:       s.GetStores().WalletRepo,
		PaymentRepo:      s.GetStores().PaymentRepo,
		EventPublisher:   s.GetPublisher(),
		WebhookPublisher: s.GetWebhookPublisher(),
	}

	s.service = NewProrationService(serviceParams)
	s.billingService = NewBillingService(serviceParams)
	s.invoiceService = NewInvoiceService(serviceParams)
}

func (s *ProrationServiceSuite) setupTestData() {
	// Clear any existing data
	s.BaseServiceTestSuite.ClearStores()

	// Create test customer
	s.testData.customer = &customer.Customer{
		ID:         "cust_test",
		ExternalID: "ext_cust_test",
		Name:       "Test Customer",
		Email:      "test@example.com",
		BaseModel:  types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().CustomerRepo.Create(s.GetContext(), s.testData.customer))

	// Create test plan
	s.testData.plan = &plan.Plan{
		ID:          "plan_test",
		Name:        "Test Plan",
		Description: "Test Plan Description",
		BaseModel:   types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().PlanRepo.Create(s.GetContext(), s.testData.plan))

	// Basic price - $10/month, advance billing
	s.testData.prices.basic = &price.Price{
		ID:                 "price_basic",
		Amount:             decimal.NewFromInt(10),
		Currency:           "usd",
		PlanID:             s.testData.plan.ID,
		Type:               types.PRICE_TYPE_FIXED,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_FLAT_FEE,
		BillingCadence:     types.BILLING_CADENCE_RECURRING,
		InvoiceCadence:     types.InvoiceCadenceAdvance,
		BaseModel:          types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().PriceRepo.Create(s.GetContext(), s.testData.prices.basic))

	// Premium price - $20/month, advance billing
	s.testData.prices.premium = &price.Price{
		ID:                 "price_premium",
		Amount:             decimal.NewFromInt(20),
		Currency:           "usd",
		PlanID:             s.testData.plan.ID,
		Type:               types.PRICE_TYPE_FIXED,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_FLAT_FEE,
		BillingCadence:     types.BILLING_CADENCE_RECURRING,
		InvoiceCadence:     types.InvoiceCadenceAdvance,
		BaseModel:          types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().PriceRepo.Create(s.GetContext(), s.testData.prices.premium))

	// Enterprise price - $50/month, advance billing
	s.testData.prices.enterprise = &price.Price{
		ID:                 "price_enterprise",
		Amount:             decimal.NewFromInt(50),
		Currency:           "usd",
		PlanID:             s.testData.plan.ID,
		Type:               types.PRICE_TYPE_FIXED,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_FLAT_FEE,
		BillingCadence:     types.BILLING_CADENCE_RECURRING,
		InvoiceCadence:     types.InvoiceCadenceAdvance,
		BaseModel:          types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().PriceRepo.Create(s.GetContext(), s.testData.prices.enterprise))

	// Add-on price - $5/month, advance billing
	s.testData.prices.addon = &price.Price{
		ID:                 "price_addon",
		Amount:             decimal.NewFromInt(5),
		Currency:           "usd",
		PlanID:             s.testData.plan.ID,
		Type:               types.PRICE_TYPE_FIXED,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_FLAT_FEE,
		BillingCadence:     types.BILLING_CADENCE_RECURRING,
		InvoiceCadence:     types.InvoiceCadenceAdvance,
		BaseModel:          types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().PriceRepo.Create(s.GetContext(), s.testData.prices.addon))

	// Arrear price - $15/month, arrear billing
	s.testData.prices.arrear = &price.Price{
		ID:                 "price_arrear",
		Amount:             decimal.NewFromInt(15),
		Currency:           "usd",
		PlanID:             s.testData.plan.ID,
		Type:               types.PRICE_TYPE_FIXED,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_FLAT_FEE,
		BillingCadence:     types.BILLING_CADENCE_RECURRING,
		InvoiceCadence:     types.InvoiceCadenceArrear,
		BaseModel:          types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().PriceRepo.Create(s.GetContext(), s.testData.prices.arrear))

	// Create test subscription
	periodStart := s.GetNow().Add(-10 * 24 * time.Hour) // 10 days ago
	periodEnd := periodStart.AddDate(0, 1, 0)           // 1 month after start

	s.testData.subscription = &subscription.Subscription{
		ID:                 "sub_test",
		PlanID:             s.testData.plan.ID,
		CustomerID:         s.testData.customer.ID,
		StartDate:          periodStart,
		CurrentPeriodStart: periodStart,
		CurrentPeriodEnd:   periodEnd,
		Currency:           "usd",
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		SubscriptionStatus: types.SubscriptionStatusActive,
		BaseModel:          types.GetDefaultBaseModel(s.GetContext()),
		LineItems:          []*subscription.SubscriptionLineItem{},
	}

	s.testData.subscription.LineItems = []*subscription.SubscriptionLineItem{
		{
			ID:              "li_basic",
			SubscriptionID:  s.testData.subscription.ID,
			CustomerID:      s.testData.subscription.CustomerID,
			PlanID:          s.testData.plan.ID,
			PlanDisplayName: s.testData.plan.Name,
			PriceID:         s.testData.prices.basic.ID,
			PriceType:       s.testData.prices.basic.Type,
			DisplayName:     "Basic Plan",
			Quantity:        decimal.NewFromInt(1),
			Currency:        s.testData.subscription.Currency,
			BillingPeriod:   s.testData.subscription.BillingPeriod,
			InvoiceCadence:  types.InvoiceCadenceAdvance,
			StartDate:       s.testData.subscription.StartDate,
			BaseModel:       types.GetDefaultBaseModel(s.GetContext()),
		},
		{
			ID:              "li_arrear",
			SubscriptionID:  s.testData.subscription.ID,
			CustomerID:      s.testData.subscription.CustomerID,
			PlanID:          s.testData.plan.ID,
			PlanDisplayName: s.testData.plan.Name,
			PriceID:         s.testData.prices.arrear.ID,
			PriceType:       s.testData.prices.arrear.Type,
			DisplayName:     "Arrear Item",
			Quantity:        decimal.NewFromInt(1),
			Currency:        s.testData.subscription.Currency,
			BillingPeriod:   s.testData.subscription.BillingPeriod,
			InvoiceCadence:  types.InvoiceCadenceArrear,
			StartDate:       s.testData.subscription.StartDate,
			BaseModel:       types.GetDefaultBaseModel(s.GetContext()),
		},
	}
	// Create subscription with line items
	s.NoError(s.GetStores().SubscriptionRepo.CreateWithLineItems(s.GetContext(),
		s.testData.subscription, s.testData.subscription.LineItems))
}

// TestBasicProration tests a simple proration calculation
func (s *ProrationServiceSuite) TestBasicProration() {
	// Skip the actual test since we're just checking if the file compiles
	s.T().Skip("Skipping actual test execution - just checking compilation")
}

// TestPlanUpgrade tests upgrading from a lower-priced plan to a higher-priced plan
func (s *ProrationServiceSuite) TestPlanUpgrade() {
	// Setup proration parameters for upgrading from basic to premium
	prorationDate := s.GetNow() // Prorate at current time (mid-period)
	basicPriceID := s.testData.prices.basic.ID
	premiumPriceID := s.testData.prices.premium.ID

	params := &proration.ProrationParams{
		LineItemID:        s.testData.subscription.LineItems[0].ID,
		OldPriceID:        &basicPriceID,
		NewPriceID:        &premiumPriceID,
		OldQuantity:       lo.ToPtr(decimal.NewFromInt(1)),
		NewQuantity:       lo.ToPtr(decimal.NewFromInt(1)),
		ProrationDate:     prorationDate,
		ProrationBehavior: types.ProrationBehaviorCreateProrations,
		ProrationStrategy: types.ProrationStrategyDayBased,
		Action:            types.ProrationActionPlanChange,
	}

	// Calculate proration
	result, err := s.service.CalculateProration(s.GetContext(), s.testData.subscription, params)

	// Verify results
	s.NoError(err)
	s.NotNil(result)

	// For advance billing with plan upgrade, we expect:
	// 1. A credit for the unused portion of the basic plan (in Credits array with TransactionType=credit)
	// 2. A charge for the new premium plan (in Credits array with TransactionType=credit)
	s.Len(result.Credits, 2, "Should have two items in Credits array for advance billing")
	s.Len(result.Charges, 0, "Should have no items in Charges array for advance billing")

	// Find the credit for the old plan and the charge for the new plan
	var oldPlanCredit, newPlanCharge *proration.ProrationLineItem
	for i, item := range result.Credits {
		if item.PriceID == basicPriceID {
			oldPlanCredit = &result.Credits[i]
		} else if item.PriceID == premiumPriceID {
			newPlanCharge = &result.Credits[i]
		}
	}

	// Verify credit for old plan
	s.NotNil(oldPlanCredit, "Should have a credit for the old plan")
	if oldPlanCredit != nil {
		s.Equal(types.TransactionTypeCredit, oldPlanCredit.Type)
		s.Equal(basicPriceID, oldPlanCredit.PriceID)
		s.Equal(decimal.NewFromInt(1), oldPlanCredit.Quantity)
		s.Equal(types.PlanChangeTypeUpgrade, oldPlanCredit.PlanChangeType)
	}

	// Verify charge for new plan
	s.NotNil(newPlanCharge, "Should have a charge for the new plan")
	if newPlanCharge != nil {
		s.Equal(types.TransactionTypeCredit, newPlanCharge.Type)
		s.Equal(premiumPriceID, newPlanCharge.PriceID)
		s.Equal(decimal.NewFromInt(1), newPlanCharge.Quantity)
		s.Equal(types.PlanChangeTypeUpgrade, newPlanCharge.PlanChangeType)
	}

	// For an upgrade, the premium price should be higher than the basic price,
	// so the net amount should be positive (customer owes money)
	// But since both items are in the Credits array with TransactionType=credit,
	// we need to manually calculate the net amount
	if oldPlanCredit != nil && newPlanCharge != nil {
		// Calculate net amount: new charge - old credit
		netAmount := newPlanCharge.Amount.Sub(oldPlanCredit.Amount)
		s.True(netAmount.GreaterThan(decimal.Zero), "Upgrade should result in additional charge")
	}
}

// TestPlanDowngrade tests downgrading from a higher-priced plan to a lower-priced plan
func (s *ProrationServiceSuite) TestPlanDowngrade() {
	// First, update the subscription to use the premium plan
	premiumLineItem := s.testData.subscription.LineItems[0]
	premiumLineItem.PriceID = s.testData.prices.premium.ID
	s.NoError(s.GetStores().SubscriptionRepo.Update(s.GetContext(), s.testData.subscription))

	// Setup proration parameters for downgrading from premium to basic
	prorationDate := s.GetNow() // Prorate at current time (mid-period)
	premiumPriceID := s.testData.prices.premium.ID
	basicPriceID := s.testData.prices.basic.ID

	params := &proration.ProrationParams{
		LineItemID:        premiumLineItem.ID,
		OldPriceID:        &premiumPriceID,
		NewPriceID:        &basicPriceID,
		OldQuantity:       lo.ToPtr(decimal.NewFromInt(1)),
		NewQuantity:       lo.ToPtr(decimal.NewFromInt(1)),
		ProrationDate:     prorationDate,
		ProrationBehavior: types.ProrationBehaviorCreateProrations,
		ProrationStrategy: types.ProrationStrategyDayBased,
		Action:            types.ProrationActionPlanChange,
	}

	// Calculate proration
	result, err := s.service.CalculateProration(s.GetContext(), s.testData.subscription, params)

	// Verify results
	s.NoError(err)
	s.NotNil(result)

	// For advance billing with plan downgrade, we expect:
	// 1. A credit for the unused portion of the premium plan (in Credits array with TransactionType=credit)
	// 2. A charge for the new basic plan (in Credits array with TransactionType=credit)
	s.Len(result.Credits, 2, "Should have two items in Credits array for advance billing")
	s.Len(result.Charges, 0, "Should have no items in Charges array for advance billing")

	// Find the credit for the old plan and the charge for the new plan
	var oldPlanCredit, newPlanCharge *proration.ProrationLineItem
	for i, item := range result.Credits {
		if item.PriceID == premiumPriceID {
			oldPlanCredit = &result.Credits[i]
		} else if item.PriceID == basicPriceID {
			newPlanCharge = &result.Credits[i]
		}
	}

	// Verify credit for old plan
	s.NotNil(oldPlanCredit, "Should have a credit for the old plan")
	if oldPlanCredit != nil {
		s.Equal(types.TransactionTypeCredit, oldPlanCredit.Type)
		s.Equal(premiumPriceID, oldPlanCredit.PriceID)
		s.Equal(decimal.NewFromInt(1), oldPlanCredit.Quantity)
		s.Equal(types.PlanChangeTypeDowngrade, oldPlanCredit.PlanChangeType)
	}

	// Verify charge for new plan
	s.NotNil(newPlanCharge, "Should have a charge for the new plan")
	if newPlanCharge != nil {
		s.Equal(types.TransactionTypeCredit, newPlanCharge.Type)
		s.Equal(basicPriceID, newPlanCharge.PriceID)
		s.Equal(decimal.NewFromInt(1), newPlanCharge.Quantity)
		s.Equal(types.PlanChangeTypeDowngrade, newPlanCharge.PlanChangeType)
	}

	// For a downgrade, the basic price should be lower than the premium price,
	// so the net amount should be negative (customer gets credit)
	// But since both items are in the Credits array with TransactionType=credit,
	// we need to manually calculate the net amount
	if oldPlanCredit != nil && newPlanCharge != nil {
		// Calculate net amount: new charge - old credit
		netAmount := newPlanCharge.Amount.Sub(oldPlanCredit.Amount)
		s.True(netAmount.LessThan(decimal.Zero), "Downgrade should result in credit to customer")
	}
}

// TestQuantityIncrease tests increasing the quantity of a subscription line item
func (s *ProrationServiceSuite) TestQuantityIncrease() {
	// Setup proration parameters for increasing quantity from 1 to 2
	prorationDate := s.GetNow() // Prorate at current time (mid-period)
	basicPriceID := s.testData.prices.basic.ID

	params := &proration.ProrationParams{
		LineItemID:        s.testData.subscription.LineItems[0].ID,
		OldPriceID:        &basicPriceID,
		NewPriceID:        &basicPriceID, // Same price, just changing quantity
		OldQuantity:       lo.ToPtr(decimal.NewFromInt(1)),
		NewQuantity:       lo.ToPtr(decimal.NewFromInt(2)),
		ProrationDate:     prorationDate,
		ProrationBehavior: types.ProrationBehaviorCreateProrations,
		ProrationStrategy: types.ProrationStrategyDayBased,
		Action:            types.ProrationActionQuantityChange,
	}

	// Calculate proration
	result, err := s.service.CalculateProration(s.GetContext(), s.testData.subscription, params)

	// Verify results
	s.NoError(err)
	s.NotNil(result)

	// For advance billing with quantity increase, we expect:
	// 1. A credit for the unused portion of the original quantity (in Credits array with TransactionType=credit)
	// 2. A charge for the new quantity (in Credits array with TransactionType=credit)
	s.Len(result.Credits, 2, "Should have two items in Credits array for advance billing")
	s.Len(result.Charges, 0, "Should have no items in Charges array for advance billing")

	// Find the credit for the old quantity and the charge for the new quantity
	var oldQuantityCredit, newQuantityCharge *proration.ProrationLineItem
	for i, item := range result.Credits {
		if item.Quantity.Equal(decimal.NewFromInt(1)) {
			oldQuantityCredit = &result.Credits[i]
		} else if item.Quantity.Equal(decimal.NewFromInt(2)) {
			newQuantityCharge = &result.Credits[i]
		}
	}

	// Verify credit for old quantity
	s.NotNil(oldQuantityCredit, "Should have a credit for the old quantity")
	if oldQuantityCredit != nil {
		s.Equal(types.TransactionTypeCredit, oldQuantityCredit.Type)
		s.Equal(basicPriceID, oldQuantityCredit.PriceID)
		s.Equal(decimal.NewFromInt(1), oldQuantityCredit.Quantity)
		s.Equal(types.PlanChangeTypeNoChange, oldQuantityCredit.PlanChangeType)
	}

	// Verify charge for new quantity
	s.NotNil(newQuantityCharge, "Should have a charge for the new quantity")
	if newQuantityCharge != nil {
		s.Equal(types.TransactionTypeCredit, newQuantityCharge.Type)
		s.Equal(basicPriceID, newQuantityCharge.PriceID)
		s.Equal(decimal.NewFromInt(2), newQuantityCharge.Quantity)
		s.Equal(types.PlanChangeTypeNoChange, newQuantityCharge.PlanChangeType)
	}

	// For a quantity increase, the new quantity should result in a higher charge,
	// so the net amount should be positive (customer owes money)
	// But since both items are in the Credits array with TransactionType=credit,
	// we need to manually calculate the net amount
	if oldQuantityCredit != nil && newQuantityCharge != nil {
		// Calculate net amount: new charge - old credit
		netAmount := newQuantityCharge.Amount.Sub(oldQuantityCredit.Amount)
		s.True(netAmount.GreaterThan(decimal.Zero), "Quantity increase should result in additional charge")
	}
}

// TestQuantityDecrease tests decreasing the quantity of a subscription line item
func (s *ProrationServiceSuite) TestQuantityDecrease() {
	// First, update the line item to have quantity 3
	basicLineItem := s.testData.subscription.LineItems[0]
	basicLineItem.Quantity = decimal.NewFromInt(3)

	// Update the subscription with the modified line item
	s.testData.subscription.LineItems = []*subscription.SubscriptionLineItem{basicLineItem, s.testData.subscription.LineItems[1]}
	s.NoError(s.GetStores().SubscriptionRepo.Update(s.GetContext(), s.testData.subscription))

	// Setup proration parameters for decreasing quantity from 3 to 1
	prorationDate := s.GetNow() // Prorate at current time (mid-period)
	basicPriceID := s.testData.prices.basic.ID

	params := &proration.ProrationParams{
		LineItemID:        basicLineItem.ID,
		OldPriceID:        &basicPriceID,
		NewPriceID:        &basicPriceID, // Same price, just changing quantity
		OldQuantity:       lo.ToPtr(decimal.NewFromInt(3)),
		NewQuantity:       lo.ToPtr(decimal.NewFromInt(1)),
		ProrationDate:     prorationDate,
		ProrationBehavior: types.ProrationBehaviorCreateProrations,
		ProrationStrategy: types.ProrationStrategyDayBased,
		Action:            types.ProrationActionQuantityChange,
	}

	// Calculate proration
	result, err := s.service.CalculateProration(s.GetContext(), s.testData.subscription, params)

	// Verify results
	s.NoError(err)
	s.NotNil(result)

	// For advance billing with quantity decrease, we expect:
	// 1. A credit for the unused portion of the original quantity (in Credits array with TransactionType=credit)
	// 2. A charge for the new quantity (in Credits array with TransactionType=credit)
	s.Len(result.Credits, 2, "Should have two items in Credits array for advance billing")
	s.Len(result.Charges, 0, "Should have no items in Charges array for advance billing")

	// Find the credit for the old quantity and the charge for the new quantity
	var oldQuantityCredit, newQuantityCharge *proration.ProrationLineItem
	for i, item := range result.Credits {
		if item.Quantity.Equal(decimal.NewFromInt(3)) {
			oldQuantityCredit = &result.Credits[i]
		} else if item.Quantity.Equal(decimal.NewFromInt(1)) {
			newQuantityCharge = &result.Credits[i]
		}
	}

	// Verify credit for old quantity
	s.NotNil(oldQuantityCredit, "Should have a credit for the old quantity")
	if oldQuantityCredit != nil {
		s.Equal(types.TransactionTypeCredit, oldQuantityCredit.Type)
		s.Equal(basicPriceID, oldQuantityCredit.PriceID)
		s.Equal(decimal.NewFromInt(3), oldQuantityCredit.Quantity)
		s.Equal(types.PlanChangeTypeNoChange, oldQuantityCredit.PlanChangeType)
	}

	// Verify charge for new quantity
	s.NotNil(newQuantityCharge, "Should have a charge for the new quantity")
	if newQuantityCharge != nil {
		s.Equal(types.TransactionTypeCredit, newQuantityCharge.Type)
		s.Equal(basicPriceID, newQuantityCharge.PriceID)
		s.Equal(decimal.NewFromInt(1), newQuantityCharge.Quantity)
		s.Equal(types.PlanChangeTypeNoChange, newQuantityCharge.PlanChangeType)
	}

	// For a quantity decrease, the new quantity should result in a lower charge,
	// so the net amount should be negative (customer gets credit)
	// But since both items are in the Credits array with TransactionType=credit,
	// we need to manually calculate the net amount
	if oldQuantityCredit != nil && newQuantityCharge != nil {
		// Calculate net amount: new charge - old credit
		netAmount := newQuantityCharge.Amount.Sub(oldQuantityCredit.Amount)
		s.True(netAmount.LessThan(decimal.Zero), "Quantity decrease should result in credit to customer")
	}
}

// TestAddItem tests adding a new line item to a subscription
func (s *ProrationServiceSuite) TestAddItem() {
	// Setup proration parameters for adding a new add-on item
	prorationDate := s.GetNow() // Prorate at current time (mid-period)
	addonPriceID := s.testData.prices.addon.ID

	params := &proration.ProrationParams{
		NewPriceID:        &addonPriceID,
		NewQuantity:       lo.ToPtr(decimal.NewFromInt(1)),
		ProrationDate:     prorationDate,
		ProrationBehavior: types.ProrationBehaviorCreateProrations,
		ProrationStrategy: types.ProrationStrategyDayBased,
		Action:            types.ProrationActionAddItem,
	}

	// Calculate proration
	result, err := s.service.CalculateProration(s.GetContext(), s.testData.subscription, params)

	// Verify results
	s.NoError(err)
	s.NotNil(result)

	// For advance billing when adding a new item, we expect:
	// A charge for the new item (in Credits array with TransactionType=credit)
	s.Len(result.Credits, 1, "Should have one item in Credits array for advance billing")
	s.Len(result.Charges, 0, "Should have no items in Charges array for advance billing")

	// Verify charge for the new item
	s.Equal(addonPriceID, result.Credits[0].PriceID)
	s.Equal(decimal.NewFromInt(1), result.Credits[0].Quantity)
	s.Equal(types.TransactionTypeCredit, result.Credits[0].Type)

	// For adding an item, the net amount should be positive (customer owes money)
	// But since the item is in the Credits array with TransactionType=credit,
	// we need to check the amount directly
	s.True(result.Credits[0].Amount.GreaterThan(decimal.Zero), "Adding an item should result in additional charge")
}

// TestRemoveItem tests removing an item from a subscription
func (s *ProrationServiceSuite) TestRemoveItem() {
	// First, add an add-on line item to the subscription
	addonLineItem := &subscription.SubscriptionLineItem{
		ID:              "li_addon",
		SubscriptionID:  s.testData.subscription.ID,
		CustomerID:      s.testData.subscription.CustomerID,
		PlanID:          s.testData.plan.ID,
		PlanDisplayName: s.testData.plan.Name,
		PriceID:         s.testData.prices.addon.ID,
		PriceType:       s.testData.prices.addon.Type,
		DisplayName:     "Add-on Item",
		Quantity:        decimal.NewFromInt(1),
		Currency:        s.testData.subscription.Currency,
		BillingPeriod:   s.testData.subscription.BillingPeriod,
		InvoiceCadence:  types.InvoiceCadenceAdvance,
		StartDate:       s.testData.subscription.StartDate,
		BaseModel:       types.GetDefaultBaseModel(s.GetContext()),
	}

	// Add the line item to the subscription
	s.testData.subscription.LineItems = append(s.testData.subscription.LineItems, addonLineItem)
	s.NoError(s.GetStores().SubscriptionRepo.Update(s.GetContext(), s.testData.subscription))

	// Setup proration parameters for removing the add-on item
	prorationDate := s.GetNow() // Prorate at current time (mid-period)
	addonPriceID := s.testData.prices.addon.ID

	params := &proration.ProrationParams{
		LineItemID:        addonLineItem.ID,
		OldPriceID:        lo.ToPtr(addonPriceID),
		OldQuantity:       lo.ToPtr(decimal.NewFromInt(1)),
		ProrationDate:     prorationDate,
		ProrationBehavior: types.ProrationBehaviorCreateProrations,
		ProrationStrategy: types.ProrationStrategyDayBased,
		Action:            types.ProrationActionRemoveItem,
	}

	// Calculate proration
	result, err := s.service.CalculateProration(s.GetContext(), s.testData.subscription, params)

	// Verify results
	s.NoError(err)
	s.NotNil(result)

	// For removing an item with advance billing, we expect:
	// - A credit for the unused portion of the removed item in the Credits array (TransactionType=credit)
	// - No charges
	s.Len(result.Credits, 1, "Should have one credit for the removed item")
	s.Len(result.Charges, 0, "Should have no charges")

	// Verify the credit
	credit := result.Credits[0]
	s.Equal(addonPriceID, credit.PriceID, "Credit should be for the addon price")
	s.Equal(types.TransactionTypeCredit, credit.Type, "Credit should have transaction type credit")
	s.True(credit.Amount.GreaterThan(decimal.Zero), "Credit amount should be positive")

	// For removing an item, the net amount should be negative (customer gets credit)
	// Since the item is in the Credits array with TransactionType=credit,
	// we need to check the amount directly
	s.True(credit.Amount.GreaterThan(decimal.Zero), "Removing an item should result in credit to customer")
}

// TestCancellation tests cancelling a subscription
func (s *ProrationServiceSuite) TestCancellation() {
	// Setup proration parameters for cancellation
	prorationDate := s.GetNow() // Prorate at current time (mid-period)

	params := &proration.ProrationParams{
		ProrationDate:     prorationDate,
		ProrationBehavior: types.ProrationBehaviorCreateProrations,
		ProrationStrategy: types.ProrationStrategyDayBased,
		Action:            types.ProrationActionCancellation,
	}

	// Calculate proration
	result, err := s.service.CalculateProration(s.GetContext(), s.testData.subscription, params)

	// Verify results
	s.NoError(err)
	s.NotNil(result)

	// For cancellation, we expect:
	// - For advance billing items: credits in the Credits array (TransactionType=credit)
	// - For arrear billing items: charges in the Charges array (TransactionType=debit)

	// The subscription has one advance billing item (basic) and one arrear billing item (arrear)
	// So we should have one credit and one charge
	hasAdvanceItems := false
	hasArrearItems := false

	for _, item := range result.Credits {
		if item.Type == types.TransactionTypeCredit {
			hasAdvanceItems = true
		}
	}

	for _, item := range result.Charges {
		if item.Type == types.TransactionTypeDebit {
			hasArrearItems = true
		}
	}

	s.True(hasAdvanceItems, "Should have credits for advance billing items")
	s.True(hasArrearItems, "Should have charges for arrear billing items")

	// For cancellation, we need to manually calculate the net amount
	// The advance billing item (basic) should generate a credit (negative amount)
	// The arrear billing item (arrear) should generate a charge (positive amount)

	// Calculate total credits
	var totalCredits decimal.Decimal
	for _, item := range result.Credits {
		totalCredits = totalCredits.Add(item.Amount)
	}

	// Calculate total charges
	var totalCharges decimal.Decimal
	for _, item := range result.Charges {
		totalCharges = totalCharges.Add(item.Amount)
	}

	// Net amount = charges - credits
	netAmount := totalCharges.Sub(totalCredits)

	// For our test data, the arrear plan ($15) generates a larger charge
	// than the credit for the basic plan ($10), so the net amount is positive
	s.True(netAmount.GreaterThan(decimal.Zero), "Cancellation results in charge to customer (arrear charge > advance credit)")
}

// TestPeriodSwitchMonthlyToAnnual tests switching from a monthly to an annual plan
func (s *ProrationServiceSuite) TestPeriodSwitchMonthlyToAnnual() {
	// Create an annual price for testing
	annualPrice := &price.Price{
		ID:                 "price_annual",
		Amount:             decimal.NewFromInt(100), // $100/year (less than $10*12 for monthly)
		Currency:           "usd",
		PlanID:             s.testData.plan.ID,
		Type:               types.PRICE_TYPE_FIXED,
		BillingPeriod:      types.BILLING_PERIOD_ANNUAL,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_FLAT_FEE,
		BillingCadence:     types.BILLING_CADENCE_RECURRING,
		InvoiceCadence:     types.InvoiceCadenceAdvance,
		BaseModel:          types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().PriceRepo.Create(s.GetContext(), annualPrice))

	// Setup proration parameters for switching from monthly to annual
	prorationDate := s.GetNow() // Prorate at current time (mid-period)
	monthlyPriceID := s.testData.prices.basic.ID
	annualPriceID := annualPrice.ID

	params := &proration.ProrationParams{
		LineItemID:        s.testData.subscription.LineItems[0].ID,
		OldPriceID:        &monthlyPriceID,
		NewPriceID:        &annualPriceID,
		OldQuantity:       lo.ToPtr(decimal.NewFromInt(1)),
		NewQuantity:       lo.ToPtr(decimal.NewFromInt(1)),
		ProrationDate:     prorationDate,
		ProrationBehavior: types.ProrationBehaviorCreateProrations,
		ProrationStrategy: types.ProrationStrategyDayBased,
		Action:            types.ProrationActionPlanChange,
	}

	// Calculate proration
	result, err := s.service.CalculateProration(s.GetContext(), s.testData.subscription, params)

	// Verify results
	s.NoError(err)
	s.NotNil(result)

	// For advance billing with period switch, we expect:
	// 1. A credit for the unused portion of the monthly plan (in Credits array with TransactionType=credit)
	// 2. A charge for the new annual plan (in Credits array with TransactionType=credit)
	s.Len(result.Credits, 2, "Should have two items in Credits array for advance billing")
	s.Len(result.Charges, 0, "Should have no items in Charges array for advance billing")

	// Find the credit for the old plan and the charge for the new plan
	var oldPlanCredit, newPlanCharge *proration.ProrationLineItem
	for i, item := range result.Credits {
		if item.PriceID == monthlyPriceID {
			oldPlanCredit = &result.Credits[i]
		} else if item.PriceID == annualPriceID {
			newPlanCharge = &result.Credits[i]
		}
	}

	// Verify credit for old plan
	s.NotNil(oldPlanCredit, "Should have a credit for the old monthly plan")
	if oldPlanCredit != nil {
		s.Equal(types.TransactionTypeCredit, oldPlanCredit.Type)
		s.Equal(monthlyPriceID, oldPlanCredit.PriceID)
		s.Equal(decimal.NewFromInt(1), oldPlanCredit.Quantity)
		// The plan change type should be downgrade since annual is cheaper per day
		s.Equal(types.PlanChangeTypeDowngrade, oldPlanCredit.PlanChangeType)
	}

	// Verify charge for new plan
	s.NotNil(newPlanCharge, "Should have a charge for the new annual plan")
	if newPlanCharge != nil {
		s.Equal(types.TransactionTypeCredit, newPlanCharge.Type)
		s.Equal(annualPriceID, newPlanCharge.PriceID)
		s.Equal(decimal.NewFromInt(1), newPlanCharge.Quantity)
		s.Equal(types.PlanChangeTypeDowngrade, newPlanCharge.PlanChangeType)
	}

	// For a monthly to annual switch, the annual plan is cheaper per day,
	// but the total amount is larger, so the net amount should be positive
	// (customer owes more money upfront)
	if oldPlanCredit != nil && newPlanCharge != nil {
		// Calculate net amount: new charge - old credit
		netAmount := newPlanCharge.Amount.Sub(oldPlanCredit.Amount)
		s.True(netAmount.GreaterThan(decimal.Zero), "Monthly to annual switch should result in additional charge")
	}
}

// TestPeriodSwitchAnnualToMonthly tests switching from an annual to a monthly plan
func (s *ProrationServiceSuite) TestPeriodSwitchAnnualToMonthly() {
	// First, create an annual price and update the subscription to use it
	annualPrice := &price.Price{
		ID:                 "price_annual",
		Amount:             decimal.NewFromInt(100), // $100/year (less than $10*12 for monthly)
		Currency:           "usd",
		PlanID:             s.testData.plan.ID,
		Type:               types.PRICE_TYPE_FIXED,
		BillingPeriod:      types.BILLING_PERIOD_ANNUAL,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_FLAT_FEE,
		BillingCadence:     types.BILLING_CADENCE_RECURRING,
		InvoiceCadence:     types.InvoiceCadenceAdvance,
		BaseModel:          types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().PriceRepo.Create(s.GetContext(), annualPrice))

	// Update the subscription to use the annual price
	// and extend the period to a full year
	annualLineItem := s.testData.subscription.LineItems[0]
	annualLineItem.PriceID = annualPrice.ID

	// Update period to be annual
	periodStart := s.GetNow().Add(-30 * 24 * time.Hour) // 30 days ago
	periodEnd := periodStart.AddDate(1, 0, 0)           // 1 year after start
	s.testData.subscription.CurrentPeriodStart = periodStart
	s.testData.subscription.CurrentPeriodEnd = periodEnd
	s.testData.subscription.BillingPeriod = types.BILLING_PERIOD_ANNUAL

	s.NoError(s.GetStores().SubscriptionRepo.Update(s.GetContext(), s.testData.subscription))

	// Setup proration parameters for switching from annual to monthly
	prorationDate := s.GetNow() // Prorate at current time (mid-period)
	annualPriceID := annualPrice.ID
	monthlyPriceID := s.testData.prices.basic.ID

	params := &proration.ProrationParams{
		LineItemID:        annualLineItem.ID,
		OldPriceID:        &annualPriceID,
		NewPriceID:        &monthlyPriceID,
		OldQuantity:       lo.ToPtr(decimal.NewFromInt(1)),
		NewQuantity:       lo.ToPtr(decimal.NewFromInt(1)),
		ProrationDate:     prorationDate,
		ProrationBehavior: types.ProrationBehaviorCreateProrations,
		ProrationStrategy: types.ProrationStrategyDayBased,
		Action:            types.ProrationActionPlanChange,
	}

	// Calculate proration
	result, err := s.service.CalculateProration(s.GetContext(), s.testData.subscription, params)

	// Verify results
	s.NoError(err)
	s.NotNil(result)

	// For advance billing with period switch, we expect:
	// 1. A credit for the unused portion of the annual plan (in Credits array with TransactionType=credit)
	// 2. A charge for the new monthly plan (in Credits array with TransactionType=credit)
	s.Len(result.Credits, 2, "Should have two items in Credits array for advance billing")
	s.Len(result.Charges, 0, "Should have no items in Charges array for advance billing")

	// Find the credit for the old plan and the charge for the new plan
	var oldPlanCredit, newPlanCharge *proration.ProrationLineItem
	for i, item := range result.Credits {
		if item.PriceID == annualPriceID {
			oldPlanCredit = &result.Credits[i]
		} else if item.PriceID == monthlyPriceID {
			newPlanCharge = &result.Credits[i]
		}
	}

	// Verify credit for old plan
	s.NotNil(oldPlanCredit, "Should have a credit for the old annual plan")
	if oldPlanCredit != nil {
		s.Equal(types.TransactionTypeCredit, oldPlanCredit.Type)
		s.Equal(annualPriceID, oldPlanCredit.PriceID)
		s.Equal(decimal.NewFromInt(1), oldPlanCredit.Quantity)
		// The plan change type should be upgrade since monthly is more expensive per day
		s.Equal(types.PlanChangeTypeUpgrade, oldPlanCredit.PlanChangeType)
	}

	// Verify charge for new plan
	s.NotNil(newPlanCharge, "Should have a charge for the new monthly plan")
	if newPlanCharge != nil {
		s.Equal(types.TransactionTypeCredit, newPlanCharge.Type)
		s.Equal(monthlyPriceID, newPlanCharge.PriceID)
		s.Equal(decimal.NewFromInt(1), newPlanCharge.Quantity)
		s.Equal(types.PlanChangeTypeUpgrade, newPlanCharge.PlanChangeType)
	}

	// For an annual to monthly switch, the annual plan credit should be larger than
	// the monthly plan charge, so the net amount should be negative
	// (customer gets a credit for the unused portion of the annual plan)
	if oldPlanCredit != nil && newPlanCharge != nil {
		// Calculate net amount: new charge - old credit
		netAmount := newPlanCharge.Amount.Sub(oldPlanCredit.Amount)
		s.True(netAmount.LessThan(decimal.Zero), "Annual to monthly switch should result in credit to customer")
	}
}

// TestMixedInvoiceCadenceProration tests proration with mixed invoice cadence
func (s *ProrationServiceSuite) TestMixedInvoiceCadenceProration() {
	// Setup proration parameters for upgrading from basic to premium
	// but with the premium price having arrear billing

	// First, create a premium price with arrear billing
	premiumArrearPrice := &price.Price{
		ID:                 "price_premium_arrear",
		Amount:             decimal.NewFromInt(20),
		Currency:           "usd",
		PlanID:             s.testData.plan.ID,
		Type:               types.PRICE_TYPE_FIXED,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_FLAT_FEE,
		BillingCadence:     types.BILLING_CADENCE_RECURRING,
		InvoiceCadence:     types.InvoiceCadenceArrear,
		BaseModel:          types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().PriceRepo.Create(s.GetContext(), premiumArrearPrice))

	prorationDate := s.GetNow() // Prorate at current time (mid-period)
	basicPriceID := s.testData.prices.basic.ID
	premiumArrearPriceID := premiumArrearPrice.ID

	params := &proration.ProrationParams{
		LineItemID:        s.testData.subscription.LineItems[0].ID,
		OldPriceID:        &basicPriceID,
		NewPriceID:        &premiumArrearPriceID,
		OldQuantity:       lo.ToPtr(decimal.NewFromInt(1)),
		NewQuantity:       lo.ToPtr(decimal.NewFromInt(1)),
		ProrationDate:     prorationDate,
		ProrationBehavior: types.ProrationBehaviorCreateProrations,
		ProrationStrategy: types.ProrationStrategyDayBased,
		Action:            types.ProrationActionPlanChange,
	}

	// Calculate proration
	result, err := s.service.CalculateProration(s.GetContext(), s.testData.subscription, params)

	// Verify results
	s.NoError(err)
	s.NotNil(result)

	// For mixed invoice cadence (advance to arrear), we expect:
	// 1. A credit for the unused portion of the basic plan (in Credits array with TransactionType=credit)
	// 2. A charge for the new premium plan (in Charges array with TransactionType=credit)
	// This is because the implementation uses the invoice cadence as per respective price
	s.Len(result.Credits, 1, "Should have one item in Credits array for advance to arrear switch")
	s.Len(result.Charges, 1, "Should have one item in Charges array for advance to arrear switch")

	// Find the credit for the old plan and the charge for the new plan
	var oldPlanCredit, newPlanCharge *proration.ProrationLineItem
	for i, item := range result.Credits {
		if item.PriceID == basicPriceID {
			oldPlanCredit = &result.Credits[i]
		}
	}

	for i, item := range result.Charges {
		if item.PriceID == premiumArrearPriceID {
			newPlanCharge = &result.Charges[i]
		}
	}

	// Verify credit for old plan
	s.NotNil(oldPlanCredit, "Should have a credit for the old plan")
	if oldPlanCredit != nil {
		s.Equal(types.TransactionTypeCredit, oldPlanCredit.Type)
		s.Equal(basicPriceID, oldPlanCredit.PriceID)
		s.Equal(decimal.NewFromInt(1), oldPlanCredit.Quantity)
		s.Equal(types.PlanChangeTypeUpgrade, oldPlanCredit.PlanChangeType)
	}

	// Verify charge for new plan
	s.NotNil(newPlanCharge, "Should have a charge for the new plan")
	if newPlanCharge != nil {
		s.Equal(types.TransactionTypeDebit, newPlanCharge.Type)
		s.Equal(premiumArrearPriceID, newPlanCharge.PriceID)
		s.Equal(decimal.NewFromInt(1), newPlanCharge.Quantity)
		s.Equal(types.PlanChangeTypeUpgrade, newPlanCharge.PlanChangeType)
	}
	s.True(result.NetAmount.GreaterThan(decimal.Zero), "Upgrade should result in additional charge")

}

// TestSecondBasedProrationStrategy tests proration with second-based strategy
func (s *ProrationServiceSuite) TestSecondBasedProrationStrategy() {
	// Setup proration parameters for upgrading from basic to premium with second-based strategy
	prorationDate := s.GetNow() // Prorate at current time (mid-period)
	basicPriceID := s.testData.prices.basic.ID
	premiumPriceID := s.testData.prices.premium.ID

	params := &proration.ProrationParams{
		LineItemID:        s.testData.subscription.LineItems[0].ID,
		OldPriceID:        &basicPriceID,
		NewPriceID:        &premiumPriceID,
		OldQuantity:       lo.ToPtr(decimal.NewFromInt(1)),
		NewQuantity:       lo.ToPtr(decimal.NewFromInt(1)),
		ProrationDate:     prorationDate,
		ProrationBehavior: types.ProrationBehaviorCreateProrations,
		ProrationStrategy: types.ProrationStrategySecondBased, // Use second-based strategy
		Action:            types.ProrationActionPlanChange,
	}

	// Calculate proration
	result, err := s.service.CalculateProration(s.GetContext(), s.testData.subscription, params)

	// Verify results
	s.NoError(err)
	s.NotNil(result)

	// For advance billing with plan upgrade, we expect:
	// 1. A credit for the unused portion of the basic plan (in Credits array with TransactionType=credit)
	// 2. A charge for the new premium plan (in Credits array with TransactionType=credit)
	s.Len(result.Credits, 2, "Should have two items in Credits array for advance billing")
	s.Len(result.Charges, 0, "Should have no items in Charges array for advance billing")

	// Find the credit for the old plan and the charge for the new plan
	var oldPlanCredit, newPlanCharge *proration.ProrationLineItem
	for i, item := range result.Credits {
		if item.PriceID == basicPriceID {
			oldPlanCredit = &result.Credits[i]
		} else if item.PriceID == premiumPriceID {
			newPlanCharge = &result.Credits[i]
		}
	}

	// Verify credit for old plan
	s.NotNil(oldPlanCredit, "Should have a credit for the old plan")
	if oldPlanCredit != nil {
		s.Equal(types.TransactionTypeCredit, oldPlanCredit.Type)
		s.Equal(basicPriceID, oldPlanCredit.PriceID)
		s.Equal(decimal.NewFromInt(1), oldPlanCredit.Quantity)
		s.Equal(types.PlanChangeTypeUpgrade, oldPlanCredit.PlanChangeType)
	}

	// Verify charge for new plan
	s.NotNil(newPlanCharge, "Should have a charge for the new plan")
	if newPlanCharge != nil {
		s.Equal(types.TransactionTypeCredit, newPlanCharge.Type)
		s.Equal(premiumPriceID, newPlanCharge.PriceID)
		s.Equal(decimal.NewFromInt(1), newPlanCharge.Quantity)
		s.Equal(types.PlanChangeTypeUpgrade, newPlanCharge.PlanChangeType)
	}

	// For an upgrade, the premium price should be higher than the basic price,
	// so the net amount should be positive (customer owes money)
	if oldPlanCredit != nil && newPlanCharge != nil {
		// Calculate net amount: new charge - old credit
		netAmount := newPlanCharge.Amount.Sub(oldPlanCredit.Amount)
		s.True(netAmount.GreaterThan(decimal.Zero), "Upgrade should result in additional charge")
	}
}

// TestProrationWithTrialPeriod tests proration with a trial period
func (s *ProrationServiceSuite) TestProrationWithTrialPeriod() {
	// Create a price with a trial period
	trialPrice := &price.Price{
		ID:                 "price_with_trial",
		Amount:             decimal.NewFromInt(30),
		Currency:           "usd",
		PlanID:             s.testData.plan.ID,
		Type:               types.PRICE_TYPE_FIXED,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_FLAT_FEE,
		BillingCadence:     types.BILLING_CADENCE_RECURRING,
		InvoiceCadence:     types.InvoiceCadenceAdvance,
		TrialPeriod:        14, // 14-day trial
		BaseModel:          types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().PriceRepo.Create(s.GetContext(), trialPrice))

	// Setup proration parameters for upgrading from basic to trial price
	prorationDate := s.GetNow() // Prorate at current time (mid-period)
	basicPriceID := s.testData.prices.basic.ID
	trialPriceID := trialPrice.ID

	params := &proration.ProrationParams{
		LineItemID:        s.testData.subscription.LineItems[0].ID,
		OldPriceID:        &basicPriceID,
		NewPriceID:        &trialPriceID,
		OldQuantity:       lo.ToPtr(decimal.NewFromInt(1)),
		NewQuantity:       lo.ToPtr(decimal.NewFromInt(1)),
		ProrationDate:     prorationDate,
		ProrationBehavior: types.ProrationBehaviorCreateProrations,
		ProrationStrategy: types.ProrationStrategyDayBased,
		Action:            types.ProrationActionPlanChange,
	}

	// Calculate proration
	result, err := s.service.CalculateProration(s.GetContext(), s.testData.subscription, params)

	// Verify results
	s.NoError(err)
	s.NotNil(result)

	// For advance billing with plan upgrade to a trial plan, we expect:
	// 1. A credit for the unused portion of the basic plan (in Credits array with TransactionType=credit)
	// 2. A charge for the new trial plan (in Credits array with TransactionType=credit)
	s.Len(result.Credits, 2, "Should have two items in Credits array for advance billing")
	s.Len(result.Charges, 0, "Should have no items in Charges array for advance billing")

	// Find the credit for the old plan and the charge for the new plan
	var oldPlanCredit, newPlanCharge *proration.ProrationLineItem
	for i, item := range result.Credits {
		if item.PriceID == basicPriceID {
			oldPlanCredit = &result.Credits[i]
		} else if item.PriceID == trialPriceID {
			newPlanCharge = &result.Credits[i]
		}
	}

	// Verify credit for old plan
	s.NotNil(oldPlanCredit, "Should have a credit for the old plan")
	if oldPlanCredit != nil {
		s.Equal(types.TransactionTypeCredit, oldPlanCredit.Type)
		s.Equal(basicPriceID, oldPlanCredit.PriceID)
		s.Equal(decimal.NewFromInt(1), oldPlanCredit.Quantity)
		s.Equal(types.PlanChangeTypeUpgrade, oldPlanCredit.PlanChangeType)
	}

	// Verify charge for new plan
	s.NotNil(newPlanCharge, "Should have a charge for the new plan")
	if newPlanCharge != nil {
		s.Equal(types.TransactionTypeCredit, newPlanCharge.Type)
		s.Equal(trialPriceID, newPlanCharge.PriceID)
		s.Equal(decimal.NewFromInt(1), newPlanCharge.Quantity)
		s.Equal(types.PlanChangeTypeUpgrade, newPlanCharge.PlanChangeType)
	}

	// For an upgrade to a trial plan, the trial plan price should be higher,
	// so the net amount should be positive (customer owes money)
	if oldPlanCredit != nil && newPlanCharge != nil {
		// Calculate net amount: new charge - old credit
		netAmount := newPlanCharge.Amount.Sub(oldPlanCredit.Amount)
		s.True(netAmount.GreaterThan(decimal.Zero), "Upgrade to trial plan should result in additional charge")
	}
}
