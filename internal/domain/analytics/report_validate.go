package analytics

import (
	"fmt"
	"slices"
	"strings"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

type ReportQueryFilter struct {
	Field string
	Op    string
	Value []string
}

type ReportQuerySort struct {
	Field string
	Dir   string
}

// ReportQuery is the transport-neutral report query.
type ReportQuery struct {
	Shape      string
	Dimensions []string
	Grain      string
	Currency   string
	Filters    []ReportQueryFilter
	Compare    string
	Limit      int
	Offset     int
	Sort       []ReportQuerySort
}

// ValidateReport checks a report definition against its catalog entry.
func (v ViewDefinition) ValidateReport(maxRows int) error {
	if err := v.Validate(); err != nil {
		return err
	}
	entry, _ := LookupReport(string(v.Metrics[0]))
	q := &ReportQuery{
		Shape: string(v.Shape), Dimensions: v.Dimensions, Grain: string(v.Time.Grain),
		Compare: v.Compare, Currency: v.Currency, Limit: v.Limit,
	}
	for _, f := range v.Filters {
		if f != nil {
			q.Filters = append(q.Filters, ReportQueryFilter{Field: f.Field, Op: string(f.Op), Value: f.Value})
		}
	}
	for _, so := range v.Sort {
		if so != nil {
			q.Sort = append(q.Sort, ReportQuerySort{Field: so.Field, Dir: string(so.Dir)})
		}
	}
	if err := q.ValidateAgainst(entry, maxRows); err != nil {
		return err
	}
	days, _ := ReportRangeMaxDays(v.Time.Range)
	if d := entry.MaxRangeDays; d > 0 && days > d {
		return ierr.NewErrorf("range exceeds %d days for this report", d).
			WithHint(fmt.Sprintf("Choose a range of %d days or less", d)).
			Mark(ierr.ErrValidation)
	}
	return nil
}

func (req *ReportQuery) ValidateAgainst(e *ReportCatalogEntry, maxRows int) error {
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
	if req.Offset < 0 || (req.Offset > 0 && (req.Limit == 0 || req.Shape != string(types.ShapeBreakdown) || len(req.Sort) > 0)) {
		return ierr.NewError("offset not allowed").
			WithHint("offset needs a breakdown with a limit and the default sort").
			Mark(ierr.ErrValidation)
	}
	if req.Limit > 0 && req.Shape != string(types.ShapeBreakdown) {
		return ierr.NewError("limit only applies to breakdown").
			WithHint("omit limit unless shape is breakdown").
			Mark(ierr.ErrValidation)
	}
	bm, _ := LookupBillingMetric(e.Key)
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
