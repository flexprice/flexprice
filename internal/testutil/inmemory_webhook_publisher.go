package testutil

import (
	"context"
	"sync"

	"github.com/flexprice/flexprice/internal/types"
	webhookPublisher "github.com/flexprice/flexprice/internal/webhook/publisher"
)

// InMemoryWebhookPublisher is a test double for webhookPublisher.WebhookPublisher
// that captures published events in memory so tests can assert on them, instead
// of firing them into a pubsub nothing consumes.
type InMemoryWebhookPublisher struct {
	mu     sync.Mutex
	events []*types.WebhookEvent
}

// compile-time check that it satisfies the interface used across the app.
var _ webhookPublisher.WebhookPublisher = (*InMemoryWebhookPublisher)(nil)

// NewInMemoryWebhookPublisher returns a fresh capturing webhook publisher.
func NewInMemoryWebhookPublisher() *InMemoryWebhookPublisher {
	return &InMemoryWebhookPublisher{}
}

func (p *InMemoryWebhookPublisher) PublishWebhook(_ context.Context, event *types.WebhookEvent) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.events = append(p.events, event)
	return nil
}

func (p *InMemoryWebhookPublisher) Close() error { return nil }

// Events returns a copy of the captured webhook events.
func (p *InMemoryWebhookPublisher) Events() []*types.WebhookEvent {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]*types.WebhookEvent, len(p.events))
	copy(out, p.events)
	return out
}

// Reset clears captured events.
func (p *InMemoryWebhookPublisher) Reset() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.events = nil
}
