package logger

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

func newObserved(level zapcore.Level) (*Logger, *observer.ObservedLogs) {
	core, logs := observer.New(level)
	return NewFromSugared(zap.New(core).Sugar()), logs
}

func TestLoggerLevelGate(t *testing.T) {
	tests := []struct {
		name      string
		level     zapcore.Level
		emit      func(*Logger)
		wantCount int
	}{
		{"debug dropped at info", zapcore.InfoLevel, func(l *Logger) { l.Debug(context.Background(), "m") }, 0},
		{"info emitted at info", zapcore.InfoLevel, func(l *Logger) { l.Info(context.Background(), "m") }, 1},
		{"warn emitted at info", zapcore.InfoLevel, func(l *Logger) { l.Warn(context.Background(), "m") }, 1},
		{"info dropped at warn", zapcore.WarnLevel, func(l *Logger) { l.Info(context.Background(), "m") }, 0},
		{"debug emitted at debug", zapcore.DebugLevel, func(l *Logger) { l.Debug(context.Background(), "m") }, 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			l, logs := newObserved(tc.level)
			tc.emit(l)
			assert.Equal(t, tc.wantCount, logs.Len())
		})
	}
}

func TestLoggerEmittedEntryCarriesContextFields(t *testing.T) {
	l, logs := newObserved(zapcore.InfoLevel)
	l.Info(context.Background(), "m", "k", "v")

	entries := logs.All()
	assert.Len(t, entries, 1)
	fields := entries[0].ContextMap()
	assert.Contains(t, fields, "request_id")
	assert.Contains(t, fields, "tenant_id")
	assert.Equal(t, "v", fields["k"])
}

func TestLoggerDisabledLevelDoesNotAllocateContextFields(t *testing.T) {
	l, _ := newObserved(zapcore.InfoLevel)
	ctx := context.Background()

	allocs := testing.AllocsPerRun(100, func() {
		l.Debug(ctx, "m", "k", "v")
	})
	assert.LessOrEqual(t, allocs, float64(2))
}
