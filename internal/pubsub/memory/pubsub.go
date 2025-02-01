package memory

import (
	"context"

	"github.com/ThreeDotsLabs/watermill"
	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/ThreeDotsLabs/watermill/message/router/middleware"
	"github.com/ThreeDotsLabs/watermill/pubsub/gochannel"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/pubsub"
	"github.com/flexprice/flexprice/internal/types"
)

// PubSub implements both Publisher and Subscriber interfaces using watermill's gochannel
type PubSub struct {
	pubsub *gochannel.GoChannel
	config *config.Webhook
	logger *logger.Logger
}

// NewPubSub creates a new memory-based pubsub
func NewPubSub(
	cfg *config.Configuration,
	logger *logger.Logger,
) pubsub.PubSub {
	goChannel := gochannel.NewGoChannel(
		gochannel.Config{
			// Enable persistence to ensure messages aren't lost
			Persistent: true,
			// Block publish until subscriber ack to ensure delivery
			BlockPublishUntilSubscriberAck: false,
			// Buffer size for output channel
			OutputChannelBuffer: 100,
		},
		watermill.NewStdLogger(true, false),
	)

	return &PubSub{
		pubsub: goChannel,
		config: &cfg.Webhook,
		logger: logger,
	}
}

// Publish publishes a webhook event
func (p *PubSub) Publish(ctx context.Context, topic string, msg *message.Message) error {
	msg.SetContext(ctx)
	middleware.SetCorrelationID(types.GetRequestID(ctx), msg)
	return p.pubsub.Publish(topic, msg)
}

// Subscribe starts consuming webhook events
func (p *PubSub) Subscribe(ctx context.Context, topic string) (<-chan *message.Message, error) {
	return p.pubsub.Subscribe(ctx, topic)
}

// Close closes both publisher and subscriber
func (p *PubSub) Close() error {
	// Not necessary since the pubsub is in-memory and uses a singleton instance
	return nil
}
