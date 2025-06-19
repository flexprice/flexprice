package sentry

import (
	"context"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/getsentry/sentry-go"
	"go.uber.org/fx"
)

type Service struct {
	cfg    *config.Configuration
	logger *logger.Logger
}

// Module provides fx options for Sentry
func Module() fx.Option {
	return fx.Options(
		fx.Provide(NewSentryService),
		fx.Invoke(RegisterHooks),
	)
}

// registerHooks registers lifecycle hooks for Sentry
func RegisterHooks(lc fx.Lifecycle, svc *Service) {
	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			if !svc.cfg.Sentry.Enabled {
				svc.logger.Info("Sentry is disabled")
				return nil
			}

			err := sentry.Init(sentry.ClientOptions{
				Dsn:              svc.cfg.Sentry.DSN,
				Environment:      svc.cfg.Sentry.Environment,
				EnableTracing:    true,
				TracesSampleRate: svc.cfg.Sentry.SampleRate,
				BeforeSend: func(event *sentry.Event, hint *sentry.EventHint) *sentry.Event {
					return event
				},
				TracesSampler: sentry.TracesSampler(func(ctx sentry.SamplingContext) float64 {
					if ctx.Span.Name == "GET /health" {
						return 0.0
					}
					return svc.cfg.Sentry.SampleRate
				}),
			})
			if err != nil {
				svc.logger.Errorw("Failed to initialize Sentry", "error", err)
				return err
			}
			svc.logger.Infow("Sentry initialized successfully",
				"environment", svc.cfg.Sentry.Environment,
				"sample_rate", svc.cfg.Sentry.SampleRate,
			)
			return nil
		},
		OnStop: func(ctx context.Context) error {
			if svc.cfg.Sentry.Enabled {
				svc.logger.Info("Flushing Sentry events before shutdown")
				sentry.Flush(2 * time.Second)
			}
			return nil
		},
	})
}

// NewSentryService creates a new Sentry service
func NewSentryService(cfg *config.Configuration, logger *logger.Logger) *Service {
	return &Service{
		cfg:    cfg,
		logger: logger,
	}
}

func (s *Service) IsEnabled() bool {
	return s.cfg.Sentry.Enabled
}

// CaptureException captures an error in Sentry
func (s *Service) CaptureException(err error) {
	if !s.IsEnabled() {
		return
	}
	sentry.CaptureException(err)
}

// AddBreadcrumb adds a breadcrumb to the current scope
func (s *Service) AddBreadcrumb(category, message string, data map[string]interface{}) {
	if !s.IsEnabled() {
		return
	}
	sentry.AddBreadcrumb(&sentry.Breadcrumb{
		Category: category,
		Message:  message,
		Level:    sentry.LevelInfo,
		Data:     data,
	})
}

// Flush waits for queued events to be sent
func (s *Service) Flush(timeout uint) bool {
	if !s.IsEnabled() {
		return true
	}
	return sentry.Flush(time.Duration(timeout) * time.Second)
}

// StartDBSpan starts a new database span in the current transaction
func (s *Service) StartDBSpan(ctx context.Context, operation string, params map[string]interface{}) (*sentry.Span, context.Context) {
	if !s.IsEnabled() {
		return nil, ctx
	}

	span := sentry.StartSpan(ctx, operation)
	if span != nil {
		span.Description = operation
		span.Op = "db.postgres"

		for k, v := range params {
			span.SetData(k, v)
		}
	}

	return span, span.Context()
}

// StartClickHouseSpan starts a new ClickHouse span in the current transaction
func (s *Service) StartClickHouseSpan(ctx context.Context, operation string, params map[string]interface{}) (*sentry.Span, context.Context) {
	if !s.cfg.Sentry.Enabled {
		return nil, ctx
	}

	span := sentry.StartSpan(ctx, operation)
	if span != nil {
		span.Description = operation
		span.Op = "db.clickhouse"

		for k, v := range params {
			span.SetData(k, v)
		}
	}

	return span, span.Context()
}

// StartKafkaConsumerSpan starts a new Kafka consumer span in the current transaction
func (s *Service) StartKafkaConsumerSpan(ctx context.Context, topic string) (*sentry.Span, context.Context) {
	if !s.cfg.Sentry.Enabled {
		return nil, ctx
	}

	span := sentry.StartSpan(ctx, "kafka.consume."+topic)
	if span != nil {
		span.Description = "Consuming message from " + topic
		span.Op = "kafka.consume"
		span.SetData("topic", topic)
	}

	return span, span.Context()
}

// MonitorEventProcessing tracks event processing in Sentry
func (s *Service) MonitorEventProcessing(ctx context.Context, eventName string, eventTimestamp time.Time, metadata map[string]interface{}) (*sentry.Span, context.Context) {
	if !s.cfg.Sentry.Enabled {
		return nil, ctx
	}

	span := sentry.StartSpan(ctx, "event.process")
	if span != nil {
		span.Description = "Processing event"
		span.Op = "event.process"
		span.SetData("event_name", eventName)

		// Calculate lag
		lag := time.Since(eventTimestamp)
		lagMs := lag.Milliseconds()
		span.SetData("lag_ms", lagMs)

		// Set lag as transaction tag for alerting
		tx := sentry.TransactionFromContext(ctx)
		if tx != nil {
			tx.SetTag("event.lag.ms", fmt.Sprintf("%d", lagMs))

			// Set severity tags for easier alerting thresholds
			if lag.Milliseconds() >= 5*time.Minute.Milliseconds() {
				tx.SetTag("event.lag.severity", "critical")
			} else if lag.Milliseconds() >= 1*time.Minute.Milliseconds() {
				tx.SetTag("event.lag.severity", "warning")
			} else {
				tx.SetTag("event.lag.severity", "normal")
			}
		}

		for k, v := range metadata {
			span.SetData(k, v)
		}
	}

	return span, span.Context()
}

// StartTransaction creates a new transaction or returns an existing one from context
func (s *Service) StartTransaction(ctx context.Context, name string, options ...sentry.SpanOption) (*sentry.Span, context.Context) {
	if !s.cfg.Sentry.Enabled {
		return nil, ctx
	}

	hub := sentry.GetHubFromContext(ctx)
	if hub == nil {
		hub = sentry.CurrentHub().Clone()
		ctx = sentry.SetHubOnContext(ctx, hub)
	}

	opts := append([]sentry.SpanOption{
		sentry.WithOpName(name),
		sentry.WithTransactionSource(sentry.SourceCustom),
	}, options...)

	transaction := sentry.StartTransaction(ctx, name, opts...)
	return transaction, transaction.Context()
}

// SpanFinisher is a helper that finishes a span when calling Finish()
type SpanFinisher struct {
	Span *sentry.Span
}

// Finish completes the span if it exists
func (f *SpanFinisher) Finish() {
	if f.Span != nil {
		f.Span.Finish()
	}
}

// StartRepositorySpan creates a span for a repository operation
func (s *Service) StartRepositorySpan(ctx context.Context, repository, operation string, params map[string]interface{}) (*sentry.Span, context.Context) {
	if !s.cfg.Sentry.Enabled {
		return nil, ctx
	}

	operationName := fmt.Sprintf("repository.%s.%s", repository, operation)
	span := sentry.StartSpan(ctx, operationName)
	if span != nil {
		span.Description = operationName
		span.Op = "db.repository"

		// Add common repository data
		span.SetData("repository", repository)
		span.SetData("operation", operation)

		// Add additional parameters
		for k, v := range params {
			span.SetData(k, v)
		}
	}

	return span, span.Context()
}

// StartStripeAPISpan starts a new Stripe API span in the current transaction
func (s *Service) StartStripeAPISpan(ctx context.Context, operation, endpoint string, params map[string]interface{}) (*sentry.Span, context.Context) {
	if !s.cfg.Sentry.Enabled {
		return nil, ctx
	}

	span := sentry.StartSpan(ctx, "stripe.api."+operation)
	if span != nil {
		span.Description = fmt.Sprintf("Stripe API: %s %s", operation, endpoint)
		span.Op = "stripe.api"
		span.SetData("endpoint", endpoint)
		span.SetData("operation", operation)

		for k, v := range params {
			span.SetData(k, v)
		}
	}

	return span, span.Context()
}

// StartStripeWebhookSpan starts a new Stripe webhook span in the current transaction
func (s *Service) StartStripeWebhookSpan(ctx context.Context, eventType string, params map[string]interface{}) (*sentry.Span, context.Context) {
	if !s.cfg.Sentry.Enabled {
		return nil, ctx
	}

	span := sentry.StartSpan(ctx, "stripe.webhook."+eventType)
	if span != nil {
		span.Description = "Processing Stripe webhook: " + eventType
		span.Op = "stripe.webhook"
		span.SetData("event_type", eventType)

		for k, v := range params {
			span.SetData(k, v)
		}
	}

	return span, span.Context()
}

// StartStripeSyncSpan starts a new Stripe sync span in the current transaction
func (s *Service) StartStripeSyncSpan(ctx context.Context, syncType string, params map[string]interface{}) (*sentry.Span, context.Context) {
	if !s.cfg.Sentry.Enabled {
		return nil, ctx
	}

	span := sentry.StartSpan(ctx, "stripe.sync."+syncType)
	if span != nil {
		span.Description = "Stripe sync: " + syncType
		span.Op = "stripe.sync"
		span.SetData("sync_type", syncType)

		for k, v := range params {
			span.SetData(k, v)
		}
	}

	return span, span.Context()
}

// CaptureStripeError captures a Stripe-specific error with additional context
func (s *Service) CaptureStripeError(err error, operation string, context map[string]interface{}) {
	if !s.IsEnabled() {
		return
	}

	sentry.WithScope(func(scope *sentry.Scope) {
		scope.SetTag("component", "stripe_integration")
		scope.SetTag("operation", operation)

		for k, v := range context {
			if contextMap, ok := v.(map[string]interface{}); ok {
				scope.SetContext(k, contextMap)
			} else {
				// Convert single values to a context map
				scope.SetContext(k, map[string]interface{}{"value": v})
			}
		}

		sentry.CaptureException(err)
	})
}

// AddStripeBreadcrumb adds a Stripe-specific breadcrumb
func (s *Service) AddStripeBreadcrumb(operation, message string, data map[string]interface{}) {
	if !s.IsEnabled() {
		return
	}

	breadcrumbData := make(map[string]interface{})
	breadcrumbData["component"] = "stripe_integration"
	breadcrumbData["operation"] = operation

	for k, v := range data {
		breadcrumbData[k] = v
	}

	sentry.AddBreadcrumb(&sentry.Breadcrumb{
		Category: "stripe",
		Message:  message,
		Level:    sentry.LevelInfo,
		Data:     breadcrumbData,
	})
}
