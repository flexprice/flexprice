package activity

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"entgo.io/ent"
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
	argActorUser   = 11
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

// A system write derived inside a user's request keeps its own actor, even though
// the batch is flushed with the request's context.
func TestFlushUsesEachEntrysOwnActor(t *testing.T) {
	reg := NewRegistry(
		Definition{EntType: "Subscription", EntityType: "subscription", Table: "subscriptions"},
		Definition{EntType: "Invoice", EntityType: "invoice", Table: "invoices"},
	)
	ctx := WithCollector(baseCtx())
	next := ent.MutateFunc(func(ctx context.Context, m ent.Mutation) (ent.Value, error) { return nil, nil })
	h := hook{reg: reg}
	create := func(ctx context.Context, typ, id string) {
		m := &fakeMutation{op: ent.OpCreate, typ: typ, ids: []string{id}, fields: map[string]any{"id": id}}
		if _, err := h.mutate(ctx, next, m); err != nil {
			t.Fatal(err)
		}
	}
	create(ctx, "Subscription", "sub_1")
	create(types.WithDerivedSystemActor(ctx, "subscription_billing", "Subscription billing"), "Invoice", "inv_1")
	// RecordAction under the derived actor is attributed to it too.
	RecordAction(types.WithDerivedSystemActor(ctx, "subscription_billing", "Subscription billing"), Entry{EntityType: "invoice", EntityID: "inv_2", Action: "invoice.finalized"})

	ex := &recExec{}
	if err := Flush(ctx, ex, reg, nil); err != nil {
		t.Fatal(err)
	}
	const cols = 22
	if len(ex.args) != 3*cols {
		t.Fatalf("want 3 rows, got %d args", len(ex.args))
	}
	row := func(i int) []any { return ex.args[i*cols : (i+1)*cols] }
	if row(0)[argActorType] != "user" || actorUserArg(row(0)) != "" {
		t.Fatalf("subscription row must keep the user: %v", row(0))
	}
	for i := 1; i <= 2; i++ {
		r := row(i)
		if r[argActorType] != "system" || r[argActorLabel] != "Subscription billing" || actorUserArg(r) != "u1" {
			t.Fatalf("row %d must be system on behalf of u1: %v", i, r)
		}
	}
}

// actorUserArg reads the nullable actor_user_id argument of a flushed row; "" is NULL.
func actorUserArg(row []any) string {
	if p, _ := row[argActorUser].(*string); p != nil {
		return *p
	}
	return ""
}
