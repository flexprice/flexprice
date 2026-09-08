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

// MultiCadenceAddonMatrixSuite pins the invariant that an addon costs the same whether it was
// attached while the subscription was created (priced by CalculateFixedCharges) or attached
// afterwards (priced by LineItemProrationService), across 1:1 and multi-cadence setups.
type MultiCadenceAddonMatrixSuite struct {
	testutil.BaseServiceTestSuite
	params  ServiceParams
	billing BillingService
}

func TestMultiCadenceAddonMatrix(t *testing.T) {
	suite.Run(t, new(MultiCadenceAddonMatrixSuite))
}

func (s *MultiCadenceAddonMatrixSuite) SetupTest() {
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

func (s *MultiCadenceAddonMatrixSuite) TearDownTest() {
	s.BaseServiceTestSuite.TearDownTest()
}

func d(y int, m time.Month, day int) time.Time {
	return time.Date(y, m, day, 0, 0, 0, 0, time.UTC)
}

// scenario is one subscription with a plan line item and (optionally) an addon line item.
type scenario struct {
	sub     *subscription.Subscription
	planLI  *subscription.SubscriptionLineItem
	addonLI *subscription.SubscriptionLineItem
	addonPr *price.Price
}

type scenarioSpec struct {
	name              string
	cycle             types.BillingCycle
	subPeriod         types.BillingPeriod
	periodStart       time.Time
	periodEnd         time.Time
	anchor            time.Time
	prorationBehavior types.ProrationBehavior
	planAmount        int64
	addonPeriod       types.BillingPeriod // empty = no addon
	addonAmount       int64
	addonStart        time.Time
}

func (s *MultiCadenceAddonMatrixSuite) build(spec scenarioSpec) *scenario {
	ctx := s.GetContext()
	id := types.GenerateUUIDWithPrefix("mx")

	cust := &customer.Customer{ID: "cust_" + id, ExternalID: "ext_" + id, Name: "MX", BaseModel: types.GetDefaultBaseModel(ctx)}
	s.NoError(s.GetStores().CustomerRepo.Create(ctx, cust))
	pl := &plan.Plan{ID: "plan_" + id, Name: "MX Plan", BaseModel: types.GetDefaultBaseModel(ctx)}
	s.NoError(s.GetStores().PlanRepo.Create(ctx, pl))

	planPrice := &price.Price{
		ID: "price_plan_" + id, Amount: decimal.NewFromInt(spec.planAmount), Currency: "usd",
		EntityType: types.PRICE_ENTITY_TYPE_PLAN, EntityID: pl.ID, Type: types.PRICE_TYPE_FIXED,
		BillingPeriod: spec.subPeriod, BillingPeriodCount: 1, BillingModel: types.BILLING_MODEL_FLAT_FEE,
		BillingCadence: types.BILLING_CADENCE_RECURRING, InvoiceCadence: types.InvoiceCadenceAdvance,
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().PriceRepo.Create(ctx, planPrice))

	sub := &subscription.Subscription{
		ID: "sub_" + id, PlanID: pl.ID, CustomerID: cust.ID,
		StartDate: spec.periodStart, BillingAnchor: spec.anchor,
		CurrentPeriodStart: spec.periodStart, CurrentPeriodEnd: spec.periodEnd,
		Currency: "usd", BillingPeriod: spec.subPeriod, BillingPeriodCount: 1,
		BillingCycle: spec.cycle, SubscriptionStatus: types.SubscriptionStatusActive,
		Timezone: "UTC", ProrationBehavior: spec.prorationBehavior,
		BaseModel: types.GetDefaultBaseModel(ctx),
	}

	planLI := &subscription.SubscriptionLineItem{
		ID: "li_plan_" + id, SubscriptionID: sub.ID, CustomerID: cust.ID,
		EntityID: pl.ID, EntityType: types.SubscriptionLineItemEntityTypePlan,
		PriceID: planPrice.ID, PriceType: types.PRICE_TYPE_FIXED, DisplayName: "Plan",
		Quantity: decimal.NewFromInt(1), Currency: "usd",
		BillingPeriod: spec.subPeriod, BillingPeriodCount: 1,
		InvoiceCadence: types.InvoiceCadenceAdvance, StartDate: spec.periodStart,
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	items := []*subscription.SubscriptionLineItem{planLI}

	sc := &scenario{sub: sub, planLI: planLI}
	if spec.addonPeriod != "" {
		addonPrice := &price.Price{
			ID: "price_addon_" + id, Amount: decimal.NewFromInt(spec.addonAmount), Currency: "usd",
			EntityType: types.PRICE_ENTITY_TYPE_ADDON, EntityID: "addon_" + id, Type: types.PRICE_TYPE_FIXED,
			BillingPeriod: spec.addonPeriod, BillingPeriodCount: 1, BillingModel: types.BILLING_MODEL_FLAT_FEE,
			BillingCadence: types.BILLING_CADENCE_RECURRING, InvoiceCadence: types.InvoiceCadenceAdvance,
			BaseModel: types.GetDefaultBaseModel(ctx),
		}
		s.NoError(s.GetStores().PriceRepo.Create(ctx, addonPrice))
		addonLI := &subscription.SubscriptionLineItem{
			ID: "li_addon_" + id, SubscriptionID: sub.ID, CustomerID: cust.ID,
			EntityID: addonPrice.EntityID, EntityType: types.SubscriptionLineItemEntityTypeAddon,
			PriceID: addonPrice.ID, PriceType: types.PRICE_TYPE_FIXED, DisplayName: "Addon",
			Quantity: decimal.NewFromInt(1), Currency: "usd",
			BillingPeriod: spec.addonPeriod, BillingPeriodCount: 1,
			InvoiceCadence: types.InvoiceCadenceAdvance, StartDate: spec.addonStart,
			BaseModel: types.GetDefaultBaseModel(ctx),
		}
		items = append(items, addonLI)
		sc.addonLI = addonLI
		sc.addonPr = addonPrice
	}

	s.NoError(s.GetStores().SubscriptionRepo.CreateWithLineItems(ctx, sub, items))
	sub.LineItems = items
	return sc
}

// atCreateAddonTotal prices the addon the way the opening invoice does.
func (s *MultiCadenceAddonMatrixSuite) atCreateAddonTotal(sc *scenario) decimal.Decimal {
	only := *sc.sub
	only.LineItems = []*subscription.SubscriptionLineItem{sc.addonLI}
	res, err := s.billing.CalculateFixedCharges(s.GetContext(), &dto.CalculateFixedChargesParams{
		Subscription: &only,
		PeriodStart:  sc.sub.CurrentPeriodStart,
		PeriodEnd:    sc.sub.CurrentPeriodEnd,
	})
	s.NoError(err)
	return res.TotalAmount
}

// attachLaterAddonTotal prices the addon the way a mid-cycle attach does.
func (s *MultiCadenceAddonMatrixSuite) attachLaterAddonTotal(sc *scenario) decimal.Decimal {
	summary, err := NewLineItemProrationService(s.params).Compute(s.GetContext(), LineItemProrationRequest{
		Subscription: sc.sub,
		Entries: []LineItemProrationEntry{{
			LineItem: sc.addonLI,
			Price:    sc.addonPr,
			Action:   types.ProrationActionAddItem,
		}},
		EffectiveDate: sc.addonLI.StartDate,
		Behavior:      types.ProrationBehaviorCreateProrations,
	})
	s.NoError(err)
	return summary.TotalChargeAmount
}

func (s *MultiCadenceAddonMatrixSuite) planCharge(sc *scenario) decimal.Decimal {
	only := *sc.sub
	only.LineItems = sc.sub.LineItems // keep the addon so mixed-cadence classification applies
	res, err := s.billing.CalculateFixedCharges(s.GetContext(), &dto.CalculateFixedChargesParams{
		Subscription: &only,
		PeriodStart:  sc.sub.CurrentPeriodStart,
		PeriodEnd:    sc.sub.CurrentPeriodEnd,
	})
	s.NoError(err)
	total := decimal.Zero
	for _, li := range res.LineItems {
		if li.SubscriptionLineItemID != nil && *li.SubscriptionLineItemID == sc.planLI.ID {
			total = total.Add(li.Amount)
		}
	}
	return total
}

func (s *MultiCadenceAddonMatrixSuite) equalMoney(want string, got decimal.Decimal, msg string) {
	expected := decimal.RequireFromString(want)
	s.True(got.Sub(expected).Abs().LessThanOrEqual(decimal.NewFromFloat(0.05)),
		"%s: want ~%s, got %s", msg, want, got)
}

// --- Parity: multi-cadence -------------------------------------------------

// Quarterly sub Jan 1 - Apr 1, $100 MONTHLY addon starting Feb 15.
// Monthly windows: [Feb 1, Mar 1) half-covered = 50, [Mar 1, Apr 1) full = 100.
func (s *MultiCadenceAddonMatrixSuite) TestMultiCadence_AnniversaryQuarter_Parity() {
	sc := s.build(scenarioSpec{
		cycle: types.BillingCycleAnniversary, subPeriod: types.BILLING_PERIOD_QUARTER,
		periodStart: d(2025, time.January, 1), periodEnd: d(2025, time.April, 1),
		anchor: d(2025, time.January, 1), prorationBehavior: types.ProrationBehaviorNone,
		planAmount: 300, addonPeriod: types.BILLING_PERIOD_MONTHLY, addonAmount: 100,
		addonStart: d(2025, time.February, 15),
	})

	s.equalMoney("150", s.atCreateAddonTotal(sc), "at-create addon total")
	s.equalMoney("150", s.attachLaterAddonTotal(sc), "attach-later addon total")
}

// --- Parity: 1:1 cadence (must not change) ---------------------------------

func (s *MultiCadenceAddonMatrixSuite) TestSameCadence_Monthly_Parity() {
	sc := s.build(scenarioSpec{
		cycle: types.BillingCycleAnniversary, subPeriod: types.BILLING_PERIOD_MONTHLY,
		periodStart: d(2025, time.January, 1), periodEnd: d(2025, time.February, 1),
		anchor: d(2025, time.January, 1), prorationBehavior: types.ProrationBehaviorNone,
		planAmount: 100, addonPeriod: types.BILLING_PERIOD_MONTHLY, addonAmount: 100,
		addonStart: d(2025, time.January, 15),
	})

	s.equalMoney("54.84", s.atCreateAddonTotal(sc), "at-create addon total")
	s.equalMoney("54.84", s.attachLaterAddonTotal(sc), "attach-later addon total")
}

func (s *MultiCadenceAddonMatrixSuite) TestSameCadence_Quarterly_Parity() {
	sc := s.build(scenarioSpec{
		cycle: types.BillingCycleAnniversary, subPeriod: types.BILLING_PERIOD_QUARTER,
		periodStart: d(2025, time.January, 1), periodEnd: d(2025, time.April, 1),
		anchor: d(2025, time.January, 1), prorationBehavior: types.ProrationBehaviorNone,
		planAmount: 300, addonPeriod: types.BILLING_PERIOD_QUARTER, addonAmount: 300,
		addonStart: d(2025, time.February, 15),
	})

	s.equalMoney("150", s.atCreateAddonTotal(sc), "at-create addon total")
	s.equalMoney("150", s.attachLaterAddonTotal(sc), "attach-later addon total")
}

// --- Short first period ----------------------------------------------------

// Calendar quarterly sub whose first period is the stub Feb 15 - Apr 1. The plan is a $300
// quarterly price and must be prorated over the full Jan 1 - Apr 1 quarter (45/90), even
// though a monthly addon makes the subscription mixed-cadence.
func (s *MultiCadenceAddonMatrixSuite) TestCalendarStub_PlanProratesWithMixedCadenceAddon() {
	sc := s.build(scenarioSpec{
		cycle: types.BillingCycleCalendar, subPeriod: types.BILLING_PERIOD_QUARTER,
		periodStart: d(2025, time.February, 15), periodEnd: d(2025, time.April, 1),
		anchor: d(2025, time.April, 1), prorationBehavior: types.ProrationBehaviorCreateProrations,
		planAmount: 300, addonPeriod: types.BILLING_PERIOD_MONTHLY, addonAmount: 100,
		addonStart: d(2025, time.February, 20),
	})

	s.equalMoney("150", s.planCharge(sc), "plan charge on a calendar stub period")
}

// The [Feb 15, Mar 1) window is 14 days, but the addon's own period is a month. A $100
// monthly addon live for 9 of those days costs 100 x 9/28, not 100 x 9/14.
func (s *MultiCadenceAddonMatrixSuite) TestCalendarStub_AddonRatioUsesItsOwnPeriod() {
	sc := s.build(scenarioSpec{
		cycle: types.BillingCycleCalendar, subPeriod: types.BILLING_PERIOD_QUARTER,
		periodStart: d(2025, time.February, 15), periodEnd: d(2025, time.April, 1),
		anchor: d(2025, time.April, 1), prorationBehavior: types.ProrationBehaviorNone,
		planAmount: 300, addonPeriod: types.BILLING_PERIOD_MONTHLY, addonAmount: 100,
		addonStart: d(2025, time.February, 20),
	})

	s.equalMoney("132.14", s.atCreateAddonTotal(sc), "addon total across the stub")
}

// Anniversary with a custom anchor one month out: the first period is Feb 15 - Mar 15, a
// third of the quarterly plan's period, so the $300 plan costs 300 x 28/90. No addon here,
// so this is independent of the mixed-cadence path.
func (s *MultiCadenceAddonMatrixSuite) TestAnniversaryCustomAnchorStub_PlanProrates() {
	sc := s.build(scenarioSpec{
		cycle: types.BillingCycleAnniversary, subPeriod: types.BILLING_PERIOD_QUARTER,
		periodStart: d(2025, time.February, 15), periodEnd: d(2025, time.March, 15),
		anchor: d(2025, time.March, 15), prorationBehavior: types.ProrationBehaviorCreateProrations,
		planAmount: 300,
	})

	s.equalMoney("93.33", s.planCharge(sc), "plan charge on an anchored stub period")
}

// --- Ratio denominator: window vs the line item's own period ----------------

// A partial line item is charged as a fraction of its OWN billing period. These cases pin
// when the invoice window and that period diverge, and that the amount tracks the period.
func (s *MultiCadenceAddonMatrixSuite) TestRatioDenominator_WindowVsItemPeriod() {
	cases := []struct {
		name string
		spec scenarioSpec
		want string
		why  string
	}{
		{
			name: "same cadence, full window: window == item period",
			spec: scenarioSpec{
				cycle: types.BillingCycleAnniversary, subPeriod: types.BILLING_PERIOD_MONTHLY,
				periodStart: d(2025, time.January, 1), periodEnd: d(2025, time.February, 1),
				anchor: d(2025, time.January, 1), planAmount: 100,
				addonPeriod: types.BILLING_PERIOD_MONTHLY, addonAmount: 100,
				addonStart: d(2025, time.January, 15),
			},
			want: "54.84",
			why:  "17 of 31 days; window and month agree so nothing changes",
		},
		{
			name: "same cadence, calendar stub: window shorter than the month",
			spec: scenarioSpec{
				cycle: types.BillingCycleCalendar, subPeriod: types.BILLING_PERIOD_MONTHLY,
				periodStart: d(2025, time.January, 20), periodEnd: d(2025, time.February, 1),
				anchor: d(2025, time.February, 1), planAmount: 100,
				addonPeriod: types.BILLING_PERIOD_MONTHLY, addonAmount: 100,
				addonStart: d(2025, time.January, 25),
			},
			want: "22.58",
			why:  "7 days of a month (31d), not 7 of the 12-day stub",
		},
		{
			name: "multi cadence, single clamped window",
			spec: scenarioSpec{
				cycle: types.BillingCycleCalendar, subPeriod: types.BILLING_PERIOD_QUARTER,
				periodStart: d(2025, time.September, 8), periodEnd: d(2025, time.October, 1),
				anchor: d(2025, time.October, 1), planAmount: 300,
				addonPeriod: types.BILLING_PERIOD_MONTHLY, addonAmount: 100,
				addonStart: d(2025, time.September, 21),
			},
			want: "33.33",
			why:  "fan-out collapses to one 23-day window; divisor is still the 30-day month",
		},
		{
			name: "multi cadence, whole interior windows: ratio never applies",
			spec: scenarioSpec{
				cycle: types.BillingCycleAnniversary, subPeriod: types.BILLING_PERIOD_QUARTER,
				periodStart: d(2025, time.January, 1), periodEnd: d(2025, time.April, 1),
				anchor: d(2025, time.January, 1), planAmount: 300,
				addonPeriod: types.BILLING_PERIOD_MONTHLY, addonAmount: 100,
				addonStart: d(2025, time.February, 1),
			},
			want: "200",
			why:  "addon covers Feb and Mar in full, so both windows bill at list price",
		},
	}

	for _, tc := range cases {
		s.Run(tc.name, func() {
			tc.spec.prorationBehavior = types.ProrationBehaviorNone
			sc := s.build(tc.spec)
			s.equalMoney(tc.want, s.atCreateAddonTotal(sc), tc.why)
		})
	}
}
