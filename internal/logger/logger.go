package logger

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/fluent/fluent-logger-golang/fluent"
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
//   - Fluentd (optional, via FluentdEnabled config)
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
	fluentdLogger   *fluent.Fluent
	otelLogProvider *sdklog.LoggerProvider
	serviceName     string
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

// Global logger for convenience
var L *Logger

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

	// Initialize Fluentd logger based on configuration
	var fluentdLogger *fluent.Fluent
	var fluentdHost string
	var fluentdPort int

	if cfg.Logging.FluentdEnabled {
		fluentdHost = cfg.Logging.FluentdHost
		fluentdPort = cfg.Logging.FluentdPort
	}

	// Initialize Fluentd client if host and port are configured
	if fluentdHost != "" && fluentdPort > 0 {
		fluentdLogger, err = fluent.New(fluent.Config{
			FluentHost:   fluentdHost,
			FluentPort:   fluentdPort,
			Async:        true,
			BufferLimit:  8 * 1024 * 1024, // 8MB buffer
			WriteTimeout: 3 * time.Second,
			RetryWait:    500,
			MaxRetry:     5,
		})
		if err != nil {
			zapLogger.Sugar().Warnf("Failed to initialize Fluentd logger: %v, falling back to stdout only", err)
		} else {
			zapLogger.Sugar().Infof("Fluentd logger initialized successfully (host: %s, port: %d)", fluentdHost, fluentdPort)
		}
	} else if cfg.Logging.FluentdEnabled {
		zapLogger.Sugar().Warn("Fluentd is enabled but host/port not configured properly")
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
		fluentdLogger:   fluentdLogger,
		otelLogProvider: otelLogProvider,
		serviceName:     string(cfg.Deployment.Mode),
	}, nil
}

// NewNoopLogger returns a logger that discards all output. For use in tests only.
func NewNoopLogger() *Logger {
	return &Logger{SugaredLogger: zap.NewNop().Sugar()}
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

// Initialize default logger and set it as global while also using Dependency Injection
// Given logger is a heavily used object and is used in many places so it's a good idea to
// have it as a global variable as well for usecases like scripts but for everywhere else
// we should try to use the Dependency Injection approach only.
func init() {
	L, _ = NewLogger(config.GetDefaultConfig())
}

func GetLogger() *Logger {
	if L == nil {
		L, _ = NewLogger(config.GetDefaultConfig())
	}
	return L
}

func GetLoggerWithContext(ctx context.Context) *Logger {
	return GetLogger().WithContext(ctx)
}

// sanitizeValue converts error objects to strings for msgpack serialization
// Also handles nested structures (maps and slices) that may contain errors
func sanitizeValue(v interface{}) interface{} {
	// Convert error objects to strings
	if err, ok := v.(error); ok {
		return err.Error()
	}

	// Handle nested maps
	if m, ok := v.(map[string]interface{}); ok {
		sanitized := make(map[string]interface{}, len(m))
		for k, val := range m {
			sanitized[k] = sanitizeValue(val)
		}
		return sanitized
	}

	// Handle slices/arrays
	if s, ok := v.([]interface{}); ok {
		sanitized := make([]interface{}, len(s))
		for i, val := range s {
			sanitized[i] = sanitizeValue(val)
		}
		return sanitized
	}

	return v
}

// sendToFluentd sends structured log data to Fluentd
func (l *Logger) sendToFluentd(level string, msg string, fields map[string]interface{}) {
	if l.fluentdLogger == nil {
		return // Fluentd not configured, skip
	}

	logData := map[string]interface{}{
		"level":     level,
		"message":   msg,
		"service":   l.serviceName,
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	}

	// Merge additional fields, converting error objects to strings
	for k, v := range fields {
		logData[k] = sanitizeValue(v)
	}

	// Post to Fluentd asynchronously (non-blocking)
	// Tag format: app.logs
	err := l.fluentdLogger.Post("app.logs", logData)
	if err != nil {
		// If Fluentd fails, log to stderr but don't block the application
		l.SugaredLogger.Warnf("Failed to send log to Fluentd: %v", err)
	}
}

// Helper methods to make logging more convenient
func (l *Logger) Debugf(template string, args ...interface{}) {
	l.SugaredLogger.Debugf(template, args...)
	l.sendToFluentd("debug", l.sprintf(template, args...), nil)
}

func (l *Logger) Infof(template string, args ...interface{}) {
	l.SugaredLogger.Infof(template, args...)
	l.sendToFluentd("info", l.sprintf(template, args...), nil)
}

func (l *Logger) Warnf(template string, args ...interface{}) {
	l.SugaredLogger.Warnf(template, args...)
	l.sendToFluentd("warning", l.sprintf(template, args...), nil)
}

func (l *Logger) Errorf(template string, args ...interface{}) {
	l.SugaredLogger.Errorf(template, args...)
	l.sendToFluentd("error", l.sprintf(template, args...), nil)
}

func (l *Logger) Fatalf(template string, args ...interface{}) {
	msg := l.sprintf(template, args...)
	l.sendToFluentd("fatal", msg, nil)
	l.SugaredLogger.Fatalf(template, args...)
}

// sprintf is a helper to format strings
func (l *Logger) sprintf(template string, args ...interface{}) string {
	if len(args) == 0 {
		return template
	}
	return fmt.Sprintf(template, args...)
}

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
	if sc.IsValid() {
		newSugared = newSugared.WithOptions(zap.WrapCore(func(c zapcore.Core) zapcore.Core {
			return &otelCtxCore{Core: c, ctx: ctx}
		}))
	}

	return &Logger{
		SugaredLogger:   newSugared,
		fluentdLogger:   l.fluentdLogger,
		otelLogProvider: l.otelLogProvider,
		serviceName:     l.serviceName,
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

// Structured logging methods that include context fields
func (l *Logger) Debugw(msg string, keysAndValues ...interface{}) {
	l.SugaredLogger.Debugw(msg, keysAndValues...)
	l.sendToFluentd("debug", msg, l.keysAndValuesToMap(keysAndValues...))
}

func (l *Logger) Infow(msg string, keysAndValues ...interface{}) {
	l.SugaredLogger.Infow(msg, keysAndValues...)
	l.sendToFluentd("info", msg, l.keysAndValuesToMap(keysAndValues...))
}

func (l *Logger) Warnw(msg string, keysAndValues ...interface{}) {
	l.SugaredLogger.Warnw(msg, keysAndValues...)
	l.sendToFluentd("warning", msg, l.keysAndValuesToMap(keysAndValues...))
}

func (l *Logger) Errorw(msg string, keysAndValues ...interface{}) {
	l.SugaredLogger.Errorw(msg, keysAndValues...)
	l.sendToFluentd("error", msg, l.keysAndValuesToMap(keysAndValues...))
}

// Context-aware logging methods — these bind the request context for trace correlation.
// Use these in service/repository methods instead of the plain variants:
//
//	s.Logger.ErrorwCtx(ctx, "failed", "error", err)   instead of   s.Logger.Errorw("failed", "error", err)
func (l *Logger) DebugwCtx(ctx context.Context, msg string, keysAndValues ...interface{}) {
	l.WithContext(ctx).Debugw(msg, keysAndValues...)
}

func (l *Logger) InfowCtx(ctx context.Context, msg string, keysAndValues ...interface{}) {
	l.WithContext(ctx).Infow(msg, keysAndValues...)
}

func (l *Logger) WarnwCtx(ctx context.Context, msg string, keysAndValues ...interface{}) {
	l.WithContext(ctx).Warnw(msg, keysAndValues...)
}

func (l *Logger) ErrorwCtx(ctx context.Context, msg string, keysAndValues ...interface{}) {
	l.WithContext(ctx).Errorw(msg, keysAndValues...)
}

func (l *Logger) DebugfCtx(ctx context.Context, template string, args ...interface{}) {
	l.WithContext(ctx).Debugf(template, args...)
}

func (l *Logger) InfofCtx(ctx context.Context, template string, args ...interface{}) {
	l.WithContext(ctx).Infof(template, args...)
}

func (l *Logger) WarnfCtx(ctx context.Context, template string, args ...interface{}) {
	l.WithContext(ctx).Warnf(template, args...)
}

func (l *Logger) ErrorfCtx(ctx context.Context, template string, args ...interface{}) {
	l.WithContext(ctx).Errorf(template, args...)
}

// keysAndValuesToMap converts variadic key-value pairs to a map
func (l *Logger) keysAndValuesToMap(keysAndValues ...interface{}) map[string]interface{} {
	fields := make(map[string]interface{})
	for i := 0; i < len(keysAndValues); i += 2 {
		if i+1 < len(keysAndValues) {
			if key, ok := keysAndValues[i].(string); ok {
				// Convert error objects to strings for msgpack serialization
				fields[key] = sanitizeValue(keysAndValues[i+1])
			}
		}
	}
	return fields
}

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
	r.logger.Infof(format, v...)
}

// GetEntLogger returns an ent-compatible logger function
func (l *Logger) GetEntLogger() func(...any) {
	return func(args ...any) {
		// Ent typically passes query strings, format them properly
		if len(args) > 0 {
			// If args is a single string, use it as the query
			if len(args) == 1 {
				if query, ok := args[0].(string); ok {
					l.Debugw("ent_query", "query", query)
					return
				}
			}
			// Otherwise, format all args as a single query string
			l.Debugw("ent_query", "query", args)
		}
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
	g.logger.Info(string(p))
	return len(p), nil
}
