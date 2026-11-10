package v1

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHubspotTimestampFresh(t *testing.T) {
	const now = int64(1_700_000_000_000)
	tests := []struct {
		name string
		ts   int64
		want bool
	}{
		{"current", now, true},
		{"just inside past edge", now - hubspotTimestampToleranceMs, true},
		{"stale", now - hubspotTimestampToleranceMs - 1, false},
		{"just inside future edge", now + hubspotTimestampToleranceMs, true},
		{"future", now + hubspotTimestampToleranceMs + 1, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, hubspotTimestampFresh(tt.ts, now))
		})
	}
}
