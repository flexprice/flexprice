package service

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/entitlement"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/feature"
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

// BillingUsageChargesSuite drives the raw-events usage billing paths —
// CalculateUsageCharges and CalculateFeatureUsageCharges — through the
// entitlement reset-period branches, line-item commitments, overage
// metadata and subscription true-up.
//
// Each scenario builds its own plan + subscription (via newChargesFixture) so
// plan-level entitlements never leak between table cases.
type BillingUsageChargesSuite struct {
	testutil.BaseServiceTestSuite
	service   BillingService
	billing   *billingService
	eventRepo *testutil.InMemoryEventStore
	customer  *customer.Customer

	meterSum    *meter.Meter
	meterBucket *meter.Meter
	featSum     *feature.Feature
	featBucket  *feature.Feature
	priceSum    *price.Price
	priceBucket *price.Price

	monthStart time.Time // 2025-06-01
	monthEnd   time.Time // 2025-07-01
	yearStart  time.Time // 2025-01-01
	yearEnd    time.Time // 2026-01-01
}

func TestBillingUsageCharges(t *testing.T) {
	suite.Run(t, new(BillingUsageChargesSuite))
}

func (s *BillingUsageChargesSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	ctx := s.GetContext()
	s.eventRepo = s.GetStores().EventRepo.(*testutil.InMemoryEventStore)
	s.service = NewBillingService(newTestServiceParams(&s.BaseServiceTestSuite))
	s.billing = s.service.(*billingService)

	s.monthStart = time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	s.monthEnd = time.Date(2025, 7, 1, 0, 0, 0, 0, time.UTC)
	s.yearStart = time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	s.yearEnd = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	s.customer = &customer.Customer{
		ID:         "cust_buc",
		ExternalID: "ext_buc",
		Name:       "Usage Charges Customer",
		Email:      "buc@test.com",
		BaseModel:  types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().CustomerRepo.Create(ctx, s.customer))

	s.meterSum = &meter.Meter{
		ID:        "meter_buc_sum",
		Name:      "BUC Sum Meter",
		EventName: "buc_event",
		Aggregation: meter.Aggregation{
			Type:  types.AggregationSum,
			Field: "qty",
		},
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().MeterRepo.CreateMeter(ctx, s.meterSum))

	s.meterBucket = &meter.Meter{
		ID:        "meter_buc_bucket",
		Name:      "BUC Bucketed Meter",
		EventName: "buc_bucket_event",
		Aggregation: meter.Aggregation{
			Type:  types.AggregationMax,
			Field: "qty",
			// Hourly buckets: the in-memory event store routes day/month windows
			// through SUM/COUNT-only aggregation, so MAX bucketing needs a
			// non-day window size to exercise the real bucketed-max math.
			BucketSize: types.WindowSizeHour,
		},
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().MeterRepo.CreateMeter(ctx, s.meterBucket))

	s.featSum = &feature.Feature{
		ID:        "feat_buc_sum",
		Name:      "BUC Sum Feature",
		Type:      types.FeatureTypeMetered,
		MeterID:   s.meterSum.ID,
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().FeatureRepo.Create(ctx, s.featSum))

	s.featBucket = &feature.Feature{
		ID:        "feat_buc_bucket",
		Name:      "BUC Bucket Feature",
		Type:      types.FeatureTypeMetered,
		MeterID:   s.meterBucket.ID,
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().FeatureRepo.Create(ctx, s.featBucket))

	upTo1000 := uint64(1000)
	mkPrice := func(id, meterID string) *price.Price {
		p := &price.Price{
			ID:                 id,
			Amount:             decimal.Zero,
			Currency:           "usd",
			EntityType:         types.PRICE_ENTITY_TYPE_PLAN,
			EntityID:           "plan_buc_shared",
			Type:               types.PRICE_TYPE_USAGE,
			BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
			BillingPeriodCount: 1,
			BillingModel:       types.BILLING_MODEL_TIERED,
			BillingCadence:     types.BILLING_CADENCE_RECURRING,
			InvoiceCadence:     types.InvoiceCadenceArrear,
			TierMode:           types.BILLING_TIER_SLAB,
			MeterID:            meterID,
			Tiers: []price.PriceTier{
				{UpTo: &upTo1000, UnitAmount: decimal.RequireFromString("0.02")},
				{UpTo: nil, UnitAmount: decimal.RequireFromString("0.01")},
			},
			BaseModel: types.GetDefaultBaseModel(ctx),
		}
		s.NoError(s.GetStores().PriceRepo.Create(ctx, p))
		return p
	}
	s.priceSum = mkPrice("price_buc_sum", s.meterSum.ID)
	s.priceBucket = mkPrice("price_buc_bucket", s.meterBucket.ID)
}

// chargesFixture is one plan + subscription + line item combination.
type chargesFixture struct {
	plan *plan.Plan
	sub  *subscription.Subscription
	item *subscription.SubscriptionLineItem
}

// newChargesFixture creates a plan and an active subscription with a single
// usage line item. bucketed selects the bucketed MAX meter over the SUM meter.
func (s *BillingUsageChargesSuite) newChargesFixture(suffix string, billingPeriod types.BillingPeriod, bucketed bool) *chargesFixture {
	ctx := s.GetContext()
	fx := &chargesFixture{}

	fx.plan = &plan.Plan{
		ID:        "plan_buc_" + suffix,
		Name:      "BUC Plan " + suffix,
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().PlanRepo.Create(ctx, fx.plan))

	periodStart, periodEnd := s.monthStart, s.monthEnd
	startDate := time.Date(2025, 3, 1, 0, 0, 0, 0, time.UTC)
	if billingPeriod == types.BILLING_PERIOD_ANNUAL {
		periodStart, periodEnd = s.yearStart, s.yearEnd
		startDate = s.yearStart
	}

	m, p := s.meterSum, s.priceSum
	if bucketed {
		m, p = s.meterBucket, s.priceBucket
	}

	fx.sub = &subscription.Subscription{
		ID:                 "sub_buc_" + suffix,
		PlanID:             fx.plan.ID,
		CustomerID:         s.customer.ID,
		StartDate:          startDate,
		BillingAnchor:      periodStart,
		CurrentPeriodStart: periodStart,
		CurrentPeriodEnd:   periodEnd,
		Currency:           "usd",
		BillingPeriod:      billingPeriod,
		BillingPeriodCount: 1,
		SubscriptionStatus: types.SubscriptionStatusActive,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	fx.item = &subscription.SubscriptionLineItem{
		ID:               "li_buc_" + suffix,
		SubscriptionID:   fx.sub.ID,
		CustomerID:       s.customer.ID,
		EntityID:         fx.plan.ID,
		EntityType:       types.SubscriptionLineItemEntityTypePlan,
		PlanDisplayName:  fx.plan.Name,
		PriceID:          p.ID,
		PriceType:        types.PRICE_TYPE_USAGE,
		MeterID:          m.ID,
		MeterDisplayName: m.Name,
		DisplayName:      "Usage " + suffix,
		Quantity:         decimal.Zero,
		Currency:         "usd",
		BillingPeriod:    billingPeriod,
		InvoiceCadence:   types.InvoiceCadenceArrear,
		StartDate:        startDate,
		BaseModel:        types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().SubscriptionRepo.CreateWithLineItems(ctx, fx.sub,
		[]*subscription.SubscriptionLineItem{fx.item}))
	fx.sub.LineItems = []*subscription.SubscriptionLineItem{fx.item}
	return fx
}

// addEntitlement attaches a metered plan entitlement for the given feature.
func (s *BillingUsageChargesSuite) addEntitlement(fx *chargesFixture, feat *feature.Feature, limit *int64, reset types.EntitlementUsageResetPeriod) {
	ent := &entitlement.Entitlement{
		ID:               "ent_" + fx.plan.ID + "_" + feat.ID,
		EntityType:       types.ENTITLEMENT_ENTITY_TYPE_PLAN,
		EntityID:         fx.plan.ID,
		FeatureID:        feat.ID,
		FeatureType:      types.FeatureTypeMetered,
		IsEnabled:        true,
		UsageLimit:       limit,
		UsageResetPeriod: reset,
		BaseModel:        types.GetDefaultBaseModel(s.GetContext()),
	}
	_, err := s.GetStores().EntitlementRepo.Create(s.GetContext(), ent)
	s.NoError(err)
}

// insertEvent seeds one raw event for a meter's event name.
func (s *BillingUsageChargesSuite) insertEvent(eventName string, ts time.Time, qty float64) {
	ctx := s.GetContext()
	s.NoError(s.eventRepo.InsertEvent(ctx, &events.Event{
		ID:                 s.GetUUID(),
		TenantID:           types.GetTenantID(ctx),
		EnvironmentID:      types.GetEnvironmentID(ctx),
		EventName:          eventName,
		ExternalCustomerID: s.customer.ExternalID,
		Timestamp:          ts,
		Properties:         map[string]interface{}{"qty": qty},
	}))
}

// charge builds a usage charge for the fixture's line item (price matched by ID
// for CalculateUsageCharges, line item ID for CalculateFeatureUsageCharges).
func (s *BillingUsageChargesSuite) charge(fx *chargesFixture, qty, amount float64, p *price.Price) *dto.SubscriptionUsageByMetersResponse {
	return &dto.SubscriptionUsageByMetersResponse{
		SubscriptionLineItemID: fx.item.ID,
		MeterID:                fx.item.MeterID,
		MeterDisplayName:       fx.item.MeterDisplayName,
		Quantity:               qty,
		Amount:                 amount,
		Currency:               "usd",
		Price:                  p,
	}
}

func (s *BillingUsageChargesSuite) calcUsage(fx *chargesFixture, usage *dto.GetUsageBySubscriptionResponse) (*dto.CalculateUsageChargesResult, error) {
	return s.billing.CalculateUsageCharges(s.GetContext(), &dto.CalculateUsageChargesParams{
		Subscription: fx.sub,
		Usage:        usage,
		PeriodStart:  fx.sub.CurrentPeriodStart,
		PeriodEnd:    fx.sub.CurrentPeriodEnd,
	})
}

func (s *BillingUsageChargesSuite) calcFeatureUsage(fx *chargesFixture, usage *dto.GetUsageBySubscriptionResponse) (*dto.CalculateFeatureUsageChargesResult, error) {
	return s.billing.CalculateFeatureUsageCharges(s.GetContext(), &dto.CalculateFeatureUsageChargesParams{
		Subscription: fx.sub,
		Usage:        usage,
		PeriodStart:  fx.sub.CurrentPeriodStart,
		PeriodEnd:    fx.sub.CurrentPeriodEnd,
		Source:       types.UsageSourceInvoiceCreation,
	})
}

func mkUsage(charges ...*dto.SubscriptionUsageByMetersResponse) *dto.GetUsageBySubscriptionResponse {
	return &dto.GetUsageBySubscriptionResponse{Charges: charges}
}

// ---------------------------------------------------------------------------
// CalculateUsageCharges (raw events path)
// ---------------------------------------------------------------------------

func (s *BillingUsageChargesSuite) TestCalculateUsageCharges_ValidationAndNilUsage() {
	fx := s.newChargesFixture("val", types.BILLING_PERIOD_MONTHLY, false)

	s.Run("missing_subscription_returns_validation_error", func() {
		_, err := s.billing.CalculateUsageCharges(s.GetContext(), &dto.CalculateUsageChargesParams{
			PeriodStart: s.monthStart,
			PeriodEnd:   s.monthEnd,
		})
		s.Error(err)
	})

	s.Run("nil_usage_returns_zero_total", func() {
		result, err := s.calcUsage(fx, nil)
		s.NoError(err)
		s.True(result.TotalAmount.IsZero())
		s.Empty(result.LineItems)
	})
}

func (s *BillingUsageChargesSuite) TestCalculateUsageCharges_MonthlyResetOnAnnualSub() {
	fx := s.newChargesFixture("monthly", types.BILLING_PERIOD_ANNUAL, false)
	s.addEntitlement(fx, s.featSum, lo.ToPtr(int64(100)), types.ENTITLEMENT_USAGE_RESET_PERIOD_MONTHLY)

	// Jan: 150 (overage 50), Feb: 80 (none) → billable 50 → $1.
	s.insertEvent("buc_event", time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC), 150)
	s.insertEvent("buc_event", time.Date(2025, 2, 15, 12, 0, 0, 0, time.UTC), 80)

	result, err := s.calcUsage(fx, mkUsage(s.charge(fx, 230, 0, s.priceSum)))
	s.NoError(err)
	s.Len(result.LineItems, 1)
	s.True(result.LineItems[0].Quantity.Equal(decimal.NewFromInt(50)), "quantity should be 50, got %s", result.LineItems[0].Quantity)
	s.True(result.TotalAmount.Equal(decimal.NewFromInt(1)), "total should be 1, got %s", result.TotalAmount)
	s.Equal("monthly", result.LineItems[0].Metadata["usage_reset_period"])
}

func (s *BillingUsageChargesSuite) TestCalculateUsageCharges_NeverReset() {
	fx := s.newChargesFixture("never", types.BILLING_PERIOD_MONTHLY, false)
	s.addEntitlement(fx, s.featSum, lo.ToPtr(int64(100)), types.ENTITLEMENT_USAGE_RESET_PERIOD_NEVER)

	// Pre-period 300 (already billed), in-period 500 → billable (500 - 100) = 400 → $8.
	s.insertEvent("buc_event", time.Date(2025, 4, 10, 0, 0, 0, 0, time.UTC), 300)
	s.insertEvent("buc_event", time.Date(2025, 6, 10, 0, 0, 0, 0, time.UTC), 500)

	result, err := s.calcUsage(fx, mkUsage(s.charge(fx, 500, 0, s.priceSum)))
	s.NoError(err)
	s.Len(result.LineItems, 1)
	s.True(result.LineItems[0].Quantity.Equal(decimal.NewFromInt(400)), "quantity should be 400, got %s", result.LineItems[0].Quantity)
	s.True(result.TotalAmount.Equal(decimal.NewFromInt(8)), "total should be 8, got %s", result.TotalAmount)
	s.Equal("never", result.LineItems[0].Metadata["usage_reset_period"])
}

func (s *BillingUsageChargesSuite) TestCalculateUsageCharges_WeeklyResetFallsBackToSimpleSubtraction() {
	fx := s.newChargesFixture("weekly", types.BILLING_PERIOD_MONTHLY, false)
	s.addEntitlement(fx, s.featSum, lo.ToPtr(int64(200)), types.ENTITLEMENT_USAGE_RESET_PERIOD_WEEKLY)

	// Unsupported reset period → default branch: billable = 500 - 200 = 300 → $6.
	result, err := s.calcUsage(fx, mkUsage(s.charge(fx, 500, 0, s.priceSum)))
	s.NoError(err)
	s.Len(result.LineItems, 1)
	s.True(result.LineItems[0].Quantity.Equal(decimal.NewFromInt(300)), "quantity should be 300, got %s", result.LineItems[0].Quantity)
	s.True(result.TotalAmount.Equal(decimal.NewFromInt(6)), "total should be 6, got %s", result.TotalAmount)
	s.NotNil(result.LineItems[0].AdjustedEntitlementQuantity)
	s.True(result.LineItems[0].AdjustedEntitlementQuantity.Equal(decimal.NewFromInt(200)))
}

func (s *BillingUsageChargesSuite) TestCalculateUsageCharges_BucketedMeterEntitlements() {
	// Bucketed MAX meter, hourly buckets: 08:00 bucket max(10, 7) = 10,
	// 09:00 bucket max = 20 → total quantity 30.
	s.insertEvent("buc_bucket_event", time.Date(2025, 6, 3, 8, 0, 0, 0, time.UTC), 10)
	s.insertEvent("buc_bucket_event", time.Date(2025, 6, 3, 8, 30, 0, 0, time.UTC), 7)
	s.insertEvent("buc_bucket_event", time.Date(2025, 6, 3, 9, 15, 0, 0, time.UTC), 20)

	s.Run("usage_limit_reduces_aggregate_quantity", func() {
		fx := s.newChargesFixture("bkt_lim", types.BILLING_PERIOD_MONTHLY, true)
		s.addEntitlement(fx, s.featBucket, lo.ToPtr(int64(5)), types.ENTITLEMENT_USAGE_RESET_PERIOD_MONTHLY)

		result, err := s.calcUsage(fx, mkUsage(s.charge(fx, 0, 0, s.priceBucket)))
		s.NoError(err)
		s.Len(result.LineItems, 1)
		// 30 - 5 = 25 → 25 * $0.02 = $0.5
		s.True(result.LineItems[0].Quantity.Equal(decimal.NewFromInt(25)), "quantity should be 25, got %s", result.LineItems[0].Quantity)
		s.True(result.TotalAmount.Equal(decimal.RequireFromString("0.5")), "total should be 0.5, got %s", result.TotalAmount)
	})

	s.Run("unlimited_entitlement_zeroes_bucketed_charge", func() {
		fx := s.newChargesFixture("bkt_unl", types.BILLING_PERIOD_MONTHLY, true)
		s.addEntitlement(fx, s.featBucket, nil, types.ENTITLEMENT_USAGE_RESET_PERIOD_MONTHLY)

		result, err := s.calcUsage(fx, mkUsage(s.charge(fx, 0, 0, s.priceBucket)))
		s.NoError(err)
		s.Len(result.LineItems, 1)
		s.True(result.LineItems[0].Quantity.IsZero())
		s.True(result.TotalAmount.IsZero(), "unlimited entitlement should bill zero, got %s", result.TotalAmount)
		s.NotNil(result.LineItems[0].AdjustedEntitlementQuantity)
		s.True(result.LineItems[0].AdjustedEntitlementQuantity.Equal(decimal.NewFromInt(30)))
	})
}

func (s *BillingUsageChargesSuite) TestCalculateUsageCharges_LineItemCommitment() {
	// Bucketed usage for the windowed case: two hourly buckets with max 10 and 20.
	s.insertEvent("buc_bucket_event", time.Date(2025, 6, 3, 8, 0, 0, 0, time.UTC), 10)
	s.insertEvent("buc_bucket_event", time.Date(2025, 6, 3, 9, 0, 0, 0, time.UTC), 20)

	s.Run("flat_commitment_bills_overage_at_overage_rate", func() {
		fx := s.newChargesFixture("cmt_flat", types.BILLING_PERIOD_MONTHLY, false)
		item := *fx.item
		item.CommitmentAmount = lo.ToPtr(decimal.NewFromInt(5))
		item.CommitmentType = types.COMMITMENT_TYPE_AMOUNT
		item.CommitmentOverageFactor = lo.ToPtr(decimal.NewFromInt(2))
		subCopy := *fx.sub
		subCopy.LineItems = []*subscription.SubscriptionLineItem{&item}
		fxCopy := &chargesFixture{plan: fx.plan, sub: &subCopy, item: &item}

		// usage $10 over $5 commitment at 2x → 5 + 5*2 = $15
		result, err := s.calcUsage(fxCopy, mkUsage(s.charge(fxCopy, 500, 10, s.priceSum)))
		s.NoError(err)
		s.Len(result.LineItems, 1)
		s.True(result.TotalAmount.Equal(decimal.NewFromInt(15)), "total should be 15, got %s", result.TotalAmount)
		info := result.LineItems[0].CommitmentInfo
		s.NotNil(info)
		s.True(info.ComputedCommitmentUtilizedAmount.Equal(decimal.NewFromInt(5)))
		s.True(info.ComputedOverageAmount.Equal(decimal.NewFromInt(10)))
	})

	s.Run("windowed_commitment_applies_per_bucket", func() {
		fx := s.newChargesFixture("cmt_win", types.BILLING_PERIOD_MONTHLY, true)
		item := *fx.item
		item.CommitmentAmount = lo.ToPtr(decimal.RequireFromString("0.3"))
		item.CommitmentType = types.COMMITMENT_TYPE_AMOUNT
		item.CommitmentOverageFactor = lo.ToPtr(decimal.NewFromInt(2))
		item.CommitmentWindowed = true
		subCopy := *fx.sub
		subCopy.LineItems = []*subscription.SubscriptionLineItem{&item}
		fxCopy := &chargesFixture{plan: fx.plan, sub: &subCopy, item: &item}

		// window costs $0.20 / $0.40 vs $0.30 per-window commitment at 2x
		// → 0.2 + (0.3 + 0.1*2) = $0.7
		result, err := s.calcUsage(fxCopy, mkUsage(s.charge(fxCopy, 0, 0, s.priceBucket)))
		s.NoError(err)
		s.Len(result.LineItems, 1)
		s.True(result.TotalAmount.Equal(decimal.RequireFromString("0.7")), "total should be 0.7, got %s", result.TotalAmount)
		info := result.LineItems[0].CommitmentInfo
		s.NotNil(info)
		s.True(info.ComputedCommitmentUtilizedAmount.Equal(decimal.RequireFromString("0.5")))
		s.True(info.ComputedOverageAmount.Equal(decimal.RequireFromString("0.2")))
	})
}

func (s *BillingUsageChargesSuite) TestCalculateUsageCharges_OverageAndTrueUp() {
	s.Run("overage_charge_keeps_amount_and_gets_overage_metadata", func() {
		fx := s.newChargesFixture("ovr", types.BILLING_PERIOD_MONTHLY, false)
		ch := s.charge(fx, 100, 3, s.priceSum)
		ch.IsOverage = true
		ch.OverageFactor = 1.5

		result, err := s.calcUsage(fx, mkUsage(ch))
		s.NoError(err)
		s.Len(result.LineItems, 1)
		li := result.LineItems[0]
		s.True(li.Amount.Equal(decimal.NewFromInt(3)), "overage amount must be kept verbatim, got %s", li.Amount)
		s.Equal("true", li.Metadata["is_overage"])
		s.Equal("1.5", li.Metadata["overage_factor"])
		s.Equal("Usage ovr (Overage)", lo.FromPtr(li.DisplayName))
	})

	s.Run("trueup_line_added_when_commitment_underutilized", func() {
		fx := s.newChargesFixture("tru", types.BILLING_PERIOD_MONTHLY, false)
		subCopy := *fx.sub
		subCopy.CommitmentAmount = lo.ToPtr(decimal.NewFromInt(20))
		subCopy.OverageFactor = lo.ToPtr(decimal.NewFromInt(2))
		subCopy.EnableTrueUp = true
		fxCopy := &chargesFixture{plan: fx.plan, sub: &subCopy, item: fx.item}
		subCopy.LineItems = fx.sub.LineItems

		usage := mkUsage(s.charge(fxCopy, 500, 10, s.priceSum))
		usage.CommitmentUtilized = 10

		result, err := s.calcUsage(fxCopy, usage)
		s.NoError(err)
		s.Len(result.LineItems, 2)
		s.True(result.TotalAmount.Equal(decimal.NewFromInt(20)), "total should be 10 + 10 true-up, got %s", result.TotalAmount)
		trueUp := result.LineItems[1]
		s.Equal("true", trueUp.Metadata["is_commitment_trueup"])
		s.Equal("20", trueUp.Metadata["commitment_amount"])
		s.Equal("10", trueUp.Metadata["commitment_utilized"])
		s.True(trueUp.Amount.Equal(decimal.NewFromInt(10)))
	})

	s.Run("no_trueup_when_usage_has_overage", func() {
		fx := s.newChargesFixture("tru_ovr", types.BILLING_PERIOD_MONTHLY, false)
		subCopy := *fx.sub
		subCopy.CommitmentAmount = lo.ToPtr(decimal.NewFromInt(20))
		subCopy.OverageFactor = lo.ToPtr(decimal.NewFromInt(2))
		subCopy.EnableTrueUp = true
		subCopy.LineItems = fx.sub.LineItems
		fxCopy := &chargesFixture{plan: fx.plan, sub: &subCopy, item: fx.item}

		usage := mkUsage(s.charge(fxCopy, 500, 10, s.priceSum))
		usage.CommitmentUtilized = 10
		usage.HasOverage = true

		result, err := s.calcUsage(fxCopy, usage)
		s.NoError(err)
		s.Len(result.LineItems, 1)
		s.True(result.TotalAmount.Equal(decimal.NewFromInt(10)))
	})
}

func (s *BillingUsageChargesSuite) TestCalculateUsageCharges_MissingPriceUnitSkipsLineItem() {
	fx := s.newChargesFixture("pu", types.BILLING_PERIOD_MONTHLY, false)
	item := *fx.item
	item.PriceUnit = lo.ToPtr("credits_missing") // not registered in PriceUnitRepo
	subCopy := *fx.sub
	subCopy.LineItems = []*subscription.SubscriptionLineItem{&item}
	fxCopy := &chargesFixture{plan: fx.plan, sub: &subCopy, item: &item}

	result, err := s.calcUsage(fxCopy, mkUsage(s.charge(fxCopy, 500, 10, s.priceSum)))
	s.NoError(err)
	// The line item is skipped when its price unit cannot be resolved.
	// NOTE: the amount has already been added to the running total before the
	// price-unit lookup, so TotalAmount still includes the skipped line item.
	s.Empty(result.LineItems)
	s.True(result.TotalAmount.Equal(decimal.NewFromInt(10)),
		"documented behavior: total includes skipped line item amount, got %s", result.TotalAmount)
}

// ---------------------------------------------------------------------------
// CalculateFeatureUsageCharges (feature_usage path)
// ---------------------------------------------------------------------------

func (s *BillingUsageChargesSuite) TestCalculateFeatureUsageCharges_ValidationAndNilUsage() {
	fx := s.newChargesFixture("fval", types.BILLING_PERIOD_MONTHLY, false)

	s.Run("missing_subscription_returns_validation_error", func() {
		_, err := s.billing.CalculateFeatureUsageCharges(s.GetContext(), &dto.CalculateFeatureUsageChargesParams{
			PeriodStart: s.monthStart,
			PeriodEnd:   s.monthEnd,
		})
		s.Error(err)
	})

	s.Run("nil_usage_returns_zero_total", func() {
		result, err := s.calcFeatureUsage(fx, nil)
		s.NoError(err)
		s.True(result.TotalAmount.IsZero())
		s.Empty(result.LineItems)
	})
}

func (s *BillingUsageChargesSuite) TestCalculateFeatureUsageCharges_MeterNotFoundReturnsError() {
	fx := s.newChargesFixture("fmiss", types.BILLING_PERIOD_MONTHLY, false)
	item := *fx.item
	item.MeterID = "meter_missing"
	subCopy := *fx.sub
	subCopy.LineItems = []*subscription.SubscriptionLineItem{&item}
	fxCopy := &chargesFixture{plan: fx.plan, sub: &subCopy, item: &item}

	_, err := s.calcFeatureUsage(fxCopy, mkUsage(s.charge(fxCopy, 100, 0, s.priceSum)))
	s.Error(err)
	s.ErrorContains(err, "meter not found")
}

func (s *BillingUsageChargesSuite) TestCalculateFeatureUsageCharges_MonthlyResetOnAnnualSub() {
	fx := s.newChargesFixture("fmonthly", types.BILLING_PERIOD_ANNUAL, false)
	s.addEntitlement(fx, s.featSum, lo.ToPtr(int64(100)), types.ENTITLEMENT_USAGE_RESET_PERIOD_MONTHLY)

	s.insertEvent("buc_event", time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC), 150)
	s.insertEvent("buc_event", time.Date(2025, 2, 15, 12, 0, 0, 0, time.UTC), 80)

	result, err := s.calcFeatureUsage(fx, mkUsage(s.charge(fx, 230, 0, s.priceSum)))
	s.NoError(err)
	s.Len(result.LineItems, 1)
	s.True(result.LineItems[0].Quantity.Equal(decimal.NewFromInt(50)), "quantity should be 50, got %s", result.LineItems[0].Quantity)
	s.True(result.TotalAmount.Equal(decimal.NewFromInt(1)), "total should be 1, got %s", result.TotalAmount)
	s.Equal("monthly", result.LineItems[0].Metadata["usage_reset_period"])
}

func (s *BillingUsageChargesSuite) TestCalculateFeatureUsageCharges_NeverReset() {
	fx := s.newChargesFixture("fnever", types.BILLING_PERIOD_MONTHLY, false)
	s.addEntitlement(fx, s.featSum, lo.ToPtr(int64(100)), types.ENTITLEMENT_USAGE_RESET_PERIOD_NEVER)

	s.insertEvent("buc_event", time.Date(2025, 4, 10, 0, 0, 0, 0, time.UTC), 300)
	s.insertEvent("buc_event", time.Date(2025, 6, 10, 0, 0, 0, 0, time.UTC), 500)

	result, err := s.calcFeatureUsage(fx, mkUsage(s.charge(fx, 500, 0, s.priceSum)))
	s.NoError(err)
	s.Len(result.LineItems, 1)
	s.True(result.LineItems[0].Quantity.Equal(decimal.NewFromInt(400)), "quantity should be 400, got %s", result.LineItems[0].Quantity)
	s.True(result.TotalAmount.Equal(decimal.NewFromInt(8)), "total should be 8, got %s", result.TotalAmount)
	s.Equal("never", result.LineItems[0].Metadata["usage_reset_period"])
}

func (s *BillingUsageChargesSuite) TestCalculateFeatureUsageCharges_WeeklyAndUnlimitedEntitlements() {
	s.Run("weekly_reset_falls_back_to_simple_subtraction", func() {
		fx := s.newChargesFixture("fweekly", types.BILLING_PERIOD_MONTHLY, false)
		s.addEntitlement(fx, s.featSum, lo.ToPtr(int64(200)), types.ENTITLEMENT_USAGE_RESET_PERIOD_WEEKLY)

		result, err := s.calcFeatureUsage(fx, mkUsage(s.charge(fx, 500, 0, s.priceSum)))
		s.NoError(err)
		s.Len(result.LineItems, 1)
		s.True(result.LineItems[0].Quantity.Equal(decimal.NewFromInt(300)))
		s.True(result.TotalAmount.Equal(decimal.NewFromInt(6)), "total should be 6, got %s", result.TotalAmount)
	})

	s.Run("unlimited_entitlement_zeroes_charge", func() {
		fx := s.newChargesFixture("funl", types.BILLING_PERIOD_MONTHLY, false)
		s.addEntitlement(fx, s.featSum, nil, types.ENTITLEMENT_USAGE_RESET_PERIOD_MONTHLY)

		result, err := s.calcFeatureUsage(fx, mkUsage(s.charge(fx, 500, 10, s.priceSum)))
		s.NoError(err)
		s.Len(result.LineItems, 1)
		s.True(result.LineItems[0].Quantity.IsZero())
		s.True(result.TotalAmount.IsZero())
		s.NotNil(result.LineItems[0].AdjustedEntitlementQuantity)
		s.True(result.LineItems[0].AdjustedEntitlementQuantity.Equal(decimal.NewFromInt(500)))
	})
}

func (s *BillingUsageChargesSuite) TestCalculateFeatureUsageCharges_BucketedMeterEntitlements() {
	// The in-memory FeatureUsageRepo returns empty bucketed usage, so bucketed
	// feature-usage charges compute to zero — these cases pin the control flow
	// (bucketed query + entitlement gates), not ClickHouse aggregation math.
	s.Run("bucketed_meter_with_usage_limit", func() {
		fx := s.newChargesFixture("fbkt_lim", types.BILLING_PERIOD_MONTHLY, true)
		s.addEntitlement(fx, s.featBucket, lo.ToPtr(int64(5)), types.ENTITLEMENT_USAGE_RESET_PERIOD_MONTHLY)

		result, err := s.calcFeatureUsage(fx, mkUsage(s.charge(fx, 0, 0, s.priceBucket)))
		s.NoError(err)
		s.Len(result.LineItems, 1)
		s.True(result.TotalAmount.IsZero())
	})

	s.Run("bucketed_meter_with_unlimited_entitlement", func() {
		fx := s.newChargesFixture("fbkt_unl", types.BILLING_PERIOD_MONTHLY, true)
		s.addEntitlement(fx, s.featBucket, nil, types.ENTITLEMENT_USAGE_RESET_PERIOD_MONTHLY)

		result, err := s.calcFeatureUsage(fx, mkUsage(s.charge(fx, 0, 0, s.priceBucket)))
		s.NoError(err)
		s.Len(result.LineItems, 1)
		s.True(result.LineItems[0].Amount.IsZero())
		s.True(result.LineItems[0].Quantity.IsZero())
	})
}

func (s *BillingUsageChargesSuite) TestCalculateFeatureUsageCharges_LineItemCommitment() {
	s.Run("flat_commitment_bills_overage_at_overage_rate", func() {
		fx := s.newChargesFixture("fcmt_flat", types.BILLING_PERIOD_MONTHLY, false)
		item := *fx.item
		item.CommitmentAmount = lo.ToPtr(decimal.NewFromInt(5))
		item.CommitmentType = types.COMMITMENT_TYPE_AMOUNT
		item.CommitmentOverageFactor = lo.ToPtr(decimal.NewFromInt(2))
		subCopy := *fx.sub
		subCopy.LineItems = []*subscription.SubscriptionLineItem{&item}
		fxCopy := &chargesFixture{plan: fx.plan, sub: &subCopy, item: &item}

		// recalculated usage cost 500 * 0.02 = $10 over $5 commitment at 2x → $15
		result, err := s.calcFeatureUsage(fxCopy, mkUsage(s.charge(fxCopy, 500, 0, s.priceSum)))
		s.NoError(err)
		s.Len(result.LineItems, 1)
		s.True(result.TotalAmount.Equal(decimal.NewFromInt(15)), "total should be 15, got %s", result.TotalAmount)
		info := result.LineItems[0].CommitmentInfo
		s.NotNil(info)
		s.True(info.ComputedOverageAmount.Equal(decimal.NewFromInt(10)))
	})

	s.Run("windowed_commitment_with_no_usage_bills_zero", func() {
		fx := s.newChargesFixture("fcmt_win", types.BILLING_PERIOD_MONTHLY, true)
		item := *fx.item
		item.CommitmentAmount = lo.ToPtr(decimal.RequireFromString("0.3"))
		item.CommitmentType = types.COMMITMENT_TYPE_AMOUNT
		item.CommitmentOverageFactor = lo.ToPtr(decimal.NewFromInt(2))
		item.CommitmentWindowed = true
		subCopy := *fx.sub
		subCopy.LineItems = []*subscription.SubscriptionLineItem{&item}
		fxCopy := &chargesFixture{plan: fx.plan, sub: &subCopy, item: &item}

		// In-memory feature_usage bucketed query returns no windows → $0.
		result, err := s.calcFeatureUsage(fxCopy, mkUsage(s.charge(fxCopy, 0, 0, s.priceBucket)))
		s.NoError(err)
		s.Len(result.LineItems, 1)
		s.True(result.TotalAmount.IsZero())
	})
}

func (s *BillingUsageChargesSuite) TestCalculateFeatureUsageCharges_OverageTrueUpAndPriceUnit() {
	s.Run("overage_charge_keeps_amount_and_gets_overage_metadata", func() {
		fx := s.newChargesFixture("fovr", types.BILLING_PERIOD_MONTHLY, false)
		ch := s.charge(fx, 100, 3, s.priceSum)
		ch.IsOverage = true
		ch.OverageFactor = 1.5

		result, err := s.calcFeatureUsage(fx, mkUsage(ch))
		s.NoError(err)
		s.Len(result.LineItems, 1)
		li := result.LineItems[0]
		s.True(li.Amount.Equal(decimal.NewFromInt(3)))
		s.Equal("true", li.Metadata["is_overage"])
		s.Equal("Usage fovr (Overage)", lo.FromPtr(li.DisplayName))
	})

	s.Run("trueup_line_added_when_commitment_underutilized", func() {
		fx := s.newChargesFixture("ftru", types.BILLING_PERIOD_MONTHLY, false)
		subCopy := *fx.sub
		subCopy.CommitmentAmount = lo.ToPtr(decimal.NewFromInt(20))
		subCopy.OverageFactor = lo.ToPtr(decimal.NewFromInt(2))
		subCopy.EnableTrueUp = true
		subCopy.LineItems = fx.sub.LineItems
		fxCopy := &chargesFixture{plan: fx.plan, sub: &subCopy, item: fx.item}

		usage := mkUsage(s.charge(fxCopy, 500, 0, s.priceSum))
		usage.CommitmentUtilized = 10

		result, err := s.calcFeatureUsage(fxCopy, usage)
		s.NoError(err)
		s.Len(result.LineItems, 2)
		s.True(result.TotalAmount.Equal(decimal.NewFromInt(20)), "total should be 10 usage + 10 true-up, got %s", result.TotalAmount)
		trueUp := result.LineItems[1]
		s.Equal("true", trueUp.Metadata["is_commitment_trueup"])
		s.True(trueUp.Amount.Equal(decimal.NewFromInt(10)))
	})

	s.Run("missing_price_unit_returns_error", func() {
		fx := s.newChargesFixture("fpu", types.BILLING_PERIOD_MONTHLY, false)
		item := *fx.item
		item.PriceUnit = lo.ToPtr("credits_missing")
		subCopy := *fx.sub
		subCopy.LineItems = []*subscription.SubscriptionLineItem{&item}
		fxCopy := &chargesFixture{plan: fx.plan, sub: &subCopy, item: &item}

		// NOTE: unlike CalculateUsageCharges (which skips the line item and keeps
		// going), CalculateFeatureUsageCharges fails the whole calculation when a
		// line item's price unit cannot be resolved.
		_, err := s.calcFeatureUsage(fxCopy, mkUsage(s.charge(fxCopy, 500, 0, s.priceSum)))
		s.Error(err)
		s.ErrorContains(err, "price unit")
	})
}
