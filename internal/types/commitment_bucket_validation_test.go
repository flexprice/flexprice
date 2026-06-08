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
