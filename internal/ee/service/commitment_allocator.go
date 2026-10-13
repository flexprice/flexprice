package service

import (
	"sort"
	"time"

	"github.com/flexprice/flexprice/internal/domain/commitment"
	"github.com/shopspring/decimal"
)

type stackedCommitment struct {
	C        *commitment.Commitment
	Rates    commitmentRates
	Capacity func(windowStart time.Time) decimal.Decimal
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

type allocationResult struct {
	ByCommitment   map[string]*commitmentAllocation
	OnDemandByLine map[string]decimal.Decimal
}

// sortCommitmentStack orders by priority, then start date, then ID (ERD D5).
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

// allocateCommitments fills each window's usage through the stack as a waterfall; the rest is on-demand (ERD §6.4, D36).
func allocateCommitments(stack []*stackedCommitment, windows []windowUsage) *allocationResult {
	res := &allocationResult{ByCommitment: map[string]*commitmentAllocation{}, OnDemandByLine: map[string]decimal.Decimal{}}
	for _, sc := range stack {
		res.ByCommitment[sc.C.ID] = &commitmentAllocation{UsedByLine: map[string]decimal.Decimal{}}
	}
	for _, w := range windows {
		remaining := make(map[string]decimal.Decimal, len(w.ByLine))
		for id, v := range w.ByLine {
			remaining[id] = v
		}
		for _, sc := range stack {
			alloc := res.ByCommitment[sc.C.ID]
			capacity := sc.Capacity(w.Start)
			left := capacity
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
			alloc.Capacity = alloc.Capacity.Add(capacity)
			alloc.Unused = alloc.Unused.Add(left)
		}
		for id, v := range remaining {
			if v.IsPositive() {
				res.OnDemandByLine[id] = res.OnDemandByLine[id].Add(v)
			}
		}
	}
	return res
}
