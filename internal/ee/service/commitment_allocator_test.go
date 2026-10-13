package service

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/domain/commitment"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func fixedCap(v string) func(time.Time) decimal.Decimal {
	return func(time.Time) decimal.Decimal { return dec(v) }
}

func TestAllocateCommitments_Waterfall(t *testing.T) {
	t0 := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	newStack := func() []*stackedCommitment {
		return []*stackedCommitment{
			{C: &commitment.Commitment{ID: "cmt_b", Priority: 2, StartDate: t0}, Capacity: fixedCap("10"), LineIDs: []string{"li_gpu"}},
			{C: &commitment.Commitment{ID: "cmt_a", Priority: 1, StartDate: t0}, Capacity: fixedCap("10"), LineIDs: []string{"li_gpu"}},
		}
	}
	tests := []struct {
		usage                      string
		wantA, wantB, wantOnDemand string
	}{
		{"6", "6", "0", "0"},
		{"15", "10", "5", "0"},
		{"24", "10", "10", "4"},
	}
	for _, tc := range tests {
		t.Run("usage "+tc.usage, func(t *testing.T) {
			stack := newStack()
			sortCommitmentStack(stack)
			res := allocateCommitments(stack, []windowUsage{{Start: t0, ByLine: map[string]decimal.Decimal{"li_gpu": dec(tc.usage)}}})
			require.True(t, res.ByCommitment["cmt_a"].UsedByLine["li_gpu"].Equal(dec(tc.wantA)))
			require.True(t, res.ByCommitment["cmt_b"].UsedByLine["li_gpu"].Equal(dec(tc.wantB)))
			require.True(t, res.OnDemandByLine["li_gpu"].Equal(dec(tc.wantOnDemand)))
			require.True(t, res.ByCommitment["cmt_a"].Unused.Equal(dec("10").Sub(dec(tc.wantA))))
		})
	}
}

func TestAllocateCommitments_InactiveWindowSkipped(t *testing.T) {
	t0 := time.Date(2027, 1, 1, 3, 0, 0, 0, time.UTC)
	stack := []*stackedCommitment{
		{C: &commitment.Commitment{ID: "cmt_a", Priority: 1}, Capacity: fixedCap("8"), LineIDs: []string{"li"}},
		{C: &commitment.Commitment{ID: "cmt_b", Priority: 2}, Capacity: fixedCap("0"), LineIDs: []string{"li"}}, // outside its hours
	}
	res := allocateCommitments(stack, []windowUsage{{Start: t0, ByLine: map[string]decimal.Decimal{"li": dec("12")}}})
	require.True(t, res.ByCommitment["cmt_a"].UsedByLine["li"].Equal(dec("8")))
	require.True(t, res.ByCommitment["cmt_b"].UsedByLine["li"].IsZero())
	require.True(t, res.OnDemandByLine["li"].Equal(dec("4")))
}

func TestAllocateCommitments_PooledFillsLinesInOrder(t *testing.T) {
	t0 := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	stack := []*stackedCommitment{{C: &commitment.Commitment{ID: "cmt"}, Capacity: fixedCap("100"), LineIDs: []string{"li_api", "li_storage"}}}
	res := allocateCommitments(stack, []windowUsage{{Start: t0, ByLine: map[string]decimal.Decimal{"li_api": dec("70"), "li_storage": dec("50")}}})
	require.True(t, res.ByCommitment["cmt"].UsedByLine["li_api"].Equal(dec("70")))
	require.True(t, res.ByCommitment["cmt"].UsedByLine["li_storage"].Equal(dec("30")))
	require.True(t, res.OnDemandByLine["li_storage"].Equal(dec("20")))
	require.True(t, res.OnDemandByLine["li_api"].IsZero())
}

func TestAllocateCommitments_QuantityConservation(t *testing.T) {
	t0 := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	stack := []*stackedCommitment{{C: &commitment.Commitment{ID: "c"}, Capacity: fixedCap("8"), LineIDs: []string{"li"}}}
	windows := []windowUsage{
		{Start: t0, ByLine: map[string]decimal.Decimal{"li": dec("10")}},
		{Start: t0.Add(time.Minute), ByLine: map[string]decimal.Decimal{"li": dec("5")}},
		{Start: t0.Add(2 * time.Minute), ByLine: map[string]decimal.Decimal{"li": dec("8")}},
	}
	res := allocateCommitments(stack, windows)
	require.True(t, res.ByCommitment["c"].UsedByLine["li"].Equal(dec("21")))
	require.True(t, res.ByCommitment["c"].Unused.Equal(dec("3")))
	require.True(t, res.OnDemandByLine["li"].Equal(dec("2")))
	require.True(t, res.ByCommitment["c"].UsedByLine["li"].Add(res.OnDemandByLine["li"]).Equal(dec("23")))
}
