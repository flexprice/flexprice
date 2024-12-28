package testutil

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/publisher"
)

// InMemoryPublisherService provides an in-memory implementation of publisher.Service for testing
type InMemoryPublisherService struct {
	mu       sync.RWMutex
	events   []*events.Event
	messages map[string][]*message.Message
	subs     []chan *message.Message
	// Add event store for consumer simulation
	eventStore *InMemoryEventStore
}

var _ publisher.EventPublisher = (*InMemoryPublisherService)(nil)

// NewInMemoryEventPublisher creates a new instance of InMemoryPublisherService
func NewInMemoryEventPublisher(eventStore *InMemoryEventStore) publisher.EventPublisher {
	pub := &InMemoryPublisherService{
		events:     make([]*events.Event, 0),
		messages:   make(map[string][]*message.Message),
		subs:       make([]chan *message.Message, 0),
		eventStore: eventStore,
	}

	// Start consumer goroutine
	go pub.startConsumer()

	return pub
}

// startConsumer simulates a Kafka consumer that processes events and stores them
func (p *InMemoryPublisherService) startConsumer() {
	ch := p.Subscribe()
	for msg := range ch {
		var event events.Event
		if err := json.Unmarshal(msg.Payload, &event); err != nil {
			continue
		}

		// Simulate async storage in event store
		if p.eventStore != nil {
			_ = p.eventStore.InsertEvent(context.Background(), &event)
		}
	}
}

// Publish implements publisher.Service interface
func (p *InMemoryPublisherService) Publish(ctx context.Context, event *events.Event) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Store the event in publisher's memory
	p.events = append(p.events, event)

	// Create a message for subscribers
	payload, err := json.Marshal(event)
	if err != nil {
		return err
	}

	msg := message.NewMessage(event.ID, payload)
	topic := "events"

	if p.messages[topic] == nil {
		p.messages[topic] = make([]*message.Message, 0)
	}
	p.messages[topic] = append(p.messages[topic], msg)

	// Notify subscribers
	for _, sub := range p.subs {
		select {
		case sub <- msg:
		default:
			// Skip if channel is full
		}
	}

	return nil
}

// Subscribe returns a channel for receiving messages
func (p *InMemoryPublisherService) Subscribe() chan *message.Message {
	p.mu.Lock()
	defer p.mu.Unlock()

	ch := make(chan *message.Message, 100)
	p.subs = append(p.subs, ch)
	return ch
}

// GetEvents returns all published events
func (p *InMemoryPublisherService) GetEvents() []*events.Event {
	p.mu.RLock()
	defer p.mu.RUnlock()
	events := make([]*events.Event, len(p.events))
	copy(events, p.events)
	return events
}

// HasEvent checks if an event with the given ID exists in publisher's memory
func (p *InMemoryPublisherService) HasEvent(id string) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	for _, evt := range p.events {
		if evt.ID == id {
			return true
		}
	}
	return false
}

// HasMessage checks if a message with the given ID exists in the given topic
func (p *InMemoryPublisherService) HasMessage(topic, id string) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()

	messages, ok := p.messages[topic]
	if !ok {
		return false
	}

	for _, msg := range messages {
		if msg.UUID == id {
			return true
		}
	}
	return false
}

// Clear removes all published events and messages
func (p *InMemoryPublisherService) Clear() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.events = make([]*events.Event, 0)
	p.messages = make(map[string][]*message.Message)

	// Close all subscriber channels
	for _, sub := range p.subs {
		close(sub)
	}
	p.subs = make([]chan *message.Message, 0)
}
