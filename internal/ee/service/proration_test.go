package service

import (
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/addon"
	"github.com/flexprice/flexprice/internal/domain/addonassociation"
	"github.com/flexprice/flexprice/internal/domain/creditgrant"
	"github.com/flexprice/flexprice/internal/domain/creditgrantapplication"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/entitlement"
	"github.com/flexprice/flexprice/internal/domain/entitlementgrant"
	"github.com/flexprice/flexprice/internal/domain/feature"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/domain/plan"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/proration"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/domain/wallet"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
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
		SettingsRepo:        s.GetStores().SettingsRepo,
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
		CustomerID:         "cust_123",
		StartDate:          s.testData.now.Add(-30 * 24 * time.Hour),
		CurrentPeriodStart: s.testData.now.Add(-24 * time.Hour),
		CurrentPeriodEnd:   s.testData.now.Add(6 * 24 * time.Hour),
		Currency:           "USD",
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		SubscriptionStatus: types.SubscriptionStatusActive,
		Timezone:           "UTC",
		BaseModel:          types.GetDefaultBaseModel(s.GetContext()),
		BillingAnchor:      types.CalculateCalendarBillingAnchor(s.testData.now.Add(-30*24*time.Hour), types.BILLING_PERIOD_MONTHLY, ""),
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
	nextNumber, err := s.GetStores().InvoiceRepo.GetNextInvoiceNumber(s.GetContext(), &types.InvoiceConfig{
		InvoiceNumberPrefix:        "INV",
		InvoiceNumberFormat:        types.InvoiceNumberFormatYYYYMM,
		InvoiceNumberTimezone:      "UTC",
		InvoiceNumberStartSequence: 1,
		InvoiceNumberSeparator:     "-",
		InvoiceNumberSuffixLength:  5,
	})
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
	marchSub := &subscription.Subscription{BillingAnchor: time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC), Timezone: "UTC"}
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
				CurrentPeriodEnd:   time.Date(2024, 4, 1, 0, 0, 0, 0, time.UTC),
				ProrationBehavior:  types.ProrationBehaviorCreateProrations,
				Subscription:       marchSub,
				BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
				BillingPeriodCount: 1,
				PlanPayInAdvance:   true,
				OriginalAmountPaid: decimal.NewFromInt(10),
				Currency:           "USD",
			},
			want: &proration.ProrationResult{
				NetAmount:     decimal.NewFromFloat(5.49), // Credit: -(10 * 17/31) = -5.48, Charge: (10 * 17/31) = 5.49, Net: 5.49
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
				CurrentPeriodEnd:   time.Date(2024, 4, 1, 0, 0, 0, 0, time.UTC),
				ProrationBehavior:  types.ProrationBehaviorCreateProrations,
				Subscription:       marchSub,
				BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
				BillingPeriodCount: 1,
				PlanPayInAdvance:   true,
				OriginalAmountPaid: decimal.NewFromInt(10),
				Currency:           "USD",
			},
			want: &proration.ProrationResult{
				NetAmount:     decimal.NewFromFloat(5.49), // Credit: -(10 * 17/31) = -5.48, Charge: (10 * 17/31) = 5.48, Net: 5.48
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
				CurrentPeriodEnd:   time.Date(2024, 4, 1, 0, 0, 0, 0, time.UTC),
				ProrationBehavior:  types.ProrationBehaviorCreateProrations,
				Subscription:       marchSub,
				BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
				BillingPeriodCount: 1,
				PlanPayInAdvance:   true,
				OriginalAmountPaid: decimal.NewFromInt(50), // 5 seats * $10 per seat
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
				CurrentPeriodEnd:   time.Date(2024, 4, 1, 0, 0, 0, 0, time.UTC),
				ProrationBehavior:  types.ProrationBehaviorCreateProrations,
				Subscription:       marchSub,
				BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
				BillingPeriodCount: 1,
				PlanPayInAdvance:   true,
				OriginalAmountPaid: decimal.NewFromInt(1000),
				Currency:           "USD",
			},
			want: &proration.ProrationResult{
				NetAmount:     decimal.NewFromFloat(-274.20), // Credit: -(1000 * 17/31) = -548.39, Charge: (-500 * 17/31) = -274.19, Net: -274.19
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
				CurrentPeriodEnd:   time.Date(2024, 4, 1, 0, 0, 0, 0, time.UTC),
				ProrationBehavior:  types.ProrationBehaviorCreateProrations,
				Subscription:       marchSub,
				BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
				BillingPeriodCount: 1,
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
			s.Equal(tt.want.NetAmount.String(), got.NetAmount.String())
			s.Equal(tt.want.Currency, got.Currency)
			s.Equal(tt.want.Action, got.Action)
			s.Equal(tt.want.ProrationDate.Unix(), got.ProrationDate.Unix())
		})
	}
}

// ProrationScenarioSuite covers proration end to end through subscription create, addon attach and
// remove, trials and backdating. Dates are in 2027 because create_prorations rejects past start dates.
type ProrationScenarioSuite struct {
	testutil.BaseServiceTestSuite
	params  ServiceParams
	subSvc  SubscriptionService
	billing BillingService
	cust    *customer.Customer
	meter   *meter.Meter
}

func TestProrationScenarios(t *testing.T) {
	suite.Run(t, new(ProrationScenarioSuite))
}

const (
	pvrAmount  = 31
	pvrCredits = 1000
	pvrQuota   = 100
)

func pvrDate(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }

func (s *ProrationScenarioSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	s.ClearStores()
	st := s.GetStores()
	s.params = ServiceParams{
		Logger:                       s.GetLogger(),
		Config:                       s.GetConfig(),
		DB:                           s.GetDB(),
		TaxAssociationRepo:           st.TaxAssociationRepo,
		TaxRateRepo:                  st.TaxRateRepo,
		SubRepo:                      st.SubscriptionRepo,
		SubscriptionLineItemRepo:     st.SubscriptionLineItemRepo,
		SubscriptionPhaseRepo:        st.SubscriptionPhaseRepo,
		SubScheduleRepo:              st.SubscriptionScheduleRepo,
		PlanRepo:                     st.PlanRepo,
		PriceRepo:                    st.PriceRepo,
		PriceUnitRepo:                st.PriceUnitRepo,
		EventRepo:                    st.EventRepo,
		MeterRepo:                    st.MeterRepo,
		CustomerRepo:                 st.CustomerRepo,
		InvoiceRepo:                  st.InvoiceRepo,
		InvoiceLineItemRepo:          st.InvoiceLineItemRepo,
		EntitlementRepo:              st.EntitlementRepo,
		EntitlementGrantRepo:         st.EntitlementGrantRepo,
		EnvironmentRepo:              st.EnvironmentRepo,
		FeatureRepo:                  st.FeatureRepo,
		TenantRepo:                   st.TenantRepo,
		UserRepo:                     st.UserRepo,
		AuthRepo:                     st.AuthRepo,
		WalletRepo:                   st.WalletRepo,
		PaymentRepo:                  st.PaymentRepo,
		CreditGrantRepo:              st.CreditGrantRepo,
		CreditGrantApplicationRepo:   st.CreditGrantApplicationRepo,
		CouponRepo:                   st.CouponRepo,
		CouponAssociationRepo:        st.CouponAssociationRepo,
		CouponApplicationRepo:        st.CouponApplicationRepo,
		AlertLogsRepo:                st.AlertLogsRepo,
		WalletBalanceAlertPubSub:     types.WalletBalanceAlertPubSub{PubSub: testutil.NewInMemoryPubSub()},
		AddonRepo:                    st.AddonRepo,
		AddonAssociationRepo:         st.AddonAssociationRepo,
		CheckoutSessionRepo:          st.CheckoutSessionRepo,
		TaxAppliedRepo:               st.TaxAppliedRepo,
		ConnectionRepo:               st.ConnectionRepo,
		SettingsRepo:                 st.SettingsRepo,
		EventPublisher:               s.GetPublisher(),
		WebhookPublisher:             s.GetWebhookPublisher(),
		ProrationCalculator:          s.GetCalculator(),
		MeterUsageRepo:               st.MeterUsageRepo,
		IntegrationFactory:           s.GetIntegrationFactory(),
		PlanPriceSyncRepo:            st.PlanPriceSyncRepo,
		EntityIntegrationMappingRepo: st.EntityIntegrationMappingRepo,
	}
	s.subSvc = NewSubscriptionService(s.params)
	s.billing = NewBillingService(s.params)

	ctx := s.GetContext()
	s.cust = &customer.Customer{
		ID:         types.GenerateUUIDWithPrefix(types.UUID_PREFIX_CUSTOMER),
		ExternalID: "pvr_cust",
		Name:       "PVR Customer",
		Email:      "pvr@example.com",
		BaseModel:  types.GetDefaultBaseModel(ctx),
	}
	s.Require().NoError(st.CustomerRepo.Create(ctx, s.cust))

	s.meter = &meter.Meter{
		ID:          types.GenerateUUIDWithPrefix(types.UUID_PREFIX_METER),
		Name:        "PVR Calls",
		EventName:   "pvr_call",
		Aggregation: meter.Aggregation{Type: types.AggregationCount},
		BaseModel:   types.GetDefaultBaseModel(ctx),
	}
	s.Require().NoError(st.MeterRepo.CreateMeter(ctx, s.meter))

	s.Require().NoError(st.WalletRepo.CreateWallet(ctx, &wallet.Wallet{
		ID:                  "wallet_pvr",
		CustomerID:          s.cust.ID,
		Name:                "PVR Wallet",
		Currency:            "usd",
		WalletStatus:        types.WalletStatusActive,
		ConversionRate:      decimal.NewFromInt(1),
		TopupConversionRate: decimal.NewFromInt(1),
		BaseModel:           types.GetDefaultBaseModel(ctx),
	}))
}

func (s *ProrationScenarioSuite) TearDownTest() {
	s.BaseServiceTestSuite.TearDownTest()
	s.ClearStores()
}

// -----------------------------------------------------------------------------
// expectations
// -----------------------------------------------------------------------------

func (s *ProrationScenarioSuite) pvrExpect(label, want string, got decimal.Decimal) {
	expected := decimal.RequireFromString(want)
	ok := got.Sub(expected).Abs().LessThanOrEqual(decimal.NewFromFloat(0.01))
	s.True(ok, "%s: expected %s, got %s", label, want, got.StringFixed(2))
}

func (s *ProrationScenarioSuite) pvrExpectTime(label string, want, got time.Time) {
	s.True(want.Equal(got), "%s: expected %s, got %s", label, want.UTC(), got.UTC())
}

// -----------------------------------------------------------------------------
// fixtures
// -----------------------------------------------------------------------------

type pvrPlanSpec struct {
	id     string
	withCG bool
	withEG bool
}

// pvrSeedPlan registers a plan with a $31 fixed advance monthly price and optional plan CG/EG.
// Returns the feature id the plan EG feeds (empty if none).
func (s *ProrationScenarioSuite) pvrSeedPlan(spec pvrPlanSpec) string {
	ctx := s.GetContext()
	st := s.GetStores()
	s.Require().NoError(st.PlanRepo.Create(ctx, &plan.Plan{ID: spec.id, Name: spec.id, BaseModel: types.GetDefaultBaseModel(ctx)}))
	s.Require().NoError(st.PriceRepo.Create(ctx, &price.Price{
		ID: "price_" + spec.id, Amount: decimal.NewFromInt(pvrAmount), Currency: "usd",
		EntityType: types.PRICE_ENTITY_TYPE_PLAN, EntityID: spec.id, Type: types.PRICE_TYPE_FIXED,
		BillingCadence: types.BILLING_CADENCE_RECURRING, BillingPeriod: types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1, BillingModel: types.BILLING_MODEL_FLAT_FEE, InvoiceCadence: types.InvoiceCadenceAdvance,
		BaseModel: types.GetDefaultBaseModel(ctx),
	}))
	if spec.withCG {
		planID := spec.id
		_, err := NewCreditGrantService(s.params).CreateCreditGrant(ctx, dto.CreateCreditGrantRequest{
			Name: "Plan Monthly Credits", Scope: types.CreditGrantScopePlan, PlanID: &planID,
			Credits: decimal.NewFromInt(pvrCredits), Cadence: types.CreditGrantCadenceRecurring,
			Period: lo.ToPtr(types.CREDIT_GRANT_PERIOD_MONTHLY), PeriodCount: lo.ToPtr(1),
			ExpirationType: types.CreditGrantExpiryTypeNever, Priority: lo.ToPtr(1),
		})
		s.Require().NoError(err)
	}
	if !spec.withEG {
		return ""
	}
	return s.pvrSeedEntitlement(types.ENTITLEMENT_ENTITY_TYPE_PLAN, spec.id)
}

func (s *ProrationScenarioSuite) pvrSeedEntitlement(entityType types.EntitlementEntityType, entityID string) string {
	ctx := s.GetContext()
	st := s.GetStores()
	featureID := "feat_" + entityID
	s.Require().NoError(st.FeatureRepo.Create(ctx, &feature.Feature{
		ID: featureID, Name: featureID, Type: types.FeatureTypeMetered, MeterID: s.meter.ID,
		BaseModel: types.GetDefaultBaseModel(ctx),
	}))
	dv := 1
	q := decimal.NewFromInt(pvrQuota)
	_, err := st.EntitlementRepo.Create(ctx, &entitlement.Entitlement{
		ID: "ent_" + entityID, EntityType: entityType, EntityID: entityID, FeatureID: featureID,
		FeatureType: types.FeatureTypeMetered, IsEnabled: true,
		GrantMeasure: types.EntitlementGrantMeasureQuantity, GrantDurationValue: &dv,
		GrantDurationUnit: types.EntitlementGrantDurationUnitSubscriptionPeriod, GrantQuota: &q,
		BaseModel: types.GetDefaultBaseModel(ctx),
	})
	s.Require().NoError(err)
	return featureID
}

type pvrAddonSpec struct {
	id         string
	fixedPrice bool
	withCG     bool
	withEG     bool
	period     types.BillingPeriod // fixed price cadence; default MONTHLY
	amount     int64               // fixed price amount; default 31
}

// pvrSeedAddon registers an addon ($31 fixed advance monthly, or a zero usage price) with optional
// CG/EG. Returns the feature id the addon EG feeds (empty if none).
func (s *ProrationScenarioSuite) pvrSeedAddon(spec pvrAddonSpec) string {
	ctx := s.GetContext()
	st := s.GetStores()
	s.Require().NoError(st.AddonRepo.Create(ctx, &addon.Addon{
		ID: spec.id, LookupKey: spec.id, Name: spec.id, BaseModel: types.GetDefaultBaseModel(ctx),
	}))
	pr := &price.Price{
		ID: "price_" + spec.id, Currency: "usd", EntityType: types.PRICE_ENTITY_TYPE_ADDON, EntityID: spec.id,
		BillingCadence: types.BILLING_CADENCE_RECURRING, BillingPeriod: types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1, BillingModel: types.BILLING_MODEL_FLAT_FEE, BaseModel: types.GetDefaultBaseModel(ctx),
	}
	if spec.fixedPrice {
		pr.Amount = decimal.NewFromInt(lo.CoalesceOrEmpty(spec.amount, pvrAmount))
		pr.BillingPeriod = lo.CoalesceOrEmpty(spec.period, types.BILLING_PERIOD_MONTHLY)
		pr.Type = types.PRICE_TYPE_FIXED
		pr.InvoiceCadence = types.InvoiceCadenceAdvance
	} else {
		pr.Amount = decimal.Zero
		pr.Type = types.PRICE_TYPE_USAGE
		pr.InvoiceCadence = types.InvoiceCadenceArrear
		pr.MeterID = s.meter.ID
	}
	s.Require().NoError(st.PriceRepo.Create(ctx, pr))
	if spec.withCG {
		addonID := spec.id
		_, err := NewCreditGrantService(s.params).CreateCreditGrant(ctx, dto.CreateCreditGrantRequest{
			Name: "Addon Monthly Credits", Scope: types.CreditGrantScopeAddon, AddonID: &addonID,
			Credits: decimal.NewFromInt(pvrCredits), Cadence: types.CreditGrantCadenceRecurring,
			Period: lo.ToPtr(types.CREDIT_GRANT_PERIOD_MONTHLY), PeriodCount: lo.ToPtr(1),
			ExpirationType: types.CreditGrantExpiryTypeNever, Priority: lo.ToPtr(1),
		})
		s.Require().NoError(err)
	}
	if !spec.withEG {
		return ""
	}
	return s.pvrSeedEntitlement(types.ENTITLEMENT_ENTITY_TYPE_ADDON, spec.id)
}

type pvrSubSpec struct {
	planID    string
	start     time.Time
	cycle     types.BillingCycle
	behavior  types.ProrationBehavior
	anchor    *time.Time
	trialDays *int
	addons    []dto.AddAddonToSubscriptionRequest
	period    types.BillingPeriod // default MONTHLY
	include   *[]string           // include_price_ids
}

func (s *ProrationScenarioSuite) pvrCreateSub(spec pvrSubSpec) *subscription.Subscription {
	ctx := s.GetContext()
	start := spec.start
	req := dto.CreateSubscriptionRequest{
		CustomerID: s.cust.ID, PlanID: spec.planID, Currency: "usd", StartDate: &start,
		BillingCadence: types.BILLING_CADENCE_RECURRING, BillingPeriod: lo.CoalesceOrEmpty(spec.period, types.BILLING_PERIOD_MONTHLY),
		BillingPeriodCount: 1, BillingCycle: spec.cycle, ProrationBehavior: spec.behavior,
		Timezone: "UTC", BillingAnchor: spec.anchor, TrialPeriodDays: spec.trialDays,
		IncludePriceIDs: spec.include,
	}
	req.Addons = spec.addons
	resp, err := s.subSvc.CreateSubscription(ctx, req)
	s.Require().NoError(err)
	return s.pvrReload(resp.ID)
}

func (s *ProrationScenarioSuite) pvrReload(subID string) *subscription.Subscription {
	sub, items, err := s.GetStores().SubscriptionRepo.GetWithLineItems(s.GetContext(), subID)
	s.Require().NoError(err)
	sub.LineItems = items
	return sub
}

func pvrAddReq(addonID string, at time.Time, behavior types.ProrationBehavior) dto.AddAddonToSubscriptionRequest {
	return dto.AddAddonToSubscriptionRequest{
		AddonID: addonID, Cadence: types.AddonCadenceRecurring, StartDate: &at, ProrationBehavior: behavior,
	}
}

func (s *ProrationScenarioSuite) pvrAttach(subID string, req dto.AddAddonToSubscriptionRequest) error {
	_, err := s.subSvc.AddAddonToSubscription(s.GetContext(), &dto.AddAddonRequest{
		SubscriptionID: subID, AddAddonToSubscriptionRequest: req,
	})
	return err
}

// pvrPreview quotes a bulk addon change through AddonChangeService, the path AddAddon/RemoveAddon run.
func (s *ProrationScenarioSuite) pvrPreview(subID string, params *dto.SubModifyBulkAddonParams) *LineItemProrationSummary {
	sub := s.pvrReload(subID)
	config, _, err := NewAddonChangeService(s.params).Preview(s.GetContext(), NewAddonChangeRequest(sub, params))
	s.Require().NoError(err)
	quote := config.getQuote()
	s.Require().NotNil(quote)
	return quote
}

func (s *ProrationScenarioSuite) pvrLineItem(sub *subscription.Subscription, entityType types.SubscriptionLineItemEntityType) *subscription.SubscriptionLineItem {
	items, err := s.GetStores().SubscriptionLineItemRepo.ListBySubscription(s.GetContext(), sub)
	s.Require().NoError(err)
	li, found := lo.Find(items, func(li *subscription.SubscriptionLineItem) bool {
		return li.EntityType == entityType && li.PriceType == types.PRICE_TYPE_FIXED
	})
	s.Require().True(found, "fixed %s line item not found", entityType)
	return li
}

func (s *ProrationScenarioSuite) pvrAssociation(subID string) *addonassociation.AddonAssociation {
	filter := types.NewNoLimitAddonAssociationFilter()
	filter.EntityIDs = []string{subID}
	list, err := s.GetStores().AddonAssociationRepo.List(s.GetContext(), filter)
	s.Require().NoError(err)
	s.Require().NotEmpty(list, "addon association not found")
	return list[0]
}

// pvrFixed returns the CalculateFixedCharges total and per line item amount for [start, end).
func (s *ProrationScenarioSuite) pvrFixed(sub *subscription.Subscription, start, end time.Time) (decimal.Decimal, map[string]decimal.Decimal) {
	res, err := s.billing.CalculateFixedCharges(s.GetContext(), &dto.CalculateFixedChargesParams{
		Subscription: sub, PeriodStart: start, PeriodEnd: end,
	})
	s.Require().NoError(err)
	per := map[string]decimal.Decimal{}
	for _, line := range res.LineItems {
		id := lo.FromPtr(line.SubscriptionLineItemID)
		per[id] = per[id].Add(line.Amount)
	}
	return res.TotalAmount, per
}

func (s *ProrationScenarioSuite) pvrGrants(subID, addonID string) []*creditgrant.CreditGrant {
	filter := types.NewNoLimitCreditGrantFilter()
	filter.SubscriptionIDs = []string{subID}
	grants, err := s.GetStores().CreditGrantRepo.List(s.GetContext(), filter)
	s.Require().NoError(err)
	return lo.Filter(grants, func(g *creditgrant.CreditGrant, _ int) bool {
		if g.Scope != types.CreditGrantScopeSubscription {
			return false
		}
		if addonID == "" {
			return g.AddonID == nil || *g.AddonID == ""
		}
		return g.AddonID != nil && *g.AddonID == addonID
	})
}

func (s *ProrationScenarioSuite) pvrFirstApp(subID, addonID string) *creditgrantapplication.CreditGrantApplication {
	grants := s.pvrGrants(subID, addonID)
	s.Require().Len(grants, 1, "expected exactly one materialized grant")
	apps, err := s.GetStores().CreditGrantApplicationRepo.List(s.GetContext(), &types.CreditGrantApplicationFilter{
		CreditGrantIDs: []string{grants[0].ID}, QueryFilter: types.NewNoLimitQueryFilter(),
	})
	s.Require().NoError(err)
	s.Require().NotEmpty(apps, "expected at least one application")
	sort.Slice(apps, func(i, j int) bool { return apps[i].PeriodStart.Before(apps[j].PeriodStart) })
	return apps[0]
}

func (s *ProrationScenarioSuite) pvrFirstEG(featureID string) *entitlementgrant.EntitlementGrant {
	rows, err := s.GetStores().EntitlementGrantRepo.List(s.GetContext(), types.NewNoLimitEntitlementGrantFilter())
	s.Require().NoError(err)
	rows = lo.Filter(rows, func(g *entitlementgrant.EntitlementGrant, _ int) bool { return g.FeatureID() == featureID })
	s.Require().NotEmpty(rows, "expected an entitlement grant row for %s", featureID)
	sort.Slice(rows, func(i, j int) bool { return rows[i].ValidFrom.Before(rows[j].ValidFrom) })
	return rows[0]
}

// -----------------------------------------------------------------------------
// scenarios
// -----------------------------------------------------------------------------

// B2: calendar monthly, create_prorations, start Jan 31: stub [Jan31, Feb1) of [Jan1, Feb1) = 1/31 x 31.
func (s *ProrationScenarioSuite) TestCalendarStartLastDayOfMonth() {
	s.pvrSeedPlan(pvrPlanSpec{id: "plan_pvr_b2"})
	sub := s.pvrCreateSub(pvrSubSpec{
		planID: "plan_pvr_b2", start: pvrDate(2027, 1, 31), cycle: types.BillingCycleCalendar,
		behavior: types.ProrationBehaviorCreateProrations,
	})
	s.pvrExpectTime("B2 current_period_end", pvrDate(2027, 2, 1), sub.CurrentPeriodEnd)
	total, _ := s.pvrFixed(sub, sub.CurrentPeriodStart, sub.CurrentPeriodEnd)
	s.pvrExpect("B2 opening charge Jan31-Feb1 (1/31)", "1.00", total)
}

// D4: anniversary start Jan 10, anchor Jan 20; $31 addon attached Jan 15: 5/31 of [Dec20, Jan20).
func (s *ProrationScenarioSuite) TestAddonOnAnniversaryAnchorStub() {
	s.pvrSeedPlan(pvrPlanSpec{id: "plan_pvr_d4"})
	anchor := pvrDate(2027, 1, 20)
	sub := s.pvrCreateSub(pvrSubSpec{
		planID: "plan_pvr_d4", start: pvrDate(2027, 1, 10), cycle: types.BillingCycleAnniversary,
		behavior: types.ProrationBehaviorCreateProrations, anchor: &anchor,
	})
	s.pvrExpectTime("D4 current_period_end", anchor, sub.CurrentPeriodEnd)
	s.pvrSeedAddon(pvrAddonSpec{id: "addon_pvr_d4", fixedPrice: true})
	add := pvrAddReq("addon_pvr_d4", pvrDate(2027, 1, 15), types.ProrationBehaviorCreateProrations)
	quote := s.pvrPreview(sub.ID, &dto.SubModifyBulkAddonParams{Adds: []*dto.AddAddonToSubscriptionRequest{&add}})
	s.pvrExpect("D4 addon attach charge (5/31)", "5.00", quote.TotalChargeAmount)
}

// D5: anniversary start Jan 1, current period [Feb1, Mar1); addon attached at Feb 1 00:00 bills
// the whole period: 31.00 in a single full-period line.
func (s *ProrationScenarioSuite) TestAddonAttachedAtPeriodBoundary() {
	s.pvrSeedPlan(pvrPlanSpec{id: "plan_pvr_d5"})
	sub := s.pvrCreateSub(pvrSubSpec{
		planID: "plan_pvr_d5", start: pvrDate(2027, 1, 1), cycle: types.BillingCycleAnniversary,
		behavior: types.ProrationBehaviorCreateProrations,
	})
	sub.CurrentPeriodStart = pvrDate(2027, 2, 1)
	sub.CurrentPeriodEnd = pvrDate(2027, 3, 1)
	s.Require().NoError(s.GetStores().SubscriptionRepo.Update(s.GetContext(), sub))

	s.pvrSeedAddon(pvrAddonSpec{id: "addon_pvr_d5", fixedPrice: true})
	add := pvrAddReq("addon_pvr_d5", pvrDate(2027, 2, 1), types.ProrationBehaviorCreateProrations)
	quote := s.pvrPreview(sub.ID, &dto.SubModifyBulkAddonParams{Adds: []*dto.AddAddonToSubscriptionRequest{&add}})
	s.pvrExpect("D5 boundary attach charge (full period)", "31.00", quote.TotalChargeAmount)
	s.Equal(1, len(quote.ChargeLineItems), "D5: expected a single full-period charge line")
	if len(quote.ChargeLineItems) == 1 {
		l := quote.ChargeLineItems[0]
		s.pvrExpectTime("D5 charge line period_start", pvrDate(2027, 2, 1), lo.FromPtr(l.PeriodStart))
		s.pvrExpectTime("D5 charge line period_end", pvrDate(2027, 3, 1), lo.FromPtr(l.PeriodEnd))
	}
}

// G4: calendar sub start Jan 15 with a $31 addon attached at creation (billed 17.00 on the opening
// invoice); removing it Jan 20 with create_prorations credits 12/31 x 31 = 12.00.
func (s *ProrationScenarioSuite) TestRemoveAddonInsideCalendarStub() {
	s.pvrSeedPlan(pvrPlanSpec{id: "plan_pvr_g4"})
	s.pvrSeedAddon(pvrAddonSpec{id: "addon_pvr_g4", fixedPrice: true})
	start := pvrDate(2027, 1, 15)
	sub := s.pvrCreateSub(pvrSubSpec{
		planID: "plan_pvr_g4", start: start, cycle: types.BillingCycleCalendar,
		behavior: types.ProrationBehaviorCreateProrations,
		addons:   []dto.AddAddonToSubscriptionRequest{pvrAddReq("addon_pvr_g4", start, types.ProrationBehaviorCreateProrations)},
	})
	li := s.pvrLineItem(sub, types.SubscriptionLineItemEntityTypeAddon)

	_, per := s.pvrFixed(sub, sub.CurrentPeriodStart, sub.CurrentPeriodEnd)
	s.pvrExpect("G4 opening addon charge Jan15-Feb1 (17/31)", "17.00", per[li.ID])

	removeAt := pvrDate(2027, 1, 20)
	assoc := s.pvrAssociation(sub.ID)
	quote := s.pvrPreview(sub.ID, &dto.SubModifyBulkAddonParams{Removes: []*dto.RemoveAddonRequest{{
		AddonAssociationID: assoc.ID, ProrationBehavior: types.ProrationBehaviorCreateProrations, EffectiveDate: &removeAt,
	}}})
	s.pvrExpect("G4 addon removal credit (12/31)", "12.00", quote.TotalCreditAmount)
}

// H7: proration_behavior=none never prorates grants: plan CG 1000 / EG 100 on the Jan 15 calendar
// stub, and an addon attached Jan 20 with none gets CG 1000 / EG 100.
func (s *ProrationScenarioSuite) TestProrationNoneGrantsFull() {
	planFeature := s.pvrSeedPlan(pvrPlanSpec{id: "plan_pvr_h7", withCG: true, withEG: true})
	sub := s.pvrCreateSub(pvrSubSpec{
		planID: "plan_pvr_h7", start: pvrDate(2027, 1, 15), cycle: types.BillingCycleCalendar,
		behavior: types.ProrationBehaviorNone,
	})
	s.Run("plan_cg_full", func() {
		app := s.pvrFirstApp(sub.ID, "")
		s.pvrExpect("H7 plan CG first application (none)", "1000", app.Credits)
	})
	s.Run("plan_eg_full", func() {
		eg := s.pvrFirstEG(planFeature)
		s.pvrExpect("H7 plan EG first window quota (none)", "100", eg.Quota)
	})

	addonFeature := s.pvrSeedAddon(pvrAddonSpec{id: "addon_pvr_h7", withCG: true, withEG: true})
	s.Require().NoError(s.pvrAttach(sub.ID, pvrAddReq("addon_pvr_h7", pvrDate(2027, 1, 20), types.ProrationBehaviorNone)))
	s.Run("addon_cg_full", func() {
		app := s.pvrFirstApp(sub.ID, "addon_pvr_h7")
		s.pvrExpect("H7 addon CG first application (none)", "1000", app.Credits)
	})
	s.Run("addon_eg_full", func() {
		eg := s.pvrFirstEG(addonFeature)
		s.pvrExpect("H7 addon EG first window quota (none)", "100", eg.Quota)
	})
}

// I3: anniversary sub start Jan 15 with a 14-day trial; trial end at Jan 29 re-anchors to Jan 29,
// first paid period [Jan29, Feb28) bills the full 31.00.
func (s *ProrationScenarioSuite) TestAnniversaryTrialEndReanchors() {
	s.pvrSeedPlan(pvrPlanSpec{id: "plan_pvr_i3"})
	sub := s.pvrCreateSub(pvrSubSpec{
		planID: "plan_pvr_i3", start: pvrDate(2027, 1, 15), cycle: types.BillingCycleAnniversary,
		behavior: types.ProrationBehaviorCreateProrations, trialDays: lo.ToPtr(14),
	})
	s.Require().NotNil(sub.TrialEnd)
	s.pvrExpectTime("I3 trial_end", pvrDate(2027, 1, 29), lo.FromPtr(sub.TrialEnd))

	svc := s.subSvc.(*subscriptionService)
	inv, err := svc.processSubscriptionTrialEnd(s.GetContext(), sub, NewInvoiceService(s.params), pvrDate(2027, 1, 29))
	s.Require().NoError(err)

	after := s.pvrReload(sub.ID)
	s.pvrExpectTime("I3 billing_anchor after trial end", pvrDate(2027, 1, 29), after.BillingAnchor)
	s.pvrExpectTime("I3 current_period_start", pvrDate(2027, 1, 29), after.CurrentPeriodStart)
	s.pvrExpectTime("I3 current_period_end", pvrDate(2027, 2, 28), after.CurrentPeriodEnd)
	total, _ := s.pvrFixed(after, after.CurrentPeriodStart, after.CurrentPeriodEnd)
	s.pvrExpect("I3 first paid period charge (full)", "31.00", total)
	if inv != nil {
		s.pvrExpect("I3 trial-end invoice subtotal (full)", "31.00", inv.Subtotal)
	}
}

// J3: backdated calendar sub (proration none) start Jul 15, anchor Aug 1, addon line item from
// Aug 10, each past window charged directly with CalculateFixedCharges.
func (s *ProrationScenarioSuite) TestBackdatedSubPastPeriods() {
	ctx := s.GetContext()
	st := s.GetStores()
	start := pvrDate(2026, 7, 15)
	addonStart := pvrDate(2026, 8, 10)
	planID, addonID := "plan_pvr_j3", "addon_pvr_j3"
	s.Require().NoError(st.PlanRepo.Create(ctx, &plan.Plan{ID: planID, Name: planID, BaseModel: types.GetDefaultBaseModel(ctx)}))

	sub := &subscription.Subscription{
		ID: "sub_pvr_j3", PlanID: planID, CustomerID: s.cust.ID,
		StartDate: start, BillingAnchor: pvrDate(2026, 8, 1),
		CurrentPeriodStart: start, CurrentPeriodEnd: pvrDate(2026, 8, 1),
		Currency: "usd", BillingPeriod: types.BILLING_PERIOD_MONTHLY, BillingPeriodCount: 1,
		BillingCycle: types.BillingCycleCalendar, SubscriptionStatus: types.SubscriptionStatusActive,
		Timezone: "UTC", ProrationBehavior: types.ProrationBehaviorNone, BaseModel: types.GetDefaultBaseModel(ctx),
	}
	mk := func(key, entityID string, entityType types.SubscriptionLineItemEntityType, priceEntity types.PriceEntityType, liStart time.Time) *subscription.SubscriptionLineItem {
		pr := &price.Price{
			ID: "price_pvr_j3_" + key, Amount: decimal.NewFromInt(pvrAmount), Currency: "usd",
			EntityType: priceEntity, EntityID: entityID, Type: types.PRICE_TYPE_FIXED,
			BillingPeriod: types.BILLING_PERIOD_MONTHLY, BillingPeriodCount: 1, BillingModel: types.BILLING_MODEL_FLAT_FEE,
			BillingCadence: types.BILLING_CADENCE_RECURRING, InvoiceCadence: types.InvoiceCadenceAdvance,
			StartDate: lo.ToPtr(pvrDate(2025, 1, 1)), BaseModel: types.GetDefaultBaseModel(ctx),
		}
		s.Require().NoError(st.PriceRepo.Create(ctx, pr))
		return &subscription.SubscriptionLineItem{
			ID: "li_pvr_j3_" + key, SubscriptionID: sub.ID, CustomerID: s.cust.ID,
			EntityID: entityID, EntityType: entityType, PriceID: pr.ID, PriceType: types.PRICE_TYPE_FIXED,
			DisplayName: key, Quantity: decimal.NewFromInt(1), Currency: "usd",
			BillingPeriod: types.BILLING_PERIOD_MONTHLY, BillingPeriodCount: 1,
			InvoiceCadence: types.InvoiceCadenceAdvance, StartDate: liStart, BaseModel: types.GetDefaultBaseModel(ctx),
		}
	}
	planLI := mk("plan", planID, types.SubscriptionLineItemEntityTypePlan, types.PRICE_ENTITY_TYPE_PLAN, start)
	addonLI := mk("addon", addonID, types.SubscriptionLineItemEntityTypeAddon, types.PRICE_ENTITY_TYPE_ADDON, addonStart)
	items := []*subscription.SubscriptionLineItem{planLI, addonLI}
	s.Require().NoError(st.SubscriptionRepo.CreateWithLineItems(ctx, sub, items))
	sub.LineItems = items

	periods := []struct {
		name        string
		start, end  time.Time
		plan, addon string
	}{
		{"P1 Jul15-Aug1", start, pvrDate(2026, 8, 1), "31.00", "0.00"},
		// D5: with none, the addon starting Aug 10 is free until the next period.
		{"P2 Aug1-Sep1", pvrDate(2026, 8, 1), pvrDate(2026, 9, 1), "31.00", "0.00"},
		{"P3 Sep1-Oct1", pvrDate(2026, 9, 1), pvrDate(2026, 10, 1), "31.00", "31.00"},
	}
	for _, p := range periods {
		s.Run(p.name, func() {
			sub.CurrentPeriodStart, sub.CurrentPeriodEnd = p.start, p.end
			_, per := s.pvrFixed(sub, p.start, p.end)
			s.pvrExpect("J3 "+p.name+" plan (none)", p.plan, per[planLI.ID])
			s.pvrExpect("J3 "+p.name+" addon (none, rule: proration only with create_prorations)", p.addon, per[addonLI.ID])
		})
	}
}

// A7: a billing anchor may be at most one period after the start; an earlier anchor only sets the
// schedule. Stripe imports keep their anchor, and draft activation keeps a custom anchor that is still in range.
func (s *ProrationScenarioSuite) TestBillingAnchorRange() {
	s.pvrSeedPlan(pvrPlanSpec{id: "plan_pvr_a7"})
	start := pvrDate(2027, 1, 10)
	create := func(anchor time.Time, workflow *types.TemporalWorkflowType, status types.SubscriptionStatus) (*dto.SubscriptionResponse, error) {
		return s.subSvc.CreateSubscription(s.GetContext(), dto.CreateSubscriptionRequest{
			CustomerID: s.cust.ID, PlanID: "plan_pvr_a7", Currency: "usd", StartDate: &start,
			BillingCadence: types.BILLING_CADENCE_RECURRING, BillingPeriod: types.BILLING_PERIOD_MONTHLY,
			BillingPeriodCount: 1, BillingCycle: types.BillingCycleAnniversary, Timezone: "UTC",
			BillingAnchor: &anchor, Workflow: workflow, SubscriptionStatus: status,
		})
	}

	cases := []struct {
		name   string
		anchor time.Time
		ok     bool
	}{
		{"anchor on start", start, true},
		{"anchor inside first period", pvrDate(2027, 1, 20), true},
		{"anchor one period after start", pvrDate(2027, 2, 10), true},
		{"anchor more than one period ahead", pvrDate(2027, 3, 5), false},
		{"anchor before start", pvrDate(2026, 12, 20), true},
		{"anchor months before start", pvrDate(2026, 3, 1), true},
	}
	for _, tc := range cases {
		s.Run(tc.name, func() {
			_, err := create(tc.anchor, nil, "")
			s.Equal(tc.ok, err == nil, "anchor %s: err=%v", tc.anchor.Format(time.DateOnly), err)
		})
	}

	s.Run("stripe import keeps an out-of-range anchor", func() {
		anchor := pvrDate(2026, 12, 20)
		resp, err := create(anchor, lo.ToPtr(types.TemporalStripeIntegrationWorkflow), "")
		s.Require().NoError(err)
		sub := s.pvrReload(resp.ID)
		s.pvrExpectTime("A7 stripe anchor kept", anchor, sub.BillingAnchor)
		s.pvrExpectTime("A7 stripe first period end", pvrDate(2027, 1, 20), sub.CurrentPeriodEnd)
	})

	s.Run("draft activation keeps a custom anchor still in range", func() {
		resp, err := create(pvrDate(2027, 1, 20), nil, types.SubscriptionStatusDraft)
		s.Require().NoError(err)
		_, err = s.subSvc.ActivateDraftSubscription(s.GetContext(), resp.ID, dto.ActivateDraftSubscriptionRequest{StartDate: lo.ToPtr(pvrDate(2027, 1, 12))})
		s.Require().NoError(err)
		sub := s.pvrReload(resp.ID)
		s.pvrExpectTime("A7 draft anchor kept", pvrDate(2027, 1, 20), sub.BillingAnchor)
		s.pvrExpectTime("A7 draft first period end", pvrDate(2027, 1, 20), sub.CurrentPeriodEnd)
	})

	s.Run("draft activation moves an out-of-range anchor with the start", func() {
		resp, err := create(pvrDate(2027, 1, 20), nil, types.SubscriptionStatusDraft)
		s.Require().NoError(err)
		_, err = s.subSvc.ActivateDraftSubscription(s.GetContext(), resp.ID, dto.ActivateDraftSubscriptionRequest{StartDate: lo.ToPtr(pvrDate(2027, 1, 25))})
		s.Require().NoError(err)
		sub := s.pvrReload(resp.ID)
		s.pvrExpectTime("A7 draft anchor shifted by 15 days", pvrDate(2027, 2, 4), sub.BillingAnchor)
		s.pvrExpectTime("A7 draft first period end", pvrDate(2027, 2, 4), sub.CurrentPeriodEnd)
	})
}

// An immediate cancel of a subscription with only one-time charges credits nothing and does not fail.
func (s *ProrationScenarioSuite) TestCancelWithOnlyOnetimeItems() {
	ctx := s.GetContext()
	st := s.GetStores()
	planID := "plan_pvr_onetime"
	s.Require().NoError(st.PlanRepo.Create(ctx, &plan.Plan{ID: planID, Name: planID, BaseModel: types.GetDefaultBaseModel(ctx)}))
	pr := &price.Price{
		ID: "price_pvr_onetime", Amount: decimal.NewFromInt(500), Currency: "usd",
		EntityType: types.PRICE_ENTITY_TYPE_PLAN, EntityID: planID, Type: types.PRICE_TYPE_FIXED,
		BillingPeriod: types.BILLING_PERIOD_ONETIME, BillingPeriodCount: 1, BillingModel: types.BILLING_MODEL_FLAT_FEE,
		BillingCadence: types.BILLING_CADENCE_RECURRING, InvoiceCadence: types.InvoiceCadenceAdvance,
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.Require().NoError(st.PriceRepo.Create(ctx, pr))

	start := pvrDate(2027, 1, 1)
	sub := &subscription.Subscription{
		ID: "sub_pvr_onetime", PlanID: planID, CustomerID: s.cust.ID,
		StartDate: start, BillingAnchor: start, CurrentPeriodStart: start, CurrentPeriodEnd: pvrDate(2027, 2, 1),
		Currency: "usd", BillingPeriod: types.BILLING_PERIOD_MONTHLY, BillingPeriodCount: 1,
		BillingCycle: types.BillingCycleAnniversary, SubscriptionStatus: types.SubscriptionStatusActive,
		Timezone: "UTC", ProrationBehavior: types.ProrationBehaviorCreateProrations, BaseModel: types.GetDefaultBaseModel(ctx),
	}
	li := &subscription.SubscriptionLineItem{
		ID: "li_pvr_onetime", SubscriptionID: sub.ID, CustomerID: s.cust.ID,
		EntityID: planID, EntityType: types.SubscriptionLineItemEntityTypePlan, PriceID: pr.ID, PriceType: types.PRICE_TYPE_FIXED,
		DisplayName: "setup", Quantity: decimal.NewFromInt(1), Currency: "usd",
		BillingPeriod: types.BILLING_PERIOD_ONETIME, BillingPeriodCount: 1,
		InvoiceCadence: types.InvoiceCadenceAdvance, StartDate: start, BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.Require().NoError(st.SubscriptionRepo.CreateWithLineItems(ctx, sub, []*subscription.SubscriptionLineItem{li}))

	res, err := NewProrationService(s.params).CalculateSubscriptionCancellationProration(
		ctx, sub, []*subscription.SubscriptionLineItem{li}, types.CancellationTypeImmediate, pvrDate(2027, 1, 20), "test",
		types.ProrationBehaviorCreateProrations)
	s.Require().NoError(err)
	s.True(res.TotalProrationAmount.IsZero(), "one-time charges are never credited")
}

// J4: an addon dated before the subscription start is accepted at creation, as before.
func (s *ProrationScenarioSuite) TestAddonBeforeSubscriptionStart() {
	s.pvrSeedPlan(pvrPlanSpec{id: "plan_pvr_j4"})
	s.pvrSeedAddon(pvrAddonSpec{id: "addon_pvr_j4", fixedPrice: true})
	start := pvrDate(2027, 1, 15)
	_, err := s.subSvc.CreateSubscription(s.GetContext(), dto.CreateSubscriptionRequest{
		CustomerID: s.cust.ID, PlanID: "plan_pvr_j4", Currency: "usd", StartDate: &start,
		BillingCadence: types.BILLING_CADENCE_RECURRING, BillingPeriod: types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1, BillingCycle: types.BillingCycleCalendar, Timezone: "UTC",
		ProrationBehavior: types.ProrationBehaviorCreateProrations,
		SubscriptionCreationConfig: dto.SubscriptionCreationConfig{
			Addons: []dto.AddAddonToSubscriptionRequest{pvrAddReq("addon_pvr_j4", pvrDate(2027, 1, 10), types.ProrationBehaviorCreateProrations)},
		},
	})
	s.NoError(err)
}

// J5: system rollouts (e.g. prepare_processed_events) may still backdate a line item.
func (s *ProrationScenarioSuite) TestSystemRolloutMayBackdateLineItem() {
	s.pvrSeedPlan(pvrPlanSpec{id: "plan_pvr_j5s"})
	sub := s.pvrCreateSub(pvrSubSpec{
		planID: "plan_pvr_j5s", start: pvrDate(2027, 1, 1), cycle: types.BillingCycleAnniversary,
		behavior: types.ProrationBehaviorCreateProrations,
	})
	sub.CurrentPeriodStart, sub.CurrentPeriodEnd = pvrDate(2027, 2, 1), pvrDate(2027, 3, 1)
	s.Require().NoError(s.GetStores().SubscriptionRepo.Update(s.GetContext(), sub))

	at := pvrDate(2027, 1, 20)
	_, err := s.subSvc.AddSubscriptionLineItem(s.GetContext(), sub.ID, dto.CreateSubscriptionLineItemRequest{
		PriceID: "price_plan_pvr_j5s", StartDate: &at, SkipEntitlementCheck: true,
	})
	s.NoError(err)
}

// pvrPlanPrice adds a fixed advance price to a plan.
func (s *ProrationScenarioSuite) pvrPlanPrice(planID string, period types.BillingPeriod, count int, amount int64) string {
	ctx := s.GetContext()
	id := types.GenerateUUIDWithPrefix(types.UUID_PREFIX_PRICE)
	s.Require().NoError(s.GetStores().PriceRepo.Create(ctx, &price.Price{
		ID: id, Amount: decimal.NewFromInt(amount), Currency: "usd",
		EntityType: types.PRICE_ENTITY_TYPE_PLAN, EntityID: planID, Type: types.PRICE_TYPE_FIXED,
		BillingCadence: types.BILLING_CADENCE_RECURRING, BillingPeriod: period, BillingPeriodCount: count,
		BillingModel: types.BILLING_MODEL_FLAT_FEE, InvoiceCadence: types.InvoiceCadenceAdvance,
		BaseModel: types.GetDefaultBaseModel(ctx),
	}))
	return id
}

// E18/E19: a $365 annual addon on a calendar monthly sub from Jan 15 2027 runs on the Jan 1 grid, so
// attach and removal charge or credit against its own year, e.g. attach Mar 10 → [Mar 10, Jan 1) = 297/365.
func (s *ProrationScenarioSuite) TestLongerCadenceAddon_AttachAndRemove() {
	start := pvrDate(2027, 1, 15)
	removeJan20 := pvrDate(2027, 1, 20)

	tests := []struct {
		name                   string
		atCreation             bool
		periodStart, periodEnd time.Time // current period before the change; zero keeps [Jan 15, Feb 1)
		attachAt               time.Time
		removeAt               *time.Time // nil removes with no change_at (current period end)
		remove                 bool
		wantCharge, wantCredit string
	}{
		{name: "attach mid-period charges the rest of the item year", periodStart: pvrDate(2027, 3, 1), periodEnd: pvrDate(2027, 4, 1),
			attachAt: pvrDate(2027, 3, 10), wantCharge: "297.00", wantCredit: "0"},
		{name: "remove immediately credits the rest of the item year", atCreation: true, remove: true, removeAt: &removeJan20,
			wantCharge: "0", wantCredit: "346.00"},
		{name: "remove at period end credits the item year after Feb 1", atCreation: true, remove: true,
			wantCharge: "0", wantCredit: "334.00"},
		// The period ends with the item year: nothing billed is left, and the next year was never billed.
		{name: "remove at period end on the item boundary credits nothing", atCreation: true, remove: true,
			periodStart: pvrDate(2027, 12, 1), periodEnd: pvrDate(2028, 1, 1), wantCharge: "0", wantCredit: "0"},
	}

	for i, tt := range tests {
		s.Run(tt.name, func() {
			planID := fmt.Sprintf("plan_pvr_e18_%d", i)
			addonID := fmt.Sprintf("addon_pvr_e18_%d", i)
			s.pvrSeedPlan(pvrPlanSpec{id: planID})
			s.pvrSeedAddon(pvrAddonSpec{id: addonID, fixedPrice: true, period: types.BILLING_PERIOD_ANNUAL, amount: 365})
			spec := pvrSubSpec{planID: planID, start: start, cycle: types.BillingCycleCalendar, behavior: types.ProrationBehaviorCreateProrations}
			if tt.atCreation {
				spec.addons = []dto.AddAddonToSubscriptionRequest{pvrAddReq(addonID, start, types.ProrationBehaviorCreateProrations)}
			}
			sub := s.pvrCreateSub(spec)
			if tt.atCreation {
				_, per := s.pvrFixed(sub, sub.CurrentPeriodStart, sub.CurrentPeriodEnd)
				s.pvrExpect("opening addon charge [Jan 15, Jan 1) = 351/365", "351.00", per[s.pvrLineItem(sub, types.SubscriptionLineItemEntityTypeAddon).ID])
			}
			if !tt.periodStart.IsZero() {
				sub.CurrentPeriodStart, sub.CurrentPeriodEnd = tt.periodStart, tt.periodEnd
				s.Require().NoError(s.GetStores().SubscriptionRepo.Update(s.GetContext(), sub))
			}

			params := &dto.SubModifyBulkAddonParams{}
			if !tt.attachAt.IsZero() {
				add := pvrAddReq(addonID, tt.attachAt, types.ProrationBehaviorCreateProrations)
				params.Adds = []*dto.AddAddonToSubscriptionRequest{&add}
			}
			if tt.remove {
				params.Removes = []*dto.RemoveAddonRequest{{
					AddonAssociationID: s.pvrAssociation(sub.ID).ID, ProrationBehavior: types.ProrationBehaviorCreateProrations,
					EffectiveDate: tt.removeAt,
				}}
			}
			quote := s.pvrPreview(sub.ID, params)
			s.pvrExpect("charge", tt.wantCharge, quote.TotalChargeAmount)
			s.pvrExpect("credit", tt.wantCredit, quote.TotalCreditAmount)
		})
	}
}

// E18 via include_price_ids at creation and via the line item API: an annual plan price on a calendar
// monthly sub from Jan 15 2027 bills its own stub, [Jan 15, Jan 1) = 351 or [Mar 10, Jan 1) = 297.
func (s *ProrationScenarioSuite) TestLongerCadencePlanPrice_EntryPoints() {
	ctx := s.GetContext()
	s.pvrSeedPlan(pvrPlanSpec{id: "plan_pvr_e18p"})
	annualID := s.pvrPlanPrice("plan_pvr_e18p", types.BILLING_PERIOD_ANNUAL, 1, 365)

	s.Run("include_price_ids at creation", func() {
		sub := s.pvrCreateSub(pvrSubSpec{planID: "plan_pvr_e18p", start: pvrDate(2027, 1, 15), cycle: types.BillingCycleCalendar,
			behavior: types.ProrationBehaviorCreateProrations, include: &[]string{"price_plan_pvr_e18p", annualID}})
		items, err := s.GetStores().SubscriptionLineItemRepo.ListBySubscription(ctx, sub)
		s.Require().NoError(err)
		annual, found := lo.Find(items, func(li *subscription.SubscriptionLineItem) bool { return li.PriceID == annualID })
		s.Require().True(found)
		_, per := s.pvrFixed(sub, sub.CurrentPeriodStart, sub.CurrentPeriodEnd)
		s.pvrExpect("annual plan line on the opening invoice", "351.00", per[annual.ID])
	})

	s.Run("line item API mid-period", func() {
		sub := s.pvrCreateSub(pvrSubSpec{planID: "plan_pvr_e18p", start: pvrDate(2027, 1, 15), cycle: types.BillingCycleCalendar,
			behavior: types.ProrationBehaviorCreateProrations, include: &[]string{"price_plan_pvr_e18p"}})
		sub.CurrentPeriodStart, sub.CurrentPeriodEnd = pvrDate(2027, 3, 1), pvrDate(2027, 4, 1)
		s.Require().NoError(s.GetStores().SubscriptionRepo.Update(ctx, sub))

		at := pvrDate(2027, 3, 10)
		resp, err := s.subSvc.AddSubscriptionLineItem(ctx, sub.ID, dto.CreateSubscriptionLineItemRequest{
			PriceID: annualID, Quantity: decimal.NewFromInt(1), StartDate: &at, SkipEntitlementCheck: true,
			ProrationBehavior: types.ProrationBehaviorCreateProrations,
		})
		s.Require().NoError(err)
		s.Equal(types.BILLING_PERIOD_ANNUAL, resp.SubscriptionLineItem.BillingPeriod)

		invoices, err := s.GetStores().InvoiceRepo.List(ctx, &types.InvoiceFilter{QueryFilter: types.NewNoLimitQueryFilter(), SubscriptionID: sub.ID})
		s.Require().NoError(err)
		oneOff, found := lo.Find(invoices, func(inv *invoice.Invoice) bool { return inv.InvoiceType == types.InvoiceTypeOneOff })
		s.Require().True(found, "expected a ONE_OFF attach invoice")
		s.pvrExpect("line item API attach charge", "297.00", oneOff.AmountDue)
	})
}

// E13/G30: plan change v2 on Jan 20 credits each outgoing item and charges each incoming one on its own
// cadence. Monthly sub: 12/31 x 31 + 71/90 x 90 out, 12/31 x 62 + 71/90 x 180 in (3-month price).
// Quarterly sub: 71/90 x 90 + (12/31 x 31 + 31 + 31) out, 71/90 x 180 in.
func (s *ProrationScenarioSuite) TestPlanChangeV2_MixedCadences() {
	type pvrPrice struct {
		period types.BillingPeriod
		count  int
		amount int64
	}
	tests := []struct {
		name                   string
		subPeriod              types.BillingPeriod
		from, to               []pvrPrice
		wantCredit, wantCharge string
	}{
		{name: "longer items on a monthly sub", subPeriod: types.BILLING_PERIOD_MONTHLY,
			from:       []pvrPrice{{types.BILLING_PERIOD_MONTHLY, 1, 31}, {types.BILLING_PERIOD_QUARTER, 1, 90}},
			to:         []pvrPrice{{types.BILLING_PERIOD_MONTHLY, 1, 62}, {types.BILLING_PERIOD_MONTHLY, 3, 180}},
			wantCredit: "83.00", wantCharge: "166.00"},
		{name: "shorter item on a quarterly sub", subPeriod: types.BILLING_PERIOD_QUARTER,
			from:       []pvrPrice{{types.BILLING_PERIOD_QUARTER, 1, 90}, {types.BILLING_PERIOD_MONTHLY, 1, 31}},
			to:         []pvrPrice{{types.BILLING_PERIOD_QUARTER, 1, 180}},
			wantCredit: "145.00", wantCharge: "142.00"},
	}

	for i, tt := range tests {
		s.Run(tt.name, func() {
			ctx := s.GetContext()
			fromID, toID := fmt.Sprintf("plan_pvr_v2_from_%d", i), fmt.Sprintf("plan_pvr_v2_to_%d", i)
			include := []string{}
			for _, id := range []string{fromID, toID} {
				s.Require().NoError(s.GetStores().PlanRepo.Create(ctx, &plan.Plan{ID: id, Name: id, BaseModel: types.GetDefaultBaseModel(ctx)}))
			}
			for _, p := range tt.from {
				include = append(include, s.pvrPlanPrice(fromID, p.period, p.count, p.amount))
			}
			for _, p := range tt.to {
				s.pvrPlanPrice(toID, p.period, p.count, p.amount)
			}
			created := s.pvrCreateSub(pvrSubSpec{planID: fromID, start: pvrDate(2027, 1, 1), cycle: types.BillingCycleAnniversary,
				behavior: types.ProrationBehaviorCreateProrations, period: tt.subPeriod, include: &include})

			svc := s.subSvc.(*subscriptionService)
			sub, err := svc.loadSubscriptionForChange(ctx, created.ID, false)
			s.Require().NoError(err)
			r, err := svc.resolvePlanChange(ctx, sub, dto.SubscriptionChangeV2Request{
				TargetPlanID: toID, ProrationBehavior: types.ProrationBehaviorCreateProrations,
			}, pvrDate(2027, 1, 20))
			s.Require().NoError(err)
			s.Require().NotNil(r.settlementQuote)
			s.pvrExpect("plan change credit", tt.wantCredit, r.settlementQuote.TotalCreditAmount)
			s.pvrExpect("plan change charge", tt.wantCharge, r.settlementQuote.TotalChargeAmount)
		})
	}
}
