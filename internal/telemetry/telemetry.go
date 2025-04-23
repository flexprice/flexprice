package telemetry

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/logger"
	sentryotel "github.com/getsentry/sentry-go/otel"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.21.0"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/fx"
)

// Service handles OpenTelemetry telemetry setup
type Service struct {
	cfg            *config.Configuration
	logger         *logger.Logger
	tracerProvider *sdktrace.TracerProvider
}

// Module provides fx options for Telemetry
func Module() fx.Option {
	return fx.Options(
		fx.Provide(NewTelemetryService),
		fx.Invoke(RegisterHooks),
	)
}

// RegisterHooks registers lifecycle hooks for telemetry
func RegisterHooks(lc fx.Lifecycle, svc *Service) {
	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			if !svc.cfg.Telemetry.Enabled {
				svc.logger.Info("OpenTelemetry is disabled")
				return nil
			}

			// Initialize the tracer provider
			err := svc.setupTracing(ctx)
			if err != nil {
				svc.logger.Errorw("Failed to initialize OpenTelemetry", "error", err)
				return err
			}

			svc.logger.Infow("OpenTelemetry initialized successfully",
				"service", svc.cfg.Telemetry.ServiceName,
				"environment", svc.cfg.Telemetry.Environment,
			)
			return nil
		},
		OnStop: func(ctx context.Context) error {
			if svc.cfg.Telemetry.Enabled && svc.tracerProvider != nil {
				svc.logger.Info("Shutting down OpenTelemetry tracer provider")
				// Shutdown the tracer provider gracefully
				ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
				defer cancel()
				if err := svc.tracerProvider.Shutdown(ctx); err != nil {
					svc.logger.Errorw("Error shutting down tracer provider", "error", err)
					return err
				}
			}
			return nil
		},
	})
}

// NewTelemetryService creates a new telemetry service
func NewTelemetryService(cfg *config.Configuration, logger *logger.Logger) *Service {
	return &Service{
		cfg:    cfg,
		logger: logger,
	}
}

// setupTracing initializes OpenTelemetry tracing
func (s *Service) setupTracing(ctx context.Context) error {
	// Create OTLP exporter
	var exporter *otlptrace.Exporter
	var err error

	if s.cfg.Telemetry.OtelCollectorURL != "" {
		// Use OTLP gRPC exporter to send traces to a collector
		exporter, err = otlptracegrpc.New(ctx,
			otlptracegrpc.WithEndpoint(s.cfg.Telemetry.OtelCollectorURL),
			otlptracegrpc.WithInsecure(), // For development, use WithTLSCredentials in production
		)
	} else {
		// Console exporter if no collector URL is specified
		s.logger.Warn("No OpenTelemetry collector URL specified, using no-op exporter")
		exporter = otlptrace.NewUnstarted(otlptracegrpc.NewClient())
	}

	if err != nil {
		return err
	}

	// Create resource with service information
	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceNameKey.String(s.cfg.Telemetry.ServiceName),
			semconv.ServiceVersionKey.String(s.cfg.Telemetry.ServiceVersion),
			semconv.DeploymentEnvironmentKey.String(s.cfg.Telemetry.Environment),
		),
	)
	if err != nil {
		return err
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSpanProcessor(sentryotel.NewSentrySpanProcessor()),
	)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(sentryotel.NewSentryPropagator())

	// Create and register tracer provider
	s.tracerProvider = sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.AlwaysSample()), // Configure sampling strategy as needed
	)

	// Set global tracer provider
	otel.SetTracerProvider(s.tracerProvider)

	// Set propagators for distributed tracing
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	return nil
}

// Tracer returns a named tracer for instrumentation
func (s *Service) Tracer(name string) trace.Tracer {
	return otel.Tracer(name)
}

// GetTraceProvider returns the trace provider
func (s *Service) GetTraceProvider() *sdktrace.TracerProvider {
	return s.tracerProvider
}
