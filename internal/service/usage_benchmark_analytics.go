package service

import (
	"context"
	"encoding/json"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/types"
)

// processAnalyticsMessage handles analytics-kind benchmark events. It replays the
// captured request through both pipelines, joins the per-(feature_id, group_key)
// items, and writes one row per joined item to analytics_benchmark.
func (s *usageBenchmarkService) processAnalyticsMessage(ctx context.Context, msg *message.Message, evt *events.UsageBenchmarkEvent) error {
	if s.analyticsBenchRepo == nil {
		if s.Logger != nil {
			s.Logger.WarnwCtx(ctx, "usage benchmark: analytics repo not wired, skipping analytics event")
		}
		return nil
	}
	if len(evt.AnalyticsRequest) == 0 {
		if s.Logger != nil {
			s.Logger.WarnwCtx(ctx, "usage benchmark: analytics event missing request body, skipping")
		}
		return nil
	}

	var req dto.GetUsageAnalyticsRequest
	if err := json.Unmarshal(evt.AnalyticsRequest, &req); err != nil {
		if s.Logger != nil {
			s.Logger.ErrorwCtx(ctx, "usage benchmark: failed to unmarshal analytics request", "error", err)
		}
		return nil
	}

	featureResp, featureErr := s.callAnalyticsFeaturePipeline(ctx, req)
	meterResp, meterErr := s.callAnalyticsMeterPipeline(ctx, &req)

	if featureErr != nil && s.Logger != nil {
		s.Logger.WarnwCtx(ctx, "usage benchmark: analytics feature pipeline failed",
			"tenant_id", evt.TenantID,
			"error", featureErr,
		)
	}
	if meterErr != nil && s.Logger != nil {
		s.Logger.WarnwCtx(ctx, "usage benchmark: analytics meter pipeline failed",
			"tenant_id", evt.TenantID,
			"error", meterErr,
		)
	}

	featureItems := []dto.UsageAnalyticItem{}
	meterItems := []dto.UsageAnalyticItem{}
	currency := ""
	if featureResp != nil {
		featureItems = featureResp.Items
		currency = featureResp.Currency
	}
	if meterResp != nil {
		meterItems = meterResp.Items
		if currency == "" {
			currency = meterResp.Currency
		}
	}

	records := joinAnalyticsResults(featureItems, meterItems)
	if len(records) == 0 {
		// Nothing to write — still no-op ack.
		return nil
	}

	eventID := msg.UUID
	if eventID == "" {
		eventID = types.GenerateUUID()
	}

	parsed := extractRequestFields(&req, evt.AnalyticsRequest)
	now := time.Now().UTC()
	startTime := evt.StartTime
	if startTime.IsZero() {
		startTime = req.StartTime
	}
	endTime := evt.EndTime
	if endTime.IsZero() {
		endTime = req.EndTime
	}

	for _, r := range records {
		r.TenantID = evt.TenantID
		r.EnvironmentID = evt.EnvironmentID
		r.EventID = eventID
		r.StartTime = startTime
		r.EndTime = endTime
		r.ExternalCustomerID = parsed.ExternalCustomerID
		r.ExternalCustomerIDs = parsed.ExternalCustomerIDs
		r.FeatureIDs = parsed.FeatureIDs
		r.Sources = parsed.Sources
		r.GroupBy = parsed.GroupBy
		r.WindowSize = parsed.WindowSize
		r.Expand = parsed.Expand
		r.IncludeChildren = parsed.IncludeChildren
		r.HasPropertyFilters = parsed.HasPropertyFilters
		r.RequestJSON = parsed.RequestJSON
		r.Currency = currency
		r.CreatedAt = now
	}

	if err := s.analyticsBenchRepo.BulkInsert(ctx, records); err != nil {
		if s.Logger != nil {
			s.Logger.ErrorwCtx(ctx, "usage benchmark: failed to bulk insert analytics rows",
				"event_id", eventID,
				"rows", len(records),
				"error", err,
			)
		}
		// Ack anyway — benchmark data is non-critical.
	}
	return nil
}

// callAnalyticsFeaturePipeline invokes the feature-usage tracking analytics service.
func (s *usageBenchmarkService) callAnalyticsFeaturePipeline(ctx context.Context, req dto.GetUsageAnalyticsRequest) (*dto.GetUsageAnalyticsResponse, error) {
	if s.featureUsageTrackingService == nil {
		return nil, nil
	}
	// Pass-by-value copy: the live handler hands the same request to its target
	// service, so callers may mutate downstream — keep our copy isolated.
	reqCopy := req
	return s.featureUsageTrackingService.GetDetailedUsageAnalytics(ctx, &reqCopy)
}

// callAnalyticsMeterPipeline invokes the meter-usage analytics service mirroring the handler params.
func (s *usageBenchmarkService) callAnalyticsMeterPipeline(ctx context.Context, req *dto.GetUsageAnalyticsRequest) (*dto.GetUsageAnalyticsResponse, error) {
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
	}
	return s.meterUsageService.GetDetailedAnalytics(ctx, params)
}

// canonicalGroupKey produces a deterministic group-key string for an analytics
// item by walking Source/Sources + Properties in sorted-key order so feature and
// meter results can be joined regardless of map iteration order.
func canonicalGroupKey(item *dto.UsageAnalyticItem) string {
	if item == nil {
		return ""
	}
	parts := make([]string, 0, 1+len(item.Properties))

	// Source first. Prefer the singular Source (set when grouping by source);
	// otherwise fall back to a sorted join of Sources so the key stays stable
	// when the result type carries the multi-source variant.
	// User-derived strings are percent-encoded so the '=', '|' and ',' delimiters
	// can't appear in payload values and collide with structural separators.
	if item.Source != "" {
		parts = append(parts, "source="+url.QueryEscape(item.Source))
	} else if len(item.Sources) > 0 {
		sortedSources := make([]string, len(item.Sources))
		copy(sortedSources, item.Sources)
		sort.Strings(sortedSources)
		encoded := make([]string, len(sortedSources))
		for i, s := range sortedSources {
			encoded[i] = url.QueryEscape(s)
		}
		parts = append(parts, "sources="+strings.Join(encoded, ","))
	}

	if len(item.Properties) > 0 {
		keys := make([]string, 0, len(item.Properties))
		for k := range item.Properties {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			parts = append(parts, "properties."+url.QueryEscape(k)+"="+url.QueryEscape(item.Properties[k]))
		}
	}

	return strings.Join(parts, "|")
}

// joinAnalyticsResults outer-joins two pipelines' items by (feature_id, group_key)
// and emits one AnalyticsBenchmarkRecord per joined item. Tenant/env/event_id/
// request fields are filled by the caller.
func joinAnalyticsResults(featureItems, meterItems []dto.UsageAnalyticItem) []*events.AnalyticsBenchmarkRecord {
	type joinKey struct {
		featureID string
		groupKey  string
	}

	featureMap := make(map[joinKey]*dto.UsageAnalyticItem, len(featureItems))
	for i := range featureItems {
		k := joinKey{featureID: featureItems[i].FeatureID, groupKey: canonicalGroupKey(&featureItems[i])}
		featureMap[k] = &featureItems[i]
	}
	meterMap := make(map[joinKey]*dto.UsageAnalyticItem, len(meterItems))
	for i := range meterItems {
		k := joinKey{featureID: meterItems[i].FeatureID, groupKey: canonicalGroupKey(&meterItems[i])}
		meterMap[k] = &meterItems[i]
	}

	// Sort keys for deterministic insert order so multiple test runs / replays
	// produce the same ClickHouse ordering for a given event_id.
	seen := make(map[joinKey]struct{}, len(featureMap)+len(meterMap))
	keys := make([]joinKey, 0, len(featureMap)+len(meterMap))
	for k := range featureMap {
		if _, ok := seen[k]; !ok {
			seen[k] = struct{}{}
			keys = append(keys, k)
		}
	}
	for k := range meterMap {
		if _, ok := seen[k]; !ok {
			seen[k] = struct{}{}
			keys = append(keys, k)
		}
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].featureID != keys[j].featureID {
			return keys[i].featureID < keys[j].featureID
		}
		return keys[i].groupKey < keys[j].groupKey
	})

	records := make([]*events.AnalyticsBenchmarkRecord, 0, len(keys))
	for _, k := range keys {
		fItem, fOk := featureMap[k]
		mItem, mOk := meterMap[k]

		rec := &events.AnalyticsBenchmarkRecord{
			FeatureID: k.featureID,
			GroupKey:  k.groupKey,
		}
		switch {
		case fOk && mOk:
			rec.MatchStatus = events.AnalyticsBenchmarkMatchMatched
			rec.FeatureTotalUsage = fItem.TotalUsage
			rec.FeatureTotalCost = fItem.TotalCost
			rec.FeatureEventCount = fItem.EventCount
			rec.MeterTotalUsage = mItem.TotalUsage
			rec.MeterTotalCost = mItem.TotalCost
			rec.MeterEventCount = mItem.EventCount
		case fOk:
			rec.MatchStatus = events.AnalyticsBenchmarkMatchFeatureOnly
			rec.FeatureTotalUsage = fItem.TotalUsage
			rec.FeatureTotalCost = fItem.TotalCost
			rec.FeatureEventCount = fItem.EventCount
		case mOk:
			rec.MatchStatus = events.AnalyticsBenchmarkMatchMeterOnly
			rec.MeterTotalUsage = mItem.TotalUsage
			rec.MeterTotalCost = mItem.TotalCost
			rec.MeterEventCount = mItem.EventCount
		}
		rec.UsageDiff = rec.FeatureTotalUsage.Sub(rec.MeterTotalUsage)
		rec.CostDiff = rec.FeatureTotalCost.Sub(rec.MeterTotalCost)
		records = append(records, rec)
	}
	return records
}

// parsedRequestFields holds the fields we promote from GetUsageAnalyticsRequest
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
