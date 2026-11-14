package contract

import (
	"context"
	"encoding/json"
	"github.com/flexprice/flexprice/internal/ee/agents/extraction"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/domain/customer"
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
			sanitizeTerm(&got, tt.allowed)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestSanitize_ResultShape(t *testing.T) {
	r := &extraction.Result{Documents: []extraction.Document{{Type: "SOW", PageStart: -1}, {Type: "brochure"}}, Charges: []extraction.Charge{{Kind: "Subscription"}}}
	sanitize(r)
	require.Equal(t, DocumentTypeOther, r.Documents[0].Type)
	require.Equal(t, 0, r.Documents[0].PageStart)
	require.Equal(t, DocumentTypeOther, r.Documents[1].Type)
	require.Equal(t, "", r.Charges[0].Kind)
	require.NotNil(t, r.Charges[0].Tiers)
	require.NotNil(t, r.Commitments)
	require.NotNil(t, r.Doubts)
}

func TestDerive(t *testing.T) {
	base := func() *extraction.Result {
		r := &extraction.Result{}
		r.Term.EffectiveDate = read("2026-01-15", 95, 1, "q")
		r.Term.TermMonths = read("24", 92, 1, "q")
		return r
	}
	t.Run("end date and no notice", func(t *testing.T) {
		d := derive(base())
		require.Equal(t, "2028-01-14", d.EndDate.Value)
		require.Equal(t, 92, d.EndDate.Confidence)
		require.False(t, d.NoticeDeadline.IsSet())
	})
	t.Run("missing inputs derive nothing", func(t *testing.T) {
		r := base()
		r.Term.TermMonths = missing()
		d := derive(r)
		require.False(t, d.EndDate.IsSet())
		require.Empty(t, d.InvoiceSchedules)
	})
	schedule := func(period, quoted, timing, amount string) []extraction.InvoiceSchedule {
		r := base()
		r.Charges = []extraction.Charge{{Kind: ChargeKindRecurringFixed, Name: read("Fee", 90, 1, "q"), Amount: read(amount, 90, 1, "q"),
			AmountPeriod: read(quoted, 90, 1, "q"), BillingPeriod: read(period, 90, 1, "q"), BillingTiming: read(timing, 90, 1, "q")}}
		return derive(r).InvoiceSchedules
	}
	t.Run("monthly from annual", func(t *testing.T) {
		s := schedule("MONTHLY", "ANNUAL", "advance", "120000")
		require.Len(t, s, 1)
		require.Equal(t, "10000.00", s[0].AmountPerInvoice)
		require.Len(t, s[0].Dates, 24)
	})
	t.Run("half yearly quoted per period", func(t *testing.T) {
		s := schedule("HALF_YEARLY", "", "advance", "5000")
		require.Equal(t, "5000.00", s[0].AmountPerInvoice)
		require.Equal(t, []string{"2026-01-15", "2026-07-15", "2027-01-15", "2027-07-15"}, s[0].Dates)
	})
	t.Run("annual", func(t *testing.T) {
		require.Len(t, schedule("ANNUAL", "ANNUAL", "advance", "99")[0].Dates, 2)
	})
	t.Run("arrears and bad amount skipped", func(t *testing.T) {
		require.Empty(t, schedule("MONTHLY", "ANNUAL", "arrears", "100"))
		require.Empty(t, schedule("MONTHLY", "ANNUAL", "advance", "abc"))
		require.Empty(t, schedule("WEEKLY", "ANNUAL", "advance", "100"))
	})
}

func TestNormalizeName(t *testing.T) {
	tests := map[string]string{
		"Northwind Health, Inc.": "northwind health",
		"northwind health":       "northwind health",
		"ACME Corp. Ltd":         "acme",
		"Globex Private Limited": "globex",
		"The Co":                 "the",
		"  Initech, L.L.C.  ":    "initech",
		"":                       "",
	}
	for in, want := range tests {
		require.Equal(t, want, normalizeName(in), in)
	}
}

func TestMatchCustomer(t *testing.T) {
	rows := []*customer.Customer{
		{ID: "c_email", Name: "Something Else", Email: "ap@acme.com"},
		{ID: "c_name", Name: "Globex Corporation", Email: "finance@globex.io"},
		{ID: "c_gmail", Name: "Solo Founder", Email: "solo@gmail.com"},
	}
	tests := []struct {
		name string
		in   extraction.Customer
		want *extraction.CustomerMatch
	}{
		{name: "exact email wins", in: extraction.Customer{LegalName: read("Acme Inc.", 99, 1, "q"), BillingEmail: read("AP@acme.com", 90, 1, "q")},
			want: &extraction.CustomerMatch{Status: CustomerMatchExisting, CustomerID: "c_email", Name: "Something Else", MatchedOn: []string{MatchedOnEmail}}},
		{name: "legal name with domain", in: extraction.Customer{LegalName: read("Globex Corp", 99, 1, "q"), BillingEmail: read("ap@globex.io", 90, 1, "q")},
			want: &extraction.CustomerMatch{Status: CustomerMatchExisting, CustomerID: "c_name", Name: "Globex Corporation", MatchedOn: []string{MatchedOnLegalName, MatchedOnEmailDomain}}},
		{name: "legal name only", in: extraction.Customer{LegalName: read("Globex", 99, 1, "q")},
			want: &extraction.CustomerMatch{Status: CustomerMatchExisting, CustomerID: "c_name", Name: "Globex Corporation", MatchedOn: []string{MatchedOnLegalName}}},
		{name: "domain alone never matches", in: extraction.Customer{LegalName: read("Globex Labs", 99, 1, "q"), BillingEmail: read("x@globex.io", 90, 1, "q")},
			want: &extraction.CustomerMatch{Status: CustomerMatchNew}},
		{name: "partial name is not a match", in: extraction.Customer{LegalName: read("Globex Holdings", 99, 1, "q")},
			want: &extraction.CustomerMatch{Status: CustomerMatchNew}},
		{name: "nothing extracted", in: extraction.Customer{}, want: &extraction.CustomerMatch{Status: CustomerMatchNew}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := newTestService(&fakeOpenRouter{}, &fakeCustomers{rows: rows}).matchCustomer(context.Background(), tt.in)
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestNeedsCheck(t *testing.T) {
	require.True(t, needsCheck(read("x", 89, 1, "q"), 90))
	require.False(t, needsCheck(read("x", 90, 1, "q"), 90))
	require.True(t, needsCheck(extraction.Term{Page: 2, Options: []string{"a", "b"}, Confidence: 95}, 90))
	require.False(t, needsCheck(missing(), 90))
	require.False(t, needsCheck(derivedTerm("2027-01-01", 10), 90))
}

func TestResponseSchema_StrictShape(t *testing.T) {
	var root map[string]any
	require.NoError(t, json.Unmarshal(ResponseSchema(), &root))

	var walk func(path string, node map[string]any)
	walk = func(path string, node map[string]any) {
		switch node["type"] {
		case "object":
			require.Equal(t, false, node["additionalProperties"], path)
			props := node["properties"].(map[string]any)
			required := node["required"].([]any)
			require.Len(t, required, len(props), "every property must be required at %s", path)
			for k, v := range props {
				walk(path+"."+k, v.(map[string]any))
			}
		case "array":
			walk(path+"[]", node["items"].(map[string]any))
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

func TestAddMonthsClampsMonthEnd(t *testing.T) {
	tests := []struct {
		start  string
		months int
		want   string
	}{
		{"2026-01-31", 1, "2026-02-28"},
		{"2028-01-31", 1, "2028-02-29"},
		{"2026-08-31", 3, "2026-11-30"},
		{"2026-11-15", 3, "2027-02-15"},
	}
	for _, tt := range tests {
		start, _ := time.Parse(dateLayout, tt.start)
		require.Equal(t, tt.want, addMonths(start, tt.months).Format(dateLayout), tt.start)
	}
}
