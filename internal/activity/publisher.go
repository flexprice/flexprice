package activity

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/metrics"
	"github.com/flexprice/flexprice/internal/types"
)

const (
	publishAttempts = 3
	publishBackoff  = 200 * time.Millisecond
)

// Publisher hands committed batches to Kafka from a bounded buffer; it never blocks the caller.
type Publisher struct {
	cfg    config.ActivityConfig
	log    *logger.Logger
	pubsub types.ActivityPubSub

	mu      sync.RWMutex
	closed  bool
	queue   chan Batch
	workers sync.WaitGroup
}

func NewPublisher(cfg *config.Configuration, log *logger.Logger, ps types.ActivityPubSub) *Publisher {
	return &Publisher{cfg: cfg.Activity, log: log, pubsub: ps, queue: make(chan Batch, cfg.Activity.Publisher.BufferSize)}
}

// Publish queues b; a full buffer drops it and counts the entries.
func (p *Publisher) Publish(b Batch) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.closed {
		countDropped(len(b.Entries), "shutdown")
		return
	}
	select {
	case p.queue <- b:
	default:
		countDropped(len(b.Entries), "buffer_full")
	}
}

func (p *Publisher) Start() {
	for i := 0; i < p.cfg.Publisher.Workers; i++ {
		p.workers.Add(1)
		go func() {
			defer p.workers.Done()
			for b := range p.queue {
				p.send(b)
			}
		}()
	}
}

// Stop closes the buffer and waits for workers to drain it, until ctx or the shutdown timeout ends.
func (p *Publisher) Stop(ctx context.Context) error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil
	}
	p.closed = true
	close(p.queue)
	p.mu.Unlock()

	ctx, cancel := context.WithTimeout(ctx, p.cfg.Publisher.ShutdownTimeout)
	defer cancel()
	done := make(chan struct{})
	go func() { p.workers.Wait(); close(done) }()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		for b := range p.queue {
			countDropped(len(b.Entries), "shutdown_timeout")
		}
		return ctx.Err()
	}
}

func (p *Publisher) send(b Batch) {
	pub := p.cfg.Publisher
	for _, m := range split(b, pub.MaxEntriesPerMessage, pub.MaxMessageBytes, pub.MaxSnapshotBytes) {
		if err := p.publishWithRetry(m); err != nil {
			countDropped(len(m.Events), "publish_failed")
			p.log.Error(context.Background(), "failed to publish activity message", "error", err, "message_id", m.MessageID, "tenant_id", m.TenantID)
		}
	}
}

func (p *Publisher) publishWithRetry(m Message) error {
	if p.pubsub.PubSub == nil {
		return errors.New("activity pubsub is not configured")
	}
	payload, err := json.Marshal(m)
	if err != nil {
		return err
	}
	var lastErr error
	for attempt := 0; attempt < publishAttempts; attempt++ {
		if attempt > 0 {
			time.Sleep(publishBackoff * time.Duration(attempt))
		}
		msg := message.NewMessage(m.MessageID, payload)
		msg.Metadata.Set("tenant_id", m.TenantID)
		msg.Metadata.Set("environment_id", m.EnvironmentID)
		msg.Metadata.Set("partition_key", m.MessageID)
		if lastErr = p.pubsub.Publish(context.Background(), p.cfg.Topic, msg); lastErr == nil {
			return nil
		}
	}
	return lastErr
}

func countDropped(n int, reason string) {
	metrics.RecordCounter(context.Background(), metrics.ActivityDropped, int64(n), metrics.L(metrics.KeyReason, reason))
}
