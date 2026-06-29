package types

import (
	"context"
	"sync"
	"testing"
)

func TestRoutingStats_InstallAndRetrieve(t *testing.T) {
	ctx := WithRoutingStats(context.Background())
	stats := GetRoutingStats(ctx)
	if stats == nil {
		t.Fatal("expected non-nil RoutingStats after WithRoutingStats")
	}
}

func TestRoutingStats_NilOnBareContext(t *testing.T) {
	if GetRoutingStats(context.Background()) != nil {
		t.Fatal("expected nil RoutingStats on bare context")
	}
}

func TestRoutingStats_VisibleOnDerivedContext(t *testing.T) {
	root := WithRoutingStats(context.Background())
	child := SetTenantID(root, "t1")
	stats := GetRoutingStats(child)
	if stats == nil {
		t.Fatal("expected RoutingStats visible on derived context")
	}
	stats.Reader.Add(3)
	if GetRoutingStats(root).Reader.Load() != 3 {
		t.Fatal("expected counter visible on root context (shared pointer)")
	}
}

func TestRoutingStats_ConcurrentAccess(t *testing.T) {
	ctx := WithRoutingStats(context.Background())
	stats := GetRoutingStats(ctx)
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			stats.WriterPinned.Add(1)
		}()
	}
	wg.Wait()
	if stats.WriterPinned.Load() != 50 {
		t.Fatalf("expected 50, got %d", stats.WriterPinned.Load())
	}
}
