package logger

import (
	"context"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/fluent/fluent-logger-golang/fluent"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// Logger wraps zap.SugaredLogger to provide logging functionality
type Logger struct {
	*zap.SugaredLogger
	fluentdLogger *fluent.Fluent
	serviceName   string
}

// Global logger for convenience
var L *Logger

// NewLogger creates and returns a new Logger instance
func NewLogger(cfg *config.Configuration) (*Logger, error) {
	config := zap.NewProductionConfig()

	if cfg.Logging.DBLevel == types.LogLevelDebug {
		config = zap.NewDevelopmentConfig()
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

	return &Logger{
		SugaredLogger: zapLogger.Sugar(),
		fluentdLogger: fluentdLogger,
		serviceName:   string(cfg.Deployment.Mode),
	}, nil
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
		fluentdLogger: l.fluentdLogger,
		serviceName:   l.serviceName,
	}
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
