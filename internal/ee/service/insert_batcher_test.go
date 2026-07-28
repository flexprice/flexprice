package service

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// The batcher's contract is that a caller does not return until its rows are
// durably written, because Watermill acks (and Kafka commits the offset) on
// return. These tests pin that contract down.

func TestInsertBatcher_FlushesWhenBatchIsFull(t *testing.T) {
	var flushes int32
	var flushed []int

	b := newInsertBatcher(context.Background(), 3, time.Hour, 5*time.Second, func(_ context.Context, items []int) error {
		atomic.AddInt32(&flushes, 1)
		flushed = append(flushed, items...)
		return nil
	})

	var wg sync.WaitGroup
	for i := 1; i <= 3; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			if err := b.Add(context.Background(), []int{n}); err != nil {
				t.Errorf("Add(%d) returned error: %v", n, err)
			}
		}(i)
	}
	wg.Wait()

	// maxDelay is an hour, so the only thing that can have flushed the batch is
	// reaching maxSize.
	if got := atomic.LoadInt32(&flushes); got != 1 {
		t.Fatalf("expected exactly 1 flush, got %d", got)
	}
	if len(flushed) != 3 {
		t.Fatalf("expected 3 rows flushed, got %d", len(flushed))
	}
}

func TestInsertBatcher_FlushesOnMaxDelay(t *testing.T) {
	var flushed []int
	done := make(chan struct{})

	// maxSize is far above what we add, so only the delay timer can flush.
	b := newInsertBatcher(context.Background(), 1000, 50*time.Millisecond, 5*time.Second, func(_ context.Context, items []int) error {
		flushed = append(flushed, items...)
		close(done)
		return nil
	})

	go func() {
		if err := b.Add(context.Background(), []int{42}); err != nil {
			t.Errorf("Add returned error: %v", err)
		}
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("batch was never flushed by the delay timer")
	}

	if len(flushed) != 1 || flushed[0] != 42 {
		t.Fatalf("expected [42] flushed, got %v", flushed)
	}
}

// The no-message-loss guarantee: if the ClickHouse write fails, every caller in
// that batch must see the error so none of them ack. An ack on a failed write
// would commit the offset and lose the rows permanently.
func TestInsertBatcher_FlushErrorReachesEveryCaller(t *testing.T) {
	flushErr := errors.New("clickhouse unavailable")

	b := newInsertBatcher(context.Background(), 3, time.Hour, 5*time.Second, func(_ context.Context, _ []int) error {
		return flushErr
	})

	var wg sync.WaitGroup
	errs := make([]error, 3)
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			errs[idx] = b.Add(context.Background(), []int{idx})
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if !errors.Is(err, flushErr) {
			t.Errorf("caller %d got %v, want %v — a caller that does not see the "+
				"error would ack and lose its messages", i, err, flushErr)
		}
	}
}

// Add must not return before flushFn has completed. If it returned early the
// handler would ack while the write was still in flight, so a crash in that
// window would lose the rows.
func TestInsertBatcher_AddBlocksUntilFlushCompletes(t *testing.T) {
	var writeFinished atomic.Bool

	b := newInsertBatcher(context.Background(), 2, time.Hour, 5*time.Second, func(_ context.Context, _ []int) error {
		time.Sleep(100 * time.Millisecond)
		writeFinished.Store(true)
		return nil
	})

	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			_ = b.Add(context.Background(), []int{n})
			if !writeFinished.Load() {
				t.Error("Add returned before the flush completed — the caller would " +
					"ack an unwritten message")
			}
		}(i)
	}
	wg.Wait()
}

// A stalled write must not park callers forever. Before flushTimeout existed,
// the timer-triggered flush ran under a context stripped of its deadline, so a
// wedged ClickHouse connection held every caller in the batch indefinitely.
func TestInsertBatcher_FlushTimeoutUnblocksCallers(t *testing.T) {
	b := newInsertBatcher(context.Background(), 2, time.Hour, 100*time.Millisecond,
		func(ctx context.Context, _ []int) error {
			// Simulate a write that never completes on its own.
			<-ctx.Done()
			return ctx.Err()
		})

	start := time.Now()
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			errs[idx] = b.Add(context.Background(), []int{idx})
		}(i)
	}
	wg.Wait()
	elapsed := time.Since(start)

	if elapsed > 2*time.Second {
		t.Fatalf("callers blocked for %v — flushTimeout did not bound the write", elapsed)
	}
	for i, err := range errs {
		if err == nil {
			t.Errorf("caller %d got nil error on a timed-out flush; it would ack "+
				"a message that was never written", i)
		}
	}
}

// Rows from different tenants must never share an INSERT: the write runs under
// one tenant-scoped context, and mixing tenants is shared mutable state across
// a tenant boundary.
func TestBatcherGroup_DoesNotMixKeys(t *testing.T) {
	var mu sync.Mutex
	batches := map[string][]int{}

	g := newBatcherGroup(context.Background(), 2, time.Hour, 5*time.Second,
		func(_ context.Context, items []int) error {
			mu.Lock()
			defer mu.Unlock()
			// Every item in a batch is tagged with its tenant via value range:
			// tenant A uses 10-19, tenant B uses 20-29.
			key := "A"
			if items[0] >= 20 {
				key = "B"
			}
			batches[key] = append(batches[key], items...)
			return nil
		})

	var wg sync.WaitGroup
	for _, spec := range []struct {
		key  string
		vals []int
	}{
		{"tenantA:env1", []int{10, 11}},
		{"tenantB:env1", []int{20, 21}},
	} {
		wg.Add(1)
		go func(k string, v []int) {
			defer wg.Done()
			_ = g.Add(context.Background(), k, v)
		}(spec.key, spec.vals)
	}
	wg.Wait()

	mu.Lock()
	defer mu.Unlock()
	for key, items := range batches {
		for _, it := range items {
			inA := it >= 10 && it < 20
			if (key == "A") != inA {
				t.Fatalf("batch for %s contained cross-tenant row %d: %v", key, it, batches)
			}
		}
	}
	if len(batches["A"]) != 2 || len(batches["B"]) != 2 {
		t.Fatalf("expected 2 rows per tenant, got %v", batches)
	}
}

// Only one write may be in flight per batcher. Without serialization a slow
// ClickHouse insert fans out into unbounded concurrent writes, each pinning a
// batch's rows and its blocked callers in memory instead of applying
// backpressure.
func TestInsertBatcher_SerializesFlushes(t *testing.T) {
	var inFlight, maxInFlight int32

	b := newInsertBatcher(context.Background(), 2, time.Hour, 5*time.Second,
		func(_ context.Context, _ []int) error {
			cur := atomic.AddInt32(&inFlight, 1)
			for {
				prev := atomic.LoadInt32(&maxInFlight)
				if cur <= prev || atomic.CompareAndSwapInt32(&maxInFlight, prev, cur) {
					break
				}
			}
			time.Sleep(30 * time.Millisecond)
			atomic.AddInt32(&inFlight, -1)
			return nil
		})

	// 20 callers => 10 full batches, all contending at once.
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			_ = b.Add(context.Background(), []int{n})
		}(i)
	}
	wg.Wait()

	if got := atomic.LoadInt32(&maxInFlight); got > 1 {
		t.Fatalf("observed %d concurrent flushes; writes must be serialized per batcher", got)
	}
}

func TestInsertBatcher_EmptyAddIsNoop(t *testing.T) {
	var flushes int32
	b := newInsertBatcher(context.Background(), 2, 50*time.Millisecond, 5*time.Second, func(_ context.Context, _ []int) error {
		atomic.AddInt32(&flushes, 1)
		return nil
	})

	if err := b.Add(context.Background(), nil); err != nil {
		t.Fatalf("Add(nil) returned error: %v", err)
	}
	time.Sleep(150 * time.Millisecond)

	if got := atomic.LoadInt32(&flushes); got != 0 {
		t.Fatalf("expected no flush for an empty Add, got %d", got)
	}
}

// Rows from separate messages must end up in one INSERT — that coalescing is
// the entire point, since per-message inserts capped a task at ~7-13 msg/s.
func TestInsertBatcher_CoalescesAcrossCallers(t *testing.T) {
	var batchSizes []int
	var mu sync.Mutex

	b := newInsertBatcher(context.Background(), 10, time.Hour, 5*time.Second, func(_ context.Context, items []int) error {
		mu.Lock()
		batchSizes = append(batchSizes, len(items))
		mu.Unlock()
		return nil
	})

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			_ = b.Add(context.Background(), []int{n})
		}(i)
	}
	wg.Wait()

	mu.Lock()
	defer mu.Unlock()
	if len(batchSizes) != 1 {
		t.Fatalf("expected 10 callers to coalesce into 1 flush, got %d flushes: %v",
			len(batchSizes), batchSizes)
	}
	if batchSizes[0] != 10 {
		t.Fatalf("expected a batch of 10 rows, got %d", batchSizes[0])
	}
}
