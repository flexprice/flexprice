package service

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func TestPickNoFinalFirst_Deterministic(t *testing.T) {
	id := "event-stable-id"
	first := pickNoFinalFirst(id)
	for i := 0; i < 100; i++ {
		require.Equal(t, first, pickNoFinalFirst(id), "must be deterministic for same id")
	}
}

func TestPickNoFinalFirst_RoughlyBalanced(t *testing.T) {
	const n = 1000
	trueCount := 0
	for i := 0; i < n; i++ {
		if pickNoFinalFirst(fmt.Sprintf("event-id-%d", i)) {
			trueCount++
		}
	}
	require.GreaterOrEqual(t, trueCount, 400, "should be roughly half true")
	require.LessOrEqual(t, trueCount, 600, "should be roughly half true")
}

func TestBuildBenchmarkRecord_BothSucceedAndMatch(t *testing.T) {
	usage := decimal.NewFromFloat(12.5)
	cost := decimal.NewFromFloat(99.99)

	resp := &dto.GetUsageAnalyticsResponse{
		TotalCost: cost,
		Currency:  "USD",
		Items: []dto.UsageAnalyticItem{
			{TotalUsage: decimal.NewFromFloat(7.5)},
			{TotalUsage: decimal.NewFromFloat(5)},
		},
	}
	nofinalStats := meterQueryStats{durationMs: 10, scanRows: 100, scanBytes: 1024, readDiskBytes: 256, memPeakBytes: 2048, resultRows: 2}
	finalStats := meterQueryStats{durationMs: 30, scanRows: 100, scanBytes: 1024, readDiskBytes: 256, memPeakBytes: 4096, resultRows: 2}

	evt := &events.UsageBenchmarkEvent{TenantID: "t1", EnvironmentID: "e1"}
	parsed := parsedRequestFields{ExternalCustomerID: "cust1"}
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)

	rec := buildBenchmarkRecord(
		evt, "evt1", parsed, start, end,
		resp, nil, nofinalStats,
		resp, nil, finalStats,
		"nofinal",
	)

	require.Equal(t, "t1", rec.TenantID)
	require.Equal(t, "e1", rec.EnvironmentID)
	require.Equal(t, "evt1", rec.EventID)
	require.Equal(t, "cust1", rec.ExternalCustomerID)
	require.Equal(t, "USD", rec.Currency)
	require.Equal(t, "nofinal", rec.FirstSide)
	require.Equal(t, start, rec.StartTime)
	require.Equal(t, end, rec.EndTime)

	require.True(t, rec.NoFinalTotalUsage.Equal(usage), "got %s want %s", rec.NoFinalTotalUsage, usage)
	require.True(t, rec.FinalTotalUsage.Equal(usage))
	require.True(t, rec.UsageDiff.IsZero())
	require.True(t, rec.NoFinalTotalCost.Equal(cost))
	require.True(t, rec.FinalTotalCost.Equal(cost))
	require.True(t, rec.CostDiff.IsZero())
	require.Equal(t, uint64(2), rec.NoFinalItemCount)
	require.Equal(t, uint64(2), rec.FinalItemCount)
	require.Equal(t, uint8(1), rec.ResultsMatch)

	require.Equal(t, float64(10), rec.NoFinalDurationMs)
	require.Equal(t, float64(30), rec.FinalDurationMs)
	require.Equal(t, float64(20), rec.DurationDiffMs)
	require.Equal(t, int64(0), rec.ScanRowsDiff)
	require.Equal(t, int64(2048), rec.MemPeakDiffBytes)
	require.Equal(t, uint64(100), rec.NoFinalScanRows)
	require.Equal(t, uint64(4096), rec.FinalMemPeakBytes)
	require.Equal(t, uint64(2), rec.FinalResultRows)

	require.Empty(t, rec.NoFinalError)
	require.Empty(t, rec.FinalError)
}

func TestBuildBenchmarkRecord_BothSucceedWithDiff(t *testing.T) {
	nofinalResp := &dto.GetUsageAnalyticsResponse{
		TotalCost: decimal.NewFromFloat(100),
		Currency:  "USD",
		Items:     []dto.UsageAnalyticItem{{TotalUsage: decimal.NewFromFloat(10)}},
	}
	finalResp := &dto.GetUsageAnalyticsResponse{
		TotalCost: decimal.NewFromFloat(95),
		Currency:  "USD",
		Items:     []dto.UsageAnalyticItem{{TotalUsage: decimal.NewFromFloat(9)}},
	}

	rec := buildBenchmarkRecord(
		&events.UsageBenchmarkEvent{TenantID: "t1"}, "evt1", parsedRequestFields{},
		time.Time{}, time.Time{},
		nofinalResp, nil, meterQueryStats{},
		finalResp, nil, meterQueryStats{},
		"final",
	)

	require.True(t, rec.UsageDiff.Equal(decimal.NewFromFloat(1)), "10 - 9 = 1 got %s", rec.UsageDiff)
	require.True(t, rec.CostDiff.Equal(decimal.NewFromFloat(5)), "100 - 95 = 5 got %s", rec.CostDiff)
	require.Equal(t, uint8(0), rec.ResultsMatch, "diffs make results_match=0")
	require.Equal(t, "final", rec.FirstSide)
}

func TestBuildBenchmarkRecord_ItemCountMismatch(t *testing.T) {
	resp := &dto.GetUsageAnalyticsResponse{
		Items: []dto.UsageAnalyticItem{{TotalUsage: decimal.NewFromFloat(5)}},
	}
	respDouble := &dto.GetUsageAnalyticsResponse{
		Items: []dto.UsageAnalyticItem{
			{TotalUsage: decimal.NewFromFloat(2.5)},
			{TotalUsage: decimal.NewFromFloat(2.5)},
		},
	}

	rec := buildBenchmarkRecord(
		&events.UsageBenchmarkEvent{}, "evt1", parsedRequestFields{},
		time.Time{}, time.Time{},
		resp, nil, meterQueryStats{},
		respDouble, nil, meterQueryStats{},
		"nofinal",
	)

	require.True(t, rec.UsageDiff.IsZero(), "usage totals match")
	require.Equal(t, uint64(1), rec.NoFinalItemCount)
	require.Equal(t, uint64(2), rec.FinalItemCount)
	require.Equal(t, uint8(0), rec.ResultsMatch, "item count diff blocks match")
}

func TestBuildBenchmarkRecord_NoFinalFails(t *testing.T) {
	finalResp := &dto.GetUsageAnalyticsResponse{
		TotalCost: decimal.NewFromFloat(42),
		Currency:  "EUR",
	}

	rec := buildBenchmarkRecord(
		&events.UsageBenchmarkEvent{}, "evt1", parsedRequestFields{},
		time.Time{}, time.Time{},
		nil, errors.New("boom"), meterQueryStats{},
		finalResp, nil, meterQueryStats{},
		"final",
	)

	require.Equal(t, "boom", rec.NoFinalError)
	require.Empty(t, rec.FinalError)
	require.True(t, rec.NoFinalTotalCost.IsZero())
	require.True(t, rec.FinalTotalCost.Equal(decimal.NewFromFloat(42)))
	require.Equal(t, "EUR", rec.Currency)
	require.Equal(t, uint8(0), rec.ResultsMatch)
}

func TestBuildBenchmarkRecord_FinalFails(t *testing.T) {
	nofinalResp := &dto.GetUsageAnalyticsResponse{Currency: "USD"}

	rec := buildBenchmarkRecord(
		&events.UsageBenchmarkEvent{}, "evt1", parsedRequestFields{},
		time.Time{}, time.Time{},
		nofinalResp, nil, meterQueryStats{},
		nil, errors.New("kaboom"), meterQueryStats{},
		"nofinal",
	)

	require.Empty(t, rec.NoFinalError)
	require.Equal(t, "kaboom", rec.FinalError)
	require.Equal(t, "USD", rec.Currency, "falls back to no-FINAL response")
	require.Equal(t, uint8(0), rec.ResultsMatch)
}

func TestBuildBenchmarkRecord_BothFail(t *testing.T) {
	rec := buildBenchmarkRecord(
		&events.UsageBenchmarkEvent{}, "evt1", parsedRequestFields{},
		time.Time{}, time.Time{},
		nil, errors.New("a"), meterQueryStats{},
		nil, errors.New("b"), meterQueryStats{},
		"final",
	)

	require.Equal(t, "a", rec.NoFinalError)
	require.Equal(t, "b", rec.FinalError)
	require.Empty(t, rec.Currency)
	require.True(t, rec.NoFinalTotalUsage.IsZero())
	require.True(t, rec.FinalTotalUsage.IsZero())
	require.Equal(t, uint8(0), rec.ResultsMatch)
}
