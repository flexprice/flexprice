package logger

import (
	"context"

	"go.temporal.io/sdk/log"
)

// temporalLogger adapts our Logger to temporal's logging interface.
// Pass workflow.Context (from workflow) or activity context (from activity).
type temporalLogger struct {
	logger *Logger
	ctx    context.Context
}

// GetTemporalLogger returns a temporal-compatible logger bound to the given ctx.
func (l *Logger) GetTemporalLogger(ctx context.Context) log.Logger {
	return &temporalLogger{logger: l, ctx: ctx}
}

func (t *temporalLogger) Debug(msg string, keyvals ...interface{}) {
	t.logger.Debug(t.ctx, msg, keyvals...)
}

func (t *temporalLogger) Info(msg string, keyvals ...interface{}) {
	t.logger.Info(t.ctx, msg, keyvals...)
}

func (t *temporalLogger) Warn(msg string, keyvals ...interface{}) {
	t.logger.Warn(t.ctx, msg, keyvals...)
}

func (t *temporalLogger) Error(msg string, keyvals ...interface{}) {
	t.logger.Error(t.ctx, msg, keyvals...)
}
