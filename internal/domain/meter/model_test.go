package meter

import (
	"testing"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

func validBase() *Meter {
	return &Meter{
		ID:        "meter_test",
		Name:      "test meter",
		EventName: "api_call",
		BaseModel: types.BaseModel{TenantID: "tenant_test", Status: types.StatusPublished},
	}
}

func TestMeter_Validate_ExpressionOnSupportedTypes(t *testing.T) {
	supported := []types.AggregationType{
		types.AggregationSum,
		types.AggregationAvg,
		types.AggregationMax,
		types.AggregationLatest,
	}
	for _, typ := range supported {
		t.Run(string(typ), func(t *testing.T) {
			m := validBase()
			m.Aggregation = Aggregation{Type: typ, Expression: "tokens * 2"}
			if err := m.Validate(); err != nil {
				t.Fatalf("expected no error for %s with expression, got %v", typ, err)
			}
		})
	}
}

func TestMeter_Validate_ExpressionOnUnsupportedTypes(t *testing.T) {
	cases := []struct {
		typ      types.AggregationType
		setExtra func(*Aggregation) // satisfy any per-type required fields so we hit OUR check, not theirs
	}{
		{types.AggregationCount, nil},
		{types.AggregationCountUnique, nil},
		{types.AggregationSumWithMultiplier, func(a *Aggregation) {
			mult := decimal.NewFromInt(1000)
			a.Multiplier = &mult
		}},
		{types.AggregationWeightedSum, nil},
	}
	for _, c := range cases {
		t.Run(string(c.typ), func(t *testing.T) {
			m := validBase()
			m.Aggregation = Aggregation{Type: c.typ, Expression: "tokens * 2"}
			if c.setExtra != nil {
				c.setExtra(&m.Aggregation)
			}
			err := m.Validate()
			if err == nil {
				t.Fatalf("expected validation error for %s with expression, got nil", c.typ)
			}
			if !ierr.IsValidation(err) {
				t.Fatalf("expected ErrValidation, got %v", err)
			}
		})
	}
}

func TestMeter_Validate_FieldAndExpressionMutuallyExclusive(t *testing.T) {
	m := validBase()
	m.Aggregation = Aggregation{
		Type:       types.AggregationSum,
		Field:      "tokens",
		Expression: "tokens * 2",
	}
	err := m.Validate()
	if err == nil {
		t.Fatal("expected validation error when both field and expression set")
	}
	if !ierr.IsValidation(err) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}
}

func TestMeter_Validate_NeitherFieldNorExpression(t *testing.T) {
	// Pre-existing rule, but locking it in here so the matrix is complete.
	m := validBase()
	m.Aggregation = Aggregation{Type: types.AggregationSum}
	err := m.Validate()
	if err == nil {
		t.Fatal("expected validation error when SUM has neither field nor expression")
	}
	if !ierr.IsValidation(err) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}
}

func TestMeter_Validate_FieldOnlyStillWorks(t *testing.T) {
	// Regression: a plain field-based SUM meter must still validate.
	m := validBase()
	m.Aggregation = Aggregation{Type: types.AggregationSum, Field: "tokens"}
	if err := m.Validate(); err != nil {
		t.Fatalf("plain field-based SUM meter should validate, got %v", err)
	}
}

func TestMeter_Normalize_TrimsLookupKeys(t *testing.T) {
	m := validBase()
	m.EventName = " llm_usage "
	m.Aggregation = Aggregation{Type: types.AggregationSum, Field: " output_tokens"}
	m.Filters = []Filter{{Key: " model_name ", Values: []string{"gpt-4o"}}}

	m.Normalize()

	if m.EventName != "llm_usage" {
		t.Fatalf("event name not trimmed: %q", m.EventName)
	}
	if m.Aggregation.Field != "output_tokens" {
		t.Fatalf("aggregation field not trimmed: %q", m.Aggregation.Field)
	}
	if m.Filters[0].Key != "model_name" {
		t.Fatalf("filter key not trimmed: %q", m.Filters[0].Key)
	}
	// Filter values are tenant data, not identifiers, and are left untouched.
	if m.Filters[0].Values[0] != "gpt-4o" {
		t.Fatalf("filter value changed: %q", m.Filters[0].Values[0])
	}
}

func TestMeter_Normalize_WhitespaceOnlyFieldFailsValidation(t *testing.T) {
	m := validBase()
	m.Aggregation = Aggregation{Type: types.AggregationSum, Field: "   "}

	m.Normalize()

	err := m.Validate()
	if err == nil {
		t.Fatal("expected whitespace-only field to be rejected as missing")
	}
	if !ierr.IsValidation(err) {
		t.Fatalf("expected validation error, got %v", err)
	}
}

func TestMeter_Normalize_TrimsExpressionAndGroupBy(t *testing.T) {
	m := validBase()
	m.Aggregation = Aggregation{
		Type:       types.AggregationMax,
		Expression: "  tokens * 2 ",
		GroupBy:    " request_id ",
	}

	m.Normalize()

	if m.Aggregation.Expression != "tokens * 2" {
		t.Fatalf("expression not trimmed: %q", m.Aggregation.Expression)
	}
	if m.Aggregation.GroupBy != "request_id" {
		t.Fatalf("group_by not trimmed: %q", m.Aggregation.GroupBy)
	}
}
