// Package tracing provides OpenTelemetry-based distributed tracing for Flexprice.
//
// Tracing is OTel-native: spans are exported via OTLP (gRPC or HTTP) to any
// compatible backend (SigNoz, Grafana Tempo, Datadog, etc.). Error and
// exception capture is also OTel-native — CaptureException records an
// "exception" span event (see internal/spanerr) which surfaces in SigNoz's
// Exceptions tab. Sentry init/flush hooks remain behind the (now default-off)
// Sentry config purely for transitional rollback; they are no longer the sink
// for CaptureException and will be removed in a follow-up.
//
// The Service exposes the same span helpers the codebase historically used
// (StartRepositorySpan, StartDBSpan, StartClickHouseSpan, etc.) and returns a
// thin *Span wrapper around the OTel SDK so call sites do not need to change.
package tracing

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/spanerr"
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
		s.logger.Info(context.Background(), "Sentry is disabled")
		return nil
	}

	err := sentry.Init(sentry.ClientOptions{
		Dsn:           s.cfg.Sentry.DSN,
		Environment:   s.cfg.Sentry.Environment,
		EnableTracing: false, // Tracing is handled by OTel; Sentry is errors-only.
	})
	if err != nil {
		s.logger.Error(context.Background(), "Failed to initialize Sentry", "error", err)
		return err
	}

	s.sentryEnabled = true
	s.logger.Info(context.Background(), "Sentry initialized (errors-only mode)",
		"environment", s.cfg.Sentry.Environment,
	)
	return nil
}

func (s *Service) initTracer(ctx context.Context) error {
	tracesCfg := s.cfg.Otel.Traces
	if !s.cfg.Otel.Enabled || !tracesCfg.Enabled || tracesCfg.Endpoint == "" {
		s.logger.Info(context.Background(), "OTel tracing is disabled")
		return nil
	}

	exporter, err := s.newTraceExporter(ctx)
	if err != nil {
		s.logger.Error(ctx, "Failed to initialize OTel trace exporter", "error", err)
		return err
	}

	res, err := s.newResource(ctx)
	if err != nil {
		// resource.ErrPartialResource means some auto-detectors failed (e.g.
		// resource.WithHost failed in a restricted container) but a usable partial
		// resource was still built. Treat this as a non-fatal warning so OTel
		// starts with whatever attributes were collected rather than aborting the
		// entire service startup.
		if !errors.Is(err, resource.ErrPartialResource) {
			return err
		}
		s.logger.Warn(ctx, "OTel resource: partial detection, some attributes may be missing", "error", err)
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
	s.logger.Info(ctx, "OTel tracing initialized",
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

	if strings.HasPrefix(protocol, "http") {
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
		// Gzip-compress the OTLP/HTTP payload. Sentry's OTLP gateway expects
		// compressed protobuf (their reference OpenTelemetry Collector config uses
		// `compression: gzip`); uncompressed proto is accepted with HTTP 200 but
		// silently dropped before it reaches the spans store.
		opts = append(opts, otlptracehttp.WithCompression(otlptracehttp.GzipCompression))
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

	// service.version — set via SERVICE_VERSION env var at deploy time (e.g. git SHA).
	// Enables version-scoped queries and error tracking in SigNoz / Sentry.
	if v := strings.TrimSpace(os.Getenv("SERVICE_VERSION")); v != "" {
		attrs = append(attrs, semconv.ServiceVersion(v))
	}

	// deployment.environment — emit both old and new semconv keys for broad
	// backend compatibility (Sentry relay reads the legacy key).
	env := s.cfg.Logging.Environment
	if env == "" {
		env = s.cfg.Sentry.Environment
	}
	if env != "" {
		attrs = append(attrs,
			semconv.DeploymentEnvironmentName(env),          // deployment.environment.name (OTel v1.22+)
			attribute.String("deployment.environment", env), // legacy key (Sentry, some backends)
		)
	}

	if s.cfg.Logging.Region != "" {
		attrs = append(attrs, semconv.CloudRegion(s.cfg.Logging.Region))
	}

	// app.component identifies which binary this process is (api / consumer /
	// temporal_worker). Visible in SigNoz as a filterable resource attribute.
	if mode := string(s.cfg.Deployment.Mode); mode != "" {
		attrs = append(attrs, attribute.String("app.component", mode))
	}

	return resource.New(ctx,
		resource.WithAttributes(attrs...),
		// Auto-detect host.name (container hostname on ECS), process.pid,
		// process.executable.name, and os.type. These populate the "Infrastructure"
		// section in SigNoz Span Details and enable host-level filtering.
		resource.WithHost(),
		resource.WithProcess(),
		resource.WithOS(),
		// Merge OTEL_RESOURCE_ATTRIBUTES env var (standard OTel SDK mechanism for
		// injecting per-deployment attributes without code changes).
		resource.WithFromEnv(),
	)
}

func (s *Service) shutdown(ctx context.Context) {
	if s.tracerProvider != nil {
		s.logger.Info(ctx, "Shutting down OTel tracer provider")
		if err := s.tracerProvider.Shutdown(ctx); err != nil {
			s.logger.Error(ctx, "OTel tracer provider shutdown error", "error", err)
		}
	}
	if s.sentryEnabled {
		s.logger.Info(ctx, "Flushing Sentry events before shutdown")
		sentry.Flush(2 * time.Second)
	}
}

// IsEnabled reports whether any observability backend is active (tracing OR
// Sentry error capture). Kept broad so existing call sites that gate "should
// we do observability work?" continue to behave sensibly.
func (s *Service) IsEnabled() bool {
	if s == nil {
		return false
	}
	return s.tracingEnabled || s.sentryEnabled
}

// IsTracingEnabled reports whether OTel span export is active.
func (s *Service) IsTracingEnabled() bool {
	if s == nil {
		return false
	}
	return s.tracingEnabled
}

// IsSentryEnabled reports whether Sentry error capture is configured.
func (s *Service) IsSentryEnabled() bool {
	if s == nil {
		return false
	}
	return s.sentryEnabled
}

// IsStorageSpansEnabled reports whether per-query storage spans (DB, cache,
// ClickHouse) should be created. Controlled by
// FLEXPRICE_OTEL_TRACES_STORAGE_SPANS_ENABLED (default: false) to avoid span
// volume explosion before operators have a feel for the cost.
func (s *Service) IsStorageSpansEnabled() bool {
	if s == nil {
		return false
	}
	return s.tracingEnabled && s.cfg.Otel.Traces.StorageSpansEnabled
}

// Tracer returns the underlying OTel tracer (for callers that prefer the raw API).
func (s *Service) Tracer() trace.Tracer {
	return s.tracer
}

// Flush is a no-op for the OTel pipeline (BatchSpanProcessor handles its own
// flushing on shutdown) but ensures Sentry events are delivered.
func (s *Service) Flush(timeout uint) bool {
	if s == nil {
		return true
	}
	if s.sentryEnabled {
		return sentry.Flush(time.Duration(timeout) * time.Second)
	}
	return true
}

// CaptureException records err as an OTel "exception" span event so it surfaces
// in SigNoz's Exceptions tab. If ctx carries a recording span, the event is
// attached to it. Otherwise a short-lived "error.capture" span is synthesized so
// the error is captured even outside any active trace (background goroutines,
// some consumers). Sentry is no longer the sink — see package docs.
func (s *Service) CaptureException(ctx context.Context, err error) {
	if s == nil || err == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}

	// Active span present: record directly onto it (with per-scope dedup).
	if sp := trace.SpanFromContext(ctx); sp.SpanContext().IsValid() && sp.IsRecording() {
		spanerr.Record(ctx, err)
		return
	}

	// No active span. Synthesize one so the exception still reaches SigNoz.
	// Requires tracing to be enabled; otherwise there is nowhere to export it.
	if !s.tracingEnabled {
		return
	}
	_, sp := s.tracer.Start(ctx, "error.capture")
	defer sp.End()
	sp.RecordError(err, trace.WithStackTrace(true))
	sp.SetStatus(codes.Error, err.Error())
}

// AddBreadcrumb attaches a contextual breadcrumb as an OTel span event on the
// active span. Breadcrumbs show up in the Span Details timeline in SigNoz,
// alongside any exception events, giving the same "what led up to this" trail
// Sentry breadcrumbs provided. No-op when ctx has no recording span.
func (s *Service) AddBreadcrumb(ctx context.Context, category, message string, data map[string]interface{}) {
	if s == nil || ctx == nil {
		return
	}
	sp := trace.SpanFromContext(ctx)
	if !sp.SpanContext().IsValid() || !sp.IsRecording() {
		return
	}
	attrs := make([]attribute.KeyValue, 0, len(data)+1)
	attrs = append(attrs, attribute.String("breadcrumb.message", message))
	for k, v := range data {
		attrs = append(attrs, toAttr("breadcrumb.data."+k, v))
	}
	sp.AddEvent("breadcrumb."+category, trace.WithAttributes(attrs...))
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

// SetStatusError marks the span as failed and records the error as an exception
// event with a stacktrace (so it lands in SigNoz's Exceptions tab). Routes
// through spanerr for a stacktrace and per-scope dedup; falls back to the raw
// OTel RecordError if the span isn't reachable via context.
func (s *Span) SetStatusError(err error) {
	if s == nil || s.span == nil || err == nil {
		return
	}
	if s.ctx != nil && spanerr.Record(s.ctx, err) {
		return
	}
	s.span.RecordError(err, trace.WithStackTrace(true))
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
	if s == nil || !s.tracingEnabled {
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

// startStorageSpan starts a SpanKindClient span carrying the OTel `db.system`
// semconv attribute. Both are required for trace backends to classify the span
// as a database call (SigNoz's "Database Calls" tab filters on
// spanKind=Client AND db.system != ''); a plain internal span renders as an
// anonymous child in the waterfall and never reaches that tab.
func (s *Service) startStorageSpan(ctx context.Context, name, op, dbSystem string, params map[string]interface{}) (*Span, context.Context) {
	if s == nil || !s.tracingEnabled {
		return nil, ctx
	}
	newCtx, sp := s.tracer.Start(ctx, name, trace.WithSpanKind(trace.SpanKindClient))
	sp.SetAttributes(
		attribute.String("span.op", op),
		attribute.String("db.system", dbSystem),
	)
	for k, v := range params {
		sp.SetAttributes(toAttr(k, v))
	}
	return &Span{span: sp, ctx: newCtx}, newCtx
}

// StartDBSpan starts a span representing a Postgres operation.
func (s *Service) StartDBSpan(ctx context.Context, operation string, params map[string]interface{}) (*Span, context.Context) {
	return s.startStorageSpan(ctx, operation, "db.postgres", "postgresql", params)
}

// StartClickHouseSpan starts a span representing a ClickHouse operation.
func (s *Service) StartClickHouseSpan(ctx context.Context, operation string, params map[string]interface{}) (*Span, context.Context) {
	return s.startStorageSpan(ctx, operation, "db.clickhouse", "clickhouse", params)
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
		case lag >= 5*time.Minute:
			root.SetAttributes(attribute.String("event.lag.severity", "critical"))
		case lag >= 1*time.Minute:
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
	if s == nil || !s.tracingEnabled {
		return nil, ctx
	}
	// Seed a dedup scope on the transaction so an error that is both logged
	// (auto-capture) and explicitly captured within it yields one exception event.
	newCtx, sp := s.tracer.Start(spanerr.WithDedup(ctx), name, trace.WithSpanKind(trace.SpanKindServer))
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
	if s == nil {
		return nil
	}
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

// rootSpan returns the currently active span from the context. OTel does not
// expose span ancestry, so this is the innermost active span rather than the
// root. For our purposes (tagging lag severity) it is the closest analogue to
// the old Sentry transaction object. Callers should not rely on it being the
// outermost span.
func rootSpan(ctx context.Context) trace.Span {
	sp := trace.SpanFromContext(ctx)
	if sp == nil || !sp.SpanContext().IsValid() {
		return nil
	}
	return sp
}

func (s *Service) StartSvixSpan(ctx context.Context, operation string, params map[string]interface{}) (*Span, context.Context) {
	if s == nil || !s.tracingEnabled {
		return nil, ctx
	}

	operationName := fmt.Sprintf("svix.%s", operation)
	span, newCtx := s.startSpan(ctx, operationName, operation, params)
	if span != nil {
		for k, v := range params {
			span.SetData(k, v)
		}
	}

	return span, newCtx
}
