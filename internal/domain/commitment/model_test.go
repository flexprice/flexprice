package commitment

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCommitment_IsActiveAt(t *testing.T) {
	start := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(1, 0, 0)
	termAt := start.AddDate(0, 6, 0)
	base := &Commitment{StartDate: start, EndDate: end}
	terminated := &Commitment{StartDate: start, EndDate: end, TerminatedAt: &termAt}

	tests := []struct {
		name string
		c    *Commitment
		at   time.Time
		want bool
	}{
		{"before start", base, start.Add(-time.Nanosecond), false},
		{"at start", base, start, true},
		{"one instant before end", base, end.Add(-time.Nanosecond), true},
		{"at end", base, end, false},
		{"before terminated_at", terminated, termAt.Add(-time.Nanosecond), true},
		{"at terminated_at", terminated, termAt, false},
		{"after terminated_at", terminated, termAt.Add(time.Hour), false},
		{"nil", nil, start, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, tt.c.IsActiveAt(tt.at))
		})
	}
}
