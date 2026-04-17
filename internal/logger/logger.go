package logger

import (
	"context"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/fluent/fluent-logger-golang/fluent"
	"github.com/getsentry/sentry-go"
	"go.opentelemetry.io/contrib/bridges/otelzap"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	otellog "go.opentelemetry.io/otel/log"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// sentryToZapLevel maps a sentry LogLevel to its zapcore equivalent for level-gating.
var sentryToZapLevel = map[sentry.LogLevel]zapcore.Level{
	sentry.LogLevelDebug: zapcore.DebugLevel,
	sentry.LogLevelInfo:  zapcore.InfoLevel,
	sentry.LogLevelWarn:  zapcore.WarnLevel,
	sentry.LogLevelError: zapcore.ErrorLevel,
	sentry.LogLevelFatal: zapcore.FatalLevel,
}

// Logger wraps zap.SugaredLogger to provide logging functionality
type Logger struct {
	*zap.SugaredLogger
	fluentdLogger   *fluent.Fluent
	otelLogProvider *sdklog.LoggerProvider
	serviceName     string
	sentryEnabled   bool
	sentryCtx       context.Context // used for sentry.NewLogger; defaults to context.Background()
}

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

	// Initialize OpenTelemetry log exporter (for any OTLP backend)
	var otelLogProvider *sdklog.LoggerProvider
	if cfg.Logging.OtelEnabled && cfg.Logging.OtelEndpoint != "" {
		otelLogProvider, err = newOtelLogProvider(context.Background(), cfg)
		if err != nil {
			zapLogger.Sugar().Warnf("Failed to initialize OTel log exporter: %v, falling back to stdout only", err)
			otelLogProvider = nil
		} else {
			zapLogger.Sugar().Infof("OTel log exporter initialized (endpoint: %s, protocol: %s, auth_header: %s, auth_value_set: %v)", cfg.Logging.OtelEndpoint, cfg.Logging.OtelProtocol, cfg.Logging.OtelAuthHeader, cfg.Logging.OtelAuthValue != "")
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
		sentryEnabled:   false,
		sentryCtx:       context.Background(),
	}, nil
}

// newOtelLogProvider builds a sdklog.LoggerProvider that exports via OTLP (gRPC or HTTP).
func newOtelLogProvider(ctx context.Context, cfg *config.Configuration) (*sdklog.LoggerProvider, error) {
	headers := map[string]string{}
	if cfg.Logging.OtelAuthHeader != "" && cfg.Logging.OtelAuthValue != "" {
		headers[cfg.Logging.OtelAuthHeader] = cfg.Logging.OtelAuthValue
	}

	var exporter sdklog.Exporter
	var err error

	if cfg.Logging.OtelProtocol == "http" {
		httpOpts := []otlploghttp.Option{
			otlploghttp.WithEndpoint(cfg.Logging.OtelEndpoint),
		}
		if cfg.Logging.OtelInsecure {
			httpOpts = append(httpOpts, otlploghttp.WithInsecure())
		}
		if len(headers) > 0 {
			httpOpts = append(httpOpts, otlploghttp.WithHeaders(headers))
		}
		exporter, err = otlploghttp.New(ctx, httpOpts...)
	} else {
		// default: grpc
		grpcOpts := []otlploggrpc.Option{
			otlploggrpc.WithEndpoint(cfg.Logging.OtelEndpoint),
		}
		if cfg.Logging.OtelInsecure {
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
		semconv.ServiceName(cfg.Logging.ServiceName),
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
	msg := l.sprintf(template, args...)
	l.sendToFluentd("debug", msg, nil)
	l.sendToSentryLogs(sentry.LogLevelDebug, msg)
}

func (l *Logger) Infof(template string, args ...interface{}) {
	l.SugaredLogger.Infof(template, args...)
	msg := l.sprintf(template, args...)
	l.sendToFluentd("info", msg, nil)
	l.sendToSentryLogs(sentry.LogLevelInfo, msg)
}

func (l *Logger) Warnf(template string, args ...interface{}) {
	l.SugaredLogger.Warnf(template, args...)
	msg := l.sprintf(template, args...)
	l.sendToFluentd("warning", msg, nil)
	l.sendToSentryLogs(sentry.LogLevelWarn, msg)
	l.captureToSentry(sentry.LevelWarning, msg)
}

func (l *Logger) Errorf(template string, args ...interface{}) {
	l.SugaredLogger.Errorf(template, args...)
	msg := l.sprintf(template, args...)
	l.sendToFluentd("error", msg, nil)
	l.sendToSentryLogs(sentry.LogLevelError, msg)
	l.captureToSentry(sentry.LevelError, msg)
}

func (l *Logger) Fatalf(template string, args ...interface{}) {
	msg := l.sprintf(template, args...)
	l.sendToFluentd("fatal", msg, nil)
	l.sendToSentryLogs(sentry.LogLevelFatal, msg)
	l.SugaredLogger.Fatalf(template, args...)
}

// sprintf is a helper to format strings
func (l *Logger) sprintf(template string, args ...interface{}) string {
	if len(args) == 0 {
		return template
	}
	// Use standard library fmt.Sprintf
	return fmt.Sprintf(template, args...)
}

func (l *Logger) WithContext(ctx context.Context) *Logger {
	requestID := types.GetRequestID(ctx)
	tenantID := types.GetTenantID(ctx)
	userID := types.GetUserID(ctx)

	return &Logger{
		SugaredLogger: l.SugaredLogger.With(
			"request_id", requestID,
			"tenant_id", tenantID,
			"user_id", userID,
		),
		fluentdLogger:   l.fluentdLogger,
		otelLogProvider: l.otelLogProvider,
		serviceName:     l.serviceName,
		sentryEnabled:   l.sentryEnabled,
		sentryCtx:       ctx,
	}
}

// Ctx is a short alias for WithContext — use at the top of service methods to get a
// request-scoped logger that carries the correct Sentry trace ID:
//
//	log := s.Logger.Ctx(ctx)
//	log.Errorw("failed", "error", err)
func (l *Logger) Ctx(ctx context.Context) *Logger {
	return l.WithContext(ctx)
}

// sendToSentryLogs sends a structured log record to Sentry Logs (requires EnableLogs: true).
// This is separate from captureToSentry — it feeds the Sentry Logs product, not the Issues/Events stream.
// It respects the configured zap log level, so debug logs won't be sent in production.
func (l *Logger) sendToSentryLogs(level sentry.LogLevel, msg string, keysAndValues ...interface{}) {
	if !l.sentryEnabled {
		return
	}
	if zapLevel, ok := sentryToZapLevel[level]; ok {
		if !l.SugaredLogger.Desugar().Core().Enabled(zapLevel) {
			return
		}
	}
	hub := sentry.GetHubFromContext(l.sentryCtx)
	if hub == nil {
		hub = sentry.CurrentHub()
	}
	if hub.Client() == nil {
		return
	}

	sl := sentry.NewLogger(l.sentryCtx)
	var entry sentry.LogEntry
	switch level {
	case sentry.LogLevelDebug:
		entry = sl.Debug()
	case sentry.LogLevelInfo:
		entry = sl.Info()
	case sentry.LogLevelWarn:
		entry = sl.Warn()
	case sentry.LogLevelError:
		entry = sl.Error()
	default:
		entry = sl.Info()
	}

	for i := 0; i+1 < len(keysAndValues); i += 2 {
		key, ok := keysAndValues[i].(string)
		if !ok {
			continue
		}
		switch v := keysAndValues[i+1].(type) {
		case string:
			entry = entry.String(key, v)
		case int:
			entry = entry.Int(key, v)
		case int64:
			entry = entry.Int64(key, v)
		case float64:
			entry = entry.Float64(key, v)
		case bool:
			entry = entry.Bool(key, v)
		case error:
			entry = entry.String(key, v.Error())
		default:
			entry = entry.String(key, fmt.Sprintf("%v", v))
		}
	}

	entry.Emit(msg)
}

// captureToSentry sends an event to Sentry if enabled.
// It looks for an "error" key in keysAndValues and uses CaptureException;
// otherwise it falls back to CaptureMessage.
func (l *Logger) captureToSentry(level sentry.Level, msg string, keysAndValues ...interface{}) {
	if !l.sentryEnabled {
		return
	}
	// Use the per-request hub if available (set by sentrygin on c.Request.Context()),
	// otherwise fall back to the global hub.
	hub := sentry.GetHubFromContext(l.sentryCtx)
	if hub == nil {
		hub = sentry.CurrentHub()
	}
	if hub.Client() == nil {
		return
	}

	// Look for an error value in key-value pairs
	for i := 1; i < len(keysAndValues); i += 2 {
		if err, ok := keysAndValues[i].(error); ok {
			hub.WithScope(func(scope *sentry.Scope) {
				scope.SetLevel(level)
				scope.SetExtra("message", msg)
				for j := 0; j < len(keysAndValues)-1; j += 2 {
					if key, ok := keysAndValues[j].(string); ok {
						scope.SetExtra(key, keysAndValues[j+1])
					}
				}
				hub.CaptureException(err)
			})
			return
		}
	}

	hub.WithScope(func(scope *sentry.Scope) {
		scope.SetLevel(level)
		for i := 0; i < len(keysAndValues)-1; i += 2 {
			if key, ok := keysAndValues[i].(string); ok {
				scope.SetExtra(key, keysAndValues[i+1])
			}
		}
		hub.CaptureMessage(msg)
	})
}

// Structured logging methods that include context fields
func (l *Logger) Debugw(msg string, keysAndValues ...interface{}) {
	l.SugaredLogger.Debugw(msg, keysAndValues...)
	l.sendToFluentd("debug", msg, l.keysAndValuesToMap(keysAndValues...))
	l.sendToSentryLogs(sentry.LogLevelDebug, msg, keysAndValues...)
}

func (l *Logger) Infow(msg string, keysAndValues ...interface{}) {
	l.SugaredLogger.Infow(msg, keysAndValues...)
	l.sendToFluentd("info", msg, l.keysAndValuesToMap(keysAndValues...))
	l.sendToSentryLogs(sentry.LogLevelInfo, msg, keysAndValues...)
}

func (l *Logger) Warnw(msg string, keysAndValues ...interface{}) {
	l.SugaredLogger.Warnw(msg, keysAndValues...)
	l.sendToFluentd("warning", msg, l.keysAndValuesToMap(keysAndValues...))
	l.sendToSentryLogs(sentry.LogLevelWarn, msg, keysAndValues...)
	l.captureToSentry(sentry.LevelWarning, msg, keysAndValues...)
}

func (l *Logger) Errorw(msg string, keysAndValues ...interface{}) {
	l.SugaredLogger.Errorw(msg, keysAndValues...)
	l.sendToFluentd("error", msg, l.keysAndValuesToMap(keysAndValues...))
	l.sendToSentryLogs(sentry.LogLevelError, msg, keysAndValues...)
	l.captureToSentry(sentry.LevelError, msg, keysAndValues...)
}

// Context-aware logging methods — these bind the request context for Sentry trace correlation.
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
