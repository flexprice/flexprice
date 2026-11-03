package events

import (
	"time"

	"github.com/shopspring/decimal"
)

// AnalyticsV2KeyMonth is the time column key: one column per calendar month in the customer's timezone.
const AnalyticsV2KeyMonth = "month"

// AnalyticsV2Scope bounds every analytics v2 read to one tenant, environment and set of customers.
type AnalyticsV2Scope struct {
	TenantID            string
	EnvironmentID       string
	ExternalCustomerIDs []string
	// DataThrough excludes events received after this instant, so every page reads the same data.
	DataThrough time.Time
	UseFinal    bool
}

// AnalyticsV2Line is one usage line over one window; an event belongs to it when the meter
// matches and Start <= timestamp < End. Windows of the same meter may overlap.
type AnalyticsV2Line struct {
	ID      string
	MeterID string
	Start   time.Time
	End     time.Time
	// IsMax splits the line's amounts by each row's own peak instead of quantity x rate.
	IsMax bool
}

// AnalyticsV2LineUsageParams asks for every line's full-window quantity, used to turn line costs into rates.
type AnalyticsV2LineUsageParams struct {
	Scope AnalyticsV2Scope
	Lines []AnalyticsV2Line
}

// AnalyticsV2LineUsage is one line's quantity over its whole window; results align with the request's Lines.
type AnalyticsV2LineUsage struct {
	Quantity   decimal.Decimal
	EventCount uint64
}

// AnalyticsV2Amounts is a cost split by component; Cost() is the parts minus the discount.
type AnalyticsV2Amounts struct {
	Usage              decimal.Decimal
	CommitmentUtilized decimal.Decimal
	Overage            decimal.Decimal
	TrueUp             decimal.Decimal
	Discount           decimal.Decimal
	// Free is the quantity covered by free quota.
	Free decimal.Decimal
}

func (a AnalyticsV2Amounts) Subtotal() decimal.Decimal {
	return a.Usage.Add(a.CommitmentUtilized).Add(a.Overage).Add(a.TrueUp)
}

func (a AnalyticsV2Amounts) Cost() decimal.Decimal {
	return a.Subtotal().Sub(a.Discount)
}

// AnalyticsV2Filter includes (or, with Exclude, drops) events whose key has one of Values.
type AnalyticsV2Filter struct {
	Key     string
	Exclude bool
	Values  []string
}

type AnalyticsV2SortBy string

const (
	AnalyticsV2SortByTotal      AnalyticsV2SortBy = "total"
	AnalyticsV2SortByColumn     AnalyticsV2SortBy = "column"
	AnalyticsV2SortByName       AnalyticsV2SortBy = "name"
	AnalyticsV2SortByUsage      AnalyticsV2SortBy = "usage"
	AnalyticsV2SortByEventCount AnalyticsV2SortBy = "event_count"
)

type AnalyticsV2Sort struct {
	By     AnalyticsV2SortBy
	Column string
	Desc   bool
}

// AnalyticsV2RankParams ranks rows of events priced by their line's rates.
// Rates[i] belongs to Lines[i]: per-unit rates for additive lines, whole-line amounts for MAX lines.
type AnalyticsV2RankParams struct {
	Scope     AnalyticsV2Scope
	StartTime time.Time
	EndTime   time.Time
	Lines     []AnalyticsV2Line
	Rates     []AnalyticsV2Amounts
	// MeterFeatures maps meter ID to feature ID for the feature_id key.
	MeterFeatures map[string]string
	GroupBy       []string
	Column        string
	Timezone      string
	Filters       []AnalyticsV2Filter
	Search        string
	Sort          AnalyticsV2Sort
	Limit         int
	Offset        int
}

// AnalyticsV2Measure is what a row or column adds up to.
type AnalyticsV2Measure struct {
	Amounts AnalyticsV2Amounts
	// Quantity sums additive lines; Peak is the highest MAX-line value. Meaningful only for single-meter rows.
	Quantity   decimal.Decimal
	Peak       decimal.Decimal
	EventCount uint64
}

type AnalyticsV2Row struct {
	// Group holds the row key values in GroupBy order.
	Group   []string
	Measure AnalyticsV2Measure
	Columns map[string]AnalyticsV2Measure
	// LineIndexes are the positions in Lines whose events contributed to the row.
	LineIndexes []int
}

type AnalyticsV2RankResult struct {
	Rows       []AnalyticsV2Row
	TotalCount int64
	// Totals and ColumnTotals cover every row that matched, not just the page.
	Totals       AnalyticsV2Measure
	ColumnTotals map[string]AnalyticsV2Measure
}
