package dto

import (
	"time"

	"github.com/flexprice/flexprice/internal/domain/usageanalytics"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// UsageNoneValue is the row and column value shown for events without the key. The $ prefix marks a
// system value, so it does not pass for a real property value.
const UsageNoneValue = "$none"

// UsageOtherColumn collects the amount of column values beyond MaxUsageColumns.
const UsageOtherColumn = "$other"

// MaxUsageColumns is how many column values a response lists; the rest go to UsageOtherColumn.
const MaxUsageColumns = 50

// defaultUsageRange is the range used when start_time and end_time are both omitted.
const defaultUsageRange = 7 * 24 * time.Hour

// UsageExpand names an object map a response can carry for the IDs on its rows.
type UsageExpand string

const (
	UsageExpandPrice        UsageExpand = "price"
	UsageExpandPlan         UsageExpand = "plan"
	UsageExpandAddon        UsageExpand = "addon"
	UsageExpandSubscription UsageExpand = "subscription"
	UsageExpandMeter        UsageExpand = "meter"
	UsageExpandFeature      UsageExpand = "feature"
)

// UsageQueryRequest is the body of POST /usage/analytics.
type UsageQueryRequest struct {
	ExternalCustomerID string             `json:"external_customer_id" binding:"required"`
	StartTime          *time.Time         `json:"start_time,omitempty"`
	EndTime            *time.Time         `json:"end_time,omitempty"`
	Timezone           string             `json:"timezone,omitempty"`
	MeterIDs           []string           `json:"meter_ids,omitempty"`
	FeatureIDs         []string           `json:"feature_ids,omitempty"`
	Sources            []string           `json:"sources,omitempty"`  // event sources to keep; same as a source "in" filter
	GroupBy            []string           `json:"group_by,omitempty"` // defaults to meter_id
	Columns            string             `json:"columns,omitempty"`
	Filters            []UsageQueryFilter `json:"filters,omitempty"`
	Search             string             `json:"search,omitempty"`
	Sort               *UsageQuerySort    `json:"sort,omitempty"`
	Metrics            []UsageQueryMetric `json:"metrics,omitempty"`
	Expand             []UsageExpand      `json:"expand,omitempty"`
	Deduplicate        bool               `json:"deduplicate,omitempty"`
	WindowSize         types.WindowSize   `json:"window_size,omitempty"`
	// ApplyDiscounts prices each line after its percentage coupons (forever or repeated), as the invoice does.
	ApplyDiscounts bool `json:"apply_discounts,omitempty"`
	Limit          int  `json:"limit,omitempty"`
	Offset         int  `json:"offset,omitempty"`
}

// UsageQueryFilter keeps (in) or drops (not_in) events by a key's values; UsageNoneValue matches events without the key.
type UsageQueryFilter struct {
	Key    string   `json:"key"`
	Op     string   `json:"op"`
	Values []string `json:"values"`
}

// UsageQuerySort orders rows. Order is "desc" (default) or "asc"; Column is the column value for field "column".
type UsageQuerySort struct {
	Field  string `json:"field"`
	Column string `json:"column,omitempty"`
	Order  string `json:"order,omitempty"`
}

// UsageQueryMetric is an extra per-row aggregate from a fixed list, e.g. {"type": "distinct", "key": "properties.owner"}.
type UsageQueryMetric struct {
	Type string `json:"type"`
	Key  string `json:"key"`
}

// ToQuery validates the request and converts it to a query; now fills the default range.
func (r *UsageQueryRequest) ToQuery(now time.Time) (*usageanalytics.Query, error) {
	q := &usageanalytics.Query{
		ExternalCustomerIDs: []string{r.ExternalCustomerID},
		MeterIDs:            lo.Uniq(r.MeterIDs),
		FeatureIDs:          lo.Uniq(r.FeatureIDs),
		Search:              r.Search,
		Deduplicate:         r.Deduplicate,
		WindowSize:          r.WindowSize,
		WithLineItemIDs:     true,
		Limit:               lo.Ternary(r.Limit == 0, usageanalytics.DefaultLimit, r.Limit),
		Offset:              r.Offset,
	}

	switch {
	case r.StartTime == nil && r.EndTime == nil:
		q.End = now
		q.Start = now.Add(-defaultUsageRange)
	case r.StartTime != nil && r.EndTime != nil:
		q.Start, q.End = *r.StartTime, *r.EndTime
	default:
		return nil, usageValidationErr("start_time and end_time must be set together")
	}

	tz, err := time.LoadLocation(lo.Ternary(r.Timezone == "", "UTC", r.Timezone))
	if err != nil {
		return nil, usageValidationErr("unknown timezone %q", r.Timezone)
	}
	q.Timezone = tz

	groupBy := r.GroupBy
	if len(groupBy) == 0 {
		groupBy = []string{string(usageanalytics.KeyMeterID)}
	}
	for _, s := range groupBy {
		k, err := usageanalytics.ParseKey(s)
		if err != nil {
			return nil, err
		}
		q.GroupBy = append(q.GroupBy, k)
	}
	if r.Columns != "" {
		k, err := usageanalytics.ParseKey(r.Columns)
		if err != nil {
			return nil, err
		}
		if k.Kind == usageanalytics.KeyFeatureID || k.Kind == usageanalytics.KeySubLineItemID {
			return nil, usageValidationErr("columns supports properties.*, source, meter_id and month")
		}
		q.Columns = &k
	}

	if len(r.Sources) > 0 {
		q.Filters = append(q.Filters, usageanalytics.Filter{
			Key: usageanalytics.Key{Kind: usageanalytics.KeySource}, Op: usageanalytics.FilterIn, Values: lo.Uniq(r.Sources),
		})
	}
	for _, f := range r.Filters {
		k, err := usageanalytics.ParseKey(f.Key)
		if err != nil {
			return nil, err
		}
		values := lo.Map(f.Values, func(v string, _ int) string { return lo.Ternary(v == UsageNoneValue, "", v) })
		q.Filters = append(q.Filters, usageanalytics.Filter{Key: k, Op: usageanalytics.FilterOp(f.Op), Values: values})
	}

	for _, m := range r.Metrics {
		k, err := usageanalytics.ParseKey(m.Key)
		if err != nil {
			return nil, err
		}
		q.Metrics = append(q.Metrics, usageanalytics.Metric{Type: usageanalytics.MetricType(m.Type), Key: k})
	}

	q.Sort = usageanalytics.Sort{Field: usageanalytics.SortAmount, Desc: true}
	if r.Sort != nil {
		if r.Sort.Order != "" && r.Sort.Order != "asc" && r.Sort.Order != "desc" {
			return nil, usageValidationErr("sort order must be asc or desc")
		}
		q.Sort = usageanalytics.Sort{
			Field:  usageanalytics.SortField(r.Sort.Field),
			Column: lo.Ternary(r.Sort.Column == UsageNoneValue, "", r.Sort.Column),
			Desc:   r.Sort.Order != "asc",
		}
	}

	for _, e := range r.Expand {
		switch e {
		case UsageExpandPrice, UsageExpandPlan, UsageExpandAddon, UsageExpandSubscription, UsageExpandMeter, UsageExpandFeature:
		default:
			return nil, usageValidationErr("unknown expand %q", e)
		}
	}

	if err := q.Validate(); err != nil {
		return nil, err
	}
	return q, nil
}

func usageValidationErr(format string, args ...any) error {
	return ierr.NewErrorf(format, args...).Mark(ierr.ErrValidation)
}

// UsageQueryResponse is one page of usage rows.
type UsageQueryResponse struct {
	Currency     string           `json:"currency"`
	ColumnValues []string         `json:"column_values,omitempty"`
	Totals       UsageQueryTotals `json:"totals"`
	Items        []UsageQueryItem `json:"items"`
	// Pagination.Total counts rows across all pages; Offset echoes the request.
	Pagination types.PaginationResponse `json:"pagination"`
	// UnappliedCouponIDs are coupons active in the range that apply_discounts cannot spread over events
	// (fixed amount or one-time); the invoice may be lower by them.
	UnappliedCouponIDs []string `json:"unapplied_coupon_ids,omitempty"`
	// Object maps, keyed by ID, for the IDs on this page's rows; each is set only when requested in expand.
	// Meters and features are those of the rows' prices.
	Prices        map[string]*PriceResponse        `json:"prices,omitempty"`
	Plans         map[string]*PlanResponse         `json:"plans,omitempty"`
	Addons        map[string]*AddonResponse        `json:"addons,omitempty"`
	Subscriptions map[string]*SubscriptionResponse `json:"subscriptions,omitempty"`
	Meters        map[string]*MeterResponse        `json:"meters,omitempty"`
	Features      map[string]*FeatureResponse      `json:"features,omitempty"`
}

// UsageAmounts is money rounded to the currency; Discount is Subtotal minus Amount, zero without apply_discounts.
type UsageAmounts struct {
	Subtotal string `json:"subtotal"`
	Discount string `json:"discount"`
	Amount   string `json:"amount"`
}

// UsageQueryTotals are summed over every row, not just the page.
type UsageQueryTotals struct {
	UsageAmounts
	Usage      string                 `json:"usage"`
	EventCount uint64                 `json:"event_count"`
	Columns    map[string]UsageColumn `json:"columns,omitempty"`
}

// UsageQueryItem is one row.
type UsageQueryItem struct {
	Group map[string]string `json:"group"`
	UsageAmounts
	Usage           string                     `json:"usage"`
	EventCount      uint64                     `json:"event_count"`
	Columns         map[string]UsageColumn     `json:"columns,omitempty"`
	Metrics         map[string]UsageQueryValue `json:"metrics,omitempty"`
	PriceIDs        []string                   `json:"price_ids,omitempty"`
	PlanIDs         []string                   `json:"plan_ids,omitempty"`
	AddonIDs        []string                   `json:"addon_ids,omitempty"`
	SubscriptionIDs []string                   `json:"subscription_ids,omitempty"`
	Points          []UsagePoint               `json:"points,omitempty"`
}

// UsagePoint is one time window, starting at Timestamp; every window in the range is listed, empty ones at zero.
type UsagePoint struct {
	Timestamp time.Time `json:"timestamp"`
	UsageAmounts
	Usage string `json:"usage"`
}

// UsageColumn is a row's or total's amount in one column value.
type UsageColumn struct {
	Amount string `json:"amount"`
}

// UsageQueryValue is one metric of a row; Values is set when there are at most five.
type UsageQueryValue struct {
	Count  uint64   `json:"count"`
	Values []string `json:"values,omitempty"`
}
