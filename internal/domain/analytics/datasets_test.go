package analytics

import (
	"regexp"
	"slices"
	"testing"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

func datasetDef(ds string, mods ...func(*ViewDefinition)) ViewDefinition {
	d := ViewDefinition{
		Family:   types.FamilyDataset,
		Dataset:  ds,
		Measures: []*Measure{{Agg: "count"}},
		Time:     TimeSpecRaw{Range: "this_month", Field: "billing_period"},
	}
	for _, m := range mods {
		m(&d)
	}
	return d
}

func TestValidateDatasetAccepts(t *testing.T) {
	cases := map[string]ViewDefinition{
		"invoices": datasetDef("invoices", func(d *ViewDefinition) {
			d.Measures = append(d.Measures, &Measure{Agg: "sum", Field: "total"}, &Measure{Agg: "count_distinct", Field: "invoice_id"})
			d.Dimensions = []string{"invoice_status"}
			d.Filters = []*Filter{{Field: "invoice_status", Op: types.IN, Value: []string{"FINALIZED"}}}
			d.Sort = []*SortSpec{{Field: "m1", Dir: types.SortDirectionDesc}}
			d.Limit = 10
			d.Compare = "previous_period"
			d.Display = "chart"
		}),
		"invoice_lines": datasetDef("invoice_lines", func(d *ViewDefinition) {
			d.Measures = append(d.Measures, &Measure{Agg: "sum", Field: "amount"})
			d.Dimensions = []string{"meter"}
			d.Filters = []*Filter{{Field: "amount", Op: FilterOpBetween, Value: []string{"1", "10.5"}}}
			d.Time.Grain = types.GrainDay
		}),
		"wallet_transactions": datasetDef("wallet_transactions", func(d *ViewDefinition) {
			d.Measures = append(d.Measures, &Measure{Agg: "sum", Field: "amount"})
			d.Dimensions = []string{"type"}
			d.Filters = []*Filter{{Field: "type", Op: types.EQUAL, Value: []string{"credit"}}}
			d.Time.Field = "created_at"
		}),
		"credit_notes": datasetDef("credit_notes", func(d *ViewDefinition) {
			d.Measures = append(d.Measures, &Measure{Agg: "sum", Field: "total_amount"})
			d.Dimensions = []string{"customer"}
			d.Filters = []*Filter{{Field: "currency", Op: types.NOT_IN, Value: []string{"usd"}}}
			d.Time.Field = "finalized_at"
		}),
		"subscriptions": datasetDef("subscriptions", func(d *ViewDefinition) {
			d.Measures = append(d.Measures, &Measure{Agg: "sum", Field: "commitment_amount"}, &Measure{Agg: "count_distinct", Field: "customer_id"})
			d.Dimensions = []string{"subscription_status", "billing_period"}
			d.Filters = []*Filter{{Field: "commitment_amount", Op: FilterOpLTE, Value: []string{"100"}}}
			d.Time.Field = "start_date"
		}),
		"customers": datasetDef("customers", func(d *ViewDefinition) {
			d.Dimensions = []string{"address_country"}
			d.Filters = []*Filter{{Field: "billing_currency", Op: types.EQUAL, Value: []string{"usd"}}}
			d.Time.Field = "created_at"
		}),
		"usage": datasetDef("usage", func(d *ViewDefinition) {
			d.Measures = append(d.Measures, &Measure{Agg: "sum", Field: "quantity"})
			d.Dimensions = []string{"customer"}
			d.Filters = []*Filter{{Field: "meter", Op: types.IN, Value: []string{"meter_1"}}}
			d.Time = TimeSpecRaw{Range: "last_7_days", Field: "timestamp"}
		}),
	}
	for name, def := range cases {
		t.Run(name, func(t *testing.T) {
			if err := def.Validate(); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestValidateDatasetRejects(t *testing.T) {
	inv := func(mods ...func(*ViewDefinition)) ViewDefinition { return datasetDef("invoices", mods...) }
	filter := func(f *Filter) func(*ViewDefinition) {
		return func(d *ViewDefinition) { d.Filters = []*Filter{f} }
	}
	cases := map[string]ViewDefinition{
		"unknown dataset":      datasetDef("nope"),
		"no measures":          inv(func(d *ViewDefinition) { d.Measures = nil }),
		"six measures":         inv(func(d *ViewDefinition) { d.Measures = slices.Repeat([]*Measure{{Agg: "count"}}, 6) }),
		"unknown agg":          inv(func(d *ViewDefinition) { d.Measures = []*Measure{{Agg: "median", Field: "total"}} }),
		"count with field":     inv(func(d *ViewDefinition) { d.Measures = []*Measure{{Agg: "count", Field: "total"}} }),
		"sum without field":    inv(func(d *ViewDefinition) { d.Measures = []*Measure{{Agg: "sum"}} }),
		"sum on string field":  inv(func(d *ViewDefinition) { d.Measures = []*Measure{{Agg: "sum", Field: "currency"}} }),
		"sum on count-only id": inv(func(d *ViewDefinition) { d.Measures = []*Measure{{Agg: "sum", Field: "invoice_id"}} }),
		"unknown measure":      inv(func(d *ViewDefinition) { d.Measures = []*Measure{{Agg: "avg", Field: "bogus"}} }),
		"count_distinct time":  inv(func(d *ViewDefinition) { d.Measures = []*Measure{{Agg: "count_distinct", Field: "issue_date"}} }),
		"unknown dimension":    inv(func(d *ViewDefinition) { d.Dimensions = []string{"bogus"} }),
		"non-group dimension":  inv(func(d *ViewDefinition) { d.Dimensions = []string{"total"} }),
		"duplicate dimension":  inv(func(d *ViewDefinition) { d.Dimensions = []string{"currency", "currency"} }),
		"three dimensions":     inv(func(d *ViewDefinition) { d.Dimensions = []string{"currency", "invoice_type", "customer"} }),
		"two dims with grain": inv(func(d *ViewDefinition) {
			d.Dimensions = []string{"currency", "invoice_type"}
			d.Time.Grain = types.GrainMonth
		}),
		"eleven filters": inv(func(d *ViewDefinition) {
			d.Filters = slices.Repeat([]*Filter{{Field: "currency", Op: types.EQUAL, Value: []string{"usd"}}}, 11)
		}),
		"unknown filter field":    inv(filter(&Filter{Field: "bogus", Op: types.EQUAL, Value: []string{"x"}})),
		"time field filter":       inv(filter(&Filter{Field: "issue_date", Op: types.EQUAL, Value: []string{"x"}})),
		"gt on string":            inv(filter(&Filter{Field: "currency", Op: types.GREATER_THAN, Value: []string{"x"}})),
		"between with one value":  inv(filter(&Filter{Field: "total", Op: FilterOpBetween, Value: []string{"1"}})),
		"eq with two values":      inv(filter(&Filter{Field: "currency", Op: types.EQUAL, Value: []string{"a", "b"}})),
		"in with no values":       inv(filter(&Filter{Field: "currency", Op: types.IN})),
		"gt non-numeric":          inv(filter(&Filter{Field: "total", Op: types.GREATER_THAN, Value: []string{"abc"}})),
		"enum value not in list":  inv(filter(&Filter{Field: "invoice_status", Op: types.IN, Value: []string{"PAID"}})),
		"missing time field":      inv(func(d *ViewDefinition) { d.Time.Field = "" }),
		"non-time time field":     inv(func(d *ViewDefinition) { d.Time.Field = "total" }),
		"bad range":               inv(func(d *ViewDefinition) { d.Time.Range = "forever" }),
		"usage with last_30_days": datasetDef("usage", func(d *ViewDefinition) { d.Time = TimeSpecRaw{Range: "last_30_days", Field: "timestamp"} }),
		"hour grain":              inv(func(d *ViewDefinition) { d.Time.Grain = types.GrainHour }),
		"bad compare":             inv(func(d *ViewDefinition) { d.Compare = "yesterday" }),
		"bad display":             inv(func(d *ViewDefinition) { d.Display = "pie" }),
		"sort unknown field":      inv(func(d *ViewDefinition) { d.Sort = []*SortSpec{{Field: "total"}} }),
		"sort measure oob":        inv(func(d *ViewDefinition) { d.Sort = []*SortSpec{{Field: "m1"}} }),
		"usage compare": datasetDef("usage", func(d *ViewDefinition) {
			d.Time = TimeSpecRaw{Range: "last_7_days", Field: "timestamp"}
			d.Compare = "previous_period"
		}),
		"between reversed": inv(filter(&Filter{Field: "total", Op: FilterOpBetween, Value: []string{"10", "1"}})),
		"negative limit":   inv(func(d *ViewDefinition) { d.Limit = -1 }),
	}
	for name, def := range cases {
		t.Run(name, func(t *testing.T) {
			err := def.Validate()
			if err == nil {
				t.Fatal("expected error")
			}
			if !ierr.IsValidation(err) {
				t.Fatalf("expected validation error, got %v", err)
			}
		})
	}
}

func TestDatasetShape(t *testing.T) {
	if s := DatasetShape(datasetDef("invoices")); s != types.ShapeScalar {
		t.Fatalf("got %s", s)
	}
	if s := DatasetShape(datasetDef("invoices", func(d *ViewDefinition) { d.Dimensions = []string{"currency"} })); s != types.ShapeBreakdown {
		t.Fatalf("got %s", s)
	}
	if s := DatasetShape(datasetDef("invoices", func(d *ViewDefinition) { d.Time.Grain = types.GrainDay })); s != types.ShapeTimeseries {
		t.Fatalf("got %s", s)
	}
}

var aliasRef = regexp.MustCompile(`\b([a-z]+)\.([a-z_]+)`)

func TestDatasetCatalogIntegrity(t *testing.T) {
	seen := map[string]bool{}
	for _, ds := range Datasets() {
		if seen[ds.Key] {
			t.Errorf("duplicate dataset %s", ds.Key)
		}
		seen[ds.Key] = true
		cols := map[string][]string{}
		for _, j := range ds.Joins {
			cols[j.Alias] = j.Cols
		}
		checkExpr := func(where, expr string) {
			for _, m := range aliasRef.FindAllStringSubmatch(expr, -1) {
				if m[1] == ds.Alias {
					continue
				}
				c, ok := cols[m[1]]
				if !ok {
					t.Errorf("%s: %s uses undeclared alias %q", ds.Key, where, m[1])
				} else if !slices.Contains(c, m[2]) {
					t.Errorf("%s: %s uses %s.%s not in join cols", ds.Key, where, m[1], m[2])
				}
			}
		}
		for _, j := range ds.Joins {
			checkExpr("join "+j.Alias, j.On)
		}
		keys := map[string]bool{}
		hasTime := false
		for _, f := range ds.Fields {
			if keys[f.Key] {
				t.Errorf("%s: duplicate field %s", ds.Key, f.Key)
			}
			keys[f.Key] = true
			hasTime = hasTime || f.Kind == FieldKindTime
			if f.Money && f.Kind != FieldKindNumber {
				t.Errorf("%s: money field %s is not numeric", ds.Key, f.Key)
			}
			checkExpr(f.Key, f.Expr)
			checkExpr(f.Key+" label", f.LabelExpr)
		}
		if !hasTime {
			t.Errorf("%s: no time field", ds.Key)
		}
	}
}
