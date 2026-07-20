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
		AddonRepo:                  s.GetStores().AddonRepo,
		AddonAssociationRepo:       s.GetStores().AddonAssociationRepo,
		ConnectionRepo:             s.GetStores().ConnectionRepo,
		SettingsRepo:               s.GetStores().SettingsRepo,
		EventPublisher:             s.GetPublisher(),
		WebhookPublisher:           s.GetWebhookPublisher(),
		ProrationCalculator:        s.GetCalculator(),
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

// TestThresholdBilling_InvoiceCreatedWhenThresholdExceeded verifies that when
// current-period usage exceeds the subscription's AutoInvoiceThreshold, a mid-period
// invoice is created and CurrentPeriodStart is advanced.
func (s *SubscriptionThresholdBillingTestSuite) TestThresholdBilling_InvoiceCreatedWhenThresholdExceeded() {
	ctx := s.GetContext()

	// 1001 calls x $0.01 = $10.01, which exceeds the $10 threshold.
	// All events are timestamped 1 hour ago, inside CurrentPeriodStart (now-7d) .. now.
	s.insertEvents(1001, s.testData.now.Add(-1*time.Hour))

	result, err := s.service.ProcessAutoInvoiceThresholdBilling(ctx)
	s.NoError(err)
	s.Require().NotNil(result)

	// Only subA qualifies (subB has no threshold).
	s.Equal(1, result.TotalChecked)
	s.Equal(1, result.TotalInvoiced)
	s.Equal(0, result.TotalSkipped)
	s.Equal(0, result.TotalFailed)

	s.Require().Len(result.Items, 1)
	item := result.Items[0]
	s.Equal(s.testData.subA.ID, item.SubscriptionID)
	s.True(item.Invoiced)
	s.NotEmpty(item.InvoiceID)
	s.Empty(item.Error)

	// Verify invoice is persisted with the correct billing reason.
	filter := types.NewNoLimitInvoiceFilter()
	filter.SubscriptionID = s.testData.subA.ID
	invoices, err := s.GetStores().InvoiceRepo.List(ctx, filter)
	s.NoError(err)
	s.Require().Len(invoices, 1)
	s.Equal(string(types.InvoiceBillingReasonAutoInvoiceThreshold), invoices[0].BillingReason)

	// Verify CurrentPeriodStart was advanced (no longer the original now-7d).
	reloaded, err := s.GetStores().SubscriptionRepo.Get(ctx, s.testData.subA.ID)
	s.NoError(err)
	originalStart := s.testData.now.Add(-7 * 24 * time.Hour)
	s.True(reloaded.CurrentPeriodStart.After(originalStart),
		"CurrentPeriodStart should have advanced beyond the original now-7d")
}

// TestThresholdBilling_PeriodAdvanceResetsUsageWindow verifies that after a threshold
// invoice advances CurrentPeriodStart, events from the previous period are excluded
// from the next run's usage window — preventing double-counting.
func (s *SubscriptionThresholdBillingTestSuite) TestThresholdBilling_PeriodAdvanceResetsUsageWindow() {
	ctx := s.GetContext()

	// Period 1: 1001 events -> $10.01, crosses threshold.
	// Timestamp 1 hour before now, inside the original period (now-7d .. now).
	s.insertEvents(1001, s.testData.now.Add(-1*time.Hour))

	result1, err := s.service.ProcessAutoInvoiceThresholdBilling(ctx)
	s.NoError(err)
	s.Equal(1, result1.TotalInvoiced, "first run should invoice subA")

	// Record the new CurrentPeriodStart (T1 approx time of first run).
	reloadedAfterFirst, err := s.GetStores().SubscriptionRepo.Get(ctx, s.testData.subA.ID)
	s.NoError(err)
	t1 := reloadedAfterFirst.CurrentPeriodStart

	// Period 2: only 500 events -> $5.00, below the $10 threshold.
	// Timestamps are 1 minute after T1 to ensure they fall inside the new window.
	s.insertEvents(500, t1.Add(1*time.Minute))

	result2, err := s.service.ProcessAutoInvoiceThresholdBilling(ctx)
	s.NoError(err)
	s.Require().NotNil(result2)

	// Second run must skip subA: $5 usage < $10 threshold.
	s.Equal(1, result2.TotalChecked)
	s.Equal(0, result2.TotalInvoiced)
	s.Equal(1, result2.TotalSkipped)
	s.Equal(0, result2.TotalFailed)

	// Still exactly one invoice in the store (the one from the first run).
	filter := types.NewNoLimitInvoiceFilter()
	filter.SubscriptionID = s.testData.subA.ID
	invoices, err := s.GetStores().InvoiceRepo.List(ctx, filter)
	s.NoError(err)
	s.Len(invoices, 1, "no second invoice should have been created")

	// CurrentPeriodStart must not have moved again.
	reloadedAfterSecond, err := s.GetStores().SubscriptionRepo.Get(ctx, s.testData.subA.ID)
	s.NoError(err)
	s.True(reloadedAfterSecond.CurrentPeriodStart.Equal(t1),
		"CurrentPeriodStart should remain at T1 after the skipped second run")
}

// TestThresholdBilling_SkipsSubscriptionWithNoUsage verifies that a subscription
// with AutoInvoiceThreshold set but zero current-period usage is skipped without
// creating an invoice or advancing CurrentPeriodStart.
func (s *SubscriptionThresholdBillingTestSuite) TestThresholdBilling_SkipsSubscriptionWithNoUsage() {
	ctx := s.GetContext()

	// No events inserted — usage = $0.00, well below the $10 threshold.

	result, err := s.service.ProcessAutoInvoiceThresholdBilling(ctx)
	s.NoError(err)
	s.Require().NotNil(result)

	s.Equal(1, result.TotalChecked)
	s.Equal(0, result.TotalInvoiced)
	s.Equal(1, result.TotalSkipped)
	s.Equal(0, result.TotalFailed)

	s.Require().Len(result.Items, 1)
	item := result.Items[0]
	s.Equal(s.testData.subA.ID, item.SubscriptionID)
	s.False(item.Invoiced)
	s.Empty(item.InvoiceID)
	s.Empty(item.Error)

	// No invoice should have been created.
	filter := types.NewNoLimitInvoiceFilter()
	filter.SubscriptionID = s.testData.subA.ID
	invoices, err := s.GetStores().InvoiceRepo.List(ctx, filter)
	s.NoError(err)
	s.Empty(invoices, "no invoice should exist when usage is zero")

	// CurrentPeriodStart must be unchanged.
	reloaded, err := s.GetStores().SubscriptionRepo.Get(ctx, s.testData.subA.ID)
	s.NoError(err)
	originalStart := s.testData.now.Add(-7 * 24 * time.Hour)
	s.True(reloaded.CurrentPeriodStart.Equal(originalStart) ||
		reloaded.CurrentPeriodStart.Sub(originalStart).Abs() < time.Second,
		"CurrentPeriodStart should be unchanged when usage is below threshold")
}
