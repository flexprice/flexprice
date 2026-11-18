package service

import (
	"slices"
	"sort"
	"time"

	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// billedWindow is what a line billed in one display window of a billing period.
type billedWindow struct {
	start  time.Time
	billed types.CommitmentInfo
}

// billedCharge is one charge of a breakdown: its amount and quantity. Utilized and overage follow usage; the true-up is
// unused capacity, which follows none.
type billedCharge struct {
	amount   func(*types.CommitmentInfo) *decimal.Decimal
	quantity func(*types.CommitmentInfo) *decimal.Decimal
	trueUp   bool
}

const (
	utilizedCharge = iota
	overageCharge
	trueUpCharge
)

// billedCharges are indexed by utilizedCharge, overageCharge and trueUpCharge.
var billedCharges = [...]billedCharge{
	utilizedCharge: {
		amount:   func(i *types.CommitmentInfo) *decimal.Decimal { return &i.ComputedCommitmentUtilizedAmount },
		quantity: func(i *types.CommitmentInfo) *decimal.Decimal { return &i.ComputedCommitmentUtilizedQuantity },
	},
	overageCharge: {
		amount:   func(i *types.CommitmentInfo) *decimal.Decimal { return &i.ComputedOverageAmount },
		quantity: func(i *types.CommitmentInfo) *decimal.Decimal { return &i.ComputedOverageQuantity },
	},
	trueUpCharge: {
		amount:   func(i *types.CommitmentInfo) *decimal.Decimal { return &i.ComputedTrueUpAmount },
		quantity: func(i *types.CommitmentInfo) *decimal.Decimal { return &i.ComputedTrueUpQuantity },
		trueUp:   true,
	},
}

// carriesCharge reports whether info bills or counts anything.
func carriesCharge(info *types.CommitmentInfo) bool {
	return lo.SomeBy(billedCharges[:], func(c billedCharge) bool {
		return !c.amount(info).IsZero() || !c.quantity(info).IsZero()
	})
}

// commitmentLine keys what one commitment drew from one line.
type commitmentLine struct {
	commitmentID string
	lineID       string
}

// windowPlacer lays what a period's window commitments bill each line on the display windows the waterfall drew it in:
// utilized where usage was taken, overage where it was left, true-up where capacity went unused.
type windowPlacer struct {
	currency string
	grain    types.WindowSize                     // the request's window size; empty places nothing
	end      time.Time                            // the range's end in the period; buckets from it on are not billed yet
	starts   []time.Time                          // display window starts, ascending; end stands for the buckets past it
	lines    map[string]bool                      // line item IDs a window commitment bills
	used     map[commitmentLine][]decimal.Decimal // per display window
	unused   map[string][]decimal.Decimal         // by commitment ID
	covered  map[string][]decimal.Decimal         // usage left where a commitment covers, by line item ID
	plain    map[string][]decimal.Decimal         // usage left where none does
	placed   map[string][]types.CommitmentInfo    // by line item ID, per display window
	unplaced map[string]bool                      // lines with a charge that no window drew
}

// newWindowPlacer cuts the buckets of b's window stacks into display windows of window, coarsened to the bucket as
// analytics points are. Buckets from period's end on, the rest of a projected open period, are set apart as beyond.
func newWindowPlacer(
	l *commitmentLoader,
	b *commitmentBilling,
	window types.WindowSize,
	anchor *time.Time,
	period types.Period,
) *windowPlacer {
	p := &windowPlacer{
		currency: l.sub.Currency,
		grain:    window,
		end:      period.End,
		lines:    map[string]bool{},
		used:     map[commitmentLine][]decimal.Decimal{},
		unused:   map[string][]decimal.Decimal{},
		covered:  map[string][]decimal.Decimal{},
		plain:    map[string][]decimal.Decimal{},
		placed:   map[string][]types.CommitmentInfo{},
		unplaced: map[string]bool{},
	}
	keyOf := make([][]time.Time, len(b.stacks))
	for si, stack := range b.stacks {
		if stack.Measurement != types.CommitmentMeasurementWindow || len(stack.Commitments) == 0 {
			continue
		}

		windows := newPointWindows(window.Max(l.scopeByCommitment[stack.Commitments[0].ID].bucket), anchor)
		keyOf[si] = make([]time.Time, len(stack.Windows))
		for w, t := range stack.Windows {
			keyOf[si][w] = period.End
			if t.Before(period.End) {
				keyOf[si][w] = windows.startOf(maxTime(t, period.Start))
			}
		}

		for _, line := range stack.Lines {
			p.lines[line.Item.ID] = true
		}
	}

	p.starts = lo.Uniq(slices.Concat(keyOf...))
	slices.SortFunc(p.starts, time.Time.Compare)
	index := make(map[time.Time]int, len(p.starts))
	for i, start := range p.starts {
		index[start] = i
	}

	for si, stack := range b.stacks {
		if keyOf[si] == nil {
			continue
		}

		at := func(w int) int { return index[keyOf[si][w]] }
		alloc := b.allocs[si]
		for ci, sc := range stack.Commitments {
			got := alloc.Commitments[ci]
			for _, take := range got.Takes {
				key := commitmentLine{commitmentID: sc.ID, lineID: sc.Lines[take.Line].Item.ID}
				addWeight(p.used, key, len(p.starts), at(take.Window), take.Usage)
			}

			// Each window of a window stack holds a pool of its own, so what a pool kept is its window's unused capacity.
			for w, pool := range sc.PoolOf {
				if pool != noPool {
					addWeight(p.unused, sc.ID, len(p.starts), at(w), got.PoolsLeft[pool])
				}
			}
		}

		for li, line := range stack.Lines {
			for w, left := range alloc.Overage[li] {
				if !left.IsPositive() {
					continue
				}

				target := p.plain
				if stack.covers(line, w) {
					target = p.covered
				}

				addWeight(target, line.Item.ID, len(p.starts), at(w), left)
			}
		}
	}

	return p
}

// addWeight adds v to key's weight in display window at.
func addWeight[K comparable](weights map[K][]decimal.Decimal, key K, size, at int, v decimal.Decimal) {
	w := weights[key]
	if w == nil {
		w = make([]decimal.Decimal, size)
		weights[key] = w
	}

	w[at] = w[at].Add(v)
}

// utilized places a commitment's utilized row on a line by the usage it took from the line in each window.
func (p *windowPlacer) utilized(commitmentID, lineID string, amount, quantity decimal.Decimal) {
	p.place(lineID, p.used[commitmentLine{commitmentID: commitmentID, lineID: lineID}], utilizedCharge, amount, quantity)
}

// overage places an overage or plain usage row on its line by the usage the waterfall left in each window it covers, or
// in each window it does not.
func (p *windowPlacer) overage(lineID string, kind types.CommitmentLineKind, amount, quantity decimal.Decimal) {
	weights := p.covered[lineID]
	if kind == types.CommitmentLinePlain {
		weights = p.plain[lineID]
	}

	p.place(lineID, weights, overageCharge, amount, quantity)
}

// trueUp places a line's share of a commitment's true-up by the capacity the commitment left unused in each window.
func (p *windowPlacer) trueUp(commitmentID, lineID string, amount, quantity decimal.Decimal) {
	p.place(lineID, p.unused[commitmentID], trueUpCharge, amount, quantity)
}

// place shares one row of a line over the display windows by weights: amounts rounded to the currency, quantities not.
func (p *windowPlacer) place(lineID string, weights []decimal.Decimal, charge int, amount, quantity decimal.Decimal) {
	if !p.lines[lineID] || (amount.IsZero() && quantity.IsZero()) {
		return
	}

	if !lo.SomeBy(weights, decimal.Decimal.IsPositive) {
		p.unplaced[lineID] = true
		return
	}

	placed := p.placed[lineID]
	if placed == nil {
		placed = make([]types.CommitmentInfo, len(p.starts))
		p.placed[lineID] = placed
	}

	c := billedCharges[charge]
	amounts := roundShares(allocateCostByWeight(amount, weights), p.currency)
	quantities := allocateCostByWeight(quantity, weights)
	for i := range placed {
		*c.amount(&placed[i]) = c.amount(&placed[i]).Add(amounts[i])
		*c.quantity(&placed[i]) = c.quantity(&placed[i]).Add(quantities[i])
	}
}

// windows returns where a line's charges belong, or nil when the line is not placed: a period commitment bills it,
// one of its charges fell in no window, or the request has no window size.
func (p *windowPlacer) windows(lineID string) []billedWindow {
	if p.unplaced[lineID] || p.grain == "" {
		return nil
	}

	var out []billedWindow
	for i, info := range p.placed[lineID] {
		if carriesCharge(&info) && p.starts[i].Before(p.end) {
			out = append(out, billedWindow{start: p.starts[i], billed: info})
		}
	}

	return out
}

// beyond returns what a line's buckets past the range bill. The range does not owe it yet, so it leaves the line's
// total. A line that is not placed keeps it.
func (p *windowPlacer) beyond(lineID string) types.CommitmentInfo {
	last := len(p.starts) - 1
	if last < 0 || p.unplaced[lineID] || p.placed[lineID] == nil || !p.starts[last].Equal(p.end) {
		return types.CommitmentInfo{}
	}

	return p.placed[lineID][last]
}

// placeGroupShares lays each group's period share, split by weights, on the windows the line's charges fell in:
// utilized and overage by the group's own usage there, true-up by the line's unused capacity.
func placeGroupShares(
	groups []*events.DetailedUsageAnalytic,
	shares []types.CommitmentInfo,
	weights []decimal.Decimal,
	placed []billedWindow,
	windows pointWindows,
) {
	holding := make([][]int, len(groups))
	for gi, group := range groups {
		index := newPointIndex(windows, group.Points)
		holding[gi] = make([]int, len(placed))
		for k, bw := range placed {
			holding[gi][k] = index.holding(windows.middleOf(bw.start))
		}
	}

	// natural[gi][k] is the group's part of window k's charges had each window been split on its own.
	aggregation, currency := groups[0].AggregationType, groups[0].Currency
	natural := make([][]types.CommitmentInfo, len(groups))
	for gi := range groups {
		natural[gi] = make([]types.CommitmentInfo, len(placed))
	}

	for k, bw := range placed {
		usage := make([]decimal.Decimal, len(groups))
		for gi, group := range groups {
			if j := holding[gi][k]; j >= 0 {
				usage[gi] = group.Points[j].Billed()
			}
		}

		if lo.EveryBy(usage, decimal.Decimal.IsZero) {
			usage = weights
		}

		for _, c := range billedCharges {
			by := usage
			if c.trueUp {
				by = weights
			}

			amounts := shareBilled(*c.amount(&bw.billed), by, c.trueUp, aggregation)
			quantities := shareBilled(*c.quantity(&bw.billed), by, c.trueUp, aggregation)
			for gi := range groups {
				*c.amount(&natural[gi][k]) = amounts[gi]
				*c.quantity(&natural[gi][k]) = quantities[gi]
			}
		}
	}

	for gi, group := range groups {
		// A charge the group's windows carry none of follows the line's own windows.
		parts := make([]types.CommitmentInfo, len(placed))
		spread := func(field func(*types.CommitmentInfo) *decimal.Decimal, trueUp, round bool) {
			by := lo.Map(natural[gi], func(info types.CommitmentInfo, _ int) decimal.Decimal { return *field(&info) })
			if lo.EveryBy(by, decimal.Decimal.IsZero) {
				by = lo.Map(placed, func(bw billedWindow, _ int) decimal.Decimal { return *field(&bw.billed) })
			}

			values := shareBilled(*field(&shares[gi]), by, trueUp, aggregation)
			if round {
				values = roundShares(values, currency)
			}

			for k := range parts {
				*field(&parts[k]) = values[k]
			}
		}

		for _, c := range billedCharges {
			spread(c.amount, c.trueUp, true)
			spread(c.quantity, c.trueUp, false)
		}

		for k, part := range parts {
			if !carriesCharge(&part) {
				continue
			}

			j := holding[gi][k]
			if j < 0 {
				start := placed[k].start
				group.Points = append(group.Points, events.UsageAnalyticPoint{Timestamp: start, WindowStart: start})
				j = len(group.Points) - 1
			}

			addToPoint(&group.Points[j], &part)
		}
	}
}

// shareBilled shares total by weights as splitBilled does: a true-up by positive weights only, usage charges keeping
// signed SUM corrections.
func shareBilled(total decimal.Decimal, weights []decimal.Decimal, trueUp bool, aggregation types.AggregationType) []decimal.Decimal {
	if trueUp {
		return allocateCostByWeight(total, weights)
	}

	return allocateUsageCost(total, weights, aggregation)
}

// addToPoint adds part's charges to point, and their amounts to its cost.
func addToPoint(point *events.UsageAnalyticPoint, part *types.CommitmentInfo) {
	point.ComputedCommitmentUtilizedAmount = point.ComputedCommitmentUtilizedAmount.Add(part.ComputedCommitmentUtilizedAmount)
	point.ComputedOverageAmount = point.ComputedOverageAmount.Add(part.ComputedOverageAmount)
	point.ComputedTrueUpAmount = point.ComputedTrueUpAmount.Add(part.ComputedTrueUpAmount)
	point.ComputedCommitmentUtilizedQuantity = point.ComputedCommitmentUtilizedQuantity.Add(part.ComputedCommitmentUtilizedQuantity)
	point.ComputedOverageQuantity = point.ComputedOverageQuantity.Add(part.ComputedOverageQuantity)
	point.ComputedTrueUpQuantity = point.ComputedTrueUpQuantity.Add(part.ComputedTrueUpQuantity)
	point.Cost = point.Cost.Add(part.ComputedCommitmentUtilizedAmount).
		Add(part.ComputedOverageAmount).
		Add(part.ComputedTrueUpAmount)
}

// pointIndex finds a group's point by the instants its window holds.
type pointIndex struct {
	windows pointWindows
	points  []events.UsageAnalyticPoint
	order   []int // point indexes by start
}

func newPointIndex(windows pointWindows, points []events.UsageAnalyticPoint) pointIndex {
	order := make([]int, len(points))
	for i := range order {
		order[i] = i
	}

	sort.SliceStable(order, func(a, b int) bool { return points[order[a]].Timestamp.Before(points[order[b]].Timestamp) })
	return pointIndex{windows: windows, points: points, order: order}
}

// holding returns the index of the point whose window holds t, or -1.
func (x pointIndex) holding(t time.Time) int {
	i := sort.Search(len(x.order), func(i int) bool { return x.points[x.order[i]].Timestamp.After(t) }) - 1
	if i < 0 {
		return -1
	}

	j := x.order[i]
	if t.Before(x.windows.endOf(x.points[j].Timestamp)) {
		return j
	}

	return -1
}
