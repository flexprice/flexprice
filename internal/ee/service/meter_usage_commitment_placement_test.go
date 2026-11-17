package service

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

// placementFixture allocates one hourly window stack: cmt_w reserves 2 units in each of six hours from midnight and
// li_w uses 3 in the first, so hour 0 runs 1 over and hours 1-5 leave 2 unused each.
func placementFixture(t *testing.T, measurement types.CommitmentMeasurement) (*commitmentLoader, *commitmentBilling, time.Time) {
	t.Helper()
	midnight := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	windows := make([]time.Time, 6)
	for i := range windows {
		windows[i] = midnight.Add(time.Duration(i) * time.Hour)
	}

	line := quantityLine(3, 0, 0, 0, 0, 0)
	line.Item = &subscription.SubscriptionLineItem{ID: "li_w"}
	sc := stacked("cmt_w", 0, []int{0, 1, 2, 3, 4, 5}, []*usageLine{line}, 2, 2, 2, 2, 2, 2)
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
	require.Equal(t, []windowTake{{Window: 0, Line: 0, Usage: intDec(2)}}, got.Takes)
	requireDecimals(t, []int64{0, 2, 2, 2, 2, 2}, got.PoolsLeft)

	loader := &commitmentLoader{
		sub:               &subscription.Subscription{Currency: "usd"},
		scopeByCommitment: map[string]*loadedScope{"cmt_w": {bucket: types.WindowSizeHour}},
	}
	return loader, &commitmentBilling{stacks: []*commitmentStack{stack}, allocs: []*allocation{alloc}}, midnight
}

func TestWindowPlacer(t *testing.T) {
	type placedWindow struct {
		hour                      int
		utilized, overage, trueUp string
	}

	tests := []struct {
		name        string
		measurement types.CommitmentMeasurement
		window      types.WindowSize
		periodHours int
		trueUp      string // what the floor leaves for the true-up row
		want        []placedWindow
		beyond      string // the true-up of the hours past the range
	}{
		{
			name:        "each hour carries its own charges",
			measurement: types.CommitmentMeasurementWindow,
			window:      types.WindowSizeHour,
			periodHours: 6,
			trueUp:      "10",
			want: []placedWindow{
				{hour: 0, utilized: "2", overage: "2"},
				{hour: 1, trueUp: "2"},
				{hour: 2, trueUp: "2"},
				{hour: 3, trueUp: "2"},
				{hour: 4, trueUp: "2"},
				{hour: 5, trueUp: "2"},
			},
		},
		{
			// A projected open period's later hours are not owed in the range.
			name:        "hours past the range are left out",
			measurement: types.CommitmentMeasurementWindow,
			window:      types.WindowSizeHour,
			periodHours: 3,
			trueUp:      "10",
			want: []placedWindow{
				{hour: 0, utilized: "2", overage: "2"},
				{hour: 1, trueUp: "2"},
				{hour: 2, trueUp: "2"},
			},
			beyond: "6",
		},
		{
			name:        "a coarser request window adds its hours up",
			measurement: types.CommitmentMeasurementWindow,
			window:      types.WindowSizeDay,
			periodHours: 6,
			trueUp:      "10",
			want:        []placedWindow{{hour: 0, utilized: "2", overage: "2", trueUp: "10"}},
		},
		{
			name:        "a period commitment pools its capacity, so it is not placed",
			measurement: types.CommitmentMeasurementBillingPeriod,
			window:      types.WindowSizeHour,
			periodHours: 6,
			trueUp:      "10",
		},
		{
			name:        "no request window places nothing",
			measurement: types.CommitmentMeasurementWindow,
			periodHours: 6,
			trueUp:      "10",
		},
		{
			name:        "no request window still leaves out the hours past the range",
			measurement: types.CommitmentMeasurementWindow,
			periodHours: 3,
			trueUp:      "10",
			beyond:      "6",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			loader, billing, midnight := placementFixture(t, tt.measurement)
			period := types.Period{Start: midnight, End: midnight.Add(time.Duration(tt.periodHours) * time.Hour)}
			p := newWindowPlacer(loader, billing, tt.window, nil, period)
			p.utilized("cmt_w", "li_w", intDec(2), intDec(2))
			p.overage("li_w", types.CommitmentLineOverage, intDec(2), intDec(1))
			p.trueUp("cmt_w", "li_w", decimal.RequireFromString(tt.trueUp), decimal.RequireFromString(tt.trueUp))

			beyond := p.beyond("li_w")
			wantBeyond := decimal.Zero
			if tt.beyond != "" {
				wantBeyond = decimal.RequireFromString(tt.beyond)
			}
			require.True(t, beyond.ComputedTrueUpAmount.Equal(wantBeyond), "beyond %s", beyond.ComputedTrueUpAmount)
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
	loader, billing, midnight := placementFixture(t, types.CommitmentMeasurementWindow)
	p := newWindowPlacer(loader, billing, types.WindowSizeHour, nil, types.Period{Start: midnight, End: midnight.Add(6 * time.Hour)})
	p.utilized("cmt_w", "li_w", intDec(2), intDec(2))
	p.overage("li_w", types.CommitmentLinePlain, intDec(5), intDec(1))
	require.Nil(t, p.windows("li_w"))
}
