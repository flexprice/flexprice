package activity

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	flexent "github.com/flexprice/flexprice/ent"
)

type fakeMutation struct {
	ent.Mutation
	op      ent.Op
	typ     string
	fields  map[string]any
	cleared []string
	ids     []string
}

func (f *fakeMutation) ClearedFields() []string { return f.cleared }

func (f *fakeMutation) Op() ent.Op   { return f.op }
func (f *fakeMutation) Type() string { return f.typ }
func (f *fakeMutation) Fields() []string {
	out := make([]string, 0, len(f.fields))
	for k := range f.fields {
		out = append(out, k)
	}
	return out
}
func (f *fakeMutation) Field(name string) (ent.Value, bool)   { v, ok := f.fields[name]; return v, ok }
func (f *fakeMutation) IDs(context.Context) ([]string, error) { return f.ids, nil }
func (f *fakeMutation) ID() (string, bool) {
	if len(f.ids) == 1 {
		return f.ids[0], true
	}
	return "", false
}

type fakeOld map[string]map[string]any // id -> columns

func (o fakeOld) oldValues(_ context.Context, _ Definition, ids []string, cols []string) (map[string]map[string]any, error) {
	out := map[string]map[string]any{}
	for _, id := range ids {
		row := map[string]any{}
		for _, c := range cols {
			row[c] = o[id][c]
		}
		out[id] = row
	}
	return out, nil
}

func TestHookRecordsUpdateDiff(t *testing.T) {
	reg := NewRegistry(Definition{EntType: "Subscription", EntityType: "subscription", Table: "subscriptions"})
	ctx := WithCollector(context.Background())
	m := &fakeMutation{op: ent.OpUpdate, typ: "Subscription", ids: []string{"sub_1"},
		fields: map[string]any{"status": "paused", "plan_id": "plan_1", "updated_at": "x"}}
	old := fakeOld{"sub_1": {"status": "active", "plan_id": "plan_1", "updated_at": "y"}}
	next := ent.MutateFunc(func(ctx context.Context, m ent.Mutation) (ent.Value, error) { return nil, nil })
	h := hook{reg: reg, old: old.oldValues}
	if _, err := h.mutate(ctx, next, m); err != nil {
		t.Fatal(err)
	}
	e := CollectorFrom(ctx).Entries()
	if len(e) != 1 || e[0].Op != OpUpdate || e[0].Changes["status"].To != "paused" || len(e[0].Changes) != 1 {
		t.Fatalf("unexpected %+v", e)
	}
}

func TestHookRecordsClearedField(t *testing.T) {
	reg := NewRegistry(Definition{EntType: "Subscription", EntityType: "subscription", Table: "subscriptions"})
	ctx := WithCollector(context.Background())
	m := &fakeMutation{op: ent.OpUpdateOne, typ: "Subscription", ids: []string{"sub_1"},
		cleared: []string{"cancel_at", "pause_reason"}}
	old := fakeOld{"sub_1": {"cancel_at": "2026-11-01T00:00:00Z", "pause_reason": nil}}
	h := hook{reg: reg, old: old.oldValues}
	if _, err := h.mutate(ctx, noopNext(), m); err != nil {
		t.Fatal(err)
	}
	e := CollectorFrom(ctx).Entries()
	if len(e) != 1 || len(e[0].Changes) != 1 {
		t.Fatalf("want one update with one change, got %+v", e)
	}
	ch, ok := e[0].Changes["cancel_at"]
	if !ok || ch.From != "2026-11-01T00:00:00Z" || ch.To != nil {
		t.Fatalf("want cancel_at from the old value to nil, got %+v", e[0].Changes)
	}
}

func TestHookSoftDeleteIsDelete(t *testing.T) {
	reg := NewRegistry(Definition{EntType: "Customer", EntityType: "customer", Table: "customers"})
	ctx := WithCollector(context.Background())
	m := &fakeMutation{op: ent.OpUpdate, typ: "Customer", ids: []string{"cus_1"}, fields: map[string]any{"status": "deleted"}}
	old := fakeOld{"cus_1": {"status": "published"}}
	next := ent.MutateFunc(func(ctx context.Context, m ent.Mutation) (ent.Value, error) { return nil, nil })
	h := hook{reg: reg, old: old.oldValues}
	_, _ = h.mutate(ctx, next, m)
	if e := CollectorFrom(ctx).Entries(); len(e) != 1 || e[0].Op != OpDelete {
		t.Fatalf("want delete, got %+v", e)
	}
}

func TestHookCreateSnapshot(t *testing.T) {
	reg := NewRegistry(Definition{EntType: "Customer", EntityType: "customer", Table: "customers", RedactFields: []string{"tax_id"}})
	ctx := WithCollector(context.Background())
	m := &fakeMutation{op: ent.OpCreate, typ: "Customer", ids: []string{"cus_1"},
		fields: map[string]any{"id": "cus_1", "name": "Acme", "tax_id": "secret", "created_at": "x"}}
	next := ent.MutateFunc(func(ctx context.Context, m ent.Mutation) (ent.Value, error) { return nil, nil })
	h := hook{reg: reg}
	_, _ = h.mutate(ctx, next, m)
	e := CollectorFrom(ctx).Entries()[0]
	if e.Op != OpCreate || e.Snapshot["name"] != "Acme" || e.Snapshot["tax_id"] != "[redacted]" {
		t.Fatalf("unexpected snapshot %+v", e.Snapshot)
	}
	if _, has := e.Snapshot["created_at"]; has {
		t.Fatal("bookkeeping must not be in snapshot")
	}
}

func TestHookUnregisteredPassesThrough(t *testing.T) {
	reg := NewRegistry()
	ctx := WithCollector(context.Background())
	m := &fakeMutation{op: ent.OpUpdate, typ: "Meter", ids: []string{"m_1"}, fields: map[string]any{"name": "x"}}
	called := false
	next := ent.MutateFunc(func(ctx context.Context, m ent.Mutation) (ent.Value, error) { called = true; return nil, nil })
	h := hook{reg: reg}
	_, _ = h.mutate(ctx, next, m)
	if !called || len(CollectorFrom(ctx).Entries()) != 0 {
		t.Fatal("unregistered type must pass through untouched")
	}
}

func TestHookSuppressed(t *testing.T) {
	reg := NewRegistry(Definition{EntType: "Wallet", EntityType: "wallet", Table: "wallets"})
	ctx := Suppress(WithCollector(context.Background()), "balance tick")
	m := &fakeMutation{op: ent.OpUpdate, typ: "Wallet", ids: []string{"w_1"}, fields: map[string]any{"balance": "1"}}
	next := ent.MutateFunc(func(ctx context.Context, m ent.Mutation) (ent.Value, error) { return nil, nil })
	h := hook{reg: reg, old: fakeOld{"w_1": {"balance": "0"}}.oldValues}
	_, _ = h.mutate(ctx, next, m)
	if len(CollectorFrom(ctx).Entries()) != 0 {
		t.Fatal("suppressed mutation must not be collected")
	}
}

func noopNext() ent.Mutator {
	return ent.MutateFunc(func(ctx context.Context, m ent.Mutation) (ent.Value, error) { return nil, nil })
}

func TestHookDeleteRecordsEveryID(t *testing.T) {
	reg := NewRegistry(Definition{EntType: "Customer", EntityType: "customer", Table: "customers"})
	ctx := WithCollector(context.Background())
	m := &fakeMutation{op: ent.OpDelete, typ: "Customer", ids: []string{"cus_1", "cus_2"}}
	h := hook{reg: reg}
	if _, err := h.mutate(ctx, noopNext(), m); err != nil {
		t.Fatal(err)
	}
	e := CollectorFrom(ctx).Entries()
	if len(e) != 2 || e[0].Op != OpDelete || e[1].Op != OpDelete || e[0].EntityID != "cus_1" || e[1].EntityID != "cus_2" {
		t.Fatalf("want two deletes, got %+v", e)
	}
}

func TestHookDiffErrorDegradesRow(t *testing.T) {
	reg := NewRegistry(Definition{EntType: "Customer", EntityType: "customer", Table: "customers"})
	ctx := WithCollector(context.Background())
	m := &fakeMutation{op: ent.OpUpdateOne, typ: "Customer", ids: []string{"cus_1"}, fields: map[string]any{"name": "B"}}
	called := false
	next := ent.MutateFunc(func(ctx context.Context, m ent.Mutation) (ent.Value, error) { called = true; return nil, nil })
	h := hook{reg: reg, old: func(context.Context, Definition, []string, []string) (map[string]map[string]any, error) {
		return nil, errors.New("select failed")
	}}
	if _, err := h.mutate(ctx, next, m); err != nil {
		t.Fatalf("a diff error must not fail the mutation: %v", err)
	}
	e := CollectorFrom(ctx).Entries()
	if !called || len(e) != 1 || e[0].Degraded != "diff_error" || e[0].Op != OpUpdate {
		t.Fatalf("want one degraded update, got %+v", e)
	}
}

func TestHookMutationErrorRecordsNothing(t *testing.T) {
	reg := NewRegistry(Definition{EntType: "Customer", EntityType: "customer", Table: "customers"})
	ctx := WithCollector(context.Background())
	boom := errors.New("boom")
	next := ent.MutateFunc(func(ctx context.Context, m ent.Mutation) (ent.Value, error) { return nil, boom })
	h := hook{reg: reg, old: fakeOld{"cus_1": {"name": "A"}}.oldValues}
	for _, op := range []ent.Op{ent.OpCreate, ent.OpUpdateOne, ent.OpDeleteOne} {
		m := &fakeMutation{op: op, typ: "Customer", ids: []string{"cus_1"}, fields: map[string]any{"name": "B"}}
		if _, err := h.mutate(ctx, next, m); !errors.Is(err, boom) {
			t.Fatalf("%v: want the mutation error back, got %v", op, err)
		}
	}
	if e := CollectorFrom(ctx).Entries(); len(e) != 0 {
		t.Fatalf("a failed mutation must not be collected, got %+v", e)
	}
}

func TestHookUnchangedUpdateRecordsNothing(t *testing.T) {
	reg := NewRegistry(Definition{EntType: "Customer", EntityType: "customer", Table: "customers"})
	ctx := WithCollector(context.Background())
	m := &fakeMutation{op: ent.OpUpdateOne, typ: "Customer", ids: []string{"cus_1"}, fields: map[string]any{"name": "A", "updated_at": "now"}}
	h := hook{reg: reg, old: fakeOld{"cus_1": {"name": "A", "updated_at": "then"}}.oldValues}
	_, _ = h.mutate(ctx, noopNext(), m)
	if e := CollectorFrom(ctx).Entries(); len(e) != 0 {
		t.Fatalf("a no-op update must not be collected, got %+v", e)
	}
}

// stubConn is a database/sql driver connection that records every statement
// and answers queries from a fixed row set, so the hook's real SELECT and
// direct-write paths run without a database.
type stubConn struct {
	mu      *sync.Mutex
	execs   *[]string
	queries *[]string
	cols    []string
	rows    [][]driver.Value
	execErr error
}

func (c stubConn) Prepare(string) (driver.Stmt, error) { return nil, errors.New("no prepare in stub") }
func (c stubConn) Close() error                        { return nil }
func (c stubConn) Begin() (driver.Tx, error)           { return nil, errors.New("no tx in stub") }

func (c stubConn) ExecContext(_ context.Context, q string, _ []driver.NamedValue) (driver.Result, error) {
	c.mu.Lock()
	*c.execs = append(*c.execs, q)
	c.mu.Unlock()
	if c.execErr != nil {
		return nil, c.execErr
	}
	return driver.RowsAffected(1), nil
}

func (c stubConn) QueryContext(_ context.Context, q string, _ []driver.NamedValue) (driver.Rows, error) {
	c.mu.Lock()
	*c.queries = append(*c.queries, q)
	c.mu.Unlock()
	return &stubRows{cols: c.cols, rows: c.rows}, nil
}

type stubRows struct {
	cols []string
	rows [][]driver.Value
	i    int
}

func (r *stubRows) Columns() []string { return r.cols }
func (r *stubRows) Close() error      { return nil }
func (r *stubRows) Next(dest []driver.Value) error {
	if r.i >= len(r.rows) {
		return io.EOF
	}
	copy(dest, r.rows[r.i])
	r.i++
	return nil
}

type stubDriver struct{ conn stubConn }

func (d stubDriver) Open(string) (driver.Conn, error) { return d.conn, nil }

// stubClient returns an ent client over the stub driver and the statements it saw.
func stubClient(t *testing.T, conn stubConn) (*flexent.Client, *[]string, *[]string) {
	t.Helper()
	execs, queries := &[]string{}, &[]string{}
	conn.mu, conn.execs, conn.queries = &sync.Mutex{}, execs, queries
	name := fmt.Sprintf("activity-stub-%s", t.Name())
	sql.Register(name, stubDriver{conn: conn})
	db, err := sql.Open(name, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return flexent.NewClient(flexent.Driver(entsql.OpenDB(dialect.Postgres, db))), execs, queries
}

// clientMutation is a fakeMutation that, like generated ent mutations,
// exposes the client it runs on.
type clientFakeMutation struct {
	*fakeMutation
	client *flexent.Client
}

func (m clientFakeMutation) Client() *flexent.Client { return m.client }

func TestHookDirectWriteWithoutCollector(t *testing.T) {
	reg := NewRegistry(Definition{EntType: "Customer", EntityType: "customer", Table: "customers"})
	client, execs, _ := stubClient(t, stubConn{})
	m := clientFakeMutation{&fakeMutation{op: ent.OpCreate, typ: "Customer", ids: []string{"cus_1"}, fields: map[string]any{"name": "Acme"}}, client}
	h := hook{reg: reg}
	if _, err := h.mutate(baseCtx(), noopNext(), m); err != nil {
		t.Fatal(err)
	}
	if len(*execs) != 1 || !strings.HasPrefix((*execs)[0], "INSERT INTO activity_logs") {
		t.Fatalf("want one direct insert on the mutation's client, got %v", *execs)
	}
}

func TestHookDirectWriteErrorDoesNotFailMutation(t *testing.T) {
	reg := NewRegistry(Definition{EntType: "Customer", EntityType: "customer", Table: "customers"})
	client, execs, _ := stubClient(t, stubConn{execErr: errors.New("insert failed")})
	m := clientFakeMutation{&fakeMutation{op: ent.OpCreate, typ: "Customer", ids: []string{"cus_1"}, fields: map[string]any{"name": "Acme"}}, client}
	h := hook{reg: reg}
	if _, err := h.mutate(baseCtx(), noopNext(), m); err != nil {
		t.Fatalf("a direct-write failure must not fail the mutation: %v", err)
	}
	if len(*execs) != 1 {
		t.Fatalf("want one attempted insert, got %v", *execs)
	}
}

func TestHookSelectsOldValues(t *testing.T) {
	reg := NewRegistry(Definition{EntType: "Customer", EntityType: "customer", Table: "customers"})
	client, _, queries := stubClient(t, stubConn{
		cols: []string{"id", "name"},
		rows: [][]driver.Value{{"cus_1", []byte("Old")}, {"cus_2", []byte("New")}},
	})
	ctx := WithCollector(context.Background())
	m := clientFakeMutation{&fakeMutation{op: ent.OpUpdate, typ: "Customer", ids: []string{"cus_1", "cus_2"}, fields: map[string]any{"name": "New"}}, client}
	h := hook{reg: reg}
	if _, err := h.mutate(ctx, noopNext(), m); err != nil {
		t.Fatal(err)
	}
	if len(*queries) != 1 || !strings.Contains((*queries)[0], `FROM "customers" WHERE id = ANY($1)`) {
		t.Fatalf("want one SELECT of old values, got %v", *queries)
	}
	e := CollectorFrom(ctx).Entries()
	if len(e) != 1 || e[0].EntityID != "cus_1" || e[0].Changes["name"].From != "Old" || e[0].Changes["name"].To != "New" {
		t.Fatalf("want only cus_1 changed Old->New, got %+v", e)
	}
}

func TestHookHardDeleteCarriesLabelAndRollup(t *testing.T) {
	def := Definition{
		EntType: "SubscriptionLineItem", EntityType: "subscription_line_item", Table: "subscription_line_items",
		LabelFields: []string{"display_name", "subscription_id"}, ParentFields: []string{"customer_id"},
		CustomerID: func(f map[string]any) string { return str(f, "customer_id") },
		Label:      func(f map[string]any) string { return str(f, "display_name") },
	}
	reg := NewRegistry(def)
	client, _, queries := stubClient(t, stubConn{
		cols: []string{"id", "display_name", "subscription_id", "customer_id"},
		rows: [][]driver.Value{{"li_1", "Seats", "sub_1", "cus_1"}},
	})
	ctx := WithCollector(baseCtx())
	m := clientFakeMutation{&fakeMutation{op: ent.OpDeleteOne, typ: "SubscriptionLineItem", ids: []string{"li_1"}}, client}
	h := hook{reg: reg}
	if _, err := h.mutate(ctx, noopNext(), m); err != nil {
		t.Fatal(err)
	}
	if len(*queries) != 1 || !strings.Contains((*queries)[0], `SELECT id, "display_name", "subscription_id", "customer_id" FROM "subscription_line_items"`) {
		t.Fatalf("want one SELECT of label and parent columns before the delete, got %v", *queries)
	}
	e := CollectorFrom(ctx).Entries()
	if len(e) != 1 || e[0].Op != OpDelete || e[0].Label != "Seats" {
		t.Fatalf("want one labelled delete, got %+v", e)
	}
	if e[0].Fields["customer_id"] != "cus_1" || e[0].Fields["subscription_id"] != "sub_1" {
		t.Fatalf("want customer and subscription roll-up fields, got %+v", e[0].Fields)
	}
	ex := &recExec{}
	if err := Flush(ctx, ex, reg, nil); err != nil {
		t.Fatal(err)
	}
	if !contains(ex.args, "subscription_line_item.deleted") || !contains(ex.args, "Seats") || !contains(ex.args, "cus_1") || !contains(ex.args, "sub_1") {
		t.Fatalf("want the delete row to carry label, customer_id and subscription_id, got %v", ex.args)
	}
}

// A wallet's stored balance changes only when a transaction is applied, so it is recorded;
// the frequent background evaluations are suppressed at their call sites.
func TestWalletBalanceChangesAreRecorded(t *testing.T) {
	reg := NewRegistry(Definitions()...)
	ctx := WithCollector(context.Background())
	m := &fakeMutation{op: ent.OpUpdate, typ: "Wallet", ids: []string{"w_1"},
		fields: map[string]any{"balance": "300", "credit_balance": "300"}}
	old := fakeOld{"w_1": {"balance": "200", "credit_balance": "200"}}
	h := hook{reg: reg, old: old.oldValues}
	if _, err := h.mutate(ctx, noopNext(), m); err != nil {
		t.Fatal(err)
	}
	e := CollectorFrom(ctx).Entries()
	if len(e) != 1 || e[0].Op != OpUpdate {
		t.Fatalf("want one update entry, got %+v", e)
	}
	for _, f := range []string{"balance", "credit_balance"} {
		ch, ok := e[0].Changes[f]
		if !ok || ch.From != "200" || ch.To != "300" {
			t.Fatalf("%s change not recorded as 200 -> 300: %+v", f, e[0].Changes)
		}
	}
}
