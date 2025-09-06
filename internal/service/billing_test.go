package service

import (
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
		Logger:                s.GetLogger(),
		Config:                s.GetConfig(),
		DB:                    s.GetDB(),
		SubRepo:               s.GetStores().SubscriptionRepo,
		PlanRepo:              s.GetStores().PlanRepo,
		PriceRepo:             s.GetStores().PriceRepo,
		EventRepo:             s.GetStores().EventRepo,
		MeterRepo:             s.GetStores().MeterRepo,
		CustomerRepo:          s.GetStores().CustomerRepo,
		InvoiceRepo:           s.GetStores().InvoiceRepo,
		EntitlementRepo:       s.GetStores().EntitlementRepo,
		EnvironmentRepo:       s.GetStores().EnvironmentRepo,
		FeatureRepo:           s.GetStores().FeatureRepo,
		TenantRepo:            s.GetStores().TenantRepo,
		UserRepo:              s.GetStores().UserRepo,
		AuthRepo:              s.GetStores().AuthRepo,
		WalletRepo:            s.GetStores().WalletRepo,
		PaymentRepo:           s.GetStores().PaymentRepo,
		CouponAssociationRepo: s.GetStores().CouponAssociationRepo,
		CouponRepo:            s.GetStores().CouponRepo,
		CouponApplicationRepo: s.GetStores().CouponApplicationRepo,
		TaxRateRepo:           s.GetStores().TaxRateRepo,
		TaxAssociationRepo:    s.GetStores().TaxAssociationRepo,
		TaxAppliedRepo:        s.GetStores().TaxAppliedRepo,
		SettingsRepo:          s.GetStores().SettingsRepo,
		EventPublisher:        s.GetPublisher(),
		WebhookPublisher:      s.GetWebhookPublisher(),
		ProrationCalculator:   s.GetCalculator(),
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
	s.testData.subscription = &subscription.Subscription{
		ID:                 "sub_123",
		PlanID:             s.testData.plan.ID,
		CustomerID:         s.testData.customer.ID,
		StartDate:          s.testData.now.Add(-30 * 24 * time.Hour),
		CurrentPeriodStart: s.testData.now.Add(-48 * time.Hour),
		CurrentPeriodEnd:   s.testData.now.Add(6 * 24 * time.Hour),
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
			expectedAmount:      decimal.NewFromInt(25),
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
			s.True(tt.expectedAmount.Equal(req.AmountDue), "Amount due mismatch")
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
			if item.PriceType == types.PRICE_TYPE_USAGE {
				usageArrearItem = item
			} else if item.PriceType == types.PRICE_TYPE_FIXED {
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
		Logger:              s.GetLogger(),
		Config:              s.GetConfig(),
		DB:                  s.GetDB(),
		SubRepo:             s.GetStores().SubscriptionRepo,
		PlanRepo:            s.GetStores().PlanRepo,
		PriceRepo:           s.GetStores().PriceRepo,
		EventRepo:           s.GetStores().EventRepo,
		MeterRepo:           s.GetStores().MeterRepo,
		CustomerRepo:        s.GetStores().CustomerRepo,
		InvoiceRepo:         s.GetStores().InvoiceRepo,
		EntitlementRepo:     s.GetStores().EntitlementRepo,
		EnvironmentRepo:     s.GetStores().EnvironmentRepo,
		FeatureRepo:         s.GetStores().FeatureRepo,
		TenantRepo:          s.GetStores().TenantRepo,
		UserRepo:            s.GetStores().UserRepo,
		AuthRepo:            s.GetStores().AuthRepo,
		WalletRepo:          s.GetStores().WalletRepo,
		PaymentRepo:         s.GetStores().PaymentRepo,
		EventPublisher:      s.GetPublisher(),
		ProrationCalculator: s.GetCalculator(),
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
			bucketValues:     []decimal.Decimal{decimal.NewFromInt(9), decimal.NewFromInt(15)}, // Bucket 1: max(2,5,6,9)=9, Bucket 2: max(10,15)=15
			expectedAmount:   decimal.NewFromFloat(1.65),                                       // Bucket 1: 9*0.10=$0.90, Bucket 2: 10*0.10+5*0.05=$1.25, Total=$1.65
			expectedQuantity: decimal.NewFromInt(24),                                           // 9 + 15 = 24
			description:      "Tiered slab: Bucket1[2,5,6,9]→max=9→9*$0.10=$0.90, Bucket2[10,15]→max=15→10*$0.10+5*$0.05=$1.25, Total=$1.65",
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

			// Update subscription with new line item
			s.testData.subscription.LineItems = append(s.testData.subscription.LineItems, lineItem)
			s.NoError(s.GetStores().SubscriptionRepo.Update(ctx, s.testData.subscription))

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
				s.testData.subscription,
				usage,
				s.testData.subscription.CurrentPeriodStart,
				s.testData.subscription.CurrentPeriodEnd,
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

			result, err := s.service.(*billingService).calculateNeverResetUsage(
				ctx,
				testSubscription,
				lineItem,
				s.testData.customer,
				eventService,
				tt.periodStart,
				tt.periodEnd,
				tt.usageAllowed,
			)

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
