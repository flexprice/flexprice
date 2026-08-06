package types

import (
	"context"
	"sync"
	"testing"
)

func TestWriterPinning_DefaultUnpinned(t *testing.T) {
	ctx := context.Background()

	// Without a pin installed, nothing is pinned and PinWriter is a no-op
	if IsWriterPinned(ctx) {
		t.Fatal("expected unpinned context without pin holder")
	}
	PinWriter(ctx) // must not panic
	if IsWriterPinned(ctx) {
		t.Fatal("PinWriter without a pin holder must be a no-op")
	}
}

func TestWriterPinning_PinVisibleAcrossDerivedContexts(t *testing.T) {
	root := WithWriterPinning(context.Background())

	if IsWriterPinned(root) {
		t.Fatal("freshly installed pin must start unpinned")
	}

	// Derive a child context (as services do with WithValue), pin on the child,
	// and verify the pin is visible on the root and on sibling contexts.
	child := SetTenantID(root, "tenant-1")
	PinWriter(child)

	if !IsWriterPinned(child) {
		t.Fatal("expected child context to be pinned")
	}
	if !IsWriterPinned(root) {
		t.Fatal("expected pin to propagate to root context (shared holder)")
	}
	sibling := SetEnvironmentID(root, "env-1")
	if !IsWriterPinned(sibling) {
		t.Fatal("expected pin to be visible on sibling context")
	}
}

func TestWriterPinning_IdempotentInstall(t *testing.T) {
	root := WithWriterPinning(context.Background())
	// Re-installing on a context that already has a pin must reuse the holder,
	// so a pin set through the re-installed context is visible on the original.
	reinstalled := WithWriterPinning(SetTenantID(root, "tenant-1"))
	PinWriter(reinstalled)
	if !IsWriterPinned(root) {
		t.Fatal("expected re-install to reuse the existing pin holder")
	}
}

func TestWriterPinning_IndependentUnitsOfWork(t *testing.T) {
	a := WithWriterPinning(context.Background())
	b := WithWriterPinning(context.Background())

	PinWriter(a)

	if !IsWriterPinned(a) {
		t.Fatal("expected context a to be pinned")
	}
	if IsWriterPinned(b) {
		t.Fatal("pinning one unit of work must not affect another")
	}
}

func TestWriterPinning_ConcurrentAccess(t *testing.T) {
	ctx := WithWriterPinning(context.Background())

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			PinWriter(ctx)
		}()
		go func() {
			defer wg.Done()
			_ = IsWriterPinned(ctx)
		}()
	}
	wg.Wait()

	if !IsWriterPinned(ctx) {
		t.Fatal("expected context to be pinned after concurrent writes")
	}
}
