package testutil

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/flexprice/flexprice/internal/domain/events"
)

type InMemoryKafka struct {
	mu       sync.RWMutex
	messages map[string][]*message.Message
	subs     []chan *message.Message
}

func NewInMemoryKafka() *InMemoryKafka {
	return &InMemoryKafka{
		messages: make(map[string][]*message.Message),
		subs:     make([]chan *message.Message, 0),
	}
}

func (b *InMemoryKafka) Publish(ctx context.Context, event *events.Event) error {
	payload, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal event: %w", err)
	}

	msg := message.NewMessage(event.ID, payload)

	b.mu.Lock()
	defer b.mu.Unlock()

	topic := "events"
	if b.messages[topic] == nil {
		b.messages[topic] = make([]*message.Message, 0)
	}
	b.messages[topic] = append(b.messages[topic], msg)

	// Notify subscribers
	for _, sub := range b.subs {
		select {
		case sub <- msg:
		default:
			// Skip if channel is full
		}
	}

	return nil
}

func (b *InMemoryKafka) Subscribe() chan *message.Message {
	b.mu.Lock()
	defer b.mu.Unlock()

	ch := make(chan *message.Message, 100)
	b.subs = append(b.subs, ch)
	return ch
}

func (b *InMemoryKafka) HasMessage(topic, id string) bool {
	b.mu.RLock()
	defer b.mu.RUnlock()

	messages, ok := b.messages[topic]
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

func (b *InMemoryKafka) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	for _, sub := range b.subs {
		close(sub)
	}
	b.subs = make([]chan *message.Message, 0)
	return nil
}
