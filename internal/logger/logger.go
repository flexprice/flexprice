package logger

import (
	"context"
	"fmt"
	"strings"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/types"
	"go.opentelemetry.io/contrib/bridges/otelzap"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	otellog "go.opentelemetry.io/otel/log"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// Logger wraps zap.SugaredLogger to provide logging functionality.
//
// Outputs:
//   - stdout (zap JSON encoder)
//   - OTLP logs (optional, via OtelEnabled config; goes to SigNoz / any OTLP backend)
//
// Trace-log correlation: call WithContext(ctx) / Ctx(ctx) to bind a request
// context. This injects trace_id and span_id as string fields (visible in stdout
// and OTLP attributes) AND sets the OTLP log record-level TraceId/SpanId fields
// via the otelzap bridge so SigNoz can link logs to their trace in the Span
// Details "Logs" tab. Error capture into Sentry is NOT done here —
// callers use tracing.Service.CaptureException explicitly.
type Logger struct {
	*zap.SugaredLogger
	otelLogProvider *sdklog.LoggerProvider
	// ctxBound is true once WithContext has wrapped the core with otelCtxCore.
	// Guards against repeated WithContext calls accumulating nested wrappers,
	// which would append multiple _span_ctx fields on every Write.
	ctxBound bool
}

// ---------------------------------------------------------------------------
// Trace-log correlation helpers
// ---------------------------------------------------------------------------

// otelCtxCore wraps a zapcore.Core and injects the request context into every
// Write call. The otelzap bridge detects context.Context values in fields and
// extracts the active span's TraceId/SpanId into the OTLP log record's
// record-level fields (not attributes). Without this, SigNoz cannot auto-link
// logs to their trace in the Span Details "Logs" tab.
//
// The context is wrapped in noopJSONContext so the stdout JSON encoder emits
// "_span_ctx":null rather than attempting to reflect-encode the context.
// The otelzap bridge removes the field from OTLP attributes after extraction,
// so OTLP log records are clean.
type otelCtxCore struct {
	zapcore.Core
	ctx context.Context
}

func (c *otelCtxCore) Enabled(level zapcore.Level) bool {
	return c.Core.Enabled(level)
}

func (c *otelCtxCore) With(fields []zapcore.Field) zapcore.Core {
	return &otelCtxCore{Core: c.Core.With(fields), ctx: c.ctx}
}

func (c *otelCtxCore) Write(entry zapcore.Entry, fields []zapcore.Field) error {
	// Append the span context field last so otelzap can extract TraceId/SpanId.
	// noopJSONContext satisfies context.Context (detected by otelzap) and
	// marshals to null so stdout JSON output is not polluted with raw Go context values.
	return c.Core.Write(entry, append(fields, zap.Any("_span_ctx", noopJSONContext{c.ctx})))
}

// Check adds otelCtxCore itself to the CheckedEntry instead of delegating to the
// inner core directly. This is critical: if we delegate (c.Core.Check), the inner
// tee adds its own constituent cores (jsonCore, otelTeeCore) to the CE. Their Write
// methods are then called WITHOUT going through otelCtxCore.Write — so _span_ctx
// is never injected and the OTLP log record never gets TraceId/SpanId set.
// By adding ourselves (ce.AddCore(entry, c)), ce.Write calls our Write, which
// appends _span_ctx and then calls c.Core.Write for all inner cores.
func (c *otelCtxCore) Check(entry zapcore.Entry, ce *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	if c.Core.Enabled(entry.Level) {
		return ce.AddCore(entry, c)
	}
	return ce
}

// noopJSONContext embeds context.Context so otelzap detects it via type assertion,
// but marshals to JSON null so stdout logs don't contain a raw context dump.
type noopJSONContext struct{ context.Context }

func (noopJSONContext) MarshalJSON() ([]byte, error) { return []byte("null"), nil }

// NewLogger creates and returns a new Logger instance
func NewLogger(cfg *config.Configuration) (*Logger, error) {
	config := zap.NewProductionConfig()

	if cfg.Logging.DBLevel == types.LogLevelDebug {
		config = zap.NewDevelopmentConfig()
	}

	// Apply the configured log level (debug/info/warn/error) to the zap logger.
	// zap's AtomicLevel.UnmarshalText understands standard level strings.
	if err := config.Level.UnmarshalText([]byte(cfg.Logging.Level)); err != nil {
		// Fallback to info if the level string is invalid
		config.Level = zap.NewAtomicLevelAt(zapcore.InfoLevel)
	}

	config.EncoderConfig.TimeKey = "timestamp"
	config.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder

	// Disable stack traces for warnings to reduce log noise
	config.DisableStacktrace = true

	zapLogger, err := config.Build()
	if err != nil {
		return nil, err
	}

	// Initialize OpenTelemetry log exporter (for any OTLP backend). Reads the
	// unified otel.logs.* config first; falls back to legacy logging.otel_*
	// fields (deprecated) so existing deployments keep working.
	logsCfg, headers, legacy := resolveOtelLogsConfig(cfg)
	var otelLogProvider *sdklog.LoggerProvider
	if logsCfg.Enabled && logsCfg.Endpoint != "" {
		if legacy {
			zapLogger.Sugar().Warn("DEPRECATED: configure OTel log export under `otel.logs.*` instead of `logging.otel_*`; legacy keys will be removed in a future release")
		}
		otelLogProvider, err = newOtelLogProvider(context.Background(), cfg, logsCfg, headers)
		if err != nil {
			zapLogger.Sugar().Warnf("Failed to initialize OTel log exporter: %v, falling back to stdout only", err)
			otelLogProvider = nil
		} else {
			zapLogger.Sugar().Infof("OTel log exporter initialized (endpoint: %s, protocol: %s, header_count: %d)", logsCfg.Endpoint, logsCfg.Protocol, len(headers))
		}
		if cfg.Logging.OtelDebug {
			// Route otel SDK internal errors (e.g. failed exports, auth errors) to zap so they appear in logs.
			otel.SetErrorHandler(otel.ErrorHandlerFunc(func(e error) {
				zapLogger.Sugar().Errorf("OTel export error: %v", e)
			}))
		}
	}

	// Build the final zap logger, optionally tee-ing into the otelzap bridge.
	// zapcore.NewTee's Enabled() is true if ANY core enables the level. The otelzap
	// core delegates to OTel Logger.Enabled(), which (with the SDK batch processor)
	// accepts all severities, so Debug would still flow to OTLP when the main core
	// is gated to Info. Wrap the otel core with the same LevelEnabler as the
	// preset logger so OTLP respects logging.level.
	finalLogger := zapLogger
	if otelLogProvider != nil {
		scopeName := cfg.Logging.ServiceName
		if scopeName == "" {
			scopeName = string(cfg.Deployment.Mode)
		}
		otelCore := otelzap.NewCore(scopeName, otelzap.WithLoggerProvider(otelLogProvider))
		otelTeeCore, incrErr := zapcore.NewIncreaseLevelCore(otelCore, config.Level)
		if incrErr != nil {
			return nil, incrErr
		}
		finalLogger = zap.New(zapcore.NewTee(zapLogger.Core(), otelTeeCore), zap.WithCaller(true))
	}

	sugar := finalLogger.Sugar()
	if cfg.Logging.ServiceName != "" {
		sugar = sugar.With("service.name", cfg.Logging.ServiceName)
	}
	if cfg.Logging.Environment != "" {
		sugar = sugar.With("deployment.environment", cfg.Logging.Environment)
	}
	if cfg.Logging.Region != "" {
		sugar = sugar.With("cloud.region", cfg.Logging.Region)
	}

	return &Logger{
		SugaredLogger:   sugar,
		otelLogProvider: otelLogProvider,
	}, nil
}

// NewNoopLogger returns a logger that discards all output. For use in tests only.
func NewNoopLogger() *Logger {
	return &Logger{SugaredLogger: zap.NewNop().Sugar()}
}

// NewFromSugared creates a Logger from an existing SugaredLogger. For use in tests only.
func NewFromSugared(s *zap.SugaredLogger) *Logger {
	return &Logger{SugaredLogger: s}
}

// resolveOtelLogsConfig picks the active log-export settings. Precedence:
//  1. otel.enabled && otel.logs.* (unified config)
//  2. logging.otel_* (legacy)
//
// The bool return is true when the legacy path supplied the values; the caller
// uses it to print a deprecation warning.
func resolveOtelLogsConfig(cfg *config.Configuration) (config.OtelLogsConfig, map[string]string, bool) {
	if cfg.Otel.Enabled && cfg.Otel.Logs.Enabled && cfg.Otel.Logs.Endpoint != "" {
		logs := cfg.Otel.Logs
		// Always normalize through ResolveProtocol so values like "http/protobuf"
		// collapse to the canonical "http" transport. Without this, an exact
		// `logsCfg.Protocol == "http"` check below would miss "http/protobuf" and
		// silently fall back to the gRPC exporter (404 against an HTTP endpoint).
		logs.Protocol = cfg.Otel.ResolveProtocol(logs.Protocol)
		headers := cfg.Otel.ResolveHeaders(logs.MergedHeaders())
		return logs, headers, false
	}

	// Legacy fallback
	if cfg.Logging.OtelEnabled && cfg.Logging.OtelEndpoint != "" {
		legacyHeaders := map[string]string{}
		if cfg.Logging.OtelAuthHeader != "" && cfg.Logging.OtelAuthValue != "" {
			legacyHeaders[cfg.Logging.OtelAuthHeader] = cfg.Logging.OtelAuthValue
		}
		protocol := cfg.Logging.OtelProtocol
		if protocol == "" {
			protocol = "grpc"
		}
		return config.OtelLogsConfig{
			Enabled:  true,
			Endpoint: cfg.Logging.OtelEndpoint,
			Protocol: protocol,
			Headers:  legacyHeaders,
		}, legacyHeaders, true
	}

	return config.OtelLogsConfig{}, nil, false
}

// newOtelLogProvider builds a sdklog.LoggerProvider that exports via OTLP (gRPC or HTTP).
func newOtelLogProvider(ctx context.Context, cfg *config.Configuration, logsCfg config.OtelLogsConfig, headers map[string]string) (*sdklog.LoggerProvider, error) {
	// Insecure is shared across signals at the top-level (rare to need TLS for
	// one signal but not another — usually both go to the same network zone).
	insecure := cfg.Otel.Insecure || cfg.Logging.OtelInsecure

	var exporter sdklog.Exporter
	var err error

	endpointIsURL := strings.HasPrefix(logsCfg.Endpoint, "http://") || strings.HasPrefix(logsCfg.Endpoint, "https://")

	if strings.HasPrefix(logsCfg.Protocol, "http") {
		httpOpts := []otlploghttp.Option{}
		if endpointIsURL {
			httpOpts = append(httpOpts, otlploghttp.WithEndpointURL(logsCfg.Endpoint))
		} else {
			httpOpts = append(httpOpts, otlploghttp.WithEndpoint(logsCfg.Endpoint))
		}
		if insecure {
			httpOpts = append(httpOpts, otlploghttp.WithInsecure())
		}
		if len(headers) > 0 {
			httpOpts = append(httpOpts, otlploghttp.WithHeaders(headers))
		}
		exporter, err = otlploghttp.New(ctx, httpOpts...)
	} else {
		// default: grpc
		grpcOpts := []otlploggrpc.Option{}
		if endpointIsURL {
			grpcOpts = append(grpcOpts, otlploggrpc.WithEndpointURL(logsCfg.Endpoint))
		} else {
			grpcOpts = append(grpcOpts, otlploggrpc.WithEndpoint(logsCfg.Endpoint))
		}
		if insecure {
			grpcOpts = append(grpcOpts, otlploggrpc.WithInsecure())
		}
		if len(headers) > 0 {
			grpcOpts = append(grpcOpts, otlploggrpc.WithHeaders(headers))
		}
		exporter, err = otlploggrpc.New(ctx, grpcOpts...)
	}
	if err != nil {
		return nil, err
	}

	// Build resource with service.name, deployment.environment, and cloud.region
	resAttrs := []attribute.KeyValue{
		semconv.ServiceName(cfg.Otel.ResolveServiceName(cfg)),
	}
	if cfg.Logging.Environment != "" {
		resAttrs = append(resAttrs, semconv.DeploymentEnvironmentName(cfg.Logging.Environment))
	}
	if cfg.Logging.Region != "" {
		resAttrs = append(resAttrs, semconv.CloudRegion(cfg.Logging.Region))
	}
	res, err := resource.New(ctx,
		resource.WithAttributes(resAttrs...),
	)
	if err != nil {
		return nil, err
	}

	// OtelDebug: synchronous processor exports immediately — use to confirm delivery without waiting for batch timer.
	var processor sdklog.Processor
	if cfg.Logging.OtelDebug {
		processor = sdklog.NewSimpleProcessor(exporter)
	} else {
		processor = sdklog.NewBatchProcessor(exporter)
	}

	provider := sdklog.NewLoggerProvider(
		sdklog.WithProcessor(processor),
		sdklog.WithResource(res),
	)
	return provider, nil
}

// Shutdown flushes and closes the OTel log provider. Call this on application exit.
func (l *Logger) Shutdown(ctx context.Context) {
	if l.otelLogProvider != nil {
		_ = l.otelLogProvider.Shutdown(ctx)
	}
}

// OtelLogProvider returns the underlying OTel LoggerProvider (e.g. to register as global).
func (l *Logger) OtelLogProvider() otellog.LoggerProvider {
	return l.otelLogProvider
}

// ---------------------------------------------------------------------------
// Context binding
// ---------------------------------------------------------------------------

// WithContext binds request-scoped fields and (if present) the active OTel
// span's trace_id / span_id so logs correlate to traces in SigNoz.
//
// Two correlation mechanisms are applied:
//  1. trace_id / span_id are injected as string fields — visible in stdout JSON
//     and as OTLP log attributes for human-readable inspection.
//  2. The context is injected via otelCtxCore so the otelzap bridge sets the
//     OTLP log record's record-level TraceId and SpanId fields — these are what
//     SigNoz uses to link logs to their trace in the Span Details "Logs" tab.
func (l *Logger) WithContext(ctx context.Context) *Logger {
	// Guard against nil ctx: noopJSONContext embeds context.Context as an
	// interface, so noopJSONContext{nil}.Value(...) panics deep in the OTel SDK.
	if ctx == nil {
		ctx = context.Background()
	}

	fields := []interface{}{
		"request_id", types.GetRequestID(ctx),
		"tenant_id", types.GetTenantID(ctx),
		"user_id", types.GetUserID(ctx),
		"environment_id", types.GetEnvironmentID(ctx),
	}

	sc := trace.SpanFromContext(ctx).SpanContext()
	if sc.IsValid() {
		fields = append(fields,
			"trace_id", sc.TraceID().String(),
			"span_id", sc.SpanID().String(),
		)
	}

	newSugared := l.SugaredLogger.With(fields...)

	// Inject ctx into the otelzap bridge so it can populate the OTLP log record's
	// record-level TraceId/SpanId (not just string attributes). This enables
	// SigNoz's native trace-log correlation. We do this even for ended spans —
	// the span context (TraceId, SpanId) remains valid after span.End().
	//
	// Guard: only wrap once. Repeated WithContext calls (e.g. middleware chains
	// calling Ctx(ctx) on an already-context-bound logger) would otherwise
	// accumulate nested otelCtxCore layers, producing duplicate _span_ctx fields
	// on every Write. ctxBound is set on the returned Logger to prevent this.
	ctxBound := l.ctxBound
	if sc.IsValid() && !l.ctxBound {
		newSugared = newSugared.WithOptions(zap.WrapCore(func(c zapcore.Core) zapcore.Core {
			return &otelCtxCore{Core: c, ctx: ctx}
		}))
		ctxBound = true
	}

	return &Logger{
		SugaredLogger:   newSugared,
		otelLogProvider: l.otelLogProvider,
		ctxBound:        ctxBound,
	}
}

// Ctx is a short alias for WithContext — use at the top of service methods to
// get a request-scoped logger that carries the correct OTel trace ID:
//
//	log := s.Logger.Ctx(ctx)
//	log.Errorw("failed", "error", err)
func (l *Logger) Ctx(ctx context.Context) *Logger {
	return l.WithContext(ctx)
}

// ---------------------------------------------------------------------------
// ctx-first logging API (preferred)
// ---------------------------------------------------------------------------

// Debug logs at debug level with auto-bound context fields.
func (l *Logger) Debug(ctx context.Context, msg string, fields ...any) {
	l.WithContext(ctx).SugaredLogger.Debugw(msg, fields...)
}

// Info logs at info level with auto-bound context fields.
func (l *Logger) Info(ctx context.Context, msg string, fields ...any) {
	l.WithContext(ctx).SugaredLogger.Infow(msg, fields...)
}

// Warn logs at warn level. Only use in bootstrap/startup code.
func (l *Logger) Warn(ctx context.Context, msg string, fields ...any) {
	l.WithContext(ctx).SugaredLogger.Warnw(msg, fields...)
}

// Error logs at error level with auto-bound context fields.
func (l *Logger) Error(ctx context.Context, msg string, fields ...any) {
	l.WithContext(ctx).SugaredLogger.Errorw(msg, fields...)
}

// Fatal logs at fatal level then calls os.Exit(1). Use only in cmd/.
func (l *Logger) Fatal(ctx context.Context, msg string, fields ...any) {
	l.WithContext(ctx).SugaredLogger.Fatalw(msg, fields...)
}

// ---------------------------------------------------------------------------
// Legacy w-variants (kept for gradual migration, do not use in new code)
// ---------------------------------------------------------------------------

// Debugw logs at debug level with key-value pairs.
func (l *Logger) Debugw(msg string, keysAndValues ...interface{}) {
	l.SugaredLogger.Debugw(msg, keysAndValues...)
}

// Infow logs at info level with key-value pairs.
func (l *Logger) Infow(msg string, keysAndValues ...interface{}) {
	l.SugaredLogger.Infow(msg, keysAndValues...)
}

// Warnw logs at warn level with key-value pairs.
func (l *Logger) Warnw(msg string, keysAndValues ...interface{}) {
	l.SugaredLogger.Warnw(msg, keysAndValues...)
}

// Errorw logs at error level with key-value pairs.
func (l *Logger) Errorw(msg string, keysAndValues ...interface{}) {
	l.SugaredLogger.Errorw(msg, keysAndValues...)
}

// ---------------------------------------------------------------------------
// Structured field helpers
// ---------------------------------------------------------------------------

// Err produces structured fields for an error. Always use on log.Error calls.
func Err(err error) []any {
	if err == nil {
		return []any{"error", "<nil>", "error_type", "<nil>"}
	}
	return []any{"error", err.Error(), "error_type", fmt.Sprintf("%T", err)}
}

// Op produces a structured "operation" field. Value should be "<entity>.<verb_past>".
func Op(name string) []any {
	return []any{"operation", name}
}

// Event produces "entity", "action", and "operation" fields.
func Event(entity, action string) []any {
	return []any{"entity", entity, "action", action, "operation", entity + "." + action}
}

// Entity produces an "<entity>_id" field.
func Entity(name, id string) []any {
	return []any{name + "_id", id}
}

// ---------------------------------------------------------------------------
// Framework adapter loggers
// ---------------------------------------------------------------------------

// retryableHTTPLogger adapts our Logger to go-retryablehttp's logging interface
type retryableHTTPLogger struct {
	logger *Logger
}

// GetRetryableHTTPLogger returns a retryable HTTP client-compatible logger
func (l *Logger) GetRetryableHTTPLogger() *retryableHTTPLogger {
	return &retryableHTTPLogger{logger: l}
}

// Printf implements the Logger interface for go-retryablehttp
func (r *retryableHTTPLogger) Printf(format string, v ...interface{}) {
	r.logger.Info(context.Background(), fmt.Sprintf(format, v...))
}

// GetEntLogger returns an ent-compatible logger function bound to the given ctx.
func (l *Logger) GetEntLogger(ctx context.Context) func(...any) {
	return func(args ...any) {
		if len(args) == 0 {
			return
		}
		if len(args) == 1 {
			if query, ok := args[0].(string); ok {
				l.Debug(ctx, "ent_query", "query", query)
				return
			}
		}
		l.Debug(ctx, "ent_query", "query", fmt.Sprint(args...))
	}
}

// ginLogger adapts our Logger to gin's logging interface
type ginLogger struct {
	logger *Logger
}

// GetGinLogger returns a gin-compatible logger
func (l *Logger) GetGinLogger() *ginLogger {
	return &ginLogger{logger: l}
}

// Write implements the io.Writer interface for gin
func (g *ginLogger) Write(p []byte) (n int, err error) {
	g.logger.Info(context.Background(), string(p))
	return len(p), nil
}
