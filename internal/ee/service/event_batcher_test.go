package service

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/domain/events"
)

func mkEvents(n int) []*events.Event {
	out := make([]*events.Event, n)
	for i := range out {
		out[i] = &events.Event{}
	}
	return out
}

func TestEventBatcher_SizeTriggerReleasesAllWaiters(t *testing.T) {
	var calls int32
	b := newEventBatcher(100, 60_000, func(ctx context.Context, evts []*events.Event) error {
		atomic.AddInt32(&calls, 1)
		return nil
	})
	defer b.Close()

	// 10 callers x 10 events = 100 -> size trigger, before the 60s ticker.
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := b.Enqueue(context.Background(), mkEvents(10)); err != nil {
				t.Errorf("expected nil, got %v", err)
			}
		}()
	}
	wg.Wait()

	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("expected 1 publish call, got %d", got)
	}
}

func TestEventBatcher_TimeTriggerFlushes(t *testing.T) {
	b := newEventBatcher(10_000, 50, func(ctx context.Context, evts []*events.Event) error {
		return nil
	})
	defer b.Close()

	// Well below size threshold; only the ticker can release it.
	if err := b.Enqueue(context.Background(), mkEvents(1)); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

func TestEventBatcher_PublishErrorPropagates(t *testing.T) {
	want := errors.New("boom")
	b := newEventBatcher(2, 60_000, func(ctx context.Context, evts []*events.Event) error {
		return want
	})
	defer b.Close()

	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := b.Enqueue(context.Background(), mkEvents(1)); !errors.Is(err, want) {
				t.Errorf("expected boom, got %v", err)
			}
		}()
	}
	wg.Wait()
}

func TestEventBatcher_CloseReleasesInFlight(t *testing.T) {
	release := make(chan struct{})
	b := newEventBatcher(1, 60_000, func(ctx context.Context, evts []*events.Event) error {
		<-release
		return nil
	})

	done := make(chan error, 1)
	go func() {
		done <- b.Enqueue(context.Background(), mkEvents(1))
	}()

	// Let Enqueue block in publish, then close and unblock.
	time.Sleep(20 * time.Millisecond)
	go func() {
		time.Sleep(10 * time.Millisecond)
		close(release)
		b.Close()
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Enqueue hung across Close")
	}

	// Enqueue after Close must not hang; it returns an error.
	if err := b.Enqueue(context.Background(), mkEvents(1)); !errors.Is(err, errBatcherClosed) {
		t.Fatalf("expected closed error, got %v", err)
	}
}
