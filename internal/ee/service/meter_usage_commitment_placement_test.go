package service

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

// placementFixture bills one hourly window stack: cmt_w reserves 2 units in each of six hours from midnight, valued at
// committed, and li_w, at 1 a unit through 3 and 2 above, uses 3 and 4 in the first two. They run 1 and 2 over, at
// margins of 1 and 3, and hours 2-5 leave 2 unused each.
func placementFixture(
	t *testing.T,
	measurement types.CommitmentMeasurement,
	committed *price.Price,
	overagePrice *price.Price,
) (*commitmentLoader, *commitmentBilling, time.Time) {
	t.Helper()
	midnight := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	windows := make([]time.Time, 6)
	for i := range windows {
		windows[i] = midnight.Add(time.Duration(i) * time.Hour)
	}

	ctx := context.Background()
	prices := NewPriceService(ServiceParams{Logger: logger.NewNoopLogger()})
	amountOf := func(qty decimal.Decimal) decimal.Decimal { return prices.CalculateCost(ctx, committed, qty) }

	line := quantityLine(3, 4, 0, 0, 0, 0)
	line.Item = &subscription.SubscriptionLineItem{ID: "li_w"}
	line.Price = &price.Price{
		BillingModel: types.BILLING_MODEL_TIERED,
		TierMode:     types.BILLING_TIER_SLAB,
		Tiers:        []price.PriceTier{{UpTo: lo.ToPtr(uint64(3)), UnitAmount: intDec(1)}, {UnitAmount: intDec(2)}},
	}
	line.OveragePrice, line.PerWindow = overagePrice, true
	sc := stacked("cmt_w", 0, []int{0, 1, 2, 3, 4, 5}, []*usageLine{line}, 2, 2, 2, 2, 2, 2)
	sc.Binding, sc.CommittedPrice = true, committed
	sc.Period = commitmentPeriod{Amount: amountOf(intDec(12)), Capacity: intDec(12)}
	stack := &commitmentStack{
		Type:        types.COMMITMENT_TYPE_QUANTITY,
		Measurement: measurement,
		Windows:     windows,
		Commitments: []*stackedCommitment{sc},
		Lines:       []*usageLine{line},
	}
	alloc, err := allocate(stack)
	require.NoError(t, err)

	got := alloc.Commitments[0]
	require.Equal(t, []windowTake{{Window: 0, Line: 0, Usage: intDec(2)}, {Window: 1, Line: 0, Usage: intDec(2)}}, got.Takes)
	requireDecimals(t, []int64{0, 0, 2, 2, 2, 2}, got.PoolsLeft)

	cost := func(p *price.Price, qty decimal.Decimal) decimal.Decimal { return prices.CalculateCost(ctx, p, qty) }
	lines := commitmentInvoiceLines([]*commitmentStack{stack}, []*allocation{alloc}, "usd", midnight, midnight.Add(6*time.Hour), cost)
	loader := &commitmentLoader{
		sub:               &subscription.Subscription{Currency: "usd"},
		priceSvc:          prices,
		scopeByCommitment: map[string]*loadedScope{"cmt_w": {bucket: types.WindowSizeHour, amountOf: amountOf}},
	}
	return loader, &commitmentBilling{stacks: []*commitmentStack{stack}, allocs: []*allocation{alloc}, lines: lines}, midnight
}

func TestWindowPlacer(t *testing.T) {
	type placedWindow struct {
		hour                      int
		utilized, overage, trueUp string
		overageQty, trueUpQty     string
	}

	flat := &price.Price{BillingModel: types.BILLING_MODEL_FLAT_FEE, Amount: intDec(1)}
	tests := []struct {
		name         string
		measurement  types.CommitmentMeasurement
		window       types.WindowSize
		periodHours  int
		committed    *price.Price
		overagePrice *price.Price
		want         []placedWindow
		beyond       string // the true-up of the hours past the range
	}{
		{
			// Overage lands at each hour's margin, 1 and 3, not its 4 shared by 1 and 2 units.
			name:        "each hour carries its own charges",
			measurement: types.CommitmentMeasurementWindow,
			window:      types.WindowSizeHour,
			periodHours: 6,
			committed:   flat,
			want: []placedWindow{
				{hour: 0, utilized: "2", overage: "1", overageQty: "1"},
				{hour: 1, utilized: "2", overage: "3", overageQty: "2"},
				{hour: 2, trueUp: "2", trueUpQty: "2"},
				{hour: 3, trueUp: "2", trueUpQty: "2"},
				{hour: 4, trueUp: "2", trueUpQty: "2"},
				{hour: 5, trueUp: "2", trueUpQty: "2"},
			},
		},
		{
			// An overage price bills the 3 units once, 15, so its overage follows them.
			name:         "an overage price follows the units",
			measurement:  types.CommitmentMeasurementWindow,
			window:       types.WindowSizeHour,
			periodHours:  6,
			committed:    flat,
			overagePrice: &price.Price{BillingModel: types.BILLING_MODEL_FLAT_FEE, Amount: intDec(5)},
			want: []placedWindow{
				{hour: 0, utilized: "2", overage: "5", overageQty: "1"},
				{hour: 1, utilized: "2", overage: "10", overageQty: "2"},
				{hour: 2, trueUp: "2", trueUpQty: "2"},
				{hour: 3, trueUp: "2", trueUpQty: "2"},
				{hour: 4, trueUp: "2", trueUpQty: "2"},
				{hour: 5, trueUp: "2", trueUpQty: "2"},
			},
		},
		{
			// A projected open period's later hours are not owed in the range.
			name:        "hours past the range are left out",
			measurement: types.CommitmentMeasurementWindow,
			window:      types.WindowSizeHour,
			periodHours: 3,
			committed:   flat,
			want: []placedWindow{
				{hour: 0, utilized: "2", overage: "1", overageQty: "1"},
				{hour: 1, utilized: "2", overage: "3", overageQty: "2"},
				{hour: 2, trueUp: "2", trueUpQty: "2"},
			},
			beyond: "6",
		},
		{
			// At 3 a unit through 6 and 1 above, the hours past the range add 24-18 = 6, leaving the range what its 6
			// units are worth less what its 4 used ones bill: 18-12 = 6.
			name:        "a tiered committed price prices the range's capacity first",
			measurement: types.CommitmentMeasurementWindow,
			window:      types.WindowSizeHour,
			periodHours: 3,
			committed: &price.Price{
				BillingModel: types.BILLING_MODEL_TIERED,
				TierMode:     types.BILLING_TIER_SLAB,
				Tiers:        []price.PriceTier{{UpTo: lo.ToPtr(uint64(6)), UnitAmount: intDec(3)}, {UnitAmount: intDec(1)}},
			},
			want: []placedWindow{
				{hour: 0, utilized: "6", overage: "1", overageQty: "1"},
				{hour: 1, utilized: "6", overage: "3", overageQty: "2"},
				{hour: 2, trueUp: "6", trueUpQty: "2"},
			},
			beyond: "6",
		},
		{
			name:        "a coarser request window adds its hours up",
			measurement: types.CommitmentMeasurementWindow,
			window:      types.WindowSizeDay,
			periodHours: 6,
			committed:   flat,
			want:        []placedWindow{{hour: 0, utilized: "4", overage: "4", overageQty: "3", trueUp: "8", trueUpQty: "8"}},
		},
		{
			name:        "a period commitment pools its capacity, so it is not placed",
			measurement: types.CommitmentMeasurementBillingPeriod,
			window:      types.WindowSizeHour,
			periodHours: 6,
			committed:   flat,
		},
		{
			name:        "no request window places nothing",
			measurement: types.CommitmentMeasurementWindow,
			periodHours: 6,
			committed:   flat,
		},
		{
			name:        "no request window still leaves out the hours past the range",
			measurement: types.CommitmentMeasurementWindow,
			periodHours: 3,
			committed:   flat,
			beyond:      "6",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			loader, billing, midnight := placementFixture(t, tt.measurement, tt.committed, tt.overagePrice)
			period := types.Period{Start: midnight, End: midnight.Add(time.Duration(tt.periodHours) * time.Hour)}
			p := newWindowPlacer(context.Background(), loader, billing, tt.window, nil, period)
			for _, row := range billing.lines {
				switch kind := types.CommitmentLineKind(row.Metadata[types.MetadataKeyCommitmentLineKind]); kind {
				case types.CommitmentLineUtilized:
					p.utilized("cmt_w", "li_w", row.Amount, row.Quantity)
				case types.CommitmentLineTrueUp:
					p.trueUp("cmt_w", "li_w", row.Amount, row.Quantity)
				default:
					p.overage("li_w", kind, row.Amount, row.Quantity)
				}
			}

			// The hours past the range keep their 2 unused units each, whatever their true-up is worth.
			beyond := p.beyond("li_w")
			wantBeyond := decimal.Zero
			if tt.beyond != "" {
				wantBeyond = decimal.RequireFromString(tt.beyond)
			}
			require.True(t, beyond.ComputedTrueUpAmount.Equal(wantBeyond), "beyond %s", beyond.ComputedTrueUpAmount)
			require.True(t, beyond.ComputedTrueUpQuantity.Equal(intDec(int64(2*(6-tt.periodHours)))), "beyond quantity %s", beyond.ComputedTrueUpQuantity)
			require.True(t, beyond.ComputedCommitmentUtilizedAmount.IsZero() && beyond.ComputedOverageAmount.IsZero())

			got := p.windows("li_w")
			if tt.want == nil {
				require.Nil(t, got)
				return
			}

			require.Len(t, got, len(tt.want))
			for i, want := range tt.want {
				bw := got[i]
				require.True(t, bw.start.Equal(midnight.Add(time.Duration(want.hour)*time.Hour)), "window %d starts %s", i, bw.start)
				for _, c := range []struct {
					name string
					want string
					got  decimal.Decimal
				}{
					{"utilized", want.utilized, bw.billed.ComputedCommitmentUtilizedAmount},
					{"overage", want.overage, bw.billed.ComputedOverageAmount},
					{"true-up", want.trueUp, bw.billed.ComputedTrueUpAmount},
					{"overage quantity", want.overageQty, bw.billed.ComputedOverageQuantity},
					{"true-up quantity", want.trueUpQty, bw.billed.ComputedTrueUpQuantity},
				} {
					wantAmount := decimal.Zero
					if c.want != "" {
						wantAmount = decimal.RequireFromString(c.want)
					}
					require.True(t, c.got.Equal(wantAmount), "window %d %s %s, want %s", i, c.name, c.got, wantAmount)
				}
			}
		})
	}
}

// A charge with no window to carry it, such as plain usage where every window is covered, leaves the line to be shared
// by usage instead of losing the charge.
func TestWindowPlacerLeavesALineWithAnUnplaceableCharge(t *testing.T) {
	flat := &price.Price{BillingModel: types.BILLING_MODEL_FLAT_FEE, Amount: intDec(1)}
	loader, billing, midnight := placementFixture(t, types.CommitmentMeasurementWindow, flat, nil)
	p := newWindowPlacer(context.Background(), loader, billing, types.WindowSizeHour, nil, types.Period{Start: midnight, End: midnight.Add(6 * time.Hour)})
	p.utilized("cmt_w", "li_w", intDec(2), intDec(2))
	p.overage("li_w", types.CommitmentLinePlain, intDec(5), intDec(1))
	require.Nil(t, p.windows("li_w"))
}
