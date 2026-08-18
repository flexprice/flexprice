package spec

import "testing"

func TestLoad_ParsesEmbeddedSpec(t *testing.T) {
	doc, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if doc.Info == nil || doc.Info.Title == "" {
		t.Fatal("spec has no Info.Title")
	}
}

func TestRegions_ComeFromServers(t *testing.T) {
	doc, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	regions := Regions(doc)
	if len(regions) < 2 {
		t.Fatalf("Regions returned %d entries, want at least 2", len(regions))
	}

	byKey := map[string]Region{}
	for _, r := range regions {
		byKey[r.Key] = r
	}
	us, ok := byKey["us"]
	if !ok {
		t.Fatalf("no region keyed \"us\"; got %v", byKey)
	}
	if us.BaseURL != "https://us.api.flexprice.io/v1" {
		t.Errorf("us BaseURL = %q", us.BaseURL)
	}
	if _, ok := byKey["in"]; !ok {
		t.Errorf("no region keyed \"in\"; got %v", byKey)
	}
}

// The Webhook Events tag holds 56 documentation stubs with synthetic paths that
// 404 if called. They must never become commands. Design doc §5.
func TestOperations_ExcludeWebhookEventStubs(t *testing.T) {
	doc, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	for _, op := range Operations(doc) {
		if op.Tag == WebhookEventsTag {
			t.Fatalf("operation %s is tagged %q and must be excluded", op.ID, WebhookEventsTag)
		}
		if op.ID == "" {
			t.Fatalf("operation at %s %s has no operationId", op.Method, op.Path)
		}
	}
}

func TestEventTypes_ComeFromWebhookEventStubs(t *testing.T) {
	doc, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	types := EventTypes(doc)
	if len(types) < 20 {
		t.Fatalf("EventTypes returned %d, want the full stub list", len(types))
	}
	// The design doc's sample list included "invoice.created" as an example event
	// name, but the embedded spec has no such event (verified: zero occurrences of
	// "invoice.created" in spec/openapi.json). It has "invoice.create.drafted",
	// "invoice.update.finalized", etc. instead. "customer.created" genuinely exists
	// and exercises the same code path.
	found := false
	for _, e := range types {
		if e == "customer.created" {
			found = true
		}
	}
	if !found {
		t.Errorf("customer.created missing from event types: %v", types)
	}
}
