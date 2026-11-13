package testutil

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/usageanalytics"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// InMemoryUsageQueryStore implements usageanalytics.Repository over an InMemoryMeterUsageStore.
// It mirrors the ClickHouse usage query in plain Go and serves as its reference in tests.
type InMemoryUsageQueryStore struct {
	usage *InMemoryMeterUsageStore
}

func NewInMemoryUsageQueryStore(usage *InMemoryMeterUsageStore) *InMemoryUsageQueryStore {
	return &InMemoryUsageQueryStore{usage: usage}
}

type pricedEvent struct {
	lineID                string
	ts                    time.Time
	qty, amount, subtotal decimal.Decimal
	keys                  map[usageanalytics.Key]string
}

type usageGroup struct {
	row     usageanalytics.Row
	metrics []map[string]bool
	lines   map[string]bool
	points  map[int64]*usageanalytics.Point
}

// addPoint adds an event's amount to its window.
func addPoint(points map[int64]*usageanalytics.Point, q *usageanalytics.Query, ev pricedEvent) {
	start := usageanalytics.WindowStart(ev.ts, q.WindowSize, q.Timezone)
	p, ok := points[start.Unix()]
	if !ok {
		p = &usageanalytics.Point{WindowStart: start, Amount: decimal.Zero, Subtotal: decimal.Zero, Quantity: decimal.Zero}
		points[start.Unix()] = p
	}
	p.Amount = p.Amount.Add(ev.amount)
	p.Subtotal = p.Subtotal.Add(ev.subtotal)
	p.Quantity = p.Quantity.Add(ev.qty)
}

func sortedPoints(points map[int64]*usageanalytics.Point) []usageanalytics.Point {
	if len(points) == 0 {
		return nil
	}
	out := lo.Map(lo.Values(points), func(p *usageanalytics.Point, _ int) usageanalytics.Point { return *p })
	sort.Slice(out, func(i, j int) bool { return out[i].WindowStart.Before(out[j].WindowStart) })
	return out
}

func (s *InMemoryUsageQueryStore) GetBreakdown(_ context.Context, params *usageanalytics.QueryParams) (*usageanalytics.Result, error) {
	q := params.Query
	if err := q.Validate(); err != nil {
		return nil, err
	}
	result := &usageanalytics.Result{Items: []usageanalytics.Row{}, Totals: usageanalytics.Row{Amount: decimal.Zero, Subtotal: decimal.Zero, Quantity: decimal.Zero}}
	if len(params.Lines) == 0 {
		return result, nil
	}

	groups := map[string]*usageGroup{}
	for _, ev := range s.pricedEvents(params) {
		if !matchesUsageFilters(q, ev) {
			continue
		}
		group := lo.Map(q.GroupBy, func(k usageanalytics.Key, _ int) string { return ev.keys[k] })
		id := groupID(group)
		g, ok := groups[id]
		if !ok {
			g = &usageGroup{
				row:     usageanalytics.Row{Group: group, Amount: decimal.Zero, Subtotal: decimal.Zero, Quantity: decimal.Zero, Metrics: map[string]usageanalytics.MetricValue{}},
				metrics: make([]map[string]bool, len(q.Metrics)),
				lines:   map[string]bool{},
				points:  map[int64]*usageanalytics.Point{},
			}
			for i := range g.metrics {
				g.metrics[i] = map[string]bool{}
			}
			if q.Columns != nil {
				g.row.Columns = map[string]decimal.Decimal{}
			}
			groups[id] = g
		}
		g.row.Amount = g.row.Amount.Add(ev.amount)
		g.row.Subtotal = g.row.Subtotal.Add(ev.subtotal)
		g.row.Quantity = g.row.Quantity.Add(ev.qty)
		g.row.EventCount++
		g.lines[ev.lineID] = true
		if q.WindowSize != "" {
			addPoint(g.points, q, ev)
		}
		if q.Columns != nil {
			c := ev.keys[*q.Columns]
			g.row.Columns[c] = g.row.Columns[c].Add(ev.amount)
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

	rows := make([]usageanalytics.Row, 0, len(groups))
	totals := usageanalytics.Row{Amount: decimal.Zero, Subtotal: decimal.Zero, Quantity: decimal.Zero}
	if q.Columns != nil {
		totals.Columns = map[string]decimal.Decimal{}
	}
	for _, g := range groups {
		for i, m := range q.Metrics {
			v := usageanalytics.MetricValue{Count: uint64(len(g.metrics[i]))}
			if v.Count <= usageanalytics.MaxDistinctValues {
				v.Values = lo.Keys(g.metrics[i])
				sort.Strings(v.Values)
			}
			g.row.Metrics[m.Name()] = v
		}
		if q.WithLineItemIDs {
			g.row.LineItemIDs = lo.Keys(g.lines)
			sort.Strings(g.row.LineItemIDs)
		}
		g.row.Points = sortedPoints(g.points)
		totals.Amount = totals.Amount.Add(g.row.Amount)
		totals.Subtotal = totals.Subtotal.Add(g.row.Subtotal)
		totals.Quantity = totals.Quantity.Add(g.row.Quantity)
		totals.EventCount += g.row.EventCount
		for c, v := range g.row.Columns {
			totals.Columns[c] = totals.Columns[c].Add(v)
		}
		rows = append(rows, g.row)
	}
	sortUsageRows(rows, q.Sort)

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

// pricedEvents prices each event once per line window it falls in.
func (s *InMemoryUsageQueryStore) pricedEvents(params *usageanalytics.QueryParams) []pricedEvent {
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
				lineID:   l.LineItemID,
				ts:       ts,
				qty:      qty,
				amount:   qty.Mul(l.UnitPrice),
				subtotal: qty.Mul(l.ListUnitPrice),
				keys:     usageKeyValues(q, r, l, ts),
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

func usageKeyValues(q *usageanalytics.Query, r *events.MeterUsage, l usageanalytics.LineWindow, ts time.Time) map[usageanalytics.Key]string {
	keys := append([]usageanalytics.Key{}, q.GroupBy...)
	if q.Columns != nil {
		keys = append(keys, *q.Columns)
	}
	for _, f := range q.Filters {
		keys = append(keys, f.Key)
	}
	for _, m := range q.Metrics {
		keys = append(keys, m.Key)
	}

	out := make(map[usageanalytics.Key]string, len(keys))
	for _, k := range keys {
		switch k.Kind {
		case usageanalytics.KeyProperty:
			out[k] = usagePropertyValue(r.Properties, k.Property)
		case usageanalytics.KeySource:
			out[k] = r.Source
		case usageanalytics.KeyMeterID:
			out[k] = r.MeterID
		case usageanalytics.KeyFeatureID:
			out[k] = l.FeatureID
		case usageanalytics.KeySubLineItemID:
			out[k] = l.LineItemID
		case usageanalytics.KeyMonth:
			out[k] = ts.In(q.Timezone).Format("2006-01")
		}
	}
	return out
}

// usagePropertyValue reads a property as ClickHouse does: strings as-is, anything else as raw JSON.
func usagePropertyValue(props map[string]interface{}, name string) string {
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

func matchesUsageFilters(q *usageanalytics.Query, ev pricedEvent) bool {
	for _, f := range q.Filters {
		in := lo.Contains(f.Values, ev.keys[f.Key])
		if in != (f.Op == usageanalytics.FilterIn) {
			return false
		}
	}
	if q.Search == "" {
		return true
	}
	search := strings.ToLower(q.Search)
	return lo.SomeBy(q.GroupBy, func(k usageanalytics.Key) bool {
		return strings.Contains(strings.ToLower(ev.keys[k]), search)
	})
}

// sortUsageRows orders by the sort field, then by group values ascending.
func sortUsageRows(rows []usageanalytics.Row, s usageanalytics.Sort) {
	byGroup := func(a, b usageanalytics.Row) int {
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
		case usageanalytics.SortAmount:
			c = a.Amount.Cmp(b.Amount)
		case usageanalytics.SortUsage:
			c = a.Quantity.Cmp(b.Quantity)
		case usageanalytics.SortEventCount:
			c = compareUint64(a.EventCount, b.EventCount)
		case usageanalytics.SortColumn:
			c = a.Columns[s.Column].Cmp(b.Columns[s.Column])
		case usageanalytics.SortName:
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
