package service

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/plan"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/suite"
)

// Ensure decimal is used to avoid unused import errors; later test methods will use it directly.
var _ = decimal.Zero

type FixedChargeBillingSuite struct {
	testutil.BaseServiceTestSuite
	service BillingService
}

func TestFixedChargeBilling(t *testing.T) {
	suite.Run(t, new(FixedChargeBillingSuite))
}

func (s *FixedChargeBillingSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
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

func (s *FixedChargeBillingSuite) TearDownTest() {
	s.BaseServiceTestSuite.TearDownTest()
}

// seedCustomerAndPlan creates an Aruba customer and a plan, returns both.
func (s *FixedChargeBillingSuite) seedCustomerAndPlan(custID, planID string) (*customer.Customer, *plan.Plan) {
	ctx := s.GetContext()
	cust := &customer.Customer{
		ID:         custID,
		ExternalID: "ext_" + custID,
		Name:       "Aruba",
		Email:      "aruba@example.com",
		BaseModel:  types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().CustomerRepo.Create(ctx, cust))

	pl := &plan.Plan{
		ID:        planID,
		Name:      "Aruba Plan " + planID,
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().PlanRepo.Create(ctx, pl))
	return cust, pl
}

// seedPrice creates a price in PriceRepo and returns it.
func (s *FixedChargeBillingSuite) seedPrice(p *price.Price) *price.Price {
	s.NoError(s.GetStores().PriceRepo.Create(s.GetContext(), p))
	return p
}

// seedSubscriptionWithLineItem creates a subscription and one line item, returns the subscription
// (with LineItems populated in memory).
func (s *FixedChargeBillingSuite) seedSubscriptionWithLineItem(
	sub *subscription.Subscription,
	li *subscription.SubscriptionLineItem,
) *subscription.Subscription {
	ctx := s.GetContext()
	s.NoError(s.GetStores().SubscriptionRepo.CreateWithLineItems(ctx, sub, []*subscription.SubscriptionLineItem{li}))
	sub.LineItems = []*subscription.SubscriptionLineItem{li}
	return sub
}

// seedSubscriptionWithLineItems creates a subscription and multiple line items.
func (s *FixedChargeBillingSuite) seedSubscriptionWithLineItems(
	sub *subscription.Subscription,
	lis []*subscription.SubscriptionLineItem,
) *subscription.Subscription {
	ctx := s.GetContext()
	s.NoError(s.GetStores().SubscriptionRepo.CreateWithLineItems(ctx, sub, lis))
	sub.LineItems = lis
	return sub
}

// fixedPeriod returns a standard monthly period: Jan 1 – Feb 1 2025.
func fixedPeriod() (time.Time, time.Time) {
	start := time.Date(2025, time.January, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, time.February, 1, 0, 0, 0, 0, time.UTC)
	return start, end
}

func (s *FixedChargeBillingSuite) TestFlatFee_Advance_Monthly() {
	ctx := s.GetContext()
	periodStart, periodEnd := fixedPeriod()

	_, pl := s.seedCustomerAndPlan("cust_ff_adv", "plan_ff_adv")

	p := s.seedPrice(&price.Price{
		ID:                 "price_ff_adv",
		Amount:             decimal.NewFromInt(100),
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
	})

	sub := &subscription.Subscription{
		ID:                 "sub_ff_adv",
		PlanID:             pl.ID,
		CustomerID:         "cust_ff_adv",
		StartDate:          periodStart,
		BillingAnchor:      periodStart,
		CurrentPeriodStart: periodStart,
		CurrentPeriodEnd:   periodEnd,
		Currency:           "usd",
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		SubscriptionStatus: types.SubscriptionStatusActive,
		CustomerTimezone:   "UTC",
		ProrationBehavior:  types.ProrationBehaviorNone,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	li := &subscription.SubscriptionLineItem{
		ID:                 types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION_LINE_ITEM),
		SubscriptionID:     sub.ID,
		CustomerID:         sub.CustomerID,
		EntityID:           pl.ID,
		EntityType:         types.SubscriptionLineItemEntityTypePlan,
		PriceID:            p.ID,
		PriceType:          types.PRICE_TYPE_FIXED,
		DisplayName:        "Flat Fee",
		Quantity:           decimal.NewFromInt(3),
		Currency:           "usd",
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		InvoiceCadence:     types.InvoiceCadenceAdvance,
		StartDate:          periodStart,
		EndDate:            periodEnd,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	s.seedSubscriptionWithLineItem(sub, li)

	result, err := s.service.CalculateFixedCharges(ctx, &dto.CalculateFixedChargesParams{
		Subscription: sub,
		PeriodStart:  periodStart,
		PeriodEnd:    periodEnd,
	})
	s.NoError(err)
	s.Require().Len(result.LineItems, 1, "expected 1 line item for flat fee advance")
	s.True(result.LineItems[0].Amount.Equal(decimal.NewFromInt(300)),
		"expected $100 × 3 = $300, got %s", result.LineItems[0].Amount)
	s.True(result.TotalAmount.Equal(decimal.NewFromInt(300)))
}
