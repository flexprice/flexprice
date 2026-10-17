package main

import (
	"reflect"
	"testing"
)

func TestGetPath(t *testing.T) {
	doc := map[string]any{
		"id": "cust_1",
		"subscription": map[string]any{
			"id":     "sub_1",
			"status": "active",
		},
		"items": []any{
			map[string]any{"feature_id": "f1", "qty": 1.0},
			map[string]any{"feature_id": "f2", "qty": 5.0},
		},
	}

	tests := []struct {
		path  string
		want  any
		found bool
	}{
		{"id", "cust_1", true},
		{"subscription.id", "sub_1", true},
		{"items.0.feature_id", "f1", true},
		{"items.1.qty", 5.0, true},
		{"items.*.feature_id", []any{"f1", "f2"}, true},
		{"items.*.missing", []any{}, true},
		{"", doc, true},
		{"missing", nil, false},
		{"subscription.missing", nil, false},
		{"items.9.feature_id", nil, false},
		{"id.nested", nil, false},
	}
	for _, tc := range tests {
		got, found := GetPath(doc, tc.path)
		if found != tc.found {
			t.Errorf("GetPath(%q): found=%v, want %v", tc.path, found, tc.found)
			continue
		}
		if found && !reflect.DeepEqual(got, tc.want) {
			t.Errorf("GetPath(%q) = %#v, want %#v", tc.path, got, tc.want)
		}
	}
}

func TestLooseEqual(t *testing.T) {
	tests := []struct {
		a, b any
		want bool
	}{
		{"500", 500.0, true},
		{"500.00", "500", true},
		{500, "500.00", true},
		{"abc", "abc", true},
		{"abc", "abd", false},
		{true, true, true},
		{true, false, false},
		{[]any{"a"}, []any{"a"}, true},
	}
	for _, tc := range tests {
		if got := looseEqual(tc.a, tc.b); got != tc.want {
			t.Errorf("looseEqual(%v, %v) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestExpectationOperators(t *testing.T) {
	rc := NewRenderCtx(nil, "test")
	doc := map[string]any{
		"amount":  "49.99",
		"items":   []any{map[string]any{"qty": 0.0}, map[string]any{"qty": 3.0}},
		"status":  "active",
		"empty":   []any{},
		"missing": nil,
	}

	boolPtr := func(b bool) *bool { return &b }
	intPtr := func(i int) *int { return &i }

	pass := []*Expectation{
		{Path: "status", Equals: "active"},
		{Path: "amount", Approx: &Approx{Value: 50.0, Epsilon: 0.05}},
		{Path: "amount", Gt: 49},
		{Path: "items", LenEq: intPtr(2)},
		{Path: "items.*.qty", AnyGt: 1},
		{Path: "empty", NotEmpty: boolPtr(false)},
		{Path: "nope", Exists: boolPtr(false)},
		{Path: "status", Matches: "^act"},
	}
	for i, e := range pass {
		if err := e.Eval(doc, rc); err != nil {
			t.Errorf("expectation %d should pass: %v", i, err)
		}
	}

	fail := []*Expectation{
		{Path: "status", Equals: "cancelled"},
		{Path: "items.*.qty", AnyGt: 10},
		{Path: "amount", Approx: &Approx{Value: 51.0, Epsilon: 0.05}},
		{Path: "nope", Exists: boolPtr(true)},
	}
	for i, e := range fail {
		if err := e.Eval(doc, rc); err == nil {
			t.Errorf("expectation %d should fail", i)
		}
	}
}

func TestTemplateCoercion(t *testing.T) {
	rc := NewRenderCtx(map[string]any{"limit": 1000}, "test")
	rc.SetCaptures("s1", map[string]any{"count": 42.0})

	v, err := rc.RenderString("{{= .vars.limit }}")
	if err != nil {
		t.Fatal(err)
	}
	if f, ok := v.(float64); !ok || f != 1000 {
		t.Errorf("coerced render = %#v, want 1000.0", v)
	}

	v, err = rc.RenderString("prefix-{{ .steps.s1.count }}")
	if err != nil {
		t.Fatal(err)
	}
	if v != "prefix-42" {
		t.Errorf("string render = %#v", v)
	}

	// Missing capture → ErrMissingDependency
	_, err = rc.RenderString("{{ .steps.nope.value }}")
	if _, ok := err.(*ErrMissingDependency); !ok {
		t.Errorf("expected ErrMissingDependency, got %v", err)
	}
}
