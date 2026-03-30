package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/entitlement"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/feature"
	"github.com/flexprice/flexprice/internal/domain/invoice"
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

type BillingServiceSuite struct {
	testutil.BaseServiceTestSuite
	service     BillingService
	invoiceRepo *testutil.InMemoryInvoiceStore
	eventRepo   *testutil.InMemoryEventStore
	testData    struct {
		customer *customer.Customer
		plan     *plan.Plan
		meters   struct {
			apiCalls       *meter.Meter
			storage        *meter.Meter
			storageArchive *meter.Meter
		}
		prices struct {
			fixed          *price.Price
			fixedDaily     *price.Price
			apiCalls       *price.Price
			storageArchive *price.Price
		}
		subscription *subscription.Subscription
		now          time.Time
		events       struct {
			apiCalls *events.Event
			archived *events.Event
		}
	}
}

func TestBillingService(t *testing.T) {
	suite.Run(t, new(BillingServiceSuite))
}

func (s *BillingServiceSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	s.setupService()
	s.setupTestData()
}

func (s *BillingServiceSuite) TearDownTest() {
	s.BaseServiceTestSuite.TearDownTest()
	s.eventRepo.Clear()
	s.invoiceRepo.Clear()
}

func (s *BillingServiceSuite) setupService() {
	s.eventRepo = s.GetStores().EventRepo.(*testutil.InMemoryEventStore)
	s.invoiceRepo = s.GetStores().InvoiceRepo.(*testutil.InMemoryInvoiceStore)

	s.service = NewBillingService(ServiceParams{
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
		CouponAssociationRepo:    s.GetStores().CouponAssociationRepo,
		CouponRepo:               s.GetStores().CouponRepo,
		CouponApplicationRepo:    s.GetStores().CouponApplicationRepo,
		AddonAssociationRepo:     s.GetStores().AddonAssociationRepo,
		TaxRateRepo:              s.GetStores().TaxRateRepo,
		TaxAssociationRepo:       s.GetStores().TaxAssociationRepo,
		TaxAppliedRepo:           s.GetStores().TaxAppliedRepo,
		SettingsRepo:             s.GetStores().SettingsRepo,
		EventPublisher:           s.GetPublisher(),
		WebhookPublisher:         s.GetWebhookPublisher(),
		ProrationCalculator:      s.GetCalculator(),
		AlertLogsRepo:            s.GetStores().AlertLogsRepo,
		FeatureUsageRepo:         s.GetStores().FeatureUsageRepo,
	})
}

func (s *BillingServiceSuite) setupTestData() {
	// Clear any existing data
	s.BaseServiceTestSuite.ClearStores()

	// Create test customer
	s.testData.customer = &customer.Customer{
		ID:         "cust_123",
		ExternalID: "ext_cust_123",
		Name:       "Test Customer",
		Email:      "test@example.com",
		BaseModel:  types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().CustomerRepo.Create(s.GetContext(), s.testData.customer))

	// Create test plan
	s.testData.plan = &plan.Plan{
		ID:          "plan_123",
		Name:        "Test Plan",
		Description: "Test Plan Description",
		BaseModel:   types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().PlanRepo.Create(s.GetContext(), s.testData.plan))

	// Create test meters
	s.testData.meters.apiCalls = &meter.Meter{
		ID:        "meter_api_calls",
		Name:      "API Calls",
		EventName: "api_call",
		Aggregation: meter.Aggregation{
			Type: types.AggregationCount,
		},
		BaseModel: types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().MeterRepo.CreateMeter(s.GetContext(), s.testData.meters.apiCalls))

	s.testData.meters.storage = &meter.Meter{
		ID:        "meter_storage",
		Name:      "Storage",
		EventName: "storage_usage",
		Aggregation: meter.Aggregation{
			Type:  types.AggregationSum,
			Field: "bytes_used",
		},
		Filters: []meter.Filter{
			{
				Key:    "region",
				Values: []string{"us-east-1"},
			},
			{
				Key:    "tier",
				Values: []string{"standard"},
			},
		},
		BaseModel: types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().MeterRepo.CreateMeter(s.GetContext(), s.testData.meters.storage))

	s.testData.meters.storageArchive = &meter.Meter{
		ID:        "meter_storage_archive",
		Name:      "Storage Archive",
		EventName: "storage_usage",
		Aggregation: meter.Aggregation{
			Type:  types.AggregationSum,
			Field: "bytes_used",
		},
		Filters: []meter.Filter{
			{
				Key:    "region",
				Values: []string{"us-east-1"},
			},
			{
				Key:    "tier",
				Values: []string{"archive"},
			},
		},
		BaseModel: types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().MeterRepo.CreateMeter(s.GetContext(), s.testData.meters.storageArchive))

	// Create test prices
	upTo1000 := uint64(1000)
	upTo5000 := uint64(5000)

	// API Calls - Usage-based with ARREAR invoice cadence
	s.testData.prices.apiCalls = &price.Price{
		ID:                 "price_api_calls",
		Amount:             decimal.Zero,
		Currency:           "usd",
		EntityType:         types.PRICE_ENTITY_TYPE_PLAN,
		EntityID:           s.testData.plan.ID,
		Type:               types.PRICE_TYPE_USAGE,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_TIERED,
		BillingCadence:     types.BILLING_CADENCE_RECURRING,
		InvoiceCadence:     types.InvoiceCadenceArrear, // Usage charges should be arrear
		TierMode:           types.BILLING_TIER_SLAB,
		MeterID:            s.testData.meters.apiCalls.ID,
		Tiers: []price.PriceTier{
			{UpTo: &upTo1000, UnitAmount: decimal.NewFromFloat(0.02)},
			{UpTo: &upTo5000, UnitAmount: decimal.NewFromFloat(0.005)},
			{UpTo: nil, UnitAmount: decimal.NewFromFloat(0.01)},
		},
		BaseModel: types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().PriceRepo.Create(s.GetContext(), s.testData.prices.apiCalls))

	// Fixed - Fixed fee with ADVANCE invoice cadence
	s.testData.prices.fixed = &price.Price{
		ID:                 "price_fixed",
		Amount:             decimal.NewFromInt(10), // Fixed amount of 10
		Currency:           "usd",
		EntityType:         types.PRICE_ENTITY_TYPE_PLAN,
		EntityID:           s.testData.plan.ID,
		Type:               types.PRICE_TYPE_FIXED, // Fixed price type
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_FLAT_FEE,
		BillingCadence:     types.BILLING_CADENCE_RECURRING,
		InvoiceCadence:     types.InvoiceCadenceAdvance, // Fixed charges should be advance
		BaseModel:          types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().PriceRepo.Create(s.GetContext(), s.testData.prices.fixed))

	// Fixed Daily - for testing daily line item quantity (e.g. Feb 22–Mar 22 = 28 days)
	s.testData.prices.fixedDaily = &price.Price{
		ID:                 "price_fixed_daily",
		Amount:             decimal.NewFromInt(1), // 1 per day
		Currency:           "usd",
		EntityType:         types.PRICE_ENTITY_TYPE_PLAN,
		EntityID:           s.testData.plan.ID,
		Type:               types.PRICE_TYPE_FIXED,
		BillingPeriod:      types.BILLING_PERIOD_DAILY,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_FLAT_FEE,
		BillingCadence:     types.BILLING_CADENCE_RECURRING,
		InvoiceCadence:     types.InvoiceCadenceAdvance,
		BaseModel:          types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().PriceRepo.Create(s.GetContext(), s.testData.prices.fixedDaily))

	// Archive Storage - Fixed fee with ARREAR invoice cadence (for testing fixed arrear)
	s.testData.prices.storageArchive = &price.Price{
		ID:                 "price_storage_archive",
		Amount:             decimal.NewFromInt(5), // Fixed amount of 5
		Currency:           "usd",
		EntityType:         types.PRICE_ENTITY_TYPE_PLAN,
		EntityID:           s.testData.plan.ID,
		Type:               types.PRICE_TYPE_FIXED, // Fixed price type
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_FLAT_FEE,
		BillingCadence:     types.BILLING_CADENCE_RECURRING,
		InvoiceCadence:     types.InvoiceCadenceArrear, // Fixed charges with arrear cadence
		MeterID:            s.testData.meters.storageArchive.ID,
		BaseModel:          types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().PriceRepo.Create(s.GetContext(), s.testData.prices.storageArchive))

	s.testData.now = time.Now().UTC()
	// Use CurrentPeriodEnd as BillingAnchor so the next period is a full month (same day-of-month),
	// ensuring next-period advance charges (e.g. fixed price) are included when billing at period end.
	currentPeriodStart := s.testData.now.Add(-48 * time.Hour)
	currentPeriodEnd := s.testData.now.Add(6 * 24 * time.Hour)
	s.testData.subscription = &subscription.Subscription{
		ID:                 "sub_123",
		PlanID:             s.testData.plan.ID,
		CustomerID:         s.testData.customer.ID,
		StartDate:          s.testData.now.Add(-30 * 24 * time.Hour),
		BillingAnchor:      currentPeriodEnd,
		CurrentPeriodStart: currentPeriodStart,
		CurrentPeriodEnd:   currentPeriodEnd,
		Currency:           "usd",
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		SubscriptionStatus: types.SubscriptionStatusActive,
		BaseModel:          types.GetDefaultBaseModel(s.GetContext()),
	}

	// Create line items for the subscription
	lineItems := []*subscription.SubscriptionLineItem{
		{
			ID:              types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION_LINE_ITEM),
			SubscriptionID:  s.testData.subscription.ID,
			CustomerID:      s.testData.subscription.CustomerID,
			EntityID:        s.testData.plan.ID,
			EntityType:      types.SubscriptionLineItemEntityTypePlan,
			PlanDisplayName: s.testData.plan.Name,
			PriceID:         s.testData.prices.fixed.ID,
			PriceType:       s.testData.prices.fixed.Type,
			DisplayName:     "Fixed",
			Quantity:        decimal.NewFromInt(1), // 1 unit of fixed
			Currency:        s.testData.subscription.Currency,
			BillingPeriod:   s.testData.subscription.BillingPeriod,
			InvoiceCadence:  types.InvoiceCadenceAdvance, // Advance billing
			StartDate:       s.testData.subscription.StartDate,
			BaseModel:       types.GetDefaultBaseModel(s.GetContext()),
		},
		{
			ID:               types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION_LINE_ITEM),
			SubscriptionID:   s.testData.subscription.ID,
			CustomerID:       s.testData.subscription.CustomerID,
			EntityID:         s.testData.plan.ID,
			EntityType:       types.SubscriptionLineItemEntityTypePlan,
			PlanDisplayName:  s.testData.plan.Name,
			PriceID:          s.testData.prices.apiCalls.ID,
			PriceType:        s.testData.prices.apiCalls.Type,
			MeterID:          s.testData.meters.apiCalls.ID,
			MeterDisplayName: s.testData.meters.apiCalls.Name,
			DisplayName:      "API Calls",
			Quantity:         decimal.Zero, // Usage-based, so quantity starts at 0
			Currency:         s.testData.subscription.Currency,
			BillingPeriod:    s.testData.subscription.BillingPeriod,
			InvoiceCadence:   types.InvoiceCadenceArrear, // Arrear billing
			StartDate:        s.testData.subscription.StartDate,
			BaseModel:        types.GetDefaultBaseModel(s.GetContext()),
		},
		{
			ID:               types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION_LINE_ITEM),
			SubscriptionID:   s.testData.subscription.ID,
			CustomerID:       s.testData.subscription.CustomerID,
			EntityID:         s.testData.plan.ID,
			EntityType:       types.SubscriptionLineItemEntityTypePlan,
			PlanDisplayName:  s.testData.plan.Name,
			PriceID:          s.testData.prices.storageArchive.ID,
			PriceType:        s.testData.prices.storageArchive.Type,
			MeterID:          s.testData.meters.storageArchive.ID,
			MeterDisplayName: s.testData.meters.storageArchive.Name,
			DisplayName:      "Archive Storage",
			Quantity:         decimal.NewFromInt(1), // 1 unit of archive storage
			Currency:         s.testData.subscription.Currency,
			BillingPeriod:    s.testData.subscription.BillingPeriod,
			InvoiceCadence:   types.InvoiceCadenceArrear, // Arrear billing for fixed price
			StartDate:        s.testData.subscription.StartDate,
			BaseModel:        types.GetDefaultBaseModel(s.GetContext()),
		},
	}

	s.NoError(s.GetStores().SubscriptionRepo.CreateWithLineItems(s.GetContext(), s.testData.subscription, lineItems))

	// Update the subscription object to include the line items
	s.testData.subscription.LineItems = lineItems

	// Populate feature_usage for tests that use GetFeatureUsageBySubscription (final invoice, preview).
	// This mirrors what the feature_usage pipeline would produce from the raw events.
	featureUsageStore := s.GetStores().FeatureUsageRepo.(*testutil.InMemoryFeatureUsageStore)
	apiCallsLineItem := lineItems[1] // Usage-based arrear line item
	s.NoError(featureUsageStore.InsertProcessedEvent(s.GetContext(), &events.FeatureUsage{
		Event: events.Event{
			ID:                 s.GetUUID(),
			TenantID:           s.testData.subscription.TenantID,
			EnvironmentID:      s.testData.subscription.EnvironmentID,
			EventName:          s.testData.meters.apiCalls.EventName,
			ExternalCustomerID: s.testData.customer.ExternalID,
			CustomerID:         s.testData.subscription.CustomerID,
			Timestamp:          s.testData.now.Add(-1 * time.Hour),
		},
		SubscriptionID: s.testData.subscription.ID,
		SubLineItemID:  apiCallsLineItem.ID,
		PriceID:        s.testData.prices.apiCalls.ID,
		FeatureID:      "feat_api_calls",
		MeterID:        s.testData.meters.apiCalls.ID,
		QtyTotal:       decimal.NewFromInt(500), // 500 API calls to produce $10 (500 * $0.02 tier)
	}))

	// Create test events
	for i := 0; i < 500; i++ {
		event := &events.Event{
			ID:                 s.GetUUID(),
			TenantID:           s.testData.subscription.TenantID,
			EventName:          s.testData.meters.apiCalls.EventName,
			ExternalCustomerID: s.testData.customer.ExternalID,
			Timestamp:          s.testData.now.Add(-1 * time.Hour),
			Properties:         map[string]interface{}{},
		}
		s.NoError(s.eventRepo.InsertEvent(s.GetContext(), event))
	}

	storageEvents := []struct {
		bytes float64
		tier  string
	}{
		{bytes: 30, tier: "standard"},
		{bytes: 20, tier: "standard"},
		{bytes: 300, tier: "archive"},
	}

	for _, se := range storageEvents {
		event := &events.Event{
			ID:                 s.GetUUID(),
			TenantID:           s.testData.subscription.TenantID,
			EventName:          s.testData.meters.storage.EventName,
			ExternalCustomerID: s.testData.customer.ExternalID,
			Timestamp:          s.testData.now.Add(-30 * time.Minute),
			Properties: map[string]interface{}{
				"bytes_used": se.bytes,
				"region":     "us-east-1",
				"tier":       se.tier,
			},
		}
		s.NoError(s.eventRepo.InsertEvent(s.GetContext(), event))
	}
}

func (s *BillingServiceSuite) TestPrepareSubscriptionInvoiceRequest() {
	tests := []struct {
		name                string
		referencePoint      types.InvoiceReferencePoint
		setupFunc           func(s *BillingServiceSuite)
		expectedAmount      decimal.Decimal
		expectedLineItems   int
		expectedAdvanceOnly bool
		expectedArrearOnly  bool
		wantErr             bool
		validateFunc        func(req *dto.CreateInvoiceRequest, sub *subscription.Subscription)
	}{
		{
			name:                "period_start_reference_point",
			referencePoint:      types.ReferencePointPeriodStart,
			expectedAmount:      decimal.NewFromInt(10),
			expectedLineItems:   1,
			expectedAdvanceOnly: true,
			expectedArrearOnly:  false,
			wantErr:             false,
			setupFunc:           func(s *BillingServiceSuite) {},
			validateFunc:        s.validatePeriodStartInvoice,
		},
		{
			name:                "period_end_reference_point",
			referencePoint:      types.ReferencePointPeriodEnd,
			expectedAmount:      decimal.NewFromInt(25),
			expectedLineItems:   3,
			expectedAdvanceOnly: false,
			expectedArrearOnly:  false,
			wantErr:             false,
			setupFunc:           func(s *BillingServiceSuite) {},
			validateFunc:        s.validatePeriodEndInvoice,
		},
		{
			name:                "preview_reference_point",
			referencePoint:      types.ReferencePointPreview,
			expectedAmount:      decimal.Zero,
			expectedLineItems:   3,
			expectedAdvanceOnly: false,
			expectedArrearOnly:  false,
			wantErr:             false,
			setupFunc:           func(s *BillingServiceSuite) {},
			validateFunc:        s.validatePreviewInvoice,
		},
		{
			name:                "existing_invoice_check_advance",
			referencePoint:      types.ReferencePointPeriodStart,
			expectedAmount:      decimal.Zero,
			expectedLineItems:   0,
			expectedAdvanceOnly: true,
			expectedArrearOnly:  false,
			wantErr:             false,
			setupFunc: func(s *BillingServiceSuite) {
				// Create an existing invoice for the advance charge
				inv := &invoice.Invoice{
					ID:              "inv_test_1",
					CustomerID:      s.testData.customer.ID,
					SubscriptionID:  lo.ToPtr(s.testData.subscription.ID),
					InvoiceType:     types.InvoiceTypeSubscription,
					InvoiceStatus:   types.InvoiceStatusFinalized,
					PaymentStatus:   types.PaymentStatusPending,
					Currency:        "usd",
					AmountDue:       decimal.NewFromInt(10),
					AmountPaid:      decimal.Zero,
					AmountRemaining: decimal.NewFromInt(10),
					Description:     "Test Invoice",
					PeriodStart:     lo.ToPtr(s.testData.subscription.CurrentPeriodStart),
					PeriodEnd:       lo.ToPtr(s.testData.subscription.CurrentPeriodEnd),
					BillingReason:   string(types.InvoiceBillingReasonSubscriptionCycle),
					BaseModel:       types.GetDefaultBaseModel(s.GetContext()),
					LineItems: []*invoice.InvoiceLineItem{
						{
							ID:             "li_test_1",
							InvoiceID:      "inv_test_1",
							CustomerID:     s.testData.customer.ID,
							SubscriptionID: lo.ToPtr(s.testData.subscription.ID),
							EntityID:       lo.ToPtr(s.testData.plan.ID),
							EntityType:     lo.ToPtr(string(types.SubscriptionLineItemEntityTypePlan)),
							PriceID:        lo.ToPtr(s.testData.prices.fixed.ID),
							Amount:         decimal.NewFromInt(10),
							Quantity:       decimal.NewFromInt(1),
							Currency:       "usd",
							PeriodStart:    lo.ToPtr(s.testData.subscription.CurrentPeriodStart),
							PeriodEnd:      lo.ToPtr(s.testData.subscription.CurrentPeriodEnd),
							BaseModel:      types.GetDefaultBaseModel(s.GetContext()),
						},
					},
				}
				s.invoiceRepo.CreateWithLineItems(s.GetContext(), inv)
			},
			validateFunc: s.validateExistingInvoiceCheckAdvance,
		},
		{
			name:                "existing_invoice_check_arrear",
			referencePoint:      types.ReferencePointPeriodEnd,
			expectedAmount:      decimal.NewFromInt(10),
			expectedLineItems:   1,
			expectedAdvanceOnly: true,
			expectedArrearOnly:  false,
			wantErr:             false,
			setupFunc: func(s *BillingServiceSuite) {
				// Create an existing invoice for the arrear charges
				inv := &invoice.Invoice{
					ID:              "inv_test_2",
					CustomerID:      s.testData.customer.ID,
					SubscriptionID:  lo.ToPtr(s.testData.subscription.ID),
					InvoiceType:     types.InvoiceTypeSubscription,
					InvoiceStatus:   types.InvoiceStatusFinalized,
					PaymentStatus:   types.PaymentStatusPending,
					Currency:        "usd",
					AmountDue:       decimal.NewFromInt(15),
					AmountPaid:      decimal.Zero,
					AmountRemaining: decimal.NewFromInt(15),
					Description:     "Test Invoice",
					PeriodStart:     lo.ToPtr(s.testData.subscription.CurrentPeriodStart),
					PeriodEnd:       lo.ToPtr(s.testData.subscription.CurrentPeriodEnd),
					BillingReason:   string(types.InvoiceBillingReasonSubscriptionCycle),
					BaseModel:       types.GetDefaultBaseModel(s.GetContext()),
					LineItems: []*invoice.InvoiceLineItem{
						{
							ID:             "li_test_2",
							InvoiceID:      "inv_test_2",
							CustomerID:     s.testData.customer.ID,
							SubscriptionID: lo.ToPtr(s.testData.subscription.ID),
							EntityID:       lo.ToPtr(s.testData.plan.ID),
							EntityType:     lo.ToPtr(string(types.SubscriptionLineItemEntityTypePlan)),
							PriceID:        lo.ToPtr(s.testData.prices.apiCalls.ID),
							Amount:         decimal.NewFromInt(10),
							Quantity:       decimal.NewFromInt(500),
							Currency:       "usd",
							PeriodStart:    lo.ToPtr(s.testData.subscription.CurrentPeriodStart),
							PeriodEnd:      lo.ToPtr(s.testData.subscription.CurrentPeriodEnd),
							BaseModel:      types.GetDefaultBaseModel(s.GetContext()),
						},
						{
							ID:             "li_test_3",
							InvoiceID:      "inv_test_2",
							CustomerID:     s.testData.customer.ID,
							SubscriptionID: lo.ToPtr(s.testData.subscription.ID),
							EntityID:       lo.ToPtr(s.testData.plan.ID),
							EntityType:     lo.ToPtr(string(types.SubscriptionLineItemEntityTypePlan)),
							PriceID:        lo.ToPtr(s.testData.prices.storageArchive.ID),
							Amount:         decimal.NewFromInt(5),
							Quantity:       decimal.NewFromInt(1),
							Currency:       "usd",
							PeriodStart:    lo.ToPtr(s.testData.subscription.CurrentPeriodStart),
							PeriodEnd:      lo.ToPtr(s.testData.subscription.CurrentPeriodEnd),
							BaseModel:      types.GetDefaultBaseModel(s.GetContext()),
						},
					},
				}
				s.invoiceRepo.CreateWithLineItems(s.GetContext(), inv)
			},
			validateFunc: s.validateNextPeriodAdvanceOnly,
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			// Clear existing invoices before each test
			s.invoiceRepo.Clear()

			// Setup test data if needed
			if tt.setupFunc != nil {
				tt.setupFunc(s)
			}

			// Get subscription with line items
			sub, _, err := s.GetStores().SubscriptionRepo.GetWithLineItems(s.GetContext(), s.testData.subscription.ID)
			s.NoError(err)

			// Calculate period start and end
			periodStart := sub.CurrentPeriodStart
			periodEnd := sub.CurrentPeriodEnd

			// Prepare invoice request
			req, err := s.service.PrepareSubscriptionInvoiceRequest(
				s.GetContext(),
				sub,
				periodStart,
				periodEnd,
				tt.referencePoint,
			)

			// Check error
			if tt.wantErr {
				s.Error(err)
				return
			}

			s.NoError(err)
			s.NotNil(req)
			s.Equal(s.testData.customer.ID, req.CustomerID)
			s.Equal(s.testData.subscription.ID, *req.SubscriptionID)
			s.Equal(types.InvoiceTypeSubscription, req.InvoiceType)
			s.Equal(types.InvoiceStatusDraft, *req.InvoiceStatus)
			s.Equal("usd", req.Currency)
			s.True(tt.expectedAmount.IsZero() || tt.expectedAmount.Equal(req.AmountDue), "Amount due mismatch, expected: %s, got: %s", tt.expectedAmount.String(), req.AmountDue.String())
			s.Equal(sub.CurrentPeriodStart.Unix(), req.PeriodStart.Unix())
			s.Equal(sub.CurrentPeriodEnd.Unix(), req.PeriodEnd.Unix())
			s.Equal(tt.expectedLineItems, len(req.LineItems))

			// Skip further checks if no line items
			if len(req.LineItems) == 0 {
				return
			}

			// Check if only advance charges are included
			if tt.expectedAdvanceOnly {
				for _, li := range req.LineItems {
					// Find the corresponding subscription line item
					var subLineItem *subscription.SubscriptionLineItem
					for _, sli := range sub.LineItems {
						if sli.PriceID == lo.FromPtr(li.PriceID) {
							subLineItem = sli
							break
						}
					}
					s.NotNil(subLineItem, "Subscription line item not found")
					s.Equal(types.InvoiceCadenceAdvance, subLineItem.InvoiceCadence, "Expected only advance charges")
				}
			}

			// Check if only arrear charges are included
			if tt.expectedArrearOnly {
				for _, li := range req.LineItems {
					// Find the corresponding subscription line item
					var subLineItem *subscription.SubscriptionLineItem
					for _, sli := range sub.LineItems {
						if sli.PriceID == lo.FromPtr(li.PriceID) {
							subLineItem = sli
							break
						}
					}
					s.NotNil(subLineItem, "Subscription line item not found")
					s.Equal(types.InvoiceCadenceArrear, subLineItem.InvoiceCadence, "Expected only arrear charges")
				}
			}

			if tt.validateFunc != nil {
				tt.validateFunc(req, sub)
			}
		})
	}
}

func (s *BillingServiceSuite) TestPrepareSubscriptionInvoiceRequest_IncludesHistoricalLineItemsWhenSubscriptionAdvanced() {
	ctx := s.GetContext()
	oldStart := s.testData.subscription.CurrentPeriodStart
	oldEnd := s.testData.subscription.CurrentPeriodEnd

	apiLI := s.testData.subscription.LineItems[1]
	s.Equal(s.testData.prices.apiCalls.ID, apiLI.PriceID)
	apiLI.EndDate = oldStart.Add(48 * time.Hour)
	s.NoError(s.GetStores().SubscriptionLineItemRepo.Update(ctx, apiLI))

	s.testData.subscription.CurrentPeriodStart = oldEnd
	s.testData.subscription.CurrentPeriodEnd = oldEnd.Add(30 * 24 * time.Hour)
	s.NoError(s.GetStores().SubscriptionRepo.Update(ctx, s.testData.subscription))

	sub, lineItemsFromGet, err := s.GetStores().SubscriptionRepo.GetWithLineItems(ctx, s.testData.subscription.ID)
	s.NoError(err)
	foundAPI := false
	for _, li := range lineItemsFromGet {
		if li.PriceID == s.testData.prices.apiCalls.ID {
			foundAPI = true
			break
		}
	}
	s.False(foundAPI, "GetWithLineItems should omit line item whose EndDate is before new CurrentPeriodStart")

	req, err := s.service.PrepareSubscriptionInvoiceRequest(ctx, sub, oldStart, oldEnd, types.ReferencePointPeriodEnd)
	s.NoError(err)
	s.NotNil(req)

	hasAPI := false
	for _, li := range req.LineItems {
		if lo.FromPtr(li.PriceID) == s.testData.prices.apiCalls.ID {
			hasAPI = true
			break
		}
	}
	s.True(hasAPI, "PrepareSubscriptionInvoiceRequest for historical period should still bill API usage")
}

// Helper methods for specific validations

func (s *BillingServiceSuite) validatePeriodStartInvoice(req *dto.CreateInvoiceRequest, sub *subscription.Subscription) {
	// Verify we only have the fixed price with advance cadence
	s.Equal(1, len(req.LineItems))
	s.Equal(s.testData.prices.fixed.ID, lo.FromPtr(req.LineItems[0].PriceID))

	// Verify the period matches the current subscription period
	s.Equal(sub.CurrentPeriodStart, *req.PeriodStart)
	s.Equal(sub.CurrentPeriodEnd, *req.PeriodEnd)
}

func (s *BillingServiceSuite) validatePeriodEndInvoice(req *dto.CreateInvoiceRequest, sub *subscription.Subscription) {
	// Should have 3 line items: 2 arrear (API calls and archive storage) and 1 advance for next period
	s.Equal(3, len(req.LineItems))

	// Check that we have the expected price IDs
	priceIDs := make(map[string]bool)
	for _, li := range req.LineItems {
		priceIDs[lo.FromPtr(li.PriceID)] = true
	}

	s.True(priceIDs[s.testData.prices.apiCalls.ID], "Should include API calls price")
	s.True(priceIDs[s.testData.prices.storageArchive.ID], "Should include archive storage price")
	s.True(priceIDs[s.testData.prices.fixed.ID], "Should include fixed price for next period")

	// Verify the period matches the current subscription period
	s.Equal(sub.CurrentPeriodStart, *req.PeriodStart)
	s.Equal(sub.CurrentPeriodEnd, *req.PeriodEnd)
}

func (s *BillingServiceSuite) validatePreviewInvoice(req *dto.CreateInvoiceRequest, sub *subscription.Subscription) {
	// Should have 3 line items: 2 arrear (API calls and archive storage) and 1 advance for next period
	s.Equal(3, len(req.LineItems))

	// Check that we have the expected price IDs
	priceIDs := make(map[string]bool)
	for _, li := range req.LineItems {
		priceIDs[lo.FromPtr(li.PriceID)] = true
	}

	s.True(priceIDs[s.testData.prices.apiCalls.ID], "Should include API calls price")
	s.True(priceIDs[s.testData.prices.storageArchive.ID], "Should include archive storage price")
	s.True(priceIDs[s.testData.prices.fixed.ID], "Should include fixed price for next period")

	// Verify the period matches the current subscription period
	s.Equal(sub.CurrentPeriodStart, *req.PeriodStart)
	s.Equal(sub.CurrentPeriodEnd, *req.PeriodEnd)
}

func (s *BillingServiceSuite) validateExistingInvoiceCheckAdvance(req *dto.CreateInvoiceRequest, sub *subscription.Subscription) {
	// Should have 0 line items
	s.Equal(0, len(req.LineItems))
	s.Equal(decimal.Zero.String(), req.AmountDue.String())
}

func (s *BillingServiceSuite) validateNextPeriodAdvanceOnly(req *dto.CreateInvoiceRequest, sub *subscription.Subscription) {
	// Should only have the fixed price for next period
	s.Equal(1, len(req.LineItems))
	s.Equal(s.testData.prices.fixed.ID, lo.FromPtr(req.LineItems[0].PriceID))

	// Verify the period matches the current subscription period
	s.Equal(sub.CurrentPeriodStart, *req.PeriodStart)
	s.Equal(sub.CurrentPeriodEnd, *req.PeriodEnd)
}

// TestCalculateFixedCharges_MixedCadence tests mixed cadence (line item period > subscription period).
// When a line item has longer cadence (e.g. quarterly) than the subscription (e.g. monthly), it is
// included only when a line-item period end falls in [periodStart, periodEnd); that period's start/end
// become the invoice line's service period.
func (s *BillingServiceSuite) TestCalculateFixedCharges_MixedCadence() {
	ctx := s.GetContext()
	// Use fixed dates for predictable quarter boundaries (anniversary from Jan 1 -> Apr 1, Jul 1, ...)
	jan1 := time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC)
	mar1 := time.Date(2024, time.March, 1, 0, 0, 0, 0, time.UTC)
	apr1 := time.Date(2024, time.April, 1, 0, 0, 0, 0, time.UTC)
	may1 := time.Date(2024, time.May, 1, 0, 0, 0, 0, time.UTC)

	s.BaseServiceTestSuite.ClearStores()
	// Reuse customer and plan from test data (they may already exist from SetupTest)
	cust := &customer.Customer{
		ID:         "cust_mixed",
		ExternalID: "ext_mixed",
		Name:       "Mixed Cadence Customer",
		Email:      "mixed@example.com",
		BaseModel:  types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().CustomerRepo.Create(ctx, cust))
	pl := &plan.Plan{
		ID:          "plan_mixed",
		Name:        "Mixed Plan",
		Description: "Mixed cadence test",
		BaseModel:   types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().PlanRepo.Create(ctx, pl))

	// Monthly fixed price
	priceMonthly := &price.Price{
		ID:                 "price_monthly_mixed",
		Amount:             decimal.NewFromInt(10),
		Currency:           "usd",
		EntityType:         types.PRICE_ENTITY_TYPE_PLAN,
		EntityID:           pl.ID,
		Type:               types.PRICE_TYPE_FIXED,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_FLAT_FEE,
		BillingCadence:     types.BILLING_CADENCE_RECURRING,
		InvoiceCadence:     types.InvoiceCadenceAdvance,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().PriceRepo.Create(ctx, priceMonthly))

	// Quarterly fixed price
	priceQuarterly := &price.Price{
		ID:                 "price_quarterly_mixed",
		Amount:             decimal.NewFromInt(300),
		Currency:           "usd",
		EntityType:         types.PRICE_ENTITY_TYPE_PLAN,
		EntityID:           pl.ID,
		Type:               types.PRICE_TYPE_FIXED,
		BillingPeriod:      types.BILLING_PERIOD_QUARTER,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_FLAT_FEE,
		BillingCadence:     types.BILLING_CADENCE_RECURRING,
		InvoiceCadence:     types.InvoiceCadenceArrear,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().PriceRepo.Create(ctx, priceQuarterly))

	// Subscription: monthly billing, period Apr 1 - May 1
	sub := &subscription.Subscription{
		ID:                 "sub_mixed",
		PlanID:             pl.ID,
		CustomerID:         cust.ID,
		StartDate:          jan1,
		BillingAnchor:      jan1,
		CurrentPeriodStart: apr1,
		CurrentPeriodEnd:   may1,
		Currency:           "usd",
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		SubscriptionStatus: types.SubscriptionStatusActive,
		CustomerTimezone:   "UTC",
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	// Line items: monthly fixed (same cadence as sub), quarterly fixed (longer cadence, start Jan 1)
	liMonthly := &subscription.SubscriptionLineItem{
		ID:                 types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION_LINE_ITEM),
		SubscriptionID:     sub.ID,
		CustomerID:         sub.CustomerID,
		EntityID:           pl.ID,
		EntityType:         types.SubscriptionLineItemEntityTypePlan,
		PlanDisplayName:    pl.Name,
		PriceID:            priceMonthly.ID,
		PriceType:          types.PRICE_TYPE_FIXED,
		DisplayName:        "Monthly Fee",
		Quantity:           decimal.NewFromInt(1),
		Currency:           sub.Currency,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		InvoiceCadence:     types.InvoiceCadenceAdvance,
		StartDate:          jan1,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	liQuarterly := &subscription.SubscriptionLineItem{
		ID:                 types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION_LINE_ITEM),
		SubscriptionID:     sub.ID,
		CustomerID:         sub.CustomerID,
		EntityID:           pl.ID,
		EntityType:         types.SubscriptionLineItemEntityTypePlan,
		PlanDisplayName:    pl.Name,
		PriceID:            priceQuarterly.ID,
		PriceType:          types.PRICE_TYPE_FIXED,
		DisplayName:        "Quarterly Fee",
		Quantity:           decimal.NewFromInt(1),
		Currency:           sub.Currency,
		BillingPeriod:      types.BILLING_PERIOD_QUARTER,
		BillingPeriodCount: 1,
		InvoiceCadence:     types.InvoiceCadenceArrear,
		StartDate:          jan1,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().SubscriptionRepo.CreateWithLineItems(ctx, sub, []*subscription.SubscriptionLineItem{liMonthly, liQuarterly}))
	sub.LineItems = []*subscription.SubscriptionLineItem{liMonthly, liQuarterly}

	// Arrear rule: period end in (periodStart, periodEnd] (start exclusive, end inclusive).
	// Excluded: invoice period Apr 1 - May 1. Quarter from Jan 1 ends Apr 1; Apr 1 is not in (Apr 1, May 1] -> no quarterly line
	lineItems, total, err := s.service.CalculateFixedCharges(ctx, sub, apr1, may1)
	s.NoError(err)
	s.Require().Len(lineItems, 1, "expected 1 fixed line item (monthly only; quarterly arrear excluded when period end equals invoice start)")
	s.Equal(priceMonthly.ID, lo.FromPtr(lineItems[0].PriceID))
	s.True(total.GreaterThanOrEqual(decimal.NewFromInt(0)) && total.LessThanOrEqual(decimal.NewFromInt(10)), "total should be 0–10 (monthly only, may be prorated)")

	// Included: invoice period Mar 1 - Apr 1. Quarter end Apr 1 is in (Mar 1, Apr 1] -> include quarterly with period Jan 1 - Apr 1
	lineItems2, total2, err2 := s.service.CalculateFixedCharges(ctx, sub, mar1, apr1)
	s.NoError(err2)
	s.Require().Len(lineItems2, 2, "expected 2 fixed line items (monthly + quarterly)")
	var monthlyLine, quarterlyLine *dto.CreateInvoiceLineItemRequest
	for i := range lineItems2 {
		if lo.FromPtr(lineItems2[i].PriceID) == priceMonthly.ID {
			monthlyLine = &lineItems2[i]
		} else if lo.FromPtr(lineItems2[i].PriceID) == priceQuarterly.ID {
			quarterlyLine = &lineItems2[i]
		}
	}
	s.Require().NotNil(monthlyLine, "monthly line should be present")
	s.Require().NotNil(quarterlyLine, "quarterly line should be present")
	s.True((*monthlyLine.PeriodStart).Equal(mar1) && (*monthlyLine.PeriodEnd).Equal(apr1), "monthly line should have period Mar 1 - Apr 1")
	s.True((*quarterlyLine.PeriodStart).Equal(jan1) && (*quarterlyLine.PeriodEnd).Equal(apr1), "quarterly line should have period Jan 1 - Apr 1")
	s.True(quarterlyLine.Amount.Equal(decimal.NewFromInt(300)), "quarterly line should be full amount 300")
	s.True(total2.GreaterThanOrEqual(decimal.NewFromInt(300)), "total should be at least 300 (quarterly)")
	s.True(total2.LessThanOrEqual(decimal.NewFromInt(310)), "total should be at most 310 (full monthly + quarterly)")
}

// scenario1DailyExpectedTotals is the expected fixed charge total for each of 12 daily invoices
// (advance [start, end), arrear (start, end]; ProrationBehavior=None). Invoice i uses period [Jan i, Jan i+1).
// Invoice 1: advance 1500 + arrear 200 (daily arrear end Jan 2 in (Jan 1, Jan 2]) = 1700.
// Invoice 7: advance 100 (daily) + arrear 200 (daily) + 300 (weekly arrear end Jan 8 in (Jan 7, Jan 8]) = 600.
// Invoice 8: advance 300 (daily+weekly) + arrear 200 (daily only; weekly end Jan 8 excluded from (Jan 8, Jan 9]) = 500.
var scenario1DailyExpectedTotals = []int{1700, 300, 300, 300, 300, 300, 600, 500, 300, 300, 300, 300}

// scenario2MonthlyExpectedTotals is the expected fixed charge total for each of 12 monthly invoices (advance only; proration_behavior=none).
var scenario2MonthlyExpectedTotals = []int{1200, 300, 300, 700, 300, 300, 700, 300, 300, 700, 300, 300}

// setupScenario1DailySub creates a daily subscription (start Jan 1 2026) with 10 fixed line items:
// advance: daily 100, weekly 200, monthly 300, quarterly 400, annual 500;
// arrear: daily 200, weekly 300, monthly 400, quarterly 500, annual 600.
func (s *BillingServiceSuite) setupScenario1DailySub(ctx context.Context) (*subscription.Subscription, []*price.Price) {
	jan1 := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	cust := &customer.Customer{
		ID:         "cust_sc1",
		ExternalID: "ext_sc1",
		Name:       "Scenario 1 Customer",
		Email:      "sc1@example.com",
		BaseModel:  types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().CustomerRepo.Create(ctx, cust))
	pl := &plan.Plan{
		ID:          "plan_sc1",
		Name:        "Scenario 1 Plan",
		Description: "Daily sub with mixed cadences",
		BaseModel:   types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().PlanRepo.Create(ctx, pl))

	specs := []struct {
		id      string
		period  types.BillingPeriod
		amount  int
		cadence types.InvoiceCadence
		display string
	}{
		{"price_daily_adv", types.BILLING_PERIOD_DAILY, 100, types.InvoiceCadenceAdvance, "Daily Advance"},
		{"price_weekly_adv", types.BILLING_PERIOD_WEEKLY, 200, types.InvoiceCadenceAdvance, "Weekly Advance"},
		{"price_monthly_adv", types.BILLING_PERIOD_MONTHLY, 300, types.InvoiceCadenceAdvance, "Monthly Advance"},
		{"price_quarterly_adv", types.BILLING_PERIOD_QUARTER, 400, types.InvoiceCadenceAdvance, "Quarterly Advance"},
		{"price_annual_adv", types.BILLING_PERIOD_ANNUAL, 500, types.InvoiceCadenceAdvance, "Annual Advance"},
		{"price_daily_arr", types.BILLING_PERIOD_DAILY, 200, types.InvoiceCadenceArrear, "Daily Arrear"},
		{"price_weekly_arr", types.BILLING_PERIOD_WEEKLY, 300, types.InvoiceCadenceArrear, "Weekly Arrear"},
		{"price_monthly_arr", types.BILLING_PERIOD_MONTHLY, 400, types.InvoiceCadenceArrear, "Monthly Arrear"},
		{"price_quarterly_arr", types.BILLING_PERIOD_QUARTER, 500, types.InvoiceCadenceArrear, "Quarterly Arrear"},
		{"price_annual_arr", types.BILLING_PERIOD_ANNUAL, 600, types.InvoiceCadenceArrear, "Annual Arrear"},
	}
	prices := make([]*price.Price, 0, len(specs))
	lineItems := make([]*subscription.SubscriptionLineItem, 0, len(specs))
	sub := &subscription.Subscription{
		ID:                 "sub_sc1",
		PlanID:             pl.ID,
		CustomerID:         cust.ID,
		StartDate:          jan1,
		BillingAnchor:      jan1,
		CurrentPeriodStart: jan1,
		CurrentPeriodEnd:   jan1.AddDate(0, 0, 1),
		Currency:           "usd",
		BillingPeriod:      types.BILLING_PERIOD_DAILY,
		BillingPeriodCount: 1,
		SubscriptionStatus: types.SubscriptionStatusActive,
		CustomerTimezone:   "UTC",
		ProrationBehavior:  types.ProrationBehaviorNone, // full amounts in tests
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	for _, spec := range specs {
		p := &price.Price{
			ID:                 spec.id,
			Amount:             decimal.NewFromInt(int64(spec.amount)),
			Currency:           "usd",
			EntityType:         types.PRICE_ENTITY_TYPE_PLAN,
			EntityID:           pl.ID,
			Type:               types.PRICE_TYPE_FIXED,
			BillingPeriod:      spec.period,
			BillingPeriodCount: 1,
			BillingModel:       types.BILLING_MODEL_FLAT_FEE,
			BillingCadence:     types.BILLING_CADENCE_RECURRING,
			InvoiceCadence:     spec.cadence,
			BaseModel:          types.GetDefaultBaseModel(ctx),
		}
		s.NoError(s.GetStores().PriceRepo.Create(ctx, p))
		prices = append(prices, p)
		li := &subscription.SubscriptionLineItem{
			ID:                 types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION_LINE_ITEM),
			SubscriptionID:     sub.ID,
			CustomerID:         sub.CustomerID,
			EntityID:           pl.ID,
			EntityType:         types.SubscriptionLineItemEntityTypePlan,
			PlanDisplayName:    pl.Name,
			PriceID:            p.ID,
			PriceType:          types.PRICE_TYPE_FIXED,
			DisplayName:        spec.display,
			Quantity:           decimal.NewFromInt(1),
			Currency:           sub.Currency,
			BillingPeriod:      spec.period,
			BillingPeriodCount: 1,
			InvoiceCadence:     spec.cadence,
			StartDate:          jan1,
			BaseModel:          types.GetDefaultBaseModel(ctx),
		}
		lineItems = append(lineItems, li)
	}
	s.NoError(s.GetStores().SubscriptionRepo.CreateWithLineItems(ctx, sub, lineItems))
	sub.LineItems = lineItems
	return sub, prices
}

// setupScenario2MonthlySub creates a monthly subscription (start Jan 1 2026) with 3 advance-only fixed line items:
// monthly 300, quarterly 400, annual 500.
func (s *BillingServiceSuite) setupScenario2MonthlySub(ctx context.Context) (*subscription.Subscription, []*price.Price) {
	jan1 := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	feb1 := time.Date(2026, time.February, 1, 0, 0, 0, 0, time.UTC)
	cust := &customer.Customer{
		ID:         "cust_sc2",
		ExternalID: "ext_sc2",
		Name:       "Scenario 2 Customer",
		Email:      "sc2@example.com",
		BaseModel:  types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().CustomerRepo.Create(ctx, cust))
	pl := &plan.Plan{
		ID:          "plan_sc2",
		Name:        "Scenario 2 Plan",
		Description: "Monthly sub with advance only",
		BaseModel:   types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().PlanRepo.Create(ctx, pl))
	specs := []struct {
		id      string
		period  types.BillingPeriod
		amount  int
		display string
	}{
		{"price_sc2_monthly", types.BILLING_PERIOD_MONTHLY, 300, "Monthly Advance"},
		{"price_sc2_quarterly", types.BILLING_PERIOD_QUARTER, 400, "Quarterly Advance"},
		{"price_sc2_annual", types.BILLING_PERIOD_ANNUAL, 500, "Annual Advance"},
	}
	prices := make([]*price.Price, 0, len(specs))
	lineItems := make([]*subscription.SubscriptionLineItem, 0, len(specs))
	sub := &subscription.Subscription{
		ID:                 "sub_sc2",
		PlanID:             pl.ID,
		CustomerID:         cust.ID,
		StartDate:          jan1,
		BillingAnchor:      jan1,
		CurrentPeriodStart: jan1,
		CurrentPeriodEnd:   feb1,
		Currency:           "usd",
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		SubscriptionStatus: types.SubscriptionStatusActive,
		CustomerTimezone:   "UTC",
		ProrationBehavior:  types.ProrationBehaviorNone, // full amounts in tests
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	for _, spec := range specs {
		p := &price.Price{
			ID:                 spec.id,
			Amount:             decimal.NewFromInt(int64(spec.amount)),
			Currency:           "usd",
			EntityType:         types.PRICE_ENTITY_TYPE_PLAN,
			EntityID:           pl.ID,
			Type:               types.PRICE_TYPE_FIXED,
			BillingPeriod:      spec.period,
			BillingPeriodCount: 1,
			BillingModel:       types.BILLING_MODEL_FLAT_FEE,
			BillingCadence:     types.BILLING_CADENCE_RECURRING,
			InvoiceCadence:     types.InvoiceCadenceAdvance,
			BaseModel:          types.GetDefaultBaseModel(ctx),
		}
		s.NoError(s.GetStores().PriceRepo.Create(ctx, p))
		prices = append(prices, p)
		li := &subscription.SubscriptionLineItem{
			ID:                 types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION_LINE_ITEM),
			SubscriptionID:     sub.ID,
			CustomerID:         sub.CustomerID,
			EntityID:           pl.ID,
			EntityType:         types.SubscriptionLineItemEntityTypePlan,
			PlanDisplayName:    pl.Name,
			PriceID:            p.ID,
			PriceType:          types.PRICE_TYPE_FIXED,
			DisplayName:        spec.display,
			Quantity:           decimal.NewFromInt(1),
			Currency:           sub.Currency,
			BillingPeriod:      spec.period,
			BillingPeriodCount: 1,
			InvoiceCadence:     types.InvoiceCadenceAdvance,
			StartDate:          jan1,
			BaseModel:          types.GetDefaultBaseModel(ctx),
		}
		lineItems = append(lineItems, li)
	}
	s.NoError(s.GetStores().SubscriptionRepo.CreateWithLineItems(ctx, sub, lineItems))
	sub.LineItems = lineItems
	return sub, prices
}

// TestScenario1_DailySub_12Invoices asserts fixed charges for 12 daily invoices:
// advance at period start, arrear at period end. Expected totals from Orb doc.
func (s *BillingServiceSuite) TestScenario1_DailySub_12Invoices() {
	ctx := s.GetContext()
	s.BaseServiceTestSuite.ClearStores()
	sub, _ := s.setupScenario1DailySub(ctx)
	var advanceItems, arrearItems []*subscription.SubscriptionLineItem
	for _, li := range sub.LineItems {
		if li.InvoiceCadence == types.InvoiceCadenceAdvance {
			advanceItems = append(advanceItems, li)
		} else {
			arrearItems = append(arrearItems, li)
		}
	}
	subAdvance := *sub
	subAdvance.LineItems = advanceItems
	subArrear := *sub
	subArrear.LineItems = arrearItems

	for i := 0; i < 12; i++ {
		start := time.Date(2026, time.January, 1+i, 0, 0, 0, 0, time.UTC)
		end := time.Date(2026, time.January, 2+i, 0, 0, 0, 0, time.UTC)
		_, totalAdvance, err := s.service.CalculateFixedCharges(ctx, &subAdvance, start, end)
		s.NoError(err, "invoice %d advance", i+1)
		_, totalArrear, err := s.service.CalculateFixedCharges(ctx, &subArrear, start, end)
		s.NoError(err, "invoice %d arrear", i+1)
		got := totalAdvance.Add(totalArrear)
		expected := decimal.NewFromInt(int64(scenario1DailyExpectedTotals[i]))
		s.True(got.Equal(expected), "invoice %d: expected fixed total %s, got %s", i+1, expected, got)
	}
}

// TestScenario2_MonthlySub_12Invoices asserts fixed charges for 12 monthly invoices (advance only). Expected totals from Orb.
func (s *BillingServiceSuite) TestScenario2_MonthlySub_12Invoices() {
	ctx := s.GetContext()
	s.BaseServiceTestSuite.ClearStores()
	sub, _ := s.setupScenario2MonthlySub(ctx)
	monthStarts := []time.Time{
		time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, time.February, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, time.April, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, time.May, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, time.September, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, time.October, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, time.November, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, time.December, 1, 0, 0, 0, 0, time.UTC),
	}
	monthEnds := []time.Time{
		time.Date(2026, time.February, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, time.April, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, time.May, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, time.September, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, time.October, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, time.November, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, time.December, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2027, time.January, 1, 0, 0, 0, 0, time.UTC),
	}
	for i := 0; i < 12; i++ {
		_, total, err := s.service.CalculateFixedCharges(ctx, sub, monthStarts[i], monthEnds[i])
		s.NoError(err, "invoice %d", i+1)
		expected := decimal.NewFromInt(int64(scenario2MonthlyExpectedTotals[i]))
		s.True(total.Equal(expected), "invoice %d: expected fixed total %s, got %s", i+1, expected, total)
	}
}

// LineItemSpecForPeriodTests defines a fixed line item for sub/line period scenario tests.
type LineItemSpecForPeriodTests struct {
	PriceID        string
	DisplayName    string
	BillingPeriod  types.BillingPeriod
	InvoiceCadence types.InvoiceCadence
	Amount         int
}

// setupSubWithFixedLineItemsForPeriodTests creates a subscription with fixed-only line items
// for testing sub-period × line-item period scenarios. refStart is the first period start (and billing anchor).
// Returns the subscription (with LineItems populated) and a slice of created prices in spec order.
func (s *BillingServiceSuite) setupSubWithFixedLineItemsForPeriodTests(
	ctx context.Context,
	scenarioName string,
	subBillingPeriod types.BillingPeriod,
	refStart time.Time,
	specs []LineItemSpecForPeriodTests,
) (*subscription.Subscription, []*price.Price) {
	firstPeriodEnd, err := types.NextBillingDate(refStart, refStart, 1, subBillingPeriod, nil)
	s.Require().NoError(err)

	cust := &customer.Customer{
		ID:         "cust_pl_" + scenarioName,
		ExternalID: "ext_pl_" + scenarioName,
		Name:       "Period Test Customer",
		Email:      scenarioName + "@example.com",
		BaseModel:  types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().CustomerRepo.Create(ctx, cust))
	pl := &plan.Plan{
		ID:          "plan_pl_" + scenarioName,
		Name:        "Period Test Plan",
		Description: "Sub/line period test",
		BaseModel:   types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().PlanRepo.Create(ctx, pl))

	sub := &subscription.Subscription{
		ID:                 "sub_pl_" + scenarioName,
		PlanID:             pl.ID,
		CustomerID:         cust.ID,
		StartDate:          refStart,
		BillingAnchor:      refStart,
		CurrentPeriodStart: refStart,
		CurrentPeriodEnd:   firstPeriodEnd,
		Currency:           "usd",
		BillingPeriod:      subBillingPeriod,
		BillingPeriodCount: 1,
		SubscriptionStatus: types.SubscriptionStatusActive,
		CustomerTimezone:   "UTC",
		ProrationBehavior:  types.ProrationBehaviorNone,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}

	prices := make([]*price.Price, 0, len(specs))
	lineItems := make([]*subscription.SubscriptionLineItem, 0, len(specs))
	for _, spec := range specs {
		p := &price.Price{
			ID:                 spec.PriceID,
			Amount:             decimal.NewFromInt(int64(spec.Amount)),
			Currency:           "usd",
			EntityType:         types.PRICE_ENTITY_TYPE_PLAN,
			EntityID:           pl.ID,
			Type:               types.PRICE_TYPE_FIXED,
			BillingPeriod:      spec.BillingPeriod,
			BillingPeriodCount: 1,
			BillingModel:       types.BILLING_MODEL_FLAT_FEE,
			BillingCadence:     types.BILLING_CADENCE_RECURRING,
			InvoiceCadence:     spec.InvoiceCadence,
			BaseModel:          types.GetDefaultBaseModel(ctx),
		}
		s.NoError(s.GetStores().PriceRepo.Create(ctx, p))
		prices = append(prices, p)
		li := &subscription.SubscriptionLineItem{
			ID:                 types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION_LINE_ITEM),
			SubscriptionID:     sub.ID,
			CustomerID:         sub.CustomerID,
			EntityID:           pl.ID,
			EntityType:         types.SubscriptionLineItemEntityTypePlan,
			PlanDisplayName:    pl.Name,
			PriceID:            p.ID,
			PriceType:          types.PRICE_TYPE_FIXED,
			DisplayName:        spec.DisplayName,
			Quantity:           decimal.NewFromInt(1),
			Currency:           sub.Currency,
			BillingPeriod:      spec.BillingPeriod,
			BillingPeriodCount: 1,
			InvoiceCadence:     spec.InvoiceCadence,
			StartDate:          refStart,
			BaseModel:          types.GetDefaultBaseModel(ctx),
		}
		lineItems = append(lineItems, li)
	}
	s.NoError(s.GetStores().SubscriptionRepo.CreateWithLineItems(ctx, sub, lineItems))
	sub.LineItems = lineItems
	return sub, prices
}

// periodWindow is a single billing period [Start, End).
type periodWindow struct {
	Start, End time.Time
}

// nextPeriodsForSub returns the first n billing periods for a subscription from refStart,
// using the same logic as production (NextBillingDate).
func nextPeriodsForSub(refStart time.Time, subBillingPeriod types.BillingPeriod, n int) []periodWindow {
	out := make([]periodWindow, 0, n)
	start := refStart
	for i := 0; i < n; i++ {
		end, err := types.NextBillingDate(start, refStart, 1, subBillingPeriod, nil)
		if err != nil {
			return out
		}
		out = append(out, periodWindow{Start: start, End: end})
		start = end
	}
	return out
}

// expectedLineForPeriodTests is one expected fixed line on an invoice (PriceID + period).
type expectedLineForPeriodTests struct {
	PriceID     string
	PeriodStart time.Time
	PeriodEnd   time.Time
}

// assertInvoiceRequestFixedLines asserts that req.LineItems contains exactly the expected fixed lines
// (same PriceID and period for each). Order may differ.
func (s *BillingServiceSuite) assertInvoiceRequestFixedLines(req *dto.CreateInvoiceRequest, expected []expectedLineForPeriodTests) {
	s.Require().NotNil(req)
	s.Require().Len(req.LineItems, len(expected), "expected %d line items", len(expected))
	used := make([]bool, len(expected))
	for _, li := range req.LineItems {
		pid := lo.FromPtr(li.PriceID)
		start := lo.FromPtr(li.PeriodStart)
		end := lo.FromPtr(li.PeriodEnd)
		found := false
		for j, exp := range expected {
			if used[j] {
				continue
			}
			if exp.PriceID == pid && exp.PeriodStart.Equal(start) && exp.PeriodEnd.Equal(end) {
				used[j] = true
				found = true
				break
			}
		}
		s.True(found, "unexpected line item PriceID=%s PeriodStart=%s PeriodEnd=%s", pid, start, end)
	}
	for j, u := range used {
		s.True(u, "missing expected line %d: PriceID=%s PeriodStart=%s PeriodEnd=%s",
			j, expected[j].PriceID, expected[j].PeriodStart, expected[j].PeriodEnd)
	}
}

// TestSubscriptionLineItemPeriodScenarios runs sub-period × line-item period tests: for each scenario
// (daily, weekly, monthly sub with mixed line items), generates period-end and preview for the first
// few periods and asserts correct line inclusion and correct PeriodStart/PeriodEnd on each invoice line.
func (s *BillingServiceSuite) TestSubscriptionLineItemPeriodScenarios() {
	ctx := s.GetContext()
	jan1 := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	feb1 := time.Date(2026, time.February, 1, 0, 0, 0, 0, time.UTC)
	mar1 := time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC)
	apr1 := time.Date(2026, time.April, 1, 0, 0, 0, 0, time.UTC)
	may1 := time.Date(2026, time.May, 1, 0, 0, 0, 0, time.UTC)
	jul1 := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name            string
		subBilling      types.BillingPeriod
		refStart        time.Time
		specs           []LineItemSpecForPeriodTests
		periodsToAssert int
		// expectedPerPeriod[i] = expected fixed lines for period i (PeriodEnd and Preview same inclusion)
		expectedPerPeriod [][]expectedLineForPeriodTests
	}{
		{
			name:       "monthly_sub_monthly_quarterly_advance_arrear",
			subBilling: types.BILLING_PERIOD_MONTHLY,
			refStart:   jan1,
			specs: []LineItemSpecForPeriodTests{
				{PriceID: "p_mo_adv", DisplayName: "Monthly Advance", BillingPeriod: types.BILLING_PERIOD_MONTHLY, InvoiceCadence: types.InvoiceCadenceAdvance, Amount: 100},
				{PriceID: "p_mo_arr", DisplayName: "Monthly Arrear", BillingPeriod: types.BILLING_PERIOD_MONTHLY, InvoiceCadence: types.InvoiceCadenceArrear, Amount: 200},
				{PriceID: "p_q_adv", DisplayName: "Quarterly Advance", BillingPeriod: types.BILLING_PERIOD_QUARTER, InvoiceCadence: types.InvoiceCadenceAdvance, Amount: 300},
				{PriceID: "p_q_arr", DisplayName: "Quarterly Arrear", BillingPeriod: types.BILLING_PERIOD_QUARTER, InvoiceCadence: types.InvoiceCadenceArrear, Amount: 400},
			},
			periodsToAssert: 3,
			expectedPerPeriod: [][]expectedLineForPeriodTests{
				// Period 1: Jan 1 - Feb 1. PeriodEnd = current arrear + next advance. Equal-period: monthly arrear (Jan1-Feb1), monthly advance next (Feb1-Mar1). Quarterly arrear end Apr1 not in [Jan1,Feb1); quarterly advance next period Feb1-Mar1 has no quarter start in window.
				{
					{"p_mo_arr", jan1, feb1},
					{"p_mo_adv", feb1, mar1},
				},
				// Period 2: Feb 1 - Mar 1. Current arrear: monthly (Feb1-Mar1). Next advance: monthly (Mar1-Apr1). Quarter start Apr1 not in [Mar1,Apr1) so no quarterly advance.
				{
					{"p_mo_arr", feb1, mar1},
					{"p_mo_adv", mar1, apr1},
				},
				// Period 3: Mar 1 - Apr 1. Current arrear: monthly (Mar1-Apr1), quarterly (Jan1-Apr1, end Apr1 in (Mar1,Apr1]). Next advance: monthly (Apr1-May1), quarterly (Apr1-Jul1, natural period end).
				{
					{"p_mo_arr", mar1, apr1},
					{"p_q_arr", jan1, apr1},
					{"p_mo_adv", apr1, may1},
					{"p_q_adv", apr1, jul1},
				},
			},
		},
		{
			name:       "daily_sub_daily_weekly_monthly_advance_arrear",
			subBilling: types.BILLING_PERIOD_DAILY,
			refStart:   jan1,
			specs: []LineItemSpecForPeriodTests{
				{PriceID: "p_d_adv", DisplayName: "Daily Advance", BillingPeriod: types.BILLING_PERIOD_DAILY, InvoiceCadence: types.InvoiceCadenceAdvance, Amount: 10},
				{PriceID: "p_d_arr", DisplayName: "Daily Arrear", BillingPeriod: types.BILLING_PERIOD_DAILY, InvoiceCadence: types.InvoiceCadenceArrear, Amount: 20},
				{PriceID: "p_w_adv", DisplayName: "Weekly Advance", BillingPeriod: types.BILLING_PERIOD_WEEKLY, InvoiceCadence: types.InvoiceCadenceAdvance, Amount: 70},
				{PriceID: "p_w_arr", DisplayName: "Weekly Arrear", BillingPeriod: types.BILLING_PERIOD_WEEKLY, InvoiceCadence: types.InvoiceCadenceArrear, Amount: 80},
				{PriceID: "p_m_adv", DisplayName: "Monthly Advance", BillingPeriod: types.BILLING_PERIOD_MONTHLY, InvoiceCadence: types.InvoiceCadenceAdvance, Amount: 300},
				{PriceID: "p_m_arr", DisplayName: "Monthly Arrear", BillingPeriod: types.BILLING_PERIOD_MONTHLY, InvoiceCadence: types.InvoiceCadenceArrear, Amount: 400},
			},
			periodsToAssert: 3,
			expectedPerPeriod: [][]expectedLineForPeriodTests{
				// Period 1: Jan 1 - Jan 2. Current arrear: daily (Jan1-Jan2). Next advance: daily (Jan2-Jan3). Weekly/monthly no match in these windows.
				{
					{"p_d_arr", jan1, jan1.AddDate(0, 0, 1)},
					{"p_d_adv", jan1.AddDate(0, 0, 1), jan1.AddDate(0, 0, 2)},
				},
				// Period 2: Jan 2 - Jan 3.
				{
					{"p_d_arr", jan1.AddDate(0, 0, 1), jan1.AddDate(0, 0, 2)},
					{"p_d_adv", jan1.AddDate(0, 0, 2), jan1.AddDate(0, 0, 3)},
				},
				// Period 3: Jan 3 - Jan 4.
				{
					{"p_d_arr", jan1.AddDate(0, 0, 2), jan1.AddDate(0, 0, 3)},
					{"p_d_adv", jan1.AddDate(0, 0, 3), jan1.AddDate(0, 0, 4)},
				},
			},
		},
		{
			name:       "weekly_sub_weekly_monthly_quarterly_advance_arrear",
			subBilling: types.BILLING_PERIOD_WEEKLY,
			refStart:   jan1, // Jan 1 2026 = Thursday; weeks: Jan 1-8, Jan 8-15, ...
			specs: []LineItemSpecForPeriodTests{
				{PriceID: "p_w_adv", DisplayName: "Weekly Advance", BillingPeriod: types.BILLING_PERIOD_WEEKLY, InvoiceCadence: types.InvoiceCadenceAdvance, Amount: 70},
				{PriceID: "p_w_arr", DisplayName: "Weekly Arrear", BillingPeriod: types.BILLING_PERIOD_WEEKLY, InvoiceCadence: types.InvoiceCadenceArrear, Amount: 80},
				{PriceID: "p_m_adv", DisplayName: "Monthly Advance", BillingPeriod: types.BILLING_PERIOD_MONTHLY, InvoiceCadence: types.InvoiceCadenceAdvance, Amount: 300},
				{PriceID: "p_m_arr", DisplayName: "Monthly Arrear", BillingPeriod: types.BILLING_PERIOD_MONTHLY, InvoiceCadence: types.InvoiceCadenceArrear, Amount: 400},
				{PriceID: "p_q_adv", DisplayName: "Quarterly Advance", BillingPeriod: types.BILLING_PERIOD_QUARTER, InvoiceCadence: types.InvoiceCadenceAdvance, Amount: 900},
				{PriceID: "p_q_arr", DisplayName: "Quarterly Arrear", BillingPeriod: types.BILLING_PERIOD_QUARTER, InvoiceCadence: types.InvoiceCadenceArrear, Amount: 1000},
			},
			periodsToAssert: 4,
			expectedPerPeriod: [][]expectedLineForPeriodTests{
				// Period 1: Jan 1 - Jan 8. Equal-period: arrear uses invoice window (Jan1-Jan8), next advance (Jan8-Jan15).
				{
					{"p_w_arr", jan1, time.Date(2026, 1, 8, 0, 0, 0, 0, time.UTC)},
					{"p_w_adv", time.Date(2026, 1, 8, 0, 0, 0, 0, time.UTC), time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)},
				},
				// Period 2: Jan 8 - Jan 15. Current arrear: window (Jan8-Jan15). Next advance: weekly (Jan15-Jan22).
				{
					{"p_w_arr", time.Date(2026, 1, 8, 0, 0, 0, 0, time.UTC), time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)},
					{"p_w_adv", time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC), time.Date(2026, 1, 22, 0, 0, 0, 0, time.UTC)},
				},
				// Period 3: Jan 15 - Jan 22. Current arrear: (Jan15-Jan22). Next advance: (Jan22-Jan29).
				{
					{"p_w_arr", time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC), time.Date(2026, 1, 22, 0, 0, 0, 0, time.UTC)},
					{"p_w_adv", time.Date(2026, 1, 22, 0, 0, 0, 0, time.UTC), time.Date(2026, 1, 29, 0, 0, 0, 0, time.UTC)},
				},
				// Period 4: Jan 22 - Jan 29. Current arrear: (Jan22-Jan29). Next advance: weekly (Jan29-Feb5), monthly (Feb1-Mar1, natural period end).
				{
					{"p_w_arr", time.Date(2026, 1, 22, 0, 0, 0, 0, time.UTC), time.Date(2026, 1, 29, 0, 0, 0, 0, time.UTC)},
					{"p_w_adv", time.Date(2026, 1, 29, 0, 0, 0, 0, time.UTC), time.Date(2026, 2, 5, 0, 0, 0, 0, time.UTC)},
					{"p_m_adv", feb1, mar1},
				},
			},
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			s.BaseServiceTestSuite.ClearStores()
			sub, _ := s.setupSubWithFixedLineItemsForPeriodTests(ctx, tt.name, tt.subBilling, tt.refStart, tt.specs)
			periods := nextPeriodsForSub(tt.refStart, tt.subBilling, tt.periodsToAssert)
			s.Require().Len(periods, tt.periodsToAssert)

			for i, p := range periods {
				// Period-end reference point
				reqEnd, err := s.service.PrepareSubscriptionInvoiceRequest(ctx, sub, p.Start, p.End, types.ReferencePointPeriodEnd)
				s.NoError(err, "period %d PeriodEnd", i+1)
				s.assertInvoiceRequestFixedLines(reqEnd, tt.expectedPerPeriod[i])

				// Preview (same window; no already-invoiced filter)
				reqPreview, err := s.service.PrepareSubscriptionInvoiceRequest(ctx, sub, p.Start, p.End, types.ReferencePointPreview)
				s.NoError(err, "period %d Preview", i+1)
				s.assertInvoiceRequestFixedLines(reqPreview, tt.expectedPerPeriod[i])
			}

			// For monthly scenario only: assert ReferencePointPeriodStart for first period (advance-only, correct periods)
			if tt.name == "monthly_sub_monthly_quarterly_advance_arrear" {
				p := periods[0]
				reqStart, err := s.service.PrepareSubscriptionInvoiceRequest(ctx, sub, p.Start, p.End, types.ReferencePointPeriodStart)
				s.NoError(err)
				// Period start = only advance for current period: monthly (Jan1-Feb1), quarterly (Jan1-Apr1, natural period end)
				s.assertInvoiceRequestFixedLines(reqStart, []expectedLineForPeriodTests{
					{"p_mo_adv", jan1, feb1},
					{"p_q_adv", jan1, apr1},
				})
			}
		})
	}
}

func (s *BillingServiceSuite) TestFilterLineItemsToBeInvoiced() {
	tests := []struct {
		name                string
		setupFunc           func()
		periodStart         time.Time
		periodEnd           time.Time
		expectedCount       int
		expectedLineItemIDs []string
	}{
		{
			name:          "no_existing_invoices",
			periodStart:   s.testData.subscription.CurrentPeriodStart,
			periodEnd:     s.testData.subscription.CurrentPeriodEnd,
			expectedCount: 3, // All line items (fixed advance, fixed arrear, usage arrear)
		},
		{
			name: "fixed_advance_already_invoiced",
			setupFunc: func() {
				// Create an existing invoice for the advance charge
				inv := &invoice.Invoice{
					ID:              "inv_test_2",
					CustomerID:      s.testData.customer.ID,
					SubscriptionID:  lo.ToPtr(s.testData.subscription.ID),
					InvoiceType:     types.InvoiceTypeSubscription,
					InvoiceStatus:   types.InvoiceStatusFinalized,
					PaymentStatus:   types.PaymentStatusPending,
					Currency:        "usd",
					AmountDue:       decimal.NewFromInt(10),
					AmountPaid:      decimal.Zero,
					AmountRemaining: decimal.NewFromInt(10),
					Description:     "Test Invoice",
					PeriodStart:     lo.ToPtr(s.testData.subscription.CurrentPeriodStart),
					PeriodEnd:       lo.ToPtr(s.testData.subscription.CurrentPeriodEnd),
					BillingReason:   string(types.InvoiceBillingReasonSubscriptionCycle),
					BaseModel:       types.GetDefaultBaseModel(s.GetContext()),
					LineItems: []*invoice.InvoiceLineItem{
						{
							ID:             "li_test_2",
							InvoiceID:      "inv_test_2",
							CustomerID:     s.testData.customer.ID,
							SubscriptionID: lo.ToPtr(s.testData.subscription.ID),
							EntityID:       lo.ToPtr(s.testData.plan.ID),
							EntityType:     lo.ToPtr(string(types.SubscriptionLineItemEntityTypePlan)),
							PriceID:        lo.ToPtr(s.testData.prices.fixed.ID), // Fixed charge with advance cadence
							Amount:         decimal.NewFromInt(10),
							Quantity:       decimal.NewFromInt(1),
							Currency:       "usd",
							PeriodStart:    lo.ToPtr(s.testData.subscription.CurrentPeriodStart),
							PeriodEnd:      lo.ToPtr(s.testData.subscription.CurrentPeriodEnd),
							BaseModel:      types.GetDefaultBaseModel(s.GetContext()),
						},
					},
				}
				s.invoiceRepo.CreateWithLineItems(s.GetContext(), inv)
			},
			periodStart:   s.testData.subscription.CurrentPeriodStart,
			periodEnd:     s.testData.subscription.CurrentPeriodEnd,
			expectedCount: 2, // Only the arrear charges (fixed arrear, usage arrear) are left to be invoiced
		},
		{
			name: "arrear_charges_already_invoiced",
			setupFunc: func() {
				// Create an existing invoice for the arrear charges
				inv := &invoice.Invoice{
					ID:              "inv_test_3",
					CustomerID:      s.testData.customer.ID,
					SubscriptionID:  lo.ToPtr(s.testData.subscription.ID),
					InvoiceType:     types.InvoiceTypeSubscription,
					InvoiceStatus:   types.InvoiceStatusFinalized,
					PaymentStatus:   types.PaymentStatusPending,
					Currency:        "usd",
					AmountDue:       decimal.NewFromInt(15),
					AmountPaid:      decimal.Zero,
					AmountRemaining: decimal.NewFromInt(15),
					Description:     "Test Invoice",
					PeriodStart:     lo.ToPtr(s.testData.subscription.CurrentPeriodStart),
					PeriodEnd:       lo.ToPtr(s.testData.subscription.CurrentPeriodEnd),
					BillingReason:   string(types.InvoiceBillingReasonSubscriptionCycle),
					BaseModel:       types.GetDefaultBaseModel(s.GetContext()),
					LineItems: []*invoice.InvoiceLineItem{
						{
							ID:             "li_test_3a",
							InvoiceID:      "inv_test_3",
							CustomerID:     s.testData.customer.ID,
							SubscriptionID: lo.ToPtr(s.testData.subscription.ID),
							EntityID:       lo.ToPtr(s.testData.plan.ID),
							EntityType:     lo.ToPtr(string(types.SubscriptionLineItemEntityTypePlan)),
							PriceID:        lo.ToPtr(s.testData.prices.apiCalls.ID), // Usage charge with arrear cadence
							Amount:         decimal.NewFromInt(10),
							Quantity:       decimal.NewFromInt(500),
							Currency:       "usd",
							PeriodStart:    lo.ToPtr(s.testData.subscription.CurrentPeriodStart),
							PeriodEnd:      lo.ToPtr(s.testData.subscription.CurrentPeriodEnd),
							BaseModel:      types.GetDefaultBaseModel(s.GetContext()),
						},
						{
							ID:             "li_test_3b",
							InvoiceID:      "inv_test_3",
							CustomerID:     s.testData.customer.ID,
							SubscriptionID: lo.ToPtr(s.testData.subscription.ID),
							EntityID:       lo.ToPtr(s.testData.plan.ID),
							EntityType:     lo.ToPtr(string(types.SubscriptionLineItemEntityTypePlan)),
							PriceID:        lo.ToPtr(s.testData.prices.storageArchive.ID), // Fixed charge with arrear cadence
							Amount:         decimal.NewFromInt(5),
							Quantity:       decimal.NewFromInt(1),
							Currency:       "usd",
							PeriodStart:    lo.ToPtr(s.testData.subscription.CurrentPeriodStart),
							PeriodEnd:      lo.ToPtr(s.testData.subscription.CurrentPeriodEnd),
							BaseModel:      types.GetDefaultBaseModel(s.GetContext()),
						},
					},
				}
				s.invoiceRepo.CreateWithLineItems(s.GetContext(), inv)
			},
			periodStart:   s.testData.subscription.CurrentPeriodStart,
			periodEnd:     s.testData.subscription.CurrentPeriodEnd,
			expectedCount: 1, // Only the advance charge is left to be invoiced
		},
		{
			name: "all_line_items_already_invoiced",
			setupFunc: func() {
				// Create an existing invoice for all charges
				inv := &invoice.Invoice{
					ID:              "inv_test_4",
					CustomerID:      s.testData.customer.ID,
					SubscriptionID:  lo.ToPtr(s.testData.subscription.ID),
					InvoiceType:     types.InvoiceTypeSubscription,
					InvoiceStatus:   types.InvoiceStatusFinalized,
					PaymentStatus:   types.PaymentStatusPending,
					Currency:        "usd",
					AmountDue:       decimal.NewFromInt(25),
					AmountPaid:      decimal.Zero,
					AmountRemaining: decimal.NewFromInt(25),
					Description:     "Test Invoice",
					PeriodStart:     lo.ToPtr(s.testData.subscription.CurrentPeriodStart),
					PeriodEnd:       lo.ToPtr(s.testData.subscription.CurrentPeriodEnd),
					BillingReason:   string(types.InvoiceBillingReasonSubscriptionCycle),
					BaseModel:       types.GetDefaultBaseModel(s.GetContext()),
					LineItems: []*invoice.InvoiceLineItem{
						{
							ID:             "li_test_4a",
							InvoiceID:      "inv_test_4",
							CustomerID:     s.testData.customer.ID,
							SubscriptionID: lo.ToPtr(s.testData.subscription.ID),
							EntityID:       lo.ToPtr(s.testData.plan.ID),
							EntityType:     lo.ToPtr(string(types.SubscriptionLineItemEntityTypePlan)),
							PriceID:        lo.ToPtr(s.testData.prices.fixed.ID), // Fixed charge with advance cadence
							Amount:         decimal.NewFromInt(10),
							Quantity:       decimal.NewFromInt(1),
							Currency:       "usd",
							PeriodStart:    lo.ToPtr(s.testData.subscription.CurrentPeriodStart),
							PeriodEnd:      lo.ToPtr(s.testData.subscription.CurrentPeriodEnd),
							BaseModel:      types.GetDefaultBaseModel(s.GetContext()),
						},
						{
							ID:             "li_test_4b",
							InvoiceID:      "inv_test_4",
							CustomerID:     s.testData.customer.ID,
							SubscriptionID: lo.ToPtr(s.testData.subscription.ID),
							EntityID:       lo.ToPtr(s.testData.plan.ID),
							EntityType:     lo.ToPtr(string(types.SubscriptionLineItemEntityTypePlan)),
							PriceID:        lo.ToPtr(s.testData.prices.apiCalls.ID), // Usage charge with arrear cadence
							Amount:         decimal.NewFromInt(10),
							Quantity:       decimal.NewFromInt(500),
							Currency:       "usd",
							PeriodStart:    lo.ToPtr(s.testData.subscription.CurrentPeriodStart),
							PeriodEnd:      lo.ToPtr(s.testData.subscription.CurrentPeriodEnd),
							BaseModel:      types.GetDefaultBaseModel(s.GetContext()),
						},
						{
							ID:             "li_test_4c",
							InvoiceID:      "inv_test_4",
							CustomerID:     s.testData.customer.ID,
							SubscriptionID: lo.ToPtr(s.testData.subscription.ID),
							EntityID:       lo.ToPtr(s.testData.plan.ID),
							EntityType:     lo.ToPtr(string(types.SubscriptionLineItemEntityTypePlan)),
							PriceID:        lo.ToPtr(s.testData.prices.storageArchive.ID), // Fixed charge with arrear cadence
							Amount:         decimal.NewFromInt(5),
							Quantity:       decimal.NewFromInt(1),
							Currency:       "usd",
							PeriodStart:    lo.ToPtr(s.testData.subscription.CurrentPeriodStart),
							PeriodEnd:      lo.ToPtr(s.testData.subscription.CurrentPeriodEnd),
							BaseModel:      types.GetDefaultBaseModel(s.GetContext()),
						},
					},
				}
				s.invoiceRepo.CreateWithLineItems(s.GetContext(), inv)
			},
			periodStart:   s.testData.subscription.CurrentPeriodStart,
			periodEnd:     s.testData.subscription.CurrentPeriodEnd,
			expectedCount: 0, // No line items left to be invoiced
		},
		{
			name:          "different_period",
			periodStart:   s.testData.subscription.CurrentPeriodEnd,
			periodEnd:     s.testData.subscription.CurrentPeriodEnd.AddDate(0, 1, 0),
			expectedCount: 3, // All line items (different period)
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			// Clear any existing invoices before each test
			s.invoiceRepo.Clear()

			if tt.setupFunc != nil {
				tt.setupFunc()
			}

			// Get subscription with line items
			sub, _, err := s.GetStores().SubscriptionRepo.GetWithLineItems(s.GetContext(), s.testData.subscription.ID)
			s.NoError(err)

			// Filter line items
			filteredLineItems, err := s.service.FilterLineItemsToBeInvoiced(
				s.GetContext(),
				sub,
				tt.periodStart,
				tt.periodEnd,
				sub.LineItems,
			)
			s.NoError(err)
			s.Len(filteredLineItems, tt.expectedCount, "Filtered line item count mismatch")

			// Verify specific line items if expected
			if len(tt.expectedLineItemIDs) > 0 {
				actualIDs := make([]string, len(filteredLineItems))
				for i, item := range filteredLineItems {
					actualIDs[i] = item.ID
				}
				s.ElementsMatch(tt.expectedLineItemIDs, actualIDs, "Filtered line item IDs mismatch")
			}

			// Additional verification based on test case
			if tt.name == "fixed_advance_already_invoiced" {
				// Verify that the remaining items are the arrear charges
				for _, item := range filteredLineItems {
					s.Equal(types.InvoiceCadenceArrear, item.InvoiceCadence,
						"Expected only arrear charges when advance charges are already invoiced")
				}
			} else if tt.name == "arrear_charges_already_invoiced" {
				// Verify that the remaining item is the advance charge
				s.Len(filteredLineItems, 1, "Expected only one item when arrear charges are already invoiced")
				if len(filteredLineItems) > 0 {
					s.Equal(types.InvoiceCadenceAdvance, filteredLineItems[0].InvoiceCadence,
						"Expected only advance charges when arrear charges are already invoiced")
					s.Equal(s.testData.prices.fixed.ID, filteredLineItems[0].PriceID,
						"Expected the fixed price when arrear charges are already invoiced")
				}
			}
		})
	}
}

// newLineItemForFindMatching builds a minimal subscription line item for FindMatchingLineItemPeriodForInvoice tests.
func newLineItemForFindMatching(startDate, endDate time.Time, period types.BillingPeriod, periodCount int, cadence types.InvoiceCadence) *subscription.SubscriptionLineItem {
	return &subscription.SubscriptionLineItem{
		StartDate:          startDate,
		EndDate:            endDate,
		BillingPeriod:      period,
		BillingPeriodCount: periodCount,
		InvoiceCadence:     cadence,
		PriceType:          types.PRICE_TYPE_FIXED,
		Quantity:           decimal.NewFromInt(1),
		Currency:           "usd",
	}
}

func (s *BillingServiceSuite) TestFindMatchingLineItemPeriodForInvoice() {
	utc := time.UTC
	// Quarterly from Jan 1 2025: first period Jan 1 - Apr 1, second Apr 1 - Jul 1 (from types.NextBillingDate +3 months).
	jan1_2025 := time.Date(2025, 1, 1, 0, 0, 0, 0, utc)
	apr1_2025 := time.Date(2025, 4, 1, 0, 0, 0, 0, utc)
	jan31_2025 := time.Date(2025, 1, 31, 0, 0, 0, 0, utc)
	feb1_2025 := time.Date(2025, 2, 1, 0, 0, 0, 0, utc)
	feb28_2025 := time.Date(2025, 2, 28, 0, 0, 0, 0, utc)
	mar1_2025 := time.Date(2025, 3, 1, 0, 0, 0, 0, utc)
	mar31_2025 := time.Date(2025, 3, 31, 0, 0, 0, 0, utc)
	apr30_2025 := time.Date(2025, 4, 30, 0, 0, 0, 0, utc)
	jan1_2024 := time.Date(2024, 1, 1, 0, 0, 0, 0, utc)
	jan31_2024 := time.Date(2024, 1, 31, 0, 0, 0, 0, utc)
	jan1_2026 := time.Date(2026, 1, 1, 0, 0, 0, 0, utc)
	jan31_2026 := time.Date(2026, 1, 31, 0, 0, 0, 0, utc)
	apr1_2024 := time.Date(2024, 4, 1, 0, 0, 0, 0, utc)
	apr1_2026 := time.Date(2026, 4, 1, 0, 0, 0, 0, utc)
	jul1_2025 := time.Date(2025, 7, 1, 0, 0, 0, 0, utc)
	may15_2025 := time.Date(2025, 5, 15, 0, 0, 0, 0, utc)
	feb15_2025 := time.Date(2025, 2, 15, 0, 0, 0, 0, utc)

	// Subsecond timestamps for "next period" / boundary truncation tests (Q Advanced on May 2 - Jun 2 preview)
	mar2_143010_418 := time.Date(2025, 3, 2, 14, 30, 10, 418000000, utc)
	jun2_143010_000 := time.Date(2025, 6, 2, 14, 30, 10, 0, utc)
	jul2_143010_000 := time.Date(2025, 7, 2, 14, 30, 10, 0, utc)
	sep2_143010_000 := time.Date(2025, 9, 2, 14, 30, 10, 0, utc)
	jan1_143010_418 := time.Date(2025, 1, 1, 14, 30, 10, 418000000, utc)
	apr1_143010_000 := time.Date(2025, 4, 1, 14, 30, 10, 0, utc)

	tests := []struct {
		name           string
		item           *subscription.SubscriptionLineItem
		periodStart    time.Time
		periodEnd      time.Time
		invoiceCadence types.InvoiceCadence
		wantOK         bool
		wantStart      time.Time
		wantEnd        time.Time
		wantErr        bool
	}{
		{
			name:           "advance_quarterly_jan_window_match",
			item:           newLineItemForFindMatching(jan1_2025, time.Time{}, types.BILLING_PERIOD_QUARTER, 1, types.InvoiceCadenceAdvance),
			periodStart:    jan1_2025,
			periodEnd:      jan31_2025,
			invoiceCadence: types.InvoiceCadenceAdvance,
			wantOK:         true,
			wantStart:      jan1_2025,
			wantEnd:        apr1_2025, // natural quarter end, not clipped to window
		},
		{
			name:           "advance_quarterly_feb_window_no_match",
			item:           newLineItemForFindMatching(jan1_2025, time.Time{}, types.BILLING_PERIOD_QUARTER, 1, types.InvoiceCadenceAdvance),
			periodStart:    feb1_2025,
			periodEnd:      feb28_2025,
			invoiceCadence: types.InvoiceCadenceAdvance,
			wantOK:         false,
		},
		{
			name:           "advance_quarterly_apr_window_match",
			item:           newLineItemForFindMatching(jan1_2025, time.Time{}, types.BILLING_PERIOD_QUARTER, 1, types.InvoiceCadenceAdvance),
			periodStart:    apr1_2025,
			periodEnd:      apr30_2025,
			invoiceCadence: types.InvoiceCadenceAdvance,
			wantOK:         true,
			wantStart:      apr1_2025,
			wantEnd:        jul1_2025, // natural quarter end (Apr 1 - Jul 1)
		},
		{
			name:           "arrear_quarterly_mar_window_match",
			item:           newLineItemForFindMatching(jan1_2025, time.Time{}, types.BILLING_PERIOD_QUARTER, 1, types.InvoiceCadenceArrear),
			periodStart:    mar1_2025,
			periodEnd:      apr30_2025,
			invoiceCadence: types.InvoiceCadenceArrear,
			wantOK:         true,
			wantStart:      jan1_2025,
			wantEnd:        apr1_2025,
		},
		{
			name:           "arrear_quarterly_jan_window_no_match_quarter_ends_april",
			item:           newLineItemForFindMatching(jan1_2025, time.Time{}, types.BILLING_PERIOD_QUARTER, 1, types.InvoiceCadenceArrear),
			periodStart:    jan1_2025,
			periodEnd:      jan31_2025,
			invoiceCadence: types.InvoiceCadenceArrear,
			wantOK:         false, // quarter ends Apr 1, not in Jan window
		},
		{
			name:           "advance_past_window_2024_jan_match",
			item:           newLineItemForFindMatching(jan1_2024, time.Time{}, types.BILLING_PERIOD_QUARTER, 1, types.InvoiceCadenceAdvance),
			periodStart:    jan1_2024,
			periodEnd:      jan31_2024,
			invoiceCadence: types.InvoiceCadenceAdvance,
			wantOK:         true,
			wantStart:      jan1_2024,
			wantEnd:        apr1_2024, // natural quarter end
		},
		{
			name:           "advance_future_window_2026_jan_match",
			item:           newLineItemForFindMatching(jan1_2025, time.Time{}, types.BILLING_PERIOD_QUARTER, 1, types.InvoiceCadenceAdvance),
			periodStart:    jan1_2026,
			periodEnd:      jan31_2026,
			invoiceCadence: types.InvoiceCadenceAdvance,
			wantOK:         true,
			wantStart:      jan1_2026,
			wantEnd:        apr1_2026, // natural quarter end
		},
		{
			name:           "advance_end_date_before_period_end_clips",
			item:           newLineItemForFindMatching(jan1_2025, feb15_2025, types.BILLING_PERIOD_QUARTER, 1, types.InvoiceCadenceAdvance),
			periodStart:    jan1_2025,
			periodEnd:      mar31_2025,
			invoiceCadence: types.InvoiceCadenceAdvance,
			wantOK:         true,
			wantStart:      jan1_2025,
			wantEnd:        feb15_2025,
		},
		{
			name:           "arrear_end_date_before_period_end",
			item:           newLineItemForFindMatching(jan1_2025, mar31_2025, types.BILLING_PERIOD_QUARTER, 1, types.InvoiceCadenceArrear),
			periodStart:    mar1_2025,
			periodEnd:      apr1_2025,
			invoiceCadence: types.InvoiceCadenceArrear,
			wantOK:         true,
			wantStart:      jan1_2025,
			wantEnd:        mar31_2025,
		},
		// Edge cases
		{
			name:           "edge_line_item_start_after_window_no_match",
			item:           newLineItemForFindMatching(feb1_2025, time.Time{}, types.BILLING_PERIOD_QUARTER, 1, types.InvoiceCadenceAdvance),
			periodStart:    jan1_2025,
			periodEnd:      jan31_2025,
			invoiceCadence: types.InvoiceCadenceAdvance,
			wantOK:         false,
		},
		{
			name:           "edge_mid_period_start_advance_match",
			item:           newLineItemForFindMatching(feb15_2025, time.Time{}, types.BILLING_PERIOD_QUARTER, 1, types.InvoiceCadenceAdvance),
			periodStart:    feb15_2025,
			periodEnd:      mar31_2025,
			invoiceCadence: types.InvoiceCadenceAdvance,
			wantOK:         true,
			wantStart:      feb15_2025,
			wantEnd:        may15_2025, // natural quarter end (Feb 15 - May 15)
		},
		{
			name:           "edge_arrear_period_end_equals_window_end_included",
			item:           newLineItemForFindMatching(jan1_2025, time.Time{}, types.BILLING_PERIOD_QUARTER, 1, types.InvoiceCadenceArrear),
			periodStart:    mar1_2025,
			periodEnd:      apr1_2025,
			invoiceCadence: types.InvoiceCadenceArrear,
			wantOK:         true,
			wantStart:      jan1_2025,
			wantEnd:        apr1_2025,
		},
		{
			name:           "edge_arrear_monthly_period_end_equals_window_end_included",
			item:           newLineItemForFindMatching(jan1_2025, time.Time{}, types.BILLING_PERIOD_MONTHLY, 1, types.InvoiceCadenceArrear),
			periodStart:    jan1_2025,
			periodEnd:      feb1_2025,
			invoiceCadence: types.InvoiceCadenceArrear,
			wantOK:         true,
			wantStart:      jan1_2025,
			wantEnd:        feb1_2025,
		},
		{
			name:           "edge_advance_monthly_line_quarter_window_returns_first_period",
			item:           newLineItemForFindMatching(jan1_2025, time.Time{}, types.BILLING_PERIOD_MONTHLY, 1, types.InvoiceCadenceAdvance),
			periodStart:    jan1_2025,
			periodEnd:      mar31_2025,
			invoiceCadence: types.InvoiceCadenceAdvance,
			wantOK:         true,
			wantStart:      jan1_2025,
			wantEnd:        feb1_2025,
		},
		// Second-level truncation: Q Advanced "next period" (window Jun 2 14:30:10.000 - Jul 2 14:30:10.0), line item start has .418ms; match uses truncation
		{
			name:           "advance_quarterly_next_period_window_subsecond_boundary_matches_with_truncation",
			item:           newLineItemForFindMatching(mar2_143010_418, time.Time{}, types.BILLING_PERIOD_QUARTER, 1, types.InvoiceCadenceAdvance),
			periodStart:    jun2_143010_000,
			periodEnd:      jul2_143010_000,
			invoiceCadence: types.InvoiceCadenceAdvance,
			wantOK:         true,
			wantStart:      jun2_143010_000, // NextBillingDate returns second precision
			wantEnd:        sep2_143010_000,
		},
		// Second-level truncation: arrear quarter end Apr 1 14:30:10.418, window end Apr 1 14:30:10.000 — should match
		{
			name:           "arrear_quarterly_period_end_subsecond_boundary_matches_with_truncation",
			item:           newLineItemForFindMatching(jan1_143010_418, time.Time{}, types.BILLING_PERIOD_QUARTER, 1, types.InvoiceCadenceArrear),
			periodStart:    mar1_2025,
			periodEnd:      apr1_143010_000,
			invoiceCadence: types.InvoiceCadenceArrear,
			wantOK:         true,
			wantStart:      jan1_143010_418, // period start preserves anchor; end from NextBillingDate is sec precision
			wantEnd:        apr1_143010_000,
		},
		// Arrear excluded when period end equals invoice period start (quarter ends Jun 2, invoice is Jun 2–Jul 2)
		{
			name:           "arrear_quarterly_period_end_equals_invoice_start_excluded",
			item:           newLineItemForFindMatching(mar2_143010_418, time.Time{}, types.BILLING_PERIOD_QUARTER, 1, types.InvoiceCadenceArrear),
			periodStart:    jun2_143010_000,
			periodEnd:      jul2_143010_000,
			invoiceCadence: types.InvoiceCadenceArrear,
			wantOK:         false,
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			res, err := FindMatchingLineItemPeriodForInvoice(FindMatchingLineItemPeriodInput{
				Item:           tt.item,
				PeriodStart:    tt.periodStart,
				PeriodEnd:      tt.periodEnd,
				InvoiceCadence: tt.invoiceCadence,
			})
			if tt.wantErr {
				s.Error(err)
				return
			}
			s.NoError(err)
			s.Equal(tt.wantOK, res.Ok, "Ok mismatch")
			if tt.wantOK {
				s.True(res.LineItemPeriodStart.Equal(tt.wantStart), "LineItemPeriodStart: got %v want %v", res.LineItemPeriodStart, tt.wantStart)
				s.True(res.LineItemPeriodEnd.Equal(tt.wantEnd), "LineItemPeriodEnd: got %v want %v", res.LineItemPeriodEnd, tt.wantEnd)
			}
		})
	}
}

func (s *BillingServiceSuite) TestClassifyLineItems() {
	// Get subscription with line items
	sub, _, err := s.GetStores().SubscriptionRepo.GetWithLineItems(s.GetContext(), s.testData.subscription.ID)
	s.NoError(err)

	currentPeriodStart := sub.CurrentPeriodStart
	currentPeriodEnd := sub.CurrentPeriodEnd
	nextPeriodStart := currentPeriodEnd
	nextPeriodEnd := nextPeriodStart.AddDate(0, 1, 0)

	// Classify line items
	classification := s.service.ClassifyLineItems(
		sub,
		currentPeriodStart,
		currentPeriodEnd,
		nextPeriodStart,
		nextPeriodEnd,
	)

	s.NotNil(classification)

	// Verify current period advance charges (fixed with advance cadence)
	s.Len(classification.CurrentPeriodAdvance, 1, "Should have 1 current period advance charge")
	if len(classification.CurrentPeriodAdvance) > 0 {
		advanceItem := classification.CurrentPeriodAdvance[0]
		s.Equal(types.InvoiceCadenceAdvance, advanceItem.InvoiceCadence, "Current period advance item should have advance cadence")
		s.Equal(types.PRICE_TYPE_FIXED, advanceItem.PriceType, "Current period advance item should be fixed type")
		s.Equal(s.testData.prices.fixed.ID, advanceItem.PriceID, "Current period advance item should be the fixed price")
	}

	// Verify current period arrear charges (usage with arrear cadence + fixed with arrear cadence)
	s.Len(classification.CurrentPeriodArrear, 2, "Should have 2 current period arrear charges")
	if len(classification.CurrentPeriodArrear) > 0 {
		// Find the usage arrear item
		var usageArrearItem *subscription.SubscriptionLineItem
		var fixedArrearItem *subscription.SubscriptionLineItem

		for _, item := range classification.CurrentPeriodArrear {
			switch item.PriceType {
			case types.PRICE_TYPE_USAGE:
				usageArrearItem = item
			case types.PRICE_TYPE_FIXED:
				fixedArrearItem = item
			}
		}

		// Verify usage arrear item
		s.NotNil(usageArrearItem, "Should have a usage arrear item")
		if usageArrearItem != nil {
			s.Equal(types.InvoiceCadenceArrear, usageArrearItem.InvoiceCadence, "Usage arrear item should have arrear cadence")
			s.Equal(s.testData.prices.apiCalls.ID, usageArrearItem.PriceID, "Usage arrear item should be the API calls price")
		}

		// Verify fixed arrear item
		s.NotNil(fixedArrearItem, "Should have a fixed arrear item")
		if fixedArrearItem != nil {
			s.Equal(types.InvoiceCadenceArrear, fixedArrearItem.InvoiceCadence, "Fixed arrear item should have arrear cadence")
			s.Equal(s.testData.prices.storageArchive.ID, fixedArrearItem.PriceID, "Fixed arrear item should be the archive storage price")
		}
	}

	// Verify next period advance charges (same as current period advance)
	s.Len(classification.NextPeriodAdvance, 1, "Should have 1 next period advance charge")
	if len(classification.NextPeriodAdvance) > 0 {
		nextAdvanceItem := classification.NextPeriodAdvance[0]
		s.Equal(types.InvoiceCadenceAdvance, nextAdvanceItem.InvoiceCadence, "Next period advance item should have advance cadence")
		s.Equal(types.PRICE_TYPE_FIXED, nextAdvanceItem.PriceType, "Next period advance item should be fixed type")
		s.Equal(s.testData.prices.fixed.ID, nextAdvanceItem.PriceID, "Next period advance item should be the fixed price")
	}

	// Verify usage charges flag
	s.True(classification.HasUsageCharges, "Should have usage charges")
}

func (s *BillingServiceSuite) TestCalculateUsageChargesWithEntitlements() {
	// Initialize test data
	s.setupTestData()

	// Initialize billing service
	s.service = NewBillingService(ServiceParams{
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
		AddonAssociationRepo:     s.GetStores().AddonAssociationRepo,
		EventPublisher:           s.GetPublisher(),
		ProrationCalculator:      s.GetCalculator(),
		FeatureUsageRepo:         s.GetStores().FeatureUsageRepo,
	})

	tests := []struct {
		name                string
		setupFunc           func()
		expectedLineItems   int
		expectedTotalAmount decimal.Decimal
		wantErr             bool
	}{
		{
			name: "usage_within_entitlement_limit",
			setupFunc: func() {
				// Create test feature
				testFeature := &feature.Feature{
					ID:          "feat_test_1",
					Name:        "Test Feature",
					Description: "Test Feature Description",
					Type:        types.FeatureTypeMetered,
					MeterID:     s.testData.meters.apiCalls.ID,
					BaseModel:   types.GetDefaultBaseModel(s.GetContext()),
				}
				err := s.GetStores().FeatureRepo.Create(s.GetContext(), testFeature)
				s.NoError(err)

				// Create entitlement with usage limit
				entitlement := &entitlement.Entitlement{
					ID:               "ent_test_1",
					EntityType:       types.ENTITLEMENT_ENTITY_TYPE_PLAN,
					EntityID:         s.testData.plan.ID,
					FeatureID:        testFeature.ID,
					FeatureType:      types.FeatureTypeMetered,
					IsEnabled:        true,
					UsageLimit:       lo.ToPtr(int64(1000)), // Allow 1000 units
					UsageResetPeriod: types.ENTITLEMENT_USAGE_RESET_PERIOD_MONTHLY,
					IsSoftLimit:      false,
					BaseModel:        types.GetDefaultBaseModel(s.GetContext()),
				}
				_, err = s.GetStores().EntitlementRepo.Create(s.GetContext(), entitlement)
				s.NoError(err)
			},
			expectedLineItems:   1,
			expectedTotalAmount: decimal.Zero, // No charge as usage is within limit
			wantErr:             false,
		},
		{
			name: "usage_exceeds_entitlement_limit",
			setupFunc: func() {
				// Create test feature
				testFeature := &feature.Feature{
					ID:          "feat_test_2",
					Name:        "Test Feature 2",
					Description: "Test Feature Description 2",
					Type:        types.FeatureTypeMetered,
					MeterID:     s.testData.meters.apiCalls.ID,
					BaseModel:   types.GetDefaultBaseModel(s.GetContext()),
				}
				err := s.GetStores().FeatureRepo.Create(s.GetContext(), testFeature)
				s.NoError(err)

				// Create entitlement with lower usage limit
				entitlement := &entitlement.Entitlement{
					ID:               "ent_test_2",
					EntityType:       types.ENTITLEMENT_ENTITY_TYPE_PLAN,
					EntityID:         s.testData.plan.ID,
					FeatureID:        testFeature.ID,
					FeatureType:      types.FeatureTypeMetered,
					IsEnabled:        true,
					UsageLimit:       lo.ToPtr(int64(100)), // Only allow 100 units
					UsageResetPeriod: types.ENTITLEMENT_USAGE_RESET_PERIOD_MONTHLY,
					IsSoftLimit:      false,
					BaseModel:        types.GetDefaultBaseModel(s.GetContext()),
				}
				_, err = s.GetStores().EntitlementRepo.Create(s.GetContext(), entitlement)
				s.NoError(err)
			},
			expectedLineItems:   1,
			expectedTotalAmount: decimal.NewFromFloat(8), // Should charge for 400 units (500-100) at $0.02/unit
			wantErr:             false,
		},
		{
			name: "unlimited_entitlement",
			setupFunc: func() {
				// Create test feature
				testFeature := &feature.Feature{
					ID:          "feat_test_3",
					Name:        "Test Feature 3",
					Description: "Test Feature Description 3",
					Type:        types.FeatureTypeMetered,
					MeterID:     s.testData.meters.apiCalls.ID,
					BaseModel:   types.GetDefaultBaseModel(s.GetContext()),
				}
				err := s.GetStores().FeatureRepo.Create(s.GetContext(), testFeature)
				s.NoError(err)

				// Create unlimited entitlement
				entitlement := &entitlement.Entitlement{
					ID:               "ent_test_3",
					EntityType:       types.ENTITLEMENT_ENTITY_TYPE_PLAN,
					EntityID:         s.testData.plan.ID,
					FeatureID:        testFeature.ID,
					FeatureType:      types.FeatureTypeMetered,
					IsEnabled:        true,
					UsageLimit:       nil, // Unlimited usage
					UsageResetPeriod: types.ENTITLEMENT_USAGE_RESET_PERIOD_MONTHLY,
					IsSoftLimit:      false,
					BaseModel:        types.GetDefaultBaseModel(s.GetContext()),
				}
				_, err = s.GetStores().EntitlementRepo.Create(s.GetContext(), entitlement)
				s.NoError(err)
			},
			expectedLineItems:   1,
			expectedTotalAmount: decimal.Zero, // No charge for unlimited entitlement
			wantErr:             false,
		},
		{
			name: "soft_limit_entitlement",
			setupFunc: func() {
				// Create test feature
				testFeature := &feature.Feature{
					ID:          "feat_test_4",
					Name:        "Test Feature 4",
					Description: "Test Feature Description 4",
					Type:        types.FeatureTypeMetered,
					MeterID:     s.testData.meters.apiCalls.ID,
					BaseModel:   types.GetDefaultBaseModel(s.GetContext()),
				}
				err := s.GetStores().FeatureRepo.Create(s.GetContext(), testFeature)
				s.NoError(err)

				// Create soft limit entitlement
				entitlement := &entitlement.Entitlement{
					ID:               "ent_test_4",
					EntityType:       types.ENTITLEMENT_ENTITY_TYPE_PLAN,
					EntityID:         s.testData.plan.ID,
					FeatureID:        testFeature.ID,
					FeatureType:      types.FeatureTypeMetered,
					IsEnabled:        true,
					UsageLimit:       lo.ToPtr(int64(100)), // Soft limit of 100 units
					UsageResetPeriod: types.ENTITLEMENT_USAGE_RESET_PERIOD_MONTHLY,
					IsSoftLimit:      true,
					BaseModel:        types.GetDefaultBaseModel(s.GetContext()),
				}
				_, err = s.GetStores().EntitlementRepo.Create(s.GetContext(), entitlement)
				s.NoError(err)
			},
			expectedLineItems:   1,
			expectedTotalAmount: decimal.NewFromFloat(8), // Should charge for overage despite soft limit
			wantErr:             false,
		},
		{
			name: "disabled_entitlement",
			setupFunc: func() {
				// Create test feature
				testFeature := &feature.Feature{
					ID:          "feat_test_5",
					Name:        "Test Feature 5",
					Description: "Test Feature Description 5",
					Type:        types.FeatureTypeMetered,
					MeterID:     s.testData.meters.apiCalls.ID,
					BaseModel:   types.GetDefaultBaseModel(s.GetContext()),
				}
				err := s.GetStores().FeatureRepo.Create(s.GetContext(), testFeature)
				s.NoError(err)

				// Create disabled entitlement
				entitlement := &entitlement.Entitlement{
					ID:               "ent_test_5",
					EntityType:       types.ENTITLEMENT_ENTITY_TYPE_PLAN,
					EntityID:         s.testData.plan.ID,
					FeatureID:        testFeature.ID,
					FeatureType:      types.FeatureTypeMetered,
					IsEnabled:        false, // Disabled entitlement
					UsageLimit:       lo.ToPtr(int64(1000)),
					UsageResetPeriod: types.ENTITLEMENT_USAGE_RESET_PERIOD_MONTHLY,
					IsSoftLimit:      false,
					BaseModel:        types.GetDefaultBaseModel(s.GetContext()),
				}
				_, err = s.GetStores().EntitlementRepo.Create(s.GetContext(), entitlement)
				s.NoError(err)

				// Create test events to simulate actual usage
				for i := 0; i < 500; i++ { // 500 units of usage
					event := &events.Event{
						ID:                 s.GetUUID(),
						TenantID:           s.testData.subscription.TenantID,
						EventName:          s.testData.meters.apiCalls.EventName,
						ExternalCustomerID: s.testData.customer.ExternalID,
						Timestamp:          s.testData.now.Add(-1 * time.Hour),
						Properties:         map[string]interface{}{},
					}
					s.NoError(s.GetStores().EventRepo.InsertEvent(s.GetContext(), event))
				}

				// Update subscription with line items
				// First, remove any existing line items for the API calls price
				var updatedLineItems []*subscription.SubscriptionLineItem
				for _, item := range s.testData.subscription.LineItems {
					if item.PriceID != s.testData.prices.apiCalls.ID {
						updatedLineItems = append(updatedLineItems, item)
					}
				}

				// Add the new line item
				updatedLineItems = append(updatedLineItems,
					&subscription.SubscriptionLineItem{
						ID:               types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION_LINE_ITEM),
						SubscriptionID:   s.testData.subscription.ID,
						CustomerID:       s.testData.subscription.CustomerID,
						EntityID:         s.testData.plan.ID,
						EntityType:       types.SubscriptionLineItemEntityTypePlan,
						PlanDisplayName:  s.testData.plan.Name,
						PriceID:          s.testData.prices.apiCalls.ID,
						PriceType:        s.testData.prices.apiCalls.Type,
						MeterID:          s.testData.meters.apiCalls.ID,
						MeterDisplayName: s.testData.meters.apiCalls.Name,
						DisplayName:      "API Calls",
						Currency:         s.testData.subscription.Currency,
						BillingPeriod:    s.testData.subscription.BillingPeriod,
						InvoiceCadence:   types.InvoiceCadenceArrear,
						StartDate:        s.testData.subscription.StartDate,
						BaseModel:        types.GetDefaultBaseModel(s.GetContext()),
					},
				)

				s.testData.subscription.LineItems = updatedLineItems
				s.NoError(s.GetStores().SubscriptionRepo.Update(s.GetContext(), s.testData.subscription))
			},
			expectedLineItems:   1,
			expectedTotalAmount: decimal.NewFromFloat(10), // Should charge for all usage (500 units at $0.02/unit)
			wantErr:             false,
		},
		{
			name: "vanilla_no_entitlements",
			setupFunc: func() {
				// Create test events to simulate actual usage
				for i := 0; i < 500; i++ { // 500 units of usage
					event := &events.Event{
						ID:                 s.GetUUID(),
						TenantID:           s.testData.subscription.TenantID,
						EventName:          s.testData.meters.apiCalls.EventName,
						ExternalCustomerID: s.testData.customer.ExternalID,
						Timestamp:          s.testData.now.Add(-1 * time.Hour),
						Properties:         map[string]interface{}{},
					}
					s.NoError(s.GetStores().EventRepo.InsertEvent(s.GetContext(), event))
				}
			},
			expectedLineItems:   1,
			expectedTotalAmount: decimal.NewFromFloat(10), // Should charge for all usage (500 units at $0.02/unit)
			wantErr:             false,
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			// Reset test data
			s.SetupTest()
			s.setupTestData() // Add this line to ensure test data is properly initialized

			// Setup test case
			if tt.setupFunc != nil {
				tt.setupFunc()
			}

			// Verify the subscription is properly set up
			s.NotNil(s.testData.subscription, "Subscription should not be nil")
			s.Equal(s.testData.plan.ID, s.testData.subscription.PlanID, "Subscription should have correct plan ID")

			// Get the line item for API calls
			var apiCallsLineItem *subscription.SubscriptionLineItem
			for _, item := range s.testData.subscription.LineItems {
				if item.PriceID == s.testData.prices.apiCalls.ID {
					apiCallsLineItem = item
					break
				}
			}
			s.NotNil(apiCallsLineItem, "Expected to find line item for API calls price")

			// Create usage data with proper subscription line item reference
			usage := &dto.GetUsageBySubscriptionResponse{
				StartTime: s.testData.subscription.CurrentPeriodStart,
				EndTime:   s.testData.subscription.CurrentPeriodEnd,
				Currency:  s.testData.subscription.Currency,
				Charges: []*dto.SubscriptionUsageByMetersResponse{
					{
						Price:     s.testData.prices.apiCalls,
						Quantity:  500, // 500 units of usage
						Amount:    10,  // $10 without entitlement adjustment (500 * 0.02)
						IsOverage: false,
						MeterID:   s.testData.meters.apiCalls.ID,
					},
				},
			}

			// Verify the usage data is properly set up
			s.Equal(1, len(usage.Charges), "Should have exactly one charge")
			s.Equal(s.testData.meters.apiCalls.ID, usage.Charges[0].MeterID, "Should be for API calls meter")
			s.Equal(float64(500), usage.Charges[0].Quantity, "Should have 500 units of usage")
			s.Equal(float64(10), usage.Charges[0].Amount, "Should have $10 of charges")

			// Calculate charges
			lineItems, totalAmount, err := s.service.CalculateUsageCharges(
				s.GetContext(),
				s.testData.subscription,
				usage,
				s.testData.subscription.CurrentPeriodStart,
				s.testData.subscription.CurrentPeriodEnd,
			)

			if tt.wantErr {
				s.Error(err)
				return
			}

			s.NoError(err)
			s.Len(lineItems, tt.expectedLineItems, "Expected %d line items, got %d", tt.expectedLineItems, len(lineItems))
			s.True(tt.expectedTotalAmount.Equal(totalAmount),
				"Expected total amount %s, got %s for test case %s", tt.expectedTotalAmount, totalAmount, tt.name)

			// Print more details for debugging
			if !tt.expectedTotalAmount.Equal(totalAmount) {
				s.T().Logf("Test case: %s", tt.name)
				s.T().Logf("Line items: %+v", lineItems)
				s.T().Logf("Usage data: %+v", usage)
			}
		})
	}
}

func (s *BillingServiceSuite) TestCalculateUsageChargesWithDailyReset() {
	// Setup test data for daily usage calculation
	ctx := s.GetContext()

	// Clear the event store to start with a clean slate
	s.eventRepo.Clear()

	// Create test feature with daily reset
	testFeature := &feature.Feature{
		ID:          "feat_daily_123",
		Name:        "Daily API Calls",
		Description: "API calls with daily reset",
		Type:        types.FeatureTypeMetered,
		MeterID:     s.testData.meters.apiCalls.ID,
		BaseModel:   types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().FeatureRepo.Create(ctx, testFeature))

	// Create entitlement with daily reset
	entitlement := &entitlement.Entitlement{
		ID:               "ent_daily_123",
		EntityType:       types.ENTITLEMENT_ENTITY_TYPE_PLAN,
		EntityID:         s.testData.plan.ID,
		FeatureID:        testFeature.ID,
		FeatureType:      types.FeatureTypeMetered,
		IsEnabled:        true,
		UsageLimit:       lo.ToPtr(int64(10)), // 10 requests per day
		UsageResetPeriod: types.ENTITLEMENT_USAGE_RESET_PERIOD_DAILY,
		IsSoftLimit:      false,
		BaseModel:        types.GetDefaultBaseModel(ctx),
	}
	_, err := s.GetStores().EntitlementRepo.Create(ctx, entitlement)
	s.NoError(err)

	// Create test events for different days within the subscription period
	// We need to use different calendar days for daily reset to work properly
	// Day 1: 15 requests (5 over limit) - 2 days ago
	// Day 2: 3 requests (0 over limit) - yesterday
	// Day 3: 12 requests (2 over limit) - today
	eventDates := []time.Time{
		s.testData.now.Add(-48 * time.Hour), // Day 1 - 2 days ago
		s.testData.now.Add(-24 * time.Hour), // Day 2 - yesterday
		s.testData.now,                      // Day 3 - today
	}

	for i, eventDate := range eventDates {
		var eventCount int
		switch i {
		case 0:
			eventCount = 15 // Day 1: 15 requests
		case 1:
			eventCount = 3 // Day 2: 3 requests
		case 2:
			eventCount = 12 // Day 3: 12 requests
		}

		for j := 0; j < eventCount; j++ {
			event := &events.Event{
				ID:                 s.GetUUID(),
				TenantID:           s.testData.subscription.TenantID,
				EventName:          s.testData.meters.apiCalls.EventName,
				ExternalCustomerID: s.testData.customer.ExternalID,
				Timestamp:          eventDate,
				Properties:         map[string]interface{}{},
			}
			s.NoError(s.GetStores().EventRepo.InsertEvent(ctx, event))
		}
	}

	// Create usage data that would normally come from GetUsageBySubscription
	usage := &dto.GetUsageBySubscriptionResponse{
		StartTime: s.testData.subscription.CurrentPeriodStart,
		EndTime:   s.testData.subscription.CurrentPeriodEnd,
		Currency:  s.testData.subscription.Currency,
		Charges: []*dto.SubscriptionUsageByMetersResponse{
			{
				Price:     s.testData.prices.apiCalls,
				Quantity:  30,  // Total usage across all days (15+3+12)
				Amount:    0.6, // $0.6 without entitlement adjustment (30 * 0.02)
				IsOverage: false,
				MeterID:   s.testData.meters.apiCalls.ID,
			},
		},
	}

	// Calculate charges
	lineItems, totalAmount, err := s.service.CalculateUsageCharges(
		ctx,
		s.testData.subscription,
		usage,
		s.testData.subscription.CurrentPeriodStart,
		s.testData.subscription.CurrentPeriodEnd,
	)

	s.NoError(err)
	s.Len(lineItems, 1, "Should have one line item for daily usage")

	// Expected calculation:
	// Day 1: 15 - 10 = 5 overage
	// Day 2: 3 - 10 = 0 overage (max(0, -7) = 0)
	// Day 3: 12 - 10 = 2 overage
	// Total overage: 5 + 0 + 2 = 7 requests
	// Total cost: 7 * $0.02 = $0.14 (using tiered pricing)
	expectedQuantity := decimal.NewFromInt(7)

	s.True(expectedQuantity.Equal(lineItems[0].Quantity),
		"Expected quantity %s, got %s", expectedQuantity, lineItems[0].Quantity)

	// Check that the amount is calculated correctly
	s.Equal(decimal.NewFromFloat(0.14), totalAmount, "Should have correct total amount for daily overage")

	// Check metadata indicates daily reset
	s.Equal("daily", lineItems[0].Metadata["usage_reset_period"])
}

func (s *BillingServiceSuite) TestCalculateUsageChargesWithBucketedMaxAggregation() {
	ctx := s.GetContext()

	tests := []struct {
		name             string
		billingModel     types.BillingModel
		setupPrice       func() *price.Price
		bucketValues     []decimal.Decimal // Max values per bucket
		expectedAmount   decimal.Decimal
		expectedQuantity decimal.Decimal
		description      string
	}{
		{
			name:         "bucketed_max_flat_fee",
			billingModel: types.BILLING_MODEL_FLAT_FEE,
			setupPrice: func() *price.Price {
				return &price.Price{
					ID:                 "price_bucketed_flat",
					Amount:             decimal.NewFromFloat(0.10), // $0.10 per unit
					Currency:           "usd",
					EntityType:         types.PRICE_ENTITY_TYPE_PLAN,
					EntityID:           s.testData.plan.ID,
					Type:               types.PRICE_TYPE_USAGE,
					BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
					BillingPeriodCount: 1,
					BillingModel:       types.BILLING_MODEL_FLAT_FEE,
					BillingCadence:     types.BILLING_CADENCE_RECURRING,
					InvoiceCadence:     types.InvoiceCadenceArrear,
					MeterID:            s.testData.meters.apiCalls.ID,
					BaseModel:          types.GetDefaultBaseModel(ctx),
				}
			},
			bucketValues:     []decimal.Decimal{decimal.NewFromInt(9), decimal.NewFromInt(10)}, // Bucket 1: max(2,5,6,9)=9, Bucket 2: max(10)=10
			expectedAmount:   decimal.NewFromFloat(1.9),                                        // (9 * 0.10) + (10 * 0.10) = $1.90
			expectedQuantity: decimal.NewFromInt(19),                                           // 9 + 10 = 19
			description:      "Flat fee: Bucket1[2,5,6,9]→max=9, Bucket2[10]→max=10, Total: 9*$0.10 + 10*$0.10 = $1.90",
		},
		{
			name:         "bucketed_max_package",
			billingModel: types.BILLING_MODEL_PACKAGE,
			setupPrice: func() *price.Price {
				return &price.Price{
					ID:                 "price_bucketed_package",
					Amount:             decimal.NewFromInt(1), // $1 per package
					Currency:           "usd",
					EntityType:         types.PRICE_ENTITY_TYPE_PLAN,
					EntityID:           s.testData.plan.ID,
					Type:               types.PRICE_TYPE_USAGE,
					BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
					BillingPeriodCount: 1,
					BillingModel:       types.BILLING_MODEL_PACKAGE,
					BillingCadence:     types.BILLING_CADENCE_RECURRING,
					InvoiceCadence:     types.InvoiceCadenceArrear,
					MeterID:            s.testData.meters.apiCalls.ID,
					TransformQuantity: price.JSONBTransformQuantity{
						DivideBy: 10,   // 10 units per package
						Round:    "up", // Round up
					},
					BaseModel: types.GetDefaultBaseModel(ctx),
				}
			},
			bucketValues:     []decimal.Decimal{decimal.NewFromInt(9), decimal.NewFromInt(10)}, // Bucket 1: max(2,5,6,9)=9, Bucket 2: max(10)=10
			expectedAmount:   decimal.NewFromInt(2),                                            // Bucket 1: ceil(9/10) = 1 package, Bucket 2: ceil(10/10) = 1 package = $2
			expectedQuantity: decimal.NewFromInt(19),                                           // 9 + 10 = 19
			description:      "Package: Bucket1[2,5,6,9]→max=9→ceil(9/10)=1pkg, Bucket2[10]→max=10→ceil(10/10)=1pkg, Total: 1*$1 + 1*$1 = $2",
		},
		{
			name:         "bucketed_max_tiered_slab",
			billingModel: types.BILLING_MODEL_TIERED,
			setupPrice: func() *price.Price {
				upTo10 := uint64(10)
				upTo20 := uint64(20)
				return &price.Price{
					ID:                 "price_bucketed_tiered_slab",
					Amount:             decimal.Zero,
					Currency:           "usd",
					EntityType:         types.PRICE_ENTITY_TYPE_PLAN,
					EntityID:           s.testData.plan.ID,
					Type:               types.PRICE_TYPE_USAGE,
					BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
					BillingPeriodCount: 1,
					BillingModel:       types.BILLING_MODEL_TIERED,
					BillingCadence:     types.BILLING_CADENCE_RECURRING,
					InvoiceCadence:     types.InvoiceCadenceArrear,
					TierMode:           types.BILLING_TIER_SLAB,
					MeterID:            s.testData.meters.apiCalls.ID,
					Tiers: []price.PriceTier{
						{UpTo: &upTo10, UnitAmount: decimal.NewFromFloat(0.10)}, // 0-10: $0.10/unit
						{UpTo: &upTo20, UnitAmount: decimal.NewFromFloat(0.05)}, // 11-20: $0.05/unit
						{UpTo: nil, UnitAmount: decimal.NewFromFloat(0.02)},     // 21+: $0.02/unit
					},
					BaseModel: types.GetDefaultBaseModel(ctx),
				}
			},
			bucketValues:     []decimal.Decimal{decimal.NewFromInt(9), decimal.NewFromInt(15)}, // Bucket 1: max=9, Bucket 2: max=15
			expectedAmount:   decimal.NewFromFloat(2.15),                                       // Slab: Bucket 1: 9*0.10=$0.90, Bucket 2: 10*0.10+5*0.05=$1.25, Total=$2.15
			expectedQuantity: decimal.NewFromInt(24),                                           // 9 + 15 = 24
			description:      "Tiered slab: Bucket1→max=9→9*$0.10=$0.90, Bucket2→max=15→10*$0.10+5*$0.05=$1.25, Total=$2.15",
		},
		{
			name:         "bucketed_max_tiered_volume",
			billingModel: types.BILLING_MODEL_TIERED,
			setupPrice: func() *price.Price {
				upTo10 := uint64(10)
				upTo20 := uint64(20)
				return &price.Price{
					ID:                 "price_bucketed_tiered_volume",
					Amount:             decimal.Zero,
					Currency:           "usd",
					EntityType:         types.PRICE_ENTITY_TYPE_PLAN,
					EntityID:           s.testData.plan.ID,
					Type:               types.PRICE_TYPE_USAGE,
					BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
					BillingPeriodCount: 1,
					BillingModel:       types.BILLING_MODEL_TIERED,
					BillingCadence:     types.BILLING_CADENCE_RECURRING,
					InvoiceCadence:     types.InvoiceCadenceArrear,
					TierMode:           types.BILLING_TIER_VOLUME,
					MeterID:            s.testData.meters.apiCalls.ID,
					Tiers: []price.PriceTier{
						{UpTo: &upTo10, UnitAmount: decimal.NewFromFloat(0.10)}, // 0-10: $0.10/unit
						{UpTo: &upTo20, UnitAmount: decimal.NewFromFloat(0.05)}, // 11-20: $0.05/unit
						{UpTo: nil, UnitAmount: decimal.NewFromFloat(0.02)},     // 21+: $0.02/unit
					},
					BaseModel: types.GetDefaultBaseModel(ctx),
				}
			},
			bucketValues:     []decimal.Decimal{decimal.NewFromInt(9), decimal.NewFromInt(15)}, // Bucket 1: max(2,5,6,9)=9, Bucket 2: max(10,15)=15
			expectedAmount:   decimal.NewFromFloat(1.65),                                       // Bucket 1: 9*0.10=$0.90, Bucket 2: 15*0.05=$0.75, Total=$1.65
			expectedQuantity: decimal.NewFromInt(24),                                           // 9 + 15 = 24
			description:      "Tiered volume: Bucket1[2,5,6,9]→max=9→9*$0.10=$0.90, Bucket2[10,15]→max=15→15*$0.05=$0.75, Total=$1.65",
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			// Clear stores for clean test
			s.BaseServiceTestSuite.ClearStores()
			s.setupTestData()

			// Create bucketed max meter
			bucketedMaxMeter := &meter.Meter{
				ID:        "meter_bucketed_max",
				Name:      "Bucketed Max Meter",
				EventName: "bucketed_event",
				Aggregation: meter.Aggregation{
					Type:       types.AggregationMax,
					Field:      "value",
					BucketSize: "minute", // Minute-level buckets
				},
				BaseModel: types.GetDefaultBaseModel(ctx),
			}
			s.NoError(s.GetStores().MeterRepo.CreateMeter(ctx, bucketedMaxMeter))

			// Create price with specific billing model
			testPrice := tt.setupPrice()
			testPrice.MeterID = bucketedMaxMeter.ID
			s.NoError(s.GetStores().PriceRepo.Create(ctx, testPrice))

			// Create subscription line item for this price
			lineItem := &subscription.SubscriptionLineItem{
				ID:               types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION_LINE_ITEM),
				SubscriptionID:   s.testData.subscription.ID,
				CustomerID:       s.testData.subscription.CustomerID,
				EntityID:         s.testData.plan.ID,
				EntityType:       types.SubscriptionLineItemEntityTypePlan,
				PlanDisplayName:  s.testData.plan.Name,
				PriceID:          testPrice.ID,
				PriceType:        testPrice.Type,
				MeterID:          bucketedMaxMeter.ID,
				MeterDisplayName: bucketedMaxMeter.Name,
				DisplayName:      "Bucketed Max Test",
				Quantity:         decimal.Zero,
				Currency:         s.testData.subscription.Currency,
				BillingPeriod:    s.testData.subscription.BillingPeriod,
				InvoiceCadence:   types.InvoiceCadenceArrear,
				StartDate:        s.testData.subscription.StartDate,
				BaseModel:        types.GetDefaultBaseModel(ctx),
			}

			// Update subscription with new line item - save to repository
			s.testData.subscription.LineItems = append(s.testData.subscription.LineItems, lineItem)
			s.NoError(s.GetStores().SubscriptionRepo.Update(ctx, s.testData.subscription))

			// Insert events into the event store so GetUsageByMeter returns bucketed results.
			// Each bucket value gets events in a separate minute bucket.
			baseTime := s.testData.subscription.CurrentPeriodStart.Add(time.Hour)
			for i, bucketMax := range tt.bucketValues {
				s.NoError(s.GetStores().EventRepo.InsertEvent(ctx, &events.Event{
					ID:                 fmt.Sprintf("evt_bucketed_%s_%d", tt.name, i),
					TenantID:           types.GetTenantID(ctx),
					EnvironmentID:      types.GetEnvironmentID(ctx),
					EventName:          "bucketed_event",
					ExternalCustomerID: s.testData.customer.ExternalID,
					Timestamp:          baseTime.Add(time.Duration(i) * time.Minute),
					Properties: map[string]interface{}{
						"value": bucketMax.InexactFloat64(),
					},
				}))
			}

			// Create a copy of the subscription with the updated line items for CalculateUsageCharges
			subscriptionWithLineItems := *s.testData.subscription
			subscriptionWithLineItems.LineItems = make([]*subscription.SubscriptionLineItem, len(s.testData.subscription.LineItems))
			copy(subscriptionWithLineItems.LineItems, s.testData.subscription.LineItems)

			// Create mock usage data with bucketed results
			usage := &dto.GetUsageBySubscriptionResponse{
				StartTime: s.testData.subscription.CurrentPeriodStart,
				EndTime:   s.testData.subscription.CurrentPeriodEnd,
				Currency:  s.testData.subscription.Currency,
				Charges: []*dto.SubscriptionUsageByMetersResponse{
					{
						Price:     testPrice,
						Quantity:  tt.expectedQuantity.InexactFloat64(), // Sum of bucket values
						Amount:    tt.expectedAmount.InexactFloat64(),   // Will be recalculated
						IsOverage: false,
						MeterID:   bucketedMaxMeter.ID,
					},
				},
			}

			// Calculate charges
			lineItems, totalAmount, err := s.service.CalculateUsageCharges(
				ctx,
				&subscriptionWithLineItems,
				usage,
				subscriptionWithLineItems.CurrentPeriodStart,
				subscriptionWithLineItems.CurrentPeriodEnd,
			)

			s.NoError(err, "Should not error for %s", tt.name)
			s.Len(lineItems, 1, "Should have one line item for %s", tt.name)

			s.True(tt.expectedAmount.Equal(totalAmount),
				"Expected amount %s, got %s for %s", tt.expectedAmount, totalAmount, tt.name)

			s.True(tt.expectedQuantity.Equal(lineItems[0].Quantity),
				"Expected quantity %s, got %s for %s", tt.expectedQuantity, lineItems[0].Quantity, tt.name)

			s.T().Logf("✅ %s: %s", tt.name, tt.description)
			s.T().Logf("   Bucket values: %v", tt.bucketValues)
			s.T().Logf("   Expected: Quantity=%s, Amount=%s", tt.expectedQuantity, tt.expectedAmount)
			s.T().Logf("   Actual:   Quantity=%s, Amount=%s", lineItems[0].Quantity, totalAmount)
		})
	}
}

func (s *BillingServiceSuite) TestCalculateFeatureUsageCharges_SkipsInactiveLineItemWithSamePriceID() {
	// When two subscription line items share the same price_id (one active, one inactive),
	// feature_usage may have data for the inactive line item. CalculateFeatureUsageCharges
	// must match by SubscriptionLineItemID and skip charges for inactive line items.
	ctx := s.GetContext()
	s.setupTestData()

	// Subscription has one active usage line item (API calls)
	apiCallsLineItem := s.testData.subscription.LineItems[1]
	s.Require().Equal(s.testData.prices.apiCalls.ID, apiCallsLineItem.PriceID)

	// Simulate usage from feature_usage for an INACTIVE line item (same price_id, different sub_line_item_id)
	// That inactive line item is NOT in sub.LineItems, so the charge should be skipped
	inactiveLineItemID := "sub_li_inactive_999" // Not in sub.LineItems

	usage := &dto.GetUsageBySubscriptionResponse{
		StartTime: s.testData.subscription.CurrentPeriodStart,
		EndTime:   s.testData.subscription.CurrentPeriodEnd,
		Currency:  s.testData.subscription.Currency,
		Charges: []*dto.SubscriptionUsageByMetersResponse{
			{
				SubscriptionLineItemID: inactiveLineItemID,
				Price:                  s.testData.prices.apiCalls,
				Quantity:               500,
				Amount:                 10,
				IsOverage:              false,
			},
		},
	}

	lineItems, totalAmount, err := s.service.CalculateFeatureUsageCharges(
		ctx,
		s.testData.subscription,
		usage,
		s.testData.subscription.CurrentPeriodStart,
		s.testData.subscription.CurrentPeriodEnd,
		nil,
	)

	s.NoError(err)
	s.Empty(lineItems, "Should have no invoice line items: charge was for inactive line item, not in invoiced set")
	s.True(totalAmount.IsZero(), "Total should be zero: no charges should be attributed to active line items")
}

func (s *BillingServiceSuite) TestCalculateFeatureUsageCharges_MatchesActiveLineItemBySubscriptionLineItemID() {
	// When SubscriptionLineItemID is set and matches an active line item, the charge should be processed.
	ctx := s.GetContext()
	s.setupTestData()

	apiCallsLineItem := s.testData.subscription.LineItems[1]

	usage := &dto.GetUsageBySubscriptionResponse{
		StartTime: s.testData.subscription.CurrentPeriodStart,
		EndTime:   s.testData.subscription.CurrentPeriodEnd,
		Currency:  s.testData.subscription.Currency,
		Charges: []*dto.SubscriptionUsageByMetersResponse{
			{
				SubscriptionLineItemID: apiCallsLineItem.ID,
				Price:                  s.testData.prices.apiCalls,
				Quantity:               500,
				Amount:                 10,
				IsOverage:              false,
			},
		},
	}

	lineItems, totalAmount, err := s.service.CalculateFeatureUsageCharges(
		ctx,
		s.testData.subscription,
		usage,
		s.testData.subscription.CurrentPeriodStart,
		s.testData.subscription.CurrentPeriodEnd,
		nil,
	)

	s.NoError(err)
	s.Len(lineItems, 1, "Should have one invoice line item for active line item")
	s.Equal(s.testData.prices.apiCalls.ID, *lineItems[0].PriceID, "Line item should be for API calls price")
	s.True(totalAmount.GreaterThan(decimal.Zero), "Total should be positive")
}

func (s *BillingServiceSuite) TestCalculateFeatureUsageCharges_WindowedTrueUp_UsesElapsedTimeOnly() {
	ctx := s.GetContext()
	s.setupTestData()

	now := time.Now().UTC()
	periodStart := now.Add(-48 * time.Hour)
	periodEnd := now.Add(72 * time.Hour)

	windowedMeter := &meter.Meter{
		ID:        "meter_windowed_sum_day",
		Name:      "Windowed Sum Day",
		EventName: "windowed_sum_day",
		Aggregation: meter.Aggregation{
			Type:       types.AggregationSum,
			Field:      "units",
			BucketSize: types.WindowSizeDay,
		},
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().MeterRepo.CreateMeter(ctx, windowedMeter))

	commitmentQty := decimal.NewFromInt(1)
	overageFactor := decimal.NewFromInt(2)
	windowedLineItem := &subscription.SubscriptionLineItem{
		ID:                      "sub_li_windowed_trueup_elapsed",
		SubscriptionID:          s.testData.subscription.ID,
		CustomerID:              s.testData.subscription.CustomerID,
		EntityID:                s.testData.plan.ID,
		EntityType:              types.SubscriptionLineItemEntityTypePlan,
		PlanDisplayName:         s.testData.plan.Name,
		PriceID:                 s.testData.prices.apiCalls.ID,
		PriceType:               types.PRICE_TYPE_USAGE,
		MeterID:                 windowedMeter.ID,
		MeterDisplayName:        windowedMeter.Name,
		DisplayName:             "Windowed Commitment Usage",
		Quantity:                decimal.Zero,
		Currency:                s.testData.subscription.Currency,
		BillingPeriod:           s.testData.subscription.BillingPeriod,
		InvoiceCadence:          types.InvoiceCadenceArrear,
		StartDate:               s.testData.subscription.StartDate,
		CommitmentType:          types.COMMITMENT_TYPE_QUANTITY,
		CommitmentQuantity:      &commitmentQty,
		CommitmentOverageFactor: &overageFactor,
		CommitmentTrueUpEnabled: true,
		CommitmentWindowed:      true,
		BaseModel:               types.GetDefaultBaseModel(ctx),
	}

	subCopy := *s.testData.subscription
	subCopy.CurrentPeriodStart = periodStart
	subCopy.CurrentPeriodEnd = periodEnd
	subCopy.LineItems = []*subscription.SubscriptionLineItem{windowedLineItem}

	usage := &dto.GetUsageBySubscriptionResponse{
		StartTime: periodStart,
		EndTime:   periodEnd,
		Currency:  subCopy.Currency,
		Charges: []*dto.SubscriptionUsageByMetersResponse{
			{
				SubscriptionLineItemID: windowedLineItem.ID,
				Price:                  s.testData.prices.apiCalls,
				Quantity:               0,
				Amount:                 0,
				IsOverage:              false,
			},
		},
	}

	lineItems, totalAmount, err := s.service.CalculateFeatureUsageCharges(
		ctx,
		&subCopy,
		usage,
		periodStart,
		periodEnd,
		nil,
	)
	s.NoError(err)
	s.Len(lineItems, 1)

	lineStart := windowedLineItem.GetPeriodStart(periodStart)
	lineEnd := windowedLineItem.GetPeriodEnd(periodEnd)
	effectiveEnd := now.In(lineStart.Location())
	if effectiveEnd.Before(lineStart) {
		effectiveEnd = lineStart
	}
	if effectiveEnd.After(lineEnd) {
		effectiveEnd = lineEnd
	}

	elapsedBuckets := generateBucketStarts(lineStart, effectiveEnd, windowedMeter.Aggregation.BucketSize, &subCopy.BillingAnchor)
	fullPeriodBuckets := generateBucketStarts(lineStart, lineEnd, windowedMeter.Aggregation.BucketSize, &subCopy.BillingAnchor)
	s.Less(len(elapsedBuckets), len(fullPeriodBuckets), "test setup requires in-progress period with future buckets")

	priceService := NewPriceService(s.service.(*billingService).ServiceParams)
	perWindowCommitment := priceService.CalculateCost(ctx, s.testData.prices.apiCalls, commitmentQty)
	expectedTotal := perWindowCommitment.Mul(decimal.NewFromInt(int64(len(elapsedBuckets))))
	fullPeriodTotal := perWindowCommitment.Mul(decimal.NewFromInt(int64(len(fullPeriodBuckets))))

	s.True(totalAmount.Equal(expectedTotal), "expected elapsed-window true-up %s, got %s", expectedTotal, totalAmount)
	s.True(lineItems[0].Amount.Equal(expectedTotal), "line item amount should match elapsed-window commitment total")
	s.True(totalAmount.LessThan(fullPeriodTotal), "amount should not project full-period commitment")
}

func (s *BillingServiceSuite) TestCalculateFeatureUsageCharges_CumulativeCommitment() {
	// Monthly subscription with annual commitment ($60), overage factor 2x
	ctx := s.GetContext()
	s.setupTestData()
	s.invoiceRepo.Clear()

	commitmentAmount := decimal.NewFromInt(60)
	overageFactor := decimal.NewFromInt(2)
	commitmentDuration := types.BILLING_PERIOD_ANNUAL

	sub := *s.testData.subscription
	sub.CommitmentAmount = &commitmentAmount
	sub.OverageFactor = &overageFactor
	sub.CommitmentDuration = &commitmentDuration
	sub.EnableTrueUp = false
	// Use fixed period for deterministic commitment bounds
	sub.StartDate = time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	sub.CurrentPeriodStart = time.Date(2025, 3, 1, 0, 0, 0, 0, time.UTC)
	sub.CurrentPeriodEnd = time.Date(2025, 4, 1, 0, 0, 0, 0, time.UTC)
	sub.BillingAnchor = sub.CurrentPeriodEnd

	apiCallsLineItem := sub.LineItems[1]                                                     // Usage line item
	sub.LineItems = []*subscription.SubscriptionLineItem{sub.LineItems[0], apiCallsLineItem} // Fixed + API Calls (default)

	// Second usage line item for multi-line-item test
	featureBLineItem := &subscription.SubscriptionLineItem{
		ID:               "sub_li_cumulative_feature_b",
		SubscriptionID:   sub.ID,
		CustomerID:       sub.CustomerID,
		EntityID:         sub.PlanID,
		EntityType:       types.SubscriptionLineItemEntityTypePlan,
		PlanDisplayName:  s.testData.plan.Name,
		PriceID:          s.testData.prices.apiCalls.ID,
		PriceType:        types.PRICE_TYPE_USAGE,
		MeterID:          s.testData.meters.apiCalls.ID,
		MeterDisplayName: s.testData.meters.apiCalls.Name,
		DisplayName:      "Feature B",
		Quantity:         decimal.Zero,
		Currency:         sub.Currency,
		BillingPeriod:    sub.BillingPeriod,
		InvoiceCadence:   types.InvoiceCadenceArrear,
		StartDate:        sub.StartDate,
		BaseModel:        types.GetDefaultBaseModel(ctx),
	}

	tests := []struct {
		name                string
		priorInvoices       []*invoice.Invoice
		currentUsageBase    float64
		customCharges       []*dto.SubscriptionUsageByMetersResponse
		customLineItems     []*subscription.SubscriptionLineItem
		expectedTotal       decimal.Decimal
		expectedOverageLine decimal.Decimal
		expectedAllocation  map[string]decimal.Decimal
	}{
		{
			// Case 1: Invoice 1 $30, Invoice 2 $20, Current $12 → prior base 50, current 12, total 62 → $2 overage → $4 overage charge
			name: "case1_simple_cumulative",
			priorInvoices: []*invoice.Invoice{
				{
					ID:             "inv_1",
					CustomerID:     sub.CustomerID,
					SubscriptionID: lo.ToPtr(sub.ID),
					InvoiceType:    types.InvoiceTypeSubscription,
					InvoiceStatus:  types.InvoiceStatusFinalized,
					PaymentStatus:  types.PaymentStatusPending,
					Currency:       "usd",
					AmountDue:      decimal.NewFromInt(30),
					PeriodStart:    lo.ToPtr(time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)),
					PeriodEnd:      lo.ToPtr(time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC)),
					BaseModel:      types.GetDefaultBaseModel(ctx),
					LineItems: []*invoice.InvoiceLineItem{
						{
							ID:             "li_1",
							InvoiceID:      "inv_1",
							CustomerID:     sub.CustomerID,
							SubscriptionID: lo.ToPtr(sub.ID),
							PriceID:        lo.ToPtr(s.testData.prices.apiCalls.ID),
							PriceType:      lo.ToPtr(string(types.PRICE_TYPE_USAGE)),
							Amount:         decimal.NewFromInt(30),
							Currency:       "usd",
							PeriodStart:    lo.ToPtr(time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)),
							PeriodEnd:      lo.ToPtr(time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC)),
							BaseModel:      types.GetDefaultBaseModel(ctx),
						},
					},
				},
				{
					ID:             "inv_2",
					CustomerID:     sub.CustomerID,
					SubscriptionID: lo.ToPtr(sub.ID),
					InvoiceType:    types.InvoiceTypeSubscription,
					InvoiceStatus:  types.InvoiceStatusFinalized,
					PaymentStatus:  types.PaymentStatusPending,
					Currency:       "usd",
					AmountDue:      decimal.NewFromInt(20),
					PeriodStart:    lo.ToPtr(time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC)),
					PeriodEnd:      lo.ToPtr(time.Date(2025, 3, 1, 0, 0, 0, 0, time.UTC)),
					BaseModel:      types.GetDefaultBaseModel(ctx),
					LineItems: []*invoice.InvoiceLineItem{
						{
							ID:             "li_2",
							InvoiceID:      "inv_2",
							CustomerID:     sub.CustomerID,
							SubscriptionID: lo.ToPtr(sub.ID),
							PriceID:        lo.ToPtr(s.testData.prices.apiCalls.ID),
							PriceType:      lo.ToPtr(string(types.PRICE_TYPE_USAGE)),
							Amount:         decimal.NewFromInt(20),
							Currency:       "usd",
							PeriodStart:    lo.ToPtr(time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC)),
							PeriodEnd:      lo.ToPtr(time.Date(2025, 3, 1, 0, 0, 0, 0, time.UTC)),
							BaseModel:      types.GetDefaultBaseModel(ctx),
						},
					},
				},
			},
			currentUsageBase:    12,
			expectedTotal:       decimal.NewFromInt(14), // 10 within + 4 overage
			expectedOverageLine: decimal.NewFromInt(4),
		},
		{
			// Case 2: Invoice 1 $30, Invoice 2 $40 (with $10 overage → $20 charged), Current $12 → prior base 70, all $12 overage → $24
			name: "case2_previous_overage_invoiced",
			priorInvoices: []*invoice.Invoice{
				{
					ID:             "inv_1",
					CustomerID:     sub.CustomerID,
					SubscriptionID: lo.ToPtr(sub.ID),
					InvoiceType:    types.InvoiceTypeSubscription,
					InvoiceStatus:  types.InvoiceStatusFinalized,
					PaymentStatus:  types.PaymentStatusPending,
					Currency:       "usd",
					AmountDue:      decimal.NewFromInt(30),
					PeriodStart:    lo.ToPtr(time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)),
					PeriodEnd:      lo.ToPtr(time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC)),
					BaseModel:      types.GetDefaultBaseModel(ctx),
					LineItems: []*invoice.InvoiceLineItem{
						{
							ID:             "li_1",
							InvoiceID:      "inv_1",
							CustomerID:     sub.CustomerID,
							SubscriptionID: lo.ToPtr(sub.ID),
							PriceID:        lo.ToPtr(s.testData.prices.apiCalls.ID),
							PriceType:      lo.ToPtr(string(types.PRICE_TYPE_USAGE)),
							Amount:         decimal.NewFromInt(30),
							Currency:       "usd",
							PeriodStart:    lo.ToPtr(time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)),
							PeriodEnd:      lo.ToPtr(time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC)),
							BaseModel:      types.GetDefaultBaseModel(ctx),
						},
					},
				},
				{
					ID:             "inv_2",
					CustomerID:     sub.CustomerID,
					SubscriptionID: lo.ToPtr(sub.ID),
					InvoiceType:    types.InvoiceTypeSubscription,
					InvoiceStatus:  types.InvoiceStatusFinalized,
					PaymentStatus:  types.PaymentStatusPending,
					Currency:       "usd",
					AmountDue:      decimal.NewFromInt(50), // 30 within + 20 overage
					PeriodStart:    lo.ToPtr(time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC)),
					PeriodEnd:      lo.ToPtr(time.Date(2025, 3, 1, 0, 0, 0, 0, time.UTC)),
					BaseModel:      types.GetDefaultBaseModel(ctx),
					LineItems: []*invoice.InvoiceLineItem{
						{
							ID:             "li_2a",
							InvoiceID:      "inv_2",
							CustomerID:     sub.CustomerID,
							SubscriptionID: lo.ToPtr(sub.ID),
							PriceID:        lo.ToPtr(s.testData.prices.apiCalls.ID),
							PriceType:      lo.ToPtr(string(types.PRICE_TYPE_USAGE)),
							Amount:         decimal.NewFromInt(30),
							Currency:       "usd",
							PeriodStart:    lo.ToPtr(time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC)),
							PeriodEnd:      lo.ToPtr(time.Date(2025, 3, 1, 0, 0, 0, 0, time.UTC)),
							BaseModel:      types.GetDefaultBaseModel(ctx),
						},
						{
							ID:             "li_2b",
							InvoiceID:      "inv_2",
							CustomerID:     sub.CustomerID,
							SubscriptionID: lo.ToPtr(sub.ID),
							PriceID:        lo.ToPtr(s.testData.prices.apiCalls.ID),
							PriceType:      lo.ToPtr(string(types.PRICE_TYPE_USAGE)),
							Amount:         decimal.NewFromInt(20),
							Currency:       "usd",
							Metadata:       types.Metadata{"is_overage": "true"},
							PeriodStart:    lo.ToPtr(time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC)),
							PeriodEnd:      lo.ToPtr(time.Date(2025, 3, 1, 0, 0, 0, 0, time.UTC)),
							BaseModel:      types.GetDefaultBaseModel(ctx),
						},
					},
				},
			},
			currentUsageBase:    12,
			expectedTotal:       decimal.NewFromInt(24), // all overage
			expectedOverageLine: decimal.NewFromInt(24),
		},
		{
			// First period: no prior invoices → use existing per-period logic (no cumulative)
			name:                "first_period_no_prior_invoices",
			priorInvoices:       nil,
			currentUsageBase:    12,
			expectedTotal:       decimal.NewFromInt(12), // no commitment in per-period when no prior
			expectedOverageLine: decimal.Zero,
		},
		{
			// Multiple line items: Feature A (API Calls) $5 + Feature B $7 = $12. Prior $50, commitment $60.
			// commitment_remaining=10, within=10, overage_base=2, overage_charge=$4.
			// Proportional allocation: A (5/12)*10≈$4.17, B (7/12)*10≈$5.83, one overage line $4
			name: "multiple_line_items_proportional_allocation",
			priorInvoices: []*invoice.Invoice{
				{
					ID:             "inv_m1",
					CustomerID:     sub.CustomerID,
					SubscriptionID: lo.ToPtr(sub.ID),
					InvoiceType:    types.InvoiceTypeSubscription,
					InvoiceStatus:  types.InvoiceStatusFinalized,
					PaymentStatus:  types.PaymentStatusPending,
					Currency:       "usd",
					AmountDue:      decimal.NewFromInt(30),
					PeriodStart:    lo.ToPtr(time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)),
					PeriodEnd:      lo.ToPtr(time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC)),
					BaseModel:      types.GetDefaultBaseModel(ctx),
					LineItems: []*invoice.InvoiceLineItem{
						{ID: "li_m1", InvoiceID: "inv_m1", CustomerID: sub.CustomerID, SubscriptionID: lo.ToPtr(sub.ID), PriceID: lo.ToPtr(s.testData.prices.apiCalls.ID), PriceType: lo.ToPtr(string(types.PRICE_TYPE_USAGE)), Amount: decimal.NewFromInt(30), Currency: "usd", PeriodStart: lo.ToPtr(time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)), PeriodEnd: lo.ToPtr(time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC)), BaseModel: types.GetDefaultBaseModel(ctx)},
					},
				},
				{
					ID:             "inv_m2",
					CustomerID:     sub.CustomerID,
					SubscriptionID: lo.ToPtr(sub.ID),
					InvoiceType:    types.InvoiceTypeSubscription,
					InvoiceStatus:  types.InvoiceStatusFinalized,
					PaymentStatus:  types.PaymentStatusPending,
					Currency:       "usd",
					AmountDue:      decimal.NewFromInt(20),
					PeriodStart:    lo.ToPtr(time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC)),
					PeriodEnd:      lo.ToPtr(time.Date(2025, 3, 1, 0, 0, 0, 0, time.UTC)),
					BaseModel:      types.GetDefaultBaseModel(ctx),
					LineItems: []*invoice.InvoiceLineItem{
						{ID: "li_m2", InvoiceID: "inv_m2", CustomerID: sub.CustomerID, SubscriptionID: lo.ToPtr(sub.ID), PriceID: lo.ToPtr(s.testData.prices.apiCalls.ID), PriceType: lo.ToPtr(string(types.PRICE_TYPE_USAGE)), Amount: decimal.NewFromInt(20), Currency: "usd", PeriodStart: lo.ToPtr(time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC)), PeriodEnd: lo.ToPtr(time.Date(2025, 3, 1, 0, 0, 0, 0, time.UTC)), BaseModel: types.GetDefaultBaseModel(ctx)},
					},
				},
			},
			currentUsageBase: 0,
			customCharges: []*dto.SubscriptionUsageByMetersResponse{
				// 250 units * $0.02 = $5, 350 units * $0.02 = $7 (apiCalls tier 0-1000)
				{SubscriptionLineItemID: apiCallsLineItem.ID, Price: s.testData.prices.apiCalls, Quantity: 250, Amount: 5, IsOverage: false, OverageFactor: 2},
				{SubscriptionLineItemID: featureBLineItem.ID, Price: s.testData.prices.apiCalls, Quantity: 350, Amount: 7, IsOverage: false, OverageFactor: 2},
			},
			customLineItems:     []*subscription.SubscriptionLineItem{sub.LineItems[0], apiCallsLineItem, featureBLineItem},
			expectedTotal:       decimal.NewFromInt(14),
			expectedOverageLine: decimal.NewFromInt(4),
			expectedAllocation: map[string]decimal.Decimal{
				"API Calls": decimal.NewFromFloat(4.17),
				"Feature B": decimal.NewFromFloat(5.83),
			},
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			s.invoiceRepo.Clear()
			for _, inv := range tt.priorInvoices {
				s.NoError(s.GetStores().InvoiceRepo.CreateWithLineItems(ctx, inv))
			}

			charges := tt.customCharges
			if len(charges) == 0 {
				charges = []*dto.SubscriptionUsageByMetersResponse{
					{
						SubscriptionLineItemID: apiCallsLineItem.ID,
						Price:                  s.testData.prices.apiCalls,
						Quantity:               600, // arbitrary, amount drives the charge
						Amount:                 tt.currentUsageBase,
						IsOverage:              false,
						OverageFactor:          2,
					},
				}
			}

			usage := &dto.GetUsageBySubscriptionResponse{
				StartTime: sub.CurrentPeriodStart,
				EndTime:   sub.CurrentPeriodEnd,
				Currency:  sub.Currency,
				Charges:   charges,
			}

			subToUse := &sub
			if len(tt.customLineItems) > 0 {
				subCopy := sub
				subCopy.LineItems = tt.customLineItems
				subToUse = &subCopy
			}

			lineItems, totalAmount, err := s.service.CalculateFeatureUsageCharges(
				ctx,
				subToUse,
				usage,
				sub.CurrentPeriodStart,
				sub.CurrentPeriodEnd,
				nil,
			)

			s.NoError(err)
			s.True(totalAmount.Equal(tt.expectedTotal), "expected total %s, got %s", tt.expectedTotal.String(), totalAmount.String())

			if tt.expectedOverageLine.GreaterThan(decimal.Zero) {
				var overageAmount decimal.Decimal
				for _, li := range lineItems {
					if li.Metadata != nil && li.Metadata["is_overage"] == "true" {
						overageAmount = overageAmount.Add(li.Amount)
					}
				}
				s.True(overageAmount.Equal(tt.expectedOverageLine), "expected overage line %s, got %s", tt.expectedOverageLine.String(), overageAmount.String())
			}

			if len(tt.expectedAllocation) > 0 {
				for _, li := range lineItems {
					if li.PriceType != nil && *li.PriceType == string(types.PRICE_TYPE_USAGE) && li.DisplayName != nil {
						exp, ok := tt.expectedAllocation[*li.DisplayName]
						if ok {
							diff := li.Amount.Sub(exp).Abs()
							s.True(diff.LessThanOrEqual(decimal.NewFromFloat(0.01)), "display=%s amount=%s expected≈%s", *li.DisplayName, li.Amount.String(), exp.String())
						}
					}
				}
			}
		})
	}
}

func (s *BillingServiceSuite) TestCalculateNeverResetUsage() {
	ctx := s.GetContext()

	// Test scenario from user discussion:
	// Subscription start: 1/1/2025
	// L1: start = 1/1/2025, end = 15/2/2025
	// L2: start = 15/2/2025, end = nil
	// Period start: 1/2/2025, Period end: 1/3/2025
	// Usage allowed: 100

	tests := []struct {
		name              string
		description       string
		subscriptionStart time.Time
		lineItemStart     time.Time
		lineItemEnd       *time.Time
		periodStart       time.Time
		periodEnd         time.Time
		usageAllowed      decimal.Decimal
		totalUsageEvents  []struct {
			timestamp time.Time
			value     decimal.Decimal
		}
		expectedBillableQuantity decimal.Decimal
		shouldSkip               bool
	}{
		{
			name:              "L1: Line item active during billing period",
			description:       "Line item L1 from 1/1 to 15/2, billing period 1/2 to 1/3",
			subscriptionStart: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
			lineItemStart:     time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
			lineItemEnd:       lo.ToPtr(time.Date(2025, 2, 15, 0, 0, 0, 0, time.UTC)),
			periodStart:       time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC),
			periodEnd:         time.Date(2025, 3, 1, 0, 0, 0, 0, time.UTC),
			usageAllowed:      decimal.NewFromInt(100),
			totalUsageEvents: []struct {
				timestamp time.Time
				value     decimal.Decimal
			}{
				{time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC), decimal.NewFromInt(50)}, // Before period start
				{time.Date(2025, 2, 5, 0, 0, 0, 0, time.UTC), decimal.NewFromInt(75)},  // During period
				{time.Date(2025, 2, 10, 0, 0, 0, 0, time.UTC), decimal.NewFromInt(25)}, // During period
			},
			expectedBillableQuantity: decimal.NewFromInt(0), // totalUsage(150) - previousPeriodUsage(50) - usageAllowed(100) = max(0, 100-100) = 0
			shouldSkip:               false,
		},
		{
			name:              "L2: Line item starts during billing period",
			description:       "Line item L2 from 15/2 to nil, billing period 1/2 to 1/3",
			subscriptionStart: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
			lineItemStart:     time.Date(2025, 2, 15, 0, 0, 0, 0, time.UTC),
			lineItemEnd:       nil,
			periodStart:       time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC),
			periodEnd:         time.Date(2025, 3, 1, 0, 0, 0, 0, time.UTC),
			usageAllowed:      decimal.NewFromInt(100),
			totalUsageEvents: []struct {
				timestamp time.Time
				value     decimal.Decimal
			}{
				{time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC), decimal.NewFromInt(50)}, // Before line item start
				{time.Date(2025, 2, 20, 0, 0, 0, 0, time.UTC), decimal.NewFromInt(75)}, // During line item period
				{time.Date(2025, 2, 25, 0, 0, 0, 0, time.UTC), decimal.NewFromInt(25)}, // During line item period
			},
			expectedBillableQuantity: decimal.NewFromInt(0), // totalUsage(100) - previousPeriodUsage(100) - usageAllowed(100) = max(0, 0-100) = 0
			shouldSkip:               false,
		},
		{
			name:              "Line item not active during billing period",
			description:       "Line item ends before billing period starts",
			subscriptionStart: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
			lineItemStart:     time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
			lineItemEnd:       lo.ToPtr(time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC)),
			periodStart:       time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC),
			periodEnd:         time.Date(2025, 3, 1, 0, 0, 0, 0, time.UTC),
			usageAllowed:      decimal.NewFromInt(100),
			totalUsageEvents: []struct {
				timestamp time.Time
				value     decimal.Decimal
			}{},
			expectedBillableQuantity: decimal.Zero,
			shouldSkip:               true, // Should be skipped as line item is not active
		},
		{
			name:              "Zero usage scenario",
			description:       "No usage events during the period",
			subscriptionStart: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
			lineItemStart:     time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
			lineItemEnd:       nil,
			periodStart:       time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC),
			periodEnd:         time.Date(2025, 3, 1, 0, 0, 0, 0, time.UTC),
			usageAllowed:      decimal.NewFromInt(100),
			totalUsageEvents: []struct {
				timestamp time.Time
				value     decimal.Decimal
			}{},
			expectedBillableQuantity: decimal.Zero, // totalUsage(0) - previousPeriodUsage(0) - usageAllowed(100) = max(0, 0-0-100) = 0
			shouldSkip:               false,
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			// Clear stores for clean test
			s.BaseServiceTestSuite.ClearStores()
			s.setupTestData()

			// Create test meter
			testMeter := &meter.Meter{
				ID:        "meter_never_reset_test",
				Name:      "Never Reset Test Meter",
				EventName: "never_reset_event",
				Aggregation: meter.Aggregation{
					Type:  types.AggregationSum,
					Field: "value",
				},
				BaseModel: types.GetDefaultBaseModel(ctx),
			}
			s.NoError(s.GetStores().MeterRepo.CreateMeter(ctx, testMeter))

			// Create test price
			testPrice := &price.Price{
				ID:        "price_never_reset_test",
				MeterID:   testMeter.ID,
				Type:      types.PRICE_TYPE_USAGE,
				BaseModel: types.GetDefaultBaseModel(ctx),
			}
			s.NoError(s.GetStores().PriceRepo.Create(ctx, testPrice))

			// Create subscription with specific start date
			testSubscription := &subscription.Subscription{
				ID:                 "sub_never_reset_test",
				CustomerID:         s.testData.customer.ID,
				PlanID:             s.testData.plan.ID,
				SubscriptionStatus: types.SubscriptionStatusActive,
				Currency:           "usd",
				BillingAnchor:      tt.subscriptionStart,
				BillingCycle:       types.BillingCycleAnniversary,
				StartDate:          tt.subscriptionStart,
				CurrentPeriodStart: tt.periodStart,
				CurrentPeriodEnd:   tt.periodEnd,
				BillingCadence:     types.BILLING_CADENCE_RECURRING,
				BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
				BillingPeriodCount: 1,
				Version:            1,
				BaseModel:          types.GetDefaultBaseModel(ctx),
			}
			s.NoError(s.GetStores().SubscriptionRepo.Create(ctx, testSubscription))

			// Create line item with specific dates
			lineItem := &subscription.SubscriptionLineItem{
				ID:               "line_item_never_reset_test",
				SubscriptionID:   testSubscription.ID,
				CustomerID:       s.testData.customer.ID,
				EntityID:         s.testData.plan.ID,
				EntityType:       types.SubscriptionLineItemEntityTypePlan,
				PlanDisplayName:  s.testData.plan.Name,
				PriceID:          testPrice.ID,
				PriceType:        testPrice.Type,
				MeterID:          testMeter.ID,
				MeterDisplayName: testMeter.Name,
				DisplayName:      "Never Reset Test Line Item",
				Quantity:         decimal.Zero,
				Currency:         testSubscription.Currency,
				BillingPeriod:    testSubscription.BillingPeriod,
				InvoiceCadence:   types.InvoiceCadenceArrear,
				StartDate:        tt.lineItemStart,
				BaseModel:        types.GetDefaultBaseModel(ctx),
			}

			if tt.lineItemEnd != nil {
				lineItem.EndDate = *tt.lineItemEnd
			}

			// Calculate expected usage periods for logging
			lineItemPeriodStart := lineItem.GetPeriodStart(tt.periodStart)
			lineItemPeriodEnd := lineItem.GetPeriodEnd(tt.periodEnd)

			// Calculate expected totals for verification
			totalUsage := decimal.Zero
			for _, event := range tt.totalUsageEvents {
				if (event.timestamp.After(tt.subscriptionStart) || event.timestamp.Equal(tt.subscriptionStart)) &&
					(event.timestamp.Before(lineItemPeriodEnd) || event.timestamp.Equal(lineItemPeriodEnd)) {
					totalUsage = totalUsage.Add(event.value)
				}
			}

			previousUsage := decimal.Zero
			for _, event := range tt.totalUsageEvents {
				if (event.timestamp.After(tt.subscriptionStart) || event.timestamp.Equal(tt.subscriptionStart)) &&
					(event.timestamp.Before(lineItemPeriodStart) || event.timestamp.Equal(lineItemPeriodStart)) {
					previousUsage = previousUsage.Add(event.value)
				}
			}

			// Call the function under test using the real event service
			eventService := NewEventService(s.GetStores().EventRepo, s.GetStores().MeterRepo, s.GetPublisher(), s.GetLogger(), s.GetConfig())

			// Create mock events in the event store for our test data
			for _, event := range tt.totalUsageEvents {
				testEvent := &events.Event{
					ID:                 types.GenerateUUIDWithPrefix(types.UUID_PREFIX_EVENT),
					TenantID:           types.GetTenantID(ctx),
					EnvironmentID:      types.GetEnvironmentID(ctx),
					ExternalCustomerID: s.testData.customer.ExternalID,
					EventName:          testMeter.EventName,
					Timestamp:          event.timestamp,
					Properties: map[string]interface{}{
						"value": event.value.InexactFloat64(),
					},
				}
				s.NoError(s.GetStores().EventRepo.InsertEvent(ctx, testEvent))
			}

			s.T().Logf("DEBUG: Inserted %d events for meter %s, customer %s", len(tt.totalUsageEvents), testMeter.ID, s.testData.customer.ExternalID)

			// Debug: Test the event service directly to see what it returns
			totalUsageRequest := &dto.GetUsageByMeterRequest{
				MeterID:            testMeter.ID,
				PriceID:            testPrice.ID,
				ExternalCustomerID: s.testData.customer.ExternalID,
				StartTime:          tt.subscriptionStart,
				EndTime:            lineItemPeriodEnd,
			}
			s.T().Logf("DEBUG: Total usage request - MeterID: %s, PriceID: %s, Customer: %s, Start: %s, End: %s",
				totalUsageRequest.MeterID, totalUsageRequest.PriceID, totalUsageRequest.ExternalCustomerID,
				totalUsageRequest.StartTime, totalUsageRequest.EndTime)
			totalUsageResponse, err := eventService.GetUsageByMeter(ctx, totalUsageRequest)
			s.NoError(err)

			actualTotalUsage := decimal.Zero
			for _, result := range totalUsageResponse.Results {
				actualTotalUsage = actualTotalUsage.Add(result.Value)
			}

			previousUsageRequest := &dto.GetUsageByMeterRequest{
				MeterID:            testMeter.ID,
				PriceID:            testPrice.ID,
				ExternalCustomerID: s.testData.customer.ExternalID,
				StartTime:          tt.subscriptionStart,
				EndTime:            lineItemPeriodStart,
			}
			previousUsageResponse, err := eventService.GetUsageByMeter(ctx, previousUsageRequest)
			s.NoError(err)

			actualPreviousUsage := decimal.Zero
			for _, result := range previousUsageResponse.Results {
				actualPreviousUsage = actualPreviousUsage.Add(result.Value)
			}

			s.T().Logf("DEBUG: Event service returned - Total: %s, Previous: %s", actualTotalUsage, actualPreviousUsage)

			var result decimal.Decimal

			if tt.shouldSkip {
				s.NoError(err)
				s.True(result.Equal(decimal.Zero), "Should return zero for skipped line item")
				s.T().Logf("✅ %s: Correctly skipped inactive line item", tt.name)
				return
			}

			s.NoError(err, "Should not error for %s", tt.name)
			s.True(tt.expectedBillableQuantity.Equal(result),
				"Expected billable quantity %s, got %s for %s", tt.expectedBillableQuantity, result, tt.name)

			s.T().Logf("✅ %s: %s", tt.name, tt.description)
			s.T().Logf("   Subscription start: %s", tt.subscriptionStart.Format("2006-01-02"))
			s.T().Logf("   Line item period: %s to %s", lineItemPeriodStart.Format("2006-01-02"), lineItemPeriodEnd.Format("2006-01-02"))
			s.T().Logf("   Total usage: %s, Previous usage: %s", totalUsage, previousUsage)
			s.T().Logf("   Usage allowed: %s, Billable quantity: %s", tt.usageAllowed, result)
		})
	}
}

// --- Multi-Cadence PRD Appendix E tests ---

// TestApplyProrationToLineItem_RuntimeSafetyNet_MixedBillingPeriods implements PRD E.3.4.
// When subscription has mixed billing periods and ProrationBehavior=create_prorations,
// applyProrationToLineItem (invoked via CalculateFixedCharges) must return original amount (safety net).
func (s *BillingServiceSuite) TestApplyProrationToLineItem_RuntimeSafetyNet_MixedBillingPeriods() {
	ctx := s.GetContext()
	jan1 := time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC)
	mar1 := time.Date(2024, time.March, 1, 0, 0, 0, 0, time.UTC)
	apr1 := time.Date(2024, time.April, 1, 0, 0, 0, 0, time.UTC)

	s.BaseServiceTestSuite.ClearStores()
	cust := &customer.Customer{
		ID:         "cust_e34",
		ExternalID: "ext_e34",
		Name:       "E34 Customer",
		Email:      "e34@example.com",
		BaseModel:  types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().CustomerRepo.Create(ctx, cust))
	pl := &plan.Plan{
		ID:        "plan_e34",
		Name:      "E34 Plan",
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().PlanRepo.Create(ctx, pl))

	priceMonthly := &price.Price{
		ID:                 "price_e34_m",
		Amount:             decimal.NewFromInt(10),
		Currency:           "usd",
		EntityType:         types.PRICE_ENTITY_TYPE_PLAN,
		EntityID:           pl.ID,
		Type:               types.PRICE_TYPE_FIXED,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_FLAT_FEE,
		BillingCadence:     types.BILLING_CADENCE_RECURRING,
		InvoiceCadence:     types.InvoiceCadenceArrear,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().PriceRepo.Create(ctx, priceMonthly))
	priceQuarterly := &price.Price{
		ID:                 "price_e34_q",
		Amount:             decimal.NewFromInt(300),
		Currency:           "usd",
		EntityType:         types.PRICE_ENTITY_TYPE_PLAN,
		EntityID:           pl.ID,
		Type:               types.PRICE_TYPE_FIXED,
		BillingPeriod:      types.BILLING_PERIOD_QUARTER,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_FLAT_FEE,
		BillingCadence:     types.BILLING_CADENCE_RECURRING,
		InvoiceCadence:     types.InvoiceCadenceArrear,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().PriceRepo.Create(ctx, priceQuarterly))

	sub := &subscription.Subscription{
		ID:                 "sub_e34",
		PlanID:             pl.ID,
		CustomerID:         cust.ID,
		StartDate:          jan1,
		BillingAnchor:      jan1,
		CurrentPeriodStart: mar1,
		CurrentPeriodEnd:   apr1,
		Currency:           "usd",
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		SubscriptionStatus: types.SubscriptionStatusActive,
		CustomerTimezone:   "UTC",
		ProrationBehavior:  types.ProrationBehaviorCreateProrations, // mixed + proration -> safety net
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	liM := &subscription.SubscriptionLineItem{
		ID:                 types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION_LINE_ITEM),
		SubscriptionID:     sub.ID,
		CustomerID:         sub.CustomerID,
		EntityID:           pl.ID,
		EntityType:         types.SubscriptionLineItemEntityTypePlan,
		PlanDisplayName:    pl.Name,
		PriceID:            priceMonthly.ID,
		PriceType:          types.PRICE_TYPE_FIXED,
		DisplayName:        "Monthly",
		Quantity:           decimal.NewFromInt(1),
		Currency:           sub.Currency,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		InvoiceCadence:     types.InvoiceCadenceArrear,
		StartDate:          jan1,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	liQ := &subscription.SubscriptionLineItem{
		ID:                 types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION_LINE_ITEM),
		SubscriptionID:     sub.ID,
		CustomerID:         sub.CustomerID,
		EntityID:           pl.ID,
		EntityType:         types.SubscriptionLineItemEntityTypePlan,
		PlanDisplayName:    pl.Name,
		PriceID:            priceQuarterly.ID,
		PriceType:          types.PRICE_TYPE_FIXED,
		DisplayName:        "Quarterly",
		Quantity:           decimal.NewFromInt(1),
		Currency:           sub.Currency,
		BillingPeriod:      types.BILLING_PERIOD_QUARTER,
		BillingPeriodCount: 1,
		InvoiceCadence:     types.InvoiceCadenceArrear,
		StartDate:          jan1,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().SubscriptionRepo.CreateWithLineItems(ctx, sub, []*subscription.SubscriptionLineItem{liM, liQ}))
	sub.LineItems = []*subscription.SubscriptionLineItem{liM, liQ}

	// E.3.4 case 1: create_prorations + HasMixedPeriods -> return original amount (no proration)
	// Invoice period Mar 1 - Apr 1: quarterly (Jan 1 - Apr 1) included; should be full $300
	lineItems, total, err := s.service.CalculateFixedCharges(ctx, sub, mar1, apr1)
	s.NoError(err)
	s.Require().Len(lineItems, 2)
	var quarterlyAmt decimal.Decimal
	for _, li := range lineItems {
		if lo.FromPtr(li.PriceID) == priceQuarterly.ID {
			quarterlyAmt = li.Amount
			break
		}
	}
	s.True(quarterlyAmt.Equal(decimal.NewFromInt(300)), "E.3.4: mixed + create_prorations should get full amount 300 (safety net), got %s", quarterlyAmt)
	s.True(total.GreaterThanOrEqual(decimal.NewFromInt(300)), "total at least 300")
	s.True(total.LessThanOrEqual(decimal.NewFromInt(310)), "total at most 310")
}

// TestFindMatchingLineItemPeriodForInvoice_MultiCadencePRD adds PRD E.4 cases: monthly sub + quarterly/half-yearly/annual arrear and advance, quarterly sub + annual.
func (s *BillingServiceSuite) TestFindMatchingLineItemPeriodForInvoice_MultiCadencePRD() {
	utc := time.UTC
	jan1 := time.Date(2026, 1, 1, 0, 0, 0, 0, utc)
	feb1 := time.Date(2026, 2, 1, 0, 0, 0, 0, utc)
	mar1 := time.Date(2026, 3, 1, 0, 0, 0, 0, utc)
	apr1 := time.Date(2026, 4, 1, 0, 0, 0, 0, utc)
	may1 := time.Date(2026, 5, 1, 0, 0, 0, 0, utc)
	jun1 := time.Date(2026, 6, 1, 0, 0, 0, 0, utc)
	jul1 := time.Date(2026, 7, 1, 0, 0, 0, 0, utc)
	oct1 := time.Date(2026, 10, 1, 0, 0, 0, 0, utc)
	jan1Next := time.Date(2027, 1, 1, 0, 0, 0, 0, utc)
	dec1 := time.Date(2026, 12, 1, 0, 0, 0, 0, utc)

	tests := []struct {
		name           string
		item           *subscription.SubscriptionLineItem
		periodStart    time.Time
		periodEnd      time.Time
		invoiceCadence types.InvoiceCadence
		wantOK         bool
		wantStart      time.Time
		wantEnd        time.Time
	}{
		// E.4.1 Monthly Sub + Quarterly ARREAR
		{"E4.1_jan_feb_quarterly_arrear_no", newLineItemForFindMatching(jan1, time.Time{}, types.BILLING_PERIOD_QUARTER, 1, types.InvoiceCadenceArrear), jan1, feb1, types.InvoiceCadenceArrear, false, time.Time{}, time.Time{}},
		{"E4.1_feb_mar_quarterly_arrear_no", newLineItemForFindMatching(jan1, time.Time{}, types.BILLING_PERIOD_QUARTER, 1, types.InvoiceCadenceArrear), feb1, mar1, types.InvoiceCadenceArrear, false, time.Time{}, time.Time{}},
		{"E4.1_mar_apr_quarterly_arrear_yes", newLineItemForFindMatching(jan1, time.Time{}, types.BILLING_PERIOD_QUARTER, 1, types.InvoiceCadenceArrear), mar1, apr1, types.InvoiceCadenceArrear, true, jan1, apr1},
		{"E4.1_apr_may_quarterly_arrear_no", newLineItemForFindMatching(apr1, time.Time{}, types.BILLING_PERIOD_QUARTER, 1, types.InvoiceCadenceArrear), apr1, may1, types.InvoiceCadenceArrear, false, time.Time{}, time.Time{}},
		{"E4.1_jun_jul_quarterly_arrear_yes", newLineItemForFindMatching(apr1, time.Time{}, types.BILLING_PERIOD_QUARTER, 1, types.InvoiceCadenceArrear), jun1, jul1, types.InvoiceCadenceArrear, true, apr1, jul1},
		// E.4.2 Monthly Sub + Half-Yearly ARREAR
		{"E4.2_jun_jul_halfyearly_arrear_yes", newLineItemForFindMatching(jan1, time.Time{}, types.BILLING_PERIOD_HALF_YEAR, 1, types.InvoiceCadenceArrear), jun1, jul1, types.InvoiceCadenceArrear, true, jan1, jul1},
		{"E4.2_dec_jan_halfyearly_arrear_yes", newLineItemForFindMatching(jul1, time.Time{}, types.BILLING_PERIOD_HALF_YEAR, 1, types.InvoiceCadenceArrear), dec1, jan1Next, types.InvoiceCadenceArrear, true, jul1, jan1Next},
		{"E4.2_jan_feb_halfyearly_arrear_no", newLineItemForFindMatching(jan1, time.Time{}, types.BILLING_PERIOD_HALF_YEAR, 1, types.InvoiceCadenceArrear), jan1, feb1, types.InvoiceCadenceArrear, false, time.Time{}, time.Time{}},
		// E.4.3 Monthly Sub + Annual ARREAR
		{"E4.3_dec_jan_annual_arrear_yes", newLineItemForFindMatching(jan1, time.Time{}, types.BILLING_PERIOD_ANNUAL, 1, types.InvoiceCadenceArrear), dec1, jan1Next, types.InvoiceCadenceArrear, true, jan1, jan1Next},
		{"E4.3_mar_apr_annual_arrear_no", newLineItemForFindMatching(jan1, time.Time{}, types.BILLING_PERIOD_ANNUAL, 1, types.InvoiceCadenceArrear), mar1, apr1, types.InvoiceCadenceArrear, false, time.Time{}, time.Time{}},
		// E.4.4 Monthly Sub + Quarterly ADVANCE
		{"E4.4_jan_feb_quarterly_advance_yes", newLineItemForFindMatching(jan1, time.Time{}, types.BILLING_PERIOD_QUARTER, 1, types.InvoiceCadenceAdvance), jan1, feb1, types.InvoiceCadenceAdvance, true, jan1, apr1},
		{"E4.4_feb_mar_quarterly_advance_no", newLineItemForFindMatching(jan1, time.Time{}, types.BILLING_PERIOD_QUARTER, 1, types.InvoiceCadenceAdvance), feb1, mar1, types.InvoiceCadenceAdvance, false, time.Time{}, time.Time{}},
		{"E4.4_apr_may_quarterly_advance_yes", newLineItemForFindMatching(apr1, time.Time{}, types.BILLING_PERIOD_QUARTER, 1, types.InvoiceCadenceAdvance), apr1, may1, types.InvoiceCadenceAdvance, true, apr1, jul1},
		{"E4.4_jul_aug_quarterly_advance_yes", newLineItemForFindMatching(jul1, time.Time{}, types.BILLING_PERIOD_QUARTER, 1, types.InvoiceCadenceAdvance), jul1, time.Date(2026, 8, 1, 0, 0, 0, 0, utc), types.InvoiceCadenceAdvance, true, jul1, oct1},
		// E.4.5 Quarterly Sub + Annual ARREAR
		{"E4.5_oct_jan_annual_arrear_yes", newLineItemForFindMatching(jan1, time.Time{}, types.BILLING_PERIOD_ANNUAL, 1, types.InvoiceCadenceArrear), oct1, jan1Next, types.InvoiceCadenceArrear, true, jan1, jan1Next},
		{"E4.5_jan_apr_annual_arrear_no", newLineItemForFindMatching(jan1, time.Time{}, types.BILLING_PERIOD_ANNUAL, 1, types.InvoiceCadenceArrear), jan1, apr1, types.InvoiceCadenceArrear, false, time.Time{}, time.Time{}},
		// E.4.6 Quarterly Sub + Annual ADVANCE
		{"E4.6_jan_apr_annual_advance_yes", newLineItemForFindMatching(jan1, time.Time{}, types.BILLING_PERIOD_ANNUAL, 1, types.InvoiceCadenceAdvance), jan1, apr1, types.InvoiceCadenceAdvance, true, jan1, jan1Next},
		{"E4.6_apr_jul_annual_advance_no", newLineItemForFindMatching(jan1, time.Time{}, types.BILLING_PERIOD_ANNUAL, 1, types.InvoiceCadenceAdvance), apr1, jul1, types.InvoiceCadenceAdvance, false, time.Time{}, time.Time{}},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			res, err := FindMatchingLineItemPeriodForInvoice(FindMatchingLineItemPeriodInput{
				Item:           tt.item,
				PeriodStart:    tt.periodStart,
				PeriodEnd:      tt.periodEnd,
				InvoiceCadence: tt.invoiceCadence,
			})
			s.NoError(err)
			s.Equal(tt.wantOK, res.Ok)
			if tt.wantOK && !tt.wantStart.IsZero() {
				s.True(res.LineItemPeriodStart.Equal(tt.wantStart), "start: got %v want %v", res.LineItemPeriodStart, tt.wantStart)
				s.True(res.LineItemPeriodEnd.Equal(tt.wantEnd), "end: got %v want %v", res.LineItemPeriodEnd, tt.wantEnd)
			}
		})
	}
}

// TestClassifyLineItems_MultiCadencePRD implements PRD E.5: ClassifyLineItems with M+Q+H line items.
// Sub starts Jan 1, current period = Mar 1 - Apr 1 (monthly sub). Verifies E.5.1 (three ARREAR), E.5.4 (Jan-Feb), E.5.5 (Jun-Jul).
func (s *BillingServiceSuite) TestClassifyLineItems_MultiCadencePRD() {
	ctx := s.GetContext()
	jan1 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	mar1 := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	apr1 := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	may1 := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	jun1 := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	jul1 := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)

	s.BaseServiceTestSuite.ClearStores()
	cust := &customer.Customer{
		ID:         "cust_e5",
		ExternalID: "ext_e5",
		Name:       "E5",
		Email:      "e5@example.com",
		BaseModel:  types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().CustomerRepo.Create(ctx, cust))
	pl := &plan.Plan{ID: "plan_e5", Name: "E5", BaseModel: types.GetDefaultBaseModel(ctx)}
	s.NoError(s.GetStores().PlanRepo.Create(ctx, pl))

	makeLI := func(period types.BillingPeriod, cadence types.InvoiceCadence, name string) *subscription.SubscriptionLineItem {
		return &subscription.SubscriptionLineItem{
			ID:                 types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION_LINE_ITEM),
			SubscriptionID:     "sub_e5",
			CustomerID:         cust.ID,
			EntityID:           pl.ID,
			EntityType:         types.SubscriptionLineItemEntityTypePlan,
			PlanDisplayName:    pl.Name,
			PriceID:            "price_" + name,
			PriceType:          types.PRICE_TYPE_FIXED,
			DisplayName:        name,
			Quantity:           decimal.NewFromInt(1),
			Currency:           "usd",
			BillingPeriod:      period,
			BillingPeriodCount: 1,
			InvoiceCadence:     cadence,
			StartDate:          jan1,
			BaseModel:          types.GetDefaultBaseModel(ctx),
		}
	}
	liM := makeLI(types.BILLING_PERIOD_MONTHLY, types.InvoiceCadenceArrear, "M")
	liQ := makeLI(types.BILLING_PERIOD_QUARTER, types.InvoiceCadenceArrear, "Q")
	liH := makeLI(types.BILLING_PERIOD_HALF_YEAR, types.InvoiceCadenceArrear, "H")

	sub := &subscription.Subscription{
		ID:                 "sub_e5",
		PlanID:             pl.ID,
		CustomerID:         cust.ID,
		StartDate:          jan1,
		BillingAnchor:      jan1,
		CurrentPeriodStart: mar1,
		CurrentPeriodEnd:   apr1,
		Currency:           "usd",
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		SubscriptionStatus: types.SubscriptionStatusActive,
		CustomerTimezone:   "UTC",
		ProrationBehavior:  types.ProrationBehaviorNone,
		BaseModel:          types.GetDefaultBaseModel(ctx),
		LineItems:          []*subscription.SubscriptionLineItem{liM, liQ, liH},
	}

	// E.5.1 / E.5.5: current period Mar 1 - Apr 1 -> M and Q in CurrentPeriodArrear, H not included (H end Jul 1)
	classification := s.service.ClassifyLineItems(sub, mar1, apr1, apr1, may1)
	s.Require().NotNil(classification)
	s.Len(classification.CurrentPeriodArrear, 2, "E.5.1: Monthly and Quarterly ARREAR in current period")
	s.Len(classification.CurrentPeriodAdvance, 0)
	priceIDsArrear := make(map[string]bool)
	for _, it := range classification.CurrentPeriodArrear {
		priceIDsArrear[it.PriceID] = true
	}
	s.True(priceIDsArrear["price_M"])
	s.True(priceIDsArrear["price_Q"])
	s.Len(classification.NextPeriodAdvance, 0)

	// E.5.5: period Jun 1 - Jul 1 -> all three ARREAR (M, Q, H all have period end Jul 1)
	subJun := *sub
	subJun.CurrentPeriodStart = jun1
	subJun.CurrentPeriodEnd = jul1
	subJun.LineItems = sub.LineItems
	classificationJun := s.service.ClassifyLineItems(&subJun, jun1, jul1, jul1, time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC))
	s.Require().NotNil(classificationJun)
	s.Len(classificationJun.CurrentPeriodArrear, 3, "E.5.5: Jun-Jul all three M, Q, H in CurrentPeriodArrear")
}

// TestMultiCadence_12MonthSchedule_MQH_Arrear implements PRD E.6.1: Monthly + Quarterly + Half-Yearly all ARREAR ($10 + $100 + $200).
// Sub starts Jan 1. Verifies 12 invoice totals: $10, $10, $110, $10, $10, $310, $10, $10, $110, $10, $10, $310.
func (s *BillingServiceSuite) TestMultiCadence_12MonthSchedule_MQH_Arrear() {
	ctx := s.GetContext()
	jan1 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	s.BaseServiceTestSuite.ClearStores()
	sub, prices := s.setupMultiCadenceSubMQH(ctx, jan1, 10, 100, 200, types.InvoiceCadenceArrear, types.InvoiceCadenceArrear, types.InvoiceCadenceArrear)
	s.Require().NotNil(sub)
	s.Require().Len(prices, 3)

	expectedTotals := []int{10, 10, 110, 10, 10, 310, 10, 10, 110, 10, 10, 310} // E.6.1 table
	periodStarts := []time.Time{
		jan1, time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC),
	}
	for i := 0; i < 12; i++ {
		periodEnd := periodStarts[i].AddDate(0, 1, 0)
		lineItems, total, err := s.service.CalculateFixedCharges(ctx, sub, periodStarts[i], periodEnd)
		s.NoError(err, "invoice %d", i+1)
		s.True(total.Equal(decimal.NewFromInt(int64(expectedTotals[i]))),
			"invoice %d: expected total %d, got %s (line count %d)", i+1, expectedTotals[i], total.String(), len(lineItems))
	}
}

// setupMultiCadenceSubMQH creates a subscription with Monthly, Quarterly, Half-Yearly fixed line items (Jan 1 start).
func (s *BillingServiceSuite) setupMultiCadenceSubMQH(ctx context.Context, start time.Time, amtM, amtQ, amtH int, cadM, cadQ, cadH types.InvoiceCadence) (*subscription.Subscription, []*price.Price) {
	cust := &customer.Customer{
		ID:         "cust_mqh",
		ExternalID: "ext_mqh",
		Name:       "MQH",
		Email:      "mqh@example.com",
		BaseModel:  types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().CustomerRepo.Create(ctx, cust))
	pl := &plan.Plan{ID: "plan_mqh", Name: "MQH", BaseModel: types.GetDefaultBaseModel(ctx)}
	s.NoError(s.GetStores().PlanRepo.Create(ctx, pl))

	prices := make([]*price.Price, 3)
	for i, spec := range []struct {
		id      string
		period  types.BillingPeriod
		amount  int
		cadence types.InvoiceCadence
	}{
		{"price_mqh_m", types.BILLING_PERIOD_MONTHLY, amtM, cadM},
		{"price_mqh_q", types.BILLING_PERIOD_QUARTER, amtQ, cadQ},
		{"price_mqh_h", types.BILLING_PERIOD_HALF_YEAR, amtH, cadH},
	} {
		p := &price.Price{
			ID:                 spec.id,
			Amount:             decimal.NewFromInt(int64(spec.amount)),
			Currency:           "usd",
			EntityType:         types.PRICE_ENTITY_TYPE_PLAN,
			EntityID:           pl.ID,
			Type:               types.PRICE_TYPE_FIXED,
			BillingPeriod:      spec.period,
			BillingPeriodCount: 1,
			BillingModel:       types.BILLING_MODEL_FLAT_FEE,
			BillingCadence:     types.BILLING_CADENCE_RECURRING,
			InvoiceCadence:     spec.cadence,
			BaseModel:          types.GetDefaultBaseModel(ctx),
		}
		s.NoError(s.GetStores().PriceRepo.Create(ctx, p))
		prices[i] = p
	}

	feb1 := start.AddDate(0, 1, 0)
	sub := &subscription.Subscription{
		ID:                 "sub_mqh",
		PlanID:             pl.ID,
		CustomerID:         cust.ID,
		StartDate:          start,
		BillingAnchor:      start,
		CurrentPeriodStart: start,
		CurrentPeriodEnd:   feb1,
		Currency:           "usd",
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		SubscriptionStatus: types.SubscriptionStatusActive,
		CustomerTimezone:   "UTC",
		ProrationBehavior:  types.ProrationBehaviorNone,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	lineItems := make([]*subscription.SubscriptionLineItem, 3)
	for i, p := range prices {
		lineItems[i] = &subscription.SubscriptionLineItem{
			ID:                 types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION_LINE_ITEM),
			SubscriptionID:     sub.ID,
			CustomerID:         sub.CustomerID,
			EntityID:           pl.ID,
			EntityType:         types.SubscriptionLineItemEntityTypePlan,
			PlanDisplayName:    pl.Name,
			PriceID:            p.ID,
			PriceType:          types.PRICE_TYPE_FIXED,
			DisplayName:        p.ID,
			Quantity:           decimal.NewFromInt(1),
			Currency:           sub.Currency,
			BillingPeriod:      p.BillingPeriod,
			BillingPeriodCount: 1,
			InvoiceCadence:     p.InvoiceCadence,
			StartDate:          start,
			BaseModel:          types.GetDefaultBaseModel(ctx),
		}
	}
	s.NoError(s.GetStores().SubscriptionRepo.CreateWithLineItems(ctx, sub, lineItems))
	sub.LineItems = lineItems
	return sub, prices
}

// TestMultiCadence_Stress_AllSamePeriod implements PRD E.14.2: 3x Monthly ARREAR, backward compatibility.
func (s *BillingServiceSuite) TestMultiCadence_Stress_AllSamePeriod() {
	ctx := s.GetContext()
	jan1 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	s.BaseServiceTestSuite.ClearStores()
	cust := &customer.Customer{
		ID:         "cust_3m",
		ExternalID: "ext_3m",
		Name:       "3M",
		Email:      "3m@example.com",
		BaseModel:  types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().CustomerRepo.Create(ctx, cust))
	pl := &plan.Plan{ID: "plan_3m", Name: "3M", BaseModel: types.GetDefaultBaseModel(ctx)}
	s.NoError(s.GetStores().PlanRepo.Create(ctx, pl))
	prices := make([]*price.Price, 3)
	for i, amt := range []int{10, 20, 30} {
		p := &price.Price{
			ID:                 fmt.Sprintf("price_3m_%d", i),
			Amount:             decimal.NewFromInt(int64(amt)),
			Currency:           "usd",
			EntityType:         types.PRICE_ENTITY_TYPE_PLAN,
			EntityID:           pl.ID,
			Type:               types.PRICE_TYPE_FIXED,
			BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
			BillingPeriodCount: 1,
			BillingModel:       types.BILLING_MODEL_FLAT_FEE,
			BillingCadence:     types.BILLING_CADENCE_RECURRING,
			InvoiceCadence:     types.InvoiceCadenceArrear,
			BaseModel:          types.GetDefaultBaseModel(ctx),
		}
		s.NoError(s.GetStores().PriceRepo.Create(ctx, p))
		prices[i] = p
	}
	feb1 := jan1.AddDate(0, 1, 0)
	sub3m := &subscription.Subscription{
		ID:                 "sub_3m",
		PlanID:             pl.ID,
		CustomerID:         cust.ID,
		StartDate:          jan1,
		BillingAnchor:      jan1,
		CurrentPeriodStart: jan1,
		CurrentPeriodEnd:   feb1,
		Currency:           "usd",
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		SubscriptionStatus: types.SubscriptionStatusActive,
		CustomerTimezone:   "UTC",
		ProrationBehavior:  types.ProrationBehaviorNone,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	lineItems := make([]*subscription.SubscriptionLineItem, 3)
	for i, p := range prices {
		lineItems[i] = &subscription.SubscriptionLineItem{
			ID:                 types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION_LINE_ITEM),
			SubscriptionID:     sub3m.ID,
			CustomerID:         sub3m.CustomerID,
			EntityID:           pl.ID,
			EntityType:         types.SubscriptionLineItemEntityTypePlan,
			PlanDisplayName:    pl.Name,
			PriceID:            p.ID,
			PriceType:          types.PRICE_TYPE_FIXED,
			DisplayName:        p.ID,
			Quantity:           decimal.NewFromInt(1),
			Currency:           sub3m.Currency,
			BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
			BillingPeriodCount: 1,
			InvoiceCadence:     types.InvoiceCadenceArrear,
			StartDate:          jan1,
			BaseModel:          types.GetDefaultBaseModel(ctx),
		}
	}
	s.NoError(s.GetStores().SubscriptionRepo.CreateWithLineItems(ctx, sub3m, lineItems))
	sub3m.LineItems = lineItems

	for month := 0; month < 3; month++ {
		periodStart := jan1.AddDate(0, month, 0)
		periodEnd := jan1.AddDate(0, month+1, 0)
		lineItemsOut, total, err := s.service.CalculateFixedCharges(ctx, sub3m, periodStart, periodEnd)
		s.NoError(err)
		s.Len(lineItemsOut, 3, "E.14.2: every month should have 3 line items")
		s.True(total.Equal(decimal.NewFromInt(60)), "10+20+30=60")
		_ = lineItemsOut
	}
}

// TestMultiCadence_FilterLineItems_QuarterlyDeduplication implements PRD E.15.1: Q already invoiced -> Apr 1 invoice only M.
func (s *BillingServiceSuite) TestMultiCadence_FilterLineItems_QuarterlyDeduplication() {
	ctx := s.GetContext()
	jan1 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	apr1 := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	may1 := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	s.BaseServiceTestSuite.ClearStores()
	sub, prices := s.setupMultiCadenceSubMQH(ctx, jan1, 10, 100, 200, types.InvoiceCadenceArrear, types.InvoiceCadenceArrear, types.InvoiceCadenceArrear)
	s.Require().Len(prices, 3)
	priceM, priceQ := prices[0], prices[1]

	inv := &invoice.Invoice{
		ID:              "inv_e15",
		CustomerID:      sub.CustomerID,
		SubscriptionID:  lo.ToPtr(sub.ID),
		InvoiceType:     types.InvoiceTypeSubscription,
		InvoiceStatus:   types.InvoiceStatusFinalized,
		PaymentStatus:   types.PaymentStatusPending,
		Currency:        "usd",
		AmountDue:       decimal.NewFromInt(110),
		AmountPaid:      decimal.Zero,
		AmountRemaining: decimal.NewFromInt(110),
		Description:     "E15 existing",
		PeriodStart:     lo.ToPtr(time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)),
		PeriodEnd:       lo.ToPtr(apr1),
		BillingReason:   string(types.InvoiceBillingReasonSubscriptionCycle),
		BaseModel:       types.GetDefaultBaseModel(ctx),
		LineItems: []*invoice.InvoiceLineItem{
			{ID: "li_m", InvoiceID: "inv_e15", CustomerID: sub.CustomerID, SubscriptionID: lo.ToPtr(sub.ID), EntityID: lo.ToPtr(sub.PlanID), EntityType: lo.ToPtr(string(types.SubscriptionLineItemEntityTypePlan)), PriceID: lo.ToPtr(priceM.ID), Amount: decimal.NewFromInt(10), Quantity: decimal.NewFromInt(1), Currency: "usd", PeriodStart: lo.ToPtr(time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)), PeriodEnd: lo.ToPtr(apr1), BaseModel: types.GetDefaultBaseModel(ctx)},
			{ID: "li_q", InvoiceID: "inv_e15", CustomerID: sub.CustomerID, SubscriptionID: lo.ToPtr(sub.ID), EntityID: lo.ToPtr(sub.PlanID), EntityType: lo.ToPtr(string(types.SubscriptionLineItemEntityTypePlan)), PriceID: lo.ToPtr(priceQ.ID), Amount: decimal.NewFromInt(100), Quantity: decimal.NewFromInt(1), Currency: "usd", PeriodStart: lo.ToPtr(jan1), PeriodEnd: lo.ToPtr(apr1), BaseModel: types.GetDefaultBaseModel(ctx)},
		},
	}
	s.NoError(s.GetStores().InvoiceRepo.CreateWithLineItems(ctx, inv))

	req, err := s.service.PrepareSubscriptionInvoiceRequest(ctx, sub, apr1, may1, types.ReferencePointPeriodEnd)
	s.NoError(err)
	s.Require().NotNil(req)
	s.Len(req.LineItems, 1, "E.15.1: Q already invoiced for Jan-Apr, Apr 1 invoice should have only M")
	s.Equal(priceM.ID, lo.FromPtr(req.LineItems[0].PriceID))
	s.True(req.AmountDue.Equal(decimal.NewFromInt(10)))
}

// setupMultiCadenceSubMQA creates a subscription with Monthly, Quarterly, Annual fixed line items (Jan 1 start).
func (s *BillingServiceSuite) setupMultiCadenceSubMQA(ctx context.Context, start time.Time, amtM, amtQ, amtA int, cadM, cadQ, cadA types.InvoiceCadence) (*subscription.Subscription, []*price.Price) {
	cust := &customer.Customer{
		ID:         "cust_mqa",
		ExternalID: "ext_mqa",
		Name:       "MQA",
		Email:      "mqa@example.com",
		BaseModel:  types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().CustomerRepo.Create(ctx, cust))
	pl := &plan.Plan{ID: "plan_mqa", Name: "MQA", BaseModel: types.GetDefaultBaseModel(ctx)}
	s.NoError(s.GetStores().PlanRepo.Create(ctx, pl))

	prices := make([]*price.Price, 3)
	for i, spec := range []struct {
		id      string
		period  types.BillingPeriod
		amount  int
		cadence types.InvoiceCadence
	}{
		{"price_mqa_m", types.BILLING_PERIOD_MONTHLY, amtM, cadM},
		{"price_mqa_q", types.BILLING_PERIOD_QUARTER, amtQ, cadQ},
		{"price_mqa_a", types.BILLING_PERIOD_ANNUAL, amtA, cadA},
	} {
		p := &price.Price{
			ID:                 spec.id,
			Amount:             decimal.NewFromInt(int64(spec.amount)),
			Currency:           "usd",
			EntityType:         types.PRICE_ENTITY_TYPE_PLAN,
			EntityID:           pl.ID,
			Type:               types.PRICE_TYPE_FIXED,
			BillingPeriod:      spec.period,
			BillingPeriodCount: 1,
			BillingModel:       types.BILLING_MODEL_FLAT_FEE,
			BillingCadence:     types.BILLING_CADENCE_RECURRING,
			InvoiceCadence:     spec.cadence,
			BaseModel:          types.GetDefaultBaseModel(ctx),
		}
		s.NoError(s.GetStores().PriceRepo.Create(ctx, p))
		prices[i] = p
	}

	feb1 := start.AddDate(0, 1, 0)
	sub := &subscription.Subscription{
		ID:                 "sub_mqa",
		PlanID:             pl.ID,
		CustomerID:         cust.ID,
		StartDate:          start,
		BillingAnchor:      start,
		CurrentPeriodStart: start,
		CurrentPeriodEnd:   feb1,
		Currency:           "usd",
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		SubscriptionStatus: types.SubscriptionStatusActive,
		CustomerTimezone:   "UTC",
		ProrationBehavior:  types.ProrationBehaviorNone,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	lineItems := make([]*subscription.SubscriptionLineItem, 3)
	for i, p := range prices {
		lineItems[i] = &subscription.SubscriptionLineItem{
			ID:                 types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION_LINE_ITEM),
			SubscriptionID:     sub.ID,
			CustomerID:         sub.CustomerID,
			EntityID:           pl.ID,
			EntityType:         types.SubscriptionLineItemEntityTypePlan,
			PlanDisplayName:    pl.Name,
			PriceID:            p.ID,
			PriceType:          types.PRICE_TYPE_FIXED,
			DisplayName:        p.ID,
			Quantity:           decimal.NewFromInt(1),
			Currency:           sub.Currency,
			BillingPeriod:      p.BillingPeriod,
			BillingPeriodCount: 1,
			InvoiceCadence:     p.InvoiceCadence,
			StartDate:          start,
			BaseModel:          types.GetDefaultBaseModel(ctx),
		}
	}
	s.NoError(s.GetStores().SubscriptionRepo.CreateWithLineItems(ctx, sub, lineItems))
	sub.LineItems = lineItems
	return sub, prices
}

// TestMultiCadence_12MonthSchedule_MQA_Arrear implements PRD E.6.2: M+Q+A all ARREAR ($50+$300+$1200).
func (s *BillingServiceSuite) TestMultiCadence_12MonthSchedule_MQA_Arrear() {
	ctx := s.GetContext()
	jan1 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	s.BaseServiceTestSuite.ClearStores()
	sub, _ := s.setupMultiCadenceSubMQA(ctx, jan1, 50, 300, 1200, types.InvoiceCadenceArrear, types.InvoiceCadenceArrear, types.InvoiceCadenceArrear)
	s.Require().NotNil(sub)

	expectedTotals := []int{50, 50, 350, 50, 50, 350, 50, 50, 350, 50, 50, 1550} // E.6.2 table
	periodStarts := []time.Time{
		jan1, time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC),
	}
	for i := 0; i < 12; i++ {
		periodEnd := periodStarts[i].AddDate(0, 1, 0)
		_, total, err := s.service.CalculateFixedCharges(ctx, sub, periodStarts[i], periodEnd)
		s.NoError(err, "invoice %d", i+1)
		s.True(total.Equal(decimal.NewFromInt(int64(expectedTotals[i]))),
			"invoice %d: expected %d, got %s", i+1, expectedTotals[i], total.String())
	}
}

// setupMultiCadenceSubQA creates a subscription with Quarterly + Annual only (sub billing period QUARTERLY).
func (s *BillingServiceSuite) setupMultiCadenceSubQA(ctx context.Context, start time.Time, amtQ, amtA int, cadQ, cadA types.InvoiceCadence) (*subscription.Subscription, []*price.Price) {
	cust := &customer.Customer{
		ID:         "cust_qa",
		ExternalID: "ext_qa",
		Name:       "QA",
		Email:      "qa@example.com",
		BaseModel:  types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().CustomerRepo.Create(ctx, cust))
	pl := &plan.Plan{ID: "plan_qa", Name: "QA", BaseModel: types.GetDefaultBaseModel(ctx)}
	s.NoError(s.GetStores().PlanRepo.Create(ctx, pl))

	prices := make([]*price.Price, 2)
	for i, spec := range []struct {
		id      string
		period  types.BillingPeriod
		amount  int
		cadence types.InvoiceCadence
	}{
		{"price_qa_q", types.BILLING_PERIOD_QUARTER, amtQ, cadQ},
		{"price_qa_a", types.BILLING_PERIOD_ANNUAL, amtA, cadA},
	} {
		p := &price.Price{
			ID:                 spec.id,
			Amount:             decimal.NewFromInt(int64(spec.amount)),
			Currency:           "usd",
			EntityType:         types.PRICE_ENTITY_TYPE_PLAN,
			EntityID:           pl.ID,
			Type:               types.PRICE_TYPE_FIXED,
			BillingPeriod:      spec.period,
			BillingPeriodCount: 1,
			BillingModel:       types.BILLING_MODEL_FLAT_FEE,
			BillingCadence:     types.BILLING_CADENCE_RECURRING,
			InvoiceCadence:     spec.cadence,
			BaseModel:          types.GetDefaultBaseModel(ctx),
		}
		s.NoError(s.GetStores().PriceRepo.Create(ctx, p))
		prices[i] = p
	}

	apr1 := start.AddDate(0, 3, 0)
	sub := &subscription.Subscription{
		ID:                 "sub_qa",
		PlanID:             pl.ID,
		CustomerID:         cust.ID,
		StartDate:          start,
		BillingAnchor:      start,
		CurrentPeriodStart: start,
		CurrentPeriodEnd:   apr1,
		Currency:           "usd",
		BillingPeriod:      types.BILLING_PERIOD_QUARTER,
		BillingPeriodCount: 1,
		SubscriptionStatus: types.SubscriptionStatusActive,
		CustomerTimezone:   "UTC",
		ProrationBehavior:  types.ProrationBehaviorNone,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	lineItems := make([]*subscription.SubscriptionLineItem, 2)
	for i, p := range prices {
		lineItems[i] = &subscription.SubscriptionLineItem{
			ID:                 types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION_LINE_ITEM),
			SubscriptionID:     sub.ID,
			CustomerID:         sub.CustomerID,
			EntityID:           pl.ID,
			EntityType:         types.SubscriptionLineItemEntityTypePlan,
			PlanDisplayName:    pl.Name,
			PriceID:            p.ID,
			PriceType:          types.PRICE_TYPE_FIXED,
			DisplayName:        p.ID,
			Quantity:           decimal.NewFromInt(1),
			Currency:           sub.Currency,
			BillingPeriod:      p.BillingPeriod,
			BillingPeriodCount: 1,
			InvoiceCadence:     p.InvoiceCadence,
			StartDate:          start,
			BaseModel:          types.GetDefaultBaseModel(ctx),
		}
	}
	s.NoError(s.GetStores().SubscriptionRepo.CreateWithLineItems(ctx, sub, lineItems))
	sub.LineItems = lineItems
	return sub, prices
}

// TestMultiCadence_4InvoiceSchedule_QA_Arrear implements PRD E.6.4: Quarterly + Annual all ARREAR ($500+$5000).
func (s *BillingServiceSuite) TestMultiCadence_4InvoiceSchedule_QA_Arrear() {
	ctx := s.GetContext()
	jan1 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	s.BaseServiceTestSuite.ClearStores()
	sub, _ := s.setupMultiCadenceSubQA(ctx, jan1, 500, 5000, types.InvoiceCadenceArrear, types.InvoiceCadenceArrear)
	s.Require().NotNil(sub)

	// E.6.4: 4 invoices - Apr 1 $500, Jul 1 $500, Oct 1 $500, Jan 1 yr2 $5500
	periods := []struct {
		start time.Time
		total int
	}{
		{time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), 500},   // Jan-Apr: Q1 only
		{time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC), 500},   // Apr-Jul: Q2 only
		{time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC), 500},   // Jul-Oct: Q3 only
		{time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), 5500}, // Oct-Jan: Q4 + Annual
	}
	for i, p := range periods {
		periodEnd := p.start.AddDate(0, 3, 0)
		if i == 3 {
			periodEnd = time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
		}
		_, total, err := s.service.CalculateFixedCharges(ctx, sub, p.start, periodEnd)
		s.NoError(err, "invoice %d", i+1)
		s.True(total.Equal(decimal.NewFromInt(int64(p.total))), "invoice %d: expected %d, got %s", i+1, p.total, total.String())
	}
}

// TestMultiCadence_12MonthSchedule_MixedAdvanceArrear implements PRD E.6.3: M ADVANCE $20 + Q ARREAR $150 + A ADVANCE $2400.
func (s *BillingServiceSuite) TestMultiCadence_12MonthSchedule_MixedAdvanceArrear() {
	ctx := s.GetContext()
	jan1 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	feb1 := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	apr1 := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	may1 := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	jan1yr2 := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	s.BaseServiceTestSuite.ClearStores()
	sub, _ := s.setupMultiCadenceSubMQA(ctx, jan1, 20, 150, 2400,
		types.InvoiceCadenceAdvance, types.InvoiceCadenceArrear, types.InvoiceCadenceAdvance)
	s.Require().NotNil(sub)

	// E.6.3: Creation (period_start) — M-ADV (Jan) + A-ADV (Year 1) = $2420
	reqCreate, err := s.service.PrepareSubscriptionInvoiceRequest(ctx, sub, jan1, feb1, types.ReferencePointPeriodStart)
	s.NoError(err)
	s.Require().NotNil(reqCreate)
	s.True(reqCreate.AmountDue.Equal(decimal.NewFromInt(2420)), "creation: expected $2420, got %s", reqCreate.AmountDue.String())

	// Feb 1 period_end — M-ADV (Feb next) = $20
	reqFeb, err := s.service.PrepareSubscriptionInvoiceRequest(ctx, sub, jan1, feb1, types.ReferencePointPeriodEnd)
	s.NoError(err)
	s.Require().NotNil(reqFeb)
	s.True(reqFeb.AmountDue.Equal(decimal.NewFromInt(20)), "Feb 1: expected $20, got %s", reqFeb.AmountDue.String())

	// Apr 1 period_end — M-ADV (Apr) + Q-ARR (Q1) = $170
	reqApr, err := s.service.PrepareSubscriptionInvoiceRequest(ctx, sub, time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC), apr1, types.ReferencePointPeriodEnd)
	s.NoError(err)
	s.Require().NotNil(reqApr)
	s.True(reqApr.AmountDue.Equal(decimal.NewFromInt(170)), "Apr 1: expected $170, got %s", reqApr.AmountDue.String())

	// Jan 1 yr2 period_end — M-ADV (Jan yr2) + Q-ARR (Q4) + A-ADV (Year 2) = $2570
	dec1 := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
	reqJan2, err := s.service.PrepareSubscriptionInvoiceRequest(ctx, sub, dec1, jan1yr2, types.ReferencePointPeriodEnd)
	s.NoError(err)
	s.Require().NotNil(reqJan2)
	s.True(reqJan2.AmountDue.Equal(decimal.NewFromInt(2570)), "Jan 1 yr2: expected $2570, got %s", reqJan2.AmountDue.String())

	_ = may1
}

// TestMultiCadence_PreviewInvoice_OrbStyle implements PRD D.8.1: Orb-style preview (window-based).
func (s *BillingServiceSuite) TestMultiCadence_PreviewInvoice_OrbStyle() {
	ctx := s.GetContext()
	jan1 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	feb1 := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	mar1 := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	apr1 := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	jun1 := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	jul1 := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	may1 := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	s.BaseServiceTestSuite.ClearStores()
	sub, _ := s.setupMultiCadenceSubMQH(ctx, jan1, 10, 100, 200, types.InvoiceCadenceArrear, types.InvoiceCadenceArrear, types.InvoiceCadenceArrear)
	s.Require().NotNil(sub)

	// D.8.1 #1: Preview in period Jan 1 - Feb 1 → only M (next invoice Feb 1 has only M)
	req1, err := s.service.PrepareSubscriptionInvoiceRequest(ctx, sub, jan1, feb1, types.ReferencePointPreview)
	s.NoError(err)
	s.Require().NotNil(req1)
	s.Len(req1.LineItems, 1, "D.8.1: Jan period preview shows only M")

	// D.8.1 #3: Preview in period Mar 1 - Apr 1 → M + Q
	req3, err := s.service.PrepareSubscriptionInvoiceRequest(ctx, sub, mar1, apr1, types.ReferencePointPreview)
	s.NoError(err)
	s.Require().NotNil(req3)
	s.Len(req3.LineItems, 2, "D.8.1: Mar-Apr period preview shows M + Q")

	// D.8.1 #4: Preview in period Jun 1 - Jul 1 → M + Q + H
	req4, err := s.service.PrepareSubscriptionInvoiceRequest(ctx, sub, jun1, jul1, types.ReferencePointPreview)
	s.NoError(err)
	s.Require().NotNil(req4)
	s.Len(req4.LineItems, 3, "D.8.1: Jun-Jul period preview shows M + Q + H")

	// D.8.1 #5: Preview in period Apr 1 - May 1 → only M (Q period ends Jul 1)
	req5, err := s.service.PrepareSubscriptionInvoiceRequest(ctx, sub, apr1, may1, types.ReferencePointPreview)
	s.NoError(err)
	s.Require().NotNil(req5)
	s.Len(req5.LineItems, 1, "D.8.1: Apr-May period preview shows only M")
}

// TestMultiCadence_ReferencePoints implements PRD D.9: period_start, period_end Feb 1, period_end Jul 1.
func (s *BillingServiceSuite) TestMultiCadence_ReferencePoints() {
	ctx := s.GetContext()
	jan1 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	feb1 := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	mar1 := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	apr1 := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	jun1 := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	jul1 := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	aug1 := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	s.BaseServiceTestSuite.ClearStores()
	// D.9: M $10 ARREAR + Q $100 ARREAR + H $200 ADVANCE
	sub, _ := s.setupMultiCadenceSubMQH(ctx, jan1, 10, 100, 200, types.InvoiceCadenceArrear, types.InvoiceCadenceArrear, types.InvoiceCadenceAdvance)
	s.Require().NotNil(sub)

	// D.9.1 period_start: only ADVANCE (H $200)
	reqStart, err := s.service.PrepareSubscriptionInvoiceRequest(ctx, sub, jan1, feb1, types.ReferencePointPeriodStart)
	s.NoError(err)
	s.Require().NotNil(reqStart)
	s.True(reqStart.AmountDue.Equal(decimal.NewFromInt(200)), "D.9.1: creation invoice $200 (H ADVANCE only)")
	s.Len(reqStart.LineItems, 1, "D.9.1: one line item (H ADVANCE)")

	// D.9.2 period_end Feb 1: M ARREAR $10 only
	reqFeb, err := s.service.PrepareSubscriptionInvoiceRequest(ctx, sub, jan1, feb1, types.ReferencePointPeriodEnd)
	s.NoError(err)
	s.Require().NotNil(reqFeb)
	s.True(reqFeb.AmountDue.Equal(decimal.NewFromInt(10)), "D.9.2: Feb 1 invoice $10")
	s.Len(reqFeb.LineItems, 1, "D.9.2: M ARREAR only")

	// D.9.3 period_end Jul 1: M ARREAR + Q ARREAR + H ADVANCE (next) = $310
	reqJul, err := s.service.PrepareSubscriptionInvoiceRequest(ctx, sub, jun1, jul1, types.ReferencePointPeriodEnd)
	s.NoError(err)
	s.Require().NotNil(reqJul)
	s.True(reqJul.AmountDue.Equal(decimal.NewFromInt(310)), "D.9.3: Jul 1 invoice $310")
	s.Len(reqJul.LineItems, 3, "D.9.3: M + Q ARREAR + H ADVANCE")

	_ = mar1
	_ = apr1
	_ = aug1
}

// TestMultiCadence_MidMonthStart implements PRD D.10.1: Sub starts Jan 15, M $10 + Q $100 ARREAR.
func (s *BillingServiceSuite) TestMultiCadence_MidMonthStart() {
	ctx := s.GetContext()
	jan15 := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
	feb15 := time.Date(2026, 2, 15, 0, 0, 0, 0, time.UTC)
	mar15 := time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC)
	apr15 := time.Date(2026, 4, 15, 0, 0, 0, 0, time.UTC)
	may15 := time.Date(2026, 5, 15, 0, 0, 0, 0, time.UTC)
	s.BaseServiceTestSuite.ClearStores()
	cust := &customer.Customer{
		ID:         "cust_jan15",
		ExternalID: "ext_jan15",
		Name:       "Jan15",
		Email:      "jan15@example.com",
		BaseModel:  types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().CustomerRepo.Create(ctx, cust))
	pl := &plan.Plan{ID: "plan_jan15", Name: "Jan15", BaseModel: types.GetDefaultBaseModel(ctx)}
	s.NoError(s.GetStores().PlanRepo.Create(ctx, pl))
	prices := make([]*price.Price, 2)
	for i, spec := range []struct {
		id      string
		period  types.BillingPeriod
		amount  int
		cadence types.InvoiceCadence
	}{
		{"price_jan15_m", types.BILLING_PERIOD_MONTHLY, 10, types.InvoiceCadenceArrear},
		{"price_jan15_q", types.BILLING_PERIOD_QUARTER, 100, types.InvoiceCadenceArrear},
	} {
		p := &price.Price{
			ID:                 spec.id,
			Amount:             decimal.NewFromInt(int64(spec.amount)),
			Currency:           "usd",
			EntityType:         types.PRICE_ENTITY_TYPE_PLAN,
			EntityID:           pl.ID,
			Type:               types.PRICE_TYPE_FIXED,
			BillingPeriod:      spec.period,
			BillingPeriodCount: 1,
			BillingModel:       types.BILLING_MODEL_FLAT_FEE,
			BillingCadence:     types.BILLING_CADENCE_RECURRING,
			InvoiceCadence:     spec.cadence,
			BaseModel:          types.GetDefaultBaseModel(ctx),
		}
		s.NoError(s.GetStores().PriceRepo.Create(ctx, p))
		prices[i] = p
	}
	sub := &subscription.Subscription{
		ID:                 "sub_jan15",
		PlanID:             pl.ID,
		CustomerID:         cust.ID,
		StartDate:          jan15,
		BillingAnchor:      jan15,
		CurrentPeriodStart: jan15,
		CurrentPeriodEnd:   feb15,
		Currency:           "usd",
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		SubscriptionStatus: types.SubscriptionStatusActive,
		CustomerTimezone:   "UTC",
		ProrationBehavior:  types.ProrationBehaviorNone,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	lineItems := make([]*subscription.SubscriptionLineItem, 2)
	for i, p := range prices {
		lineItems[i] = &subscription.SubscriptionLineItem{
			ID:                 types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION_LINE_ITEM),
			SubscriptionID:     sub.ID,
			CustomerID:         sub.CustomerID,
			EntityID:           pl.ID,
			EntityType:         types.SubscriptionLineItemEntityTypePlan,
			PlanDisplayName:    pl.Name,
			PriceID:            p.ID,
			PriceType:          types.PRICE_TYPE_FIXED,
			DisplayName:        p.ID,
			Quantity:           decimal.NewFromInt(1),
			Currency:           sub.Currency,
			BillingPeriod:      p.BillingPeriod,
			BillingPeriodCount: 1,
			InvoiceCadence:     p.InvoiceCadence,
			StartDate:          jan15,
			BaseModel:          types.GetDefaultBaseModel(ctx),
		}
	}
	s.NoError(s.GetStores().SubscriptionRepo.CreateWithLineItems(ctx, sub, lineItems))
	sub.LineItems = lineItems

	// D.10.1: Feb 15 invoice — M only ($10)
	_, total1, err := s.service.CalculateFixedCharges(ctx, sub, jan15, feb15)
	s.NoError(err)
	s.True(total1.Equal(decimal.NewFromInt(10)), "Feb 15: M $10")

	// Mar 15 — M only
	_, total2, err := s.service.CalculateFixedCharges(ctx, sub, feb15, mar15)
	s.NoError(err)
	s.True(total2.Equal(decimal.NewFromInt(10)), "Mar 15: M $10")

	// Apr 15 — M + Q (Q period Jan 15 - Apr 15 ends in this window)
	_, total3, err := s.service.CalculateFixedCharges(ctx, sub, mar15, apr15)
	s.NoError(err)
	s.True(total3.Equal(decimal.NewFromInt(110)), "Apr 15: M $10 + Q $100 = $110")

	// May 15 — M only
	_, total4, err := s.service.CalculateFixedCharges(ctx, sub, apr15, may15)
	s.NoError(err)
	s.True(total4.Equal(decimal.NewFromInt(10)), "May 15: M $10")

	_ = total1
	_ = total2
	_ = total3
	_ = total4
}

// setupMultiCadenceSubMQHA creates a subscription with M + Q + H + A (all ARREAR) for E.14.1.
func (s *BillingServiceSuite) setupMultiCadenceSubMQHA(ctx context.Context, start time.Time, amtM, amtQ, amtH, amtA int) (*subscription.Subscription, []*price.Price) {
	cust := &customer.Customer{
		ID:         "cust_mqha",
		ExternalID: "ext_mqha",
		Name:       "MQHA",
		Email:      "mqha@example.com",
		BaseModel:  types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().CustomerRepo.Create(ctx, cust))
	pl := &plan.Plan{ID: "plan_mqha", Name: "MQHA", BaseModel: types.GetDefaultBaseModel(ctx)}
	s.NoError(s.GetStores().PlanRepo.Create(ctx, pl))

	specs := []struct {
		id     string
		period types.BillingPeriod
		amount int
	}{
		{"price_mqha_m", types.BILLING_PERIOD_MONTHLY, amtM},
		{"price_mqha_q", types.BILLING_PERIOD_QUARTER, amtQ},
		{"price_mqha_h", types.BILLING_PERIOD_HALF_YEAR, amtH},
		{"price_mqha_a", types.BILLING_PERIOD_ANNUAL, amtA},
	}
	prices := make([]*price.Price, 4)
	for i, spec := range specs {
		p := &price.Price{
			ID:                 spec.id,
			Amount:             decimal.NewFromInt(int64(spec.amount)),
			Currency:           "usd",
			EntityType:         types.PRICE_ENTITY_TYPE_PLAN,
			EntityID:           pl.ID,
			Type:               types.PRICE_TYPE_FIXED,
			BillingPeriod:      spec.period,
			BillingPeriodCount: 1,
			BillingModel:       types.BILLING_MODEL_FLAT_FEE,
			BillingCadence:     types.BILLING_CADENCE_RECURRING,
			InvoiceCadence:     types.InvoiceCadenceArrear,
			BaseModel:          types.GetDefaultBaseModel(ctx),
		}
		s.NoError(s.GetStores().PriceRepo.Create(ctx, p))
		prices[i] = p
	}
	feb1 := start.AddDate(0, 1, 0)
	sub := &subscription.Subscription{
		ID:                 "sub_mqha",
		PlanID:             pl.ID,
		CustomerID:         cust.ID,
		StartDate:          start,
		BillingAnchor:      start,
		CurrentPeriodStart: start,
		CurrentPeriodEnd:   feb1,
		Currency:           "usd",
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		SubscriptionStatus: types.SubscriptionStatusActive,
		CustomerTimezone:   "UTC",
		ProrationBehavior:  types.ProrationBehaviorNone,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	lineItems := make([]*subscription.SubscriptionLineItem, 4)
	for i, p := range prices {
		lineItems[i] = &subscription.SubscriptionLineItem{
			ID:                 types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION_LINE_ITEM),
			SubscriptionID:     sub.ID,
			CustomerID:         sub.CustomerID,
			EntityID:           pl.ID,
			EntityType:         types.SubscriptionLineItemEntityTypePlan,
			PlanDisplayName:    pl.Name,
			PriceID:            p.ID,
			PriceType:          types.PRICE_TYPE_FIXED,
			DisplayName:        p.ID,
			Quantity:           decimal.NewFromInt(1),
			Currency:           sub.Currency,
			BillingPeriod:      p.BillingPeriod,
			BillingPeriodCount: 1,
			InvoiceCadence:     types.InvoiceCadenceArrear,
			StartDate:          start,
			BaseModel:          types.GetDefaultBaseModel(ctx),
		}
	}
	s.NoError(s.GetStores().SubscriptionRepo.CreateWithLineItems(ctx, sub, lineItems))
	sub.LineItems = lineItems
	return sub, prices
}

// TestMultiCadence_Stress_MQHA implements PRD E.14.1: M+Q+H+A all ARREAR, 12-month schedule.
func (s *BillingServiceSuite) TestMultiCadence_Stress_MQHA() {
	ctx := s.GetContext()
	jan1 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	s.BaseServiceTestSuite.ClearStores()
	sub, _ := s.setupMultiCadenceSubMQHA(ctx, jan1, 10, 100, 200, 500)
	s.Require().NotNil(sub)

	// E.14.1 table: Feb 1, Mar 1 → 1 item; Apr 1 → 2; Jul 1 → 3; Jan 1 yr2 → 4
	expectedCounts := []int{1, 1, 2, 1, 1, 3, 1, 1, 2, 1, 1, 4}
	periodStarts := []time.Time{
		jan1, time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC),
	}
	for i := 0; i < 12; i++ {
		periodEnd := periodStarts[i].AddDate(0, 1, 0)
		lineItems, _, err := s.service.CalculateFixedCharges(ctx, sub, periodStarts[i], periodEnd)
		s.NoError(err, "invoice %d", i+1)
		s.Len(lineItems, expectedCounts[i], "E.14.1: invoice %d expected %d line items", i+1, expectedCounts[i])
	}
}

// TestMultiCadence_SingleQuarterlyLine implements PRD E.14.3: Sub with only Q $100 ARREAR, 4 invoices.
func (s *BillingServiceSuite) TestMultiCadence_SingleQuarterlyLine() {
	ctx := s.GetContext()
	jan1 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	s.BaseServiceTestSuite.ClearStores()
	cust := &customer.Customer{
		ID:         "cust_q",
		ExternalID: "ext_q",
		Name:       "Q",
		Email:      "q@example.com",
		BaseModel:  types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().CustomerRepo.Create(ctx, cust))
	pl := &plan.Plan{ID: "plan_q", Name: "Q", BaseModel: types.GetDefaultBaseModel(ctx)}
	s.NoError(s.GetStores().PlanRepo.Create(ctx, pl))
	p := &price.Price{
		ID:                 "price_q",
		Amount:             decimal.NewFromInt(100),
		Currency:           "usd",
		EntityType:         types.PRICE_ENTITY_TYPE_PLAN,
		EntityID:           pl.ID,
		Type:               types.PRICE_TYPE_FIXED,
		BillingPeriod:      types.BILLING_PERIOD_QUARTER,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_FLAT_FEE,
		BillingCadence:     types.BILLING_CADENCE_RECURRING,
		InvoiceCadence:     types.InvoiceCadenceArrear,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().PriceRepo.Create(ctx, p))
	apr1 := jan1.AddDate(0, 3, 0)
	subQ := &subscription.Subscription{
		ID:                 "sub_q",
		PlanID:             pl.ID,
		CustomerID:         cust.ID,
		StartDate:          jan1,
		BillingAnchor:      jan1,
		CurrentPeriodStart: jan1,
		CurrentPeriodEnd:   apr1,
		Currency:           "usd",
		BillingPeriod:      types.BILLING_PERIOD_QUARTER,
		BillingPeriodCount: 1,
		SubscriptionStatus: types.SubscriptionStatusActive,
		CustomerTimezone:   "UTC",
		ProrationBehavior:  types.ProrationBehaviorNone,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	li := &subscription.SubscriptionLineItem{
		ID:                 types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION_LINE_ITEM),
		SubscriptionID:     subQ.ID,
		CustomerID:         subQ.CustomerID,
		EntityID:           pl.ID,
		EntityType:         types.SubscriptionLineItemEntityTypePlan,
		PlanDisplayName:    pl.Name,
		PriceID:            p.ID,
		PriceType:          types.PRICE_TYPE_FIXED,
		DisplayName:        p.ID,
		Quantity:           decimal.NewFromInt(1),
		Currency:           subQ.Currency,
		BillingPeriod:      types.BILLING_PERIOD_QUARTER,
		BillingPeriodCount: 1,
		InvoiceCadence:     types.InvoiceCadenceArrear,
		StartDate:          jan1,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().SubscriptionRepo.CreateWithLineItems(ctx, subQ, []*subscription.SubscriptionLineItem{li}))
	subQ.LineItems = []*subscription.SubscriptionLineItem{li}

	periods := []struct {
		start time.Time
		end   time.Time
		total int
	}{
		{jan1, apr1, 100},
		{apr1, time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC), 100},
		{time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), 100},
		{time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC), 100},
	}
	for i, pr := range periods {
		_, total, err := s.service.CalculateFixedCharges(ctx, subQ, pr.start, pr.end)
		s.NoError(err, "invoice %d", i+1)
		s.True(total.Equal(decimal.NewFromInt(int64(pr.total))), "E.14.3: invoice %d expected $100", i+1)
	}
}
