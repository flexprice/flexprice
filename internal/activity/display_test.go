package activity

import "testing"

func TestSummaryTiers(t *testing.T) {
	def := Definition{EntityType: "subscription",
		Label:   func(f map[string]any) string { return "sub_1" },
		Actions: map[string]string{"subscription.paused": "{actor} paused {entity}"}}
	in := SummaryInput{ActorLabel: "Alice", EntityLabel: "sub_1"}

	in.Action = "subscription.paused"
	if got := Summary(def, in); got != "Alice paused sub_1" {
		t.Fatalf("tier 2: %q", got)
	}
	in.Action = "subscription.updated"
	in.Changes = map[string]any{"status": map[string]any{"from": "a", "to": "b"}}
	if got := Summary(def, in); got != "Alice updated subscription sub_1: status from a to b" {
		t.Fatalf("tier 1 single: %q", got)
	}
	in.Changes = map[string]any{"end_date": map[string]any{"from": nil, "to": "2026-10-02"}}
	if got := Summary(def, in); got != "Alice updated subscription sub_1: end date from none to 2026-10-02" {
		t.Fatalf("tier 1 unset from: %q", got)
	}
	in.Changes = map[string]any{"status": map[string]any{"from": "a", "to": "b"}}
	in.Changes["plan_id"] = map[string]any{"from": "x", "to": "y"}
	if got := Summary(def, in); got != "Alice updated 2 fields on subscription sub_1" {
		t.Fatalf("tier 1 multi: %q", got)
	}
	in.Action = "subscription.created"
	in.Changes = nil
	if got := Summary(def, in); got != "Alice created subscription sub_1" {
		t.Fatalf("tier 1 create: %q", got)
	}
}

func TestAnnotateChanges(t *testing.T) {
	def := Definition{FieldLabels: map[string]FieldDisplay{"plan_id": {Label: "Plan", Format: "text"}}}
	out := AnnotateChanges(def, map[string]any{
		"plan_id":        map[string]any{"from": "a", "to": "b"},
		"billing_anchor": map[string]any{"from": "x", "to": "y"},
	})
	if out["plan_id"].(map[string]any)["label"] != "Plan" {
		t.Fatal("override label missing")
	}
	if out["billing_anchor"].(map[string]any)["label"] != "Billing anchor" {
		t.Fatalf("humanize failed: %v", out["billing_anchor"])
	}
	ref := AnnotateChanges(def, map[string]any{"plan_id": map[string]any{"from": "plan_01A", "to": "plan_01B"}})
	if ref["plan_id"].(map[string]any)["format"] != "ref:plan" {
		t.Fatalf("prefix ref detection failed: %v", ref["plan_id"])
	}
	meta := AnnotateMetadata(def, map[string]any{"from_plan_id": "plan_01A", "reason": "x"})
	if meta["from_plan_id"].(map[string]any)["format"] != "ref:plan" || meta["reason"].(map[string]any)["value"] != "x" {
		t.Fatalf("metadata annotation failed: %v", meta)
	}
}
