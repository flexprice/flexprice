package service

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

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
	ListReports(ctx context.Context) (*dto.ReportCatalogResponse, error)
	RunReport(ctx context.Context, key string, req *dto.ReportQueryRequest) (*dto.AnalyticsQueryResult, error)
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
	if rv.Family == types.FamilyBilling {
		return s.executeBilling(ctx, rv)
	}
	return s.executeUsage(ctx, rv)
}

func (s *analyticsService) executeUsage(ctx context.Context, rv *analytics.ResolvedView) (*dto.AnalyticsQueryResult, error) {
	if err := analytics.ValidateDimensions(rv.Dimensions); err != nil {
		return nil, err
	}

	// keep meters distinguishable
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

func (s *analyticsService) executeBilling(ctx context.Context, rv *analytics.ResolvedView) (*dto.AnalyticsQueryResult, error) {
	if err := s.requireReports(); err != nil {
		return nil, err
	}
	out, err := s.ReportsRepo.RunReport(ctx, rv, types.GetTenantID(ctx), types.GetEnvironmentID(ctx))
	if err != nil {
		if s.Logger != nil {
			s.Logger.Error(ctx, "billing analytics execution failed", "error", err)
		}
		return nil, err
	}
	rows := out.Rows
	if rows == nil {
		rows = [][]string{}
	}

	res := dto.AnalyticsQueryResult{Rows: rows, Meta: dto.AnalyticsQueryMeta{QuerySource: "billing"}}
	res.Meta.From = rv.Time.From.UTC().Format(time.RFC3339)
	res.Meta.To = rv.Time.To.UTC().Format(time.RFC3339)
	if rv.Compare != "" {
		pf, pt, _, err := analytics.PreviousWindow(rv.Time.From, rv.Time.To, rv.Compare)
		if err != nil {
			return nil, err
		}
		res.Meta.PrevFrom = pf.UTC().Format(time.RFC3339)
		res.Meta.PrevTo = pt.UTC().Format(time.RFC3339)
	}
	for _, c := range out.Columns {
		res.Columns = append(res.Columns, dto.NewAnalyticsColumn(c.Name, c.Type, c.Role, ""))
	}
	if rv.Shape == types.ShapeBreakdown {
		if err := sortBreakdownRows(&res, rv.Sort); err != nil {
			return nil, err
		}
		truncateRows(&res, rv.Limit)
	}
	return &res, nil
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

func (s *analyticsService) requireReports() error {
	if s.ReportsRepo == nil || !s.ReportsRepo.Enabled() {
		return ierr.NewError("financial reports are not enabled").
			WithHint("Financial reports are not configured for this deployment").
			Mark(ierr.ErrPermissionDenied)
	}
	return nil
}

func (s *analyticsService) ListReports(_ context.Context) (*dto.ReportCatalogResponse, error) {
	if err := s.requireReports(); err != nil {
		return nil, err
	}
	out := &dto.ReportCatalogResponse{Items: []*dto.ReportCatalogEntry{}}
	for _, e := range analytics.ReportCatalog() {
		item := &dto.ReportCatalogEntry{
			Key:             string(e.Key),
			Name:            e.Name,
			Description:     e.Description,
			Category:        e.Category,
			Kind:            e.Kind,
			ValueType:       e.ValueType,
			HigherIsBetter:  e.HigherIsBetter,
			DefaultShape:    string(e.DefaultShape),
			SupportsCompare: e.SupportsCompare,
			VariantOf:       string(e.VariantOf),
			VariantLabel:    e.VariantLabel,
			MaxRangeDays:    e.MaxRangeDays,
			Shapes:          []string{},
			Dimensions:      []dto.ReportDimension{},
			Grains:          []string{},
		}
		for _, sh := range e.Shapes {
			item.Shapes = append(item.Shapes, string(sh))
		}
		for _, d := range e.Dimensions {
			item.Dimensions = append(item.Dimensions, dto.ReportDimension{Key: d.Key, Label: d.Label})
		}
		for _, g := range e.Grains {
			item.Grains = append(item.Grains, string(g))
		}
		out.Items = append(out.Items, item)
	}
	return out, nil
}

func validateReportRequest(e *analytics.ReportCatalogEntry, req *dto.ReportQueryRequest, maxRows int) error {
	shapes := make([]string, len(e.Shapes))
	for i, sh := range e.Shapes {
		shapes[i] = string(sh)
	}
	if !slices.Contains(shapes, req.Shape) {
		return ierr.NewErrorf("shape %q not allowed", req.Shape).
			WithHint("shape must be one of: " + strings.Join(shapes, ", ")).
			Mark(ierr.ErrValidation)
	}
	dims := make([]string, len(e.Dimensions))
	for i, d := range e.Dimensions {
		dims[i] = d.Key
	}
	for _, d := range req.Dimensions {
		if !slices.Contains(dims, d) {
			return ierr.NewErrorf("dimension %q not allowed", d).
				WithHint("dimensions must be one of: " + strings.Join(dims, ", ")).
				Mark(ierr.ErrValidation)
		}
	}
	if req.Grain != "" && req.Shape != string(types.ShapeTimeseries) {
		return ierr.NewError("grain only applies to timeseries").
			WithHint("omit grain unless shape is timeseries").
			Mark(ierr.ErrValidation)
	}
	if req.Limit < 0 || (maxRows > 0 && req.Limit > maxRows) {
		return ierr.NewErrorf("limit %d not allowed", req.Limit).
			WithHint(fmt.Sprintf("limit must be between 0 and %d (0 = no limit)", maxRows)).
			Mark(ierr.ErrValidation)
	}
	if req.Limit > 0 && req.Shape != string(types.ShapeBreakdown) {
		return ierr.NewError("limit only applies to breakdown").
			WithHint("omit limit unless shape is breakdown").
			Mark(ierr.ErrValidation)
	}
	bm, _ := analytics.LookupBillingMetric(e.Key)
	hasCurrency := bm != nil && bm.Currency
	if req.Currency != "" && !hasCurrency {
		return ierr.NewError("report has no currency").
			WithHint("omit currency for this report").
			Mark(ierr.ErrValidation)
	}
	if len(req.Sort) > 0 {
		if req.Shape != string(types.ShapeBreakdown) {
			return ierr.NewError("sort only applies to breakdown").
				WithHint("omit sort unless shape is breakdown").
				Mark(ierr.ErrValidation)
		}
		fields := append(slices.Clone(dims), string(e.Key))
		if req.Compare != "" {
			fields = append(fields, string(e.Key)+"_prev")
		}
		for _, so := range req.Sort {
			if so.Dir != "asc" && so.Dir != "desc" {
				return ierr.NewErrorf("sort dir %q not allowed", so.Dir).
					WithHint("dir must be asc or desc").
					Mark(ierr.ErrValidation)
			}
			if !slices.Contains(fields, so.Field) {
				return ierr.NewErrorf("sort field %q not allowed", so.Field).
					WithHint("sort field must be one of: " + strings.Join(fields, ", ")).
					Mark(ierr.ErrValidation)
			}
		}
	}
	if req.Shape == string(types.ShapeTimeseries) && req.Grain != "" {
		grains := make([]string, len(e.Grains))
		for i, g := range e.Grains {
			grains[i] = string(g)
		}
		if !slices.Contains(grains, req.Grain) {
			return ierr.NewErrorf("grain %q not allowed", req.Grain).
				WithHint("grain must be one of: " + strings.Join(grains, ", ")).
				Mark(ierr.ErrValidation)
		}
	}
	if req.Compare != "" {
		if !e.SupportsCompare || (req.Compare != "previous_period" && req.Compare != "previous_year") {
			return ierr.NewErrorf("compare %q not allowed", req.Compare).
				WithHint("compare must be previous_period or previous_year").
				Mark(ierr.ErrValidation)
		}
	}
	for _, f := range req.Filters {
		if !slices.Contains(dims, f.Field) && !(f.Field == "currency" && hasCurrency) {
			return ierr.NewErrorf("filter field %q not allowed", f.Field).
				WithHint("filter fields must be one of: currency, " + strings.Join(dims, ", ")).
				Mark(ierr.ErrValidation)
		}
		if (f.Op != "eq" && f.Op != "in") || len(f.Value) == 0 || (f.Op == "eq" && len(f.Value) != 1) {
			return ierr.NewErrorf("filter on %q invalid", f.Field).
				WithHint("op must be eq (1 value) or in (1+ values)").
				Mark(ierr.ErrValidation)
		}
	}
	return nil
}

func (s *analyticsService) RunReport(ctx context.Context, key string, req *dto.ReportQueryRequest) (*dto.AnalyticsQueryResult, error) {
	if err := s.requireReports(); err != nil {
		return nil, err
	}
	entry, ok := analytics.LookupReport(key)
	if !ok {
		return nil, ierr.NewErrorf("report %q not found", key).Mark(ierr.ErrNotFound)
	}
	if err := validateReportRequest(entry, req, s.ReportsRepo.MaxRows()); err != nil {
		return nil, err
	}

	if req.Shape == string(types.ShapeTimeseries) && req.Grain == "" && len(entry.Grains) > 0 {
		req.Grain = string(entry.Grains[0])
	}
	if entry.Key == types.MetricEventsVolume && req.Shape == string(types.ShapeBreakdown) && len(req.Dimensions) == 0 {
		req.Dimensions = []string{"meter_id"}
	}

	def := analytics.ViewDefinition{
		Family:     types.FamilyBilling,
		Shape:      types.Shape(req.Shape),
		Metrics:    []types.Metric{entry.Key},
		Dimensions: req.Dimensions,
		Time:       analytics.TimeSpecRaw{Range: req.Range, Grain: types.Grain(req.Grain)},
		Limit:      req.Limit,
	}
	for _, f := range req.Filters {
		def.Filters = append(def.Filters, &analytics.Filter{Field: f.Field, Op: types.FilterOperatorType(f.Op), Value: f.Value})
	}
	if req.Currency != "" {
		def.Filters = append(def.Filters, &analytics.Filter{Field: "currency", Op: types.EQUAL, Value: []string{req.Currency}})
	}
	for _, so := range req.Sort {
		def.Sort = append(def.Sort, &analytics.SortSpec{Field: so.Field, Dir: types.SortDirection(so.Dir)})
	}

	rv, err := analytics.ResolveVariables(&def, nil)
	if err != nil {
		return nil, err
	}
	if d := entry.MaxRangeDays; d > 0 && rv.Time.To.Sub(rv.Time.From) > time.Duration(d)*24*time.Hour {
		return nil, ierr.NewErrorf("range exceeds %d days for this report", d).
			WithHint(fmt.Sprintf("Choose a range of %d days or less", d)).
			Mark(ierr.ErrValidation)
	}
	rv.Compare = req.Compare
	res, err := s.executeBilling(ctx, rv)
	if err != nil {
		return nil, err
	}
	if req.Compare != "" {
		addChangePct(res, string(entry.Key))
	}
	return res, nil
}

func addChangePct(res *dto.AnalyticsQueryResult, key string) {
	cur, prev := -1, -1
	for i, c := range res.Columns {
		switch c.Name {
		case key:
			cur = i
		case key + "_prev":
			prev = i
		}
	}
	if cur < 0 || prev < 0 {
		return
	}
	res.Columns = append(res.Columns, dto.NewAnalyticsColumn(key+"_change_pct", types.ColumnTypeDecimal, types.ColumnRoleMetric, ""))
	for i, row := range res.Rows {
		pct := ""
		c, cerr := decimal.NewFromString(row[cur])
		p, perr := decimal.NewFromString(row[prev])
		if cerr == nil && perr == nil && !p.IsZero() {
			pct = c.Sub(p).Div(p).Mul(decimal.NewFromInt(100)).Round(1).String()
		}
		res.Rows[i] = append(row, pct)
	}
}
