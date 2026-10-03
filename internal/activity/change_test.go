package activity

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/types"
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

func TestNormalizeTimeAtMicrosecondPrecision(t *testing.T) {
	inMemory := time.Date(2026, 10, 3, 12, 0, 0, 123456789, time.UTC)
	stored := inMemory.Truncate(time.Microsecond)
	if Normalize(inMemory) != Normalize(stored) || Normalize(&inMemory) != Normalize(stored) {
		t.Fatalf("nanoseconds must not count: %q vs %q", Normalize(inMemory), Normalize(stored))
	}
	if _, changed := Diff(subDef, map[string]any{"due_date": stored}, map[string]any{"due_date": inMemory}); changed {
		t.Fatal("a re-saved time differing only below the microsecond must not diff")
	}
	if Normalize(stored) == Normalize(stored.Add(time.Microsecond)) {
		t.Fatal("a microsecond difference must still count")
	}
}

func TestDiffRedacts(t *testing.T) {
	changes, _ := Diff(subDef, map[string]any{"secret_note": "a"}, map[string]any{"secret_note": "b"})
	c := changes["secret_note"]
	if !c.Redacted || c.From != nil || c.To != nil {
		t.Fatalf("expected redacted marker, got %+v", c)
	}
}

func TestDiffBulkNoOpAcrossRepresentations(t *testing.T) {
	ts := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	name := "same-name"
	old := map[string]any{
		"status":         "active",
		"count":          int64(1_000_000),
		"amount":         decimal.NewFromInt(42),
		"synced_at":      ts,
		"metadata":       []byte(`{"b":1,"a":2}`),
		"cadence":        types.BILLING_CADENCE_RECURRING,
		"label":          &name,
		"tags":           map[string]string(nil),
		"tenant_id":      "tenant_1",
		"environment_id": "env_1",
		"updated_at":     time.Unix(1, 0),
		"updated_by":     "user_1",
	}
	new := map[string]any{
		"status":         "active",
		"count":          int64(1_000_000),
		"amount":         []byte("42"),
		"synced_at":      ts,
		"metadata":       map[string]any{"a": 2, "b": 1},
		"cadence":        "RECURRING",
		"label":          name,
		"tags":           []byte("null"),
		"tenant_id":      "tenant_1",
		"environment_id": "env_1",
		"updated_at":     time.Unix(2, 0),
		"updated_by":     "user_2",
	}
	if changes, changed := Diff(subDef, old, new); changed {
		t.Fatalf("expected no change across representation-only differences, got %+v", changes)
	}
}

func TestDiffJSONBKeyOrderOnlyIsNoOp(t *testing.T) {
	old := map[string]any{"metadata": []byte(`{"a":1,"b":2}`)}
	new := map[string]any{"metadata": map[string]any{"b": 2, "a": 1}}
	if _, changed := Diff(subDef, old, new); changed {
		t.Fatal("expected no change for reordered JSONB keys")
	}
}

func TestDiffCreateSkipsNilFields(t *testing.T) {
	new := map[string]any{"status": "active", "description": nil}
	changes, changed := Diff(subDef, nil, new)
	if !changed || len(changes) != 1 {
		t.Fatalf("want exactly status reported, got %+v", changes)
	}
	if _, ok := changes["description"]; ok {
		t.Fatalf("nil field with no old value should be absent, got %+v", changes["description"])
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
