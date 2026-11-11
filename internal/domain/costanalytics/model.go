// Package costanalytics holds the types of the cost analytics query: who spent what, attributed
// per event from the customer's own billed prices.
package costanalytics

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/shopspring/decimal"
)

// Limits shared by the API and the query builder.
const (
	MaxGroupByKeys  = 3
	MaxLimit        = 100
	DefaultLimit    = 25
	MaxFilterValues = 100
	MaxMetrics      = 5
	MaxSearchLength = 200
)

// KeyKind is what a group-by, column, filter or metric key reads from.
type KeyKind string

const (
	KeyProperty      KeyKind = "property"         // properties.<name>
	KeySource        KeyKind = "source"           // event source
	KeyMeterID       KeyKind = "meter_id"         // meter of the event
	KeyFeatureID     KeyKind = "feature_id"       // feature of the line's meter
	KeySubLineItemID KeyKind = "sub_line_item_id" // subscription line item that priced the event
	KeyMonth         KeyKind = "month"            // calendar month in the query timezone; columns only
)

const propertyPrefix = "properties."

// propertyNamePattern keeps property names safe to inline as SQL string literals.
var propertyNamePattern = regexp.MustCompile(`^[A-Za-z0-9_\-:/@$ ]{1,128}$`)

// Key is a parsed key such as "properties.user_id" or "source".
type Key struct {
	Kind     KeyKind
	Property string // set only for KeyProperty
}

// ParseKey parses the API spelling of a key.
func ParseKey(s string) (Key, error) {
	if name, ok := strings.CutPrefix(s, propertyPrefix); ok {
		if !propertyNamePattern.MatchString(name) {
			return Key{}, ierr.NewErrorf("invalid property key %q", s).
				WithHint("Property names may use letters, digits, spaces and _ - : / @ $, up to 128 characters").
				Mark(ierr.ErrValidation)
		}
		return Key{Kind: KeyProperty, Property: name}, nil
	}
	switch k := KeyKind(s); k {
	case KeySource, KeyMeterID, KeyFeatureID, KeySubLineItemID, KeyMonth:
		return Key{Kind: k}, nil
	}
	return Key{}, ierr.NewErrorf("unknown key %q", s).
		WithHint("Use properties.<name>, source, meter_id, feature_id or sub_line_item_id").
		Mark(ierr.ErrValidation)
}

// valid reports whether the key is one ParseKey can produce, so the SQL builder can rely on it.
func (k Key) valid() bool {
	switch k.Kind {
	case KeyProperty:
		return propertyNamePattern.MatchString(k.Property)
	case KeySource, KeyMeterID, KeyFeatureID, KeySubLineItemID, KeyMonth:
		return k.Property == ""
	}
	return false
}

// String returns the API spelling of the key.
func (k Key) String() string {
	if k.Kind == KeyProperty {
		return propertyPrefix + k.Property
	}
	return string(k.Kind)
}

// FilterOp is a filter operator.
type FilterOp string

const (
	FilterIn    FilterOp = "in"
	FilterNotIn FilterOp = "not_in"
)

// Filter keeps or drops events by a key's value. An empty string value matches events without the key.
type Filter struct {
	Key    Key
	Op     FilterOp
	Values []string
}

// SortField is what rows are ordered by.
type SortField string

const (
	SortTotal      SortField = "total"       // cost
	SortColumn     SortField = "column"      // cost in one column value
	SortName       SortField = "name"        // group values
	SortUsage      SortField = "usage"       // quantity
	SortEventCount SortField = "event_count" // number of events
)

// Sort orders the rows. Ties are always broken by the group values, so pages are stable.
type Sort struct {
	Field  SortField
	Column string // column value, for SortColumn
	Desc   bool
}

// MetricType is an allowed aggregate. The server maps each type to its SQL; callers never send SQL.
type MetricType string

const (
	MetricDistinct MetricType = "distinct"
)

// MaxDistinctValues is how many values a distinct metric returns; above it only the count is returned.
const MaxDistinctValues = 5

// Metric is an extra aggregate per row, e.g. the distinct owners behind a group.
type Metric struct {
	Type MetricType
	Key  Key
}

// Name is the metric's key in the response, e.g. "distinct:properties.owner".
func (m Metric) Name() string {
	return fmt.Sprintf("%s:%s", m.Type, m.Key)
}

// Query is a validated cost analytics request.
type Query struct {
	CustomerID          string
	ExternalCustomerIDs []string
	Start, End          time.Time
	Timezone            *time.Location
	MeterIDs            []string
	FeatureIDs          []string
	GroupBy             []Key
	Columns             *Key
	Filters             []Filter
	Search              string
	Sort                Sort
	Metrics             []Metric
	Deduplicate         bool
	Limit, Offset       int
}

// Validate checks the parts of the query the SQL builder relies on.
func (q *Query) Validate() error {
	if !q.Start.Before(q.End) {
		return validationErr("start_time must be before end_time")
	}
	if n := len(q.GroupBy); n == 0 || n > MaxGroupByKeys {
		return validationErr("group_by needs 1 to %d keys", MaxGroupByKeys)
	}
	keys := append([]Key{}, q.GroupBy...)
	if q.Columns != nil {
		keys = append(keys, *q.Columns)
	}
	for _, f := range q.Filters {
		keys = append(keys, f.Key)
	}
	for _, m := range q.Metrics {
		keys = append(keys, m.Key)
	}
	for _, k := range keys {
		if !k.valid() {
			return validationErr("invalid key %q", k)
		}
	}
	seen := map[Key]bool{}
	for _, k := range q.GroupBy {
		if k.Kind == KeyMonth {
			return validationErr("month can be used only in columns")
		}
		if seen[k] {
			return validationErr("group_by key %s is repeated", k)
		}
		seen[k] = true
	}
	for _, f := range q.Filters {
		if f.Key.Kind == KeyMonth {
			return validationErr("month cannot be filtered")
		}
		if f.Op != FilterIn && f.Op != FilterNotIn {
			return validationErr("filter op must be in or not_in")
		}
		if n := len(f.Values); n == 0 || n > MaxFilterValues {
			return validationErr("filter on %s needs 1 to %d values", f.Key, MaxFilterValues)
		}
	}
	if len(q.Search) > MaxSearchLength {
		return validationErr("search is longer than %d characters", MaxSearchLength)
	}
	if len(q.Metrics) > MaxMetrics {
		return validationErr("at most %d metrics", MaxMetrics)
	}
	for _, m := range q.Metrics {
		if m.Type != MetricDistinct {
			return validationErr("unknown metric type %q", m.Type)
		}
		if m.Key.Kind == KeyMonth {
			return validationErr("month cannot be a metric key")
		}
	}
	switch q.Sort.Field {
	case SortTotal, SortName, SortUsage, SortEventCount:
	case SortColumn:
		if q.Columns == nil {
			return validationErr("sort by column needs columns")
		}
	default:
		return validationErr("unknown sort %q", q.Sort.Field)
	}
	if q.Limit < 1 || q.Limit > MaxLimit {
		return validationErr("limit must be between 1 and %d", MaxLimit)
	}
	if q.Offset < 0 {
		return validationErr("offset must not be negative")
	}
	if q.Timezone == nil {
		return validationErr("timezone is required")
	}
	return nil
}

func validationErr(format string, args ...any) error {
	return ierr.NewErrorf(format, args...).Mark(ierr.ErrValidation)
}

// BillingPeriodSegment is the part of the requested range inside one billing period of one subscription.
type BillingPeriodSegment struct {
	SubscriptionID         string
	PeriodStart, PeriodEnd time.Time // full billing period
	Start, End             time.Time // requested range clipped to the period
	Closed                 bool      // period has ended
}

// CoversWholePeriod reports whether the segment spans its full billing period.
func (s BillingPeriodSegment) CoversWholePeriod() bool {
	return s.Start.Equal(s.PeriodStart) && s.End.Equal(s.PeriodEnd)
}

// LineWindow is one line item inside one billing period segment: the only pricing input ClickHouse gets.
type LineWindow struct {
	LineItemID string
	MeterID    string
	FeatureID  string
	// ExternalCustomerIDs whose events this line prices: the subscription's customer, plus inherited
	// children for a parent subscription.
	ExternalCustomerIDs []string
	Segment             BillingPeriodSegment
	Start, End          time.Time // line active range clipped to the segment; end exclusive
	Currency            string
	UnitPrice           decimal.Decimal
}

// Row is one group of the result. Group values follow Query.GroupBy; an empty value means the
// events had no such key.
type Row struct {
	Group      []string
	Cost       decimal.Decimal // unrounded
	Quantity   decimal.Decimal
	EventCount uint64
	Columns    map[string]decimal.Decimal // cost per column value, when columns are requested
	Metrics    map[string]MetricValue     // keyed by Metric.Name()
}

// MetricValue is one metric of a row.
type MetricValue struct {
	Count  uint64
	Values []string // only when Count <= MaxDistinctValues
}

// Result is one page of rows plus totals over all rows that matched.
type Result struct {
	Items      []Row
	Totals     Row // Group and Metrics are empty
	TotalCount uint64
}
