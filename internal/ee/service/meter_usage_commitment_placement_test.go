package service

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
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

// billStack allocates and renders stack as bill does, priced by cost.
func billStack(t *testing.T, stack *commitmentStack, cost lineCost) *commitmentBilling {
	t.Helper()
	alloc, err := allocate(stack)
	require.NoError(t, err)

	start := stack.Windows[0]
	cost = withCommittedPrices(cost)
	lines := commitmentInvoiceLines([]*commitmentStack{stack}, []*allocation{alloc}, "usd", start, start.AddDate(0, 1, 0), cost)
	return &commitmentBilling{stacks: []*commitmentStack{stack}, allocs: []*allocation{alloc}, lines: lines, cost: cost}
}

// placeRows lays rendered rows on p as periodCommitments does for a commitment scoping only lineID.
func placeRows(p *windowPlacer, rows []dto.CreateInvoiceLineItemRequest, lineID string) {
	for _, row := range rows {
		switch kind := types.CommitmentLineKind(row.Metadata[types.MetadataKeyCommitmentLineKind]); kind {
		case types.CommitmentLineUtilized:
			p.utilized(lo.FromPtr(row.CommitmentID), lineID, row.Amount, row.Quantity)
		case types.CommitmentLineTrueUp:
			p.trueUp(lo.FromPtr(row.CommitmentID), lineID, row.Amount, row.Quantity)
		default:
			p.overage(lineID, kind, row.Amount, row.Quantity)
		}
	}
}

// A line priced per window bills each window's overage at its own margin, so that is where its overage lands.
func TestWindowPlacerPlacesOverageAtEachWindowsMargin(t *testing.T) {
	// slabCost charges 1 per unit up to 120 and 2 per unit above.
	slabCost := func(_ *price.Price, qty decimal.Decimal) decimal.Decimal {
		first := decimal.Min(qty, intDec(120))
		return first.Add(qty.Sub(first).Mul(intDec(2)))
	}
	line := invoiceLine("a", "280", "0", "130", "150")
	line.PerWindow = true
	stack := &commitmentStack{
		Type:        types.COMMITMENT_TYPE_QUANTITY,
		Measurement: types.CommitmentMeasurementWindow,
		Windows:     engineWindows(2),
		Lines:       []*usageLine{line},
		Commitments: []*stackedCommitment{committed("c1", true, "200", []int{0, 1}, []*usageLine{line}, "100", "100")},
	}
	billing := billStack(t, stack, slabCost)
	loader := &commitmentLoader{
		sub:               &subscription.Subscription{Currency: "usd"},
		scopeByCommitment: map[string]*loadedScope{"c1": {bucket: types.WindowSizeDay}},
	}
	start := stack.Windows[0]

	p := newWindowPlacer(loader, billing, types.WindowSizeDay, nil, types.Period{Start: start, End: start.AddDate(0, 1, 0)})
	placeRows(p, billing.lines, "li_a")

	// Each window covers 100: cost(130)-cost(100) = 40 and cost(150)-cost(100) = 80, not the 120 shared by 30 and 50 units.
	got := p.windows("li_a")
	require.Len(t, got, 2)
	for i, want := range []struct{ amount, quantity string }{{"40", "30"}, {"80", "50"}} {
		require.True(t, got[i].billed.ComputedOverageAmount.Equal(dec(want.amount)), "window %d overage %s", i, got[i].billed.ComputedOverageAmount)
		require.True(t, got[i].billed.ComputedOverageQuantity.Equal(dec(want.quantity)), "window %d quantity %s", i, got[i].billed.ComputedOverageQuantity)
	}
}

// A projected period's buckets past the range owe what their capacity is worth priced on from the range's, so the range
// keeps the true-up it would bill as a closed range.
func TestWindowPlacerValuesTheTrueUpPastTheRangeByTier(t *testing.T) {
	// A slab committed price: 2 per unit up to 10 and 1 above, so a month of one unit a day is worth 41.
	slab := &price.Price{
		ID:           "committed_price_c1",
		BillingModel: types.BILLING_MODEL_TIERED,
		TierMode:     types.BILLING_TIER_SLAB,
		Tiers:        []price.PriceTier{{UpTo: lo.ToPtr(uint64(10)), UnitAmount: intDec(2)}, {UnitAmount: intDec(1)}},
	}
	flat := &price.Price{ID: "committed_price_c1", BillingModel: types.BILLING_MODEL_FLAT_FEE, Amount: intDec(1)}

	tests := []struct {
		name       string
		committed  *price.Price
		dayZero    string   // usage on the first day; one unit is covered
		wantRange  []string // true-up of each of the range's five days
		wantBeyond string
	}{
		{
			// A closed range of five days is worth 2*5 = 10.
			name:       "a slab price values the range at its first tier",
			committed:  slab,
			dayZero:    "0",
			wantRange:  []string{"2", "2", "2", "2", "2"},
			wantBeyond: "31",
		},
		{
			// The range is worth 10, less the 2 its one used unit bills.
			name:       "usage in the range lowers only the range's true-up",
			committed:  slab,
			dayZero:    "3",
			wantRange:  []string{"0", "2", "2", "2", "2"},
			wantBeyond: "31",
		},
		{
			name:       "a flat price values every day alike",
			committed:  flat,
			dayZero:    "0",
			wantRange:  []string{"1", "1", "1", "1", "1"},
			wantBeyond: "26",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			windows := engineWindows(31)
			poolOf := make([]int, len(windows))
			capacities := make([]string, len(windows))
			usage := make([]string, len(windows))
			for i := range windows {
				poolOf[i], capacities[i], usage[i] = i, "1", "0"
			}
			usage[0] = tt.dayZero

			line := invoiceLine("a", tt.dayZero, tt.dayZero, usage...)
			line.PerWindow = true
			cost := withCommittedPrices(unitCost)
			amountOf := func(qty decimal.Decimal) decimal.Decimal { return cost(tt.committed, qty) }

			sc := committed("c1", true, amountOf(intDec(31)).String(), poolOf, []*usageLine{line}, capacities...)
			sc.CommittedPrice, sc.Currency = tt.committed, "usd"
			stack := &commitmentStack{
				Type:        types.COMMITMENT_TYPE_QUANTITY,
				Measurement: types.CommitmentMeasurementWindow,
				Windows:     windows,
				Lines:       []*usageLine{line},
				Commitments: []*stackedCommitment{sc},
			}
			billing := billStack(t, stack, unitCost)
			start := windows[0]
			loader := &commitmentLoader{
				sub:         &subscription.Subscription{Currency: "usd", BillingAnchor: start},
				periodStart: start,
				scopeByCommitment: map[string]*loadedScope{
					"c1": {bucket: types.WindowSizeDay, amountOf: amountOf, accrualStart: start},
				},
			}

			// The range covers five days of the projected month.
			p := newWindowPlacer(loader, billing, types.WindowSizeDay, nil, types.Period{Start: start, End: start.AddDate(0, 0, 5)})
			placeRows(p, billing.lines, "li_a")

			trueUps := make([]decimal.Decimal, 5)
			for _, bw := range p.windows("li_a") {
				trueUps[int(bw.start.Sub(start).Hours()/24)] = bw.billed.ComputedTrueUpAmount
			}
			for i, want := range tt.wantRange {
				require.True(t, trueUps[i].Equal(dec(want)), "day %d true-up %s, want %s", i, trueUps[i], want)
			}

			// The quantity past the range stays its unused capacity: 26 days of one unit.
			beyond := p.beyond("li_a")
			require.True(t, beyond.ComputedTrueUpAmount.Equal(dec(tt.wantBeyond)), "beyond %s", beyond.ComputedTrueUpAmount)
			require.True(t, beyond.ComputedTrueUpQuantity.Equal(intDec(26)), "beyond quantity %s", beyond.ComputedTrueUpQuantity)
		})
	}
}
