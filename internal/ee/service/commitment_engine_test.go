package service

import (
	"strings"
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
		wantOverage  [][]int64
	}{
		{
			name: "E17 usage below first commitment",
			build: func() *commitmentStack {
				return e17Stack(6)
			},
			wantUsed:     []int64{6, 0},
			wantUnused:   []int64{4, 10},
			wantCapacity: []int64{10, 10},
			wantOverage:  [][]int64{{0}},
		},
		{
			name: "E17 usage spills into second commitment",
			build: func() *commitmentStack {
				return e17Stack(15)
			},
			wantUsed:     []int64{10, 5},
			wantUnused:   []int64{0, 5},
			wantCapacity: []int64{10, 10},
			wantOverage:  [][]int64{{0}},
		},
		{
			name: "E17 usage exceeds the whole stack",
			build: func() *commitmentStack {
				return e17Stack(24)
			},
			wantUsed:     []int64{10, 10},
			wantUnused:   []int64{0, 0},
			wantCapacity: []int64{10, 10},
			wantOverage:  [][]int64{{4}},
		},
		{
			name: "E18 second commitment outside its window sends overflow to overage",
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
			wantOverage:  [][]int64{{5, 0}},
		},
		{
			name: "E3 quantity per window with overage",
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
			wantOverage:  [][]int64{{0, 50, 0}},
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
			wantOverage:  [][]int64{{30, 0, 20, 20, 30}},
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
			wantOverage:  [][]int64{{0}, {20}},
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
			wantOverage:  [][]int64{{20}, {0}},
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
			wantOverage:  [][]int64{{0, 20}},
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

			require.Len(t, got.Overage, len(tt.wantOverage))
			for i := range tt.wantOverage {
				requireDecimals(t, tt.wantOverage[i], got.Overage[i])
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
		Price:    &price.Price{ID: "price_" + id, DisplayName: "Price " + id, BillingModel: types.BILLING_MODEL_FLAT_FEE},
		Quantity: dec(quantity),
		Cost:     dec(cost),
		Usage:    windows,
	}
}

// committed builds a commitment stacked over the given lines with one pool per capacity.
func committed(id string, binding bool, periodAmount string, poolOf []int, lines []*usageLine, capacities ...string) *stackedCommitment {
	pools := make([]capacityPool, len(capacities))
	capacity := decimal.Zero
	for i, c := range capacities {
		pools[i] = capacityPool{Capacity: dec(c)}
		capacity = capacity.Add(dec(c))
	}

	return &stackedCommitment{
		C: &commitment.Commitment{
			ID:                 id,
			Binding:            binding,
			InvoiceLabel:       "Label " + id,
			InvoiceDescription: lo.ToPtr("Description " + id),
			CommittedPriceID:   lo.ToPtr("committed_price_" + id),
		},
		CommittedPrice: &price.Price{ID: "committed_price_" + id, BillingModel: types.BILLING_MODEL_FLAT_FEE, Amount: intDec(1)},
		Period:         commitmentPeriod{Amount: dec(periodAmount), Capacity: capacity},
		Lines:          lines,
		Pools:          pools,
		PoolOf:         poolOf,
	}
}

type wantLine struct {
	kind   types.CommitmentLineKind
	name   string
	qty    string
	amount string
}

// withCommittedPrices prices committed prices by their flat amount or slab tiers, and every other price with cost.
func withCommittedPrices(cost lineCost) lineCost {
	return func(p *price.Price, qty decimal.Decimal) decimal.Decimal {
		if !strings.HasPrefix(p.ID, "committed_price_") {
			return cost(p, qty)
		}

		if p.BillingModel != types.BILLING_MODEL_TIERED {
			return qty.Mul(p.Amount)
		}

		total, from := decimal.Zero, decimal.Zero
		for _, tier := range p.Tiers {
			upTo := qty
			if tier.UpTo != nil {
				upTo = decimal.Min(qty, decimal.NewFromUint64(*tier.UpTo))
			}

			if upTo.GreaterThan(from) {
				total = total.Add(upTo.Sub(from).Mul(tier.UnitAmount))
				from = upTo
			}
		}

		return total
	}
}

func renderStack(t *testing.T, stack *commitmentStack, cost lineCost) []dto.CreateInvoiceLineItemRequest {
	t.Helper()
	alloc, err := allocate(stack)
	require.NoError(t, err)

	start := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	return commitmentInvoiceLines(
		[]*commitmentStack{stack},
		[]*allocation{alloc},
		"usd",
		start,
		start.AddDate(0, 1, 0),
		withCommittedPrices(cost),
	)
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
	// slabCost charges 1 per unit up to 120 and 2 per unit above, so the overage units cost by where they sit.
	slabCost := func(_ *price.Price, qty decimal.Decimal) decimal.Decimal {
		first := decimal.Min(qty, intDec(120))
		return first.Add(qty.Sub(first).Mul(intDec(2)))
	}
	// overageCost prices only overage prices: flat at qty × amount, tiered at 3 for the first 20 units and 5 after.
	overageCost := func(p *price.Price, qty decimal.Decimal) decimal.Decimal {
		if p.ID != "overage_a" {
			t.Errorf("cost called with price %s, want the overage price", p.ID)
		}

		if p.BillingModel == types.BILLING_MODEL_TIERED {
			first := decimal.Min(qty, intDec(20))
			return first.Mul(intDec(3)).Add(qty.Sub(first).Mul(intDec(5)))
		}

		return qty.Mul(p.Amount)
	}

	tests := []struct {
		name  string
		build func() *commitmentStack
		cost  lineCost
		want  []wantLine
	}{
		{
			name: "E1 amount commitment fully used with spill to overage has no true-up",
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
				{types.CommitmentLineUtilized, "Label c1", "100", "100"},
				{types.CommitmentLineOverage, "Usage a", "20", "20"},
			},
		},
		{
			name: "E2 quantity commitment under used bills within plus true-up",
			build: func() *commitmentStack {
				line := invoiceLine("a", "60", "0", "60")
				sc := committed("c1", true, "200", []int{0}, []*usageLine{line}, "100")
				sc.CommittedPrice.Amount = intDec(2)
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
				{types.CommitmentLineUtilized, "Label c1", "60", "120"},
				{types.CommitmentLineTrueUp, "Label c1 (unused)", "40", "80"},
			},
		},
		{
			name: "tiered committed price walks its tiers once over the commitment's usage and splits it by quantity",
			build: func() *commitmentStack {
				a := invoiceLine("a", "80", "0", "80")
				b := invoiceLine("b", "40", "0", "40")
				sc := committed("c1", true, "125", []int{0}, []*usageLine{a, b}, "150")
				sc.CommittedPrice = &price.Price{
					ID:           "committed_price_c1",
					BillingModel: types.BILLING_MODEL_TIERED,
					TierMode:     types.BILLING_TIER_SLAB,
					Tiers: []price.PriceTier{
						{UpTo: lo.ToPtr(uint64(100)), UnitAmount: intDec(1)},
						{UnitAmount: dec("0.5")},
					},
				}
				return &commitmentStack{
					Type:        types.COMMITMENT_TYPE_QUANTITY,
					Measurement: types.CommitmentMeasurementBillingPeriod,
					Windows:     engineWindows(1),
					Lines:       []*usageLine{a, b},
					Commitments: []*stackedCommitment{sc},
				}
			},
			cost: unitCost,
			want: []wantLine{
				{types.CommitmentLineUtilized, "Label c1", "80", "73.33"},
				{types.CommitmentLineUtilized, "Label c1", "40", "36.67"},
				{types.CommitmentLineTrueUp, "Label c1 (unused)", "30", "15"},
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
				{types.CommitmentLineUtilized, "Label c1", "60", "60"},
			},
		},
		{
			name: "E6 group scope fills lines in order and the second carries the overage",
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
				{types.CommitmentLineUtilized, "Label c1", "70", "70"},
				{types.CommitmentLineUtilized, "Label c1", "15", "30"},
				{types.CommitmentLineOverage, "Usage b", "10", "20"},
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
				{types.CommitmentLineUtilized, "Label c1", "20", "20"},
				{types.CommitmentLineTrueUp, "Label c1 (unused)", "30", "30"},
			},
		},
		{
			name: "E12 flat overage price prices overage instead of the line's price",
			build: func() *commitmentStack {
				line := invoiceLine("a", "130", "0", "130")
				line.OveragePrice = &price.Price{ID: "overage_a", BillingModel: types.BILLING_MODEL_FLAT_FEE, Amount: intDec(3)}
				return &commitmentStack{
					Type:        types.COMMITMENT_TYPE_QUANTITY,
					Measurement: types.CommitmentMeasurementBillingPeriod,
					Windows:     engineWindows(1),
					Lines:       []*usageLine{line},
					Commitments: []*stackedCommitment{committed("c1", true, "100", []int{0}, []*usageLine{line}, "100")},
				}
			},
			cost: overageCost,
			want: []wantLine{
				{types.CommitmentLineUtilized, "Label c1", "100", "100"},
				{types.CommitmentLineOverage, "Usage a", "30", "90"},
			},
		},
		{
			name: "tiered overage price walks its tiers over the period total of a window stack",
			build: func() *commitmentStack {
				line := invoiceLine("a", "230", "0", "110", "120")
				line.OveragePrice = &price.Price{ID: "overage_a", BillingModel: types.BILLING_MODEL_TIERED}
				return &commitmentStack{
					Type:        types.COMMITMENT_TYPE_QUANTITY,
					Measurement: types.CommitmentMeasurementWindow,
					Windows:     engineWindows(2),
					Lines:       []*usageLine{line},
					Commitments: []*stackedCommitment{committed("c1", true, "200", []int{0, 1}, []*usageLine{line}, "100", "100")},
				}
			},
			cost: overageCost,
			// Overage is 10 and 20: the summed 30 is 20*3 + 10*5 = 110, where per window would cost 10*3 + 20*3 = 90.
			want: []wantLine{
				{types.CommitmentLineUtilized, "Label c1", "200", "200"},
				{types.CommitmentLineOverage, "Usage a", "30", "110"},
			},
		},
		{
			name: "a per window line prices each window's overage above the units covered in it",
			build: func() *commitmentStack {
				line := invoiceLine("a", "280", "0", "130", "150")
				line.PerWindow = true
				return &commitmentStack{
					Type:        types.COMMITMENT_TYPE_QUANTITY,
					Measurement: types.CommitmentMeasurementWindow,
					Windows:     engineWindows(2),
					Lines:       []*usageLine{line},
					Commitments: []*stackedCommitment{committed("c1", true, "200", []int{0, 1}, []*usageLine{line}, "100", "100")},
				}
			},
			cost: slabCost,
			// Each window covers 100: cost(130)-cost(100) = 140-100 = 40 and cost(150)-cost(100) = 180-100 = 80, where
			// the summed line would cost cost(280)-cost(200) = 440-280 = 160.
			want: []wantLine{
				{types.CommitmentLineUtilized, "Label c1", "200", "200"},
				{types.CommitmentLineOverage, "Usage a", "80", "120"},
			},
		},
		{
			name: "billing period overage is priced once on the line's units above the covered ones",
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
			cost: slabCost,
			// 100 of 180 covered: cost(180)-cost(100) = 240-100 = 140, where per window would cost
			// cost(60)-cost(40) + cost(60) = 20 + 60 = 80.
			want: []wantLine{
				{types.CommitmentLineUtilized, "Label c1", "100", "100"},
				{types.CommitmentLineOverage, "Usage a", "80", "140"},
			},
		},
		{
			name: "a tiered line half covered prices its overage from the covered quantity up, not from tier zero",
			build: func() *commitmentStack {
				line := invoiceLine("a", "200", "150", "200")
				return &commitmentStack{
					Type:        types.COMMITMENT_TYPE_QUANTITY,
					Measurement: types.CommitmentMeasurementBillingPeriod,
					Windows:     engineWindows(1),
					Lines:       []*usageLine{line},
					Commitments: []*stackedCommitment{committed("c1", true, "100", []int{0}, []*usageLine{line}, "100")},
				}
			},
			// 100 at 1 then 0.50: the 100 overage units are 101-200, cost(200)-cost(100) = 150-100 = 50; restarting
			// at tier zero would charge cost(100) = 100.
			cost: func(_ *price.Price, qty decimal.Decimal) decimal.Decimal {
				first := decimal.Min(qty, intDec(100))
				return first.Add(qty.Sub(first).Mul(dec("0.5")))
			},
			want: []wantLine{
				{types.CommitmentLineUtilized, "Label c1", "100", "100"},
				{types.CommitmentLineOverage, "Usage a", "100", "50"},
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
				{types.CommitmentLineUtilized, "Label c1", "10.004", "10.00"},
				{types.CommitmentLineUtilized, "Label c1", "10.004", "10.00"},
				{types.CommitmentLineUtilized, "Label c1", "79.992", "80.00"},
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
				{types.CommitmentLineUtilized, "Label c1", "10.004", "10.00"},
				{types.CommitmentLineUtilized, "Label c1", "10.004", "10.00"},
				{types.CommitmentLineUtilized, "Label c1", "59.992", "60.00"},
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
				{types.CommitmentLineOverage, "Usage a", "500", "0"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			requireLines(t, tt.want, renderStack(t, tt.build(), tt.cost))
		})
	}
}

// A mid-period preview accrues pool capacity only to asOf, while the floor spans the whole period.
// The true-up line's quantity must follow the span, like its amount, not the as-of allocation.
func TestCommitmentInvoiceLinesTrueUpQuantitySpansThePeriodOnPreviews(t *testing.T) {
	line := invoiceLine("a", "16", "0", "16")
	sc := committed("c1", true, "372", []int{0}, []*usageLine{line}, "100")
	sc.CommittedPrice.Amount = dec("1.5")
	sc.Period.Capacity = dec("248")
	stack := &commitmentStack{
		Type:        types.COMMITMENT_TYPE_QUANTITY,
		Measurement: types.CommitmentMeasurementWindow,
		Windows:     engineWindows(1),
		Lines:       []*usageLine{line},
		Commitments: []*stackedCommitment{sc},
	}

	flatCost := func(p *price.Price, qty decimal.Decimal) decimal.Decimal { return qty.Mul(p.Amount) }
	got := renderStack(t, stack, flatCost)
	requireLines(t, []wantLine{
		{types.CommitmentLineUtilized, "Label c1", "16", "24"},
		{types.CommitmentLineTrueUp, "Label c1 (unused)", "232", "348"},
	}, got)
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
		{types.CommitmentLineUtilized, "Label c1", "100", "100"},
		{types.CommitmentLineTrueUp, "Label c1 (unused)", "0", "50"},
		{types.CommitmentLineOverage, "Usage a", "30", "30"},
	}, got)

	periodStart := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	for i, g := range got {
		require.Equal(t, periodStart, lo.FromPtr(g.PeriodStart), "line %d period start", i)
		require.Equal(t, periodStart.AddDate(0, 1, 0), lo.FromPtr(g.PeriodEnd), "line %d period end", i)
	}

	within, trueUp, overage := got[0], got[1], got[2]
	for _, g := range []dto.CreateInvoiceLineItemRequest{within, trueUp} {
		require.Equal(t, "c1", lo.FromPtr(g.CommitmentID))
		require.Equal(t, "c1", g.Metadata[types.MetadataKeyCommitmentID])
		require.Equal(t, "Description c1", g.Metadata[types.MetadataKeyDescription])
	}

	// Overage belongs to the stack, not to any one commitment.
	require.Nil(t, overage.CommitmentID)
	require.NotContains(t, overage.Metadata, types.MetadataKeyCommitmentID)
	require.NotContains(t, overage.Metadata, types.MetadataKeyDescription)
	require.Equal(t, string(types.CommitmentLineOverage), overage.Metadata[types.MetadataKeyCommitmentLineKind])

	for _, g := range []dto.CreateInvoiceLineItemRequest{within, overage} {
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
		{types.CommitmentLineUtilized, "Label c1", "1", "0"},
		{types.CommitmentLineUtilized, "Label c1", "1", "0"},
		{types.CommitmentLineUtilized, "Label c1", "1", "0.01"},
		{types.CommitmentLineUtilized, "Label c1", "1", "0.01"},
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

// partialHalves renders a binding 100-unit April commitment, committed at 50 x 1 then 0.50, over its two half-month
// invoices with the given usage in each.
func partialHalves(t *testing.T, binding bool, used ...string) [][]dto.CreateInvoiceLineItemRequest {
	t.Helper()
	apr := time.Date(2027, 4, 1, 0, 0, 0, 0, time.UTC)
	split, may := apr.AddDate(0, 0, 15), apr.AddDate(0, 1, 0)
	tiers := []price.PriceTier{{UpTo: lo.ToPtr(uint64(50)), UnitAmount: intDec(1)}, {UnitAmount: dec("0.5")}}
	committedPrice := &price.Price{ID: "committed_price_c1", BillingModel: types.BILLING_MODEL_TIERED, TierMode: types.BILLING_TIER_SLAB, Tiers: tiers}
	amountOf := func(q decimal.Decimal) decimal.Decimal { return withCommittedPrices(nil)(committedPrice, q) }
	c := &commitment.Commitment{
		ID:          "c1",
		Type:        types.COMMITMENT_TYPE_QUANTITY,
		Value:       intDec(100),
		Currency:    "usd",
		Measurement: types.CommitmentMeasurementBillingPeriod,
		Timezone:    "UTC",
		StartDate:   apr,
		EndDate:     lo.ToPtr(may),
	}
	periods, err := commitmentPeriods(c, monthlySub(apr), amountOf, "", apr, may)
	require.NoError(t, err)
	require.Len(t, periods, 1)

	edges := []time.Time{apr, split, may}
	out := make([][]dto.CreateInvoiceLineItemRequest, len(used))
	for i, u := range used {
		part := clipCommitmentPeriod(c, periods[0], edges[i], edges[i+1], amountOf, "", apr)
		line := invoiceLine("a", u, "0", u)
		sc := committed("c1", binding, "0", []int{0}, []*usageLine{line}, part.Capacity.String())
		sc.CommittedPrice, sc.Period = committedPrice, part
		out[i] = renderStack(t, &commitmentStack{
			Type:        types.COMMITMENT_TYPE_QUANTITY,
			Measurement: types.CommitmentMeasurementBillingPeriod,
			Windows:     engineWindows(1),
			Lines:       []*usageLine{line},
			Commitments: []*stackedCommitment{sc},
		}, func(_ *price.Price, qty decimal.Decimal) decimal.Decimal { return qty })
	}

	return out
}

func TestCommitmentInvoiceLinesPartialWithinIsCappedAtTheFloor(t *testing.T) {
	tests := []struct {
		name    string
		binding bool
		used    []string
		want    [][]wantLine
		total   string
	}{
		{
			name:    "fully used halves bill the whole period's 75, not 50 + 50",
			binding: true,
			used:    []string{"50", "50"},
			// Floors are 50 and 75-50 = 25; the second half's cost(50) = 50 within is capped at its 25.
			want: [][]wantLine{
				{{types.CommitmentLineUtilized, "Label c1", "50", "50"}},
				{{types.CommitmentLineUtilized, "Label c1", "50", "25"}},
			},
			total: "75",
		},
		{
			name:    "an under-used first half trues up and an over-the-cap second half is capped",
			binding: true,
			used:    []string{"30", "50"},
			// Half one: within cost(30) = 30, true-up 50-30 = 20. Half two: within 50 capped at 25.
			want: [][]wantLine{
				{{types.CommitmentLineUtilized, "Label c1", "30", "30"}, {types.CommitmentLineTrueUp, "Label c1 (unused)", "20", "20"}},
				{{types.CommitmentLineUtilized, "Label c1", "50", "25"}},
			},
			total: "75",
		},
		{
			name:    "a non-binding commitment has no floor, so its partial within restarts the tiers",
			binding: false,
			used:    []string{"50", "50"},
			want: [][]wantLine{
				{{types.CommitmentLineUtilized, "Label c1", "50", "50"}},
				{{types.CommitmentLineUtilized, "Label c1", "50", "50"}},
			},
			total: "100",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			total := decimal.Zero
			for i, lines := range partialHalves(t, tt.binding, tt.used...) {
				requireLines(t, tt.want[i], lines)
				for _, l := range lines {
					total = total.Add(l.Amount)
				}
			}

			require.True(t, total.Equal(dec(tt.total)), "total %s", total)
		})
	}
}

func TestCommitmentInvoiceLinesCappedWithinConservesTheFloorAcrossLines(t *testing.T) {
	a, b, c := invoiceLine("a", "10", "0", "10"), invoiceLine("b", "10", "0", "10"), invoiceLine("c", "10", "0", "10")
	sc := committed("c1", true, "10", []int{0}, []*usageLine{a, b, c}, "30")
	sc.Period.Clipped = true
	lines := renderStack(t, &commitmentStack{
		Type:        types.COMMITMENT_TYPE_QUANTITY,
		Measurement: types.CommitmentMeasurementBillingPeriod,
		Windows:     engineWindows(1),
		Lines:       []*usageLine{a, b, c},
		Commitments: []*stackedCommitment{sc},
	}, func(_ *price.Price, qty decimal.Decimal) decimal.Decimal { return qty })

	// 30 within capped at 10 scales each row to 3.33; the cent left over lands on the first, largest row.
	requireLines(t, []wantLine{
		{types.CommitmentLineUtilized, "Label c1", "10", "3.34"},
		{types.CommitmentLineUtilized, "Label c1", "10", "3.33"},
		{types.CommitmentLineUtilized, "Label c1", "10", "3.33"},
	}, lines)
}

// A volume price reprices every unit by the total, so 150 used with 100 covered would difference to
// cost(150)-cost(100) = 75-100 = -25; the 50 overage units are priced afresh instead.
func TestCommitmentInvoiceLinesVolumeLinePricesOverageAfresh(t *testing.T) {
	volumeCost := func(_ *price.Price, qty decimal.Decimal) decimal.Decimal {
		if qty.GreaterThan(intDec(100)) {
			return qty.Mul(dec("0.5"))
		}

		return qty
	}

	for _, perWindow := range []bool{false, true} {
		line := invoiceLine("a", "150", "75", "150")
		line.Price.BillingModel, line.Price.TierMode, line.PerWindow = types.BILLING_MODEL_TIERED, types.BILLING_TIER_VOLUME, perWindow
		lines := renderStack(t, &commitmentStack{
			Type:        types.COMMITMENT_TYPE_QUANTITY,
			Measurement: types.CommitmentMeasurementBillingPeriod,
			Windows:     engineWindows(1),
			Lines:       []*usageLine{line},
			Commitments: []*stackedCommitment{committed("c1", true, "100", []int{0}, []*usageLine{line}, "100")},
		}, volumeCost)

		// cost(50) lands in the first volume tier: 50 x 1 = 50.
		requireLines(t, []wantLine{
			{types.CommitmentLineUtilized, "Label c1", "100", "100"},
			{types.CommitmentLineOverage, "Usage a", "50", "50"},
		}, lines)
	}
}

// A package price bills whole packages, so differencing 100 used against 50 covered would price the 50 overage units at
// cost(100)-cost(50) = $0; the remainder is priced afresh as one package.
func TestCommitmentInvoiceLinesPackageLinePricesOverageAfresh(t *testing.T) {
	packageCost := func(_ *price.Price, qty decimal.Decimal) decimal.Decimal {
		return qty.Div(intDec(100)).Ceil().Mul(intDec(10))
	}

	for _, perWindow := range []bool{false, true} {
		line := invoiceLine("a", "100", "10", "100")
		line.Price.BillingModel, line.PerWindow = types.BILLING_MODEL_PACKAGE, perWindow
		lines := renderStack(t, &commitmentStack{
			Type:        types.COMMITMENT_TYPE_QUANTITY,
			Measurement: types.CommitmentMeasurementBillingPeriod,
			Windows:     engineWindows(1),
			Lines:       []*usageLine{line},
			Commitments: []*stackedCommitment{committed("c1", true, "50", []int{0}, []*usageLine{line}, "50")},
		}, packageCost)

		requireLines(t, []wantLine{
			{types.CommitmentLineUtilized, "Label c1", "50", "50"},
			{types.CommitmentLineOverage, "Usage a", "50", "10"},
		}, lines)
	}
}
