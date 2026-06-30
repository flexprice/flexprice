package router

import (
	"context"
	"fmt"
	"time"

	"github.com/ThreeDotsLabs/watermill"
	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/ThreeDotsLabs/watermill/message/router/middleware"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/tracing"
)

// Router manages all message routing
type Router struct {
	router  *message.Router
	logger  *logger.Logger
	tracing *tracing.Service
	config  *config.Webhook
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

	dlqPub, err := NewDLQPublisher(cfg, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create DLQ publisher: %w", err)
	}

	// Middleware order matters: outermost runs first, innermost runs last before handler.
	// DynamicPoisonQueue is outermost so it only fires after Retry has exhausted all attempts.
	// Publisher is captured in the middleware closure — not stored on the Router.
	router.AddMiddleware(
		DynamicPoisonQueue(dlqPub, logger), // FIRST: route to DLQ after retries, or ack silently
		middleware.Recoverer,               // SECOND: recover from panics
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
		router:  router,
		logger:  logger,
		tracing: tracingSvc,
		config:  &cfg.Webhook,
	}, nil
}

// AddNoPublishHandler adds a handler that doesn't publish messages and has no DLQ.
// Messages that permanently fail after all retries are acked silently.
func (r *Router) AddNoPublishHandler(
	handlerName string,
	topicName string,
	subscriber message.Subscriber,
	handlerFunc func(msg *message.Message) error,
	middlewares ...message.HandlerMiddleware,
) {
	r.addHandler(handlerName, topicName, subscriber, handlerFunc, middlewares...)
}

// AddNoPublishHandlerWithDLQ adds a handler that doesn't publish messages but has a
// dedicated DLQ topic. After all retries are exhausted the failed message is published
// to dlqTopic (conventionally {consumer_group_name}_dlq) and then acked.
//
// The DLQ topic is stamped into the message metadata by a thin handler-level middleware
// so the router-level poison middleware can read it without needing a registry.
func (r *Router) AddNoPublishHandlerWithDLQ(
	handlerName string,
	topicName string,
	subscriber message.Subscriber,
	handlerFunc func(msg *message.Message) error,
	dlqTopic string,
	middlewares ...message.HandlerMiddleware,
) {
	// Stamp middleware: encodes the DLQ topic into the message metadata so the
	// router-level poison middleware can route it after all retries are exhausted.
	stampMiddleware := func(h message.HandlerFunc) message.HandlerFunc {
		return func(msg *message.Message) ([]*message.Message, error) {
			msg.Metadata.Set("dlq_topic", dlqTopic)
			return h(msg)
		}
	}

	allMiddlewares := append([]message.HandlerMiddleware{stampMiddleware}, middlewares...)
	r.addHandler(handlerName, topicName, subscriber, handlerFunc, allMiddlewares...)
}

// addHandler is the shared internal registration logic used by both AddNoPublishHandler
// and AddNoPublishHandlerWithDLQ.
func (r *Router) addHandler(
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
