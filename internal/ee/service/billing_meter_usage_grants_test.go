package service

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/entitlement"
	"github.com/flexprice/flexprice/internal/domain/entitlementgrant"
	eventsDomain "github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/meter"
	priceDomain "github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
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

// sequentialWindows tiles the grants into back-to-back one-hour windows. makeGrant
// hands every row the same span, which would read as overlapping slots — the shape
// the merged-window measurement exists for, not the sequential one these tests mean.
func sequentialWindows(grants ...*entitlementgrant.EntitlementGrant) []*entitlementgrant.EntitlementGrant {
	start := time.Now().Add(-time.Duration(len(grants)) * time.Hour)
	for i, g := range grants {
		g.ValidFrom = start.Add(time.Duration(i) * time.Hour)
		g.ValidTo = g.ValidFrom.Add(time.Hour)
	}
	return grants
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
	grants := sequentialWindows(
		makeGrant(100, 40, types.EntitlementGrantMeasureQuantity),  // no overage
		makeGrant(100, 250, types.EntitlementGrantMeasureQuantity), // overage 150
		makeGrant(50, 60, types.EntitlementGrantMeasureQuantity),   // overage 10
	)

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

func decs(vals ...string) []decimal.Decimal {
	return lo.Map(vals, func(v string, _ int) decimal.Decimal { return decimal.RequireFromString(v) })
}

func TestSplitByShare(t *testing.T) {
	tests := []struct {
		name   string
		gross  []decimal.Decimal
		charge string
		want   []decimal.Decimal
	}{
		{"prd example", decs("70", "50"), "90", decs("52.5", "37.5")},
		{"daily windows example", decs("30", "10"), "20", decs("15", "5")},
		{"nothing over quota", decs("70", "50"), "0", decs("0", "0")},
		{"charge above gross bills gross", decs("70", "50"), "150", decs("70", "50")},
		{"exact split", decs("10", "10", "20"), "10", decs("2.5", "2.5", "5")},
		{"thirds add up to the charge", decs("10", "10", "10.01"), "10", decs("3.33", "3.33", "3.34")},
		{"zero and negative lines get nothing", decs("0", "-5", "40"), "10", decs("0", "0", "10")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := splitByShare(tt.gross, decimal.RequireFromString(tt.charge), 2)
			require.Len(t, got, len(tt.want))
			for i := range tt.want {
				require.True(t, tt.want[i].Equal(got[i]), "line %d: want %s, got %s", i, tt.want[i], got[i])
			}
		})
	}
}

func (s *EntitlementGrantSuite) subGrantBilling() *billingService {
	return &billingService{ServiceParams: s.buildServiceParams()}
}

// seedSubGrantWindow stores a subscription grant window with its evaluated usage.
func (s *EntitlementGrantSuite) seedSubGrantWindow(id string, fx subGrantFixture, from, to time.Time, quota, usage int64, crossedAt *time.Time) *entitlementgrant.EntitlementGrant {
	g := &entitlementgrant.EntitlementGrant{
		ID:                  id,
		EntitlementConfigID: "ec_" + id,
		CustomerID:          fx.sub.CustomerID,
		SubscriptionID:      fx.sub.ID,
		ScopeEntityType:     types.EntitlementGrantScopeSubscription,
		ScopeEntityID:       fx.sub.ID,
		Measure:             types.EntitlementGrantMeasureAmount,
		Quota:               decimal.NewFromInt(quota),
		Usage:               decimal.NewFromInt(usage),
		QuotaCrossedAt:      crossedAt,
		ValidFrom:           from,
		ValidTo:             to,
		GrantStatus:         types.EntitlementGrantStatusActive,
		EnvironmentID:       types.GetEnvironmentID(s.GetContext()),
		BaseModel:           types.GetDefaultBaseModel(s.GetContext()),
	}
	_, err := s.GetStores().EntitlementGrantRepo.Create(s.GetContext(), g)
	s.Require().NoError(err)
	return g
}

// subGrantBlockers are the billing inputs that can stop subscription grants from applying.
type subGrantBlockers struct {
	featureGrants     map[string][]*entitlementgrant.EntitlementGrant
	commitmentLineIDs map[string]bool
}

// invoiceLines builds one invoice line per usage line with the given gross amounts.
func invoiceLines(fx subGrantFixture, calls, store string) []dto.CreateInvoiceLineItemRequest {
	return []dto.CreateInvoiceLineItemRequest{
		{SubscriptionLineItemID: lo.ToPtr(fx.callsLine), Amount: decimal.RequireFromString(calls), Quantity: decimal.NewFromInt(1)},
		{SubscriptionLineItemID: lo.ToPtr(fx.storeLine), Amount: decimal.RequireFromString(store), Quantity: decimal.NewFromInt(1)},
	}
}

func (s *EntitlementGrantSuite) applySubGrants(fx subGrantFixture, lines []dto.CreateInvoiceLineItemRequest, blockers subGrantBlockers) decimal.Decimal {
	b := s.subGrantBilling()
	start, end := fx.sub.CurrentPeriodStart, fx.sub.CurrentPeriodEnd
	grants, err := b.loadSubscriptionGrants(s.GetContext(), fx.sub, blockers.featureGrants, nil, blockers.commitmentLineIDs, nil, start, end)
	s.Require().NoError(err)
	reduction, err := b.applySubscriptionGrants(s.GetContext(), fx.sub, grants, lines, start, end)
	s.Require().NoError(err)
	return reduction
}

func (s *EntitlementGrantSuite) TestSubscriptionGrantBilling_SplitsChargeByShare() {
	fx := s.newSubGrantFixture("bill-share", dailySubscriptionGrant(30, types.EntitlementGrantAllocationBehaviorFirstUsage))
	start := fx.sub.CurrentPeriodStart
	s.seedSubGrantWindow("eg-share", fx, start, fx.sub.CurrentPeriodEnd, 30, 120, nil)

	lines := invoiceLines(fx, "70", "50")
	reduction := s.applySubGrants(fx, lines, subGrantBlockers{})

	s.True(decimal.NewFromInt(30).Equal(reduction), "got %s", reduction)
	s.True(decimal.RequireFromString("52.5").Equal(lines[0].Amount), "calls: got %s", lines[0].Amount)
	s.True(decimal.RequireFromString("37.5").Equal(lines[1].Amount), "storage: got %s", lines[1].Amount)
	s.Equal("17.5", lines[0].Metadata[metadataKeySubscriptionGrantAllowance])
	s.Equal("12.5", lines[1].Metadata[metadataKeySubscriptionGrantAllowance])
	s.True(decimal.NewFromInt(1).Equal(lines[0].Quantity), "quantity keeps the real usage")
}

func (s *EntitlementGrantSuite) TestSubscriptionGrantBilling_DisjointWindowsAddUp() {
	fx := s.newSubGrantFixture("bill-days", dailySubscriptionGrant(10, types.EntitlementGrantAllocationBehaviorFirstUsage))
	day := fx.sub.CurrentPeriodStart
	// Day 1: X $30 against $10 (over by $20). Day 2: Y $10, fully free.
	s.seedSubGrantWindow("eg-day1", fx, day, day.Add(24*time.Hour), 10, 30, nil)
	s.seedSubGrantWindow("eg-day2", fx, day.Add(24*time.Hour), day.Add(48*time.Hour), 10, 10, nil)

	lines := invoiceLines(fx, "30", "10")
	reduction := s.applySubGrants(fx, lines, subGrantBlockers{})

	s.True(decimal.NewFromInt(20).Equal(reduction), "got %s", reduction)
	s.True(decimal.NewFromInt(15).Equal(lines[0].Amount), "calls: got %s", lines[0].Amount)
	s.True(decimal.NewFromInt(5).Equal(lines[1].Amount), "storage: got %s", lines[1].Amount)
}

func (s *EntitlementGrantSuite) TestSubscriptionGrantBilling_ParallelBillsOwnSpendAfterCrossing() {
	fx := s.newSubGrantFixture("bill-par", dailySubscriptionGrant(10, types.EntitlementGrantAllocationBehaviorFirstUsage))
	start := fx.sub.CurrentPeriodStart
	crossed := start.Add(2 * time.Hour)
	// Before the crossing: calls $10. After: calls $3, storage $4.
	s.seedMeterUsage(fx.extID, fx.callsMeter, start.Add(time.Hour), 100)
	s.seedMeterUsage(fx.extID, fx.callsMeter, crossed.Add(time.Hour), 30)
	s.seedMeterUsage(fx.extID, fx.storeMeter, crossed.Add(2*time.Hour), 4)
	// Two parallel windows over the same span, both crossed at the same evaluation.
	end := start.Add(24 * time.Hour)
	s.seedSubGrantWindow("eg-par-a", fx, start, end, 10, 17, lo.ToPtr(crossed))
	s.seedSubGrantWindow("eg-par-b", fx, start, end, 8, 17, lo.ToPtr(crossed))

	lines := invoiceLines(fx, "13", "4")
	reduction := s.applySubGrants(fx, lines, subGrantBlockers{})

	s.True(decimal.NewFromInt(3).Equal(lines[0].Amount), "calls bills its spend after the crossing, got %s", lines[0].Amount)
	s.True(decimal.NewFromInt(4).Equal(lines[1].Amount), "storage bills its spend after the crossing, got %s", lines[1].Amount)
	s.True(decimal.NewFromInt(10).Equal(reduction), "got %s", reduction)
}

func (s *EntitlementGrantSuite) TestSubscriptionGrantBilling_RejectedSubscriptionsBillInFull() {
	tests := []struct {
		name  string
		setup func(fx subGrantFixture) subGrantBlockers
	}{
		{"minimum spend", func(fx subGrantFixture) subGrantBlockers {
			fx.sub.CommitmentAmount = lo.ToPtr(decimal.NewFromInt(100))
			return subGrantBlockers{}
		}},
		{"line priced by a commitment", func(fx subGrantFixture) subGrantBlockers {
			return subGrantBlockers{commitmentLineIDs: map[string]bool{fx.storeLine: true}}
		}},
		{"line on another cadence", func(fx subGrantFixture) subGrantBlockers {
			item, err := s.GetStores().SubscriptionLineItemRepo.Get(s.GetContext(), fx.storeLine)
			s.Require().NoError(err)
			item.BillingPeriod = types.BILLING_PERIOD_WEEKLY
			s.Require().NoError(s.GetStores().SubscriptionLineItemRepo.Update(s.GetContext(), item))
			return subGrantBlockers{}
		}},
		{"feature grants on the subscription", func(fx subGrantFixture) subGrantBlockers {
			return subGrantBlockers{featureGrants: map[string][]*entitlementgrant.EntitlementGrant{fx.callsMeter: {{ID: "eg-feature"}}}}
		}},
	}
	for i, tt := range tests {
		s.Run(tt.name, func() {
			fx := s.newSubGrantFixture("bill-reject-"+string(rune('a'+i)), dailySubscriptionGrant(30, types.EntitlementGrantAllocationBehaviorFirstUsage))
			s.seedSubGrantWindow("eg-reject-"+string(rune('a'+i)), fx, fx.sub.CurrentPeriodStart, fx.sub.CurrentPeriodEnd, 30, 120, nil)
			blockers := tt.setup(fx)

			lines := invoiceLines(fx, "70", "50")
			reduction := s.applySubGrants(fx, lines, blockers)

			s.True(reduction.IsZero(), "got %s", reduction)
			s.True(decimal.NewFromInt(70).Equal(lines[0].Amount))
			s.True(decimal.NewFromInt(50).Equal(lines[1].Amount))
		})
	}
}

func (s *EntitlementGrantSuite) TestSubscriptionGrantBilling_InvoiceLinesThroughMeterCharges() {
	fx := s.newSubGrantFixture("bill-e2e", dailySubscriptionGrant(30, types.EntitlementGrantAllocationBehaviorFirstUsage))
	start := fx.sub.CurrentPeriodStart
	s.seedMeterUsage(fx.extID, fx.callsMeter, start.Add(time.Hour), 700)  // $70
	s.seedMeterUsage(fx.extID, fx.storeMeter, start.Add(2*time.Hour), 50) // $50
	s.seedSubGrantWindow("eg-e2e", fx, start, fx.sub.CurrentPeriodEnd, 30, 120, nil)

	ctx := s.GetContext()
	params := s.buildServiceParams()
	items, err := s.GetStores().SubscriptionLineItemRepo.ListBySubscription(ctx, fx.sub)
	s.Require().NoError(err)
	fx.sub.LineItems = items
	usage, err := NewSubscriptionService(params).GetMeterUsageBySubscription(ctx, &dto.GetUsageBySubscriptionRequest{
		SubscriptionID: fx.sub.ID,
		StartTime:      start,
		EndTime:        fx.sub.CurrentPeriodEnd,
		Source:         string(types.UsageSourceInvoiceCreation),
	})
	s.Require().NoError(err)

	asOf := fx.sub.CurrentPeriodEnd
	lines, total, err := s.subGrantBilling().CalculateMeterUsageCharges(ctx, fx.sub, usage,
		start, fx.sub.CurrentPeriodEnd, types.UsageSourceInvoiceCreation, &asOf)
	s.Require().NoError(err)

	byLine := make(map[string]decimal.Decimal)
	for _, l := range lines {
		byLine[lo.FromPtr(l.SubscriptionLineItemID)] = l.Amount
	}
	s.True(decimal.RequireFromString("52.5").Equal(byLine[fx.callsLine]), "calls: got %s", byLine[fx.callsLine])
	s.True(decimal.RequireFromString("37.5").Equal(byLine[fx.storeLine]), "storage: got %s", byLine[fx.storeLine])
	s.True(decimal.NewFromInt(90).Equal(total), "total: got %s", total)
}

// A $50 monthly grant with a $15 auto invoice threshold: two threshold invoices and the month-end
// invoice bill $16, $16 and $18, which is the $100 spent less the $50 grant.
func (s *EntitlementGrantSuite) TestSubscriptionGrantBilling_MonthlyGrantAcrossThresholdInvoices() {
	fx := s.newSubGrantFixture("bill-threshold", &entitlement.Entitlement{
		GrantDurationUnit: types.EntitlementGrantDurationUnitSubscriptionPeriod,
		GrantQuota:        lo.ToPtr(decimal.NewFromInt(50)),
	})
	ctx := s.GetContext()
	items, err := s.GetStores().SubscriptionLineItemRepo.ListBySubscription(ctx, fx.sub)
	s.Require().NoError(err)
	fx.sub.LineItems = items
	periodEnd := fx.sub.CurrentPeriodEnd
	day := func(n int) time.Time { return fx.sub.StartDate.Add(time.Duration(n) * 24 * time.Hour) }

	// billedSince refreshes the grant at `at` and prices usage since `from`, as the threshold job does.
	billedSince := func(from, at time.Time) decimal.Decimal {
		grants, meta, err := s.grantService.EnsureGrantsForSubscriptions(ctx, s.customerOf(fx.sub), []*subscription.Subscription{fx.sub}, at)
		s.Require().NoError(err)
		s.Require().Len(grants, 1, "the threshold invoice must not open a second monthly window")
		s.Require().NoError(s.evalAlertService().evaluateEntitlementGrantsForCustomer(ctx, s.customerOf(fx.sub), meta, grants, at))
		// The threshold check's calculation, with the clock at `at` (the fixture subscription starts now).
		result, err := s.subGrantBilling().calculateMeterUsageCharges(ctx, fx.sub, fx.sub.LineItems, from, at, true, &at)
		s.Require().NoError(err)
		return result.TotalAmount
	}
	// invoice is a threshold invoice at `at`: the period restarts there.
	invoice := func(at time.Time) decimal.Decimal {
		amount := billedSince(fx.sub.CurrentPeriodStart, at)
		fx.sub.CurrentPeriodStart = at
		s.Require().NoError(s.GetStores().SubscriptionRepo.Update(ctx, fx.sub))
		return amount
	}

	s.seedMeterUsage(fx.extID, fx.callsMeter, day(10).Add(time.Hour), 400) // $40, month $40
	s.True(billedSince(fx.sub.CurrentPeriodStart, day(10).Add(2*time.Hour)).IsZero(), "inside the grant")

	s.seedMeterUsage(fx.extID, fx.storeMeter, day(13).Add(time.Hour), 20) // $20, month $60
	s.True(decimal.NewFromInt(10).Equal(billedSince(fx.sub.CurrentPeriodStart, day(13).Add(2*time.Hour))), "below the threshold")

	s.seedMeterUsage(fx.extID, fx.callsMeter, day(14).Add(time.Hour), 60) // $6, month $66
	first := invoice(day(14).Add(2 * time.Hour))
	s.True(decimal.NewFromInt(16).Equal(first), "first threshold invoice: got %s", first)

	s.seedMeterUsage(fx.extID, fx.storeMeter, day(18).Add(time.Hour), 10) // $10, month $76
	s.True(decimal.NewFromInt(10).Equal(billedSince(fx.sub.CurrentPeriodStart, day(18).Add(2*time.Hour))), "only spend since the cut")

	s.seedMeterUsage(fx.extID, fx.callsMeter, day(21).Add(time.Hour), 60) // $6, month $82
	second := invoice(day(21).Add(2 * time.Hour))
	s.True(decimal.NewFromInt(16).Equal(second), "second threshold invoice: got %s", second)

	s.seedMeterUsage(fx.extID, fx.storeMeter, day(28).Add(time.Hour), 18) // $18, month $100
	last := billedSince(fx.sub.CurrentPeriodStart, periodEnd)
	s.True(decimal.NewFromInt(18).Equal(last), "month-end invoice: got %s", last)
	s.True(decimal.NewFromInt(50).Equal(first.Add(second).Add(last)), "$100 spent less the $50 grant")
}
