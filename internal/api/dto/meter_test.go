package dto

import (
	"testing"

	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/types"
)

func TestCreateMeterRequest_ToMeter_TrimsWhitespace(t *testing.T) {
	req := &CreateMeterRequest{
		Name:      "  My Meter  ",
		EventName: "  api_request  ",
		Aggregation: meter.Aggregation{
			Type:       types.AggregationSum,
			Field:      "  duration_ms  ",
			Expression: "  token * duration  ",
			GroupBy:    "  region  ",
		},
		Filters: []meter.Filter{
			{
				Key:    "  status  ",
				Values: []string{"  success  ", "  error  "},
			},
			{
				Key:    "region",
				Values: []string{" us-east-1 "},
			},
		},
		ResetUsage: types.ResetUsageBillingPeriod,
	}

	m := req.ToMeter("tenant_1", "user_1")

	if m.EventName != "api_request" {
		t.Errorf("expected event_name to be trimmed, got %q", m.EventName)
	}
	if m.Aggregation.Field != "duration_ms" {
		t.Errorf("expected aggregation.field to be trimmed, got %q", m.Aggregation.Field)
	}
	if m.Aggregation.Expression != "token * duration" {
		t.Errorf("expected aggregation.expression to be trimmed, got %q", m.Aggregation.Expression)
	}
	if m.Aggregation.GroupBy != "region" {
		t.Errorf("expected aggregation.group_by to be trimmed, got %q", m.Aggregation.GroupBy)
	}

	if len(m.Filters) != 2 {
		t.Fatalf("expected 2 filters, got %d", len(m.Filters))
	}

	if m.Filters[0].Key != "status" {
		t.Errorf("expected filter key to be trimmed, got %q", m.Filters[0].Key)
	}
	if len(m.Filters[0].Values) != 2 || m.Filters[0].Values[0] != "success" || m.Filters[0].Values[1] != "error" {
		t.Errorf("expected filter values to be trimmed, got %v", m.Filters[0].Values)
	}

	if m.Filters[1].Key != "region" {
		t.Errorf("expected filter key to be trimmed, got %q", m.Filters[1].Key)
	}
	if len(m.Filters[1].Values) != 1 || m.Filters[1].Values[0] != "us-east-1" {
		t.Errorf("expected filter value to be trimmed, got %v", m.Filters[1].Values)
	}

	// Name is not part of the trimmed field set for this fix; only confirm it
	// passed through untouched so we don't accidentally assert on it.
	if m.Name != "  My Meter  " {
		t.Errorf("expected name to pass through unchanged, got %q", m.Name)
	}
}

func TestCreateMeterRequest_ToMeter_NilFilters(t *testing.T) {
	req := &CreateMeterRequest{
		Name:      "My Meter",
		EventName: "api_request",
		Aggregation: meter.Aggregation{
			Type: types.AggregationCount,
		},
		ResetUsage: types.ResetUsageBillingPeriod,
	}

	m := req.ToMeter("tenant_1", "user_1")

	if m.Filters != nil {
		t.Errorf("expected nil filters to remain nil, got %v", m.Filters)
	}
}

func TestUpdateMeterRequest_Sanitize_TrimsFilterKeysAndValues(t *testing.T) {
	req := &UpdateMeterRequest{
		Filters: []meter.Filter{
			{
				Key:    "  status  ",
				Values: []string{"  active  ", "  inactive  "},
			},
			{
				Key:    "\tregion\n",
				Values: []string{" us-east-1 ", "us-west-2"},
			},
		},
	}

	req.Sanitize()

	if len(req.Filters) != 2 {
		t.Fatalf("expected 2 filters, got %d", len(req.Filters))
	}

	if req.Filters[0].Key != "status" {
		t.Errorf("expected filter key to be trimmed, got %q", req.Filters[0].Key)
	}
	if len(req.Filters[0].Values) != 2 || req.Filters[0].Values[0] != "active" || req.Filters[0].Values[1] != "inactive" {
		t.Errorf("expected filter values to be trimmed, got %v", req.Filters[0].Values)
	}

	if req.Filters[1].Key != "region" {
		t.Errorf("expected filter key to be trimmed, got %q", req.Filters[1].Key)
	}
	if len(req.Filters[1].Values) != 2 || req.Filters[1].Values[0] != "us-east-1" || req.Filters[1].Values[1] != "us-west-2" {
		t.Errorf("expected filter values to be trimmed, got %v", req.Filters[1].Values)
	}
}

func TestSanitizeFilters_NilAndEmpty(t *testing.T) {
	if got := sanitizeFilters(nil); got != nil {
		t.Errorf("expected nil input to return nil, got %v", got)
	}

	got := sanitizeFilters([]meter.Filter{})
	if got == nil || len(got) != 0 {
		t.Errorf("expected empty slice input to return empty slice, got %v", got)
	}
}
