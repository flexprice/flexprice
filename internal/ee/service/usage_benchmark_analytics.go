package service

import (
	"context"
	"encoding/json"
	"hash/fnv"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// pickNoFinalFirst returns true when the no-FINAL meter call should run before
// the FINAL one for this event. Uses fnv32 of the eventID modulo 2 so the
// choice is deterministic per event (re-runs stay consistent) but evenly
// distributed across many events, balancing cache-warmth bias post-hoc.
func pickNoFinalFirst(eventID string) bool {
	h := fnv.New32a()
	_, _ = h.Write([]byte(eventID))
	return h.Sum32()&1 == 0
}

// meterQueryStats accumulates server-side counters from clickhouse-go's
// native-protocol ProfileInfo / ProfileEvents callbacks across every CH query
// fired during one meter_usage analytics call (main + N points sub-queries).
type meterQueryStats struct {
	durationMs    float64
	scanRows      int64  // sum of ProfileEvents[SelectedRows]
	scanBytes     int64  // sum of ProfileEvents[SelectedBytes]   (uncompressed)
	readDiskBytes int64  // sum of ProfileEvents[ReadCompressedBytes]
	memPeakBytes  int64  // max of ProfileEvents[MemoryTrackerUsage]
	resultRows    uint64 // sum of ProfileInfo.Rows
}

// runInstrumentedMeterCall wraps the meter analytics call with ctx-attached
// ProfileEvents / ProfileInfo callbacks. The callbacks fire for every conn.Query
// the meter_usage repo makes (main aggregation + per-group points sub-queries)
// and accumulate into stats by closure capture. Wall-clock is added around the
// call. Returns once call returns; all events for the request have fired by then.
//
// This pattern relies on the meter_usage service and repo passing ctx unchanged
// down to conn.Query. Any future change there that wraps ctx without preserving
// the callbacks would silently zero these stats — watch for nofinal_scan_rows=0
// in the table as the canary.
func runInstrumentedMeterCall(
	parentCtx context.Context,
	call func(context.Context) (*dto.GetUsageAnalyticsResponse, error),
) (*dto.GetUsageAnalyticsResponse, error, meterQueryStats) {
	var stats meterQueryStats
	ctx := clickhouse.Context(parentCtx,
		clickhouse.WithProfileInfo(func(p *clickhouse.ProfileInfo) {
			if p == nil {
				return
			}
			stats.resultRows += p.Rows
		}),
		clickhouse.WithProfileEvents(func(evs []clickhouse.ProfileEvent) {
			for _, e := range evs {
				switch e.Name {
				case "SelectedRows":
					stats.scanRows += e.Value
				case "SelectedBytes":
					stats.scanBytes += e.Value
				case "ReadCompressedBytes":
					stats.readDiskBytes += e.Value
				case "MemoryTrackerUsage":
					if e.Value > stats.memPeakBytes {
						stats.memPeakBytes = e.Value
					}
				}
			}
		}),
	)
	t0 := time.Now()
	resp, err := call(ctx)
	stats.durationMs = float64(time.Since(t0).Microseconds()) / 1000.0
	return resp, err, stats
}

// processAnalyticsMessage handles analytics-kind benchmark events. It replays
// the captured request against meter_usage twice (no-FINAL and FINAL), captures
// wall-clock + server-side counters for each side, and writes one row to
// analytics_benchmark.
func (s *usageBenchmarkService) processAnalyticsMessage(ctx context.Context, msg *message.Message, evt *events.UsageBenchmarkEvent) error {
	if s.analyticsBenchRepo == nil {
		if s.Logger != nil {
			s.Logger.Info(ctx, "usage benchmark: analytics repo not wired, skipping analytics event")
		}
		return nil
	}
	if len(evt.AnalyticsRequest) == 0 {
		if s.Logger != nil {
			s.Logger.Info(ctx, "usage benchmark: analytics event missing request body, skipping")
		}
		return nil
	}

	var req dto.GetUsageAnalyticsRequest
	if err := json.Unmarshal(evt.AnalyticsRequest, &req); err != nil {
		if s.Logger != nil {
			s.Logger.Error(ctx, "usage benchmark: failed to unmarshal analytics request", "error", err)
		}
		return nil
	}

	eventID := msg.UUID
	if eventID == "" {
		eventID = types.GenerateUUID()
	}

	nofinalFirst := pickNoFinalFirst(eventID)
	firstSide := "nofinal"
	if !nofinalFirst {
		firstSide = "final"
	}

	runSide := func(useFinal bool) (*dto.GetUsageAnalyticsResponse, error, meterQueryStats) {
		return runInstrumentedMeterCall(ctx, func(callCtx context.Context) (*dto.GetUsageAnalyticsResponse, error) {
			return s.callAnalyticsMeterPipeline(callCtx, &req, useFinal)
		})
	}

	var (
		nofinalResp, finalResp   *dto.GetUsageAnalyticsResponse
		nofinalErr, finalErr     error
		nofinalStats, finalStats meterQueryStats
	)
	if nofinalFirst {
		nofinalResp, nofinalErr, nofinalStats = runSide(false)
		finalResp, finalErr, finalStats = runSide(true)
	} else {
		finalResp, finalErr, finalStats = runSide(true)
		nofinalResp, nofinalErr, nofinalStats = runSide(false)
	}

	if nofinalErr != nil && s.Logger != nil {
		s.Logger.Info(ctx, "usage benchmark: no-FINAL meter pipeline failed",
			"tenant_id", evt.TenantID, "error", nofinalErr,
		)
	}
	if finalErr != nil && s.Logger != nil {
		s.Logger.Info(ctx, "usage benchmark: FINAL meter pipeline failed",
			"tenant_id", evt.TenantID, "error", finalErr,
		)
	}

	parsed := extractRequestFields(&req, evt.AnalyticsRequest)
	startTime := evt.StartTime
	if startTime.IsZero() {
		startTime = req.StartTime
	}
	endTime := evt.EndTime
	if endTime.IsZero() {
		endTime = req.EndTime
	}

	rec := buildBenchmarkRecord(
		evt, eventID, parsed, startTime, endTime,
		nofinalResp, nofinalErr, nofinalStats,
		finalResp, finalErr, finalStats,
		firstSide,
	)

	if err := s.analyticsBenchRepo.BulkInsert(ctx, []*events.AnalyticsBenchmarkRecord{rec}); err != nil {
		if s.Logger != nil {
			s.Logger.Error(ctx, "usage benchmark: failed to insert analytics benchmark row",
				"event_id", eventID, "error", err,
			)
		}
		// Ack anyway — benchmark data is non-critical.
	}
	return nil
}

// callAnalyticsMeterPipeline invokes the meter-usage analytics service. Takes
// useFinal explicitly because the benchmark consumer needs both variants per request.
func (s *usageBenchmarkService) callAnalyticsMeterPipeline(
	ctx context.Context, req *dto.GetUsageAnalyticsRequest, useFinal bool,
) (*dto.GetUsageAnalyticsResponse, error) {
	if s.meterUsageService == nil {
		return nil, nil
	}
	params := &events.MeterUsageDetailedAnalyticsParams{
		TenantID:            types.GetTenantID(ctx),
		EnvironmentID:       types.GetEnvironmentID(ctx),
		ExternalCustomerID:  req.ExternalCustomerID,
		ExternalCustomerIDs: req.ExternalCustomerIDs,
		FeatureIDs:          req.FeatureIDs,
		StartTime:           req.StartTime,
		EndTime:             req.EndTime,
		GroupBy:             req.GroupBy,
		PropertyFilters:     req.PropertyFilters,
		Sources:             req.Sources,
		WindowSize:          req.WindowSize,
		Expand:              req.Expand,
		IncludeChildren:     req.IncludeChildren,
		UseFinal:            useFinal,
	}
	return s.meterUsageService.GetDetailedAnalytics(ctx, params)
}

// buildBenchmarkRecord assembles the single analytics_benchmark row for one
// benchmark trigger event. Pure function so the per-case tests can drive it
// directly.
func buildBenchmarkRecord(
	evt *events.UsageBenchmarkEvent,
	eventID string,
	parsed parsedRequestFields,
	startTime, endTime time.Time,
	nofinalResp *dto.GetUsageAnalyticsResponse, nofinalErr error, nofinalStats meterQueryStats,
	finalResp *dto.GetUsageAnalyticsResponse, finalErr error, finalStats meterQueryStats,
	firstSide string,
) *events.AnalyticsBenchmarkRecord {
	nofinalUsage, nofinalCost, nofinalItems := summarizeResponse(nofinalResp)
	finalUsage, finalCost, finalItems := summarizeResponse(finalResp)

	currency := ""
	if nofinalResp != nil {
		currency = nofinalResp.Currency
	}
	if currency == "" && finalResp != nil {
		currency = finalResp.Currency
	}

	usageDiff := nofinalUsage.Sub(finalUsage)
	costDiff := nofinalCost.Sub(finalCost)

	resultsMatch := uint8(0)
	if nofinalErr == nil && finalErr == nil &&
		usageDiff.IsZero() && costDiff.IsZero() &&
		nofinalItems == finalItems {
		resultsMatch = 1
	}

	return &events.AnalyticsBenchmarkRecord{
		TenantID:      evt.TenantID,
		EnvironmentID: evt.EnvironmentID,
		EventID:       eventID,
		StartTime:     startTime,
		EndTime:       endTime,

		ExternalCustomerID:  parsed.ExternalCustomerID,
		ExternalCustomerIDs: parsed.ExternalCustomerIDs,
		FeatureIDs:          parsed.FeatureIDs,
		Sources:             parsed.Sources,
		GroupBy:             parsed.GroupBy,
		WindowSize:          parsed.WindowSize,
		Expand:              parsed.Expand,
		IncludeChildren:     parsed.IncludeChildren,
		HasPropertyFilters:  parsed.HasPropertyFilters,
		RequestJSON:         parsed.RequestJSON,

		NoFinalDurationMs:    nofinalStats.durationMs,
		NoFinalScanRows:      uint64(nofinalStats.scanRows),
		NoFinalScanBytes:     uint64(nofinalStats.scanBytes),
		NoFinalReadDiskBytes: uint64(nofinalStats.readDiskBytes),
		NoFinalMemPeakBytes:  uint64(nofinalStats.memPeakBytes),
		NoFinalResultRows:    nofinalStats.resultRows,

		FinalDurationMs:    finalStats.durationMs,
		FinalScanRows:      uint64(finalStats.scanRows),
		FinalScanBytes:     uint64(finalStats.scanBytes),
		FinalReadDiskBytes: uint64(finalStats.readDiskBytes),
		FinalMemPeakBytes:  uint64(finalStats.memPeakBytes),
		FinalResultRows:    finalStats.resultRows,

		DurationDiffMs:    finalStats.durationMs - nofinalStats.durationMs,
		ScanRowsDiff:      finalStats.scanRows - nofinalStats.scanRows,
		ScanBytesDiff:     finalStats.scanBytes - nofinalStats.scanBytes,
		ReadDiskBytesDiff: finalStats.readDiskBytes - nofinalStats.readDiskBytes,
		MemPeakDiffBytes:  finalStats.memPeakBytes - nofinalStats.memPeakBytes,

		NoFinalTotalUsage: nofinalUsage,
		FinalTotalUsage:   finalUsage,
		UsageDiff:         usageDiff,
		NoFinalTotalCost:  nofinalCost,
		FinalTotalCost:    finalCost,
		CostDiff:          costDiff,
		NoFinalItemCount:  nofinalItems,
		FinalItemCount:    finalItems,
		ResultsMatch:      resultsMatch,

		NoFinalError: errString(nofinalErr),
		FinalError:   errString(finalErr),

		FirstSide: firstSide,
		Currency:  currency,
		CreatedAt: time.Now().UTC(),
	}
}

// summarizeResponse returns (sum of items.TotalUsage, response.TotalCost, len(items)).
// Nil response → zero values.
func summarizeResponse(resp *dto.GetUsageAnalyticsResponse) (decimal.Decimal, decimal.Decimal, uint64) {
	if resp == nil {
		return decimal.Zero, decimal.Zero, 0
	}
	var usage decimal.Decimal
	for i := range resp.Items {
		usage = usage.Add(resp.Items[i].TotalUsage)
	}
	return usage, resp.TotalCost, uint64(len(resp.Items))
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// parsedRequestFields holds the fields promoted from GetUsageAnalyticsRequest
// to dedicated ClickHouse columns for SQL filtering.
type parsedRequestFields struct {
	ExternalCustomerID  string
	ExternalCustomerIDs []string
	FeatureIDs          []string
	Sources             []string
	GroupBy             []string
	WindowSize          string
	Expand              []string
	IncludeChildren     uint8
	HasPropertyFilters  uint8
	RequestJSON         string
}

func extractRequestFields(req *dto.GetUsageAnalyticsRequest, raw json.RawMessage) parsedRequestFields {
	p := parsedRequestFields{
		ExternalCustomerID:  req.ExternalCustomerID,
		ExternalCustomerIDs: req.ExternalCustomerIDs,
		FeatureIDs:          req.FeatureIDs,
		Sources:             req.Sources,
		GroupBy:             req.GroupBy,
		WindowSize:          string(req.WindowSize),
		Expand:              req.Expand,
	}
	if req.IncludeChildren {
		p.IncludeChildren = 1
	}
	if len(req.PropertyFilters) > 0 {
		p.HasPropertyFilters = 1
	}
	if len(raw) > 0 {
		p.RequestJSON = string(raw)
	}
	return p
}
