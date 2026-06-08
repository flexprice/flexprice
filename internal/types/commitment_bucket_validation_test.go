package types

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTimeOfDayBuckets_ValidateNoOverlap(t *testing.T) {
	tests := []struct {
		name    string
		buckets TimeOfDayBuckets
		wantErr bool
		errSub  string
	}{
		{
			name: "non-overlapping disjoint",
			buckets: TimeOfDayBuckets{
				{Start: Bucket{9, 0}, End: Bucket{12, 0}},
				{Start: Bucket{13, 0}, End: Bucket{17, 0}},
			},
		},
		{
			name: "adjacent allowed (half-open)",
			buckets: TimeOfDayBuckets{
				{Start: Bucket{9, 0}, End: Bucket{12, 0}},
				{Start: Bucket{12, 0}, End: Bucket{17, 0}},
			},
		},
		{
			name: "overlapping",
			buckets: TimeOfDayBuckets{
				{Start: Bucket{9, 0}, End: Bucket{12, 0}},
				{Start: Bucket{11, 0}, End: Bucket{14, 0}},
			},
			wantErr: true, errSub: "overlap",
		},
		{
			name: "midnight-wrap and morning overlap",
			buckets: TimeOfDayBuckets{
				{Start: Bucket{22, 0}, End: Bucket{6, 0}},
				{Start: Bucket{4, 0}, End: Bucket{8, 0}},
			},
			wantErr: true, errSub: "overlap",
		},
		{
			name: "midnight-wrap with non-overlapping morning",
			buckets: TimeOfDayBuckets{
				{Start: Bucket{22, 0}, End: Bucket{6, 0}},
				{Start: Bucket{7, 0}, End: Bucket{20, 0}},
			},
		},
		{
			name: "single bucket",
			buckets: TimeOfDayBuckets{
				{Start: Bucket{9, 0}, End: Bucket{17, 0}},
			},
		},
		{
			name:    "empty buckets",
			buckets: TimeOfDayBuckets{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.buckets.ValidateNoOverlap()
			if tt.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errSub)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestTimeOfDayBuckets_ValidateWindowAlignment(t *testing.T) {
	tests := []struct {
		name      string
		buckets   TimeOfDayBuckets
		windowMin int
		wantErr   bool
		errSub    string
	}{
		{
			name:      "1h window, hour-aligned 1h bucket",
			buckets:   TimeOfDayBuckets{{Start: Bucket{9, 0}, End: Bucket{10, 0}}},
			windowMin: 60,
		},
		{
			name:      "1h window, hour-aligned 8h range bucket",
			buckets:   TimeOfDayBuckets{{Start: Bucket{9, 0}, End: Bucket{17, 0}}},
			windowMin: 60,
		},
		{
			name:      "1h window, misaligned start",
			buckets:   TimeOfDayBuckets{{Start: Bucket{9, 30}, End: Bucket{10, 30}}},
			windowMin: 60,
			wantErr:   true, errSub: "alignment",
		},
		{
			name:      "1h window, fractional duration",
			buckets:   TimeOfDayBuckets{{Start: Bucket{9, 0}, End: Bucket{9, 30}}},
			windowMin: 60,
			wantErr:   true, errSub: "multiple",
		},
		{
			name:      "15m window, 45m bucket",
			buckets:   TimeOfDayBuckets{{Start: Bucket{9, 0}, End: Bucket{9, 45}}},
			windowMin: 15,
		},
		{
			name:      "15m window, misaligned start",
			buckets:   TimeOfDayBuckets{{Start: Bucket{9, 10}, End: Bucket{9, 25}}},
			windowMin: 15,
			wantErr:   true, errSub: "alignment",
		},
		{
			name:      "window > 60 min rejected",
			buckets:   TimeOfDayBuckets{{Start: Bucket{9, 0}, End: Bucket{12, 0}}},
			windowMin: 180,
			wantErr:   true, errSub: "60",
		},
		{
			name:      "non-windowed (0) rejected when buckets present",
			buckets:   TimeOfDayBuckets{{Start: Bucket{9, 0}, End: Bucket{10, 0}}},
			windowMin: 0,
			wantErr:   true, errSub: "windowed",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.buckets.ValidateWindowAlignment(tt.windowMin)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errSub)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
