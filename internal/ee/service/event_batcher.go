package service

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/flexprice/flexprice/internal/domain/events"
)

// errBatcherClosed is returned by Enqueue after Close.
var errBatcherClosed = errors.New("event batcher is closed")

// batchItem is one Kafka message's events plus a barrier channel that the
// collector releases with the publish result.
type batchItem struct {
	events []*events.Event
	result chan error
}

// eventBatcher coalesces events from many concurrent Enqueue callers into one
// PublishBatch, releasing every waiter only after the publish returns.
type eventBatcher struct {
	input       chan batchItem
	publish     func(ctx context.Context, evts []*events.Event) error
	flushSize   int
	flushMillis int

	closeOnce sync.Once
	done      chan struct{}
	wg        sync.WaitGroup
}

func newEventBatcher(
	flushSize, flushMillis int,
	publish func(ctx context.Context, evts []*events.Event) error,
) *eventBatcher {
	if flushSize <= 0 {
		flushSize = 500
	}
	if flushMillis <= 0 {
		flushMillis = 200
	}
	b := &eventBatcher{
		input:       make(chan batchItem, 256),
		publish:     publish,
		flushSize:   flushSize,
		flushMillis: flushMillis,
		done:        make(chan struct{}),
	}
	b.wg.Add(1)
	go b.run()
	return b
}

// Enqueue blocks until the events are published (or fail), returning that result.
func (b *eventBatcher) Enqueue(ctx context.Context, evts []*events.Event) error {
	if len(evts) == 0 {
		return nil
	}
	item := batchItem{events: evts, result: make(chan error, 1)}
	select {
	case b.input <- item:
	case <-b.done:
		return errBatcherClosed
	}
	select {
	case err := <-item.result:
		return err
	case <-b.done:
		return errBatcherClosed
	}
}

func (b *eventBatcher) run() {
	defer b.wg.Done()

	ticker := time.NewTicker(time.Duration(b.flushMillis) * time.Millisecond)
	defer ticker.Stop()

	var pending []batchItem
	pendingCount := 0

	flush := func() {
		if len(pending) == 0 {
			return
		}
		all := make([]*events.Event, 0, pendingCount)
		for _, it := range pending {
			all = append(all, it.events...)
		}
		// Detached ctx; PublishBatch groups by each event's tenant/env.
		err := b.publish(context.Background(), all)
		// Ack only after publish: release waiters with the publish result.
		for _, it := range pending {
			it.result <- err
		}
		pending = pending[:0]
		pendingCount = 0
	}

	for {
		select {
		case item := <-b.input:
			pending = append(pending, item)
			pendingCount += len(item.events)
			if pendingCount >= b.flushSize {
				flush()
			}
		case <-ticker.C:
			flush()
		case <-b.done:
			flush()
			// Drain any items that raced past the done check in Enqueue.
			for {
				select {
				case item := <-b.input:
					item.result <- errBatcherClosed
				default:
					return
				}
			}
		}
	}
}

// Close stops the collector, flushes pending items, and makes later Enqueue fail.
func (b *eventBatcher) Close() {
	b.closeOnce.Do(func() {
		close(b.done)
	})
	b.wg.Wait()
}
