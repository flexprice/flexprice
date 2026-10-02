package activity

import (
	"context"
	"testing"
)

func TestCollectorMergesSameEntity(t *testing.T) {
	ctx := WithCollector(context.Background())
	c := CollectorFrom(ctx)
	c.Add(Record{EntityType: "subscription", EntityID: "sub_1", Op: OpUpdate,
		Changes: map[string]Change{"status": {From: "active", To: "paused"}}})
	c.Add(Record{EntityType: "subscription", EntityID: "sub_1", Op: OpUpdate,
		Changes: map[string]Change{"status": {From: "paused", To: "active"}, "plan_id": {From: "a", To: "b"}}})
	entries := c.Entries()
	if len(entries) != 1 {
		t.Fatalf("want 1 merged entry, got %d", len(entries))
	}
	e := entries[0]
	if e.Changes["status"].From != "active" || e.Changes["status"].To != "active" {
		t.Fatalf("merge should keep first from and last to: %+v", e.Changes["status"])
	}
	if e.Changes["plan_id"].To != "b" {
		t.Fatal("union of fields lost plan_id")
	}
}

func TestCollectorCreateThenUpdateIsCreate(t *testing.T) {
	ctx := WithCollector(context.Background())
	c := CollectorFrom(ctx)
	c.Add(Record{EntityType: "customer", EntityID: "cus_1", Op: OpCreate, Snapshot: map[string]any{"name": "a"}})
	c.Add(Record{EntityType: "customer", EntityID: "cus_1", Op: OpUpdate,
		Changes: map[string]Change{"name": {From: "a", To: "b"}}, Fields: map[string]any{"name": "b"}})
	e := c.Entries()[0]
	if e.Op != OpCreate || e.Snapshot["name"] != "b" {
		t.Fatalf("want create with final snapshot, got op=%v snap=%v", e.Op, e.Snapshot)
	}
}

func TestRecordActionNamesEntryEitherOrder(t *testing.T) {
	ctx := WithCollector(context.Background())
	RecordAction(ctx, Entry{EntityType: "subscription", EntityID: "sub_1", Action: "subscription.paused",
		Metadata: map[string]any{"reason": "x"}})
	CollectorFrom(ctx).Add(Record{EntityType: "subscription", EntityID: "sub_1", Op: OpUpdate,
		Changes: map[string]Change{"status": {From: "active", To: "paused"}}})
	e := CollectorFrom(ctx).Entries()[0]
	if e.Action != "subscription.paused" || e.Metadata["reason"] != "x" {
		t.Fatalf("name not applied: %+v", e)
	}
}

func TestSuppressRequiresReason(t *testing.T) {
	ctx := WithCollector(context.Background())
	sctx := Suppress(ctx, "balance tick")
	if !IsSuppressed(sctx) || IsSuppressed(ctx) {
		t.Fatal("suppression scoping wrong")
	}
	defer func() {
		if recover() == nil {
			t.Fatal("empty reason must panic")
		}
	}()
	Suppress(ctx, "")
}
