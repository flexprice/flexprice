package tracing

import (
	"context"
	"os"
	"testing"

	"github.com/flexprice/flexprice/internal/config"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
	"go.opentelemetry.io/otel/trace"
)

// ctxWithTraceID returns a context carrying a span with the given low-8-byte
// trace ID value (the bytes storageSpanSampled hashes on).
func ctxWithTraceID(low uint64) context.Context {
	var tid trace.TraceID
	tid[0] = 1 // keep the ID valid (not all-zero)
	for i := 0; i < 8; i++ {
		tid[15-i] = byte(low >> (8 * i))
	}
	sc := trace.NewSpanContext(trace.SpanContextConfig{TraceID: tid})
	return trace.ContextWithSpanContext(context.Background(), sc)
}

func svcWithRate(rate float64) *Service {
	return &Service{cfg: &config.Configuration{
		Otel: config.OtelConfig{Traces: config.OtelTracesConfig{StorageSpansSampleRate: rate}},
	}}
}

func TestStorageSpanSampled(t *testing.T) {
	// low 8 bytes = 0 -> val 0 (smallest); all-ones -> val ~max.
	lowCtx := ctxWithTraceID(0)
	highCtx := ctxWithTraceID(^uint64(0))

	tests := []struct {
		name string
		rate float64
		ctx  context.Context
		want bool
	}{
		{"rate 1.0 always samples", 1.0, highCtx, true},
		{"rate 0.0 never samples", 0.0, lowCtx, false},
		{"below threshold sampled", 0.5, lowCtx, true},
		{"above threshold dropped", 0.5, highCtx, false},
		{"no trace context always sampled", 0.5, context.Background(), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := svcWithRate(tt.rate).storageSpanSampled(tt.ctx); got != tt.want {
				t.Fatalf("storageSpanSampled(rate=%v) = %v, want %v", tt.rate, got, tt.want)
			}
		})
	}
}

// A kept/dropped decision must be identical for every span in the same trace.
func TestStorageSpanSampledDeterministic(t *testing.T) {
	s := svcWithRate(0.3)
	ctx := ctxWithTraceID(12345)
	first := s.storageSpanSampled(ctx)
	for i := 0; i < 100; i++ {
		if s.storageSpanSampled(ctx) != first {
			t.Fatal("decision not stable within a trace")
		}
	}
}

// svcWithManualMeter builds a Service wired to a manual metric reader so the
// full recording path (startStorageSpan → Finish → recordStorageMetric) can be
// exercised in-process, without an OTLP endpoint. Storage spans stay disabled
// (default cfg) to prove metrics record independently of span emission.
func svcWithManualMeter(t *testing.T) (*Service, *sdkmetric.ManualReader) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	meter := mp.Meter("test")
	s := &Service{cfg: &config.Configuration{}, metricsEnabled: true}
	var err error
	if s.dbDuration, err = meter.Float64Histogram("db.client.duration"); err != nil {
		t.Fatal(err)
	}
	if s.cacheRequests, err = meter.Int64Counter("cache.requests"); err != nil {
		t.Fatal(err)
	}
	return s, reader
}

// collect gathers metrics and returns, per metric name, the list of data-point
// attribute sets (as maps) so tests can assert labels without wrestling generics.
func collect(t *testing.T, reader *sdkmetric.ManualReader) map[string][]map[string]string {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatal(err)
	}
	out := map[string][]map[string]string{}
	attrsOf := func(set attribute.Set) map[string]string {
		m := map[string]string{}
		for _, kv := range set.ToSlice() {
			m[string(kv.Key)] = kv.Value.Emit()
		}
		return m
	}
	for _, sm := range rm.ScopeMetrics {
		for _, mtr := range sm.Metrics {
			switch d := mtr.Data.(type) {
			case metricdata.Histogram[float64]:
				for _, dp := range d.DataPoints {
					out[mtr.Name] = append(out[mtr.Name], attrsOf(dp.Attributes))
				}
			case metricdata.Sum[int64]:
				for _, dp := range d.DataPoints {
					out[mtr.Name] = append(out[mtr.Name], attrsOf(dp.Attributes))
				}
			}
		}
	}
	return out
}

func hasDP(dps []map[string]string, want map[string]string) bool {
	for _, dp := range dps {
		match := true
		for k, v := range want {
			if dp[k] != v {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

func TestStorageMetricsRecorded(t *testing.T) {
	s, reader := svcWithManualMeter(t)
	ctx := context.Background()

	// 1) Successful Postgres repository call.
	sp, _ := s.startStorageSpan(ctx, "repository.price.list", "db.repository", "postgresql", nil)
	sp.Finish()

	// 2) Failed ClickHouse call → status=error.
	sp, _ = s.startStorageSpan(ctx, "clickhouse.query", "db.clickhouse", "clickhouse", nil)
	sp.SetStatusError(context.DeadlineExceeded)
	sp.Finish()

	// 3) Redis cache hit → db.client.duration + cache.requests{result=hit}.
	sp, _ = s.startStorageSpan(ctx, "cache.secret.get", "cache.get", "redis", nil)
	sp.SetCacheHit(true)
	sp.Finish()

	got := collect(t, reader)

	dur := got["db.client.duration"]
	if !hasDP(dur, map[string]string{"operation": "repository.price.list", "db_system": "postgresql", "status": "ok"}) {
		t.Errorf("missing ok postgres duration point; got %v", dur)
	}
	if !hasDP(dur, map[string]string{"operation": "clickhouse.query", "db_system": "clickhouse", "status": "error"}) {
		t.Errorf("missing error clickhouse duration point; got %v", dur)
	}
	if !hasDP(got["cache.requests"], map[string]string{"operation": "cache.secret.get", "result": "hit"}) {
		t.Errorf("missing cache hit point; got %v", got["cache.requests"])
	}
}

// Metrics must NOT record when the service has metrics disabled.
func TestNoMetricsWhenDisabled(t *testing.T) {
	s, reader := svcWithManualMeter(t)
	s.metricsEnabled = false // startStorageSpan must not stamp metric metadata

	sp, _ := s.startStorageSpan(context.Background(), "repository.price.list", "db.repository", "postgresql", nil)
	sp.Finish() // sp is nil here (spans off + metrics off) — must be a safe no-op

	if got := collect(t, reader); len(got["db.client.duration"]) != 0 {
		t.Errorf("expected no data points when metrics disabled; got %v", got)
	}
}

// A nil *Service must be a safe no-op — StartRepositorySpan is called on a nil
// tracing service when tracing isn't wired (regression: the metrics change
// added a direct s.metricsEnabled deref that panicked on nil, FLE-1003 CI).
func TestNilServiceStorageSpanNoPanic(t *testing.T) {
	var s *Service // nil
	span, _ := s.StartRepositorySpan(context.Background(), "postgresql", "price", "list", nil)
	span.Finish() // must also be nil-safe
	if span != nil {
		t.Errorf("expected nil span from nil service, got %v", span)
	}
}

func TestTemporalMetricsHandler(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))

	t.Run("nil service", func(t *testing.T) {
		var s *Service
		if s.TemporalMetricsHandler() != nil {
			t.Fatal("expected nil handler from nil service")
		}
	})

	t.Run("metrics disabled", func(t *testing.T) {
		s := &Service{
			cfg: &config.Configuration{
				Otel: config.OtelConfig{Metrics: config.OtelMetricsConfig{TemporalEnabled: true}},
			},
			metricsEnabled: false,
			meterProvider:  mp,
		}
		if s.TemporalMetricsHandler() != nil {
			t.Fatal("expected nil when metrics pipeline is off")
		}
	})

	t.Run("temporal flag off", func(t *testing.T) {
		s := &Service{
			cfg: &config.Configuration{
				Otel: config.OtelConfig{Metrics: config.OtelMetricsConfig{TemporalEnabled: false}},
			},
			metricsEnabled: true,
			meterProvider:  mp,
		}
		if s.TemporalMetricsHandler() != nil {
			t.Fatal("expected nil when temporal_enabled is false")
		}
	})

	t.Run("metrics on and temporal enabled", func(t *testing.T) {
		s := &Service{
			cfg: &config.Configuration{
				Otel: config.OtelConfig{Metrics: config.OtelMetricsConfig{TemporalEnabled: true}},
			},
			metricsEnabled: true,
			meterProvider:  mp,
		}
		if s.TemporalMetricsHandler() == nil {
			t.Fatal("expected non-nil Temporal MetricsHandler")
		}
	})
}

// meterWithHTTPViews builds a MeterProvider with the views initMeter installs.
func meterWithHTTPViews(t *testing.T, httpServerEnabled bool) (metric.Meter, *sdkmetric.ManualReader) {
	t.Helper()
	s := &Service{cfg: &config.Configuration{
		Otel: config.OtelConfig{Metrics: config.OtelMetricsConfig{HTTPServerEnabled: httpServerEnabled}},
	}}
	reader := sdkmetric.NewManualReader()
	opts := append([]sdkmetric.Option{sdkmetric.WithReader(reader)}, s.httpMetricViews()...)
	return sdkmetric.NewMeterProvider(opts...).Meter("test"), reader
}

// recordHTTPServerMetrics emits what otelgin does, with its full attribute set.
func recordHTTPServerMetrics(t *testing.T, meter metric.Meter) {
	t.Helper()
	dur, err := meter.Float64Histogram("http.server.request.duration")
	if err != nil {
		t.Fatal(err)
	}
	body, err := meter.Int64Histogram("http.server.request.body.size")
	if err != nil {
		t.Fatal(err)
	}
	client, err := meter.Float64Histogram("http.client.request.duration")
	if err != nil {
		t.Fatal(err)
	}
	attrs := metric.WithAttributes(
		attribute.String("http.request.method", "POST"),
		attribute.String("http.route", "/v1/events"),
		attribute.Int("http.response.status_code", 200),
		attribute.String("server.address", "api.flexprice.io"),
		attribute.Int("server.port", 8080),
		attribute.String("network.protocol.version", "1.1"),
	)
	dur.Record(context.Background(), 0.12, attrs)
	body.Record(context.Background(), 512, attrs)
	client.Record(context.Background(), 0.05, attrs)
}

// Flag off keeps today's behaviour: every HTTP metric is dropped.
func TestHTTPServerMetricsDroppedByDefault(t *testing.T) {
	meter, reader := meterWithHTTPViews(t, false)
	recordHTTPServerMetrics(t, meter)

	got := collect(t, reader)
	for _, name := range []string{
		"http.server.request.duration",
		"http.server.request.body.size",
		"http.client.request.duration",
	} {
		if len(got[name]) != 0 {
			t.Errorf("%s should be dropped when http_server_enabled is false; got %v", name, got[name])
		}
	}
}

// Flag on: request duration survives trimmed; body-size and client stay dropped.
func TestHTTPServerMetricsKeptWhenEnabled(t *testing.T) {
	meter, reader := meterWithHTTPViews(t, true)
	recordHTTPServerMetrics(t, meter)

	got := collect(t, reader)
	dps := got["http.server.request.duration"]
	if len(dps) != 1 {
		t.Fatalf("expected 1 request-duration point, got %v", dps)
	}
	if !hasDP(dps, map[string]string{
		"http.request.method":       "POST",
		"http.route":                "/v1/events",
		"http.response.status_code": "200",
	}) {
		t.Errorf("expected route/method/status attributes to survive; got %v", dps)
	}
	for _, dropped := range []string{"server.address", "server.port", "network.protocol.version"} {
		if _, ok := dps[0][dropped]; ok {
			t.Errorf("attribute %q should have been filtered out; got %v", dropped, dps[0])
		}
	}
	if len(got["http.server.request.body.size"]) != 0 {
		t.Errorf("body-size histogram should stay dropped; got %v", got["http.server.request.body.size"])
	}
	if len(got["http.client.request.duration"]) != 0 {
		t.Errorf("http.client.* should stay dropped; got %v", got["http.client.request.duration"])
	}
}

// service.instance.id must be on both resources and has no off switch: without it
// N replicas write one series with independent counter resets.
func TestResourcesAlwaysCarryInstanceID(t *testing.T) {
	host, err := os.Hostname()
	if err != nil || host == "" {
		t.Skip("hostname unavailable")
	}

	s := &Service{cfg: &config.Configuration{}}

	// Both signals carry it, so a suspect series leads to that replica's spans.
	for name, build := range map[string]func(context.Context) (*resource.Resource, error){
		"metrics": s.newMetricResource,
		"traces":  s.newResource,
	} {
		res, err := build(context.Background())
		if err != nil {
			t.Fatalf("%s resource: %v", name, err)
		}
		got, ok := instanceIDOf(res)
		if !ok {
			t.Errorf("service.instance.id missing from the %s resource", name)
			continue
		}
		if got != host {
			t.Errorf("%s service.instance.id = %q, want hostname %q", name, got, host)
		}
	}
}

// instanceIDOf returns the resource's service.instance.id, if it has one.
func instanceIDOf(res *resource.Resource) (string, bool) {
	for _, kv := range res.Attributes() {
		if kv.Key == semconv.ServiceInstanceIDKey {
			return kv.Value.AsString(), true
		}
	}
	return "", false
}

// WithFromEnv merges last, so a deployment can override the hostname identity.
func TestMetricResourceInstanceIDEnvOverride(t *testing.T) {
	t.Setenv("OTEL_RESOURCE_ATTRIBUTES", "service.instance.id=pod-from-downward-api")

	s := &Service{cfg: &config.Configuration{}}
	res, err := s.newMetricResource(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	got, ok := instanceIDOf(res)
	if !ok {
		t.Fatal("service.instance.id missing from the metric resource")
	}
	if got != "pod-from-downward-api" {
		t.Fatalf("service.instance.id = %q, want the env override to win", got)
	}
}
