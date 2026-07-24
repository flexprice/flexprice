package svix

import "testing"

func TestAppSubscriptions_Has(t *testing.T) {
	all := newAppSubscriptions()
	all.addEndpoint(nil) // empty filterTypes = subscribe to every event
	if !all.has("anything.happened") {
		t.Fatalf("empty filterTypes must match every event type")
	}

	filtered := newAppSubscriptions()
	filtered.addEndpoint([]string{"invoice.created"})
	if !filtered.has("invoice.created") {
		t.Fatalf("filtered set must match listed type")
	}
	if filtered.has("invoice.voided") {
		t.Fatalf("filtered set must not match unlisted type")
	}

	var nilSubs *appSubscriptions
	if nilSubs.has("x") {
		t.Fatalf("nil subscriptions must not match")
	}
	nilSubs.addEndpoint([]string{"x"}) // must not panic
}
