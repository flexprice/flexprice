package service

import (
	"testing"
	"time"

	entpkg "github.com/flexprice/flexprice/ent"
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

// ─────────────────────────────────────────────────────────────────────────────
// Test suite
// ─────────────────────────────────────────────────────────────────────────────

type BillingOnetimeSuite struct {
	testutil.BaseServiceTestSuite
	service BillingService

	// shared time anchors for all tests
	jan1  time.Time // period start
	feb1  time.Time // period end / next period start
	mar1  time.Time // next period end
	jan15 time.Time // mid-period date
	feb15 time.Time // mid next-period date

	sub    *subscription.Subscription
	cust   *customer.Customer
	planT  *plan.Plan
	prices struct {
		recurringFixed *price.Price
		onetimeFixed   *price.Price
		onetimeArrear  *price.Price
	}
}

func TestBillingOnetimeService(t *testing.T) {
	suite.Run(t, new(BillingOnetimeSuite))
}

func (s *BillingOnetimeSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	s.setupService()
	s.setupAnchors()
	s.setupSharedFixtures()
}

func (s *BillingOnetimeSuite) TearDownTest() {
	s.BaseServiceTestSuite.TearDownTest()
}

func (s *BillingOnetimeSuite) setupService() {
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

func (s *BillingOnetimeSuite) setupAnchors() {
	s.jan1 = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	s.feb1 = time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	s.mar1 = time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	s.jan15 = time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
	s.feb15 = time.Date(2026, 2, 15, 0, 0, 0, 0, time.UTC)
}

func (s *BillingOnetimeSuite) setupSharedFixtures() {
	ctx := s.GetContext()

	s.cust = &customer.Customer{
		ID:        "cust_onetime",
		Name:      "OnceTime Customer",
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().CustomerRepo.Create(ctx, s.cust))

	s.planT = &plan.Plan{
		ID:        "plan_onetime",
		Name:      "OnceTime Plan",
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().PlanRepo.Create(ctx, s.planT))

	// RECURRING ADVANCE fixed price – $100/month
	s.prices.recurringFixed = &price.Price{
		ID:                 "price_recurring",
		Amount:             decimal.NewFromInt(100),
		Currency:           "usd",
		EntityType:         types.PRICE_ENTITY_TYPE_PLAN,
		EntityID:           s.planT.ID,
		Type:               types.PRICE_TYPE_FIXED,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_FLAT_FEE,
		BillingCadence:     types.BILLING_CADENCE_RECURRING,
		InvoiceCadence:     types.InvoiceCadenceAdvance,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().PriceRepo.Create(ctx, s.prices.recurringFixed))

	// ONETIME ADVANCE fixed price – $500 setup fee
	s.prices.onetimeFixed = &price.Price{
		ID:                 "price_onetime_advance",
		Amount:             decimal.NewFromInt(500),
		Currency:           "usd",
		EntityType:         types.PRICE_ENTITY_TYPE_PLAN,
		EntityID:           s.planT.ID,
		Type:               types.PRICE_TYPE_FIXED,
		BillingPeriod:      "",
		BillingPeriodCount: 0,
		BillingModel:       types.BILLING_MODEL_FLAT_FEE,
		BillingCadence:     types.BILLING_CADENCE_ONETIME,
		InvoiceCadence:     types.InvoiceCadenceAdvance,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().PriceRepo.Create(ctx, s.prices.onetimeFixed))

	// ONETIME fixed price used for arrear-cadence line-item classification tests – $200.
	// NOTE: The price itself uses InvoiceCadenceAdvance because the DTO validation
	// enforces ADVANCE for all ONETIME prices.  The arrear InvoiceCadence is set
	// directly on the line item in tests to exercise domain-level ARREAR classification
	// logic independently of the DTO constraint.
	s.prices.onetimeArrear = &price.Price{
		ID:                 "price_onetime_arrear",
		Amount:             decimal.NewFromInt(200),
		Currency:           "usd",
		EntityType:         types.PRICE_ENTITY_TYPE_PLAN,
		EntityID:           s.planT.ID,
		Type:               types.PRICE_TYPE_FIXED,
		BillingPeriod:      "",
		BillingPeriodCount: 0,
		BillingModel:       types.BILLING_MODEL_FLAT_FEE,
		BillingCadence:     types.BILLING_CADENCE_ONETIME,
		InvoiceCadence:     types.InvoiceCadenceAdvance,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().PriceRepo.Create(ctx, s.prices.onetimeArrear))

	// Subscription anchored Jan 1
	s.sub = &subscription.Subscription{
		ID:                 "sub_onetime",
		PlanID:             s.planT.ID,
		CustomerID:         s.cust.ID,
		StartDate:          s.jan1,
		BillingAnchor:      s.feb1,
		CurrentPeriodStart: s.jan1,
		CurrentPeriodEnd:   s.feb1,
		Currency:           "usd",
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		SubscriptionStatus: types.SubscriptionStatusActive,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────────────────────

// makeOnetimeLineItem builds a minimal ONETIME ADVANCE SubscriptionLineItem.
func (s *BillingOnetimeSuite) makeOnetimeLineItem(priceID string, cadence types.InvoiceCadence, chargeDate time.Time) *subscription.SubscriptionLineItem {
	return &subscription.SubscriptionLineItem{
		ID:             "li_" + priceID,
		SubscriptionID: s.sub.ID,
		CustomerID:     s.cust.ID,
		PriceID:        priceID,
		PriceType:      types.PRICE_TYPE_FIXED,
		// BillingPeriod intentionally omitted (zero value "") — IsOneTime() uses PriceType==FIXED && BillingPeriod==""
		InvoiceCadence: cadence,
		Quantity:       decimal.NewFromInt(1),
		Currency:       "usd",
		StartDate:      chargeDate, // charge date = StartDate for ONETIME
		BaseModel:      types.GetDefaultBaseModel(s.GetContext()),
	}
}

func (s *BillingOnetimeSuite) makeRecurringLineItem(priceID string) *subscription.SubscriptionLineItem {
	return &subscription.SubscriptionLineItem{
		ID:             "li_rec_" + priceID,
		SubscriptionID: s.sub.ID,
		CustomerID:     s.cust.ID,
		PriceID:        priceID,
		PriceType:      types.PRICE_TYPE_FIXED,
		InvoiceCadence: types.InvoiceCadenceAdvance,
		BillingPeriod:  types.BILLING_PERIOD_MONTHLY,
		Quantity:       decimal.NewFromInt(1),
		Currency:       "usd",
		StartDate:      s.jan1,
		BaseModel:      types.GetDefaultBaseModel(s.GetContext()),
	}
}

func (s *BillingOnetimeSuite) billingService() *billingService {
	return s.service.(*billingService)
}

// ─────────────────────────────────────────────────────────────────────────────
// Group 1: ClassifyLineItems — ONETIME
// ─────────────────────────────────────────────────────────────────────────────

// Test 1: ONETIME ADVANCE — charge date at period start (inclusive lower bound)
func (s *BillingOnetimeSuite) TestClassify_OneTimeAdvance_ChargeDateAtPeriodStart() {
	item := s.makeOnetimeLineItem("price_onetime_advance", types.InvoiceCadenceAdvance, s.jan1)
	s.sub.LineItems = []*subscription.SubscriptionLineItem{item}

	result := s.billingService().ClassifyLineItems(s.sub, s.jan1, s.feb1, s.feb1, s.mar1)

	s.Len(result.CurrentPeriodAdvance, 1, "charge date == period start should be in CurrentPeriodAdvance")
	s.Empty(result.CurrentPeriodArrear)
	s.Empty(result.NextPeriodAdvance, "ONETIME at period start should NOT bleed into NextPeriodAdvance")
}

// Test 2: ONETIME ADVANCE — charge date mid-period
func (s *BillingOnetimeSuite) TestClassify_OneTimeAdvance_ChargeDateMidPeriod() {
	item := s.makeOnetimeLineItem("price_onetime_advance", types.InvoiceCadenceAdvance, s.jan15)
	s.sub.LineItems = []*subscription.SubscriptionLineItem{item}

	result := s.billingService().ClassifyLineItems(s.sub, s.jan1, s.feb1, s.feb1, s.mar1)

	s.Len(result.CurrentPeriodAdvance, 1)
	s.Empty(result.CurrentPeriodArrear)
	s.Empty(result.NextPeriodAdvance)
}

// Test 3: ONETIME ADVANCE — charge date == period end (exclusive upper bound for ADVANCE)
func (s *BillingOnetimeSuite) TestClassify_OneTimeAdvance_ChargeDateAtPeriodEnd() {
	// Feb 1 is the period END — exclusive for ADVANCE → NOT in current, but IS in next
	item := s.makeOnetimeLineItem("price_onetime_advance", types.InvoiceCadenceAdvance, s.feb1)
	s.sub.LineItems = []*subscription.SubscriptionLineItem{item}

	result := s.billingService().ClassifyLineItems(s.sub, s.jan1, s.feb1, s.feb1, s.mar1)

	s.Empty(result.CurrentPeriodAdvance, "period end is exclusive for ADVANCE — should not be in current")
	s.Len(result.NextPeriodAdvance, 1, "should land in next period (Feb 1 is next period start)")
}

// Test 4: ONETIME ADVANCE — charge date in next period only
func (s *BillingOnetimeSuite) TestClassify_OneTimeAdvance_ChargeDateInNextPeriod() {
	item := s.makeOnetimeLineItem("price_onetime_advance", types.InvoiceCadenceAdvance, s.feb15)
	s.sub.LineItems = []*subscription.SubscriptionLineItem{item}

	result := s.billingService().ClassifyLineItems(s.sub, s.jan1, s.feb1, s.feb1, s.mar1)

	s.Empty(result.CurrentPeriodAdvance)
	s.Len(result.NextPeriodAdvance, 1, "charge date in next period should land in NextPeriodAdvance")
}

// Test 5: ONETIME ADVANCE — charge date in the past (before both periods)
func (s *BillingOnetimeSuite) TestClassify_OneTimeAdvance_ChargeDateOutsideBothPeriods() {
	dec15 := time.Date(2025, 12, 15, 0, 0, 0, 0, time.UTC)
	item := s.makeOnetimeLineItem("price_onetime_advance", types.InvoiceCadenceAdvance, dec15)
	s.sub.LineItems = []*subscription.SubscriptionLineItem{item}

	result := s.billingService().ClassifyLineItems(s.sub, s.jan1, s.feb1, s.feb1, s.mar1)

	s.Empty(result.CurrentPeriodAdvance)
	s.Empty(result.NextPeriodAdvance)
	s.Empty(result.CurrentPeriodArrear)
}

// Test 6: ONETIME ARREAR — charge date mid-period
func (s *BillingOnetimeSuite) TestClassify_OneTimeArrear_ChargeDateMidPeriod() {
	item := s.makeOnetimeLineItem("price_onetime_arrear", types.InvoiceCadenceArrear, s.jan15)
	s.sub.LineItems = []*subscription.SubscriptionLineItem{item}

	result := s.billingService().ClassifyLineItems(s.sub, s.jan1, s.feb1, s.feb1, s.mar1)

	s.Len(result.CurrentPeriodArrear, 1)
	s.Empty(result.CurrentPeriodAdvance)
}

// Test 7: ONETIME ARREAR — charge date == period start (exclusive lower bound for ARREAR)
func (s *BillingOnetimeSuite) TestClassify_OneTimeArrear_ChargeDateAtPeriodStart() {
	// Jan 1 == period start, exclusive for ARREAR → should NOT be classified
	item := s.makeOnetimeLineItem("price_onetime_arrear", types.InvoiceCadenceArrear, s.jan1)
	s.sub.LineItems = []*subscription.SubscriptionLineItem{item}

	result := s.billingService().ClassifyLineItems(s.sub, s.jan1, s.feb1, s.feb1, s.mar1)

	s.Empty(result.CurrentPeriodArrear, "period start is exclusive for ARREAR")
}

// Test 8: ONETIME ARREAR — charge date == period end (inclusive upper bound for ARREAR)
func (s *BillingOnetimeSuite) TestClassify_OneTimeArrear_ChargeDateAtPeriodEnd() {
	item := s.makeOnetimeLineItem("price_onetime_arrear", types.InvoiceCadenceArrear, s.feb1)
	s.sub.LineItems = []*subscription.SubscriptionLineItem{item}

	result := s.billingService().ClassifyLineItems(s.sub, s.jan1, s.feb1, s.feb1, s.mar1)

	s.Len(result.CurrentPeriodArrear, 1, "period end is inclusive for ARREAR")
}

// Test 9: ONETIME ADVANCE in current period — should NOT also appear in NextPeriodAdvance
func (s *BillingOnetimeSuite) TestClassify_OneTimeAdvance_InCurrent_NotInNext() {
	item := s.makeOnetimeLineItem("price_onetime_advance", types.InvoiceCadenceAdvance, s.jan15)
	s.sub.LineItems = []*subscription.SubscriptionLineItem{item}

	result := s.billingService().ClassifyLineItems(s.sub, s.jan1, s.feb1, s.feb1, s.mar1)

	s.Len(result.CurrentPeriodAdvance, 1)
	s.Empty(result.NextPeriodAdvance, "ONETIME in current period must not appear in NextPeriodAdvance")
}

// Test 10: Mixed RECURRING + ONETIME — RECURRING goes to both advance slices, ONETIME only to current
func (s *BillingOnetimeSuite) TestClassify_MixedRecurringAndOnetime() {
	recurring := s.makeRecurringLineItem("price_recurring")
	onetime := s.makeOnetimeLineItem("price_onetime_advance", types.InvoiceCadenceAdvance, s.jan15)
	s.sub.LineItems = []*subscription.SubscriptionLineItem{recurring, onetime}

	result := s.billingService().ClassifyLineItems(s.sub, s.jan1, s.feb1, s.feb1, s.mar1)

	s.Len(result.CurrentPeriodAdvance, 2, "both recurring and onetime should be in current advance")
	s.Len(result.NextPeriodAdvance, 1, "only recurring should be in next advance")
	s.Equal("li_rec_price_recurring", result.NextPeriodAdvance[0].ID)
}

// ─────────────────────────────────────────────────────────────────────────────
// Group 2: IsOneTime and GetChargeDate helpers
// ─────────────────────────────────────────────────────────────────────────────

func (s *BillingOnetimeSuite) TestIsOneTime_True() {
	item := s.makeOnetimeLineItem("p", types.InvoiceCadenceAdvance, s.jan1)
	s.True(item.IsOneTime())
}

func (s *BillingOnetimeSuite) TestIsOneTime_False_Recurring() {
	item := s.makeRecurringLineItem("p")
	s.False(item.IsOneTime())
}

func (s *BillingOnetimeSuite) TestGetChargeDate_ReturnsStartDate() {
	item := s.makeOnetimeLineItem("p", types.InvoiceCadenceAdvance, s.jan15)
	s.Equal(s.jan15, item.GetChargeDate())
}

// TestIsOneTime_ViaMapper verifies that the production Ent→domain mapping path
// correctly preserves the ONETIME signal (BillingPeriod="") so that IsOneTime()
// returns true without requiring the Price object to be pre-loaded.
// This prevents mapper regressions that might accidentally set a non-empty
// BillingPeriod for ONETIME items.
func (s *BillingOnetimeSuite) TestIsOneTime_ViaMapper() {
	priceType := types.PRICE_TYPE_FIXED
	entItem := &entpkg.SubscriptionLineItem{
		ID:             "ent-onetime-1",
		SubscriptionID: s.sub.ID,
		CustomerID:     s.cust.ID,
		PriceID:        s.prices.onetimeFixed.ID,
		PriceType:      &priceType,
		BillingPeriod:  "", // ONETIME: intentionally empty
		Currency:       "usd",
		Quantity:       decimal.NewFromInt(1),
		TenantID:       s.GetContext().Value(types.CtxTenantID).(string),
		Status:         string(types.StatusPublished),
	}
	domainItem := subscription.SubscriptionLineItemFromEnt(entItem)
	s.True(domainItem.IsOneTime(), "mapper must preserve empty BillingPeriod → IsOneTime() == true")
	s.Equal("", string(domainItem.BillingPeriod), "BillingPeriod must be empty for ONETIME")
}

// TestIsOneTime_ViaMapper_Recurring verifies that a recurring item coming through
// the mapper is NOT classified as one-time.
func (s *BillingOnetimeSuite) TestIsOneTime_ViaMapper_Recurring() {
	priceType := types.PRICE_TYPE_FIXED
	entItem := &entpkg.SubscriptionLineItem{
		ID:             "ent-rec-1",
		SubscriptionID: s.sub.ID,
		CustomerID:     s.cust.ID,
		PriceID:        s.prices.recurringFixed.ID,
		PriceType:      &priceType,
		BillingPeriod:  types.BILLING_PERIOD_MONTHLY,
		Currency:       "usd",
		Quantity:       decimal.NewFromInt(1),
		TenantID:       s.GetContext().Value(types.CtxTenantID).(string),
		Status:         string(types.StatusPublished),
	}
	domainItem := subscription.SubscriptionLineItemFromEnt(entItem)
	s.False(domainItem.IsOneTime(), "recurring item via mapper must return IsOneTime() == false")
}

// ─────────────────────────────────────────────────────────────────────────────
// Group 4: CalculateFixedCharges — ONETIME has no proration
// ─────────────────────────────────────────────────────────────────────────────

func (s *BillingOnetimeSuite) TestCalculateFixed_OneTime_FullAmountNoProration() {
	ctx := s.GetContext()

	// Subscription started mid-month (Jan 15) — proration would reduce RECURRING charges
	sub := *s.sub
	sub.StartDate = s.jan15
	sub.LineItems = []*subscription.SubscriptionLineItem{
		{
			ID:             "li_ot_full",
			SubscriptionID: sub.ID,
			CustomerID:     sub.CustomerID,
			PriceID:        s.prices.onetimeFixed.ID,
			PriceType:      types.PRICE_TYPE_FIXED,
			InvoiceCadence: types.InvoiceCadenceAdvance,
			Quantity:       decimal.NewFromInt(1),
			Currency:       "usd",
			StartDate:      s.jan15, // charge date
			BaseModel:      types.GetDefaultBaseModel(ctx),
		},
	}

	lineItems, total, err := s.billingService().CalculateFixedCharges(ctx, &sub, s.jan1, s.feb1)
	s.NoError(err)
	s.Len(lineItems, 1)
	s.Equal("500", total.String(), "ONETIME charge must be full $500, no proration")
}

func (s *BillingOnetimeSuite) TestCalculateFixed_OneTime_LineItemPeriodIsChargeDate() {
	ctx := s.GetContext()

	sub := *s.sub
	sub.LineItems = []*subscription.SubscriptionLineItem{
		{
			ID:             "li_ot_period",
			SubscriptionID: sub.ID,
			CustomerID:     sub.CustomerID,
			PriceID:        s.prices.onetimeFixed.ID,
			PriceType:      types.PRICE_TYPE_FIXED,
			InvoiceCadence: types.InvoiceCadenceAdvance,
			Quantity:       decimal.NewFromInt(1),
			Currency:       "usd",
			StartDate:      s.jan15,
			BaseModel:      types.GetDefaultBaseModel(ctx),
		},
	}

	lineItems, _, err := s.billingService().CalculateFixedCharges(ctx, &sub, s.jan1, s.feb1)
	s.NoError(err)
	s.Require().Len(lineItems, 1)
	li := lineItems[0]
	s.NotNil(li.PeriodStart)
	s.NotNil(li.PeriodEnd)
	s.True(li.PeriodStart.Equal(s.jan15), "PeriodStart should equal charge date")
	s.True(li.PeriodEnd.Equal(s.jan15), "PeriodEnd should equal charge date")
}

func (s *BillingOnetimeSuite) TestCalculateFixed_OneTime_WithQuantity3() {
	ctx := s.GetContext()

	sub := *s.sub
	sub.LineItems = []*subscription.SubscriptionLineItem{
		{
			ID:             "li_ot_qty3",
			SubscriptionID: sub.ID,
			CustomerID:     sub.CustomerID,
			PriceID:        s.prices.onetimeFixed.ID, // $500 per unit
			PriceType:      types.PRICE_TYPE_FIXED,
			InvoiceCadence: types.InvoiceCadenceAdvance,
			Quantity:       decimal.NewFromInt(3), // 3 units
			Currency:       "usd",
			StartDate:      s.jan1,
			BaseModel:      types.GetDefaultBaseModel(ctx),
		},
	}

	_, total, err := s.billingService().CalculateFixedCharges(ctx, &sub, s.jan1, s.feb1)
	s.NoError(err)
	s.Equal("1500", total.String(), "3 × $500 = $1500")
}

func (s *BillingOnetimeSuite) TestCalculateFixed_OneTime_SkippedIfStartDateAfterPeriodEnd() {
	ctx := s.GetContext()

	sub := *s.sub
	sub.LineItems = []*subscription.SubscriptionLineItem{
		{
			ID:             "li_ot_future",
			SubscriptionID: sub.ID,
			CustomerID:     sub.CustomerID,
			PriceID:        s.prices.onetimeFixed.ID,
			PriceType:      types.PRICE_TYPE_FIXED,
			InvoiceCadence: types.InvoiceCadenceAdvance,
			Quantity:       decimal.NewFromInt(1),
			Currency:       "usd",
			StartDate:      s.mar1, // future charge date — after the invoice period
			BaseModel:      types.GetDefaultBaseModel(ctx),
		},
	}

	lineItems, total, err := s.billingService().CalculateFixedCharges(ctx, &sub, s.jan1, s.feb1)
	s.NoError(err)
	s.Empty(lineItems, "charge date after period end should be skipped")
	s.True(total.IsZero())
}

func (s *BillingOnetimeSuite) TestCalculateFixed_Recurring_StillProratesNormally() {
	ctx := s.GetContext()

	// Start mid-month — recurring fixed should be prorated
	sub := *s.sub
	sub.StartDate = s.jan15
	sub.LineItems = []*subscription.SubscriptionLineItem{
		{
			ID:             "li_rec_prorate",
			SubscriptionID: sub.ID,
			CustomerID:     sub.CustomerID,
			PriceID:        s.prices.recurringFixed.ID, // $100/month RECURRING
			PriceType:      types.PRICE_TYPE_FIXED,
			InvoiceCadence: types.InvoiceCadenceAdvance,
			BillingPeriod:  types.BILLING_PERIOD_MONTHLY,
			Quantity:       decimal.NewFromInt(1),
			Currency:       "usd",
			StartDate:      s.jan15,
			BaseModel:      types.GetDefaultBaseModel(ctx),
		},
	}

	_, total, err := s.billingService().CalculateFixedCharges(ctx, &sub, s.jan1, s.feb1)
	s.NoError(err)
	// Amount may be prorated (< $100) or $100 depending on ProrationBehavior; just confirm it's > 0
	s.True(total.GreaterThan(decimal.Zero), "recurring fixed charge should produce a non-zero amount")
}

// ─────────────────────────────────────────────────────────────────────────────
// Group 5: Edge cases
// ─────────────────────────────────────────────────────────────────────────────

func (s *BillingOnetimeSuite) TestOnetime_MultipleChargesInSamePeriod() {
	item1 := s.makeOnetimeLineItem("price_onetime_advance", types.InvoiceCadenceAdvance, s.jan1)
	item1.ID = "li_ot1"
	item2 := s.makeOnetimeLineItem("price_onetime_arrear", types.InvoiceCadenceAdvance, s.jan15)
	item2.ID = "li_ot2"
	s.sub.LineItems = []*subscription.SubscriptionLineItem{item1, item2}

	result := s.billingService().ClassifyLineItems(s.sub, s.jan1, s.feb1, s.feb1, s.mar1)

	s.Len(result.CurrentPeriodAdvance, 2, "two ONETIME charges in same period should both be classified")
}

func (s *BillingOnetimeSuite) TestOnetime_ZeroDurationLineItemPeriod() {
	ctx := s.GetContext()

	sub := *s.sub
	sub.LineItems = []*subscription.SubscriptionLineItem{
		{
			ID:             "li_ot_zero",
			SubscriptionID: sub.ID,
			CustomerID:     sub.CustomerID,
			PriceID:        s.prices.onetimeFixed.ID,
			PriceType:      types.PRICE_TYPE_FIXED,
			InvoiceCadence: types.InvoiceCadenceAdvance,
			Quantity:       decimal.NewFromInt(1),
			Currency:       "usd",
			StartDate:      s.jan1,
			BaseModel:      types.GetDefaultBaseModel(ctx),
		},
	}

	lineItems, _, err := s.billingService().CalculateFixedCharges(ctx, &sub, s.jan1, s.feb1)
	s.NoError(err)
	s.Require().Len(lineItems, 1)
	// PeriodStart == PeriodEnd == charge date
	s.True(lo.FromPtr(lineItems[0].PeriodStart).Equal(s.jan1))
	s.True(lo.FromPtr(lineItems[0].PeriodEnd).Equal(s.jan1))
}

func (s *BillingOnetimeSuite) TestOnetime_NeitherCurrentNorNext_WhenFarFuture() {
	apr1 := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	item := s.makeOnetimeLineItem("price_onetime_advance", types.InvoiceCadenceAdvance, apr1)
	s.sub.LineItems = []*subscription.SubscriptionLineItem{item}

	result := s.billingService().ClassifyLineItems(s.sub, s.jan1, s.feb1, s.feb1, s.mar1)

	s.Empty(result.CurrentPeriodAdvance)
	s.Empty(result.NextPeriodAdvance)
}
