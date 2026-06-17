package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// processAnalyticsMessage handles analytics-kind benchmark events. It replays the
// captured request through both pipelines, joins the per-(feature_id, group_key)
// items, and writes one row per joined item to analytics_benchmark.
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

	featureResp, featureErr := s.callAnalyticsFeaturePipeline(ctx, req)
	meterResp, meterErr := s.callAnalyticsMeterPipeline(ctx, &req)

	if featureErr != nil && s.Logger != nil {
		s.Logger.Info(ctx, "usage benchmark: analytics feature pipeline failed",
			"tenant_id", evt.TenantID,
			"error", featureErr,
		)
	}
	if meterErr != nil && s.Logger != nil {
		s.Logger.Info(ctx, "usage benchmark: analytics meter pipeline failed",
			"tenant_id", evt.TenantID,
			"error", meterErr,
		)
	}

	// Skip entirely when both pipelines failed (or returned no responses).
	// Emitting a zero-zero summary row would be noise — we have no signal.
	if featureResp == nil && meterResp == nil {
		return nil
	}

	currency := ""
	if featureResp != nil {
		currency = featureResp.Currency
	}
	if meterResp != nil && currency == "" {
		currency = meterResp.Currency
	}

	records := joinAnalyticsResults(featureResp, meterResp)
	if len(records) == 0 {
		// Defensive: joinAnalyticsResults always emits at least a summary row
		// when one side is non-nil, so this path is unreachable today. Keep it
		// for safety in case the contract changes.
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
			s.Logger.Error(ctx, "usage benchmark: failed to bulk insert analytics rows",
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

// joinAnalyticsResults compares the two pipelines' analytics responses and emits
// rows at three granularities — each answering a different debugging question:
//
//   - summary:   1 row per event_id with response.TotalCost from each side. The
//                authoritative "did billing match overall?" signal — read this
//                first, drill down only when cost_diff != 0.
//   - feature:   1 row per (feature_id, group_key) with items SUMMED across
//                line-item splits on each side. Robust to duplicate feature_ids
//                (we don't compare item-by-item, we aggregate first) so the
//                "what order are items in" question goes away.
//   - line_item: 1 row per (feature_id, sub_line_item_id, group_key) for
//                drill-down detail when a feature row has a diff.
//
// Each row also carries a diff_reason tag so spurious diffs (multi-feature-meter
// ambiguity, item collisions at this granularity) can be filtered out of the
// "real bug" view.
//
// Tenant/env/event_id/request fields are filled by the caller.
func joinAnalyticsResults(featureResp, meterResp *dto.GetUsageAnalyticsResponse) []*events.AnalyticsBenchmarkRecord {
	var featureItems, meterItems []dto.UsageAnalyticItem
	var featureTotal, meterTotal decimal.Decimal
	if featureResp != nil {
		featureItems = featureResp.Items
		featureTotal = featureResp.TotalCost
	}
	if meterResp != nil {
		meterItems = meterResp.Items
		meterTotal = meterResp.TotalCost
	}

	// Set of feature_ids per meter_id across BOTH sides. A meter mapped to
	// more than one feature signals that runtime meter→feature attribution
	// (1:1 by current design) can pick a different feature than the
	// ingest-time snapshot in feature_usage. Cost_diffs on rows for these
	// meters are an attribution artifact, not a real bug — we tag them
	// multi_feature_meter so they can be filtered out.
	meterFeatures := make(map[string]map[string]struct{})
	collectFeatures := func(items []dto.UsageAnalyticItem) {
		for i := range items {
			it := &items[i]
			if it.MeterID == "" {
				continue
			}
			set := meterFeatures[it.MeterID]
			if set == nil {
				set = make(map[string]struct{})
				meterFeatures[it.MeterID] = set
			}
			set[it.FeatureID] = struct{}{}
		}
	}
	collectFeatures(featureItems)
	collectFeatures(meterItems)
	isMultiFeatureMeter := func(meterID string) bool {
		return len(meterFeatures[meterID]) > 1
	}

	records := make([]*events.AnalyticsBenchmarkRecord, 0, 1+len(featureItems)+len(meterItems))
	records = append(records, buildSummaryRow(featureTotal, meterTotal, featureItems, meterItems))
	records = append(records, buildFeatureRows(featureItems, meterItems, isMultiFeatureMeter)...)
	records = append(records, buildLineItemRows(featureItems, meterItems, isMultiFeatureMeter)...)
	return records
}

// buildSummaryRow emits the single per-event_id row that records the response's
// root TotalCost from each pipeline verbatim. This is the only row that can be
// trusted to answer "did the two pipelines produce the same billable total?" —
// per-item rows can suffer from collisions, aggregation drift, or ordering
// artifacts at finer granularities.
func buildSummaryRow(
	featureTotal, meterTotal decimal.Decimal,
	featureItems, meterItems []dto.UsageAnalyticItem,
) *events.AnalyticsBenchmarkRecord {
	var featureUsage, meterUsage decimal.Decimal
	var featureEC, meterEC uint64
	for i := range featureItems {
		featureUsage = featureUsage.Add(featureItems[i].TotalUsage)
		featureEC += featureItems[i].EventCount
	}
	for i := range meterItems {
		meterUsage = meterUsage.Add(meterItems[i].TotalUsage)
		meterEC += meterItems[i].EventCount
	}

	rec := &events.AnalyticsBenchmarkRecord{
		RowType:           events.AnalyticsBenchmarkRowSummary,
		FeatureItemCount:  uint64(len(featureItems)),
		MeterItemCount:    uint64(len(meterItems)),
		FeatureTotalUsage: featureUsage,
		MeterTotalUsage:   meterUsage,
		FeatureTotalCost:  featureTotal,
		MeterTotalCost:    meterTotal,
		FeatureEventCount: featureEC,
		MeterEventCount:   meterEC,
	}
	rec.UsageDiff = rec.FeatureTotalUsage.Sub(rec.MeterTotalUsage)
	rec.CostDiff = rec.FeatureTotalCost.Sub(rec.MeterTotalCost)
	switch {
	case len(featureItems) > 0 && len(meterItems) > 0:
		rec.MatchStatus = events.AnalyticsBenchmarkMatchMatched
	case len(featureItems) > 0:
		rec.MatchStatus = events.AnalyticsBenchmarkMatchFeatureOnly
	case len(meterItems) > 0:
		rec.MatchStatus = events.AnalyticsBenchmarkMatchMeterOnly
	default:
		rec.MatchStatus = events.AnalyticsBenchmarkMatchMatched
	}
	// Summary rows aren't subject to the meter-attribution or item-collision
	// caveats — by construction they aggregate across everything.
	rec.DiffReason = classifyDiff(rec.CostDiff, rec.MatchStatus, false, false)
	return rec
}

// featureAgg holds a per-side accumulation for one (feature_id, group_key) bucket.
// items is kept by reference so we can read SubLineItemID/PriceID when needed.
type featureAgg struct {
	items      []*dto.UsageAnalyticItem
	usage      decimal.Decimal
	cost       decimal.Decimal
	eventCount uint64
	meterID    string
	priceIDs   []string
}

func (a *featureAgg) add(item *dto.UsageAnalyticItem) {
	a.items = append(a.items, item)
	a.usage = a.usage.Add(item.TotalUsage)
	a.cost = a.cost.Add(item.TotalCost)
	a.eventCount += item.EventCount
	if a.meterID == "" && item.MeterID != "" {
		a.meterID = item.MeterID
	}
	if item.PriceID != "" {
		// Keep insertion order but de-duplicate so the joined PriceID label
		// reflects the distinct prices that contributed.
		for _, p := range a.priceIDs {
			if p == item.PriceID {
				return
			}
		}
		a.priceIDs = append(a.priceIDs, item.PriceID)
	}
}

func (a *featureAgg) firstPriceID() string {
	if len(a.priceIDs) == 0 {
		return ""
	}
	return a.priceIDs[0]
}

// buildFeatureRows emits one row per (feature_id, group_key), aggregating all
// items on each side that share that key. This sidesteps the duplicate-feature
// ordering question — we don't pair items, we sum them.
func buildFeatureRows(
	featureItems, meterItems []dto.UsageAnalyticItem,
	isMultiFeatureMeter func(string) bool,
) []*events.AnalyticsBenchmarkRecord {
	type key struct {
		featureID string
		groupKey  string
	}
	makeKey := func(it *dto.UsageAnalyticItem) key {
		return key{featureID: it.FeatureID, groupKey: canonicalGroupKey(it)}
	}

	accumulate := func(items []dto.UsageAnalyticItem) map[key]*featureAgg {
		out := make(map[key]*featureAgg, len(items))
		for i := range items {
			k := makeKey(&items[i])
			agg, ok := out[k]
			if !ok {
				agg = &featureAgg{}
				out[k] = agg
			}
			agg.add(&items[i])
		}
		return out
	}
	featureMap := accumulate(featureItems)
	meterMap := accumulate(meterItems)

	keys := unionAndSortFeatureKeys(featureMap, meterMap)
	records := make([]*events.AnalyticsBenchmarkRecord, 0, len(keys))
	for _, k := range keys {
		fAgg, fOk := featureMap[k]
		mAgg, mOk := meterMap[k]

		rec := &events.AnalyticsBenchmarkRecord{
			RowType:   events.AnalyticsBenchmarkRowFeature,
			FeatureID: k.featureID,
			GroupKey:  k.groupKey,
		}
		var multiItem bool
		switch {
		case fOk && mOk:
			rec.MatchStatus = events.AnalyticsBenchmarkMatchMatched
			rec.MeterID = firstNonEmpty(fAgg.meterID, mAgg.meterID)
			rec.FeaturePriceID = fAgg.firstPriceID()
			rec.MeterPriceID = mAgg.firstPriceID()
			rec.FeatureItemCount = uint64(len(fAgg.items))
			rec.MeterItemCount = uint64(len(mAgg.items))
			rec.FeatureTotalUsage = fAgg.usage
			rec.FeatureTotalCost = fAgg.cost
			rec.FeatureEventCount = fAgg.eventCount
			rec.MeterTotalUsage = mAgg.usage
			rec.MeterTotalCost = mAgg.cost
			rec.MeterEventCount = mAgg.eventCount
			multiItem = len(fAgg.items) > 1 || len(mAgg.items) > 1
		case fOk:
			rec.MatchStatus = events.AnalyticsBenchmarkMatchFeatureOnly
			rec.MeterID = fAgg.meterID
			rec.FeaturePriceID = fAgg.firstPriceID()
			rec.FeatureItemCount = uint64(len(fAgg.items))
			rec.FeatureTotalUsage = fAgg.usage
			rec.FeatureTotalCost = fAgg.cost
			rec.FeatureEventCount = fAgg.eventCount
			multiItem = len(fAgg.items) > 1
		case mOk:
			rec.MatchStatus = events.AnalyticsBenchmarkMatchMeterOnly
			rec.MeterID = mAgg.meterID
			rec.MeterPriceID = mAgg.firstPriceID()
			rec.MeterItemCount = uint64(len(mAgg.items))
			rec.MeterTotalUsage = mAgg.usage
			rec.MeterTotalCost = mAgg.cost
			rec.MeterEventCount = mAgg.eventCount
			multiItem = len(mAgg.items) > 1
		}
		rec.UsageDiff = rec.FeatureTotalUsage.Sub(rec.MeterTotalUsage)
		rec.CostDiff = rec.FeatureTotalCost.Sub(rec.MeterTotalCost)
		// Feature-level multi_item means "one side had several line-item splits"
		// — informational, not a bug. Drill into line_item rows to see them.
		rec.DiffReason = classifyDiff(rec.CostDiff, rec.MatchStatus, isMultiFeatureMeter(rec.MeterID), multiItem)
		records = append(records, rec)
	}
	return records
}

// buildLineItemRows emits the granular per-(feature_id, sub_line_item_id, group_key)
// rows. Defensive against collisions (sums items at the same key rather than
// overwriting) so a stray duplicate never silently picks a winner.
func buildLineItemRows(
	featureItems, meterItems []dto.UsageAnalyticItem,
	isMultiFeatureMeter func(string) bool,
) []*events.AnalyticsBenchmarkRecord {
	type key struct {
		featureID     string
		subLineItemID string
		groupKey      string
	}
	makeKey := func(it *dto.UsageAnalyticItem) key {
		return key{
			featureID:     it.FeatureID,
			subLineItemID: it.SubLineItemID,
			groupKey:      canonicalGroupKey(it),
		}
	}

	accumulate := func(items []dto.UsageAnalyticItem) map[key]*featureAgg {
		out := make(map[key]*featureAgg, len(items))
		for i := range items {
			k := makeKey(&items[i])
			agg, ok := out[k]
			if !ok {
				agg = &featureAgg{}
				out[k] = agg
			}
			agg.add(&items[i])
		}
		return out
	}
	featureMap := accumulate(featureItems)
	meterMap := accumulate(meterItems)

	seen := make(map[key]struct{}, len(featureMap)+len(meterMap))
	keys := make([]key, 0, len(featureMap)+len(meterMap))
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
		if keys[i].subLineItemID != keys[j].subLineItemID {
			return keys[i].subLineItemID < keys[j].subLineItemID
		}
		return keys[i].groupKey < keys[j].groupKey
	})

	records := make([]*events.AnalyticsBenchmarkRecord, 0, len(keys))
	for _, k := range keys {
		fAgg, fOk := featureMap[k]
		mAgg, mOk := meterMap[k]

		rec := &events.AnalyticsBenchmarkRecord{
			RowType:       events.AnalyticsBenchmarkRowLineItem,
			FeatureID:     k.featureID,
			SubLineItemID: k.subLineItemID,
			GroupKey:      k.groupKey,
		}
		var multiItem bool
		switch {
		case fOk && mOk:
			rec.MatchStatus = events.AnalyticsBenchmarkMatchMatched
			rec.MeterID = firstNonEmpty(fAgg.meterID, mAgg.meterID)
			rec.FeaturePriceID = fAgg.firstPriceID()
			rec.MeterPriceID = mAgg.firstPriceID()
			rec.FeatureItemCount = uint64(len(fAgg.items))
			rec.MeterItemCount = uint64(len(mAgg.items))
			rec.FeatureTotalUsage = fAgg.usage
			rec.FeatureTotalCost = fAgg.cost
			rec.FeatureEventCount = fAgg.eventCount
			rec.MeterTotalUsage = mAgg.usage
			rec.MeterTotalCost = mAgg.cost
			rec.MeterEventCount = mAgg.eventCount
			multiItem = len(fAgg.items) > 1 || len(mAgg.items) > 1
		case fOk:
			rec.MatchStatus = events.AnalyticsBenchmarkMatchFeatureOnly
			rec.MeterID = fAgg.meterID
			rec.FeaturePriceID = fAgg.firstPriceID()
			rec.FeatureItemCount = uint64(len(fAgg.items))
			rec.FeatureTotalUsage = fAgg.usage
			rec.FeatureTotalCost = fAgg.cost
			rec.FeatureEventCount = fAgg.eventCount
			multiItem = len(fAgg.items) > 1
		case mOk:
			rec.MatchStatus = events.AnalyticsBenchmarkMatchMeterOnly
			rec.MeterID = mAgg.meterID
			rec.MeterPriceID = mAgg.firstPriceID()
			rec.MeterItemCount = uint64(len(mAgg.items))
			rec.MeterTotalUsage = mAgg.usage
			rec.MeterTotalCost = mAgg.cost
			rec.MeterEventCount = mAgg.eventCount
			multiItem = len(mAgg.items) > 1
		}
		rec.UsageDiff = rec.FeatureTotalUsage.Sub(rec.MeterTotalUsage)
		rec.CostDiff = rec.FeatureTotalCost.Sub(rec.MeterTotalCost)
		rec.DiffReason = classifyDiff(rec.CostDiff, rec.MatchStatus, isMultiFeatureMeter(rec.MeterID), multiItem)
		records = append(records, rec)
	}
	return records
}

// classifyDiff is the single source of truth for diff_reason categorization.
// Precedence: unmatched > none > multi_feature_meter > multi_item > material.
func classifyDiff(
	costDiff decimal.Decimal,
	matchStatus events.AnalyticsBenchmarkMatchStatus,
	isMultiFeatureMeter, multiItem bool,
) events.AnalyticsBenchmarkDiffReason {
	if matchStatus != events.AnalyticsBenchmarkMatchMatched {
		return events.AnalyticsBenchmarkDiffUnmatched
	}
	if costDiff.IsZero() {
		return events.AnalyticsBenchmarkDiffNone
	}
	if isMultiFeatureMeter {
		return events.AnalyticsBenchmarkDiffMultiFeatureMeter
	}
	if multiItem {
		return events.AnalyticsBenchmarkDiffMultiItem
	}
	return events.AnalyticsBenchmarkDiffMaterial
}

// firstNonEmpty returns the first non-empty string among the args, or "".
func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}

// unionAndSortFeatureKeys collects the union of keys across both maps and sorts
// them for deterministic ClickHouse insert order.
func unionAndSortFeatureKeys[K comparable](a, b map[K]*featureAgg) []K {
	seen := make(map[K]struct{}, len(a)+len(b))
	out := make([]K, 0, len(a)+len(b))
	for k := range a {
		if _, ok := seen[k]; !ok {
			seen[k] = struct{}{}
			out = append(out, k)
		}
	}
	for k := range b {
		if _, ok := seen[k]; !ok {
			seen[k] = struct{}{}
			out = append(out, k)
		}
	}
	// Generic sort delegates to the natural ordering via fmt.Sprintf. For the
	// concrete key shape used here (string fields), this matches the per-field
	// lex sort buildLineItemRows does explicitly. We only use this helper for
	// the 2-field feature-row key, where this collapse is unambiguous.
	sort.Slice(out, func(i, j int) bool {
		return fmt.Sprintf("%v", out[i]) < fmt.Sprintf("%v", out[j])
	})
	return out
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
