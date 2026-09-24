package service

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/config"
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
	}, nil)
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
	}, nil)
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
	}, nil)
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
	}, nil)

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

// Defect 3: a panicking publish must release waiters with an error and the
// collector must survive to serve a subsequent healthy flush.
func TestEventBatcher_PublishPanicRecoversAndSurvives(t *testing.T) {
	var boom int32 = 1
	b := newEventBatcher(1, 60_000, func(ctx context.Context, evts []*events.Event) error {
		if atomic.LoadInt32(&boom) == 1 {
			panic("kaboom")
		}
		return nil
	}, nil)
	defer b.Close()

	done := make(chan error, 1)
	go func() { done <- b.Enqueue(context.Background(), mkEvents(1)) }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected error from panicking publish, got nil")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Enqueue hung on publish panic")
	}

	// Collector must still serve the next batch with a healthy publish.
	atomic.StoreInt32(&boom, 0)
	done2 := make(chan error, 1)
	go func() { done2 <- b.Enqueue(context.Background(), mkEvents(1)) }()
	select {
	case err := <-done2:
		if err != nil {
			t.Fatalf("expected nil after recovery, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("collector did not survive panic")
	}
}

// Defect 5: a ctx cancelled while Enqueue is blocked must return ctx.Err()
// promptly and never report success (which would ack an unpublished event).
func TestEventBatcher_EnqueueRespectsCtxCancel(t *testing.T) {
	block := make(chan struct{})
	b := newEventBatcher(1, 60_000, func(ctx context.Context, evts []*events.Event) error {
		<-block
		return nil
	}, nil)
	defer func() { close(block); b.Close() }()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- b.Enqueue(ctx, mkEvents(1)) }()

	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context.Canceled, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Enqueue ignored ctx cancellation")
	}
}

// Ordering: a waiter must be released only AFTER publish returns.
func TestEventBatcher_WaiterReleasedAfterPublish(t *testing.T) {
	var publishDone int32
	b := newEventBatcher(1, 60_000, func(ctx context.Context, evts []*events.Event) error {
		time.Sleep(30 * time.Millisecond)
		atomic.StoreInt32(&publishDone, 1)
		return nil
	}, nil)
	defer b.Close()

	if err := b.Enqueue(context.Background(), mkEvents(1)); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
	if atomic.LoadInt32(&publishDone) != 1 {
		t.Fatal("waiter released before publish completed")
	}
}

// Defect 2: the fixed shutdown sequence (CloseBatcher before router.Close)
// must drain an in-flight item and complete without hanging.
func TestEventBatcher_ShutdownSequenceNoDeadlock(t *testing.T) {
	published := make(chan struct{}, 1)
	b := newEventBatcher(1, 60_000, func(ctx context.Context, evts []*events.Event) error {
		published <- struct{}{}
		return nil
	}, nil)

	// Fake in-flight handler blocked in Enqueue (router.Close would wait on it).
	handlerDone := make(chan error, 1)
	go func() { handlerDone <- b.Enqueue(context.Background(), mkEvents(1)) }()

	// Fixed order: drain batcher first, then the router stops. Emulate the
	// router.Close() wait as a join on the in-flight handler.
	shutdown := make(chan struct{})
	go func() {
		b.Close()      // CloseBatcher: flushes + releases waiters
		<-handlerDone  // router.Close() waits on in-flight handler
		close(shutdown)
	}()

	select {
	case <-shutdown:
	case <-time.After(2 * time.Second):
		t.Fatal("shutdown sequence deadlocked")
	}

	select {
	case <-published:
	default:
		t.Fatal("in-flight item was dropped, not flushed")
	}
}

// Defect 1: the billing event id must be identical across calls on the same
// source event and deterministically derived from the source id.
func TestExpandWithBillingEvent_DeterministicID(t *testing.T) {
	s := &eventConsumptionService{
		ServiceParams: ServiceParams{
			Config: &config.Configuration{
				Billing: config.BillingConfig{
					TenantID:      "billing_tenant",
					EnvironmentID: "billing_env",
				},
			},
		},
	}

	src := &events.Event{ID: "event_abc123", TenantID: "t1", EventName: "api_call", Timestamp: time.Unix(1700000000, 0).UTC()}

	first := s.expandWithBillingEvent(src)
	second := s.expandWithBillingEvent(src)

	if len(first) != 2 || len(second) != 2 {
		t.Fatalf("expected 2 events each, got %d/%d", len(first), len(second))
	}
	if first[1].ID != second[1].ID {
		t.Fatalf("billing id not stable: %q vs %q", first[1].ID, second[1].ID)
	}
	if first[1].ID != "tenant_event_"+src.ID {
		t.Fatalf("billing id not deterministic from source: %q", first[1].ID)
	}
	// Dedup key is (tenant, env, timestamp, id): timestamp must also be stable.
	if !first[1].Timestamp.Equal(src.Timestamp) {
		t.Fatalf("billing timestamp not from source: got %v want %v", first[1].Timestamp, src.Timestamp)
	}
	if !first[1].Timestamp.Equal(second[1].Timestamp) {
		t.Fatalf("billing timestamp not stable across redelivery: %v vs %v", first[1].Timestamp, second[1].Timestamp)
	}
}
