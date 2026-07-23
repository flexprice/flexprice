package svix

import "testing"

func TestAppSubscriptions_Has(t *testing.T) {
	all := &appSubscriptions{subscribeAll: true}
	if !all.has("anything.happened") {
		t.Fatalf("subscribeAll must match every event type")
	}

	filtered := &appSubscriptions{types: map[string]struct{}{
		"invoice.created": {},
	}}
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
}
