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

// FirstPeriodStubSuite covers pricing of short first billing periods, which happen when the
// billing anchor lands inside the first period. The invariant under test: a first period is
// never longer than one billing interval, and its price is proportional to its length.
//
// The counter-cases matter as much as the positive ones. A period can be shorter than one
// interval for reasons that have nothing to do with the anchor — a contract end_date, a
// scheduled cancellation — and those must keep their full price here and be handled by the
// mid-cycle proration path instead.
type FirstPeriodStubSuite struct {
	testutil.BaseServiceTestSuite
	service BillingService
}

func TestFirstPeriodStub(t *testing.T) {
	suite.Run(t, new(FirstPeriodStubSuite))
}

func (s *FirstPeriodStubSuite) SetupTest() {
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

func (s *FirstPeriodStubSuite) TearDownTest() {
	s.BaseServiceTestSuite.TearDownTest()
}

// stubCase describes one subscription shape and the amount its first invoice should carry.
type stubCase struct {
	name string

	amount        decimal.Decimal
	billingPeriod types.BillingPeriod
	cadence       types.InvoiceCadence

	startDate     time.Time
	billingAnchor time.Time
	endDate       *time.Time

	// The period being invoiced, and where the subscription's own period pointer sits at the
	// time of invoicing. They diverge for arrear billing and invoice recalculation.
	periodStart        time.Time
	periodEnd          time.Time
	currentPeriodStart time.Time
	currentPeriodEnd   time.Time

	prorationBehavior types.ProrationBehavior

	want decimal.Decimal
}

// run seeds a customer, plan, price, subscription and line item for the case, then asserts the
// amount CalculateFixedCharges produces for the first invoice.
func (s *FirstPeriodStubSuite) run(id string, tc stubCase) {
	ctx := s.GetContext()

	cust := &customer.Customer{
		ID:         "cust_" + id,
		ExternalID: "ext_cust_" + id,
		Name:       "Stub Co",
		Email:      "stub@example.com",
		BaseModel:  types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().CustomerRepo.Create(ctx, cust))

	pl := &plan.Plan{
		ID:        "plan_" + id,
		Name:      "Stub Plan " + id,
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().PlanRepo.Create(ctx, pl))

	p := &price.Price{
		ID:                 "price_" + id,
		Amount:             tc.amount,
		Currency:           "usd",
		EntityType:         types.PRICE_ENTITY_TYPE_PLAN,
		EntityID:           pl.ID,
		Type:               types.PRICE_TYPE_FIXED,
		BillingPeriod:      tc.billingPeriod,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_FLAT_FEE,
		BillingCadence:     types.BILLING_CADENCE_RECURRING,
		InvoiceCadence:     tc.cadence,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().PriceRepo.Create(ctx, p))

	sub := &subscription.Subscription{
		ID:                 "sub_" + id,
		PlanID:             pl.ID,
		CustomerID:         cust.ID,
		StartDate:          tc.startDate,
		EndDate:            tc.endDate,
		BillingAnchor:      tc.billingAnchor,
		CurrentPeriodStart: tc.currentPeriodStart,
		CurrentPeriodEnd:   tc.currentPeriodEnd,
		Currency:           "usd",
		BillingPeriod:      tc.billingPeriod,
		BillingPeriodCount: 1,
		SubscriptionStatus: types.SubscriptionStatusActive,
		Timezone:           "UTC",
		ProrationBehavior:  tc.prorationBehavior,
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
		DisplayName:        "Stub Fee",
		Quantity:           decimal.NewFromInt(1),
		Currency:           "usd",
		BillingPeriod:      tc.billingPeriod,
		BillingPeriodCount: 1,
		InvoiceCadence:     tc.cadence,
		StartDate:          tc.periodStart,
		EndDate:            tc.periodEnd,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().SubscriptionRepo.CreateWithLineItems(ctx, sub, []*subscription.SubscriptionLineItem{li}))
	sub.LineItems = []*subscription.SubscriptionLineItem{li}

	result, err := s.service.CalculateFixedCharges(ctx, &dto.CalculateFixedChargesParams{
		Subscription: sub,
		PeriodStart:  tc.periodStart,
		PeriodEnd:    tc.periodEnd,
	})
	s.NoError(err)
	s.Require().Len(result.LineItems, 1)
	s.True(result.LineItems[0].Amount.Equal(tc.want),
		"expected %s, got %s", tc.want, result.LineItems[0].Amount)
}

var (
	mar2_2026 = time.Date(2026, time.March, 2, 0, 0, 0, 0, time.UTC)
	jul2_2026 = time.Date(2026, time.July, 2, 0, 0, 0, 0, time.UTC)
	jul2_2027 = time.Date(2027, time.July, 2, 0, 0, 0, 0, time.UTC)
	mar1_2027 = time.Date(2027, time.March, 1, 0, 0, 0, 0, time.UTC)
)

// An annual subscription anchored four months after it starts must bill the 122-day stub at
// 122/365 of the annual price, not a full year.
func (s *FirstPeriodStubSuite) TestAnnualAnchorStubIsProrated() {
	s.run("annual_stub", stubCase{
		amount:             decimal.NewFromInt(1200),
		billingPeriod:      types.BILLING_PERIOD_ANNUAL,
		cadence:            types.InvoiceCadenceAdvance,
		startDate:          mar2_2026,
		billingAnchor:      jul2_2026,
		periodStart:        mar2_2026,
		periodEnd:          jul2_2026,
		currentPeriodStart: mar2_2026,
		currentPeriodEnd:   jul2_2026,
		prorationBehavior:  types.ProrationBehaviorCreateProrations,
		// 1200 * 122/365
		want: decimal.RequireFromString("401.10"),
	})
}

// Stripe-aligned: proration_behavior=none skips charging the short first period.
func (s *FirstPeriodStubSuite) TestAnnualAnchorStubIsFreeWhenProrationNone() {
	s.run("annual_stub_free", stubCase{
		amount:             decimal.NewFromInt(1200),
		billingPeriod:      types.BILLING_PERIOD_ANNUAL,
		cadence:            types.InvoiceCadenceAdvance,
		startDate:          mar2_2026,
		billingAnchor:      jul2_2026,
		periodStart:        mar2_2026,
		periodEnd:          jul2_2026,
		currentPeriodStart: mar2_2026,
		currentPeriodEnd:   jul2_2026,
		prorationBehavior:  types.ProrationBehaviorNone,
		want:               decimal.Zero,
	})
}

// Regression: NextBillingDate clips the period end to the subscription end_date, so a one-year
// term ending Mar 1 produced a 364-day period against a 365-day interval. That is a contract
// term, not an anchor stub, and must still bill the full annual price.
func (s *FirstPeriodStubSuite) TestFixedTermEndDateIsNotAStub() {
	s.run("fixed_term", stubCase{
		amount:             decimal.NewFromInt(1200),
		billingPeriod:      types.BILLING_PERIOD_ANNUAL,
		cadence:            types.InvoiceCadenceAdvance,
		startDate:          mar2_2026,
		billingAnchor:      mar2_2026,
		endDate:            &mar1_2027,
		periodStart:        mar2_2026,
		periodEnd:          mar1_2027,
		currentPeriodStart: mar2_2026,
		currentPeriodEnd:   mar1_2027,
		prorationBehavior:  types.ProrationBehaviorNone,
		want:               decimal.NewFromInt(1200),
	})
}

// Regression: a scheduled cancellation truncates CurrentPeriodEnd in place, which made the
// final period look like a stub and turned a cancellation credit into a prorated charge.
// With proration_behavior=none the full amount stands.
func (s *FirstPeriodStubSuite) TestScheduledCancellationTruncationIsNotAStub() {
	jan1 := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	jan15 := time.Date(2026, time.January, 15, 0, 0, 0, 0, time.UTC)
	s.run("cancel_trunc", stubCase{
		amount:             decimal.NewFromInt(100),
		billingPeriod:      types.BILLING_PERIOD_MONTHLY,
		cadence:            types.InvoiceCadenceAdvance,
		startDate:          jan1,
		billingAnchor:      jan1,
		periodStart:        jan1,
		periodEnd:          jan15,
		currentPeriodStart: jan1,
		currentPeriodEnd:   jan15,
		prorationBehavior:  types.ProrationBehaviorNone,
		want:               decimal.NewFromInt(100),
	})
}

// Regression: stub detection used to require the invoice period to equal the subscription's
// current period. Arrear billing invoices the first period only after the pointer has already
// advanced, which silently restored the full price. Invoice recalculation replays historical
// periods the same way.
func (s *FirstPeriodStubSuite) TestArrearStubPricedAfterPeriodAdvanced() {
	s.run("arrear_stub", stubCase{
		amount:        decimal.NewFromInt(1200),
		billingPeriod: types.BILLING_PERIOD_ANNUAL,
		cadence:       types.InvoiceCadenceArrear,
		startDate:     mar2_2026,
		billingAnchor: jul2_2026,
		periodStart:   mar2_2026,
		periodEnd:     jul2_2026,
		// The subscription has already rolled into its first full year.
		currentPeriodStart: jul2_2026,
		currentPeriodEnd:   jul2_2027,
		prorationBehavior:  types.ProrationBehaviorCreateProrations,
		want:               decimal.RequireFromString("401.10"),
	})
}

// Calendar billing always derives an anchor strictly after the start date, but a start that
// lands exactly on a calendar boundary still spans a full interval and must not be prorated.
func (s *FirstPeriodStubSuite) TestCalendarBoundaryStartIsNotAStub() {
	jan1 := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	feb1 := time.Date(2026, time.February, 1, 0, 0, 0, 0, time.UTC)
	s.run("cal_boundary", stubCase{
		amount:             decimal.NewFromInt(100),
		billingPeriod:      types.BILLING_PERIOD_MONTHLY,
		cadence:            types.InvoiceCadenceAdvance,
		startDate:          jan1,
		billingAnchor:      feb1, // CalculateCalendarBillingAnchor returns the next boundary
		periodStart:        jan1,
		periodEnd:          feb1,
		currentPeriodStart: jan1,
		currentPeriodEnd:   feb1,
		prorationBehavior:  types.ProrationBehaviorNone,
		want:               decimal.NewFromInt(100),
	})
}

// A calendar subscription starting mid-month bills only the days to the boundary.
func (s *FirstPeriodStubSuite) TestCalendarMidMonthStartIsProrated() {
	jul15 := time.Date(2026, time.July, 15, 0, 0, 0, 0, time.UTC)
	aug1 := time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC)
	s.run("cal_midmonth", stubCase{
		amount:             decimal.NewFromInt(100),
		billingPeriod:      types.BILLING_PERIOD_MONTHLY,
		cadence:            types.InvoiceCadenceAdvance,
		startDate:          jul15,
		billingAnchor:      aug1,
		periodStart:        jul15,
		periodEnd:          aug1,
		currentPeriodStart: jul15,
		currentPeriodEnd:   aug1,
		prorationBehavior:  types.ProrationBehaviorCreateProrations,
		// 100 * 17/31
		want: decimal.RequireFromString("54.84"),
	})
}

func (s *FirstPeriodStubSuite) TestCalendarMidMonthStartIsFreeWhenProrationNone() {
	jul15 := time.Date(2026, time.July, 15, 0, 0, 0, 0, time.UTC)
	aug1 := time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC)
	s.run("cal_midmonth_free", stubCase{
		amount:             decimal.NewFromInt(100),
		billingPeriod:      types.BILLING_PERIOD_MONTHLY,
		cadence:            types.InvoiceCadenceAdvance,
		startDate:          jul15,
		billingAnchor:      aug1,
		periodStart:        jul15,
		periodEnd:          aug1,
		currentPeriodStart: jul15,
		currentPeriodEnd:   aug1,
		prorationBehavior:  types.ProrationBehaviorNone,
		want:               decimal.Zero,
	})
}

// Renewal periods are full intervals and must never be touched by stub pricing, even though
// their start no longer matches the subscription start date.
func (s *FirstPeriodStubSuite) TestRenewalPeriodIsNotAStub() {
	s.run("renewal", stubCase{
		amount:             decimal.NewFromInt(1200),
		billingPeriod:      types.BILLING_PERIOD_ANNUAL,
		cadence:            types.InvoiceCadenceAdvance,
		startDate:          mar2_2026,
		billingAnchor:      jul2_2026,
		periodStart:        jul2_2026,
		periodEnd:          jul2_2027,
		currentPeriodStart: jul2_2026,
		currentPeriodEnd:   jul2_2027,
		prorationBehavior:  types.ProrationBehaviorNone,
		want:               decimal.NewFromInt(1200),
	})
}
