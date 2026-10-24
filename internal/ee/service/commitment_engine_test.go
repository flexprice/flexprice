package service

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/commitment"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func intDec(v int64) decimal.Decimal { return decimal.NewFromInt(v) }

func intDecs(vs ...int64) []decimal.Decimal {
	out := make([]decimal.Decimal, len(vs))
	for i, v := range vs {
		out[i] = intDec(v)
	}

	return out
}

func engineWindows(n int) []time.Time {
	start := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	out := make([]time.Time, n)
	for i := range out {
		out[i] = start.AddDate(0, 0, i)
	}

	return out
}

func quantityLine(usage ...int64) *usageLine {
	total := int64(0)
	for _, u := range usage {
		total += u
	}

	return &usageLine{Quantity: intDec(total), Usage: intDecs(usage...)}
}

// stacked builds a commitment with the given pool capacities and window-to-pool mapping.
func stacked(id string, priority int, poolOf []int, lines []*usageLine, capacities ...int64) *stackedCommitment {
	pools := make([]capacityPool, len(capacities))
	for i, c := range capacities {
		pools[i] = capacityPool{Capacity: intDec(c)}
	}

	return &stackedCommitment{
		C:      &commitment.Commitment{ID: id, Priority: priority},
		Lines:  lines,
		Pools:  pools,
		PoolOf: poolOf,
	}
}

func requireDecimals(t *testing.T, want []int64, got []decimal.Decimal) {
	t.Helper()
	require.Len(t, got, len(want))
	for i := range want {
		require.True(t, intDec(want[i]).Equal(got[i]), "index %d: want %d, got %s", i, want[i], got[i])
	}
}

func TestAllocate(t *testing.T) {
	tests := []struct {
		name         string
		build        func() *commitmentStack
		wantUsed     []int64
		wantUnused   []int64
		wantCapacity []int64
		wantOnDemand [][]int64
	}{
		{
			name: "E17 usage below first commitment",
			build: func() *commitmentStack {
				return e17Stack(6)
			},
			wantUsed:     []int64{6, 0},
			wantUnused:   []int64{4, 10},
			wantCapacity: []int64{10, 10},
			wantOnDemand: [][]int64{{0}},
		},
		{
			name: "E17 usage spills into second commitment",
			build: func() *commitmentStack {
				return e17Stack(15)
			},
			wantUsed:     []int64{10, 5},
			wantUnused:   []int64{0, 5},
			wantCapacity: []int64{10, 10},
			wantOnDemand: [][]int64{{0}},
		},
		{
			name: "E17 usage exceeds the whole stack",
			build: func() *commitmentStack {
				return e17Stack(24)
			},
			wantUsed:     []int64{10, 10},
			wantUnused:   []int64{0, 0},
			wantCapacity: []int64{10, 10},
			wantOnDemand: [][]int64{{4}},
		},
		{
			name: "E18 second commitment outside its window sends overflow on demand",
			build: func() *commitmentStack {
				line := quantityLine(15, 15)
				return &commitmentStack{
					Type:        types.COMMITMENT_TYPE_QUANTITY,
					Measurement: types.CommitmentMeasurementWindow,
					Windows:     engineWindows(2),
					Lines:       []*usageLine{line},
					Commitments: []*stackedCommitment{
						stacked("c1", 1, []int{0, 1}, []*usageLine{line}, 10, 10),
						stacked("c2", 2, []int{-1, 0}, []*usageLine{line}, 10),
					},
				}
			},
			wantUsed:     []int64{20, 5},
			wantUnused:   []int64{0, 5},
			wantCapacity: []int64{20, 10},
			wantOnDemand: [][]int64{{5, 0}},
		},
		{
			name: "E3 quantity per window with on demand",
			build: func() *commitmentStack {
				line := quantityLine(80, 150, 100)
				return &commitmentStack{
					Type:        types.COMMITMENT_TYPE_QUANTITY,
					Measurement: types.CommitmentMeasurementWindow,
					Windows:     engineWindows(3),
					Lines:       []*usageLine{line},
					Commitments: []*stackedCommitment{
						stacked("c1", 1, []int{0, 1, 2}, []*usageLine{line}, 100, 100, 100),
					},
				}
			},
			wantUsed:     []int64{280},
			wantUnused:   []int64{20},
			wantCapacity: []int64{300},
			wantOnDemand: [][]int64{{0, 50, 0}},
		},
		{
			name: "billing period pool drains across stretches and gives nothing before its start",
			build: func() *commitmentStack {
				line := quantityLine(30, 60, 60, 20, 30)
				return &commitmentStack{
					Type:        types.COMMITMENT_TYPE_QUANTITY,
					Measurement: types.CommitmentMeasurementBillingPeriod,
					Windows:     engineWindows(5),
					Lines:       []*usageLine{line},
					Commitments: []*stackedCommitment{
						stacked("c1", 1, []int{-1, 0, 0, -1, 0}, []*usageLine{line}, 100),
					},
				}
			},
			wantUsed:     []int64{100},
			wantUnused:   []int64{0},
			wantCapacity: []int64{100},
			wantOnDemand: [][]int64{{30, 0, 20, 20, 30}},
		},
		{
			name: "E8 meter scope lines share one capacity in fill order",
			build: func() *commitmentStack {
				a, b := quantityLine(70), quantityLine(50)
				return &commitmentStack{
					Type:        types.COMMITMENT_TYPE_QUANTITY,
					Measurement: types.CommitmentMeasurementBillingPeriod,
					Windows:     engineWindows(1),
					Lines:       []*usageLine{a, b},
					Commitments: []*stackedCommitment{
						stacked("c1", 1, []int{0}, []*usageLine{a, b}, 100),
					},
				}
			},
			wantUsed:     []int64{100},
			wantUnused:   []int64{0},
			wantCapacity: []int64{100},
			wantOnDemand: [][]int64{{0}, {20}},
		},
		{
			name: "E8 fill order follows the commitment's line order not the stack's",
			build: func() *commitmentStack {
				a, b := quantityLine(70), quantityLine(50)
				return &commitmentStack{
					Type:        types.COMMITMENT_TYPE_QUANTITY,
					Measurement: types.CommitmentMeasurementBillingPeriod,
					Windows:     engineWindows(1),
					Lines:       []*usageLine{a, b},
					Commitments: []*stackedCommitment{
						stacked("c1", 1, []int{0}, []*usageLine{b, a}, 100),
					},
				}
			},
			wantUsed:     []int64{100},
			wantUnused:   []int64{0},
			wantCapacity: []int64{100},
			wantOnDemand: [][]int64{{20}, {0}},
		},
		{
			name: "amount stack allocates cost",
			build: func() *commitmentStack {
				line := &usageLine{Cost: intDec(130), Usage: intDecs(50, 80)}
				return &commitmentStack{
					Type:        types.COMMITMENT_TYPE_AMOUNT,
					Measurement: types.CommitmentMeasurementWindow,
					Windows:     engineWindows(2),
					Lines:       []*usageLine{line},
					Commitments: []*stackedCommitment{
						stacked("c1", 1, []int{0, 1}, []*usageLine{line}, 60, 60),
					},
				}
			},
			wantUsed:     []int64{110},
			wantUnused:   []int64{10},
			wantCapacity: []int64{120},
			wantOnDemand: [][]int64{{0, 20}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := allocate(tt.build())
			require.NoError(t, err)
			require.Len(t, got.Commitments, len(tt.wantUsed))
			for i := range tt.wantUsed {
				require.True(t, intDec(tt.wantUsed[i]).Equal(got.Commitments[i].Used), "commitment %d used: %s", i, got.Commitments[i].Used)
				require.True(t, intDec(tt.wantUnused[i]).Equal(got.Commitments[i].Unused), "commitment %d unused: %s", i, got.Commitments[i].Unused)
				require.True(t, intDec(tt.wantCapacity[i]).Equal(got.Commitments[i].Capacity), "commitment %d capacity: %s", i, got.Commitments[i].Capacity)
			}

			require.Len(t, got.OnDemand, len(tt.wantOnDemand))
			for i := range tt.wantOnDemand {
				requireDecimals(t, tt.wantOnDemand[i], got.OnDemand[i])
			}
		})
	}
}

func e17Stack(usage int64) *commitmentStack {
	line := quantityLine(usage)
	return &commitmentStack{
		Type:        types.COMMITMENT_TYPE_QUANTITY,
		Measurement: types.CommitmentMeasurementBillingPeriod,
		Windows:     engineWindows(1),
		Lines:       []*usageLine{line},
		Commitments: []*stackedCommitment{
			stacked("c1", 1, []int{0}, []*usageLine{line}, 10),
			stacked("c2", 2, []int{0}, []*usageLine{line}, 10),
		},
	}
}

func TestAllocateUsedByLine(t *testing.T) {
	a, b := quantityLine(70, 0), quantityLine(10, 50)
	stack := &commitmentStack{
		Type:        types.COMMITMENT_TYPE_QUANTITY,
		Measurement: types.CommitmentMeasurementWindow,
		Windows:     engineWindows(2),
		Lines:       []*usageLine{a, b},
		Commitments: []*stackedCommitment{
			stacked("c1", 1, []int{0, 1}, []*usageLine{b, a}, 40, 40),
		},
	}

	got, err := allocate(stack)
	require.NoError(t, err)

	// UsedByLine follows the commitment's own Lines order: b then a.
	requireDecimals(t, []int64{50, 30}, got.Commitments[0].UsedByLine)
	require.True(t, intDec(80).Equal(got.Commitments[0].Used))
}

func TestAllocateRejectsBrokenInput(t *testing.T) {
	tests := []struct {
		name  string
		build func() *commitmentStack
	}{
		{
			name: "quantity not conserved",
			build: func() *commitmentStack {
				line := &usageLine{Quantity: intDec(100), Usage: intDecs(40, 50)}
				return &commitmentStack{
					Type:        types.COMMITMENT_TYPE_QUANTITY,
					Windows:     engineWindows(2),
					Lines:       []*usageLine{line},
					Commitments: []*stackedCommitment{stacked("c1", 1, []int{0, 0}, []*usageLine{line}, 100)},
				}
			},
		},
		{
			name: "cost not conserved on amount stack",
			build: func() *commitmentStack {
				line := &usageLine{Quantity: intDec(90), Cost: intDec(100), Usage: intDecs(40, 50)}
				return &commitmentStack{
					Type:        types.COMMITMENT_TYPE_AMOUNT,
					Windows:     engineWindows(2),
					Lines:       []*usageLine{line},
					Commitments: []*stackedCommitment{stacked("c1", 1, []int{0, 0}, []*usageLine{line}, 100)},
				}
			},
		},
		{
			name: "usage length differs from windows",
			build: func() *commitmentStack {
				line := quantityLine(40)
				return &commitmentStack{
					Type:        types.COMMITMENT_TYPE_QUANTITY,
					Windows:     engineWindows(2),
					Lines:       []*usageLine{line},
					Commitments: []*stackedCommitment{stacked("c1", 1, []int{0, 0}, []*usageLine{line}, 100)},
				}
			},
		},
		{
			name: "pool index out of range",
			build: func() *commitmentStack {
				line := quantityLine(40)
				return &commitmentStack{
					Type:        types.COMMITMENT_TYPE_QUANTITY,
					Windows:     engineWindows(1),
					Lines:       []*usageLine{line},
					Commitments: []*stackedCommitment{stacked("c1", 1, []int{3}, []*usageLine{line}, 100)},
				}
			},
		},
		{
			name: "commitment line missing from the stack",
			build: func() *commitmentStack {
				line := quantityLine(40)
				return &commitmentStack{
					Type:        types.COMMITMENT_TYPE_QUANTITY,
					Windows:     engineWindows(1),
					Lines:       []*usageLine{line},
					Commitments: []*stackedCommitment{stacked("c1", 1, []int{0}, []*usageLine{quantityLine(40)}, 100)},
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := allocate(tt.build())
			require.Error(t, err)
			require.Nil(t, got)
			require.True(t, ierr.IsInternal(err), "want internal error, got %v", err)
		})
	}
}

func TestSortCommitmentStack(t *testing.T) {
	day := func(d int) time.Time { return time.Date(2027, 1, d, 0, 0, 0, 0, time.UTC) }
	mk := func(id string, priority int, start time.Time) *stackedCommitment {
		return &stackedCommitment{C: &commitment.Commitment{ID: id, Priority: priority, StartDate: start}}
	}

	tests := []struct {
		name  string
		input []*stackedCommitment
		want  []string
	}{
		{
			name:  "priority ascending",
			input: []*stackedCommitment{mk("c1", 3, day(1)), mk("c2", 1, day(5)), mk("c3", 2, day(2))},
			want:  []string{"c2", "c3", "c1"},
		},
		{
			name:  "earlier start breaks priority ties",
			input: []*stackedCommitment{mk("c1", 1, day(9)), mk("c2", 1, day(3))},
			want:  []string{"c2", "c1"},
		},
		{
			name:  "lower id breaks full ties",
			input: []*stackedCommitment{mk("c9", 1, day(3)), mk("c2", 1, day(3)), mk("c5", 1, day(3))},
			want:  []string{"c2", "c5", "c9"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sortCommitmentStack(tt.input)
			got := make([]string, len(tt.input))
			for i, sc := range tt.input {
				got[i] = sc.C.ID
			}

			require.Equal(t, tt.want, got)
		})
	}
}

// invoiceLine builds a usage line backed by a line item, so rendered lines carry its fields.
func invoiceLine(id string, quantity, cost string, usage ...string) *usageLine {
	windows := make([]decimal.Decimal, len(usage))
	for i, u := range usage {
		windows[i] = dec(u)
	}

	return &usageLine{
		Item: &subscription.SubscriptionLineItem{
			ID:               "li_" + id,
			PriceID:          "price_" + id,
			PriceType:        types.PRICE_TYPE_USAGE,
			MeterID:          "meter_" + id,
			MeterDisplayName: "Meter " + id,
			EntityID:         "plan_" + id,
			EntityType:       types.SubscriptionLineItemEntityTypePlan,
			PlanDisplayName:  "Plan " + id,
			DisplayName:      "Usage " + id,
		},
		Price:    &price.Price{ID: "price_" + id, DisplayName: "Price " + id},
		Quantity: dec(quantity),
		Cost:     dec(cost),
		Usage:    windows,
	}
}

// committed builds a commitment stacked over the given lines with one pool per capacity.
func committed(id string, binding bool, periodAmount string, poolOf []int, lines []*usageLine, capacities ...string) *stackedCommitment {
	pools := make([]capacityPool, len(capacities))
	for i, c := range capacities {
		pools[i] = capacityPool{Capacity: dec(c)}
	}

	return &stackedCommitment{
		C: &commitment.Commitment{
			ID:                 id,
			Binding:            binding,
			InvoiceLabel:       "Label " + id,
			InvoiceDescription: lo.ToPtr("Description " + id),
			CommittedPriceID:   lo.ToPtr("committed_price_" + id),
		},
		CommittedRate: decimal.NewFromInt(1),
		Period:        commitmentPeriod{Amount: dec(periodAmount)},
		Lines:         lines,
		Pools:         pools,
		PoolOf:        poolOf,
	}
}

type wantLine struct {
	kind   types.CommitmentLineKind
	name   string
	qty    string
	amount string
}

func renderStack(t *testing.T, stack *commitmentStack, cost lineCost) []dto.CreateInvoiceLineItemRequest {
	t.Helper()
	alloc, err := allocate(stack)
	require.NoError(t, err)

	start := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	return commitmentInvoiceLines([]*commitmentStack{stack}, []*allocation{alloc}, "usd", start, start.AddDate(0, 1, 0), cost)
}

func requireLines(t *testing.T, want []wantLine, got []dto.CreateInvoiceLineItemRequest) {
	t.Helper()
	require.Len(t, got, len(want))
	for i, w := range want {
		g := got[i]
		require.Equal(t, string(w.kind), g.Metadata[types.MetadataKeyCommitmentLineKind], "line %d kind", i)
		require.Equal(t, w.name, lo.FromPtr(g.DisplayName), "line %d name", i)
		require.True(t, dec(w.qty).Equal(g.Quantity), "line %d qty: want %s, got %s", i, w.qty, g.Quantity)
		require.True(t, dec(w.amount).Equal(g.Amount), "line %d amount: want %s, got %s", i, w.amount, g.Amount)
	}
}

func TestCommitmentInvoiceLines(t *testing.T) {
	unitCost := func(_ *price.Price, qty decimal.Decimal) decimal.Decimal { return qty }
	// Stepped pricing makes per-window and summed pricing differ: a window over 35 units costs 2 each.
	stepCost := func(_ *price.Price, qty decimal.Decimal) decimal.Decimal {
		if qty.GreaterThan(intDec(35)) {
			return qty.Mul(intDec(2))
		}

		return qty
	}

	tests := []struct {
		name  string
		build func() *commitmentStack
		cost  lineCost
		want  []wantLine
	}{
		{
			name: "E1 amount commitment fully used with spill on demand has no true-up",
			build: func() *commitmentStack {
				line := invoiceLine("a", "120", "120", "120")
				return &commitmentStack{
					Type:        types.COMMITMENT_TYPE_AMOUNT,
					Measurement: types.CommitmentMeasurementBillingPeriod,
					Windows:     engineWindows(1),
					Lines:       []*usageLine{line},
					Commitments: []*stackedCommitment{committed("c1", true, "100", []int{0}, []*usageLine{line}, "100")},
				}
			},
			cost: unitCost,
			want: []wantLine{
				{types.CommitmentLineWithin, "Label c1", "100", "100"},
				{types.CommitmentLineOnDemand, "Usage a", "20", "20"},
			},
		},
		{
			name: "E2 quantity commitment under used bills within plus true-up",
			build: func() *commitmentStack {
				line := invoiceLine("a", "60", "0", "60")
				sc := committed("c1", true, "200", []int{0}, []*usageLine{line}, "100")
				sc.CommittedRate = intDec(2)
				return &commitmentStack{
					Type:        types.COMMITMENT_TYPE_QUANTITY,
					Measurement: types.CommitmentMeasurementBillingPeriod,
					Windows:     engineWindows(1),
					Lines:       []*usageLine{line},
					Commitments: []*stackedCommitment{sc},
				}
			},
			cost: unitCost,
			want: []wantLine{
				{types.CommitmentLineWithin, "Label c1", "60", "120"},
				{types.CommitmentLineTrueUp, "Label c1 (unused)", "40", "80"},
			},
		},
		{
			name: "E5 non binding commitment has no true-up",
			build: func() *commitmentStack {
				line := invoiceLine("a", "60", "0", "60")
				return &commitmentStack{
					Type:        types.COMMITMENT_TYPE_QUANTITY,
					Measurement: types.CommitmentMeasurementBillingPeriod,
					Windows:     engineWindows(1),
					Lines:       []*usageLine{line},
					Commitments: []*stackedCommitment{committed("c1", false, "100", []int{0}, []*usageLine{line}, "100")},
				}
			},
			cost: unitCost,
			want: []wantLine{
				{types.CommitmentLineWithin, "Label c1", "60", "60"},
			},
		},
		{
			name: "E6 group scope fills lines in order and the second carries the on demand",
			build: func() *commitmentStack {
				a := invoiceLine("a", "70", "70", "70")
				b := invoiceLine("b", "25", "50", "50")
				return &commitmentStack{
					Type:        types.COMMITMENT_TYPE_AMOUNT,
					Measurement: types.CommitmentMeasurementBillingPeriod,
					Windows:     engineWindows(1),
					Lines:       []*usageLine{a, b},
					Commitments: []*stackedCommitment{committed("c1", true, "100", []int{0}, []*usageLine{a, b}, "100")},
				}
			},
			cost: unitCost,
			// b's 30 of 50 cost covers 25*30/50 = 15 units, the other 10 units cost 20.
			want: []wantLine{
				{types.CommitmentLineWithin, "Label c1", "70", "70"},
				{types.CommitmentLineWithin, "Label c1", "15", "30"},
				{types.CommitmentLineOnDemand, "Usage b", "10", "20"},
			},
		},
		{
			name: "E11 clipped period prorates the true-up",
			build: func() *commitmentStack {
				line := invoiceLine("a", "20", "0", "20")
				return &commitmentStack{
					Type:        types.COMMITMENT_TYPE_QUANTITY,
					Measurement: types.CommitmentMeasurementBillingPeriod,
					Windows:     engineWindows(1),
					Lines:       []*usageLine{line},
					Commitments: []*stackedCommitment{committed("c1", true, "50", []int{0}, []*usageLine{line}, "50")},
				}
			},
			cost: unitCost,
			want: []wantLine{
				{types.CommitmentLineWithin, "Label c1", "20", "20"},
				{types.CommitmentLineTrueUp, "Label c1 (unused)", "30", "30"},
			},
		},
		{
			name: "E12 custom overage rate prices on demand without the cost function",
			build: func() *commitmentStack {
				line := invoiceLine("a", "130", "0", "130")
				line.OverageRate = lo.ToPtr(intDec(3))
				return &commitmentStack{
					Type:        types.COMMITMENT_TYPE_QUANTITY,
					Measurement: types.CommitmentMeasurementBillingPeriod,
					Windows:     engineWindows(1),
					Lines:       []*usageLine{line},
					Commitments: []*stackedCommitment{committed("c1", true, "100", []int{0}, []*usageLine{line}, "100")},
				}
			},
			cost: func(*price.Price, decimal.Decimal) decimal.Decimal {
				t.Error("cost must not be called when the line has an overage rate")
				return decimal.Zero
			},
			want: []wantLine{
				{types.CommitmentLineWithin, "Label c1", "100", "100"},
				{types.CommitmentLineOnDemand, "Usage a", "30", "90"},
			},
		},
		{
			name: "window measured on demand is priced per window",
			build: func() *commitmentStack {
				line := invoiceLine("a", "280", "0", "130", "150")
				return &commitmentStack{
					Type:        types.COMMITMENT_TYPE_QUANTITY,
					Measurement: types.CommitmentMeasurementWindow,
					Windows:     engineWindows(2),
					Lines:       []*usageLine{line},
					Commitments: []*stackedCommitment{committed("c1", true, "200", []int{0, 1}, []*usageLine{line}, "100", "100")},
				}
			},
			cost: stepCost,
			// On demand is 30 and 50: 30*1 + 50*2 = 130, where the summed 80 would cost 160.
			want: []wantLine{
				{types.CommitmentLineWithin, "Label c1", "200", "200"},
				{types.CommitmentLineOnDemand, "Usage a", "80", "130"},
			},
		},
		{
			name: "billing period on demand is priced once on the summed quantity",
			build: func() *commitmentStack {
				line := invoiceLine("a", "180", "0", "60", "60", "60")
				return &commitmentStack{
					Type:        types.COMMITMENT_TYPE_QUANTITY,
					Measurement: types.CommitmentMeasurementBillingPeriod,
					Windows:     engineWindows(3),
					Lines:       []*usageLine{line},
					Commitments: []*stackedCommitment{committed("c1", true, "100", []int{0, 0, 0}, []*usageLine{line}, "100")},
				}
			},
			cost: stepCost,
			// On demand is 0, 20 and 60 across windows: 80*2 = 160, where per window would cost 20 + 120.
			want: []wantLine{
				{types.CommitmentLineWithin, "Label c1", "100", "100"},
				{types.CommitmentLineOnDemand, "Usage a", "80", "160"},
			},
		},
		{
			name: "E14 rounding drift lands on the largest within line",
			build: func() *commitmentStack {
				a := invoiceLine("a", "10.004", "10.004", "10.004")
				b := invoiceLine("b", "10.004", "10.004", "10.004")
				c := invoiceLine("c", "79.992", "79.992", "79.992")
				return &commitmentStack{
					Type:        types.COMMITMENT_TYPE_AMOUNT,
					Measurement: types.CommitmentMeasurementBillingPeriod,
					Windows:     engineWindows(1),
					Lines:       []*usageLine{a, b, c},
					Commitments: []*stackedCommitment{committed("c1", true, "100", []int{0}, []*usageLine{a, b, c}, "100")},
				}
			},
			cost: unitCost,
			// Rounded lines sum to 99.99 against a rounded total of 100.00, so c takes the extra cent.
			want: []wantLine{
				{types.CommitmentLineWithin, "Label c1", "10.004", "10.00"},
				{types.CommitmentLineWithin, "Label c1", "10.004", "10.00"},
				{types.CommitmentLineWithin, "Label c1", "79.992", "80.00"},
			},
		},
		{
			name: "E14 within plus true-up equals the rounded period amount",
			build: func() *commitmentStack {
				a := invoiceLine("a", "10.004", "10.004", "10.004")
				b := invoiceLine("b", "10.004", "10.004", "10.004")
				c := invoiceLine("c", "59.992", "59.992", "59.992")
				return &commitmentStack{
					Type:        types.COMMITMENT_TYPE_AMOUNT,
					Measurement: types.CommitmentMeasurementBillingPeriod,
					Windows:     engineWindows(1),
					Lines:       []*usageLine{a, b, c},
					Commitments: []*stackedCommitment{committed("c1", true, "100.004", []int{0}, []*usageLine{a, b, c}, "100.004")},
				}
			},
			cost: unitCost,
			// Within rounds to 10.00 + 10.00 + 59.99 + 0.01 drift = 80.00; true-up is 100.00 - 80.00.
			want: []wantLine{
				{types.CommitmentLineWithin, "Label c1", "10.004", "10.00"},
				{types.CommitmentLineWithin, "Label c1", "10.004", "10.00"},
				{types.CommitmentLineWithin, "Label c1", "59.992", "60.00"},
				{types.CommitmentLineTrueUp, "Label c1 (unused)", "1", "20.00"},
			},
		},
		{
			name: "free usage on an amount stack shows its quantity at zero amount",
			build: func() *commitmentStack {
				line := invoiceLine("a", "500", "0", "0")
				return &commitmentStack{
					Type:        types.COMMITMENT_TYPE_AMOUNT,
					Measurement: types.CommitmentMeasurementBillingPeriod,
					Windows:     engineWindows(1),
					Lines:       []*usageLine{line},
					Commitments: []*stackedCommitment{committed("c1", false, "100", []int{0}, []*usageLine{line}, "100")},
				}
			},
			cost: unitCost,
			want: []wantLine{
				{types.CommitmentLineOnDemand, "Usage a", "500", "0"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			requireLines(t, tt.want, renderStack(t, tt.build(), tt.cost))
		})
	}
}

func TestCommitmentInvoiceLinesFields(t *testing.T) {
	line := invoiceLine("a", "130", "0", "130")
	stack := &commitmentStack{
		Type:        types.COMMITMENT_TYPE_QUANTITY,
		Measurement: types.CommitmentMeasurementBillingPeriod,
		Windows:     engineWindows(1),
		Lines:       []*usageLine{line},
		Commitments: []*stackedCommitment{committed("c1", true, "150", []int{0}, []*usageLine{line}, "100")},
	}

	got := renderStack(t, stack, func(_ *price.Price, qty decimal.Decimal) decimal.Decimal { return qty })
	requireLines(t, []wantLine{
		{types.CommitmentLineWithin, "Label c1", "100", "100"},
		{types.CommitmentLineTrueUp, "Label c1 (unused)", "0", "50"},
		{types.CommitmentLineOnDemand, "Usage a", "30", "30"},
	}, got)

	periodStart := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	for i, g := range got {
		require.Equal(t, periodStart, lo.FromPtr(g.PeriodStart), "line %d period start", i)
		require.Equal(t, periodStart.AddDate(0, 1, 0), lo.FromPtr(g.PeriodEnd), "line %d period end", i)
	}

	within, trueUp, onDemand := got[0], got[1], got[2]
	for _, g := range []dto.CreateInvoiceLineItemRequest{within, trueUp} {
		require.Equal(t, "c1", lo.FromPtr(g.CommitmentID))
		require.Equal(t, "c1", g.Metadata[types.MetadataKeyCommitmentID])
		require.Equal(t, "Description c1", g.Metadata[types.MetadataKeyDescription])
	}

	// On-demand belongs to the stack, not to any one commitment.
	require.Nil(t, onDemand.CommitmentID)
	require.NotContains(t, onDemand.Metadata, types.MetadataKeyCommitmentID)
	require.NotContains(t, onDemand.Metadata, types.MetadataKeyDescription)
	require.Equal(t, string(types.CommitmentLineOnDemand), onDemand.Metadata[types.MetadataKeyCommitmentLineKind])

	for _, g := range []dto.CreateInvoiceLineItemRequest{within, onDemand} {
		require.Equal(t, "price_a", lo.FromPtr(g.PriceID))
		require.Equal(t, string(types.PRICE_TYPE_USAGE), lo.FromPtr(g.PriceType))
		require.Equal(t, "meter_a", lo.FromPtr(g.MeterID))
		require.Equal(t, "Meter a", lo.FromPtr(g.MeterDisplayName))
		require.Equal(t, "plan_a", lo.FromPtr(g.EntityID))
		require.Equal(t, string(types.SubscriptionLineItemEntityTypePlan), lo.FromPtr(g.EntityType))
		require.Equal(t, "Plan a", lo.FromPtr(g.PlanDisplayName))
		require.Equal(t, "li_a", lo.FromPtr(g.SubscriptionLineItemID))
	}

	require.Equal(t, "committed_price_c1", lo.FromPtr(trueUp.PriceID))
	require.Equal(t, string(types.PRICE_TYPE_FIXED), lo.FromPtr(trueUp.PriceType))
	require.Equal(t, string(types.InvoiceLineItemEntityTypeCommitment), lo.FromPtr(trueUp.EntityType))
	require.Equal(t, "c1", lo.FromPtr(trueUp.EntityID))
	require.Nil(t, trueUp.SubscriptionLineItemID)
}

func TestCommitmentInvoiceLinesNegativeDriftNeverGoesBelowZero(t *testing.T) {
	// Four half-cent lines each round up to $0.01; the rounded total is $0.02, so $0.02 of
	// negative drift must come back out without turning any row into a credit.
	lines := []*usageLine{
		invoiceLine("a", "1", "0.005", "0.005"),
		invoiceLine("b", "1", "0.005", "0.005"),
		invoiceLine("c", "1", "0.005", "0.005"),
		invoiceLine("d", "1", "0.005", "0.005"),
	}
	stack := &commitmentStack{
		Type:        types.COMMITMENT_TYPE_AMOUNT,
		Measurement: types.CommitmentMeasurementBillingPeriod,
		Windows:     engineWindows(1),
		Lines:       lines,
		Commitments: []*stackedCommitment{committed("c1", true, "100", []int{0}, lines, "100")},
	}

	got := renderStack(t, stack, func(_ *price.Price, qty decimal.Decimal) decimal.Decimal { return qty })
	requireLines(t, []wantLine{
		{types.CommitmentLineWithin, "Label c1", "1", "0"},
		{types.CommitmentLineWithin, "Label c1", "1", "0"},
		{types.CommitmentLineWithin, "Label c1", "1", "0.01"},
		{types.CommitmentLineWithin, "Label c1", "1", "0.01"},
		{types.CommitmentLineTrueUp, "Label c1 (unused)", "1", "99.98"},
	}, got)

	sum := decimal.Zero
	for _, g := range got {
		require.False(t, g.Amount.IsNegative(), "no row may be negative, got %s", g.Amount)
		sum = sum.Add(g.Amount)
	}

	require.True(t, sum.Equal(dec("100")), "within plus true-up must equal the period amount, got %s", sum)
}

func TestAllocateRejectsPoolIndexBelowSentinel(t *testing.T) {
	line := quantityLine(10)
	stack := &commitmentStack{
		Type:        types.COMMITMENT_TYPE_QUANTITY,
		Measurement: types.CommitmentMeasurementWindow,
		Windows:     engineWindows(1),
		Lines:       []*usageLine{line},
		Commitments: []*stackedCommitment{stacked("c1", 1, []int{-2}, []*usageLine{line}, 10)},
	}

	_, err := allocate(stack)
	require.Error(t, err)
	require.Contains(t, err.Error(), "references pool")
}
