package logger

import (
	"context"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/types"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// Logger wraps zap.SugaredLogger to provide logging functionality
type Logger struct {
	*zap.SugaredLogger
}

// Global logger for convenience
var L *Logger

// NewLogger creates and returns a new Logger instance
func NewLogger(cfg *config.Configuration) (*Logger, error) {
	config := zap.NewProductionConfig()

	if cfg.Logging.Level == types.LogLevelDebug {
		config = zap.NewDevelopmentConfig()
	}

	config.EncoderConfig.TimeKey = "timestamp"
	config.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder

	zapLogger, err := config.Build()
	if err != nil {
		return nil, err
	}

	return &Logger{
		SugaredLogger: zapLogger.Sugar(),
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

// Helper methods to make logging more convenient
func (l *Logger) Debugf(template string, args ...interface{}) {
	l.SugaredLogger.Debugf(template, args...)
}

func (l *Logger) Infof(template string, args ...interface{}) {
	l.SugaredLogger.Infof(template, args...)
}

func (l *Logger) Warnf(template string, args ...interface{}) {
	l.SugaredLogger.Warnf(template, args...)
}

func (l *Logger) Errorf(template string, args ...interface{}) {
	l.SugaredLogger.Errorf(template, args...)
}

func (l *Logger) Fatalf(template string, args ...interface{}) {
	l.SugaredLogger.Fatalf(template, args...)
}

func (l *Logger) WithContext(ctx context.Context) *Logger {
	return &Logger{
		SugaredLogger: l.SugaredLogger.With(
			"request_id", types.GetRequestID(ctx),
			"tenant_id", types.GetTenantID(ctx),
			"user_id", types.GetUserID(ctx),
		),
	}
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
