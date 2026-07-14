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
	"github.com/flexprice/flexprice/internal/types"
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
		// Outermost: establishes the per-message context — installs the writer-pin
		// holder (read-your-writes) and starts the db.resolved_target span, rolling
		// the handler's success/failure onto it. Placed before Recoverer so a
		// handler panic is converted to an error underneath it and still closes the
		// span.
		consumerContextMiddleware(tracingSvc),
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

// consumerTraceMiddleware starts a root span for every consumed message and
// attaches it to the message context, so the reader/writer DB router can record
// db.resolved_target on it (handlers otherwise process on a bare context with no
// span, and the tag is silently dropped). The span is named kafka.consume.<handler>
// and its status is set from the handler's returned error.
//
// Any handler that derives its ctx from msg.Context() inherits this span for free.
// Handlers that intentionally detach from the message lifecycle — e.g. onboarding,
// which hands work to a goroutine outliving the message via context.Background() —
// do NOT inherit it and own their own span, because a message-scoped span cannot
// cover work that outlives the message.
//
// Nil/disabled tracing is safe: StartKafkaConsumerSpan returns a nil *Span whose
// methods no-op, and SetContext(ctx) with the unchanged ctx is a no-op.
func consumerContextMiddleware(tracingSvc *tracing.Service) message.HandlerMiddleware {
	return func(h message.HandlerFunc) message.HandlerFunc {
		return func(msg *message.Message) ([]*message.Message, error) {
			// Install the writer-pin holder for this message's unit of work, so a
			// write anywhere in the handler pins later reads to the primary
			// (read-your-writes). Handlers that derive their ctx from msg.Context()
			// inherit it and no longer call WithWriterPinning themselves.
			//
			// Exception: onboarding uses context.Background() (its work outlives the
			// message via a goroutine) so it does NOT inherit this and installs its
			// own pin — see onboarding.processMessage.
			ctx := types.WithWriterPinning(msg.Context())

			// Start a root span on that ctx so the reader/writer DB router can record
			// db.resolved_target on it, and roll the handler's outcome onto the span.
			span, ctx := tracingSvc.StartKafkaConsumerSpan(ctx, message.HandlerNameFromCtx(ctx))
			msg.SetContext(ctx)
			defer span.Finish()

			msgs, err := h(msg)
			if err != nil {
				span.SetStatusError(err)
			} else {
				span.SetStatusOK()
			}
			return msgs, err
		}
	}
}

// AddNoPublishHandler adds a handler that doesn't publish messages.
// topicDLQ overrides the global DLQ topic for this handler; pass "" to use
// the global kafka.topic_dlq fallback.
func (r *Router) AddNoPublishHandler(
	handlerName string,
	topicName string,
	topicDLQ string,
	subscriber message.Subscriber,
	handlerFunc func(ctx context.Context, msg *message.Message) error,
	middlewares ...message.HandlerMiddleware,
) {
	handler := r.router.AddNoPublisherHandler(
		handlerName,
		topicName,
		subscriber,
		func(msg *message.Message) error {
			tenantID := msg.Metadata.Get("tenant_id")
			environmentID := msg.Metadata.Get("environment_id")

			// Detach from msg.Context() cancellation so a consumer-group rebalance
			// or subscriber shutdown doesn't kill an in-flight handler mid-write.
			// WithoutCancel keeps values (tracing span, writer pin, handler name)
			// but strips cancellation, so the 600s below is a real floor, not just
			// a ceiling under whichever cancels first. Safe because unacked messages
			// are redelivered on the next session and handlers are idempotent.
			ctx, cancel := context.WithTimeout(context.WithoutCancel(msg.Context()), 600*time.Second)
			defer cancel()
			ctx = context.WithValue(ctx, types.CtxTenantID, tenantID)
			ctx = context.WithValue(ctx, types.CtxEnvironmentID, environmentID)

			err := handlerFunc(ctx, msg)
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
