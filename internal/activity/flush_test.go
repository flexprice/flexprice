package activity

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/flexprice/flexprice/internal/types"
)

type recExec struct {
	q    string
	args []any
}

func (r *recExec) ExecContext(_ context.Context, q string, args ...any) (sql.Result, error) {
	r.q, r.args = q, args
	return nil, nil
}

func baseCtx() context.Context {
	ctx := types.SetTenantID(context.Background(), "t1")
	ctx = types.SetEnvironmentID(ctx, "e1")
	ctx = types.SetActor(ctx, types.Actor{Type: types.ActorTypeUser, ID: "u1", Label: "Alice"})
	ctx = types.SetSource(ctx, types.SourceDashboard)
	return ctx
}

func TestFlushBuildsOneInsert(t *testing.T) {
	reg := NewRegistry(Definition{EntType: "Subscription", EntityType: "subscription", Table: "subscriptions",
		CustomerID: func(f map[string]any) string { s, _ := f["customer_id"].(string); return s }})
	ctx := WithCollector(baseCtx())
	c := CollectorFrom(ctx)
	c.Add(Record{EntityType: "subscription", EntityID: "sub_1", Op: OpUpdate, Label: "growth-acme",
		Changes: map[string]Change{"status": {From: "active", To: "paused"}}, Fields: map[string]any{"customer_id": "cus_1"}})
	c.Name(Entry{EntityType: "subscription", EntityID: "sub_1", Action: "subscription.paused"})
	c.Add(Record{EntityType: "subscription", EntityID: "sub_2", Op: OpUpdate,
		Changes: map[string]Change{"status": {From: "a", To: "b"}}})
	ex := &recExec{}
	if err := Flush(ctx, ex, reg, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(ex.q, "INSERT INTO activity_logs") || strings.Count(ex.q, "($") != 2 || !strings.Contains(ex.q, "subscription_id") {
		t.Fatalf("want one multi-row insert, got %s", ex.q)
	}
	if !contains(ex.args, "subscription.paused") || !contains(ex.args, "subscription.updated") || !contains(ex.args, "cus_1") || !contains(ex.args, "growth-acme") {
		t.Fatalf("args missing action or customer id: %v", ex.args)
	}
}

func TestFlushRefusesEmptyActor(t *testing.T) {
	reg := NewRegistry(Definition{EntType: "Customer", EntityType: "customer", Table: "customers"})
	ctx := WithCollector(types.SetTenantID(types.SetEnvironmentID(context.Background(), "e"), "t"))
	CollectorFrom(ctx).Add(Record{EntityType: "customer", EntityID: "c1", Op: OpCreate, Snapshot: map[string]any{}})
	if err := Flush(ctx, &recExec{}, reg, nil); err != ErrEmptyActor {
		t.Fatalf("want ErrEmptyActor, got %v", err)
	}
}

func TestFlushNothingPending(t *testing.T) {
	ex := &recExec{}
	if err := Flush(WithCollector(baseCtx()), ex, NewRegistry(), nil); err != nil || ex.q != "" {
		t.Fatal("empty collector must not execute")
	}
}

// insert column positions within one row of args.
const (
	argTenant      = 1
	argEnv         = 2
	argEntityLabel = 6
	argAction      = 7
	argActorType   = 8
	argActorLabel  = 10
	argSubID       = 14
	argChanges     = 18
	argSnapshot    = 19
	argMetadata    = 20
)

func flushOne(t *testing.T, ctx context.Context, reg *Registry) *recExec {
	t.Helper()
	ex := &recExec{}
	if err := Flush(ctx, ex, reg, nil); err != nil {
		t.Fatal(err)
	}
	if len(ex.args) != 22 {
		t.Fatalf("want 22 args for one row, got %d", len(ex.args))
	}
	return ex
}

func TestFlushRowCarriesTenantAndActor(t *testing.T) {
	ctx := WithCollector(baseCtx())
	CollectorFrom(ctx).Add(Record{EntityType: "customer", EntityID: "c1", Op: OpCreate, Snapshot: map[string]any{"name": "Acme"}})
	ex := flushOne(t, ctx, NewRegistry())
	if ex.args[argTenant] != "t1" || ex.args[argEnv] != "e1" || ex.args[argActorType] != "user" || ex.args[argActorLabel] != "Alice" {
		t.Fatalf("tenant, environment or actor missing: %v", ex.args)
	}
	if ex.args[argAction] != "customer.created" || ex.args[argSnapshot] != `{"name":"Acme"}` || ex.args[argChanges] != nil {
		t.Fatalf("create row wrong: %v", ex.args)
	}
}

func TestFlushPureActionIsNotCreate(t *testing.T) {
	ctx := WithCollector(baseCtx())
	RecordAction(ctx, Entry{EntityType: "subscription", EntityID: "sub_1", Action: "subscription.renewal_reminded",
		Metadata: map[string]any{"days": 3}})
	ex := flushOne(t, ctx, NewRegistry())
	if ex.args[argAction] != "subscription.renewal_reminded" || ex.args[argSnapshot] != nil || ex.args[argChanges] != nil {
		t.Fatalf("pure action must carry its own action and no snapshot: %v", ex.args)
	}
	if ex.args[argMetadata] != `{"days":3}` {
		t.Fatalf("metadata missing: %v", ex.args[argMetadata])
	}
	if ex.args[argSubID] != "sub_1" {
		t.Fatalf("subscription row must carry its own id as subscription_id: %v", ex.args[argSubID])
	}
}

func TestFlushDegradedGoesToMetadata(t *testing.T) {
	ctx := WithCollector(baseCtx())
	CollectorFrom(ctx).Add(Record{EntityType: "customer", EntityID: "c1", Op: OpUpdate, Degraded: "diff_error", Changes: map[string]Change{}})
	ex := flushOne(t, ctx, NewRegistry())
	if ex.args[argMetadata] != `{"degraded":"diff_error"}` || ex.args[argAction] != "customer.updated" {
		t.Fatalf("degraded row wrong: %v", ex.args)
	}
}

func TestFlushTruncatesLabels(t *testing.T) {
	long := strings.Repeat("é", 300)
	ctx := types.SetActor(baseCtx(), types.Actor{Type: types.ActorTypeUser, ID: "u1", Label: long})
	ctx = WithCollector(ctx)
	CollectorFrom(ctx).Add(Record{EntityType: "customer", EntityID: "c1", Op: OpCreate, Label: long, Snapshot: map[string]any{}})
	ex := flushOne(t, ctx, NewRegistry())
	for _, i := range []int{argEntityLabel, argActorLabel} {
		if s, _ := ex.args[i].(string); len([]rune(s)) != 255 {
			t.Fatalf("arg %d: want 255 runes, got %d", i, len([]rune(s)))
		}
	}
}

func TestFlushClosesCollector(t *testing.T) {
	ctx := WithCollector(baseCtx())
	_ = Flush(ctx, &recExec{}, NewRegistry(), nil)
	CollectorFrom(ctx).Add(Record{EntityType: "customer", EntityID: "c1", Op: OpCreate})
	if len(CollectorFrom(ctx).Entries()) != 0 {
		t.Fatal("a flushed collector must accept no more records")
	}
}

func contains(args []any, want string) bool {
	for _, a := range args {
		if s, ok := a.(string); ok && s == want {
			return true
		}
	}
	return false
}
