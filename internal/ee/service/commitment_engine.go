package service

import (
	"sort"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/commitment"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// commitmentStack is a subscription's commitments and usage over a set of windows, ready to allocate.
type commitmentStack struct {
	Type        types.CommitmentType
	Measurement types.CommitmentMeasurement
	Windows     []time.Time
	Commitments []*stackedCommitment
	Lines       []*usageLine
}

// stackedCommitment is a commitment with its capacity laid out per window.
type stackedCommitment struct {
	C              *commitment.Commitment
	CommittedPrice *price.Price // nil for amount stacks
	Period         commitmentPeriod
	Lines          []*usageLine // in fill order
	Pools          []capacityPool
	PoolOf         []int // window index to pool index, -1 when the commitment has no capacity in that window
}

// capacityPool is a capacity that one or more windows draw from.
type capacityPool struct {
	Capacity decimal.Decimal
}

// usageLine is one subscription line's usage, per window.
type usageLine struct {
	Item         *subscription.SubscriptionLineItem
	Price        *price.Price
	OveragePrice *price.Price // prices overage instead of Price when a covering commitment sets one
	Quantity     decimal.Decimal
	Cost         decimal.Decimal
	Usage        []decimal.Decimal // quantity for quantity stacks, cost for amount stacks
	PerWindow    bool              // each window is one bucket and prices on its own, as bucketed prices do
}

// allocation is the result of filling a stack.
type allocation struct {
	Commitments []commitmentAllocation // aligned with stack.Commitments
	Overage     [][]decimal.Decimal    // [line][window], aligned with stack.Lines and stack.Windows
}

// commitmentAllocation is what one commitment absorbed.
type commitmentAllocation struct {
	Used       decimal.Decimal
	UsedByLine []decimal.Decimal // aligned with the commitment's Lines
	Capacity   decimal.Decimal
	Unused     decimal.Decimal
}

// sortCommitmentStack orders commitments by priority, then earlier start, then lower id.
func sortCommitmentStack(cs []*stackedCommitment) {
	sort.SliceStable(cs, func(i, j int) bool {
		a, b := cs[i].C, cs[j].C
		if a.Priority != b.Priority {
			return a.Priority < b.Priority
		}

		if !a.StartDate.Equal(b.StartDate) {
			return a.StartDate.Before(b.StartDate)
		}

		return a.ID < b.ID
	})
}

// validate checks allocate's input contract: every line's usage is window-aligned and sums to its
// period total, and every commitment's pools and lines are consistent with the stack.
func (s *commitmentStack) validate() error {
	for i, line := range s.Lines {
		if len(line.Usage) != len(s.Windows) {
			return ierr.NewErrorf("usage line %d covers %d windows, stack has %d", i, len(line.Usage), len(s.Windows)).
				Mark(ierr.ErrInternal)
		}

		total := decimal.Zero
		for _, u := range line.Usage {
			total = total.Add(u)
		}

		want := line.Quantity
		if s.Type == types.COMMITMENT_TYPE_AMOUNT {
			want = line.Cost
		}

		if !total.Equal(want) {
			return ierr.NewErrorf("usage line %d windows sum to %s, expected %s", i, total, want).
				Mark(ierr.ErrInternal)
		}
	}

	for _, sc := range s.Commitments {
		if len(sc.PoolOf) != len(s.Windows) {
			return ierr.NewErrorf("commitment %s maps %d windows, stack has %d", sc.C.ID, len(sc.PoolOf), len(s.Windows)).
				Mark(ierr.ErrInternal)
		}

		for _, p := range sc.PoolOf {
			if p < -1 || p >= len(sc.Pools) {
				return ierr.NewErrorf("commitment %s references pool %d of %d", sc.C.ID, p, len(sc.Pools)).
					Mark(ierr.ErrInternal)
			}
		}

		for _, line := range sc.Lines {
			if !lo.Contains(s.Lines, line) {
				return ierr.NewErrorf("commitment %s fills a line that is not in the stack", sc.C.ID).
					Mark(ierr.ErrInternal)
			}
		}
	}

	return nil
}

// allocate fills the stack as a waterfall. For each window, commitments drain their pool across
// their lines in order; each is filled to capacity before the next receives anything, and whatever
// usage is left after the last one is overage.
func allocate(stack *commitmentStack) (*allocation, error) {
	if err := stack.validate(); err != nil {
		return nil, err
	}

	// What each line still has per window; whatever the waterfall leaves is overage.
	overage := make([][]decimal.Decimal, len(stack.Lines))
	for i, line := range stack.Lines {
		overage[i] = append([]decimal.Decimal(nil), line.Usage...)
	}

	out := &allocation{
		Commitments: make([]commitmentAllocation, len(stack.Commitments)),
		Overage:     overage,
	}

	remaining := make([][]decimal.Decimal, len(stack.Commitments))
	stackIndex := make([][]int, len(stack.Commitments))
	for ci, sc := range stack.Commitments {
		remaining[ci] = make([]decimal.Decimal, len(sc.Pools))
		capacity := decimal.Zero
		for pi, pool := range sc.Pools {
			remaining[ci][pi] = pool.Capacity
			capacity = capacity.Add(pool.Capacity)
		}

		stackIndex[ci] = make([]int, len(sc.Lines))
		for li, line := range sc.Lines {
			stackIndex[ci][li] = lo.IndexOf(stack.Lines, line)
		}

		out.Commitments[ci] = commitmentAllocation{
			Used:       decimal.Zero,
			UsedByLine: make([]decimal.Decimal, len(sc.Lines)),
			Capacity:   capacity,
		}
	}

	for w := range stack.Windows {
		for ci, sc := range stack.Commitments {
			if sc.PoolOf[w] < 0 {
				continue
			}

			pool := &remaining[ci][sc.PoolOf[w]]
			for li := range sc.Lines {
				if !pool.IsPositive() {
					break
				}

				left := &overage[stackIndex[ci][li]][w]
				take := decimal.Min(*pool, *left)
				if !take.IsPositive() {
					continue
				}

				*pool = pool.Sub(take)
				*left = left.Sub(take)
				out.Commitments[ci].UsedByLine[li] = out.Commitments[ci].UsedByLine[li].Add(take)
				out.Commitments[ci].Used = out.Commitments[ci].Used.Add(take)
			}
		}
	}

	for ci := range out.Commitments {
		out.Commitments[ci].Unused = out.Commitments[ci].Capacity.Sub(out.Commitments[ci].Used)
	}

	return out, nil
}

// lineCost prices a quantity of usage on a price the way the normal billing path would.
type lineCost func(p *price.Price, qty decimal.Decimal) decimal.Decimal

// proportionalQuantity converts a money value on the line into units at its average rate.
func proportionalQuantity(line *usageLine, value decimal.Decimal) decimal.Decimal {
	if line.Cost.IsZero() {
		return decimal.Zero
	}

	return line.Quantity.Mul(value).Div(line.Cost)
}

func (l *usageLine) displayName() string {
	if l.Item.DisplayName != "" {
		return l.Item.DisplayName
	}

	return l.Price.DisplayName
}

type lineRenderer struct {
	currency    string
	periodStart time.Time
	periodEnd   time.Time
	cost        lineCost
}

// commitmentInvoiceLines renders each stack and its allocation (index-aligned) as utilized, true-up
// and overage invoice lines.
func commitmentInvoiceLines(
	stacks []*commitmentStack,
	allocs []*allocation,
	currency string,
	periodStart, periodEnd time.Time,
	cost lineCost,
) []dto.CreateInvoiceLineItemRequest {
	r := lineRenderer{currency: currency, periodStart: periodStart, periodEnd: periodEnd, cost: cost}
	var out []dto.CreateInvoiceLineItemRequest
	for si, stack := range stacks {
		for ci, sc := range stack.Commitments {
			out = append(out, r.commitmentLines(stack, sc, allocs[si].Commitments[ci])...)
		}

		for li, line := range stack.Lines {
			out = append(out, r.overageLines(stack, line, allocs[si].Overage[li])...)
		}
	}

	return out
}

// commitmentLines renders one commitment's utilized lines and its true-up; for a binding commitment the rows sum
// to the rounded period amount, and a partial period's utilized value is capped there. A quantity commitment prices its
// whole usage once on the committed price, so tiers walk the total, and splits that value across lines by quantity.
func (r lineRenderer) commitmentLines(
	stack *commitmentStack,
	sc *stackedCommitment,
	used commitmentAllocation,
) []dto.CreateInvoiceLineItemRequest {
	usedValue := used.Used
	if stack.Type == types.COMMITMENT_TYPE_QUANTITY && used.Used.IsPositive() {
		usedValue = r.cost(sc.CommittedPrice, used.Used)
	}

	var rows []dto.CreateInvoiceLineItemRequest
	for li, line := range sc.Lines {
		share := used.UsedByLine[li]
		if !share.IsPositive() {
			continue
		}

		qty, amount := share, usedValue.Mul(share).Div(used.Used)
		if stack.Type == types.COMMITMENT_TYPE_AMOUNT {
			qty, amount = proportionalQuantity(line, share), share
		}

		rows = append(rows, r.row(sc.C, line, types.CommitmentLineUtilized, sc.C.InvoiceLabel, qty, amount))
	}

	utilizedValue := r.absorbDrift(rows, usedValue)
	if !sc.C.Binding {
		return rows
	}

	floor := types.RoundToCurrencyPrecision(sc.Period.Amount, r.currency)
	if sc.Period.Clipped && utilizedValue.GreaterThan(floor) {
		// A partial floor is the period's value accrued in this window; usage valued from tier zero cannot exceed it.
		for i := range rows {
			rows[i].Amount = types.RoundToCurrencyPrecision(rows[i].Amount.Mul(floor).Div(utilizedValue), r.currency)
		}

		r.absorbDrift(rows, floor)
		return rows
	}

	trueUp := floor.Sub(utilizedValue)
	if !trueUp.IsPositive() {
		return rows
	}

	// The amount is the span floor's remainder, so the quantity is the span capacity's remainder —
	// not the as-of unused, which stops at asOf and disagrees with the amount on mid-period previews.
	trueUpQty := sc.Period.Capacity.Sub(used.Used)
	if stack.Type == types.COMMITMENT_TYPE_AMOUNT {
		trueUpQty = decimal.NewFromInt(1)
	}

	row := r.row(sc.C, nil, types.CommitmentLineTrueUp, sc.C.InvoiceLabel+" (unused)", trueUpQty, trueUp)
	row.PriceID = sc.C.CommittedPriceID
	row.PriceType = lo.ToPtr(string(types.PRICE_TYPE_FIXED))
	return append(rows, row)
}

// absorbDrift reconciles individually rounded rows with their rounded exact total, never driving
// a row below zero, and returns the rows' final sum.
func (r lineRenderer) absorbDrift(rows []dto.CreateInvoiceLineItemRequest, usedValue decimal.Decimal) decimal.Decimal {
	if len(rows) == 0 {
		return decimal.Zero
	}

	sum := decimal.Zero
	for i := range rows {
		sum = sum.Add(rows[i].Amount)
	}

	drift := types.RoundToCurrencyPrecision(usedValue, r.currency).Sub(sum)
	for remaining := drift.Abs(); remaining.IsPositive(); {
		largest := 0
		for i := range rows {
			if rows[i].Amount.GreaterThan(rows[largest].Amount) {
				largest = i
			}
		}

		if drift.IsPositive() {
			rows[largest].Amount = rows[largest].Amount.Add(remaining)
			break
		}

		take := decimal.Min(rows[largest].Amount, remaining)
		if !take.IsPositive() {
			break
		}

		rows[largest].Amount = rows[largest].Amount.Sub(take)
		remaining = remaining.Sub(take)
	}

	sum = decimal.Zero
	for i := range rows {
		sum = sum.Add(rows[i].Amount)
	}

	return sum
}

// overageLines prices one line's usage left after the whole stack was filled. Overage belongs
// to the stack, not to any one commitment, so its rows carry no commitment id.
func (r lineRenderer) overageLines(
	stack *commitmentStack,
	line *usageLine,
	windows []decimal.Decimal,
) []dto.CreateInvoiceLineItemRequest {
	if stack.Type == types.COMMITMENT_TYPE_AMOUNT {
		value := decimal.Zero
		for _, units := range windows {
			if units.IsPositive() {
				value = value.Add(units)
			}
		}

		switch {
		case value.IsPositive():
			return []dto.CreateInvoiceLineItemRequest{r.row(nil, line, types.CommitmentLineOverage, line.displayName(), proportionalQuantity(line, value), value)}
		case line.Cost.IsZero() && line.Quantity.IsPositive():
			// Free usage takes no capacity, but its quantity still shows on the invoice.
			return []dto.CreateInvoiceLineItemRequest{r.row(nil, line, types.CommitmentLineOverage, line.displayName(), line.Quantity, decimal.Zero)}
		}

		return nil
	}

	// Remainder in windows a commitment applies to bills at the custom rate; the rest at the line's own margin.
	listQty, listAmount, overageQty := decimal.Zero, decimal.Zero, decimal.Zero
	for w, units := range windows {
		if !units.IsPositive() {
			continue
		}

		applied := lo.SomeBy(stack.Commitments, func(sc *stackedCommitment) bool {
			return sc.PoolOf[w] >= 0 && lo.Contains(sc.Lines, line)
		})
		if line.OveragePrice != nil && applied {
			overageQty = overageQty.Add(units)
			continue
		}

		listQty = listQty.Add(units)
		if line.PerWindow {
			listAmount = listAmount.Add(r.marginalCost(line.Price, line.Usage[w], units))
		}
	}

	var rows []dto.CreateInvoiceLineItemRequest
	if overageQty.IsPositive() {
		rows = append(rows, r.row(nil, line, types.CommitmentLineOverage, line.displayName(), overageQty, r.cost(line.OveragePrice, overageQty)))
	}

	if listQty.IsPositive() {
		if !line.PerWindow {
			listAmount = r.marginalCost(line.Price, line.Quantity.Sub(overageQty), listQty)
		}

		rows = append(rows, r.row(nil, line, types.CommitmentLineOverage, line.displayName(), listQty, listAmount))
	}

	return rows
}

// marginalCost prices the top qty units of full. A margin is well defined only for monotone marginal models (flat
// fee, slab tiers); every other model prices qty afresh, as entitlement remainders do.
func (r lineRenderer) marginalCost(p *price.Price, full, qty decimal.Decimal) decimal.Decimal {
	isSlab := p.BillingModel == types.BILLING_MODEL_TIERED && p.TierMode == types.BILLING_TIER_SLAB
	if p.BillingModel != types.BILLING_MODEL_FLAT_FEE && !isSlab {
		return r.cost(p, qty)
	}

	return r.cost(p, full).Sub(r.cost(p, full.Sub(qty)))
}

// row builds one invoice line; a nil commitment means a stack-level line with no commitment id.
func (r lineRenderer) row(
	c *commitment.Commitment,
	line *usageLine,
	kind types.CommitmentLineKind,
	name string,
	qty, amount decimal.Decimal,
) dto.CreateInvoiceLineItemRequest {
	out := dto.CreateInvoiceLineItemRequest{
		DisplayName: lo.ToPtr(name),
		Quantity:    qty,
		Amount:      types.RoundToCurrencyPrecision(amount, r.currency),
		PeriodStart: lo.ToPtr(r.periodStart),
		PeriodEnd:   lo.ToPtr(r.periodEnd),
		Metadata: types.Metadata{
			types.MetadataKeyCommitmentLineKind: string(kind),
		},
	}

	if c != nil {
		out.CommitmentID = lo.ToPtr(c.ID)
		out.Metadata[types.MetadataKeyCommitmentID] = c.ID
		if c.InvoiceDescription != nil {
			out.Metadata[types.MetadataKeyDescription] = *c.InvoiceDescription
		}
	}

	if line == nil {
		out.EntityType = lo.ToPtr(string(types.InvoiceLineItemEntityTypeCommitment))
		out.EntityID = lo.ToPtr(c.ID)
		return out
	}

	out.PriceID = lo.ToPtr(line.Item.PriceID)
	out.PriceType = lo.ToPtr(string(line.Item.PriceType))
	out.MeterID = lo.ToPtr(line.Item.MeterID)
	out.MeterDisplayName = lo.ToPtr(line.Item.MeterDisplayName)
	out.EntityID = lo.ToPtr(line.Item.EntityID)
	out.EntityType = lo.ToPtr(string(line.Item.EntityType))
	out.PlanDisplayName = lo.ToPtr(line.Item.PlanDisplayName)
	out.SubscriptionLineItemID = lo.ToPtr(line.Item.ID)
	return out
}
