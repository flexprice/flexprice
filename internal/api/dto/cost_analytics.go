package dto

import (
	"time"

	"github.com/flexprice/flexprice/internal/domain/costanalytics"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/samber/lo"
)

// CostNoneValue is the row and column value shown for events without the key.
const CostNoneValue = "(none)"

// CostOtherColumn collects the cost of column values beyond MaxCostColumns.
const CostOtherColumn = "(other)"

// MaxCostColumns is how many column values a response lists; the rest go to CostOtherColumn.
const MaxCostColumns = 50

// defaultCostRange is the range used when start_time and end_time are both omitted.
const defaultCostRange = 7 * 24 * time.Hour

// CostExpand names the IDs a response row can carry.
type CostExpand string

const (
	CostExpandPrice        CostExpand = "price"
	CostExpandPlan         CostExpand = "plan"
	CostExpandAddon        CostExpand = "addon"
	CostExpandSubscription CostExpand = "subscription"
)

// CostAnalyticsRequest is the body of POST /analytics/cost/query.
type CostAnalyticsRequest struct {
	ExternalCustomerID string                `json:"external_customer_id" binding:"required"`
	StartTime          *time.Time            `json:"start_time,omitempty"`
	EndTime            *time.Time            `json:"end_time,omitempty"`
	Timezone           string                `json:"timezone,omitempty"`
	MeterIDs           []string              `json:"meter_ids,omitempty"`
	FeatureIDs         []string              `json:"feature_ids,omitempty"`
	GroupBy            []string              `json:"group_by" binding:"required"`
	Columns            string                `json:"columns,omitempty"`
	Filters            []CostAnalyticsFilter `json:"filters,omitempty"`
	Search             string                `json:"search,omitempty"`
	Sort               *CostAnalyticsSort    `json:"sort,omitempty"`
	Metrics            []CostAnalyticsMetric `json:"metrics,omitempty"`
	Expand             []CostExpand          `json:"expand,omitempty"`
	Deduplicate        bool                  `json:"deduplicate,omitempty"`
	Limit              int                   `json:"limit,omitempty"`
	Offset             int                   `json:"offset,omitempty"`
}

// CostAnalyticsFilter keeps (in) or drops (not_in) events by a key's values; "(none)" matches events without the key.
type CostAnalyticsFilter struct {
	Key    string   `json:"key"`
	Op     string   `json:"op"`
	Values []string `json:"values"`
}

// CostAnalyticsSort orders rows. Order is "desc" (default) or "asc"; Column is the column value for field "column".
type CostAnalyticsSort struct {
	Field  string `json:"field"`
	Column string `json:"column,omitempty"`
	Order  string `json:"order,omitempty"`
}

// CostAnalyticsMetric is an extra per-row aggregate from a fixed list, e.g. {"type": "distinct", "key": "properties.owner"}.
type CostAnalyticsMetric struct {
	Type string `json:"type"`
	Key  string `json:"key"`
}

// ToQuery validates the request and converts it to a query; now fills the default range.
func (r *CostAnalyticsRequest) ToQuery(now time.Time) (*costanalytics.Query, error) {
	q := &costanalytics.Query{
		ExternalCustomerIDs: []string{r.ExternalCustomerID},
		MeterIDs:            lo.Uniq(r.MeterIDs),
		FeatureIDs:          lo.Uniq(r.FeatureIDs),
		Search:              r.Search,
		Deduplicate:         r.Deduplicate,
		WithLineItemIDs:     len(r.Expand) > 0,
		Limit:               lo.Ternary(r.Limit == 0, costanalytics.DefaultLimit, r.Limit),
		Offset:              r.Offset,
	}

	switch {
	case r.StartTime == nil && r.EndTime == nil:
		q.End = now
		q.Start = now.Add(-defaultCostRange)
	case r.StartTime != nil && r.EndTime != nil:
		q.Start, q.End = *r.StartTime, *r.EndTime
	default:
		return nil, costValidationErr("start_time and end_time must be set together")
	}

	tz, err := time.LoadLocation(lo.Ternary(r.Timezone == "", "UTC", r.Timezone))
	if err != nil {
		return nil, costValidationErr("unknown timezone %q", r.Timezone)
	}
	q.Timezone = tz

	for _, s := range r.GroupBy {
		k, err := costanalytics.ParseKey(s)
		if err != nil {
			return nil, err
		}
		q.GroupBy = append(q.GroupBy, k)
	}
	if r.Columns != "" {
		k, err := costanalytics.ParseKey(r.Columns)
		if err != nil {
			return nil, err
		}
		if k.Kind == costanalytics.KeyFeatureID || k.Kind == costanalytics.KeySubLineItemID {
			return nil, costValidationErr("columns supports properties.*, source, meter_id and month")
		}
		q.Columns = &k
	}

	for _, f := range r.Filters {
		k, err := costanalytics.ParseKey(f.Key)
		if err != nil {
			return nil, err
		}
		values := lo.Map(f.Values, func(v string, _ int) string { return lo.Ternary(v == CostNoneValue, "", v) })
		q.Filters = append(q.Filters, costanalytics.Filter{Key: k, Op: costanalytics.FilterOp(f.Op), Values: values})
	}

	for _, m := range r.Metrics {
		k, err := costanalytics.ParseKey(m.Key)
		if err != nil {
			return nil, err
		}
		q.Metrics = append(q.Metrics, costanalytics.Metric{Type: costanalytics.MetricType(m.Type), Key: k})
	}

	q.Sort = costanalytics.Sort{Field: costanalytics.SortTotal, Desc: true}
	if r.Sort != nil {
		if r.Sort.Order != "" && r.Sort.Order != "asc" && r.Sort.Order != "desc" {
			return nil, costValidationErr("sort order must be asc or desc")
		}
		q.Sort = costanalytics.Sort{
			Field:  costanalytics.SortField(r.Sort.Field),
			Column: lo.Ternary(r.Sort.Column == CostNoneValue, "", r.Sort.Column),
			Desc:   r.Sort.Order != "asc",
		}
	}

	for _, e := range r.Expand {
		switch e {
		case CostExpandPrice, CostExpandPlan, CostExpandAddon, CostExpandSubscription:
		default:
			return nil, costValidationErr("unknown expand %q", e)
		}
	}

	if err := q.Validate(); err != nil {
		return nil, err
	}
	return q, nil
}

func costValidationErr(format string, args ...any) error {
	return ierr.NewErrorf(format, args...).Mark(ierr.ErrValidation)
}

// CostAnalyticsResponse is one page of cost rows.
type CostAnalyticsResponse struct {
	Currency       string                  `json:"currency"`
	TotalCount     uint64                  `json:"total_count"`
	ColumnValues   []string                `json:"column_values,omitempty"`
	Totals         CostAnalyticsTotals     `json:"totals"`
	Items          []CostAnalyticsItem     `json:"items"`
	ExcludedMeters []CostExcludedMeter     `json:"excluded_meters"`
	ContractRows   []CostAnalyticsContract `json:"contract_rows"`
}

// CostAnalyticsTotals are summed over every row, not just the page.
type CostAnalyticsTotals struct {
	Cost       string                `json:"cost"`
	Quantity   string                `json:"quantity"`
	EventCount uint64                `json:"event_count"`
	Columns    map[string]CostColumn `json:"columns,omitempty"`
}

// CostAnalyticsItem is one row.
type CostAnalyticsItem struct {
	Group           map[string]string             `json:"group"`
	Cost            string                        `json:"cost"`
	Quantity        string                        `json:"quantity"`
	EventCount      uint64                        `json:"event_count"`
	Columns         map[string]CostColumn         `json:"columns,omitempty"`
	Metrics         map[string]CostAnalyticsValue `json:"metrics,omitempty"`
	PriceIDs        []string                      `json:"price_ids,omitempty"`
	PlanIDs         []string                      `json:"plan_ids,omitempty"`
	AddonIDs        []string                      `json:"addon_ids,omitempty"`
	SubscriptionIDs []string                      `json:"subscription_ids,omitempty"`
}

// CostColumn is a row's or total's cost in one column value.
type CostColumn struct {
	Cost string `json:"cost"`
}

// CostAnalyticsValue is one metric of a row; Values is set when there are at most five.
type CostAnalyticsValue struct {
	Count  uint64   `json:"count"`
	Values []string `json:"values,omitempty"`
}

// CostExcludedMeter is a requested meter left out of every row and total, with the line and reason.
type CostExcludedMeter struct {
	MeterID        string `json:"meter_id"`
	SubLineItemID  string `json:"sub_line_item_id,omitempty"`
	SubscriptionID string `json:"subscription_id,omitempty"`
	Reason         string `json:"reason"`
}

// CostAnalyticsContract is a named amount not caused by events, such as a commitment true-up.
type CostAnalyticsContract struct {
	Name string `json:"name"`
	Cost string `json:"cost"`
}
