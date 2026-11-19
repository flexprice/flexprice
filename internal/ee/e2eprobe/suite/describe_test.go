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
	for _, want := range []string{"taxed", "e2eprobe-cust-persistent-0", "usage", "invoice-tax", "events/day: 28080 of max 50000", "connections: stripe", "standard (27/tick)", "preflight"} {
		if !strings.Contains(out, want) {
			t.Errorf("describe output lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "billing-matrix-opening") {
		t.Error("disabled scenarios must not be listed as bindings")
	}
}
