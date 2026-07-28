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

	// baseCtx is the context every flush derives from. It is independent of any
	// caller so one caller's cancellation cannot abort a write that other
	// callers in the same batch are blocked on.
	baseCtx context.Context
	// flushTimeout is the hard ceiling on a single flush. Without it a stalled
	// ClickHouse write would hold every caller in the batch indefinitely; with
	// it the batch fails, nothing is acked, and Kafka redelivers.
	flushTimeout time.Duration

	mu      sync.Mutex
	pending []T
	// waiters are the callers blocked on the current batch. Each gets the
	// flush result exactly once.
	waiters []chan error
	// timer fires maxDelay after the first item lands in an empty batch, so a
	// partially-filled batch still drains under low traffic instead of sitting
	// until maxSize is reached.
	timer *time.Timer
	// flushing serializes writes: only one flush may be in flight per batcher.
	// Without it, a second full batch would start its own INSERT while the first
	// is still running, so slow ClickHouse writes would fan out into unbounded
	// concurrent inserts — each pinning a batch's rows and its blocked callers in
	// memory. Serializing means a slow write applies backpressure instead.
	flushing bool
}

func newInsertBatcher[T any](
	baseCtx context.Context,
	maxSize int,
	maxDelay time.Duration,
	flushTimeout time.Duration,
	flushFn func(context.Context, []T) error,
) *insertBatcher[T] {
	if baseCtx == nil {
		baseCtx = context.Background()
	}
	if flushTimeout <= 0 {
		flushTimeout = defaultInsertFlushTimeout
	}
	return &insertBatcher[T]{
		maxSize:      maxSize,
		maxDelay:     maxDelay,
		flushFn:      flushFn,
		baseCtx:      baseCtx,
		flushTimeout: flushTimeout,
	}
}

// defaultInsertFlushTimeout bounds a single batch write. Generous enough for a
// large INSERT over PrivateLink, short enough that a wedged connection fails
// the batch rather than parking every caller in it.
const defaultInsertFlushTimeout = 30 * time.Second

// Add enqueues items and blocks until they have been flushed, returning the
// flush error (nil on success). Callers must propagate a non-nil return to
// Watermill so the message is redelivered rather than acked.
//
// ctx governs only this caller's wait. It is deliberately NOT used for the
// flush: a batch holds rows contributed by many concurrent callers, so running
// the insert under whichever caller happened to trip the threshold would let
// one caller's cancellation abort a write that other callers are waiting on.
// The flush instead runs under a context derived from the batcher's base
// context with flushTimeout as a hard ceiling, so a stalled ClickHouse write
// fails the batch instead of blocking every caller indefinitely.
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
		b.timer = time.AfterFunc(b.maxDelay, b.flush)
	}

	full := len(b.pending) >= b.maxSize
	b.mu.Unlock()

	if full {
		b.flush()
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

// flush drains pending batches one at a time. Only one write is ever in flight
// per batcher: a caller that arrives while a flush is running returns
// immediately, and the in-flight drain loop picks up whatever accumulated. That
// keeps a slow ClickHouse write applying backpressure — batches queue behind it
// — instead of fanning out into concurrent inserts that each pin a batch's rows
// and its blocked callers in memory.
func (b *insertBatcher[T]) flush() {
	b.mu.Lock()
	if b.flushing {
		// Another goroutine owns the drain loop; it will pick up our rows.
		b.mu.Unlock()
		return
	}
	b.flushing = true

	for {
		if len(b.pending) == 0 {
			b.flushing = false
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

		err := b.writeBatch(batch)

		// Every caller in this batch gets the same verdict. Buffered channels, so
		// no send blocks even if a caller already gave up on ctx.Done().
		for _, w := range waiters {
			w <- err
		}

		b.mu.Lock()
	}
}

// writeBatch performs one bounded write.
func (b *insertBatcher[T]) writeBatch(batch []T) error {
	ctx, cancel := context.WithTimeout(b.baseCtx, b.flushTimeout)
	defer cancel()
	return b.flushFn(ctx, batch)
}

// batcherGroup keeps one insertBatcher per key so rows from different keys are
// never mixed into the same INSERT.
//
// Why this matters: a single process-wide batcher would coalesce rows from all
// tenants and environments into one write executed under one arbitrary
// caller's context — shared mutable state across tenant boundaries, and a
// tenant-scoped context applied to another tenant's rows. Keying by
// (tenant, environment) keeps each batch within a single tenant's scope.
type batcherGroup[T any] struct {
	mu       sync.Mutex
	batchers map[string]*insertBatcher[T]

	baseCtx      context.Context
	maxSize      int
	maxDelay     time.Duration
	flushTimeout time.Duration
	flushFn      func(context.Context, []T) error
}

func newBatcherGroup[T any](
	baseCtx context.Context,
	maxSize int,
	maxDelay time.Duration,
	flushTimeout time.Duration,
	flushFn func(context.Context, []T) error,
) *batcherGroup[T] {
	if baseCtx == nil {
		baseCtx = context.Background()
	}
	return &batcherGroup[T]{
		batchers:     make(map[string]*insertBatcher[T]),
		baseCtx:      baseCtx,
		maxSize:      maxSize,
		maxDelay:     maxDelay,
		flushTimeout: flushTimeout,
		flushFn:      flushFn,
	}
}

// Add routes items to the batcher for key, creating it on first use, and
// blocks until that batch has been flushed.
func (g *batcherGroup[T]) Add(ctx context.Context, key string, items []T) error {
	if len(items) == 0 {
		return nil
	}

	g.mu.Lock()
	b, ok := g.batchers[key]
	if !ok {
		b = newInsertBatcher(g.baseCtx, g.maxSize, g.maxDelay, g.flushTimeout, g.flushFn)
		g.batchers[key] = b
	}
	g.mu.Unlock()

	return b.Add(ctx, items)
}
