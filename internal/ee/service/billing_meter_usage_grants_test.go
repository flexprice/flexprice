package service

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/entitlementgrant"
	priceDomain "github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/logger"
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
	_, applied := bs.adjustMeterUsageGrants(context.Background(), li, c, nil, newTestPriceService())
	if applied {
		t.Fatalf("empty grants should not be applied")
	}
	if c.Quantity != 1000 {
		t.Fatalf("matchingCharge.Quantity mutated on no-op")
	}
}

func TestAdjustMeterUsageGrants_QuantityLane_SumOfOverages(t *testing.T) {
	// Two quantity grants — one under quota, one over. Overage-sum model
	// (ERD §11.3): sum(max(0, usage−quota)) across grants. Only the second
	// contributes; first returns 0.
	bs := newTestBillingService()
	li := linItem(false, false)
	c := charge(flatPrice(0.5))
	grants := []*entitlementgrant.EntitlementGrant{
		makeGrant(100, 40, types.EntitlementGrantMeasureQuantity),   // no overage
		makeGrant(100, 250, types.EntitlementGrantMeasureQuantity),  // overage 150
		makeGrant(50, 60, types.EntitlementGrantMeasureQuantity),    // overage 10
	}

	res, applied := bs.adjustMeterUsageGrants(context.Background(), li, c, grants, newTestPriceService())
	if !applied {
		t.Fatalf("expected applied=true for qty grants")
	}
	wantQty := decimal.NewFromInt(160) // 150 + 10
	if !res.AdjustedQty.Equal(wantQty) {
		t.Fatalf("AdjustedQty = %s; want %s", res.AdjustedQty, wantQty)
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
	res, applied := bs.adjustMeterUsageGrants(context.Background(), li, c, grants, newTestPriceService())
	if !applied {
		t.Fatalf("expected applied=true for flat-priced amount grants")
	}
	if !res.OverageAmount.Equal(decimal.NewFromInt(150)) {
		t.Fatalf("OverageAmount = %s; want 150", res.OverageAmount)
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
	_, applied := bs.adjustMeterUsageGrants(context.Background(), li, c, grants, newTestPriceService())
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
	_, applied := bs.adjustMeterUsageGrants(context.Background(), li, c, grants, newTestPriceService())
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
	_, applied := bs.adjustMeterUsageGrants(context.Background(), li, c, grants, newTestPriceService())
	if applied {
		t.Fatalf("true-up enabled must reject amount-lane grants")
	}
}

func TestAdjustMeterUsageGrants_MixedMeasureSkips(t *testing.T) {
	// Schema drift / admin surgery could produce a mixed-measure grant set on
	// one feature. Rather than pick one and silently ignore the other, fall
	// through to the legacy adjustment.
	bs := newTestBillingService()
	li := linItem(false, false)
	c := charge(flatPrice(0.5))
	grants := []*entitlementgrant.EntitlementGrant{
		makeGrant(100, 250, types.EntitlementGrantMeasureQuantity),
		makeGrant(100, 250, types.EntitlementGrantMeasureAmount),
	}
	_, applied := bs.adjustMeterUsageGrants(context.Background(), li, c, grants, newTestPriceService())
	if applied {
		t.Fatalf("mixed measures must fall through, got applied=true")
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
	res, applied := bs.adjustMeterUsageGrants(context.Background(), li, c, grants, newTestPriceService())
	if !applied {
		t.Fatalf("expected applied=true (grants exist even if no overage)")
	}
	if !res.AdjustedQty.IsZero() {
		t.Fatalf("AdjustedQty should be zero, got %s", res.AdjustedQty)
	}
	if c.Amount != 0 || c.Quantity != 0 {
		t.Fatalf("under-quota grants should zero the charge, got amount=%v qty=%v", c.Amount, c.Quantity)
	}
}
