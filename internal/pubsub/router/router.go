package router

import (
	"context"
	"time"

	"github.com/ThreeDotsLabs/watermill"
	watermillKafka "github.com/ThreeDotsLabs/watermill-kafka/v2/pkg/kafka"
	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/ThreeDotsLabs/watermill/message/router/middleware"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/kafka"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/tracing"
)

// Router manages all message routing
type Router struct {
	router       *message.Router
	logger       *logger.Logger
	tracing      *tracing.Service
	config       *config.Webhook
	dlqPublisher message.Publisher
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

	var dlqPublisher message.Publisher

	dlqPublisher, err = createDLQPublisher(cfg, logger)
	if err != nil {
		return nil, err
	}
	logger.Info(context.Background(), "DLQ publisher initialized")

	router.AddMiddleware(
		middleware.Recoverer,
		middleware.CorrelationID,
	)

	return &Router{
		router:       router,
		logger:       logger,
		tracing:      tracingSvc,
		config:       &cfg.Webhook,
		dlqPublisher: dlqPublisher,
	}, nil
}

func createDLQPublisher(cfg *config.Configuration, logger *logger.Logger) (message.Publisher, error) {
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

	logger.Info(context.Background(), "DLQ publisher initialized", "brokers", kc.Brokers, "dlq_topic", kc.TopicDLQ)
	return publisher, nil
}

// AddNoPublishHandler adds a handler that doesn't publish messages.
// topicDLQ overrides the global DLQ topic for this handler; pass "" to use
// the global kafka.topic_dlq fallback.
func (r *Router) AddNoPublishHandler(
	handlerName string,
	topicName string,
	subscriber message.Subscriber,
	handlerFunc func(msg *message.Message) error,
	topicDLQ string,
	middlewares ...message.HandlerMiddleware,
) {
	handler := r.router.AddNoPublisherHandler(
		handlerName,
		topicName,
		subscriber,
		func(msg *message.Message) error {
			err := handlerFunc(msg)
			if err != nil {
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

	// PoisonQueue must be outermost so it catches failures after retries are exhausted
	if r.dlqPublisher != nil && topicDLQ != "" {
		pq, err := middleware.PoisonQueue(r.dlqPublisher, topicDLQ)
		if err != nil {
			r.logger.Error(context.Background(), "failed to create poison queue middleware, DLQ disabled for handler",
				"handler", handlerName,
				"error", err,
			)
		} else {
			handler.AddMiddleware(pq)
		}
	}

	handler.AddMiddleware(middleware.Retry{
		MaxRetries:          3,
		InitialInterval:     1 * time.Second,
		MaxInterval:         10 * time.Second,
		Multiplier:          2.0,
		MaxElapsedTime:      2 * time.Minute,
		RandomizationFactor: 0.5,
		Logger:              watermill.NewStdLogger(true, false),
		OnRetryHook: func(retryNum int, delay time.Duration) {
			r.logger.Info(context.Background(), "retrying message",
				"handler", handlerName,
				"retry_number", retryNum,
				"max_retries", 3,
				"delay", delay,
			)
		},
	}.Middleware)

	for _, mw := range middlewares {
		handler.AddMiddleware(mw)
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
