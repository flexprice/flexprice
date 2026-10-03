package postgres

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"strings"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/internal/activity"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
)

// newRoutingTestClient builds a Client with distinct writer/reader ent clients
// so tests can assert which endpoint a context routes to. The clients carry no
// driver — no queries are executed.
func newRoutingTestClient() (*Client, *ent.Client, *ent.Client) {
	writer := ent.NewClient()
	reader := ent.NewClient()
	return &Client{
		writerClient: writer,
		readerClient: reader,
		hasReader:    true,
	}, writer, reader
}

func TestReader_DefaultsToReplica(t *testing.T) {
	c, _, reader := newRoutingTestClient()
	ctx := types.WithWriterPinning(context.Background())

	if got := c.Reader(ctx); got != reader {
		t.Fatal("expected read to route to reader before any write")
	}
}

func TestReader_ForceWriterFlag(t *testing.T) {
	c, writer, _ := newRoutingTestClient()
	ctx := types.WithForceWriter(context.Background())

	if got := c.Reader(ctx); got != writer {
		t.Fatal("expected force-writer context to route reads to writer")
	}
}

func TestReader_PinnedAfterWrite(t *testing.T) {
	c, writer, reader := newRoutingTestClient()
	ctx := types.WithWriterPinning(context.Background())

	if got := c.Reader(ctx); got != reader {
		t.Fatal("expected reader before any write")
	}

	// Simulate a write: fetching the writer client pins the unit of work
	if got := c.Writer(ctx); got != writer {
		t.Fatal("expected Writer to return writer client")
	}

	if got := c.Reader(ctx); got != writer {
		t.Fatal("expected reads after a write to route to writer (read-your-writes)")
	}
}

func TestReader_PinFromDerivedContextAffectsParent(t *testing.T) {
	c, writer, reader := newRoutingTestClient()
	root := types.WithWriterPinning(context.Background())

	// A write deep in the call stack on a derived context...
	derived := types.SetTenantID(root, "tenant-1")
	_ = c.Writer(derived)

	// ...pins reads issued later on the root (same request)
	if got := c.Reader(root); got != writer {
		t.Fatal("expected pin set on derived context to affect parent context reads")
	}

	// A separate unit of work stays on the replica
	other := types.WithWriterPinning(context.Background())
	if got := c.Reader(other); got != reader {
		t.Fatal("expected unrelated unit of work to keep reading from replica")
	}
}

func TestReader_UnpinnedContextWithoutHolderStaysOnReplica(t *testing.T) {
	c, _, reader := newRoutingTestClient()
	// Context without a pin holder (e.g. a flow not yet covered by an
	// entrypoint): writes don't pin, reads stay on replica — matches the
	// pre-pinning behavior.
	ctx := context.Background()
	_ = c.Writer(ctx)

	if got := c.Reader(ctx); got != reader {
		t.Fatal("expected context without pin holder to keep reading from replica")
	}
}

// stubDriver is a database/sql driver that can begin, commit, roll back and
// run Exec — enough for withTx and the activity flush it performs. Exec only
// records the statement, so it needs no schema and no database.
type stubDriver struct {
	rollbacks *int
	events    *[]string
}

func (d stubDriver) Open(string) (driver.Conn, error) {
	return stubConn{rollbacks: d.rollbacks, events: d.events}, nil
}

type stubConn struct {
	rollbacks *int
	events    *[]string
}

func (c stubConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("no statements in stub")
}
func (c stubConn) Close() error { return nil }
func (c stubConn) Begin() (driver.Tx, error) {
	return stubTx{rollbacks: c.rollbacks, events: c.events}, nil
}
func (c stubConn) ExecContext(_ context.Context, q string, _ []driver.NamedValue) (driver.Result, error) {
	record(c.events, "exec: "+q)
	return driver.RowsAffected(1), nil
}

type stubTx struct {
	rollbacks *int
	events    *[]string
}

func (t stubTx) Commit() error {
	record(t.events, "commit")
	return nil
}
func (t stubTx) Rollback() error {
	if t.rollbacks != nil {
		*t.rollbacks++
	}
	record(t.events, "rollback")
	return nil
}

func record(events *[]string, e string) {
	if events != nil {
		*events = append(*events, e)
	}
}

// newTxTestClient builds a Client whose writer can open real transactions
// against the stub driver above.
func newTxTestClient(t *testing.T, rollbacks *int) *Client {
	t.Helper()
	return newStubClient(t, stubDriver{rollbacks: rollbacks})
}

func newStubClient(t *testing.T, d stubDriver, opts ...Option) *Client {
	t.Helper()
	name := fmt.Sprintf("stub-%s", t.Name())
	sql.Register(name, d)
	db, err := sql.Open(name, "")
	if err != nil {
		t.Fatalf("opening stub db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	drv := entsql.OpenDB(dialect.Postgres, db)
	writer := ent.NewClient(ent.Driver(drv))
	return NewClient(&EntClients{Writer: writer, Reader: writer}, logger.NewNoopLogger(), nil, opts...).(*Client)
}

// TestWithTx_RunsPostCommitHooksAfterCommit: work registered inside the
// transaction must not run until the commit has published its writes.
func TestWithTx_RunsPostCommitHooksAfterCommit(t *testing.T) {
	c := newTxTestClient(t, nil)

	var ranInside, ranAfter bool
	err := c.withTx(context.Background(), func(txCtx context.Context) error {
		if !types.RegisterPostCommit(txCtx, func() { ranAfter = true }) {
			t.Fatal("a transaction context must accept post-commit registration")
		}
		ranInside = ranAfter
		return nil
	})
	if err != nil {
		t.Fatalf("withTx: %v", err)
	}
	if ranInside {
		t.Fatal("the hook ran while the transaction was still open")
	}
	if !ranAfter {
		t.Fatal("the hook never ran after the commit")
	}
}

// TestWithTx_DiscardsPostCommitHooksOnRollback: the writes the work would read
// never landed, so the work is dropped and a later caller runs inline.
func TestWithTx_DiscardsPostCommitHooksOnRollback(t *testing.T) {
	rollbacks := 0
	c := newTxTestClient(t, &rollbacks)

	var ran bool
	var txCtx context.Context
	wantErr := errors.New("boom")
	err := c.withTx(context.Background(), func(ctx context.Context) error {
		txCtx = ctx
		types.RegisterPostCommit(ctx, func() { ran = true })
		return wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected the original error back, got %v", err)
	}
	if rollbacks != 1 {
		t.Fatalf("expected one rollback, got %d", rollbacks)
	}
	if ran {
		t.Fatal("work queued by a rolled-back transaction must not run")
	}
	if types.RegisterPostCommit(txCtx, func() {}) {
		t.Fatal("registration must be closed once the transaction has ended")
	}
}

func actorCtx() context.Context {
	ctx := types.SetTenantID(context.Background(), "t1")
	ctx = types.SetEnvironmentID(ctx, "e1")
	return types.SetActor(ctx, types.Actor{Type: types.ActorTypeUser, ID: "u1"})
}

// TestWithTx_FlushesActivityBeforeCommit: the activity rows are written inside
// the transaction, so they commit or roll back with the business writes.
func TestWithTx_FlushesActivityBeforeCommit(t *testing.T) {
	var events []string
	c := newStubClient(t, stubDriver{events: &events}, WithActivity(activity.NewRegistry()))

	err := c.withTx(actorCtx(), func(ctx context.Context) error {
		activity.RecordAction(ctx, activity.Entry{EntityType: "customer", EntityID: "cus_1", Action: "customer.noted"})
		return nil
	})
	if err != nil {
		t.Fatalf("withTx: %v", err)
	}
	if len(events) != 2 || !strings.HasPrefix(events[0], "exec: INSERT INTO activity_logs") || events[1] != "commit" {
		t.Fatalf("want the activity insert then the commit, got %v", events)
	}
}

// TestWithTx_EmptyActorRollsBack: a flush refused for a missing actor fails
// the transaction instead of committing writes nobody can be blamed for.
func TestWithTx_EmptyActorRollsBack(t *testing.T) {
	var events []string
	c := newStubClient(t, stubDriver{events: &events}, WithActivity(activity.NewRegistry()))

	err := c.withTx(context.Background(), func(ctx context.Context) error {
		activity.RecordAction(ctx, activity.Entry{EntityType: "customer", EntityID: "cus_1", Action: "customer.noted"})
		return nil
	})
	if !errors.Is(err, activity.ErrEmptyActor) {
		t.Fatalf("want ErrEmptyActor, got %v", err)
	}
	if len(events) != 1 || events[0] != "rollback" {
		t.Fatalf("want only a rollback, got %v", events)
	}
}

// TestWithTx_ClosesCollectorOnError: a rolled-back transaction's entries are
// dropped and nothing more can be recorded into them.
func TestWithTx_ClosesCollectorOnError(t *testing.T) {
	var events []string
	c := newStubClient(t, stubDriver{events: &events}, WithActivity(activity.NewRegistry()))

	var txCtx context.Context
	_ = c.withTx(actorCtx(), func(ctx context.Context) error {
		txCtx = ctx
		activity.RecordAction(ctx, activity.Entry{EntityType: "customer", EntityID: "cus_1", Action: "customer.noted"})
		return errors.New("boom")
	})
	col := activity.CollectorFrom(txCtx)
	if col == nil {
		t.Fatal("withTx must install a collector")
	}
	activity.RecordAction(txCtx, activity.Entry{EntityType: "customer", EntityID: "cus_2", Action: "customer.noted"})
	for _, e := range col.Entries() {
		if e.EntityID == "cus_2" {
			t.Fatal("a closed collector must accept no more entries")
		}
	}
	for _, e := range events {
		if strings.HasPrefix(e, "exec:") {
			t.Fatalf("a failed transaction must not flush, got %v", events)
		}
	}
}

// TestWithTx_ClosesCollectorOnPanic: the panic path drops entries like the error path.
func TestWithTx_ClosesCollectorOnPanic(t *testing.T) {
	c := newStubClient(t, stubDriver{}, WithActivity(activity.NewRegistry()))

	var txCtx context.Context
	func() {
		defer func() { _ = recover() }()
		_ = c.withTx(actorCtx(), func(ctx context.Context) error {
			txCtx = ctx
			panic("boom")
		})
	}()
	activity.RecordAction(txCtx, activity.Entry{EntityType: "customer", EntityID: "cus_1", Action: "customer.noted"})
	if n := len(activity.CollectorFrom(txCtx).Entries()); n != 0 {
		t.Fatalf("a closed collector must accept no more entries, got %d", n)
	}
}

// TestWithTx_NoActivityWithoutRegistry: clients built without WithActivity
// (scripts, tests) install no collector, so they never flush or need an actor.
func TestWithTx_NoActivityWithoutRegistry(t *testing.T) {
	c := newTxTestClient(t, nil)
	err := c.withTx(context.Background(), func(ctx context.Context) error {
		if activity.CollectorFrom(ctx) != nil {
			t.Fatal("no collector without a registry")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("withTx: %v", err)
	}
}
