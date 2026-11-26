// Package logger is the probe's structured logger: JSON lines on stdout with
// the ctx-first calling convention every probe package shares, and a bounded
// memory of recent lines for the support bundle.
package logger

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
)

// recentLines is how many emitted lines a logger remembers.
const recentLines = 500

// Logger writes structured JSON lines. A nil Logger discards everything.
type Logger struct {
	s    *slog.Logger
	ring *ring
}

// New returns a logger writing to stdout at level debug, info, warn or
// error; an empty level means info.
func New(level string) (*Logger, error) {
	lvl, err := parseLevel(level)
	if err != nil {
		return nil, err
	}
	return newWith(os.Stdout, lvl, recentLines), nil
}

// Noop returns a logger that discards everything and remembers nothing.
func Noop() *Logger {
	return &Logger{s: slog.New(slog.NewJSONHandler(io.Discard, nil))}
}

func newWith(w io.Writer, lvl slog.Level, keep int) *Logger {
	r := &ring{keep: keep}
	return &Logger{s: slog.New(slog.NewJSONHandler(io.MultiWriter(w, r), &slog.HandlerOptions{Level: lvl})), ring: r}
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

// Recent returns the newest lines the logger emitted, oldest first.
func (l *Logger) Recent() []string {
	if l == nil || l.ring == nil {
		return nil
	}
	return l.ring.recent()
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

// ring keeps the last keep lines written to it; slog writes one line per call.
type ring struct {
	mu    sync.Mutex
	lines []string
	keep  int
}

func (r *ring) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lines = append(r.lines, strings.TrimRight(string(p), "\n"))
	if len(r.lines) > r.keep {
		r.lines = append([]string(nil), r.lines[len(r.lines)-r.keep:]...)
	}
	return len(p), nil
}

func (r *ring) recent() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.lines...)
}
