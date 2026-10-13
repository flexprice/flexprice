package postgres

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/flexprice/flexprice/ent"
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

// stubDriver is a database/sql driver that can only begin, commit and roll
// back — enough for withTx, which is what these tests exercise. No statement
// ever reaches it, so it needs no schema and no database.
type stubDriver struct{ rollbacks *int }

func (d stubDriver) Open(string) (driver.Conn, error) { return stubConn{rollbacks: d.rollbacks}, nil }

type stubConn struct{ rollbacks *int }

func (c stubConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("no statements in stub")
}
func (c stubConn) Close() error              { return nil }
func (c stubConn) Begin() (driver.Tx, error) { return stubTx{rollbacks: c.rollbacks}, nil }

type stubTx struct{ rollbacks *int }

func (t stubTx) Commit() error { return nil }
func (t stubTx) Rollback() error {
	if t.rollbacks != nil {
		*t.rollbacks++
	}
	return nil
}

// newTxTestClient builds a Client whose writer can open real transactions
// against the stub driver above.
func newTxTestClient(t *testing.T, rollbacks *int) *Client {
	t.Helper()
	name := fmt.Sprintf("stub-%s", t.Name())
	sql.Register(name, stubDriver{rollbacks: rollbacks})
	db, err := sql.Open(name, "")
	if err != nil {
		t.Fatalf("opening stub db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	drv := entsql.OpenDB(dialect.Postgres, db)
	return &Client{
		writerClient: ent.NewClient(ent.Driver(drv)),
		logger:       logger.NewNoopLogger(),
	}
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
