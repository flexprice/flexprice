package service

import (
	"context"
	"sync"
	"time"
)

// insertBatcher coalesces per-message ClickHouse inserts into one round-trip.
//
// Why this exists: each Kafka message used to issue its own INSERT, so a task's
// throughput was capped at 1/round-trip-latency (~7-13 msg/s measured in prod
// against ClickHouse Cloud over PrivateLink). Consumption sat at ~450 msg/s
// against ~2,075 msg/s of production, and no realistic task count closed that
// gap because the cost is per-message, not per-task. Batching turns N
// round-trips into one, so the same task handles N messages for the price of
// one network hop.
//
// AT-LEAST-ONCE SAFETY — the important part:
//
// Watermill acks a Kafka message when its handler returns nil, and the offset
// is committed from there. So the handler MUST NOT return before the rows are
// durably in ClickHouse; otherwise a task crash between ack and write loses
// data permanently.
//
// Add() therefore blocks its caller until the batch that contains its rows has
// been flushed, and returns that flush's error to every caller in the batch:
//
//   - flush succeeds -> every caller gets nil -> Watermill acks -> offset commits
//   - flush fails    -> every caller gets the error -> no ack -> Kafka redelivers
//   - task crashes   -> no handler returned -> no offset committed -> redelivery
//
// Duplicates remain possible (a crash after the ClickHouse write but before the
// offset commit replays those messages). That was already true of the
// per-message path, and meter_usage carries unique_hash for dedup downstream.
type insertBatcher[T any] struct {
	maxSize  int
	maxDelay time.Duration
	flushFn  func(context.Context, []T) error

	mu      sync.Mutex
	pending []T
	// waiters are the callers blocked on the current batch. Each gets the
	// flush result exactly once.
	waiters []chan error
	// timer fires maxDelay after the first item lands in an empty batch, so a
	// partially-filled batch still drains under low traffic instead of sitting
	// until maxSize is reached.
	timer *time.Timer
}

func newInsertBatcher[T any](maxSize int, maxDelay time.Duration, flushFn func(context.Context, []T) error) *insertBatcher[T] {
	return &insertBatcher[T]{
		maxSize:  maxSize,
		maxDelay: maxDelay,
		flushFn:  flushFn,
	}
}

// Add enqueues items and blocks until they have been flushed, returning the
// flush error (nil on success). Callers must propagate a non-nil return to
// Watermill so the message is redelivered rather than acked.
//
// ctx governs the caller's own wait. The flush itself runs on the goroutine
// that trips the size/delay threshold, using that goroutine's context.
func (b *insertBatcher[T]) Add(ctx context.Context, items []T) error {
	if len(items) == 0 {
		return nil
	}

	done := make(chan error, 1)

	b.mu.Lock()
	b.pending = append(b.pending, items...)
	b.waiters = append(b.waiters, done)

	// First item into an empty batch starts the delay clock.
	if b.timer == nil {
		b.timer = time.AfterFunc(b.maxDelay, func() {
			b.flushLocked(context.WithoutCancel(ctx))
		})
	}

	full := len(b.pending) >= b.maxSize
	b.mu.Unlock()

	if full {
		b.flushLocked(ctx)
	}

	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		// The batch may still flush; we just stop waiting. Returning an error
		// means no ack, so the message is redelivered — safe, possibly duplicate.
		return ctx.Err()
	}
}

// flushLocked takes the current batch and writes it. Named "locked" for the
// swap it performs under b.mu; the actual flush runs unlocked so other
// goroutines can keep filling the next batch while this one is in flight.
func (b *insertBatcher[T]) flushLocked(ctx context.Context) {
	b.mu.Lock()
	if len(b.pending) == 0 {
		b.mu.Unlock()
		return
	}

	batch := b.pending
	waiters := b.waiters
	b.pending = nil
	b.waiters = nil
	if b.timer != nil {
		b.timer.Stop()
		b.timer = nil
	}
	b.mu.Unlock()

	err := b.flushFn(ctx, batch)

	// Every caller in this batch gets the same verdict. Buffered channels, so
	// no send blocks even if a caller already gave up on ctx.Done().
	for _, w := range waiters {
		w <- err
	}
}
