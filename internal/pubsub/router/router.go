package router

import (
	"context"
	"time"

	"github.com/ThreeDotsLabs/watermill"
	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/ThreeDotsLabs/watermill/message/router/middleware"
	"github.com/ThreeDotsLabs/watermill/pubsub/gochannel"
	"github.com/flexprice/flexprice/internal/config"
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

	poisonQueue, err := middleware.PoisonQueue(getTempDLQ(), "webhooks_dlq")
	if err != nil {
		return nil, err
	}

	// Add middleware in correct order
	router.AddMiddleware(
		poisonQueue,
		middleware.Recoverer,     // Recover from panics
		middleware.CorrelationID, // Add correlation IDs
		middleware.Retry{
			MaxRetries:          cfg.Webhook.MaxRetries,
			InitialInterval:     cfg.Webhook.InitialInterval,
			MaxInterval:         cfg.Webhook.MaxInterval,
			Multiplier:          cfg.Webhook.Multiplier,
			MaxElapsedTime:      cfg.Webhook.MaxElapsedTime,
			RandomizationFactor: 0.5,
			Logger:              watermill.NewStdLogger(true, false),
			OnRetryHook: func(retryNum int, delay time.Duration) {
				logger.Infow("retrying message",
					"retry_number", retryNum,
					"max_retries", cfg.Webhook.MaxRetries,
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

// getTempDLQ returns a temporary DLQ for testing and not actually used
func getTempDLQ() *gochannel.GoChannel {
	return gochannel.NewGoChannel(
		gochannel.Config{
			Persistent: false,
		},
		watermill.NewStdLogger(true, false),
	)
}
