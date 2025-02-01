package pubsub

import (
	"context"

	"github.com/ThreeDotsLabs/watermill/message"
)

// Publisher defines the interface for publishing webhook events
type Publisher interface {
	// Publish publishes a webhook event
	Publish(ctx context.Context, topic string, msg *message.Message) error
	// Close closes the publisher
	Close() error
}

// Subscriber defines the interface for subscribing to webhook events
type Subscriber interface {
	// Subscribe starts consuming webhook events
	Subscribe(ctx context.Context, topic string) (<-chan *message.Message, error)
	// Close closes the subscriber
	Close() error
}

// PubSub combines both Publisher and Subscriber interfaces
type PubSub interface {
	Publisher
	Subscriber
}
