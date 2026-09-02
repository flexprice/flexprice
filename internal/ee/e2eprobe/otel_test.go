package e2eprobe

import (
	"context"
	"testing"
)

func TestNewTracerProvider_DisabledReturnsNoop(t *testing.T) {
	tp, shutdown, err := NewTracerProvider(context.Background(), OTELConfig{Enabled: false}, "e2eprobe-test")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if tp == nil || shutdown == nil {
		t.Fatal("nil result")
	}
	_, span := tp.Tracer("x").Start(context.Background(), "op")
	span.End()
	if err := shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
}

func TestNewTracerProvider_NoEndpoint_FallsBackToNoop(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	tp, shutdown, err := NewTracerProvider(context.Background(), OTELConfig{Enabled: true}, "e2eprobe-test")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if tp == nil {
		t.Fatal("tp nil")
	}
	_ = shutdown(context.Background())
}
