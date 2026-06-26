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
	"github.com/flexprice/flexprice/internal/tracing"
)

// Router manages all message routing
type Router struct {
	router    *message.Router
	logger    *logger.Logger
	tracing   *tracing.Service
	config    *config.Webhook
	dlqRouter *routingDLQPublisher
}

// NewRouter creates a new message router
func NewRouter(cfg *config.Configuration, logger *logger.Logger, tracingSvc *tracing.Service) (*Router, error) {
	router, err := message.NewRouter(
		message.RouterConfig{},
		watermill.NewStdLogger(true, false),
	)
	if err != nil {
		return nil, err
	}

	// Always create a Kafka publisher for the DLQ. The actual DLQ topic is chosen
	// per-handler at publish time by routingDLQPublisher, so no topic is needed
	// at construction — only the broker config.
	kafkaPub, err := createDLQPublisher(cfg, logger)
	if err != nil {
		return nil, err
	}

	// Legacy shared DLQ fallback for handlers without a per-consumer DLQ topic.
	// When cfg.Kafka.TopicDLQ is set, reuse the Kafka publisher pointed at that
	// single shared topic; otherwise fall back to the ephemeral in-memory queue.
	var fallbackPub message.Publisher
	var fallbackTopic string
	if cfg.Kafka.TopicDLQ != "" {
		fallbackPub = kafkaPub
		fallbackTopic = cfg.Kafka.TopicDLQ
		logger.Info(context.Background(), "DLQ fallback using shared Kafka topic", "fallback_topic", fallbackTopic)
	} else {
		fallbackPub = getTempDLQ()
		fallbackTopic = "poison_queue"
		logger.Info(context.Background(), "DLQ fallback using in-memory queue (no kafka.topic_dlq configured)")
	}

	// routingDLQPublisher reads the poisoned handler name from message metadata
	// and routes to the per-consumer-group DLQ topic registered for it. Handlers
	// without a configured topic fall back to the legacy shared DLQ above.
	dlqRouter := newRoutingDLQPublisher(kafkaPub, fallbackPub, fallbackTopic, logger, tracingSvc)

	// PoisonQueue middleware (unchanged position). The "_dlq_placeholder" topic is
	// required by the constructor but ignored at runtime since routingDLQPublisher
	// always routes by handler name.
	poisonQueue, err := middleware.PoisonQueue(dlqRouter, "_dlq_placeholder")
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
				logger.Info(context.Background(), "retrying message",
					"retry_number", retryNum,
					"max_retries", 3,
					"delay", delay,
				)
			},
		}.Middleware,
	)

	return &Router{
		router:    router,
		logger:    logger,
		tracing:   tracingSvc,
		config:    &cfg.Webhook,
		dlqRouter: dlqRouter,
	}, nil
}

func createDLQPublisher(cfg *config.Configuration, logger *logger.Logger) (message.Publisher, error) {
	// DLQ lives on the deployment's local/consume cluster (same one its consumers read).
	kc := &cfg.Kafka
	saramaConfig := kafka.GetSaramaConfig(kc)
	if saramaConfig != nil {
		saramaConfig.Producer.Return.Successes = true
		saramaConfig.Producer.Return.Errors = true
	}

	publisher, err := watermillKafka.NewPublisher(
		watermillKafka.PublisherConfig{
			Brokers:               kc.Brokers,
			Marshaler:             watermillKafka.DefaultMarshaler{},
			OverwriteSaramaConfig: saramaConfig,
		},
		watermill.NewStdLogger(false, false),
	)
	if err != nil {
		return nil, err
	}

	logger.Info(context.Background(), "DLQ publisher initialized", "brokers", kc.Brokers)
	return publisher, nil
}

// AddNoPublishHandlerWithDLQ is like AddNoPublishHandler but opts the handler into
// per-consumer-group DLQ routing. consumerGroup is used only as a log/trace label;
// dlqTopic comes from config/env and an empty value disables DLQ for this env.
func (r *Router) AddNoPublishHandlerWithDLQ(
	handlerName string,
	topicName string,
	consumerGroup string,
	dlqTopic string,
	subscriber message.Subscriber,
	handlerFunc func(msg *message.Message) error,
	middlewares ...message.HandlerMiddleware,
) {
	r.dlqRouter.Register(handlerName, consumerGroup, dlqTopic)
	r.AddNoPublishHandler(handlerName, topicName, subscriber, handlerFunc, middlewares...)
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
				// No request span on this watermill callback — CaptureException
				// synthesizes a span so the failure still reaches SigNoz.
				r.tracing.CaptureException(context.Background(), err)
				r.logger.Error(context.Background(), "handler failed",
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
	ctx, cancel := context.WithCancel(context.Background())
	r.logger.Info(ctx, "starting router")
	defer cancel()
	return r.router.Run(ctx)
}

// Close gracefully shuts down the router
func (r *Router) Close() error {
	r.logger.Info(context.Background(), "closing router")
	return r.router.Close()
}

// getTempDLQ returns a temporary in-memory DLQ used as the legacy shared-DLQ
// fallback when kafka.topic_dlq is not configured (original behavior).
func getTempDLQ() *gochannel.GoChannel {
	return gochannel.NewGoChannel(
		gochannel.Config{
			Persistent: false,
		},
		watermill.NewStdLogger(true, false),
	)
}
