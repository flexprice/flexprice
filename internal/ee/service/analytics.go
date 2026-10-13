package service

import (
	"context"
	"slices"
	"sort"
	"strings"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/analytics"
	"github.com/flexprice/flexprice/internal/domain/events"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

type AnalyticsService interface {
	ExecuteView(ctx context.Context, def *analytics.ViewDefinition, vars map[string][]string) (*dto.AnalyticsQueryResult, error)
	CreateView(ctx context.Context, v *analytics.View) error
	QueryView(ctx context.Context, id string, vars map[string][]string) (*dto.AnalyticsQueryResult, error)
}

type analyticsService struct {
	ServiceParams
	views      analytics.Repository
	meterUsage MeterUsageService
}

func NewAnalyticsService(params ServiceParams) AnalyticsService {
	return &analyticsService{
		ServiceParams: params,
		views:         params.AnalyticsViewRepo,
		meterUsage:    NewMeterUsageService(params),
	}
}

func (s *analyticsService) ExecuteView(ctx context.Context, def *analytics.ViewDefinition, vars map[string][]string) (*dto.AnalyticsQueryResult, error) {
	rv, err := analytics.ResolveVariables(def, vars)
	if err != nil {
		return nil, err
	}

	if err := analytics.ValidateDimensions(rv.Dimensions); err != nil {
		return nil, err
	}

	// keep multi-meter results distinguishable by adding meter_id as a dimension
	if !slices.Contains(rv.Dimensions, "meter_id") {
		dims := make([]string, len(rv.Dimensions), len(rv.Dimensions)+1)
		copy(dims, rv.Dimensions)
		rv.Dimensions = append(dims, "meter_id")
	}

	switch rv.Shape {
	case types.ShapeBreakdown:
		return s.executeBreakdown(ctx, rv)
	case types.ShapeTimeseries:
		return s.executeTimeseries(ctx, rv)
	default:
		return nil, ierr.NewErrorf("unsupported shape %q", rv.Shape).Mark(ierr.ErrValidation)
	}
}

// executeBreakdown translates + executes a breakdown view through
// MeterUsageService.GetDetailedAnalytics, which (for admin-style queries with
// no external_customer_id) resolves each meter's own aggregation type via
// getDetailedAnalyticsWithoutSubscriptionContext. Dimensions are optional —
// an empty GroupBy yields one row per meter.
func (s *analyticsService) executeBreakdown(ctx context.Context, rv *analytics.ResolvedView) (*dto.AnalyticsQueryResult, error) {
	params, err := translateBreakdown(ctx, rv)
	if err != nil {
		return nil, err
	}

	resp, err := s.meterUsage.GetDetailedAnalytics(ctx, params)
	if err != nil {
		s.Logger.Error(ctx, "analytics breakdown execution failed", "error", err)
		return nil, err
	}

	res := shapeBreakdown(resp.Items, rv.Dimensions, rv.Metrics)
	if err := sortBreakdownRows(&res, rv.Sort); err != nil {
		return nil, err
	}
	truncateRows(&res, rv.Limit)
	return &res, nil
}

// executeTimeseries translates + executes a timeseries view through the same
// MeterUsageService.GetDetailedAnalytics engine as breakdown (unlike the
// earlier Phase-1 cut, which called MeterUsageRepo.GetUsage directly and
// required exactly one meter with no dimensions). Dimensions act as a
// split-by; multiple meters are allowed since the engine derives each
// meter's real aggregation type on its own.
func (s *analyticsService) executeTimeseries(ctx context.Context, rv *analytics.ResolvedView) (*dto.AnalyticsQueryResult, error) {
	params, err := translateTimeseries(ctx, rv)
	if err != nil {
		return nil, err
	}

	resp, err := s.meterUsage.GetDetailedAnalytics(ctx, params)
	if err != nil {
		s.Logger.Error(ctx, "analytics timeseries execution failed", "error", err)
		return nil, err
	}

	// Sort/Limit are rejected upfront for timeseries (ViewDefinition.Validate),
	// so rows here stay in their per-bucket time order. Series-level ranking
	// (top-N-series) is a future enhancement.
	res := shapeTimeseries(resp.Items, rv.Dimensions, rv.Metrics)
	return &res, nil
}

func (s *analyticsService) CreateView(ctx context.Context, v *analytics.View) error {
	if v == nil {
		return ierr.NewError("view is required").Mark(ierr.ErrValidation)
	}
	if v.Definition == nil {
		return ierr.NewError("view definition is required").Mark(ierr.ErrValidation)
	}
	if err := v.Definition.Validate(); err != nil {
		return err
	}
	if v.ID == "" {
		v.ID = types.GenerateUUIDWithPrefix(types.UUID_PREFIX_ANALYTICS_VIEW)
	}
	if v.Version == 0 {
		v.Version = 1
	}
	if err := s.views.Create(ctx, v); err != nil {
		s.Logger.Error(ctx, "failed to create analytics view", "error", err, "view_id", v.ID)
		return err
	}
	return nil
}

func (s *analyticsService) QueryView(ctx context.Context, id string, vars map[string][]string) (*dto.AnalyticsQueryResult, error) {
	v, err := s.views.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	return s.ExecuteView(ctx, v.Definition, vars)
}

// translateDimensions applies translateDimension to every dim, preserving
// order and length so the shaper can still render columns using the view's
// ORIGINAL dimension names (see dimensionValue).
func translateDimensions(dims []string) []string {
	if len(dims) == 0 {
		return nil
	}
	out := make([]string, len(dims))
	for i, d := range dims {
		switch d {
		case "customer_id":
			out[i] = "external_customer_id"
		default:
			out[i] = d
		}
	}
	return out
}

// validateFilterOp rejects any Filter.Op the engine cannot execute. The
// property/structural filter handling below only supports equality and
// set-membership: eq (including multi-valued eq) and in both map onto the
// same IN-style handling. Anything else (gt/lt/contains/...) fails loud
// rather than being silently treated as IN.
func validateFilterOp(op types.FilterOperatorType) error {
	switch op {
	case types.EQUAL, types.IN:
		return nil
	default:
		return ierr.NewErrorf("operator %q is not supported in this version", op).
			WithHint("filter operator must be one of: eq, in").
			Mark(ierr.ErrValidation)
	}
}

// applyAnalyticsFilters splits a ResolvedView's filters into the typed slices
// the meter_usage params expect (meter_id/customer_id/source);
// anything else is treated as a property filter
func applyAnalyticsFilters(filters []*analytics.Filter, meterIDs, customerIDs, sources *[]string, propertyFilters map[string][]string) error {
	for _, f := range filters {
		if f == nil {
			continue
		}
		if err := validateFilterOp(f.Op); err != nil {
			return err
		}
		switch f.Field {
		case "meter_id":
			*meterIDs = append(*meterIDs, f.Value...)
		case "customer_id", "external_customer_id":
			*customerIDs = append(*customerIDs, f.Value...)
		case "source":
			*sources = append(*sources, f.Value...)
		default:
			key := strings.TrimPrefix(f.Field, "properties.")
			propertyFilters[key] = append(propertyFilters[key], f.Value...)
		}
	}
	return nil
}

// sortBreakdownRows sorts a breakdown result's rows in place per the view's
// sort spec(s), applied in order as successive tie-breakers. Each
// SortSpec.Field must name an output column (a dimension or a metric); an
// unknown field fails loud instead of silently no-op'ing. Decimal (metric)
// columns compare numerically; everything else compares lexically. An empty
// Dir defaults to asc.
func sortBreakdownRows(res *dto.AnalyticsQueryResult, sortSpecs []*analytics.SortSpec) error {
	if res == nil || len(sortSpecs) == 0 {
		return nil
	}
	colIdx := make(map[string]int, len(res.Columns))
	colType := make(map[string]types.ColumnType, len(res.Columns))
	for i, c := range res.Columns {
		colIdx[c.Name] = i
		colType[c.Name] = c.Type
	}
	specs := make([]*analytics.SortSpec, 0, len(sortSpecs))
	for _, s := range sortSpecs {
		if s == nil {
			continue
		}
		if _, ok := colIdx[s.Field]; !ok {
			return ierr.NewErrorf("sort field %q does not match any output column", s.Field).
				WithHint("sort field must reference a dimension or metric column of the result").
				Mark(ierr.ErrValidation)
		}
		specs = append(specs, s)
	}
	sort.SliceStable(res.Rows, func(i, j int) bool {
		for _, s := range specs {
			idx := colIdx[s.Field]
			cmp := compareCells(res.Rows[i][idx], res.Rows[j][idx], colType[s.Field])
			if cmp == 0 {
				continue
			}
			if s.Dir == types.SortDirectionDesc {
				return cmp > 0
			}
			return cmp < 0
		}
		return false
	})
	return nil
}

// compareCells compares two row cells for sortBreakdownRows: decimal
// (metric) columns compare numerically by parsing the cell, everything else
// compares lexically.
func compareCells(a, b string, colType types.ColumnType) int {
	if colType == types.ColumnTypeDecimal {
		da, errA := decimal.NewFromString(a)
		db, errB := decimal.NewFromString(b)
		if errA == nil && errB == nil {
			return da.Cmp(db)
		}
	}
	return strings.Compare(a, b)
}

// truncateRows applies a breakdown view's row limit in place. limit<=0 means
// unbounded (no-op).
func truncateRows(res *dto.AnalyticsQueryResult, limit int) {
	if res == nil || limit <= 0 || limit >= len(res.Rows) {
		return
	}
	res.Rows = res.Rows[:limit]
}

// translateBreakdown maps a resolved analytics view onto the shared detailed
// meter_usage analytics params and calls MeterUsageService.GetDetailedAnalytics.
func translateBreakdown(ctx context.Context, rv *analytics.ResolvedView) (*events.MeterUsageDetailedAnalyticsParams, error) {
	if rv == nil {
		return nil, ierr.NewError("resolved view is required").Mark(ierr.ErrValidation)
	}

	p := &events.MeterUsageDetailedAnalyticsParams{
		TenantID:        types.GetTenantID(ctx),
		EnvironmentID:   types.GetEnvironmentID(ctx),
		StartTime:       rv.Time.From,
		EndTime:         rv.Time.To,
		GroupBy:         translateDimensions(rv.Dimensions),
		PropertyFilters: map[string][]string{},
		UseFinal:        true,
	}
	if err := applyAnalyticsFilters(rv.Filters, &p.MeterIDs, &p.ExternalCustomerIDs, &p.Sources, p.PropertyFilters); err != nil {
		return nil, err
	}
	return p, nil
}

// translateTimeseries maps a resolved analytics view onto the shared detailed
// meter_usage analytics params and calls MeterUsageService.GetDetailedAnalytics.
// WindowSize is derived from the view's time.grain so the response carries per-bucket Points.
func translateTimeseries(ctx context.Context, rv *analytics.ResolvedView) (*events.MeterUsageDetailedAnalyticsParams, error) {
	if rv == nil {
		return nil, ierr.NewError("resolved view is required").Mark(ierr.ErrValidation)
	}

	p := &events.MeterUsageDetailedAnalyticsParams{
		TenantID:        types.GetTenantID(ctx),
		EnvironmentID:   types.GetEnvironmentID(ctx),
		StartTime:       rv.Time.From,
		EndTime:         rv.Time.To,
		WindowSize:      rv.Time.Grain.ToWindowSize(),
		GroupBy:         translateDimensions(rv.Dimensions),
		PropertyFilters: map[string][]string{},
		UseFinal:        true,
	}
	if err := applyAnalyticsFilters(rv.Filters, &p.MeterIDs, &p.ExternalCustomerIDs, &p.Sources, p.PropertyFilters); err != nil {
		return nil, err
	}
	return p, nil
}
