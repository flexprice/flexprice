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
	addonArrear       bool
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
		addonCadence := types.InvoiceCadenceAdvance
		if spec.addonArrear {
			addonCadence = types.InvoiceCadenceArrear
		}
		addonPrice := &price.Price{
			ID: "price_addon_" + id, Amount: decimal.NewFromInt(spec.addonAmount), Currency: "usd",
			EntityType: types.PRICE_ENTITY_TYPE_ADDON, EntityID: "addon_" + id, Type: types.PRICE_TYPE_FIXED,
			BillingPeriod: spec.addonPeriod, BillingPeriodCount: 1, BillingModel: types.BILLING_MODEL_FLAT_FEE,
			BillingCadence: types.BILLING_CADENCE_RECURRING, InvoiceCadence: addonCadence,
			BaseModel: types.GetDefaultBaseModel(ctx),
		}
		s.NoError(s.GetStores().PriceRepo.Create(ctx, addonPrice))
		addonLI := &subscription.SubscriptionLineItem{
			ID: "li_addon_" + id, SubscriptionID: sub.ID, CustomerID: cust.ID,
			EntityID: addonPrice.EntityID, EntityType: types.SubscriptionLineItemEntityTypeAddon,
			PriceID: addonPrice.ID, PriceType: types.PRICE_TYPE_FIXED, DisplayName: "Addon",
			Quantity: decimal.NewFromInt(1), Currency: "usd",
			BillingPeriod: spec.addonPeriod, BillingPeriodCount: 1,
			InvoiceCadence: addonCadence, StartDate: spec.addonStart,
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
			NewPrice: sc.addonPr, NewQuantity: sc.addonLI.Quantity,
			Action: types.ProrationActionAddItem,
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
		anchor: d(2025, time.January, 1), prorationBehavior: types.ProrationBehaviorCreateProrations,
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
		anchor: d(2025, time.January, 1), prorationBehavior: types.ProrationBehaviorCreateProrations,
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
		anchor: d(2025, time.January, 1), prorationBehavior: types.ProrationBehaviorCreateProrations,
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
		anchor: d(2025, time.April, 1), prorationBehavior: types.ProrationBehaviorCreateProrations,
		planAmount: 300, addonPeriod: types.BILLING_PERIOD_MONTHLY, addonAmount: 100,
		addonStart: d(2025, time.February, 20),
	})

	s.equalMoney("132.14", s.atCreateAddonTotal(sc), "addon total across the stub")
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
			tc.spec.prorationBehavior = types.ProrationBehaviorCreateProrations
			sc := s.build(tc.spec)
			s.equalMoney(tc.want, s.atCreateAddonTotal(sc), tc.why)
		})
	}
}

// A stub period shorter than the addon's own month: at-create and attach-later must agree,
// and both must charge a fraction of the month rather than of the stub.
func (s *MultiCadenceAddonMatrixSuite) TestCalendarStub_Parity() {
	sc := s.build(scenarioSpec{
		cycle: types.BillingCycleCalendar, subPeriod: types.BILLING_PERIOD_QUARTER,
		periodStart: d(2025, time.September, 8), periodEnd: d(2025, time.October, 1),
		anchor: d(2025, time.October, 1), prorationBehavior: types.ProrationBehaviorCreateProrations,
		planAmount: 300, addonPeriod: types.BILLING_PERIOD_MONTHLY, addonAmount: 100,
		addonStart: d(2025, time.September, 21),
	})

	s.equalMoney("33.33", s.atCreateAddonTotal(sc), "at-create addon total")
	s.equalMoney("33.33", s.attachLaterAddonTotal(sc), "attach-later addon total")
}

// --- Whole periods are never prorated --------------------------------------

// --- Detach ----------------------------------------------------------------

// --- Arrear ----------------------------------------------------------------

// An arrear addon is billed at the end of each period, so attaching mid-period raises no
// upfront charge for any window.
func (s *MultiCadenceAddonMatrixSuite) TestArrearAddon_AttachRaisesNoCharge() {
	sc := s.build(scenarioSpec{
		cycle: types.BillingCycleAnniversary, subPeriod: types.BILLING_PERIOD_QUARTER,
		periodStart: d(2025, time.January, 1), periodEnd: d(2025, time.April, 1),
		anchor: d(2025, time.January, 1), prorationBehavior: types.ProrationBehaviorNone,
		planAmount: 300, addonPeriod: types.BILLING_PERIOD_MONTHLY, addonAmount: 100,
		addonStart: d(2025, time.February, 15), addonArrear: true,
	})

	s.True(s.attachLaterAddonTotal(sc).IsZero(),
		"arrear attach must not charge upfront, got %s", s.attachLaterAddonTotal(sc))
}

// --- Cancellation ----------------------------------------------------------

// Cancelling a mixed-cadence subscription still bills its fixed charges; step 3 opened this
// path by scoping the mixed-cadence bail per line item.
func (s *MultiCadenceAddonMatrixSuite) TestCancelledSubscription_MixedCadenceStillBills() {
	sc := s.build(scenarioSpec{
		cycle: types.BillingCycleAnniversary, subPeriod: types.BILLING_PERIOD_QUARTER,
		periodStart: d(2025, time.January, 1), periodEnd: d(2025, time.April, 1),
		anchor: d(2025, time.January, 1), prorationBehavior: types.ProrationBehaviorCreateProrations,
		planAmount: 300, addonPeriod: types.BILLING_PERIOD_MONTHLY, addonAmount: 100,
		addonStart: d(2025, time.January, 1),
	})
	sc.sub.SubscriptionStatus = types.SubscriptionStatusCancelled

	res, err := s.billing.CalculateFixedCharges(s.GetContext(), &dto.CalculateFixedChargesParams{
		Subscription: sc.sub,
		PeriodStart:  sc.sub.CurrentPeriodStart,
		PeriodEnd:    sc.sub.CurrentPeriodEnd,
	})
	s.NoError(err)
	for _, li := range res.LineItems {
		s.False(li.Amount.IsNegative(), "a fixed charge line must not be negative, got %s", li.Amount)
	}
	s.equalMoney("600", res.TotalAmount, "plan 300 + three monthly addon windows")
}

// An addon that starts with the subscription still only gets the days a calendar stub covers,
// so its charge must match what attaching it a moment later would cost.
func (s *MultiCadenceAddonMatrixSuite) TestCalendarStub_AddonStartingAtPeriodStart_Parity() {
	sc := s.build(scenarioSpec{
		cycle: types.BillingCycleCalendar, subPeriod: types.BILLING_PERIOD_QUARTER,
		periodStart: d(2025, time.September, 8), periodEnd: d(2025, time.October, 1),
		anchor: d(2025, time.October, 1), prorationBehavior: types.ProrationBehaviorCreateProrations,
		planAmount: 300, addonPeriod: types.BILLING_PERIOD_MONTHLY, addonAmount: 100,
		addonStart: d(2025, time.September, 8),
	})

	s.equalMoney("76.67", s.atCreateAddonTotal(sc), "at-create addon total")
	s.equalMoney("76.67", s.attachLaterAddonTotal(sc), "attach-later addon total")
}

func pvmDate(y int, m time.Month, day int) time.Time {
	return time.Date(y, m, day, 0, 0, 0, 0, time.UTC)
}

func pvmLocalDate(tz string, y int, m time.Month, day int) time.Time {
	loc, err := time.LoadLocation(tz)
	if err != nil {
		panic(err)
	}
	return time.Date(y, m, day, 0, 0, 0, 0, loc).UTC()
}

type pvmSubSpec struct {
	cycle       types.BillingCycle
	period      types.BillingPeriod
	periodCount int
	start       time.Time
	periodStart time.Time
	periodEnd   time.Time
	anchor      time.Time
	behavior    types.ProrationBehavior
	timezone    string
	endDate     *time.Time
	grouping    types.LineItemGrouping
}

type pvmItemSpec struct {
	period    types.BillingPeriod
	count     int
	amount    int64
	start     time.Time
	end       time.Time
	arrear    bool
	priceType types.PriceType
}

type pvmFixture struct {
	sub    *subscription.Subscription
	items  []*subscription.SubscriptionLineItem
	prices []*price.Price
}

func (s *MultiCadenceAddonMatrixSuite) pvmBuild(spec pvmSubSpec, itemSpecs ...pvmItemSpec) *pvmFixture {
	ctx := s.GetContext()
	id := types.GenerateUUIDWithPrefix("pvm")

	cust := &customer.Customer{ID: "cust_" + id, ExternalID: "ext_" + id, Name: "PVM", BaseModel: types.GetDefaultBaseModel(ctx)}
	s.Require().NoError(s.GetStores().CustomerRepo.Create(ctx, cust))
	pl := &plan.Plan{ID: "plan_" + id, Name: "PVM Plan", BaseModel: types.GetDefaultBaseModel(ctx)}
	s.Require().NoError(s.GetStores().PlanRepo.Create(ctx, pl))

	tz := spec.timezone
	if tz == "" {
		tz = "UTC"
	}
	count := spec.periodCount
	if count == 0 {
		count = 1
	}
	start := spec.start
	if start.IsZero() {
		start = spec.periodStart
	}
	sub := &subscription.Subscription{
		ID: "sub_" + id, PlanID: pl.ID, CustomerID: cust.ID,
		StartDate: start, BillingAnchor: spec.anchor, EndDate: spec.endDate,
		CurrentPeriodStart: spec.periodStart, CurrentPeriodEnd: spec.periodEnd,
		Currency: "usd", BillingPeriod: spec.period, BillingPeriodCount: count,
		BillingCycle: spec.cycle, SubscriptionStatus: types.SubscriptionStatusActive,
		Timezone: tz, ProrationBehavior: spec.behavior, LineItemGrouping: spec.grouping,
		BaseModel: types.GetDefaultBaseModel(ctx),
	}

	fx := &pvmFixture{sub: sub}
	for i, is := range itemSpecs {
		cadence := types.InvoiceCadenceAdvance
		if is.arrear {
			cadence = types.InvoiceCadenceArrear
		}
		priceType := is.priceType
		if priceType == "" {
			priceType = types.PRICE_TYPE_FIXED
		}
		itemCount := is.count
		if itemCount == 0 {
			itemCount = 1
		}
		billingCadence := types.BILLING_CADENCE_RECURRING
		pr := &price.Price{
			ID: types.GenerateUUIDWithPrefix("price_pvm"), Amount: decimal.NewFromInt(is.amount), Currency: "usd",
			EntityType: types.PRICE_ENTITY_TYPE_PLAN, EntityID: pl.ID, Type: priceType,
			BillingPeriod: is.period, BillingPeriodCount: itemCount, BillingModel: types.BILLING_MODEL_FLAT_FEE,
			BillingCadence: billingCadence, InvoiceCadence: cadence,
			BaseModel: types.GetDefaultBaseModel(ctx),
		}
		s.Require().NoError(s.GetStores().PriceRepo.Create(ctx, pr))
		li := &subscription.SubscriptionLineItem{
			ID: types.GenerateUUIDWithPrefix("li_pvm"), SubscriptionID: sub.ID, CustomerID: cust.ID,
			EntityID: pl.ID, EntityType: types.SubscriptionLineItemEntityTypePlan,
			PriceID: pr.ID, PriceType: priceType, DisplayName: "item" + string(rune('A'+i)),
			Quantity: decimal.NewFromInt(1), Currency: "usd",
			BillingPeriod: is.period, BillingPeriodCount: itemCount,
			InvoiceCadence: cadence, StartDate: is.start, EndDate: is.end,
			BaseModel: types.GetDefaultBaseModel(ctx),
		}
		fx.items = append(fx.items, li)
		fx.prices = append(fx.prices, pr)
	}
	s.Require().NoError(s.GetStores().SubscriptionRepo.CreateWithLineItems(ctx, sub, fx.items))
	sub.LineItems = fx.items
	return fx
}

func (s *MultiCadenceAddonMatrixSuite) pvmFixed(sub *subscription.Subscription, start, end time.Time) (*dto.CalculateFixedChargesResult, error) {
	return s.billing.CalculateFixedCharges(s.GetContext(), &dto.CalculateFixedChargesParams{
		Subscription: sub, PeriodStart: start, PeriodEnd: end,
	})
}

func (s *MultiCadenceAddonMatrixSuite) pvmMoney(want string, got decimal.Decimal, msg string, args ...interface{}) {
	expected := decimal.RequireFromString(want)
	s.Truef(got.Sub(expected).Abs().LessThanOrEqual(decimal.NewFromFloat(0.05)),
		"want ~%s, got %s: "+msg, append([]interface{}{want, got.StringFixed(2)}, args...)...)
}

// --- Shorter-cadence items on calendar stubs --------------------------------

// Shorter-cadence item on a calendar sub whose first period is a stub. The stub window is
// prorated against the item-cadence period it belongs to; whole windows bill 1x.
func (s *MultiCadenceAddonMatrixSuite) TestShorterCadence_CalendarStub_FixedCharges() {
	cases := []struct {
		name     string
		sub      pvmSubSpec
		item     pvmItemSpec
		want     string
		wantRows int
	}{
		{
			name: "quarterly item on calendar annual stub",
			sub: pvmSubSpec{cycle: types.BillingCycleCalendar, period: types.BILLING_PERIOD_ANNUAL,
				periodStart: pvmDate(2026, 2, 15), periodEnd: pvmDate(2027, 1, 1), anchor: pvmDate(2027, 1, 1),
				behavior: types.ProrationBehaviorCreateProrations},
			item: pvmItemSpec{period: types.BILLING_PERIOD_QUARTER, amount: 300, start: pvmDate(2026, 2, 15)},
			// [Feb15,Apr1) 45/90 of Q1 = 150, then Q2..Q4 = 900
			want: "1050", wantRows: 4,
		},
		{
			name: "quarterly item on calendar annual stub, proration none",
			sub: pvmSubSpec{cycle: types.BillingCycleCalendar, period: types.BILLING_PERIOD_ANNUAL,
				periodStart: pvmDate(2026, 2, 15), periodEnd: pvmDate(2027, 1, 1), anchor: pvmDate(2027, 1, 1),
				behavior: types.ProrationBehaviorNone},
			item: pvmItemSpec{period: types.BILLING_PERIOD_QUARTER, amount: 300, start: pvmDate(2026, 2, 15)},
			want: "1200", wantRows: 4,
		},
		{
			name: "half-year item on calendar annual stub",
			sub: pvmSubSpec{cycle: types.BillingCycleCalendar, period: types.BILLING_PERIOD_ANNUAL,
				periodStart: pvmDate(2026, 2, 15), periodEnd: pvmDate(2027, 1, 1), anchor: pvmDate(2027, 1, 1),
				behavior: types.ProrationBehaviorCreateProrations},
			item: pvmItemSpec{period: types.BILLING_PERIOD_HALF_YEAR, amount: 600, start: pvmDate(2026, 2, 15)},
			// [Feb15,Jul1) 136/181 of H1 = 450.83, then H2 = 600
			want: "1050.83", wantRows: 2,
		},
		{
			name: "quarterly item on calendar half-year stub",
			sub: pvmSubSpec{cycle: types.BillingCycleCalendar, period: types.BILLING_PERIOD_HALF_YEAR,
				periodStart: pvmDate(2026, 2, 15), periodEnd: pvmDate(2026, 7, 1), anchor: pvmDate(2026, 7, 1),
				behavior: types.ProrationBehaviorCreateProrations},
			item: pvmItemSpec{period: types.BILLING_PERIOD_QUARTER, amount: 300, start: pvmDate(2026, 2, 15)},
			want: "450", wantRows: 2,
		},
		{
			name: "monthly item, 1-day stub on calendar quarterly: divisor is Jan (31d) not Jan31->Feb28 (28d)",
			sub: pvmSubSpec{cycle: types.BillingCycleCalendar, period: types.BILLING_PERIOD_QUARTER,
				periodStart: pvmDate(2026, 1, 31), periodEnd: pvmDate(2026, 4, 1), anchor: pvmDate(2026, 4, 1),
				behavior: types.ProrationBehaviorCreateProrations},
			item: pvmItemSpec{period: types.BILLING_PERIOD_MONTHLY, amount: 100, start: pvmDate(2026, 1, 31)},
			want: "203.23", wantRows: 3,
		},
		{
			name: "monthly item on calendar annual stub",
			sub: pvmSubSpec{cycle: types.BillingCycleCalendar, period: types.BILLING_PERIOD_ANNUAL,
				periodStart: pvmDate(2026, 2, 15), periodEnd: pvmDate(2027, 1, 1), anchor: pvmDate(2027, 1, 1),
				behavior: types.ProrationBehaviorCreateProrations},
			item: pvmItemSpec{period: types.BILLING_PERIOD_MONTHLY, amount: 100, start: pvmDate(2026, 2, 15)},
			want: "1050", wantRows: 11,
		},
		{
			name: "monthly item on calendar half-year stub",
			sub: pvmSubSpec{cycle: types.BillingCycleCalendar, period: types.BILLING_PERIOD_HALF_YEAR,
				periodStart: pvmDate(2026, 2, 15), periodEnd: pvmDate(2026, 7, 1), anchor: pvmDate(2026, 7, 1),
				behavior: types.ProrationBehaviorCreateProrations},
			item: pvmItemSpec{period: types.BILLING_PERIOD_MONTHLY, amount: 100, start: pvmDate(2026, 2, 15)},
			want: "450", wantRows: 5,
		},
		{
			name: "monthly item on calendar quarterly stub, proration none charges the stub window in full",
			sub: pvmSubSpec{cycle: types.BillingCycleCalendar, period: types.BILLING_PERIOD_QUARTER,
				periodStart: pvmDate(2026, 2, 15), periodEnd: pvmDate(2026, 4, 1), anchor: pvmDate(2026, 4, 1),
				behavior: types.ProrationBehaviorNone},
			item: pvmItemSpec{period: types.BILLING_PERIOD_MONTHLY, amount: 100, start: pvmDate(2026, 2, 15)},
			want: "200", wantRows: 2,
		},
		{
			name: "monthly arrear item on calendar quarterly stub",
			sub: pvmSubSpec{cycle: types.BillingCycleCalendar, period: types.BILLING_PERIOD_QUARTER,
				periodStart: pvmDate(2026, 2, 15), periodEnd: pvmDate(2026, 4, 1), anchor: pvmDate(2026, 4, 1),
				behavior: types.ProrationBehaviorCreateProrations},
			item: pvmItemSpec{period: types.BILLING_PERIOD_MONTHLY, amount: 100, start: pvmDate(2026, 2, 15), arrear: true},
			want: "150", wantRows: 2,
		},
		{
			name: "monthly item starting mid stub window",
			sub: pvmSubSpec{cycle: types.BillingCycleCalendar, period: types.BILLING_PERIOD_QUARTER,
				periodStart: pvmDate(2026, 2, 15), periodEnd: pvmDate(2026, 4, 1), anchor: pvmDate(2026, 4, 1),
				behavior: types.ProrationBehaviorCreateProrations},
			item: pvmItemSpec{period: types.BILLING_PERIOD_MONTHLY, amount: 100, start: pvmDate(2026, 2, 22)},
			// 7/28 + 1
			want: "125", wantRows: 2,
		},
		{
			name: "monthly item starting at interior boundary",
			sub: pvmSubSpec{cycle: types.BillingCycleCalendar, period: types.BILLING_PERIOD_QUARTER,
				periodStart: pvmDate(2026, 2, 15), periodEnd: pvmDate(2026, 4, 1), anchor: pvmDate(2026, 4, 1),
				behavior: types.ProrationBehaviorCreateProrations},
			item: pvmItemSpec{period: types.BILLING_PERIOD_MONTHLY, amount: 100, start: pvmDate(2026, 3, 1)},
			want: "100", wantRows: 1,
		},
		{
			name: "monthly item ending mid-period on anniversary quarterly",
			sub: pvmSubSpec{cycle: types.BillingCycleAnniversary, period: types.BILLING_PERIOD_QUARTER,
				periodStart: pvmDate(2026, 1, 1), periodEnd: pvmDate(2026, 4, 1), anchor: pvmDate(2026, 1, 1),
				behavior: types.ProrationBehaviorCreateProrations},
			item: pvmItemSpec{period: types.BILLING_PERIOD_MONTHLY, amount: 100, start: pvmDate(2026, 1, 1), end: pvmDate(2026, 2, 15)},
			// Jan full + 14/28 Feb
			want: "150", wantRows: 2,
		},
		{
			name: "quarterly item on anniversary annual",
			sub: pvmSubSpec{cycle: types.BillingCycleAnniversary, period: types.BILLING_PERIOD_ANNUAL,
				periodStart: pvmDate(2026, 2, 15), periodEnd: pvmDate(2027, 2, 15), anchor: pvmDate(2026, 2, 15),
				behavior: types.ProrationBehaviorCreateProrations},
			item: pvmItemSpec{period: types.BILLING_PERIOD_QUARTER, amount: 300, start: pvmDate(2026, 2, 15)},
			want: "1200", wantRows: 4,
		},
		{
			name: "quarterly item on calendar annual renewal (period after the stub)",
			sub: pvmSubSpec{cycle: types.BillingCycleCalendar, period: types.BILLING_PERIOD_ANNUAL,
				start: pvmDate(2026, 2, 15), periodStart: pvmDate(2027, 1, 1), periodEnd: pvmDate(2028, 1, 1), anchor: pvmDate(2027, 1, 1),
				behavior: types.ProrationBehaviorCreateProrations},
			item: pvmItemSpec{period: types.BILLING_PERIOD_QUARTER, amount: 300, start: pvmDate(2026, 2, 15)},
			want: "1200", wantRows: 4,
		},
		{
			name: "monthly item on anniversary annual anchored on the 31st",
			sub: pvmSubSpec{cycle: types.BillingCycleAnniversary, period: types.BILLING_PERIOD_ANNUAL,
				periodStart: pvmDate(2026, 1, 31), periodEnd: pvmDate(2027, 1, 31), anchor: pvmDate(2026, 1, 31),
				behavior: types.ProrationBehaviorCreateProrations},
			item: pvmItemSpec{period: types.BILLING_PERIOD_MONTHLY, amount: 100, start: pvmDate(2026, 1, 31)},
			want: "1200", wantRows: 12,
		},
	}

	for _, tc := range cases {
		s.Run(tc.name, func() {
			fx := s.pvmBuild(tc.sub, tc.item)
			res, err := s.pvmFixed(fx.sub, tc.sub.periodStart, tc.sub.periodEnd)
			s.Require().NoError(err)
			s.Equal(tc.wantRows, len(res.LineItems), "invoice rows (one per item-cadence window)")
			s.pvmMoney(tc.want, res.TotalAmount, "fixed total")
		})
	}
}

// The mid-cycle attach quote walks the same item-cadence windows as the invoice.
func (s *MultiCadenceAddonMatrixSuite) TestShorterCadence_CalendarAnnualStub_AttachLater() {
	fx := s.pvmBuild(pvmSubSpec{cycle: types.BillingCycleCalendar, period: types.BILLING_PERIOD_ANNUAL,
		periodStart: pvmDate(2026, 2, 15), periodEnd: pvmDate(2027, 1, 1), anchor: pvmDate(2027, 1, 1),
		behavior: types.ProrationBehaviorCreateProrations},
		pvmItemSpec{period: types.BILLING_PERIOD_QUARTER, amount: 300, start: pvmDate(2026, 3, 1)})

	summary, err := NewLineItemProrationService(s.params).Compute(s.GetContext(), LineItemProrationRequest{
		Subscription: fx.sub,
		Entries: []LineItemProrationEntry{{
			LineItem: fx.items[0], Action: types.ProrationActionAddItem,
			NewPrice: fx.prices[0], NewQuantity: decimal.NewFromInt(1),
		}},
		EffectiveDate: pvmDate(2026, 3, 1),
		Behavior:      types.ProrationBehaviorCreateProrations,
	})
	s.Require().NoError(err)
	// [Mar1,Apr1) 31/90 of Q1 = 103.33, then Q2..Q4 = 900
	s.pvmMoney("1003.33", summary.TotalChargeAmount, "attach-later quarterly item on calendar annual stub")
}

// Usage on a calendar stub is metered in one window per item-cadence period; a longer usage cadence
// is metered over each sub invoice period.
func (s *MultiCadenceAddonMatrixSuite) TestUsageWindows_FollowItemCadence() {
	cases := []struct {
		name string
		sub  pvmSubSpec
		item pvmItemSpec
		want []time.Time // window starts
	}{
		{
			name: "quarterly usage on calendar annual stub",
			sub: pvmSubSpec{cycle: types.BillingCycleCalendar, period: types.BILLING_PERIOD_ANNUAL,
				periodStart: pvmDate(2026, 2, 15), periodEnd: pvmDate(2027, 1, 1), anchor: pvmDate(2027, 1, 1)},
			item: pvmItemSpec{period: types.BILLING_PERIOD_QUARTER, priceType: types.PRICE_TYPE_USAGE, arrear: true, start: pvmDate(2026, 2, 15)},
			want: []time.Time{pvmDate(2026, 2, 15), pvmDate(2026, 4, 1), pvmDate(2026, 7, 1), pvmDate(2026, 10, 1)},
		},
		{
			name: "monthly usage on calendar quarterly stub in IST",
			sub: pvmSubSpec{cycle: types.BillingCycleCalendar, period: types.BILLING_PERIOD_QUARTER, timezone: "Asia/Kolkata",
				periodStart: pvmLocalDate("Asia/Kolkata", 2026, 2, 15), periodEnd: pvmLocalDate("Asia/Kolkata", 2026, 4, 1),
				anchor: pvmLocalDate("Asia/Kolkata", 2026, 4, 1)},
			item: pvmItemSpec{period: types.BILLING_PERIOD_MONTHLY, priceType: types.PRICE_TYPE_USAGE, arrear: true, start: pvmLocalDate("Asia/Kolkata", 2026, 2, 15)},
			want: []time.Time{pvmLocalDate("Asia/Kolkata", 2026, 2, 15), pvmLocalDate("Asia/Kolkata", 2026, 3, 1)},
		},
		{
			name: "annual usage on calendar monthly sub",
			sub: pvmSubSpec{cycle: types.BillingCycleCalendar, period: types.BILLING_PERIOD_MONTHLY,
				periodStart: pvmDate(2026, 3, 1), periodEnd: pvmDate(2026, 4, 1), anchor: pvmDate(2026, 2, 1)},
			item: pvmItemSpec{period: types.BILLING_PERIOD_ANNUAL, priceType: types.PRICE_TYPE_USAGE, arrear: true, start: pvmDate(2026, 1, 15)},
			want: []time.Time{pvmDate(2026, 3, 1)},
		},
		{
			name: "3-month usage on anniversary monthly sub",
			sub: pvmSubSpec{cycle: types.BillingCycleAnniversary, period: types.BILLING_PERIOD_MONTHLY,
				periodStart: pvmDate(2026, 3, 10), periodEnd: pvmDate(2026, 4, 10), anchor: pvmDate(2026, 1, 10)},
			item: pvmItemSpec{period: types.BILLING_PERIOD_MONTHLY, count: 3, priceType: types.PRICE_TYPE_USAGE, arrear: true, start: pvmDate(2026, 1, 10)},
			want: []time.Time{pvmDate(2026, 3, 10)},
		},
	}
	for _, tc := range cases {
		s.Run(tc.name, func() {
			fx := s.pvmBuild(tc.sub, tc.item)
			windows, err := usageWindows(tc.sub.periodStart, tc.sub.periodEnd, fx.items[0], fx.sub)
			s.Require().NoError(err)
			got := make([]time.Time, 0, len(windows))
			for _, w := range windows {
				got = append(got, w.Start)
			}
			s.Equal(tc.want, got)
		})
	}
}

// --- Onetime items --------------------------------------------------------------

// A onetime advance item attached mid-period with create_prorations is charged in full at attach.
func (s *MultiCadenceAddonMatrixSuite) TestOnetime_AttachWithProrationQuote() {
	fx := s.pvmBuild(pvmSubSpec{cycle: types.BillingCycleAnniversary, period: types.BILLING_PERIOD_MONTHLY,
		periodStart: pvmDate(2026, 3, 1), periodEnd: pvmDate(2026, 4, 1), anchor: pvmDate(2026, 1, 1),
		behavior: types.ProrationBehaviorCreateProrations},
		pvmItemSpec{period: types.BILLING_PERIOD_ONETIME, amount: 500, start: pvmDate(2026, 3, 15)})

	summary, err := NewLineItemProrationService(s.params).Compute(s.GetContext(), LineItemProrationRequest{
		Subscription: fx.sub,
		Entries: []LineItemProrationEntry{{
			LineItem: fx.items[0], Action: types.ProrationActionAddItem,
			NewPrice: fx.prices[0], NewQuantity: decimal.NewFromInt(1),
		}},
		EffectiveDate: pvmDate(2026, 3, 15),
		Behavior:      types.ProrationBehaviorCreateProrations,
	})
	s.Require().NoError(err, "a onetime addon attached mid-period must not error")
	s.pvmMoney("500", summary.TotalChargeAmount, "onetime charged once in full at attach")
}

// --- Renewal, grouping, timezone ---------------------------------------------

// Period-end renewal of a calendar quarterly sub: next-quarter advance windows for a monthly
// item are emitted once each; the monthly arrear item bills the current quarter's windows.
func (s *MultiCadenceAddonMatrixSuite) TestRenewal_ShorterCadenceNextPeriodAdvance() {
	fx := s.pvmBuild(pvmSubSpec{cycle: types.BillingCycleCalendar, period: types.BILLING_PERIOD_QUARTER,
		start: pvmDate(2026, 2, 15), periodStart: pvmDate(2026, 2, 15), periodEnd: pvmDate(2026, 4, 1), anchor: pvmDate(2026, 4, 1),
		behavior: types.ProrationBehaviorCreateProrations},
		pvmItemSpec{period: types.BILLING_PERIOD_MONTHLY, amount: 100, start: pvmDate(2026, 2, 15)},
		pvmItemSpec{period: types.BILLING_PERIOD_MONTHLY, amount: 10, start: pvmDate(2026, 2, 15), arrear: true})

	cls := s.billing.ClassifyLineItems(&dto.ClassifyLineItemsParams{
		Subscription: fx.sub, CurrentPeriodStart: pvmDate(2026, 2, 15), CurrentPeriodEnd: pvmDate(2026, 4, 1),
		NextPeriodStart: pvmDate(2026, 4, 1), NextPeriodEnd: pvmDate(2026, 7, 1),
	})
	s.Require().Len(cls.NextPeriodAdvance, 1)
	s.Require().Len(cls.CurrentPeriodArrear, 1)

	adv := *fx.sub
	adv.LineItems = cls.NextPeriodAdvance
	res, err := s.pvmFixed(&adv, pvmDate(2026, 4, 1), pvmDate(2026, 7, 1))
	s.Require().NoError(err)
	s.Len(res.LineItems, 3)
	s.pvmMoney("300", res.TotalAmount, "Apr, May, Jun advance windows")

	arr := *fx.sub
	arr.LineItems = cls.CurrentPeriodArrear
	res, err = s.pvmFixed(&arr, pvmDate(2026, 2, 15), pvmDate(2026, 4, 1))
	s.Require().NoError(err)
	s.pvmMoney("15", res.TotalAmount, "stub arrear: 14/28 of Feb + Mar")
}

func (s *MultiCadenceAddonMatrixSuite) TestGrouping_PerBillingPeriodMergesWindows() {
	for _, grouping := range []types.LineItemGrouping{types.LineItemGroupingPerChargePeriod, types.LineItemGroupingPerBillingPeriod} {
		s.Run(string(grouping), func() {
			fx := s.pvmBuild(pvmSubSpec{cycle: types.BillingCycleCalendar, period: types.BILLING_PERIOD_QUARTER,
				periodStart: pvmDate(2026, 2, 15), periodEnd: pvmDate(2026, 4, 1), anchor: pvmDate(2026, 4, 1),
				behavior: types.ProrationBehaviorCreateProrations, grouping: grouping},
				pvmItemSpec{period: types.BILLING_PERIOD_MONTHLY, amount: 100, start: pvmDate(2026, 2, 15)})
			res, err := s.pvmFixed(fx.sub, pvmDate(2026, 2, 15), pvmDate(2026, 4, 1))
			s.Require().NoError(err)
			out := applyLineItemGrouping(fx.sub, &dto.BillingCalculationResult{FixedCharges: res.LineItems, TotalAmount: res.TotalAmount})
			want := 2
			if grouping == types.LineItemGroupingPerBillingPeriod {
				want = 1
				s.Equal(pvmDate(2026, 2, 15), *out.FixedCharges[0].PeriodStart)
				s.Equal(pvmDate(2026, 4, 1), *out.FixedCharges[0].PeriodEnd)
			}
			s.Len(out.FixedCharges, want)
			sum := decimal.Zero
			for _, li := range out.FixedCharges {
				sum = sum.Add(li.Amount)
			}
			s.pvmMoney("150", sum, "grouping must not change the total")
		})
	}
}

func (s *MultiCadenceAddonMatrixSuite) TestTimezone_CrossCadence() {
	s.Run("IST calendar annual stub, monthly item", func() {
		tz := "Asia/Kolkata"
		fx := s.pvmBuild(pvmSubSpec{cycle: types.BillingCycleCalendar, period: types.BILLING_PERIOD_ANNUAL, timezone: tz,
			periodStart: pvmLocalDate(tz, 2026, 2, 15), periodEnd: pvmLocalDate(tz, 2027, 1, 1), anchor: pvmLocalDate(tz, 2027, 1, 1),
			behavior: types.ProrationBehaviorCreateProrations},
			pvmItemSpec{period: types.BILLING_PERIOD_MONTHLY, amount: 100, start: pvmLocalDate(tz, 2026, 2, 15)})
		res, err := s.pvmFixed(fx.sub, fx.sub.CurrentPeriodStart, fx.sub.CurrentPeriodEnd)
		s.Require().NoError(err)
		s.Require().Len(res.LineItems, 11)
		s.Equal(pvmLocalDate(tz, 2026, 3, 1), *res.LineItems[1].PeriodStart, "window boundary is IST midnight")
		s.pvmMoney("1050", res.TotalAmount, "IST")
	})
	s.Run("IST calendar annual stub, quarterly item", func() {
		tz := "Asia/Kolkata"
		fx := s.pvmBuild(pvmSubSpec{cycle: types.BillingCycleCalendar, period: types.BILLING_PERIOD_ANNUAL, timezone: tz,
			periodStart: pvmLocalDate(tz, 2026, 2, 15), periodEnd: pvmLocalDate(tz, 2027, 1, 1), anchor: pvmLocalDate(tz, 2027, 1, 1),
			behavior: types.ProrationBehaviorCreateProrations},
			pvmItemSpec{period: types.BILLING_PERIOD_QUARTER, amount: 300, start: pvmLocalDate(tz, 2026, 2, 15)})
		res, err := s.pvmFixed(fx.sub, fx.sub.CurrentPeriodStart, fx.sub.CurrentPeriodEnd)
		s.Require().NoError(err)
		s.pvmMoney("1050", res.TotalAmount, "IST quarterly on annual stub")
	})
	s.Run("DST New York calendar half-year, monthly windows across the March shift bill 1x", func() {
		tz := "America/New_York"
		fx := s.pvmBuild(pvmSubSpec{cycle: types.BillingCycleCalendar, period: types.BILLING_PERIOD_HALF_YEAR, timezone: tz,
			periodStart: pvmLocalDate(tz, 2026, 1, 1), periodEnd: pvmLocalDate(tz, 2026, 7, 1), anchor: pvmLocalDate(tz, 2026, 1, 1),
			behavior: types.ProrationBehaviorCreateProrations},
			pvmItemSpec{period: types.BILLING_PERIOD_MONTHLY, amount: 100, start: pvmLocalDate(tz, 2026, 1, 1)})
		res, err := s.pvmFixed(fx.sub, fx.sub.CurrentPeriodStart, fx.sub.CurrentPeriodEnd)
		s.Require().NoError(err)
		s.Len(res.LineItems, 6)
		s.pvmMoney("600", res.TotalAmount, "no DST hour leaks into a ratio")
	})
	s.Run("DST New York calendar quarterly stub straddling the shift", func() {
		tz := "America/New_York"
		fx := s.pvmBuild(pvmSubSpec{cycle: types.BillingCycleCalendar, period: types.BILLING_PERIOD_QUARTER, timezone: tz,
			periodStart: pvmLocalDate(tz, 2026, 3, 1), periodEnd: pvmLocalDate(tz, 2026, 4, 1), anchor: pvmLocalDate(tz, 2026, 4, 1),
			behavior: types.ProrationBehaviorCreateProrations},
			pvmItemSpec{period: types.BILLING_PERIOD_MONTHLY, amount: 100, start: pvmLocalDate(tz, 2026, 3, 1)})
		res, err := s.pvmFixed(fx.sub, fx.sub.CurrentPeriodStart, fx.sub.CurrentPeriodEnd)
		s.Require().NoError(err)
		s.pvmMoney("100", res.TotalAmount, "a whole local month (743h) is a full window")
	})
}
