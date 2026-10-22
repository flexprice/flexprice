package service

import (
	"sort"
	"time"

	"github.com/flexprice/flexprice/internal/domain/commitment"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

type stackedCommitment struct {
	C        *commitment.Commitment
	Rates    commitmentRates
	Capacity func(windowStart time.Time) decimal.Decimal // billing_period: the period's whole capacity wherever it is in force
	LineIDs  []string
}

type windowUsage struct {
	Start  time.Time
	ByLine map[string]decimal.Decimal
}

type commitmentAllocation struct {
	UsedByLine map[string]decimal.Decimal
	Capacity   decimal.Decimal
	Unused     decimal.Decimal
}

// onDemandUsage is a line's usage left in one window after every commitment there is full.
type onDemandUsage struct {
	Start    time.Time
	LineID   string
	Quantity decimal.Decimal
	Owner    *stackedCommitment // the last commitment in force in the window; its overage rate applies
}

type allocationResult struct {
	ByCommitment map[string]*commitmentAllocation
	OnDemand     []onDemandUsage
}

// sortCommitmentStack orders by priority, then start date, then ID.
func sortCommitmentStack(stack []*stackedCommitment) {
	sort.SliceStable(stack, func(i, j int) bool {
		a, b := stack[i].C, stack[j].C
		if a.Priority != b.Priority {
			return a.Priority < b.Priority
		}

		if !a.StartDate.Equal(b.StartDate) {
			return a.StartDate.Before(b.StartDate)
		}

		return a.ID < b.ID
	})
}

// allocateCommitments pours each window's usage through the stack in order as a waterfall.
// Usage left over after every commitment is full is billed on-demand.
func allocateCommitments(stack []*stackedCommitment, windows []windowUsage) *allocationResult {
	res := &allocationResult{
		ByCommitment: map[string]*commitmentAllocation{},
	}
	for _, sc := range stack {
		res.ByCommitment[sc.C.ID] = &commitmentAllocation{
			UsedByLine: map[string]decimal.Decimal{},
		}
	}

	for _, w := range windows {
		remaining := make(map[string]decimal.Decimal, len(w.ByLine))
		for id, v := range w.ByLine {
			remaining[id] = v
		}

		owners := map[string]*stackedCommitment{}
		for _, sc := range stack {
			alloc := res.ByCommitment[sc.C.ID]
			capacity := sc.Capacity(w.Start)
			if capacity.IsPositive() || sc.C.IsActiveAt(w.Start) {
				for _, lineID := range sc.LineIDs {
					owners[lineID] = sc
				}
			}

			// A billing-period capacity is one pool per invoice, drawn down across the windows where it is in force.
			pooled := sc.C.Measurement == types.CommitmentMeasurementBillingPeriod
			available := capacity
			if pooled {
				if alloc.Capacity.IsZero() {
					alloc.Capacity = capacity
					alloc.Unused = capacity
				}

				available = decimal.Min(capacity, alloc.Unused)
			}

			left := available
			for _, lineID := range sc.LineIDs {
				if !left.IsPositive() {
					break
				}

				used := decimal.Min(remaining[lineID], left)
				if !used.IsPositive() {
					continue
				}

				remaining[lineID] = remaining[lineID].Sub(used)
				alloc.UsedByLine[lineID] = alloc.UsedByLine[lineID].Add(used)
				left = left.Sub(used)
			}

			if pooled {
				alloc.Unused = alloc.Unused.Sub(available.Sub(left))
				continue
			}

			alloc.Capacity = alloc.Capacity.Add(capacity)
			alloc.Unused = alloc.Unused.Add(left)
		}

		lineIDs := lo.Keys(remaining)
		sort.Strings(lineIDs)
		for _, lineID := range lineIDs {
			if !remaining[lineID].IsPositive() {
				continue
			}

			owner := owners[lineID]
			if owner == nil {
				owner = ownerOf(stack, lineID)
			}

			res.OnDemand = append(res.OnDemand, onDemandUsage{
				Start:    w.Start,
				LineID:   lineID,
				Quantity: remaining[lineID],
				Owner:    owner,
			})
		}
	}

	return res
}
