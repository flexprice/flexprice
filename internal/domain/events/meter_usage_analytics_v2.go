package events

import (
	"time"

	"github.com/flexprice/flexprice/internal/types"
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
	// IsMax prices a row by its own peak (rate x peak) instead of quantity x rate.
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
// Rates[i] belongs to Lines[i]: per unit of quantity for additive lines, per unit of row peak for MAX lines.
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

// AnalyticsV2LinePeakParams asks, per MAX line, for the sum over all rows of each row's peak in the range.
// Filters are ignored on purpose, so a row's MAX cost is the same filtered or not.
type AnalyticsV2LinePeakParams struct {
	Scope     AnalyticsV2Scope
	StartTime time.Time
	EndTime   time.Time
	Lines     []AnalyticsV2Line
	GroupBy   []string
	// MeterFeatures maps meter ID to feature ID for the feature_id key.
	MeterFeatures map[string]string
}

// AnalyticsV2DetailParams asks for time points and distinct-value metrics of the page rows,
// plus per-window points across every matching row. Sort, Limit and Offset in Rank are unused.
type AnalyticsV2DetailParams struct {
	Rank AnalyticsV2RankParams
	// Rows are the page rows' group values, in GroupBy order.
	Rows          [][]string
	WindowSize    types.WindowSize
	BillingAnchor *time.Time
	// DistinctKeys are the keys whose distinct values are counted per page row.
	DistinctKeys []string
}

// AnalyticsV2PointCell is one (row, window, column, line) aggregate. Row is the index into the
// request's Rows, or -1 for the cell summed over every matching row.
type AnalyticsV2PointCell struct {
	Row         int
	WindowStart time.Time
	Column      string
	Line        int
	Quantity    decimal.Decimal
	// PeakWeight is the cell's part of its row's peak: row peak x cell peak / sum of the row's cell peaks.
	// A MAX line's cell costs rate x PeakWeight, so a row's cells add up to rate x row peak.
	PeakWeight decimal.Decimal
	// Peak is the highest event value in the cell.
	Peak       decimal.Decimal
	EventCount uint64
}

// AnalyticsV2Distinct counts a key's distinct values in a row; Values is set when there are few.
type AnalyticsV2Distinct struct {
	Count  uint64
	Values []string
}

type AnalyticsV2Detail struct {
	Points []AnalyticsV2PointCell
	// Distinct[row][key] for each page row and requested key.
	Distinct []map[string]AnalyticsV2Distinct
}
