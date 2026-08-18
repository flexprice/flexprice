package cmd

import "testing"

func TestResponseID(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"top-level id", `{"id":"cust_01","name":"Ada"}`, "cust_01"},
		{"no id field", `{"name":"Ada"}`, ""},
		{"id is not a string", `{"id":42}`, ""},
		{"not JSON at all", `<html>502</html>`, ""},
		{"empty body", ``, ""},
		{"a list, not an object", `{"items":[{"id":"cust_01"}]}`, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := responseID([]byte(tc.raw)); got != tc.want {
				t.Errorf("responseID(%s) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

func TestSingular(t *testing.T) {
	cases := map[string]string{
		"customers":     "customer",
		"invoices":      "invoice",
		"subscriptions": "subscription",
		"prices":        "price",
		// No trailing s: unchanged rather than mangled.
		"rbac":     "rbac",
		"checkout": "checkout",
		// Double-s must not lose a letter: "address" -> "address", not "addres".
		"address": "address",
	}
	for in, want := range cases {
		if got := singular(in); got != want {
			t.Errorf("singular(%q) = %q, want %q", in, got, want)
		}
	}
}

// Read actions must not emit a receipt: "Retrieved customer cust_01" adds
// nothing to output that already shows the customer.
func TestReceiptVerbs_ExcludeReadActions(t *testing.T) {
	for _, action := range []string{"list", "retrieve", "get", "lookup", "usage"} {
		if verb, ok := receiptVerbs[action]; ok {
			t.Errorf("read action %q should have no receipt verb, got %q", action, verb)
		}
	}
}

// Every destructive action must produce a receipt: these are the ones a user
// most needs written confirmation of.
func TestReceiptVerbs_CoverEveryDestructiveAction(t *testing.T) {
	for action := range destructive {
		if _, ok := receiptVerbs[action]; !ok {
			t.Errorf("destructive action %q produces no receipt", action)
		}
	}
}
