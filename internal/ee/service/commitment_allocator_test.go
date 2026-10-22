package service

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/domain/commitment"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func fixedCap(v string) func(time.Time) decimal.Decimal {
	return func(time.Time) decimal.Decimal {
		return dec(v)
	}
}

func onDemandOf(res *allocationResult, lineID string) decimal.Decimal {
	total := decimal.Zero
	for _, od := range res.OnDemand {
		if od.LineID == lineID {
			total = total.Add(od.Quantity)
		}
	}

	return total
}

func TestAllocateCommitments_Waterfall(t *testing.T) {
	t0 := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	newStack := func() []*stackedCommitment {
		return []*stackedCommitment{
			{
				C: &commitment.Commitment{
					ID:        "cmt_b",
					Priority:  2,
					StartDate: t0,
				},
				Capacity: fixedCap("10"),
				LineIDs:  []string{"li_gpu"},
			},
			{
				C: &commitment.Commitment{
					ID:        "cmt_a",
					Priority:  1,
					StartDate: t0,
				},
				Capacity: fixedCap("10"),
				LineIDs:  []string{"li_gpu"},
			},
		}
	}

	tests := []struct {
		usage        string
		wantA        string
		wantB        string
		wantOnDemand string
	}{
		{"6", "6", "0", "0"},
		{"15", "10", "5", "0"},
		{"24", "10", "10", "4"},
	}
	for _, tc := range tests {
		t.Run("usage "+tc.usage, func(t *testing.T) {
			stack := newStack()
			sortCommitmentStack(stack)

			res := allocateCommitments(stack, []windowUsage{
				{
					Start:  t0,
					ByLine: map[string]decimal.Decimal{"li_gpu": dec(tc.usage)},
				},
			})

			require.True(t, res.ByCommitment["cmt_a"].UsedByLine["li_gpu"].Equal(dec(tc.wantA)))
			require.True(t, res.ByCommitment["cmt_b"].UsedByLine["li_gpu"].Equal(dec(tc.wantB)))
			require.True(t, onDemandOf(res, "li_gpu").Equal(dec(tc.wantOnDemand)))
			require.True(t, res.ByCommitment["cmt_a"].Unused.Equal(dec("10").Sub(dec(tc.wantA))))
		})
	}
}

func TestAllocateCommitments_InactiveWindowSkipped(t *testing.T) {
	t0 := time.Date(2027, 1, 1, 3, 0, 0, 0, time.UTC)
	stack := []*stackedCommitment{
		{
			C: &commitment.Commitment{
				ID:       "cmt_a",
				Priority: 1,
			},
			Capacity: fixedCap("8"),
			LineIDs:  []string{"li"},
		},
		{
			C: &commitment.Commitment{
				ID:       "cmt_b",
				Priority: 2,
			},
			Capacity: fixedCap("0"), // outside its hours
			LineIDs:  []string{"li"},
		},
	}

	res := allocateCommitments(stack, []windowUsage{
		{
			Start:  t0,
			ByLine: map[string]decimal.Decimal{"li": dec("12")},
		},
	})

	require.True(t, res.ByCommitment["cmt_a"].UsedByLine["li"].Equal(dec("8")))
	require.True(t, res.ByCommitment["cmt_b"].UsedByLine["li"].IsZero())
	require.True(t, onDemandOf(res, "li").Equal(dec("4")))
}

func TestAllocateCommitments_PooledFillsLinesInOrder(t *testing.T) {
	t0 := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	stack := []*stackedCommitment{
		{
			C:        &commitment.Commitment{ID: "cmt"},
			Capacity: fixedCap("100"),
			LineIDs:  []string{"li_api", "li_storage"},
		},
	}

	res := allocateCommitments(stack, []windowUsage{
		{
			Start: t0,
			ByLine: map[string]decimal.Decimal{
				"li_api":     dec("70"),
				"li_storage": dec("50"),
			},
		},
	})

	require.True(t, res.ByCommitment["cmt"].UsedByLine["li_api"].Equal(dec("70")))
	require.True(t, res.ByCommitment["cmt"].UsedByLine["li_storage"].Equal(dec("30")))
	require.True(t, onDemandOf(res, "li_storage").Equal(dec("20")))
	require.True(t, onDemandOf(res, "li_api").IsZero())
}

func TestAllocateCommitments_QuantityConservation(t *testing.T) {
	t0 := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	stack := []*stackedCommitment{
		{
			C:        &commitment.Commitment{ID: "c"},
			Capacity: fixedCap("8"),
			LineIDs:  []string{"li"},
		},
	}
	windows := []windowUsage{
		{Start: t0, ByLine: map[string]decimal.Decimal{"li": dec("10")}},
		{Start: t0.Add(time.Minute), ByLine: map[string]decimal.Decimal{"li": dec("5")}},
		{Start: t0.Add(2 * time.Minute), ByLine: map[string]decimal.Decimal{"li": dec("8")}},
	}

	res := allocateCommitments(stack, windows)

	require.True(t, res.ByCommitment["c"].UsedByLine["li"].Equal(dec("21")))
	require.True(t, res.ByCommitment["c"].Unused.Equal(dec("3")))
	require.True(t, onDemandOf(res, "li").Equal(dec("2")))
	require.True(t, res.ByCommitment["c"].UsedByLine["li"].Add(onDemandOf(res, "li")).Equal(dec("23")))
}

func TestAllocateCommitments_OnDemandOwnerPerWindow(t *testing.T) {
	t0 := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	switchAt := t0.Add(time.Hour)
	terminated := &stackedCommitment{
		C: &commitment.Commitment{
			ID:           "cmt_old",
			Priority:     1,
			StartDate:    t0,
			EndDate:      t0.AddDate(0, 1, 0),
			TerminatedAt: &switchAt,
		},
		LineIDs: []string{"li"},
	}
	replacement := &stackedCommitment{
		C: &commitment.Commitment{
			ID:        "cmt_new",
			Priority:  1,
			StartDate: switchAt,
			EndDate:   t0.AddDate(0, 1, 0),
		},
		LineIDs: []string{"li"},
	}
	for _, sc := range []*stackedCommitment{terminated, replacement} {
		c := sc.C
		sc.Capacity = func(t time.Time) decimal.Decimal {
			if c.IsActiveAt(t) {
				return dec("8")
			}

			return decimal.Zero
		}
	}

	stack := []*stackedCommitment{terminated, replacement}
	sortCommitmentStack(stack)

	res := allocateCommitments(stack, []windowUsage{
		{Start: t0, ByLine: map[string]decimal.Decimal{"li": dec("10")}},
		{Start: switchAt, ByLine: map[string]decimal.Decimal{"li": dec("13")}},
	})

	require.Len(t, res.OnDemand, 2)
	require.Equal(t, "cmt_old", res.OnDemand[0].Owner.C.ID)
	require.True(t, res.OnDemand[0].Quantity.Equal(dec("2")))
	require.Equal(t, "cmt_new", res.OnDemand[1].Owner.C.ID)
	require.True(t, res.OnDemand[1].Quantity.Equal(dec("5")))
}

// inForceCap gives a billing-period commitment its whole period capacity in every window where it is in force.
func inForceCap(c *commitment.Commitment, v string) func(time.Time) decimal.Decimal {
	return func(t time.Time) decimal.Decimal {
		if c.IsActiveAt(t) {
			return dec(v)
		}

		return decimal.Zero
	}
}

func TestAllocateCommitments_BillingPeriodSplitAtReplacement(t *testing.T) {
	t0 := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	switchAt := time.Date(2027, 1, 15, 0, 0, 0, 0, time.UTC)
	end := t0.AddDate(0, 1, 0)
	old := &commitment.Commitment{
		ID:           "cmt_a",
		Priority:     1,
		Measurement:  types.CommitmentMeasurementBillingPeriod,
		StartDate:    t0,
		EndDate:      end,
		TerminatedAt: &switchAt,
	}
	replacement := &commitment.Commitment{
		ID:          "cmt_b",
		Priority:    1,
		Measurement: types.CommitmentMeasurementBillingPeriod,
		StartDate:   switchAt,
		EndDate:     end,
	}
	stack := []*stackedCommitment{
		{
			C:        old,
			Capacity: inForceCap(old, "12"),
			LineIDs:  []string{"li"},
		},
		{
			C:        replacement,
			Capacity: inForceCap(replacement, "8"),
			LineIDs:  []string{"li"},
		},
	}
	sortCommitmentStack(stack)

	res := allocateCommitments(stack, []windowUsage{
		{Start: t0, ByLine: map[string]decimal.Decimal{"li": dec("10")}},
		{Start: switchAt, ByLine: map[string]decimal.Decimal{"li": dec("13")}},
	})

	// A's 2 unused units stay unused; usage after the switch fills B and goes on-demand at B's rate.
	require.True(t, res.ByCommitment["cmt_a"].UsedByLine["li"].Equal(dec("10")))
	require.True(t, res.ByCommitment["cmt_a"].Capacity.Equal(dec("12")))
	require.True(t, res.ByCommitment["cmt_a"].Unused.Equal(dec("2")))
	require.True(t, res.ByCommitment["cmt_b"].UsedByLine["li"].Equal(dec("8")))
	require.True(t, res.ByCommitment["cmt_b"].Unused.IsZero())
	require.Len(t, res.OnDemand, 1)
	require.Equal(t, "cmt_b", res.OnDemand[0].Owner.C.ID)
	require.True(t, res.OnDemand[0].Quantity.Equal(dec("5")))
}

func TestAllocateCommitments_BillingPeriodCapacityPooledAcrossSplit(t *testing.T) {
	t0 := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	switchAt := time.Date(2027, 1, 15, 0, 0, 0, 0, time.UTC)
	end := t0.AddDate(0, 1, 0)
	mk := func(id string, priority int, start time.Time, terminated *time.Time) *commitment.Commitment {
		return &commitment.Commitment{
			ID:           id,
			Priority:     priority,
			Measurement:  types.CommitmentMeasurementBillingPeriod,
			StartDate:    start,
			EndDate:      end,
			TerminatedAt: terminated,
		}
	}

	whole := mk("cmt_c", 1, t0, nil)
	old := mk("cmt_a", 2, t0, &switchAt)
	replacement := mk("cmt_b", 2, switchAt, nil)
	stack := []*stackedCommitment{
		{
			C:        whole,
			Capacity: inForceCap(whole, "15"),
			LineIDs:  []string{"li"},
		},
		{
			C:        old,
			Capacity: inForceCap(old, "4"),
			LineIDs:  []string{"li"},
		},
		{
			C:        replacement,
			Capacity: inForceCap(replacement, "8"),
			LineIDs:  []string{"li"},
		},
	}
	sortCommitmentStack(stack)

	res := allocateCommitments(stack, []windowUsage{
		{Start: t0, ByLine: map[string]decimal.Decimal{"li": dec("10")}},
		{Start: switchAt, ByLine: map[string]decimal.Decimal{"li": dec("13")}},
	})

	// C's 15 is shared: 10 before the switch, the remaining 5 after it; B takes the next 8.
	c := res.ByCommitment["cmt_c"]
	require.True(t, c.Capacity.Equal(dec("15")), "capacity %s", c.Capacity)
	require.True(t, c.UsedByLine["li"].Equal(dec("15")), "used %s", c.UsedByLine["li"])
	require.True(t, c.Unused.IsZero(), "unused %s", c.Unused)
	require.True(t, res.ByCommitment["cmt_a"].UsedByLine["li"].IsZero())
	require.True(t, res.ByCommitment["cmt_a"].Unused.Equal(dec("4")))
	require.True(t, res.ByCommitment["cmt_b"].UsedByLine["li"].Equal(dec("8")))
	require.True(t, onDemandOf(res, "li").IsZero())
}
