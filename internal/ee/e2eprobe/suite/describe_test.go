package suite

import (
	"bytes"
	"strings"
	"testing"
)

func TestDescribe_ListsEveryBindingAndBudget(t *testing.T) {
	var buf bytes.Buffer
	if err := Describe(&buf, Default(), newFakeCatalog()); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{"taxed", "e2eprobe-taxed", "usage", "invoice-tax", "events/day: 53472 of max 100000", "profiles 28080", "flows 25392", "connections: (none)", "standard (27/tick)", "preflight",
		"Coverage", "exact totals for every meter over the last full hour", "preview bills the commitment with a true-up under it and factored overage over it", "committed (e2eprobe-committed)", "disabled: billing-matrix",
		"Areas", "covered by: analytics-hourly ×1, ingest ×8, usage ×8", "not covered: not in the suite and no persona has trait payment (auto-charge, checkout-link, payment-methods)", "not covered: billing-matrix disabled"} {
		if !strings.Contains(out, want) {
			t.Errorf("describe output lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "billing-matrix-opening") {
		t.Error("disabled scenarios must not be listed as bindings")
	}
	buf.Reset()
	if err := Describe(&buf, withPayer(Default()), newFakeCatalog()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "covered by: auto-charge ×1, checkout-link ×1, payment-methods ×1") {
		t.Errorf("a payer persona must show the payments area as covered:\n%s", buf.String())
	}
}
