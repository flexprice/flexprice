package postgres

import (
	"context"
	"testing"

	"github.com/flexprice/flexprice/ent"
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
