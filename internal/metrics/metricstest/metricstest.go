// Package metricstest installs one in-memory global meter provider per test
// binary. Values are cumulative across tests, so assert on deltas.
package metricstest

import (
	"context"
	"sync"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

var (
	installOnce sync.Once
	reader      *sdkmetric.ManualReader
)

// Reader reads back recorded metrics.
type Reader struct{ t testing.TB }

// Install sets the global meter provider on first call and returns a reader for it.
func Install(t testing.TB) *Reader {
	installOnce.Do(func() {
		reader = sdkmetric.NewManualReader()
		otel.SetMeterProvider(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)))
	})
	return &Reader{t: t}
}

// Sum totals a counter over series whose labels include match.
func (r *Reader) Sum(name string, match map[string]string) int64 {
	var total int64
	for _, s := range r.Series(name) {
		if contains(s.Labels, match) {
			total += s.Value
		}
	}
	return total
}

// Series is one counter series.
type Series struct {
	Labels map[string]string
	Value  int64
}

// Series returns every series recorded for a counter.
func (r *Reader) Series(name string) []Series {
	r.t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		r.t.Fatalf("collect metrics: %v", err)
	}
	var out []Series
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			sum, ok := m.Data.(metricdata.Sum[int64])
			if m.Name != name || !ok {
				continue
			}
			for _, dp := range sum.DataPoints {
				out = append(out, Series{Labels: labelMap(dp.Attributes), Value: dp.Value})
			}
		}
	}
	return out
}

func labelMap(set attribute.Set) map[string]string {
	out := map[string]string{}
	for _, kv := range set.ToSlice() {
		out[string(kv.Key)] = kv.Value.Emit()
	}
	return out
}

func contains(labels, match map[string]string) bool {
	for k, v := range match {
		if labels[k] != v {
			return false
		}
	}
	return true
}
