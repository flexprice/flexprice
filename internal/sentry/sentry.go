package sentry

import (
	"context"
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
		fx.Invoke(registerHooks),
	)
}

// registerHooks registers lifecycle hooks for Sentry
func registerHooks(lc fx.Lifecycle, svc *Service) {
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

// CaptureException captures an error in Sentry
func (s *Service) CaptureException(err error) {
	if !s.cfg.Sentry.Enabled {
		return
	}
	sentry.CaptureException(err)
}

// AddBreadcrumb adds a breadcrumb to the current scope
func (s *Service) AddBreadcrumb(category, message string, data map[string]interface{}) {
	if !s.cfg.Sentry.Enabled {
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
	if !s.cfg.Sentry.Enabled {
		return true
	}
	return sentry.Flush(time.Duration(timeout) * time.Second)
}
