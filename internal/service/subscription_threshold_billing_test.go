package service

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/domain/plan"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/suite"
)

type SubscriptionThresholdBillingTestSuite struct {
	testutil.BaseServiceTestSuite
	service  SubscriptionService
	testData struct {
		customer *customer.Customer
		plan     *plan.Plan
		meter    *meter.Meter
		price    *price.Price
		subA     *subscription.Subscription // has AutoInvoiceThreshold = $10
		subB     *subscription.Subscription // no AutoInvoiceThreshold (control)
		now      time.Time
	}
}

func TestSubscriptionThresholdBillingService(t *testing.T) {
	suite.Run(t, new(SubscriptionThresholdBillingTestSuite))
}

func (s *SubscriptionThresholdBillingTestSuite) SetupSuite() {
	s.BaseServiceTestSuite.SetupSuite()
}

func (s *SubscriptionThresholdBillingTestSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	s.setupService()
	s.setupTestData()
}

func (s *SubscriptionThresholdBillingTestSuite) TearDownTest() {
	s.BaseServiceTestSuite.TearDownTest()
}

func (s *SubscriptionThresholdBillingTestSuite) setupService() {
	s.service = NewSubscriptionService(ServiceParams{
		Logger:                     s.GetLogger(),
		Config:                     s.GetConfig(),
		DB:                         s.GetDB(),
		TaxAssociationRepo:         s.GetStores().TaxAssociationRepo,
		TaxRateRepo:                s.GetStores().TaxRateRepo,
		SubRepo:                    s.GetStores().SubscriptionRepo,
		SubscriptionLineItemRepo:   s.GetStores().SubscriptionLineItemRepo,
		SubscriptionPhaseRepo:      s.GetStores().SubscriptionPhaseRepo,
		SubScheduleRepo:            s.GetStores().SubscriptionScheduleRepo,
		PlanRepo:                   s.GetStores().PlanRepo,
		PriceRepo:                  s.GetStores().PriceRepo,
		PriceUnitRepo:              s.GetStores().PriceUnitRepo,
		EventRepo:                  s.GetStores().EventRepo,
		MeterRepo:                  s.GetStores().MeterRepo,
		CustomerRepo:               s.GetStores().CustomerRepo,
		InvoiceRepo:                s.GetStores().InvoiceRepo,
		InvoiceLineItemRepo:        s.GetStores().InvoiceLineItemRepo,
		EntitlementRepo:            s.GetStores().EntitlementRepo,
		EnvironmentRepo:            s.GetStores().EnvironmentRepo,
		FeatureRepo:                s.GetStores().FeatureRepo,
		TenantRepo:                 s.GetStores().TenantRepo,
		UserRepo:                   s.GetStores().UserRepo,
		AuthRepo:                   s.GetStores().AuthRepo,
		WalletRepo:                 s.GetStores().WalletRepo,
		PaymentRepo:                s.GetStores().PaymentRepo,
		CreditGrantRepo:            s.GetStores().CreditGrantRepo,
		CreditGrantApplicationRepo: s.GetStores().CreditGrantApplicationRepo,
		CouponRepo:                 s.GetStores().CouponRepo,
		CouponAssociationRepo:      s.GetStores().CouponAssociationRepo,
		CouponApplicationRepo:      s.GetStores().CouponApplicationRepo,
		AddonRepo:                  testutil.NewInMemoryAddonStore(), // not in GetStores(); matches pattern used by other suites
		AddonAssociationRepo:       s.GetStores().AddonAssociationRepo,
		ConnectionRepo:             s.GetStores().ConnectionRepo,
		SettingsRepo:               s.GetStores().SettingsRepo,
		EventPublisher:             s.GetPublisher(),
		WebhookPublisher:           s.GetWebhookPublisher(),
		ProrationCalculator:        s.GetCalculator(),
		FeatureUsageRepo:           s.GetStores().FeatureUsageRepo,
		IntegrationFactory:         s.GetIntegrationFactory(),
	})
}

func (s *SubscriptionThresholdBillingTestSuite) setupTestData() {
	ctx := s.GetContext()
	s.testData.now = s.GetNow()

	// Customer
	s.testData.customer = &customer.Customer{
		ID:         "cust_threshold_123",
		ExternalID: "ext_cust_threshold_123",
		Name:       "Threshold Test Customer",
		Email:      "threshold@example.com",
		BaseModel:  types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().CustomerRepo.Create(ctx, s.testData.customer))

	// Plan
	s.testData.plan = &plan.Plan{
		ID:        "plan_threshold_123",
		Name:      "Threshold Test Plan",
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().PlanRepo.Create(ctx, s.testData.plan))

	// Meter: COUNT aggregation on "api_call" events
	s.testData.meter = &meter.Meter{
		ID:        "meter_threshold_api_calls",
		Name:      "API Calls",
		EventName: "api_call",
		Aggregation: meter.Aggregation{
			Type: types.AggregationCount,
		},
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().MeterRepo.CreateMeter(ctx, s.testData.meter))

	// Price: flat-fee usage, $0.01 per call, arrear billing
	// Math: 1001 calls x $0.01 = $10.01 -> crosses $10 threshold
	//       500  calls x $0.01 = $5.00  -> stays below $10 threshold
	s.testData.price = &price.Price{
		ID:                 "price_threshold_api_calls",
		Amount:             decimal.NewFromFloat(0.01),
		Currency:           "usd",
		EntityType:         types.PRICE_ENTITY_TYPE_PLAN,
		EntityID:           s.testData.plan.ID,
		Type:               types.PRICE_TYPE_USAGE,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_FLAT_FEE,
		BillingCadence:     types.BILLING_CADENCE_RECURRING,
		InvoiceCadence:     types.InvoiceCadenceArrear,
		MeterID:            s.testData.meter.ID,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().PriceRepo.Create(ctx, s.testData.price))

	threshold := decimal.NewFromInt(10) // $10 threshold

	// Sub A: active, threshold set -> eligible for processing
	subALineItem := &subscription.SubscriptionLineItem{
		ID:               types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION_LINE_ITEM),
		SubscriptionID:   "sub_threshold_a",
		CustomerID:       s.testData.customer.ID,
		EntityID:         s.testData.plan.ID,
		EntityType:       types.SubscriptionLineItemEntityTypePlan,
		PlanDisplayName:  s.testData.plan.Name,
		PriceID:          s.testData.price.ID,
		PriceType:        s.testData.price.Type,
		MeterID:          s.testData.meter.ID,
		MeterDisplayName: s.testData.meter.Name,
		DisplayName:      s.testData.meter.Name,
		Quantity:         decimal.Zero,
		Currency:         "usd",
		BillingPeriod:    types.BILLING_PERIOD_MONTHLY,
		InvoiceCadence:   types.InvoiceCadenceArrear,
		StartDate:        s.testData.now.Add(-7 * 24 * time.Hour),
		BaseModel:        types.GetDefaultBaseModel(ctx),
	}
	s.testData.subA = &subscription.Subscription{
		ID:                   "sub_threshold_a",
		PlanID:               s.testData.plan.ID,
		CustomerID:           s.testData.customer.ID,
		StartDate:            s.testData.now.Add(-30 * 24 * time.Hour),
		CurrentPeriodStart:   s.testData.now.Add(-7 * 24 * time.Hour),
		CurrentPeriodEnd:     s.testData.now.Add(23 * 24 * time.Hour),
		BillingAnchor:        s.testData.now.Add(-30 * 24 * time.Hour),
		Currency:             "usd",
		BillingCycle:         types.BillingCycleAnniversary,
		BillingPeriod:        types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount:   1,
		SubscriptionStatus:   types.SubscriptionStatusActive,
		SubscriptionType:     types.SubscriptionTypeStandalone,
		AutoInvoiceThreshold: &threshold,
		BaseModel:            types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().SubscriptionRepo.CreateWithLineItems(ctx, s.testData.subA, []*subscription.SubscriptionLineItem{subALineItem}))

	// Sub B: active, no threshold -> excluded from GetSubscriptionsWithAutoInvoiceThreshold
	s.testData.subB = &subscription.Subscription{
		ID:                 "sub_threshold_b",
		PlanID:             s.testData.plan.ID,
		CustomerID:         s.testData.customer.ID,
		StartDate:          s.testData.now.Add(-30 * 24 * time.Hour),
		CurrentPeriodStart: s.testData.now.Add(-7 * 24 * time.Hour),
		CurrentPeriodEnd:   s.testData.now.Add(23 * 24 * time.Hour),
		BillingAnchor:      s.testData.now.Add(-30 * 24 * time.Hour),
		Currency:           "usd",
		BillingCycle:       types.BillingCycleAnniversary,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		SubscriptionStatus: types.SubscriptionStatusActive,
		SubscriptionType:   types.SubscriptionTypeStandalone,
		// AutoInvoiceThreshold intentionally nil
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().SubscriptionRepo.CreateWithLineItems(ctx, s.testData.subB, []*subscription.SubscriptionLineItem{}))
}

// insertEvents inserts n COUNT events for subA's customer, all at the given timestamp.
func (s *SubscriptionThresholdBillingTestSuite) insertEvents(n int, ts time.Time) {
	ctx := s.GetContext()
	for i := 0; i < n; i++ {
		s.NoError(s.GetStores().EventRepo.InsertEvent(ctx, &events.Event{
			ID:                 s.GetUUID(),
			TenantID:           s.testData.subA.TenantID,
			EventName:          s.testData.meter.EventName,
			ExternalCustomerID: s.testData.customer.ExternalID,
			Timestamp:          ts,
			Properties:         map[string]interface{}{},
		}))
	}
}
