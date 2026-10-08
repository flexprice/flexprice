package revenue

// GetRevenueAnalytics — the read surface over revenue_facts. The whole request
// is one pipeline:
//
//	1. listFactsInRange   — page every matching fact (bounded by a row budget).
//	2. splitFactsBySource — only for group_by "source": split each fact's
//	                        metrics across the event sources behind it
//	                        (revenue_facts_source.go).
//	3. revenueAggregation — fold each (fact, fraction) into a bucket keyed by
//	                        (group values, time bucket, adjustment kind) and
//	                        sum the metrics; day granularity places
//	                        whole-period facts per the allocation policy.
//	4. response           — sorted rows + the contains_allocated flag.

import (
	"context"
	"github.com/flexprice/flexprice/internal/ee/service"
	"sort"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/revenuefact"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/utils"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// analyticsRowBudget bounds the total facts one request may scan; a
// range/filter set that exceeds it must be narrowed, never scanned open-endedly.
const analyticsRowBudget = 250000

// GetRevenueAnalytics aggregates revenue_facts into the requested buckets:
// grouped by the given dimensions, at day/period/total granularity, with
// whole-period amounts placed per the allocation policy. Denied unless the
// tenant opted in via settings.
func (s *revenueService) GetRevenueAnalytics(ctx context.Context, req *dto.RevenueAnalyticsRequest) (*dto.RevenueAnalyticsResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	if err := s.requireRevenueAnalyticsEnabled(ctx); err != nil {
		return nil, err
	}

	facts, err := s.listFactsInRange(ctx, req)
	if err != nil {
		return nil, err
	}
	splits, err := s.splitFactsBySource(ctx, req, facts)
	if err != nil {
		return nil, err
	}

	agg := newRevenueAggregation(req)
	for _, f := range facts {
		for _, sh := range splits(f) {
			agg.add(f, sh.source, sh.fraction)
		}
	}
	res := agg.response()
	res.Query = req
	return res, nil
}

// listFactsInRange pages every fact matching the request's range and filters,
// stopping with a validation error once the row budget is exceeded.
func (s *revenueService) listFactsInRange(ctx context.Context, req *dto.RevenueAnalyticsRequest) ([]*revenuefact.RevenueFact, error) {
	const pageSize = 5000
	var all []*revenuefact.RevenueFact
	for offset := 0; ; offset += pageSize {
		if offset >= analyticsRowBudget {
			return nil, ierr.NewErrorf("query matches more than %d revenue fact rows", analyticsRowBudget).
				WithHint("Narrow the time range or filters").
				Mark(ierr.ErrValidation)
		}
		facts, err := s.RevenueFactRepo.ListFacts(ctx, revenuefact.FactsFilter{
			// Rows are dated at day grain, so the bounds are the requested
			// range's whole UTC days — a clock time inside a day must not
			// drop that day's rows.
			DayStart:        dayFloorUTC(req.StartTime),
			DayEnd:          dayFloorUTC(req.EndTime),
			Status:          req.Status,
			CustomerIDs:     req.CustomerIDs,
			SubscriptionIDs: req.SubscriptionIDs,
			PriceIDs:        req.PriceIDs,
			MeterIDs:        req.MeterIDs,
			Currency:        req.Currency,
			Limit:           pageSize,
			Offset:          offset,
		})
		if err != nil {
			return nil, err
		}
		all = append(all, facts...)
		if len(facts) < pageSize {
			return all, nil
		}
	}
}

// revenueAnalyticsEnabled reports whether this context's tenant and environment
// opted in. A blank tenant or environment is not an opt-in: an empty
// environment id makes subscription listing match every environment.
func (s *revenueService) revenueAnalyticsEnabled(ctx context.Context) (bool, error) {
	if types.GetTenantID(ctx) == "" || types.GetEnvironmentID(ctx) == "" {
		return false, nil
	}

	setting, err := s.SettingsRepo.GetByKey(ctx, types.SettingKeyRevenueAnalyticsConfig)
	if err != nil {
		if ierr.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}
	cfg, err := utils.ToStruct[types.RevenueAnalyticsConfig](setting.Value)
	if err != nil || !cfg.Enabled {
		return false, nil
	}
	return true, nil
}

// requireRevenueAnalyticsEnabled denies the tenant-facing read surface unless
// the tenant opted in via settings.
func (s *revenueService) requireRevenueAnalyticsEnabled(ctx context.Context) error {
	enabled, err := s.revenueAnalyticsEnabled(ctx)
	if err != nil {
		return err
	}
	if !enabled {
		return ierr.NewError("revenue analytics is not enabled for this tenant").
			WithHint("Enable the revenue_analytics_config setting to use revenue analytics").
			Mark(ierr.ErrPermissionDenied)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Aggregation
// ---------------------------------------------------------------------------

// revenueAggregation folds (fact, fraction) contributions into buckets keyed
// by (group values, time bucket, adjustment kind) and sums the metrics.
type revenueAggregation struct {
	req     *dto.RevenueAnalyticsRequest
	buckets map[string]*dto.RevenueAnalyticsRow
	// containsAllocated flips when a whole-period amount lands in a day view.
	containsAllocated bool
}

func newRevenueAggregation(req *dto.RevenueAnalyticsRequest) *revenueAggregation {
	return &revenueAggregation{req: req, buckets: map[string]*dto.RevenueAnalyticsRow{}}
}

// adjustmentType labels the non-obvious components broken out by
// include_adjustments: a revert (contra) row, a commitment true-up, or an
// overage charge. Empty means a plain row.
func adjustmentType(f *revenuefact.RevenueFact) string {
	switch {
	case f.IsRevert:
		return "revert"
	case f.RevenueSource == types.RevenueSourceCommitmentTrueup:
		return string(types.RevenueSourceCommitmentTrueup)
	case f.RevenueSource == types.RevenueSourceOverage:
		return string(types.RevenueSourceOverage)
	default:
		return ""
	}
}

// displaySource is the revenue_source a fact shows under. When adjustments
// fold (default), true-up and overage count as usage so visible rows read as
// plain usage/fixed; reverts keep their own (mapped) source.
func (a *revenueAggregation) displaySource(f *revenuefact.RevenueFact) string {
	src := f.RevenueSource
	if !a.req.IncludeAdjustments &&
		(src == types.RevenueSourceCommitmentTrueup || src == types.RevenueSourceOverage) {
		src = types.RevenueSourceUsage
	}
	return string(src)
}

// add folds fraction of f's metrics into the aggregation. source is f's
// event-source label, meaningful only when "source" is a group dimension.
func (a *revenueAggregation) add(f *revenuefact.RevenueFact, source string, fraction decimal.Decimal) {
	group := map[string]string{}
	for _, g := range a.req.GroupBy {
		switch g {
		case "revenue_source":
			group[g] = a.displaySource(f)
		case "source":
			group[g] = source
		case "customer_id":
			group[g] = f.CustomerID
		case "subscription_id":
			group[g] = f.SubscriptionID
		case "price_id":
			group[g] = lo.FromPtr(f.PriceID)
		case "meter_id":
			group[g] = lo.FromPtr(f.MeterID)
		case "currency":
			group[g] = f.Currency
		}
	}
	adjType := ""
	if a.req.IncludeAdjustments {
		adjType = adjustmentType(f)
	}

	switch a.req.Granularity {
	case types.RevenueGranularityTotal:
		a.fold(group, adjType, nil, nil, nil, f, fraction)
	case types.RevenueGranularityPeriod:
		ps, pe := f.PeriodStart, f.PeriodEnd
		a.fold(group, adjType, nil, &ps, &pe, f, fraction)
	default: // day
		if f.DecompositionMode == types.Marginal || a.req.AllocationPolicy == types.RevenueAllocationBilled {
			if f.DecompositionMode == types.PeriodOnly {
				a.containsAllocated = true
			}
			day := f.Day
			a.fold(group, adjType, &day, nil, nil, f, fraction)
			return
		}
		// Amortized: spread the whole-period row evenly across its period's
		// days, remainder on the last day so sums stay exact.
		a.containsAllocated = true
		days := int(f.PeriodEnd.Sub(f.PeriodStart).Hours()/24) + 1
		if days < 1 {
			days = 1
		}
		weights := make([]decimal.Decimal, days)
		for i := range weights {
			weights[i] = decimal.NewFromInt(1)
		}
		shares := service.SpreadAmount(fraction, weights)
		for i, share := range shares {
			day := f.PeriodStart.AddDate(0, 0, i)
			a.fold(group, adjType, &day, nil, nil, f, share)
		}
	}
}

// fold adds fraction x of f's metrics into the bucket for (group, adjustment
// kind, time bucket).
func (a *revenueAggregation) fold(group map[string]string, adjType string, day, periodStart, periodEnd *time.Time, f *revenuefact.RevenueFact, fraction decimal.Decimal) {
	keyParts := make([]string, 0, len(a.req.GroupBy)+4)
	for _, g := range a.req.GroupBy {
		keyParts = append(keyParts, group[g])
	}
	// Status is part of a row's identity: a booked row and an in-progress one
	// for the same bucket describe different things and must not be summed.
	keyParts = append(keyParts, string(f.Status))
	if day != nil {
		keyParts = append(keyParts, day.Format("2006-01-02"))
	}
	if periodStart != nil {
		keyParts = append(keyParts, periodStart.Format("2006-01-02"), periodEnd.Format("2006-01-02"))
	}
	if adjType != "" {
		keyParts = append(keyParts, adjType)
	}
	key := strings.Join(keyParts, "|")

	row, ok := a.buckets[key]
	if !ok {
		row = &dto.RevenueAnalyticsRow{
			Day: day, PeriodStart: periodStart, PeriodEnd: periodEnd,
			Status: f.Status, AdjustmentType: adjType,
		}
		if len(group) > 0 {
			row.Group = group
		}
		a.buckets[key] = row
	}
	row.NetAmount = row.NetAmount.Add(f.NetAmount.Mul(fraction))
	row.UsageAtListRate = row.UsageAtListRate.Add(f.UsageAtListRate.Mul(fraction))
	row.TierDelta = row.TierDelta.Add(f.TierDelta.Mul(fraction))
	row.EntitlementAmount = row.EntitlementAmount.Add(f.EntitlementAmount.Mul(fraction))
	row.LineDiscount = row.LineDiscount.Add(f.LineDiscount.Mul(fraction))
	row.InvoiceDiscount = row.InvoiceDiscount.Add(f.InvoiceDiscount.Mul(fraction))
	row.BillableQty = row.BillableQty.Add(f.BillableQty.Mul(fraction))
	row.EntitlementQty = row.EntitlementQty.Add(f.EntitlementQty.Mul(fraction))
}

// response sorts the buckets by time bucket first, then by group values, with
// adjustment rows after their plain siblings.
// emptyRow reports whether a bucket carries no value at all. Days a line item
// was rolled over but saw nothing — before a commitment is crossed, or a meter
// that was idle — produce such buckets, and they are noise in a response. A
// day of fully entitled usage is NOT empty: its quantity columns say what the
// entitlement absorbed.
func emptyRow(r *dto.RevenueAnalyticsRow) bool {
	for _, v := range []decimal.Decimal{
		r.NetAmount, r.UsageAtListRate, r.TierDelta, r.EntitlementAmount,
		r.LineDiscount, r.InvoiceDiscount, r.BillableQty, r.EntitlementQty,
	} {
		if !v.IsZero() {
			return false
		}
	}
	return true
}

func (a *revenueAggregation) response() *dto.RevenueAnalyticsResponse {
	rows := make([]*dto.RevenueAnalyticsRow, 0, len(a.buckets))
	for _, row := range a.buckets {
		if emptyRow(row) {
			continue
		}
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool {
		ti, tj := rowSortTime(rows[i]), rowSortTime(rows[j])
		if !ti.Equal(tj) {
			return ti.Before(tj)
		}
		return rowSortKey(rows[i]) < rowSortKey(rows[j])
	})
	return &dto.RevenueAnalyticsResponse{Rows: rows, ContainsAllocated: a.containsAllocated}
}

func rowSortTime(r *dto.RevenueAnalyticsRow) time.Time {
	if r.Day != nil {
		return *r.Day
	}
	if r.PeriodStart != nil {
		return *r.PeriodStart
	}
	return time.Time{}
}

func rowSortKey(r *dto.RevenueAnalyticsRow) string {
	parts := make([]string, 0, len(r.Group)+1)
	keys := make([]string, 0, len(r.Group))
	for k := range r.Group {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		parts = append(parts, r.Group[k])
	}
	parts = append(parts, string(r.Status))
	if r.AdjustmentType != "" {
		// The "~" prefix sorts after every printable group value, keeping
		// adjustment rows below their plain siblings in the same time bucket.
		parts = append(parts, "~"+r.AdjustmentType)
	}
	return strings.Join(parts, "|")
}
