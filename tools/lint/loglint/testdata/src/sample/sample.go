package sample

import (
	"context"
	"fmt"

	"logger"
)

type Logger struct{}

func (l *Logger) Error(ctx context.Context, msg string, kv ...interface{}) {}
func (l *Logger) Info(ctx context.Context, msg string, kv ...interface{})  {}

// LL002: direct use of logger.L
func globalLoggerUsage() {
	logger.L.Error(context.Background(), "bad", "error", "x") // want `LL002`
}

// LL002: use of logger.GetLogger()
func getLoggerUsage(ctx context.Context) {
	logger.GetLogger() // want `LL002`
}

// LL004: fmt.Println banned outside cmd/
func fmtPrintUsage() {
	fmt.Println("debug value") // want `LL004`
}

// LL006: Error() without "error" key
func errorMissingField(log *Logger) {
	log.Error(context.Background(), "something failed") // want `LL006`
}

// LL008: Info() with checkpoint message
func checkpointInfo(log *Logger) {
	log.Info(context.Background(), "processing request") // want `LL008`
}

// goodUsage: no violations expected
func goodUsage(log *Logger, ctx context.Context) {
	log.Error(ctx, "invoice failed", "error", "details here")
	log.Info(ctx, "invoice finalized", "invoice_id", "inv_123")
}
