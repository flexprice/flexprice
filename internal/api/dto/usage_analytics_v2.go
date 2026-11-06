package dto

import (
	"fmt"
	"math"
	"regexp"
	"strings"
	"time"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/validator"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

const (
	AnalyticsV2MaxRowKeys      = 3
	AnalyticsV2DefaultLimit    = 25
	AnalyticsV2MaxLimit        = 100
	AnalyticsV2MaxWindows      = 400
	AnalyticsV2MaxColumns      = 50
	AnalyticsV2MaxMetrics      = 3
	AnalyticsV2MaxSearchLength = 100
	// Filter count and values per filter are bounded by validate tags (max=20, max=1000) so the spec shows them.
	AnalyticsV2MaxFilters      = 20
	AnalyticsV2MaxFilterValues = 1000
	AnalyticsV2DefaultRange    = 7 * 24 * time.Hour
	// AnalyticsV2MaxRange bounds a request because every billing period in range is priced with the invoice code.
	AnalyticsV2MaxRange = 366 * 24 * time.Hour

	// AnalyticsV2KeyMonth is the time column key: one column per calendar month in the range.
	AnalyticsV2KeyMonth = "month"
	// AnalyticsV2MetricDistinctPrefix requests a count of distinct values of a key per row, e.g. "distinct:properties.owner".
	AnalyticsV2MetricDistinctPrefix = "distinct:"
	// AnalyticsV2MaxDistinctValues is how many distinct values are listed before only the count is returned.
	AnalyticsV2MaxDistinctValues = 5
)

var propertyNamePattern = regexp.MustCompile(`^[A-Za-z0-9_.]+$`)

// Keys that identify a line rather than an event property; grouping by one of them makes rows single-meter.
var lineKeys = []string{"meter_id", "feature_id", "sub_line_item_id"}

type AnalyticsV2FilterOp string

const (
	AnalyticsV2FilterOpIn    AnalyticsV2FilterOp = "in"
	AnalyticsV2FilterOpNotIn AnalyticsV2FilterOp = "not_in"
)

type AnalyticsV2SortBy string

const (
	AnalyticsV2SortByTotal      AnalyticsV2SortBy = "total"
	AnalyticsV2SortByColumn     AnalyticsV2SortBy = "column"
	AnalyticsV2SortByName       AnalyticsV2SortBy = "name"
	AnalyticsV2SortByUsage      AnalyticsV2SortBy = "usage"
	AnalyticsV2SortByEventCount AnalyticsV2SortBy = "event_count"
)

// GetUsageAnalyticsV2Request is the request for POST /v1/events/analytics/query.
type GetUsageAnalyticsV2Request struct {
	ExternalCustomerID string    `json:"external_customer_id" validate:"required" example:"cust_123"`
	StartTime          time.Time `json:"start_time,omitempty" example:"2026-08-01T00:00:00Z"`
	EndTime            time.Time `json:"end_time,omitempty" example:"2026-09-01T00:00:00Z"`
	FeatureIDs         []string  `json:"feature_ids,omitempty"`
	MeterIDs           []string  `json:"meter_ids,omitempty"`
	// GroupBy lists 1-3 row keys: "source", "meter_id", "feature_id", "sub_line_item_id" or "properties.<name>".
	GroupBy []string `json:"group_by" validate:"required,min=1,max=3" example:"properties.app"`
	// Columns spreads one key's values into columns on every row; also accepts "month".
	Columns string `json:"columns,omitempty" example:"properties.type"`
	// Filters include (in) or exclude (not_in) values of any key; entries are ANDed.
	Filters []AnalyticsV2Filter `json:"filters,omitempty" validate:"omitempty,max=20,dive"`
	// Search is a case-insensitive contains match on the row key values.
	Search     string           `json:"search,omitempty"`
	Sort       *AnalyticsV2Sort `json:"sort,omitempty"`
	Limit      int              `json:"limit,omitempty" example:"25"`
	Offset     int              `json:"offset,omitempty" example:"0"`
	WindowSize types.WindowSize `json:"window_size,omitempty" example:"DAY"`
	// Expand adds price, plan, addon and subscription objects; meter and feature need a meter_id, feature_id or sub_line_item_id row key.
	Expand      []string `json:"expand,omitempty"`
	Metrics     []string `json:"metrics,omitempty" example:"distinct:properties.owner"`
	Deduplicate bool     `json:"deduplicate,omitempty"`
}

type AnalyticsV2Filter struct {
	Key    string              `json:"key" validate:"required" example:"properties.app"`
	Op     AnalyticsV2FilterOp `json:"op" validate:"required" example:"not_in"`
	Values []string            `json:"values" validate:"required,min=1,max=1000"`
}

type AnalyticsV2Sort struct {
	By AnalyticsV2SortBy `json:"by" example:"total"`
	// Column is the column value to sort by when By is "column", e.g. "run" or "2026-08".
	Column string              `json:"column,omitempty"`
	Order  types.SortDirection `json:"order,omitempty" example:"desc"`
}

// ApplyDefaults fills the range, paging and sort defaults; call before Validate.
func (r *GetUsageAnalyticsV2Request) ApplyDefaults(now time.Time) {
	if r.EndTime.IsZero() {
		r.EndTime = now
	}
	if r.StartTime.IsZero() {
		r.StartTime = r.EndTime.Add(-AnalyticsV2DefaultRange)
	}
	r.StartTime = r.StartTime.UTC()
	r.EndTime = r.EndTime.UTC()
	if r.Limit == 0 {
		r.Limit = AnalyticsV2DefaultLimit
	}
	if r.Sort == nil {
		r.Sort = &AnalyticsV2Sort{}
	}
	if r.Sort.By == "" {
		r.Sort.By = AnalyticsV2SortByTotal
	}
	if r.Sort.Order == "" {
		r.Sort.Order = types.SortDirectionDesc
	}
}

func (r *GetUsageAnalyticsV2Request) Validate() error {
	// Keys first, so a wrong group_by gets its own message instead of the generic min/max tag error.
	if err := r.validateKeys(); err != nil {
		return err
	}
	if err := validator.ValidateRequest(r); err != nil {
		return err
	}
	if !r.EndTime.After(r.StartTime) {
		return validationError("end_time must be after start_time", nil)
	}
	if r.EndTime.Sub(r.StartTime) > AnalyticsV2MaxRange {
		return validationError("the range between start_time and end_time is limited to 366 days", nil)
	}
	if err := r.validateFilters(); err != nil {
		return err
	}
	if err := r.validatePaging(); err != nil {
		return err
	}
	if err := r.validateSort(); err != nil {
		return err
	}
	if err := r.validateWindows(); err != nil {
		return err
	}
	if err := r.validateExpand(); err != nil {
		return err
	}
	return r.validateMetrics()
}

// SingleMeter reports whether every row is guaranteed to hold one meter, so usage is in one unit.
func (r *GetUsageAnalyticsV2Request) SingleMeter() bool {
	if len(r.MeterIDs)+len(r.FeatureIDs) == 1 {
		return true
	}
	return lo.SomeBy(r.GroupBy, func(k string) bool { return lo.Contains(lineKeys, k) })
}

func (r *GetUsageAnalyticsV2Request) validateKeys() error {
	if len(r.GroupBy) == 0 || len(r.GroupBy) > AnalyticsV2MaxRowKeys {
		return validationError(fmt.Sprintf("group_by needs 1 to %d keys", AnalyticsV2MaxRowKeys), map[string]any{"group_by": r.GroupBy})
	}
	if len(lo.Uniq(r.GroupBy)) != len(r.GroupBy) {
		return validationError("group_by has duplicate keys", map[string]any{"group_by": r.GroupBy})
	}
	for _, k := range r.GroupBy {
		if !isValidKey(k) {
			return validationError(fmt.Sprintf("group_by key %q is not supported", k), map[string]any{"key": k})
		}
	}
	// Rows name their values without the "properties." prefix, so "source" and "properties.source" would collide.
	names := lo.Map(r.GroupBy, func(k string, _ int) string { return strings.TrimPrefix(k, "properties.") })
	if len(lo.Uniq(names)) != len(names) {
		return validationError("group_by keys must have distinct names once the properties. prefix is dropped", map[string]any{"group_by": r.GroupBy})
	}
	if r.Columns == "" {
		return nil
	}
	if r.Columns != AnalyticsV2KeyMonth && !isValidKey(r.Columns) {
		return validationError(fmt.Sprintf("columns key %q is not supported", r.Columns), map[string]any{"columns": r.Columns})
	}
	if lo.Contains(r.GroupBy, r.Columns) {
		return validationError("columns key cannot also be a group_by key", map[string]any{"columns": r.Columns})
	}
	return nil
}

func (r *GetUsageAnalyticsV2Request) validateFilters() error {
	for _, f := range r.Filters {
		if !isValidKey(f.Key) {
			return validationError(fmt.Sprintf("filter key %q is not supported", f.Key), map[string]any{"key": f.Key})
		}
		if f.Op != AnalyticsV2FilterOpIn && f.Op != AnalyticsV2FilterOpNotIn {
			return validationError(fmt.Sprintf("filter op %q is not supported (use in or not_in)", f.Op), map[string]any{"op": f.Op})
		}
	}
	if len(r.Search) > AnalyticsV2MaxSearchLength {
		return validationError(fmt.Sprintf("search is limited to %d characters", AnalyticsV2MaxSearchLength), nil)
	}
	return nil
}

func (r *GetUsageAnalyticsV2Request) validatePaging() error {
	if r.Limit < 1 || r.Limit > AnalyticsV2MaxLimit {
		return validationError(fmt.Sprintf("limit must be between 1 and %d", AnalyticsV2MaxLimit), map[string]any{"limit": r.Limit})
	}
	if r.Offset < 0 {
		return validationError("offset cannot be negative", map[string]any{"offset": r.Offset})
	}
	return nil
}

func (r *GetUsageAnalyticsV2Request) validateSort() error {
	s := r.Sort
	if s.Order != types.SortDirectionAsc && s.Order != types.SortDirectionDesc {
		return validationError(fmt.Sprintf("sort order %q is not supported (use asc or desc)", s.Order), nil)
	}
	switch s.By {
	case AnalyticsV2SortByTotal, AnalyticsV2SortByName, AnalyticsV2SortByEventCount:
		return nil
	case AnalyticsV2SortByColumn:
		if r.Columns == "" || s.Column == "" {
			return validationError("sort by column needs columns in the request and sort.column", nil)
		}
		return nil
	case AnalyticsV2SortByUsage:
		if !r.SingleMeter() {
			return validationError("sort by usage needs a single meter: pass one meter or feature id, or group by meter_id", nil)
		}
		return nil
	default:
		return validationError(fmt.Sprintf("sort by %q is not supported", s.By), nil)
	}
}

func (r *GetUsageAnalyticsV2Request) validateWindows() error {
	if r.WindowSize == "" {
		return nil
	}
	if err := r.WindowSize.Validate(); err != nil {
		return err
	}
	windows := math.Ceil(r.EndTime.Sub(r.StartTime).Minutes() / float64(r.WindowSize.ToMinutes()))
	if windows > AnalyticsV2MaxWindows {
		return validationError(fmt.Sprintf("range has %d %s windows; the limit is %d, use a larger window_size", int(windows), r.WindowSize, AnalyticsV2MaxWindows),
			map[string]any{"window_size": r.WindowSize})
	}
	return nil
}

func (r *GetUsageAnalyticsV2Request) validateExpand() error {
	lineRows := lo.SomeBy(r.GroupBy, func(k string) bool { return lo.Contains(lineKeys, k) })
	for _, e := range r.Expand {
		switch e {
		case "price", "plan", "addon", "subscription":
		case "meter", "feature":
			if !lineRows {
				return validationError(fmt.Sprintf("expand %q needs meter_id, feature_id or sub_line_item_id in group_by", e), nil)
			}
		default:
			return validationError(fmt.Sprintf("expand %q is not supported (use price, plan, addon, subscription, meter or feature)", e), nil)
		}
	}
	return nil
}

func (r *GetUsageAnalyticsV2Request) validateMetrics() error {
	if len(r.Metrics) > AnalyticsV2MaxMetrics {
		return validationError(fmt.Sprintf("at most %d metrics are allowed", AnalyticsV2MaxMetrics), nil)
	}
	for _, m := range r.Metrics {
		key, ok := strings.CutPrefix(m, AnalyticsV2MetricDistinctPrefix)
		if !ok || !isValidKey(key) {
			return validationError(fmt.Sprintf("metric %q is not supported (use distinct:<key>)", m), nil)
		}
		if lo.Contains(r.GroupBy, key) {
			return validationError(fmt.Sprintf("metric %q counts a group_by key, which is always 1", m), nil)
		}
	}
	return nil
}

func isValidKey(k string) bool {
	if k == "source" || lo.Contains(lineKeys, k) {
		return true
	}
	name, ok := strings.CutPrefix(k, "properties.")
	return ok && propertyNamePattern.MatchString(name)
}

func validationError(msg string, details map[string]any) error {
	e := ierr.NewError(msg).WithHint(msg)
	if details != nil {
		e = e.WithReportableDetails(details)
	}
	return e.Mark(ierr.ErrValidation)
}

// GetUsageAnalyticsV2Response is one sorted page of rows plus totals over the full filtered set.
type GetUsageAnalyticsV2Response struct {
	Currency string `json:"currency"`
	// PriceUnit is the custom price unit (e.g. credits) when every billed line is priced in it; rows and
	// totals then also carry price_unit_cost.
	PriceUnit  string `json:"price_unit,omitempty"`
	TotalCount int64  `json:"total_count"`
	// DataThrough is when the event scans were cut off: events received later are not split across rows.
	// It is a watermark, not a billing snapshot: invoice pricing reads usage as of the request, so events
	// received during the request can be billed but not yet split.
	DataThrough time.Time `json:"data_through"`
	// ColumnValues lists every column across the full result, so every page has the same header.
	ColumnValues []string                `json:"column_values,omitempty"`
	Totals       AnalyticsV2Totals       `json:"totals"`
	Items        []AnalyticsV2Item       `json:"items"`
	Unattributed AnalyticsV2Unattributed `json:"unattributed"`
	// Object maps are filled only for the values requested in expand.
	Meters        map[string]*MeterResponse        `json:"meters,omitempty"`
	Prices        map[string]*PriceResponse        `json:"prices,omitempty"`
	Features      map[string]*FeatureResponse      `json:"features,omitempty"`
	Plans         map[string]*PlanResponse         `json:"plans,omitempty"`
	Addons        map[string]*AddonResponse        `json:"addons,omitempty"`
	Subscriptions map[string]*SubscriptionResponse `json:"subscriptions,omitempty"`
}

// AnalyticsV2Totals: Cost = RowsCost + UnattributedCost = Subtotal - Discount,
// and the Breakdown parts add up to Subtotal.
type AnalyticsV2Totals struct {
	Cost             decimal.Decimal              `json:"cost" swaggertype:"string"`
	Subtotal         decimal.Decimal              `json:"subtotal" swaggertype:"string"`
	Discount         decimal.Decimal              `json:"discount" swaggertype:"string"`
	RowsCost         decimal.Decimal              `json:"rows_cost" swaggertype:"string"`
	UnattributedCost decimal.Decimal              `json:"unattributed_cost" swaggertype:"string"`
	PriceUnitCost    *decimal.Decimal             `json:"price_unit_cost,omitempty" swaggertype:"string"`
	Breakdown        AnalyticsV2Cost              `json:"breakdown"`
	Usage            *decimal.Decimal             `json:"usage,omitempty" swaggertype:"string"`
	FreeUsage        *decimal.Decimal             `json:"free_usage,omitempty" swaggertype:"string"`
	Columns          map[string]AnalyticsV2Column `json:"columns,omitempty"`
	Points           []AnalyticsV2Point           `json:"points,omitempty"`
}

// AnalyticsV2Cost splits a cost into its parts; the parts add up to the cost.
type AnalyticsV2Cost struct {
	Usage              decimal.Decimal `json:"usage" swaggertype:"string"`
	CommitmentUtilized decimal.Decimal `json:"commitment_utilized" swaggertype:"string"`
	Overage            decimal.Decimal `json:"overage" swaggertype:"string"`
	TrueUp             decimal.Decimal `json:"true_up" swaggertype:"string"`
}

type AnalyticsV2Item struct {
	Group    map[string]string `json:"group"`
	Cost     decimal.Decimal   `json:"cost" swaggertype:"string"`
	Subtotal decimal.Decimal   `json:"subtotal" swaggertype:"string"`
	Discount decimal.Decimal   `json:"discount" swaggertype:"string"`
	// PriceUnitCost is Cost in the response's price_unit, converted before rounding.
	PriceUnitCost *decimal.Decimal `json:"price_unit_cost,omitempty" swaggertype:"string"`
	Breakdown     AnalyticsV2Cost  `json:"breakdown"`
	// Usage and FreeUsage are set only when the row holds a single meter.
	Usage      *decimal.Decimal             `json:"usage,omitempty" swaggertype:"string"`
	FreeUsage  *decimal.Decimal             `json:"free_usage,omitempty" swaggertype:"string"`
	EventCount uint64                       `json:"event_count"`
	Columns    map[string]AnalyticsV2Column `json:"columns,omitempty"`
	// Metrics is keyed by the requested metric string, e.g. "distinct:properties.owner".
	Metrics map[string]AnalyticsV2MetricValue `json:"metrics,omitempty"`
	Points  []AnalyticsV2Point                `json:"points,omitempty"`
	// A row can span several lines (a price change, two subscriptions), so these are lists.
	PriceIDs        []string             `json:"price_ids,omitempty"`
	SubscriptionIDs []string             `json:"subscription_ids,omitempty"`
	PlanIDs         []string             `json:"plan_ids,omitempty"`
	AddonIDs        []string             `json:"addon_ids,omitempty"`
	Unit            string               `json:"unit,omitempty"`
	UnitPlural      string               `json:"unit_plural,omitempty"`
	ReportingUnit   *types.ReportingUnit `json:"reporting_unit,omitempty"`
}

type AnalyticsV2Column struct {
	Cost  decimal.Decimal  `json:"cost" swaggertype:"string"`
	Usage *decimal.Decimal `json:"usage,omitempty" swaggertype:"string"`
}

// AnalyticsV2Point is one window of a row (or of all rows): Cost = Subtotal - Discount, and the Breakdown
// parts add up to Subtotal.
type AnalyticsV2Point struct {
	WindowStart time.Time       `json:"window_start"`
	Cost        decimal.Decimal `json:"cost" swaggertype:"string"`
	Subtotal    decimal.Decimal `json:"subtotal" swaggertype:"string"`
	Discount    decimal.Decimal `json:"discount" swaggertype:"string"`
	Breakdown   AnalyticsV2Cost `json:"breakdown"`
	// Usage is set only when the row holds a single meter.
	Usage      *decimal.Decimal             `json:"usage,omitempty" swaggertype:"string"`
	EventCount uint64                       `json:"event_count"`
	Columns    map[string]AnalyticsV2Column `json:"columns,omitempty"`
}

// AnalyticsV2Unattributed is invoice cost no row's usage can carry, e.g. unused commitment.
type AnalyticsV2Unattributed struct {
	Cost      decimal.Decimal `json:"cost" swaggertype:"string"`
	Breakdown AnalyticsV2Cost `json:"breakdown"`
	// Charges lists each billed charge no row carries, e.g. one per unused commitment.
	Charges []AnalyticsV2UnattributedCharge `json:"charges,omitempty"`
}

type AnalyticsV2UnattributedReason string

const (
	AnalyticsV2ReasonUnusedCommitment AnalyticsV2UnattributedReason = "unused_commitment"
	// AnalyticsV2ReasonNoUsage is billed usage cost on a line with no usage in the visible events.
	AnalyticsV2ReasonNoUsage AnalyticsV2UnattributedReason = "no_usage"
)

type AnalyticsV2UnattributedCharge struct {
	SubscriptionID string                        `json:"subscription_id,omitempty"`
	PlanID         string                        `json:"plan_id,omitempty"`
	AddonID        string                        `json:"addon_id,omitempty"`
	MeterID        string                        `json:"meter_id,omitempty"`
	SubLineItemID  string                        `json:"sub_line_item_id,omitempty"`
	PriceID        string                        `json:"price_id,omitempty"`
	Cost           decimal.Decimal               `json:"cost" swaggertype:"string"`
	Breakdown      AnalyticsV2Cost               `json:"breakdown"`
	Reason         AnalyticsV2UnattributedReason `json:"reason"`
}

// AnalyticsV2MetricValue holds one metric's result; distinct metrics set Count and, when small, Values.
type AnalyticsV2MetricValue struct {
	Count  *uint64  `json:"count,omitempty"`
	Values []string `json:"values,omitempty"`
}
