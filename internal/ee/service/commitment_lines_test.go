package service

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/commitment"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func linesByKind(lines []dto.CreateInvoiceLineItemRequest, kind types.CommitmentLineKind) []dto.CreateInvoiceLineItemRequest {
	return lo.Filter(lines, func(l dto.CreateInvoiceLineItemRequest, _ int) bool {
		return l.Metadata[types.MetadataKeyCommitmentLineKind] == string(kind)
	})
}

func perUnitCost(p *price.Price, qty decimal.Decimal) decimal.Decimal {
	return p.Amount.Mul(qty)
}

func amountInput(usageCost string, binding bool) commitmentLineInput {
	t0 := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	p := &price.Price{
		ID:          "price_api",
		Amount:      dec("1"),
		DisplayName: "API calls",
	}
	li := &subscription.SubscriptionLineItem{
		ID:          "li_api",
		PriceID:     p.ID,
		PriceType:   types.PRICE_TYPE_USAGE,
		DisplayName: "API calls",
	}
	c := &commitment.Commitment{
		ID:               "cmt_1",
		Type:             types.COMMITMENT_TYPE_AMOUNT,
		Value:            dec("100"),
		Binding:          binding,
		InvoiceLabel:     "Platform commitment",
		CollectionMethod: lo.ToPtr(types.CommitmentCollectionPerPeriod),
	}
	stack := []*stackedCommitment{
		{
			C:        c,
			Rates:    commitmentRates{Committed: dec("1")},
			Capacity: fixedCap("100"),
			LineIDs:  []string{"li_api"},
		},
	}

	cost := dec(usageCost)
	alloc := allocateCommitments(stack, []windowUsage{
		{
			Start:  t0,
			ByLine: map[string]decimal.Decimal{"li_api": cost},
		},
	})

	return commitmentLineInput{
		Stack: stack,
		Alloc: alloc,
		Lines: map[string]*scopedLine{
			"li_api": {
				Item:     li,
				Price:    p,
				Quantity: cost,
				Cost:     cost,
			},
		},
		PeriodByCmt: map[string]commitmentPeriod{
			"cmt_1": {
				Amount:   dec("100"),
				Capacity: dec("100"),
			},
		},
		PeriodStart: t0,
		PeriodEnd:   t0.AddDate(0, 1, 0),
		Currency:    "usd",
		PriceCost:   perUnitCost,
	}
}

func TestBuildCommitmentLines_PerPeriodAmount(t *testing.T) {
	// E1: usage 120 → within 100, on-demand 20, no true-up
	lines, err := buildCommitmentLines(amountInput("120", true))
	require.NoError(t, err)
	require.True(t, linesByKind(lines, types.CommitmentLineWithin)[0].Amount.Equal(dec("100")))
	require.True(t, linesByKind(lines, types.CommitmentLineOnDemand)[0].Amount.Equal(dec("20")))
	require.Empty(t, linesByKind(lines, types.CommitmentLineTrueUp))

	// E2: usage 60 → within 60, true-up 40
	lines, err = buildCommitmentLines(amountInput("60", true))
	require.NoError(t, err)

	within := linesByKind(lines, types.CommitmentLineWithin)[0]
	trueUp := linesByKind(lines, types.CommitmentLineTrueUp)[0]
	require.True(t, within.Amount.Equal(dec("60")))
	require.True(t, trueUp.Amount.Equal(dec("40")))
	require.Equal(t, "Platform commitment (unused)", lo.FromPtr(trueUp.DisplayName))
	require.Equal(t, "cmt_1", lo.FromPtr(trueUp.CommitmentID))

	// E14: within + true-up = period amount
	require.True(t, within.Amount.Add(trueUp.Amount).Equal(dec("100")))

	// E5: non-binding, usage 60 → within 60, no true-up
	lines, err = buildCommitmentLines(amountInput("60", false))
	require.NoError(t, err)
	require.Empty(t, linesByKind(lines, types.CommitmentLineTrueUp))
}

func TestBuildCommitmentLines_QuantityTrueUpAtCommittedRate(t *testing.T) {
	// E3: 8/min; usage 10, 5, 8 → within 21 @0.048, true-up 3 @0.048, on-demand 2 @ list 0.06
	t0 := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	p := &price.Price{
		ID:          "price_h100",
		Amount:      dec("0.06"),
		DisplayName: "H100",
	}
	li := &subscription.SubscriptionLineItem{
		ID:          "li",
		PriceID:     p.ID,
		PriceType:   types.PRICE_TYPE_USAGE,
		DisplayName: "H100",
	}
	c := &commitment.Commitment{
		ID:               "cmt_gpu",
		Type:             types.COMMITMENT_TYPE_QUANTITY,
		Value:            dec("8"),
		Binding:          true,
		InvoiceLabel:     "1× H100 reserved GPU",
		CommittedPriceID: lo.ToPtr("price_committed"),
		CollectionMethod: lo.ToPtr(types.CommitmentCollectionPerPeriod),
	}
	stack := []*stackedCommitment{
		{
			C:        c,
			Rates:    commitmentRates{Committed: dec("0.048")},
			Capacity: fixedCap("8"),
			LineIDs:  []string{"li"},
		},
	}

	alloc := allocateCommitments(stack, []windowUsage{
		{Start: t0, ByLine: map[string]decimal.Decimal{"li": dec("10")}},
		{Start: t0.Add(time.Minute), ByLine: map[string]decimal.Decimal{"li": dec("5")}},
		{Start: t0.Add(2 * time.Minute), ByLine: map[string]decimal.Decimal{"li": dec("8")}},
	})
	lines, err := buildCommitmentLines(commitmentLineInput{
		Stack: stack,
		Alloc: alloc,
		Lines: map[string]*scopedLine{
			"li": {
				Item:     li,
				Price:    p,
				Quantity: dec("23"),
				Cost:     dec("1.38"),
			},
		},
		PeriodByCmt: map[string]commitmentPeriod{
			"cmt_gpu": {
				Capacity: dec("24"),
				Amount:   dec("1.152"),
			},
		},
		PeriodStart: t0,
		PeriodEnd:   t0.AddDate(0, 1, 0),
		Currency:    "usd",
		PriceCost:   perUnitCost,
	})
	require.NoError(t, err)

	within := linesByKind(lines, types.CommitmentLineWithin)[0]
	require.True(t, within.Quantity.Equal(dec("21")))
	require.True(t, within.Amount.Equal(dec("1.01"))) // 21 × 0.048 = 1.008 → currency precision

	trueUp := linesByKind(lines, types.CommitmentLineTrueUp)[0]
	require.True(t, trueUp.Quantity.Equal(dec("3")))
	require.Equal(t, "price_committed", lo.FromPtr(trueUp.PriceID))

	onDemand := linesByKind(lines, types.CommitmentLineOnDemand)[0]
	require.True(t, onDemand.Quantity.Equal(dec("2")))
	require.True(t, onDemand.Amount.Equal(dec("0.12")))
}

func TestBuildCommitmentLines_ConservationViolation(t *testing.T) {
	in := amountInput("120", true)
	in.Lines["li_api"].Cost = dec("500") // allocation saw 120; the line says 500

	_, err := buildCommitmentLines(in)
	require.Error(t, err)
}

func TestBuildCommitmentLines_RoundingReconcilesToPeriodAmount(t *testing.T) {
	t0 := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)

	// Quantity: rate 0.045, capacity 24, used 21 → within 0.95 + true-up 0.13 == 1.08
	p := &price.Price{
		ID:          "p",
		Amount:      dec("0.06"),
		DisplayName: "H100",
	}
	li := &subscription.SubscriptionLineItem{
		ID:        "li",
		PriceID:   p.ID,
		PriceType: types.PRICE_TYPE_USAGE,
	}
	c := &commitment.Commitment{
		ID:           "cmt_q",
		Type:         types.COMMITMENT_TYPE_QUANTITY,
		Value:        dec("24"),
		Binding:      true,
		InvoiceLabel: "GPU",
	}
	stack := []*stackedCommitment{
		{
			C:        c,
			Rates:    commitmentRates{Committed: dec("0.045")},
			Capacity: fixedCap("24"),
			LineIDs:  []string{"li"},
		},
	}

	alloc := allocateCommitments(stack, []windowUsage{
		{
			Start:  t0,
			ByLine: map[string]decimal.Decimal{"li": dec("21")},
		},
	})
	lines, err := buildCommitmentLines(commitmentLineInput{
		Stack: stack,
		Alloc: alloc,
		Lines: map[string]*scopedLine{
			"li": {
				Item:     li,
				Price:    p,
				Quantity: dec("21"),
				Cost:     dec("1.26"),
			},
		},
		PeriodByCmt: map[string]commitmentPeriod{
			"cmt_q": {
				Capacity: dec("24"),
				Amount:   dec("1.08"),
			},
		},
		PeriodStart: t0,
		PeriodEnd:   t0.AddDate(0, 1, 0),
		Currency:    "usd",
		PriceCost:   perUnitCost,
	})
	require.NoError(t, err)

	total := linesByKind(lines, types.CommitmentLineWithin)[0].Amount.
		Add(linesByKind(lines, types.CommitmentLineTrueUp)[0].Amount)
	require.True(t, total.Equal(dec("1.08")), total.String())

	// Amount: two lines of 33.335 against 100 → sum exactly 100.00
	mk := func(id string) (*subscription.SubscriptionLineItem, *price.Price) {
		return &subscription.SubscriptionLineItem{
				ID:        id,
				PriceID:   "p_" + id,
				PriceType: types.PRICE_TYPE_USAGE,
			}, &price.Price{
				ID:     "p_" + id,
				Amount: dec("1"),
			}
	}
	li1, p1 := mk("a")
	li2, p2 := mk("b")
	ca := &commitment.Commitment{
		ID:           "cmt_a",
		Type:         types.COMMITMENT_TYPE_AMOUNT,
		Value:        dec("100"),
		Binding:      true,
		InvoiceLabel: "Plat",
	}
	stack = []*stackedCommitment{
		{
			C:        ca,
			Rates:    commitmentRates{Committed: dec("1")},
			Capacity: fixedCap("100"),
			LineIDs:  []string{"a", "b"},
		},
	}

	use := map[string]decimal.Decimal{
		"a": dec("33.335"),
		"b": dec("33.335"),
	}
	alloc = allocateCommitments(stack, []windowUsage{{Start: t0, ByLine: use}})
	lines, err = buildCommitmentLines(commitmentLineInput{
		Stack: stack,
		Alloc: alloc,
		Lines: map[string]*scopedLine{
			"a": {
				Item:     li1,
				Price:    p1,
				Quantity: dec("33.335"),
				Cost:     dec("33.335"),
			},
			"b": {
				Item:     li2,
				Price:    p2,
				Quantity: dec("33.335"),
				Cost:     dec("33.335"),
			},
		},
		PeriodByCmt: map[string]commitmentPeriod{
			"cmt_a": {
				Amount:   dec("100"),
				Capacity: dec("100"),
			},
		},
		PeriodStart: t0,
		PeriodEnd:   t0.AddDate(0, 1, 0),
		Currency:    "usd",
		PriceCost:   perUnitCost,
	})
	require.NoError(t, err)

	sum := decimal.Zero
	for _, l := range lines {
		sum = sum.Add(l.Amount)
	}
	require.True(t, sum.Equal(dec("100")), sum.String())
}

func TestBuildCommitmentLines_OnDemandOmitsInvoiceDescription(t *testing.T) {
	in := amountInput("120", true)
	in.Stack[0].C.InvoiceDescription = lo.ToPtr("desc")

	lines, err := buildCommitmentLines(in)
	require.NoError(t, err)
	require.Equal(t, "desc", linesByKind(lines, types.CommitmentLineWithin)[0].Metadata[types.MetadataKeyDescription])

	od := linesByKind(lines, types.CommitmentLineOnDemand)[0]
	require.NotContains(t, od.Metadata, types.MetadataKeyDescription)
	require.Equal(t, "cmt_1", lo.FromPtr(od.CommitmentID))

	in = amountInput("60", true)
	in.Stack[0].C.InvoiceDescription = lo.ToPtr("desc")

	lines, err = buildCommitmentLines(in)
	require.NoError(t, err)
	require.Equal(t, "desc", linesByKind(lines, types.CommitmentLineTrueUp)[0].Metadata[types.MetadataKeyDescription])
}

func TestBuildCommitmentLines_RoundingNeverExceedsPeriodAmount(t *testing.T) {
	// 24 units at 0.045 split 7/7/10: lines round to 0.32 + 0.32 + 0.45 = 1.09 against a 1.08 commitment
	t0 := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	usage := map[string]decimal.Decimal{
		"a": dec("7"),
		"b": dec("7"),
		"c": dec("10"),
	}

	lines := map[string]*scopedLine{}
	for id, qty := range usage {
		lines[id] = &scopedLine{
			Item: &subscription.SubscriptionLineItem{
				ID:        id,
				PriceID:   "p_" + id,
				PriceType: types.PRICE_TYPE_USAGE,
			},
			Price: &price.Price{
				ID:     "p_" + id,
				Amount: dec("0.06"),
			},
			Quantity: qty,
			Cost:     qty.Mul(dec("0.06")),
		}
	}

	stack := []*stackedCommitment{
		{
			C: &commitment.Commitment{
				ID:           "cmt_q",
				Type:         types.COMMITMENT_TYPE_QUANTITY,
				Value:        dec("24"),
				Binding:      true,
				InvoiceLabel: "GPU",
			},
			Rates:    commitmentRates{Committed: dec("0.045")},
			Capacity: fixedCap("24"),
			LineIDs:  []string{"a", "b", "c"},
		},
	}

	out, err := buildCommitmentLines(commitmentLineInput{
		Stack: stack,
		Alloc: allocateCommitments(stack, []windowUsage{{Start: t0, ByLine: usage}}),
		Lines: lines,
		PeriodByCmt: map[string]commitmentPeriod{
			"cmt_q": {
				Capacity: dec("24"),
				Amount:   dec("1.08"),
			},
		},
		PeriodStart: t0,
		PeriodEnd:   t0.AddDate(0, 1, 0),
		Currency:    "usd",
		PriceCost:   perUnitCost,
	})
	require.NoError(t, err)
	require.Empty(t, linesByKind(out, types.CommitmentLineTrueUp))

	total := decimal.Zero
	for _, l := range linesByKind(out, types.CommitmentLineWithin) {
		total = total.Add(l.Amount)
	}
	require.True(t, total.Equal(dec("1.08")), total.String())
}

func gpuLine(id string) *scopedLine {
	return &scopedLine{
		Item: &subscription.SubscriptionLineItem{
			ID:          id,
			PriceID:     "p_" + id,
			PriceType:   types.PRICE_TYPE_USAGE,
			DisplayName: "H100",
		},
		Price: &price.Price{
			ID:     "p_" + id,
			Amount: dec("1"),
		},
	}
}

func TestBuildCommitmentLines_OnDemandPricedPerWindow(t *testing.T) {
	// $2 for the first 10 units, $1 after: two windows of 10 on-demand cost 20 + 20, not 30 as one lump
	tiered := func(_ *price.Price, qty decimal.Decimal) decimal.Decimal {
		first := decimal.Min(qty, dec("10"))
		return first.Mul(dec("2")).Add(qty.Sub(first))
	}

	t0 := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	stack := []*stackedCommitment{
		{
			C: &commitment.Commitment{
				ID:           "cmt_w",
				Type:         types.COMMITMENT_TYPE_QUANTITY,
				Measurement:  types.CommitmentMeasurementWindow,
				Value:        dec("5"),
				InvoiceLabel: "GPU",
			},
			Rates:    commitmentRates{Committed: dec("1")},
			Capacity: fixedCap("5"),
			LineIDs:  []string{"li"},
		},
	}

	line := gpuLine("li")
	line.Quantity = dec("30")
	out, err := buildCommitmentLines(commitmentLineInput{
		Stack: stack,
		Alloc: allocateCommitments(stack, []windowUsage{
			{Start: t0, ByLine: map[string]decimal.Decimal{"li": dec("15")}},
			{Start: t0.Add(time.Hour), ByLine: map[string]decimal.Decimal{"li": dec("15")}},
		}),
		Lines: map[string]*scopedLine{
			"li": line,
		},
		PeriodByCmt: map[string]commitmentPeriod{},
		PeriodStart: t0,
		PeriodEnd:   t0.AddDate(0, 1, 0),
		Currency:    "usd",
		PriceCost:   tiered,
	})
	require.NoError(t, err)

	onDemand := linesByKind(out, types.CommitmentLineOnDemand)
	require.Len(t, onDemand, 1)
	require.True(t, onDemand[0].Quantity.Equal(dec("20")))
	require.True(t, onDemand[0].Amount.Equal(dec("40")), onDemand[0].Amount.String())
}

func TestBuildCommitmentLines_OnDemandAtOverageOfCommitmentInForce(t *testing.T) {
	// The old commitment is terminated after the first window and replaced with a different overage rate.
	t0 := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	switchAt := t0.Add(time.Hour)
	mk := func(id string, start time.Time, terminated *time.Time, overage string) *stackedCommitment {
		c := &commitment.Commitment{
			ID:           id,
			Type:         types.COMMITMENT_TYPE_QUANTITY,
			Measurement:  types.CommitmentMeasurementWindow,
			Value:        dec("8"),
			InvoiceLabel: id,
			StartDate:    start,
			EndDate:      t0.AddDate(0, 1, 0),
			TerminatedAt: terminated,
		}

		return &stackedCommitment{
			C: c,
			Rates: commitmentRates{
				Committed: dec("1"),
				Overage:   lo.ToPtr(dec(overage)),
			},
			Capacity: func(t time.Time) decimal.Decimal {
				if c.IsActiveAt(t) {
					return c.Value
				}

				return decimal.Zero
			},
			LineIDs: []string{"li"},
		}
	}

	stack := []*stackedCommitment{
		mk("cmt_old", t0, &switchAt, "2"),
		mk("cmt_new", switchAt, nil, "3"),
	}
	sortCommitmentStack(stack)

	line := gpuLine("li")
	line.Quantity = dec("23")
	out, err := buildCommitmentLines(commitmentLineInput{
		Stack: stack,
		Alloc: allocateCommitments(stack, []windowUsage{
			{Start: t0, ByLine: map[string]decimal.Decimal{"li": dec("10")}},
			{Start: switchAt, ByLine: map[string]decimal.Decimal{"li": dec("13")}},
		}),
		Lines: map[string]*scopedLine{
			"li": line,
		},
		PeriodByCmt: map[string]commitmentPeriod{},
		PeriodStart: t0,
		PeriodEnd:   t0.AddDate(0, 1, 0),
		Currency:    "usd",
		PriceCost:   perUnitCost,
	})
	require.NoError(t, err)

	onDemand := linesByKind(out, types.CommitmentLineOnDemand)
	require.Len(t, onDemand, 2)
	require.Equal(t, "cmt_old", lo.FromPtr(onDemand[0].CommitmentID))
	require.True(t, onDemand[0].Amount.Equal(dec("4")), onDemand[0].Amount.String()) // 2 × 2
	require.Equal(t, "cmt_new", lo.FromPtr(onDemand[1].CommitmentID))
	require.True(t, onDemand[1].Amount.Equal(dec("15")), onDemand[1].Amount.String()) // 5 × 3
}

func TestBuildCommitmentLines_FreeUsageKeepsItsQuantity(t *testing.T) {
	in := amountInput("60", true)
	free := gpuLine("li_free")
	free.Quantity = dec("50")
	in.Lines["li_free"] = free
	in.Stack[0].LineIDs = append(in.Stack[0].LineIDs, "li_free")

	out, err := buildCommitmentLines(in)
	require.NoError(t, err)

	onDemand := linesByKind(out, types.CommitmentLineOnDemand)
	require.Len(t, onDemand, 1)
	require.Equal(t, "li_free", lo.FromPtr(onDemand[0].SubscriptionLineItemID))
	require.True(t, onDemand[0].Quantity.Equal(dec("50")))
	require.True(t, onDemand[0].Amount.IsZero())
	require.True(t, linesByKind(out, types.CommitmentLineTrueUp)[0].Amount.Equal(dec("40")))
}

func TestBuildCommitmentLines_BillingPeriodSplitAtReplacement(t *testing.T) {
	// A is terminated on the 15th and replaced by B with a different overage rate; each half bills against its own commitment.
	t0 := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	switchAt := time.Date(2027, 1, 15, 0, 0, 0, 0, time.UTC)
	mk := func(id string, start time.Time, terminated *time.Time, pool string, overage string) *stackedCommitment {
		c := &commitment.Commitment{
			ID:           id,
			Type:         types.COMMITMENT_TYPE_QUANTITY,
			Measurement:  types.CommitmentMeasurementBillingPeriod,
			Value:        dec(pool),
			InvoiceLabel: id,
			StartDate:    start,
			EndDate:      t0.AddDate(0, 1, 0),
			TerminatedAt: terminated,
		}

		return &stackedCommitment{
			C: c,
			Rates: commitmentRates{
				Committed: dec("1"),
				Overage:   lo.ToPtr(dec(overage)),
			},
			Capacity: inForceCap(c, pool),
			LineIDs:  []string{"li"},
		}
	}

	stack := []*stackedCommitment{
		mk("cmt_a", t0, &switchAt, "12", "2"),
		mk("cmt_b", switchAt, nil, "8", "3"),
	}
	sortCommitmentStack(stack)

	line := gpuLine("li")
	line.Quantity = dec("33")
	out, err := buildCommitmentLines(commitmentLineInput{
		Stack: stack,
		Alloc: allocateCommitments(stack, []windowUsage{
			{Start: t0, ByLine: map[string]decimal.Decimal{"li": dec("20")}},
			{Start: switchAt, ByLine: map[string]decimal.Decimal{"li": dec("13")}},
		}),
		Lines: map[string]*scopedLine{
			"li": line,
		},
		PeriodByCmt: map[string]commitmentPeriod{},
		PeriodStart: t0,
		PeriodEnd:   t0.AddDate(0, 1, 0),
		Currency:    "usd",
		PriceCost:   perUnitCost,
	})
	require.NoError(t, err)

	within := linesByKind(out, types.CommitmentLineWithin)
	require.Len(t, within, 2)
	require.Equal(t, "cmt_a", lo.FromPtr(within[0].CommitmentID))
	require.True(t, within[0].Quantity.Equal(dec("12")), within[0].Quantity.String())
	require.Equal(t, "cmt_b", lo.FromPtr(within[1].CommitmentID))
	require.True(t, within[1].Quantity.Equal(dec("8")), within[1].Quantity.String())

	onDemand := linesByKind(out, types.CommitmentLineOnDemand)
	require.Len(t, onDemand, 2)
	require.Equal(t, "cmt_a", lo.FromPtr(onDemand[0].CommitmentID))
	require.True(t, onDemand[0].Amount.Equal(dec("16")), onDemand[0].Amount.String()) // 8 × 2
	require.Equal(t, "cmt_b", lo.FromPtr(onDemand[1].CommitmentID))
	require.True(t, onDemand[1].Amount.Equal(dec("15")), onDemand[1].Amount.String()) // 5 × 3
}

func TestBuildCommitmentLines_BillingPeriodOnDemandPricedOnce(t *testing.T) {
	// $2 for the first 10 units, $1 after: a billing-period split does not restart the tiers for one owner.
	tiered := func(_ *price.Price, qty decimal.Decimal) decimal.Decimal {
		first := decimal.Min(qty, dec("10"))
		return first.Mul(dec("2")).Add(qty.Sub(first))
	}

	t0 := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	c := &commitment.Commitment{
		ID:           "cmt_c",
		Type:         types.COMMITMENT_TYPE_QUANTITY,
		Measurement:  types.CommitmentMeasurementBillingPeriod,
		Value:        dec("5"),
		InvoiceLabel: "C",
		StartDate:    t0,
		EndDate:      t0.AddDate(0, 1, 0),
	}
	stack := []*stackedCommitment{
		{
			C:        c,
			Rates:    commitmentRates{Committed: dec("1")},
			Capacity: inForceCap(c, "5"),
			LineIDs:  []string{"li"},
		},
	}

	line := gpuLine("li")
	line.Quantity = dec("30")
	out, err := buildCommitmentLines(commitmentLineInput{
		Stack: stack,
		Alloc: allocateCommitments(stack, []windowUsage{
			{Start: t0, ByLine: map[string]decimal.Decimal{"li": dec("15")}},
			{Start: t0.AddDate(0, 0, 14), ByLine: map[string]decimal.Decimal{"li": dec("15")}},
		}),
		Lines: map[string]*scopedLine{
			"li": line,
		},
		PeriodByCmt: map[string]commitmentPeriod{},
		PeriodStart: t0,
		PeriodEnd:   t0.AddDate(0, 1, 0),
		Currency:    "usd",
		PriceCost:   tiered,
	})
	require.NoError(t, err)

	onDemand := linesByKind(out, types.CommitmentLineOnDemand)
	require.Len(t, onDemand, 1)
	require.True(t, onDemand[0].Quantity.Equal(dec("25")))
	require.True(t, onDemand[0].Amount.Equal(dec("35")), onDemand[0].Amount.String())
}
