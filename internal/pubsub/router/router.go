package router

import (
	"context"
	"time"

	"github.com/ThreeDotsLabs/watermill"
	watermillKafka "github.com/ThreeDotsLabs/watermill-kafka/v2/pkg/kafka"
	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/ThreeDotsLabs/watermill/message/router/middleware"
	"github.com/ThreeDotsLabs/watermill/pubsub/gochannel"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/kafka"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/sentry"
)

// Router manages all message routing
type Router struct {
	router *message.Router
	logger *logger.Logger
	sentry *sentry.Service
	config *config.Webhook
}

// NewRouter creates a new message router
func NewRouter(cfg *config.Configuration, logger *logger.Logger, sentry *sentry.Service) (*Router, error) {
	router, err := message.NewRouter(
		message.RouterConfig{},
		watermill.NewStdLogger(true, false),
	)
	if err != nil {
		return nil, err
	}

	// Create publisher for PoisonQueue middleware
	var poisonQueuePublisher message.Publisher
	var dlqTopicName string

	if cfg.Kafka.TopicDLQ != "" {
		// Use real Kafka DLQ when configured
		var err error
		poisonQueuePublisher, err = createDLQPublisher(cfg, logger)
		if err != nil {
			return nil, err
		}
		dlqTopicName = cfg.Kafka.TopicDLQ
		logger.Infow("DLQ enabled with Kafka", "dlq_topic", cfg.Kafka.TopicDLQ)
	} else {
		// Use in-memory DLQ (original behavior) when not configured
		poisonQueuePublisher = getTempDLQ()
		dlqTopicName = "poison_queue"
		logger.Infow("DLQ using in-memory queue (no topic_dlq configured)")
	}

	// PoisonQueue middleware (always present, just with different publisher)
	poisonQueue, err := middleware.PoisonQueue(poisonQueuePublisher, dlqTopicName)
	if err != nil {
		return nil, err
	}

	// Add middleware in correct order
	router.AddMiddleware(
		poisonQueue,          // FIRST: catch permanently failed messages
		middleware.Recoverer, // SECOND: recover from panics
		middleware.CorrelationID,
		middleware.Retry{
			MaxRetries:          3, // Hardcoded as requested
			InitialInterval:     1 * time.Second,
			MaxInterval:         10 * time.Second,
			Multiplier:          2.0,
			MaxElapsedTime:      2 * time.Minute,
			RandomizationFactor: 0.5,
			Logger:              watermill.NewStdLogger(true, false),
			OnRetryHook: func(retryNum int, delay time.Duration) {
				logger.Infow("retrying message",
					"retry_number", retryNum,
					"max_retries", 3,
					"delay", delay,
				)
			},
		}.Middleware,
	)

	return &Router{
		router: router,
		logger: logger,
		sentry: sentry,
		config: &cfg.Webhook,
	}, nil
}

func createDLQPublisher(cfg *config.Configuration, logger *logger.Logger) (message.Publisher, error) {
	// Use the existing Kafka infrastructure
	saramaConfig := kafka.GetSaramaConfig(cfg)
	if saramaConfig != nil {
		saramaConfig.Producer.Return.Successes = true
		saramaConfig.Producer.Return.Errors = true
	}

	publisher, err := watermillKafka.NewPublisher(
		watermillKafka.PublisherConfig{
			Brokers:               cfg.Kafka.Brokers,
			Marshaler:             watermillKafka.DefaultMarshaler{},
			OverwriteSaramaConfig: saramaConfig,
		},
		watermill.NewStdLogger(false, false),
	)
	if err != nil {
		return nil, err
	}

	logger.Infow("DLQ publisher initialized", "brokers", cfg.Kafka.Brokers, "dlq_topic", cfg.Kafka.TopicDLQ)
	return publisher, nil
}

// AddNoPublishHandler adds a handler that doesn't publish messages
func (r *Router) AddNoPublishHandler(
	handlerName string,
	topicName string,
	subscriber message.Subscriber,
	handlerFunc func(msg *message.Message) error,
	middlewares ...message.HandlerMiddleware,
) {
	handler := r.router.AddNoPublisherHandler(
		handlerName,
		topicName,
		subscriber,
		func(msg *message.Message) error {
			err := handlerFunc(msg)
			if err != nil {
				r.sentry.CaptureException(err)
				r.logger.Errorw("handler failed",
					"error", err,
					"correlation_id", middleware.MessageCorrelationID(msg),
					"message_uuid", msg.UUID,
				)
			}
			return err
		},
	)

	for _, middleware := range middlewares {
		handler.AddMiddleware(middleware)
	}
}

// Run starts the router
func (r *Router) Run() error {
	r.logger.Info("starting router")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	return r.router.Run(ctx)
}

// Close gracefully shuts down the router
func (r *Router) Close() error {
	r.logger.Info("closing router")
	return r.router.Close()
}

// getTempDLQ returns a temporary in-memory DLQ (original behavior when topic_dlq not configured)
func getTempDLQ() *gochannel.GoChannel {
	return gochannel.NewGoChannel(
		gochannel.Config{
			Persistent: false,
		},
		watermill.NewStdLogger(true, false),
	)
}
