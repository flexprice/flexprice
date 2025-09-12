package logger

import "go.temporal.io/sdk/log"

// temporalLogger adapts our Logger to temporal's logging interface
type temporalLogger struct {
	logger *Logger
}

// GetTemporalLogger returns a temporal-compatible logger
func (l *Logger) GetTemporalLogger() log.Logger {
	return &temporalLogger{logger: l}
}

func (t *temporalLogger) Debug(msg string, keyvals ...interface{}) {
	t.logger.Debugw(msg, keyvals...)
}

func (t *temporalLogger) Info(msg string, keyvals ...interface{}) {
	t.logger.Infow(msg, keyvals...)
}

func (t *temporalLogger) Warn(msg string, keyvals ...interface{}) {
	t.logger.Warnw(msg, keyvals...)
}

func (t *temporalLogger) Error(msg string, keyvals ...interface{}) {
	t.logger.Errorw(msg, keyvals...)
}
