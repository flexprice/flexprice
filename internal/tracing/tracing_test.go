package tracing

import (
	"context"
	"testing"

	"github.com/flexprice/flexprice/internal/config"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
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
