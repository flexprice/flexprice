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
	"encoding/binary"
	"errors"
	"fmt"
	"math"
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
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
	"go.opentelemetry.io/otel/trace"
	"go.temporal.io/sdk/client"
	temporalotel "go.temporal.io/sdk/contrib/opentelemetry"
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

	// App-level metrics (independent of span export; see initMeter).
	meterProvider  *sdkmetric.MeterProvider
	metricsEnabled bool
	meterInitDone  bool                    // true after a successful enable or deliberate skip (idempotent)
	dbDuration     metric.Float64Histogram // db.client.duration (ms) — {operation, db_system, status}
	cacheRequests  metric.Int64Counter     // cache.requests — {operation, result}
}

// Module wires the Service into fx and registers OnStart / OnStop hooks.
func Module() fx.Option {
	return fx.Options(
		fx.Provide(NewService),
		fx.Invoke(RegisterHooks),
	)
}

// NewService creates the Service. Tracer and Sentry init still happen in
// RegisterHooks. MeterProvider is initialized eagerly here so FX Provide-time
// consumers (Temporal client dial) can attach a MetricsHandler before OnStart.
func NewService(cfg *config.Configuration, log *logger.Logger) *Service {
	s := &Service{
		cfg:    cfg,
		logger: log,
		tracer: otel.Tracer(tracerName),
	}
	if err := s.initMeter(context.Background()); err != nil {
		// Leave metrics unset so RegisterHooks.OnStart can retry and fail boot
		// when metrics are enabled but the exporter cannot be created.
		s.logger.Error(context.Background(), "OTel metrics eager init failed; will retry on start", "error", err)
	}
	return s
}

// RegisterHooks attaches lifecycle hooks for tracer + Sentry init/shutdown.
func RegisterHooks(lc fx.Lifecycle, s *Service) {
	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			if err := s.initTracer(ctx); err != nil {
				return err
			}
			if err := s.initMeter(ctx); err != nil {
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

// baseResourceAttrs returns the service-level resource attributes shared by the
// trace and metric resources (service name/version, environment, region,
// component). No per-host/process attributes — those are added only for traces.
func (s *Service) baseResourceAttrs() []attribute.KeyValue {
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

	// service.instance.id identifies which replica emitted this, on both signals.
	// Metrics need it: without it every replica exports the same label set, so one
	// series takes samples from N writers whose cumulative counters reset
	// independently — duplicates get dropped and a rolling restart reads as a
	// phantom spike. Aggregate it away at query time (sum by (job) (rate(x[5m])) —
	// rate() per series first) or in a collector, not here.
	// Hostname is the pod name on k8s and the container ID on ECS;
	// OTEL_RESOURCE_ATTRIBUTES overrides it.
	if host, err := os.Hostname(); err == nil && host != "" {
		attrs = append(attrs, semconv.ServiceInstanceID(host))
	}
	return attrs
}

func (s *Service) newResource(ctx context.Context) (*resource.Resource, error) {
	return resource.New(ctx,
		resource.WithAttributes(s.baseResourceAttrs()...),
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

// newMetricResource builds a SERVICE-LEVEL resource: no host.name/process attrs,
// which would multiply every series without adding anything service.instance.id
// does not already say.
func (s *Service) newMetricResource(ctx context.Context) (*resource.Resource, error) {
	return resource.New(ctx,
		resource.WithAttributes(s.baseResourceAttrs()...),
		resource.WithFromEnv(),
	)
}

// initMeter wires the OTLP metric pipeline (PeriodicReader → exporter) and
// creates the app-level DB/cache instruments. Independent of tracing: metrics
// stay on even when storage spans are sampled down or off. Idempotent: safe to
// call from NewService (eager) and again from RegisterHooks.OnStart.
func (s *Service) initMeter(ctx context.Context) error {
	if s.meterInitDone {
		return nil
	}

	mc := s.cfg.Otel.Metrics
	if !s.cfg.Otel.Enabled || !mc.Enabled || mc.Endpoint == "" {
		s.logger.Info(ctx, "OTel metrics is disabled")
		s.meterInitDone = true
		return nil
	}

	exporter, err := s.newMetricExporter(ctx)
	if err != nil {
		s.logger.Error(ctx, "Failed to initialize OTel metric exporter", "error", err)
		return err
	}

	res, err := s.newMetricResource(ctx)
	if err != nil {
		if !errors.Is(err, resource.ErrPartialResource) {
			return err
		}
		s.logger.Warn(ctx, "OTel metric resource: partial detection, some attributes may be missing", "error", err)
	}

	interval := time.Duration(mc.IntervalSeconds) * time.Second
	if interval <= 0 {
		interval = 60 * time.Second
	}
	opts := []sdkmetric.Option{
		sdkmetric.WithResource(res),
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(exporter, sdkmetric.WithInterval(interval))),
	}
	opts = append(opts, s.httpMetricViews()...)
	mp := sdkmetric.NewMeterProvider(opts...)
	otel.SetMeterProvider(mp)
	s.meterProvider = mp

	meter := mp.Meter(tracerName)
	if s.dbDuration, err = meter.Float64Histogram("db.client.duration",
		metric.WithUnit("ms"),
		metric.WithDescription("Latency of a DB/cache/ClickHouse repository call")); err != nil {
		return err
	}
	if s.cacheRequests, err = meter.Int64Counter("cache.requests",
		metric.WithDescription("Cache lookups by result (hit/miss)")); err != nil {
		return err
	}

	s.metricsEnabled = true
	s.meterInitDone = true
	s.logger.Info(ctx, "OTel metrics initialized", "endpoint", mc.Endpoint, "interval", interval.String())
	return nil
}

// httpMetricViews decides what happens to the HTTP metrics otelgin emits for free.
// Everything is dropped by default (~31% of ingestion, and SigNoz derives the same
// view from spans). http_server_enabled keeps request.duration for deployments with
// no trace backend, trimmed to bounded attributes — the raw semconv set adds
// server.address/port and protocol version, which cost series and say nothing here.
func (s *Service) httpMetricViews() []sdkmetric.Option {
	drop := func(pattern string) sdkmetric.Option {
		return sdkmetric.WithView(sdkmetric.NewView(
			sdkmetric.Instrument{Name: pattern},
			sdkmetric.Stream{Aggregation: sdkmetric.AggregationDrop{}},
		))
	}

	var views []sdkmetric.Option
	if s.cfg.Otel.Metrics.HTTPServerEnabled {
		// First matching view wins, so this must precede the http.server.* drop.
		views = append(views, sdkmetric.WithView(sdkmetric.NewView(
			sdkmetric.Instrument{Name: "http.server.request.duration"},
			sdkmetric.Stream{AttributeFilter: attribute.NewAllowKeysFilter(
				"http.request.method",
				"http.route",
				"http.response.status_code",
				"error.type",
			)},
		)))
	}
	return append(views, drop("http.server.*"), drop("http.client.*"))
}

// newMetricExporter builds the OTLP metric exporter (gRPC or HTTP), mirroring
// newTraceExporter's endpoint/protocol/header handling.
func (s *Service) newMetricExporter(ctx context.Context) (sdkmetric.Exporter, error) {
	mc := s.cfg.Otel.Metrics
	protocol := s.cfg.Otel.ResolveProtocol(mc.Protocol)
	headers := s.cfg.Otel.ResolveHeaders(mc.MergedHeaders())
	endpointIsURL := strings.HasPrefix(mc.Endpoint, "http://") || strings.HasPrefix(mc.Endpoint, "https://")

	if strings.HasPrefix(protocol, "http") {
		opts := []otlpmetrichttp.Option{}
		if endpointIsURL {
			opts = append(opts, otlpmetrichttp.WithEndpointURL(mc.Endpoint))
		} else {
			opts = append(opts, otlpmetrichttp.WithEndpoint(mc.Endpoint))
		}
		if s.cfg.Otel.Insecure {
			opts = append(opts, otlpmetrichttp.WithInsecure())
		}
		if len(headers) > 0 {
			opts = append(opts, otlpmetrichttp.WithHeaders(headers))
		}
		opts = append(opts, otlpmetrichttp.WithCompression(otlpmetrichttp.GzipCompression))
		return otlpmetrichttp.New(ctx, opts...)
	}

	opts := []otlpmetricgrpc.Option{}
	if endpointIsURL {
		opts = append(opts, otlpmetricgrpc.WithEndpointURL(mc.Endpoint))
	} else {
		opts = append(opts, otlpmetricgrpc.WithEndpoint(mc.Endpoint))
	}
	if s.cfg.Otel.Insecure {
		opts = append(opts, otlpmetricgrpc.WithInsecure())
	}
	if len(headers) > 0 {
		opts = append(opts, otlpmetricgrpc.WithHeaders(headers))
	}
	return otlpmetricgrpc.New(ctx, opts...)
}

func (s *Service) shutdown(ctx context.Context) {
	if s.tracerProvider != nil {
		s.logger.Info(ctx, "Shutting down OTel tracer provider")
		if err := s.tracerProvider.Shutdown(ctx); err != nil {
			s.logger.Error(ctx, "OTel tracer provider shutdown error", "error", err)
		}
	}
	if s.meterProvider != nil {
		s.logger.Info(ctx, "Shutting down OTel meter provider")
		if err := s.meterProvider.Shutdown(ctx); err != nil {
			s.logger.Error(ctx, "OTel meter provider shutdown error", "error", err)
		}
	}
	if s.sentryEnabled {
		s.logger.Info(ctx, "Flushing Sentry events before shutdown")
		sentry.Flush(2 * time.Second)
	}
}

// TemporalMetricsHandler returns a Temporal SDK MetricsHandler backed by this
// process's OTEL MeterProvider when metrics are initialized and
// otel.metrics.temporal_enabled is set. Otherwise returns nil (Temporal dials
// without SDK metrics). This is the only Temporal-metrics surface on Service —
// MeterProvider and contrib options stay private.
func (s *Service) TemporalMetricsHandler() client.MetricsHandler {
	if s == nil || !s.metricsEnabled || s.meterProvider == nil || s.cfg == nil || !s.cfg.Otel.Metrics.TemporalEnabled {
		return nil
	}

	return temporalotel.NewMetricsHandler(temporalotel.MetricsHandlerOptions{
		Meter: s.meterProvider.Meter("temporal-sdk-go"),
		OnError: func(err error) {
			if s.logger == nil {
				return
			}
			s.logger.Error(context.Background(), "temporal otel metrics handler error", "error", err)
		},
	})
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

// IsStorageSpansEnabled is the master switch for ALL per-query storage spans
// (DB, ClickHouse, cache). When false, no storage span emits regardless of
// per-type toggles. Controlled by FLEXPRICE_OTEL_TRACES_STORAGE_SPANS_ENABLED
// (default: false) to avoid span volume explosion before operators have a
// feel for the cost. When true, DB and ClickHouse spans emit; cache spans
// additionally require their per-type flag — see IsRedisCacheSpansEnabled /
// IsInMemoryCacheSpansEnabled.
func (s *Service) IsStorageSpansEnabled() bool {
	if s == nil {
		return false
	}
	return s.tracingEnabled && s.cfg.Otel.Traces.StorageSpansEnabled
}

// IsRedisCacheSpansEnabled reports whether Redis cache spans (db.system=redis)
// should be created. Requires the master IsStorageSpansEnabled to also be true
// — this flag is an opt-in for the noisy cache fan-out on top of the storage
// master switch, not a replacement for it. Controlled by
// FLEXPRICE_OTEL_TRACES_REDIS_CACHE_SPANS_ENABLED (default: false).
func (s *Service) IsRedisCacheSpansEnabled() bool {
	if s == nil {
		return false
	}
	return s.IsStorageSpansEnabled() && s.cfg.Otel.Traces.RedisCacheSpansEnabled
}

// IsInMemoryCacheSpansEnabled reports whether in-memory cache spans
// (db.system=in_memory) should be created. Requires the master
// IsStorageSpansEnabled to also be true — this flag is an opt-in on top of
// the storage master switch. In-memory hits have a completely different
// latency profile from Redis and are gated separately so operators can enable
// only what they need. Controlled by
// FLEXPRICE_OTEL_TRACES_IN_MEMORY_CACHE_SPANS_ENABLED (default: false).
func (s *Service) IsInMemoryCacheSpansEnabled() bool {
	if s == nil {
		return false
	}
	return s.IsStorageSpansEnabled() && s.cfg.Otel.Traces.InMemoryCacheSpansEnabled
}

// storageSpanSampled applies otel.traces.storage_spans_sample_rate as a per-trace
// throttle on storage spans, independent of the global trace sampler — so the
// noisy DB/cache/ClickHouse fan-out can be thinned while HTTP server spans stay
// at 100%. Deterministic on the trace ID (mirrors OTel TraceIDRatioBased), so a
// kept trace retains its whole DB waterfall and, at equal rates, the kept set is
// a subset of the head sampler's. Spans with no trace context are always emitted.
func (s *Service) storageSpanSampled(ctx context.Context) bool {
	if s == nil {
		return false
	}
	rate := s.cfg.Otel.Traces.StorageSpansSampleRate
	if rate >= 1.0 {
		return true
	}
	if rate <= 0 {
		return false
	}
	tid := trace.SpanContextFromContext(ctx).TraceID()
	if !tid.IsValid() {
		return true
	}
	val := binary.BigEndian.Uint64(tid[8:16]) >> 1
	return val < uint64(rate*float64(uint64(1)<<63))
}

// recordStorageMetric emits db.client.duration (+ cache.requests for cache ops)
// for a finished storage span. Labels are bounded (operation, db_system, status)
// — never tenant/pod/query — to keep metric cardinality flat.
func (s *Service) recordStorageMetric(sp *Span) {
	if s.dbDuration == nil {
		return
	}
	status := "ok"
	if sp.hadError {
		status = "error"
	}
	ms := float64(time.Since(sp.metricStart).Microseconds()) / 1000.0
	s.dbDuration.Record(sp.ctx, ms, metric.WithAttributes(
		attribute.String("operation", sp.metricOp),
		attribute.String("db_system", sp.dbSystem),
		attribute.String("status", status),
	))
	if sp.cacheHit != nil && s.cacheRequests != nil {
		result := "miss"
		if *sp.cacheHit {
			result = "hit"
		}
		s.cacheRequests.Add(sp.ctx, 1, metric.WithAttributes(
			attribute.String("operation", sp.metricOp),
			attribute.String("result", result),
		))
	}
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
		// Bound before the multiply so an oversized timeout cannot wrap
		// time.Duration into a negative/invalid value.
		const maxSeconds = uint(math.MaxInt64 / int64(time.Second))
		if timeout > maxSeconds {
			timeout = maxSeconds
		}
		return sentry.Flush(time.Duration(timeout) * time.Second) // #nosec G115 -- bounded above before multiply
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

	// Metric fields — populated when metrics are enabled, recorded on Finish.
	// Present even when span is nil (storage spans sampled out / disabled).
	svc         *Service
	metricStart time.Time
	metricOp    string
	dbSystem    string
	hadError    bool
	cacheHit    *bool
}

// Finish records the storage metric (if metrics are enabled) and ends the span.
// Safe to call on nil.
func (s *Span) Finish() {
	if s == nil {
		return
	}
	if s.svc != nil && !s.metricStart.IsZero() {
		s.svc.recordStorageMetric(s)
	}
	if s.span != nil {
		s.span.End()
	}
}

// SetCacheHit records hit/miss on the span (attribute) and stashes it for the
// cache.requests metric emitted on Finish. No-op on nil.
func (s *Span) SetCacheHit(hit bool) {
	if s == nil {
		return
	}
	s.cacheHit = &hit
	if s.span != nil {
		s.span.SetAttributes(attribute.Bool("cache.hit", hit))
	}
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
	if s == nil || err == nil {
		return
	}
	s.hadError = true // drives the metric status label even when the span is nil
	if s.span == nil {
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
// spanKind=Client AND a non-empty db.system); a plain internal span renders
// as an anonymous child in the waterfall and never reaches that tab.
//
// Gated on otel.traces.storage_spans_enabled (master switch) and throttled by
// storage_spans_sample_rate (per-trace), so every storage span — DB, ClickHouse,
// repository — obeys both regardless of call path.
func (s *Service) startStorageSpan(ctx context.Context, name, op, dbSystem string, params map[string]interface{}) (*Span, context.Context) {
	return s.startStorageSpanGated(ctx, name, op, dbSystem, s.IsStorageSpansEnabled(), params)
}

// startStorageSpanGated is the common implementation. The caller passes an
// explicit spanEnabled gate so DB/CH callers can use the storage_spans_enabled
// switch while cache callers use their per-type flag. The per-trace sample
// rate throttle and the always-on metrics path are shared.
func (s *Service) startStorageSpanGated(ctx context.Context, name, op, dbSystem string, spanEnabled bool, params map[string]interface{}) (*Span, context.Context) {
	if s == nil { // a nil tracing service is a valid no-op (tracing not wired)
		return nil, ctx
	}
	spanOn := spanEnabled && s.storageSpanSampled(ctx)
	if !spanOn && !s.metricsEnabled {
		return nil, ctx
	}
	out := &Span{ctx: ctx}
	newCtx := ctx
	if spanOn {
		var sp trace.Span
		newCtx, sp = s.tracer.Start(ctx, name, trace.WithSpanKind(trace.SpanKindClient))
		sp.SetAttributes(
			attribute.String("span.op", op),
			attribute.String("db.system", dbSystem),
		)
		for k, v := range params {
			sp.SetAttributes(toAttr(k, v))
		}
		out.span = sp
		out.ctx = newCtx
	}
	// Metrics are always-on (independent of span sampling): record on Finish.
	if s.metricsEnabled {
		out.svc = s
		out.metricStart = time.Now()
		out.metricOp = name
		out.dbSystem = dbSystem
	}
	return out, newCtx
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

// StartRepositorySpan starts a span for a repository.<repository>.<operation>
// call. dbSystem identifies the underlying store ("postgresql", "clickhouse")
// so the span carries the OTel db.system attribute and is recognized as a
// database call by trace backends (e.g. SigNoz's Database Calls tab).
//
// Gated by otel.traces.storage_spans_enabled (via startStorageSpan) — this
// fires once per repository method call, so it is subject to the same
// noise/volume tradeoff as StartDBSpan/StartClickHouseSpan.
func (s *Service) StartRepositorySpan(ctx context.Context, dbSystem, repository, operation string, params map[string]interface{}) (*Span, context.Context) {
	name := fmt.Sprintf("repository.%s.%s", repository, operation)
	span, newCtx := s.startStorageSpan(ctx, name, "db.repository", dbSystem, params)
	if span != nil {
		span.SetData("repository", repository)
		span.SetData("operation", operation)
	}
	return span, newCtx
}

// StartCacheSpan starts a span for a cache.<entity>.<operation> call. Uses
// db.system=<dbSystem> so the span lands in the Database Calls tab of trace
// backends alongside the DB / ClickHouse spans; in-memory cache calls share
// the same code path and are tagged the same way (cache.entity distinguishes
// what was accessed).
//
// Gated per db.system on top of the storage master switch:
//   - otel.traces.storage_spans_enabled must be true for ANY cache span to
//     emit (master kill switch shared with DB/ClickHouse spans).
//   - db.system=redis     additionally requires otel.traces.redis_cache_spans_enabled.
//   - db.system=in_memory additionally requires otel.traces.in_memory_cache_spans_enabled.
//
// Both per-type flags default false because cache calls fire on every
// get/set/delete and are the noisiest fan-out in a trace. Splitting the flag
// lets operators enable DB/ClickHouse spans without dragging in cache noise,
// while still using storage_spans_enabled as a single kill switch when they
// want everything off.
func (s *Service) StartCacheSpan(ctx context.Context, dbSystem, cacheEntity, operation string, params map[string]interface{}) (*Span, context.Context) {
	var spanEnabled bool
	switch dbSystem {
	case "redis":
		spanEnabled = s.IsRedisCacheSpansEnabled()
	case "in_memory":
		spanEnabled = s.IsInMemoryCacheSpansEnabled()
	default:
		spanEnabled = s.IsStorageSpansEnabled()
	}
	name := fmt.Sprintf("cache.%s.%s", cacheEntity, operation)
	span, newCtx := s.startStorageSpanGated(ctx, name, "cache."+operation, dbSystem, spanEnabled, params)
	if span != nil {
		span.SetData("cache.entity", cacheEntity)
		span.SetData("cache.operation", operation)
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
