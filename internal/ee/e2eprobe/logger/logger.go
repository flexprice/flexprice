// Package logger is the probe's structured logger: JSON lines on stdout with
// the ctx-first calling convention every probe package shares.
package logger

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
)

// Logger writes structured JSON lines. A nil Logger discards everything.
type Logger struct {
	s *slog.Logger
}

// New returns a logger writing to stdout at level debug, info, warn or
// error; an empty level means info.
func New(level string) (*Logger, error) {
	lvl, err := parseLevel(level)
	if err != nil {
		return nil, err
	}
	return newWith(os.Stdout, lvl), nil
}

// Noop returns a logger that discards everything; tests use it.
func Noop() *Logger {
	return newWith(io.Discard, slog.LevelError)
}

func newWith(w io.Writer, lvl slog.Level) *Logger {
	return &Logger{s: slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{Level: lvl}))}
}

func parseLevel(level string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "", "info":
		return slog.LevelInfo, nil
	case "debug":
		return slog.LevelDebug, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	}
	return 0, fmt.Errorf("unknown log level %q (debug, info, warn, error)", level)
}

func (l *Logger) Debug(ctx context.Context, msg string, fields ...any) {
	l.log(ctx, slog.LevelDebug, msg, fields)
}

func (l *Logger) Info(ctx context.Context, msg string, fields ...any) {
	l.log(ctx, slog.LevelInfo, msg, fields)
}

func (l *Logger) Warn(ctx context.Context, msg string, fields ...any) {
	l.log(ctx, slog.LevelWarn, msg, fields)
}

func (l *Logger) Error(ctx context.Context, msg string, fields ...any) {
	l.log(ctx, slog.LevelError, msg, fields)
}

func (l *Logger) log(ctx context.Context, lvl slog.Level, msg string, fields []any) {
	if l == nil || l.s == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	l.s.Log(ctx, lvl, msg, fields...)
}
