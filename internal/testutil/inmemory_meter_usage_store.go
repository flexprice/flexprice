package testutil

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// InMemoryMeterUsageStore implements events.MeterUsageRepository for testing.
// It mirrors the ClickHouse SQL behavior in Go:
//   - All six aggregations (SUM, COUNT, COUNT_UNIQUE, MAX, AVG, LATEST)
//   - Window bucketing with billing-anchor MONTH support
//   - group_by over meter_id / source / properties.<field>
//   - Property filters (equality and IN)
//   - Source filters
//   - Time-series Points for detailed analytics when WindowSize is set
//   - Bucketed meters (MAX/SUM with bucket_size) with optional GroupByProperty
//
// Each query method matches the corresponding ClickHouse SQL so service-layer
// tests can exercise the real aggregation logic without a ClickHouse instance.
type InMemoryMeterUsageStore struct {
	mu      sync.RWMutex
	records []*events.MeterUsage
}

func NewInMemoryMeterUsageStore() *InMemoryMeterUsageStore {
	return &InMemoryMeterUsageStore{}
}

func (s *InMemoryMeterUsageStore) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records = nil
}

func (s *InMemoryMeterUsageStore) BulkInsertMeterUsage(_ context.Context, records []*events.MeterUsage) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records = append(s.records, records...)
	return nil
}

func (s *InMemoryMeterUsageStore) IsDuplicate(_ context.Context, meterID, uniqueHash string) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, r := range s.records {
		if r.MeterID == meterID && r.UniqueHash == uniqueHash {
			return true, nil
		}
	}
	return false, nil
}

// ---------------------------------------------------------------------------
// Filter helpers (mirror BuildWhereClause / BuildDetailedWhereClause)
// ---------------------------------------------------------------------------

// matchRecord checks the scalar-query WHERE clause:
// tenant/env/customer/meter/time window + COUNT_UNIQUE's unique_hash filter.
func matchRecord(r *events.MeterUsage, p *events.MeterUsageQueryParams) bool {
	if p.TenantID != "" && r.TenantID != p.TenantID {
		return false
	}
	if p.EnvironmentID != "" && r.EnvironmentID != p.EnvironmentID {
		return false
	}
	if p.MeterID != "" && r.MeterID != p.MeterID {
		return false
	}
	if len(p.MeterIDs) > 0 && !containsString(p.MeterIDs, r.MeterID) {
		return false
	}
	if !p.StartTime.IsZero() && r.Timestamp.Before(p.StartTime) {
		return false
	}
	if !p.EndTime.IsZero() && !r.Timestamp.Before(p.EndTime) {
		return false
	}
	if p.ExternalCustomerID != "" && r.ExternalCustomerID != p.ExternalCustomerID {
		return false
	}
	if len(p.ExternalCustomerIDs) > 0 && !containsString(p.ExternalCustomerIDs, r.ExternalCustomerID) {
		return false
	}
	if p.AggregationType == types.AggregationCountUnique && r.UniqueHash == "" {
		return false
	}
	return true
}

// matchDetailedRecord checks the detailed-analytics WHERE clause:
// scalar filters + source filter + property filters. COUNT_UNIQUE's
// unique_hash filter is NOT applied here (matching ClickHouse — the
// detailed query includes count_unique alongside other aggregations and
// computes it as COUNT(DISTINCT unique_hash) without WHERE exclusion).
func matchDetailedRecord(r *events.MeterUsage, p *events.MeterUsageDetailedAnalyticsParams) bool {
	if p.TenantID != "" && r.TenantID != p.TenantID {
		return false
	}
	if p.EnvironmentID != "" && r.EnvironmentID != p.EnvironmentID {
		return false
	}
	if p.ExternalCustomerID != "" && r.ExternalCustomerID != p.ExternalCustomerID {
		return false
	}
	if len(p.ExternalCustomerIDs) > 0 && !containsString(p.ExternalCustomerIDs, r.ExternalCustomerID) {
		return false
	}
	if len(p.MeterIDs) > 0 && !containsString(p.MeterIDs, r.MeterID) {
		return false
	}
	if !p.StartTime.IsZero() && r.Timestamp.Before(p.StartTime) {
		return false
	}
	if !p.EndTime.IsZero() && !r.Timestamp.Before(p.EndTime) {
		return false
	}
	if len(p.Sources) > 0 && !containsString(p.Sources, r.Source) {
		return false
	}
	if len(p.PropertyFilters) > 0 {
		for key, allowed := range p.PropertyFilters {
			if len(allowed) == 0 {
				continue
			}
			v := propertyValue(r.Properties, key)
			if !containsString(allowed, v) {
				return false
			}
		}
	}
	return true
}

func containsString(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

// propertyValue reads a property as string from the event's Properties map.
// Mirrors JSONExtractString — missing keys return "".
func propertyValue(props map[string]interface{}, key string) string {
	if props == nil {
		return ""
	}
	v, ok := props[key]
	if !ok || v == nil {
		return ""
	}
	switch x := v.(type) {
	case string:
		return x
	case fmt.Stringer:
		return x.String()
	default:
		return fmt.Sprintf("%v", v)
	}
}

// ---------------------------------------------------------------------------
// Window bucketing (mirrors formatWindowSize / formatWindowSizeWithBillingAnchor)
// ---------------------------------------------------------------------------

// truncateToWindow returns the start of the window that t falls in.
// Returns time.Time{} when ws is empty (no bucketing requested).
func truncateToWindow(t time.Time, ws types.WindowSize, billingAnchor *time.Time) time.Time {
	t = t.UTC()
	switch ws {
	case "":
		return time.Time{}
	case types.WindowSizeMinute:
		return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), 0, 0, time.UTC)
	case types.WindowSize15Min:
		return truncateInterval(t, 15*time.Minute)
	case types.WindowSize30Min:
		return truncateInterval(t, 30*time.Minute)
	case types.WindowSizeHour:
		return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), 0, 0, 0, time.UTC)
	case types.WindowSize3Hour:
		return truncateHourInterval(t, 3)
	case types.WindowSize6Hour:
		return truncateHourInterval(t, 6)
	case types.WindowSize12Hour:
		return truncateHourInterval(t, 12)
	case types.WindowSizeDay:
		return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
	case types.WindowSizeWeek:
		// ClickHouse toStartOfWeek default mode (0) starts on Sunday.
		offset := int(t.Weekday()) // Sunday=0..Saturday=6
		monday := t.AddDate(0, 0, -offset)
		return time.Date(monday.Year(), monday.Month(), monday.Day(), 0, 0, 0, 0, time.UTC)
	case types.WindowSizeMonth:
		if billingAnchor != nil {
			anchorDay := billingAnchor.Day()
			shifted := t.AddDate(0, 0, -(anchorDay - 1))
			monthStart := time.Date(shifted.Year(), shifted.Month(), 1, 0, 0, 0, 0, time.UTC)
			return monthStart.AddDate(0, 0, anchorDay-1)
		}
		return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
	default:
		return time.Time{}
	}
}

func truncateInterval(t time.Time, d time.Duration) time.Time {
	dayStart := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
	delta := t.Sub(dayStart)
	buckets := int64(delta / d)
	return dayStart.Add(time.Duration(buckets) * d)
}

func truncateHourInterval(t time.Time, hours int) time.Time {
	dayStart := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
	bucket := t.Hour() / hours
	return dayStart.Add(time.Duration(bucket*hours) * time.Hour)
}

// ---------------------------------------------------------------------------
// Aggregation primitives (mirror the SQL aggregation functions)
// ---------------------------------------------------------------------------

// aggregateScalar reduces a slice of records to a single value per the agg type.
// Empty input yields decimal.Zero (matches ClickHouse aggregate-of-empty semantics).
func aggregateScalar(records []*events.MeterUsage, aggType types.AggregationType) decimal.Decimal {
	if len(records) == 0 {
		return decimal.Zero
	}
	switch aggType {
	case types.AggregationCount:
		return decimal.NewFromInt(int64(distinctIDCount(records)))
	case types.AggregationCountUnique:
		return decimal.NewFromInt(int64(distinctUniqueHashCount(records)))
	case types.AggregationMax:
		max := records[0].QtyTotal
		for _, r := range records[1:] {
			if r.QtyTotal.GreaterThan(max) {
				max = r.QtyTotal
			}
		}
		return max
	case types.AggregationAvg:
		var sum decimal.Decimal
		for _, r := range records {
			sum = sum.Add(r.QtyTotal)
		}
		return sum.Div(decimal.NewFromInt(int64(len(records))))
	case types.AggregationLatest:
		latest := records[0]
		for _, r := range records[1:] {
			if r.Timestamp.After(latest.Timestamp) {
				latest = r
			}
		}
		return latest.QtyTotal
	default: // SUM, SumWithMultiplier, WeightedSum
		var sum decimal.Decimal
		for _, r := range records {
			sum = sum.Add(r.QtyTotal)
		}
		return sum
	}
}

func distinctIDCount(records []*events.MeterUsage) int {
	seen := make(map[string]struct{}, len(records))
	for _, r := range records {
		seen[r.ID] = struct{}{}
	}
	return len(seen)
}

func distinctUniqueHashCount(records []*events.MeterUsage) int {
	seen := make(map[string]struct{}, len(records))
	for _, r := range records {
		// Matches ClickHouse COUNT(DISTINCT unique_hash): empty strings are
		// counted as a distinct value. Single-meter scalar queries pre-filter
		// empty hashes via matchRecord, so this only differs for detailed
		// analytics where multiple aggregations are computed together.
		seen[r.UniqueHash] = struct{}{}
	}
	return len(seen)
}

// ---------------------------------------------------------------------------
// GetUsage / GetUsageMultiMeter
// ---------------------------------------------------------------------------

func (s *InMemoryMeterUsageStore) GetUsage(_ context.Context, params *events.MeterUsageQueryParams) (*events.MeterUsageAggregationResult, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	matched := make([]*events.MeterUsage, 0)
	for _, r := range s.records {
		if matchRecord(r, params) {
			matched = append(matched, r)
		}
	}

	result := &events.MeterUsageAggregationResult{
		MeterID:         params.MeterID,
		AggregationType: params.AggregationType,
	}

	if params.WindowSize == "" {
		result.TotalValue = aggregateScalar(matched, params.AggregationType)
		result.EventCount = uint64(distinctIDCount(matched))
		return result, nil
	}

	// Windowed: bucket records, aggregate per bucket, accumulate total + points.
	buckets := bucketRecords(matched, params.WindowSize, params.BillingAnchor)
	points := make([]events.MeterUsageResult, 0, len(buckets))
	for _, b := range buckets {
		points = append(points, events.MeterUsageResult{
			WindowStart: b.start,
			Value:       aggregateScalar(b.records, params.AggregationType),
			EventCount:  uint64(distinctIDCount(b.records)),
		})
	}
	sort.Slice(points, func(i, j int) bool { return points[i].WindowStart.Before(points[j].WindowStart) })

	for _, p := range points {
		result.TotalValue = result.TotalValue.Add(p.Value)
		result.EventCount += p.EventCount
	}
	result.Points = points
	return result, nil
}

func (s *InMemoryMeterUsageStore) GetUsageMultiMeter(_ context.Context, params *events.MeterUsageQueryParams) ([]*events.MeterUsageAggregationResult, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	byMeter := make(map[string][]*events.MeterUsage)
	for _, r := range s.records {
		if !matchRecord(r, params) {
			continue
		}
		byMeter[r.MeterID] = append(byMeter[r.MeterID], r)
	}

	results := make([]*events.MeterUsageAggregationResult, 0, len(byMeter))
	for meterID, recs := range byMeter {
		res := &events.MeterUsageAggregationResult{
			MeterID:         meterID,
			AggregationType: params.AggregationType,
		}

		if params.WindowSize == "" {
			res.TotalValue = aggregateScalar(recs, params.AggregationType)
			res.EventCount = uint64(distinctIDCount(recs))
			results = append(results, res)
			continue
		}

		buckets := bucketRecords(recs, params.WindowSize, params.BillingAnchor)
		points := make([]events.MeterUsageResult, 0, len(buckets))
		for _, b := range buckets {
			points = append(points, events.MeterUsageResult{
				WindowStart: b.start,
				Value:       aggregateScalar(b.records, params.AggregationType),
				EventCount:  uint64(distinctIDCount(b.records)),
			})
		}
		sort.Slice(points, func(i, j int) bool { return points[i].WindowStart.Before(points[j].WindowStart) })
		for _, p := range points {
			res.TotalValue = res.TotalValue.Add(p.Value)
			res.EventCount += p.EventCount
		}
		res.Points = points
		results = append(results, res)
	}
	return results, nil
}

type recordBucket struct {
	start   time.Time
	records []*events.MeterUsage
}

func bucketRecords(records []*events.MeterUsage, ws types.WindowSize, anchor *time.Time) []recordBucket {
	if ws == "" {
		return nil
	}
	byStart := make(map[time.Time][]*events.MeterUsage)
	for _, r := range records {
		key := truncateToWindow(r.Timestamp, ws, anchor)
		byStart[key] = append(byStart[key], r)
	}
	out := make([]recordBucket, 0, len(byStart))
	for k, v := range byStart {
		out = append(out, recordBucket{start: k, records: v})
	}
	return out
}

// ---------------------------------------------------------------------------
// GetUsageForBucketedMeters
// ---------------------------------------------------------------------------

// GetUsageForBucketedMeters implements the bucketed-meter SQL (BuildBucketedQuery):
//   - WindowSize defines the bucket boundaries.
//   - GroupByProperty (when set) sub-buckets each window by a JSON property.
//   - AggregationType selects MAX (default) or SUM as the per-(bucket[, group]) reducer.
//   - result.Value = sum of all per-bucket values (matches "SELECT sum(...) AS total").
func (s *InMemoryMeterUsageStore) GetUsageForBucketedMeters(_ context.Context, params *events.MeterUsageQueryParams) (*events.AggregationResult, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	matched := make([]*events.MeterUsage, 0)
	for _, r := range s.records {
		if matchRecord(r, params) {
			matched = append(matched, r)
		}
	}

	result := &events.AggregationResult{
		Type:    params.AggregationType,
		MeterID: params.MeterID,
	}

	aggFn := func(recs []*events.MeterUsage) decimal.Decimal {
		if params.AggregationType == types.AggregationSum {
			return aggregateScalar(recs, types.AggregationSum)
		}
		return aggregateScalar(recs, types.AggregationMax)
	}

	hasGroupBy := params.GroupByProperty != ""

	// Bucket records by window first.
	byBucket := make(map[time.Time][]*events.MeterUsage)
	for _, r := range matched {
		key := truncateToWindow(r.Timestamp, params.WindowSize, params.BillingAnchor)
		byBucket[key] = append(byBucket[key], r)
	}

	type entry struct {
		bucket time.Time
		group  string
		value  decimal.Decimal
	}
	entries := make([]entry, 0)

	if hasGroupBy {
		for bucket, recs := range byBucket {
			byGroup := make(map[string][]*events.MeterUsage)
			for _, r := range recs {
				gk := propertyValue(r.Properties, params.GroupByProperty)
				byGroup[gk] = append(byGroup[gk], r)
			}
			for gk, grecs := range byGroup {
				entries = append(entries, entry{bucket: bucket, group: gk, value: aggFn(grecs)})
			}
		}
		sort.Slice(entries, func(i, j int) bool {
			if !entries[i].bucket.Equal(entries[j].bucket) {
				return entries[i].bucket.Before(entries[j].bucket)
			}
			return entries[i].group < entries[j].group
		})
	} else {
		for bucket, recs := range byBucket {
			entries = append(entries, entry{bucket: bucket, value: aggFn(recs)})
		}
		sort.Slice(entries, func(i, j int) bool { return entries[i].bucket.Before(entries[j].bucket) })
	}

	for _, e := range entries {
		result.Value = result.Value.Add(e.value)
		ur := events.UsageResult{WindowSize: e.bucket, Value: e.value}
		if hasGroupBy {
			ur.GroupKey = e.group
		}
		result.Results = append(result.Results, ur)
	}
	return result, nil
}

// ---------------------------------------------------------------------------
// GetDistinctMeterIDs
// ---------------------------------------------------------------------------

func (s *InMemoryMeterUsageStore) GetDistinctMeterIDs(_ context.Context, params *events.MeterUsageQueryParams) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	seen := make(map[string]struct{})
	for _, r := range s.records {
		if !matchRecord(r, params) {
			continue
		}
		seen[r.MeterID] = struct{}{}
	}
	ids := make([]string, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids, nil
}

// ---------------------------------------------------------------------------
// GetDetailedAnalytics
// ---------------------------------------------------------------------------

// detailedGroupKey is the composite key produced by params.GroupBy for one record.
// It's used as a map key (so each field is positional and stringly-encoded).
type detailedGroupKey struct {
	meterID    string
	source     string
	properties string // joined "k=v|k=v" of all property dims, in GroupBy order
}

// buildDetailedGroupKey extracts the group dims for a record matching params.GroupBy.
// Also returns the per-property values (for Properties on the result struct).
func buildDetailedGroupKey(r *events.MeterUsage, groupBy []string) (detailedGroupKey, map[string]string) {
	k := detailedGroupKey{}
	props := make(map[string]string)
	parts := make([]string, 0)
	for _, g := range groupBy {
		switch {
		case g == "meter_id":
			k.meterID = r.MeterID
		case g == "source":
			k.source = r.Source
		case strings.HasPrefix(g, "properties."):
			name := strings.TrimPrefix(g, "properties.")
			v := propertyValue(r.Properties, name)
			props[name] = v
			parts = append(parts, name+"="+v)
		}
	}
	k.properties = strings.Join(parts, "|")
	return k, props
}

func (s *InMemoryMeterUsageStore) GetDetailedAnalytics(_ context.Context, params *events.MeterUsageDetailedAnalyticsParams) ([]*events.MeterUsageDetailedResult, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	matched := make([]*events.MeterUsage, 0)
	for _, r := range s.records {
		if matchDetailedRecord(r, params) {
			matched = append(matched, r)
		}
	}

	sourceInGroupBy := containsString(params.GroupBy, "source")

	type group struct {
		key        detailedGroupKey
		meterID    string
		source     string
		properties map[string]string
		records    []*events.MeterUsage
	}
	byKey := make(map[detailedGroupKey]*group)
	for _, r := range matched {
		key, props := buildDetailedGroupKey(r, params.GroupBy)
		g, ok := byKey[key]
		if !ok {
			g = &group{
				key:        key,
				meterID:    key.meterID,
				source:     key.source,
				properties: props,
			}
			byKey[key] = g
		}
		g.records = append(g.records, r)
	}

	results := make([]*events.MeterUsageDetailedResult, 0, len(byKey))
	for _, g := range byKey {
		res := &events.MeterUsageDetailedResult{
			MeterID:          g.meterID,
			Source:           g.source,
			Properties:       g.properties,
			TotalUsage:       aggregateScalar(g.records, types.AggregationSum),
			MaxUsage:         aggregateScalar(g.records, types.AggregationMax),
			LatestUsage:      aggregateScalar(g.records, types.AggregationLatest),
			CountUniqueUsage: uint64(distinctUniqueHashCount(g.records)),
			EventCount:       uint64(distinctIDCount(g.records)),
		}

		// When source isn't a group dimension, ClickHouse returns groupUniqArray(source)
		// — the set of distinct sources contributing to this group.
		if !sourceInGroupBy {
			srcs := make(map[string]struct{})
			for _, r := range g.records {
				srcs[r.Source] = struct{}{}
			}
			sources := make([]string, 0, len(srcs))
			for s := range srcs {
				sources = append(sources, s)
			}
			sort.Strings(sources)
			res.Sources = sources
		}

		if params.WindowSize != "" {
			res.Points = computeDetailedPoints(g.records, params.WindowSize, params.BillingAnchor)
		}

		results = append(results, res)
	}

	// Deterministic ordering: by meter_id, then source, then properties string.
	sort.Slice(results, func(i, j int) bool {
		if results[i].MeterID != results[j].MeterID {
			return results[i].MeterID < results[j].MeterID
		}
		if results[i].Source != results[j].Source {
			return results[i].Source < results[j].Source
		}
		return propertiesString(results[i].Properties) < propertiesString(results[j].Properties)
	})

	return results, nil
}

// computeDetailedPoints buckets a group's records by WindowSize and computes
// all five aggregations per bucket (mirroring buildConditionalAggregationColumns).
func computeDetailedPoints(records []*events.MeterUsage, ws types.WindowSize, anchor *time.Time) []events.MeterUsageDetailedPoint {
	buckets := bucketRecords(records, ws, anchor)
	points := make([]events.MeterUsageDetailedPoint, 0, len(buckets))
	for _, b := range buckets {
		points = append(points, events.MeterUsageDetailedPoint{
			WindowStart:      b.start,
			TotalUsage:       aggregateScalar(b.records, types.AggregationSum),
			MaxUsage:         aggregateScalar(b.records, types.AggregationMax),
			LatestUsage:      aggregateScalar(b.records, types.AggregationLatest),
			CountUniqueUsage: uint64(distinctUniqueHashCount(b.records)),
			EventCount:       uint64(distinctIDCount(b.records)),
		})
	}
	sort.Slice(points, func(i, j int) bool { return points[i].WindowStart.Before(points[j].WindowStart) })
	return points
}

func propertiesString(p map[string]string) string {
	if len(p) == 0 {
		return ""
	}
	keys := make([]string, 0, len(p))
	for k := range p {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+p[k])
	}
	return strings.Join(parts, "|")
}

// Ensure interface compliance
var _ events.MeterUsageRepository = (*InMemoryMeterUsageStore)(nil)
