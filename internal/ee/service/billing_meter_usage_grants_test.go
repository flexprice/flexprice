package service

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/entitlementgrant"
	eventsDomain "github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/meter"
	priceDomain "github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// -----------------------------------------------------------------------------
// Focused unit tests for adjustMeterUsageGrants. We construct a bare
// billingService with only the fields the helper touches (Logger); the
// grant slice + line item + matchingCharge is enough to drive every branch.
// -----------------------------------------------------------------------------

func makeGrant(quota, usage int64, measure types.EntitlementGrantMeasure) *entitlementgrant.EntitlementGrant {
	return &entitlementgrant.EntitlementGrant{
		ID:                  "eg_" + measure.String() + "_" + decimal.NewFromInt(quota).String(),
		EntitlementConfigID: "ec_x",
		CustomerID:          "cust_x",
		SubscriptionID:      "sub_x",
		ScopeEntityType:     types.EntitlementGrantScopeFeature,
		ScopeEntityID:       "feat_x",
		Measure:             measure,
		Quota:               decimal.NewFromInt(quota),
		Usage:               decimal.NewFromInt(usage),
		ValidFrom:           time.Now().Add(-time.Hour),
		ValidTo:             time.Now().Add(4 * time.Hour),
		GrantStatus:         types.EntitlementGrantStatusActive,
	}
}

func newTestPriceService() PriceService {
	return &priceService{ServiceParams: ServiceParams{Logger: newTestLogger()}}
}

func newTestLogger() *logger.Logger {
	cfg := &config.Configuration{
		Logging: config.LoggingConfig{Level: types.LogLevelInfo},
		Secrets: config.SecretsConfig{EncryptionKey: "test-key-billing-grants"},
	}
	l, _ := logger.NewLogger(cfg)
	return l
}

func newTestBillingService() *billingService {
	return &billingService{ServiceParams: ServiceParams{Logger: newTestLogger()}}
}

func linItem(withCommitment bool, withTrueUp bool) *subscription.SubscriptionLineItem {
	li := &subscription.SubscriptionLineItem{
		ID:      "sli_x",
		MeterID: "meter_x",
	}
	if withCommitment {
		amount := decimal.NewFromInt(500)
		li.CommitmentAmount = &amount
		li.CommitmentType = types.COMMITMENT_TYPE_AMOUNT
	}
	if withTrueUp {
		li.CommitmentTrueUpEnabled = true
	}
	return li
}

func charge(price *priceDomain.Price) *dto.SubscriptionUsageByMetersResponse {
	return &dto.SubscriptionUsageByMetersResponse{
		SubscriptionLineItemID: "sli_x",
		MeterID:                "meter_x",
		Quantity:               1000,
		Price:                  price,
	}
}

func flatPrice(unit float64) *priceDomain.Price {
	return &priceDomain.Price{
		ID:           "price_flat",
		Amount:       decimal.NewFromFloat(unit),
		Currency:     "usd",
		Type:         types.PRICE_TYPE_USAGE,
		BillingModel: types.BILLING_MODEL_FLAT_FEE,
		MeterID:      "meter_x",
	}
}

func tieredPrice() *priceDomain.Price {
	return &priceDomain.Price{
		ID:           "price_tier",
		Currency:     "usd",
		Type:         types.PRICE_TYPE_USAGE,
		BillingModel: types.BILLING_MODEL_TIERED,
		TierMode:     types.BILLING_TIER_VOLUME,
		MeterID:      "meter_x",
	}
}

func TestAdjustMeterUsageGrants_NoGrants(t *testing.T) {
	// Empty slice → applied=false, no matchingCharge mutation. The billing
	// loop must fall through to the legacy entitlement path in this case.
	bs := newTestBillingService()
	li := linItem(false, false)
	c := charge(flatPrice(0.01))
	_, applied, _ := bs.adjustMeterUsageGrants(context.Background(), li, c, nil, newTestPriceService(), nil, nil, nil)
	if applied {
		t.Fatalf("empty grants should not be applied")
	}
	if c.Quantity != 1000 {
		t.Fatalf("matchingCharge.Quantity mutated on no-op")
	}
}

func makeSlotGrant(ecID string, quota, usage int64, measure types.EntitlementGrantMeasure) *entitlementgrant.EntitlementGrant {
	g := makeGrant(quota, usage, measure)
	g.ID = "eg_" + ecID + "_" + decimal.NewFromInt(quota).String()
	g.EntitlementConfigID = ecID
	return g
}

func TestAdjustMeterUsageGrants_QuantityLane_SumOfOverages(t *testing.T) {
	// All grants share one EC slot (sequential windows of the same bucket):
	// their windows partition distinct events, so overages SUM.
	bs := newTestBillingService()
	li := linItem(false, false)
	c := charge(flatPrice(0.5))
	grants := []*entitlementgrant.EntitlementGrant{
		makeGrant(100, 40, types.EntitlementGrantMeasureQuantity),   // no overage
		makeGrant(100, 250, types.EntitlementGrantMeasureQuantity),  // overage 150
		makeGrant(50, 60, types.EntitlementGrantMeasureQuantity),    // overage 10
	}

	res, applied, _ := bs.adjustMeterUsageGrants(context.Background(), li, c, grants, newTestPriceService(), nil, nil, nil)
	if !applied {
		t.Fatalf("expected applied=true for qty grants")
	}
	wantQty := decimal.NewFromInt(160) // 150 + 10
	if !res.Overage.Equal(wantQty) {
		t.Fatalf("Overage = %s; want %s", res.Overage, wantQty)
	}

	// matchingCharge.Quantity mutated to the billable qty; Amount recomputed
	// via priceService.CalculateCost(price, adjustedQty) = 0.5 * 160 = 80.
	if int(c.Quantity) != 160 {
		t.Fatalf("matchingCharge.Quantity = %v; want 160", c.Quantity)
	}
	if int(c.Amount) != 80 {
		t.Fatalf("matchingCharge.Amount = %v; want 80", c.Amount)
	}
}

func TestAdjustMeterUsageGrants_AmountLane_FlatPricingAccepted(t *testing.T) {
	// Amount grants short-circuit the pricer — overage is already priced,
	// so we plug the sum straight into Amount and zero out Quantity.
	bs := newTestBillingService()
	li := linItem(false, false)
	c := charge(flatPrice(0.01))
	grants := []*entitlementgrant.EntitlementGrant{
		makeGrant(100, 250, types.EntitlementGrantMeasureAmount), // overage 150
	}
	res, applied, _ := bs.adjustMeterUsageGrants(context.Background(), li, c, grants, newTestPriceService(), nil, nil, nil)
	if !applied {
		t.Fatalf("expected applied=true for flat-priced amount grants")
	}
	if !res.Overage.Equal(decimal.NewFromInt(150)) {
		t.Fatalf("Overage = %s; want 150", res.Overage)
	}
	if int(c.Amount) != 150 {
		t.Fatalf("matchingCharge.Amount = %v; want 150", c.Amount)
	}
	if c.Quantity != 0 {
		t.Fatalf("amount-lane must zero out matchingCharge.Quantity; got %v", c.Quantity)
	}
}

func TestAdjustMeterUsageGrants_AmountLane_TieredPriceGuardRejects(t *testing.T) {
	// Runtime guard: amount lane must NOT apply when the price is tiered.
	// The caller then falls back to the legacy entitlement adjustment.
	bs := newTestBillingService()
	li := linItem(false, false)
	c := charge(tieredPrice())
	grants := []*entitlementgrant.EntitlementGrant{
		makeGrant(100, 250, types.EntitlementGrantMeasureAmount),
	}
	_, applied, _ := bs.adjustMeterUsageGrants(context.Background(), li, c, grants, newTestPriceService(), nil, nil, nil)
	if applied {
		t.Fatalf("tiered price must reject amount-lane grants")
	}
	if c.Quantity != 1000 {
		t.Fatalf("matchingCharge should be untouched when guard trips")
	}
}

func TestAdjustMeterUsageGrants_AmountLane_LineItemCommitmentGuardRejects(t *testing.T) {
	// Same guard — line-item commitment breaks per-window pricing composability.
	bs := newTestBillingService()
	li := linItem(true, false)
	c := charge(flatPrice(0.01))
	grants := []*entitlementgrant.EntitlementGrant{
		makeGrant(100, 200, types.EntitlementGrantMeasureAmount),
	}
	_, applied, _ := bs.adjustMeterUsageGrants(context.Background(), li, c, grants, newTestPriceService(), nil, nil, nil)
	if applied {
		t.Fatalf("committed line item must reject amount-lane grants")
	}
}

func TestAdjustMeterUsageGrants_AmountLane_TrueUpGuardRejects(t *testing.T) {
	bs := newTestBillingService()
	li := linItem(false, true)
	c := charge(flatPrice(0.01))
	grants := []*entitlementgrant.EntitlementGrant{
		makeGrant(100, 200, types.EntitlementGrantMeasureAmount),
	}
	_, applied, _ := bs.adjustMeterUsageGrants(context.Background(), li, c, grants, newTestPriceService(), nil, nil, nil)
	if applied {
		t.Fatalf("true-up enabled must reject amount-lane grants")
	}
}

func TestAdjustMeterUsageGrants_QuantityLane_TieredPriceGuardRejects(t *testing.T) {
	// The quantity lane prices the overage standalone — on a tiered price that
	// would restart at tier 1 and misprice the marginal units. Guard rejects;
	// the caller falls back to the legacy path.
	bs := newTestBillingService()
	li := linItem(false, false)
	c := charge(tieredPrice())
	grants := []*entitlementgrant.EntitlementGrant{
		makeGrant(100, 250, types.EntitlementGrantMeasureQuantity),
	}
	_, applied, _ := bs.adjustMeterUsageGrants(context.Background(), li, c, grants, newTestPriceService(), nil, nil, nil)
	if applied {
		t.Fatalf("tiered price must reject quantity-lane grants too")
	}
	if c.Quantity != 1000 {
		t.Fatalf("matchingCharge should be untouched when guard trips")
	}
}

func TestAdjustMeterUsageGrants_NonAdditiveAggregationGuardRejects(t *testing.T) {
	// Snapshot sums and merged-window measurement assume usage is additive over
	// disjoint time windows; MAX/LATEST/AVG do not decompose that way.
	bs := newTestBillingService()
	li := linItem(false, false)
	c := charge(flatPrice(0.01))
	grants := []*entitlementgrant.EntitlementGrant{
		makeGrant(100, 250, types.EntitlementGrantMeasureQuantity),
	}
	m := &meter.Meter{ID: "meter_max", Aggregation: meter.Aggregation{Type: types.AggregationMax}}
	_, applied, _ := bs.adjustMeterUsageGrants(context.Background(), li, c, grants, newTestPriceService(), m, nil, nil)
	if applied {
		t.Fatalf("MAX aggregation must reject grant folding")
	}
	if c.Quantity != 1000 {
		t.Fatalf("matchingCharge should be untouched when guard trips")
	}
}

func TestAdjustMeterUsageGrants_BucketedMeterGuardRejects(t *testing.T) {
	// Bucketed meters price through their own bucketed cost path; window
	// measurement over raw usage would disagree with it.
	bs := newTestBillingService()
	li := linItem(false, false)
	c := charge(flatPrice(0.01))
	grants := []*entitlementgrant.EntitlementGrant{
		makeGrant(100, 250, types.EntitlementGrantMeasureQuantity),
	}
	m := &meter.Meter{ID: "meter_bucketed", Aggregation: meter.Aggregation{Type: types.AggregationSum, BucketSize: types.WindowSizeHour}}
	_, applied, _ := bs.adjustMeterUsageGrants(context.Background(), li, c, grants, newTestPriceService(), m, nil, nil)
	if applied {
		t.Fatalf("bucketed meter must reject grant folding")
	}
}

// ---------------------------------------------------------------------------
// Merged overage windows: grants pre-materialized (quota_crossed_at, as the
// evaluator writes it), fold measures usage inside the merged windows.
// ---------------------------------------------------------------------------

var unionT0 = time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)

// makeCrossedGrant builds a grant whose quota exhaustion the evaluator has
// already recorded.
func makeCrossedGrant(ecID string, quota, usage int64, from, to time.Time, crossedAt time.Time) *entitlementgrant.EntitlementGrant {
	g := makeGrant(quota, usage, types.EntitlementGrantMeasureQuantity)
	g.ID = "eg_" + ecID + "_" + from.Format("15:04:05")
	g.EntitlementConfigID = ecID
	g.ValidFrom = from
	g.ValidTo = to
	g.QuotaCrossedAt = &crossedAt
	return g
}

func newMergedWindowBillingService(t *testing.T, events []struct {
	at  time.Time
	qty int64
}) (*billingService, *meter.Meter) {
	t.Helper()
	store := testutil.NewInMemoryMeterUsageStore()
	rows := make([]*eventsDomain.MeterUsage, 0, len(events))
	for i, e := range events {
		rows = append(rows, &eventsDomain.MeterUsage{
			Event: eventsDomain.Event{
				ID:                 decimal.NewFromInt(int64(i)).String(),
				ExternalCustomerID: "cust_ext",
				EventName:          "api_call",
				Timestamp:          e.at,
			},
			MeterID:  "meter_x",
			QtyTotal: decimal.NewFromInt(e.qty),
		})
	}
	if err := store.BulkInsertMeterUsage(context.Background(), rows); err != nil {
		t.Fatalf("seed failed: %v", err)
	}
	bs := &billingService{ServiceParams: ServiceParams{Logger: newTestLogger(), MeterUsageRepo: store}}
	m := &meter.Meter{ID: "meter_x", Aggregation: meter.Aggregation{Type: types.AggregationSum}}
	return bs, m
}

func TestAdjustMeterUsageGrants_MergedWindows_DisjointWindowsBothBill(t *testing.T) {
	// Two ECs exhausted at different times with non-overlapping overage
	// windows: usage after each exhaustion bills — 300 + 700 = 1000.
	dayCross := unionT0.Add(2 * time.Hour)
	weekCross := unionT0.Add(3 * 24 * time.Hour)
	bs, m := newMergedWindowBillingService(t, []struct {
		at  time.Time
		qty int64
	}{
		{unionT0.Add(1 * time.Hour), 3000},     // pre-exhaustion, never billed
		{unionT0.Add(4 * time.Hour), 300},      // after day EC exhausted
		{unionT0.Add(4 * 24 * time.Hour), 700}, // after week EC exhausted
	})

	grants := []*entitlementgrant.EntitlementGrant{
		makeCrossedGrant("ec_day", 2000, 3300, unionT0, unionT0.Add(24*time.Hour), dayCross),
		makeCrossedGrant("ec_week", 3500, 4000, unionT0, unionT0.Add(7*24*time.Hour), weekCross),
	}
	li := linItem(false, false)
	c := charge(flatPrice(1))

	res, applied, _ := bs.adjustMeterUsageGrants(context.Background(), li, c, grants, newTestPriceService(), m, nil, []string{"cust_ext"})
	if !applied {
		t.Fatalf("expected applied=true")
	}
	if !res.Overage.Equal(decimal.NewFromInt(1000)) {
		t.Fatalf("Overage = %s; want 1000 (300 after day cross + 700 after week cross)", res.Overage)
	}
}

func TestAdjustMeterUsageGrants_MergedWindows_OverlapBillsOnce(t *testing.T) {
	// Both ECs in overage over an overlapping stretch: the windows merge, so
	// shared usage bills once — 50 (EC-a only region) + 100 (shared) = 150,
	// never 250.
	aCross := unionT0.Add(10 * time.Minute)
	bCross := unionT0.Add(30 * time.Minute)
	bs, m := newMergedWindowBillingService(t, []struct {
		at  time.Time
		qty int64
	}{
		{unionT0.Add(20 * time.Minute), 50},  // inside EC-a's window only
		{unionT0.Add(40 * time.Minute), 100}, // inside both windows
	})

	grants := []*entitlementgrant.EntitlementGrant{
		makeCrossedGrant("ec_a", 10, 160, unionT0, unionT0.Add(time.Hour), aCross),
		makeCrossedGrant("ec_b", 40, 150, unionT0, unionT0.Add(24*time.Hour), bCross),
	}
	li := linItem(false, false)
	c := charge(flatPrice(1))

	res, applied, _ := bs.adjustMeterUsageGrants(context.Background(), li, c, grants, newTestPriceService(), m, nil, []string{"cust_ext"})
	if !applied {
		t.Fatalf("expected applied=true")
	}
	if !res.Overage.Equal(decimal.NewFromInt(150)) {
		t.Fatalf("Overage = %s; want 150 (shared usage bills once)", res.Overage)
	}
}

func TestAdjustMeterUsageGrants_MeasurementFailurePropagates(t *testing.T) {
	// Money movement never bills a knowingly wrong number: when the merged
	// windows can't be measured (here: no usage repo), the error propagates
	// and the charge calculation fails instead of billing an approximation.
	bs := newTestBillingService() // nil MeterUsageRepo
	li := linItem(false, false)
	c := charge(flatPrice(1))
	cross := unionT0.Add(10 * time.Minute)
	grants := []*entitlementgrant.EntitlementGrant{
		makeCrossedGrant("ec_a", 10, 150, unionT0, unionT0.Add(time.Hour), cross),
		makeCrossedGrant("ec_b", 100, 150, unionT0, unionT0.Add(24*time.Hour), cross),
	}
	_, applied, err := bs.adjustMeterUsageGrants(context.Background(), li, c, grants, newTestPriceService(), nil, nil, nil)
	if err == nil {
		t.Fatalf("expected measurement error to propagate")
	}
	if applied {
		t.Fatalf("nothing must be applied on error")
	}
}

func TestAdjustMeterUsageGrants_MultiEC_NothingCrossed_QueryFree(t *testing.T) {
	// Multiple ECs, none exhausted (or exhaustion not yet recorded): zero
	// overage and zero queries — nil MeterUsageRepo proves ClickHouse is
	// never touched. A recording lag just delays billing to the next pass.
	bs := newTestBillingService()
	li := linItem(false, false)
	c := charge(flatPrice(1))
	grants := []*entitlementgrant.EntitlementGrant{
		makeSlotGrant("ec_a", 100, 150, types.EntitlementGrantMeasureQuantity), // over quota, crossing not recorded yet
		makeSlotGrant("ec_b", 200, 40, types.EntitlementGrantMeasureQuantity),
	}
	res, applied, _ := bs.adjustMeterUsageGrants(context.Background(), li, c, grants, newTestPriceService(), nil, nil, nil)
	if !applied {
		t.Fatalf("expected applied=true")
	}
	if !res.Overage.IsZero() {
		t.Fatalf("Overage = %s; want 0 (bills on the next pass once recorded)", res.Overage)
	}
	if !res.PerECOverage["ec_a"].Equal(decimal.NewFromInt(50)) {
		t.Fatalf("PerECOverage attribution wrong: %v", res.PerECOverage)
	}
}

func TestAdjustMeterUsageGrants_AllUnderQuota_ZerosBillable(t *testing.T) {
	// Every grant has usage <= quota → total overage is zero. Qty lane
	// pushes 0 into matchingCharge.Quantity (nothing billable this cycle).
	bs := newTestBillingService()
	li := linItem(false, false)
	c := charge(flatPrice(0.5))
	grants := []*entitlementgrant.EntitlementGrant{
		makeGrant(100, 40, types.EntitlementGrantMeasureQuantity),
		makeGrant(200, 100, types.EntitlementGrantMeasureQuantity),
	}
	res, applied, _ := bs.adjustMeterUsageGrants(context.Background(), li, c, grants, newTestPriceService(), nil, nil, nil)
	if !applied {
		t.Fatalf("expected applied=true (grants exist even if no overage)")
	}
	if !res.Overage.IsZero() {
		t.Fatalf("Overage should be zero, got %s", res.Overage)
	}
	if c.Amount != 0 || c.Quantity != 0 {
		t.Fatalf("under-quota grants should zero the charge, got amount=%v qty=%v", c.Amount, c.Quantity)
	}
}
