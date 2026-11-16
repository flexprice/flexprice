package clickhouse

import (
	"context"
	"testing"

	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveInsertChunk(t *testing.T) {
	tests := []struct {
		name string
		in   int
		want int
	}{
		{"zero uses default", 0, 100},
		{"negative uses default", -5, 100},
		{"in range kept", 5000, 5000},
		{"capped", 1_000_000, 10000},
		{"cap boundary", 10000, 10000},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, resolveInsertChunk(tt.in))
		})
	}
}

func TestBulkInsertMeterUsageEmptyIsNoop(t *testing.T) {
	r := &MeterUsageRepository{}
	require.NoError(t, r.BulkInsertMeterUsage(context.Background(), nil))
	require.NoError(t, r.BulkInsertMeterUsageChunked(context.Background(), []*events.MeterUsage{}, 5000))
}
