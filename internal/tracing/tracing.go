// Package tracing provides OpenTelemetry-based distributed tracing for Flexprice.
//
// Tracing is OTel-native: spans are exported via OTLP (gRPC or HTTP) to any
// compatible backend (SigNoz, Grafana Tempo, Datadog, etc.). Sentry is kept
// only for error capture (CaptureException) — it no longer receives traces
// or transactions.
//
// The Service exposes the same span helpers the codebase historically used
// (StartRepositorySpan, StartDBSpan, StartClickHouseSpan, etc.) and returns a
// thin *Span wrapper around the OTel SDK so call sites do not need to change.
package tracing

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/getsentry/sentry-go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/fx"
)

const (
	tracerName = "github.com/flexprice/flexprice"
)

// Service owns the OTel tracer provider and the Sentry SDK (errors only).
type Service struct {
	cfg            *config.Configuration
	logger         *logger.Logger
	tracerProvider *sdktrace.TracerProvider
	tracer         trace.Tracer
	sentryEnabled  bool
	tracingEnabled bool
}

// Module wires the Service into fx and registers OnStart / OnStop hooks.
func Module() fx.Option {
	return fx.Options(
		fx.Provide(NewService),
		fx.Invoke(RegisterHooks),
	)
}

// NewService creates the Service. Initialization of the tracer provider and
// Sentry client happens in RegisterHooks so we don't block fx graph wiring.
func NewService(cfg *config.Configuration, log *logger.Logger) *Service {
	return &Service{
		cfg:    cfg,
		logger: log,
		tracer: otel.Tracer(tracerName),
	}
}

// RegisterHooks attaches lifecycle hooks for tracer + Sentry init/shutdown.
func RegisterHooks(lc fx.Lifecycle, s *Service) {
	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			if err := s.initSentry(); err != nil {
				return err
			}
			if err := s.initTracer(ctx); err != nil {
				return err
			}
			return nil
		},
		OnStop: func(ctx context.Context) error {
			s.shutdown(ctx)
			return nil
		},
	})
}

func (s *Service) initSentry() error {
	if !s.cfg.Sentry.Enabled {
		s.logger.Info("Sentry is disabled")
		return nil
	}

	err := sentry.Init(sentry.ClientOptions{
		Dsn:           s.cfg.Sentry.DSN,
		Environment:   s.cfg.Sentry.Environment,
		EnableTracing: false, // Tracing is handled by OTel; Sentry is errors-only.
	})
	if err != nil {
		s.logger.Errorw("Failed to initialize Sentry", "error", err)
		return err
	}

	s.sentryEnabled = true
	s.logger.Infow("Sentry initialized (errors-only mode)",
		"environment", s.cfg.Sentry.Environment,
	)
	return nil
}

func (s *Service) initTracer(ctx context.Context) error {
	tracesCfg := s.cfg.Otel.Traces
	if !s.cfg.Otel.Enabled || !tracesCfg.Enabled || tracesCfg.Endpoint == "" {
		s.logger.Info("OTel tracing is disabled")
		return nil
	}

	exporter, err := s.newTraceExporter(ctx)
	if err != nil {
		s.logger.Errorw("Failed to initialize OTel trace exporter", "error", err)
		return err
	}

	res, err := s.newResource(ctx)
	if err != nil {
		return err
	}

	sampleRate := tracesCfg.SampleRate
	if sampleRate <= 0 {
		sampleRate = 1.0
	}
	if sampleRate > 1.0 {
		sampleRate = 1.0
	}
	sampler := sdktrace.ParentBased(sdktrace.TraceIDRatioBased(sampleRate))

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sampler),
	)

	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	s.tracerProvider = tp
	s.tracer = tp.Tracer(tracerName)
	s.tracingEnabled = true

	protocol := s.cfg.Otel.ResolveProtocol(tracesCfg.Protocol)
	headers := s.cfg.Otel.ResolveHeaders(tracesCfg.MergedHeaders())
	s.logger.Infow("OTel tracing initialized",
		"endpoint", tracesCfg.Endpoint,
		"protocol", protocol,
		"sample_rate", sampleRate,
		"header_count", len(headers),
	)
	return nil
}

func (s *Service) newTraceExporter(ctx context.Context) (sdktrace.SpanExporter, error) {
	tracesCfg := s.cfg.Otel.Traces
	protocol := s.cfg.Otel.ResolveProtocol(tracesCfg.Protocol)
	headers := s.cfg.Otel.ResolveHeaders(tracesCfg.MergedHeaders())
	endpointIsURL := strings.HasPrefix(tracesCfg.Endpoint, "http://") || strings.HasPrefix(tracesCfg.Endpoint, "https://")

	if protocol == "http" {
		opts := []otlptracehttp.Option{}
		if endpointIsURL {
			// Full URL form: vendor-specific path (e.g. Sentry's OTLP gateway).
			opts = append(opts, otlptracehttp.WithEndpointURL(tracesCfg.Endpoint))
		} else {
			opts = append(opts, otlptracehttp.WithEndpoint(tracesCfg.Endpoint))
		}
		if s.cfg.Otel.Insecure {
			opts = append(opts, otlptracehttp.WithInsecure())
		}
		if len(headers) > 0 {
			opts = append(opts, otlptracehttp.WithHeaders(headers))
		}
		return otlptrace.New(ctx, otlptracehttp.NewClient(opts...))
	}

	opts := []otlptracegrpc.Option{}
	if endpointIsURL {
		opts = append(opts, otlptracegrpc.WithEndpointURL(tracesCfg.Endpoint))
	} else {
		opts = append(opts, otlptracegrpc.WithEndpoint(tracesCfg.Endpoint))
	}
	if s.cfg.Otel.Insecure {
		opts = append(opts, otlptracegrpc.WithInsecure())
	}
	if len(headers) > 0 {
		opts = append(opts, otlptracegrpc.WithHeaders(headers))
	}
	return otlptrace.New(ctx, otlptracegrpc.NewClient(opts...))
}

func (s *Service) newResource(ctx context.Context) (*resource.Resource, error) {
	attrs := []attribute.KeyValue{
		semconv.ServiceName(s.cfg.Otel.ResolveServiceName(s.cfg)),
	}
	if s.cfg.Logging.Environment != "" {
		attrs = append(attrs, semconv.DeploymentEnvironmentName(s.cfg.Logging.Environment))
	}
	if s.cfg.Logging.Region != "" {
		attrs = append(attrs, semconv.CloudRegion(s.cfg.Logging.Region))
	}
	return resource.New(ctx, resource.WithAttributes(attrs...))
}

func (s *Service) shutdown(ctx context.Context) {
	if s.tracerProvider != nil {
		s.logger.Info("Shutting down OTel tracer provider")
		if err := s.tracerProvider.Shutdown(ctx); err != nil {
			s.logger.Warnw("OTel tracer provider shutdown error", "error", err)
		}
	}
	if s.sentryEnabled {
		s.logger.Info("Flushing Sentry events before shutdown")
		sentry.Flush(2 * time.Second)
	}
}

// IsEnabled reports whether any observability backend is active (tracing OR
// Sentry error capture). Kept broad so existing call sites that gate "should
// we do observability work?" continue to behave sensibly.
func (s *Service) IsEnabled() bool {
	return s.tracingEnabled || s.sentryEnabled
}

// IsTracingEnabled reports whether OTel span export is active.
func (s *Service) IsTracingEnabled() bool {
	return s.tracingEnabled
}

// IsSentryEnabled reports whether Sentry error capture is configured.
func (s *Service) IsSentryEnabled() bool {
	return s.sentryEnabled
}

// Tracer returns the underlying OTel tracer (for callers that prefer the raw API).
func (s *Service) Tracer() trace.Tracer {
	return s.tracer
}

// Flush is a no-op for the OTel pipeline (BatchSpanProcessor handles its own
// flushing on shutdown) but ensures Sentry events are delivered.
func (s *Service) Flush(timeout uint) bool {
	if s.sentryEnabled {
		return sentry.Flush(time.Duration(timeout) * time.Second)
	}
	return true
}

// CaptureException reports an error to Sentry Issues.
func (s *Service) CaptureException(err error) {
	if !s.sentryEnabled || err == nil {
		return
	}
	sentry.CaptureException(err)
}

// AddBreadcrumb attaches a Sentry breadcrumb to the current scope.
func (s *Service) AddBreadcrumb(category, message string, data map[string]interface{}) {
	if !s.sentryEnabled {
		return
	}
	sentry.AddBreadcrumb(&sentry.Breadcrumb{
		Category: category,
		Message:  message,
		Level:    sentry.LevelInfo,
		Data:     data,
	})
}

// ---------------------------------------------------------------------------
// Span wrapper — preserves the SetData/SetTag/Finish/Context API the rest of
// the codebase used with sentry-go's *Span.
// ---------------------------------------------------------------------------

// Span wraps an OTel span and exposes the small surface our helpers historically
// relied on. A nil *Span is safe to call methods on (all become no-ops).
type Span struct {
	span trace.Span
	ctx  context.Context
}

// Finish ends the span. Safe to call on nil.
func (s *Span) Finish() {
	if s == nil || s.span == nil {
		return
	}
	s.span.End()
}

// SetData attaches a typed attribute to the span. Mirrors sentry.Span.SetData.
func (s *Span) SetData(key string, value interface{}) {
	if s == nil || s.span == nil {
		return
	}
	s.span.SetAttributes(toAttr(key, value))
}

// SetTag attaches a string attribute (semantically a low-cardinality tag).
func (s *Span) SetTag(key, value string) {
	if s == nil || s.span == nil {
		return
	}
	s.span.SetAttributes(attribute.String(key, value))
}

// SetStatusError marks the span as failed and records the error.
func (s *Span) SetStatusError(err error) {
	if s == nil || s.span == nil || err == nil {
		return
	}
	s.span.RecordError(err)
	s.span.SetStatus(codes.Error, err.Error())
}

// SetStatusOK marks the span as successful.
func (s *Span) SetStatusOK() {
	if s == nil || s.span == nil {
		return
	}
	s.span.SetStatus(codes.Ok, "")
}

// Context returns the context carrying this span.
func (s *Span) Context() context.Context {
	if s == nil {
		return context.Background()
	}
	return s.ctx
}

// SpanFinisher is a defer-friendly wrapper. Calling Finish() on a zero value
// is a no-op, matching the previous sentry.SpanFinisher behaviour.
type SpanFinisher struct {
	Span *Span
}

// Finish ends the wrapped span if present.
func (f *SpanFinisher) Finish() {
	if f == nil {
		return
	}
	f.Span.Finish()
}

// ---------------------------------------------------------------------------
// Span starters — same signatures as the old sentry.Service.
// ---------------------------------------------------------------------------

func (s *Service) startSpan(ctx context.Context, name, op string, params map[string]interface{}) (*Span, context.Context) {
	if !s.tracingEnabled {
		return nil, ctx
	}
	newCtx, sp := s.tracer.Start(ctx, name)
	if op != "" {
		sp.SetAttributes(attribute.String("span.op", op))
	}
	for k, v := range params {
		sp.SetAttributes(toAttr(k, v))
	}
	return &Span{span: sp, ctx: newCtx}, newCtx
}

// StartDBSpan starts a span representing a Postgres operation.
func (s *Service) StartDBSpan(ctx context.Context, operation string, params map[string]interface{}) (*Span, context.Context) {
	return s.startSpan(ctx, operation, "db.postgres", params)
}

// StartClickHouseSpan starts a span representing a ClickHouse operation.
func (s *Service) StartClickHouseSpan(ctx context.Context, operation string, params map[string]interface{}) (*Span, context.Context) {
	return s.startSpan(ctx, operation, "db.clickhouse", params)
}

// StartKafkaConsumerSpan starts a span around a Kafka consume.
func (s *Service) StartKafkaConsumerSpan(ctx context.Context, topic string) (*Span, context.Context) {
	return s.startSpan(ctx, "kafka.consume."+topic, "kafka.consume", map[string]interface{}{
		"topic": topic,
	})
}

// MonitorEventProcessing tracks event processing latency relative to the
// event's source timestamp. Tag thresholds match the previous Sentry behaviour
// so existing alerts continue to work once their backend is repointed.
func (s *Service) MonitorEventProcessing(ctx context.Context, eventName string, eventTimestamp time.Time, metadata map[string]interface{}) (*Span, context.Context) {
	span, newCtx := s.startSpan(ctx, "event.process", "event.process", metadata)
	if span == nil {
		return span, newCtx
	}
	span.SetData("event_name", eventName)

	lag := time.Since(eventTimestamp)
	lagMs := lag.Milliseconds()
	span.SetData("lag_ms", lagMs)

	// Mirror the old Sentry transaction-tag scheme by writing severity onto
	// the active span. With OTel there's no separate "transaction" object —
	// the root span is the transaction.
	if root := rootSpan(newCtx); root != nil {
		root.SetAttributes(attribute.String("event.lag.ms", fmt.Sprintf("%d", lagMs)))
		switch {
		case lag.Milliseconds() >= 5*time.Minute.Milliseconds():
			root.SetAttributes(attribute.String("event.lag.severity", "critical"))
		case lag.Milliseconds() >= 1*time.Minute.Milliseconds():
			root.SetAttributes(attribute.String("event.lag.severity", "warning"))
		default:
			root.SetAttributes(attribute.String("event.lag.severity", "normal"))
		}
	}
	return span, newCtx
}

// StartTransaction starts a new top-level span. In OTel there's no separate
// transaction concept; we just start a span with the SpanKindServer hint.
func (s *Service) StartTransaction(ctx context.Context, name string) (*Span, context.Context) {
	if !s.tracingEnabled {
		return nil, ctx
	}
	newCtx, sp := s.tracer.Start(ctx, name, trace.WithSpanKind(trace.SpanKindServer))
	return &Span{span: sp, ctx: newCtx}, newCtx
}

// StartRepositorySpan starts a span for a repository.<repository>.<operation>.
func (s *Service) StartRepositorySpan(ctx context.Context, repository, operation string, params map[string]interface{}) (*Span, context.Context) {
	name := fmt.Sprintf("repository.%s.%s", repository, operation)
	span, newCtx := s.startSpan(ctx, name, "db.repository", params)
	if span != nil {
		span.SetData("repository", repository)
		span.SetData("operation", operation)
	}
	return span, newCtx
}

// GetSpanFromContext returns the currently active span (wrapped), if any.
func (s *Service) GetSpanFromContext(ctx context.Context) *Span {
	sp := trace.SpanFromContext(ctx)
	if sp == nil || !sp.SpanContext().IsValid() {
		return nil
	}
	return &Span{span: sp, ctx: ctx}
}

// StartMonitoringSpan starts a generic monitoring span (monitoring.<operation>).
func (s *Service) StartMonitoringSpan(ctx context.Context, operation string, params map[string]interface{}) (*Span, context.Context) {
	name := fmt.Sprintf("monitoring.%s", operation)
	return s.startSpan(ctx, name, "monitoring.operation", params)
}

// StartKafkaLagMonitoringSpan tracks Kafka consumer lag metrics with tags so
// downstream alerting can filter by topic / consumer group.
func (s *Service) StartKafkaLagMonitoringSpan(ctx context.Context, operation string, params map[string]interface{}) (*Span, context.Context) {
	name := fmt.Sprintf("monitoring.%s", operation)
	span, newCtx := s.startSpan(ctx, name, "monitoring.kafka.lag", params)
	if span != nil {
		if topic, ok := params["topic"].(string); ok {
			span.SetTag("kafka.topic", topic)
		}
		if cg, ok := params["consumer_group"].(string); ok {
			span.SetTag("kafka.consumer_group", cg)
		}
	}
	return span, newCtx
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func toAttr(key string, v interface{}) attribute.KeyValue {
	switch val := v.(type) {
	case string:
		return attribute.String(key, val)
	case bool:
		return attribute.Bool(key, val)
	case int:
		return attribute.Int(key, val)
	case int32:
		return attribute.Int64(key, int64(val))
	case int64:
		return attribute.Int64(key, val)
	case float32:
		return attribute.Float64(key, float64(val))
	case float64:
		return attribute.Float64(key, val)
	case []string:
		return attribute.StringSlice(key, val)
	case error:
		return attribute.String(key, val.Error())
	default:
		return attribute.String(key, fmt.Sprintf("%v", v))
	}
}

// rootSpan walks back to the outermost span in the current context. OTel does
// not expose this directly, so we fall back to the active span — which is
// already the closest thing to "transaction-level" for our purposes.
func rootSpan(ctx context.Context) trace.Span {
	sp := trace.SpanFromContext(ctx)
	if sp == nil || !sp.SpanContext().IsValid() {
		return nil
	}
	return sp
}
