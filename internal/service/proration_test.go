package service

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/proration"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/suite"
)

type BaseProrationData struct {
	service  proration.Service
	testData struct {
		subscription *subscription.Subscription
		prices       struct {
			standard *price.Price
			premium  *price.Price
		}
		lineItems struct {
			standard *subscription.SubscriptionLineItem
			premium  *subscription.SubscriptionLineItem
		}
		now time.Time
	}
}

type ProrationServiceSuite struct {
	testutil.BaseServiceTestSuite
	BaseProrationData
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
	s.service = NewProrationService(ServiceParams{
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
		WebhookPublisher:    s.GetWebhookPublisher(),
		ProrationCalculator: s.GetCalculator(),
	})
}

func (s *ProrationServiceSuite) setupTestData() {
	s.testData.now = time.Now().UTC()

	// Create test prices
	s.testData.prices.standard = &price.Price{
		ID:                 "price_standard",
		Amount:             decimal.NewFromInt(10),
		Currency:           "USD",
		Type:               types.PRICE_TYPE_FIXED,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_FLAT_FEE,
		BillingCadence:     types.BILLING_CADENCE_RECURRING,
		InvoiceCadence:     types.InvoiceCadenceAdvance,
		BaseModel:          types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().PriceRepo.Create(s.GetContext(), s.testData.prices.standard))

	s.testData.prices.premium = &price.Price{
		ID:                 "price_premium",
		Amount:             decimal.NewFromInt(20),
		Currency:           "USD",
		Type:               types.PRICE_TYPE_FIXED,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_FLAT_FEE,
		BillingCadence:     types.BILLING_CADENCE_RECURRING,
		InvoiceCadence:     types.InvoiceCadenceAdvance,
		BaseModel:          types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().PriceRepo.Create(s.GetContext(), s.testData.prices.premium))

	// Create test subscription
	s.testData.subscription = &subscription.Subscription{
		ID:                 "sub_123",
		StartDate:          s.testData.now.Add(-30 * 24 * time.Hour),
		CurrentPeriodStart: s.testData.now.Add(-24 * time.Hour),
		CurrentPeriodEnd:   s.testData.now.Add(6 * 24 * time.Hour),
		Currency:           "USD",
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		SubscriptionStatus: types.SubscriptionStatusActive,
		CustomerTimezone:   "UTC",
		BaseModel:          types.GetDefaultBaseModel(s.GetContext()),
		BillingAnchor:      types.CalculateCalendarBillingAnchor(s.testData.now.Add(-30*24*time.Hour), types.BILLING_PERIOD_MONTHLY),
	}

	// Create line items
	s.testData.lineItems.standard = &subscription.SubscriptionLineItem{
		ID:             "li_standard",
		SubscriptionID: s.testData.subscription.ID,
		PriceID:        s.testData.prices.standard.ID,
		Quantity:       decimal.NewFromInt(1),
		Currency:       "USD",
		BillingPeriod:  types.BILLING_PERIOD_MONTHLY,
		BaseModel:      types.GetDefaultBaseModel(s.GetContext()),
	}

	s.testData.lineItems.premium = &subscription.SubscriptionLineItem{
		ID:             "li_premium",
		SubscriptionID: s.testData.subscription.ID,
		PriceID:        s.testData.prices.premium.ID,
		Quantity:       decimal.NewFromInt(1),
		Currency:       "USD",
		BillingPeriod:  types.BILLING_PERIOD_MONTHLY,
		BaseModel:      types.GetDefaultBaseModel(s.GetContext()),
	}

	s.testData.subscription.LineItems = []*subscription.SubscriptionLineItem{
		s.testData.lineItems.standard,
	}

	s.NoError(s.GetStores().SubscriptionRepo.CreateWithLineItems(s.GetContext(), s.testData.subscription, s.testData.subscription.LineItems))

	// Create an invoice for the current period
	nextNumber, err := s.GetStores().InvoiceRepo.GetNextInvoiceNumber(s.GetContext())
	s.NoError(err)

	nextSeq, err := s.GetStores().InvoiceRepo.GetNextBillingSequence(s.GetContext(), s.testData.subscription.ID)
	s.NoError(err)

	inv := &invoice.Invoice{
		SubscriptionID:  &s.testData.subscription.ID,
		InvoiceType:     types.InvoiceTypeSubscription,
		InvoiceStatus:   types.InvoiceStatusDraft,
		PaymentStatus:   types.PaymentStatusPending,
		Currency:        s.testData.subscription.Currency,
		InvoiceNumber:   &nextNumber,
		BillingSequence: &nextSeq,
		Description:     "Test Invoice",
		BillingReason:   string(types.InvoiceBillingReasonSubscriptionCreate),
		PeriodStart:     &s.testData.subscription.CurrentPeriodStart,
		PeriodEnd:       &s.testData.subscription.CurrentPeriodEnd,
		EnvironmentID:   s.testData.subscription.EnvironmentID,
		BaseModel: types.BaseModel{
			TenantID: s.testData.subscription.TenantID,
			Status:   types.StatusPublished,
		},
	}

	s.NoError(s.GetStores().InvoiceRepo.Create(s.GetContext(), inv))
}

func (s *ProrationServiceSuite) TestCalculateProration() {
	tests := []struct {
		name    string
		params  proration.ProrationParams
		want    *proration.ProrationResult
		wantErr bool
	}{
		{
			name: "upgrade_standard_to_premium",
			params: proration.ProrationParams{
				Action:             types.ProrationActionUpgrade,
				OldPriceID:         "price_old",
				NewPriceID:         "price_new",
				OldQuantity:        decimal.NewFromInt(1),
				NewQuantity:        decimal.NewFromInt(1),
				OldPricePerUnit:    decimal.NewFromInt(10),
				NewPricePerUnit:    decimal.NewFromInt(20),
				ProrationDate:      time.Date(2024, 3, 15, 0, 0, 0, 0, time.UTC),
				CurrentPeriodStart: time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC),
				CurrentPeriodEnd:   time.Date(2024, 3, 31, 0, 0, 0, 0, time.UTC),
				CustomerTimezone:   "UTC",
				ProrationBehavior:  types.ProrationBehaviorCreateProrations,
				ProrationStrategy:  types.StrategyDayBased,
				PlanPayInAdvance:   true,
				Currency:           "USD",
			},
			want: &proration.ProrationResult{
				NetAmount:     decimal.NewFromFloat(5.48), // Credit: -(10 * 17/31) = -5.48, Charge: (10 * 17/31) = 5.48, Net: 5.48
				Action:        types.ProrationActionUpgrade,
				ProrationDate: time.Date(2024, 3, 15, 0, 0, 0, 0, time.UTC),
				Currency:      "USD",
				IsPreview:     false,
			},
			wantErr: false,
		},
		{
			name: "mixed_billing_upgrade_base_plus_usage",
			params: proration.ProrationParams{
				Action:             types.ProrationActionUpgrade,
				OldPriceID:         "price_base_10_plus_usage",
				NewPriceID:         "price_base_20_plus_usage",
				OldQuantity:        decimal.NewFromInt(1),
				NewQuantity:        decimal.NewFromInt(1),
				OldPricePerUnit:    decimal.NewFromInt(10),
				NewPricePerUnit:    decimal.NewFromInt(20),
				ProrationDate:      time.Date(2024, 3, 15, 0, 0, 0, 0, time.UTC),
				CurrentPeriodStart: time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC),
				CurrentPeriodEnd:   time.Date(2024, 3, 31, 0, 0, 0, 0, time.UTC),
				CustomerTimezone:   "UTC",
				ProrationBehavior:  types.ProrationBehaviorCreateProrations,
				ProrationStrategy:  types.StrategyDayBased,
				PlanPayInAdvance:   true,
				Currency:           "USD",
			},
			want: &proration.ProrationResult{
				NetAmount:     decimal.NewFromFloat(5.48), // Credit: -(10 * 17/31) = -5.48, Charge: (10 * 17/31) = 5.48, Net: 5.48
				Action:        types.ProrationActionUpgrade,
				ProrationDate: time.Date(2024, 3, 15, 0, 0, 0, 0, time.UTC),
				Currency:      "USD",
				IsPreview:     false,
			},
			wantErr: false,
		},
		{
			name: "quantity_change_with_usage_tracking",
			params: proration.ProrationParams{
				Action:             types.ProrationActionQuantityChange,
				OldPriceID:         "price_per_seat",
				NewPriceID:         "price_per_seat",
				OldQuantity:        decimal.NewFromInt(5),
				NewQuantity:        decimal.NewFromInt(10),
				OldPricePerUnit:    decimal.NewFromInt(10),
				NewPricePerUnit:    decimal.NewFromInt(10),
				ProrationDate:      time.Date(2024, 3, 15, 0, 0, 0, 0, time.UTC),
				CurrentPeriodStart: time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC),
				CurrentPeriodEnd:   time.Date(2024, 3, 31, 0, 0, 0, 0, time.UTC),
				CustomerTimezone:   "UTC",
				ProrationBehavior:  types.ProrationBehaviorCreateProrations,
				ProrationStrategy:  types.StrategyDayBased,
				PlanPayInAdvance:   true,
				Currency:           "USD",
			},
			want: &proration.ProrationResult{
				NetAmount:     decimal.NewFromFloat(27.42), // Credit: -(10 * 5 * 17/31) = -27.42, Charge: (10 * 5 * 17/31) = 27.42, Net: 27.42
				Action:        types.ProrationActionQuantityChange,
				ProrationDate: time.Date(2024, 3, 15, 0, 0, 0, 0, time.UTC),
				Currency:      "USD",
				IsPreview:     false,
			},
			wantErr: false,
		},
		{
			name: "downgrade_with_minimum_commitment",
			params: proration.ProrationParams{
				Action:             types.ProrationActionDowngrade,
				OldPriceID:         "price_enterprise",
				NewPriceID:         "price_team",
				OldQuantity:        decimal.NewFromInt(1),
				NewQuantity:        decimal.NewFromInt(1),
				OldPricePerUnit:    decimal.NewFromInt(1000),
				NewPricePerUnit:    decimal.NewFromInt(500),
				ProrationDate:      time.Date(2024, 3, 15, 0, 0, 0, 0, time.UTC),
				CurrentPeriodStart: time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC),
				CurrentPeriodEnd:   time.Date(2024, 3, 31, 0, 0, 0, 0, time.UTC),
				CustomerTimezone:   "UTC",
				ProrationBehavior:  types.ProrationBehaviorCreateProrations,
				ProrationStrategy:  types.StrategyDayBased,
				PlanPayInAdvance:   true,
				OriginalAmountPaid: decimal.NewFromInt(1000),
				Currency:           "USD",
			},
			want: &proration.ProrationResult{
				NetAmount:     decimal.NewFromFloat(-274.19), // Credit: -(1000 * 17/31) = -548.39, Charge: (-500 * 17/31) = -274.19, Net: -274.19
				Action:        types.ProrationActionDowngrade,
				ProrationDate: time.Date(2024, 3, 15, 0, 0, 0, 0, time.UTC),
				Currency:      "USD",
				IsPreview:     false,
			},
			wantErr: false,
		},
		{
			name: "add_usage_based_item",
			params: proration.ProrationParams{
				Action:             types.ProrationActionAddItem,
				NewPriceID:         "price_api_calls",
				NewQuantity:        decimal.NewFromInt(1),
				NewPricePerUnit:    decimal.NewFromInt(0),
				ProrationDate:      time.Date(2024, 3, 15, 0, 0, 0, 0, time.UTC),
				CurrentPeriodStart: time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC),
				CurrentPeriodEnd:   time.Date(2024, 3, 31, 0, 0, 0, 0, time.UTC),
				CustomerTimezone:   "UTC",
				ProrationBehavior:  types.ProrationBehaviorCreateProrations,
				ProrationStrategy:  types.StrategyDayBased,
				PlanPayInAdvance:   false,
				Currency:           "USD",
			},
			want: &proration.ProrationResult{
				NetAmount:     decimal.Zero,
				Action:        types.ProrationActionAddItem,
				ProrationDate: time.Date(2024, 3, 15, 0, 0, 0, 0, time.UTC),
				Currency:      "USD",
				IsPreview:     false,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			got, err := s.service.CalculateProration(s.GetContext(), tt.params)
			if tt.wantErr {
				s.Error(err)
				return
			}

			s.NoError(err)
			s.NotNil(got)
			s.Equal(tt.want.NetAmount.StringFixed(2), got.NetAmount.StringFixed(2))
			s.Equal(tt.want.Currency, got.Currency)
			s.Equal(tt.want.Action, got.Action)
			s.Equal(tt.want.ProrationDate.Unix(), got.ProrationDate.Unix())
		})
	}
}

func (s *ProrationServiceSuite) TestApplyProration() {
	tests := []struct {
		name    string
		setup   func() *proration.ProrationResult
		wantErr bool
	}{
		{
			name: "create_new_invoice",
			setup: func() *proration.ProrationResult {
				return &proration.ProrationResult{
					NetAmount:          decimal.NewFromInt(10),
					Currency:           "usd",
					Action:             types.ProrationActionUpgrade,
					ProrationDate:      s.testData.now,
					CurrentPeriodStart: s.testData.subscription.CurrentPeriodStart,
					CurrentPeriodEnd:   s.testData.subscription.CurrentPeriodEnd,
					CreditItems: []proration.ProrationLineItem{
						{
							Description: "Credit for unused time",
							Amount:      decimal.NewFromInt(-5),
							StartDate:   s.testData.now,
							EndDate:     s.testData.now.Add(24 * time.Hour),
							Quantity:    decimal.NewFromInt(1),
							PriceID:     s.testData.prices.standard.ID,
							IsCredit:    true,
						},
					},
					ChargeItems: []proration.ProrationLineItem{
						{
							Description: "Charge for upgrade",
							Amount:      decimal.NewFromInt(15),
							StartDate:   s.testData.now,
							EndDate:     s.testData.now.Add(24 * time.Hour),
							Quantity:    decimal.NewFromInt(1),
							PriceID:     s.testData.prices.premium.ID,
							IsCredit:    false,
						},
					},
				}
			},
			wantErr: false,
		},
		{
			name: "no_action_behavior",
			setup: func() *proration.ProrationResult {
				return &proration.ProrationResult{
					NetAmount: decimal.NewFromInt(10),
					Currency:  "usd",
				}
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			result := tt.setup()
			err := s.service.ApplyProration(
				s.GetContext(),
				result,
				types.ProrationBehaviorCreateProrations,
				types.GetTenantID(s.GetContext()),
				types.GetEnvironmentID(s.GetContext()),
				s.testData.subscription.ID,
			)

			if tt.wantErr {
				s.Error(err)
				return
			}

			s.NoError(err)
		})
	}
}

func (s *ProrationServiceSuite) TestCalculateAndApplySubscriptionProration() {
	tests := []struct {
		name    string
		params  proration.SubscriptionProrationParams
		want    *proration.SubscriptionProrationResult
		wantErr bool
	}{
		{
			name: "calendar_billing_active_proration_multiple_items",
			params: proration.SubscriptionProrationParams{
				Subscription: &subscription.Subscription{
					ID:                 s.testData.subscription.ID,
					StartDate:          s.testData.subscription.StartDate,
					CurrentPeriodStart: s.testData.subscription.CurrentPeriodStart,
					CurrentPeriodEnd:   s.testData.subscription.CurrentPeriodEnd,
					Currency:           s.testData.subscription.Currency,
					BillingPeriod:      s.testData.subscription.BillingPeriod,
					BillingPeriodCount: s.testData.subscription.BillingPeriodCount,
					SubscriptionStatus: s.testData.subscription.SubscriptionStatus,
					CustomerTimezone:   s.testData.subscription.CustomerTimezone,
					BaseModel:          s.testData.subscription.BaseModel,
					BillingAnchor:      s.testData.subscription.BillingAnchor,
					LineItems: []*subscription.SubscriptionLineItem{
						s.testData.lineItems.standard,
					},
				},
				Prices: map[string]*price.Price{
					s.testData.prices.standard.ID: s.testData.prices.standard,
					s.testData.prices.premium.ID:  s.testData.prices.premium,
				},
				ProrationMode: types.ProrationModeActive,
				BillingCycle:  types.BillingCycleCalendar,
			},
			want: &proration.SubscriptionProrationResult{
				Currency:             "USD",
				TotalProrationAmount: decimal.NewFromInt(10), // Standard price amount
				LineItemResults: map[string]*proration.ProrationResult{
					s.testData.lineItems.standard.ID: {
						NetAmount:          decimal.NewFromInt(10),
						Currency:           "USD",
						Action:             types.ProrationActionAddItem,
						ProrationDate:      s.testData.subscription.CurrentPeriodStart,
						CurrentPeriodStart: s.testData.subscription.CurrentPeriodStart,
						CurrentPeriodEnd:   s.testData.subscription.CurrentPeriodEnd,
					},
				},
			},
			wantErr: false,
		},
		{
			name: "calendar_billing_no_proration",
			params: proration.SubscriptionProrationParams{
				Subscription: &subscription.Subscription{
					ID:                 s.testData.subscription.ID,
					StartDate:          s.testData.subscription.StartDate,
					CurrentPeriodStart: s.testData.subscription.CurrentPeriodStart,
					CurrentPeriodEnd:   s.testData.subscription.CurrentPeriodEnd,
					Currency:           s.testData.subscription.Currency,
					BillingPeriod:      s.testData.subscription.BillingPeriod,
					BillingPeriodCount: s.testData.subscription.BillingPeriodCount,
					SubscriptionStatus: s.testData.subscription.SubscriptionStatus,
					CustomerTimezone:   s.testData.subscription.CustomerTimezone,
					BaseModel:          s.testData.subscription.BaseModel,
					BillingAnchor:      s.testData.subscription.BillingAnchor,
					LineItems: []*subscription.SubscriptionLineItem{
						s.testData.lineItems.standard,
					},
				},
				Prices: map[string]*price.Price{
					s.testData.prices.standard.ID: s.testData.prices.standard,
				},
				ProrationMode: types.ProrationModeNone,
				BillingCycle:  types.BillingCycleCalendar,
			},
			want: &proration.SubscriptionProrationResult{
				Currency:             "USD",
				TotalProrationAmount: decimal.Zero,
				LineItemResults:      make(map[string]*proration.ProrationResult),
			},
			wantErr: false,
		},
		{
			name: "anniversary_billing_no_proration",
			params: proration.SubscriptionProrationParams{
				Subscription: &subscription.Subscription{
					ID:                 s.testData.subscription.ID,
					StartDate:          s.testData.subscription.StartDate,
					CurrentPeriodStart: s.testData.subscription.CurrentPeriodStart,
					CurrentPeriodEnd:   s.testData.subscription.CurrentPeriodEnd,
					Currency:           s.testData.subscription.Currency,
					BillingPeriod:      s.testData.subscription.BillingPeriod,
					BillingPeriodCount: s.testData.subscription.BillingPeriodCount,
					SubscriptionStatus: s.testData.subscription.SubscriptionStatus,
					CustomerTimezone:   s.testData.subscription.CustomerTimezone,
					BaseModel:          s.testData.subscription.BaseModel,
					BillingAnchor:      s.testData.subscription.BillingAnchor,
					LineItems: []*subscription.SubscriptionLineItem{
						s.testData.lineItems.standard,
					},
				},
				Prices: map[string]*price.Price{
					s.testData.prices.standard.ID: s.testData.prices.standard,
				},
				ProrationMode: types.ProrationModeActive,
				BillingCycle:  types.BillingCycleAnniversary,
			},
			want: &proration.SubscriptionProrationResult{
				Currency:             "USD",
				TotalProrationAmount: decimal.Zero,
				LineItemResults:      make(map[string]*proration.ProrationResult),
			},
			wantErr: false,
		},
		{
			name: "invalid_subscription",
			params: proration.SubscriptionProrationParams{
				Subscription: nil,
				Prices:       map[string]*price.Price{},
			},
			want:    nil,
			wantErr: true,
		},
		{
			name: "missing_prices",
			params: proration.SubscriptionProrationParams{
				Subscription: &subscription.Subscription{
					ID:                 s.testData.subscription.ID,
					StartDate:          s.testData.subscription.StartDate,
					CurrentPeriodStart: s.testData.subscription.CurrentPeriodStart,
					CurrentPeriodEnd:   s.testData.subscription.CurrentPeriodEnd,
					LineItems: []*subscription.SubscriptionLineItem{
						s.testData.lineItems.standard,
					},
				},
				Prices:        nil,
				ProrationMode: types.ProrationModeActive,
				BillingCycle:  types.BillingCycleCalendar,
			},
			want:    nil,
			wantErr: true,
		},
		{
			name: "upgrade_standard_to_premium",
			params: proration.SubscriptionProrationParams{
				Subscription: &subscription.Subscription{
					ID:                 s.testData.subscription.ID,
					StartDate:          s.testData.subscription.StartDate,
					CurrentPeriodStart: s.testData.subscription.CurrentPeriodStart,
					CurrentPeriodEnd:   s.testData.subscription.CurrentPeriodEnd,
					Currency:           s.testData.subscription.Currency,
					BillingPeriod:      s.testData.subscription.BillingPeriod,
					BillingPeriodCount: s.testData.subscription.BillingPeriodCount,
					SubscriptionStatus: s.testData.subscription.SubscriptionStatus,
					CustomerTimezone:   s.testData.subscription.CustomerTimezone,
					BaseModel:          s.testData.subscription.BaseModel,
					BillingAnchor:      s.testData.subscription.BillingAnchor,
					LineItems: []*subscription.SubscriptionLineItem{
						s.testData.lineItems.premium,
					},
				},
				Prices: map[string]*price.Price{
					s.testData.prices.standard.ID: s.testData.prices.standard,
					s.testData.prices.premium.ID:  s.testData.prices.premium,
				},
				ProrationMode: types.ProrationModeActive,
				BillingCycle:  types.BillingCycleCalendar,
			},
			want: &proration.SubscriptionProrationResult{
				Currency:             "USD",
				TotalProrationAmount: decimal.NewFromInt(20), // Premium price amount
				LineItemResults: map[string]*proration.ProrationResult{
					s.testData.lineItems.premium.ID: {
						NetAmount:          decimal.NewFromInt(20),
						Currency:           "USD",
						Action:             types.ProrationActionAddItem,
						ProrationDate:      s.testData.subscription.CurrentPeriodStart,
						CurrentPeriodStart: s.testData.subscription.CurrentPeriodStart,
						CurrentPeriodEnd:   s.testData.subscription.CurrentPeriodEnd,
					},
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			// Clear any existing invoices before each test
			invoiceRepo := s.GetStores().InvoiceRepo.(*testutil.InMemoryInvoiceStore)
			invoiceRepo.Clear()

			// Create a new invoice for the test
			nextNumber, err := s.GetStores().InvoiceRepo.GetNextInvoiceNumber(s.GetContext())
			s.NoError(err)

			nextSeq, err := s.GetStores().InvoiceRepo.GetNextBillingSequence(s.GetContext(), tt.params.Subscription.ID)
			s.NoError(err)

			if !tt.wantErr {
				inv := &invoice.Invoice{
					SubscriptionID:  &tt.params.Subscription.ID,
					InvoiceType:     types.InvoiceTypeSubscription,
					InvoiceStatus:   types.InvoiceStatusDraft,
					PaymentStatus:   types.PaymentStatusPending,
					Currency:        tt.params.Subscription.Currency,
					InvoiceNumber:   &nextNumber,
					BillingSequence: &nextSeq,
					Description:     "Test Invoice",
					BillingReason:   string(types.InvoiceBillingReasonSubscriptionCreate),
					PeriodStart:     &tt.params.Subscription.CurrentPeriodStart,
					PeriodEnd:       &tt.params.Subscription.CurrentPeriodEnd,
					EnvironmentID:   tt.params.Subscription.EnvironmentID,
					BaseModel: types.BaseModel{
						TenantID: tt.params.Subscription.TenantID,
						Status:   types.StatusPublished,
					},
				}

				s.NoError(s.GetStores().InvoiceRepo.Create(s.GetContext(), inv))
			}

			got, err := s.service.CalculateAndApplySubscriptionProration(s.GetContext(), tt.params)
			if tt.wantErr {
				s.Error(err)
				return
			}

			s.NoError(err)
			s.NotNil(got)
			s.Equal(tt.want.Currency, got.Currency)
			s.Equal(tt.want.TotalProrationAmount.StringFixed(2), got.TotalProrationAmount.StringFixed(2))

			// Check line item results
			s.Equal(len(tt.want.LineItemResults), len(got.LineItemResults))
			for itemID, wantResult := range tt.want.LineItemResults {
				gotResult, exists := got.LineItemResults[itemID]
				s.True(exists)
				s.Equal(wantResult.NetAmount.StringFixed(2), gotResult.NetAmount.StringFixed(2))
				s.Equal(wantResult.Currency, gotResult.Currency)
				s.Equal(wantResult.Action, gotResult.Action)
				s.Equal(wantResult.ProrationDate.Unix(), gotResult.ProrationDate.Unix())
				s.Equal(wantResult.CurrentPeriodStart.Unix(), gotResult.CurrentPeriodStart.Unix())
				s.Equal(wantResult.CurrentPeriodEnd.Unix(), gotResult.CurrentPeriodEnd.Unix())
			}
		})
	}
}
