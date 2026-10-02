package activity

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

var subDef = Definition{EntType: "Subscription", EntityType: "subscription", Table: "subscriptions",
	IgnoreFields: []string{"version"}, RedactFields: []string{"secret_note"}}

func TestDiffDropsUnchangedAndBookkeeping(t *testing.T) {
	old := map[string]any{"status": "active", "plan_id": "plan_1", "updated_at": time.Unix(1, 0), "version": 1}
	new := map[string]any{"status": "paused", "plan_id": "plan_1", "updated_at": time.Unix(2, 0), "version": 2}
	changes, changed := Diff(subDef, old, new)
	if !changed || len(changes) != 1 {
		t.Fatalf("want exactly status, got %+v", changes)
	}
	if changes["status"].From != "active" || changes["status"].To != "paused" {
		t.Fatalf("bad change %+v", changes["status"])
	}
}

func TestDiffNoEffectiveChange(t *testing.T) {
	old := map[string]any{"status": "active", "updated_at": time.Unix(1, 0)}
	new := map[string]any{"status": "active", "updated_at": time.Unix(2, 0)}
	if _, changed := Diff(subDef, old, new); changed {
		t.Fatal("expected no change")
	}
}

func TestDiffRedacts(t *testing.T) {
	changes, _ := Diff(subDef, map[string]any{"secret_note": "a"}, map[string]any{"secret_note": "b"})
	c := changes["secret_note"]
	if !c.Redacted || c.From != nil || c.To != nil {
		t.Fatalf("expected redacted marker, got %+v", c)
	}
}

func TestNormalizeEquivalents(t *testing.T) {
	cases := []struct{ a, b any }{
		{[]byte(`{"b":1,"a":2}`), map[string]any{"a": 2, "b": 1}},
		{decimal.NewFromInt(10), "10"},
		{time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), "2026-01-01T00:00:00Z"},
		{int64(3), float64(3)},
		{[]byte("plain"), "plain"},
	}
	for _, c := range cases {
		if Normalize(c.a) != Normalize(c.b) {
			t.Fatalf("%v and %v should normalize equal: %q vs %q", c.a, c.b, Normalize(c.a), Normalize(c.b))
		}
	}
}
