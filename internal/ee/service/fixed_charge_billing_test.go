package service

import (
	"fmt"
	"math/rand"
	"os"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/plan"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/suite"
)

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
		Timezone:           "UTC",
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

func (s *FixedChargeBillingSuite) TestFlatFee_Arrear_Monthly() {
	ctx := s.GetContext()
	periodStart, periodEnd := fixedPeriod()

	_, pl := s.seedCustomerAndPlan("cust_ff_arr", "plan_ff_arr")

	p := s.seedPrice(&price.Price{
		ID:                 "price_ff_arr",
		Amount:             decimal.NewFromInt(100),
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
	})

	sub := &subscription.Subscription{
		ID:                 "sub_ff_arr",
		PlanID:             pl.ID,
		CustomerID:         "cust_ff_arr",
		StartDate:          periodStart,
		BillingAnchor:      periodStart,
		CurrentPeriodStart: periodStart,
		CurrentPeriodEnd:   periodEnd,
		Currency:           "usd",
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		SubscriptionStatus: types.SubscriptionStatusActive,
		Timezone:           "UTC",
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
		DisplayName:        "Flat Fee Arrear",
		Quantity:           decimal.NewFromInt(3),
		Currency:           "usd",
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		InvoiceCadence:     types.InvoiceCadenceArrear,
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
	s.Require().Len(result.LineItems, 1, "expected 1 arrear line item")
	s.True(result.LineItems[0].Amount.Equal(decimal.NewFromInt(300)),
		"expected $100 × 3 = $300 for arrear, got %s", result.LineItems[0].Amount)
	s.True(result.TotalAmount.Equal(decimal.NewFromInt(300)))
}

func (s *FixedChargeBillingSuite) TestPackage_Advance_Monthly() {
	ctx := s.GetContext()
	periodStart, periodEnd := fixedPeriod()

	_, pl := s.seedCustomerAndPlan("cust_pkg_adv", "plan_pkg_adv")

	// $50 per package of 10 units; quantity=25 → ceil(25/10)=3 packages → $150
	p := s.seedPrice(&price.Price{
		ID:                 "price_pkg_adv",
		Amount:             decimal.NewFromInt(50),
		Currency:           "usd",
		EntityType:         types.PRICE_ENTITY_TYPE_PLAN,
		EntityID:           pl.ID,
		Type:               types.PRICE_TYPE_FIXED,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_PACKAGE,
		BillingCadence:     types.BILLING_CADENCE_RECURRING,
		InvoiceCadence:     types.InvoiceCadenceAdvance,
		TransformQuantity: price.JSONBTransformQuantity{
			DivideBy: 10,
			Round:    types.ROUND_UP,
		},
		BaseModel: types.GetDefaultBaseModel(ctx),
	})

	sub := &subscription.Subscription{
		ID:                 "sub_pkg_adv",
		PlanID:             pl.ID,
		CustomerID:         "cust_pkg_adv",
		StartDate:          periodStart,
		BillingAnchor:      periodStart,
		CurrentPeriodStart: periodStart,
		CurrentPeriodEnd:   periodEnd,
		Currency:           "usd",
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		SubscriptionStatus: types.SubscriptionStatusActive,
		Timezone:           "UTC",
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
		DisplayName:        "Package Fee",
		Quantity:           decimal.NewFromInt(25),
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
	s.Require().Len(result.LineItems, 1, "expected 1 package line item")
	// ceil(25/10) = 3 packages × $50 = $150
	s.True(result.LineItems[0].Amount.Equal(decimal.NewFromInt(150)),
		"expected ceil(25/10)=3 packages × $50 = $150, got %s", result.LineItems[0].Amount)
	s.True(result.TotalAmount.Equal(decimal.NewFromInt(150)))
}

func (s *FixedChargeBillingSuite) TestPackage_Arrear_Monthly() {
	ctx := s.GetContext()
	periodStart, periodEnd := fixedPeriod()

	_, pl := s.seedCustomerAndPlan("cust_pkg_arr", "plan_pkg_arr")

	p := s.seedPrice(&price.Price{
		ID:                 "price_pkg_arr",
		Amount:             decimal.NewFromInt(50),
		Currency:           "usd",
		EntityType:         types.PRICE_ENTITY_TYPE_PLAN,
		EntityID:           pl.ID,
		Type:               types.PRICE_TYPE_FIXED,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_PACKAGE,
		BillingCadence:     types.BILLING_CADENCE_RECURRING,
		InvoiceCadence:     types.InvoiceCadenceArrear,
		TransformQuantity: price.JSONBTransformQuantity{
			DivideBy: 10,
			Round:    types.ROUND_UP,
		},
		BaseModel: types.GetDefaultBaseModel(ctx),
	})

	sub := &subscription.Subscription{
		ID:                 "sub_pkg_arr",
		PlanID:             pl.ID,
		CustomerID:         "cust_pkg_arr",
		StartDate:          periodStart,
		BillingAnchor:      periodStart,
		CurrentPeriodStart: periodStart,
		CurrentPeriodEnd:   periodEnd,
		Currency:           "usd",
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		SubscriptionStatus: types.SubscriptionStatusActive,
		Timezone:           "UTC",
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
		DisplayName:        "Package Fee Arrear",
		Quantity:           decimal.NewFromInt(25),
		Currency:           "usd",
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		InvoiceCadence:     types.InvoiceCadenceArrear,
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
	s.Require().Len(result.LineItems, 1, "expected 1 arrear package line item")
	s.True(result.LineItems[0].Amount.Equal(decimal.NewFromInt(150)),
		"expected $150 for arrear package, got %s", result.LineItems[0].Amount)
	s.True(result.TotalAmount.Equal(decimal.NewFromInt(150)))
}

func (s *FixedChargeBillingSuite) TestTieredSlab_Advance_Monthly() {
	ctx := s.GetContext()
	periodStart, periodEnd := fixedPeriod()

	_, pl := s.seedCustomerAndPlan("cust_slab_adv", "plan_slab_adv")

	upTo10 := uint64(10)
	upTo50 := uint64(50)
	// Tiers: 0–10 @ $5/unit, 11–50 @ $3/unit; quantity=20
	// SLAB: (10×$5) + (10×$3) = $50 + $30 = $80
	p := s.seedPrice(&price.Price{
		ID:                 "price_slab_adv",
		Amount:             decimal.Zero,
		Currency:           "usd",
		EntityType:         types.PRICE_ENTITY_TYPE_PLAN,
		EntityID:           pl.ID,
		Type:               types.PRICE_TYPE_FIXED,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_TIERED,
		TierMode:           types.BILLING_TIER_SLAB,
		BillingCadence:     types.BILLING_CADENCE_RECURRING,
		InvoiceCadence:     types.InvoiceCadenceAdvance,
		Tiers: price.JSONBTiers{
			{UpTo: &upTo10, UnitAmount: decimal.NewFromInt(5)},
			{UpTo: &upTo50, UnitAmount: decimal.NewFromInt(3)},
		},
		BaseModel: types.GetDefaultBaseModel(ctx),
	})

	sub := &subscription.Subscription{
		ID:                 "sub_slab_adv",
		PlanID:             pl.ID,
		CustomerID:         "cust_slab_adv",
		StartDate:          periodStart,
		BillingAnchor:      periodStart,
		CurrentPeriodStart: periodStart,
		CurrentPeriodEnd:   periodEnd,
		Currency:           "usd",
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		SubscriptionStatus: types.SubscriptionStatusActive,
		Timezone:           "UTC",
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
		DisplayName:        "Tiered Slab Fee",
		Quantity:           decimal.NewFromInt(20),
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
	s.Require().Len(result.LineItems, 1, "expected 1 tiered slab line item")
	// (10×$5) + (10×$3) = $80
	s.True(result.LineItems[0].Amount.Equal(decimal.NewFromInt(80)),
		"expected SLAB (10×$5)+(10×$3)=$80, got %s", result.LineItems[0].Amount)
	s.True(result.TotalAmount.Equal(decimal.NewFromInt(80)))
}

func (s *FixedChargeBillingSuite) TestTieredVolume_Advance_Monthly() {
	ctx := s.GetContext()
	periodStart, periodEnd := fixedPeriod()

	_, pl := s.seedCustomerAndPlan("cust_vol_adv", "plan_vol_adv")

	upTo10 := uint64(10)
	upTo50 := uint64(50)
	// VOLUME: all 20 units priced at matching tier → tier 2 ($3) → 20×$3 = $60
	p := s.seedPrice(&price.Price{
		ID:                 "price_vol_adv",
		Amount:             decimal.Zero,
		Currency:           "usd",
		EntityType:         types.PRICE_ENTITY_TYPE_PLAN,
		EntityID:           pl.ID,
		Type:               types.PRICE_TYPE_FIXED,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_TIERED,
		TierMode:           types.BILLING_TIER_VOLUME,
		BillingCadence:     types.BILLING_CADENCE_RECURRING,
		InvoiceCadence:     types.InvoiceCadenceAdvance,
		Tiers: price.JSONBTiers{
			{UpTo: &upTo10, UnitAmount: decimal.NewFromInt(5)},
			{UpTo: &upTo50, UnitAmount: decimal.NewFromInt(3)},
		},
		BaseModel: types.GetDefaultBaseModel(ctx),
	})

	sub := &subscription.Subscription{
		ID:                 "sub_vol_adv",
		PlanID:             pl.ID,
		CustomerID:         "cust_vol_adv",
		StartDate:          periodStart,
		BillingAnchor:      periodStart,
		CurrentPeriodStart: periodStart,
		CurrentPeriodEnd:   periodEnd,
		Currency:           "usd",
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		SubscriptionStatus: types.SubscriptionStatusActive,
		Timezone:           "UTC",
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
		DisplayName:        "Tiered Volume Fee",
		Quantity:           decimal.NewFromInt(20),
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
	s.Require().Len(result.LineItems, 1, "expected 1 tiered volume line item")
	// VOLUME: all 20 units at tier-2 rate $3 → $60
	s.True(result.LineItems[0].Amount.Equal(decimal.NewFromInt(60)),
		"expected VOLUME 20×$3=$60, got %s", result.LineItems[0].Amount)
	s.True(result.TotalAmount.Equal(decimal.NewFromInt(60)))
}

func (s *FixedChargeBillingSuite) TestFlatFee_Annual_Advance() {
	ctx := s.GetContext()
	periodStart := time.Date(2025, time.January, 1, 0, 0, 0, 0, time.UTC)
	periodEnd := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)

	_, pl := s.seedCustomerAndPlan("cust_ann_adv", "plan_ann_adv")

	p := s.seedPrice(&price.Price{
		ID:                 "price_ann_adv",
		Amount:             decimal.NewFromInt(1200),
		Currency:           "usd",
		EntityType:         types.PRICE_ENTITY_TYPE_PLAN,
		EntityID:           pl.ID,
		Type:               types.PRICE_TYPE_FIXED,
		BillingPeriod:      types.BILLING_PERIOD_ANNUAL,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_FLAT_FEE,
		BillingCadence:     types.BILLING_CADENCE_RECURRING,
		InvoiceCadence:     types.InvoiceCadenceAdvance,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	})

	sub := &subscription.Subscription{
		ID:                 "sub_ann_adv",
		PlanID:             pl.ID,
		CustomerID:         "cust_ann_adv",
		StartDate:          periodStart,
		BillingAnchor:      periodStart,
		CurrentPeriodStart: periodStart,
		CurrentPeriodEnd:   periodEnd,
		Currency:           "usd",
		BillingPeriod:      types.BILLING_PERIOD_ANNUAL,
		BillingPeriodCount: 1,
		SubscriptionStatus: types.SubscriptionStatusActive,
		Timezone:           "UTC",
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
		DisplayName:        "Annual Fee",
		Quantity:           decimal.NewFromInt(1),
		Currency:           "usd",
		BillingPeriod:      types.BILLING_PERIOD_ANNUAL,
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
	s.Require().Len(result.LineItems, 1, "expected 1 annual line item")
	s.True(result.LineItems[0].Amount.Equal(decimal.NewFromInt(1200)),
		"expected full annual amount $1200, got %s", result.LineItems[0].Amount)
	s.True(result.TotalAmount.Equal(decimal.NewFromInt(1200)))
}

func (s *FixedChargeBillingSuite) TestMixedPlan_FlatFeeAndTieredSlab_Advance() {
	ctx := s.GetContext()
	periodStart, periodEnd := fixedPeriod()

	_, pl := s.seedCustomerAndPlan("cust_mix", "plan_mix")

	// Price 1: FLAT_FEE $100 × 1 = $100
	pFlat := s.seedPrice(&price.Price{
		ID:                 "price_mix_flat",
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

	upTo10 := uint64(10)
	upTo50 := uint64(50)
	// Price 2: TIERED SLAB, quantity=20 → (10×$5)+(10×$3) = $80
	pTiered := s.seedPrice(&price.Price{
		ID:                 "price_mix_tiered",
		Amount:             decimal.Zero,
		Currency:           "usd",
		EntityType:         types.PRICE_ENTITY_TYPE_PLAN,
		EntityID:           pl.ID,
		Type:               types.PRICE_TYPE_FIXED,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_TIERED,
		TierMode:           types.BILLING_TIER_SLAB,
		BillingCadence:     types.BILLING_CADENCE_RECURRING,
		InvoiceCadence:     types.InvoiceCadenceAdvance,
		Tiers: price.JSONBTiers{
			{UpTo: &upTo10, UnitAmount: decimal.NewFromInt(5)},
			{UpTo: &upTo50, UnitAmount: decimal.NewFromInt(3)},
		},
		BaseModel: types.GetDefaultBaseModel(ctx),
	})

	sub := &subscription.Subscription{
		ID:                 "sub_mix",
		PlanID:             pl.ID,
		CustomerID:         "cust_mix",
		StartDate:          periodStart,
		BillingAnchor:      periodStart,
		CurrentPeriodStart: periodStart,
		CurrentPeriodEnd:   periodEnd,
		Currency:           "usd",
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		SubscriptionStatus: types.SubscriptionStatusActive,
		Timezone:           "UTC",
		ProrationBehavior:  types.ProrationBehaviorNone,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	liFlat := &subscription.SubscriptionLineItem{
		ID:                 types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION_LINE_ITEM),
		SubscriptionID:     sub.ID,
		CustomerID:         sub.CustomerID,
		EntityID:           pl.ID,
		EntityType:         types.SubscriptionLineItemEntityTypePlan,
		PriceID:            pFlat.ID,
		PriceType:          types.PRICE_TYPE_FIXED,
		DisplayName:        "Flat Fee",
		Quantity:           decimal.NewFromInt(1),
		Currency:           "usd",
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		InvoiceCadence:     types.InvoiceCadenceAdvance,
		StartDate:          periodStart,
		EndDate:            periodEnd,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	liTiered := &subscription.SubscriptionLineItem{
		ID:                 types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION_LINE_ITEM),
		SubscriptionID:     sub.ID,
		CustomerID:         sub.CustomerID,
		EntityID:           pl.ID,
		EntityType:         types.SubscriptionLineItemEntityTypePlan,
		PriceID:            pTiered.ID,
		PriceType:          types.PRICE_TYPE_FIXED,
		DisplayName:        "Tiered Fee",
		Quantity:           decimal.NewFromInt(20),
		Currency:           "usd",
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		InvoiceCadence:     types.InvoiceCadenceAdvance,
		StartDate:          periodStart,
		EndDate:            periodEnd,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	s.seedSubscriptionWithLineItems(sub, []*subscription.SubscriptionLineItem{liFlat, liTiered})

	result, err := s.service.CalculateFixedCharges(ctx, &dto.CalculateFixedChargesParams{
		Subscription: sub,
		PeriodStart:  periodStart,
		PeriodEnd:    periodEnd,
	})
	s.NoError(err)
	s.Require().Len(result.LineItems, 2, "expected 2 line items for mixed plan")

	var flatAmt, tieredAmt decimal.Decimal
	for _, item := range result.LineItems {
		switch lo.FromPtr(item.PriceID) {
		case pFlat.ID:
			flatAmt = item.Amount
		case pTiered.ID:
			tieredAmt = item.Amount
		default:
			s.Fail("unexpected PriceID in line items: %s", lo.FromPtr(item.PriceID))
		}
	}
	s.True(flatAmt.Equal(decimal.NewFromInt(100)), "flat fee line item should be $100, got %s", flatAmt)
	s.True(tieredAmt.Equal(decimal.NewFromInt(80)), "tiered slab line item should be $80, got %s", tieredAmt)
	s.True(result.TotalAmount.Equal(decimal.NewFromInt(180)), "total should be $180, got %s", result.TotalAmount)
}

// FixedChargeProrationSuite covers proration of fixed charges by CalculateFixedCharges across
// calendar and anniversary stubs, later periods, timezones and proration behaviours.
type FixedChargeProrationSuite struct {
	testutil.BaseServiceTestSuite
	params  ServiceParams
	billing BillingService
}

func TestFixedChargeProration(t *testing.T) {
	suite.Run(t, new(FixedChargeProrationSuite))
}

func (s *FixedChargeProrationSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	stores := s.GetStores()
	s.params = ServiceParams{
		Logger:                   s.GetLogger(),
		Config:                   s.GetConfig(),
		DB:                       s.GetDB(),
		SubRepo:                  stores.SubscriptionRepo,
		SubscriptionLineItemRepo: stores.SubscriptionLineItemRepo,
		PlanRepo:                 stores.PlanRepo,
		PriceRepo:                stores.PriceRepo,
		PriceUnitRepo:            stores.PriceUnitRepo,
		EventRepo:                stores.EventRepo,
		MeterRepo:                stores.MeterRepo,
		CustomerRepo:             stores.CustomerRepo,
		InvoiceRepo:              stores.InvoiceRepo,
		InvoiceLineItemRepo:      stores.InvoiceLineItemRepo,
		EntitlementRepo:          stores.EntitlementRepo,
		EnvironmentRepo:          stores.EnvironmentRepo,
		FeatureRepo:              stores.FeatureRepo,
		TenantRepo:               stores.TenantRepo,
		UserRepo:                 stores.UserRepo,
		AuthRepo:                 stores.AuthRepo,
		WalletRepo:               stores.WalletRepo,
		PaymentRepo:              stores.PaymentRepo,
		CouponRepo:               stores.CouponRepo,
		CouponAssociationRepo:    stores.CouponAssociationRepo,
		CouponApplicationRepo:    stores.CouponApplicationRepo,
		AddonAssociationRepo:     stores.AddonAssociationRepo,
		TaxRateRepo:              stores.TaxRateRepo,
		TaxAssociationRepo:       stores.TaxAssociationRepo,
		TaxAppliedRepo:           stores.TaxAppliedRepo,
		SettingsRepo:             stores.SettingsRepo,
		EventPublisher:           s.GetPublisher(),
		WebhookPublisher:         s.GetWebhookPublisher(),
		ProrationCalculator:      s.GetCalculator(),
		AlertLogsRepo:            stores.AlertLogsRepo,
	}
	s.billing = NewBillingService(s.params)
}

func (s *FixedChargeProrationSuite) TearDownTest() {
	s.BaseServiceTestSuite.TearDownTest()
}

// pvAt returns local midnight of the given date in tz, as a UTC instant.
func pvAt(y int, m time.Month, day int, tz string) time.Time {
	loc, err := time.LoadLocation(tz)
	if err != nil {
		panic(err)
	}
	return time.Date(y, m, day, 0, 0, 0, 0, loc).UTC()
}

func pvUTC(y int, m time.Month, day int) time.Time { return pvAt(y, m, day, "UTC") }

type pvItemSpec struct {
	key        string
	period     types.BillingPeriod
	amount     string
	qty        int64
	model      types.BillingModel
	tierMode   types.BillingTier
	tiers      []price.PriceTier
	transform  *price.TransformQuantity
	start      time.Time // zero = sub start
	end        time.Time
	entityType types.SubscriptionLineItemEntityType
}

type pvSubSpec struct {
	cycle    types.BillingCycle
	period   types.BillingPeriod
	start    time.Time
	anchor   time.Time // zero = derive as production does
	tz       string
	behavior types.ProrationBehavior
	endDate  *time.Time
	items    []pvItemSpec
}

type pvSub struct {
	sub   *subscription.Subscription
	items map[string]*subscription.SubscriptionLineItem
}

func (s *FixedChargeProrationSuite) build(spec pvSubSpec) *pvSub {
	ctx := s.GetContext()
	id := types.GenerateUUIDWithPrefix("pv")
	tz := lo.Ternary(spec.tz == "", "UTC", spec.tz)

	cust := &customer.Customer{ID: "cust_" + id, ExternalID: "ext_" + id, Name: "PV", BaseModel: types.GetDefaultBaseModel(ctx)}
	s.Require().NoError(s.GetStores().CustomerRepo.Create(ctx, cust))
	pl := &plan.Plan{ID: "plan_" + id, Name: "PV Plan", BaseModel: types.GetDefaultBaseModel(ctx)}
	s.Require().NoError(s.GetStores().PlanRepo.Create(ctx, pl))

	// Mirror subscription creation (subscription.go: anchor + first period end).
	anchor := spec.anchor
	if anchor.IsZero() {
		if spec.cycle == types.BillingCycleCalendar {
			anchor = types.CalculateCalendarBillingAnchor(spec.start, spec.period, tz)
		} else {
			anchor = spec.start
		}
	}
	periodEnd, err := types.NextBillingDate(&types.NextBillingDateParams{
		CurrentPeriodStart: spec.start, BillingAnchor: anchor, Unit: 1,
		Period: spec.period, SubscriptionEndDate: spec.endDate, Timezone: tz,
	})
	s.Require().NoError(err)

	sub := &subscription.Subscription{
		ID: "sub_" + id, PlanID: pl.ID, CustomerID: cust.ID,
		StartDate: spec.start, BillingAnchor: anchor, EndDate: spec.endDate,
		CurrentPeriodStart: spec.start, CurrentPeriodEnd: periodEnd,
		Currency: "usd", BillingPeriod: spec.period, BillingPeriodCount: 1,
		BillingCycle: spec.cycle, SubscriptionStatus: types.SubscriptionStatusActive,
		Timezone: tz, ProrationBehavior: spec.behavior,
		BaseModel: types.GetDefaultBaseModel(ctx),
	}

	out := &pvSub{sub: sub, items: map[string]*subscription.SubscriptionLineItem{}}
	lineItems := make([]*subscription.SubscriptionLineItem, 0, len(spec.items))
	for _, it := range spec.items {
		model := lo.Ternary(it.model == "", types.BILLING_MODEL_FLAT_FEE, it.model)
		entityType := lo.Ternary(it.entityType == "", types.SubscriptionLineItemEntityTypePlan, it.entityType)
		entityID := pl.ID
		priceEntity := types.PRICE_ENTITY_TYPE_PLAN
		if entityType == types.SubscriptionLineItemEntityTypeAddon {
			entityID = "addon_" + id + "_" + it.key
			priceEntity = types.PRICE_ENTITY_TYPE_ADDON
		}
		amount := decimal.Zero
		if it.amount != "" {
			amount = decimal.RequireFromString(it.amount)
		}
		pr := &price.Price{
			ID: "price_" + id + "_" + it.key, Amount: amount, Currency: "usd",
			EntityType: priceEntity, EntityID: entityID, Type: types.PRICE_TYPE_FIXED,
			BillingPeriod: it.period, BillingPeriodCount: 1, BillingModel: model,
			TierMode: it.tierMode, Tiers: it.tiers,
			BillingCadence: types.BILLING_CADENCE_RECURRING, InvoiceCadence: types.InvoiceCadenceAdvance,
			BaseModel: types.GetDefaultBaseModel(ctx),
		}
		if it.transform != nil {
			pr.TransformQuantity = price.JSONBTransformQuantity(*it.transform)
		}
		s.Require().NoError(s.GetStores().PriceRepo.Create(ctx, pr))

		qty := lo.Ternary(it.qty == 0, int64(1), it.qty)
		start := lo.Ternary(it.start.IsZero(), spec.start, it.start)
		li := &subscription.SubscriptionLineItem{
			ID: "li_" + id + "_" + it.key, SubscriptionID: sub.ID, CustomerID: cust.ID,
			EntityID: entityID, EntityType: entityType,
			PriceID: pr.ID, PriceType: types.PRICE_TYPE_FIXED, DisplayName: it.key,
			Quantity: decimal.NewFromInt(qty), Currency: "usd",
			BillingPeriod: it.period, BillingPeriodCount: 1,
			InvoiceCadence: types.InvoiceCadenceAdvance, StartDate: start, EndDate: it.end,
			BaseModel: types.GetDefaultBaseModel(ctx),
		}
		lineItems = append(lineItems, li)
		out.items[it.key] = li
	}

	s.Require().NoError(s.GetStores().SubscriptionRepo.CreateWithLineItems(ctx, sub, lineItems))
	sub.LineItems = lineItems
	return out
}

// setCurrent rolls the subscription's current period the way production does on period advance:
// CurrentPeriodStart/End move, BillingAnchor is left untouched.
func (sc *pvSub) setCurrent(start, end time.Time) {
	sc.sub.CurrentPeriodStart = start
	sc.sub.CurrentPeriodEnd = end
}

// charge returns the invoice total and per-item totals for [start, end).
func (s *FixedChargeProrationSuite) charge(sc *pvSub, start, end time.Time) (decimal.Decimal, map[string]decimal.Decimal) {
	res, err := s.billing.CalculateFixedCharges(s.GetContext(), &dto.CalculateFixedChargesParams{
		Subscription: sc.sub,
		PeriodStart:  start,
		PeriodEnd:    end,
	})
	s.Require().NoError(err)
	perItem := map[string]decimal.Decimal{}
	for key, li := range sc.items {
		total := decimal.Zero
		for _, line := range res.LineItems {
			if line.SubscriptionLineItemID != nil && *line.SubscriptionLineItemID == li.ID {
				total = total.Add(line.Amount)
			}
		}
		perItem[key] = total
	}
	return res.TotalAmount, perItem
}

// current charges the subscription's own current period.
func (s *FixedChargeProrationSuite) current(sc *pvSub) (decimal.Decimal, map[string]decimal.Decimal) {
	return s.charge(sc, sc.sub.CurrentPeriodStart, sc.sub.CurrentPeriodEnd)
}

func (s *FixedChargeProrationSuite) expect(label, want string, got decimal.Decimal) {
	expected := decimal.RequireFromString(want)
	ok := got.Sub(expected).Abs().LessThanOrEqual(decimal.NewFromFloat(0.01))
	s.True(ok, "%s: expected %s, got %s", label, want, got.StringFixed(2))
}

func flat(key string, period types.BillingPeriod, amount string) pvItemSpec {
	return pvItemSpec{key: key, period: period, amount: amount}
}

// 1. Calendar monthly start Jan 15: stub [Jan15,Feb1) over [Jan1,Feb1) = 17/31.
func (s *FixedChargeProrationSuite) TestCalendarMonthlyStub() {
	sc := s.build(pvSubSpec{
		cycle: types.BillingCycleCalendar, period: types.BILLING_PERIOD_MONTHLY,
		start: pvUTC(2026, time.January, 15), behavior: types.ProrationBehaviorCreateProrations,
		items: []pvItemSpec{flat("plan", types.BILLING_PERIOD_MONTHLY, "31")},
	})
	s.Equal(pvUTC(2026, time.February, 1), sc.sub.CurrentPeriodEnd)
	total, _ := s.current(sc)
	s.expect("S01 calendar monthly stub Jan15-Feb1", "17.00", total)
}

// 2. Same sub, later periods must bill 1x.
func (s *FixedChargeProrationSuite) TestCalendarMonthlyLaterPeriods() {
	spec := pvSubSpec{
		cycle: types.BillingCycleCalendar, period: types.BILLING_PERIOD_MONTHLY,
		start: pvUTC(2026, time.January, 15), behavior: types.ProrationBehaviorCreateProrations,
		items: []pvItemSpec{flat("plan", types.BILLING_PERIOD_MONTHLY, "31")},
	}
	s.Run("period2_current_rolled", func() {
		sc := s.build(spec)
		sc.setCurrent(pvUTC(2026, time.February, 1), pvUTC(2026, time.March, 1))
		total, _ := s.current(sc)
		s.expect("S02a calendar monthly period 2 Feb1-Mar1 (current rolled)", "31.00", total)
	})
	s.Run("period3_current_rolled", func() {
		sc := s.build(spec)
		sc.setCurrent(pvUTC(2026, time.March, 1), pvUTC(2026, time.April, 1))
		total, _ := s.current(sc)
		s.expect("S02b calendar monthly period 3 Mar1-Apr1 (current rolled)", "31.00", total)
	})
	s.Run("period2_as_next_period_advance", func() {
		// Period-end invoice of period 1: advance for [Feb1,Mar1) while current is still [Jan15,Feb1).
		sc := s.build(spec)
		total, _ := s.charge(sc, pvUTC(2026, time.February, 1), pvUTC(2026, time.March, 1))
		s.expect("S02c calendar monthly period 2 as next-period advance", "31.00", total)
	})
}

// 3. Anniversary with anchor ahead of start: stub [Jan10,Jan20) over [Dec20,Jan20) = 10/31.
func (s *FixedChargeProrationSuite) TestAnniversaryAnchorAhead() {
	spec := pvSubSpec{
		cycle: types.BillingCycleAnniversary, period: types.BILLING_PERIOD_MONTHLY,
		start: pvUTC(2026, time.January, 10), anchor: pvUTC(2026, time.January, 20),
		behavior: types.ProrationBehaviorCreateProrations,
		items:    []pvItemSpec{flat("plan", types.BILLING_PERIOD_MONTHLY, "31")},
	}
	s.Run("period1_stub", func() {
		sc := s.build(spec)
		s.Equal(pvUTC(2026, time.January, 20), sc.sub.CurrentPeriodEnd)
		total, _ := s.current(sc)
		s.expect("S03a anniversary anchor-ahead stub Jan10-Jan20", "10.00", total)
	})
	s.Run("period2_full", func() {
		sc := s.build(spec)
		sc.setCurrent(pvUTC(2026, time.January, 20), pvUTC(2026, time.February, 20))
		total, _ := s.current(sc)
		s.expect("S03b anniversary anchor-ahead period 2 Jan20-Feb20", "31.00", total)
	})
}

// 4. Anniversary sub anchored Jan 10; addon added Jan 25. Period 2 addon must be full.
func (s *FixedChargeProrationSuite) TestAnniversaryAddonMidPeriodThenFullPeriod() {
	spec := pvSubSpec{
		cycle: types.BillingCycleAnniversary, period: types.BILLING_PERIOD_MONTHLY,
		start: pvUTC(2026, time.January, 10), behavior: types.ProrationBehaviorCreateProrations,
		items: []pvItemSpec{
			flat("plan", types.BILLING_PERIOD_MONTHLY, "31"),
			{key: "addon", period: types.BILLING_PERIOD_MONTHLY, amount: "31",
				start: pvUTC(2026, time.January, 25), entityType: types.SubscriptionLineItemEntityTypeAddon},
		},
	}
	s.Run("period1_addon_partial", func() {
		sc := s.build(spec)
		_, per := s.current(sc)
		s.expect("S04a anniversary period 1 addon from Jan25 (16/31)", "16.00", per["addon"])
		s.expect("S04a anniversary period 1 plan", "31.00", per["plan"])
	})
	s.Run("period2_addon_full", func() {
		sc := s.build(spec)
		sc.setCurrent(pvUTC(2026, time.February, 10), pvUTC(2026, time.March, 10))
		_, per := s.current(sc)
		s.expect("S04b anniversary period 2 Feb10-Mar10 addon", "31.00", per["addon"])
		s.expect("S04b anniversary period 2 Feb10-Mar10 plan", "31.00", per["plan"])
	})
}

// 5. Calendar quarterly start Feb 15: 45/90 of the Jan1-Apr1 quarter, then full.
func (s *FixedChargeProrationSuite) TestCalendarQuarterly() {
	spec := pvSubSpec{
		cycle: types.BillingCycleCalendar, period: types.BILLING_PERIOD_QUARTER,
		start: pvUTC(2026, time.February, 15), behavior: types.ProrationBehaviorCreateProrations,
		items: []pvItemSpec{flat("plan", types.BILLING_PERIOD_QUARTER, "90")},
	}
	s.Run("period1_stub", func() {
		sc := s.build(spec)
		s.Equal(pvUTC(2026, time.April, 1), sc.sub.CurrentPeriodEnd)
		total, _ := s.current(sc)
		s.expect("S05a calendar quarterly stub Feb15-Apr1", "45.00", total)
	})
	s.Run("period2_full", func() {
		sc := s.build(spec)
		sc.setCurrent(pvUTC(2026, time.April, 1), pvUTC(2026, time.July, 1))
		total, _ := s.current(sc)
		s.expect("S05b calendar quarterly period 2 Apr1-Jul1", "90.00", total)
	})
}

// 6. Calendar annual start Mar 15 2026: 292/365.
func (s *FixedChargeProrationSuite) TestCalendarAnnual() {
	sc := s.build(pvSubSpec{
		cycle: types.BillingCycleCalendar, period: types.BILLING_PERIOD_ANNUAL,
		start: pvUTC(2026, time.March, 15), behavior: types.ProrationBehaviorCreateProrations,
		items: []pvItemSpec{flat("plan", types.BILLING_PERIOD_ANNUAL, "365")},
	})
	s.Equal(pvUTC(2027, time.January, 1), sc.sub.CurrentPeriodEnd)
	total, _ := s.current(sc)
	s.expect("S06 calendar annual stub Mar15-Jan1 (292/365)", "292.00", total)

	s.Run("leap_year_feb29_start", func() {
		// [Feb 29 2028, Jan 1 2029) is 307 of 2028's 366 days.
		sc := s.build(pvSubSpec{
			cycle: types.BillingCycleCalendar, period: types.BILLING_PERIOD_ANNUAL,
			start: pvUTC(2028, time.February, 29), behavior: types.ProrationBehaviorCreateProrations,
			items: []pvItemSpec{flat("plan", types.BILLING_PERIOD_ANNUAL, "365")},
		})
		s.Equal(pvUTC(2029, time.January, 1), sc.sub.CurrentPeriodEnd)
		total, _ := s.current(sc)
		s.expect("S06b calendar annual stub Feb29-Jan1 (307/366)", "306.16", total)
	})
}

// 8. Timezone-aware calendar stubs.
func (s *FixedChargeProrationSuite) TestTimezoneCalendarStub() {
	s.Run("asia_kolkata", func() {
		tz := "Asia/Kolkata"
		sc := s.build(pvSubSpec{
			cycle: types.BillingCycleCalendar, period: types.BILLING_PERIOD_MONTHLY, tz: tz,
			start: pvAt(2026, time.February, 15, tz), behavior: types.ProrationBehaviorCreateProrations,
			items: []pvItemSpec{flat("plan", types.BILLING_PERIOD_MONTHLY, "31")},
		})
		s.Equal(pvAt(2026, time.March, 1, tz), sc.sub.CurrentPeriodEnd)
		total, _ := s.current(sc)
		s.expect("S08a Asia/Kolkata calendar monthly stub Feb15-Mar1 (14/28)", "15.50", total)
	})
	s.Run("america_new_york_dst", func() {
		tz := "America/New_York"
		sc := s.build(pvSubSpec{
			cycle: types.BillingCycleCalendar, period: types.BILLING_PERIOD_MONTHLY, tz: tz,
			start: pvAt(2026, time.March, 17, tz), behavior: types.ProrationBehaviorCreateProrations,
			items: []pvItemSpec{flat("plan", types.BILLING_PERIOD_MONTHLY, "31")},
		})
		s.Equal(pvAt(2026, time.April, 1, tz), sc.sub.CurrentPeriodEnd)
		total, _ := s.current(sc)
		// Seconds-based: 15 days / (31 days - 1h DST) = 360/743 -> 31*360/743 = 15.02.
		s.expect("S08b America/New_York calendar monthly stub Mar17-Apr1 (360h/743h)", "15.02", total)
	})

	// want is $31 x used/full in real seconds, with the full month taken from local midnights.
	cases := []struct {
		name, tz   string
		start, end time.Time
		want       string
	}{
		// +5:45, no DST: 14 of February's 28 days.
		{"asia_kathmandu", "Asia/Kathmandu", pvAt(2026, time.February, 15, "Asia/Kathmandu"), pvAt(2026, time.March, 1, "Asia/Kathmandu"), "15.50"},
		// DST starts Oct 4: 408h used of a 743h October.
		{"australia_sydney_dst_start", "Australia/Sydney", pvAt(2026, time.October, 15, "Australia/Sydney"), pvAt(2026, time.November, 1, "Australia/Sydney"), "17.02"},
		// K2: DST ends Nov 1 at 02:00, so October is a plain 744h month: 29/31.
		{"america_new_york_october", "America/New_York", pvAt(2026, time.October, 3, "America/New_York"), pvAt(2026, time.November, 1, "America/New_York"), "29.00"},
		// DST ended Nov 1: 384h used of a 721h November.
		{"america_new_york_dst_end", "America/New_York", pvAt(2026, time.November, 15, "America/New_York"), pvAt(2026, time.December, 1, "America/New_York"), "16.51"},
	}
	for _, tc := range cases {
		s.Run(tc.name, func() {
			sc := s.build(pvSubSpec{
				cycle: types.BillingCycleCalendar, period: types.BILLING_PERIOD_MONTHLY, tz: tc.tz,
				start: tc.start, behavior: types.ProrationBehaviorCreateProrations,
				items: []pvItemSpec{flat("plan", types.BILLING_PERIOD_MONTHLY, "31")},
			})
			s.Equal(tc.end, sc.sub.CurrentPeriodEnd)
			total, _ := s.current(sc)
			s.expect("S08 "+tc.name, tc.want, total)
		})
	}
}

// 9. Subscription end date or cancellation mid-period: last period [Mar1, Mar10) = 9/31.
func (s *FixedChargeProrationSuite) TestEndDateMidPeriod() {
	endDate := pvUTC(2026, time.March, 10)
	spec := pvSubSpec{
		cycle: types.BillingCycleCalendar, period: types.BILLING_PERIOD_MONTHLY,
		start: pvUTC(2026, time.January, 1), behavior: types.ProrationBehaviorCreateProrations,
		endDate: &endDate,
		items:   []pvItemSpec{flat("plan", types.BILLING_PERIOD_MONTHLY, "31")},
	}
	s.Run("current_period_clamped_to_end_date", func() {
		sc := s.build(spec)
		periodEnd, err := types.NextBillingDate(&types.NextBillingDateParams{
			CurrentPeriodStart: pvUTC(2026, time.March, 1), BillingAnchor: sc.sub.BillingAnchor, Unit: 1,
			Period: types.BILLING_PERIOD_MONTHLY, SubscriptionEndDate: &endDate, Timezone: "UTC",
		})
		s.Require().NoError(err)
		s.Equal(endDate, periodEnd, "last period should be clamped to end date")
		sc.setCurrent(pvUTC(2026, time.March, 1), periodEnd)
		total, _ := s.current(sc)
		s.expect("S09a end date Mar10, last period Mar1-Mar10 (9/31)", "9.00", total)
	})
	s.Run("line_item_end_date_full_window", func() {
		spec2 := spec
		spec2.items = []pvItemSpec{{key: "plan", period: types.BILLING_PERIOD_MONTHLY, amount: "31", end: endDate}}
		sc := s.build(spec2)
		sc.setCurrent(pvUTC(2026, time.March, 1), pvUTC(2026, time.April, 1))
		total, _ := s.current(sc)
		s.expect("S09b item end Mar10 in window Mar1-Apr1 (9/31)", "9.00", total)
	})
	s.Run("cancelled_sub_last_period", func() {
		sc := s.build(spec)
		sc.sub.SubscriptionStatus = types.SubscriptionStatusCancelled
		sc.sub.CancelledAt = lo.ToPtr(endDate)
		sc.setCurrent(pvUTC(2026, time.March, 1), endDate)
		total, _ := s.current(sc)
		s.expect("S09c cancelled at Mar10, last period Mar1-Mar10 (9/31)", "9.00", total)
	})
	s.Run("cancelled_sub_item_end_date_full_window", func() {
		spec2 := spec
		spec2.items = []pvItemSpec{{key: "plan", period: types.BILLING_PERIOD_MONTHLY, amount: "31", end: endDate}}
		sc := s.build(spec2)
		sc.sub.SubscriptionStatus = types.SubscriptionStatusCancelled
		sc.sub.CancelledAt = lo.ToPtr(endDate)
		sc.setCurrent(pvUTC(2026, time.March, 1), pvUTC(2026, time.April, 1))
		total, _ := s.current(sc)
		s.expect("S09d cancelled, item end Mar10 in Mar1-Apr1 (9/31)", "9.00", total)
	})
}

// 10. Tier-aware money: CalculateCost(price, qty) x coefficient on a 17/31 stub.
func (s *FixedChargeProrationSuite) TestTieredAndPackageStub() {
	base := pvSubSpec{
		cycle: types.BillingCycleCalendar, period: types.BILLING_PERIOD_MONTHLY,
		start: pvUTC(2026, time.January, 15), behavior: types.ProrationBehaviorCreateProrations,
	}
	s.Run("volume_tiers", func() {
		spec := base
		spec.items = []pvItemSpec{{
			key: "plan", period: types.BILLING_PERIOD_MONTHLY, qty: 10,
			model: types.BILLING_MODEL_TIERED, tierMode: types.BILLING_TIER_VOLUME,
			tiers: []price.PriceTier{
				{UpTo: lo.ToPtr(uint64(10)), UnitAmount: decimal.NewFromInt(10)},
				{UpTo: nil, UnitAmount: decimal.NewFromInt(8)},
			},
		}}
		sc := s.build(spec)
		total, _ := s.current(sc)
		// CalculateCost = 10 x $10 = $100; x 17/31 = 54.84.
		s.expect("S10a volume tiers qty10 ($100) on 17/31 stub", "54.84", total)
	})
	s.Run("volume_tiers_qty12", func() {
		spec := base
		spec.items = []pvItemSpec{{
			key: "plan", period: types.BILLING_PERIOD_MONTHLY, qty: 12,
			model: types.BILLING_MODEL_TIERED, tierMode: types.BILLING_TIER_VOLUME,
			tiers: []price.PriceTier{
				{UpTo: lo.ToPtr(uint64(10)), UnitAmount: decimal.NewFromInt(10)},
				{UpTo: nil, UnitAmount: decimal.NewFromInt(8)},
			},
		}}
		sc := s.build(spec)
		total, _ := s.current(sc)
		// CalculateCost = 12 x $8 = $96; x 17/31 = 52.65.
		s.expect("S10b volume tiers qty12 ($96) on 17/31 stub", "52.65", total)
	})
	s.Run("package", func() {
		spec := base
		spec.items = []pvItemSpec{{
			key: "plan", period: types.BILLING_PERIOD_MONTHLY, qty: 7, amount: "20",
			model:     types.BILLING_MODEL_PACKAGE,
			transform: &price.TransformQuantity{DivideBy: 5, Round: types.ROUND_UP},
		}}
		sc := s.build(spec)
		total, _ := s.current(sc)
		// CalculateCost = ceil(7/5)=2 packages x $20 = $40; x 17/31 = 21.94.
		s.expect("S10c package 5/unit $20 qty7 ($40) on 17/31 stub", "21.94", total)
	})
	s.Run("slab_tiers", func() {
		spec := base
		spec.items = []pvItemSpec{{
			key: "plan", period: types.BILLING_PERIOD_MONTHLY, qty: 20,
			model: types.BILLING_MODEL_TIERED, tierMode: types.BILLING_TIER_SLAB,
			tiers: []price.PriceTier{
				{UpTo: lo.ToPtr(uint64(10)), UnitAmount: decimal.NewFromInt(5)},
				{UpTo: nil, UnitAmount: decimal.NewFromInt(3)},
			},
		}}
		sc := s.build(spec)
		total, _ := s.current(sc)
		// CalculateCost = 10 x $5 + 10 x $3 = $80; x 17/31 = 43.87.
		s.expect("S10d slab tiers qty20 ($80) on 17/31 stub", "43.87", total)
	})
}

// 12. proration_behavior=none: no proration at all.
func (s *FixedChargeProrationSuite) TestProrationNone() {
	s.Run("calendar_monthly_stub", func() {
		sc := s.build(pvSubSpec{
			cycle: types.BillingCycleCalendar, period: types.BILLING_PERIOD_MONTHLY,
			start: pvUTC(2026, time.January, 15), behavior: types.ProrationBehaviorNone,
			items: []pvItemSpec{flat("plan", types.BILLING_PERIOD_MONTHLY, "31")},
		})
		total, _ := s.current(sc)
		s.expect("S12a none: calendar monthly stub Jan15-Feb1", "31.00", total)
	})
	s.Run("anniversary_anchor_ahead", func() {
		sc := s.build(pvSubSpec{
			cycle: types.BillingCycleAnniversary, period: types.BILLING_PERIOD_MONTHLY,
			start: pvUTC(2026, time.January, 10), anchor: pvUTC(2026, time.January, 20),
			behavior: types.ProrationBehaviorNone,
			items:    []pvItemSpec{flat("plan", types.BILLING_PERIOD_MONTHLY, "31")},
		})
		total, _ := s.current(sc)
		s.expect("S12b none: anniversary anchor-ahead stub Jan10-Jan20", "31.00", total)
	})
}

// D7: addon attached Oct 3 00:00 New York on a calendar monthly sub. The divisor is the local
// [Oct 1, Nov 1) month (744h), not a month counted forward from the attach across the DST change.
func (s *FixedChargeProrationSuite) TestAttachBeforeDSTChangeUsesFullPeriod() {
	ctx := s.GetContext()
	tz := "America/New_York"
	attachAt := pvAt(2026, time.October, 3, tz)
	sc := s.build(pvSubSpec{
		cycle: types.BillingCycleCalendar, period: types.BILLING_PERIOD_MONTHLY, tz: tz,
		start: pvAt(2026, time.October, 1, tz), behavior: types.ProrationBehaviorCreateProrations,
		items: []pvItemSpec{
			flat("plan", types.BILLING_PERIOD_MONTHLY, "31"),
			{key: "addon", period: types.BILLING_PERIOD_MONTHLY, amount: "31", start: attachAt,
				entityType: types.SubscriptionLineItemEntityTypeAddon},
		},
	})
	addonPrice, err := s.GetStores().PriceRepo.Get(ctx, sc.items["addon"].PriceID)
	s.Require().NoError(err)

	quote, err := NewLineItemProrationService(s.params).Compute(ctx, LineItemProrationRequest{
		Subscription: sc.sub,
		Entries: []LineItemProrationEntry{{
			LineItem: sc.items["addon"], NewPrice: addonPrice, NewQuantity: decimal.NewFromInt(1),
			Action: types.ProrationActionAddItem,
		}},
		EffectiveDate: attachAt,
		Behavior:      types.ProrationBehaviorCreateProrations,
	})
	s.Require().NoError(err)
	s.expect("S13a New York attach Oct 3 (29/31)", "29.00", quote.TotalChargeAmount)

	_, per := s.current(sc)
	s.expect("S13b New York addon on the opening invoice (29/31)", "29.00", per["addon"])
}

// D1–D7: a $31 monthly addon attached mid-period is charged used/full of the sub's period, and
// (D3) the same as a sub starting that day on the same schedule.
func (s *FixedChargeProrationSuite) TestAttachMidPeriod() {
	ctx := s.GetContext()
	jan31Last := pvUTC(2026, time.January, 31).Add(24*time.Hour - time.Second)
	cases := []struct {
		name          string
		cycle         types.BillingCycle
		tz            string
		start         time.Time
		current       *types.Period
		attach        time.Time
		want          string
		wantLineStart time.Time
	}{
		{name: "D1 calendar stub Jan 20 (12/31)", cycle: types.BillingCycleCalendar, start: pvUTC(2026, time.January, 15),
			attach: pvUTC(2026, time.January, 20), want: "12.00"},
		{name: "D2 anniversary Jan 20 (12/31)", cycle: types.BillingCycleAnniversary, start: pvUTC(2026, time.January, 1),
			attach: pvUTC(2026, time.January, 20), want: "12.00"},
		{name: "D6 last day Jan 31 (1/31)", cycle: types.BillingCycleCalendar, start: pvUTC(2026, time.January, 15),
			attach: pvUTC(2026, time.January, 31), want: "1.00"},
		{name: "one second before the period end", cycle: types.BillingCycleCalendar, start: pvUTC(2026, time.January, 15),
			attach: jan31Last, want: "0.00"},
		{name: "D5 on the boundary (full)", cycle: types.BillingCycleAnniversary, start: pvUTC(2026, time.January, 1),
			current: &types.Period{Start: pvUTC(2026, time.February, 1), End: pvUTC(2026, time.March, 1)},
			attach:  pvUTC(2026, time.February, 1), want: "31.00"},
		// DST starts Mar 8: 528h used of a 743h March.
		{name: "D7 New York DST start Mar 10", cycle: types.BillingCycleCalendar, tz: "America/New_York",
			start: pvAt(2026, time.March, 1, "America/New_York"), attach: pvAt(2026, time.March, 10, "America/New_York"), want: "22.03"},
		{name: "Kathmandu Feb 15 (14/28)", cycle: types.BillingCycleCalendar, tz: "Asia/Kathmandu",
			start: pvAt(2026, time.February, 1, "Asia/Kathmandu"), attach: pvAt(2026, time.February, 15, "Asia/Kathmandu"), want: "15.50"},
	}
	for _, tc := range cases {
		s.Run(tc.name, func() {
			sc := s.build(pvSubSpec{
				cycle: tc.cycle, period: types.BILLING_PERIOD_MONTHLY, tz: tc.tz, start: tc.start,
				behavior: types.ProrationBehaviorCreateProrations,
				items: []pvItemSpec{
					flat("plan", types.BILLING_PERIOD_MONTHLY, "31"),
					{key: "addon", period: types.BILLING_PERIOD_MONTHLY, amount: "31", start: tc.attach,
						entityType: types.SubscriptionLineItemEntityTypeAddon},
				},
			})
			if tc.current != nil {
				sc.setCurrent(tc.current.Start, tc.current.End)
			}
			addonPrice, err := s.GetStores().PriceRepo.Get(ctx, sc.items["addon"].PriceID)
			s.Require().NoError(err)
			quote, err := NewLineItemProrationService(s.params).Compute(ctx, LineItemProrationRequest{
				Subscription: sc.sub,
				Entries: []LineItemProrationEntry{{
					LineItem: sc.items["addon"], NewPrice: addonPrice, NewQuantity: decimal.NewFromInt(1),
					Action: types.ProrationActionAddItem,
				}},
				EffectiveDate: tc.attach,
				Behavior:      types.ProrationBehaviorCreateProrations,
			})
			s.Require().NoError(err)
			s.expect(tc.name+" attach charge", tc.want, quote.TotalChargeAmount)

			// D3: a sub starting at the attach on the same schedule (anchor = this period's end).
			parity := s.build(pvSubSpec{
				cycle: types.BillingCycleAnniversary, period: types.BILLING_PERIOD_MONTHLY, tz: tc.tz,
				start: tc.attach, anchor: sc.sub.CurrentPeriodEnd, behavior: types.ProrationBehaviorCreateProrations,
				items: []pvItemSpec{flat("plan", types.BILLING_PERIOD_MONTHLY, "31")},
			})
			s.Equal(sc.sub.CurrentPeriodEnd, parity.sub.CurrentPeriodEnd)
			total, _ := s.current(parity)
			s.expect(tc.name+" parity with a sub starting at the attach", tc.want, total)
		})
	}
}

// D9 with create_prorations: a backdated calendar sub from Jul 15 with an addon from Aug 10 bills
// the plan stub, then the addon's 22/31, then both in full.
func (s *FixedChargeProrationSuite) TestBackdatedAddonPeriodsCreateProrations() {
	sc := s.build(pvSubSpec{
		cycle: types.BillingCycleCalendar, period: types.BILLING_PERIOD_MONTHLY,
		start: pvUTC(2026, time.July, 15), behavior: types.ProrationBehaviorCreateProrations,
		items: []pvItemSpec{
			flat("plan", types.BILLING_PERIOD_MONTHLY, "31"),
			{key: "addon", period: types.BILLING_PERIOD_MONTHLY, amount: "31", start: pvUTC(2026, time.August, 10),
				entityType: types.SubscriptionLineItemEntityTypeAddon},
		},
	})
	periods := []struct {
		name        string
		start, end  time.Time
		plan, addon string
	}{
		{"P1 Jul15-Aug1", pvUTC(2026, time.July, 15), pvUTC(2026, time.August, 1), "17.00", "0.00"},
		{"P2 Aug1-Sep1", pvUTC(2026, time.August, 1), pvUTC(2026, time.September, 1), "31.00", "22.00"},
		{"P3 Sep1-Oct1", pvUTC(2026, time.September, 1), pvUTC(2026, time.October, 1), "31.00", "31.00"},
	}
	for _, p := range periods {
		s.Run(p.name, func() {
			sc.setCurrent(p.start, p.end)
			_, per := s.current(sc)
			s.expect("J3 "+p.name+" plan", p.plan, per["plan"])
			s.expect("J3 "+p.name+" addon", p.addon, per["addon"])
		})
	}
}

// Anchor later on the start day: the first period ends at the anchor.
// e.g. start Jan 31 00:00, anchor Jan 31 12:00 → [Jan 31 00:00, Jan 31 12:00) of [Dec 31 12:00, Jan 31 12:00) = $0.50.
func (s *FixedChargeProrationSuite) TestAnchorLaterInTheDayFirstPeriod() {
	start := pvUTC(2026, time.January, 31)
	sc := s.build(pvSubSpec{
		cycle: types.BillingCycleAnniversary, period: types.BILLING_PERIOD_MONTHLY,
		start: start, anchor: start.Add(12 * time.Hour), behavior: types.ProrationBehaviorCreateProrations,
		items: []pvItemSpec{flat("plan", types.BILLING_PERIOD_MONTHLY, "31")},
	})
	s.Equal(time.Date(2026, time.January, 31, 12, 0, 0, 0, time.UTC), sc.sub.CurrentPeriodEnd)
	total, _ := s.current(sc)
	s.expect("stub up to the anchor", "0.50", total)
}

// A longer item on an anniversary sub whose anchor is later in the day than the start: the item's
// sub-day stub and its first full period both start in the opening period; both must be billed.
// e.g. start Jan 10 00:00, anchor Jan 10 12:00 → annual item [Jan 10 00:00, Jan 10 12:00) then
// [Jan 10 12:00 2026, Jan 10 12:00 2027).
func (s *FixedChargeProrationSuite) TestLongerItemWithAnchorLaterInTheDay() {
	start := pvUTC(2026, time.January, 10)
	anchor := start.Add(12 * time.Hour)
	cases := []struct {
		name          string
		period        types.BillingPeriod
		amount        string
		firstFullEnds time.Time
	}{
		{"annual", types.BILLING_PERIOD_ANNUAL, "365", time.Date(2027, time.January, 10, 12, 0, 0, 0, time.UTC)},
		{"quarterly", types.BILLING_PERIOD_QUARTER, "90", time.Date(2026, time.April, 10, 12, 0, 0, 0, time.UTC)},
	}
	for _, tc := range cases {
		s.Run(tc.name, func() {
			sc := s.build(pvSubSpec{
				cycle: types.BillingCycleAnniversary, period: types.BILLING_PERIOD_MONTHLY,
				start: start, anchor: anchor, behavior: types.ProrationBehaviorCreateProrations,
				items: []pvItemSpec{flat("item", tc.period, tc.amount)},
			})
			billed := false
			cur, end := sc.sub.CurrentPeriodStart, sc.sub.CurrentPeriodEnd
			for i := 0; i < 13 && !billed; i++ {
				res, err := s.billing.CalculateFixedCharges(s.GetContext(), &dto.CalculateFixedChargesParams{Subscription: sc.sub, PeriodStart: cur, PeriodEnd: end})
				s.Require().NoError(err)
				billed = lo.ContainsBy(res.LineItems, func(l dto.CreateInvoiceLineItemRequest) bool {
					return lo.FromPtr(l.PeriodEnd).Equal(tc.firstFullEnds)
				})
				cur = end
				end = time.Date(2026, time.March+time.Month(i), 10, 12, 0, 0, 0, time.UTC)
			}
			s.True(billed, "item period ending %s was never billed", tc.firstFullEnds)
		})
	}
}

// Randomized end-to-end proration checks: every allowed sub/item cadence pair is billed through the
// real flow (opening invoice, attach charge, period-end invoices) and checked against an independent
// billing grid. PROP_SEED and PROP_ROUNDS override the defaults.
type PropSuite struct {
	testutil.BaseServiceTestSuite
	params  ServiceParams
	billing BillingService
	lip     LineItemProrationService
}

func TestProrationProperties(t *testing.T) { suite.Run(t, new(PropSuite)) }

func (s *PropSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	st := s.GetStores()
	s.params = ServiceParams{
		Logger: s.GetLogger(), Config: s.GetConfig(), DB: s.GetDB(),
		SubRepo: st.SubscriptionRepo, SubscriptionLineItemRepo: st.SubscriptionLineItemRepo,
		SubscriptionPhaseRepo: st.SubscriptionPhaseRepo, SubScheduleRepo: st.SubscriptionScheduleRepo,
		PlanRepo: st.PlanRepo, PriceRepo: st.PriceRepo, PriceUnitRepo: st.PriceUnitRepo,
		EventRepo: st.EventRepo, MeterRepo: st.MeterRepo, CustomerRepo: st.CustomerRepo,
		InvoiceRepo: st.InvoiceRepo, InvoiceLineItemRepo: st.InvoiceLineItemRepo,
		EntitlementRepo: st.EntitlementRepo, EnvironmentRepo: st.EnvironmentRepo, FeatureRepo: st.FeatureRepo,
		TenantRepo: st.TenantRepo, UserRepo: st.UserRepo, AuthRepo: st.AuthRepo, WalletRepo: st.WalletRepo,
		PaymentRepo: st.PaymentRepo, CreditGrantRepo: st.CreditGrantRepo, CreditGrantApplicationRepo: st.CreditGrantApplicationRepo,
		CouponRepo: st.CouponRepo, CouponAssociationRepo: st.CouponAssociationRepo, CouponApplicationRepo: st.CouponApplicationRepo,
		AddonRepo: st.AddonRepo, AddonAssociationRepo: st.AddonAssociationRepo, SettingsRepo: st.SettingsRepo,
		TaxAssociationRepo: st.TaxAssociationRepo, TaxRateRepo: st.TaxRateRepo, TaxAppliedRepo: st.TaxAppliedRepo,
		AlertLogsRepo: st.AlertLogsRepo, EventPublisher: s.GetPublisher(), WebhookPublisher: s.GetWebhookPublisher(),
		ProrationCalculator: s.GetCalculator(), IntegrationFactory: s.GetIntegrationFactory(),
	}
	s.billing = NewBillingService(s.params)
	s.lip = NewLineItemProrationService(s.params)
}

type propCad struct {
	p types.BillingPeriod
	n int
}

func (c propCad) String() string { return fmt.Sprintf("%s×%d", c.p, c.n) }

func (c propCad) months() int { return types.EffectiveMonths(c.p, c.n) }

type propLine struct {
	start, end time.Time
	amount     decimal.Decimal
	src        string
}

type propScenario struct {
	cycle     types.BillingCycle
	subCad    propCad
	itemCad   propCad
	tz        string
	cadence   types.InvoiceCadence
	behavior  types.ProrationBehavior
	midAttach bool
}

func (sc propScenario) key() string {
	return fmt.Sprintf("%s|sub=%s|item=%s|%s|%s|%s|mid=%v", sc.cycle, sc.subCad, sc.itemCad, sc.tz, sc.cadence, sc.behavior, sc.midAttach)
}

// ---------- independent oracle ----------

func propLastDay(y int, m time.Month, loc *time.Location) int {
	return time.Date(y, m+1, 0, 0, 0, 0, 0, loc).Day()
}

// propGridDate is anchor + k item periods in loc, month periods clamped to month end.
func propGridDate(anchor time.Time, c propCad, k int, loc *time.Location) time.Time {
	a := anchor.In(loc)
	switch c.p {
	case types.BILLING_PERIOD_DAILY:
		return a.AddDate(0, 0, k*c.n).UTC()
	case types.BILLING_PERIOD_WEEKLY:
		return a.AddDate(0, 0, 7*k*c.n).UTC()
	}
	total := int(a.Month()) - 1 + k*c.months()
	y := a.Year() + floorDiv(total, 12)
	m := time.Month(floorMod(total, 12) + 1)
	d := min(a.Day(), propLastDay(y, m, loc))
	h, mi, sec := a.Clock()
	return time.Date(y, m, d, h, mi, sec, a.Nanosecond(), loc).UTC()
}

func floorDiv(a, b int) int {
	q := a / b
	if (a%b != 0) && ((a < 0) != (b < 0)) {
		q--
	}
	return q
}

func floorMod(a, b int) int { return a - floorDiv(a, b)*b }

// propGridIndex finds k with gridDate(k) == t (to the second), or ok=false.
func propGridIndex(anchor time.Time, c propCad, t time.Time, loc *time.Location) (int, bool) {
	var est int
	if c.months() > 0 {
		a, x := anchor.In(loc), t.In(loc)
		est = ((x.Year()-a.Year())*12 + int(x.Month()) - int(a.Month())) / c.months()
	} else {
		days := c.n
		if c.p == types.BILLING_PERIOD_WEEKLY {
			days *= 7
		}
		est = int(t.Sub(anchor).Hours()/24) / days
	}
	for k := est - 3; k <= est+3; k++ {
		if propGridDate(anchor, c, k, loc).Truncate(time.Second).Equal(t.Truncate(time.Second)) {
			return k, true
		}
	}
	return 0, false
}

// propNextBoundary is the first grid date >= t.
func propNextBoundary(anchor time.Time, c propCad, t time.Time, loc *time.Location) time.Time {
	var est int
	if c.months() > 0 {
		a, x := anchor.In(loc), t.In(loc)
		est = ((x.Year()-a.Year())*12+int(x.Month())-int(a.Month()))/c.months() - 2
	} else {
		days := c.n
		if c.p == types.BILLING_PERIOD_WEEKLY {
			days *= 7
		}
		est = int(t.Sub(anchor).Hours()/24)/days - 2
	}
	for k := est; ; k++ {
		if g := propGridDate(anchor, c, k, loc); !g.Before(t) {
			return g
		}
	}
}

// propCalendarAnchor is the next calendar boundary of the item's base period after t (mirrors D7).
func propCalendarAnchor(t time.Time, p types.BillingPeriod, loc *time.Location) time.Time {
	x := t.In(loc)
	switch p {
	case types.BILLING_PERIOD_DAILY:
		return time.Date(x.Year(), x.Month(), x.Day()+1, 0, 0, 0, 0, loc).UTC()
	case types.BILLING_PERIOD_WEEKLY:
		d := (8 - int(x.Weekday())) % 7
		if d == 0 {
			d = 7
		}
		return time.Date(x.Year(), x.Month(), x.Day()+d, 0, 0, 0, 0, loc).UTC()
	}
	step := types.BillingPeriodToMonths(p)
	m0 := (int(x.Month())-1)/step*step + step // next boundary month index from 0
	return time.Date(x.Year(), time.Month(m0+1), 1, 0, 0, 0, 0, loc).UTC()
}

func propSecs(a, b time.Time) int64 { return int64(b.Sub(a).Round(time.Second) / time.Second) }

func propSameLocalDay(a, b time.Time, loc *time.Location) bool {
	ay, am, ad := a.In(loc).Date()
	by, bm, bd := b.In(loc).Date()
	return ay == by && am == bm && ad == bd
}

// checkSubPeriods: the first sub period ends on the first grid date after the start; every later
// period is one grid step.
func (s *PropSuite) checkSubPeriods(sc propScenario, seed int64, res *propResult, start, anchor time.Time, periods []types.Period, loc *time.Location) bool {
	st, a := start.In(loc), anchor.In(loc)
	from := st
	if sc.subCad.months() == 0 {
		// DAILY/WEEKLY still compare on the anchor's clock.
		from = time.Date(st.Year(), st.Month(), st.Day(), a.Hour(), a.Minute(), a.Second(), 0, loc)
	}
	if want := propNextBoundary(anchor, sc.subCad, from.Add(time.Second), loc); !periods[0].End.Equal(want) {
		res.failf(sc, seed, "first sub period [%s, %s) should end %s (anchor %s)", start, periods[0].End, want, anchor)
		return false
	}
	for i, p := range periods[1:] {
		k, ok := propGridIndex(anchor, sc.subCad, p.Start, loc)
		if want := propGridDate(anchor, sc.subCad, k+1, loc); !ok || !p.End.Equal(want) {
			res.failf(sc, seed, "sub period %d [%s, %s) is not one grid step (anchor %s)", i+1, p.Start, p.End, anchor)
			return false
		}
	}
	return true
}

// ---------- scenario building ----------

var (
	propSubCads  = []propCad{{types.BILLING_PERIOD_DAILY, 1}, {types.BILLING_PERIOD_WEEKLY, 1}, {types.BILLING_PERIOD_MONTHLY, 1}, {types.BILLING_PERIOD_MONTHLY, 2}, {types.BILLING_PERIOD_QUARTER, 1}, {types.BILLING_PERIOD_HALF_YEAR, 1}, {types.BILLING_PERIOD_ANNUAL, 1}}
	propItemCads = []propCad{{types.BILLING_PERIOD_DAILY, 1}, {types.BILLING_PERIOD_WEEKLY, 1}, {types.BILLING_PERIOD_MONTHLY, 1}, {types.BILLING_PERIOD_MONTHLY, 2}, {types.BILLING_PERIOD_MONTHLY, 3}, {types.BILLING_PERIOD_QUARTER, 1}, {types.BILLING_PERIOD_HALF_YEAR, 1}, {types.BILLING_PERIOD_MONTHLY, 6}, {types.BILLING_PERIOD_ANNUAL, 1}, {types.BILLING_PERIOD_ANNUAL, 2}}
	propTZs      = []string{"UTC", "America/New_York", "Asia/Kolkata", "Australia/Sydney", "Asia/Kathmandu"}
	propAmounts  = []string{"31", "90", "365", "100", "12.34", "999.99", "0.07"}
)

func propRandStart(r *rand.Rand, loc *time.Location) time.Time {
	y := 2025 + r.Intn(3)
	m := time.Month(1 + r.Intn(12))
	var d int
	switch r.Intn(4) {
	case 0: // month end
		d = propLastDay(y, m, loc)
	case 1:
		if r.Intn(2) == 0 {
			y, m, d = 2028, time.February, 29
		} else {
			d = 28 + r.Intn(3)
			d = min(d, propLastDay(y, m, loc))
		}
	default:
		d = 1 + r.Intn(propLastDay(y, m, loc))
	}
	h, mi, sec := 0, 0, 0
	if r.Intn(2) == 0 {
		h, mi, sec = r.Intn(24), r.Intn(60), r.Intn(60)
	}
	return time.Date(y, m, d, h, mi, sec, 0, loc).UTC()
}

func (s *PropSuite) nextDate(start, anchor time.Time, c propCad, tz string) time.Time {
	d, err := types.NextBillingDate(&types.NextBillingDateParams{
		CurrentPeriodStart: start, BillingAnchor: anchor, Unit: c.n, Period: c.p, Timezone: tz,
	})
	s.Require().NoError(err)
	return d
}

func propHorizon(c propCad) int {
	switch c.p {
	case types.BILLING_PERIOD_DAILY:
		return 75
	case types.BILLING_PERIOD_WEEKLY:
		return 30
	}
	return max(4, 40/c.months()+2)
}

type propResult struct {
	failures []string
}

func (r *propResult) failf(sc propScenario, seed int64, format string, args ...any) {
	r.failures = append(r.failures, fmt.Sprintf("[%s seed=%d] %s", sc.key(), seed, fmt.Sprintf(format, args...)))
}

func (s *PropSuite) TestBillingFlowProperties() {
	seed := int64(1)
	if v, err := strconv.ParseInt(os.Getenv("PROP_SEED"), 10, 64); err == nil {
		seed = v
	}
	rounds := 1
	if v, err := strconv.Atoi(os.Getenv("PROP_ROUNDS")); err == nil {
		rounds = v
	}
	s.T().Logf("PROP_SEED=%d PROP_ROUNDS=%d", seed, rounds)

	res := &propResult{}
	scenarios := 0
	for round := 0; round < rounds; round++ {
		for _, cycle := range []types.BillingCycle{types.BillingCycleCalendar, types.BillingCycleAnniversary} {
			for _, subCad := range propSubCads {
				for _, itemCad := range propItemCads {
					if !types.IsCadenceAllowed(subCad.p, subCad.n, itemCad.p, itemCad.n) {
						continue
					}
					for _, tz := range propTZs {
						for _, cad := range []types.InvoiceCadence{types.InvoiceCadenceAdvance, types.InvoiceCadenceArrear} {
							for _, beh := range []types.ProrationBehavior{types.ProrationBehaviorCreateProrations, types.ProrationBehaviorNone} {
								for _, mid := range []bool{false, true} {
									sc := propScenario{cycle, subCad, itemCad, tz, cad, beh, mid}
									scSeed := seed + int64(scenarios)*7919
									s.runScenario(sc, scSeed, res)
									scenarios++
								}
							}
						}
					}
				}
			}
		}
	}

	s.T().Logf("scenarios=%d failures=%d", scenarios, len(res.failures))
	if len(res.failures) > 0 {
		sort.Strings(res.failures)
		shown := res.failures
		if len(shown) > 80 {
			shown = shown[:80]
		}
		s.Fail(fmt.Sprintf("%d property failures (first %d):\n%s", len(res.failures), len(shown), strings.Join(shown, "\n")))
	}
}

func (s *PropSuite) runScenario(sc propScenario, seed int64, res *propResult) {
	ctx := s.GetContext()
	r := rand.New(rand.NewSource(seed))
	loc, _ := time.LoadLocation(sc.tz)

	start := propRandStart(r, loc)
	var anchor time.Time
	if sc.cycle == types.BillingCycleCalendar {
		anchor = types.CalculateCalendarBillingAnchor(start, sc.subCad.p, sc.tz)
	} else {
		anchor = start
		if r.Intn(2) == 0 {
			firstEnd := s.nextDate(start, start, sc.subCad, sc.tz)
			days := int(firstEnd.Sub(start).Hours() / 24)
			if days > 1 {
				anchor = start.In(loc).AddDate(0, 0, 1+r.Intn(days-1)).UTC()
			}
		}
		switch r.Intn(4) {
		case 0: // same anchor day, another time of day (before or after the start's)
			a := anchor.In(loc)
			anchor = time.Date(a.Year(), a.Month(), a.Day(), r.Intn(24), r.Intn(60), r.Intn(60), 0, loc).UTC()
		case 1: // Stripe import: an anchor before the start, kept as-is
			a := start.In(loc).AddDate(0, 0, -1-r.Intn(400))
			anchor = time.Date(a.Year(), a.Month(), a.Day(), r.Intn(24), r.Intn(60), r.Intn(60), 0, loc).UTC()
		}
	}

	n := propHorizon(sc.subCad)
	periods := make([]types.Period, 0, n)
	cur := start
	for i := 0; i < n; i++ {
		end := s.nextDate(cur, anchor, sc.subCad, sc.tz)
		if !end.After(cur) {
			res.failf(sc, seed, "sub period %d does not advance: %s", i, cur)
			return
		}
		periods = append(periods, types.Period{Start: cur, End: end})
		cur = end
	}
	if !s.checkSubPeriods(sc, seed, res, start, anchor, periods, loc) {
		return
	}

	amount := decimal.RequireFromString(propAmounts[r.Intn(len(propAmounts))])
	pr := &price.Price{
		ID: types.GenerateUUIDWithPrefix(types.UUID_PREFIX_PRICE), Amount: amount, Currency: "usd",
		Type: types.PRICE_TYPE_FIXED, BillingPeriod: sc.itemCad.p, BillingPeriodCount: sc.itemCad.n,
		BillingModel: types.BILLING_MODEL_FLAT_FEE, BillingCadence: types.BILLING_CADENCE_RECURRING,
		InvoiceCadence: sc.cadence, BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.Require().NoError(s.GetStores().PriceRepo.Create(ctx, pr))

	attachIdx := 0
	itemStart := start
	if sc.midAttach {
		attachIdx = 1 + r.Intn(min(3, n-3))
		p := periods[attachIdx]
		if r.Intn(8) == 0 {
			itemStart = p.Start // attach exactly on a boundary
		} else {
			span := p.End.Sub(p.Start)
			itemStart = p.Start.Add(time.Duration(1+r.Int63n(int64(span-time.Second)/int64(time.Second))) * time.Second)
		}
	}

	item := &subscription.SubscriptionLineItem{
		ID: types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION_LINE_ITEM), PriceID: pr.ID,
		PriceType: types.PRICE_TYPE_FIXED, DisplayName: "prop", Quantity: decimal.NewFromInt(1), Currency: "usd",
		BillingPeriod: sc.itemCad.p, BillingPeriodCount: sc.itemCad.n, InvoiceCadence: sc.cadence,
		StartDate: itemStart, BaseModel: types.GetDefaultBaseModel(ctx),
	}
	sub := &subscription.Subscription{
		ID: types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION), CustomerID: "cust_prop",
		StartDate: start, BillingAnchor: anchor, BillingCycle: sc.cycle, BillingPeriod: sc.subCad.p,
		BillingPeriodCount: sc.subCad.n, Timezone: sc.tz, Currency: "usd", ProrationBehavior: sc.behavior,
		SubscriptionStatus: types.SubscriptionStatusActive, LineItems: []*subscription.SubscriptionLineItem{item},
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	item.SubscriptionID = sub.ID

	withPeriod := func(p types.Period) *subscription.Subscription {
		cp := *sub
		cp.CurrentPeriodStart, cp.CurrentPeriodEnd = p.Start, p.End
		return &cp
	}
	fixed := func(p types.Period, items []*subscription.SubscriptionLineItem, src string) ([]propLine, bool) {
		if len(items) == 0 {
			return nil, true
		}
		cp := withPeriod(p)
		cp.LineItems = items
		out, err := s.billing.CalculateFixedCharges(ctx, &dto.CalculateFixedChargesParams{Subscription: cp, PeriodStart: p.Start, PeriodEnd: p.End})
		if err != nil {
			res.failf(sc, seed, "%s CalculateFixedCharges [%s, %s): %v", src, p.Start, p.End, err)
			return nil, false
		}
		lines := make([]propLine, 0, len(out.LineItems))
		for _, l := range out.LineItems {
			lines = append(lines, propLine{lo.FromPtr(l.PeriodStart), lo.FromPtr(l.PeriodEnd), l.Amount, src})
		}
		return lines, true
	}

	var lines []propLine

	// Attach: the opening invoice at the sub start, or the attach charge mid-life.
	if !sc.midAttach {
		cl := s.billing.ClassifyLineItems(&dto.ClassifyLineItemsParams{Subscription: withPeriod(periods[0]),
			CurrentPeriodStart: periods[0].Start, CurrentPeriodEnd: periods[0].End,
			NextPeriodStart: periods[1].Start, NextPeriodEnd: periods[1].End})
		got, ok := fixed(periods[0], cl.CurrentPeriodAdvance, "opening")
		if !ok {
			return
		}
		lines = append(lines, got...)
	} else {
		summary, err := s.lip.Compute(ctx, LineItemProrationRequest{
			Subscription: withPeriod(periods[attachIdx]), EffectiveDate: itemStart, Behavior: sc.behavior,
			Entries: []LineItemProrationEntry{{LineItem: item, Action: types.ProrationActionAddItem, NewPrice: pr, NewQuantity: item.Quantity}},
		})
		if err != nil {
			res.failf(sc, seed, "attach Compute: %v", err)
			return
		}
		for _, l := range summary.ChargeLineItems {
			lines = append(lines, propLine{lo.FromPtr(l.PeriodStart), lo.FromPtr(l.PeriodEnd), l.Amount, "attach"})
		}

		// Parity (D3): the attach charge equals the invoice an item starting on that day would get.
		if sc.cadence == types.InvoiceCadenceAdvance && sc.behavior == types.ProrationBehaviorCreateProrations {
			inv, ok := fixed(periods[attachIdx], []*subscription.SubscriptionLineItem{item}, "parity")
			if !ok {
				return
			}
			sumInv := lo.Reduce(inv, func(a decimal.Decimal, l propLine, _ int) decimal.Decimal { return a.Add(l.amount) }, decimal.Zero)
			if !sumInv.Equal(summary.TotalChargeAmount) {
				res.failf(sc, seed, "parity: attach %s vs invoice %s (start %s, period %s→%s)", summary.TotalChargeAmount, sumInv, itemStart, periods[attachIdx].Start, periods[attachIdx].End)
			}
		}
	}

	// Period-end invoices: arrear for the period, advance for the next one.
	for i := attachIdx; i < n-1; i++ {
		cl := s.billing.ClassifyLineItems(&dto.ClassifyLineItemsParams{Subscription: withPeriod(periods[i]),
			CurrentPeriodStart: periods[i].Start, CurrentPeriodEnd: periods[i].End,
			NextPeriodStart: periods[i+1].Start, NextPeriodEnd: periods[i+1].End})
		arr, ok := fixed(periods[i], cl.CurrentPeriodArrear, fmt.Sprintf("arrear#%d", i))
		if !ok {
			return
		}
		adv, ok := fixed(periods[i+1], cl.NextPeriodAdvance, fmt.Sprintf("advance#%d", i+1))
		if !ok {
			return
		}
		lines = append(lines, arr...)
		lines = append(lines, adv...)
	}

	s.checkLines(sc, seed, res, sub, amount, periods, attachIdx, itemStart, loc, lines)
	if !sc.midAttach && sc.cadence == types.InvoiceCadenceAdvance {
		s.checkCredits(sc, seed, res, sub, item, pr, periods, r, loc)
	}
}

// itemGridAnchor mirrors the decided anchors: the sub's for same/shorter items; for longer items the
// item's calendar boundary on calendar subs, else the sub's.
func propItemAnchor(sc propScenario, sub *subscription.Subscription, itemStart time.Time, loc *time.Location) time.Time {
	longer := sc.itemCad.months() > 0 && sc.subCad.months() > 0 && sc.itemCad.months() > sc.subCad.months()
	if longer && sc.cycle == types.BillingCycleCalendar {
		cal := propCalendarAnchor(itemStart, sc.itemCad.p, loc)
		if _, onSubGrid := propGridIndex(sub.BillingAnchor, sc.subCad, cal, loc); onSubGrid {
			return cal
		}
	}
	return sub.BillingAnchor
}

// propWant is the oracle amount for [start, end) where end is on the item grid.
func propWant(sc propScenario, anchor time.Time, cost decimal.Decimal, start, end time.Time, loc *time.Location) (decimal.Decimal, time.Time, bool) {
	k, ok := propGridIndex(anchor, sc.itemCad, end, loc)
	if !ok {
		return decimal.Zero, time.Time{}, false
	}
	prev := propGridDate(anchor, sc.itemCad, k-1, loc)
	if sc.behavior == types.ProrationBehaviorNone || !start.After(prev) {
		return cost, prev, true
	}
	used, full := propSecs(start, end), propSecs(prev, end)
	return cost.Mul(decimal.NewFromInt(used).Div(decimal.NewFromInt(full))).Round(2), prev, true
}

// propExpectedFirst is where billing should start: the item start, or under none (current D5 behavior)
// the next sub period for an attached advance item, else the item's next boundary.
func propExpectedFirst(sc propScenario, anchor time.Time, periods []types.Period, attachIdx int, itemStart time.Time, loc *time.Location) time.Time {
	if sc.behavior != types.ProrationBehaviorNone || !sc.midAttach {
		return itemStart
	}
	strictlyNext := func() time.Time {
		return propNextBoundary(anchor, sc.itemCad, itemStart.Add(time.Second), loc)
	}
	if sc.cadence == types.InvoiceCadenceAdvance {
		if sc.cadenceShorterOrEqual() {
			return periods[attachIdx+1].Start
		}
		return strictlyNext()
	}
	if sc.cadenceShorterOrEqual() {
		return propNextBoundary(anchor, sc.itemCad, itemStart, loc)
	}
	if itemStart.Equal(periods[attachIdx].Start) {
		return itemStart
	}
	return strictlyNext()
}

func (s *PropSuite) checkLines(sc propScenario, seed int64, res *propResult, sub *subscription.Subscription,
	cost decimal.Decimal, periods []types.Period, attachIdx int, itemStart time.Time, loc *time.Location, lines []propLine) {
	n := len(periods)
	anchor := propItemAnchor(sc, sub, itemStart, loc)
	expectedFirst := propExpectedFirst(sc, anchor, periods, attachIdx, itemStart, loc)
	zeroSegment := func(a, b time.Time) bool {
		w, _, ok := propWant(sc, anchor, cost, a, b, loc)
		return ok && w.IsZero()
	}

	if len(lines) == 0 {
		var reachable bool
		if sc.cadence == types.InvoiceCadenceAdvance {
			reachable = expectedFirst.Before(periods[n-1].Start)
		} else {
			reachable = !propNextBoundary(anchor, sc.itemCad, expectedFirst.Add(time.Second), loc).After(periods[n-2].End)
		}
		if reachable {
			res.failf(sc, seed, "no lines billed (start %s, expected from %s)", itemStart, expectedFirst)
		}
		return
	}
	sort.Slice(lines, func(i, j int) bool { return lines[i].start.Before(lines[j].start) })

	for i, l := range lines {
		if !l.end.After(l.start) {
			res.failf(sc, seed, "empty line %s [%s, %s)", l.src, l.start, l.end)
		}
		if i > 0 {
			prev := lines[i-1]
			if l.start.Before(prev.end) {
				res.failf(sc, seed, "OVERLAP %s [%s, %s) with %s [%s, %s)", prev.src, prev.start, prev.end, l.src, l.start, l.end)
			} else if l.start.After(prev.end) && !zeroSegment(prev.end, l.start) {
				res.failf(sc, seed, "GAP between %s end %s and %s start %s", prev.src, prev.end, l.src, l.start)
			}
		}
	}

	// Coverage start.
	first := lines[0]
	if !first.start.Equal(expectedFirst) && !(first.start.After(expectedFirst) && zeroSegment(expectedFirst, first.start)) {
		res.failf(sc, seed, "coverage starts %s, expected %s (item start %s)", first.start, expectedFirst, itemStart)
	}

	// Coverage end.
	last := lines[len(lines)-1]
	if sc.cadence == types.InvoiceCadenceAdvance {
		if last.end.Before(periods[n-1].End) {
			res.failf(sc, seed, "advance coverage ends %s before horizon %s", last.end, periods[n-1].End)
		}
	} else {
		horizon := periods[n-2].End
		if last.end.After(horizon) {
			res.failf(sc, seed, "arrear billed beyond its period: %s > %s", last.end, horizon)
		}
		if k, ok := propGridIndex(anchor, sc.itemCad, last.end, loc); ok {
			if nextEnd := propGridDate(anchor, sc.itemCad, k+1, loc); !nextEnd.After(horizon) {
				res.failf(sc, seed, "arrear item period ending %s (<= horizon %s) was never billed", nextEnd, horizon)
			}
		}
	}

	// Amounts against the independent grid.
	for _, l := range lines {
		want, prevBoundary, ok := propWant(sc, anchor, cost, l.start, l.end, loc)
		if !ok {
			res.failf(sc, seed, "%s line end %s is not on the item grid (anchor %s)", l.src, l.end, anchor)
			continue
		}
		// DAILY/WEEKLY first periods still absorb a sliver earlier on the anchor's day.
		if l.start.Before(prevBoundary) && !(sc.subCad.months() == 0 && propSameLocalDay(l.start, prevBoundary, loc)) {
			res.failf(sc, seed, "%s line [%s, %s) spans more than one item period (boundary %s)", l.src, l.start, l.end, prevBoundary)
		}
		if !l.amount.Equal(want) {
			res.failf(sc, seed, "%s line [%s, %s) amount %s, want %s (cost %s)", l.src, l.start, l.end, l.amount, want, cost)
		}
	}
}

func (sc propScenario) cadenceShorterOrEqual() bool {
	return !(sc.itemCad.months() > 0 && sc.subCad.months() > 0 && sc.itemCad.months() > sc.subCad.months())
}

// checkCredits: remove == cancel, and add at t == remove at t, for an item active since the sub start.
func (s *PropSuite) checkCredits(sc propScenario, seed int64, res *propResult, sub *subscription.Subscription, item *subscription.SubscriptionLineItem,
	pr *price.Price, periods []types.Period, r *rand.Rand, loc *time.Location) {
	ctx := s.GetContext()
	k := r.Intn(min(4, len(periods)-1))
	p := periods[k]
	t := p.Start.Add(time.Duration(r.Int63n(int64(p.End.Sub(p.Start)/time.Second))) * time.Second)

	cp := *sub
	cp.CurrentPeriodStart, cp.CurrentPeriodEnd = p.Start, p.End

	removed, err := s.lip.Compute(ctx, LineItemProrationRequest{Subscription: &cp, EffectiveDate: t, Behavior: sc.behavior,
		Entries: []LineItemProrationEntry{{LineItem: item, Action: types.ProrationActionRemoveItem, CurrentPrice: pr, CurrentQuantity: item.Quantity}}})
	if err != nil {
		res.failf(sc, seed, "remove Compute at %s: %v", t, err)
		return
	}
	cancelled, err := NewProrationService(s.params).CalculateSubscriptionCancellationProration(ctx, &cp,
		[]*subscription.SubscriptionLineItem{item}, types.CancellationTypeImmediate, t, "prop", sc.behavior)
	if err != nil {
		res.failf(sc, seed, "cancel at %s: %v", t, err)
		return
	}
	if !removed.TotalCreditAmount.Equal(cancelled.TotalProrationAmount.Neg()) {
		res.failf(sc, seed, "remove credit %s != cancel credit %s at %s", removed.TotalCreditAmount, cancelled.TotalProrationAmount.Neg(), t)
	}
	if sc.behavior == types.ProrationBehaviorNone && !removed.TotalCreditAmount.IsZero() {
		res.failf(sc, seed, "none: remove credited %s", removed.TotalCreditAmount)
	}

	// add at t (item starting at t) must cost what removing at t credits, when both sit on one grid
	longerCalendarMulti := !sc.cadenceShorterOrEqual() && sc.cycle == types.BillingCycleCalendar && sc.itemCad.n > 1
	if longerCalendarMulti || sc.behavior == types.ProrationBehaviorNone {
		return
	}
	added := *item
	added.ID = types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION_LINE_ITEM)
	added.StartDate = t
	addSummary, err := s.lip.Compute(ctx, LineItemProrationRequest{Subscription: &cp, EffectiveDate: t, Behavior: sc.behavior,
		Entries: []LineItemProrationEntry{{LineItem: &added, Action: types.ProrationActionAddItem, NewPrice: pr, NewQuantity: added.Quantity}}})
	if err != nil {
		res.failf(sc, seed, "add Compute at %s: %v", t, err)
		return
	}
	if !addSummary.TotalChargeAmount.Equal(removed.TotalCreditAmount) {
		res.failf(sc, seed, "add charge %s != remove credit %s at %s (period %s→%s)", addSummary.TotalChargeAmount, removed.TotalCreditAmount, t, p.Start, p.End)
	}
}
