package contract

import (
	"encoding/json"
	"testing"

	"github.com/flexprice/flexprice/internal/ee/agents/services/extraction"

	"github.com/stretchr/testify/require"
)

func TestSanitizeTerm(t *testing.T) {
	tests := []struct {
		name    string
		in      extraction.Term
		allowed []string
		want    extraction.Term
	}{
		{name: "clamps confidence", in: extraction.Term{Value: "x", Confidence: 140, Page: 2}, want: extraction.Term{Value: "x", Confidence: 100, Page: 2, Options: []string{}}},
		{name: "negative page", in: extraction.Term{Value: "x", Confidence: 50, Page: -3}, want: extraction.Term{Value: "x", Confidence: 50, Page: 0, Options: []string{}}},
		{name: "missing clears evidence", in: extraction.Term{Confidence: 80, Page: 4, Quote: "q"}, want: extraction.Term{Options: []string{}}},
		{name: "options keep evidence", in: extraction.Term{Confidence: 70, Page: 9, Quote: "tiered", Options: []string{" A ", ""}},
			want: extraction.Term{Confidence: 70, Page: 9, Quote: "tiered", Options: []string{"A"}}},
		{name: "enum case folded", in: extraction.Term{Value: "quarterly", Confidence: 90, Page: 1}, allowed: billingPeriods,
			want: extraction.Term{Value: "QUARTERLY", Confidence: 90, Page: 1, Options: []string{}}},
		{name: "enum invalid cleared", in: extraction.Term{Value: "fortnightly", Confidence: 90, Page: 1, Quote: "q"}, allowed: billingPeriods,
			want: extraction.Term{Options: []string{}, Note: `Read as "fortnightly", which is not an allowed value.`}},
		{name: "derived flag dropped", in: extraction.Term{Value: "x", Confidence: 90, Page: 1, Derived: true}, want: extraction.Term{Value: "x", Confidence: 90, Page: 1, Options: []string{}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.in
			extractor().sanitizeTerm(&got, tt.allowed)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestSanitize_ResultShape(t *testing.T) {
	r := &extraction.Result{Documents: []extraction.Document{{Type: "SOW", PageStart: -1}, {Type: "brochure"}}, Charges: []extraction.Charge{{Kind: "Subscription"}}}
	extractor().sanitize(r)
	require.Equal(t, DocumentTypeOther, r.Documents[0].Type)
	require.Equal(t, 0, r.Documents[0].PageStart)
	require.Equal(t, DocumentTypeOther, r.Documents[1].Type)
	require.Equal(t, "", r.Charges[0].Kind)
	require.NotNil(t, r.Charges[0].Tiers)
	require.NotNil(t, r.Commitments)
	require.NotNil(t, r.Doubts)
}

func TestResponseSchema_Shape(t *testing.T) {
	var root map[string]any
	require.NoError(t, json.Unmarshal(extractor().schema, &root))

	var walk func(path string, node map[string]any)
	walk = func(path string, node map[string]any) {
		switch node["type"] {
		case "object":
			for k, v := range node["properties"].(map[string]any) {
				walk(path+"."+k, v.(map[string]any))
			}
		case "array":
			walk(path+"[]", node["items"].(map[string]any))
		default:
			require.NotEmpty(t, node["description"], "every leaf needs a description at %s", path)
		}
	}
	walk("$", root)

	props := root["properties"].(map[string]any)
	for _, k := range []string{"documents", "contract_type", "customer", "term", "billing", "charges", "commitments", "credits", "service_levels", "doubts"} {
		require.Contains(t, props, k)
	}
	for _, k := range []string{"customer_match", "derived", "to_check"} {
		require.NotContains(t, props, k)
	}
}
