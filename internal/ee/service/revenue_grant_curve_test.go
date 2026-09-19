package service

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/domain/entitlementgrant"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// grantCurveFixture seeds usage at perDay/day for `days` days and returns a
// service + input ready for buildGrantOverageCurve.
func grantCurveFixture(t *testing.T, ctx context.Context, perDay int64, days int, grants []*entitlementgrant.EntitlementGrant) (*revenueService, grantCurveInput) {
	t.Helper()
	store := testutil.NewInMemoryMeterUsageStore()

	periodStart := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	const meterID = "meter_grant_curve"
	records := make([]*events.MeterUsage, 0, days)
	for d := 0; d < days; d++ {
		ts := periodStart.AddDate(0, 0, d).Add(time.Hour)
		id := types.GenerateUUIDWithPrefix("mu_grant")
		records = append(records, &events.MeterUsage{
			Event: events.Event{
				ID:            id,
				TenantID:      types.GetTenantID(ctx),
				EnvironmentID: types.GetEnvironmentID(ctx),
				EventName:     "grant_curve_event",
				Timestamp:     ts,
				IngestedAt:    ts,
			},
			MeterID:    meterID,
			QtyTotal:   decimal.NewFromInt(perDay),
			UniqueHash: id,
		})
	}
	require.NoError(t, store.BulkInsertMeterUsage(ctx, records))

	svc := &revenueService{ServiceParams: ServiceParams{
		Logger:         logger.NewNoopLogger(),
		MeterUsageRepo: store,
	}}
	in := grantCurveInput{
		Price:       &price.Price{ID: "price_grant_curve", Amount: decimal.RequireFromString("0.01"), Currency: "usd", Type: types.PRICE_TYPE_USAGE, BillingModel: types.BILLING_MODEL_FLAT_FEE},
		Meter:       &meter.Meter{ID: meterID, Aggregation: meter.Aggregation{Type: types.AggregationSum}},
		PeriodStart: periodStart,
		PeriodEnd:   periodStart.AddDate(0, 0, days),
		Grants:      grants,
	}
	return svc, in
}

func grantCurveGrant(id string, crossedAt *time.Time, validFrom, validTo time.Time, ec string) *entitlementgrant.EntitlementGrant {
	return &entitlementgrant.EntitlementGrant{
		ID:                  id,
		EntitlementConfigID: ec,
		Measure:             types.EntitlementGrantMeasureQuantity,
		Quota:               decimal.NewFromInt(1),
		QuotaCrossedAt:      crossedAt,
		ValidFrom:           validFrom,
		ValidTo:             validTo,
	}
}

// TestGrantOverageCurve_ParallelECsCountOnce: two ECs with overlapping
// overage windows merge — a unit inside both windows bills once.
func TestGrantOverageCurve_ParallelECsCountOnce(t *testing.T) {
	ctx := context.Background()
	periodStart := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	end := periodStart.AddDate(0, 0, 10)

	// EC A in overage days 2..7, EC B days 5..10 — union covers days 2..10
	// (8 days x 100 = 800 billed units); the day 5..7 overlap must not double.
	crossedA := periodStart.AddDate(0, 0, 2)
	crossedB := periodStart.AddDate(0, 0, 5)
	grants := []*entitlementgrant.EntitlementGrant{
		grantCurveGrant("eg_a", &crossedA, periodStart, periodStart.AddDate(0, 0, 7), "ec_a"),
		grantCurveGrant("eg_b", &crossedB, periodStart, end, "ec_b"),
	}
	svc, in := grantCurveFixture(t, ctx, 100, 10, grants)
	in.EngineAmount = decimal.NewFromInt(8) // 800 units x $0.01

	curve, ok, err := svc.buildGrantOverageCurve(ctx, in)
	require.NoError(t, err)
	require.True(t, ok)
	require.Len(t, curve, 10)

	last := curve[len(curve)-1]
	assert.Equal(t, "800", last.CumulativeBillableQty.String(), "overlap must count once")
	assert.Equal(t, "1000", last.CumulativeGrossQty.String())
	assert.Equal(t, "200", last.CumulativeEntitlementQty.String(), "days before any crossing stay entitled")
	assert.True(t, last.CumulativeCharge.Equal(in.EngineAmount), "curve must sum to the engine charge")
	assert.True(t, curve[1].CumulativeCharge.IsZero(), "pre-crossing days bill nothing")
}

// TestGrantOverageCurve_ScaleAbsorbsSnapshotDrift: when the engine's charge
// (priced from frozen snapshots) differs from measured window usage, the
// curve scales to the engine total and every marginal row still satisfies
// the reconciliation identity.
func TestGrantOverageCurve_ScaleAbsorbsSnapshotDrift(t *testing.T) {
	ctx := context.Background()
	periodStart := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	crossed := periodStart.AddDate(0, 0, 5)
	grants := []*entitlementgrant.EntitlementGrant{
		grantCurveGrant("eg_drift", &crossed, periodStart, periodStart.AddDate(0, 0, 10), "ec_a"),
	}
	svc, in := grantCurveFixture(t, ctx, 100, 10, grants)
	// Measured window usage = 500 units = $5; the engine billed $6.
	in.EngineAmount = decimal.NewFromInt(6)

	curve, ok, err := svc.buildGrantOverageCurve(ctx, in)
	require.NoError(t, err)
	require.True(t, ok)

	li := previewLineItem{SubLineItemID: "sli_drift", Currency: "usd", Price: in.Price, Meter: in.Meter, EngineAmount: in.EngineAmount,
		PeriodStart: periodStart, PeriodEnd: periodStart.AddDate(0, 0, 9)}
	rows := decomposeUsageMarginal(li, curve)
	require.Len(t, rows, 10)

	total := decimal.Zero
	for _, r := range rows {
		total = total.Add(r.NetAmount)
		residual, identityOK := reconcileRow(r)
		assert.True(t, identityOK, "identity must hold under scaling, residual %s on %s", residual, r.Day)
	}
	assert.True(t, total.Equal(in.EngineAmount), "rows must sum exactly to the engine charge, got %s", total)
}

// TestGrantOverageCurve_NoShapeFallsBack: an engine charge with no visible
// window usage cannot be attributed to days — the caller must fall back.
func TestGrantOverageCurve_NoShapeFallsBack(t *testing.T) {
	ctx := context.Background()
	svc, in := grantCurveFixture(t, ctx, 100, 10, nil) // no crossed windows
	in.EngineAmount = decimal.NewFromInt(5)

	_, ok, err := svc.buildGrantOverageCurve(ctx, in)
	require.NoError(t, err)
	assert.False(t, ok, "no window shape must force the period_only fallback")
}

// TestGrantsBillable_GuardMirrorsEngine: lines the engine would refuse to
// grant-bill (tiered price here) must take the normal curve, not the grant one.
func TestGrantsBillable_GuardMirrorsEngine(t *testing.T) {
	m := &meter.Meter{ID: "m", Aggregation: meter.Aggregation{Type: types.AggregationSum}}
	flat := &price.Price{ID: "p", Amount: decimal.NewFromInt(1), BillingModel: types.BILLING_MODEL_FLAT_FEE}
	tiered := &price.Price{ID: "p2", BillingModel: types.BILLING_MODEL_TIERED, TierMode: types.BILLING_TIER_SLAB,
		Tiers: price.JSONBTiers{{UpTo: lo.ToPtr(uint64(10)), UnitAmount: decimal.NewFromInt(1)}}}
	grants := []*entitlementgrant.EntitlementGrant{grantCurveGrant("eg", nil, time.Time{}, time.Time{}, "ec")}

	assert.True(t, grantsBillable(nil, flat, m, grants))
	assert.False(t, grantsBillable(nil, tiered, m, grants), "tiered prices are rejected by the engine's own guard")
	assert.False(t, grantsBillable(nil, flat, m, nil))
}
