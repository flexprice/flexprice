package httpclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	oteltrace "go.opentelemetry.io/otel/trace"
)

func TestOtelTransport_WrapsBase(t *testing.T) {
	// nil base -> still returns an otelhttp transport (using DefaultTransport)
	rt := OtelTransport(nil)
	if rt == nil {
		t.Fatal("OtelTransport(nil) returned nil")
	}
	if _, ok := rt.(*otelhttp.Transport); !ok {
		t.Fatalf("expected *otelhttp.Transport, got %T", rt)
	}

	// explicit base -> still wrapped in an otelhttp transport
	base := &http.Transport{}
	rt = OtelTransport(base)
	if _, ok := rt.(*otelhttp.Transport); !ok {
		t.Fatalf("expected *otelhttp.Transport wrapping base, got %T", rt)
	}
}

func TestNewOtelHTTPClient_PreservesTimeout(t *testing.T) {
	c := NewOtelHTTPClient(42 * time.Second)
	if c.Timeout != 42*time.Second {
		t.Fatalf("expected timeout 42s, got %s", c.Timeout)
	}
	if _, ok := c.Transport.(*otelhttp.Transport); !ok {
		t.Fatalf("expected otel-wrapped transport, got %T", c.Transport)
	}
}

func TestNewDefaultClient_IsInstrumented(t *testing.T) {
	c, ok := NewDefaultClient().(*DefaultClient)
	if !ok {
		t.Fatal("NewDefaultClient did not return *DefaultClient")
	}
	if _, ok := c.client.Transport.(*otelhttp.Transport); !ok {
		t.Fatalf("expected otel-wrapped transport on default client, got %T", c.client.Transport)
	}
}

func TestNewClientWithConfig_IsInstrumented(t *testing.T) {
	c, ok := NewClientWithConfig(ClientConfig{Timeout: 5 * time.Second}).(*DefaultClient)
	if !ok {
		t.Fatal("NewClientWithConfig did not return *DefaultClient")
	}
	if c.client.Timeout != 5*time.Second {
		t.Fatalf("expected timeout 5s, got %s", c.client.Timeout)
	}
	if _, ok := c.client.Transport.(*otelhttp.Transport); !ok {
		t.Fatalf("expected otel-wrapped transport, got %T", c.client.Transport)
	}
}

// TestSend_RecordsClientSpan verifies that an outbound request through the
// instrumented client produces exactly one CLIENT-kind span carrying the HTTP
// semantic attributes that SigNoz External API Monitoring relies on, and that
// the span is parented to an active span in the request context.
func TestSend_RecordsClientSpan(t *testing.T) {
	// Install a recording TracerProvider as the global provider that otelhttp uses.
	sr := tracetest.NewSpanRecorder()
	tp := trace.NewTracerProvider(trace.WithSpanProcessor(sr))
	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	defer otel.SetTracerProvider(prev)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	// Start a parent span so we can assert child linkage.
	ctx, parent := tp.Tracer("test").Start(context.Background(), "parent")

	client := NewDefaultClient()
	if _, err := client.Send(ctx, &Request{Method: http.MethodGet, URL: srv.URL}); err != nil {
		t.Fatalf("Send returned error: %v", err)
	}
	parent.End()

	var clientSpans []trace.ReadOnlySpan
	for _, s := range sr.Ended() {
		if s.SpanKind() == oteltrace.SpanKindClient {
			clientSpans = append(clientSpans, s)
		}
	}
	if len(clientSpans) != 1 {
		t.Fatalf("expected exactly 1 client span, got %d", len(clientSpans))
	}

	span := clientSpans[0]
	if span.Parent().SpanID() != parent.SpanContext().SpanID() {
		t.Errorf("client span is not parented to the active span")
	}

	attrs := map[string]string{}
	for _, kv := range span.Attributes() {
		attrs[string(kv.Key)] = kv.Value.Emit()
	}
	// otelhttp emits server.address and the request method; both are required by
	// SigNoz to attribute the call to an external host.
	if attrs["server.address"] == "" {
		t.Errorf("expected server.address attribute to be set, got attrs=%v", attrs)
	}
	if attrs["http.request.method"] == "" {
		t.Errorf("expected http.request.method attribute to be set, got attrs=%v", attrs)
	}
}
