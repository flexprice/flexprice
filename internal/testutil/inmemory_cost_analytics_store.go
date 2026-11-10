package testutil

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/domain/costanalytics"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// InMemoryCostAnalyticsStore implements costanalytics.Repository over an InMemoryMeterUsageStore.
// It mirrors the ClickHouse cost query in plain Go and serves as its reference in tests.
type InMemoryCostAnalyticsStore struct {
	usage *InMemoryMeterUsageStore
}

func NewInMemoryCostAnalyticsStore(usage *InMemoryMeterUsageStore) *InMemoryCostAnalyticsStore {
	return &InMemoryCostAnalyticsStore{usage: usage}
}

type pricedEvent struct {
	lineID    string
	qty, cost decimal.Decimal
	keys      map[costanalytics.Key]string
}

type costGroup struct {
	row     costanalytics.Row
	metrics []map[string]bool
	lines   map[string]bool
}

func (s *InMemoryCostAnalyticsStore) GetCostBreakdown(_ context.Context, params *costanalytics.QueryParams) (*costanalytics.Result, error) {
	q := params.Query
	if err := q.Validate(); err != nil {
		return nil, err
	}
	result := &costanalytics.Result{Items: []costanalytics.Row{}, Totals: costanalytics.Row{Cost: decimal.Zero, Quantity: decimal.Zero}}
	if len(params.Lines) == 0 {
		return result, nil
	}

	groups := map[string]*costGroup{}
	for _, ev := range s.pricedEvents(params) {
		if !matchesCostFilters(q, ev) {
			continue
		}
		group := lo.Map(q.GroupBy, func(k costanalytics.Key, _ int) string { return ev.keys[k] })
		id := groupID(group)
		g, ok := groups[id]
		if !ok {
			g = &costGroup{
				row:     costanalytics.Row{Group: group, Cost: decimal.Zero, Quantity: decimal.Zero, Metrics: map[string]costanalytics.MetricValue{}},
				metrics: make([]map[string]bool, len(q.Metrics)),
				lines:   map[string]bool{},
			}
			for i := range g.metrics {
				g.metrics[i] = map[string]bool{}
			}
			if q.Columns != nil {
				g.row.Columns = map[string]decimal.Decimal{}
			}
			groups[id] = g
		}
		g.row.Cost = g.row.Cost.Add(ev.cost)
		g.row.Quantity = g.row.Quantity.Add(ev.qty)
		g.row.EventCount++
		g.lines[ev.lineID] = true
		if q.Columns != nil {
			c := ev.keys[*q.Columns]
			g.row.Columns[c] = g.row.Columns[c].Add(ev.cost)
		}
		for i, m := range q.Metrics {
			if v := ev.keys[m.Key]; v != "" {
				g.metrics[i][v] = true
			}
		}
	}
	if len(groups) == 0 {
		return result, nil
	}

	rows := make([]costanalytics.Row, 0, len(groups))
	totals := costanalytics.Row{Cost: decimal.Zero, Quantity: decimal.Zero}
	if q.Columns != nil {
		totals.Columns = map[string]decimal.Decimal{}
	}
	for _, g := range groups {
		for i, m := range q.Metrics {
			v := costanalytics.MetricValue{Count: uint64(len(g.metrics[i]))}
			if v.Count <= costanalytics.MaxDistinctValues {
				v.Values = lo.Keys(g.metrics[i])
				sort.Strings(v.Values)
			}
			g.row.Metrics[m.Name()] = v
		}
		if q.WithLineItemIDs {
			g.row.LineItemIDs = lo.Keys(g.lines)
			sort.Strings(g.row.LineItemIDs)
		}
		totals.Cost = totals.Cost.Add(g.row.Cost)
		totals.Quantity = totals.Quantity.Add(g.row.Quantity)
		totals.EventCount += g.row.EventCount
		for c, v := range g.row.Columns {
			totals.Columns[c] = totals.Columns[c].Add(v)
		}
		rows = append(rows, g.row)
	}
	sortCostRows(rows, q.Sort)

	result.TotalCount = uint64(len(rows))
	result.Totals = totals
	if q.Offset < len(rows) {
		result.Items = rows[q.Offset:min(q.Offset+q.Limit, len(rows))]
	}
	return result, nil
}

// groupID encodes group values without separator collisions.
func groupID(group []string) string {
	b, _ := json.Marshal(group)
	return string(b)
}

// pricedEvents costs each event once per line window it falls in.
func (s *InMemoryCostAnalyticsStore) pricedEvents(params *costanalytics.QueryParams) []pricedEvent {
	q := params.Query
	s.usage.mu.RLock()
	records := dedupeMeterUsage(s.usage.records, q.Deduplicate)
	s.usage.mu.RUnlock()

	var out []pricedEvent
	for _, r := range records {
		if r.TenantID != params.TenantID || r.EnvironmentID != params.EnvironmentID {
			continue
		}
		ts := r.Timestamp.UTC().Truncate(time.Second)
		for _, l := range params.Lines {
			if l.MeterID != r.MeterID || !lo.Contains(l.ExternalCustomerIDs, r.ExternalCustomerID) {
				continue
			}
			if ts.Before(l.Start.Truncate(time.Second)) || !ts.Before(l.End.Truncate(time.Second)) {
				continue
			}
			qty := r.QtyTotal.Truncate(15)
			out = append(out, pricedEvent{
				lineID: l.LineItemID,
				qty:    qty,
				cost:   qty.Mul(l.UnitPrice),
				keys:   costKeyValues(q, r, l, ts),
			})
		}
	}
	return out
}

// dedupeMeterUsage keeps the latest ingested row per ClickHouse sorting key when dedupe is on.
func dedupeMeterUsage(records []*events.MeterUsage, dedupe bool) []*events.MeterUsage {
	if !dedupe {
		return records
	}
	latest := map[string]*events.MeterUsage{}
	var order []string
	for _, r := range records {
		k := strings.Join([]string{r.TenantID, r.EnvironmentID, r.ExternalCustomerID, r.MeterID,
			r.Timestamp.UTC().Truncate(time.Second).String(), r.ID}, "\x00")
		prev, ok := latest[k]
		if !ok {
			order = append(order, k)
		}
		if !ok || !r.IngestedAt.Before(prev.IngestedAt) {
			latest[k] = r
		}
	}
	return lo.Map(order, func(k string, _ int) *events.MeterUsage { return latest[k] })
}

func costKeyValues(q *costanalytics.Query, r *events.MeterUsage, l costanalytics.LineWindow, ts time.Time) map[costanalytics.Key]string {
	keys := append([]costanalytics.Key{}, q.GroupBy...)
	if q.Columns != nil {
		keys = append(keys, *q.Columns)
	}
	for _, f := range q.Filters {
		keys = append(keys, f.Key)
	}
	for _, m := range q.Metrics {
		keys = append(keys, m.Key)
	}

	out := make(map[costanalytics.Key]string, len(keys))
	for _, k := range keys {
		switch k.Kind {
		case costanalytics.KeyProperty:
			out[k] = costPropertyValue(r.Properties, k.Property)
		case costanalytics.KeySource:
			out[k] = r.Source
		case costanalytics.KeyMeterID:
			out[k] = r.MeterID
		case costanalytics.KeyFeatureID:
			out[k] = l.FeatureID
		case costanalytics.KeySubLineItemID:
			out[k] = l.LineItemID
		case costanalytics.KeyMonth:
			out[k] = ts.In(q.Timezone).Format("2006-01")
		}
	}
	return out
}

// costPropertyValue reads a property as ClickHouse does: strings as-is, anything else as raw JSON.
func costPropertyValue(props map[string]interface{}, name string) string {
	v, ok := props[name]
	if !ok {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(raw)
}

func matchesCostFilters(q *costanalytics.Query, ev pricedEvent) bool {
	for _, f := range q.Filters {
		in := lo.Contains(f.Values, ev.keys[f.Key])
		if in != (f.Op == costanalytics.FilterIn) {
			return false
		}
	}
	if q.Search == "" {
		return true
	}
	search := strings.ToLower(q.Search)
	return lo.SomeBy(q.GroupBy, func(k costanalytics.Key) bool {
		return strings.Contains(strings.ToLower(ev.keys[k]), search)
	})
}

// sortCostRows orders by the sort field, then by group values ascending.
func sortCostRows(rows []costanalytics.Row, s costanalytics.Sort) {
	byGroup := func(a, b costanalytics.Row) int {
		for i := range a.Group {
			if c := strings.Compare(a.Group[i], b.Group[i]); c != 0 {
				return c
			}
		}
		return 0
	}
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		var c int
		switch s.Field {
		case costanalytics.SortTotal:
			c = a.Cost.Cmp(b.Cost)
		case costanalytics.SortUsage:
			c = a.Quantity.Cmp(b.Quantity)
		case costanalytics.SortEventCount:
			c = compareUint64(a.EventCount, b.EventCount)
		case costanalytics.SortColumn:
			c = a.Columns[s.Column].Cmp(b.Columns[s.Column])
		case costanalytics.SortName:
			c = byGroup(a, b)
			if s.Desc {
				c = -c
			}
			return c < 0
		}
		if s.Desc {
			c = -c
		}
		if c == 0 {
			c = byGroup(a, b)
		}
		return c < 0
	})
}

func compareUint64(a, b uint64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}
