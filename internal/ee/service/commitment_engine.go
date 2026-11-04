package service

import (
	"slices"
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
	*commitment.Commitment
	CommittedPrice *price.Price // nil for amount stacks
	Period         commitmentPeriod
	Lines          []*usageLine      // in fill order
	Pools          []decimal.Decimal // capacities that one or more windows draw from
	PoolOf         []int             // window index to pool index, or noPool
}

// noPool marks a window in which a commitment holds no capacity.
const noPool = -1

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
		a, b := cs[i], cs[j]
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
			return ierr.NewErrorf("commitment %s maps %d windows, stack has %d", sc.ID, len(sc.PoolOf), len(s.Windows)).
				Mark(ierr.ErrInternal)
		}

		for _, p := range sc.PoolOf {
			if p < noPool || p >= len(sc.Pools) {
				return ierr.NewErrorf("commitment %s references pool %d of %d", sc.ID, p, len(sc.Pools)).
					Mark(ierr.ErrInternal)
			}
		}

		for _, line := range sc.Lines {
			if !lo.Contains(s.Lines, line) {
				return ierr.NewErrorf("commitment %s fills a line that is not in the stack", sc.ID).
					Mark(ierr.ErrInternal)
			}
		}
	}

	return nil
}

// allocate fills the stack as a waterfall: in each window, every commitment drains its pool across its lines in order
// before the next receives anything, and usage left after the last one is overage.
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

	// What each commitment's pools still hold, and where each of its lines sits in stack.Lines.
	capacityLeft := make([][]decimal.Decimal, len(stack.Commitments))
	lineIndex := make([][]int, len(stack.Commitments))
	for ci, sc := range stack.Commitments {
		capacityLeft[ci] = slices.Clone(sc.Pools)
		lineIndex[ci] = make([]int, len(sc.Lines))
		for li, line := range sc.Lines {
			lineIndex[ci][li] = lo.IndexOf(stack.Lines, line)
		}

		out.Commitments[ci] = commitmentAllocation{
			Used:       decimal.Zero,
			UsedByLine: make([]decimal.Decimal, len(sc.Lines)),
			Capacity:   decimal.Sum(decimal.Zero, sc.Pools...),
		}
	}

	for w := range stack.Windows {
		for ci, sc := range stack.Commitments {
			if sc.PoolOf[w] == noPool {
				continue
			}

			pool := &capacityLeft[ci][sc.PoolOf[w]]
			got := &out.Commitments[ci]
			for li := range sc.Lines {
				if !pool.IsPositive() {
					break
				}

				usageLeft := &overage[lineIndex[ci][li]][w]
				take := decimal.Min(*pool, *usageLeft)
				if !take.IsPositive() {
					continue
				}

				*pool = pool.Sub(take)
				*usageLeft = usageLeft.Sub(take)
				got.UsedByLine[li] = got.UsedByLine[li].Add(take)
				got.Used = got.Used.Add(take)
			}
		}
	}

	for ci := range out.Commitments {
		got := &out.Commitments[ci]
		got.Unused = got.Capacity.Sub(got.Used)
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
	r := lineRenderer{
		currency:    currency,
		periodStart: periodStart,
		periodEnd:   periodEnd,
		cost:        cost,
	}
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

// commitmentLines renders a commitment's utilized lines and, when binding, its true-up to the period floor; a
// clipped period caps utilized value at its floor.
func (r lineRenderer) commitmentLines(
	stack *commitmentStack,
	sc *stackedCommitment,
	alloc commitmentAllocation,
) []dto.CreateInvoiceLineItemRequest {
	// A quantity commitment prices its whole usage once on the committed price, so tiers walk the total.
	usageValue := alloc.Used
	if stack.Type == types.COMMITMENT_TYPE_QUANTITY && alloc.Used.IsPositive() {
		usageValue = r.cost(sc.CommittedPrice, alloc.Used)
	}

	var rows []dto.CreateInvoiceLineItemRequest
	for li, line := range sc.Lines {
		share := alloc.UsedByLine[li]
		if !share.IsPositive() {
			continue
		}

		qty, amount := share, usageValue.Mul(share).Div(alloc.Used)
		if stack.Type == types.COMMITMENT_TYPE_AMOUNT {
			qty, amount = proportionalQuantity(line, share), share
		}

		rows = append(rows, r.row(sc.Commitment, line, types.CommitmentLineUtilized, sc.InvoiceLabel, qty, amount))
	}

	billed := r.absorbDrift(rows, usageValue)
	if !sc.Binding {
		return rows
	}

	floor := types.RoundToCurrencyPrecision(sc.Period.Amount, r.currency)
	if sc.Period.Clipped && billed.GreaterThan(floor) {
		// A partial floor is the period's value accrued in this window; usage valued from tier zero cannot exceed it.
		for i := range rows {
			rows[i].Amount = types.RoundToCurrencyPrecision(rows[i].Amount.Mul(floor).Div(billed), r.currency)
		}

		r.absorbDrift(rows, floor)
		return rows
	}

	trueUp := floor.Sub(billed)
	if !trueUp.IsPositive() {
		return rows
	}

	// The amount is the span floor's remainder, so the quantity is the span capacity's remainder —
	// not the as-of unused, which stops at asOf and disagrees with the amount on mid-period previews.
	trueUpQty := sc.Period.Capacity.Sub(alloc.Used)
	if stack.Type == types.COMMITMENT_TYPE_AMOUNT {
		trueUpQty = decimal.NewFromInt(1)
	}

	return append(rows, r.trueUpRow(sc.Commitment, trueUpQty, trueUp))
}

// absorbDrift reconciles individually rounded rows with their rounded target, never driving a row below zero, and
// returns the rows' final sum.
func (r lineRenderer) absorbDrift(rows []dto.CreateInvoiceLineItemRequest, target decimal.Decimal) decimal.Decimal {
	if len(rows) == 0 {
		return decimal.Zero
	}

	drift := types.RoundToCurrencyPrecision(target, r.currency).Sub(sumAmounts(rows))
	switch {
	case drift.IsPositive():
		largest := largestRow(rows)
		rows[largest].Amount = rows[largest].Amount.Add(drift)
	case drift.IsNegative():
		for excess := drift.Neg(); excess.IsPositive(); {
			largest := largestRow(rows)
			take := decimal.Min(rows[largest].Amount, excess)
			if !take.IsPositive() {
				break
			}

			rows[largest].Amount = rows[largest].Amount.Sub(take)
			excess = excess.Sub(take)
		}
	}

	return sumAmounts(rows)
}

// largestRow returns the index of the first row with the largest amount.
func largestRow(rows []dto.CreateInvoiceLineItemRequest) int {
	largest := 0
	for i := range rows {
		if rows[i].Amount.GreaterThan(rows[largest].Amount) {
			largest = i
		}
	}

	return largest
}

// sumAmounts adds up the rows' amounts.
func sumAmounts(rows []dto.CreateInvoiceLineItemRequest) decimal.Decimal {
	total := decimal.Zero
	for i := range rows {
		total = total.Add(rows[i].Amount)
	}

	return total
}

// overageLines prices one line's usage left after the whole stack was filled. Overage belongs
// to the stack, not to any one commitment, so its rows carry no commitment id.
func (r lineRenderer) overageLines(
	stack *commitmentStack,
	line *usageLine,
	left []decimal.Decimal,
) []dto.CreateInvoiceLineItemRequest {
	if stack.Type == types.COMMITMENT_TYPE_AMOUNT {
		total := decimal.Zero
		for _, value := range left {
			if value.IsPositive() {
				total = total.Add(value)
			}
		}

		switch {
		case total.IsPositive():
			return []dto.CreateInvoiceLineItemRequest{
				r.row(nil, line, types.CommitmentLineOverage, line.displayName(), proportionalQuantity(line, total), total),
			}
		case line.Cost.IsZero() && line.Quantity.IsPositive():
			// Free usage takes no capacity, but its quantity still shows on the invoice.
			return []dto.CreateInvoiceLineItemRequest{
				r.row(nil, line, types.CommitmentLineOverage, line.displayName(), line.Quantity, decimal.Zero),
			}
		}

		return nil
	}

	// Remainder in windows a commitment applies to bills at the custom rate; the rest at the line's own margin.
	listQty, listAmount, overageQty := decimal.Zero, decimal.Zero, decimal.Zero
	for w, units := range left {
		if !units.IsPositive() {
			continue
		}

		covered := lo.SomeBy(stack.Commitments, func(sc *stackedCommitment) bool {
			return sc.PoolOf[w] != noPool && lo.Contains(sc.Lines, line)
		})
		if line.OveragePrice != nil && covered {
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
		overageAmount := r.cost(line.OveragePrice, overageQty)
		rows = append(rows, r.row(nil, line, types.CommitmentLineOverage, line.displayName(), overageQty, overageAmount))
	}

	if listQty.IsPositive() {
		if !line.PerWindow {
			listAmount = r.marginalCost(line.Price, line.Quantity.Sub(overageQty), listQty)
		}

		rows = append(rows, r.row(nil, line, types.CommitmentLineOverage, line.displayName(), listQty, listAmount))
	}

	return rows
}

// marginalCost prices the top qty units of full. Only flat fee and slab tiers have a margin; other models price qty
// afresh.
func (r lineRenderer) marginalCost(p *price.Price, full, qty decimal.Decimal) decimal.Decimal {
	isSlab := p.BillingModel == types.BILLING_MODEL_TIERED && p.TierMode == types.BILLING_TIER_SLAB
	if p.BillingModel != types.BILLING_MODEL_FLAT_FEE && !isSlab {
		return r.cost(p, qty)
	}

	return r.cost(p, full).Sub(r.cost(p, full.Sub(qty)))
}

// newRow builds the fields every commitment invoice line shares; a nil c leaves the commitment out.
func (r lineRenderer) newRow(
	c *commitment.Commitment,
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

	if c == nil {
		return out
	}

	out.CommitmentID = lo.ToPtr(c.ID)
	out.Metadata[types.MetadataKeyCommitmentID] = c.ID
	if c.InvoiceDescription != nil {
		out.Metadata[types.MetadataKeyDescription] = *c.InvoiceDescription
	}

	return out
}

// row builds the invoice line of a usage line; a nil c means overage, which belongs to no commitment.
func (r lineRenderer) row(
	c *commitment.Commitment,
	line *usageLine,
	kind types.CommitmentLineKind,
	name string,
	qty, amount decimal.Decimal,
) dto.CreateInvoiceLineItemRequest {
	out := r.newRow(c, kind, name, qty, amount)
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

// trueUpRow bills what a binding commitment's usage left of its floor, as a fixed charge on its committed price.
func (r lineRenderer) trueUpRow(c *commitment.Commitment, qty, amount decimal.Decimal) dto.CreateInvoiceLineItemRequest {
	out := r.newRow(c, types.CommitmentLineTrueUp, c.InvoiceLabel+" (unused)", qty, amount)
	out.EntityType = lo.ToPtr(string(types.InvoiceLineItemEntityTypeCommitment))
	out.EntityID = lo.ToPtr(c.ID)
	out.PriceID = c.CommittedPriceID
	out.PriceType = lo.ToPtr(string(types.PRICE_TYPE_FIXED))
	return out
}
