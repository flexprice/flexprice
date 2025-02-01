package testutil

import (
	"context"
	"sync"

	"github.com/ThreeDotsLabs/watermill/message"
)

// InMemoryPubSub is an in-memory implementation of pubsub.PubSub interface
type InMemoryPubSub struct {
	subscribers map[string][]chan *message.Message
	messages    map[string][]*message.Message
	mu          sync.RWMutex
}

// NewInMemoryPubSub creates a new instance of InMemoryPubSub
func NewInMemoryPubSub() *InMemoryPubSub {
	return &InMemoryPubSub{
		subscribers: make(map[string][]chan *message.Message),
		messages:    make(map[string][]*message.Message),
	}
}

// Publish implements pubsub.Publisher interface
func (ps *InMemoryPubSub) Publish(ctx context.Context, topic string, msg *message.Message) error {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	// Store the message
	ps.messages[topic] = append(ps.messages[topic], msg)

	// Notify all subscribers
	if subscribers, ok := ps.subscribers[topic]; ok {
		for _, ch := range subscribers {
			select {
			case ch <- msg:
			default:
				// If channel is full, we skip the message
				// In real implementation, this would be handled differently
			}
		}
	}

	return nil
}

// Subscribe implements pubsub.Subscriber interface
func (ps *InMemoryPubSub) Subscribe(ctx context.Context, topic string) (<-chan *message.Message, error) {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	ch := make(chan *message.Message, 100) // Buffer size of 100 messages
	ps.subscribers[topic] = append(ps.subscribers[topic], ch)

	// Send all existing messages for this topic
	if messages, ok := ps.messages[topic]; ok {
		go func() {
			for _, msg := range messages {
				select {
				case ch <- msg:
				case <-ctx.Done():
					return
				}
			}
		}()
	}

	return ch, nil
}

// Close implements pubsub.PubSub interface
func (ps *InMemoryPubSub) Close() error {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	// Close all subscriber channels
	for _, subscribers := range ps.subscribers {
		for _, ch := range subscribers {
			close(ch)
		}
	}

	// Clear the maps
	ps.subscribers = make(map[string][]chan *message.Message)
	ps.messages = make(map[string][]*message.Message)

	return nil
}

// GetMessages returns all messages published to a topic
func (ps *InMemoryPubSub) GetMessages(topic string) []*message.Message {
	ps.mu.RLock()
	defer ps.mu.RUnlock()

	if messages, ok := ps.messages[topic]; ok {
		return messages
	}
	return nil
}

// ClearMessages clears all stored messages
func (ps *InMemoryPubSub) ClearMessages() {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	ps.messages = make(map[string][]*message.Message)
}
